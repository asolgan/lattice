# Story 8.5 — Edge Case Hunter review (Loom adopts ConsumerSupervisor)

Method: walked every branching path / boundary in the change set against the supervisor contract
(`internal/substrate/consumer_supervisor*.go`), the pump state machine, `internal/loom/source.go`,
and `docs/contracts/05-health-kv.md`. Reports ONLY unhandled cases. Many of the prompt's candidate
races resolved to "handled" once the serialization model was confirmed — those are listed at the end
so the adjudicator can see they were checked, not skipped.

---

## Findings

### F1 — Removed domain's consumer-state cache entry is never deleted; heartbeat reports a phantom consumer forever  [MEDIUM]

- **Path/boundary:** `reconcileConsumers` Remove branch (`internal/loom/engine.go:425-435`) +
  `consumerStateCache` (`internal/loom/health.go:49-72`, no `delete` method exists).
- **Scenario:** A domain `widget` is referenced, `loom-widget` is Added → its `HealthSink.Load`/
  `SetActive` calls `states.set("loom-widget", "running")`. The last pattern referencing `widget` is
  removed → `reconcileConsumers` calls `supervisor.Remove("loom-widget")` and `delete(e.domains, d)`.
  But nothing removes `"loom-widget"` from `e.states`. The supervisor's `Remove` only deletes the
  durable and stops the pump (`consumer_supervisor.go:107-125`) — it never touches the sink or the
  cache. From the next heartbeat onward (`health.go:emit` → `states.snapshot()`), `metrics.consumers`
  permanently advertises `loom-widget: running` for a consumer that no longer exists. If the consumer
  was paused at removal time (`pausedStructural`), the phantom also keeps emitting a Contract #5
  `ConsumerPaused` *warning* issue forever (`health.go:186-194`) — a stuck health-warning for a torn-
  down consumer, which is exactly the kind of false signal the health plane exists to avoid.
- **Existing test coverage:** NONE. `TestSupervisor_RemovedPatternTearsDownDurable` asserts the
  durable is gone but never re-reads the heartbeat's `metrics.consumers` after removal.
- **Fix sketch:** add `consumerStateCache.delete(name)` and call it in the Remove branch after a
  successful `supervisor.Remove`.

### F2 — Removed domain's per-consumer pause-state entry orphans in health-kv  [LOW]

- **Path/boundary:** `consumerHealthSink` key `health.loom.<instance>.consumer.loom-<domain>`
  (`internal/loom/health_sink.go:38`) vs `supervisor.Remove` (`consumer_supervisor.go:107-125`).
- **Scenario:** `loom-<domain>` is paused (manual/structural) → sink persists
  `health.loom.<instance>.consumer.loom-<domain> = {status:paused,...}`. The domain is then removed.
  `Remove` deletes the durable but the sink is never told to delete its KV entry. The paused entry
  lingers in `health-kv` indefinitely. On a *future re-add of the same domain*, the new consumer's
  `HealthSink.Load` reads that stale entry (`health_sink.go:61-82`) and **restores the freshly re-
  added consumer straight into the old pause** — a re-added domain that an operator never paused
  comes back paused, silently wedging completions for it. This is more than cosmetic: it is a
  correctness footgun for the add→remove→add-same-domain lifecycle the prompt called out.
- **Existing test coverage:** NONE.
- **Fix sketch:** delete the sink key on Remove (give the sink a `Clear(ctx)` and call it from the
  Remove branch), OR have Remove-time reconcile clear the persisted entry. At minimum document the
  re-add-resurrects-pause behavior; today neither code nor docs mention it (the docs only mention the
  *instance-orphan* caveat, F6 below — a different gap).

### F3 — Instance-identity collision corrupts the heartbeat AND every per-consumer pause entry  [MEDIUM]

- **Path/boundary:** `heartbeater.key()` = `health.loom.<instance>` (`health.go:218`) and
  `consumerHealthSink` key = `health.loom.<instance>.consumer.<name>` (`health_sink.go:38`), both
  keyed solely on `cfg.Instance`.
- **Scenario:** Two Loom processes booted with the **same** `Instance` (a misconfigured replica /
  duplicate deployment — and `cfg.Instance` has no uniqueness guard or default in `withDefaults`)
  share one `health-kv` bucket. They last-write-wins each other's `health.loom.<instance>` heartbeat
  (operator sees one flapping liveness/uptime for two processes) AND each writes the *same*
  `...consumer.loom-trigger` pause entry. Worse for restore: process A pauses its trigger, persisting
  `paused`; process B (still active) is unaffected in memory but on B's *next restart* B's
  `HealthSink.Load` reads A's `paused` and restores B's trigger into a pause B's operator never set.
  The doc claims `health.loom.<instance>` is the Contract #5 doc but never states the instance-
  uniqueness precondition.
- **Existing test coverage:** NONE (tests use distinct hard-coded instance names).
- **Fix sketch:** out of scope to *prevent* collisions, but the precondition (instance MUST be
  cluster-unique) should be documented in `docs/components/loom.md`, and ideally `withDefaults` should
  reject an empty `Instance` rather than silently writing `health.loom.` (empty suffix) — see F4.

### F4 — Empty `cfg.Instance` produces malformed health keys, not an error  [LOW]

- **Path/boundary:** `Config.withDefaults` (`engine.go:75-...`) defaults `HealthKVBucket` but never
  validates `Instance`; `heartbeater.key()` / sink key concatenate it raw.
- **Scenario:** If `cmd/loom` ever fails to populate `instance` (or a future caller constructs an
  Engine directly), the heartbeat key becomes `health.loom.` and every per-consumer entry becomes
  `health.loom..consumer.<name>` — silently writing garbage to the health plane rather than failing
  fast. Unlike the other plane buckets, the instance segment is load-bearing for the Contract #5
  key shape and has no default.
- **Existing test coverage:** NONE (all tests pass an explicit instance).
- **Fix sketch:** in `withDefaults`, either default `Instance` to a generated id or have `Start`
  return an error when it is empty.

### F5 — Pause-state restore can fail open when health-kv read errors at Add time  [LOW]

- **Path/boundary:** `consumerHealthSink.Load` (`health_sink.go:61-69`) → `restoreState`
  (`consumer_supervisor_pump.go:354-363`).
- **Scenario:** On a *non-NotFound* KVGet error (health-kv bucket transiently unavailable at engine
  restart — the exact moment restore matters), `Load` returns `(StatusActive, "", err)`. The
  supervisor's `restoreState` logs "health load failed, assuming active" and **runs the consumer
  active** (`pump.go:359-362`). So a consumer an operator deliberately paused (structural — "do not
  touch until I look at it") can silently resume itself if the health bucket blips during the restart
  window. This is the documented HealthSink contract (missing/malformed → active), but the brief's
  intent ("pause-state persists/restores across restart") is violated specifically on a transient
  infra error, which is *more* likely during a coordinated restart than at steady state. Note this is
  a property of the substrate contract, not introduced by Loom — flagging because 8.5 is the first
  consumer to rely on structural-pause durability and inherits the fail-open.
- **Existing test coverage:** `TestSupervisor_PauseStateSurvivesRestart` covers only the happy path
  (clean restart, bucket healthy). The error-during-restore branch is untested.
- **Fix sketch:** acceptable as-is if intentional, but the fail-open-on-infra-error semantics should
  be called out in `docs/components/loom.md` so an operator doesn't assume a structural pause is
  restart-proof.

### F6 — Docs caveat wording understates the in-flight-orphan behavior  [LOW — doc accuracy]

- **Path/boundary:** `docs/components/loom.md` "Known limitation" + `engine.go:reconcileConsumers`
  doc comment.
- **Scenario:** Both say removing a pattern with an in-flight instance "orphans that instance's
  completion … unchanged by delete-vs-preserve (no consumer means no completion delivery either
  way)." That last clause is **only true for the domain that was removed**. A pattern can reference
  multiple domains; if the removed pattern shared a domain with a *surviving* pattern, the domain
  consumer is NOT torn down (correctly — `bindingRegistry` still lists it), so that instance's
  completions on the shared domain *do* keep arriving, but the instance's *pattern definition* is gone
  from `source.known` — `handleCompletion`'s pattern lookup (`source.get`) will miss and drop the
  completion. The orphan mechanism is "pattern def removed", not "consumer torn down"; the docs
  attribute it solely to consumer teardown, which is misleading for the shared-domain case.
- **Existing test coverage:** NONE for the shared-domain-survivor variant.
- **Fix sketch:** reword the caveat to attribute the orphan to pattern-def removal, independent of
  whether the consumer survives.

---

## Verified-and-handled (checked, NOT findings)

- **Reconcile reentrance (load + update callbacks concurrent):** NOT a race. Both callbacks fire from
  the single `patternSource.consume` goroutine (`source.go:100-112`), serialized; `reconcileConsumers`
  additionally holds `e.mu` for the whole diff. No concurrent reconcile within an engine.
- **Two patterns share a domain, one removed:** handled — `bindingRegistry` re-aggregates the full
  snapshot, the domain stays desired, no Remove. (But see F6 for the pattern-def-orphan nuance.)
- **Both relay failure paths → NakWithDelay → 5s floor:** confirmed. `relaySpec.RedeliveryDelay` is 0,
  `applyDecision` substitutes `DefaultRedeliveryDelay` (`consumer.go:230-235`); both publish-fail and
  KVDelete-fail return `NakWithDelay` (`actuator.go`). Publish-success + delete-fail redelivery re-
  publishes — idempotent on the Contract #4 tracker (op-record collapse), claim holds.
- **specFingerprint completeness:** covers stream/filterSubject/deliverPolicy/deliverGroup. AckWait,
  RedeliveryDelay, ProbeInterval are excluded but those don't require a durable recreate (they're pump-
  side, refreshed via UpdateSpec), so their omission is correct, not a drift gap.
- **supervisedHandler (Decision,error) seam:** every wrapped handler (`handleTrigger`,
  `handleCompletion`, `handleDeadline`) returns nil error unconditionally; the relay handler likewise.
  No wrapped path can hand a non-nil error to a nil-Classify spec, so the "classify with no hook"
  worry doesn't materialize.
- **Heartbeater ticker lifecycle:** `run` selects on `ctx.Done()`, stops the ticker (defer), emits a
  final shutdown beat on a detached 2s context. No goroutine leak on engine stop.
- **Concurrent map access (HealthSink callbacks vs heartbeat marshal):** `consumerStateCache` guards
  all access with its mutex and `snapshot()` returns a copy; the heartbeat marshals the copy. Safe.
- **UpdateSpec ok then Reset fails:** `e.domains[d]` stays at the old fp, so the next reconcile re-
  detects the diff and retries (UpdateSpec is an idempotent set). Self-heals.
- **token.<token> pointers after a domain Remove:** pointers live in loom-state keyed by token, not by
  domain; they are resolved by GET on any replica (Contract #10 §10.6) and are unaffected by tearing
  down a domain consumer. No dangling-pointer leak from Remove. (The instance-orphan is F6's concern,
  not a pointer leak.)

---

## Summary

Counts by severity: **MEDIUM 2, LOW 4** (6 findings). No CRITICAL/HIGH.

Top 3:
1. **F1 (MEDIUM)** — removed domain's `consumerStateCache` entry is never deleted; the heartbeat
   reports a phantom `running` (or a stuck `pausedStructural` warning) for a torn-down consumer
   forever. Easy fix, real false health signal, zero test coverage.
2. **F3 (MEDIUM)** — same `Instance` on two processes corrupts both the heartbeat and the per-consumer
   pause entries (cross-restart pause leakage); no uniqueness guard, undocumented precondition.
3. **F2 (LOW, but a correctness footgun)** — removed domain's persisted pause entry orphans in
   health-kv and *resurrects the pause on a future re-add of the same domain*, silently wedging a
   re-added domain an operator never paused.
