# Story 10.2: Nudge wired into the Actuator + idempotency proof

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform developer,
I want the Strategist's external playbooks to drive nudges through the live Actuator dispatch path, with a retry-safety test,
so that the FR58 idempotency guarantee is proven end-to-end (a failed/retried external call cannot duplicate an action, and a crash between claim and resolve leaves the operation recoverable without a duplicate).

## Acceptance Criteria

1. **Given** Story 10.1's Two-Phase Nudge mechanism (`internal/weaver/nudge/`: `Adapter`, `Registry`,
   `ClaimStore`, `Nudger.Run`/`Nudger.Recover`) and the §10.8 `nudge` action with `GapAction.Adapter`/
   `GapAction.Operation`/`GapAction.Subject`/`GapAction.Params`
   **When** the Strategist plans a `nudge` gap and the Actuator fires it
   **Then** the `buildPlan` `actionNudge` **stub** (`internal/weaver/strategist.go:182` —
   `&planError{kind: errConfig, msg: "nudge is not yet implemented"}`) is **removed** and replaced
   with a real plan that resolves the adapter name + resolve-operation type + subject + params (templated
   from the row exactly like the other actions via `resolveStringParam`/`resolveParam`), and the live
   lane-1 dispatch path drives the 10.1 `Nudger` rather than a plain `actuator.submit`.
   **And** a `nudge` gap no longer produces a `PlaybookConfigError` Health issue / Ack-skip — it
   dispatches.

2. **Given** the §10.3 invariant that a nudge's `claimId` is minted atomically with the `weaver-state`
   mark CAS-create (`markStore.createNudge`, already shipped in 10.1)
   **When** the Actuator fires a `nudge` episode for the first time
   **Then** the dispatch path: (a) CAS-creates the mark via **`createNudge`** (not the plain `create`),
   minting the `claimId` into the mark in the single `KVCreateWithTTL`; (b) on a fresh create-win,
   runs `Nudger.Run` with that minted `claimId` — which writes the claim to `weaver-claims.<claimId>`
   with `state="claimed"` **before** the external call (NFR-S11), advances to `executing`, calls the
   gap's mapped adapter on `idempotencyKey = claimId`, then submits a **resolve op** through the
   `actuator.submit` seam (a normal fire-and-forget publish to `ops.<lane>` carrying the `claimId` as a
   payload reference field) and advances the claim to `state="resolved"` recording `resolvedAt` +
   `resolveRef`.
   **And** the non-nudge actions (`triggerLoom`/`assignTask`/`directOp`) continue to use the plain
   `markStore.create` (no `claimId`) and the plain `fire`/`actuator.submit` — only the `nudge` action
   diverges to `createNudge` + the protocol.

3. **Given** the 10.1 handoff item "the resolve op MUST use a deterministic `requestId` derived from the
   `claimId` so a duplicate resolve collapses on the Contract #4 idempotency tracker"
   **When** the protocol submits the resolve op (the `ResolveFunc` seam)
   **Then** the resolve op's `requestId` is **deterministic, derived from the `claimId`** (a new
   `deriveID` namespace, e.g. `deriveResolveRequestID(claimId)` — NOT a fresh `substrate.NewNanoID()`
   per call, and NOT `deriveEpisodeRequestID` which is keyed on the mark revision), so a redelivery /
   recovery that re-submits the resolve for the same `claimId` re-derives the SAME `requestId` and
   collapses on the Contract #4 `vtx.op.<requestId>` tracker — exactly one resolve mutation in Core KV.
   The `resolveRef` recorded on the claim is that `requestId`.

4. **Given** the reconciler sweep's expired-lease reclaim path (`reconciler.go` `reclaim`, which today
   re-arms the mark via `replace` and calls `e.fire` directly)
   **When** the sweep reclaims an expired **nudge** mark whose gap is still violating
   **Then** the reclaim (a) re-arms the mark via **`replaceCarryingClaim`** carrying the EXISTING mark's
   `claimId` forward (never `replace`, which would write an empty `claimId`; never minting a new one —
   the 10.1 corrupt-empty-`claimId` guard already deletes+alerts a nudge mark with no `claimId` and that
   guard must remain intact and ordered BEFORE the reclaim), and (b) drives **`Nudger.Recover`** (not
   `e.fire`) with a real `ResolveProbe` that reads Core KV (the authoritative business outcome) for an
   already-landed resolve — so a claim whose resolve already committed is advanced to `resolved` without
   a second external side-effect, and a `claimed`/`executing`/`failed` claim re-attempts on the SAME
   `idempotencyKey` (adapter de-dups). The `ResolveProbe` reads the Core KV `vtx.op.<resolveRequestId>`
   tracker (the deterministic resolve `requestId` from AC #3 is the probe key — read-before-act,
   mirroring the §10.6 tracker-GET precedent).

5. **Given** a `FakeStripe` reference adapter (mirroring `FakeBackgroundCheck`)
   **When** the package is built
   **Then** `internal/weaver/nudge/` defines `FakeStripe` — deterministic, in-memory, idempotent on
   the `idempotencyKey` (records the keys it has charged; a repeat key returns the SAME result with NO
   second side-effect; exposes a `SideEffects(key)` count like `FakeBackgroundCheck`), with a
   configurable failure mode (e.g. `FailNext()` / a fail-once toggle) so the idempotency-proof test can
   drive a FAILED external call. No network, no real I/O. It imports only `internal/substrate/*` (the
   module-boundary rule — assert via the existing `boundary_test.go`).

6. **Given** the FR58 idempotency proof (the heart of this story)
   **When** an end-to-end **idempotency-proof test** drives a FAILED then RETRIED external call (FakeStripe
   configured to fail the first attempt, or the lane-1 dispatch redelivered with `NumDelivered > 1`)
   **Then** it asserts NO duplicate action/charge — `FakeStripe.SideEffects(claimId)` is **at most 1**
   across the failure + retry, the claim is the idempotency boundary (records exactly one side-effect),
   and exactly one resolve op lands in Core KV (the deterministic `requestId` collapses any duplicate
   resolve on the Contract #4 tracker).

7. **Given** the crash-between-claim-and-resolve scenario (NFR-S11 "records a visible claim state before
   executing any external call and does not re-initiate a claimed operation")
   **When** an end-to-end **crash test** simulates a crash AFTER the claim is written (and/or after
   execute) but BEFORE the resolve op is submitted — e.g. the `ResolveFunc` errors / the process is
   interrupted between execute and resolve
   **Then** it asserts (a) the claim is visible in `weaver-claims.<claimId>` in `claimed`/`executing`
   state, (b) the operation is NOT re-initiated (no duplicate side-effect) on a plain lane-1 redelivery
   over the live mark (the in-flight skip / the create-semantic `Write` `ErrClaimExists` routing), and
   (c) when the reconciler reclaims the expired lease, `Nudger.Recover` reuses the SAME `claimId`, the
   `ResolveProbe` prevents a duplicate resolve (or the same deterministic `requestId` collapses it), and
   the claim converges to `resolved` with `FakeStripe.SideEffects(claimId)` still **at most 1**.

8. **Given** `docs/components/weaver.md` is design-first ("update in the same commit as the code")
   **When** the work is declared done
   **Then** the `nudge` row in the **Implementation status — Actions** table flips from "framework +
   protocol mechanics shipped (10.1); Actuator wiring + FakeStripe idempotency proof is 10.2" to
   **shipped** (the loud-stub `planError` is gone; the live dispatch path drives the protocol; `FakeStripe`
   reference adapter added; end-to-end idempotency + crash proofs passing), and the In/Out `weaver-claims`
   row + the Two-Phase Nudge "Build status (10.1 / 10.2 split)" note are updated to "wired end-to-end".
   The frozen `docs/contracts/*` are NOT edited.

9. **Given** the verification gates (CLAUDE.md house rules)
   **When** the work is declared done
   **Then** `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`,
   `make test-bypass` (Gate 2, all BLOCKED), `make test-capability-adversarial` (Gate 3, all DEFENDED),
   and `go test ./internal/weaver/... ./internal/weaver/nudge/...` all pass.

## Tasks / Subtasks

- [x] **Task 1 — Replace the `actionNudge` stub in `buildPlan`** (AC: #1, #2)
  - [x] In `internal/weaver/strategist.go`, remove the `case actionNudge:` `planError`
        (`"nudge is not yet implemented"`, line ~179-182) and the now-stale doc-comment sentence on
        `buildPlan` ("The nudge action is not yet implemented…", line ~76-77 — replace with a sentence
        describing the live behavior; **no history/changelog comment** per CLAUDE.md).
  - [x] Resolve the nudge action's fields with the existing helpers: `adapter` (literal from
        `ga.Adapter` — adapter names are config, not templated from the row; reject blank as `errConfig`,
        mirroring `directOp`'s operation guard), `operation` (`ga.Operation`, the resolve op type — a
        literal, reject a `row.` prefix as `errConfig` like `directOp`), `subject`
        (`resolveStringParam("subject", ga.Subject, row)`), and `params`
        (`resolveParam` over `ga.Params`, like `directOp`). A missing/blank adapter or operation is a
        config error (`errConfig`); a templated-but-null `subject`/param is a data error (`errData`) —
        same routing the other actions use.
  - [x] Decide the `plan` shape for nudge. The existing `plan` struct carries
        `operationType`/`authTarget`/`payload(markRevision)`, which fits the plain-submit actions. The
        nudge needs the resolved adapter name + operation + subject + params to reach the protocol at
        fire time. **Recommended:** add a nudge-specific carrier to `plan` (e.g. a `*nudgePlan` field
        holding `adapter, operation, subject string` + `params map[string]string`, nil for non-nudge
        actions) so `fireEpisode`/`fire` can branch on `pl.nudge != nil`. Keep the change minimal and
        do NOT regress the other actions' `plan` usage. (The `payload`/`operationType`/`authTarget`
        fields stay the plain-submit path; nudge ignores them.) Document the chosen shape on the struct.

- [x] **Task 2 — `FakeStripe` reference adapter** (AC: #5)
  - [x] Add `internal/weaver/nudge/fake_stripe.go` mirroring `fake_background_check.go`:
        deterministic, in-memory, idempotent on `req.IdempotencyKey` (a repeat key returns the first
        call's `Result` with NO second side-effect; per-key side-effect counter), `NewFakeStripe()`
        constructor, `SideEffects(idempotencyKey string) int` accessor. The `Result.Detail` is an
        opaque confirmation string (e.g. `"charge confirmed for <subject>"`).
  - [x] Add a **configurable failure mode** for the idempotency proof: e.g. a `FailNext()` /
        `FailUntil(n)` toggle (mutex-guarded) so the first Execute on a key returns an error (the claim
        lands `failed`) and a later retry on the SAME key succeeds — and crucially the failed attempt
        records NO side-effect (a Stripe charge that errors did not charge), so the eventual single
        success is the only side-effect. Document the failure semantics on the type.
  - [x] Confirm `internal/weaver/nudge` still imports only `internal/substrate/*` (run
        `internal/weaver/nudge/boundary_test.go`; extend its assertion if needed). `FakeStripe` must
        not pull in any new dependency.

- [x] **Task 3 — Wire the live dispatch path (claim→execute→resolve) into the Actuator** (AC: #1, #2, #3)
  - [x] In `internal/weaver/evaluator.go`, the nudge path diverges at the mark-create and at fire.
        `fireEpisode` (line ~214) takes the `action` string — branch on `action == actionNudge`:
    - [x] **Fresh create:** call `e.marks.createNudge(...)` instead of `e.marks.create(...)` so the
          mark carries a minted `claimId`. On a create-win, capture the returned `claimId` and call a new
          `e.fireNudge(ctx, ..., claimID, markRev, pl)` instead of `e.fire`. On `exists` (the in-flight
          branch) follow the same redelivered/anti-storm disposition the other actions use — but a
          redelivery of an in-flight nudge must read the mark's existing `claimId` (via `marks.get`) and
          route to `Nudger.Recover` (read-before-act), NOT `Nudger.Run` (which would land
          `ErrClaimExists` because the claim already exists). **Run on the create-win path; Recover on the
          redelivery-over-live-mark path** — this is the §10.3 single-writer discipline the 10.1
          `ClaimStore.Write` create-semantics enforce.
    - [x] **`fireNudge`:** build the `nudge.Dispatch{ClaimID, Adapter, Operation, Subject, Params}` from
          the resolved nudge plan, then call `e.nudger.Run(ctx, dispatch, e.resolveFunc(...))`. Map the
          outcome to a `substrate.Decision`: a successful resolve → `Ack`; an adapter hard-fail (claim
          `failed`) → surface a Health issue and `Ack` (re-attempt is the reconciler's job on the lease —
          do NOT hot-loop a hard-fail via Nak; the claim is durable and re-drivable); a resolve-submit
          failure (claim left `executing`) → `Nak`/`NakWithDelay` so the redelivery re-drives via Recover
          (or the sweep reclaims). Pick the disposition that keeps the gap converging without a hot loop —
          mirror `fire`'s posture and document it.
  - [x] **`resolveFunc` seam (AC #3):** implement the `nudge.ResolveFunc` the engine passes to the
        Nudger. It (a) derives the deterministic resolve `requestId` from the `claimId` (new
        `deriveResolveRequestID(claimID)` in `actuator.go` using `deriveID` with a fresh `"resolve:"`
        namespace), (b) builds the resolve op payload carrying the `claimId` as a reference field
        (e.g. `{"claimId": claimID, "result": result.Detail, ...}` — plus any op-type-specific fields the
        resolve operation needs; keep it minimal and document the payload shape), and (c) calls
        `e.act.submit(ctx, resolveRequestID, op.Operation, payload, authTarget)`, returning the
        `resolveRequestID` as the `resolveRef`. The resolve op's `operationType` is the nudge action's
        `Operation` (the §10.8 resolve op type). The `authTarget` is the nudge's subject (or the §10.8
        target) — confirm against the action table; document the choice.
  - [x] Confirm the engine fields constructed in 10.1 (`e.nudger`, `e.claims`, `e.adapters`) are now
        READ on this live path (10.1 left them constructed-but-unread, which was correct for the split).
        The adapter set must be REGISTERED somewhere — see Task 5 (adapter registration wiring).

- [x] **Task 4 — Wire recovery into the reconciler sweep** (AC: #4, #7)
  - [x] In `internal/weaver/reconciler.go` `reclaim`, the nudge mark must NOT take the plain
        `e.marks.replace` + `e.fire` path. After the existing corrupt-empty-`claimId` guard
        (`ga.Action == actionNudge && rec.ClaimID == ""` → `deleteCorrupt`, KEEP intact and ordered
        first), branch the reclaim for `ga.Action == actionNudge`:
    - [x] Re-arm via `e.marks.replaceCarryingClaim(...)` carrying `rec.ClaimID` forward (the existing
          mark's `claimId`), conditioned on the revision read this pass — same revision-condition / conflict
          handling as the current `replace` call.
    - [x] On a successful re-arm, drive `e.nudger.Recover(ctx, dispatch, probe, e.resolveFunc(...))`
          (with the dispatch rebuilt from the reclaim's resolved plan + `rec.ClaimID`) instead of
          `e.fire`. A non-Ack disposition leaves the fresh mark's lease to bound the retry (mirror the
          existing reclaim Warn-log posture).
  - [x] **`ResolveProbe` (AC #4):** implement the `nudge.ResolveProbe` the engine passes to `Recover`.
        It reads Core KV for the already-landed resolve: GET the `vtx.op.<resolveRequestId>` tracker
        (the deterministic resolve `requestId` derived from the `claimId`, AC #3) — present ⇒
        `landed=true, resolveRef=<that requestId>`; absent ⇒ `landed=false`. This is the read half of
        read-before-act: it asks the authoritative Core KV whether the resolve already committed before
        any re-execute. Use the same `substrate.Conn.KVGet` against the Core KV bucket the engine already
        holds (`cfg.CoreKVBucket`); a `substrate.ErrKeyNotFound` ⇒ not landed.
        **Confirm the exact tracker key shape** against Contract #4 / the §10.6 read-before-act precedent
        Loom uses (op tracker = `vtx.op.<requestId>` per Contract #1/#4 — see Open Questions) and
        document it.
  - [x] The corrupt-empty-`claimId` guard and the non-nudge reclaim legs (lease reclaim, orphan delete,
        gapClosed clear, warm-up gating, revision-conditioned deletes) stay byte-for-byte intact — the
        nudge-recovery wiring is ADDITIVE, only the nudge action's reclaim diverges.

- [x] **Task 5 — Adapter registration wiring** (AC: #1, #5)
  - [x] The `nudge.Registry` (`e.adapters`) is constructed empty in `NewEngine`. The engine must
        register the reference adapters so a `nudge` gap's `adapter` name resolves at dispatch
        (`Registry.Lookup`). **Recommended:** add an exported seam on the engine (e.g.
        `Engine.RegisterAdapter(name string, a nudge.Adapter) error` delegating to
        `e.adapters.Register`) so `cmd/weaver` / tests register `FakeStripe`/`FakeBackgroundCheck` by the
        names the §10.8 playbooks use, BEFORE `Start`. Do NOT hard-code adapter registration inside the
        engine (the engine is adapter-agnostic; the framework is engine, adapters are config — keep that
        boundary). A nudge gap naming an unregistered adapter is already surfaced by `Nudger.Run`'s
        `"no adapter registered"` config error → route it to a Health issue (errConfig posture), never a
        silent skip.
  - [x] If `cmd/weaver` is in scope, register the Phase-2 reference adapters there (mocked — demo).
        Otherwise document the registration seam and exercise it from the test. (Confirm whether
        `cmd/weaver` should wire the demo adapters now or whether that lands with Epic 11 `lease-signing`
        — see Open Questions.)

- [x] **Task 6 — Tests** (AC: #1–#7, #9)
  - [x] **`nudge` package unit tests:** `FakeStripe` returns the same result + no second side-effect for
        a repeated `idempotencyKey`; `FakeStripe` failure mode fails the first Execute (claim → `failed`,
        zero side-effect) and a retry on the same key succeeds with exactly one side-effect.
  - [x] **`internal/weaver` dispatch test (the FR58 idempotency proof, AC #6):** drive a `nudge` gap end
        to end through the live lane-1 dispatch path (a §10.2 fixture row + a meta.weaverTarget playbook
        naming the `nudge` action + a registered `FakeStripe`). Configure `FakeStripe` to fail the first
        attempt, then redeliver / reclaim; assert `FakeStripe.SideEffects(claimId) <= 1` and exactly one
        resolve op (the deterministic resolve `requestId` collapses any duplicate). Assert the claim
        record advances `claimed → executing → (failed) → executing → resolved` with `resolveRef` set.
  - [x] **Crash-between-claim-and-resolve test (AC #7):** simulate a crash after the claim write (and/or
        after execute) but before resolve — e.g. a `ResolveFunc` that errors once (leaving the claim
        `executing`), then assert: the claim is visible in `weaver-claims` (`claimed`/`executing`); a plain
        lane-1 redelivery does NOT re-initiate (no second side-effect — the in-flight mark / the
        create-semantic `Write` `ErrClaimExists` routes to Recover, and the adapter de-dups); the
        reconciler reclaim drives `Recover` reusing the same `claimId`, the `ResolveProbe`/deterministic
        `requestId` prevents a duplicate resolve, and the claim converges to `resolved` with side-effects
        still ≤ 1.
  - [x] **Deterministic-resolve-requestId test (AC #3):** assert `deriveResolveRequestID(claimID)` is
        stable across calls for the same `claimId` and disjoint from `deriveEpisodeRequestID` /
        `deriveEpisodeTaskID` / `deriveTimerRequestID` (namespace separation, like the existing
        derive-ID tests).
  - [x] **Regression guard:** a `nudge` gap no longer raises `PlaybookConfigError`; the
        `triggerLoom`/`assignTask`/`directOp` dispatch paths are unchanged (a non-nudge mark still carries
        no `claimId`; `create`/`replace`/`fire` untouched). Run the full `go test ./internal/weaver/...`
        (incl. the E2E + reconciler internal tests) to prove no 9.x / 10.1 regression.
  - [x] Run all verification gates (AC #9).

- [x] **Task 7 — Documentation** (in the same commit as the code, AC: #8)
  - [x] Update `docs/components/weaver.md`:
    - [x] **Implementation status — Actions row** (line ~390): flip `nudge` from the 10.1/10.2-split
          wording to **shipped** (live dispatch via `createNudge` + `Nudger.Run`; recovery via the sweep +
          `Nudger.Recover` with a Core KV `ResolveProbe`; `FakeStripe` reference adapter; deterministic
          resolve `requestId`; end-to-end idempotency + crash proofs passing). The loud-stub `planError`
          is gone.
    - [x] **In/Out `weaver-claims` row** (line ~354): "Claim store + record shape built (10.1); live
          Actuator wiring is 10.2" → "wired end-to-end (10.2)".
    - [x] **Two-Phase Nudge "Build status (10.1 / 10.2 split)" note** (line ~210-218): update to reflect
          10.2 shipped the end-to-end wiring (`buildPlan` drives the protocol via the live lane-1 path,
          `FakeStripe`, crash-between-claim-and-resolve proof).
    - [x] If the Reconciler-sweep row (line ~388) or the Dispatch-OCC `claimId` row (line ~387) needs a
          note that nudge reclaim now drives `Recover`, add it. Do NOT edit `docs/contracts/*`.

## Dev Notes

### What this story IS and IS NOT (Story Boundary — read first)

Story 10.1 (commit `e615a59`, CI green) shipped the **mechanism** in isolation: the
`internal/weaver/nudge/` framework (`Adapter`, `Registry`, `FakeBackgroundCheck`, `ClaimStore` with
create-semantic `Write`→`ErrClaimExists`, `Advance`/`reopen`/`Resolve`, and
`Nudger.Run`/`Nudger.Recover`), the claim-minting mark-create (`markStore.createNudge`) and the
claimId-carrying reclaim helper (`markStore.replaceCarryingClaim`), the corrupt-empty-`claimId`
reconciler guard, and the engine `Config` + constructed-but-unread `e.nudger`/`e.claims`/`e.adapters`
fields. **10.1 deliberately left `buildPlan`'s `actionNudge` as the loud `planError` and did NOT wire
the protocol into the live dispatch path** — that is exactly this story's job.

**Story 10.2 (this story) owns the end-to-end wiring**: remove the `actionNudge` stub, drive the live
lane-1 dispatch through `createNudge` + `Nudger.Run`, derive the deterministic resolve `requestId` from
the `claimId`, wire the reconciler sweep's nudge reclaim through `replaceCarryingClaim` +
`Nudger.Recover` with a Core KV `ResolveProbe`, add the `FakeStripe` reference adapter, and prove FR58
end-to-end with the idempotency-proof + crash-between-claim-and-resolve tests. This is the FINAL
Phase-2 Weaver story (Epic 10 closes here).

**Do not rebuild 10.1's mechanism.** `Nudger.Run`/`Recover`, `ClaimStore`, `createNudge`,
`replaceCarryingClaim`, and the corrupt-claim guard already exist and are unit-proven — wire them, do
not reimplement them. The seams 10.1 left for you:
- `Nudger.Run(ctx, Dispatch, ResolveFunc) (*Claim, error)` — the fresh dispatch path.
- `Nudger.Recover(ctx, Dispatch, ResolveProbe, ResolveFunc) (*Claim, error)` — the recovery path
  (requires a non-nil probe; short-circuits only on `resolved`; re-attempts `failed`/`executing`).
- `markStore.createNudge(...) (claimID, revision, exists, err)` — mints the `claimId` atomically.
- `markStore.replaceCarryingClaim(...claimID..., expectedRevision)` — reclaim reusing the `claimId`.
- `ResolveFunc(ctx, claimID, Result) (resolveRef, err)` — YOU supply the actuator-backed submit.
- `ResolveProbe(ctx, claimID) (resolveRef, landed, err)` — YOU supply the Core KV read.

### Relevant architecture patterns and constraints

- **Two-Phase Nudge protocol (the spec):** Claim → write `weaver-claims.<claimId>` (direct KV, intent
  BEFORE the call, NFR-S11) → Execute → call the (mocked) adapter on `idempotencyKey=claimId` → Resolve
  → submit a normal op via the Processor carrying the `claimId`, `state=resolved`. Recovery is
  read-before-act (reuse `claimId`, check Core KV for an already-landed resolve before re-executing).
  [Source: docs/components/weaver.md#Two-Phase Nudge; docs/contracts/10-orchestration-surfaces.md#10.3 weaver-claims]
- **Arch Item 3 / FR58 / NFR-S11:** "External operations … are idempotent; a failed or retried external
  call cannot result in a duplicate charge or duplicated action. The orchestration engine records a
  visible claim state before executing any external call and does not re-initiate a claimed operation."
  Claims live in Weaver operational KV (`weaver-claims`), NOT Core KV; the resolve mutation goes through
  the Processor into Core KV carrying the `claim-id`; the resolve mutation in Core KV references the
  claim ID (the audit join). [Source: _bmad-output/planning-artifacts/lattice-architecture.md#Item 3 (lines ~954-969)]
- **§10.3 `weaver-claims` record (frozen, build EXACTLY to it — already modeled in `nudge.Claim`):**
  `{ claimId, adapter, operation, subject, params, idempotencyKey(=claimId),
  state(claimed→executing→resolved|failed), claimedAt, resolvedAt?, resolveRef? }`; `resolveRef =
  requestId / op key of the resolve mutation in Core KV`. External idempotency is the `idempotencyKey`
  the adapter dedups on — NOT a CAS on the claim key (the `weaver-state` mark already serialized the
  dispatch and carries the `claimId`, so the claim has a single writer).
  [Source: docs/contracts/10-orchestration-surfaces.md#10.3 weaver-claims (lines ~280-304)]
- **§10.3 `weaver-state` `claimId` invariant:** minted atomically with the CAS-create (`createNudge`);
  an empty `claimId` on a nudge mark is corrupt → alert + NEVER mint a fresh id (a fresh id = a second
  `idempotencyKey` = a duplicate external call); a nudge reclaim carries the existing `claimId` forward.
  [Source: docs/contracts/10-orchestration-surfaces.md#10.3 weaver-state (lines ~267-272)]
- **Deterministic requestId = Contract #4 idempotency (the 10.1 handoff):** the resolve op MUST use a
  `requestId` DERIVED from the `claimId` so a duplicate resolve collapses on the
  `vtx.op.<requestId>` tracker. This mirrors the existing `deriveEpisodeRequestID` discipline
  (`actuator.go:111`) but is keyed on the `claimId`, not the mark revision — because a recovery
  re-submit happens under a DIFFERENT (reclaimed) mark revision yet must collapse to the SAME resolve op.
  Add a `deriveResolveRequestID(claimID)` using the shared `deriveID` with a new `"resolve:"` namespace.
  [Source: _bmad-output/implementation-artifacts/10-1-weaver-two-phase-nudge.md#10.2 handoff / deferred;
  internal/weaver/actuator.go (deriveID/deriveEpisodeRequestID)]
- **Module boundary (hard rule):** `internal/weaver/nudge` imports ONLY `internal/substrate/*` (assert
  via `boundary_test.go`). The resolve goes through the Actuator's fire-and-forget publish to
  `ops.<lane>` — never a request-reply, never a raw NATS handle in the engine. The engine (not the nudge
  package) holds the Actuator and the Core KV `Conn`; it supplies them to the protocol via the
  `ResolveFunc`/`ResolveProbe` callbacks. [Source: docs/components/weaver.md#Principles; internal/weaver/actuator.go]
- **P2 / P1:** the Processor is the sole Core KV writer (the resolve op is a normal submit);
  claims/state are operational KV (P1). [Source: docs/components/weaver.md#Principles]
- **CLAUDE.md conventions:** no history/changelog comments in code (delete the stale "not yet
  implemented" comment, don't annotate the change); frozen contracts under `docs/contracts/*` are
  build-to, never edit; new docs go in `/docs`; sub-agents never commit/push/branch (the lead commits to
  `main`). Link-naming / key-shape conventions apply to any new KV key (Contract #1).

### Source tree components to touch

- `internal/weaver/strategist.go` — REMOVE the `actionNudge` `planError` (line ~179-182) and the stale
  `buildPlan` doc-comment sentence (line ~76-77); add the real nudge plan branch (resolve
  adapter/operation/subject/params). Likely add a `nudgePlan` carrier (see Task 1) — decide the `plan`
  shape and document it.
- `internal/weaver/evaluator.go` — branch `fireEpisode` (line ~214) on `actionNudge`: `createNudge`
  instead of `create`, `Recover` on the redelivery-over-live-mark branch, and a new `fireNudge` that
  builds the `nudge.Dispatch` and calls `Nudger.Run` with the engine's `resolveFunc`. Map the protocol
  outcome to a `substrate.Decision`.
- `internal/weaver/actuator.go` — add `deriveResolveRequestID(claimID)` (new `"resolve:"` `deriveID`
  namespace). The resolve op submit reuses the existing `actuator.submit` (no new publish primitive).
- `internal/weaver/reconciler.go` — in `reclaim` (line ~232), branch the nudge action: re-arm via
  `replaceCarryingClaim` (carry `rec.ClaimID`), drive `Nudger.Recover` (not `e.fire`) with the engine's
  `ResolveProbe`+`resolveFunc`. KEEP the corrupt-empty-`claimId` guard (line ~290) intact and ordered
  before the reclaim. The probe + the resolve-func are engine methods (they hold the `Conn` + the
  Actuator).
- `internal/weaver/engine.go` — add the `resolveFunc`/`resolveProbe` engine methods (or a small
  per-dispatch closure factory); add `Engine.RegisterAdapter` (delegating to `e.adapters.Register`) as
  the adapter-registration seam. `e.nudger`/`e.claims`/`e.adapters` are already constructed (10.1) —
  this story makes them READ on the live path.
- **NEW** `internal/weaver/nudge/fake_stripe.go` — `FakeStripe` reference adapter (mirror
  `fake_background_check.go`; add a configurable fail-once mode). Substrate-only.
- `internal/weaver/nudge/boundary_test.go` — confirm/extend the substrate-only assertion covers the new
  file (it should automatically — it asserts the whole subpackage's deps).
- `cmd/weaver/*` (IF in scope) — register the Phase-2 reference adapters before `Start` (mocked demo).
  See Open Questions on whether this lands now or with Epic 11.
- `docs/components/weaver.md` — Implementation-status Actions row + In/Out `weaver-claims` row +
  Two-Phase Nudge build-status note (Task 7). Editable (design-first). `docs/contracts/*` is NOT.

### Key integration facts (verified against current code)

- **`buildPlan` switch** (`strategist.go:81`): `actionTriggerLoom`/`actionAssignTask`/`actionDirectOp`
  each build a `plan` with `operationType`/`authTarget`/`payload`; `actionNudge` (line ~179) is the
  stub to replace. `actionNudge = "nudge"` const is line 14; `GapAction.Adapter`/`.Operation`/`.Subject`/
  `.Params` (registry.go:39-48) carry the nudge fields. Use `resolveStringParam`/`resolveParam` for
  templating (strategist.go:193,211).
- **`fireEpisode`/`fire`** (`evaluator.go:214,248`): fresh delivery → `marks.create` CAS → `fire`
  (`actuator.submit` with `deriveEpisodeRequestID`). In-flight + redelivered → re-`fire` the same
  episode. The nudge path diverges here (createNudge + protocol). `dispatchGap` (line ~133) passes
  `ga.Action` into `fireEpisode` already — the action string is in hand.
- **`actuator.submit(ctx, requestID, operationType, payload, authTarget)`** (`actuator.go:57`): ONE
  fire-and-forget publish to `ops.<lane>`. The resolve op is a normal submit with the deterministic
  resolve `requestId`. `deriveID(namespace, seed, revision)` (line ~136) is the shared derivation;
  `deriveEpisodeRequestID` (line ~111) is the precedent for the new `deriveResolveRequestID`.
- **`Nudger.Run`** (`nudge/protocol.go:81`): Lookup adapter → `claims.Write` (create-semantic,
  `ErrClaimExists` if live) → `Advance(executing)` → `execute` (panic-contained) → on success
  `resolve` (calls the `ResolveFunc`, then `claims.Resolve(resolveRef)`); on adapter fail → `failed`.
  **A redelivery that reaches `Run` over an existing claim returns `ErrClaimExists` — the caller MUST
  route an in-flight nudge to `Recover` instead.** This is why `fireEpisode`'s in-flight branch must
  call `Recover`, not `Run`.
- **`Nudger.Recover`** (`nudge/protocol.go:131`): requires a non-nil `probe`; rejects a blank `claimId`;
  short-circuits ONLY on `state=resolved`; otherwise probes Core KV (landed → `claims.Resolve`), else
  re-executes on the same `claimId` (reopen → execute → resolve). `failed`/`executing` re-attempt.
- **`ClaimStore`** (`nudge/claims.go`): `Write` (create-on-absent, `ErrClaimExists`), `Get`, `Advance`,
  `reopen` (failed→executing), `Resolve` (records `resolveRef`, idempotent on same ref). `Claim` is the
  frozen §10.3 shape. Built; do not modify unless a gap surfaces (then raise a
  `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md`, never edit the frozen contract).
- **`markStore.createNudge`** (`state.go:113`): mints `claimId` into the mark in the single
  `KVCreateWithTTL`; returns `(claimID, revision, exists, err)`. **`markStore.replaceCarryingClaim`**
  (`state.go:189`): reclaim carrying a supplied `claimId` forward, revision-conditioned. `replace`
  (state.go:177) delegates to it with an empty claimId (the non-nudge reclaim).
- **`reconciler.go` `reclaim`** (line ~232): plans the gap, then `e.marks.replace` + `e.fire`. The
  corrupt-empty-`claimId` guard is line ~290 (`ga.Action == actionNudge && rec.ClaimID == ""` →
  `deleteCorrupt`). Branch the nudge reclaim AFTER that guard.
- **Engine** (`engine.go:287` `NewEngine`): `adapters := nudge.NewRegistry()`, `claims := nudge.NewClaimStore(...)`,
  `nudger := nudge.NewNudger(claims, adapters)` — all constructed, all currently unread on a live path.
  `cfg.CoreKVBucket` (the Core KV bucket the `ResolveProbe` reads), `cfg.ClaimRetention` (default 90d),
  `cfg.WeaverClaimsBucket` (default `weaver-claims`) are wired.
- **`substrate`:** `Conn.KVGet`/`KVCreateWithTTL`/`KVPutWithTTL` (kv.go), `NewNanoID`/`Alphabet`/
  `NanoIDLength` (nanoid.go), `ErrKeyNotFound`/`ErrRevisionConflict`, `FormatTimestamp`. NATS per-key
  TTL floor is 1s.

### Regression guardrails (do not break 9.x / 10.1)

- **Non-nudge dispatch is byte-for-byte unchanged.** `triggerLoom`/`assignTask`/`directOp` keep
  `markStore.create` (no `claimId`) and the plain `fire`/`actuator.submit`. Only `actionNudge` diverges
  to `createNudge` + the protocol. A non-nudge mark must NEVER carry a `claimId` (the `omitempty` keeps
  the JSON identical — the 10.1 `TestNonNudgeCreate_CarriesNoClaimID` must stay green).
- **The reconciler sweep's existing legs** (level-clearing, orphan reclaim, expired-lease reclaim for
  non-nudge, corrupt-mark delete, the corrupt-empty-`claimId` guard, warm-up gating,
  revision-conditioned deletes/replaces) keep their current behavior — the nudge-recovery wiring is
  ADDITIVE.
- **10.1's mechanism is frozen-by-test.** `Nudger.Run`/`Recover`, `ClaimStore`, `createNudge`,
  `replaceCarryingClaim` have unit tests (`nudge/protocol_test.go`, `state_nudge_internal_test.go`,
  `reconciler_nudge_internal_test.go`). Wiring them must not require changing their signatures; if it
  does, that is a design smell — flag it.
- **Module boundary:** `internal/weaver/nudge` stays substrate-only. The `ResolveFunc`/`ResolveProbe`
  callbacks are the seam that keeps the Actuator + Core KV `Conn` on the engine side, out of the
  protocol package.

### Testing standards summary

- Go table-driven tests; package-internal tests use `_internal_test.go` + `package weaver`
  (see `state_nudge_internal_test.go`, `reconciler_nudge_internal_test.go`); the `nudge` package gets
  `fake_stripe_test.go` (`package nudge`). The end-to-end idempotency + crash proofs likely live as a
  weaver-package test (internal or the build-tagged `weaver_e2e_test.go` style — match the existing E2E
  harness if it provides a real NATS/substrate fixture; otherwise a focused internal test exercising the
  dispatch path with a fake `Conn`/registered `FakeStripe`).
- **The idempotency assertion is the heart of FR58:** `FakeStripe.SideEffects(claimId) <= 1` across a
  failed-then-retried call AND across a crash-between-claim-and-resolve recovery; exactly one resolve op
  (the deterministic `requestId` collapses duplicates).
- **The crash proof asserts NFR-S11 literally:** claim visible in `weaver-claims` BEFORE/at the crash
  point; no re-initiation on plain redelivery; recovery converges via `Recover` + probe with no
  duplicate.
- Gates (CLAUDE.md): `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`,
  `make test-bypass` (Gate 2, all BLOCKED), `make test-capability-adversarial` (Gate 3, all DEFENDED),
  `go test ./internal/weaver/... ./internal/weaver/nudge/...`.

### Project Structure Notes

- `FakeStripe` goes in `internal/weaver/nudge/fake_stripe.go` alongside `fake_background_check.go` — the
  reference-adapter home (`docs/components/weaver.md`: "the framework is engine, reference adapters prove
  it"). Substrate-only.
- The `ResolveFunc`/`ResolveProbe` implementations are ENGINE methods (`internal/weaver`), not nudge-package
  code — they hold the Actuator and the Core KV `Conn`. This preserves the 10.1 seam (the protocol package
  stays substrate-only; the engine supplies the live submit + the Core KV read).
- `deriveResolveRequestID` lives in `actuator.go` next to the other `deriveID` helpers (the deterministic
  requestId derivation is an Actuator concern).
- The `plan` carrier change (nudge fields) stays in `strategist.go` (the `plan` type's home). Keep it
  minimal — a nil-able `*nudgePlan` field is cleaner than overloading `payload`/`operationType`.

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 10.2 (lines 323-337)]
- [Source: _bmad-output/implementation-artifacts/10-1-weaver-two-phase-nudge.md (the shipped mechanism + the "## 10.2 handoff / deferred" section)]
- [Source: docs/contracts/10-orchestration-surfaces.md#10.3 — weaver-claims (lines ~280-304) + weaver-state claimId/corrupt-claim rule (lines ~267-272)]
- [Source: _bmad-output/planning-artifacts/lattice-architecture.md#Item 3: Two-Phase Nudge — External Operation Idempotency (FR58) (lines ~954-969)]
- [Source: docs/components/weaver.md#Two-Phase Nudge + #Implementation status — Actions row (line ~390) + In/Out weaver-claims row (line ~354)]
- [Source: internal/weaver/strategist.go — actionNudge (line 14), the "nudge is not yet implemented" planError (line ~179-182), resolveStringParam/resolveParam]
- [Source: internal/weaver/evaluator.go — dispatchGap (line ~133), fireEpisode (line ~214), fire (line ~248)]
- [Source: internal/weaver/actuator.go — submit (line ~57), deriveEpisodeRequestID (line ~111), deriveID (line ~136)]
- [Source: internal/weaver/reconciler.go — reclaim (line ~232), corrupt-empty-claimId guard (line ~290)]
- [Source: internal/weaver/state.go — createNudge (line ~113), replaceCarryingClaim (line ~189), replace (line ~177)]
- [Source: internal/weaver/engine.go — NewEngine adapters/claims/nudger construction (line ~287-312); Config CoreKVBucket/WeaverClaimsBucket/ClaimRetention]
- [Source: internal/weaver/nudge/protocol.go — Nudger.Run (line 81), Nudger.Recover (line 131), ResolveFunc/ResolveProbe (line 14,23), Dispatch (line 44)]
- [Source: internal/weaver/nudge/claims.go — ClaimStore (Write/Get/Advance/reopen/Resolve), Claim shape, ErrClaimExists]
- [Source: internal/weaver/nudge/adapter.go — Adapter/Request/Result/Registry; internal/weaver/nudge/fake_background_check.go — the FakeStripe template]
- [Source: internal/weaver/registry.go — GapAction.Adapter/Operation/Subject/Params (line 39-48)]
- [Source: CLAUDE.md — house rules: frozen contracts, no history comments, verification gates, sub-agents don't commit]

## Open Questions

1. **`ResolveProbe` Core KV tracker key shape (AC #4) — confirm against Contract #4 / Contract #1.**
   §10.3 says `resolveRef = requestId / op key of the resolve mutation in Core KV`, and the §10.6
   read-before-act precedent (Loom) GETs the op tracker. The op tracker key is `vtx.op.<requestId>` per
   Contract #1/#4 (idempotency tracker). **Recommendation:** the `ResolveProbe` GETs
   `vtx.op.<deriveResolveRequestID(claimId)>` in `cfg.CoreKVBucket`; present ⇒ landed. The dev agent
   should confirm the exact tracker key shape + bucket against Contract #4 (and the Loom read-before-act
   code, e.g. `internal/loom`) before wiring — flag if the tracker is keyed differently (a wrong key
   would make the probe always-absent → always re-execute, which the adapter de-dups but is wasteful and
   would mask a real duplicate-resolve bug). This is the one genuinely cross-contract integration point.

2. **Adapter registration: now (`cmd/weaver`) or with Epic 11 `lease-signing`?** The reference adapters
   (`FakeStripe`/`FakeBackgroundCheck`) must be REGISTERED for a `nudge` gap's `adapter` name to resolve.
   **Recommendation:** add the `Engine.RegisterAdapter` seam in this story (the engine must expose it
   regardless) and exercise it from the idempotency-proof test; register the demo adapters in
   `cmd/weaver` only if `cmd/weaver` already has a Phase-2 demo wiring point — otherwise leave the
   real-playbook adapter wiring to Epic 11 (`lease-signing`, which authors the actual §10.8 playbooks).
   Flag if `cmd/weaver` should wire them now for the Loftspace demo.

3. **`fireNudge` failure→Decision mapping.** A nudge adapter HARD-fail leaves the claim `failed`
   (durable, re-drivable by the sweep on lease expiry). **Recommendation:** `Ack` it (surface a Health
   issue) rather than `Nak` — a `Nak` would hot-loop the lane-1 redelivery against a deterministically
   failing adapter, and the §10.3 model is that the reconciler re-attempts on the lease. A resolve-SUBMIT
   failure (claim left `executing`) → `Nak`/`NakWithDelay` so the redelivery re-drives via `Recover`. The
   dev agent should pick the disposition that keeps the gap converging without a hot loop and document it;
   flag if the lead prefers a different posture (this mirrors the deferred "executing-wedge Health
   surfacing" 10.1 handoff item — a claim stuck `executing` past a generous bound should surface a Health
   issue rather than re-execute unboundedly; consider landing that bound here since this is the live path).

4. **Resolve op payload shape + `authTarget`.** The resolve op carries the `claimId` reference field
   (arch Item 3) plus the adapter `Result.Detail` and whatever the resolve `operationType` (the §10.8
   `nudge` action's `Operation`) requires as a business mutation. **Recommendation:** keep the payload
   minimal — `{ "claimId": <claimId>, "result": <Result.Detail>, "expectedRevision"? }` — and set
   `authTarget` to the nudge's resolved `subject` (the entity the external action concerns), matching the
   §10.8 action-table target semantics. The dev agent should confirm the resolve op type's expected
   payload against the §10.8 action table (and Epic 11's `lease-signing` playbook intent, if available)
   and document it; the exact business-mutation fields are playbook-driven and may be thin for the
   Phase-2 mocked demo.

## Dev Agent Record

### Open-Question resolutions (lead-ratified, applied verbatim)

1. **ResolveProbe key shape** — grounded in Contract #4 (`docs/contracts/04-idempotency-tracker.md`:
   tracker key is `vtx.op.<requestId>` in Core KV) and the Loom read-before-act precedent
   (`internal/loom/engine.go:1119` `trackerExists` — GETs `vtx.op.<requestId>` in `cfg.CoreKVBucket`).
   `Engine.resolveProbe` GETs `vtx.op.<deriveResolveRequestID(claimId)>` in `cfg.CoreKVBucket`;
   `ErrKeyNotFound` ⇒ not landed. The resolve op's `requestId` is deterministic from the claimId via
   the new `deriveResolveRequestID(claimId)` (a `"resolve:"` `deriveID` namespace), so a duplicate
   resolve collapses on the Contract #4 tracker.
2. **Adapter registration** — added the `Engine.RegisterAdapter` seam (delegates to
   `nudge.Registry.Register`); `cmd/weaver` registers `FakeStripe` (`"stripe"`) and
   `FakeBackgroundCheck` (`"backgroundCheck"`) before `Start`. Package-data-driven registration stays
   Epic 11; no `lease-signing` import.
3. **Failure posture** — fire-and-forget: an adapter hard-fail leaves the claim `failed` (durable) and
   Acks + raises a `NudgeAdapterFailed` Health issue (the reconciler re-attempts on the lease; no Nak
   hot-loop); a resolve-submit failure leaves the claim `executing` and Naks so the redelivery
   re-drives via `Recover`. Also landed the deferred 10.1 item: a claim unresolved past the Contract #4
   24h idempotency horizon (`claimWedgeBound`) surfaces a `NudgeClaimWedged` Health issue from the
   reconciler rather than re-executing beyond the adapter dedup window unguarded.
4. **Resolve op** — minimal payload `{claimId, result, expectedRevision}`; `operationType` = the
   §10.8 nudge action's `Operation`; `authTarget` = the resolved nudge subject; deterministic
   `requestId` via `deriveResolveRequestID(claimId)`; submitted under Weaver's service-actor authority
   (no `authContext` beyond the target), consistent with the existing `actuator.submit` pattern.

### Completion Notes

- Removed the `actionNudge` loud-stub `planError`; `buildPlan` now resolves a `*nudgePlan`
  (adapter/operation/subject/params) with config/data routing matching the other actions. Non-nudge
  actions are byte-for-byte unchanged (still `markStore.create` + plain `fire`).
- Live lane-1 dispatch: `fireEpisode` branches on `actionNudge` → `createNudge` + `fireNudge`
  (`Nudger.Run`) on the create-win; a redelivery over a live mark routes to `recoverNudge`
  (`Nudger.Recover`) using the mark's existing `claimId`.
- Reconciler `reclaim` branches the nudge action AFTER the corrupt-empty-`claimId` guard (kept intact,
  ordered first): re-arm via `replaceCarryingClaim` (carry `rec.ClaimID`) + `Nudger.Recover` with the
  real `resolveProbe`. The executing-wedge Health bound is checked on reclaim.
- `FakeStripe` reference adapter added (substrate-only; idempotent on `idempotencyKey`; `FailNext`/
  `FailUntil` fail modes; a failed attempt records NO side-effect).
- All verification gates pass (see Change Log). Module boundary still substrate-only.

### File List

- `internal/weaver/strategist.go` — removed the `actionNudge` `planError` stub + stale doc-comment;
  added the real nudge plan branch and the `nudgePlan` carrier on `plan`.
- `internal/weaver/actuator.go` — added `deriveResolveRequestID(claimID)` (`"resolve:"` namespace).
- `internal/weaver/evaluator.go` — `fireEpisode` nudge branch (`createNudge` + `fireNudge`/
  `recoverNudge`); `nudgeDispatch`/`fireNudge`/`recoverNudge`/`nudgeDecision`; `issueKeyNudge`;
  threaded `rowRevision` through `fireEpisode`.
- `internal/weaver/reconciler.go` — nudge `reclaimNudge` (`replaceCarryingClaim` + `Nudger.Recover`);
  `claimWedgeBound` + `NudgeClaimWedged` wedge check.
- `internal/weaver/engine.go` — `RegisterAdapter` seam; `resolveFunc`/`resolveProbe` engine methods.
- `internal/weaver/nudge/fake_stripe.go` — **NEW** `FakeStripe` reference adapter.
- `cmd/weaver/main.go` — register `FakeStripe` + `FakeBackgroundCheck` before `Start`.
- `internal/weaver/nudge/fake_stripe_test.go` — **NEW** FakeStripe unit tests (idempotency + fail mode).
- `internal/weaver/nudge_dispatch_internal_test.go` — **NEW** FR58 idempotency proof,
  crash-between-claim-and-resolve proof, landed-resolve probe test, fresh happy-path, sweep-reclaim
  recovery, and `TestBuildPlan_Nudge` config/data routing.
- `internal/weaver/requestid_internal_test.go` — added `TestDeriveResolveRequestID_DeterministicAndDisjoint`.
- `internal/weaver/reconciler_nudge_internal_test.go` — refreshed the stale 10.1 comment on
  `TestSweep_NudgeMarkWithClaimIDNotCorrupt` (behavior now live, assertions unchanged).
- `docs/components/weaver.md` — Two-Phase Nudge build-status, In/Out `weaver-claims` row,
  Implementation-status Actions + Reconciler-sweep rows flipped to shipped/wired end-to-end.

### Change Log

- 2026-06-13 — Story 10.2 implemented: nudge wired into the live Actuator dispatch + reconciler
  recovery; `FakeStripe` reference adapter; deterministic resolve `requestId`; FR58 idempotency +
  crash-between-claim-and-resolve proofs. Gates: `go build ./...` ✅, `make vet` ✅,
  `golangci-lint run ./...` ✅ (0 issues), `make verify-kernel` ✅, `make test-bypass` ✅ (Gate 2 all
  BLOCKED), `make test-capability-adversarial` ✅ (Gate 3 PASSED — 3 DEFENDED + 1 ACCEPTED-WINDOW),
  `go test ./internal/weaver/... ./internal/weaver/nudge/...` ✅. No CONTRACT-AMENDMENT-REQUEST filed
  (Contract #4 tracker key shape matched the existing Loom precedent; the frozen §10.3 claim shape
  fit the live wiring without change).

## Review fixes

Applied after the 3-layer adversarial code review (Blind Hunter / Edge Case Hunter / Acceptance
Auditor). Six fixes-forward, plus a contract-amendment request:

1. **Missing-adapter → Ack + Health, not Nak (Blind HIGH / Edge F8).** Added the
   `nudge.ErrAdapterNotFound` sentinel (`internal/weaver/nudge/protocol.go`), wrapped by both `Run`
   and `Recover` on a registry miss. `nudgeDecision` now classifies `errors.Is(err,
   nudge.ErrAdapterNotFound)` → `substrate.Ack` + a `NudgeAdapterMissing` config Health issue
   (errConfig posture), so a nudge gap naming an unregistered adapter no longer hot-loops lane-1.
   Test: `TestNudgeDispatch_MissingAdapterAcksAndAlerts`.
2. **`NudgeClaimWedged` persists + enforced on both recovery paths (Blind MEDIUM×2 / Edge F4).** The
   wedge alert moved to its own issue key (`issueKeyNudgeWedge`) so `nudgeDecision`'s clear/raise on
   `issueKeyNudge` can no longer clobber it, and the `claimWedgeBound` check moved out of the sweep
   (`reclaimNudge`) into the shared `recoverNudge` (`checkClaimWedge`) so it fires on both the sweep
   reclaim and the lane-1 live-redelivery recovery. Test: `TestNudgeRecovery_WedgedClaimRaisesHealth`.
3. **`resolveProbe` mirrors Contract #4's `isDeleted:false` guard (Edge F7).** Verified substrate
   `KVGet` returns a logically-deleted (`isDeleted:true`) Core KV envelope normally (only a hard NATS
   purge yields `ErrKeyNotFound`). The probe now unmarshals the `vtx.op.<requestId>` tracker envelope
   and treats `isDeleted:true` (the §4.3 operator-driven retry signal) and an unparseable envelope as
   **not landed**, so a genuinely-incomplete claim is re-driven rather than silently advanced to
   resolved off a tombstone. Test: `TestNudgeRecovery_TombstonedResolveNotLanded`.
4. **`claimedAt` parse failure surfaces Health (Edge F10).** `checkClaimWedge` raises a
   `NudgeClaimCorrupt` Health issue on an unparseable `claimedAt` instead of silently skipping the
   wedge check (going dark on the only operator signal for a lapsed dedup guarantee).
5. **Corrected the "single writer, no CAS" doc in `claims.go` (Edge F5 — doc-only, no behavior
   change).** The comment now states that the sweep and lane-1 redelivery can both drive recovery for
   one claimId concurrently within an instance, and that duplicate-safety rests on the adapter
   de-duping the same idempotencyKey and the resolve op collapsing on the deterministic Contract #4
   requestId (NOT on single-writer); a transient state-field flip is self-healing on the next
   recovery.
6. **Filed a CONTRACT-AMENDMENT-REQUEST for the §10.8 `operation` seam (Acceptance Auditor).** §10.8's
   frozen `nudge` action row is `{ adapter, subject, params? }` and its `missing_bgcheck` example omits
   `operation`, but the §10.3 `weaver-claims` value shape (same contract) already includes `operation`
   and the implementation requires it (the resolve-op type). `operation` is kept required; the gap is
   raised in `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md` (Request 4 — add `operation` to the §10.8
   nudge action row + update the example), **not** edited into the frozen contract. This supersedes
   the earlier change-log line "No CONTRACT-AMENDMENT-REQUEST filed."

Docs updated (`docs/components/weaver.md`): missing-adapter Ack/`NudgeAdapterMissing` posture; the
`isDeleted:false` probe guard; the wedge issue-key/both-paths note.
