# Async external-reply — Increment 3: Weaver pending-suppression + Loom guard

**Status:** 📋 Design (ready for review → build). The increment that makes async **safe for a real
adapter**: it stops Weaver re-dispatching a still-pending external call. Parent: `async-reply-design.md`
(increments 1–2 are on `main`). This is the **live/sensitive** increment (touches Weaver's convergence
dispatch) → full 3-layer review before commit.

## Verdict (the gating question, settled)

**Approach A is a LOCALIZED change, not a Weaver dispatch-model extension** — proceed with A, the
extend-the-mark-lease fork is unnecessary. The decisive precedent is **`freshUntilColumn`**
(`internal/weaver/temporal.go:33-38`): Weaver already recognizes a non-`missing_`, engine-level
companion column on the §10.2 row and uses it to alter behavior (arming an `@at`). A per-gap `inflight_`
companion column read in the dispatch loop to *skip* a gap is the same shape, same mechanism class — no
`GapAction`/§10.8 model change, no playbook binding, no frozen contract touched.

## Two scope corrections vs the parent design

1. **The load-bearing skip is the sweep, not the live handler.** The 30-min `MarkLease` expiry
   (`reconciler.go:17`) → `reclaim` (`reconciler.go:227-316`) is the actual re-dispatch path for a
   long-pending call. The suppression MUST be added to **both** the lane-1 dispatch loop
   (`evaluator.go:98-106`) **and** `reclaim` (`reconciler.go:257-264`). Skipping only the handler leaves
   the bug. This is an explicit AC with its own test.
2. **Loom's externalTask deadline is already disarmed after creation** (increment 1):
   `onExternalTaskDeadline` (`internal/loom/engine.go:1312-1320`) disarms without touching the cursor
   once the instanceOp tracker exists — "the bounded creation wait is over and the unbounded bridge wait
   begins." So there is **no horizon to resize**; the parent design's "48h check trips Loom as stuck"
   premise is already handled. Increment 3's Loom work reduces to a **guard/regression test** (an
   externalTask that stays Pending past `CreateTaskTimeout` (60s default) is NOT failed by Loom).

## Design — what to build

### A. Lens — per-gap in-flight columns (package-local, `packages/lease-signing/lenses.go`)
Add `inflight_bgcheck` / `inflight_payment`: a service instance of the family with a `.dispatch` aspect,
**no `.outcome`**, and `deadline > $now`. Inside the **existing no-WHERE `providedTo` fan** (NOT a new
filtered OPTIONAL MATCH — that drops the anchor; the FR58 no-drop guard `lens_cypher_test.go:464-490`
must stay green), mirroring the `missing_*` CASE aggregation:

```cypher
count(DISTINCT CASE WHEN inst.family.data.value = 'backgroundCheck'
  AND inst.dispatch.data.deadline > $now AND inst.outcome.data.status = null
  THEN inst.key ELSE null END) AS bgInflight,
count(DISTINCT CASE WHEN inst.family.data.value = 'payment'
  AND inst.dispatch.data.deadline > $now AND inst.outcome.data.status = null
  THEN inst.key ELSE null END) AS payInflight
```
RETURN `(bgInflight > 0) AS inflight_bgcheck`, `(payInflight > 0) AS inflight_payment`; add both to
`Output.BodyColumns` (`lenses.go:47`). Conventions: `= null` is the engine's absence test (not `IS
NULL`); `deadline > $now` is the same lexical-RFC3339 compare as `validUntil > $now` (the dispatchOp
already canonical-UTC-normalizes `deadline`); `inflight_*` deliberately omits the bgcheck freshness
predicate (a pending call's give-up horizon is its own `deadline`, not the outcome `validUntil`).

### B. Weaver — the companion-column skip (generic, `internal/weaver`)
- New engine-recognized **suppression companions**: for gap `missing_<g>`, `inflight_<g>` (a call is in
  flight) and `exhausted_<g>` (retry budget spent — §E); both prefix-swaps (define
  `inflightColumnPrefix`/`exhaustedColumnPrefix` near `gapColumnPrefix`, `state.go:17`). They are §10.2
  BodyColumns the engine reads, like `freshUntil` — **not** `gaps` keys, so
  `openGapColumns`/`markCandidateColumns` (which match `missing_` only) never treat them as gaps, never
  mark them. Document the convention in `docs/components/weaver.md` (where `freshUntilColumn` lives).
- **Skip site 1 — lane-1 dispatch loop** (`evaluator.go:98-106`): before `dispatchGap`,
  `if e.gapSuppressed(targetID, row, col) { continue }`, where `gapSuppressed` ORs `row[inflight_<col>]`
  and `row[exhausted_<col>]` via the existing `boolColumn` helper (`evaluator.go:281`; a non-bool/absent
  value reads false → dispatch proceeds, the safe default).
- **Skip site 2 — the sweep `reclaim`** (`reconciler.go:257-264`): the same `gapSuppressed` check beside
  the existing `violating` gate. **Load-bearing** (see scope correction #1). `reclaim` already re-reads
  the row, so the companion columns are in hand.
- The gap stays `violating` (the applicant is not satisfied — the check could still fail); only
  re-dispatch is suppressed.

### C. Loom — guard only (generic, `internal/loom`)
No horizon change. Add a regression test: an externalTask whose instanceOp committed and whose adapter
is Pending past `CreateTaskTimeout` (60s) is NOT failed by Loom (the `engine.go:1312-1320` disarm holds).
Confirm increment 1/2 didn't already cover it.

### D. End-to-end proof (the live test)
An async variant of the lease-convergence e2e: an async-mode fake bgcheck (FakeAsyncCheck-style) driven
through the **real** Weaver + lens + bridge, asserting: (1) **no second dispatch** while the call is
in-flight even across a mark-lease expiry / sweep tick (the core AC), (2) the poll resolves → the gap
closes, (3) a timeout → `failed` outcome → the gap stays violating. **Production stays sync** —
FakeAsyncCheck remains test-only (`cmd/bridge/main.go` unchanged); flipping the lease demo to async is a
trivial, deliberately-separate follow-up so increment 3 lands the safety machinery without changing the
demo default.

### E. Bounded auto-retry (Andrew's constraint — the retry budget)
The wedged-state retry (open-question §6) must NOT loop forever. The lens already projects one service
instance per attempt, so it counts the **failed** ones and projects a *second* per-gap suppression
companion, `exhausted_<g>`:

```cypher
count(DISTINCT CASE WHEN inst.family.data.value = 'backgroundCheck'
  AND inst.outcome.data.status = 'failed' THEN inst.key ELSE null END) AS bgFailed,
-- … same for payment …
(bgFailed >= maxBgcheckRetries) AS exhausted_bgcheck,
(payFailed >= maxPaymentRetries) AS exhausted_payment
```
`maxBgcheckRetries` / `maxPaymentRetries` are **package constants** (**3** per family — Andrew-ratified
default, 2026-06-20; tunable, separate per family, baked like `bgcheckFreshnessWindow`). Weaver's dispatch skip (both sites — the lane-1 loop AND the sweep
`reclaim`, §B) fires on **`inflight_<g>` OR `exhausted_<g>`**. So after N failed attempts Weaver **stops
auto-dispatching**: the gap stays `violating` (the applicant is NOT approved) and `exhausted_<g>` is the
operator-/Loupe-visible **"needs human escalation"** signal. A human (or a control-plane action) resolves
it — a manually-submitted check that *completes* closes the gap and ends the chain. This is a **lifetime**
cap (count of failed instances), not a windowed one — simplest; the deliberate terminal is "stop +
escalate," NOT "silently reject." Both `inflight_` and `exhausted_` are recognized suppression companions
(prefix-swap from `missing_`); Weaver ORs them in the dispatch gate. Add lens tests: N-1 failures →
`exhausted=false` (still dispatching); N failures → `exhausted=true` + `violating=true` + no dispatch.

## Open-question calls (made)

1. **Per-gap `inflight_<g>` (prefix-swap), not a single scalar** — a target with bgcheck AND payment
   needs per-gap resolution; prefix-swap stays generic with zero playbook config. ✅ decided.
2. **Convention `inflight_`** — documented in `weaver.md` alongside `missing_`/`freshUntil`. ✅ decided.
3. **The sweep-skip is an explicit AC** with a lease-expiry test (let the mark's lease expire; assert NO
   reclaim fires while `inflight_*=true`). ✅ decided.
4. **Loom = guard test only** (deadline already disarmed-after-creation). ✅ decided (scope reduction).
5. **No eager `@at` on `deadline`** — increment 2's bridge timeout is the eager driver (it posts the
   terminal at `deadline`, a CDC touch → re-projection flips `inflight_*`); the lens predicate
   re-opening on the next touch suffices. ✅ decided.
6. **The wedged-state seam — DECIDED (Andrew, 2026-06-20): auto-retry on timeout as a fresh call, but
   BOUNDED.** When `deadline ≤ $now`, `inflight_*` flips false and Weaver resumes dispatching → a
   **fresh** external call (a NEW claim vertex / handle): intended retry-on-timeout escalation (the
   parent gap-state table: a `failed`/`timedOut` outcome → "policy re-dispatch, never a silent
   auto-resubmit of the same vendorRef"; a fresh claim is a new vendorRef). The old claim is terminally
   `failed` (create-only `.outcome`, first-writer-wins); the new claim is independent. No double-dispatch
   of the *same* in-flight call. **Andrew's constraint: this must NOT retry indefinitely** → the retry
   budget in §E below caps it.

## Verification
`go build`, `make vet`, `golangci-lint`, `go test ./internal/weaver/... ./packages/lease-signing/...
./internal/loom/...`, the new async e2e, and the **lease-convergence e2e** (the sync vertical must stay
green). Contract: none touched (the `inflight_` convention is a §10.2 BodyColumn like `freshUntil`,
within the existing model). Review: **full 3-layer** (live + touches Weaver convergence).

---

## Revision (2026-06-20): budget mechanism **B** + 3-layer review fixes

The first build landed (lens `exhausted_<g>` = a **lifetime** failed-instance count) and the full
3-layer review found two real issues + test gaps. The budget is re-homed per Andrew's ruling (option
**B**), and the four review fixes apply. **The production logic is otherwise confirmed correct** (all 7
ACs passed; both skip sites present; the inflight suppression, null semantics, and cap boundaries
checked out).

### Why B (the lifetime lens-count is wrong, and a lens reset isn't expressible)
A lifetime `count(failed instances) >= cap` never resets on success: a bgcheck that fails ≥cap times,
is then completed, then goes **stale** (the lens is freshness-aware) reopens `missing_<g>` while
`exhausted_<g>` is still true from the old failures → the renewal is **wedged forever**. A true
reset-on-success ("failures since the last success") cannot be a lens predicate: the full engine has no
`max`/`size` aggregate (`executor.go` dispatches only `collect`/`count`), and the op that records the
outcome — the bridge's read-free, type-agnostic replyOp — cannot write applicant state to reset a
counter. So the budget moves to **Weaver-state**, where gap-close *is* the reset.

### Budget mechanism B — Weaver-state dispatch-count (generic Weaver)
- **A per-`(target, entity, gapColumn)` dispatch-count in `weaver-state`.** Incremented on each actual
  dispatch (in `fireEpisode`, where the mark is CAS-created and the op fires — so the count tracks real
  attempts, one per anti-storm window). It is **chain-scoped**: it persists across mark-lease/TTL
  expiries (unlike the mark, which is TTL-bound for anti-storm recovery — the count must survive a
  reclaim so a multi-attempt chain accumulates), and is **DELETED on gap-close** by `clearClosedMarks`
  (the same close path that deletes the mark) — i.e. a success resets it. Give it a long TTL backstop —
  well past a max chain's duration, which is `cap × CallDeadline` (the **bridge** give-up horizon paces
  each attempt's `failed` outcome, NOT the mark lease; ≈72h at defaults) — only to GC an orphaned entity,
  never to expire mid-chain (the implemented `256 × lease` ≈ 128h clears 72h with ~1.8× headroom).
- **The cap is a lens column, package-owned.** The lens projects `maxretries_<g>` (a constant column,
  baked from `retry_budget.go` like the freshness window) so the *policy* stays in the package and **no
  contract changes**; Weaver reads it off the row (like `freshUntil`).
- **The dispatch gate** (the existing skip at BOTH sites — `evaluator.go` lane-1 + `reconciler.go`
  `reclaim`) skips a gap when `inflight_<g>` (the row) **OR** `dispatchCount(target,entity,gap) >=
  row[maxretries_<g>]`. After cap attempts Weaver stops dispatching; the gap stays `violating` — the
  operator-visible "needs escalation" terminal. A human-submitted check that completes → gap closes →
  the count is deleted → a later renewal starts a fresh budget.
- **Drop** the lens `exhausted_<g>` columns; `retry_budget.go` stays (now feeds `maxretries_<g>`).

### The four review fixes (apply regardless of budget mechanism)
1. **`inflight_<g>` presence-based (the FIX-FIRST correctness catch).** Was `dispatch.deadline > $now AND
   outcome.status = null` — which flips false when the deadline passes *even if the call is still
   unresolved*, so a dead/slow bridge that never posts the timeout outcome would let Weaver double-dispatch
   against the vendor. Change to **`inst.dispatch.data.vendorRef <> null AND inst.outcome.data.status =
   null`** (dispatch-present AND unresolved; drop the deadline term). Re-dispatch is then driven by the
   `failed` outcome *actually landing*, and a stuck-pending call waits for the bridge instead of being
   duplicated. (`<>` is `!equalsAny`; `vendorRef <> null` is true iff the `.dispatch` aspect exists —
   verified `executor.go:1354,1376`.)
2. **CI-flaky test.** The new `Never`-goroutine helper calls `require.NoError` on a context-canceled KV
   read, misattributing the failure to the next test. Make `serviceOutcomes` (+ siblings) tolerate
   `errors.Is(err, context.Canceled)` (return empty) or stop polling once `h.ctx` is done.
3. **Escape-hatch test.** Add a deterministic test that a completed check **resets** the budget: under B,
   seed a chain to the cap, then a completed-fresh instance → gap closes → the dispatch-count is deleted →
   a subsequent reopen is dispatchable again. (Replaces the dropped "human completes → chain ends" e2e.)
4. **E2e margin.** Widen the no-double-dispatch-across-sweep timing (larger `MarkLease`/`PollsUntilResolved`,
   or assert the sweeper observed the in-flight mark) so the AC can't silently stop exercising the sweep
   under CI load.
