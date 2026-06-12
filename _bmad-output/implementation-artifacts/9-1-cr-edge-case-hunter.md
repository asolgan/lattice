# Story 9.1 — Edge Case Hunter review (Weaver violation-driven lane)

Method: walked every branching path / boundary in the change set (`internal/weaver/*`,
`cmd/weaver/main.go`, bootstrap/verify diffs) against the substrate semantics they ride on
(`consumer_supervisor*.go`, `consumer.go`, `subscribe.go`, `kv.go`, `publish.go`) and the frozen
shapes in Contract #10 §10.2/§10.3/§10.8. Reports ONLY unhandled cases. The accepted 9.1 interim
(crash between CAS-create and publish wedges one gap until 9.2's lease; rejected-op wedge; no
reconciler sweep) is NOT re-reported — but several findings below are *distinct* wedge paths that
the story's "redelivery covers a lost publish" claim does not actually cover. Items the prompt
flagged that resolved to "handled" are listed at the end.

---

## Findings

### F1 — Registry-source death is silent: channel close ends the registry forever while the heartbeat keeps reporting healthy  [HIGH]

- **Path/boundary:** `targetSource.consume` (`internal/weaver/registry.go:135-147`) on the
  `!ok` channel-close branch vs `runKVSubscription`'s unrecoverable-error exits
  (`internal/substrate/subscribe.go:183-228` — `defer close(out)`, returns on iterator open
  failure or `mc.Next()` error).
- **Triggering sequence:** any unrecoverable iterator error on the `KV_core-kv` consumer (server
  restart window, consumer deleted out-of-band, sustained connectivity loss exhausting the
  iterator) → `SubscribeKVChanges` closes the events channel → `consume` returns silently. No
  log, no `issueCache` entry, no restart attempt. From then on: no target installs/removals/spec
  updates are ever seen, `reconcileConsumers` never runs again, yet the Contract #5 heartbeat
  (`health.go:194`) keeps emitting `status: "healthy"` with the frozen `targets` count. FR29-class
  silent degradation of the engine's definition source.
- **Existing test coverage:** NONE (no test kills the registry consumer mid-run).
- **Fix sketch:** in `consume`, on `!ok` set a permanent `issues.set("source", "error",
  "RegistrySourceLost", ...)` and either re-`start` the subscription or flip the heartbeat status.

### F2 — Soft-deleting the spec ASPECT wipes the vertex's class; a subsequent spec write buffers forever (target unregistrable until restart)  [HIGH]

- **Path/boundary:** `targetSource.handle` KindAspect `IsDeleted` branch
  (`internal/weaver/registry.go:192-195`) calls `removeVertex(id)`, which deletes `classes[id]`
  (`registry.go:313`) even though the *vertex* still exists with its class.
- **Triggering sequence:** vertex V (`meta.weaverTarget`) + spec S1 → target registered. The spec
  aspect is soft-deleted (package upgrade/uninstall step) → `removeVertex` unregisters the target
  AND forgets V's class. A new spec S2 is then written under V → KindAspect path finds
  `classes[V]` absent → S2 parked in `pendingSpecs` forever (the vertex envelope produces no new
  CDC event, so the buffer is never replayed). The target stays uninstalled with no alert until
  the process restarts (the per-boot replay rebuilds `classes`). Same wedge for `meta.loomPattern`
  spec aspects (triggerLoom resolution lost).
- **Existing test coverage:** NONE (tests tombstone the vertex, never the aspect alone).
- **Fix sketch:** on an aspect delete, unregister the owned target/pattern but keep `classes[id]`
  (the vertex's class is vertex-lifecycle state, not aspect-lifecycle state).

### F3 — Gap-column charset is unvalidated: one bad column name = invalid weaver-state key = plain-Nak hot loop on that subject  [HIGH]

- **Path/boundary:** `validateTarget` checks only the `missing_` prefix of gaps keys
  (`internal/weaver/registry.go:302-306`), and `markCandidateColumns` admits ANY row column with
  the prefix (`internal/weaver/evaluator.go:196-212`); both feed `markKey`
  (`internal/weaver/state.go:50-52`) verbatim. NATS KV keys reject spaces/most non-`[-/_=.a-zA-Z0-9]`
  bytes.
- **Triggering sequence:** (a) a playbook gaps key like `"missing_bg check"` passes install
  validation; the column goes true → `marks.create` errors on the invalid key → `Nak`
  (`evaluator.go:150-152`) → immediate redelivery → infinite hot loop, no health issue. (b) Worse,
  no playbook entry needed: a Lens projecting a row column `"missing_bg check": false` makes it a
  clearing *candidate* → `marks.delete` errors → `clearClosedMarks` returns false → `Nak`
  (`evaluator.go:50-52,184-189`) → every update of that subject hot-loops forever. Both Naks are
  plain (not `NakWithDelay`) so the loop spins at redelivery speed.
- **Existing test coverage:** NONE.
- **Fix sketch:** validate gaps keys against a KV-key-segment pattern at install time (reject +
  alert, like `targetId`), and treat an invalid *row* column as a data error (alert + skip from
  the candidate set) instead of letting `KVDelete` fail.

### F4 — Permanently-unresolvable pattern/operation refs are classified "transient": infinite immediate-redelivery loop with NO health issue  [HIGH]

- **Path/boundary:** `buildPlan` returns `errTransient` for an unresolved `patternMetaKey` /
  `opMetaKey` (`internal/weaver/strategist.go:84-90,119-123`); the evaluator maps `errTransient`
  to a `Warn` log + plain `Nak` (`internal/weaver/evaluator.go:115-119`) — no `issueCache` entry,
  no backoff.
- **Triggering sequence:** a playbook with a typo'd `pattern: "onbaording"` (or naming a pattern
  whose package is not installed, or whose spec failed `indexPattern`'s silent Debug-level
  unwrap/unmarshal bail at `registry.go:347-356`). The row goes violating → Nak → immediate
  redelivery → Nak → … forever. The condition is indistinguishable from genuine replay lag, the
  retry has no floor (`NakWithDelay` exists and is unused), and the health plane never learns —
  a permanent config error presents as a hot CPU/IO loop visible only in logs. FR29.
- **Existing test coverage:** NONE (tests only exercise resolvable refs).
- **Fix sketch:** return `NakWithDelay` for `errTransient` (set `ConsumerSpec.RedeliveryDelay`),
  and set a health issue keyed `gap:<target>.<col>` after the first failure (cleared on
  successful resolution) so a never-resolving ref surfaces.

### F5 — History=1 coalescing defeats the NumDelivered retry disambiguation: a failed publish wedges when the row is upserted before redelivery  [MEDIUM]

- **Path/boundary:** the `inFlight && NumDelivered <= 1` anti-storm drop
  (`internal/weaver/evaluator.go:138-143`) vs `weaver-targets` `MaxMsgsPerSubject=1`
  (`internal/bootstrap/primordial.go:75-79`, asserted in `weaver_targets_bucket_test.go:58`).
- **Triggering sequence:** M1 (violating) delivered → mark CAS-created → publish to `ops.<lane>`
  fails → `Nak`. Before JetStream redelivers, the Lens re-projects the row (M2, same subject,
  still violating) → with history=1 M1 is REMOVED from the stream → its redelivery never happens.
  M2 arrives with `NumDelivered=1` → mark exists → anti-storm drop. The episode's op was never
  published and nothing will ever re-fire it (no lease until 9.2). Same outcome for
  crash-before-ack + interleaved upsert. The retry design ("a lost publish is not wedged behind
  its own mark", `evaluator.go:92-97`) holds only while the exact message survives — on a
  history-1 bucket that is precisely the window a busy Lens closes.
- **Existing test coverage:** NONE (AntiStorm test re-upserts only after a successful publish).
- **Fix sketch:** acknowledge as a 9.1-interim wedge in the docs alongside the CAS/publish-crash
  one, or (better) on the fresh-delivery in-flight path verify the episode's op tracker
  (`vtx.op.<requestId>`) exists before dropping.

### F6 — Coalesced close→reopen: the `false` state is never delivered, so a stale mark shadows the re-opened gap indefinitely  [MEDIUM]

- **Path/boundary:** level-reconciled clearing runs only on delivered row states
  (`internal/weaver/evaluator.go:50,176-191`); history=1 keeps only the latest state per subject.
- **Triggering sequence:** episode A fires for `missing_x` (mark exists). Remediation lands →
  Lens projects `missing_x:false` (M2) → before the consumer pulls M2, the gap re-violates →
  `missing_x:true` (M3) purges M2. Only M3 is ever delivered: the column is true, so clearing
  skips it; the mark (episode A, already completed) exists, `NumDelivered=1` → drop. The
  legitimate re-open is never dispatched until some future delivery shows the column false, or
  9.2's lease expires the mark. Contract §10.3 anticipates exactly this shadow and assigns it to
  episode tagging + lease — 9.1 ships neither leg, and the story's interim note only covers the
  crash-wedge, not the close/reopen shadow.
- **Existing test coverage:** NONE.
- **Fix sketch:** document as a second accepted 9.1 wedge bound (resolved by 9.2's TTL), or
  compare the mark's `claimedAt`/revision against the row's `projectedAt` to detect a prior-
  episode mark.

### F7 — Stale mark escapes BOTH clearing legs when the playbook drops a gaps key and the column leaves the row  [MEDIUM]

- **Path/boundary:** `markCandidateColumns` = current playbook gaps keys ∪ current row's
  `missing_*` columns (`internal/weaver/evaluator.go:196-212`).
- **Triggering sequence:** episode fires for `missing_old` (mark created). A package upgrade
  rewrites the target spec without `missing_old` AND the new Lens stops projecting that column
  (or the entity row is tombstoned after the playbook change — a nil row enumerates only the
  *current* playbook's keys, `evaluator.go:176-183`). Every subsequent delivery's candidate set
  excludes `missing_old` → the mark is never deleted: permanent orphan in `weaver-state`,
  inflating `marksInFlight` forever, and if a later spec re-adds the column the stale mark
  shadows the first new dispatch (F5/F6 mechanics). No sweep exists until 9.2.
- **Existing test coverage:** NONE.
- **Fix sketch:** on a target *update* callback, additionally enumerate marks by the
  `<targetId>.` key prefix (a bounded `KVListKeys` filter) and reconcile those against the row —
  or explicitly fold this into 9.2's sweep scope in the story doc.

### F8 — Removed/renamed target leaves its marks behind; a re-install of the same targetId is shadowed by them  [MEDIUM]

- **Path/boundary:** the reconcile Remove branch (`internal/weaver/engine.go:290-305`) and
  `removeVertex`/rename (`internal/weaver/registry.go:271-283,310-332`) tear down the consumer +
  health entry but never touch `weaver-state` `<targetId>.*` keys; `handleRow` drops rows for
  unregistered targets *before* the clearing pass (`evaluator.go:25-31`).
- **Triggering sequence:** target T fires episodes (marks exist) → T's vertex is tombstoned (or
  its spec renames `targetId`) → consumer removed, marks orphaned. Package re-installed with the
  same `targetId`: `DeliverLastPerSubject` replays the rows fresh (`NumDelivered=1`) → each still-
  open gap with an orphan mark anti-storm-drops, and no redelivery is pending to take the
  `NumDelivered>1` retry leg. Those gaps stay un-remediated until the column flips false or 9.2.
  `marksInFlight` also counts the orphans forever.
- **Existing test coverage:** `TestWeaverE2E_ReconcileTeardownAndReinstall` reinstalls but the
  violating row lands *while uninstalled* — no pre-existing mark, so the shadow path is untested.
- **Fix sketch:** on target removal, delete `weaver-state` keys under `<targetId>.` (the prefix is
  install-validated unique, so the enumeration is safe), or document as a 9.2-lease-resolved bound.

### F9 — A failed supervisor Add/Remove/Reset is retried only on the next registry CDC event; a quiet registry never converges  [MEDIUM]

- **Path/boundary:** `reconcileConsumers` error branches `continue` after logging
  (`internal/weaver/engine.go:268-271,279-286,295-298`); reconcile is invoked solely from
  source load/update callbacks + once at Start (`engine.go:195-200`). There is no periodic
  re-reconcile tick.
- **Triggering sequence:** engine boots while NATS JetStream is briefly degraded →
  `supervisor.Add` fails for target T → logged, `e.targets[T]` unset. No further meta.* mutation
  ever arrives (stable installed set) → T's consumer is never created; violating rows accumulate
  unwatched. Heartbeat reports `targets: N` but `consumers` lacks T — no issue is raised, so the
  discrepancy is only visible by diffing two metrics fields. Same for a failed Remove (durable
  leaks + keeps delivering to a handler whose target lookup now drops everything).
- **Existing test coverage:** NONE.
- **Fix sketch:** re-run `reconcileConsumers` on the heartbeat tick (cheap diff, already
  serialized by `e.mu`), and/or set an issue keyed `consumer:<targetId>` on Add/Remove failure.

### F10 — Duplicate-targetId loser is never re-evaluated after the winner is removed  [MEDIUM]

- **Path/boundary:** uniqueness rejection (`internal/weaver/registry.go:240-245`) does not record
  the rejected body; `removeVertex` of the winning vertex (`registry.go:310-332`) unregisters the
  targetId but replays nothing.
- **Triggering sequence:** vertex A registers `targetId=T`; vertex B (same T) is rejected with an
  alert. A is later tombstoned (the conflicting package uninstalled — the natural repair) → T is
  now free, but B's registration is only retried if B's spec aspect is *rewritten*. The system
  sits with T uninstalled and B's stale `TargetRejected` issue until an operator re-touches B or
  the process restarts (replay re-dispatches B after A's tombstone). Loud but non-convergent.
- **Existing test coverage:** `TestWeaverE2E_InstallValidations` covers the rejection, not the
  winner-removal recovery.
- **Fix sketch:** buffer rejected-for-duplicate specs keyed by targetId and re-dispatch them when
  `removeVertex` frees that targetId (or document restart as the recovery).

### F11 — Soft-delete → restore of a target vertex leaves it unregistered until restart  [MEDIUM]

- **Path/boundary:** `handle` KindVertex non-deleted path (`internal/weaver/registry.go:163-185`)
  re-learns the class but only replays `pendingSpecs`; a restored vertex's spec aspect produces no
  new CDC event.
- **Triggering sequence:** target vertex V is soft-deleted (envelope `isDeleted:true` → `removeVertex`,
  consumer torn down) then restored (`isDeleted:false` rewrite — the Processor un-delete path).
  The vertex event arrives, `classes[V]` is re-set, but the spec was consumed long ago and is not
  in `pendingSpecs` → the target stays unregistered, silently, until the next process restart
  replays history. (Mirror image of F2; same root: derived state keyed to events that won't recur.)
- **Existing test coverage:** NONE.
- **Fix sketch:** on a routed vertex (re-)arrival with no owned registration, KVGet the spec
  aspect (`vtx.meta.<id>.spec`) directly and dispatch it — one bounded read, no replay dependence.

### F12 — Two replicas sharing the per-target durable can process same-subject states out of order: a closed gap gets a fresh mark + a stale-row dispatch  [MEDIUM]

- **Path/boundary:** the lane-1 durable name is `weaver-target-<targetId>` with no instance
  suffix and no `DeliverGroup` (`internal/weaver/engine.go:224-235`) — a second Weaver process
  attaches to the SAME durable and messages split across the two pumps; nothing serializes
  same-subject handling across replicas.
- **Triggering sequence:** M1 (violating) delivered to slow replica R1; the gap closes → M2
  (non-violating) delivered to R2, which deletes the mark; R1 *then* finishes M1: mark absent →
  CAS-create succeeds → fires an episode from the stale row (stale `expectedRevision` → the op is
  OCC-rejected downstream — good) and leaves a fresh mark on a *closed* gap. With no further row
  updates and no lease, that mark persists, and a genuine re-open is shadowed (F6 mechanics). The
  marks doc claims "any replica resolves it" (`engine.go:112-115`) but the ordering assumption is
  single-consumer.
- **Existing test coverage:** NONE (single engine per test).
- **Fix sketch:** document single-replica-per-target as a 9.1 precondition in
  `docs/components/weaver.md` (the control surface is 9.4), or compare the row's `projectedAt`
  against the mark's `claimedAt` before CAS-creating from a row older than the last clear.

### F13 — Several health issues are set on paths that can never clear them  [LOW]

- **Path/boundary:** `issueKeyData(targetID, "entityKey")` (`internal/weaver/evaluator.go:67-69`)
  is cleared nowhere ("entityKey" is never a gap column, and the clear calls at
  `evaluator.go:130-131` key on gap columns). `GapWithoutPlaybook` / `TemplateDataError`
  (`evaluator.go:105-107,121-123`) are cleared only inside `dispatchGap`, which runs only while
  the column is true — if the gap simply closes (or the row is deleted) the issue outlives the
  condition forever.
- **Triggering sequence:** Lens bug ships a violating row without `entityKey` → alert; Lens fixed,
  rows now carry it → dispatches proceed but the `RowDataError` issue stays in every heartbeat
  until process restart. Same for a `missing_*`-without-playbook column that stops being projected.
- **Fix sketch:** clear `issueKeyData(targetID,"entityKey")` on the first well-formed violating
  row; clear gap/data issues for a column inside `clearClosedMarks` when the column is observed
  false/absent.

### F14 — Malformed row key / unparseable row JSON: Warn + Ack only — no Health KV issue, and the JSON case skips mark-clearing  [LOW]

- **Path/boundary:** `internal/weaver/evaluator.go:19-24` (key not `<targetId>.<NanoID>`) and
  `evaluator.go:36-41` (unmarshal failure → return BEFORE `clearClosedMarks`).
- **Triggering sequence:** a buggy Lens projects garbage JSON for an entity whose previous state
  created marks → every update is dropped at Warn level (engine-side FR29 alerting uses
  `e.alert` everywhere else, but not here), and existing marks for that entity are never
  reconciled as long as the value stays unparseable.
- **Fix sketch:** route both through `e.alert` (keyed per target), and run `clearClosedMarks`
  with a nil row check… (note: nil row clears ALL candidates, which is wrong while violating
  state is unknown — so alert-only is the right minimal fix).

### F15 — Non-bool `violating` / `missing_*` values are silently treated as false: a type-sloppy Lens disables remediation AND clears live marks  [LOW]

- **Path/boundary:** `row["violating"].(bool)` (`internal/weaver/evaluator.go:57`),
  `row[col].(bool)` in clearing (`evaluator.go:181`) and `openGapColumns` (`evaluator.go:223`).
- **Triggering sequence:** a Lens (or fixture) projects `"violating": "true"` or
  `"missing_x": 1` → type assertion yields false → the row is treated as non-violating, the
  clearing pass DELETES any in-flight marks for it, and nothing dispatches — with zero log/issue.
  §10.2 freezes these as bools, so a non-bool is a data error that per FR29 should alert, not
  silently invert the level.
- **Fix sketch:** distinguish "present but not a bool" from "false/absent"; alert via
  `issueKeyData` on the former and skip the row (Ack) rather than clearing on it.

### F16 — `NumDelivered==0` / `Sequence==0` (JetStream metadata unavailable) take the wrong branches  [LOW]

- **Path/boundary:** `Message.NumDelivered` is documented zero-when-unavailable
  (`internal/substrate/consumer.go:56-60`, `consumer.go:213-216`); `msg.NumDelivered <= 1`
  (`internal/weaver/evaluator.go:139`) classifies 0 as a FRESH delivery, and `msg.Sequence` feeds
  `expectedRevision` (`evaluator.go:110-113`) so 0 publishes `expectedRevision: 0`.
- **Triggering sequence:** a metadata-less redelivery with an existing mark → anti-storm drop
  instead of the retry re-fire (episode wedge); separately a 0 sequence ships an op whose OCC
  condition is the "must not exist" sentinel → guaranteed downstream rejection → mark wedge.
- **Fix sketch:** treat `NumDelivered==0` + existing mark as the retry path (re-fire is the safe
  side — same requestId collapses), and refuse to fire on `Sequence==0` (Nak).

### F17 — Per-boot registry durables are never deleted: one parked `KV_core-kv` consumer leaks per process start  [LOW]

- **Path/boundary:** `targetSourceDurablePrefix + "-" + instance` (`internal/weaver/registry.go:120-127`,
  instance contains a per-construction NanoID, `engine.go:69-80`) on a subscription whose shutdown
  deliberately preserves durables (`internal/substrate/subscribe.go:160-175`).
- **Triggering sequence:** every engine start (incl. every restart in a crash loop, every test
  run against a shared server) creates a new durable on `KV_core-kv` filtered `vtx.meta.>` that
  is never consumed again and never removed — unbounded catalog growth and retained interest.
- **Fix sketch:** delete the registry-source durable on engine shutdown (it is per-boot by design,
  so its persisted position is worthless), e.g. an explicit DeleteConsumer in `Start`'s teardown.

### F18 — `WEAVER_INSTANCE` / `Config.Instance` and `WEAVER_LANE` are used unvalidated in KV keys, durable names, and the ops subject  [LOW]

- **Path/boundary:** only the *auto-generated* hostname is sanitized
  (`internal/weaver/engine.go:60-79`); an explicit `Instance` flows raw into
  `health.weaver.<instance>` (`health.go:246-248`), the per-consumer key (`health_sink.go:41`) and
  the registry durable name (`registry.go:125`). `Lane` flows raw into `ops.<lane>`
  (`actuator.go:78`) and the envelope. `cmd/weaver` passes env values through untouched
  (`cmd/weaver/main.go:48-57`).
- **Triggering sequence:** `WEAVER_INSTANCE=prod.weaver.1` → durable name with dots → consumer
  creation fails at start (loud but cryptic) after the heartbeater has already written a
  mis-segmented health key; `WEAVER_LANE=system.foo` (or with a space) → ops published to an
  unintended/invalid subject — fire-and-forget, so rejected silently.
- **Fix sketch:** sanitize/validate `Instance` and `Lane` in `withDefaults` (reject or replace
  dots/invalid subject tokens), mirroring the `targetId` regex discipline.

### F19 — Missing bootstrap file: cmd/weaver silently mints fresh primordial IDs, then every op is rejected with no observable failure  [LOW]

- **Path/boundary:** `bootstrap.LoadOrGenerate` (`cmd/weaver/main.go:60-63`) generates a new ID
  set when the file is absent; the `freshlyGenerated` return is discarded. `cmd/loom` shares this
  pattern, but Loom *observes* op rejection via its deadline backstop — Weaver's submit is pure
  fire-and-forget with no lease until 9.2.
- **Triggering sequence:** wrong `BOOTSTRAP_JSON_PATH` in a deployment → fresh `WeaverIdentityKey`
  that matches no provisioned identity → every remediation op rejected at the Processor → marks
  wedge engine-wide; logs show successful submits, heartbeat healthy.
- **Fix sketch:** error (or at minimum log loudly) when `freshlyGenerated` is true in cmd/weaver —
  a weaver process should never be the one minting platform IDs.

### F20 — `internal/bootstrap/verify.go` bucket list (edited by this diff) still omits `loom-state`; `scripts/verify-kernel.go` includes it  [LOW]

- **Path/boundary:** `internal/bootstrap/verify.go:163-171` checks
  `{core-kv, health-kv, capability-kv, weaver-state, weaver-claims, weaver-targets,
  refractor-adjacency}` — no `LoomStateBucket` — while the same list in
  `scripts/verify-kernel.go:263-268` carries it. The diff touched exactly these lines in both
  files and propagated the drift.
- **Triggering sequence:** an environment missing the `loom-state` bucket passes
  `VerifyKernel()` (the library entry point) but fails `make verify-kernel` — two verifiers, two
  answers.
- **Fix sketch:** add `LoomStateBucket` to the `verify.go` list (or derive both lists from one
  shared slice).

### F21 — A `directOp` playbook param named `expectedRevision` is silently clobbered  [LOW]

- **Path/boundary:** `internal/weaver/strategist.go:154-162` — user params are resolved into
  `params`, then `params["expectedRevision"] = expectedRevision` overwrites any same-named
  playbook param without error or alert.
- **Triggering sequence:** a package author passes `params: {expectedRevision: "row.someRev"}`
  expecting it to reach the op; the engine replaces it with the CDC sequence — config error
  masked, op behaves unexpectedly.
- **Fix sketch:** reject `expectedRevision` (and any future engine-reserved name) as a params key
  in `validateTarget`/`buildPlan` with an `errConfig`.

---

## Checked and resolved as handled

- **Crash orderings around the mark (terrain 1):** crash before publish → no ack → redelivery →
  `NumDelivered>1` + mark → same-episode re-fire; crash after publish before ack → re-fire
  collapses on the Contract #4 tracker; concurrent replica CAS race → loser drops. All handled
  *provided the same message is still redeliverable* — the history-1 hole is F5.
- **Empty body = entity tombstone:** `decodeKVMessage`/`handleRow` agree (empty body → nil row →
  clear-all candidates incl. playbook keys). NATS `kv.Create` succeeds over a delete marker, so a
  re-violating entity after deletion re-dispatches correctly.
- **Mark `delete` of a missing key is success** (`state.go:102-108`) — clearing by candidate, not
  presence, is correctly idempotent.
- **`reconcileConsumers` serialization:** whole pass under `e.mu`; callbacks come from the single
  `consume` goroutine; `e.ctx` guard prevents post-Stop Adds (Add-after-Stop also rejected by the
  supervisor itself). Stop waits for pump completion before returning.
- **Spec-change Reset leg:** fingerprint diff is mechanically dead today (name-derived filter) but
  written generically and correct; UpdateSpec-then-Reset ordering matches the supervisor contract.
- **Health-sink delete on Remove** (the Loom 8.5 F1/F2 lessons) is implemented
  (`engine.go:300-303`, `health_sink.go:91-97`): no phantom consumer, no resurrected pause on
  re-add.
- **`unwrapSpecBody`** handles bare, envelope-wrapped, and degenerate bodies; a gaps-less target
  registers with an empty playbook and every true column alerts (FR29 path, e2e-tested).
- **`violating:true` with no `entityKey`** alerts and does not fire (handled; the *clear* of that
  issue is F13).
- **Duplicate `patternId` across two pattern vertices:** last-writer-wins, and deleting the winner
  drops the mapping (the loser is not restored) — same event-replay root as F10/F11; folded there
  rather than reported separately.
- **Heartbeat shutdown write** uses a detached 2s context (`health.go:188-191`) — no write-after-
  cancel loss; `emit` tolerates a failed mark scan by omitting the metric.
