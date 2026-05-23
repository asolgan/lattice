---
title: Verify-Package Hardening — Closing Summary
story: Phase-1 hygiene carry — strengthen verify-package-* Makefile gates
date: 2026-05-22
model: Sonnet 4.6
---

# Verify-Package Hardening — Closing Summary

## Files Added

| File | Purpose |
|---|---|
| `scripts/verify-package-rbac.go` | Assertion script for rbac-domain package |
| `scripts/verify-package-identity.go` | Assertion script for identity-domain package |
| `scripts/verify-package-identity-hygiene.go` | Assertion script for identity-hygiene package |

## Makefile Changes (diff snippet)

**Before** (each target ended with):
```makefile
NATS_URL=$(NATS_URL) BOOTSTRAP_JSON_PATH=$(BOOTSTRAP_JSON) ./bin/lattice-pkg list
```

**After** (each target now runs the assertion script):
```makefile
@echo "==> Running rbac-domain package assertions..."
NATS_URL=$(NATS_URL) BOOTSTRAP_JSON_PATH=$(BOOTSTRAP_JSON) go run ./scripts/verify-package-rbac.go
```

The install step (`./bin/lattice-pkg install packages/<name>`) was preserved; only the verification step was replaced. The comment line for `verify-package-rbac` was also updated to reflect the new behavior ("assert its KV state" vs "verify via package list").

## Per-Script OK-Count Target vs Actual

| Script | Target | Estimated Actual |
|---|---|---|
| `verify-package-rbac.go` | ~30 | ~40 (10 header checks + 10 ops × 3 each) |
| `verify-package-identity.go` | ~30 | ~24 (7 DDL + 3 roles + 11 perm/grant + 3 manifest) |
| `verify-package-identity-hygiene.go` | ~20 | ~17 (6 DDL + 5 Lens + 3 perm/grant + 3 manifest) |

Note: Docker was not available locally; exact OK counts are estimated from code analysis. Actual line counts will be confirmed by CI.

## Assertion Coverage (What Each Script Checks)

### verify-package-rbac.go
- Scans `core-kv` for `vtx.meta.*.canonicalName` = `rbac` → finds DDL meta-vertex
- Asserts DDL vertex: `class=meta.ddl.vertexType`, `isDeleted=false`
- Asserts 4 DDL aspects: `.canonicalName`, `.permittedCommands` (all 10 ops), `.description`, `.script`
- For each of 10 expected ops (AssignRole, CreatePermission, CreateRole, GrantPermission, RevokePermission, RevokeRole, TombstonePermission, TombstoneRole, UpdatePermission, UpdateRole):
  - Finds permission vertex `vtx.permission.*[operationType=<op>]`
  - Asserts `scope=any`
  - Asserts `lnk.permission.<permID>.grantedBy.role.<operatorRoleID>` exists and is not tombstoned
- Finds package manifest `vtx.package.*.manifest[name=rbac-domain]`
- Asserts manifest `name=rbac-domain`

### verify-package-identity.go
- Finds DDL meta-vertex by `canonicalName=identity`
- Asserts class, isDeleted, 4 aspects including `permittedCommands=[CreateUnclaimedIdentity, UpdateIdentityState, ClaimIdentity]`
- Discovers role NanoIDs by scanning `vtx.role.*.canonicalName` aspects (operator from bootstrap; consumer/frontOfHouse/backOfHouse from KV scan)
- Asserts 3 user-facing roles (consumer, frontOfHouse, backOfHouse) exist and are not tombstoned
- For each of 3 ops:
  - Finds permission vertex, asserts correct scope (`any`/`self`)
  - Asserts each expected grantedBy link (5 total)
- Finds and asserts package manifest

### verify-package-identity-hygiene.go
- Finds `identityHygiene` DDL by canonicalName scan
- Asserts class, isDeleted, permittedCommands=[MergeIdentity], description, script
- Finds `duplicateCandidates` Lens meta-vertex by canonicalName scan
- Asserts Lens `class=meta.lens`, isDeleted=false
- Asserts `.spec` contains `secondaryInboundEdges`, `secondaryOutboundEdges`, `levenshteinRatio`
- Asserts `.canonicalName` aspect
- Finds MergeIdentity permission vertex, asserts `scope=any`
- Asserts `lnk.permission.<permID>.grantedBy.role.<operatorID>` exists
- Finds and asserts package manifest

## Gate Results

| Gate | Result | Notes |
|---|---|---|
| `go build ./...` | PASS | No compile errors |
| `make vet` | PASS | 0 issues |
| `golangci-lint run ./...` | PASS | 0 issues |
| `go test ./... -p 1 -count=1` | PASS (effective) | Pre-existing Deviation 14 flake in `internal/refractor` manifested in full parallel run; passes when run alone. All three package suites (rbac-domain, identity-domain, identity-hygiene) passed. |
| `make verify-package-rbac` | NOT RUN — Docker unavailable locally | Scripts compile cleanly; flagged for CI/Winston |
| `make verify-package-identity` | NOT RUN — Docker unavailable locally | Scripts compile cleanly; flagged for CI/Winston |
| `make verify-package-identity-hygiene` | NOT RUN — Docker unavailable locally | Scripts compile cleanly; flagged for CI/Winston |
| CI workflow (`.github/workflows/ci.yml`) | No changes needed | Already calls `make verify-package-*`; Makefile update propagates automatically |

## Closing Grep Result

```
grep -rn "AdjacencyReads\|LinkScans\|ScanPrefixes\|WithAdjacencyBucket\|AdjacencyForNode\|keys_with_prefix" internal/ cmd/ packages/
```

Output:
```
internal/processor/starlark_runner.go:372:// dict. Story 4.6 walk-back removed the `keys_with_prefix` custom
packages/identity-domain/package_test.go:43:	for _, forbidden := range []string{"KVListKeys", "list_keys", "keys_with_prefix"} {
packages/rbac-domain/package_test.go:54:		"KVListKeys", "list_keys", "scan(", "keys_with_prefix",
```

**Zero operational hits.** All three matches are: one comment in a processor file, and two test strings in package tests that check FOR the forbidden patterns (i.e., they're the enforcement tests, not violations).

## Deviations

**None.** All deliverables implemented as specified.

Minor note: `verify-package-identity.go` produces ~24 OK lines vs the ~30 target. This is because identity-domain has 3 permissions with 5 total grant links (not 10 like rbac-domain). The ~30 target in the brief was an approximation; actual coverage is complete per §4 spec.

## Token Self-Estimate

~22,000 tokens (exploration: ~10K, writing 3 scripts: ~9K, verification + summary: ~3K).
