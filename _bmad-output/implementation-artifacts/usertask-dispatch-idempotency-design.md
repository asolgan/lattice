# Design — userTask dispatch idempotency (stop duplicate human tasks)

**Status:** 📐 Proposal — awaiting Andrew ratification on the §10.3 amendment; the rest is
Winston-ratified and build-ready once the amendment lands.
**Author:** Winston (Steward, 2026-06-25). **Owner area:** Weaver core (reconciler/strategist) + the
`CreateTask` / `StartLoomPattern` ops. **Frozen-contract touch:** Contract #10 §10.3 (amendment text in
§7 below — Andrew's call).

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

### 4.3 The act — a reconciler check-before-act probe (this *is* §10.3's "check-before-act")

Deterministic identity tells us *which* task a re-dispatch would (re)create; the reclaim then **checks
before acting**. In the reconciler `reclaim`, for a userTask action, Weaver derives the deterministic key
and **GETs it from Core KV before re-dispatching**:

- **present-and-open** → the task still exists (the human simply hasn't acted) → **renew the mark and
  skip the dispatch** — no op submitted.
- **absent / tombstoned** → the task was completed-and-the-gap-reopened, or lost out-of-band → **re-dispatch**.

This is the literal §10.3 *"robust check-before-act variant"*, and it has direct precedent: Loom's
own §10.6 userTask creation-deadline already *"GETs the task vertex `vtx.task.<taskId>` from Core KV …
present → disarm … the legitimate unbounded human wait."* Weaver's reconciler doing the same for its
re-dispatch is consistent (and Weaver is a platform component, so the Core-KV read is sanctioned).

**Why the probe, not an "idempotent op via reads":** the obvious alternative — have `CreateTask` declare
`vtx.task.<taskId>` in its reads and no-op if present — is **blocked by the reads-miss-is-fatal rule**: on
the *first* dispatch the task key is absent, and a declared read of an absent key is a fatal
HydrationMiss (the same constraint the identity-domain create-or-skip / large-file CC5 pattern works
around by *pre-reading and conditionally declaring*). The reconciler probe sidesteps it cleanly: when the
task is present we **never submit the op at all**, so there is no absent-key read and no rejected-op noise.
The `CreateTask`/`StartLoomPattern` `CreateOnly` on the deterministic key **stays** as a belt-and-suspenders
guard against a lane-1/reclaim race, but it is never the steady-state path.

The per-episode `requestId` **stays `markRevision`-derived** so *within*-episode redelivery still
collapses at the Contract #4 tracker (unchanged §10.6 crash-safety); the reconciler probe handles
*across*-episode reclaim. Two clean, independent layers.

> **Scope of the probe:** only the reconciler **reclaim** path re-dispatches an open gap (the lane-1 CDC
> path is gated by the mark's CAS-create anti-storm guard — while the mark exists, lane-1 never
> re-dispatches; §10.8). So the probe lives in `reclaim` alone; lane-1's *first* dispatch is unchanged
> except that it now mints `claimId` and derives the deterministic id.

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

| Property | `maxretries=1` (interim cap) | blind `(t,e,g)` id, no probe | **`claimId` id + reconciler probe (this design)** |
|---|---|---|---|
| No duplicate while task open | ✅ | ✅ | ✅ |
| Re-create after legit close→reopen | ✅ (count resets) | ❌ collapses on stale task | ✅ fresh `claimId` ⇒ fresh task |
| Self-heal a task lost out-of-band | ❌ never re-creates | ⚠️ depends | ✅ absent on probe ⇒ re-dispatch |
| No rejected-op noise per lease | ✅ (suppressed) | ❌ CreateOnly reject | ✅ present ⇒ op never submitted |
| General (all packages) | ❌ per-package columns | ✅ | ✅ |

The interim cap is create-once-*forever* (never recovers a lost task) and is per-package; this design is
reopen-correct, self-healing, and general. **The cap becomes redundant and is reverted as part of this
fix** (the `maxretries_onboarding`/`maxretries_signature` columns + the `retry_budget.go` constants).

---

## 7. Contract impact — §10.3 amendment (Andrew ratification)

Proposed replacement for the §10.3 *"Re-fire after lease expiry — idempotency by action"* bullet (do
**not** edit the frozen file until ratified):

> **Re-fire after lease expiry — check-before-act by deterministic episode identity.** A userTask
> reclaim is keyed by the **open-episode identity**: the mark's `claimId` (minted at the mark's CAS-create,
> **preserved** across reclaims) seeds the dispatched artifact's id — `assignTask`'s `taskId` and
> `triggerLoom`'s Loom `instanceId`. Before re-dispatching, the reconciler **GETs that artifact** from Core
> KV (as Loom's §10.6 deadline already does): **present-and-open → renew the mark and skip** (no op
> submitted); **absent/tombstoned → re-dispatch**. A legitimate close→reopen mints a new mark ⇒ new
> `claimId` ⇒ a fresh artifact; an out-of-band deletion self-heals (absent ⇒ re-dispatch). The dispatched
> op's `CreateOnly` on the deterministic key remains a belt-and-suspenders race guard. The within-episode
> `requestId` stays revision-derived (Contract #4 tracker collapse for redelivery, §10.6). This
> **supersedes** the prior "accepted rare double / check-before-act = Phase-3 hardening" disposition for the
> two human userTask actions; external gaps retain episode-scoped (per-reclaim) dispatch, bounded by
> `inflight_<g>` + `maxretries_<g>`.

Also: §10.3 mark value-shape note — `claimId` regains a **producer** (CAS-create) and a **consumer** (id
derivation); strike *"left optional … no remaining producer."*

This is the only frozen-contract change. Everything in §4–§6 is Winston-ratified and build-ready the
moment the amendment lands.

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
3. **`StartLoomPattern` honours `instanceId`** — accept an optional caller-supplied `instanceId` (verbatim
   if present, minted if absent), mirroring `CreateTask`'s `taskId` seam, so the reconciler can probe a
   predictable `instance.<instanceId>` and a stray re-trigger still `CreateOnly`-collapses.
4. **Reconciler check-before-act probe** — in `reclaim` ([reconciler.go:231](../../internal/weaver/reconciler.go)),
   for `assignTask`/`triggerLoom`, GET the deterministic `vtx.task.<taskId>` / `instance.<instanceId>` from
   Core KV before re-dispatching: **present-and-open → re-arm the mark and return (no op)**; **absent →
   proceed to dispatch**. No Starlark/op `reads` change (sidesteps reads-miss-is-fatal); the ops' existing
   `CreateOnly` stays as the race backstop. (Lane-1's first dispatch is untouched beyond minting `claimId`
   + the deterministic id — the mark CAS-create already blocks lane-1 re-dispatch, §10.8.)
5. **Revert the interim cap** — drop `maxretries_onboarding`/`maxretries_signature` from
   `packages/lease-signing/{lenses.go,retry_budget.go}` and the `UserTaskDispatchCaps` test.

## 9. Test plan

- **Weaver unit:** a reclaim of a userTask gap with a preserved `claimId` derives the **same** `taskId`
  (vs `markRevision` deriving a different one today); the reclaim op is idempotent (present → no-op).
- **`claimId` lifecycle:** minted on CAS-create, preserved across `replace`, gone on gap-close
  (`clearClosedMarks`); a fresh mark after reopen mints a **new** `claimId` (fresh task id).
- **Reconciler probe:** a reclaim whose deterministic task/instance key is **present-and-open** re-arms
  the mark and submits **no** op; one whose key is **absent/tombstoned** re-dispatches. (Pure unit test
  with a stubbed Core-KV GET — no wall-clock.)
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
- **Probe altitude (reconciler, not op).** Forced by the reads-miss-is-fatal rule (§4.3): an op cannot
  unconditionally declare a maybe-absent task key in its `reads`. The reconciler probe avoids it and is the
  §10.3-named "check-before-act"; Loom's §10.6 deadline is the precedent for a platform component GETting
  the task vertex. It costs one Core-KV GET per *open-userTask* reclaim (rare — only after a 30-min lease,
  only while a human hasn't acted), well within the sweep's existing read budget.
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

## 11. Sequencing

1. Andrew ratifies the §7 §10.3 amendment (or redirects the approach).
2. Apply the amendment to `docs/contracts/10-orchestration-surfaces.md` (Andrew's edit / on his nod).
3. Build §8 in Weaver core + the two ops; party + 3-layer adversarial review; gates green.
4. Revert the interim cap (§8.5) in the same change.
5. Commit direct to main, CI green.

Until then: the interim `maxretries=1` cap stays (duplication is stopped for lease-signing); other
packages' userTask gaps remain exposed to the 30-min duplication until this general fix lands.
