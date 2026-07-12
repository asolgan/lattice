# Apply-to-lease 403s for every applicant — app flow gap, not a platform bug (finding, 2026-07-11, corrected)

**Repro (live, this dev stack, PO discovery fire):** LoftSpace `+ New applicant` (submits
`CreateUnclaimedIdentity`) → select the new applicant → `Apply` on any listing → **`Application rejected —
AuthDenied: no matching platformPermission`**. Reproduced on 3 identities: two pre-existing picker entries
and a brand-new one created fresh during this fire. Every applicant identity currently in this stack's
picker holds zero `holdsRole` links.

**Corrected verdict (superseding this doc's earlier version): `ProvisionConsumerIdentity`'s idempotency
check — "if the target vertex already exists, no-op" (`packages/identity-domain/ddls.go:724`) — is
correct as written, not a bug.** An op whose job is to *provision a new identity* is right to refuse to
touch a vertex it didn't create. The earlier framing proposed swapping that check for a `holdsRole`-link
check instead; on reflection that's wrong on two counts: (1) mechanically, the op's mutation set bundles
three creates (vertex + `.state` + link) behind one gate — conditionally creating just the link while the
vertex already exists is a different, much larger behavior change than "fix an idempotency check"; (2)
more importantly, it would let `ProvisionConsumerIdentity` silently grant the `consumer` role — i.e.
effectively auto-claim — a vertex some *other* op created, with no verification at all. That's exactly the
check `ClaimIdentity`'s claim-secret gate exists to enforce, and bypassing it is a real authorization hole,
not a fix.

**The actual root cause is `loftspace-app`'s own flow, which skips the claim ceremony by design.**
`cmd/loftspace-app/web/app.js:565`: *"the applicant is created directly (no claim ceremony in this
demo)."* The app creates U (`CreateUnclaimedIdentity`) for the new applicant, then mints a dev-token whose
`sub` is **U's own key** and self-submits `CreateLeaseApplication` as U directly. That collapses the
platform's two-identity design (`gateway-claim-flow-identity-provisioning-design.md` §11.0/§11.1, ✅
Andrew-ratified 2026-07-06) into one: there is no separate credential identity (A), so `U` never goes
through the flow that would grant it the `consumer` role. `clinic-app` has the identical shape
(`app.js:272-290`, `asSelf`/`authContext.target`) and will hit the same gap.

**The ratified design already specifies the real flow (§11.1, "Scenario A — the walk-in applicant"):**
staff creates U (unclaimed) → the applicant later signs up at the IdP on their own device → the Gateway's
existing `ProvisionConsumerIdentity` pre-flight auto-provisions a **bare, distinct** credential identity A
→ the app calls `ClaimIdentity{targetIdentityKey: U, claimKey: <secret>}` with `authContext.target = A`
(§11.1 step 6) — this is what binds A→U and grants **U** the `consumer` role. From then on the person acts
as U. `ProvisionConsumerIdentity` only ever touches A in this design; it was never meant to be called
directly on U, which is what the demo shortcut does today.

**Fix belongs entirely in the vertical app(s), not the platform.** `loftspace-app` (and `clinic-app`) need
to implement the real claim-link handoff already spelled out in §11.1a (client-minted secret, URL-fragment
claim link, `ClaimIdentity` call) instead of self-submitting directly as U. No platform/package change is
needed — the mechanism (`ProvisionConsumerIdentity`, `ClaimIdentity`, the claim-key flow) is already built
and already ratified; it's just not wired into either app's demo UX yet.
