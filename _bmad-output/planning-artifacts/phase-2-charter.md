# Lattice Phase 2 — Charter (pointer)

**Status:** Planning complete (charter 2026-06-01 · architecture sprint + epics/stories 2026-06-01→02).

> This document was the pre-sprint charter. Its decisions and stories have been **absorbed into the
> authoritative docs below** and are maintained there — this page is now a thin pointer to avoid
> two copies drifting. Do not add decision detail here; add it to the source of record.

## Where the live content lives

| What | Source of record |
|------|------------------|
| **Phase 2 decisions (D1–D5)** + rationale | [`lattice-architecture.md`](./lattice-architecture.md) → "Phase 2 Architecture — Orchestration Core" |
| **Epics & stories** (Epics 7–11, 17 stories) | [`epics.md`](./epics.md) → "Phase 2 Epics — Detailed Stories" |
| **Engine reference** (Loom / Weaver) | [`/docs/components/loom.md`](/docs/components/loom.md), [`/docs/components/weaver.md`](/docs/components/weaver.md) |
| **Data-contract shapes** (DESIGN, pending freeze) | [`/docs/contracts/10-orchestration-surfaces.md`](/docs/contracts/10-orchestration-surfaces.md) |
| **Cross-session summary** | memory `project_phase_2_charter` (concise, for fast load) |

## One-paragraph orientation

**Thesis:** Lattice can converge a target state by orchestrating utilities, idempotently, against
the real world — proven by one installable package. **Scope:** orchestration core only — Loom +
Weaver + Two-Phase Nudge (FR26, FR27, FR29, FR30, FR58); read-path auth / Gateway / Vault /
AI-authoring / console / historical-query → Phase 3. **Delivery:** Loom/Weaver are core engines
(`internal/loom`, `internal/weaver`); the Loftspace lease-application ships as an installable
`lease-signing` package — the demo dogfoods that the package model carries orchestration content.

## ⛔ Next step before implementation

A **dedicated Loom/Weaver data-contracts session** (mirroring the pre-Phase-1 Processor contracts
work) hardens `docs/contracts/10-orchestration-surfaces.md` from DESIGN → frozen. Implementation
(Epic 7 first, session-per-story) starts only against frozen contracts.

---

*Workflow rules (carried from Phase 1.5): no sprints; session-per-story; Winston runs CS→DS→CR via
sub-agents that don't commit, adjudicates/commits/watches CI; comment policy in the
`lattice-architecture.md` Anti-Patterns table; new docs land in `/docs`.*
