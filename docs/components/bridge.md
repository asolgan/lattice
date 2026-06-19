# Bridge

**Component reference** | Audience: implementers + architects | Status: **Phase 2 — built (service actor 13.3 + component 13.4); end-to-end wiring lands in Epic 14** | Decided: 2026-06-18

> Decisions of record: `_bmad-output/planning-artifacts/sprint-change-proposal-2026-06-18.md`
> (the "External I/O Bridge" change proposal, RATIFIED 2026-06-18). Data shapes are frozen in
> `docs/contracts/10-orchestration-surfaces.md` — §10.5/§10.6 (Loom's `externalTask` step + the
> `payload.externalRef` correlation key), §10.3 (the retired `weaver-claims` / Two-Phase Nudge, and the
> pinned FR58-determinism invariant). The `external.<adapter>` event envelope spec lives **here** (it is
> package + bridge data, not a contract amendment — the `external` domain is ordinary, §10.5). Update
> this page in the same commit as the code; drift between page and code is a documentation bug.
>
> **Built (Stories 13.3–13.4).** The bridge service actor (13.3) and the component itself
> (`internal/bridge/` + `cmd/bridge/`, the adapter registry, the moved `Fake*` adapters, the FR58
> crash/retry proof — 13.4) are implemented. The end-to-end Loom→bridge wiring on a real `externalTask`
> is Epic 14 (Story 14.5); in 13.4 the `external.*` events are test fixtures.

---

## Overview

The bridge is the platform's **generic, trusted-infra egress** — the one component that makes
**outbound** calls to external systems (payment, background check, …). It is a **durable consumer on
`events.external.>`** that, for each event, dispatches to a **named adapter**, calls it **idempotently**,
and posts a **result op** back to `core-operations`. It owns the **adapter registry**.

The bridge is **vertex-type-agnostic.** It treats `instanceKey`/`externalRef` as an **opaque
correlation token** and the `replyOp` as package-data DDL; it never parses or assumes the claim vertex's
type. The lease demo's `service.<x>.instance` is **one package's** modeling choice — not a bridge
constraint. A package could anchor its external-call claim on any vertex type its DDL defines.

The bridge exists because external I/O does not belong in either orchestration engine. Weaver
*detects* divergence; Loom *executes* deterministic procedures. Embedding outbound calls in Weaver's
convergence lane (the retired Two-Phase Nudge) smeared I/O into detection and forced Weaver to
re-implement durable-claim / idempotency / recovery machinery Loom already has. The bridge isolates
**all** external I/O in one purpose-built, trusted component, and keeps Loom and Weaver pure and
event-driven — consistent with Lattice's CDC / event-sourced spine.

External calls become **event-driven and symmetric to userTasks**: Loom dispatches to an async
completer and parks; the completer is a **human** (userTask) or the **bridge** (`externalTask`). See
`docs/components/loom.md` → "External steps (`externalTask`)".

The bridge is an **internal service actor** at root-equivalent capability (a third primordial identity
alongside Loom and Weaver — see "Service actor" below). It **submits operations through the Processor**
(the result op); it never writes Core KV directly.

---

## The `external.<adapter>` event envelope

Emitted by the **`externalTask`'s `instanceOp` DDL** via the Processor's transactional event outbox —
**not** by Loom directly (Loom stays pure; the instance commits *before* the event is publishable, so
NFR-S11 "visible claim before the call" holds structurally). The `external` domain is **ordinary** (the
open `<domain>.<eventName>` model — no Processor allowlist, no Contract #3 amendment); the event-type is
declared as package DDL.

```
class:   external.<adapter>                 # e.g. external.backgroundCheck, external.stripe
payload: {
  "instanceKey":    "<handle>",             # opaque correlation token (a bare handle in the reference vertical); Loom minted it write-ahead and the instanceOp's DDL forms the claim-vertex key vtx.<type>.<handle> from it (package-chosen type; demo: vtx.service.<id>). The bridge never parses it.
  "adapter":        "<name>",               # which registered adapter to dispatch to
  "params":         { … },                  # adapter call inputs (resolved from the Loom step's row/subject templates)
  "replyOp":        "<ResolveOp>",          # the result-op type the bridge posts back
  "idempotencyKey": "<instanceKey>",        # = instanceKey; handed to the adapter so IT dedups the real external action
  "externalRef":    "<instanceKey>"         # = instanceKey; echoed back on the result op so Loom's correlationKeys resolves
}
```

`idempotencyKey` and `externalRef` are both the **instance key** — one claim vertex = one external
call. The instance key is the single binding token across the whole loop: the claim, the adapter's
dedup key, the result holder, and Loom's park handle. The bridge treats it as an **opaque token** — it
never parses the type segment or assumes what the claim vertex is.

---

## The flow (end-to-end)

```
Weaver detects a stale/absent gap  →  triggerLoom the execution pattern
  Loom pattern: … → externalTask:
    Loom submits the step's instanceOp (instanceKey minted write-ahead)
      └─ instanceOp DDL: (a) CREATE the claim vertex — package-chosen type, Core KV
                             (the lease demo uses vtx.service.<id>, class service.<x>.instance)
                         (b) EMIT external.<adapter>{…} via the op's transactional outbox
    Loom PARKS on token.<instanceKey>                         (the externalRef correlation key, §10.6)
  ┌─────────────────────────────────────────────────────────────────────────────────────────────┐
  │ BRIDGE: durable consumer on events.external.>  (instanceKey/externalRef are opaque tokens)    │
  │   → (optional) skip redundant call on redelivery: GET vtx.op.<deterministic-reqId> tracker    │
  │   → dispatch to the named adapter, idempotencyKey = instanceKey                               │
  │   → post replyOp to core-operations:                                                          │
  │        requestId = deterministic(instanceKey)            (redelivery collapses on Contract #4) │
  │        payload   = { externalRef: instanceKey, <outcome fields> }                             │
  └─────────────────────────────────────────────────────────────────────────────────────────────┘
  replyOp DDL commits → records the outcome as ASPECT(s) on the claim vertex (D5; root data stays
                        minimal) → emits orchestration.externalTaskCompleted{externalRef}
    → Loom correlationKeys: payload.externalRef → live token.<instanceKey> GET → instance → ADVANCE
    → the actorAggregate convergence lens reprojects (the claim's outcome aspect changed) → gap clears
```

A later Loom step may branch on the outcome (this is a genuine wait-for-completion, not
fire-and-forget).

---

## Idempotency & FR58 (the hard invariant)

External calls must be **at-most-once-effective** under at-least-once event redelivery and crash/retry.
Three mechanisms — the first two are the load-bearing guarantees (pinned by Contract #10 §10.3); the
third is an optimization:

1. **Deterministic result-op requestId (pinned invariant).** The bridge's result-op `requestId` **MUST**
   be `deterministic(idempotencyKey = instanceKey)`. A redelivered `external.*` event therefore produces
   the **same** result-op requestId, which collapses on the Contract #4 `vtx.op.<requestId>` tracker
   (`internal/processor/step2_dedup.go`) → **exactly one** result mutation. Without it a redelivery
   double-writes the result. This is the event-plane analog of the §10.4 deterministic-requestId rule
   for the fired-timer→op path. **Generic** — the op tracker is the same key shape for every op.
2. **Adapter `idempotencyKey` dedup.** The adapter is called with `idempotencyKey = instanceKey` and
   **must** dedup the real external action on it (a contract requirement of every adapter). So even a
   redelivered event that re-reaches the adapter produces **no** duplicate external action. Correctness
   holds via (1) + (2) **without** the bridge reading any vertex.
3. **(Optional) skip the redundant call on redelivery — generic, no typed read.** Before dispatching,
   the bridge MAY GET the **Contract #4 op tracker for its own deterministic result-op `requestId`**
   (`vtx.op.<deterministic-reqId>`): present (and not tombstoned) → the result already landed → ack
   without re-calling. This uses the **generic** op tracker (same key shape for all ops), **not** a read
   of the typed claim vertex — so the bridge stays vertex-type-agnostic. Purely an optimization to avoid
   a redundant adapter round-trip that (2) would dedup anyway.

**The claim vertex IS the visible claim — structurally, before the bridge acts.** FR58 / NFR-S11 ("a
visible claim recorded before the external call") is satisfied **more honestly** than the retired
`weaver-claims` record: the claim vertex is created by the `instanceOp` **before** the `external.*`
event is even publishable (the event rides that op's post-commit outbox), so the claim is **always**
visible before the bridge consumes the event — the bridge needs **no read** to guarantee it. The vertex
unifies the claim + the result holder + the audit record into **one auditable business vertex** in Core
KV; its **type is package-chosen** (the lease demo's `service.<x>.instance`), and its **outcome lives in
aspect(s)** per **D5**, not fat root `data`.

The FR58 crash/retry idempotency proof lands **with the bridge**, on a **bridge-only harness** (Story 13.4 —
pulled forward, not deferred to the final lease e2e): `FakeStripe.FailUntil` / `SideEffects == 1` under
event redelivery + mid-flight-failure recovery.

---

## Service actor (a third primordial identity)

The bridge posts its result ops under a **bootstrap-provisioned service actor** —
`identity.system.bridge`, operator-equivalent, established purely by a `holdsRole → operator` edge,
exactly like Loom and Weaver (`docs/components/service-actors.md`). Consequences handled in Story 13.3:

- It is a **third** primordial service identity → it **moves the `verify-kernel` assertion count** and
  the bootstrap-file identity set. The bootstrap-file `version` bumps (a hard mismatch → `make down &&
  make up`, no in-place migration — see service-actors.md), and **both** kernel-verify enumerations
  (`scripts/verify-kernel.go`, `internal/bootstrap/verify.go`) update **in lockstep** (the 12.4 lesson).
- `protected: true` (a package uninstall must never tombstone a kernel service actor).
- When lane enforcement lands, its capability projection must include the `system` lane (same carry as
  Loom/Weaver).

---

## What this component owns

| Path | Role |
|------|------|
| `internal/bridge/` | Engine: durable `events.external.>` consumer, adapter registry, idempotent dispatch (deterministic result-op requestId + adapter `idempotencyKey` dedup; optional generic op-tracker skip-on-redelivery), result-op submission to `ops.<lane>`. Also the `Fake*` adapters **moved** (not copied) from `internal/weaver/nudge/` — `FakeBackgroundCheck`, `FakeStripe` (substrate-only, idempotent on `idempotencyKey`); real Stripe / background-check is Phase 3 |
| `cmd/bridge/` | Binary entry point (extractable; shares only `substrate/*`); pins `ActorKey = bootstrap.BridgeIdentityKey` and registers the reference adapters for the demo |

**Engine vs package:** the consumer, registry, dispatch, recovery, and result-op submission are
**engine code**. Which adapters exist, the `external.<adapter>` event-type DDL, the `instanceOp` /
`replyOp` DDLs, and the Loom patterns that emit them are **package data**.

---

## In / Out contracts

| Direction | Contract | Notes |
|-----------|----------|-------|
| In | `events.external.>` durable consumer | one fixed durable; the envelope above; domain is ordinary (no allowlist) |
| In (optional) | the Contract #4 op tracker `vtx.op.<deterministic-reqId>` | generic skip-on-redelivery probe (same key shape for all ops) — **not** a read of the typed claim vertex; the bridge stays type-agnostic |
| Out | `replyOp` result op via `core-operations` | `requestId = deterministic(instanceKey)`; `payload.externalRef = instanceKey` + outcome fields; its DDL records the outcome as **aspect(s)** on the claim vertex (D5) **and emits `orchestration.externalTaskCompleted{externalRef}`** (the uniform Loom completion signal, §10.6); submitted under the bridge service actor |
| Out | adapter calls | the actual external I/O — the only component that makes them; `idempotencyKey = instanceKey` |
| Out | Health (Contract #5) | heartbeat at `health.bridge.<instance>`; an unregistered adapter / unparseable envelope surfaces an issue, never a silent skip |

---

## State & crash-safety

The bridge holds **no durable bucket of its own** — this is the deliberate simplification over the
retired Two-Phase Nudge (which needed `weaver-claims`). Its durable state is:

- the **claim vertex** (Core KV, written by the `instanceOp`; outcome recorded as aspect(s) by the
  result op) — the claim + outcome + audit, **type package-chosen**;
- the **`events.external.>` consumer ack floor** — missed-while-down recovery resumes from it;
- the **Contract #4 op tracker** for the deterministic result-op requestId — the dedup record.

Crash points and their recovery:

| Crash point | Recovery |
|-------------|----------|
| After event consumed, before adapter call | redelivery from the ack floor → the optional op-tracker probe finds no result → call proceeds (or, without the probe, it proceeds and the adapter dedups) |
| After adapter call, before result op | redelivery → the adapter dedups on `idempotencyKey` → the re-call is a no-op → the result op posts (deterministic requestId) |
| After result op published, before ack | redelivery → the deterministic result-op requestId collapses on the Contract #4 tracker → exactly one mutation |
| Result op rejected at the Processor | off-stream; Loom's `externalTask` deadline backstop (§10.6) probes the instanceOp tracker and re-arms / fails — the same path as a systemOp |

---

## Failure modes

| Mode | Behavior |
|------|----------|
| Unregistered adapter named in an event | `errConfig` posture — Ack + a Health issue (redelivery can never fix a name the registry lacks); never a silent skip |
| Adapter call fails transiently | re-attempt on the same `idempotencyKey` (the adapter dedups); bounded-cadence redelivery, never a hot loop |
| Adapter panics | panic-contained — the framework, not the adapter, is the safety boundary; the event is re-drivable, the dispatch goroutine survives |
| Never-completing external call | Loom's `externalTask` per-step deadline (§10.6) is the backstop on the *waiting* side — the bridge itself does not wedge Loom |
| Poison event | head-of-line blocks the `external` consumer only (domain-scoped blast radius) |

---

## Principles that apply

- **P2** — the Processor is the sole Core KV writer / event producer; the bridge is a client (the result
  op goes through the ledger; the `external.*` event is emitted by the `instanceOp`'s outbox, not a
  bridge publish).
- **P1** — the claim vertex is business state (Core KV; outcome in aspect(s) per D5); the bridge keeps no
  operational bucket.
- **Decision #10 / "everything is a package"** — the bridge engine is generic; adapters + event-types +
  patterns are package data. A new external integration is **just a new adapter registration + a Loom
  pattern** — no new component.
- **Module boundary** — `bridge` imports only `substrate/*`; it talks to the Processor / Loom via NATS,
  never Go calls.

---

## Build status (Epic 13, stories 13.1 → 13.5)

- **13.1 (done)** — gating contract amendments ratified + applied (this page, the Loom/Weaver doc
  updates, Contract #10 §10.2/§10.3/§10.5/§10.6/§10.8).
- **13.2** — Loom `externalTask` step kind (the `external` event needs no Processor/bootstrap change).
- **13.3 (done)** — the **bridge service actor** + bootstrap provisioning (bootstrap-file version bump;
  both verify enumerations in lockstep).
- **13.4 (done)** — **this component**: `internal/bridge/` + `cmd/bridge/`, the adapter registry, the
  moved `Fake*` adapters, the FR58 crash/retry proof on a bridge-only harness. The adapter contract
  types (`Adapter`/`Registry`/`Request`/`Result`) live here. The `external.*` events are test fixtures;
  the result-op `requestId` is `deriveReplyRequestID(instanceKey)` and the bridge holds no durable
  bucket of its own.
- **13.5 (done)** — retired Weaver's former external-I/O path: the `internal/weaver/nudge/` package and
  the `weaver-claims` bucket are gone, and `internal/weaver` depends on neither `internal/bridge` nor any
  adapter contract. External I/O is `triggerLoom` of an `externalTask`, executed by this component.

## Deferred (Phase 3+)

- Real external adapters (Stripe, background-check) — Phase 3 integration; Phase 2 uses the substrate-only
  `Fake*` adapters.
- Generic egress as a first-class platform feature beyond the two demo integrations (the PM's
  "Phase-3 dividend" framing) — the bridge is built generic now, inside the reference vertical, but its
  broad reuse is a Phase-3 concern.
