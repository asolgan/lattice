---
title: Story 1.5 Implementation Handoff Brief
story: 1.5 — Processor — Consume, Dedup & Auth Stub (Steps 1-3)
model_tier: Opus (locked)
token_budget: ~115K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-13
---

# Story 1.5 — Processor Steps 1-3: Implementation Handoff Brief

## Your Role

You are implementing the first three commit-path steps of the Lattice Processor. Steps 4-10 are implemented by Stories 1.6, 1.7, 1.8 — scaffold the interfaces but stub the bodies in 1.5. **You do not have authority to modify story scope, AC, or planning artifacts.** If you find a contradiction with a contract, raise `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` and continue with the story as scoped.

## Operating Rules

- **Model tier:** Opus only.
- **No PRs.** After Winston review, commit direct to `main`.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is source of truth.
- **No git commits by you.** Winston + Andrew commit.
- **Bypass permissions enabled.**
- **Token tracker update:** Row 1.5 at session close.
- **DO NOT** silently edit planning artifacts — use `CONTRACT-AMENDMENT-REQUEST.md` for any concerns.

## Story Scope (from `epics.md` — authoritative)

> As a platform engineer, I want the first three steps of the Processor commit path — JetStream consumption, idempotency dedup, and auth stub — implemented and integration-tested, so that the Processor can receive, deduplicate, and stub-authorize operations before the full execution pipeline is wired.

**Recommended model tier:** Opus
**Estimated token budget:** ~115K

### Acceptance Criteria (from `epics.md` — read in full to confirm no drift)

**Given** a Processor instance connected to the running NATS dev harness (Story 1.3)
**When** a valid operation envelope is published to `core-operations`
**Then** the Processor consumes it (step 1), checks the Idempotency Tracker KV for the `requestId` (step 2), and proceeds to auth stub (step 3) if the ID is not found.

**Given** the same operation envelope is published a second time with the same `requestId`
**When** the Processor processes the duplicate
**Then** it short-circuits at step 2, emits a `DuplicateDetected` log entry with the `requestId`, does not proceed to auth or execution, and acks the message so it is not redelivered.

**Given** an operation envelope with a `requestId` not yet in the tracker
**When** the auth stub (step 3) is evaluated
**Then** the stub always returns `authorized: true` for any valid envelope; this is explicitly a stub — real Capability KV auth lands in Story 3.3; the stub is feature-flaggable so it can be replaced without changing the step 3 interface.

**And** if the operation envelope fails JSON unmarshaling or is missing required fields (per Contract #2), the Processor nacks with `term: true` (no redelivery), logs the malformed envelope, and emits a `MalformedOperation` health signal to Health KV.

**And** the dedup check and tracker write use a single NATS atomic batch (per Story 1.1 spike findings) to ensure the tracker entry is written before ack.

**And** integration tests cover: first delivery (accepted), duplicate (short-circuited), malformed envelope (terminated), and tracker write failure (crash-safe retry).

## Required Context — Read These Only

| File | Section | Why |
|---|---|---|
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #2 — Operation Envelope (full) | EXACT envelope shape the Processor parses |
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #4 — Idempotency Tracker | Tracker key pattern (`vtx.op.<requestId>`), TTL (24h), atomic batch placement, expiry semantics |
| `_bmad-output/planning-artifacts/lattice-architecture.md` | "10-step commit path" or equivalent section | Commit-path overview; steps 4-10 you stub |
| `_bmad-output/planning-artifacts/epics.md` | Story 1.5 (canonical AC) | Source of truth |
| `internal/spike/nats-batch/README.md` | Test 1 + API Discovery Note | TTL-in-batch behavior; raw header pattern (substrate hides this) |
| `internal/substrate/doc.go` + public API surface | Whole package | The API you must use; do not reimplement KV/batch primitives |
| `cmd/bootstrap/main.go` + `internal/bootstrap/*` | Skim | Reference for how substrate is consumed in practice; cargo-cult patterns where appropriate |

## Architectural Decisions Already Made (apply without re-litigation)

1. **All KV/batch operations go through `internal/substrate`.** Do NOT use `nats.go` or `jetstream` packages directly for KV — use `substrate.Conn`, `substrate.KVGet`, `substrate.KVPut`, `substrate.AtomicBatch`. For JetStream stream consumption (`core-operations` subject) you DO use the `jetstream` client directly — substrate does not yet wrap stream consumers.

2. **Processor binary at `cmd/processor/main.go`.** Configurable via env vars / flags. Logs to stderr in structured form (use `log/slog`, matching Story 1.3's bootstrap binary style).

3. **Auth stub interface:** define `type Authorizer interface { Authorize(ctx, *OperationEnvelope) (Decision, error) }` with a `StubAuthorizer` implementation that always returns `Decision{Authorized: true, Stub: true}`. Feature-flag selection via env var `LATTICE_AUTH_MODE` (values: `stub` default, `capability` reserved for Story 3.3). Log a warning when stub mode is active.

4. **Tracker entry shape:** the tracker key `vtx.op.<requestId>` holds a small JSON document — at minimum `{class: "op-tracker", isDeleted: false, requestId, committed: true|false, observedAt}`. For Story 1.5 (steps 4-10 stubbed), `committed` is always `true` in the tracker — the absence of mutations means the operation is conceptually "committed as a no-op." Story 1.7's atomic batch will refine this when real mutations are added.

5. **TTL on tracker entry:** 24 hours (`24*time.Hour`) per Contract #4. Use substrate's atomic-batch TTL header support. NATS minimum is 1 second, 24h is well above that.

6. **Atomic batch on Story 1.5's commit path:** the batch contains exactly ONE message — the tracker write. Future stories (1.7) add the real mutations alongside. The batch path must work today with a single-message payload.

7. **Steps 4-10 stubbing:** define them as Go interfaces (`Hydrator`, `Executor`, `Validator`, `EventPublisher`, `Acker` — or your refined naming) and stub implementations that log "step N: stubbed" and return success. Story 1.6 swaps in real implementations behind these interfaces. Do NOT do their work in 1.5; this is the most common scope-creep failure mode.

8. **Malformed envelope handling:** detected at step 1 (parse). Nack with `term: true` (NATS API `msg.TermWithReason(...)`). Emit `health.processor.<instance>.malformed-operation.<requestId>` if requestId is parseable, otherwise log without health record (no requestId to key it under).

9. **Health emissions:** Processor instance writes `health.processor.<instance>` periodically (every 10s minimum per NFR-O1). Include: ops processed, duplicates detected, malformed count, last activity timestamp. Use the same pattern as Story 1.3's refractor-stub heartbeat-style writes.

10. **JetStream durable consumer name:** `processor-main` (configurable via env). Subject filter: `core-operations.>` (or whatever the bootstrap created — confirm by reading `cmd/bootstrap/main.go`).

11. **Reply semantics for the operation submitter:** when an operation completes step 8 (atomic batch commit), the Processor returns a reply envelope (Contract #2 §2.x). In Story 1.5 with steps 4-10 stubbed, return a stub reply per Contract #2 — reply envelope with `decision: "accepted-stub", trackerKey: "vtx.op.<id>"`. Story 1.7 swaps in real reply construction.

## Suggested Layout

```
cmd/processor/
└── main.go              Entry point: parse flags, create deps, start consumer
internal/processor/
├── doc.go               Package overview
├── envelope.go          Operation envelope parsing (per Contract #2)
├── step1_consume.go     JetStream message receive + parse
├── step2_dedup.go       Tracker lookup
├── step3_auth.go        Authorizer interface + StubAuthorizer
├── steps_4_10_stub.go   Stubbed interfaces for downstream steps
├── commit_path.go       Top-level driver that wires steps 1-3 + stubbed 4-10
├── reply.go             Reply envelope construction
├── tracker.go           Tracker entry shape + atomic batch construction
├── health.go            Periodic health emission
└── *_test.go            Unit + integration tests using embedded NATS or Docker harness
```

## Deliverables Checklist

1. ✅ `cmd/processor/main.go` — runnable binary that connects to NATS, starts consumer
2. ✅ `internal/processor/` package — clean separation of concerns per suggested layout
3. ✅ Integration test suite covering: first delivery accepted, duplicate short-circuited, malformed envelope nack-with-term, tracker-write-failure retry-safe
4. ✅ `Makefile` updated with `make processor` (build + run locally against `make up` harness) — or document the run command if you prefer not to add a Make target
5. ✅ `make verify-bootstrap` still passes 29/29 (you should not be touching bootstrap; this is just a regression check)
6. ✅ `go build ./...` exits 0; `go vet ./...` exits 0; `go test ./internal/processor/...` exits 0
7. ✅ Updated Row 1.5 in token tracker
8. ✅ Closing summary

## What Story 1.5 Is NOT

- **Not** the full Processor — steps 4-10 are stubbed
- **Not** the real auth path — that's Story 3.3 (your stub interface must be swappable)
- **Not** the Starlark execution layer — that's Story 1.6
- **Not** business mutations — Story 1.5's atomic batch only contains the tracker write

## Escalation

- NATS/contract surface gap → stop, write finding, escalate
- Contract contradiction → `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md`
- Token usage past 135K (~18% over) → flag before continuing

## Closing

1. Verify all 8 deliverables
2. `go build ./...`, `go vet ./...`, `go test ./internal/processor/...` exit 0
3. `make down && make up && make verify-bootstrap && make down` still works (regression check)
4. Run a small end-to-end exercise: `make up`, start processor, publish a test envelope via `nats` CLI or a small Go test client, observe consume/dedup/stub-auth behavior in logs
5. Update token tracker Row 1.5
6. Return closing summary with: deliverables present (yes/no), e2e behavior demonstrated, any contract amendments raised, token estimate, build/test status, any open questions

Do NOT commit. Winston + Andrew review and commit.
