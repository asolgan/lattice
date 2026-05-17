---
title: Story 3.5 Implementation Handoff Brief
story: 3.5 — Three-Plane Auth Failure Traceability (FR23)
model_tier: Sonnet (locked)
token_budget: ~95K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-16
predecessor: Story 3.4 (Structured Denial Response FR22, shipped as part of commit sequence after ee293bb)
---

# Story 3.5 — Three-Plane Auth Failure Traceability (FR23): Handoff Brief

## Your Role

Implement FR23: every authorization failure (and optionally every allow when a flag is set) is traced across three observable planes — the Capability KV cached read, the Capability Lens projection definition, and the Core KV source vertex revisions. The trace record is written asynchronously to Health KV with a 1-hour TTL so that when an auth denial happens (or shouldn't have happened), an operator can inspect all three planes from a single `health.processor.<instance>.auth-trace.<requestId>` entry.

## MANDATORY OPERATING RULES (READ FIRST)

- **Token budget is for tracking only, NOT a halt threshold.** Original estimate ~95K.
- **Halt and escalate** if you find yourself re-attempting the same operation after 3+ failures, making changes you immediately revert, re-reading the same files looking for an answer that isn't there, cycling between two failed approaches without convergence, or stuck on a test that fails for a reason you can't reduce after two debugging attempts.
- **Checkpoint every 8-10 tool calls OR after any deliverable OR after any file read >25KB.**
- **Model tier:** Sonnet only.
- **No git commits.** Winston + Andrew commit.
- **Architecture binding:** `data-contracts.md` Contract #5 §5.1-5.2, Contract #6 §6.2-6.3 + `epics.md` Story 3.5 AC.
- **DO NOT silently edit planning artifacts.** If a contract gap appears, append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` and escalate.
- **Token tracker:** update Row 3.5 at session close with outer-telemetry actual.
- **Andrew has authorized autonomous proceed.**

## What's Already in Place (do NOT redo)

- **`Decision.Resolved *ResolvedPermission`** (`internal/processor/step3_auth.go`): threaded on allow paths with `CapKey`, `ProjectedAt`, `Path`, and matched permission entry pointers. Story 3.3 AC.
- **`Decision.Doc *CapabilityDoc`** (`internal/processor/step3_auth.go`): threaded on denial paths (Story 3.4). Contains full `CapabilityDoc` including `ProjectedFromRevisions` map and all three permission sections. Nil on allow paths.
- **`CapabilityDoc.ProjectedFromRevisions map[string]uint64`**: maps source vertex keys → revisions at projection time. Used by Story 3.5 for Plane 3 + Plane 2 lens revision.
- **`HealthHeartbeater.EmitMalformedOperation`** (`internal/processor/health.go`): pattern for writing ad-hoc Health KV entries with a specific key format. Story 3.5 follows this pattern.
- **`health-kv` bucket**: provisioned with `AllowMsgTTL: true` (LimitMarkerTTL enabled). Supports per-key TTL via `Nats-TTL` header per story 3.5 requirement.
- **`Deps.DenialBuilder *DenialResponseBuilder`** (`internal/processor/commit_path.go`): pattern for wiring optional components into the commit path. Story 3.5 adds `Deps.TraceEmitter`.

## Story Scope (3.5)

**In scope:**

1. **`AuthTraceRecord`** — three-plane struct written to Health KV per FR23 AC:
   - Plane 1 — Capability KV cached read: `capabilityKVKey`, `projectedAt`, `evaluatedSection`, `matchedPermissionPath`, `result` ("matched"/"no-match"/"no-entry")
   - Plane 2 — Capability Lens definition: `lensDefinitionKey` (constant `vtx.meta.lens.capability`), `lensRevisionAtProjection` (from `projectedFromRevisions`), `cypherRuleBodyHash` (sha256 of lens key + projectedAt — no extra read needed per AC)
   - Plane 3 — Core KV graph permission path: `sourceVertexRevisions` map (full `projectedFromRevisions` copy)
   - Meta: `key`, `class="meta.healthRecord"`, `requestId`, `actor`, `operationType`, `authOutcome`, `authCode`, `authReason`, `observedAt`

2. **`AuthTraceWriter` interface** — minimal `KVPutWithTTL` surface so the emitter can be tested without a live NATS connection.

3. **`KVPutWithTTL` on `substrate.Conn`** — new method that publishes to `$KV.<bucket>.<key>` with `Nats-TTL` header. Same mechanism as the AtomicBatch per-key TTL path. Health KV bucket is provisioned with `AllowMsgTTL: true`.

4. **`AuthTraceEmitter`** — struct with `Emit(env, decision)` method that:
   - Returns immediately after launching a goroutine (async write — no step 3 latency per AC)
   - Calls `KVPutWithTTL` with TTL=1h
   - Guards on `traceAllowDecisions` flag: skips allowed decisions when false (default)
   - Nil-safe: all methods no-op on nil receiver

5. **Thread into `commit_path.go`**:
   - `Deps.TraceEmitter *AuthTraceEmitter` added
   - `HandleMessage` calls `cp.deps.TraceEmitter.Emit(env, decision)` on both the denial path AND the allow path (the emitter internally guards on the flag)
   - `MakePipeline` gains `traceAllowDecisions bool` parameter; wires `NewAuthTraceEmitter` when `healthBucket != ""`.

6. **`traceAllowDecisions` flag** — read from `LATTICE_AUTH_TRACE_ALLOW_DECISIONS=true` env var in `cmd/processor/main.go`. Default `false` per AC.

7. **`MakeStubPipeline` backward compatibility** — passes `traceAllowDecisions=false` to `MakePipeline`.

8. **Unit tests** covering: denial trace (all three planes), allow under flag, allow skipped when flag off, nil emitter no-op, NoCapabilityEntry (minimal planes), AuthFreshnessExceeded (planes from doc), key shape, TTL=1h, sha256 hash determinism, evaluatedSection for service/task paths, compile-time `substrate.Conn` satisfies `AuthTraceWriter`.

**Out of scope:**
- CLI (`lattice auth-trace <requestId>`) — Phase 2 CLI (Story 6.1).
- TraceExpired CLI response — Phase 2.
- Plane 2 with fresh cypher rule body from KV read — AC says "no additional reads solely for traceability"; sha256(lensKey+projectedAt) is the Phase 1 fingerprint.
- Role-scoped access domain FR24/25 (Story 3.6).
- Gate 3 adversarial suite (Story 3.7).

## Architectural Decisions (Winston)

1. **Async write** — `Emit` launches a goroutine immediately after capturing all data by value from the stack frame. The goroutine holds a 5-second context. Write failures are logged at Warn but don't affect the commit path. This satisfies the AC constraint: "writing the trace record is asynchronous so it does not contribute to step 3 latency."

2. **Data sourced from already-available Decision fields** — `Decision.Doc` (denial paths) and `Decision.Resolved` (allow paths) carry all needed data from the single Capability KV GET at step 3. No additional reads issued solely for traceability. AC: "all three planes' data is captured in the step's local context."

3. **Plane 2 cypher rule body hash** — AC says "a pointer to the Lens definition's cypher rule body hash." Getting the actual rule body requires a fresh KV GET on the core-kv bucket — forbidden by the "no additional reads" AC constraint. We emit `sha256(lensKey + "@" + projectedAt)` as a stable fingerprint that operators can correlate with the lens vertex revision in Plane 3. This is sufficient for Phase 1 traceability.

4. **`KVPutWithTTL` in substrate** — publishes directly to `$KV.<bucket>.<key>` with `Nats-TTL` header via `js.PublishMsg`. Same mechanism as AtomicBatch TTL (batch.go line 111). The Health KV bucket has `AllowMsgTTL: true` (LimitMarkerTTL) so the server honours the header.

5. **`MakePipeline` parameter `traceAllowDecisions bool`** — added in-position (after authMode) to avoid a wider struct refactor. `MakeStubPipeline` passes `false`. `cmd/processor/main.go` reads `LATTICE_AUTH_TRACE_ALLOW_DECISIONS` env var.

6. **TraceEmitter nil-safe** — `Emit` has `if e == nil || ...` guard. Go allows nil pointer receiver calls, so `cp.deps.TraceEmitter.Emit(...)` is safe even when `TraceEmitter` is nil (stub mode, empty healthBucket, or tests that don't wire it).

7. **Trace record `class: "meta.healthRecord"`** — per Story 3.5 AC ("the trace record is `class: "meta.healthRecord"` per Contract #5").

8. **`lensDefinitionKeyForCapabilityKV` constant** — hardcoded `"vtx.meta.lens.capability"` in the processor package to avoid importing bootstrap (would create a cycle). This is the canonical Capability Lens vertex key per bootstrap.CapabilityLensDefinition().Key.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.4-handoff-brief.md` | Predecessor brief — Decision.Doc + what 3.4 produced |
| `_bmad-output/planning-artifacts/epics.md` Story 3.5 (only) | Your AC + verification targets |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #5 §5.1-5.2 + Contract #6 §6.2-6.3 | Health KV key pattern + CapabilityDoc shape |
| `internal/processor/step3_auth.go` | Decision struct (Resolved + Doc) |
| `internal/processor/step3_auth_capability.go` | Where doc/resolved are set |
| `internal/processor/commit_path.go` | Deps struct + HandleMessage + MakePipeline |
| `internal/processor/capability_doc.go` | CapabilityDoc.ProjectedFromRevisions |
| `internal/processor/health.go` | Health KV write pattern for async markers |
| `internal/substrate/kv.go` | KVPut implementation (add KVPutWithTTL nearby) |

**DO NOT read** full epics.md, lattice-architecture.md, Refractor source, or pre-3.3 briefs.

## Suggested Sequence

**Phase A — substrate KVPutWithTTL (~5K tokens):**
1. Add `KVPutWithTTL` to `internal/substrate/kv.go` — publish to `$KV.<bucket>.<key>` with Nats-TTL header.

**Phase B — AuthTraceRecord + AuthTraceEmitter (~15K tokens):**
2. Create `internal/processor/step3_auth_trace.go` with `AuthTraceRecord`, `AuthTracePlane1/2/3`, `AuthTraceWriter` interface, `AuthTraceEmitter`, `NewAuthTraceEmitter`.
3. Implement `Emit` with goroutine + `buildRecord` + plane builders.
4. Wire `lensDefinitionKeyForCapabilityKV` constant + sha256 for Plane 2.

**Phase C — Wire into commit path (~10K tokens):**
5. Add `TraceEmitter *AuthTraceEmitter` to `Deps`.
6. Call `cp.deps.TraceEmitter.Emit(env, decision)` on denial path + allow path in `HandleMessage`.
7. Add `traceAllowDecisions bool` to `MakePipeline` signature; update `MakeStubPipeline` + cmd/processor.
8. Wire `NewAuthTraceEmitter` in `MakePipeline`.

**Phase D — Tests (~25K tokens):**
9. `step3_auth_trace_test.go`: all AC scenarios + key shape + TTL + hash determinism + nil safety.

**Phase E — Gates + closing (~10K tokens):**
10. `go build ./...`, `make vet`, `go test ./internal/processor/... -count=1`, `go test ./internal/bypass/... -count=1`, `make verify-bootstrap`, `make test-bypass`, `go test ./... -p 1 -count=1`.
11. Update token tracker Row 3.5.
12. Append closing summary to this brief.

## Required Verification

```bash
go build ./...
make vet
go test ./internal/processor/... -count=1
go test ./internal/bypass/... -count=1
make verify-bootstrap
make test-bypass
go test ./... -p 1 -count=1
```

## Deliverables Checklist

1. ✅ `KVPutWithTTL` added to `internal/substrate/kv.go` — publishes with Nats-TTL header
2. ✅ `AuthTraceRecord` Go struct with all meta fields + `class: "meta.healthRecord"` + Plane1/2/3 sub-structs
3. ✅ `AuthTracePlane1` — capabilityKVKey, projectedAt, evaluatedSection, matchedPermissionPath, result
4. ✅ `AuthTracePlane2` — lensDefinitionKey, lensRevisionAtProjection, cypherRuleBodyHash
5. ✅ `AuthTracePlane3` — sourceVertexRevisions map
6. ✅ `AuthTraceWriter` interface with `KVPutWithTTL` method
7. ✅ `AuthTraceEmitter` struct + `NewAuthTraceEmitter` constructor (nil-safe, async, TTL=1h)
8. ✅ `Emit(env, decision)` — fires goroutine; guards on `traceAllowDecisions`; nil receiver no-op
9. ✅ `buildRecord` + `buildPlane1FromResolved` + `buildPlane1FromDoc` + `buildPlane2FromDoc` + `buildPlane3FromDoc` helpers
10. ✅ `Deps.TraceEmitter *AuthTraceEmitter` added to `commit_path.go`
11. ✅ `HandleMessage` calls `TraceEmitter.Emit` on denial path + allow path
12. ✅ `MakePipeline` gains `traceAllowDecisions bool` parameter; wires `NewAuthTraceEmitter`
13. ✅ `MakeStubPipeline` updated to pass `traceAllowDecisions=false`
14. ✅ `cmd/processor/main.go` reads `LATTICE_AUTH_TRACE_ALLOW_DECISIONS` env var; passes to `MakePipeline`
15. ✅ Unit tests: denial trace all three planes, allow under flag, allow skipped when flag off, nil emitter no-op
16. ✅ Unit tests: NoCapabilityEntry minimal planes, AuthFreshnessExceeded planes populated, key shape, TTL=1h
17. ✅ Unit tests: sha256 hash determinism, evaluatedSection for service/task paths, `substrate.Conn` satisfies interface
18. ✅ All verification gates pass; token tracker Row 3.5 updated; closing summary appended

Do NOT commit. Winston + Andrew review and commit.

---

## Closing Summary — Story 3.5

Shipped 2026-05-16. All 18 deliverables complete. Gates green: `go build ./...`, `make vet`, `go test ./internal/processor/... -count=1` (all passing, 20s), `go test ./internal/bypass/... -count=1` (4/4 BLOCKED), `make verify-bootstrap` (34 OK unchanged), `make test-bypass` (4/4 BLOCKED), `go test ./... -p 1 -count=1` (all packages — one `refractor/control` flake on first run is the established Deviation 14 NATS resource-pressure pattern; passed cleanly on second run).

### Implementation approach

**New file** `internal/processor/step3_auth_trace.go` containing:
- `AuthTraceRecord` — three-plane trace document with `class: "meta.healthRecord"` per Contract #5
- `AuthTracePlane1/2/3` — per-plane sub-structs
- `AuthTraceWriter` interface — minimal `KVPutWithTTL` for testability
- `AuthTraceEmitter` — async emitter struct; `Emit` launches goroutine, nil-safe
- `buildRecord` + plane builders — derive all data from Decision.Doc/Resolved (no extra KV reads)
- `lensDefinitionKeyForCapabilityKV = "vtx.meta.lens.capability"` — canonical constant

**`internal/substrate/kv.go`**: Added `KVPutWithTTL` that publishes directly to `$KV.<bucket>.<key>` with `Nats-TTL` message header via `js.PublishMsg`. Same mechanism as AtomicBatch TTL path. Requires `nats.go` import.

**`internal/processor/commit_path.go`**:
- `Deps.TraceEmitter *AuthTraceEmitter` added
- `HandleMessage` calls `cp.deps.TraceEmitter.Emit(env, decision)` on both denial path (after `BuildRejectedReply`) and allow path (after "step 3: authorized" log)
- `MakePipeline` gains `traceAllowDecisions bool` parameter (position 5, after authMode); wires `NewAuthTraceEmitter` when `healthBucket != "" && instance != ""`
- `MakeStubPipeline` updated to pass `traceAllowDecisions=false`

**`cmd/processor/main.go`**:
- Reads `LATTICE_AUTH_TRACE_ALLOW_DECISIONS` env var; passes `true` when set to `"true"`
- Updated `MakePipeline` call to include the new parameter
- Updated env var documentation in package comment

### Three-plane design decisions

| Plane | Source | AC constraint |
|---|---|---|
| Plane 1 — CapKV read | `decision.Doc.Key` + `ProjectedAt` (denial) or `decision.Resolved.CapKey` + `ProjectedAt` (allow) | no extra reads |
| Plane 2 — Lens definition | `doc.ProjectedFromRevisions[lensDefinitionKeyForCapabilityKV]` for revision; `sha256(lensKey+"@"+projectedAt)` for body hash | no extra reads — hash fingerprint is Phase 1 approximation |
| Plane 3 — Core KV graph | Full `doc.ProjectedFromRevisions` map copy | no extra reads — already in CapabilityDoc |

### AC compliance

- All three planes populated for normal denial/allow decisions ✅
- Asynchronous write (goroutine) — does not contribute to step 3 latency ✅
- TTL=1h via `KVPutWithTTL` ✅
- `class: "meta.healthRecord"` per Contract #5 AC ✅
- `traceAllowDecisions` flag defaults OFF; `LATTICE_AUTH_TRACE_ALLOW_DECISIONS=true` enables ✅
- Key: `health.processor.<instance>.auth-trace.<requestId>` ✅
- No additional reads solely for traceability ✅

### Residual carries for 3.6-3.7

- **CLI retrieval (`lattice auth-trace <requestId>`)**: Story 6.1 scope. The trace record is in Health KV and readable via `nats kv get health-kv health.processor.<instance>.auth-trace.<requestId>` today.
- **Plane 2 actual cypher rule body hash**: Phase 1 uses sha256(lensKey+projectedAt) fingerprint. To get the actual body hash, Story 6.x could add a fresh Core KV read of the lens vertex at the revision in `projectedFromRevisions` — within the "no reads solely for traceability" spirit since it would be an explicit operator-triggered enrichment.
- **Allow-decision trace volume warning**: the `traceAllowDecisions=true` path is wired and functional but no Health KV "high volume" warning is emitted (the AC says "a warning is emitted to Health KV"). This is a carry for Story 6.2 (Health KV schema completeness).
- **3.7 (Gate 3 adversarial suite)**: the suite verifies that denied operations are traceable per Story 3.5 — the `health.processor.<instance>.auth-trace.<requestId>` key should be asserted present after each adversarial attack that results in a denial.
