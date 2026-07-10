# External actor authN — IdP subject binding (R3) + Contract #11 un-deferred (design)

**Status:** ✅ **Built (2026-07-10).** Ratified same-day; the §9 fire (both increments) shipped in one
commit (`9812231`) — Lattice lane (Security & trust boundary) · Directed by Andrew's 2026-07-10
questions on `gateway.md` + the gateway design docs.

> **Ratified (Andrew, 2026-07-10).** Fork = **A** as recommended (binding is a property of the trust
> source: configured external sources always `opaque` with a mandatory per-source issuer; the DEV-ONLY
> key pinned `nanoid` in code, never operator-selectable). **Contract #11 committed with this
> ratification** (`docs/contracts/11-external-actor-authn.md` + the README index row). Build-ready for
> the Lattice Steward: the one S–M fire of §9.

---

## For Andrew

**What it does (two lines).** Answers "how does a real 3rd-party IdP return a JWT whose `sub` is a
Lattice identity NanoID?" — **it doesn't, and never needs to**: the verifier derives the credential
identity deterministically from the token's `(iss, sub)` pair, so the IdP stays completely ignorant of
Lattice. And it **un-defers Contract #11**: the external actor-authN token profile + subject binding is
now frozen as `docs/contracts/11-external-actor-authn.md`, **staged uncommitted in `main`** for your
ratification.

**The direct answer to your question.** Your recollection is right about the code: today
`internal/gateway/auth/auth.go:274` builds `ActorID = "vtx.identity." + sub` verbatim, so the JWT `sub`
must literally *be* the NanoID — which works only because every current token is dev-minted
(`gateway dev-token -sub <identityNanoID>`, the app dev-token endpoints, the planned Fake IdP). No
external IdP can do that, and **no mapping is pushed to the IdP to fix it**. The ratified direction
already exists in sketch form — `gateway-claim-flow-identity-provisioning-design.md` §11.4 (refinement
R3, ratified 2026-07-06 with the rest of that design, sequenced Phase 2 and **not yet built**): derive
`A = vtx.identity.<SHA256NanoID(iss|sub)>` at the verifier. Two mappings exist and **both live inside
Lattice**:

1. **IdP account → credential identity (A):** a pure function of the verified token's `(iss, sub)` —
   no enrollment state, no IdP configuration, restart-safe (this design, realizing R3).
2. **Credential identity (A) → business identity (U):** the Contract #9 claim flow +
   `credential-bindings` resolution seam — **already shipped** (`internal/gateway/credentialbinding`,
   wired into the Gateway write stamp and both vertical read boundaries, `8846771`).

This design turns the §11.4 sketch into the complete mechanism (subject modes per trust source, exact
derivation, fail-closed validation, dev interplay, provenance) and stages the contract that freezes it.

**Architectural fork — designed through, my recommendation; the call is yours:**

| Fork | Options | My recommendation |
|---|---|---|
| **Subject-binding model** | **(A)** Binding is a property of the **trust source**, per `kid`: a **configured external source** (static PEM dir / JWKS) is always **`opaque`** (sub is IdP-native → derived) and MUST declare its expected issuer; the **in-code DEV-ONLY key** is pinned **`nanoid`** (sub IS the NanoID — validated). Not an operator-per-token knob. **(B)** Uniform derivation + a custom JWT claim (`lattice_sub`) that overrides when present. **(C)** Shape-sniffing: a NanoID-shaped `sub` passes through, else derive. | **A.** Binding follows the issuer's subject space, deterministically, with zero token-content magic; dev flows keep working (the dev key mints NanoID subs). **C is rejected hard** — default-open: any trusted IdP could mint a NanoID-shaped `sub` and impersonate *any* identity (root included); shape must never select trust grade. **B is rejected as the default** — token *content* would switch binding semantics (one issuer binding two ways), and its override claim IS C's arbitrary-identity power; kept absorbable later as an explicit third `claim` mode if a deployment enrolls its IdP. **Two refinements from the adversarial pass (§10), folded into A:** (1) the `opaque` issuer pin is a **MUST, per source** — without it a trusted IdP can replay a *peer* IdP's `sub` and cross-derive a peer user (the confinement guarantee is real only under per-source issuer binding); (2) `nanoid` is the in-code dev pin **only**, never operator-env-selectable — the "grant a real IdP nanoid semantics" knob had zero legitimate use and was an impersonation footgun, so it is removed structurally. |

**Frozen-contract change: Contract #11, staged uncommitted.** `docs/contracts/11-external-actor-authn.md`
(new) + the `docs/contracts/README.md` index row — both left uncommitted in `main` per the house rule;
the diff is the proposal. **Why un-defer is grounded, not just directed:** the 2026-06-29 defer condition
("freeze when a **second** JWT consumer needs it — the Edge node, or control-plane authz") is **met and
exceeded** — the verifier seam is consumed today by **eight verify call-sites across nine binaries**
(`cmd/gateway`'s write *and* read fronts, all four vertical apps' read boundaries —
`loftspace`/`clinic`/`wellness`/`cafe` — `cmd/loupe`, and the one `internal/controlauth` seam that
fronts three control-plane binaries, Weaver/Loom/Refractor), and the ratified browser-direct topology
makes the token profile a **public interface** the vertical FEs mint against. That many surfaces
agreeing on one binding rule is precisely what a frozen contract is for — and the binding rule is about
to get
subtler (modes + derivation), which is the moment to freeze it, not after.

---

## 1. Problem & intent

**The gap.** The platform's external actor identity rests on the JWT `sub` claim, and the shipped
binding is raw passthrough: `ActorID = IdentityKeyPrefix + sub` (`auth.go:274`). Every current token
source mints `sub` = a seeded identity's bare NanoID, so this works — in dev. A production IdP
(Auth0 `auth0|507f…`, Google `110169484474386276334`, Keycloak UUIDs) mints **opaque, IdP-native
subjects**. Under passthrough those produce non-NanoID identity keys (violating Contract #1 §1.1's
key shape — `substrate.IsValidNanoID` requires 20 chars over the canonical 58-char alphabet), and no
validation rejects them: the malformed key flows into capability lookups, RLS `set_config`, revocation
keys, and — post-provisioning-build — `ProvisionConsumerIdentity`, which would fail it *late*
(`InvalidArgument` at step-6) rather than at the trust boundary. The claim-flow design flagged exactly
this as a known residual (§8: *"worth its own small hardening item when a production IdP integration is
actually being wired up"*) and sketched the fix (§11.4) without fully designing it. This is that item.

**The intent.** One binding rule, applied at the **single** construction point every surface already
shares (`auth.Verifier.Verify`), such that: a real IdP integrates with **zero Lattice knowledge**
(standard OIDC — Lattice consumes `(iss, sub)` as opaque input); dev flows keep their
act-as-a-seeded-identity ergonomics; a malformed subject is rejected at the boundary (fail-closed); and
the rule is **frozen** (Contract #11) so the eight-and-growing verifying surfaces cannot drift.

**Also in scope: un-deferring Contract #11.** `gateway-external-trust-boundary-design.md` §5 deferred
a *"Contract #11 — Gateway / actor-authN (JWT format)"* freezing `{sub, exp, iss, aud, kid, jti}` +
the `vtx.identity.<sub>` binding, trigger: a second consumer. Andrew directed the un-defer (2026-07-10);
§0 above shows the trigger was independently already met. Note the deferred title is now too narrow on
both axes — the contract is not Gateway-only (every external-authN surface) and not format-only (the *binding* is the
load-bearing half) — hence the staged name: **External Actor Authentication (JWT profile & subject
binding)**.

---

## 2. Grounding — what exists, precisely

### 2.1 The one construction point and its consumers

`internal/gateway/auth` is the **only** place a token becomes an actor. `Verifier.Verify` (auth.go)
enforces: asymmetric-only algs (`RS256/384/512, ES256/384/512`; HS*/`none` structurally refused via
`jwt.WithValidMethods` + a keyfunc re-assert), `kid` **required** and resolved against the trusted set
(no single-key fallback), `exp` **required** with bounded skew (default 60s), `nbf`/`iat` enforced when
present, `iss`/`aud` enforced **when configured** (optional today), `sub` required non-empty →
`VerifiedActor{ActorID: "vtx.identity."+sub, Subject: sub, TokenID: jti, ExpiresAt}`. The
`Authenticator` composes the per-request revocation kill-switch keyed on `ActorID`. Consumers (all via
this one seam): the Gateway write stamp + RLS read front (`cmd/gateway`, `internal/gateway/read.go`),
the four vertical apps' read boundaries (`cmd/{loftspace,clinic,wellness,cafe}-app/readauth.go`),
Loupe's operator tier (`cmd/loupe/readauth.go`), and the control plane
(`internal/controlauth/wire_actor_verifier.go` — Weaver/Loom/Refractor, *"same JWT, same trust
model"*).

Three downstream dependents make `VerifiedActor.Subject`/`ActorID` load-bearing beyond the envelope
stamp: the **RLS actor var** (`set_config('lattice.actor_id', <Subject>)` — matched against bare-NanoID
grant rows, D1.3), the **revocation key** (`IsRevoked(ActorID)`), and the **credential-bindings
resolver key** (`Resolve(ActorID)`). Whatever the binding rule is, it must hold identically for all
three — which it does for free if (and only if) the binding happens inside `Verify`.

### 2.2 What shipped since the sketch (the R-ledger)

From `gateway-claim-flow-identity-provisioning-design.md` §11.5, as of 2026-07-10: **R1 shipped** —
the `internal/gateway/credentialbinding` resolver + the `credential-bindings` materializer, consulted by
the Gateway write path (with the `ClaimIdentity` raw-credential carve-out, `gateway.go:204-209`) and
both vertical read boundaries (`8846771`). **R2 shipped** — `ClaimIdentity` grants
`holdsRole → consumer` to the claimed identity atomically (`packages/identity-domain/ddls.go:617-635`).
**`ProvisionConsumerIdentity` shipped** (first-touch consumer provisioning under the Gateway's
`identityProvisioner` system identity). **R3 (this design) is the one unbuilt refinement** in the
credential plane; R4 (`RotateClaimKey`, lost-secret re-issue) is orthogonal and also open.

### 2.3 The derivation primitive already exists

`internal/substrate/derive.go` `SHA256NanoID(s)`: SHA-256 → PCG seed → rejection-sampled 20-char NanoID
over the canonical alphabet — **deterministic, byte-identical to the Starlark `crypto.sha256NanoID`
builtin**, already the codebase's idiom for exactly this job (the `credentialindex` key is
`sha256NanoID(actorKey)`; the identity dedup keys are `sha256NanoID("email:"+email)`). The derivation
this design specifies is one call to it. No new primitive, no new dependency.

### 2.4 Vendor grounding (primary sources, fetched 2026-07-10)

- **OIDC Core 1.0** (openid.net, the authoritative spec): `sub` is *"a locally unique and never
  reassigned identifier **within the Issuer** for the End-User"*, ≤255 case-sensitive ASCII chars. →
  **Uniqueness is scoped per-issuer**, which is why `iss` MUST be a derivation input (two IdPs may mint
  the same `sub` string for different humans). §8 defines **public** vs **pairwise** subject types:
  pairwise IdPs mint per-client/sector-distinct subs — see §8 risk below.
- **Google OIDC** (developers.google.com/identity/openid-connect, Andrew's named example): `sub` is
  *"unique among all Google Accounts and never reused"*; Google explicitly instructs *"use `sub`, never
  `email`, as the unique-identifier key"*. And a live gotcha: **Google's `iss` legitimately appears in
  two forms** — `https://accounts.google.com` *or* `accounts.google.com` — which would split one human
  into two derived identities if the deployment doesn't pin one form (§3.2 handles this).
- **golang-jwt v5.2.1** (`docs/vendors.md` row, pinned): the profile above is enforced via
  `WithValidMethods` + the v5 error tree — unchanged by this design.

### 2.5 The architecture's standing decisions honored

**F3 (ratified 2026-06-29): external IdP; Lattice never owns signing keys** — derivation keeps that
pure (Lattice consumes public keys + claims; it never asks the IdP to store, compute, or return
anything). The rejected F3-B (Lattice-owned IdP) stays rejected: a token-exchange service minting
Lattice-signed NanoID tokens would quietly re-create it. **P2/P5 untouched** — this design changes how
a string is computed inside the verifier; no new KV reads or writes anywhere.

---

## 3. The shape

### 3.1 The three-layer identity model (the consolidated answer)

```
IdP account            credential identity (A)              business identity (U)
(iss, sub)  ──derive──▶ vtx.identity.<SHA256NanoID(…)>  ──claim/bind──▶ vtx.identity.<staff-minted NanoID>
 lives at the IdP;       created on first touch by            created by CreateUnclaimedIdentity;
 opaque to Lattice       ProvisionConsumerIdentity            carries PII, links, grants
                         (holdsRole → consumer);              (Contract #9 claim → credentialBinding;
                         revocation targets A                  resolved per-request, SHIPPED R1)
```

Layer 2→3 is shipped. Layer 1→2 is this design. The IdP participates in **none** of it — it issues a
standard OIDC token; Lattice does both mappings internally. (Scenario B humans — self-signup-first,
no staff pre-creation — simply *are* their A; U and the claim never enter the picture. Both scenarios
in claim-flow §11.1/§11.2 work unchanged.)

### 3.2 Subject binding — a property of the trust source

Every trusted key enters the Verifier from a **key source** (the static `<kid>.pem` dir, the JWKS
poller, or the dev key) and now carries a per-`kid` **binding spec** `{mode, issuer}`, fixed at load
time. `Verify` resolves the spec of the kid that verified the signature and binds accordingly. A kid
with **no** declared spec is a **construction error** (`NewVerifier`/`SetKeysWithInfo` reject it) —
maximally fail-closed: a trusted key never binds by silent default (adversarial finding A2/MAJOR-2).
Which mode a source gets is **not** an operator per-token choice:

- **A configured external source (static dir / JWKS) is always `opaque`, and MUST declare an expected
  issuer.** The `sub` is an IdP-native identifier. `Verify` requires the token's `iss` to be non-empty
  **and equal to the verifying source's declared issuer** (`ErrIssuerMismatch`/`ErrMissingIssuer`),
  then:

  ```
  derivationInput = "idpsub:" + <decimal len(iss)> + ":" + iss + ":" + sub
  Subject         = substrate.SHA256NanoID(derivationInput)          // bare 20-char NanoID
  ActorID         = "vtx.identity." + Subject
  ```

  The length framing makes the input **injection-proof**: no `(iss', sub')` ≠ `(iss, sub)` can produce
  the same input string, even with `:` or `|` inside either value. Golden vector (**verified** against
  the real `substrate.SHA256NanoID` in an isolated worktree, 2026-07-10; the conformance test derives
  and asserts it, §7): `iss = https://accounts.google.com`, `sub = 110169484474386276334` →
  input `idpsub:27:https://accounts.google.com:110169484474386276334` →
  `ActorID = vtx.identity.1FF5tdoN7GEGfDedQZ95`. The raw `(iss, sub)` ride out on new
  `VerifiedActor.Issuer` / `VerifiedActor.RawSubject` fields (provenance, §3.3); `Subject`/`ActorID`
  carry the **derived** id so the RLS var, revocation key, resolver key, and envelope stamp all agree
  with zero per-surface changes.

  **The per-source issuer pin is a MUST, and it is load-bearing for confinement (finding A8).** The
  single global `Config.Issuer` field is insufficient for a **multi-IdP** deployment (two trusted
  sources, one issuer field can't pin both). If a source is left unpinned, a hostile-but-trusted IdP-A
  can sign `iss = <IdP-B's issuer>`, `sub = <a real IdP-B user's sub>` and derive **that user's**
  ActorID — cross-issuer sub-replay, consumer-to-consumer impersonation. So the issuer moves onto the
  per-kid spec and the check is mandatory *before* derivation. (A single-IdP deployment is the
  degenerate case: one source, one issuer.) A source whose IdP emits multiple `iss` forms (Google,
  §2.4) declares **one**; the other form is then **rejected** (the check is exact-match, not
  normalization — pinning selects one accepted form and denies the rest, it does not transparently
  fold them — finding MINOR-6). The failure direction of a wrong pin is denial, never impersonation.

- **`nanoid` — the in-code dev pin only.** The `sub` **is** the bare identity NanoID. Fail-closed shape
  gate: `substrate.IsValidNanoID(sub)` must pass, else reject (`ErrInvalidSubject`, 401) — closing the
  flagged residual (garbage can no longer become a key). Binding is today's passthrough:
  `Subject = sub`, `ActorID = prefix + sub`. This mode is an **arbitrary-identity assertion grant**
  (a holder can mint a token for *any* identity, system actors included) — exactly right for the
  DEV-ONLY checked-in key + the dev/e2e minters that sign with it, exactly wrong for a third-party IdP.
  It is therefore pinned **in code** to the dev key and is **not operator-env-selectable** (finding
  MAJOR-4): the "grant a configured source nanoid semantics" knob had zero legitimate use (every real
  source is an external IdP → opaque) and was a pure impersonation footgun, so it is removed
  structurally rather than guarded by documentation.

**Mode resolution:** the spec rides the *key that verified* (per-kid); on a kid collision across
sources, the existing precedence holds (operator-configured static keys are re-merged over every JWKS
poll and win) — logged. Mode is **never** inferred from token content or subject shape (fork Option C,
rejected as default-open, §4).

**Config surface (operational, not contract):** each configured source declares its issuer — the
static dir and the JWKS URL each carry a `…_JWT_ISSUER` / `…_JWKS_ISSUER` (the existing single
`GATEWAY_JWT_ISSUER` generalizes to per-source). There is **no** `…_SUBJECT_MODE` knob (configured ⇒
opaque; dev key ⇒ nanoid, in code). The apps' loopback dev-auth posture rides the same in-code dev
pin. A configured source with no issuer declared **refuses to start** (fail-closed — no silent
unpinned opaque).

### 3.3 First-touch provisioning + provenance (the small identity-domain delta)

Derived subjects compose directly with the shipped provisioning: every opaque-mode `ActorID` is
NanoID-valid **by construction**, so `ProvisionConsumerIdentity`'s existing shape gate passes and a
real-IdP first touch provisions exactly like a dev one — this design *completes* the loop the
claim-flow design deliberately left open (§3.1: *"production IdP integrations already need an
enrollment step that maps their own subject id to a Lattice-minted NanoID; this design does not solve
that mapping"* — now solved, with derivation instead of enrollment).

One addition (claim-flow §11.4 already called for it): the Gateway's provisioning pre-flight passes the
provenance — payload gains optional `{idpIssuer, idpSubject}` (from `VerifiedActor.Issuer`/
`.RawSubject`) — and the script writes an **`.idpBinding` aspect** on A:
`{ "class": "idpBinding", "data": { "iss": "...", "sub": "..." } }`. This is the audit/support
answer to "which IdP account is this identity?" (the derivation is one-way; without the aspect the
question is unanswerable). Sensitivity: **`.idpBinding` is classed sensitive, like every other identity
PII aspect.** (An earlier draft claimed email/phone were stored plain, quoting the claim-flow design —
**stale**: `packages/identity-domain/ddls.go` now classes `name`/`email`/`phone`/`ssn`/`dob` all
sensitive, Vault-encrypted at rest per-identity-DEK. Some IdPs put emails in `sub`, so `.idpBinding`
joins its siblings.) Two consequences, both good: A gains a DEK, so **crypto-shredding A severs the
IdP-account linkage** (the claim-flow §11.2 "shred the discoverable identity-set" walk now covers the
credential plane too); and the audit read goes through the sanctioned Vault decrypt path like any other
PII, not a plain read. A is otherwise bare by design (claim-flow §11.1 step 5) — `.idpBinding` is
credential-plane provenance, not business PII accretion.

### 3.4 What does NOT change (verified, not assumed)

- **The resolution seam (R1)** — resolver keys are `ActorID` strings; derived ids flow through
  untouched. The `ClaimIdentity` carve-out likewise (the credentialindex hashes `op.actor`, which is A
  in both modes; the carve-out logic is `gateway.go:390-393`, the const/doc at `:204-209`).
- **Revocation** — keys off the pre-resolution `ActorID` (= A); a derived A revokes exactly like a
  passthrough one. (Loupe's F11 revoke console displays/accepts actor keys — consistent, since
  everything shares `ActorID`.)
- **RLS read path** — the `set_config('lattice.actor_id', …)` var carries the bare id of the
  **§11.4-resolved** actor (`nanoIdFromKey(resolveActor(ActorID))`, `read.go:105`; the apps rewrite
  `Subject` to the resolved id first, `loftspace-app/readauth.go:215`), *not* the raw `Subject` —
  correcting an earlier draft that said "carries Subject" (finding MAJOR-3). Grant rows are projected
  from graph state (A/U vertex keys), so both sides see the same resolved id. What changes is only how
  `ActorID` was computed upstream; the resolve-then-scope step is unchanged.
- **Contract #2 stamping, #6 capability lookup, #9 claim mechanics** — all consume the actor string;
  none knows or cares how it was computed.
- **Internal service actors** — Loom/Weaver/objmgr/privacy/Gateway system identities authenticate by
  NATS user (transport), not JWT; out of this contract's scope entirely (stated in #11 §11.1 so nobody
  "fixes" them into it).
- **A surface that pins a *specific expected* ActorID must pin the DERIVED key under opaque** — Loupe's
  operator gate compares `actor.ActorID` to a configured operator key (`cmd/loupe/readauth.go:364`).
  Under opaque, that configured key must be the operator's *derived* `vtx.identity.<SHA256NanoID(iss|sub)>`,
  obtained by running the derivation on their `(iss, sub)` (a one-line helper / `gateway derive-actor`
  aids this). Soft today (Loupe real-login is deferred; dev posture is the nanoid pin), but a genuine
  deployment step — flagged so it isn't a surprise (finding MINOR-8).

---

## 4. The fork, designed through: how a subject becomes an actor

**Option A — binding is a property of the trust source (RECOMMENDED; §3.2).** A configured external
source is always `opaque` + a mandatory declared issuer; the in-code dev key is `nanoid`. *Pros:*
deterministic per issuer (one issuer ⇒ one binding, always); no operator "mode" knob to misset — trust
grade follows the source kind structurally; dev flows unchanged (dev key pinned `nanoid`); zero
token-content coupling; the spec rides the per-kid provenance machinery that already exists. *Cons:*
each configured source must declare its issuer (a MUST, not a default — but the deployment already
needed to know its IdP's issuer); a *deployment* that wants IdP-enrolled NanoIDs doesn't get it yet
(see B). **The two adversarial refinements (§10 A8/MAJOR-1, MAJOR-4) are folded in**: per-source issuer
is mandatory (else confinement is unsound), and there is no operator-selectable `nanoid` (else a real
IdP could be handed impersonation power). Both make A *strictly* more fail-closed than the first draft;
neither adds surface.

**Option B — uniform derivation + custom-claim override.** The claim-flow §11.4 "(b)-compat" path: a
namespaced claim (`lattice_sub`) carrying a Lattice NanoID wins when present. *Pros:* one binding rule
for `sub`; per-token explicitness. *Cons:* token content switches binding semantics — one issuer's
tokens bind **two different ways** depending on a claim's presence, so one human can non-deterministically
become two actors (with vs. without the claim across token refreshes, IdP rule changes, or an IdP-side
misconfig); the override IS the arbitrary-identity assertion power (any claim-trusted IdP can name any
identity — the C problem, opt-in); and every dev minter (dev-token, the app endpoints, the Fake IdP)
must be re-tooled to mint the claim. Rejected as the default. **Absorbable later without rework**: if a
deployment ever genuinely enrolls its IdP (writes NanoIDs back at registration), that is a third
per-source mode (`claim`) added beside the two — the mode mechanism is the extension point, so B is not
foreclosed, just not built ahead of a real driver (dead-scaffolding test: no such deployment exists).

**Option C — shape-sniffing passthrough (REJECTED, and worth recording why).** "If `sub` parses as a
NanoID, pass it through; else derive." Zero config — and **default-open**: any trusted IdP (or any
tenant who controls their sub at a multi-tenant IdP, e.g. a `preferred_username`-mapped sub) could mint
a NanoID-shaped `sub` equal to an existing identity — the bootstrap root, a staff member, a system
actor — and the verifier would hand them that actor. The entire point of derivation is that an
`opaque` source's reach is **structurally confined to its own derived subspace** (collision-resistance
of SHA-256: reaching a chosen existing NanoID requires a preimage under a fixed `iss` prefix — *and*
the mandatory per-source `iss` check, §3.2/A8, denies a foreign issuer before derivation even runs);
C destroys that confinement on the attacker's terms. The check-the-default reflex, applied: a
configured source can *only* be `opaque` (confined), and `nanoid` (unconfined) is reachable only from
the in-code dev pin, never from config.

---

## 5. Contract surface

**Contract #11 — staged UNCOMMITTED in `main`** (the diff is the proposal, per the house rule):

- **NEW `docs/contracts/11-external-actor-authn.md`** — freezes: the accepted token profile (the §2.1
  MUSTs: asymmetric-only algs, required `kid`, required `exp`+skew, `aud` conditional, `sub` required;
  `jti` reserved), the **subject binding** (the §3.2 rules: binding per trust source carried per-`kid`
  with a mandatory spec — a spec-less kid is a construction error; configured source ⇒ `opaque` with a
  **mandatory per-source issuer** + the length-framed derivation string + the test-asserted golden
  vector; dev key ⇒ `nanoid` with the `IsValidNanoID` gate; never token-inferred), the **confinement
  guarantee and its precondition** (per-source issuer binding), the **all-surfaces-identical invariant**
  (any two surfaces verifying the same token MUST bind the same `ActorID` under the same specs), the
  **revocation binding** (keyed on pre-resolution `ActorID` = A), and the **credential→business
  resolution invariant** (resolution uniform across the *resolving* surfaces — the Gateway write stamp
  + the app read boundaries; the control-plane relay does not re-resolve; `ClaimIdentity` always submits
  raw A; a miss is deny-safe: act as A).
- **`docs/contracts/README.md`** — the #11 index row (same uncommitted proposal).

**Build-to (no change), verified per contract:** **#1** — derived/validated ids satisfy §1.1 key shapes
(that's the point); **#2** — the Gateway stamps the same reserved `actor` field; **#6** — step-3 looks
up whatever actor is stamped, no doc-shape change; **#9** — claim mechanics byte-identical (the
carve-out is #11's to state, not a #9 change); **#7** — no new system actors.

---

## 6. Reconciliation with the existing mental model

- **"Didn't we cover the IdP mapping?"** In sketch, yes — claim-flow §11.4/R3 (ratified 2026-07-06)
  chose derivation over IdP enrollment and named `sha256NanoID(iss|sub)`. But it was two paragraphs
  inside another design's walk-through, unbuilt, and silent on: how dev tokens keep working (modes),
  what happens to a non-NanoID sub *today* (nothing — the flagged unfixed residual), the `iss`-absent
  case, delimiter injection, the multi-form-`iss` gotcha, and where the rule gets frozen. Hence Andrew's
  question was the right one — the answer existed but wasn't consolidated anywhere discoverable, let
  alone contract-grade. This design is the consolidation; it **contradicts nothing ratified** (it
  realizes R3's chosen option (a), and its Option B analysis matches §11.4's rejection of (b)).
- **"The gateway now expects the NanoID in `sub`"** — true today, and *stays true for dev*: that
  behavior becomes the `nanoid` mode, pinned to the dev key. What changes is its **scope**: from "the
  only rule" to "the dev/enrolled posture," with `opaque`-derivation as the production default.
- **"Doesn't the claim flow / credentialindex already map external accounts to identities?"** It maps
  **credential identities (A) to business identities (U)** — layer 2→3, shipped. It cannot be layer
  1→2: an actor key must exist *before* any op (including `ClaimIdentity` itself) can be submitted or
  authorized, so the IdP-account→A step must be a pure verifier-side function, not graph state.
- **Does this introduce new state?** One optional aspect per provisioned identity (`.idpBinding`,
  §3.3) and a per-kid enum on the existing `KeyInfo`. No new bucket, lens, vertex type, or contract
  concept beyond #11 itself.
- **Parallel in-flight designs checked** (the same-seam collision rule): no other 📐/🏗️ design touches
  the verifier binding — `edge-lattice-full` consumes tokens downstream of it (EDGE.1 gains a frozen
  profile to build against, a benefit); `loupe-operator-auth-lift` and control-plane authz shipped
  against the passthrough and ride the same one-point change; the real-actor e2e Phase-1 proving lane
  explicitly declared the mapping out of its scope (§10 residual → this design).

---

## 7. Migration & test strategy

**Compatibility: dev *runtime* invisible, but the test corpus and the loader change — budgeted honestly
(finding MAJOR-2).** There is no production deployment (pre-public). Every **runtime** dev token mints a
real seeded/provisioned NanoID `sub` (the web JS mints `bareId(realKey)`; the staff/operator endpoints
mint the seeded admin/operator id; `gateway dev-token -sub <NanoID>`), all under the dev key → `nanoid`
pin → byte-identical. What is **not** free:
- **The loader gains a signature.** `auth.LoadTrustedKeys` / `KeySourceConfig` / the JWKS poller config
  now emit a per-`kid` binding spec `{mode, issuer}`; the three runtime dev call sites
  (`loftspace-app`/`clinic-app`/`loupe` readauth) thread the dev pin explicitly (they pass no `KeyInfo`
  today, so the spec must be supplied, not defaulted).
- **~30 direct-construction test call sites update.** `NewVerifier`/`SetKeysWithInfo` now require a
  binding spec per kid (no zero-value passthrough — the fail-closed default). Fixtures constructing a
  verifier with a test key must declare one. Many of those fixtures use convenient non-NanoID subjects
  (`"someone"`, `SOMEACTOR000000000000`, …) that fail `IsValidNanoID` under `nanoid` mode and have no
  `iss` under `opaque` — they are **not** "asserting the bug," they just need a **valid** NanoID subject
  (and, for opaque cases, a matching declared issuer). This is mechanical but real; it is in the fire's
  scope, not a footnote.

The *behavioral* improvement for real inputs stands: a non-NanoID `sub` that today silently becomes a
garbage key (failing late at `ProvisionConsumerIdentity`, `ddls.go:493-497`) is now rejected at the
trust boundary.

**Unit (`internal/gateway/auth`):** the binding matrix — `nanoid` × {valid sub, malformed sub → 401,
system-actor NanoID (allowed — documents the trust grade)}, `opaque` × {issuer matches → derive, missing
`iss` → reject, `iss` ≠ source's declared issuer → `ErrIssuerMismatch` (the cross-issuer-replay guard,
A8), `:`/`|`-laden `iss`+`sub` pairs → distinct ids (injection test), 255-char sub}; a spec-less kid →
**construction error**; the **golden derivation vector** (§3.2) **derived** via `substrate.SHA256NanoID`
and asserted against the frozen literal (the test is authoritative — finding MINOR-7); binding
resolution by verifying-kid incl. the operator-wins collision case; `VerifiedActor.Issuer`/`RawSubject`
population.

**Cross-surface agreement (the #11 invariant made executable):** one token verified through the Gateway
path and a readauth path binds the identical `ActorID` (table-driven over both modes).

**E2E (ephemeral stack, rides the shipped capability lane):** an opaque-mode token with a Google-shaped
`(iss, sub)` → first touch auto-provisions `vtx.identity.<derived>` with `.idpBinding` + `consumer` →
a second token for the same `(iss, sub)` resolves the **same** identity (determinism across restarts)
→ the derived actor round-trips a scoped allow/deny + a revoke (kill-switch keyed on the derived id).

**Verification gates:** the standard set (`go build ./...`, `make vet`, `golangci-lint`,
`make verify-kernel`, `go test ./...` — full suite: this touches a widely-constructed type's behavior)
+ `make verify-package-identity-domain` for the §3.3 delta.

---

## 8. Risks & alternatives considered

- **Pairwise-sub IdPs (OIDC §8).** A pairwise IdP mints per-client/sector subs; re-registering the
  relying party can change every `sub` → new derived As. Consequence: the humans' business identities
  (U) are safe — recovery is **re-claim** (bind the new A to the same U via Contract #9; A is bare by
  design, so nothing else is lost); Scenario-B identities (A-as-business) would strand, which is an
  operational migration event, not a silent failure. Documented in #11 as a deployment consideration
  (pin your IdP client; prefer public-subject IdPs for Scenario-B verticals). Not designed around
  further — an enrollment mode (B) wouldn't survive the same event either.
- **Multi-form `iss` (Google).** Handled by the mandatory per-source issuer declaration (§3.2): the
  source declares one form; the other is rejected at verification (exact-match, not normalization).
  Failure direction of a wrong declaration is denial, never impersonation.
- **Cross-issuer sub-replay in a multi-IdP deployment (finding A8/MAJOR-1).** Two trusted `opaque`
  sources with no per-source issuer binding would let source A sign `iss = <B's issuer>`, `sub =
  <a B user>` and derive that B-user's identity. **Closed** by making the per-source issuer check
  mandatory *before* derivation (§3.2) — the confinement guarantee holds only with it, so it is a MUST.
  Note this is why the single global `Config.Issuer` field is insufficient (it cannot pin two sources)
  and the issuer moves onto the per-kid spec.
- **Revocation evasion by re-signup.** A revoked human creates a fresh IdP account → new `(iss,sub)` →
  new A. Bounded: the new A is a bare `consumer` with none of U's grants (the claim secret is spent;
  staff re-issue is the only path back to U), so evasion buys nothing above anonymous signup — same
  posture as any system with self-service registration. Stated, not solved (an IdP-side ban is the
  deployment's lever).
- **Derivation collision.** 20 chars over a 58-char alphabet ≈ 117 bits from SHA-256 — birthday bound
  ~2^58 subjects for a collision; the platform's own `sha256NanoID` dedup/credentialindex keys already
  accept this class of bound. Not a new risk grade.
- **Rejected — validate-only (skip derivation, just reject non-NanoID subs).** Closes the residual but
  answers Andrew's question with "the IdP must know the mapping" — re-opening enrollment coupling F3
  already rejected. The derivation is ~15 lines on an existing primitive; validate-only saves nothing
  worth the coupling.
- **Rejected — options B and C** per §4.

---

## 9. Decomposition for the Steward (one fire, two increments)

**One S–M fire** (coupled-must-ship-together: the modes and the validation are one behavior change at
one seam), buildable immediately on ratification:

1. **Increment 1 — the binding seam.** `internal/gateway/auth`: a per-`kid` binding spec `{mode,
   issuer}` on `KeySourceConfig`/`LoadTrustedKeys`/the JWKS poller config; the `Verify` binding
   (derivation + mandatory per-source `iss` match + `IsValidNanoID` gate + new sentinels
   `ErrIssuerMismatch`/`ErrInvalidSubject`, `NewVerifier`/`SetKeysWithInfo` reject a spec-less kid);
   `VerifiedActor.Issuer`/`RawSubject`; the loader-signature change threaded through the runtime dev
   call sites (three readauth surfaces) + the ~30 test call sites (§7); the §7 unit + cross-surface +
   golden-vector tests. **Implementation note (finding MINOR-5):** the mode+issuer become a hot-path
   *trust* input, so co-locate them with the key behind a **single** atomically-swapped map
   (`map[kid]trustedKey{key, mode, issuer}`), not a second parallel `atomic.Pointer` beside `keys` —
   the current two-pointer `keys`/`info` split (`auth.go:145-146`) would allow a torn read pairing new
   keys with stale specs; and remove/guard the mode-discarding plain `SetKeys` (`auth.go:203`) so a
   swap can't silently downgrade a pinned set. `docs/components/gateway.md` (the fail-closed key-loading
   section gains the per-source mode+issuer; the header's "no frozen interface contract of its own" line
   updates to cite #11) + `agents/` skill docs untouched.
2. **Increment 2 — provisioning provenance.** `packages/identity-domain`: `ProvisionConsumerIdentity`
   payload gains optional `{idpIssuer, idpSubject}` → `.idpBinding` aspect; Gateway pre-flight passes
   them; the §7 e2e. (Rides with increment 1 in the same fire; split only if the fire needs to land in
   two greens.)

**On ratification, Winston commits** the staged `docs/contracts/11-external-actor-authn.md` + README
row + this doc's status flip + the board row in one scoped commit (the ratified-contract-commit rule).

---

## 10. Self-adversarial pass (security plane — run twice; a fanned-out review sharpened it)

The first pass produced A1–A7. A dedicated read-only adversarial sub-agent then reviewed the drafted
design + contract against the code and surfaced **A8 (a genuine unsoundness in A1's confinement claim)**
plus the RLS/migration/simplification findings folded through §3–§9 (MAJOR-2/3/4, MINOR-5/6/7/8). A1 is
corrected below rather than left as first written — the elegant "confined subspace" claim was only true
under a precondition the first draft didn't enforce.

- **A8 (MAJOR — folded, was the missing precondition on A1) — cross-issuer sub-replay.** With **two**
  trusted `opaque` sources and no per-source issuer binding, hostile-but-trusted source A signs
  `iss = <B's issuer>`, `sub = <a real B user>` → derives **that B user's** identity (no preimage
  work — the `iss` is A's to choose). The confinement was "its own subspace" but the true reach without
  the fix is "the union of all trusted opaque subspaces." → **Closed** by making the per-source issuer
  check mandatory *before* derivation (§3.2); confinement is now stated with its precondition, in the
  design and the contract, not as a bare absolute.
- **A1 (corrected) — can an opaque source reach an *existing* identity?** Under the A8 fix: no. It can
  only present tokens whose `iss` equals its declared issuer, so it is confined to that issuer's derived
  subspace; reaching a chosen NanoID there needs a SHA-256 preimage; worst case per token is a fresh
  bare `consumer`. It cannot reach a peer issuer's subspace (A8 check), nor a `nanoid`-space seeded
  actor/root (disjoint spaces). → §4-C.
- **A2 — Does any default flip open?** Checked each under the revised model: a spec-less kid →
  **construction error** (not a silent default); configured source → `opaque` (confined) + mandatory
  issuer; `nanoid` → in-code dev pin only, never config; missing/mismatched `iss` in opaque → reject;
  malformed sub in nanoid → reject; unknown kid → reject. No omission grants anything. → §3.2.
- **A3 — Delimiter injection in the derivation input.** `iss`/`sub` are attacker-influenced (a
  hostile-but-trusted IdP); naive `iss + "|" + sub` is ambiguous (`a|b`+`c` ≡ `a`+`b|c`). → Length
  framing (§3.2); injection unit test (§7).
- **A4 — Spec confusion across sources.** A JWKS kid shadowing the static dir's kid with a different
  spec → the existing operator-wins merge precedence resolves it deterministically; log on collision.
  → §3.2.
- **A5 — `nanoid` is impersonation-grade.** True and unavoidable (dev needs it); the mitigation is now
  **structural**, not documentary — it is unreachable from config (the operator-selectable knob is
  removed, MAJOR-4), so no operator can grant a third-party IdP nanoid semantics at all. → §3.2.
- **A6 — Provisioning DoS via infinite fresh subjects.** A hostile trusted IdP (or a stolen dev key)
  can mint unlimited fresh `(iss,sub)` → unlimited bare identities. Pre-existing property of the
  shipped provisioning design (idempotent, consumer-only, rate-limited at the reverse-proxy, Gateway
  Fire 5); derivation neither widens nor narrows it. Not re-litigated.
- **A7 — Does the derived id leak the IdP account?** One-way (no enumeration from the id); linkability
  lives only in the explicit `.idpBinding` aspect, which is the *point* (audit), is **sensitivity-classed
  and Vault-encrypted like its sibling identity PII aspects** (correcting a stale plain-storage claim,
  §3.3), and is severed by crypto-shredding A (the aspect gives A its DEK). → §3.3.

---

## 11. Companion doc/board updates made in this fire

- `docs/contracts/11-external-actor-authn.md` + `docs/contracts/README.md` row — **staged UNCOMMITTED**
  for Andrew.
- `gateway-external-trust-boundary-design.md` — §5's "Deferred (Contract #11)" paragraph and §10's
  deferral bullet rewritten in place to point here (the un-defer supersedes them; rewrite-not-banner
  rule); the dated 2026-06-29 ratification record gains a bracketed pointer only (history stays).
- `gateway-claim-flow-identity-provisioning-design.md` — §11.4 gains a one-line pointer (R3's design of
  record is now this doc + the staged #11).
- `docs/vendors.md` — new **External IdP (OIDC)** row (OIDC Core 1.0 + per-vendor docs are now
  load-bearing for #11; includes the Google two-form-`iss` note).
- `_bmad-output/planning-artifacts/backlog/lattice.md` — new Security-&-trust-boundary row,
  📐 awaiting-Andrew, linking here.
- *(2026-07-10, ratification Q&A)* §12 added; §3.3/§10-A7 PII-sensitivity claim corrected against
  current code; two follow-up rows filed (multi-credential linking + merge credential-awareness →
  `lattice.md`; app-read revocation gap → `verticals.md`).

---

## 12. Ratification Q&A (Andrew, 2026-07-10) — resolution cost; multi-IdP duplicates

Two questions raised while reviewing the staged design, answered from the code and folded in here so
the doc is the record.

### 12.1 Is the per-request A→U resolution a bottleneck?

**Your reading of the posture is correct and unchanged by this design:** when A is bound to U, the
door that receives the request resolves before acting — the Gateway stamps the resolved actor on every
write, and (browser-direct) the app's read boundary rewrites the RLS actor on every read. Exactly
**one resolution per external request** at whichever door received it (the control plane never
re-resolves — #11 §11.4; the Processor consumes the stamped actor as-is).

**What a resolution costs today (grounded).** `credentialbinding.Resolver.Resolve` is a single NATS-KV
`Get` against the deployment-local `credential-bindings` bucket (`credentialbinding.go:67`) — no cache,
deliberately simple. Per external request the doors run: Gateway write = JWT verify (in-process
crypto) + revocation `Get` + resolution `Get` + the Processor request-reply commit; Gateway read =
verify + revocation `Get` + resolution `Get` + the RLS `SELECT`; app read = verify + resolution `Get` +
the RLS `SELECT`. So the resolution `Get` is the **same cost class as the revocation `Get` the Gateway
already runs per request** (the accepted precedent): one round-trip on the same persistent connection
to the same NATS server the request's real work rides — and that real work (a JetStream
request-reply commit, or a Postgres query) is the latency floor, an order of magnitude above a KV
`Get`. At the platform's target write rates (the 10–100 ops/sec envelope the Gateway F2 fork was
argued against) this is noise, not a bottleneck.

**"Did we already have the answer?" — half of it.** The *staleness* posture was designed (the M3
CDC-lag window: a fresh claim resolves as A until the materializer folds it — deny-safe, self-healing)
and the *provisioning* pre-flight already carries an in-process cache (`provisionedCache`, bounded
100k, "pure latency optimization"). The resolution **throughput** posture was never written down. Now
it is, with the scale path pre-named so nobody redesigns under pressure:

1. **In-process TTL cache** on the resolver (positive entries only, seconds-scale TTL) — the
   `provisionedCache` pattern. Caveat that bounds the TTL: a future merge/rebind (§12.2 Gap 2) would
   repoint a binding, and a long-lived positive cache would keep resolving to the merged-loser.
2. **The NATS-native endgame: a KV watch-mirror.** The bucket is small (one entry per claimed human);
   a `Watch` folding it into an in-memory map makes resolution zero-round-trip at every door — the
   same local-fold discipline the Gateway's own revocation/bindings materializers already demonstrate.
   Correct under the same deny-safe miss semantics (worst case: act as A for the watch-lag window).

Neither is needed now; both are correctness-preserving when needed. **One asymmetry worth knowing
(deliberate, verified):** a revocation-check *error* denies (fail-closed — security boundary); a
resolution *error* falls back to acting as raw A (deny-safe — reduced scope, `readauth.go:210`,
`gateway.go`'s `resolveActor`). Both are the right direction for what each protects.

**One adjacent gap surfaced by this grounding (filed, `verticals.md`):** all four vertical apps'
read postures construct `NewAuthenticator(verifier, nil)` — **no revocation checker** — so a revoked
credential keeps *reading* at the apps until its token expires (writes die immediately at the
Gateway). The short-TTL backstop covers it, but the apps already open the bindings bucket per request;
opening `token-revocation` beside it is symmetric and cheap.

### 12.2 Multi-IdP sign-in (Google yesterday, Apple today) — duplicate identities

**Reframe with the two layers first.** Sign-in via a second IdP mints a second **credential** identity
(A_G, A_A — distinct `(iss, sub)`, distinct derived keys, both bare). That much is by design and
harmless *in itself* — a human with two sign-in methods legitimately holds two credentials. Whether it
becomes a **duplicate-identity problem** splits by scenario, and the honest answer is: **prevention is
structurally impossible to do completely, resolution machinery exists but has two real gaps, and the
strongest lever is a deployment-topology choice.**

- **Scenario A (walk-in; business identity U exists).** The dupes are *only credentials*. The problem
  is **Gap 1 — there is no second-credential binding path**: `credentialBinding` is a singular aspect,
  `ClaimIdentity` requires `state == "unclaimed"`, and the claim secret is spent on first use — so
  strictly **one A per U, ever**, today. The Apple login *works* but acts as bare A_A and sees none of
  the person's data (deny-safe, but reads as "the app lost my account"). The fix is an explicit
  **link-another-sign-in flow** — authenticated as U (via A_G), authorize binding A_A; or a staff-gated
  re-issue scoped to *claimed* identities (today's `RotateClaimKey`/R4 correctly fails closed on
  claimed). Note #11 is already compatible: its dedup guard is per-credential (each A binds ≤ 1 U);
  N credentials → one U is an additive extension, no contract change.
- **Scenario B (self-signup; A_G *is* the business identity).** A_A is a genuine duplicate business
  identity. **Prevention (partial, buildable):** the token's **verified email claim** can probe the
  existing blind dedup index — `vtx.identityindex.<sha256NanoID("email:"+email)>` is computable
  transiently without persisting the email (a read-probe, compatible with "the Gateway writes no PII
  from tokens"), and it works **despite Vault encryption** because the index is a deterministic hash
  vertex. A hit at first-touch means "this human probably exists — offer linking," instead of silently
  minting a parallel identity. But a probe with no linking flow to act on the hit is dead scaffolding —
  so it sequences **with** Gap 1's flow, not before it. **Honest limit:** email matching is a
  heuristic, not a guarantee — Apple's Hide-My-Email mints per-app relay addresses that defeat email
  matching *everywhere* (Lattice-side and IdP-broker-side alike), and humans use different emails per
  provider. The only complete mechanism is explicit user-driven linking + after-the-fact merge.
- **Resolution — identity-hygiene, audited for exactly your question.** Two findings, one already
  tracked, one new:
  1. **Candidate discovery is inert over Vault ciphertext** — `duplicateCandidates`' WHERE (email/phone
     equality, name-Levenshtein) runs on per-identity-DEK ciphertext. Already a tracked board row
     (Privacy/Vault, needs-design). Note for that design: the **identityindex is precisely the blind
     index the equality half wants** (shared-index-membership as the match signal); only the
     name-Levenshtein half genuinely needs something new.
  2. **`MergeIdentity` is not credential-aware (new — Gap 2, filed).** Verified in the package: the
     merge script rekeys only the operator-supplied link lists and writes
     `state=merged`/`mergedInto`/ACR — it never repoints `vtx.credentialindex.<hash(A)>` entries or
     `credentialBinding` aspects, **and** the Gateway/app binding materializers fold **only**
     `identity.claimed` events (`credential_bindings_materializer.go:119`), so no merge-driven rebind
     could reach the local buckets even if the graph were fixed. Consequence: if U2 (claimed by A)
     merges into U1, **A resolves to the merged-loser U2 forever** — graph-side and bucket-side. The
     fix shape: merge repoints the credentialindex + emits an `identity.rebound`-class event the
     materializers fold (and any resolver cache from §12.1 must respect).
- **The strongest prevention is topological: prefer the IdP-broker posture.** Your premise (N raw
  issuers + a chooser) is supported by this design's per-`kid` specs — but the **recommended** prod
  posture is **one Lattice-facing issuer**: the deployment's broker (Keycloak/Auth0) federates
  Google/Apple upstream, and account-linking happens **at the broker**, the layer that owns the email
  + interactive context (Keycloak's First-Broker-Login flow does exactly this — "detect existing
  user," automatic/manual linking, disable-auto-create; vendor-corroborated 2026-07-10). Lattice then
  sees **one `(iss, sub)` per human** no matter which button they clicked, and the whole dupe class
  shrinks to the broker's linking quality. Under #11 this is just the degenerate one-source case — no
  contract or code delta. Direct multi-raw-IdP trust remains supported and inherits the
  in-Lattice mechanisms above.

**Filed from this Q&A:** `lattice.md` → *Multi-credential identity linking + merge
credential-awareness* (Gap 1 + Gap 2 + the provision-time probe, one design item — they share the
binding seam); `verticals.md` → *app read boundaries skip the revocation kill-switch* (§12.1).
Neither changes the staged #11.
