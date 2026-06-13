# Platform message scheduling — core-schedules stream

**Component reference** | Audience: component authors + architects | Status: **Phase 2 — shipped** | Contract authority: `docs/contracts/10-orchestration-surfaces.md` §10.4 (FROZEN 2026-06-02)

---

## What is provisioned

`internal/bootstrap/primordial.go → provisionStreams()` creates a single JetStream stream at bootstrap:

| Stream | Subjects | Key flags |
|--------|----------|-----------|
| `core-schedules` | `schedule.>` | `AllowMsgSchedules: true`, file storage, limits retention, `MaxMsgsPerSubject: 1` |

This is a **platform capability** (like Health KV) — not owned by any single engine. Any component may publish a scheduled message; the NATS scheduler fires it at the configured time.

---

## Subject convention

```
schedule.<domain>.<kind>.<entityId>
```

| Token | Meaning |
|-------|---------|
| `<domain>` | Owning component domain — e.g. `weaver`, `loom`, `orchestration` |
| `<kind>` | Entity type — e.g. `timer`, `task`, `lease` |
| `<entityId>` | **NanoID only** (20-char, substrate alphabet). Must NOT be a dotted vertex key (`vtx.op.abc`) because dots are NATS subject-token separators. The full entity key rides the message payload. |

Example schedule subject (generic per-entity template): `schedule.weaver.timer.sL9k2mN3pQrT7vWx8yZ0`. This is the minimal one-token-per-entity shape; Weaver itself keys per *target* and entity (see the next paragraph), and the publish example below shows the generic form.

A publisher may key its schedules with additional dot-free tokens between `<kind>` and `<entityId>` when one timer slot per entity is too coarse — replace semantics are per *subject*, so the keying chooses the replacement granularity. Weaver keys per target AND entity (`schedule.weaver.timer.<targetId>.<entityId>`), so two targets watching the same entity hold independent timers.

---

## How to publish a scheduled message

Set two headers on your `nats.Msg` before publishing to `schedule.<domain>.<kind>.<entityId>`:

| Header constant | Wire value | Meaning |
|-----------------|-----------|---------|
| `server.JSSchedulePattern` | `"Nats-Schedule"` | Schedule spec — Phase 2 supports `@at <RFC3339>` (one-shot absolute time). Example: `@at 2026-06-06T14:00:00Z` |
| `server.JSScheduleTarget` | `"Nats-Schedule-Target"` | The subject the NATS scheduler republishes the payload to when the schedule fires — **must lie within `schedule.>`** (the server rejects an out-of-stream target at publish time) |

Use the constants from `github.com/nats-io/nats-server/v2/server` — do not hardcode the raw strings. In-repo components use `substrate.ScheduleHeader` / `substrate.ScheduleTargetHeader` (`internal/substrate/publish.go`), which are test-pinned to the server constants, and publish through `Conn.Publish` (a JetStream publish) rather than holding a raw NATS handle.

```go
msg := nats.NewMsg("schedule.weaver.timer." + entityID)
msg.Header.Set(server.JSSchedulePattern, "@at "+fireAt.UTC().Format(time.RFC3339))
msg.Header.Set(server.JSScheduleTarget, "schedule.weaver.timer.fired."+entityID)
msg.Data = []byte(`{"entityKey":"vtx.op.` + entityID + `"}`)
_, err = js.PublishMsg(ctx, msg) // JetStream publish — the schedule is stored in the stream
```

The payload is preserved and delivered verbatim at the target subject when the schedule fires.

---

## Replace semantics — one schedule per subject

Re-publishing to the same `schedule.<domain>.<kind>.<entityId>` subject **replaces** the prior schedule for that entity. The NATS scheduler enforces this via a per-subject rollup (automatically enabled by `AllowMsgSchedules: true` on the stream). The `MaxMsgsPerSubject: 1` config provides an additional storage bound so the stream cannot accumulate unbounded pending-schedule entries.

To cancel a pending schedule, publish to the same subject with `Nats-Schedule-Next: purge` and a `Nats-Scheduler` header identifying your scheduler ID.

---

## Choosing the republish target subject

The target subject is **publisher-chosen but MUST lie within `schedule.>`**: the NATS scheduler fires by storing the payload **back into the `core-schedules` stream** at the target subject and validates that target against the stream's own subjects, rejecting an out-of-stream target at publish time (`JSMessageSchedulesTargetInvalidError`). Each component namespaces its own fired subjects and consumes them via a **JetStream consumer filtered on its target-subject prefix** (a plain NATS subscribe never sees them):

- Weaver schedules at `schedule.weaver.timer.<targetId>.<entityId>` with target `schedule.weaver.timer.fired.<targetId>.<entityId>`, and consumes via the durable `weaver-temporal` filtered on `schedule.weaver.timer.fired.>`.
- No cross-component fan-out occurs — each component's filtered consumer receives only its own fired messages.

The fired message is then processed by the consuming component (e.g. Weaver converts it to a normal op via the Processor). The transactional outbox (Contract #3) remains the sole event producer — the `core-schedules` stream does not route to `core-events`.

---

## Phase 2 scope

Phase 2 supports `@at <RFC3339>` (one-shot absolute schedules) only. `@every` recurring schedules are deferred.

---

## Readiness gate

`core-schedules` is a JetStream stream, not a KV bucket. `CreateOrUpdateStream` is synchronous — the stream exists before `ProvisionBuckets` returns. `SeedPrimordial` and `MarkBootstrapComplete` run after `ProvisionBuckets`, so `core-schedules` is always present before the bootstrap-complete marker is written. No readiness-gate polling is required (unlike KV projections, which require Refractor to be running).

---

## References

- `docs/contracts/10-orchestration-surfaces.md` §10.4 — FROZEN shape authority (stream name, subject pattern, `AllowMsgSchedules`, `@at` header, target-subject mechanism, replace semantics, `<entityId>` = NanoID).
- `internal/bootstrap/primordial.go` — `provisionStreams()` for the exact `StreamConfig`.
- `internal/bootstrap/scheduling_smoke_test.go` — smoke test confirming stream flag, `@at` firing, payload round-trip, and replace semantics.
