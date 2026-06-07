# Story 8.1: Loom walking skeleton

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform developer,
I want the Loom engine machinery — pattern source, trigger consumer, per-domain completion consumers,
the durable `loom-state` token index, the Actuator, and the event-only lifecycle ops — proven
end-to-end against one simple multi-step systemOp pattern,
so that the core interpreter loop is standing before user-tasks and guards.

This story stands up the **engine machinery**, not a rich pattern library. The fixture is deliberately
simple (one ≥2-step systemOp pattern, no guards) because the machinery cannot be exercised without a
pattern to drive — but the deliverable is the machinery: the durable subscriptions, the durable token
index on `loom-state`, the event-plane trigger/lifecycle, and the Actuator.

## Acceptance Criteria

> **Amendment (CAR Request 5, ratified 2026-06-06) — command outbox.** AC#3/#4/#5 below are superseded on
> the op-submission path: a step op is **not** submitted by a synchronous Actuator call. Instead Loom
> writes an `outbox.<token>` record + arms a `deadline.<instanceId>` TTL **in the same `loom-state`
> AtomicBatch** as the cursor/token transition; a durable **relay** (`loom-outbox-relay`) fire-and-forget
> publishes it to `core-operations` and deletes the record on publish-ack. **AC#4's failed/rejected
> terminal** is no longer the `ops.<lane>` submit reply — it is the `deadline.<instanceId>` TTL expiry +
> a read-before-act probe (`GET vtx.op.<token>`: committed → advance+alert; not-yet-relayed → re-arm;
> else → fail). `loom-state` therefore holds **four** key shapes (`instance.`/`token.`/`outbox.`/
> `deadline.`). This makes `internal/loom` carry **no raw nats handle** (AC#8 fully met). See
> §10.3/§10.6 and the Senior Review section below.

1. **Pattern source (durable Core-KV subscription — the spine, not a stub).** Given a `meta.loomPattern` of **systemOp steps with no guards** installed as package/fixture data, when Loom starts it loads patterns from Core KV the same way Refractor loads `meta.lens` defs — mirror **`internal/refractor/lens/corekv_source.go` (`CoreKVSource`)** and its wiring at `cmd/refractor/main.go:425` (`NewCoreKVSource` → `SetLoadCallback`/`SetUpdateCallback` → `Start`). Concretely: a **durable JetStream subscription on the Core KV backing stream via `substrate.SubscribeKVChanges(ctx, coreKVBucket, "vtx.meta.", "<loom-pattern-source durable>", {IncludeHistory: true})`**, routed by envelope class `meta.loomPattern` inside the handler — **not** an ephemeral `kv.WatchAll()` and **not** a one-shot point-read. Durable means: replays existing patterns on first connect (`IncludeHistory`); a pattern installed *after* startup registers via the same callback **without an engine restart** (§10.5 "loaded via CDC like a Lens def"; the walking skeleton is thin on **step kinds** — systemOp-only — **not** on the loader spine). The engine ships **zero domain knowledge**; the pattern (`{patternId, subjectType, completionDomains?, steps:[{kind:"systemOp", operation}]}`) is package data, per Contract #10 §10.5 and `docs/components/loom.md`.

2. **Per-domain completion consumers reconciled from `completionDomains` (D2).** The AC#1 pattern source fires load/update callbacks (mirroring `CoreKVSource.SetLoadCallback` → `startPipeline` in `cmd/refractor/main.go:426`); from those Loom rebuilds the **binding registry** and reconciles a **durable per-domain consumer** on `core-events` (`events.<domain>.>`) for each domain in a registered pattern's **`completionDomains`** (Contract #10 §10.5; **default `[subjectType]`** when the field is omitted; a domain = the first dot-free segment of an event class). **One consumer per referenced domain**, **not** per-(pattern×event). A pattern installed live that references a not-yet-seen domain reconciles a **new** consumer without restart. The registry's **only** job is choosing *which domains to subscribe to* — completion is correlated by the durable `token.<token>` GET (AC#4), never by a registry-named event-type. These per-domain consumers use the substrate `DurableConsumer` primitive (Story 7.6, `Conn.RunDurableConsumer`); the pattern source uses `Conn.SubscribeKVChanges` (AC#1) — **both substrate helpers; no new `nats.io`/`jetstream` handle appears in `internal/loom`**.

3. **Event-plane trigger + execution loop runs to completion.** The trigger is an **event**, not a direct Go call. A caller submits `StartLoomPattern{patternRef, subjectKey}` (an **event-only** op — see AC#7); its commit emits `events.loom.patternStarted`. Loom runs a **fixed durable consumer on `events.loom.patternStarted`** (always-on, **independent of `completionDomains`**); on the event it validates `patternRef` against the loaded registry, creates the `loom-state instance.<instanceId>` cursor (with `instanceId = StartLoomPattern` `requestId`), and submits step 0. The engine then runs `completion event → advance cursor → submit next op → completion event` to completion: for the cursor step it write-aheads the step's `pendingToken` (the chosen `requestId`) **and** the `token.<token>` pointer to `loom-state` in **one `substrate.AtomicBatch`** before the Actuator submits the systemOp through the **Processor** (via `core-operations`, never a direct Core KV write — P2); a committed terminal advances the cursor (another atomic batch); pattern exhausted → Loom submits **`CompletePattern{instanceId}`** (event-only) → outbox → `events.loom.patternCompleted`.

4. **systemOp terminal correlation — durable token GET, two transports (§10.6).** Loom correlates a systemOp step by the `requestId` it chose (the `pendingToken`), read from the **message body** (`Event.requestId`, `internal/processor/step7_events.go`) and resolved by a **direct `token.<requestId>` GET** on `loom-state` (domain-independent, multi-instance-safe) — never from a subject segment, event-type, or in-memory map. The two terminals do **not** both ride `core-events`:
   - **committed → advance cursor.** The op's commit publishes its business event(s); each Event body carries `requestId`. A consumed event whose `requestId` matches a **live `token.` pointer** is the committed terminal → advance via the atomic batch (which deletes the consumed pointer). **Idempotency by pointer presence:** pointer gone → drop/ack, no re-advance. Correlate on `requestId`, **not** event-type (a systemOp may emit several classes; Loom ships zero knowledge of which).
   - **failed/rejected → `status=failed` / `retryCount` per policy.** A rejected op writes **no tracker and emits no event** (Processor denies before commit step 8), so this terminal is **absent from `core-events`**. Loom obtains it from the **op-submission reply** on the `ops.<lane>` reply-inbox (`cmd/lattice/output/submit.go`) — or a bounded per-step timeout (which also backstops a mis-declared `completionDomains`). This off-stream path is what guarantees a rejected systemOp does **not** wait forever.

5. **`loom-state` durable index (two disjoint-prefixed keys + AtomicBatch + AllowAtomicPublish).** The instance cursor lives in the new operational bucket `loom-state` under **`instance.<instanceId>`** (`{instanceId, patternRef, subjectKey, cursor, pendingToken, status, retryCount}`); the `pendingToken → instance` reverse index is the co-located **`token.<pendingToken>`** thin pointer (`{instanceId}`) — per Contract #10 §10.3, **never** Core KV and **never** a separate index bucket. Each step transition is a **single `substrate.AtomicBatch` on `loom-state`** (`internal/substrate/batch.go`): update `instance.<id>`, write the new `token.<newToken>`, delete the prior `token.<oldToken>`. `loom-state` **joins the primordial bucket create list** (add it to `internal/bootstrap/primordial.go` `ProvisionBuckets`, dash-named, TTL-capable) **and must be provisioned with `AllowAtomicPublish: true`** — extend the `enableAtomicPublish` gate currently scoped to `CoreKVBucket` only (and the `scripts/verify-kernel.go` assertion), or `Conn.AtomicBatch` on `loom-state` is rejected.

6. **Restart resumes; exactly-once completion (no in-memory index).** On engine restart the durable per-domain consumers **resume from their last ack**; correlation is the durable **`token.<token>` GET** — there is **no in-memory `pendingToken → instance` index to rebuild** and **no watch-suspend-until-rebuild gate** (Crash-safety invariant 3 is removed, §10.6). A redelivered completion mid-restart resolves against the durable pointer regardless of engine age. The run completes **exactly once**: op submission is idempotent because Loom chose the `requestId` write-ahead (in the atomic batch), so a re-attempt after a crash collapses on the Contract #4 `vtx.op.<requestId>` tracker, and pointer-presence is the at-least-once redelivery guard.

7. **Event-only lifecycle ops in `orchestration-base` + the `loom` domain.** Three **event-only** ops are added (no business mutation — they rely on `BuildEventList` constructing events from emitted `EventSpec`s independent of mutations; they write only the universal `vtx.op.<requestId>` tracker): `StartLoomPattern{patternRef, subjectKey}` (posted by the caller / fixture → `events.loom.patternStarted`), `CompletePattern{instanceId}` and `FailPattern{instanceId, reason?}` (posted by Loom under `identity:loom` → `events.loom.patternCompleted`/`Failed`). The instance is **operational-only** — it lives solely in `loom-state` (`instance.<instanceId>`); there is **NO Core-KV `vtx.loomInstance` (or any instance) vertex**. `loom` is a first-class domain (Loom consumes `patternStarted`, emits `patternCompleted`/`Failed`). **Implementation check:** confirm the Processor pipeline accepts a **zero-mutation op** (tracker-only atomic batch at step 8 + emitted events at step 7/9) — verify no upstream guard rejects an empty `MutationBatch` when `result.Events` is non-empty.

8. **Module boundary.** `internal/loom` imports **only `substrate/*`** — **no Go import of `internal/processor`, `internal/weaver`, or `internal/refractor`**. All cross-component interaction is via NATS (submit ops to `core-operations`; consume `core-events`; read/write `loom-state`).

9. **Verification — fixture run.** A test installs ONE simple fixture `meta.loomPattern` of ≥2 systemOp steps (no guards) over a subject vertex, drives it via a real `StartLoomPattern` submission (which emits the trigger event), and asserts: `events.loom.patternStarted` drove instance creation, every step's op committed in order, the cursor advanced to exhaustion, `events.loom.patternCompleted` was emitted, and a mid-run engine restart resumes to the **same** completion exactly once (no double submission) — correlated against the durable `token.<token>` pointer (no in-memory index).

## Tasks / Subtasks

- [x] **Task 1 — `loom-state` bucket joins primordial bootstrap, `AllowAtomicPublish: true`** (AC: #5)
  - [x] Add `LoomStateBucket = "loom-state"` constant + entry to the `buckets` slice in `internal/bootstrap/primordial.go` `ProvisionBuckets` (dash-named, TTL-capable like `weaver-state`/`weaver-claims`). [Source: docs/contracts/10-orchestration-surfaces.md#10.3; internal/bootstrap/primordial.go]
  - [x] **Extend `enableAtomicPublish` to `loom-state`** — today it is gated to `CoreKVBucket` only (`internal/bootstrap/primordial.go`); set `AllowAtomicPublish: true` on `loom-state`'s underlying stream too, or `Conn.AtomicBatch` on it is rejected. Extend the `scripts/verify-kernel.go` assertion accordingly. [Source: docs/contracts/10-orchestration-surfaces.md#10.3]
  - [x] Idempotent re-provision (CreateOrUpdateKeyValue already idempotent); add/extend a bootstrap test asserting the bucket exists, is TTL-capable, and has `AllowAtomicPublish`.

- [x] **Task 2 — `internal/loom` package skeleton + `loom-state` cursor + token store** (AC: #5, #8)
  - [x] Create `internal/loom/` (package `loom`); imports **only `internal/substrate`** + stdlib. Add a `doc.go` describing the engine.
  - [x] Cursor store over `loom-state`: `instance.<instanceId>` value `{instanceId, patternRef, subjectKey, cursor, pendingToken, status, retryCount}` (status ∈ `{running, complete, failed}`) + the co-located `token.<pendingToken>` thin pointer (`{instanceId}`). Read via `substrate.Conn` KV helpers; **write every transition via `substrate.AtomicBatch`** (`internal/substrate/batch.go`): update `instance.<id>`, write the new `token.<newToken>`, delete the prior `token.<oldToken>` — all-or-nothing. [Source: docs/contracts/10-orchestration-surfaces.md#10.3; internal/substrate/batch.go]
  - [x] No Go import of Processor/Weaver/Refractor (AC #8) — enforce with a package-boundary test (`go list -deps`).

- [x] **Task 3 — pattern source (CDC, like a Lens def) + `completionDomains`** (AC: #1, #2)
  - [x] Define the in-engine `loomPattern` / `step` types matching the §10.5 JSON shape, including optional **`completionDomains: ["<domain>", …]`** (default `[subjectType]` when omitted); Phase-8.1 scope = `{kind:"systemOp", operation}` steps, **no guard handling** (guards are Story 8.3 — the fixture carries none). A `Domains()` helper returns the dedup'd set of first-dot-free-segment domains from `completionDomains`. [Source: docs/contracts/10-orchestration-surfaces.md#10.5]
  - [x] Load `meta.loomPattern` meta-vertices from Core KV by mirroring **`internal/refractor/lens/corekv_source.go` (`CoreKVSource`)** + its wiring at `cmd/refractor/main.go:425-440`: a **durable** `substrate.SubscribeKVChanges(ctx, coreKVBucket, "vtx.meta.", "<loom-pattern-source>", {IncludeHistory:true})`, routed by class `meta.loomPattern`, dispatching load/update callbacks. **Not** `kv.WatchAll()` (ephemeral) and **not** a one-shot point-read — a pattern installed after startup must register live (AC #1). Note: the manager (`consumer/manager.go`) is the *downstream* per-consumer factory, **not** the loader. [Source: internal/refractor/lens/corekv_source.go; cmd/refractor/main.go:425; internal/substrate/subscribe.go]
  - [x] Build the **binding registry** in the load/update callbacks (rebuilt on each pattern add/remove): the dedup'd set of `completionDomains` across registered patterns, so Task 4 reconciles one consumer per referenced domain and a newly-seen domain spins up a consumer without restart.

- [x] **Task 4 — Sensorium: trigger consumer + per-domain completion consumers (D2)** (AC: #2, #3, #4, #6)
  - [x] **Fixed trigger consumer:** `substrate.Conn.RunDurableConsumer` on `Stream: "core-events"`, `FilterSubject: "events.loom.patternStarted"`, stable `Durable: "loom-trigger"` (always-on, **independent of `completionDomains`**). Handler: validate `patternRef` against the loaded registry, create the `instance.<instanceId>` cursor (`instanceId = StartLoomPattern` `requestId`, read from body), submit step 0. Idempotent on `instanceId` (cursor already present → skip). [Source: docs/contracts/10-orchestration-surfaces.md#10.9]
  - [x] **Per-domain completion consumers:** for each domain in the binding registry, `RunDurableConsumer` with `FilterSubject: "events.<domain>.>"`, stable `Durable: "loom-<domain>"`, idempotent `HandlerFunc`. **Reuse the existing primitive — no raw jetstream** (AC #8). [Source: internal/substrate/consumer.go; internal/processor/outbox/consumer.go]
  - [x] Completion handler reads `Event.requestId` **from the message body** and correlates via a **direct `token.<requestId>` GET** on `loom-state` — match on `requestId`, not event-type (AC #4). **committed terminal** = live pointer found → advance cursor via atomic batch (which deletes the pointer); **idempotency by pointer presence** (pointer gone → drop/ack). The **failed/rejected terminal is NOT on `core-events`** — get it from the **op-submission reply** on the `ops.<lane>` reply-inbox (or a bounded timeout) → `status=failed`/`retryCount`, so a rejected op never wedges. [Source: docs/contracts/10-orchestration-surfaces.md#10.6; internal/processor/step7_events.go; cmd/lattice/output/submit.go]

- [x] **Task 5 — Transition Engine + Actuator: advance cursor, submit next op** (AC: #3, #5)
  - [x] On the trigger event (subject must be a vertex of the pattern's `subjectType`): create the `instance.<instanceId>` cursor (atomic batch).
  - [x] For the cursor step: in **one `substrate.AtomicBatch`**, write-ahead the `pendingToken` (the chosen `requestId`) on `instance.<id>` **and** the `token.<token>` pointer **before** submitting (Crash-safety invariant 1, §10.6) — guardless steps complete **solely** via their token (invariant 2). Then the Actuator submits the systemOp envelope (`Actor: identity:loom`, deterministic `requestId`) to `core-operations` over NATS. [Source: cmd/lattice/output/submit.go; internal/processor/envelope.go]
  - [x] On the step's terminal-committed event: advance `cursor` (atomic batch — update `instance`, delete the consumed `token.`, write the next step's `token.`); pattern exhausted → submit **`CompletePattern{instanceId}`** (event-only) whose commit emits `events.loom.patternCompleted` through the outbox (**never** a direct publish). On the off-stream rejected/timeout terminal → submit `FailPattern{instanceId, reason?}`. [Source: docs/components/loom.md#Out-contracts; docs/contracts/10-orchestration-surfaces.md#10.9]

- [x] **Task 6 — event-only lifecycle ops in `orchestration-base` + the `loom` domain** (AC: #3, #7)
  - [x] Add the three **event-only** ops to `orchestration-base`: `StartLoomPattern{patternRef, subjectKey}` → emits `loom.patternStarted`; `CompletePattern{instanceId}` → `loom.patternCompleted`; `FailPattern{instanceId, reason?}` → `loom.patternFailed`. Each has **no business mutation** (event body: `instanceId, patternRef, subjectKey, requestId`) — they write only the universal `vtx.op.<requestId>` tracker. **NO Core-KV `vtx.loomInstance` (or any instance) vertex** — the instance lives solely in `loom-state`. `StartLoomPattern` carries `authContext.target = vtx.meta.loomPattern.<patternId>` (§10.8). [Source: docs/contracts/10-orchestration-surfaces.md#10.9, #10.8]
  - [x] **Implementation check:** confirm the Processor pipeline accepts a **zero-mutation op** — `BuildEventList` (`internal/processor/step7_events.go`) constructs events independent of mutations; verify no upstream guard rejects an empty `MutationBatch` when `result.Events` is non-empty. If a guard rejects it, that is the Processor change this story unblocks (flag to Winston before working around it). [Source: docs/contracts/10-orchestration-surfaces.md#10.9 implementation check]

- [x] **Task 7 — restart / re-drive path (no in-memory index)** (AC: #6)
  - [x] On startup, the durable per-domain + trigger consumers resume from their last ack; correlation is the durable `token.<token>` GET — **no in-memory index to rebuild, no watch-suspend gate** (invariant 3 removed). A redelivered completion resolves against the durable pointer. [Source: docs/contracts/10-orchestration-surfaces.md#10.3, #10.6]
  - [x] Re-attempt of a still-pending step is safe (idempotent op submission via the deterministic `requestId` + Contract #4 tracker) — do not re-run a step whose token is still pending (invariant 2); pointer-presence is the at-least-once redelivery guard.

- [x] **Task 8 — `cmd/loom/` binary entry point** (AC: #8)
  - [x] Minimal `cmd/loom/main.go` wiring a `substrate.Conn`, starting the engine (pattern source + trigger consumer + per-domain Sensorium + Transition Engine + Actuator). Shares only `substrate/*` (extractable later, per `docs/components/loom.md`).

- [x] **Task 9 — fixture e2e test** (AC: #9)
  - [x] Install ONE simple fixture `meta.loomPattern` of ≥2 systemOp steps (no guards) + a subject vertex; drive via a real `StartLoomPattern` submission (emits the trigger event); assert `events.loom.patternStarted` drove instance creation, ordered op commits, cursor exhaustion, `events.loom.patternCompleted` emitted, and a **mid-run restart** resumes to the same completion **exactly once** (no double submission) — correlated against the durable `token.<token>` pointer. [Source: docs/contracts/10-orchestration-surfaces.md#10.6, #10.9 crash-safety]

## Dev Notes

### Architecture patterns & constraints

- **Loom = generic linear-sequence interpreter** for deterministic procedures; **zero domain knowledge** — patterns are package data, the engine interprets them. This story stands up the **engine machinery**: systemOp steps only, **no guards, no user-tasks** (those are 8.2/8.3); the fixture is one simple ≥2-step pattern that exercises the machinery. [Source: lattice-architecture.md#D3; docs/components/loom.md]
- **P2 — Processor is the sole Core KV writer / event producer.** Loom is a *client*: it submits ops to `core-operations`; its only direct writes are to its own `loom-state` bucket. The `loom.patternStarted/Completed/Failed` lifecycle events are produced by **event-only ops** through the Processor → outbox → `core-events`; Loom **never** publishes to `core-events` directly (the transactional outbox, Story 1.5.10, remains the sole event producer). [Source: docs/components/loom.md#Principles; docs/contracts/10-orchestration-surfaces.md#10.9]
- **Trigger is on the EVENT plane, not a direct Go call.** A caller submits `StartLoomPattern` (event-only op) → outbox → `events.loom.patternStarted`; Loom runs a **fixed** durable consumer on that subject (always-on, independent of `completionDomains`). Do **not** wire a direct-Go `StartInstance` trigger. [Source: docs/contracts/10-orchestration-surfaces.md#10.9]
- **D2 — one durable consumer per *domain*** (`events.<domain>.>`), engine-reconciled from each pattern's **`completionDomains`** (default `[subjectType]`), fanning out to registered patterns internally. **Packages declare `completionDomains`; they do NOT provision NATS infra** — the engine reconciles consumers. [Source: lattice-architecture.md#D2; docs/contracts/10-orchestration-surfaces.md#10.5; docs/components/loom.md#In-contracts]
- **Crash-safety invariants are BINDING (not latitude), §10.6:** (1) write-ahead = the **atomic batch** (the `token.` pointer + `instance.<id>` update) *before* the side effect; (2) a guardless step completes **solely** via its token (re-drive must not re-run a step whose token is still pending). Invariant 3 (watch-suspended-until-rebuild) is **REMOVED** — there is no in-memory index. [Source: docs/contracts/10-orchestration-surfaces.md#10.6]
- **Correlation is the durable `token.<token>` GET** — domain-independent, multi-instance-safe, resolved by a direct GET on the co-located reverse pointer. There is **NO in-memory `pendingToken → instance` index** and **NO secondary index bucket** (forbidden — dual-write/drift); the sanctioned design is the co-located `token.` prefix written in the **same `substrate.AtomicBatch`** as the `instance.` update. **Idempotency by pointer presence:** a redelivered completion whose pointer is gone (step already advanced) is dropped, not re-advanced. [Source: docs/contracts/10-orchestration-surfaces.md#10.3, #10.6]
- **systemOp correlation has two terminals.** **committed** = a `core-events` body `requestId` matching a live pointer → advance. **failed/rejected** is **off-stream** (a rejected op writes no tracker/event) — from the `ops.<lane>` submit reply or a bounded per-step timeout → `status=failed`/`retryCount`. A rejected op must not wait forever. [Source: docs/contracts/10-orchestration-surfaces.md#10.6]
- **Idempotency / exactly-once:** systemOp submission is write-ahead — Loom chooses the `requestId`, so a redelivered restart collapses on the Contract #4 `vtx.op.<requestId>` tracker; the `StartLoomPattern` trigger dedups on the `instanceId` (= its `requestId`) via cursor presence. [Source: docs/contracts/10-orchestration-surfaces.md#10.6, #10.9]
- **Instance is operational-only.** It lives solely in `loom-state` (`instance.<instanceId>`); there is **NO Core-KV instance vertex**. "Which flows are running" is a future **control-plane** concern (like `internal/refractor/control`, reading `loom-state`), not in this story. [Source: docs/contracts/10-orchestration-surfaces.md#10.9]
- **Guards are out of scope** for 8.1 (Story 8.3). The fixture pattern carries no guards; re-drive on restart is purely the durable consumer + the durable `token.` pointer (not guard replay).

### Three durable mechanisms, `2 + N` durables — keep them visibly separate

Loom wires **three distinct substrate consumer mechanisms**; the dev-story must not conflate them (what patterns exist / what triggered a flow / what completions happened):

| Mechanism | Substrate helper | Stream | Count | Purpose |
|-----------|------------------|--------|-------|---------|
| **Pattern source** | `Conn.SubscribeKVChanges` | Core KV backing stream (`vtx.meta.>`, class `meta.loomPattern`) | **exactly 1** (durable `loom-pattern-source`) | loads/updates pattern defs → fires the load/update callbacks (AC #1) |
| **Trigger consumer** | `Conn.RunDurableConsumer` (Story 7.6) | `core-events` (`events.loom.patternStarted`) | **exactly 1** (durable `loom-trigger`) | always-on, **independent of `completionDomains`**; creates the cursor + submits step 0 (AC #3) |
| **Per-domain completion consumer** | `Conn.RunDurableConsumer` (Story 7.6) | `core-events` (`events.<domain>.>`) | **N — one per `completionDomains` domain** (durable `loom-<domain>`) | receives commit events, correlates by the durable `token.<requestId>` GET → advances cursors (AC #2/#4) |

So the **total durable count is `2 + N`**, where `N` = the number of distinct domains across all registered patterns' `completionDomains` (deduped — *not* per-pattern, *not* per-(pattern×event), D2). `N` is **dynamic**: the pattern-source callbacks rebuild the binding registry on each pattern add/remove, so a newly-referenced domain adds an `N+1`-th durable live, and a no-longer-referenced domain's consumer can be torn down — without an engine restart. The pattern source + trigger consumer are the *drivers*; the per-domain consumers are the *driven* set. All are durable; none introduces a raw `nats.io`/`jetstream` handle in `internal/loom` (AC #8).

### Substrate primitive (the one shared completion consumer) — reuse, don't re-wire

- Story 7.6 generalized the outbox's durable-consumer pattern into `internal/substrate`: `Conn.RunDurableConsumer(ctx, DurableConsumerConfig{Stream, FilterSubject, Durable, MaxDeliver?, Logger?}, HandlerFunc) error`. The `HandlerFunc` returns `substrate.Ack` / `Nak` / `Term`; re-running with the same `Durable` resumes from the last ack. **AC #8 requires Loom consume this primitive — no new `nats.io`/`jetstream` handles in `internal/loom`.** [Source: internal/substrate/consumer.go]
- The outbox is the **reference consumer** to mirror for engine wiring (config + handler shape, read-from-body, idempotent handler). [Source: internal/processor/outbox/consumer.go]

### Op submission (Actuator) shape

- `processor.OperationEnvelope{ RequestID, Lane, OperationType, Actor: "<identity:loom key>", SubmittedAt, Payload, Class?, AuthContext? }`. `StartLoomPattern` carries `authContext.target = vtx.meta.loomPattern.<patternId>` (§10.8) — relevant when Weaver triggers Loom (9.1); for the 8.1 fixture the trigger may be a direct `StartLoomPattern` submission. The systemOp steps are submitted under `identity:loom` authority (root-equivalent via `holdsRole` → operator, provisioned in `internal/bootstrap`). [Source: internal/processor/envelope.go; cmd/lattice/output/submit.go; docs/contracts/10-orchestration-surfaces.md#10.5, #10.8]
- `cmd/lattice/output/submit.go` shows the publish-to-`ops.<lane>` + reply-inbox pattern; Loom's Actuator submits similarly. The **committed** terminal is correlated asynchronously off `core-events` (by `requestId`), but the Actuator **must still capture the submit reply** — that reply (rejected/duplicate/committed) is the **only** source of the failed/rejected terminal, which never reaches `core-events` (AC #4). Treat `duplicate` as committed (idempotent re-attempt collapsed on the Contract #4 tracker).

### Source tree components to touch

- **NEW:** `internal/loom/` (engine: pattern loader, Sensorium, Transition Engine, Actuator, `loom-state` cursor store), `cmd/loom/main.go`.
- **EDIT:** `internal/bootstrap/primordial.go` — add `loom-state` to `ProvisionBuckets`.
- **READ-ONLY references (do not import into `internal/loom`):** `internal/substrate/consumer.go` (`RunDurableConsumer`, per-domain core-events), `internal/substrate/subscribe.go` (`SubscribeKVChanges`, durable Core-KV definition source), `internal/refractor/lens/corekv_source.go` + `cmd/refractor/main.go:425-440` (the **pattern-loader mirror** — durable def source + load/update callbacks), `internal/processor/outbox/consumer.go`, `cmd/lattice/output/submit.go`, `internal/processor/step7_events.go` (Event body / `requestId`), `internal/processor/envelope.go`.
- The `identity:loom` service actor (+ `holdsRole`→operator + capability doc) is **already provisioned** by Story 7.3 (`internal/bootstrap/nanoid.go` / `primordial.go`) — do not re-provision; consume it as the Actuator's actor.

### Testing standards

- Go test packages: `go test ./internal/loom/... ./internal/bootstrap/...` plus the fixture e2e. Honor the repo verification gates: `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`, `make test-bypass` (Gate 2 BLOCKED), `make test-capability-adversarial` (Gate 3 DEFENDED) where touched. [Source: CLAUDE.md#Workflow]
- The e2e must exercise the **mid-run restart** (AC #9) — the load-bearing proof of "resumes; exactly once," correlated against the durable `token.<token>` pointer (no in-memory index). Mirror existing e2e harness setup (embedded NATS / JetStream) used by `internal/refractor/*_e2e_test.go` and `internal/bootstrap/service_actor_e2e_test.go`.

### Comment policy (binding — CLAUDE.md)

- **No history/changelog narration in code.** No `// Story 8.1 …`, `// was …`, `// renamed from …`. Comments describe what the code does *now*. git blame + the commit message are the record.
- **Key-shape conventions (Contract #1):** aspects 4-segment `vtx.<type>.<id>.<localName>`; links 6-segment `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>`; meta-vertices `vtx.meta.<NanoID>` + `.canonicalName`. Bucket names are **dash-named, no dots** (`loom-state`).

### Project Structure Notes

- `cmd/` already holds `bootstrap`, `lattice`, `lattice-pkg`, `processor`, `refractor`; `cmd/loom/` joins them. `internal/` holds `bootstrap`, `processor`, `refractor`, `substrate`, `pkgmgr`, `weaver` (skeleton TBD); `internal/loom/` is new and currently absent (confirmed — this is the walking skeleton).
- No conflicts: `loom-state`, `cmd/loom/`, `internal/loom/` are all greenfield. The only edit to existing code is the additive primordial-bucket entry.

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story-8.1] — story statement, AC, deps (7.1, 7.6, 1.5.10).
- [Source: docs/components/loom.md] — overview, core model, execution loop, ownership table, in/out contracts, state & crash-safety, failure modes, principles, deferred items.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.3] — `loom-state` bucket; `instance.<id>` + `token.<token>` disjoint keys; durable co-located reverse index; per-transition `substrate.AtomicBatch`; `AllowAtomicPublish: true` provisioning.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.5] — `meta.loomPattern` shape; `StartLoomPattern{patternRef, subjectKey}`; step `{kind, operation, guard?}`; linear-only.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.5] — `completionDomains` (default `[subjectType]`); pattern shape.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.6] — step completion & correlation via the durable `token.<token>` GET (both terminals; off-stream failed/rejected); **crash-safety invariants** (write-ahead atomic batch / guardless-token-only; invariant 3 removed).
- [Source: docs/contracts/10-orchestration-surfaces.md#10.9] — event-only trigger/lifecycle ops (`StartLoomPattern`/`CompletePattern`/`FailPattern`) on the `loom` domain; instance operational-only (no Core-KV vertex); Processor zero-mutation-op implementation check.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.4] — outbox stays the sole event producer; never publish to `core-events` directly.
- [Source: _bmad-output/planning-artifacts/lattice-architecture.md#D2, #D3, #D5] — per-domain consumer, runtime mechanics, task/service DDL placement.
- [Source: internal/substrate/consumer.go] — `RunDurableConsumer` / `DurableConsumerConfig` / `HandlerFunc` / `Decision`.
- [Source: internal/processor/outbox/consumer.go] — reference durable-consumer wiring (config + idempotent handler + read-from-body).
- [Source: internal/bootstrap/primordial.go] — `ProvisionBuckets` (where `loom-state` joins); existing `weaver-state`/`weaver-claims` precedent.
- [Source: internal/bootstrap/nanoid.go, primordial.go] — `identity:loom` service actor already provisioned (Story 7.3).
- [Source: internal/processor/envelope.go; cmd/lattice/output/submit.go] — op envelope + submit-to-`ops.<lane>` path for the Actuator.

## Senior Review (Winston) — 2026-06-06

3-layer adversarial review run (Blind Hunter diff-only, Edge Case Hunter diff+repo, Acceptance
Auditor diff+spec+contracts). Build + `go test ./internal/loom/... ./packages/orchestration-base/...`
+ `TestCommit_ZeroMutationEventOnly` all PASS. 8 of 9 ACs satisfied. Findings below are **fix-forward
next session** (committed as a checkpoint; story stays in `review`). No fixes applied this pass.

**CAR status:** fully ratified into Contract #10 (§10.3/§10.5/§10.6/§10.9 + 2026-06-06 revision entry);
all 6 working-tree reconciliation deltas applied. Nothing in the CAR itself remains unresolved.

> **Update 2026-06-06 (course-correction):** F1, F2, F5 (and the C2 blocking-callback) are now resolved
> **by design** via **CAR Request 5 — Loom command outbox** (`cmd/loom/CONTRACT-AMENDMENT-REQUEST.md`),
> PENDING Andrew's ratification. The outbox makes submission a durable atomic fact + async relay (kills
> F2/C2), routes the lifecycle ops through it (F5), and lets `internal/loom` drop the raw nats handle
> (F1). The failed/rejected terminal becomes a crash-safe per-step deadline + read-before-act tracker
> probe (synchronous reply removed). F4 and F6 remain independent fix-forward items. Do the R5 code
> reconciliation (CAR §"Request 5 reconciliation") after ratification.

### Second adversarial review — Request 5 reconciliation (2026-06-06)

3-layer pass (Blind Hunter diff-only, Edge Case Hunter diff+repo, Acceptance Auditor diff+spec) over the
command-outbox/deadline code. **Acceptance Auditor: all CAR-5 claims SATISFIED, gates green, F1/F2/F5
genuinely retired.** Build/vet/lint/`-race`/verify-kernel/loom-suite all pass.

**Resolved by Request 5 (verified in code):** **F1** (no raw nats — `internal/loom` imports only
substrate; boundary test enforces), **F2** (no dual-write — op submission is the in-batch `outbox.`
record), **F3** (per-step backstop now exists — the deadline + probe), **F5** (lifecycle ops route
through the same outbox), and the C2 blocking-callback.

**Verified / refuted (no action):**
- *TTL-reset-on-overwrite* (ECH C2 / R5-6): **verified sound** — `loom-state` is History:1, so a re-arm
  PUT evicts the prior TTL'd message; an old step's deadline cannot fire after advance. Documented in
  `state.go` so it can't be silently broken by raising history.
- *CreateOnly self-retry wedge* (ECH H3): **refuted** — the transition batch is all-or-nothing, so a
  crash-retry re-GETs `PendingToken == newToken` and routes to the drop branch, never re-submitting.
- *Silent un-announced terminal* (BH F9): **refuted for transient failure** — the CompletePattern outbox
  record persists and the relay retries until the lane recovers; only "lost" if the relay is permanently
  dead (= platform down).
- *Concurrent advance double-side-effect* (BH F6 / ECH H2): correct — `CreateOnly` on the new token
  serializes racing advancers (loser's batch rejected); terminal double-exec is idempotent. Now commented.

**Fixed this pass (small, safe):** `StepTimeout` clamped to >= 1s (ECH M1, was silently degradable);
History:1 + CreateOnly invariants documented in `state.go`.

**Remaining follow-ups (NOT blocking; tracked as Epic 8 hardening stories 8.4 & 8.5, spawned 2026-06-07):**
- **F4 → Story 8.4 (durable-consumer backoff) — hot Nak-loop / no backoff on the relay + deadline durables** (BH F3 / ECH H1). Under
  *sustained* downstream failure (e.g. `ops.<lane>` down) the relay re-publishes / the deadline re-arms
  in a tight loop with no delay. Note: the relay SHOULD retry unboundedly (at-least-once delivery — a
  `MaxDeliver` cap would drop the op), so the fix is **`NakWithDelay`/backoff**, which needs a substrate
  enhancement (the `HandlerFunc` Decision enum carries no delay). Covers the trigger consumer too (the
  original F4).
- **In-flight-slow false-fail** (BH F5): if a step's commit latency exceeds `StepTimeout`, the probe
  (tracker absent + outbox absent) fails a healthy op. This is the **documented bounded divergence**
  (§10.6: deadline ≫ op latency; late commit finds the pointer gone → dropped + alerted) — inherent to
  the off-stream design, mitigated by the generous default. No code change; ensure deployments set
  `StepTimeout` ≫ real op latency.
- **F6 → Story 8.5 (per-domain consumer teardown)** — teardown on pattern removal (unchanged from the first review).
- **DeliverAll cold-start sweep** (ECH C1): on first creation of the relay/deadline durables they replay
  loom-state history (bounded by History:1, one msg/subject); mostly desired recovery, terminal markers
  no-op via the status guard. Acceptable; revisit if a `DeliverPolicy` knob is added to the substrate
  primitive.

### Superseded — first-review MUST-FIX (pre-Request-5)

> F1/F2/F3/F5 below are **RESOLVED** by Request 5 (see above). Retained for history; do not re-action.

#### MUST-FIX before `done`

- **F1 (AC#8 violation, cross-confirmed + verified by `go list -deps`).** `internal/loom` directly
  imports `github.com/nats-io/nats.go` **and** `.../jetstream` via `actuator.go` (op submit-with-reply:
  `conn.NATS().SubscribeSync` + `conn.JetStream().PublishMsg`). AC#2/#8 require "no new nats.io/jetstream
  handle in internal/loom"; `doc.go` falsely claims "only substrate + stdlib", and `boundary_test.go`
  doesn't catch it (only forbids processor/weaver/refractor). **Resolve:** add a `substrate` submit-with-
  reply helper and tighten the boundary test to forbid `nats-io/*`, OR amend AC#8/doc.go wording with
  Andrew's sign-off. Architectural decision — flagging, not silently working around.

- **F2 (Critical wedge — Blind Hunter C2 + Edge Case Hunter C2, code-confirmed `engine.go:341-350`).**
  In `submitStep`, `transition` commits the AtomicBatch (deletes `token.<oldToken>`) **before**
  `act.submit`. If `act.submit` returns a *transport* error (reply ctx deadline / publish failure /
  broker blip), it returns err → `handleCompletion` Naks → redelivery re-resolves `token.<oldToken>`
  which is already deleted → drop/Ack. Instance is parked at cursor N+1 with a `pendingToken` whose op
  was never submitted, **and there is no redelivery hook or timeout to rescue it → hangs forever.**

- **F3 (Critical — AC#4 backstop missing entirely, Edge Case Hunter C1, grep-confirmed).** No per-step
  timeout exists anywhere in non-test loom code (`time.After`/`AfterFunc`/timer → none). AC#4/§10.6
  mandate a *bounded per-step timeout* as the second off-stream terminal (also the not-silent backstop
  for a mis-declared `completionDomains`). Its absence is the root cause that turns F2, and "pattern
  removed/never-loaded mid-flight", from "eventually fails" into "hangs forever".

### SHOULD-FIX

- **F4 (High — trigger Nak-storm, Blind Hunter H1 / Edge Case Hunter M4).** `handleTrigger` returns a
  plain `Nak` (no delay) when the referenced pattern isn't loaded. A typo'd/deleted `patternRef` → tight
  infinite redelivery loop. Use `NakWithDelay` + a `MaxDeliver`→`Term`/alert cap.

- **F5 (High — terminal-flip-then-lose-event, Blind Hunter H2).** `complete`/`fail` flip `loom-state` to
  terminal (deleting the token) *before* the announce op; a transport error on that submit returns err →
  Nak → redelivery finds the pointer gone → drop. Net: instance terminal but `patternCompleted`/`Failed`
  never emitted and never retried. Make announcement genuinely best-effort (don't return the err / don't
  Nak), or retry the announce idempotently.

- **F6 (Medium — no consumer teardown, Blind Hunter M2 / Edge Case Hunter M6).** `reconcileConsumers` is
  additive-only; when the last pattern referencing a domain is removed, its `loom-<domain>` durable +
  goroutine leak forever. Contradicts the doc claim that an unreferenced domain's consumer "can be torn
  down without restart." Either implement teardown or downgrade the doc claim for skeleton scope.

- **F7 (Medium — pattern removed/spec-delete mid-flight, Edge Case Hunter M4/M5).** Deleting a
  `meta.loomPattern` (or only its `spec` aspect) strands running instances (`advance` → "pattern not
  loaded" → Nak forever) and can prevent a spec-only re-add from reloading. Depends on F3 for the
  not-silent terminal.

### NOTES (low / verify)

- **N1** `actuator.go` reply is consumed without matching `reply.RequestID == requestID` (per-call inbox
  makes cross-talk unlikely; cheap to harden). [Blind Hunter L7]
- **N2** Each per-domain consumer does a `loom-state` GET on *every* business event on
  `events.<domain>.>` even with zero waiting instances — scaling smell, not a bug. [Blind Hunter M3]
- **N3** gofmt: `gofmt -l` flags 60+ files repo-wide including many untouched by 8.1; **all new loom
  files are clean**. This is local gofmt-version skew, not a story regression — verify against CI's
  gofmt version before acting. [Acceptance Auditor GAP-2, reassessed]
- **N4** `e.ctx` write in `Start` is unsynchronized vs reads under `e.mu` in the consumer goroutine —
  likely `-race` flag, low real impact. [Blind Hunter M1]

## Dev Agent Record

> Reopened for rework against the ratified Contract #10 amendment
> (`cmd/loom/CONTRACT-AMENDMENT-REQUEST.md`): durable co-located `loom-state` token index +
> `AtomicBatch`/`AllowAtomicPublish`, `completionDomains`, §10.6 correlation rewrite (no in-memory
> index), and the §10.9 event-only trigger/lifecycle on the `loom` domain (instance stays
> operational-only). A prior implementation built the pre-amendment in-memory-index / direct-trigger /
> `<Flow>Complete` model and must be reconciled to the ACs/Tasks above before review resumes.

### Agent Model Used

claude-opus-4-8 (Amelia, dev-story rework sub-agent).

### Debug Log References

- **Gating zero-mutation-op verify (DONE FIRST, the §10.9 implementation check).** Traced the commit
  path: `step6_validate.go` iterates mutations (empty set passes), `commit_path.go` `primaryKey` guard
  only fires when `result.PrimaryKey != ""` (event-only ops set none), `step8_commit.go` builds the
  tracker op unconditionally + the outbox aspect when `len(events) > 0`, and `step7_events.go`
  `BuildEventList` constructs events independent of mutations. **No upstream guard rejects an empty
  `MutationBatch` when `result.Events` is non-empty.** Confirmed at runtime with a focused test
  (`processor.TestCommit_ZeroMutationEventOnly`): a `ScriptResult{Mutations:nil, Events:[loom.patternStarted]}`
  commits a tracker-only atomic batch + outbox aspect and surfaces the event in the CommitAck. **PASS —
  the event-only lifecycle ops are sound; no Processor change or workaround needed.**
- Substrate gap found + closed: `substrate.BatchOp` could not express a KV delete, but §10.3 mandates the
  prior `token.<oldToken>` be deleted **in the same AtomicBatch** as the cursor update + new-token write.
  Added a `BatchOp.Delete` flag that emits the NATS `KV-Operation: DEL` marker, so the whole step
  transition is one truly-atomic batch. Proven by `substrate.TestAtomicBatch_DeleteMarkerInBatch`.

### Completion Notes List

- **state.go** — rewrote to the two disjoint-prefixed keys `instance.<id>` (cursor) + `token.<token>`
  (`{instanceId}` reverse pointer). `transition()` is a single `AtomicBatch` (update instance, write new
  token, in-batch-delete prior token). `createInstance()` uses KVCreate so a duplicate trigger collapses.
  `resolveToken()` is the direct GET correlation. Removed the startup `scan`.
- **engine.go** — deleted the in-memory `pendingIndex`, `rebuildIndex`, `resubmitPending`, and the
  `indexReady` suspend-watch gate (invariant 3). Added the **fixed `events.loom.patternStarted` trigger
  consumer** (durable `loom-trigger`, idempotent on `instanceId` = StartLoomPattern requestId). Completion
  correlates by `resolveToken(requestId)`. Removed the direct-Go `StartInstance` entrypoint. Lifecycle:
  `complete()` submits `CompletePattern`, `fail()` submits `FailPattern` (event-only). The lifecycle op's
  requestId derives from a `lifecycleCursor = -1` sentinel so it never collides with a step token.
- **pattern.go** — `EventDomains` → `CompletionDomains`; `Domains()` now returns `[subjectType]` only when
  the field is omitted, else the declared set verbatim (per §10.5 the declared set is NOT unioned with
  subjectType). `Domains()`/`bindingRegistry` kept.
- **actuator.go** — generalized `submit()` to take an arbitrary payload map (systemOp + the lifecycle ops);
  reply-classification (accepted/duplicate → committed; else off-stream rejected) unchanged.
- **orchestration-base** (lives at `packages/orchestration-base/`, a Go `pkgmgr.Definition`) — authored the
  three EVENT-ONLY ops as a new `loomLifecycle` DDL (`loom_lifecycle.go`): `StartLoomPattern`,
  `CompletePattern`, `FailPattern`, each returning `{mutations:[], events:[loom.*]}`. Added their operator
  scope:any grants. Wired into `DDLs()`/`Permissions()`; updated `manifest.yaml`, `package.go` doc, and
  `package_test.go` counts (2 DDLs, 7 permissions). `StartLoomPattern` carries `authContext.target =
  vtx.meta.loomPattern.<id>` (set by the submitter). Class→subject: `loom.patternStarted` →
  `events.loom.patternStarted` (dots preserved; domain = `loom`).
- **primordial.go** — extended `enableAtomicPublish` from `CoreKVBucket`-only to also cover `loom-state`
  (the bucket-create entry was already present from the prior run). **verify-kernel.go** now asserts
  `AllowAtomicPublish` on both `KV_core-kv` and `KV_loom-state`.
- **e2e (loom_e2e_test.go)** — fully reworked: drives via a **real `StartLoomPattern` submission** that
  produces the trigger event (no direct Go call). The fake Processor now publishes the full Event envelope
  (top-level `requestId` + `payload{…}`) honoring the §10.6 seam, handles the three event-only lifecycle
  ops, and writes the Contract #4 tracker for dedup. Tests assert: `loom.patternStarted` drove instance
  creation, ordered systemOp commits, cursor exhaustion, `loom.patternCompleted`/`patternFailed` lifecycle,
  and **mid-run restart exactly-once correlating against the durable `token.` pointer** (no in-memory index).
- Module boundary (AC #8) holds — `internal/loom` imports only `substrate/*` + stdlib (boundary test green).

**Parked for Winston's triage:** none blocking. Note: the `BatchOp.Delete` addition to substrate is a small
shared-API change (used only by Loom today); it is covered by a dedicated substrate test and leaves all
existing AtomicBatch callers unaffected (default `Delete:false`).

### File List

- **CAR Request 5 reconciliation (command outbox), 2026-06-06:**
  - `internal/substrate/publish.go` (NEW — `Conn.Publish` fire-and-forget helper; lets loom drop the raw nats handle)
  - `internal/loom/state.go` (added `outbox.`/`deadline.` key shapes; `transition` writes the outbox record + arms/disarms the deadline TTL; `outboxExists`/`rearmDeadline` helpers)
  - `internal/loom/actuator.go` (the Actuator is now the command-outbox **relay** — durable consumer on loom-state `outbox.>` → `substrate.Publish` → delete-on-ack; removed the synchronous reply + the raw nats import)
  - `internal/loom/engine.go` (submitStep/complete/fail write via the outbox; added the deadline watcher + `onDeadline` read-before-act probe + `trackerExists`; `StepTimeout` config; relay+watcher goroutines in Start)
  - `internal/loom/boundary_test.go` (added `TestModuleBoundary_NoRawNATS` — direct-imports check forbidding `github.com/nats-io/*`)
  - `internal/loom/doc.go` (2+N → 3+N durables; relay + deadline)
  - `internal/loom/loom_e2e_test.go` (reject test now drives the failed terminal via the deadline+probe with a short `StepTimeout`; `newEngine` takes config overrides)
  - `docs/contracts/10-orchestration-surfaces.md` (§10.3 outbox+deadline keys; §10.6 deadline+probe terminal; invariant 1 outbox-inclusive; revision-history entry)
  - `docs/components/loom.md`, `_bmad-output/planning-artifacts/lattice-architecture.md` (command outbox + deadline + 3+N)
- `internal/loom/state.go` (rewritten — two-key durable store + AtomicBatch transition)
- `internal/loom/engine.go` (rewritten — trigger consumer, durable-token correlation, lifecycle ops; no in-memory index)
- `internal/loom/pattern.go` (CompletionDomains rename + default semantics)
- `internal/loom/actuator.go` (generic payload submit)
- `internal/loom/doc.go` (2+N durables, durable-token correlation, event-plane trigger)
- `internal/loom/loom_e2e_test.go` (rewritten — real StartLoomPattern trigger; full-Event fake Processor)
- `internal/loom/pattern_test.go` (updated domain-default tests)
- `internal/substrate/batch.go` (new `BatchOp.Delete` → in-batch KV delete marker)
- `internal/substrate/substrate_test.go` (TestAtomicBatch_DeleteMarkerInBatch)
- `packages/orchestration-base/loom_lifecycle.go` (NEW — loomLifecycle DDL + lifecycle permissions)
- `packages/orchestration-base/ddls.go` (wire loomLifecycle DDL)
- `packages/orchestration-base/permissions.go` (append lifecycle permissions)
- `packages/orchestration-base/package.go` (doc)
- `packages/orchestration-base/manifest.yaml` (declare loomLifecycle DDL + 3 permissions)
- `packages/orchestration-base/package_test.go` (counts + lifecycle op assertions)
- `internal/bootstrap/primordial.go` (enableAtomicPublish extended to loom-state)
- `internal/bootstrap/loom_state_bucket_test.go` (AllowAtomicPublish assertion)
- `scripts/verify-kernel.go` (AllowAtomicPublish assertion for core-kv + loom-state)
- `internal/processor/step8_commit_test.go` (TestCommit_ZeroMutationEventOnly — the gating verify)
- `cmd/loom/main.go` (unchanged this run; CompletionOperation field removed from Config — main never set it)
