---
title: Story 1.4 Implementation Handoff Brief
story: 1.4 — `internal/substrate` Package
model_tier: Opus (locked)
token_budget: ~110K (input + output combined)
session: Fresh implementation session — first action is reading this brief
architecture_lead: Winston (any architectural question routes to Winston via Andrew)
date: 2026-05-13
---

# Story 1.4 — `internal/substrate` Package: Implementation Handoff Brief

## Your Role

You are the implementing engineer for Story 1.4. This is the **lowest-level shared Go package** in Lattice. Stories 1.5–1.8 (Processor) and 4.x (Identity ops) and many later stories will import this package. **API design quality matters here** — your interfaces will be called by every component.

Winston (architect) and Andrew (PO) have locked the scope and AC. You do not have authority to change story scope, AC, or architectural contracts. If you find a contradiction, document it in `internal/substrate/CONTRACT-AMENDMENT-REQUEST.md` and continue with the story as scoped.

## Operating Rules

- **Model tier:** Opus only. Halt if you detect Sonnet or Haiku.
- **No PRs.** After implementation + Winston review, commit directly to `main`.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is source of truth.
- **No git commits by you.** Winston + Andrew commit after review.
- **Bypass permissions enabled** for `go get`, `go mod tidy`, `go test`.
- **Token tracker update:** at session close, update `_bmad-output/implementation-artifacts/token-usage-tracker.md` Row 1.4 with model, actual usage estimate, session date, notes.

## Story Scope (Copy from `epics.md` — Authoritative)

> As a platform engineer, I want a shared Go package `internal/substrate` that provides NATS connectivity, NanoID generation, KV helpers, and document envelope construction, so that all Lattice components use consistent, contract-compliant primitives without duplicating low-level NATS code.

**Recommended model tier:** Opus
**Estimated token budget:** ~110K

### Acceptance Criteria (from `epics.md` — read in full to confirm no drift)

**Given** the package is imported by any Lattice component
**When** a caller uses `substrate.NewNanoID()`
**Then** the returned ID is exactly 20 characters drawn from the 58-character custom alphabet (A-Za-z0-9 excluding I, l, O, 0); unit-tested with 10,000 generated IDs verifying length and alphabet compliance; no generated ID contains a forbidden character.

**Given** a caller constructs a vertex key using `substrate.VertexKey(vertexType, id)`
**When** the function is called
**Then** the returned string matches `vtx.<type>.<id>` exactly; analogous helpers `substrate.AspectKey(vtxKey, localName)` and `substrate.LinkKey(type1, id1, linkName, type2, id2)` produce correct 4-segment and 6-segment keys; all helpers unit-tested against Contract #1.

**Given** a caller uses `substrate.NewDocumentEnvelope(class, actor, operationType)`
**When** the function is called
**Then** the returned struct contains all mandatory envelope fields — `class`, `isDeleted: false`, `createdAt`, `createdBy`, `createdByOp`, `lastModifiedAt`, `lastModifiedBy`, `lastModifiedByOp` — with correct zero values; `data` is nil until set by caller; struct serializes to JSON without omitting required fields.

**Given** a caller uses `substrate.KVGet(ctx, bucket, key)` and `substrate.KVPut(ctx, bucket, key, value)`
**When** the NATS connection is healthy
**Then** operations complete within configured timeout; when the key does not exist, `KVGet` returns typed `ErrKeyNotFound`; when revision conflict occurs on a conditional put, returns typed `ErrRevisionConflict`.

**And** the package includes a `substrate_test.go` integration test that starts an embedded NATS server (or uses the dev harness from Story 1.3 — your call; embedded is simpler for unit testing) and exercises all helpers end-to-end.

**And** zero external dependencies beyond `go.starlark.net` (for type interop, optional) and the official `nats.go` client are introduced.

## Required Context — Read These Sections Only

| File | Section | Why |
|---|---|---|
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #1 — Addressing Model & Document Envelope | EXACT NanoID spec, key patterns, envelope shape — your implementation must match byte-for-byte |
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #2 (operation envelope) §2.1 op tracker shape ONLY | KVGet error pattern alignment; you do NOT implement Operation Envelope itself |
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #4 — Idempotency Tracker | TTL semantics for KV helpers; per-key TTL must be supported via headers |
| `_bmad-output/planning-artifacts/epics.md` | Story 1.4 (canonical AC) | Source of truth |
| `internal/spike/nats-batch/main.go` | The `publishAtomicBatch` helper (~50 lines) | Reference pattern — port this into substrate as `substrate.AtomicBatch` |
| `internal/spike/nats-batch/README.md` | "API Discovery Note for Story 1.7" | The raw NATS batch headers; substrate must hide this from callers |
| `internal/bootstrap/nanoid.go` | Whole file | The fixed primordial IDs that already exist; substrate's runtime NanoID generator should NOT replace this file's constants but the alphabet should be defined canonically in substrate |

## Architectural Decisions Already Made (Apply Without Re-Litigation)

1. **NanoID spec (locked):** 20 chars from custom 58-char alphabet `ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789` (excludes I, l, O, 0). Per Contract #1. Already used by Story 1.3 bootstrap fixed IDs. Substrate defines this alphabet as the canonical constant; if Story 1.3's `internal/bootstrap/nanoid.go` duplicates it, refactor `internal/bootstrap/nanoid.go` to import the alphabet from substrate. Do NOT replace the bootstrap's fixed-ID constants — those stay; only the alphabet constant moves to substrate.

2. **Atomic batch helper:** Port the `publishAtomicBatch` from `internal/spike/nats-batch/main.go` into substrate as `substrate.AtomicBatch`. The helper must:
   - Accept a slice of messages each with subject, payload, optional revision condition, optional TTL
   - Drive the raw NATS headers (`Nats-Batch-Id`, `Nats-Batch-Sequence`, `Nats-Batch-Commit`) internally
   - Return a typed `ErrAtomicBatchRejected` (with the underlying NATS error code) on failure
   - Be used by Story 1.7 (Processor step 8) and the existing `cmd/bootstrap` (which currently writes individually — refactor bootstrap to use the helper)

3. **Key segment validation:** All key-builder helpers (`VertexKey`, `AspectKey`, `LinkKey`) must validate their inputs:
   - Type segment: lowercase alphanum, no dots
   - ID segment: 20-char NanoID (use `IsValidNanoID(s string) bool` helper)
   - Local name for aspect: lowercase camelCase, no dots
   - Link name: lowercase camelCase, no dots
   - Invalid input → panic (programmer error, not runtime error — keys are constructed from typed Go values, never from user input directly)

4. **Document envelope:** Implements Contract #1. The struct should have JSON tags matching the exact field names in Contract #1 §1.5. The `data` field is `map[string]any` (caller fills). Provide `(e *DocumentEnvelope) Update(actor, opType string)` method for the `lastModified*` triplet update flow used by Processor step 7.

5. **KV helper return types:** `substrate.KVGet` returns `(*KVEntry, error)` where `KVEntry` carries `Key`, `Value []byte`, `Revision uint64`, `Timestamp time.Time`. `substrate.KVPut` returns `(uint64, error)` (new revision). Provide `substrate.KVCreate` for create-if-absent (revision=0 condition), `substrate.KVUpdate` for revision-conditioned update.

6. **NATS connection management:** Provide `substrate.Connect(ctx, opts ConnectOpts) (*Conn, error)` that wraps `nats.Connect`; the returned `*Conn` is what KV helpers take as receiver. Don't make callers manage `nats.Conn`, `jetstream.JetStream`, and `jetstream.KeyValue` separately — substrate hides those layers.

7. **Refactor Story 1.3's bootstrap:** Once substrate exposes envelope construction, key helpers, and atomic batch, refactor `cmd/bootstrap/main.go` + `internal/bootstrap/*.go` to use substrate. Goal: reduce bootstrap's bespoke envelope/key code to zero lines; bootstrap becomes a thin orchestrator on top of substrate. Verify the existing `make verify-bootstrap` still passes 29/29 after refactor.

8. **No external deps:** Beyond `github.com/nats-io/nats.go` (already in go.mod), do NOT add new dependencies. Use stdlib `crypto/rand` for NanoID entropy.

## Suggested Package Layout

```
internal/substrate/
├── doc.go            Package-level godoc
├── nanoid.go         NewNanoID(), IsValidNanoID(), alphabet constant
├── keys.go           VertexKey, AspectKey, LinkKey + parsers + validation
├── envelope.go       DocumentEnvelope struct + NewDocumentEnvelope + Update method
├── conn.go           Connect, *Conn type with KV-aware helpers
├── kv.go             KVGet, KVPut, KVCreate, KVUpdate, KVDelete + typed errors
├── batch.go          AtomicBatch helper (raw headers hidden)
├── errors.go         Sentinel errors: ErrKeyNotFound, ErrRevisionConflict, ErrAtomicBatchRejected
└── *_test.go         Unit + integration tests
```

Refine as you go. Document layout choices in the closing summary.

## Deliverables Checklist

1. ✅ `internal/substrate/` package with the eight files above (or your refined equivalent)
2. ✅ `internal/substrate/*_test.go` — unit tests for NanoID (10K generation), key helpers, envelope, errors; integration tests for KV helpers + AtomicBatch using embedded NATS
3. ✅ `cmd/bootstrap` and `internal/bootstrap/*` refactored to use substrate (alphabet centralized; envelope/key construction calls substrate; atomic batch helper used where bootstrap currently does single writes — only refactor; do NOT alter bootstrap behavior)
4. ✅ `make verify-bootstrap` still passes 29/29 assertions after the refactor
5. ✅ `go build ./...` exits 0; `go vet ./...` exits 0; `go test ./internal/substrate/...` exits 0
6. ✅ `make up && make verify-bootstrap && make down` cycle still works end-to-end
7. ✅ Updated Row 1.4 in `token-usage-tracker.md`
8. ✅ Closing summary documenting any layout choices or open questions

## What Story 1.4 Is NOT

- **Not** the Processor — that arrives in Stories 1.5–1.8
- **Not** a NATS client wrapper that exposes everything — substrate is opinionated, hides what callers don't need
- **Not** a generic Go library — Lattice-specific. NanoID alphabet, key patterns, envelope shape are all Contract #1-specific

## Escalation Path

- If a NATS API doesn't support what the AC assumes → STOP, write a finding, escalate
- If an architecture contract appears wrong → document in `internal/substrate/CONTRACT-AMENDMENT-REQUEST.md` and continue
- If tokens trend past 130K (~18% over budget) → flag before continuing

## Closing the Session

1. Verify all 8 deliverables above
2. Run `go build ./...` and `go vet ./...` from repo root — must exit 0
3. Run `go test ./internal/substrate/...` — must exit 0
4. Run `make down && make up && make verify-bootstrap && make down` — confirm bootstrap still works after refactor
5. Update the token tracker
6. Return closing summary listing all deliverables, refactor outcomes, any open questions, build/test status

Do NOT commit. Winston + Andrew will review and commit.
