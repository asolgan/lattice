# Link- & aspect-triggered reprojection for plain / GrantTable lenses — design

**Status: 📐 awaiting-Andrew (ratification)** · Designer fire 2026-06-29 (Winston) · Lattice lane,
Refractor projection-maturity · Backlog row: *[Refractor] Link-triggered reprojection for plain /
GrantTable lenses* (planning-artifacts/backlog/lattice.md).

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

**Recommendation:** ratify the design; **sequence the build behind a real eager-freshness consumer** (a
package lens that opts in) — but the **e2e proving harness IS a sufficient first consumer** (mirrors how
D1.1's base cap-read lens shipped as a Lattice primitive proven by the seeders, ahead of the Verticals
package lenses). So Fire 1 is **not** dead scaffolding: it ships a verifiable platform primitive with a
test lens that exercises eager link+aspect reprojection end-to-end. One primary fire; an optional
precision/perf follow-on (Fire 2) only if measured cost warrants it.

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
- **Contract #6 §6.14** (read-path authorization / GrantTable) — **untouched, build-to.** GrantTable
  lenses gain eager link/aspect freshness with no change to the `actor_read_grants` shape, the protected/
  grantTable flags, or the RLS policy.
- **Contract #6 §6.2** (monotonic projection-write guard) — **honored unchanged.** Every fan-out
  reprojection writes through the same guarded write path; a GrantTable / auth-plane reprojection inherits
  the `projectionSeq` guard verbatim (no resurrection window opened). The guard is what makes the
  over-reproject-superset safe under reordering.
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
- **e2e (the proving consumer — ephemeral embedded-NATS stack, `jsstore.Dir`):** a synthetic plain lens
  (one full-engine GrantTable-shaped + one simple-engine plain) declaring `fanOut.anchorType`:
  1. **Eager link-create** — emit *only* a `manages`-shaped link (no vertex/aspect touch); assert the
     anchor's grant row gains the new anchor **without** any vertex event.
  2. **Eager link-tombstone** — revoke the link; assert the stale anchor is **retracted** (subsumes L309
     link-tombstone re-projection).
  3. **Eager aspect-only update** — mutate a projected aspect with no vertex root event; assert the
     projected field refreshes.
  4. **Trigger filter** — emit an unrelated link relation; assert **no** reprojection (ack-skip,
     observable via no write + the skip log).
  5. **Backward-compat** — a lens with no `fanOut` block: assert the link/aspect events still ack-skip.
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
  trigger filter; over-reprojection is **idempotent** (overwrite-by-reprojection under the §6.2 guard) —
  redundant work, never incorrectness. **Quantified bound:** ≤ `maxActors` (10 000) reprojections per
  triggering event, truncated-with-warning above (matching the auth-plane cap exactly). For the residence
  case the real fan is tiny (units a landlord manages × open leaseapps per unit).
- **Retraction interplay with L305 (GrantTable anchor-tombstone retraction).** Complementary, not
  overlapping: L305 handles the **anchor vertex** tombstone; this handles the **link** mutation
  (`manages` revoked → the leaseapp reprojects without the old landlord → the stale anchor drops from
  `authz_anchors`, an *update* via overwrite-by-reprojection; or, for a link-keyed grant row, a retract
  via the existing composite Delete). Both run under the §6.2 guard, so monotonic ordering holds. The
  e2e link-tombstone case pins this.
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
- **A3 — "a GrantTable reprojection could open a §6.2 resurrection window."** No — the reprojection writes
  through the **same guarded path**; the guard is plane-derived (auth-plane bucket / soft-tombstone
  empty-behavior), not fan-out-derived, so it stays on. *Folded:* §3 + the e2e tombstone case assert the
  guard holds.
- **A4 — default-direction of the opt-in.** Omitting `fanOut` ⇒ **no eager reprojection** (today's
  behavior), which is the **safe** default for a *freshness* feature (a missing trigger = eventual
  self-heal, never a security exposure — this is read-model freshness, not an authz grant decision). The
  authz *correctness* still rests on the §6.14 RLS membership + the §6.2 guard, which this does not touch.
  So default-off is correct here (unlike a default-open *grant*, which would be a bug). *Confirmed sound.*

**Pre-build gate (Designer-lane obligation, to discharge before Fire 1 is build-ready):** an adversarial
pass on the **GrantTable-on-the-auth-plane reprojection** specifically — confirm that a relationship-grant
lens reprojected by link fan-out cannot (a) double-write a stale grant under reorder (guard), or (b)
under-retract on a link tombstone that races the anchor tombstone (L305 interplay). This is the one
security-adjacent boundary; everything else is read-model freshness. *(Run on the design before the
Steward cold-starts Fire 1.)*

---

## 8. Fire-by-fire decomposition (for the Lattice Steward)

**Fire 1 — the plain-lens fan-out primitive + opt-in + e2e (the whole feature; one fire).**
Generalize the enumerator gate (`fanOutEnabled`/`fanOutMode`), add `reprojectAnchors(plain)` reusing
`evaluateForEntry` + the existing plain retraction, the `fanOut.{anchorType,triggerLinks,triggerAspects}`
lens-spec fields + activation validation, the `InstallPlainFanOut` wire-up, the `KindLink`/`KindAspect`
plain-path routing, and the full unit + e2e suite (§5) with a synthetic full-engine **and** simple-engine
opt-in lens. Independently shippable + green; the e2e test lens is the proving consumer (not dead
scaffolding). **Scale of review:** thorough-lead is *insufficient* because a GrantTable variant touches
the auth plane the RLS trusts — run the **full 3-layer** for Fire 1 (Blind / Edge-Case / Acceptance), per
the row's "security-plane — the grant source RLS trusts" note, after discharging the §7 pre-build gate.

**Fire 2 (optional follow-on, only if measured cost warrants) — precise invalidation.** Replace broad
BFS with the compiled invalidation forest for *both* auth and plain lenses (the precise-future the plan
already carries). Its own row; lands for both planes together. **Do not** build pre-emptively — broad BFS
is correct and sufficient; this is a perf refinement gated on an actual measured hot lens.

**Real eager-freshness consumer (Verticals lane, after Fire 1 ratifies + ships).** loftspace's residence
read-grant lens adds `fanOut: {anchorType: leaseapp, triggerLinks: [manages, appliesToUnit]}` so
ownership transfers reproject eagerly. This is **package work** (Verticals stream), not part of this
Lattice-lane primitive — but it is the production consumer the primitive exists for.

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
