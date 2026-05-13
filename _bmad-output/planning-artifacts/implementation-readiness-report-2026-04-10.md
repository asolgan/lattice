---
stepsCompleted: [1, 2, 3, 4, 5, 6]
workflowStatus: complete
documentsAssessed:
  prd: "_bmad-output/planning-artifacts/prd.md"
  architecture: "_bmad-output/planning-artifacts/lattice-architecture.md"
  epics: null
  ux: null
date: "2026-04-10"
project: "Lattice"
---

# Implementation Readiness Assessment Report

**Date:** 2026-04-10
**Project:** Lattice

## Document Inventory

### PRD Documents
**Whole Documents:**
- `prd.md` (79K, modified 2026-04-10) — complete, 12/12 steps, workflow status: complete

**Sharded Documents:** None

### Architecture Documents
**Whole Documents:**
- `lattice-architecture.md` (62K, modified 2026-04-10)

**Sharded Documents:** None

**Additional Context Documents:**
- `materializer-morph-plan.md` (46K, modified 2026-04-09) — morph plan for Materializer → Refractor; referenced in PRD as brownfield foundation

### Epics & Stories Documents
**Not found** — no epic or story documents exist yet. Epics and stories are a planned next step.

### UX Design Documents
**Not found** — no UX design documents exist yet. UX design is a planned next step.

---

## PRD Analysis

### Functional Requirements Extracted

**Total FRs: 58** across 8 capability areas.

| # | FR | Capability Area |
|---|-----|----------------|
| FR1 | Staff can create an unclaimed identity record for a prospect or leaseholder without requiring the person to have an active account | Identity |
| FR2 | A resident or member can self-register and bind their credentials to an existing unclaimed identity record using matching claim keys | Identity |
| FR3 | System detects potential duplicate identity records via fuzzy matching; detection presented for human review, never resolved automatically | Identity |
| FR4 | Staff can review duplicate identity candidates and approve a merge; merges cannot occur without explicit staff confirmation | Identity |
| FR5 | A leaseholder with a grandfathered account can claim their existing identity vertex and immediately access full account history | Identity |
| FR6 | An identity record exists in trackable states: unclaimed, claimed, flagged-for-review, merged | Identity |
| FR7 | A member's identity and full interaction history persist after a lease or membership ends | Identity |
| FR8 | All state mutations submitted through a single validated write path; no direct writes to core data store possible from outside the platform | Write Path |
| FR9 | Business rules expressed as deterministic scripts with no access to external I/O, network, secrets, or non-deterministic state | Write Path |
| FR10 | New entity types, business rules, and projection definitions can be authored and activated without redeployment | Write Path |
| FR11 | Every submitted operation produces an immutable, ordered ledger entry with author identity, timestamp, and full payload | Write Path |
| FR12 | Operations are idempotent; resubmitting produces the same outcome without duplicate or conflicting state | Write Path |
| FR13 | Multiple related state changes can be submitted as a single operation that commits entirely or fails entirely | Write Path |
| FR14 | An actor can confirm a submitted operation has been durably committed before reading dependent projections | Write Path |
| FR15 | Business state can be projected into query-optimized external targets via configurable projection definitions | Query & Projection |
| FR16 | Projection definitions can be created, modified, and activated as platform operations without redeployment | Query & Projection |
| FR17 | Front-of-house staff can query pre-computed member context through a role-scoped projection surface | Query & Projection |
| FR18 | Back-of-house operators can query operational projections (occupancy, rent roll, payment status, maintenance SLA) through a role-scoped surface | Query & Projection |
| FR19 | A Lattice-aware AI agent can traverse from any identity vertex to available commands, input schemas, and plain-language field descriptions without hardcoded knowledge | Query & Projection |
| FR20 | Projection lag between committed operation and query surface appearance is bounded and observable | Query & Projection |
| FR21 | Permission relationships are derived from graph structure and used to authorize every operation in real time | Access Control |
| FR22 | A permission denial response specifies exact permissions required, actor's current role, and available escalation or routing paths | Access Control |
| FR23 | Auth failures are traceable across three planes: graph permission path, projection definition, cached permission check | Access Control |
| FR24 | Platform operators can define and assign role-scoped access for all actor types | Access Control |
| FR25 | Operators can audit which actors hold which permissions at any point in time | Access Control |
| FR26 | Multi-step business processes can be defined as workflows with conditional branching and human approval gates | Workflow |
| FR27 | Platform can enforce convergence targets and automatically assign remediation tasks when actual state diverges | Workflow |
| FR28 | Tasks can be assigned to a specific actor or role-based queue with fallback when primary assignee is unavailable | Workflow |
| FR29 | Unrouted tasks surface in operational health monitoring and are never silently dropped | Workflow |
| FR30 | Operators can view, modify, and revoke all active convergence targets from a management surface | Workflow |
| FR31 | An operator can describe a desired capability in natural language and receive proposed entity type definition, business rule script, and projection definition | AI Interaction |
| FR32 | A proposed capability bundle is reviewed and approved via a task-based workflow before activation | AI Interaction |
| FR33 | An AI agent's pending intent is persisted in the graph between sessions | AI Interaction |
| FR34 | A Lattice-aware AI agent can submit validated intent through the standard write path with the same safety guarantees as human-submitted operations | AI Interaction |
| FR35 | Operators can view all AI-authored capability changes (author, timestamp, approver); governance surfaces accessible alongside operational health | AI Interaction |
| FR36 | Capability authorship governance surfaces are accessible from the same surface as operational health state | AI Interaction / Privacy |
| FR37 | Personal data fields can be individually encrypted; encryption keys can be shredded to render fields irrecoverable without affecting other fields | Privacy |
| FR38 | Non-personal fields (decision outcomes, denial reasons, criteria) are retained after key shredding; audit record remains intact | Privacy |
| FR39 | Erasure of an identity with active financial obligations requires explicit operator override | Privacy |
| FR40 | Payment records store transaction references, status codes, and amounts; raw payment credentials are never stored | Privacy |
| FR41 | Data retention policy per data type is configurable by the operator | Privacy |
| FR42 | Complete, ordered, immutable operation ledger can be mirrored to external long-term retention store without platform changes | Privacy |
| FR43 | A developer can boot a complete local platform environment from a single command | Developer Experience |
| FR44 | A developer can complete a minimal working vertical slice in under 60 minutes from a fresh clone | Developer Experience |
| FR45 | A CLI tool allows developers and operators to submit operations, inspect graph state, and query projection surfaces | Developer Experience |
| FR46 | Platform operational health is readable from a dedicated health data store separate from the business state store | Developer Experience |
| FR47 | A developer/operator console surfaces AI-suggested capability changes as human-review tasks alongside real-time operational health | Developer Experience |
| FR48 | Platform deployments are isolated at the infrastructure level; each operator deployment maintains its own independent data and event streams | Developer Experience |
| FR49 | The platform can notify actors of state changes, assigned tasks, or time-sensitive events relevant to them | Developer Experience |
| FR50 | An actor can resume a previously interrupted AI interaction with prior context and preferences without re-stating them | AI Interaction |
| FR51 | Operators can query historical operational state across a configurable time range | Query & Projection |
| FR52 | Platform automatically emits health signals to a dedicated observability store | Developer Experience |
| FR53 | An operator can revert any capability change by submitting a compensating operation without platform downtime | AI Interaction |
| FR54 | A Lattice-aware AI agent can detect and flag data quality anomalies encountered during graph traversal | AI Interaction |
| FR55 | Platform includes a canonical reference implementation serving as integration test suite, developer onboarding, and demonstrable vertical slice | Developer Experience |
| FR56 | Actor is authorized to complete an operation associated with a task assigned to them; manager authorized via reporting-relationship links | Access Control |
| FR57 | Each data type definition declares which operation types are permitted to mutate it; platform enforces write-scope constraint on every operation | Write Path |
| FR58 | External operations initiated by orchestration are idempotent; failed/retried external calls cannot produce duplicate charges or actions | Workflow |

### Non-Functional Requirements Extracted

**7 NFR categories, 43 distinct criteria:**

**Performance (8 targets):** Write throughput 10–100 ops/sec; Core KV up to 100K keys; CDC lag < 500ms p99; Starlark execution < 100ms p99; End-to-end latency < 2s p99; Commit confirmation within CDC lag window; Dev boot < 3 min first run / < 30s warm restart; Onboarding < 60 min.

**Reliability (6):** Crash-recoverable commit path (10-step fault injection); Refractor resume from last offset; Unrouted tasks never silently dropped; AI degradation produces explicit caveats; Append-only ledger; Single NATS server acceptable for Phase 1.

**Security (11):** Encryption at rest and in transit; Capability Lens sole auth boundary (4-category bypass test suite); Starlark sandbox (4-vector adversarial test suite); Signed JWT/Lattice-Actor; Per-identity PII encryption in external KMS; Specific auth denial responses; Permission revocation within 500ms p99; GDPR/CCPA crypto-shredding; PCI DSS out of scope; AI actor authority via identity vertex + Capability Lens; External operation auditability before call executes.

**Data Integrity (5):** Atomic multi-key commit/fail; Idempotency with duplicate short-circuit; Immutable append-only ledger; Revision conditions with retry (count TBD); DDL schema violations rejected at write path.

**Scalability (4):** Phase 1: 100K keys / 10–100 ops/sec / ~500 members / ~50 concurrent sessions; Cell-agnostic key design validated Phase 1; Postgres Lens unshareded at Phase 1; Operator isolation via independent NATS clusters.

**Evolvability (4):** New capabilities active within CDC lag window without restart; Change propagation within same lag window; Rollback via compensating operation; Deterministic replay for local unit testing.

**Operational Observability (5):** Health signals updated ≤ 10s; Health readable without Refractor; Health signals include 5 specified metrics; 3-plane auth failure trace; Phase 1 fault injection test harness.

### Additional Requirements & Constraints

- **5 Phase 1 completion gates:** Starlark spike result, attempted bypass test suite, Capability Lens 4 attack vectors, compensating operation / DDL rollback test, "Hello Lattice" onboarding verified by external tester
- **7 integration dependencies** (with degradation posture): NATS (hard), Postgres (integration), Payment processor (integration), KMS/HSM (integration), External IdP (integration), Reverse proxy (deployment), BigQuery (Phase 2 optional)
- **5 open decisions:** Beachhead vertical, subscription/pricing, notification delivery model, revision conflict retry count, Edge Lattice phasing
- **API surface:** No public REST/gRPC at Phase 1; NATS-native + CLI only; Gateway deferred to Phase 2
- **Multi-tenancy:** Deployment-level isolation at Phase 1; cell-level at Phase 3

### PRD Completeness Assessment

The PRD is comprehensive and well-structured. Key strengths:
- All 58 FRs are testable and implementation-agnostic
- NFRs are specific and measurable with quantitative targets
- 7 user journeys provide clear traceability from user need to capability
- Phase 1 completion gates provide unambiguous "done" criteria
- Open decisions are explicitly logged — no hidden assumptions

Gaps requiring resolution before epics can be written:
- Notification delivery model (FR49) — push/in-app/email by actor type not specified
- Revision conflict retry count — flagged as architecture decision, not yet resolved
- Beachhead vertical — required before Phase 2 epics

---

## Epic Coverage Validation

### Coverage Status

**Epics and stories document does not exist.** This is an expected state — the PRD was completed on 2026-04-10 and epic creation is the next planned workflow step.

### FR Coverage Matrix

All 58 FRs are currently **uncovered** — no epic document exists to validate against.

| Status | Count |
|--------|-------|
| ✅ Covered in epics | 0 |
| ❌ Not yet in epics | 58 |
| **Coverage %** | **0%** |

### Interpretation

0% epic coverage is **not a readiness failure** at this stage — it is the expected pre-epic state. The readiness assessment will instead validate:
- Whether the PRD is complete enough to write epics from (Step 4+)
- Whether the architecture document covers all PRD requirements
- Whether there are any PRD gaps that would block epic writing

---

## UX Alignment Assessment

### UX Document Status

**Not found.** No UX design document exists.

### UX Implied Assessment

UX/UI is **strongly implied** by the PRD. The following user-facing surfaces are explicitly required:

| Surface | FRs | Phase |
|---------|-----|-------|
| Resident / member app (AI concierge, lease management, notifications) | FR1–FR7, FR33, FR49, FR50 | Phase 2 (Loftspace) |
| Front-of-house staff surface (pre-computed resident context, maintenance, ticketing) | FR17, FR22, FR28 | Phase 2 |
| Back-of-house dashboard (rent roll, occupancy, lease applications, reporting) | FR18, FR25, FR51 | Phase 2 |
| Developer/operator console (AI-suggested capabilities, health monitoring, governance) | FR35, FR46, FR47, FR52 | Phase 2 |
| CLI tool (operations submission, graph inspection, projection queries) | FR45 | Phase 1 |

### UX Warnings

**⚠️ WARNING — Notification delivery model undefined (FR49)**
FR49 states the platform can notify actors of state changes and assigned tasks. The PRD does not specify the delivery mechanism (push notification, in-app, email) by actor type. UX design cannot proceed for notification flows without this decision.

**⚠️ WARNING — Console UX interaction model undefined**
FR47 (developer/operator console) describes what surfaces are needed but not the interaction model. Given the AI-authorship loop is the primary console use case, UX design must resolve: chat interface, structured form, diff-review view, or hybrid. This is a non-trivial UX design question.

**ℹ️ INFO — Phase 1 is CLI-only**
No browser UI exists at Phase 1. UX design is a Phase 2 prerequisite and can begin in parallel with Phase 1 implementation. No UX blocker for Phase 1 epic writing.

**ℹ️ INFO — AI interaction per persona is well-specified**
The PRD frontmatter defines three distinct AI interaction models: AI-as-primary-interface (consumer), embedded AI (front-of-house), directed AI (back-of-house). This provides clear UX design starting points when the design workflow begins.

---

## Epic Quality Review

### Status

**Epics do not exist** — this step cannot be executed. No quality violations can be found or documented.

### Pre-Epic Quality Guidance

When epics are created (via `bmad-create-epics-and-stories`), the following PRD-specific quality criteria apply:

**Phase 1 epic structure must:**
- Stream 0 Epic: Starlark execution spike must be the **first story** — it is a completion gate for all downstream stream design
- "Hello Lattice" vertical slice must be a standalone epic with a single completion test: developer completes it in < 60 minutes from `git clone`
- No epic may claim Phase 1 completion without the 5 stated completion gates all passing

**Epic independence risks to watch:**
- Stream 2 (Refractor) depends on Stream 1 (Processor commit path finalized) — not a violation, but must be sequenced
- Capability Lens adversarial test suite spans Stream 2 and Stream 3 semantics — joint ownership must be explicit in stories
- Identity (Stream 3) ClaimIdentity read amplification spike must produce a result before claim-key index design stories are written

**User value framing guidance:**
- Stream 0–1 stories are infrastructure — frame them as "Developer/operator can [verify capability]" not "Implement [component]"
- Example: NOT "Build Starlark sandbox" — YES "Developer can submit a deterministic business rule that the platform enforces without I/O access"

---

## Summary and Recommendations

### Overall Readiness Status

## ✅ READY FOR PHASE 1 EPIC CREATION

The PRD is complete, well-structured, and sufficiently detailed to begin Phase 1 epic and story writing. Architecture alignment check is recommended before Phase 2 epic writing begins.

### Findings Summary

| Category | Issues | Severity |
|----------|--------|----------|
| PRD completeness | 3 open decisions block Phase 2 (not Phase 1) | 🟡 Minor |
| Epic coverage | 0% — expected pre-epic state | ℹ️ Info |
| UX alignment | No UX doc; 2 warnings (notification model, console UX) | 🟡 Minor |
| Architecture alignment | Architecture predates PRD — 4 new requirements need validation | 🟠 Major |
| Epic quality | Cannot assess — no epics yet | ℹ️ Info |

### Critical Issues Requiring Immediate Action

None that block Phase 1 epic writing.

### Issues Requiring Action Before Phase 2 Epics

**🟠 Architecture alignment (before Phase 2 epics)**
`lattice-architecture.md` predates the PRD. Four new PRD requirements were added during this workflow that must be validated in the architecture doc:
- FR56 — Task-based authorization (authorization at assignment time + manager delegation via reporting links)
- FR57 — Write-scope per DDL (each data type declares permitted mutation types)
- FR58 — Two-Phase Nudge / external operation idempotency (orchestration claims task before external call)
- Security NFR — AI actor authority model (AI agents as identity vertices, no special actor class)

**🟡 Open decisions (before Phase 2 epics)**
1. Beachhead vertical: Loftspace / The Campus / Membership Club → **Loftspace recommended**
2. Subscription / pricing model (startup path)
3. Notification delivery model: push / in-app / email by actor type → **needed for FR49 UX design**

**🟡 UX design (before Phase 2 implementation)**
- Console UX interaction model (chat vs. structured form vs. diff-review vs. hybrid)
- Notification delivery model decision feeds directly into UX flows

### Recommended Next Steps

1. **Run `bmad-create-epics-and-stories`** — PRD is ready; begin with Phase 1 (Streams 0–3 partial). Starlark spike is story #1 in Stream 0 epic.
2. **Run `bmad-create-architecture`** — validate `lattice-architecture.md` covers FR56, FR57, FR58, AI actor authority NFR; document the 4 architectural items parked from the PRD session (task-based auth paths, write-scope per DDL, encrypted aspect projection, aspect-level sensitivity boundary).
3. **Decide beachhead vertical** — score Loftspace / Campus / Membership Club before Phase 2 epic writing begins.
4. **Decide notification delivery model** — unblocks FR49 UX design.
5. **Begin UX design workflow** (`bmad-create-ux-design`) — can run in parallel with Phase 1 implementation; targets Phase 2 surfaces.

### Final Note

This assessment identified **7 issues** across **4 categories**. Zero issues block Phase 1 epic creation. Four issues (architecture alignment + 3 open decisions) should be resolved before Phase 2 epics are written. The PRD is production-quality: 58 testable FRs, 43 measurable NFR criteria, 7 traced user journeys, explicit Phase 1 completion gates, and an implementer reading guide.

**Assessment completed:** 2026-04-10
**Assessor:** BMAD Implementation Readiness Workflow v6.2.1
