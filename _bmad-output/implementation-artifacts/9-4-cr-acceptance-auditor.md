# Story 9.4 — Acceptance Auditor review (diff + spec + contracts)

Reviewer lens: Acceptance Auditor (every AC met; Refractor-mirror fidelity; scope discipline;
CLAUDE.md hygiene). Evidence is file:line against the uncommitted working tree.

## Tree state (independently re-run)

- `go build ./...` — clean (no output).
- `go vet ./internal/weaver/... ./internal/weaver/control/... ./cmd/lattice/weaver/... ./cmd/weaver/...` — clean.
- `golangci-lint run ./internal/weaver/... ./internal/weaver/control/... ./cmd/lattice/weaver/...` — `0 issues`.
- `go test -count=1 ./internal/weaver/... ./internal/weaver/control/... ./cmd/lattice/weaver/... ./cmd/weaver/...`
  — `internal/weaver` ok (72.2s), `internal/weaver/control` ok (0.93s), `cmd/lattice/weaver` ok (1.16s),
  `cmd/weaver` no test files. All green.
- One-way dependency confirmed: `go list -deps internal/weaver` includes neither
  `internal/weaver/control` nor `nats.go/micro` (both grep-count 0). `boundary_test.go` and
  `weaver_e2e_test.go` are unmodified (`git status --porcelain` empty for both).
- evaluator.go / reconciler.go diffed against committed `722421b` (Story 9.3 base): both changes are
  purely additive early-return guards — no rewrite of existing decision trees.

## AC verdict table

| AC | Topic | Verdict | Evidence |
|----|-------|---------|----------|
| 1 | `list` returns targetId/lensRef/gaps/state | PASS (with noted nuance) | `control.go:67-95` ListTargets; state from in-memory disabled-set per adjudicated Q6, not `consumerStateCache` directly — see Finding F1 |
| 2 | `disable` = Pause + dispatch-skip marker; no Lens removal | PASS | `control.go:110-121` (Pause + setDisabled + in-mem set); no registry/reconcile/Lens mutation |
| 3 | `__control` reserved key, JSON body, enable deletes | PASS | `state.go:182-187` suffix const; `state.go:204-227` setDisabled body `{disabled,disabledAt}`; collision-safety asserted `state_internal_test.go:104,130` |
| 4 | `revoke` = Remove + health-sink delete + prefix-delete + issue-clear; no spec mutation; reconcile re-add bound documented | PASS | `control.go:165-197`; `state.go:236-265` deleteByTargetPrefix; bound documented `docs/components/weaver.md:250-265` + CLI `weaver.go:139` |
| 5 | New sibling pkg `internal/weaver/control` may import micro; boundary untouched | PASS | `control/service.go:8` package; one-way dep verified above; `boundary_test.go` unmodified |
| 6 | 4 exported `*Engine` methods; Disable/Enable error if unregistered, Revoke idempotent | PASS | `control.go:67,110,129,165`; Disable/Enable `:111,:130` error; Revoke no early-return; tests `control_internal_test.go:216,278,349` |
| 7 | handleRow + scheduleFreshness skip dispatch; mark-clearing leg unaffected | PASS | `evaluator.go:60-67` (guard AFTER `clearClosedMarks` line 52); `temporal.go:95-101`; test `control_internal_test.go:398` |
| 8 | `weaver-control` micro service; `list` 4-token, others 5-token wildcard; default queue; StartNATSListener | PASS | `service.go:129-174`; `list` exact `:146`, wildcard `:154`; `targetIDFromSubject` 5-token `:238-250`; ctx.Done stop `:167-172` |
| 9 | `cmd/weaver` wires control alongside engine.Start | PASS | `cmd/weaver/main.go:116-120` (StartNATSListener before blocking Start); compile-time iface check `:42-49` |
| 10 | `cmd/lattice/weaver` group, 4 subcmds, output conventions, registered, bounded-timeout error path | PASS | `cmd/lattice/weaver/weaver.go` whole; `root.go:79` register; `DefaultTimeout` `weaver.go:35`; `PrintJSONError("ControlError",...)` `:67` etc. |
| 11 | Stub-authorize-and-log posture, called before op | PASS | `control/capability.go` (allow-all + slog.Info); called `service.go:180,204` before dispatch |
| 12 | boundary + e2e unmodified & pass; new dispatch-skip/enable/revoke tests; control pkg + CLI tests | PASS | unmodified verified above; `control_internal_test.go:391,472,538`; `control/service_test.go` 9 tests; `weaver_test.go` 6 tests |

All 12 ACs PASS. The 7 lead adjudications (package split, 2-value State enum, Revoke strict-superset,
handleFiredTimer disabled-skip, subject scheme/CLI signature, in-memory disabled-set, enable naming)
are each implemented as specified — notably the AMENDED #4 `handleFiredTimer` guard
(`temporal.go:205-218`) and #6 in-memory set rebuilt from durable `__control` via
`seedDisabledTargets` (`control.go:39-59`, called at `engine.go:Start`).

## Findings (none blocking)

- **F1 (OBSERVATION, not a defect).** AC #1's literal text derives `list` state from "the lane-1
  consumer's pause state (`consumerStateCache`) AND the dispatch-skip marker." The implementation
  (`control.go:83-86`) derives state solely from the in-memory `disabledTargetSet`, which is seeded
  from the `__control` marker (`control.go:39-59`) and updated synchronously by Disable/Enable/Revoke.
  This is the lead-adjudicated Q6 single-source-of-truth posture (marker is durable truth; the set is
  its cache), and Disable always writes both Pause AND marker together, so the two never disagree.
  Acceptable — but note that an out-of-band `supervisor.Pause` (no marker, e.g. a future direct
  operator pause) would report `"active"` in `list`. Not reachable by any current code path; flagging
  only for traceability against AC #1's exact wording.

- **F2 (OBSERVATION).** The epic AC (`phase-2-epics.md:296`) says revoke = "Remove + clearing the
  target's weaver-state marks." The strict-superset implementation deletes all marks then RE-WRITES
  the `<targetId>.__control` marker (`control.go:190`). This is the adjudicated Q3 amendment (revoke
  leaves a standing disabled marker so a reconcile re-add stays inert), documented at
  `docs/components/weaver.md:250-265`. Intentional and correct per the amendment — noted only because
  it is a deliberate superset of the frozen-epic wording.

- **F3 (NIT).** `service.go:130` reads `s.microSvc != nil` without holding a lock (the Refractor
  mirror at `refractor/control/service.go:315-320` takes `s.mu` for the same check). The Weaver
  `Service` has no mutex field at all and `StartNATSListener` is called exactly once from
  `cmd/weaver/main.go` before any concurrency, so there is no actual race. Harmless divergence from
  the mirror; could add a mutex for strict fidelity but not required for correctness.

## Reserved-key / sweep-guard (lead-mandated) — PRESENT

- Collision guard: `controlKeySuffix = ".__control"` (`state.go:182`); `__control` is non-NanoID
  (`substrate.Alphabet` has no `_`); a real mark is 2-dot `<targetId>.<entityId>.<gapColumn>` vs the
  1-dot `<targetId>.__control`. Asserted by `TestControlKey_NoCollisionWithMark` and
  `TestControlKeySuffix_NotProducibleByNanoID` (`state_internal_test.go:104,130`).
- Sweep guard: `reconciler.go:120-127` skips `controlKeySuffix`-suffixed keys before `sweepMark`,
  proven by `TestSweep_ControlMarkerSurvives` (`reconciler_internal_test.go:784`) — survives passes,
  never enumerated as `CorruptMark`, sweep-corrupt counter stays 0.

## Hygiene & scope

- No history/changelog comments in the production diff (grep for `Story 9.4`/`Replaces`/`Previously`/
  `renamed from`/`moved from`/`Was:` over all touched production files — empty). Comments reference
  contract anchors (AC #/FR30/§10.3) only, consistent with existing `internal/weaver` precedent.
- Control package mirrors Refractor transport/naming: `ControlResponse` per-op-result-field envelope,
  `<x>IDFromSubject` 5-token parse, `StartNATSListener` lifecycle, `StubCapabilityChecker` posture —
  all structurally faithful (only the documented `list` 4-token asymmetry and F3 lock nit diverge).
- Operator-facing only: no console/UI dependency; `cmd/lattice/weaver` uses plain
  `nc.RequestWithContext` against the micro responders, bounded by `output.DefaultTimeout`.
- Scope intact: no new convergence behavior; evaluator/reconciler changes are additive guards only
  (diffed vs 722421b); boundary_test.go + weaver_e2e_test.go untouched and green.

## Verdict

**APPROVE.** All 12 ACs met; all 7 lead adjudications implemented as specified; reserved-key collision
guard + sweep guard present and tested; one-way package dependency and boundary tests intact; full
targeted suite + build + vet + lint green. Three non-blocking observations (F1 AC#1 state-source
nuance, F2 deliberate revoke superset over epic wording, F3 cosmetic unlocked-read in StartNATSListener)
— none affect correctness or warrant a fix-forward gate.
