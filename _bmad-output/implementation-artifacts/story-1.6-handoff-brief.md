---
title: Story 1.6 Implementation Handoff Brief
story: 1.6 — Processor — Starlark Sandbox & JIT Hydration (Steps 4-5)
model_tier: Opus (locked)
token_budget: ~130K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-14
---

# Story 1.6 — Processor Steps 4-5: Implementation Handoff Brief

## Your Role

You implement commit-path steps 4 (JIT Hydration) and 5 (Starlark Execution) for the Lattice Processor. Steps 1-3 exist (Story 1.5); steps 6-10 remain stubs (handled in 1.7 and 1.8). You replace Story 1.5's stubbed `Hydrator` and `Executor` interfaces with real implementations.

## 🔴 MANDATORY BUDGET CHECKPOINT RULE (READ FIRST)

**Story 1.5's sub-agent self-reported ~70K actual but consumed ~161K — a 56%-of-budget undercount that led to a 40% overrun.** To prevent recurrence:

- **At any point you estimate your cumulative token usage has exceeded ~65K (50% of your 130K budget), STOP and send a status message** containing: estimated tokens used, deliverables completed so far, deliverables remaining, recommendation (continue / scope-down / halt for Winston review).
- **Halt unconditionally if estimate exceeds 110K (~85% of budget) without explicit Winston greenlight.**
- **Self-estimate every ~10 tool calls or after any file read >20KB.** Be honest — undercounting helps no one.

## Operating Rules

- **Model tier:** Opus only.
- **No PRs.** After Winston review, commit direct to `main`.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is the source of truth.
- **No git commits by you.** Winston + Andrew commit.
- **DO NOT** silently edit planning artifacts — use `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` for any concerns. Story 1.4's agent did this correctly; Story 1.5's agent did this correctly. Maintain the pattern.
- **Token tracker update:** Row 1.6 at session close.

## Story Scope (from `epics.md` — authoritative)

> As a platform engineer, I want steps 4 and 5 of the Processor commit path — JIT vertex hydration from Core KV and Starlark script execution in a validated sandbox — implemented and integration-tested, so that business rules execute deterministically against live graph state.

**Recommended model tier:** Opus
**Estimated token budget:** ~130K

### Acceptance Criteria (from `epics.md`)

**Given** an authorized operation envelope has passed steps 1–3
**When** step 4 (JIT hydration) executes
**Then** the Processor reads the vertices referenced in the operation's `contextHint` from Core KV, materializes them into the hydration context, and makes them available to the Starlark script; if a referenced vertex key does not exist in Core KV, the step returns a typed `HydrationError` and the commit path terminates with a rejection response to the caller.

**Given** the hydration context is populated
**When** step 5 (Starlark execution) runs the DDL-associated script for the operation type
**Then** the script executes within the sandbox validated in Story 1.2 (no I/O, no network, no secrets, no non-deterministic calls); the script receives hydrated vertex data and the operation payload; the script returns a proposed `MutationBatch`; if the script raises a Starlark error, execution terminates with a `ScriptError` and no MutationBatch is produced.

**Given** a Starlark script attempts any forbidden operation (external HTTP, filesystem read, `os.Getenv`, non-deterministic call)
**When** the script executes
**Then** the sandbox rejects the forbidden operation at runtime; the script error is caught; the commit path terminates with a `SandboxViolation` error; no partial mutation reaches step 6.

**And** the `ScriptContext` API matches the interface prototyped in Story 1.2's spike.
**And** script lookup uses the `class` field from the operation's target vertex envelope to find the DDL meta-vertex and its associated script body.
**And** integration tests cover: clean execution (MutationBatch returned), hydration miss (HydrationError), script error (ScriptError), and all four sandbox violation vectors.

## Required Context — Read These Only

| File | Section | Why |
|---|---|---|
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #1 §1.5 (envelope) + §1.6 (permissive default) + §1.7 (class field) | Class-based DDL lookup |
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #2 — esp. `contextHint` field | What JIT hydration reads |
| `_bmad-output/planning-artifacts/data-contracts.md` | Contract #3 — MutationBatch shape (§3.1, §3.2) | The script's return value contract |
| `_bmad-output/planning-artifacts/epics.md` | Story 1.6 (canonical AC) | Source of truth |
| `internal/spike/starlark/runner.go` + `types.go` + `api_ergonomics.go` | Whole files | The prototype API; port these patterns into `internal/processor/` |
| `internal/spike/starlark/sandbox_correctness.go` | Whole file | The four sandbox-violation tests; bring these into Story 1.6's integration tests with adaptation |
| `internal/spike/starlark/README.md` | Sections 1-2 | Sandbox config + ScriptContext design rationale |
| `internal/processor/steps_4_10_stub.go` | Hydrator and Executor interfaces | The interface contract you must satisfy when replacing the stubs |
| `internal/processor/commit_path.go` | Whole file | How step 4 + step 5 get invoked |
| `internal/substrate/kv.go` + `keys.go` | Public API | Use these for Core KV reads |

## Architectural Decisions Already Made (Winston)

### From Story 1.2 spike (locked):
1. **Library:** `go.starlark.net` (already in `go.mod` via the spike).
2. **Sandbox model:** empty-by-construction. Only globals exposed are `state`, `op`, `ddl`. No `load` (`thread.Load` left nil). No `os`, `time`, `http`, `open`, `random`, etc.
3. **`ScriptContext` API:** keep the shape from `internal/spike/starlark/types.go` — `Operation`, `Hydrated`, `DDLLookup` as the three inputs; `ScriptResult` with `Mutations + Events` as output, conforming to Contract #3.

### From Story 1.2 Winston-decided answers (deferred to this story):
4. **`nanoid.new()` and `nanoid.short()` exposure:** YES — scripts get both as Starlark builtins. Pure deterministic computation: seeded per-invocation by `op.requestId` per Contract #3 §3.8 (one operation always produces the same NanoIDs across replays, no side effects). Implement via `starlark.NewBuiltin` in your runner.
5. **Execution timeout:** wall-clock, NOT step count. NFR-P4 specifies "100ms p99" — that's wall-clock. Use `context.WithTimeout` + a goroutine running the script with `thread.Cancel()` invoked on timeout. Add `thread.SetMaxSteps()` as a secondary safeguard (catch infinite loops) but it's not the primary mechanism. Default wall-clock budget for Phase 1: 250ms (gives headroom over NFR-P4's 100ms p99 ceiling for typical operations; configurable via env).
6. **Script discovery:** Scripts live as aspects on DDL meta-vertices in Core KV — NOT in `scripts/` directory. Class-based lookup: operation's target vertex has `class` field; that maps to a `vtx.meta.<class>` meta-vertex; the meta-vertex has an aspect (e.g., `vtx.meta.<class>.script`) holding the Starlark source. JIT hydration of the DDL meta-vertex + script aspect happens alongside business-state hydration (same step 4 round-trip).

### New decisions for 1.6:
7. **DDL hydration is automatic in step 4.** Even if `contextHint` doesn't list the DDL meta-vertex, step 4 derives the operation's target class (from the operation envelope or its target vertex's existing class in Core KV) and hydrates `vtx.meta.<class>` + its `script` aspect into `ScriptContext.DDLLookup`.
8. **Missing script handling:** If the DDL meta-vertex has no `script` aspect (i.e., no business rule defined for this operation type), the operation is rejected with `NoScriptForClass` — NOT silently allowed. Permissive-by-default applies to *aspects on vertices*, not to *script presence on a DDL meta-vertex referenced by an operation type*. Story 1.7 may revisit this when DDL validation lands.
9. **MutationBatch construction:** Story 1.6 produces the proposed `MutationBatch` as the `ScriptResult.Mutations` slice. **Do NOT validate or apply mutations in 1.6** — that's Story 1.7's job (step 6 DDL validation + step 8 atomic batch). 1.6's commit path simply passes the `MutationBatch` to the still-stubbed step 6, which logs it.
10. **`HydrationError` typed shape:** `{Code: "HydrationMiss", MissingKey: string, OperationRequestID: string}`. Surface this to the caller via the rejection reply path established in Story 1.5.
11. **`ScriptError` typed shape:** `{Code: "ScriptError" | "SandboxViolation" | "ScriptTimeout", Message: string, Line: int, Column: int, OperationRequestID: string}`. Map go.starlark.net's error types onto this.
12. **NanoID builtin determinism:** the per-invocation random source is seeded from `op.requestId` using a deterministic hash. Two invocations of the same operation (e.g., during a retry) produce the same NanoID sequence. Use `crypto/sha256(op.requestId)[:8]` as a `math/rand/v2.PCG` seed source; generate the NanoID by indexing into `substrate.Alphabet`.
13. **The `contextHint` may be empty.** A valid operation may have no `contextHint` (e.g., a pure `CreateXxx` operation where no prior state is consulted). Step 4 must handle this — only DDL hydration is forced; business-state hydration only runs if `contextHint` is non-empty.

## Suggested Layout

```
internal/processor/
├── (existing files from 1.5 — do not break)
├── step4_hydrate.go       NEW: Hydrator implementation
├── step5_execute.go       NEW: Executor implementation
├── starlark_runner.go     NEW: Port internal/spike/starlark/runner.go
├── starlark_builtins.go   NEW: nanoid.new(), nanoid.short() Starlark builtins
├── script_context.go      NEW: ScriptContext / ScriptResult types (port from spike)
└── (matching _test.go files)
```

You replace `internal/processor/steps_4_10_stub.go`'s `Hydrator` and `Executor` stubs with real implementations — keep the same interface signatures so the commit path wiring continues to work.

## Deliverables Checklist

1. ✅ `internal/processor/step4_hydrate.go` + tests — Hydrator real implementation
2. ✅ `internal/processor/step5_execute.go` + tests — Executor real implementation
3. ✅ `internal/processor/starlark_runner.go` + `starlark_builtins.go` + `script_context.go` — Starlark integration ported from Story 1.2 spike
4. ✅ `internal/processor/steps_4_10_stub.go` — `Hydrator` and `Executor` stubs REMOVED (or marked deprecated); steps 6-10 stubs remain
5. ✅ Integration tests covering: clean execution, hydration miss → HydrationError, script error → ScriptError, all four sandbox violations
6. ✅ `make verify-bootstrap` still 30/30 (regression)
7. ✅ `go build ./...` exits 0; `go vet ./...` exits 0; `go test ./internal/processor/...` exits 0
8. ✅ Updated Row 1.6 in token tracker
9. ✅ Closing summary

## What Story 1.6 Is NOT

- **Not** DDL validation — that's Story 1.7 (step 6). Whatever the script returns goes through (1.6 does NOT validate `permittedCommands`).
- **Not** the atomic batch commit — that's Story 1.7 (step 8). Mutations are logged, not applied.
- **Not** event publication — that's Story 1.8 (step 9).
- **Not** the bootstrap subject-pattern fix from Story 1.5's open amendment — leave that alone; it's deferred to whichever story naturally touches that code.

## Escalation

- NATS/Starlark/contract surface gap → stop, write finding, escalate
- Contract contradiction → `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` (append, don't overwrite Story 1.5's amendment which is still OPEN)
- **Budget checkpoint at 65K (mandatory) and 110K (halt)** — see top of brief

## Closing

1. Verify all 9 deliverables
2. `go build ./...`, `go vet ./...`, `go test ./internal/processor/...` exit 0
3. `make down && make up && make verify-bootstrap && make down` regression check
4. End-to-end exercise: with `make up` running, submit a real-shape operation through the processor that hits all paths (clean exec, hydration miss, script error, one sandbox violation) — capture log output in the closing summary
5. Update token tracker Row 1.6
6. Return closing summary with: deliverables present, e2e behaviors observed, any contract amendments raised, **honest token estimate (per the budget rule — undercounting helps no one)**, build/test status, open questions

Do NOT commit. Winston + Andrew review and commit.
