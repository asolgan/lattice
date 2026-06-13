# Story 9.3: Temporal lane (ADR-51 scheduled messages)

Status: review

## Story

As a platform developer,
I want time-derived violations to surface without polling,
So that freshness rules (e.g. "background check older than N") converge.

## Acceptance Criteria

(Authoritative source: `_bmad-output/planning-artifacts/epics/phase-2-epics.md` → Epic 9 → Story 9.3,
as amended 2026-06-12 to the corrected §10.4 shape — the current epic text governs over any
paraphrase here.)

1. **Given** a resolved entity with a freshness window, **when** the Actuator schedules an
   `@at(resolve+window)` message on the **`core-schedules`** stream (`AllowMsgSchedules`,
   provisioned since 7.4), schedule subject `schedule.weaver.timer.<targetId>.<entityId>`
   (per-target — see Adjudicated decision 3, amended 2026-06-12), with
   `Nats-Schedule-Target: schedule.weaver.timer.fired.<targetId>.<entityId>` — per the **corrected
   §10.4 (2026-06-05)**: the fired target **must lie within `schedule.>`** (the earlier
   `weaver.timer.fired.>` internal-subject notation is obsolete; an out-of-stream target is
   rejected at publish time).
2. **Then** at expiry NATS republishes back into `core-schedules` at the target subject; the
   temporal lane — a **supervised consumer** filtered on `schedule.weaver.timer.fired.>` — submits
   a `MarkExpired` **op** via the Processor (never injected into `core-events`), carrying a
   **deterministic `requestId`** derived from the schedule subject + fire instant so the Contract
   #4 tracker collapses at-least-once redelivery (a redelivered timer never double-acts).
3. **And** CDC re-projects the target; the freshness gap flips violating.
4. **And** re-doing the entity before expiry re-publishes to the same schedule subject,
   **replacing** the prior timer.
5. **And** the schedule is durable across a Weaver restart.

## Tasks / Subtasks

- [ ] Task 1: Substrate — §10.4 schedule-header constants (the one authorized substrate edit) (AC 1)
  - [ ] `internal/weaver` may not import `nats-io/*` (boundary test), so the two ADR-51 header
        names must be reachable through substrate. Add two exported string constants beside
        `Conn.Publish` in `internal/substrate/publish.go`:
        `ScheduleHeader = "Nats-Schedule"` (value: `@at <RFC3339>` one-shot in Phase 2) and
        `ScheduleTargetHeader = "Nats-Schedule-Target"` (the republish target subject — MUST lie
        within the scheduling stream's own subject space; the server rejects an out-of-stream
        target at publish time). Godoc cites Contract #10 §10.4 / ADR-51.
  - [ ] Substrate test pinning both constants to the server's exported values
        (`natsserver.JSSchedulePattern` / `natsserver.JSScheduleTarget` — nats-server is already a
        test dependency); a drifted constant fails the build-time pin, never silently misroutes.
  - [ ] **No publish helper is needed** — `Conn.Publish(ctx, subject, data, header)` already
        carries an optional header map (verified). No other substrate changes; a further substrate
        gap = STOP and write it up in Questions.
- [ ] Task 2: Scheduling leg — the Actuator publishes `@at(freshUntil)` on row updates (AC 1, 4)
  - [ ] **The freshness window lives in the target cypher, never in the engine** (weaver.md
        "Temporal lane", D4): the Lens computes `resolve + window` and projects it as the
        engine-recognized **optional row column `freshUntil`** (RFC3339 string, a §10.2 free-form
        param column by carriage; see Adjudicated decision 1). The engine converts time→op only.
  - [ ] New `internal/weaver/temporal.go`: subject constants
        `scheduleSubjectPrefix = "schedule.weaver.timer."` and
        `firedSubjectPrefix = "schedule.weaver.timer.fired."` (the subject is keyed per
        `<targetId>.<entityId>` — both dot-free tokens: targetId is install-validated, entityId is
        the row's NanoID already validated by `splitRowKey`; never the dotted vertex key, §10.4
        discipline; the reserved `fired` token at the targetId position is refused loudly so a
        pending schedule cannot land inside the fired filter).
  - [ ] In `handleRow` (after `clearClosedMarks`, for non-nil rows, **regardless of `violating`**):
        if `freshUntil` is present — parse RFC3339; truncate to `time.RFC3339` seconds precision so
        the header string and the payload string are byte-identical and the requestId derivation is
        stable — publish via the actuator (a past instant is published verbatim: nats-server stores
        and fires an overdue @at immediately, which is correct immediate-expiry level semantics, and
        the payload's fireAt stays the deadline instant so a re-projected past deadline derives the
        same deterministic requestId):
        subject `schedule.weaver.timer.<targetId>.<entityId>`, headers
        `substrate.ScheduleHeader: "@at " + fireAt` and
        `substrate.ScheduleTargetHeader: "schedule.weaver.timer.fired." + targetId + "." + entityId`, payload
        `{ "entityKey": <row entityKey>, "targetId": <targetId>, "fireAt": <fireAt RFC3339> }`.
        This is **level-driven and idempotent**: every delivery (fresh, redelivery, restart replay
        via DeliverLastPerSubject) re-publishes the same schedule; one-schedule-per-subject replace
        makes the re-publish a no-op-equivalent, and a row re-projected with a NEW `freshUntil`
        (the entity re-done before expiry) **replaces** the prior timer — AC 4 falls out of the
        level design, no edge detection.
  - [ ] Edge handling (FR29 loud-failure discipline, mirroring `boolColumn`):
        `freshUntil` present but non-string/unparseable → Warn + `RowDataError` issue
        (`issueKeyData(targetID, "freshUntil")`), skip scheduling (redelivery cannot fix a
        projected row). `freshUntil` in the past → publish verbatim (nats-server stores and fires
        an overdue @at immediately = correct immediate-expiry; the payload's fireAt stays the
        deadline instant, so a re-projected past deadline collapses on the same deterministic
        requestId at the Contract #4 tracker). `freshUntil` requires the row's `entityKey` — absent
        entityKey on a freshUntil row routes through the same data-error surface.
        A schedule **publish failure** → `NakWithDelay` from `handleRow` (bounded cadence — a
        persistent stream failure must not hot-loop; the redelivery re-runs the whole level pass,
        and any in-flight gap re-fires its same episode requestId, which is the safe side).
  - [ ] Actuator: add `scheduleTimer(ctx, entityID string, payload []byte, fireAt string) error`
        to `actuator.go` (one `conn.Publish` with the two headers — fire-and-forget like `submit`;
        Info log with entityId/fireAt).
- [ ] Task 3: Temporal lane — supervised fired-timer consumer → `MarkExpired` op (AC 2)
  - [ ] Engine `Start` adds ONE static supervised consumer (beside the lane-1 reconcile; mirror
        `targetSpec`): `Name: "weaver-temporal"` (a **fixed** durable — the ack floor IS the
        missed-while-down recovery: fired messages persist in `core-schedules` under limits
        retention and the durable resumes from its floor on restart), `Stream:
        cfg.CoreSchedulesStream`, `FilterSubject: "schedule.weaver.timer.fired.>"`,
        `DeliverPolicy: substrate.DeliverAll`, `Handler: supervisedHandler(e.handleFiredTimer)`,
        `Health: newConsumerHealthSink(… "weaver-temporal" …)` — the per-consumer pause-state doc
        at `health.weaver.<instance>.consumer.weaver-temporal` rides the existing sink.
  - [ ] New `Config.CoreSchedulesStream` (default `"core-schedules"`, the literal mirroring
        `internal/bootstrap.CoreSchedulesStreamName` — kept literal like the bucket defaults so
        weaver does not import bootstrap).
  - [ ] `handleFiredTimer` (in `temporal.go`): the fired message's subject IS the target subject —
        recover `<targetId>.<entityId>` by trimming `firedSubjectPrefix` and `splitRowKey`;
        **reconstruct the schedule subject** as `scheduleSubjectPrefix + targetId + "." + entityId`
        (never trust a payload-carried subject). Parse the payload `{entityKey, targetId, fireAt}`;
        a payload targetId that disagrees with the subject-derived targetId → TimerDataError + Ack
        (foreign/corrupt publish into `schedule.>`). Malformed payload / missing `entityKey` or
        `fireAt` / non-NanoID subject tail → Warn + keyed Health issue (`timer:<targetId>`, or
        `timer:` for an unsplittable subject — keyed per target so the issue cache is bounded, not
        per-entity; the message carries the latest offender) + **Ack** (redelivery cannot fix a
        stored payload; never silent — FR29). Before submitting, **read-before-act**: the
        weaver-targets row must still exist (absent → Ack — covers a deleted entity and a removed
        target whose Lens stopped projecting), and the row's current `freshUntil` must not be
        strictly LATER than the firing's instant (renewed-while-down → Ack, stale firing
        suppressed). A present row whose target is NOT in the registry cache → NakWithDelay (NOT
        Ack): the registry replays asynchronously at startup with no replay-done signal, and the
        temporal consumer can deliver a missed-while-down firing before that replay lands —
        Ack-dropping would irreversibly discard a valid firing during the startup window, so the
        bounded-cadence retry is the safe side (a genuinely-removed target retries only until its
        Lens clears the row). This read-before-act is the only stale-firing guard on the mark-less
        leg and makes a durable-replay harmless.
  - [ ] Submit the op via the existing actuator path: `operationType: opMarkExpired`
        (`const opMarkExpired = "MarkExpired"` beside `opStartLoomPattern` in `strategist.go` —
        the epic names the op; platform vocabulary, not domain knowledge), payload
        `{ "entityKey": …, "targetId": …, "expiredAt": <fireAt> }`, **no authContext**
        (Weaver's service-actor authority, like a target-less directOp — Adjudicated decision 5),
        `requestId = deriveTimerRequestID(scheduleSubject, fireAt)` — a deterministic NanoID via
        the existing `deriveID` (namespace `"timer:"`, seed `scheduleSubject + "\x00" + fireAt`,
        revision 0). §10.4: the requestId derives from the **schedule subject + fire instant**, so
        an at-least-once redelivery of the same firing reuses the same requestId and collapses on
        the Contract #4 tracker, while a NEW firing of a re-armed timer (new fireAt) is a genuinely
        new op. Publish failure → `NakWithDelay` (bounded cadence — a core-operations outage must
        not hot-loop; the redelivery re-derives the same requestId, which collapses on the
        Contract #4 tracker).
  - [ ] **No weaver-state mark for the temporal conversion** — the §10.4 deterministic requestId
        is the dedup; the mark/OCC machinery belongs to remediation dispatch (lane-1 picks it up
        after the row flips violating). Do not CAS-create anything in this handler.
- [ ] Task 4: Health — heartbeat counters (AC 2 observability)
  - [ ] `temporal.go` keeps two since-start atomic counters: `timersScheduled` (Task 2 publishes)
        and `timersFired` (Task 3 ops submitted). Surface both in `heartbeater.emit` beside the
        sweep counters (extend `newHeartbeater` with the temporal handle, the `sweep` precedent).
- [ ] Task 5: Tests (all ACs)
  - [ ] **Verified groundwork (2026-06-12, story-prep probe):** the embedded test server
        (`natstest.RunServer`, nats-server v2.14.0, JetStream on) **accepts schedule-header
        publishes AND fires `@at` schedules** (~1.5s for a +2s schedule; fired message lands in the
        stream at the target subject and a filtered durable fetches it). Story 7.4's
        "embedded NATS may not run the scheduler" caution is obsolete — **the full loop e2e runs on
        the existing embedded harness**; no Docker-stack-only skip-guarded test is required. If the
        dev observes otherwise, STOP and re-verify before restructuring the tests.
  - [ ] Harness: `provision` in `weaver_e2e_test.go` additionally creates `core-schedules`
        (`Subjects: ["schedule.>"]`, `AllowMsgSchedules: true`, `MaxMsgsPerSubject: 1`,
        FileStorage/LimitsPolicy — mirror `internal/bootstrap/primordial.go`'s config).
  - [ ] **E2E happy loop (AC 1–3)**: install a target; fixture row `violating: false` with
        `freshUntil = now+2s` → assert the schedule message lands in `core-schedules` at
        `schedule.weaver.timer.<entityId>` with the `@at` + target headers (read back via a
        filtered JetStream consumer in the test) → wait for the fire → assert exactly one
        `MarkExpired` op on `ops.<lane>` with the §10.4-derived requestId, payload
        `{entityKey, targetId, expiredAt}`, no authContext → fixture then re-projects the row
        `violating: true, missing_<gap>: true` (the "CDC re-projects" leg is fixture-driven,
        mirroring 9.1 — Refractor wiring is Epic 11) → lane-1 dispatches the remediation
        (mark + op), proving the time→op→violation→remediation chain.
  - [ ] **Replace-on-reschedule (AC 4)**: row with `freshUntil = now+2s`, then BEFORE expiry
        re-project the same row with `freshUntil = now+4s` → assert NO firing in the first window
        and exactly ONE `MarkExpired` overall, carrying `expiredAt` = the second instant (the
        7.4 smoke test's tightened replace assertion, ported).
  - [ ] **Restart durability (AC 5)**: schedule via a row (`freshUntil = now+3s`), cancel the
        engine ctx (full stop), start a FRESH engine (same server) → the timer fires and the new
        engine's `weaver-temporal` durable converts it — exactly one op. (NATS holds the schedule;
        the fixed durable resumes from its ack floor.)
  - [ ] **Missed-while-down**: schedule, stop the engine, let the timer fire while no engine runs,
        restart → the durable picks the stored fired message up; exactly one op.
  - [ ] **Redelivery dedup (AC 2)**: unit-level (`temporal_internal_test.go`, the
        `evaluator_internal_test.go` constructed-Message harness pattern): the same fired message
        delivered twice (NumDelivered 1 then 2) derives the SAME requestId; distinct fireAt →
        distinct requestId; `deriveTimerRequestID` determinism pinned (the
        `requestid_internal_test.go` style).
  - [ ] **Edges (unit)**: malformed fired payload → Ack + Health issue, no op; missing
        entityKey/fireAt → same; non-bool/unparseable `freshUntil` → RowDataError issue, no
        schedule publish; past `freshUntil` → no publish, no issue; tombstone row → clearing runs,
        no schedule publish, no purge attempted.
  - [ ] **Regression net**: the full existing weaver suite green unmodified (HappyPath, AntiStorm,
        AssignTask, ReconcileTeardownAndReinstall, InstallValidations, NudgeStub, MidFlightKill,
        SweepOrphanedTargetMarks + the internal suites) — 9.1/9.2 behavior must not move; plus
        `./internal/loom/... ./internal/substrate/... ./internal/bootstrap/...`.
- [ ] Task 6: Documentation + verification gates
  - [ ] `docs/components/weaver.md` (same commit): status table — Lane 3 row flips to shipped
        (supervised `weaver-temporal` durable on `core-schedules`, `freshUntil` convention,
        deterministic timer requestId, no-mark posture, accepted bounds from the decisions below);
        **rewrite the stale "Temporal lane" section** to the corrected §10.4 reality (it still
        shows `lattice-schedules` and the obsolete out-of-stream `weaver.timer.fired.>` republish —
        the contract correction of 2026-06-05 governs); the pipeline/lanes table lane-3 row.
  - [ ] `docs/components/scheduling.md` (same commit): fix the **out-of-stream target drift** —
        the publish example (`"weaver.timer.fired."+entityID`) and the "Choosing the republish
        target subject" section still show targets OUTSIDE `schedule.>`, which the server rejects
        at publish time; correct both to `schedule.weaver.timer.fired.<entityId>` + the
        filtered-JetStream-consumer consumption pattern (the smoke test already demonstrates it).
  - [ ] Gates, all green: `go build ./...`, `make vet`, `golangci-lint run ./...`,
        `make verify-kernel` (count unchanged — nothing primordial moves this story),
        `make test-bypass` (Gate 2, all BLOCKED), `make test-capability-adversarial` (Gate 3, all
        DEFENDED), `go test ./internal/weaver/... ./internal/substrate/...
        ./internal/bootstrap/...` (and `./internal/loom/...` untouched-green).

## Dev Notes

### Adjudicated decisions (binding — encode, do not re-litigate; flagged for Winston in Questions)

1. **WHO schedules — the row, level-reconciled (the minimal fixture-proven shape).** The epic's
   "Actuator schedules @at(resolve+window) on resolve" is grounded thus: *the freshness rule lives
   in the target cypher* (weaver.md "Temporal lane"; the brief's pin: "the temporal lane only
   converts time→op"), so the Lens computes `resolve + window` and projects the deadline as the
   optional row column **`freshUntil`** (RFC3339); the engine's row handler — the watch-update path
   it already owns — has the Actuator publish `@at(freshUntil)` on every delivery where the instant
   is future. "On resolve" is exactly when the Lens re-projects the row with a fresh deadline; "the
   Actuator schedules" is literal (D4: "the Actuator publishes an `@at` scheduled message").
   Re-publishing per delivery is idempotent under one-schedule-per-subject replace, restart replay
   re-arms for free, and no edge/no in-memory state is introduced. In Phase 2 the Lens is the test
   fixture writing §10.2 rows (the 9.1 posture); Epic 11's lease-signing cypher takes over the
   `freshUntil` production.
2. **`freshUntil` is an engine-recognized convention column**, carried as a §10.2 free-form param
   column (the frozen column list is untouched). It joins the `missing_*` class of engine-read
   conventions — if Winston judges this a §10.2 seam addition worth pinning, it is an
   **annotation-class CAR** (the 7.4 precedent), not a blocker; the story documents the convention
   in weaver.md either way. Do NOT add a config knob for the column name.
3. **Schedule subject is per-target-per-entity** (`schedule.weaver.timer.<targetId>.<entityId>`,
   amended 2026-06-12 — the epic AC and §10.4 template were widened by a publisher-chosen
   `<targetId>` token; CAR Request 2). Two installed targets projecting `freshUntil` for the SAME
   entity now hold INDEPENDENT timer slots — no cross-target last-write-wins on the shared
   `MaxMsgsPerSubject: 1` rollup. Both tokens are dot-free (targetId install-validated, entityId the
   row NanoID). Within a single target, a re-projected `freshUntil` for the same entity still
   replaces on the one subject (AC 4). Phase 2's demo has one target; no cross-target
   earliest-wins coordination is built.
4. **No weaver-state mark, no lease, for the temporal conversion.** §10.4's deterministic
   requestId (schedule subject + fire instant) is the dedup for the fired→op leg; marks/OCC remain
   remediation-dispatch machinery (lane-1, after the violation flips). Corollary (documented
   bound): a `MarkExpired` op **rejected at the Processor** is not re-attempted by Weaver (fire-
   and-forget, nothing leases it) — the freshness flip then waits for the next CDC touch of the
   entity. Operator-visible at the Processor; accepted for Phase 2. A *publish* failure IS retried
   (Nak → same requestId). Related stream nuance (accepted, document in weaver.md): with
   `MaxMsgsPerSubject: 1` on `core-schedules` (7.4), a NEWER firing at the same fired subject
   rolls up an older one the consumer has not yet processed — only the latest conversion is
   delivered, which is level-correct (the newest `expiredAt` supersedes; both would poke the same
   entity).
5. **`MarkExpired` carries no `authContext`** — submitted under Weaver's service-actor authority
   (the target-less `directOp` posture). The op's DDL/grants are package data (Epic 11); the
   engine pins only the envelope. `opMarkExpired` is a strategist-style constant, not config.
6. **No cancel/purge.** An entity deleted (tombstone row) or a target removed while a timer is
   pending leaves the timer armed; when it fires, the handler's read-before-act guard re-reads the
   current weaver-targets row and Acks without submitting (unregistered target, or absent/renewed
   row) — so the stray fire is suppressed at the engine, not merely rejected downstream.
   `Nats-Schedule-Next: purge` mechanics are scheduler-internal and not built here. Documented in
   weaver.md.
7. **Past-`freshUntil` rows publish verbatim** (amended 2026-06-12 — the past-instant skip was
   removed). nats-server 2.14 stores an overdue `@at` and fires it immediately (verified by the ECH
   ledger and the e2e); an immediate firing of an already-elapsed freshness deadline IS correct
   level semantics (immediate expiry). The published payload's `fireAt` stays the deadline instant
   (not "now"), so a re-projected past deadline derives the SAME deterministic requestId and the
   Contract #4 tracker collapses the duplicate. The sub-second-truncation concern the old guard
   carried disappears with it.
8. **Fixed durable `weaver-temporal`** (not per-instance): the ack floor is the
   missed-while-down recovery and must survive restart. Phase 2 is single-instance (the lane-1
   per-target durables make the same assumption); multi-instance fan-out is a Phase-3 concern.
9. **One authorized substrate edit**: the two header-name constants + their pin test (Task 1).
   `Conn.Publish` already takes headers — do not add a schedule-publish helper, do not route
   around the supervisor for the fired consumer.
10. **Module boundary unchanged**: `internal/weaver` imports only `substrate/*`
    (`boundary_test.go` enforces); zero domain literals in the engine (`MarkExpired`,
    `schedule.weaver.timer.*`, `freshUntil` are platform/engine vocabulary, not domain words —
    domain literals like `missing_bgcheck` stay in tests).
11. **Scale of change**: no lane-2, no `weaver-work`, no Epic 10 nudge, no 9.4 control surface,
    no bootstrap/provisioning change (`core-schedules` exists since 7.4), no edits to
    `internal/loom` / `internal/refractor` / `internal/processor` / frozen contracts / planning
    artifacts.

### Grounding map (read these before writing code)

- `docs/contracts/10-orchestration-surfaces.md` (FROZEN): **§10.4** (~lines 305–352, including the
  2026-06-05 correction block: target within `schedule.>`, republish-back-into-stream, filtered-
  consumer consumption, per-entity replace, deterministic fired-timer requestId, never-into-
  core-events) — the sole shape authority; **§10.3 `weaver-work` note** (~292–301: "the temporal
  lane replays from the core-schedules stream (§10.4, durable consumer)" — the fixed-durable
  posture is contract-anchored). Contracts are FROZEN — a genuine gap goes to
  `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md` (one §10.3 CAR is already pending there from 9.2;
  append, don't overwrite).
- `_bmad-output/implementation-artifacts/7-4-platform-message-scheduling-stream.md` — the stream
  config as shipped (`MaxMsgsPerSubject: 1`, rollup-on-fire), the smoke-test publish/consume
  mechanics, the lead's replace-assertion fix (port that rigor), and the CAR that became the §10.4
  correction.
- `internal/bootstrap/scheduling_smoke_test.go` — working header/publish/filtered-consumer code
  against the real scheduler (`natsserver.JSSchedulePattern`/`JSScheduleTarget`, `js.PublishMsg`
  not core publish, JetStream consumer not plain subscribe).
- `docs/components/scheduling.md` — the operator reference (carries the Task 6 drift to fix).
- `docs/components/weaver.md` — "Temporal lane" section (stale, Task 6 rewrite) + status table +
  In/Out table (the lane-3 In row is already correct: `schedule.weaver.timer.fired.>` on
  `core-schedules`).
- `_bmad-output/planning-artifacts/lattice-architecture.md` D3/D4 (~1130–1165) — LOCKED: 3 lanes,
  Actuator-publishes-@at, never-into-core-events, no custom scheduler subsystem.
- `internal/weaver/evaluator.go` — `handleRow` (the Task 2 seam: after `clearClosedMarks`, before
  the violating gate ends the non-violating path), `boolColumn`/`issueKeyData` (the data-error
  surface to mirror), `fire` (the publish-failure Nak posture).
- `internal/weaver/engine.go` — `Config`/`withDefaults` (add `CoreSchedulesStream`), `Start` (the
  static-consumer anchor beside the sweep/heartbeater), `targetSpec` (the ConsumerSpec shape to
  mirror for `weaver-temporal`), `supervisedHandler`.
- `internal/weaver/actuator.go` — `submit` (the op-envelope path `handleFiredTimer` reuses),
  `deriveID` (the deterministic-NanoID derivation `deriveTimerRequestID` namespaces).
- `internal/weaver/health.go` — `newHeartbeater`/`emit` (the sweep-counter precedent for
  `timersScheduled`/`timersFired`), `issueCache`.
- `internal/weaver/health_sink.go` — `newConsumerHealthSink` (reused verbatim for the temporal
  consumer's pause-state doc).
- `internal/substrate/publish.go` — `Conn.Publish(ctx, subject, data, header)` (headers already
  supported — Task 1 adds only the constants); `internal/substrate/consumer_supervisor_spec.go` —
  `DeliverAll` (zero value), `ConsumerSpec` fields.
- `internal/weaver/weaver_e2e_test.go` — harness (`startNATS`, `provision`, `putRow`,
  `installWeaverTarget`, `subscribeOps`/`nextOp`/`requireNoOp`) the new e2e scenarios extend.
- `_bmad-output/implementation-artifacts/9-1-weaver-target-lens-violation-lane.md` +
  `9-2-weaver-mark-ttl-reconciler.md` — predecessor rulings that still bind (NanoID-only subjects,
  no in-memory durable state, FR29 loud-failure, NakWithDelay cadences, fixture-not-Refractor,
  zero domain literals) and the post-review fix batches (issue-key scheme, `boolColumn`
  conservatism, metadata guards) this story must not regress.

### Out of scope — do NOT pull in

- **Lane-2 (event-targeted-audit) + `weaver-work` durable bucket** → Phase 3 (§10.3).
- **Two-Phase Nudge / `weaver-claims` / claimId** → Epic 10 (the nudge stub stays a stub).
- **Control API / pause surface** → 9.4. **Real target Lens + `MarkExpired` op DDL/package data**
  → Epic 11 (`lease-signing`); this story's Lens and Processor are the test fixture + fake
  processor loop, exactly as 9.1.
- **`@every` recurring schedules** (Phase 2 is `@at` one-shot, §10.4), **cancel/purge**, a
  **scheduler-ID header**, any custom scheduler subsystem, the op-vertex pruner (#47/#49).
- **Cross-target timer coordination** (decision 3) and a **temporal-conversion mark/lease**
  (decision 4).
- No bootstrap/provisioning changes; no edits to `internal/loom`, `internal/refractor`,
  `internal/processor`, `docs/contracts/*`, `_bmad-output/planning-artifacts/*`; no per-message
  KV scans; no `MaxDeliver`; no sprint tooling.

### House rules (binding, from CLAUDE.md)

- **NO history/changelog comments** — no `// Story 9.3`, `// since 7.4`, `// was lattice-schedules`,
  `// corrected 2026-06-05` in code. Comments describe what the code does NOW; godoc may cite
  contracts ("Contract #10 §10.4"). The doc-page edits MAY describe the current contract state but
  not narrate the change history beyond what those pages' own conventions already do.
- Key shapes per Contract #1; link names read as sentences (no new links this story).
- Sub-agents never commit/push/branch — leave the working tree for Winston.
- New docs → `/docs` (this story edits two existing `/docs/components` pages; no new page needed —
  scheduling.md already exists as the operator reference).

### Project Structure Notes

- New: `internal/weaver/temporal.go` (+ `temporal_internal_test.go`).
- Modified: `internal/substrate/publish.go` (two constants) + a substrate test (constant pin);
  `internal/weaver/engine.go` (Config + static temporal consumer); `internal/weaver/evaluator.go`
  (the `handleRow` scheduling leg call); `internal/weaver/actuator.go` (`scheduleTimer`,
  `deriveTimerRequestID`); `internal/weaver/strategist.go` (`opMarkExpired` constant);
  `internal/weaver/health.go` (two counters); `internal/weaver/weaver_e2e_test.go` (harness
  `provision` + new scenarios); `docs/components/weaver.md`; `docs/components/scheduling.md`.
- `internal/weaver` stays flat; durable names now: `weaver-target-<targetId>` (lane-1),
  `weaver-temporal` (lane-3), `weaver-target-source-<instance>` (registry).

### Previous story intelligence (9.1 done, 9.2 done)

- 9.1/9.2 reviews found Majors cluster in the handler/reconcile seams, not the supervisor — the
  scheduling leg in `handleRow` and `handleFiredTimer` are exactly such seams; the edge tests in
  Task 5 are the named net.
- The lane-1 redelivery semantics (`NumDelivered != 1` → blanket same-episode re-fire) and the
  9.1 post-review metadata guards (`Sequence == 0` defer) must survive untouched — the scheduling
  leg slots in WITHOUT altering any existing Decision path except adding its own NakWithDelay on
  schedule-publish failure.
- `boolColumn`'s conservative non-bool posture and the keyed issue scheme
  (`gap:`/`data:`/`consumer:`/`sweep:` prefixes) are post-review fixes — extend (`timer:` prefix),
  don't fork.
- 9.2's `markStore`/sweeper are untouched by this story; the sweep neither schedules nor clears
  timers (timers are not marks).
- The 7.4 lead review's replace-assertion lesson: a replace test must be able to FAIL when replace
  is broken (both schedules inside the observation window) — Task 5's replace test follows it.
- Story-prep probe (2026-06-12, this story's creation session): embedded `natstest.RunServer`
  (JetStream on, v2.14.0) fired an `@at now+2s` schedule to a `schedule.weaver.timer.fired.<id>`
  target in ~1.5s and a filtered durable fetched it — the basis of the Task 5 embedded-harness
  ruling.

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 9.3 (amended 2026-06-12, corrected §10.4 shape)]
- [Source: docs/contracts/10-orchestration-surfaces.md §10.4 (corrected 2026-06-05), §10.3 (weaver-work note)]
- [Source: docs/components/weaver.md (Temporal lane, status table, In/Out)]
- [Source: docs/components/scheduling.md]
- [Source: _bmad-output/planning-artifacts/lattice-architecture.md#D3, #D4]
- [Source: _bmad-output/implementation-artifacts/7-4-platform-message-scheduling-stream.md]
- [Source: internal/bootstrap/scheduling_smoke_test.go; internal/bootstrap/primordial.go (core-schedules config)]
- [Source: internal/weaver/{evaluator,engine,actuator,strategist,health,health_sink,state}.go]
- [Source: internal/substrate/{publish,consumer_supervisor_spec}.go]
- [Source: _bmad-output/implementation-artifacts/9-1-weaver-target-lens-violation-lane.md; 9-2-weaver-mark-ttl-reconciler.md]

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (dev + fix-forward sub-agents under Winston).

### Debug Log References

- Gate 2 hang during the first dev pass was an environment hiccup (NATS container restart), not code; the lead re-ran all gates green.

### Completion Notes List

- Implementation built to the amended per-target schedule subject (`schedule.weaver.timer.<targetId>.<entityId>`), the two §10.4 header constants in substrate, the fixed `weaver-temporal` durable, the deterministic timer requestId, and the no-mark temporal posture.
- Post-review fix-forward batch (3 adversarial layers, BH/ECH/AA findings): op-publish failure → NakWithDelay; past-deadline skip removed (overdue `@at` fires immediately = correct level expiry; payload `fireAt` stays the deadline instant → same deterministic requestId); read-before-act guard in the fired handler (payload-vs-subject targetId cross-check, unregistered-target Ack, deleted-entity Ack, stale-firing suppression when the row was renewed with a later `freshUntil`); issue-lifecycle hygiene (per-target keyed TimerDataError/ScheduleConfigError/SchedulePublishError carrying the latest offender; RowDataError cleared when the column goes absent; json.Marshal failure → RowDataError + Ack, never eternal NakWithDelay; schedule-publish failure raises a Health issue cleared on next success).

### File List

- internal/weaver/temporal.go (new) — temporal lane: supervised consumer on schedule.weaver.timer.fired.>, fired-timer → MarkExpired op, scheduling leg, read-before-act guard
- internal/weaver/temporal_internal_test.go (new)
- internal/weaver/export_test.go (new)
- internal/substrate/publish.go — two pinned schedule-header constants (ScheduleHeader, ScheduleTargetHeader); no helper added
- internal/substrate/publish_schedule_test.go (new) — constant pin against the nats-server exported values
- internal/weaver/actuator.go, engine.go, evaluator.go, health.go, strategist.go — freshUntil scheduling leg, consumer wiring, heartbeat counters
- internal/weaver/weaver_e2e_test.go — temporal e2e (fire, replace-on-reschedule, restart durability, missed-while-down)
- internal/bootstrap/scheduling_smoke_test.go — obsolete docker-required comment updated
- docs/components/weaver.md, docs/components/scheduling.md — corrected §10.4 shapes
- cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md — two annotation-class requests appended (Request 2: §10.4 per-target subject token; Request 3: §10.2 freshUntil convention column)

### Change Log

- Dev sub-agent completed implementation; was killed mid-gates (environment hang in Gate 2 caused by a NATS container restart, not the code). Lead re-ran all gates green: build/vet/lint clean, verify-kernel PASSED, Gate 2 PASSED, Gate 3 PASSED 4/4, weaver+substrate+bootstrap+loom tests ok.
- Fix-forward batch applied after the 3-layer adversarial review (BH/ECH/AA): NakWithDelay on op-publish failure, past-deadline guard removed, read-before-act stale-firing guard, issue-lifecycle hygiene, CAR appended, story/docs/tests reconciled to the per-target subject. Gates re-run.

## Questions for Winston (non-blocking — drafted around contract-compliant defaults)

1. **WHO schedules (decision 1):** the epic's "Actuator schedules @at(resolve+window) on resolve"
   is realized as the row-driven level shape — the target cypher projects the deadline
   (`freshUntil`) and the engine re-publishes the per-entity schedule on every row delivery
   (idempotent under replace). This keeps the window in package data and the engine time-only,
   per weaver.md/D4. Alternative rejected: scheduling from the remediation/nudge resolve path —
   that path is Epic 10/11 machinery and would put window knowledge in the engine. Confirm.
2. **`freshUntil` convention column (decision 2):** engine-recognized optional §10.2 param column.
   Flag whether you want an annotation-class CAR pinning it into §10.2 (the 7.4 precedent), or
   weaver.md documentation only.
3. **Per-entity timer slot (decision 3):** `schedule.weaver.timer.<entityId>` is pinned by the AC;
   the multi-target-same-entity last-write-wins bound is accepted + documented. Confirm no
   cross-target coordination in Phase 2.
4. **No mark/lease on the temporal conversion (decision 4):** a Processor-rejected `MarkExpired`
   is not re-attempted (next-CDC self-heal, operator-visible at the Processor). Confirm this
   documented bound (the alternative — leasing fired conversions in weaver-state — rebuilds lane-1
   machinery for a non-dispatch op).
5. **`MarkExpired` envelope (decision 5):** constant op type, no authContext, payload
   `{entityKey, targetId, expiredAt}`. The real op DDL arrives with Epic 11 — flag if you want a
   `Config` knob for the op type instead of the constant.
6. **Embedded-scheduler test ruling (Task 5):** verified by probe that the embedded server runs
   the scheduler, so the full firing/replace/restart e2e lives in the standard embedded harness
   (no Docker-only skip-guarded test). 7.4's contrary caution is superseded — confirm, and note
   the smoke test (`TestCoreSchedulesSmoke`) still covers the real-stack path.
