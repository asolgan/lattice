# Story 12.2: Invalidation-compiler spike (D-PIPELINE spike)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As the architect,
I want to prove a narrow invalidation compiler can derive the affected-anchor set from the full openCypher AST,
so that 12.3 is built on a validated approach rather than a speculative one.

## ⭐ THIS IS A SPIKE — NON-SHIPPING (read first)

The deliverable is **a decision report + a passing equivalence test + a go/no-go recommendation for 12.3**, NOT production wiring into the pipeline. Build-to-throw, prove-the-approach.

**The spike code MUST NOT touch the live projection path.** Specifically, it MUST NOT:

- modify `internal/refractor/pipeline/pipeline.go`'s live projection / reprojection path;
- replace, wrap, or call-into the production `ActorEnumerator` BFS on any real reprojection (`internal/refractor/pipeline/actor_enumerator.go` is used *as an oracle to diff against*, not swapped out);
- add any `projectionKind` handling to the running engine (that is **12.3/12.4**, explicitly out of scope here);
- add a `projectionKind`/`actorAggregate` aspect, a plan compiler, or any `ProjectionPlan` type to production packages (12.3);
- edit the `capabilityEphemeral` / `myTasks` lens definitions (`packages/orchestration-base/lenses.go`), the canonical-name switch (`cmd/refractor/main.go`), or any frozen contract.

The spike lives in its **own throwaway package** under the established spike home (`internal/spike/invalidation-compiler/` — mirroring the existing `internal/spike/nats-batch/`, `internal/spike/starlark/` pattern, each a self-contained subdir with a README). Nothing under `internal/refractor/**`, `cmd/**`, or `packages/**` changes. The spike **imports** the existing types it reuses (`simple.TraversalStep`/`QueryPlan`, `full` AST, `adjacency`) read-only; it does not modify them.

**The bar for "done" is lower than a shipping story:** the equivalence test passes (`go test` the spike package), `go build ./...` stays green, `go vet ./...` / `make vet` stays green, and the report + go/no-go are written. **Gate 2 / Gate 3 are NOT the bar** for a non-shipping spike (the spike adds no production code path to attack). Do not add a Gate 2/3 vector for this story.

## Context — why this spike de-risks 12.3

The decision record (D-PIPELINE) adopts the brief's option 1 (compiled invalidation plan + a typed `projectionKind: actorAggregate` marker), and Winston's value-add is the claim that **the machinery already exists on both sides**, so 12.3 is tractable rather than an open-ended compiler project. This spike's whole job is to **validate that claim on real lens cypher before 12.3 commits to it**:

- The **simple** engine already compiles reverse-traversal invalidation: `simple.reverseTraverse` (`internal/refractor/ruleengine/simple/evaluator.go:198`) / `walkBackToAnchor` (`:226`) walk `QueryPlan.Steps` **backward** from a changed non-anchor node to the affected anchors. This is the Materializer pattern the brief wants to revive — and it operates on `[]simple.TraversalStep`.
- The **full** engine has a clean, ANTLR-free AST (`internal/refractor/ruleengine/full/ast.go`): `Match` (with `Optional`), `PathPattern` (alternating `Nodes []NodePattern` / `Rels []RelPattern`), `RelPattern` with `Direction` (`DirOut`/`DirIn`/…), `MinHops`, `MaxHops`.

The spike compiles the **real** `capabilityEphemeral` + `myTasks` full-engine ASTs into a `simple.TraversalStep`-shaped step list (anchor → leaf, with direction + hop bounds), runs the **existing** reverse-traversal from a changed non-anchor vertex/link/aspect, and proves the result against the broad `ActorEnumerator` BFS as oracle. If the equivalence + subset properties hold on a fixture graph for **vertex, link, AND aspect** CDC events, 12.3's approach is validated; if they don't, the go/no-go says so and 12.3 reconsiders.

## Acceptance Criteria

(Verbatim backbone from `_bmad-output/planning-artifacts/epics/phase-2-epics.md` § "Story 12.2", re-grounded to the current tree.)

1. **A narrow compiler turns the real full-engine ASTs into a `simple.TraversalStep`-shaped step list.** The spike parses the **actual** `capabilityEphemeral` and `myTasks` cypher (`packages/orchestration-base/lenses.go` — `capabilityEphemeralSpec`, `myTasksSpec`) via the **full** engine (`full.Engine.Parse`, `internal/refractor/ruleengine/full/full.go:39`) to obtain the `full` AST, then walks each `Match`/`OPTIONAL MATCH` `PathPattern` chain (anchor → leaf) into an ordered `[]simple.TraversalStep` (`FromLabel`/`EdgeType`/`Direction`/`ToLabel`, carrying hop bounds from `RelPattern.MinHops`/`MaxHops`). The compiled plan is anchored on the bound `$actorKey` identity (the first node of the first required MATCH), matching both lenses' anchoring.

2. **The compiled plan drives the EXISTING reverse-traversal.** From a changed **non-anchor** node (a task vertex, an `assignedTo`/`forOperation`/`scopedTo`/`reportsTo` link, or a task-`data` aspect), the spike runs `simple.reverseTraverse`/`walkBackToAnchor` (or an in-spike equivalent that calls the same adjacency walk) over the compiled `QueryPlan.Steps` to derive the **affected-anchor set** (identity vertex keys). The spike does **not** reimplement the reverse-walk algorithm — it reuses the simple engine's.

3. **Correctness ORACLE holds on a fixture graph, for vertex, link, AND aspect CDC events.** For each of the three event kinds, with the changed node being a non-anchor in the lens pattern:
   - **(a) SUBSET (precision win):** the compiled affected-anchor set is a **strict/non-strict subset of** the broad `ActorEnumerator` BFS set for the same change (`pipeline.ActorEnumerator.Enumerate`, `internal/refractor/pipeline/actor_enumerator.go:90`). The two are deliberately **NOT equal** — the compiled plan is *precise*, the BFS *over-reprojects* (undirected, depth-bounded, edge-name-blind). At least one fixture event must demonstrate `compiled ⊊ BFS` (a real over-reprojection eliminated), not merely `compiled ⊆ BFS`.
   - **(b) NO MISSED ANCHOR (soundness):** the compiled set **contains every actor whose projection output actually changes.** Verified empirically: **reproject the BFS superset** (run each lens's projection for every actor in the BFS set, before vs. after the fixture change) and diff the outputs; **every actor whose projected doc differs must be in the compiled set.** No actor whose output changed may be missing from the compiled set. (This is the load-bearing safety property — a missed anchor on `capabilityEphemeral` would be a missed revocation.)
   - **(c) RECORD THE WIN:** record `len(BFS) − len(compiled)` (the over-reprojection the plan eliminates) per event, in the report.

4. **The report enumerates exactly which openCypher constructs the narrow compiler COVERS and which it does NOT**, with the fallback policy. Covered (at least): `MATCH` / `OPTIONAL MATCH`, node labels, relationship names + directions, variable-length hops **within the existing cap** (`DefaultActorMaxDepth = 10`, `internal/refractor/pipeline/actor_enumerator.go`), conservative `WHERE` (a `WHERE` that only *narrows* the matched set is safe to ignore for invalidation — it can only make the compiled set a tighter subset, never miss an anchor; the report must state this reasoning explicitly), and simple **path-preserving** `WITH`. NOT covered (enumerate them, with why each is unsafe-to-narrow or unsupported): e.g. `WHERE` that could *broaden* reachability, aggregation/`collect` that changes the source set, computed/synthetic relationships, `UNION`, subqueries, anything the spike can't prove subset-safe. The fallback policy is stated: **non-security** projections may fall back to broad BFS on an uncovered construct; **auth-plane actor-aggregate** lenses (e.g. `capabilityEphemeral`) must compile or **fail activation closed** (12.3 enforces this; the spike only records the policy).

5. **A go/no-go recommendation for 12.3 is recorded** in the report, grounded in the spike's results: GO if the oracle holds for all three event kinds on both lenses with a real precision win and zero missed anchors; NO-GO/CONDITIONAL otherwise, naming the specific construct or property that failed and what 12.3 would have to change.

6. **The spike is non-shipping and isolated.** No file under `internal/refractor/**`, `cmd/**`, `packages/**`, or `docs/contracts/**` is modified. `go build ./...`, `go vet ./...` (and `make vet`) stay green. The spike package's `go test` passes. (Gate 2 / Gate 3 are explicitly not run/extended for this story.)

## Tasks / Subtasks

- [ ] **Task 1 — stand up the isolated spike package** (AC: #6)
  - [ ] Create `internal/spike/invalidation-compiler/` (mirror the existing `internal/spike/nats-batch/` / `internal/spike/starlark/` convention: a self-contained subdir, package name e.g. `invalidationcompiler`, with a `README.md`).
  - [ ] The package **imports** (read-only) `internal/refractor/ruleengine/full`, `internal/refractor/ruleengine/simple`, `internal/refractor/adjacency`, and `internal/refractor/pipeline` (for `ActorEnumerator`). It modifies none of them.
  - [ ] Confirm the spike adds **zero** production code path: nothing under `internal/refractor/**`/`cmd/**`/`packages/**` is edited. (If you find you *need* to export a currently-unexported helper from `simple`/`pipeline` to reuse it, prefer copying the small walk into the spike over editing production — see Open Questions #2; do not widen a production package's API for a throwaway.)

- [ ] **Task 2 — the narrow AST→TraversalStep compiler** (AC: #1)
  - [ ] Parse the two real specs (`capabilityEphemeralSpec`, `myTasksSpec`, copied verbatim from `packages/orchestration-base/lenses.go` into the spike as test inputs — or imported if exported; see Open Questions #1) through `full.Engine{}.Parse(spec)` to get the `full` AST (`*full.Query` via the engine's compiled-rule shape — inspect `full.go` for how to reach the AST from a `CompiledRule`).
  - [ ] Walk each `Match` clause's `PathPattern`s (`Nodes`/`Rels`) into `[]simple.TraversalStep`: for rel `i`, `FromLabel = Nodes[i].Label`, `ToLabel = Nodes[i+1].Label`, `EdgeType = Rels[i].Type`, `Direction =` (map `full.Direction` → `simple.EdgeDirection`), carrying `MinHops`/`MaxHops`. This mirrors the simple engine's own `Compile` (`internal/refractor/ruleengine/simple/compiler.go:37–53`) — use it as the reference shape, but the spike compiles from the **full** AST (which the simple `Compile` does not consume), which is the new bit being proven.
  - [ ] **Direction mapping is load-bearing and must be exact** — the reverse-walk reverses it (`reverseDirection`, `evaluator.go:228`). `capabilityEphemeral`'s manager-delegation hop `(identity)<-[:reportsTo]-(report:identity)<-[:assignedTo]-(task2:task)` is the multi-hop / direction-sensitive case; get its two inbound hops right. Map `full.DirOut`→`simple.Outbound`, `full.DirIn`→`simple.Inbound` (verify the exact `simple.EdgeDirection` enum names in `plan.go`).
  - [ ] **Edge-type casing:** the simple engine matches `EdgeType` against `adjacency.EdgeEntry.Name` (`evaluator.go:259`, `filterEdges`). The lens cypher uses relation names like `assignedTo`/`forOperation`/`scopedTo`/`reportsTo` (Contract #1 lowerCamel). Ensure the compiled `EdgeType` matches whatever the **adjacency entries in your fixture** carry (build the fixture with the same names) — a casing/naming mismatch silently yields an empty affected set and a false "subset" pass. Assert non-empty where a hit is expected.

- [ ] **Task 3 — build a fixture graph + adjacency** (AC: #2, #3)
  - [ ] Construct a small in-memory (or test-JetStream-KV) fixture: a few `identity` vertices (including a manager with `reportsTo` reports), several `task` vertices `assignedTo` those identities, `forOperation`→`op` and `scopedTo`→`tgt` links, with adjacency entries written the way the real bootstrapper writes them (`adjacency.EdgeEntry{Name, Direction, OtherNodeID, OtherType}`, `internal/refractor/adjacency/builder.go:22`; written via `adjacency` store helpers — see `adjacency/store.go` `Neighbors` for the read shape you must satisfy). Reuse `internal/refractor/fixture/` helpers if they fit; otherwise a minimal local fixture is fine (it's a spike).
  - [ ] The fixture must contain at least one identity reachable by the BFS but **not** by the directed compiled plan (an over-reprojection case — e.g. an identity linked to the changed task by an undirected adjacency hop that the lens pattern's direction/edge-name excludes), so AC #3(a)'s strict-subset demonstration is real.
  - [ ] Provide the three changed-node kinds: a **vertex** event (a `task` vertex changes), a **link** event (an `assignedTo` / `forOperation` / `scopedTo` / `reportsTo` link changes), and an **aspect** event (a `task.data.*` aspect like `expiresAt`/`status` changes). For each, derive the event's non-anchor node identity to feed both the reverse-walk and `ActorEnumerator.Enumerate(ctx, eventVertexKey, eventVertexType)`.

- [ ] **Task 4 — run the existing reverse-traversal over the compiled plan** (AC: #2)
  - [ ] For each event, run the affected-anchor derivation using the simple engine's reverse-walk semantics over the compiled `QueryPlan{Steps}`. Prefer calling `simple.reverseTraverse` directly if reachable from the spike package; if it is unexported and you choose not to export it (Open Questions #2), copy the ~30-line `reverseTraverse`/`walkBackToAnchor`/`filterEdges` walk into the spike **verbatim** (a throwaway copy is acceptable for a spike and keeps production untouched) and note the copy in the report so 12.3 knows it must call the real one.
  - [ ] The output is the **compiled affected-anchor set** (identity vertex keys) per event.

- [ ] **Task 5 — the oracle: subset + no-missed-anchor + win count** (AC: #3)
  - [ ] **Subset:** assert `compiled ⊆ BFS` for all three events; assert at least one event has `compiled ⊊ BFS` (strict) and record `len(BFS) − len(compiled)`.
  - [ ] **No missed anchor (the safety property):** for each event, reproject **both lenses for every actor in the BFS superset** before and after the fixture change, diff each actor's projected output, collect the set of actors whose output **actually changed**, and assert that set ⊆ the compiled set (every changed-output actor is caught by the compiled plan). To reproject, run the lens projection the way the spike already parses it (full engine evaluate against the fixture for a bound `$actorKey`) — or, if wiring a full evaluate is heavy, approximate the "output changed" predicate conservatively but **soundly** (e.g. an actor's output changes iff the changed node is on a directed lens path to that actor) and justify the predicate's soundness in the report. Prefer the real reproject-and-diff if tractable; the AC's intent is an **empirical** no-missed-anchor check, not a tautology.
  - [ ] These assertions ARE the equivalence test (`go test ./internal/spike/invalidation-compiler/...`). Make failures explanatory (print compiled vs BFS vs changed-output sets on mismatch).

- [ ] **Task 6 — write the spike decision report + go/no-go** (AC: #4, #5)
  - [ ] Write the report as a doc under **`docs/`** (house rule: new docs → `/docs`, close to the code) — e.g. `docs/decisions/12.2-invalidation-compiler-spike-report.md` (alongside the existing `docs/decisions/projection-plane-decomposition.md`). The README in the spike package points to it.
  - [ ] Report contents: (a) the compiled step lists for both lenses (show the actual `[]TraversalStep`); (b) per-event subset/win numbers (`len(BFS)−len(compiled)`); (c) the no-missed-anchor result; (d) the **covered vs not-covered openCypher construct table** with the subset-safety reasoning per construct (esp. why a *narrowing* `WHERE` is safe to ignore and a *broadening* one is not); (e) the fallback policy (non-security → BFS fallback; auth-plane → fail-closed); (f) the **go/no-go for 12.3** with specifics; (g) a "what 12.3 inherits / must do for real" note (call the real `reverseTraverse`, not the spike copy; enforce fail-closed; handle the constructs the spike punted on).

- [ ] **Task 7 — verification (spike bar, NOT Gate 2/3)** (AC: #6)
  - [ ] `go build ./...` — green (the spike package compiles, nothing else changed).
  - [ ] `go vet ./...` and `make vet` — green.
  - [ ] `go test ./internal/spike/invalidation-compiler/... -count=1` — the equivalence/oracle test passes.
  - [ ] Confirm `git status` shows changes ONLY under `internal/spike/invalidation-compiler/` and `docs/` (and this story file). If anything under `internal/refractor/**`/`cmd/**`/`packages/**` shows as modified, STOP — that violates the spike's isolation rule.
  - [ ] Do **not** run/extend `make test-bypass` / `make test-capability-adversarial` for this story (no production attack surface added).

## Dev Notes

### Where the spike lives (and why there)

`internal/spike/` is an **established convention** in this repo (`nats-batch/`, `starlark/` — each a self-contained subdir with a README and throwaway code that imports production read-only). Put this spike at `internal/spike/invalidation-compiler/`. This is the single most important structural decision: it guarantees the spike cannot touch the live projection path, satisfying the non-shipping mandate by construction.

### The two real lens cyphers (compile THESE, not invented patterns)

Both are in `packages/orchestration-base/lenses.go`:

- **`myTasksSpec`** (`:71`): `MATCH (identity:identity {key:$actorKey})`, then `OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task) WHERE task.data.status='open'`, `OPTIONAL MATCH (task)-[:forOperation]->(op)`, `OPTIONAL MATCH (task)-[:scopedTo]->(tgt)`. Anchor = `identity`. Non-anchor changeable nodes: `task`, `op`, `tgt` and the `assignedTo`/`forOperation`/`scopedTo` links + the `task.data.status` aspect.
- **`capabilityEphemeralSpec`** (`:112`): same anchor + the same three optional hops on the direct task, **plus** the manager-delegation 2-hop `OPTIONAL MATCH (identity)<-[:reportsTo]-(report:identity)<-[:assignedTo]-(task2:task)` and its `forOperation`/`scopedTo` hops. This is the **multi-hop, direction-sensitive** case that exercises the reverse-walk's direction reversal across two inbound hops — the most valuable thing the spike proves. `WHERE task.data.expiresAt > $now` is a *narrowing* filter (only live grants); the report must argue ignoring it for invalidation is subset-safe.

Both are anchored on the bound `$actorKey` identity (the lens comments say so explicitly: "Anchored on the bound identity … so reprojection traverses adjacency from the actor"). That anchoring is exactly what makes the reverse-walk applicable.

### The machinery to reuse (do not reinvent)

- **Reverse-walk:** `simple.reverseTraverse` (`internal/refractor/ruleengine/simple/evaluator.go:198`) and `walkBackToAnchor` (`:226`) walk `plan.Steps` backward from a changed node (matched by `step.ToLabel == entry.NodeLabel`) to the anchor at step 0, reversing each `step.Direction` via `reverseDirection` (`:228`) and filtering adjacency edges by `EdgeType` + reversed direction (`filterEdges`, `:256`). It reads adjacency via `adjacency.Neighbors(ctx, adjKV, adjID)` (`internal/refractor/adjacency/store.go:17`).
- **TraversalStep / QueryPlan:** `internal/refractor/ruleengine/simple/plan.go:15` (`TraversalStep{FromVariable,FromLabel,EdgeType,Direction,ToVariable,ToLabel,Optional}`) and `:26` (`QueryPlan{AnchorLabel,AnchorVariable,Steps,Columns}`). `EdgeDirection` enum (`Outbound`/`Inbound`/…) is in `plan.go` — verify exact names.
- **Full AST:** `internal/refractor/ruleengine/full/ast.go` — `Query.Clauses` (`:38`), `Match{Optional,Patterns,Where}` (`:47`), `PathPattern{Nodes,Rels}` (`:75`), `NodePattern{Variable,Label,Properties}` (`:81`), `RelPattern{Variable,Type,Direction,MinHops,MaxHops,Properties}` (`:97`), `Direction` enum `DirOut`/`DirIn`/… (`:10–24`). Parse via `full.Engine{}.Parse(spec)` (`internal/refractor/ruleengine/full/full.go:39`) — inspect how to reach the `*full.Query` AST from the returned `ruleengine.CompiledRule`.
- **Broad BFS oracle:** `pipeline.NewActorEnumerator(adjKV, coreKV, "identity")` (`:62`) → `.Enumerate(ctx, eventVertexKey, eventVertexType)` (`:90`). Returns full `vtx.identity.<NanoID>` keys. Undirected, depth-bounded BFS (`DefaultActorMaxDepth=10`) — this is *exactly* the over-reprojection the compiled plan improves on. Use it **as the oracle to diff against**, never as a thing the spike replaces.
- **Adjacency fixture shape:** `adjacency.EdgeEntry{Name,Direction,OtherNodeID,OtherType}` (`internal/refractor/adjacency/builder.go:22`). Build fixture adjacency with the **same relation names + directions** the lens cypher uses, or the walk silently returns empty (false pass — assert non-empty).

### Why subset-but-not-equal is the *correct* shape (don't "fix" it to equal)

The BFS is intentionally broad: undirected, edge-name-blind, depth-capped — it reprojects every actor *near* the change. The compiled plan follows only the lens's **directed, named** paths, so it is a precise subset. The spike *proving they're equal* would be a bug in the fixture (too simple a graph). The win is the over-reprojection eliminated (`len(BFS)−len(compiled) > 0` for at least one event). Pair the subset with the **empirical no-missed-anchor** check (reproject the superset, diff outputs) — subset alone is not safety; subset + no-missed-output-change is.

### Conservative WHERE — the subtle correctness argument the report must make

A `WHERE` that only **narrows** the matched set (`task.data.status='open'`, `expiresAt > $now`) is safe to **ignore** for invalidation: ignoring it can only make the compiled affected-anchor set *larger* (a superset of the truly-affected, still a subset of BFS), so it never *misses* an anchor — at worst it reprojects an actor whose output didn't change (a harmless over-reprojection, which the reproject-and-diff tolerates). A `WHERE` that could **broaden** reachability (e.g. an `OR` pulling in an unrelated path, or a predicate the compiler can't bound) is **not** subset-safe and goes in the "not covered → fail-closed for auth lenses" column. Spell this out — it is the heart of why the narrow compiler is sound.

### House rules (CLAUDE.md)

- **No history/changelog comments in code.** Even in throwaway spike code: comments describe what the code does now, not "// was BFS" / "// Story 12.2".
- **Contract #1 key shapes.** Fixture vertices are `vtx.identity.<NanoID>`, `vtx.task.<NanoID>`; links are 6-segment `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>`. Relation names read as a sentence (`task assignedTo identity`, `task forOperation op`, `task scopedTo tgt`, `report reportsTo identity`). Get these right or the adjacency walk misses.
- **New docs → `/docs`.** The spike report is a doc → `docs/decisions/`. The spike package README stays in the package dir (it documents the throwaway code, mirroring `internal/spike/*/README.md`).
- **Sub-agents do not commit/push/branch** — leave changes in the working tree for Winston to adjudicate.

### Project Structure Notes

- All new code under `internal/spike/invalidation-compiler/` (Go package + `_test.go` + `README.md`). The report under `docs/decisions/`. **Nothing else changes.** This is the structural guarantee that the spike is non-shipping.
- The spike imports production packages read-only. If reuse forces a production API change (exporting `reverseTraverse`), prefer copying the small walk into the spike instead — keep the spike's blast radius at zero production files (Open Questions #2).

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 12.2] — ACs (verbatim backbone), spike non-shipping mandate, the oracle (subset + no-missed-anchor + win count), covered/not-covered constructs, fallback policy, go/no-go.
- [Source: docs/decisions/projection-plane-decomposition.md#D-PIPELINE] — "the machinery already exists on both sides"; the reverse-walk + full-AST reuse argument; fail-closed-on-security-plane fallback policy; sequencing (spike 12.2 → compiler 12.3 → migrate 12.4).
- Code: `packages/orchestration-base/lenses.go` — `capabilityEphemeralSpec` :112, `myTasksSpec` :71 (the real cypher to compile).
- Code: `internal/refractor/ruleengine/simple/evaluator.go` — `reverseTraverse` :198, `walkBackToAnchor` :226, `reverseDirection` :228, `filterEdges` :256.
- Code: `internal/refractor/ruleengine/simple/plan.go` — `TraversalStep` :15, `QueryPlan` :26, `EdgeDirection` enum.
- Code: `internal/refractor/ruleengine/simple/compiler.go` — `Compile` :14, step-building loop :37–53 (reference shape; compiles from simple AST — spike compiles from full AST).
- Code: `internal/refractor/ruleengine/full/ast.go` — `Query` :38, `Match` :47, `PathPattern` :75, `NodePattern` :81, `RelPattern` :97, `Direction` :10.
- Code: `internal/refractor/ruleengine/full/full.go` — `Engine.Parse` :39 (how to reach the AST).
- Code: `internal/refractor/pipeline/actor_enumerator.go` — `ActorEnumerator` :44, `NewActorEnumerator` :62, `Enumerate` :90, `DefaultActorMaxDepth` (=10).
- Code: `internal/refractor/adjacency/store.go` — `Neighbors` :17; `internal/refractor/adjacency/builder.go` — `EdgeEntry` :22 (fixture adjacency shape).
- Convention: `internal/spike/nats-batch/`, `internal/spike/starlark/` — existing self-contained spike packages with READMEs (the home + pattern to mirror).

## Previous Story Intelligence

- **Story 12.1a (just done, the immediately-prior Epic 12 story)** plumbed `projectionSeq` end-to-end and made the NATS-KV adapter guard the two at-risk lenses (`capabilityEphemeral`, `myTasks`). That story's `Dev Agent Record` confirms these two lenses are the actor-aggregate, adjacency-traversed projections this spike compiles — and that their **anchoring on `$actorKey`** plus **adjacency reprojection of affected actors** is real, live behavior (12.1a relies on `evalLinkFanOut`/`evalAspectFanOut` reprojecting affected actors). The spike does **not** depend on the 12.1a guard and must not touch it; it operates one layer up (which actors to reproject), where 12.1a operated on the write (whether a reprojection's write wins).
- **12.1a established the Epic-12 working pattern**: implementation isolated to the right package, proof in a dedicated test, docs under `docs/`, status in the story file. The spike follows the same shape, swapping the Gate 3 adversarial proof (12.1a, a security write-path change) for an equivalence test (12.2, a non-shipping read-side spike) — because the spike adds no attackable production path.
- The decision record's D-PIPELINE sequencing is explicit: **12.2 (this) proves the compiler equals/subsets the BFS on a fixture → 12.3 builds the plan compiler + `projectionKind` marker → 12.4 migrates the built-ins off the switch and deletes it.** The spike's go/no-go directly gates whether 12.3 proceeds as designed.

## Git Intelligence

- Recent commits (`29b9536` etc.) ratified + applied the Epic 12 contract amendments and shipped 12.1a. **This spike touches no contract and ships no production code** — it adds an isolated spike package + a decision report only. Raise no CONTRACT-AMENDMENT-REQUEST (nothing here changes a contract).
- The repo pattern for spikes is set by `internal/spike/*` (throwaway, self-contained, README'd). Follow it; do not invent a new location.

## Open Questions

1. **Re-parse the lens specs, or import the constants?** `capabilityEphemeralSpec`/`myTasksSpec` are **unexported** consts in `packages/orchestration-base`. The spike can either (a) copy the two cypher strings verbatim into the spike's test inputs (zero production change, but the spike could drift if the real lens cypher changes later), or (b) export the consts / a `Specs()` accessor from `orchestration-base` (keeps them in sync, but edits a production package — against the spike's zero-blast-radius rule). **Recommendation: (a) copy verbatim**, with a `// source: packages/orchestration-base/lenses.go` reference comment, and the report notes the spike pins a snapshot. Confirm.

2. **Call the real `reverseTraverse`, or copy it into the spike?** `reverseTraverse`/`walkBackToAnchor`/`filterEdges` are **unexported** in `package simple`. To reuse them the spike must either (a) export them from `simple` (production API change — violates zero-blast-radius), or (b) copy the ~50 lines into the spike package verbatim. **Recommendation: (b) copy** (a throwaway copy is acceptable for a spike; the report flags that 12.3 must call the *real* one, not the copy). Equally, the spike could be written as a `_test.go` in `package simple` to reach the unexported funcs — but that would put spike code inside the production package (and risk it being mistaken for a real test). **Prefer the isolated copy.** Confirm — this is the main "how do we reuse unexported production code without editing production" decision.

3. **Real reproject-and-diff vs. a sound conservative "output-changed" predicate for the no-missed-anchor check (AC #3b).** Wiring a full `full`-engine evaluate of each lens for every BFS actor (before/after the change) is the most faithful oracle but the heaviest spike code. A lighter alternative is a **provably-sound** predicate ("an actor's output changes ⟹ the changed node lies on a directed lens path to that actor"), asserted to be a superset of the true changed-output set. **Recommendation: attempt the real reproject-and-diff first** (it's the AC's literal intent and the strongest evidence for the go/no-go); fall back to the justified predicate only if the evaluate wiring balloons the spike — and if so, state the predicate's soundness argument in the report. Confirm which bar Winston wants.

## Adjudication (Winston, 2026-06-14) — all three resolved; build to these

1. **Lens specs → (a) copy verbatim** into the spike test inputs with a `// source: packages/orchestration-base/lenses.go (capabilityEphemeralSpec / myTasksSpec, snapshot 2026-06-14)` comment. Zero production blast radius; the report records that the spike pins a snapshot and 12.3 will compile the live specs.

2. **`reverseTraverse`/`walkBackToAnchor`/`filterEdges` → (b) copy verbatim** into the spike package (throwaway). Two hard requirements so the proof transfers: (i) it must be a **verbatim** copy, not a reimplementation — divergence would make the spike prove the wrong thing; head the copy with `// VERBATIM copy of internal/refractor/ruleengine/simple/{evaluator,plan}.go (snapshot 2026-06-14) — spike only; 12.3 wires the real functions`; (ii) the **report's go/no-go must explicitly call out** that 12.3's production wiring exports/relocates the real functions (or moves the compiler into `package simple`) and does NOT ship this copy. Do NOT add a `_test.go` to `package simple` (keeps the zero-production-files-edited guarantee clean).

3. **No-missed-anchor check → attempt the REAL reproject-and-diff first** (AC #3b's literal intent, the strongest evidence for a security-critical go/no-go): for each CDC event, run the `full` engine evaluate of the lens over the BFS superset before/after and diff the outputs; assert the compiled affected-anchor set ⊇ {actors whose output changed}. Only if the evaluate wiring genuinely balloons the spike, fall back to the **provably-sound conservative predicate** — and if so, the report MUST state the predicate's soundness argument (changed-output ⟹ changed node on a directed lens path to the actor) and flag that 12.3/its review must validate it against the real evaluate. State in the Dev Record which bar you hit.

**Review depth (stated explicitly so it can be overridden):** this is a **non-shipping spike** — zero production code, no Gate-2/3 attack surface. So the plan is a **thorough lead (Winston) review** focused on the one thing that matters — **oracle soundness** (is the ⊆ assertion real and strict where claimed; is the no-missed-anchor diff actually sound, not vacuously passing) + the report's go/no-go reasoning — **plus ONE adversarial sub-agent** attacking the oracle's soundness, rather than the full 3-layer (which is reserved for production/security-plane changes). The 12.3 build that consumes this go/no-go IS security-critical and gets full 3-layer.

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (Amelia, dev-story).

### Debug Log References

- `go test ./internal/spike/invalidation-compiler/... -count=1 -v` — all PASS.
- `go build ./...` — green. `make vet` — green. `golangci-lint run ./internal/spike/...` — 0 issues.
- `git status --porcelain` isolation check — only the spike package, the docs
  report, and this story file are new; nothing under
  `internal/refractor/**`/`cmd/**`/`packages/**`/`docs/contracts/**` changed.

### Completion Notes List

- **No-missed-anchor bar hit: the REAL reproject-and-diff (decision #3, the
  higher bar), NOT the conservative predicate fallback.** Every BFS-superset
  actor is reprojected through the production `full.Engine.ExecuteWith` before/
  after each fixture mutation and the projected rows diffed; the assertion is
  `{changed-output actors} ⊆ {compiled set}`. The full-engine evaluate wired
  cleanly (no ballooning) using the existing contract-test fixture shape.
- **Headline finding (load-bearing for 12.3's go/no-go):** the naive
  flatten-to-one-`Steps`-slice + verbatim reverse-walk approach is **UNSOUND for
  multi-branch lenses** (`capabilityEphemeral`). The spike's no-missed-anchor
  diff EMPIRICALLY caught a dropped manager anchor on the
  `capabilityEphemeral`/`link_assignedTo` event (a missed revocation). Fixed in
  the COMPILER layer (not the verbatim walk) by segmenting the AST into a forest
  of per-branch linear chains and running the unchanged reverse walk per branch.
  This per-branch segmentation is now a HARD requirement for 12.3, recorded in
  the report's go/no-go.
- **Over-reprojection win (BFS − compiled), per event:** vertex_task=2,
  link_assignedTo=2 (myTasks) / 1 (capabilityEphemeral), aspect_task_data=2.
  Strict subset (`compiled ⊊ BFS`) demonstrated on every event for both lenses.
  The capabilityEphemeral/link_assignedTo row catches 2 actors (rep + mgr via
  delegation), matching changed-output=2 — the corrected forest compiler is sound.
- **Decisions honored:** (1) lens specs copied VERBATIM into `specs.go` with the
  `// source:` snapshot comment; (2) `reverseTraverse`/`walkBackToAnchor`/
  `filterEdges`/`reverseDirection` copied VERBATIM into `reverse_copy.go` with the
  required header; the report's go/no-go explicitly states 12.3 exports/relocates
  the real functions and does NOT ship the copy; no `_test.go` added to
  `package simple`; (3) real reproject-and-diff as above.
- **GO recommendation** for 12.3 — conditional on the per-branch segmentation,
  real-function wiring, re-referenced-anchor label backfill, and fail-closed
  enforcement (full detail in the report).

### File List

- `internal/spike/invalidation-compiler/specs.go` (new) — verbatim lens cypher snapshots
- `internal/spike/invalidation-compiler/compiler.go` (new) — full AST → per-branch QueryPlan forest
- `internal/spike/invalidation-compiler/reverse_copy.go` (new) — verbatim reverse-walk copy
- `internal/spike/invalidation-compiler/equivalence_test.go` (new) — the correctness oracle
- `internal/spike/invalidation-compiler/README.md` (new)
- `docs/decisions/12.2-invalidation-compiler-spike-report.md` (new) — decision report + go/no-go
