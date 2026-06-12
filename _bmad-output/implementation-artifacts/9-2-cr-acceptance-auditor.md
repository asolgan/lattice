# Story 9.2 — Acceptance Auditor report (layer 3: diff + spec + contracts)

Audited: uncommitted working tree at HEAD `d8c1b87` (2026-06-12).
Inputs: story `9-2-weaver-mark-ttl-reconciler.md`; amended Epic 9.2 AC
(`phase-2-epics.md`, incl. the orphaned-mark reclaim clause); Contract #10 §10.3
`weaver-state` (FROZEN); 9.1 review escapes F5–F8 (`9-1-cr-edge-case-hunter.md`);
full diff + untracked files.

Independent verification run by this audit: `go build ./...` OK; `go vet` OK;
`golangci-lint run ./internal/weaver/... ./internal/substrate/...` → 0 issues;
`go test -count=1 ./internal/weaver/... ./internal/substrate/...` → ok (27.3s / 6.8s).

## Verdict: PASS — all 4 ACs met with evidence. 1 LOW hygiene finding, 3 notes. No blockers.

## AC verdict table

| AC | Verdict | Evidence |
|----|---------|----------|
| **AC 1** — full §10.3 shape: per-key TTL (TTL-provisioned bucket) mirrored by `leaseExpiresAt`, lease ≫ remediation latency, active reconciler sweep, TTL-as-backstop | **PASS** | Mark write: `internal/weaver/state.go` — `markStore.create` populates `LeaseExpiresAt = claimedAt + lease`, `HeldBy = instance`, and writes via `KVCreateWithTTL` with TTL = `markTTLBackstopFactor (2) × lease` (state.go:18-26 constant + invariant rationale, :73-94 create). Substrate primitive: `internal/substrate/kv.go:93-122` `KVCreateWithTTL` wraps `kv.Create(..., jetstream.KeyTTL(ttl))` — tombstone-aware CAS preserved, error mapping byte-identical to `KVCreate` (compared :75-91 vs new), `ttl<=0` → plain create. Bucket TTL provisioning pre-exists: `internal/bootstrap/primordial.go:72,88-94` (`weaver-state` in the `ttl:true` set, `LimitMarkerTTL=1s` → AllowMsgTTL) — story correctly made NO bootstrap change; both test harnesses provision `LimitMarkerTTL` (`weaver_e2e_test.go:49`, `evaluator_internal_test.go:42-44`, `reconciler_internal_test.go:43`). Lease ≫ latency: default 30m (`reconciler.go:14-17`), clamped ≥ 1s NATS floor in `engine.go` `withDefaults` (:116-124, same posture as loom `StepTimeout` clamp at `loom/engine.go:144-148`). Active sweep: `internal/weaver/reconciler.go` (new), started in `Engine.Start` beside the heartbeater (`engine.go:233`). Proof: `TestKVCreateWithTTL` (substrate, real server-side expiry polled to `ErrKeyNotFound`, CAS conflict, create-after-soft-delete, ttl=0 fallback — substrate_test.go:140-201); `TestMarkCreate_TTLBackstop` asserts the wire `Nats-TTL` header == `(2×MarkLease).String()` and `leaseExpiresAt` mirrors claimedAt+lease (reconciler_internal_test.go:557-610). |
| **AC 2** — level-reconciled clearing on the sweep; orphaned-mark reclaim (removed target / column absent from row+playbook — F5–F8 escapes); `claimedAt`/`claimId` episode tagging, claimId shape-only until Epic 10 | **PASS** | Sweep clearing leg: `reconciler.go:139-168` — row gone (`ErrKeyNotFound`) or `boolColumn` not true → revision-conditioned delete (no lease wait); unparseable row → leave ("never delete on unreadable evidence", :151-160). Orphan legs: target uninstalled → delete-only, gated by warm-up guard (:188-198, first-pass skip per adjudicated decision 8, :81-101 flips `firstPassDone` only after a COMPLETED enumeration); playbook lacks gap → `orphanColumn` delete-only (:200-206). Corrupt key/value → `CorruptMark` alert + delete (:127-136, :250-262). Expired/absent lease + column still true → plan-FIRST then delete then fresh episode via shared `fireEpisode` (:184-244; decision 5 ordering honored — failed plan leaves the mark, `TestSweep_PlanFailureLeavesMark`). All deletes `KVDeleteRevision` at the pass's read revision; conflict = skip (:264-291, `TestSweep_DeleteRevisionRace`). `claimedAt` written since 9.1; `ClaimID` stays empty — asserted (`TestMarkCreate_TTLBackstop` reconciler_internal_test.go:607-609), matching the amended AC's "populated when Epic 10's nudge arrives". The "absent from both row and playbook" clause is subsumed by row-absence alone (adjudicated in the story, Task 3b). F-escape closure: F5/F6 shadow → `TestWeaverE2E_MidFlightKill` (re-upsert does not re-fire) + `TestSweep_ReclaimExpired`; F6 prompt half + F7 row variants → `TestSweep_LevelClear` (4 cases incl. tombstone + unparseable); F7 playbook-drop + re-add-unshadowed → `TestSweep_OrphanColumn`; F8 + warm-up → `TestSweep_WarmUpGuardAndOrphanTarget` + `TestWeaverE2E_SweepOrphanedTargetMarks` (heartbeat-observed first-pass skip, reinstall replay dispatch). Legacy 9.1 lease-less mark reads expired (`leaseLive`, reconciler.go:314-329; `TestSweep_LegacyMarkReclaimed`). |
| **AC 3** — re-fire idempotency per §10.3: nudge safe via claimId; triggerLoom/assignTask documented rare-double, operator-visible | **PASS** | Reclaim = delete + fresh CAS-create → new revision → NEW `requestId` (proven exactly: `TestSweep_ReclaimExpired` asserts op requestId == `deriveEpisodeRequestID(..., newRev)` and ≠ the dead episode's). Operator visibility: every reclaim/orphan delete logs Warn with targetId/entityId/gap/action/reason (`deleteMark`, reconciler.go:269-291); since-start counters `sweepReclaims`/`sweepOrphansDeleted`/`sweepCorrupt`/`sweepLastRunAt` surfaced in the heartbeat beside `marksInFlight` (`health.go:218-226`). No check-before-act probe built (Phase-3, per §10.3 + decision 3 — verified absent). Nudge: no claimId minting anywhere; the strategist still rejects nudge as errConfig (`strategist.go:178-181`), so a sweep reclaim of a hypothetical nudge mark fails the plan and LEAVES the mark — it can never mint a fresh id (§10.3 never-mint rule safe by construction). Documented: `docs/components/weaver.md` Dispatch-OCC row (claimId-atomic + corrupt-alert-never-mint rule) and new Reconciler-sweep row (rare-double posture, Warn + counters, Phase-3 deferral). |
| **AC 4** — mid-flight-kill test: lease expires, target re-attempted, never wedged | **PASS** | `TestWeaverE2E_MidFlightKill` (weaver_e2e_test.go): constructs the exact post-CAS-create / pre-publish state (mark present with a 4s lease, op never published — the story's authorized simulation of the kill; an in-process deterministic mid-function kill is not otherwise achievable), starts the engine with a 300ms sweep: (1) replayed fresh delivery anti-storm-drops while the lease lives (`requireNoOp`); (2) lease expiry → sweep reclaims → exactly one `StartLoomPattern` op with non-zero `expectedRevision`; (3) mark re-created at a different revision, `heldBy` = this instance, live lease; (4) `requireNoOp` after — never a storm; (5) F5 coalesce angle: a fresh re-upsert of the still-violating row does not re-fire. |

## Cross-cutting checks

- **Frozen contracts untouched**: `git diff docs/contracts/` empty. No amendment request raised; none needed (see N3).
- **Planning artifacts untouched**: `git diff _bmad-output/planning-artifacts/` empty.
- **Scope discipline**: no `weaver-claims` writes, no claimId minting, no temporal lane, no
  marker-watcher on weaver-state (decision 2 — sweep is ticker-only), no pause-awareness, no
  bootstrap changes, `internal/loom`/`refractor`/`processor` untouched. Modified set == story File
  List + regenerated gate reports.
- **One substrate edit, minimal**: `KVCreateWithTTL` only (+ `TestKVCreateWithTTL`); error
  mapping identical to `KVCreate`; godoc states the `LimitMarkerTTL` prerequisite and 1s floor.
- **9.1 fix batch survived verbatim**: diff of `evaluator.go` against committed `d8c1b87` (which
  contains the post-review batch) shows a pure extraction: `planGap` keeps the
  errTransient→NakWithDelay / errData→alert+Ack / errConfig→alert+Ack routing and issue keys
  byte-for-byte; the `msg.Sequence == 0` defer-guard stays in the lane-1 wrapper untouched;
  `redelivered = msg.NumDelivered != 1` preserves the NumDelivered-0-re-fires-never-drops
  semantics exactly (comment moved with it); `clearClosedMarks` and its unconditional `KVDelete`
  untouched (decision 6). Full 9.1 e2e suite green unmodified.
- **splitMarkKey soundness**: gap columns and targetId are install-validated single
  `singleTokenPattern` segments (`registry.go:350-371`), so the positional parse cannot
  misclassify a legitimate mark as corrupt.
- **No sub-agent commit**: HEAD `d8c1b87` and the four commits above the session-start snapshot
  (`f2e221f`…`d8c1b87`) are the lead's prior 9.1 landings; the 9.2 work is entirely uncommitted
  in the working tree, as required.

## Findings

### F1 — Story-number comment in test code [LOW — hygiene]
`internal/weaver/reconciler_internal_test.go:89-90`: `// putMark writes a constructed §10.3 mark
value directly (no TTL — the shape a crashed pre-9.2 writer or a manually-aged episode leaves
behind)`. "pre-9.2 writer" narrates a change relative to a prior state — the CLAUDE.md
most-violated rule, in a test file. Trivial reword (e.g. "the shape a lease-less legacy mark or a
manually-aged episode has"). Only occurrence in the whole diff; production code is clean.

### N2 — MidFlightKill e2e asserts the new episode via mark revision, not the op's requestId [NOTE — covered elsewhere]
Task 5's bullet says the e2e asserts "a requestId ≠ the dead episode's". The e2e asserts the
re-created mark's revision ≠ the dead revision (requestId is a pure derivation of that revision),
and the explicit old-vs-new requestId assertion lives in `TestSweep_ReclaimExpired`
(reconciler_internal_test.go:263-270). AC 4 itself is fully satisfied; this is a task-text
placement nuance, not a coverage gap.

### N3 — TTL = 2×lease vs §10.3's literal "leaseExpiresAt mirrors the TTL" [NOTE — adjudicated, flag for Winston's confirmation]
The frozen text reads "`leaseExpiresAt` mirrors the TTL for visibility"; the implementation has
`leaseExpiresAt` = lease and TTL = 2×lease. The story pre-adjudicated this (binding decision 1)
with sound reasoning — the same §10.3 paragraph requires the active sweep to reclaim expired
leases, which is unreachable if the key self-deletes exactly at the lease — and raises it as
Question 1 for Winston. No contract edit was made. Compliant under the adjudication; Winston
should ratify the ×2 reading when triaging (no amendment request strictly needed since the
contract's two clauses are only satisfiable together this way, but it is the audit's job to
surface the textual delta).

### N4 — Reclaim counter increments before the re-dispatch publishes [NOTE — informational]
`reconciler.go:230-244`: `sweepReclaims` bumps after the old mark's delete succeeds but before
`fireEpisode`; a publish failure (or a lost CAS race) after that still counts as a reclaim. The
delete did happen and the Warn log distinguishes the no-publish case, so the metric is honest as
"expired marks reclaimed"; mentioning only so the metric's semantics are on record.

## Gate evidence (this audit's independent runs)

- `go build ./...` — OK
- `go vet ./internal/weaver/... ./internal/substrate/...` — OK
- `golangci-lint run ./internal/weaver/... ./internal/substrate/...` — 0 issues
- `go test -count=1 ./internal/weaver/...` — ok 27.312s (incl. all new sweep/e2e tests)
- `go test -count=1 ./internal/substrate/...` — ok 6.838s (incl. `TestKVCreateWithTTL`)
- Gate 2 / Gate 3 reports regenerated by the dev at this tree (`Run at 2026-06-12T21:36Z`,
  `Commit: d8c1b87`): all BLOCKED / all DEFENDED — header-only diffs, no result changes.
