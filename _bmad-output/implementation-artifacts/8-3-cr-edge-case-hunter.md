# Story 8.3 — Edge Case Hunter review (declarative guards, skip-advance, cursor rebuild)

Scope: `git diff HEAD` + untracked `internal/loom/guard.go`, `guard_eval.go`, `guard_test.go`,
`guard_e2e_test.go`. Verified against `engine.go`, `state.go`, `token.go`, `pattern.go`, the
Refractor tombstone path, and the FROZEN §10.5/§10.6 spec.

Method: walked every branch of the parser, the evaluator, the skip-advance loop, the
disaster-recovery path, and the deadline/concurrency interplay against the real Core KV envelope
shapes. Only REAL unhandled cases below — verified, not speculative.

---

## Findings

### F1 — Disaster recovery double-submits a guardless **systemOp** step (HIGH)

**Path:** `engine.go:632 advanceToRunnableStep` (guardless step always RUNs) + `token.go:38
deriveID` (token keyed on `instanceID`) + total `loom-state` wipe.

**Breaking scenario:**
1. A guardless **systemOp** step submits its op; the op COMMITS (Contract #4 tracker
   `vtx.op.<requestId>` written) but its completion event is still in flight.
2. Total loom-state loss + crash (the §10.6 disaster precondition the story tests).
3. A fresh `StartLoomPattern` is re-submitted. Per `loom_e2e_test.go:333` the instanceId is a
   **fresh random NanoID**, so `deriveRequestID(instanceID, cursor)` yields a **different**
   requestId for the same step.
4. Guard-replay cannot skip a guardless step (§10.6 invariant 2 — correct by design), so gen-2
   re-runs it and submits with the NEW requestId. The Contract #4 idempotency tracker keys on
   requestId, so it does **NOT** dedup against gen-1's committed op → **the systemOp executes
   twice**.

This is exactly the §10.6-invariant-2 interplay the brief flagged: under TOTAL loss the pending
token is gone, so invariant 2's "don't re-run a step whose token is pending" protection is gone
too, and nothing downstream catches the re-submit because the cross-generation requestIds diverge.
For a **userTask** guardless step the same holds: `deriveTaskID` is also instanceId-keyed, so gen-2
mints a **new** `vtx.task.<id>` → a duplicate task vertex, not a dedup.

**Severity:** HIGH for non-idempotent systemOps; the spec's "rebuildable cursor" claim quietly
assumes either (a) guarded steps everywhere, or (b) idempotent guardless ops. The change ships
guardless systemOp support (`pattern_test.go` "valid guarded systemOp accepted" + the systemOp
submit path) without surfacing this limitation.

**Test coverage:** NOT exercised. `TestGuardE2E_DisasterRecoveryCursorRebuild` uses an all-**userTask**
pattern and gen-1 parks on step 1 (SetPhone) — it never reaches the guardless step 2 (SetAddress)
in gen-1, so neither the systemOp double-submit nor the userTask double-create is triggered. The
test asserts the SAFE case only.

**Recommendation:** Either (1) document explicitly (doc.go / loom.md / a CONTRACT-AMENDMENT-REQUEST
if it's a genuine §10.6 gap) that disaster recovery across a fresh instanceId re-runs any
already-committed guardless step, and constrain guardless steps to idempotent ops; or (2) add a
test that drives gen-1 PAST a guardless step before the wipe and asserts the (currently absent)
dedup, to make the real behavior visible. At minimum this should not pass silently.

---

### F2 — Pattern UPDATE mid-flight: in-flight instance reads the LIVE definition, not a pinned one (MEDIUM)

**Path:** `engine.go:600 advance` → `e.source.get(...)` returns the CURRENT pattern;
`source.go:285 get` has no version pinning; `Instance` carries only `PatternRef`, no version.

**Breaking scenario:** A `meta.loomPattern` is updated (CDC reload) while an instance is parked at
cursor N. On the next completion, `advance` re-reads the NEW step list and evaluates `Steps[N]`
against it. If the update inserted/removed/reordered steps or changed a guard, the cursor now
indexes a *different* step than the instance was running — it can skip a step that should run or
re-run one already done. Bounds are SAFE (a shortened list makes `cursor >= len` → completed via
`advanceToRunnableStep`, and `submitStep` is only reached when `runCursor < len`), so no panic — but
the step semantics are silently wrong.

**Severity:** MEDIUM. Likely out of scope for 8.3 (no version-pinning mechanism exists yet), but the
change set makes guards part of the live definition, so a mid-flight guard edit is now a concrete
way to corrupt an in-flight cursor. Worth an explicit "in-flight instances follow the live
definition" caveat alongside the existing reconcile caveat at `engine.go:448`.

**Test coverage:** none.

---

### F3 — Completed-on-trigger instance persists `Cursor: 0`, not the end cursor (LOW)

**Path:** `engine.go:409-420`. When step-0's (and every subsequent) guard is false, `completed` is
true but the handler calls `complete(...)` WITHOUT first doing `inst.Cursor = runCursor`.
`advanceToRunnableStep` deliberately does not mutate `inst.Cursor`, so the persisted complete
instance shows `Cursor: 0`.

Contrast: the normal completion path in `advance` (`engine.go:605`) does `inst.Cursor++` BEFORE
`advanceToRunnableStep`, so a normally-completed instance lands on a higher cursor (the disaster
test asserts `Cursor == 3`). An all-skipped-on-trigger instance reports `Cursor: 0` for a pattern
of length ≥1 that fully completed.

**Severity:** LOW (observability only — `Cursor` on a terminal instance is informational; correlation
uses tokens). But it's an inconsistency a control-plane / dashboard reading `loom-state` would
trip on ("completed at step 0" for a 3-step pattern). `TestGuardE2E_AllGuardsFalseCompletesOnTrigger`
asserts status only, never the cursor, so this is unobserved.

**Recommendation:** set `inst.Cursor = runCursor` before the `complete` call in the trigger handler
(and consider the same in `advance`'s completed branch for consistency) so a completed instance's
cursor reflects how far it ran.

---

## Boundaries walked and CONFIRMED SAFE (no finding)

- **Pinned absence semantics** (`guard_eval.go:141 absent`): null / missing key / missing aspect /
  missing root vertex / empty-string / whitespace-after-trim → absent; `"0"` / `0` / `false` / `[]`
  / `{}` → present. Matches §10.5 exactly. `data: null` and `data:`-non-object both → absent
  (type-assertion miss at `resolve:125`). Correct.
- **Soft-delete tombstone shape** (`guard_eval.go:219`): `body["isDeleted"].(bool)` matches the
  real Processor envelope (`step8_commit_test.go:148`, `script_context.go:37`) AND Refractor's
  `fetchNode` (`executor.go:469`) byte-for-byte, including the JSON-null-body → nil-map → absent
  case. Correct.
- **Number equality / json.Number** (`guard_eval.go:229 jsonValuesEqual`): both the KV body and the
  guard comparand decode via stdlib `json` into `float64` (no `UseNumber()` on either side), so
  `1`/`1.0` compare equal and `"1"` (string) never equals `1` (float). Type-aware and consistent.
  No json.Number/float64 mismatch.
- **Aspect key shape** (`guard_eval.go:115`): `subjectKey + "." + aspect` =
  `vtx.<type>.<id>.<aspect>` — the 4-segment Contract #1 aspect key. Correct.
- **equals on absent path** (`equals:166`): short-circuits to false before comparing, so an absent
  path never equals null/""/anything. Correct per §10.5.
- **Skip persisted in the same AtomicBatch as the next real submit** (write-ahead invariant §10.6
  #1): `advanceToRunnableStep` does not mutate the cursor; the caller sets `inst.Cursor = runCursor`
  then `submitStep` → `transition` writes the (skipped-forward) cursor + token + outbox + deadline
  in ONE `AtomicBatch` (`state.go:160`). A multi-step skip is one atomic fact. Correct.
- **Deadline markers for skipped steps**: skipped steps never arm a deadline (no transition runs for
  them); only the landed step's `transition` (re)arms `deadline.<instanceId>`, overwriting the prior
  step's marker (History:1 eviction, `state.go:210`). No leaked/armed marker per skipped step.
- **Deadline fires mid-skip-run** (concurrency): the prior step's deadline can fire while
  `advanceToRunnableStep` is reading KV. `onDeadline` re-reads instance, sees `PendingToken` still ==
  oldToken, probes the tracker (committed → that's why we're advancing) → calls `advance` again. Both
  advancers derive the SAME deterministic newToken; the `CreateOnly` guard on `token.<newToken>`
  (`state.go:184`) rejects the loser's batch → loser errors → Nak → re-GET sees the new token → drops.
  Race-safe.
- **Two completions racing one instance during a skip-run**: same CreateOnly-on-newToken guard plus
  the `inst.PendingToken != token` CAS at `advance:595`. Only one commits. Safe.
- **Empty allOf/anyOf, `not` with 0/2+ children, equals missing path/value, deeply nested
  composites, unknown keys, multi-key objects, bare-string guard, Starlark reserved**: all rejected
  at `validate()`/`parseGuard` with the correct sentinel; covered by `guard_test.go` and the new
  `pattern_test.go` cases.
- **Field name containing a dot** (`parseGuardPath:255`): a field literally named `a.b` is
  unreachable (parsed as too-deep/aspect path). This is a documented grammar limitation (leaf field
  only, nested = Starlark/Weaver), not a bug.
- **Guard parse error at step entry** (`advanceToRunnableStep:644`): defensive — validate()
  guarantees a loaded pattern's guards parse, so this surfaces an invariant break as an error (Nak),
  never silent. Acceptable.
