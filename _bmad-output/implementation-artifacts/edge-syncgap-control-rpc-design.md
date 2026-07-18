# Edge gap detection without STREAM.INFO — the `personal.syncgap` control RPC

**Status: ✅ SHIPPED (2026-07-17) — Inc 1 `0acd68c` (the platform op) + Inc 2 `7fc7b42` (the client
swap), both CI-green. The Edge node (Go + browser) now speaks no `$JS.API.STREAM.*` verb; the EDGE.5
Gate-3 reconnect leg is unblocked (a fresh attended :9222 re-run is the remaining verification). One
deliberate deviation from §8: the "live syncgap round-trip through the real control service" is delivered
at the `internal/edge/sync` layer (a real `control.Service` wired with `SetSyncFirstSeq` over embedded
NATS, driven via real `natstransport` with stream eviction advancing `FirstSeq`) rather than by extending
the Refractor manifest e2e — that harness drains SYNC via a raw consumer and has no control-service /
sync-Manager seam to extend.**

**Originally: ✅ Andrew-ratified (2026-07-17) — boolean result chosen (the §5 `firstSeq`
alternative declined, on ownership-of-semantics + minimal-wire grounds); no frozen-contract edit
(builds to Contract #6 §230's reserved `ctrl.<comp>.<verb>` namespace); the §7 availability trade-off
(warm boot now depends on the control plane being up) accepted with the bounded-retry + `cmd/facet`
sync-restart mitigation in scope for Inc 2. Build: ONE fire, two increments (§9), owned by the Lattice
Steward.**
Author: Winston (Designer fire, 2026-07-17). Ratified by Andrew in a /ratify session, 2026-07-17.
Backlog row: `planning-artifacts/backlog/lattice.md` → *Security & trust boundary → Edge gap-detection
needs STREAM.INFO, which the grant denies*.
Demand source: filed 2026-07-17 by the EDGE.5 W3-inc-3b parity fire
(`edge-browser-node-design.md` inc-3b finding (2): STREAM.INFO denied under the real per-identity
grant; pre-existing, hits the shipped Go nodes too; routed to the Designer by `34e1ef6`).

---

## For Andrew (one-look ratification block)

**What it does, in two lines.** Retention-gap detection moves off the JetStream admin API and onto the
Refractor personal control plane: a new identity-bound **`lattice.ctrl.refractor.personal.syncgap`**
RPC answers *"has SYNC retention pruned past my cursor?"* with a single boolean, and
`FirstSequence` is **deleted** from the Edge transport seam (Go transport, wasm transport, and the JS
shell all shed their only `$JS.API.STREAM.*` call). The per-identity NATS grant does not widen by one
stream verb — it's the mirror of the EDGE.4 `sessionkey` pattern, in the same four-places lockstep.

**No architectural fork.** This is not a trust-boundary change: the Edge stays confined to its
per-identity subjects; the control plane (which already holds a full-grant substrate connection as the
SYNC stream's owner) answers one more identity-bound question. No frozen-contract edit — the new
`ctrl.refractor.syncgap` verb builds **to** Contract #6 §230's reserved `ctrl.<comp>.<verb>` namespace
(exact-match operationType), exactly as register/hydrate/sessionkey did.

**The one real design call — DECIDED (Andrew, 2026-07-17): the boolean, not the first-sequence number
(trade-off in §5, stated honestly).** The adversarial pass (§10) established that the boolean is **not**
a side-channel closure: a client that controls `cursor` can binary-search `FirstSeq` in ≤64 calls, so
under adaptive probing the two shapes disclose the same watermark. The boolean still wins on three
grounds — the gap *semantic* (`cursor < FirstSeq`, and any future safety margin) stays with the
retention owner and can change without a wire change; the watermark is *extracted, not handed out*
(active probing is visible and rate-limitable later; a returned number is free); and the wire commits
to the minimum. If Andrew weighs client-side diagnosability higher, flipping the result to
`{firstSeq}` changes nothing else in the design (§5).

**Why not the two options the backlog row sketched.** Both are dead on the pinned vendor source (§4):
a scoped `$JS.API.STREAM.INFO.SYNC` grant cannot be made safe because the *request body* carries
`subjects_filter` (subject ACLs cannot constrain bodies — any identity could page through every
`lattice.sync.user.<id>` subject + per-subject message counts); and `CONSUMER.INFO` carries no stream
first-sequence at all, and per-subject prune-awareness is not derivable from it.

---

## 1. Problem + intent

`sync.Manager.gapped()` (`internal/edge/sync/sync.go:212`) is the Edge node's freshness gate: on warm
boot with a stored cursor, it asks the transport for the SYNC stream's earliest retained sequence and
re-hydrates iff `cursor < FirstSeq` — the edge-lattice-full-design.md §3.2/§3.3 "ephemerality:
re-hydrate, don't backlog-replay" posture (SYNC carries a 24h MaxAge). Both transport implementations
answer via `$JS.API.STREAM.INFO.SYNC`:

- Go: `natstransport.FirstSequence` → `JetStream().Stream(ctx, stream)` → STREAM.INFO
  (`internal/edge/transport/natstransport/natstransport.go:68`).
- Browser: shell `firstSequence` → `jsm.streams.info(stream)` (`internal/edge/browser/shell/shell.mjs:208`),
  called through `jsTransport.FirstSequence`.

The per-identity callout grant (`natsauth.PermissionsFor`, `internal/gateway/natsauth/natsauth.go:348`)
deliberately allows only the four per-durable CONSUMER verbs + per-durable ACK — **no STREAM.INFO**.
So every warm resume with a stored cursor fails closed at the gap check and `Run()` errors out. Latent
today only because cold start (no cursor) skips the check and hydrates; the first long-lived node that
restarts warm hits it. EDGE.1's trusted-posture nodes predated the EDGE.3 security turn-on, which is
why the call sat unexercised under the real grant until the W3-inc-3b parity harness drove it.

Intent: restore warm-resume gap detection for **both** node families without widening the per-identity
substrate grant, keeping the EDGE.3 confinement invariant — an Edge connection can reach only its own
sync subject, its own durable's consumer verbs, and the identity-bound personal control RPCs.

## 2. Grounding — the existing pattern this mirrors

The personal pseudo-lens control plane already carries exactly this shape of RPC, four times over
(`register`/`deregister`/`hydrate` — per-identity-nats-subscribe-acl Fire 2, `ca9affe`; `sessionkey` —
EDGE.4, `fb557cb`). The established lockstep, which this design copies verbatim
(`natsauth.go:83–113`'s own doc-comment names it):

1. **Wire**: `controlwire.ControlRequest`/`ControlResponse` op fields + a per-op result struct.
2. **Service**: `internal/refractor/control/service.go` — `supportedOps`, the §3.4 identity-binding
   `switch` in `dispatchEndpoint` (verified actor overrides/validates `body.IdentityID`), a per-op
   handler + timeout const, and a settable backend seam (`SetCoreKV`/`SetVault` precedent).
3. **Capability plane**: `internal/controlauth/ops.go` `RefractorOps` entry +
   `packages/control-authz/manifest.yaml` grant to the consumer role (scope=any, made safe by the §3.4
   binding).
4. **Transport grant**: `natsauth.go` `controlRPCs` list (the callout mints it into every per-identity
   user JWT).

Both halves (capability grant + transport grant) are independently necessary, jointly sufficient —
the natsauth doc-comment records the Fire-2 lesson that landing one without the other is either a
no-op or an unreachable grant. A fire building this must land all four together.

Interest-set registrations persist in the `personal-lens-interest` KV bucket
(`internal/refractor/personalinterest/interest.go`), so warm resume correctly skips `register` — this
design does not change the warm-boot call set beyond swapping which freshness question is asked, and
of whom.

## 3. The shape

### 3.1 Wire (`internal/refractor/control/controlwire`)

`ControlRequest` gains one field, used only by `syncgap`:

```go
// Cursor is used by the "syncgap" op: the last SYNC stream sequence this
// device applied. Serialized without omitempty — 0 (no deltas ever applied)
// is a legitimate, maximally-conservative value that must reach the server.
Cursor uint64 `json:"cursor"`
```

`ControlResponse` gains `PersonalSyncGap *PersonalSyncGapResult json:"personalSyncGap,omitempty"`:

```go
// PersonalSyncGapResult is the synchronous answer returned by the "syncgap"
// op: whether SYNC retention has pruned messages past the requesting
// device's cursor (edge-lattice-full-design.md §3.2 — a gapped cursor means
// a durable resume would silently skip deltas, so the device must
// re-hydrate). Deliberately a boolean, not the stream's FirstSeq: the
// watermark itself is stream-global state whose advance rate is an
// aggregate-activity side channel no per-identity caller needs.
type PersonalSyncGapResult struct {
    Gapped bool `json:"gapped"`
}
```

Subject: `lattice.ctrl.refractor.personal.syncgap` (the fixed `personal` pseudo-lensId, 5-token shape
`lensIDFromSubject` already parses). Request carries `{identityId, deviceId, cursor}`; identityId is
bound server-side, deviceId travels for logging symmetry with the sibling ops.

### 3.2 Service (`internal/refractor/control/service.go`)

- `supportedOps` += `"syncgap"`; the §3.4 identity-binding switch adds `"syncgap"` to its
  `case "register", "deregister", "hydrate", "sessionkey"` list (uniformity: the op has no
  per-identity *effect* to confine — the answer is identity-independent — but binding keeps the
  invariant "every personal op authenticates as exactly the caller" unconditional, and keeps a future
  per-identity refinement from starting default-open).
- A new settable backend seam, mirroring `SetCoreKV`/`SetVault`:

```go
// SetSyncFirstSeq registers the "syncgap" op's stream-state read: a func
// returning the SYNC stream's earliest still-retained sequence. The control
// host wires it to its own substrate connection's STREAM.INFO — the trusted
// full-grant read the per-identity Edge grant deliberately denies.
func (s *Service) SetSyncFirstSeq(fn func(ctx context.Context) (uint64, error))
```

  The wiring site is the existing `projection.IsPersonalLens(r)` branch in `cmd/refractor/main.go`
  (beside `controlSvc.SetPersonalHydrator`, ~:609): a closure over the host's `substrate.Conn` and the
  **lens rule's `r.Into.Stream`** — the authoritative stream name (the same value the adapter and the
  hydrator are wired from; a `"SYNC"` literal here could gap-check the wrong stream in a deployment
  whose personal lens targets a differently-named one, and a wrong-stream `FirstSeq` can yield a false
  "not gapped"). Same nil-clear semantics as `SetPersonalHydrator` when the personal lens rule
  unloads. The request deliberately carries no stream parameter, so the op can never be turned into an
  info oracle for other streams; the handler is lensID-independent (like `sessionkey` — the subject's
  `personal` token is a fixed pseudo-lensId, and `CapabilityKVChecker.Authorize` matches on
  operationType + scope, not lensID, with the transport ACL pinning Edge connections to the exact
  `personal` subject anyway).
- Handler `personalSyncGap` (own timeout const, `syncGapTimeout`, same order of magnitude as
  `authorizeTimeout` — one STREAM.INFO round-trip): nil seam → `ControlResponse{Error: "syncgap: stream
  state not configured"}` (fail closed, the `SetVault`-absent precedent); read `firstSeq`; respond
  `PersonalSyncGap: &PersonalSyncGapResult{Gapped: body.Cursor < firstSeq}`. No cursor validation
  needed: 0 yields `gapped=true` (max-conservative → re-hydrate), any huge value yields `false` —
  a client lying about its own cursor only mis-serves itself.
- Ops table: `RefractorOps["syncgap"] = {Verb: "syncgap", Read: true}` — its **own** verb (granting
  the consumer role the generic `ctrl.refractor.read` would also open `health`/`validate` on every
  lens, a topology leak; each-op-its-own-verb is the sessionkey precedent), classified `Read: true`
  honestly (it reveals one derived bit of stream state, mutates nothing).
- Manifest: `packages/control-authz/manifest.yaml` grants `ctrl.refractor.syncgap` scope=any to the
  consumer role, beside the four siblings.
- `controlRPCs` (natsauth) += `"lattice.ctrl.refractor.personal.syncgap"`.

### 3.3 Client — the seam SHRINKS

`transport.DeltaSource` **drops `FirstSequence`** (its only caller is `gapped()`), and
`sync.Manager.gapped()` becomes one `controlRequest(ctx, "syncgap", ControlRequest{IdentityID,
DeviceID, Cursor})` through the plumbing `registerInterest`/`callHydrate` already use — the Manager
already holds a `ControlClient`. **Response validation must not copy the siblings' nil-check shape
blindly**: `PersonalRegister == nil || !Registered` works because `false` is never legitimate there,
but `gapped:false` is this op's *common* answer — so the rule is `resp.Error != "" ||
resp.PersonalSyncGap == nil` → **error** (never a default), and only a present result's `Gapped` is
consulted. A builder who defaults nil→`false` has built the silent-data-loss direction. Deleted
outright: `natstransport.FirstSequence`, `jsTransport.FirstSequence`
(`internal/edge/browser/jstransport.go:99`) + `firstSequence` in jstransport's required-shell-method
list (~:47), the shell's `firstSequence` (`shell.mjs:208` + its `createShell` passthrough ~:376 +
test-double entries in `shell.test.mjs`, `consumer_create_driver.mjs` (comment), and the js host
test's fake shell `internal/edge/browser/host_js_test.go:76`). Net: the Edge node — Go and browser
alike — no longer speaks **any** `$JS.API.STREAM.*` verb; its JetStream-API footprint is purely its
own durable's consumer verbs. A syncgap failure surfaces from `ensureFresh` and fails `Run()` (fail
closed — never resume unverified), after a **bounded in-call retry with backoff** for transient
control-plane unavailability (§7 — new with this design; the STREAM.INFO call it replaces was
answered by the NATS server itself and needed no such tolerance).

## 4. Why the backlog row's two sketched options are dead (pinned-vendor grounding)

Pin: NATS server **v2.14.0** (`go.mod`, `docker-compose.yml`; `docs/vendors.md`).

- **Scoped STREAM.INFO grant** — `server/jetstream_api.go:437` (v2.14.0):
  `JSApiStreamInfoRequest{ApiPagedRequest; DeletedDetails bool; SubjectsFilter string}`, and
  `:1980` wires `req.SubjectsFilter` into the response's per-subject state (paged up to
  `JSMaxSubjectDetails` = 100k). A NATS subject ACL constrains the *subject*, never the *body* — so
  granting `$JS.API.STREAM.INFO.SYNC` to a per-identity connection hands every identity a paged
  enumeration of all `lattice.sync.user.<id>` subjects with per-subject message counts, plus the
  stream-global `Msgs`/`Bytes`/`FirstSeq`/`LastSeq`. That is the cross-identity metadata leak the
  grant was designed to exclude; "scoped" cannot exist at the ACL layer.
- **Gap from CONSUMER.INFO** — `server/consumer.go:55` (v2.14.0): `ConsumerInfo` carries
  `Delivered`/`AckFloor`/`NumAckPending`/`NumRedelivered`/`NumWaiting`/`NumPending` — **no stream
  first-sequence**. Nor is the gap derivable: after a purge, a filtered consumer's counters cannot
  distinguish "no messages matched my filter in the pruned range" from "my messages were pruned"
  (JetStream silently skips deleted messages on resume — the exact silent-skip `gapped()` exists to
  prevent). Recreating the durable at `OptStartSeq` to probe clamping behavior would destroy the ack
  floor the resume depends on, and the grant pins the consumer name anyway.

A third non-option, for completeness: `$JS.API.DIRECT.GET.SYNC.<subject>` (the subject-scoped form)
answers only *last*-by-subject; the general form's body takes arbitrary subjects (same
body-vs-ACL problem as STREAM.INFO), and SYNC has no reason to enable allow_direct.

## 5. Alternatives considered

- **Return `firstSeq` instead of the boolean** (keep the compare client-side, keep the seam method's
  semantics). Genuinely close, and the honest accounting (§10 finding): the side-channel argument
  does **not** separate them — with a client-chosen `cursor`, the boolean is a comparison oracle and
  `FirstSeq` is recoverable by binary search in ≤64 calls, so an *adversary* learns the watermark
  either way. What still separates them: the boolean keeps the gap semantic (and any future safety
  margin) behind the wire, owned by the retention owner; it doesn't hand the number out passively
  (extraction is active, visible, and rate-limitable later); and it commits the API to the minimum.
  What `firstSeq` buys: a richer client log line and client-side margin policy. If Andrew prefers the
  number for diagnosability, flip `PersonalSyncGapResult` to `{FirstSeq uint64}` and keep `gapped()`
  client-side; nothing else in §3 changes. My recommendation is the boolean, on ownership-of-semantics
  grounds — not on side-channel closure, which would be a false claim.
- **Fold the answer into `personal.register`'s response and always re-register on warm boot.** Zero
  new ops/grants — tempting. Rejected: `register` is a mutate op (a per-boot KV write for a read-only
  question), the §2 grounding shows warm boot correctly needs no re-registration (interest persists
  in its own bucket), and overloading "register = interest set" with "also freshness" muddies both.
  The dead-scaffolding test doesn't bite the dedicated op: its consumer (`gapped()`) exists and is
  broken today.
- **Widen the grant with `$JS.API.STREAM.INFO.SYNC`** / **derive from CONSUMER.INFO** — dead on
  pinned-vendor grounds, §4.
- **Do nothing (document "warm resume requires a fresh hydrate")** — i.e. drop the cursor and always
  cold-start. Rejected: it silently converts every device restart into a full bulk re-projection
  (the exact load `hydrate` exists to amortize), and scales per-device-boot with fleet size.

## 6. Reconciliation with the existing mental model

- *Didn't we already handle this?* The grant was **designed** narrow (per-identity-nats-subscribe-acl
  §3.3) and the gap check predates the security turn-on (EDGE.1, trusted posture). Nothing regressed;
  EDGE.3 exposed a call that was always outside the intended grant. The fix direction (control-plane
  RPC, not grant widening) is the same call EDGE.4 made for `sessionkey`.
- *Does this duplicate a parallel in-flight design?* Checked the 📐/🏗️ set — no other design touches
  `gapped()`/`FirstSequence`/the control-plane op table (EDGE.5 W4's remaining tail is the live
  Gate-3 e2e; the RR-1…RR-5 follow-ons are closed). The Loupe lane doesn't reach this seam.
- *New state?* None. One new read-only RPC over state that already exists (stream metadata the
  control host can already read); the client persists nothing new.
- *Retraction/write-guard checks* — n/a: no lens, no projection, no KV write anywhere in the design.

## 7. Risks + residuals

- **Check-then-subscribe race (pre-existing, unchanged).** A purge can land between the syncgap
  answer and the consumer resume; a cursor within seconds of the 24h retention cliff could pass the
  check and still lose a message. Identical to today's STREAM.INFO-based check — the window is
  seconds vs. a 24h MaxAge, and the durable's DeliverAll + LWW-by-revision store bound the blast
  radius to deltas already ≥24h old. Not widened, not worth a margin heuristic now; noted so it isn't
  rediscovered as a regression.
- **Warm boot now depends on the control plane being up — a real availability regression, accepted
  with a mitigation (the adversarial pass corrected an earlier, softer framing).** Today the
  STREAM.INFO answer comes from the NATS *server*: a warm node resumes its durable even while
  Refractor is down, and the durable catches up on its own when Refractor returns. After this change,
  Refractor-down (or the personal lens rule not yet loaded — seam nil, fail-closed) at warm boot
  fails `ensureFresh`, and **neither host retries `Run` today**: `cmd/facet` logs "sync manager
  exited" and leaves sync dead for the session (`cmd/facet/engine.go:147`); `cmd/edge` exits
  (`cmd/edge/main.go:141`). Mitigation, in scope for Inc 2: a bounded retry-with-backoff of the
  syncgap RPC inside `ensureFresh` (covers transient boot-order windows), plus fixing `cmd/facet` to
  restart the sync manager with backoff instead of log-and-abandon — a pre-existing host bug this
  design turns from latent to likely, so it lands with the increment that exposes it. A persistent
  control-plane outage still fails closed (correct: freshness is unverifiable), with local store
  reads unaffected (offline-first posture unchanged).
- **Server cost.** One STREAM.INFO per device warm boot, made by the control host. No caching —
  premature at this fleet size; a cache would only trade staleness into the race above.
- **A deleted-and-recreated SYNC stream reports `FirstSeq` from scratch** (a cursor far ahead of a
  reset stream reads `gapped=false`). Pre-existing and unchanged — today's client-side compare has
  the identical blind spot (a full *purge* is detected: it sets `FirstSeq = LastSeq+1`); stream
  deletion is a bootstrap-destroying operation with bigger consequences than this check. Noted so it
  isn't rediscovered as a regression.
- **A malicious client's `cursor` is self-harm only** (§3.2): the answer gates nothing but the
  caller's own hydrate decision, and the op is rate-bounded by the same callout-issued connection the
  siblings share.

## 8. Test strategy

- **Unit (service):** `syncgap` mirror of `personal_sessionkey_test.go` — §3.4 binding (mismatched
  `identityId` rejected; verified actor bound in), nil-seam fail-closed error, `Gapped` boundary
  vectors (`cursor=0` → true when firstSeq≥1; `cursor=firstSeq-1` → true; `cursor=firstSeq` → false).
- **Unit (client):** `sync_test.go` fake `ControlClient` — warm resume calls syncgap once and skips
  hydrate on `gapped=false`, hydrates on `true`, errors (no resume) on RPC failure **and on a
  decodable response whose `personalSyncGap` is absent** (the nil→false default is the
  silent-data-loss direction, §3.3); cold start makes no syncgap call; transient-failure retry is
  bounded (no unbounded boot hang).
- **Wire round-trip:** extend the RR-4 producer↔consumer round-trip test
  (`internal/edge/sync/producer_roundtrip_test.go`) to `PersonalSyncGapResult` + the `Cursor` field
  (the drift class controlwire's doc-comment names).
- **Grant vectors:** `natsauth` `PermissionsFor` asserts the new subject in the minted allow list; the
  natsperm Edge vector suite adds the **explicit deny** vector for `$JS.API.STREAM.INFO.SYNC` (today
  proven only incidentally by the parity harness) beside the syncgap **allow** vector — the pair pins
  the design's whole point at the ACL layer.
- **Live e2e:** extend the Refractor edge e2e (`edge_manifest_fire1_e2e_test.go` harness): live
  syncgap round-trip through the real control service (cursor beyond head → `false`; after a
  `STREAM.PURGE` of SYNC, stored cursor → `true` → the node re-hydrates and converges). The
  edge-consumer-parity job needs only the *removal* side (shell loses `firstSequence`; its test
  doubles updated).

## 9. Decomposition for the Steward — ONE fire, two increments

Coupled work (grant + consumer must move together per §2's both-halves lesson), so one fire with an
internal order, each increment independently green:

- **Inc 1 — the platform op** (§3.1–§3.2 + grant vectors): wire structs, service handler + seam +
  the `cmd/refractor` `IsPersonalLens` wiring (stream name from `r.Into.Stream`, §3.2), ops-table +
  manifest + `controlRPCs`, unit + round-trip + grant-vector tests. The lockstep is really **six**
  places, not four — the two op-enumerating tests move with it: `internal/controlauth/checker_test.go`
  (~:177, the explicit refractor read/mutate op lists) and `packages/control-authz/package_test.go`
  (~:77, the granted `ctrl.refractor.*` enumeration). Green standalone: the op exists and is proven
  identity-bound; no client calls it yet.
- **Inc 2 — the client swap** (§3.3 + e2e): `gapped()` over syncgap (bounded retry + strict nil-result
  handling), delete `FirstSequence` from the seam + both transports + the shell (full deletion list in
  §3.3), the `cmd/facet` sync-restart fix (§7), update fakes/parity doubles, live e2e, and the doc
  truth-up: `docs/components/edge.md` (~:61 still describes gap detection via the transport's
  `FirstSeq`) + `docs/components/refractor.md`'s control-op list (~:69 — already stale, missing
  `sessionkey`; add both). Green: warm resume works under the real grant end-to-end;
  `rg 'STREAM\.INFO'` under `internal/edge/` returns nothing.

Run `make verify-package-control-authz` (manifest change) + the standard gates; no DDL, no lens, no
contract edit.

## 10. Adversarial pass (run this fire, findings folded in — gate DISCHARGED)

Two passes, 2026-07-17. **Author pass** (during drafting): the request originally carried a `stream`
field — removed (info-oracle risk, §3.2); `Cursor` originally `omitempty` — a zero cursor must
serialize (§3.1); the missing explicit STREAM.INFO deny vector (§8). **Independent adversarial
review** (read-only sub-agent, five lenses: confinement, fail-closed, mechanism truth, availability,
completeness) — 9 findings, all folded: the boolean-vs-firstSeq rationale rewritten honestly (the
boolean is a comparison oracle; §5 / For-Andrew), the nil-`personalSyncGap` fail-closed rule
specified (§3.3, §8), the availability regression named truthfully with the `cmd/facet`
log-and-abandon evidence + mitigation (§7), the stream name sourced from `r.Into.Stream` at the
`IsPersonalLens` wiring site (§3.2), two missed deletion sites (§3.3), the six-place (not four)
lockstep with the two op-enumerating tests (§9), the docs/components truth-up (§9), the explicit
lensID-independence statement (§3.2), and the recreated-stream blind spot (§7). The reviewer verified
every code-line citation and both pinned-vendor claims (v2.14.0 `jetstream_api.go:437`,
`consumer.go:55`) against the module cache. Verdict: sound to flag for ratification with the findings
folded. No open findings; no deferred gate left for the Steward.
