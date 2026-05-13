---
title: Lattice Phase 1 Token Usage Tracker
purpose: Track actual implementation-session token usage against per-story budgets; flag model-tier deviations early.
maintainedBy: Winston (architecture lead)
updateCadence: Per implementation session at close
---

# Lattice Phase 1 — Token Usage Tracker

## Operating Rules

- **Each story runs in its own implementation session** (fresh context per story).
- **Each story's budgeted tier is locked** — Sonnet stories MUST run on Sonnet; Opus stories MUST run on Opus. Deviations require Winston's sign-off and a note in the table.
- **At session close**: record actual input+output tokens consumed; compute delta vs budget.
- **Overrun threshold:** if actual > 1.2 × budget, mark `OVERRUN` and add a root-cause note (under-scoped story / mid-implementation architecture surface / external blocker).
- **Underrun is not a problem** but a > 50% underrun should be noted — may indicate the story was over-scoped or that some AC was deferred.

## Per-Story Ledger

| Story | Title | Model (planned) | Model (actual) | Budget | Actual | Δ | Status | Session Date | Notes |
|---|---|---|---|---|---|---|---|---|---|
| 1.1 | NATS Atomic Batch Spike | Sonnet | — | 52K | — | — | PENDING | — | Critical spike. Gate 1 dependency. |
| 1.2 | Starlark Execution Spike | Sonnet | — | 65K | — | — | PENDING | — | Critical spike. Gate 1. |
| 1.3 | Dev Harness with Primordial Bootstrap | Sonnet | — | 95K | — | — | PENDING | — | Adds Docker, NATS, Postgres, both Capability Lenses seeded. |
| 1.4 | `internal/substrate` Package | Opus | — | 110K | — | — | PENDING | — | NanoID, key helpers, envelopes, KV helpers. |
| 1.5 | Processor — Consume, Dedup & Auth Stub (Steps 1-3) | Opus | — | 115K | — | — | PENDING | — | |
| 1.6 | Processor — Starlark Sandbox & JIT Hydration (Steps 4-5) | Opus | — | 130K | — | — | PENDING | — | |
| 1.7 | Processor — DDL Validation & Atomic Batch (Steps 6-8) | Opus | — | 145K | — | — | PENDING | — | |
| 1.8 | Processor — Event Publication & Fault Injection (Steps 9-10) | Opus | — | 145K | — | — | PENDING | — | NFR-R1 fault injection harness. |
| 1.9 | Write-Scope Enforcement per DDL (FR57) | Sonnet | — | 85K | — | — | PENDING | — | |
| 1.10 | Attempted Bypass Test Suite (Gate 2) | Sonnet | — | 105K | — | — | PENDING | — | Phase 1 Gate 2 closure. |
| 2.1 | Materializer → Refractor Morph | Opus | — | 145K | — | — | PENDING | — | Largest single story. |
| 2.2 | Functional Gap Analysis | Opus | — | 130K | — | — | PENDING | — | Closing artifact for Epic 2. |
| 3.1 | openCypher `full` Engine Integration | Opus | — | 135K | — | — | PENDING | — | `go-opencypher` library. |
| 3.2 | Capability Lens Activation & Capability KV Projection | Opus | — | 140K | — | — | PENDING | — | Both Capability Lenses live. |
| 3.3 | Processor Step 3 — Capability KV Authorization | Opus | — | 125K | — | — | PENDING | — | Auth stub → real. |
| 3.4 | Structured Denial Response (FR22) | Sonnet | — | 95K | — | — | PENDING | — | |
| 3.5 | Three-Plane Auth Failure Traceability (FR23) | Sonnet | — | 95K | — | — | PENDING | — | |
| 3.6 | Role-Scoped Access Domain & Audit (FR24, FR25) | Sonnet | — | 100K | — | — | PENDING | — | |
| 3.7 | Capability Lens Adversarial Test Suite (Gate 3) | Sonnet | — | 110K | — | — | PENDING | — | Phase 1 Gate 3 closure. |
| 4.1 | Identity Domain DDL & State Machine | Opus | — | 120K | — | — | PENDING | — | |
| 4.2 | Staff Creates Unclaimed Identity (FR1) | Sonnet | — | 90K | — | — | PENDING | — | |
| 4.3 | Two-Phase Identity Claim (FR2, FR5) | Sonnet | — | 100K | — | — | PENDING | — | |
| 4.4 | Duplicate Identity Detection (FR3) | Sonnet | — | 110K | — | — | PENDING | — | |
| 4.5 | Staff-Approved Identity Merge (FR4) | Opus | — | 135K | — | — | PENDING | — | |
| 5.1 | DDL Self-Description Aspects (FR19 substrate) | Opus | — | 115K | — | — | PENDING | — | |
| 5.2 | Cold-Start AI Agent Traversal & Operation Submission | Sonnet | — | 115K | — | — | PENDING | — | FR19 north-star integration test. |
| 5.3 | Compensating Operation & DDL Rollback (Gate 4) | Opus | — | 130K | — | — | PENDING | — | Phase 1 Gate 4 closure. |
| 6.1 | Lattice CLI Tool (FR45) | Sonnet | — | 120K | — | — | PENDING | — | |
| 6.2 | Health KV Schema & Completeness (FR46, FR52) | Sonnet | — | 90K | — | — | PENDING | — | |
| 6.3 | Deployment Isolation Specification (FR48) | Sonnet | — | 70K | — | — | PENDING | — | |
| 6.4 | "Hello Lattice" Reference Implementation (Gate 5) | Sonnet | — | 130K | — | — | PENDING | — | Phase 1 Gate 5 closure. External tester required. |

## Rolling Totals

| Tier | Budget (K tokens) | Actual (K tokens) | Δ | Stories complete |
|---|---|---|---|---|
| Opus | 1,820 | 0 | 0 | 0 / 12 |
| Sonnet | 1,627 | 0 | 0 | 0 / 19 |
| Haiku | 0 | 0 | 0 | 0 / 0 |
| **Phase 1 Total** | **3,447** | **0** | **0** | **0 / 31** |

## Update Procedure (For Each Implementation Session Close)

1. Read the session's total token usage (from session metadata or transcript count).
2. Fill in `Model (actual)`, `Actual`, and `Session Date` for the row.
3. Compute Δ = Actual − Budget. If Actual > 1.2 × Budget, set Status to `OVERRUN` and add root-cause note.
4. If Status would be `OVERRUN`, raise to Winston for review before closing the session.
5. Update Rolling Totals.
6. Commit this file as part of the story's closing commit (alongside the implementation artifacts).
