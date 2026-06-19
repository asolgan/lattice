# Loom

**Component reference** | Audience: implementers + architects

> Decisions of record live in `_bmad-output/planning-artifacts/lattice-architecture.md` →
> "Phase 2 Architecture — Orchestration Core" (D2, D3, D5); data shapes are frozen in
> `docs/contracts/10-orchestration-surfaces.md` (§10.3/§10.5/§10.6/§10.9). Update this page in the
> same commit as the code; drift between page and code is a documentation bug.

---

## Overview

Loom is the **deterministic procedure engine** — a generic interpreter that drives a
**pre-determined, linear sequence of steps** to completion. It is *not* inherently
user-facing: a step may be a **user-task** (collect/verify a field via a task assigned to an
identity), a **system-op** (e.g. a tenant-provisioning saga: `createTenant → seedRoles →
createWorkspace → markReady`), or an **external-task** (dispatch an idempotent external call via the
**bridge** and await its result — Contract #10 §10.5/§10.6). Loom ships **zero domain knowledge** —
patterns are package data; the engine interprets them.

Loom is the imperative counterpart to Weaver's declarative convergence (brainstorming #122):
**Loom = "do these things in this order"; Weaver = "this target state must hold, converge to
it."** When conditional/branching logic appears, that is the signal the work belongs to Weaver,
not a Loom branch.

Loom is an **internal service actor** at root-equivalent capability. It **submits operations
through the Processor** — it never writes Core KV directly. Its only direct writes are to its
own operational bucket `loom-state` (dash-named; keys may be dotted — `instance.<instanceId>` /
`instance.<instanceId>.pattern` / `token.<pendingToken>` / `outbox.<token>` /
`deadline.<instanceId>`, Contract #10 §10.3).

---

## Core model

A **pattern** (package data, a Core KV meta-vertex like a Lens def) is an **ordered list of
steps**. A **step**:

| Field | Meaning |
|-------|---------|
| operation | the op to perform when the step runs (creates a task vertex, or submits a system op) |
| guard (optional) | an **on/off** predicate — if false, the step is **skipped** (cursor advances), no op emitted |

Step completion is **implicit** — there is no per-step completion-event field. A step is correlated
to its instance by a **unique token** (the `taskKey` (`vtx.task.<id>`) of the task it created, read from
`payload.taskKey` on the completion event, or the `requestId` of the systemOp it submitted), resolved
against the durable reverse pointer in `loom-state` (§10.6). The pattern declares an optional
**`completionDomains: ["<domain>", …]`** — the set of `events.<domain>.>` the engine reconciles a durable
consumer for (default `[subjectType]`); a flow completing in a domain other than the subject's lists it
explicitly (Contract #10 §10.5). Every event class is `<domain>.<eventName>` (Contract #3 §3.4), so a
**userTask completes on the `orchestration` domain** — the `orchestration.taskCompleted` event — and an
all-userTask onboarding pattern declares `completionDomains: ["orchestration"]` (NOT `["identity"]`).

**Binding constraints:**

- **Linear only** — no branches, no loops, no fan-out. Conditional *paths* → Weaver.
- **Guards are pure, deterministic predicates over current state.** This is what makes the
  instance cursor rebuildable (see State). No side effects, no external reads, no Starlark
  with I/O.
- Guard semantics give the **"collect vs verify" reuse**: the same `[name, phone, address]`
  pattern serves first-time collection (guards false → all become tasks) and re-verification
  (guards skip fields already present).

### Definition binding (pinning)

**Definitions bind at instance start.** When the trigger consumer creates an instance, it writes a
full copy of the pattern — as loaded at trigger time — to `instance.<instanceId>.pattern` in the
**same `AtomicBatch`** that creates `instance.<instanceId>`, and every subsequent step resolution
(advance, completion, deadline recovery) reads that **pinned** copy, never the live pattern source.
A pattern update mid-flight therefore affects **NEW instances only**: an in-flight instance
completes under the definition it started with, and its durable cursor can never be mis-indexed
against reordered/inserted/changed steps. The pin is deleted in the same terminal batch that flips
the instance to `complete`/`failed`, so listing `instance.*.pattern` keys yields exactly the live
set (this drives the reconcile union, below). A missing pin for a live running instance is an
invariant break (the pin is written atomically with the instance) — surfaced as an error, never a
silent fallback to the live source. The live source remains authoritative only for **new triggers**
(which pin from it), consumer reconcile, and load-time validation; a userTask's `forOperation`
(operationType → op meta-vertex key) also resolves live, because the task grant must reference the
op definition as it exists when the task is created.

**Disaster recovery re-binds to the CURRENT definition.** Total `loom-state` loss destroys the pin
along with the cursor; a fresh `StartLoomPattern` (the narrow recovery semantics) starts a
new instance that pins **today's** definition. Recovery is re-convergence under today's truth; the
guard-replay properties below make that safe. Do **not** attempt event-embedded pins to preserve a
dead generation's definition: `core-events` has `MaxAge=7d` while a userTask wait is unbounded, so
such a pin would evaporate mid-instance (analyzed and rejected).

**Authoring guidance:** a **semantic** redefinition of a flow deserves a **new pattern DDL/id** —
in-flight instances of the old id then drain under their pinned (old) definition while new starts
target the new id explicitly. Mechanical edits (wording, an inserted cheap step, a tightened guard)
may safely update a pattern in place; only new instances see them.

### Guard grammar

A guard is a **declarative** predicate (Contract #10 §10.5), parsed and validated at pattern-load
time and rejected wholesale if malformed (`internal/loom/pattern.go` `validate()` →
`internal/loom/guard.go` `parseGuard`, same doctrine as an unknown `kind`):

- **Atoms** — `{"absent": <path>}`, `{"present": <path>}`, `{"equals": {"path": <path>, "value": <any>}}`.
- **Composition** — `{"allOf": [<guard>…]}`, `{"anyOf": [<guard>…]}`, `{"not": <guard>}`, composed
  into ONE boolean (never branching). An empty `allOf`/`anyOf` list is a validate error.
- **Paths are explicit**, exactly two shapes: `subject.data.<field>` (root vertex's own `data`
  envelope) or `subject.<aspect>.data.<field>` (point-read the `<subjectKey>.<aspect>` aspect, read
  its `data.<field>`). Any other shape is rejected at parse time. Guards read **only** the subject +
  its aspects — no link-walking.
- **Pinned absence semantics** (binding): `absent` = the path resolves to **null / missing /
  soft-deleted aspect (`isDeleted`) / (for strings) empty-after-trim**; `present` = not absent. An
  empty-string-after-trim is **absent**; `"0"` / `false` / `0` are **present** (never "falsy"). An
  absent path never `equals` anything (including a `null`/`""` comparand).
- **Starlark escape hatch (`{"reads": […], "starlark": "…"}`) is RESERVED** — recognized at parse
  time and rejected with a precise "reserved, not yet supported" error. The pure-evaluator extraction
  lands only when the first Starlark guard is authored (§10.5); declarative-only ships without it.

Hydration is **per-evaluation** (no cross-step cache): at step entry the engine JIT-reads the subject
root + the referenced aspects from Core KV and resolves the path. The loom-local resolver
(`internal/loom/guard_eval.go`) mirrors the Refractor executor's `resolveProperty` /
`fetchNode` aspect-navigation and tombstone check
(`internal/refractor/ruleengine/full/executor.go:1270-1290` / `:453-476`) — re-implemented, not
imported (loom imports only `substrate/*` + stdlib). Within one guard evaluation the resolver dedupes
GETs per distinct key, so a composite guard sees ONE snapshot of each key (a correctness property:
two reads of the same key mid-evaluation must not straddle a concurrent write).

### Disaster recovery — guardless steps

Total `loom-state` loss (not a normal restart — see State & crash-safety below) followed by a
re-triggered `StartLoomPattern` re-runs **every guardless step whose effects don't alter a guard**.
A fresh instance (a new `instanceId`, since the lost cursor was the old one's key) replays guards
from cursor 0 against the subject's current Core KV state: a guarded step whose guard is now false
is correctly re-skipped (no double-submit), but a guardless step has no guard-replay signal — its
run/skip can never be inferred from Core KV (§10.6 invariant 2) — so replay always **lands on** and
**re-runs** it. Because each step's `requestId` derives from `(instanceId, cursor)`, the re-run's
`requestId` is gen-2's own, distinct from gen-1's already-committed one — Contract #4's
`vtx.op.<requestId>` dedup tracker cannot see across generations, so the guardless step's op commits
a second time.

This is the Contract #10 **documented-bound** doctrine (Contract #10 ~line 242): the duplicate is
**bounded and operator-visible** (one extra commit per guardless step in the recovery window, never
an unbounded loop), not a silent risk. A robust check-before-act variant is Phase-3 hardening.
Note the wipe also destroys the instance's pinned definition — the recovery instance **re-binds to
the current pattern definition** (see Definition binding above).

**Authoring guidance:** give a guard to any step whose re-run is costly. A guarded step is
**recovery-idempotent by construction** — guard replay re-skips it once its precondition is already
satisfied, so it never re-runs after the first generation that satisfied the guard. A guardless step
trades that idempotency for "always runs" — appropriate for cheap/idempotent ops (e.g. a `Sync`),
not for ops with an observable side effect that a duplicate would double (e.g. sending a
notification).

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
  ← completion event (user submits bound op → orchestration.taskCompleted, or system op commits)
       on a per-domain consumer
       → GET token.<requestId | payload.taskKey> → instance → advance cursor (atomic batch) → next step
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
**Long waits** (a user takes days) are correct by construction: a userTask arms a **bounded
creation-deadline** (`CreateTaskTimeout`) that **disarms once the task vertex exists** (Contract #10
§10.6), after which the human wait is **unbounded** — the durable cursor + live `token.<taskKey>`
pointer survive any restart, so when the user finally acts the completion correlates and the cursor
advances. A rejected/lost `CreateTask` is failed by the creation-deadline probe (never a silent wedge);
a mis-declared `completionDomains` is caught by a load-time warn.

### External steps (`externalTask`)

A third step kind, **`externalTask`**, makes an idempotent external call **wait-for-result** — symmetric
to a userTask (dispatch to an async completer, then park; the completer is a human for userTask, the
**bridge** for externalTask). It is **two-op-shaped** (unlike the single-op userTask/systemOp):

```
{ "kind": "externalTask", "adapter": "<name>", "params": { … }, "replyOp": "<ResolveOp>", "instanceOp": "<CreateInstanceOp>" }
```

- Loom submits the **`instanceOp`**, whose DDL creates the **claim vertex** (Core KV — the FR58
  *visible claim before the call*; its **type is package-chosen** — the lease demo uses
  `service.<x>.instance`, but the bridge is **type-agnostic**) and emits an `external.<adapter>` event
  via **that op's transactional outbox**; Loom then **parks** on `token.<instanceKey>`. The external
  **outcome is later recorded as aspect(s)** on this vertex by the `replyOp` (**D5** — business data in
  aspects, not fat root `data`).
- The **instance key** is the write-ahead handle Loom **mints itself** — Loom does not own the bridge's
  later result-op `requestId`, so it parks on a handle it controls, minted exactly as the deterministic
  `taskId` is for a userTask (§10.6 invariant 1).
- The **bridge** (`docs/components/bridge.md`) consumes `events.external.>`, calls the adapter
  idempotently (`idempotencyKey = instanceKey`), and posts the **`replyOp`** back carrying
  `payload.externalRef = instanceKey`. The `replyOp` DDL records the outcome aspect(s) **and emits
  `orchestration.externalTaskCompleted{externalRef}`** — the uniform orchestration-domain completion
  signal, symmetric to a userTask's `orchestration.taskCompleted{taskKey}`. Loom's **third** correlation
  key `payload.externalRef` resolves `→ token.<instanceKey> → instance` on the **`orchestration`** domain
  and the cursor advances (a later step may branch on the outcome). An externalTask pattern therefore
  declares `completionDomains: ["orchestration"]`, exactly like an all-userTask pattern.
- **Loom stays pure:** the external event rides the **instanceOp's outbox**, not a Loom-held NATS handle
  — the `internal/loom` substrate-only boundary is unchanged.
- **The deadline is handled like a userTask, not a systemOp** (§10.6): it is a **bounded creation-deadline
  on the `instanceOp` submission**, probed via the `instanceOp`'s own `vtx.op.<opRequestId>` tracker. Once
  the `instanceOp` commits the deadline **disarms** and the wait for the bridge's `replyOp` is
  **unbounded** — it **never advances the cursor** (only `orchestration.externalTaskCompleted` does). A
  rejected/lost `instanceOp` → `FailPattern` (FR29, never a silent wedge). A dead bridge surfaces on the
  **bridge's own** Health, not a per-instance Loom timeout — symmetric to the unbounded human wait.

The `externalTask` step kind, its two-op dispatch, the third `payload.externalRef` correlation key, and
the userTask-symmetric deadline are implemented in `internal/loom` (Contract #10 §10.5/§10.6).

---

## What this component owns

| Path | Role |
|------|------|
| `internal/loom/` | Engine: pattern source (durable Core-KV subscription), Sensorium (per-domain + trigger consumers), Transition Engine (cursor advance + guard eval), Actuator (the command-outbox relay: `outbox.<token>` → `core-operations`), deadline watcher (timeout backstop), pattern interpreter |
| `internal/loom/control/` | Control plane — serves "which flows are running" by reading `loom-state` (analogous to `internal/refractor/control`; the operator-facing control API is future work) |
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
| `events.<domain>.>` per-domain completion consumer | `core-events` (post-outbox) | D2: one consumer per domain in a registered pattern's `completionDomains` (default `[subjectType]`), engine-reconciled; correlates by top-level `requestId` (systemOp) or `payload.taskKey` (userTask) in the event body |
| Current Core KV state | point-reads for guard evaluation | Guards are pure over this snapshot |

## Out-contracts (produces)

| Output | Target | Notes |
|--------|--------|-------|
| Step operations | Processor via `core-operations` | Submitted via the **command outbox**: written as `outbox.<token>` in the transition batch, fire-and-forget published by the relay (no dual write, no request-reply) |
| `loom.patternStarted` / `Completed` / `Failed` | **lifecycle** ops (`StartLoomPattern`/`CompletePattern`/`FailPattern`) → outbox → `core-events` | Lifecycle on the first-class `loom` domain; no Core-KV business vertex (events ride the standard `vtx.op.<requestId>.events` outbox aspect); drives nesting + Weaver re-projection |
| Instance cursor + pinned pattern + token index + outbox + deadline | `loom-state` (own bucket) | `instance.<id>` cursor + `instance.<id>.pattern` pinned definition (written with the create, deleted at terminal) + `token.<token>` reverse pointer + `outbox.<token>` op record + `deadline.<instanceId>` (TTL); one atomic batch per transition |
| Tasks | **Core KV** (via Processor) | Business state — queryable, UI-rendered, audited, read by Weaver target Lens |

---

## State & crash-safety

| State | Where | Why |
|-------|-------|-----|
| **Tasks** (+ assignment links, completion) | **Core KV** | Business-meaningful, cross-component, audited |
| **Instance cursor + pinned pattern + token index** (pattern ref, pinned definition, step pointer, run status, reverse pointer) | **`loom-state`** | Single-component orchestration bookkeeping (P1 boundary); the instance has **no Core-KV vertex** — its sole durable home is the cursor; the pinned definition (`instance.<id>.pattern`) is what the cursor indexes into |

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
| Relay publish (or outbox-delete) fails | The outbox record persists; the relay returns **`NakWithDelay`** → JetStream redelivers no sooner than the 5s floor (`substrate.DefaultRedeliveryDelay`) → re-publish (idempotent). Bounded cadence, unbounded count: at-least-once preserved, no `MaxDeliver`, and the relay never hot-loops against a failing ops stream **or** a failing `loom-state` KV. Submission cannot be lost between batch and broker |
| Rejected / failed / unseen step | Off-stream terminal (a rejected op writes no tracker/event) — learned via the `deadline.<instanceId>` TTL expiry + a read-before-act probe (`GET vtx.op.<token>`: committed → advance+alert; not yet relayed → re-arm; else → `status=failed`). Never the submit reply; never wedges |

---

## Supervised consumers (`substrate.ConsumerSupervisor`)

All four of Loom's durables run on one per-engine `substrate.ConsumerSupervisor`: the fixed trigger
(`loom-trigger`), the dynamic per-domain completion consumers (`loom-<domain>`), the command-outbox
relay (`loom-outbox-relay`), and the deadline watcher (`loom-deadline`). The supervisor owns the pump
goroutines, a composable pause state machine (infra / structural / manual), the `NakWithDelay` backoff
floor, and `HealthSink` persist/restore. Loom continues to import only `substrate/*` — no
`jetstream`/`nats.go` import appears anywhere in `internal/loom` (non-test code).

### Desired-vs-running reconcile (per-domain set)

`reconcileConsumers` runs on every pattern load/update/remove callback **and after every instance
terminal (complete/fail)**, and resolves to a real diff of the desired domain set against the
last-applied set. The desired set is the **UNION** of (a) `bindingRegistry` aggregated across the
current pattern snapshot and (b) the domains of the **pinned patterns of live instances**
(enumerated from the `instance.*.pattern` keys in `loom-state` — pins are deleted at terminal, so
the listing is exactly the live set):

- **Add** — a domain newly referenced by any pattern spins up `loom-<domain>` live (unchanged additive
  behavior).
- **Remove (F6)** — when a domain is referenced by **no current pattern AND no live instance's pin**,
  the supervisor stops the pump **and deletes the JetStream durable**. "No leaked consumer" is the
  guarantee: an un-pumped server-side durable IS the leak. Correctness on a future re-add rests on
  `loom-state` + Contract #4 idempotency + `token.` pointer presence (a `DeliverAll` replay on re-add is
  safe; its cost is accepted) — not a preserved ack floor. If the pinned-domain enumeration fails,
  the Remove phase is skipped for that pass (a deferred teardown is harmless; a premature one is not).
- **Reset** — a domain whose desired spec config diverges from the running durable is recreated
  (delete-and-recreate), never silently left unchanged. The per-domain filter (`events.<domain>.>`) is
  name-derived and stable, so this branch is reachable in practice only if a future spec field changes;
  the diff is written generically so such a change is caught.

The three fixed durables (trigger, relay, deadline) are `Add`ed once at `Start` and are **not**
force-removed on shutdown — `Stop()` preserves their ack-floor position (substrate doctrine: a durable's
persisted position is the point of its durability). Only a live per-domain teardown diff calls `Remove`.

**In-flight instances survive pattern removal/update.** With pinning + the union, an in-flight
instance completes under its **pinned** definition even after its pattern is removed or updated
away: `advance` reads `instance.<id>.pattern`, never the (gone/changed) live definition, and the
union keeps the instance's completion-domain consumer alive until the instance reaches terminal.
The terminal batch deletes the pin, and the terminal-triggered reconcile then tears the drained
`loom-<domain>` consumer down once no current pattern and no remaining live instance references the
domain.

### Health surface (Contract #5)

- **Heartbeat** — Loom writes a Contract #5 §5.2 document to `health.loom.<instance>` (bucket
  `health-kv`) every 10s. `metrics` carries `runningInstances` (a heartbeat-cadence scan of
  `instance.<id>` entries with `status=running`, never per-message) and `consumers` (a map of consumer
  name → state: `running` | `pausedInfra` | `pausedStructural` | `pausedManual`). The consumer states
  come from a Loom-side cache fed by the per-consumer `HealthSink` writes — the supervisor persists
  through the sink but exposes no read-back, so Loom caches each transition. `issues` is empty unless a
  consumer sits in `pausedStructural` (one `warning` / `ConsumerPaused` entry).
- **Per-consumer pause-state** — each managed consumer also implements `substrate.HealthSink`, persisting
  a small `{status, pauseReason, lastError}` document to `health.loom.<instance>.consumer.<name>` (a
  SEPARATE key from the heartbeat). Pause-state persists and restores across an engine restart via the
  supervisor's `Add`-time restore semantics (manual > structural > infra precedence): a consumer paused
  before a restart comes back paused without an explicit `Resume`. Loom exposes no operator
  `Pause`/`Resume` control surface yet — the supervisor API is callable but not externally surfaced.
  When a per-domain consumer is torn down (Remove, above),
  both its `consumerStateCache` entry and its `health.loom.<instance>.consumer.loom-<domain>` pause entry
  are deleted, so a future re-add of the same domain starts clean (active, not resurrected into a stale
  pause) and the heartbeat does not keep reporting a phantom consumer.

### `Instance` uniqueness (Contract #5 precondition)

`Config.Instance` is the key segment for this process's heartbeat (`health.loom.<instance>`) and every
per-consumer pause entry (`health.loom.<instance>.consumer.<name>`) in the shared `health-kv` bucket. **It
MUST be unique per Loom process sharing that bucket.** When empty it defaults to
`<hostname>-<pid>-<NanoID>` (sanitized for KV key segments) — the hostname+pid prefix makes an
auto-generated heartbeat attributable to the process that wrote it, and the NanoID suffix keeps each
`Engine` construction unique (the pattern-source durable name is also derived from `Instance` and depends
on per-boot uniqueness, even across multiple `Engine`s in one process). The default is therefore unique per
construction, not just per host/pid — operators running multiple Loom replicas who want a *stable*,
human-recognizable `Instance` across restarts (for dashboards/alerting) should set it explicitly to
something cluster-unique.

If two processes ever do run with the same `Instance` against the same `health-kv` bucket:

- their `health.loom.<instance>` heartbeats last-write-wins each other — an operator sees one flapping
  liveness/uptime document for two processes, not two;
- their per-consumer pause entries (`health.loom.<instance>.consumer.<name>`) are the same key — one
  process's manual pause can be silently restored onto the other process's consumer of the same name at
  its next restart (cross-process pause restore).

## Principles that apply

- **P2** — Processor is the sole Core KV writer / event producer; Loom is a client (tasks and the
  `loom.*` lifecycle events go through the ledger / outbox — never a direct Core-KV write or publish).
- **P1** — tasks are vertices (business state); the instance cursor is operational state (`loom-state`),
  with **no** Core-KV instance vertex.
- **Decision #10** — engine is minimal/generic; flows are packages.
- **Module boundary** — `loom` imports only `substrate/*`; talks to Weaver/Processor via NATS,
  never Go calls.

## Implementation status

**Built (Phase 2).** The engine is fully implemented and CI-gated: the linear-sequence interpreter,
the rebuildable durable cursor with definition pinning, the declarative guard grammar, the command
outbox + deadline backstop, the supervised consumers, and all three step kinds — **userTask**,
**systemOp**, and **externalTask**. The `externalTask` step dispatches an idempotent external call via
the **bridge** (`docs/components/bridge.md`) and awaits its result; this is the sole external-I/O
placement (Contract #10 §10.3/§10.8).

**Deferred (Phase 3+).**

- Starlark guard evaluation — the reserved `{reads, starlark}` escape hatch is recognized and rejected
  at parse time. The shared verified-pure sandbox lands only when the first Starlark guard is authored
  (§10.5); the declarative grammar above covers the field-presence / equality predicates the current
  flows need. It must remain side-effect-free.
- An externally-surfaced Loom control API (operator pause/resume, a durable `loom.*` read model) — the
  control plane reads `loom-state` today; a public control surface is future work.
