# AI-Agent Contract CR Fix Summary — Phase 1.5

## P0

| Finding | File:Line | Description |
|---------|-----------|-------------|
| F-001 | `internal/aiagent/traversal.go` — `DDLAspects` struct, `ReadDDLAspects` | Added `Script string` and `PermittedCommands []string` to `DDLAspects`; added KVGet+unmarshal blocks for `.script` (data.source) and `.permittedCommands` (data.commands) in `ReadDDLAspects`; confirmed field names match Bootstrap writers (`meta_ddl.go` + `primordial.go`); both treat missing/tombstoned as `ErrAspectMissing`. |

## P1

| Finding | File:Line | Description |
|---------|-----------|-------------|
| F-002 | `internal/aiagent/traversal.go` — `ReadDDLAspects` aspect blocks | Added `isDeleted` check on all seven aspect envelopes using the shared `aspectEnvelope` helper (N-002 also applied); returns `ErrAspectMissing` wrapping aspect name if tombstoned. |
| F-003 | `internal/aiagent/traversal.go:DiscoverDDL` | Changed early-return on first match to accumulate all live matches; returns distinct error `"aiagent: multiple live DDLs with canonicalName %q; cell is in inconsistent state"` when count > 1. |
| F-004 | `internal/aiagent/traversal.go:ReadCapability` godoc | Updated godoc with staleness warning: callers in latency-sensitive paths should check `doc.ProjectedAt` against their clock; Processor will reject stale caps with `AuthFreshnessExceeded`. `ProjectedAt` was already exported on `CapabilityDoc`. |

## P2

| Finding | File:Line | Description |
|---------|-----------|-------------|
| F-005 | `internal/aiagent/traversal.go` — `ErrAspectMissing`, `ErrCompensationAspectMissing` | Added godoc to both sentinels explaining the double-`%w` chain: when caused by a missing KV key the error also wraps `substrate.ErrKeyNotFound`; callers can use `errors.Is` for either. |
| F-006 | `internal/aiagent/traversal.go:NewTraverser` | Added panic guards for nil conn and empty bucket names (programming-error panics matching substrate keys.go style). |
| F-007 | `internal/aiagent/traversal.go:ReadDDLAspects` | Added liveness pre-check at the top: KVGet vertex at ddlKey, unmarshal `isDeleted`, return error (wrapping `ErrAspectMissing` for missing key, distinct tombstone error if deleted) before any aspect reads. |
| F-008 | `internal/aiagent/traversal.go:DiscoverDDL` godoc | Added doc note that `DiscoverDDL` scans all Core KV keys on every call; client-side 3-segment `vtx.meta.*` filter already present; KVListKeysByPrefix + per-Traverser caching tracked as contracts-hardening work. |

## Nits

| Finding | File:Line | Description |
|---------|-----------|-------------|
| N-001 | `internal/aiagent/traversal.go:ReadDDLAspects` godoc | Updated "five" → "seven" in godoc (covered as part of F-001). |
| N-002 | `internal/aiagent/traversal.go:ReadDDLAspects` | Introduced `aspectEnvelope` named helper type to hold `IsDeleted bool` + `Data json.RawMessage`; replaced all six (now seven) inline anonymous structs with this type, eliminating repetition. |

## Story-reference comment cleanup

Stripped bare `Story 5.1` attribution from the package-level algorithm comment (line 19 of the original file). Kept substantive aspect-shape documentation throughout. Removed story numbers from `ExampleEntry` and `DDLAspects` godoc; kept factual descriptions.

## Test changes

- `internal/aiagent/traversal_test.go` — `seedDDLAspects` now seeds the vertex key (for F-007 liveness pre-check) plus `.script` and `.permittedCommands` aspects; added `seedDDLAspectsFull` variant for callers needing explicit values.
- `TestReadDDLAspects_HappyPath` — updated to use `seedDDLAspectsFull` and assert `Script` + `PermittedCommands` on returned `DDLAspects`.
- `TestReadDDLAspects_MissingAspect` — seeds vertex key so liveness pre-check passes; still expects `ErrAspectMissing` when inputSchema and remaining aspects are absent.
- Added `TestDiscoverDDL_DuplicateCanonicalName` — seeds two live meta-vertices with the same canonicalName; asserts non-`ErrDDLNotFound` error containing "inconsistent".

## Build / test results

- `go build ./...` — PASS
- `go vet -unreachable=false $(go list ./... | grep -v 'internal/refractor/ruleengine/full/cypher')` — PASS
- `go test ./internal/aiagent/... -p 1 -count=1` — PASS (14 tests)
  - `TestFR19_ColdStartAIAgentTraversal` continues to pass; north-star test uses CreateMetaVertex through the Processor which writes the vertex key and all nine aspects, so F-007 pre-check and F-001 new fields are both satisfied.
