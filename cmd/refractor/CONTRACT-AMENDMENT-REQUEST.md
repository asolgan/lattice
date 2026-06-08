# Contract / Planning Artifact Amendment Requests (Story 2.1)

These are planning-artifact-only corrections discovered during Story 2.1 morph work. Each is a text-only fix in `_bmad-output/planning-artifacts/epics.md`; no code impact.

---

## Request 1: epics.md AC #1 binary name

**Location:** `epics.md` Story 2.1 AC #1
**Current text:** "binary is `lattice-refractor`"
**Requested text:** "binary is `refractor`"
**Rationale:** All other Lattice binaries (`bootstrap`, `processor`, `refractor-stub`) use bare component names; no `lattice-` prefix is established convention. Winston ratified `refractor` as the binary name in handoff brief Decision #1.

## Request 2: epics.md AC #2 key prefix table

**Location:** `epics.md` Story 2.1 AC #2 — text mentioning `vtx.*`, `asp.*`, `lnk.*` prefix routing
**Current text:** Implies `asp.*` is a top-level key prefix for aspects.
**Requested text:** Replace with reference to `data-contracts.md` Contract #1 §1.5: aspects are keyed `vtx.<type>.<id>.<localName>` (4-segment `vtx.` prefix), NOT `asp.*`. Classification is by key SHAPE (segment count) and the value document's `class` field, not by raw prefix string matching. Use `substrate.ClassifyKey`.
**Rationale:** `asp.*` does not exist in the data contract. The stale text predates the contract finalization.

## Request 3 [Story 2.1b — RESOLVED]: data-contracts.md §1.7 meta-vertex key shape

**Location:** `data-contracts.md` Contract #1 §1.7 (meta-vertex pattern)
**Issue:** The handoff brief Decision #5 references `vtx.meta.lens.<NanoID>` as the lens-definition key. This is a 4-segment key, which `substrate.ClassifyKey` treats as an ASPECT, not a vertex. Story 2.1 used `vtx.lens.<NanoID>` (3-segment) instead.
**Requested clarification:** Either (a) confirm §1.7 actually uses the 3-segment shape `vtx.<reservedType>.<id>` where `meta` is a type-prefix convention (in which case the brief wording is just imprecise), or (b) define the multi-segment meta-vertex shape as a substrate extension with explicit support in `ClassifyKey`. See MORPH-DEVIATIONS Deviation 12.

**Resolution (Story 2.1b):** Option (a) confirmed by data-contracts.md §1.2 line 70: "`lens`, `event`, `ddl`, `actor` — these are *flavors of `meta`*, distinguished by the document's `class` field (`meta.lens`, `meta.event.<name>`, `meta.ddl.vertexType`, etc.)" The canonical lens key shape is `vtx.meta.<NanoID>` (3-segment vertex with type `meta`) with the document envelope's `class` field set to `"meta.lens"`. No amendment to `data-contracts.md` is needed; the Story 2.1 handoff brief Decision #5 wording was just imprecise (it said `vtx.meta.lens.<NanoID>` when it should have said `vtx.meta.<NanoID>` with class `meta.lens`). Story 2.1b corrected the implementation accordingly — see MORPH-DEVIATIONS Deviation 12 resolution. The 3-segment shape with class-based routing is also what `internal/bootstrap/primordial.go` already uses for the Capability Lens (line 322: `MakeVertexEnvelope(CapabilityLensKey, "meta.lens", ...)`), confirming the pattern across Lattice components.

---

# Epic 12 — Projection-plane integrity & capability decomposition (proposed 2026-06-07)

Source: Winston architecture session on `_bmad-output/planning-artifacts/refractor-lens-decomposition-brief.md`; rationale in `docs/decisions/projection-plane-decomposition.md`. Contract #6 §6.1/§6.2/§6.3/§6.8/§6.13 are FROZEN — these are amendment *requests*, ratified by the planning lead before any edit.

## Request 4 [Story 12.1 — D-INTEGRITY]: monotonic `projectionSeq` write-ordering guard on the capability plane

**Location:** Contract #6 §6.2 (document shape) + §6.3 (field spec) + §6.8 ("No Entry = No Access").

**Problem.** `internal/refractor/adapter/natskv.go` `Upsert`/`Delete` write unconditionally (last-writer-wins). The pipeline retry queue replays a **captured row** (`pipeline.go` `enqueueRetry` → `a.Upsert(capturedResult.Keys, capturedResult.Row)`), so a stale "open-era" projection can land after a close-`Delete` and **resurrect a revoked ephemeral grant on the security plane** (`cap.ephemeral.<actor>`) — no further CDC event re-deletes it. Confirmed reachable (see decision record).

**Requested amendment:**
1. Add an ordering field: **`projectionSeq`** (integer) = the JetStream stream sequence of the triggering CDC message. Required on the actor-aggregate classes `cap.<actor>`, `cap.ephemeral.<actor>`, `my-tasks.<actor>`. **`cap.role-by-operation.<op>` is excluded** — it is an operation-aggregate (keyed by `operationType`, not actor), with a different resurrection profile (party review, finding #7).
2. Define write-ordering semantics: a projection write to a guarded key is **rejected as an idempotent no-op when `incoming.projectionSeq ≤ stored.projectionSeq`**. The compare-and-set is **atomic against the target key's KV revision** (`Update`/`ExpectedRevision`) with a **bounded re-read-on-conflict loop** — load-bearing because the Refractor retry queue replays writes from a **separate goroutine** (`failure/retry.go:102`) concurrently with the main consumer.
3. §6.8: a **`Delete` on a guarded key is a soft tombstone** carrying `projectionSeq` + `isDeleted:true` (the high-water mark must survive physical absence). Absence and tombstone remain equivalent for auth (both deny) — no step-3 behavior change.
4. **Adapter-interface impact (implementation note, not contract):** `adapter.Adapter.Upsert/Delete` gain the ordering token (or an `EvalResult`-shaped arg); the **Postgres adapter is exempt** (pass-through, no guard); only `NatsKVAdapter` enforces. `EvalResult` gains a `projectionSeq` field so the retry-queue capture replays the *original* (lower) seq.
5. **Rebuild interaction (party review, finding #4):** `Rebuild(truncate=false)` replays historical lower-seq events that the guard would reject against live high-seq watermarks (rebuild silently restores nothing). Resolution: guarded buckets either force `truncate=true` (watermark cleared with data) or rebuild bypasses the guard for the replay — defined and tested in Story 12.1b.

**Rationale.** `projectedAt` is anchor-provenance-derived and is identical across open/close reprojections of the same actor (the actor vertex is unchanged when a task closes), so it cannot order these writes; `projectedFromRevisions` is incomplete (actor+lens only) and ambiguous under source-set shrink. The substrate's stream sequence is a total order that is plan-independent and deterministic-replay-safe. See decision record for the rejected alternatives (brief options b/c).

## Request 4b [Story 12.1a — D-INTEGRITY]: my-tasks tombstone consumer obligation (Contract #10 §10.1)

**Location:** Contract #10 §10.1 (`my-tasks` projection shape).

**Problem.** Story 12.1a changes the `my-tasks` delete from hard-delete (key vanishes) to **soft tombstone** (`{isDeleted:true, projectionSeq}`). Today the only reader is the E2E test; when a real UI/query consumes the `my-tasks` bucket it must skip tombstones or a user sees ghost tasks they already completed (party review, finding #11 — Sally).

**Requested amendment:** record in §10.1 that **`my-tasks` consumers MUST treat an `isDeleted:true` document as absence** (skip it). Forward obligation; no current production reader.

## Request 5 [Story 12.3 — D-PIPELINE]: `projectionKind` meta-lens aspect + declarative Output descriptor

**Location:** Contract #6 §6.13 (Implementation Notes / meta-lens aspect inventory).

**Problem.** A per-actor aggregating lens cannot be added without a core edit (a `case` in `cmd/refractor/main.go` + a wrapper in `internal/refractor/capabilityenv/`) — contradicting the package-layering rule.

**Requested amendment:** define a new optional `meta.lens` aspect **`projectionKind`** with value `"actorAggregate"`, plus a constrained **Output descriptor** (lens-definition aspects) that replaces the Go wrappers: `anchorType`, `outputKeyPattern` (constrained pattern, e.g. `cap.ephemeral.{actorSuffix}`), `bodyColumns`, `emptyBehavior` (`delete`|`softDelete`|`emptyDoc`|`skip`), `realnessFilter` (`{field}` — drop degenerate collect artifacts), `freshness: auto`. When present, Refractor compiles a `ProjectionPlan{Execution, Invalidation, Output}` and drives the lens generically (compiled reverse-traversal invalidation replaces the broad `ActorEnumerator` BFS). An auth-plane actor-aggregate lens whose MATCH uses an uncompilable construct **fails activation** (fail closed); non-security lenses fall back to broad BFS with a warning.

**Rationale.** All four built-in wrappers reduce to this descriptor + the compiled plan; the machinery (simple-engine reverse-traversal + full-engine AST) already exists. See decision record D-PIPELINE.

## Request 6 [Story 12.3 — D-PIPELINE]: widen `projectedFromRevisions` to the full contributing source set

**Location:** Contract #6 §6.3 (`projectedFromRevisions` field).

**Problem.** The field currently stamps only the actor + lens-def revisions (`capabilityenv/envelope.go:99-110`), so it does not reflect the tasks/roles/links the projection actually read — the coherence-window detection the bypass suite relies on is incomplete.

**Requested amendment:** `projectedFromRevisions` MUST cover the source set the compiled plan read for the projection — actor + contributing roles/tasks/services/links. **Scope (party review, finding #10):** v1 covers sources that *contributed a binding*. Covering sources that were *read-then-excluded* (e.g. a now-closed task) requires the full executor to report every Core-KV key it touched-then-dropped (executor instrumentation) — Story 12.3 must state whether that is in-scope or a follow-up. This is the coherence/debug datum and is distinct from the `projectionSeq` ordering guard (Request 4).

**Note:** `projectionKind` (Request 5) covers `actorAggregate`. `capabilityRoleIndex` needs either a second kind (e.g. `operationAggregate`) or a documented bespoke path — it is **not** an `actorAggregate` (Story 12.4; party review finding #7).

## Request 7 [Story 12.6/12.7 — D-PROJECTION]: disjoint-key conventions for decomposed grant projections

**Location:** Contract #6 §6.1 (Bucket and Key Pattern) + the multi-Lens / contract-contribution note.

**Requested amendment:** register the new disjoint key prefixes produced by package-owned grant lenses as the god-cypher decomposes — `cap.roles.<actor>` (rbac-domain role/permission grants, Story 12.6) and a service-access disjoint key (working name `cap.svc.<actor>`, Story 12.7) — mirroring the existing `cap.ephemeral.<actor>` contribution (§6.6 amendment). Record that the bootstrap `capability` cypher shrinks to the primordial-identity anchor (or retires), with core owning only the bucket + key conventions + the step-3 dispatcher. Mark the `lattice-architecture.md` god-cypher open item resolved (planning-lead action).

**Note on `service-location` (Story 12.7).** The `service-location` package **does not exist** — it is specified only as a concept (`packages/service-location/CONCEPT.md`, authored 2026-06-07). 12.7 is two-path: implement `capabilityServiceAccess` → `cap.svc.<actor>` if the package exists, **else just delete** the service/location MATCHes from the bootstrap cypher and leave the `cap.svc.*` key space registered-but-empty for a future service package. The contract amendment should register the `cap.svc.*` prefix regardless (so a later package projects into it with no core/contract churn).
