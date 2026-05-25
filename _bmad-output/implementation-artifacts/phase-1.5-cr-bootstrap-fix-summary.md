# Bootstrap/Kernel CR Fix Summary — Phase 1.5

## Applied fixes

| ID | File:Line (after edit) | Description |
|---|---|---|
| F-001 | `internal/bootstrap/nanoid.go:138–209` | Added `BootstrapFile.Status` field (version "4"); `LoadOrGenerate` now writes status="in-progress" before `SeedPrimordial` and detects it on crash-recovery reload; `PersistCommitted` rewrites with status="committed" after seeding succeeds |
| F-001 | `cmd/bootstrap/main.go:81–100` | Updated main flow to call `PersistCommitted` instead of `Persist`; updated package doc comment to describe two-phase commit protocol |
| F-003 | `internal/bootstrap/verify.go:65–156` | Replaced existence-only aspect checks with full envelope validation: JSON valid, key echo, class match, isDeleted=false, vertexKey matches parent vertex |
| F-003 | `scripts/verify-kernel.go:118–270` | Same aspect envelope validation added to standalone script, including class checks for all 9 meta-DDL aspects, 6 aspect-type aspects × 5 vertices, 2 role aspects, 5 lens aspects × 2 lenses |
| F-004 | `internal/bootstrap/primordial.go:709–713` | Replaced `_ = ok` with explicit error return on `strings.Cut` boolean for `addLensAspects` |
| F-005 | `internal/bootstrap/nanoid.go:220–235` | Added `checkVersion()` called from both `Load()` and the file-exists branch of `LoadOrGenerate()`; returns clear "bootstrap file version mismatch: got X, want '3' or '4' — run make down && make up" message |
| F-006 | `internal/bootstrap/primordial.go:31–42` | Deleted `OpsMetaStreamName` and `OpsMetaSubject` constants; updated constants block comment |
| F-008 | `internal/bootstrap/primordial.go:276–284` | Added `errors.Is(err, jetstream.ErrKeyExists)` sentinel check as first branch of `looksLikeCreateConflict`; also updated `seedPrimordialPerKey` to call `looksLikeCreateConflict` instead of inline string checks |
| N-001 | `internal/bootstrap/primordial.go:170–202` | Updated `SeedPrimordial` doc comment: "≈ 34" → "≈ 69", rewrote composition enumeration to include aspect-type vertices and their aspects |
| N-002 | `internal/bootstrap/primordial.go:108` | Changed "Provision core-operations and ops.meta streams" → "Provision core-operations and core-events streams" |
| N-003 | `internal/bootstrap/primordial.go:809–822` | Added immediate `kv.Get` check before the ticker loop in `WaitForBootstrapComplete` |
| N-004 | `internal/bootstrap/primordial.go:237–239` | Added comment documenting the 30s hardcoded timeout and lack of ctx propagation on `AtomicBatch` |

## History comments sweep (27 instances)

| File | Comment excerpt removed/rewritten |
|---|---|
| `primordial.go:1` | "for Story 1.3" removed from package doc |
| `primordial.go:28` | "Story 2.1:" removed from `RefractorAdjacencyKV` comment |
| `primordial.go:37–38` | P2: CONTRACT-AMENDMENT-REQUEST narration replaced with current-state explanation of `ops.>` subject |
| `primordial.go:42` | "Story 1.8:" removed from `EventsWildcardSubject` |
| `primordial.go:87` | "Story 1.1 spike finding" removed from LimitMarkerTTL comment |
| `primordial.go:99` | "Per Story 1.1 spike:" removed from AllowAtomicPublish comment |
| `primordial.go:146` | "Story 1.8:" removed from core-events stream comment |
| `primordial.go:195–200` | P2: "Story 1.4 refactor" narration replaced with current-behavior description of AtomicBatch |
| `primordial.go:350` | "Story 5.1: 4 additional" comment removed |
| `primordial.go:402` | "Story 5.3: seed the .compensation aspect" rewritten as current-state description |
| `primordial.go:438` | "Story 5.1:" removed from aspect-type meta-vertices comment |
| `primordial.go:473` | "once Story 5.3's ops-routed installer arrives" replaced with current-state description |
| `primordial.go:522` | "for Story 5.1" removed from `seedAspectTypeMeta` doc comment |
| `primordial.go:678` | "Story 3.2a:" removed from `addLensAspects` doc comment |
| `primordial.go:706` | "Story 3.2a Phase D" removed from spec aspect comment |
| `primordial.go:745` | "Story 3.2b verifies…" reference removed from key field comment |
| `primordial.go:773–775` | P2: "Historically… Story 2.1…Resolution:" changelog replaced with: "cmd/bootstrap writes this marker itself because it is the only process guaranteed to run after primordial seeding completes" |
| `envelope.go:12` | "which matters for the bypass test oracle in Story 1.10" removed |
| `envelope.go:19–21` | P2: "Story 1.4 refactor: this function was the bespoke envelope formatter prior to…" prior-behavior narration removed |
| `nanoid.go:27–29` | P2: "Story 4.7 trim: …moved to their respective Capability Packages" replaced with current-state description |
| `nanoid.go:54` | "Story 5.3" removed from `CompensationAspectClass` comment |
| `nanoid.go:82–85` | P2: "Story 4.7" and "once Story 5.3 routes installs…" references removed |
| `nanoid.go:93–95` | "Story 5.1:" removed from aspect-type NanoIDs comment |
| `nanoid.go:113–116` | P2: migration concern narration replaced with current-state description |
| `meta_ddl.go:3` | "Story 4.7 —" removed from package comment |
| `meta_ddl.go:35–49` | P2: "Story 5.3 additions" doc block and "rejected compensatingOperation…" design alternative narration replaced with current-behavior description |
| `meta_ddl.go` Starlark inline | All `# Story 5.3:`, `# SC-1 (Story 5.3)`, `# MF-2 (Story 5.3)` annotations removed/rewritten as current-state comments |

## Deferred findings

| ID | Reason |
|---|---|
| F-002 | Deferred per mandate (carved into own Phase 1.5 story) |
| F-007 | Deferred per mandate (will pair with B3 Processor cache eviction) |

## Build/test results

- `go build ./...` — PASS
- `go vet -unreachable=false $(go list ./... | grep -v 'internal/refractor/ruleengine/full/cypher')` — pre-existing failures in `adjacency` test files (confirmed present on base branch before these changes); bootstrap packages pass vet cleanly
- `go test ./internal/bootstrap/... -p 1 -count=1` — PASS (0.715s)
- `make verify-kernel` — not run (Docker stack not up; not brought up per mandate)
