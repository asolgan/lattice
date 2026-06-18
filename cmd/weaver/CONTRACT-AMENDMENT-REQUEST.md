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
`errConfig` posture as a blank/templated `adapter` (surfaced to Health, not dispatched). Epic 14
playbooks will be authored against this amended row.

---

# Contract #10 Amendment Requests (13.1 — External I/O Bridge package)

Part of the **External I/O Bridge** amendment package (ratify together with the sibling requests in
`cmd/{loom,refractor}/CONTRACT-AMENDMENT-REQUEST.md`; umbrella =
`_bmad-output/planning-artifacts/sprint-change-proposal-2026-06-18.md`). **STATUS: RATIFIED 2026-06-18 (Andrew) — APPLIED 2026-06-18**: §10.3 + §10.8 of
`docs/contracts/10-orchestration-surfaces.md` amended (`weaver-claims` + Two-Phase Nudge retired with the FR58
determinism invariant pinned; the `nudge` GapAction retired) and the 13.1 revision-history entry added. Raised **before** implementation; the External I/O Bridge
epic builds to the ratified text. Unlike Requests 1–4 above (which were retroactive — the working tree already built
to the requested text), this is a **gating** request: the 13.1 story does not start until the surface is
agreed.

> **Pre-commit coherence refinement (Andrew, 2026-06-18).** The **applied** §10.3 retirement note
> **generalizes** the replacement claim from "the service-instance vertex" to a **package-chosen** claim
> vertex (the bridge is type-agnostic; `service.<x>.instance` is the lease demo's choice). The external
> **outcome is recorded as aspect(s) per D5** (minimum data in the vertex root), not fat root `data`,
> and FR58 idempotency rests on the deterministic result-op `requestId` + the adapter's `idempotencyKey`
> dedup — not a typed-vertex read. The "Requested text" below is illustrative with the lease demo's
> vertex; the applied §10.3 text is the generic form.

This file carries **two** Contract #10 touches: **Request A** (§10.3 — retire `weaver-claims` / the
Two-Phase Nudge protocol; pin FR58 determinism) and **Request B** (§10.8 — retire the `nudge`
GapAction). They **supersede** Request 4 above (Story 10.2's `operation`-on-nudge addition): the entire
`nudge` action and the `weaver-claims` record it fed are being retired, so the `operation` field added
to `nudge` is moot. The companion amendments touch Contract #10 §10.5/§10.6 (`cmd/loom`, the
`externalTask` step) and §10.2 (`cmd/refractor`, the actorAggregate target-lens key shape). The
`external` event domain needs **no** Contract #3 amendment (an ordinary domain under the open
`<domain>.<eventName>` model; realized via a package event-type DDL + the bridge consumer).

> **What is RETIRED vs KEPT (bounded — proposal M3):** retire `internal/weaver/nudge/` (dispatch +
> resolve-op machinery), the `nudge` strategist/actuator branches, **and** the `weaver-claims` bucket +
> its provisioning constant. **KEEP `weaver-state` + the reconciler/sweeper** — they still serve the
> surviving `triggerLoom` / `assignTask` / `directOp` actions (the reconciler branches into nudge only
> at the `ga.Action == actionNudge` check). The two `Fake*` adapters **move** (not copy) to the bridge.
> **CI lockstep (the 12.4 lesson):** `WeaverClaimsBucket` is asserted in **two** kernel-verify
> enumerations — `scripts/verify-kernel.go:275` (bucket list 273-277) and `internal/bootstrap/verify.go:168`
> (bucket list 166-169) — both must drop it in the same change, plus its `primordial.go` constant +
> bucket-create.

## Request A: §10.3 — retire `weaver-claims` + the Two-Phase Nudge protocol; pin the FR58 determinism invariant

**Location:** §10.3 "Operational KV namespaces" — the `weaver-claims` bucket row in the namespace table,
the entire "**`weaver-claims` — Two-Phase Nudge claim record**" subsection (key/value shape + the
Claim→Execute→Resolve protocol + recovery bullets + 90d retention), and the `weaver-state`
subsection's nudge-specific clauses (the `claimId`-on-nudge-mark bullet).

**Current text (§10.3, the namespace-table row):**

> | `weaver-claims` | Weaver | `<claimId>` | primordial (exists), 90d retention |

**Current text (§10.3, the `weaver-claims` subsection — the record shape + protocol):**

> ### `weaver-claims` — Two-Phase Nudge claim record (FR58, arch Item 3)
>
> ```
> key:   <claimId>                             # minted NanoID per nudge dispatch
> value: { claimId, adapter, operation, subject, params,
>          idempotencyKey,                     # = claimId; handed to the adapter so IT dedups the real external action
>          state,                              # claimed → executing → resolved | failed
>          claimedAt, resolvedAt?, resolveRef? }   # resolveRef = requestId / op key of the resolve mutation in Core KV
> ```
> - Protocol (arch Item 3): **Claim** (write record, `state=claimed`) → **Execute** (external call with
>   `idempotencyKey`; `state=executing`) → **Resolve** (submit a normal op through the Processor → Core
>   KV, carrying `claimId`; `state=resolved`). The resolve mutation is the audit join (Core KV =
>   business outcome, `weaver-claims` = operational intent).
> - **External idempotency is the `idempotencyKey` (=claimId) the adapter dedups on** — *not* a CAS on
>   the claim key. …
> - **Recovery (reconciler) is read-before-act.** A claim found in `claimed`/`executing` past its lease:
>   the reconciler (a) **reuses the same `claimId`/`idempotencyKey`** … and (b) **checks `resolveRef` /
>   Core KV for an already-landed resolve before re-executing**. …
> - 90d retention (configurable).

**Requested text:** **retire `weaver-claims`** (drop the namespace-table row, the subsection, the bucket
+ its primordial constant/provisioning, and the two CI-verify enumerations) **and retire the in-Weaver
Two-Phase Nudge dispatch/resolve** (the Claim→Execute→Resolve protocol leaves Weaver entirely). The
FR58/NFR-S11 "**visible claim recorded before the external call**" guarantee is now satisfied by the
**service-instance vertex in Core KV** — the `vtx.service.<id>` instance vertex (class `service.<x>.instance`) created by the
`externalTask`'s `instanceOp` **before** the `external.*` event is even publishable (`cmd/loom` Request
E2). The claim, the resolve target, and the result holder unify into **one
auditable business vertex** instead of an operational `weaver-claims` record.

**Hard invariant (FR58 determinism — proposal M4, pinned):** the **bridge's result-op `requestId` MUST
be `deterministic(idempotencyKey = instanceKey)`**. A redelivered `external.*` event therefore produces
the **same** result-op requestId, which collapses on the Contract #4 `vtx.op.<requestId>` tracker
(`internal/processor/step2_dedup.go` `CheckDedup` → `DedupDuplicate` on a present non-deleted tracker)
→ **exactly one** result mutation. Without this, a redelivery double-writes the result. This is the
event-plane analog of the deterministic-requestId rule already in the contract for the temporal
fired-timer→op path (§10.4) and the retired Weaver resolve-op (`deriveResolveRequestID`,
`internal/weaver/engine.go:519`).

**KEEP `weaver-state` + the reconciler/sweeper.** Only the **nudge-specific** clauses of the
`weaver-state` subsection retire — the `claimId`-minted-with-the-CAS-create bullet and the "`nudge` is
safe (same `claimId`)" half of the re-fire-idempotency bullet. The mark, its TTL/lease, the
level-reconciled mark-clearing, and the reconciler sweep all **stand** (they serve
`triggerLoom`/`assignTask`/`directOp`).

**Rationale:** the retired nudge resolve-op **cannot address a candidate entity distinct from the nudge
`subject`** — the structural defect the reference vertical surfaced. Evidence (file:line): the resolve-op
payload is hard-coded `{ claimId, result, expectedRevision }` with `authTarget = np.subject`
(`internal/weaver/engine.go:517-530`, the payload map at 520-524), and a **Starlark DDL op cannot read
`authContext`** — `internal/processor/starlark_runner.go:465-472` binds only `{ requestId, lane,
operationType, actor, submittedAt, payload }` (no `authContext`), so the DDL that should record the
result has **no channel** to learn which vertex (candidate ≠ subject) to write. Moving external I/O onto
the bridge dissolves the addressing gap entirely (Loom created the instance, so its key rides the event
and the bridge posts straight back — no `authContext`-in-Starlark, no resolve-payload hack). The
instance-vertex-as-claim is **more honest** than an operational claim record: it is auditable business
state with a natural idempotency key (one instance = one call), and read-before-act on the instance
prevents double-action on redelivery. Retirement is **bounded** (keep the reconciler/sweeper); the
`Fake*` adapters relocate to the bridge **before** teardown so there is never a window where neither path
works (move-then-delete; full teardown only after the convergence e2e is green). Security/trusted-infra
plane — full 3-layer adversarial review + Gate 2 (BLOCKED) + Gate 3 (DEFENDED, convergence must stay
defended with the nudge gone) when 13.5 lands.

## Request B: §10.8 — retire the `nudge` GapAction

**Location:** §10.8 "Action contracts" table — the **`nudge`** row (the one Request 4 above just amended
to add `operation` in Story 10.2 — **this supersedes that amendment**), plus the §10.8 `meta.weaverTarget`
example's `missing_bgcheck` nudge entry and the §10.8 flow/anti-storm "Re-fire idempotency by action"
clause's nudge half.

**Current text (§10.8, the `nudge` action row — as amended by Request 4):**

> | `nudge` | `{ adapter, operation, subject, params? }` | Two-Phase Nudge to the external adapter (§10.3 `weaver-claims`); `subject` = the entity the nudge concerns. `operation` is **required** — it is the **resolve-op type** submitted in the Resolve leg to record the external outcome back into Core KV (the `operation` field of the §10.3 `weaver-claims` record). A blank or `row.`-templated `operation` is a config error → alert (same posture as a blank `adapter`). |

**Current text (§10.8, the `missing_bgcheck` example entry):**

> ```json
> "missing_bgcheck": { "action": "nudge", "adapter": "backgroundCheck",
>                      "operation": "ResolveBackgroundCheck", "subject": "row.applicant" }
> ```

**Requested text:** **remove (deprecate) the `nudge` action** from the §10.8 action-contracts table and
the `missing_bgcheck` example; **external remediation is now `triggerLoom` of a pattern containing an
`externalTask`** (`cmd/loom` Request E2). The example `missing_bgcheck` gap becomes a `triggerLoom`:

```json
"missing_bgcheck": { "action": "triggerLoom", "pattern": "backgroundCheck", "subject": "row.applicant" }
```

(where the `backgroundCheck` pattern's body is an `externalTask` against the `backgroundCheck` adapter).
The §10.8 "Re-fire idempotency by action" clause drops its `nudge`-is-safe half; the surviving actions
(`triggerLoom`/`assignTask`/`directOp`) keep their documented re-fire bounds (§10.3 weaver-state).

**Rationale:** external I/O **moves out of Weaver** (convergence *detection*) into **Loom + bridge**
(deterministic *execution*) — the architectural-placement correction at the heart of the proposal
(Andrew: embedding external I/O in the convergence engine "never sat right"). Weaver's job collapses to
**detect → `triggerLoom`**; it no longer dispatches or resolves external calls, so the `nudge` action has
no remaining producer or consumer. This row was *just* amended in Story 10.2 (Request 4 added `operation`)
— that amendment is explicitly **superseded** here, not reverted in isolation: the whole `nudge` surface
retires together with `weaver-claims` (Request A). Security plane — same review/gate bar as Request A.
