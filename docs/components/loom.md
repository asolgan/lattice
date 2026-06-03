# Loom

**Component reference** | Audience: implementers + architects | Status: **Phase 2 — design (no code yet)** | Decided: 2026-06-01

> Design page authored in the Phase 2 architecture sprint. Decisions of record live in
> `_bmad-output/planning-artifacts/lattice-architecture.md` → "Phase 2 Architecture —
> Orchestration Core" (D2, D3, D5). Update this page in the same commit as the code once
> implementation begins; drift between page and code is a documentation bug.

---

## Overview

Loom is the **deterministic procedure engine** — a generic interpreter that drives a
**pre-determined, linear sequence of steps** to completion. It is *not* inherently
user-facing: a step may be a **user-task** (collect/verify a field via a task assigned to an
identity) or a **system-op** (e.g. a tenant-provisioning saga: `createTenant → seedRoles →
createWorkspace → markReady`). Loom ships **zero domain knowledge** — patterns are package
data; the engine interprets them.

Loom is the imperative counterpart to Weaver's declarative convergence (brainstorming #122):
**Loom = "do these things in this order"; Weaver = "this target state must hold, converge to
it."** When conditional/branching logic appears, that is the signal the work belongs to Weaver,
not a Loom branch.

Loom is an **internal service actor** at root-equivalent capability. It **submits operations
through the Processor** — it never writes Core KV directly. Its only direct writes are to its
own operational bucket `loom.state.>`.

---

## Core model

A **pattern** (package data, a Core KV meta-vertex like a Lens def) is an **ordered list of
steps**. A **step**:

| Field | Meaning |
|-------|---------|
| operation | the op to perform when the step runs (creates a task vertex, or submits a system op) |
| completion event | the `core-events` event that advances the cursor past this step |
| guard (optional) | an **on/off** predicate — if false, the step is **skipped** (cursor advances), no op emitted |

**Binding constraints:**

- **Linear only** — no branches, no loops, no fan-out. Conditional *paths* → Weaver.
- **Guards are pure, deterministic predicates over current state.** This is what makes the
  instance cursor rebuildable (see State). No side effects, no external reads, no Starlark
  with I/O.
- Guard semantics give the **"collect vs verify" reuse**: the same `[name, phone, address]`
  pattern serves first-time collection (guards false → all become tasks) and re-verification
  (guards skip fields already present).

### Execution loop

```
trigger (op from Weaver, or a StartX op)  →  instantiate pattern for subject
  └─ for cursor step: eval guard
       guard false → advance cursor, repeat
       guard true  → Actuator submits the step operation (e.g. create task vertex
                     assigned to identity; task.operation = the bound op the UI renders)
                     ... WAIT ...
  ← completion event (user submits bound op, or system op commits) on the per-domain consumer
       → advance cursor → next step
  pattern exhausted → emit <Flow>Complete event
```

Waiting for user input does not break the loop — the advancing event is simply user-triggered.
**Long waits** (a user takes days) exceed the 24h idempotency horizon; handled at the engine
(extended-dedupe), not by changing the loop shape.

---

## What this component will own

| Path | Role |
|------|------|
| `internal/loom/` | Engine: Sensorium (event consumer), Transition Engine (cursor advance + guard eval), Actuator (op submission), pattern interpreter |
| `cmd/loom/` | Binary entry point (extractable later; shares only `substrate/*`) |

**Engine vs package:** the interpreter, Sensorium, Transition Engine, Actuator are **engine
code**. Pattern definitions, guards, step→operation bindings, and the `task` type DDL are
**package data** (`task` DDL → foundational `orchestration-base`; specific flows →
`lease-signing` or an `identity` package).

---

## In-contracts (consumes)

| Contract | Source | Notes |
|----------|--------|-------|
| `events.<domain>.>` per-domain durable consumer | `core-events` (post-outbox) | D2: one consumer per referenced domain, engine-reconciled from declared bindings; engine fans out to registered patterns |
| Pattern definitions | Core KV meta-vertices (package-installed) | Loaded via CDC like Lens defs |
| Trigger ops | Weaver Actuator (op-based, D3) or a client `StartX` op | Auditable ledger entry starts a flow |
| Current Core KV state | point-reads for guard evaluation | Guards are pure over this snapshot |

## Out-contracts (produces)

| Output | Target | Notes |
|--------|--------|-------|
| Step operations | Processor via `core-operations` | Create task vertices / submit system ops; OCC where relevant |
| `<Flow>Complete` / progress events | Core KV mutations → outbox → `core-events` | Drives Weaver target re-projection |
| Instance cursor | `loom.state.>` (own bucket) | Bookkeeping; rebuildable (see below) |
| Tasks | **Core KV** (via Processor) | Business state — queryable, UI-rendered, audited, read by Weaver target Lens |

---

## State & crash-safety

| State | Where | Why |
|-------|-------|-----|
| **Tasks** (+ assignment links, completion) | **Core KV** | Business-meaningful, cross-component, audited |
| **Instance cursor** (pattern ref, step pointer, run status) | **`loom.state.>`** | Single-component orchestration bookkeeping (P1 boundary) |

The cursor is a **rebuildable cache**: because guards are pure, recovery **replays the guards**
against current Core KV tasks — cursor = first step whose guard is true and whose task is
incomplete. If `loom.state.>` is lost, Loom re-derives from the ledger. Source of truth stays in
Core KV; the durable per-domain consumer resumes from its last ack.

> A skipped step (guard false) and a not-yet-reached step both have "no task" — they are
> distinguishable **only** by replaying guards. This is why guard purity is binding, not a
> preference.

---

## Failure modes

| Mode | Behavior |
|------|----------|
| Poison event in a domain | Head-of-line blocks that domain's consumer only (domain-scoped blast radius, D2) |
| `loom.state.>` loss | Rebuild cursor by replaying guards over Core KV tasks |
| Long-waiting instance > 24h | Extended-dedupe at engine (idempotency horizon, arch §85) |
| Crash mid-step | Durable consumer + idempotent op submission (OCC) → re-drive safely |

---

## Principles that apply

- **P2** — Processor is the sole Core KV writer; Loom is a client (tasks go through the ledger).
- **P1** — tasks are vertices (business state); the cursor is operational state (`loom.state.>`).
- **Decision #10** — engine is minimal/generic; flows are packages.
- **Module boundary** — `loom` imports only `substrate/*`; talks to Weaver/Processor via NATS,
  never Go calls.

## Deferred (Phase 2+)

- External-call steps in Loom (a deterministic *saga* with outbound calls) — would require
  promoting the Two-Phase Nudge actuator to a shared package. Today external calls are
  Weaver-owned. Flagged, not built.
- Guard expression surface — pure-predicate language TBD at implementation (simple declarative
  field-presence checks vs. a restricted pure-Starlark subset). Must remain side-effect-free.
