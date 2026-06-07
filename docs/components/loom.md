# Loom

**Component reference** | Audience: implementers + architects | Status: **Phase 2 — engine in build (Epic 8)** | Decided: 2026-06-01

> Decisions of record live in `_bmad-output/planning-artifacts/lattice-architecture.md` →
> "Phase 2 Architecture — Orchestration Core" (D2, D3, D5); data shapes are frozen in
> `docs/contracts/10-orchestration-surfaces.md` (§10.3/§10.5/§10.6/§10.9). Update this page in the
> same commit as the code; drift between page and code is a documentation bug.

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
own operational bucket `loom-state` (dash-named; keys may be dotted — `instance.<instanceId>` /
`token.<pendingToken>` / `outbox.<token>` / `deadline.<instanceId>`, Contract #10 §10.3).

---

## Core model

A **pattern** (package data, a Core KV meta-vertex like a Lens def) is an **ordered list of
steps**. A **step**:

| Field | Meaning |
|-------|---------|
| operation | the op to perform when the step runs (creates a task vertex, or submits a system op) |
| guard (optional) | an **on/off** predicate — if false, the step is **skipped** (cursor advances), no op emitted |

Step completion is **implicit** — there is no per-step completion-event field. A step is correlated
to its instance by a **unique token** (the `taskId` of the task it created, or the `requestId` of the
systemOp it submitted), resolved against the durable reverse pointer in `loom-state` (§10.6). The
pattern declares an optional **`completionDomains: ["<domain>", …]`** — the set of `events.<domain>.>`
the engine reconciles a durable consumer for (default `[subjectType]`); a flow completing in a domain
other than the subject's lists it explicitly (Contract #10 §10.5).

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
StartLoomPattern{patternRef, subjectKey}  →  outbox  →  events.loom.patternStarted
  └─ fixed events.loom.patternStarted consumer: validate patternRef, create the
     loom-state instance.<instanceId> cursor (instanceId = StartLoomPattern requestId),
     submit step 0
  └─ for cursor step: eval guard
       guard false → advance cursor, repeat
       guard true  → ONE atomic batch: write-ahead pendingToken + token.<token> pointer
                     + outbox.<token> op record + arm deadline.<instanceId> (TTL);
                     the relay then publishes the op from that record (e.g. create task
                     vertex; task.operation = the bound op the UI renders) ... WAIT ...
  ← completion event (user submits bound op, or system op commits) on a per-domain consumer
       → GET token.<requestId|taskId> → instance → advance cursor (atomic batch) → next step
  ⌛ deadline.<instanceId> TTL expiry (no completion seen) → read-before-act probe
       → GET vtx.op.<token>: committed → advance+alert; not yet relayed → re-arm; else → fail
  pattern exhausted → CompletePattern{instanceId} (via outbox) → events.loom.patternCompleted
```

The trigger is an **event**: `StartLoomPattern` is a `loom`-domain op (writes no business vertex) whose
commit emits `events.loom.patternStarted` the ordinary way (the event rides the `vtx.op.<requestId>.events`
outbox aspect); Loom runs a fixed durable consumer on that subject (always-on, independent of
`completionDomains`). Completion correlates by a **direct `token.<token>` GET** on the
durable reverse pointer — domain-independent and multi-instance-safe; the per-domain consumer only
decides *which events Loom sees*, never *which instance* (§10.6). Waiting for user input does not break
the loop — the advancing event is simply user-triggered.
**Long waits** (a user takes days) exceed the 24h idempotency horizon; handled at the engine
(extended-dedupe), not by changing the loop shape.

---

## What this component will own

| Path | Role |
|------|------|
| `internal/loom/` | Engine: pattern source (durable Core-KV subscription), Sensorium (per-domain + trigger consumers), Transition Engine (cursor advance + guard eval), Actuator (the command-outbox relay: `outbox.<token>` → `core-operations`), deadline watcher (timeout backstop), pattern interpreter |
| `internal/loom/control/` | Control plane — serves "which flows are running" by reading `loom-state` (analogous to `internal/refractor/control`; a future control-API story) |
| `cmd/loom/` | Binary entry point (extractable later; shares only `substrate/*`) |

**Engine vs package:** the interpreter, Sensorium, Transition Engine, Actuator are **engine
code**. Pattern definitions, guards, step→operation bindings, and the `task` type DDL are
**package data** (`task` DDL → foundational `orchestration-base`; specific flows →
`lease-signing` or an `identity` package).

---

## In-contracts (consumes)

| Contract | Source | Notes |
|----------|--------|-------|
| Pattern definitions | Core KV `vtx.meta.>` (package-installed) | Durable `SubscribeKVChanges` on the Core-KV backing stream, routed by class `meta.loomPattern` — loaded via CDC like Lens defs; live-registers new patterns without restart |
| `events.loom.patternStarted` trigger consumer | `core-events` (post-outbox) | **Fixed**, always-on durable consumer (independent of `completionDomains`); validates `patternRef`, creates the cursor, submits step 0 |
| `events.<domain>.>` per-domain completion consumer | `core-events` (post-outbox) | D2: one consumer per domain in a registered pattern's `completionDomains` (default `[subjectType]`), engine-reconciled; correlates by `requestId`/`taskId` in the event body |
| Current Core KV state | point-reads for guard evaluation | Guards are pure over this snapshot |

## Out-contracts (produces)

| Output | Target | Notes |
|--------|--------|-------|
| Step operations | Processor via `core-operations` | Submitted via the **command outbox**: written as `outbox.<token>` in the transition batch, fire-and-forget published by the relay (no dual write, no request-reply) |
| `loom.patternStarted` / `Completed` / `Failed` | **lifecycle** ops (`StartLoomPattern`/`CompletePattern`/`FailPattern`) → outbox → `core-events` | Lifecycle on the first-class `loom` domain; no Core-KV business vertex (events ride the standard `vtx.op.<requestId>.events` outbox aspect); drives nesting + Weaver re-projection |
| Instance cursor + token index + outbox + deadline | `loom-state` (own bucket) | `instance.<id>` cursor + `token.<token>` reverse pointer + `outbox.<token>` op record + `deadline.<instanceId>` (TTL); one atomic batch per transition |
| Tasks | **Core KV** (via Processor) | Business state — queryable, UI-rendered, audited, read by Weaver target Lens |

---

## State & crash-safety

| State | Where | Why |
|-------|-------|-----|
| **Tasks** (+ assignment links, completion) | **Core KV** | Business-meaningful, cross-component, audited |
| **Instance cursor + token index** (pattern ref, step pointer, run status, reverse pointer) | **`loom-state`** | Single-component orchestration bookkeeping (P1 boundary); the instance has **no Core-KV vertex** — its sole durable home is the cursor |

The instance is **operational-only**: there is no Core-KV instance vertex — `loom-state` is its sole
durable home (P1). Each step transition is a **single `substrate.AtomicBatch`** that, all-or-nothing,
updates the `instance.<id>` cursor, writes the new `token.<newToken> → instanceId` pointer, deletes the
prior `token.<oldToken>`, **writes the `outbox.<token>` op record, and arms `deadline.<instanceId>` (TTL)**.
Because the op-to-submit lives in the same batch (the **command outbox**), submission is no longer a dual
write: the relay publishes it fire-and-forget and deletes the record on publish-ack (re-publish idempotent
via the chosen `requestId` + the Contract #4 tracker). Write-ahead therefore holds by construction.

Correlation on a completion is a **direct `token.<token>` GET** — durable, domain-independent, and
**multi-instance-safe**: any engine replica resolves any token via the bucket (no in-memory index, no
startup rebuild barrier). Idempotency is by **pointer presence**: pointer gone (step already advanced,
deleted in the batch) → drop/ack, no re-advance. The durable per-domain consumers resume from their
last ack, so a redelivered completion mid-restart resolves against the durable pointer regardless of
engine age.

> A skipped step (guard false) and a not-yet-reached step both have "no task" — they are
> distinguishable **only** by replaying guards. This is why guard purity is binding, not a
> preference.

**Queryability** ("which flows are running") is served by Loom's **control plane** (reading
`loom-state`), analogous to Refractor's `internal/refractor/control` — **not** Core KV. A Refractor lens
over the `loom.*` event stream remains an option for a durable read model if one is later wanted.

---

## Failure modes

| Mode | Behavior |
|------|----------|
| Poison event in a domain | Head-of-line blocks that domain's consumer only (domain-scoped blast radius, D2) |
| Engine restart / replica change | Durable per-domain consumers resume from last ack; completion resolves via the durable `token.<token>` pointer (no in-memory index to rebuild) |
| Long-waiting instance > 24h | Extended-dedupe at engine (idempotency horizon, arch §85) |
| Crash mid-step | Write-ahead atomic batch (pointer + cursor + outbox record before any side effect); the relay re-publishes the `outbox.<token>` op on resume, collapsing on the Contract #4 tracker → re-drive safely; pointer presence is the idempotency guard |
| Relay publish fails | The outbox record persists; the relay Naks → JetStream redelivers → re-publish (idempotent). Submission cannot be lost between batch and broker |
| Rejected / failed / unseen step | Off-stream terminal (a rejected op writes no tracker/event) — learned via the `deadline.<instanceId>` TTL expiry + a read-before-act probe (`GET vtx.op.<token>`: committed → advance+alert; not yet relayed → re-arm; else → `status=failed`). Never the submit reply; never wedges |

---

## Principles that apply

- **P2** — Processor is the sole Core KV writer / event producer; Loom is a client (tasks and the
  `loom.*` lifecycle events go through the ledger / outbox — never a direct Core-KV write or publish).
- **P1** — tasks are vertices (business state); the instance cursor is operational state (`loom-state`),
  with **no** Core-KV instance vertex.
- **Decision #10** — engine is minimal/generic; flows are packages.
- **Module boundary** — `loom` imports only `substrate/*`; talks to Weaver/Processor via NATS,
  never Go calls.

## Deferred (Phase 2+)

- External-call steps in Loom (a deterministic *saga* with outbound calls) — would require
  promoting the Two-Phase Nudge actuator to a shared package. Today external calls are
  Weaver-owned. Flagged, not built.
- Guard expression surface — pure-predicate language TBD at implementation (simple declarative
  field-presence checks vs. a restricted pure-Starlark subset). Must remain side-effect-free.
