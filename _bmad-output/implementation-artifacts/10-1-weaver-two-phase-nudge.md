# Story 10.1: External Adapter framework + Two-Phase Nudge protocol

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform developer,
I want a claim→execute→resolve protocol around external calls,
so that a failed or retried external call cannot duplicate an action.

## Acceptance Criteria

1. **Given** a new External Adapter framework package `internal/weaver/nudge/`
   **When** the package is built
   **Then** it defines (a) an `Adapter` interface an external integration implements — the unit of
   "call the external system idempotently" — keyed on the `idempotencyKey` (= `claimId`, Contract #10
   §10.3 `weaver-claims`) so the adapter (not Weaver) is the de-dup boundary for the real external
   action; and (b) a `FakeBackgroundCheck` **reference adapter** that proves the framework end-to-end
   (mocked — no real network; demo/Phase-2 adapters are mocked, real Stripe/background-check is Phase 3
   per `docs/components/weaver.md` Two-Phase Nudge).
   **And** `internal/weaver/nudge` imports only `internal/substrate/*` (the module-boundary rule —
   `docs/components/weaver.md` Principles "Module boundary"); it does **not** import `internal/processor`
   or a raw `nats.io`/`jetstream` handle.

2. **Given** the §10.3 `weaver-claims` bucket (primordial, `WeaverClaimsBucket = "weaver-claims"`,
   `internal/bootstrap/primordial.go`) and the frozen record shape
   `{ claimId, adapter, operation, subject, params, idempotencyKey(=claimId), state, claimedAt,
   resolvedAt?, resolveRef? }`
   **When** Weaver performs an external action (the nudge protocol runs)
   **Then** the protocol writes the claim record to `weaver-claims.<claimId>` with `state="claimed"`
   **before** the external call (NFR-S11 "records a visible claim state before executing any external
   call"), then transitions `state="executing"` and calls the (mocked) adapter with
   `idempotencyKey = claimId`, then on a successful call submits a **resolve op** through the
   Processor (a normal `actuator.submit` publish to `ops.<lane>`) carrying the `claimId` as a payload
   reference field, and finally advances the claim to `state="resolved"` recording
   `resolvedAt` + `resolveRef` (the resolve op's `requestId`).

3. **Given** the §10.3 invariant that a nudge's `claimId` is "minted and written into the
   `weaver-state` mark in the SAME atomic op as the CAS-create"
   **When** a nudge gap is dispatched (its in-flight mark is created)
   **Then** the mark's CAS-create mints a fresh NanoID `claimId` (`substrate.NewNanoID`) and writes it
   into the `mark.ClaimID` field **in the single `KVCreateWithTTL` that creates the mark** — so a nudge
   mark **always** carries a non-empty `claimId`, impossible-by-construction (a new claim-aware create
   path alongside the existing `markStore.create`, since `create` today writes no `claimId`).
   **And** the claim record at `weaver-claims.<claimId>` uses that **same** `claimId` as both its key
   and its `idempotencyKey` — the mark→claim link that lets a crash-retry within a live lease resume the
   *same* claim rather than start a new external call.

4. **Given** a claim found by the reconciler in `claimed`/`executing` state past its lease (recovery)
   **When** the reconciler recovers it
   **Then** recovery is **read-before-act**: it (a) **reuses the same `claimId`/`idempotencyKey`** and
   **never mints a new one**, and (b) checks `resolveRef` / Core KV for an already-landed resolve before
   re-executing — if the resolve already committed it just advances the record to `resolved` (Core KV is
   authoritative; a claim stuck pre-`resolved` is merely a stale operational record); adapter idempotency
   on the reused `idempotencyKey` is what makes an `executing`-state retry safe.
   **And** an **empty `claimId` on a nudge mark is corrupt**: it is alerted (Health KV issue) and the
   reconciler **never mints a fresh `claimId`** for it (a fresh id would mean a second `idempotencyKey`
   → a duplicate external call) — matching the §10.3 / `state.go` rule already documented as Epic-10 work.

5. **Given** the 90d (default, configurable) retention requirement (§10.3 `weaver-claims`,
   arch Item 3 "default: 90 days")
   **When** the claim store is provisioned/used
   **Then** claims are retained per a `Config`-tunable retention (default 90d), and an audit can join
   the resolve op (Core KV business outcome, via `resolveRef`) to the claim (operational intent, the
   `weaver-claims.<claimId>` record) — the two-sided audit trail NFR-S11 requires.

6. **Given** the verification gates (CLAUDE.md house rules)
   **When** the work is declared done
   **Then** `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`,
   `make test-bypass` (Gate 2, all BLOCKED), `make test-capability-adversarial` (Gate 3, all DEFENDED),
   and `go test ./internal/weaver/... ./internal/weaver/nudge/...` all pass.

## Tasks / Subtasks

- [x] **Task 1 — `internal/weaver/nudge/` External Adapter framework** (AC: #1)
  - [x] Create `internal/weaver/nudge/doc.go` describing the package: "External Adapter framework +
        Two-Phase Nudge protocol mechanics; framework is engine, reference adapters prove it."
  - [x] Define the `Adapter` interface: a single `Execute(ctx, req)` method (or equivalently named)
        taking the dispatch params + the `idempotencyKey` and returning a result/error. The
        `idempotencyKey` is the contract: the adapter MUST treat two calls with the same key as the same
        action (it de-dups, not Weaver). Document this on the interface.
  - [x] Define an `Adapter`-registry / lookup keyed by adapter name (the §10.8 `GapAction.Adapter`
        field; `internal/weaver/registry.go` already carries `Adapter string`), so the nudge action can
        resolve `adapter` → concrete adapter at dispatch time. A missing adapter is a config error
        (surfaced, never silent — mirrors `buildPlan`'s `errConfig` posture).
  - [x] Implement `FakeBackgroundCheck` reference adapter: deterministic, in-memory, **records the
        `idempotencyKey`s it has seen and returns the same result for a repeat key WITHOUT a second
        side-effect** (this is the literal proof of external idempotency). No network, no real I/O.
  - [x] Module-boundary: confirm via `go list -deps` / the existing `boundary_test.go` style that
        `internal/weaver/nudge` imports only `internal/substrate/*` (no `internal/processor`, no raw
        `nats.go`/`jetstream`). Add a boundary assertion test if the existing one does not already cover
        the subpackage.

- [x] **Task 2 — `weaver-claims` claim store + record shape** (AC: #2, #3, #5)
  - [x] Add a `claimStore` (new file, e.g. `internal/weaver/nudge/claims.go` or
        `internal/weaver/claims.go` — see Project Structure Notes) wrapping the `weaver-claims` bucket
        (`bootstrap.WeaverClaimsBucket`), mirroring `markStore`'s constructor/accessor style
        (`internal/weaver/state.go`): `conn *substrate.Conn`, `bucket string`, retention `time.Duration`,
        `instance string`.
  - [x] Define the claim record struct **exactly** to the frozen §10.3 shape (field names = the JSON
        keys in `docs/contracts/10-orchestration-surfaces.md` §10.3 `weaver-claims`):
        `{ claimId, adapter, operation, subject, params, idempotencyKey, state, claimedAt, resolvedAt?,
        resolveRef? }`. `state ∈ {claimed, executing, resolved, failed}` — model as typed constants.
        `idempotencyKey == claimId` is an invariant (set both from the one minted NanoID), not two inputs.
  - [x] `writeClaim` — direct KV write (`KVPut` / `KVPutWithTTL`) of a `state="claimed"` record at key
        `<claimId>` BEFORE the call (this is the "visible claim state before executing" of NFR-S11).
        Apply the retention as the bucket/record TTL **or** document that retention is the bucket's
        `MaxAge` (decide per the verify-kernel posture — see Dev Notes; the simplest contract-faithful
        approach is a per-key TTL = retention, mirroring `markStore`'s TTL discipline, since the bucket is
        primordial and TTL-capable). Default retention 90d, `Config`-tunable (Task 5).
  - [x] `advanceClaim` / state transitions: `claimed → executing` (before the adapter call),
        `executing → resolved` (after the resolve op publishes, writing `resolvedAt` + `resolveRef`), and
        `executing/claimed → failed` (adapter hard-fails). Use a read-modify-write that preserves the
        immutable fields; transitions are idempotent (re-advancing to a state already reached is a no-op).
  - [x] `getClaim` — read a claim by `claimId` for recovery (Task 4) and audit join.

- [x] **Task 3 — Claim-minting mark create (atomic claimId)** (AC: #3)
  - [x] In `internal/weaver/state.go`, add a nudge-aware create path that mints
        `claimId, _ := substrate.NewNanoID()` and sets `rec.ClaimID = claimId` BEFORE marshaling, so the
        single `KVCreateWithTTL` that creates the mark carries the `claimId` — the §10.3 "same atomic op"
        invariant. Keep the existing non-nudge `create` (no claimId) for triggerLoom/assignTask/directOp;
        the nudge create returns the minted `claimId` to the caller so the claim record uses the same id.
        (Do NOT mint a claimId on a non-nudge mark.)
  - [x] Mirror the same claimId-minting in the reconciler's reclaim path **only as a guarded read** — a
        reclaim of an EXISTING nudge mark must REUSE the mark's existing `claimId` (Task 4 / AC #4), never
        mint a new one on replace. (The replace path in `state.go` currently writes no claimId — it must
        carry forward the existing mark's `claimId` when reclaiming a nudge mark.)

- [x] **Task 4 — Two-Phase Nudge protocol orchestration (claim→execute→resolve) + recovery** (AC: #2, #4)
  - [x] Implement the protocol as a callable unit (e.g. `nudge.Run(ctx, ...)` or a `nudger` type) that,
        given a resolved nudge dispatch (adapter, operation, subject, params), the minted `claimId`, and a
        resolve-submit callback (so the orchestrator does not itself hold an actuator — keeps the package
        substrate-only and lets 10.2 wire the real `actuator.submit`): (1) writes the claim
        (`state=claimed`), (2) advances to `executing` + calls the adapter with `idempotencyKey=claimId`,
        (3) on success submits the resolve op carrying `claimId` via the callback and advances to
        `resolved` recording `resolveRef`; on adapter failure advances to `failed` and surfaces it.
  - [x] **Recovery (reconciler) is read-before-act** (AC #4): a claim in `claimed`/`executing` past its
        lease — reuse the same `claimId`/`idempotencyKey`, GET `resolveRef`/Core KV for an already-landed
        resolve first; if resolved-already → advance the record to `resolved` (no re-execute); else
        re-execute via the adapter on the SAME `idempotencyKey` (adapter de-dups). Wire this into the
        existing reconciler sweep (`internal/weaver/reconciler.go`) for nudge marks, or document the
        recovery entry point clearly if full sweep-integration is deferred to 10.2 (see Story Boundary).
  - [x] **Corrupt-claim guard** (AC #4): a nudge mark observed with an EMPTY `claimId` is corrupt → alert
        via the `issueCache` (Health KV issue, `internal/weaver/health.go`) and **never mint a fresh
        claimId** for it. This is the rule already written into `state.go`'s `mark` doc comment and §10.3.

- [x] **Task 5 — Config wiring + retention** (AC: #5)
  - [x] Add a `WeaverClaimsBucket` field + a `ClaimRetention time.Duration` (default 90d) to the engine
        `Config` (`internal/weaver/engine.go`), defaulted in `withDefaults()` exactly like
        `WeaverStateBucket`/`MarkLease`. Construct the `claimStore` in `New` alongside `newMarkStore`.
  - [x] Confirm `weaver-claims` is already primordial (`internal/bootstrap/primordial.go` — it is) and
        TTL-capable for the retention approach chosen; if a `MaxAge`/`LimitMarkerTTL` provisioning change
        is needed, raise it via a `cmd/<area>/CONTRACT-AMENDMENT-REQUEST.md` rather than editing the
        frozen contract (CLAUDE.md). Prefer per-key TTL if the bucket already supports it (it is created
        with TTL enabled — verify against the primordial create list).

- [x] **Task 6 — Tests** (AC: #1, #2, #3, #4, #6)
  - [x] `internal/weaver/nudge` unit tests: `FakeBackgroundCheck` returns the same result + no second
        side-effect for a repeated `idempotencyKey`; the protocol writes `claimed` before calling the
        adapter; the full claim→execute→resolve advances the claim through `claimed→executing→resolved`
        with `resolveRef` set; an adapter failure lands `failed`.
  - [x] `internal/weaver` test (internal, follows `state_internal_test.go` style): the claim-minting mark
        create writes a non-empty `claimId` atomically; a reclaim of a nudge mark reuses the existing
        `claimId`; an empty-claimId nudge mark is flagged corrupt and no fresh id is minted.
  - [x] A **retry/idempotency** test at the protocol level (the 10.1 share of the FR58 proof): a simulated
        re-execute on the SAME `claimId` (recovery path) produces NO second adapter side-effect and NO
        second resolve op. (The full end-to-end idempotency proof through the Actuator + `FakeStripe` is
        Story 10.2 — see Story Boundary; 10.1 proves the mechanism in isolation.)
  - [x] Run all verification gates (AC #6).

- [x] **Task 7 — Documentation** (in the same commit as the code, per `docs/components/weaver.md` header)
  - [x] Update the `docs/components/weaver.md` **Implementation status** table row for **Actions**: the
        `nudge` "loud stub" becomes "framework + protocol mechanics shipped (10.1); Actuator wiring +
        FakeStripe idempotency proof is 10.2" — keep the row honest about the 10.1/10.2 split. Do NOT
        claim end-to-end wiring that 10.2 owns.
  - [x] If the `weaver-claims` row in the In/Out contracts table or the Two-Phase Nudge section needs a
        status note (built vs. wired), update it. Do NOT edit the frozen contract (`docs/contracts/*`).

## Dev Notes

### What this story IS and IS NOT (Story Boundary — read first)

This story builds the **mechanism**: the `internal/weaver/nudge/` framework, the `weaver-claims` claim
store, the claim→execute→resolve protocol as a callable/testable unit, the claim-minting mark-create
variant, recovery (read-before-act), and the `FakeBackgroundCheck` reference adapter — proven in
isolation by tests including a same-`claimId` retry that produces no duplicate side-effect.

**Story 10.2 (separate, later) owns the end-to-end wiring**: replacing the `actionNudge` planError in
`buildPlan` (`internal/weaver/strategist.go:182` "nudge is not yet implemented"), threading the nudge
through the Actuator dispatch (`fire`/`fireEpisode` in `internal/weaver/evaluator.go`) so a real
`actionNudge` playbook entry drives the protocol with the real `actuator.submit` resolve, AND the
`FakeStripe` adapter + the full end-to-end idempotency proof (crash-between-claim-and-resolve leaves the
claim visible and the op not re-initiated until reconciled). **Note this boundary; do not implement
10.2's work here.** Concretely: 10.1 MAY leave `buildPlan`'s `actionNudge` case as the existing loud
planError (or convert it to a recognized-but-deferred branch) — but it must NOT silently change dispatch
behavior; the protocol unit is callable and tested independently of the `buildPlan` switch.

The design tension to resolve cleanly: the §10.3 invariant requires the `claimId` to be minted **in the
same atomic op as the mark CAS-create**, but the claim *record* and the external call are the nudge
protocol's job. The clean seam: the **claim-minting mark create (Task 3)** returns the minted `claimId`;
the **protocol (Task 4)** takes that `claimId` as an input and owns the claim record + adapter call +
resolve. 10.1 builds and unit-tests both halves and the seam between them; 10.2 connects that seam into
the live lane-1 dispatch path. Keep the protocol orchestrator free of an `actuator` dependency (take a
resolve-submit callback) so `internal/weaver/nudge` stays substrate-only and 10.2 supplies the real
submit.

### Relevant architecture patterns and constraints

- **Two-Phase Nudge protocol (the spec):** `docs/components/weaver.md` "Two-Phase Nudge" section —
  `1. Claim → write weaver-claims.<claimId> (direct KV; intent recorded BEFORE the call)`,
  `2. Execute → call the external (mocked) adapter; claim prevents any other instance re-initiating`,
  `3. Resolve → submit a normal op via Processor recording the result, carrying the claimId reference`.
  Claims retained 90d; audit joins Core KV (business outcome) to the claim (operational intent).
  [Source: docs/components/weaver.md#Two-Phase Nudge]
- **The frozen `weaver-claims` record shape (build EXACTLY to this):**
  `key: <claimId>`; `value: { claimId, adapter, operation, subject, params, idempotencyKey(=claimId),
  state(claimed→executing→resolved|failed), claimedAt, resolvedAt?, resolveRef? }`; 90d retention;
  protocol Claim→Execute→Resolve; external idempotency is the `idempotencyKey` the **adapter** dedups on,
  **not** a CAS on the claim key (the `weaver-state` mark already serialized the dispatch and carries the
  `claimId`, so the claim has a single writer); recovery is read-before-act reusing the same `claimId` and
  checking `resolveRef`/Core KV before re-executing.
  [Source: docs/contracts/10-orchestration-surfaces.md#10.3 — weaver-claims — Two-Phase Nudge claim record]
- **The `weaver-state` mark `claimId` invariant:** "minted and written into the mark in the SAME atomic op
  as the CAS-create"; "an empty `claimId` on a nudge mark is corrupt: the reconciler alerts and never
  mints a fresh id". This is already reflected (shape-only) in `internal/weaver/state.go` — the `mark`
  struct's `ClaimID string \`json:"claimId,omitempty"\`` field (line ~42) and its doc comment.
  [Source: docs/contracts/10-orchestration-surfaces.md#10.3 — weaver-state; internal/weaver/state.go]
- **Arch Item 3 (FR58, NFR-S11):** claims live in Weaver operational KV (`weaver.claims.>`, dash-named
  `weaver-claims` per §10.3), NOT Core KV; resolve mutations go through the Processor into Core KV as
  normal business state, carrying the `claim-id` as a reference field; "records a visible claim state
  before executing any external call and does not re-initiate a claimed operation."
  [Source: _bmad-output/planning-artifacts/lattice-architecture.md#Item 3: Two-Phase Nudge]
- **Module boundary (hard rule):** `internal/weaver/nudge` imports only `internal/substrate/*`. The
  resolve goes through the Actuator's fire-and-forget publish (`ops.<lane>`), never a request-reply and
  never a raw NATS handle in the engine. [Source: docs/components/weaver.md#Principles that apply;
  internal/weaver/actuator.go]
- **P2 / P1:** Processor is the sole Core KV writer (the resolve op); claims/state are operational KV
  (P1), not Core KV. [Source: docs/components/weaver.md#Principles that apply]
- **CLAUDE.md conventions:** no history/changelog comments in code (comments describe what the code does
  *now*); frozen contracts under `docs/contracts/*` are build-to, never edit (a gap → a
  `cmd/<area>/CONTRACT-AMENDMENT-REQUEST.md`); new docs go in `/docs`. Sub-agents never commit/push/branch.

### Source tree components to touch

- **NEW** `internal/weaver/nudge/` — the External Adapter framework + protocol orchestration +
  `FakeBackgroundCheck` reference adapter (+ `doc.go`). Substrate-only imports.
- `internal/weaver/state.go` — add a claim-minting mark-create variant (mints the NanoID claimId into the
  mark in the single `KVCreateWithTTL`); make the reclaim/replace path carry forward an existing nudge
  mark's `claimId`. The `mark` struct's `ClaimID` field already exists (line ~42).
- `internal/weaver/strategist.go` — the `actionNudge` const (line 16) + the planError at line ~182. 10.1
  may leave the planError or convert it to a recognized-but-deferred branch; the live `buildPlan` wiring
  is 10.2. Do not regress dispatch behavior.
- `internal/weaver/engine.go` — `Config` (add `WeaverClaimsBucket`, `ClaimRetention`; default in
  `withDefaults()`), construct the `claimStore` in `New` (alongside `newMarkStore`, ~line 274).
- `internal/weaver/reconciler.go` — recovery (read-before-act) for nudge claims/marks; corrupt-empty-
  claimId alert. (Full sweep-integration may be staged with 10.2 — document the entry point either way.)
- `internal/weaver/health.go` — surface a corrupt-claim / recovery issue via the existing `issueCache`
  (the `set`/`clear` pattern; the heartbeat already renders the snapshot as Contract #5 issues).
- `internal/bootstrap/primordial.go` — `WeaverClaimsBucket = "weaver-claims"` already exists (line 27)
  and is in the primordial create list (line 73, TTL-capable `true`). Verify TTL suffices for retention.

### Regression guardrails (do not break the shipped 9.x lanes)

- **Non-nudge dispatch is unchanged.** `triggerLoom`/`assignTask`/`directOp` must continue to use the
  existing `markStore.create` (no `claimId`) and `markStore.replace`. Only the nudge action gets the
  claim-minting create. A non-nudge mark must NEVER carry a `claimId` (the `omitempty` keeps the JSON
  identical — assert this in a test so the 9.1/9.2 mark shape is provably unregressed).
- **The reconciler sweep's existing legs** (level-clearing, orphan reclaim, expired-lease reclaim,
  corrupt-mark delete) must keep their current behavior; the nudge-recovery / corrupt-empty-claimId
  handling is ADDITIVE. The sweep's revision-conditioned deletes/replaces and the `SweepOrphanWarmup`
  gating stay intact.
- **`weaver-targets`/`weaver-state` watch + dispatch OCC, lane-3 temporal, and the control plane**
  (9.1–9.4) are untouched by 10.1 except for the additive claim-minting create. Run the full
  `go test ./internal/weaver/...` (incl. the E2E + reconciler internal tests) to prove no regression.
- **Docs editability:** `docs/components/weaver.md` is editable (design-first, "update in the same commit
  as the code"); only `docs/contracts/*` and `_bmad-output/planning-artifacts/*` are off-limits.

### Key integration facts (verified against current code)

- `substrate.NewNanoID() (string, error)` mints a canonical-alphabet NanoID (`internal/substrate/nanoid.go`).
- `Conn.KVCreateWithTTL(ctx, bucket, key, value, ttl)` — CAS-on-absent create with a per-key TTL; the
  existing `markStore.create` already uses it. `KVPutWithTTL`, `KVPut`, `KVGet`, `KVUpdate(WithTTL)`,
  `KVDelete`, `KVListKeys` are all available (`internal/substrate/kv.go`). NATS enforces a 1s TTL floor.
- The Actuator's resolve = `actuator.submit(ctx, requestID, operationType, payload, authTarget)` →
  one fire-and-forget publish to `ops.<lane>` (`internal/weaver/actuator.go:57`). The resolve op's
  `payload` carries the `claimId` reference field. `deriveEpisodeRequestID(...)` derives the deterministic
  per-episode requestId; the resolve op's `requestId` is the `resolveRef` recorded on the claim.
- `GapAction` (`internal/weaver/registry.go:39`) already carries `Adapter string` and `Operation string`
  — the nudge action's adapter name + the resolve operation type. `Subject` / `Params` carry the rest.
- `issueCache.set(key, severity, code, message)` / `.clear(key)` (`internal/weaver/health.go:57`) is the
  FR29 "never silently drop" surface; the heartbeat renders the snapshot (Contract #5 §5.2 issues).
- The dispatch flow today: `evaluator.fireEpisode` CAS-creates the mark then `fire` publishes the op
  (`internal/weaver/evaluator.go:214,248`). The nudge path diverges at the mark-create (claim-minting
  create) and at `fire` (run the protocol instead of a plain submit) — that divergence is **10.2's wiring**;
  10.1 makes the diverging pieces exist and be unit-testable.

### Testing standards summary

- Go table-driven tests; package-internal tests use the `_internal_test.go` suffix and `package weaver`
  (see `state_internal_test.go`, `reconciler_internal_test.go`); the `nudge` package gets its own
  `*_test.go`. Existing E2E lives in `weaver_e2e_test.go` (build-tagged); the 10.1 idempotency proof can
  be a focused unit/protocol test — the full E2E idempotency proof is 10.2.
- The idempotency assertion is the heart of FR58: a repeated `idempotencyKey` must produce **no second
  side-effect** (the `FakeBackgroundCheck` adapter counts/records its calls; assert the count does not
  increment on the recovery re-execute).
- Gates (CLAUDE.md): `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`,
  `make test-bypass` (Gate 2, all BLOCKED), `make test-capability-adversarial` (Gate 3, all DEFENDED),
  and `go test ./internal/weaver/... ./internal/weaver/nudge/...`.

### Project Structure Notes

- The architecture's source-tree sketch lists `internal/weaver/nudge/` as
  "Two-Phase Nudge, external adapters" [Source:
  _bmad-output/planning-artifacts/lattice-architecture.md ~line 676], matching `docs/components/weaver.md`
  ("External adapter framework lives in `internal/weaver/nudge/`" and the
  "What this component will own" table row). Put the `Adapter` interface, the registry, the protocol
  orchestrator, and `FakeBackgroundCheck` in `internal/weaver/nudge/`.
- **`claimStore` placement decision (call-out, can be overridden):** the claim record/store can live
  EITHER in `internal/weaver/nudge/` (cohesive with the protocol; keeps `internal/weaver` lean) OR in
  `internal/weaver/` next to `markStore` (cohesive with the mark, since the mark's `claimId` is the join
  key). Recommendation: put the `claimStore` in `internal/weaver/nudge/` so the protocol package is
  self-contained and substrate-only, and have the engine construct it and pass it where lane-1 dispatch
  needs it in 10.2 — but either is contract-faithful; pick one and be consistent.
- The claim-minting mark create MUST stay in `internal/weaver/state.go` (it is a `markStore` concern; the
  mark is `weaver-state`, not `weaver-claims`). Only the `claimId` value crosses the seam.

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 10.1 (lines 307-321)]
- [Source: docs/components/weaver.md#Two-Phase Nudge (external idempotency, FR58 — PRD-Alignment Item 3)]
- [Source: docs/components/weaver.md#Implementation status — Actions row (nudge is a loud stub)]
- [Source: docs/contracts/10-orchestration-surfaces.md#10.3 Operational KV namespaces — weaver-claims (lines ~280-304)]
- [Source: docs/contracts/10-orchestration-surfaces.md#10.3 — weaver-state — claimId atomic-with-CAS-create + corrupt-empty-claimId rule (lines ~267-272)]
- [Source: _bmad-output/planning-artifacts/lattice-architecture.md#Item 3: Two-Phase Nudge — External Operation Idempotency (FR58) (lines ~954-969)]
- [Source: internal/weaver/strategist.go — actionNudge (line 16), the "nudge is not yet implemented" planError (line ~182)]
- [Source: internal/weaver/state.go — mark.ClaimID field (line ~42), markStore.create/replace (lines ~78,133)]
- [Source: internal/weaver/actuator.go — actuator.submit fire-and-forget dispatch (line ~57)]
- [Source: internal/weaver/evaluator.go — fireEpisode/fire dispatch core (lines ~214,248)]
- [Source: internal/weaver/health.go — issueCache (line ~48)]
- [Source: internal/weaver/registry.go — GapAction.Adapter/Operation (line ~39)]
- [Source: internal/bootstrap/primordial.go — WeaverClaimsBucket (line 27), primordial create list (line 73)]
- [Source: internal/substrate/kv.go — KVCreateWithTTL/KVPut/KVGet/etc.; internal/substrate/nanoid.go — NewNanoID]
- [Source: CLAUDE.md — house rules: frozen contracts, no history comments, verification gates, sub-agents don't commit]

## Open Questions

1. **Retention mechanism — per-key TTL vs. bucket `MaxAge`.** The frozen §10.3 says "90d retention" but
   does not dictate the mechanism. `weaver-claims` is primordial and created TTL-capable (`true` flag).
   Recommendation: per-key TTL = `ClaimRetention` (mirrors `markStore`'s TTL discipline, no provisioning
   change). If a `MaxAge` on the bucket is preferred, confirm `verify-kernel` won't flag a primordial
   provisioning drift — and if it would, raise a `CONTRACT-AMENDMENT-REQUEST.md` rather than editing the
   frozen contract. Leaving the mechanism to the dev agent with the TTL recommendation.

2. **Reconciler recovery integration depth in 10.1 vs. 10.2.** AC #4 specifies read-before-act recovery
   semantics, but the *full* sweep integration (the reconciler enumerating nudge claims past lease and
   re-driving the protocol live) is intertwined with the lane-1 wiring 10.2 owns. The story scopes 10.1 to
   build the recovery LOGIC as a callable, unit-tested unit (reuse claimId, check resolveRef/Core KV,
   no-mint-on-corrupt) and leaves the live sweep loop hook to 10.2 if it proves cleaner. The dev agent
   should land whichever boundary keeps both stories green; flag if the split needs Winston's adjudication.

3. **`resolveRef` = requestId vs. op-vertex key.** §10.3 says `resolveRef = requestId / op key of the
   resolve mutation in Core KV`. The Actuator submits with a deterministic `requestId`
   (`deriveEpisodeRequestID`), which is the natural `resolveRef` (the `vtx.op.<requestId>` tracker is the
   Core KV join). Recommendation: record the resolve op's `requestId` as `resolveRef`. Confirm this is the
   intended audit-join key (it matches the §10.6 read-before-act tracker-GET precedent Loom uses).

4. **`buildPlan` `actionNudge` disposition in 10.1.** Whether to leave the existing loud planError as-is
   (cleanest 10.1/10.2 split — 10.2 replaces it) or pre-stage a recognized branch now. Recommendation:
   leave the planError untouched in 10.1 so dispatch behavior does not change mid-story; 10.2 replaces it
   when it wires the protocol into `fire`. Flag if a different staging is preferred.

## Dev Agent Record

### Open-Question dispositions (lead-ratified, applied as-is)

1. **Retention mechanism** — per-key TTL = `ClaimRetention` (default 90d), the recommended option.
   `weaver-claims` is provisioned TTL-capable (`internal/bootstrap/primordial.go` line 73,
   `LimitMarkerTTL` enabled) and `verify-kernel` asserts the bucket exists and stays green — no
   provisioning change, **no CONTRACT-AMENDMENT-REQUEST needed**.
2. **Recovery depth** — the read-before-act recovery LOGIC lands in 10.1's claim store / protocol
   (`Nudger.Recover`) and is unit-proven (reuse `claimId`, check `resolveRef`/Core KV via a probe,
   no-mint-on-empty-`claimId`). The corrupt-empty-`claimId` mark guard is wired into the live sweep
   (`reconciler.go`, additive — only nudge marks). Full sweep-drives-the-protocol-live integration is
   left to 10.2 (it is intertwined with the lane-1 dispatch wiring 10.2 owns); the recovery entry point
   is the documented `Nudger.Recover`.
3. **`resolveRef`** = the resolve op's `requestId` (the value returned by the `ResolveFunc` callback).
4. **`buildPlan` `actionNudge`** — left as the existing loud `planError` ("nudge is not yet
   implemented"). Dispatch behavior is unchanged; the protocol unit is callable and tested independently
   of the `buildPlan` switch. A nudge mark therefore never reaches a live dispatch in 10.1 (it is the
   reconciler's corrupt-`claimId` guard and the protocol unit tests that exercise the new code).

### Implementation notes

- `internal/weaver/nudge/` is substrate-only (boundary asserted by a new `boundary_test.go` in the
  subpackage: no processor/loom/refractor, no raw NATS handle). The protocol orchestrator
  (`Nudger`) takes a `ResolveFunc` callback for the resolve op so it holds no Actuator — 10.2 supplies
  the real `actuator.submit`.
- The claim store lives in `internal/weaver/nudge/claims.go` (the story's recommended placement —
  self-contained, substrate-only). The claim-minting mark create (`markStore.createNudge`) and the
  claimId-carrying reclaim (`markStore.replaceCarryingClaim`) stay in `internal/weaver/state.go` (a
  `markStore`/`weaver-state` concern); only the `claimId` value crosses the seam.
- Engine `Config` gains `WeaverClaimsBucket` + `ClaimRetention` (defaulted in `withDefaults()`); the
  engine constructs the `nudge.Registry`, `nudge.ClaimStore`, and `nudge.Nudger` in `NewEngine` — wired
  and ready for 10.2's dispatch path (not yet read on a live path, which is correct for the 10.1/10.2
  split).
- Regression guard proven: a non-nudge mark carries no `claimId` and its on-wire JSON omits the key
  entirely (`omitempty`) — `TestNonNudgeCreate_CarriesNoClaimID`.

### Verification gates (all green)

- `go build ./...` — PASS
- `make vet` — PASS
- `golangci-lint run ./...` — PASS (0 issues)
- `make verify-kernel` — PASS (ALL ASSERTIONS PASSED; `weaver-claims` bucket OK)
- `make test-bypass` — PASS (Gate 2: all vectors BLOCKED)
- `make test-capability-adversarial` — PASS (Gate 3: 3 DEFENDED, 1 ACCEPTED-WINDOW, 4/4 cleared)
- `go test ./internal/weaver/... ./internal/weaver/nudge/...` — PASS

## File List

**New:**
- `internal/weaver/nudge/doc.go`
- `internal/weaver/nudge/adapter.go`
- `internal/weaver/nudge/fake_background_check.go`
- `internal/weaver/nudge/claims.go`
- `internal/weaver/nudge/protocol.go`
- `internal/weaver/nudge/adapter_test.go`
- `internal/weaver/nudge/protocol_test.go`
- `internal/weaver/nudge/boundary_test.go`
- `internal/weaver/state_nudge_internal_test.go`
- `internal/weaver/reconciler_nudge_internal_test.go`

**Modified:**
- `internal/weaver/state.go` (`markStore.createNudge`, `markStore.replaceCarryingClaim`; `replace`
  now delegates to `replaceCarryingClaim` with an empty claimId)
- `internal/weaver/reconciler.go` (`defaultClaimRetention` const; corrupt-empty-`claimId` nudge-mark
  guard in `reclaim`)
- `internal/weaver/engine.go` (`Config.WeaverClaimsBucket` + `Config.ClaimRetention` + defaults;
  `claims`/`adapters`/`nudger` engine fields constructed in `NewEngine`)
- `docs/components/weaver.md` (Implementation-status header + Actions row + In/Out `weaver-claims` row
  + Dispatch-OCC `claimId` note + Two-Phase Nudge section: 10.1/10.2 build-vs-wired split)

## Review fixes

Applied from the adversarial code-review triage (each with a unit test):

1. **Create-semantic claim write — `Run` cannot clobber a live claim (H1).**
   `ClaimStore.Write` is now create-on-absent (`KVCreateWithTTL`, mirroring the `createNudge`
   mark create) and returns `nudge.ErrClaimExists` if a claim already exists. A redelivery/retry
   routed to `Run` over a live `resolved`/`executing`/`failed` claim is rejected (the caller must
   route it to `Recover`) — it can no longer reset the claim to `claimed` and re-call the adapter.
   Test: `TestNudger_RunRejectsExistingClaim` (no overwrite, no adapter call).

2. **Panic-contain the adapter (M3).** Every `Adapter.Execute` call goes through `execute(...)`,
   which `recover()`s a panic and converts it to a normal execution error → the claim lands
   `failed` (re-drivable), never a propagated panic that strands the claim in `executing`.
   Test: `TestNudger_RunContainsAdapterPanic`.

3. **Mandatory `Recover` probe (H2/M2).** A `nil` `ResolveProbe` is now rejected like a blank
   `claimId`, so the already-landed-resolve check can never be silently skipped (closing the
   duplicate-resolve hole on a crash between execute and record).
   Test: `TestNudger_RecoverRejectsNilProbe`; the already-landed path stays covered by
   `TestNudger_RecoverReusesClaimIDNoSecondSideEffect`.

4. **A `failed` claim re-attempts, never wedges (M1).** Recovery now short-circuits **only** on
   `resolved`; a `failed` (or `executing`) claim re-attempts on the SAME `claimId`/`idempotencyKey`
   (new `ClaimStore.reopen` allows `failed → executing`; `Resolve` now accepts a `failed` claim
   settling to `resolved`, while still rejecting an already-`resolved` claim with a conflicting
   `resolveRef`). The adapter dedups any partial side-effect, so a re-attempt is at-most-one
   side-effect and the gap converges (§10.3 re-fire-is-safe).
   Test: `TestNudger_RecoverFailedReAttemptsSameKey` (one side-effect on dedup, then resolved).

## 10.2 handoff / deferred

Accepted-deferral items carried to Story 10.2 wiring:

- **Deterministic resolve `requestId` (10.2).** The resolve op MUST use a deterministic `requestId`
  derived from the `claimId` so a duplicate resolve collapses on the Contract #4 idempotency
  tracker. 10.1's `ResolveFunc` seam takes the `claimId`; the live actuator wiring in 10.2 must
  derive the `requestId` from it (not a fresh NanoID per call).
- **`executing`-wedge Health surfacing (10.2, operational).** A claim stuck in `executing` past a
  generous bound should surface a Health issue rather than silently re-execute arbitrarily far
  beyond a real adapter's dedup window. 10.1 re-attempts unboundedly (correct against the
  `FakeBackgroundCheck` infinite-dedup adapter); the operational bound + Health issue lands with the
  live dispatch path in 10.2.
- **No-CAS single-writer claim model accepted (§10.3).** The claim record uses no CAS on its
  lifecycle transitions — the `weaver-state` mark serializes dispatch (single writer per `claimId`)
  and Phase 2 is single-instance. (Only the initial `Write` is create-semantic, as a guard against a
  redelivery clobbering a live claim — not a concurrency CAS.) This is per §10.3 and is accepted.

## Change Log

| Date | Change |
|------|--------|
| 2026-06-13 | Review fixes (H1/M3/H2-M2/M1): create-semantic claim `Write` + `ErrClaimExists` (no clobber of a live claim); panic-contained adapter `execute` (panic → `failed`); mandatory `Recover` probe (no silent skip of the landed-resolve check); recovery short-circuits only on `resolved`, a `failed`/`executing` claim re-attempts on the same `claimId` via `ClaimStore.reopen` + `Resolve` accepting a `failed` claim (gap converges, adapter dedups). 4 new unit tests. Docs + 10.2-handoff/deferral notes recorded. |
| 2026-06-13 | Story 10.1 implemented: `internal/weaver/nudge/` External Adapter framework (`Adapter` + registry + `FakeBackgroundCheck`), `weaver-claims` claim store (§10.3 shape, 90d per-key TTL), claim-minting mark-create (`createNudge`) + claimId-carrying reclaim, claim→execute→resolve protocol + read-before-act recovery as a substrate-only callable unit, corrupt-empty-`claimId` reconciler guard, engine Config wiring. Unit-proven incl. same-`claimId` retry → no second side-effect. Docs updated (10.1/10.2 split). All gates green. Status → review. |
