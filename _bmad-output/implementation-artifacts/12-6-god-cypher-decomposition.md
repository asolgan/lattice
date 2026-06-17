# Story 12.6: God-cypher decomposition — rbac role/permission projection + retire service/location remnants (closes Epic 12)

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

> **Security-critical — the auth hot path / security plane.** Full 3-layer adversarial review (Blind Hunter / Edge Case Hunter / Acceptance Auditor) + Gate 2 (BLOCKED) + Gate 3 (DEFENDED) follow dev. They are NOT optional.
>
> **This is the CLOSING story of Epic 12.** It merges the planned Story 12.6 (rbac role/permission decomposition) and Story 12.7 (retire service/location remnants) into ONE story, taking **Path B** for 12.7 (Andrew, planning lead): the `service-location` package does **not** exist, so the service/location remnants are simply **deleted** — do NOT build a placeholder package. When this lands, the bootstrap god-cypher no longer references rbac OR service/location types, and Epic 12 is done.

## Story

As a platform developer,
I want `rbac-domain` to own its role/permission grant projection as a package lens (Part A) and the bootstrap god-cypher's service/location remnants deleted with no replacement (Part B, Path B),
so that core stops referencing the rbac AND service/location grant vocabularies, the bootstrap `capability` cypher shrinks to (at most) the primordial-identity anchor, and Epic 12's "packages own their projections" decomposition is complete.

## ⭐ Scope framing — READ THIS FIRST (the things that, if gotten wrong, cost a whole story)

This story completes the read+write decomposition the keystone stories (12.1–12.5) built the machinery for. **You are adding ZERO new core mechanism** — 12.3/12.4 gave you `projectionKind: actorAggregate` + the `OutputDescriptor` + `InstallPackage`-activated package lenses; 12.5 gave you the data-driven `authEntry` registry with the `ExtraEntries` seam. This story uses those seams to: (Part A) move rbac role/permission projection into `rbac-domain` as a package lens + a registered auth-hook; (Part B / Path B) delete the service/location MATCHes from the god-cypher with no replacement; and (cross-cutting) **harden the registry's duplicate detection** because rbac-domain is the FIRST package-derived auth-hook entry.

**Five pins (party-review + decision-record-pinned — see References):**

1. **rbac-domain is a Go package whose install is DATA.** `packages/rbac-domain/` exports `var Package = pkgmgr.Definition{...}` (currently `DDLs` + `Permissions`, `Lenses: []`). You add a `capabilityRoles` entry to `Definition.Lenses` (a `pkgmgr.LensSpec` with `ProjectionKind: "actorAggregate"` + an `Output` descriptor + cypher) — exactly the 12.4 AC5 proof-lens shape, now a PRODUCTION lens. You do **NOT** write projection Go code: the 12.3/12.4 plan compiler + driver project it. Core owns the matcher *kinds*; rbac-domain *declares* the wiring as data.

2. **The auth-hook is a `pkgmgr`-sourced `authEntry`, not package Go.** 12.5's `SelectAuthorizerOpts.ExtraEntries []authEntry` is the seam. The matcher *kind* is the EXISTING core `matchPlatformPermissionKind` (rbac grants are platform permissions). rbac-domain declares, as install-time data, the triple `(path predicate → platform matcher kind → cap.roles.<actor> key derivation)`. The wiring that turns install-time data into an `authEntry` lives in CORE (where the processor is constructed), reading what rbac-domain declared — packages cannot ship Go that the processor links. **State in Completion Notes exactly where the rbac `authEntry` is constructed and how it is gated on rbac-domain being installed.**

3. **CARRIED OBLIGATION FROM 12.5 — now load-bearing (cross-cutting AC).** 12.5 left `buildAuthRegistry` duplicate-detection as **name-only** because all extras were core-injected/trusted. rbac-domain is the **FIRST package-derived entry**, so name-only is no longer sufficient. Duplicate detection must move to **predicate-overlap / coverage**: a uniquely-named package entry whose `selects` overlaps a core path (or is always-true) must be **REJECTED at registration (fail-closed)** — never silently shadowed by precedence, never allowed to capture the platform path onto a package-controlled key. This is a SECURITY hardening, not a nicety: a broad package extra ordered before `platform` could otherwise siphon the platform read onto `cap.roles.<actor>`. (See decision record, the 12.5 bullet "Carried obligation for 12.6".)

4. **Primordial-vs-package composition is the HARDEST part — get it right (party-review #9).** The primordial admin AND the two service-actors (Loom, Weaver) get their root-equivalent platform grants **today through the EXACT rbac vocabulary this story is deleting**: `identity -[:holdsRole]-> operator role <-[:grantedBy]- permission` (`internal/bootstrap/primordial.go:581-641`). So "core projects primordial grants even when rbac-domain is absent" is NOT free — naively deleting the `holdsRole`/`grantedBy`/`role`/`permission` MATCHes from the god-cypher would strip the kernel admin + Loom + Weaver of all platform permissions and **brick the platform**. The story must define how a **core-projected primordial grant** and an **rbac-domain-projected `cap.roles.<actor>` grant** compose on the platform read path WITHOUT collision, preserving one-key-per-path. See "The primordial-composition problem" in Dev Notes — this is the single highest-stakes design decision and has an explicit open question for Winston.

5. **Path B for service/location (Andrew): DELETE, do not replace.** `packages/service-location/` holds only a `CONCEPT.md` (no package). Delete the god-cypher's `containedIn`/`availableAt`/`unavailableAt`/`permitsOperation` MATCHes with **no replacement projection authored**. The service matcher kind (`matchServiceAccessKind`) + its disjoint key derivation stay **registered-but-unpopulated** — absence = denial (§6.8) — until a real service package projects into them later (a pure package addition, no core edit). **Do NOT build a placeholder package for symmetry.** The bypass suite's service-access oracle is reconciled to Path B (service ops now DENY with the no-entry path; the §6.10 service fixtures move with whatever owns service projection — documented, not silently dropped).

**What "done" looks like.** The bootstrap `capability` cypher (`internal/bootstrap/lenses.go:74-104`) no longer references `role`/`permission` (Part A) OR `service`/`location` (Part B) types — it shrinks to (at most) the primordial-identity anchor, or retires entirely with core owning just the bucket + key conventions + the step-3 dispatcher. `rbac-domain` ships `capabilityRoles` projecting `cap.roles.<actor>` + registers its hook. The registry rejects overlapping package entries. All gates green; Gate 2 BLOCKED, Gate 3 DEFENDED; Epic 12 closes.

## Acceptance Criteria

> Backbone: `_bmad-output/planning-artifacts/epics/phase-2-epics.md` § "### Story 12.6" + "### Story 12.7" (Path B branch ONLY — Path A is explicitly dropped per Andrew). Build TO the already-amended FROZEN contracts: Contract #6 §6.1 (key spaces `cap.roles.<actor>` / `cap.svc.<actor>` + the §6.1 decomposition note), §6.4/§6.7 (platformPermissions), §6.8 (no-entry=denial + soft-tombstone), §6.10 (role-specialization + service behaviors), §6.13 (projectionKind + Output descriptor); Contract #2 §2.8 (one-key-per-path dispatcher). Frozen contracts are build-to, NEVER edited in-flight.

### Part A — rbac role/permission decomposition (was Story 12.6)

**AC-A1 — `rbac-domain` ships a `capabilityRoles` actor-aggregate lens projecting to a disjoint key.**
**Given** the plan compiler + driver (12.4) and the `pkgmgr.LensSpec` install path (12.4 closed the package-lens activation gap).
**When** `rbac-domain` adds a `capabilityRoles` lens to `packages/rbac-domain/package.go`'s `Definition.Lenses` — `ProjectionKind: "actorAggregate"`, an `Output` descriptor (`anchorType: identity`, `outputKeyPattern: "cap.roles.{actorSuffix}"`, `bodyColumns: [platformPermissions, roles]`, `emptyBehavior: delete`, `freshness: auto`), `Engine: "full"`, `Bucket: "capability"`, and a cypher that walks `identity -[:holdsRole]-> role <-[:grantedBy]- permission` to project role-derived `platformPermissions` (+ `roles`).
**Then** installing rbac-domain via the real `InstallPackage` path projects a `cap.roles.<actor>` document for every actor holding an rbac role, with the same `platformPermissions[]` shape (`{operationType, scope}`) the god-cypher's role branch produced today, and a `roles[]` list of role keys.
**And** the lens activates through the live `CoreKVSource` + the generic `projectionKind`-keyed path (the 12.4 `installActorAggregate`) with ZERO `cmd/` or `internal/refractor/capabilityenv/` edits — it is a pure package addition.
**And** the `cap.roles.<actor>` key is GUARDED (auth-plane: target bucket `capability-kv`, so the 12.4 `plan.AuthPlane || RequiresGuardedTombstone()` predicate resolves guarded — the 12.1a `projectionSeq` write-ordering guard + soft-tombstone apply, same as `cap.<actor>`).

**AC-A2 — `rbac-domain` registers its auth-hook via the 12.5 dispatcher (read side).**
**Given** the 12.5 `SelectAuthorizerOpts.ExtraEntries []authEntry` seam (constructor-injected, the adjudicated Option 1).
**When** rbac-domain is installed, an `authEntry` is constructed (in core, from rbac-domain's install-time declaration — see pin #2) binding: a path predicate that selects the rbac/platform read for an ORDINARY actor → the EXISTING `matchPlatformPermissionKind` matcher kind → a `cap.roles.<actor>` key derivation; `threadsDocOnDenial: true` (FR22 `actorRoles` source moves with it); the same `absentKeyCode`/`absentKeyReason` semantics the platform path produces today.
**Then** the platform-path step-3 read for an ordinary actor targets `cap.roles.<actor>` via the registered hook, exactly one KV GET fires (one-key-per-path preserved), and the FR22 denial `actorRoles` is sourced from the `cap.roles.<actor>` doc threaded onto the denial (no second read on the hot path).
**And** when rbac-domain is NOT installed, NO rbac `authEntry` is registered — and the ordinary-actor platform path degrades to the core `platform` catch-all reading `cap.<actor>` (which, post-Part-A, no longer carries role-derived permissions → denial, §6.8). State this degradation explicitly in Completion Notes (it is the chosen behavior, not a surprise).

**AC-A3 — The god-cypher DROPS its rbac MATCHes (core no longer references rbac types).**
**Given** rbac-domain now owns role/permission projection (AC-A1) and the read side routes to `cap.roles.<actor>` (AC-A2).
**When** the bootstrap `capability` cypher (`internal/bootstrap/lenses.go`) is edited.
**Then** its `OPTIONAL MATCH (identity)-[:holdsRole]->(role:role)<-[:grantedBy]-(perm:permission)` branch and the `platformPermissions` + `roles` RETURN columns it feeds are **DROPPED** (subject to the primordial-anchor resolution in AC-A4) — core's bootstrap cypher no longer names `role`, `permission`, `holdsRole`, or `grantedBy`. The dependency direction flips package→core, mirroring the 7.1 `task`-type extraction.
**And** `capabilityRoleIndex` ownership is resolved (AC-A5).

**AC-A4 — Primordial-vs-package composition is DEFINED and preserves the platform (party-review #9).**
**Given** the primordial admin + Loom + Weaver get root-equivalent platform grants TODAY through the rbac vocabulary (`holdsRole → operator role <-grantedBy- permission`, `internal/bootstrap/primordial.go:581-641`) — the SAME MATCHes AC-A3 drops.
**When** the decomposition lands.
**Then** the primordial/system identities **retain** their root-equivalent platform permissions, and an ordinary actor reads its role-derived grants from `cap.roles.<actor>`, with the dispatcher reading **exactly ONE key by actor class** (one-key-per-path, §2.8): primordial/system actor class → a core-owned key (e.g. `cap.<actor>` projected by the shrunk primordial-anchor branch); ordinary actor → `cap.roles.<actor>` (rbac-domain). A core-projected primordial grant and an rbac-domain-projected grant NEVER collide on a single key.
**And** the resolution is implemented per Winston's adjudication of Open Question #1 (the primordial-anchor approach — see "The primordial-composition problem" in Dev Notes; the leading option: core's shrunk `capability` cypher keeps a narrow primordial-only projection that does NOT reference the rbac vocabulary for ordinary actors, OR rbac-domain projects the primordial identities too while core retains a minimal hard-coded root-grant for the system actors). The dev agent MUST NOT silently brick the kernel admin / Loom / Weaver — the bootstrap-quiescent E2E and the service-actor auth-parity test (`internal/processor/service_actor_auth_parity_test.go`) prove the system actors still authorize.
**And** the role-manipulation attack vector (Gate 3) still DEFENDED: a non-primordial actor cannot acquire root by writing/forging an rbac grant, and the primordial path cannot be captured onto a package-controlled key (this ties to the AC-X registry hardening).

**AC-A5 — `capabilityRoleIndex` ownership is resolved + its absence-degradation stated.**
**Given** `capabilityRoleIndex` (the `cap.role-by-operation.<op>` operation-aggregate index — `internal/bootstrap/lenses.go:136`) feeds FR22 `rolesCarryingPermission` on the denial path and is built from `role <-grantedBy- permission` (the rbac vocabulary).
**When** the decomposition lands.
**Then** `capabilityRoleIndex` ownership moves to / is consistently owned by `rbac-domain` (it is an rbac-vocabulary projection) — declared as a second `pkgmgr.LensSpec` on `rbac-domain` (NOT actor-aggregate; the 12.4 `Into.Key==["operationType"]` operation-aggregate path handles it), and its bootstrap definition (`CapabilityRoleIndexLensDefinition`) is removed from core's primordial seed.
**And** the degradation when rbac-domain is absent is STATED and tested: FR22 `rolesCarryingPermission` (and `actorRoles` where it derives from the index) is **empty** — a chosen behavior, not a surprise. (If keeping `capabilityRoleIndex` in core is materially simpler AND core retains no rbac-type reference elsewhere, the dev may flag that as an alternative in Open Questions — but the AC default is: it moves to rbac-domain, since it is pure rbac vocabulary.)

**AC-A6 — Behavior preserved (fixtures migrate, outcomes hold).**
**Given** the role/permission projection moved keys.
**When** the gates run.
**Then** the bypass suite's role-manipulation attack vector (Gate 3, `internal/bypass/capadv_*`), the §6.10 role-specialization behavior (item 4: role-derived platform permissions appear in the projection independent of service access), and the §6.2 conformance test all pass — with fixtures/oracles **migrated** to the `cap.roles.<actor>` disjoint key where the projection moved, but the asserted auth OUTCOMES holding byte-for-byte.

### Part B — Retire service/location remnants, Path B (was Story 12.7)

> **Path B is fixed (Andrew). Path A (build `capabilityServiceAccess` in `service-location`) is DROPPED.** Confirm at story time that `packages/service-location/` still holds only `CONCEPT.md` (no `package.go`); if a real `service-location` package has somehow landed, STOP and raise it to Winston rather than silently switching paths.

**AC-B1 — The god-cypher DROPS its service/location MATCHes with NO replacement.**
**Given** Path B (no `service-location` package).
**When** the bootstrap `capability` cypher is edited.
**Then** its `OPTIONAL MATCH (identity)-[:containedIn*0..]->(loc)-[:availableAt]->(svc)` branch, the `WHERE NOT ... unavailableAt ...` exclusion, the `serviceAccess` RETURN column, and the inline `permitsOperation` sub-pattern are **DELETED** — with **no replacement projection authored**. Core's bootstrap cypher no longer names `containedIn`, `availableAt`, `unavailableAt`, `permitsOperation`, `location`, or `service` types.

**AC-B2 — The service matcher kind + `cap.svc` key stay registered-but-unpopulated (absence = denial).**
**Given** the service path's matcher kind (`matchServiceAccessKind`) + its disjoint key derivation remain in core (12.5 seed entries).
**When** a service op (`authContext.Service != ""`) is authorized after Part B.
**Then** the service path reads its disjoint key, finds NO entry (no producer projects it yet), and DENIES with the no-entry path (§6.8) — `AuthContextMismatch`/`AuthDenied` per the existing service-path codes. The matcher kind + key space are registered-but-empty until a real service package projects into them later (a pure package addition, no core edit — the 12.3/12.4/12.5 machinery).
**And** the `cap.svc.<actor>` key space is confirmed against Contract #6 §6.1 (registered-but-may-be-empty). State in Completion Notes whether the service path now derives `cap.svc.<actor>` (the §6.1 decomposition target) or still derives `cap.<actor>` (the 12.5 seed) — and if it should move to `cap.svc.<actor>` to match §6.1, treat that key-derivation change as a CI-only-check trigger (see Dev Notes → "CI-only checks").

**AC-B3 — The bypass service-access oracle is reconciled to Path B (documented, not silently dropped).**
**Given** the bypass suite + Hello Lattice / §6.10 service fixtures asserted service access via the god-cypher's service branch.
**When** Part B removes that projection.
**Then** the service-access oracle is reconciled: a service op now DENIES with the no-entry path until a service package lands. The §6.10 service fixtures (multi-level containment exclusion, transitive availability, operation override — items 1/2/3) and the Hello Lattice service milestones move WITH whatever owns service projection (i.e. they are relocated to a future service-package fixture set or explicitly marked deferred-to-service-package) — **documented in Completion Notes + a `docs/decisions/` note, not silently deleted.**
**And** Gate 2 (BLOCKED) + Gate 3 (DEFENDED) still pass with the service path denying-by-absence.

### Cross-cutting — registry hardening (carried obligation from 12.5)

**AC-X1 — `buildAuthRegistry` duplicate detection moves from name-only to predicate-overlap / coverage (fail-closed).**
**Given** 12.5's `buildAuthRegistry` (`internal/processor/step3_auth_matcher.go:101`) currently rejects only duplicate path *names* — sufficient when all extras were core-injected/trusted, but NOT when a package supplies an entry (rbac-domain is the first).
**When** the registry is built with package-derived extras.
**Then** a package entry is REJECTED at registration (fail-closed — `buildAuthRegistry` returns an error, so `NewCapabilityAuthorizer`/`SelectAuthorizerArgs` fail closed) if its `selects` predicate **overlaps** a core path (task / service / platform) OR is **always-true** (would capture the platform catch-all path onto a package-controlled key). Name-collision rejection is RETAINED (a package entry may not reuse a core path name). The rbac-domain entry from AC-A2 must pass this guard (it selects only the ordinary-actor platform read, disjoint from the core task/service paths and NOT always-true).
**And** the overlap check is structural, not best-effort: state in Completion Notes the chosen overlap model (e.g. each entry declares a coverage descriptor — actor-class + path-kind — and overlap is a set-intersection test; OR a representative-authContext probe matrix). The dispatcher NEVER resolves an ambiguous path by fanning into N reads.

**AC-X2 — A test proves the hardening.**
**Given** AC-X1.
**When** a package entry whose predicate overlaps the platform path (or is always-true) is registered.
**Then** a unit test asserts `buildAuthRegistry` / `NewCapabilityAuthorizer` returns an error (nil authorizer) — fail-closed, never silently shadowed by precedence.
**And** a second test asserts the legitimate rbac-domain-shaped entry (new disjoint path, not overlapping a core path, not always-true) is ACCEPTED and routes correctly.

### Cross-cutting — gates + end state

**AC-G1 — End state: core references neither rbac nor service/location types.**
**Then** the bootstrap `capability` cypher shrinks to (at most) the primordial-identity anchor (or retires entirely), and a `grep` over `internal/bootstrap/lenses.go` + the god-cypher confirms it no longer names `role`/`permission`/`holdsRole`/`grantedBy` (Part A) NOR `containedIn`/`availableAt`/`unavailableAt`/`permitsOperation`/`location`/`service` (Part B). Core owns the Capability KV bucket + key conventions + the step-3 dispatcher only.
**And** the follow-ups are FLAGGED for Winston/Andrew (do NOT edit these in-flight): (1) mark the `lattice-architecture.md` god-cypher open item resolved; (2) record the completed contract-contribution decomposition in Contract #6 §6.1. These are planning-artifact / frozen-contract edits owned by the planning lead — propose, do not perform.

**AC-G2 — Verification gates pass (security plane).**
**Then** all gates pass, run INLINE in CI's exact order: `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`, the package-install verify scripts (incl. `make verify-package-rbac` — its asserted shape CHANGES when `capabilityRoles`[+`capabilityRoleIndex`] lenses are added; see CI-only-checks), `make test-bypass` (Gate 2 — all BLOCKED), `make test-capability-adversarial` (Gate 3 — all DEFENDED), the §6.2/§6.10 conformance + role-manipulation-attack-vector bypass tests, and `go test ./...` (the FULL tree — CI runs the whole repo, not one package; the story's own packages must be deterministically green).

## Tasks / Subtasks

- [x] **Task 1 — Add the `capabilityRoles` lens to rbac-domain (Part A write side). (AC: A1)**
  - [x] Add a `Lenses()` source (mirror `DDLs()` / `Permissions()`) returning a `pkgmgr.LensSpec` for `capabilityRoles`: `CanonicalName: "capabilityRoles"`, `Class: "meta.lens"`, `Engine: "full"`, `Adapter: "nats-kv"`, `Bucket: "capability"`, `ProjectionKind: "actorAggregate"`, `Output: &pkgmgr.OutputDescriptorSpec{AnchorType:"identity", OutputKeyPattern:"cap.roles.{actorSuffix}", BodyColumns:["platformPermissions","roles"], EmptyBehavior:"delete", Freshness:"auto"}`, and a `Spec` cypher walking `MATCH (identity:identity {key:$actorKey}) OPTIONAL MATCH (identity)-[:holdsRole]->(role:role)<-[:grantedBy]-(perm:permission) RETURN identity.key AS actorKey, collect(DISTINCT {operationType: perm.data.operationType, scope: perm.data.scope}) AS platformPermissions, collect(DISTINCT role.key) AS roles`. Wire `Lenses: Lenses()` into `Package` in `package.go`.
  - [x] Update `packages/rbac-domain/manifest.yaml` `lenses: []` → declare the `capabilityRoles` (+ `capabilityRoleIndex` if AC-A5 moves it here) lens(es) — the manifest cross-check (`internal/pkgmgr/manifest.go:97-114`) asserts manifest lens count + canonicalName matches `Definition.Lenses`.
  - [x] Confirm the `cap.roles.<actor>` key resolves GUARDED via the 12.4 plan predicate (auth-plane bucket) — add/extend a test asserting `projectionSeq` + soft-tombstone apply to `cap.roles.<actor>` like `cap.<actor>`.

- [x] **Task 2 — Register the rbac auth-hook (Part A read side). (AC: A2, A4)**
  - [x] In core (where the processor's `CapabilityAuthorizer` is constructed — `internal/processor/commit_path.go:640` `MakePipeline` → `SelectAuthorizerArgs`), construct the rbac `authEntry` from rbac-domain's install-time declaration and pass it via `SelectAuthorizerOpts.ExtraEntries`. Gate it on rbac-domain being installed (state HOW you detect installed: package vertex presence, a lens-presence probe, or a config flag — pin #2). The matcher kind is the EXISTING `matchPlatformPermissionKind`; the key derivation is `cap.roles.<actor>`; `threadsDocOnDenial: true`.
  - [x] Ensure the rbac entry selects ONLY the ordinary-actor platform read (NOT the task path, NOT the service path, NOT always-true) — this is what lets it pass the AC-X1 overlap guard and preserves one-key-per-path.
  - [x] Confirm exactly ONE KV GET fires for an ordinary-actor platform authorize (reuse the single-GET test pattern from `step3_auth_capability_test.go`).

- [x] **Task 3 — The primordial-composition resolution (Part A). (AC: A4)**
  - [x] Implement Winston's adjudicated answer to Open Question #1. Do NOT proceed past this task on best-judgment alone if the kernel-admin/Loom/Weaver authorization would break — this is the brick-the-platform risk. Default (pending adjudication): core's shrunk `capability` cypher keeps a NARROW primordial-only platform-grant projection anchored on the system identities, NOT referencing the rbac vocabulary for ordinary actors; the dispatcher reads `cap.<actor>` for the system actor class and `cap.roles.<actor>` for ordinary actors.
  - [x] Prove the system actors still authorize: `internal/processor/service_actor_auth_parity_test.go` + the bootstrap-quiescent / Hello-Lattice E2E pass with admin + Loom + Weaver retaining their `CreateMetaVertex`/`UpdateMetaVertex`/`TombstoneMetaVertex`/`InstallPackage`/`UninstallPackage` platform grants.

- [x] **Task 4 — Drop the god-cypher rbac MATCHes + resolve `capabilityRoleIndex`. (AC: A3, A5, G1)**
  - [x] Edit `internal/bootstrap/lenses.go` `CapabilityLensDefinition().CypherRule`: remove the `holdsRole`/`grantedBy`/`role`/`permission` branch + the `platformPermissions`/`roles` RETURN columns (subject to Task 3's primordial anchor). Update the Output descriptor `bodyColumns` accordingly. Keep the §6.2-required field shape the Processor's `CapabilityDoc` parser tolerates (verify `internal/processor/capability_doc.go`).
  - [x] Move `capabilityRoleIndex` to rbac-domain (AC-A5): add it as a `pkgmgr.LensSpec` (operation-aggregate, `Into.Key==["operationType"]` path), remove `CapabilityRoleIndexLensDefinition` from the primordial seed (`internal/bootstrap/primordial.go:495-503` + `lenses.go:136`). Confirm the FR22 denial path degrades to empty `rolesCarryingPermission` when rbac-domain is absent, and a test asserts it.

- [x] **Task 5 — Delete service/location remnants, Path B (Part B). (AC: B1, B2, B3)**
  - [x] Confirm `packages/service-location/` holds ONLY `CONCEPT.md` (no `package.go`); if a real package exists, STOP and raise to Winston.
  - [x] Edit `internal/bootstrap/lenses.go` `CypherRule`: delete the `containedIn`/`availableAt`/`unavailableAt`/`permitsOperation` branch + the `serviceAccess` RETURN column. No replacement.
  - [x] Confirm the service matcher kind + its disjoint key derivation stay registered (12.5 seed) and now deny-by-absence. Decide + state whether the service key derivation moves to `cap.svc.<actor>` (matches §6.1) — if so, treat as a CI-only-check trigger.
  - [x] Reconcile the bypass service-access oracle to Path B; relocate/defer the §6.10 + Hello Lattice service fixtures with a `docs/decisions/` note (do not silently drop).

- [x] **Task 6 — Harden `buildAuthRegistry` (cross-cutting). (AC: X1, X2)**
  - [x] In `internal/processor/step3_auth_matcher.go`, extend `buildAuthRegistry` so package-derived extras are rejected when their `selects` overlaps a core path (task/service/platform) or is always-true. Retain name-collision rejection + the missing-predicate/kind/key guard. State the overlap model in Completion Notes (e.g. an explicit coverage descriptor per entry, or a representative-authContext probe matrix — structural, not best-effort).
  - [x] Tests: a package entry overlapping the platform path (and one always-true) → `buildAuthRegistry`/`NewCapabilityAuthorizer` returns error (nil authorizer); the legitimate rbac-shaped disjoint entry → accepted + routes.

- [x] **Task 7 — Code conventions sweep (house rules). (AC: all)**
  - [x] NO history/changelog comments (`// Story 12.6…`, `// was…`, `// previously…`, `// dropped the rbac MATCHes…`, `// moved from core…`). Comments describe what the code does NOW for a reader who has no idea a change happened.
  - [x] Key-shape conventions (Contract #1): `cap.roles.<actor-suffix>` / `cap.svc.<actor-suffix>` are Capability-KV keys (NOT `vtx.*`); `{actorSuffix}` strips `vtx.` (→ `cap.roles.identity.<id>`). Link-naming reads as a sentence; do not invent new shapes.
  - [x] New docs → `/docs` (e.g. extend `docs/decisions/projection-plane-decomposition.md` with the primordial-anchor resolution + the Path-B service-fixture disposition), NOT `_bmad-output/`.

- [x] **Task 8 — Verification gates (security plane), run INLINE in CI's exact order. (AC: G2)**
  - [x] `go build ./...`; `make vet`; `golangci-lint run ./...`; `make verify-kernel`.
  - [x] **CI-only checks (the lesson that bit 12.4 — DO NOT SKIP):** `grep scripts/verify-*.go` for the affected shapes. `make verify-package-rbac` (`scripts/verify-package-rbac.go`) asserts the rbac-domain install shape (~34 OK lines: 1 DDL, 10 perms, 10 grantedBy links, package vertex + manifest). Adding `capabilityRoles`(+`capabilityRoleIndex`) lens(es) changes the install shape → UPDATE the verify script's expected lens/aspect counts. Also check `verify-package-identity-hygiene.go` (which 12.4 had to fix to read lens cypher from `spec.cypherRule`) and `verify-kernel.go` (asserts the capability lens's aspect set — Part A/B shrink the cypher + change `bodyColumns`).
  - [x] `make test-bypass` (Gate 2 — all BLOCKED); `make test-capability-adversarial` (Gate 3 — all DEFENDED, incl. the role-manipulation vector).
  - [x] `go test ./...` (FULL tree). Note: an unrelated embedded-NATS JetStream timing flake (e.g. `internal/loom`, `processor/outbox`, a `pkgmgr` race) may surface under fully-parallel `go test ./...` — but the story's OWN packages (`internal/bootstrap`, `internal/processor`, `internal/refractor`, `packages/rbac-domain`, `internal/bypass`) must be deterministically green; re-run isolated to confirm any flake is pre-existing.

## Dev Notes

### The primordial-composition problem (pin #4 / AC-A4) — read this carefully, it is the crux

The primordial admin (`vtx.identity.<BootstrapIdentityID>`, class `identity`) and the two service-actors Loom (`identity.system.loom`) + Weaver (`identity.system.weaver`) get their **entire** root-equivalent platform-permission set through the rbac vocabulary, established at `internal/bootstrap/primordial.go`:

- entry 7: the `operator` role vertex (`vtx.role.<RoleOperatorID>`, canonicalName `operator`).
- entries 8–9: 5 permission vertices (`CreateMetaVertex`/`UpdateMetaVertex`/`TombstoneMetaVertex`/`InstallPackage`/`UninstallPackage`, all `scope:"any"`) each linked to operator via a `grantedBy` link.
- entries 10/10a: `admin -[:holdsRole]-> operator`, `loom -[:holdsRole]-> operator`, `weaver -[:holdsRole]-> operator`.

The god-cypher's `holdsRole → role <-grantedBy- permission` branch (the one AC-A3 deletes) is **exactly** what projects these into `cap.identity.<id>.platformPermissions[]`. **Delete it naively and the kernel admin + Loom + Weaver lose all platform permissions → the platform cannot install packages, mutate meta-vertices, or run convergence → bricked.** This is the highest-stakes trap in the story.

The FROZEN Contract #6 §6.1 decomposition note (lines 49–67) pre-resolves the END STATE: "the bootstrap `capability` cypher shrinks to the **primordial-identity anchor** (root-equivalent platform grants core must project even when no RBAC package is installed) — or retires entirely." So core KEEPS a primordial-only platform-grant projection. The open design question (Winston's call, OQ#1) is HOW to express that anchor WITHOUT re-importing the rbac vocabulary into core for ordinary actors:

- **Option (a) — narrow primordial-anchor cypher (leading).** Core's `capability` cypher keeps a projection restricted to the system identities (anchor on the `identity.system.*` class for Loom/Weaver + the protected primordial admin identity), emitting their known root grants. It does this WITHOUT a general `holdsRole/role/permission` walk over arbitrary actors — but it may still need SOME way to know the operator's permission set. Cleanest: hard-code the system actors' root-grant set in core (they ARE core — protected, kernel-seeded, fixed) rather than deriving it through rbac links. Then `role`/`permission`/`holdsRole`/`grantedBy` truly vanish from core.
- **Option (b) — rbac-domain projects the primordial identities too.** rbac-domain's `capabilityRoles` lens projects `cap.roles.<actor>` for the system actors as well (they hold the operator role). Core retains a minimal hard-coded root-grant ONLY as a fallback so the kernel is not bricked when rbac-domain is uninstalled. The dispatcher still reads one key by actor class.

Either way the invariant AC-A4 enforces: **the dispatcher reads exactly ONE key by actor class** — system actor → core-owned key; ordinary actor → `cap.roles.<actor>` — and a core primordial grant never collides with a package grant on one key. **Do not pick silently — this is Open Question #1; implement Winston's adjudication.** Prove non-brick via `service_actor_auth_parity_test.go` + the bootstrap-quiescent E2E.

### How packages ship lenses (the 12.4 seam you are reusing)

`pkgmgr.Definition.Lenses []LensSpec` (`internal/pkgmgr/definition.go:38,116`) carries `ProjectionKind` + `Output` (added in 12.4, subtask 1.1). `internal/pkgmgr/build.go` emits the lens meta-vertex + the full `LensSpec` body as the `spec` aspect (12.4 fixed this so `corekv_source` can read `cypherRule`/`projectionKind`/`output`). The 12.4 AC5 proof lens (`internal/refractor/refractor_package_actoraggregate_proof_e2e_test.go`, the throwaway `proofRoster`) is your TEMPLATE — `capabilityRoles` is that exact shape, promoted to a production lens in `rbac-domain`. The live `installActorAggregate` in `cmd/refractor/main.go` routes it via `projectionKind` with zero `cmd/` edits.

### The 12.5 dispatcher seam you are reusing (read side)

`internal/processor/step3_auth_matcher.go` holds `authEntry`, `buildAuthRegistry`, the three seed matcher kinds, and `seedSpecificEntries`/`seedPlatformEntry`. `internal/processor/step3_auth_capability.go` holds `Authorize` (the generic dispatcher: select entry before read → one GET → matcher kind) + `selectEntry`. The seam is `NewCapabilityAuthorizer(..., extraEntries ...authEntry)` ← `SelectAuthorizerOpts.ExtraEntries` ← `SelectAuthorizerArgs`. The registry assembles `[task, service, …extras, platform]`; platform's predicate is always-true and MUST stay last. Your rbac entry sits in `…extras` and must select ONLY the ordinary-actor platform read (so platform's catch-all still handles system actors / no-rbac degradation). **`matchPlatformPermissionKind` is reused unchanged** — rbac grants are platform permissions; you change the KEY the platform-class read targets for ordinary actors, not the matching logic.

### Registry hardening (pin #3 / AC-X) — why name-only is now unsafe

12.5's `buildAuthRegistry` (`step3_auth_matcher.go:101-123`) rejects duplicate path *names* and missing predicate/kind/key. That was safe because extras were core-injected. rbac-domain is the first package-derived entry, and a package entry whose `selects` is broad (or always-true) ordered BEFORE the `platform` catch-all could siphon the platform read onto `cap.roles.<actor>` — a privilege-routing bug. So the guard must reject **predicate overlap / coverage**, not just name reuse. The structural challenge: `selects` is an opaque `func(*AuthContext) bool`, so you cannot intersect two arbitrary closures. Practical models (pick one, state it): (1) attach a small declarative coverage descriptor to each `authEntry` (actor-class + path-kind) and intersect those; (2) probe each entry's predicate against a representative authContext matrix (task-set / service-set / platform-only / system-actor) and reject an extra that matches a cell a core path already claims, or matches all cells. The rbac entry's coverage is "ordinary-actor platform read only" → disjoint from task/service and not always-true → accepted.

### Path B service fixtures (AC-B3) — what moves where

Today the bypass + §6.10 + Hello Lattice fixtures assert service access through the god-cypher's service branch. After Path B that projection is gone, so a service op denies-by-absence. The §6.10 behaviors (multi-level containment exclusion / transitive availability / operation override — Contract #6 §6.10 items 1/2/3) are SERVICE-package behaviors now — relocate those fixtures to a clearly-labeled "deferred until a service package ships service projection" set (a `docs/decisions/` note + a skipped/relocated test), NOT a silent delete. The Hello Lattice service milestones similarly move with whatever owns service projection. The bypass service-access oracle now expects DENY-by-no-entry for service ops.

### CI-only checks (the lesson that bit 12.4 — and 12.4's follow-up)

A spec/key-shape change breaks CI-only verify scripts even when `go test` passes. Story 12.4 failed `verify-package-identity-hygiene` and needed a follow-up (commit `d2c1506`) once the lens spec aspect carried the full body. For THIS story:
- `scripts/verify-package-rbac.go` — asserts the rbac-domain install shape (~34 OK lines). Adding `capabilityRoles`(+`capabilityRoleIndex`) lens(es) ADDS lens meta-vertices + aspects → the script's expected counts MUST be updated, or `make verify-package-rbac` fails in CI.
- `scripts/verify-kernel.go` — asserts the bootstrap `capability` lens aspect set; Part A/B shrink the cypher + change `bodyColumns` + (if `capabilityRoleIndex` leaves core) remove the role-index lens from the kernel seed → update the kernel assertions.
- `scripts/verify-package-identity-hygiene.go` — reads lens cypher from `spec.cypherRule`; verify the new rbac lens passes its hygiene checks.
Reproduce CI in its EXACT order (build → vet → lint → verify-kernel → package-install verifies → Gate 2 → Gate 3 → full `go test`) before declaring done.

### House rules (CLAUDE.md) — non-negotiable

- **NO history/changelog comments in code.** Never `// Story 12.6…`, `// was the rbac branch`, `// previously projected here`, `// moved to rbac-domain`. git blame is the record. Comments describe what the code does NOW.
- **Key-shape + link-naming conventions (Contract #1).** `cap.roles.<actor-suffix>` / `cap.svc.<actor-suffix>` Capability-KV keys; aspects 4-segment `vtx.<type>.<id>.<localName>`; links 6-segment `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>` reading as a sentence.
- **Frozen contracts** (Contract #2 §2.8, Contract #6 §6.1/§6.4/§6.7/§6.8/§6.10/§6.13) are build-to, NEVER edited in-flight. The §6.1 "records the completed decomposition" amendment + the `lattice-architecture.md` open-item resolution are PROPOSED to the planning lead (AC-G1) — flag as follow-ups, do not perform.
- **Sub-agents never commit/push/branch** — leave the change in the working tree for Winston to adjudicate.

### Source tree components to touch

- `packages/rbac-domain/package.go`, `packages/rbac-domain/lenses.go` (NEW — mirror `permissions.go`/`ddls.go`), `packages/rbac-domain/manifest.yaml` — declare `capabilityRoles` (+ `capabilityRoleIndex` per AC-A5).
- `internal/bootstrap/lenses.go` — shrink the `capability` cypher (drop rbac + service/location MATCHes; keep primordial anchor per Task 3); remove `CapabilityRoleIndexLensDefinition` if AC-A5 moves it.
- `internal/bootstrap/primordial.go` — remove the `capabilityRoleIndex` lens seed (entry 6) if it moves; the primordial role/permission/holdsRole topology (entries 7–10a) STAYS (it is the graph material rbac-domain's lens reads + the kernel's root grant) — do NOT delete it.
- `internal/processor/step3_auth_matcher.go` — harden `buildAuthRegistry` (AC-X).
- `internal/processor/commit_path.go` (or wherever the processor authorizer is constructed) — build + inject the rbac `authEntry` via `ExtraEntries`, gated on rbac-domain installed.
- `internal/processor/step3_auth_capability_test.go`, `service_actor_auth_parity_test.go` — single-GET + system-actor parity.
- `internal/bypass/capadv_*`, `internal/bypass/bypass_*` — fixtures migrate to `cap.roles.<actor>` (Part A) + service deny-by-absence (Part B); outcomes hold.
- `scripts/verify-package-rbac.go`, `scripts/verify-kernel.go` — update expected shapes (CI-only).
- `docs/decisions/projection-plane-decomposition.md` — append the primordial-anchor resolution + Path-B service-fixture disposition (new docs → /docs).

### Testing standards summary

- Unit: `step3_auth_capability_test.go` (fake reader, fixed clock) — rbac-path single-GET, system-actor class routing, registry-overlap rejection. `internal/refractor/projection/` driver tests for `cap.roles.<actor>` shape + guard.
- Conformance: §6.2 doc shape + §6.10 role-specialization (item 4) on the `cap.roles.<actor>` projection.
- E2E: real `InstallPackage` projecting `cap.roles.<actor>` + reproject on `holdsRole`/`grantedBy` CDC; system-actor parity (admin/Loom/Weaver still authorize); FR22 `rolesCarryingPermission` empty when rbac-domain absent.
- Gate 2 (`make test-bypass`, all BLOCKED) + Gate 3 (`make test-capability-adversarial`, all DEFENDED incl. role-manipulation vector) require a running Docker stack (`make up`).
- Run gates INLINE; if a gate appears to hang past ~10 min, kill it and report — never background a long-running command and wait.

### Project Structure Notes

- `rbac-domain` is a Go package that exports `pkgmgr.Definition` *data*; you add a lens *declaration*, not projection logic — the projection is the 12.3/12.4 plan-driven path. This is the contract-contribution model (Contract #6 §6.1): core owns the bucket + reader; the package owns its grant projection + auth-hook wiring (declared as data, constructed in core).
- The rbac `authEntry` construction lives in CORE (the processor wiring) reading rbac-domain's install-time declaration — packages cannot ship Go the processor links. This mirrors the 12.5 `ExtraEntries` seam exactly; do not invent a plugin mechanism.
- Verify import direction: nothing under `internal/processor` should import `packages/rbac-domain`. The processor reads the disjoint key by string; the binding is data.

### References

- [Source: _bmad-output/planning-artifacts/epics/phase-2-epics.md#Story 12.6] + [#Story 12.7] — AC backbone (12.7 = Path B branch only; Path A dropped per Andrew).
- [Source: docs/decisions/projection-plane-decomposition.md#D-PROJECTION + D-CONSUMER] — 12.5→12.7 sequencing; the 12.5 bullet "Carried obligation for 12.6" (predicate-overlap hardening, pin #3); party-review #9 (primordial composition), #12 (fixtures migrate), #13 (12.7 two-path → Path B committed).
- [Source: docs/contracts/06-capability-kv.md#6.1] — `cap.roles.<actor>` / `cap.svc.<actor>` key spaces + the decomposition note (the END STATE + primordial-anchor resolution, lines 49–67); §6.4/§6.7 platformPermissions; §6.8 no-entry=denial + soft-tombstone; §6.10 role-specialization (item 4) + service behaviors (items 1/2/3); §6.13 projectionKind + Output descriptor.
- [Source: docs/contracts/02-operation-envelope.md#2.8] — one-key-per-path dispatcher, precedence, forgery resistance (already amended/ratified, build-to).
- [Source: internal/bootstrap/lenses.go:43-159] — the god-cypher (`CapabilityLensDefinition`) being decomposed + `CapabilityRoleIndexLensDefinition`.
- [Source: internal/bootstrap/primordial.go:325-643] — primordial admin + Loom + Weaver identities (entries 2/2a) and the operator-role / permission / grantedBy / holdsRole topology (entries 7–10a) — the rbac vocabulary the primordial grants flow through (pin #4).
- [Source: internal/processor/step3_auth_matcher.go:53-142] — `authEntry`, `buildAuthRegistry` (name-only dedup to harden), the three seed matcher kinds.
- [Source: internal/processor/step3_auth_capability.go:134-252] — the generic dispatcher (`Authorize`, `selectEntry`) + key derivations.
- [Source: internal/processor/step3_auth.go:124-169] — `SelectAuthorizerOpts.ExtraEntries` seam + `SelectAuthorizerArgs`.
- [Source: internal/processor/commit_path.go:628-666] — `MakePipeline` (where the authorizer is constructed — rbac `authEntry` injection point).
- [Source: packages/rbac-domain/{package.go,permissions.go,ddls.go,manifest.yaml}] — the package shape to extend (`Lenses: []` → add `capabilityRoles`).
- [Source: internal/pkgmgr/definition.go:18-160] — `Definition.Lenses` + `LensSpec` (+ `OutputDescriptorSpec`); manifest.go:97-114 cross-check.
- [Source: scripts/verify-package-rbac.go] — the CI-only install-shape assertion that CHANGES (~34 OK lines → +lens(es)).
- [Source: _bmad-output/implementation-artifacts/12-5-auth-hook-dispatcher.md] — the dispatcher seam (DONE/review); `ExtraEntries` adjudication (Option 1).
- [Source: _bmad-output/implementation-artifacts/12-4-migrate-builtins-delete-switch.md] — the package-lens activation pattern + AC5 proof lens (`proofRoster`) = the `capabilityRoles` template; the `Into.Key==["operationType"]` operation-aggregate path for `capabilityRoleIndex`.
- [Source: packages/service-location/CONCEPT.md] — the concept (Path A would have built this; Path B does NOT — confirm it is still concept-only).

## Open Questions for Winston

> **RESOLVED by Andrew + Winston before dev; implemented exactly as adjudicated.**

1. **Primordial-composition resolution (AC-A4 / Task 3).** **RESOLVED = Option (a)** — narrow
   primordial-anchor cypher. Core's shrunk `capability` cypher (`internal/bootstrap/lenses.go`)
   projects a **hard-coded** root-grant set (`Create/Update/TombstoneMetaVertex`,
   `Install/UninstallPackage`, all scope:any) for **protected** identities only
   (`WHERE identity.data.protected = true`); ordinary actors match nothing → no core `cap.<actor>`
   doc. `role`/`permission`/`holdsRole`/`grantedBy` truly vanish from core's cypher. System actors
   keep reading `cap.<actor>` (NO `cap.system.<actor>` key space — minimal churn). The read-side
   class-aware branch is a SINGLE platform entry whose key derivation routes system → `cap.<actor>`,
   ordinary → `cap.roles.<actor>`. Non-brick proved live: `make up` readiness gate satisfied
   `capProjections=3` and the admin `cap.identity.<id>` carries the 5 root grants.

2. **`capabilityRoleIndex` ownership (AC-A5).** **RESOLVED = move to rbac-domain.** Declared as the
   second `rbac-domain` lens (operation-aggregate, `IntoKey: ["operationType"]`); removed from the
   kernel seed (`CapabilityRoleIndexLensDefinition` deleted, nanoid registry bumped to bootstrap-file
   version 7). Core retains NO `role <-grantedBy- permission` reference (clean AC-G1). FR22
   `rolesCarryingPermission` degrades to empty when rbac-domain is absent (the denial builder's GET
   simply not-founds — already-handled path, no code change).

3. **Service key derivation under Path B (AC-B2).** **RESOLVED = leave on the 12.5 seed key
   `cap.<actor>`.** Part B is a deletion, not a re-key. After deleting the service MATCHes the service
   path finds no `serviceAccess` entry and denies by absence (§6.8). Not moved to `cap.svc.<actor>`.

4. **rbac-installed detection for the auth-hook (AC-A2 / pin #2).** **RESOLVED = startup package-vertex
   probe.** `pkgmgr.IsPackageInstalled(ctx, conn, "rbac-domain")` is called in `cmd/processor/main.go`
   and threaded through `processor.MakePipeline` (`AuthWiring.RbacRolesActive`) →
   `SelectAuthorizerOpts`. Uninstalled rbac-domain → `RbacRolesActive=false` → platform read targets
   `cap.<actor>` for all actors (ordinary actors deny by absence, AC-A2 degradation).

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (Amelia / bmad-dev-story)

### Debug Log References

(none — no stuck-loop halts.)

### Completion Notes List

**Part A — rbac role/permission decomposition.**
- `packages/rbac-domain/lenses.go` (NEW) declares two lenses wired into `Package.Lenses`:
  `capabilityRoles` (actor-aggregate, `cap.roles.{actorSuffix}`, bodyColumns `[platformPermissions,
  roles]`, walks `holdsRole → role <-grantedBy- permission`) and `capabilityRoleIndex`
  (operation-aggregate, `IntoKey: ["operationType"]`, the FR22 `cap.role-by-operation.<op>` index).
  Both activate through the live 12.3/12.4 `projectionKind`/operation-aggregate paths with zero
  `cmd/` edits. Added `LensSpec.IntoKey` (`internal/pkgmgr/definition.go`) + emission in `build.go`
  so the operation-aggregate lens keys by `operationType` (routes via the existing
  `isOperationRoleIndexLens` in `cmd/refractor/main.go`).
- **Where the rbac auth-hook is constructed + how it is gated (pin #2):** NOT a separate
  `ExtraEntries` dispatch entry. Per Q1, the rbac routing is folded into the SINGLE platform entry's
  **class-aware key derivation** (`classAwarePlatformKey` in `step3_auth_matcher.go`): system actor →
  `cap.<actor>`, ordinary actor → `cap.roles.<actor>`. It is gated by `AuthWiring.RbacRolesActive`,
  computed in `cmd/processor/main.go` from `pkgmgr.IsPackageInstalled(ctx, conn, "rbac-domain")` (a
  startup core-kv package-vertex probe) and threaded through `processor.MakePipeline` →
  `SelectAuthorizerArgs`. The system-actor set is discovered at startup via
  `bootstrap.SystemActorKeys` (scans core-kv for protected `identity` vertices — exactly the set the
  anchor projects — so the processor stays self-contained and does not depend on the bootstrap-file
  key space being loaded into the processor process).

**Primordial-anchor approach (Q1 = Option (a)).** Core's `capability` cypher anchors per-identity,
filters `WHERE identity.data.protected = true`, and RETURNs a LITERAL list of the 5 scope:any kernel
root grants — no rbac graph walk. Ordinary actors → zero rows → no core `cap.<actor>` doc (they read
`cap.roles.<actor>`). bodyColumns shrank to `[platformPermissions]`; `serviceAccess`/`roles` moved to
`StaticEmptyColumns` so the §6.2 doc shape stays intact (parser-tolerant). Proven non-brick LIVE: the
`make up` readiness gate satisfied `capProjections=3` and the admin doc carries the 5 root grants;
`service_actor_auth_parity_test.go` still green.

**Part B — service/location remnants retired (Path B).** Confirmed `packages/service-location/` holds
only `CONCEPT.md`. Deleted the `containedIn`/`availableAt`/`unavailableAt`/`permitsOperation` branch +
`serviceAccess` RETURN column from the god-cypher (done as part of the anchor rewrite — the new cypher
names none of those types). Service path stays on the 12.5 seed key `cap.<actor>` (Q3) and denies by
absence. §6.10 service behaviors + Hello-Lattice service milestones deferred to a future service
package; recorded in `docs/decisions/projection-plane-decomposition.md` (not silently dropped). The
capability conformance/e2e tests that asserted service-access via the god-cypher were reconciled:
role/platform assertions moved to the `capabilityRoles` lens, service assertions dropped.

**Registry hardening (AC-X) — overlap model + why it accepts the real rbac config but rejects a
hostile one.** Moved `buildAuthRegistry` from name-only to a **structural coverage descriptor**: each
`authEntry` declares `authCoverage{kind ∈ {platform,task,service}, catchAll, scopeTag}`. A
package-derived extra is rejected fail-closed when it reuses a core path name, claims a core
specific cell (task/service), claims the always-true platform catch-all (no scope tag), reuses a
platform scope tag, or — via a representative-authContext **probe matrix
(`checkCoverageMatchesPredicate`)** — matches a foreign cell or an unconditional platform context
(`nil`/`{}`). The probe matrix is structural, never a runtime fan-out. **Why it accepts the real rbac
config:** the rbac contribution is NOT an extra at all — it is the platform entry's key derivation, so
it is structurally part of the trusted core platform path and never enters the guard. **Why it rejects
a hostile one:** an attacker-supplied extra ordered before `platform` that selects the platform cell
(to siphon the read onto a package key) either declares `pathPlatform` with no scope tag → rejected as
the always-true claim, or declares a narrow scope but its predicate matches `nil`/`{}` → rejected by
the probe-matrix cross-check. A legitimately disjoint scoped extra (unique scope tag, matches only a
narrow non-unconditional slice) is accepted. The pre-existing 12.5 `ext-route` extension-point test
was reconciled with the new coverage field and still passes. Tests:
`internal/processor/step3_auth_rbac_hook_test.go` (overlap reject + always-true reject + mislabeled
reject + disjoint accept + name-reuse reject + ordinary-actor cap.roles single-GET + system-actor
cap.<actor> single-GET + ordinary-deny-by-absence).

**capabilityRoleIndex move + bootstrap-file version bump.** Removed `CapabilityRoleIndexLensDefinition`
(core's last `role <-grantedBy- permission` reference) and its primordial seed + nanoid-registry
fields; bumped the bootstrap-file version 6 → 7 (forces a clean regen on `make down && up`, which CI
does for Gate 2/3). `PrimordialVertexKeyCount` 28 → 27. `verify-kernel.go` + `internal/bootstrap/
verify.go` updated to verify 1 lens (capability anchor, now incl. projectionKind+output aspects);
`verify-package-rbac.go` extended to verify the 2 new rbac lenses (install via the REAL InstallPackage
path → 64 OK, was ~34).

**Follow-ups for the planning lead (NOT done — flagged per AC-G1):** (1) mark the
`lattice-architecture.md` god-cypher open item resolved; (2) record the completed decomposition in
Contract #6 §6.1. Both are planning-artifact / frozen-contract edits — proposed, not performed.

**Verification gates — all PASS (run inline, CI order):**
- `go build ./...` → PASS (exit 0).
- `make vet` → PASS.
- `golangci-lint run ./...` → PASS (0 issues).
- `make verify-kernel` → PASS (ALL ASSERTIONS PASSED; live anchor projection confirmed,
  `capProjections=3`).
- `make verify-package-rbac` → PASS (64 OK; both new lenses verified via real install).
- `make verify-package-identity-hygiene` → PASS (31 OK; lens-cypher hygiene unaffected).
- `go test ./...` (FULL tree) → PASS (47 packages OK, 0 failures). `internal/processor/outbox`
  flaked once on the known embedded-NATS `meta.inf.tmp` JetStream timing issue; passed on re-run.
- `make test-bypass` (Gate 2) → PASS (4/4 BLOCKED).
- `make test-capability-adversarial` (Gate 3) → PASS (6/6 cleared — 5 DEFENDED, 1 ACCEPTED-WINDOW;
  matches pre-existing baseline; role-manipulation vectors v1/v3/v4 DEFENDED).

**Uncertain / for review:** (1) The narrow-anchor's `protected:true` predicate selects exactly the
3 kernel-seeded system identities (only the kernel sets `protected` on identities; the step-8 commit
gate blocks packages from setting it). If a future kernel change ever seeds a protected NON-system
identity it would erroneously receive root grants — worth a guard rail later, but out of scope here.
(2) Stale-key cleanup on a PRODUCTION upgrade: an ordinary actor that had a core `cap.<actor>` under
the old god-cypher gets no new core projection (zero rows ≠ delete without a realness filter), so a
pre-existing `cap.<actor>` would linger until rbac-domain reprojects `cap.roles.<actor>`. Fresh
systems + all tests start clean so this is invisible to the gates; production migration is an ops/
Winston concern, noted not handled.

### File List

**New:**
- `packages/rbac-domain/lenses.go`
- `internal/bootstrap/system_actors.go`
- `internal/processor/step3_auth_rbac_hook_test.go`

**Modified — core/source:**
- `internal/bootstrap/lenses.go` (narrow primordial-anchor cypher; removed `CapabilityRoleIndexLensDefinition`)
- `internal/bootstrap/primordial.go` (dropped role-index seed; updated `makeLensSpecBody`)
- `internal/bootstrap/nanoid.go` (removed `CapabilityRoleIndexLens*`; version 6→7; `PrimordialVertexKeyCount` 28→27)
- `internal/bootstrap/verify.go` (capability-lens aspect set incl. projectionKind+output; dropped role-index)
- `internal/pkgmgr/definition.go` (`LensSpec.IntoKey`)
- `internal/pkgmgr/build.go` (emit `IntoKey` in lens spec body)
- `internal/pkgmgr/installer.go` (`IsPackageInstalled` probe)
- `internal/processor/step3_auth_matcher.go` (coverage model + hardened `buildAuthRegistry` + class-aware platform key)
- `internal/processor/step3_auth_capability.go` (`rolesKeyFromActor`; `newCapabilityAuthorizer` options)
- `internal/processor/step3_auth.go` (`RbacRolesActive`/`SystemActorKeys` opts → class-aware derivation)
- `internal/processor/commit_path.go` (`AuthWiring` param on `MakePipeline`)
- `cmd/processor/main.go` (rbac-installed probe + `SystemActorKeys` → `AuthWiring`)
- `packages/rbac-domain/package.go` (`Lenses: Lenses()`)
- `packages/rbac-domain/manifest.yaml` (declare the 2 lenses)

**Modified — CI verify scripts:**
- `scripts/verify-kernel.go` (1 lens; dropped role-index assertions; counts)
- `scripts/verify-package-rbac.go` (verify the 2 new rbac lenses)

**Modified — tests reconciled:**
- `internal/processor/step3_auth_capability_test.go` (ext-route coverage field)
- `internal/refractor/refractor_capability_e2e_test.go` (anchor: protected actor, no service/roles)
- `internal/refractor/refractor_capability_multi_e2e_test.go` (drive capabilityRoles + role-index; cap.roles keys; drop service)
- `internal/refractor/refractor_capability_aspectfanout_e2e_test.go` (drive capabilityRoles; cap.roles key)
- `internal/refractor/refractor_capability_linkfanout_e2e_test.go` (drive capabilityRoles; cap.roles key)
- `internal/refractor/e2e_supervisor_helper_test.go` (`capabilityRolesSpecForTest` helper)
- `internal/refractor/ruleengine/full/parse_test.go` (anchor shape; new capabilityRoles parse test; removed unused `hasAntiPattern`)
- `internal/refractor/ruleengine/full/service_actor_class_test.go` (primordial-anchor protected-gates-root)
- `internal/refractor/ruleengine/full/bootstrap_e2e_test.go` (capabilityRoles lens e2e; drop service)
- `internal/refractor/ruleengine/full/capability_lens_contract_test.go` (anchor §6.2 shape; protected actor)

**Modified — docs:**
- `docs/decisions/projection-plane-decomposition.md` (12.6 implementation record + primordial-anchor + Path-B disposition)
