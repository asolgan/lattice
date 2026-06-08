# Story 8.2: User-task steps

Status: done

## Senior Review (Winston) — 2026-06-07

Two adversarial review rounds (3 parallel layers each: Blind Hunter diff-only, Edge Case Hunter
diff+repo, Acceptance Auditor diff+spec+contracts).

- **Round 1 — userTask seam.** Found one HIGH (a rejected/lost `CreateTask` parked the instance
  forever — no backstop, since a userTask arms no deadline). Fixed forward: a bounded
  `CreateTaskTimeout` creation-deadline + a task-vertex read-before-act probe (`onUserTaskDeadline`)
  that disarms once the task exists (unbounded human wait begins) and fails a rejected CreateTask —
  the userTask analog of the §10.6 systemOp deadline+probe. Plus a load-time warn for a misconfigured
  `completionDomains`, the `disarmDeadline` re-entry guard, and comment/house-rule cleanup.
- **Course-correction (Andrew-ratified).** The review surfaced that the whole event taxonomy was
  domain-less (every class dot-free PascalCase), so §10.5's per-domain routing was fictional
  codebase-wide. Established the event-domain model (`<domain>.<eventName>`, enforced at commit step 7,
  surfaced as a discrete `domain` field on the Event document), renamed ~23 classes across all
  producers + tests, and reconciled 8.2 to the `orchestration` domain. Reshaped Epic-8: stories 8.4+8.5
  consolidated into one "Loom durable-consumer lifecycle manager + Health KV" story (also absorbs ECH
  Path #3 filter-recreate). Contracts #3 + #10 amended with 2026-06-07 revision entries; CAR R6–R9
  ratified.
- **Round 2 — event-domain model.** All three layers clean: **no Critical/High/Medium**; the
  load-bearing risk (a missed rename → runtime rejection under the new enforcement) turned up **zero
  stragglers**, verified repo-wide including unexercised/integration/demo paths; enforcement correctly
  scoped to events only (not vertex/aspect/link classes). Three cosmetic findings fixed inline
  (`token_test.go` AC-narration, `engine.go` stale `data.taskKey`→`payload.taskKey` comment, hardened
  the admin `taskId` validation against subject-hostile chars).

Gates all green: `go build`, `make vet`, `golangci-lint` (0 issues), `make verify-kernel`,
`go test ./...` (40 pkgs, exit 0), `make test-bypass` (Gate 2 BLOCKED), `make test-capability-adversarial`
(Gate 3 DEFENDED). Committed to `main`; CI watched green.

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Winston adjudication (open design questions — RESOLVED 2026-06-07)

Three design questions were raised at story-creation; I (Winston) have independently verified the
load-bearing claims against the code and ruled. These are binding for the dev-story.

1. **`CreateTask` optional `taskId` (the write-ahead seam) — APPROVED.** Verified: `CreateTask` mints
   `task_id = nanoid.new()` internally (`packages/orchestration-base/ddls.go:173`), so Loom cannot
   write-ahead `token.<taskKey>` without controlling the id. The `task` DDL is **package data, not a
   frozen `docs/contracts/*` contract** — adding an *optional* `taskId` (present → used verbatim; absent
   → `nanoid.new()`, so every existing caller is unaffected) is backward-compatible and is the correct
   seam. Proceed as Task 1 specifies. Validate the supplied id is a bare NanoID (CreateOnly semantics
   so a crash-retry collapses on the Contract #4 tracker).

2. **Filter-subject for the dot-free `TaskCompleted` domain — CONFIRMED REAL BUG; fix as follows.**
   Verified: `EventSubject("TaskCompleted")` → `events.TaskCompleted` (single segment,
   `publisher.go:163`); Loom's per-domain consumer filters `events.<domain>.>` (`engine.go:272`), and
   JetStream's `events.TaskCompleted.>` requires ≥1 trailing token — so it would **never** deliver
   `events.TaskCompleted`. **Ruling:** the per-domain consumer must match **both** `events.<domain>`
   (exact, dot-free classes) **and** `events.<domain>.>` (dotted classes) — set the durable's filter to
   the two-subject set `["events.<domain>", "events.<domain>.>"]` (nats.go `jetstream` `FilterSubjects`,
   supported since server 2.10). If `substrate.DurableConsumerConfig` does not yet expose multiple
   filter subjects, **add it** as a small substrate primitive enhancement (mirroring how 8.1 added
   `BatchOp.Delete`) with a dedicated substrate test. **Do NOT** rename the `TaskCompleted` event class
   (the frozen §10.6 contract names it `TaskCompleted`), and **do NOT** fall back to a broad `events.>`
   subscription (the N2 scaling smell from the 8.1 review). The onboarding e2e must positively assert
   the `TaskCompleted` event reaches the `loom-TaskCompleted` consumer — this is the story's single most
   likely silent "waits forever" failure.

3. **`OnboardingComplete` terminal — map to `loom.patternCompleted`; do NOT invent a distinct event.**
   The engine ships **zero domain knowledge**, so it must not emit a domain-specific `OnboardingComplete`
   event. The conceptual "OnboardingComplete" is the existing 8.1 terminal: pattern exhausted →
   `CompletePattern{instanceId}` → `events.loom.patternCompleted`, keyed by `patternRef = "onboarding"`.
   A downstream consumer that wants a distinctly-classed event appends a final `systemOp` step to the
   *pattern* (package data) — out of scope for 8.2. Proceed as AC#7 specifies.

**Minor steer (not blocking):** for "Resolving the bound op to its meta-vertex" (Dev Notes), prefer
option (b) — Loom resolves the op **name** → its `vtx.meta.<opId>` via a Core-KV read of the op meta by
canonical name (Loom already reads Core KV in `trackerExists`); do not invent a third mechanism. For
"Where the bound ops live," the fixture/test DDL is correct — do **not** add onboarding ops to
`orchestration-base` or `identity-domain`.

## Event-domain model amendment (course-correction, Andrew-ratified 2026-06-07)

The 8.2 review surfaced that `TaskCompleted` has no domain, and the deeper investigation found the
**whole event taxonomy is domain-less**: every business event class is dot-free PascalCase
(`IdentityCreated`, `RoleAssigned`, `TaskCreated`, `PackageInstalled`, `MetaVertexCreated`…), so
§10.5's "domain = first dot-free segment" routing model is **fictional codebase-wide** — only Loom's own
`loom.*` lifecycle events were domain-shaped. Andrew ratified establishing a real event-domain model and
**folding it into this story's commit**. This **supersedes** the original surgical CARs R6/R7 (which
become consequences of the model).

**The model (binding):**
- Every core-event class is **`<domain>.<eventName>`**, `eventName` in **lowerCamelCase** (matching the
  existing `loom.patternStarted`). A **domain** is the first segment.
- **Enforced:** a dot-free event class is **rejected** — at commit step 7 (`internal/processor/step7_events.go`)
  and as a belt-and-braces guard in `EventSubject` (`internal/processor/outbox/publisher.go`). There is no
  registered event-type DDL today (Contract #3's "must match a registered event-type DDL" is currently
  unenforced), so the rename touches **only the class string at each emission site + consumers + tests** —
  no event-DDL canonicalNames to keep in sync.
- **Surfaced in the document:** the Event envelope (`internal/processor/step7_events.go` `Event` struct)
  gains a discrete **`domain`** field, set by the Processor from the class's first segment in
  `BuildEventList`. Subject stays `events.<domain>.<eventName>` (the outbox already keeps dots). Domain is
  thus in **both** the subject and the document, single source of truth = the class.

**Taxonomy (the rename map — confirmed):**

| Domain | New classes | Producer |
|--------|-------------|----------|
| `identity` | `identity.created`, `identity.claimed`, `identity.stateChanged`, `identity.merged` | `packages/identity-domain/ddls.go`, `packages/identity-hygiene/ddls.go` |
| `orchestration` | `orchestration.taskCreated`, `orchestration.taskReAssigned`, `orchestration.taskCompleted`, `orchestration.taskCancelled` | `packages/orchestration-base/ddls.go`, `internal/processor/autocomplete.go` |
| `rbac` | `rbac.roleCreated`/`roleUpdated`/`roleTombstoned`/`roleAssigned`/`roleRevoked`, `rbac.permissionCreated`/`permissionUpdated`/`permissionTombstoned`/`permissionGranted`/`permissionRevoked` | `packages/rbac-domain/ddls.go` |
| `package` | `package.installed`, `package.uninstalled` | `internal/bootstrap/install_ddl.go` |
| `meta` | `meta.vertexCreated`, `meta.vertexUpdated`, `meta.vertexTombstoned` | `internal/bootstrap/meta_ddl.go` |
| `loom` | `loom.patternStarted`/`patternCompleted`/`patternFailed` — **UNCHANGED** (already domain-shaped) | `packages/orchestration-base` lifecycle |
| `loftspace` (test) | `loftspace.leaseApproved`, `loftspace.bookCreated`, `Probe`→a domain-shaped name, etc. | test fixtures only (`*_test.go`) |

**Contracts to amend (FROZEN — edit with a revision-history entry, this is the ratified amendment path):**
- **Contract #3** (`docs/contracts/03-mutation-batch-event-list.md`): the event `class` must be
  `<domain>.<eventName>` (a domain segment is required); the Event document carries a `domain` field set by
  the Processor from the first segment; revision entry dated 2026-06-07.
- **Contract #10 §10.5/§10.6**: ratify R6–R9 with the **`orchestration`** domain — the onboarding example
  becomes `completionDomains: ["orchestration"]`; the §10.6 userTask row correlates on `payload.taskKey`
  via the `TaskCompleted`→`orchestration.taskCompleted` event on the `orchestration` domain; invariant-1
  notes the engine supplies the task id; R9 deadline+probe covers the userTask creation path. The "domain =
  first segment" model is now **true**, not illustrative. Revision entry dated 2026-06-07.

**8.2 reconciliation (this story's code, built against the pre-amendment naming):**
- Onboarding fixture `completionDomains: ["TaskCompleted"]` → **`["orchestration"]`**.
- Fixture/fake-Processor emit `TaskCompleted` → **`orchestration.taskCompleted`** (and `TaskCreated` →
  `orchestration.taskCreated`).
- Loom's per-domain consumer filter: revert the dot-free `FilterSubjects` two-subject workaround back to a
  single **`events.<domain>.>`** — enforcement makes dot-free classes impossible, so the workaround is moot.
  Keep the substrate `FilterSubjects` *capability* only if trivially harmless; otherwise revert it + its
  test (the consolidated Story 8.4 will own consumer-lifecycle machinery).
- Mark CAR R6–R9 **ratified** in `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md` (superseded by the broader model).
- Update `docs/components/loom.md` (userTask completes on the `orchestration` domain) + the
  architecture event-model note.

**Out of scope (deferred to the reshaped Epic-8 Story 8.4):** the shared substrate durable-consumer
lifecycle manager (backoff/teardown/`Reset`) + Loom Health KV. Do **not** build those here.

## Story

As a flow author,
I want a Loom step that assigns a task to the subject identity and advances when the user submits the
step's bound operation, so that human-in-the-loop flows (onboarding) run deterministically — and a
long human wait never breaks the cursor.

Story 8.1 stood up the Loom **engine machinery** (pattern source, trigger consumer, per-domain
completion consumers, the durable `loom-state` token index, the command-outbox relay/Actuator, the
deadline watcher, and the event-only lifecycle ops) — but only for **`systemOp`** steps. This story
adds the **`userTask` step kind** on top of that machinery: a step that submits **`CreateTask`**
(assigning a task to the subject identity, bound to the step's operation via `forOperation`), then
**waits** for the user to perform that bound operation. Completing the bound op auto-completes the task
(the commit-path auto-complete is **already built** — `internal/processor/autocomplete.go`), which
emits **`TaskCompleted(taskKey)`**; Loom correlates that back to the instance and advances the cursor.
The deliverable is the **userTask seam** — the task-id write-ahead, the `TaskCompleted` correlation
path, and an onboarding pattern proven end-to-end including a long-wait restart.

## Acceptance Criteria

> **Design seam this story must nail (see Dev Notes "The userTask token write-ahead seam").** For a
> `systemOp` the write-ahead token is the `requestId` Loom chooses. For a `userTask` the §10.6 token
> is the **`taskKey`** of the task `CreateTask` mints — but today **`CreateTask` mints its own
> `task_id = nanoid.new()` internally** (`packages/orchestration-base/ddls.go:173`), so Loom cannot
> know the `taskKey` before commit and cannot write `token.<taskKey>` write-ahead. The chosen
> resolution (AC#1) is the load-bearing decision of this story.

1. **`userTask` step is interpreted (was rejected in 8.1).** Given a `meta.loomPattern` whose steps
   are `{kind:"userTask", operation:"<BoundOp>"}` (no guards — guards are Story 8.3), when Loom reaches
   the step it submits a **`CreateTask`** op (not the bound op) via the command outbox, **not** a
   `systemOp` submission of the bound op. `pattern.validate()` must **accept** `userTask` steps (it
   currently rejects every non-`systemOp` kind — `pattern.go:91-94`). Each step is dispatched by `Kind`
   in the Transition Engine: `systemOp` → submit the bound op directly (8.1 path, unchanged);
   `userTask` → submit `CreateTask`. [Source: docs/contracts/10-orchestration-surfaces.md#10.5]

2. **`CreateTask` submission shape — assignee + forOperation + scopedTo all = the subject (§10.5).**
   The `userTask` `CreateTask` op the Actuator submits has payload:
   `{assignee: <subjectKey>, forOperation: vtx.meta.<boundOpId>, scopedTo: <subjectKey>, expiresAt: <ts>}`.
   §10.5 binds a Loom `userTask`'s links: **`assignedTo` → the subject**, **`forOperation` → the step's
   bound operation meta-vertex**, **`scopedTo` → the subject** (a Loom userTask scopes its grant to the
   instance subject; the frozen step shape carries no separate target field). `forOperation` is the
   `vtx.meta.<opId>` of the **bound operation** named by `step.operation` — Loom must resolve the
   op-name → its op meta-vertex key (see Dev Notes "Resolving the bound op to its meta-vertex").
   `CreateTask` validates all three endpoints exist (no-orphan FR29/P4); the subject identity must be
   alive. The grant this task creates is what later **authorizes** the user's bound op
   (`authContext.task` → §10.7), and performing that bound op auto-completes the task. `expiresAt` is
   set to a generous horizon (a userTask wait is by design long — see AC#6).
   [Source: docs/contracts/10-orchestration-surfaces.md#10.5, #10.7; packages/orchestration-base/ddls.go]

3. **The userTask token write-ahead — Loom controls the task id.** Loom must write the
   `token.<token>` reverse pointer **in the same `loom-state` AtomicBatch** as the cursor/outbox/
   deadline write-ahead, **before** `CreateTask` commits (Crash-safety invariant 1, §10.6). The token
   for a userTask is the **`taskKey`** the completion event will carry (`TaskCompleted.data.taskKey`).
   Therefore Loom must **determine the `taskKey` deterministically before submission** and `CreateTask`
   must mint the **same** key. The resolution (Dev Notes): `CreateTask` accepts an **optional
   caller-supplied `taskId`** (when present, it is used verbatim instead of `nanoid.new()`); Loom
   derives a deterministic `taskId` from `(instanceId, cursor)` exactly as it derives a systemOp
   `requestId` (`deriveRequestID`), passes it in the `CreateTask` payload, and write-aheads
   `token.<vtx.task.<taskId>>`. A crash-retry re-submits the **same** `CreateTask` with the **same**
   `taskId`, collapsing on the Contract #4 `vtx.op.<requestId>` tracker (idempotent — no duplicate
   task). The `CreateTask` op's own `requestId` remains Loom's deterministic step requestId (the
   submission idempotency handle); the **token** is the derived `taskKey` (the completion-correlation
   handle). [Source: docs/contracts/10-orchestration-surfaces.md#10.6 crash-safety invariant 1;
   internal/loom/engine.go submitStep; packages/orchestration-base/ddls.go:173]

4. **`TaskCompleted` correlation — by `taskKey`, on the `TaskCompleted` domain.** Completion of a
   userTask step is the **`TaskCompleted`** core-event the auto-complete path emits when the user's
   bound op commits (`internal/processor/autocomplete.go` — **already built; this story consumes it,
   does not build it**). That event carries **`data.taskKey`** (a `vtx.task.<id>` key). Loom's
   completion handler must, for the userTask path, read **`data.taskKey`** from the event body and
   resolve the durable **`token.<taskKey>` GET** → instance → advance (the same idempotent pointer-
   presence guard as systemOp). The event class is `TaskCompleted` → the outbox subjects it
   `events.TaskCompleted` (`EventSubject`, `internal/processor/outbox/publisher.go:163`) → its
   **domain is `TaskCompleted`** (first dot-free segment, §10.5). So a userTask pattern's
   `completionDomains` **must include `"TaskCompleted"`** — **NOT** `["identity"]`. (See Dev Notes
   "The §10.5 example onboarding `completionDomains` is misleading" — the contract's *example* lists
   `["identity"]`, which would never see `events.TaskCompleted`; raise a CONTRACT-AMENDMENT-REQUEST
   note, do not edit the frozen contract.) [Source: docs/contracts/10-orchestration-surfaces.md#10.6,
   #10.5; internal/processor/autocomplete.go; internal/processor/outbox/publisher.go:163]

5. **Completion handler reads two body shapes without per-pattern knowledge.** Today
   `handleCompletion` reads only top-level `Event.requestId` (the systemOp correlation key,
   `engine.go:284-322`). The userTask completion event (`TaskCompleted`) carries its correlation key in
   **`data.taskKey`**, not in `requestId` (the top-level `requestId` on a `TaskCompleted` event is the
   *user's bound-op* requestId, which Loom does not know). The completion handler must therefore try
   **both** correlation keys against the durable token store — resolve by `requestId` (systemOp) **and**
   by `data.taskKey` (userTask) — and advance on whichever resolves a live pointer (at most one will;
   tokens are unique). This keeps Loom domain-ignorant: it does not know which event is which; it tries
   the two structural keys the contract defines and the pointer decides. A `TaskCompleted` whose pointer
   is gone (already advanced) is dropped (idempotent). [Source:
   docs/contracts/10-orchestration-surfaces.md#10.6]

6. **Long wait does not break correctness — the human wait is unbounded by design.** A userTask step
   **must not** arm the systemOp-style bounded `deadline.<instanceId>` that would false-fail a step
   legitimately waiting on a human (the deadline watcher's `onDeadline` would otherwise probe an absent
   tracker → `fail`). A userTask step either arms **no** step deadline, or one set far beyond any human
   response window — so a user taking days is never failed. The instance cursor is durable in
   `loom-state` (`instance.<instanceId>`), so the wait survives an engine restart: a mid-wait restart
   resumes the durable per-domain consumers from their ack floor and the `token.<taskKey>` pointer is
   still live, so when the user finally acts the completion correlates and the cursor advances. The
   e2e (AC#9) proves this with a restart **before** the user acts. [Source:
   docs/contracts/10-orchestration-surfaces.md#10.6 invariant 1/2; docs/components/loom.md "Long waits"]

7. **The onboarding bound ops + the terminal.** The fixture onboarding pattern is
   `[collectName, collectPhone, collectAddress]` userTask steps whose bound ops are
   `SetName`/`SetPhone`/`SetAddress` over the subject identity. These bound ops do **not** exist yet —
   author them as simple aspect-writing ops in a **test/fixture** package (or as a small fixture DDL in
   the e2e harness; do **not** bloat `orchestration-base` unless that is the natural home — see Dev
   Notes "Where the bound ops live"). Each writes one aspect on the subject identity and is
   task-authorized (`authContext.task` → §10.7), so its commit auto-completes the task. **Completing
   the final step emits `OnboardingComplete`** — which maps to the pattern's existing terminal:
   pattern exhausted → Loom submits **`CompletePattern{instanceId}`** → its commit emits
   **`events.loom.patternCompleted`** (built in 8.1, `engine.go` `complete()`). "OnboardingComplete" is
   the **conceptual** name for this terminal; the concrete event is `loom.patternCompleted` keyed by
   the onboarding instance. (If a distinct `OnboardingComplete` business event is genuinely required by
   a consumer, that is a separate pattern-level concern — flag it; the AC is satisfied by the
   `patternCompleted` terminal.) [Source: docs/contracts/10-orchestration-surfaces.md#10.5, #10.9;
   internal/loom/engine.go complete()]

8. **Module boundary unchanged.** `internal/loom` still imports **only `substrate/*`** + stdlib (the
   8.1 boundary test `boundary_test.go` / `TestModuleBoundary_NoRawNATS` must still pass). The userTask
   path adds **no** import of `internal/processor`/`weaver`/`refractor` and **no** raw `nats.io`/
   `jetstream` handle. The `CreateTask` submission is just another outbox op (the relay publishes it
   exactly like a systemOp). [Source: internal/loom/boundary_test.go; 8.1 AC#8]

9. **Verification — onboarding e2e, end-to-end + long-wait restart.** A test installs the onboarding
   `meta.loomPattern` (`[collectName, collectPhone, collectAddress]` userTask steps, no guards,
   `completionDomains: ["TaskCompleted"]`) over a subject identity, drives it via a **real
   `StartLoomPattern` submission**, and asserts:
   - each step in order submits a **`CreateTask`** (assignee/forOperation/scopedTo = the subject, bound
     op set) and the flow **waits** (cursor parked, `token.<taskKey>` live, no advance);
   - the test **simulates the user** submitting each bound op (`SetName` → `SetPhone` → `SetAddress`),
     which triggers the **auto-complete** path → `TaskCompleted(taskKey)` → Loom correlates and advances
     the cursor to the next step;
   - the cursor advances to **exhaustion** and the terminal **`loom.patternCompleted`**
     ("OnboardingComplete") is emitted;
   - a **long wait** is correct: an engine restart **mid-flow, before the user acts** on a step, does
     not break the flow — after restart the user submits the bound op and the cursor advances exactly
     once (no double `CreateTask`, no double advance), correlated against the durable `token.<taskKey>`
     pointer (no in-memory index). [Source: docs/contracts/10-orchestration-surfaces.md#10.6, #10.9
     crash-safety; internal/loom/loom_e2e_test.go]

## Tasks / Subtasks

- [x] **Task 1 — `CreateTask` accepts an optional caller-supplied `taskId` (the write-ahead seam)** (AC: #3)
  - [x] In `packages/orchestration-base/ddls.go` `taskDDLScript`, change `CreateTask` to use a
        caller-supplied `taskId` when present, else `nanoid.new()`. Validate it is a bare NanoID
        (no dots / key prefixes) so the minted `task_key = "vtx.task." + task_id` stays well-formed;
        reject a malformed `taskId` with a structured `ScriptError`. Add `taskId` to `InputSchema`
        (optional) + `FieldDescription`. **Do not** make it required (admin/manual `CreateTask` keeps
        minting its own). [Source: packages/orchestration-base/ddls.go:145-191]
  - [x] Update `create_task_test.go` / `task_script_test.go`: a `CreateTask` with a supplied `taskId`
        commits `vtx.task.<thatId>`; a duplicate `CreateTask` with the same `taskId` collapses on the
        Contract #4 tracker (idempotent — no second task vertex); a malformed `taskId` is rejected.
  - [x] Confirm this does not weaken the no-orphan / endpoint-validation invariants (assignee /
        forOperation / scopedTo still validated). [Source: packages/orchestration-base/ddls.go]

- [x] **Task 2 — `pattern.go`: accept `userTask` steps** (AC: #1)
  - [x] Relax `Pattern.validate()` to accept `{kind:"userTask"}` steps (currently every non-`systemOp`
        kind is rejected, `pattern.go:91-94`). Keep the guard rejection (guards remain Story 8.3) and
        the `operation` required check. A `userTask` step's `operation` is the **bound op name** (e.g.
        `SetName`). [Source: internal/loom/pattern.go:84-104; docs/contracts/10-orchestration-surfaces.md#10.5]
  - [x] Update `pattern_test.go`: a userTask pattern validates; a guarded step still rejects in 8.2 scope.

- [x] **Task 3 — Transition Engine dispatches by step kind** (AC: #1, #2, #3, #6)
  - [x] In `engine.go submitStep`, branch on `step.Kind`:
        - **`systemOp`** → existing path (submit the bound op; arm the bounded `deadline` —
          unchanged 8.1 behaviour).
        - **`userTask`** → submit **`CreateTask`** (not the bound op). Build its payload
          `{assignee, forOperation, scopedTo, expiresAt, taskId}` per AC#2/#3; the write-ahead **token
          is the `taskKey` (`vtx.task.<derivedTaskId>`)**, not the `CreateTask` op requestId; **do not
          arm the bounded step deadline** (or arm a far-future one) — a human wait must never false-fail
          (AC#6). [Source: internal/loom/engine.go:357-375; docs/contracts/10-orchestration-surfaces.md#10.5, #10.6]
  - [x] The `CreateTask` op's `requestId` stays Loom's deterministic step requestId
        (`deriveRequestID(instanceId, cursor)`); the **token** written into the AtomicBatch is
        `vtx.task.<deriveTaskId(instanceId, cursor)>`. Keep these two derivations distinct + documented
        (the op-submission idempotency handle vs the completion-correlation handle). [Source:
        internal/loom/engine.go deriveRequestID]
  - [x] Resolve the bound op name (`step.operation`, e.g. `SetName`) → its `vtx.meta.<opId>` for the
        `forOperation` link (see Dev Notes "Resolving the bound op to its meta-vertex"). [Source:
        docs/contracts/10-orchestration-surfaces.md#10.5]

- [x] **Task 4 — completion handler correlates by `taskKey` as well as `requestId`** (AC: #4, #5)
  - [x] Extend `eventBody` (`engine.go:284`) to also read `data.taskKey`. In `handleCompletion`, resolve
        the live `token.<token>` pointer trying **both** keys (top-level `requestId` for systemOp,
        `data.taskKey` for userTask); advance on whichever resolves a live pointer. Drop (ack) when
        neither resolves (not Loom's event, or already advanced — idempotent by pointer presence).
        [Source: internal/loom/engine.go:281-322; docs/contracts/10-orchestration-surfaces.md#10.6]
  - [x] Ensure the `TaskCompleted` domain consumer exists: the engine reconciles one durable consumer
        per `completionDomains` entry; an onboarding pattern declaring `completionDomains:
        ["TaskCompleted"]` reconciles a `loom-TaskCompleted` consumer on `events.TaskCompleted.>`
        (existing 8.1 reconcile path — verify it handles a class with no sub-segments:
        `events.TaskCompleted` vs filter `events.TaskCompleted.>`; see Dev Notes "Filter-subject for a
        dot-free event class"). [Source: internal/loom/engine.go reconcileConsumers, runDomainConsumer;
        docs/contracts/10-orchestration-surfaces.md#10.5]

- [x] **Task 5 — the onboarding fixture bound ops (`SetName`/`SetPhone`/`SetAddress`)** (AC: #7)
  - [x] Author the three bound ops as simple aspect-writing ops over the subject identity, each
        task-authorizable (`authContext.task` path, §10.7) so its commit auto-completes the task.
        Place them per Dev Notes "Where the bound ops live" (a fixture/test DDL unless `orchestration-base`
        is the natural home). Each writes one aspect (`name`/`phone`/`address`) and emits a business
        event; the auto-complete path injects `TaskCompleted` on commit. [Source:
        docs/contracts/10-orchestration-surfaces.md#10.7; internal/processor/autocomplete.go]
  - [x] Provision the per-op grants so the subject identity, holding the task, is authorized to perform
        the bound op on itself (the §10.7 ephemeral-grant path). [Source:
        docs/contracts/10-orchestration-surfaces.md#10.7]

- [x] **Task 6 — onboarding e2e (end-to-end + long-wait restart)** (AC: #9)
  - [x] Extend `loom_e2e_test.go` (or a sibling `onboarding_e2e_test.go`): the fake Processor must now
        also handle **`CreateTask`** (mint `vtx.task.<suppliedTaskId>`, write the Contract #4 tracker)
        and the three **bound ops** (`SetName`/`SetPhone`/`SetAddress` → on commit, emit
        `TaskCompleted(taskKey)` as the auto-complete path would). Drive via a real `StartLoomPattern`;
        assert ordered `CreateTask` per step, the wait, user-bound-op → `TaskCompleted` → advance,
        exhaustion → `loom.patternCompleted`. [Source: internal/loom/loom_e2e_test.go fakeProcessor]
  - [x] **Long-wait restart sub-test:** drive to a userTask step, **restart the engine before** the
        user acts, then simulate the user bound op post-restart; assert exactly-once advance (no double
        `CreateTask`, no double advance), correlated on the durable `token.<taskKey>` pointer. Mirror
        `TestLoomE2E_MidRunRestartExactlyOnce`. [Source:
        docs/contracts/10-orchestration-surfaces.md#10.6 crash-safety]

- [x] **Task 7 — CONTRACT-AMENDMENT-REQUEST note for the §10.5/§10.6 discrepancies** (AC: #4)
  - [x] Write `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md` (or append to the existing one) flagging, for
        Andrew's ratification, the two doc/code drifts this story surfaces — **do NOT edit the frozen
        contract**: (a) the §10.5 example onboarding `completionDomains: ["identity"]` would never see
        `events.TaskCompleted` (the real userTask completion domain is `TaskCompleted`); (b) the §10.6
        table says the `TaskCompleted` body carries `taskId`, but the implemented event carries
        `taskKey` (`vtx.task.<id>`), and the §10.6 narrative implies a caller cannot control the task id
        while the write-ahead invariant requires it (resolved here by the optional `taskId` on
        `CreateTask`). [Source: CLAUDE.md "Frozen contracts"; docs/contracts/10-orchestration-surfaces.md#10.5, #10.6]

## Dev Notes

### Architecture patterns & constraints

- **This story is the `userTask` seam, not new engine machinery.** The pattern source, trigger
  consumer, per-domain completion consumers, the `loom-state` durable store + AtomicBatch transition,
  the command-outbox relay, and the deadline watcher are **all done** (Story 8.1, `done`). 8.2 adds:
  (1) accept `userTask` in `pattern.validate()`; (2) dispatch by `step.Kind` in `submitStep`;
  (3) submit `CreateTask` (not the bound op) for a userTask, with the **taskKey** as the write-ahead
  token; (4) correlate `TaskCompleted.data.taskKey`; (5) author the onboarding fixture bound ops;
  (6) the onboarding e2e incl. a long-wait restart. [Source: 8-1-loom-walking-skeleton.md; docs/components/loom.md]

- **The userTask token write-ahead seam (THE load-bearing decision).** Crash-safety invariant 1
  (§10.6) requires the `token.<token>` pointer be persisted **before** the side effect. For a userTask
  the §10.6 token is the **`taskKey`** of the task `CreateTask` creates. But `CreateTask` today mints
  `task_id = nanoid.new()` **internally** (`ddls.go:173`), so Loom cannot know the `taskKey` ahead of
  commit → it cannot write `token.<taskKey>` write-ahead. **Resolution (chosen): `CreateTask` accepts an
  optional caller-supplied `taskId`.** Loom derives a deterministic `taskId` from `(instanceId, cursor)`
  (a sibling of `deriveRequestID`), passes it in the `CreateTask` payload, mints
  `vtx.task.<taskId>` deterministically, and write-aheads `token.<vtx.task.<taskId>>` in the
  transition batch. Crash-retry re-submits the same `CreateTask` (same op `requestId`), which collapses
  on the Contract #4 tracker → no duplicate task. **Two distinct derivations** must stay separate and
  commented: the `CreateTask` op's **`requestId`** = the submission idempotency handle
  (`deriveRequestID(instanceId, cursor)`); the **token** = the completion-correlation handle
  (`vtx.task.<deriveTaskId(instanceId, cursor)>`). *(Alternative considered + rejected: a "Loom learns
  the taskKey from the `CreateTask` commit reply / `TaskCreated` event, then write-aheads after submit"
  design — rejected because it inverts invariant 1: the side effect would precede the pointer write, and
  a crash between them orphans the task. The caller-supplied-id design keeps the write-ahead atomic.)*
  [Source: docs/contracts/10-orchestration-surfaces.md#10.6 invariant 1; packages/orchestration-base/ddls.go:173;
  internal/loom/engine.go submitStep, deriveRequestID]

- **The completion event is `TaskCompleted` — the auto-complete is ALREADY BUILT; 8.2 consumes it.**
  When the user submits the bound op authorized via `authContext.task = T`, the commit path
  (`internal/processor/commit_path.go:369` `commitWithTaskAutoComplete` →
  `internal/processor/autocomplete.go`) injects `status→complete` (CAS-on-open) + a
  `TaskCompleted` event `{taskKey: T}` into the **same atomic batch**. **8.2 does not build this** — it
  consumes the `TaskCompleted` event. **Verify it works for the userTask path** (the bound op must be
  routed through the task auth path, `rp.Path == "task"`, so `taskKeyFromTaskPathDecision` returns the
  taskKey — i.e. the bound op is **task-authorized**, performed by the subject identity holding the
  task; AC#5/§10.7). If the bound op is performed under some other auth path, no auto-complete fires —
  that is the integration risk to prove in the e2e. [Source: internal/processor/commit_path.go:369;
  internal/processor/autocomplete.go; docs/contracts/10-orchestration-surfaces.md#10.6, #10.7]

- **`completionDomains` for onboarding = `["TaskCompleted"]`, NOT `["identity"]`.** `EventSubject`
  (`internal/processor/outbox/publisher.go:163`) maps class `TaskCompleted` → subject
  `events.TaskCompleted`; a **domain** is the first dot-free segment (§10.5), so the userTask
  completion domain is **`TaskCompleted`**. A pattern declaring `completionDomains: ["identity"]` would
  reconcile a `loom-identity` consumer on `events.identity.>` and **never see** `events.TaskCompleted`
  — the flow would wait forever (caught only by the deadline backstop, which userTask must not arm).
  **The default (`[subjectType]` = `["identity"]`) is wrong for a userTask pattern** — onboarding must
  declare `completionDomains: ["TaskCompleted"]` explicitly. The §10.5 *example* in the frozen contract
  shows `["identity"]`; that example is misleading for the userTask completion path → Task 7 raises a
  CAR note (do not edit the frozen contract). [Source: internal/processor/outbox/publisher.go:163;
  docs/contracts/10-orchestration-surfaces.md#10.5]

- **The §10.6 table says `taskId`; the code carries `taskKey`.** §10.6's userTask row reads "body
  carries `taskId`". The implemented `TaskCompleted` event (`autocomplete.go:96-101`,
  `ddls.go:304`) carries **`data.taskKey`** = the full `vtx.task.<id>` key. Loom correlates on the
  **`taskKey`** (the full key) — write-ahead `token.<vtx.task.<id>>` and resolve
  `data.taskKey` → that token. Do **not** strip to a bare id (the systemOp token is a bare requestId,
  but the userTask token is the full task key — both are just opaque token strings to the durable
  pointer; keep them whatever the completion event actually carries). [Source:
  internal/processor/autocomplete.go:96-101; docs/contracts/10-orchestration-surfaces.md#10.6]

- **Long wait = unbounded human wait; the cursor is durable.** A userTask step **must not** arm the
  bounded `deadline.<instanceId>` the systemOp path uses (`engine.go submitStep` passes
  `e.cfg.StepTimeout` → `transition`). If it did, `onDeadline` would fire while the human is still
  thinking, probe an absent tracker (the bound op hasn't been performed), find no outbox record, and
  **`fail`** the instance — a false-fail. So pass `deadlineTTL = 0` (no deadline) for a userTask step,
  **or** a far-future TTL well beyond any human window. The 8.1 `transition` already treats
  `deadlineTTL <= 0` as "disarm" — passing 0 writes no live deadline, which is exactly the unbounded
  wait we want. The cursor + `token.<taskKey>` are durable in `loom-state`, so a restart mid-wait
  resumes correctly. (The 24h idempotency-horizon "extended-dedupe" noted in `docs/components/loom.md`
  is **not** required for 8.2: the durable cursor + durable token pointer satisfy the long-wait AC; the
  redelivery dedupe is the per-domain consumer's ack floor + pointer presence, neither of which has a
  24h horizon for a userTask whose token stays live until the user acts. Flag if a reviewer disagrees.)
  [Source: internal/loom/engine.go submitStep; internal/loom/state.go transition; docs/components/loom.md "Long waits"]

- **P2 / module boundary hold.** Loom submits `CreateTask` and the bound ops are submitted by the
  *user* (the test simulates the user) — Loom never performs the bound op, never writes Core KV, never
  publishes to `core-events`. `internal/loom` stays `substrate/*`-only (AC#8). [Source:
  docs/components/loom.md#Principles; internal/loom/boundary_test.go]

### Resolving the bound op to its meta-vertex (`forOperation`)

`CreateTask`'s `forOperation` is the **`vtx.meta.<opId>`** of the bound operation (e.g. `SetName`). The
pattern step carries the op **name** (`step.operation`), not the meta-vertex key. Loom must map the op
name → its meta-vertex key. Options the dev-story must pick (and document the choice):
- **(a) the step already carries the meta key.** If a pattern's `userTask` step `operation` is authored
  as the full `vtx.meta.<opId>` (the same way a step's operation reference resolves elsewhere), Loom
  passes it straight through. Check how systemOp `step.operation` is used today (`submitStep` uses it as
  the `OperationType` op name, not a meta key) — so this likely needs a name→meta resolution.
- **(b) Loom resolves name → meta via a Core-KV read** (a known-key read of the op's meta-vertex by
  canonical name, like other components resolve DDL/op metas). Loom may read Core KV (it already does in
  `trackerExists`). Prefer this if the step carries an op **name**.
Pick the one consistent with how the rest of the codebase resolves an op name to its `vtx.meta.<opId>`;
do **not** invent a third mechanism. [Source: internal/loom/engine.go submitStep, trackerExists;
docs/contracts/10-orchestration-surfaces.md#10.5]

### Filter-subject for a dot-free event class

The 8.1 per-domain consumer filters `events.<domain>.>` (`runDomainConsumer`,
`engine.go:273`). For domain `TaskCompleted` the completion subject is exactly `events.TaskCompleted`
(no trailing segment), so a `events.TaskCompleted.>` filter **may not match** a subject with no tokens
after `TaskCompleted`. Verify the JetStream filter semantics: `events.TaskCompleted.>` requires ≥1
token after `TaskCompleted`; `events.TaskCompleted` is an exact match. The fix is likely to filter
`events.<domain>` (exact) **or** `events.<domain>.>` depending on whether the class has sub-segments —
or to subscribe `events.<domain>>`-style. **Prove the onboarding e2e actually delivers
`events.TaskCompleted` to the `loom-TaskCompleted` consumer** — this is the single most likely silent
failure of the story (the flow would wait forever). Adjust the filter-subject construction in
`runDomainConsumer`/`reconcileConsumers` if dot-free domains don't match. [Source:
internal/loom/engine.go:269-279; internal/processor/outbox/publisher.go:163]

### Where the bound ops live

`SetName`/`SetPhone`/`SetAddress` write aspects on a `vtx.identity.<id>` subject. The identity DDL
(`packages/identity-domain/ddls.go`) owns `CreateUnclaimedIdentity`/`UpdateIdentityState`/
`ClaimIdentity` — it does **not** have a generic profile-aspect setter. Do **not** bloat
`identity-domain` or `orchestration-base` with onboarding-specific ops unless they are genuinely
foundational. Prefer a **fixture/test DDL** authored in (or alongside) the e2e harness — the bound ops
are onboarding-flow-specific fixture data, mirroring how 8.1's e2e installs a fixture pattern + fake
Processor. Per `docs/components/loom.md` ("Engine vs package"): specific flows + their step→operation
bindings are **package data** (`lease-signing` / an `identity` package), not engine code; an onboarding
fixture DDL in the test is the right scope for 8.2. Keep them minimal: one aspect write each, emit a
business event, be task-authorizable. [Source: packages/identity-domain/ddls.go; docs/components/loom.md "Engine vs package"]

### Source tree components to touch

- **EDIT:** `internal/loom/pattern.go` (accept `userTask` in `validate`),
  `internal/loom/engine.go` (`submitStep` dispatch by kind; `handleCompletion`/`eventBody` correlate by
  `taskKey` too; no-deadline for userTask), possibly `internal/loom/actuator.go` (`buildOutbox` already
  generic — likely no change), `packages/orchestration-base/ddls.go` (`CreateTask` optional `taskId`).
- **NEW:** the onboarding fixture bound ops (`SetName`/`SetPhone`/`SetAddress`) — a fixture/test DDL;
  `internal/loom/onboarding_e2e_test.go` (or extend `loom_e2e_test.go`); `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md` note.
- **READ-ONLY references (do NOT import into `internal/loom`):**
  `internal/processor/autocomplete.go` (the auto-complete path — already built),
  `internal/processor/commit_path.go:369` (`commitWithTaskAutoComplete` wiring),
  `internal/processor/outbox/publisher.go:163` (`EventSubject`, the class→subject map),
  `packages/orchestration-base/ddls.go` (`CreateTask` / task DDL),
  `docs/contracts/10-orchestration-surfaces.md` §10.5/§10.6/§10.7.
- **DO NOT re-touch (8.1, stable):** `internal/loom/state.go` (transition/AtomicBatch),
  `internal/loom/actuator.go` relay, `internal/bootstrap/primordial.go` (`loom-state` bucket),
  `scripts/verify-kernel.go`. The `identity:loom` actor + `loom-state` bucket are already provisioned.

### Testing standards

- Go test packages: `go test ./internal/loom/... ./packages/orchestration-base/... ./internal/processor/...`
  plus the onboarding e2e. Honor the repo verification gates: `go build ./...`, `make vet`,
  `golangci-lint run ./...`, `make verify-kernel`, `make test-bypass` (Gate 2 BLOCKED),
  `make test-capability-adversarial` (Gate 3 DEFENDED). [Source: CLAUDE.md#Workflow]
- The onboarding e2e is the load-bearing proof: ordered `CreateTask` per step → wait → simulated user
  bound op → `TaskCompleted` → advance → exhaustion → `loom.patternCompleted`, **plus** the long-wait
  restart-before-user-acts exactly-once assertion. Mirror the 8.1 harness (embedded NATS/JetStream, the
  `fakeProcessor` envelope shape that publishes the full Event with top-level `requestId` + `payload`,
  and `TestLoomE2E_MidRunRestartExactlyOnce`). The fake Processor must additionally honor `CreateTask`
  (mint `vtx.task.<suppliedTaskId>` + tracker) and the bound ops (emit `TaskCompleted{taskKey}` on
  commit). [Source: internal/loom/loom_e2e_test.go]

### Comment policy (binding — CLAUDE.md)

- **No history/changelog narration in code.** No `// Story 8.2 …`, `// was systemOp-only …`,
  `// now also handles userTask …`. Comments describe what the code does *now*. git blame + the commit
  message are the record. (Most-violated rule — do not reintroduce it.)
- **Key-shape conventions (Contract #1):** aspects 4-segment `vtx.<type>.<id>.<localName>`; links
  6-segment `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>` reading "source <relation> target" (the task is
  the later-arriving SOURCE: `task assignedTo identity`, `task forOperation meta`, `task scopedTo
  <subject>`); meta-vertices `vtx.meta.<NanoID>`; bucket names dash-named, no dots (`loom-state`).

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story-8.2] — story statement, AC,
  deps (8.1, 7.2), FR26.
- [Source: _bmad-output/implementation-artifacts/8-1-loom-walking-skeleton.md] — the engine machinery
  this story builds on (Dev Agent Record + File List); systemOp-only baseline.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.5] — pattern def; `userTask` kind (links
  assignedTo/forOperation/scopedTo → subject); `completionDomains`; the (misleading) onboarding example.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.6] — step completion & correlation; userTask
  token = `taskId`/`taskKey`; `TaskCompleted` signal; auto-complete-on-commit; crash-safety invariants.
- [Source: docs/contracts/10-orchestration-surfaces.md#10.7] — ephemeral task grants / auth (UNCHANGED;
  the bound op is task-authorized → auto-completes the task).
- [Source: docs/components/loom.md] — core model, execution loop (userTask branch), "Long waits".
- [Source: internal/loom/engine.go] — `submitStep`, `handleCompletion`/`eventBody`, `complete()`,
  `reconcileConsumers`/`runDomainConsumer`, `deriveRequestID`, deadline arming.
- [Source: internal/loom/pattern.go] — `Step.Kind`, `validate()` (currently rejects userTask),
  `Domains()`/`CompletionDomains`.
- [Source: internal/loom/state.go] — `transition` (AtomicBatch; `deadlineTTL<=0` disarms), token store.
- [Source: internal/processor/autocomplete.go] — the **already-built** auto-complete-on-commit:
  `TaskCompleted{taskKey}` injected into the bound op's batch (8.2 consumes this).
- [Source: internal/processor/commit_path.go:369] — `commitWithTaskAutoComplete` (the wiring point;
  `rp.Path == "task"` gate).
- [Source: internal/processor/outbox/publisher.go:163] — `EventSubject` (class `TaskCompleted` →
  `events.TaskCompleted` → domain `TaskCompleted`).
- [Source: packages/orchestration-base/ddls.go] — `task` DDL / `CreateTask` (mints its own nanoid today;
  Task 1 adds the optional `taskId`).
- [Source: packages/identity-domain/ddls.go] — identity DDL (no profile-aspect setter; the bound ops are
  new fixture ops).
- [Source: internal/loom/loom_e2e_test.go] — the 8.1 e2e harness (fake Processor, restart-exactly-once
  test) to mirror.

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (Amelia / bmad-dev-story sub-agent).

### Debug Log References

- Onboarding e2e first run failed: userTask step false-failed via the deadline path. Root cause: a
  userTask passes `deadlineTTL=0` → `transition` writes a `Delete` on `deadline.<instanceId>`, whose
  empty-body delete marker reaches the deadline watcher. For systemOp that disarm is a no-op (status
  != running on terminal), but a userTask leaves the instance `running` with a `vtx.task.` token, so
  `onDeadline` ran the probe against the wrong keys (it probes `pendingToken`, but a userTask's outbox/
  tracker are keyed on the CreateTask requestId) → false `fail`. Fixed by guarding `onDeadline`: a
  pending token with the `vtx.task.` prefix is an unbounded human wait → no-op (AC#6).
- Second failure: cursor parked at step 0 but the bound-op `TaskCompleted` never advanced it. Root
  cause: the published core-events Event envelope nests business fields under **`payload`** (Processor
  `BuildEventList` maps `EventSpec.Data` → `Event.payload`), but `eventBody` read `data.taskKey`.
  Fixed to read `payload.taskKey`. (Surfaced a real §10.6 doc drift — Request 7 in the CAR.)

### Completion Notes List

Implemented the userTask seam on top of the 8.1 systemOp engine. All 7 tasks + 22 subtasks complete.

- **Task 1 (`CreateTask` optional `taskId`).** `packages/orchestration-base/ddls.go`: `bare_nanoid_or_mint`
  helper — present `taskId` (validated as a bare NanoID, no dots) used verbatim, absent → `nanoid.new()`.
  Added optional `taskId` to InputSchema + FieldDescription (NOT required — admin/manual callers
  unaffected). Endpoint/no-orphan validation unchanged. Script tests: supplied-verbatim, mint-on-absent,
  dotted-rejected, empty-rejected.
- **Task 2 (`pattern.validate` accepts userTask).** `internal/loom/pattern.go`: accept
  systemOp|userTask; reject unknown kinds; guards still rejected (Story 8.3). Updated pattern_test.
- **Task 3 (dispatch by kind).** `internal/loom/engine.go` `submitStep` → `submitSystemOp` (unchanged
  8.1 path) | `submitUserTask`. The userTask submits `CreateTask` with `{assignee, forOperation,
  scopedTo, expiresAt, taskId}` (assignee=scopedTo=subject, §10.5); write-ahead token = the `taskKey`
  (`vtx.task.<deriveTaskID(instanceId,cursor)>`), distinct from the CreateTask op's requestId
  (`deriveRequestID`). `deriveTaskID` is a namespaced sibling in `token.go` (disjoint from requestId).
  No bounded deadline for a userTask (`deadlineTTL=0`) — unbounded human wait (AC#6).
- **forOperation resolution (Dev Notes option b).** Extended the pattern source CDC (`source.go`,
  already subscribing `vtx.meta.>`) to index non-pattern op meta-vertices by `data.operationType` →
  `vtx.meta.<opId>`. `submitUserTask` resolves the bound-op name via `source.opMetaKey`. No Core-KV
  scan, no new key shape — same CDC the engine already runs (not a third mechanism).
- **Task 4 (completion correlates by taskKey + requestId).** `handleCompletion` now tries both
  structural keys (top-level `requestId`, `payload.taskKey`); the durable pointer decides. Per-domain
  consumer filter changed to the two-subject set `{events.<domain>, events.<domain>.>}` so a dot-free
  class like `TaskCompleted` (subject `events.TaskCompleted`) is delivered — required adding
  `FilterSubjects` to `substrate.DurableConsumerConfig` (with a mutual-exclusion guard + substrate
  tests proving dot-free + dotted match, and the negative that a lone `.>` filter misses the dot-free
  subject).
- **Task 5/6 (onboarding fixture + e2e).** Bound ops `SetName/SetPhone/SetAddress` are fixture
  concepts: the e2e fake Processor honours `CreateTask` (mint `vtx.task.<suppliedTaskId>` + Contract #4
  tracker, no completion event) and the bound ops (on commit emit `TaskCompleted{payload.taskKey}` read
  from `authContext.task` — simulating the real auto-complete, proving the task-auth integration). Two
  e2e tests: full flow (ordered CreateTask → wait → user bound op → advance → exhaustion →
  `loom.patternCompleted`) and a long-wait restart BEFORE the user acts (exactly-once: one CreateTask
  per step across both engine generations, correlated on the durable `token.<taskKey>` pointer).
- **Task 7 (CAR note).** Appended a "Story 8.2" section to `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md`:
  R6 (§10.5 example `completionDomains:["identity"]` is misleading → `["TaskCompleted"]`), R7 (§10.6
  carries `taskKey` under `payload`, not a bare top-level `taskId`), R8 (invariant 1 needs the
  caller-supplied `taskId`). Frozen contract NOT edited.

**Verification (all gates):** `go build ./...` OK; `make vet` OK; `golangci-lint run ./...` 0 issues;
`make verify-kernel` ALL PASSED (incl. loom-state + AllowAtomicPublish); `make test-bypass` Gate 2 all
BLOCKED/PASSED; `make test-capability-adversarial` Gate 3 4/4 cleared (3 DEFENDED, 1 ACCEPTED-WINDOW);
`go test ./internal/loom/... ./packages/orchestration-base/... ./internal/processor/... ./internal/substrate/...`
PASS (processor + processor/outbox flake only under high parallelism — embedded-NATS JetStream store
creation; both pass with `-p 1`; unrelated to this change — no processor/outbox code touched). All
touched files gofmt-clean. Module boundary `TestModuleBoundary_NoRawNATS`/`OnlySubstrate` stay green.

**Deviations from rulings:** none. forOperation resolution uses the CDC-driven in-memory index
(faithful to option b "Loom resolves name → meta from Core KV it already reads" without a scan or new
key shape); flagged for review. The discovered `payload`-vs-`data` nesting (a third drift beyond the
two the story named) is folded into the CAR as Request 7.

**For Winston to scrutinise in review:**
1. The `onDeadline` userTask guard (`isUserTaskToken`, `vtx.task.` prefix). It relies on the token
   prefix to mean "unbounded wait." Confirm a systemOp token can never collide with that prefix (it is
   a bare requestId NanoID, no dots) and that a mixed userTask→systemOp transition (which arms a real
   deadline via PUT) is unaffected.
2. The op-meta CDC index in `source.go` indexes ANY non-pattern `vtx.meta.*` carrying
   `data.operationType` by operationType, last-writer-wins on duplicate operationType (mirrors the
   DDLCache "keep first-seen" only loosely — here it's last-seen). For Phase 2 fixtures this is fine;
   confirm acceptable, or tighten to first-seen + a warn.
3. `userTaskGrantTTL = 30d`. Generous-by-design (AC#6) but a magic constant — confirm the horizon.
4. The e2e simulates auto-complete in the fake Processor (the real path is in
   `internal/processor/autocomplete.go`, exercised in the processor package). The integration claim
   "the bound op routes through `rp.Path == "task"` so auto-complete fires" is asserted structurally
   (the bound op carries `authContext.task`), not through the real auth pipeline — consistent with the
   8.1 fake-Processor harness, but worth a reviewer's eye on whether a real-pipeline e2e is wanted.

### Fix-forward (post-review, Winston-adjudicated 2026-06-07)

Four reviewer findings fixed in the working tree:

- **FIX 1 (HIGH) — userTask creation-deadline + task-vertex probe backstop.** `submitUserTask` no longer
  disarms the deadline; it arms a **bounded** `CreateTaskTimeout` (new `Config` field, default 60s, same
  `<1s` clamp as `StepTimeout`; `cmd/loom/main.go` does not set it → default applies). `onDeadline` routes
  a `vtx.task.` token to the new `onUserTaskDeadline`: GET `vtx.task.<taskId>` from Core KV — **present**
  → disarm (new `state.disarmDeadline`, guarded on the key being present so its own DEL marker does not
  re-fire the watcher into a loop) and stay running (unbounded human wait); **absent** → probe the
  `CreateTask` op's tracker / outbox exactly like the systemOp path (re-arm) → else `fail("CreateTask
  rejected")` + Warn. New `taskVertexExists` Core-KV GET helper alongside `trackerExists` (READ-only;
  module boundary unchanged). This is the §10.6 systemOp deadline+probe analog for task creation: a
  rejected/lost `CreateTask` now fails the instance instead of wedging it forever.
- **FIX 2 (diagnostic) — load-time warn for a misconfigured userTask pattern.** `source.go` `dispatchSpec`
  emits a loud `logger.Warn` when a pattern has a userTask step and its **effective** completionDomains
  (after the `[subjectType]` default) omits `TaskCompleted` (new `Pattern.userTaskCompletionUnobservable`).
  The pattern is NOT rejected (a future userTask completion domain could differ). Unit test
  `TestUserTaskCompletionUnobservable` covers the four condition cases.
- **FIX 3 (house rule) — removed change-narrating / story-AC-phase comments.** Stripped `AC#6`/`AC #2`/
  `Story 7.3`/`Story 8.3` narration from `engine.go`, `token.go`, `state.go`, `pattern.go`, keeping
  `§10.x` contract citations. The new FIX 1 code introduces none.
- **FIX 4 (hardening comments) — pinned implicit invariants.** `token.go` (near `deriveRequestID`):
  systemOp tokens are bare NanoIDs (no dots) so they can never carry the `vtx.task.` prefix
  `isUserTaskToken` keys on. `engine.go` (near `correlationKeys`): trying `requestId` before
  `payload.taskKey` is safe because both are unguessable NanoIDs (a `TaskCompleted`'s top-level requestId
  is the user's bound-op id, never a live Loom systemOp token).

New tests: `TestOnboardingE2E_RejectedCreateTaskFails` (rejected `CreateTask` → creation-deadline fires →
probe finds no task/tracker/outbox → instance `failed` + `loom.patternFailed`; does NOT hang — the
load-bearing assertion) and `TestOnboardingE2E_CreatedTaskDisarmsForUnboundedWait` (CreateTask commits,
human waits past `CreateTaskTimeout` → deadline fires → probe finds the task vertex → disarms → instance
stays `running` at cursor 0; user finally acts → cursor advances exactly once → exhaustion; exactly one
CreateTask per step). CAR Request 9 records the §10.6 userTask-creation deadline+probe for ratification.

### Event-domain model course-correction (Andrew-ratified 2026-06-07)

Folded the event-domain model into this story's commit (the 8.2 review surfaced that `TaskCompleted`
had no domain; the whole event taxonomy was domain-less). Every core-event class is now
`<domain>.<eventName>` (lowerCamelCase event name), enforced at the Processor.

- **Event document gains `domain`.** `internal/processor/step7_events.go`: `Event` struct has a `Domain`
  field; `BuildEventList` sets it from the class's first segment (single source of truth = the class).
- **Enforcement (turned on after the rename).** Step 7 (`BuildEventList`) rejects any class that is not
  `<domain>.<eventName>` (no dot, empty domain, or empty eventName). Belt-and-braces in `EventSubject`
  (`outbox/publisher.go`): a dot-free class routes to `events._nodomain` (loud, not silent). Proven by
  `TestBuildEventList_RejectsDotFreeClass`; the full suite re-run was the safety net that caught every
  straggler (4 found: `step8_commit_test`, `step8_e2e_test`, `fr19_northstar_test`, plus assertion files).
- **Renamed every production event class** (the rename map below).
- **8.2 reconciliation.** Onboarding `completionDomains: ["TaskCompleted"]` → `["orchestration"]`; the
  e2e fake Processor emits `orchestration.taskCompleted`/`orchestration.taskCreated`; `pattern.go`'s
  `taskCompletedDomain="TaskCompleted"` → `userTaskCompletionDomain="orchestration"`. **Reverted** the
  dot-free `FilterSubjects` two-subject workaround: `internal/substrate/consumer.go` +
  `consumer_test.go` restored to HEAD (single `FilterSubject` primitive); `engine.go` `runDomainConsumer`
  back to a single `FilterSubject: "events.<domain>.>"` (enforcement makes dot-free classes impossible,
  so `events.<domain>.>` always matches).
- **Contracts amended** (frozen, via revision entries): Contract #3 §3.4 (class MUST be
  `<domain>.<eventName>`; Event carries `domain`; revision 2026-06-07 + new revision-history section);
  Contract #10 §10.5/§10.6 (onboarding `["orchestration"]`; userTask correlates `payload.taskKey` via
  `orchestration.taskCompleted`; invariant-1 engine-supplies-taskId; R9 creation-deadline+probe;
  revision 2026-06-07). CAR R6–R9 marked **ratified** (superseded by the broader model).
- **Docs updated:** `docs/components/loom.md` (userTask completes on `orchestration`; long-wait model),
  `docs/components/processor.md` (step 7 enforcement + `events.<domain>.<eventName>` subjects).

**Rename map (production):**
- `identity.created`, `identity.claimed`, `identity.stateChanged` (`packages/identity-domain/ddls.go`);
  `identity.merged` (`packages/identity-hygiene/ddls.go`).
- `orchestration.taskCreated`, `orchestration.taskReAssigned`, `orchestration.taskCompleted`,
  `orchestration.taskCancelled` (`packages/orchestration-base/ddls.go` + `internal/processor/autocomplete.go`'s
  injected class).
- `rbac.roleCreated`/`roleUpdated`/`roleTombstoned`/`roleAssigned`/`roleRevoked`,
  `rbac.permissionCreated`/`permissionUpdated`/`permissionTombstoned`/`permissionGranted`/`permissionRevoked`
  (`packages/rbac-domain/ddls.go`).
- `package.installed`, `package.uninstalled` (`internal/bootstrap/install_ddl.go`).
- `meta.vertexCreated`, `meta.vertexUpdated`, `meta.vertexTombstoned` (`internal/bootstrap/meta_ddl.go`).
- `loom.patternStarted`/`patternCompleted`/`patternFailed` — UNCHANGED (already domain-shaped).
- Test fixtures: `loftspace.leaseApproved`, `loftspace.bookCreated`, `loftspace.bookRegistered`,
  `health.probe`, and all `*_test.go` assertion literals.

**Files touched by the course-correction (in addition to the 8.2 File List below):**
- `internal/processor/step7_events.go` (Event.Domain + eventDomain + step-7 enforcement)
- `internal/processor/outbox/publisher.go` (EventSubject dot-free guard → `events._nodomain`)
- `internal/processor/step7_events_test.go`, `step8_commit_test.go`, `step8_e2e_test.go`,
  `autocomplete.go`, `autocomplete_test.go`, `autocomplete_integration_test.go`, `step5_execute_test.go`,
  `outbox/publisher_test.go`
- `packages/identity-domain/ddls.go`, `packages/identity-hygiene/ddls.go`,
  `packages/orchestration-base/ddls.go`, `packages/rbac-domain/ddls.go` (+ their `*_test.go` assertions)
- `internal/bootstrap/install_ddl.go`, `internal/bootstrap/meta_ddl.go`,
  `internal/bootstrap/tombstone_metavertex_test.go`
- `internal/hellolattice/hellolattice_test.go`, `internal/bypass/capadv_projection_lag_test.go`,
  `internal/aiagent/fr19_northstar_test.go`
- `internal/loom/pattern.go`, `pattern_test.go`, `engine.go`, `source.go`, `token.go`,
  `onboarding_e2e_test.go`, `loom_e2e_test.go`
- `internal/substrate/consumer.go`, `consumer_test.go` (**reverted** the FilterSubjects workaround to HEAD)
- `docs/contracts/03-mutation-batch-event-list.md`, `docs/contracts/10-orchestration-surfaces.md`,
  `docs/components/loom.md`, `docs/components/processor.md`, `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md`

### File List

- `packages/orchestration-base/ddls.go` (modified — CreateTask optional taskId)
- `packages/orchestration-base/task_script_test.go` (modified — taskId tests)
- `internal/loom/pattern.go` (modified — accept userTask; FIX 2 userTaskCompletionUnobservable; FIX 3)
- `internal/loom/pattern_test.go` (modified — userTask/unknown-kind/guard cases; FIX 2 condition test)
- `internal/loom/engine.go` (modified — submitStep dispatch, submitUserTask, handleCompletion two-key
  correlation, two-subject domain filter, onDeadline; FIX 1 CreateTaskTimeout config + onUserTaskDeadline
  + taskVertexExists; FIX 3/FIX 4 comments)
- `internal/loom/state.go` (modified — FIX 1 disarmDeadline helper; FIX 3)
- `internal/loom/source.go` (modified — op-meta operationType→metaKey CDC index; FIX 2 load-time warn)
- `internal/loom/token.go` (modified — deriveTaskID namespaced sibling; FIX 3/FIX 4 invariant comment)
- `internal/loom/token_test.go` (modified — deriveTaskID disjointness test)
- `internal/loom/onboarding_e2e_test.go` (new — onboarding e2e + long-wait restart; FIX 1 reject→fail +
  disarm→unbounded-wait e2e)
- `internal/loom/loom_e2e_test.go` (modified — fakeProcessor honours CreateTask + bound ops)
- `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md` (modified — Story 8.2 CAR section + Request 9 userTask
  creation deadline+probe)
- `internal/substrate/consumer.go` (reverted to HEAD — the FilterSubjects workaround removed under the
  event-domain course-correction; enforcement makes dot-free classes impossible)
- `internal/substrate/consumer_test.go` (reverted to HEAD — FilterSubjects tests removed)
