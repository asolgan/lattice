# ADR 0001 — Refractor per-actor lens projection: generalization, capability decomposition, and projection-plane integrity

- **Status:** Proposed — framing document for a dedicated Winston architecture session. Nothing here is ratified.
- **Date:** 2026-06-05
- **Deciders:** Andrew + Winston (architecture session, to be scheduled)
- **Canonical ADR number:** to be assigned at ratification (the global ADR-NN sequence is planning-owned, in `_bmad-output/planning-artifacts/lattice-architecture.md`). This file uses a local `docs/decisions` number until then.
- **Supersedes/extends:** the open item *"Capability Lens god-cypher → contract-contribution model"* in `lattice-architecture.md` (§ near line 1188). This ADR keeps that item's two decisions and adds two more that the 7.2 review surfaced.

---

## 1. Why this document exists

Story 7.2 added a `myTasks` lens to `orchestration-base`. Both the Acceptance Auditor and Andrew independently flagged the same smell: a *package* lens cannot be added without editing **core** — specifically a new `case` in `cmd/refractor/main.go` and a new wrapper in `internal/refractor/capabilityenv/`. That contradicts the minimal-core / everything-is-a-package principle and the package-layering rule (a package may depend on core, never the reverse).

This ADR consolidates four related decisions so they can be taken together rather than piecemeal across stories. Two are already-tracked planning open items; two are new from the 7.2 review.

A crucial framing the existing open item does **not** capture: the existing item assumes "each package projects the grant types it owns." The 7.2 review shows packages **currently can't** do that without a core code change. **Decision D-PIPELINE below is therefore a prerequisite** for the projection side of the existing decomposition — not an independent nicety.

---

## 2. Current state (what is and isn't data-driven)

The lens **definition** is already data, sourced from Core-KV and processed generically:

- `cmd/refractor/main.go` watches `vtx.meta.>` and builds a runtime Rule per lens definition.
- Each definition carries, as data: the cypher `Spec`, `engineKind`, target (`Into.Bucket` / `Into.Key` / `Into.Target` ∈ {nats_kv, postgres}), and delete mode.
- A **plain** lens (e.g. `identity-hygiene`'s `duplicateCandidates`) matches **no** `case` in the per-lens switch and runs the pure path: watch → cypher → write rows to its bucket. This is the "any other lens" path.

What is **not** data-driven — hardcoded in Go, keyed on `CanonicalName` — is the **per-actor projection plumbing**. Four lenses hit the switch (`capability`, `capabilityRoleIndex`, `capabilityEphemeral`, `myTasks`), each wiring three behaviors a plain lens lacks, via `pipeline` setters:

| Behavior | Setter | What it does | Why a cypher can't |
|---|---|---|---|
| **Envelope** | `SetEnvelopeFn` | Reshapes each cypher row into a target-specific output schema (Contract #6 §6.2), derives the **output KV key** (`cap.identity.<id>`, `cap.ephemeral.<id>`, `my-tasks.identity.<id>`), drops non-matching rows, and stamps **`projectedFromRevisions`** freshness metadata (the Story 1.5.4 auth-coherence field). | Output schema, key derivation, and source-revision metadata live outside the RETURN row. |
| **Fan-out** | `SetActorEnumerator` | These are per-actor aggregations anchored on `$actorKey`, but CDC events arrive on a role / permission / task / link — **not** the identity. The enumerator walks adjacency to "which actors are now stale?" so each affected projection recomputes. | A plain lens only reprojects the vertex that changed; cross-vertex fan-out needs an adjacency walk + depth/actor-set caps (policy). |
| **Delete-key** | `SetActorDeleteKey` | On actor-vertex disappearance, derive which projection key to delete (disjoint per lens). | The deletion key isn't in any row; it's derived from the (now-absent) actor. |

**Origin (from in-code provenance):** the envelope-at-pipeline-layer pattern is **Story 3.2a Phase C, Decision #3** — *"the envelope shape is target-specific; we wrap at the pipeline layer so the generic `adapter.Adapter` interface stays unchanged."* Fan-out is **3.2b Decision #4**; freshness is **Story 1.5.4**. It was the right call at **N=1**. It is now **N=4**, and each new per-actor lens forces a core edit.

---

## 3. Decisions to be taken (for the session)

### D-PIPELINE — Make per-actor projection plumbing declarable (NEW; prerequisite for the decomposition)

**Problem.** The three behaviors in §2 are Go keyed on `CanonicalName`. A package cannot ship a per-actor aggregating lens without a core change.

**Direction.** Promote the three behaviors into the **LensDefinition contract** so the refractor interprets them generically and the `main.go` switch is deleted:

- **Envelope schema** → a declarative template/shape carried in the definition.
- **Output-key derivation** → a declarative rule (e.g. pattern `cap.<vertexType>.<id>` / `my-tasks.<vertexType>.<id>`).
- **Freshness** → should be **automatic for every lens**, not opt-in (today only the four switch cases get `projectedFromRevisions`).
- **Fan-out** → the genuinely hard one: an **anchor + traversal spec** (anchor vertex type + adjacency relations to walk + depth/actor-set caps as declared policy). This is the part that needs the most design.

**Options to weigh:**
1. **Full data-drive** — all four declarable; refractor becomes a generic per-actor projector; switch dies entirely.
2. **Typed engine-kind** — add a `projectionKind` (e.g. `per-actor-aggregate`) to the definition; refractor selects a generic envelope/fan-out *engine* parameterized by a few declared fields. Less expressive than (1), much smaller blast radius, still kills the per-name switch.
3. **Status quo + guardrail** — keep the switch but add a registry interface so packages *register* a wrapper at install time instead of core hardcoding names. Removes the core-edit requirement without a full contract change.

**Winston's lean:** (2) for the near term — a typed `projectionKind` captures `capability`, `capabilityEphemeral`, and `myTasks` (all "per-actor aggregate, disjoint key, fan-out over adjacency") with a handful of declared fields, kills the switch, and is a far smaller change than a full template language. Revisit (1) only if a lens appears that (2) can't express. (3) is a fallback if we want to unblock package authorship *before* the contract work lands.

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

**Proposed mechanism (Andrew's lean):** **CAS on `projectedFromRevision`** — carry the source vertex's Core-KV revision into the projection and **reject an Upsert whose source revision is older** than what the target already reflects (e.g. `kv.Update` against the last-seen revision, or a per-key monotonic `projectedFromRevisions` check in the adapter). The freshness field that already exists for auth-coherence (§2) becomes the ordering guard too — elegant, since it's the same datum.

**Options to weigh:** (a) CAS-on-`projectedFromRevision` (lean); (b) adapter-level per-key monotonic sequence; (c) make projection writes idempotent + a reconciliation sweep. **Open question:** what is the correct revision to compare when a projection aggregates *multiple* source vertices (the actor + N roles/tasks)? A single scalar may be insufficient — may need max-revision-per-source, which `projectedFromRevisions` already models as a map.

---

## 4. Consequences & constraints

- **Contract #6 §6.2 / §6.3 are FROZEN.** D-PIPELINE's envelope template and D-INTEGRITY's revision semantics touch the §6.2 envelope shape and the §6.3 `projectedFromRevisions` field — these are **contract-amendment-shaped** changes (raise via the amendment process, not in-flight edits).
- **D-CONSUMER and the security plane of D-INTEGRITY are auth-critical** — full 3-layer adversarial review + Gate 2 (BLOCKED) + Gate 3 (DEFENDED) are mandatory when either is scheduled.
- **Sequencing:** D-PIPELINE unblocks the projection side of D-PROJECTION. D-INTEGRITY is independent and can land first (it's a correctness/security fix, not a refactor) — and arguably should, given the security-plane exposure.
- **Scope:** D-PIPELINE + D-PROJECTION + D-CONSUMER together are **epic-sized**, not story-sized; they must not be folded into 7.x feature work. D-INTEGRITY is story-sized and already has a task chip.

---

## 5. Recommended framing for the session

1. **Take D-INTEGRITY first** — it's a bounded security/correctness fix with a clear lean (CAS-on-`projectedFromRevision`); resolve the multi-source-revision open question and ship it.
2. **Decide D-PIPELINE next** — pick option (1)/(2)/(3); this is the keystone that makes "packages own their projections" actually true. Recommended: (2) typed `projectionKind`.
3. **Then schedule D-PROJECTION + D-CONSUMER** as the full contract-contribution decomposition, gated on D-PIPELINE, with security rigor for the consumer side.

---

## 6. References

- Code: `cmd/refractor/main.go` (the `CanonicalName` switch, ~line 253), `internal/refractor/capabilityenv/envelope.go` (the wrappers + `projectedFromRevisions`), `internal/refractor/adapter/natskv.go` (unconditional `Upsert`/`Delete`), `internal/refractor/pipeline/` (`ActorEnumerator`, retry queue), `internal/processor/step3_auth_capability.go` (consumer dispatch), `internal/bootstrap/lenses.go` (the god-cypher).
- Contracts: Contract #6 §6.1 (multi-lens / disjoint-key / contract-contribution), §6.2–§6.3 (envelope + `projectedFromRevisions`), §6.6 amendment (ephemeral extraction); Contract #10 §10.1/§10.7 (task + ephemeral grants).
- Planning: `lattice-architecture.md` open item *"Capability Lens god-cypher → contract-contribution model"* (~line 1188).
- Stories: 3.2a/3.2b (envelope/fan-out origin), 1.5.4 (freshness), 7.1 (`capabilityEphemeral` first extraction), 7.2 (`myTasks`, review that surfaced D-PIPELINE + D-INTEGRITY).
- Background task: `task_3d57a524` (revision-guard projection writes — D-INTEGRITY).
