# Design — userTask dispatch idempotency (stop duplicate human tasks)

**Status:** 📐 Proposal — awaiting Andrew ratification; deferred to a **fresh Steward run** (Andrew,
2026-06-25). Enforcement is **consumer-side** (Processor/Starlark), not a producer-side GET — see §4.3.
**Author:** Winston (Steward, 2026-06-25). **Owner area:** Weaver (claimId mint/derive) + the Processor
(a new idempotent-create affordance, §4.4) + the `CreateTask` / `StartLoomPattern` ops.
**Frozen-contract touches:** Contract #10 §10.3 (§7) **and** Contract #2 §2.5 *or* #3 §3.2 (the §4.4
affordance) — Andrew's call. The interim `maxretries=1` cap (10a5d7a) holds the line until this lands.

---

## 1. Problem

Weaver re-creates **human userTasks** indefinitely. For a lease application a person has not yet
completed, the Tasks inbox accumulates a fresh `RecordIdentityPII` and `SignLease` every ~30 minutes
(found live by Andrew; the API showed 3× each at 30-min intervals).

Root cause is the §10.3 reconciler **mark lease**. Weaver records a per-gap suppression mark with a
30-minute lease (`defaultMarkLease`, [reconciler.go:17](../../internal/weaver/reconciler.go)); when the
lease expires the sweep presumes the dispatched work dead and **re-dispatches** the gap action
([reconciler.go:231](../../internal/weaver/reconciler.go) `reclaim`), gated only by `gapSuppressed`
(skip if `inflight_<g>` set **or** the dispatch-count reached `maxretries_<g>`,
[evaluator.go:381](../../internal/weaver/evaluator.go)). The two external gaps (bgcheck/payment) are
protected by their `inflight_<g>` companion; the two **human** userTask gaps (`missing_onboarding`,
`missing_signature`) have **no suppression companion**, so each lease expiry re-creates their task.

The re-fire itself is **by design**: the dispatched identity is **episode-scoped on `markRevision`**,
which changes every reclaim —
`deriveEpisodeTaskID(targetID, entityID, gapColumn, markRevision)`
([actuator.go:140](../../internal/weaver/actuator.go), [strategist.go:152](../../internal/weaver/strategist.go)) —
and `triggerLoom`/onboarding passes **no** instance id at all
([strategist.go:109](../../internal/weaver/strategist.go)), so Loom mints a fresh instance (hence a fresh
task) per `StartLoomPattern`. Each reclaim is intentionally a **new episode**.

§10.3 already names this: a re-fired `triggerLoom`/`assignTask` is an *"accepted rare double (lease ≫
remediation latency makes it rare … a duplicate task is operator-visible) — documented bound … the robust
**check-before-act** variant is a **Phase-3 hardening**."*

**The defect is the assumption, not the code.** *"Lease ≫ remediation latency"* holds for **machine**
remediation (an external call resolves in seconds, far inside the 30-min lease — re-fire is genuinely
rare). It is **false for a human**: a person legitimately takes hours-to-days, *far longer* than the
lease, so for human-paced gaps the "rare double" is a **guaranteed ~30-min duplication**. The model
under-specified human-paced gaps. It is **general** — every userTask gap in every package (clinic
included) inherits it.

This is the deferred Phase-3 "check-before-act" hardening, now forced concrete.

---

## 2. Principle

The duplicate is the one place orchestration mints a **non-deterministic** (episode-scoped) identity
where a userTask wants a **stable** one. Everywhere else, at-least-once delivery is made safe by
deterministic identity collapsing on an existing record:

- the Contract #4 tracker `vtx.op.<requestId>` (re-submit collapses),
- Loom's userTask `taskId` from `(instanceId, cursor)` (§10.6 crash-retry collapses),
- the bridge's external dedup on the deterministic service-instance `instanceKey`.

So the fix is not a new mechanism — it makes the reconciler obey the **same idempotency philosophy** as
the rest of the platform: a userTask dispatch carries a **deterministic, per-open-episode identity**, and
re-dispatch is **idempotent** against it.

---

## 3. The key: revive the dormant `claimId` as the per-episode token

A purely `(targetId, entityId, gapColumn)` key has a correctness hole: it cannot distinguish a *fresh*
open-episode from a *completed-then-reopened* one (a freshness-reopening gap would collapse on the old
completed task and never re-create). We need an identity **stable across reclaims** but **fresh across a
legitimate close→reopen**.

That token already exists, dormant and purpose-built. The reconciler `mark` carries `ClaimID`
([state.go:78](../../internal/weaver/state.go)) — minted atomically with the CAS-create as a per-episode
identity for the old nudge's idempotency, orphaned when the nudge was retired (13.1, 2026-06-18); §10.3
records it as *"left optional … no remaining producer."* It is the exact hook, because of **when** it is
minted:

- **Mint `claimId` at the mark's CAS-create** — the start of an open-gap episode (a fresh NanoID).
- **Preserve `claimId` across reclaims** — the reconciler `replace` keeps it; only the lease/`claimedAt`
  refresh.
- **Derive the userTask identity from it** — `deriveID("task:", targetID+entityID+gapColumn+claimId, 0)`.

→ **stable across reclaims** (no duplicate while the human takes their time), **fresh across reopen** (a
new mark ⇒ new `claimId` ⇒ a fresh task). `markRevision` is too fresh (changes per reclaim); a blind
triple is too stable (misses reopen). `claimId` is exactly right.

---

## 4. Mechanism — both userTask paths

Two gap actions produce human tasks; both switch from `markRevision`- to `claimId`-derived identity.
**Nothing else changes** (see §5 scope).

### 4.1 `assignTask` (the SignLease task)

- Strategist derives the task id from the episode token:
  `taskId = deriveStableTaskID(targetID, entityID, gapColumn, claimId)` (a new helper alongside
  `deriveEpisodeTaskID`; folds `claimId` into the `deriveID` seed, [actuator.go](../../internal/weaver/actuator.go)).
  The `payload` closure already receives the mark context at plan time
  ([strategist.go:146](../../internal/weaver/strategist.go)); thread `claimId` through instead of the bare
  `markRevision`.

### 4.2 `triggerLoom` (the onboarding pattern → RecordIdentityPII task)

- Strategist passes a **deterministic instance id** to `StartLoomPattern`:
  `instanceId = deriveStableInstanceID(targetID, entityID, gapColumn, claimId)`
  ([strategist.go:109](../../internal/weaver/strategist.go) — today the payload is
  `{patternRef, subjectKey, expectedRevision}` with no instance id).
- `StartLoomPattern` honours a caller-supplied `instanceId` (verbatim if present, minted if absent —
  mirrors `CreateTask`'s optional `taskId`, §10.6). A re-trigger for the same open-episode collapses on
  the existing `instance.<instanceId>` (CreateOnly) → no new Loom instance → no new task. This dedups the
  whole pattern, not just its task — the correct altitude for `triggerLoom`.

### 4.3 The act — enforce idempotency on the CONSUMER (Processor), not a producer-side GET

A producer-side "GET the task; skip if present" probe (in Weaver's reconciler) is **race-prone, and the
race is structural**: a dispatch is *published to `core-operations` and committed to Core KV
asynchronously*, so between Weaver publishing `CreateTask` and the task appearing in Core KV there is a
propagation window. A second reclaim (or the lane-1 path) that GETs Core KV inside that window sees
**absent** and re-publishes — two `CreateTask`s now race in the queue. The producer GET does not *prevent*
the double-publish; only the **consumer** (the Processor, committing against actual Core-KV state) can
decide create-vs-collapse atomically. So enforcement belongs at the Processor.

**This is exactly how Loom already survives the same lag** (§10.6): Loom never relies on a producer GET
to gate re-publishing. It (a) keys the task on a **deterministic** `(instanceId, cursor)` id, (b) writes
the op **write-ahead to its outbox** and relays it once, (c) guards on the **pendingToken** (a redelivered
trigger *"finds the cursor present"* and drops, [engine.go:398](../../internal/loom/engine.go)), and (d)
its creation-deadline probe only ever **disarms-on-present** — *"once onDeadline's probe confirms the task
vertex exists, it disarms"* ([engine.go:903](../../internal/loom/engine.go)); it **never blindly
re-publishes on absent**. The net effect: any re-publish **collapses** on the deterministic id at the
Processor (the Contract #4 tracker / `CreateOnly`), never duplicates. Loom doesn't duplicate because it
**never re-episodes** — Weaver's reconciler intentionally does (a fresh `markRevision` per reclaim), which
is right for *external* retries but wrong for human tasks.

So the fix moves Weaver's userTask dispatch onto the same footing: **deterministic `claimId`-derived
identity (§3) + the Processor as the single idempotency authority.** With a deterministic `taskId`, a
re-published `CreateTask` is decided at commit against real Core-KV state. The remaining question is only
*how the Processor expresses "already there → fine"* without noise:

- **`create` today is `CreateOnly`** ([step8_commit.go:154](../../internal/processor/step8_commit.go)) —
  a re-publish **rejects** (no duplicate, but a rejected op per lease = operator-noise).
- A declared **read** of the maybe-absent task key is a **fatal HydrationMiss**
  ([step4_hydrate.go:152](../../internal/processor/step4_hydrate.go)) — so the Starlark **cannot** simply
  read-before-create without the caller pre-reading + conditionally declaring (the identity-domain /
  large-file CC5 pattern) — which is the producer GET we are rejecting.

So a clean (noise-free, durable) consumer-side create-or-skip needs **one new Processor affordance**
(§4.4). The producer-side probe is explicitly **rejected** for the structural-race reason above.

### 4.4 What it takes — the Processor affordance (pick one)

Both are consumer-side, lag-free, and noise-free; both compose with the `claimId`-derived `taskId`. **Recommend (a).**

- **(a) Optional/lenient `contextHint` read** — add `ContextHint.OptionalReads` (or a per-key lenient
  flag): a declared optional read that **hydrates as absent (`key not in state`)** instead of
  HydrationMiss. Then `CreateTask` declares the deterministic `vtx.task.<taskId>` as optional, and the
  Starlark is **create-or-skip**: `present-and-alive → no-op success (return the existing key); else
  create`. The commit-time `CreateOnly` remains the atomic backstop for a hydrate→commit race. *Touch:*
  Contract #2 §2.5 (`contextHint` shape) + `step4_hydrate.go`. *Why preferred:* broadly useful — every
  idempotent-create op (identity-index, object-attach, this) currently hand-rolls the pre-read+conditional-declare
  dance; a first-class optional read retires that platform-wide.
- **(b) `createIfAbsent` mutation** — a new mutation op (alongside `create`/`update`/`tombstone`) that is
  **create-or-noop** at commit (no-op if the key exists alive, create if absent). `CreateTask` emits
  `createIfAbsent(vtx.task.<taskId>, …)`; no read needed. *Touch:* Contract #3 §3.2 (mutation vocabulary)
  + `starlark_runner.go` + `step8_commit.go` (+ the substrate atomic-batch, which today is strict
  `CreateOnly` — a per-op skip-if-exists needs either a hydrate-time existence read or a substrate
  primitive). More surgical in spirit but it pushes a read/skip decision into the atomic-batch layer.

> **The deterministic `requestId` alone is not enough.** Deriving the dispatch `requestId` from `claimId`
> would collapse a re-publish at the Contract #4 tracker — but the tracker is `CreateOnly` with a **24h
> TTL** ([step8_commit.go:173](../../internal/processor/step8_commit.go)); a human routinely exceeds 24h,
> after which the tracker is gone and the re-publish runs, falling back to the `CreateOnly`-reject noise.
> So the durable, noise-free guard must live at the **`taskId`** level (the affordance above), not only the
> requestId/tracker.

---

## 5. Scope — surgical, userTask-only

- Only `assignTask` and `triggerLoom` switch to `claimId`-derived identity. **External gaps keep
  `markRevision` episode-scoping** — their reclaim-retry is *intended* (re-call a dead vendor / mint a
  fresh service instance) and already bounded by `inflight_<g>` + `maxretries_<g>`. `directOp` likewise
  unchanged.
- **No package code** — the fix is entirely in Weaver core + the two generic orchestration ops, so it
  fixes every userTask gap in every package (clinic included) with zero per-package work.
- `directOp`'s `reads`-injected `expectedRevision` and the external `inflight`/budget machinery are
  untouched.

---

## 6. Correctness properties

| Property | `maxretries=1` (interim cap) | producer-side GET probe | **`claimId` id + consumer-side create-or-skip (this design)** |
|---|---|---|---|
| No duplicate while task open | ✅ | ⚠️ races the publish→commit lag | ✅ decided atomically at commit |
| Race-free under propagation lag | ✅ | ❌ GET sees absent mid-lag → double-publish | ✅ Processor is the single authority |
| Re-create after legit close→reopen | ✅ (count resets) | ⚠️ | ✅ fresh `claimId` ⇒ fresh task |
| Self-heal a task lost out-of-band | ❌ never re-creates | ⚠️ | ✅ absent ⇒ create |
| No rejected-op noise per lease | ✅ (suppressed) | ⚠️ | ✅ create-or-skip no-ops (with §4.4) |
| General (all packages) | ❌ per-package columns | ✅ | ✅ |

The interim cap is create-once-*forever* (never recovers a lost task) and is per-package; this design is
reopen-correct, self-healing, and general. **The cap becomes redundant and is reverted as part of this
fix** (the `maxretries_onboarding`/`maxretries_signature` columns + the `retry_budget.go` constants).

---

## 7. Contract impact — §10.3 amendment (Andrew ratification)

Proposed replacement for the §10.3 *"Re-fire after lease expiry — idempotency by action"* bullet (do
**not** edit the frozen file until ratified):

> **Re-fire after lease expiry — consumer-enforced idempotency by deterministic episode identity.** A
> userTask reclaim is keyed by the **open-episode identity**: the mark's `claimId` (minted at the mark's
> CAS-create, **preserved** across reclaims) seeds the dispatched artifact's id — `assignTask`'s `taskId`
> and `triggerLoom`'s Loom `instanceId`. Weaver re-publishes the dispatch **without** a producer-side
> existence check (which would race the publish→commit propagation lag); the **Processor** is the single
> idempotency authority — the `CreateTask` / `StartLoomPattern` op is **create-or-skip** on the
> deterministic key (present-and-alive → no-op success, absent → create), decided atomically at commit
> against real Core-KV state. A legitimate close→reopen mints a new mark ⇒ new `claimId` ⇒ a fresh
> artifact; an out-of-band deletion self-heals (absent ⇒ create). This **supersedes** the "accepted rare
> double / check-before-act = Phase-3 hardening" disposition for the two human userTask actions; external
> gaps retain episode-scoped (per-reclaim) dispatch, bounded by `inflight_<g>` + `maxretries_<g>`.

Plus the §4.4 affordance contract (one of): **Contract #2 §2.5** gains an optional/lenient `contextHint`
read (absent → not-in-state, no HydrationMiss); **or Contract #3 §3.2** gains a `createIfAbsent`
mutation. And the §10.3 mark value-shape note — `claimId` regains a producer (CAS-create) + consumer (id
derivation); strike *"left optional … no remaining producer."*

The frozen-contract touches are: §10.3 (above) and the §4.4 affordance (#2 §2.5 *or* #3 §3.2). The
mechanism, scope, and identity design (§3–§6) are Winston-ratified; the affordance choice + both
amendments are Andrew's to ratify on the fresh run.

---

## 8. Implementation plan (post-ratification)

1. **Mint + preserve `claimId`** — set `ClaimID` (fresh NanoID) on the mark CAS-create in `fireEpisode`
   ([evaluator.go:222](../../internal/weaver/evaluator.go)); preserve it in the reconciler `replace`
   ([reconciler.go:310](../../internal/weaver/reconciler.go) → the `marks.replace` seam) — only
   lease/`claimedAt`/`heldBy` refresh.
2. **Stable id derivation** — `deriveStableTaskID` / `deriveStableInstanceID` (claimId-seeded) next to
   `deriveEpisodeTaskID` ([actuator.go:140](../../internal/weaver/actuator.go)); use them in the
   `assignTask` and `triggerLoom` plan branches ([strategist.go:104](../../internal/weaver/strategist.go),
   [strategist.go:152](../../internal/weaver/strategist.go)). Thread `claimId` into the plan `payload`
   closure (it already takes the mark context).
3. **Processor affordance (§4.4)** — implement the chosen one. **(a, recommended):** add
   `ContextHint.OptionalReads` (Contract #2 §2.5) and the lenient hydrate leg in
   [step4_hydrate.go:143](../../internal/processor/step4_hydrate.go) (absent → skip, not HydrationMiss).
   **(b):** add the `createIfAbsent` mutation to `starlark_runner.go` (parse) + `step6_validate.go` +
   `step8_commit.go` (commit).
4. **Idempotent `CreateTask` + `StartLoomPattern`** — consumer-side create-or-skip on the deterministic key.
   With (a): declare `vtx.task.<taskId>` / `instance.<instanceId>` in `OptionalReads`; Starlark branches
   `present-and-alive → no-op success; else create` (keep `TestCreateTaskReads_MatchDDLScript` /
   drift-guards in lock-step). With (b): emit `createIfAbsent(...)`. `StartLoomPattern` also gains an
   optional caller-supplied `instanceId` (verbatim if present), mirroring `CreateTask`'s `taskId` seam.
   **No producer-side GET** in Weaver — it simply re-publishes the deterministic-id dispatch and the
   Processor decides create-vs-skip at commit. (Lane-1's first dispatch is unchanged beyond minting
   `claimId` + the deterministic id; the mark CAS-create already throttles lane-1 re-dispatch, §10.8.)
5. **Revert the interim cap** — drop `maxretries_onboarding`/`maxretries_signature` from
   `packages/lease-signing/{lenses.go,retry_budget.go}` and the `UserTaskDispatchCaps` test.

## 9. Test plan

- **Weaver unit:** a reclaim of a userTask gap with a preserved `claimId` derives the **same** `taskId`
  (vs `markRevision` deriving a different one today); the reclaim op is idempotent (present → no-op).
- **`claimId` lifecycle:** minted on CAS-create, preserved across `replace`, gone on gap-close
  (`clearClosedMarks`); a fresh mark after reopen mints a **new** `claimId` (fresh task id).
- **Consumer-side idempotency:** `CreateTask` / `StartLoomPattern` given a deterministic key whose vertex
  already exists-and-is-alive returns a **no-op success** (no second vertex, no rejection); absent → creates;
  tombstoned → re-creates. Drive it at the Processor (commit-path) level, not via a producer GET.
- **Lag race (the load-bearing case):** two `CreateTask`s with the **same** deterministic `taskId` arriving
  back-to-back (the publish→commit window) commit to **one** task — the second no-ops/collapses. This is the
  scenario a producer GET cannot cover.
- **Integration / heavy e2e:** drive a lease application, leave the userTasks open across ≥1 mark lease
  (a shortened `MarkLease` test config — see §10), assert **exactly one** `RecordIdentityPII` + one
  `SignLease` persist; then complete one and confirm reopen behaviour where applicable.
- **No-regression:** external gaps still re-dispatch on reclaim (episode-scoped), bounded by
  `inflight`/`maxretries`.

## 10. Risks / open items (Winston-resolved unless noted)

- **`MarkLease` not test-configurable.** The heavy e2e needs a short lease to observe a reclaim within
  wall-clock; `MarkLease`/`SweepInterval` are config fields ([engine.go:57](../../internal/weaver/engine.go))
  but `cmd/weaver` exposes no env for them. *Resolution:* the **engine config already carries them** — the
  test harness sets them directly; no `cmd/weaver` env needed for the test (a separate, optional
  operability nicety, not part of this fix).
- **Enforcement altitude (consumer, not producer) — Andrew's call, 2026-06-25.** A producer-side GET is
  rejected because it races the publish→commit propagation lag (§4.3): inside the window the GET sees
  absent and re-publishes, so it never *prevents* a double-publish — only the Processor, committing against
  real Core-KV state, can. Cost: one new Processor affordance (§4.4); benefit: it generalizes (every
  idempotent-create op stops hand-rolling the pre-read dance). The reads-miss-is-fatal rule
  ([step4_hydrate.go:152](../../internal/processor/step4_hydrate.go)) is *why* an op cannot just
  read-before-create today — the affordance is what removes that wall.
- **Affordance choice (a vs b).** (a) optional read is broader and lower-risk (one hydrate leg, no
  atomic-batch change); (b) `createIfAbsent` is conceptually tidy but pushes a skip-if-exists decision into
  the strict-`CreateOnly` atomic batch. Recommend (a); final call on the fresh run.
- **`StartLoomPattern` idempotency interaction with Loom's own deterministic `taskId`.** Once the Loom
  *instance* id is stable, Loom's `(instanceId, cursor)` task id is automatically stable too — the two
  compose; no separate Loom-task change needed.
- **Adversarial review** (pre-mortem): the one true hazard is a stable id colliding across the
  close→reopen boundary — closed by minting `claimId` at CAS-create (a reopen is a *new* mark). The
  second is an idempotent op masking a real second-task need — there is none for these gaps (assignee ==
  scopedTo == subject, §10.5; one open task per gap is the invariant). Recommend a full party + 3-layer
  adversarial pass on the **built** change before admit (Weaver core + a frozen-contract amendment clears
  the L+ bar).

---

## 11. Sequencing — a fresh Steward run (Andrew, 2026-06-25)

1. Andrew ratifies (a) the §4.4 affordance choice (recommend the optional read) and (b) the §7 §10.3
   amendment — or redirects.
2. Apply the contract edits (§10.3 + the affordance's §2.5/§3.2) to `docs/contracts/*` (Andrew's edit / on
   his nod).
3. Build §8: the Processor affordance first (it is the keystone + reusable), then the Weaver `claimId`
   mint/derive, then the idempotent `CreateTask`/`StartLoomPattern`. Party + 3-layer adversarial review
   (Processor + Weaver core + two frozen contracts = clearly L+); gates green incl. `make verify-package-*`
   for any touched op DDL.
4. Revert the interim cap (§8.5) in the same change.
5. Commit direct to main, CI green.

Until then: the interim `maxretries=1` cap stays (duplication is stopped for lease-signing); other
packages' userTask gaps remain exposed to the 30-min duplication until this general fix lands.
