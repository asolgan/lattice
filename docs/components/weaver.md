# Weaver

**Component reference** | Audience: implementers + architects | Status: **Phase 2 — design (no code yet)** | Decided: 2026-06-01

> Design page authored in the Phase 2 architecture sprint. Decisions of record live in
> `_bmad-output/planning-artifacts/lattice-architecture.md` → "Phase 2 Architecture —
> Orchestration Core" (D3, D4) and PRD-Alignment Item 3 (Two-Phase Nudge). Update this page in
> the same commit as the code; drift is a documentation bug.

---

## Overview

Weaver is the **convergence engine** — it drives the graph toward declared **target states** by
detecting discrepancies and remediating them, optionally by triggering Loom utilities. It is
the declarative counterpart to Loom (brainstorming #122): **Weaver decides *what* is missing;
Loom executes a *fixed procedure* to fill a gap.** A "Lease Application complete" is a *target
state*, not a workflow.

Crucially, **a Weaver target is a Lens** — Weaver is a **consumer of the Refractor**, never its
own cypher runtime. The Refractor projects "currently-violating" rows; Weaver reacts.

Weaver is an **internal service actor** at root-equivalent capability. It **submits operations
through the Processor** (never writes Core KV directly). Its direct writes are to `weaver.state.>`
(dispatch/bookkeeping) and `weaver.claims.>` (Two-Phase Nudge claims).

---

## Pipeline

```
Sensorium (3-lane work stream weaver.work.>)
   → Evaluator (L1 re-confirm + in-flight dedup; L2 hydrate + classify gap + select playbook)
   → Strategist (playbook registry: gap-type → action)
   → Actuator (OCC ops via Processor; trigger Loom via op; external via Two-Phase Nudge)
```

### The 3 lanes (`weaver.work.>`)

Each lane exists because the others structurally cannot see its violations:

| Lane | Trigger | Rationale |
|------|---------|-----------|
| **1. Violation-driven** | a row in a target Lens's output (CDC over the target KV) | the main path; Refractor keeps the target live |
| **2. Event-driven (targeted-audit)** | a `core-events` event → re-evaluate only the touched subgraph | for targets too costly to keep continuously projected — **built but unexercised in the Phase 2 demo** |
| **3. Temporal** | NATS scheduled-message expiry (ADR-51) | time-derived violations emit no CDC (e.g. "bgcheck older than 90d") |

### Evaluator tiers

| Tier | Job | Phase 2 |
|------|-----|---------|
| **L1** | re-confirm row is still violating; drop if already in-flight (`weaver.state.>`) | ✅ |
| **L2** | hydrate context, classify the specific gap, select playbook input | ✅ |
| **L3** | AI-assisted reasoning for ambiguous/novel discrepancies | **deferred → Phase 3** |

### Strategist — playbook registry (package data)

A **playbook** maps gap-type → action. Engine is a generic dispatcher; playbooks ship in the
package (`lease-signing`):

| Gap (example) | Action |
|---------------|--------|
| `missing_onboarding` | submit `StartOnboarding` op → **triggers Loom** (op-based, D3) |
| `missing_bgcheck` | **Two-Phase Nudge** → background-check adapter |
| `missing_payment` | Two-Phase Nudge (Stripe) or trigger a Loom collect-payment flow |
| `missing_signature` | assign a sign **task** (direct op) |

### Actuator

- **OCC** — every op carries a revision-condition (substrate per-key revisions) so
  two ticks can't double-apply.
- **Triggers Loom via an op** — auditable, idempotent ledger entry (not a Go call; engines share
  only `substrate/*`).
- **External actions** — Two-Phase Nudge (below).

---

## Targets as Lenses (D4)

Targets project **one row per *candidate* entity with a `violating` boolean** (+ gap columns +
authz-anchor), **not** row-only-when-violating — a gap closing flips the flag via a normal
**upsert** (already supported); only true entity deletion deletes a row (`IsDeleted`, already
handled). This **avoids forcing Refractor retraction work** in Phase 2.

```
weaver-targets.<target>.>   (NATS-KV bucket, existing nats_kv adapter)
  row: { entity_key, applicant_id, violating, missing_onboarding, missing_bgcheck,
         missing_payment, missing_signature, <authz-anchor> }
```

Weaver **watches** the bucket (lane 1). A satisfied entity has `violating=false`; the row
vanishes only on true deletion. (True "emit-only-when-violating" + Refractor
negative/filter-retraction projection is a **deferred** scale-time capability.)

The freshness rule lives **in the target cypher**: `missing_bgcheck = NOT EXISTS(check WHERE
date > now − window)`.

---

## Temporal lane — NATS scheduled messages (ADR-51)

Time-derived violations emit no CDC, so Weaver converts **time into an op** using NATS native
message scheduling (ADR-51, stable in 2.14):

```
Actuator resolves bgcheck
  → publish @at(resolve + window) scheduled message on the LATTICE-WIDE `lattice-schedules`
       stream (platform infra, not Weaver-owned), subject lattice.schedule.lease.bgcheck.<checkId>
  ... NATS holds it (durable across restart) ...
  → at expiry NATS republishes to Weaver's chosen target subject (weaver.timer.fired.>)
  → temporal lane consumes → submits MarkCheckExpired op via Processor
  → CDC + outbox event → target Lens re-projects → missing_bgcheck flips true
```

- **Durable** across restart; **replace-on-reschedule** (re-doing a check before expiry
  re-publishes to the same subject, replacing the prior timer; one schedule per subject → key by
  entity id).
- **Never injected into `core-events` directly** — the transactional outbox stays
  the sole event producer; the timer fires an internal subject that becomes a normal **op**.
- Uses the **lattice-wide** `lattice-schedules` stream (`AllowMsgSchedules: true`, bootstrapped as
  platform infra in Epic 7 — not owned by Weaver). No custom scheduler subsystem; full
  scheduler/op-vertex-pruner (#47/#49) remain Phase 2+ maturity.

---

## Two-Phase Nudge (external idempotency, FR58 — PRD-Alignment Item 3)

External adapter framework lives in `internal/weaver/nudge/`. The **framework is engine**; a
**reference adapter** (`FakeStripe`, `FakeBackgroundCheck`) proves it (demo uses mocked
adapters — the External Adapter framework is proven by a reference adapter; real Stripe is
Phase 3).

```
1. Claim   → write weaver.claims.<claim-id> (direct KV; intent recorded BEFORE the call)
2. Execute → call the external (mocked) adapter; claim prevents any other instance re-initiating
3. Resolve → submit a normal op via Processor recording the result, carrying claim-id reference
```

Claims retained (default 90d) in `weaver.claims.>`; audit joins Core KV (business outcome) to
the claim (operational intent).

---

## What this component will own

| Path | Role |
|------|------|
| `internal/weaver/` | Engine: Sensorium, 3-lane work stream, Evaluator tiers, Strategist dispatcher, Actuator |
| `internal/weaver/nudge/` | External Adapter framework + Two-Phase Nudge |
| `cmd/weaver/` | Binary entry point (extractable; shares only `substrate/*`) |

**Package data:** target Lens cypher, playbook definitions, gap→action mappings, mocked-adapter
config (`lease-signing`).

---

## In / Out contracts

| Direction | Contract | Notes |
|-----------|----------|-------|
| In | `weaver-targets.<target>.>` KV watch | lane 1 (primary input — **not** the core-events consumer) |
| In | `events.<domain>.>` per-domain consumer | lane 2 targeted-audit only |
| In | `weaver.timer.fired.>` internal subject | lane 3 (from ADR-51 scheduled messages) |
| Out | ops via `core-operations` | OCC; trigger-Loom; resolve mutations carry `claim-id` |
| Out (own) | `weaver.state.>` | in-flight convergence (anti-storm), TTL-leased |
| Out (own) | `weaver.claims.>` | Two-Phase Nudge claims (90d retention) |

---

## Failure modes

| Mode | Behavior |
|------|----------|
| Re-trigger storm | violation persists until gap closes *and* re-projects → `weaver.state.>` in-flight mark suppresses re-trigger |
| **Actuator crash mid-flight** | in-flight marks carry a **TTL/lease**; reconciliation reclaims expired leases so a target is never wedged. *(Test: kill mid-flight.)* |
| External call retried/failed | Two-Phase Nudge claim prevents double-charge; resolve is idempotent |
| Target too costly to keep live | lane-2 on-demand evaluation (deferred-exercise) |

---

## Principles that apply

- **P2** — Processor is the sole Core KV writer; Weaver is a client. Claims/state are
  operational KV, not Core KV (P1).
- **P4** — Weaver enforces declarative convergence invariants; Starlark enforces single-op
  invariants only.
- **Weaver targets ARE Lenses** — Weaver consumes the Refractor; it is not a cypher runtime.
- **Module boundary** — `weaver` imports only `substrate/*`; triggers Loom via NATS/op.

## Deferred (Phase 2+)

- Refractor negative/filter-retraction projection (true emit-only-when-violating).
- Lane-2 on-demand evaluation (built, unexercised in demo).
- L3 evaluator (AI-assisted).
- Full temporal scheduler / op-vertex pruner (#47/#49).
- Real external adapters (Stripe/background-check) — Phase 3 integration.
