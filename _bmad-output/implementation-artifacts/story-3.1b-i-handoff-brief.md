---
title: Story 3.1b-i Implementation Handoff Brief
story: 3.1b-i — Full openCypher Engine Visitor + AST (parse-only)
model_tier: Opus (locked)
token_budget: ~70K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-15
predecessor: Story 3.1a (engine boundary + selection, shipped at df187dd)
follow-up: 3.1b-ii will add the executor, bootstrap Capability Lens e2e, latency, C1/C2 convergence, arch doc pin
---

# Story 3.1b-i — Full openCypher Engine: Visitor + AST (parse-only): Handoff Brief

## Your Role

You deliver the **parse half** of Story 3.1b: the visitor that translates ANTLR parse trees into a Refractor-native AST, the AST types themselves, and a real `full.Engine.Parse` that replaces 3.1a's stub. The executor stays stubbed (returns "executor not yet implemented (Story 3.1b-ii)" error). The bootstrap Capability Lens query must PARSE cleanly through your visitor; execution is 3.1b-ii's problem.

Story 3.1b was originally scoped as visitor + executor + e2e in one session; a fresh Opus sub-agent declined to start citing realistic-scope-vs-budget mismatch (visitor + executor + e2e against unfamiliar grammar in 120K). Winston accepted the split. You are 3.1b-i — parse-side only.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables done, deliverables remaining, honest token estimate (lower bound, rounded UP).
- **Halt unconditionally if you estimate > 75K used** (about 7% over budget).
- **Model tier:** Opus only. Halt if Sonnet/Haiku.
- **No PRs.** Direct commit to `main` after Winston review.
- **No git commits by you.** Winston + Andrew commit.
- **DO NOT silently edit planning artifacts.** Use `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` (append).
- **Token tracker:** add Row 3.1b-i at session close — HONEST estimate, round UP.

## What's Already in Place (do NOT redo)

- **Vendored ANTLR parser** at `internal/refractor/ruleengine/full/cypher/` (Cypher.g4 + 4 generated Go files). ANTLR runtime `github.com/antlr4-go/antlr/v4 v4.13.1` in `go.mod`. Lint + vet already exclude this package.
- **Story 3.1a (commit df187dd):** package boundary at `internal/refractor/ruleengine/{simple,full,full/cypher}`, `RuleEngine` interface + `Registry` + `SelectForLens`, Lens schema's `ruleEngine` field, health emission carries resolved engine, 5 selection tests pass.
- **Current `full.Engine`** at `internal/refractor/ruleengine/full/full.go` is a stub: `Parse` returns `ParseError{Engine: "full", Message: "full engine not yet implemented (Story 3.1b)"}`; `Execute` panics defensively.

Tree is clean (`go build ./...` + `make vet` green) at session start.

## Story Scope (3.1b-i ONLY)

**In scope:**
- Refractor-native AST at `internal/refractor/ruleengine/full/ast.go`
- Visitor at `internal/refractor/ruleengine/full/visitor.go` (implements `cypher.CypherListener`, walks parse tree, emits AST)
- `full.Engine.Parse` replaces 3.1a stub — uses cypher package to lex+parse, walks via visitor, returns `ruleengine.CompiledRule` wrapping the Refractor-native AST
- Per-feature **parse tests** (one test per required cypher feature; tests assert AST shape, NOT execution)
- **Bootstrap Capability Lens query parses cleanly** (`internal/bootstrap/lenses.go::CapabilityLensDefinition` — your acceptance oracle)
- `full.Engine.Execute` remains a stub that returns a typed error `"executor not yet implemented (Story 3.1b-ii)"` (replace the panic with a clean error — the executor will land in 3.1b-ii)

**Out of scope (3.1b-ii):**
- Executor (Core KV / Adjacency KV reads, traversal, aggregation, WITH grouping, parameter substitution at runtime, soft-delete filter)
- Bootstrap Capability Lens e2e test
- Latency measurement
- C1/C2 convergence (production execution flow through RuleEngine.Execute)
- `lattice-architecture.md` openCypher dependency pin

## Required Cypher Feature Support (parse-side)

Your visitor + AST must accept (NOT execute) these constructs against the bootstrap query:

- **`MATCH` / `OPTIONAL MATCH`** with simple node patterns + node-label predicates + property predicates (`{key: value}`)
- **Path patterns:** outbound `-[:type]->`, inbound `<-[:type]-`, variable-length `*` quantifiers (specifically `*0..` and bounded `*0..2`)
- **`WHERE` clause:** equality, AND/OR, negated existence (anti-pattern `NOT (path_expression)`)
- **`WITH` clauses** for intermediate aggregation (carries forward named bindings into a follow-on `MATCH`/`RETURN`)
- **`RETURN`** with map literal projections, e.g. `{operationType: perm.operationType, scope: perm.scope}`
- **Function calls in RETURN:** `collect()` with optional `DISTINCT` modifier
- **List concatenation in RETURN/WITH:** `+` operator between list-valued expressions
- **Parameter references:** `$actorKey`, `$now`, `$projectedAt` (visitor records parameter NAMES on the AST; executor binds at runtime in 3.1b-ii)
- **Property access:** `node.prop`, `node.prop.subprop`

If a required feature can't be modeled in the AST or visited cleanly, HALT and escalate. Do NOT ship a visitor that silently drops grammar productions — that's the exact failure mode the parent brief forbids.

## Architectural Decisions Already Made (Winston)

1. **Visitor implements `cypher.CypherListener`** (the generated base listener interface). Override the `Enter*` / `Exit*` methods for the rules you care about; ignore the rest. Use the parse-tree walker idiom standard for ANTLR Go listeners (a stack-based AST builder is the common pattern — push partial nodes on `Enter*`, finalize on `Exit*`).

2. **Refractor-native AST is in `internal/refractor/ruleengine/full/ast.go`** with at minimum:
   ```go
   type Query struct { Clauses []Clause }
   type Clause interface { isClause() }
   type Match struct { Optional bool; Pattern PathPattern; Where Expr }
   type With struct { Items []ProjectionItem; Where Expr }
   type Return struct { Items []ProjectionItem; Distinct bool }
   type PathPattern struct { Nodes []NodePattern; Rels []RelPattern }  // alternating
   type NodePattern struct { Variable, Label string; Properties map[string]Expr }
   type RelPattern struct { Variable, Type string; Direction Direction; MinHops, MaxHops int /* -1 = unbounded */ }
   type ProjectionItem struct { Expr Expr; Alias string }
   type Expr interface { isExpr() }
   // Concrete Expr types: Literal, ParameterRef, PropertyAccess, BinaryOp, FunctionCall, MapLiteral, ListConcat, NotPattern, AndOr, ...
   ```
   Final shape is your call; the above is a sketch. Keep types small, named, and free of ANTLR/cypher imports.

3. **No ANTLR types leak past `internal/refractor/ruleengine/full/`.** Verify with `grep -rn "antlr.\|cypher\." internal/refractor/` — only matches should be inside `full/`.

4. **`full.Engine.Parse` flow:**
   ```
   input := antlr.NewInputStream(ruleBody)
   lexer := cypher.NewCypherLexer(input)
   stream := antlr.NewCommonTokenStream(lexer, 0)
   parser := cypher.NewCypherParser(stream)
   tree := parser.OC_Cypher()
   visitor := newASTVisitor()
   antlr.ParseTreeWalkerDefault.Walk(visitor, tree)
   if visitor.err != nil { return nil, &ruleengine.ParseError{Engine: "full", Message: visitor.err.Error()} }
   return &CompiledRule{Query: visitor.query}, nil
   ```
   Wire ANTLR's `BaseErrorListener` to capture syntax errors and propagate them as `ParseError`.

5. **`full.CompiledRule` satisfies `ruleengine.CompiledRule`** (whatever that interface looks like in 3.1a — read `internal/refractor/ruleengine/ruleengine.go`). Internal shape is your call: `type CompiledRule struct { Query *Query }` is fine.

6. **`full.Engine.Execute` becomes a typed error stub:** replace the defensive panic with `return ruleengine.ProjectionResult{}, errors.New("full engine: executor not yet implemented (Story 3.1b-ii)")`. This is a deliberate intermediate state — 3.1b-ii replaces it.

7. **Parameter handling:** the visitor records parameter NAMES (e.g., `$actorKey` → `ParameterRef{Name: "actorKey"}`) on the AST. Do NOT attempt to bind values at parse time. Binding happens in the executor (3.1b-ii) from `EventContext.Parameters`.

8. **Per-feature parse tests** at `internal/refractor/ruleengine/full/visitor_test.go` (or `parse_test.go` — your choice). One test per required feature:
   - `TestParse_MatchSimple`
   - `TestParse_OptionalMatch`
   - `TestParse_OutboundRel`
   - `TestParse_InboundRel`
   - `TestParse_VarLengthZeroToUnbounded` (`*0..`)
   - `TestParse_VarLengthBounded` (`*0..2`)
   - `TestParse_WhereAndOr`
   - `TestParse_WhereAntiPattern` (`NOT (a)-[:b]->(c)`)
   - `TestParse_WithChain`
   - `TestParse_ReturnMapLiteral`
   - `TestParse_CollectDistinct`
   - `TestParse_ListConcat`
   - `TestParse_Parameters` (asserts `$actorKey`/`$now`/`$projectedAt` are captured as `ParameterRef`)
   - `TestParse_PropertyAccess` (single + nested)
   - `TestParse_BootstrapCapabilityLens` — full body from `internal/bootstrap/lenses.go::CapabilityLensDefinition`; asserts no parse error and that the resulting AST contains at least one MATCH + one OPTIONAL MATCH + one WITH + one collect(DISTINCT) + one anti-pattern + one map literal in RETURN.

   Each test asserts the AST SHAPE (e.g., "after parsing, query has 3 clauses, second is Optional Match with var-length pattern of MinHops=0 MaxHops=2") — NOT execution.

9. **Selection tests from 3.1a still pass.** They use the stub-fails-by-design behavior; once your `full.Engine.Parse` succeeds for valid cypher, the `TestSelectForLens_ExplicitFull_StubFailsByDesign` test will START FAILING (because full no longer fails by design). You have two options:
   - **(a)** Update that test to use a deliberately-malformed rule that BOTH simple AND full reject, so it still asserts the "both engines fail → InvalidRule with both parser errors" path. RENAME to e.g. `TestSelectForLens_ExplicitFull_FailsOnInvalidCypher`.
   - **(b)** Add a NEW test for `ExplicitFull_SUCCESS` using a tiny valid cypher rule, AND keep the original test name but update its rule body.

   Pick whichever is cleaner. The 5 selection-path coverage points (explicit-simple ok, explicit-simple fail, explicit-full ok-or-fail, absent-fallback simple-wins, absent-fallback both-fail) must still all have passing tests.

10. **Absent-fallback test for "full wins after simple fails":** currently the 3.1a `TestSelectForLens_AbsentFallback_BothFail` asserts both fail. With your real `full.Parse`, you now need ONE MORE test: `TestSelectForLens_AbsentFallback_FullWins` — a rule that's NOT valid for simple's grammar but IS valid for full's grammar (e.g., uses `WITH` or `OPTIONAL MATCH` or `*` — features simple doesn't support). Activation succeeds with resolved=`full`. This is a 3.1b-i deliverable because it's directly enabled by your `full.Parse` working.

11. **No CONTRACT-AMENDMENT-REQUEST expected.** If one emerges, append + escalate.

12. **CI gate:** `.github/workflows/ci.yml` is active. After your changes, CI must go green.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.1-handoff-brief.md` | Parent brief — context for the 10 architectural decisions |
| `_bmad-output/implementation-artifacts/story-3.1a-handoff-brief.md` | Predecessor brief — what 3.1a delivered |
| `_bmad-output/implementation-artifacts/story-3.1b-handoff-brief.md` | Original (now-split) 3.1b brief — context only; YOUR scope is just the parse half |
| `internal/refractor/ruleengine/ruleengine.go` | The `RuleEngine` interface + `CompiledRule` shape you satisfy |
| `internal/refractor/ruleengine/full/full.go` | The stub you replace (Parse only — Execute stays stubbed) |
| `internal/refractor/ruleengine/full/cypher/cypher_listener.go` | The generated listener interface methods you override (use grep for `Enter*`/`Exit*` of the rules you need: `Match`, `Where`, `With`, `Return`, `PatternElement`, `RelationshipPattern`, `RangeLiteral`, `Properties`, `MapLiteral`, `FunctionInvocation`, `Parameter`, `ListLiteral`) |
| `internal/refractor/ruleengine/full/cypher/cypher_parser.go` | Reference for rule node types (large — use grep, don't read top-to-bottom) |
| `internal/refractor/ruleengine/full/cypher/Cypher.g4` | The grammar definition — short, scannable for understanding rule structure |
| `internal/bootstrap/lenses.go` | `CapabilityLensDefinition` — your acceptance oracle |
| `internal/refractor/ruleengine/selection_test.go` | The 3.1a selection tests — you update one and add one |

**DO NOT read** the executor reference (`simple/evaluator.go`), the adjacency package, the pipeline, or Contract #6 details. Those are 3.1b-ii's concern.

## Suggested Sequence

**Phase A — Grammar familiarization (≤ 15K tokens):**
1. Read the bootstrap `CapabilityLensDefinition` query body — write down EVERY grammar feature it uses (it's the spec for what your visitor must handle).
2. Grep `cypher_listener.go` for the `Enter*`/`Exit*` methods that correspond to those features. List them.
3. Skim `Cypher.g4` for the rule definitions of those features. Confirm structural correctness of the parse-tree shape you'll see.
4. Send a "Phase A checkpoint": list of features, list of listener methods to override, sketch of AST shape, honest token estimate.

**Phase B — AST + visitor scaffold (≤ 20K tokens):**
5. Build `ast.go` with the AST type set.
6. Build `visitor.go` with the listener struct + state stack + Enter/Exit methods for the simplest rules (Match, Return, NodePattern).
7. Wire `full.Engine.Parse` to lex/parse/walk and return the CompiledRule. Replace the Execute panic with the typed-error stub.
8. Add the simplest parse tests (`TestParse_MatchSimple`, `TestParse_ReturnMapLiteral`).
9. `go build ./...` green.

**Phase C — Required features (≤ 25K tokens):**
10. Add visitor methods for: OPTIONAL MATCH, var-length `*`, inbound rel, WHERE anti-pattern, WITH chain, collect(DISTINCT), list concat, parameters, property access (single + nested), AND/OR.
11. Per-feature parse tests for each.
12. `TestParse_BootstrapCapabilityLens` — full body, asserts shape.

**Phase D — Selection-test updates (≤ 5K tokens):**
13. Per Decision #9: update existing `ExplicitFull_StubFailsByDesign` to use a deliberately-malformed cypher (so both engines still fail).
14. Per Decision #10: add `TestSelectForLens_AbsentFallback_FullWins`.

**Phase E — Gates + closing (≤ 5K tokens):**
15. `go build ./...`, `make vet`, `go test ./internal/refractor/ruleengine/... -count=1`, `go test ./... -p 1 -count=1`, `make verify-bootstrap`, `make test-bypass` all green.
16. Update token tracker Row 3.1b-i — round UP.
17. Closing summary.

## Deliverables Checklist

1. ✅ Phase-A checkpoint sent
2. ✅ `internal/refractor/ruleengine/full/ast.go` — Refractor-native AST types
3. ✅ `internal/refractor/ruleengine/full/visitor.go` — ANTLR parse-tree → AST listener
4. ✅ `full.Engine.Parse` replaces stub (uses cypher package; returns wrapped CompiledRule)
5. ✅ `full.Engine.Execute` becomes a typed-error stub (replaces the defensive panic)
6. ✅ Per-feature parse tests covering all 14 required-feature points (Decision #8 list)
7. ✅ `TestParse_BootstrapCapabilityLens` passes with AST-shape assertions
8. ✅ Decision #9: existing `ExplicitFull_StubFailsByDesign` updated to use malformed-cypher; rename if helpful
9. ✅ Decision #10: new `TestSelectForLens_AbsentFallback_FullWins` test
10. ✅ ANTLR/cypher types do NOT leak past `internal/refractor/ruleengine/full/` (verified by grep)
11. ✅ `go build ./...`, `make vet`, all required tests, `make verify-bootstrap`, `make test-bypass` green
12. ✅ Token tracker Row 3.1b-i added — HONEST estimate, round UP
13. ✅ Closing summary including: feature parse matrix (one row per required feature with PASS/FAIL), AST type listing (one-line per type), residual notes for 3.1b-ii

## What 3.1b-i Is NOT

- **Not** the executor (3.1b-ii)
- **Not** Core KV or Adjacency KV reads
- **Not** the bootstrap Capability Lens e2e (3.1b-ii)
- **Not** latency measurement (3.1b-ii)
- **Not** C1/C2 convergence (3.1b-ii)
- **Not** `lattice-architecture.md` openCypher pin (3.1b-ii)

## Escalation

Halt and escalate via Andrew/Winston if:
- A required grammar feature can't be modeled in the AST cleanly
- The ANTLR listener interface doesn't expose enough parse-tree detail for a required feature
- Token estimate exceeds 75K
- A CONTRACT-AMENDMENT-REQUEST emerges

## Closing

1. Verify all 13 deliverables
2. Run all required gates
3. Update token tracker Row 3.1b-i — round UP
4. Closing summary as Deliverable #13 (feature parse matrix + AST listing + residual notes)

Do NOT commit. Winston + Andrew review and commit.
