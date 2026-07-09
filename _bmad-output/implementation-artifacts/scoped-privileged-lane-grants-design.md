# Scoped privileged-lane grants — a middle tier between ordinary and root (design)

**Status:** ✅ **Andrew-ratified (2026-07-06) — mechanism C1.** Designer fire (Winston, 2026-07-06) ·
Lattice lane (Security & trust boundary) · **the "C" of** `loupe-operator-auth-lift-design.md` §4 ·
**sequenced after** B (the scoped `consoleOperator` role).

> **Ratification (Andrew, 2026-07-06): C1** — per-op `lanes` on the grant + a **core-owned allowlist** of
> grantable `{op→privileged-lane}` (v1 = the pkg-lifecycle trio at `meta`); `consoleOperator` stays an
> **ordinary** `cap.roles` actor (no anchor, no `SystemActorKeys` snapshot), so it also **fixes the
> boot-snapshot staleness finding**. C2 (curated intermediate anchor) rejected as analyzed (§4). Build
> sequenced after B, behind `real-actor-write-auth` Phase 1. **Contract #6 §6.4 edit** (§5): staged
> uncommitted when C's build is the next fire (specified now to avoid a days-long dangling edit in the
> shared tree).
>
> **B progress:** the `consoleOperator` role + its non-privileged permissions **shipped** `5bee182`
> (`packages/console-operator`); re-scope + relay wiring **shipped** `56911ac`/`6b1ab6e` — B is CLOSED,
> unblocking C.
>
> **C progress — Fire 1 (§7 item 1) shipped:** `PermissionSpec.Lanes []string` (`internal/pkgmgr`),
> optional per-op `lanes` on `PlatformPermission` (`internal/capabilitykv`), rbac-domain's
> `capabilityRoles` lens projects it, and the Processor's step-3 platform-path lane gate moved from a
> whole-doc pre-check to a per-matched-permission check (falls back to doc-level `lanes` when an entry
> carries none) — `internal/processor/step3_auth_capability.go`. Contract #6 §6.4 edit committed alongside
> (marked not-yet-enforced pending Fire 2). Behavior-preserving today: no `PermissionSpec` sets `Lanes`
> yet.
>
> **Fire 2 (§7 item 2) shipped** `0982345`: the core `{operationType→allowed-privileged-lanes}`
> allowlist (v1 = the pkg-lifecycle trio at `meta`) gates any entry-level `PlatformPermission.Lanes` —
> a non-allowlisted privileged lane is stripped to `default` and raises `PrivilegedLaneGrantRejected`
> (`AuthAlertEmitter`, wired through `SelectAuthorizerArgs`'s capability-mode branch). The anchor's own
> root grant never carries entry-level lanes (its cypher projects doc-level `Lanes` only), so it's
> unaffected by the allowlist — proven by a dedicated unit test. **Next: Fire 3** —
> `consoleOperator` gains the allowlisted pkg-lifecycle grants + retires B's root-admin interim for
> the pkg tab.

---

## For Andrew

**What it does (two lines).** Lets a role grant a **specific privileged-lane op** (e.g. `InstallPackage`
on the `meta` lane) **without** conferring the whole kernel root floor — creating a real **middle tier**
between an ordinary default-lane actor and a root-equivalent `holdsRole→operator` identity. This is the
fix for the finding that `operator` is class-blind all-or-nothing root; with it, a `consoleOperator` can
run the pkg-lifecycle tab without being kernel root, and Loupe's operator lift (B) stops needing a
separate root-admin path for that tab.

**You already ratified doing C** (built after B). This design picks **how** — the one genuine fork:

| Mechanism | What | Trade-off |
|---|---|---|
| **C1 — per-op lanes in `cap.roles`, gated by a core allowlist (RECOMMENDED)** | A permission grant carries the lane(s) it authorizes; the step-3 lane gate becomes **per-matched-op**, not doc-level. Packages may grant an op at a privileged lane **only if `{op→lane}` is on a core-owned allowlist** (v1 = the pkg-lifecycle trio at `meta`). `consoleOperator` stays an **ordinary** `cap.roles` actor. | **Also fixes the boot-snapshot staleness finding** (no anchor, no `SystemActorKeys`, so a post-boot operator works immediately). Core keeps control of *which* privileged grants are possible; packages control *who* gets them. Cost: a small Contract #6 §6.4 touch (per-op `lanes`) + a per-op lane gate. |
| **C2 — a core-curated intermediate anchor tier** | Core projects a *narrower anchor* for a `holdsRole→consoleOperator` topology (pkg-lifecycle ops + `Lanes:[default,meta]`, nothing else privileged), read via the existing union. | Reuses the shipped anchor/union mechanism; privileged grants stay 100% anchor-owned. **But inherits boot-snapshot staleness** (consoleOperators must be seeded before Processor boot) and needs `SystemActorKeys`/routing to also discover the new topology. Doc-level lanes are coarser than per-op. |

**Recommendation: C1.** It fixes *both* findings (all-or-nothing **and** boot-snapshot staleness — a
post-boot `consoleOperator` just works), keeps the security-critical property (**core owns the policy of
what may be privileged-granted**, via the allowlist) while letting packages assign it, and is the more
general primitive. C2 is the conservative "reuse the anchor" option but drags the snapshot seam along. A
(operators = full root) is already rejected.

**Frozen-contract change: Contract #6 §6.4** — the `platformPermissions[]` entry gains an **optional
`lanes`** field, and the lane gate is specified as **per-matched-op** with the **core-allowlist**
constraint on privileged lanes. The exact edit is in §5 (I've *specified* it there rather than dangling it
uncommitted in the shared tree — C builds several fires out, behind B behind Phase 1, and a days-long
dangling contract edit across ~20 concurrent fires is the hazard the house rules warn about; I'll stage it
uncommitted when C's build is the next fire, or now if you say so).

---

## 1. Problem & intent — the missing middle tier

Grounded finding (`loupe-operator-auth-lift-design.md` §4, from the operator-privilege grounding): a
`holdsRole→operator` identity is **class-blind, fully root-equivalent** — it gets the anchor's 4 privileged
lanes + 6 kernel ops. There is **no way to express** "this actor may run the package-lifecycle ops but
nothing else privileged." Two consequences:

1. **All-or-nothing.** Making a human a Loupe operator makes them kernel root (`InstallPackage` +
   `CreateMetaVertex`/`TombstoneMetaVertex` + urgent/system lanes), when the intent was just "run the
   console, incl. installing a package."
2. **Boot-snapshot staleness.** Privileged routing is decided once at Processor boot (`SystemActorKeys`
   snapshot, `cmd/processor/main.go:119-135`), so a privileged identity created *after* boot is
   under-privileged until restart.

**Intent.** A **scoped privileged grant**: a role that confers a *named subset* of privileged-lane ops
(v1: the pkg-lifecycle trio at `meta`) and nothing more — so B's `consoleOperator` can run the pkg tab
without root, and a post-boot operator works without a Processor bounce.

---

## 2. Grounding — how lane auth works today (and the invariant it rests on)

- **Lane auth is doc-level.** `internal/processor/step3_auth_capability.go:257` —
  `if pathPlatform && !laneGranted(env.Lane, doc.Lanes)` → `LaneUnauthorized`. `laneGranted` (line 483)
  checks `env.Lane ∈ doc.Lanes`. The check runs **once** against the merged doc's `Lanes` array, **after**
  the op-match, independent of *which* permission matched.
- **Privileged lanes are anchor-only.** The bootstrap anchor (`internal/bootstrap/lenses.go:115`) projects
  `Lanes:["default","meta","urgent","system"]` + the 6 kernel ops, **only** for the `holdsRole→operator`
  root topology (`lenses.go:118-137`). `cap.roles` (`lenses.go:207`) projects `Lanes:["default"]` —
  static, package ops only. The union read (`internal/capabilitykv/read.go:33-84`) merges anchor ∪
  `cap.roles`, unioning lanes.
- **The `platformPermissions[]` entry is `{operationType, scope}`** — **no lane field** (Contract #6 §6.4).
  The lane is a property of the *whole doc*, not the *grant*.
- **The invariant this protects** (system-actor design §7.1): "*the floor's lanes must stay anchor-owned*"
  — a package must not be able to grant itself a privileged lane. Today that's structural: `cap.roles` is
  hard-coded `["default"]`. C must preserve the *intent* of that invariant (core controls privileged
  grants) even as it adds a middle tier.
- **What the lane actually gates** (verified reasoning, load-bearing for §8): the lane governs (a) the
  lane **gate** (this design's subject) and (b) **which Processor consumer/subject** processes the op
  (`ops.meta.>` vs `ops.default.>`). It does **not** itself confer capability — an op only commits if the
  actor **holds the op grant**. So "which lane may this op ride" is a *secondary* gate; the *primary* gate
  is always "is the op granted." That is why a scoped `{op→lane}` grant is safe: the op grant is the real
  key.

---

## 3. The mechanism (C1 — recommended)

### 3.1 Per-op lanes on the grant

`platformPermissions[]` entries gain an **optional `lanes: []string`** (default `["default"]` when absent
— back-compatible). A grant now says *"this op, at these lanes, at this scope."* `PermissionSpec`
(`internal/pkgmgr/definition.go`) gains a `Lanes []string`; a package declares
`{OperationType:"InstallPackage", Lanes:["meta"], GrantsTo:["consoleOperator"]}`.

### 3.2 The lane gate becomes per-matched-op

Step-3 today checks `env.Lane ∈ doc.Lanes` once. C1 changes the **platform path** to: after the op+scope
match, check `env.Lane ∈ matchedPermission.lanes` (falling back to the doc-level `Lanes` for the anchor and
for legacy entries with no per-op `lanes`, so root + existing behavior are unchanged). This is the natural
per-op refinement the `lenses.go:113-114` comment already anticipated.

### 3.3 The core allowlist — the safety gate (the security crux)

A package must **not** be able to grant an arbitrary op at a privileged lane (that would let any package
escalate a role to `meta`/`urgent`/`system`). So: a **core-owned allowlist** of
`{operationType → allowed-privileged-lanes}` (a Processor constant, v1 =
`{InstallPackage:[meta], UninstallPackage:[meta], UpgradePackage:[meta]}`). At step-3 (and/or at the
rbac-domain lens projection), a per-op **privileged** lane from `cap.roles` is **honored only if
`{op,lane}` is allowlisted**; otherwise it is **stripped to `default`** and a loud
`PrivilegedLaneGrantRejected` Health issue is raised. `default` is never restricted (it's the ordinary
floor). The result:

- **Core owns the *policy*** (which ops may ever be privilege-granted, and at which lane) — the invariant's
  real intent survives.
- **Packages own the *assignment*** (which role/actor gets the allowlisted grant) — the flexibility the
  finding needs.

### 3.4 `consoleOperator` stays an ordinary actor

Because the grant rides `cap.roles` (not the anchor), a `consoleOperator` is an **ordinary** actor: no
`holdsRole→operator`, not in `SystemActorKeys`, **no boot-snapshot dependency**. A post-boot
`consoleOperator` authorizes `InstallPackage` at `meta` immediately (its `cap.roles` doc carries the
allowlisted per-op lane). This **fixes the boot-snapshot staleness finding** as a side effect — the second
reason C1 beats C2.

### 3.5 Loupe (B) simplifies

With C1 built, B's "pkg-lifecycle stays a distinct root-admin path" interim is retired: the
`consoleOperator` role gains the allowlisted `{InstallPackage/Uninstall/Upgrade → meta}` grants, and
Loupe's pkg tab works as the verified operator — no root, no separate admin path. (B ships first with the
interim; C removes it.)

---

## 4. The fork, designed through: C1 vs C2

**C1 — per-op lanes + core allowlist (RECOMMENDED).** §3. Fixes both findings; core owns the privileged
policy; general. Cost: a §6.4 contract touch + a per-op lane gate + the allowlist.

**C2 — core-curated intermediate anchor.** Core projects a narrower anchor doc for a
`holdsRole→consoleOperator` topology (`{InstallPackage/Uninstall/Upgrade}` + `Lanes:[default,meta]`),
read via the existing union; `SystemActorKeys` extended to discover the new topology.
- *Pros:* reuses the shipped anchor/union verbatim; privileged grants stay 100% anchor-owned (no allowlist
  needed); no contract change (a second core lens + a routing tweak).
- *Cons:* (1) **inherits boot-snapshot staleness** — a `consoleOperator` must exist before Processor boot,
  so the finding's second half is *not* fixed; (2) **doc-level lanes are coarser** — the intermediate
  anchor's `Lanes:[default,meta]` lets the actor ride `meta` for *any* op it holds (harmless per §2's
  "lane isn't a capability" reasoning — the op grant is the real gate — but less precise than C1's
  per-op); (3) adding a *new* privileged tier later = a **core edit + bootstrap reseat** (the same
  rigidity the system-actor design rejected for "widen the anchor").
- *Why not C2:* it's the conservative reuse, but it leaves the boot-snapshot seam open and makes every
  future tier a core/bootstrap change. C1's allowlist keeps core in control of policy *without* baking each
  tier into the bootstrap cypher.

**Could a C2 variant beat C1?** A "core-curated anchor **without** the boot-snapshot dependency" would
require making the anchor read dynamic (per-request topology check) — a much larger change than C1's
allowlist. So no; C1 is the smaller path to fixing both findings.

---

## 5. Contract surface — Contract #6 §6.4 (the exact edit, specified)

The `platformPermissions[]` entry table gains a row, and the dispatch note gains the per-op lane + allowlist
rule. Precise proposed edit (to `docs/contracts/06-capability-kv.md` §6.4):

- **Add to the field table:**
  > `| lanes | no | Optional array of lanes this grant authorizes (default `["default"]` when absent). The step-3 lane gate checks `env.Lane` against the **matched permission's** `lanes` on the platform path (falling back to the doc-level `Lanes` for the anchor and for entries without `lanes`). A **privileged** lane (`meta`/`urgent`/`system`) in a package-projected (`cap.roles`) grant is honored **only if `{operationType, lane}` is on the core privileged-lane allowlist** (a Processor constant; v1 = pkg-lifecycle at `meta`); otherwise it is stripped to `default` and a `PrivilegedLaneGrantRejected` Health issue is raised. The **anchor** doc's lanes are unaffected (root keeps all four). |`
- **Add a sentence to §6.4 dispatch / and a note near §6.1's "privileged lanes are anchor-owned":** that
  the anchor-owned invariant is now "*privileged lanes are **core-policy-owned**: a package may **assign**
  an allowlisted `{op→privileged-lane}` grant, but only core defines the allowlist*" — the invariant's
  intent (core controls what may be privileged) is preserved; only the *mechanism* moves from "hard-coded
  `cap.roles=[default]`" to "core allowlist."

No other §6 section changes (the doc shape, the anchor, the union read are untouched beyond the per-op
`lanes` field). `PermissionSpec` (code, not contract) gains `Lanes []string`.

---

## 6. Reconciliation with the existing mental model

- **Doesn't this break "privileged lanes are anchor-owned" (system-actor §7.1)?** It **relaxes the
  mechanism, preserves the intent.** Today the invariant is enforced by hard-coding `cap.roles=[default]`.
  C1 replaces that with a **core allowlist**: a package can *assign* a privileged grant, but only from the
  set core sanctions. Core still decides *what may ever be privileged*; packages decide *who gets it*.
  That's the same trust boundary (privileged policy is core's), expressed as data instead of a hard-coded
  constant. Flagged explicitly so it's a conscious relaxation, not a smuggled one.
- **Didn't we reject "widen the anchor"?** Yes (system-actor Option A) — because it re-admitted package op
  *vocabulary* into the core bootstrap cypher and forced a core edit per new op. C1 does the opposite: no
  package vocabulary in core; the allowlist names only *kernel* ops (already core-known); packages reference
  them by grant. C2 is closer to the rejected "widen" shape (a bootstrap change per tier) — another mark
  against it.
- **New state?** A `lanes` field on permissions (optional, back-compat) + a core allowlist constant + the
  per-op gate. No new lens/bucket. `consoleOperator` is a role (rbac data, from B).

---

## 7. Decomposition (Lattice lane; sequenced after B)

1. **`lanes` on the grant + the per-op lane gate.** `PermissionSpec.Lanes`; the rbac-domain lens projects
   per-op lanes; `platformPermissions[]` carries `lanes`; step-3's platform-path lane gate checks the
   matched permission's lanes (fallback to doc-level). §6.4 edit committed with ratification.
2. **The core privileged-lane allowlist + fail-closed strip.** The Processor constant
   (`{InstallPackage/Uninstall/Upgrade → meta}`); honor a privileged per-op lane only if allowlisted, else
   strip to `default` + `PrivilegedLaneGrantRejected` Health issue. Unit matrix: allowlisted grant →
   `meta` allowed; non-allowlisted privileged grant → stripped + denied on `meta`; `default` always fine;
   anchor/root unchanged.
3. **`consoleOperator` gains the allowlisted pkg-lifecycle grants** (a package edit, from B) + retire B's
   root-admin interim for the pkg tab. E2e: a `consoleOperator` (ordinary actor, seeded **post**-boot)
   runs `InstallPackage` through the Gateway under capability mode → **allowed**; the same actor on
   `CreateMetaVertex` (not granted) → **denied**; on `urgent`/`system` lane → **`LaneUnauthorized`**.

Sequenced after B (which ships `consoleOperator` + the interim). Each fire independently green.

---

## 8. Self-adversarial pass (security plane — run, folded in)

- **Package privilege-escalation (the primary threat).** Without the allowlist, any package could grant a
  role a `meta`/`urgent`/`system` op → escalation. The **core allowlist** is the gate: a privileged per-op
  lane from `cap.roles` is honored **only if core-sanctioned**, else stripped to `default` + a loud Health
  issue. Default-closed: an unrecognized `{op,privileged-lane}` never authorizes. Assert a rogue package
  granting `TombstoneMetaVertex` at `meta` to a low role → **stripped, denied, alerted**.
- **Lane isn't a capability (why doc-level breadth in C2, and per-op in C1, are both safe on the op
  axis).** An op commits only if **granted**; the lane is a secondary gate + consumer routing (§2). So the
  worst a mis-scoped lane does is route a *granted* op to a different consumer — never grant an *ungranted*
  op. The allowlist still bounds *which* ops may ride a privileged lane at all.
- **Fail-closed default.** Absent `lanes` ⇒ `["default"]` (not "all") — omission narrows, never widens.
  A privileged lane not on the allowlist ⇒ stripped, not honored. Mirrors §6.8's deny-on-absence.
- **Boot-snapshot: fixed, not inherited.** C1's `consoleOperator` is ordinary (`cap.roles`, no
  `SystemActorKeys`), so the snapshot seam that plagues the anchor doesn't apply — a post-boot operator
  authorizes immediately. (C2 would have inherited it — a concrete C1-over-C2 win.)
- **Revocation.** A `consoleOperator`'s grant is a normal `cap.roles` projection: tombstone the role/grant
  → the lens drops it → the per-op lane vanishes with it (the standard soft-tombstone + `projectionSeq`
  guard, no new resurrection window).

No default-open, no package-escalation path, no snapshot seam. A `bmad-party-mode` pass is the pre-build
gate (security-plane), run as the Designer-lane obligation before build-ready.

---

## 9. Test strategy

- **Unit (`internal/processor`, rbac lens):** per-op lane gate (allowlisted `meta` grant → allow on
  `meta`, deny on `urgent`); non-allowlisted privileged grant → stripped + `PrivilegedLaneGrantRejected`;
  absent `lanes` → default-only; anchor/root doc → unchanged (all 4 lanes); the merge preserves per-op
  lanes.
- **E2e (`up-full-capability`):** the §7.3 triad — post-boot `consoleOperator` allowed `InstallPackage`
  (proves no snapshot dependency), denied a non-granted meta op, `LaneUnauthorized` on urgent/system.
- **Conformance:** the §6.4 contract-conformance test extended for the `lanes` field + the allowlist rule.

---

## 10. Risks + alternatives

- **Rejected — C2 (curated intermediate anchor).** Reuses the anchor but inherits boot-snapshot staleness
  and makes each new tier a bootstrap reseat. §4.
- **Rejected — no allowlist (packages freely grant privileged lanes).** Straightforward escalation surface;
  the allowlist is the non-negotiable safety gate. §8.
- **Rejected — Option A (operators = full root).** The original finding; C exists to avoid it.
- **Residual — the allowlist is a Processor constant (v1).** Adding a new privilege-grantable op is a small
  core edit. Acceptable and *intended* — it's precisely the core-owned policy the invariant wants. A
  data-driven allowlist (a core-seeded config) is a later refinement if the set churns; v1's set (pkg
  lifecycle) is stable.

---

## 11. Companion updates in this fire

- `loupe-operator-auth-lift-design.md` — §4/banner already record "B then C"; this doc is C's mechanism.
- `_bmad-output/planning-artifacts/backlog/lattice.md` — the operator-root finding row updated to point
  here as the ratified-direction fix (C1), sequenced after B.
- **Contract #6 §6.4 edit is *specified* in §5**, not dangled uncommitted (multi-fire shared-tree hazard);
  staged uncommitted when C's build is next, or on request.
