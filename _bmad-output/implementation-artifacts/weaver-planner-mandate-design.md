# Weaver planner mandate — from dispatcher to solver

**Status:** 🔭 Proposed (exploration with Andrew, 2026-07-04). Nothing here is ratified; Contract #10
amendments are drafted below but NOT staged. `docs/components/weaver.md` is untouched until Fire 1
lands code.

**Audience:** architects + the session-per-fire implementers. Each fire below is scoped so a fresh
session can author its own handoff brief from this doc + the referenced contracts alone — no
conversation history required (token-budget doctrine: this doc is the durable context).

---

## Motivation — the dispatcher critique

Weaver's charter says *declarative convergence*, but today only **detection** is declarative
(targets are Lenses). **Remediation** is a hand-authored static table: §10.8 maps each
`missing_<g>` gap to exactly one fixed action (`packages/lease-signing/targets.go` — five gaps,
five hardcoded actions). Sequencing lives in the lens predicates (e.g. `missing_listingLeased`
opens only when the applicant gaps are closed AND the landlord approved). Semantically that is a
guarded linear procedure: each gap column is a guard, each playbook entry is a step — i.e. a Loom
pattern with guards, plus a standing CDC trigger and a retry budget. The Strategist is a table
lookup, not a strategist: Weaver decides *whether/when* to act (marks, leases, budgets, timers)
but never *what* to do.

What Loom-with-guards genuinely cannot replicate today — and what we keep — is the standing,
fleet-wide, level-triggered nature: a target covers every candidate entity present and future;
freshness re-*opens* closed gaps; a Loom instance is one-shot per subject. That is the actuation
substrate. The mandate change adds the missing brain.

## The mandate

> Given a goal predicate and the graph's own catalog of operations, **synthesize, verify, and
> continuously re-plan** the path that closes a gap — and **prove the system is actually
> converging**.

Three new powers, none expressible as playbook entries or Loom guards:

1. **Derive *what* to do** from declared (and observationally verified) operation effects —
   selection and plan synthesis instead of table lookup.
2. **See across rows and targets** — contraction monitoring, oscillation detection between
   targets, fleet-level admission control.
3. **Close the loop** — observe whether a dispatched action actually flipped its gap; demote
   actions whose declared effects stop matching reality; escalate to the Augur only when the
   declared model runs out.

## Planner decisions of record (PD1–PD8)

### PD1 — In-place evolution, NOT a parallel Weaver 2.0 engine

The 2026-07-02 arch review rates Weaver the healthiest, best-conformed engine; its machinery
(marks/leases/OCC, reclaim backoff, temporal lane, sweep, control plane, Augur dispatch
validation) is exactly the actuation substrate the planner needs beneath it. A parallel engine
would re-port all of that AND create a double-dispatch fencing problem on the shared
`weaver-targets` / `weaver-state` buckets. The confidence-before-cutover goal is met instead by
**shadow mode** (PD5, Fire 4): the planner runs beside the playbook lookup on real traffic,
logging what it *would* have chosen, never dispatching, until a target is explicitly flipped.
Cutover is **per-target** (a §10.8 mode field), reversible via the existing control plane —
never a big-bang engine swap. The seam is `internal/weaver/strategist.go`: detection (lanes,
lenses) and actuation (marks, actuator, budgets) do not change.

### PD2 — The planner core is deterministic Go; determinism is load-bearing, not aesthetic

Plan synthesis is classical goal regression (STRIPS/GOAP-class) over a small closed catalog —
package data, dozens of actions — with bounded depth and **canonical tie-breaking** (sort
candidates by cost, then lexicographic action ref). The plan MUST be a pure function of
`(row snapshot, catalog snapshot, confidence stats snapshot)` because Weaver's idempotency
machinery assumes re-deciding reproduces the decision: deterministic `requestId`s derived from
mark revisions, Contract #4 dedup collapsing re-fires, sweep reclaims re-dispatching the *same*
episode. A nondeterministic planner breaks replay — a reclaim could synthesize a different plan
and double-act. No LLM, no wall-clock, no map-iteration order anywhere in the decision path.

### PD3 — Effect vocabulary = the Loom guard grammar atoms, nothing more

Declared effects (and planner-checked preconditions) use exactly the §10.5 guard grammar:
`absent` / `present` / `equals` atoms over the two path shapes (`subject.data.<field>`,
`subject.<aspect>.data.<field>`), composed with `allOf`/`anyOf`/`not`, with the pinned absence
semantics. Rationale: (a) plan-time entailment over this vocabulary is trivially decidable and
fast; arbitrary Starlark effects make it undecidable — those cases fall back to
dispatch-and-verify or the Augur; (b) one grammar, one evaluator lineage, one set of absence
semantics across Loom guards, planner preconditions, and declared effects. The Starlark escape
hatch stays RESERVED here exactly as it is in Loom.

### PD4 — Multi-step plans execute as content-addressed `meta.loomPattern` vertices; zero Loom engine change

A synthesized plan compiles to an ordinary Loom pattern definition. Weaver submits it as a
`meta.loomPattern` vertex **through the Processor** (auditable, reviewed write path — P2), keyed
by content hash: `plan-<hash(canonical plan JSON)>` — so the same (state, catalog) re-decision
maps to the same vertex and re-fires collapse instead of duplicating patterns. `triggerLoom`
then proceeds exactly as today; **Loom's definition pinning does the rest** (the instance pins
at start and drains under its definition even if the plan vertex is later retired). Plan
vertices are GC'd when no live instance pins them and no violating row would re-derive them
(Fire 6 defines the sweep). Semantic versioning falls out for free: a changed world produces a
different plan hash, a new vertex — the loom.md "new pattern id for a semantic redefinition"
doctrine, automated.

### PD5 — Playbooks remain the fast path and the override; planner engagement is additive, opt-in, per-target

§10.8 today is not "targeting Weaver 1.0" — it is pure data with no engine assumption beyond
lookup, so it extends additively (draft below). Precedence per gap: an explicit single `Action`
(today's shape) always wins — the operator override. A gap may instead declare `candidates`
(Fire 5: planner selects one step among declared candidates) or `goal` (Fire 6: planner
synthesizes multi-step plans from the catalog). A target-level `mode: shadow | planned` field
gates dispatch; absent mode = today's behavior, bit-for-bit. Existing targets keep working
untouched forever.

### PD6 — Closed-loop effect verification with deterministic confidence bookkeeping

Weaver already observes both legs: the dispatch (its own act) and the outcome (the lens
re-projection of the row). Fire 2 records, per `(targetId, gapColumn, actionRef)`, dispatch and
gap-close counters in `weaver-state` (new reserved key shape — must be added to Contract #10
§10.3's reserved-shape list; the arch review already flagged undocumented reserved shapes as
drift). "Confidence" is these counters + a deterministic decay rule keyed on observed events
(never wall-clock sampling). Consumers: Health issues on chronic non-closure ("actions commit
but the goal never flips — suspect lens/effect mismatch", today's silent
`violation-never-flips` failure mode made loud), and the Fire 5 ranking term.

### PD7 — Uncertainty is handled by level-triggered replanning, never contingent planning

No branch trees, no probabilistic planning. Plan for the current state; dispatch; if the world
diverges (a payment declines, a check fails), the gap is still violating, the row re-projects,
and the next tick replans from the *new* state — possibly choosing a different candidate
(PD6's confidence demotes the failed one). Weaver's existing level-triggered nature IS the
feedback loop; the planner just makes each tick's decision state-dependent.

### PD8 — AI appears only at the model's boundary, behind the existing Augur human gate

The deterministic planner's honest failure is "no plan exists in the declared model" (novel
gap, or declared effects proven wrong by PD6). That failure **escalates to the Augur** — which
also finally wires the ratified-but-dormant `exhausted` escalation trigger the arch review
flagged (backlog `weaver-exhausted-escalation-and-model`), joined by a new `noPlan` trigger.
The Augur proposes; humans review; approved plans dispatch through the existing
proposal-scoped machinery. The compounding move: a synthesized plan that repeatedly succeeds
(deterministic counter threshold) generates a **playbook-promotion proposal** through the same
reviewed write path — operational knowledge becomes platform capability under review. The
autonomy boundary (`augur.autoApply`) stays Andrew-gated, unchanged.

## What does NOT change

- Targets are Lenses; Weaver is never a cypher runtime (D4).
- Marks/leases/OCC, dispatch-count budgets, `inflight_<g>`, the temporal lane, the sweep, the
  control plane — all carried forward as-is beneath the planner.
- The Bridge stays the sole egress; external work is still `triggerLoom` of an `externalTask`.
- No branching added to playbooks (that would re-invent Loom inside a table).
- Loom engine: zero changes (PD4).
- P1/P2 boundaries; `weaver` imports only `substrate/*`.

## Contract touchpoints (drafts to stage at each fire, not before)

| Surface | Amendment | Fire |
|---------|-----------|------|
| Op DDL (Contract #8 / DDL shape) | optional `effects: [<guard>…]` block, guard-grammar atoms only; pkgmgr validates grammar + paths | 1 |
| Contract #10 §10.3 | new reserved `weaver-state` key shapes: effect counters (`<targetId>.<gapColumn>.<actionRef>.__effect`), planner shadow marks if any | 2, 4 |
| Contract #10 §10.8 | additive per-gap `candidates` / `goal` fields; target-level `mode: shadow\|planned`; precedence rule (explicit Action > candidates > goal) | 4–6 |
| Contract #10 (new §) | plan-vertex convention: `meta.loomPattern` content-hash naming, GC rules | 6 |
| Contract #10 §10.2 | optional priority/urgency column convention for arbitration (prefix-swap-class, like `freshUntil`) | 8 |

## The fire ladder

Ordering doctrine: value early, dispatch-behavior risk late. Fires 1–4 change no dispatch
decision and are safe in any session. Every fire updates `docs/components/weaver.md` (+ contract
text it touches) in the same commit as its code — drift is a documentation bug. Each fire's
detailed handoff brief is authored just-in-time by its implementing session from this section
(BMAD session-per-story; do NOT pre-author all briefs — earlier fires will move the ground).

### Fire 1 — Declared effects on op DDLs (data only)

Op DDL gains optional `effects` (PD3 grammar). `pkgmgr` validates: parseable guards, legal path
shapes, reject-wholesale on malformed (same doctrine as Loom pattern load). Consumed by nothing.
Reference data: author effects for the lease-signing ops (`SignLease` → `.signature` present,
`SetListingStatus` → `.listing.status equals leased`, `RecordIdentityPII` → `.ssn` present).
**Acceptance:** install-time validation tests; malformed-effect rejection is loud (Health/install
error); zero engine behavior change. **Risk: none.**

### Fire 2 — Effect-verification loop (observability)

On each actual dispatch (lane-1 CAS-create-and-fire AND sweep reclaim — both legs, mirroring the
dispatch-count seam), increment the `__effect` dispatch counter; on gap-close observation
(`clearClosedMarks` path — the same level-reconciled close seam that resets the retry budget),
increment the close counter attributing to the last-dispatched actionRef. New heartbeat metrics;
a Health issue when a `(target, gap, action)` crosses a chronic non-closure threshold — making
the documented "violation never flips" package-bug class operator-visible. Contract #10 §10.3
reserved-shape amendment staged. **Acceptance:** counters survive restart; sweep GC of orphaned
counters (gap gone, target revoked — join the existing orphan legs); e2e proving a
never-closing fixture raises the issue. **Risk: none** (no dispatch decision touched).

### Fire 3 — Planner core as a pure library

`internal/weaver/planner`: `ActionSpec{Ref, Kind, Pre, Effects, Cost}`, catalog snapshot type,
goal-regression search with bounded depth, canonical tie-breaking (PD2), entailment over PD3
atoms. Pure functions; no engine wiring, no I/O; imports at most `substrate` types. Exhaustive
table tests including determinism proofs (permuted catalog input → identical plan) and
no-plan-found results as first-class returns. **Acceptance:** table tests green; a fuzz/property
test asserting plan(state,catalog) is stable under catalog reordering. **Risk: none** (unwired).

### Fire 4 — Shadow mode

§10.8 gains `mode: shadow` + per-gap `candidates`/`goal` (parsed, validated at install like the
rest of §10.8 — reject-and-alert). In shadow, the evaluator runs the planner beside the playbook
lookup and records the divergence (chosen-by-table vs would-be-chosen-by-planner) — heartbeat
metrics + a queryable log surface (Health doc or lens-friendly op — implementer's brief
decides), **never dispatching from the planner**. This is the parallel-run: per-target
confidence on real traffic before any cutover. **Acceptance:** shadow target dispatches
bit-identically to today; divergence stats visible; zero shadow writes outside weaver-state.
**Risk: none.**

### Fire 5 — Single-step selection live (per-target cutover begins)

`mode: planned` + per-gap `candidates`: the Strategist asks the planner to pick ONE candidate
by (precondition satisfaction against the row/graph snapshot, PD6 confidence, declared cost;
canonical tie-break). Everything downstream is unchanged — same mark, same budget
(`maxretries_<g>` now bounds the *gap*, spanning candidates), same actuator. The emergent
behavior: a failing candidate's confidence drops and the next tick's replan (PD7) falls through
to the next candidate — an unauthored escalation ladder. **Acceptance:** e2e — candidate A
declines twice (fixture), engine falls to candidate B without human action; explicit-Action
gaps and mode-less targets bit-identical to today; revert = flip mode back via control plane.
**Risk: low** (dispatch selection changes only for opted-in targets).

### Fire 6 — Multi-step plans as content-addressed Loom patterns

Per-gap `goal` with no candidates: full goal regression over the installed op/pattern catalog
(ops with declared effects + Loom patterns as macro-actions). Compile plan → canonical JSON →
`meta.loomPattern` vertex `plan-<hash>` via Processor (PD4) → `triggerLoom`. Plan-vertex GC
sweep. Speculative validation of the plan's first op (dry-run against the Processor's
validation path) gates dispatch if a validate-only lane exists by then; otherwise deferred to
its own follow-up fire — the brief must check what the Processor exposes. **Acceptance:** e2e —
two entities in different states derive different plans for the same goal; same state derives
the same vertex (hash-stable); GC proven; no Loom code touched. **Risk: medium** (new vertex
class + GC; mitigated by content addressing and pinning).

### Fire 7 — Convergence diagnostics (contraction + oscillation)

Per-target violation-set trajectory (contraction monitor: shrinking / steady / diverging over a
window) and cross-target oscillation detection (same aspect alternately written by two targets'
dispatches — provenance from dispatch bookkeeping). On detection: freeze both targets' dispatch
(the existing `__control` disable seam), one Health issue naming the causal pair. Orthogonal to
Fires 3–6; may run any time after Fire 2. **Acceptance:** fixture with two fighting targets is
frozen + alerted within N sweep passes; contraction metrics on heartbeat. **Risk: low**
(freeze + alert only — never a new dispatch).

### Fire 8 — Fleet arbitration / admission control

A dispatch scheduler between evaluator and actuator: declared budgets (per-adapter rate, global
concurrency) + per-row priority (an optional §10.2 priority column, lens-projected like
`freshUntil`) → token-bucket paced dispatch with a legible plan surface ("N violations, M/hr,
ETA"). Default: unlimited budget = today's behavior. **Acceptance:** 3k-row backfill fixture
dispatches paced and priority-ordered; mode-less behavior unchanged. **Risk: medium**
(touches the shared dispatch path; default-off).

### Fire 9 — Augur integration: no-plan escalation + playbook promotion

Wire `noPlan` and the dormant `exhausted` triggers through `augurEscalation` (threading
`augur.model` — closes the arch-review finding). Augur proposals may carry plan-shaped
`proposedAction` sequences (dispatch via the Fire 6 plan-vertex path under the existing
proposal-scoped requestId + human gate). Promotion: a plan crossing the deterministic success
threshold emits a playbook-amendment proposal into the same review queue. `augur.autoApply`
remains Andrew-gated. **Acceptance:** e2e — planner failure raises a proposal; approved
plan-proposal dispatches once (collapse-proven); promotion proposal materializes after the
threshold fixture. **Risk: medium** (all behind the human gate).

## Open questions for Andrew (decide by the fire that needs them)

1. **Effect inference vs declaration** (Fire 1): effects are hand-declared. Static analysis of
   op Starlark to *suggest* effects is possible later — worth a backlog line, not a blocker.
2. **Shadow-divergence surface** (Fire 4): Health doc vs a small lens — implementer proposes.
3. **Speculative validation** (Fire 6): does the Processor grow a validate-only lane, or does
   Fire 6 ship dispatch-and-verify only? Depends on Processor appetite — separate decision.
4. **Confidence decay constants** (Fire 5): deterministic decay rule parameters — propose in
   the Fire 5 brief, tune via config like `MarkLease`/`SweepInterval`.
