---
title: Story 1.8 Implementation Handoff Brief
story: 1.8 — Processor — Event Publication & Fault Injection (Steps 9-10)
model_tier: Opus (locked)
token_budget: ~145K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-14
---

# Story 1.8 — Processor Steps 9-10 + Fault Injection: Implementation Handoff Brief

## Your Role

You implement steps 9 (Event Publication) and 10 (JetStream Ack) of the Processor commit path, AND deliver the `FailAfterN` fault injection harness that proves NFR-R1 (crash-recoverability) at every one of the 10 steps. After Story 1.8, the Processor's hot path is real end-to-end and Phase 1 Gate 2 (Story 1.10) has the substrate it needs.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

**Pattern across Stories 1.1, 1.5, 1.6, 1.7: sub-agents have self-reported tokens 30-50% under actual outer telemetry.** Story 1.7 self-reported 155K but outer showed 204K (32% gap, 40% over budget). Your self-estimate is NOT reliable.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a status message containing deliverables completed, deliverables remaining, your honest token estimate, AND mark it explicitly as a "checkpoint message".
- **Treat your self-estimate as a LOWER BOUND, round UP.** If you "feel like" you've used 80K, report 110K.
- **Halt unconditionally if you estimate > 150K used** (5% over budget). Wait for explicit Winston greenlight.

Other rules (unchanged):
- **Model tier:** Opus only. Halt if Sonnet/Haiku.
- **No PRs.** Direct commit to `main` after Winston review.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is source of truth.
- **DO NOT silently edit planning artifacts.** Use `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` (append).
- **All KV/JetStream ops through `internal/substrate`.** If you need a new substrate primitive, add it there — don't reach around it.
- **No git commits by you.** Winston + Andrew commit.
- **Token tracker:** update Row 1.8 at session close with HONEST estimate (round UP).

## Story Scope (from `epics.md` lines 489-514 — authoritative)

> As a platform engineer, I want steps 9 and 10 of the Processor commit path — JetStream event publication and JetStream ack — implemented with a fault injection harness, so that the complete 10-step commit path is crash-recoverable and NFR-R1 is validated.

**Recommended model tier:** Opus
**Estimated token budget:** ~145K

### Acceptance Criteria (verbatim from epics.md)

**Given** the atomic batch commit (step 8) has succeeded
**When** step 9 (event publication) runs
**Then** each event in the EventList is published to the appropriate JetStream subject; publication is ordered (events published in EventList sequence); if publication fails for any event, the step retries up to the configured maximum before surfacing a `PublicationError`; partial event publication (some events published, some not) is not possible — the step uses a batch publish.

**Given** event publication succeeds
**When** step 10 (JetStream ack) runs
**Then** the original operation message is acked to JetStream; the operation is removed from the durable consumer's pending set; no redelivery occurs.

**Given** a `FailAfterN` fault injector wrapper is applied to the JetStream client
**When** fault injection is triggered at each of the 10 commit path steps (one test per step)
**Then** after the injected failure, the Processor restarts (simulated via restart of the consumer loop), reprocesses the operation from JetStream (redelivery), and produces the same final state as a clean run; no partial state is visible in Core KV that would not exist after a clean run; the idempotency tracker prevents double-application of any already-committed step.

**And** the fault injection harness is implemented as `internal/testutil/faultinjector.go` with a `FailAfterN(n int) JetStreamPublisher` constructor.
**And** the complete 10-step happy path is covered by a single integration test that publishes one operation, traces it through all 10 steps, and asserts final Core KV state, Idempotency Tracker entry, and emitted events.
**And** NFR-R1 is marked verified in the test suite output upon successful fault injection at all 10 steps.

## Required Context — Read These Only

| File | Section | Why |
|---|---|---|
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #3 §3.4 (EventList) + Contract #2 §2.3 (subjects) | Event shape + JetStream subject conventions |
| `_bmad-output/planning-artifacts/data-contracts.md` | Lines 524 (durability anchor: reply after step 8, NOT step 10) | Confirms step 9/10 are post-reply; redelivery-safe |
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #4 (Idempotency Tracker) | Why redelivery is safe — tracker short-circuit |
| `internal/spike/nats-batch/README.md` | "API Discovery Note" | Atomic batch behavior baseline |
| `internal/substrate/batch.go` | Whole file | Where you may add a JetStream-only batch publish helper if needed (see Decision #1) |
| `internal/substrate/conn.go` | Whole file | `Conn.JS()` access for raw JetStream publish |
| `internal/processor/commit_path.go` | Whole file | Where step 9 + 10 plug in (currently `cp.deps.Events.Publish` at line 247, ack via consumer's natural ack) |
| `internal/processor/steps_4_10_stub.go` | `EventPublisher`, `Acker` interfaces; `StubEventPublisher` | Interfaces you implement; stub you replace |
| `internal/processor/step1_consume.go` | Whole file | Where the JetStream msg ack happens today |
| `internal/processor/step2_dedup.go` + `tracker.go` | Whole files | Tracker short-circuit (the foundation of redelivery safety) |
| `internal/processor/step7_events.go` | Whole file | EventList shape your publisher consumes |
| `internal/processor/step8_commit.go` | Whole file | Where ConflictError + DDLViolation map; pattern for typed-error returns |
| `internal/bootstrap/primordial.go` | Lines 134-148 (provisionStreams) | Where to add `core-events` stream |

**DO NOT read** the full `epics.md`, `lattice-architecture.md`, or other contracts beyond the rows above. The brief is self-contained.

## Architectural Decisions Already Made (Winston)

1. **`core-events` stream:** Provision a new JetStream stream in `internal/bootstrap/primordial.go::provisionStreams` with:
   - `Name: "core-events"`
   - `Subjects: ["events.>"]`
   - Retention: `LimitsPolicy`, MaxAge: 7 days (Phase 1 default — events are short-lived per Contract #3 lifetime norms)
   - Storage: file
   Add a `CoreEventsStreamName = "core-events"` and `EventsWildcardSubject = "events.>"` constant alongside the existing `CoreOpsStreamName`. Update `verify-bootstrap` to assert the new stream exists.

2. **Per-event subject:** `events.<class>` where `<class>` is the event's `class` field from EventList. Example: `events.identity.created`. Subject sanitization: replace any non-subject-token chars (whitespace, `>`, `*`) in `class` with `_` — but in practice DDL class names already conform.

3. **Step 9 publish strategy — sequential with retry, NOT atomic batch.** Reasoning:
   - The atomic-batch primitive (`substrate.AtomicBatch`) is for **KV** atomicity within ONE stream. Events go to a DIFFERENT stream (`core-events`). Cross-stream atomicity is not what NATS batches give us.
   - The AC line "partial event publication is not possible — the step uses a batch publish" is satisfied by **`substrate.PublishBatch`** (the JetStream-side batch that Story 1.1's spike already validated; it's the same mechanism the atomic-batch uses for the *publish* phase but without revision conditions). If `substrate` doesn't currently expose a non-conditional JetStream batch publisher, ADD one as a new method on `Conn`: `PublishBatch(subject string, msgs [][]byte, timeout time.Duration) ([]BatchAck, error)`, or `PublishBatchHeterogeneous([]PublishOp) ([]BatchAck, error)` if heterogeneous subjects are needed. Implement it per the spike's pattern (`Nats-Batch-Id` + `Nats-Batch-Sequence` + `Nats-Batch-Commit` headers).
   - Configure retry: `maxRetries = 3` with exponential backoff (50ms, 200ms, 800ms), surfaced via a `PublicationError` typed error carrying `EventClass`, `Subject`, `Attempts`, `LastErr`.
   - If the batch publish fails entirely after retries, return `PublicationError`. The commit_path will NOT ack (step 10 won't run); JetStream redelivery + Story 1.5 tracker dedup will re-attempt — the tracker entry from step 8 makes this safe (step 9 retry on a duplicate-detected redelivery is a no-op because step 2 short-circuits).

4. **Step 10 (Acker):** Replace `step1_consume.go`'s natural `msg.Ack()` with an explicit `Acker` boundary that the commit_path invokes after `Events.Publish` returns nil. This makes fault injection at step 10 testable. The Acker stores the `jetstream.Msg` reference (passed via context or a per-message struct) and calls `Ack(ctx)` on it. If ack fails, log + return — JetStream will redeliver, and the tracker short-circuits.

5. **Fault injection harness — `internal/testutil/faultinjector.go`:**
   - `FailAfterN(n int)` returns a wrapper type that counts step-boundary calls and panics/errors after the Nth.
   - The wrapper is applied to one of: the JetStream `Conn` (for steps 1, 8, 9, 10), the Hydrator (step 4), the Executor (step 5), the Validator (step 6+7), the Committer (step 8).
   - For uniformity, define `FailAfterN[T any](inner T, failOnCall int, failErr error) T` as a generic interface-passthrough wrapper, OR define one wrapper per Processor interface (Hydrator, Executor, Validator, Committer, EventPublisher, Acker). Pick whichever is cleaner; the AC only requires the constructor name `FailAfterN`.
   - Crash semantics: on Nth call, the wrapper returns `errors.New("fault injected at step X call N")` — NOT panic — so the commit_path's existing error-return discipline triggers redelivery cleanly.

6. **NFR-R1 verification:** A single test file `internal/processor/nfr_r1_test.go` runs ten subtests (`TestNFR_R1_FaultAtStep1` … `TestNFR_R1_FaultAtStep10`). Each:
   - Publishes one operation
   - Injects a fault at the step under test
   - Asserts the operation is NACKed/redelivered (or returns an error that the commit_path handles → no ack)
   - Removes the fault, lets redelivery succeed
   - Asserts final Core KV state matches a fault-free baseline (use a helper `runCleanBaseline(t)` that captures the post-commit Core KV snapshot for diff)
   - Asserts the tracker has exactly one committed entry (no double-application)
   - The test file's `TestMain` (or a top-level assertion in a roll-up test) prints `NFR-R1: VERIFIED (10/10 steps)` on success.

7. **Complete 10-step happy path test:** `internal/processor/step10_e2e_test.go` (NEW). Publishes one fully-formed operation that produces one mutation + one event. Traces all 10 steps via the structured logger (assert log lines for steps 1-10 in order). Asserts:
   - Core KV: the mutation document exists at its expected key with correct revision
   - Idempotency Tracker: `vtx.op.<requestId>` exists with `committed: true`, `mutationKeys` populated, `eventClasses` populated, 24h TTL
   - `core-events`: the published event is durably stored — subscribe ephemerally to `events.<class>` and assert exactly one message with the expected payload

8. **Order-preserving publish:** EventList order is preserved in the batch publish because `substrate.PublishBatch` uses sequential headers (`Nats-Batch-Sequence: 1, 2, 3…`). Events 1..N land on `core-events` in that order. Verify in the e2e test by publishing two events and asserting consumed sequence.

9. **No new Contract amendments expected.** If you find one, append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` (don't overwrite). Escalate before resolving.

10. **Keep `StubEventPublisher` removed** from the wired commit_path. You may keep the type itself in `steps_4_10_stub.go` if any existing test relies on it (similar to Story 1.7's retention of `StubCommitter` for race tests) — but `MakeStubPipeline` should default to the real publisher.

## Suggested Layout

```
internal/processor/
├── step9_publish.go         NEW: EventPublisherImpl using substrate batch publish
├── step9_publish_test.go    NEW
├── step10_ack.go            NEW: AckerImpl wrapping jetstream.Msg.Ack
├── step10_ack_test.go       NEW
├── nfr_r1_test.go           NEW: 10 fault-injection subtests
├── step10_e2e_test.go       NEW: full 10-step happy path

internal/testutil/
├── faultinjector.go         NEW: FailAfterN wrappers

internal/substrate/
├── batch.go                 EXTEND if needed: add non-conditional PublishBatch helper

internal/bootstrap/
├── primordial.go            EDIT: provision core-events stream
```

Modifies: `commit_path.go` (wires real EventPublisher + real Acker; threads `jetstream.Msg` to Acker), `step1_consume.go` (defers ack to commit_path), `steps_4_10_stub.go` (drops StubEventPublisher from default wiring), `scripts/verify-bootstrap.go` (assert `core-events` stream).

## Deliverables Checklist

1. ✅ `core-events` stream provisioned in bootstrap + verify-bootstrap assertion (`make verify-bootstrap` 31+ assertions)
2. ✅ `internal/substrate` non-conditional `PublishBatch` (if not already present) + tests
3. ✅ `step9_publish.go` EventPublisherImpl with sequential-with-retry batch publish + PublicationError typed error + tests
4. ✅ `step10_ack.go` AckerImpl + tests
5. ✅ `commit_path.go` updated: real Events + Acker wired; `jetstream.Msg` threaded to step 10; default `MakeStubPipeline` (rename to `MakePipeline` if appropriate) uses real implementations
6. ✅ `step1_consume.go` no longer eagerly acks — defers to step 10 boundary
7. ✅ `internal/testutil/faultinjector.go` with `FailAfterN` constructor(s)
8. ✅ `nfr_r1_test.go` — 10 subtests, prints `NFR-R1: VERIFIED (10/10 steps)` on full pass
9. ✅ `step10_e2e_test.go` — single integration test covering all 10 steps; asserts Core KV, tracker, and `core-events` content
10. ✅ `make verify-bootstrap` green (now 31+ assertions including core-events)
11. ✅ `go build ./...`, `go vet ./...`, `go test ./internal/processor/... ./internal/substrate/... ./internal/testutil/... -count=1` exit 0
12. ✅ Token tracker Row 1.8 updated — HONEST estimate, round UP
13. ✅ Closing summary with checkpoint-protocol token estimate

## What Story 1.8 Is NOT

- **Not** event DDL validation. Contract #3 §3.4 says events MUST have registered DDLs and step 7 validates them — Story 1.7's step 7 deferred this. Story 1.8 publishes whatever the EventList contains; event-DDL validation is folded into Story 1.10's bypass test suite work (or earlier if Andrew requests).
- **Not** FR57 write-scope (Story 1.9).
- **Not** the bypass test suite (Story 1.10).

## Escalation

Halt and escalate to Winston via Andrew if:
- JetStream batch publish behavior differs from Story 1.1 spike findings
- A structural problem appears with fault-injection at any one of the 10 steps that can't be resolved within the brief's pattern
- Token estimate exceeds 150K
- Any Contract amendment is required

## Closing

1. Verify all 13 deliverables
2. Full reset: `make down && make up && make verify-bootstrap` green
3. Full e2e: run `step10_e2e_test.go` and `nfr_r1_test.go` against the live Docker NATS
4. Manual e2e: publish a real operation through `cmd/processor`, observe steps 1→10 in logs, confirm event landed on `core-events`
5. Update token tracker Row 1.8 — round UP
6. Closing summary: deliverables present, e2e observations, fault-injection results (10/10), token estimate (honest), open questions

Do NOT commit. Winston + Andrew review and commit.
