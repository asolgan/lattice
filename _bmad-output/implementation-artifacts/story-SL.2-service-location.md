# Story SL.2 — service-location package (the cap.svc access scheme)

**Status:** done (CI green — e4af07c + the 715b14b cypher-pin fix-forward; §6.12 contract edit left UNCOMMITTED for Andrew's review)
**Design:** `_bmad-output/implementation-artifacts/service-location-design.md` (rev.3 — §2, §3, §4, §5, §6)
**Review depth:** FULL 3-layer adversarial (security-plane — a projection bug = privilege escalation or DoS).
**Depends on:** `location-domain` (SL.1, done — `vtx.unit/building/property` + `containedIn`), `service-domain`
(the `service` templates + `availableAt`).

## Goal

Ship `service-location` — the residence-based **service-access authorization scheme** that projects
`cap.svc.<actor>` (the location grant source). Plus the `service-domain` `availableAt` refactor and the
core service-key re-point. After this, a service op is authorized iff the actor's residence chain reaches a
location where the service is `availableAt` (with `unavailableAt` exclusions), via the real Capability lens.

## Acceptance criteria

1. **`service-location` package** — `Depends: ["location-domain", "service-domain"]`. Wire/unwire ops for
   `residesIn` (identity→location), `availableAt` (service-template→location), `unavailableAt`
   (service-template→location), `permitsOperation` (service→op-meta). Each wire-op **validates endpoint
   classes** at the op (residesIn target ∈ location; availableAt/unavailableAt source = a service *template*,
   target = location; permitsOperation source = service). `residesIn` cardinality: multiple allowed. Link
   direction per Contract #1 §1.1 (run the sentence test). Permissions `grantsTo:[operator]`, scope `any`.
2. **`service-domain` refactor** — remove `availableAt` from `CreateServiceTemplate` (the param + the
   link-write in the Starlark script + its test fixtures/assertions). `availableAt` is now owned by
   service-location. Keep `providedBy`/`instanceOf`/`providedTo`. `service-domain` becomes location-unaware.
3. **`capabilityServiceAccess` lens** — `Bucket: "capability-kv"`, `ProjectionKind: "actorAggregate"`,
   `OutputKeyPattern: "cap.svc.{actorSuffix}"`, `BodyColumns: ["serviceAccess"]`, `EmptyBehavior: "delete"`,
   `Freshness: "auto"`, `AnchorType: "identity"`. Cypher (mirror `rbac-domain`'s `capabilityRoles` spec shape):

   ```cypher
   MATCH (identity:identity {key: $actorKey})
   OPTIONAL MATCH (identity)-[:residesIn]->(loc0)-[:containedIn*0..]->(loc)<-[:availableAt]-(svc)
   WHERE svc.class = $templateClass
     AND NOT (identity)-[:residesIn]->(ex0)-[:containedIn*0..]->(exLoc)<-[:unavailableAt]-(svc)
   RETURN
     identity.key AS actorKey,
     collect(DISTINCT {
       service: svc.key, serviceClass: svc.class, resolvedVia: [loc.key],
       allowedOperations: [(svc)-[:permitsOperation]->(op) | {operationType: op.data.operationType}]
     }) AS serviceAccess
   ```
   - **`availableAt`/`unavailableAt` are `service→location`** — match `(loc)<-[:availableAt]-(svc)` (the link
     points FROM the service TO the location). Do NOT invert.
   - **The exclusion existential MUST use FRESH variables** `ex0`/`exLoc` (never bound elsewhere) so it
     re-walks the actor's whole chain for a *closer* `unavailableAt`. A bound-`loc` version pins the matched
     location and silently over-grants (§6.10 item 1). The constrained `full` engine only pins
     already-bound targets (`internal/refractor/ruleengine/full/executor.go:613-619`); fresh vars seed free.
   - **`svc` class-guarded to templates** (`svc.class` = the template class, or a STARTS/ENDS-WITH predicate
     if the engine supports it; verify against the engine). Prevents sweeping service *instances* / claim
     vertices. If the exact predicate form is awkward, match the `.class` aspect — but the guard MUST exist.
   - Determine `$templateClass`: `service-domain` templates carry class `service.<family>.template`. The
     guard must admit any `.template` and exclude `.instance`. Decide the cleanest engine-expressible form
     (a parameter, a STARTS WITH, or an aspect match) and document it.
4. **Core service-key re-point** — add `serviceKeyFromActor(actor) → "cap.svc." + <suffix>` (mirror
   `ephemeralKeyFromActor`/`rolesKeyFromActor` in `internal/processor/step3_auth_capability.go`), and swap
   the `service` entry's `keyDerivation` (`internal/processor/step3_auth_matcher.go` ~line 112) from
   `capabilityKeyFromActor` → `serviceKeyFromActor`. **Unconditional** (system actors never set
   `ac.Service`). One-key-per-path preserved.
5. **Contract §6.12 edit — IN PLACE, UNCOMMITTED.** Edit `docs/contracts/06-capability-kv.md` §6.12 to
   reconcile that a service-op denial no longer surfaces `actorRoles` (the re-pointed `cap.svc.<actor>` key
   carries no roles; under the residence scheme that's correct). Leave it **UNCOMMITTED** for Andrew's
   review — this is NOT a CAR (Andrew's explicit call). Do NOT commit any `docs/contracts/*` change.
6. **§6.10 executor-proof fixtures (the proof the cypher is right — author these and MAKE THEM PASS):**
   - **Multi-level exclusion (§6.10 item 1):** actor `residesIn` penthouse; penthouse `containedIn` building;
     laundry `availableAt` building; laundry `unavailableAt` penthouse → laundry **NOT** in `serviceAccess`.
     (This behavior has NEVER been tested in this codebase — it is the load-bearing proof.)
   - **Transitive availability (§6.10 item 2):** resident of a unit in a building gets a service
     `availableAt` the building.
   - **Instance-not-swept:** a service *instance* (class `.instance`, no `availableAt`) is never projected.
   - Run these against the REAL lens execution (the `full` engine), not a hand-built doc.
7. **Reconcile oracles** — update the bypass + auth service-path oracles to the re-pointed key: a service op
   now denies-by-absence on missing `cap.svc.<actor>` (reason may shift to `NoCapabilityEntry`). Keep the
   asserted allow/deny OUTCOMES; update only the key/reason where the projection moved.
8. **Guard + parity tests** — a service-plane resurrection test (an `availableAt`-era upsert replayed after an
   `unavailableAt`/`residesIn` tombstone must NOT resurrect access; the lens is auth-plane ⇒ guarded); a
   system-actor service deny-by-absence test (drive `authContext.Service` for a system actor → deny on
   missing `cap.svc.<systemActor>`); `service_actor_auth_parity_test.go` + bootstrap-quiescent E2E stay green.
9. **Gates green** — `go build ./...`, `make vet`, `golangci-lint run ./...`, `make verify-kernel`,
   `make verify-package-service-location` (new) + co-install with `location-domain` + `service-domain`,
   `make test-bypass` (Gate 2 BLOCKED), `make test-capability-adversarial` (Gate 3 DEFENDED — incl. the
   multi-level exclusion vector against the REAL lens), package tests.
10. **No history/changelog comments** (house rule). Match surrounding idioms.

## Dev notes

- **Lens template:** `packages/rbac-domain/lenses.go` (`capabilityRoles`) — copy the `LensSpec` /
  `OutputDescriptorSpec` shape line-for-line; only the cypher + key pattern + body columns differ.
  `orchestration-base` `capabilityEphemeral` is a second actorAggregate example.
- **Activation is Warn-and-proceed, NOT fail-closed:** the `containedIn*0..` hop yields a `*CompileError`,
  but `InstallActorAggregate` (`internal/refractor/projection/driver.go:168-182`) catches it, logs a Warn,
  and activates with broad-BFS fan-out; auth-plane ⇒ the projection guard is enabled. Expect the Warn; it is
  not an error.
- **Lens-walked links must filter tombstoned vertices** (SL.1 note: Tombstone doesn't cascade `containedIn`)
  — a dead location/service in the chain must not grant access.
- **Sequence the build:** (1) package + 4 ops + service-domain refactor; (2) the lens + the §6.10 executor
  fixtures (make them pass — that's the cypher proof); (3) the core re-point + oracle reconciliation + the
  §6.12 edit + guard/parity tests + e2e.
- Sub-agent builds; **does NOT commit/push/branch**, and does **NOT** commit any `docs/contracts/*` change
  (the §6.12 edit stays uncommitted). Winston reviews, runs the 3-layer, fixes forward, commits, watches CI.

## Completion notes

Built by a dev sub-agent (recovered after an accidental stop — its work was intact in the tree; the lead
finished the `cmd/lattice-pkg` registration it hadn't reached and the gate run) + a full 3-layer adversarial
review (Blind Hunter / Edge-Case Hunter / Acceptance Auditor). All gates green: build, lint (0), vet,
verify-kernel, verify-package-service-location (51 OK), the §6.10 lens fixtures, the processor re-point tests,
Gate 2 (BLOCKED) + Gate 3 (DEFENDED).

**3-layer review outcome:**
- **Fixed — MAJOR (Blind + Edge-Case, empirically proven):** the multi-level exclusion re-walked from
  `identity` over the actor's *whole* residence set, so a service `unavailableAt` ONE residence was
  suppressed for the ENTIRE actor (a wrong-deny — fails safe, never an over-grant). Fixed: the exclusion
  existential now anchors on the bound `loc0` (the granting residence) — `NOT (loc0)-[:containedIn*0..]->
  (exLoc)<-[:unavailableAt]-(svc)` — making it per-residence-chain. Proven by
  `TestServiceLocationLens_MultiResidence_{Partial,Full}Exclusion`; the single-residence §6.10 item-1 test
  still passes (no regression).
- **Accept-with-note (inert / safe — all three lenses verified):**
  - `serviceClass: svc.class` emits the bare root class `"service"`, not the rich `.class`-aspect value the
    §6.2 example shows. Inert today (no code consumer; `deniedServiceClass` reads the service vertex's aspect
    directly at denial time) and the rich class is not lens-reachable (the root-field-vs-aspect "class" name
    collision). **Flagged for Andrew**; revisit when the FR22 denial-detail consumers are actually built.
  - The §6.10 multi-level vector is proven by the full-engine package test, not wired into the Gate-3
    `TestCapAdv` suite (the bypass service infra was removed in Story 12.6, Path B). Coverage exists; an
    end-to-end Gate-3 service vector is a scoped follow-up.
  - `resolvedVia` per-path singletons + the `:service`-label-free `svc` match (op-validated + instanceOf
    guard) — documented design choices, harmless for auth.
- **§6.12 contract edit:** made IN-PLACE in `docs/contracts/06-capability-kv.md`, left **UNCOMMITTED** for
  Andrew's review (his explicit instruction — not a CAR).

**The dev agent's notable finds (better than the spec):** the structural template guard
`NOT (svc)-[:instanceOf]->(svcTpl)` — since `svc.class` resolves to the bare root class and can't reach the
`.class` aspect — and the `threadsDocOnDenial` removal (the `cap.svc` doc carries no roles).
