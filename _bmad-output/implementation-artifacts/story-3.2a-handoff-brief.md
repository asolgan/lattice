---
title: Story 3.2a Implementation Handoff Brief
story: 3.2a — Capability Lens Live Activation Through Real Pipeline
model_tier: Opus (locked)
token_budget: ~120K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-15
predecessor: Story 3.1b-ii (full engine executor + bootstrap e2e, shipped at ce13ef2)
---

# Story 3.2a — Capability Lens Live Activation Through Real Pipeline: Handoff Brief

## Your Role

You activate the bootstrap-seeded Capability Lens(es) against the real Refractor pipeline (NOT just in the test harness). The full engine works end-to-end in tests (3.1b-ii: p99=11.7ms with synthetic keys); the seeded `vtx.meta.<NanoID>` Lens definitions are present in the primordial bootstrap; the `capability-kv` bucket is provisioned. Your job is to land the production wiring that the 3.1b-ii closing summary deferred: route the pipeline through `RuleEngine.Execute`, populate `EventContext.Parameters` from the live event/clock, fix the adjacency NodeID-extraction issue (NOT a validator change — see §"Carries from 3.1b-ii"), reshape the NatsKVAdapter output to Contract #6 §6.2 envelope shape, and demonstrate a real Capability KV entry appearing under `cap.<actor-suffix>` after one identity mutation.

Story 3.2b will follow with the secondary `capabilityRoleIndex` Lens validation, multi-identity e2e, contract-conformance byte test, tombstone re-projection, and NFR-P3 health emission.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **Token budget is for tracking only, NOT a halt threshold.** Original estimate ~120K. Record actual outer-telemetry consumption in the tracker at session close. Do NOT stop work based on token count.

- **Halt and escalate** if you find yourself in any of these patterns:
  - Re-attempting the same operation after 3+ failures
  - Making changes you immediately revert
  - Re-reading the same files looking for an answer that isn't there
  - Cycling between two failed approaches without convergence
  - Stuck on a test that fails for a reason you can't reduce after two debugging attempts

  These are stuck-loop signals. Token consumption alone is NOT a halt signal.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables done, deliverables remaining, honest token estimate, and any concerns.

- **Model tier:** Opus only. Halt if Sonnet/Haiku.
- **No PRs.** Direct commit to `main` after Winston review.
- **No git commits by you.** Winston + Andrew commit.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 + Contract #1 §1.5 + `_bmad-output/planning-artifacts/epics.md` Story 3.2 are source of truth.
- **DO NOT silently edit planning artifacts.** If a contract gap appears, append to `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` and escalate.
- **Token tracker:** update Row 3.2a at session close with outer-telemetry actual.

## What's Already in Place (do NOT redo)

- **Story 3.1a/b (df187dd, e9d30c4, ce13ef2):** `ruleengine.RuleEngine` interface + selection registry; `full.Engine.Parse` (AST + visitor); `full.Engine.ExecuteWith` real executor (latency p99=11.7ms in bootstrap e2e); `EventContext.Parameters` field exists; `simple` engine isolated behind adapter at `ruleengine/simple/`.
- **Bootstrap (internal/bootstrap/lenses.go):** `CapabilityLensDefinition()` (canonical name `capability`, target bucket `capability`) and `CapabilityRoleIndexLensDefinition()` (canonical name `capabilityRoleIndex`, target bucket `capability`) both seeded as `meta.lens` records during primordial bootstrap.
- **Capability KV bucket (internal/bootstrap/primordial.go:25,71):** `capability-kv` bucket is provisioned at bootstrap with description "Lattice Capability KV — Refractor projection targets".
- **CoreKVSource watch (internal/refractor/lens/corekv_source.go):** activates lens definitions arriving on `vtx.meta.>` with class `meta.lens`. Seeded primordial lenses will appear on this watch at startup.
- **cmd/refractor activation (cmd/refractor/main.go:280-294):** dev-mode `REFRACTOR_BOOTSTRAP_LENS` env-gated path activates `lens.BootstrapLens()` as a fallback. Production path: CoreKVSource activates seeded `meta.lens` records automatically.

Tree is clean at session start (`go build`, `make vet`, `make verify-bootstrap`, `make test-bypass` all green at commit 0b8ec0a).

## Story Scope (3.2a)

**In scope:**
1. **Adjacency NodeID-extraction fix** in both engine call sites (full + simple).
2. **C1 convergence finish:** route pipeline execution through `RuleEngine.Execute` (or `ExecuteWith` if engine-neutral signature stays narrow) instead of `simple.Evaluate` directly.
3. **Live `EventContext.Parameters` wiring** at the production caller — `$actorKey`, `$now`, `$projectedAt` populated from event/clock.
4. **Capability KV envelope reshape** — `NatsKVAdapter` (or a thin adapter layer above) emits the Contract #6 §6.2 envelope shape (`key, actor, version, projectedAt, projectedFromRevisions, lanes, platformPermissions, serviceAccess, ephemeralGrants, roles`) for the Capability Lens target, NOT raw projection row.
5. **Primary Capability Lens activation through real pipeline.** When `make up` completes and bootstrap seeds `CapabilityLensDefinition`, Refractor's CoreKVSource watch picks it up, the full engine parses the cypher rule, and the pipeline provisions a durable consumer on Core KV.
6. **One-identity live e2e** — seed one identity + role + permission + a service-availability topology via Core KV writes (NOT through Processor — direct fixture writes are fine); demonstrate `cap.<actor-suffix>` entry appearing under `capability-kv` bucket with three Contract #6 sections populated.
7. **Health KV reports `active`** for both seeded Capability Lenses within the 10s NFR-O1 window.

**Out of scope (defer to 3.2b):**
- Multi-identity e2e (3 identities per Story 3.2 AC)
- Contract-conformance byte test (`capability_lens_contract_test.go`)
- Tombstone re-projection semantics
- `health.refractor.<instance>.lens.capability.*` latency emission (NFR-P3 visibility)
- Secondary `capabilityRoleIndex` Lens RETURN-shape verification (its activation should ride along but you don't have to assert its specific output shape in 3.2a)
- Processor step 3 auth (Story 3.3)

**Hard escalation triggers:**
- A required wiring needs a contract change → CONTRACT-AMENDMENT-REQUEST + escalate.
- Adjacency NodeID fix requires >5 file touches outside the two engine call sites.
- C1 convergence requires reshaping the `RuleEngine.Execute` signature in a way that touches >5 packages.
- Envelope reshape requires changing the `adapter.Adapter` interface (a 2.1 Decision #8 prohibition).
- One-identity e2e fails for a reason you can't reduce after two debug attempts.

## Carries from 3.1b-ii (REQUIRED CONTEXT)

The 3.1b-ii closing summary identified four residual items, of which three are 3.2a's responsibility:

### 1. Adjacency NodeID extraction (HIGHEST priority — closing-summary mis-framed)

The closing summary said: *"`subjects.AdjKey` forbids dots, so `adjacency.Build/Neighbors` cannot directly take Contract #1 `vtx.<type>.<id>` keys. Tests use synthetic Materializer-style keys to work around."*

**This framing is wrong.** Andrew (PO) and Winston investigated. The actual situation:

- `subjects.AdjKey(nodeID string)` correctly expects a **bare NodeID** (NanoID), not a Core KV key. The dot rejection is appropriate NATS-subject hygiene.
- Materializer always passed a bare `NodeID` — verified in `internal/refractor/adjacency/builder.go` (`CoreKVEvent.NodeID` field; `EdgeEntry.OtherNodeID` is also a bare NodeID).
- The bug is at the two **callers**: `internal/refractor/ruleengine/full/executor.go:528` passes `f.node.key` (the full `vtx.<type>.<id>` key) to `adjacency.Neighbors`. The simple engine has the same shape at `internal/refractor/ruleengine/simple/evaluator.go:108,203` — though it's latent (only fixture tests exercise it, with synthetic `node_*` keys).

**The fix is one-line at each call site:** before calling `adjacency.Build` / `adjacency.Neighbors` with a Core KV key, extract the NodeID via `substrate.ParseVertexKey(key)` and pass `nodeID`.

```go
// WRONG (3.1b-ii executor.go:528):
edges, err := adjacency.Neighbors(ex.adjKV, f.node.key)

// RIGHT:
_, nodeID, ok := substrate.ParseVertexKey(f.node.key)
if !ok {
    return nil, fmt.Errorf("full executor: cannot extract NodeID from key %q", f.node.key)
}
edges, err := adjacency.Neighbors(ex.adjKV, nodeID)
```

After the fix:
- The 3.1b-ii test workarounds (`fromKey: "alice"`, `node_agreement_a1`) can be replaced with Contract #1 keys (`vtx.identity.<NanoID>`) — strongly prefer doing so to remove latent test-only divergence.
- The simple engine's two call sites get the same treatment. Even though no production code reaches them today (after C1 convergence, even fewer will), removing the latent shape mismatch closes the gap.
- The `EdgeEntry.OtherNodeID` field is already a bare NodeID by Materializer convention — but verify the executor's pattern-matching logic reads it consistently (e.g., joins with the target node's Core KV record correctly).

**Why this matters for 3.2a:** the live pipeline produces Contract #1 keys. Without this fix, the moment Refractor activates the Capability Lens and a real identity mutation lands, the full engine's traversal fails.

### 2. C1 convergence finish

3.1b-ii left a TODO at `internal/refractor/pipeline/pipeline.go:643` (and a sibling site at line 1089):

```go
// TODO(3.2 or later): C1 convergence — route through ruleengine.RuleEngine.
// ... semantics that the engine-neutral RuleEngine.Execute (single
// ProjectionResult) doesn't carry ...
results, err := simple.Evaluate(ctx, p.currentPlan(), entry, p.adjKV, p.coreKV)
```

The blocker noted was: `simple.Evaluate` returns `[]EvalResult + Delete` semantics that the engine-neutral `RuleEngine.Execute` (single `ProjectionResult`) doesn't carry; KV handles aren't in the engine-neutral signature either.

**Your call.** Two paths:
- **(a) Reshape `RuleEngine.Execute`** to carry KV handles and return a richer result that can express both engines' output. Touches `ruleengine.go`, both engine adapters, and the pipeline. Cleaner long-term.
- **(b) Pipeline routes per-engine:** the pipeline holds a typed handle to the resolved engine (full or simple) and calls the engine-specific method (`full.Engine.ExecuteWith` or `simple.Evaluate`) directly. The `RuleEngine` interface stays for selection only. Pragmatic — minimizes interface churn.

**Recommendation: (b).** The engine-neutral `RuleEngine.Execute` was always going to be a leaky abstraction; the selection registry is the part of the interface that pulls its weight. Don't force a unifying signature that both engines have to bend to. The pipeline can switch on `lens.ResolvedEngine` (or a typed handle stored on the Lens) and call the right concrete method.

**Hard constraint:** if you pick (a) and find >5 packages need touching, fall back to (b). Don't burn budget on interface design.

### 3. EventContext.Parameters at the production caller

3.1b-ii populated `EventContext.Parameters` only in the test harness. In production:

- `$actorKey` — for the bootstrap CapabilityLens, this is the **anchor identity** being projected for. Since the Lens's cypher rule starts with `MATCH (i:Identity {key: $actorKey})`, the pipeline needs to bind `$actorKey` to the **vertex key being processed by the current CDC event** (for identity mutations) OR to the actor whose capability projection is affected by a downstream link/role mutation. For 3.2a, the simple case is: the CDC event is on `vtx.identity.<NanoID>`, so `$actorKey = key`. Cross-vertex fan-out (a role-permission link mutation affecting all actors with that role) is a 3.2b/Phase-2 concern — 3.2a only needs the direct case.
- `$now` — `time.Now().UTC()` at execution time, formatted RFC3339.
- `$projectedAt` — same value as `$now` (the time the projection is being written). Used in the Contract #6 envelope field.

If the cypher rule references a parameter not bound, the executor returns `MissingParameterError` (existing 3.1b-ii behavior).

### 4. Adapter document-envelope reshape (open carry from Story 2.1 Deliverable #12)

The current `NatsKVAdapter.Upsert` serializes the row map as JSON directly. Contract #6 §6.2 mandates a richer envelope:

```json
{
  "key": "cap.identity.<NanoID>",
  "actor": "vtx.identity.<NanoID>",
  "version": "1.0",
  "projectedAt": "...",
  "projectedFromRevisions": {...},
  "lanes": ["default"],
  "platformPermissions": [...],
  "serviceAccess": [...],
  "ephemeralGrants": [...],
  "roles": [...]
}
```

The cypher RETURN already produces `platformPermissions`, `serviceAccess`, `ephemeralGrants`, `roles`. The envelope additions (`key`, `actor`, `version`, `projectedAt`, `projectedFromRevisions`, `lanes`) need to be wrapped around the RETURN output before writing.

**Two implementation options:**
- **(a) Modify `NatsKVAdapter`:** add a config flag or constructor option `EnvelopeWrap: "capability"` that wraps the row in the Contract #6 envelope. Touches the adapter interface? No — `Upsert(keys, row)` signature stays; the adapter just produces a different on-wire shape.
- **(b) Wrap at the pipeline layer:** the pipeline (or a thin "Capability projection envelope" helper between the executor and the adapter) builds the envelope and passes a single map to `Upsert`. The adapter stays generic.

**Recommendation: (b).** The Capability KV envelope is Lens-target-specific (NATS-streams adapters would have their own envelope; Postgres has its own row shape). Pushing envelope shape into the adapter couples it to one target's schema. A pipeline-layer wrapper keeps the adapter dumb.

`projectedFromRevisions` — for 3.2a, populate at minimum with the anchor vertex's revision (read from the CDC event metadata or re-read with `KeyValue.Get`). Fuller revision tracking (every source vertex referenced by the rule's traversal) is 3.2b/Phase-2; document the partial coverage in a closing-summary note.

`lanes` — Phase 1 default is `["default"]`. Don't try to derive from graph; hardcode for 3.2a.

`version` — `"1.0"` per Contract #6 §6.3.

## Architectural Decisions Already Made (Winston)

1. **Adjacency NodeID fix is at the engine call sites, NOT at the validator.** Do not relax `subjects.AdjKey`. Use `substrate.ParseVertexKey` at the two call sites.

2. **C1 convergence: prefer per-engine routing (option b above).** If you find a clean way to reshape `RuleEngine.Execute` in <5 package touches, that's fine — otherwise route per-engine and let `RuleEngine` stay a selection-only interface.

3. **Capability envelope built at the pipeline layer, not in the adapter.** Adapter stays generic.

4. **`$actorKey` binding for 3.2a:** bind to the CDC event's anchor vertex key for the direct case (identity mutation → re-project that identity). Cross-vertex fan-out is 3.2b/Phase-2 scope. If the bootstrap cypher rule expects `$actorKey` and the current event is a non-identity vertex, the pipeline can either skip (acceptable for 3.2a) or compute the affected actor set (3.2b).

5. **Capability Lens activation path:** rely on the existing `CoreKVSource` watch — both seeded lenses appear on `vtx.meta.>` as `class: meta.lens` records and activate automatically. Do NOT add a new bootstrap-trigger code path. If the CoreKVSource doesn't already route based on the `engine` field of the lens spec to select the `full` engine, add that routing (the registry's `SelectForLens` is the right entry).

6. **Test fixture identity:** for the one-identity e2e, create a fixture identity NanoID in test code (NOT seeded in primordial). Use direct Core KV writes to seed the identity vertex + role link + service availability topology. The test must NOT go through the Processor — that's Story 3.3's scope.

7. **`projectedFromRevisions` partial coverage acceptable.** Anchor vertex revision + Lens definition vertex revision are the minimum. Note partial coverage in closing summary.

8. **`refractor_e2e_test.go` p99 measurement:** if you can keep it running unchanged and it still meets NFR-P3, fine. If your wiring breaks its synthetic-key path, fix the test to use Contract #1 keys (which is the right direction anyway) and verify p99 is still well under 500ms.

9. **Bootstrap regression gate:** `make verify-bootstrap` must stay green. If your changes affect bootstrap output shape, update assertions in `scripts/verify-bootstrap.go`.

10. **Bypass regression gate:** `make test-bypass` must stay 4/4 BLOCKED.

11. **CoreKVSource activation race:** when Refractor starts, both `capability` and `capabilityRoleIndex` lenses arrive on the watch. Ensure activation doesn't deadlock or starve (sequential activation is fine for 3.2a; parallel optimization is 3.2b/Phase-2).

12. **If the secondary `capabilityRoleIndex` Lens fails to activate** because its cypher rule body uses unsupported features (verify against the full engine's parse-test coverage), that's an escalation. Don't try to fix the cypher body in 3.2a — flag and escalate.

13. **No new CONTRACT-AMENDMENT-REQUEST expected.** If one emerges, append and escalate.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.1b-ii-handoff-brief.md` | Predecessor brief — full engine state at session start |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 §6.1, §6.2, §6.3, §6.13 | Capability KV envelope shape + bucket key pattern |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.5 (key shapes) | NodeID extraction reference |
| `_bmad-output/planning-artifacts/epics.md` Story 3.2 (only) | AC #1, #5 (activation + Health KV reflection + adapter wiring) |
| `internal/bootstrap/lenses.go` | Both seeded Lens definitions — their cypher rule bodies are your acceptance oracle |
| `internal/bootstrap/primordial.go` | `capability-kv` bucket provisioning + 32-assertion verify-bootstrap |
| `internal/refractor/ruleengine/full/executor.go` | Adjacency NodeID call site (line 528) |
| `internal/refractor/ruleengine/full/executor_test.go` | The test workaround you're removing |
| `internal/refractor/ruleengine/simple/evaluator.go` | Adjacency NodeID sister call sites (lines 108, 203) |
| `internal/refractor/ruleengine/ruleengine.go` | `RuleEngine` interface + `EventContext.Parameters` (3.1b-ii additions) |
| `internal/refractor/pipeline/pipeline.go` | C1 TODO sites (lines 643, 1089) + envelope wrapping site |
| `internal/refractor/lens/corekv_source.go` | Lens activation watch — verify it routes via SelectForLens |
| `internal/refractor/lens/schema.go` | Lens parse path (3.1a `ResolvedEngine` field) |
| `internal/refractor/adapter/natskv.go` | Today's row-only Upsert; verify the envelope wrapping site is upstream of it |
| `cmd/refractor/main.go` | Activation entry — verify both seeded lenses route through `CoreKVSource` |
| `internal/substrate/keys.go` (`ParseVertexKey`) | NodeID extraction helper |

**DO NOT read** the full `_bmad-output/planning-artifacts/lattice-architecture.md`, the Materializer source, full epics.md, or the cypher_parser.go/cypher_listener.go (vendored ANTLR — 3.1b-i handled those).

## Suggested Sequence

**Phase A — Adjacency NodeID fix (target ~15K tokens):**
1. Update `internal/refractor/ruleengine/full/executor.go` line 528 (and any other Build/Neighbors call sites you find in that package): extract NodeID via `substrate.ParseVertexKey` before passing to adjacency functions.
2. Update `internal/refractor/ruleengine/simple/evaluator.go` lines 108, 203 with the same pattern.
3. Update `internal/refractor/ruleengine/full/executor_test.go` fixtures: replace synthetic keys (`"alice"`, etc.) with Contract #1 keys (`vtx.identity.<NanoID>`). The tests should still pass — the NodeID extraction at the call site makes them equivalent.
4. Run `go test ./internal/refractor/ruleengine/... -count=1` — all tests pass.

**Phase B — C1 convergence + EventContext.Parameters wiring (target ~30K tokens):**
5. Decide per-engine routing (b) vs. interface reshape (a). Document choice in the closing summary.
6. Pipeline routes through resolved engine: if `lens.ResolvedEngine == "full"`, call `full.Engine.ExecuteWith(...)`; else `simple.Evaluate(...)`. The pipeline holds typed handles to both (or a switch).
7. Populate `EventContext.Parameters` at the call site:
   - `$actorKey`: the anchor vertex key from the CDC event.
   - `$now`: `time.Now().UTC().Format(time.RFC3339)`.
   - `$projectedAt`: same as `$now`.
8. Remove the TODO comments at lines 643 and 1089 if convergence lands; convert to a permanent comment + rationale if partial.
9. Test: existing pipeline tests pass; if you added or changed test fixtures to exercise the full engine path, document.

**Phase C — Capability KV envelope wrapping (target ~20K tokens):**
10. Add a pipeline-layer helper (or inline at the projection-write site) that builds the Contract #6 §6.2 envelope around the executor's RETURN output. Fields:
    - `key`: derived from `cap.<actor-vertex-suffix>` (strip `vtx.` prefix from `$actorKey`).
    - `actor`: full vertex key (`$actorKey` value).
    - `version`: `"1.0"`.
    - `projectedAt`: `$now`.
    - `projectedFromRevisions`: at minimum `{actorKey: <revision>, lensDefKey: <revision>}`.
    - `lanes`: `["default"]`.
    - Three sections + `roles`: from the executor's RETURN.
11. The envelope is the value `NatsKVAdapter.Upsert` writes — wrap before calling Upsert. The adapter interface stays unchanged.

**Phase D — Live activation + one-identity e2e (target ~35K tokens):**
12. Verify `CoreKVSource` routes Lens spec parsing through `ruleengine.SelectForLens` so the `engine: full` field on the spec lands the full engine. If the seeded Lens specs don't include `engine: full`, either add the field at the bootstrap layer (preferred — `internal/bootstrap/lenses.go`) or ensure the SelectForLens fallback chain picks full when simple fails (already implemented in 3.1a).
13. Author `internal/refractor/refractor_capability_e2e_test.go` (NEW): with embedded NATS + JetStream + capability-kv bucket provisioned via bootstrap helpers, start Refractor's pipeline, write the seeded primordial state (or call `bootstrap.SeedPrimordial` if accessible), write one additional identity + role link + service availability topology to Core KV (synthetic NanoIDs are fine; use Contract #1 shape), wait for the Capability Lens to project, assert:
    - `capability-kv` bucket contains an entry at `cap.identity.<NanoID>`.
    - The entry's body parses as Contract #6 §6.2 envelope.
    - Three sections (`platformPermissions`, `serviceAccess`, `ephemeralGrants`) and `roles` are present (may be empty arrays for sections the fixture doesn't exercise).
    - `version == "1.0"`, `actor == "vtx.identity.<NanoID>"`.
14. Verify Health KV reports `active` for both Capability Lenses within 10s.
15. Run `make verify-bootstrap`, `make test-bypass`, `go test ./... -p 1 -count=1` — all green.

**Phase E — Gates + closing (target ~20K tokens):**
16. Update token tracker Row 3.2a with outer-telemetry actual.
17. Closing summary: convergence approach taken (a vs b), envelope wrapping location, e2e shape, residual risks for 3.2b.

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

1. ✅ Adjacency NodeID extraction at `internal/refractor/ruleengine/full/executor.go` (formerly line 528)
2. ✅ Adjacency NodeID extraction at `internal/refractor/ruleengine/simple/evaluator.go` (formerly lines 108, 203)
3. ✅ Full-engine executor tests migrated to Contract #1 keys (no synthetic `"alice"` / `node_*` fixtures)
4. ✅ Pipeline routes through resolved engine (per-engine routing OR interface reshape — document choice)
5. ✅ `EventContext.Parameters` populated at production caller with `$actorKey`, `$now`, `$projectedAt`
6. ✅ TODO comments at `pipeline.go:643, 1089` removed (or replaced with permanent rationale)
7. ✅ Capability KV envelope wrapping (Contract #6 §6.2 shape: `key, actor, version, projectedAt, projectedFromRevisions, lanes, platformPermissions, serviceAccess, ephemeralGrants, roles`) — built at pipeline layer, not in adapter
8. ✅ `projectedFromRevisions` includes at least the anchor vertex + Lens definition vertex revisions (partial coverage documented)
9. ✅ `lanes: ["default"]`, `version: "1.0"` populated correctly
10. ✅ Both seeded Capability Lenses activate via CoreKVSource at refractor startup (verified by Health KV `active` within 10s)
11. ✅ Lens spec parsing selects `full` engine for the capability lenses (verify `engine: full` lands either via bootstrap spec field or selection fallback)
12. ✅ `internal/refractor/refractor_capability_e2e_test.go` — one-identity live e2e demonstrating `cap.<actor-suffix>` entry appears with Contract #6 §6.2 shape
13. ✅ `make verify-bootstrap` regression PASS (assertion count unchanged or increased; current is 32 OK)
14. ✅ `make test-bypass` regression PASS (4/4 BLOCKED)
15. ✅ `go build ./...`, `make vet`, `go test ./... -p 1 -count=1` all green
16. ✅ Token tracker Row 3.2a updated with outer-telemetry actual
17. ✅ Closing summary including: convergence approach (a vs b + rationale), envelope wrapping location, fixture identity & e2e shape, residual risks/handoffs for 3.2b

## What 3.2a Is NOT

- **Not** the multi-identity (3 identities) e2e from Story 3.2 AC — that's 3.2b
- **Not** the contract-conformance byte test (`capability_lens_contract_test.go`) — that's 3.2b
- **Not** tombstone re-projection semantics — that's 3.2b
- **Not** `health.refractor.<instance>.lens.capability.*` latency emission — that's 3.2b
- **Not** Processor step 3 auth (Story 3.3)
- **Not** denial-response shaping (Story 3.4)
- **Not** Capability Lens adversarial suite (Story 3.7)

## Escalation

Halt and escalate via Andrew/Winston if:
- A required wiring needs a contract change (CONTRACT-AMENDMENT-REQUEST)
- Adjacency NodeID fix requires >5 file touches outside the two engine call sites
- C1 convergence requires reshaping `RuleEngine.Execute` in a way that touches >5 packages (fall back to per-engine routing instead)
- Envelope reshape requires changing the `adapter.Adapter` interface (forbidden — wrap at pipeline layer)
- The secondary `capabilityRoleIndex` Lens fails to activate because its cypher uses unsupported features (don't fix in 3.2a)
- One-identity e2e fails for a reason you can't reduce after two debug attempts
- You hit a stuck-loop pattern per the operating rules

## Closing

1. Verify all 17 deliverables
2. Run all required gates
3. Update token tracker Row 3.2a with outer-telemetry actual
4. Closing summary as Deliverable #17

Do NOT commit. Winston + Andrew review and commit.
