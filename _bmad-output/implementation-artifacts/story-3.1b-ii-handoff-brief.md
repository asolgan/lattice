---
title: Story 3.1b-ii Implementation Handoff Brief
story: 3.1b-ii — Full openCypher Engine Executor + Bootstrap Capability Lens E2E
model_tier: Opus (locked)
token_budget: ~100K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-15
predecessor: Story 3.1b-i (visitor + AST + parse-only, shipped at e9d30c4)
---

# Story 3.1b-ii — Full openCypher Engine Executor + Bootstrap Capability Lens E2E: Handoff Brief

## Your Role

You close Story 3.1. The visitor + AST + parse layer are in place from 3.1b-i (commit e9d30c4) — the bootstrap CapabilityLensDefinition already parses cleanly. Your job is to execute the parsed `*Query` against Core KV (vertex/aspect data) and Adjacency KV (edges), produce a Contract #6-conforming Capability KV projection, measure latency, and converge the production execution path through `RuleEngine.Execute`.

After 3.1b-ii, Story 3.2 (Capability Lens live activation) can build on a real engine.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **Token budget is for tracking only, NOT a halt threshold.** Original estimate ~100K. Record actual outer-telemetry consumption in the tracker at session close. Do NOT stop work based on token count.

- **Halt and escalate** if you find yourself in any of these patterns:
  - Re-attempting the same operation after 3+ failures
  - Making changes you immediately revert
  - Re-reading the same files looking for an answer that isn't there
  - Cycling between two failed approaches without convergence
  - Stuck on a test that fails for a reason you can't reduce after two debugging attempts

  These are stuck-loop signals. Token consumption alone is NOT a halt signal.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables done, deliverables remaining, honest token estimate (for visibility, not enforcement), and any concerns.

- **Model tier:** Opus only. Halt if Sonnet/Haiku.
- **No PRs.** Direct commit to `main` after Winston review.
- **No git commits by you.** Winston + Andrew commit.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 and `_bmad-output/planning-artifacts/epics.md` Story 3.1 are source of truth.
- **DO NOT silently edit planning artifacts** except `_bmad-output/planning-artifacts/lattice-architecture.md` for the openCypher dependency pin (Decision #13 below), and `_bmad-output/planning-artifacts/refractor-gap-analysis.md` ONLY if the bootstrap-query p95 exceeds the NFR-P3 budget (Decision #11).
- **Token tracker:** update Row 3.1b-ii at session close with outer-telemetry actual.

## What's Already in Place (do NOT redo)

- **Story 3.1a (df187dd):** package boundary at `internal/refractor/ruleengine/{simple,full,full/cypher}`; `RuleEngine` interface + `Registry` + `SelectForLens`; Lens schema `ruleEngine` field; health emission tracks resolved engine.
- **Story 3.1b-i (e9d30c4):** Refractor-native AST at `internal/refractor/ruleengine/full/ast.go`; visitor at `internal/refractor/ruleengine/full/visitor.go`; `full.Engine.Parse` real implementation; 14 per-feature parse tests + `TestParse_BootstrapCapabilityLens` all PASS; `full.Engine.Execute` is currently a typed-error stub `"full engine: executor not yet implemented (Story 3.1b-ii)"`.
- Tree is clean (`go build` + `make vet` green) at session start.

Read `internal/refractor/ruleengine/full/ast.go` first — it's your input contract.

## Story Scope (3.1b-ii)

**In scope:**
- Real `full.Engine.Execute` that walks the AST against Core KV (vertex/aspect data) and Adjacency KV (edges), applies WITH grouping/aggregation, OPTIONAL MATCH null-preserving semantics, anti-pattern WHERE, parameter binding, soft-delete filter — and produces a `ruleengine.ProjectionResult` suitable for existing adapters.
- Bootstrap Capability Lens e2e: seed a representative graph, run `CapabilityLensDefinition` end-to-end, assert Contract #6 §6.10 / §6.2 three-section output (`platformPermissions`, `serviceAccess`, `ephemeralGrants`).
- Latency measurement (mean + p95 + p99 if practical).
- Adjacency KV inbound-index extension if needed for inbound traversal (see Decision #6 scope guard).
- C1/C2 convergence (3.1a residuals) — production execution path through `RuleEngine.Execute`.
- `lattice-architecture.md` openCypher dependency pin.

**Out of scope:**
- Capability Lens live activation in production make-up (Story 3.2)
- Processor step 3 auth (Story 3.3)
- Denial-response shaping (Story 3.4)
- Capability Lens adversarial suite (Story 3.7)
- A generalized Cypher database — implement only the Contract #6 / Story 3.1 feature set

## Required Execution Semantics

The executor must handle (per the bootstrap query):

1. **MATCH** — find vertices matching label + property predicates via Core KV.
2. **OPTIONAL MATCH** — same as MATCH but row-preserving with nulls when no match.
3. **Outbound `-[:type]->`** — edge lookup via Adjacency KV.
4. **Inbound `<-[:type]-`** — edge lookup via Adjacency KV in reverse direction (see Decision #6).
5. **Variable-length `*MinHops..MaxHops`** — BFS traversal with cycle detection; default max-depth cap = 10 (sanity guard).
6. **`WHERE`** — equality, AND/OR, anti-pattern `NOT (path_expression)` (evaluate inner pattern as exists-predicate).
7. **`WITH`** — projection + grouping. Non-aggregating columns are grouping keys; aggregating columns (e.g. `collect`) reduce per group.
8. **`collect(DISTINCT)`** — deduped list aggregation.
9. **Map literal `{k1: v1, ...}`** in RETURN/WITH — evaluate values, emit as `map[string]any`.
10. **List concat `+`** — flat concatenation of two list-valued expressions.
11. **Pattern comprehension `[(x)-[:t]->(y) | {k: y.prop}]`** — sub-query that yields a list of evaluated projections (the bootstrap query uses this for `allowedOperations`).
12. **Parameter substitution** — `$actorKey`, `$now`, `$projectedAt` (and arbitrary `$name`) resolved from `EventContext.Parameters` (you add this field — see Decision #8).
13. **Soft-delete filter** — every Core KV read filters `isDeleted: true` per Contract #1.

## Architectural Decisions Already Made (Winston)

1. **Executor structure:** one or two files under `internal/refractor/ruleengine/full/`:
   - `executor.go` — the public Execute entry point + top-level query-walker.
   - `evaluator.go` (or merged into executor) — expression evaluation (Literal, PropertyAccess, BinaryOp, FunctionCall, MapLiteral, ListConcat, anti-pattern predicate, parameter resolution, soft-delete-aware reads).

2. **Refactor surface:** the executor is the ONLY new code in `internal/refractor/ruleengine/full/`. Do NOT add a new "plan" intermediate stage between AST and execution — walk the AST directly. If a planning step would meaningfully simplify execution, propose it; otherwise treat the AST as the executable IR.

3. **Edge lookups go through Adjacency KV; vertex/aspect reads go through Core KV.** Per parent brief Decision #5 (binding) — do not bypass.

4. **Reference simple engine for patterns:** `internal/refractor/ruleengine/simple/evaluator.go` is your reference for how the existing engine reads Core KV / Adjacency KV. Borrow patterns (key construction, batched lookups, soft-delete filter). DO NOT depend on simple-package symbols from full — the engines are independent.

5. **WITH grouping semantics (Cypher-standard):**
   - Non-aggregating projection items become grouping keys.
   - Aggregating items (`collect`, `count`, etc. — for now only `collect` is required) reduce per group.
   - `WITH a, b, collect(c) AS cs WHERE ...` filters AFTER grouping.

6. **Inbound traversal `<-[:type]-`:** Adjacency KV today is likely outbound-keyed. Check the actual shape in `internal/refractor/adjacency/`. Two options:
   - **(a) Extend adjacency builder** to also emit reverse-direction index entries (preferred — one-time projection cost; lookups stay O(1)).
   - **(b) Implement inbound traversal as a scan with a filter** (acceptable only if (a) is complex).

   PREFER (a). **Scope guard:** if (a) requires touching >10 files in `internal/refractor/adjacency/` or its callers, escalate to Winston rather than balloon scope.

7. **Anti-pattern `NOT (path)`:** evaluate the inner path as a sub-query; if it returns ≥1 row, the outer predicate is FALSE; if 0, TRUE. Implement as a generic `existsAsPredicate(pattern, bindings)` helper — the bootstrap query uses it for service exclusions, but the helper is general.

8. **Parameter substitution surface:** the executor receives parameters via `EventContext.Parameters map[string]any`. **You ADD this field to `EventContext`** in `internal/refractor/ruleengine/ruleengine.go` (additive, omitempty-equivalent — empty map by default; doesn't break 3.1a callers). Caller responsibility (Story 3.2) to populate `$actorKey`, `$now`, `$projectedAt` from live event/clock — in 3.1b-ii your e2e test harness binds them directly.

   If a parameter is referenced in the AST but not bound at execution time, return a typed error `MissingParameterError` with the parameter name.

9. **Bootstrap e2e test fixture:**
   - Build a small representative graph in `internal/refractor/ruleengine/full/testdata/` (test-only, not bootstrap-modifying). Include the entity types the bootstrap query references (identity, role, permission, service, location, etc.) with realistic but minimal counts.
   - Use the LITERAL `CapabilityLensDefinition.RuleBody` from `internal/bootstrap/lenses.go` — do NOT hand-copy a simplified version (parent brief Decision #8 binding).
   - Assert the projection output rows conform to Contract #6 §6.10 / §6.2: three sections (`platformPermissions`, `serviceAccess`, `ephemeralGrants`).
   - If the bootstrap query expects entities that don't exist in the existing primordial seed (e.g., a specific test identity), ADD them ONLY to the test-only fixture — do NOT modify `internal/bootstrap/lenses.go` or the primordial seed.

10. **C1/C2 (3.1a residual carries) — converge:**
    - **C1:** production execution path should flow through `RuleEngine.Execute`, not `simple.Evaluate` directly. Update the pipeline (`internal/refractor/pipeline/pipeline.go`) to call the resolved engine's Execute method.
    - **C2:** `lens.Parse` re-runs `simple.Compile` after `SelectForLens`. If structurally clean to drop the duplicate parse and pass forward the `CompiledRule` from selection, do it.
    - **Scope guard:** if convergence requires touching >5 production files outside `ruleengine/`, escalate.
    - **If convergence is genuinely fragile** (e.g., the pipeline depends on simple's concrete `*QueryPlan` shape in a way that can't be expressed through the `RuleEngine.Execute` interface), document as a permanent gap with rationale and leave a `// TODO(3.2 or later)` comment with the reason. Do NOT escalate over a TODO.

11. **Latency budget — record, don't halt:** the bootstrap e2e records mean + p95 (+ p99 if practical). If p95 exceeds the NFR-P3 budget (< 500ms p99 end-to-end CDC-to-projection lag), update `_bmad-output/planning-artifacts/refractor-gap-analysis.md` Risk Register or §2.4 with the specific bottleneck and proposed mitigation. Don't HALT — record and proceed. If p95 is comfortably under, no doc edit needed.

12. **CI gate:** `.github/workflows/ci.yml` is active. After your changes, CI must go green.

13. **`lattice-architecture.md` dependency pin:** find the placeholder mentioning "Open-source Go openCypher parser" or similar and replace with: `github.com/antlr4-go/antlr/v4 v4.13.1` runtime + grammar vendored from `github.com/jtejido/go-opencypher` (commit hash from `internal/refractor/ruleengine/full/cypher/README.md` if recorded; otherwise note "as of 2026-05-15"). Smallest possible edit.

14. **ANTLR/cypher containment** (parent brief Decision #3): the executor MAY use the AST from `full.ast.go` (which already lives in `full/`); it MUST NOT import `cypher` or `antlr` packages. The Parse-side already isolates those. Verify with `grep -rn "antlr\.\|cypher\." internal/refractor/` — only matches inside `full/cypher/` and `full/visitor.go` / `full.go`.

15. **No new CONTRACT-AMENDMENT-REQUEST expected.** If one emerges, append and escalate.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.1-handoff-brief.md` | Parent brief — context for the 10 architectural decisions |
| `_bmad-output/implementation-artifacts/story-3.1b-i-handoff-brief.md` | Predecessor brief — what 3.1b-i shipped |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 §6.10 + §6.2 | Bootstrap Capability Lens semantics + Capability KV three-section output shape |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.6 (envelope) — specifically the `isDeleted` field | Soft-delete filter source of truth |
| `internal/bootstrap/lenses.go` (`CapabilityLensDefinition`) | The exact query — your acceptance oracle |
| `internal/refractor/ruleengine/ruleengine.go` | The interface + `EventContext` (you add `Parameters` field here) |
| `internal/refractor/ruleengine/full/ast.go` | Your input contract (3.1b-i shipped) |
| `internal/refractor/ruleengine/full/full.go` | The Execute typed-error stub you replace |
| `internal/refractor/ruleengine/simple/evaluator.go` | Reference for Core KV / Adjacency KV access patterns |
| `internal/refractor/adjacency/` (entire package — small) | The edge-lookup API; check whether inbound indexing exists (Decision #6) |
| `internal/refractor/pipeline/pipeline.go` | Where C1 convergence lands |
| `internal/refractor/lens/schema.go` | Where C2 convergence lands (the duplicate-Compile call from 3.1a) |
| `_bmad-output/planning-artifacts/lattice-architecture.md` | Find the openCypher placeholder for Decision #13 |
| `_bmad-output/planning-artifacts/refractor-gap-analysis.md` §2.4 + Risk Register | Only if you need to record a p95 overrun |

**DO NOT read** the cypher_parser.go / cypher_listener.go (3.1b-i handled them), Materializer source, or the full epics.md. The AST is your input; you don't need to revisit the grammar.

## Suggested Sequence

**Phase A — Executor skeleton + simple cases (target ~25K tokens):**
1. Add `Parameters map[string]any` to `ruleengine.EventContext`.
2. Create `internal/refractor/ruleengine/full/executor.go` with `func (e *Engine) Execute(ctx, cr, ec) (ProjectionResult, error)` skeleton: unwrap CompiledRule, walk query clauses, emit rows.
3. Implement: simple MATCH (single node pattern), basic property predicates, simple RETURN, parameter resolution, soft-delete filter on Core KV reads.
4. Write minimal tests for each.

**Phase B — Traversal + WHERE + WITH (target ~25K tokens):**
5. Implement edge traversal via Adjacency KV (outbound first, then inbound — Decision #6).
6. Implement variable-length traversal (BFS with cycle detection, max-depth cap).
7. Implement OPTIONAL MATCH (null-preserving rows).
8. Implement WHERE evaluator (AND/OR, anti-pattern via existsAsPredicate helper).
9. Implement WITH (projection + grouping for collect(DISTINCT) + post-grouping WHERE).
10. Test each.

**Phase C — Map literal, list concat, pattern comprehension (target ~15K tokens):**
11. Implement map literal expression evaluator.
12. Implement list concat operator.
13. Implement pattern comprehension (sub-query yielding evaluated projections).
14. Test each.

**Phase D — Bootstrap Capability Lens e2e (target ~20K tokens):**
15. Build `internal/refractor/ruleengine/full/testdata/` fixture: representative graph with identities, roles, permissions, services, locations.
16. Build `internal/refractor/ruleengine/full/bootstrap_e2e_test.go`: use the literal `CapabilityLensDefinition.RuleBody`; bind `$actorKey`/`$now`/`$projectedAt`; run; assert three-section output per Contract #6.
17. Record mean + p95 (+ p99 if practical).

**Phase E — Convergence + arch doc + gates (target ~15K tokens):**
18. C1 convergence: pipeline calls RuleEngine.Execute instead of simple.Evaluate directly.
19. C2 convergence: lens.Parse drops the duplicate simple.Compile call if structurally clean.
20. Update `lattice-architecture.md` openCypher dependency pin (Decision #13).
21. Run all gates per "Required Verification" section.
22. Update token tracker Row 3.1b-ii with outer telemetry.
23. Closing summary.

## Required Verification

```bash
go build ./...
make vet
go test ./internal/refractor/ruleengine/... -count=1
go test ./internal/refractor/... -p 1 -count=1
make verify-bootstrap
make test-bypass
go test ./... -p 1 -count=1
```

## Deliverables Checklist

1. ✅ `ruleengine.EventContext.Parameters map[string]any` field added
2. ✅ `internal/refractor/ruleengine/full/executor.go` — real Execute implementation
3. ✅ `full.Engine.Execute` replaces the 3.1b-i typed-error stub
4. ✅ Soft-delete filter applied to every Core KV read in the executor
5. ✅ Parameter substitution supports `$actorKey`, `$now`, `$projectedAt`, and arbitrary `$name` parameters with `MissingParameterError` for unbound references
6. ✅ Outbound + inbound edge traversal via Adjacency KV (inbound per Decision #6)
7. ✅ Variable-length traversal with cycle detection + max-depth cap (default 10)
8. ✅ OPTIONAL MATCH null-preserving semantics
9. ✅ Anti-pattern WHERE via `existsAsPredicate` helper
10. ✅ WITH grouping with `collect(DISTINCT)` aggregation
11. ✅ Map literal, list concat, pattern comprehension expression evaluators
12. ✅ Bootstrap CapabilityLensDefinition e2e test (literal query body); output rows conform to Contract #6 three-section shape
13. ✅ Latency recorded: mean + p95 (+ p99) in closing summary
14. ✅ If p95 exceeds NFR-P3 budget, gap analysis updated (Decision #11)
15. ✅ C1 convergence: production execution flows through RuleEngine.Execute (or TODO with rationale per Decision #10)
16. ✅ C2 convergence: duplicate simple.Compile dropped (or TODO with rationale)
17. ✅ `lattice-architecture.md` openCypher dependency pin updated (Decision #13)
18. ✅ ANTLR/cypher containment unchanged: still only inside `full/cypher/` and `full/visitor.go`/`full.go`
19. ✅ `go build ./...`, `make vet`, all required tests, `make verify-bootstrap`, `make test-bypass` green
20. ✅ Token tracker Row 3.1b-ii updated with outer-telemetry actual
21. ✅ Closing summary including: feature execution matrix (one row per Required Execution Semantics item with PASS/FAIL), bootstrap query latency mean/p95/p99, C1/C2 convergence status, residual risks for Story 3.2

## What 3.1b-ii Is NOT

- **Not** Capability Lens live activation (Story 3.2)
- **Not** Processor step 3 auth (Story 3.3)
- **Not** denial-response shaping (Story 3.4)
- **Not** Capability Lens adversarial suite (Story 3.7)
- **Not** a generalized Cypher database

## Escalation

Halt and escalate via Andrew/Winston if:
- A required execution semantic can't be implemented cleanly without violating the Adjacency KV / Core KV boundary
- Adjacency KV inbound-index extension requires touching >10 files (Decision #6 scope guard)
- C1/C2 convergence requires touching >5 production files outside `ruleengine/` (Decision #10 scope guard)
- The bootstrap query's expected graph requires modifying `internal/bootstrap/lenses.go` or the primordial seed
- A CONTRACT-AMENDMENT-REQUEST emerges
- You hit a stuck-loop pattern per the operating rules (re-attempts, immediate reverts, cycling between approaches, unresolved test failure after 2 debug attempts)

## Closing

1. Verify all 21 deliverables
2. Run all required gates
3. Update token tracker Row 3.1b-ii with outer-telemetry actual
4. Closing summary as Deliverable #21 (execution matrix + latency + convergence + residual risks for Story 3.2)

Do NOT commit. Winston + Andrew review and commit.
