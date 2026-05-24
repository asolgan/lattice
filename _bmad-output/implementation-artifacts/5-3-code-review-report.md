# Code Review Report — Story 5.3: Compensating Operation & DDL Rollback

**Staged diff reviewed:** Story 5.3 staged changes (pre-commit)
**Reviewer:** bmad-code-review (Sonnet 4.6)
**Date:** 2026-05-23
**Spec file:** `_bmad-output/implementation-artifacts/5-3-compensating-operation-ddl-rollback.md`

**Files in scope:**
- `internal/bootstrap/meta_ddl.go` (MetaRootDDLScript extensions)
- `internal/bootstrap/primordial.go` (seed `.compensation`)
- `internal/bootstrap/nanoid.go` (CompensationAspectClass constant)
- `internal/aiagent/traversal.go` (ReadCompensation + DiscoverDDL tombstone guard)
- `internal/aiagent/traversal_test.go` (3 new ReadCompensation tests + SkipsTombstonedVertex)
- `internal/aiagent/gate4_rollback_test.go` (Gate 4 integration test)
- `Makefile` (test-rollback target)
- `docs/components/processor.md` (FR53 section)
- `scripts/verify-kernel.go` (verify `.compensation` aspect)

**Note:** Parallel subagent review is not available in this session. All three layers — Blind Hunter, Edge Case Hunter, Acceptance Auditor — were run sequentially inline with full project access.

---

## Summary

**2 MUST FIX items found.** The core compensation contract design is architecturally clean and all five guardrails hold. The two blockers are: (1) a pre-existing `parseMutations` gap in `starlark_runner.go` that silently drops `expectedRevision` from Starlark mutation dicts — Story 5.3 is the first story to rely on this field, so the AC5 revision-assertion mechanism is dead on arrival; and (2) the `TombstoneMetaVertex` branch does not update the `.compensation` aspect to reflect irreversibility, violating AC3 and leaving stale compensation guidance readable after tombstone.

---

## 🔴 MUST FIX

### MF-1 — `parseMutations` silently drops `expectedRevision` from Starlark mutation dicts — AC5 revision assertion is dead on arrival

**File:** `internal/processor/starlark_runner.go`, `parseMutations` function (lines 249–296)
**Review layer:** Blind Hunter + Acceptance Auditor

**What's wrong:**

The `parseMutations` function parses mutation dicts returned by the Starlark script but only extracts `op`, `key`, and (for `create`/`update`) `document`. The `expectedRevision` key set by the Starlark script is never read and `MutationOp.ExpectedRevision` is never populated:

```go
// parseMutations: lines 281–294
m := MutationOp{Op: op, Key: key}
if op == "create" || op == "update" {
    // ... parses document
}
out = append(out, m)   // ExpectedRevision stays nil
```

The MetaRootDDLScript now sets `mutations[0]["expectedRevision"] = expected_rev` in both `TombstoneMetaVertex` and `UpdateMetaVertex` branches. But since `parseMutations` never reads that key, `m.ExpectedRevision` remains `nil`. Step 8's commit path in `step8_commit.go` (lines 135–137) only sets `op.HasRevision = true` when `m.ExpectedRevision != nil` — so the revision assertion is never propagated to `BatchOp.HasRevision` / `BatchOp.Revision`.

**Consequence:** A `TombstoneMetaVertex` or `UpdateMetaVertex` op with `expectedRevision` set silently succeeds without any revision check. The defense-in-depth guarantee described in AC5 and the Dev Notes ("the substrate AtomicBatch provides the binding revision assertion") is a no-op. An operator can pass any `expectedRevision` and the Starlark pre-flight type check runs but the substrate-level assertion never fires.

**What to do:** Add `expectedRevision` extraction to `parseMutations` in `starlark_runner.go`. After building `m := MutationOp{Op: op, Key: key}`, read the optional integer field and assign `m.ExpectedRevision`:

```go
if rev, found, _ := md.Get(starlarklib.String("expectedRevision")); found && rev != starlarklib.None {
    if revInt, ok := rev.(starlarklib.Int); ok {
        if v, ok := revInt.Uint64(); ok {
            m.ExpectedRevision = &v
        }
    }
}
```

Add a test in `step5_execute_test.go` (or `starlark_runner_test.go`) that verifies a mutation dict containing `expectedRevision` produces a `MutationOp` with a non-nil `ExpectedRevision`.

**Which AC/Guardrail:** AC5 ("When present, the script asserts revision before emitting the tombstone mutation … propagated to `mutation["expectedRevision"]` for substrate enforcement.").

---

### MF-2 — `TombstoneMetaVertex` branch does not update `.compensation` aspect — stale "TombstoneMetaVertex" guidance persists after tombstone

**File:** `internal/bootstrap/meta_ddl.go`, `TombstoneMetaVertex` branch (lines 248–266)
**Review layer:** Edge Case Hunter + Acceptance Auditor

**What's wrong:**

AC3 states: "`TombstoneMetaVertex` is irreversible in Phase 1. Its `.compensation` aspect encodes `{"inverseOperationType": "none", "note": "Tombstone is irreversible in Phase 1; operator must recreate via CreateMetaVertex with prior payload."}"`

The `TombstoneMetaVertex` Starlark branch emits only `make_tombstone(meta_key)` — it does NOT emit `make_update(meta_key + ".compensation", {...})`. After tombstoning a DDL meta-vertex:

1. `ReadCompensation(ctx, metaKey)` still returns data because `TombstoneMetaVertex` only tombstones the vertex key itself, not the `.compensation` aspect key (this is documented and tested by `TestDiscoverDDL_SkipsTombstonedVertex`).
2. The stale compensation data still reads `{"inverseOperationType": "TombstoneMetaVertex", ...}` — an operator calling `ReadCompensation` on a tombstoned vertex would get incorrect guidance that the compensating op is `TombstoneMetaVertex` when in fact the vertex is already gone.

The docs (processor.md line 196) correctly describe the contract: `inverseOperationType: "none"` with a Phase 1 note. The implementation diverges from both the spec and the documentation.

**What to do:** Add a second mutation to the `TombstoneMetaVertex` branch that updates the `.compensation` aspect to reflect irreversibility:

```python
if ot == "TombstoneMetaVertex":
    meta_key = required_string(p, "metaKey")
    if not vertex_alive(state, meta_key):
        fail("UnknownMetaVertex: " + meta_key)
    force = hasattr(p, "force") and p.force == True
    mutations = [
        make_tombstone(meta_key),
        make_update(meta_key + ".compensation",
            {"inverseOperationType": "none",
             "note": "Tombstone is irreversible in Phase 1; operator must recreate via CreateMetaVertex with prior payload."}),
    ]
    if hasattr(p, "expectedRevision") and not force:
        expected_rev = p.expectedRevision
        if type(expected_rev) != type(0):
            fail("InvalidArgument: expectedRevision must be an integer")
        mutations[0]["expectedRevision"] = expected_rev
    events = [{"class": "MetaVertexTombstoned", "data": {"metaKey": meta_key}}]
    return {"mutations": mutations, "events": events,
            "response": {"metaKey": meta_key}}
```

Also add an assertion in `gate4_rollback_test.go` (steps 7–8 area) that calls `ReadCompensation` after tombstone and asserts `inverseOperationType == "none"`.

**Which AC/Guardrail:** AC3 ("`.compensation` aspect encodes `{"inverseOperationType": "none"...}` for TombstoneMetaVertex").

---

## 🟡 SHOULD CONSIDER

### SC-1 — `UpdateMetaVertex` applies the same `expectedRevision` to both description and compensation mutations — will cause spurious `RevisionConflict` once MF-1 is fixed

**File:** `internal/bootstrap/meta_ddl.go`, `UpdateMetaVertex` branch (lines 238–243)
**Review layer:** Edge Case Hunter

**What's wrong:**

The `UpdateMetaVertex` Starlark branch applies the same `expectedRevision` to both `mutations[0]` (description) and `mutations[1]` (compensation):

```python
mutations[0]["expectedRevision"] = expected_rev
mutations[1]["expectedRevision"] = expected_rev
```

The description and compensation aspects are at different KV keys (`meta_key.description` vs `meta_key.compensation`) and have independent NATS revision sequences. After the first `UpdateMetaVertex` operation, the `.compensation` aspect will be at a different NATS revision than the `.description` aspect (the `.compensation` aspect was written first by `CreateMetaVertex`; the description aspect was also written first; they may or may not have the same sequence depending on KV batch ordering). Once MF-1 is fixed and `expectedRevision` actually propagates to the substrate, passing the same revision for two keys with independent sequences will produce a `RevisionConflict` on whichever key's revision doesn't match.

The brief specifies the `expectedRevision` semantics for the description mutation (AC5 — confirming the description hasn't changed concurrently). The compensation aspect should be updated unconditionally (or with its own independent revision check if needed).

**What to do:** Remove `mutations[1]["expectedRevision"] = expected_rev` — the compensation mutation should be an unconditional update. Only apply `expectedRevision` to `mutations[0]` (the description aspect).

**Note:** This is a latent bug that is masked by MF-1. It will become a live bug once MF-1 is resolved. Recommend fixing alongside MF-1.

---

### SC-2 — Gate 4 test omits AC6 step 9: no assertion on consumer sequence position continuity

**File:** `internal/aiagent/gate4_rollback_test.go`
**Review layer:** Acceptance Auditor

**What's wrong:**

AC6 step 9 requires: "Verify no Processor or Refractor restart: consumer sequence positions held (assert no gap in the meta-lane durable's `NumPending`)." The gate4 test does not make this assertion. The test exercises the full create → discover → compensate → verify cycle but does not confirm the durable consumer on `gate4-meta-pipeline` has processed all ops without gap.

This was covered in the story spec and omitted from implementation. Given the compensating-op path goes through the same Processor commit path as all other ops, a consumer gap would be highly visible in real usage — but the spec test is incomplete as written.

**What to do:** After the final tombstone in each sub-test, call `metaCons.Info(ctx)` and assert `ci.NumPending == 0` and `ci.NumRedelivered == 0`. This confirms no pipeline restart occurred during the rollback cycle.

---

### SC-3 — `UpdateMetaVertex` compensation has no integration test coverage — prior description capture path is untested

**File:** `internal/aiagent/gate4_rollback_test.go`
**Review layer:** Acceptance Auditor

**What's wrong:**

The `UpdateMetaVertex` branch of `MetaRootDDLScript` was extended to (a) read prior description from state, (b) emit an updated `.compensation` aspect, and (c) accept `expectedRevision`. None of these paths are exercised in `gate4_rollback_test.go` — the test only exercises `CreateMetaVertex` + `TombstoneMetaVertex`.

The `ContextHint.Reads` requirement for `UpdateMetaVertex` (must declare `meta_key + ".description"`) is a client-side contract that could easily be forgotten. Without a test, it's unvalidated.

**What to do:** Add a sub-test `UpdateMetaVertex` to `TestGate4_CompensatingOpRollback` that: (1) creates a DDL, (2) submits `UpdateMetaVertex` with description + `ContextHint.Reads: [meta_key + ".description"]`, (3) reads `.compensation` and asserts `inverseOperationType == "UpdateMetaVertex"` and `payloadTemplate.description` contains the prior description, (4) submits the compensating `UpdateMetaVertex` to restore prior state, (5) verifies description is restored.

---

## 🟢 NITS

**N-1** — `internal/bootstrap/nanoid.go`, line 56: `CompensationAspectClass = "compensation"` is defined but never referenced in any production code. Both `primordial.go` and `meta_ddl.go` use the string literal `"compensation"` directly. Either reference the constant in those files or remove it if it was added speculatively. Using the constant would also give the compiler a lint-level guarantee against typos.

**N-2** — `internal/bootstrap/primordial.go`, line 412: the primordial `.compensation` aspect `data` map includes a `"note"` field not present in the canonical brief shape (AC3 aspect shape) and not emitted by the Starlark script for Starlark-created aspects. This creates a shape inconsistency between the primordial kernel's `.compensation` and runtime-created aspects. Operators or clients doing exact-key structural comparisons across all `.compensation` aspects would see divergent shapes. Remove the `note` field from the primordial entry to match the Starlark-emitted canonical shape.

**N-3** — `internal/aiagent/gate4_rollback_test.go`, lines 109, 146, 189: `int(vtxEntry.Revision)` where `Revision` is `uint64` is safe on 64-bit targets but the cast is unnecessary. Prefer `uint64(vtxEntry.Revision)` in the payload map and update the Starlark type check comment in `meta_ddl.go` to note it accepts Starlark integers from Go `uint64` via `MakeInt`. Or, even simpler, use the NATS entry directly: the payload's `expectedRevision` value is dispatched through `goValueToStarlark` as `MakeInt64(int64(x))` when passed as `int` — fine in practice but the explicit downcast from `uint64` to `int` is a lint smell.

**N-4** — `internal/bootstrap/primordial.go`, around line 186: the `SeedPrimordial` docstring comment still says "Total ≈ 33 Core KV entries" (pre-Story-5.3). After adding the `.compensation` aspect the count grew by 1 to ~34. Minor — update the count and the entry breakdown in the comment (the `verify-kernel.go` header was correctly updated to "~69"; the discrepancy is just between `primordial.go`'s local kernel-subset count and reality).

---

## Architecture / NFR Compliance Sign-off

- **NFR-S10 (same Processor path for compensating ops):** PASS. Zero compensation-specific handling in `internal/processor/`. The Gate 4 test submits forward and compensating ops through the same `CommitPath.HandleMessage` path. `grep -rn "compensation\|Compensation" internal/processor/` returns zero hits.
- **No new `OperationReply` fields (Guardrail 1):** PASS. `envelope.go` `OperationReply` struct is unchanged. No `CompensatingOperation`, `inverseOp`, or similar field present.
- **No new `ContextHint` fields (Guardrail 2):** PASS. `ContextHint` still has only `Reads []string`. No new fields added.
- **`compensation` references in `internal/processor/` (Guardrail 4):** PASS. Zero production-code hits. `grep -rn "compensation\|Compensation" internal/processor/` is clean.
- **No new KV buckets (Guardrail 3):** PASS. `primordial.go` `ProvisionBuckets` is unchanged. No new bucket added.
- **`.compensation` aspect data carries no business data (Guardrail 5/NFR-S6):** CONDITIONAL PASS. The Starlark-emitted aspects use only template references (`{{detail.metaKey}}`, `{{revisions[detail.metaKey]}}`) or concrete description text read from state. The concrete description in `UpdateMetaVertex` compensation is the operator-supplied text which may contain some business context, but it is the same information the operator originally wrote — no novel sensitive data is introduced. Note: N-2 flags that the primordial `.compensation` aspect has an extra `note` field that the canonical shape doesn't have; this note contains only operational text ("Compensate a CreateMetaVertex op...") and is not sensitive.
- **`ErrCompensationAspectMissing` is the only new sentinel error:** PASS. `ReadCompensation` correctly wraps `ErrCompensationAspectMissing` with `%w` in both the KV-miss and tombstoned-aspect paths (lines 199, 212).
- **`DiscoverDDL` tombstone guard:** PASS. The guard correctly reads the vertex key after a canonicalName match and skips if `isDeleted: true`. The unit test `TestDiscoverDDL_SkipsTombstonedVertex` covers this path directly.
- **`ReadCompensation` tombstone guard:** PASS. Returns `ErrCompensationAspectMissing` (wrapped) when `aspDoc.IsDeleted` is true. Covered by `TestTraverser_ReadCompensation_TombstonedAspect`.
- **`make test-rollback` target:** PASS. Makefile target correctly points to `TestGate4_CompensatingOpRollback` with `-p 1 -count=1`. Double `.PHONY` declaration (both in the `.PHONY` line and as a standalone) is harmless but redundant (N-level).

---

Review complete: 2 MUST FIX, 3 SHOULD CONSIDER, 4 NITS
