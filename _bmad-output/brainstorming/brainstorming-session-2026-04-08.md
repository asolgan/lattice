---
stepsCompleted: [1, 2, 3]
inputDocuments:
  - "Obsidian Vault/Lattice/Lattice System Spec.md"
session_topic: "Lattice implementation stream decomposition"
session_goals: "Produce (a) a dependency graph across parallel implementation streams and (b) a clear articulation of each component's boundaries and responsibilities. Open question: do streams map to components, or cut across them?"
selected_approach: "ai-recommended"
techniques_used:
  - "Component Inventory Sweep (divergent unit-listing)"
  - "Cohesion Clustering (group by who-changes-together)"
  - "Reverse-Dependency Probing (day-1 prerequisites per unit)"
  - "First-Principles Re-Partitioning (ignore spec headings, re-derive)"
  - "Vertical Slice Forcing Function (smallest end-to-end validates the graph)"
  - "Pre-Mortem Adversarial Cuts (this split failed because ___)"
ideas_generated: []
context_file: ""
---

# Brainstorming Session Results

**Facilitator:** Andrew
**Date:** 2026-04-08

## Session Overview

**Topic:** Lattice implementation stream decomposition — how to carve the system into parallel work streams, with explicit dependencies and component boundaries.

**Goals:**
- A dependency graph across implementation streams (what blocks what; what can truly run in parallel)
- A clear, written articulation of each component's boundaries and responsibilities (what it owns, what it does NOT own, what it consumes from neighbors)

### Context

- Greenfield codebase under code name **Lattice**
- Existing repo `github/materializer` is closest in spirit to **Refractor** — its source will be morphed to fit the Lattice model
- Source-of-truth spec: `Lattice System Spec.md`
- Component-level notes exist for: Lattice Core, Lens and Refractor, Loom, Weaver, Edge Lattice, Sharding (Cell), Observability, plus Brainstorm PII and Crypto-Shredding, Contract as Executable, The Lattice Manifest, Adversarial Review

### Session Setup

**Approach:** AI-Recommended progressive flow.
**Open framing question (C):** stream-to-component mapping is *not* presupposed — the brainstorm itself decides whether streams align 1:1 with components or cut across them.

**Technique Sequence:**
1. **Component Inventory Sweep** — list every nameable unit of work in the spec (no grouping yet)
2. **Cohesion Clustering** — group by "who must change together" (deployment, code, schema, ownership)
3. **Reverse-Dependency Probing** — for each cluster, ask "what would I need on day 1 to start coding this in isolation?" → that *is* the dependency edge
4. **First-Principles Re-Partitioning** — ignore section headings of the spec; re-derive boundaries from data flow + change frequency + deployment unit
5. **Vertical Slice Forcing Function** — smallest end-to-end (one op → ledger → processor → KV → lens → query) walks the graph and exposes missing edges
6. **Pre-Mortem Adversarial Cuts** — "this split failed because ___" exposes hidden coupling

## Phase 1 — Component Inventory Sweep

_(Raw, ungrouped, no judgment. We're listing nameable units of work pulled from the spec + component notes. Goal: 100+ items so the clustering step has real material.)_

### Inventory v1 — initial seed (76 items)

_Note: items 29–30 were originally misfiled under "Identity/Auth" — corrected per user feedback: Capability KV is a Lens built by Refractor._

#### A. Storage / data plane primitives
1. Vertex KV bucket conventions (`vtx.<type>.<id>`) + key-naming validator
2. Aspect KV bucket conventions (`asp.<vtxId>.<name>`)
3. Link KV bucket conventions (`lnk.<youngerId>.<name>.<olderId>`)
4. Soft-delete (`isDeleted`) semantics + tombstone read filter
5. KV bucket provisioning / TTL / replication policy per bucket class
6. Cross-bucket atomic write primitive (wraps NATS 2.12 atomic batch)
7. KV watcher fan-out helper (per-aspect subscription primitive)

#### B. Ledger / operations plane
8. `lattice-operations` JetStream definition + retention/limits
9. Lane subjects: `ops.meta.>`, `ops.urgent.>`, `ops.bulk.>`
10. Lane consumer wiring (sequential vs parallel per lane)
11. Operation envelope schema (headers, body, request-id)
12. **`Lattice-Actor` header spec + parser** (`root` / `identity:<vtxId>`)
13. Idempotency tracker writer (`vtx.op.<id>` creation pre-execute)
14. Idempotency cache lookup (early-return on duplicate request-id)

#### C. Processor / logic engine
15. Stateless Processor service skeleton (NATS consumer → dispatcher)
16. Starlark sandbox embedding + determinism guard (no I/O, no time, no random)
17. Starlark stdlib for Lattice (graph builders, link helpers, mutation batch ctor)
18. Script loader / cache (resolve which script handles which op)
19. MutationBatch validator against DDL schemas
20. EventList post-commit publisher

#### D. Schema, DDL, meta
21. `vtx.meta.ddl.<name>` write path + bootstrap
22. JSON Schema / OpenAPI validator integrated into Processor commit path
23. DDL migration tool (root-actor only)
24. Schema versioning + backward-compat policy
25. Self-describing introspection API ("what types/aspects exist?")

#### E. Identity, auth, ReBAC
26. Identity vertex type + canonical aspects
27. Actor authentication (signing keys → `Lattice-Actor` header)
28. Link-path traversal engine for permission paths
29. Capability KV (flattened path cache) writer **— corrected: this is a Lens built by Refractor, not a standalone auth component**
30. Capability KV reader (Processor's O(1) authz check)
31. Capability invalidation triggers (which graph mutations dirty which capabilities)

#### F. Refractor / Lenses
32. Lens meta vertex (`vtx.meta.lens.<id>`) + DDL
33. openCypher parser/IR
34. Cypher → SQL (Postgres target) compiler
35. Cypher → ES query compiler
36. Lens runtime: subscribes to `lattice-events` and/or KV deltas, materializes
37. Backfill/replay engine for new Lenses against historical ledger
38. RLS policy generator (mirrors auth links into target store)
39. Multi-target Lens registry / lifecycle
40. **Materializer → Refractor morph plan** (its own work-stream)

#### G. Orchestration
41. Loom service skeleton (event consumer → next-op publisher)
42. `vtx.meta.pattern` blueprint format + DDL
43. Loom pattern interpreter (sequential step driver)
44. Weaver service skeleton (convergence loop)
45. Target-State diffing engine
46. Nudge emitter (Task creation / command issuance)
47. Weaver temporal scheduler (time-based discrepancies, replaces cron)
48. External adapter framework (the Weaver "Nudges out" to APIs like payments)
49. Operation Vertex pruner (Weaver's idempotency-horizon GC)

#### H. Services / commands / tasks (UX surface)
50. Service vertex type + DDL
51. Command/Query vertex types + DDL
52. UI Form Schema aspect format
53. Task vertex type + assignedTo link semantics
54. Command discovery API (traverse from Identity → discoverable commands)
55. Dynamic form renderer client SDK

#### I. Crypto-shredding & PII
56. Vault service (per-identity key custody)
57. `sensitive` field marker in DDL
58. Processor encrypt-on-write middleware
59. Processor decrypt-on-read middleware (transparent to Starlark)
60. Key-shred command + propagation event
61. Refractor "Secure Lens" type with RLS
62. Weaver-driven projection nullification on shred

#### J. Semantic contracts / executable paper
63. `vtx.clause` type + DDL
64. Clause-to-state link semantics
65. Compliance Weaver mode (clause satisfaction loop)
66. AI judgment hook for clauses requiring discretion

#### K. Cross-cutting / platform
67. Observability — structured logging across all services
68. Observability — operation-id → trace span correlation (the `vtx.op` IS the trace)
69. Metrics: lane lag, lens lag, Weaver convergence time
70. Replay tool (rebuild KV state from ledger)
71. Local dev harness (NATS + KV + processor in one `make up`)
72. Test fixture: deterministic-replay golden tests for Starlark scripts
73. Multi-tenant / Cell sharding boundary (the `Cell.md` thread)
74. Edge Lattice — local-first / personal-lens runtime
75. Network/transport hardening (mTLS between services, NATS auth)
76. Schema registry CLI / IDE plugin

### Inventory v2 — additions from Cell.md, Observability notes, and adversarial pass

#### L. Sharding / Cells (from Cell.md)
77. Cell = physical NATS KV bucket; provisioning/lifecycle of cells
78. **Anchor principle enforcer**: a Vertex's aspects + outgoing links MUST be co-located in the same cell
79. **Global Adjacency Index** (`Vertex_ID → Cell_ID`, replicated KV)
80. Bridge Link writer: cross-cell links stored in BOTH source and destination cells
81. Migration coordinator: Shadow State flag (`MIGRATING`, `Target_Cell` metadata)
82. Dual-Target MutationBatch in Processor (write to current cell with revision check, blind write to target cell)
83. Hydrator (background bulk-copy of historical aspects/links during migration)
84. Merkle-tree comparator (Weaver verifies C02 matches C01)
85. Cell switchover engine (atomic flip of Global Adjacency Index)
86. `410 Gone` rejection path for stray writes to drained cell
87. Post-migration pruner (after idempotency horizon)
88. Refractor cross-cell deduplication by Revision ID
89. Cell-aware operation router (which Processor instance owns which cells)

#### M. Observability & Control (from Observability notes + Correction)
90. Health-as-KV aspect format (`asp.sys.health.<component_id>`)
91. Component heartbeat library (last_processed_sequence, error_rate, latency_p99, status)
92. Consumer-lag metric pipeline (Ledger Last_Sequence − Component Processed_Sequence per consumer)
93. **NATS Control Service endpoints** (`lattice.ctrl.<type>.<id>`): pause, resume, log-level, rebuild, replay
94. Pause-protocol semantics (consumer stops Fetch(); stream buffers)
95. Replay-from-sequence-0 primitive for Lens rebuilds
96. Closed-loop Weaver auditor (reads Health-KV, issues remediation Nudges)
97. Policy Vertex format (`vtx.infra.<pool_name>` with `desired_count` etc.)
98. **Infra-Actuator** (non-Lattice process) — watches Policy Vertices, calls K8s/AWS APIs
99. Actuator-side feedback loop (writes back to Health-KV with `current_count`)
100. Auto-circuit-breaker (Weaver issues PAUSE + creates investigation Task on repeated failures)

#### N. Adversarial / structural items I added on the pass
101. **Context Hinting on Command Vertex**: declared list of aspects/links the script needs → Processor pre-fetches in batch (kills the read-amplification death spiral from Adversarial Review #1)
102. **JIT Hydration cache**: short-lived per-op working set so co-located reads don't re-hit KV
103. **Targeted-audit Weaver**: subscribes to `lattice-events`, only audits subgraphs touched by recent events (kills full-table-scan melt-down from Adv. Review #2)
104. **Meta-Gatekeeper**: DDL changes require quorum or human-in-the-loop link; safe-mode Processor boot that bypasses DDL for recovery ops (kills DDL deadlock from Adv. Review #3)
105. **Read-Your-Own-Writes overlay**: client-side bridge that overlays `vtx.op.*` results onto Refractor projections until the projection catches up (kills async-gap UI lie from Adv. Review #4)
106. **Two-Phase Nudge** for external adapters: adapter "claims" a task with state visible in graph BEFORE calling the external API; Weaver respects claim and won't re-nudge (kills double-charge from Adv. Review #5)
107. Starlark perf budget + benchmark harness (resolves Adv. Review unchecked-assumption #1)
108. **Developer cold-start kit**: scaffolding CLI that generates DDL + minimal Starlark + minimal Lens for "Hello Lattice" (resolves unchecked-assumption #3)

#### O. My own adversarial additions (gaps not in the existing review)
109. **Cross-cell Bridge Link rewrite on migration**: when Vertex moves cells, all incoming Bridge Links from other cells point to a stale Cell_ID. Who finds and rewrites them? Weaver? Migration coordinator? Race conditions while migration is in flight?
110. **Cell-spanning openCypher queries**: a Lens projection may traverse a subgraph spanning N cells. Refractor receives change-feeds per-cell, independently — how does it know it has a *complete* enough view to emit a correct projection row? Out-of-order events across cells = wrong intermediate state. Need: explicit watermarks per cell or a vector-clock per Lens.
111. **Capability Lens staleness = auth bypass risk**: if Capability KV is a Lens (per correction), then revoking access requires Refractor to project the change BEFORE the next op runs. Processor reads capability O(1), but it's reading possibly-stale data. Race: revoke access → user submits op → Lens hasn't caught up → op succeeds. Mitigation options: (a) hard-block ops on a versioned capability vector clock, (b) treat capability as a Core KV exception, (c) accept the window and document it.
112. **DDL meta-vertex location ambiguity**: spec says `vtx.meta.ddl.*` lives in the graph (Core KV) and Processor validates against it. Good — no chicken-and-egg with Refractor. But same question as 111: if DDL changes propagate via the normal op path, when is the Processor required to refresh its in-memory cache? Schema-changing operations should fence the lane.
113. **Health-as-KV write path violates "Processor-only-writes-Core"**: services heartbeat directly. Either Health-KV is its own bucket outside Core (and we name the exception explicitly), or heartbeats become operations on the ledger (huge volume), or Health is sourced from NATS service replies and *projected* into a Lens by Refractor. Needs an architectural decision before either Refractor or Observability can be implemented.
114. **Per-cell sub-laning of `ops.urgent`**: lanes prevent HOL across types, but a slow op in Cell_A on `ops.urgent` can still block a fast op in Cell_B. Need per-cell consumer partitioning within each lane (or accept fairness loss).
115. **Atomic-batch size ceiling**: NATS atomic batching has byte/op limits. A Starlark script that legitimately touches thousands of vertices exceeds the batch size and either silently truncates or fails. Need: batch-size validator in Processor and a documented cap surfaced to script authors.
116. **Starlark script versioning + in-flight ops**: script v1 → v2 deployment race. An op submitted before the deploy may run against v2. Need: script version pinned in the operation envelope OR scripts immutable per version with versioned dispatch keys.
117. **Backfill vs head-of-stream during new Lens onboarding**: while a new Lens replays from seq 0, new events arrive at the head. Pause head? Buffer? Two-phase rebuild? Spec mentions REBUILD primitive but not the policy.
118. **`Lattice-Actor` header trust model**: NATS doesn't natively sign headers. If only the entry-gateway "trusts itself" to set the actor, any internal misbehavior = total impersonation. Need: actor claim as a signed JWT keyed by Identity vertex, verified by Processor.
119. **Idempotency horizon expiry surprise**: client retrying after the prune window gets a fresh `vtx.op` → re-execution. Need: horizon visible to clients via header AND a separate "long-tail idempotency" pattern (e.g., natural-key dedupe at the business level) for cross-day retries.
120. **Vault availability = SPOF for sensitive aspects**: per-identity keys live where? External KMS / HSM? If Vault is down, Processor cannot decrypt → all sensitive-touching operations fail. Cache strategy + degradation mode needed.
121. **Cross-cell atomic business mutations**: spec is silent on transferring state between two Identities in different cells (e.g., asset transfer A→B). Atomic batch can't span cells. Need: Saga pattern via Tracker vertices, or constrain transfers to within-cell.
122. **Loom vs Weaver overlap = capability creep**: many real workflows are "mostly sequential, with one converging branch." Choosing one forces either rigid imperative chains or wasteful convergence loops. Need decision rubric, OR the two should be the same engine with two modes.
123. **Lens schema migration / zero-downtime evolution**: when an existing Lens's target schema changes (e.g., add a column), how do you migrate without dropping queries? Spec mentions REBUILD but not the swap.
124. **Multi-tenant cell mapping**: are tenants 1:1 with cells (good isolation, bad utilization), N:1 (noisy neighbor), 1:N (atomic ops break across tenant data)? Decision needed before Cell sharding goes anywhere.
125. **AI-actor authority**: if AI agents can write DDL (per Adv. Review #3), what scopes their `Lattice-Actor`? Is `identity:ai_agent_42` just another identity, or do we need a special actor class with its own authz path?

### Resolutions (after second user pass + reading Refractor/Loom/Weaver/Edge/Personal Lens/Constraints/Materializer)

- **#109, #110, #121, #124** → DEFERRED post-MVP (single-cell MVP)
- **#111** → MVP: token-revocation KV checked by Gateway. v2: vector-clock fence (`Lattice-Min-Capability-Version` header on ops; Processor refuses dispatch until Capability Lens catches up). Token revocation handles kill-switch instantly; vector clocks handle slower-evolving permission graph consistency.
- **#112** → DDL is Core KV. Processor must invalidate DDL cache on `ops.meta.>` commits; meta lane is sequential per spec.
- **#113** → Health-as-KV is a third state plane (operational), NOT a Lens, NOT an exception. Each component owns its own operational KV.
- **#122** → RESOLVED: Loom = short modular utilities (Stripe handshake, background check, onboarding). Weaver = convergence engine that triggers Loom patterns when its declarative targets see discrepancies. Lease Application is a *target state* with multiple Loom utilities chained as fulfillments.

### Phase 1.5 — Architectural facts crystallized from the second-pass reading

**State planes (ownership boundaries → stream candidates)**:
1. **Core KV** — Processor only (writes via `lattice-operations`); business state, meta-vertices, op trackers, Loom instances, index vertices
2. **Lenses** (multi-target) — Refractor only; Postgres, ES, NATS KV (Capability), per-user NATS subjects (Personal Lens), AI-context transient lenses
3. **Refractor Health KV** + **Adjacency KV** — Refractor's own operational state
4. **Weaver Operational KV** (`weaver.state.>`) + **Weaver Work Stream** (`weaver.work.>`) — Weaver's own internal dispatch
5. **Vault** — external KMS/HSM, per-identity keys

**Loom and Weaver are CLIENTS of the Processor**, not parallel writers. Both submit ops via `lattice-operations` with revision-condition checks. Loom Instance Vertices live in Core KV. Weaver has only its own operational KV.

**Weaver depends on Refractor for declarative targets**: `vtx.meta.target` is openCypher; running it requires the Refractor's evaluator. Cleanest design: Weaver targets ARE Lenses that project "currently-violating-vertices" rows; Weaver subscribes to those rows. Weaver is then *another consumer of Refractor*, not an independent cypher runtime.

**Materializer ≈ Refractor at MVP grade** (already shipped):
- ANTLR4 openCypher parser (MATCH/OPTIONAL MATCH/RETURN; WHERE in v2)
- Anchor-first evaluator with compiled query plans
- Self-built Adjacency KV from Core KV edge events
- Postgres + NATS KV adapters (the latter unlocks Capability Lens immediately)
- Failure 4-tier classification, DLQ, retry queue, deferred re-evaluation
- Full NATS control plane (pause/resume/rebuild/replay/delete/zero-downtime migration)
- Health KV, lag metrics, audit stream
- Stateless, horizontally scaled via consumer queue groups
- In-memory NATS + YAML fixture test harness

**Refractor morph delta (what Materializer probably lacks):**
- Path Projection / RLS link mirroring
- Personal Lens (per-user NATS subject targets)
- Lens definitions sourced from `vtx.meta.lens.<id>` Core KV vertices (the "self-projecting configuration" loop)
- Crypto-shred propagation (wipe rows on KeyShredded event)
- Secure Lens (vault-decrypted aspects)
- Multi-hop link traversal richness needed by Capability Lens (depends on ReBAC path semantics)

**Constraint enforcement responsibility split** (from Constraints.md):
- Unique identity (email/SSN): Processor + Index Vertex pattern + Atomic Batch
- Data integrity (non-negative price): Processor + Starlark invariant
- Cross-entity (background check before lease): Loom + state machine
- Financial balance (invoice vs line items): Weaver + declarative audit
- Referential integrity (no links to deleted): Refractor + adjacency pruning

## Phase 2 — Cohesion Clustering: Implementation Streams (locked)

_Final cut after user feedback: Stream 7 folded into Stream 0; Infra-Actuator and Closed-loop Weaver auditor moved post-MVP; all other streams kept as proposed._

### Stream 0: Substrate + Shared Platform (foundational)
**Folded in:** original Stream 7 (Observability/Control libraries and standards)

**Owns:**
- NATS JetStream provisioning, KV bucket conventions, subject taxonomy
- `lattice-operations` and `lattice-events` stream definitions, lane subjects
- `Lattice-Actor` header spec + JWT signing/verification keyed by Identity vertex (#118)
- Operation envelope schema, request-id semantics, idempotency horizon header
- Local dev harness (`make up`: NATS + buckets + bootstrap identities) (#71)
- Shared in-memory NATS + YAML fixture test harness pattern (steal from Materializer)
- mTLS between services, NATS auth (#75)
- **Health-as-KV aspect format standard** (`asp.sys.health.<component_id>`) (#90)
- **Component heartbeat library** — shared lib used by all services (#91)
- Consumer-lag metric pipeline standard (#92)
- **NATS Control Service standard** (`lattice.ctrl.<type>.<id>`) — protocol, not implementation (#93)
- Pause-protocol semantics (#94)
- Replay-from-sequence-0 primitive (#95)
- Structured logging library (#67)
- Operation-id → trace span correlation (the `vtx.op` IS the trace) (#68)
- Lane lag / lens lag / convergence-time metrics standards (#69)
- Replay tool (rebuild KV state from ledger) (#70)
- Schema registry CLI / IDE plugin (#76) — *or move to Stream 1; tentatively here*

**Inventory items:** 8, 9, 10, 11, 12, 67, 68, 69, 70, 71, 75, 76, 90, 91, 92, 93, 94, 95, 118
**Dependencies:** none — bedrock
**Why a stream:** Every other stream consumes these primitives. Folding Observability libraries here keeps "shared platform" in one team's hands.
**Boundary:** Owns standards, libraries, conventions, and the dev harness — NOT business logic, NOT projection runtime, NOT orchestration.

---

### Stream 1: Core (Processor + KV write plane)
**Owns:**
- Vertex/Aspect/Link KV bucket conventions and key validators (#1, #2, #3)
- Soft-delete and cross-bucket atomic write primitives (#4, #6)
- KV bucket provisioning per bucket class (#5)
- Per-aspect KV watcher helpers (#7) — *consumed by Refractor; helper lives here*
- Processor: stateless dispatcher, Starlark sandbox, determinism guard, stdlib, script loader, MutationBatch validator, EventList publisher (#15–#20)
- DDL meta-vertex types + JSON Schema validator integration in commit path (#21, #22, #25)
- DDL bootstrap migration tool (root-actor only) (#23, #24)
- Index Vertex pattern primitives for unique constraints
- Idempotency tracker (`vtx.op`) writer + cache lookup (#13, #14)
- **Meta-Gatekeeper** (DDL change quorum / safe-mode boot) (#104)
- **Context Hinting on Command Vertex + JIT Hydration** (#101, #102)
- Atomic-batch size validator (#115)
- Starlark script versioning + version-pinned dispatch (#116)
- Vault interface (decryption middleware contract — implementation in Stream 6)
- Encrypt-on-write / decrypt-on-read middleware integration points (#58, #59)
- Test fixture: deterministic-replay golden tests for Starlark scripts (#72)

**Inventory items:** 1–7, 13–25, 58, 59, 72, 101, 102, 104, 115, 116
**Dependencies:** Stream 0
**Boundary:** Core never consults Refractor at runtime. Core never writes outside Core KV. Core does not interpret business semantics — it executes Starlark.

---

### Stream 2: Refractor (the projection plane)
**Owns:**
- Lens meta-vertex DDL (#32) — defined here, written via Stream 1's Processor
- Materializer codebase, morphed: parser, evaluator, adjacency KV, target adapters (#33, #34, #35, #36, #39)
- Postgres adapter (already done in Materializer)
- NATS KV adapter (already done) — **Capability Lens target**
- ES adapter (#35)
- **Path Projection / RLS link mirroring** (#38) — *new, morph delta*
- **Personal Lens** parameterized per-user NATS subject target — *new, morph delta*
- **Self-projecting configuration**: Lens definitions sourced from `vtx.meta.lens.<id>` (new circularity to design)
- Refractor Health KV (per-Lens lag/error/status)
- Adjacency KV (Refractor-private) — already in Materializer
- Backfill / replay engine for new Lenses (#37)
- Pause / resume / rebuild / replay / zero-downtime migration NATS control endpoints — already in Materializer
- Crypto-shred row-nullification handler (#62) — *new, listens for KeyShredded events*
- Secure Lens type with vault decryption integration (#61) — *new*
- 4-tier failure classification, DLQ, retry queue, deferred re-evaluation — already in Materializer
- **Read-Your-Own-Writes overlay protocol** (#105) — design lives here, client SDK code in Stream 5
- Materializer → Refractor morph plan (#40) — its own sub-task
- **Backfill vs head-of-stream policy for new Lens onboarding** (#117)
- **Lens schema migration / zero-downtime evolution** policy (#123)

**Inventory items:** 32–40, 61, 62, 105, 117, 123
**Dependencies:** Stream 0 (substrate), Stream 1 (Core writes the events Refractor consumes)
**Boundary:** Reads Core KV change feeds. Never writes Core KV. Only writes are projections to Refractor's own targets and updates to Refractor's own Health KV.

---

### Stream 3: Identity, Auth, and Capability
**Owns:**
- Identity vertex type + canonical aspects (#26)
- Actor authentication: signing key → JWT → header (#27)
- ReBAC link-path semantics: which link types form permission paths (#28)
- **Capability Lens definition** — the cypher rule that flattens permission paths into NATS-KV rows (the corrected #29)
- Capability Lens reader API (Processor's O(1) authz check) (#30)
- Capability invalidation triggers (#31)
- **Token revocation KV** at the Gateway (kill switch — MVP answer to #111)
- Gateway service: validates JWT, checks token revocation KV, stamps `Lattice-Actor`, enforces capability vector clock header in v2
- **Capability vector clock fence** (v2 — #111)
- AI actor authority semantics (#125)

**Inventory items:** 26–31, 111, 125
**Dependencies:** Stream 0 (substrate), Stream 2 (Capability Lens runs ON Refractor)
**Boundary:** Owns the *meaning* of permission paths and the Gateway. Does NOT own the projection runtime (that's Refractor). Does NOT own actor signing keys (external IdP/KMS).

---

### Stream 4: Orchestration (Loom + Weaver)
**Owns:**
- Loom: Sensorium event consumer, Transition Engine, Actuator, Instance Vertex DDL (#41, #43)
- `vtx.meta.pattern` blueprint format (#42)
- Loom utility libraries (Background Check, Stripe handshake, Onboarding sequences)
- Weaver: Sensorium 3-lane work stream (`weaver.work.>`), Operational KV (`weaver.state.>`), Evaluator (tiered intelligence L1/L2/L3), Strategist playbook registry, Actuator with revision-condition optimistic commits (#44, #45, #46)
- `vtx.meta.target` declarative target — implemented as a Refractor Lens
- **Targeted-audit Weaver** (event-driven, scoped to changed subgraph) (#103)
- Bootstrap deep-audit mode (Bulk Lane) vs steady-state mode
- AI Handshake protocol (Context Hydration → Proposed Intent → Validation → optional dual-link Human-in-Loop approval)
- Weaver temporal scheduler / Cron-killer (#47)
- External Adapter framework (#48) with **Two-Phase Nudge** (#106)
- Operation Vertex pruner (#49) — Weaver-driven idempotency-horizon GC
- Compliance Weaver mode for `vtx.clause` (#65) and clause-to-state link semantics (#63, #64) — *or move to deferred Stream 10*
- AI judgment hook (#66) — *or move to deferred Stream 10*

**Inventory items:** 41–49, 63–66, 103, 106
**Dependencies:** Stream 0, Stream 1 (Loom and Weaver submit ops via Processor; Loom Instance Vertex lives in Core KV), Stream 2 (Weaver targets are Lenses), Stream 3 (Loom and Weaver are actors needing authz)
**Boundary:** Both are *clients* of the Processor. Never write KV directly. Loom owns short utility procedures; Weaver owns convergence over target states.

---

### Stream 5: Services, Commands, Tasks, and Client SDK (UX surface)
**Owns:**
- Service vertex type + DDL (#50)
- Command/Query vertex types + DDL (#51)
- UI Form Schema aspect format (#52)
- Task vertex type + assignedTo link semantics (#53)
- Command discovery API (graph traversal from Identity → discoverable commands) (#54)
- Client SDK: dynamic form renderer (#55), command submission, op tracker subscription
- **Read-Your-Own-Writes overlay implementation** (client side of #105)
- **Developer cold-start kit** / scaffolding CLI for "Hello Lattice" (#108)
- Starlark perf budget + benchmark harness (#107)

**Inventory items:** 50–55, 105 (client side), 107, 108
**Dependencies:** Stream 0, Stream 1, Stream 2, Stream 3
**Boundary:** The entire developer + end-user contract surface.

---

### Stream 6: Privacy & Crypto-Shredding
**Owns:**
- Vault service interface — per-identity key custody (#56)
- `sensitive` field marker in DDL (#57)
- Processor encrypt-on-write / decrypt-on-read middleware *implementation* (interfaces in Stream 1) (#58, #59)
- Key-shred command + propagation event (#60)
- Vault availability strategy + degradation mode (#120)
- **Idempotency horizon expiry surprise** mitigation: client-visible horizon header + business-level natural-key dedupe pattern docs (#119)

**Inventory items:** 56–60, 119, 120
**Dependencies:** Stream 0, Stream 1 (middleware hooks), Stream 2 (shred event triggers row nullification)
**Boundary:** Tight scope, distinct expertise (cryptography, KMS integration, compliance).

---

### Deferred / post-MVP streams

**Stream 7: Closed-loop Operations & Infra-Actuator** *(deferred — was originally part of Observability/Control)*
**Owns:**
- Closed-loop Weaver auditor over Health-KV (#96)
- Auto-circuit-breaker pattern (#100)
- Policy Vertex format (`vtx.infra.<pool_name>`) (#97)
- Infra-Actuator non-Lattice process (#98, #99)

**Why deferred:** For MVP, manual `pause`/`rebuild` via the NATS control endpoints (already in Materializer / Stream 0) is sufficient. The Closed-loop Weaver auditor is mostly value-add only when there's an Infra-Actuator to receive its scaling intents — without it, the loop terminates at a Task creation, which a human could equally well do via a dashboard. **Recommendation: defer the entire Stream 7 as a unit; do not split it.**

**Stream 8: Cells & Sharding** *(deferred — post-MVP)*
**Owns:** items 77–89, plus #109 (cross-cell bridge link rewrite), #110 (cell-spanning openCypher), #114 (per-cell sub-laning), #121 (cross-cell atomic mutations), #124 (multi-tenant cell mapping)

**Stream 9: Edge Lattice** *(deferred — post-MVP)*
**Owns:** Local VAL store, Edge Processor, Sync Manager, Edge Weaver, Vault Proxy, Personal Lens consumer (#74). Depends on Stream 2 having Personal Lens projections working.

**Stream 10: Semantic Contracts (Executable Paper)** *(deferred — post-MVP)*
**Owns:** `vtx.clause` (#63), clause-to-state links (#64), Compliance Weaver mode (#65), AI judgment hook (#66). Builds on Stream 4. *Note: items #63–#66 are listed in Stream 4 above with a "or move here" tag — final placement TBD when we get there.*

---

### Cross-stream architectural ambiguities still open
- **A. Health-as-KV** → RESOLVED. Operational state plane, owned by each component.
- **B. DDL location** → RESOLVED. Core KV, written via `ops.meta.>`.
- **C. Capability Lens hot path** → RESOLVED for MVP (Lens + token-revocation KV at Gateway). v2 vector clock fencing.

### Open items NOT yet placed in any stream
- *None as of this writing — all 125 items are either in an active stream, deferred to a post-MVP stream, or marked resolved/dissolved._

---

## MVP Vertical Slice (Phase 3 sneak peek)

**"Hello Lattice" minimal end-to-end:** one operation submitted → ledger → Processor → Core KV → Refractor (Postgres lens) → query → result.

**Streams strictly required:**
- **Stream 0** (substrate, dev harness, envelope schema, NATS bootstrap) — minimal subset
- **Stream 1** (Processor, KV writers, minimal DDL, one Starlark script, atomic batch) — minimal subset
- **Stream 2** (Refractor: parser + evaluator + Postgres adapter, NO morph delta items, NO Capability Lens, NO RLS) — leverage Materializer as-is

**Streams explicitly NOT required for the slice:**
- Stream 3 (Identity/Auth) → use a hardcoded `Lattice-Actor: root` for the slice
- Stream 4 (Loom/Weaver) → no orchestration needed for one op
- Stream 5 (UX/SDK) → CLI submission is enough
- Stream 6 (Privacy/Crypto) → no sensitive aspects in the slice
- Stream 7 (deferred anyway)
- Streams 8, 9, 10 (deferred anyway)

**This is the smallest possible thing that proves the architecture works end-to-end and validates that the Stream 0 → Stream 1 → Stream 2 critical path is correct.**

---

## Phase 3 — Adversarial Pre-Mortem on the Stream Cut

_Setup: imagine it's October 2026; the 7-stream split failed. What killed it?_

### Stream-by-stream failure modes

**Stream 0 — "The God-Team Trap"**
- Every other stream is blocked waiting for envelope schema decisions; Stream 0 ships substrate in week 8 instead of week 2; Streams 1–6 dead in the water.
- Folding Observability in dilutes focus. Substrate team gets pulled into "what fields go in `asp.sys.health`" debates while Streams 1+2 starve for `lattice-operations` envelope spec.
- Shared test harness is Go-tied (inherited from Materializer). Stream 1 picks Rust for the Processor and the harness can't be reused.
- Health-as-KV "library" becomes a contested standard. Each team wants different fields, different cardinality, different update frequency. Stream 0 plays committee chair instead of building.

**Stream 1 — "The Vault and the Versioning"**
- Vault middleware integration ownership is murky. Stream 1 owns the contract; Stream 6 owns the implementation. The hook point in the commit path becomes a tug-of-war.
- Starlark script versioning (#116) and DDL versioning (#24) are *both* "versioning + cache invalidation." Built independently, then collide.
- **Meta-Gatekeeper (#104) requires identity infrastructure (quorum signers) → chicken-and-egg with Stream 3.** Stream 1 needs the Gatekeeper before bootstrap DDL goes in; Stream 3 needs Stream 1's ops API to write Identity vertices. Whoever blinks builds a hardcoded shim that becomes permanent.
- Atomic-batch size limits (#115) turn out to be unenforceable cleanly because NATS server version drift moves the limit underneath us.

**Stream 2 — "The Morph Delta Was Bigger Than We Said"**
- Materializer→Refractor morph delta estimated at 6 items. Reality: 14+. Specifically:
  - Path Projection / RLS needs a *per-target-store strategy*. Postgres RLS quirks, ES has none, NATS-KV has none. Three implementations.
  - Personal Lens NATS-subject target is a brand-new adapter, not a config change.
  - Self-projecting configuration creates a chicken-and-egg: Refractor needs to know about a lens-of-lenses before any lens is loaded. Solved by hardcoded bootstrap lens — itself a feature.
  - Materializer's openCypher subset (MATCH/OPTIONAL MATCH/RETURN, no WHERE) **probably can't express ReBAC paths.** Multi-hop filtering needs WHERE. Stream 2 must pull v2's WHERE into v1.
- Crypto-shred row nullification handler crosses with Stream 6. Neither team thinks the event schema is theirs.
- "Read-Your-Own-Writes overlay" designed in Stream 2, implemented in Stream 5, integrated nowhere. Ships as "documented but not working."

**Stream 3 — "The Capability Lens That Couldn't"**
- Capability Lens cypher exceeds Materializer's openCypher subset. Blocks Stream 2 to add WHERE, OR rewrites ReBAC, OR builds its own evaluator (worst).
- JWT signing infrastructure was assumed to be "an external IdP or KMS." Nobody picked one. Stream 3 stalls.
- Gateway is a separate deployable nobody staffed. Stream 3 absorbs it and slips 3 weeks.
- "AI actor authority" (#125) is still a placeholder when AI agents become useful in Stream 4 — Stream 4 invents an answer Stream 3 retroactively blesses.

**Stream 4 — "The Three-Engine Trap"**
- Tiered Intelligence (L1 cypher / L2 Starlark / L3 AI) means Weaver depends on **three engines**. Stream 4 stalls when any one slips.
- Loom and Weaver are different mental models even sharing substrate. One team's velocity halves.
- Bootstrap mode and Steady-state mode are two distinct code paths. Built sequentially, the second breaks the first.
- AI Handshake depends on an AI Agent Vertex type that is in nobody's inventory.
- **Idempotency horizon (24h) vs Loom workflows that sleep for weeks** is a real bug — see G5.

**Stream 5 — "The SDK That Everyone Wanted Last"**
- Blocks on Streams 1+2+3 stabilizing. They never simultaneously stabilize; ships against moving targets.
- Dynamic form renderer needs multiple frontend frameworks. Becomes its own sub-stream nobody warned about.
- Developer cold-start kit (#108) requires understanding all of Streams 1+2+3 — tech-writing role with no tech writer.
- Starlark perf benchmark (#107) is here but the *fix* lives in Stream 1. Benchmark says "slow"; Stream 1 says "not our problem."

**Stream 6 — "The Vault Was Never Picked"**
- KMS choice unmade. Stream 6 cannot start.
- Vault interface defined in Stream 1, implemented in Stream 6 → contract drift.
- `KeyShredded` event schema contested between Stream 6 and Stream 2.

### Global / cross-stream risks (the real killers)

| # | Risk | Mitigation |
|---|---|---|
| **G1** | Stream 0 becomes a critical-path bottleneck | Define a "Stream 0 minimum viable substrate" shipping in week 1: envelope schema (frozen), NATS bootstrap, KV naming, dev harness. Defer ALL Observability standards to week 4+. |
| **G2** | Materializer morph is bigger than 6 items | Spawn the Materializer-morph-research subagent **before Stream 2 starts coding**. Lock the morph delta as a numbered list before week 1. |
| **G3** | Capability Lens cypher exceeds Materializer subset | Run a 2-day spike in week 1: write the Capability Lens cypher *as if* Refractor existed. If it needs WHERE → know now. |
| **G4** | Lens-of-Lenses bootstrap problem | Hardcoded bootstrap lens shipping in the Refractor binary. Name and version it explicitly. |
| **G5** | Idempotency horizon (24h) vs Loom workflows that sleep for weeks | Loom-originated ops use their own request-id namespace with extended retention OR Loom Instance Vertex carries its own dedupe state and resubmits as fresh ops with new request-ids. |
| **G6** | DDL changes break in-flight Loom instances | Loom Instance Vertex pins its DDL version. Migration tool warns on in-flight instances before allowing breaking DDL changes. |
| **G7** | Test harness language lock-in | Decide Processor language **in week 0**. Strong recommendation: same language as Materializer (Go) for maximum reuse. |
| **G8** | No one owns developer documentation | Either a Documentation cross-cutting role attached to Stream 5, or every stream owns its own docs and Stream 5 owns the "Hello Lattice" tutorial only. |
| **G9** | Stream 0 building two things at once (substrate + Observability) | Sequence within Stream 0: weeks 1–3 = pure substrate; weeks 4–6 = Observability standards. Don't parallelize within the team. |
| **G10** | Meta-Gatekeeper (#104) chicken-and-egg with Identity (Stream 3) | For MVP, Meta-Gatekeeper is a single root signer key loaded from config. Quorum signing is post-MVP. |

---

## Phase 3 — Boundary Contracts (per stream)

### Stream 0: Substrate + Shared Platform

| | |
|---|---|
| **Owns** | NATS deployment, JetStream + KV provisioning, bucket conventions, subject taxonomy, `lattice-operations` & `lattice-events` stream definitions, lane subjects, `Lattice-Actor` header spec + JWT verification library, operation envelope schema, request-id semantics, idempotency horizon header, dev harness (`make up`), in-memory NATS test harness, mTLS/auth config, Health-as-KV format, heartbeat library, lag metric pipeline, NATS Control Service standard, structured logging library, tracing library, replay tool, schema registry CLI |
| **Consumes from** | Nothing inside Lattice. External: NATS server, KMS for mTLS certs |
| **Publishes** | (1) Operation envelope schema v1 (the most critical contract). (2) Subject taxonomy doc. (3) `Lattice-Actor` header spec + verification library. (4) Health-as-KV aspect schema. (5) NATS Control Service IDL + library. (6) Test harness Go module. (7) Dev harness. |
| **Does NOT own** | Any business logic. Vertex/aspect/link semantics beyond key naming. The Processor. The Refractor. The Vault. Identity vertices (only the actor *header*). Lens definitions. |
| **Stream-blocking handoffs** | Operation envelope schema v1 frozen by end of week 1. Subject taxonomy + KV naming by week 1. Health-as-KV and Control Service standards can wait until week 4. |

### Stream 1: Core (Processor + KV write plane)

| | |
|---|---|
| **Owns** | Vertex/Aspect/Link KV bucket implementations, soft-delete, atomic write primitive, Processor service, Starlark sandbox, Starlark stdlib, script loader/cache, MutationBatch validator, EventList publisher, DDL meta-vertex types + JSON Schema validator, DDL bootstrap migration tool, Index Vertex pattern, idempotency tracker, Meta-Gatekeeper (single-signer for MVP), Context Hinting mechanism, JIT Hydration cache, atomic-batch size validator, Starlark script versioning, Vault middleware integration points, Starlark golden test harness |
| **Consumes from** | Stream 0 (envelope schema, subject taxonomy, JWT library, dev/test harness, NATS Control Service library). Stream 6 (Vault `Encrypt`/`Decrypt` interface — defined here, implemented there). |
| **Publishes** | (1) Core KV key naming + value schemas (vertex, aspect, link JSON shapes) — consumed by Refractor via CDC. (2) `lattice-events` event envelope schema — consumed by Loom/Weaver for orchestration, NOT by Refractor. (3) Vault interface contract. (4) DDL JSON Schema format. (5) Starlark host API documentation. (6) End-to-end runnable substrate. |
| **Does NOT own** | Lens definitions or projection. Authorization decisions (consults Capability Lens via O(1) read). Workflow orchestration. UI form rendering. Vault key custody. Personal Lens streaming. |
| **Stream-blocking handoffs** | `lattice-events` envelope schema frozen end of week 2. Vault interface contract frozen end of week 2. DDL format frozen end of week 3. |

### Stream 2: Refractor (the projection plane)

| | |
|---|---|
| **Owns** | Lens meta-vertex DDL, morphed Materializer codebase, openCypher parser, evaluator, Adjacency KV (Refractor-private), Postgres adapter, NATS KV adapter, ES adapter, Path Projection / RLS link mirroring (per-target-store strategies), Personal Lens NATS-subject target adapter, self-projecting configuration with hardcoded bootstrap lens, Refractor Health KV, backfill/replay engine, pause/resume/rebuild/replay/zero-downtime-migration NATS control endpoints, crypto-shred row-nullification handler, Secure Lens type, 4-tier failure classification, DLQ, retry queue, deferred re-evaluation, Lens schema migration policy, Read-Your-Own-Writes overlay protocol |
| **Consumes from** | Stream 0 (substrate). Stream 1 (Core KV change feed via NATS KV watcher — NOT `lattice-events`; `lattice-events` are intentional business events for orchestration. Also: Vault decryption interface for Secure Lens). Stream 6 (`KeyShredded` event schema). |
| **Publishes** | (1) Lens definition format. (2) Adapter interface. (3) Refractor Health KV format. (4) Refractor NATS Control Service IDL. (5) Read-Your-Own-Writes overlay protocol. (6) Morph delta deliverables list. |
| **Does NOT own** | Anything in Core KV. The Processor. Starlark. Authorization decisions. Personal Lens consumer on the device side. Vault key custody. Permission path semantics — only the cypher rules. |
| **Stream-blocking handoffs** | Capability Lens cypher feasibility spike completes week 1. Adapter interface frozen end of week 4. Personal Lens NATS subject format published end of month 2. |

### Stream 3: Identity, Auth, and Capability

| | |
|---|---|
| **Owns** | Identity vertex type + canonical aspects, ReBAC link-path semantics, Capability Lens definition, Capability Lens reader API, Capability invalidation triggers, Token revocation KV at the Gateway, Gateway service, capability vector clock fence (v2), AI actor authority semantics |
| **Consumes from** | Stream 0 (envelope schema, JWT library, NATS bootstrap). Stream 1 (Processor exists). Stream 2 (Refractor runs Capability Lens; NATS KV adapter is the target). External: IdP/KMS for actor signing keys. |
| **Publishes** | (1) Identity vertex DDL. (2) `Lattice-Actor` JWT format (concrete content). (3) Capability Lens output schema. (4) Token revocation KV schema. (5) Permission path semantics doc. |
| **Does NOT own** | Signing key custody. The Refractor's evaluator. The Processor's auth check call site (Stream 1 owns the call; Stream 3 owns what it returns). End-user UI for auth. |
| **Stream-blocking handoffs** | Permission path semantics doc drafted end of week 2. Capability Lens output schema frozen end of week 4. JWT format frozen end of week 3. |

### Stream 4: Orchestration (Loom + Weaver)

| | |
|---|---|
| **Owns** | Loom (Sensorium, Transition Engine, Actuator, Instance Vertex DDL, utility libraries). Weaver (Sensorium 3-lane work stream, Operational KV, Tiered Evaluator, Strategist playbook registry, Actuator). `vtx.meta.pattern` blueprint format. `vtx.meta.target` declarative target as a Lens. Targeted-audit Weaver. Bootstrap deep-audit mode. AI Handshake protocol. Weaver temporal scheduler. External Adapter framework. Two-Phase Nudge. Operation Vertex pruner |
| **Consumes from** | Stream 0 (substrate). Stream 1 (Processor — Loom and Weaver are clients). Stream 2 (Refractor runs declarative targets; L1 cypher Audits use Refractor). Stream 3 (auth — both are actors). |
| **Publishes** | (1) `vtx.meta.pattern` format. (2) `vtx.meta.target` format. (3) External Adapter SDK. (4) Loom Instance Vertex schema. (5) Weaver Operational KV schema. (6) AI Agent Vertex type. (7) Two-Phase Nudge protocol. |
| **Does NOT own** | Core KV writes (uses Processor). Lens implementations (uses Refractor). Authorization decisions. Vault. UI. The Cron service that doesn't exist (replaces it). |
| **Stream-blocking handoffs** | `vtx.meta.pattern` and `vtx.meta.target` formats drafted end of week 4. External Adapter SDK frozen end of week 6. AI Handshake protocol can wait until month 3. |

### Stream 5: Services, Commands, Tasks, and Client SDK

| | |
|---|---|
| **Owns** | Service / Command / Query / Task vertex types and DDL, UI Form Schema aspect format, Command discovery API, Client SDK, Read-Your-Own-Writes overlay implementation, Developer cold-start kit, Starlark perf benchmark harness, "Hello Lattice" tutorial |
| **Consumes from** | Streams 0, 1, 2, 3. (Stream 4 for orchestration UI.) |
| **Publishes** | (1) Service / Command / Query / Task DDL. (2) UI Form Schema spec. (3) Client SDK API (versioned). (4) Developer scaffolding CLI. (5) "Hello Lattice" reference app. |
| **Does NOT own** | Frontend frameworks consumed. End-user app rendering — only the SDK. Backend business logic. |
| **Stream-blocking handoffs** | Mostly *consumer*. Only blocks Stream 9 (Edge) by needing to publish the SDK API contract by month 2. |

### Stream 6: Privacy & Crypto-Shredding

| | |
|---|---|
| **Owns** | Vault service implementation, `sensitive` field marker in DDL, Processor encrypt/decrypt middleware implementation, `KeyShredded` event schema + emission, key-shred command, Vault availability strategy + degradation mode, idempotency horizon expiry mitigation docs |
| **Consumes from** | Stream 0 (substrate). Stream 1 (middleware hooks; Vault interface contract). External: KMS choice (must be picked in week 1). |
| **Publishes** | (1) Vault interface implementation. (2) `KeyShredded` event schema. (3) `sensitive` field semantics in DDL. (4) Vault degradation mode runbook. |
| **Does NOT own** | The Processor itself. The Refractor's row nullification handler (Stream 2 owns the listener; Stream 6 owns the event). End-user "right to be forgotten" UI flow. |
| **Stream-blocking handoffs** | KMS choice in week 1 (organizational unblock). Vault interface implementation by end of week 6. `KeyShredded` event schema frozen end of week 4. |

---

## Phase 3 — Critical-Path Edges with Unblocking Conditions

| Edge | What must be locked | Owner | Deadline |
|---|---|---|---|
| Stream 0 → Stream 1 | Operation envelope schema v1 | Stream 0 | end of week 1 |
| Stream 0 → Stream 2 | Operation envelope schema v1 + subject taxonomy | Stream 0 | end of week 1 |
| Stream 0 → Stream 3 | `Lattice-Actor` header spec + JWT verification library | Stream 0 | end of week 1 |
| Stream 0 → Stream 6 | Subject taxonomy + KV bucket conventions | Stream 0 | end of week 1 |
| Stream 1 → Stream 2 | Core KV key naming + value schema (vertex, aspect, link JSON shapes) | Stream 1 | end of week 2 |
| Stream 1 → Stream 6 | Vault interface contract | Stream 1 | end of week 2 |
| Stream 1 → Stream 3 | DDL JSON Schema format + bootstrap migration tool | Stream 1 | end of week 3 |
| Stream 3 → Stream 2 | Permission path semantics doc | Stream 3 | end of week 2 |
| Stream 3 → Stream 1 | Capability Lens output schema | Stream 3 | end of week 4 |
| Stream 3 → Stream 5 | JWT format (concrete) | Stream 3 | end of week 3 |
| Stream 2 → Stream 4 | Lens definition format + adapter interface | Stream 2 | end of week 4 |
| Stream 2 → Stream 6 | Acknowledged `KeyShredded` listener contract | Stream 2 | end of week 4 |
| Stream 6 → Stream 2 | `KeyShredded` event schema | Stream 6 | end of week 4 |
| Stream 6 → Stream 1 | Vault interface implementation | Stream 6 | end of week 6 |
| Stream 4 → Stream 5 | `vtx.meta.pattern` and `vtx.meta.target` formats | Stream 4 | end of week 4 |
| Streams 1+2+3 → Stream 5 | Stable APIs (continuous) | n/a | continuous |

**Key insight:** Weeks 1–4 are **schema-locking weeks**, not coding weeks. Almost every blocker is a *contract*, not an *implementation*. If teams treat weeks 1–4 as design sprints where the artifact is a frozen JSON schema or markdown spec, all six MVP streams can start parallel coding by week 5.

---

## Post-Phase 3 Corrections (from Materializer morph research + Andrew clarifications, 2026-04-09)

### Architectural decisions resolved

1. **Refractor consumes Core KV change feed (CDC via NATS KV watcher), NOT `lattice-events`.** `lattice-events` are intentional business events (e.g., `LeaseSigned`) for orchestration (Loom/Weaver). Core KV CDC is what Materializer already does — no morph needed. The Phase 3 boundary contract for Stream 2 and the critical-path table have been updated above to reflect this.
2. **Link data lives in the JSON value, not only the key.** Lattice KV entries (vertices, aspects, links) all carry their data in the document body. Keys are addressing conventions. Links carry `{nodeId, otherNodeId, name, direction, isDeleted}` in the value — same shape as Materializer. The morph agent's finding of "Core KV event-shape mismatch" is largely dissolved.
3. **Vertices and aspects under separate keys is already compatible.** Materializer stores aspects under node-prefix-keyed entries. Same pattern as Lattice (`asp.<vtxId>.<name>`). Not a morph item.
4. **Parser strategy: import open-source Go openCypher parser, NOT ANTLR4.** Avoids Java toolchain in CI. Candidate: `github.com/jtejido/go-opencypher` (needs evaluation).
5. **Capability Lens likely needs no WHERE clause** — just structural path traversal. Spike in Stream 2 week 1 to confirm. If true, v1 hand-rolled parser ships Capability Lens today with zero changes.
6. **Variable-length paths not required for MVP.**
7. **Personal Lens target: NATS stream** with subject `lattice.sync.user.<user-id>`. Payload is a hint/pointer; actual data in multi-tenant master projection. Subject to further design.
8. **Lens-of-lenses: durable consumer on Core KV `vtx.meta.lens.>` meta vertices.** No separate stream needed. Hardcoded bootstrap lens in Go binary for MVP.

### Morph plan verdict update

**Original estimate: MEDIUM-LARGE (5–8 weeks).** After corrections: **MEDIUM (4–7 weeks)** for one engineer, all delta items. But the MVP vertical slice ships in ~1 week (fork + rename + bucket adaptation + hardcoded bootstrap lens).

Two of the four "new morph delta items" the agent found are dissolved or downgraded:
- §3.1 (Core KV event-shape mismatch) → downgraded from BLOCKER to minor adaptation (S)
- §3.4 (lattice-events envelope plumbing) → DISSOLVED entirely

Two remain:
- §3.2 (subject taxonomy rename) → S, ~2 hours
- §3.3 (control plane idiom migration to NATS Services framework) → M, post-MVP slice

### Key findings from the morph agent that stand

1. **Materializer's parser is hand-rolled, NOT ANTLR4** as the architecture document claims. The `grammar/Cypher.g4` file exists but isn't imported. WHERE support is real work if needed — but the open-source parser import strategy may eliminate this.
2. **Multi-hop fixed-length traversal already works.** The spike question (`MATCH (i:Identity)-[:memberOf]->(r:Role)-[:canExecute]->(c:Command)`) evaluates correctly today with no changes. Breaking point is predicates, not hops.
3. **~80% of Refractor needs are already shipped** in Materializer: 4-tier failure model, hot-reload, zero-downtime migration, health KV, lag metrics, audit stream, DLQ, retry queue, deferred re-evaluation, adjacency KV with CAS + live-event watch, pull-consumer pause, in-memory NATS test harness.

### Full morph plan location

`_bmad-output/planning-artifacts/materializer-morph-plan.md` — contains 10 sections with verdicts on all 10 delta items, recommended morph sequence, risk table, and evidence index.

