# Loupe F20 — Hosted-demo read-only operator

**Status:** ✅ adjudicated (Winston, 2026-07-19 — Andrew-delegated for the Loupe program).
Lane: [backlog/loupe.md](../planning-artifacts/backlog/loupe.md). Companion:
[deploy/demo/README.md](../../deploy/demo/README.md) (the hosted-demo deployment as it stands today).

**Exposure is Andrew-gated.** This design covers what must be *true* before Loupe can be a public
demo surface, and builds the Loupe-side half. Actually pointing a subdomain at the console is a
separate, explicit decision at the demo's public-launch phase — nothing here exposes anything.

## 1. What Andrew asked for

Loupe in the public demo as the behind-the-scenes view: a `demoOperator` role stripped to
inspect-only grants, so **every write surface is capability-denied at the platform** — proving "even
the console is capability-scoped" live, in front of a visitor. Plus a one-tap demo login mirroring
Facet's persona posture, its own subdomain, and a visitor disclaimer.

Today the demo deliberately does **not** expose Loupe: `deploy/demo/README.md` says "the operator
inspector is deliberately not exposed — reach Loupe via `ssh -L 7777:127.0.0.1:7777`". F20 is the
work that would make exposing it defensible.

## 2. Grounding — what the console actually does today

Read against `cmd/loupe` at `4b2e5946`.

- **One front door.** `srv.requireOperator(mux)` (`main.go:191`) gates *every* route — static UI and
  `/api/*` alike — except `/login` and the three credential-exchange endpoints.
- **One named operator.** `verifyOperatorToken` (`readauth.go:352`) denies any token whose
  `ActorID != s.operatorActorKey`. The console is a *named* identity's login, not "anyone the
  trusted key will sign". `operatorActorKey` comes from `LOUPE_OPERATOR_ACTOR_KEY`, **defaulting to
  the bootstrap `adminActor`** (`main.go:150`).
- **The minter is fixed-subject.** `POST /api/operator/dev-token` (`readauth.go:381`) mints for the
  configured `operatorActorKey` only, never a caller-supplied subject, and sets the session cookie —
  exactly the shape a one-tap demo login needs. Point `LOUPE_OPERATOR_ACTOR_KEY` at the demo identity
  and `/login`'s existing dev-login button *is* the persona card — but it does not work behind a
  proxy as things stand; see §2.3.
- **Writes are uniformly POST/DELETE.** Every mutating route method-gates, so "not a read method"
  catches every write. It is not the converse, though — see §2.2.

### 2.1 ⚠️ The finding that shapes this design

`setupOperatorAuth(logger, isLoopbackHost(bindHost))` (`main.go:162`, `readauth.go:127`) refuses
dev-auth off a loopback bind, with the stated rationale: "a misconfigured non-local bind with
dev-auth would let any network caller mint itself an operator token."

**Behind a reverse proxy that guard does not hold.** The demo binds Loupe to `127.0.0.1` and fronts
it with Caddy. The bind *is* loopback, so dev-auth is permitted — while every request arriving is a
public visitor's. The check tests the **bind host, not the peer**. So under the F20 deployment:

> any internet visitor can `POST /api/operator/dev-token` and be minted the console's fully
> configured operator credential.

This is not a bug to fix — it is F20's actual thesis, made explicit. In the demo that outcome is
**intended**: the one-tap login is *supposed* to hand every visitor the operator credential. What
makes it safe is that the credential names an identity whose **platform grants permit nothing but
reads**. Which yields the design's governing rule:

> **The security boundary is the platform's capability grants on the demo identity. Nothing in
> Loupe's own process is a boundary.** Loupe-side read-only enforcement is defense in depth and a
> UX contract — never the thing being relied on.

Everything below follows from taking that sentence literally — including, per §2.2, knowing exactly
where it stops being true.

### 2.2 ⚠️ Where the grants are NOT the boundary (adversarial review, 2026-07-19)

The rule above holds for the write surface: every op-submit relays through the Gateway under the
visitor's own operator token, and control-plane mutates carry the operator actor, so the platform
decides. Three-layer review found two places it does **not** hold, both caught before ship:

1. **Reveals ride reads.** `GET /api/objects/<oid>?decrypt=true` (`objects.go:456`) unwraps the CEK
   and serves plaintext. It is a GET, so a method rule never sees it — *and* `objectcrypto.UnwrapKey`
   issues the vault RPC on **Loupe's own NATS credentials with no `Lattice-Actor` header**, so the
   demo identity's grants are not consulted at all. A demo visitor could have read decrypted PII with
   nothing in the stack denying it. **Fixed in F20.1** at the decrypt branch's own condition. This is
   a place where Loupe's process *is* the only control — recorded so a later fire does not relax it
   believing the grants have its back.
2. **The read surface is not grant-narrowed.** Every read handler serves from Loupe's own credentials
   (all of Core KV, every vertex, the shred roster). "Inspect-only grants" constrain what the demo
   operator can *do*, not what the console will *show*. The banner copy was corrected to promise only
   what the process enforces — writes and reveals refused — rather than implying narrowed reads.

Corollary for §3.1: the demo identity's grants must be narrow because the minted token is a **real
actor JWT** the visitor holds and can replay directly at the Gateway, entirely outside this console.
That is by design, and it is why Layer 1 is the boundary — but only for what the platform mediates.

### 2.3 ⚠️ The one-tap login does not survive a reverse proxy (unowned, blocks F20.4)

`crossOriginBlocked` (`server.go:168`) requires the request's `Origin` host to be loopback or exactly
the configured bind host. All three credential-exchange endpoints call it. Behind Caddy the browser
sends `Origin: https://loupe.demo.example` while `bindHost` is `127.0.0.1` — neither match, so
**every login and logout 403s** and no visitor can get in. (The §2.1 minting hazard survives this: a
curl client sends no `Origin` at all.)

Related, same deployment: `setOperatorSessionCookie` sets `Secure` from `!isLoopbackHost(bindHost)`
(`readauth.go:236`), so a loopback bind behind a TLS proxy ships the session cookie **without
`Secure`** on a public HTTPS site.

Both need a configured public-origin/trusted-proxy posture. Not built here — changing a
rebinding-hardened gate belongs in its own fire, not bundled with the demo posture. Filed as **F20.5**
and added to the exposure checklist.

## 3. Design

Three layers, deliberately unequal in weight.

### 3.1 Layer 1 (the boundary, cross-lane) — the `demoOperator` grant scoping

A demo identity holding read grants only: no `InstallPackage`/`UpgradePackage`/`UninstallPackage`,
no `ReviewProposal`, no `ShredIdentityKey`, no `RevokeActor`, no `lattice.vault.decrypt`, no
generic op-submit. The F15 precedent is `packages/console-operator` (its own read-grant lens +
persisted identity, wired by `up-full`) — the demo role is that, minus every write.

**Cross-lane:** `packages/**` is not the Loupe lane. Filed to the Lattice lane; F20's exposure
gate depends on it. **Until it exists, Loupe must not be exposed** — see §3.2's boot guard, which
enforces exactly that precondition rather than documenting it.

### 3.2 Layer 2 (in-lane, built now) — `LOUPE_DEMO_MODE`

A process-wide demo posture in `cmd/loupe`, default off.

**(a) Fail-closed write denial.** One middleware wrapping the mux, **default-deny by method**: every
request that is not `GET`/`HEAD` is refused `403` with a stable, visitor-legible reason, except a
three-path credential-exchange allowlist (`dev-token`, `session`, `logout` — a visitor must be able
to log in and out).

Method-based rather than a path list because it is **fail-closed for routes that do not exist yet**:
a write endpoint added by a future fire is denied in demo mode without anyone remembering to update a
list. A path allowlist fails open on exactly that case.

It over-denies, accepted deliberately: the control plane tunnels three pure reads through POST (loom
`inspect`, refractor `health` and `validate`), so a demo visitor loses those inspection replies —
some of the more compelling "behind the scenes" surfaces. Restoring them means teaching the rule
which control ops are reads, a classification living in `control.go` that would fail **open** if it
drifted. Tracked as F20.2 rather than traded for a stale allowlist.

**Reveals are a separate axis** and are denied at their own call sites, not by this rule — see §2.2.

**(b) A boot guard, not a warning.** Demo mode **refuses to start** unless
`LOUPE_OPERATOR_ACTOR_KEY` is set explicitly *and* differs from the bootstrap `adminActor`.

This is the load-bearing piece. Without it, `LOUPE_DEMO_MODE=1` on a stock stack would silently run
the demo posture **as the bootstrap admin** — read-only in Loupe's own process, and omnipotent to
anything that reaches the platform another way. Per the house rule that a confinement guarantee must
never rest on an advisory precondition, the precondition that "the configured operator is a
scoped demo identity" is enforced at boot, and the process exits if it is not met. It is the
mechanism by which §3.1 cannot be skipped.

*(The guard checks the identity is distinct and explicit — it cannot verify the grants are
actually narrow, which only the platform knows. It closes the stock-stack footgun, not every
misconfiguration.)*

The flag itself is parsed fail-closed for the same reason: `LOUPE_DEMO_MODE` set to anything not
recognizable as a boolean (`=enabled`, `=Y`) **refuses to boot** rather than reading as false. A typo
that silently disables the posture also silently skips this guard, and the result is a fully writable
admin console on a public URL — the exact outcome the guard exists to prevent.

**(c) Honesty surface.** `GET /api/demo` reports the posture, and the shell renders a persistent
visitor banner: this is a live operator console, in read-only demo mode, write actions are denied by
the platform's capability grants. The banner is the disclaimer Andrew asked for; it states the
*platform* is the reason, not a UI toggle — which is both true and the point being demonstrated.

### 3.3 Layer 3 (cross-lane, launch-gated) — exposure

A `deploy/demo` Caddy site block for the Loupe subdomain, and the README's "deliberately not
exposed" paragraph rewritten. `deploy/**` is out of the Loupe lane; and exposure is Andrew's call at
public-launch regardless. **Not built.**

## 4. Adjudication (Winston, 2026-07-19)

Forks resolved:

1. **Deny by method vs. by path** → **by method** (§3.2a). Fail-closed for future routes; the
   no-read-only-POST enumeration makes it exact today.
2. **Warn vs. refuse to boot on an unscoped operator** → **refuse** (§3.2b). A demo posture whose
   safety rests on an env var someone remembered to set is not a posture.
3. **Suppress write affordances vs. let them 403** → **let them 403 for now**, with the banner
   setting expectation. The 403 is honest and, at a demo, arguably the more persuasive artifact —
   the visitor *sees* the denial. Suppression is polish, not safety; filed as F20.2 rather than
   ballooning a security-adjacent fire across ~10 view modules.
4. **Does Loupe's read-only mode count as the guarantee?** → **No for writes**, which the platform
   mediates (§2.1, §3) — but **yes for reveals**, where the vault RPC carries no actor and this
   process is the only control (§2.2). Recorded in both directions so a later fire neither promotes
   the middleware to a guarantee nor relaxes the reveal denial assuming grants cover it.

Deliberately **not** done: "fixing" the loopback/reverse-proxy gap in `setupOperatorAuth`. It is
correct for the dev posture it guards, and under F20 the minting-for-everyone behavior is intended.
Narrowing it would break the one-tap login this design depends on.

## 5. Fires

| Fire | Scope | Lane | State |
|---|---|---|---|
| **F20.1** | §3.2 — `LOUPE_DEMO_MODE`: method default-deny middleware, fail-closed flag + boot guard, reveal denial (§2.2), `/api/demo` + visitor banner | Loupe | ✅ SHIPPED 2026-07-19 |
| **F20.2** | §3.2c polish — suppress write affordances per view; restore the three read-only control POSTs; a disclaimer on `/login` (the visitor's first screen has none, and `/api/demo` is auth-gated so that page cannot read the posture without a redirect loop) | Loupe | 📋 ready (not a safety item) |
| **F20.3** | §3.1 — the `demoOperator` grant package | Lattice (cross-lane) | filed |
| **F20.4** | §3.3 — Caddy subdomain + README | deploy (cross-lane) | 🚧 Andrew-gated on public launch |
| **F20.5** | §2.3 — public-origin/trusted-proxy posture: `crossOriginBlocked` must accept the demo hostname, and the session cookie must be `Secure` behind a TLS proxy. **Blocks F20.4** — without it no visitor can log in | Loupe | 📋 ready |

**Exposure checklist** — every line must hold before Loupe is reachable publicly:

1. F20.3 shipped, the demo identity provisioned, and its grants spot-checked live (a denied write
   observed against the deployed stack, not inferred).
2. F20.5 shipped — otherwise login 403s and the session cookie is not `Secure`.
3. `LOUPE_DEMO_MODE=1` with `LOUPE_OPERATOR_ACTOR_KEY` naming the demo identity — the boot guard
   proves both, and a malformed flag now refuses to boot rather than failing open.
4. A rate limit in front of `/api/operator/dev-token`: minting is unauthenticated by construction and
   hands out a replayable actor JWT.
5. A decision on `/api/events/stream`: it is a GET, so demo-allowed, and the global cap is **4**
   concurrent tails (`events.go:131`) — sized for one operator. On a public URL the live feed dies
   for visitor #5 onward with a message written for an operator with too many tabs.
6. Andrew's explicit go-ahead. Exposure is his call, not a fire's.
