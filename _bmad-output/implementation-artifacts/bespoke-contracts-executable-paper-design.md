# Bespoke Contracts / "Executable Paper" — semantic clauses as convergence targets

**Status: ✅ Andrew-ratified (2026-07-02) — the SCOPING RE-CALL accepted: pattern + package, no platform
engine.** Lattice = the §10.8/§10.2 clarification (committed with this ratification) + the rounding UDF on
demand; the reference package (Fires V1–V3) routes to the Verticals board — its ledger dependency shipped
2026-07-01/02, so it is buildable now.

> **Ratification Q&A (2026-07-02) — the three-tier bespoke-ness model + an explicit VAULT DEVIATION.**
> Andrew's question: how can clause DDL + Starlark be designed upfront for bespoke contracts? Answer —
> **no bespoke Starlark per clause exists, deliberately deviating from the vault's letter** (*Anatomy of
> a Clause* says "Predicate: a Starlark script that the Weaver evaluates" and imagines clause-carried
> code). Bespoke-ness decomposes: **Tier 1 — bespoke VALUES = pure data** (a clause instance is
> `vtx.clause` + a terms aspect + links; a new contract = ordinary ops, no DDL/script/install — the
> overwhelming majority). **Tier 2 — bespoke SHAPES = new archetype convergence lenses** (declarative
> cypher, reviewable, F-004 hot-installed; the Starlark stays the ledger's one generic `DebitAccount`
> with a row-templated numeric param; the vault's proration example is archetype cypher arithmetic —
> hence the on-demand rounding UDF). **Tier 3 — AI-AUTHORED archetypes** (the vault's real endgame):
> prose → proposed lens/DDL via the ratified ai-authored-capabilities loop (human review, deterministic
> validation, rollback) — bespoke-ness at AUTHORING time, never at runtime. The vault's clause-carried
> Starlark is REJECTED on security grounds: code smuggled in as vertex data bypasses the package-review/
> `permittedCommands` model (any clause-writer could inject money-moving execution) and makes Weaver a
> script runtime against standing doctrine. Brainstorm #66's "AI judgment hook" = judgment clauses open
> Tasks reviewed via the Augur loop.

Designer (Winston) · 2026-07-01 · Lattice lane (Stream 2).
Backlog row: *"Bespoke contracts / 'Executable Paper' — Starlark-backed semantic clauses"* —
`_bmad-output/planning-artifacts/backlog/lattice.md` → Lattice feature backlog → AI-native.
Vision source: Obsidian vault `Contract as Executable paper/{Semantic Contracts, Anatomy of a Clause}.md`;
brainstorm #63–#66 (Stream 10 — Semantic Contracts).

---

## For Andrew

**What it does (two lines).** Turns a lease/agreement into *executable paper*: each provision is a
`vtx.clause` vertex (prose + machine predicate + formula) linked to the state it governs, and the
**Weaver continuously audits clause satisfaction against a resident/patient ledger** — auto-debiting
computational clauses and opening a Task for judgment ones, with every ledger entry linked back to the
clause that authorized it ("why was I charged this?").

**The one decision for you — a scoping fork, designed through.** The board filed this as an **XL platform
feature** whose open question was *"3rd consumer of the held `internal/starlarksandbox` leaf — Weaver-side
continuous eval, not a Loom guard."* **Grounding dissolves that premise.** "Executable Paper" needs **no new
engine, no Weaver Starlark runtime, and no frozen-contract change** — it is a **modeling pattern that rides
the convergence machinery the platform already shipped** (Weaver targets-as-lenses, `directOp`/`assignTask`
row-templated dispatch, the temporal lane, anchor-tombstone retraction). Concretely I verified in code that
the three things the vision seemed to need already exist:

1. **The "compliance Weaver"** — a clause-satisfaction **lens** (§10.2 convergence target) that projects
   `violating = clause active AND no charge for this period`; Weaver's lane-1 loop is the tireless clerk.
2. **The "formula (how much)"** — the full cypher engine **already evaluates arithmetic** (`+ - * / %`,
   `executor.go:1483`), and a numeric result **flows type-preserved** through `directOp.params`
   (`resolveParam` returns the row value as `any`, `strategist.go:341`). For formulas beyond cypher, the
   amount is computed **Processor-side in the `DebitAccount` op script** (the existing verified-pure Starlark
   sandbox) — *never* in Weaver.
3. **"Auto-debit vs open-a-Task"** — the existing `directOp` (→ `DebitAccount`) and `assignTask` actions,
   selected by the clause's playbook gap. One-time-charge idempotency and recurring-period re-trigger fall
   out of existing convergence + temporal-lane machinery.

So the **Lattice-lane deliverable is thin** — a design (this doc), a `docs/contracts/10` **clarification**
(directOp param templating is type-preserving; the clause-as-convergence-target pattern is sanctioned), and
**at most one small, on-demand platform primitive**: a money-safe rounding cypher UDF (`round`/`floor`),
added *only if* a real proration clause needs division (the engine coerces all numbers to `float64` and has
no rounding function). **The marquee build is a Verticals-lane reference package** (`bespoke-contracts` for
LoftSpace) built to this pattern, **sequenced behind the parallel payment-ledger PO item** it consumes.

- **Recommendation — ratify the *pattern* and re-scope the row: Lattice ships the design + the §10.8
  clarification (+ the rounding UDF on demand); the realization is Verticals-lane package work.** This is the
  simplest extension of what exists, it keeps Weaver a lens consumer (not a Starlark runtime — the standing
  "no new engine Core-KV reads / Weaver-is-not-a-cypher-runtime" doctrine), and it avoids building an XL
  engine that grounding shows is unnecessary. The one thing I need from you is the **scoping call** — accept
  that this collapses to a pattern + package rather than a platform engine, and I'll route the package rows to
  the Verticals lane.
- **Alternative (rejected): build Weaver-side Starlark continuous evaluation** (make Weaver a predicate/formula
  runtime, the leaf's "3rd consumer"). Rejected as **dead scaffolding** — it duplicates the Refractor's cypher
  runtime *and* the Processor's script sandbox inside the convergence engine, for a capability both already
  provide; §5 shows every clause archetype is expressible without it.

**Frozen-contract change:** a **clarification** to `docs/contracts/10-orchestration-surfaces.md` §10.8
(Templating + the `directOp` action row) — no shape change, it documents the *already-true* type-preserving
behavior and blesses the clause-as-convergence-target pattern. **I have NOT staged it** pending your scoping
call (if you'd rather keep §10.8 untouched and carry the note in `docs/components/weaver.md` only, say so).
The exact edit text is in §6.

**Ratification state: 📐 awaiting-Andrew → ✅ Andrew-ratified.** Then the Lattice Steward builds the thin
platform slice (§10 Fire L1–L2) and the Verticals Steward builds the reference package behind the ledger.

---

## 1. Problem & intent

Today a lease is a **static artifact** plus **hard-coded workflow steps**: `SignLease` /
`DecideLeaseApplication` fire fixed procedures, and there is no financial record at all (the parallel
Verticals PO item *"LoftSpace — tenant payment ledger"* fills that). Unusual terms — a prorated first-month
amenity fee, a one-time lockout charge, a conditioned pet fee, a recurring smart-home package — have nowhere
to live except more hard-coded billing tables and workflow branches.

The vault's **"Executable Paper"** vision (`Contract as Executable paper/*`, brainstorm #63–#66) reframes the
agreement as a **living graph of clauses**:

- **Atomic clauses** (`vtx.clause`) — each provision is its own vertex, carrying the human-readable **prose**
  (legal record) and a **machine predicate + formula** (what "fulfillment" means digitally).
- **State-bound execution** — a clause is **Linked** to the state it governs (a "Late Fee" clause links to a
  lease's payment aspect; a "Pet Fee" clause activates only while a `resident owns pet` link exists).
- **The Compliance Weaver** — the Weaver continuously audits every active clause; an unmet **computational**
  clause triggers an operation automatically (a debit), an unmet **judgment** clause ("keep premises clean")
  opens a **Task** for a human inspector.
- **Traceability** — every ledger entry Links back to the authorizing clause ("click the $45 charge → see the
  exact paragraph you signed"); amendments update Links (old clauses superseded), not files.

**Intent, restated architecturally:** give the platform a *sanctioned pattern* for expressing bespoke,
state-driven obligations as convergence targets, so a vertical can add a new charging rule by installing a
clause + a lens + a playbook — **package data, no engine change, no redeploy** — and get continuous
enforcement, idempotency, audit, and self-amendment for free.

---

## 2. Reconciliation — "didn't we already build this?" (the load-bearing section)

This is the whole finding, so I state it before the shape. The vault was written before the convergence
machinery existed; read against today's platform, **the four pillars of "Executable Paper" each map onto a
shipped mechanism**:

| Vault concept | Today's mechanism | Evidence |
|---|---|---|
| "The Compliance Weaver continuously audits active clauses" | A **§10.2 convergence target lens** + Weaver's **lane-1 loop** (watch → mark → dispatch → reconcile). Weaver **is** the tireless clerk; it already does exactly this for `leaseApplicationComplete`. | `docs/components/weaver.md` "Targets as Lenses (D4)"; `internal/weaver/evaluator.go` |
| "Predicate — the *when* a charge is due" | The **lens cypher** — `violating = clauseActive AND NOT EXISTS(charge for this period)`. Identical in kind to the shipped `missing_bgcheck = NOT EXISTS(check WHERE date > now−window)`. | §10.2; the `lease-signing` convergence lens |
| "Formula — the *how much*" | **Cypher arithmetic** (`daily_rate_cents * days`, `executor.go:1483-1580`) projected as a numeric column, **or** — for anything beyond cypher — the **`DebitAccount` op's Processor-side Starlark script** computing from hydrated state. **Never a Weaver runtime.** | `numericOp` (executor.go:1563); `internal/processor/starlark_runner.go` |
| "Auto-debit computational / open-a-Task for judgment" | The **`directOp`** action (→ `DebitAccount`) vs the **`assignTask`** action, chosen by the clause's playbook gap. | §10.8 action table; `internal/weaver/strategist.go:197,155` |
| "One-time charge → debit once, mark COMPLETED" | Convergence **idempotency**: the charge link makes `violating` flip `false` (upsert); a one-time clause carries a `completedAt` aspect so it never re-violates. | §10.8 anti-storm; §10.2 retraction-by-upsert |
| "Recurring fee re-triggers each period" | The **temporal lane** (§10.4 `@at`/`freshUntil`) or **`@every`** (Fire 1+2 shipped): the lens projects `freshUntil = next period boundary`; expiry flips `violating`; the debit fires; the new-period charge link closes it. | `docs/components/weaver.md` "Temporal lane"; `44b385a` / `e04498e` |
| "Ledger entry links back to the clause" | A **`lnk.transaction.authorizedBy.clause`** link written by the `DebitAccount` op; the FE follows it. | Contract #1 links; package DDL |
| "Self-amending contracts — supersede clauses via links" | **Anchor-tombstone retraction** (shipped): tombstone the old clause vertex → its convergence rows retract; link the new clause. | `refractor.md` "Anchor-tombstone retraction"; `679fe25` |
| "Global formula change → re-converge for everyone" | Update the **clause/lens vertex** → Refractor re-projects → Weaver re-converges. Standard reprojection. | `refractor.md` lens lifecycle |

**What genuinely does *not* exist yet** (and is therefore the real work): (a) the **payment ledger** itself —
a Verticals PO item already filed, `📋 ready`, that this feature consumes; (b) a **reference clause package**
(`vtx.clause` DDL + the satisfaction lens + the playbook + the audit link) — Verticals-lane package data; and
(c) **at most one small platform primitive** — a money-safe rounding cypher UDF — and *only* if a proration
clause needs float division (§7). Everything else is assembly of shipped parts.

**Does this introduce new *platform* state?** No. Clauses and ledger entries are ordinary **Core-KV
vertices/aspects/links** (P1); the satisfaction lens is an ordinary **read-model** (P5 — package work, not a
platform gap); the debit is an ordinary **op** (P2). The Weaver keeps only its existing convergence marks.

---

## 3. The shape

Everything below the platform line is **package data** (LoftSpace `bespoke-contracts` package); the platform
provides only the generic mechanisms named in §2. I specify the package shapes concretely so the pattern is
reviewable and the Verticals Steward can build to it.

### 3.1 Data model (Core KV — package DDL, P1)

Following Contract #1 key-shapes (4-seg aspects `vtx.<type>.<id>.<local>`; 6-seg links reading
"source relation target", later-arriving vertex = source):

**`vtx.clause`** — one per provision. Root body carries only `class: clause` + minimal envelope; business
data in aspects:
- `vtx.clause.<id>.prose` → `data.text` (the legal paragraph; human record).
- `vtx.clause.<id>.terms` → `data.{kind, amountCents?, rateCents?, period?, basis?}` — the machine terms.
  `kind ∈ {computational, judgment}`. `amountCents` (fixed fee) **or** `rateCents`+`basis` (formula input,
  e.g. `rateCents=5000`, `basis="daysOccupied"`). `period ∈ {oneTime, monthly, …}`.
- `vtx.clause.<id>.status` → `data.{state, completedAt?, supersededBy?}` — `state ∈ {active, completed,
   superseded}`.

**Links** (each reads "source relation target"; the clause is the later-arriving vertex on install, so it is
the **source**):
- `lnk.clause.<cid>.governs.lease.<lid>` — the clause governs a lease (state-bound execution).
- `lnk.clause.<cid>.chargesTo.account.<aid>` — the clause debits this ledger account.
- `lnk.clause.<cid>.conditionedOn.<targetType>.<tid>` *(optional)* — a conditioned fee (e.g. `…conditionedOn.pet.<pid>`); absent link ⇒ unconditional.
- `lnk.transaction.<txid>.authorizedBy.clause.<cid>` — **written by `DebitAccount`**; the audit chain of custody. (`transaction` is later-arriving ⇒ source.)

**Ledger (the parallel PO item, consumed here):** `asp.ledger` on `vtx.account.<id>` with `Debit/CreditAccount`
ops and a `lnk.transaction.postedTo.account` (or a per-transaction vertex + link — the ledger design's call).
This design **builds to** the ledger; it does not define it.

### 3.2 Read path (P5) — the clause-satisfaction convergence lens

A §10.2 Weaver target lens `clauseSatisfaction`, `engine: "full"`, projecting into `weaver-targets` keyed
`clauseSatisfaction.<clauseNanoId>` — one row per **active** clause, carrying:

```
value: {
  entityKey,                       // vtx.clause.<id> (full key, §10.2 rides in the value)
  violating,                       // clause active AND unsatisfied for the current period
  missing_charge,                  // computational-clause gap bool (§10.8 missing_<gap>)
  missing_inspection,              // judgment-clause gap bool
  accountKey, clauseKey,           // param columns (bare vertex keys → row-templated into directOp.reads)
  amountCents,                     // numeric param column — the computed debit (cypher arithmetic OR a fixed term)
  inspectorKey,                    // param column for the judgment branch
  freshUntil?                      // recurring clauses: next period boundary (RFC3339) — arms the temporal lane
}
```

The predicate is **cypher, deterministic over `$projectedAt`** (the committing op's timestamp, bound in —
`refractor.md` Capability envelope §; deterministic, replay-stable):

```cypher
-- computational, one-time (conditioned):
MATCH (c:clause)-[:chargesTo]->(a:account)
WHERE c.status.data.state = 'active'
OPTIONAL MATCH (c)-[:conditionedOn]->(cond)            -- pet, etc.
OPTIONAL MATCH (t:transaction)-[:authorizedBy]->(c)     -- an existing charge for this clause
WITH c, a, cond, t
RETURN nanoIdFromKey(c.key) AS clauseKey_id,
       c.key AS clauseKey, a.key AS accountKey,
       c.terms.data.amountCents AS amountCents,
       (t IS NULL AND (c.terms.data.basis IS NULL OR cond IS NOT NULL)) AS missing_charge,
       (t IS NULL AND ...) AS violating
```

For **recurring** clauses the "existing charge" match is period-scoped (`WHERE t.period = currentPeriod($projectedAt)`)
and `freshUntil = nextPeriodBoundary($projectedAt)` arms the temporal lane so the row re-violates each period.
For **proration** the `amountCents` column multiplies `rateCents * daysOccupied` (cypher arithmetic) — the one
place a rounding UDF may be needed if the formula divides (§7).

This lens is **P5-clean**: it is a Refractor read-model, not a Core-KV scan by a consumer. It is package DDL —
**a missing lens is package work, not a platform gap** (CLAUDE.md P5).

### 3.3 Write path (P2) — the playbook and the two actions

A `meta.weaverTarget` playbook (§10.8, package data) maps each gap to an existing action:

```jsonc
{
  "targetId": "clauseSatisfaction",
  "lensRef":  "<meta.lens id of clauseSatisfaction>",
  "gaps": {
    "missing_charge": {                              // computational → auto-debit (existing directOp)
      "action":    "directOp",
      "operation": "DebitAccount",                   // LITERAL op name (the directOp-must-be-literal guard holds)
      "target":    "row.accountKey",
      "params":    { "amountCents": "row.amountCents", "clauseRef": "row.clauseKey" },
      "reads":     ["row.accountKey", "row.clauseKey"]   // Processor hydrates account + clause
    },
    "missing_inspection": {                          // judgment → open a Task (existing assignTask)
      "action":    "assignTask",
      "operation": "InspectPremises",
      "assignee":  "row.inspectorKey",
      "target":    "row.clauseKey"
    }
  }
}
```

- **Computational clause → `directOp`.** The op name (`DebitAccount`) is a **literal** — the existing
  directOp-must-be-literal guard is satisfied; **only the params/target/reads are row-templated**, which the
  normal path already supports. The **amount is type-preserved** (`resolveParam` returns `row["amountCents"]`
  as `any`; a JSON number stays a number — verified `strategist.go:341`). The Processor hydrates the account +
  clause (via `reads`), the `DebitAccount` op script appends the ledger entry, writes
  `lnk.transaction.authorizedBy.clause`, and (for one-time clauses) sets `status.state = completed`. **No
  `proposedOp`, no Augur, no human gate** — this is a plain, playbook-configured directOp.
- **Judgment clause → `assignTask`.** Opens a Task for the inspector (existing `CreateTask` semantics, stable
  taskId, §10.8). Human completes it; the completion op writes the satisfying state; the lens flips
  `violating=false`.

**Idempotency** is the shipped convergence property: the anti-storm mark suppresses re-dispatch in-flight; the
charge link makes `missing_charge` flip `false` on the next reprojection (level-reconciled mark clear); a
one-time clause's `completed` status removes it from the lens entirely (retracts via anchor-tombstone / a
`WHERE state='active'` drop — see §9 R3 on the retraction transport). A recurring clause re-arms via
`freshUntil`.

### 3.4 Where the formula and predicate actually live (the doctrine)

| Vault term | Lives where | Never |
|---|---|---|
| **Predicate ("when")** | the **lens cypher** (`violating` / `missing_<g>`) | not a Weaver Starlark predicate |
| **Formula ("how much"), simple** | the **lens cypher** arithmetic → `amountCents` column | not a Weaver runtime |
| **Formula ("how much"), complex** | the **`DebitAccount` op's Processor-side Starlark script** (existing sandbox), reading the clause's `terms` aspect + hydrated state via `contextHint.reads` | not a Weaver runtime |
| **Compliance loop** | the **Weaver lane-1 convergence loop** (unchanged engine) | — |

This is the direct application of the standing doctrine — *"Core-KV reads and computation default
Processor-side; Weaver is a lens consumer, not a cypher/Starlark runtime"* ([[feedback_no_new_engine_corekv_reads]]).
The vault imagined "Weaver evaluates a Starlark predicate" because it predated targets-as-lenses; grounded
against the platform, that runtime is **redundant** (the lens is the predicate) and would **duplicate** two
existing engines. It is rejected (§9 A1).

---

## 4. The three clause archetypes, worked end-to-end

To prove the pattern covers the vault's worked examples (`Anatomy of a Clause` §3):

1. **Fixed / one-time "Nuisance" charge (Lockout Fee).** Clause `terms={kind:computational, amountCents:4500,
   period:oneTime}`. Lens: `missing_charge = (no authorizedBy transaction)`. Weaver `directOp DebitAccount`
   with `amountCents=row.amountCents (4500)`. Op appends the ledger entry, links it to the clause, sets
   `status.state=completed`. Clause drops from the lens (state≠active) → no re-fire. **Existing machinery,
   zero platform change.**
2. **Conditioned fee (Pet Fee).** Clause with `lnk.clause.conditionedOn.pet`. Lens matches the clause only
   while the pet link is live (`OPTIONAL MATCH (c)-[:conditionedOn]->(pet)` → `missing_charge = pet IS NOT
   NULL AND no charge`). Remove the pet (tombstone the link) → the condition fails → Weaver stops nudging.
   **Existing machinery.** (Recurring pet fee: add the period scope + `freshUntil`.)
3. **Prorated first-month amenity fee.** Clause `terms={kind:computational, rateCents:5000, basis:daysOccupied,
   period:oneTime}`. `daysOccupied = billingStart − moveIn` — a **date delta**. Two sub-cases:
   (a) if the delta+multiply is expressible in cypher and the result is integer cents, the lens projects
   `amountCents` directly; (b) if it needs **division/rounding** (`monthlyCents * days / 30`), either add the
   on-demand rounding UDF (§7) **or** move the computation to the `DebitAccount` op script (integer cents,
   Processor-side). I recommend **(b)-op-script for any dividing formula** — money precision is a correctness
   plane and integer-cents arithmetic in Starlark is exact, whereas cypher's `float64` division rounds
   implicitly (§7).

**Money precision is binding.** All amounts are **integer cents**. Cypher coerces every number to `float64`
(`numericOp`, `executor.go:1563`); `float64` represents integers exactly up to 2⁵³, so `+ - *` on realistic
cents are exact, but `/` is not. Rule: **a formula that only adds/subtracts/multiplies cents may compute
lens-side; a formula that divides must compute in the op script (integer cents) or use the rounding UDF.**
Stated in the design + the package README so no author ships a float-rounding money bug.

---

## 5. Orchestration precedents mirrored

- **Weaver target-as-lens (D4)** — `clauseSatisfaction` mirrors `leaseApplicationComplete` exactly (row per
  candidate + `violating` + gap bools + param columns; upsert retraction).
- **`directOp` with row-templated params + reads** — mirrors the shipped `TombstoneObject` directOp
  (`reads: [row.entityKey]`) and the amount rides the **already-type-preserving** param path.
- **Temporal lane (`@at`/`freshUntil`)** — recurring clauses mirror the bgcheck-freshness pattern
  (`freshUntil` → NATS `@at` → `MarkExpired` → re-violate).
- **Anchor-tombstone retraction** — clause supersession mirrors the shipped plain-lens anchor-tombstone
  retraction; the `completed`/`superseded` state drop is a `WHERE state='active'` filter-flip (§9 R3).
- **Processor script sandbox** — a complex formula reuses the Processor's `execute` script path (unchanged),
  **not** the held `internal/starlarksandbox` Weaver consumer.

No new orchestration primitive is introduced.

---

## 6. Contract surface

**Build-to, with one clarifying edit — no shape change.** Every mechanism (§10.2 target rows, §10.8 playbook +
`directOp`/`assignTask` actions + templating, §10.4 temporal lane) is used **as specified**. The clause vertex,
ledger aspect, satisfaction lens, and playbook are **package data**, which §10.8 explicitly designates.

The one edit — a **clarification** to `docs/contracts/10-orchestration-surfaces.md`, staged **UNCOMMITTED**
for Andrew per the frozen-contract rule (I have *not* staged it pending the scoping call in "For Andrew"):

- **§10.8 Templating** — add one sentence making the **type-preserving** substitution explicit (it is already
  the code behavior; package authors billing money must be able to rely on it): *"Substitution is
  type-preserving: a `row.<column>` resolving to a JSON number (e.g. a lens-computed `amountCents`) is passed
  to the op as a number, not stringified — `resolveParam` returns the row value verbatim. A monetary param is
  integer cents; see the money-precision note."*
- **§10.8 `directOp` action row** — append: *"A clause-billing target is the canonical consumer: `operation`
  is the literal `DebitAccount`, `target`/`params`/`reads` are row-templated (the amount is a numeric param
  column, the clause + account keys route into `reads` for hydration)."*
- **§10.2** — one non-normative note that the **clause-satisfaction lens** is a sanctioned use of the target
  shape (a clause is a candidate entity; `missing_charge`/`missing_inspection` are ordinary gap columns).

This is documentation of already-true behavior + a blessed pattern, **not** a new field or shape — hence a
*clarification*, and hence the light touch. If Andrew prefers **zero** contract edit, the same content lives
in `docs/components/weaver.md` (a doc, not a contract) and this design still stands. **Affected consumers:**
package authors (the pattern) + the Weaver engine (no code change — the behavior already exists).

---

## 7. The single genuine platform seam — a money-safe rounding UDF (on demand)

The **only** thing grounding surfaced that the platform lacks and a clause might need: the full engine has
**no `round`/`floor`/`ceil`/`toInteger`** function (only `levenshteinDist`/`levenshteinRatio`,
`executor.go:1209-1233`) and coerces all arithmetic to `float64`. A **dividing** proration formula computed
lens-side would therefore need explicit rounding to land on exact integer cents.

**Recommendation — do NOT build it speculatively (dead-scaffolding test).** Two paths avoid it entirely:
(1) compute any dividing formula **Processor-side** in the `DebitAccount` op script (integer cents, exact); or
(2) restrict lens-side formulas to `+ - *` on cents (exact in `float64`). Add the UDF **only when a real
LoftSpace clause needs lens-side division** — and then it is a ~20-line pure, deterministic UDF mirroring the
Levenshtein pattern (`round(x) -> int`, `floor(x) -> int`), a small platform fire, not an XL build. Until a
consumer needs it, it stays a **named-but-unbuilt** option (this section is its design).

*This is the correct application of the dead-scaffolding + "add the small primitive on demand, don't
enshrine a workaround" reflexes: the primitive is real, but it has no consumer yet, so it is designed and
sequenced, not built.*

---

## 8. Migration & test strategy

**No platform migration** — the platform slice is a doc clarification (+ the optional UDF, which is additive
and behind its own tests). No bootstrap bump, no key change, no data migration.

**Package build proves the pattern end-to-end** (Verticals lane, `make up-loftspace`):
- **Lens unit** (`internal/refractor/ruleengine/full/*_test.go`): the `clauseSatisfaction` cypher projects
  `violating`/`missing_charge` correctly for each archetype (fixed / conditioned present-and-absent /
  recurring-period / proration); `amountCents` is a **numeric** column (not string); `$projectedAt` makes the
  period deterministic.
- **Weaver dispatch** (`internal/weaver/*_test.go`): a violating clause row dispatches `DebitAccount` with a
  **numeric** `amountCents` param (type-preservation regression) + the clause/account keys in `reads`; a
  judgment clause dispatches `assignTask`; the mark clears on the charge link (idempotency); a re-fire under
  lease-expiry collapses (no double debit — the charge link already exists → `missing_charge=false`).
- **Op script** (`DebitAccount`): appends the ledger entry, writes `authorizedBy` link, sets `completed`;
  Starlark integer-cents arithmetic for the complex-formula case; the op is **idempotent** under a repeated
  requestId (Contract #4 tracker) — a double dispatch produces at most one debit.
- **e2e** (`make up-loftspace` + the ephemeral-stack harness): install the clause package behind the ledger →
  a one-time lockout fee auto-debits once and links to the clause → a recurring fee re-debits next period → a
  removed pet stops the pet fee → supersede a clause and confirm its rows retract.
- **Money-precision test**: a proration formula lands on exact integer cents (op-script path) — a golden
  fixture that would fail under naive `float64` division.

Gates: `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`,
`make verify-package-*` (the clause package DDL/permissions), `make test-bypass` (Gate 2), the relevant
`go test` packages. The debit is a security-relevant write (money) — the package build gets a **full 3-layer
review**.

---

## 9. Risks & alternatives

| # | Risk | Mitigation |
|---|---|---|
| R1 | **Over-building — an XL Weaver engine that grounding shows is unnecessary.** | The design's central move is to *avoid* it; §2 proves every pillar is shipped. The Lattice slice is a doc + an on-demand UDF. |
| R2 | **Money as `float64` → rounding bug.** | Integer cents everywhere; `+ - *` exact in `float64`; **division computes Processor-side (integer cents) or via the rounding UDF** (§4, §7). A golden proration test guards it. |
| R3 | **A completed/superseded clause's row fails to retract → a stale/over-charge.** | The satisfaction lens matches `WHERE status.state='active'`; a clause leaving `active` is either an **anchor tombstone** (supersede → tombstone the clause vertex → shipped anchor-tombstone retraction deletes the row) or a **status-flip that drops the anchor from the matched set** — the latter is the broader *negative/filter-retraction* case (📐 on the backlog). **For v1, model supersession as a clause-vertex tombstone** (retracts today), and model one-time completion as a tombstone too (or accept the row lingers with `missing_charge=false` = non-violating = harmless, the shipped `violating` upsert). A *filter-flip that must retract while the anchor survives* is deferred to the retraction primitive — **do not depend on upsert to retract a dropped composite key** ([[feedback_designer_parallel_overlap_and_retraction]]). Stated as a package build constraint. |
| R4 | **Double-debit under sweep reclaim / redelivery.** | The charge link flips `missing_charge=false` before a reclaim would re-fire (level-reconciled mark clear); the `DebitAccount` op is idempotent on its Contract #4 requestId; the anti-storm mark suppresses in-flight. Same guarantee the shipped `directOp` reclaim path carries. |
| R5 | **Formula complexity creep → someone reaches for a Weaver runtime.** | The doctrine (§3.4) routes all computation to the lens or the op script; the package README + the §6 note make Weaver-side eval a non-option. |
| R6 | **Cross-lane sequencing — clause package built before the ledger exists (dead scaffolding).** | The reference package is sequenced **behind** the Verticals payment-ledger PO item (`DebitAccount`/`asp.ledger`). Don't build clause-billing against a non-existent ledger. §10 makes the ledger a hard predecessor. |

**Alternatives considered:**
- **A1 — Weaver-side Starlark continuous evaluation** (the leaf's "3rd consumer"). **Rejected — dead
  scaffolding + doctrine violation.** It makes Weaver a predicate/formula runtime, duplicating the Refractor
  cypher engine (the predicate) and the Processor script sandbox (the formula) inside the convergence engine.
  §2 shows every archetype is expressible without it; the standing "Weaver is a lens consumer, not a cypher
  runtime" + "no new engine Core-KV reads" reflexes forbid it. Could a *variant* beat the recommendation? Only
  if a clause predicate were **inexpressible as a lens** — but a lens is a full-cypher read over the exact
  state a clause governs, which is strictly more capable than a subject-scoped Starlark predicate. No variant
  wins.
- **A2 — relax the `directOp`-must-be-literal guard so the whole op is row-sourced** (like `proposedOp`).
  **Rejected — unnecessary and weaker.** The op name is a **literal** (`DebitAccount`); only params are
  row-templated, which already works. `proposedOp` exists for the *Augur's* dynamic-op case (a human-approved
  AI proposal) with re-validation + a human gate — a computational clause needs **neither** (it's
  playbook-configured and auto-fires). Reusing `proposedOp` would wrongly drag in the approval gate.
- **A3 — a new `debit`/`billing` Weaver action.** **Rejected — `directOp` covers it.** A dedicated action adds
  engine surface for zero capability the generic `directOp` + a package op lacks. Keep the engine generic.
- **A4 — clause = a Loom pattern (a workflow), not a convergence target.** **Rejected — a clause is a *target
  state* ("a charge is owed until paid"), not a fixed procedure** — the exact Weaver-vs-Loom boundary
  (`weaver.md` overview). A judgment clause's *inspection* could be a Loom `userTask`, but the *audit* of
  clause satisfaction is convergence. Use Weaver; let a judgment clause `assignTask` (which may itself sit in
  a Loom flow if the vertical wants).

---

## 10. Fire-by-fire decomposition (cross-lane; sequenced behind the ledger)

The build spans both lanes. **Lattice-lane fires are thin**; the marquee realization is **Verticals-lane**
package work. All sequenced **behind** the payment-ledger PO item (a hard predecessor — R6).

**Lattice lane (platform — after ✅ Andrew-ratified):**
- **Fire L1 — the §10.8 clarification + `weaver.md` pattern note.** Land the type-preserving-templating
  sentence + the clause-as-convergence-target note (the staged-uncommitted contract edit, if Andrew accepts
  it; else the `weaver.md`-only variant). Doc-only; no code. *Thorough lead review (doc).*
- **Fire L2 — money-rounding cypher UDF (`round`/`floor` → int), ON DEMAND ONLY.** Built **with** the first
  LoftSpace clause that needs lens-side division, mirroring the Levenshtein UDF pattern; pure, deterministic,
  unit-tested. **Skip entirely if the vertical computes all dividing formulas Processor-side** (the
  recommended path). *Full review (it touches the cypher executor).* — **held as designed-not-built until a
  consumer needs it (§7).**

**Verticals lane (the reference realization — after the payment ledger ships):**
- **Fire V1 — `bespoke-contracts` package skeleton + a fixed/one-time clause.** `vtx.clause` DDL (prose/terms/
  status aspects + governs/chargesTo links), the `clauseSatisfaction` lens (fixed-fee archetype only), the
  playbook (`missing_charge → directOp DebitAccount`), and the `DebitAccount` op wired to the ledger +
  `authorizedBy` link + `completed` status. e2e: one lockout fee auto-debits once, links to the clause.
- **Fire V2 — conditioned + judgment clauses.** Add `conditionedOn` (pet fee) + the `missing_inspection →
  assignTask` gap (a judgment clause opens an inspector Task). e2e: pet removed → fee stops; inspection Task
  opens + completes.
- **Fire V3 — recurring + proration.** The temporal-lane `freshUntil` period boundary (recurring smart-home
  fee) + a proration clause (computed Processor-side in the op script, integer cents — or trigger Fire L2 if
  lens-side division is chosen). e2e: recurring fee re-debits next period; proration lands exact cents.
- **Fire V4 — self-amendment + the "why was I charged this?" FE.** Supersede a clause (tombstone + link the
  new one; rows retract) and a LoftSpace FE view following `authorizedBy` from a ledger line to the clause
  prose. (FE = `fe-engineer` + UX; a read-model, P5.)

Each fire independently shippable + green. The **whole marquee vision is realized by V1–V4 on existing
platform machinery**, with L1 (+ optionally L2) as the only platform touches.

> **🏗️ CHECKPOINT (Vertical Steward, 2026-07-02) — Fire V1 SHIPPED, `f47c78e`.** No persistent worktree
> (finished in one fire; the branch was merged and deleted). Built exactly to spec: `packages/bespoke-contracts`
> (clause DDL + clauseProse/clauseTerms/clauseStatus aspect DDLs, the `clauseSatisfaction` actorAggregate lens
> anchored on `clause` — deliberately **no** `WHERE status='active'` filter, since that would depend on the
> unbuilt filter-retraction primitive per R3; convergence is purely "does an `authorizedBy` transaction exist,"
> which upserts safely — the playbook `missing_charge → directOp(DebitAccount)`), plus `loftspace-ledger`
> `DebitAccount`'s new optional `clauseRef` param (writes the `authorizedBy` link + marks the clause completed;
> a plain DebitAccount with no clauseRef is regression-tested unaffected). Registered in `lattice-pkg`'s
> package registry + `install-loftspace`/`refresh-loftspace`. Full local gates green (build/vet/lint/
> golangci-lint/full `go test ./...` short suite); `make verify-kernel`/`test-bypass`/`test-capability-
> adversarial` were **not** run — they require `make down && make up` against the shared multi-fire stack,
> which this fire does not own (no security/capability-plane surface touched here regardless). **Not yet
> verified live in-browser** (a newly-added package needs a fresh bootstrap, not a hot F-004 refresh) — proven
> instead via a self-contained Processor-pipeline integration test (`bespoke_contracts_test.go`, embedded NATS)
> exercising CreateClause → DebitAccount{clauseRef} end-to-end, plus a `full`-engine cypher unit test of the
> lens. **Fire L1** (the §10.8 doc clarification) was **not** done this fire — it's a Lattice-lane doc touch
> and V1 didn't need it (the type-preserving templating behavior it documents was already load-bearing and
> already true in code). **Next: Fire V2** (conditioned `pet` fee via `conditionedOn` + the judgment
> `missing_inspection → assignTask` gap) — a fresh `git worktree add` per the fresh-worktree-per-fire rule.

> **🏗️ CHECKPOINT (Vertical Steward, 2026-07-02) — Fire V2 SHIPPED, `e9408e7`.** Fresh worktree
> (`steward-bespoke-v2`), merged + deleted. Built both remaining Fire V2 archetypes: CreateClause gains an
> optional `conditionedOnKey` (any live vertex — generic per the design, not a concrete `pet` type; validated
> alive, writes a `conditionedOn` link + an explicit `terms.conditioned` flag) and `kind=judgment` (assigns an
> inspector via a `requiresInspectionBy` link instead of charging an account). A new `InspectPremises` op —
> routed through the `clause` DDL's own script, mirroring `SignLease` acting on its own `leaseapp` — writes a
> CreateOnly `.inspection` aspect closing the gap. The `clauseSatisfaction` lens gained `missing_inspection`
> alongside `missing_charge`; the §10.8 playbook dispatches `assignTask(InspectPremises)`.
> **Real defect found + fixed building this**: the `full` engine's `IS NULL`/`IS NOT NULL` is an unimplemented
> no-op (`visitStringListNullOp` passes the suffix through unevaluated) and a bare unmatched OPTIONAL MATCH node
> variable is a Go typed-nil pointer boxed in an interface, not a clean nil — null-testing it directly silently
> mis-evaluates. Fixed throughout by null-testing only `.key`/aspect property projections with explicit
> `= null`/`<> null`. A dedicated adversarial-review agent pass (money-safety focus, since this gates
> `DebitAccount`) then caught a second, self-inflicted bug pre-merge: the conditioned gate used
> `conditioned = false`, which silently permanently suppressed the charge for every Fire-V1-shaped legacy clause
> (no `conditioned` field at all, resolves nil, never equals `false` either) — fixed to `conditioned <> true` +
> a regression test. Full local gates green (build/vet/golangci-lint/lint-conventions/full `go test
> ./packages/...`); `verify-kernel`/`test-bypass`/`test-capability-adversarial` not run (same rationale as V1 —
> no security/capability-plane surface touched; `InspectPremises` is an ordinary operator-granted op, same model
> as `SignLease`/`DebitAccount`). Not yet verified live in-browser (no FE this fire; proven via the `full`-engine
> lens unit tests + the real-Processor integration tests). **Next: Fire V3** (recurring `freshUntil` period
> boundary + a proration clause, computed Processor-side in integer cents) — fresh worktree.

> **✅ CHECKPOINT (Vertical Steward, 2026-07-02) — Fire V3 SHIPPED, `47ba7c6`.** Fresh worktree
> (`steward-bespoke-v3`), merged + deleted. Built both remaining Fire V3 archetypes, **no platform touch, no
> rounding UDF** (§7 stays designed-not-built): (1) **recurring `period="monthly"`** — `clauseSatisfaction`'s
> `missing_charge` gate branches on `period`: non-monthly keeps the exact Fire V1/V2 `chargeCount=0` check;
> monthly instead reads `.status.data.chargeValidUntil` freshness (`= null OR <= $now`), mirroring lease-signing's
> bgcheck-freshness `validUntil` pattern verbatim, and projects `freshUntil` (`Freshness: "auto"`) to arm Weaver's
> `internal/weaver/temporal.go` `@at` lane exactly like `appointmentReminders`/bgcheck already do. `DebitAccount`
> (loftspace-ledger) gained an optional `period` param, templated as `row.period` by the §10.8 playbook, that
> re-arms `chargeValidUntil` instead of completing the clause. (2) **proration** — `CreateClause` accepts
> `rateCents`+`periodDays`+`daysOccupied` instead of a flat `amountCents` and computes the charge **once, at
> creation**, as `(rateCents*daysOccupied) // periodDays` using explicit Starlark `int(...)` conversions before
> the multiply/floor-divide — genuine bignum integers, never `float64`, so the design's §7/R2 rounding hazard
> (e.g. `9000/30` landing on `8999`) cannot occur; the result then flows through the unchanged flat-fee path
> (no `DebitAccount`/lens change needed for proration at all).
> **A dedicated adversarial money-safety-review agent pass** (same discipline as Fire V2's) traced the Starlark/
> cypher semantics from first principles (confirmed JSON integers pre-convert to genuine Starlark `Int`s, `//` is
> exact bignum floor division, `$now`/`chargeValidUntil` are lexically-sound whole-second UTC RFC3339) and found
> the arithmetic and lens gates correct, but caught one real gap: `DebitAccount`'s `period` param is
> caller-supplied and never cross-checked against the clause's own stored `.terms.data.period` (that aspect isn't
> hydrated — Reads only names the bare clause root); a hand-submitted, non-Weaver `DebitAccount` (an ordinary
> operator-granted op, not Weaver-exclusive) omitting/misreporting `period` against a genuinely-monthly clause
> would never stamp `chargeValidUntil`, leaving it permanently violating and Weaver re-dispatching indefinitely.
> Closing this properly (hydrating the clause's own `.terms` to cross-check) is blocked on a real platform gap —
> the `full` cypher engine has **no string-concatenation operator**, so a lens cannot project a derived aspect key
> (`clauseKey + ".terms"`) for the Weaver-driven Reads to name, and the existing Reads-templating grammar only
> substitutes bare `row.<col>` values, never a `row.<col>` + literal suffix — filed as a small follow-on platform
> primitive if a future fire wants full closure. **Fixed inline instead** (defense-in-depth, no platform
> primitive needed): `chargeValidUntil` is now stamped **unconditionally** in both branches — inert for a
> genuinely one-time clause (that gate never reads it) but closes the dangerous direction of the mismatch for a
> genuinely-monthly one, regardless of what the caller's `period` said. A regression test
> (`TestDebitAccount_RecurringClause_MismatchedPeriodOmitted_StillReArms`) proves it. **Noted, not fixed (pre-existing,
> low severity, orthogonal to this fire):** flat `amountCents` (the non-prorated path, unchanged since Fire V1) is
> never coerced to an integer the way the new prorated path is — a caller could submit a fractional cents value
> unrounded; a follow-up guard is a candidate for a future maintenance fire, not scoped here.
> Full local gates green (build/vet/golangci-lint/lint-conventions/full `go test ./...` — the entire repo, not
> just this package); `verify-kernel`/`test-bypass`/`test-capability-adversarial` not run (same rationale as V1/V2
> — no new security/capability-plane surface; the `period` param is an ordinary op field, not a permission/scope
> change). Not yet verified live in-browser (no FE this fire — the design's Fire V4 "why was I charged this?" FE
> is the first one; proven via the `full`-engine lens unit tests covering the never-charged/fresh/lapsed
> lifecycle + real-Processor integration tests covering proration's exact-boundary money case and the recurring
> debit cycle). **All of V1-V4 named in §10 are now: V1/V2/V3 shipped, V4 (self-amendment + FE) is the only
> remaining increment** — no next-fire pointer needed until V4 is picked up.

---

## 11. Self-adversarial pass (Designer, folded in — the L/XL gate, discharged)

Run as a solo adversarial sweep (the substantial-design rigor; a `bmad-party-mode` pass on the money-precision
+ retraction boundary is **recommended before Verticals Fire V3** at build time). Findings folded above:

- **"Is `amountCents` *really* type-preserved through the playbook?"** — Yes. `resolveParam` (`strategist.go:341`)
  returns `row[col]` as `any`; a lens numeric column is a JSON number in the row value; it reaches the op
  payload as a number. The Augur's separate "type-preserving materialisation" is for `proposedParams` (a
  `map[string]any` from a proposal), a different path — it does **not** imply the playbook path stringifies.
  **Folded** as the §8 regression test.
- **"Does upsert-retraction actually retract a completed/superseded clause?"** — **No, not universally** — the
  transport-of-retraction blind spot. An upsert-only reprojection that emits *fewer* rows does not delete a
  dropped composite key. So v1 models supersession/completion as a **clause-vertex tombstone** (shipped
  anchor-tombstone retraction deletes the row) and otherwise leaves the row **non-violating** (`missing_charge=
  false`, harmless). A *filter-flip that must retract while the anchor survives* is the deferred
  negative/filter-retraction case — **not** relied on. **Folded** as R3 + a package build constraint. (This is
  the security-plane-adjacent case — a lingering *non-violating* row is harmless; a lingering *violating* row
  would over-charge, which the tombstone path prevents.)
- **"Money as float64 — a real bug?"** — Yes for division; exact for `+ - *` on cents. **Folded** as R2 + §4
  the binding integer-cents rule + §7 the on-demand UDF.
- **"Does this collide with a parallel design?"** — Grepped the `📐`/`🏗️` designs + lane rows for `clause`,
  `ledger`, `directOp`, `proposedOp`, `resolveParam`: **no parallel design touches this seam.** The Augur
  dispatch design is adjacent (row-sourced dispatch) but disjoint (dynamic AI op + human gate vs a static
  playbook directOp). No consolidation needed.
- **"Which engine does the real consumer use — is the UDF necessity load-bearing?"** — The satisfaction lens is
  **full-engine** (arithmetic + `nanoIdFromKey`). The rounding UDF is only load-bearing **if** a formula
  divides lens-side; the op-script path removes even that. So the UDF is genuinely on-demand, not a hidden
  dependency. **Folded** as §7.
- **"Is a clause a target state or a workflow?"** — A *target state* (owed-until-paid) → Weaver, not Loom
  (A4). Judgment-clause *inspection* may be a Loom `userTask` downstream, but the *satisfaction audit* is
  convergence. **Folded** as A4.

**Gate discharged:** the substantial-design adversarial pass is run and recorded; the design is build-ready on
✅ Andrew-ratified (a `bmad-party-mode` pass on money/retraction is a recommended *build-time* checkpoint for
Fire V3, not an open design gate).

---

## 12. Summary

"Executable Paper" is not an XL engine — it is a **modeling pattern** that turns bespoke, state-driven
obligations into **convergence targets** on the machinery the platform already shipped: a clause-satisfaction
**lens** is the predicate, cypher/op-script arithmetic is the formula, Weaver's lane-1 loop is the compliance
clerk, `directOp DebitAccount` auto-debits, `assignTask` opens judgment Tasks, the temporal lane handles
recurrence, and anchor-tombstone retraction handles amendment. The **Lattice-lane deliverable is a design + a
§10.8 clarification (+ a money-rounding UDF on demand)**; the marquee is a **Verticals-lane `bespoke-contracts`
package** built behind the payment ledger. Weaver stays a lens consumer — **no new engine, no Weaver Starlark
runtime, no frozen-contract shape change.**

The one call for Andrew: **ratify the pattern and the re-scope** (platform pattern + package, not a platform
engine); optionally accept the light §10.8 clarification (staged uncommitted).

**Ratification state: 📐 awaiting-Andrew → ✅ Andrew-ratified** (then the Lattice Steward builds Fire L1, and
the Verticals lane builds V1–V4 behind the ledger).
