# Story 9.1: Target-as-Lens + violation-driven lane + OCC actuator

Status: done

## Story

As a platform developer,
I want Weaver to watch a target Lens's violation output and remediate gaps,
So that a declared target state converges.

## Acceptance Criteria

(Authoritative source: `_bmad-output/planning-artifacts/epics/phase-2-epics.md` → Epic 9 → Story 9.1,
as amended 2026-06-12. The epic text governs over any paraphrase.)

1. **Given** a fixture target Lens projecting **one row per candidate with a `violating` flag** (+ gap
   columns) to the shared `weaver-targets` bucket, key `<targetId>.<entityId>` (NATS-KV; entity =
   vertex, key on the NanoID, full `entityKey` in the value), the target discovered via a
   **`meta.weaverTarget` registry CDC source** (§10.8; mirrors Loom's `internal/loom/source.go`)
   carrying §10.8's install-time validations (`targetId` uniqueness; every `gaps` key matches
   `missing_*`).
2. **And given** **`weaver-targets` joins the primordial bucket create list** (§10.2 "NEW", like
   `loom-state`) with a `verify-kernel` assertion.
3. **When** a row with `violating: true` appears, **then** the Sensorium's lane-1 is a **per-target
   supervised KV-CDC durable** on the `weaver-targets` backing stream (`FilterSubject
   $KV.weaver-targets.<targetId>.>`, `DeliverLastPerSubject` — the Refractor CDC pattern, **not** a
   raw KV watcher), driven through `substrate.ConsumerSupervisor` (8.4) as a desired-vs-running
   reconcile over the registry: a removed/revoked target's consumer is `Remove`d **and its JetStream
   durable deleted**; a changed filter/config is `Reset`, never silently unchanged (the 8.5 lessons,
   adopted day one).
4. **And** Evaluator L1 confirms still-violating + **not in-flight** — the in-flight check reads the
   `weaver-state` **CAS-create mark** (§10.8's dispatch OCC, created in this story; mark-clearing is
   **level-reconciled on each watch update**: any mark whose `missing_<col>` is now `false` is
   deleted; the TTL/lease + reconciler sweep land in 9.2); L2 classifies the gap; Strategist selects a
   playbook; the Actuator submits a remediation op via the Processor with an **OCC
   revision-condition**.
5. **And** the Actuator's submit is a **fire-and-forget publish** to `core-operations` via substrate
   with a deterministic per-dispatch-episode `requestId` (Contract #4 collapses a re-fire within the
   horizon) — **no request-reply** (the Loom F1 lesson); rejected/lost-op recovery is the §10.3
   mark-lease + level-reconcile, **not** a command outbox (Weaver has no cursor advance to dual-write
   — pinned so review does not demand one).
6. **And** triggering a Loom utility is done **via an op** (not a Go call).
7. **And** when the gap closes the row's flag flips `false` via upsert (no retraction needed); Weaver
   stops acting and the mark is cleared.
8. **And** each supervised consumer carries a **`health.weaver.<target>` HealthSink** and Weaver
   publishes a Contract #5 heartbeat, mirroring `internal/loom/health_sink.go` / `health.go`.
9. **And** `weaver` imports only `substrate/*`, enforced by a `boundary_test.go` forbidding
   `nats-io/*` from day one.

## Tasks / Subtasks

- [x] Task 1: Provision the `weaver-targets` primordial bucket + verify-kernel assertion (AC 2)
  - [x] Add `WeaverTargetsBucket = "weaver-targets"` to the bucket-name constants in
        `internal/bootstrap/primordial.go` (~line 22) and append it to the `ProvisionBuckets` list
        (~line 63). `ttl: false` — target rows are durable Lens projections, no per-key TTL keys live
        here (marks with TTL live in `weaver-state`); History stays the KV default 1, which is exactly
        what `DeliverLastPerSubject` CDC wants. No `AllowAtomicPublish` (Weaver does no atomic batch
        on this bucket).
  - [x] Add `WeaverTargetsBucket` to the §7 KV-bucket assertion list in
        `internal/bootstrap/verify.go` (~line 164) — that list is the callable verify-kernel surface
        (`scripts/verify-kernel.go` exercises the same assertions). Note: `LoomStateBucket` is absent
        from that list today (it is asserted via `internal/bootstrap/loom_state_bucket_test.go`
        instead); do NOT "fix" that here — just add `weaver-targets`.
  - [x] Bootstrap test mirroring `internal/bootstrap/loom_state_bucket_test.go`
        (`TestLoomStateBucket_Provisioned`) asserting the bucket exists after `ProvisionBuckets`.
- [x] Task 2: `internal/weaver` package skeleton + module boundary (AC 9)
  - [x] New package `internal/weaver` (flat, one-concern-per-file like `internal/loom`: `engine.go`,
        `registry.go` (the meta.weaverTarget source), `evaluator.go`, `strategist.go`, `actuator.go`,
        `state.go` (weaver-state marks), `health.go`, `health_sink.go`, `doc.go`).
  - [x] `boundary_test.go` copied from `internal/loom/boundary_test.go`, adjusted: forbid
        `internal/processor`, `internal/loom`, `internal/refractor` transitively (`go list -deps`) and
        forbid direct `github.com/nats-io/*` imports (`go list -f '{{ join .Imports "\n" }}'`).
        Write this test FIRST — it is the day-one guard the AC demands.
- [x] Task 3: `meta.weaverTarget` registry CDC source (AC 1)
  - [x] `registry.go` mirrors `internal/loom/source.go` (`patternSource`): durable
        `Conn.SubscribeKVChanges` on `core-kv` prefix `vtx.meta.`, per-boot durable name
        (`weaver-target-source-<instance>`, `IncludeHistory: true`), class probe routes only
        `class == "meta.weaverTarget"` vertices; the target body rides the vertex's `spec` aspect
        (`vtx.meta.<id>.spec`), with the same out-of-order buffering (spec-before-class) and the same
        envelope unwrap (`data` sub-object) as `unwrapPatternBody`.
  - [x] Parse into a `Target` struct: `targetId`, `lensRef`, `gaps: map[string]GapAction`
        (`action`, plus per-action params: `pattern`/`subject`/`adapter`/`operation`/`assignee`/
        `target`/`params` — Contract #10 §10.8 action table).
  - [x] **Install-time validations (§10.8-mandated, AC 1):** on load/update, reject-and-alert
        (log Error + surface a Health KV issue; never panic, never silently skip):
        (a) every `gaps` key matches `^missing_` ;
        (b) `targetId` is unique across currently-registered targets (two registered targets with the
        same `targetId` = config error — keep the first, reject the later, alert);
        (c) `targetId` must be a valid single KV-key segment (no dots — it is a `weaver-targets` key
        prefix and a durable-name segment).
  - [x] Load/update/remove callbacks drive the lane-1 reconcile (Task 4), exactly as
        `patternSource.setLoadCallback`/`setUpdateCallback` drive `reconcileConsumers`.
- [x] Task 4: Lane-1 per-target supervised KV-CDC consumers (AC 3)
  - [x] Engine holds one `substrate.ConsumerSupervisor` (`NewConsumerSupervisor(conn)`); per-target
        spec: `Name: "weaver-target-<targetId>"`, `Stream: "KV_weaver-targets"` (i.e. `"KV_" +
        cfg.WeaverTargetsBucket`), `FilterSubject: "$KV.<bucket>.<targetId>.>"`,
        `DeliverPolicy: substrate.DeliverLastPerSubject`, `Handler` = the lane-1 handler,
        `Health` = per-target sink (Task 7). This is a durable consumer on the backing stream — NOT
        `kv.Watch`.
  - [x] Desired-vs-running reconcile copied from `internal/loom/engine.go` `reconcileConsumers`:
        a `specFingerprint`-style diff (stream/filter/policy/group), serialize the whole pass under a
        mutex, `Add` on new target, `Remove` (durable deleted server-side) on target removal/tombstone,
        `UpdateSpec`+`Reset` on fingerprint change — never silently unchanged. Seed one reconcile at
        engine start (the 8.5 zero-patterns-loaded lesson).
  - [x] On `Remove`, also delete the per-target health-sink entry (mirror
        `consumerHealthSink.delete` usage in loom's reconcile Remove branch).
- [x] Task 5: Evaluator L1/L2 + Strategist + weaver-state CAS-create mark (AC 4, 7)
  - [x] Lane-1 handler: parse the KV-CDC message (value = the §10.2 row JSON; deleted/empty body =
        entity deletion → level-reconcile marks for that `<targetId>.<entityId>` then Ack). Extract
        `<targetId>.<entityId>` from the subject (strip `$KV.<bucket>.` prefix, the loom relay/deadline
        subject-recovery pattern).
  - [x] **Level-reconciled mark-clearing runs on EVERY row update first** (violating or not): list
        existing `weaver-state` marks under `<targetId>.<entityId>.` and delete any mark whose
        `missing_<col>` in the CURRENT row is `false` or absent (§10.3: never edge-triggered; a
        coalescing watch can drop the transitional flip). This single code path satisfies AC 7.
  - [x] L1: if `violating != true` → done (clearing already ran). If `violating == true`, for **every**
        `missing_*` column that is `true`: skip if a `weaver-state` mark exists (in-flight).
  - [x] L2 + Strategist: look up `gaps[col]` on the registered target. A `missing_*: true` column with
        **no** `gaps[col]` entry is a **config error → alert** (log Error + Health KV issue; never
        silently skipped — FR29 discipline). Resolve templated params: a param value is a literal or
        the token `row.<column>` substituted from the row; a `row.<column>` resolving null/absent is a
        **data error → surface, do not fire** (§10.8 Templating).
  - [x] Dispatch OCC: `Conn.KVCreate` (CAS-on-absent) of the mark at
        `weaver-state` key `<targetId>.<entityId>.<gapColumn>`, value
        `{ targetId, entityKey, gap, action, claimedAt }` (§10.3 shape minus the 9.2 fields:
        `leaseExpiresAt`/TTL, `claimId`, `heldBy` land in 9.2 — but keep the struct fields present and
        omit-empty so 9.2 extends, not migrates). Create wins → dispatch; create loses
        (already-exists) → drop, the winner dispatched. Use plain `KVCreate`, NOT `KVPutWithTTL`
        (TTL is 9.2).
  - [x] Gaps fire in parallel-safe sequence (independent marks); gap *dependencies* are the Lens's
        problem, not Weaver's (§10.8 — do not build ordering).
- [x] Task 6: Actuator — fire-and-forget OCC op submit (AC 4, 5, 6)
  - [x] Carry Weaver's own copy of the Contract #2 `opEnvelope` (mirror
        `internal/loom/actuator.go` lines 19–32 — module boundary forbids importing
        `internal/processor`). `Actor` = the primordial weaver service-actor key
        (`vtx.identity.<WeaverIdentityID>` — resolved in `cmd/weaver` from
        `bootstrap.WeaverIdentityKey`, passed in via `Config.ActorKey` like loom's).
  - [x] Submit = ONE `conn.Publish(ctx, "ops."+cfg.Lane, envelope, nil)` — fire-and-forget. **NO
        request-reply** (the Loom F1 lesson: a synchronous reply wait was rejected in 8.x), and **NO
        command outbox** — Weaver, unlike Loom, has no cursor advance to keep atomic with the submit;
        its crash-recovery story is the §10.3 mark + level-reconcile (+ 9.2's lease). A failed publish
        → Nak (the CDC message redelivers; the mark already exists, so the retry path re-reads the
        mark and re-publishes the SAME requestId). This is pinned: reviewers must not demand an
        outbox here.
  - [x] **Deterministic per-dispatch-episode `requestId`:** derive a 20-char NanoID from
        `(targetId, entityId, gapColumn, markRevision)` using the `internal/loom/token.go`
        `deriveID` technique (sha256 → canonical alphabet; re-implement in weaver, no loom import).
        The mark's KV create revision (returned by `KVCreate`, re-readable via `KVGet`) is the episode
        tag: a re-fire of the SAME episode reuses the same requestId and collapses on the Contract #4
        tracker; a legitimately re-opened gap (mark deleted, new CAS-create) gets a new revision →
        new requestId → a real new dispatch.
  - [x] **OCC revision-condition (AC 4):** every remediation op's payload carries the candidate
        entity's expected revision so two ticks can't double-apply. The substrate per-key revision of
        the entity row arrives free on the CDC message (`KVEvent`/`Message` revision metadata) —
        thread it through L2 into the op payload as `expectedRevision` alongside the target key.
        Note: the Processor's generic op path validates what the op's Starlark/processor step checks;
        for the walking-skeleton ops used here, carrying the condition in the payload + the §10.3 mark
        is the enforced OCC. Do not invent a new processor feature; if a genuine Contract #2 gap
        surfaces, STOP and write it up (CONTRACT-AMENDMENT-REQUEST), don't edit frozen contracts.
  - [x] Action dispatch per §10.8 table:
        `triggerLoom` → op `StartLoomPattern`, payload `{ patternRef, subjectKey }`,
        **`authContext.target = "vtx.meta." + <patternId>`** (pattern-as-target; Weaver holds
        `StartLoomPattern @ scope: any` via the operator role — already seeded, see
        `packages/orchestration-base/manifest.yaml` ~line 38 and the primordial
        `weaver holdsRole operator` link). Resolving `pattern` (a patternId literal like
        `"onboarding"`) to the `vtx.meta.<NanoID>` pattern vertex: index `meta.loomPattern`-class
        vertices in the registry source (same CDC stream, same class-probe pattern —
        mirror `patternSource.indexOpMeta`).
        `assignTask` → op `CreateTask`, payload `{ assignee, forOperation, scopedTo, expiresAt,
        taskId }` (§10.1 / loom's `submitUserTask` payload shape).
        `directOp` → submit `operation` with templated params.
        `nudge` → **OUT OF SCOPE** (Epic 10): the Strategist recognizes the action and routes it to a
        stub that logs + surfaces "nudge not yet implemented" as a Health issue (never silently
        dropped); do NOT build `internal/weaver/nudge/`.
  - [x] Triggering Loom is via the op ONLY — no Go call into `internal/loom` (boundary test enforces).
- [x] Task 7: Health — per-consumer sinks + Contract #5 heartbeat (AC 8)
  - [x] `health_sink.go`: copy `internal/loom/health_sink.go` structurally (cannot import it):
        per-consumer pause-state doc at **`health.weaver.<instance>.consumer.<name>`** where `<name>`
        is the durable (`weaver-target-<targetId>`) — this is the per-target
        `health.weaver.<target>` surface the AC names; `Load` restores pause state across restart;
        sink feeds an in-memory `consumerStateCache`.
  - [x] `health.go`: copy loom's heartbeater shape — Contract #5 §5.2 doc at
        `health.weaver.<instance>`, `component: "weaver"`, 10s cadence (§5.6), metrics:
        `consumers` (state map), `targets` (registered-target count), `marksInFlight`
        (heartbeat-cadence scan of `weaver-state`, mirroring `runningInstanceCounter` — never
        per-message); `issues` carry pausedStructural consumers AND the Task 3/5 config-error alerts
        (unknown gap column, duplicate targetId, template data error).
- [x] Task 8: `cmd/weaver` binary
  - [x] Mirror `cmd/loom/main.go`: NATS_URL / BOOTSTRAP_JSON_PATH / WEAVER_INSTANCE / WEAVER_LANE
        envs, resolve `bootstrap.WeaverIdentityKey` for `Config.ActorKey`, slog to stderr, SIGINT/
        SIGTERM graceful shutdown. (`cmd/weaver` may import `internal/bootstrap` for key resolution,
        exactly as `cmd/loom` does; `internal/weaver` itself may not.)
- [x] Task 9: E2E + unit tests (all ACs)
  - [x] Reuse the embedded-NATS harness pattern from `internal/loom/loom_e2e_test.go`
        (`startNATS`, `provision`, fake processor loop) — copy the helpers into
        `internal/weaver/weaver_e2e_test.go`, do not import loom.
  - [x] **Fixture, not Refractor:** the "fixture target Lens" is the TEST writing §10.2-shaped rows
        directly into `weaver-targets` (Refractor wiring of a real target Lens is the lease-signing
        package's job, Epic 11). The fixture must use the exact §10.2 value shape (`entityKey`,
        `violating`, `missing_*`, param columns, `projectedAt`).
  - [x] E2E happy path: install a `meta.weaverTarget` (via the test's meta-vertex write path) → lane-1
        consumer appears (assert durable exists) → fixture writes `violating: true` row → mark
        CAS-created → op observed on `ops.<lane>` with correct envelope/authContext/requestId →
        fixture flips row to `violating: false` → mark deleted (level-reconcile), no further ops.
  - [x] Anti-storm: re-upsert the SAME violating row (CDC re-delivery) → no second op (mark exists).
  - [x] OCC/idempotency: crash-sim re-fire of the same episode → same requestId (unit-test the
        derivation); re-open after close → new requestId.
  - [x] Reconcile: tombstone the meta.weaverTarget → consumer `Remove`d AND JetStream durable deleted
        (assert via consumer-info absence); re-install → fresh consumer replays rows
        (`DeliverLastPerSubject`).
  - [x] Validation: `gaps` key without `missing_` prefix → target rejected + alert; duplicate
        targetId → second rejected; `missing_x: true` with no playbook entry → alert, no dispatch.
  - [x] Boundary tests (Task 2) green.
- [x] Task 10: Documentation + verification gates
  - [x] Update `docs/components/weaver.md`: status line (design → Phase 2 in progress), the shipped
        lane-1/§10.3-subset reality (CAS-create mark without TTL until 9.2, no nudge until Epic 10,
        no temporal until 9.3, no lane-2). Same-commit-as-code rule is stated in that file's header.
  - [x] Gates, all green: `go build ./...`, `make vet`, `golangci-lint run ./...`,
        `make verify-kernel`, `make test-bypass` (all BLOCKED), `make test-capability-adversarial`
        (all DEFENDED), `go test ./internal/weaver/... ./internal/bootstrap/...` (and
        `./internal/loom/... ./internal/substrate/...` untouched-green as the regression net).

## Dev Notes

### Adjudicated decisions (binding — encode, do not re-litigate)

1. **No command outbox, no request-reply.** The Actuator's submit is a bare fire-and-forget
   `Publish` to `ops.<lane>`. Loom needed an outbox because its op submit had to be atomic with a
   cursor advance; Weaver has no cursor — the §10.3 mark + level-reconcile (+ 9.2 lease/TTL) is its
   recovery story. The epic pins this explicitly "so review does not demand one." A rejected/lost op
   in 9.1 simply leaves the mark in place; 9.2's lease expiry re-attempts it. Document this 9.1
   interim posture in `docs/components/weaver.md`.
2. **9.1 ships the §10.3 mark WITHOUT TTL/lease/reconciler.** `KVCreate` plain; value carries
   `claimedAt` (episode tag) but `leaseExpiresAt`/`claimId`/`heldBy` are empty/absent until 9.2.
   Mark-clearing in 9.1 is level-reconciled **on watch updates only** (the sweep is 9.2's).
   Consequence (accepted, 9.2 fixes it): an Actuator crash after CAS-create and before publish wedges
   that one gap until 9.2's lease lands — do NOT hack a workaround.
3. **Lane-1 is a supervised durable on `KV_weaver-targets`** (`$KV.<bucket>.<targetId>.>`,
   `DeliverLastPerSubject`) via `substrate.ConsumerSupervisor` — never `kv.Watch`, never a raw
   JetStream handle. The Refractor CDC pattern; loom's relay/deadline specs show the exact spec shape
   for a KV-CDC durable.
4. **Teardown deletes the durable** (`supervisor.Remove`) — an un-pumped server-side durable IS the
   leak the 8.5 ruling forbids. Re-add replays via `DeliverLastPerSubject`; replay is safe because
   the mark CAS + level-reconcile make the handler idempotent.
5. **`weaver-targets` is provisioned `ttl: false`** (no per-key TTL keys live there; rows are durable
   projections), History default 1, no `AllowAtomicPublish`. `weaver-state`/`weaver-claims` already
   exist as primordial TTL-capable buckets — touch nothing about them.
6. **`nudge` is a stub in 9.1** (Epic 10 builds the Two-Phase Nudge + `internal/weaver/nudge/`).
   The stub must be loud (log + Health issue), never silent (FR29 discipline). Do not create
   `weaver-claims` logic in this story.
7. **Keys and subjects carry NanoIDs, never dotted vertex keys** (§10.2; the 8.1 lesson). The
   `weaver-targets` key is `<targetId>.<entityId>` (entity NanoID); the full `vtx.<type>.<id>` lives
   in the row's `entityKey` (document-is-truth). Same for `weaver-state` keys. Validate `targetId`
   is dot-free at install time.
8. **No in-memory indexes for durable state** (the 8.1 in-memory-correlation-index mistake). The
   in-flight check is a KV read of `weaver-state`, not a map. In-memory caches are fine ONLY for
   derived/registry state rebuilt by CDC replay (the registry source, consumer-state cache) — the
   same line loom draws.
9. **Pattern-definition pinning is Loom's concern, not Weaver's** — Weaver resolves the
   `meta.loomPattern` vertex for `authContext.target` from the LIVE registry at dispatch time (like
   loom's `opMetaKey` live resolution).
10. **Engine ships zero domain knowledge.** Target + playbook are package data; the engine is a
    generic dispatcher. Nothing in `internal/weaver` may mention `leaseApplicationComplete`,
    `missing_bgcheck`, etc. — those literals appear only in tests/fixtures.

### Grounding map (read these before writing code)

- `docs/contracts/10-orchestration-surfaces.md` (FROZEN): **§10.2** (lines ~74–140: bucket, key shape,
  row columns, watch semantics, retraction), **§10.3 weaver-state** (lines ~236–264: mark key/value,
  CAS-create, level-reconciled clearing — 9.1 implements the CAS-create + watch-update clearing
  subset), **§10.8** (lines ~639–738: meta.weaverTarget shape, install validations, action contracts,
  templating, `StartLoomPattern` auth, flow & anti-storm). Contracts are FROZEN — a genuine gap goes
  to `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md` (new file, mirror `cmd/loom`'s), never an edit.
- `docs/components/weaver.md` — pipeline (Sensorium → Evaluator L1/L2 → Strategist → Actuator),
  3 lanes (only lane 1 in this story), module-boundary principle. Update it in the same commit.
- `_bmad-output/planning-artifacts/lattice-architecture.md` — D3 (lines ~1130–1145, Weaver runtime
  mechanics) and D4 (lines ~1147–1165, target-as-Lens) — LOCKED decisions; read-only.
- `internal/loom/engine.go` — `ConsumerSpec` construction (`triggerSpec`/`relaySpec`/`deadlineSpec`),
  `specFingerprint` + `reconcileConsumers` (the desired-vs-running diff under one mutex; Add/Reset/
  Remove semantics; health-sink cleanup on Remove), `Config`/`withDefaults` shape, `defaultInstance`.
- `internal/loom/source.go` — the meta-vertex CDC source to mirror: per-boot durable +
  `IncludeHistory`, class probe, spec-aspect buffering, `unwrapPatternBody`, `indexOpMeta` (the
  live operationType→meta-key index — same technique for patternId→meta-key).
- `internal/loom/health_sink.go` / `health.go` — HealthSink impl + Contract #5 heartbeater to mirror
  (key prefix becomes `health.weaver.`, component `"weaver"`).
- `internal/loom/actuator.go` — `opEnvelope` (the Contract #2 wire shape weaver re-declares),
  `authContext{Target}`, the `"ops."+lane` publish, subject→key recovery via `$KV.<bucket>.` prefix.
- `internal/loom/token.go` — `deriveID` deterministic-NanoID technique for the episode requestId.
- `internal/loom/boundary_test.go` — the two boundary tests to copy.
- `internal/substrate/consumer_supervisor.go` / `consumer_supervisor_spec.go` — supervisor API
  (`Add`/`Remove`/`Reset`/`UpdateSpec`/`Stop`/`Pause`/`Resume`), `ConsumerSpec` fields,
  `DeliverLastPerSubject`, `SupervisedHandler` (Decision, error) semantics, `HealthSink` contract.
- `internal/substrate/kv.go` — `KVCreate` (CAS-on-absent; this IS the dispatch OCC), `KVGet`,
  `KVDelete`, `KVListKeys`; `internal/substrate/subscribe.go` — `SubscribeKVChanges` (registry
  source), `KVEvent{Key, Value, IsDeleted, Revision…}`.
- `internal/bootstrap/primordial.go` — bucket constants + `ProvisionBuckets` (~lines 22–110),
  `WeaverIdentityKey`/`WeaverIdentityID` (the service actor, already provisioned with
  `weaver holdsRole operator`); `internal/bootstrap/verify.go` §7 bucket list (~line 164).
- `packages/orchestration-base/manifest.yaml` — `StartLoomPattern scope: any grantsTo: [operator]`
  (~line 38) and `CreateTask scope: any` already seeded: Weaver's authority needs NO new grants.
- `cmd/loom/main.go` — the binary shape `cmd/weaver` mirrors.
- `internal/loom/loom_e2e_test.go` — embedded-NATS harness + fake-processor loop the e2e tests copy
  (note its `StartLoomPattern` handling ~line 186 if a test exercises a real triggerLoom round trip).
- `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md` — the 8.1 lessons codified (in-memory index rejection,
  domain-binding declaration, fire-and-forget posture); background for decisions 1/7/8 above.

### Out of scope — do NOT pull in

- **TTL/lease + reconciler sweep + `claimId` + mid-flight-kill test** → Story 9.2.
- **Temporal lane / core-schedules** → Story 9.3. **Control API/CLI (Pause/Resume surface)** → 9.4
  (the supervisor's `Pause`/`Resume` exist; no operator surface here).
- **Two-Phase Nudge / `internal/weaver/nudge/` / `weaver-claims` writes** → Epic 10 (stub only).
- **Lane-2 (event-targeted-audit) and `weaver-work`** → Phase-3-deferred (§10.3 `weaver-work`).
- **Real target Lens via Refractor + playbook package data** → Epic 11 (`lease-signing`); this story
  uses a test fixture writing §10.2 rows.
- No edits to `internal/substrate`, `internal/loom`, `internal/refractor`, `docs/contracts/*`,
  `_bmad-output/planning-artifacts/*`. A genuine substrate gap = STOP and write it up in Questions,
  not a patch.
- No exponential backoff, no `MaxDeliver`, no per-message KV scans, no sprint tooling.

### House rules (binding, from CLAUDE.md)

- **NO history/changelog comments** — no `// Story 9.1`, `// mirrors loom`, `// copied from`,
  `// like Loom's X`. Comments describe what THIS code does now. (This story copies many loom shapes
  — resist narrating the provenance in comments; godoc may cite contracts, e.g. "Contract #10 §10.3".)
- Key shapes per Contract #1: aspects `vtx.<type>.<id>.<localName>`, links 6-segment, meta-vertices
  `vtx.meta.<NanoID>` + `.canonicalName`.
- Sub-agents never commit/push/branch — leave the working tree for Winston.
- New docs → `/docs` (the weaver.md update), never `_bmad-output/`.

### Project Structure Notes

- New: `internal/weaver/{doc,engine,registry,evaluator,strategist,actuator,state,health,health_sink}.go`
  + `{boundary,weaver_e2e,…}_test.go`; `cmd/weaver/main.go`.
- Modified: `internal/bootstrap/primordial.go` (constant + bucket list),
  `internal/bootstrap/verify.go` (§7 list), new `internal/bootstrap/weaver_targets_bucket_test.go`
  (or extend the existing bucket-test file pattern), `docs/components/weaver.md`.
- `internal/weaver` stays flat (no sub-packages) per the loom precedent; `nudge/` arrives in Epic 10.
- Naming: durables `weaver-target-<targetId>` (lane-1), `weaver-target-source-<instance>` (registry);
  health keys `health.weaver.<instance>` + `health.weaver.<instance>.consumer.<name>`.

### Previous story intelligence (8.5, done; Epic 8 complete)

- 8.5's review found the adapter seam (handler wrapping, HealthSink, Classify), not the supervisor
  core, is where Majors cluster — same risk profile here: the supervisor is 3-layer-reviewed and
  solid; Weaver's handler/reconcile/health code is where care is needed.
- The 8.5 zero-patterns-on-restart lesson: seed one reconcile at engine start; do not rely solely on
  source callbacks to bring consumers up.
- 8.3/commit `1aa120a` lesson (pattern pinning): definitions read at a stable point, never mid-flight
  re-resolution against mutated state — for Weaver this maps to decision 9 (live resolution is the
  deliberate choice for authContext targets; the dispatch episode itself is pinned by the mark).
- Classify hook: loom shipped handlers that fully encode outcomes as `Decision` with nil error, so a
  nil `Classify` (= always transient) was accepted. Same posture acceptable here — note it in
  Completion Notes if taken.
- `Resume` semantics (8.4 godoc): clears only reasons active at call time — relevant only to tests.

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 9.1 (amended 2026-06-12)]
- [Source: docs/contracts/10-orchestration-surfaces.md §10.2, §10.3 (weaver-state), §10.8]
- [Source: docs/components/weaver.md]
- [Source: _bmad-output/planning-artifacts/lattice-architecture.md#D3, #D4]
- [Source: internal/loom/{engine,source,actuator,health,health_sink,token,boundary_test}.go]
- [Source: internal/substrate/{consumer_supervisor,consumer_supervisor_spec,kv,subscribe}.go]
- [Source: internal/bootstrap/{primordial,verify}.go; internal/bootstrap/loom_state_bucket_test.go]
- [Source: packages/orchestration-base/manifest.yaml (StartLoomPattern/CreateTask grants)]
- [Source: cmd/loom/CONTRACT-AMENDMENT-REQUEST.md (8.1 lessons)]
- [Source: _bmad-output/implementation-artifacts/8-5-loom-adopts-consumer-supervisor.md (rulings carried forward)]

## Dev Agent Record

### Agent Model Used

claude-fable-5 (dev-story sub-agent, 2026-06-12)

### Debug Log References

- All gates run locally 2026-06-12: `go build ./...` ✅, `make vet` ✅, `golangci-lint run ./...`
  (0 issues) ✅, `make verify-kernel` (ALL ASSERTIONS PASSED, incl. `OK bucket: weaver-targets`) ✅,
  `make test-bypass` (Gate 2: 4/4 BLOCKED) ✅, `make test-capability-adversarial` (Gate 3: 4/4
  cleared — 3 DEFENDED, 1 ACCEPTED-WINDOW, the standing v2 baseline) ✅,
  `go test ./internal/weaver/... ./internal/bootstrap/...` ✅, regression net
  `go test ./internal/loom/... ./internal/substrate/...` ✅ (untouched-green).

### Completion Notes List

- **Adjudicated decisions encoded as pinned**: no outbox/no request-reply (bare `Publish` to
  `ops.<lane>`); §10.3 mark WITHOUT TTL (plain `KVCreate`; `claimId`/`leaseExpiresAt`/`heldBy`
  declared omit-empty for 9.2 extension); lane-1 = supervised durable on `KV_weaver-targets`
  (`DeliverLastPerSubject`), teardown deletes the durable; `weaver-targets` provisioned
  `ttl: false`, history 1, no AllowAtomicPublish; nudge = loud stub (Health issue, no mark, no
  dispatch); NanoID-only keys/subjects; in-flight check is a KV read; pattern/op-meta resolution
  is LIVE registry at dispatch time; zero domain literals outside tests.
- **OCC per the lead's adjudication of Q1**: the `weaver-state` CAS-create is the primary dispatch
  OCC; every remediation op payload carries `expectedRevision` (the row's KV revision — free off
  the CDC message, `Message.Sequence` == KV revision on a KV-backed stream). No Contract #2
  amendment, no Processor-side enforcement. NOTE for Epic 11: walking-skeleton ops tolerate the
  extra payload field; a real `StartLoomPattern`/`CreateTask` inputSchema with
  `additionalProperties: false` would need the field admitted when those payloads are validated.
- **Retry-vs-anti-storm reconciliation** (AC 5's "retry re-reads the mark and re-publishes the
  SAME requestId" vs §10.8's "create loses → drop"): the handler distinguishes a FRESH CDC
  delivery (`Message.NumDelivered == 1`; mark exists → anti-storm drop) from a REDELIVERY
  (`NumDelivered > 1`, i.e. a prior delivery Nak'd after a failed publish or crashed before ack;
  mark exists → re-derive the same episode requestId from the mark's create revision and
  re-publish, idempotent at the Contract #4 tracker). Marks are never updated in 9.1, so the read
  revision IS the create revision.
- **Level-reconciled mark-clearing by candidate enumeration**: marks can only exist at
  `<targetId>.<entityId>.<gapColumn>` for columns the playbook names, so the clearing pass
  enumerates (playbook gaps keys ∪ row's `missing_*` columns) and deletes any candidate whose
  column is not currently true — semantically the story's "list existing marks under the entity
  prefix" without a per-message full-bucket `KVListKeys` scan (substrate has no prefix-filtered
  list, and the story forbids per-message KV scans). Entity deletion (empty body) clears all
  candidates. A stray mark at a column dropped from BOTH the playbook and the row is left for
  9.2's reconciler sweep.
- **verify-kernel surface**: `WeaverTargetsBucket` added to BOTH `internal/bootstrap/verify.go`
  §7 (the callable surface, per the task) AND `scripts/verify-kernel.go`'s duplicated bucket list
  (what `make verify-kernel` actually runs — without it the AC-2 assertion would not be exercised
  by the gate). The loom-state asymmetry in verify.go was NOT touched, per the lead's Q2 ruling.
- **Classify posture**: handlers fully encode outcomes as `Decision` with nil error, so Classify
  is nil (= always transient) — the same accepted posture as Loom 8.5.
- **triggerLoom resolution**: the playbook's `pattern` (literal patternId like "onboarding", a
  vertex NanoID, or a full `vtx.meta.<id>` key) resolves against the live `meta.loomPattern`
  index to the pattern vertex key, which is used as BOTH `payload.patternRef` and
  `authContext.target` (Loom's `patternIDFromRef` accepts the `vtx.meta.<id>` form and its
  registry is keyed by vertex id, so the resolved key is the form that actually round-trips).
  Unresolved pattern/op-meta references Nak for redelivery (the CDC registry replays
  asynchronously) rather than alerting as config errors.
- **assignTask**: `forOperation` resolves via the registry's op-meta index
  (operationType → `vtx.meta.<opId>`, mirroring loom's `indexOpMeta`); `taskId` is
  episode-deterministic (`deriveEpisodeTaskID`, namespace-disjoint from the requestId) so a
  re-fire collapses on the tracker with no duplicate task; grant `expiresAt` = now + 30d
  (mirrors loom's `userTaskGrantTTL` posture).
- **Duplicate-targetId edge**: the later registration is rejected + alerted; if the FIRST owner is
  later tombstoned, the rejected one does not auto-promote until its spec is re-delivered (CDC
  redeliver/update). Accepted 9.1 behavior — the alert keeps it visible.
- Health issues are keyed (`target:<vtxId>`, `gap:<targetId>.<col>`, `data:<targetId>.<col>`) and
  clear when the condition resolves; the heartbeater surfaces them plus pausedStructural
  consumers. Issue keys are bounded by (targets × gap columns), not by entity.
- **Working-tree note for the lead**: `_bmad-output/planning-artifacts/epics/phase-2-epics.md`
  carries the 2026-06-12 Story 9.1/9.2 AC amendment (pre-existing uncommitted state from the
  story-creation phase — NOT edited by this dev session); the gate2/gate3 report files were
  regenerated by the gate runs.
- No contract gaps encountered → no `cmd/weaver/CONTRACT-AMENDMENT-REQUEST.md` needed.

#### Post-review fix batch (2026-06-12, lead-adjudicated; other findings accepted/deferred)

1. **Unresolvable-ref hot loop (BH-F6/ECH-F4/AA-1, the gate item)**: an unresolved
   `meta.loomPattern`/op-meta reference now returns `substrate.NakWithDelay` (bounded cadence,
   unbounded count — loom 8.5 posture) and surfaces a keyed `UnresolvedReference` Health issue,
   cleared on the first successful plan. A later-installed pattern still recovers via redelivery;
   never Term'd (`strategist.go`, `evaluator.go`).
2. **Gap-column charset (ECH-F3)**: install-time validation rejects `gaps` keys that are not a
   single KV-key segment (they become the `<targetId>.<entityId>.<gapColumn>` mark-key segment);
   mark create and level-reconcile delete KV failures now NakWithDelay instead of plain Nak
   (`registry.go` `validateTarget`, `evaluator.go`). The mark *read* failure path still plain-Naks
   (not in the adjudicated batch — flagged for the lead).
3. **Spec-aspect lifecycle (BH-F1/BH-F2/ECH-F2)**: deleting a `spec` ASPECT unregisters the target/
   pattern but keeps the vertex's class (`removeSpec`), so a re-created spec registers immediately;
   `pendingSpecs` is bounded (class recorded for every parsed vertex; pending dropped when the class
   is learned non-routed; evicted on vertex/spec delete); a spec pending past 5 min surfaces an
   `OrphanedSpec` Health issue on the heartbeat cadence (`registry.go`, `health.go`).
4. **Reconcile failure visibility (BH-F3/ECH-F9)**: failed `supervisor.Add`/`UpdateSpec`/`Reset`/
   `Remove` raises a `ConsumerReconcileError` Health issue keyed `consumer:<targetId>`, cleared on
   later success. No retry ticker (accepted posture; 9.2's sweep adds the periodic leg)
   (`engine.go`).
5. **Non-bool row values (ECH-F15)**: a present non-bool `violating`/`missing_*` value surfaces
   (Warn log + `RowDataError` Health issue) and reads conservatively as not-actionable — never a
   silent false (`boolColumn`, `evaluator.go`).
6. **Metadata-unavailable guard (ECH-F16)**: `Sequence == 0` defers the gap on NakWithDelay (never
   publishes `expectedRevision: 0`); `NumDelivered == 0` no longer takes the anti-storm drop — a
   possible redelivery re-fires the same episode requestId (safe side) (`evaluator.go`).
7. **Reserved param (BH-F17/ECH-F21)**: a playbook param named `expectedRevision` is rejected at
   install time instead of silently clobbered at dispatch (`registry.go`).
8. **Instance/lane sanitization (BH-F12/ECH-F18)**: `Engine.Start` rejects a `Config.Instance` or
   `Config.Lane` that is not a single dot-free token (covers `WEAVER_INSTANCE`/`WEAVER_LANE` —
   cmd/weaver exits non-zero); the shared `singleTokenPattern` also covers targetId and gap columns.
9. **Hygiene (AA-4, BH-F11, BH-F16, BH-F19)**: all story/AC/epic-number references stripped from
   production code; the strategist `plan` doc now states idempotency rests on the requestId, not
   payload equality (assignTask `expiresAt` is computed at fire time); the dead `opEnvelope.Class`
   field removed; the dispatchGap doc now describes the redelivery retry as a blanket re-fire of
   every in-flight gap on the row.
10. **Tests (AA-2 + new fixes)**: `evaluator_internal_test.go` (handler-level harness with
    constructed `substrate.Message`) covers fresh+mark drop, redelivery+mark same-requestId
    re-fire, metadata-missing NakWithDelay, NumDelivered-0 re-fire, and the unresolved-ref
    NakWithDelay + Health-issue + late-install recovery; `registry_internal_test.go` covers
    spec-aspect delete→recreate, pending-spec bounds, the OrphanedSpec issue, gap-column charset
    rejection, and the reserved-param rejection.

### File List

New:
- `internal/weaver/doc.go`
- `internal/weaver/engine.go`
- `internal/weaver/registry.go`
- `internal/weaver/evaluator.go`
- `internal/weaver/strategist.go`
- `internal/weaver/actuator.go`
- `internal/weaver/state.go`
- `internal/weaver/health.go`
- `internal/weaver/health_sink.go`
- `internal/weaver/boundary_test.go`
- `internal/weaver/requestid_internal_test.go`
- `internal/weaver/registry_internal_test.go`
- `internal/weaver/evaluator_internal_test.go`
- `internal/weaver/weaver_e2e_test.go`
- `internal/bootstrap/weaver_targets_bucket_test.go`
- `cmd/weaver/main.go`

Modified:
- `internal/bootstrap/primordial.go` (WeaverTargetsBucket constant + ProvisionBuckets entry, ttl false)
- `internal/bootstrap/verify.go` (§7 bucket list + weaver-targets)
- `scripts/verify-kernel.go` (bucket assertion list + weaver-targets)
- `docs/components/weaver.md` (status → Phase 2 in progress; 9.1 shipped-reality section; §10.3 bucket-name corrections)

### Change Log

- 2026-06-12: Story 9.1 implemented — weaver-targets primordial bucket + verify-kernel assertion;
  `internal/weaver` lane-1 engine (registry CDC source with §10.8 validations, per-target
  supervised KV-CDC consumers, level-reconciled CAS-create marks, fire-and-forget OCC actuator,
  Contract #5 health); `cmd/weaver` binary; e2e + unit + boundary tests; weaver.md updated.
  All verification gates green.
- 2026-06-12: Post-review fix batch applied (10 lead-adjudicated items — unresolved-ref
  NakWithDelay + Health, gap-column charset, spec-aspect lifecycle + bounded pendingSpecs,
  reconcile-failure Health issues, non-bool row surfacing, metadata-unavailable guards, reserved
  expectedRevision param, Instance/Lane token validation, comment/dead-code hygiene, new internal
  test coverage). All gates re-run green.

## Questions for Winston (non-blocking — drafted around contract-compliant defaults)

1. **OCC revision-condition depth (AC 4):** the story has the Actuator carry `expectedRevision` in
   the op payload (the entity-row revision off the CDC message). If you want the Processor to
   *enforce* a revision precondition generically (a Contract #2 envelope field), that is a contract
   amendment, not a 9.1 payload convention — confirm the payload-carried condition + mark-OCC is the
   9.1 bar (the §10.8 text calls the CAS-create itself "the OCC", which the story treats as primary).
2. **`verify-kernel` bucket list asymmetry:** `LoomStateBucket` is absent from
   `internal/bootstrap/verify.go`'s §7 list (asserted only by a bootstrap test). The story adds
   `weaver-targets` to the verify.go list per the AC; flag whether loom-state should be backfilled in
   a separate follow-up.
3. **Fixture-vs-Refractor:** the story scopes the "fixture target Lens" as test-written §10.2 rows
   (no Refractor wiring); real target-Lens projection lands with `lease-signing` (Epic 11). Confirm.
4. **`cmd/weaver` inclusion:** included (mirrors `cmd/loom`, ~100 lines) so the engine is runnable
   and 9.4's control surface has a host process. Cut if you want the story thinner.
