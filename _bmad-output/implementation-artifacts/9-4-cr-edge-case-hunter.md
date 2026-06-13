# Story 9.4 — Edge Case Hunter review (Weaver control-API / CLI)

Method: every branching path + boundary condition in the uncommitted 9.4 diff walked with full
repo context. Scope: `internal/weaver/`, `internal/weaver/control/`, `cmd/lattice/weaver/`,
`cmd/weaver/`. Reported below are **unhandled** cases only; a checked-and-handled ledger follows.

Severity key: **High** = incorrect behavior an operator can hit on a normal path; **Medium** =
correctness gap on a plausible but narrower sequence; **Low** = observability / cosmetic / very
narrow window.

---

## Unhandled cases

### 1. [Medium] Disabled-then-uninstalled target leaks its `__control` marker forever; re-install silently starts disabled
`internal/weaver/control.go:39` (`seedDisabledTargets`), `internal/weaver/engine.go:435-453`
(`reconcileConsumers` removal branch), `internal/weaver/state.go:206` (`setDisabled`).

Sequence:
1. Operator `disable t1` → `__control` marker written, in-memory set has `t1`.
2. Operator retires t1's Lens (the op-path the story explicitly defers to: `lattice lens
   deactivate` / `TombstoneMetaVertex`). `targetSource` drops t1; the next `reconcileConsumers`
   pass hits the removal branch (engine.go:435) — it `Remove`s the consumer and deletes the
   health-sink entry, but **nothing deletes `t1.__control`**.

Nothing in the diff or the pre-existing code ever deletes a `__control` marker on a genuine
uninstall (only `Enable` and `Revoke`'s prefix-delete touch it, and `Revoke` re-writes it). The
in-memory set entry also never gets pruned — `seedDisabledTargets` is add-only and runs once at
`Start`. Consequences:
- The marker is a permanent orphan in `weaver-state` (re-seeded into the in-memory set on every
  restart).
- If `t1` is ever **re-installed** (a fresh `meta.weaverTarget` with the same targetId), it
  silently comes up **disabled** — a fresh install that an operator did not disable. The
  operator gets no signal except `list` showing `disabled`, and `disable`/`enable` only work
  once the target is registered again, so recovery is non-obvious.

The story's AC #4 only reasons about the *revoke*-then-still-registered re-add window; it never
addresses disable-then-uninstall. Revoke's deliberate "stays disabled on re-add" design (Q3)
makes the same orphan even more likely via the revoke path. Recommend: the `reconcileConsumers`
removal branch (or a heartbeat-cadence sweep) should delete the `__control` marker + prune the
in-memory set when a target leaves `targetSource`, OR the story should explicitly document this
as an accepted leak with the recovery procedure (operator must `revoke`→`enable`, or clear the
KV key out of band).

### 2. [Low] `countInFlight` (the `marksInFlight` health gauge) counts `__control` markers as in-flight marks
`internal/weaver/state.go:174-180` (`countInFlight`), consumed at `internal/weaver/health.go:216`.

The 9.2 sweep was correctly taught to skip `__control` (reconciler.go:120), but `countInFlight`
is a raw `len(KVListKeys(weaver-state))` and was **not**. Every disabled or revoked target adds
one permanent `__control` key, so the heartbeat's `marksInFlight` metric over-reports the true
in-flight-dispatch count by exactly the number of disabled/revoked targets. On a system where an
operator disables several targets, this gauge (an anti-storm signal) reads persistently non-zero
even with zero real dispatch in flight. Same class of bug as #1's metric inflation. Fix:
`countInFlight` should filter `controlKeySuffix` the same way the sweep does.

### 3. [Medium] Crash between `Enable`'s `Resume` and marker-delete leaves a "half-enabled" target: consumer running, dispatch silently suppressed
`internal/weaver/control.go:129-140` (`Enable`).

`Enable` does, in order: `supervisor.Resume` (persists `active` durably via `persistActive`,
`consumer_supervisor.go:236`) → `marks.setDisabled(false)` (deletes `__control`) → in-memory
clear. If the process dies after `Resume` persisted but before `setDisabled(false)`:
- On restart, `supervisor.Add`→`restoreState` sees no PauseManual → **lane-1 resumes delivering
  rows**.
- But `seedDisabledTargets` reads the surviving `__control` marker → in-memory set has the
  target → `handleRow`'s dispatch-skip guard (evaluator.go:65) **Acks every row without
  dispatching**.

Result: the consumer pumps rows but produces no ops, and `list` reports `disabled` while the
operator believes they enabled it (their `enable` either errored or never returned a success).
Re-running `enable` recovers it, but there is no self-healing and no alert. The lane-1 pause and
the dispatch-skip marker are two independently-persisted truths that `Enable`/`Disable` write
non-atomically; only the happy path keeps them in sync.

### 4. [Medium] Symmetric crash window in `Disable`: `Pause` persisted but marker not written → lane-3 keeps acting on a "disabled" target after restart
`internal/weaver/control.go:110-121` (`Disable`).

Mirror of #3. `Disable` does `Pause` (persists `pausedManual`) → `setDisabled(true)` → in-memory
set. Crash after `Pause` persisted, before the marker write:
- On restart, lane-1 is paused (correct, no rows).
- But `__control` is absent → `seedDisabledTargets` leaves the in-memory set empty → the
  `handleFiredTimer` (temporal.go:215) and `scheduleFreshness` (temporal.go:95) guards do **not**
  fire. A freshness timer armed before the disable fires on the shared lane-3 consumer and
  **submits a `MarkExpired` op for a target the operator disabled**.

So a crash at the wrong instant defeats exactly the lane-3 protection the story added the
`__control` marker to provide (Dev Notes "Why lane-3 needs its own skip"). The operator sees
`list` → `active` (no marker) and may not realize the disable half-applied. Recommend writing the
marker BEFORE `Pause` in `Disable` (marker-first is the safe order: a marker without a pause is
inert-correct since lane-1 dispatch is also guarded by the marker; a pause without a marker is
the dangerous half), and deleting the marker AFTER `Resume` in `Enable`.

### 5. [Medium] Skipped freshness timer is silently lost across disable→enable when the row is not re-touched
`internal/weaver/temporal.go:208-219` (`handleFiredTimer` disabled-skip) +
`internal/weaver/temporal.go:103-110` (`scheduleFreshness` only schedules from a delivered row).

A timer armed before `disable` fires while disabled → `handleFiredTimer` Acks and drops it
(no `MarkExpired`, schedule message consumed, no redelivery). The code comment (temporal.go:213)
asserts recovery: "the next row delivery re-arms the @at timer." But lane-1 is
`DeliverLastPerSubject` and `Resume` does **not** replay already-acked messages — it continues
the iterator from the pause point. So if the entity's `weaver-targets` row is **not touched again
after enable** (no new CDC event), `scheduleFreshness` never re-runs, the past-deadline timer is
never re-armed, and the freshness expiry is **silently never converted**. The freshness violation
that should have produced a `MarkExpired` simply never surfaces until the next unrelated CDC touch
of that entity (which may be never for a quiescent entity).

The "fires immediately on a past @at" behavior (temporal.go:145-150) does make recovery correct
**if** a delivery happens — so the gap is precisely the no-redelivery case. This is the
strictest reading of the lead's Q4 ruling (handleFiredTimer skips while disabled) interacting
with `Resume`'s non-replaying semantics. Recommend either documenting this as an accepted
missed-expiry bound, or having `Enable` force a one-shot lane-1 replay / re-seed so deadlines that
elapsed during the disabled window are re-evaluated.

### 6. [Low] CLI `disable`/`enable`/`revoke` of a target whose id contains a `.` (or other multi-token text) hangs to timeout with an opaque "no responders", never a clean "invalid target"
`cmd/lattice/weaver/weaver.go:34-57` (`request`), `internal/weaver/control/service.go:238-250`
(`targetIDFromSubject`), built subject from `TargetSubject` (service.go:260).

`TargetSubject("a.b", "disable")` = `lattice.ctrl.weaver.a.b.disable` (6 tokens). The wildcard
endpoint subscribes `lattice.ctrl.weaver.*.disable` where `*` matches exactly one token, so the
6-token subject matches **no** endpoint → no responder → the CLI blocks until
`output.DefaultTimeout` and surfaces a generic `no responders`/timeout error, not "invalid target
id". `targetIDFromSubject`'s `len(parts) != 5` guard is therefore unreachable for this input (the
message never routes to it). Target ids are documented as NanoIDs (no dots), so this is a
malformed-operator-input boundary, not a normal path — but the failure mode (silent hang +
opaque error) is poor. A `.`/whitespace/empty-arg validation in the CLI before publishing would
turn it into an immediate clear error. (Empty arg is already blocked by `cobra.ExactArgs(1)`; a
literal empty string `disable ""` still builds `lattice.ctrl.weaver..disable` and times out the
same way.)

---

## Checked-and-handled ledger (walked, found correctly handled)

- **Seed-vs-first-delivery race (terrain #1a).** `seedDisabledTargets` runs at `engine.go:314`,
  synchronously BEFORE the temporal consumer `Add` (323) and before `source.start`/`reconcile`
  (329-332) bring up any lane-1/lane-3 pump. No consumer delivers a row before the seed
  completes — no race at startup. ✔
- **Restart while disabled — pause + marker agree (terrain #1b, happy path).** `Disable` persists
  both the lane-1 PauseManual (via `persistPaused`, restored by `restoreState`
  consumer_supervisor_pump.go:374) and the `__control` marker (re-seeded by `seedDisabledTargets`).
  On a clean (non-crash) restart both restore and agree. (The crash-window divergence is #3/#4.) ✔
- **`__control` vs 9.2 reconciler sweep (terrain #2).** `sweeper.pass` skips
  `controlKeySuffix`-suffixed keys (reconciler.go:120) before `sweepMark`/`splitMarkKey`, so the
  marker is never treated as orphan/corrupt/level-clear. `TestSweep_ControlMarkerSurvives`
  asserts it. ✔
- **`__control` vs 9.1 level-clearing in handleRow.** The dispatch-skip guard (evaluator.go:65)
  is placed AFTER `clearClosedMarks` (evaluator.go:52), so a disabled target's resolved gaps
  still clear their `<entityId>.<gap>` marks; the guard only short-circuits NEW dispatch. The
  `__control` key itself is never a `<targetId>.<entityId>.<gap>` mark, so `clearClosedMarks`'
  candidate-set logic never touches it. `TestHandleRow_DisabledSkipsDispatchButClearsMarks`
  covers both halves. ✔
- **`__control` key-shape collision.** `controlKey` = `<targetId>.__control` (exactly one dot vs a
  mark's two); `substrate.Alphabet` has no `_`, so `__control` is unproducible by
  `NewNanoID()`. Asserted in `state_internal_test.go`. ✔
- **`deleteByTargetPrefix` numeric-prefix overlap (terrain #2/#4).** The `targetID + "."` prefix
  means `t1.` never matches `t10.`'s keys; `TestDeleteByTargetPrefix_OnlyMatchesOwnTarget`
  proves it. ErrKeyNotFound mid-scan tolerated. ✔
- **Revoke re-add stays inert (terrain #3).** `Revoke` re-writes the `__control` marker AFTER the
  prefix-delete (control.go:186-193), so a `reconcileConsumers` re-add of a still-registered
  target's consumer comes up dispatch-skipped. `TestRevoke_RemovesDurableMarksAndStaysDisabled`
  and `TestHandleRow_RevokeRemovesDurableAndConsumerGone` confirm. (The re-added consumer is NOT
  re-paused — but the marker keeps dispatch inert, so the only residue is lane-1 pumping rows
  that all Ack-skip; harmless, matches the design.) ✔
- **Revoke idempotent on unknown target.** `supervisor.Remove` is a no-op-if-unmanaged
  (consumer_supervisor.go:110), `deleteByTargetPrefix` tolerates absence, and Revoke writes the
  marker anyway. `TestRevoke_NotRegistered_NoError` covers it. ✔
- **Disable/in-flight CAS-marked episode (terrain #4).** Disable does not touch existing
  `<entityId>.<gap>` marks; an already-CAS-marked + published episode completes normally (the op
  was already submitted; the mark's lease/TTL governs it). Disable only stops NEW
  mark-create/dispatch on subsequent deliveries. No wedge. ✔
- **Enable after disable — marks still present.** Enable clears only the `__control` marker +
  resumes; pre-existing in-flight `<entityId>.<gap>` marks are untouched and resume their normal
  lease/sweep lifecycle. `TestHandleRow_EnableResumesDispatch` shows dispatch resuming. ✔
- **`scheduleFreshness` belt vs handleRow guard.** handleRow Acks before calling
  `scheduleFreshness` for a disabled target (evaluator.go:65 precedes :73), so
  scheduleFreshness's own guard (temporal.go:95) is a defensive belt, not a live path on lane-1 —
  correctly a belt, not a contradiction. ✔
- **`list` exact-subject vs wildcard collision (terrain #6).** `list` registers on the 4-token
  `lattice.ctrl.weaver.list`; disable/enable/revoke on 5-token `lattice.ctrl.weaver.*.<op>`.
  Token counts differ, so no overlap. A target literally named `list` → `disable list` builds
  `lattice.ctrl.weaver.list.disable` (5 tokens, routes to the disable endpoint, targetID="list")
  — correct. ✔
- **Concurrent `list` during a reconcile.** `ListTargets` takes no `e.mu`; it reads
  `targetSource.targetIDs()`/`target()` (mu-guarded) + the RWMutex-guarded `disabledTargetSet`.
  The targetIDs()/target() TOCTOU (id removed between the two calls) is handled by the explicit
  `if !ok { continue }` (control.go:73). No lock-ordering conflict with `reconcileConsumers`'
  `e.mu`. ✔
- **Disable of an already-disabled / Enable of an already-enabled target.** `supervisor.Pause`
  is idempotent (PauseManual add); `setDisabled(false)` treats missing key as success. Re-issuing
  either op is a clean no-op-equivalent. ✔
- **Unknown op subject.** No endpoint registered → request times out at the client (CLI surfaces
  a bounded-timeout error, never a hang past `DefaultTimeout`). `TestControl_UnknownOp` +
  `dispatchEndpoint`'s `default` branch (unreachable, but present). ✔
- **CLI connection error / no Weaver running.** `request` returns the connect/request error;
  RunE maps it to `output.PrintJSONError("ControlError", …)` under `--output json` or a plain
  error otherwise, bounded by `output.DefaultTimeout`. `TestWeaverDisable_NotRegistered_JSON`
  covers the engine-error path. ✔
- **boundary_test.go untouched.** `internal/weaver/control` is a sibling package; `internal/weaver`
  does not import it; the `micro`/`nats` imports live only in the new package + `cmd/*`. ✔
