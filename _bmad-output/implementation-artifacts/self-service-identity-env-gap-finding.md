# Self-service identity 403 — env gap, not a code gap (finding, 2026-07-10)

**Repro (live, this dev stack):** staff `CreateUnclaimedIdentity` → mint a dev token for the new
identity's own key → `CreateLeaseApplication{authContext.target: <new identity>}` → `403 AuthDenied,
no matching platformPermission, rolesCarryingPermission: [operator, consumer]`.

**The board's originally-filed fix is wrong.** "Call `ClaimIdentity` right after `CreateUnclaimedIdentity`"
would still 403: `ClaimIdentity`'s `scope=self` gate is a two-stage Processor check
(`internal/processor/step3_auth_capability.go:484-519`) — an **existence gate** (actor must already hold
*some* role granting `ClaimIdentity`) runs before the self-match check. A bare unclaimed identity holds no
role at all, so it can't pass the existence gate to claim itself — the exact chicken-and-egg
`gateway-claim-flow-identity-provisioning-design.md` §2.2-§2.3 already documents. That design's §11.1a is
explicit that `CreateUnclaimedIdentity`→`ClaimIdentity` are **not** meant to chain in one session/actor;
they're two different actors separated by an out-of-band secret handoff.

**The real mechanism already exists, built and committed:**
- `ProvisionConsumerIdentity` op (`packages/identity-domain/ddls.go`) — idempotent, auto-grants `consumer`
  to any never-before-seen actor.
- The Gateway pre-flight (`internal/gateway/gateway.go:372,419-477`, `provisionActorIfNeeded`) — runs before
  every op, submits `ProvisionConsumerIdentity` under the Gateway's own system identity for any actor not
  yet in its provisioned cache.
- Landed across `7326774` (2026-07-06, Increment 2), `60f5fca` (2026-07-06, R1/R2), `d439919` (2026-07-09
  23:01, fixed the pre-flight's `actorID` read from required `Reads` — which faults `HydrationMiss` on a
  brand-new actor — to `OptionalReads`).

Because `loftspace-app`'s "new applicant" flow mints its dev-token `sub` = the new identity's **own** key
(`app.js:217`, `bareId(state.applicant)`), this identity's first `CreateLeaseApplication` call *is* its
first-ever Gateway touch — exactly the case `ProvisionConsumerIdentity` exists to auto-grant `consumer`
for. **No FE/package code change is needed at all** once the mechanism is actually live on this stack.

**Why it's still 403ing here — two environment gaps, both confirmed by grounding, not guessed:**
1. `bin/gateway` (built 2026-07-09 22:08) predates `d439919` (23:01) — the running binary still declares
   the pre-flight's `actorID` read as a hard `Reads` entry, which faults on every first-touch actor.
   (Rebuilt in-place during this investigation; not yet redeployed — see below.)
2. `make provision-gateway-identity-provisioner` (Makefile) — the one-time, documented ops step that
   grants the Gateway's own system identity the `identityProvisioner` role — has never been run against
   this stack's bootstrap identity. Without it, the Gateway's own `ProvisionConsumerIdentity` submission is
   itself denied (tolerated as a best-effort no-op, per the Makefile's own comment at that target), so the
   symptom is silent: the consumer grant just never appears for anyone, on any vertical.

**Why this fire didn't apply the fix.** Both remaining steps write to shared bootstrap/identity state used
by the concurrent Lattice-stream fire and Andrew's own session (`AssignRole` against the Gateway's system
identity, plus a restart of the shared Gateway process) — outside the Vertical Steward's remit
(`packages/<vertical>*` + `cmd/<x>-app`) and not something to run unattended without explicit authorization.
Flagged via a spawn_task chip instead of applied directly.

**Fix, when authorized (two commands, no code changes beyond the already-committed source):**
```
make provision-gateway-identity-provisioner   # one-time, idempotent
pkill -f "bin/gateway"; go build -o bin/gateway ./cmd/gateway; \
  NATS_URL=... NATS_NKEY=... GATEWAY_DEV_MODE=true ... ./bin/gateway >gateway.log 2>&1 &
```
Then re-run this fire's repro (fresh `CreateUnclaimedIdentity` → `CreateLeaseApplication` as that identity)
to confirm it now commits instead of 403ing.
