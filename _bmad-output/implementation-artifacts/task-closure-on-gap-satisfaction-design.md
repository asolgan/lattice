# Resolved — close an assignTask task when its gap is satisfied

**Status:** ✅ Resolved (Steward + Andrew, 2026-06-25). The originally-proposed Weaver
core-orchestration change is **RETRACTED** as over-built — the platform already auto-completes the
task on the correct path; the real gap was an app-level one and is fixed with a small change.

## The correct mechanism (it was already there)

The Processor **auto-completes a task when the gap-closing op is submitted *via that task's ephemeral
grant*** — the "task path" (Contract #10 §10.7). When an op's authorization resolves on
`authContext.task = T` (the actor's right to submit the op came from the userTask grant, not a standing
permission), the commit path injects T's completion — `status → complete` +
`orchestration.taskCompleted{taskKey:T}` — into the **same atomic batch**, CAS-conditional on
`status == open` (so it never double-completes or resurrects a cancelled task). See
`internal/processor/autocomplete.go` (`readTaskAutoCompletion` / `injectTaskAutoCompletion`) and
`taskKeyFromTaskPathDecision`. `CompleteTask(taskKey)` is, per §10.7, "retained only as an explicit
admin / out-of-band completion path."

**So a task only lingers open in the *no-hint* case:** the gap-closing op is authorized by a **standing
permission** rather than the task's ephemeral grant, so `authContext.task` is empty and nothing is
injected.

## Why the lingering task was observed live

The `loftspace-app` is structurally always in the no-hint case **by construction**: `cmd/loftspace-app/main.go`
— it submits every op as the **primordial admin actor** ("submits operations on the applicant's
behalf"), which holds standing rights to `SignLease` / `RecordIdentityPII`. So completing a task from
the Tasks tab authorized on the admin's permanent grant, never the task path → no auto-complete → the
task stayed `open`. In the *proper* per-identity flow (an applicant authorized **only** via the task
grant — the deferred read-path-auth / Personal-Lens work), the existing auto-complete closes it for free,
nothing to build.

For these userTasks the bound op **is** the sole gap-closer (`.signature` is written only by
`SignLease`), so whoever closes the gap either uses the task grant (auto-complete) or is the admin tool
(which should submit the explicit `CompleteTask`). A Weaver-side reconciliation would only add value for
a gap closed by an op *unrelated* to its task — which does not occur here. Hence the retraction.

## The fix (small, app-level, contract-sanctioned)

`cmd/loftspace-app/web/app.js` — after the Tasks-tab completion submits the bound op and it commits,
the FE submits an explicit **`CompleteTask(taskKey)`** (`class:"task"`, `reads:[taskKey]`,
`payload:{taskKey}`) — the §10.7 out-of-band path. The app already has `taskKey` from the `my-tasks`
row. Best-effort: a benign rejection (the task already closed) or a transport error is logged, not
surfaced, because the gap-closing op already succeeded. No Weaver change, no `§10.8` contract question,
no frozen-contract touch.

## Retracted alternative (for the record)

The earlier proposal extended Weaver's `clearClosedMarks` to derive the episode task id from the §10.3
mark's revision and dispatch an idempotent `CompleteTask` directOp on gap-close. Mechanically sound, but
it duplicates the Processor's existing auto-complete for a case the trusted-tool app can resolve
directly, and it would have introduced a gap-*close*-driven dispatch into the §10.8 action table (a
frozen-contract change). Not worth it. If a future need arises — a gap closable by an op with **no**
task association — revisit then.
