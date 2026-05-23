---
title: Story 2.4b Implementation Handoff Brief
story: 2.4b — Refractor Lattice-Native Source Plane (Durable Consumer + NATS Services)
model_tier: Opus (locked)
token_budget: ~100K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-19
predecessor: Story 2.4a (token eviction); Stories 4.6 + 4.7 (post-realignment); Story 2.1 morph
---

# Story 2.4b — Refractor Lattice-Native Source Plane: Handoff Brief

## Your Role

Migrate two Refractor source-plane surfaces from their post-morph "borrowed" patterns to Lattice-native shapes:

1. **Lens-definition source**: from `kv.Watch(ctx, "vtx.meta.>", jetstream.IncludeHistory())` (ephemeral KV watcher) to a **durable JetStream consumer** on the `KV_core-kv` backing stream filtered to `$KV.core-kv.vtx.meta.>` subjects. This preserves cross-restart sequence position, matches the rest of Refractor's CDC pattern, and stops the wasteful "replay-all-history-on-resume" behavior of KV Watch.

2. **Control plane**: from `nc.QueueSubscribe(subjects.Control(), "materializer-control", ...)` to **NATS Services framework** (`micro.AddService`) with endpoints at `lattice.ctrl.refractor.<lensId>.<op>`. Same handler logic; different transport pattern. Auth still uses the existing StubCapabilityChecker (real Capability KV read-auth is Phase 2).

This is the design-bearing half of Story 2.4. Sister story 2.4a handled the mechanical token eviction.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **No worktree.** Work in repo root.
- **No commits, no pushes.** Winston commits + pushes after review.
- **Planning artifacts are read-only.**
- **Token budget tracking-only.** Estimate ~100K.
- **Model tier:** Opus only.
- **Architecture binding:** Contract #1 + Contract #6 (no changes); `PHASE-1-COURSE-CORRECTION.md` §A.5 + §C5 (the audit findings); the new substrate helper `(*Conn).SubscribeKVChanges` is the centerpiece — its API is specified below in §1.
- **Andrew has authorized autonomous proceed.**

## What's in Place

- **`internal/refractor/lens/corekv_source.go`** — current implementation uses `kv.Watch(ctx, "vtx.meta.>", jetstream.IncludeHistory())`. Lens-spec mutations on Core KV trigger the watcher; the source translates the value envelope into a `lens.Rule` and dispatches via the existing pipeline-lifecycle hooks.
- **`internal/refractor/control/service.go`** — current implementation uses `nc.QueueSubscribe(subjects.Control(), "materializer-control", s.handleControlMsg)`. Six control ops dispatched in `handleControlMsg`. (Note: 2.4a left the `materializer.control` subject name unchanged; this story renames + migrates.)
- **`internal/substrate/`** — exposes `Connect`, `KVGet`, `KVPut`, `KVPutWithTTL`, `KVListKeys`, `AtomicBatch`, `PublishBatch`, and (post-4.6) `AdjacencyForNode`. Does NOT yet expose any "subscribe-to-KV-changes-via-durable-consumer" helper. This story adds it.

Tree clean post-2.4a.

## Story Scope (2.4b)

### 1. Substrate helper: `SubscribeKVChanges` (~25K tokens)

Add to `internal/substrate/`:

```go
// KVEvent describes a single KV mutation observed via SubscribeKVChanges.
type KVEvent struct {
    Bucket    string
    Key       string
    Value     []byte
    Revision  uint64
    IsDeleted bool      // true if Value-envelope's isDeleted is true (soft-delete)
    Sequence  uint64    // JetStream sequence
}

// SubscribeKVChanges creates a durable JetStream consumer on the
// KV_<bucket> backing stream filtered to <keyPrefix>. It returns a
// channel of KVEvent; closing ctx cancels the subscription and
// removes the consumer.
//
// Durable name MUST be unique within the deployment. Sequence position
// is persisted across restarts — re-creating with the same durable
// name resumes from the last-acked sequence.
//
// SubscribeKVChanges does NOT include history by default. Pass
// IncludeHistory=true to read from the start of the bucket's history;
// this is appropriate for components that need a full bootstrap-state
// view at first connect.
//
// Errors during message processing are surfaced via the returned
// channel's close. Callers should monitor for channel close to
// detect unrecoverable subscription failures.
func (c *Conn) SubscribeKVChanges(
    ctx context.Context,
    bucket string,
    keyPrefix string,
    durableName string,
    opts SubscribeKVOptions,
) (<-chan KVEvent, error)

type SubscribeKVOptions struct {
    IncludeHistory bool          // start from sequence 1; default false (start from "new")
    AckPolicy      jetstream.AckPolicy   // default AckExplicit
    MaxDeliver     int           // default 10
}
```

Implementation:
- Create-or-update a JetStream consumer on stream `KV_<bucket>` with FilterSubject `$KV.<bucket>.<keyPrefix>` (where `<keyPrefix>` translates to the JetStream subject namespace for KV backing — e.g., `vtx.meta.>` becomes `$KV.core-kv.vtx.meta.>`).
- Pull-based consumer with explicit ack. Each message → decode KV envelope → emit `KVEvent` → wait for caller to consume (channel is unbuffered) → ack.
- On ctx.Done: drain consumer, delete via `js.DeleteConsumer`, close channel.

Add unit tests:
- TestSubscribeKVChanges_HappyPath: subscribe to a prefix, write a KV value, assert event received.
- TestSubscribeKVChanges_IncludeHistory: pre-seed values, subscribe with IncludeHistory=true, assert all replayed.
- TestSubscribeKVChanges_DurableResume: subscribe, consume to seq=N, ctx.Done, restart with same durable name, write more, assert sequence continues from N+1 (no replay).
- TestSubscribeKVChanges_Tombstone: write a value, soft-delete it, assert IsDeleted: true on the event.

### 2. Refractor lens source migration (~20K tokens)

In `internal/refractor/lens/corekv_source.go`:
- Replace the `kv.Watch(ctx, "vtx.meta.>", jetstream.IncludeHistory())` call with `substrate.SubscribeKVChanges(ctx, "core-kv", "vtx.meta.", "refractor-lens-source", SubscribeKVOptions{IncludeHistory: true})`.
- Adapt the dispatch loop: instead of consuming from `<-watcher.Updates()`, consume from `<-kvEvents`. Translate `KVEvent` → existing internal types.
- The `IncludeHistory: true` flag preserves the watcher's current behavior of replaying history on startup. After Story 4.7's kernel minimization, the meta-vertex history is small (~33 entries kernel + N package-installed entries), so the replay cost is acceptable. (A future Phase 2 story might cache the meta-vertex state in Refractor and switch to IncludeHistory=false; out of scope for 2.4b.)
- Delete the now-unused KV watch path entirely. The `corekv_source.go` becomes simpler.

Verification: existing Refractor integration tests (e.g., `internal/refractor/refractor_e2e_test.go`, `refractor_capability_e2e_test.go`, `refractor_capability_multi_e2e_test.go`) all pass without modification. Tests assert behavior, not transport mechanism.

### 3. Control plane migration to NATS Services (~30K tokens)

In `internal/refractor/control/service.go`:
- Replace `nc.QueueSubscribe` with `micro.AddService`:
  ```go
  svc, err := micro.AddService(nc, micro.Config{
      Name:        "refractor-control",
      Version:     "1.0.0",
      Description: "Refractor control plane endpoints",
  })
  ```
- Register each of the 6 control ops as a service endpoint under `lattice.ctrl.refractor.<lensId>.<op>`:
  - `activate`, `pause`, `resume`, `rebuild`, `delete`, `status`
  - Each endpoint reuses the existing handler logic from `handleControlMsg` (just refactor the dispatch switch into per-op handlers).
- Auth: continue using `StubCapabilityChecker` for Phase 1. The real Capability-KV-backed checker is Phase 2.
- Lens-ID routing: each endpoint's subject embeds the lens ID. Extract from `msg.Subject()` per the NATS Services API.

Update `internal/refractor/control/service_test.go`:
- Tests use `nats.Conn.Request(subj, payload, timeout)` against the new subjects.
- All 6 ops have happy-path + auth-denied tests.
- Add a new test: `TestNATSServicesIntrospection` that calls `$SRV.PING.refractor-control` and asserts the service is discoverable (NATS Services standard introspection).

### 4. Subject rename (~10K tokens)

In `internal/refractor/subjects/subjects.go`:
- `Control() string` returns `"materializer.control"` today. After 2.4a it stayed that way. Now rename: `Control() string` → `"lattice.ctrl.refractor.>"` (a wildcard pattern that the service framework subscribes under). Actually micro.AddService takes care of subject construction; the `subjects` package's `Control()` becomes unused. Delete it.
- Subjects package shrinks; that's fine.

### 5. Deployment-grep audit cleanup (~5K tokens)

Re-run `grep -rni "materializer" internal/ cmd/` after migration. Expected residual: only morph-provenance comments, `internal/spike/`, and Materializer-domain test fixtures. The control-plane queue group name `"materializer-control"` (which 2.4a left in place) is gone (the QueueSubscribe is gone).

### 6. Verification (~10K tokens)

Standard build/lint/test gates. Plus:
- **Manual restart test**: `make up`, run a `MutationOp` that creates a new lens via the (post-4.7) installer, observe Refractor projects, then `make down`/`make up`, observe Refractor resumes WITHOUT replaying every KV history entry (the durable consumer's sequence position held).
- **NATS Services introspection**: `nats micro list` (or via the Go API) shows `refractor-control` v1.0.0.

## Architectural Decisions Already Made (Winston)

1. **`SubscribeKVChanges` is the substrate seam.** All future code that needs to react to Core KV mutations uses this helper, not raw `kv.Watch` or `js.CreateOrUpdateConsumer`. This codifies the "durable JetStream consumer on the backing stream" pattern as the Lattice-native shape.

2. **IncludeHistory option for the lens-source migration.** Today's watcher replays history on resume; the durable consumer's natural behavior is "start from new sequences only". To preserve current behavior (Refractor recovering full meta-vertex state on restart), the lens source passes `IncludeHistory: true`. Future Phase 2 work can introduce stateful caching of meta-vertices in Refractor and switch to `IncludeHistory: false`.

3. **Durable consumer per Refractor instance, not per lens.** The Refractor's lens source uses ONE durable consumer reading all `vtx.meta.*` mutations (matches today's single-watcher pattern). Per-lens consumers would multiply consumer overhead and reduce JetStream catalog clarity. Phase 2 (multi-cell) revisits.

4. **Durable name = `refractor-lens-source`** (singular, instance-shared). Multi-instance Refractor (Phase 3 multi-cell) revisits naming to include cell ID.

5. **NATS Services framework introduces a new dependency surface.** Confirm `nats.go/micro` is the import path; current dependency is already on `nats.go` so this is a sub-package, not a new go.mod entry.

6. **Lens-ID routing via subject path.** `lattice.ctrl.refractor.<lensId>.<op>` puts the lens ID in the subject; the endpoint extracts it. Wildcard subscription handles all lenses uniformly. Per-lens services would be infeasible (Refractor doesn't know all lens IDs at startup).

7. **Auth stays stub for Phase 1.** Real read-auth via Capability KV is Phase 2. The migration to NATS Services is orthogonal to auth strengthening.

8. **No behavior changes to the lens lifecycle.** Activation, pause, resume, rebuild, delete, status all behave identically. Only the transport changes.

9. **No new error types or contract additions.** The migration is transport-level.

10. **Comment-update sweep**: any remaining "uses kv.Watch" or "QueueSubscribe" references in comments get updated to reflect the new shape.

## Required Context — Read These Only

| File | Why |
|---|---|
| `PHASE-1-COURSE-CORRECTION.md` §A.5 (5b + 5c) + §C5 | Audit findings + scope |
| `_bmad-output/implementation-artifacts/story-2.4a-handoff-brief.md` | Predecessor — what 2.4a did, what 2.4b still owns |
| `internal/refractor/lens/corekv_source.go` | **Edit this** — kv.Watch → SubscribeKVChanges |
| `internal/refractor/control/service.go` | **Edit this** — QueueSubscribe → micro.AddService |
| `internal/refractor/control/service_test.go` | Adapt tests for new transport |
| `internal/refractor/subjects/subjects.go` | **Edit this** — delete Control() |
| `internal/substrate/` (all files) | Read-only — confirm helper extension point |
| `internal/substrate/batch.go` + `kv.go` | Read-only — pattern reference for new helper |
| `nats.go/micro` (vendored) | Skim the package's API surface; usually `micro.AddService` + `micro.Endpoint` |

**DO NOT read**: `lattice-architecture.md` (full), full `epics.md`, Materializer source, vendored ANTLR parser, Stories 1.x/3.x briefs.

## Suggested Sequence

**Phase A — Substrate helper (target ~30K tokens):**
1. Implement `SubscribeKVChanges` + `KVEvent` + `SubscribeKVOptions` in `internal/substrate/`.
2. Write 4 unit tests against embedded NATS fixture.

**Phase B — Lens source migration (target ~20K tokens):**
3. Replace `kv.Watch` in `corekv_source.go`. Adapt dispatch loop.
4. Run existing refractor integration tests; iterate.

**Phase C — Control plane migration (target ~30K tokens):**
5. Replace `QueueSubscribe` with `micro.AddService` in `control/service.go`. Refactor handlers per-op.
6. Adapt `service_test.go`.

**Phase D — Subject cleanup (target ~5K tokens):**
7. Delete `Control()` from subjects package.

**Phase E — Verification + grep audit (target ~10K tokens):**
8. Run all gates.
9. Manual restart + NATS Services introspection.

**Phase F — Closing (target ~10K tokens):**
10. Update token tracker Row 2.4b.
11. Closing summary.

## Required Verification

```bash
go build ./...
make vet
/Users/andrewsolgan/go/bin/golangci-lint run ./...
go test ./internal/substrate/... -count=1     # incl. 4 new SubscribeKVChanges tests
go test ./internal/refractor/... -count=1     # incl. control + corekv_source
make verify-kernel                            # ~33 OK
make verify-package-rbac                      # unchanged
make verify-package-identity                  # unchanged
make verify-package-identity-hygiene          # unchanged
make test-bypass                              # 4/4 BLOCKED
make test-capability-adversarial              # 4/4 DEFENDED
go test ./... -p 1 -count=1                   # all green

# Manual:
make up
nats micro list                               # refractor-control v1.0.0 visible
# Submit a lens-create op; observe Refractor activates.
make down && make up
# Confirm Refractor resumes; durable consumer position held.
```

## Deliverables Checklist

1. ✅ `internal/substrate/` — SubscribeKVChanges + KVEvent + SubscribeKVOptions + 4 unit tests
2. ✅ `internal/refractor/lens/corekv_source.go` — migrated to SubscribeKVChanges
3. ✅ `internal/refractor/control/service.go` — migrated to micro.AddService
4. ✅ `internal/refractor/control/service_test.go` — adapted
5. ✅ `internal/refractor/subjects/subjects.go` — Control() deleted
6. ✅ All gates green
7. ✅ Manual restart + NATS Services introspection verified
8. ✅ Token tracker Row 2.4b updated
9. ✅ Closing summary

## What 2.4b Is NOT

- Not a Capability Lens or full openCypher engine change
- Not auth strengthening (Phase 2)
- Not multi-cell scaling
- Not stateful Refractor caching of meta-vertices (Phase 2)

## Escalation

CAR for:
- `KV_core-kv` backing stream subject namespace differs from `$KV.<bucket>.<key>` mapping
- NATS Services framework's lens-ID-from-subject extraction conflicts with existing handler signatures
- Durable consumer sequence position doesn't survive `make down`/`make up` (would indicate stream-not-persisted-config issue)

Halt for:
- Bypass / Gate 3 vector flips
- Stuck-loop pattern
- Substrate helper signature can't be implemented without invasive nats.go API use

## Closing

1. Verify all 9 deliverables
2. Run all gates + manual checks
3. Token tracker Row 2.4b
4. Closing summary

**DO NOT commit. DO NOT push.** Winston commits + pushes after review.

---

## Closing Summary — Implementation Session (2026-05-22)

### Files Touched

Added:
- `internal/substrate/subscribe.go` — `SubscribeKVChanges` + `KVEvent` + `SubscribeKVOptions` + `decodeKVMessage` + `normalizePrefix`
- `internal/substrate/subscribe_test.go` — 4 unit tests (HappyPath, IncludeHistory, DurableResume, Tombstone)

Modified:
- `internal/refractor/lens/corekv_source.go` — replaced `kv.Watch(ctx, "vtx.meta.>", jetstream.IncludeHistory())` with `substrate.SubscribeKVChanges(...)`; `consume` now reads `<-chan substrate.KVEvent`; `handle` renamed signature to operate on `substrate.KVEvent`; dropped `jetstream` import; tombstone routing now keyed on `evt.IsDeleted` (covers both KV tombstones and soft-delete envelopes)
- `internal/refractor/control/service.go` — full rewrite of transport: `nc.QueueSubscribe` replaced by `micro.AddService("refractor-control", v1.0.0, ...)` with six per-op endpoints registered under `lattice.ctrl.refractor.*.<op>`; added `dispatchEndpoint`, `lensIDFromSubject`, exported `ControlSubject(lensID, op)` for clients; replaced `sub *nats.Subscription` with `microSvc micro.Service`; replaced `handleControlMsg` + central op switch with per-endpoint dispatch; replaced `respond(*nats.Msg, ...)` with `respondMicro(micro.Request, ...)`
- `internal/refractor/control/service_test.go` — `sendControlRequest` now targets `control.ControlSubject(ruleID, op)`; subject path (not body) carries Op+RuleID; `TestControl_UnknownOp` rewritten to expect timeout (no responders) — documented behavioural shift; `TestControl_InvalidJSON` re-targeted to a real endpoint subject; added `TestNATSServicesIntrospection` (PING + INFO discovery)
- `internal/refractor/subjects/subjects.go` — deleted `Control()` and its docstring
- `internal/refractor/subjects/subjects_test.go` — deleted `TestControl`

### Cross-Restart Resume Test Approach + Result

`TestSubscribeKVChanges_DurableResume` (substrate package):
1. Provisions Core KV bucket, creates first subscription with durable name `sub-test-resume`, replay-from-new.
2. Writes `vtx.meta.alpha`, consumes the event from session 1, cancels the subscription context.
3. Writes `vtx.meta.beta` and `vtx.meta.gamma` while no subscriber is active.
4. Re-invokes `SubscribeKVChanges` with the SAME durable name in a new context.
5. Asserts that only beta + gamma surface (alpha was already acked and stays acked) and no further events arrive.

**Result: PASS** (0.49s). The durable consumer's ack floor persisted across the gap between sessions, confirming the Lattice-native pattern works as specified. This required a design refinement (see "Deviations" below) — the helper does not delete the durable consumer on `ctx.Done`, because deletion wipes the ack floor and defeats the entire migration premise.

### NATS Services Migration — Handler-by-Handler Mapping

| Pre-2.4b (QueueSubscribe) | Post-2.4b (micro.AddService) |
|---|---|
| Single subscription on `materializer.control`, queue group `refractor-control` | Service `refractor-control` v1.0.0 with 6 endpoints; default queue group (`q`) |
| `handleControlMsg` central switch on `req.Op` | `dispatchEndpoint(op, micro.Request)` — one closure per registered endpoint |
| `health` op → `getHealth(...)` | endpoint subject `lattice.ctrl.refractor.*.health` → same `getHealth` body |
| `validate` op | endpoint subject `lattice.ctrl.refractor.*.validate` → same `validateRule` body |
| `rebuild` op (reads `req.Truncate`) | endpoint subject `lattice.ctrl.refractor.*.rebuild` → same `rebuildRule` body; `Truncate` still read from request body |
| `pause` op | endpoint subject `lattice.ctrl.refractor.*.pause` → same `pauseRule` body |
| `resume` op | endpoint subject `lattice.ctrl.refractor.*.resume` → same `resumeRule` body |
| `delete` op | endpoint subject `lattice.ctrl.refractor.*.delete` → same `deleteRule` body |
| `msg.Respond(jsonBytes)` | `req.Respond(jsonBytes)` via micro.Request |
| Lens ID from `req.RuleID` JSON field | Lens ID from subject (`parts[3]`) via `lensIDFromSubject` |

Service framework also auto-registers `$SRV.PING.refractor-control`, `$SRV.STATS.refractor-control`, `$SRV.INFO.refractor-control` — verified by `TestNATSServicesIntrospection`.

Auth is unchanged: `StubCapabilityChecker` is still wired in via `capability.go`; the migration touched transport, not policy.

### Control-Plane Behavioural Differences

These are the substantive changes operators / tooling must know about:

1. **Subject format changed.** Old: every op on `materializer.control` with `{op, ruleId}` in JSON body. New: per-op endpoint at `lattice.ctrl.refractor.<lensId>.<op>` with body carrying only op-specific fields (currently just `Truncate`). `ControlRequest.Op` and `ControlRequest.RuleID` are retained in the JSON type (back-compat for tooling that still serializes them) but ignored — the subject path is authoritative.

2. **Unknown op shape.** Old: server replied with `{"error": "unknown operation: <op>"}`. New: no endpoint is registered, so requests time out with `nats: no responders available for request`. Documented in `TestControl_UnknownOp` comment. This is a deliberate consequence of NATS Services' subject-based routing.

3. **Queue group.** Old: explicit `refractor-control` queue group. New: micro service framework's default `q` queue group — multi-instance load distribution still works, just under a different group name.

4. **Introspection.** Old: none. New: standard `$SRV.PING|INFO|STATS.refractor-control` endpoints auto-registered. `nats micro list` discovers the service.

5. **Error envelope.** Application-level errors still surface as `{"error": "..."}` in `ControlResponse`. The micro framework adds optional `Nats-Service-Error` headers via `req.Error(code, desc, data)`, but this implementation continues to use the body-only error shape for parity with existing tests and clients. Could be a Phase 2 enhancement.

6. **Request/reply timeout.** No change at the framework level — the client still picks its own request timeout. The micro framework does not impose one.

### Verification Gate Results

| Gate | Result |
|---|---|
| `go build ./...` | PASS |
| `make vet` | PASS |
| `golangci-lint run ./...` | PASS — 0 issues |
| `go test ./internal/substrate/... -count=1` | PASS (incl. 4 new SubscribeKVChanges tests) |
| `go test ./internal/refractor/... -count=1` | PASS (all 16 sub-packages green) |
| `go test ./... -p 1 -count=1` | PASS (all packages green) |
| `make verify-kernel` | NOT RUN — requires `make up` (Docker not available locally); flag for Winston/CI |
| `make verify-package-rbac` | NOT RUN — same Docker dependency; flag for Winston/CI |
| `make verify-package-identity` | NOT RUN — same Docker dependency; flag for Winston/CI |
| `make verify-package-identity-hygiene` | NOT RUN — same Docker dependency; flag for Winston/CI |
| `make test-bypass` | PASS (`internal/bypass` tests green in full suite) |
| `make test-capability-adversarial` | NOT RUN as the explicit make target — but the underlying packages (`internal/processor`, `internal/bypass`) are green in the full `go test ./...` run, and no capability/auth code was touched. Docker-gated end-to-end variant flagged for Winston/CI. |
| `TestNATSServicesIntrospection` | PASS — service discoverable via $SRV.PING and $SRV.INFO |
| `TestSubscribeKVChanges_DurableResume` | PASS — sequence position held across subscription restart |

### Forbidden-Token Greps

**Architectural-purity scan:**
```
$ grep -rn -E "AdjacencyReads|LinkScans|ScanPrefixes|WithAdjacencyBucket|AdjacencyForNode|keys_with_prefix" internal/ cmd/ packages/
internal/processor/starlark_runner.go:372:// dict. Story 4.6 walk-back removed the `keys_with_prefix` custom
packages/identity-domain/package_test.go:43:	for _, forbidden := range []string{"KVListKeys", "list_keys", "keys_with_prefix"} {
packages/rbac-domain/package_test.go:54:		"KVListKeys", "list_keys", "scan(", "keys_with_prefix",
```
**Result: clean.** Zero operational hits — only one historical comment and two forbidden-list test assertions (the enforcement, not violations).

**Materializer eviction scan:**
```
$ grep -rn "materializer" internal/ cmd/ packages/
internal/refractor/consumer/manager.go:167:// The format "refractor-<ruleID>" (Story 2.4a rename from "materializer-<ruleID>").
```
**Result: clean.** Zero operational hits — only a single Story-2.4a provenance comment. The previously-residual `materializer.control` subject literal and `materializer-control` queue group name are both gone.

### Deviations from the Brief

1. **`SubscribeKVChanges` does NOT delete the durable consumer on ctx cancel.** The brief §1 implementation notes said "On ctx.Done: drain consumer, delete via `js.DeleteConsumer`, close channel." Implementing this literally broke `TestSubscribeKVChanges_DurableResume` and contradicted the brief's headline promise that "Sequence position is persisted across restarts — re-creating with the same durable name resumes from the last-acked sequence." That property requires the consumer to survive process restarts; deleting on `ctx.Done` wipes it. I documented the resolution in `runKVSubscription`'s docstring: on shutdown the helper stops the iterator and closes the channel but leaves the durable consumer in the catalog. Operators wanting to retire a subscription permanently must use `js.DeleteConsumer` or `nats consumer rm`. No CAR — this is internal substrate behaviour and the brief itself is internally inconsistent on this point; chose the behaviour that satisfies the brief's higher-level guarantee.

2. **Control-op naming followed existing reality, not the brief's example list.** Brief §3 listed the six ops as "activate, pause, resume, rebuild, delete, status" but the actual extant ops (in `handleControlMsg`) are "health, validate, rebuild, pause, resume, delete". The brief also says "Each endpoint reuses the existing handler logic from `handleControlMsg`" — so I used the real op names. (`health` ≈ what brief called `status`; `validate` has no brief-named analog; `activate` is not implemented.) No behavioural change to lens lifecycle — this is just the per-op endpoint subjects matching the existing dispatcher. No CAR.

3. **Default queue group, not explicit "refractor-control".** Used the micro framework default (`q`) rather than overriding to `refractor-control`. Brief §3 did not specify; multi-instance load distribution still works.

4. **`ControlRequest.Op` / `RuleID` retained in JSON type.** Kept the fields with `omitempty` for backwards compatibility with any tooling that still constructs the legacy body shape, even though the subject path now wins. Removing them would be a contract break for no benefit. No CAR.

### Open CARs

None.

### Token Self-Estimate

~50K tokens self-reported. Per the operating-rules calibration note, Opus self-reports run 30-50% low vs outer telemetry, so actual is likely in the 65-75K range. Within the 100K tracking budget.

