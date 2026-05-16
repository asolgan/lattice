---
title: Story 3.2b Implementation Handoff Brief
story: 3.2b — Capability Lens AC Closure (multi-identity, link bridge, capabilityRoleIndex, tombstone, health emission, contract conformance)
model_tier: Opus (locked)
token_budget: ~150K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-15
predecessor: Story 3.2a (single-identity live activation, shipped at c7bf9c4)
---

# Story 3.2b — Capability Lens AC Closure: Handoff Brief

## Your Role

You close Story 3.2. 3.2a landed single-identity live activation: the primary `CapabilityLens` is wired through the real pipeline, its projection lands at `cap.identity.<NanoID>` in Capability KV with the Contract #6 §6.2 envelope shape, p99=9.6ms on the one-identity e2e. Your job is to satisfy the rest of Story 3.2's Acceptance Criteria: bridge Contract #1 link envelopes into the adjacency bootstrapper (so live `make up` has populated adjacency, not just synthetic test data), activate the secondary `capabilityRoleIndex` lens for real (currently NullKeySkipper-absorbed), implement cross-vertex fan-out (events on non-identity vertices currently drop), demonstrate a multi-identity end-to-end test, add tombstone re-projection semantics, emit NFR-P3 latency to Health KV, and ship the byte-shape contract-conformance test that catches Capability KV schema drift.

After 3.2b, Story 3.3 (Processor step 3 — real Capability KV authorization) is unblocked.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **Token budget is for tracking only, NOT a halt threshold.** Original estimate ~150K. Record actual outer-telemetry consumption in the tracker at session close. Do NOT stop work based on token count.

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
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.5 + Contract #5 (Health KV) + Contract #6 + `_bmad-output/planning-artifacts/epics.md` Story 3.2 are source of truth.
- **DO NOT silently edit planning artifacts.** If a contract gap appears, append to `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` and escalate.
- **Token tracker:** update Row 3.2b at session close with outer-telemetry actual.
- **Permission to split:** if you find the scope is more than one session can deliver well, declare a 3.2b-i / 3.2b-ii split mid-session, document the split rationale, and ship 3.2b-i with explicit residuals for 3.2b-ii. The split decision is yours. Precedent: 3.1 → 3.1a + 3.1b-i + 3.1b-ii.

## What's Already in Place (do NOT redo)

- **Story 3.2a (c7bf9c4):**
  - Adjacency NodeID-extraction fix at both engine call sites (full + simple).
  - C1 convergence: per-engine routing in pipeline; `engineKind`/`fullEngine`/`fullCR` fields; `UseFullEngine`/`SetEnvelopeFn`; `pipeline/evaluate.go` normalises both engines' outputs into `[]simple.EvalResult`.
  - `EventContext.Parameters` wired at production caller: `$actorKey` = CDC event vertex key, `$now`/`$projectedAt` = `time.Now().UTC().Format(time.RFC3339)`.
  - Contract #6 §6.2 envelope wrapper in new `internal/refractor/capabilityenv` package; built at pipeline layer (NOT in adapter). `cap.<actor-suffix>` key derivation, `version="1.0"`, `lanes=["default"]`, `projectedFromRevisions` populated with anchor + lens-def revisions (partial coverage acceptable per 3.2a Decision #7).
  - Primordial seeds a 5th `spec` aspect per Lens with `engine:"full"`; CoreKVSource extracts via `unwrapSpecBody`; `verify-bootstrap` count is now 34.
  - One-identity e2e (`internal/refractor/refractor_capability_e2e_test.go`) passes; p99=9.6ms.
  - `capabilityRoleIndex` lens activates but its rows are absorbed by a `NullKeySkipper` envelope — actual per-operationType projection does NOT land yet.
  - `cmd/refractor/main.go`'s `updateCB` (MatchChange hot-reload) still uses the simple-only path. Untouched by 3.2a.

- **Story 3.1b-ii (ce13ef2):** full engine executor; bootstrap CapabilityLens e2e p99=11.7ms with synthetic keys; `EventContext.Parameters` + `MissingParameterError`.

Tree is clean at session start (`go build`, `make vet`, `make verify-bootstrap`, `make test-bypass`, full test suite all green at c7bf9c4).

## Story Scope (3.2b)

**In scope (must close all of these to declare Story 3.2 done):**

1. **Contract #1 link envelopes → adjacency bootstrapper bridge.** The adjacency `Bootstrapper.processMsg` today (at `internal/refractor/consumer/bootstrap.go:142-157`) unmarshals into `adjacency.CoreKVEvent` and skips anything where `evt.NodeID == ""`. Contract #1 link envelopes (key shape `lnk.<srcType>.<srcId>.<linkName>.<dstType>.<dstId>` per §1.5) don't carry that field, so they're silently skipped. Fix: detect link envelopes by key shape (`substrate.ClassifyKey` returns `KindLink`), parse the 6-segment key, and emit TWO `adjacency.CoreKVEvent`s into `adjacency.Build` — one outbound from src, one inbound from dst. After the fix, `make up` should populate `refractor-adjacency` from the seeded primordial role/permission/service topology links.

2. **`capabilityRoleIndex` Lens full activation.** Today the secondary Lens activates but the envelope absorbs all rows via `NullKeySkipper` because the cypher RETURN produces null `operationType` for events on non-role-permission topology. After link-bridge (#1) and cross-vertex fan-out (#3), this Lens should write `cap.role-by-operation.<operationType>` entries containing `{"roles": [...]}` per Contract #6 §6.1. The envelope shape for this Lens is **different** from the primary Lens — it's a flat `{key, projectedAt, roles}` object, not the §6.2 three-section envelope. Add a per-Lens envelope strategy (the `capabilityenv` package needs a second envelope-builder selected by Lens canonical name OR a more general per-Lens envelope-spec mechanism — your call on the cleanest approach).

3. **Cross-vertex fan-out for non-identity CDC events.** Today the envelope wrapper drops rows where `row.actorKey == null` OR the actor's vtx-type is not `identity`. Real role/permission/service mutations affect every actor who currently holds the affected role / has access to the affected service. The pipeline needs to reverse-traverse from the mutated vertex to the set of affected actors, then re-project each. Approach: on a non-identity CDC event, the pipeline consults adjacency (now populated by #1) to find all identities reachable from the mutated vertex via the topology relations the Capability Lens cares about; for each, re-execute the cypher rule with `$actorKey = <that identity key>` and write/update the projection. Implementation detail: this could live in the pipeline's event-routing step (before invoking the engine), or as a dedicated "affected actor enumerator" called once per CDC event. Pick the cleaner location.

   **Scope guard:** depth-bounded enumeration. Use the same max-depth cap as the executor's variable-length traversal (default 10). If the enumeration would touch more actors than a configurable cap (default 10,000), log a warning and proceed — don't error. The architectural gap analysis §2.4 / R2 explicitly flags Capability KV fan-out latency as unmeasured under load; 3.2b will produce the first real measurement.

4. **Multi-identity (3-actor) e2e** per Story 3.2 AC. Build on `refractor_capability_e2e_test.go` (or a new sibling test): seed THREE distinct identities:
   - Identity A: platform admin (role with a platformPermissions grant).
   - Identity B: regular user with a role granting service access via location topology (containedIn → location → availableAt → service).
   - Identity C: user with an `assignedTo` task that produces an `ephemeralGrants[]` entry (FR56 task-derived grant).

   Assert each identity's `cap.identity.<NanoID>` entry has the expected three-section content. Also assert at least one `cap.role-by-operation.<operationType>` entry contains the expected role list.

5. **Tombstone re-projection semantics.** Per Story 3.2 AC: *"a tombstone mutation lands on an identity, role assignment, or task assignment ... Refractor processes the CDC event ... affected Capability KV entries are recomputed with soft-delete semantics — tombstoned vertices/links filtered out of the cypher result; entries themselves are rewritten with the recomputed permission set (not deleted, unless the identity itself is tombstoned)."* The executor already filters soft-deleted reads (per 3.1b-ii). Your job: ensure the pipeline observes tombstone events on Core KV and triggers re-projection. If the actor itself is tombstoned, delete the `cap.<actor-suffix>` entry. If only an assignment edge is tombstoned, recompute and rewrite. Add a test in the multi-identity e2e (or a focused tombstone test): tombstone Identity B's role-assignment link → assert their `serviceAccess[]` shrinks (or empties).

6. **NFR-P3 latency emission to Health KV.** Per Story 3.2 AC: *"Capability Lens projection latency (per-event mean, p95, p99) is emitted to Health KV under `health.refractor.<instance>.lens.capability.*` and `health.refractor.<instance>.lens.capabilityRoleIndex.*`."* Refractor's heartbeat already publishes `health.refractor.<instance>` (Contract #5 §5.2 / `internal/refractor/health/lattice_heartbeater.go`). Extend the heartbeat — or add a sibling emitter — that publishes per-Lens latency: a sliding-window or per-heartbeat-interval recomputed mean/p95/p99 over CDC-event → projection-write latency for each Lens. Persist the per-event timings in a small ring buffer per Lens, summarised at heartbeat tick. Format per Contract #5; the subject pattern `health.refractor.<instance>.lens.<canonicalName>.*` is suggested by the Story 3.2 AC; finalize the exact key shape consistent with §5.2.

7. **Contract-conformance byte-shape test** at `internal/refractor/ruleengine/full/capability_lens_contract_test.go` (NEW). Per Contract #6 §6.13: *"Story 3.2's contract-conformance test runs the bootstrap cypher query against a deterministically seeded graph and asserts the output structure matches the shape below. This test catches schema drift if anyone modifies the Capability Lens cypher query without updating this contract (or vice versa)."* Seed a deterministic graph; run the LITERAL bootstrap `CapabilityLensDefinition().CypherRule` (do NOT hand-copy a simplified version — same Decision as 3.1b-ii); assert the projected document's structure matches Contract #6 §6.2 byte-for-byte modulo timestamps and revision numbers. Use field-by-field comparison (not raw byte diff) so a sane error message surfaces on drift.

8. **MatchChange hot-reload routes per-engine.** `cmd/refractor/main.go`'s `updateCB` still calls `simple.Parse` + `simple.Compile` directly. Mirror `startPipeline`'s per-engine routing (set `engineKind`, populate `fullEngine`/`fullCR` for the full-engine case, install the right envelope). Add a focused test if straightforward; otherwise document.

**Out of scope:**
- Processor step 3 auth (Story 3.3).
- Denial-response shaping (Story 3.4).
- Three-plane auth failure traceability (Story 3.5).
- Capability Lens adversarial suite (Story 3.7).
- `projectedFromRevisions` full coverage (every source vertex referenced by traversal). Partial coverage from 3.2a (anchor + lens-def) is acceptable; add additional vertices as they fall naturally out of the cross-vertex fan-out plumbing, but don't engineer a full source-tracking pass.
- Cross-cell/multi-cell scale-out.
- Personal Lens / Secure Lens / NATS-streams adapter.
- Generalized incremental projection (full-recompute-per-affected-actor is acceptable per Story 3.2 AC).

## Architectural Decisions Already Made (Winston)

1. **Link bridge is a translator at the bootstrapper, not a new bootstrapper-input format.** The `adjacency.CoreKVEvent` shape stays as-is (Materializer-compatible). Add a translator in `internal/refractor/consumer/bootstrap.go` (or a new helper in the same package) that detects Contract #1 link envelopes by key shape, parses the 6-segment key, reads the value-envelope's `isDeleted` field, and emits the two directional events. The translator is the only new code; `adjacency.Build` stays unchanged.

2. **Per-Lens envelope strategy.** The `capabilityenv` package should expose two envelope functions: one for the per-actor primary Lens (existing 3.2a shape), one for the role-coverage secondary Lens (`{key, projectedAt, roles}`). Selection by canonical name is fine — the seeded Lens specs carry `canonicalName`, and `cmd/refractor/main.go` already routes by canonical name when wiring envelopes. If you find a cleaner abstraction (e.g., the envelope is part of the Lens spec itself), feel free, but don't over-engineer for hypothetical future Lenses.

3. **Cross-vertex fan-out enumerator lives in the pipeline, not the engine.** The engine stays a pure function (graph → projection) per the original architectural seam. The pipeline's job is to translate "this CDC event affected vertex X" into "re-project actor set {A1, A2, ...}" and call the engine N times. Implementation can be:
   - A dedicated `actor_enumerator.go` file in the pipeline package, OR
   - Inlined at the event-routing site if it's <60 lines.
   - Either is fine; pick whichever is more readable.

4. **Tombstone semantics: simple route.** On CDC event with `isDeleted: true` on an actor vertex → delete the `cap.<actor-suffix>` entry. On `isDeleted: true` on a non-actor vertex (role link, task assignment, service link, etc.) → fan out per #3 and re-project each affected actor (the cypher rule's soft-delete filter naturally removes the tombstoned topology from the result). No special-casing of partial deletes.

5. **Latency emission: per-heartbeat aggregation.** Each Lens pipeline maintains a small ring buffer (size 128, configurable) of recent per-event projection latencies. At heartbeat tick (10s NFR-O1), compute mean/p95/p99 over the buffer and publish to `health.refractor.<instance>.lens.<canonicalName>.{mean,p95,p99}` (or a single map document — your call). Reset or roll the buffer per tick. Don't emit per-event — that's too noisy.

6. **Contract-conformance test depth.** Field-by-field comparison with descriptive failure messages — NOT raw byte diff. Test must use the LITERAL `CapabilityLensDefinition().CypherRule`. If the literal doesn't produce something Contract #6 §6.2-shaped against the seeded graph, the test is the safety net that catches the drift — your test FAILING is information, not a test bug.

7. **Multi-identity e2e fixture:** seed deterministically (fixed NanoIDs in test code, like 3.2a's pattern). The three identities should exercise: platform permission (admin), location-derived service access (regular user), task-derived ephemeral grant (user with assigned task). All three actor projections + at least one `cap.role-by-operation.*` entry should be asserted.

8. **Hot-reload route (`updateCB`):** mirror `startPipeline`'s pattern. Don't engineer a shared helper unless the duplication is more than 30 lines.

9. **`make verify-bootstrap` and `make test-bypass` regression gates.** Both must stay green. Update assertion count in `scripts/verify-bootstrap.go` if you add any new primordial seeds.

10. **Capability KV bucket sole writer.** Per Contract #6 §6.1: Refractor is the sole writer; Processor reads only. Don't add any Capability KV writes outside the Refractor pipeline projection path.

11. **`projectedFromRevisions` improvements are opportunistic.** If cross-vertex fan-out plumbing naturally surfaces additional source vertices, add them to the map. Don't engineer a full source-tracking pass for 3.2b.

12. **CI gate:** `.github/workflows/ci.yml` is active. After your changes, CI must go green. CI flake pattern (JetStream redelivery + tracker dedup on GitHub Actions runners) is documented — if a NFR-R1 fault test times out in CI specifically, bump the timeout; if any other test flakes, root-cause it.

13. **Andrew has authorized autonomous proceed.** No mid-session approvals required unless you hit an architectural gap you cannot resolve. Halt + escalate criteria are documented in MANDATORY OPERATING RULES.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.2a-handoff-brief.md` | Predecessor brief — what 3.2a shipped + the carries you're closing |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.5 (key shapes) | Link envelope shape `lnk.<srcType>.<srcId>.<linkName>.<dstType>.<dstId>` |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #5 §5.2 (Refractor heartbeat shape) | Health KV emission for latency |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 (full) | Capability KV envelope shape (§6.2) + secondary key pattern (§6.1) + cypher rule required behaviors (§6.10) + worked example (§6.12) + implementation notes (§6.13) |
| `_bmad-output/planning-artifacts/epics.md` Story 3.2 (only) | AC #1 through #8 — your closure target |
| `_bmad-output/planning-artifacts/refractor-gap-analysis.md` §2.4 + R2 | NFR-P3 fan-out caveat — your measurement is the first real evidence |
| `internal/refractor/consumer/bootstrap.go` | Today's `processMsg` (skips link envelopes); your bridge target |
| `internal/refractor/adjacency/builder.go` | `CoreKVEvent` shape; `Build` semantics |
| `internal/refractor/capabilityenv/envelope.go` | 3.2a primary-Lens envelope; add the secondary alongside |
| `internal/refractor/pipeline/pipeline.go` + `pipeline/evaluate.go` | 3.2a per-engine routing; cross-vertex fan-out enumerator lands near here |
| `internal/refractor/refractor_capability_e2e_test.go` | 3.2a one-identity baseline; multi-identity extension builds on this |
| `cmd/refractor/main.go` | `startPipeline` (per-engine routed) + `updateCB` (simple-only — your target) |
| `internal/refractor/health/lattice_heartbeater.go` | Refractor heartbeat — latency emission lives here or sibling |
| `internal/refractor/ruleengine/full/bootstrap_e2e_test.go` | 3.1b-ii bootstrap e2e — pattern for contract-conformance test |
| `internal/bootstrap/lenses.go` | Both seeded Lens definitions (`CapabilityLensDefinition` + `CapabilityRoleIndexLensDefinition`) — the literal cypher bodies |
| `internal/substrate/keys.go` (`ClassifyKey`, `ParseLinkKey` if it exists; otherwise add) | Key parsing helpers — verify a `ParseLinkKey` exists; add if missing |

**DO NOT read** the full `lattice-architecture.md`, full epics.md, Materializer source, vendored ANTLR parser, or 3.1a/3.1b-i briefs (3.2a brief contains everything you need from those).

## Suggested Sequence

**Phase A — Link bridge (target ~20K tokens):**
1. Verify `substrate.ClassifyKey` returns `KindLink` for 6-segment `lnk.*` keys; check for an existing `ParseLinkKey` or add one returning `(srcType, srcID, linkName, dstType, dstID, ok)`.
2. Update `internal/refractor/consumer/bootstrap.go::processMsg`: when `ClassifyKey == KindLink`, parse the key, read the value envelope's `isDeleted`, emit two `adjacency.CoreKVEvent`s into `adjacency.Build` (one with NodeID=srcID/Direction=outbound/OtherNodeID=dstID/OtherType=dstType, one with NodeID=dstID/Direction=inbound/OtherNodeID=srcID/OtherType=srcType). The `EdgeID` can be the link's full key (it's unique).
3. Add a focused test in `internal/refractor/consumer/bootstrap_test.go` (or new): publish a synthetic Contract #1 link envelope to Core KV → verify both adjacency entries appear.
4. Verify `make up` end-to-end (or a corresponding test fixture) populates `refractor-adjacency` from primordial role/permission/service topology links. The current 3.2a e2e calls `adjacency.Build` directly; after this fix the seeded primordial topology should reach adjacency automatically.

**Phase B — capabilityRoleIndex envelope + cross-vertex fan-out (target ~30K tokens):**
5. Add a second envelope builder in `capabilityenv` for the role-coverage secondary Lens. Shape: `{key: "cap.role-by-operation.<operationType>", projectedAt, roles: [...]}`. Select per-Lens by canonical name in `cmd/refractor/main.go`.
6. Replace the `NullKeySkipper` shim in 3.2a for `capabilityRoleIndex` with the real envelope.
7. Implement cross-vertex fan-out enumerator: on a non-identity CDC event, enumerate affected actors via adjacency traversal from the mutated vertex; re-execute the cypher rule per actor; write per-actor projection. Bound the enumeration with the max-depth cap (10) and the actor-set cap (10,000 — log warning, proceed).
8. Test: synthetic mutation on a role-permission link → assert all actors holding that role get their `cap.<actor>` re-projected.

**Phase C — Multi-identity e2e + tombstone (target ~25K tokens):**
9. Author `internal/refractor/refractor_capability_multi_e2e_test.go` (or extend the 3.2a test): seed three identities (admin / location-user / task-grantee), seed full topology, activate both Lenses, assert all three `cap.<actor>` entries plus at least one `cap.role-by-operation.*` entry.
10. Add a tombstone sub-test: tombstone an identity → its `cap.<actor>` entry deleted. Tombstone a role link → affected actor's projection re-projected (without that role's permissions/services).

**Phase D — NFR-P3 health emission (target ~20K tokens):**
11. Add per-Lens ring buffer in the pipeline (size 128, configurable). Capture per-event latency (CDC arrival → projection write).
12. Extend `lattice_heartbeater.go` (or sibling) to publish `health.refractor.<instance>.lens.<canonicalName>.{mean,p95,p99}` (or a single map document under `health.refractor.<instance>.lens.<canonicalName>`) at heartbeat tick.
13. Test: drive N projection events, verify Health KV emission appears with non-zero numbers and consistent shape per Contract #5.

**Phase E — Contract-conformance byte-shape test + hot-reload (target ~25K tokens):**
14. Author `internal/refractor/ruleengine/full/capability_lens_contract_test.go`: seed deterministic graph, run literal `CapabilityLensDefinition().CypherRule`, field-by-field assert against Contract #6 §6.2 shape with descriptive failure messages.
15. Update `cmd/refractor/main.go::updateCB` to mirror `startPipeline`'s per-engine routing.

**Phase F — Gates + closing (target ~15K tokens):**
16. Run all required gates per "Required Verification".
17. Update token tracker Row 3.2b with outer-telemetry actual.
18. Closing summary as Deliverable #18.

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

1. ✅ Contract #1 link envelope translator in `internal/refractor/consumer/bootstrap.go`; both directional `adjacency.CoreKVEvent`s emitted per link
2. ✅ Focused test in `consumer/` verifying link envelope → both adjacency entries
3. ✅ Live `make up` (or equivalent fixture) populates `refractor-adjacency` from primordial topology automatically (3.2a e2e no longer needs the `adjacency.Build` workaround — though leaving it is fine)
4. ✅ Second envelope builder in `capabilityenv` for `capabilityRoleIndex` shape `{key, projectedAt, roles}`
5. ✅ `NullKeySkipper` shim replaced by real envelope for `capabilityRoleIndex`; per-operationType `cap.role-by-operation.<X>` entries land in Capability KV
6. ✅ Cross-vertex fan-out enumerator in pipeline: non-identity CDC events drive per-affected-actor re-projection; depth bound 10; actor-set warning cap 10,000
7. ✅ Multi-identity e2e (3 actors: admin / location-user / task-grantee) — `cap.<actor>` entries verified for all three; `cap.role-by-operation.*` entries verified for at least one operation type
8. ✅ Tombstone semantics: identity tombstone → entry deleted; topology-edge tombstone → affected actors re-projected with shrunk capability set; test asserts both
9. ✅ Per-Lens latency ring buffer (size 128) captures CDC→projection-write latency
10. ✅ Heartbeat publishes `health.refractor.<instance>.lens.<canonicalName>.{mean,p95,p99}` per Contract #5 §5.2 conventions; test asserts emission shape
11. ✅ `internal/refractor/ruleengine/full/capability_lens_contract_test.go` — field-by-field byte-shape assertions against Contract #6 §6.2 using literal `CapabilityLensDefinition().CypherRule`
12. ✅ `cmd/refractor/main.go::updateCB` mirrors `startPipeline`'s per-engine routing
13. ✅ All adapter envelope/wiring decisions documented in closing summary (per-Lens envelope strategy, fan-out enumerator location, etc.)
14. ✅ `go build ./...`, `make vet`, all required tests, `make verify-bootstrap`, `make test-bypass` green
15. ✅ NFR-P3 conformance evidence: multi-identity e2e latency mean/p95/p99 recorded — if p95 exceeds budget, gap-analysis §2.4 / R2 updated with specific bottleneck
16. ✅ No silent edits to data-contracts.md / lattice-architecture.md; CONTRACT-AMENDMENT-REQUEST appended only if a real contract gap surfaces
17. ✅ Token tracker Row 3.2b updated with outer-telemetry actual
18. ✅ Closing summary including: link bridge design, per-Lens envelope approach, fan-out enumerator location, multi-identity e2e shape, tombstone test coverage, latency numbers (NFR-P3 evidence under live fan-out — the first real measurement), residual carries for Story 3.3 and beyond

## What 3.2b Is NOT

- **Not** Processor step 3 auth (Story 3.3)
- **Not** denial-response shaping (Story 3.4)
- **Not** Capability Lens adversarial suite (Story 3.7)
- **Not** full `projectedFromRevisions` source tracking (opportunistic only)
- **Not** incremental projection optimization (full-recompute-per-affected-actor is acceptable per Story 3.2 AC)
- **Not** multi-cell / Personal Lens / Secure Lens / NATS-streams adapter

## Escalation

Halt and escalate via Andrew/Winston if:
- A required wiring needs a contract change (CONTRACT-AMENDMENT-REQUEST)
- Cross-vertex fan-out enumeration has a structural problem you can't resolve in <3 attempts (e.g., adjacency doesn't have the relations you need; or the enumeration cost is structurally too high for NFR-P3 even on the test fixture)
- Tombstone re-projection has a semantic ambiguity (e.g., tombstoning a vertex with cascading dependencies surfaces a contract question)
- Contract-conformance test fails with a structural shape mismatch (this is information, not a bug — escalate with the structural diff so Winston/Andrew can decide whether to amend the contract or fix the cypher)
- A CONTRACT-AMENDMENT-REQUEST emerges
- You hit a stuck-loop pattern per the operating rules

## Closing

1. Verify all 18 deliverables
2. Run all required gates
3. Update token tracker Row 3.2b with outer-telemetry actual
4. Closing summary as Deliverable #18 (link bridge + per-Lens envelope + fan-out + e2e + tombstone + latency + contract test — residuals for 3.3 and beyond)

Do NOT commit. Winston + Andrew review and commit.
