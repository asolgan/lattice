---
title: Story 3.1b Implementation Handoff Brief
story: 3.1b — Full openCypher Engine Visitor + Executor + Bootstrap Capability Lens E2E
model_tier: Opus (locked)
token_budget: ~120K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-15
predecessor: Story 3.1a (engine boundary + simple-engine isolation + Lens engine selection — shipped at df187dd)
---

# Story 3.1b — Full openCypher Engine: Visitor + Executor + Bootstrap Capability Lens E2E: Handoff Brief

## Your Role

You deliver the **semantic core** of the `full` openCypher engine: the parse-tree visitor that translates ANTLR output into Refractor-native query structures, the executor that runs those structures against Core KV (vertex/aspect data) and Adjacency KV (edges), and the e2e proof that the bootstrap-seeded Capability Lens query parses, executes, and produces a Contract #6-conforming projection. After 3.1b, Story 3.2 (Capability Lens activation) can build on top of a real engine.

Story 3.1a already shipped the package boundary, the engine registry, the Lens `ruleEngine` field, the selection semantics, and a stub `full.Engine` that always returns `InvalidRule`. Your job is to REPLACE the stub with a real implementation.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

**Pattern across Phase 1: sub-agents have self-reported tokens 30-50% under outer telemetry.** This story is parser + execution architecture against an unfamiliar grammar — highest creep risk of any story so far.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables done, deliverables remaining, honest token estimate (lower bound, rounded UP).
- **Halt unconditionally if you estimate > 125K used** (about 4% over budget).
- **Model tier:** Opus only. Halt if Sonnet/Haiku.
- **No PRs.** Direct commit to `main` after Winston review.
- **No git commits by you.** Winston + Andrew commit.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 and `_bmad-output/planning-artifacts/epics.md` Story 3.1 are source of truth.
- **DO NOT silently edit planning artifacts** except `lattice-architecture.md` for pinning the openCypher dependency (per parent 3.1 brief), and `_bmad-output/planning-artifacts/refractor-gap-analysis.md` ONLY if bootstrap-query p95 exceeds NFR-P3 budget (per parent brief — see Decision #11 below).
- **Token tracker:** add Row 3.1b at session close — HONEST estimate, round UP.

## What 3.1a Already Delivered (do NOT redo)

Read `internal/refractor/ruleengine/ruleengine.go` first to understand the contract you're satisfying. Briefly:
- `RuleEngine` interface: `Name() / Parse(string) (CompiledRule, error) / Execute(ctx, CompiledRule, EventContext) (ProjectionResult, error)`.
- `simple.Engine` is the existing parser/compiler/evaluator (verbatim Materializer carryover, isolated under `ruleengine/simple/`).
- `full.Engine` is the STUB at `ruleengine/full/full.go`. You replace its `Parse` and `Execute` methods.
- Selection semantics in `ruleengine.SelectForLens` are already correct — don't touch the selection logic. You only need to make the `full` engine actually work; the selection layer will route activations to it correctly once `full.Engine.Parse` stops returning `InvalidRule`.

3.1a left three residual carries (named in its commit message and the parent brief's closing notes):
- C1: production execution path still calls `simple.Evaluate` directly with `*simple.QueryPlan`; doesn't flow through `RuleEngine.Execute`. **Your call in 3.1b:** converge or accept permanently. Recommend converging — see Decision #10 below.
- C2: `lens.Parse` re-runs `simple.Compile` after `SelectForLens` (duplicate parse). **Same call as C1.** Recommend converging.
- C3: `full.Engine.Execute` panics defensively. Remove the panic once Execute is real.

## Story Scope (Authoritative)

From `_bmad-output/planning-artifacts/epics.md` Story 3.1 ACs (the parts NOT satisfied by 3.1a):

1. **`full` parser visitor** at `internal/refractor/ruleengine/full/visitor.go` walks the ANTLR parse tree to build a Refractor-native query AST. The visitor consumes the vendored `cypher` package; no ANTLR types leak out of `internal/refractor/ruleengine/full/`.
2. **`full` executor** runs the compiled rule against Core KV (vertex/aspect data) and Adjacency KV (edges) and produces a `ProjectionResult` suitable for existing adapters.
3. **Bootstrap Capability Lens query** (`internal/bootstrap/lenses.go::CapabilityLensDefinition`) parses cleanly, executes against a representative seeded graph, and produces a Capability KV projection conforming to Contract #6's three-section model.
4. **Mean and p95 execution latency** recorded for the bootstrap query.
5. **Selection-with-real-full tests:** the 5 selection tests from 3.1a still pass; ADDITIONALLY, a test must demonstrate `explicit-full` succeeding when the rule is valid full-cypher, AND a test must demonstrate `absent-fallback` choosing `full` when the rule fails the simple parser but succeeds in full.
6. **Lattice-architecture.md** updated to pin the openCypher dependency (replace the "Open-source Go openCypher parser" placeholder with the actual version: `github.com/antlr4-go/antlr/v4 v4.13.1` + a note that the grammar files are vendored from `github.com/jtejido/go-opencypher`).

## Required Cypher Feature Support (parent brief)

The `full` engine is complete only when these execute correctly against the bootstrap query:

- Map literal expressions in `RETURN`, e.g. `{operationType: perm.operationType, scope: perm.scope}`
- `collect()` aggregation with `DISTINCT`
- `WITH` clauses for intermediate aggregation
- `OPTIONAL MATCH`
- Variable-length path patterns using `*`, including `*0..` and the Phase 1 two-hop `reportsTo` manager-delegation limit
- List concatenation, e.g. `collect(...) + collect(...)`
- Inbound traversal syntax, e.g. `<-[:reportsTo]-`
- `WHERE` filters needed by the bootstrap query, including negated existence/anti-pattern behavior for service exclusions
- Parameter references used by bootstrap queries, including `$actorKey`, `$now`, and `$projectedAt`

If one of these cannot be supported cleanly in this story, **HALT and escalate**. Do NOT land a "full" engine that parses the query but silently drops semantic clauses.

## Architectural Decisions Already Made (Winston)

1. **Visitor pattern:** implement a Refractor-owned visitor at `internal/refractor/ruleengine/full/visitor.go`. It implements `cypher.CypherListener` (the generated listener interface) and walks the parse tree to build a Refractor-native query AST in your own types (e.g., `Query`, `MatchClause`, `PathPattern`, `Predicate`, `Projection`, `WithClause`, `OptionalMatch`, `VarLengthPath`, etc.). DO NOT expose cypher/antlr types from any exported symbol.

2. **Refractor-native AST lives in `internal/refractor/ruleengine/full/`** (e.g., `ast.go`). Keep it separate from `simple/`'s AST — different grammar, different shape. Even if naming overlaps (`MatchClause`), the types are independent. Do NOT try to share AST types between engines.

3. **`full.CompiledRule`** wraps the Refractor-native AST + any pre-computed plan structures. It satisfies the `ruleengine.CompiledRule` interface. Internal concrete shape is your call — but it MUST round-trip through `ruleengine.RuleEngine.Execute`.

4. **Executor at `internal/refractor/ruleengine/full/executor.go`**:
   - Edge traversals (including variable-length, inbound, anti-pattern existence) consult Refractor's Adjacency KV.
   - Vertex / aspect data reads go to Core KV (never Adjacency KV).
   - Soft-delete: readers filter `isDeleted == true` documents independently per Contract #1. The executor MUST apply this filter to every Core KV read.
   - Parameter substitution: `$actorKey`, `$now`, `$projectedAt`, and any other `$name` parameters resolve from `EventContext.Parameters` (a `map[string]any`). If a parameter is referenced in the query but not bound at execution time, return a typed error (`MissingParameterError`).
   - Aggregation: `collect(DISTINCT)` builds a deduped list; `WITH` groups by the non-aggregating projection columns. Standard semantics — borrow conceptually from Cypher's grouping rules.
   - List concatenation: `+` between two list-valued expressions produces a flat list (no nested-list flattening beyond what Cypher specifies).

5. **Variable-length paths (`*` quantifiers):**
   - Phase 1 bootstrap uses `*0..` (zero-or-more reflexive) and an implicit two-hop limit for the `reportsTo` chain.
   - Implement BFS up to a configurable max-depth; default max-depth = 10 (sanity guard; the bootstrap query's manager-chain limit is 2, but the engine doesn't hard-code that — the LIMIT comes from the query or from the configurable max).
   - Cycle detection: deduplicate visited vertex keys per path-traversal.
   - DO NOT load all reachable vertices into memory — stream via Adjacency KV's existing batched lookups.

6. **Inbound traversal `<-[:type]-`:**
   - Adjacency KV today is probably outbound-keyed (Materializer convention: `adj.<sourceVtx>.<edgeName>.<targetVtx>` or similar). Check the actual key shape in `internal/refractor/adjacency/`.
   - If the existing adjacency builder doesn't index inbound edges, you have two options: (a) extend the adjacency builder to also emit reverse-direction entries (preferred — Adjacency KV is internal to Refractor; one-time projection cost), OR (b) implement inbound traversal as a scan with a filter (acceptable only if the index extension is complex). PREFER (a). If (a) requires touching >10 files, halt and escalate with the scope.

7. **Anti-pattern `NOT (path)` in WHERE:**
   - Evaluate the inner path pattern as a sub-query (find any match); if it returns ≥1 row, the outer predicate is FALSE. If zero, TRUE.
   - Implement as a generic "exists-as-predicate" helper; the bootstrap query uses it for service exclusions but the helper is general.

8. **Parameters surface:** the executor receives parameters via `EventContext.Parameters map[string]any`. Add a `Parameters` field to `ruleengine.EventContext` if it doesn't already exist. Caller responsibility (Story 3.2) to populate `$actorKey`, `$now`, `$projectedAt` from the live event/clock — in 3.1b, the test harness binds them directly.

9. **Test seeded graph:**
   - Build a small-but-representative graph in test setup using the existing bootstrap identities + roles + permissions + services + locations. Reuse `internal/bootstrap/lenses.go::CapabilityLensDefinition` literally as the query under test — do not hand-copy a simplified version (per parent brief Decision #8).
   - Project once, assert the output rows include the three sections of Contract #6 (`platformPermissions`, `serviceAccess`, `ephemeralGrants`) shaped per §6.10 / §6.2.
   - If the bootstrap query references identities/roles/services/locations that don't exist in the existing bootstrap seed, ADD them to a test-only fixture under `internal/refractor/ruleengine/full/testdata/`. Do NOT modify `internal/bootstrap/lenses.go` or the primordial seed itself.

10. **Carry C1/C2 resolution (3.1a residual):**
    - **Decision:** converge in 3.1b. Production execution path SHOULD flow through `RuleEngine.Execute`. The pipeline already has the resolved engine + the compiled rule from selection; pass the compiled rule forward instead of re-parsing.
    - **Practical scope:** if converging requires touching >5 production files outside `ruleengine/`, halt and escalate. Otherwise, do it.
    - **lens.Parse re-running simple.Compile (C2):** if the SelectForLens path now hands back a usable `simple.CompiledRule`, drop the duplicate `simple.Compile` call. If the structural mismatch makes this fragile, leave a TODO citing 3.1a's note and move on.

11. **Latency budget:** bootstrap query test records mean + p95 (and p99 if you can without bloating the test). If p95 exceeds the NFR-P3 budget (< 500ms p99 end-to-end CDC-to-projection lag), update `_bmad-output/planning-artifacts/refractor-gap-analysis.md` Risk Register or §2.4 with the specific bottleneck and proposed mitigation. Don't HALT — record and proceed. If p95 is comfortably under (< 200ms), no doc edit needed.

12. **CI gate:** `.github/workflows/ci.yml` is active. After your changes, CI must go green.

13. **`lattice-architecture.md` pinning edit:** find the placeholder mentioning "Open-source Go openCypher parser" or similar and replace with: `github.com/antlr4-go/antlr/v4 v4.13.1` runtime + grammar vendored from `github.com/jtejido/go-opencypher` (commit hash from `internal/refractor/ruleengine/full/cypher/README.md` if recorded; otherwise note "as of 2026-05-15"). Smallest possible edit.

14. **No CONTRACT-AMENDMENT-REQUEST expected.** If one emerges, append to `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` and escalate.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.1-handoff-brief.md` | Parent brief — the source of the 10 architectural decisions. Read for context. |
| `_bmad-output/implementation-artifacts/story-3.1a-handoff-brief.md` | Predecessor brief — what 3.1a delivered. |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 §6.10 + §6.2 | Bootstrap Capability Lens semantics + Capability KV output shape |
| `internal/bootstrap/lenses.go` (`CapabilityLensDefinition` + role-index Lens) | The exact queries — your acceptance oracle |
| `internal/refractor/ruleengine/ruleengine.go` | The interface you implement |
| `internal/refractor/ruleengine/full/full.go` | The stub you replace (Parse + Execute) |
| `internal/refractor/ruleengine/full/cypher/cypher_listener.go` + `cypher_parser.go` (skim — these are large; use grep for specific rule names like `OC_Match`, `OC_With`, `OC_Return`, `OC_OptionalMatch`, `OC_PatternElement`, `OC_RelationshipPattern`, `OC_RangeLiteral`, `OC_Properties`, `OC_PropertyKeyName`, `OC_Where`, `OC_FunctionInvocation`, `OC_Parameter`) | Available parse-tree nodes for the visitor |
| `internal/refractor/adjacency/` (entire package — small) | The edge-lookup API you must use; check if inbound indexing exists |
| `internal/refractor/ruleengine/simple/evaluator.go` | Reference for how simple engine reads Core KV / Adjacency KV — patterns to follow |
| `internal/refractor/pipeline/pipeline.go` | Where the production execution flows; you'll touch this for C1 convergence |
| `_bmad-output/planning-artifacts/refractor-gap-analysis.md` §2.4 + Risk Register | Only if you need to record a p95 overrun |

**DO NOT read** the full epics.md, full Contract specs other than #6, or anything from Materializer. The brief is self-contained.

## Suggested Sequence

**Phase A — Visitor architecture spike (≤ 20K tokens):**
1. Read `internal/refractor/ruleengine/full/cypher/cypher_listener.go` to identify the listener methods you'll override.
2. Read `internal/bootstrap/lenses.go::CapabilityLensDefinition` query body — extract the exact list of grammar features used.
3. Send a checkpoint with: (a) parse-tree nodes you'll visit, (b) Refractor-native AST shape sketch, (c) confirmation that all required features (parent brief list) are reachable via the listener.

**Phase B — Visitor + AST (≤ 35K tokens):**
4. Build `internal/refractor/ruleengine/full/ast.go` (Refractor-native types).
5. Build `internal/refractor/ruleengine/full/visitor.go` translating ANTLR parse trees → AST.
6. Replace `full.Engine.Parse` stub: parse via `cypher` package, walk via visitor, return wrapped CompiledRule.
7. Unit tests per required feature (one test per Phase: map literals, collect(DISTINCT), WITH, OPTIONAL MATCH, var-length `*`, list concat, inbound traversal, WHERE NOT, parameters).

**Phase C — Executor (≤ 40K tokens):**
8. Build `internal/refractor/ruleengine/full/executor.go`.
9. Replace `full.Engine.Execute` panic with real execution against Core KV + Adjacency KV.
10. Implement aggregation, WITH, OPTIONAL MATCH null-preserving semantics, variable-length BFS, inbound traversal, anti-pattern WHERE, map literal eval, list concatenation, parameter substitution.
11. If inbound traversal requires Adjacency KV index extension, do it now (Decision #6 option a) — scope-guard if >10 files.
12. Test each executor feature against minimal in-memory fixtures.

**Phase D — Bootstrap Capability Lens e2e (≤ 15K tokens):**
13. Build `internal/refractor/ruleengine/full/bootstrap_e2e_test.go`.
14. Seed a representative graph in test setup (test-only fixtures if needed under `testdata/`).
15. Run `CapabilityLensDefinition` query; assert output rows conform to Contract #6 §6.10 / §6.2 three-section model.
16. Record mean + p95 + p99 latency.

**Phase E — C1/C2 convergence + selection tests (≤ 8K tokens):**
17. Pipeline + lens.Parse converge on RuleEngine.Execute (Decision #10) if scope-safe.
18. Add 2 new selection tests: explicit-full SUCCESS and absent-fallback choosing full.
19. Remove `full.Engine.Execute`'s defensive panic.

**Phase F — Gates + closing (≤ 5K tokens):**
20. `lattice-architecture.md` dependency pin (Decision #13).
21. Run all gates per "Required Verification" section below.
22. Update token tracker.
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

1. ✅ Phase-A architecture spike checkpoint sent
2. ✅ `internal/refractor/ruleengine/full/ast.go` — Refractor-native AST types
3. ✅ `internal/refractor/ruleengine/full/visitor.go` — ANTLR parse-tree → AST
4. ✅ `full.Engine.Parse` replaces stub; cypher types stay inside `full/`
5. ✅ `internal/refractor/ruleengine/full/executor.go` — Core KV + Adjacency KV execution
6. ✅ `full.Engine.Execute` replaces stub panic
7. ✅ Inbound traversal handled per Decision #6 (Adjacency KV index extension OR scan-with-filter)
8. ✅ Soft-delete (`isDeleted: true`) filter applied to every Core KV read in the executor
9. ✅ Parameter substitution for `$actorKey`, `$now`, `$projectedAt`, and arbitrary `$name` parameters
10. ✅ Bootstrap Capability Lens e2e test passes; output rows conform to Contract #6 three-section shape
11. ✅ Latency recorded: mean + p95 (+ p99 if practical) in the test output and closing summary
12. ✅ New selection tests: explicit-full SUCCESS, absent-fallback choosing full
13. ✅ Existing 5 selection tests from 3.1a still pass
14. ✅ C1/C2 (3.1a residuals) resolved per Decision #10 (or TODO with rationale if scope-guarded)
15. ✅ `lattice-architecture.md` openCypher dependency pin updated
16. ✅ ANTLR types do not leak past `internal/refractor/ruleengine/full/` (verify with `grep -rn "antlr\|cypher" internal/refractor` outside the full/ directory)
17. ✅ `go build ./...`, `make vet`, all required tests, `make verify-bootstrap`, `make test-bypass` all green
18. ✅ Token tracker Row 3.1b added — HONEST estimate, round UP
19. ✅ Closing summary including: feature matrix (one row per required cypher feature with PASS/FAIL/PARTIAL), latency mean/p95/p99, residual risks for Story 3.2

## What Story 3.1b Is NOT

- **Not** Capability Lens live activation in production (Story 3.2)
- **Not** Processor step 3 auth (Story 3.3)
- **Not** denial-response shaping (Story 3.4)
- **Not** Capability Lens adversarial suite (Story 3.7)
- **Not** a generalized Cypher database; implement the Story 3.1 / Contract #6 feature set only

## Escalation

Halt and escalate via Andrew/Winston if:
- A required cypher feature cannot be supported cleanly without violating the Adjacency KV / Core KV boundary
- Adjacency KV inbound-index extension requires touching >10 files (Decision #6 scope guard)
- C1/C2 convergence requires touching >5 production files outside `ruleengine/` (Decision #10 scope guard)
- The bootstrap query's seeded-graph requirements need changes to `internal/bootstrap/lenses.go` or the primordial seed
- A CONTRACT-AMENDMENT-REQUEST emerges
- Token estimate exceeds 125K

## Closing

1. Verify all 19 deliverables
2. Run all required gates
3. Update token tracker Row 3.1b — round UP
4. Closing summary as Deliverable #19 (feature matrix + latency + residual risks for 3.2)

Do NOT commit. Winston + Andrew review and commit.
