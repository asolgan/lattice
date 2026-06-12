# Story 8.3 — Blind Hunter (Adversarial, diff-only)

Reviewer: Blind Hunter (`bmad-review-adversarial-general`), diff-only / no story or wider-repo context.

Scope: `git diff HEAD` for `internal/loom/{pattern.go,pattern_test.go,engine.go,export_test.go,doc.go}`,
`docs/components/loom.md`, plus untracked `internal/loom/{guard.go,guard_eval.go,guard_test.go,guard_e2e_test.go}`
read in full.

---

## Findings

### 1. [LOW] Duplicate JSON keys in a guard object are silently accepted — last value wins, no error
**File:** `internal/loom/guard.go:110-117` (`parseGuard`, `guardEnvelope` decode)

`dec.DisallowUnknownFields()` does **not** reject duplicate keys — `encoding/json`'s decoder simply
overwrites the struct field on each occurrence, keeping the last one. Verified empirically:

```go
// {"absent":"subject.data.a","absent":"subject.data.b"}
// decodes to env.Absent == "subject.data.b" (the SECOND value), err == nil
```

Because the "exactly one of absent|present|equals|allOf|anyOf|not" check (`guard.go:126-148`) counts
populated **struct fields**, not raw JSON object keys, a guard object with a duplicated key
(`{"absent":"X","absent":"Y"}`) parses successfully as a single `guardAbsent` node on path `Y` — the
`X` occurrence is silently dropped. Since "a malicious pattern is package DATA" (per the brief), this
is a guard whose on-the-wire shape looks like it tests two different paths but actually only tests
one, with no diagnostic. Pattern authors/reviewers reading the raw JSON could be misled about what a
loaded pattern actually does.

**Why it matters:** `parseGuard` documents itself as enforcing "exactly one declarative shape" — a
duplicate-key object is not "one shape", it's an ambiguous shape that happens to decode to one. The
grammar doc (`docs/components/loom.md`, new "Guard grammar" section) doesn't mention duplicate-key
handling at all, so this silent-acceptance is undocumented behavior.

**Verdict:** Real, low-severity (no crash, no incorrect *execution* vs. the parsed AST — but a
silent shape-collapse that the validate-at-load-time doctrine ("a half-understood pattern never
partially executes") arguably should catch, since `errMalformedGuard` already exists for exactly this
class of "ambiguous shape" rejection).

---

### 2. [LOW] `inst.Cursor` is left stale (not advanced to `runCursor`) when `advanceToRunnableStep` returns `completed=true`, producing an inconsistent persisted terminal Cursor
**File:** `internal/loom/engine.go:404-421` (handleTrigger), `:605-614` (advance)

In both call sites, when `advanceToRunnableStep` returns `completed=true`, `inst.Cursor` is **not**
set to `runCursor` before `e.complete(ctx, inst, pattern, ...)` is called — only the `!completed`
branch does `inst.Cursor = runCursor` (engine.go:421, :613). `complete()` persists `inst` as-is via
`e.state.transition` (engine.go:767), including whatever `Cursor` value it currently holds.

Concretely:

- **handleTrigger, all-guards-false** (engine.go:404-420): `inst.Cursor` is created as `0`
  (engine.go:397) and never reassigned before `complete()` — a `status=complete` instance is
  persisted with `cursor: 0`, even though every step (e.g. all N steps) was evaluated and skipped.
  `TestGuardE2E_AllGuardsFalseCompletesOnTrigger` (guard_e2e_test.go:224-272) does **not** assert on
  `inst.Cursor`, so this is untested.

- **advance, mid-pattern all-remaining-guards-false** (engine.go:605-614): `inst.Cursor++` advances
  by exactly one before `advanceToRunnableStep` is called; if that single increment lands at
  `len(pattern.Steps)` the loop immediately returns `(len, true, nil)` and `inst.Cursor` (== `len`)
  happens to already be correct (this is the case the existing
  `TestGuardE2E_FalseGuardSkipsStep`/`DisasterRecoveryCursorRebuild` tests exercise, where the
  *guardless* tail step is what actually completes via its token, not this skip-to-end path). But if
  `inst.Cursor++` lands on a step whose guard is false and there are *multiple* remaining steps all
  guard-false (e.g. a 4-step pattern, cursor 0 just ran, cursor becomes 1, and steps 1-3 are all
  guard-false), `advanceToRunnableStep` returns `(4, true, nil)` while `inst.Cursor` stays at `1` —
  the persisted terminal record shows `cursor: 1` of 4 steps for a `status=complete` instance.

**Why it matters:** `Cursor` is a durably-persisted field (`internal/loom/state.go:43`,
`json:"cursor"`) read back by `getInstance` and logged (`engine.go:696,744,795` etc. all log
`inst.Cursor`). A completed instance whose `cursor` field reads `0` or `1` out of N is misleading for
any future debugging/observability/audit reading of loom-state — "how far did this instance get"
cannot be answered from the persisted record once guard-skip-to-completion paths are exercised. The
new `advanceToRunnableStep` doc comment (engine.go:617-631) explicitly frames `runCursor` as "the
cursor of the first step that should RUN — or `completed=true` if the cursor runs off the end", which
implies the off-the-end value IS meaningful, yet it is discarded on the completed path.

**Verdict:** Real bug in the new code (asymmetric handling of `runCursor` between the two return
branches), low severity (no functional/correctness impact on advancement, tokens, or idempotency —
purely a persisted-bookkeeping inconsistency), but plausibly worth a one-line fix
(`inst.Cursor = runCursor` before calling `complete` in both branches) given the field is durable and
logged.

---

### 3. [INFO / no finding — verified safe] Deep guard nesting does not cause unbounded recursion
**File:** `internal/loom/guard.go` (`parseGuard` / `parseGuardList` recursion), `guard_eval.go`
(`eval` recursion)

Checked empirically: `encoding/json`'s `Decoder.Decode` enforces its own ~10000-level nesting depth
limit and returns `"invalid character '{' exceeded max depth"` **even when decoding into a
`json.RawMessage` field** (as `guardEnvelope.Not/AllOf/AnyOf` do). A maliciously deep
`{"not":{"not":{"not": ... }}}` guard therefore fails at the very first `dec.Decode(&env)` call inside
`parseGuard` with `errMalformedGuard`, before any of `parseGuard`'s own recursion happens. No
finding — the "recursion safety in composite evaluation" risk is mitigated by the stdlib decoder's
own depth cap, which both `parseGuard` and (transitively) `evalGuard`'s `eval` inherit.

---

### 4. [INFO / no finding — verified safe] Path-parser segment handling
**File:** `internal/loom/guard.go:255-278` (`parseGuardPath`)

Verified the boundary cases:
- `"subject."` (trailing dot) → `rest=""` → `strings.Split("", ".")` → `[""]` (len 1) → falls to
  `default` → rejected. Correct.
- `"subject..data.x"` (double dot / empty aspect segment) → `rest="..data.x"`... actually
  `strings.Split(".data.x", ".")` → `["", "data", "x"]` (len 3) → case-3 branch checks
  `segs[0] == ""` → true → rejected via the "aspect path must be subject.<aspect>.data.<field>"
  error. Correct — an empty-aspect-segment path cannot slip through as `aspect: ""`.
- `"subject.data.x.y"` (too deep) → len 3, `segs[1] != "data"` (it's `"x"`) → rejected. Covered by the
  existing `"too deep"` test case (guard_test.go:49).

No injection vector found: the path is split on literal `.` only, and both legal shapes are checked
structurally (segment count + fixed literal `"data"` at the expected index) — no field name can
masquerade as a `data`/aspect separator.

---

### 5. [INFO / no finding — verified safe] KV-miss vs KV-error are correctly distinguished in hydration
**File:** `internal/loom/guard_eval.go:203-223` (`fetchEnvelope`)

`fetchEnvelope` uses `errors.Is(err, substrate.ErrKeyNotFound)` to map a genuine miss to `(nil, nil)`
(absent), and returns a wrapped non-nil error for anything else
(`fmt.Errorf("loom: guard hydrate %q: %w", key, err)`). Cross-checked against
`internal/substrate/kv.go:36-54` (`KVGet`): `ErrKeyNotFound` is the only sentinel `KVGet` wraps for a
genuine miss; all other failures (context cancellation, JetStream errors, etc.) come back as a
different wrapped error and are NOT silently swallowed — `evalGuard` → `advanceToRunnableStep`
propagates them up to `e.logger.Error(...); return substrate.Nak` (engine.go:405-408,
:607-609/handled via `advance`'s caller). No KV-miss/KV-error conflation found.

---

### 6. [INFO / no finding] No banned history/changelog comments introduced
Searched all six changed/new files for "Story 8.x", "Replaces", "Previously", "Was:", "renamed from",
"moved from" patterns. The only hit is `internal/loom/doc.go:14` — `"Module boundary (Contract /
Story 8.1 AC #8): ..."` — which is **pre-existing, unmodified context** (not part of this diff's
added/changed lines; confirmed via `git diff` hunk boundaries). No new violations.

---

### 7. [LOW — documentation accuracy, not code] `equals` with an object/array comparand always evaluates false regardless of the resolved value's shape
**File:** `internal/loom/guard_eval.go:229-247` (`jsonValuesEqual`)

`parseEquals` (guard.go:188-210) does not restrict `value`'s JSON type — `{"equals":{"path":"...",
"value":{"nested":1}}}` parses successfully (passes `validate()`). At evaluation time,
`jsonValuesEqual`'s `default` branch returns `false` whenever `a` (the resolved field) is itself an
object/array, **without checking `b`'s type at all** — so `{"equals":{"path":"subject.data.profile",
"value":{"nested":1}}}` against a field whose live value is literally `{"nested":1}` still evaluates
to `false` (mismatch), per the comment "Objects/arrays are not legal equals comparands... fall back to
strict mismatch."

This is a deliberate, documented, fail-closed choice (an `equals` guard against an object-valued field
can never spuriously evaluate `true`), and the §10.5 grammar comment says the path "reads a leaf
field... not a nested path" — so object-valued `data.<field>` entries are arguably out of grammar
scope already. Not a bug, but worth flagging: a pattern author who writes an `equals` guard against an
object-shaped field gets a guard that is **always false** (parses fine, never matches), with no
parse-time signal that the comparand shape is meaningless for that path. Low severity — purely a
silent-no-op-guard authoring trap, not a security or correctness issue in the engine itself.

---

## Verdict Table

| # | Severity | File:Line | Summary | Status |
|---|----------|-----------|---------|--------|
| 1 | LOW | `internal/loom/guard.go:110-148` | Duplicate JSON keys in a guard object silently collapse to "last wins", no `errMalformedGuard` | Real, unresolved |
| 2 | LOW | `internal/loom/engine.go:404-421`, `:605-614` | `inst.Cursor` not set to `runCursor` on the `completed=true` branch → inconsistent persisted Cursor for `status=complete` instances reached via guard-skip | Real, unresolved |
| 3 | INFO | `internal/loom/guard.go` (parse), `guard_eval.go` (eval) | Deep-nesting recursion — mitigated by stdlib `encoding/json` depth cap (verified) | No finding |
| 4 | INFO | `internal/loom/guard.go:255-278` | Path-parser segment edge cases (trailing dot, double dot, too-deep) all correctly rejected (verified) | No finding |
| 5 | INFO | `internal/loom/guard_eval.go:203-223` | KV-miss vs KV-error correctly distinguished via `errors.Is(ErrKeyNotFound)` | No finding |
| 6 | INFO | all changed/new files | No new banned history/changelog comments | No finding |
| 7 | LOW | `internal/loom/guard_eval.go:229-247` | `equals` against an object/array-valued field is always-false (silent no-op guard), documented/fail-closed but no parse-time signal | Real, low severity, by-design |

**Counts:** 2 real LOW findings (#1, #2), 1 real LOW documentation/authoring-trap observation (#7,
by-design/fail-closed), 4 explicitly-verified no-findings (#3-#6).

No CRITICAL/HIGH/MEDIUM findings — the JSON parsing, path parsing, recursion, and KV-hydration error
handling all held up under adversarial probing with concrete test inputs.
