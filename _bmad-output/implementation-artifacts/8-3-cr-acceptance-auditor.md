# Story 8.3 — Acceptance Auditor report

Scope: `internal/loom/guard.go`, `internal/loom/guard_eval.go`, `internal/loom/guard_test.go`,
`internal/loom/guard_e2e_test.go`, plus diffs to `internal/loom/{engine,pattern,pattern_test,doc,export_test}.go`
and `docs/components/loom.md`.

Verdict legend: SATISFIED (file:line) / PARTIAL / VIOLATED / NOT-VERIFIABLE.

## AC-by-AC verdicts

### AC#1 — Guard grammar interpreted at validate(); malformed/Starlark rejection

**SATISFIED** — `internal/loom/pattern.go:134-140` replaces the unconditional rejection with
`parseGuard(s.Guard)`, wrapped with positional detail (`fmt.Errorf("pattern %q step %d: %w", ...)`).
- No-guard steps (`len(s.Guard)==0`) skip validation entirely — `pattern.go:137` guard.
- Declarative shapes parse via `guard.go:102-184` (`parseGuard`), discriminator-key envelope with
  `DisallowUnknownFields` (`guard.go:110-117`). Exactly-one-key enforcement at `guard.go:126-148`.
- Malformed shapes (unknown key, multi-key, empty `allOf`/`anyOf`, bad path) → `errMalformedGuard`
  (`guard.go:26`), wrapped per-step, rejecting the WHOLE pattern (same `validate()` return path as the
  unknown-`kind` branch at `pattern.go:127-129` — verified by reading the full `validate()` body).
- `{reads, starlark}` → distinct `errStarlarkReserved` sentinel (`guard.go:31`, `guard.go:121-123`),
  asserted via `errors.Is` in `TestPatternValidate_StarlarkGuardRejectedAsReserved`
  (`pattern_test.go:93-110`), with a message-substring check for "reserved" (`pattern_test.go:107-109`).
- `pattern_test.go:37-71` covers: valid guarded systemOp/userTask accepted (p3/p3b), unknown top-level
  key (p3c), multi-key object (p3d), empty `allOf` (p3e), bad path shape `subject.profile.name` (p3f),
  Starlark-reserved (p3g) — each `wantErr: true` except p3/p3b.

### AC#2 — Guard evaluation at step-entry skips false-guarded steps; repeat-on-false; all-false completes

**SATISFIED** — `internal/loom/engine.go:642-678` (`advanceToRunnableStep`):
- Absent guard (`len(step.Guard)==0`) → run (line 663-665).
- False guard → `cursor++`, loop continues — repeat-on-false within ONE call (lines 675-677).
- `cursor >= len(pattern.Steps)` → `completed=true` (lines 659-661).
- Wired at **both** call sites:
  - `handleTrigger` (step 0): `engine.go:404-421` — pre-pass before `submitStep`; `completed` →
    `e.complete(...)` instead (lines 410-417), `inst.Cursor = runCursor` set before submit (line 420).
  - `advance` (post-increment): `engine.go:606-616` — `inst.Cursor++` then pre-pass; same branch.
- Skip writes nothing: the false branch only does `cursor++` — no `CreateTask`/op/token/outbox call on
  that path (verified — the only side-effecting calls in `advanceToRunnableStep` are the two `KVGet`s
  inside `evalGuard`, which are reads).
- `TestGuardE2E_FalseGuardSkipsStep` (`guard_e2e_test.go:169-219`) proves a run of TWO consecutive false
  guards skips both in one transition (lands directly on cursor 2, `createTaskCount()==1` for the single
  un-skipped step).
- `TestGuardE2E_AllGuardsFalseCompletesOnTrigger` (`guard_e2e_test.go:224-272`) proves a 2-step
  all-guarded pattern with no guardless tail completes on trigger, `createTaskCount()==0`.

### AC#3 — Hydration: subject root + guard-referenced aspects, per-evaluation

**SATISFIED** — `evalGuard` (`guard_eval.go:32-42`) builds a `guardResolver` with `envelopes`/`fetched`
maps scoped to the single call; `envelope()` (`guard_eval.go:190-201`) memoizes per distinct Core KV key
within that call. `resolve()` (`guard_eval.go:112-135`) computes the key as `subjectKey` (root, when
`p.aspect==""`) or `subjectKey+"."+aspect`. A guard with no aspect paths never calls `envelope()` for an
aspect key — only the root is fetched if referenced. No cross-evaluation cache exists (a fresh
`guardResolver` is constructed per `evalGuard` call, called once per step from
`advanceToRunnableStep`'s loop body, line 671).

### AC#4 — Path-resolution mirrors Refractor's resolveProperty/fetchNode; loom-local; two shapes only

**SATISFIED**
- `subject.data.<field>` → `guardPath{aspect:"", field}`, root envelope's `data.<field>`
  (`guard.go:262-268`, `guard_eval.go:112-135`).
- `subject.<aspect>.data.<field>` → `guardPath{aspect, field}`, point-read
  `<subjectKey>.<aspect>` and read `data.<field>` (`guard.go:269-274`, `guard_eval.go:113-115`).
- Any other shape (no `subject.` prefix, aspect-without-`.data.`, deeper nesting, bare `subject`,
  empty leaf) → `errMalformedGuard` (`guard.go:255-278`); table-tested at `guard_test.go:46-51`.
- Tombstone check (`isDeleted: true` → absent) re-implemented loom-local at `guard_eval.go:219-221`,
  mirroring `executor.go:471-473` per the godoc comment (`guard_eval.go:183-189`).
- `internal/loom` imports only `substrate/*` + stdlib — `guard.go`/`guard_eval.go` import lists
  (`guard.go:3-8`, `guard_eval.go:3-11`) confirm no `internal/refractor` import. `go build ./...` and
  `go vet ./...` both clean (verified).

### AC#5 — Pinned absence semantics

**SATISFIED**, with one minor coverage gap noted below.
- `absent` per `guard_eval.go:141-159`: not-found → absent (line 146-148); `nil` (JSON null) → absent
  (150-152); string → `TrimSpace == ""` → absent (153-154); everything else (numbers incl. 0, bools
  incl. false, objects, arrays) → present (155-157, comment explicit).
- `present` = `!absent` (`guard_eval.go:68-73`).
- `equals`: absent path → `false` unconditionally, never compared (`guard_eval.go:166-171`); present
  path → `jsonValuesEqual` type-aware compare (`guard_eval.go:225-247`) — numbers both `float64` after
  JSON decode compare numerically regardless of int/float source encoding, strings/bools direct,
  `nil==nil`.
- `allOf([])`/`anyOf([])` → `errMalformedGuard` at `guard.go:231-233` (`parseGuardList`), tested at
  `guard_test.go:37-38`.
- Table test `TestGuardEval_PinnedAbsenceSemantics` (`guard_e2e_test.go:69-163`) covers, **verbatim**,
  each pinned case: present field not absent; missing field absent; null field absent; empty-string
  absent; blank-after-trim absent; `"0"` string PRESENT (line 128, asserted via `{"absent":...}` ==
  `false`); `false` bool PRESENT (129); `0` number PRESENT (130); missing aspect absent; soft-deleted
  aspect field absent (and `present` on it == false, line 137); `equals` string/number
  match+mismatch; `equals` zero-number and `equals` false-bool BOTH present-and-equal (146-147,
  pinning that `0`/`false` are not just "present" but also usable in `equals`); absent-never-equals
  null/empty-string/blank-after-trim (148-150); `allOf`/`anyOf`/`not` composition (152-156).
  - **Minor gap (non-blocking)**: the pinned `"0"` (string) case is exercised only via `{"absent":...}`
    (line 128), not also via `{"equals":{"path":..., "value":"0"}}`. The `equals`-type-aware claim is
    still covered for number-zero and bool-false (146-147). This is a coverage nit, not a semantics
    violation — `jsonValuesEqual`'s string branch (`guard_eval.go:236-238`) is generic and exercised by
    the string-match/mismatch cases (142-143). Flagging for completeness, not blocking.

### AC#6 — Disaster-recovery cursor rebuild (§10.6 invariant 2)

**SATISFIED** — `TestGuardE2E_DisasterRecoveryCursorRebuild` (`guard_e2e_test.go:281-360`):
1. §10.5-fixture pattern (`guardedOnboardingPattern`, `guard_e2e_test.go:51-62`) — SetName guarded on
   `profile.data.name` absent, SetPhone guarded on `profile.data.phone` absent, SetAddress guardless.
2. `profile.data.name` seeded present BEFORE gen-1 starts (line 305) — step 0 guard false from the
   start.
3. Gen-1: step 0 skipped (no CreateTask), step 1 (SetPhone) runs; `waitTaskKey(...,1)` +
   `createTaskCount()==1` (lines 314-317).
4. `wipeLoomState` (lines 320-322) — list+delete every `loom-state` key (`guard_e2e_test.go:22-29`),
   using `KVListKeys`+`KVDelete` per Winston Q3 (no bucket-wipe primitive). Since `KVDelete` writes a
   tombstone and subsequent `KVGet` returns `ErrKeyNotFound` (`internal/substrate/kv.go:35`,
   confirmed), and `getInstance` maps `ErrKeyNotFound → (nil, nil)` (`state.go:97-99`), this correctly
   simulates "nothing readable survives" for `handleTrigger`'s idempotency check.
5. Gen-2 fresh engine, same conn/buckets, same `StartLoomPattern` re-submitted (lines 328-334).
6. New `instanceId` asserted distinct (line 335, per Winston ruling 1). Re-driven instance lands on
   step 1 (SetPhone) again, NOT step 0 (lines 339-340); `createTaskCount()==2` total — gen-1's SetPhone
   + gen-2's own first SetPhone; SetName is never created in either generation (lines 342-346).
7. Gen-2 driven to completion: SetPhone completed, SetAddress (guardless) task created and completed,
   `events.loom.patternCompleted` observed, `inst.Cursor==3` at `status=complete` (lines 348-359).

This is the "narrow reading" scenario per Winston ruling 1 — verified against the test code, the
assertions match the documented reasoning exactly (see ruling 1 below).

### AC#7 — Linear only, unchanged

**SATISFIED** — no new step `kind` introduced; `validate()` (`pattern.go:119-145`) still rejects any
`kind` other than `systemOp`/`userTask` (unchanged branch, `pattern.go:127-129`, confirmed present in
full file read). Guard composition (`allOf`/`anyOf`/`not`) reduces to ONE boolean
(`guard_eval.go:76-103`) — no branching semantics introduced into the engine's cursor logic
(`advanceToRunnableStep` always advances `cursor` linearly by 1 or returns it).

### AC#8 — Test gates green; substrate/refractor untouched

**SATISFIED**
- `go test ./internal/loom/...` — `ok` (32.8s, full run including e2e/NATS tests, executed during this
  audit).
- `go build ./...` and `make vet` (`go vet ./...`) — clean.
- `git diff HEAD --stat -- internal/substrate internal/refractor internal/processor docs/contracts` —
  **empty** (verified during this audit; no files touched in any of these trees).

## Winston's rulings — verification

### Ruling 1 (Q1, narrow reading — same effective step, new instanceId)

**SATISFIED** — `guard_e2e_test.go:335` asserts `require.NotEqual(t, instanceID, instanceID2, ...)`
("lost loom-state ⇒ a new instance id"), and lines 339-346 assert the NEW instance lands on the same
EFFECTIVE step (step 1/SetPhone) a continuing instance would occupy, with `createTaskCount()==2`
documented as gen-1's SetPhone + gen-2's own first SetPhone (no double-submit of the already-skipped
SetName). This matches ruling 1's text exactly — guard replay places the cursor; tokens govern the new
instance's own in-flight idempotency from there; no stable/recoverable `instanceId` is asserted or
implied.

### Ruling 2 (Q2, in-call hydration dedup is a CORRECTNESS requirement — snapshot-per-evaluation)

**SATISFIED** — `guardResolver.envelope()` (`guard_eval.go:190-201`) memoizes per distinct Core KV key
within ONE `evalGuard` call via `fetched`/`envelopes` maps constructed fresh per call
(`guard_eval.go:33-40`). The godoc on `evalGuard` (`guard_eval.go:13-31`) explicitly documents the
snapshot-per-evaluation correctness property in the wording the ruling specifies: "This dedup is a
correctness requirement, not just an optimization: it pins the whole guard's evaluation to ONE snapshot
of each key, so a concurrent write mid-evaluation cannot make allOf/anyOf straddle two states." The
"no cache ACROSS evaluations" scope is also documented (lines 24-26) as a forward-looking note, phrased
as "today X; a cache would do Y" (not a TODO/FIXME) — consistent with the house no-history-comments
rule and the Dev Notes' phrasing guidance.

### Ruling 3 (Q3, `wipeLoomState` helper in a `_test.go` file)

**SATISFIED** — `wipeLoomState` is defined in `internal/loom/guard_e2e_test.go:22-29` (a `_test.go`
file, package `loom_test`), uses `KVListKeys`+`KVDelete` (the list+delete shape ruling 3 explicitly
sanctions), is `t.Helper()`-marked, and is documented with a comment explaining the "lost entirely"
simulation and citing the Q3 ruling by reference to its substance (not as a "Story 8.3" history
comment — the comment describes current behavior, not a change narrative; compliant with house rules).

## House rules

- **No history/changelog comments**: `git diff HEAD` grep for `Story 8`/`Replaces`/`Previously`/`Was:`/
  `renamed from`/`moved from`/`TODO`/`FIXME` in added lines — **zero matches**. All new comments
  describe current behavior ("is parsed", "is RESERVED", "Today there is no cache...", etc.) or
  forward-looking non-TODO notes per Dev Notes guidance.
- **Contracts untouched**: `git diff HEAD --stat -- docs/contracts` — empty. Confirmed.
- **substrate/refractor/processor diffs EMPTY**: confirmed empty via `git diff HEAD --stat`.
- **docs/components/loom.md updated, present-tense**: new "Guard grammar (shipped)" section
  (`docs/components/loom.md:63-92` in the diff) uses present tense throughout ("is parsed and
  validated", "Guards read only...", "is RESERVED", "Hydration is per-evaluation..."). The deferred
  list at the bottom is rewritten from "Guard expression surface — TBD" to "Starlark guard evaluation
  — the reserved {reads, starlark} escape hatch (validated-and-rejected today)" — accurately reflects
  shipped state, present tense ("today").
- **Pattern-load rejection doctrine**: `pattern.go:134-140` — a malformed/Starlark guard returns an
  error from `validate()`, which (per the unchanged caller contract — `validate()`'s single error
  return rejects the WHOLE `Pattern`) means the pattern never partially loads/executes. Same code path
  as the pre-existing unknown-`kind` rejection at `pattern.go:127-129`. No partial-acceptance branch
  exists anywhere in the diff.

## `validate()` extension — malformed-guard rejection at load

**SATISFIED** — `pattern_test.go:37-71` (table-driven, `TestPatternValidate_AcceptsSystemOpAndUserTask_RejectsGuardsAndUnknownKinds`)
covers: valid guarded systemOp (p3) and userTask (p3b) accepted; unknown top-level key (p3c, `"exists"`);
multi-key guard object (p3d, both `absent`+`present` set); empty `allOf` (p3e); bad path shape (p3f,
`subject.profile.name` — aspect path missing `.data.`); Starlark-reserved (p3g) — each `wantErr: true`
except p3/p3b, with the error formatted via `fmt.Errorf("pattern %q step %d: %w", ...)` so the
underlying `errMalformedGuard`/`errStarlarkReserved` sentinel survives `errors.Is` (separately asserted
for the Starlark case in `TestPatternValidate_StarlarkGuardRejectedAsReserved`, `pattern_test.go:93-110`).

## Summary

| AC / Ruling | Verdict |
|---|---|
| AC#1 grammar + validate() | SATISFIED |
| AC#2 skip semantics + repeat + all-false-complete | SATISFIED |
| AC#3 hydration scope | SATISFIED |
| AC#4 path resolution / loom-local resolver | SATISFIED |
| AC#5 pinned absence semantics | SATISFIED (minor non-blocking coverage nit: `equals` not separately tested against `"0"` string comparand) |
| AC#6 disaster-recovery cursor rebuild | SATISFIED |
| AC#7 linear-only unchanged | SATISFIED |
| AC#8 test gates / substrate+refractor untouched | SATISFIED |
| Ruling 1 (narrow reading) | SATISFIED |
| Ruling 2 (in-call dedup = correctness) | SATISFIED |
| Ruling 3 (wipeLoomState in _test.go) | SATISFIED |
| House rules (comments, contracts, docs, rejection doctrine) | SATISFIED |

**Counts: 8 ACs + 3 rulings + house-rules block = 12 items audited. 12 SATISFIED, 0 PARTIAL, 0 VIOLATED,
0 NOT-VERIFIABLE.** One non-blocking coverage nit noted under AC#5 (does not affect correctness — the
type-aware `equals` comparator is generic and already exercised for number/bool/string).
