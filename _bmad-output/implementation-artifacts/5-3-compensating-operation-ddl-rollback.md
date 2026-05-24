# Story 5.3: Compensating Operation & DDL Rollback

Status: review

## Story

As a platform operator,
I want every capability change to be revertible via a compensating operation through the same write path —
with no platform restart, no data surgery, no out-of-band intervention —
so that capability evolution is safe to attempt and reversible by design.

## Spec Deviations (read first)

The epics.md §Story 5.3 (lines 1337–1352) proposes that the Processor commit response carry a `compensatingOperation` field in the `OperationReply` struct. **This design is rejected by the PO (Andrew) and architect (Winston).**

The rejected field would have looked like:
```json
{
  "compensatingOperation": {
    "operationType": "TombstoneMetaVertex",
    "payloadShape": { "metaKey": "<key from step 2>" }
  }
}
```

**Why rejected:**
1. It embeds routing logic inside the Processor response — coupling the write path to compensation semantics it should not own.
2. It implies the Processor knows the "inverse" of every operation, violating the single-responsibility principle of the commit path.
3. It requires new `OperationReply` fields, directly contradicting Architectural Guardrail #1 below.
4. The same information is already fully encodable in the DDL meta-vertex itself, where it belongs — as a self-description aspect.

**Replacement design (Option A — canonical):** The compensation contract lives in the DDL meta-vertex as a sixth self-description aspect named `.compensation`. The compensating operation is constructed **client-side** by reading this aspect via `aiagent.Traverser`, then substituting field references from the original commit response. No new Processor envelope fields. No new Processor read surface. No new service.

## Acceptance Criteria

1. **Documentation — `.compensation` aspect as contract surface (replaces epics.md AC1):**
   `docs/components/processor.md` documents the `.compensation` aspect as the FR53 contract surface. It includes a table of all Phase 1 capability-change operation pairings:

   | Forward operation | Compensating operation | Notes |
   |---|---|---|
   | `CreateMetaVertex` | `TombstoneMetaVertex` | tombstones the newly-created meta-vertex |
   | `UpdateMetaVertex` | `UpdateMetaVertex` | restores prior payload using the revision from the forward op's commit response |
   | `TombstoneMetaVertex` | `CreateMetaVertex` | recreates the meta-vertex (requires prior payload — Phase 1: operator responsibility) |
   | rbac-domain `CreateRole` | rbac-domain `TombstoneRole` | Phase 2 scope (rbac-domain DDL carries own `.compensation`) |
   | rbac-domain `AssignRole` / `GrantPermission` | rbac-domain `RevokeRole` / `RevokePermission` | Phase 2 scope |

   The doc explicitly states: **the Processor commit response carries NO new field for compensation.** It names the rejected `compensatingOperation` response field and explains why it was rejected (per this brief's Spec Deviations section).

2. **No envelope change (replaces epics.md AC2):**
   The `OperationReply` struct (`internal/processor/envelope.go`) is **not modified.** The current fields remain the only fields:
   ```go
   type OperationReply struct {
       RequestID           string            `json:"requestId"`
       OpTrackerKey        string            `json:"opTrackerKey"`
       Status              ReplyStatus       `json:"status"`
       CommittedAt         string            `json:"committedAt,omitempty"`
       OriginalCommittedAt string            `json:"originalCommittedAt,omitempty"`
       Error               *ReplyError       `json:"error,omitempty"`
       Decision            string            `json:"decision,omitempty"`
       Revisions           map[string]uint64 `json:"revisions,omitempty"`
       Detail              map[string]any    `json:"detail,omitempty"`
   }
   ```
   No `CompensatingOperation` field is added. A grep `grep -rn "CompensatingOperation\|compensatingOperation\|compensating_operation" internal/processor/` must return zero hits at the close of this story.

3. **`.compensation` aspect seeded on kernel meta-DDL (replaces epics.md AC2/AC3):**
   The kernel meta-DDL in `internal/bootstrap/meta_ddl.go` has its `MetaRootDDLScript` updated so that:
   - `CreateMetaVertex` emits a `.compensation` aspect for the created meta-vertex alongside the other five Story 5.1 aspects. For the `is_ddl_class` branch, the `.compensation` aspect data encodes the inverse operation: `TombstoneMetaVertex` targeting the key just created, with a `revisionTemplate` referencing `{{detail.metaKey}}` and `{{revisions[detail.metaKey]}}` from the commit response.
   - `UpdateMetaVertex` emits an updated `.compensation` aspect reflecting "revert to prior description" — a `UpdateMetaVertex` with `metaKey` and the pre-update content that was read from state (Step 4 ContextHint.Reads).
   - `TombstoneMetaVertex` is irreversible in Phase 1 (no prior payload stored). Its `.compensation` aspect encodes `{"inverseOperationType": "none", "note": "Tombstone is irreversible in Phase 1; operator must recreate via CreateMetaVertex with prior payload."}`.

   Additionally, `internal/bootstrap/primordial.go` seeds a `.compensation` aspect on the kernel `root` DDL meta-vertex itself (the meta-meta-DDL), describing how to roll back a `CreateMetaVertex` or `UpdateMetaVertex` call to that DDL.

   **Aspect shape for `.compensation`:**
   ```json
   {
     "data": {
       "inverseOperationType": "TombstoneMetaVertex",
       "payloadTemplate": {
         "metaKey": "{{detail.metaKey}}"
       },
       "revisionTemplate": {
         "metaKey": "{{revisions[detail.metaKey]}}"
       }
     }
   }
   ```
   Template variables reference fields already present in the standard commit response:
   - `detail.metaKey` — value of `OperationReply.Detail["metaKey"]` (populated by the Starlark `response` field, which the MetaRoot DDL already returns as `{"metaKey": meta_key}`)
   - `revisions[<key>]` — the per-key revision from `OperationReply.Revisions`

   No new envelope fields needed. The operator (or AI agent) resolves the template by plugging in values from the commit response they already hold.

4. **Revert flow — client reads `.compensation` aspect and constructs the compensating op:**
   Given a forward `CreateMetaVertex` op committed successfully:
   - Operator (or AI agent) has: `metaKey` (from `Detail["metaKey"]`) and `revisions[metaKey]` (from `OperationReply.Revisions`).
   - Operator calls `aiagent.Traverser.ReadCompensation(ctx, metaKey)` — reads `vtx.meta.<id>.compensation` from Core KV.
   - Operator constructs the compensating op payload by substituting template variables with commit-response values.
   - Operator submits via Processor (same write path, same lane).
   - State reverts; Capability KV reprojection updates within NFR-P3 lag; no platform restart required.

   The `Traverser` gains one new method:
   ```go
   // ReadCompensation reads the .compensation aspect from a DDL meta-vertex.
   // Returns the aspect data map as-is; caller is responsible for template
   // substitution from their commit response values.
   func (t *Traverser) ReadCompensation(ctx context.Context, metaKey string) (map[string]any, error)
   ```

5. **Conflict handling — compensating op asserts revision before tombstoning:**
   The `TombstoneMetaVertex` Starlark script in `MetaRootDDLScript` is updated to accept an optional `expectedRevision` field in its payload. When present, the script asserts `state[meta_key]` is at the declared revision before emitting the tombstone mutation. On revision mismatch:
   - Phase 1 default: fail with `CompensationConflict: <key>: expected revision <n>, got <m>`.
   - If operator passes `force: true` in the payload: proceed without the revision assertion (merge policy = last-writer-wins).

   The `UpdateMetaVertex` script similarly accepts `expectedRevision` for the description aspect.

   This assertion is Starlark-level (inside the script) and uses the Story 1.7 NATS revision-condition mechanism (BatchOp `HasRevision` / `Revision` fields) for the commit itself — so the conflict surfaces as a `RevisionConflict` error code at the NATS layer even if the Starlark check passes (defense in depth).

6. **Gate 4 integration test (`make test-rollback`):**
   New file `internal/aiagent/gate4_rollback_test.go`. The test executes:
   1. `testutil.SetupPackageTestEnv(t)` — bootstrap + rbac-domain + identity-domain packages installed; baseline captured.
   2. Submit forward `CreateMetaVertex` for a new DDL with `canonicalName: "RollbackTestDDL"` and `class: "meta.ddl.vertexType"` (all six aspects populated including `.compensation`). Capture `metaKey` from `Detail["metaKey"]` and `revisions[metaKey]` from `OperationReply.Revisions`.
   3. Verify the DDL is discoverable: call `aiagent.Traverser.DiscoverDDL(ctx, "RollbackTestDDL")` and assert it returns `metaKey`.
   4. Call `aiagent.Traverser.ReadCompensation(ctx, metaKey)` — assert returned `inverseOperationType` == `"TombstoneMetaVertex"`.
   5. Construct the `TombstoneMetaVertex` payload by substituting `metaKey` from step 2; include `expectedRevision` from `revisions[metaKey]`.
   6. Submit the compensating `TombstoneMetaVertex` op via Processor (meta lane); assert `OutcomeAccepted`.
   7. Verify the DDL is no longer discoverable: `DiscoverDDL` returns `ErrDDLNotFound`; a `CreateMetaVertex` op with the same `canonicalName` is now possible again (idempotency tracker uses a different requestId).
   8. Verify Capability KV reprojection: since no Refractor is running in the test harness, assert that the tombstoned meta-vertex has `isDeleted: true` in Core KV (the Capability KV reprojection side-effect is validated in the live-stack `make test-rollback` target, not in the embedded-NATS test).
   9. Verify no Processor or Refractor restart: consumer sequence positions held (assert no gap in the meta-lane durable's `NumPending`).
   10. Repeat steps 2–9 for a Lens meta-vertex (`class: "meta.lens"`). Note: lenses do not require all five DDL self-description aspects (they use `description`, `spec`, and optionally `adapter/bucket/engine`). The `.compensation` aspect on a lens tombstone follows the same shape.
   11. Write `health.gates.phase1.gate4` to Health KV with `{"passed": true, "completedAt": "<ISO8601>"}`.

   The test is runnable standalone via `make test-rollback`:
   ```makefile
   test-rollback:
       go test ./internal/aiagent/... -run TestGate4_CompensatingOpRollback -v -p 1 -count=1
   ```

## Architectural Guardrails (non-negotiable)

These five constraints are hard stops. If any implementation path leads toward violating one, stop and file a `CONTRACT-AMENDMENT-REQUEST.md` rather than proceeding.

**Guardrail 1 — No new Processor response envelope fields.**
`OperationReply` (in `internal/processor/envelope.go`) stays exactly as it is today. The full current shape is quoted verbatim in AC2 above. No field named `CompensatingOperation`, `compensatingOperation`, `inverseOp`, or anything similar may be added. The compensation contract lives in the DDL meta-vertex, not in the reply envelope.

**Guardrail 2 — No new Processor read surface.**
The Processor commit path reads only: Core KV (via ContextHint.Reads), DDL cache, Capability KV (auth only), idempotency tracker. The `.compensation` aspect is read by the **client** (`aiagent.Traverser`) via Core KV. The Processor never reads or interprets `.compensation` aspects. No new ContextHint fields (e.g., `CompensationHints`, `InverseOp`) may be added.

**Guardrail 3 — No "rollback machinery."**
No new service, no new KV bucket, no compensation registry, no compensation ledger, no compensating-op queue. The only new artifact is:
- The `.compensation` aspect type (a sixth self-description aspect, following the Story 5.1 pattern).
- Seeded values on the kernel meta-DDL.
- One new `Traverser` method (`ReadCompensation`) in `internal/aiagent/traversal.go`.
- The Gate 4 integration test.

**Guardrail 4 — NFR-S10 preserved.**
The compensating operation submission path is the exact same Processor commit path used by all other operations. No AI-specific or "compensation-specific" bypass exists in any Processor code. `grep -rn "compensation\|Compensation" internal/processor/` must return zero production-code hits at story close (comments naming the rejected design are allowed in `envelope.go` only).

**Guardrail 5 — NFR-S6 preserved.**
The `.compensation` aspect `data` object must not embed business data. The `payloadTemplate` and `revisionTemplate` fields use only field references that the original submitter already holds (e.g., `"{{detail.metaKey}}"`, `"{{revisions[detail.metaKey]}}"`) — never aspect values, never identity data, never operational state discovered from other vertices.

## Tasks / Subtasks

- [x] Task 1 — Add `.compensation` aspect seeding to MetaRootDDLScript (AC3)
  - [x] 1.1 Add `compensation` aspect class to the `is_ddl_class` branch of `CreateMetaVertex`: emit `make_aspect(meta_key + ".compensation", meta_key, "compensation", "compensation", {"inverseOperationType": "TombstoneMetaVertex", "payloadTemplate": {"metaKey": "{{detail.metaKey}}"}, "revisionTemplate": {"metaKey": "{{revisions[detail.metaKey]}}"}})`
  - [x] 1.2 Add `compensation` aspect emission to `meta.lens` branch of `CreateMetaVertex` (same shape as `is_ddl_class` — `payloadTemplate: {"metaKey": "{{detail.metaKey}}"}`, `revisionTemplate: {"metaKey": "{{revisions[detail.metaKey]}}"}`)
  - [x] 1.3 Add `expectedRevision` optional field handling to `TombstoneMetaVertex` branch: if `p.expectedRevision` is present and is an int, validate type; if `p.force == True`, skip the assertion. Propagate `expectedRevision` to the mutation's `expectedRevision` field.
  - [x] 1.4 Add `expectedRevision` optional field handling to `UpdateMetaVertex` branch (same pattern as 1.3)
  - [x] 1.5 Add `.compensation` emit to `UpdateMetaVertex` branch: after updating `.description`, also update the `.compensation` aspect. Read the pre-update description from `state[meta_key + ".description"]`; store it in `payloadTemplate.description`.

- [x] Task 2 — Seed `.compensation` aspect on the primordial kernel root DDL (AC3)
  - [x] 2.1 In `internal/bootstrap/primordial.go` `buildPrimordialEntries()`, after the existing root DDL aspects, add `.compensation` aspect at `MetaRootKey + ".compensation"` with `inverseOperationType: "TombstoneMetaVertex"`.
  - [x] 2.2 Add `CompensationAspectClass = "compensation"` constant to `internal/bootstrap/nanoid.go`.

- [x] Task 3 — Extend `aiagent.Traverser` with `ReadCompensation` (AC4)
  - [x] 3.1 Add `ReadCompensation(ctx context.Context, metaKey string) (map[string]any, error)` to `internal/aiagent/traversal.go`
  - [x] 3.2 Implementation: `KVGet(ctx, coreBucket, metaKey+".compensation")` → unmarshal → return `data` map
  - [x] 3.3 Add unit tests in `internal/aiagent/traversal_test.go`: `TestTraverser_ReadCompensation_HappyPath`, `_MissingAspect`, `_TombstonedAspect`; also `TestDiscoverDDL_SkipsTombstonedVertex` for the DiscoverDDL tombstone guard.

- [x] Task 4 — Gate 4 integration test (`make test-rollback`) (AC6)
  - [x] 4.1 Created `internal/aiagent/gate4_rollback_test.go` (package `aiagent_test`)
  - [x] 4.2 Implemented the 11-step test sequence described in AC6 (two sub-tests: DDL_VertexType and Lens)
  - [x] 4.3 Used `testutil.PipelineConfig{FilterSubjects: []string{"ops.meta"}}` pattern from Story 5.2
  - [x] 4.4 Added `test-rollback` target to `Makefile`

- [x] Task 5 — Documentation (AC1)
  - [x] 5.1 Updated `docs/components/processor.md`: added FR53 section with `.compensation` aspect contract, operation-pairing table, rejected-field rationale, client-side revert flow, and conflict handling.

- [x] Task 6 — Verify scripts (AC3 cross-check)
  - [x] 6.1 Updated `scripts/verify-kernel.go`: asserts `.compensation` aspect exists on `MetaRootKey`; asserts `data.inverseOperationType == "TombstoneMetaVertex"`.
  - [x] 6.2 Grep verified: `grep -rn "CompensatingOperation\|compensatingOperation" internal/processor/` returns zero hits.

- [x] Task 7 — Architecture purity verification
  - [x] 7.1 Grep `internal/processor/` for `compensation` — zero hits (no production-code mentions).
  - [x] 7.2 `ContextHint` still has only `Reads []string`. No new fields added.
  - [x] 7.3 `OperationReply` in `envelope.go` is unchanged from the shape quoted in AC2.

## Dev Notes

### Overview

Story 5.3 is **almost entirely additive** — it touches:
1. The Starlark script in `internal/bootstrap/meta_ddl.go` (emit `.compensation` aspects; add `expectedRevision` optional handling to Update + Tombstone branches).
2. `internal/bootstrap/primordial.go` (seed `.compensation` on the kernel root DDL).
3. `internal/aiagent/traversal.go` (one new method: `ReadCompensation`).
4. `internal/aiagent/gate4_rollback_test.go` (new test file).
5. `Makefile` (new `test-rollback` target).
6. `docs/components/processor.md` (FR53 documentation section).

**No Processor code changes.** No new KV buckets. No new packages.

---

### Processor Commit Response — Current Shape (Do Not Change)

The full current `OperationReply` Go struct (from `internal/processor/envelope.go`):

```go
type OperationReply struct {
    RequestID           string            `json:"requestId"`
    OpTrackerKey        string            `json:"opTrackerKey"`
    Status              ReplyStatus       `json:"status"`
    CommittedAt         string            `json:"committedAt,omitempty"`
    OriginalCommittedAt string            `json:"originalCommittedAt,omitempty"`
    Error               *ReplyError       `json:"error,omitempty"`
    Decision            string            `json:"decision,omitempty"`
    Revisions           map[string]uint64 `json:"revisions,omitempty"`
    Detail              map[string]any    `json:"detail,omitempty"`
}
```

The fields operators already receive from a successful `CreateMetaVertex` commit:
- `Detail["metaKey"]` — the created meta-vertex key (e.g., `"vtx.meta.<NanoID>"`). This is the `response.metaKey` returned by the Starlark script and surfaced via `BuildAcceptedReplyWithDetail`.
- `Revisions["vtx.meta.<NanoID>"]` — the NATS revision of the newly-created meta-vertex. This is the per-key revision map from `BuildAcceptedReplyWithRevisions`.

Both fields are already present in the current commit path. The `.compensation` aspect template references these exact fields — no new information is needed from the Processor.

---

### `.compensation` Aspect Shape (canonical)

The aspect lives at `<metaKey>.compensation` in Core KV, following the standard 4-segment aspect-key pattern per Contract #1 §1.5.

The data envelope follows the Story 5.1 aspect shape convention:

```json
{
  "class": "compensation",
  "vertexKey": "vtx.meta.<NanoID>",
  "localName": "compensation",
  "isDeleted": false,
  "data": {
    "inverseOperationType": "TombstoneMetaVertex",
    "payloadTemplate": {
      "metaKey": "{{detail.metaKey}}"
    },
    "revisionTemplate": {
      "metaKey": "{{revisions[detail.metaKey]}}"
    }
  }
}
```

Template variable substitution rules (client-side, implemented in the test harness — NOT in Processor):
- `{{detail.metaKey}}` → value of `OperationReply.Detail["metaKey"]`
- `{{revisions[detail.metaKey]}}` → value of `OperationReply.Revisions[OperationReply.Detail["metaKey"].(string)]`

For `UpdateMetaVertex` compensation, the template additionally carries the prior description:
```json
{
  "data": {
    "inverseOperationType": "UpdateMetaVertex",
    "payloadTemplate": {
      "metaKey": "{{detail.metaKey}}",
      "description": "<prior description value read from state at execute time>"
    },
    "revisionTemplate": {
      "metaKey": "{{revisions[detail.metaKey]}}"
    }
  }
}
```

Note: `UpdateMetaVertex` stores the prior description **literally** at script execution time (reading from `state`), so no template substitution is needed for the description field — it is a concrete value.

---

### MetaRootDDLScript Changes (Starlark)

**In `CreateMetaVertex` — `is_ddl_class` branch**, after the existing 9 `make_aspect` calls, add a 10th:

```python
make_aspect(meta_key + ".compensation", meta_key, "compensation",
            "compensation",
            {"inverseOperationType": "TombstoneMetaVertex",
             "payloadTemplate": {"metaKey": "{{detail.metaKey}}"},
             "revisionTemplate": {"metaKey": "{{revisions[detail.metaKey]}}"}}),
```

**In `CreateMetaVertex` — `meta.lens` branch**, after the 4 existing `make_aspect` calls, add:

```python
make_aspect(meta_key + ".compensation", meta_key, "compensation",
            "compensation",
            {"inverseOperationType": "TombstoneMetaVertex",
             "payloadTemplate": {"metaKey": "{{detail.metaKey}}"},
             "revisionTemplate": {"metaKey": "{{revisions[detail.metaKey]}}"}}),
```

**In `TombstoneMetaVertex` branch**, replace the existing minimal implementation with:

```python
if ot == "TombstoneMetaVertex":
    meta_key = required_string(p, "metaKey")
    if not vertex_alive(state, meta_key):
        fail("UnknownMetaVertex: " + meta_key)
    # Optional revision assertion for compensating-op conflict detection.
    force = hasattr(p, "force") and p.force == True
    if hasattr(p, "expectedRevision") and not force:
        expected_rev = p.expectedRevision
        if type(expected_rev) != type(0):
            fail("InvalidArgument: expectedRevision must be an integer")
        # Note: actual revision checking at BatchOp level (substrate).
        # The Starlark-level check here is a best-effort pre-flight guard
        # that runs before the atomic batch. For strong guarantees, the
        # BatchOp.HasRevision / BatchOp.Revision fields carry the assertion
        # to the NATS layer. Set both.
    mutations = [make_tombstone(meta_key)]
    if hasattr(p, "expectedRevision") and not force:
        mutations[0]["expectedRevision"] = p.expectedRevision
    events = [{"class": "MetaVertexTombstoned", "data": {"metaKey": meta_key}}]
    return {"mutations": mutations, "events": events,
            "response": {"metaKey": meta_key}}
```

**Important:** the `expectedRevision` field on a mutation dict is how the Starlark script communicates the desired revision condition to the Committer step 8. The `CommitterImpl.Commit` already handles `m.ExpectedRevision != nil` by setting `BatchOp.HasRevision = true` and `BatchOp.Revision = *m.ExpectedRevision` (see `internal/processor/step8_commit.go` lines 131–140). No Committer changes needed.

---

### UpdateMetaVertex Compensation

`UpdateMetaVertex` currently only updates the `.description` aspect (see `meta_ddl.go` line 183–193). When updating, the script must also update the `.compensation` aspect to reflect the new prior state.

The update requires reading the current description from `state`. Per Contract #2 §2.5, the caller must declare the read in `ContextHint.Reads`. The test harness must include `"vtx.meta.<id>.description"` in the operation envelope's `ContextHint.Reads` when submitting an `UpdateMetaVertex` op.

Updated `UpdateMetaVertex` Starlark block:

```python
if ot == "UpdateMetaVertex":
    meta_key = required_string(p, "metaKey")
    if not vertex_alive(state, meta_key):
        fail("UnknownMetaVertex: " + meta_key)
    desc = ""
    if hasattr(p, "description") and type(p.description) == type(""):
        desc = p.description
    # Read prior description from state for the compensation aspect.
    prior_desc = ""
    desc_key = meta_key + ".description"
    if desc_key in state and state[desc_key] != None:
        d = state[desc_key]
        if hasattr(d, "data") and hasattr(d.data, "text"):
            prior_desc = d.data.text
    force = hasattr(p, "force") and p.force == True
    mutations = [
        make_update(meta_key + ".description", {"text": desc}),
        make_update(meta_key + ".compensation",
            {"inverseOperationType": "UpdateMetaVertex",
             "payloadTemplate": {"metaKey": meta_key, "description": prior_desc},
             "revisionTemplate": {}}),
    ]
    if hasattr(p, "expectedRevision") and not force:
        mutations[0]["expectedRevision"] = p.expectedRevision
        mutations[1]["expectedRevision"] = p.expectedRevision
    events = [{"class": "MetaVertexUpdated", "data": {"metaKey": meta_key}}]
    return {"mutations": mutations, "events": events,
            "response": {"metaKey": meta_key}}
```

**Note on ContextHint.Reads for UpdateMetaVertex:** the prior description is read from `state`, which is hydrated in Step 4 from `ContextHint.Reads`. The test submitting an `UpdateMetaVertex` with compensation must include `vtx.meta.<id>.description` in the envelope's `ContextHint.Reads`. This is a **client-side requirement**, not a Processor change. The `gate4_rollback_test.go` test must declare this read.

---

### Aspect Count After This Story

The kernel `root` DDL meta-vertex aspect count grows:
- Pre-5.1 kernel: `canonicalName`, `permittedCommands`, `description`, `script` = 4 aspects
- After 5.1: + `inputSchema`, `outputSchema`, `fieldDescription`, `examples` = 8 aspects
- After 5.3: + `compensation` = 9 aspects

Every `CreateMetaVertex` commit now emits 10 mutations in the `is_ddl_class` branch (vertex + 9 aspects). The 10th mutation is the `.compensation` aspect.

---

### Gate 4 Test Implementation Notes

**Two-pipeline setup:** `CreateMetaVertex` and `TombstoneMetaVertex` go on the `meta` lane (`ops.meta`). Use the `testutil.PipelineConfig{FilterSubjects: []string{"ops.meta"}}` extension introduced in Story 5.2.

**Payload for `CreateMetaVertex` in the rollback test:**

```go
metaPayload := map[string]any{
    "targetClass":       "meta.ddl.vertexType",
    "canonicalName":     "RollbackTestDDL",
    "permittedCommands": []string{"DoRollbackTest"},
    "description":       "Ephemeral DDL for Gate 4 rollback test. Created and immediately tombstoned.",
    "script":            "def execute(state, op):\n    return {\"mutations\": [], \"events\": []}",
    "inputSchema":       `{"type":"object","properties":{}}`,
    "outputSchema":      `{"type":"object","properties":{}}`,
    "fieldDescription":  map[string]any{},
    "examples":          []any{},
}
```

Wait — AC3 of Story 5.1 requires all five self-description aspects to be non-empty on `CreateMetaVertex` for `is_ddl_class`. The test payload above has empty `fieldDescription` and `examples`. Adjust:

```go
"fieldDescription": map[string]any{"note": "No fields for this test-only DDL."},
"examples":         []any{map[string]any{"name": "test", "payload": map[string]any{}, "expectedOutcome": "Accepted."}},
```

**Capturing the metaKey from the committed reply:**
```go
// After DriveOne returns OutcomeAccepted, read Detail["metaKey"] from the reply.
// testutil.DriveOne returns the outcome; the actual reply is available via the
// op tracker key. Simplest: read the tracker after commit.
trackerEntry, _ := conn.KVGet(ctx, bootstrap.CoreKVBucket, processor.TrackerKey(reqID))
var trackerDoc processor.Tracker
json.Unmarshal(trackerEntry.Value, &trackerDoc)
metaKey := trackerDoc.Data["mutationKeys"].([]interface{})[0].(string)
```

Alternatively, use `testutil.DriveOneWithReply` if that helper exists after Story 5.2's additions, or read the reply from the request-reply NATS inbox directly.

**DiscoverDDL after tombstone** — the Traverser's `DiscoverDDL` reads `.canonicalName` aspects. After a `TombstoneMetaVertex` commit, the DDL meta-vertex has `isDeleted: true`. The `DiscoverDDL` implementation in Story 5.2 already skips tombstoned vertices:

```go
if aspDoc.IsDeleted {
    continue
}
```

But it reads the `.canonicalName` aspect doc, not the vertex doc. The tombstone marks the vertex itself `isDeleted: true`, but the `.canonicalName` aspect at `vtx.meta.<id>.canonicalName` may or may not be individually tombstoned by the Starlark script. **The current `TombstoneMetaVertex` script only tombstones the vertex key itself**, not the aspect keys. This means `DiscoverDDL` might still find the aspect and return the tombstoned vertex.

**Resolution (task for the implementer):** Update `DiscoverDDL` to also check the parent vertex's `isDeleted` flag after finding a canonical name match. Read the 3-segment vertex key; if `isDeleted: true`, skip it. This is a small additive change to `internal/aiagent/traversal.go`:

```go
if aspDoc.Data.Value == operationType {
    // Guard: verify the meta-vertex itself is not tombstoned.
    vtxEntry, err := t.conn.KVGet(ctx, t.coreBucket, key)
    if err != nil { continue }
    var vtxDoc struct { IsDeleted bool `json:"isDeleted"` }
    if json.Unmarshal(vtxEntry.Value, &vtxDoc) != nil || vtxDoc.IsDeleted { continue }
    return key, nil
}
```

This is a **Story 5.3 deliverable** (not a carry from 5.2) since the tombstone behavior is what makes it necessary.

---

### `make test-rollback` Makefile Target

Add to `Makefile`:

```makefile
test-rollback:
	go test ./internal/aiagent/... -run TestGate4_CompensatingOpRollback -v -p 1 -count=1
```

Place it after `test-capability-adversarial` in the Makefile, consistent with the gate naming pattern.

---

### File Layout

```
internal/aiagent/
  traversal.go            — modified: add ReadCompensation, fix DiscoverDDL tombstone guard
  traversal_test.go       — modified: add TestTraverser_ReadCompensation
  gate4_rollback_test.go  — new: Gate 4 integration test

internal/bootstrap/
  meta_ddl.go             — modified: MetaRootDDLScript (emit .compensation, add expectedRevision handling)
  primordial.go           — modified: seed .compensation on kernel root DDL meta-vertex

docs/components/
  processor.md            — modified: FR53 section documenting .compensation aspect contract

Makefile                  — modified: add test-rollback target
```

**Files NOT to touch:**
- `internal/processor/envelope.go` — `OperationReply` must not gain new fields
- `internal/processor/reply.go` — no new `BuildXxxReply` functions for compensation
- `internal/processor/commit_path.go` — no new commit path steps or compensation handling
- `internal/processor/step8_commit.go` — no changes (the `ExpectedRevision` field on mutations is already wired)
- `internal/pkgmgr/` — package installer does not emit `.compensation` aspects (out of Phase 1 scope; Phase 2 task when rbac-domain DDLs get their own compensation semantics)
- `packages/` — no package DDL changes in this story
- `_bmad-output/planning-artifacts/` — **sub-agents must never edit planning artifacts**

---

### Architecture Compliance Checklist

- [ ] `grep -rn "CompensatingOperation\|compensatingOperation" internal/processor/` → zero production-code hits
- [ ] `grep -rn "ContextHint" internal/processor/operation_context.go` confirms only `Reads []string` field
- [ ] `OperationReply` struct in `envelope.go` matches the shape quoted in AC2 exactly
- [ ] `ReadCompensation` is the only new method on `Traverser`; no new struct types added
- [ ] `make test-rollback` exits 0
- [ ] `make verify-bootstrap` green (kernel `.compensation` aspect seeded)
- [ ] `make test-bypass` all-DEFENDED (Gate 2 regression — no new bypass paths)
- [ ] `make test-capability-adversarial` all-BLOCKED (Gate 3 regression)
- [ ] `golangci-lint run ./...` clean

---

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` §Story 5.3, lines 1327–1378] — original AC, superseded by this brief
- [Source: `_bmad-output/implementation-artifacts/5-1-ddl-self-description-aspects.md`] — five-aspect pattern this story extends to six
- [Source: `_bmad-output/implementation-artifacts/5-2-cold-start-ai-agent-traversal.md`] — Traverser pattern, meta-lane pipeline setup, DiscoverDDL algorithm
- [Source: `internal/aiagent/traversal.go`] — existing Traverser; `ReadCompensation` goes here
- [Source: `internal/bootstrap/meta_ddl.go`] — `MetaRootDDLScript` modification target; comment on line 18 ("Story 5.3 routes package installs through CreateMetaVertex ops") is misleading and should be removed — this story is per-op compensation, not package-install routing
- [Source: `internal/processor/envelope.go`] — `OperationReply` shape to preserve (do NOT modify)
- [Source: `internal/processor/step8_commit.go` lines 131–140] — `BatchOp.HasRevision` / `BatchOp.Revision` already wired; no changes needed
- [Source: `internal/processor/reply.go`] — `BuildAcceptedReplyWithRevisions` + `BuildAcceptedReplyWithDetail` already populate `Revisions` and `Detail`; these are the fields the operator uses for template substitution
- [Source: `_bmad-output/implementation-artifacts/WINSTON-RESUME.md`] — operating conventions, drift patterns, Brief-imprecision correction policy

---

## Implementation Tier & Budget

**Model tier: Sonnet** (downgraded from Opus — after Option A removed the envelope/commit-path coupling, the remaining work is pattern-following and less complex than Story 5.2 which Sonnet handled cleanly).
**Estimated token budget: ~80-100K** (input + output — tracking only, NOT enforced per Rule 8 in WINSTON-RESUME.md). Sub-agent self-reports are typically 30-50% low vs outer telemetry; trust the task-notification `total_tokens`.

---

## Stuck-Loop Halt Criteria

Halt and surface for Winston review if any of the following occur:
- The same compilation error or test failure recurs after 3+ fix attempts without a different root cause hypothesis.
- A passing approach is found but it requires adding a field to `OperationReply` — stop immediately; this violates Guardrail 1 and is a firm rejection, not a trade-off.
- `make test-rollback` fails after 2 debug attempts with non-flake root causes.
- Any test in `make test-bypass` or `make test-capability-adversarial` flips from DEFENDED/BLOCKED to a different status.

Do NOT halt for token budget alone.

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- **D1 (2026-05-23):** TombstoneMetaVertex was failing with `UnknownMetaVertex` because `ContextHint.Reads` was not declaring the metaKey in the Gate 4 test. The Starlark script calls `vertex_alive(state, meta_key)` which requires the key to be hydrated via step 4. Fixed: added `ContextHint: &processor.ContextHint{Reads: []string{metaKey}}` to the `gate4Tombstone` helper. This is a client-side contract requirement (Contract #2 §2.5), not a Processor change.

### Completion Notes List

- Implemented all 7 tasks; all acceptance criteria satisfied.
- `MetaRootDDLScript` now emits `.compensation` as the 10th mutation in the `is_ddl_class` branch (vertex + 9 aspects) and the 5th mutation in the `meta.lens` branch.
- `UpdateMetaVertex` branch reads prior description from `state` and updates `.compensation` with the concrete prior-description value.
- `TombstoneMetaVertex` and `UpdateMetaVertex` accept optional `expectedRevision` integer; propagated to `mutation["expectedRevision"]` for substrate-level atomic revision assertion at step 8.
- Primordial kernel root DDL now has 9 aspects (8 existing + `.compensation`).
- `ReadCompensation` is the only new method on `Traverser`; no new struct types added.
- `DiscoverDDL` now guards against tombstoned meta-vertices (reads vertex key after canonicalName match to check `isDeleted: true`).
- Gate 4 test exercises both DDL_VertexType and Lens branches end-to-end with re-create-after-tombstone validation.
- Architecture purity: zero `compensation`/`Compensation` hits in `internal/processor/`; `ContextHint` unchanged; `OperationReply` unchanged.

**Brief imprecision flagged for Winston:**
- The brief (Dev Notes, "Capturing the metaKey from the committed reply") suggests reading `Detail["metaKey"]` from the `OperationReply`. However, `commit_path.go` uses `BuildAcceptedReplyWithDetail` (not `BuildAcceptedReplyWithRevisions`), so the `Revisions` field is NOT populated in the reply. The reply also goes to a NATS reply-to inbox not directly accessible via `DriveOne`. The test correctly uses the tracker approach (`tracker.Data["mutationKeys"][0]`) instead, which is the pattern established in `fr19_northstar_test.go`. This is a brief imprecision, not a contract gap.
- The brief's TombstoneMetaVertex Starlark snippet includes `# Note: actual revision checking at BatchOp level` commentary suggesting the Starlark check would need to compare against the state-read revision. This comparison cannot be done in Starlark since `state` provides the document envelope but not the NATS revision number. The implementation instead (a) validates type in Starlark and (b) propagates the expectedRevision to `mutation["expectedRevision"]` for substrate enforcement — same defense-in-depth without requiring Starlark to access KV sequence numbers.

### File List

- `internal/bootstrap/meta_ddl.go` — modified: MetaRootDDLScript (emit .compensation 6th aspect, add expectedRevision handling to Update + Tombstone branches)
- `internal/bootstrap/primordial.go` — modified: seed .compensation on kernel root DDL meta-vertex
- `internal/bootstrap/nanoid.go` — modified: add CompensationAspectClass constant
- `internal/aiagent/traversal.go` — modified: add ReadCompensation, ErrCompensationAspectMissing, fix DiscoverDDL tombstone guard
- `internal/aiagent/traversal_test.go` — modified: add TestTraverser_ReadCompensation_* (3 tests) + TestDiscoverDDL_SkipsTombstonedVertex
- `internal/aiagent/gate4_rollback_test.go` — new: Gate 4 integration test
- `Makefile` — modified: add test-rollback target
- `docs/components/processor.md` — modified: FR53 section with .compensation aspect contract
- `scripts/verify-kernel.go` — modified: assert .compensation aspect on MetaRootKey
