# Story 13.4 — Bridge component (consumer, registry, moved adapters, FR58 proof)

**Status:** review
**Epic:** 13 — External I/O Bridge (orchestration core)
**Tier:** Opus — **largest, highest-risk story in the epic.** Net-new long-running engine (`internal/bridge` + `cmd/bridge`) on the security plane (posts result-ops under the 13.3 root-equivalent bridge service actor), PLUS a cross-package **move** (`internal/weaver/nudge/` adapters → `internal/bridge/`) that must NOT break the still-live Weaver nudge path. Review: **full 3-layer adversarial** (Blind Hunter / Edge Case Hunter / Acceptance Auditor) + **Gate 2 (`make test-bypass`, BLOCKED)** + **Gate 3 (`make test-capability-adversarial`, DEFENDED — the proof the coexisting nudge path is intact)** + the FR58 bridge-only proof.
**Epic spec:** `_bmad-output/planning-artifacts/epics/phase-2-epics.md` → "Story 13.4: Bridge component — consumer, registry, moved adapters, FR58 proof" (lines ~621–636) + the Epic 13 framing (~565–582). Read it for the user-story framing and the **four** ACs (verbatim in § 1).
**Binding grounding (FROZEN — read, build TO, do NOT edit):**
- `docs/contracts/10-orchestration-surfaces.md` §10.3 — the `weaver-claims` retirement note + **the pinned FR58-determinism invariant** (lines ~303–337: the bridge's result-op `requestId` MUST be `deterministic(idempotencyKey = instanceKey)`, collapsing on the Contract #4 `vtx.op.<requestId>` tracker); §10.6 — the `externalTask` correlation row (line ~544) + the `payload.externalRef` correlation key (~546–555) — **the seam the bridge feeds** (Loom parks on the token; the bridge echoes it back).
- **Contract #4** (idempotency tracker `vtx.op.<requestId>`) — `internal/processor/step2_dedup.go` is the dedup gate; the bridge's optional skip-on-redelivery GETs the same key shape.
- Contract #1 §1.1 (key shapes); Contract #5 (Health heartbeat + issue surfacing); Contract #2 §2.1 (the op envelope shape published to `ops.<lane>`). D5 (outcome in aspect(s), root data minimal).
**Design of record (RATIFIED):** `_bmad-output/planning-artifacts/sprint-change-proposal-2026-06-18.md` (the "External I/O Bridge" change proposal) — decisions M4 (FR58 determinism is THIS story's concern), m7 (the `external.*` event rides the instanceOp outbox, not the bridge), task split (13.4 builds the bridge; 14.4 the real DDLs; 14.5 the e2e wiring).
**Component doc of record (THIS IS THE SPEC — you MAY edit it, it is a `/docs` component page):** `docs/components/bridge.md` IN FULL — the `external.<adapter>` envelope (~47–70), the end-to-end flow (~74–96), the idempotency section / the **three mechanisms** (~103–137), "what this component owns" (~157–167), the in/out contracts (~171–179), state & crash-safety (~183–201), failure modes (~204–213), the service-actor section (~141–154), and the principles (~216–227). Your code must match this page; **drift between page and code is a documentation bug** (the page says so, ~11). Update the page's build-status (~231–241) in the same change (mark 13.4 done).
**Depends on:** 13.2 (DONE — Loom mints the `externalTask` write-ahead **bare handle** and emits the `external.*` event via the instanceOp outbox), 13.3 (DONE — `identity.system.bridge` is provisioned; you consume `bootstrap.BridgeIdentityKey`). The real `instanceOp`/`replyOp`/`external.<adapter>` DDLs are **14.4**; the end-to-end Loom→bridge wiring is **14.5**. In 13.4 the `external.*` events are **TEST FIXTURES** (mirror exactly how 13.2 used the `fakeProcessor` fixture). The nudge-path teardown is **13.5** (BLOCKED until 14.5 green — the nudge path **stays live** through this story).
**Workflow:** you are the DS (dev) sub-agent. Repo root, no worktree. Do **NOT** commit/push/branch. Do **NOT** edit frozen contracts (`docs/contracts/*`) or planning artifacts (`epics/*.md`, `lattice-architecture.md`). You MAY edit `/docs/components/*` (`bridge.md` — mark it built; `weaver.md`/`service-actors.md` only if the move forces a doc-true correction). A genuine frozen-contract gap → `cmd/bridge/CONTRACT-AMENDMENT-REQUEST.md` + flag at the TOP of your closing summary; do **not** edit the contract. Leave all changes in the working tree for Winston.

> **TOP-OF-STORY FLAG:** **No CONTRACT-AMENDMENT-REQUEST is anticipated.** Every surface this story builds to is satisfiable literally: the `external.<adapter>` envelope + the bridge are **package/engine data** (not a frozen contract — the `external` domain is ordinary, §10.5/bridge.md ~51), the FR58-determinism rule (§10.3) is a rule the bridge **obeys** (it derives the result-op requestId), and the service actor (13.3) is already provisioned. If you find a genuine gap, flag it; do not edit the contract.

---

## 0. THE HEADLINE — THE MOVE-WITHOUT-BREAKING-COEXISTENCE (read this first; it governs everything below)

This is the #1 thing to get right. The story has two halves; **the move is the dangerous one.**

### 0.1 The tension

AC #3 says the two `Fake*` adapters are **moved (not copied)** from `internal/weaver/nudge/` to `internal/bridge/`. But the **Weaver Two-Phase Nudge path is still live** (13.5 retires it, and 13.5 is BLOCKED until 14.5 green — see the Epic 13 build order). The moved types are **entangled with the staying protocol**:

**What the dev MUST internalize (verified by grep — see § 2 "Coexistence grep"):**

- The **adapter contract types** — `Adapter` (interface), `Request`, `Result`, `AdapterFunc`, `Registry` — live in `internal/weaver/nudge/adapter.go`.
- The **two `Fake*` adapters** — `FakeStripe` (`fake_stripe.go`, with `FailUntil`/`FailNext`/`SideEffects`/`Execute` keyed on `IdempotencyKey`) and `FakeBackgroundCheck` (`fake_background_check.go`) — implement `Adapter.Execute(ctx, Request) (Result, error)`.
- The **staying protocol** (`protocol.go`: `Nudger`, `Dispatch`, `ResolveFunc`, `execute()`) **directly references `Adapter`, `Request`, `Result`, `Registry`** — e.g. `Dispatch.request()` returns a `Request` (protocol.go ~61), `execute(ctx, adapter Adapter, req Request) (Result, …)` (~197), `Nudger.registry *Registry` (~40), `NewNudger(claims, registry *Registry)` (~44).
- **Weaver's engine** uses them: `internal/weaver/engine.go` has `adapters *nudge.Registry` (~206), `NewRegistry()` (~292), `RegisterAdapter(name string, a nudge.Adapter)` (~502), `resolveFunc(...) nudge.ResolveFunc` returning `nudge.Result` (~518); `internal/weaver/evaluator.go` builds `nudge.Dispatch` (~293).
- **`cmd/weaver/main.go`** constructs the live adapters: `nudge.NewFakeStripe()` / `nudge.NewFakeBackgroundCheck()` and registers them via `nudge.Adapter` (~111–113).
- **Tests** that exercise the live path: `internal/weaver/nudge_dispatch_internal_test.go` (the FR58 nudge proof — `nudge.NewFakeStripe`, `nudge.Claim`, `nudge.StateResolved`, …).

So **the adapter contract types cannot simply vanish from `nudge` — the staying protocol compiles against them.** This is the move-vs-coexistence design problem.

### 0.2 The resolution (BINDING — the dependency direction)

**Move the adapter contract types + the two `Fake*` adapters to `internal/bridge/` (the new package). The still-live `nudge` protocol then references them via a temporary `internal/weaver/nudge → internal/bridge` import; `cmd/weaver/main.go` constructs the `Fake*` via the `bridge` package. Both temporary dependencies are removed wholesale in 13.5 when the entire `nudge` package is deleted.**

Concretely:

1. **Move to `internal/bridge/` (package `bridge`), verbatim:** `adapter.go` (→ `internal/bridge/adapter.go`, package `bridge`) carrying `Adapter`, `Request`, `Result`, `AdapterFunc`, `Registry`/`NewRegistry`/`Register`/`Lookup`; `fake_stripe.go` + `fake_background_check.go` (→ `internal/bridge/`, package `bridge`); and their tests `adapter_test.go` + `fake_stripe_test.go` + `boundary_test.go` (→ `internal/bridge/`, package `bridge_test`). **Change ONLY the package clause** (and, in the moved tests, the import path `…/internal/weaver/nudge` → `…/internal/bridge` and the `nudge.` → `bridge.` qualifiers + the `go list -deps …/internal/weaver/nudge` target → `…/internal/bridge` in `boundary_test.go`). **Do NOT reshape `Request`/`Result`** — see § 0.3.
2. **Update the staying `nudge` package to reference `bridge`:** `internal/weaver/nudge/protocol.go` adds `import "…/internal/bridge"` and changes `Adapter`→`bridge.Adapter`, `Request`→`bridge.Request`, `Result`→`bridge.Result`, `Registry`→`bridge.Registry` (and `NewNudger`'s param type, `ResolveFunc`'s `Result`). `claims.go` references `Adapter` only in field/string positions (`Adapter string` is a plain string field — NOT the interface — so claims.go likely needs **no** change; confirm by reading). `doc.go` prose may mention the framework moved — keep it describing **present** behavior (no history narration). **This creates `internal/weaver/nudge → internal/bridge` (one direction only).**
3. **Update Weaver's consumers to reference `bridge`:** `internal/weaver/engine.go` `nudge.Registry`→`bridge.Registry`, `nudge.NewRegistry()`→`bridge.NewRegistry()`, `RegisterAdapter(name string, a nudge.Adapter)`→`a bridge.Adapter`, `resolveFunc` `nudge.Result`→`bridge.Result`; `internal/weaver/evaluator.go` `nudge.Dispatch` stays in `nudge` (Dispatch is protocol, not adapter) but its `request()` now returns `bridge.Request` internally — confirm whether evaluator.go references `Request`/`Result` directly (it references `nudge.Dispatch`/`nudge.Claim`/`nudge.State*`/`nudge.ErrAdapterNotFound`, all of which **stay** in `nudge`, so evaluator.go likely needs **no** adapter-type change — verify). `cmd/weaver/main.go` `nudge.NewFakeStripe()`/`nudge.NewFakeBackgroundCheck()`→`bridge.NewFakeStripe()`/`bridge.NewFakeBackgroundCheck()` and the map type `nudge.Adapter`→`bridge.Adapter` (add the `bridge` import; the `RegisterAdapter` call now takes a `bridge.Adapter`). **This creates `internal/weaver → internal/bridge` and `cmd/weaver → internal/bridge` (one direction only).**
4. **No import cycle** — `internal/bridge` imports ONLY `internal/substrate` (the module-boundary rule, bridge.md ~226). It never imports `internal/weaver`, `internal/weaver/nudge`, `internal/loom`, `internal/processor`, or raw `nats.go`. The moved `boundary_test.go` (now `bridge_test`) asserts exactly this for the new package.

**Why this direction (not the reverse, not a third shared package):** the bridge is the *destination* the adapters belong to long-term (13.5 deletes `nudge` entirely, leaving the adapters only in `bridge`). Pointing the dying `nudge`/Weaver at the surviving `bridge` means 13.5 is a pure deletion (drop the import + the package), not a second move. A third shared package (e.g. `internal/adapters`) would survive 13.5 as a needless extra hop and is over-engineering — say so if a reviewer proposes it. **If, on reading, you find an import cycle would result** (it should not — `bridge` is a leaf on `substrate`), halt and report; do **not** introduce `internal/bridge` importing anything from `internal/weaver`.

### 0.3 Do NOT reshape `Request`/`Result` in this story

`Request` today is nudge-shaped: `{ IdempotencyKey, Operation, Subject, Params map[string]string }` (adapter.go ~15). `Result` is `{ Detail string }`. The staying nudge protocol AND the bridge both consume these. **Move them verbatim.** The bridge populates `Request` from the `external.<adapter>` envelope: `IdempotencyKey = instanceKey` (the load-bearing field — every adapter dedups on it), and maps the envelope's `params`/`adapter`/instanceKey onto the remaining fields as convenient (`Subject` ← `instanceKey` or a params value; `Params` ← the envelope `params`, coerced to `map[string]string` if non-empty — the `Fake*` adapters ignore everything but `IdempotencyKey`, so the mapping only needs to be *correct*, not rich). The `Result.Detail` becomes (part of) the `replyOp` payload's outcome fields. **Reshaping `Request`/`Result` would ripple into the live nudge protocol — out of scope; the clean adapter-interface redesign, if any, is a Phase-3 concern.** Note the envelope's `params` is free-form JSON (bridge.md ~60); the `Fake*` adapters don't read it, so coercing/ignoring it is safe — pick the simplest correct mapping and document it. (Open Question Q3 records the `Request` field-shape mismatch for a future tidy.)

### 0.4 The coexistence is the gate, not an aside

After the move, the live nudge path must still build and pass its adversarial proof. **`go build ./...` + Gate 3 (`make test-capability-adversarial`, convergence DEFENDED) are the proof the live nudge path is intact** — Gate 3 brings the full stack up and runs the capability-adversarial suite; a broken Weaver nudge wiring (or a broken adapter move) shows up there. **`go test ./internal/weaver/... -count=1`** (incl. `nudge_dispatch_internal_test.go`, the live FR58 nudge proof) is the unit-level proof. If either fails after the move, the move is wrong — fix it before touching the bridge runtime.

---

## 1. The four ACs (verbatim) + adjudication

### The ACs (from `phase-2-epics.md` ~627–636)

> **Given** `internal/bridge/` + `cmd/bridge/` with a durable consumer on `events.external.>`
> **When** an `external.<adapter>` event is delivered
> **Then** the bridge dispatches to the named registered adapter with `idempotencyKey = instanceKey` and posts `replyOp` to `core-operations` with **`requestId = deterministic(instanceKey)`** + `payload.externalRef = instanceKey` (under the 13.3 service actor)
> **And** it is **vertex-type-agnostic** — treats `instanceKey`/`externalRef` as opaque; the optional skip-on-redelivery uses the **generic Contract #4 op tracker** (`vtx.op.<det-reqId>`), not a typed-vertex read
> **And** the two `Fake*` adapters are **moved** (not copied) from `internal/weaver/nudge/`; an unregistered adapter is `errConfig` (Ack + Health), never a silent skip
> **And** the **FR58 crash/retry proof** passes on a **bridge-only harness** (`FakeStripe.FailUntil` + `SideEffects == 1` under event redelivery + mid-flight-failure recovery), exercised with a **non-`service` claim type** (invariant a)

### Two invariants on EVERY Epic-13/14 AC (Andrew, 2026-06-18; epics ~579–581)

- **(a) type-agnostic** — proven by a **non-`service` fixture vertex type**, **not asserted**. The bridge never parses the `instanceKey`/`externalRef` type segment and never reads a typed claim vertex; the FR58 harness uses a non-`service` claim type so a hardcoded type would break the test.
- **(b) D5** — the external outcome is recorded as **aspect(s)** on the claim vertex, vertex root `data` minimal, **gate-asserted**. *Nuance for 13.4:* the bridge does **not** write the claim vertex — the **`replyOp` DDL** does, and the real `replyOp` DDL is **14.4**. So D5 is **not** the bridge's to enforce in 13.4. The bridge's job is to post a `replyOp` op whose `payload` carries `externalRef = instanceKey` + the outcome fields; **where** that outcome lands (aspect vs root) is the DDL's concern (14.4) and was already gate-asserted by the 13.2 fixture. **Do not** try to assert D5 from the bridge harness unless your fixture `replyOp` DDL models it; if your harness includes a fixture replyOp DDL that commits, mirror 13.2's "outcome in aspect, root minimal" assertion (a bonus, not the AC). Record this scoping in the summary so a reviewer doesn't flag "missing D5 assertion."

### Scope boundary

**In scope:**
1. **`internal/bridge/`** — the new package: a durable consumer on `events.external.>` (one fixed durable via `substrate.ConsumerSupervisor`), the **adapter registry** (moved), idempotent **dispatch**, and **result-op submission** to `ops.<lane>` via `substrate.Conn.Publish` (the op envelope shape Loom's relay builds), plus the **Contract #5 Health heartbeat** and the `errConfig` Ack-and-alert path. Imports ONLY `substrate/*`.
2. **`cmd/bridge/main.go`** — the binary entry point that pins `ActorKey = bootstrap.BridgeIdentityKey` (mirroring `cmd/loom/main.go` ~64 / `cmd/weaver/main.go` ~80), wires the engine, and registers the reference `Fake*` adapters for the demo.
3. **The move** (§ 0): the adapter contract types + the two `Fake*` + their tests relocate to `internal/bridge/`; the staying nudge protocol + Weaver + `cmd/weaver` re-point at `bridge`.
4. **`deterministic(instanceKey)`** — the bridge's own derivation of the result-op `requestId` from the opaque `instanceKey` (the FR58-determinism invariant, §10.3 / bridge.md mechanism #1). Bridge-owned (Loom does not share it — § 3).
5. The **FR58 bridge-only harness** (AC #4) — fixture `external.<adapter>` events + `FakeStripe.FailUntil` + `SideEffects == 1` under redelivery + mid-flight recovery, with a **non-`service`** claim type (invariant a).
6. The **optional skip-on-redelivery** via the generic Contract #4 op tracker GET (`vtx.op.<deterministic(instanceKey)>`) — mechanism #3; an optimization, not a correctness requirement (mechanism #1 + #2 hold without it). Build it (it is cheap and named in AC #2/#3), but the harness must prove correctness holds **with or without** it (see § 5).
7. `docs/components/bridge.md` build-status update (mark 13.4 built) + any doc-true correction the move forces in `weaver.md`/`service-actors.md`.

**Out of scope (do NOT build — later/other stories):**
- The **real** `instanceOp`/`replyOp` DDLs + the **real** `external.<adapter>` event-type DDL → **Story 14.4** (`lease-signing` / service domain). In 13.4 these are **TEST FIXTURES** only (the harness publishes fixture `external.*` events and, optionally, runs a fixture replyOp consumer to model the Processor's tracker write).
- The **end-to-end Loom→bridge wiring** (a real `externalTask` step driving a real bridge call through to a Loom advance) → **Story 14.5**. 13.4 proves the bridge in isolation on fixture events.
- **Retiring the nudge path** → **Story 13.5** (BLOCKED until 14.5). The nudge path **stays live**; you only re-point its imports at `bridge` (§ 0.2). Do **not** delete `claims.go`/`protocol.go`/`doc.go`, the `nudge_dispatch_internal_test.go` proof, or the Weaver nudge wiring.
- Any **change to 13.3's bootstrap provisioning** — the bridge identity, the readiness gate, the verify counts are DONE. You only **consume** `bootstrap.BridgeIdentityKey` (+ `BridgeIdentityID` if you need the bare id). No `internal/bootstrap/*` edit, no verify-kernel count change.
- **Real external adapters** (Stripe, background-check) — Phase 3. 13.4 uses the substrate-only `Fake*`.
- **Reshaping `Request`/`Result`** (§ 0.3) — verbatim move only.
- A **durable bucket of the bridge's own** — the bridge holds none (§ 2, the outbox-or-not decision); bridge.md ~183–186 is explicit.
- Adding `cmd/bridge` to `make up` / a compose service — **investigated, deferred** (§ 2, "Process launch"); neither `cmd/loom` nor `cmd/weaver` is in `make up` or `docker-compose.yml` today, so the bridge follows suit. The end-to-end process orchestration is a 14.5 concern.

---

## 2. The bridge runtime (item-by-item — DS builds to THIS)

The bridge is **simpler than Loom**: no cursor, no token index, no outbox, no deadline watcher, no pattern source. It is a **stateless dispatcher** — consume an event, call an adapter, publish one op, ack. Mirror Loom's *substrate wiring* (consumer supervisor, heartbeater, op-envelope publish), not its state machinery.

### Item A — the package skeleton (`internal/bridge/`)

- **`adapter.go`** (moved, package `bridge`) — `Adapter`/`Request`/`Result`/`AdapterFunc`/`Registry` (§ 0.2). No change beyond the package clause.
- **`fake_stripe.go`, `fake_background_check.go`** (moved, package `bridge`) — the `Fake*` adapters. No change beyond the package clause.
- **`engine.go`** (new) — the `Engine` + `Config` + `NewEngine` + `Start`, modeled on `internal/loom/engine.go` (the supervised-consumer + heartbeater spine, ~211–269) but stripped to: one consumer spec (the `events.external.>` durable), the heartbeater, and the registry. `Config` carries `EventsStream` (core-events), `CoreKVBucket` (for the optional tracker GET), `HealthKVBucket`, `ActorKey` (= `BridgeIdentityKey`), `Lane`, `Instance`, `HeartbeatEvery`, `Logger`. **No `LoomStateBucket`-equivalent** (no durable bucket of its own).
- **`consumer.go`** (new, or fold into engine.go) — the `events.external.>` `substrate.ConsumerSpec` (one fixed durable, e.g. `bridge-external`), `DeliverPolicy: DeliverAll`, `Handler: supervisedHandler(e.handleExternal)`, `Health: e.healthSinkFor(name)`. Mirror `internal/loom/engine.go` `domainSpec` (~303–314) exactly, with `FilterSubject: "events.external.>"`.
- **`dispatch.go`** (new, or fold into engine.go) — `handleExternal(ctx, msg) substrate.Decision`: parse the envelope → (optional) tracker-skip → dispatch → publish replyOp → ack. The decision logic (§ Item C).
- **`actuator.go`** (new) — `buildReplyEnvelope` + the publish to `ops.<lane>`, mirroring `internal/loom/actuator.go` `opEnvelope`/`buildOutbox`/the `r.conn.Publish(ctx, "ops."+lane, data, nil)` call (~79–95). **Carry the bridge's own copy of the op envelope struct** (the same `{requestId, lane, operationType, actor, submittedAt, payload, authContext?}` shape) so the bridge imports no `internal/processor` (AC: module boundary; loom does the same, actuator.go ~15–28).
- **`health.go`** (new) — the Contract #5 heartbeater, modeled on `internal/loom/health.go` (~138–238) but with `Component: "bridge"`, key `health.bridge.<instance>`, and simpler metrics (no running-instance scan — the bridge has no instances; report e.g. `dispatched`/`skipped`/`adapterErrors` counters or just an empty metrics map + the consumer-state map). Reuse the loom `consumerStateCache` + `consumerState` pattern (or a trimmed copy) for the `metrics.consumers` map and the `ConsumerPaused` issue.
- **`doc.go`** (new) — package doc: the generic egress, substrate-only boundary, the three idempotency mechanisms, the type-agnostic stance. Describe **present** behavior (no history comment, no "moved from nudge" narration — git blame is the record, CLAUDE.md rule).

### Item B — the envelope (`external.<adapter>`)

Parse the fixed envelope (bridge.md ~55–65). Define a Go struct in the bridge package:

```go
type externalEvent struct {
    InstanceKey    string          `json:"instanceKey"`    // the opaque correlation token (13.2 mints a BARE handle — see § 3)
    Adapter        string          `json:"adapter"`        // which registered adapter
    Params         json.RawMessage `json:"params"`         // adapter inputs (free-form; Fake* ignore them)
    ReplyOp        string          `json:"replyOp"`        // the result-op type the bridge posts back
    IdempotencyKey string          `json:"idempotencyKey"` // = instanceKey (the adapter's dedup key)
    ExternalRef    string          `json:"externalRef"`    // = instanceKey (echoed on the reply op)
}
```

- **The event body is the Event envelope** (Contract #3 §3.4): top-level fields + a `payload` object. The fixture harness publishes the SAME shape the 13.2 `fakeProcessor` publishes (a top-level envelope with the business fields under `payload`). So the bridge unmarshals the **`payload`** object into `externalEvent` (mirror Loom's `eventBody`/`Payload` split, engine.go ~653–658). **Confirm the exact nesting against the 13.2 fixture** — the `external.*` event is emitted by the instanceOp's outbox as a normal business event, so its body is `{requestId, …, payload:{instanceKey, adapter, params, replyOp, idempotencyKey, externalRef}}`. Read `internal/loom/loom_e2e_test.go` `fakeProcessor` (~93–287) for the exact published shape, and `internal/loom/external_e2e_test.go` for how 13.2's instanceOp fixture shaped the `external.*` event it emitted — **your bridge harness should publish the identical shape**, so the two stories' fixtures agree (the cross-story seam, Q1).
- **`instanceKey`/`externalRef`/`idempotencyKey` are opaque** — the bridge treats them as a single correlation token (bridge.md ~67–70). It NEVER parses the type segment, NEVER assumes `vtx.<type>.<id>` vs a bare handle. Per the 13.2 §0 resolution (§ 3 below), the value is in fact a **bare NanoID handle** — but the bridge does not care and must not depend on the shape.

### Item C — the dispatch decision (`handleExternal`)

The flow per event (bridge.md ~84–96 + the failure-modes table ~204–213):

1. **Empty body** → `Ack` (nothing to do). **Unparseable envelope** → **`errConfig` posture: `Ack` + a Health issue** (redelivery can never fix malformed JSON; never a silent skip, never `Term`-and-forget without surfacing — bridge.md ~179, ~212). Log + raise the issue.
2. **Compute `replyReqID = deterministic(instanceKey)`** (§ 3). This is the result-op requestId AND the optional skip-probe key.
3. **(Optional) skip-on-redelivery** — GET `vtx.op.<replyReqID>` from Core KV (mechanism #3). **Present and not tombstoned** → the result already landed → **`Ack`** without re-calling the adapter (avoids a redundant round-trip mechanism #2 would dedup anyway). **Tombstoned (`isDeleted:true`) → treat as not-found and proceed** (Contract #4 §4.3 — a present-but-tombstoned tracker is an operator retry signal, exactly as Loom's `trackerExists`/the nudge `resolveProbe` handle it; do NOT skip on a tombstone). `ErrKeyNotFound` → proceed. **A probe *error* (not not-found)** → do **not** silently skip; `NakWithDelay` (transient KV failure → redeliver) — the probe is an optimization, so failing it must fall back to the real call, not drop the event. Mirror `internal/loom/engine.go` `trackerExists` (~1277–1286) for the GET + not-found handling; add the tombstone check (read the nudge `resolveProbe`, engine.go ~532–550, for the found+`isDeleted:false` rule).
4. **Look up the adapter** — `registry.Lookup(ev.Adapter)`. **`ok == false`** → **`errConfig`: `Ack` + a Health issue** (the registry lacks the name; redelivery can't fix it — bridge.md ~208; mirror the retired nudge's `NudgeAdapterMissing`). **Never a silent skip, never a hot Nak loop.**
5. **Dispatch** — `adapter.Execute(ctx, Request{IdempotencyKey: ev.InstanceKey, …})` (§ 0.3 mapping). **`Execute` returns an error** → a (possibly transient) adapter failure → **`NakWithDelay`** (bounded-cadence redelivery on the same `idempotencyKey`; the adapter dedups, so a re-attempt is safe — bridge.md ~209). Raise a Health issue (transient adapter failure). **Never inline-retry; never hot-loop.** **Adapter panics** → must be **contained** by the framework, the dispatch goroutine survives, the event is re-drivable (bridge.md ~210) — the `supervisedHandler`/supervisor pattern already contains panics on the consumer goroutine; confirm and rely on it (the nudge `execute()` has a `recover()` at protocol.go ~197–208 — you MAY add the same belt-and-suspenders `recover()` in `handleExternal` around the `Execute` call, returning `NakWithDelay` on a recovered panic, and note it).
6. **Build + publish the replyOp** — `payload = { externalRef: ev.InstanceKey, <outcome fields from Result> }`; `env = { requestId: replyReqID, lane: cfg.Lane, operationType: ev.ReplyOp, actor: cfg.ActorKey, submittedAt: now, payload, authContext? }`; `conn.Publish(ctx, "ops."+cfg.Lane, data, nil)`. **Publish fails** → **`NakWithDelay`** (redeliver; the deterministic `replyReqID` makes re-publish idempotent — it collapses on the Contract #4 tracker — bridge.md ~199). **Publish succeeds** → **`Ack`**. Log `bridge replyOp posted` (instanceKey, adapter, replyOp, requestId).
   - **`authContext.target`** — decide and justify. The replyOp is a normal op the bridge submits under its service actor. The real `replyOp` (14.4) will record the outcome on the claim vertex `vtx.<type>.<instanceKey>` — its auth target is plausibly that claim vertex. But the bridge is **type-agnostic** and the claim-vertex *type* is not in the envelope. Options: (i) omit `authContext` entirely (the bridge actor is root-equivalent — operator `scope:"any"` — so it needs no narrow target to be authorized; Loom's relay omits authContext for systemOps unless a target is set, actuator.go ~87–89); (ii) set `target` to the opaque `instanceKey` (but that is a bare handle, not a full key — and the bridge must not synthesize a typed key). **Recommend (i): omit `authContext`** — the root-equivalent bridge actor authorizes via its operator grant regardless of target, and synthesizing a target would either leak a type assumption (violating type-agnosticism) or pass a non-key value. Document the choice; flag it in Open Questions (Q2) for the 14.4 author (if the real replyOp needs a narrow `authContext.target`, 14.4 supplies it from the DDL, not the bridge).

### Item D — `cmd/bridge/main.go`

Mirror `cmd/loom/main.go` (~36–114) / `cmd/weaver/main.go`:
- `bootstrap.Load(bootstrapJSONPath)` (strict loader — an absent/invalid file is fatal; never mint a fresh unrecognized identity).
- `actorKey := bootstrap.BridgeIdentityKey`.
- `substrate.Connect(...)` with `Name: "lattice-bridge:" + instance`.
- `bridge.NewEngine(conn, bridge.Config{ CoreKVBucket: bootstrap.CoreKVBucket, EventsStream: bootstrap.CoreEventsStreamName, HealthKVBucket: bootstrap.HealthKVBucket, ActorKey: actorKey, Instance: instance, Lane: lane, Logger: logger })`.
- **Register the reference adapters** (the demo set, mirroring `cmd/weaver/main.go` ~111–113 but via the `bridge` package): `engine.RegisterAdapter("stripe", bridge.NewFakeStripe())`, `engine.RegisterAdapter("backgroundCheck", bridge.NewFakeBackgroundCheck())`. (The engine exposes `RegisterAdapter` over its `*bridge.Registry`, mirroring Weaver's `RegisterAdapter`.) **Must run before `Start`** (the registry has no lock-step with dispatch — same rule as Weaver, engine.go ~499–501).
- SIGINT/SIGTERM graceful shutdown; `engine.Start(ctx)` blocks until ctx done.
- **Env:** `NATS_URL`, `BOOTSTRAP_JSON_PATH`, `BRIDGE_INSTANCE` (default `bridge-<NanoID>`), `BRIDGE_LANE` (default `system` — the bridge posts result-ops on the system lane like Loom; the `system`-lane carry is the deferred note from 13.3/service-actors.md — confirm `system` is the right default and note it). Document each in the package doc.

### The outbox-or-not decision (REQUIRED — resolve + justify)

**Decision: NO durable outbox. A direct `conn.Publish` + the deterministic `requestId` + at-least-once redelivery is sufficient.** Justification (this is a first-class design call the reviewer will probe):

- Loom needs an outbox because its op-submission is **coupled to a state transition** (the `token`/`instance`/`outbox`/`deadline` must persist atomically *before* the side effect, Crash-safety invariant 1, §10.6 ~679–688) — the outbox decouples "persist my cursor advance" from "publish the op" so they aren't a dual write. **The bridge has no cursor and no state to advance** — there is nothing to persist atomically alongside the publish. Its only durable state is (a) the `events.external.>` consumer ack floor and (b) the Contract #4 tracker for its deterministic replyReqID (bridge.md ~183–191). So there is no dual-write to break.
- Crash-safety holds via redelivery + determinism alone (bridge.md ~193–201): crash before the call → redeliver → call proceeds (adapter dedups); crash after the call before the op → redeliver → adapter dedups the re-call → op posts; crash after publish before ack → redeliver → the deterministic `requestId` collapses on the Contract #4 tracker → exactly one mutation. **The ack is the commit point**; an un-acked event is simply redelivered. No outbox needed.
- **If** you find a case where a publish must be atomic with a KV write the bridge owns (it should not — the bridge writes no Core KV, P2), halt and reconsider. The design of record (bridge.md ~185 "holds no durable bucket of its own") is explicit: **the absence of an outbox is the deliberate simplification over the retired Two-Phase Nudge.** Build to it.

### Process launch (REQUIRED — investigated, stated)

`cmd/bridge` is a long-running consumer like `cmd/loom`/`cmd/weaver`. **Investigated:** `docker-compose.yml` has **no** service for loom/weaver/processor/bridge (it provisions NATS + Postgres only), and `make up` launches only `refractor` + `processor` in the background (Makefile ~36–40) — **loom and weaver are NOT launched by `make up`** (they are started ad-hoc / in tests today). **Therefore 13.4 does NOT add `cmd/bridge` to `make up` or a compose service** — it follows the loom/weaver precedent. The bridge binary must **build** (`go build ./cmd/bridge`) and run standalone, but wiring it into the live stack (so a real `externalTask` drives it end-to-end) is a **14.5** concern (the end-to-end e2e). State this in the summary; do not expand scope to orchestrate the process. (You MAY add a `go build -o bin/bridge ./cmd/bridge` line is **not** required; confirm `go build ./...` covers it.)

---

## 3. `deterministic(instanceKey)` — the bridge's own derivation (REQUIRED — specify exactly)

**The result-op `requestId` MUST be `deterministic(idempotencyKey = instanceKey)`** (the pinned FR58 invariant, §10.3 ~331–336 / bridge.md mechanism #1 ~109–114). A redelivered `external.*` event must yield the **same** `replyReqID` so the re-submitted replyOp collapses on the Contract #4 `vtx.op.<replyReqID>` tracker → exactly one result mutation.

**Derivation (BINDING):** a **stable, namespaced hash over the opaque `instanceKey`**, producing a bare NanoID over the canonical Lattice alphabet (Contract #1) — modeled on `internal/loom/token.go` `deriveID` (~57–75) but keyed on a **string** (the `instanceKey`), not `(instanceID, cursor)`. Concretely, add a `bridge`-package function, e.g.:

```go
// deriveReplyRequestID returns the deterministic result-op requestId for an
// external call, derived solely from the opaque instanceKey so a redelivered
// external.* event yields the SAME requestId → the re-submitted replyOp
// collapses on the Contract #4 vtx.op.<requestId> tracker (exactly one result
// mutation; the pinned FR58 invariant, Contract #10 §10.3). The instanceKey is
// treated as an opaque token — the type segment, if any, is never parsed.
func deriveReplyRequestID(instanceKey string) string { … }  // sha256("bridge:reply:"+instanceKey) → NanoID over substrate.Alphabet
```

- **Use a distinct namespace prefix** (e.g. `"bridge:reply:"`) so the bridge's requestId can never collide with a Loom-derived id for any value. Output length = `substrate.NanoIDLength` over `substrate.Alphabet` (so it is a valid op `requestId`, bare/dot-free).
- **This is the bridge's OWN derivation — Loom does NOT share it.** Loom parks on `token.<instanceKey>` (the bare handle it minted, 13.2 §0); the bridge **echoes** that handle back as `payload.externalRef` and **independently** computes its result-op requestId from it. They are different ids serving different roles: Loom's park token = the handle; the bridge's result-op requestId = `deriveReplyRequestID(handle)`. Loom never computes the bridge's requestId, and the bridge never parks on a Loom token. (This matches §10.6 ~553–555: "the externalTask's write-ahead handle … does not own the bridge's later result-op requestId, so it parks on a handle it controls and the bridge echoes it back.")
- **Determinism, not a stored map.** Do NOT memoize requestIds in a bridge-side map — the derivation is pure over the input, so a fresh bridge replica (or a restart) computes the identical id from the same `instanceKey`. That is what makes redelivery-after-crash collapse correctly without shared state.
- **Why a hash over the bare handle is correct even though §10.6 line 544 says `instanceKey` is "the full `vtx.<type>.<id>` key":** the 13.2 §0 bare-handle resolution (DONE, shipped) established that the value Loom mints/parks-on/echoes is the **bare NanoID handle**, not the full key — the bridge consumes whatever opaque token it receives. The derivation is correct for either shape (it hashes the token verbatim). The cross-story seam is recorded in Q1; confirm the harness's fixture `external.*` event carries the **bare handle** as `instanceKey`/`idempotencyKey`/`externalRef` (matching what 13.2's `external_e2e_test.go` emits), and the bridge keys `deriveReplyRequestID` off that bare handle.

---

## 4. Vertex-type-agnostic, PROVEN not asserted (invariant a — the headline AC)

The bridge MUST be type-agnostic, and the FR58 harness MUST **prove** it by using a **non-`service`** fixture claim type — exactly as 13.2 proved its engine with `vtx.widget.<handle>`.

- The bridge **never** parses the `instanceKey`/`externalRef`/`idempotencyKey` type segment, **never** assumes `vtx.<type>.<id>` shape, **never** reads a typed claim vertex. The only Core KV read it makes is the **generic** op-tracker GET (`vtx.op.<replyReqID>`, mechanism #3) — the same key shape for every op, **not** a typed-vertex read (AC #3; bridge.md ~119–124, ~176).
- **The proof:** the FR58 harness publishes `external.*` events whose `instanceKey`/`externalRef`/`idempotencyKey` are a **non-`service` claim token** — use the bare handle for a non-`service` type, e.g. the token that would form `vtx.widget.<handle>` (consistent with 13.2). Because the bridge treats the token opaquely, the test passes **regardless** of what type it represents; if anyone later makes the bridge parse/assume a type, the non-`service` fixture breaks. **Do not** add a `service`-typed fixture — the non-`service` choice IS the proof (Andrew: "proven by a non-`service` fixture, not asserted").
- Note: since the bridge in 13.4 only **posts** the replyOp (the claim vertex is the DDL's, 14.4), "type-agnostic" here means the **derivation + the echo + the optional tracker probe** are all type-blind. Make the harness assert the bridge posted the replyOp with `requestId = deriveReplyRequestID(<bare handle>)` and `payload.externalRef = <bare handle>` for a non-`service` token, and that the optional skip-probe used `vtx.op.<replyReqID>` (generic), never `vtx.<type>.<…>`.

---

## 5. The FR58 crash/retry proof — bridge-only harness (AC #4 — first-class test plan)

This is the centerpiece. A **bridge-only** harness (no Loom, no real Processor — fixtures only), embedded NATS, modeled on `internal/loom/loom_e2e_test.go` (`startNATS` ~25–34, `provision` ~44–68, the `fakeProcessor` ~93–287). Build it in `internal/bridge/` (package `bridge_test`).

### Harness shape

- **Embedded NATS** (`startNATS`) + `provision`: create the `core-events` stream (subjects `events.>`, `AllowAtomicPublish`), the `core-operations` stream (subjects `ops.>`), and the `core-kv` bucket (for the optional tracker probe + the fixture replyOp's tracker write). Copy the loom `provision` verbatim (drop `loom-state`).
- **A fixture "Processor"** — a minimal consumer on `ops.<lane>` (the lane the bridge publishes to) that models **only** what the bridge depends on (the Contract #4 dedup): on a replyOp envelope, GET-or-create `vtx.op.<requestId>`; **first time** → write the tracker, count one `resultMutations`; **repeat requestId** → tracker present → **no second mutation** (the exactly-once guarantee). This mirrors the loom `fakeProcessor`'s systemOp branch (~writes `vtx.op.<requestId>`, increments `submitted` only on non-duplicate). It is the standby that makes mechanism #1 observable.
- **Adapter:** `bridge.NewFakeStripe()` registered as `"stripe"` (and/or `backgroundCheck`). The harness drives `FailUntil(n)` to inject failures.
- **Event publisher:** publishes fixture `external.stripe` events with body `{requestId:<any>, …, payload:{instanceKey:<bare handle for a non-service type>, adapter:"stripe", params:{…}, replyOp:"ResolveCharge", idempotencyKey:<bare handle>, externalRef:<bare handle>}}` — the **identical shape** 13.2's instanceOp fixture emits (Q1 seam). Use a **non-`service`** token (invariant a).

### The concrete test cases (count delivered tests from the diff)

1. **`TestBridge_HappyPath_PostsDeterministicReplyOp`** — publish one `external.stripe` event; assert (a) `FakeStripe.SideEffects(instanceKey) == 1` (exactly one real charge), (b) the fixture Processor saw a replyOp with `requestId == deriveReplyRequestID(instanceKey)` and `payload.externalRef == instanceKey`, (c) `resultMutations == 1`. **Type-agnostic (invariant a):** the `instanceKey` is a **non-`service`** token.
2. **`TestBridge_EventRedelivery_AtMostOneSideEffect`** — publish the **same** `external.stripe` event **twice** (simulate at-least-once redelivery — either re-publish, or use a consumer that Naks the first delivery then the supervisor redelivers). Assert `SideEffects == 1` and `resultMutations == 1` (the deterministic requestId collapsed the second replyOp on the Contract #4 tracker). **This is the redelivery half of AC #4.** Run it **both** with the optional skip-probe enabled and (if feasible to toggle) disabled — proving correctness holds via mechanisms #1+#2 **without** #3 (the skip is only an optimization).
3. **`TestBridge_FailUntilThenRecovers_ExactlyOneSideEffect`** — `FakeStripe.FailUntil(N)` (first N Execute calls error, no side-effect); publish the event and let redelivery re-drive it (NakWithDelay → the supervisor redelivers) until the (N+1)th attempt succeeds. Assert `SideEffects == 1` (only the eventual success charged), `resultMutations == 1`, and that a **Health issue** was raised on the transient-failure attempts (the `errConfig`-vs-transient distinction: transient adapter failure ⇒ Nak + Health issue, NOT errConfig). **This is the mid-flight-failure-recovery half of AC #4** — the literal FR58 "a charge that errored did not charge, and the retry converges to exactly one side-effect."
4. **`TestBridge_UnregisteredAdapter_AckAndHealth`** — publish an `external.<unknownAdapter>` event; assert the bridge **Ack**s it (no redelivery loop) AND raised a Health issue (the `errConfig` path — `Lookup` miss; bridge.md ~208), and **`SideEffects` for every registered adapter stays 0** (never a silent dispatch, never a hot Nak loop).
5. **`TestBridge_UnparseableEnvelope_AckAndHealth`** — publish a malformed `external.*` body; assert **Ack** + Health issue (redelivery can't fix malformed JSON; never `Term`-and-forget-silently).
6. **`TestBridge_SkipOnRedelivery_TrackerPresent`** (mechanism #3) — pre-seed `vtx.op.<deriveReplyRequestID(instanceKey)>` (not tombstoned) in core-kv; publish the event; assert the bridge **Ack**ed WITHOUT calling the adapter (`SideEffects == 0`) — the optional skip fired. Then a **tombstoned** variant: pre-seed `vtx.op.<…>` with `isDeleted:true`; assert the bridge **did** dispatch (`SideEffects == 1`) — a tombstone is NOT a landed result (Contract #4 §4.3; mirrors the nudge `resolveProbe` rule).
7. **`TestBridge_PublishFailure_Nak`** (optional, if cheaply injectable) — make the ops publish fail (e.g. delete the `core-operations` stream mid-test or point the lane at a non-existent stream) and assert the bridge `NakWithDelay`s (does not Ack-and-drop), so the replyOp is retried.

### Plus the moved adapter tests (they come with the move)

- `fake_stripe_test.go` + `adapter_test.go` (moved to `internal/bridge/`, package `bridge_test`) — the literal FakeStripe idempotency proof + the registry register/lookup tests. They pass unchanged (modulo `nudge.`→`bridge.`).
- `boundary_test.go` (moved) — assert `internal/bridge` imports only `substrate` (no `internal/processor`/`internal/loom`/`internal/refractor`/raw `nats.go`). **Update the `go list -deps` target** to `…/internal/bridge` and add `internal/weaver`/`internal/weaver/nudge` to the forbidden list (the bridge must not import back into Weaver — that would create the cycle § 0.2 forbids).

---

## 6. P2 / module boundary / Gate 2 (BLOCKED)

- **The bridge writes NOTHING to Core KV directly** (P2 — the Processor is the sole Core KV writer / event producer; bridge.md ~218–221). The result mutation is the **Processor's**, applied when the replyOp commits. The bridge only **submits** the replyOp (a `conn.Publish` to `ops.<lane>`) and only **reads** Core KV (the optional generic tracker GET). **No `KVPut`/`KVDelete`/atomic batch to `core-kv` anywhere in `internal/bridge`** except the **Health heartbeat** which writes `health-kv` (a different bucket — the Health-KV plane, not Core KV; Loom does the same). Make this explicit in the code + doc.
- **`internal/bridge` imports ONLY `internal/substrate`** (+ stdlib). NOT `internal/processor`, NOT `internal/loom`, NOT `internal/weaver`, NOT raw `nats.go`/`jetstream`. The moved `boundary_test.go` is the gate.
- **Gate 2 (`make test-bypass`, BLOCKED)** — the bypass suite proves no component writes Core KV outside the Processor. The bridge introduces a new component that submits ops; Gate 2 must **stay green** (all BLOCKED). The bridge's only Core KV touch is a **read** (the tracker GET), which is allowed; its only **write** is to `health-kv` via the heartbeat (allowed). Run `make test-bypass` and confirm BLOCKED. (If the bypass suite enumerates components/actors, confirm the bridge actor — root-equivalent — does not open a bypass; it submits through `ops.<lane>` like every other actor, so it should be inert to the bypass assertions. Note the result.)

---

## 7. Health (Contract #5)

- The bridge writes a **heartbeat** to `health.bridge.<instance>` on the Contract #5 cadence (default 10s, NFR-O1 floor), modeled on `internal/loom/health.go` (~138–238) with `Component: "bridge"`. Metrics: the `consumers` state map (from the supervisor's HealthSink → a `consumerStateCache`, reuse the loom pattern) + optionally dispatch counters (`dispatched`/`skipped`/`adapterErrors`). Emit `starting` on boot, `healthy` on tick, `shutdown` on ctx cancel (mirror loom `heartbeater.run` ~173–188).
- **Issue surfacing:** the `errConfig` paths (unregistered adapter, unparseable envelope) and the transient-adapter-failure path raise **Health issues** (Contract #5 §5.2 issue entries) — surfaced on the heartbeat's `Issues[]` and/or logged at warn/error. Reuse the loom `healthIssue` shape. An unregistered adapter ⇒ `severity:"error"`, code e.g. `BridgeAdapterMissing`; a transient adapter failure ⇒ `severity:"warning"`, code e.g. `BridgeAdapterFailed`; an unparseable envelope ⇒ `severity:"error"`, code e.g. `BridgeEventUnparseable`. The FR58 harness (test 3/4/5) asserts the issue is raised — **never a silent skip** is an AC.
- **Decide how issues reach the heartbeat:** Loom's heartbeater derives issues from consumer pause-state (health.go ~203–212). The bridge's dispatch-time issues (adapter missing/failed) are transient/per-event. Simplest correct approach: an `issueCache` (a small mutex-guarded map keyed by adapter name / issue code, set on the error path, cleared on a subsequent success) that the heartbeater snapshots into `Issues[]` — mirror Weaver's `issueCache` (`internal/weaver`, referenced by `e.issues.set/clear/snapshot`). Reuse or trim that pattern. Document the lifecycle (when an issue clears).

---

## 8. Verification gates (run before handing back; record each + result in the closing summary)

- `go build ./...` — **must include `./cmd/bridge` and the re-pointed `internal/weaver`/`cmd/weaver`** (the move's blast radius). A build break here means the coexistence move is wrong.
- `make vet`
- `golangci-lint run ./...`
- `make verify-kernel` — the bridge makes **no** kernel-topology change (13.3 did), but run it to prove no regression (the stack must come up; requires `make up`).
- **`make test-bypass` (Gate 2, BLOCKED)** — P2 proof; the bridge writes no Core KV directly. Must show all BLOCKED.
- **`make test-capability-adversarial` (Gate 3, DEFENDED)** — **this is the proof the coexisting nudge path is intact** after the move (it brings the full stack up and runs the capability-adversarial / convergence suite; a broken Weaver nudge wiring shows up here). Must show convergence DEFENDED.
- `go test ./internal/bridge/... ./internal/weaver/... -count=1` — the new bridge suite (incl. the FR58 bridge-only proof) **and** the live Weaver suite (incl. `nudge_dispatch_internal_test.go`, the live FR58 **nudge** proof — the unit-level coexistence gate). Both must pass.
- The full **3-layer adversarial review** is Winston's gate (Blind Hunter / Edge Case Hunter / Acceptance Auditor) per `bmad-code-review` — this is the largest/highest-risk story in the epic (net-new engine + security-plane result-ops + a cross-package move), so the full 3-layer is **mandatory** (not a lead-only review). Note it in your summary.

Flake retry per Deviation 14 is allowed; a flake claim without a re-run is a drift signal. The `internal/bridge` + `internal/weaver` unit/e2e packages use embedded NATS; only `make verify-kernel` + Gate 2 + Gate 3 need the Docker stack.

If you judge the story too large for one safe pass, halt and report a split proposal — a natural seam is **13.4a = the move + re-point + the moved tests green (build + Gate 3 + weaver tests prove coexistence)** and **13.4b = the bridge runtime + cmd/bridge + the FR58 bridge-only proof**. But land 13.4a whole if you start it (a half-done move leaves the tree un-buildable, which is strictly worse). Prefer a single pass if feasible; the two halves are independent enough that 13.4a is a safe standalone landing if 13.4b proves too large.

---

## 9. Required reading (DS does the deep reads; do not expect them pre-loaded)

- **THE SPEC:** `docs/components/bridge.md` IN FULL (every section — see the header grounding above). This page **is** the design; your code matches it.
- **FROZEN:** `docs/contracts/10-orchestration-surfaces.md` §10.3 (~303–337, the `weaver-claims` retirement + the pinned FR58-determinism invariant) + §10.6 (~526–555, the externalTask correlation row + `payload.externalRef` — the seam you feed). Contract #4 (`vtx.op.<requestId>` dedup; `internal/processor/step2_dedup.go` + §4.3 the tombstone-is-retry rule). Contract #1 §1.1 (key shapes). Contract #5 (Health). Contract #2 §2.1 (the `ops.<lane>` op envelope shape).
- **The 13.2 bare-handle seam (CRITICAL — the cross-story correlation token):** `_bmad-output/implementation-artifacts/story-13.2-loom-external-task.md` §0 (~14–51) + Q1 (~238) — `instanceKey`/`externalRef` is the **bare NanoID handle** Loom mints (NOT a full `vtx.<type>.<id>`). The bridge consumes it as the opaque token. Confirm 13.4 keys `idempotencyKey`/`externalRef`/`requestId=deriveReplyRequestID(instanceKey)` off the **bare handle**. Read `internal/loom/external_e2e_test.go` to see the exact `external.*` event shape 13.2's instanceOp fixture emits — your bridge harness publishes the identical shape.
- **The 13.3 service-actor seam:** `_bmad-output/implementation-artifacts/story-13.3-bridge-service-actor.md` Q4 (~250) — `cmd/bridge/main.go` reads `bootstrap.BridgeIdentityKey` as its `actorKey` (exactly as loom/weaver read theirs); the `system`-lane carry is deferred. `docs/components/service-actors.md` — the bridge's deferral note.
- **What MOVES vs STAYS (read all):** `internal/weaver/nudge/adapter.go` (the `Adapter`/`Request`/`Result`/`AdapterFunc`/`Registry` — MOVE), `fake_stripe.go` (`FakeStripe` w/ `FailUntil`/`FailNext`/`SideEffects`/idempotencyKey dedup — MOVE), `fake_background_check.go` (MOVE), `adapter_test.go`/`fake_stripe_test.go`/`boundary_test.go` (MOVE). `claims.go`/`protocol.go`/`doc.go` (the Two-Phase Nudge protocol + `weaver-claims` — **STAY**; retired only in 13.5). `protocol_test.go`/`nudge_dispatch_internal_test.go` (STAY — the live nudge proofs).
- **The coexistence wiring (THE headline risk — investigate with the grep in § "Coexistence grep"):** `internal/weaver/engine.go` (`adapters *nudge.Registry` ~206, `NewRegistry` ~292, `RegisterAdapter(name, nudge.Adapter)` ~502, `resolveFunc → nudge.Result` ~518), `internal/weaver/evaluator.go` (`nudge.Dispatch` ~293, `fireNudge`/`recoverNudge`/`nudgeDecision` — these reference `nudge.Claim`/`nudge.State*`/`nudge.ErrAdapterNotFound`, all STAYING types), `internal/weaver/reconciler.go` (`reclaimNudge`/`recoverNudge`), `cmd/weaver/main.go` (`nudge.NewFakeStripe`/`NewFakeBackgroundCheck` ~111–113). The move must NOT break the live nudge path: `go build ./...` + Gate 3 (DEFENDED) + `go test ./internal/weaver/...` must stay green.
- **The `cmd/bridge` template + substrate-only consumer/publish patterns:** `cmd/loom/main.go` IN FULL (~36–114 — service-actor binary: pins `ActorKey = bootstrap.LoomIdentityKey`, `substrate.Connect`, `engine.NewEngine`, SIGINT/SIGTERM, `engine.Start`) + `cmd/weaver/main.go` (the adapter-registration block ~95–119). `internal/loom/engine.go` `Start`/`NewEngine`/`domainSpec`/`supervisedHandler`/`healthSinkFor` (~211–314 — the supervised durable-consumer + heartbeater spine; the bridge mirrors this stripped of state). `internal/loom/actuator.go` `opEnvelope`/`buildOutbox`/the relay's `conn.Publish(ctx, "ops."+lane, …)` (~15–123 — the op-envelope publish; the bridge carries its own copy of the envelope struct to stay off `internal/processor`). `internal/loom/health.go` IN FULL (the Contract #5 heartbeat; `consumerStateCache`, `heartbeater`, `health.<component>.<instance>` key). `internal/loom/engine.go` `trackerExists` (~1277–1286 — the `vtx.op.<requestId>` GET the bridge's optional skip mirrors) + the nudge `resolveProbe` (`internal/weaver/engine.go` ~532–550 — the found+`isDeleted:false` tombstone rule).
- **Substrate surface (read the signatures):** `internal/substrate/consumer_supervisor.go` (`NewConsumerSupervisor`, `Add`, `Stop`, `Pause`/`Resume`), `internal/substrate/consumer_supervisor_spec.go` (`ConsumerSpec` fields), `internal/substrate/publish.go` (`Conn.Publish(ctx, subject, data, header)`), `internal/substrate/kv.go` (`Conn.KVGet`/`KVPut`), `internal/substrate/nanoid.go` (`NanoIDLength`, `Alphabet`, `NewNanoID`).
- **Test harness model:** `internal/loom/loom_e2e_test.go` IN FULL (`startNATS` ~25–34, `provision` ~44–68, the `fakeProcessor` ~93–287 — how a fixture Processor writes the Contract #4 tracker + models dedup + the exactly-once `submitted` counter + the `gate` for mid-flight control). `internal/loom/external_e2e_test.go` (13.2's externalTask fixture — the `external.*` event shape + the instanceOp/replyOp fixtures; your harness's event shape must match). `internal/weaver/nudge_dispatch_internal_test.go` (the live FR58 nudge proof you must keep green).
- **Gate targets:** `Makefile` `test-bypass` (~146–150), `test-capability-adversarial` (~160–164), `up`/`down`/`verify-kernel` (~20–64). `docker-compose.yml` (NATS + Postgres only — no loom/weaver/bridge service, confirming § 2 "Process launch").

### Coexistence grep (run it — it is the move's safety net)

Before AND after the move, run:
```
grep -rn -E "nudge\.(Adapter|Registry|Request|Result|AdapterFunc|NewRegistry|NewFakeStripe|NewFakeBackgroundCheck)" --include="*.go" .
```
This enumerates **every** site that references a **moving** type. After the move, **every** one outside `internal/bridge/` must reference `bridge.*` instead (and `internal/weaver/nudge/protocol.go`, `internal/weaver/engine.go`, `cmd/weaver/main.go` gain the `bridge` import). Then:
```
grep -rn -E "nudge\.(Nudger|ClaimStore|Claim|Dispatch|ResolveFunc|ResolveProbe|State[A-Z]|ErrClaim|ErrAdapter|NewNudger|NewClaimStore)" --include="*.go" .
```
This enumerates the **staying** types — they must STILL resolve in `nudge` (the protocol stays). A `bridge.Nudger` reference would be a wrong-direction error. Paste both outputs in the summary as the move's proof.

---

## 10. Closing summary (DS appends when done)

Deliverables vs § 1/§ 2 scope; the exact **MOVE manifest** (files moved with their new package, files staying, the resolved dependency direction `weaver/nudge → bridge` + `weaver → bridge` + `cmd/weaver → bridge`, removed in 13.5); the bridge runtime design as built (consumer → dispatch → publish; the outbox-or-not decision restated); the `deriveReplyRequestID` derivation (namespace + alphabet); the FR58 harness test list (count from the diff) + which `SideEffects == 1` cases prove redelivery vs mid-flight-recovery; the type-agnostic proof (the non-`service` fixture token used); exact files changed (`git status`); every gate + result (anything not run + why — esp. Gate 2/Gate 3 which need Docker); the two coexistence greps' output; any deviation; any new Open Question. **Confirm:** `internal/bridge` imports only `substrate`; the bridge writes no Core KV directly (only `ops.<lane>` publish + the generic tracker GET + the `health-kv` heartbeat); the live nudge path still builds + Gate 3 DEFENDED + `go test ./internal/weaver/...` green. **Do NOT commit.**

---

## Open Questions (saved for Winston / Andrew — none block the 13.4 build)

**Q1 — The cross-story `instanceKey`/`externalRef` value seam (13.2 ↔ 13.4 ↔ 14.4) = the BARE handle.** 13.2 (DONE) mints and parks on a **bare NanoID handle** and emits it as the caller-supplied `instanceKey` (NOT a full `vtx.<type>.<id>`); the bridge must echo **that same bare handle** back as `payload.externalRef` and key `deriveReplyRequestID` off it. `docs/components/bridge.md` (~58–63) still shows the envelope comment `instanceKey: "vtx.<type>.<id>"` and §10.6 line 544 calls it "the full `vtx.<type>.<id>` key" — read literally, that suggests the full key. **The 13.2 §0 resolution (bare handle) is the binding one** (it is the only reading consistent with the frozen step shape + the engine's type-blindness + the "caller-supplied id like taskId" instruction, and it is what 13.2 shipped). The bridge is **type-agnostic** (it hashes whatever opaque token it receives), so the bridge's correctness is **identical either way** — but the harness must use the **bare handle** to match 13.2, and 14.4's real instanceOp DDL must read the bare `instanceKey` and prepend its type. **Recommendation:** keep the bare-handle resolution; the bridge consumes the opaque token. (If the planning intent were the full key, that would require a 6th Loom step field — a frozen amendment — not done. Flagged, not changed.) **Action for the doc:** consider freshening bridge.md ~58–63 to say "opaque correlation token (a bare handle in the reference vertical)" so the envelope comment stops implying the full key — a `/docs` edit you MAY make, noting it.

**Q2 — `authContext.target` on the replyOp.** This story recommends the bridge **omit** `authContext` on the replyOp (the root-equivalent bridge service actor authorizes via its operator `scope:"any"` grant regardless of target; synthesizing a target would either leak a claim-vertex-type assumption — violating type-agnosticism — or pass a non-key bare handle). The real `replyOp` (14.4) records the outcome on the claim vertex; **if** the 14.4 DDL / §10.7 auth path needs a narrow `authContext.target` for the result mutation, 14.4 supplies it (the DDL knows the type), NOT the bridge. Confirm with the 14.4 author that omitting `authContext` from the bridge's submission is acceptable (the bridge is the submitter, not the DDL). Does not block 13.4 (the fixture Processor does not auth, mirroring 13.2's `fakeProcessor`).

**Q3 — `Request`/`Result` field shape (the verbatim-move tidy).** The moved `Request` is nudge-shaped: `{ IdempotencyKey, Operation, Subject, Params map[string]string }`; `Result` is `{ Detail string }`. The bridge populates `IdempotencyKey = instanceKey` (load-bearing) and maps the envelope's `params`/`adapter` onto the rest as convenient; the `Fake*` adapters read **only** `IdempotencyKey` (+ `Subject` for the Detail string), so the extra fields are inert. This story **moves them verbatim** (reshaping would ripple into the still-live nudge protocol — out of scope). A clean bridge-native adapter interface (e.g. `Request{ IdempotencyKey, Adapter, Params json.RawMessage }`) is a sensible **13.5-or-later** tidy once the nudge protocol is gone (13.5 deletes `nudge`, freeing `Request`/`Result` to be reshaped to the envelope). Recommendation: verbatim move now; reshape in/after 13.5. Flagged, not done.

**Q4 — Does `cmd/bridge` join the live stack now or at 14.5?** Investigated: neither `cmd/loom` nor `cmd/weaver` is launched by `make up` or has a `docker-compose.yml` service (the compose file is NATS + Postgres only; `make up` backgrounds only refractor + processor). So 13.4 does **not** add the bridge to `make up`/compose — it follows the loom/weaver precedent (build + run standalone; the end-to-end process orchestration that drives a real `externalTask` through the bridge is **14.5**). Recommendation: defer process-launch wiring to 14.5; 13.4 only ensures `go build ./cmd/bridge` succeeds and the binary runs standalone. Flagged so the 14.5 author owns the stack wiring (and may add loom + weaver + bridge to a compose/`make up` story-of-record then). Does not block 13.4.

**Q5 — The default `BRIDGE_LANE`.** This story defaults the bridge's result-op lane to `system` (mirroring `cmd/loom`'s `LOOM_LANE` default and the service-actor `system`-lane carry from 13.3/service-actors.md, which is *deferred* — lane enforcement is not yet live, Contract #2 §2.3). Confirm `system` is the intended lane for bridge result-ops (vs `default`). Today lane enforcement is deferred so the choice is cosmetic for routing; once it lands, the bridge's capability projection must include the `system` lane (the deferred carry). Does not block. Flagged for alignment with the lane-enforcement story.

---

## Dev Agent Record — closing summary (DS, 2026-06-18)

**No CONTRACT-AMENDMENT-REQUEST raised** (as the top-of-story flag anticipated). Every surface was satisfiable literally.

### Deliverables vs §1/§2 scope — all in-scope items built; no out-of-scope item touched.

### The MOVE manifest as executed (the headline §0)
- **Moved verbatim to `internal/bridge/` (package `bridge`)** via `git mv` (preserves history; appears as renames in `git status`): `adapter.go` (`Adapter`/`Request`/`Result`/`AdapterFunc`/`Registry`/`NewRegistry`/`Register`/`Lookup`), `fake_stripe.go` (`FakeStripe` + `FailUntil`/`FailNext`/`SideEffects`), `fake_background_check.go`, and the tests `adapter_test.go`, `fake_stripe_test.go`, `boundary_test.go` (→ `package bridge_test`). `Request`/`Result` were **NOT reshaped** (§0.3). Code/signatures/logic are unchanged; only the package clause changed, plus: error-string prefixes `nudge:`→`bridge:`, doc comments rewritten to present-tense generic-adapter framing (no `weaver-claims`/§10.8/Two-Phase-Nudge breadcrumbs — the moved code reads as if it always lived in `internal/bridge`), test import path + `nudge.`→`bridge.` qualifiers, and `boundary_test.go`'s `go list -deps` target → `internal/bridge` with the forbidden list extended to add `internal/weaver` + `internal/weaver/nudge` (and keep processor/loom/refractor).
- **Stayed in `internal/weaver/nudge/`**: `claims.go` (NO change — its `Adapter` field is a plain string), `protocol.go` (re-pointed `Adapter`/`Request`/`Result`/`Registry`→`bridge.*`; added the `bridge` import), `doc.go` (present-tense: the protocol dispatches through the bridge adapter contract), `protocol_test.go` + `nudge_dispatch_internal_test.go` (re-pointed moving-type qualifiers to `bridge.*`; added the `bridge` import; staying-type refs unchanged).

### Resolved dependency direction (temporary, removed wholesale in 13.5)
`internal/weaver/nudge → internal/bridge`, `internal/weaver → internal/bridge`, `cmd/weaver → internal/bridge` — **one direction only.** No import cycle: `internal/bridge`'s only Lattice dep is `internal/substrate` (proven by `go list -deps`). **Weaver boundary test:** `internal/weaver/boundary_test.go` forbids only `{processor, loom, refractor}` (NOT bridge), and its NoRawNATS check is direct-imports-only — so the `weaver→bridge` edge needed **no** test change. A present-tense comment at the `engine.go` import site explains the coupling (the bridge owns the adapter contract types).

### Bridge runtime as built (`internal/bridge/`, substrate-only)
- `engine.go` — `Engine`/`Config`/`NewEngine`/`Start`; one fixed durable `bridge-external` on `events.external.>` via `ConsumerSupervisor`; heartbeater; registry; `RegisterAdapter`. **No durable bucket of its own.**
- `dispatch.go` — `externalEvent` (payload struct; `instanceKey`/`externalRef`/`idempotencyKey` treated as one OPAQUE token), `eventBody` (envelope `payload` split), `handleExternal` decision: empty→Ack; unparseable/missing-adapter-or-instanceKey→errConfig (Ack+Health); optional skip-probe (present-not-tombstoned→Ack skip; tombstone/not-found→proceed; probe-error→NakWithDelay); `Lookup` miss→errConfig (Ack+Health); `Execute` error→NakWithDelay+Health (issue cleared on success); panic-contained→NakWithDelay; build+publish replyOp; publish-fail→NakWithDelay; success→Ack.
- `actuator.go` — the bridge's **own** `opEnvelope` struct (no `internal/processor` import); `submit` publishes `{requestId, lane, operationType, actor, submittedAt, payload}` to `ops.<lane>`. **`authContext` omitted** (Q2: root-equivalent actor authorizes regardless of target; the bridge is type-agnostic so it synthesizes no typed target — 14.4 supplies any narrow target).
- `token.go` — `deriveReplyRequestID(instanceKey) = sha256("bridge:reply:" + instanceKey)` expanded over `substrate.Alphabet` to `substrate.NanoIDLength`. Bridge-owned, pure (not memoized), distinct namespace from Loom's derivations.
- `health.go` — Contract #5 heartbeat `health.bridge.<instance>` (`Component:"bridge"`), `issueCache` + `consumerStateCache` + per-consumer `HealthSink` + `dispatchMetrics` (dispatched/skipped/adapterErrors). Issue codes: `BridgeAdapterMissing` (error), `BridgeAdapterFailed` (warning), `BridgeEventUnparseable` (error), `BridgeReplyPublishFailed` (warning).
- `doc.go` — present-tense package doc (generic egress, three idempotency mechanisms, type-agnostic, substrate-only, no outbox, P2).
- `cmd/bridge/main.go` — pins `ActorKey = bootstrap.BridgeIdentityKey`; registers `stripe`/`backgroundCheck` Fake* before `Start`; env `NATS_URL`/`BOOTSTRAP_JSON_PATH`/`BRIDGE_INSTANCE`/`BRIDGE_LANE` (default `system`); SIGINT/SIGTERM.

### Outbox-or-not decision (restated): **NO durable outbox.** The bridge persists no cursor/state to keep atomic with the publish, so there is no dual write to break. Crash-safety holds via at-least-once redelivery + the deterministic requestId (the re-published replyOp collapses on the Contract #4 tracker). The ack is the commit point.

### FR58 bridge-only proof (AC4) — PASSES
Harness in `package bridge_test` (`bridge_e2e_test.go` + `fr58_test.go`): embedded NATS, `provision` (core-events, core-operations, core-kv, health-kv), a fixture `fakeProcessor` on `ops.<lane>` that `trackOnce(vtx.op.<requestId>)`s (Contract #4 dedup; counts `resultMutations` on non-duplicate; writes the outcome aspect on the claim vertex as a 14.4-modeling bonus, root data minimal). All fixture `external.*` events use a **non-`service` (`widget`) bare-handle `instanceKey`** (invariant a — a hardcoded type would break every test). `export_test.go` exposes `DeriveReplyRequestID` to the external test package. **7 net-new bridge tests** (count from the diff; 18 total Test funcs in the package incl. 9 moved adapter/registry + 2 moved boundary):
1. `TestBridge_HappyPath_PostsDeterministicReplyOp` — `SideEffects==1`, replyOp `requestId == DeriveReplyRequestID(instanceKey)`, `payload.externalRef == instanceKey`, `resultMutations==1`.
2. `TestBridge_EventRedelivery_AtMostOneSideEffect` (**redelivery half**) — same event twice; `SideEffects==1` and `resultMutations==1`. Run **both** skip-probe ON and OFF; with OFF both events post a replyOp with the **identical** requestId (logged) → the fixture collapses the second → proves correctness via mechanisms #1+#2 **without** #3.
3. `TestBridge_FailUntilThenRecovers_ExactlyOneSideEffect` (**mid-flight recovery half**) — `FailUntil(2)`; NakWithDelay→redrive→3rd attempt succeeds; `SideEffects==1`, `resultMutations==1`, `BridgeAdapterFailed` Health issue raised (transient, NOT errConfig).
4. `TestBridge_UnregisteredAdapter_AckAndHealth` — Ack + `BridgeAdapterMissing`; zero dispatch.
5. `TestBridge_UnparseableEnvelope_AckAndHealth` — Ack + `BridgeEventUnparseable`; zero dispatch.
6. `TestBridge_SkipOnRedelivery_TrackerPresent` — present-not-tombstoned → skip (`SideEffects==0`); tombstoned (`isDeleted:true`) → dispatches (`SideEffects==1`), Contract #4 §4.3.
7. `TestBridge_PublishFailure_Nak` — ops stream deleted → publish fails → `BridgeReplyPublishFailed` Health issue + NakWithDelay (never Ack-and-drop).

**FR58 result: SideEffects==1 confirmed under BOTH redelivery (test 2) and mid-flight-failure recovery (test 3).** Bridge package passes with `-race`.

### Type-agnostic proof (invariant a): the non-`service` `widget` bare-handle token is used throughout; the bridge's derivation + echo + skip-probe are all type-blind (the skip-probe is the generic `vtx.op.<replyReqID>`, never a typed-vertex read). D5 is **not** asserted from the bridge (correctly — the bridge only posts the replyOp; the outcome-in-aspect is the 14.4 replyOp DDL's job; the harness's fixture replyOp models it as a bonus, mirroring 13.2).

### Gates (all FOREGROUND)
| Gate | Result |
|------|--------|
| `go build ./...` (incl. cmd/bridge + re-pointed weaver/cmd-weaver) | PASS |
| `make vet` | PASS |
| `golangci-lint run ./...` | PASS (0 issues; removed one unused `consumerStateCache.delete` the bridge never calls) |
| `make verify-kernel` | PASS — ALL ASSERTIONS PASSED (no kernel-topology regression; 13.3 already added the bridge identity) |
| **`make test-bypass` (Gate 2)** | **PASS — all bypass vectors BLOCKED** (the bridge opens no Core KV bypass; submits through `ops.<lane>`, only reads Core KV + writes health-kv) |
| **`make test-capability-adversarial` (Gate 3)** | **PASS — 6/6 cleared (5 DEFENDED, 1 ACCEPTED-WINDOW)** — proves the coexisting nudge path + full stack intact after the move |
| `go test ./internal/bridge/... ./internal/weaver/... -count=1` | PASS (bridge 17s incl. FR58; weaver 78s incl. the live `nudge_dispatch_internal_test.go` FR58 nudge proof; weaver/nudge incl. the staying protocol_test) |
| Coexistence grep 1 (moving types) | zero `nudge.*` moving-type refs anywhere |
| Coexistence grep 2 (staying types) | zero staying-type leaks to `bridge.*`; staying types still resolve in `nudge` |
| `internal/bridge` import boundary | only `internal/substrate` (go list -deps) |
| "moved from" breadcrumb scan | none |

### Confirmations: `internal/bridge` imports only `substrate`; the bridge writes **no** Core KV directly (only `ops.<lane>` publish + the generic tracker GET + the `health-kv` heartbeat); the live nudge path still builds, Gate 3 DEFENDED, and `go test ./internal/weaver/...` is green.

### Deviations
- **Doc comments in the moved files were rewritten** (not byte-verbatim) to describe present generic-adapter behavior, removing `weaver-claims`/§10.8/Two-Phase-Nudge framing that would be actively false in the bridge package. This honors both "change only the package clause" (the **code** — types/signatures/logic — is verbatim, §0.3 respected) and "no breadcrumbs / comments describe present behavior" (CLAUDE.md). Flagged for review.
- **Doc-true corrections forced by the move**, made in `docs/components/weaver.md` (the adapter-framework-location prose + the `internal/weaver/nudge/` path-table row now point at `internal/bridge`) and `docs/components/bridge.md` (build-status 13.3/13.4 marked done; the `internal/bridge/adapters/` table row corrected to the flat `internal/bridge/` location the story binds; the envelope comment freshened per Q1 to "opaque correlation token / bare handle"; header status + "Not yet built" note updated). `service-actors.md` needed no change.
- Added a `RedeliveryDelay` knob to `bridge.Config` (defaults to the substrate default; tests shorten it for fast NakWithDelay redelivery) — a legitimate production config field mirroring loom's spec.

### New Open Questions: none beyond Q1–Q5 (all recorded above, none block). Q1 (bare-handle seam) reaffirmed and the bridge.md envelope comment freshened accordingly.

**Review: full 3-layer adversarial (Blind Hunter / Edge Case Hunter / Acceptance Auditor) is mandatory for this story** (net-new engine + security-plane result-ops + cross-package move) — Winston's gate. **Not committed.**
