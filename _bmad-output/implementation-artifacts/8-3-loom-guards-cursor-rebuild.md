# Story 8.3: Pure on/off guards + cursor rebuild

Status: done

## Story

As a flow author,
I want steps to carry an optional on/off guard that skips already-satisfied steps,
so that one pattern serves both "collect" and "verify-info," and the cursor is crash-recoverable.

## Acceptance Criteria

From `_bmad-output/planning-artifacts/epics/phase-2-epics.md` § Story 8.3 (source of truth for the
*business* requirement; **`docs/contracts/10-orchestration-surfaces.md` §10.5/§10.6 is the FROZEN,
binding spec for guard grammar and crash-safety — where the epics text is less specific, the
contract governs and is transcribed below**).

1. **`Step.Guard json.RawMessage` (already present, `internal/loom/pattern.go:23`) is interpreted.**
   `validate()` currently rejects any guarded step unconditionally
   (`internal/loom/pattern.go:134-136`, `"guards are out of scope"`). That rejection is replaced by
   real grammar validation at pattern-load/validate time:
   - A step with **no `guard` field** (`len(s.Guard) == 0`) always runs — unchanged, no validation
     needed.
   - A step **with** a `guard` must parse as one of the declarative grammar shapes (§10.5, transcribed
     in Dev Notes below: `absent`/`present`/`equals` atoms, `allOf`/`anyOf`/`not` composition). A
     malformed shape (unknown key, wrong types, empty composition list, path that isn't
     `subject.data.<field>` or `subject.<aspect>.data.<field>`) is a **validate() error** — the
     pattern is rejected wholesale, same doctrine as today's unknown-`kind` rejection (a
     half-understood pattern never partially executes).
   - A guard shaped `{"reads": [...], "starlark": "..."}` is **recognized as the reserved Starlark
     escape hatch and rejected with a precise "starlark guards are reserved, not yet supported"
     error** — NOT treated as a generic malformed-guard error. Building the Starlark evaluator is
     OUT OF SCOPE for this story (§10.5: "the shared pure-evaluator extraction lands only when the
     first Starlark guard is authored").

2. **Guard evaluation at step-entry skips false-guarded steps without creating a task or submitting
   an op.** In the engine's transition path (`internal/loom/engine.go`, the `advance`/`submitStep`
   flow):
   - At step-entry, **before** dispatching to `submitUserTask`/`submitSystemOp`, the engine evaluates
     the current step's guard (absent guard = true = "run").
   - **Guard false** → the step is **skipped**: no `CreateTask`, no systemOp submitted, no
     `token.<token>` pointer written, no `outbox.` record. The cursor advances to the next step and
     guard evaluation **repeats** — a run of consecutive false guards skips multiple steps within the
     same transition. If the cursor runs off the end of `pattern.Steps` this way, the pattern
     **completes** (`opCompletePattern`), exactly as if the final real step had just completed.
   - **Guard true** → the step runs exactly as it does today (submit/park, write-ahead token, arm
     deadline).
   - This applies at **every** cursor-advance call site: step 0 (the trigger path,
     `handleTrigger` → `submitStep`) and every `advance()` call from `handleCompletion` /
     `onDeadline`'s recovery paths.

3. **Hydration: the engine reads the subject root + the aspects referenced by the current step's
   guard paths from Core KV at step entry**, per-evaluation (no caching layer this story — note
   future perf work in Dev Notes). A guard with no `subject.<aspect>.data.*` paths reads only the
   subject root vertex; a guard with `{allOf: [{absent: "subject.profile.data.name"}, ...]}` reads
   the subject root AND the `<subjectKey>.profile` aspect. Guards read **ONLY the subject + its
   aspects** — no link-walking (a guard needing related state is a Weaver signal, out of scope).

4. **Path-resolution semantics mirror Refractor's `resolveProperty`/aspect-navigation
   (`internal/refractor/ruleengine/full/executor.go:1270-1290`), implemented as a small,
   loom-local resolver** (loom imports only `substrate/*` per 8.1 AC#8 — do NOT import
   `internal/refractor/*`, do NOT move Refractor code). The loom-local resolver is far smaller than
   Refractor's general property navigation: it handles exactly the two path shapes the §10.5 grammar
   allows —
   - `subject.data.<field>` — read `<field>` from the subject root vertex's own `data` envelope.
   - `subject.<aspect>.data.<field>` — point-read the aspect key `<subjectKey>.<aspect>` from Core KV
     (mirroring `fetchNode(nr.key + "." + key)`,
     `internal/refractor/ruleengine/full/executor.go:1281-1289`) and read `<field>` from its `data`.
   - A missing root vertex, missing aspect, or missing field all resolve to "absent" per the pinned
     semantics below — they are NOT validate-time or evaluate-time errors (absence is exactly what
     `{absent: ...}` is testing for).

5. **Pinned absence semantics (binding, §10.5, transcribed in full in Dev Notes):**
   - `absent` = the path resolves to **null, missing, a soft-deleted aspect (`isDeleted: true`), OR
     (for strings) empty-after-trim**.
   - `present` = NOT absent.
   - An empty-string-after-trim is **absent**. `"0"` / `false` / `0` (numeric zero) are **present**
     (NOT absent — only emptiness/nullness/missingness/soft-delete count as absent, never
     "falsy").
   - `equals: {path, value}` — `present(path) AND` the resolved value at `path` equals `value`
     (type-aware: numbers/strings/bools compare by value; an absent path never equals anything,
     including `null`/empty-string `value`).
   - `allOf`/`anyOf`/`not` compose sub-guards into ONE boolean — NOT branching. `allOf([])` /
     `anyOf([])` are validate-time errors (empty composition list, AC#1).

6. **Disaster-recovery cursor rebuild (the epics "rebuild" AC), honoring §10.6 invariant 2.** With
   `loom-state` **lost entirely** (not a normal restart — normal restart resumes from the durable
   `instance.<id>` cursor + `token.` index, unchanged), a re-submitted `StartLoomPattern` for an
   instance whose subject has already progressed must **not** restart from step 0 and must not
   double-submit any in-flight step:
   - The recovery vehicle is `handleTrigger`'s existing idempotency path
     (`internal/loom/engine.go:374-381`): `getInstance` returns nil (loom-state is gone) →
     `createInstance` at cursor 0 → **before** calling `submitStep` for step 0, the engine evaluates
     guards forward from cursor 0 against current Core KV state, skipping every step whose guard is
     **false** (per AC#2's repeat-on-false-guard loop), landing the cursor on the first step whose
     guard is **true** (or completing if all guards are false).
   - **§10.6 invariant 2 (guardless steps have no guard-replay signal — their completion comes
     solely from their pending token; re-drive must not re-run a step whose token is still
     pending)** governs the case where the landed-on step is **guardless** (or guard-true) but is
     ALREADY in flight from a *previous* generation whose `loom-state` survived partially, OR — more
     simply for this story's scope — governs why replay is sound: a guardless step's true/false-ness
     is NOT derivable from Core KV state (no guard-replay signal exists for it), so replay can only
     ever **land on** a guardless step (treating "no guard = true = run"), never skip past one based
     on inferred completion. The recovery test (Task 4) constructs the scenario the contract
     describes: replay correctly resumes at the first true-guarded (or guardless) step without
     double-submitting.
   - This is a `loom-state`-**lost** scenario (the contract's "disaster recovery"), not the
     already-covered normal-restart path (8.1/8.2's `TestLoomE2E_MidRunRestartExactlyOnce` /
     `TestOnboardingE2E_LongWaitRestartExactlyOnce`, which rely on the surviving `token.` pointer and
     do NOT touch guard replay).

7. **Linear only — unchanged.** No new step shape, no branching/looping interpretation of guards. A
   guard is one boolean gating one step's run/skip; `validate()` continues to reject any `kind`
   other than `systemOp`/`userTask`.

8. **`go test ./internal/loom/...` green** including new guard-grammar validate() tests, new
   guard-evaluation/skip e2e test(s), and the recovery test (Task 4). `internal/substrate/...` and
   `internal/refractor/...` test suites are **UNTOUCHED and stay green** (mirror-image discipline,
   same as 8.5) — this story does not edit `internal/substrate` or `internal/refractor`.

## Tasks / Subtasks

- [x] **Task 1: Declarative guard grammar types + parser** (AC 1, 5)
  - [x] In `internal/loom/pattern.go` (or a new `internal/loom/guard.go` — recommended, keeps
    `pattern.go` focused on shape/validate), define the guard AST types for the §10.5 grammar:
    atoms `{absent: <path>}`, `{present: <path>}`, `{equals: {path: <path>, value: <any>}}`; composites
    `{allOf: [<guard>...]}`, `{anyOf: [<guard>...]}`, `{not: <guard>}`. Use `json.RawMessage` +
    a discriminator-key-based unmarshal (a guard object has exactly ONE of
    `absent|present|equals|allOf|anyOf|not|starlark` as its top-level key — multiple keys, zero keys,
    or an unrecognized key is a parse error).
  - [x] Recognize `{"reads": [...], "starlark": "..."}` as the **reserved Starlark shape** during
    parse and produce a distinct sentinel error (e.g. `errStarlarkReserved` /
    `ErrGuardStarlarkReserved`) so `validate()` can format the precise "reserved, not yet supported"
    message (AC#1) — distinguish this from `errMalformedGuard` (generic).
  - [x] Implement a `path` type/parser for `subject.data.<field>` and
    `subject.<aspect>.data.<field>` (AC#4). Reject any other shape (e.g. `subject.<aspect>.<field>`
    without `.data.`, a path not starting with `subject.`, link-walk-shaped paths) as malformed at
    parse time.
  - [x] `allOf`/`anyOf` with an empty array is a parse/validate error (AC#5).
- [x] **Task 2: `validate()` integration** (AC 1)
  - [x] Replace `internal/loom/pattern.go:134-136` (`if len(s.Guard) != 0 { return ... "guards are
    out of scope" }`) with: `if len(s.Guard) != 0 { if _, err := parseGuard(s.Guard); err != nil {
    return fmt.Errorf("pattern %q step %d: %w", p.PatternID, i, err) } }` — parse errors (including
    the Starlark-reserved sentinel) bubble as the pattern-rejection error, same doctrine as the
    unknown-`kind` branch above it.
  - [x] Update/extend `internal/loom/pattern_test.go`'s
    `TestPatternValidate_AcceptsSystemOpAndUserTask_RejectsGuardsAndUnknownKinds` — the two existing
    "guarded systemOp/userTask rejected" cases (lines 35-43) now use a guard shape that's actually
    INVALID per the new grammar (or split into new cases): add cases for (a) a valid declarative
    guard on systemOp AND userTask → accepted; (b) each malformed shape (unknown top-level key,
    multi-key guard object, empty `allOf`, bad path shape) → rejected; (c) the Starlark-reserved
    shape → rejected with the reserved-specific message (assert via `errors.Is` or message
    substring, not just non-nil).
- [x] **Task 3: Guard evaluator + Core KV hydration + engine integration** (AC 2, 3, 4, 5)
  - [x] Implement `evalGuard(ctx, conn, coreKVBucket, subjectKey string, g *guard) (bool, error)`
    (new file, e.g. `internal/loom/guard.go` or `guard_eval.go`):
    - Resolves each referenced path via the loom-local resolver (AC#4): root read is
      `conn.KVGet(ctx, coreKVBucket, subjectKey)` once per evaluation (not once per path — a
      pattern's guard may reference multiple root fields; cache the root + each distinct aspect
      fetched within one `evalGuard` call to avoid redundant GETs across paths in the SAME guard —
      this is the "per-evaluation, no caching layer" scope: no cache ACROSS evaluations/steps, but
      don't refetch the same key twice within one evaluation).
    - Aspect read is `conn.KVGet(ctx, coreKVBucket, subjectKey+"."+aspect)`.
    - `substrate.ErrKeyNotFound` on root or aspect → that path is **absent** (not an error).
    - A `data` envelope present but missing `<field>` → **absent**.
    - `isDeleted: true` on the fetched vertex/aspect envelope → **absent** for ANY field under it
      (mirrors `internal/refractor/ruleengine/full/executor.go:471-473`'s `fetchNode` tombstone
      check — re-implement loom-local, do not import).
    - String values: trim, then empty → absent.
    - `"0"` (string), `false`, `0` (number) → present (NOT absent) — only null/missing/tombstone/
      empty-string-after-trim are absent (AC#5).
    - `equals`: absent path → false (never equals, including against `null`/`""`); present path →
      compare resolved value to `value` (JSON-type-aware: numbers compare numerically regardless of
      int/float JSON encoding, strings/bools compare directly).
    - `allOf`/`anyOf`/`not`: standard boolean composition over sub-results (short-circuit is fine —
      no side effects to order).
    - Absent guard (`len(step.Guard) == 0`) → caller treats as `true` (run); `evalGuard` itself is
      only called when a guard IS present.
  - [x] Wire `evalGuard` into the transition path in `internal/loom/engine.go`. The natural seam is a
    new helper, e.g. `advanceToRunnableStep(ctx, inst, pattern) (runCursor int, completed bool, err
    error)` called from both `handleTrigger` (step 0) and `advance` (post-increment): starting from
    `inst.Cursor`, loop: if `cursor >= len(pattern.Steps)` → `completed=true`; else evaluate
    `pattern.Steps[cursor].Guard` — absent or true → return `(cursor, false, nil)`; false → `cursor++`,
    repeat. The caller then either calls `e.complete(...)` (completed) or `e.submitStep(...)` with
    the returned cursor (set `inst.Cursor = runCursor` before submitting — `submitStep` already reads
    `pattern.Steps[inst.Cursor]`).
  - [x] `handleTrigger` (`internal/loom/engine.go:404`, currently `e.submitStep(ctx, inst, pattern,
    "")`): call `advanceToRunnableStep` first; if `completed`, call `e.complete(ctx, inst, pattern,
    "")` instead of `submitStep` (a pattern whose step-0 guard — and all subsequent — are false
    completes immediately on trigger, a legitimate "verify-info, nothing to do" run).
  - [x] `advance` (`internal/loom/engine.go:587-591`, currently `inst.Cursor++; if inst.Cursor >=
    len(...) { complete } else { submitStep }`): after `inst.Cursor++`, call
    `advanceToRunnableStep` and branch the same way.
  - [x] Confirm `coreKVBucket` is reachable from these call sites (`e.cfg.CoreKVBucket`, already used
    by `trackerExists`/`taskVertexExists` — same bucket, just a different key).
- [x] **Task 4: Disaster-recovery cursor-rebuild test** (AC 6)
  - [x] New e2e test (e.g. `internal/loom/guard_e2e_test.go`, following
    `internal/loom/onboarding_e2e_test.go` / `loom_e2e_test.go` conventions —
    `startNATS`/`provision`/`newEngine`/`installPattern`/`submitStartLoomPattern`/`fakeProcessor`):
    1. Install a pattern mirroring the §10.5 example (`docs/contracts/10-orchestration-surfaces.md:350-361`):
       3 steps, e.g. `[{userTask SetName, guard: {absent: subject.profile.data.name}}, {userTask
       SetPhone, guard: {absent: subject.profile.data.phone}}, {userTask SetAddress}]` (last step
       guardless — exercises invariant 2).
    2. Seed the subject's Core KV `.profile` aspect with `name` already present (non-empty) BEFORE
       starting the engine — so step 0's guard is false from the start.
    3. Start engine generation 1, submit `StartLoomPattern`. Step 0 (guard false, `name` present) is
       skipped with NO `CreateTask`; step 1 (guard true, `phone` absent) runs — wait for its
       `token.<taskKey>` (mirroring `waitTaskKey`). Assert `inst.Cursor == 1` and exactly one
       `CreateTask` (for SetPhone, not SetName).
    4. **Wipe `loom-state` entirely** (delete the bucket / purge all keys — use whatever
       `substrate.Conn` primitive the test harness already uses for bucket teardown; if none exists,
       `KVListKeys` + `KVDelete` each key, or recreate the bucket via `provision`'s setup path —
       check `provision`/`startNATS` for a bucket-recreate helper before hand-rolling).
    5. Restart the engine (generation 2, fresh `*loom.Engine` over the same `conn`/buckets).
       Re-submit the **same** `StartLoomPattern` (same `subjectKey`, `patternRef` — a fresh
       `instanceId` is fine/expected since `loom-state` is gone and `instanceId` was the lost
       cursor's key; the test asserts on **effective resumption position**, not literal instanceId
       continuity).
    6. Assert: the new instance's cursor lands on step 1 (SetPhone) again — NOT step 0 (guard
       correctly re-evaluates `name` present → skip) — and the SetPhone `CreateTask` count is
       **exactly the count from step 3 plus at most one more from this re-submission's own step-1
       run** (i.e. no double-submit of step 0's already-skipped SetName, and the re-submission's
       step 1 either reuses semantics correctly or is a fresh instance's own first CreateTask for
       SetPhone — document in Dev Notes / Completion Notes exactly which count you assert and why,
       since "lost loom-state + re-submitted trigger = new instanceId" means this is necessarily a
       NEW instance whose own step-1 CreateTask is legitimately its first).
    7. Complete the flow (submit bound ops for SetPhone, SetAddress — the guardless final step,
       invariant 2) and assert `events.loom.patternCompleted`.
  - [x] Add a focused **unit test** (no NATS) for `advanceToRunnableStep` / `evalGuard` against a
    fake/in-memory KV (if `substrate.Conn` has a testable in-memory mode used elsewhere in
    `internal/loom`'s unit tests — check `pattern_test.go`/`fingerprint_test.go` for the pattern; if
    none exists, the e2e test above plus targeted `evalGuard` table-tests using a real embedded-NATS
    `conn` with `t.Short()` skip is acceptable, consistent with existing `internal/loom` test style
    which is overwhelmingly e2e/NATS-based).
- [x] **Task 5: Docs** (house rules: docs → `/docs`)
  - [x] Update `docs/components/loom.md` — the "Core model" / "Execution loop" sections already
    describe guards prospectively (`docs/components/loom.md:33-63`); confirm the description matches
    the SHIPPED grammar/semantics (declarative atoms, pinned absence, Starlark reserved) and tighten
    any "will" → "does" language now that guards are implemented. Add a short pointer to the
    loom-local path-resolver mirroring Refractor's `resolveProperty` (file:line reference), and to
    where `validate()` rejects malformed/Starlark guards.
  - [x] No CONTRACT-AMENDMENT-REQUEST expected — the guard spec (§10.5/§10.6) is complete for this
    story's scope. If a genuine gap surfaces, write it to
    `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md` (do NOT edit `docs/contracts/10-orchestration-surfaces.md`)
    and flag it in "Questions for Winston" below — do not block on it.
- [x] **Task 6: Verification gates**
  - [x] `go build ./...`
  - [x] `make vet`
  - [x] `golangci-lint run ./...`
  - [x] `make verify-kernel`
  - [x] `make test-bypass` (Gate 2, all BLOCKED)
  - [x] `make test-capability-adversarial` (Gate 3, all DEFENDED)
  - [x] `go test ./internal/loom/... ./internal/substrate/... ./internal/refractor/...` (the latter
    two UNTOUCHED, must stay green)

## Dev Notes

### THE FROZEN CONTRACT IS BINDING — §10.5 + §10.6 transcribed

This section transcribes the load-bearing text of `docs/contracts/10-orchestration-surfaces.md`
§10.5 (guard grammar) and §10.6 (crash-safety invariants) so the dev agent does not need to
interpret the contract independently. **The contract is FROZEN — do not edit it.** If you believe
there is a genuine gap, STOP and write `cmd/loom/CONTRACT-AMENDMENT-REQUEST.md`, then note it under
"Questions for Winston" — the epics text alone is NOT sufficient; the contract governs.

**§10.5 step shape** (`docs/contracts/10-orchestration-surfaces.md:383-412`):

> **Step shape:** `{ kind, operation, guard? }` — completion is implicit (§10.6), no per-step event.
>
> **Guards — pure predicate over the subject's current state.** Absent guard = step always runs.
>
> - **Paths are explicit**: `subject.<aspect>.data.<field>` (aspect) or `subject.data.<field>`
>   (root). Guards read **only the subject + its aspects** — no link-walking (a guard that needs
>   related state is a Weaver signal). At step-entry the engine JIT-hydrates the subject (root +
>   referenced aspects) and resolves the path with the same `resolveProperty`/aspect-navigation the
>   Refractor executor uses.
> - **Declarative grammar (default):** atoms `{absent: <path>}`, `{present: <path>}`,
>   `{equals: {path, value}}`, composable with `{allOf|anyOf|not: [...]}` (still one boolean — NOT
>   branching). **Pinned semantics (binding, removes ambiguity):** `absent` = the path resolves to
>   **null, missing, a soft-deleted aspect, OR (for strings) empty-after-trim**; `present` = not
>   absent. An empty-string-after-trim is **absent**; `"0"`/`false`/`0` are **present**.
> - **Starlark escape hatch (reserved):** for a predicate the grammar can't express, a guard may be
>   `{ "reads": ["<aspect>", ...], "starlark": "def guard(subject): return ..." }` — evaluated by the
>   **same verified-pure sandbox** the Processor uses... The shared pure-evaluator extraction lands
>   **only when the first Starlark guard is authored** (deferred until needed; declarative-only ships
>   without it).
> - Either way a guard is **pure declarative data or a pure function** → the instance cursor is
>   rebuildable by replaying guards (no side effects, deterministic).

The §10.5 worked example (`docs/contracts/10-orchestration-surfaces.md:350-361`) — use as the
recovery-test fixture shape (Task 4):

```json
{
  "patternId":   "onboarding",
  "subjectType": "identity",
  "completionDomains": ["orchestration"],
  "steps": [
    { "kind": "userTask", "operation": "SetName",
      "guard": { "absent": "subject.profile.data.name" } },
    { "kind": "userTask", "operation": "SetPhone",
      "guard": { "absent": "subject.profile.data.phone" } },
    { "kind": "userTask", "operation": "SetAddress" }
  ]
}
```

**§10.6 crash-safety invariant 2** (`docs/contracts/10-orchestration-surfaces.md:573-577`, BINDING):

> **Guardless steps complete only via their token (retained).** A step with no guard has **no
> guard-replay signal** (guard-replay can't tell a guardless step ran). So a guardless step's
> completion comes **solely** from its `pendingToken` (taskId/requestId); re-drive must **not**
> re-run a step whose token is still pending, or it double-submits. (The §10.5 example ends on a
> guardless `SetAddress`.)

**Practical reading for this story:** guard replay is a *forward skip* mechanism only — it answers
"is this step's precondition already satisfied, so should it be skipped?" It NEVER answers "did this
step already run?" (that's the token's job, unchanged from 8.1/8.2). The recovery scenario in AC#6 /
Task 4 is specifically about `loom-state` being **lost** (no token to consult at all) — guard replay
reconstructs *where a fresh instance should start*, landing on the first true-guarded-or-guardless
step exactly as a brand-new instance would if the subject already had some fields populated. It does
NOT, and cannot, recover a SPECIFIC in-flight token from a previous (lost) instance — that data is
gone with `loom-state`. The "no double-submit" guarantee in this story's scope is about the
**guard-skip path** (a false-guarded step never creates a task/op, full stop — true regardless of
replay vs. fresh start) and about the **new instance's own first run** of whatever step it lands on
being its only run (normal write-ahead idempotency, unchanged).

### Path resolution — mirror, do not import, Refractor's `resolveProperty`

Refractor's general resolver: `internal/refractor/ruleengine/full/executor.go:1270-1290`
(`resolveProperty`) + `:453-476` (`fetchNode`, the tombstone/soft-delete check via `isDeleted`).
Read both — they define the semantics this story's loom-local resolver must match for the TWO path
shapes the guard grammar allows. Key points to mirror:

- A vertex's own `data` fields are read directly from its envelope (root-body fields).
- A field NOT in the root body is an **aspect reference**: `fetchNode(nodeKey + "." + aspectName)`
  — a separate Core KV point-read of `<nodeKey>.<aspectName>`.
- `fetchNode` returns `nil` for a missing key OR a key whose envelope has `isDeleted: true`
  (`executor.go:471-473`) — re-implement this tombstone check loom-local (do not import
  `internal/refractor/*`; `internal/loom` imports only `substrate/*` + stdlib, 8.1 AC#8, restated in
  `internal/loom/doc.go:11-13`).

**loom's resolver is narrower than Refractor's**: Refractor's `resolveProperty` handles arbitrary
property names that MAY be aspect references (ambiguous until tried). loom's guard paths are
EXPLICIT — `subject.data.<field>` vs `subject.<aspect>.data.<field>` — so there is no ambiguity to
resolve at runtime; the path's SHAPE (parsed at validate-time, Task 1) already tells the evaluator
which Core KV key(s) to fetch and where in the envelope to look (`data.<field>` in both cases — note
both path shapes end in `.data.<field>`, matching the envelope convention
`{class, data: {...}, ...}` that both root vertices and aspects use).

### Engine integration points (file:line anchors)

- `internal/loom/pattern.go:23` — `Step.Guard json.RawMessage` (existing, untouched shape).
- `internal/loom/pattern.go:119-139` — `validate()`; lines 134-136 are the rejection to replace
  (Task 2).
- `internal/loom/engine.go:404` — `handleTrigger`'s `e.submitStep(ctx, inst, pattern, "")` call for
  step 0 — needs the `advanceToRunnableStep` pre-pass (Task 3).
- `internal/loom/engine.go:587-591` — `advance`'s post-increment dispatch
  (`inst.Cursor++; if ... complete else submitStep`) — needs the same pre-pass.
- `internal/loom/engine.go:603-609` — `submitStep` dispatches on `pattern.Steps[inst.Cursor].Kind`;
  unchanged, just called with the post-skip cursor.
- `internal/loom/engine.go:896, 911` — existing `e.conn.KVGet(ctx, e.cfg.CoreKVBucket, ...)` calls
  (tracker/task-vertex probes) — same bucket config (`e.cfg.CoreKVBucket`), same `substrate.Conn`
  primitive (`KVGet`) the guard-hydration reads will use for the subject root + aspects.
- `internal/loom/state.go` — `Instance.Cursor` (line 43) is the field `advanceToRunnableStep` mutates
  before `submitStep`/`complete`; no `stateStore`/schema changes needed — guard evaluation is a
  pure read against Core KV, the WRITE side (transition/AtomicBatch) is unchanged.

### Hydration scope — "per-evaluation, no caching layer"

Per Winston's adjudication: read the subject root + referenced aspects from Core KV at step entry,
per-evaluation, with NO cross-step/cross-evaluation cache. Within ONE `evalGuard` call (one step's
guard, which may be a composite referencing multiple paths), avoid redundant GETs for the same key
(e.g. `allOf` referencing two fields of the same aspect should fetch that aspect once) — this is a
trivial in-call map, not a "caching layer." Document the perf note (future: a per-transition or
per-instance hydration cache if guard-heavy patterns show GET volume) in a code comment near
`evalGuard`, NOT as a TODO/FIXME (house rules: no history comments, but a forward-looking "future
work" note describing current behavior's limits is fine — phrase as "today X; a cache would do Y" not
"TODO: add cache").

### Testing standards

- `internal/loom` tests are overwhelmingly e2e against embedded NATS (`startNATS`, `t.Skip` under
  `-short`) — see `internal/loom/loom_e2e_test.go`, `internal/loom/onboarding_e2e_test.go`. Follow
  this convention for the guard-skip and recovery tests (Task 4).
  `internal/loom/pattern_test.go` and `internal/loom/fingerprint_test.go` are the existing pure-unit
  (no-NATS) exceptions — `evalGuard`/`parseGuard`/path-parsing are good candidates for similar
  no-NATS unit tests if a guard can be evaluated against a hand-built `*substrate.Conn` fixture or
  in-memory map; if `substrate.Conn` requires a live NATS connection for `KVGet` (check
  `internal/substrate/kv.go`), e2e-with-`t.Short()`-skip is the fallback, consistent with the
  package norm.
- `internal/loom/onboarding_e2e_test.go`'s helpers (`seedOpMeta`, `submitBoundOp`, `waitTaskKey`,
  `installPattern`, `submitStartLoomPattern`, `newEngine`, `fakeProcessor`, `waitInstanceStatus`,
  `getInstance`) are directly reusable for Task 4's recovery test — seed the subject's `.profile`
  aspect with `installPattern`'s sibling pattern (write `vtx.identity.<id>.profile` via
  `conn.KVPut` with a `{class, data: {name: "Ada"}}` envelope, mirroring `seedOpMeta`'s
  `conn.KVPut(ctx, coreKVBucket, key, body)` shape at
  `internal/loom/onboarding_e2e_test.go:33-43`).
- For "wipe loom-state entirely" (Task 4 step 4): check `internal/loom/loom_e2e_test.go:44`
  (`provision`) for how buckets are created — if `provision` is idempotent re-create, calling it
  again after deleting the `loom-state` KV bucket via a substrate/NATS JetStream KV-delete-bucket
  primitive is the cleanest "lost entirely" simulation. If no bucket-delete primitive is
  conveniently exposed, `KVListKeys` + `KVDelete` every key in `loom-state` is an acceptable
  equivalent ("lost entirely" = no readable instance/token/outbox/deadline state survives).

### Project Structure Notes

- New guard-grammar/evaluator code: prefer a new file `internal/loom/guard.go` (types + `parseGuard`
  + `validate()` hookup) and optionally `internal/loom/guard_eval.go` (`evalGuard` + Core KV
  hydration) — keeps `pattern.go` (shape/validate) and `engine.go` (transition orchestration) from
  growing unboundedly. Either single-file or split is fine; prioritize readability.
- New tests: `internal/loom/guard_test.go` (unit, grammar parsing/validate) +
  `internal/loom/guard_e2e_test.go` (e2e, skip+recovery) — or fold into `pattern_test.go`/a new e2e
  file per the conventions above. No new top-level packages; everything stays under
  `internal/loom`.
- No changes to `internal/substrate` or `internal/refractor` (AC#8) — if `evalGuard` discovers it
  needs a substrate primitive that doesn't exist (e.g. a batch-GET), STOP and write the
  CONTRACT-AMENDMENT-REQUEST / Questions-for-Winston note rather than adding it; `KVGet` per-path
  (with the in-call dedup noted above) is sufficient for this story's grammar.

### References

- [Source: docs/contracts/10-orchestration-surfaces.md §10.5 (lines 337-415), §10.6 (lines 417-583,
  esp. invariant 2 at 573-577)]
- [Source: internal/loom/pattern.go (Step.Guard:23, validate:119-139, esp. 134-136)]
- [Source: internal/loom/engine.go (handleTrigger:356-410, advance:572-592, submitStep:594-609,
  trackerExists/taskVertexExists:892-919 as KVGet-on-CoreKVBucket precedent)]
- [Source: internal/loom/state.go (Instance:39-47, stateStore.transition:160-232 — unchanged by this
  story)]
- [Source: internal/refractor/ruleengine/full/executor.go (resolveProperty:1270-1290,
  fetchNode:453-476)]
- [Source: internal/loom/onboarding_e2e_test.go (helpers: seedOpMeta:33-43, waitTaskKey:99-118,
  the §10.5-shaped onboardingPattern:83-94 — the fixture pattern for Task 4)]
- [Source: internal/loom/loom_e2e_test.go (provision:44, installPattern:313-324,
  submitStartLoomPattern:331-362, newEngine:364-377)]
- [Source: internal/loom/pattern_test.go (existing guard-rejection cases:35-43 to be
  updated/replaced)]
- [Source: internal/loom/doc.go (module boundary, lines 11-13)]
- [Source: docs/components/loom.md (Core model:33-63 — prospective guard description to reconcile)]
- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md § Story 8.3 (lines 169-184)]

## Dev Agent Record

### Code Review Triage (Winston)

Three-layer review complete: Blind Hunter (`8-3-cr-blind-hunter.md`, 2 LOW + 1 by-design), Edge Case Hunter (`8-3-cr-edge-case-hunter.md`, 1 HIGH/1 MEDIUM/1 LOW, 13 boundaries verified clean), Acceptance Auditor (`8-3-cr-acceptance-auditor.md`, 12/12 SATISFIED, all rulings honored).

**ECH-F1 (HIGH) ruling — accept-and-pin, no ID redesign.** After total `loom-state` loss, recovery's fresh `instanceId` means a gen-1-committed guardless step re-runs in gen-2 (requestIds derive from instanceID; Contract #4 cannot dedup across generations). Deterministic IDs from (pattern, subject, step) would fix this but collide *legitimate* sequential re-runs of a pattern on a subject — the ID design is correct. The frozen contract's own doctrine governs (Contract #10 ~line 242): re-fire doubles are a "documented bound, not a silent risk; the robust check-before-act variant is a Phase-3 hardening." Fix-forward = make the bound real: extend the recovery e2e to exercise the guardless re-run and pin the bounded behavior, document the window + authoring guidance (guard steps whose re-run is costly) in `docs/components/loom.md`.

**Fix-forward (this story):** F1 as above; ECH-F3 + BH-2 — `advanceToRunnableStep` completed-path drops `runCursor` at BOTH call sites (trigger persists cursor 0; mid-pattern skip-to-completion persists a stale intermediate cursor) — set the cursor before `complete` and assert it; BH-1 — duplicate JSON keys in a guard object silently last-wins (`{"absent":"X","absent":"Y"}` parses as Y) — add token-walk duplicate-key rejection at guard parse (load-time rejection doctrine: guards are untrusted package data); BH-3 — `equals` comparand restricted to scalars at parse time (object/array comparand currently parses but can never match — convert the silent authoring trap into a load error); AA-nit — add the `equals`-against-`"0"` table case.

**Deferred (pre-existing, verified against `git show HEAD`):** ECH-F2 — `advance` reads the live pattern definition (`source.get`, same call sites pre-8.3), so a mid-flight pattern edit mis-indexes the cursor; guards widen the blast radius but did not create it. Version-pinning the pattern revision in `Instance` is a design item — spun off.

### Agent Model Used

Opus (per epics § Story 8.3 "Model: Opus")

### Debug Log References

- `go test ./internal/loom/... ./internal/substrate/... ./internal/refractor/...` — all green.
- `golangci-lint run ./...` — 0 issues.
- `make verify-kernel`, `make test-bypass` (Gate 2 all BLOCKED), `make test-capability-adversarial`
  (Gate 3 all DEFENDED) — all pass.

### Completion Notes List

- **Grammar (Task 1/2, `internal/loom/guard.go`).** Declarative §10.5 grammar parsed via a
  discriminator-key envelope with `DisallowUnknownFields`: exactly one of
  `absent|present|equals|allOf|anyOf|not` must be set (zero/multiple/unknown → `errMalformedGuard`).
  The reserved `{reads, starlark}` pair routes to a DISTINCT `errStarlarkReserved` sentinel (not
  generic malformed), so `validate()` surfaces the precise "reserved, not yet supported" message —
  asserted via `errors.Is` in `TestPatternValidate_StarlarkGuardRejectedAsReserved`. `equals`
  requires an explicit (possibly-null) `value` — an omitted value is malformed; a literal `null`
  value is legal (evaluator handles "absent never equals null"). Empty `allOf`/`anyOf` → malformed.
  Path parser accepts exactly `subject.data.<field>` and `subject.<aspect>.data.<field>`; everything
  else (no `subject.` prefix, aspect path without `.data.`, deeper nesting, empty leaf) → malformed.
- **Evaluator + hydration (Task 3, `internal/loom/guard_eval.go`).** `evalGuard` point-reads the
  subject root + referenced aspects from Core KV per-evaluation, deduping GETs per distinct key
  within ONE call via a `guardResolver` snapshot map — this is the **snapshot-per-evaluation
  correctness property** (Winston Q2 ruling): a composite guard sees one snapshot of each key, so a
  concurrent write mid-evaluation cannot make `allOf`/`anyOf` straddle two states. Documented in the
  evaluator godoc, with a forward-looking (non-TODO) note that there is no cross-step cache today.
  Tombstone/soft-delete (`isDeleted`) + null-body handling mirrors Refractor's `fetchNode`
  (re-implemented loom-local; loom imports only `substrate/*` + stdlib — verified, no
  `internal/refractor` import). Pinned absence semantics (null/missing/soft-deleted/empty-after-trim
  absent; `"0"`/`false`/`0` present; equals type-aware, absent-never-equals) table-tested in
  `TestGuardEval_PinnedAbsenceSemantics`.
- **Engine integration (Task 3, `internal/loom/engine.go`).** New `advanceToRunnableStep` helper
  loops from `inst.Cursor`, skipping false-guarded steps (no task/op/token/outbox) and returning the
  first runnable cursor or `completed`. Wired into BOTH `handleTrigger` (step 0; all-false-on-trigger
  → `complete` immediately, leaving cursor 0 — completion does not advance the cursor, so the
  all-skip test asserts status+task-count, not a cursor value) and `advance` (post-increment). A
  guard parse error at run time (a loaded pattern passed `validate()`, so this is an invariant break)
  surfaces as a nak-able error rather than a silent skip.
- **Disaster-recovery (Task 4, `internal/loom/guard_e2e_test.go`).** `wipeLoomState` (Winston Q3
  ruling: list+delete every loom-state key; no bucket-wipe primitive exists) simulates total
  loom-state loss. The recovery test runs to mid-pattern (step 0 SetName skipped via false guard,
  step 1 SetPhone parked), wipes loom-state, restarts a fresh engine, and re-submits the SAME
  `StartLoomPattern`. Per Winston Q1 ruling (narrow reading), the re-submission produces a NEW
  instanceId (asserted distinct); guard replay against the still-populated subject re-skips step 0
  (name present) and lands on step 1 (phone absent) again. **CreateTask count asserted == 2**: gen-1's
  SetPhone (1) + gen-2's own first SetPhone (1); SetName is NEVER created in either generation (its
  false guard skips it both times — no double-submit of the skipped step). The guardless final step
  (SetAddress, §10.6 invariant 2) is honored via the token rule to `patternCompleted`. The
  `loom-trigger` durable's ack floor lives in the events stream (survives the loom-state wipe), so
  gen-1's already-acked trigger is NOT redelivered — only the new gen-2 trigger fires.
- **Guard-skip semantics (Task 4, e2e).** `TestGuardE2E_FalseGuardSkipsStep` proves a run of two
  consecutive false guards skips both steps in one transition (lands on cursor 2, exactly one
  CreateTask). `TestGuardE2E_AllGuardsFalseCompletesOnTrigger` proves an all-guarded pattern with no
  guardless tail completes on trigger with zero tasks/ops.
- **No unit-only `advanceToRunnableStep` test.** `substrate.Conn.KVGet` requires a live NATS/JetStream
  connection (no in-memory mode in `internal/loom`'s existing unit tests — `pattern_test.go`/
  `fingerprint_test.go` are pure parser/registry tests). Per the task's stated fallback, the
  evaluator + engine integration is covered by NATS-backed table tests / e2e with `t.Short()` skip,
  consistent with the package norm. Pure (no-NATS) parser/path tests are in `guard_test.go`.
- **Test-only export shim.** `internal/loom/export_test.go` gains `ParseGuardForTest` /
  `EvalGuardForTest` so the external `loom_test` package's absence table test can exercise the
  evaluator against a real conn (the `*guard` AST stays unexported; callers hold it opaquely).
- **No CONTRACT-AMENDMENT-REQUEST.** §10.5/§10.6 were complete for this story's scope; no gap surfaced.

### File List

- `internal/loom/guard.go` (new) — §10.5 declarative guard AST + `parseGuard` + path parser + sentinels.
- `internal/loom/guard_eval.go` (new) — `evalGuard` + loom-local Core KV resolver (per-evaluation
  snapshot, tombstone/absence semantics).
- `internal/loom/guard_test.go` (new) — pure (no-NATS) grammar/path parser table tests.
- `internal/loom/guard_e2e_test.go` (new) — `wipeLoomState`/`seedAspect` helpers, pinned-absence
  table test, guard-skip + all-skip-on-trigger tests, §10.5-fixture disaster-recovery test.
- `internal/loom/pattern.go` — `validate()` now parses guards (replacing the out-of-scope rejection).
- `internal/loom/pattern_test.go` — guard validate cases (valid/malformed/starlark-reserved) +
  `TestPatternValidate_StarlarkGuardRejectedAsReserved`.
- `internal/loom/engine.go` — `advanceToRunnableStep` + guard pre-pass wired into `handleTrigger`/`advance`.
- `internal/loom/export_test.go` — `ParseGuardForTest`/`EvalGuardForTest` test seams.
- `internal/loom/doc.go` — package overview updated (guards shipped; stale "no guards" line removed).
- `docs/components/loom.md` — "Guard grammar (shipped)" section + resolver/validate pointers; deferred
  list updated to "Starlark guard evaluation".

---

## Questions for Winston

1. **"No double-submit across recovery" assertion precision (Task 4 step 6).** Because a
   `loom-state`-lost recovery necessarily produces a NEW `instanceId` (the old instance's identity
   was the lost cursor's key — `StartLoomPattern`'s requestId), the recovery test cannot literally
   assert "the SAME instance resumed without double-submitting ITS OWN prior in-flight step" — it
   can only assert "a fresh instance, guard-replayed against the subject's now-partially-populated
   state, lands on the same EFFECTIVE step a continuing original instance would have been at, and
   that landing does the normal single CreateTask/submit a fresh instance entering that step would
   do." Is this the intended reading of the epics' "a recovery test asserts identical resumption"
   AC language, or is there an expectation that `instanceId` itself should be recoverable/stable
   across a `loom-state`-loss (which would be a much larger change — e.g. deriving `instanceId` from
   `(patternRef, subjectKey)` rather than the trigger's requestId)? I've scoped Task 4 to the
   narrower, contract-consistent reading (guard replay determines STARTING POSITION for a fresh
   instance; tokens — unchanged — govern in-flight idempotency for THAT instance going forward).

2. **In-call hydration dedup vs. "no caching layer."** I've scoped "per-evaluation" hydration to mean
   no cache persists BETWEEN `evalGuard` calls (steps/transitions), but within a single composite
   guard's evaluation (e.g. `allOf` referencing two fields of the same aspect) the resolver dedupes
   identical Core KV GETs via a local map. If this reading is too liberal relative to "correctness
   first, no caching" — i.e. you'd prefer literally one GET per path reference even within one guard
   — say so and I'll have dev-story simplify to that (it's a smaller diff, just more GETs on
   pathological multi-path-same-aspect guards, which I expect to be rare).

3. **Bucket-wipe primitive for "loom-state lost entirely" (Task 4).** I haven't located a
   ready-made "delete/recreate KV bucket" helper in the `internal/loom` test harness — `provision`
   creates buckets but I didn't confirm idempotent re-creation after a delete, nor a one-call
   `KVListKeys`+`KVDelete`-all helper. If dev-story finds this awkward, a minimal
   `wipeLoomState(t, ctx, conn)` test helper (list+delete all keys in `loom-state`, or
   delete-bucket+re-`provision`) is in-scope as test-support code — flagging in case there's a
   preferred existing pattern I missed in a file I didn't read (e.g. `supervisor_test.go`'s consumer
   teardown helpers might have a bucket-delete primitive worth reusing).

## Winston's rulings (adjudicated — binding; dev builds to these)

1. **Q1 → narrow reading CONFIRMED.** "Identical resumption" means: after total `loom-state` loss, a fresh `StartLoomPattern` against the same subject lands on the same *effective step* a continuing instance would occupy — guard replay places the cursor; tokens govern the new instance's own in-flight idempotency from there. A stable/recoverable `instanceId` across loss is OUT of scope (the contract's recovery vehicle is re-trigger against current state, not instance resurrection). Task 4 stays as scoped.
2. **Q2 → in-call dedup CONFIRMED, and it is a correctness requirement, not just an optimization.** One GET per distinct Core KV key within a single guard evaluation means a composite guard evaluates against ONE snapshot of each key — two reads of the same key mid-evaluation could straddle a concurrent write and make `allOf`/`anyOf` non-deterministic over a single state. "No caching layer" forbids cross-evaluation caching only. Document the snapshot-per-evaluation property in the evaluator's godoc.
3. **Q3 → add the `wipeLoomState` test helper.** No bucket-wipe primitive exists; a minimal test-side helper (list+delete all keys, or delete-bucket+re-provision via the test-side jetstream handle — test files may use jetstream per the 8.5 precedent) is in-scope as test support. Dev picks the shape; keep it in a `_test.go` file.
