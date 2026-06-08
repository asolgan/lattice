# service-location — CONCEPT (not yet a package)

> **Status: concept / intent only.** There is **no installable `service-location` package** in this
> directory yet — no `manifest.yaml`, `ddls.go`, lenses, or permissions. This document captures what
> the package is *meant to be* so the intent stops living implicitly inside the bootstrap god-cypher
> and the planning docs. Created 2026-06-07 (Andrew's request, during the Epic 12 / projection-plane
> decomposition session). When someone builds the package, this is the brief; until then, do not treat
> a `packages/service-location/` directory containing only this file as an installable package.

## Why this exists

Today the *service/location* grant vocabulary lives in **core** — the bootstrap `capability`
god-cypher (`internal/bootstrap/lenses.go`) hard-codes `containedIn` / `availableAt` / `unavailableAt`
/ `permitsOperation` traversal over `service` and location vertices it doesn't own. That is the same
inverted dependency Epic 12 removes for `task` (orchestration-base) and `role`/`permission`
(rbac-domain). `service-location` is the package that is *supposed* to own this domain — but it has
never been specified anywhere, only named (`lattice-architecture.md` ~line 1221; Contract #6 §6.1
contract-contribution note; the refractor-lens-decomposition brief).

This package is the natural home for the **Loftspace** reference vertical's spatial model (units,
buildings, services available at locations) and for the **`serviceAccess`** half of the per-actor
capability projection.

## Scope (what the package would own)

### Vertex types
- **`service`** (`vtx.service.<id>`) — a bookable/invokable service (e.g. executive cleaning, rent
  payment, laundry). Carries `class` (echoed into `serviceAccess[].serviceClass`, Contract #6 §6.5).
- **Location types** with containment — Contract #6 examples use `unit` (`vtx.unit.<id>`), with
  `building` / `property` above via `containedIn`. The package decides the concrete location type set;
  containment is transitive.
- (Possibly) **`operation`-on-service** linkage vertices if operation-level availability overrides are
  modeled as graph (see §6.10 item 3).

### Link conventions (Contract #6 §6.9 — recommended, become package-owned here)
| Link | Source → Target | Meaning |
|---|---|---|
| `containedIn` | location → location | physical/logical containment; transitive |
| `availableAt` | service → location | service offered at this location (and, by default, locations contained within) |
| `unavailableAt` | service → location | explicit exclusion override; closer exclusion wins over distant availability |
| `permitsOperation` | service → operation-meta | which operations the service exposes |
| `residesIn` | identity → location | actor resides at a location (guests/family — independent of lease) |
| `leases` | identity → lease → (unit via `containedIn`) | actor holds a lease granting residence |

Per Contract #1 §1.1 the later-arriving vertex is the link source — run the sentence test when these
are finalized (e.g. "service availableAt location").

### Operations (DDL)
Create / update / tombstone for `service` and location vertices, plus the link-management ops that
wire `availableAt` / `unavailableAt` / `containedIn` / `residesIn`. Shape mirrors `rbac-domain`'s
operator-granted op set (see `packages/rbac-domain/`). Exact verb list TBD by the implementer.

### The `capabilityServiceAccess` lens (the Epic 12 payload)
The package ships a **`projectionKind: actorAggregate`** lens (Epic 12 Story 12.3/12.4 machinery) that
projects, per actor, the `serviceAccess[]` section of the capability model to a **disjoint key**
(working name **`cap.svc.<actor-suffix>`**) in the shared `capability-kv` bucket — the
contract-contribution pattern (Contract #6 §6.1) already used by `cap.ephemeral.*`. It also **registers
its step-3 auth-hook** (Story 12.5): the service path's matcher kind + the `cap.svc.<actor>` key, one
key per path (single-GET preserved).

This lens **must** preserve the Contract #6 §6.10 required behaviors that currently live in the
god-cypher:
1. **Multi-level containment exclusion** — an `unavailableAt` at any level of the actor's containment
   chain beats an `availableAt` higher up.
2. **Direct + transitive availability** — `availableAt` a location grants actors at that location and
   in locations contained within it.
3. **Operation-level overrides** — per-operation `availableAt`/`unavailableAt` refine service-level
   resolution; the result is `serviceAccess[].allowedOperations[]`.

Output row → `serviceAccess[]` entry shape: `{ service, serviceClass, resolvedVia[], allowedOperations[] }`
(Contract #6 §6.5).

## How it lands relative to Epic 12

- Epic 12 Stories **12.3–12.5** make "a package owns its grant projection + auth-hook" a pure package
  addition (no core edit). This package is then *just another contributor*, exactly like
  `orchestration-base`'s `capabilityEphemeral`.
- Epic 12 Story **12.7** removes the service/location MATCHes from the bootstrap god-cypher. Its
  **Path A** = "this package exists → implement the lens here per this CONCEPT"; **Path B** = "this
  package doesn't exist yet → just delete the remnants, and this package projects into the
  already-registered-but-empty `cap.svc.*` key space when it eventually ships." Either way core stops
  referencing service/location types.

## Deferred / out of scope
- **Temporal service-availability windows** (open/closed hours, holiday closures, maintenance) are
  explicitly **out** of the capability projection (Contract #6 §6.11) — static topology only; temporal
  rejection is business-logic or a separate mechanism.
- **`owned` capability scope** (ownership-link model) — Contract #6 §6.7, Phase 2+.

## Open questions for the implementer
- Concrete location type taxonomy (`unit`/`building`/`property`/`room`?) and whether `lease` is owned
  here or by a separate leasing package.
- Final disjoint key name (`cap.svc.<actor>` is a working name; confirm against Contract #6 §6.1 at
  build time).
- Whether `resolvedVia` provenance is reconstructed by the lens or carried on the grant.
- Relationship to the `lease-signing` package (Epic 11) — `lease-signing` creates lease *instances*;
  this package likely owns the lease *type* + spatial graph it sits in. Settle ownership before both
  are built.
