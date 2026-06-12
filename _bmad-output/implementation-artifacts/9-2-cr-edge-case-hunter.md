# Story 9.2 — Edge Case Hunter Review

Scope: uncommitted diff — `internal/weaver/` (engine.go, evaluator.go, state.go, health.go, reconciler.go new) + `internal/substrate/kv.go` (KVCreateWithTTL). Method: exhaustive path enumeration over the sweep/lane-1/TTL interleavings. Only UNHANDLED paths reported; everything walked-and-handled is in the ledger.

## Findings (unhandled edge cases)

### 1. [HIGH] Reclaim drop window: expired mark deleted, fresh mark never created — gap becomes permanently invisible to the sweep
- **Location:** `internal/weaver/reconciler.go:230-244` (delete-then-create ordering); failure modes inside `internal/weaver/evaluator.go:196-216` (`fireEpisode`)
- **Triggering sequence:** lease expires → sweep `reclaim` plans OK → `deleteMark` (revision-conditioned) **succeeds** → then ANY of: (a) process crash before `fireEpisode`, (b) `marks.get` KV error (→ NakWithDelay), (c) `marks.create` KV error (→ NakWithDelay). The mark key is now gone, no fresh mark exists, and no episode was published.
- **Why nothing recovers it:** the sweep enumerates **marks**, not rows (`pass` → `KVListKeys(weaver-state)`), so a markless open gap is invisible to every future pass. Lane-1 is a `DeliverLastPerSubject` durable that already acked the row — no redelivery arrives until the row's next CDC write, which for a stably-violating row may be never. The TTL backstop cannot help (the key is deleted). The Warn at `reconciler.go:241-242` ("retried at the fresh lease's expiry") is **only true for the publish-failure-after-create sub-case** (Nak from `fire`); for the get/create-error sub-cases it logs a retry that will not happen.
- **Consequence:** silent permanent loss of the re-dispatch — exactly the F5 lost-episode the reconciler exists to repair, reintroduced in a narrower window.
- **Guard sketch:** replace delete+create with an atomic revision-conditioned replace so the key is never absent: `newRev, err := e.conn.KVUpdate(ctx, bucket, key, freshMarkBody, markRev)` (then fire with `newRev` as the episode tag; conflict ⇒ fresh episode won, skip — same semantics as today's conditioned delete). Requires re-arming the per-key TTL on the update and relaxing the "mark is never updated after create" invariant in `markStore.get`'s doc (revision-as-episode-tag still holds: current revision = latest episode). Minimally: fix the `reconciler.go:241` log to distinguish "no fresh mark exists; gap unmarked until next row delivery".

### 2. [MEDIUM] Warm-up guard is one-pass-count, not registry-readiness: pass 2 can orphan-delete live targets' marks mid-replay
- **Location:** `internal/weaver/reconciler.go:48-52, 97-98, 188-198`; ordering at `internal/weaver/engine.go:233` (`go e.sweep.run(ctx)`) vs `engine.go:237` (`e.source.start(ctx)`)
- **Triggering sequence:** boot with expired-lease marks present (e.g. recovery after an outage longer than the lease — the precise disaster-recovery case) → sweep pass 1 starts **before** `source.start` and completes in milliseconds → `firstPassDone = true` → pass 2 fires at `SweepInterval` (default 1m, configurable to 1s) → registry replay (`IncludeHistory: true` over all `vtx.meta.>` history) is still in flight → `e.source.target(targetID)` returns `ok=false` for a genuinely-installed target → `warmedUp()` passes → mark deleted as `targetRemoved`, **no dispatch**.
- **Why nothing recovers it:** as in finding 1 — markless gap, sweep blind, lane-1 durable persists across the boot (no `DeliverLastPerSubject` replay; replay only happens when a durable is recreated), so no row redelivery for unchanged rows.
- **Consequence:** mass deletion of exactly the marks the disaster-recovery sweep was supposed to reclaim, proportional to replay lag vs sweep interval.
- **Guard sketch:** couple the orphan leg to source readiness instead of pass count — e.g. a `source.replayed()` signal (initial-history drained), or require the target to be absent across N≥2 consecutive passes (`missingSince[targetID]`), or gate on `time.Since(start) >= max(SweepInterval, replayBudget)`.

### 3. [LOW] `orphanColumn` leg has no warm-up/staleness guard at all: stale mid-replay playbook deletes a live gap's mark without dispatch
- **Location:** `internal/weaver/reconciler.go:200-207`
- **Triggering sequence:** registry replays full meta history (`registry.go:153`, `IncludeHistory: true`) → during replay the target is loaded at an **intermediate** definition that does not yet name `gapColumn` in `Gaps` (the gap was added in a later, not-yet-replayed revision) → sweep pass overlapping the replay finds the expired mark, `target()` ok, `Gaps[gapColumn]` missing → deleted as `orphanColumn`, no dispatch.
- **Consequence:** same invisibility as findings 1-2 (markless open gap), via a narrower window (expired lease + replay-lagged definition + sweep tick inside the window).
- **Guard sketch:** apply the same warm-up/readiness gate to the `orphanColumn` leg as to `targetRemoved` (it is warm-up-guarded today only by accident of the `installed` check ordering).

### 4. [LOW] `CorruptMark` health issue is set once and never cleared; alert text claims "deleted" before the delete is attempted
- **Location:** `internal/weaver/reconciler.go:250-262` (`deleteCorrupt`), `reconciler.go:354` (`issueKeySweep`)
- **Triggering sequence:** any corrupt mark → `alert(issueKeySweep(key), …, "…; deleted")` fires **before** `KVDeleteRevision`; (a) if the delete fails (non-conflict error), the issue text asserts a deletion that did not happen (retried next pass, so the text is eventually true — but the heartbeat lies in the interim); (b) once deleted, no code path ever calls `issues.clear(issueKeySweep(key))`, so the issue rides every heartbeat for the life of the process.
- **Consequence:** permanently-degraded heartbeat after a one-off corrupt entry; operator cannot tell a cleaned-up condition from an active one without restarting the Weaver.
- **Guard sketch:** alert after the delete outcome with accurate wording, and clear (or expire) the issue on a later pass that no longer sees the key — e.g. `defer`-style: on successful delete set an info-grade issue or schedule `e.issues.clear(issueKeySweep(key))` on the next completed pass.

## Checked-and-handled ledger

Walked and confirmed guarded — no report:

- **Sweep delete vs lane-1 CAS-create race:** every sweep delete is `KVDeleteRevision` at the revision read this pass (`reconciler.go:116, 254, 273`); a fresh CAS-create bumps the revision → conflict → skip (tested: `TestSweep_DeleteRevisionRace`).
- **TTL firing between sweep read and conditioned delete:** the NATS limit marker (and, after the 1s marker TTL, the empty subject) both fail the `LastRevision` condition → `ErrRevisionConflict` → skip. `IsRevisionConflict` covers typed sentinel + err_code 10071 text.
- **Sweep reclaiming a LIVE (slow) episode:** lease expiry ⇒ presumed-dead; re-fire is the documented §10.3 rare-double, Warn + `sweepReclaims` counter are the visibility. By design.
- **Sweep's fresh episode vs lane-1 fresh episode double-fire:** `fireEpisode(…, redelivered=false)` from the sweep — an in-flight mark found after the reclaim delete means a fresh episode won; Ack/skip (`evaluator.go:201-206`).
- **Two replicas / overlapping reclaims:** revision-conditioned delete makes exactly one winner; the loser's subsequent read either misses the key (skip) or sees a live fresh lease (leave). Within one instance, passes are serialized on a single goroutine and `time.Ticker` coalesces overruns.
- **Legacy lease-less marks:** `leaseLive("") == false` → reclaimed; replacement mark is well-formed with TTL (tested: `TestSweep_LegacyMarkReclaimed`). No immortality, no loop.
- **Garbage `leaseExpiresAt`:** parse failure reads as expired; the reclaim replaces it with a well-formed mark via revision-conditioned delete → terminates, no loop (`reconciler.go:314-329`).
- **Create-after-TTL-expiry during the 1s marker window:** nats.go v1.52.0 maps `Nats-Marker-Reason` messages to `ErrKeyDeleted` and `kv.Create` retries at the marker revision (`jetstream/kv.go:1074`); `KVGet` maps to not-found; `KVListKeys` filters markers. Lane-1 re-create over a marker succeeds.
- **`ttl <= 0` fallback in `KVCreateWithTTL`:** falls back to plain `KVCreate` (CAS preserved, tested). Engine can't reach it: `withDefaults` clamps `MarkLease` to ≥ 1s before `newMarkStore` (NewEngine calls `withDefaults` first), so the mark TTL is always ≥ 2s.
- **Sub-second / zero config values:** `MarkLease ≤ 0` → 30m default; `< 1s` → clamped to 1s (NATS floor); `SweepInterval ≤ 0` → 1m — so `time.NewTicker` never gets 0.
- **Tombstoned entity rows during sweep:** row key not found → `gapClosed` delete; empty row value (logical tombstone) → nil row → `boolColumn` false → `gapClosed` delete. Both legs mirror lane-1's level reconcile.
- **Unparseable row during sweep:** mark left, never deleted on unreadable evidence; bounded by lease/TTL (`reconciler.go:151-160`).
- **Plan failure mid-reclaim (unresolved ref / data error):** plan-before-delete ordering — mark left, shared planGap issue keys alert, retried each pass, bounded by the per-key TTL (tested: `TestSweep_PlanFailureLeavesMark`). The TTL-then-invisible tail of this is the documented backstop gap ("unwedges but cannot re-attempt", `state.go` markTTLBackstopFactor comment).
- **Missing `entityKey` on a violating row at reclaim:** alert + leave (deleting would blind the sweep) — explicit and correct (`reconciler.go:209-218`).
- **TTL = 2× lease strictly greater than lease:** constant factor 2, so the sweep's reclaim window (key exists past `leaseExpiresAt`) is structurally guaranteed; per-pass clock read (`time.Now()` at `reconciler.go:170`).
- **Clock skew:** sweeper-clock-behind or creator-clock-ahead > lease degrades to the documented TTL-backstop gap (unwedge without re-attempt); skew < lease only shifts reclaim timing inside the TTL window. Accepted §10.3 posture.
- **Mark key shape garbage:** `splitMarkKey` positional split + NanoID + single-token validation (no dots possible in either token) → corrupt-delete leg, revision-conditioned (tested: `TestSweep_CorruptMark`).
- **Stop / ctx-cancel during a pass:** per-key `ctx.Err()` check; failed list keeps `firstPassDone` down; cancelled-ctx KV calls degrade to logged Warns.
- **Heartbeat counter races:** `bump`/`metrics`/`warmedUp`/`firstPassDone`/`lastRunAt` all under `s.mu`; heartbeater nil-checks `h.sweep`.
- **`expectedRevision` 0 in the sweep path:** `rowEntry.Revision` from a successful `KVGet` is never 0 (lane-1's `msg.Sequence == 0` guard has no sweep analogue needed).
- **Lane-1 `NumDelivered != 1` refactor:** semantics identical to the prior `== 1` branch inversion, including the NumDelivered-0-counts-as-redelivery posture (comment preserved).
- **Stale-row episode from a sweep racing a row update:** the op payload carries the rowRevision read this pass as the OCC condition; the stale episode is rejected downstream and the (then-stale) fresh mark is cleared by lane-1's level reconcile or the next sweep's `gapClosed` leg; worst case bounded by the lease — the lease being the recovery is the design.
