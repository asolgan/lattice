# Story 9.3 — Acceptance Auditor report (diff + spec + contracts layer)

Audited: uncommitted working tree on `main` (base 7196488), 2026-06-13.
Inputs: story `9-3-weaver-temporal-lane.md` + the lead's six adjudications (incl. the AMENDED
per-target schedule subject `schedule.weaver.timer.<targetId>.<entityId>` / fired
`schedule.weaver.timer.fired.<targetId>.<entityId>`); Epic 9.3 AC (`phase-2-epics.md`);
Contract #10 §10.4 (FROZEN, corrected 2026-06-05); Contract #4 (requestId dedup).

## Verdict: ACCEPT — all 5 ACs met against the adjudicated shape; gates independently re-run green. Findings are traceability/coverage, not code defects.

## AC verdict table

| AC | Verdict | Evidence |
|----|---------|----------|
| 1 — Actuator schedules `@at(freshUntil)` on `core-schedules`, in-stream fired target, §10.4 headers | **PASS** (per the lead's AMENDED per-target subject) | `internal/weaver/actuator.go:92-103` (`scheduleTimer`: one `conn.Publish` with `substrate.ScheduleHeader`/`ScheduleTargetHeader`); `internal/weaver/temporal.go:21-24` (subject constants), `:85-147` (`scheduleFreshness` — level-driven, every delivery, future-only, seconds-truncated so header/payload/requestId seed are byte-identical); `internal/substrate/publish.go:10-21` (the two constants, godoc cites §10.4/ADR-51); `internal/substrate/publish_schedule_test.go:13-20` (pinned to `natsserver.JSSchedulePattern`/`JSScheduleTarget`); e2e asserts both headers and the in-stream target — `weaver_e2e_test.go` `TestWeaverE2E_TemporalHappyLoop` (`require.Equal(..., "schedule.weaver.timer.fired."+targetID+"."+entityID, sched.Header.Get(substrate.ScheduleTargetHeader))`). |
| 2 — Supervised consumer on `schedule.weaver.timer.fired.>` → `MarkExpired` op via Processor, deterministic requestId, never core-events | **PASS** | `temporal.go:64-74` (`temporalSpec`: fixed durable `weaver-temporal`, `Stream: cfg.CoreSchedulesStream`, `FilterSubject: firedSubjectPrefix+">"`, `DeliverAll`, `Handler: supervisedHandler(e.handleFiredTimer)`, `Health: newConsumerHealthSink(...)`); wired in `engine.go` `Start` (`supervisor.Add(ctx, e.temporalSpec())`). `temporal.go:160-206` (`handleFiredTimer`): schedule subject reconstructed from the fired subject, never the payload (`:169-171`); `requestID := deriveTimerRequestID(scheduleSubject, p.FireAt)` → `actuator.go:125-132` = `deriveID("timer:", scheduleSubject+"\x00"+fireAt, 0)` — exactly §10.4 (schedule subject + fire instant); op submitted via `e.act.submit` → `actuator.go:77` publishes to `ops.<lane>` only; **no `events.*` publish exists anywhere in non-test weaver code** (grepped). No authContext (`:196-199`); no weaver-state mark (unit-asserted, `temporal_internal_test.go:143-150`). Redelivery dedup pinned: `TestHandleFiredTimer_RedeliveryDedup` (NumDelivered 1→2 same requestId; new fireAt → new requestId) + `TestDeriveTimerRequestID_Deterministic`. Publish failure → `Nak` (same requestId on retry, `temporal.go:199-203`). |
| 3 — CDC re-projects; freshness gap flips violating | **PASS** (fixture-driven, the 9.1 posture — Refractor wiring is Epic 11, per story) | `TestWeaverE2E_TemporalHappyLoop`: after the `MarkExpired` op, the fixture re-projects `violating: true, missing_fresh: true` → lane-1 dispatches `FixFresh` + mark (`waitMark`), proving the full time→op→violation→remediation chain. |
| 4 — Re-do before expiry replaces the prior timer | **PASS** | Level design: `scheduleFreshness` re-publishes on every delivery; `MaxMsgsPerSubject: 1` + AllowMsgSchedules rollup replace (mirrored in both test provisioners and `internal/bootstrap/primordial.go:160-177` — configs match). `TestWeaverE2E_TemporalReplace`: first `@at now+3s`, replaced with `now+6s` BEFORE expiry, both inside the observation window (the 7.4 replace-assertion rigor — a broken replace fails on `expiredAt` = first instant); exactly ONE op asserted (`requireNoOp` after). |
| 5 — Durable across Weaver restart | **PASS** (real, not simulated) | `TestWeaverE2E_TemporalRestartDurability`: engine ctx cancelled + `<-done` BEFORE expiry, FRESH engine on the same server converts the firing — exactly one op with the §10.4 requestId. Plus `TestWeaverE2E_TemporalMissedWhileDown`: timer fires with NO engine running (fired message polled in-stream first), restart → the fixed durable's ack floor picks it up — exactly one op. |

## Cross-cutting checks

- **Frozen contracts untouched**: `git diff HEAD -- docs/contracts/ _bmad-output/planning-artifacts/` is empty. CAR file (`cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md`) unmodified — still only the pending 9.2 §10.3 request (see Finding 2 on whether a 9.3 append is owed).
- **CLAUDE.md hygiene**: diff scanned — no story-number/history comments; the two "replaces" hits are present-tense replace-on-reschedule semantics. The `scheduling_smoke_test.go` comment rewrite is present-state ("the embedded test server runs the NATS scheduler too"), not change narration.
- **Module boundary**: `temporal.go` imports only stdlib + `internal/substrate`; the boundary test ran green in the suite. Zero domain literals in engine code (`MarkExpired`, `freshUntil`, `schedule.weaver.timer.*` are platform vocabulary; `missing_fresh`/`vtx.leaseApp` live in tests only).
- **Substrate edit minimal + tested**: exactly two exported constants in `publish.go` (no helper, per adjudication 9) + the pin test against the server's exported constants. `scheduleTimer` correctly lives in weaver's actuator using the existing `Conn.Publish` header parameter.
- **9.1/9.2 intact** (diffed against f0288e3): `evaluator.go` change is a single inserted `scheduleFreshness` call after `clearClosedMarks`/tombstone-return and before the `violating` gate (`evaluator.go:56-65`) — no existing Decision path altered; `reconciler.go`/`state.go` untouched; `strategist.go` adds only the `opMarkExpired` constant; `health.go` adds only the two heartbeat counters; `engine.go` adds `Config.CoreSchedulesStream` (default `"core-schedules"`, literal — no bootstrap import) + the static consumer. Full regression suite green (below).
- **Scope**: no lane-2/`weaver-work`, no Epic 10 nudge (stub test untouched), no 9.4 control surface, no bootstrap/loom/refractor/processor edits. `core-schedules` provisioning exists since 7.4; tests mirror it.
- **Edge handling per story Task 2/3**: non-string/unparseable `freshUntil` → Warn + `RowDataError` keyed issue, skip (`temporal.go:96-102`); missing entityKey → same (`:112-119`); past instant → Debug skip, no issue (`:125-134`); tombstone → never reaches the leg (row==nil returns first; unit-asserted incl. no-purge); schedule-publish failure → `NakWithDelay` from `handleRow`; reserved `fired` targetId refused loudly (`:103-111`); malformed fired subject/payload → Ack + keyed `timer:` `TimerDataError` issue, never silent (FR29).

## Independently re-run gates (dev was killed mid-gates; lead's green re-run CONFIRMED)

- `go build ./...` — OK
- `go vet ./internal/weaver/... ./internal/substrate/...` — clean
- `go test ./internal/substrate/...` — ok (7.4s)
- `go test ./internal/weaver/...` — ok (53.9s; all temporal e2e + internal suites + full 9.1/9.2 regression net)
- `go test ./internal/bootstrap/... ./internal/loom/...` — ok
- `make verify-kernel` — ALL ASSERTIONS PASSED
- Gate 2 / Gate 3 report artifacts regenerated at commit 7196488: PASSED (4/4 BLOCKED; 4/4 cleared)

## Findings

### F1 (Minor — traceability, lead action): the shipped subject shape matches NO written AC text
The implementation is per-target (`schedule.weaver.timer.<targetId>.<entityId>`), per the lead's
amended adjudication — but the epic AC (`_bmad-output/planning-artifacts/epics/phase-2-epics.md:277`)
still reads `schedule.weaver.timer.<entityId>`, and the story file's own AC 1, Task 2/3 text, and
Adjudicated decision 3 (which calls per-entity "pinned by the epic AC" and accepts the
last-write-wins bound the per-target shape was amended to REMOVE) all still describe the per-entity
shape. The code is right per the adjudication; the paper trail is not. Winston should amend the epic
AC (planning-artifact, lead-owned) and reconcile the story file's decision 3 / Questions 3 before
setting Status: done.

### F2 (Minor — contract tension, lead judgment): the extra subject token vs the FROZEN §10.4 template
§10.4's frozen shape block reads `schedule.<domain>.<kind>.<entityId>`; the shipped subject adds a
`<targetId>` token. The fired-subject side is explicitly "publisher-chosen … e.g." (no tension), but
the schedule-subject template line is part of the frozen shape, and the new scheduling.md paragraph
("a publisher may key its schedules with additional dot-free tokens…") is effectively a /docs gloss
widening a FROZEN contract grammar. If the template is read as normative, this is an
annotation-class CAR candidate (the 7.4 precedent) for `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md` —
append, don't overwrite. Likewise decision 2's optional `freshUntil` §10.2 annotation CAR was left
to the lead and not appended (acceptable per the story's own framing; flagging for the record).

### F3 (Minor — test coverage): two untested branches in the scheduling leg
(a) The reserved-`fired` targetId guard (`temporal.go:103-111`, `ScheduleConfigError`) has no unit
test — `TestHandleRow_SchedulingLeg` covers every other branch. (b) The schedule-publish-failure →
`NakWithDelay` branch (`evaluator.go:63-65` / `temporal.go:140-144`) is untested (needs a failing
conn). Both are small, mechanical additions.

### F4 (Info — accepted bound, verify it is intended): scheduling leg gates lane-1 dispatch
`scheduleFreshness` runs before the `violating` gate, so a persistent `core-schedules` publish
failure NakWithDelay-defers the row's lane-1 remediation dispatch as well — remediation (needs only
`core-operations`) is coupled behind the scheduling stream for rows carrying `freshUntil`. This is
exactly the story's prescribed posture ("the redelivery re-runs the whole level pass"), cadence-
bounded; recorded here so the coupling is a decision, not an accident.

### F5 (Trivial — story-artifact hygiene)
`9-3-weaver-temporal-lane.md`: duplicated `### Change Log` heading (lines 389/392); Dev Agent Record
mostly empty (Agent Model Used / Completion Notes blank); File List calls the substrate edit a
"schedule-publish helper" — it is the two constants (the helper was explicitly forbidden by
adjudication 9, and the code correctly added only constants; the wording alone is wrong).

### F6 (Trivial — doc nit): scheduling.md publish example keys per-entity
The how-to snippet still shows `schedule.weaver.timer.<entityId>` / fired `…fired.<entityId>` while
the Weaver-specific bullet below documents the per-target shape. Defensible as the generic §10.4
template example; a one-line "(generic example — Weaver itself keys per target, see below)" would
remove the ambiguity.
