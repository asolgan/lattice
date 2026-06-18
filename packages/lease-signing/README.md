# lease-signing

The Loftspace lease-application convergence vertical (Epic 14 centerpiece). It
wires the prior bricks — the `leaseapp` vertex type, the actorAggregate
convergence lens, the §10.8 playbook, the Loom `externalTask` patterns + their
`instanceOp`/`replyOp` DDLs, and `SignLease` — into one installable package.

## Inventory

| Kind | Name | Purpose |
|------|------|---------|
| DDL (vertex type) | `leaseapp` | `CreateLeaseApplication` (mints `vtx.leaseapp.<id>`, root `{}`, `applicationFor` link to the applicant) + `SignLease` (writes the `.signature` aspect). |
| DDL (externalTask instanceOp) | `leaseServiceInstance` | `CreateLeaseServiceInstance` — mints the claim vertex `vtx.service.<handle>` (`.class` + `.family` aspects, `providedTo` link), emits `external.<adapter>`. |
| DDL (externalTask replyOp) | `leaseServiceReply` | `RecordLeaseServiceOutcome` — records the `.outcome` aspect from `{externalRef, result}`, emits `orchestration.externalTaskCompleted{externalRef}`. |
| Lens (actorAggregate) | `leaseApplicationComplete` | One-row-per-anchor convergence lens → `weaver-targets` bucket, bare-NanoID key (§10.2). |
| WeaverTarget (playbook) | `leaseApplicationComplete` | gap → remediation (§10.8). |
| LoomPattern | `backgroundCheck`, `collectPayment` | single `externalTask` step each, `completionDomains: ["orchestration"]`. |
| LoomPattern | `onboarding` | single `userTask` step (`RecordIdentityPII`), `completionDomains: ["orchestration"]`. |
| OpMetas | `SignLease`, `RecordIdentityPII`, `CreateLeaseServiceInstance`, `RecordLeaseServiceOutcome` | `forOperation` resolution + discoverability. |

Depends: `identity-domain`, `service-domain`, `orchestration-base`.

## The externalTask seam (Contract #10 §10.5/§10.6)

An `externalTask` step is two waits in sequence: Loom submits the **`instanceOp`**
(`CreateLeaseServiceInstance`) carrying the bare handle it minted, parks on
`token.<handle>`, and disarms the creation-deadline once the instanceOp commits
(the bridge wait is then unbounded). The instanceOp:

1. prepends the package-chosen claim-vertex type → `vtx.service.<handle>`;
2. mints it template-less with a `.class` aspect (`service.<family>.instance`, the
   14.1 shape) and a `.family` aspect (the lens's discriminator — see below), plus
   the `providedTo` link to the applicant identity;
3. emits `external.<adapter>` off its own transactional outbox, body
   `{instanceKey, adapter, replyOp, params, externalRef, idempotencyKey}` — the
   shape the bridge's `externalEvent` reader consumes.

The bridge calls the adapter and posts the **`replyOp`**
(`RecordLeaseServiceOutcome`) with payload **`{externalRef, result}` only** — no
`status`, no `completedAt`. The replyOp:

1. reconstructs `vtx.service.<externalRef>`;
2. derives `status` + `completedAt` itself (see below);
3. writes the `.outcome` aspect `{status, completedAt, result}` (the 14.1 shape, D5);
4. **emits `orchestration.externalTaskCompleted{externalRef}`** — the uniform
   completion signal Loom correlates on (symmetric to `orchestration.taskCompleted`
   for a userTask). **Without this event the step never completes** (the deadline
   disarmed, the bridge reply carried no completion signal). This is why the
   patterns declare `completionDomains: ["orchestration"]`, not the replyOp's own
   domain.

The two wrapper DDLs are a **matched pair**: both choose `service` as the claim
type, both map the bare handle ↔ `vtx.service.<handle>`, and the replyOp echoes
the same bare handle as `externalRef`.

### Why wrapper DDLs (not 14.1's service ops)

14.1's `CreateServiceInstance` does not emit `external.<adapter>`, and
`RecordServiceOutcome` takes a full `instanceKey` + caller `status`/`completedAt`
and emits `service.outcomeRecorded` — not the `orchestration.externalTaskCompleted`
Loom needs, while the bridge supplies only `{externalRef, result}`. Reusing them
would require editing the DONE service-domain. The `.outcome` aspect **shape** is
reused (D5 fidelity); the ops are package-local.

## LOUD FLAG — the `status="completed"` demo simplification (Q2)

The bridge posts a reply **only on adapter success** (an adapter error is
Nak+retry, never a reply), and supplies no structured status. So the replyOp
**derives `status="completed"` on every reply**, stores `result` verbatim
(unparsed — the free-form adapter string is brittle to parse), and derives
`completedAt = time.rfc3339_utc(op.submittedAt)` (the bridge supplies no
timestamp). **A `failed` outcome has NO producer on the Phase-2 bridge path.**

**Phase-3 plug-in point:** when an adapter returns a structured pass/fail result
(or a bridge change threads `status` onto the reply payload), the replyOp reads it
instead of hard-coding `completed`, and the lens's `missing_*` predicate keys off
the real status.

## LOUD FLAG — freshness is "completed outcome exists", not a rolling window

The §10.2 model is `missing_bgcheck = NOT EXISTS(check WHERE date > now − window)`.
The `full` rule engine has **no date arithmetic**, the actorAggregate projection
supplies only `$now`/`$projectedAt` (no window param), and the Starlark sandbox has
**no duration-add** for the replyOp to precompute an `expiresAt`. So for Phase 2 the
lens freshness predicate is **"a completed outcome of that family exists"** (the
Fake adapters always produce a completed outcome). The replyOp records
`completedAt` for provenance and that future use.

**Phase-3 refinement:** add a Starlark duration builtin (replyOp precomputes
`expiresAt = completedAt + window`, lens compares `inst.outcome.data.expiresAt > $now`)
**or** have the projection supply a window-floor param.

## Scalar convergence columns through the actorAggregate projection

The §10.2 convergence row carries **scalar** columns (`violating` / `missing_*`
bools, `entityKey` / `applicant` strings). The lens **cypher** produces them
correctly and is proven one-row-per-anchor at the rule-engine level. The
actorAggregate projection `EnvelopeFn` (`internal/refractor/projection/driver.go`)
projects each body column by the **shape** of its RETURN value: a list / `collect`
column is realness-filtered (the roster behavior — `my-tasks`,
`capabilityEphemeral`), and a **scalar** column projects **verbatim** so Weaver's
`boolColumn` reads a Go bool and the §10.8 `row.<col>` params resolve as strings
(Contract #6 §6.13 scalar-passthrough amendment, CAR E6). The 14.2 `keyColumn`
mechanism carries the bare-NanoID row key; together they make this convergence lens
projectable end-to-end through Refractor.

The lens declaration in this package is already pre-shaped for that path (keyColumn
set, scalar body columns named) and needs no change.

## Other notes

- **The type is lowercase `leaseapp`** — `leaseApp` (camelCase) is an invalid
  Contract #1 type segment. The epics / §10.2 / orchestration-base `vtx.leaseApp.*`
  strings are illustrative only. The `targetId` `leaseApplicationComplete` stays
  camelCase (it is a KV-key token, not a type segment).
- **Epics AC#3 is superseded.** The epics text ("each externalTask declares the
  replyOp's completion domain", advance-on-instanceOp-commit + deadline-as-backstop)
  was the first 13.1 ratification, corrected by the 13.6 follow-up. This package
  builds to the current Contract #10 §10.5/§10.6: `completionDomains: ["orchestration"]`,
  the replyOp emits `orchestration.externalTaskCompleted`, the deadline disarms on
  instanceOp commit.
- **The `.family` discriminator aspect.** The lens needs to bucket bgcheck vs
  payment. It cannot read the `.class` aspect via `inst.class.data.value` because the
  vertex envelope `class` field shadows the `.class` aspect on the projection read
  path. So the instanceOp writes a distinct `.family` aspect the lens reads as
  `inst.family.data.value` (the `.class` aspect is still written for 14.1 shape
  fidelity).
- **Tests use direct `.outcome` writes** (`RecordLeaseServiceOutcome` with a synthetic
  `{externalRef, result}` payload), never a live bridge — the bridge-driven e2e is
  14.5.
