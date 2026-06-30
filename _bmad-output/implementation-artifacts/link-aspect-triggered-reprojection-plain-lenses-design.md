# Link- & aspect-triggered reprojection for plain / GrantTable lenses — design

**Status: ✅ Andrew-ratified (2026-06-29)** · pre-build adversarial gate **✅ RUN 2026-06-29 (§7b)** ·
Designer fire 2026-06-29 (Winston) · Lattice lane, Refractor projection-maturity · Backlog row:
*[Refractor] Link-triggered reprojection for plain / GrantTable lenses* (planning-artifacts/backlog/lattice.md).

> **⚠️ Update 2026-06-29 — adversarial gate RUN; it recommends CONSOLIDATING this design into L308.**
> Andrew ratified, then asked for the §7 pre-build review to be discharged. It is now **§7b (✅ RUN)** and
> the headline finding (**F5**) is that this design **substantially overlaps the L308 "Negative /
> filter-retraction projection" design** (also 2026-06-29): L308 Fire 1 is the *same* plain-lens
> aspect/link reprojection, **minus my `ActorEnumerator`** — and because the **full engine re-executes by
> scanning** all anchors, L308's simpler seed-from-endpoint approach **already covers the real (full-engine)
> consumers** (residence / grant / protected lenses). **My enumerator is only needed for a simple-engine
> multi-hop lens, which has no live consumer = dead scaffolding.** **My recommendation: build L308 Fire 1
> as the shared primitive and FOLD this design's security findings into L308 — F1** (composite-GrantTable
> shrink ⇒ over-grant; restrict retraction to single-row-overwrite until L308's own Fire-2 retraction
> lands), **F2** (protected-array lens is *unguarded* last-writer-wins — route to D1), **F3** (tombstone-
> race) — then **demote this row to "subsumed by L308."** **Two things need your call: (1) consolidate
> into L308? (rec: yes); (2) F2** (the unguarded protected-array reorder posture — rec: accept as D1 M3 +
> file a hardening row). If you prefer to keep this row independent instead, Fire 1 is narrowed to
> single-row-overwrite lenses with a fail-closed F1-guard (§7b/§8).

---

## For Andrew (one-look ratification)

**What it does (two lines).** Today the **plain** Refractor projection pipeline ack-and-skips every
`KindLink` and `KindAspect` CDC event — only `actorAggregate` (capability) lenses react to a pure link or
aspect mutation. This design **generalizes the existing actorAggregate fan-out to plain (incl.
GrantTable) lenses**, opt-in per lens, so a relationship-scoped or aspect-derived projection reprojects
**eagerly** when the link/aspect that feeds it mutates — instead of staying stale until the anchor
vertex's next CDC touch.

**Architectural fork:** **none.** This is an internal Refractor capability that *reuses* the
adjacency-BFS-to-anchor-type enumerator the auth plane already runs (the `ActorEnumerator`, which is
already anchor-type-generic — only its name is capability-flavored). No new mechanism, no security plane,
no transport change.

**Frozen-contract change:** **none.** It builds to the committed contracts (#1 key-shapes, #6 §6.13/§6.14,
#6 §6.2 guard). The opt-in is a Refractor **lens-spec** field (the lens-spec schema is not a frozen
contract; §6.13's actorAggregate `anchorType` is the auth-plane analog, left untouched). Only a
`docs/components/refractor.md` doc update (not a contract) describes the generalized fan-out. The design
**questions and recommends keeping** the §6.13 actorAggregate machinery un-generalized — see §6.

**Why it's only ★ (and why it's still worth designing now).** The grounded consumer — loftspace's
leaseapp-anchored residence read-grant — already populates correctly via the **vertex**-triggered plain
path (the row's RESOLUTION corrected an earlier ★★★ "D1.3 blocker" premise). What's missing is **eager**
freshness: a unit-ownership *transfer* (a bare `manages` link mutation, no vertex/aspect change) leaves
the landlord's grant stale until each affected leaseapp's next CDC touch. It **self-heals**; this makes
it **immediate**. It also **subsumes the "Link-tombstone re-projection" backlog row** (L309) and serves
**every** future relationship-/aspect-scoped package lens, not just loftspace.

**One decision for Andrew — F2 (MEDIUM, D1 interplay).** The **protected-array** lens (the residence
consumer) and plain Postgres read models use the **unguarded** `PostgresAdapter` (last-writer-wins —
`projectionSeq` ignored); only the grant *table* (`GrantWriter`) and NATS-KV are seq-guarded. Eager
fan-out widens the reorder window for the security-bearing `authz_anchors` column (stale until the next
event — bounded, self-healing). This is the **D1 H4/M3 family** (H4 seq-guarded the grant table; the
protected-array lens was not). **My recommendation: accept it now as the ratified M3 CDC-lag posture** (the
eager fan-out *shortens* steady-state lag — net positive) **and file "seq-guard `ProtectedAdapter`" as a
D1 hardening row.** Alternatively, seq-guard the protected-array lens now (a broader D1 change). *Your
call.*

**Recommendation (build):** Fire 1 is **build-ready** for single-row-overwrite lenses (gate discharged).
The e2e proving harness is a sufficient first consumer (mirrors D1.1's base cap-read lens, proven by the
seeders ahead of the Verticals package lenses) — so Fire 1 is **not** dead scaffolding. The real residence
consumer (protected-array leaseapp lens) follows in the Verticals lane.

---

## 1. Problem + intent

### 1.1 The gap, grounded

Refractor's projection pipeline (`internal/refractor/pipeline/pipeline.go`) classifies each Core-KV CDC
event by Contract #1 §1.5 key shape and dispatches:

| Event kind | actorAggregate lens (`actorEnumerator != nil`) | **plain / GrantTable lens (`actorEnumerator == nil`)** |
|---|---|---|
| `KindVertex` (anchor) | re-execute / tombstone shortcut | re-execute; `AnchorDeleteResult` retracts on tombstone |
| `KindVertex` (secondary node) | endpoint fan-out | **re-execute** (engine refreshes dependent rows — `evaluate.go:104-115`) |
| `KindLink` | `evaluateLinkFanOut` (endpoint-seeded reprojection) | **ack-and-skip** (`pipeline.go:497-505` *"link mutation observed but no handler registered"*) |
| `KindAspect` | `evaluateAspectFanOut` (parent-seeded reprojection) | **ack-and-skip** (`pipeline.go:483-491`) |

So the gap is **narrow and precise**: a plain lens already reprojects when any **vertex** in its
traversal mutates (the executor re-scans / re-evaluates — `evaluate.go:104` comment: *"a deleted patient
nulls an appointment's patientName without deleting the appointment row"*). It does **not** reproject when
a pure **link** (relationship) or **aspect** mutates and no vertex root event accompanies it.

Concrete failure (the row's grounding, corrected): loftspace's `AssignUnitOwner`
(`packages/loftspace-domain/ownership.go`) emits **only** a `lnk.identity.<l>.manages.unit.<u>` link
mutation — no vertex/aspect change. A leaseapp-anchored residence read-grant lens walks
`(app:leaseapp)-[:appliesToUnit]->(u:unit)<-[:manages]-(landlord:identity)` and projects
`authz_anchors=[nanoIdFromKey(landlord.key)]`. When ownership transfers, **no leaseapp vertex is touched**
→ the plain pipeline never reprojects → the new landlord's read grant lags until each open leaseapp's next
unrelated CDC touch. It self-heals (eventually consistent), but is not eager.

The same shape recurs for any plain lens that **projects an aspect field** (Contract #1 aspect, e.g.
`node.unit.status.data.value`) and whose aspect can mutate without a vertex root event — an aspect-only
update is ack-skipped today (`reference_lens_aspect_projection`).

### 1.2 Intent (vision tie-in)

This is **Read-model / projection maturity** (backlog section; lattice-architecture.md "Deferred
Architectural Capabilities"). The Refractor's job is **eventually-consistent projection of the graph into
read models** (refractor.md mandate; brainstorming inventory #34–#39, #61 — "each Lens has one RETURN
producing one shape; CDC drives reprojection"). A projection that depends on a relationship or an aspect
but only reprojects on *vertex* events is a **freshness hole** in that mandate. The auth plane already
closed this hole for capability lenses (the link/aspect fan-out, Epic 12); this generalizes the same
machinery to the rest of the projection surface.

---

## 2. The shape

### 2.1 Mirror, don't reinvent — the enumerator is already generic

The actorAggregate fan-out is **two separable concerns**:

1. **Enumeration** — `ActorEnumerator.Enumerate(eventVertexKey, eventVertexType)` does an **undirected
   adjacency BFS** (depth-capped 10, set-capped 10 000) from a mutated vertex to the set of vertices of a
   configured **anchor type** (`actorType`), reading the operational Adjacency KV
   (`internal/refractor/adjacency`). *Nothing about this is capability-specific* — it is a generic
   "given a changed vertex, which anchors of type T may have changed?" superset (`actor_enumerator.go`;
   driver.go:145 *"the sound superset that can never miss an affected anchor … BFS over-reprojects, never
   under-reprojects"*).
2. **Reprojection** — `reprojectActors(actorKeys)` re-executes the cypher per anchor via
   `executeFullForActor` (binds `$actorKey`, applies the §6.13 envelope, derives the `cap.<actor>` delete
   key). *This half is capability-specific.*

The link/aspect fan-out paths (`evaluateLinkFanOut`, `evaluateAspectFanOut`, `evaluate.go:334-428`) =
enumerate from the link endpoints / aspect parent → `reprojectActors`. They also idempotently reflect the
link into Adjacency KV first (`adjacency.Build`, both directions) so the reprojection's traversal sees a
consistent edge set regardless of which consumer reached the link first (evaluate.go:336-347).

**The design: generalize concern (1) verbatim, parameterize concern (2).**

### 2.2 Why a plain lens needs the enumerator (engine-grounded)

Seeding reprojection *directly* from a link endpoint is **not** sufficient for a plain lens, because the
two engines bind their anchor differently:

- **Simple engine** (`ruleengine/simple`): the anchor is the first node of the first required MATCH
  (`compiler.go:54-64`, `AnchorLabel`/`AnchorVariable`); it evaluates **per seed node** of that label.
  Seeding from a `unit` or `landlord` endpoint when the anchor is `leaseapp` yields **nothing**. → must
  seed from the **anchor** vertices, found by walking the graph from the endpoint. **The enumerator is
  required.**
- **Full engine** (`ruleengine/full`): the executor seeds the anchor by **scan** (point-lookup only when
  the MATCH carries a `{key:$param}` binding, which a plain lens does not — `executor.go:277-345`,
  `seedNodes`). So a triggering event re-scans + reprojects all anchor rows; the seed node is just a
  trigger. The enumerator's anchor list is still the correct **seed set** (each found anchor is
  re-executed; the scan converges on the same rows).

So **one mechanism — the anchor enumerator — covers both engines**: enumerate the affected anchor vertices
from the link endpoints / aspect parent, then re-execute the plain lens seeded from each anchor.

### 2.3 The plain reprojection strategy

`reprojectActors` is renamed/generalized to `reprojectAnchors(anchorKeys, mode)`:

- **`mode = capability`** (unchanged): `executeFullForActor` + §6.13 envelope; missing anchor →
  Delete at `actorDeleteKeyFor` (`cap.<actor>`).
- **`mode = plain`** (new): for each anchor key, `fetchVertexProps`, build a
  `simple.NodeEntry{CoreKVKey, NodeLabel, Properties}`, call the **existing** `evaluateForEntry` path
  (the same call a vertex event takes — engine-agnostic, applies the lens's normal write/key shape). A
  **missing/tombstoned** anchor takes the **existing plain retraction**: `AnchorDeleteResult` (full
  engine, composite-key-aware — `anchor_delete.go`) / the simple engine's `deleteResult`. **No new
  delete-key derivation** — the plain path already retracts composite-keyed GrantTable rows correctly
  (L305 Fire 1, `faa3aec`/`d772195`).

This is the key insight that keeps the change small: the plain reprojection of an anchor is **byte-for-byte
the existing vertex-event path** — we are only widening the set of **triggers** that invoke it, not
inventing a new reprojection.

### 2.4 The opt-in (lens-spec field) + the trigger front-gate

A plain lens **opts in** to fan-out by declaring its anchor type. New Refractor lens-spec fields
(`internal/refractor/lens/corekv_source.go` source config + `schema.go`), **not** a frozen contract:

```yaml
fanOut:
  anchorType: leaseapp           # REQUIRED to enable plain-lens fan-out (the BFS target type)
  triggerLinks:   [manages, appliesToUnit]   # OPTIONAL front-gate; default = all relations
  triggerAspects: [status]                   # OPTIONAL front-gate; default = all aspect localNames
```

- **`anchorType`** — the vertex type the lens anchors on (the simple engine's `AnchorLabel`; for the
  full engine, the first MATCH node label). Enables fan-out; absent ⇒ today's ack-and-skip (full
  backward compatibility, zero behavior change for every existing lens).
- **`triggerLinks` / `triggerAspects`** — an **optional** cheap front-gate: on a `KindLink` event whose
  relation ∉ `triggerLinks`, ack-skip **before** any BFS (and symmetrically for aspect localNames).
  **Default = no filter = all** (the sound superset, matching the actorAggregate posture exactly —
  driver.go:145). Recommended for any production lens to avoid BFS on unrelated topology churn (e.g. the
  residence lens need not BFS on every `holdsRole` mutation).

**Validation at activation (fail-loud, mirrors `ValidateKeyColumns`):** if `fanOut.anchorType` is set, it
**must** equal the compiled rule's anchor label (simple) / first MATCH node label (full); a mismatch
**refuses activation** with an actionable log (a typo'd anchor type would silently never reproject — a
freshness hole that fails closed to "no eager reprojection", but we make it fail **loud** instead).

### 2.5 Wiring (the single dispatch point)

`cmd/refractor/main.go` install dispatch (the `switch` at L353) gains a third case, after
`IsActorAggregate` / `isOperationRoleIndexLens`:

```go
case projection.HasPlainFanOut(r):   // r.FanOut.AnchorType != ""
    projection.InstallPlainFanOut(p, r, adjKV, coreKV, logger)   // sets enumerator (anchorType) + plain reproject mode + trigger filter
```

`InstallPlainFanOut` mirrors `InstallActorAggregate` (driver.go:152) minus the §6.13 envelope/guard
machinery: `p.SetAnchorEnumerator(pipeline.NewActorEnumerator(adjKV, coreKV, r.FanOut.AnchorType))`,
`p.SetFanOutMode(pipeline.FanOutPlain)`, `p.SetTriggerFilter(r.FanOut.TriggerLinks, r.FanOut.TriggerAspects)`.

`pipeline.handle`'s `KindLink`/`KindAspect` cases change their gate from `if p.actorEnumerator != nil`
to `if p.fanOutEnabled()` (capability **or** plain); the link/aspect fan-out funcs consult
`p.fanOutMode` to pick the reprojection strategy. The auth-plane path is **byte-identical** (capability
mode, no trigger filter).

### 2.6 Read path (P5) / write path (P2) / P1

- **P5 (apps read lenses, never Core KV).** Unchanged — this *produces* the read model more eagerly; the
  consumer (RLS / app) still reads the lens target. The eager trigger does not add an app Core-KV read.
- **P2 (Processor is sole Core-KV writer).** Unchanged — Refractor writes **its own lens targets**, never
  Core KV. The fan-out re-reads Core KV (Refractor is a platform binary, allowed) and re-projects.
- **P1 (operational state outside Core KV).** The BFS reads **Adjacency KV** (operational), exactly as
  the auth-plane fan-out does. No business state added to Core KV.
- **Contract #1 key-shapes.** No new keys. The reprojection emits the lens's existing output keys
  (composite GrantTable rows / plain rows). The BFS parses §1.1 link keys (`ParseLinkKey`) and vertex
  keys verbatim. **No §1.1 directionality trap** — the BFS is *undirected*, so it is direction-agnostic
  (no inverted link, no mid-token-wildcard subtlety; we walk Adjacency KV, not subject filters).

### 2.7 Orchestration precedent mirrored

Mirrors **the actorAggregate link/aspect fan-out** (`evaluateLinkFanOut`/`evaluateAspectFanOut`,
`InstallActorAggregate`) — same enumerator, same adjacency-consistency pre-step, same over-reproject-never-
under-reproject soundness argument. It is the **read-model twin** of that auth-plane mechanism, decomposed
the same way (enumerate → reproject), per the "mirror the established internal pattern" discipline.

---

## 3. Contract surface

**No frozen-contract change.** Builds to:

- **Contract #6 §6.13** (actorAggregate Output descriptor) — **untouched**. The plain `fanOut.anchorType`
  is a *separate* Refractor lens-spec field for non-auth lenses; §6.13's `anchorType` governs the
  auth-plane envelope lens and is left exactly as ratified. (Reconciliation: §6 below argues *against*
  collapsing the two.)
- **Contract #6 §6.14** (read-path authorization / GrantTable) — **untouched, build-to.** Protected-array
  read models gain eager link/aspect freshness with no change to the `actor_read_grants` shape, the
  protected/grantTable flags, or the RLS policy. *(Composite-row GrantTable lenses with a shrinking key are
  scoped out of Fire 1 — §7b F1.)*
- **Contract #6 §6.2 / §6.14 guards — three distinct mechanisms, named precisely (the §7b gate corrected
  the earlier "inherits the guard verbatim" hand-wave).** The reprojection write goes through whichever
  adapter the lens targets, and **only two of three are seq-guarded**: **(1) NATS-KV** auth-plane
  (`cap-read.<actor>`) — `natskv.go` CAS guard, drops a `≤`-seq write; **(2) the grant *table*** — the
  `GrantWriterAdapter`/`PostgresGrantWriter` §6.14 monotonic guard on `projection_seq`; **(3) plain &
  protected-array Postgres** — the `PostgresAdapter`, which **ignores `projectionSeq`** (unconditional
  last-writer-wins, §6.2). The fan-out **threads the correct seq regardless** (`writeResults` stamps the
  triggering message's globally-monotonic stream sequence — §7b F4), so (1)/(2) are reorder-safe; (3) is
  LWW-by-design and self-heals on the next event (§7b F2, flagged for Andrew). **No resurrection window is
  opened on the seq-guarded targets;** the unguarded targets' reorder posture is unchanged in kind (eager
  fan-out only widens the rate — F2).
- **Contract #1 §1.1/§1.5** (key shapes) — build-to; no new keys.

**Doc (not contract):** `docs/components/refractor.md` "Link fan-out on the capability pipeline" (L288)
is rewritten "Link & aspect fan-out" to describe the generalized opt-in plain path + the trigger filter.

---

## 4. Reconciliation with the existing mental model

- **"Didn't we already handle this?"** Partly. The auth plane closed it for capability lenses (Epic 12
  fan-out). The *plain* path closes only the **vertex**-event half (`AnchorDeleteResult` + secondary-node
  re-execute). The **link** and **aspect** event halves are the lone fall-through — this design closes
  exactly those two, reusing the auth-plane machinery.
- **"Does this duplicate / contradict an established pattern?"** No — it *extends* the actorAggregate
  fan-out rather than building a parallel one. The `ActorEnumerator` was already anchor-type-generic; we
  give it a second caller. The precise-future (the compiled **invalidation forest**, which the plan
  already carries but the live pipeline ignores in favor of broad BFS — driver.go:145-149) is the *same*
  precise-future for both auth and plain lenses; this design stays on broad BFS (the sound superset),
  consistent with the auth plane.
- **"Does this introduce new state?"** No new persistent state. The enumerator is wired from the lens
  spec at activation; the BFS is stateless (reads live Adjacency KV); reprojection is stateless. The
  only *config* state is the opt-in `fanOut` block on the lens definition.
- **"Is this the §6.13 machinery again?"** No — deliberately not (see §6). §6.13 carries the envelope /
  guard / empty-behavior descriptor a *capability* lens needs; a plain domain lens has none of that.
  Reusing only the enumerator keeps the plain path lean.

---

## 5. Migration / compatibility · test strategy

**Migration.** Pure opt-in, zero migration. Existing lenses (none declare `fanOut.anchorType`) keep
ack-and-skip — **byte-identical behavior**. A lens adopts eager fan-out by adding the `fanOut` block (a
lens-definition change picked up on package (re)install; F-004 same-version caveat applies on a live dev
stack — a fresh `make up-full` or a version bump for live pickup).

**Test strategy.**

- **Unit (pipeline):** the generalized gate (`fanOutEnabled` = capability ∨ plain); the trigger filter
  (relation in/out, aspect localName in/out, default-all); `reprojectAnchors(plain)` over the
  full **and** simple engines (anchor found → reproject; anchor tombstoned → retract via the existing
  delete paths; endpoint reaching no anchor → no-op); the anchor-type validation refusal.
- **e2e (the proving consumer — ephemeral embedded-NATS stack, `jsstore.Dir`):** a synthetic
  **single-row-overwrite** lens pair (one **protected-array** full-engine lens with an `authz_anchors`
  column + one **plain** simple-engine lens) declaring `fanOut.anchorType` *(NOT a shrinking-key
  GrantTable — §7b F1)*:
  1. **Eager link-create** — emit *only* a `manages`-shaped link (no vertex/aspect touch); assert the
     anchor row's `authz_anchors` gains the new anchor **without** any vertex event.
  2. **Eager link-tombstone** — revoke the link; assert the anchor's `authz_anchors` **drops** the removed
     grantee via single-row overwrite (subsumes L309 link-tombstone re-projection).
  3. **Eager aspect-only update** — mutate a projected aspect with no vertex root event; assert the
     projected field refreshes.
  4. **Trigger filter** — emit an unrelated link relation; assert **no** reprojection (ack-skip,
     observable via no write + the skip log).
  5. **Backward-compat** — a lens with no `fanOut` block: assert the link/aspect events still ack-skip.
  6. **F3 — link-tombstone races anchor-tombstone (seq-guarded target):** delete the anchor vertex and
     tombstone its link near-simultaneously; assert **no resurrection** of the retracted row (higher-seq
     write wins; the tombstoned-anchor reprojection emits a Delete via `AnchorDeleteResult`, not an upsert).
  7. **F1-guard (fail-closed activation):** a `grantTable` lens declaring `fanOut` with a non-anchor-derived
     key column is **refused** registration (logged, no fan-out wired) — assert the refusal.
- **Regression:** the existing auth-plane fan-out e2e (capability lens) stays green unchanged (proves the
  gate generalization didn't alter the capability path).
- **Gates:** `go build ./...`, `make vet`, `golangci-lint`, STRICT lint-conventions, `go test`
  refractor (pipeline/projection/engine, **`-race`** on the fan-out), and the existing capability +
  GrantTable e2e.

---

## 6. Risks + alternatives (earn the recommendation)

### Alternatives considered

- **A — generalize §6.13 actorAggregate to plain lenses (reuse `projectionKind`).** *Rejected.* §6.13
  bundles the envelope, guard, empty-behavior, and one-row-per-anchor output a *capability* lens needs;
  a plain GrantTable lens has flat composite rows and no envelope. Forcing it through §6.13 would either
  bloat the contract or mis-shape the output. Reusing **only the enumerator** (the genuinely shared
  half) is the simplest extension. *Re-asked "could a variant beat the recommendation?":* a variant that
  factors §6.13 into "enumerator descriptor" + "envelope descriptor" is cleaner *in the abstract* but is
  a frozen-contract refactor for no functional gain today — defer until a second envelope consumer
  appears. **Recommendation holds.**
- **B — seed reprojection directly from the link endpoints (no enumerator).** *Rejected* — §2.2: the
  simple engine yields nothing seeded from a non-anchor node, so this silently under-reprojects for every
  simple-engine plain lens. The enumerator is the correctness-load-bearing piece.
- **C — always fan out plain lenses on link/aspect (no opt-in).** *Rejected* — regresses cost for the
  many plain lenses that project no relationship/aspect data (every link/aspect event in the bucket would
  BFS them). The opt-in + trigger filter scopes cost to lenses that need it; the auth plane accepts
  all-events fan-out because capability genuinely depends on most topology — a domain lens does not.
- **D — adopt the precise compiled invalidation forest now (per-relation, replaces broad BFS).**
  *Deferred* — it is the same precise-future the auth plane has **also** not adopted (driver.go carries
  it; the live pipeline ignores it). Matching the auth plane on broad BFS keeps one fan-out model across
  the codebase; adopting the forest is a separate, cross-cutting optimization (its own row) that should
  land for **both** planes together, not just plain lenses.

### Risks

- **Over-reprojection cost.** Broad BFS from a high-degree endpoint can touch many anchors. *Bounded* by
  the same depth-10 / set-10 000 caps (`DefaultActorMaxDepth`/`DefaultActorMaxSet`) and the optional
  trigger filter; over-reprojection is **idempotent for a single-row-overwrite lens** (the anchor's row is
  rewritten wholesale; seq-guarded on NATS-KV / the grant table, LWW on plain Postgres — §7b) —
  redundant work, never incorrectness. (Over-reprojection is **not** harmless for a *shrinking-key*
  composite GrantTable lens — that is exactly §7b F1, and why those are scoped out.) **Quantified bound:** ≤ `maxActors` (10 000) reprojections per
  triggering event, truncated-with-warning above (matching the auth-plane cap exactly). For the residence
  case the real fan is tiny (units a landlord manages × open leaseapps per unit).
- **Retraction interplay with L305 — CORRECTED by the §8 gate (was the design's worst error).** The
  earlier draft claimed a dropped relationship-grant row "retracts via the existing composite Delete."
  **That is false** — see §8 finding **F1**. The only composite-Delete path is the **anchor-vertex
  tombstone** (`AnchorDeleteResult`); **nothing retracts a composite row whose key merely *drops out* of a
  reprojection** (the upsert-only model never sees the old key). For the **protected-array** lens (the real
  residence consumer) the relationship change is a **single-row overwrite** (`authz_anchors` recomputed
  wholesale) — shrink-safe. For a **composite-row GrantTable** lens whose `actor_id` varies with the fanned
  relationship, the dropped row is **stale = over-grant**. The §8 gate **narrows Fire 1 to single-row-
  overwrite lenses** and **defers shrinking-key GrantTable fan-out behind L308**.
- **Adjacency consistency race.** Same race the auth-plane path already solves: the dedicated adjacency
  consumer and the fan-out both react to the link with no ordering guarantee → the fan-out idempotently
  `adjacency.Build`s the link (both directions) **before** enumerating (evaluate.go:336-347), so the
  reprojection never races ahead of its triggering edge. Reused verbatim.
- **Silent no-reproject on a mis-declared `anchorType`.** Closed by the §2.4 activation validation
  (refuse + actionable log on a label mismatch) — fails loud, not silent.

---

## 7. Self-adversarial pass (folded in)

Run as a focused adversarial pass (a ★ projection-maturity change, no contract/fork, mirroring a
heavily-reviewed auth path — a full party-mode is not warranted; the **pre-build gate** below is the
security-plane backstop).

- **A1 — "the full engine scans, so the enumerator is dead weight for full-engine lenses."** Partly true
  but kept: a uniform enumerator-then-reproject path covers *both* engines with one mechanism, and the
  full-engine scan re-executes from the enumerated anchors (convergent). Splitting by engine kind would
  add a branch for no correctness gain. *Folded:* documented as intentional uniformity, not waste.
- **A2 — "auth-plane regression risk from changing the `actorEnumerator != nil` gate."** The capability
  path must stay byte-identical. *Folded:* the gate becomes `fanOutEnabled()` which is **true for exactly
  the same lenses** when only capability lenses set the enumerator; the regression e2e (existing
  capability fan-out) pins parity; `fanOutMode` defaults to capability for the auth path.
- **A3 — "a GrantTable reprojection could open a resurrection window."** Sharpened by the §7b gate into
  **F1/F2/F4.** Resurrection (a stale *higher*-seq write) does **not** occur on the seq-guarded targets
  (F4 — seq is globally monotonic). The real defect the gate found is **non-resurrection over-grant**: an
  un-retracted *dropped* composite key (F1), plus the unguarded protected-array reorder window (F2). Both
  are addressed in §7b (scope + flag); the original A3 framing was too narrow.
- **A4 — default-direction of the opt-in.** Omitting `fanOut` ⇒ **no eager reprojection** (today's
  behavior), which is the **safe** default for a *freshness* feature (a missing trigger = eventual
  self-heal, never a security exposure — this is read-model freshness, not an authz grant decision). The
  authz *correctness* still rests on the §6.14 RLS membership + the §6.2 guard, which this does not touch.
  So default-off is correct here (unlike a default-open *grant*, which would be a bug). *Confirmed sound.*

---

## 7b. Pre-build adversarial gate — ✅ RUN 2026-06-29 (Andrew-requested), findings folded in

The §7 gate (an adversarial pass on the GrantTable-on-the-auth-plane reprojection) was **discharged** as a
grounded code-level pass (Winston, the adapter/guard/retraction paths read in `internal/refractor/adapter`
+ `pipeline`). Four findings; **F1 is a security-plane defect that changes the design's scope.**

- **F1 — HIGH (security plane). Over-grant: a composite-key GrantTable lens reprojected by link/aspect
  fan-out cannot retract a row whose key *drops out*.** Grounding: the only retraction paths are the
  **anchor-vertex tombstone** (`AnchorDeleteResult`, `ruleengine/full/anchor_delete.go`; simple
  `deleteResult`) and an explicit per-source `Delete`. **No path diffs the prior vs. current row-set** —
  an upsert-only reprojection that now emits *fewer* `(actor_id, anchor_id, grant_source)` rows leaves the
  dropped rows live (`grep` for shrink/diff/retract finds only the anchor-tombstone path). For a
  relationship-grant whose **`actor_id` varies with the fanned relationship** (e.g. ownership transfer
  L1→L2: reprojecting the leaseapp emits `(L2,…)` and silently leaves `(L1,…)` granted), this is an
  **over-grant on the table RLS trusts**. *This is exactly the failure eager link-triggering makes most
  reachable* — a `manages` change is precisely a row-set shrink. **Resolution (folded):** scope the eager
  fan-out to **single-row-overwrite lenses** — **plain lenses** and **protected-array lenses** — where a
  reprojection **overwrites the anchor's own single row** (the `authz_anchors` array recomputed wholesale,
  so a removed grantee drops out atomically; the real residence consumer is exactly this). A **composite-
  row GrantTable lens whose `actor_id` is not the reprojected anchor is REFUSED `fanOut` at activation**
  (fail-closed — see F1-guard below) until **negative/filter-retraction projection (L308)** lands; that
  L308 row is now this feature's **hard dependency for the GrantTable-shrink case**. (A GrantTable lens
  whose composite key is *fully* determined by the reprojected anchor — row-set never shrinks — may opt in;
  the activation guard checks this.)
  - **F1-guard (fail-closed activation):** `InstallPlainFanOut` **refuses** (logs + does not register
    fan-out) a `grantTable` lens declaring `fanOut` unless its key columns are all derivable from the
    declared `anchorType` vertex (non-shrinking). Mirrors `ValidateKeyColumns` — a shrink-prone grant lens
    silently over-granting is worse than no eager fan-out. Default-deny on the security plane.

- **F2 — MEDIUM (route to Andrew / D1). The protected-array lens and plain Postgres read models are
  *unguarded* (last-writer-wins) — eager fan-out widens their reorder surface.** Grounding:
  `ProtectedAdapter.Upsert/Delete` (`read_path_adapters.go:166-176`) delegate to `PostgresAdapter`, which
  **ignores `projectionSeq`** (`postgres.go:206-209,287-290` — "unconditional last-writer-wins, Contract
  #6 §6.2"). Only the **grant table** (`GrantWriterAdapter`→`PostgresGrantWriter`, seq-guarded) and
  **NATS-KV** (`natskv.go` CAS guard) enforce monotonicity. So a protected-array lens carrying the
  security-bearing `authz_anchors` column is **not** reorder-protected: under the Fire-3 multi-worker
  per-lane concurrency two reprojections of the same leaseapp row can land out of order, leaving a stale
  `authz_anchors` until the next event (a bounded, self-healing over/under-grant window). **Eager
  link/aspect fan-out increases the event rate → widens this window.** *Decision for Andrew:* this is the
  **D1 H4 / M3 family** (the grant *table* was seq-guarded by H4; the **protected-array lens was not**).
  Either (i) accept it as the ratified **M3 "CDC-lag per-resource revocation" posture** (the eager fan-out
  *shortens* steady-state lag — net positive — and LWW self-heals on the next event), or (ii) **seq-guard
  the protected-array lens too** (give `ProtectedAdapter` the §6.14 monotonic conditioning, a small D1
  follow-on). **Recommendation: (i) accept now + file (ii) as a D1 hardening row** — the window is bounded
  and self-healing, and forcing a guard onto the plain Postgres adapter is a broader D1 change than this
  freshness feature should carry. Flagged, not silently absorbed.

- **F3 — MEDIUM (implementation-pinning).** The fan-out reprojection of a **tombstoned anchor** must invoke
  the anchor's own retraction (`AnchorDeleteResult` with the *anchor's* key/type — `eventType ==
  anchorLabel` ⇒ `ok=true` retracts), **not** the link's key, and **not** an upsert (which on a missing
  anchor would resurrect). For the **seq-guarded** targets (NATS-KV cap-read, grant table) the
  link-tombstone-races-anchor-tombstone race is safe (higher-seq wins, `writeResults` stamps `msg.Sequence`
  — F4); for **unguarded** plain/protected Postgres it is LWW (the last-processed wins, self-heals on next
  event). **Folded into §5:** the e2e adds an explicit *link-tombstone-races-anchor-tombstone* case
  asserting **no resurrection** on the guarded target.

- **F4 — POSITIVE (confirms gate question (a)).** `projectionSeq` threading is **sound**: `writeResults`
  (`pipeline.go:658`) stamps `results[i].ProjectionSeq = msg.Sequence` on **every** result including the
  fan-out reprojection, and all CDC events share **one** Core-KV JetStream stream, so the seq is **globally
  monotonic** across vertex/link/aspect kinds. A link-triggered reprojection therefore carries a seq
  strictly higher than any prior write to the same key → the §6.2/§6.14 monotonic guard on the seq-guarded
  targets **cannot** be fed a stale-but-higher seq. Gate question (a) "double-write a stale grant under
  reorder" **does not materialize** for NATS-KV cap-read or the grant table. (It is moot for the unguarded
  Postgres targets, which are LWW by design — F2.)

- **F5 — HIGH (cross-design reconciliation — the most important finding; needs Andrew's call). This design
  substantially OVERLAPS the L308 "Negative / filter-retraction projection" design (also 2026-06-29), and
  L308's approach is simpler and sufficient for the real consumers.** L308 Fire 1 is *"plain-lens
  aspect/link reprojection (mirror `evalAspectFanOut`/`evalLinkFanOut`) — **minus the actor enumeration**,
  reproject from owner/endpoint vertices directly"* (L308 design §2.1). The two designs touch the **same
  code** (the `actorEnumerator == nil` gate in the `KindLink`/`KindAspect` handlers) and would **collide /
  force rework** if built independently. **Grounding that makes mine the redundant one:** the **full
  engine re-executes by SCANNING all anchors** regardless of seed (`executor.go:277-345`, `seedNodes`), so
  L308 Fire 1's seed-from-endpoint + re-execute **already reprojects a full-engine multi-hop lens
  correctly** (the scan finds the affected leaseapps on a `manages` event). **The real consumers —
  residence / grant / protected lenses — are FULL-engine** (`nanoIdFromKey`, composite keys), so L308 Fire
  1 covers them **without** my `ActorEnumerator`. My enumerator is strictly necessary **only** for a
  **simple-engine multi-hop** lens (anchor not the owner/endpoint), for which there is **no identified live
  consumer** → by the dead-scaffolding test, my distinct mechanism is **premature**. **Recommendation
  (mine, for Andrew):** **consolidate — build L308 Fire 1 as the shared plain-lens reprojection primitive**
  (simpler, cheaper, and it frames freshness as an always-on correctness fix rather than my opt-in, which
  *defaults a plain lens to silently-stale*); **fold this design's genuinely-additive findings into L308:**
  **(i) F1** (composite-GrantTable shrink ⇒ over-grant; scope retraction to single-row-overwrite until the
  L308 Fire-2 retraction lands — L308's own Fire 2 *is* that retraction), **(ii) F2** (protected-array
  unguarded-reorder, route to D1), **(iii)** the optional trigger-relation filter as a cost knob, **(iv)**
  the F1-guard. **Then demote this row to "subsumed by L308; the simple-engine-multi-hop enumerator stays
  designed-on-the-shelf until a consumer appears."** *(This reverses my own pre-gate framing — the
  enumerator generalization read as elegant but is heavier than the problem the live consumers actually
  have. The honest output of the gate is "build the simpler L308, not this.")*

**Gate verdict:** **sound but largely redundant.** For the real (full-engine) consumers, **L308 Fire 1 is
the better primitive** — simpler, cheaper, always-on-correct (F5). This design's lasting value is the
**security analysis** it forces (F1 composite-shrink over-grant, F2 protected-array reorder, F3
tombstone-race) — which must travel to L308 regardless of which row builds. **Recommendation to Andrew:
consolidate into L308 + fold F1–F3; shelve this design's distinct enumerator (no live consumer).** If
instead kept independent, Fire 1 is narrowed to single-row-overwrite lenses with the F1-guard as written
above. The §3 guard wording is corrected below.

---

## 8. Fire-by-fire decomposition (for the Lattice Steward)

**Fire 1 — eager link/aspect fan-out for SINGLE-ROW-OVERWRITE lenses (plain + protected-array).**
*(Scope narrowed by the §7b gate — F1.)* Generalize the enumerator gate (`fanOutEnabled`/`fanOutMode`),
add `reprojectAnchors(plain)` reusing `evaluateForEntry` + the existing plain retraction (incl. the F3
tombstoned-anchor `AnchorDeleteResult` path), the `fanOut.{anchorType,triggerLinks,triggerAspects}`
lens-spec fields + activation validation **including the F1-guard** (refuse `fanOut` on a `grantTable`
lens whose key is not fully anchor-derived), the `InstallPlainFanOut` wire-up, the `KindLink`/`KindAspect`
plain-path routing, and the full unit + e2e suite (§5) with a synthetic full-engine **and** simple-engine
opt-in lens **of the protected-array / plain shape** (NOT a shrinking-key GrantTable), plus the F3
link-tombstone-races-anchor-tombstone case. Independently shippable + green; the e2e test lens is the
proving consumer (not dead scaffolding). **Review:** thorough-lead is *insufficient* (the protected-array
`authz_anchors` is the auth plane RLS trusts) — run the **full 3-layer** (Blind / Edge-Case / Acceptance).
The §7b gate is already discharged.

**Fire 2 (DEFERRED behind L308 — the §7b F1 dependency) — eager fan-out for shrinking-key composite
GrantTable lenses.** Requires **negative/filter-retraction projection (L308)** so a reprojection that
emits fewer composite rows **retracts** the dropped ones. Only then is the F1-guard relaxed to admit a
relationship-grant GrantTable lens whose `actor_id` varies with the fanned relationship. **Do not build
before L308 lands** — eager add-without-retract is a security regression (over-grant). The real residence
consumer does **not** need this (it uses the protected-array model — Fire 1).

**Fire 3 (optional, only if measured cost warrants) — precise invalidation.** Replace broad BFS with the
compiled invalidation forest for *both* auth and plain lenses (the precise-future the plan already
carries). Its own row; lands for both planes together. Not pre-emptive — broad BFS is correct and
sufficient.

**Real eager-freshness consumer (Verticals lane, after Fire 1 ships).** loftspace's residence read model
is the **protected-array leaseapp lens** (`authz_anchors=[nanoIdFromKey(landlord)]`, single-row overwrite
— the shrink-safe shape, per the backlog row's own RESOLUTION) and adds
`fanOut: {anchorType: leaseapp, triggerLinks: [manages, appliesToUnit]}` so ownership transfers reproject
eagerly **and** correctly drop the prior landlord. Package work (Verticals stream).

---

## 9. Grounding index (for the builder)

- Gap: `internal/refractor/pipeline/pipeline.go:481-511` (KindLink/KindAspect ack-skip);
  `pipeline/evaluate.go:66-115` (plain vertex-event reprojection + `AnchorDeleteResult` fall-through).
- Mirror: `pipeline/evaluate.go:319-428` (`evaluateFanOut`/`evaluateLinkFanOut`/`evaluateAspectFanOut`/
  `reprojectActors`); `pipeline/actor_enumerator.go` (the generic BFS); `projection/driver.go:138-202`
  (`InstallActorAggregate`).
- Engines: `ruleengine/simple/compiler.go:54-64` (anchor label, per-seed evaluate);
  `ruleengine/full/executor.go:277-345` + `seedNodes` (scan-seeded anchor);
  `ruleengine/full/anchor_delete.go` (composite-key retraction).
- Wire point: `cmd/refractor/main.go:320-368` (install dispatch).
- Lens spec: `internal/refractor/lens/corekv_source.go` (source config + `GrantTable`/`Protected`),
  `internal/refractor/lens/schema.go` (`ResolvedEngine`, key columns).
- Consumer: `packages/loftspace-domain/ownership.go` (`AssignUnitOwner`, link-only mutation).
- Contracts (build-to): docs/contracts/06-capability-kv.md §6.2/§6.13/§6.14; docs/contracts/01 §1.1/§1.5.
- Subsumes backlog row L309 (Link-tombstone re-projection); complements L305 (GrantTable anchor-tombstone
  retraction).
