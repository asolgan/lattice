# identity-domain Capability Package

Identity vertex creation, claim, and state-machine management. Story 4.7
moved these operations out of the bootstrap kernel into this installable
package.

## Contents

- DDL `identity` (class `meta.ddl.vertexType`) handling 3 operations:
  - `CreateUnclaimedIdentity` (grants → frontOfHouse, backOfHouse, operator)
  - `UpdateIdentityState`     (grants → operator)
  - `ClaimIdentity` (scope=self, grants → consumer)
- 3 permission vertices + 5 `grantedBy` link grants
- PreInstall hook seeds the 3 user-facing roles
  (consumer, frontOfHouse, backOfHouse).

## State machine

`unclaimed → claimed` via UpdateIdentityState. The `merged` state is
set only by the identity-hygiene package's MergeIdentity script.

## Install

    lattice-pkg install packages/identity-domain

Depends on `rbac-domain` (warn-and-proceed in Phase 1).

The install is two-stage:

1. **PreInstall** (substrate-direct): creates `consumer`, `frontOfHouse`,
   `backOfHouse` role vertices and a `vtx.roleindex.*` entry per role
   for idempotent re-runs.
2. **Atomic batch**: DDL meta-vertex + 4 aspects, package manifest, 3
   permission vertices, 5 grantedBy link grants.

## Architectural notes

- All script reads are known-key only. Duplicate-detection index
  lookups use `crypto.sha256NanoID` to produce stable index keys; the
  caller declares them in `ContextHint.Reads`.
- The DDL script is verbatim from the Story 4.6-trimmed
  `internal/bootstrap/identity_ddl.go`; no functional change in 4.7.
