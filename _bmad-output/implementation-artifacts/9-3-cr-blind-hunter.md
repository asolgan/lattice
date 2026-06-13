# Story 9.3 — Blind Hunter (adversarial, diff-only) findings

Reviewer: Blind Hunter (`bmad-review-adversarial-general`), diff-only posture.
Evidence: `git status --porcelain`, `git diff` of tracked files, full content of the four
untracked files (`internal/weaver/temporal.go`, `internal/weaver/export_test.go`,
`internal/weaver/temporal_internal_test.go`, `internal/substrate/publish_schedule_test.go`).
No pre-existing files, contracts, or the story file were read. Findings are standalone-bug
hunting only; anything requiring repo context to confirm is flagged as such.

---

## Findings

### 1. MEDIUM — Past-deadline guard tests the untruncated instant but publishes the truncated one

`internal/weaver/temporal.go:124-134`

```go
fireAtStr := fireAt.UTC().Truncate(time.Second).Format(time.RFC3339)
if !fireAt.After(time.Now()) { ... skip ... }
```

The guard compares the **untruncated** `fireAt` against `now`, but the `@at` header is built
from the **truncated** `fireAtStr`. Whenever `fireAt` and `now` fall in the same second with
`fireAt`'s sub-second fraction ahead of `now`'s (e.g. `now = T.900`, `fireAt = T.950`), the
guard passes while the published instant `T` is up to ~1s in the past. The code's own comment
says "a past @at risks a publish-reject Nak loop" — this is exactly the window the guard was
written to close, and it leaks through. Failure scenario: a Lens projecting near-now deadlines
(or a slow consumer catching up on backlog) publishes a past `@at`; if the server rejects past
schedules the row is Nak'd-with-delay and burns one or more redelivery cycles before the
untruncated comparison finally flips to "past" and skips. Fix is one token: compare the
truncated instant (`fireAt.Truncate(time.Second).After(time.Now())` or parse-back of
`fireAtStr`).

### 2. MEDIUM — `handleFiredTimer` uses plain `Nak` (no delay) on op-publish failure: hot redelivery loop

`internal/weaver/temporal.go:199-203`

```go
if err := e.act.submit(ctx, requestID, opMarkExpired, payload, ""); err != nil {
    ...
    return substrate.Nak
}
```

The scheduling leg deliberately returns `NakWithDelay` and documents it as "bounded cadence,
never a hot loop" (`temporal.go:82-84`); the fired→op leg returns bare `substrate.Nak` for the
same failure class (publish to the ops stream failed). Failure scenario: `core-operations` is
unavailable for a stretch — every pending fired message is redelivered immediately, Nak'd
immediately, redelivered again: a tight loop hammering both the consumer and the down stream,
multiplied by the number of fired-but-unconverted timers. Unless the `weaver-temporal` consumer
config carries server-side backoff (not visible in this diff — `temporalSpec` sets none), this
is an unbounded spin. Should be `NakWithDelay` for symmetry with every other retryable path in
the diff (`evaluator.go` uses `NakWithDelay` at all 8 of its retry sites).

### 3. MEDIUM — `RowDataError` issue is keyed per target+column but cleared by ANY healthy row: the alert flickers off while the bad row persists

`internal/weaver/temporal.go:100, 117, 120`

Both data-error branches (non-RFC3339 `freshUntil`, missing `entityKey`) set the issue under
the shared key `issueKeyData(targetID, freshUntilColumn)`, and the success path
unconditionally clears that same key. The bad row **Acks** (no redelivery), so the issue is
set exactly once per CDC touch of the bad row. Failure scenario: a target with 1 corrupt row
and N healthy rows — the very next healthy delivery (typically milliseconds later) clears the
issue, and it stays cleared until the corrupt row is next touched by CDC, which for a stable
row may be never. The operator-visible signal the code promises ("surfaces a RowDataError
Health issue") exists for a sub-second blink and then lies quiet while the entity's freshness
timer is silently never armed. Per-entity issue keying (as `issueKeyTimer` does with the
subject tail) or clear-only-for-the-same-entity would make the level signal honest.

### 4. LOW — `TimerDataError` issues are keyed per `<targetId>.<entityId>` tail and never cleared: unbounded issue-cache growth

`internal/weaver/temporal.go:163-188, 208`

Every malformed firing alerts under `issueKeyTimer(tail)` where `tail` embeds the entity id,
and no code path in the diff ever clears a `timer:` issue. The fired subject space is writable
by anything that can publish into `schedule.>` (the docs in this diff state the target subject
is publisher-chosen within the stream). Failure scenario: a misbehaving or malicious publisher
sprays N distinct malformed subjects/payloads into `schedule.weaver.timer.fired.>` — the
issue cache and every subsequent Contract #5 heartbeat document grow by N entries, forever
(until process restart). Contrast with finding 3: the scheduling leg over-clears, this leg
never clears.

### 5. LOW — `json.Marshal` failure is treated as retryable, contradicting the function's own contract

`internal/weaver/temporal.go:135-139`

The doc comment on `scheduleFreshness` says it "returns false **only** when the schedule
publish itself failed" and that "data errors are surfaced and skipped (redelivery cannot fix a
projected row)". The marshal-failure branch returns `false` → `NakWithDelay` → eternal
redelivery of a row that will marshal-fail identically every time, and it raises no Health
issue (only a log line), violating both halves of the stated contract. With `timerPayload`
being three plain strings the branch is near-unreachable today, which is precisely why it
would be a silent perpetual-redelivery hole if the struct ever grows an unmarshalable field.
Either Ack+issue it like the other data errors, or delete the lying comment.

### 6. LOW — Lane-3 publish failure blocks lane-1 remediation for the same row

`internal/weaver/evaluator.go:59-66`

`scheduleFreshness` runs before the `violating` gate, and its `false` return Naks the entire
row delivery. Failure scenario: `core-schedules` is degraded while `core-operations` is
healthy — every violating row that also carries a `freshUntil` has its remediation dispatch
deferred on the Nak cadence, even though dispatch needs only the healthy ops stream. The
temporal lane's availability becomes a hard dependency of the violation lane for any
freshness-bearing target. The docs in this diff say "a schedule-publish failure Naks the row"
but never state the consequence that remediation is held hostage with it. Ordering dispatch
first (or Ack-with-issue on schedule failure, relying on level-driven re-arm at next CDC
touch) would decouple the lanes. Flagged as accepted-design-candidate, not a defect demand.

### 7. LOW (test-only) — e2e timing windows of 2–3s race the CDC pipeline; flake on slow CI

`internal/weaver/weaver_e2e_test.go:921-936, 998-1011`

- `TestWeaverE2E_TemporalHappyLoop` arms at `now+2s` (line 921). If row delivery +
  schedule-arm takes >2s, `scheduleFreshness` sees a past instant and never publishes —
  `waitScheduleHeader` then burns its full 15s and fatals. Separately, even after
  `waitScheduleHeader` succeeds, the follow-up `GetLastMsgForSubject` (line 933) can race the
  one-shot firing removing the pending schedule message — `require.NoError` fails.
- `TestWeaverE2E_TemporalReplace` must land the second `putRow` through CDC **and** replace
  the schedule inside the first timer's 3s fuse (lines 998-1011); a slow runner gets the
  first-instant firing and the test reports a "broken replace" that is actually scheduler
  latency.

Wider fuses (e.g. 10s/20s) with the same relative ordering would keep the assertions while
removing the wall-clock coupling.

### 8. TRIVIAL — Lying comments in the new tests

- `internal/weaver/weaver_e2e_test.go:883-885` — `freshInstant` comment says it returns
  "(time, RFC3339 string)"; it returns only the string.
- `internal/weaver/weaver_e2e_test.go:870-872` — `installFreshTarget` comment says it
  "returns its targetId"; the function returns nothing (the caller passes the id in).

---

## Verified-clean (attack surfaces probed, no finding)

- `firedToken` reservation: a targetId `"fired"` is refused before any publish, and even
  without the guard a pending schedule landing in the fired filter would fail `splitRowKey`
  and drop loudly — defense in depth holds.
- Schedule subject reconstructed from the fired subject, never from the payload — payload
  `targetId` cannot redirect the requestId derivation to another timer slot.
- `deriveTimerRequestID` namespace (`"timer:"`) is disjoint from the episode/task derivations;
  determinism + new-instant-new-id pinned by `TestDeriveTimerRequestID_Deterministic`.
- Header constants test-pinned to `nats-server` constants
  (`internal/substrate/publish_schedule_test.go`) — no silent drift.
- `temporalStats` counters are atomics; heartbeater nil-checks before reading — no race.
- Tombstone rows checked before the scheduling leg (`row == nil` Acks first); pinned by the
  tombstone case in `TestHandleRow_SchedulingLeg`.
- `MaxMsgsPerSubject: 1` rollup of an unprocessed older firing is explicitly documented as
  level-correct in `docs/components/weaver.md` — accepted bound, not a hidden hole.
