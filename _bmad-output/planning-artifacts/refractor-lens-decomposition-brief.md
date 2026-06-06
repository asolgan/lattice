# Architecture brief — Refractor per-actor lens projection: generalization, capability decomposition, and projection-plane integrity

- **Type:** Planning open-item brief, for a dedicated Winston architecture session. Nothing here is ratified — this is agenda + options, not a decision.
- **Date:** 2026-06-05
- **Deciders:** Andrew + Winston (architecture session, to be scheduled)
- **Extends:** the existing open item *"Capability Lens god-cypher → contract-contribution model"* in `lattice-architecture.md` (§ near line 1188). This brief keeps that item's two decisions and adds two more surfaced by the Story 7.2 review. The session may fold the conclusions back into `lattice-architecture.md` (planning lead's call).
- **Note on "ADR-NN":** Lattice does **not** maintain a numbered ADR series. The `ADR-51` references in the codebase (e.g. Contract #10, `weaver.md`) are to the **NATS.io JetStream "Scheduled Messages" ADR** — an external dependency's design record, not ours. Lattice tracks its own decisions as named open items in `lattice-architecture.md`.

---

## 1. Why this brief exists

Story 7.2 added a `myTasks` lens to `orchestration-base`. Both the Acceptance Auditor and Andrew independently flagged the same smell: a *package* lens cannot be added without editing **core** — a new `case` in `cmd/refractor/main.go` plus a new wrapper in `internal/refractor/capabilityenv/`. That contradicts the minimal-core / everything-is-a-package principle and the package-layering rule (a package may depend on core, never the reverse).

This brief consolidates four related decisions so they're taken together rather than piecemeal across stories. Two are already-tracked planning open items; two are new from the 7.2 review.

The crucial framing the existing open item does **not** capture: it assumes "each package projects the grant types it owns." The 7.2 review shows packages **currently can't** do that without a core code change. **Decision D-PIPELINE below is therefore a prerequisite** for the projection side of the existing decomposition — not an independent nicety.

---

## 2. Current state (what is and isn't data-driven)

The lens **definition** is already data, sourced from Core-KV and processed generically:

- `cmd/refractor/main.go` watches `vtx.meta.>` and builds a runtime Rule per lens definition.
- Each definition carries, as data: the cypher `Spec`, `engineKind`, target (`Into.Bucket` / `Into.Key` / `Into.Target` ∈ {nats_kv, postgres}), and delete mode.
- A **plain** lens (e.g. `identity-hygiene`'s `duplicateCandidates`) matches **no** `case` in the per-lens switch and runs the pure path: watch → cypher → write rows to its bucket. This is the "any other lens" path.

What is **not** data-driven — hardcoded in Go, keyed on `CanonicalName` — is the **per-actor projection plumbing**. Four lenses hit the switch (`capability`, `capabilityRoleIndex`, `capabilityEphemeral`, `myTasks`), each wiring behavior a plain lens lacks, via `pipeline` setters:

| Behavior | Setter | What it does | Why a cypher can't |
|---|---|---|---|
| **Envelope + output key** | `SetEnvelopeFn` | Reshapes each cypher row into a target-specific output schema (Contract #6 §6.2), derives the **output KV key** (`cap.identity.<id>`, `cap.ephemeral.<id>`, `my-tasks.identity.<id>`), drops non-matching rows, and stamps **`projectedFromRevisions`** freshness metadata (the Story 1.5.4 auth-coherence field). | Cypher can return `actorKey` and aggregate payloads, but target bucket key patterns, envelope fields, freshness metadata, and empty-aggregate behavior are projection-plane contract semantics. They should be declared in the lens contract, not inferred from a package name. |
| **Invalidation / fan-out** | `SetActorEnumerator` | These are per-actor aggregations anchored on `$actorKey`, but CDC events arrive on a role / permission / task / link — **not** the identity. The current enumerator walks adjacency broadly to answer "which actors are now stale?" so each affected projection recomputes. | This should not primarily be hand-declared traversal policy. The Materializer ancestor compiled MATCH topology into a query plan and used reverse traversal to find affected anchors. Lattice should recover that idea: compile the lens into an **execution plan + invalidation plan**. Declare only the anchor/projection semantics Cypher cannot express. |
| **Delete-key / empty aggregate** | `SetActorDeleteKey` + `ErrDeleteProjection` | On actor disappearance, derive which projection key to delete. When a live actor has zero real grants/tasks, delete the disjoint projection key instead of silently skipping and leaving stale state. | The actor-disappearance key is derived from the projection key pattern, not from the deleted actor row. Empty-aggregate semantics are also not pure Cypher: no real rows may mean "delete/tombstone previous projection," not merely "emit no output." |

**Origin (from in-code provenance):** the envelope-at-pipeline-layer pattern is **Story 3.2a Phase C, Decision #3** — *"the envelope shape is target-specific; we wrap at the pipeline layer so the generic `adapter.Adapter` interface stays unchanged."* Fan-out is **3.2b Decision #4**; freshness is **Story 1.5.4**. It was the right call at **N=1**. It is now **N=4**, and each new per-actor lens forces a core edit.

**Materializer ancestor note.** Refractor was lifted from the sibling `Materializer` repo. Materializer's original design explicitly included **derived triggers**, **compiled query plans**, and **topology-aware invalidation**. Its simple compiler turned MATCH/RETURN into a `QueryPlan`; when a non-anchor node changed, the evaluator reverse-traversed the compiled steps to locate affected anchors and re-evaluate each. Lattice retained this pattern for the simple engine, but the full openCypher path currently bypasses a compiled invalidation plan and uses the broader `ActorEnumerator` BFS for actor-aware lenses.

---

## 3. Decisions to be taken (for the session)

### D-PIPELINE — Compile per-actor projection/invalidation plans (NEW; prerequisite for the decomposition)

**Problem.** The three behaviors in §2 are Go keyed on `CanonicalName`. A package cannot ship a per-actor aggregating lens without a core change.

**Updated direction from Winston session (2026-06-05).** Do **not** solve this by making every behavior hand-declarable. The better center of gravity is:

> Packages declare lenses; Refractor compiles them into projection + invalidation plans. The lens contract declares only the projection semantics Cypher cannot express.

The compiled plan should have two halves:

- **Execution plan** — already largely present in the full openCypher engine: given an anchor actor (`$actorKey`), evaluate the lens against current Core-KV + Adjacency-KV state.
- **Invalidation plan** — new for full-engine lenses: derive, from the MATCH topology, which anchor actors become stale when a referenced vertex, link, or aspect changes.

What should be compiled from Cypher:

- Anchor type and anchor variable/parameter where possible (`MATCH (identity:identity {key: $actorKey})`).
- Relationship topology, direction, and path caps (`assignedTo`, `forOperation`, `scopedTo`, `reportsTo`, `holdsRole`, `grantedBy`, `containedIn`, etc.).
- Reverse paths from referenced vertex types back to the actor anchor.
- Link-event invalidation paths: on `lnk.<src>.<id>.<name>.<dst>.<id>` CDC, seed invalidation from the parsed endpoints, not only from current adjacency.

What should remain declarative in the LensDefinition contract:

- **Projection kind** — e.g. `actorAggregate`; tells Refractor this is an anchor-scoped aggregate, not a plain row projection.
- **Anchor binding contract** — actor vertex type and parameter name when not safely inferable.
- **Output-key derivation** — a constrained pattern, e.g. `cap.ephemeral.{actorSuffix}` / `my-tasks.{actorSuffix}`.
- **Envelope schema/field mapping** — constrained shape mapping, not a general template language.
- **Empty aggregate behavior** — `delete`, `softDelete`, or `emptyDoc`.
- **Freshness** — automatic `projectedFromRevisions` for projection-plane writes, with D-INTEGRITY defining monotonic semantics.

**Compiler scope / level-of-effort.** A full, exact compiler for all openCypher is epic-sized. The near-term target should be a **narrow invalidation compiler** for the subset Lattice lenses currently use: `MATCH` / `OPTIONAL MATCH`, labels, relationship names/directions, variable-length hops with existing caps, `WHERE` treated conservatively for invalidation, and `WITH` handled only where it preserves path variables simply. Unsupported constructs should either (a) fail lens activation for auth-plane actor aggregates, or (b) explicitly fall back to broad actor BFS for non-security projections.

**Options to weigh:**
1. **Compiled invalidation plan + typed projection-kind (recommended)** — full-engine lens activation builds a `ProjectionPlan{Execution, Invalidation, Output}`. Refractor uses compiled topology for precise invalidation and declarative projection fields for key/envelope/delete semantics. Kills the per-name switch without requiring a full template language.
2. **Typed projection-kind with broad actor BFS** — smaller implementation: packages declare `actorAggregate`, key pattern, and envelope mapping; Refractor keeps a generic BFS enumerator. Removes core-edit requirement but keeps imprecise invalidation and may over-reproject.
3. **Full data-drive** — all behavior hand-declarable, including traversal specs. More expressive, but risks inventing a second query language beside Cypher.
4. **Status quo + guardrail** — keep the switch but add a registry interface so packages *register* a wrapper at install time instead of core hardcoding names. Removes the core-edit requirement without the contract/compiler work, but preserves package-provided Go/plugin coupling.

**Winston's lean:** (1). The previous lean toward "typed `projectionKind`" was directionally right but incomplete. The harder and more valuable part is **compiled invalidation**, reviving the Materializer pattern for the full openCypher engine. `projectionKind: actorAggregate` should be the small contract marker that enables this planner, not the whole solution. Sequence as:

- **Spike:** extract invalidation paths from the existing full AST for `myTasks` and `capabilityEphemeral`.
- **Story:** compile and execute invalidation plans for the current package-lens subset.
- **Later epic:** broaden planning coverage for richer openCypher constructs if real lenses need them.

### D-PROJECTION — God-cypher → package-owned disjoint-key lenses (EXISTING open item)

Already tracked in `lattice-architecture.md` (§ near line 1188) and Contract #6 §6.1. Core owns the Capability-KV bucket + key conventions; **each package projects the grant types it owns** into a disjoint key space. The bootstrap `capability` cypher shrinks toward just the primordial-identity anchor; `rbac-domain` owns role/permission grants, service-location owns service access, `orchestration-base` owns ephemeral task grants.

**Status:** `capabilityEphemeral` (Story 7.1) is the **first proof-of-pattern** (the "contract-contribution model" — Contract #6 §6.1 amendment); `myTasks` (Story 7.2) is the second disjoint-key package lens. The rbac/service projection moves are **not scheduled**.

**Dependency note for the session:** this decision's projection side is **gated by D-PIPELINE**. Moving role/permission projection into `rbac-domain` as a package lens means `rbac-domain` ships a per-actor aggregating lens — which today requires a core switch case. Sequence D-PIPELINE first, or the decomposition just relocates the coupling.

### D-CONSUMER — Generic step-3 auth-hook dispatcher (EXISTING open item)

Also tracked in `lattice-architecture.md`. `internal/processor/step3_auth_capability.go` hardcodes the consumer-side dispatch (`taskSet → matchEphemeralGrant`, `serviceSet → matchServiceAccess`, default → `matchPlatformPermission`). Target: step-3 becomes a **generic dispatcher** over grant-matchers that packages **register at install time** — the dispatch table is data, not a hardcoded `switch`. Symmetric to D-PROJECTION on the read side. **Security-critical** (the auth hot path); needs the full 3-layer rigor + Gate 2/3 when scheduled.

### D-INTEGRITY — Projection-plane write ordering / stale-reprojection resurrection (NEW; from 7.2 review)

**Problem.** `internal/refractor/adapter/natskv.go` (`Upsert` / `Delete`) writes projections with **unconditional** `kv.Put` / `kv.Delete` — no `ExpectedRevision`, no monotonic guard, last-writer-wins. Combined with the pipeline retry queue and CreateTask's 4-event fan-out, a delayed/retried **stale "open-era" reprojection can land after a close-delete**, resurrecting a deleted projection that no further CDC event will re-delete. Surfaces:
- **`my-tasks`** — a closed task reappears and lingers (a queryable surface lies; no auth consequence).
- **`capabilityEphemeral`** (same adapter + fan-out) — a **revoked ephemeral grant could be resurrected on the security plane**. This is the serious one; reachability must be assessed explicitly.

Pre-existing (inherited from the Story 7.1 ephemeral lens); 7.2 is just the first story to lean on vanish-on-close as a *correctness guarantee*. The 7.2 E2E currently **masks** it with a `requireQuiescentRevision` settle-wait (relabelled in 7.2 to say so honestly). Tracked as background task `task_3d57a524` ("Revision-guard refractor projection writes").

**Proposed mechanism (Andrew's lean, refined in Winston session):** **soft-delete/tombstone + monotonic `projectedFromRevisions` guard** for affected projection classes. A pure hard delete loses the target-side revision vector; a delayed stale upsert can see "no key" and recreate it. A soft tombstone (or a sidecar watermark key, if physical absence is required for a bucket) preserves the latest source revision vector so stale upserts can be rejected.

The freshness field that already exists for auth-coherence (§2) becomes the ordering guard too — elegant, since it's the same datum. For aggregate projections, compare revision vectors/maps, not a single scalar.

**Options to weigh:** (a) soft tombstone carrying `projectedFromRevisions` (current lean for `capabilityEphemeral`; likely acceptable because absence and tombstone both deny); (b) hard delete + sidecar per-projection watermark key (for buckets where physical absence is product-visible, such as `my-tasks`); (c) adapter-level per-key monotonic sequence; (d) reconciliation sweep as a secondary safety net, not the primary guard. **Open question:** define vector comparison semantics when a projection aggregates multiple source vertices (actor + N roles/tasks/links) and the source set itself changes.

---

## 4. Consequences & constraints

- **Contract #6 §6.2 / §6.3 are FROZEN.** D-PIPELINE's envelope mapping and D-INTEGRITY's revision/tombstone semantics touch the §6.2 envelope shape and the §6.3 `projectedFromRevisions` field — these are **contract-amendment-shaped** changes (raise via the amendment process, not in-flight edits).
- **D-CONSUMER and the security plane of D-INTEGRITY are auth-critical** — full 3-layer adversarial review + Gate 2 (BLOCKED) + Gate 3 (DEFENDED) are mandatory when either is scheduled.
- **Sequencing:** D-PIPELINE unblocks the projection side of D-PROJECTION. D-INTEGRITY is independent and can land first (it's a correctness/security fix, not a refactor) — and arguably should, given the security-plane exposure.
- **Scope:** D-PIPELINE + D-PROJECTION + D-CONSUMER together are **epic-sized**, not story-sized; they must not be folded into 7.x feature work. D-INTEGRITY is story-sized and already has a task chip.

---

## 5. Recommended framing for the session

1. **Take D-INTEGRITY first** — it's a bounded security/correctness fix with a clear lean (soft tombstone or sidecar watermark + monotonic `projectedFromRevisions` guard); resolve the multi-source vector comparison question and ship it.
2. **Decide D-PIPELINE next** — adopt compiled projection/invalidation plans plus a small `projectionKind: actorAggregate` contract marker. This is the keystone that makes "packages own their projections" actually true.
3. **Then schedule D-PROJECTION + D-CONSUMER** as the full contract-contribution decomposition, gated on D-PIPELINE, with security rigor for the consumer side.

---

## 6. References

- Code: `cmd/refractor/main.go` (the `CanonicalName` switch, ~line 253), `internal/refractor/capabilityenv/envelope.go` (the wrappers + `projectedFromRevisions`), `internal/refractor/adapter/natskv.go` (unconditional `Upsert`/`Delete`), `internal/refractor/pipeline/` (`ActorEnumerator`, link/aspect fan-out, retry queue), `internal/refractor/ruleengine/full/ast.go` (full openCypher AST), `internal/refractor/ruleengine/simple/{compiler,evaluator}.go` (Materializer-derived compiled plan + reverse traversal), `internal/processor/step3_auth_capability.go` (consumer dispatch), `internal/bootstrap/lenses.go` (the god-cypher).
- Contracts: Contract #6 §6.1 (multi-lens / disjoint-key / contract-contribution), §6.2–§6.3 (envelope + `projectedFromRevisions`), §6.6 amendment (ephemeral extraction); Contract #10 §10.1/§10.7 (task + ephemeral grants).
- Ancestor: sibling `Materializer` repo, especially `_bmad-output/brainstorming/brainstorming-session-2026-03-18-001.md` (derived triggers / compiled query plans / topology-aware invalidation) and `_bmad-output/implementation-artifacts/1-8-graph-traversal-evaluator.md` (FR6a reverse traversal).
- Planning: `lattice-architecture.md` open item *"Capability Lens god-cypher → contract-contribution model"* (~line 1188).
- Stories: 3.2a/3.2b (envelope/fan-out origin), 1.5.4 (freshness), 7.1 (`capabilityEphemeral` first extraction), 7.2 (`myTasks`, review that surfaced D-PIPELINE + D-INTEGRITY).
- Background task: `task_3d57a524` (revision-guard projection writes — D-INTEGRITY).
