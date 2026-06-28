# Design — Operation/permission discovery via `instanceOf` template

**Status: 📐 awaiting-Andrew (ratification)**
**Author:** Winston (Designer fire, 2026-06-28)
**Backlog row:** `planning-artifacts/backlog/lattice.md` → *Lattice feature backlog → Refinements & ops →
"Operation/permission discovery via `instanceOf` template"* (★★, M–L). Conditional enabler for the
Verticals "Service-instance modeling — envelope-class discriminator" refactor
(`planning-artifacts/backlog/verticals.md`, Andrew directive 2026-06-28).

---

## For Andrew (ratify in one look)

**What it does (two lines).** Today the step-6 write-gate resolves a mutation's governing DDL by an
**exact** `class → canonicalName` lookup (Contract #1 §1.5). If a vertex carries a *fine-grained* envelope
class like `service.backgroundCheck.instance` — which is exactly what your envelope-class directive asks for
— that lookup **misses** and the gate silently falls to the **permissive default** (no `permittedCommands`
enforcement). This design adds one bounded step before the permissive fall-through: **resolve the governing
DDL by walking the vertex's `instanceOf` chain to its *type authority*** (the nearest ancestor that *is* a
registered DDL, e.g. the single `service` DDL). One type DDL then governs unbounded fine-grained subtypes
with **zero new DDLs**, and the envelope-class refactor stops opening a write-gate hole.

**The architectural fork (your call) — recommended: take the lift.**

| Option | DDLs | Platform change | Discriminator vs. authority | Verdict |
|---|---|---|---|---|
| **A — instanceOf lift** (this design) | **O(1)** — one type DDL per domain; new families = template *data* | Yes — one bounded resolver in the step-6 validator + a Contract #1 §1.5 amendment | **decoupled** (the directive's intent) | **Recommended** |
| **B — per-(family×role) DDLs** | **O(families×roles)** — a DDL per subtype | None (package-only) | **re-coupled** (every discriminator value is also a DDL; a new family = a meta-lane op + a duplicated script, not just data) | Rejected — see §7 |

My recommendation is **A**, built **together with** the Verticals refactor that consumes it (no
dead-scaffolding interim — see §6.4). The reasoning that earns it over B is in §7.1: B is not merely "more
DDLs," it **re-couples** the two things your directive exists to separate (the *discriminator* and the
*type authority*), and it duplicates the executing script across N family DDLs. A is the simplest extension
of machinery that **already exists** — `instanceOf` links already exist in service-domain; the DDL cache
already holds every type; the resolver is one additional lookup branch.

**Frozen-contract change (staged UNCOMMITTED in `main`).** **Contract #1 §1.5 + §1.6** — the
governing-DDL resolution algorithm gains the `instanceOf`-chain step before the permissive default; **Contract
#2 §2.1** gets a one-paragraph cross-reference noting that a vertex's *discriminator class* and the op's
*resolved script DDL* are now legitimately distinct. The diffs are the proposal (no separate amendment doc).
Affected consumers: the Processor step-6 validator only; auth (step 3) and script selection
(`ClassForCommand`) are untouched, so no auth-surface or dispatch change.

**One convention to ratify alongside (proposed, not yet edited into the architecture).** Promote your
directive to a numbered principle — **P7: a vertex's type/subtype discriminator is the envelope `class`,
never a `.class`/shadow aspect** — and back it with a `lint-conventions` gate (the Steward builds the gate;
you add the principle text to `lattice-architecture.md`, which the Designer does not edit). Proposed wording
in §8.

---

## 1. Problem & intent

### 1.1 The directive

Your 2026-06-28 directive (filed at commit `59cd944`, verticals row): **a vertex's type/subtype discriminator
MUST be the envelope `class`** — *prohibit* the `.class` discriminator aspect (service-domain) and shadow
workarounds like the `.family` aspect (lease-signing). A lease background-check claim vertex is today
**triple-labeled** and confusing to read cold:

```
vtx.service.<handle>                 envelope class = "leaseServiceInstance"     ← a package-only fork class
vtx.service.<handle>.class           aspect: { value: "service.backgroundCheck.instance" }   ← discriminator #2
vtx.service.<handle>.family          aspect: { value: "backgroundCheck" }                    ← discriminator #3 (lens reads this)
```

(`packages/lease-signing/scripts.go:614-630`.) The target shape: the **envelope class carries the type**
(`service.backgroundCheck.instance`), **no** `.class`/`.family` aspects, an `instanceOf` link → a real
`service.backgroundCheck.template`.

### 1.2 Why the envelope-class refactor needs a platform change

The Processor's step-6 write-gate (`internal/processor/step6_validate.go:99-123`) enforces
`permittedCommands` by resolving the **mutation document's `class`** against the DDL cache with an **exact**
match (Contract #1 §1.5 step 3; the code is `v.DDLs.Lookup(class)`). When the class is found, the op must
appear in that DDL's `permittedCommands` or the commit is rejected with `WriteScopeViolation`
(Contract #2 §2.6). When the class is **not** found, the gate is **permissive** — no `permittedCommands`
enforcement at all (Contract #1 §1.5 step 5 / §1.6).

Today every service vertex carries the **coarse** class `service` (the discriminator lives in the `.class`
aspect), so `Lookup("service")` hits the one `service` DDL and the gate enforces. The moment the vertex
class becomes **fine-grained** (`service.backgroundCheck.instance`), `Lookup` **misses** — there is no DDL
with that canonical name — and the gate silently degrades to the permissive default. The refactor would
therefore turn off `permittedCommands` enforcement for exactly the vertices it touches. That is a **security
regression**, not a cosmetic one: any capability-authorized actor could write a `service.*.instance` vertex
with any op.

So the envelope-class refactor cannot ship on its own. It needs *either* a DDL per fine-grained class
(Option B — re-introduce an exact-match target for every discriminator) *or* a way for the gate to find the
**shared type authority** behind a fine-grained class (Option A — this design).

### 1.3 The lift, in one sentence

Let a fine-grained-class vertex **discover its governing DDL by walking `instanceOf` to its type/template
authority**, so a single type DDL (`service`) governs every `service.*.template` / `service.*.instance`
without each needing its own DDL — the platform analog of prototypal inheritance, expressed over the
`instanceOf` links the domain already draws.

This is brainstorm-aligned: the inventory's "type as a first-class graph citizen / template-driven
instances" thread (`brainstorming-session-2026-04-08.md`) and the meta-model principle that *the platform's
meta-model uses the same VAL primitives as the business model* (Contract #1 §1.7) — `instanceOf` from a
business vertex to its type is that principle applied to authorization resolution.

---

## 2. The shape

### 2.1 The three roles (data model)

Nothing new in *primitives* — vertices, a `.class`-free root class, `instanceOf` links, and ordinary DDL
meta-vertices. What changes is how they layer:

```
# The TYPE AUTHORITY — an ordinary vertexType DDL meta-vertex. UNCHANGED from today.
vtx.meta.<svcTypeId>                       class = "meta.ddl.vertexType"
vtx.meta.<svcTypeId>.canonicalName         { value: "service" }
vtx.meta.<svcTypeId>.permittedCommands     { commands: [CreateServiceTemplate, CreateServiceInstance, RecordServiceOutcome] }
vtx.meta.<svcTypeId>.script                { source: <the service script> }

# A TEMPLATE — a business vertex whose ENVELOPE CLASS is the fine-grained type; instanceOf → the type DDL.
vtx.service.<tplId>                         class = "service.backgroundCheck.template"   data = {}
lnk.service.<tplId>.instanceOf.meta.<svcTypeId>     # template instanceOf the type authority

# An INSTANCE — envelope class is the fine-grained type; instanceOf → its template.
vtx.service.<instId>                        class = "service.backgroundCheck.instance"   data = {}
lnk.service.<instId>.instanceOf.service.<tplId>     # instance instanceOf its template
lnk.service.<instId>.providedTo.identity.<applicantId>
```

Key-shape compliance (Contract #1 §1.1): 4-segment aspects, 6-segment links, and the link name reads
*"source `instanceOf` target"* with the **later-arriving** vertex as source (the template/instance is the
source; the type/template it points at pre-exists as the target). No `.class`/`.family` aspect anywhere.

**Why two hops (instance → template → type) and not one (instance → type)?** That is a *domain* modelling
choice, not a platform constraint. service-domain keeps a template because the template is the natural anchor
for service-location's `availableAt` (template → location) and a provider's `providedBy` (template →
provider). A simpler domain could draw `instance → type` directly (one hop). The platform resolver walks
`instanceOf` regardless of how many hops the domain uses, up to a bound (§2.3).

### 2.2 Read path (P5) — unchanged

Applications still read lens projections, never Core KV. The lift touches only the **write-gate**, which is
internal to the Processor. The one downstream win for lenses: with the discriminator on the envelope `class`,
a lens anchors and discriminates on the vertex class directly (the full engine already matches a node label
against the key-type **or** the envelope class — `internal/refractor/ruleengine/full/executor.go:348`),
**retiring** the `.family` aspect that lease-signing reads today purely because the coarse class shadowed it.
That cleanup lands in the Verticals refactor, not here; this design only makes it *possible*.

### 2.3 Write path (P2) + the resolver (the one platform change)

All mutation still flows through `core-operations` → Processor → atomic batch (P2). The change is localized
to **commit step 6**, the validator's class→DDL resolution. Contract #1 §1.5's algorithm gains one step
between "exact lookup" (step 4) and "permissive default" (step 5):

```
resolveGoverningDDL(mutation, batch, workingSet):
    class := mutation.document.class
    if ref, ok := DDLs.Lookup(class); ok:                 # (today's path — exact match, unchanged)
        return ref, found

    # NEW: fine-grained class with no direct DDL → walk the instanceOf chain to the type authority.
    vtxRoot := vertexRootOf(mutation.key)                  # the 3-seg root the mutation writes/targets
    visited := {}
    for hop := 0; hop < MAX_INSTANCEOF_HOPS; hop++ {       # MAX = 4 (cycle/abuse bound; domains use 1–2)
        target, ok := instanceOfTargetOf(vtxRoot, batch, workingSet)   # the lnk.<root>.instanceOf.* target
        if !ok: break                                      # no instanceOf → no type authority
        if target ∈ visited: break                         # cycle guard → permissive default
        visited.add(target)
        if isMetaKey(target):                              # terminal: target IS a DDL meta-vertex
            if ref, ok := DDLs.LookupByMetaKey(target); ok && ref.Kind == "vertexType":
                return ref, found
            break
        if ref, ok := DDLs.Lookup(classOf(target, batch, workingSet)); ok:   # target's class is itself a DDL
            return ref, found
        vtxRoot := target                                  # keep walking (instance → template → type)

    return _, notFound                                     # → Contract #1 §1.5 step 5 permissive default
```

- **`instanceOfTargetOf`** finds the live `lnk.<vtxRoot>.instanceOf.<tType>.<tId>` link, preferring the
  **batch** (a create-time link is in the same atomic batch) over the **working set** (hydrated reads) over
  an **on-demand** Core KV read. It honors tombstones (a tombstoned `instanceOf` is no link).
- **Terminal** is either a `vtx.meta.*` DDL (read via the cache's `LookupByMetaKey`) or a business vertex
  whose own class resolves to a DDL. Both are already in the DDL cache; no schema invention.
- **Bound** `MAX_INSTANCEOF_HOPS = 4` + a `visited` cycle guard makes the walk terminating and abuse-proof
  (the deepest real domain chain is 2). Exceeding the bound or hitting a cycle yields *notFound* →
  permissive default (fail-**open** to today's behavior, never fail-into-a-wrong-DDL).

The resolved DDL feeds the **existing** `permittedCommands` / sensitivity checks verbatim
(`step6_validate.go:113-147`). The only new surface is *which* DDL those checks run against.

### 2.4 What is deliberately **unchanged**

- **Auth (step 3).** Authorization keys on `operationType` + actor + `authContext`, **never** class
  (Contract #2 §2.1: "auth-neutral"; §2.8 dispatch). A fine-grained discriminator class never enters the
  auth path, so the lift **cannot widen the auth surface**. Permissions are still granted per op
  (`CreateServiceInstance`, …) through the role/permission graph (Contract #6).
- **Script selection (`ClassForCommand`).** The op→script DDL is resolved from `operationType` via the
  reverse index (`internal/processor/ddl_cache.go:300`), which indexes **DDL canonical names only**. The
  fine-grained classes are **not** DDLs, so they never enter the index and never create ambiguity. One
  `service` type DDL keeps owning all three ops unambiguously → `ClassForCommand` resolves them as today.
  **The script that runs is the type DDL's script; the vertex it writes carries the fine-grained
  discriminator class.** That decoupling is the whole point and is new (§5.1 contract note).

This is the load-bearing property for review: **the lift moves exactly one resolution — the write-gate's
class→governing-DDL — and leaves auth and dispatch identical.**

---

## 3. Reconciliation with the existing mental model

*Didn't we already solve this with the `class` field's operationType→class index (Story 1.7)?* No — that
index resolves **which script runs** for an op (`ClassForCommand`, keyed on `operationType`). It does not
resolve **which DDL gates a write** to a vertex whose class has no DDL; that is the exact-match
`DDLs.Lookup(mutation.class)` in step 6, which §1.5 explicitly declares non-hierarchical ("Prefix matching
is not part of Phase 1"). The two resolutions are different and only the second one changes here.

*Does this duplicate or contradict an established pattern?* It **extends** one. `instanceOf` already exists
(service-domain draws `instance → template`; `packages/service-domain/ddls.go:34,370`). The DDL cache
already holds every type. The resolver is the read-path analog of the meta-model's existing
*"find the meta-vertex for this class"* lookup, with one fallback hop. It does **not** introduce a parallel
authority store, a new aspect, or a new link type (`instanceOf` is already a registered linkType in both
packages — `packages/{service-domain,lease-signing}/ddls.go`).

*Does this introduce new state — and do we already keep it?* No new state. The type authority is an ordinary
DDL meta-vertex (already the home of `permittedCommands`); templates are ordinary business vertices already
created by `CreateServiceTemplate`; the `instanceOf` links already exist. The only thing that moves is the
*discriminator string* — from a `.class` aspect (operational shadow) onto the vertex envelope `class`
(where Contract #1 §1.1 D5 says a vertex's type belongs).

*Is the permissive-default fall-through a "permanent design" or a Phase-1 simplification?* §1.5 step 5 is a
deliberate permissive default (FR-flexible writes, §1.6). The lift does **not** remove it — it inserts a
resolution attempt *before* it. A fine-grained class with no `instanceOf` chain still lands on the permissive
default, identical to today. Reserved-for-strictness, not tightened-by-surprise.

---

## 4. Worked example — service-domain & lease-signing under the lift

Tracing each op's write-gate (the consumer code is Verticals work; shown to prove the platform primitive
suffices):

| Op | Writes vertex (class) | instanceOf walk | Terminal DDL | `permittedCommands` check |
|---|---|---|---|---|
| `CreateServiceTemplate` | `service.bgCheck.template` | template → `vtx.meta.<service>` (in-batch link) | `service` | `CreateServiceTemplate` ∈ list → **PASS** |
| `CreateServiceInstance` | `service.bgCheck.instance` | instance → template (in-batch) → `vtx.meta.<service>` (committed) | `service` | `CreateServiceInstance` ∈ list → **PASS** (2 hops) |
| `RecordServiceOutcome` | (updates) `service.bgCheck.instance` | instance → template → `vtx.meta.<service>` (all committed) | `service` | `RecordServiceOutcome` ∈ list → **PASS** |
| `CreateLeaseServiceInstance` (lease-signing) | `service.bgCheck.instance`, `instanceOf` → a lease bg-check template | instance → template → `vtx.meta.<leaseService>` | `leaseService` (type DDL admitting the lease ops) | `CreateLeaseServiceInstance` ∈ list → **PASS** |

The negative path that proves the gate still bites: an actor submitting, say, `RecordServiceOutcome` against
a vertex whose `instanceOf` chain reaches a DDL **without** that op in `permittedCommands` →
`WriteScopeViolation` exactly as today. And a forged write of a bare `service.bgCheck.instance` vertex with
**no** `instanceOf` link → resolver returns *notFound* → permissive default. **That last case is why the lift
alone does not fully harden the gate** (§6.5): hardening the permissive default for fine-grained classes is a
separate, larger decision (it would break §1.6's flexible-write model) and is **out of scope** here — the
lift restores parity with today's coarse-class enforcement *for vertices that declare their type via
`instanceOf`*, which the Verticals refactor guarantees by construction (every instance/template gets an
`instanceOf` link in its create batch).

---

## 5. Contract surface

### 5.1 Contract #1 §1.5 / §1.6 — the governing-DDL resolution (CHANGE; staged uncommitted)

The substantive edit. §1.5's lookup algorithm gains the `instanceOf`-chain step between exact-match and the
permissive default; §1.6 gets a consequence note that `permittedCommands` enforcement now also reaches
fine-grained-class vertices that declare a type via `instanceOf`. The exact wording is staged uncommitted in
`docs/contracts/01-core-kv-data-model.md` (the diff is the proposal). Why §1.5 and not §2.1 (which the
backlog *guessed*): the write-gate's class→DDL resolution is specified in Contract #1 §1.5, not Contract #2
— grounding the contract surface in the code corrected the backlog's hint.

### 5.2 Contract #2 §2.1 — discriminator-class vs. script-DDL decoupling (CROSS-REF; staged uncommitted)

A one-paragraph note: the envelope `class` (op → script DDL, optional, resolved via the operationType→class
index) and the **resulting vertex's** `class` (the discriminator) are legitimately distinct under the lift —
an op may write a vertex whose class differs from the op's resolved DDL canonical name; that vertex's
write-gate authority is resolved per Contract #1 §1.5's `instanceOf` step. Staged uncommitted in
`docs/contracts/02-operation-envelope.md`.

### 5.3 No change to

Contract #4 (idempotency), #5 (health), #6 (capability/auth — the lift is auth-neutral), #10 (orchestration).
`instanceOf` is already a registered linkType in the consuming packages; no new link-type DDL is required by
the platform.

---

## 6. Decomposition for the Lattice Steward (fire-by-fire)

Each fire is independently shippable + green. The platform fires (1–2) are **sequenced with the Verticals
consumer** per §6.4 — they are ratified-and-ready, built when the refactor lands, not months ahead.

### 6.1 Fire 1 — the `instanceOf`-chain resolver in the step-6 validator (platform core)

- Thread the **batch mutations** and the **working-set (hydrated reads) + an on-demand KV reader** into
  `ValidatorImpl` (today it sees only the `ScriptResult` + the DDL cache — `step6_validate.go:39-58`). Keep
  the read path lazy: prefer batch → working set → on-demand, so a populated `contextHint` keeps the gate
  read-free on the hot path (mirrors Contract #2 §2.5's existing hydration discipline).
- Implement `resolveGoverningDDL` (§2.3): exact `Lookup` first (unchanged fast path), then the bounded
  `instanceOf` walk, then *notFound* → permissive default. Add `DDLCache.LookupByMetaKey` reuse (it already
  exists — `ddl_cache.go:313`).
- **Tests (fixtures, no vertical dependency):** (a) a fine-grained-class create with an in-batch `instanceOf`
  → type DDL enforces that DDL's `permittedCommands` (PASS when admitted; `WriteScopeViolation` when not);
  (b) a 2-hop instance → template → type chain across batch+committed state; (c) a fine-grained class with
  **no** `instanceOf` → permissive default (parity with today); (d) cycle + depth-bound → permissive
  default, no infinite loop; (e) the exact-match fast path is byte-for-byte unchanged for coarse classes
  (`service`, `identity`, every existing vertex). Backward-compatible by construction — direct `Lookup`
  still wins first, so every shipping vertex is unaffected.

### 6.2 Fire 2 — Contract commit + Loupe op-discovery confirmation

- After Andrew ratifies, commit the Contract #1 §1.5/§1.6 + Contract #2 §2.1 edits (Designer leaves them
  uncommitted; this is the Steward's post-ratification commit — see the ratified-contract-commit rule).
- **Loupe op-discovery:** `cmd/loupe/ops.go:buildOpGroups` already builds the Submit-Op catalog from DDL
  `permittedCommands`, grouping by the owning **vertexType** DDL. Because the lift keeps one type DDL per
  domain (the fine-grained classes are *not* DDLs), the catalog **already renders correctly** — the `service`
  group lists its three ops. Fire 2 is a *confirmation test* (the catalog is unchanged under the lift) plus,
  optionally, a small enrichment: surface that a type DDL governs templated instances (nice-to-have, not
  required for correctness). Scope this to a test + an optional one-line label; do not over-build.

### 6.3 Fire 3 (Verticals stream, cross-lane) — the consumer refactor

Not a Lattice-lane fire, listed for sequencing: the Verticals "Service-instance modeling" refactor
(`backlog/verticals.md`) rewrites service-domain + lease-signing onto the templated envelope-class model —
fine-grained envelope class, drop `.class`/`.family`, `instanceOf` → templates, provision bg-check/payment
templates (and their `instanceOf` → type-DDL links) at install. It **consumes** Fire 1. Built by the
Verticals stream; the Lattice Steward coordinates so Fire 1 lands just-ahead-of or with it.

### 6.4 Dead-scaffolding discipline (why Fire 1 is *ratified-and-sequenced*, not *build-now-dark*)

Fire 1's enforcement only **bites** when a fine-grained-class consumer exists. Built alone, it is inert
(backward-compatible no-op for every coarse-class vertex). Per the dead-scaffolding test — *does this
increment realize value before its consumer exists?* — the answer is **no** until the Verticals refactor
produces fine-grained classes. So: **ratify the design now; build Fire 1 with (or immediately ahead of) the
Verticals consumer**, not as standalone dark machinery. There is a real, filed consumer (the directive), so
this is genuine readiness, not speculative scaffolding.

### 6.5 Explicitly out of scope

- **Hardening the permissive default for fine-grained classes** (rejecting a fine-grained-class write that
  has *no* `instanceOf`). That would contradict §1.6's flexible-write model and is a separate, larger
  decision. The lift restores enforcement parity for typed vertices; it does not change the default.
- The **P7 lint gate** (§8) is the Steward's build; the **P7 principle text** is Andrew's edit to
  `lattice-architecture.md`.

---

## 7. Alternatives considered

### 7.1 Option B — a few per-(family×role) DDLs (package-only, no platform change) — REJECTED

Ship a DDL for each fine-grained class: `service.backgroundCheck.template`,
`service.backgroundCheck.instance`, `service.payment.template`, `service.payment.instance`, … each with the
right `permittedCommands` + the script. The exact-match write-gate then works with **no platform change**.

*Re-asked per the alternatives discipline — could a variant of B beat A?* B's appeal is real: zero platform
risk, ships today. But three grounded costs make A the right call, not a narrow one:

1. **It re-couples what the directive separates.** The directive's intent is that the *discriminator* is the
   envelope class and the *type authority* is one thing. B makes **every discriminator value also a DDL** —
   so the discriminator and the authority are the same object again. Adding a family becomes a **meta-lane
   op + a duplicated script**, not a line of template *data*. A keeps them decoupled (the directive's
   intent) and makes a new family pure data.
2. **Script duplication + reverse-index ambiguity.** All families share one executing script, but B attaches
   it to N DDLs (or a shared Go const re-referenced N times). Worse, a shared op like `CreateServiceInstance`
   now appears in *two* vertexType DDLs' `permittedCommands` → the `ClassForCommand` **ambiguity guard**
   (`ddl_cache.go:443`) drops it from the reverse index → **every** caller must now carry an explicit
   `class`. A leaves one unambiguous owner per op; callers omit `class` as today.
3. **It scales O(families×roles) in DDLs.** Two families × two roles = 4 today, but the model is meant to
   grow (the directive anticipates more service families and other templated domains). A is O(1) in DDLs
   for any number of subtypes.

B would also be **thrown-away work**: shipped, then deleted when A lands. The committed stance is to skip the
interim and build A with the consumer.

### 7.2 Class-prefix resolution (`service.bgCheck.instance` → strip to `service`) — REJECTED

Resolve the governing DDL by stripping the fine-grained class to its first segment. Simpler than a graph
walk, no validator reads. Rejected because it **bakes a naming convention into the Processor** (the type must
be the literal first dotted segment) — brittle, and it re-introduces the prefix-matching §1.5 explicitly
excluded from Phase 1. The `instanceOf` link is the **explicit, data-driven** type relationship the
directive named; it also lets the authority be a richer vertex (template carrying provider/location) rather
than a string prefix. A variant ("prefix as a fast hint, instanceOf as the source of truth") buys nothing —
the walk is already cheap when `contextHint` is populated.

### 7.3 Script-asserted authority (the script declares its own `permittedCommands`) — REJECTED

Let the step-5 script (which already reads the template) tell the validator which DDL governs. Rejected on
security grounds: the write-gate must resolve authority **independently of the script**, or a script could
claim any `permittedCommands` and defeat the gate it exists to enforce. The resolver reads the graph itself.

### 7.4 Resolve in the DDL cache as a precomputed `fine-class → typeDDL` map — CONSIDERED, deferred

Precompute, at cache-build time, a map from every known fine-grained class to its type DDL by scanning
`instanceOf` links. Avoids per-write walks entirely. Rejected for **now** because it puts business-link
scanning into the DDL-cache refresh (today meta-only) and must stay coherent as `instanceOf` links mutate —
more invalidation surface for a hot-path optimization that the `contextHint`-hydrated walk does not need at
MVP scale (10–100 ops/s). Reserved as a pure optimization if write-gate latency ever shows up (§9).

---

## 8. Proposed new principle P7 (for Andrew to add to `lattice-architecture.md`)

> **P7: A vertex's type/subtype discriminator is the envelope `class`; never a `.class`/shadow aspect.**
> A vertex declares *what it is* in its envelope `class` field (Contract #1 §1.1). Fine-grained subtypes use
> a dotted class (`service.backgroundCheck.instance`); the **type authority** that governs the subtype's ops
> and `permittedCommands` is discovered by walking the vertex's `instanceOf` link to the nearest registered
> DDL (Contract #1 §1.5). A discriminator aspect (`.class`, `.family`, `.kind`, …) that shadows the envelope
> class is prohibited — it splits the type across two stores and forces lens/auth readers to pick. (The
> meta-model already obeys this: a meta-vertex carries its kind in the envelope `class`
> `meta.ddl.vertexType`, and `.canonicalName` is a *name*, not a type.)

Backed by a `lint-conventions` gate (the Steward builds it, modeled on the existing P5 gate in
`scripts/lint-conventions.go`): flag any package DDL/script that writes a `.class` / `.family` (or other
discriminator-shaped) aspect whose value mirrors a dotted type string. The two known outliers to fix are
service-domain's `.class` aspect and lease-signing's `.class` + `.family` aspects (both retired by the
Verticals refactor).

---

## 9. Risks & test strategy

| Risk | Mitigation |
|---|---|
| **Silent permissive-default regression** if a consumer ships a fine-grained class *without* an `instanceOf` link | Fire 1 test (c) pins the parity; the Verticals refactor draws `instanceOf` in every create batch by construction; P7 lint + a Gate-3 adversarial vector (a fine-grained write with no `instanceOf` must not gain enforcement it shouldn't, and a malformed `instanceOf` must not resolve to a wrong DDL) |
| **Validator read cost** (per-write `instanceOf` reads) | Lazy batch→working-set→on-demand resolution; `contextHint.reads` keeps the hot path read-free; the walk is ≤2 hops for real domains; §7.4 precompute reserved if latency shows |
| **Cycle / unbounded walk** via crafted `instanceOf` links | `MAX_INSTANCEOF_HOPS = 4` + `visited` cycle guard → *notFound* → permissive default (fail-open to today, never into a wrong DDL) |
| **Wrong-DDL resolution** (walk reaches the *wrong* type) | Terminal requires a **vertexType** DDL; tombstoned `instanceOf` links are ignored; ambiguity is impossible (a vertex has at most one live `instanceOf` per the domain model — assert this in tests) |
| **Backward compatibility** | Exact `Lookup` runs first and unchanged; every shipping coarse-class vertex resolves identically; the walk only runs on a miss |

**Test strategy.** Unit: the `resolveGoverningDDL` table (§6.1 a–e) + the cache/working-set seams. Integration
(`internal/processor`): an end-to-end commit of a fine-grained-class op through the real validator, asserting
PASS-when-admitted and `WriteScopeViolation`-when-not, plus the coarse-class regression set. Ephemeral-stack
e2e lands with the Verticals refactor (a real `CreateServiceInstance` → fine-grained vertex → gate enforced).
Gate 2 (bypass) + Gate 3 (capability-adversarial) gain a fine-grained-class write-scope vector.

---

## 10. Review

This is a security-plane, cross-cutting change to the sole Core-KV writer's write-gate. Per the Designer
skill (§3) a substantial design warrants an adversarial/party pass **before the Steward builds** — recommend
a `bmad-party-mode` or adversarial review focused on: the permissive-default fail-open boundary (§4 last
paragraph), the auth-neutrality claim (§2.4 — prove no path lets the discriminator class reach step 3), and
the cycle/depth bound. Fold findings in before Fire 1. (Mirrors the D1 design's §8 pre-build gate.)

---

## 11. Definition of done (for the Steward, post-ratification)

1. Fire 1 merged: `resolveGoverningDDL` in the step-6 validator, backward-compatible, full test table green;
   `go build ./...`, `make vet`, `golangci-lint`, `make verify-kernel`, Gate 2 (BLOCKED), Gate 3
   (DEFENDED + the new vector), `go test ./internal/processor/...` all green; CI green.
2. Contract #1 §1.5/§1.6 + Contract #2 §2.1 committed (post-ratification).
3. Loupe op-discovery confirmation test green (catalog unchanged under the lift).
4. P7 added to `lattice-architecture.md` (Andrew) + the `lint-conventions` P7 gate shipped (Steward).
5. Built **with** the Verticals consumer refactor (no standalone dark window).
