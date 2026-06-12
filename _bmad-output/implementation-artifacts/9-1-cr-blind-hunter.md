# Story 9.1 — Blind Hunter (diff-only adversarial review)

Scope: uncommitted working tree — `git diff` for `docs/components/weaver.md`,
`internal/bootstrap/primordial.go`, `internal/bootstrap/verify.go`,
`scripts/verify-kernel.go`; full content of untracked `cmd/weaver/main.go`,
`internal/weaver/*` (doc.go, engine.go, registry.go, evaluator.go, strategist.go,
actuator.go, state.go, health.go, health_sink.go, boundary_test.go,
requestid_internal_test.go, weaver_e2e_test.go), and
`internal/bootstrap/weaver_targets_bucket_test.go`. No story/spec/contract context
consulted (diff-only lens). `_bmad-output/` changes ignored except this file.

`go build ./...`, `go vet ./internal/weaver/... ./cmd/weaver/...`, and
`go test ./internal/weaver/... ./internal/bootstrap/... -short` all pass.

---

## Findings

### F1. [Major] Deleting a `spec` aspect wipes the vertex's class entry — a re-created spec is buffered forever and the target/pattern is bricked until restart

**File/line:** `internal/weaver/registry.go:187-204` (`handle`, KindAspect branch) + `registry.go:310-332` (`removeVertex`)

```go
case substrate.KindAspect:
	...
	if evt.IsDeleted {
		s.removeVertex(id)   // <-- also does: delete(s.classes, id)
		return
	}
```

`removeVertex` deletes `s.classes[id]`, but on the aspect-delete path the *vertex
still exists* and will never be re-delivered (its KV value is unchanged, and the
durable replays history only at boot). If a `spec` aspect is later re-created
(delete + re-install, or a fix-forward of a bad spec), the new spec finds
`routed == false` and is parked in `pendingSpecs` permanently. The target (or
loom-pattern index entry) silently never registers until the vertex itself is
rewritten or the process restarts. The same path also tears down the vertex's
pattern/op-meta index entries on a mere aspect deletion.

**Failure scenario:** operator deletes a bad `vtx.meta.<id>.spec`, writes a
corrected one → no consumer ever comes up, no rejection issue either (the spec is
"pending"), and the Health doc shows nothing wrong.

### F2. [Major] `pendingSpecs` is an unbounded leak: every non-routed meta vertex with a `.spec` aspect is buffered forever

**File/line:** `internal/weaver/registry.go:172-175` (vertex branch), `registry.go:196-201` (aspect branch)

A vertex whose class is neither `meta.weaverTarget` nor `meta.loomPattern` never
gets a `classes[id]` entry (the vertex branch routes it to `indexOpMeta` and
returns). When that vertex's `spec` aspect arrives — which it will for *every*
other spec-carrying meta class in the bucket (lenses, schedules, whatever shares
`vtx.meta.>`) — the aspect branch sees `routed == false` and copies the full spec
body into `pendingSpecs`, where nothing ever drains it (only a routed vertex
arrival or `removeVertex` deletes the entry). Memory grows with every installed
spec-bearing meta vertex and every spec *update* re-copies the body
(`append([]byte(nil), evt.Value...)` overwrites, so it's one body per vertex —
but the set itself is unbounded and 100% garbage for non-routed classes).

### F3. [Major] A failed `supervisor.Add`/`Remove` is logged and forgotten — no retry, no Health issue; the target stays unwatched indefinitely

**File/line:** `internal/weaver/engine.go:267-271` (Add), `engine.go:294-298` (Remove)

```go
if err := e.supervisor.Add(e.ctx, spec); err != nil {
	e.logger.Error("weaver target consumer add failed", "targetId", id, "err", err)
	continue
}
```

`reconcileConsumers` runs only on registry load/update callbacks (plus the one
seed at `Start`). There is no periodic re-reconcile and no ticker. A transient
JetStream error during `Add` leaves a *registered* target with no lane-1 consumer
until some unrelated meta-vertex mutation happens to fire a callback — possibly
never. The failure also does not go through `issues.set`, so the Contract #5
heartbeat keeps reporting `healthy` with the target counted in `targets` but
absent from `consumers`. This contradicts the loud-failure posture used
everywhere else in the diff (`alert`, `rejectTarget`). The failed-`Remove` branch
has the same property (leaked durable keeps dispatching for a removed target
until the next callback).

### F4. [Major] Per-boot registry durables are never cleaned up — every boot leaks a server-side consumer on the core-kv backing stream

**File/line:** `internal/weaver/registry.go:117-133` (`start`), `engine.go:44-50` (Config.Instance doc)

The registry source subscribes with durable name
`weaver-target-source-<instance>` where `<instance>` is unique per boot
(`<hostname>-<pid>-<NanoID>` default; tests and cmd/weaver also generate fresh
ones). The diff contains no deletion path for this durable — not on shutdown, not
on next boot (the next boot uses a *new* name, so it can't even adopt the old
one). Unless `SubscribeKVChanges` internally creates an auto-cleanup/ephemeral
consumer (not visible in this diff — verify), every Weaver restart permanently
accumulates one durable on the `KV_core-kv` stream. The diff itself states the
standard: "an un-pumped server-side durable IS a leak" (`engine.go:243-244`).

### F5. [Major] `cmd/weaver` silently *generates* fresh primordial IDs when the bootstrap JSON is missing — the engine runs "healthy" with an actor key nothing recognizes

**File/line:** `cmd/weaver/main.go:59-63`

```go
if _, err := bootstrap.LoadOrGenerate(bootstrapJSONPath); err != nil {
	return fmt.Errorf("load primordial IDs: %w", err)
}
actorKey := bootstrap.WeaverIdentityKey
```

A typo'd `BOOTSTRAP_JSON_PATH` (or running from the wrong cwd with the `./`
default) doesn't fail — `LoadOrGenerate` mints a brand-new ID set, and
`actorKey` is a key that matches no seeded identity. Because the Actuator is
fire-and-forget with no reply, every remediation op is presumably rejected
downstream at auth with zero feedback in Weaver: marks get created, ops vanish,
gaps wedge, heartbeat stays `healthy`. A consumer-only binary should *load*, and
fail hard when the file is absent.

### F6. [Major] An unresolvable pattern/operation reference is classified transient forever → infinite Nak/redelivery loop, never surfaces as a Health issue

**File/line:** `internal/weaver/strategist.go:84-90` and `119-123`; routed at `evaluator.go:115-119`

```go
metaKey, ok := source.patternMetaKey(patternRef)
if !ok {
	return nil, &planError{kind: errTransient, ...}
}
```

The transient classification is justified as "may simply not have replayed yet",
but there is no deadline, attempt cap, or escalation: a playbook with a typo'd
pattern name (or a pattern vertex that was deleted after install) Naks the same
CDC message forever — a permanent redelivery hot-loop that logs at Warn and
never sets a Health KV issue. A pure config error is thus the *one* failure mode
in the diff that never alerts, inverting the FR29 discipline applied to every
other config error. Compounding it: `indexPattern` lets a second vertex steal an
existing `patternId` (last-writer-wins, no duplicate rejection — unlike
`targetId`), and when the thief is deleted, `removePatternLocked` drops the
mapping entirely, pushing the original, still-installed pattern's targets into
this same eternal-Nak state (`registry.go:346-380`).

### F7. [Minor] A rejected spec *update* silently keeps the previous registration running under the stale playbook

**File/line:** `internal/weaver/registry.go:222-245` (`dispatchTarget` reject paths)

All three reject paths (`unwrap`/`unmarshal`/`validateTarget` failures, and the
duplicate-targetId branch) return before `removeOwnedTargetLocked` runs. If a
vertex that already owns a registered target updates its spec to something
invalid, the old registration — old gaps map, old actions — keeps dispatching as
if nothing happened. "Keep last good" may be defensible, but it is undocumented,
unlogged (the rejection message doesn't say a stale version remains live), and
inconsistent: the operator sees `TargetRejected` and reasonably assumes the
target is *off*.

### F8. [Minor] A routed vertex whose class changes to a non-routed class keeps its stale registration and stale `classes` entry

**File/line:** `internal/weaver/registry.go:167-185` (vertex branch)

An update of a `meta.weaverTarget` vertex to any other class falls into the
`indexOpMeta` branch and returns; `s.classes[id]` still says `weaverTarget` and
the registered target stays live. Only a vertex *delete* unregisters. The class
map can also misroute a subsequent spec update (parsed as a weaverTarget spec for
a vertex that no longer is one).

### F9. [Minor] The `entityKey`-missing Health issue is never cleared once the row is fixed

**File/line:** `internal/weaver/evaluator.go:62-70` (set), `evaluator.go:130-131` (clears — gap columns only)

`issueKeyData(targetID, "entityKey")` is set when a violating row lacks
`entityKey`, but every `issues.clear(issueKeyData(...))` call uses a gap *column*
as the suffix. Once the Lens repairs the row, the error-severity issue rides
every heartbeat until process restart — a permanent false alarm.

### F10. [Minor] Marks of a removed target are orphaned with no clearing path

**File/line:** `internal/weaver/evaluator.go:25-31`

When a target is unregistered, `handleRow` Acks rows (including tombstones) for
it before `clearClosedMarks` runs, and the consumer is then torn down entirely.
Any in-flight `weaver-state` marks for that target persist forever (no TTL in
9.1, and the 9.2 sweep is framed as a lease re-attempt, not removed-target GC).
They also permanently inflate the `marksInFlight` heartbeat metric, which counts
*every* key in the bucket (`state.go:112-118`).

### F11. [Minor] Lying comment: a re-fire of the same episode does NOT "reproduce the identical op"

**File/line:** `internal/weaver/strategist.go:53-56` (plan doc) vs `strategist.go:127-136` (assignTask payload)

The `plan` doc says "payload(markRevision) materializes the op payload so a
re-fire of the same episode reproduces the identical op", but the assignTask
payload computes `expiresAt: time.Now().Add(assignTaskGrantTTL)` at *call* time —
every re-fire produces a different payload (as does `SubmittedAt` in the
envelope). Idempotency rests solely on the requestId collapse; if the tracker
ever validates payload equality for a duplicate requestId, this mismatches. Fix
the op or the comment.

### F12. [Minor] `WEAVER_INSTANCE` is unsanitized; cmd/weaver's fallback default contradicts the engine's documented default

**File/line:** `cmd/weaver/main.go:50-57` vs `internal/weaver/engine.go:56-80`

The engine carefully dot-sanitizes only its *self-generated* default
(`instanceSegmentReplacer`), but a config-supplied instance is used verbatim:
`WEAVER_INSTANCE=node1.prod` silently fragments the Contract #5 key space
(`health.weaver.node1.prod...`) and lands in the registry durable name, where a
dot is at best rejected by JetStream and at worst mis-scoped. Separately,
cmd/weaver pre-fills `weaver-<NanoID>` when the env var is empty, so the
engine's host/pid-attributable default (documented at length in `engine.go`)
is dead code in the shipped binary, and the main.go header comment documents the
cmd default while `Config.Instance` documents the other.

### F13. [Minor] The "final shutdown heartbeat" is a best-effort race, and status never degrades

**File/line:** `internal/weaver/health.go:180-197` (`run`), `health.go:199-244` (`emit`)

`run`'s comment promises "A final 'shutdown' heartbeat is emitted on ctx
cancel", but nothing joins the heartbeater goroutine: `Engine.Start` returns the
moment `ctx.Done()` fires and `cmd/weaver` exits, so the detached 2-second
shutdown emit frequently loses the race with process exit. Also, `emit` reports
`healthy` on every tick regardless of accumulated error-severity issues or
paused consumers — the status field can never say `degraded`, and if
`source.start` fails after the heartbeater launched, a corpse process briefly
advertises `starting`/`healthy`.

### F14. [Minor] `consumerHealthSink.delete` clears the in-memory cache before the KV delete — a failed delete resurrects a stale pause later

**File/line:** `internal/weaver/health_sink.go:91-97`

On a KV error the cache entry is already gone (heartbeat stops reporting the
consumer) but the persisted `paused` entry survives; a future re-add of the same
durable name `Load`s it and silently restores a pause nobody can see the origin
of. Order the KV delete first, or restore the cache entry on failure. Related:
`Load` (line 73-77) swallows a malformed persisted entry without any log — a
corrupted pause-state resets to active with zero trace.

### F15. [Minor] `internal/bootstrap/verify.go` and `scripts/verify-kernel.go` bucket lists disagree — both were edited in this diff

**File/line:** `internal/bootstrap/verify.go:163-167` vs `scripts/verify-kernel.go:263-267`

The script's list includes `LoomStateBucket`; `VerifyKernel`'s list does not.
This change touched the exact same loop in both files to add
`WeaverTargetsBucket` and left the divergence in place, so the two "verify
kernel" surfaces certify different kernels.

### F16. [Nit] Dead field: `opEnvelope.Class` is declared and never populated

**File/line:** `internal/weaver/actuator.go:26`

Hand-copied from the Processor's envelope shape; nothing in `internal/weaver`
writes it. Either set it or drop it — a half-copied wire struct is where the
"keep the module boundary clean" duplication starts drifting.

### F17. [Nit] `directOp` silently clobbers a playbook param named `expectedRevision`; cross-action junk fields pass install validation

**File/line:** `internal/weaver/strategist.go:154-162`; `internal/weaver/registry.go:294-308` (`validateTarget`)

`params["expectedRevision"] = expectedRevision` overwrites a user-supplied param
of that name without complaint. More broadly, `validateTarget` checks only
`targetId` and the `missing_*` key shape — an unknown `action`, a `triggerLoom`
entry carrying `assignee`, or a misspelled field name all install cleanly and
only blow up (or silently no-op) at first dispatch, despite the
"install-time validations" framing.

### F18. [Nit] The fingerprint/Reset machinery is unreachable by its own admission

**File/line:** `internal/weaver/engine.go:141-155`, `248-251`, `276-288`

The comment concedes "the Reset branch is mechanically reachable only if a
future spec field changes" — every fingerprint input (stream, filter, policy,
group) is derived from the target *name* and constants, so `applied == fp` is
always true for a running consumer. ~40 lines of speculative generality guarded
by a comment instead of a test.

### F19. [Nit] Any redelivery re-fires every in-flight gap on the row, not just the one that Nak'd

**File/line:** `internal/weaver/evaluator.go:137-146`

`msg.NumDelivered > 1` is a per-message signal applied per-gap: when gap B's
publish fails and Naks, the redelivery also re-publishes gap A's already-
successfully-published episode (same requestId, so the tracker absorbs it).
Harmless by design but generates redundant ops traffic proportional to gap
count, and the comment at `evaluator.go:92-97` describes the retry as targeted
when it is actually a blanket re-fire.

---

## Summary table

| # | Severity | Where | One-liner |
|---|----------|-------|-----------|
| F1 | Major | registry.go:187-204 | spec-aspect delete wipes class map; re-created spec pends forever, target bricked until restart |
| F2 | Major | registry.go:196-201 | pendingSpecs buffers every non-routed meta spec forever — unbounded leak |
| F3 | Major | engine.go:267-298 | failed supervisor Add/Remove never retried, never alerted; target silently unwatched |
| F4 | Major | registry.go:117-133 | per-boot registry durable has no cleanup path — durable leak on every restart |
| F5 | Major | cmd/weaver/main.go:59-63 | missing bootstrap JSON silently generates fresh IDs → unauthorized actor, ops vanish quietly |
| F6 | Major | strategist.go:84-90 | unresolvable pattern/op ref = transient forever → infinite Nak loop, no Health issue; patternId collisions unhandled |
| F7 | Minor | registry.go:222-245 | rejected spec update silently keeps stale registration dispatching |
| F8 | Minor | registry.go:167-185 | class change of routed vertex leaves stale registration + stale class entry |
| F9 | Minor | evaluator.go:62-70 | entityKey-missing Health issue never cleared after repair |
| F10 | Minor | evaluator.go:25-31 | marks of removed targets orphaned; inflate marksInFlight forever |
| F11 | Minor | strategist.go:53-56 | comment claims identical re-fire op; assignTask expiresAt differs per fire |
| F12 | Minor | cmd/weaver/main.go:50-57 | WEAVER_INSTANCE unsanitized (dots break key space); cmd default defeats engine default |
| F13 | Minor | health.go:180-244 | shutdown heartbeat is an unjoined race; status never degrades from healthy |
| F14 | Minor | health_sink.go:91-97 | delete clears cache before KV; failed delete resurrects invisible stale pause |
| F15 | Minor | verify.go vs verify-kernel.go | the two kernel-verify bucket lists disagree on loom-state |
| F16 | Nit | actuator.go:26 | opEnvelope.Class dead field |
| F17 | Nit | strategist.go:154-162 | directOp clobbers user expectedRevision param; junk playbook fields install cleanly |
| F18 | Nit | engine.go:141-155 | fingerprint/Reset branch unreachable by its own comment |
| F19 | Nit | evaluator.go:137-146 | any redelivery blanket re-fires all in-flight gaps on the row |
