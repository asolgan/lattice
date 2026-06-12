# Story 9.2 — Blind Hunter (adversarial, diff-only) findings

Reviewer: Blind Hunter (`bmad-review-adversarial-general`), diff-only discipline.
Evidence: `git status --porcelain`, `git diff` (tracked scope files), full content of the two
untracked files (`internal/weaver/reconciler.go`, `internal/weaver/reconciler_internal_test.go`).
No pre-existing repo files, contracts, or the story file were read. Verification runs:
`go build ./...` clean; `go test ./internal/weaver -run 'TestSweep_|TestMarkCreate_TTLBackstop'`
and both new e2e tests pass.

Scope reviewed: `internal/weaver/{reconciler.go,state.go,engine.go,evaluator.go,health.go}` +
tests, `internal/substrate/kv.go` + `substrate_test.go`, `docs/components/weaver.md`.

---

## Finding 1 — [MEDIUM] Crash window in the sweep reclaim strands a gap with NO backstop

`internal/weaver/reconciler.go:230-244` (`reclaim`): the reclaim is **delete-then-recreate** —
`deleteMark` (revision-conditioned delete of the expired mark) followed by `fireEpisode`
(fresh CAS-create + publish).

Failure scenario: the process dies (or ctx is cancelled at engine shutdown — there is no ctx
check between the two calls) after `deleteMark` succeeds and before `fireEpisode` creates the
fresh mark. Result state: **no mark, no per-key TTL, no op published**, and the row's last
delivery was already acked on the per-target durable. Nothing re-attempts the gap: the sweep
enumerates marks (the mark is gone), the durable will not redeliver an acked message, and a
restart does not replay (durable consumer). The gap is stranded until an unrelated row
re-upsert or a target-spec reset.

This is strictly worse than the pre-lease wedge this story eliminates: that wedge left a mark
the new sweep/TTL bounds; this window leaves zero trace and is unbounded. An update-in-place
lease refresh (or any ordering that keeps a mark standing at all times while the column is
true) would not have the hole. Rare (crash inside a two-KV-op window on the reclaim path),
but the impact is the story's own acceptance failure mode.

## Finding 2 — [MEDIUM] Race: lane-1 redelivery vs sweep reclaim fires two distinct episodes

`internal/weaver/evaluator.go` (`fireEpisode`, redelivery branch: `return e.fire(ctx,
targetID, entityID, col, markRev, pl)`) vs `internal/weaver/reconciler.go:230-244`.

The lane-1 redelivery path re-fires using `markRev` read by `marks.get` **without checking the
lease**, and that read is not revision-fenced against the sweep. Interleaving:

1. Lane-1 redelivery (`NumDelivered != 1`) reads the mark — lease already expired — gets
   `markRev_old`, `inFlight=true`.
2. Sweep reclaims: revision-conditioned delete of `markRev_old` succeeds, fresh CAS-create
   `markRev_new`, publishes `requestId_new`.
3. Lane-1 handler proceeds and publishes `requestId_old` (derived from `markRev_old`).

Two **different** requestIds for the same gap → Contract #4 collapsing does not dedupe →
double `triggerLoom`/`assignTask`/`directOp`. If the original publish was genuinely lost (the
exact case the redelivery branch exists for), both ops are real actions. The doc/comments
frame the rare-double as "lease expired while the episode was still alive"; this variant is a
new race created by delete-then-recreate, and is cheaply avoidable: the redelivery branch
could decline to re-fire (or re-read/fence) once `leaseLive(rec.LeaseExpiresAt, now)` is
false, ceding expired marks to the sweep.

## Finding 3 — [LOW] Warm-up guard is a pass-count proxy, not a registry-replay signal

`internal/weaver/reconciler.go:52, 84-101, 190-193`: `firstPassDone` flips after one completed
enumeration of `weaver-state` — on a small/empty bucket that is milliseconds after boot, long
before the async `meta.weaverTarget` replay can have landed. From pass 2 (at `SweepInterval`,
which is config-tunable down to sub-second — the e2e test itself runs at 300ms), an expired
mark whose target IS installed but not yet replayed is deleted as `targetRemoved` **without
dispatch**. Consequence is bounded (the target's consumer replays the row via
DeliverLastPerSubject once the registry lands, and dispatch then proceeds unshadowed) but the
guard's comment ("the first completed pass must not mass-delete live targets' marks")
oversells what one fast empty pass + one short interval actually guarantees. A minimum
wall-clock warm-up (or gating on the source's first registry callback) would make the guard
real.

## Finding 4 — [LOW] No config validation keeps the re-attempt window reachable (MarkLease vs SweepInterval)

`internal/weaver/engine.go` (`withDefaults`) enforces only `MarkLease >= 1s`;
`internal/weaver/reconciler.go` / `state.go:markTTLBackstopFactor` promise the sweep "must
observe an expired lease while the key still exists (TTL = 2 × lease **guarantees** that
window)". The guarantee only holds when the lease exceeds the sweep cadence. Configure
`MarkLease = 5s` with the default `SweepInterval = 1m` (both independently legal): every
expired mark is TTL-deleted at 10s, no sweep ever sees it, and the expired-lease re-attempt
leg is silently unreachable — every crash recovery degrades to the "unwedge but never
re-attempt" TTL path. `withDefaults` should reject/clamp `MarkLease < SweepInterval` (or at
least Warn loudly).

## Finding 5 — [LOW] Sweep reclaim gates on the gap column only — no `violating` check (possible divergence from lane-1)

`internal/weaver/reconciler.go:162-168`: the sweep's dispatch predicate is
`e.boolColumn(targetID, row, gapColumn)` alone. Every test fixture treats the row's
`violating` field as meaningful (set `true` on dispatchable rows, `false` on cleared ones),
and the docs describe lane-1 as violation-driven. If the lane-1 row handler additionally gates
on `violating == true` (outside this diff — unverifiable under diff-only), a row carrying
`violating: false` with a stale `missing_x: true` would never dispatch via lane 1 but WOULD be
reclaimed and re-dispatched by the sweep — an op fired for a non-violating row. If the Lens
contract makes `missing_* == true ⇒ violating`, this is moot; confirm and either mirror the
lane-1 gate or document the invariant at the sweep site.

## Finding 6 — [LOW] `deleteCorrupt` raises a "deleted" alert before (and regardless of) the delete succeeding

`internal/weaver/reconciler.go:250-262`: the Health issue text `"…is corrupt (…); deleted"` is
written **before** `KVDeleteRevision`. On a revision conflict the function returns silently and
on any other error it only Warn-logs — in both cases the operator-visible issue claims a
deletion that did not happen, and `sweepCorrupt` is (correctly) not bumped, so the issue and
the counter disagree. Move the alert after a successful delete, or word it as
"corrupt (…); deleting".

## Finding 7 — [LOW] entityKey-missing reclaim leg can loop a legacy mark forever, and its issue key collides across entities

`internal/weaver/reconciler.go:209-218`: when the row lacks `entityKey`, the expired mark is
deliberately left. For a 9.2 mark the per-key TTL bounds this; for a **legacy / TTL-less**
mark (the shape the diff's own tests construct via plain `KVCreate`, and the pre-9.2
production shape) nothing ever removes it — every pass re-alerts and the mark is immortal,
contradicting the `leaseLive` comment's "a lease-less mark … would otherwise be immortal"
rationale (that path only cures it when `entityKey` IS present). Additionally the issue key
`issueKeyData(targetID, "entityKey")` is per-target, not per-entity: multiple bad rows under
one target collapse into one issue, last-writer-wins on the message.

## Finding 8 — [INFO] Production weaver-state bucket must be LimitMarkerTTL-provisioned — not visible in this diff

`internal/substrate/kv.go:KVCreateWithTTL` requires the bucket be provisioned with
`LimitMarkerTTL`; the diff fixes the **internal test harness** bucket
(`evaluator_internal_test.go`) but contains no production provisioning change. The unchanged
e2e `provision` helper passes with TTL writes (verified by running both new e2e tests), so the
shared provisioning path appears already armed — but if any deployed environment's
`weaver-state` bucket predates `LimitMarkerTTL`, **every** mark CAS-create now errors at
runtime (lane-1 NakWithDelay loop). Worth a one-line check outside this review's diff-only
scope that bootstrap re-provisioning updates existing buckets.

---

No findings in: `internal/substrate/kv.go` TTL-create itself (CAS/tombstone semantics and the
`ttl<=0` fallback are sound and tested); sweep delete revision-conditioning (correct
everywhere, including the corrupt path); sweeper goroutine lifecycle vs ticker (single
goroutine, no overlapping passes, ctx checked per key); heartbeat counter plumbing (mutex use
is correct); `splitMarkKey` (positional split is safe given dot-free validated tokens and the
NanoID check); engine 1s lease floor vs the NATS TTL floor (TTL ≥ 2s holds).
