# Platform fix — forgeable `authContext.target` defeats scope=any self/workplace guards

**Status: FALSIFIED + REVERTED** (`b8e3f7d6` shipped then reverted `a9915791`, Lattice Steward fire
2026-07-24). The blanket platform blank is architecturally wrong — see **§Falsified** below. The item
returns to the board as `📐 needs per-op design`. The three-layer adversarial review passed it (it
reasoned from the same false premise this doc first stated: "operators submit with no target"); **CI's
`TestRecordPII_TaskScopedNotConfinedToUnclaimed` is what caught the break** — a reminder that green
review + green local ≠ correct, and the full suite is the real gate.

## §Falsified — why a platform blank cannot work

The fix blanked `authContext.target` on every authorized path except platform scope=self and task
ephemeralGrant. That assumed **scope=any never legitimately carries a target**. FALSE:
`identity-domain` `RecordIdentityPII` is submitted by a front-desk actor holding a **platform
scope=any** grant with `authContext.target = <the claimed onboarding identity>` (≠ the actor) as a
*legitimate* signal — the script's `authContextTarget != ""` branch (`ddls.go:1320`) exempts it from
the "unclaimed-only" walk-in confinement so front-desk can record PII for a claimed onboarding
identity. `TestRecordPII_TaskScopedNotConfinedToUnclaimed` (real `CapabilityPipeline`) asserts this;
the blank made it `rejected`.

The forged clinic-booking target and this legitimate identity-onboarding target are **both** platform
scope=any with a non-actor target — **indistinguishable at the platform level.** Which one is a
forgery is a per-op semantic question (does *this* op's guard trust the target for a security
confinement, or merely as a routing/subject hint?). A single platform rule cannot answer it, so the
root fix is **per-op**, not one processor change. clinic's W1 Inc 2a `authContextTarget == op.actor`
keying is the right shape *for ops whose exemption is a self-check* — but it would itself break
identity onboarding, which genuinely needs `target != actor`. Each of cafe / wellness / maintenance /
lease-signing / identity needs its own exploitability verdict (is the target trusted for security, and
if so must it equal actor). That is Designer-lane work.

**What stands:** the vulnerability is real (cafe/wellness/maintenance/lease-signing keying `!= ""` are
forgeable); clinic is already fixed in-package. What's retired is the *one-platform-change* approach.

---

The original design follows, kept for the record (its premise §"Why no legitimate flow breaks" is the
one CI falsified).

**Discharges the ★★★ security row in
`backlog/lattice.md` (Security & trust boundary) and the cross-package adjacent-find filed in
[`persona-worlds-design.md`](persona-worlds-design.md) §Fire W1 Inc 2a as-built.**

## The vulnerability

The Gateway forwards the client's `authContext.target` verbatim into the operation envelope
(`gateway.go:753` → `op.AuthContext.Target`), and `starlark_runner.go:432` exposes it to every op
script as `op.authContextTarget`. Step 3 (`step3_auth_capability.go` `matchPlatformPermission`)
authorizes a **platform `scope=any`** grant **without inspecting `authContext.target`**. So a
script that keys a self / workplace exemption on `authContextTarget` (most do — see the inventory)
trusts a field any `scope=any` holder can forge:

- `cafe-domain`, `wellness-domain`, `maintenance-domain`, `lease-signing` key their exemption on
  `authContextTarget != ""` → a `scope=any` operator forging any non-empty target skips workplace
  confinement / a self-service check (cafe is the same multi-org workplace gate clinic had).
- `clinic-domain` was hardened in-package during W1 Inc 2a (keys on `authContextTarget == op.actor`);
  the root enabler stayed open for the other four.

## The fix (one central processor change)

Sanitize the forgeable target at the single point where auth provenance is known — right after a
successful step-3 `Authorize`, before the script runs. Blank `env.AuthContext.Target` **unless the
resolved grant actually validated it against the actor or a minted grant.**

Two — and only two — authorized paths bind the target, so only they may forward it:

| Resolved path | Validates target? | Why |
|---|---|---|
| platform `scope=self` | **yes** | step 3 *requires* `target == actor` (`matchPlatformPermission` "self" case) |
| task / `ephemeralGrant` | **yes** | `matchEphemeralGrant` matches `g.Target == ac.Target` against the minted grant (`step3_auth_capability.go:346`) |
| platform `scope=any` | no | the "any" case never reads target — **the forgery vector** |
| service (`serviceAccess`) | no | `matchServiceAccess` never reads target |
| stub authorizer (test-only) | n/a | resolves nil; makes no security claim → envelope left untouched |

**This is a deliberate refinement of the filed hypothesis** ("blank unless scope=self"). Blanking on
*everything but scope=self* would break the legitimate **task path** — lease-signing's onboarding
userTask (`scripts.go:284`) and identity claim (`identity-domain/ddls.go:1312`) submit under an
ephemeralGrant that binds target. Keeping the task path is required for correctness, and it is safe
because the task path validates target just as tightly as scope=self.

### Why no legitimate flow breaks

`authContextTarget` is, across all six packages, the **self-service marker** — it is set only when an
actor acts as itself (scope=self, `target == actor`) or through a task grant. Operators (scope=any)
submit with **no** `authContext` on the staff path; they identify the subject via the payload
(`payload.patient` etc.), never `authContext.target`. So blanking an unvalidated target changes
behavior only for a forged target arriving through scope=any/service — exactly the vuln.

**One fail-closed edge (acceptable, pre-existing in the match order):** an actor holding *both* a
`scope=any` and a `scope=self` grant for one operationType, self-acting, matches the `scope=any` row
first (`matchPlatformPermission` returns on first match) → its target is blanked → a script's
`== op.actor` self-exemption does not fire, so the actor is confined on the operator path instead of
exempted as self. This is fail-closed (a `scope=any` match means the platform authorized it *as an
operator*), not a bypass; at worst a UX quirk for a staff-member-who-is-also-a-consumer. The
ordering, not this fix, decides which grant matches.

## Scope

- `internal/processor/commit_path.go` — add the sanitize guard after the step-3 authorize (outside
  the commit-retry loop, after the async trace `Emit`, before `commitPipeline`).
- `internal/processor/operation_context.go` (or `commit_path.go`) — add `authTargetValidated(rp)`.
- Security proof colocated in `internal/processor`: (1) exhaustive unit test on
  `authTargetValidated` over every path/scope shape; (2) a commit-path test with a recording
  executor asserting a forged scope=any target is blanked, a scope=self `target==actor` is preserved,
  and a task-grant target is preserved.

**Non-goals:** no package edits (the processor fix closes all four exploitable packages at once;
clinic's in-package `== op.actor` guard stays as redundant defense-in-depth), no Gateway change, no
contract change (this narrows what a script observes; it does not change Contract #2 §2.8 dispatch or
the `authContext` wire shape).
