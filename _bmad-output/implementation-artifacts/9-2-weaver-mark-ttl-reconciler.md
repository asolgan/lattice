# Story 9.2: Anti-storm in-flight marks + TTL reconciliation

Status: review

## Story

As a platform developer,
I want in-flight convergence marks with a TTL and reconciliation,
So that Weaver neither re-triggers on a persisting violation nor wedges forever after a crash.

## Acceptance Criteria

(Authoritative source: `_bmad-output/planning-artifacts/epics/phase-2-epics.md` → Epic 9 → Story 9.2,
as amended 2026-06-12 — the current file text governs, including the orphaned-mark reclaim clause.)

1. **Given** the 9.1 CAS-create in-flight mark (key `<targetId>.<entityId>.<gapColumn>`) suppressing
   re-triggering on a re-observed row (CDC lag), **when** the mark is hardened to the full frozen
   §10.3 `weaver-state` shape, **then** the mark carries a **NATS per-key TTL** (the bucket is
   TTL-provisioned) mirrored by `leaseExpiresAt`, sized ≫ expected remediation latency, **and** an
   **active reconciler sweep** reclaims expired leases promptly — TTL is the backstop, so a
   missing/dead reconciler can never wedge a gap forever.
2. **And** mark-clearing is level-reconciled on the **sweep** as well as on watch updates (9.1); the
   sweep also reclaims **orphaned marks** — marks whose target is no longer installed, or whose gap
   column is absent from both the current row and the current playbook (the stale-mark escapes the
   9.1 review catalogued: coalesced close→reopen, playbook-changed strays, removed-target leftovers);
   `claimedAt`/`claimId` tag the episode so a stale prior-episode mark can't shadow a re-open (the
   `claimId` field lands now per the frozen value shape; it is populated when Epic 10's nudge
   arrives).
3. **And** re-fire-after-lease-expiry idempotency follows §10.3: `nudge` safe via `claimId`;
   `triggerLoom`/`assignTask` = documented rare-double, operator-visible.
4. **And** a **mid-flight-kill test** (Actuator crashes after marking in-flight, before completing)
   asserts the lease expires and the target is re-attempted — never permanently wedged.

## Tasks / Subtasks

- [x] Task 1: Substrate — `KVCreateWithTTL` (the one authorized substrate edit) (AC 1)
  - [x] Add `(*Conn).KVCreateWithTTL(ctx, bucket, key string, value []byte, ttl time.Duration) (uint64, error)`
        to `internal/substrate/kv.go`, beside `KVCreate` (~line 96): wrap
        `kv.Create(ctx, key, value, jetstream.KeyTTL(ttl))` (nats.go v1.52.0 supports the
        `KVCreateOpt`; see `jetstream/kv_options.go` `KeyTTL`). Same error mapping as `KVCreate`
        (`ErrRevisionConflict` on exists, wrapped otherwise); `ttl <= 0` falls back to plain create
        (mirror `KVPutWithTTL`'s posture). Godoc: bucket must be provisioned with
        `LimitMarkerTTL` (AllowMsgTTL); NATS floor is 1s. Do NOT route through a raw publish —
        `kv.Create`'s tombstone-aware CAS (create-after-delete) must be preserved.
  - [x] Substrate test (`internal/substrate/kv_test.go` pattern): CAS semantics hold (second create
        → `ErrRevisionConflict`); a ~1–2s TTL key expires (KVGet → `ErrKeyNotFound`); create-after-
        soft-delete succeeds with TTL. Use a `LimitMarkerTTL`-provisioned test bucket.
  - [x] No other substrate changes. A further substrate gap = STOP and write it up in Questions.
- [x] Task 2: Mark hardening to the full §10.3 value shape (AC 1, 2)
  - [x] `internal/weaver/state.go` `markStore.create`: write via `KVCreateWithTTL`; populate
        `LeaseExpiresAt = substrate.FormatTimestamp(now.Add(lease))` and `HeldBy = <instance>`
        (`claimedAt` already present). `ClaimID` stays empty/absent — it is populated only by
        Epic 10's nudge path (§10.3: minted in the SAME atomic op as the CAS-create, nudge marks
        only). The struct fields already exist omit-empty; this story populates lease + holder.
  - [x] **Per-key TTL = 2 × lease** (`markTTLBackstopFactor = 2`, a constant, not a config knob).
        Rationale (pinned — see Adjudicated decision 1): the TTL is the *backstop* and the sweep is
        the *prompt reclaim*; the sweep can only observe an expired lease if the key still exists
        past `leaseExpiresAt`, so the TTL must be strictly longer than the lease. `leaseExpiresAt`
        mirrors the lease the TTL backstops (§10.3 "mirrored … for visibility").
  - [x] `internal/weaver/engine.go` `Config`: add `MarkLease time.Duration` (default **30m**,
        validated ≥ 1s — the NATS per-key TTL floor, same guard comment style as loom's
        `StepTimeout`) and `SweepInterval time.Duration` (default **1m**, values ≤ 0 take the
        default like `HeartbeatEvery`). Lease ≫ expected remediation latency per §10.3; both small
        in tests.
  - [x] `markStore` gains the lease/instance inputs (constructor params or fields) — keep the
        in-flight check a KV read; no in-memory mark state (9.1 decision 8 stands).
- [x] Task 3: Reconciler sweep (AC 1, 2, 3) — new `internal/weaver/reconciler.go`
  - [x] Ticker goroutine started in `Engine.Start` (beside the heartbeater), period
        `Config.SweepInterval`, stopped via ctx. Each pass enumerates `weaver-state` with
        `KVListKeys` (the bucket holds ONLY marks; bounded by in-flight count — the
        `countInFlight` precedent; `KVListKeys` already excludes tombstones/TTL-expired keys).
        Serialize passes (skip if previous still running).
  - [x] Per mark `<targetId>.<entityId>.<gapColumn>` (split: first segment targetId, NanoID
        entityId, remainder gapColumn — install-validated single segments):
        (a) **Corrupt** key (unparseable) or value (bad JSON) → alert (`issueCache`, Error log) +
        delete. Record: weaver-private bucket, garbage otherwise lives forever.
        (b) **Level-reconciled clearing (the sweep leg of §10.3)**: `KVGet` the current
        `weaver-targets` row `<targetId>.<entityId>`; if the row is gone (ErrKeyNotFound) or
        `missing_<gapColumn>` is not currently `true` (reuse `boolColumn` semantics — non-bool
        reads conservative) → `KVDeleteRevision` the mark at its read revision. This closes the
        9.1-review F6 completed-episode stale mark and F7's row-tombstone/column-gone orphan, and
        subsumes the AC's "absent from both current row and current playbook" clause (absent from
        the row alone suffices under level reconcile — a mark may only stand for a currently-true
        column). An UNPARSEABLE row (bad JSON) → Warn + leave the mark (mirror `handleRow`'s
        no-clearing posture on bad rows; the lease/TTL backstop bounds it — never delete on
        unreadable evidence).
        (c) Column still `true` and **lease unexpired** (`leaseExpiresAt` parsed, > now) → leave:
        the episode is in flight.
        (d) Column still `true` and **lease expired** — or `leaseExpiresAt` absent (a 9.1-era
        legacy mark: no lease, no TTL; treat as expired so pre-9.2 marks are reclaimed, never
        immortal) → **reclaim**: if the target is installed and its playbook has the gap, build
        the plan FIRST (see ordering note below), then `KVDeleteRevision` the old mark and
        re-dispatch through the shared dispatch path — fresh CAS-create (new revision → new
        episode → NEW `requestId`), `expectedRevision` = the row's KV revision from the `KVGet`.
        If the target is **not installed** (orphan, F8) or the playbook lacks the gap → delete
        only, no dispatch. This is the lost-publish/mid-flight-kill recovery: lease expiry →
        re-attempt (closes F5 and the 9.1 accepted crash-wedge).
  - [x] **Ordering on reclaim**: plan-build BEFORE deleting the expired mark. A failed plan
        (unresolved ref, template data error) → alert via the existing issue keys and LEAVE the
        expired mark in place — the next sweep retries; deleting first would orphan the gap until
        the next row delivery (the sweep enumerates marks, not rows). A publish failure after the
        fresh CAS-create leaves the new mark holding a fresh lease — retried at its expiry (and by
        any row redelivery, same-episode requestId). Bounded, loud, never a hot loop.
  - [x] **All sweep deletes are `KVDeleteRevision`** at the revision read this pass — a CAS-create
        racing the sweep (fresh episode) must never be deleted blind; on conflict, skip.
  - [x] **Registry warm-up guard**: the target-not-installed orphan leg (d's delete-only branch
        for missing targets, and (b) is unaffected) is SKIPPED on the first pass after engine
        start — the registry source replays `meta.weaverTarget` history asynchronously and exposes
        no replay-done signal; a boot-instant sweep must not mass-delete live targets' marks.
        From the second pass on, orphans are reclaimed promptly per the AC.
  - [x] **Operator visibility (AC 3)**: every reclaim/orphan-delete logs at Warn with
        targetId/entityId/gap/action and the reason (`leaseExpired`, `targetRemoved`,
        `orphanColumn`, `corrupt`); the sweeper keeps since-start counters surfaced as heartbeat
        metrics (`sweepReclaims`, `sweepOrphansDeleted`, `sweepCorrupt`, plus
        `sweepLastRunAt`) in `health.go` `emit` beside `marksInFlight`. A re-fired
        `triggerLoom`/`assignTask` is the §10.3 documented rare-double — the Warn log + counter IS
        the operator visibility; do NOT build a check-before-act probe (Phase-3 hardening,
        explicitly out of scope).
- [x] Task 4: Dispatch-path refactor for sweep reuse (AC 1)
  - [x] Extract the L2+Strategist+Actuator core of `internal/weaver/evaluator.go` `dispatchGap`
        into a form both callers share: lane-1 passes `msg.Sequence` (row revision) +
        redelivered = `NumDelivered > 1`; the sweep passes the `KVGet` entry's `Revision` +
        redelivered = false. Behavior of the lane-1 path is UNCHANGED (anti-storm drop on fresh
        delivery + in-flight mark; blanket same-episode re-fire on redelivery; NakWithDelay
        plumbing) — the 9.1 post-review fixes (metadata guards, NakWithDelay cadences, issue
        keys) must survive the refactor verbatim.
  - [x] The watch-update clearing leg (`clearClosedMarks`) stays as-is; the sweep does not replace
        it (§10.3: clearing runs on each watch update AND each sweep).
- [x] Task 5: Tests (all ACs)
  - [x] **Mid-flight-kill e2e (AC 4, explicit)**: simulate the Actuator dying after CAS-create,
        before publish — pre-create the mark via `markStore` (short lease, ~1s; tiny
        `SweepInterval`) against a violating fixture row, then start the engine: the lane-1 fresh
        delivery anti-storm-drops (no op), the sweep observes lease expiry → reclaims →
        re-dispatches. Assert exactly one op lands on `ops.<lane>`, with a requestId ≠ the dead
        episode's, and the mark is re-created with a fresh lease. Never permanently wedged.
  - [x] **F5 (lost publish + history-1 coalesce)**: mark exists, op never published, row
        re-upserted fresh (`NumDelivered=1`) → drop; sweep after expiry re-attempts. (Same
        mechanics as mid-flight-kill; assert from the coalesce angle: the re-upsert alone does NOT
        re-fire.)
  - [x] **F6 (coalesced close→reopen)**: completed episode's mark + current row column `true`
        again (the `false` state never delivered) → shadow holds until lease expiry, then the
        sweep re-dispatches a NEW episode. Also the prompt half: current row column `false` →
        sweep deletes within one interval (no lease wait).
  - [x] **F7 (playbook drop + column gone)**: mark exists; update the target spec without the gaps
        key AND project the row without the column (and the nil-row variant: tombstone the row) →
        sweep deletes the mark; `marksInFlight` returns to 0; a later spec re-adding the column
        dispatches fresh, unshadowed.
  - [x] **F8 (removed target leftovers)**: tombstone the target vertex with marks standing →
        sweep (second pass — assert the first-pass warm-up skip) deletes `<targetId>.` marks;
        re-install the same targetId → `DeliverLastPerSubject` replay dispatches fresh,
        not shadowed.
  - [x] **Legacy 9.1 mark** (no `leaseExpiresAt`): treated as expired → reclaimed.
  - [x] **Sweep/handler race**: `KVDeleteRevision` conflict (fresh mark CAS-created between sweep
        read and delete) → skipped, fresh episode intact (unit-level with constructed state).
  - [x] **TTL backstop**: unit-assert the create writes TTL = 2 × `MarkLease`; substrate-level
        expiry proven in Task 1's test (a "dead reconciler" e2e is the substrate test + the
        constant — do not build a long-sleep e2e).
  - [x] Regression: the full 9.1 e2e suite stays green (HappyPath, AntiStorm, AssignTask,
        ReconcileTeardownAndReinstall, InstallValidations, NudgeStub) — the refactor must not move
        observable behavior.
- [x] Task 6: Documentation + verification gates
  - [x] `docs/components/weaver.md` (same commit): the §10.3 status row — mark now ships the full
        frozen shape (TTL backstop ×2 lease + active sweep + level-reconciled clearing on both
        legs); the Actuator row's "until 9.2's lease re-attempts it" interim language; the failure-
        modes row "(Test: kill mid-flight.)" now real; note the §10.3 re-fire idempotency posture
        (nudge-safe-via-claimId arrives with Epic 10; triggerLoom/assignTask rare-double,
        operator-visible via sweep Warn + metrics) and the Epic 10 note that a nudge mark carries
        its claimId atomically — an empty claimId on a nudge mark is corrupt → alert, never mint
        (documented now, BUILT in Epic 10).
  - [x] Gates, all green: `go build ./...`, `make vet`, `golangci-lint run ./...`,
        `make verify-kernel`, `make test-bypass` (Gate 2, all BLOCKED),
        `make test-capability-adversarial` (Gate 3, all DEFENDED),
        `go test ./internal/weaver/... ./internal/substrate/... ./internal/bootstrap/...`
        (and `./internal/loom/...` untouched-green as the regression net).

## Dev Notes

### Adjudicated decisions (binding — encode, do not re-litigate)

1. **TTL strictly > lease (backstop ×2), `leaseExpiresAt` = claimedAt + lease.** §10.3 makes the
   reconciler the *prompt reclaim* and the TTL the *backstop*; a sweep can only re-attempt a gap
   whose key still exists past its lease, and nothing watches `weaver-state` (a raw TTL deletion
   unwedges but cannot re-attempt). Exact TTL==lease would make the re-attempt leg unreachable.
   "Mirrored by `leaseExpiresAt`" = the value field exposes the lease the TTL backstops.
2. **The sweep is the primary reclaim lane — do NOT build a marker-watcher.** No consumer on the
   `weaver-state` backing stream, no reaction to TTL delete markers. §10.3 mandates TTL-backstop +
   ACTIVE sweep; the loom `deadline.<instanceId>` MaxAge-marker-watcher pattern is the wrong shape
   here.
3. **Reclaim = delete expired mark + fresh episode.** New CAS-create → new revision → NEW
   requestId (a real re-dispatch, not a Contract #4 collapse). `triggerLoom`/`assignTask`
   re-fire is the §10.3 documented rare-double (lease ≫ remediation latency makes it rare; a
   duplicate task is operator-visible); the read-before-act op-tracker probe is **Phase-3
   hardening — do not build it**. The same-episode same-requestId re-fire remains the lane-1
   REDELIVERY path only (9.1, unchanged).
4. **`claimId` is shape-only in 9.2.** Empty/absent for `triggerLoom`/`assignTask`/`directOp`.
   Epic 10's nudge mints it atomically with the CAS-create; the corrupt-empty-claimId-on-nudge-mark
   alert rule (§10.3: alert, never mint a fresh id) is DOCUMENTED in weaver.md now and BUILT in
   Epic 10. The nudge stub still creates no marks.
5. **Plan-before-delete ordering on reclaim** (Task 3) — a failed plan leaves the expired mark for
   the next sweep, because the sweep enumerates marks: a markless open gap is invisible to it until
   the next row delivery.
6. **Sweep deletes are revision-conditioned** (`KVDeleteRevision`); conflict = a fresh episode won
   a race = skip. Lane-1's `clearClosedMarks` keeps its unconditional `KVDelete` (a level
   reconcile against the delivered row — unchanged from 9.1).
7. **Legacy 9.1 marks (no lease) read as expired.** They carry no TTL either; without this they are
   immortal. First post-deploy sweep reclaims them.
8. **Registry warm-up: first sweep pass skips the target-not-installed orphan leg.** The source
   replays history asynchronously with no replay-done signal (`substrate.SubscribeKVChanges`
   exposes none); a boot-instant "orphan" verdict is unreliable. Level-clearing (row-driven) and
   lease-expiry legs are unaffected.
9. **One authorized substrate edit**: `KVCreateWithTTL` + its test. `KVPutWithTTL` is not CAS and
   must not be used for marks (it would clobber a concurrent winner — the OCC is the create).
10. **Module boundary unchanged**: `internal/weaver` imports only `substrate/*`
    (`boundary_test.go` enforces); zero domain literals outside tests; no new domain knowledge in
    the engine — lease/sweep are generic mechanics.
11. **No pause-awareness in the sweep** — 9.4 wires disable/revoke; the sweep here acts on all
    marks regardless of consumer pause state.

### Grounding map (read these before writing code)

- `docs/contracts/10-orchestration-surfaces.md` (FROZEN): **§10.3 `weaver-state`** (~lines 236–262:
  full value shape `{ targetId, entityKey, gap, action, claimId?, claimedAt, leaseExpiresAt,
  heldBy? }`; passive+active lease enforcement; level-reconciled clearing on watch update AND
  sweep; claimId-atomic-with-mark rule; re-fire idempotency by action). §10.3 `weaver-claims`
  (~264–290) is Epic 10 background only. Contracts are FROZEN — a genuine gap goes to
  `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md`, never an edit.
- `_bmad-output/implementation-artifacts/9-1-weaver-target-lens-violation-lane.md` — the
  predecessor: Adjudicated decisions (esp. 1/2/7/8), Completion Notes (retry-vs-anti-storm
  `NumDelivered` disambiguation; candidate-column clearing enumeration; the "stray mark … left for
  9.2's reconciler sweep" note), and the **Post-review fix batch** (NakWithDelay cadences, issue
  keys, metadata guards — must survive the Task 4 refactor).
- `_bmad-output/implementation-artifacts/9-1-cr-edge-case-hunter.md` — **F5/F6/F7/F8** (~lines
  85–152): the catalogued stale-mark escapes this story's sweep must close; each has a Task 5
  test.
- `internal/weaver/state.go` — `mark` struct (lease fields already declared omit-empty),
  `markStore.create/get/delete/countInFlight`, `markKey`.
- `internal/weaver/evaluator.go` — `handleRow`, `dispatchGap` (the refactor seam), `fire`,
  `clearClosedMarks`, `markCandidateColumns`, `boolColumn`, issue keys.
- `internal/weaver/engine.go` — `Config`/`withDefaults` (add MarkLease/SweepInterval),
  `Start` (ticker anchor, beside the heartbeater), `singleTokenPattern` validation style.
- `internal/weaver/health.go` — `heartbeater.emit` metrics block (`marksInFlight` precedent for
  the sweep counters), `issueCache`.
- `internal/weaver/registry.go` — `targetSource.target/targetIDs` (installed-target lookup for the
  orphan leg; note: NO replay-done signal exists — decision 8).
- `internal/substrate/kv.go` — `KVCreate` (~line 96, error mapping to mirror), `KVPutWithTTL`
  (~line 143, the Nats-TTL precedent and why it is NOT the shape here — no CAS), `KVDeleteRevision`,
  `KVGet`, `KVListKeys` (live-keys-only).
- nats.go v1.52.0 `jetstream/kv.go` `Create(ctx, key, value, opts ...KVCreateOpt)` +
  `jetstream/kv_options.go` `KeyTTL` — the per-key-TTL create the substrate wrapper exposes.
- `internal/bootstrap/primordial.go` — `weaver-state` is already TTL-provisioned
  (`LimitMarkerTTL = 1s` → AllowMsgTTL; ~lines 88–94). NO bootstrap changes in this story.
- `internal/loom/state.go` (~line 370) + `internal/loom/engine.go` (~lines 62–74, 145) — the
  per-key-TTL deadline precedent: the ≥1s NATS floor guard comment style, KVPutWithTTL usage
  (loom's deadline is Put-shaped; weaver's mark is Create-shaped — hence Task 1).
- `internal/weaver/weaver_e2e_test.go` — embedded-NATS harness + fixture-row writer + op recorder
  the new e2e tests extend; `provision` already creates a TTL-capable `weaver-state`.
- `docs/components/weaver.md` — status table rows for Dispatch OCC/Actuator (~lines 22–23) and the
  failure-modes row (~line 200) to update in the same commit.

### Out of scope — do NOT pull in

- **Two-Phase Nudge / `weaver-claims` writes / claimId minting / the corrupt-claimId ALERT
  implementation** → Epic 10 (document the rule only).
- **Check-before-act re-fire probe (op-tracker read before re-dispatch)** → Phase-3 hardening
  per §10.3; the rare-double is the accepted, documented posture.
- **Temporal lane** → 9.3. **Control API / pause-aware sweep / revoke clearing marks** → 9.4
  (9.4's revoke will clear `<targetId>.` marks via its own path; the sweep's orphan leg is the
  backstop, not the 9.4 surface).
- **Marker-watcher lane on `weaver-state`** — forbidden (decision 2).
- No bootstrap/provisioning changes (`weaver-state` TTL capability exists); no edits to
  `internal/loom`, `internal/refractor`, `internal/processor`, `docs/contracts/*`,
  `_bmad-output/planning-artifacts/*`.
- No per-message KV scans (sweep is interval-cadence; heartbeat scan unchanged); no sprint tooling.

### House rules (binding, from CLAUDE.md)

- **NO history/changelog comments** — no `// Story 9.2`, `// 9.1 shipped without…`, `// was
  plain KVCreate`. Comments describe what the code does NOW; godoc may cite contracts
  ("Contract #10 §10.3").
- Key shapes per Contract #1; sub-agents never commit/push/branch — leave the tree for Winston;
  new docs → `/docs`.

### Project Structure Notes

- New: `internal/weaver/reconciler.go` (+ `reconciler_internal_test.go`); new substrate test file
  or extension of the existing kv test.
- Modified: `internal/substrate/kv.go` (KVCreateWithTTL), `internal/weaver/state.go` (lease/TTL/
  heldBy on create), `internal/weaver/engine.go` (Config knobs + sweep start),
  `internal/weaver/evaluator.go` (dispatch-core extraction), `internal/weaver/health.go` (sweep
  metrics), `internal/weaver/weaver_e2e_test.go` (new scenarios), `docs/components/weaver.md`.
- `internal/weaver` stays flat; the sweeper is engine-internal (no exported surface — 9.4 builds
  the operator surface).

### Previous story intelligence (9.1, done)

- The mark struct already declares `ClaimID`/`LeaseExpiresAt`/`HeldBy` omit-empty — extend the
  WRITE path, no migration. Marks are never updated after create (the read revision IS the episode
  tag); the sweep's reclaim preserves this: delete + fresh create, never an in-place update.
- The lane-1 redelivery path re-fires the SAME episode requestId; only a mark
  delete + re-create makes a NEW episode. Keep these two re-fire classes distinct — collapsing
  them was the wedge F5 catalogued.
- 9.1's review found the handler/reconcile seams (not the supervisor core) are where Majors
  cluster; the sweep is exactly such a seam — the F5–F8 tests are the named regression net.
- `boolColumn`'s conservative non-bool handling and the issue-key scheme
  (`gap:`/`data:`/`consumer:` prefixes) are post-review fixes — reuse, don't fork.
- The 9.1 Completion Notes explicitly deferred "a stray mark at a column dropped from BOTH the
  playbook and the row" to this sweep — that is AC 2's orphan clause (F7).

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 9.2 (amended 2026-06-12)]
- [Source: docs/contracts/10-orchestration-surfaces.md §10.3 weaver-state]
- [Source: _bmad-output/implementation-artifacts/9-1-weaver-target-lens-violation-lane.md]
- [Source: _bmad-output/implementation-artifacts/9-1-cr-edge-case-hunter.md F5–F8]
- [Source: docs/components/weaver.md (status table + failure modes)]
- [Source: internal/weaver/{state,evaluator,engine,health,registry}.go]
- [Source: internal/substrate/kv.go; nats.go v1.52.0 jetstream KV KeyTTL]
- [Source: internal/bootstrap/primordial.go (weaver-state LimitMarkerTTL)]
- [Source: internal/loom/state.go (deadline TTL precedent)]

## Dev Agent Record

### Agent Model Used

claude-fable-5 (Fable 5), bmad-dev-story sub-agent, 2026-06-12.

### Debug Log References

- Internal-test harness fix: `evaluator_internal_test.go` provisioned `weaver-state` without
  `LimitMarkerTTL`; the now-TTL-bearing mark create was rejected by the server (NakWithDelay).
  Harness updated to mirror bootstrap provisioning (`LimitMarkerTTL: 1s`). No production-code
  issue — the e2e harness and bootstrap were already TTL-capable.
- golangci-lint: one unused test helper removed (`sweepHarness.unseedTarget`).

### Completion Notes List

- **Task 1**: `substrate.KVCreateWithTTL` wraps `kv.Create(ctx, key, value, jetstream.KeyTTL(ttl))`
  — tombstone-aware CAS preserved, same error mapping as `KVCreate`, `ttl <= 0` falls back to plain
  create (KVPutWithTTL posture). `TestKVCreateWithTTL` proves CAS conflict, real server-side expiry
  (poll to `ErrKeyNotFound`), create-after-soft-delete with TTL, and the ttl<=0 fallback. No other
  substrate changes were needed.
- **Task 2**: `markStore.create` writes the full §10.3 value shape (`leaseExpiresAt = claimedAt +
  lease`, `heldBy = instance`) with per-key TTL = `markTTLBackstopFactor (2) × MarkLease`; the
  backstop-vs-prompt-reclaim rationale lives as a present-tense invariant on the constant in
  `state.go` (adjudicated decision 1). `Config.MarkLease` (default 30m, clamped to the 1s NATS
  per-key TTL floor in `withDefaults`, loom-StepTimeout guard style) and `Config.SweepInterval`
  (default 1m, <=0 takes default) added. `ClaimID` stays empty (decision 4) — asserted in
  `TestMarkCreate_TTLBackstop`, which also asserts the wire `Nats-TTL` header equals 2×lease.
- **Task 3**: `internal/weaver/reconciler.go` — single-goroutine sweeper (immediate first pass +
  ticker; passes inherently serialized), started in `Engine.Start` beside the heartbeater. Legs:
  (a) corrupt key/value → `CorruptMark` alert + revision-conditioned delete; (b) level-reconciled
  clearing (row gone / column not true → delete; unparseable row → leave, never delete on
  unreadable evidence); (c) live lease → leave; (d) expired/absent/unparseable lease → reclaim
  (plan-before-delete, fresh episode via the shared dispatch core) or orphan delete-only (target
  removed / playbook lacks gap). First pass skips the target-uninstalled leg (decision 8). All
  deletes are `KVDeleteRevision` at the read revision; conflict = fresh episode won = skip. Warn
  logs with targetId/entityId/gap/action/reason on every reclaim/orphan delete; gapClosed
  level-clears log at Info (routine reconcile, same class as lane-1's clearClosedMarks). Sweep
  counters (`sweepReclaims`, `sweepOrphansDeleted`, `sweepCorrupt`, `sweepLastRunAt`) surface in
  the heartbeat beside `marksInFlight`.
- **Task 4**: `dispatchGap` split into `planGap` (L2+Strategist plan + error routing — issue keys,
  NakWithDelay cadences verbatim) and `fireEpisode` (mark get / in-flight / CAS-create / fire).
  Lane-1 passes `redelivered = msg.NumDelivered != 1`, preserving the 9.1 post-review
  NumDelivered-0 metadata guard exactly (0 re-fires, never drops); the `msg.Sequence == 0` guard
  stays in the lane-1 wrapper. The sweep passes the row `KVGet` revision + redelivered=false.
  `clearClosedMarks` untouched. Full 9.1 suite green unmodified (only the harness provisioning
  line changed — see Debug Log).
- **Task 5 test map**: AC4 mid-flight-kill → `TestWeaverE2E_MidFlightKill` (also the F5 coalesce
  angle: fresh re-upsert does not re-fire; exactly-one-op; fresh lease + heldBy asserted). F5/F6
  shadow-half + new-requestId proof → `TestSweep_ReclaimExpired` (asserts old/new
  `deriveEpisodeRequestID`). F6 prompt half + F7 row variants (column false / column absent / row
  gone / unparseable row) → `TestSweep_LevelClear`. F7 playbook-drop + re-add-unshadowed →
  `TestSweep_OrphanColumn`. F8 + warm-up skip → `TestSweep_WarmUpGuardAndOrphanTarget` (unit) and
  `TestWeaverE2E_SweepOrphanedTargetMarks` (engine-level: heartbeat-observed first pass, reinstall
  replay dispatch). Legacy 9.1 mark → `TestSweep_LegacyMarkReclaimed`. Sweep/handler race →
  `TestSweep_DeleteRevisionRace`. Live lease respected → `TestSweep_LeaseUnexpired`.
  Plan-before-delete ordering (decision 5) → `TestSweep_PlanFailureLeavesMark`. TTL backstop →
  `TestMarkCreate_TTLBackstop` (header) + `TestKVCreateWithTTL` (substrate expiry); no long-sleep
  e2e built, per the story.
- **Task 6**: `docs/components/weaver.md` updated in the same change set (status header 9.1–9.2;
  Dispatch OCC row now full §10.3 shape incl. the Epic 10 claimId-atomic/corrupt-alert rule; new
  Reconciler-sweep row documenting the §10.3 re-fire idempotency posture and rare-double
  visibility; Actuator row interim language removed; Health metrics row; In/Out weaver-state row;
  failure-modes "kill mid-flight" row now cites the real test).
- **Gates (all green)**: `go build ./...` OK; `make vet` OK; `golangci-lint run ./...` 0 issues;
  `make verify-kernel` ALL ASSERTIONS PASSED; `make test-bypass` all BLOCKED (PASS); `make
  test-capability-adversarial` all DEFENDED — Gate 3 PASSED 4/4; `go test -count=1
  ./internal/weaver/... ./internal/substrate/... ./internal/bootstrap/...` ok (27.3s / 6.8s /
  17.7s); `./internal/loom/...` untouched-green ok (40.1s).
- No contract gaps found; no amendment request raised. `internal/loom`, `internal/refractor`,
  `internal/processor`, `docs/contracts/*`, `_bmad-output/planning-artifacts/*`, bootstrap
  provisioning: untouched.

### Post-review fix batch (2026-06-12, adjudicated by Winston from the 3-layer review)

- **BH-1/ECH-1 (reclaim atomicity)**: the delete-then-recreate reclaim is replaced with a single
  revision-conditioned **in-place replace** — new substrate primitive `KVUpdateWithTTL`
  (revision-conditioned publish to the KV subject composing
  `jetstream.WithExpectLastSequencePerSubject` + `jetstream.WithMsgTTL`, the same two options the
  KV layer's own Create/Update compose internally; nats.go v1.52.0's `kv.Update` hardcodes
  `ttl=0`, so the stock Update cannot carry KeyTTL) + `markStore.replace`. The mark key is never
  absent across a reclaim; a conflict (fresh episode / TTL marker) skips; the fresh episode's
  `requestId` derives from the replace revision; the per-key TTL is re-armed (asserted on the
  wire `Nats-TTL` header in `TestSweep_ReclaimExpired`; expiry-after-update proven in
  `TestKVUpdateWithTTL`). The publish-failure Warn now correctly describes the fresh-lease retry
  in all sub-cases (the mark exists before the publish is attempted). The sweep no longer routes
  through `fireEpisode` (lane-1 only now).
- **ECH-2/ECH-3/BH-3 (warm-up)**: the one-pass-count proxy is replaced with
  `Config.SweepOrphanWarmup` (default 5m, clamped ≥ SweepInterval) — a wall-clock
  registry-replay-readiness proxy gating BOTH orphan legs (target uninstalled AND
  playbook-lacks-column); expired-lease reclaim and level clearing stay ungated
  (`TestSweep_WarmUpGuardAndOrphanTarget` reworked to cover all three).
- **BH-4 (interval clamp)**: `withDefaults` clamps `SweepInterval ≤ MarkLease` (Warn), so an
  expired mark is always observed before the 2×lease TTL fires (`TestConfigClamps`).
- **BH-5 (violating gate)**: the reclaim dispatches only when the row's `violating` is true
  (boolColumn semantics, mirroring lane-1's L1 gate); a non-violating row with an open missing_*
  leaves the mark to level-clearing/next CDC (`TestSweep_NonViolatingRowNotReclaimed`).
- **BH-6/BH-7/ECH-4 (corrupt/entityKey legs)**: `deleteCorrupt` alerts AFTER a successful delete;
  the CorruptMark issue is retired by the next completed pass that no longer lists the key; a
  violating row with missing/empty `entityKey` routes its expired mark through the corrupt leg
  (alert + delete — no immortal re-alerting legacy marks), with the per-mark `sweep:` issue key
  (no cross-entity collision) (`TestSweep_CorruptMark`, `TestSweep_MissingEntityKeyMarks`).
- **AA-F1 (hygiene)**: the `putMark` comment reworded present-tense.
- **CAR raised**: `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md` — §10.3 per-key TTL = 2× lease
  rather than a literal mirror of `leaseExpiresAt` (PENDING ratification, Andrew); formalizes
  Question 1 below.
- **Accepted as-is (no change)**: lane-1-redelivery vs reclaim rare-double (§10.3 documented
  bound); `sweepReclaims` counting semantics; TTL bucket provisioning (already in primordial).

### File List

- internal/substrate/kv.go (modified — KVCreateWithTTL, KVUpdateWithTTL)
- internal/substrate/substrate_test.go (modified — TestKVCreateWithTTL, TestKVUpdateWithTTL)
- internal/weaver/state.go (modified — markTTLBackstopFactor, lease/heldBy/TTL on create,
  markStore.replace)
- internal/weaver/engine.go (modified — MarkLease/SweepInterval/SweepOrphanWarmup config +
  clamps, sweeper wiring)
- internal/weaver/evaluator.go (modified — planGap/fireEpisode extraction)
- internal/weaver/health.go (modified — sweep heartbeat metrics)
- internal/weaver/reconciler.go (new — the §10.3 reconciler sweep)
- internal/weaver/reconciler_internal_test.go (new — sweep unit suite)
- internal/weaver/evaluator_internal_test.go (modified — harness weaver-state LimitMarkerTTL)
- internal/weaver/weaver_e2e_test.go (modified — MidFlightKill + SweepOrphanedTargetMarks e2e)
- docs/components/weaver.md (modified — §10.3 status rows)
- cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md (new — §10.3 TTL=2×lease amendment, PENDING)

### Change Log

- 2026-06-12: Story 9.2 implemented — §10.3 mark hardening (lease + 2× TTL backstop), reconciler
  sweep with level-clearing/orphan/corrupt/expired-lease legs, dispatch-core extraction for sweep
  reuse, KVCreateWithTTL substrate primitive, full test suite, weaver.md status update. All
  verification gates green. Status → review.
- 2026-06-12: Post-review fix batch (BH-1/ECH-1 reclaim in-place replace via KVUpdateWithTTL,
  ECH-2/3/BH-3 SweepOrphanWarmup, BH-4 SweepInterval≤MarkLease clamp, BH-5 violating gate,
  BH-6/7/ECH-4 corrupt/entityKey leg rework, AA-F1 comment, CAR raised in cmd/weaver). All
  verification gates re-run green. Status stays review.

## Questions for Winston (non-blocking — drafted around contract-compliant defaults)

1. **TTL/lease ratio (decision 1):** TTL = 2 × lease so the active sweep — the only actor that can
   *re-attempt* (nothing watches weaver-state) — observes the expired lease before the key
   self-deletes. A strict TTL==leaseExpiresAt reading would make the AC's "reconciler reclaims
   expired leases" unreachable. Confirm the ×2 constant (or name a different factor).
2. **Defaults:** `MarkLease` 30m / `SweepInterval` 1m. The lease bounds BOTH the F6 reopen-shadow
   AND the assignTask duplicate-task cadence (a slow human + expired lease = the §10.3 rare-double
   task) — 30m biases toward fewer duplicates; shorten if convergence promptness matters more.
3. **Registry warm-up guard (decision 8):** first sweep pass skips only the target-not-installed
   orphan leg (no replay-done signal exists on the source). Alternative: add a replay-done signal
   to `substrate.SubscribeKVChanges` — rejected as a second substrate edit this story doesn't need.
4. **Legacy-mark posture (decision 7):** 9.1-era marks (no lease, no TTL) read as expired on the
   first sweep → reclaimed (possible one-time rare-double per open episode at deploy). The
   alternative — grandfather them until their gaps close — leaves immortal marks if the gap never
   redelivers.
5. **Corrupt mark handling:** alert + delete (weaver-private bucket; garbage otherwise outlives
   everything). Flag if you want alert-only.
