# AI-Agent Contract CR Report — Phase 1.5

## Summary

- **Component:** `internal/aiagent/` (cold-start traversal contract)
- **Files reviewed:** `traversal.go` (in-scope); `traversal_test.go`, `fr19_northstar_test.go`, `gate4_rollback_test.go` (consulted)
- **No-history comments:** Several `// Story 5.x` cross-references found in test files; two in `traversal.go` lines 19, 160, 192, 202, 220 — these are story attribution comments acceptable under project convention (they describe current design, not history).
- **P0 findings:** 1
- **P1 findings:** 3
- **P2 findings:** 4
- **Nit findings:** 2
- **Dismissed:** 2

---

## No-History Comment Audit

```
internal/aiagent/traversal.go:19:    seeded by Story 5.1 (description, inputSchema, outputSchema,
internal/aiagent/traversal.go:45:// aspect (Story 5.1 shape: {"examples": [{"name":...,"payload":...,"expectedOutcome":...}]}).
internal/aiagent/traversal.go:53:// seeded by Story 5.1. These give an AI agent (or any traverser) full
internal/aiagent/traversal.go:160:        // Story 5.3: guard against tombstoned meta-vertices.
internal/aiagent/traversal.go:192:// self-description aspect (Story 5.3). The Processor never reads or
internal/aiagent/traversal.go:202:    // Aspect envelope shape (Story 5.1 convention):
internal/aiagent/traversal.go:220:// ReadDDLAspects reads the five self-description aspects seeded by Story 5.1
```

**Assessment:** All occurrences describe current design intent (aspect shape convention, guard rationale). None say "Previously", "Replaces", or "Was X". No violations.

---

## P0 Findings

### [F-001] `ReadDDLAspects` omits `script` and `permittedCommands` aspects — AI agent cannot construct nor validate operations

**Source:** Acceptance Auditor + Blind Hunter  
**File:** `internal/aiagent/traversal.go:220–300`, `DDLAspects` struct lines 56–76  
**Severity:** P0 — spec gap; contract incompleteness

**What:** The CR brief specifies that Story 5.1 defines 7 self-description aspects for DDL meta-vertices: `canonicalName`, `script`, `permittedCommands`, `inputSchema`, `outputSchema`, `fieldDescription`, `examples`. Bootstrap (`internal/bootstrap/meta_ddl.go`) writes all of these plus `.compensation` (8 + vertex = 9 mutations). `ReadDDLAspects` reads and returns only 5: `description`, `inputSchema`, `outputSchema`, `fieldDescription`, `examples`. The `script` and `permittedCommands` aspects are neither read nor exposed in `DDLAspects`.

**Why it matters for an AI agent:**
- `script` contains the Starlark source. An agent (or a human developer) who wants to understand the execution semantics of an operation — what it actually does, what side effects to expect, what it fails on — has no way to retrieve the script through the Traverser API. They must bypass the Traverser and call `KVGet` directly with a constructed key.
- `permittedCommands` lists the command names that trigger the DDL. An agent constructing an operation envelope must set `Class` to a permitted command. Without reading `permittedCommands`, the agent either hard-codes the class (brittle) or cannot construct a valid envelope.
- The `fr19_northstar_test.go` test sets `Class: canonicalName` as a convention (line 218) — this works for that test's seeded DDL but is not the general rule. The `permittedCommands` aspect exists precisely because a DDL can have multiple permitted command names and the canonical name is not always one of them.

**Evidence:** `DDLAspects` struct (lines 56–76) has no `Script` or `PermittedCommands` fields. `ReadDDLAspects` (lines 227–299) makes exactly 5 `KVGet` calls: `.description`, `.inputSchema`, `.outputSchema`, `.fieldDescription`, `.examples`.

**Suggested fix:** Add `Script string` and `PermittedCommands []string` fields to `DDLAspects`. In `ReadDDLAspects`, add reads for `.script` (extract `data.source`) and `.permittedCommands` (extract `data.commands`). Both are required aspects per Bootstrap — treat missing the same as any other required aspect (return `ErrAspectMissing`). The doc comment on `ReadDDLAspects` also needs updating from "five" to "seven".

---

## P1 Findings

### [F-002] `ReadDDLAspects` does not check `isDeleted` on aspect envelopes — returns data from tombstoned aspects silently

**Source:** Edge Case Hunter + Blind Hunter  
**File:** `internal/aiagent/traversal.go:230–298`  
**Severity:** P1 — silent wrong return; inconsistent with `ReadCompensation` which does check `isDeleted`

**What:** Every aspect read in `ReadDDLAspects` (description, inputSchema, outputSchema, fieldDescription, examples) unmarshals the aspect envelope but never inspects `aspDoc.IsDeleted`. If Bootstrap or a future operation writes an aspect with `isDeleted: true` — e.g., during a partial Bootstrap F-007 anomaly, or a future "tombstone-aspect" operation — `ReadDDLAspects` silently returns the (stale or zeroed) data. The caller has no way to detect this.

**Contrast:** `ReadCompensation` (lines 204–217) explicitly checks `aspDoc.IsDeleted` and returns `ErrCompensationAspectMissing` if the aspect is tombstoned. The inconsistency means the contract is checked for `.compensation` but not for the five payload-facing aspects.

**Bootstrap F-007 scenario:** If Bootstrap writes the vertex + some but not all aspects in one atomic batch and then one aspect is later soft-deleted (e.g., via a future correction op that sets `isDeleted: true` on the aspect key without removing the key), `ReadDDLAspects` will return a `DDLAspects` with empty/zero fields for that aspect and no error.

**Suggested fix:** In each aspect unmarshal block, add an `isDeleted` field to the anonymous struct and check it after unmarshal. Return `ErrAspectMissing` (wrapping the aspect name) if `isDeleted` is true. Pattern already exists in `ReadCompensation`; replicate it.

**Example for description aspect:**
```go
var descDoc struct {
    IsDeleted bool                           `json:"isDeleted"`
    Data      struct{ Text string `json:"text"` } `json:"data"`
}
if err := json.Unmarshal(descEntry.Value, &descDoc); err != nil { ... }
if descDoc.IsDeleted {
    return nil, fmt.Errorf("%w: description at %s: aspect is tombstoned", ErrAspectMissing, ddlKey)
}
```

---

### [F-003] `DiscoverDDL` returns non-deterministically when multiple live meta-vertices share a `canonicalName`

**Source:** Edge Case Hunter  
**File:** `internal/aiagent/traversal.go:128–181`  
**Severity:** P1 — silent wrong answer; silent data integrity assumption

**What:** `DiscoverDDL` iterates `KVListKeys` (order "unspecified" per `kv.go:108`) and returns the first meta-vertex whose `.canonicalName` aspect matches `operationType`. If two live meta-vertices share the same `canonicalName` (e.g., Bootstrap F-007: two CreateMetaVertex ops for the same name both committed due to a race; or a partial rollback left the original live), the function returns whichever happens to come first in the key list — non-deterministically across calls.

**Why it matters:** An AI agent would receive different `ddlKey` values across calls for the same `operationType`. Since `ReadDDLAspects` is then called with the returned key, the agent could get different schemas on different invocations. Operations could be constructed from the wrong schema.

**No test covers this.** `TestDiscoverDDL_HappyPath` seeds two vertices with *different* names.

**Suggested fix options:**
1. After the loop, if more than one match was found, return an explicit error: `"aiagent: multiple live DDLs with canonicalName %q; cell is in inconsistent state"`.
2. (Stricter) On first match, continue scanning to detect duplicates and error on any second match.

Option 1 requires accumulating matches rather than returning early. Given that Core KV is expected to be consistent, returning an error for the duplicate case is preferable to silent non-determinism.

---

### [F-004] `ReadCapability` does not surface capability doc staleness — silently returns outdated permissions

**Source:** Acceptance Auditor  
**File:** `internal/aiagent/traversal.go:105–116`  
**Severity:** P1 — operational trap; diverges from stated NFR-S10 intent

**What:** The Processor enforces `AuthFreshnessExceeded` via `step3_auth_capability.go` — it rejects operations whose capability doc's `projectedAt` is older than the configured ceiling. However, `ReadCapability` returns the raw `CapabilityDoc` with no staleness check. An AI agent that calls `ReadCapability`, waits before submitting (due to schema discovery, retries, or latency), and then submits an operation will be rejected by the Processor with `AuthFreshnessExceeded` — an error the agent has no way to anticipate from the Traverser output alone.

**Worse:** The Traverser has no clock injection and no knowledge of the Processor's configured freshness ceiling. The agent cannot check the staleness itself without knowing the ceiling, which is a deployment configuration value.

**Why this matters for AI agents specifically:** Human actors typically have short-lived sessions. AI agents may cache capability docs for longer periods or retry with delays. The gap between "Traverser says this is your cap" and "Processor rejects because cap is stale" is a real operational failure mode.

**Suggested fix (two-tier):**
1. **Minimum:** Document this limitation clearly in the `ReadCapability` godoc. Add a `projectedAt` accessor note: "Callers in latency-sensitive paths should check `doc.ProjectedAt` against their clock before submitting operations; the Processor will reject operations whose capability doc exceeds the deployment's freshness ceiling."
2. **Better (future):** Accept an optional `maxAge time.Duration` parameter (or a `TraverserOpts` struct) and return a staleness warning if `projectedAt` is older than `maxAge`. This keeps the check optional and doesn't require the Traverser to know the deployment ceiling.

---

## P2 Findings

### [F-005] Double `%w` in error formatting (`errors.Join` semantics) — used pervasively but may surprise callers using `errors.As`

**Source:** Blind Hunter  
**File:** `internal/aiagent/traversal.go:199, 233, 246, 259, 272, 287`  
**Severity:** P2 — non-obvious contract; Go 1.20+ semantics

**What:** All error returns in `ReadCompensation` and `ReadDDLAspects` use the pattern `fmt.Errorf("%w: ...: %w", SentinelErr, key, underlyingErr)`. In Go 1.20+ this wraps both errors in the chain (via `errors.Join`-like semantics), so `errors.Is(err, ErrAspectMissing)` AND `errors.Is(err, substrate.ErrKeyNotFound)` both return true. This is intentional — callers can distinguish sentinel from substrate error.

**The issue:** This pattern is undocumented in the package. A caller using `errors.Is(err, substrate.ErrKeyNotFound)` to detect a missing aspect key would get a true match, but would not know *which* aspect was missing without inspecting the error string. More critically: the call chain does not document that `substrate.ErrKeyNotFound` propagates up through `ErrAspectMissing` errors, so callers may not think to check for it.

**No test covers the `errors.Is(err, substrate.ErrKeyNotFound)` path for `ReadDDLAspects`** — only `ErrAspectMissing` is tested in `TestReadDDLAspects_MissingAspect`.

**Suggested fix:** Add a godoc note to `ErrAspectMissing` and `ErrCompensationAspectMissing` explaining the dual-sentinel error chain. Example: "When caused by a missing KV key, the error also wraps `substrate.ErrKeyNotFound`; use `errors.Is` to check for either."

---

### [F-006] `NewTraverser` accepts empty bucket names with no validation

**Source:** Blind Hunter  
**File:** `internal/aiagent/traversal.go:90–96`  
**Severity:** P2 — poor failure mode; deferred error surfacing

**What:** `NewTraverser(conn, "", "")` constructs successfully. All subsequent operations will fail at the substrate layer with confusing bucket-not-found errors mentioning the empty bucket name. The contract is "both names must match deployment's bucket provisioning" (line 88) but is unenforced.

**Suggested fix:**
```go
func NewTraverser(conn *substrate.Conn, coreBucket, capBucket string) *Traverser {
    if conn == nil {
        panic("aiagent: NewTraverser: conn must not be nil")
    }
    if coreBucket == "" || capBucket == "" {
        panic("aiagent: NewTraverser: bucket names must not be empty")
    }
    return &Traverser{conn: conn, coreBucket: coreBucket, capBucket: capBucket}
}
```
Panics at construction are appropriate here (programming error, not runtime error). Alternatively return an error, but the current API doesn't.

---

### [F-007] `ReadDDLAspects` does not validate that the DDL meta-vertex itself is live before reading aspects

**Source:** Edge Case Hunter  
**File:** `internal/aiagent/traversal.go:227–300`  
**Severity:** P2 — incorrect caller contract; tombstoned DDL aspects visible

**What:** `ReadDDLAspects` accepts any `ddlKey` string and reads its aspects directly. It performs no check that the vertex at `ddlKey` is live (i.e., `isDeleted: false`). If called directly (bypassing `DiscoverDDL`) with a tombstoned meta-vertex key, or if a DDL is tombstoned between `DiscoverDDL` and `ReadDDLAspects` (TOCTOU), the method returns the aspects of a deleted DDL without error.

**TOCTOU window:** In a concurrent environment, `DiscoverDDL` could verify liveness and return a key, then `TombstoneMetaVertex` commits, then `ReadDDLAspects` reads aspects of the now-deleted DDL. The probability is low but non-zero. An AI agent would then construct a payload for an operation type that is no longer registered — the Processor will reject it with `UnknownOperationType`.

**Suggested fix:** Add a liveness pre-check at the start of `ReadDDLAspects`:
```go
vtxEntry, err := t.conn.KVGet(ctx, t.coreBucket, ddlKey)
if err != nil {
    return nil, fmt.Errorf("%w: vertex at %s: %w", ErrAspectMissing, ddlKey, err)
}
var vtxDoc struct{ IsDeleted bool `json:"isDeleted"` }
if err := json.Unmarshal(vtxEntry.Value, &vtxDoc); err != nil || vtxDoc.IsDeleted {
    return nil, fmt.Errorf("aiagent: DDL %s is tombstoned", ddlKey)
}
```
This adds one KVGet but closes the TOCTOU window and makes `ReadDDLAspects` safe to call as a standalone API.

---

### [F-008] `DiscoverDDL` O(N) full-scan on every call — no caching, no prefix filter

**Source:** Blind Hunter + Edge Case Hunter  
**File:** `internal/aiagent/traversal.go:128–181`  
**Severity:** P2 — performance; not a correctness bug

**What:** Every `DiscoverDDL` call:
1. Calls `KVListKeys` which returns ALL keys in Core KV — including vertex keys, aspect keys, tracker keys, event keys.
2. Iterates every key, filters 3-segment `vtx.meta.*` ones.
3. For each candidate: one `KVGet` for `.canonicalName`, then on match one more `KVGet` for the vertex tombstone check.

Core KV in production will have thousands of keys (all vertices + all aspects + all trackers). `KVListKeys` comment (substrate/kv.go:108) acknowledges "heavy on large buckets" and notes "meta-vertex sub-set qualifies" — but by the time an AI agent calls `DiscoverDDL`, Core KV may contain many thousands of non-meta keys.

**No cache exists.** The `Traverser` struct has no DDL cache. The Processor's `ddl_cache.go` caches meta-vertices at startup; the Traverser has no equivalent.

**Contrast:** `internal/processor/ddl_cache.go:93` also calls `KVListKeys` — but once at startup, then subscribes to changes. The Traverser does this on every `DiscoverDDL` call.

**Suggested fix:** NATS KV supports key prefix listing. If the substrate exposes a `KVListKeysByPrefix(ctx, bucket, prefix)` method, `DiscoverDDL` should use `"vtx.meta."` as the prefix to limit the scan to meta-vertex keys only. This reduces the scan from O(all Core KV keys) to O(meta-vertices). A short-lived TTL cache within the Traverser is also worth considering for repeated calls.

---

## Nit Findings

### [N-001] `ReadDDLAspects` godoc says "five self-description aspects" — will be wrong when F-001 is fixed

**File:** `internal/aiagent/traversal.go:220`  
**What:** The godoc says "five self-description aspects seeded by Story 5.1". After adding `script` and `permittedCommands` (F-001 fix) it will be seven. Update when fixing.

---

### [N-002] Inline anonymous structs repeated across `ReadDDLAspects` — could be typed

**File:** `internal/aiagent/traversal.go:235–238, 248–251, etc.`  
**What:** Each aspect unmarshal uses a unique anonymous struct. If `isDeleted` checks are added (F-002 fix), each anonymous struct gains an `IsDeleted bool` field that must be added six times. Defining a small set of named types (e.g., `aspectEnvelope[T]`) would reduce the repetition. Not a bug — purely maintainability.

---

## Dismissed Findings

- **D-001 (dismissed):** `ReadCapability` passes `actorID` directly into the key string `"cap.identity." + actorID`. A malformed `actorID` with embedded dots could produce unintended key paths. Dismissed: Lattice NanoIDs are 21-character alphanumeric strings with no dots; the actor key comes from the capability doc's `actor` field, which is already validated upstream at identity creation. Not an injection risk in the current key schema.

- **D-002 (dismissed):** No context cancellation short-circuit in the `DiscoverDDL` loop — if the context is cancelled, the loop continues until the next `KVGet` call propagates the cancellation. The loop body does not check `ctx.Err()` explicitly. Dismissed: each `KVGet` call respects the context, so cancellation propagates at the next IO boundary. Adding an explicit `ctx.Err()` check between iterations would be marginally faster to cancel but is not a correctness issue.

---

## Coverage Assessment

| Method | Happy path | Missing key | Tombstoned vertex | Tombstoned aspect | Duplicate canonicalName |
|---|---|---|---|---|---|
| `ReadCapability` | ✓ | ✓ | n/a | n/a | n/a |
| `DiscoverDDL` | ✓ | ✓ | ✓ | ✓ (canonicalName only) | ✗ missing |
| `ReadDDLAspects` | ✓ | ✓ (first missing aspect) | ✗ not checked | ✗ not checked | n/a |
| `ReadCompensation` | ✓ | ✓ | n/a | ✓ | n/a |

**Gap:** No test exercises `DiscoverDDL` with two live vertices sharing a `canonicalName` (F-003). No test exercises `ReadDDLAspects` with a tombstoned vertex (F-007) or a tombstoned aspect (F-002).

---

## Spec Alignment Summary (Story 5.1 / 5.3)

| Aspect | Bootstrap writes | `ReadDDLAspects` reads | Gap |
|---|---|---|---|
| `.canonicalName` | ✓ | Used by `DiscoverDDL` | ✓ accessible indirectly |
| `.script` | ✓ | ✗ | **F-001** |
| `.permittedCommands` | ✓ | ✗ | **F-001** |
| `.description` | ✓ | ✓ | — |
| `.inputSchema` | ✓ | ✓ | — |
| `.outputSchema` | ✓ | ✓ | — |
| `.fieldDescription` | ✓ | ✓ | — |
| `.examples` | ✓ | ✓ | — |
| `.compensation` | ✓ | Via `ReadCompensation` | ✓ separate method |
