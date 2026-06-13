# Contract #10 Amendment Request (Story 9.2 — Weaver mark TTL/lease)

This amendment to the **FROZEN** Contract #10 (`docs/contracts/10-orchestration-surfaces.md`)
was adjudicated during Story 9.2 (Weaver §10.3 mark lease + reconciler sweep), 2026-06-12. Per
CLAUDE.md "Frozen contracts," it is not an in-flight edit — it is raised here for ratification by
the contract owner (Andrew) + a Contract #10 revision-history entry. The implementation
(`internal/weaver/state.go`, `internal/weaver/reconciler.go`) already builds to the requested
text.

**STATUS: RATIFIED 2026-06-12 (Andrew).** Applied to Contract #10 (§10.2/§10.3/§10.4) + revision-history entry; the working tree already built to this text.

## Request 1: §10.3 `weaver-state` — the mark's per-key TTL is 2× the lease, not a literal mirror of `leaseExpiresAt`

**Location:** §10.3 "`weaver-state` — in-flight convergence marks" (the mark value shape and the
"`leaseExpiresAt` mirrors the TTL for visibility" clause).

**Current text:** the mark carries a NATS per-key TTL and "`leaseExpiresAt` mirrors the TTL for
visibility" — read literally, TTL == lease, i.e. the key self-deletes at the moment the lease
expires.

**Requested text:** the per-key TTL is **2 × lease** (`markTTLBackstopFactor`,
`internal/weaver/state.go` — a constant, not a config knob); `leaseExpiresAt` mirrors the
**lease** (`claimedAt + lease`), and the TTL is the lease's **dead-reconciler backstop**,
strictly longer than the lease. `Config.SweepInterval` is clamped to ≤ `MarkLease` so at least
one sweep pass lands inside the lease-to-TTL window.

**Rationale:** the same §10.3 paragraph requires the active reconciler sweep to **reclaim
expired leases**. Nothing watches the weaver-state backing stream (the sweep is interval-cadence
by design), so a raw TTL deletion unwedges the gap but can never re-attempt it — the mark is the
sweep's only evidence (it enumerates marks, not rows). The sweep can therefore reclaim only
while the key still exists **past** `leaseExpiresAt`: with TTL == lease the key self-deletes at
the exact moment it becomes reclaimable, the sweep-reclaims-expired-leases clause is mechanically
unreachable, and every crash recovery degrades to the unwedge-without-re-attempt TTL path. With
TTL = 2 × lease the sweep gets a full lease-width observation window and the TTL still bounds
the mark's life when no reconciler ever runs. The two clauses of §10.3 are only satisfiable
together this way.

---

# Contract #10 Amendment Requests (Story 9.3 — Weaver temporal lane)

These annotation-class amendments to the **FROZEN** Contract #10
(`docs/contracts/10-orchestration-surfaces.md`) were adjudicated during Story 9.3 (Weaver §10.4
temporal lane), 2026-06-12. Per CLAUDE.md "Frozen contracts," they are raised here for ratification
by the contract owner (Andrew) + a Contract #10 revision-history entry, not edited in-flight. The
implementation (`internal/weaver/temporal.go`, `internal/weaver/actuator.go`) already builds to the
requested reading.

**STATUS: RATIFIED 2026-06-12 (Andrew).** Applied to Contract #10 (§10.2/§10.3/§10.4) + revision-history entry; the working tree already built to this text.

## Request 2: §10.4 schedule-subject template widened by a publisher-chosen `<targetId>` token

**Location:** §10.4 "schedule-message subject shape" — the template line
`schedule.<domain>.<kind>.<entityId>` (and the matching fired/republish-target line).

**Current text (read as normative):** the schedule subject is
`schedule.<domain>.<kind>.<entityId>` — a single entity token after the domain/kind segments.

**Requested text:** the segment(s) after `schedule.<domain>.<kind>.` are **publisher-chosen,
dot-free tokens within the `schedule.>` stream space**; a publisher MAY key its schedules with
additional tokens. Weaver keys per **target AND entity** —
`schedule.weaver.timer.<targetId>.<entityId>`, fired
`schedule.weaver.timer.fired.<targetId>.<entityId>` — so two targets projecting a freshness
deadline for the same entity hold independent timer slots (no cross-target last-write-wins on the
shared `MaxMsgsPerSubject: 1` rollup).

**Rationale:** the per-entity-only template forced two installed targets that both project a
`freshUntil` for the same entity onto one timer slot (last projection wins; the earlier deadline is
silently overwritten). Keying the subject per target removes that collision. The token is the
publisher's to choose within `schedule.>` — the fired-target side of §10.4 already reads the
republish target as "publisher-chosen … e.g."; this pins the same reading for the schedule-subject
template line so the two are consistent and the per-target shape is contract-legal for the record.

## Request 3: §10.2 optional engine-recognized `freshUntil` param column convention

**Location:** §10.2 "weaver-targets row shape" — the §10.2 free-form param-column list / the
engine-recognized convention columns.

**Current text:** the frozen column list names the `missing_*` gap-column class and the
`entityKey`/`violating` echo columns; free-form param columns are carried but the freshness-deadline
convention is unnamed.

**Requested text:** name **`freshUntil`** as an optional engine-recognized convention column
(RFC3339 string), carried as a §10.2 free-form param column. The target cypher computes
`resolve + window` and projects the deadline as `freshUntil`; the engine converts the instant into
an `@at` schedule (the time→op leg, §10.4) and never computes the window itself. Documented in
`docs/components/weaver.md`.

**Rationale:** `freshUntil` joins the `missing_*` class of engine-read convention columns without
adding a config knob for the column name. The frozen column list is otherwise silent on where the
freshness deadline rides; pinning the convention into §10.2 makes the engine/Lens seam explicit (the
7.4 annotation-CAR precedent).

---

# Contract #10 Amendment Request (Story 10.2 — Weaver Two-Phase Nudge actuator)

This amendment to the **FROZEN** Contract #10 (`docs/contracts/10-orchestration-surfaces.md`) was
adjudicated during Story 10.2 (Weaver §10.3 Two-Phase Nudge live-wiring), 2026-06-13. Per CLAUDE.md
"Frozen contracts," it is not an in-flight edit — it is raised here for ratification by the contract
owner (Andrew) + a Contract #10 revision-history entry. The implementation
(`internal/weaver/{evaluator.go,engine.go,strategist.go}`, `internal/weaver/nudge/protocol.go`)
already builds to the requested text; the existing §10.8 `nudge` example omits `operation` and is
adjusted by the same amendment.

**STATUS: RATIFIED 2026-06-13 (Andrew).** Applied to Contract #10 (§10.8 action table + `missing_bgcheck` example) + revision-history entry; the working tree already built to this text.

## Request 4: §10.8 `nudge` action gains a required `operation` field (the resolve-op type)

**Location:** §10.8 "Action contracts" table — the `nudge` row — and the §10.8 `meta.weaverTarget`
example's `missing_bgcheck` nudge entry.

**Current text:** the `nudge` action params are `{ adapter, subject, params? }`. The example
`missing_bgcheck` nudge omits any operation field:

```json
"missing_bgcheck": { "action": "nudge", "adapter": "backgroundCheck", "subject": "row.applicant" }
```

**Requested text:** the `nudge` action params are **`{ adapter, operation, subject, params? }`**, where
`operation` is the **resolve operation type** — the op the Two-Phase Nudge submits in its Resolve
phase to record the external outcome back into Core KV (the second leg of Claim→Execute→Resolve,
arch Item 3 / §10.3). It is **required** on a nudge action. The example gains it:

```json
"missing_bgcheck": { "action": "nudge", "adapter": "backgroundCheck",
                     "operation": "ResolveBackgroundCheck", "subject": "row.applicant" }
```

**Rationale:** the §10.3 `weaver-claims` record value shape — frozen in the same contract — **already
includes `operation`** (`value: { claimId, adapter, operation, subject, params, idempotencyKey, … }`,
§10.3). The claim's `operation` can only come from the playbook's nudge action, but the §10.8 action
row that authors the playbook has no field to carry it: the two frozen shapes are internally
inconsistent. The resolve phase is not optional — without an `operation` type the Resolve leg has no
op to submit and the claim could never reach `state=resolved` — so `operation` is required, not
`operation?`. The implementation routes a blank or `row.`-templated `operation` to the same
`errConfig` posture as a blank/templated `adapter` (surfaced to Health, not dispatched). Epic 11
playbooks will be authored against this amended row.
