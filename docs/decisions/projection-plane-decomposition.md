# Decision record — Projection-plane integrity & capability decomposition

- **Date:** 2026-06-07
- **Adjudicator:** Winston (architect session), with Andrew
- **Input:** `_bmad-output/planning-artifacts/refractor-lens-decomposition-brief.md`
- **Output:** Epic 12 (`_bmad-output/planning-artifacts/epics/phase-2-epics.md`) + proposed contract
  amendments (`cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` Requests 4–7,
  `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md`).
- **Status of the brief's god-cypher open item** in `lattice-architecture.md`: this record proposes the
  resolution; folding it back into `lattice-architecture.md` is the planning lead's (Andrew's) call.

This record captures the *adjudicated decisions* and the *evidence* behind them. The brief was the
agenda; this is the conclusion. Where Winston deviates from the brief's lean, it is called out.

---

## D-INTEGRITY — confirmed-reachable security bug; monotonic `projectionSeq` guard

### The bug is real, not theoretical

The brief asked for the resurrection reachability to be "assessed explicitly." It is reachable, and
here is the exact chain, grounded in code:

1. The pipeline retry queue captures an **evaluation result (a row)**, not a re-evaluation:
   `enqueueRetry` snapshots `capturedResult := result` and the replay `WriteFn` calls
   `a.Upsert(rctx, capturedResult.Keys, capturedResult.Row)` —
   `internal/refractor/pipeline/pipeline.go:929-950`.
2. `CreateTask` fans out 4 CDC events (vertex + 3 links); each independently triggers an actor
   reprojection.
3. Sequence: an **open-era** `Upsert` (grant present) fails transiently → captured into the retry
   queue. The task later closes; the close event reprojects the actor → zero real grants →
   `ErrDeleteProjection` → `Delete` succeeds (`capabilityenv/envelope.go:166-169`).
4. The retry of the captured open-era `Upsert` fires **after** the delete and re-writes the revoked
   grant. **No further CDC event re-deletes it** — the task is already closed, so nothing re-triggers
   that actor's ephemeral reprojection.

On `capabilityEphemeral` this resurrects a **revoked ephemeral grant on the security plane** (Contract
#6 is security-critical: "A bug here equals privilege escalation"). On `my-tasks` it resurrects a
closed task (a queryable surface lies; no auth consequence). The 7.2 E2E masks it today with a
`requireQuiescentRevision` settle-wait.

### Mechanism — Winston overrides the brief's lean

The brief leaned on reusing `projectedFromRevisions` (the Story 1.5.4 auth-coherence vector) as the
ordering guard. Winston rejects that **as the guard** (keeps it as the coherence datum), for two
concrete reasons:

- **`projectedAt` cannot order these writes.** `projectedAt` is derived from the **anchor (actor)
  vertex's** commit provenance (`pipeline/evaluate.go:48-58`, `projectedAtFromProvenance`). When a
  *task* closes, the **actor vertex is unchanged**, so the open-era and close-era projections of the
  same actor carry an *identical* `projectedAt`. It is provenance, deliberately not a clock — correct
  for replay, useless for ordering a task-driven reprojection.
- **`projectedFromRevisions` is both incomplete and ambiguous as an ordering key.** Today it stamps
  only the actor + lens-def revisions (`capabilityenv/envelope.go:99-110`) — it never records the
  task/link sources. Even fixed, the source *set shrinks* on close (the closed task drops out), so a
  per-key dominance comparison is ill-defined — the brief's own "open question."

**Decision:** stamp every actor-aggregate projection write with a monotonic **`projectionSeq`** = the
**JetStream stream sequence of the triggering CDC message**. It is:

- **Totally ordered by the substrate** — all Core-KV CDC for a lens flows on one stream; a retried
  write replays its *original* (lower) seq, so it loses to any later real reprojection.
- **Plan-independent** — needs nothing from the source-set, sidestepping the multi-source dominance
  question entirely.
- **Deterministic-replay-safe** — a full rebuild replays in stream order, the highest-seq write wins,
  and the steady state is identical (unlike an adapter-local counter or wall-clock, brief option (c),
  which Winston rejects for breaking replay determinism).

**Adapter behavior:** the NATS-KV adapter writes **conditionally** (CAS against the target key's KV
revision): read current `projectionSeq`; drop the write as an idempotent no-op when
`incoming ≤ stored`; on KV-revision conflict, re-read and re-compare. **`Delete` becomes a soft
tombstone** carrying `projectionSeq` (+ `isDeleted:true`) so the high-water mark survives physical
absence. Step-3 already denies on both absent key and tombstone (no grants → no match), so auth
semantics are unchanged (Contract #6 §6.8).

`projectedFromRevisions` is *separately* widened by D-PIPELINE's compiled plan to cover the full
contributing source set (including read-but-excluded sources). Two concerns, two data: `projectionSeq`
= write ordering; `projectedFromRevisions` = coherence/debug.

**Scope:** the actor-aggregate classes — `capabilityEphemeral` (security) and `my-tasks` (correctness)
at minimum; the primary `capability` / `capabilityRoleIndex` fan-out projections for consistency.

→ **Stories 12.1a** (guard, ships first) **+ 12.1b** (rebuild reconciliation + primary lens). Contract
#6 §6.2/§6.3/§6.8 amendment = Request 4; my-tasks tombstone consumer obligation = Request 4b.

---

## D-PIPELINE — compiled invalidation plan + `projectionKind: actorAggregate` marker

The brief's recommended option 1 (compiled invalidation + typed projection-kind) is adopted. Winston's
value-add is that **the machinery already exists on both sides**, so this is tractable, not an
open-ended compiler project:

- The **simple** engine already compiles reverse-traversal invalidation: `simple.reverseTraverse` /
  `walkBackToAnchor` walk `QueryPlan.Steps` backward from a changed non-anchor node to the affected
  anchors (`ruleengine/simple/evaluator.go:193-248`) — the Materializer pattern the brief wants to
  revive.
- The **full** engine has a clean, ANTLR-free AST (`ruleengine/full/ast.go`): `Match` patterns with
  node/rel chains, `Direction`, and `MinHops`/`MaxHops`. The invalidation compiler walks these into a
  `simple.TraversalStep`-shaped plan and **reuses the existing reverse-traversal**, replacing the broad
  `ActorEnumerator` BFS (`pipeline/actor_enumerator.go`) for full-engine actor-aggregate lenses.

**The per-name switch fully reduces to declarative aspects.** The four wrappers
(`capability`, `capabilityRoleIndex`, `capabilityEphemeral`, `myTasks` —
`cmd/refractor/main.go:256-313`) differ only in: output-key pattern, which RETURN columns form the
body, freshness stamping, and empty→delete-vs-skip. Even the `realEphemeralGrants`/`realOpenTasks`
"drop degenerate `{taskKey:null}` collect artifacts" logic (`capabilityenv/envelope.go:187-205,
287-305`) generalizes to a declarative `realnessFilter`. The whole behavior =
`projectionKind: actorAggregate` + a small Output descriptor + the compiled plan, **no Go**:

```
projectionKind:   actorAggregate
anchorType:       identity          # or inferred from MATCH (x:identity {key:$actorKey})
outputKeyPattern: "cap.ephemeral.{actorSuffix}"
bodyColumns:      [ephemeralGrants] # which RETURN aliases form the doc body
emptyBehavior:    delete            # delete | softDelete | emptyDoc | skip
realnessFilter:   { field: taskKey } # drop degenerate collect artifacts
freshness:        auto              # stamp projectionSeq (12.1) + widened projectedFromRevisions
```

**Fallback policy (fail closed on the security plane):** an actor-aggregate lens whose MATCH uses a
construct the narrow compiler does not cover **fails activation** when it is an auth-plane lens, and
logs a fallback-to-broad-BFS warning when it is not.

**12.4 clarification — fail-closed-refuse binds only once the compiled forest drives live fan-out.**
The fallback policy above assumes the compiled invalidation forest *is* what reprojects affected
anchors. As of 12.4 it is not: live fan-out still runs through the broad-BFS `ActorEnumerator`
(`pipeline/actor_enumerator.go`), which the 12.2 oracle proved is a sound **superset** of the
compiled forest (it over-reprojects, never misses an affected anchor). The built-in `capability`
cypher uses a variable-length `containedIn*0..` hop the narrow compiler cannot prove subset-safe, so
`projection.Compile` returns a `*CompileError` for this auth-plane lens. Refusing activation would
take the live auth plane *down* for a lens that projects correctly via BFS — strictly worse than the
status quo, with no security gain (BFS cannot under-reproject). So `installActorAggregate` logs the
coverage gap loudly (Warn) and activates with descriptor + BFS rather than refusing; a
non-`*CompileError` (e.g. a malformed descriptor) still refuses. `Compile`'s 12.3 fail-closed
contract and tests are untouched. **Carried obligation:** when a later story wires the compiled
forest into live fan-out (replacing BFS for these lenses), this Warn-and-proceed *must* be flipped
back to fail-activation — at that point an uncovered auth-plane lens would genuinely under-reproject.
Until then, fail-closed-refuse protects a forest-driven path that does not yet exist live.

**Sequencing:** spike (prove the compiler equals the BFS on a fixture — **12.2**) → build the plan
compiler + `projectionKind` marker (**12.3**) → migrate the four built-ins off the switch and **delete
the switch** (**12.4**). The 12.4 acceptance gate is a proof test that installs a *brand-new*
actor-aggregate package lens with **zero** edits under `cmd/` or `internal/refractor/capabilityenv/`.

Contract #6 §6.13 (`projectionKind` aspect) + §6.3 (`projectedFromRevisions` widening) amendments =
Requests 5–6.

---

## D-PROJECTION + D-CONSUMER — decompose the god-cypher; the consumer is what keeps it O(1)

The brief treats D-CONSUMER as a symmetric nicety. Winston elevates it: **decomposing the projection
to disjoint keys multiplies the step-3 read fan-out, and the generic dispatcher is the mechanism that
keeps the auth hot path single-GET.**

- Today step-3 reads **one** `cap.<actor>` doc and scans its sections. Move role/permission and
  service-access to disjoint keys (`cap.roles.<actor>`, `cap.svc.<actor>`) and that single doc no
  longer holds them.
- **Resolution:** step-3 **already path-dispatches** (task/service/platform) *before* the read — the
  pattern the 7.1 ephemeral extraction established (`step3_auth_capability.go:142-166`). So each path
  reads exactly **one** path-specific disjoint key. The single-GET hot path is **preserved**. The
  denial-path `actorRoles` second read stays off the hot path.

So D-CONSUMER (generic dispatcher over package-registered grant-matchers, dispatch table as
install-time data) is not optional polish — it is the read-side half of the decomposition. **D-PROJECTION
and D-CONSUMER land together per grant-type** so read and write sides never drift:

- **12.5** — generic step-3 auth-hook dispatcher; existing three matchers re-expressed as hooks,
  behavior identical. Security-critical (Gate 2/3). Contract #2 §2.8 / Contract #6 amendment =
  `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md`. **Landed:** registry is a precedence-ordered slice
  `[task, service, …extras, platform]` (platform predicate is the always-true catch-all, last);
  `selectEntry` returns the first match → exactly one key → one GET; duplicate-name and
  missing-predicate/kind/key entries are rejected at construction (`buildAuthRegistry`); the
  `extraEntries` seam is core-injected only (no package-data path yet — that is 12.6). **Carried
  obligation for 12.6:** when entries become **package-derived** (install-time data), duplicate
  detection must move from name-only to **predicate-overlap / coverage** — a uniquely-named extra whose
  `selects` overlaps a core path (or is always-true) must be rejected at install, not silently shadowed
  by precedence. Today extras are trusted core wiring, so name-dedup suffices; the day a package
  supplies an entry, the overlap guard becomes load-bearing (a broad extra ordered before `platform`
  could otherwise capture the platform path onto a package-controlled key).
- **12.6** — `rbac-domain` owns role/permission projection (disjoint key) + registers its hook;
  bootstrap cypher drops the rbac MATCHes.
- **12.7** — retire the god-cypher's service/location remnants. **Two-path** (Andrew, 2026-06-07):
  the `service-location` package **does not exist** (`packages/service-location/` holds only a
  `CONCEPT.md` write-up authored this session). **Path A** (package exists at story time) → it ships
  `capabilityServiceAccess` + hook per the concept doc. **Path B / default** (absent) → just **delete**
  the service/location MATCHes from the bootstrap cypher; the service path's matcher + `cap.svc.*` key
  stay registered-but-empty until a real service package projects into them (a pure package addition,
  no core edit, thanks to 12.3–12.5). Either path: core stops referencing service/location types and
  the god-cypher open item resolves. **Do not build a placeholder package for symmetry.**

All three are the auth hot path / security plane → full 3-layer adversarial review + Gate 2 (BLOCKED) +
Gate 3 (DEFENDED).

## Execution order (Andrew, 2026-06-07): Epics 7–10 → 12 → 11

Epic 12 lands **before** Epic 11 (Loftspace reference vertical) despite the higher number. (1) Epic
11's convergence e2e treats tasks / ephemeral grants / `my-tasks` vanish-on-close as correctness
guarantees — exactly what 12.1a/b make sound; building 11 first inherits the masked resurrection race
and the settle-wait crutch. (2) Authoring `lease-signing` on the decomposed model (12.3–12.5) lets the
reference package own its grant projections via the contract-contribution path instead of being
written against the god-cypher and migrated later.

---

## Sequencing summary

```
12.1a D-INTEGRITY guard ............ ships FIRST, no deps (security fix)
12.1b guard ↔ rebuild reconciliation + primary capability lens ← 12.1a
12.2  invalidation-compiler spike .. informs 12.3
12.3  plan compiler + projectionKind  ← 12.2
12.4  migrate built-ins, delete switch ← 12.3, 12.1a, 12.1b
12.5  generic auth-hook dispatcher .. ← 7.1 (hard prerequisite of 12.6)
12.6  rbac-domain projection + hook .. ← 12.4, 12.5
12.7  retire service/location remnants ← 12.6   [two-path: implement if package exists, else delete remnants]
```

Then **Epic 11** (Loftspace) runs on the post-12 plane.

`D-PIPELINE` (12.2–12.4) is the keystone that makes "packages own their projections" true.
`D-PROJECTION`+`D-CONSUMER` (12.5–12.7) is the decomposition it unblocks. `D-INTEGRITY` (12.1) is
independent and goes first because it is a live security exposure.

## Party review (2026-06-07) — 13 corrections, applied

The proposal was put through `bmad-party-mode` (Bob/Amelia/Quinn/Sally/John, Winston adjudicating)
*before* any story launch — the step we skipped for Epic 8, which then needed per-story
course-correction. All findings were code-grounded (verified against the lines cited). Outcome: the
original 7 stories became 8 (12.1 split into 12.1a guard + 12.1b rebuild reconciliation), and 11
further corrections were threaded through. Ledger:

| # | Gap (who found it) | Resolution | Story |
|---|---|---|---|
| 1 | `projectionSeq` had no delivery path — it lives on `msg.Metadata()`, not in `EvalResult`/envelope (Amelia) | Thread `msg.Metadata().Sequence.Stream` → new `EvalResult` field (so the retry capture carries it) → adapter | 12.1a |
| 2 | `adapter.Adapter` has no seq param; `Delete`'s row is nil; Postgres ripple (Amelia) | Extend the interface; Postgres = pass-through no-guard, only NatsKV enforces | 12.1a |
| 3 | Retry runs on its **own goroutine** (`retry.go:102`) → real key race → plain read-then-write is racy (Amelia) | CAS via `Update`/`ExpectedRevision` + bounded re-read-on-conflict loop, as AC | 12.1a |
| 4 | Guard breaks `Rebuild(truncate=false)` — historical low-seq replays rejected, rebuild restores nothing (Amelia) | New story: force-truncate-on-guarded-bucket OR documented rebuild-bypass; tested | **12.1b** |
| 5 | my-tasks E2E asserts "must vanish" (`refractor_mytasks_e2e_test.go:226`) — soft-tombstone breaks it (Quinn) | Flip assertion to tombstone; remove `requireQuiescentRevision` settle-wait | 12.1a |
| 6 | Need fail-without/pass-with adversarial test in Gate 3, not a lone unit test (Quinn) | AC: test FAILS on `main`, PASSES with guard; lands in Gate 3 suite | 12.1a |
| 7 | `capabilityRoleIndex` is keyed by `operationType`, not actor — not an actorAggregate (Amelia) | Excluded from the guard family (12.1b) and from the actorAggregate migration (own kind or bespoke) | 12.1b/12.4 |
| 8 | **12.5 "register a matcher" = code or data?** Lattice packages are data-only (Amelia) | Pinned: **fixed core matcher *kinds*, package-*declared* keys**; one-key-per-path invariant | 12.5 |
| 9 | Primordial-vs-rbac platform-permission composition undefined (Amelia) | Composition AC: dispatcher reads one key by actor class | 12.6 |
| 10 | `projectedFromRevisions` excluded-sources needs executor instrumentation, unscoped (Amelia) | Scope decision forced into the AC (in-scope or explicit follow-up); Request 6 narrowed | 12.3 |
| 11 | my-tasks tombstones → any UI/query reader must filter `isDeleted` (Sally) | Contract #10 §10.1 forward-obligation amendment | 12.1a / Contract #10 |
| 12 | "bypass suite passes **unchanged**" is false — fixtures/oracles migrate when grants move keys (Quinn) | Per-story "fixtures migrate, outcomes hold" AC | 12.5/12.6/12.7 |
| 13 | 12.7 full service migration re-imports scope risk; brief said rbac/service "NOT scheduled" (John) | First marked stretch; **superseded same day (Andrew)** — `service-location` doesn't exist, so 12.7 became a **two-path** story (implement if package exists, else just delete the god-cypher remnants), and is committed (Path B is cheap). Concept doc written: `packages/service-location/CONCEPT.md`. | 12.7 |

The single highest-value catch was #8: without the party review, a dev agent would have hit 12.5
assuming package-shipped matcher code, discovered Lattice is data-only mid-implementation, and stalled
— exactly the Epic 8 failure mode.

## Frozen-contract discipline

Contract #6 §6.2/§6.3 and Contract #2 §2.8 are FROZEN. All shape changes here are raised as
**amendment requests** (the `CONTRACT-AMENDMENT-REQUEST.md` files), not in-flight edits — per
`CLAUDE.md`. The planning lead ratifies before any frozen contract or `lattice-architecture.md` text
changes.

## Story 12.6 — god-cypher decomposition (implemented)

Closing story of Epic 12. Two parts landed together; the bootstrap `capability` cypher no longer
references the rbac (role/permission/holdsRole/grantedBy) OR service/location
(containedIn/availableAt/unavailableAt/permitsOperation) vocabularies.

### Part A — rbac role/permission projection moves to `rbac-domain`

`packages/rbac-domain` now declares two lenses (`packages/rbac-domain/lenses.go`):

- **`capabilityRoles`** (actor-aggregate) — walks `identity -[:holdsRole]-> role <-[:grantedBy]-
  permission` and projects each role-holding actor's role-derived `platformPermissions[]` + `roles[]`
  to the disjoint key `cap.roles.<actor>` (Contract #6 §6.1). Activates through the live 12.3/12.4
  `projectionKind: actorAggregate` path with zero `cmd/` edits.
- **`capabilityRoleIndex`** (operation-aggregate, `IntoKey: ["operationType"]`) — the FR22
  role-by-operation index (`cap.role-by-operation.<op>`), moved out of the kernel seed. The Processor
  denial-response builder reads it by string key, producer-agnostic; when rbac-domain is absent the
  key is simply not found → `rolesCarryingPermission`/`actorRoles` degrade to empty (a chosen,
  tested behavior, not a surprise).

The read side routes by actor class through the **platform entry's key derivation** (NOT a separate
dispatch entry — see registry hardening below). When rbac-domain is installed, the platform path
derives `cap.roles.<actor>` for ordinary actors and `cap.<actor>` for the kernel-seeded system
actors; when absent, it derives `cap.<actor>` for everyone (ordinary actors then deny by absence,
Contract #6 §6.8). rbac-install state is a startup probe for the `rbac-domain` package vertex
(`pkgmgr.IsPackageInstalled`), wired in `cmd/processor/main.go` and threaded through
`processor.MakePipeline` → `SelectAuthorizerOpts`.

### Primordial-composition resolution (Open Question #1 → Option (a))

The kernel admin + Loom + Weaver got their root-equivalent platform grants through the exact rbac
vocabulary Part A deletes. Naively dropping the rbac walk would brick them. **Resolution (Andrew +
Winston): Option (a) — a narrow primordial-only anchor cypher.** Core's shrunk `capability` cypher
(`internal/bootstrap/lenses.go`) anchors per-identity and projects a **hard-coded** root-grant set
(`Create/Update/TombstoneMetaVertex`, `Install/UninstallPackage`, all scope:any) for **protected**
(kernel-seeded) identities only — `WHERE identity.data.protected = true`. Ordinary actors match
nothing (zero rows → no core `cap.<actor>` doc; they read `cap.roles.<actor>`). The grant set is a
literal, not a graph walk, so `role`/`permission`/`holdsRole`/`grantedBy` truly vanish from core.
The dispatcher reads exactly ONE key by actor class — system actor → `cap.<actor>`, ordinary actor →
`cap.roles.<actor>` — and a core primordial grant never collides with a package grant on one key
(one-key-per-path, Contract #2 §2.8). The system actors keep reading `cap.<actor>` (no new
`cap.system.<actor>` key space — minimal key-shape churn).

The system-actor set the platform read routes to `cap.<actor>` is discovered at processor startup by
scanning core-kv for protected `identity` vertices (`bootstrap.SystemActorKeys`) — exactly the set
the anchor projects — keeping the processor self-contained rather than dependent on the bootstrap-file
key space being loaded into the processor process.

Non-brick is proved by `internal/processor/service_actor_auth_parity_test.go` (admin/Loom/Weaver still
authorize via `cap.<actor>`) and the migrated capability anchor e2e/conformance tests.

### Part B — service/location remnants retired (Path B)

`packages/service-location/` holds only `CONCEPT.md` (confirmed — no `package.go`). The god-cypher's
`containedIn`/`availableAt`/`unavailableAt`/`permitsOperation` branch + `serviceAccess` RETURN column
are **deleted with no replacement projection**. The service matcher kind (`matchServiceAccessKind`)
and its key derivation (left on the 12.5 seed key `cap.<actor>`, per Open Question #3 — Part B is a
deletion, not a re-key) stay registered-but-unpopulated: a service op now finds no `serviceAccess`
entry and **denies by absence** (Contract #6 §6.8). A future service package projects into them as a
pure package addition (no core edit).

**Service-fixture disposition (Path B).** The §6.10 service behaviors (multi-level containment
exclusion, transitive availability, operation override — Contract #6 §6.10 items 1/2/3) and the
Hello-Lattice service milestones are **deferred to a future service package** — they are SERVICE-package
behaviors now, not core behaviors. The capability conformance/e2e tests that previously asserted
service-access projection through the god-cypher were reconciled: the role-derived platform/roles
assertions moved to the `capabilityRoles` lens (rbac-domain), and the service-access assertions were
dropped (no producer until a service package ships). They are recorded here rather than silently
deleted; when a service package lands, its own fixture set re-establishes the §6.10 behaviors against
its disjoint key.

### Registry hardening (carried obligation from 12.5)

`buildAuthRegistry` (`internal/processor/step3_auth_matcher.go`) moved from name-only duplicate
detection to **structural predicate-overlap / coverage**. Each `authEntry` declares a coverage
descriptor (`pathKind` ∈ {platform, task, service}, a `catchAll` flag for the core platform fallback,
and an optional `scopeTag` for a narrowed platform slice). A package-derived extra is REJECTED at
registration (fail-closed) when it: reuses a core path name; claims a core specific path-kind cell
(task/service); claims the always-true platform catch-all (no scope tag); reuses a platform scope tag;
or whose predicate matches a cell it does not declare (probe-matrix cross-check against
representative authContexts — catches an always-true predicate hiding behind a narrow declaration).

The rbac contribution is **folded into the platform entry's class-aware key derivation, NOT supplied
as a separate `ExtraEntries` entry** — so it never trips the overlap guard and one-key-per-path holds
(exactly one key chosen per Authorize call). The guard governs any genuinely-separate future package
path; the legitimately-disjoint case (a platform-kind entry with a unique scope tag matching only a
narrow, non-always-true slice) is accepted. Tests: `internal/processor/step3_auth_rbac_hook_test.go`.

### Review adjudication (Winston, full 3-layer) + carried obligations

Acceptance Auditor **ACCEPT** (all ACs MET, no must-fix; the X1 overlap guard is sound against the
named root/scope:any-capture threat). Edge Case Hunter: no green-CI regression. Blind Hunter
(diff-only) raised items that reconcile against repo facts as follows — none blocks, three become
carried obligations:

- **`protected:true` is the sole marker distinguishing the admin from an ordinary identity** (the admin
  is class `identity`, *not* `identity.system.*` — only Loom/Weaver carry the system class; see
  `primordial.go:327/354/360`). So a class-based defense-in-depth guard is **not available** without
  restructuring the admin's class. The narrow anchor (`WHERE identity.data.protected = true`) and
  `SystemActorKeys` both trust this flag. Today only 3 protected identities exist and setting
  `protected` requires vertex-write permission (root-only; `rejectProtectedMutations` blocks
  update/delete of protected vertices, exempting only create). **Carried obligation / chip:** verify
  the identity create/claim path cannot let a non-root actor set `data.protected:true` on an identity —
  if it can, 12.6's anchor amplifies it into the 5 root grants. This is a write-side create-gate
  concern, separate from this read-side decomposition.
- **Stale `cap.<actor>` eviction.** The anchor has no realness filter, and a "WHERE no longer matches"
  transition (a `protected:true→false` downgrade, or an ordinary actor that pre-existed under the old
  god-cypher) yields zero rows without a delete — the stale doc lingers. Moot on the supported upgrade
  path (the 6→7 bootstrap version bump forces `down && up`, a fresh store), and a misrouted/stale read
  degrades safe when rbac is active (system actors still authorize via their operator role in
  `cap.roles.<actor>`). **Carried obligation:** an actorAggregate "anchor exists but no longer matches
  the WHERE → tombstone" eviction, if in-place upgrade (no store reset) ever becomes a supported path.
- **Overlap guard is a finite probe matrix over an opaque closure.** Sound for the threat that matters
  (the root/scope:any read carries a nil/`{}` authContext → caught by the unconditional probes), and
  `ExtraEntries` is empty today (rbac is folded into the platform key, not an extra), so there is no
  live exposure. **Carried obligation:** when packages actually supply registry entries, replace the
  opaque-closure + probe-matrix with a **declarative discriminator** (the entry declares the exact
  field+value it keys on; the dispatcher evaluates it) so overlap is decidable, and make the probe
  calls nil-safe (a non-nil-safe package predicate currently panics the probe at startup — fail-closed,
  but a startup DoS).

### Follow-ups for the planning lead (NOT done in this story)

- Mark the `lattice-architecture.md` god-cypher open item resolved.
- Record the completed contract-contribution decomposition in Contract #6 §6.1 ("decomposition
  complete").

Both are planning-artifact / frozen-contract edits owned by the planning lead — proposed, not
performed.
