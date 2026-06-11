# Story 8.5 — Blind Hunter (diff-only adversarial review)

Scope: `git diff HEAD` for `internal/loom/engine.go`, `internal/loom/actuator.go`,
`cmd/loom/main.go`, `docs/components/loom.md`, plus untracked
`internal/loom/health.go`, `internal/loom/health_sink.go`,
`internal/loom/supervisor_test.go`, `internal/loom/relay_internal_test.go`,
`internal/loom/export_test.go`. No story/spec/epics context consulted (diff-only
lens). `_bmad-output/` changes ignored except this file.

`go build ./...`, `go vet ./internal/loom/...`, and `go test ./internal/loom/... -short`
all pass.

---

## Findings

### 1. [Major] `Engine.Start` leaks supervisor pump goroutines on every early-return error path

**File/line:** `internal/loom/engine.go:197-219` (new `Start`)

```go
func (e *Engine) Start(ctx context.Context) error {
	e.ctx = ctx

	for _, spec := range []substrate.ConsumerSpec{e.triggerSpec(), e.relaySpec(), e.deadlineSpec()} {
		if err := e.supervisor.Add(ctx, spec); err != nil {
			return fmt.Errorf("loom: add %s consumer: %w", spec.Name, err)
		}
	}
	...
	if err := e.source.start(ctx); err != nil {
		return fmt.Errorf("loom: start pattern source: %w", err)
	}
	...
	<-ctx.Done()
	e.supervisor.Stop()
	return nil
}
```

**What:** `e.supervisor.Stop()` is reached only via the final `<-ctx.Done()` path. Both
early-return error paths (a failed `Add` for the relay/deadline consumer after the
trigger consumer's `Add` already succeeded; or a failed `e.source.start(ctx)` after all
three fixed consumers' `Add` calls succeeded) return without calling `Stop()`.

**Why this is a real bug, not just style:** `ConsumerSupervisor.Add` (verified in
`internal/substrate/consumer_supervisor.go:77`) starts each pump goroutine on
`pumpCtx, cancel := context.WithCancel(context.Background())` — i.e. **independent of
the `ctx` passed into `Start`**. The *only* way to stop an already-`Add`ed pump is
`supervisor.Stop()` (which calls `mc.cancel()` for every managed consumer) or
`supervisor.Remove(name)`. Compare with the **previous** code, where each consumer ran
as `go e.runTriggerConsumer(ctx)` / `go e.relay.run(ctx)` / `go e.runDeadlineWatcher(ctx)`
— all three were tied directly to the caller's `ctx`, so even an early `Start` return
(with the caller subsequently cancelling `ctx`, e.g. `cmd/loom/main.go`'s
`defer cancel()`) would unwind those goroutines.

After this rewrite, if (say) `e.supervisor.Add(ctx, e.relaySpec())` fails after
`e.supervisor.Add(ctx, e.triggerSpec())` already succeeded:
- the `loom-trigger` pump goroutine keeps running forever, holding its JetStream
  `Consumer`/`Messages` iterator open and processing trigger events,
- `Start` returns an error,
- `cmd/loom/main.go`'s `defer cancel()` cancels `ctx`, but the leaked pump's `pumpCtx`
  is `context.Background()`-derived and never observes that cancellation,
- nothing in `Engine` retains a reference that lets a caller clean this up — `e.supervisor`
  is private and `Stop()` was never called.

**Blast radius today:** in the `cmd/loom` binary this is masked because `run()` returns
the error and the process exits (OS reclaims everything) — see `cmd/loom/main.go:100-102`.
But:
- `internal/loom/supervisor_test.go`'s `TestSupervisor_PauseStateSurvivesRestart`
  constructs **two engines (`e1`, `e2`) in the same test process** via
  `go func() { _ = e1.Start(ctx1) }()` / `go func() { _ = e2.Start(ctx2) }()`. If either
  engine's `Start` ever takes one of these early-return paths (e.g. a transient
  `CreateOrUpdateConsumer` error against the test NATS server), its pump goroutines and
  durable-consumer registrations from the *first* engine are never torn down and can
  collide with / interfere with the second engine's reconcile and durable names
  (`loom-trigger`, `loom-outbox-relay`, `loom-deadline` are shared names on the same
  stream).
- Any future non-CLI embedding of `loom.Engine` (long-lived process, multi-tenant host,
  test harness that retries `Start`) inherits a permanent per-attempt goroutine + NATS
  consumer leak.

**Verdict:** Real, diff-introduced regression vs. the prior behavior (prior code's
goroutines were `ctx`-scoped; new code's supervisor pumps are not). Low likelihood of
triggering in steady-state `cmd/loom` (requires a `CreateOrUpdateConsumer`/source-start
failure on startup), but when it does trigger it is a silent, unrecoverable-without-
process-restart leak. Fix: call `e.supervisor.Stop()` (or equivalent) on every
early-return path out of `Start`, e.g. `defer` it conditionally, or restructure so
`Stop()` runs via `defer` once any `Add` has succeeded.

---

### 2. [Minor] First heartbeat ("starting") can be emitted before any consumer's `HealthSink.Load` has populated `metrics.consumers`

**File/line:** `internal/loom/engine.go:200-207`; `internal/loom/health.go:155-156` (`heartbeater.run` → `h.emit(ctx, "starting")`)

**What:** `e.supervisor.Add(...)` for trigger/relay/deadline launches each pump as
`go func() { ... s.runPump(...) }()` (supervisor internal, fire-and-forget — `Add`
returns before the goroutine runs). Immediately after the `Add` loop, `Start` does
`go hb.run(ctx)`, whose first action is `h.emit(ctx, "starting")` — synchronously, with
no synchronization against the pumps' `restoreState`/`Load` calls (which is what calls
`consumerStateCache.set(name, ...)` for each consumer, per `health_sink.go:44-82`).

**Why:** The very first heartbeat document (`status: "starting"`) written to
`health.loom.<instance>` can legitimately have an empty or partial `metrics.consumers`
map — none, some, or all of `loom-trigger` / `loom-outbox-relay` / `loom-deadline` may
be missing depending on goroutine scheduling. Subsequent heartbeats (10s cadence) will
be complete. `internal/loom/supervisor_test.go`'s
`TestSupervisor_HealthHeartbeatWellFormed` accommodates this by waiting up to 12s for
all three keys to appear before asserting on the document — i.e. the test was written
*around* this race, not testing the "starting" doc's shape directly.

**Verdict:** Cosmetic / eventually-consistent, not a functional bug — but if Contract #5
(not consulted, per diff-only scope) requires `metrics.consumers` to be a *complete*
map on every persisted heartbeat (including "starting"), this is a spec violation on
the first write. Marked uncertain because the spec wasn't read.

---

### 3. [Minor] `formatISODuration` is duplicated verbatim (with a slightly different code shape) from `internal/processor/health.go`

**File/line:** `internal/loom/health.go:222-238` vs `internal/processor/health.go:205-222`

**What:** Both implementations render a `time.Duration` as `PT…S`/`PT…M…S`/`PT…H…M…S`.
Loom's version branches on `seconds < 60` / `< 3600` / else; Processor's branches on
`hours > 0` / `mins > 0` / else, using `fmt.Sprintf`. Spot-checked equivalence for
`0s`, `65s`, `3600s`, `3661s` — all four cases produce identical output
(`PT0S`, `PT1M5S`, `PT1H0M0S`, `PT1H1M1S`).

**Why flagged:** Pure duplication of a small pure function across two packages with no
shared home (`internal/substrate` would be the natural place, given `FormatTimestamp`
already lives there per `internal/substrate/envelope.go:49` and the new
`loomHealthDoc`/heartbeat code already imports `substrate`). Not a correctness bug
(verified equivalent on the cases checked), but it's the kind of "two divergent copies
will eventually disagree on an edge case nobody tests" debt — e.g. negative durations
are clamped identically (`d < 0 → d = 0`) in both, but a future tweak to one won't
propagate to the other.

**Verdict:** Nit — pure duplication, no behavioral difference found, low priority.

---

### 4. [Nit] `runningInstanceCounter.count` silently swallows all `KVGet` errors (not just not-found)

**File/line:** `internal/loom/health.go:108-111`

```go
entry, err := r.conn.KVGet(ctx, r.bucket, k)
if err != nil {
    continue
}
```

**What:** Any `KVGet` error for a key matching `instance.*` — not just
`ErrKeyNotFound` for a since-deleted instance, but also a transient bucket/connection
error — is silently dropped from the `runningInstances` count with **no log line**.
Contrast with `heartbeater.emit`, which does log (`Warn`) when `counter.count` itself
returns an error.

**Why flagged:** A systemic KV read problem (e.g. health-kv/loom-state degraded) would
manifest only as an under-reported `runningInstances` metric on the heartbeat, with
zero diagnostic trail pointing at *why* — the heartbeat itself reports "healthy"
(status comes from the ticker, not from this scan). This is the kind of thing that's
fine 99.9% of the time and actively misleading during an incident.

**Verdict:** Low-severity, plausible-but-minor observability gap. Not a functional
correctness bug for the story's stated AC (heartbeat shape / consumer health), but
worth a one-line `Warn` log on the per-key `KVGet` error path (rate-limited or just
logged-and-continue) if this is meant to be an operator-facing signal.

---

### 5. No findings on (checked, clean)

- **ack/nak correctness in `relay.handle`** (`internal/loom/actuator.go:69-105`): both
  failure-return paths now correctly return `(substrate.NakWithDelay, nil)` instead of
  the old `substrate.Nak`; the empty-body delete-marker and unparseable-record (`Term`)
  paths are unchanged in behavior, just re-shaped to the new `(Decision, error)` tuple.
  `RedeliveryDelay` is left zero on `relaySpec()` (`engine.go:273-282`), correctly
  falling back to `substrate.DefaultRedeliveryDelay` (5s, confirmed in
  `internal/substrate/consumer.go:46`) — matches the doc's claim.
- **Map access without locks**: `e.domains` (new `map[string]specFingerprint`) is only
  read/written inside `reconcileConsumers`, which holds `e.mu` for its entire body,
  including the trailing removal-diff loop (which deletes from `e.domains` while
  ranging over it — legal in Go). `consumerStateCache` (`health.go:49-72`) has its own
  mutex and is used consistently via `set`/`snapshot`. No unguarded map access found.
- **`e.ctx` race**: `e.ctx = ctx` is set synchronously at the top of `Start`, before any
  goroutine that reads it (`reconcileConsumers` via source callbacks, registered later
  in the same function) can run. The `e.ctx == nil` guard in `reconcileConsumers`
  (`engine.go:208-210`) is dead code in practice but harmless.
- **Filter-subject / stream-name consistency**: `relaySpec()`'s
  `"$KV." + e.cfg.LoomStateBucket + "." + outboxPrefix + ">"` matches `relay.subjPrefix`
  (`"$KV." + bucket + "."`, `actuator.go:56`) since both are constructed from
  `cfg.LoomStateBucket`. `deadlineSpec()`'s `subjPrefix` matches what `handleDeadline`
  expects (unchanged signature/usage). `domainSpec()`'s `events.<domain>.>` matches the
  prior `runDomainConsumer`'s filter.
- **`go build ./...`, `go vet ./internal/loom/...`, `go test ./internal/loom/... -short`**
  all pass cleanly.
- **No banned history/changelog comments** (`// Story …`, `// Previously …`,
  `// Was: …`, `// renamed from …`, `// moved from …`) found in any of the
  diff/untracked files.
- **Test-only seams**: `internal/loom/export_test.go` (`PauseForTest`,
  `ResetDomainForTest`) is a `_test.go` file — excluded from the production binary by
  Go's build rules. Confirmed `go build ./...` succeeds and these symbols don't leak
  into non-test files.
- **`internal/loom/doc.go`** (unmodified, not in this diff) still describes the old
  `Conn.RunDurableConsumer`-based architecture — now stale relative to the
  `ConsumerSupervisor` rewrite, but out of scope for a diff-only review since the file
  has no uncommitted changes.

---

## Verdict Table

| # | Severity | File:Line | Summary |
|---|----------|-----------|---------|
| 1 | Major | `internal/loom/engine.go:197-219` | `Start`'s early-return error paths skip `supervisor.Stop()`, leaking pump goroutines + durable consumers (decoupled from caller `ctx`); masked by process-exit in `cmd/loom` but live risk for in-process multi-engine tests / future embeddings |
| 2 | Minor | `internal/loom/engine.go:200-207`, `internal/loom/health.go:155-156` | First ("starting") heartbeat may have an incomplete `metrics.consumers` map due to unsynchronized pump-startup vs. heartbeat-emit ordering; uncertain whether this violates Contract #5 (not consulted) |
| 3 | Minor | `internal/loom/health.go:222-238` | `formatISODuration` duplicated from `internal/processor/health.go`; verified equivalent on spot-checked cases, but is divergence-prone duplication with no shared home despite `substrate.FormatTimestamp` precedent |
| 4 | Nit | `internal/loom/health.go:108-111` | `runningInstanceCounter.count` swallows all per-key `KVGet` errors silently (no log), unlike the outer `heartbeater.emit` which does log on a hard `count` failure |

**Counts:** Critical: 0, Major: 1, Minor: 2, Nit: 1.
