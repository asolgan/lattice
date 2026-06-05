# CONTRACT-AMENDMENT-REQUEST — Contract #10 §10.4 scheduling target-subject scope

**Raised by:** Story 7.4 dev agent (2026-06-05)
**Area:** `docs/contracts/10-orchestration-surfaces.md` §10.4
**Type:** Annotation / clarification (not a shape change — the stream config is correct as specced)

---

## Finding

ADR-51 (NATS `AllowMsgSchedules`) enforces that the `Nats-Schedule-Target` header must name a subject within the stream's own subject space. The NATS server (`nats-server v2.14.0`) validates this at publish time and returns `NewJSMessageSchedulesTargetInvalidError()` if the target is outside the stream's subjects. This was verified by reading `server/stream.go` lines ~6440–6465 (`SubjectsCollide` guard) and by an integration test.

**Impact on Contract #10 §10.4:** The contract text says:

```
target subject:    <component-chosen>
    # e.g. Weaver uses  weaver.timer.fired.<domain>.<kind>.<entityId>
```

The example target `weaver.timer.fired.*` is OUTSIDE `schedule.>`. This example is incorrect — the target subject MUST be within `schedule.>`.

**Correct subject convention:** When the NATS scheduler fires, it stores the payload back into the `core-schedules` stream at the target subject (not dispatched as a plain NATS core message). Components consume fired messages via JetStream consumers filtered on their target subject prefix.

The correct target pattern for Weaver would be:
```
schedule.weaver.timer.fired.<entityId>
```

The component then creates a JetStream consumer on `core-schedules` with `FilterSubject: "schedule.weaver.timer.fired.>"` (or equivalent) to receive its fired messages.

---

## Suggested contract amendment

In `docs/contracts/10-orchestration-surfaces.md` §10.4, change the target subject example and add a clarifying note:

```
target subject:    schedule.<component>.fired.<entityId>   # MUST be within schedule.>
                   # The NATS scheduler stores the fired message back into core-schedules at
                   # this subject. The publisher creates a JetStream consumer on core-schedules
                   # filtered on its target subject prefix to receive fired messages.
                   # e.g. Weaver uses schedule.weaver.timer.fired.<entityId>
```

---

## Impact assessment

- **Story 7.4 scope:** The `core-schedules` stream config is correct as specced (`AllowMsgSchedules: true`, subjects `schedule.>`). No stream config change needed.
- **Story 9.3 (Weaver temporal lane):** Story 9.3 must use `schedule.weaver.timer.fired.<entityId>` (within `schedule.>`) as the target, and consume via a JetStream consumer, not a plain NATS subscription.
- **No contract shape change required for this story** — just a contract annotation.

---

## Verification

Smoke test `TestCoreSchedulesSmoke` in `internal/bootstrap/scheduling_smoke_test.go` confirms the correct pattern: target is `schedule.test.timer.fired.<entityId>` (within `schedule.>`), consumer is a JetStream filtered consumer on `core-schedules`.
