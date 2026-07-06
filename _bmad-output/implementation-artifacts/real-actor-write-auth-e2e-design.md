# Real-actor write-auth end-to-end — proving scoped capability auth through the Gateway (design)

**Status:** 📐 **awaiting-Andrew (ratification)** · Designer fire (Winston, 2026-07-06) · Lattice + Verticals
lanes (Security & trust boundary / auth-plane hardening)

> Andrew steered this live on 2026-07-06 and pre-decided its two forks (recorded in "For Andrew"). This
> doc writes up the shape for his ratification and **un-shelves**
> `gateway-claim-flow-identity-provisioning-design.md` — the driver it was waiting for is *this*.

---

## For Andrew

**What it does (two lines).** Makes the vertical apps submit writes as **real, role-scoped users** through
the Gateway (instead of stamping `bootstrap.BootstrapIdentityKey` root on every op), fed by a **shared dev
"Fake IdP,"** so the platform can finally run — and *prove* — its own **capability write-auth** end to end
with genuine allow **and** deny outcomes. It retires the stub as a *load-bearing* dev crutch by giving the
app tier a real actor to be.

**The two forks you decided (2026-07-06), recorded:**
- **Topology = browser-direct.** Browser gets a token from the Fake IdP → calls the Gateway
  `POST /v1/operations` directly for writes; the app serves reads + UI. The prod SPA/agent end-state.
- **Scope = prove-it-first, keep stub default.** Add a `make up-full-capability` proving lane + a real
  staff-and-consumer e2e; keep `make up-full` on stub until that is green. Retiring stub as the *default*
  (every `up-full` = capability) is Phase 3, your call once Phase 1 is green.

**What's still yours to ratify:** the shape below — chiefly the **Fake IdP as one shared dev component**
(§3.2, my recommendation over reusing each surface's in-process key) and the **two-phase decomposition**
(§4: the proving lane now, the walk-in credential-binding after). Everything resolvable I've resolved.

**Frozen-contract change: none.** Verified in §6 — this is wiring + a dev-only IdP + package-level grants,
none of which touches a frozen contract. (The claim-flow build it un-shelves is likewise contract-free.)

---

## 1. Problem & intent — the untested surface

**The gap, precisely.** The capability *mechanism* is well-tested: `step3_auth_capability` has thorough
unit coverage, the **four engine paths** (Weaver/Loom/objmgr/privacy) already run **stub-off** end-to-end
(`system-actor-package-op-grants-design.md` Fire 2, shipped `80daa9b`), plus the `internal/bypass` residual
and the Gate-3 vectors. What has **never** run integrated under capability mode is a **non-root,
role-scoped actor** — a real `frontOfHouse` or `consumer` submitting an app op and being allowed/denied by
their **projected `cap.roles` doc**.

**Why flipping the auth mode alone would not test it.** Both apps stamp `s.adminActor`
(`= bootstrap.BootstrapIdentityKey`, root-equivalent) on every write (`cmd/loftspace-app/op.go:129`,
`cmd/clinic-app/op.go`). Root sails through the authorizer trivially, so even
`make up-full LATTICE_PROCESSOR_AUTH_MODE=capability` would exercise *root* auth, not *scoped* auth. To
actually test scoped write-auth you need the apps to submit as **real non-root users** — which needs (a) a
JWT source, (b) the Gateway stamping the verified actor, (c) real identities carrying real role grants.
That bundle is exactly this design; each piece is load-bearing, not optional.

**Intent.** Close the one integrated hole in the auth plane with the smallest honest proof: one real staff
user and one real consumer drive a vertical **through the Gateway under capability mode**, with a genuine
allow (staff does a staff op) and a genuine **deny** (consumer is refused a staff op) — real projected
grants, real Gateway stamping, real Processor step-3. The stub stops being the thing that hides whether any
of this works.

---

## 2. Grounding — what already exists (most of it)

This is far less greenfield than "a shelved design" implied. Shipped and reusable:

- **The Gateway is already in the dev stack.** `make up-full` starts it in dev-mode on `:8080`
  (`11cc15f`, 2026-07-05); the write path (`POST /v1/operations`, strip-and-stamp), JWKS live rotation,
  and the RLS read-front (Fire 3) are all shipped. `GATEWAY_DEV_MODE=true` trusts the checked-in dev key
  (`deploy/gateway-dev-key/`, kid `dev`); `gateway dev-token -sub <id>` mints a token for it
  (`cmd/gateway/main.go:418`).
- **The apps already do per-user READ auth.** `cmd/loftspace-app/readauth.go` verifies a per-user Bearer
  JWT through the **shared** `internal/gateway/auth` seam, mints per-user demo tokens **in-process**
  (`POST /api/dev-token {subject}`, `POST /api/staff/dev-token`), and scopes Postgres RLS off the verified
  subject (`set_config('lattice.actor_id', …)`). **A Fake IdP already exists, embedded in the app** — for
  reads. The gap is that **writes** (`handleOp`) still stamp root. Reads are per-user; writes are root;
  that asymmetry *is* the untested surface.
- **The claim-flow design provides the consumer identity.**
  `gateway-claim-flow-identity-provisioning-design.md` (Andrew-ratified 2026-07-06) specifies
  `ProvisionConsumerIdentity` — the Gateway auto-creates a bare identity + `holdsRole(→consumer)` on an
  actor's first authenticated touch. That is exactly how a real consumer comes to exist with a real role
  grant. **This design is that design's driver** (§9 of it asked for exactly this).
- **The projection chain is intact.** `packages/rbac-domain/lenses.go` projects `cap.roles.<actor>` from
  `(identity)-[:holdsRole]->(role)<-[:grantedBy]-(perm)`; a real role grant produces a real capability doc
  the Processor reads at step-3. Nothing new is needed on the producer side.
- **The precedent this mirrors.** `system-actor-package-op-grants-design.md` §8 Fire 2 *already teed up
  this exact decision*: "either flip `up-full` to capability mode or add a `make up-full-capability`
  target so the platform's own orchestration is routinely exercised under real auth (retiring the stub as
  a *load-bearing* dev crutch)." This design executes that for the **app tier** (Fire 2 did it for the
  engine tier).

---

## 3. The shape (browser-direct, ratified)

### 3.1 Topology

```
                    ┌─────────────── Fake IdP (dev) ───────────────┐
                    │  login-as <seeded user> → RS256 JWT (kid dev) │
                    │  JWKS endpoint  ◀── trusted by BOTH below      │
                    └───────────────────────────────────────────────┘
                              │ token (one, good for reads AND writes)
        browser (app FE) ─────┤
                              ├── WRITE ─▶ Gateway  POST /v1/operations   (verify JWT → stamp verified actor → publish)
                              └── READ  ─▶ app       GET /api/…            (readauth.go verify JWT → set RLS actor)
```

The browser holds the user's token and presents it to **two** backends: the Gateway for writes, the app
for reads + UI. The app **stops** being a write proxy — `POST /api/op` (adminActor-root submit) is no
longer the FE's write path. (Whether to *delete* `/api/op` or leave it gated is a Phase-3 enforcement
detail, §4; for the proving lane the FE simply stops calling it.)

### 3.2 The Fake IdP — one shared dev component (recommended)

Today two in-process ephemeral keys exist (the Gateway's checked-in `dev` key; loftspace-app's per-boot
`loftspace-dev` key) with **different** kids/issuers — a token minted for one is not trusted by the other.
Browser-direct makes that untenable: **one** user token must satisfy **both** the Gateway (writes) and the
app (reads). So consolidate into one small dev IdP:

- A tiny binary/service (`cmd/dev-idp`, or a mode of the existing dev tooling) that serves a **JWKS
  endpoint** and a **mint/login endpoint** (`POST /login {subjectOrRole}` → RS256 token, kid `dev`),
  signing with `deploy/gateway-dev-key` (already checked in, already DEV-ONLY-marked).
- The Gateway trusts it via `GATEWAY_JWKS_URL` (dev-mode allows the plaintext-http JWKS URL — already
  supported, `cmd/gateway/main.go:281`). The apps trust the same JWKS (extend `readauth.go` to accept a
  JWKS URL alongside its existing public-key/dev-auth postures — a small additive posture).
- **Fail-closed profile gate, mirrored from what exists.** The Fake IdP runs **only** in dev-mode and
  binds loopback only — the exact discipline `readauth.go:95` already enforces (dev-auth refused on a
  non-loopback bind) and `cmd/gateway/main.go` enforces (dev key never loads without `GATEWAY_DEV_MODE`).
  In prod the deployment's real OIDC IdP fills the same slot (the ratified Gateway F3), unchanged.

*Cheaper interim if you'd rather not build `cmd/dev-idp` first:* the browser can obtain its write token
from `gateway dev-token`-style minting and its read token from the app's existing `/api/dev-token`, both
keyed to a **single shared dev key** (point loftspace-app's read verifier at the same `deploy/gateway-dev-key`
public half instead of a per-boot ephemeral one). That unifies trust without a new component and can be the
first fire; the shared `cmd/dev-idp` is the clean end-state. Either way the FE gets one token that works
both places.

### 3.3 The identities the proof needs

- **Staff S** — a real identity holding `frontOfHouse` (or `backOfHouse`). Staff are **not**
  auto-provisioned (that's consumer-only). Dev-seed them: a `make` seed step creates the identity and an
  **operator** assigns the role (`AssignRole`, the shipped rbac path) — or the Fake IdP's "login as staff"
  maps to a pre-seeded staff subject. One seeded staff identity per role under test.
- **Consumer C** — comes into being through **`ProvisionConsumerIdentity`**: C's first authenticated hit
  on the Gateway auto-creates C's identity + `holdsRole(→consumer)` (the claim-flow design's Gateway
  pre-flight). So the proving lane **exercises the claim-flow provisioning for real** — the design isn't
  just un-shelved, it's on the critical path of the proof.

### 3.4 What the vertical must grant (the real allow + deny)

Today every clinic/loftspace op is `operator`-only (`permissions.go`) — so a consumer can do *nothing*,
which gives a deny but no scoped allow. The proof needs **≥1 op granted to `consumer`** so there is a real
scoped **allow**, alongside a staff-only op the consumer is denied. Concretely (loftspace):
`CreateLeaseApplication → consumer` (an applicant applies to a unit — the natural consumer action, with the
Starlark script enforcing applicant-self semantics), while `SetListingStatus`/`AssignUnitOwner` stay staff.
Then:
- Staff S → `SetListingStatus` → **allowed** (staff grant). ✅
- Consumer C → `SetListingStatus` → **`AuthDenied`** (consumer lacks it). ✅ the real deny.
- Consumer C → `CreateLeaseApplication` → **allowed** (new consumer grant). ✅ the real scoped allow.

All three through the real Gateway, under capability mode, against real projected `cap.roles` docs. That is
the honest proof the stub has been hiding.

---

## 4. Decomposition — two phases, cross-lane

### Phase 1 — the proving lane (the honest proof). Keep stub as the default.

**Lattice lane:**
1. **Shared dev-IdP trust** (§3.2) — one dev key both the Gateway and the apps trust (interim: point the
   app read verifier at `deploy/gateway-dev-key`; end-state: `cmd/dev-idp` JWKS both consume).
2. **`ProvisionConsumerIdentity` + `identityProvisioner` + Gateway system identity + Gateway pre-flight**
   — build the claim-flow design's §3 (the parts needed for a consumer to exist and act; the credential
   *binding* R1/R2 is Phase 2). This is the un-shelved design's Increment 1, minus the walk-in binding.
3. **`make up-full-capability`** — an `up-full` variant running the Processor with
   `LATTICE_AUTH_MODE=capability` and dev-seeding a staff identity (§3.3). Stub `up-full` unchanged.
4. **The e2e** (`internal/…` or a `test-real-actor-auth` target) — the §3.4 allow/deny/scoped-allow triad
   through the real Gateway under capability mode.

**Verticals lane:**
5. **Browser-direct FE** — the app FE sends writes to the Gateway (`:8080/v1/operations`) with the user's
   Bearer token instead of `POST /api/op`; keeps using the app for reads. (loftspace first; clinic mirrors.)
6. **Consumer-scope grant** — grant ≥1 op to `consumer` in the vertical's `permissions.go` + the
   applicant-self Starlark guard (§3.4).

**Phase-1 exit:** a real consumer applies to a unit through the Gateway and is *allowed*; is *denied* a
staff op; a real staff user is *allowed* the staff op — all under capability mode, all green in CI.

### Phase 2 — the walk-in credential binding (the richer proof)

Builds the rest of the claim-flow design (§11 R1–R4): `CreateUnclaimedIdentity` by staff → out-of-band
claim link → self-signup → `ClaimIdentity` → the person acts **as U**. This is where the **browser-direct
consequence** (§5) lands: the credential→identity (A→U) resolution must be a **shared seam**, because reads
go to the app and writes go to the Gateway — neither can own it privately. Sequenced after Phase 1 because
Phase 1's proof (C acts as C, Scenario-B style) needs no binding.

### Phase 3 — retire stub as the *default* (your Q2 Option B, later)

Flip `make up-full` itself to capability; make stub opt-in. Pairs with the **#75 Fire 2b enforcement**
(strip the apps' direct `core-operations` publish grant so they *cannot* bypass the Gateway — until then
Phase 1 proves the path works, but a compromised app could still go direct; enforcement is what makes
"through the Gateway" mandatory, not voluntary). Explicitly **not** in this design's build — flagged so the
sequence is visible.

---

## 5. The browser-direct consequence: the resolution seam must be shared

A design note worth surfacing because it falls directly out of your topology choice. In the claim-flow
design §11 I put the credential→identity (A→U) resolution **at the Gateway** — fine when the Gateway is the
only external surface. But browser-direct sends **reads to the app**, not the Gateway. So the app's read
boundary *also* needs to resolve A→U to set `lattice.actor_id = U`. Therefore the resolution cannot be
Gateway-private: it must be a **shared lookup** both the Gateway (write stamp) and the app read boundaries
consult — a small library over a `credential-bindings` projection/materializer both processes can read
(they already share `internal/gateway/auth`). This is a **Phase 2** concern (Phase 1's consumer acts as
itself, no binding), but it's a real amendment to the claim-flow design's "resolve at the Gateway" framing,
folded there as a note. It does not change any decision — it relocates one step from "Gateway-only" to
"shared seam."

---

## 6. Contract surface — none

| Contract | Why untouched |
|---|---|
| #2 Operation Envelope | The Gateway already stamps the reserved `actor` field; no new field. |
| #6 Capability KV | Real `cap.roles` docs from real grants — the shipped producer; no doc-shape change. A vertical granting an op to `consumer` is ordinary package-level `permissions.go` work. |
| #7 Bootstrap | The Gateway system identity is a new seeded actor — §7.2 explicitly anticipates more ("seeded by their respective stream's bootstrap procedures… following the same pattern"). Build-to. |
| #9 Identity Claim Flow | Untouched — Phase 2 uses the shipped claim mechanics unchanged. |

The Fake IdP is dev-only tooling (no contract), the FE change is app code, the proving lane is a Makefile
target + a test. Nothing frozen moves.

---

## 7. Reconciliation with the existing mental model

- **Didn't the read path already solve per-user auth?** Yes — `readauth.go` is the read half, shipped and
  working, including an in-process Fake IdP. This design builds the **write** half and **consolidates** the
  two dev keys into one so a single token serves both (§3.2). It extends the established pattern; it does
  not invent a parallel one.
- **Didn't system-actor Fire 2 already retire the stub?** For the **engine tier** (4 root-equivalent
  system-actor paths), yes. This is the **app tier** with **non-root, role-scoped** actors — the case Fire
  2 explicitly left as the next step. No overlap, direct continuation.
- **Does this introduce new state?** Minimally: the Fake IdP is ephemeral dev tooling; the consumer
  identities are created by the shipped-design's `ProvisionConsumerIdentity`; the staff identity is a
  dev-seed. No new lens, bucket, or contract concept.
- **Does it contradict a design-of-record?** No — it *executes* two already-ratified designs (the Gateway
  trust boundary; the claim-flow provisioning) against a driver they were waiting for, and follows the
  Fire-2 precedent for retiring the stub.

---

## 8. Self-adversarial pass (security plane — run, folded in)

- **The Fake IdP must never load in prod.** Profile-gated + loopback-only, mirroring the two guards that
  already exist (`readauth.go:95` refuses dev-auth off-loopback; `cmd/gateway/main.go` never loads the dev
  key without `GATEWAY_DEV_MODE`). A prod deployment points the Gateway/app at the real OIDC IdP; the Fake
  IdP binary is dev-tooling that is simply not deployed. Assert the loopback refusal in a test.
- **The deny must be REAL, and asserted.** The failure mode of an auth-proof is a green run where
  *everything is allowed* (e.g. the mode didn't actually flip, or the actor resolved to root). The e2e must
  assert a **negative**: consumer C is **`AuthDenied`** a staff op — and that the denial is the *scoped*
  one (`AuthDenied` from the missing grant, not `AuthInfrastructureFailure` from a misconfig). A test that
  can't fail closed proves nothing.
- **Provisioning stays fail-closed.** The Gateway auto-provisions **consumer** only (bare identity +
  `consumer`); staff roles come from an **operator** `AssignRole`, never from the Gateway. No path grants a
  privileged role to a first-touch actor. (This is the claim-flow design's §4 narrow-role decision — the
  Gateway is internet-facing, so it gets `identityProvisioner`, not `operator`.)
- **Phase 1 proves the path; it does not enforce it.** Until #75 Fire 2b strips the apps' direct
  `core-operations` publish, a compromised app could still bypass the Gateway and stamp root. Phase 1's
  claim is "capability write-auth works end-to-end through the Gateway," **not** "the app can no longer
  bypass it." Stated plainly so the proof isn't over-read as enforcement; enforcement is Phase 3.
- **Token audience/issuer split.** The app read verifier today expects issuer `loftspace-app-dev` /
  audience `lattice-read`; the Gateway expects its own. Consolidating on one Fake IdP means one
  issuer/audience both accept — set them deliberately (the shared dev IdP's `iss`/`aud`) so a token minted
  for the user validates at both surfaces; don't leave two half-overlapping expectations.

No default-open, no privileged-role-on-first-touch, no green-means-nothing test. The adversarial pass is
**run and clean**; a full `bmad-party-mode` pass is the pre-build gate for the Phase-1 fire (§10), given
this is security-plane.

---

## 9. Test strategy

- **Unit:** the Fake IdP mints a token both verifiers accept (shared kid/iss/aud); the loopback refusal
  fires off-host; `ProvisionConsumerIdentity` unit cases (per the claim-flow design §7).
- **E2E (`up-full-capability`, the core proof):** the §3.4 triad — staff allow, consumer deny (assert the
  scoped `AuthDenied`), consumer scoped-allow — driven through the **real Gateway** against the **real
  Processor** under `LATTICE_AUTH_MODE=capability`, with **real projected `cap.roles`** docs. Plus: a
  first-touch consumer auto-provisions (proves the claim-flow pre-flight), and a second request from the
  same consumer reuses the identity (idempotent).
- **Regression:** stub `up-full` unchanged (assert the default path is byte-identical); the apps' existing
  read boundary unaffected.

---

## 10. Risks + alternatives considered

- **Rejected — apps stamp real per-user actors *directly* (no Gateway).** Cheaper (skip the FE change), and
  it would test the *authorizer's* allow/deny. But it does **not** test the trust boundary: the app would
  still be trusted to assert any actor, which is the exact impersonation hole the Gateway closes. You chose
  browser-direct precisely to avoid this half-measure. Rejected.
- **Rejected — reuse each surface's in-process ephemeral key (no shared Fake IdP).** Browser-direct needs
  **one** token good at both the Gateway and the app; two different kids/issuers can't both accept one
  token. Consolidation is not optional under browser-direct. (The §3.2 interim — one shared *key* without a
  new component — is the minimal form; `cmd/dev-idp` is the clean end-state.)
- **Rejected — retire stub as the default now (your Q2 Option B).** You chose prove-it-first: a big-bang
  cutover before the path is green taxes every dev flow and risks a red stack no one can bisect. Sequenced
  as Phase 3, after Phase 1 is green.
- **Residual — the `sub` → NanoID mapping** (claim-flow §11.4/R3). In dev the Fake IdP mints tokens whose
  `sub` is already the bare NanoID of a seeded/provisioned identity, so the mapping is a no-op for the
  proving lane. It becomes real only when a production IdP (opaque subjects) is wired — tracked in the
  claim-flow design, not this fire.

---

## 11. Companion updates in this fire

- `gateway-claim-flow-identity-provisioning-design.md` — **un-shelved**: status/§9 verdict updated (Andrew
  ratified 2026-07-06 and supplied the driver; build sequenced within this initiative's Phase 1/2, no
  longer "shelve until a product driver files"); §11's "resolve at the Gateway" gains the shared-seam note
  (§5 here).
- `_bmad-output/planning-artifacts/backlog/lattice.md` — the claim-flow row → ✅ ratified; a new row for
  this initiative (📐 awaiting-Andrew → ratify live); cross-refs the Verticals-lane items.
- (Verticals-lane rows — the browser-direct FE + consumer-scope grant — filed on `verticals.md` as the
  paired lane work, since those are app-tier changes.)

---

## 12. Fire-by-fire summary (for the Stewards, once ratified)

**Lattice lane (Phase 1):** (1) shared dev-IdP trust; (2) `ProvisionConsumerIdentity` + `identityProvisioner`
+ Gateway system-identity + pre-flight (claim-flow §3, walk-in binding deferred); (3) `make up-full-capability`
+ staff dev-seed; (4) the real-actor auth e2e. **Verticals lane (Phase 1):** (5) browser-direct write FE
(loftspace, then clinic); (6) a `consumer`-scope op grant + applicant-self guard. **Phase 2 (later):** the
claim-flow walk-in binding + the shared A→U resolution seam. **Phase 3 (your call):** stub retired as the
`up-full` default + #75 Fire 2b app-bypass enforcement.

Each Phase-1 fire is independently shippable + green; the e2e (fire 4) is the one that must land last (it
depends on 1–3 and the Verticals grant). Pre-build gate: a `bmad-party-mode` pass on the Phase-1 fire set
(security-plane), run as the Designer-lane obligation before build-ready.
