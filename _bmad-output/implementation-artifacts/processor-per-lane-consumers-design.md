# Processor per-lane consumers (ConsumerSupervisor adoption) — design

**Status: ✅ Andrew-ratified (2026-06-28). 🏗️ Fire 1 SHIPPED (c16f739, CI green).** Fire 1 = the substrate reply seam + commit-path disposition refactor onto a `ConsumerSupervisor` at behavior parity (single all-lanes spec, no lane split, no migration). One sound scoping deviation from §4.2: no `Classify`/`Probe`/infra-pause in Fire 1 (mirrors Loom/Weaver; infra-pause without a Probe would be a permanent-stall regression — deferred to the lane split). **Remaining: Fire 2** (four-spec lane split + real per-lane `lane_lag` + `meta` serial pin + `processor-main` migration R4) — run the pre-build §5.2 adversarial pass on the meta-lane/DDL-cache boundary to **prove the latent today-race** (single concurrent consumer ⇒ meta-DDL not serial) so Fire 2/3 demonstrably closes it — **→ Fire 3** (per-lane concurrency from config) **→ Fire 4** (control plane, its own row).
**Component:** Core (Processor) · **Imp:** ★★ · **Size:** M · **Owner role:** Lattice Steward (builds once ratified)
**Backlog row:** `planning-artifacts/backlog/lattice.md` → Component maintenance → *[Core] Processor per-lane consumers*

---

## For Andrew (one-look ratification)

**What it does (two lines).** The Processor today runs **one** durable consumer (`processor-main`) over all
`ops.*` lanes — a Phase-1 simplification. This adopts the architecture's design-of-record: **one
`substrate.ConsumerSupervisor` consumer per lane** (`default` / `urgent` / `system` / `meta`), so the four
`lane_lag.*` keys carry real per-lane backlog, the `meta` lane is serialized in its own pump (Contract #2 §2.3),
lanes drain on independent goroutines (urgent no longer queues behind a default backlog), and the sole Core-KV
writer finally uses the same supervised-pump pattern as Loom/Weaver/Refractor.

**No frozen-contract change.** Contract #2 §2.3/§3.7 *already* specify per-lane consumers ("Each Processor
instance subscribes to one or more lane subjects"; "`meta` lane consumer is configured with `MaxAckPending=1`"),
and Contract #5 §5.4 *already* reserves the per-lane `lane_lag` keys for exactly this adoption. We build **to**
the contracts; we change none of them. (`docs/contracts/02-operation-envelope.md` is currently shown modified in
the working tree — that is a **different fire's** `kv.Links` §2.5.1 edit (op-time bounded link enumeration),
unrelated to this design; I did not touch it.)

**No architectural fork.** Two design decisions are surfaced for your awareness, neither a fork:

1. **A small substrate-API seam, not a contract change.** The supervisor was built for *fire-and-forget* event /
   CDC consumers (Loom/Weaver/Refractor) — none of which reply to a client. The Processor is the **first
   request-reply consumer** to adopt it, so its handler needs the caller's reply inbox, which today's
   `substrate.Message` does not carry. Fire 1 adds `ReplySubject` + a header accessor to `substrate.Message`
   (plain strings — the supervisor's exported surface stays jetstream-free). This is internal Go API, not a
   `docs/contracts/*` change. (§4.1)
2. **Per-lane pumps are sequential; that is the design-of-record sufficiency claim, not a regression.** Each
   supervised pump processes its lane in stream order, one op at a time. `lattice-architecture.md:1031` already
   states "single consumer per lane is likely sufficient at MVP; parallelism is a Phase 2 concern." Intra-lane
   concurrency (the `urgent: consumers: 4` config) is the **Fire 3** scaling lever (queue-group fan-out),
   deferred-friendly. I call this out only so the throughput shift from today's concurrent single `Consume` is
   on the record. (§6, §7-R3)

**Recommended pre-build pass:** a focused adversarial review of the **meta-lane / DDL-cache concurrency boundary**
(§5.2) before Fire 2 — it is the one place the lane split changes a global-ordering assumption, and §5.2 argues
it is safe (RWMutex atomic swap + per-op value-copy snapshot + OCC), but it deserves a second set of eyes.

---

## 1. Problem & intent

**Source.** Surfaced by Andrew during the Contract #5 §5.4 ratification (2026-06-27): the Processor health item
(`f16e625`) made `lane_lag` *honest* (per-lane keys reported `null` rather than fabricated zeros) but could not
make it *real* — a single consumer cannot separate per-lane backlog. The fix is the long-standing design-of-record
the backlog, the architecture, and two contracts all name: per-lane consumers.

**Three benefits (the backlog row's a/b/c):**

- **(a) Real per-lane `lane_lag`.** Each lane's durable consumer reports its own `NumPending`; the §5.4
  `lane_lag.{default,urgent,system,meta}` keys become real and `lane_lag_total` becomes their sum. The
  Lamplighter / Loupe can finally see *which* lane is backed up.
- **(b) Priority isolation + independent control.** Lanes drain on independent pump goroutines, so an `urgent`
  op is not stuck behind a `default` backlog. The supervisor's `Pause`/`Resume` operate per lane, so a lane can
  be quiesced independently (the operator-facing payoff is Fire 4 / a follow-on control plane).
- **(c) Pattern alignment.** The sole Core-KV writer joins Loom/Weaver/Refractor on one supervised pump
  (composable pause state machine, NakWithDelay backoff floor, HealthSink) instead of a hand-rolled
  `EnsureConsumer` + `cons.Consume` loop.

**Intent, not just hygiene — and a latent correctness gap (verified 2026-06-28).** The `meta` lane's
*serialization* — "DDL cache invalidation synchronous with commit; serialization prevents concurrent DDL races"
(Contract #2 §2.3) — is a **correctness** property. It would be tempting to say the single-consumer model satisfies
it incidentally ("everything is serial"), **but that is not true today**: the production `processor-main` binary
sets **no `MaxAckPending`** and drives `cons.Consume(callback)` with **no serialization guarantee**
(`commit_path.go:599`; this is *why* the DDL cache is `sync.RWMutex`-guarded and step 8 is OCC). So **`meta` DDL
mutations are not actually serial today** — two DDL ops can be dispatched concurrently, and §2.3's meta-serial
guarantee is unenforced. Pinning `meta` to its own single pump (Fire 2/3) therefore **closes a latent correctness
gap**, not merely "makes an incidental property explicit." Business-op correctness is still held by step-8 OCC
(concurrent processing won't corrupt business state), so the live exposure is narrow — concurrent DDL-mutate vs.
DDL-cache reads — but it is real, and the §5.2 pre-build adversarial pass should **prove whether it triggers
today** so Fire 2/3 demonstrably closes it.

**Publish-side: verified correct — the gap is consume-side, not publish-side (do not "fix publishing").** A
ground-truth check (2026-06-28) confirmed every publisher already routes by lane (`"ops." + env.Lane`) and tags
the right lane: **`meta`** ← pkgmgr installer (`installer.go:311` `LaneMeta`), lens CLI (`lens.go:145,217`),
bootstrap meta-DDL (`meta_ddl.go:21`); **`system`** ← loom/weaver/bridge/objmgr engines (all default `*_LANE=system`);
**`default`** ← ordinary client ops. So DDL ops already land on `ops.meta` — **no publish change is needed**. The
single gap is that one `processor-main` consumer eats all four lane subjects with no per-lane isolation or
meta-serial pinning; this design fixes that on the **consume** side. *(`urgent` is currently **unused** — nothing
sets it except an explicit `--lane urgent` flag; splitting a consumer for it is correct-by-contract but it has no
producer yet.)*

---

## 2. Grounding — the pattern this mirrors

| Concern | Existing machinery (the precedent to extend) | Reference |
|---|---|---|
| Supervised pump | `substrate.ConsumerSupervisor` — registry of `ConsumerSpec`s, per-consumer pause state machine, NakWithDelay floor, HealthSink, `Pause`/`Resume`/`PendingForConsumer` | `internal/substrate/consumer_supervisor.go` |
| One-spec-per-stream wiring | Loom adds `triggerSpec`/`relaySpec`/`deadlineSpec`; Weaver adds `temporalSpec` — `supervisor.Add(ctx, spec)` per spec | `internal/loom/engine.go:251`, `internal/weaver/engine.go:353` |
| Handler shape | `SupervisedHandler func(ctx, Message) (Decision, error)`; `supervisedHandler(...)` adapts a `func(...) Decision` | `consumer_supervisor_spec.go:106`, `engine.go:284` |
| Sequential drain | `drain` calls `mc.Next()` then `processMsg` one message at a time → in-order per consumer | `consumer_supervisor_pump.go:238` |
| Per-consumer backlog | `PendingForConsumer(ctx, name) (uint64, error)` → `NumPending` for one durable | `consumer_supervisor.go:279` |
| Commit path (unchanged core) | `CommitPath.HandleMessage` — 9-step path, OCC at step 8 (`RevisionConflict` on CAS), DDL cache RWMutex-guarded | `commit_path.go`, `step8_commit.go`, `ddl_cache.go` |

**Architecture invariants honored.** P2 (Processor stays the sole Core-KV writer — unchanged; we change only how
ops are *delivered* to the commit path). P1 (operational state — consumer lifecycle / lag — lives outside Core
KV, in the supervisor + Health KV). P5 (N/A — this is the write path). Contract #1 key-shapes (unchanged — no new
vertices/aspects/links). The lane subjects are `ops.<lane>` two-segment subjects, exactly as every publisher
emits (`submit.go`, `candidates.go`); the stream is provisioned `ops.>`.

---

## 3. The shape

### 3.1 Read path / write path

Unchanged. Reads are still lens projections (P5); writes are still ops → Processor → Core KV (P2). This design
touches only **operation delivery** — the JetStream consumer layer between `core-operations` and
`CommitPath.HandleMessage`. No vertex/aspect/link/lens/op is added or modified.

### 3.2 The four lane consumers

Replace the single `processor-main` durable with one `ConsumerSpec` per lane, all bound to the `core-operations`
stream, each filtered to its lane subject:

| Durable name | `FilterSubject` | Pumps | Notes |
|---|---|---|---|
| `processor-default` | `ops.default` | 1 (Fire 2) → N (Fire 3) | Bulk operator + AI traffic |
| `processor-urgent` | `ops.urgent` | 1 → N | Priority; drains independently of `default` |
| `processor-system` | `ops.system` | 1 → N | Loom/Weaver/admin internal actors |
| `processor-meta` | `ops.meta` | **1, pinned** | DDL mutations — **serial by contract** (§2.3); never fanned out, even in Fire 3 |

All four share **one** `SupervisedHandler` — the refactored `CommitPath.HandleMessage` (§4). The lane is not a
routing input to the commit path (it already reads `env.Lane` from the envelope for the step-3 lane-auth check);
the per-lane split is purely about *which durable* delivers the message and *how its backlog is measured*.

> **Subject precision.** `FilterSubject` is the exact two-segment subject `ops.<lane>` — matching what
> publishers emit. The contract writes the subject as `ops.<lane>.>` (the reserved deeper form); the live
> publishers use the bare two-segment subject, which the current `processor-main` filter list also uses
> (`step1_consume.go:47`). Fire 2 filters on `ops.<lane>` to match production exactly. (If a future publisher
> emits a deeper `ops.<lane>.<x>` subject, the filter becomes `ops.<lane>.>` — a one-line change, flagged here.)

### 3.3 Fan-in to the single commit path

All lane pumps invoke the same handler closure over the same `*CommitPath`. This is safe because the commit path
is **already** concurrency-correct (it must be — the design-of-record specifies `urgent: consumers: 4`, and
today's `cons.Consume` callback is itself dispatched without a serialization guarantee, which is *why* the DDL
cache is `sync.RWMutex`-guarded and step 8 is OCC):

- **Step 8 OCC.** Each op's atomic batch carries `expectedRevision` per mutation; a concurrent write to the same
  key fails the CAS → `RevisionConflict` → `Term` + rejected reply (`step8_commit.go:156`, `commit_path.go:304`).
  Cross-lane concurrent writes to the same key are resolved exactly as concurrent same-lane writes are today.
- **Step 2 dedup.** `vtx.op.<requestId>` tracker is CAS-created; a redelivery or duplicate short-circuits
  (`step2_dedup.go`) — per-op, lane-agnostic.
- **DDL cache.** `Lookup`/`ClassForCommand` take `RLock` and **return a value copy** of `MetaVertexRef`; an op
  that read its DDL is internally consistent for its whole path even if a concurrent `meta` commit invalidates
  the cache mid-flight (§5.2).

### 3.4 Per-lane health (`lane_lag` becomes real)

`HealthHeartbeater.AttachConsumer(jetstream.Consumer)` is replaced by attaching the **supervisor** (or a thin
`LaneBacklogReader` it satisfies). Each heartbeat reads per-lane backlog:

```
for lane, durable := range {default:processor-default, urgent:processor-urgent, system:processor-system, meta:processor-meta}:
    n, err := supervisor.PendingForConsumer(ctx, durable)
    lane_lag[lane] = n            // real per-lane backlog
    // err (consumer unreadable this tick) → lane_lag[lane] = null, never a fabricated 0 (preserve the §5.4 honesty rule)
lane_lag_total = sum(readable lanes)   // null only if ALL lanes unreadable
```

`ProcessorLaneLagging` (warning ⇒ `status: degraded`, via the existing `aggregateStatus`) is raised **per lane**
that exceeds the threshold, with the lane named in the message — strictly more actionable than today's single
aggregate alert. The `lagThreshold` stays a single configurable value (default 100); a per-lane override is a
trivial follow-on if a lane needs a different ceiling.

### 3.5 Lane scaling (Fire 3)

`LATTICE_PROCESSOR_LANES_<LANE>_CONSUMERS=N` (the convention already named at `lattice-architecture.md:568`) adds
**N specs per lane sharing one JetStream queue group** (`ConsumerSpec.DeliverGroup` — the NFR12 fan-out field),
so a hot `default`/`urgent` lane processes N ops concurrently while preserving at-least-once delivery. **`meta` is
pinned to 1 regardless of any override** (a fail-closed clamp — a `meta` fan-out would violate §2.3
serialization). Default sizing mirrors the arch config example: `default=2, urgent=4, system=2, meta=1`.

---

## 4. The refactor (Fire 1, the load-bearing change)

### 4.1 Substrate seam — `Message` carries the reply inbox

The commit path replies to the caller's inbox via `replySubject(msg)` — the `Lattice-Reply-Inbox` header, falling
back to `msg.Reply()` (`commit_path.go:543`). `substrate.Message` (built by `newMessage(jetstream.Msg)`) exposes
`Subject`, `Body`, `Sequence`, `NumDelivered`, `NumPending` — **but not headers or the reply subject**, because no
prior supervised consumer ever replied to a client.

Add to `substrate.Message`:

```go
// ReplySubject is the NATS reply subject of the delivered message (msg.Reply()),
// for request-reply consumers that publish a reply to the caller's inbox. Empty
// for fire-and-forget event/CDC messages. The first request-reply consumer of the
// supervisor (the Processor) needs it; event consumers (Loom/Weaver/Refractor) leave it unused.
ReplySubject string
// Header returns the value of a message header by key, or "" if absent/none.
// Provided so a request-reply handler can honor an in-band reply inbox
// (Lattice-Reply-Inbox) without reaching for a jetstream.Msg.
Header func(key string) string
```

`newMessage` populates both. The supervisor's exported surface stays free of any `nats.go`/`jetstream` type
(strings + a `func(string) string`). The Processor's `replySubject`/`replyTo` move onto the handler and read
`Message.ReplySubject` / `Message.Header("Lattice-Reply-Inbox")` instead of the raw `jetstream.Msg`.

> This is **internal Go API** (substrate), **not** a `docs/contracts/*` frozen contract. No uncommitted contract
> edit. It is the clean "add the platform primitive" move (vs. reaching around the supervisor abstraction).

### 4.2 Disposition refactor — `HandleMessage` → `SupervisedHandler`

Today `HandleMessage(ctx, m jetstream.Msg)` **both** replies to the caller **and** disposes the message
(`m.Ack()` / `m.Term()` / `m.TermWithReason()` / `m.Nak()`). The supervisor model separates these: the handler
returns a `(Decision, error)` and the supervisor disposes; the *reply* stays handler logic (it is application
output, not ack disposition). The mapping:

| Commit-path outcome | Today | Supervised `(Decision, error)` + `Classify` |
|---|---|---|
| Committed (step 8 ok) | reply accepted; `Ack` | reply accepted; return `(Ack, nil)` |
| Duplicate (step 2) | reply duplicate; `Ack` | reply duplicate; `(Ack, nil)` |
| Malformed envelope (step 1) | reply malformed; `TermWithReason` | reply malformed; `(Term, nil)` |
| Auth denied / DDL violation / protected-key / `RevisionConflict` (step 3/6/8) | reply rejected; `Term` | reply rejected; `(Term, nil)` |
| Dedup-lookup KV error (step 2) | `Nak` (blind redelivery) | `(NakWithDelay, transientErr)` → `Classify`=Transient → redeliver with floor (no hot-loop) |
| KV unreachable / infra error (step 8 commit, authorizer infra) | `Nak` / reject | return `infraErr` → `Classify`=Infra → **pump pauses + probes recovery**, message left pending |

**Two behavior deltas, both improvements, both flagged:**

- **`NakWithDelay` floor** replaces blind `Nak` on transient KV errors — kills the redelivery hot-loop against a
  still-failing dependency (the supervisor's whole reason for the floor). Bounded, not unbounded.
- **Infra pause + probe** replaces blind retry on a lane-wide infra failure (KV unreachable). A genuinely
  lane-wide infra fault pauses *that lane's* pump and probes for recovery — surfaced in Health KV via the
  HealthSink — instead of silently spinning. The `Classify` func must distinguish **per-op transient** (a single
  bad op → `Nak`/`Term` that op, keep draining) from **lane-wide infra** (dependency down → pause). The commit
  path already encodes this distinction (it `Term`s contract violations and `Nak`s only on KV-read failure); the
  `Classify` mapping makes it explicit. **This is the most important review surface of Fire 1.**

### 4.3 Wiring (`cmd/processor/main.go`)

`EnsureConsumer` + the `go cp.Run(ctx, cons)` goroutine are replaced by:

```go
sup := substrate.NewConsumerSupervisor(conn)
handler := cp.SupervisedHandler()          // the refactored §4.2 closure
for _, spec := range processor.LaneSpecs(handler, healthSink, cfg) {  // Fire 1: one spec; Fire 2: four
    if err := sup.Add(ctx, spec); err != nil { return err }
}
hb.AttachBacklogReader(sup)                 // §3.4
defer sup.Stop()                            // preserves durables (unlike Remove)
```

The outbox consumer (`internal/processor/outbox`) is untouched — it already runs its own loop and is downstream
of commit.

---

## 5. Reconciliation with the mental model (pre-empting "but didn't we…?")

### 5.1 "Didn't `f16e625` already fix `lane_lag`?"

It made it **honest** (per-lane `null` instead of fabricated `0`; real `lane_lag_total`) but explicitly could not
make it **real** — one consumer cannot split lane backlog. §5.4 reserved the per-lane keys "**not retired**…when
the Processor adopts the architecture's design-of-record per-lane consumers." This is that adoption. It
*populates* the reserved keys; it does not redefine them.

### 5.2 "Doesn't splitting lanes break the synchronous DDL-cache invalidation (§2.3)?"

**No — and here is the precise argument.** The §2.3 guarantee has two halves:

1. **"Serialization prevents concurrent DDL races"** — DDL-vs-DDL ordering. Preserved *structurally*: `meta` is
   its own **single** pump (one `mc.Next()` at a time), so two DDL mutations never apply concurrently. This is
   *stronger* than today, where meta ops are serial only because *everything* is.
2. **"DDL cache invalidation synchronous with commit"** — the `meta` handler commits the DDL mutation and calls
   `DDLCache.Invalidate` in the same handler call, before returning `Ack`. A `default`-lane op running
   concurrently interacts with the cache only through `Lookup`/`ClassForCommand`, which take `RLock` and
   **return a value copy** of `MetaVertexRef`. `Invalidate` takes the write lock and **atomically swaps** the
   map entry (`ddl_cache.go:344`). Therefore a concurrent data op sees *either* the pre-invalidation DDL *or* the
   post-invalidation DDL — **never a partial state** — and once it has read its `MetaVertexRef`, it is internally
   consistent for its entire path (hydrate→execute→validate use the snapshot, not a re-read).

The only thing the lane split changes is: a data op *concurrent with* a DDL change may use old-or-new
non-deterministically. **That non-determinism already exists today** — two clients submitting "change this DDL"
and "write an instance of it" simultaneously have no defined order under the single consumer either (delivery
order ≠ submission order). The architecture's design-of-record (`urgent: consumers: 4`, i.e. concurrent commit)
already assumes this. We add no new race; we make the existing concurrency model explicit per lane.

**Recommendation:** run the focused adversarial pass on this boundary (the For-Andrew note) before Fire 2 — it is
the one assumption worth a second reviewer, and the property to attack is: *can a data op observe a half-applied
DDL, or commit against a DDL whose meta-vertex is mid-tombstone?* (The argument above says no — value-copy
snapshot + atomic swap + the op's own step-8 OCC against the actual KV revisions — but adversarial confirmation
is cheap insurance on the sole writer.)

### 5.3 "Does this introduce new state?"

No new Core-KV state (no vertices/aspects/links). The only new operational state is the supervisor's per-consumer
lifecycle (pause state machine + HealthSink), which already exists for Loom/Weaver/Refractor and lives **outside**
Core KV (P1-clean) — in memory + Health KV. `lane_lag` is computed, not stored.

### 5.4 "Cross-lane ordering — do we lose a total order we depended on?"

Today's single consumer delivers in stream sequence (a total order across lanes). Per-lane pumps preserve
**within-lane** order (sequential drain) but drop the **cross-lane** total order. That loss is the *intent*: lanes
are priority classes; `urgent` jumping ahead of `default` is the whole point of (b). No correctness depends on
cross-lane ordering — ops are independent units reconciled by OCC, and the only ordering guarantee the platform
makes is per-`requestId` idempotency + per-key OCC, both lane-agnostic.

---

## 6. Throughput note (honest)

Today's single `processor-main` is driven by `cons.Consume(callback)`, which can dispatch the callback without a
strict serialization guarantee (hence the defensive RWMutex + OCC). Per-lane **sequential** pumps (Fire 2)
process each lane one op at a time. At MVP scale this is sufficient by the architecture's own statement
(`:1031` — "single consumer per lane is likely sufficient at MVP; parallelism is a Phase 2 concern"), and the
priority-isolation win (urgent no longer behind default) typically *improves* tail latency for the lanes that
matter. Where a lane genuinely needs concurrency, **Fire 3** restores it via queue-group fan-out
(`consumers: N`). I flag the shift rather than hide it: a deployment that today leans on cross-lane concurrency of
a single hot `default` stream will want Fire 3 sized appropriately. Fires 2 and 3 can land close together if that
matters for a given deployment.

---

## 7. Risks & alternatives

**R1 — Disposition-refactor regression on the sole writer (highest).** Re-expressing ack/term/nak as `(Decision,
error)` + `Classify` on the hot path risks a subtle disposition bug (e.g. an op that should `Term` instead
`Nak`s and redelivers forever). *Mitigation:* Fire 1 keeps a **single** all-lanes spec (behavior parity, same
`processor-main` durable, no lane split, no durable migration) so the refactor is provable in isolation; a
table test maps every commit-path outcome to its expected `(Decision, error)`; the existing commit-path e2e
(`make test-*` convergence + the per-step unit suites) must stay green. **Full 3-layer review.**

**R2 — Meta-lane concurrency (covered §5.2).** *Mitigation:* meta pinned to a single pump + the adversarial pass
on the DDL-cache boundary before Fire 2.

**R3 — Throughput shift (covered §6).** *Mitigation:* Fire 3 scaling lever; honest flag to Andrew.

**R4 — Durable migration.** Fire 2 retires `processor-main` and creates four new durables. A live stack restarted
into Fire 2 has a stale `processor-main` durable holding an ack floor. *Mitigation:* on startup, `Remove` the
legacy `processor-main` durable if present (its un-acked messages are redelivered to the new lane durables, which
start `DeliverAll` and are idempotent via the §2 dedup tracker — a redelivered already-committed op short-circuits
at step 2). Document the one-time redelivery in the Fire 2 commit. No data loss (at-least-once + dedup).

**R5 — Infra-pause blast radius.** A lane-wide infra fault now pauses that lane's pump (vs. today's blind spin).
If `Classify` mis-tags a *per-op* failure as infra, one poison op could pause a whole lane. *Mitigation:* the
`Classify` mapping defaults to **Transient** (per-op `Nak`/`Term`), and only a narrow, explicitly-enumerated set
of substrate connection/KV-unreachable sentinels classify as Infra — mirroring how Loom/Weaver classify (reuse
their `IsConnectionError`-style sentinels). Tested with a fake KV that returns a connection error vs. a logical
error.

### Alternatives considered

- **A1 — Per-lane consumers *without* the supervisor (N × `EnsureConsumer` + N × `cons.Consume`).** Gets (a) real
  per-lane lag and (b-partial) priority isolation with a smaller diff, *avoiding* the disposition refactor.
  **Rejected:** it forfeits (c) entirely, re-hand-rolls lifecycle/backoff/health the supervisor already provides,
  gives no `Pause`/`Resume` (so no clean path to Fire 4's operator control), and leaves the sole writer as the
  one component *not* on the shared pump — the exact divergence this item exists to close. The contract names
  `substrate.ConsumerSupervisor` specifically. The disposition refactor is the cost of doing it right once.
- **A2 — Keep `HandleMessage` self-disposing; wrap it to always return `Ack`.** Avoids the §4.2 refactor.
  **Rejected:** the supervisor's `applyDecision` would *double-dispose* (the handler already acked), and the
  infra-pause semantics (which rely on the handler returning an error and leaving the message **pending**) are
  unreachable if the handler already acked — you get the supervisor's shell with none of its failure posture.
  Half-adoption is worse than A1.
- **A3 — Serialize the whole commit path behind a single worker, per-lane consumers only for *visibility*.**
  Preserves total ordering + makes lag real, but kills priority isolation (b) and is *less* concurrent than
  today. **Rejected:** (b) is half the value, and the design-of-record already endorses concurrent commit.
- **A4 — Add the substrate seam as a separate fire before Fire 1.** **Rejected:** the seam is tiny and only the
  Processor consumes it; bundling it into Fire 1 keeps the refactor self-contained and the seam exercised by a
  real consumer immediately (no dead scaffolding).

---

## 8. Decomposition for the Steward (each fire independently shippable + green)

**Fire 1 — Substrate reply seam + commit-path disposition refactor (behavior parity).**
Add `ReplySubject` + `Header` to `substrate.Message` (populated in `newMessage`); move `replySubject`/`replyTo`
onto the handler; refactor `HandleMessage` into a `SupervisedHandler` + `Classify` (§4.2); wire **one**
`ConsumerSupervisor` spec (`processor-main`, `FilterSubject` = the existing four-subject coverage via the stream's
`ops.>` — or keep the spec's single `ops.>` filter) so net behavior is identical (same durable, no lane split, no
migration). Health: `AttachConsumer` → `AttachBacklogReader(sup)` reporting the single `processor-main` backlog as
`lane_lag_total` (per-lane keys stay `null`, exactly as today). *Tests:* outcome→`(Decision,error)` table;
Classify transient-vs-infra table; full commit-path unit + convergence suites green. **Full 3-layer** (sole-writer
hot path + substrate surface).

**Fire 2 — Lane split (the core).**
Replace the single spec with four (`processor-{default,urgent,system,meta}`, `FilterSubject` = `ops.<lane>`); meta
pinned to one pump. Health reports real per-lane `lane_lag.*` via `PendingForConsumer` per lane (null-honest per
lane), `lane_lag_total` = sum, `ProcessorLaneLagging` per lane. Startup migration: `Remove` legacy `processor-main`
if present (R4). *Tests:* an embedded-NATS test that publishes to each lane and asserts per-lane `NumPending`
isolation (a backlog on `default` leaves `urgent`'s lag at 0); meta-serialization assertion (concurrent DDL ops
apply in order); the migration redelivery is idempotent (dedup short-circuit). **Full 3-layer** + the §5.2
adversarial pass beforehand.

**Fire 3 — Per-lane concurrency from config (scaling lever).**
Honor `LATTICE_PROCESSOR_LANES_<LANE>_CONSUMERS=N` → N specs per lane in a shared `DeliverGroup` queue group; meta
clamped to 1 (fail-closed). Default `default=2, urgent=4, system=2, meta=1`. *Tests:* N-worker fan-out drains a
backlog concurrently; meta clamp holds even with an override env set. **Thorough lead review** (additive config,
no new disposition logic) — overridable to 3-layer if the queue-group wiring proves subtle.

**Fire 4 (separable follow-on — recommend filing as its own backlog row, not bundling).**
A `lattice.ctrl.processor.*` control responder (list lanes + lag, pause/resume a lane) mirroring
`internal/weaver/control` / `internal/loom/control`, consumed by Loupe. It has a **real** consumer (Loupe), so it
is not dead scaffolding — but it is cleanly separable from the delivery refactor and intersects the deferred
**Control-plane Capability authorization (FR30)** item (it would ship with the same allow-all `StubCapabilityChecker`
the other two planes carry until D1's actor seam lands). Recommend a dedicated row rather than stretching this
design across the control plane.

---

## 9. Contract surface (summary)

| Contract / doc | Touch? | Why |
|---|---|---|
| Contract #2 §2.3 (lanes), §3.7 (consumer config) | **Build to** — no change | Already specifies per-lane consumers + `meta` `MaxAckPending=1` |
| Contract #5 §5.4 (`lane_lag`) | **Build to** — no change | Already reserves per-lane keys for this adoption; we populate them |
| Contract #1 (key-shapes) | N/A | No new vertices/aspects/links |
| `internal/substrate` Message/Conn (Go API) | **Extend** (Fire 1) — *not* a frozen contract | First request-reply consumer needs the reply inbox on `Message` |
| `docs/components/processor.md`, `substrate.md` | **Update** (each fire) | Reflect the per-lane supervised pump (a doc fix, direct to main) |

**No uncommitted contract edit is staged by this design.** (The unrelated `docs/contracts/02-*.md` modification in
the working tree is a different fire's `kv.Links` §2.5.1 edit and is left untouched.)

---

## 10. Test strategy (cross-fire)

- **Unit:** outcome→Decision table; Classify transient-vs-infra table; per-lane `PendingForConsumer` isolation;
  meta-serialization ordering; null-honest lag per lane; meta clamp under override (Fire 3).
- **Integration (embedded NATS, `jsstore.Dir(t)` per CI-parallelism rule):** publish a mixed-lane workload,
  assert per-lane drain independence + per-lane `lane_lag`; kill-and-restart across the Fire 2 migration, assert
  no double-commit (dedup) and no lost op (at-least-once).
- **e2e (ephemeral stack):** the existing lease-convergence + object-gc convergence suites must stay green
  through every fire (they exercise the real Processor end-to-end); a focused assertion that an `urgent` op
  commits ahead of a deep `default` backlog (the priority-isolation proof).
- **Gates:** `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`, `make test-bypass`
  (Gate 2), `make test-capability-adversarial` (Gate 3), `go test ./internal/processor/... ./internal/substrate/...`.

---

## 11. Open questions — resolved (decide-don't-defer)

1. **One supervisor or one per lane?** → **One** supervisor, four specs (mirrors Loom adding three specs to one
   supervisor). Per-lane supervisors would fragment the registry for no benefit.
2. **Keep the `processor-main` durable name for Fire 1?** → **Yes** — Fire 1 is behavior-parity, so reusing the
   name means zero durable migration in Fire 1; the migration is isolated to Fire 2 (R4).
3. **Per-lane `lagThreshold`?** → **No** (single threshold) for now; per-lane override is a trivial follow-on if
   a lane needs a different ceiling. Don't add config nobody has asked for.
4. **Does the reply seam belong on `Message` or a new request-reply handler type?** → **On `Message`** (plain
   `ReplySubject` + `Header`), the smallest extension; a parallel handler type would fork the supervisor surface
   for one consumer.
5. **Fire 4 in scope?** → **No** — file as its own row (it intersects FR30 control-plane authz and is cleanly
   separable). The delivery refactor (Fires 1–3) is the ratify-and-build unit here.

---

## 12. Definition of done (for the build)

- Four lane consumers under one `ConsumerSupervisor`; `meta` serial and clamp-protected.
- `lane_lag.{default,urgent,system,meta}` real and null-honest; `lane_lag_total` = sum; per-lane
  `ProcessorLaneLagging`.
- Commit path drives the supervised handler; reply path reads `Message.ReplySubject`/`Header`; disposition via
  `(Decision, error)` + `Classify`.
- Legacy `processor-main` durable retired cleanly (Fire 2) with idempotent redelivery.
- All gates green; convergence e2e green; priority-isolation e2e proves urgent-ahead-of-default.
- `processor.md` + `substrate.md` updated to the per-lane supervised pump.
