---
stepsCompleted: [1, 2, "2b", "2c", 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]
workflowStatus: complete
completedDate: "2026-04-10"
inputDocuments:
  - "_bmad-output/brainstorming/brainstorming-session-2026-04-08.md"
  - "_bmad-output/planning-artifacts/lattice-architecture.md"
  - "_bmad-output/planning-artifacts/materializer-morph-plan.md"
workflowType: 'prd'
classification:
  projectType: "graph-native data OS + Lattice-native reference application"
  domain: "data infrastructure platform — place-based community with multiple services and persistent member identity"
  complexity: high
  projectContext: brownfield
  primaryFraming: startup
  futureOptionality: ["portfolio", "startup", "open-source"]
  coreDifferentiation: "One coherent VAL model where identity, rules, events, projections, and auth are the same abstraction — not bolted-together components"
  referenceApp:
    phase1: "Hello Lattice — domain-agnostic minimal slice (one entity, one rule, one Lens, one AI query). Functions as demo + integration test fixture + onboarding template."
    phase2Verticals: ["Loftspace (fictional single-building live/work ~200 units)", "The Campus (mixed live/work community)", "Membership Club (lifestyle/community, Soho House model)"]
    beachheadDecision: "TBD — to be scored and decided in product vision step"
    tripleRole: "demo + integration test fixture + onboarding template"
  aiNativeMandate:
    - "AI as graph navigator (identity → commands → schemas, no SDK needed)"
    - "Graph as prompt context (links replace RAG guessing)"
    - "Intent submission with deterministic guardrails (Starlark safety sandbox)"
    - "AI as platform author (self-improvement loop via write path, no redeployment)"
    - "AI authorship governance: review process and rollback mechanism needed (PRD section required)"
  aiInteractionByPersona:
    consumer: "AI-as-primary-interface (resident talks to AI assistant)"
    frontOfHouse: "Embedded AI (surfaces context before staff asks)"
    backOfHouse: "Directed AI (user directs AI toward convergence targets)"
  endUsers:
    - "Resident/member (consumer)"
    - "Front-of-house staff (concierge, maintenance, valet)"
    - "Back-of-house staff (operations, c-suite)"
    - "AI agent (fourth user — requires AI-legible auth errors, clean traversal observability)"
  capabilityTiers:
    T1: "Single entity CRUD"
    T2: "Multi-entity relationships + rules"
    T3: "Projections + queries"
    T4: "AI traversal + self-improvement"
  prdNarrativeStructure: "Demand-first — every epic opens with a user need, maps to Lattice capability"
  edgeLatticePhasing: "TBD — does Phase 2 require Edge Lattice or can it be built on Core Lattice first?"
  naming:
    internalCodeName: "Lattice"
    publicBrandName: "TBD — separate workstream, deferred. 'Lattice' and variations are taken in public domain."
  vision:
    statement: "Lattice is the operating system for experience businesses — a graph-native data platform that makes the 'spin up a new idea' promise real, by making good architecture impossible to violate and AI authorship safe by design."
    oneLiner: "The first platform where adding a new capability — or changing it three months later — doesn't require a meeting. Just an intent, a review, and it's live."
    coreDifferentiator: "Platform discipline enforced architecturally, not culturally. Sole writer rule, DDL validation, Starlark sandbox — the trust architecture that makes AI authorship enterprise-safe."
    whyNow: "Specific stack maturity (NATS 2.12 atomic batches, go.starlark.net, AI agents that can author platform capabilities through an API) + cultural shift toward collapsed PM-to-deployment pipelines."
    honestPositioning: "Meetings replaced by intent + review, not intent alone. Judgment stays human. Coordination overhead disappears."
    moat: "Living systems — not fast deployment. A capability can be authored, changed, and evolved without a meeting, ever."
    marketCategory: "Experience businesses (place-based communities with multiple services and persistent member identity)"
---

# Product Requirements Document - Lattice

**Author:** Andrew
**Date:** 2026-04-10

> **Open decisions (must resolve before Phase 2 epics):**
> - Beachhead vertical: Loftspace / The Campus / Membership Club (Loftspace recommended)
> - Subscription / pricing model (startup path)
> - Notification delivery model: push / in-app / email — by actor type (open UX decision)
> - Revision conflict retry count (architecture decision)
> - Edge Lattice phasing: does Phase 2 require Edge Lattice or Core Lattice first?
>
> **Implementer reading guide:** Executive Summary → Product Scope → Journey 0 + Journey 6 → Project Scoping & Phased Development → FR8–FR14 (write path contract). That is the critical path mental model. Everything else is detail.

## Executive Summary

**Lattice** *(internal code name; public brand TBD)* is a graph-native data operating system for **experience businesses** — organizations that manage place-based communities with multiple service lines and a persistent member identity crossing all of them. Think residential buildings with coworking, events, hospitality, and retail; boutique hotel groups; lifestyle membership clubs; student housing campuses. The defining characteristic of this market is that a single person (resident, member, guest) has a relationship with the business that transcends any individual transaction — and current technology systematically fails to honor that.

The platform stores all business and meta-domain state as **vertices, aspects, and links** in a single graph (the VAL model), processes every mutation through a deterministic rules engine (Starlark), projects query surfaces through configurable Lenses, and orchestrates workflows via event-driven Loom/Weaver components — all on a NATS JetStream substrate. New capabilities (vertex types, business rules, Lens definitions, workflows) are authored as data operations submitted through the same write path as business transactions, making the platform self-extending without redeployment.

**Target users:**
- **Operators** (back-of-house): property managers, GMs, c-suite — need new business ideas to appear without engineering sprints
- **Staff** (front-of-house): concierge, maintenance, valet — need full resident context surfaced before they ask
- **Members/residents** (consumer): need identity and history honored across every service line
- **AI agents**: navigate the graph as a first-class consumer — command discovery, context hydration, and safe intent submission by design

**The problem being solved:** The "easy to spin up a new business idea" promise has been made and broken before — not for lack of talent, but for lack of architectural discipline. When the write path can be bypassed, when projections must be hardcoded, when business rules live in application code, eighty engineers produce eighty different interpretations of the same intent. Lattice makes the discipline architecturally impossible to violate.

### What Makes This Special

**One coherent model, not bolted-together tools.** Identity, rules, events, projections, and auth are all expressions of the same VAL abstraction — not Neo4j + Kafka + OPA wired together. This is a design philosophy enforced by structure, not process.

**AI authorship, safe by design.** A non-technical operator describes intent. A Lattice-aware AI authors the vertex DDL, Starlark rule, and Lens definition, submits them through the write path, and the capability exists — after human review, without a PR or a meeting. The Starlark sandbox, DDL validation, and sole-writer rule are the trust architecture that makes this enterprise-safe, not just technically possible.

**A living system, not a deployment tool.** The moat is not fast first deployment — it is that the same capability can be changed, evolved, and extended three months later with the same zero-meeting workflow. Every iteration is as safe as the first.

**AI interaction per persona:**
- Consumer (resident/member): AI-as-primary-interface — the member talks to an AI concierge that navigates the graph on their behalf
- Front-of-house staff: embedded AI — context surfaced before the staff member asks, powered by Lens projections
- Back-of-house staff: directed AI — operator describes a convergence target in natural language; Weaver operationalizes it

**Why 2026:** For the first time, a single person or a small team can hold a complex business and technical vision in their head — and actually see it through to production. AI agents can execute at the implementation level; humans provide architectural intent and judgment. The coordination bottleneck that killed platform ambitions at scale (the 80-engineer team that couldn't agree on how to build) is no longer the price of complexity. The cultural shift — organizations collapsing the PM-to-deployment pipeline into leaner, AI-augmented teams — is already underway. Lattice is the infrastructure layer that makes that collapse safe and repeatable.

**Honest positioning:** Meetings are replaced by intent + review, not intent alone. Human judgment is preserved. Coordination overhead is eliminated.

## Project Classification

| Dimension | Value |
|-----------|-------|
| **Project Type** | Graph-native data OS (platform layer) + Lattice-native reference application |
| **Market Category** | Experience businesses (place-based communities with multiple services and persistent member identity) |
| **Domain Complexity** | High — novel architecture, AI-first design mandate, security-critical projections, privacy-critical shredding |
| **Project Context** | Brownfield — architecture fully defined (`lattice-architecture.md`); PRD defines product scope, domain semantics, user scenarios, quantitative targets |
| **Primary Framing** | Startup (sharpest scope); portfolio and open-source divergences noted where relevant |
| **Foundation** | Materializer codebase morph (~80% reuse); Go + NATS + Postgres; single-cell MVP |
| **Reference App** | Phase 1: "Hello Lattice" (minimal, domain-agnostic). Phase 2: Loftspace / The Campus / Membership Club (beachhead vertical TBD) |
| **Naming** | "Lattice" = internal code name only; public brand is a separate workstream, deferred |

## Success Criteria

### User Success

**Developer/Operator (unified role — AI-augmented human-in-the-loop):**
- Describes intent to a Lattice-aware AI → receives proposed DDL, Starlark script, and Lens definition → approves via a task assigned by Lattice → capability is live in the running system. Full loop completed in under one working day for a non-trivial capability, without writing code directly.
- "Hello Lattice" onboarding: developer clones repo, runs `make up`, completes the minimal vertical slice (one entity → one rule → one Lens → one AI query) in under 60 minutes.
- Phase 2 (Lattice-native developer console): AI-suggested changes surface as human-review tasks inside the console. Health and observability state is visible in the same surface — business state via Lens projections, operational health via direct Health KV reads.

**Staff (front-of-house):**
- Full resident/member context visible in a single surface before the interaction begins. Zero app-switching required for common workflows (check-in, maintenance request, package pickup, visitor management).

**Member/Resident (consumer):**
- Identity and interaction history present across every service line without re-introduction. A lease ending does not erase the resident's relationship with the building.
- AI concierge can answer questions about the resident's current state by graph traversal — no backend query required to be written.

### Business Success

| Future Path | Success Definition |
|------------|-------------------|
| **Portfolio** | Architecture doc + working "Hello Lattice" vertical slice + one demonstrable AI-authorship loop (operator describes → AI proposes → human approves → live) |
| **Startup** | First paying operator running a real property on the Loftspace reference app; uses the console to add one net-new capability without filing an engineering ticket |
| **Open Source** | Developer clones repo, completes "Hello Lattice" in < 60 minutes, forks Loftspace; community submits first external Starlark script contribution |

### Technical Success

| Metric | Target | Notes |
|--------|--------|-------|
| **Write throughput** | 10–100 ops/sec sustained | Single-cell MVP; single-building Loftspace scale |
| **Core KV size** | Up to 100K keys | Vertices + aspects + links combined |
| **CDC-to-projection lag** | < 500ms p99 | Capability Lens and general Lenses at MVP scale |
| **Starlark execution** | < 100ms per operation | Stream 1 concern — spike required before assuming |
| **End-to-end latency** | < 2s p99 | Op submission → projection visible in Lens target |
| **"Hello Lattice" onboarding** | < 60 minutes | From `git clone` to working vertical slice |

### Measurable Outcomes

- Processor commit path is fully idempotent and crash-recoverable (validated by fault injection tests)
- Capability Lens projection lag < 500ms p99 (auth correctness depends on this)
- A new vertex DDL + Starlark rule + Lens definition can be added to a running system without redeployment
- AI agent can navigate from identity vertex to available commands to schema to submitted intent — completing a round-trip — using only graph traversal, no hardcoded API knowledge

## Product Scope

### MVP — Minimum Viable Product

Streams 0–3 partial:
- **Stream 0 (Substrate):** NATS spike stories resolved, operation envelope schema, NanoID, dev harness (`make up`)
- **Stream 1 (Core):** Processor 10-step commit path, Starlark sandbox, DDL meta-vertices, atomic batch, idempotency
- **Stream 2 (Refractor):** Materializer fork, durable consumer per Lens, Postgres adapter, Capability Lens
- **Stream 3 partial (Identity — spec and contract):** Identity vertex type, JWT/Lattice-Actor header spec and contract, Capability KV shape. *Gateway implementation (NGINX + thin Go service) deferred to Phase 2 — no HTTP clients exist at Phase 1.*
- **Reference:** "Hello Lattice" CLI — one resident vertex, one lease aspect, one payment rule, one Lens projection, one AI traversal query
- **Note:** Health and observability via direct Health KV reads and NATS CLI; no console UI at MVP

*Detailed phase gates, MVP completion criteria, and risk mitigation strategy: see Project Scoping & Phased Development.*

### Growth Features — Phase 2

- **Streams 4–6:** Loom/Weaver (orchestration), Services/SDK (command/task vertex types), Privacy (crypto-shredding, Vault)
- **Lattice-native developer/admin console:** AI-suggested capabilities surface as human-review task vertices; business state via Lens projections; operational health via Health KV direct reads
- **Loftspace showcase vertical:** Full single-building residential + coworking + event space + café — exercises all 8 domain properties
- **Beachhead vertical decision:** Loftspace / The Campus / Membership Club — to be scored before Phase 2 epics are written

### Vision — Future

- Edge Lattice (personal graph that travels with the member)
- Multi-cell / sharding (Stream 8)
- Additional showcase verticals (Campus, Membership Club)
- Public brand name and go-to-market
- Open-source community governance model
- Managed NATS (Synadia Cloud) as a deployment option
- **Semantic contracts:** Lease clauses, compliance rules, and fair housing criteria as executable `vtx.clause` vertex types with compliance Weaver mode and AI judgment hooks for discretion-requiring decisions

## User Journeys

> *All journeys use Loftspace as the reference domain — a single mixed-use residential building with ~200 units, coworking space, event venue, and café. "Lattice" = internal code name throughout.*

---

### Journey 0 — Maya + Marcus + Robert: "Finding Your Account"

*The foundation journey — establishes persistent identity. Three paths, one hard constraint: one human, one identity vertex.*

**Path A — Clean path (prospect → self-registers, system finds them):**
Marcus applied online two weeks before his lease was signed. He created an account with his email and phone. The system matched his email against the leasing agent's prospect record (entered correctly) — flagged as a likely match, staff confirmed the merge in one click. By move-in day, Marcus has one identity vertex with his application history, background check result, and lease all linked. He logs into the app and it already knows his unit, his move-in date, and that he requested a parking spot.

**Path B — Misspelling path (staff/self-registration mismatch):**
Elena's leasing agent entered her as "Elena Kowalsky." Elena registered in the app as "Elena Kowalski." No exact email match — the agent used her work email; Elena registered with her personal email. The system finds two candidate identity vertices with similar names and overlapping phone numbers. It does not merge them. It creates a staff review task: *"Possible duplicate identity: review and confirm before lease is linked."* Sam reviews both records, confirms they are the same person, approves the merge. One identity vertex. One lease. No data loss.

**Path C — Grandfathered lease (identity must be claimed):**
The building migrated 12 legacy leases from a spreadsheet. Unit 4D has an active lease assigned to "Robert Chen" with a phone number on file. No app account exists. Charges have been accumulating for two months. Robert downloads the app and taps "Set up your account." He enters his phone number. The system finds an unclaimed identity vertex with that phone number and a pending lease. It presents: *"We found an account associated with your phone number at Loftspace. Is this you?"* Robert confirms. His credential is bound to the existing identity vertex. His two months of charge history are immediately visible. The lease is his.

**Capabilities revealed:** `CreateProspectIdentity` (staff-initiated, unclaimed); `RegisterIdentity` (self-service, claim-key checked); fuzzy dedup detection (staff review task, never auto-merge); `ClaimIdentity` (binds auth credential to existing vertex); `MergeIdentity` (staff-approved, after review task); grandfathered lease with `pendingClaimant` aspect; identity vertex states: unclaimed / claimed / merged / flagged. **Implementation flag:** `ClaimIdentity` and `MergeIdentity` require claim-key scan/index across Core KV — non-trivial read amplification; noted as Stream 3 implementation complexity.

---

### Journey 1 — Maya: "The Lease Renewal"

*Persona: Maya, 31, resident for two years. Rents a studio on the 8th floor. Has a cat, Miso. Uses the coworking space twice a week.*

**Opening Scene:** Maya gets a push notification: her lease expires in 60 days. She opens the resident app and taps "Talk to your concierge."

**Rising Action:** The AI concierge already knows who she is. It pulls her identity vertex, follows links to her current lease, her payment history (24 months on time), her coworking usage, her stated preferences (high floor, quiet side), and her pet registration. It presents two renewal options tailored to her profile — one with a coworking bundle discount, one with a longer term for rate stability. No form. No repeat questions.

**Climax:** Maya asks: *"What happens to my coworking access if I switch to the longer term?"* The AI traverses: identity → membership → coworking entitlement → pricing rules — and answers with a specific dollar figure. Maya says "let's do the longer term."

**Graceful failure:** The AI asks: "Shall I submit the renewal request?" Maya says "wait — can I think about it?" The AI responds: "Of course — I've saved your preferences. Come back any time in the next 60 days and we'll pick up here." No operation is submitted. The pending intent is persisted as a `PendingRenewal` aspect on Maya's identity vertex — not in ephemeral AI memory. Next time, the AI doesn't re-ask for her floor preference or Miso's name.

**Resolution (if Maya confirms):** A `RenewLease` operation is submitted. The Processor validates tenure, pricing, and entitlements via Starlark. The Lens projection updates. A task vertex is assigned to Sam for countersignature. Maya's app shows "Renewal in progress." Miso remains in the system.

**Capabilities revealed:** AI graph traversal (identity → lease → entitlements); AI concierge as primary consumer interface; `RenewLease` command + Starlark rule; Loom renewal workflow with human countersignature gate; task assignment to leasing staff; pending intent persistence as aspect vertex (AI state grounded in Core KV, not ephemeral); Read-Your-Own-Writes via `vtx.op.<request-id>`.

---

### Journey 2 — Carlos: "The Unexpected Arrival"

*Persona: Carlos, 44, building concierge for six years. Manages ~200 resident relationships daily.*

**Opening Scene:** Marcus walks into the lobby moving fast, clearly upset. Carlos recognizes the face but can't place the name. He glances at his staff surface and types "14C."

**Rising Action:** Before Marcus reaches the desk, Carlos's screen shows: Marcus Webb, 14C, two-year resident, premium tier. Open maintenance ticket: water pressure issue, 6 days ago, status "in progress" with no update in 4 days. Last interaction: package pickup, Tuesday. Communication preference: direct, no small talk.

**Climax:** Marcus says, "Nobody has fixed my shower in almost a week." Carlos doesn't ask for his name or unit. He pulls the ticket, escalates priority, and says: "Marcus, I can see it's been open since Thursday. I'm escalating this right now — you'll hear from facilities within two hours."

**Revision conflict note:** When Carlos submits the priority escalation, the Processor applies a revision condition to the ticket vertex. If the maintenance tech submitted a status update simultaneously, the revision condition detects the conflict and retries with the latest state. Carlos sees the final resolved state — never a stale snapshot.

**Resolution:** The Weaver convergence target ("no open critical maintenance ticket without a staff-owner update within 24 hours") fires. A `core-events` event advances the Loom maintenance workflow. Carlos closes the interaction in under 90 seconds.

**Capabilities revealed:** Staff-facing Lens projection (pre-computed resident context); maintenance ticket vertex + Loom workflow; Weaver convergence SLA enforcement; revision conflict resolution (transparent to staff); staff role-scoped Capability Lens (operational context visible, financial data not); single-surface zero-app-switching experience.

---

### Journey 3 — Sam: "The Month-End Close"

*Persona: Sam, 38, leasing manager. Manages lease applications, renewals, rent rolls, and monthly close for 200 units.*

**Opening Scene:** It's the 1st of the month. Sam opens the back-of-house dashboard — a Lens projection over the full resident and lease graph.

**Rising Action — Rent roll:** 194 occupied, 6 vacant, 3 in notice. One anomaly: unit 7B shows active lease but no payment received this cycle. A Weaver convergence rule flagged it automatically — a task is already in Sam's queue: "Review: potential late payment unit 7B." Sam opens the tenant record. The late fee workflow is one click away.

**Rising Action — Lease application:** A new application came in overnight for unit 12A. A Loom instance stepped through: application submitted → background check initiated → credit check returned → flagged for human review. Sam sees the applicant's vertex: income documentation, background check result, credit score — all as aspects. Everything meets criteria. Sam clicks "Approve." `ApproveLeaseDraft` is submitted. Starlark validates. A `LeaseApproved` event fires. Loom advances to "send lease for signature."

**Resolution:** By noon Sam has closed the rent roll, approved one application, and flagged a collections case — all via Lens projections. Full audit trail on every action via the operation ledger. No spreadsheets.

**Capabilities revealed:** Back-of-house Lens projections (rent roll, occupancy, payment status); Loom multi-step lease application workflow with human approval gate; Weaver late-payment flag; task assignment from Weaver/Loom to human actors; role-scoped Capability Lens (leasing access vs. finance access); audit trail via `core-operations` ledger.

---

### Journey 4 — Alex: "The New Revenue Line"

*Persona: Alex, 45, VP of Operations. Not a developer. Previously waited 3–6 months for engineering to implement any new business idea.*

**Opening Scene:** Resident surveys show 40% want a weekly farmers market. Alex wants it running by next Saturday.

**Rising Action:** Alex opens the Lattice developer console and describes the idea to the AI assistant. The AI proposes: two new vertex types (`Event`, `Vendor`), one command (`RegisterForEvent`), one Lens (`EventRoster` → Postgres), one Weaver target (auto-promote events with zero registrations after 48 hours). Alex simplifies the vendor roster to a freeform text aspect for now.

**AI intent retention:** When Alex requests the simplification, the AI acknowledges: *"Noted — simplified to freeform for now. If you want structured vendor management later, I'd suggest a `Vendor` vertex type with a `participates_in` link. I can prepare that whenever you're ready."* The full original intent is retained in the capability authorship session vertex.

**Climax:** Alex clicks "Submit for review." A task vertex is assigned to Alex: "Review and approve: FarmersMarket capability bundle." Alex reads the plain-language summary, approves. Operations submitted via `ops.meta.>`. Processor commits. Refractor activates the Lens. Weaver activates the convergence target.

**Resolution:** Forty minutes after Alex first typed her request, `RegisterForEvent` is available in the resident app. No PR. No sprint. No meeting. Three months later, Alex adds a cancellation fee for no-shows — describes it, AI proposes a Starlark change, Alex approves, live. Still no meeting.

**Weaver governance:** The convergence target Alex approved went through the same human-review task loop as all capability authorship. Alex can view, modify, or revoke all active Weaver targets from the console.

**Capabilities revealed:** AI-authorship loop (describe → propose → review → approve → live); DDL meta-vertex authorship via `ops.meta.>` lane; Lattice-native console with task-based approval; AI intent retention across edits (session vertex); Weaver target as AI-proposed, human-approved meta-vertex; self-improvement without redeployment; iterative evolution.

---

### Journey 5 — Carlos: "The Auth Failure"

*Edge case: a resident disputes a charge.*

**Opening Scene:** Diana Chen, 22B, disputes a late fee — she had a maintenance emergency the same week.

**Rising Action:** Carlos pulls up Diana's resident surface — profile, maintenance history, communication log all visible. He navigates to her payment record and sees the fee was applied. He tries to access the underlying rent ledger detail.

**Climax:** The system returns: *"Rent ledger detail requires Finance access. Your current role (Concierge) has access to: resident profile, maintenance history, service requests, communication log. To review payment disputes, assign this to the Leasing team or request temporary Finance access via your manager."* Specific. Actionable. Not null.

**Routing + fallback:** Carlos creates a `PaymentDisputeRequest` task assigned to Sam. If Sam's queue is at capacity or Sam is marked unavailable, the Starlark routing script falls back to the leasing team queue (role-based, not person-based). If the leasing queue is also unavailable, the operation fails with a specific error and surfaces in the health observability view as an unrouted task requiring manual intervention. No silent drop.

**Resolution:** Diana leaves with a reference number. The Capability Lens worked correctly. No privilege escalation. No silent failure.

**Capabilities revealed:** Role-scoped Capability Lens (concierge vs. leasing vs. finance); AI-legible auth denial (specific, actionable, not null); task creation + role-based routing with fallback; unrouted task surfacing in Health KV observability; auth debugging 3-plane trace (Core KV path → Capability Lens → Capability KV read).

---

### Journey 6 — AI Agent: "The Lease Inquiry"

*Persona: A Lattice-aware AI helping resident Priya understand her lease before signing.*

**Opening Scene:** Priya asks the building's AI concierge to walk her through the early termination clause.

**Rising Action — Discovery:** The AI starts at Priya's identity vertex. Follows `pending_lease` to her draft lease. Follows `governed_by` to the lease template vertex — reads `earlyTerminationPolicy`, `noticeRequirements`, `penaltyCalculation` aspects. Reads `asp.data.schema` and `asp.ui.schema` to understand field types and plain-language descriptions. No hardcoded query. No documentation lookup.

**Rising Action — Context hydration:** The agent follows links from Priya's identity → `current_unit` (12A) → `building` → `policy` aspects. Finds that this building waives early termination penalties for documented medical or job relocation events. Building-specific context — not in the lease template. The graph provided it through traversal.

**Climax:** The agent detects that the `marketRateIndex` vertex referenced in the penalty calculation hasn't been updated in 90 days. It submits a `FlagStaleData` intent via `ops.urgent.>`.

**Escalation failure path:** If the `FlagStaleData` operation fails (e.g., `ops.urgent.>` temporarily unavailable), the AI does not proceed silently. It includes a caveat: *"I noticed the market rate index may be outdated, but I wasn't able to flag it for review right now. I'd recommend confirming the penalty calculation with a leasing staff member before signing."* Graceful degradation — acknowledges the limitation, gives actionable guidance, no hallucinated confidence.

**Resolution:** Priya gets a clear, context-aware answer. The stale data flag (if submitted) creates a task for Sam. Full loop — discovery, hydration, action, escalation — using only graph traversal and the write path.

**Capabilities revealed:** Graph traversal as primary AI interface (no SDK); `asp.data.schema` + `asp.ui.schema` as self-describing API; building-specific policy via graph links; AI-legible stale data detection; intent submission via `ops.urgent.>`; task creation from AI-detected anomaly; graceful AI degradation with actionable fallback.

---

### Journey Requirements Summary

| Capability Area | Journeys | Notes |
|----------------|---------|-------|
| Two-phase identity (staff-created + resident claim) | 0 | `CreateProspectIdentity`, `RegisterIdentity`, `ClaimIdentity` |
| Fuzzy dedup detection + staff merge workflow | 0 | Hard block on auto-merge; staff review task always required |
| Grandfathered lease / unclaimed identity claim | 0 | `pendingClaimant` aspect; claim-key scan on registration |
| Pending intent persistence as aspect vertex | 1 | AI state grounded in Core KV, not ephemeral memory |
| AI graph traversal (no SDK, no hardcoded queries) | 1, 4, 6 | `can_execute`, `asp.data.schema`, `asp.ui.schema` |
| Starlark business rules + DDL validation | 0, 1, 3, 4, 5 | Command vertices with Starlark scripts |
| Lens projections (staff, back-of-house, event roster) | 2, 3, 4 | CQRS — Core KV → query-optimized Lens targets |
| Loom multi-step workflows with human approval gates | 0, 1, 3 | Lease application, renewal countersignature, onboarding |
| Weaver convergence targets (AI-proposed, human-approved) | 2, 4 | Same AI-authorship loop; operator can view/revoke all targets |
| Capability Lens + role-scoped auth | 2, 3, 5 | Clean denial with routing; 3-plane trace |
| AI-authorship loop (describe → propose → approve → live) | 4 | Console, task vertex, DDL via `ops.meta.>` |
| Task routing + role-based fallback | 1, 3, 5 | Fallback to role queue; unrouted tasks in health view |
| Revision conflict resolution (atomic batch + revision conditions) | 2 | Transparent to staff |
| AI intent retention across edits | 4 | Capability authorship session vertex |
| Graceful AI degradation (escalation failure) | 6 | Caveat + actionable guidance; no hallucinated confidence |
| Health KV direct reads (unrouted tasks, operational alerts) | 5 | Separate from Core KV; console reads directly |
| Read-Your-Own-Writes | 1 | `vtx.op.<request-id>` polled by client |
| **Phase 2 persona noted** | — | Building owner/asset manager: portfolio-level Lens projections |

## Domain-Specific Requirements

### Privacy & Data Subject Rights

**Applicable frameworks:** GDPR (EU data subjects), CCPA (California residents). The platform stores personal information about residents — names, contact details, behavioral history, financial history — making privacy compliance a first-class product concern.

**Architectural answer:** Crypto-shredding (Stream 6) is the platform-level implementation of right-to-erasure. When a resident requests deletion, their encryption key is shredded; all encrypted aspects become irrecoverable without redeployment or data surgery.

**Erasure model — anonymization, not deletion:**
Crypto-shredding operates at the **aspect level** — field by field, not record by record. GDPR Article 17(3) permits retention of records required for legal compliance, financial obligations, and defense of legal claims. The platform resolves the privacy-vs-audit-trail conflict as follows:

| Data | Classification | On erasure request |
|------|---------------|-------------------|
| Applicant name, address, government ID, income documents | `sensitive: true` aspects | Key shredded → irrecoverable |
| Application outcome (approved/denied) | Non-sensitive aspect | Retained |
| Denial reason, criteria applied | Non-sensitive aspect | Retained |
| Decision date, criteria version | Non-sensitive aspect | Retained |
| Link to identity vertex ID | Referential link | Retained (tombstoned vertex, PII gone) |

After shredding: the audit record states *"Application [ID] was denied on [date] because [criteria]. Applicant: [REDACTED]."* The auditor sees the decision and rationale. The applicant's PII is cryptographically unrecoverable. Both obligations are satisfied simultaneously.

**DDL design contract:** The platform provides selective shredding; the *application* must correctly designate fields. Denial reasons and business outcomes must be stored in non-sensitive aspects; personal identifiers in sensitive aspects. The Loftspace reference app DDL exemplifies this contract. Bad DDL design (putting denial reasons in sensitive aspects) shreds the audit trail — the platform cannot prevent this, but the PRD defines the correct pattern.

**Active lease erasure block:** The platform blocks `ShredKey` by default when the identity has active financial obligations (active lease, outstanding charges). Erasure of an active resident's identity requires explicit operator override.

**Retention policy per data type:**
- Financial records: ~7 years (US standard) — operator-configured Weaver convergence target
- Personal behavioral data: subject to erasure on request (crypto-shredded)
- Operational logs: operator-configured NATS stream compaction

**Move-out vs. deletion distinction:**
- Resident moves out: lease ends, identity vertex persists, personal aspects optionally shredded per operator policy
- Resident requests deletion: key shredded, personal aspects nullified, identity vertex tombstoned, non-sensitive business records retained

### Payment Data

**Design intent:** Lattice stores *references* to payment transactions — token IDs, status codes, amounts, timestamps — but never raw card data (PAN, CVV, expiry). Actual payment processing is an integration dependency (Stripe, ACH processor). This keeps the platform outside PCI DSS scope by architecture.

**Implication:** `paymentHistory` aspects in Core KV contain non-sensitive tokens and metadata only. Any Lens projecting payment data projects the same reference data. PCI scope stays with the payment processor, not with Lattice.

### Audit Trail & Data Retention

**Immutable ledger:** The `core-operations` JetStream stream provides a complete, ordered, immutable audit trail of every operation submitted to the platform. This satisfies audit requirements (tenant disputes, regulatory review, financial audit) by architecture.

**Compaction:** Stream compaction is a deployment configuration concern — not a platform default. Compaction policy is set per deployment by the operator. Post-MVP concern; MVP ships with no compaction configured.

**Unlimited retention path (Phase 2):** A dedicated Lens adapter — or NATS-level stream mirroring — can write the full `core-operations` ledger to **Google BigQuery** for unlimited, cost-effective long-term retention and analytics. The Lens adapter framework already supports external targets (Postgres, Elasticsearch); BigQuery is a natural additional target requiring no architectural changes.

### Multi-Jurisdiction

Single-cell MVP sidesteps data residency requirements. When multi-cell (Stream 8) is implemented, data residency becomes a cell routing concern: EU resident data must stay in EU-hosted cells. This constraint is a Stream 8 design requirement.

### Out of Scope

| Concern | Disposition |
|---------|------------|
| Fair housing / tenant law compliance | Reference app implementation concern — not platform PRD |
| Raw card data / PCI DSS | Avoided by design — Lattice stores references only |
| SOC 2 / ISO 27001 certification | Post-launch commercial requirement, not MVP |
| HIPAA | Not applicable — no health data in scope |

## Innovation & Novel Patterns

### Detected Innovation Areas

**Innovation 0: Persistent identity that outlives any single transaction**

Every competitor resets the relationship at the transaction boundary. A lease ends — the resident disappears. A maintenance request closes — the history is buried. A new service line launches — the member starts over. Lattice's VAL model makes identity a first-class persistent entity: the resident vertex outlives any lease, any service, any property. This is the experience innovation the entire architecture enables. Businesses are increasingly held accountable for the *relationship*, not just the transaction — and current technology systematically fails to support this. Lattice makes relationship failures architecturally harder to commit.

**Innovation 1: Platform discipline enforced by architecture, not culture**

Every platform that promises "spin up a new business idea easily" has failed for the same reason: discipline is communicated via documentation, code reviews, and team alignment — all of which degrade under scale and turnover. Lattice makes violations architecturally impossible. The sole-writer rule means the write path *cannot* be bypassed. DDL validation means schema violations *cannot* reach Core KV. Starlark's sandbox means business logic *cannot* perform I/O or introduce non-determinism. The platform doesn't ask engineers to agree — it enforces agreement.

**Innovation 2: The self-improvement loop — a living system, not a deployment**
*(The moat)*

Software systems are static between deployments. Lattice's capabilities — vertex types, business rules, Lens projections, workflows, convergence targets — are first-class data in Core KV, authored through the same write path as business operations. A new capability is a committed operation, not a deployment artifact. The platform is continuously evolvable by its own mechanisms, with human approval as the only gate. Three months after launch, adding a new revenue line is identical to day one: describe → propose → approve → live. This is the moat: not the initial build, but every subsequent change.

**Innovation 3: AI authorship of platform capabilities, safe by design**
*(The business model)*

Existing AI coding tools generate application code that runs outside any safety boundary. Lattice introduces a different model: AI proposes mutations to the platform itself (new vertex types, Starlark rules, Lens definitions), submitted as operations through the same validated write path as business transactions. The Starlark sandbox, DDL validation, and atomic batch semantics are the trust architecture. An AI-authored Starlark script *cannot* perform a network call, access a secret, or corrupt unrelated state. This is not a guardrail bolted on — it is the consequence of the architecture. AI authorship becomes enterprise-safe by construction.

**Innovation 4: Graph as self-describing API surface**
*(The consumer expression)*

Traditional APIs require documentation, SDKs, and client code kept in sync with the server. In Lattice, an AI agent starts at any identity vertex, follows `can_execute` links to discover available commands *within the current capability surface*, reads `asp.data.schema` and `asp.ui.schema` to understand input requirements, and submits a validated intent — with zero prior knowledge of the specific deployment. The graph is the API documentation. Every Lattice deployment is navigable by any Lattice-aware AI without bespoke integration work. Note: the graph is self-describing *within its current capabilities* — it reflects what has been authored, not what could theoretically exist.

**Innovation 5: The collapsed PM-to-deployment pipeline**

Conventional software development: PM writes spec → engineers implement → QA tests → ops deploys. Lattice: human describes intent → AI authors capability → human reviews and approves → platform activates. This is not "low-code" (configuration-bounded) or "no-code" (drag-and-drop limits) — it is full code-level expressiveness (Starlark can implement any business rule) with AI as the implementation agent and the platform as the safety layer.

### Market Context & Competitive Landscape

| Competitor / Category | What they offer | What Lattice does differently |
|----------------------|----------------|-------------------------------|
| **Yardi / RealPage** | Monolithic property management SaaS | Lattice is a platform OS, not a vertical app. Yardi can't extend itself without Yardi engineering. |
| **Salesforce (metadata API)** | Declarative customization, workflow automation | Not graph-native. Customization is configuration-bounded. No AI authorship safety model. |
| **ServiceNow** | Enterprise workflow automation | Powerful but heavyweight, not AI-native, not a data OS. No persistent identity model. |
| **Airtable / Notion** | Flexible data modeling, no-code | Not a platform for building applications on top of. No write path discipline. No auth model. |
| **Neo4j + Kafka + OPA** | Graph DB + event streaming + policy engine | No coherent authorship model — AI can query and trigger, but cannot safely extend the system's capabilities. The gap is conceptual, not operational. |
| **Event-sourcing / CQRS frameworks** | Architectural patterns | Lattice is a complete runtime, not a pattern. Includes auth, orchestration, projection, and AI authorship. |
| **"Aspirational internal platform" (prior art)** | Platforms that promised this vision and partially delivered | Lattice was designed by someone who built one of these and knows exactly why they fail. The architectural decisions (sole-writer rule, Starlark sandbox, durable consumers) are direct responses to the failure modes of aspirational platforms. |

### Validation Approach

| Innovation | Validation method | Spike / milestone |
|-----------|-----------------|-------------------|
| Persistent identity across contexts | Demo: same identity vertex spans lease + coworking + café + maintenance history | Journey 0 ("Finding Your Account") vertical slice |
| AI authorship safety (Starlark sandbox) | Adversarial test suite — attempt to break out of sandbox via AI-authored scripts | Stream 1 — Starlark safety spike |
| Graph as self-describing API | Demo: cold-start AI agent navigates Loftspace with zero prior knowledge, completes round-trip | "Hello Lattice" vertical slice |
| Self-improvement loop | Demo: operator adds net-new capability in < 1 working day | Phase 2 Loftspace console milestone |
| Write-path discipline | Integration tests — attempt all known bypass paths; verify all fail | Stream 0–1 test harness |
| Collapsed PM-to-deployment pipeline | Measure wall-clock time from "operator describes intent" to "capability live" | Phase 2 acceptance criterion |

### Risk Mitigation

| Innovation risk | Process mitigation | Technical mitigation |
|----------------|-------------------|---------------------|
| **AI-authored Starlark subtly wrong (not unsafe, just incorrect)** | Human review gate mandatory; plain-language change summary required in console | Deterministic-replay golden test suite for Starlark scripts — behavioral regressions caught before approval |
| **Graph traversal latency makes AI concierge feel slow** | Measure at MVP; SLA: < 2s p99 end-to-end | Context Hinting + JIT Hydration (architecture doc: read amplification mitigations) |
| **Self-improvement loop too complex for non-technical operators** | Console UX abstracts complexity; human approves in plain language | Platform enforces safety regardless of operator sophistication |
| **"Graph as API" breaks when schema changes** | DDL versioning convention | `asp.data.schema` always current; AI agents re-traverse rather than caching stale schema |
| **Living system accumulates bad changes** | Every change has author, timestamp, and human approver | Rollback = compensating operation through write path; full audit trail in `core-operations` ledger |

## Platform-Specific Requirements

### Project-Type Overview

Lattice is a hybrid of two project types: a **B2B platform/data OS** (multi-tenant architecture, enterprise permissions, integration ecosystem) and a **developer tool** (SDK, onboarding experience, examples, documentation). Requirements from both types apply. Mobile-first and CLI-only concerns are skipped as not applicable to the core platform.

### Multi-Tenancy Model

**MVP (single-cell):** One deployment serves one operator (one building, one property management company). All data lives in a single NATS KV bucket. No tenant isolation required at the data layer — isolation is at the deployment level.

**Post-MVP (multi-cell, Stream 8):** Multi-tenancy is implemented via cells. Each cell is an isolated NATS cluster with its own Core KV, operations stream, and events stream. Cell routing handles data residency (EU cells for EU residents). The data model is cell-agnostic by design — keys embed no cell identity, making multi-cell a routing concern layered underneath the application.

**Operator vs. end-user tenancy:** In the startup path, the *operator* (property management company) is the tenant. End users (residents, staff) are entities within that tenant's graph. This is not a "resident has their own isolated database" model; it is a "one operator graph with role-scoped access control" model.

### Permission Model

**Model:** Relationship-Based Access Control (ReBAC) via Capability Lens.

**How it works:** Permission paths live in Core KV as graph relationships (`identity → role → permission → command`). Refractor projects these into the Capability KV as a flattened cache. The Processor reads Capability KV on every operation (O(1) auth check). Gateway reads Capability KV for token validation.

**Roles (Loftspace reference):**

| Role | Access scope | Notes |
|------|-------------|-------|
| `consumer` | Own identity vertex, own lease, own service history, public commands | Resident/member |
| `staff.front_of_house` | All resident profiles, maintenance tickets, service requests | No financial data |
| `staff.back_of_house` | Leasing, financial records, reporting Lenses | No raw card data |
| `operator` | All of the above + capability authorship | AI-augmented human-in-the-loop |
| `system.internal` | Root-level — Loom, Weaver, admin tools | Within trust boundary; pre-provisioned keys |

**Auth debugging:** Permission failures must be traceable across three planes: Core KV permission path → Capability Lens projection → Capability KV read. Observability tooling must support this trace.

**Capability Lens is security-critical:** A bug in the Capability Lens projection = potential privilege escalation. Requires dedicated adversarial test suite (joint ownership: Stream 3 defines semantics, Stream 2 validates projection).

### Integration Requirements

| Integration | Type | Purpose | Dependency posture |
|------------|------|---------|-------------------|
| **NATS Server** | Core infrastructure | Ledger, KV, control plane | Hard dependency — platform cannot function without it |
| **Postgres** | Lens target store | Query surfaces (staff dashboard, back-of-house reporting) | Integration dependency — Lenses degrade if unavailable; Core KV unaffected |
| **Payment processor** (Stripe / ACH) | External service | Payment transaction processing | Integration dependency — Lattice stores references only |
| **External KMS/HSM** | Vault integration | Encryption key management | Integration dependency — sensitive aspects unavailable if down; non-sensitive unaffected |
| **External IdP** | Actor signing keys | JWT signing for Lattice-Actor authentication | Integration dependency — MVP mitigation: pre-provisioned keys |
| **Reverse proxy** (NGINX/Envoy) | Infrastructure | TLS termination, rate limiting, DDoS mitigation | Deployment dependency — internet-facing only |
| **Google BigQuery** | Lens target store (Phase 2) | Unlimited audit log retention | Phase 2; optional; no architectural changes required |

**Integration degradation posture:** The core data plane depends only on NATS. Each integration dependency has its own degradation mode — the system continues to process operations if an integration is down; only the dependent capability degrades.

### Developer Experience Requirements

**Onboarding target:** Developer clones repo, runs `make up`, completes "Hello Lattice" vertical slice (one entity → one rule → one Lens → one AI query) in under 60 minutes.

**Required developer surfaces:**
- `make up` — boots NATS + Postgres + bootstrap data in Docker
- `make test` / `make test-integration` — unit and integration test targets
- CLI tool — submit operations, inspect Core KV, query Lens projections
- CLAUDE.md — AI agent instructions for consistent implementation
- Architecture doc — single source of truth for all technical decisions

**Documentation deliverables (PRD scope):**
- "Hello Lattice" tutorial (Phase 1)
- Loftspace DDL + Starlark script examples (Phase 2)
- Starlark stdlib API reference (Stream 1)
- Lens definition authoring guide (Stream 2)

**Starlark learning curve mitigation:** The platform provides: (a) stdlib functions covering common patterns (entity creation, state machine transitions, link management); (b) golden test examples for each pattern; (c) the AI-authorship loop in the console as the primary authoring surface for non-Starlark-fluent operators.

### Subscription & Pricing Model

**Status: TBD** — deferred to startup path commercial planning.

**Options under consideration:** per-property/cell, per-seat, platform license + usage, open core (platform free, enterprise features paid).

**Decision trigger:** Before Phase 2 epics are written for the startup path.

### API & External Surface

**MVP:** No public REST/gRPC API for external clients. CLI tool connects to NATS directly. Gateway translator handles HTTP→NATS for browser clients.

**Post-MVP (Stream 5):** External API surface decision deferred — REST via Gateway, gRPC, or NATS direct. Decision made in Stream 5 based on reference app client requirements.

**AI agent API surface:** Graph traversal via NATS KV reads + command submission via `ops.urgent.>` — available from MVP. Primary AI consumer interface; no REST/gRPC required for AI agents.

## Project Scoping & Phased Development

### MVP Strategy & Philosophy

**MVP Approach:** Platform Proof MVP — a working platform that validates the core architectural claims under real conditions. The proof is a cold-start AI agent completing a round-trip (identity vertex → available commands → schema → validated intent submission) via graph traversal alone, with zero prior hardcoded knowledge of the deployment. A developer can reproduce this from `git clone` in under 60 minutes.

**MVP is not a product. It is a proof.** Phase 2 (Loftspace showcase + developer console) is where the platform becomes a product operators can run. Phase 1 establishes that it deserves to exist.

**Resource Requirements:** 1 senior engineer (Phase 1), using the AI-authorship loop from day one — the platform eats its own food during development. Phase 2 (Loftspace + console) expands to 2–3 engineers.

**AI-authorship in Phase 1:** The mechanism (describe → propose → review → approve → live) is proven via CLI and task vertices. The browser-based developer console is a Phase 2 deliverable. Phase 1 is NATS-native, CLI-first.

---

### MVP Feature Set (Phase 1)

**Stream 0 — Substrate (prerequisite gate):**
- NATS spike stories resolved (known behavioral edge cases under load)
- **Starlark execution spike — Stream 0 gate:** Must complete before Stream 1 commit path design begins. If p99 execution time exceeds 100ms threshold, the commit path architecture (hot path evaluation count, pre-compilation, caching) changes materially. This is a stream-ordering gate, not a Stream 1 activity.
- Operation envelope schema, NanoID generation
- `make up` dev harness — NATS + Postgres + bootstrap data in Docker
- `make test` / `make test-integration` targets

**Stream 1 — Core Processor:**
- 10-step commit path
- Starlark sandbox (enforced: no I/O, no non-determinism, no secret access)
- DDL meta-vertices authored via `ops.meta.>` lane
- Atomic batch + idempotency
- **Attempted bypass test suite** (Phase 1 completion gate — see below)

**Stream 2 — Refractor:**
- Materializer fork → Refractor
- Durable consumer per Lens
- Postgres adapter (MVP Lens target)
- **Capability Lens** — security-critical; adversarial test suite required (see below)

**Stream 3 partial — Identity & Gateway (spec + partial implementation):**
- Identity vertex type; states: unclaimed / claimed / flagged
- Two-phase claim model: `RegisterIdentity`, `ClaimIdentity`
- Grandfathered lease `pendingClaimant` aspect (Journey 0 Path C)
- JWT/Lattice-Actor header — **spec and contract defined in Phase 1**
- Capability KV shape finalized in Phase 1
- ClaimIdentity read amplification spike — claim-key index approach validated
- **Gateway implementation deferred to Phase 2** (Loftspace prerequisite — no HTTP clients exist at Phase 1)

**Reference Application:**
- **"Hello Lattice" vertical slice:** One resident vertex → one lease aspect → one payment rule (Starlark) → one Lens projection (Postgres) → one AI traversal query (cold-start, zero prior knowledge)
- Triple role: live demo + integration test fixture + developer onboarding tutorial
- Completion target: < 60 minutes from `git clone` to working slice

**Observability (MVP-tier):**
- Health KV direct reads (separate KV store from Core KV; console reads directly — not via Lenses)
- NATS CLI for stream inspection
- No browser-based console UI at Phase 1

**Phase 1 Journey Support:**
- Journey 0 Path A + Path C (clean self-registration + grandfathered lease claim) — identity model stress-tested with real claim-key scan before Phase 2 load
- Journey 5 (auth failure + clean Capability Lens denial) — validates trust architecture under real conditions
- Journey 6 (AI cold-start traversal + intent submission) — the Phase 1 north star
- *Journey 0 Path B (fuzzy dedup), Journey 1 (Loom renewal), Journey 2 (staff surface), Journey 3 (back-of-house), Journey 4 (AI-authorship console UX) — all Phase 2*

**Phase 1 Completion Gates (non-negotiable before Phase 2):**

| Gate | Description |
|------|-------------|
| Starlark spike result | p99 execution time documented; commit path design confirmed or revised |
| Attempted bypass test suite | Direct KV write, stream publish outside `ops.*`, Starlark I/O escape, DDL schema violation — all rejected and test-verified |
| Capability Lens attack vectors | 4 vectors tested: role escalation via direct KV write, projection lag window exposure, Lens definition mutation via AI-authored op, cross-vertex permission bleed |
| Compensating operation / DDL rollback | Integration test demonstrates a bad AI-authored DDL change can be reversed via compensating operation through write path |
| "Hello Lattice" onboarding | < 60 minutes from `git clone` to working vertical slice, verified by at least one external tester |

---

### Post-MVP Features

**Phase 2 — Growth (Streams 4–6 + Loftspace Showcase)**

*Prerequisite decision: beachhead vertical (Loftspace / The Campus / Membership Club) to be scored and selected before Phase 2 epics are written. Current recommendation: Loftspace — highest domain richness, employer-sensitivity-free, exercises all 8 domain properties.*

*Prerequisite decision: subscription/pricing model (startup path) — to be resolved before Phase 2 epics are written.*

- **Stream 3 continuation — Gateway implementation:** NGINX + thin Go service (HTTP→NATS translation); required for Loftspace resident and staff browser clients
- **Stream 4 — Loom/Weaver:** Multi-step workflows with human approval gates; Weaver convergence targets (AI-proposed, human-approved via same AI-authorship loop); natural language target state
- **Stream 5 — Services/SDK:** Command vertex types, task vertex types, role-based task routing with fallback, pending intent persistence as aspect vertex
- **Stream 6 — Privacy:** Crypto-shredding at aspect level, Vault integration for KMS/HSM, active lease erasure block, retention policy Weaver targets
- **Identity Phase 2:** Fuzzy dedup detection (Journey 0 Path B), `MergeIdentity` (staff-approved after review task), AI intent retention across edits (session vertex)
- **Loftspace reference application:** Full single-building residential + coworking + event space + café — all 7 journeys fully supported; exercises complete T1–T4 capability tier stack
- **Lattice-native developer/admin console:**
  - AI-suggested capabilities surface as human-review task vertices
  - Business state via Lens projections (Core KV → Postgres/NATS KV via Refractor)
  - Operational health via Health KV direct reads (separate plane from Core KV)
  - Weaver target management (view / modify / revoke)
  - Capability Lens auth trace (3-plane debug view: Core KV path → Capability Lens → Capability KV read)
- **BigQuery Lens adapter:** Unlimited audit log retention via `core-operations` stream mirroring to Google BigQuery; no architectural changes required (standard Lens adapter extension)

**Phase 3 — Expansion (Vision)**

- **Edge Lattice:** Personal identity graph that travels with the member across cells; requires separate cell topology design
- **Stream 8 — Multi-cell / sharding:** Data residency routing (EU cells for EU residents); cell-agnostic key design already supports this at the data layer
- **Additional showcase verticals:** The Campus (student housing + academic services), Membership Club (Soho House lifestyle model) — after beachhead vertical validates demand
- **Public brand name + go-to-market:** Separate workstream; deferred by design
- **Managed NATS (Synadia Cloud):** Deployment option; no platform changes required
- **Open-source community governance:** External contribution model, public DDL + Starlark pattern library, community Lens adapter registry

---

### Risk Mitigation Strategy

**Technical Risks:**

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| **Starlark p99 exceeds 100ms** | Medium | **Critical** | Stream 0 gate — spike completes before commit path design begins; if threshold is 150–200ms, commit path architecture revised before Stream 1 work starts |
| **Capability Lens projection has security bug** | Low | Critical | 4 specific attack vectors in Phase 1 test suite; Capability Lens treated as security-critical component with dedicated adversarial review milestone |
| **ClaimIdentity read amplification under load** | Medium | Medium | Spike in Phase 1 Stream 3 design; claim-key index approach validated before Phase 2 HTTP load |
| **NATS JetStream behavioral edge cases** | Low | High | Stream 0 spike stories resolve known gaps; single-cell MVP limits production exposure |
| **AI-authored Starlark correct-but-wrong** | High | Low–Medium | Deterministic-replay golden test suite; human review gate mandatory; plain-language change summary required before approval |
| **Living system accumulates bad changes** | Medium | Medium | Every capability change has author, timestamp, and human approver; rollback = compensating operation through write path; full audit trail in `core-operations`; proven in Phase 1 test harness |

**Market Risks:**

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| **Phase 1 produces no visible product** | Medium | High | "Hello Lattice" < 60 min is the Phase 1 milestone; portfolio door opens the moment it completes, before Phase 2 |
| **Beachhead vertical wrong choice** | Low | Medium | Three strong verticals identified; Loftspace scores highest on richness and sensitivity; decision deferred to allow scoring before Phase 2 epics |
| **"Platform OS" positioning too abstract for operators** | Medium | Medium | Demand-first narrative throughout PRD; Loftspace journeys translate abstract platform claims to concrete operator stories |
| **Competitor ships AI-authorship loop first** | Low | Medium | Moat is the trust architecture (Starlark sandbox + DDL validation + sole-writer rule) — copying the interface doesn't replicate the safety model |

**Resource Risks:**

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| **Phase 1 scope creep** | High | High | Phase 1 formally excludes Loom, Weaver, fuzzy dedup, Gateway implementation, console UI, crypto-shredding; "Hello Lattice" + 3 journeys + 5 completion gates = the entire Phase 1 definition |
| **Solo engineering bottleneck** | Medium | Medium | AI-authorship loop used from day one; Materializer morph (~80% reuse) constrains greenfield surface; platform eats its own food |
| **Phase 2 Loftspace scope underestimated** | Medium | Medium | Loftspace scope bounded: one building, ~200 units, 5 roles, 7 journeys, not multi-property at Phase 2; console scoped separately |
| **Developer console UI underestimated** | Medium | Low | CLI-first at Phase 1; Phase 2 console scoped as a standalone product surface with its own epic |

## Functional Requirements

> *This is the binding capability contract. UX designers, architects, and PMs work exclusively from this list. Any capability not listed here does not exist in the final product unless explicitly added.*
>
> *AI agent capabilities are distributed across: Query & Projection (FR19 — graph traversal as primary interface), Write Path (FR9, FR34 — Starlark sandbox + intent submission), and AI Interaction & Capability Authorship (FR31–FR36, FR54 — authorship loop, context persistence, anomaly detection).*

### Identity & Member Management

- **FR1:** Staff can create an unclaimed identity record for a prospect or leaseholder without requiring the person to have an active account
- **FR2:** A resident or member can self-register and bind their credentials to an existing unclaimed identity record using matching claim keys
- **FR3:** The system detects potential duplicate identity records based on fuzzy matching of name, phone, and email; detection is presented for human review, never resolved automatically
- **FR4:** Staff can review duplicate identity candidates and approve a merge; merges cannot occur without explicit staff confirmation
- **FR5:** A leaseholder with a grandfathered account (no app registration) can claim their existing identity vertex and immediately access full account history — charges, lease terms, and communication history
- **FR6:** An identity record exists in trackable states: unclaimed, claimed, flagged-for-review, merged
- **FR7:** A member's identity and full interaction history persist after a lease or membership ends; the relationship is not erased by a transaction boundary

### Write Path & Business Rules

- **FR8:** All state mutations are submitted through a single validated write path; no direct writes to the core data store are possible from any caller outside the platform
- **FR9:** Business rules are expressed as deterministic scripts that execute against each submitted operation; scripts have no access to external I/O, network, secrets, or non-deterministic state
- **FR10:** New entity types, business rules, and projection definitions can be authored and activated in a running system without redeployment
- **FR11:** Every submitted operation produces an immutable, ordered ledger entry including author identity, timestamp, and full operation payload
- **FR12:** Operations are idempotent; resubmitting the same operation produces the same outcome without creating duplicate or conflicting state
- **FR13:** Multiple related state changes can be submitted as a single operation that commits entirely or fails entirely
- **FR14:** An actor can confirm that a submitted operation has been durably committed before reading dependent projections
- **FR57:** Each data type definition declares which operation types are permitted to mutate it; the platform enforces this write-scope constraint on every operation

### Query & Projection (Lens)

- **FR15:** Business state can be projected into query-optimized external targets via configurable projection definitions authored as data operations
- **FR16:** Projection definitions can be created, modified, and activated as platform operations without redeployment
- **FR17:** Front-of-house staff can query pre-computed member context — identity, service history, open tickets, communication preferences — through a role-scoped projection surface
- **FR18:** Back-of-house operators can query operational projections — occupancy, rent roll, payment status, maintenance SLA status — through a role-scoped projection surface
- **FR19:** A Lattice-aware AI agent can traverse from any identity vertex to available commands, input schemas, and plain-language field descriptions without prior hardcoded knowledge of the deployment
- **FR20:** Projection lag between a committed operation and its appearance in a query surface is bounded and observable
- **FR51:** Operators can query historical operational state across a configurable time range

### Access Control & Authorization

- **FR21:** Permission relationships are derived from graph structure and used to authorize every operation in real time
- **FR22:** A permission denial response specifies the exact permissions required, the actor's current role, and available escalation or routing paths
- **FR23:** Auth failures are traceable across three planes: the graph permission path, the projection definition, and the cached permission check
- **FR24:** Platform operators can define and assign role-scoped access for all actor types: consumer, front-of-house staff, back-of-house staff, operator, and internal system actors
- **FR25:** Operators can audit which actors hold which permissions at any point in time
- **FR56:** An actor is authorized to complete an operation associated with a task assigned to them; authorization is established at task assignment time. A manager is authorized to complete tasks assigned to their direct reports, as determined by reporting-relationship links between identity vertices.

### Workflow & Orchestration

- **FR26:** Multi-step business processes can be defined as workflows with conditional branching and human approval gates; workflows advance automatically when conditions are met
- **FR27:** The platform can enforce convergence targets — desired operational states — and automatically assign remediation tasks when actual state diverges from target
- **FR28:** Tasks can be assigned to a specific actor or to a role-based queue; when the primary assignee is unavailable, tasks fall back to the role queue
- **FR29:** Unrouted tasks (no available assignee or queue) surface in operational health monitoring and are never silently dropped
- **FR30:** Operators can view, modify, and revoke all active convergence targets from a management surface
- **FR58:** External operations initiated by the platform's orchestration engine are idempotent; a failed or retried external call cannot result in a duplicate charge or duplicated action. The orchestration engine records a visible claim state before executing any external call and does not re-initiate a claimed operation.

### AI Interaction & Capability Authorship

- **FR31:** An operator can describe a desired capability in natural language and receive a proposed entity type definition, business rule script, and projection definition for review
- **FR32:** A proposed capability bundle is reviewed and approved via a task-based workflow before the capability is activated in the running system
- **FR33:** An AI agent's pending intent is persisted in the graph between sessions so that the system retains context across interruptions
- **FR34:** A Lattice-aware AI agent can submit validated intent through the standard write path with the same safety guarantees as human-submitted operations
- **FR35:** Operators can view all AI-authored capability changes — including author, timestamp, and approver — and governance surfaces are accessible alongside operational health state
- **FR50:** An actor can resume a previously interrupted AI interaction with their prior context and preferences available without re-stating them
- **FR53:** An operator can revert any capability change by submitting a compensating operation through the write path without platform downtime or data surgery
- **FR54:** A Lattice-aware AI agent can detect and flag data quality anomalies encountered during graph traversal

### Privacy & Data Subject Rights

- **FR36:** Capability authorship governance surfaces are accessible from the same surface as operational health state
- **FR37:** Personal data fields can be individually encrypted; the encryption key for specific fields can be shredded to render those fields irrecoverable without affecting other fields on the same record
- **FR38:** Non-personal fields — decision outcomes, denial reasons, business criteria applied — are retained after key shredding; the audit record remains intact and legally defensible
- **FR39:** Erasure of an identity with active financial obligations (active lease, outstanding charges) requires explicit operator override
- **FR40:** Payment records store transaction references, status codes, and amounts; raw payment credentials are never stored or processed by the platform
- **FR41:** Data retention policy per data type (financial records, behavioral data, operational logs) is configurable by the operator
- **FR42:** The complete, ordered, immutable operation ledger can be mirrored to an external long-term retention store for unlimited retention without platform changes

### Developer & Operator Experience

- **FR43:** A developer can boot a complete local platform environment — including data substrate, projection target, and bootstrap data — from a single command
- **FR44:** A developer can complete a minimal working vertical slice (one entity → one rule → one projection → one AI traversal query) in under 60 minutes from a fresh clone
- **FR45:** A CLI tool allows developers and operators to submit operations, inspect graph state, and query projection surfaces without a browser client
- **FR46:** Platform operational health — stream lag, unrouted tasks, projection errors, component status — is readable from a dedicated health data store separate from the business state store
- **FR47:** A developer/operator console surfaces AI-suggested capability changes as human-review tasks alongside real-time operational health state
- **FR48:** Platform deployments are isolated at the infrastructure level; each operator deployment maintains its own independent data and event streams
- **FR49:** The platform can notify actors of state changes, assigned tasks, or time-sensitive events relevant to them
- **FR52:** The platform automatically emits health signals — projection lag, stream consumer status, unrouted task counts, component availability — to a dedicated observability store
- **FR55:** The platform includes a canonical reference implementation that serves as an integration test suite, developer onboarding starting point, and demonstrable vertical slice

## Non-Functional Requirements

### Performance

*Targets align with Success Criteria — Technical Success and are reproduced here with measurement context for engineering consumption.*

| Requirement | Target | Notes |
|------------|--------|-------|
| Write throughput | 10–100 ops/sec sustained | Single-cell MVP; Loftspace (~200 units) scale |
| Core KV capacity | Up to 100K keys | Vertices + aspects + links combined |
| CDC-to-projection lag | < 500ms p99 | Capability Lens and general Lenses; auth correctness depends on this ceiling |
| Starlark execution | < 100ms p99 per operation | Stream 0 spike validates before commit path design begins; target revised if spike exceeds threshold |
| End-to-end latency | < 2s p99 | Operation submission → projection visible in Lens target |
| Operation commit confirmation | Within CDC lag window | Actor can confirm durable commit before reading dependent projections |
| Local dev environment boot | < 3 minutes first run (image pull + DB init); < 30 seconds warm restart | `make up` from cold / warm start |
| "Hello Lattice" onboarding | < 60 minutes | `git clone` → completed vertical slice, verified by external tester |

### Reliability

- The Processor commit path is crash-recoverable; fault injection applied independently at each of the 10 commit path steps produces the same outcome as a clean run (idempotency defined in Data Integrity)
- Refractor (Lens) consumers resume from exactly their last committed offset after any restart; no events are skipped or double-processed
- Unrouted tasks are surfaced in Health KV observability and never silently dropped
- AI agent degradation produces explicit caveats and actionable guidance; no silent confidence on unverified state
- The `core-operations` operation ledger is append-only; no compaction occurs by default (compaction is a deployment configuration set by the operator)
- Single-cell Phase 1 does not require HA clustering; a single NATS server is acceptable for development and portfolio demonstration

### Security

- All data is encrypted at rest (NATS KV encryption) and in transit (TLS on all connections)
- The Capability Lens is the sole authorization boundary; architectural bypass is impossible by design — validated by the Phase 1 attempted-bypass test suite (4 bypass categories: direct KV write, stream publish outside `ops.*`, Starlark I/O escape, DDL schema violation)
- Starlark scripts execute in a sandbox with no access to external I/O, network, secrets, or non-deterministic state; validated by the Phase 1 Capability Lens adversarial test suite (4 attack vectors: role escalation via direct KV write, projection lag window exposure, Lens definition mutation via AI-authored op, cross-vertex permission bleed) — see Project Scoping section for full test vector definitions
- JWT/Lattice-Actor tokens are cryptographically signed; Gateway validates signatures before forwarding any operation
- PII aspects are encrypted with per-identity encryption keys; key material is held in an external KMS/HSM and never stored in Core KV
- Auth denial responses are specific and actionable; they do not expose internal permission graph structure beyond what the requesting actor requires
- Permission revocations are effective within the p99 CDC-to-projection lag ceiling (< 500ms); typical revocation propagation is materially faster under normal load
- **GDPR / CCPA:** crypto-shredding at the aspect level satisfies right-to-erasure without data deletion; sensitive aspects are irrecoverable after key shredding, non-sensitive aspects (decision outcomes, denial reasons, criteria) are retained
- **PCI DSS:** out of scope by design — raw payment credentials are never stored or processed by the platform
- **AI actor authority:** AI agent actors are represented as identity vertices within the graph and subject to the same Capability Lens authorization as human actors; no special actor class bypasses the standard permission path (testable: AI agent with consumer role cannot access finance data)
- **External operation auditability:** All external operations initiated by the orchestration engine are recorded as graph-visible state changes before the external call executes, ensuring audit trail completeness even for failed external operations

### Data Integrity

- Multi-key operations commit entirely or fail entirely; no partial state is observable by any reader at any point during the commit
- Every operation carries a unique, client-generated ID; re-delivery of the same operation produces an identical outcome with no side effects (idempotency); the Processor detects and short-circuits duplicate submissions before applying any state change
- The `core-operations` JetStream stream is the immutable, ordered source of truth for all platform state; no mutation, deletion, or reordering of committed entries is permitted
- Concurrent conflicting writes to the same vertex are detected via revision conditions within the same operation processing cycle; the platform retries up to a configured maximum before surfacing a conflict error to the caller; last-write-wins is not the resolution strategy
- DDL schema violations are rejected at the write path boundary; malformed entity type definitions cannot reach Core KV

### Scalability

- **Phase 1 ceiling (single-cell):** 100K keys in Core KV; 10–100 write ops/sec; single operator deployment sized for up to ~500 registered members and ~50 concurrent active sessions
- **Scale-out path:** multi-cell architecture (Phase 3) adds horizontal scale without data model changes; the cell-agnostic key design is validated in Phase 1 — keys embed no cell identity
- **Lens target (Postgres):** sized for single-building reporting at Phase 1; no sharding required
- **Operator isolation:** each operator deployment runs in its own isolated NATS cluster; no cross-tenant data access is possible at the infrastructure level

### Evolvability

- New entity types, business rules, and projection definitions activate within the CDC-to-projection lag window (< 500ms p99) without restart, recompilation, or data migration
- Existing business rules and projection definitions propagate changes to all active consumers within the same lag window, without restart
- Any capability change is revertable via compensating operation through the write path; no out-of-band data surgery or deployment is required
- Developers can run deterministic replay of any operation sequence against a Starlark rule or Lens definition in isolation, without a live NATS instance, for local business logic unit testing

### Operational Observability

- Health signals are updated at minimum every 10 seconds; a Health KV reader never sees state older than 10 seconds plus read latency
- Health state is readable via direct KV reads without a Lens projection or a running Refractor instance
- Health signals include at minimum: projection lag per Lens consumer, stream depth, consumer offset lag, unrouted task count, and component availability status
- Authorization failures are traceable across three observable planes: the graph permission path in Core KV, the Capability Lens projection definition, and the Capability KV cached read
- Phase 1 test harness fault injection covers: Processor crash at each of the 10 commit path steps, Refractor consumer restart mid-stream, NATS temporary unavailability, and Starlark evaluation timeout
