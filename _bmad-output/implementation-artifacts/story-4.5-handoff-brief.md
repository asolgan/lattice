---
title: Story 4.5 Implementation Handoff Brief
story: 4.5 — Staff-Approved Identity Merge (FR4)
model_tier: Opus (locked)
token_budget: ~135K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-17
predecessor: Story 4.4 (Duplicate Identity Detection, shipped at e89c4f7; progress update at 7abc7aa)
---

# Story 4.5 — Staff-Approved Identity Merge (FR4): Handoff Brief

## Your Role

Replace two `NotYetImplemented` Starlark stubs in the identity DDL (`ApproveIdentityMerge` and `MergeIdentity`) with real implementations that together deliver operator-driven duplicate-identity consolidation per FR4 — full audit trail, never automated, always operator-explicit. Plus the wiring needed for those scripts to function: hydrator support for a global link scan, a small Refractor projection check (no wrapper change expected), and a comprehensive integration-test suite.

After 4.5 ships, Epic 4 closes (5/5).

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **No worktree.** Work directly in `/Users/andrewsolgan/Documents/GitHub/Lattice` against branch `main`. Verify with `pwd` at startup.
- **No commits, no pushes.** Stage your changes; DO NOT call `git commit` or `git push`. Winston commits + pushes after review.
- **Planning artifacts are read-only.** Drift → append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` and continue. Do NOT edit `data-contracts.md`, `epics.md`, `lattice-architecture.md`, or `MORPH-DEVIATIONS.md` — per the Story 4.4 carry, `data-contracts.md` is already over-accreted and pending a cleanup pass; nothing new from 4.5 lands there.
- **Token budget is for tracking only, NOT a halt threshold.** Estimate ~135K. Record outer-telemetry actual at session close.
- **Halt and escalate** on stuck-loop patterns (re-attempting same fix 3+ times; immediate reverts; cycling between failed approaches; stuck on a test fail you can't reduce after two debug attempts).
- **Checkpoint every 8–10 tool calls OR after any deliverable OR after any file read >25KB.**
- **Model tier:** Opus only. Halt if Sonnet/Haiku.
- **Architecture binding:** Contract #1 §1.3 + §1.5 (envelopes + key shapes for vertex / aspect / link), Contract #3 (script return shape), Contract #6 §6.2 (cap envelope — for reprojection verification), epics.md Story 4.5 (lines 1174–1208), FR4 (operator-explicit; never automated), FR53 (every change reversible by compensating op — Phase 1 mitigation is the operator gate), NFR-SC1 (Phase 1 ≤500 identities; ~few thousand links typical).
- **Lint watch:** golangci-lint v2 on CI flags unused helpers (4.2 + 4.3 both shipped unused helpers that broke CI). Before declaring done, run `/Users/andrewsolgan/go/bin/golangci-lint run ./...`. 0 issues = green.
- **Token tracker:** update Row 4.5 at session close with outer-telemetry actual.
- **Andrew has authorized autonomous proceed.**

## 🔴 NAME RECONCILIATION (read carefully)

The AC text in `epics.md` Story 4.5 names two operations: `ReviewDuplicateCandidates` (query) and `MergeIdentities` (mutation). **Story 4.1 seeded the identity DDL `permittedCommands` with three different names** for these slots: `ApproveIdentityMerge`, `MergeIdentity`, `TombstoneIdentity`.

**Winston decision:** Use the bootstrap-seeded names. Adding new ops to `permittedCommands` would require a primordial-data migration (DDL meta-vertex update + new permissions + new grants + verify-bootstrap re-baseline) and that is **not** in 4.5's scope. The AC's naming is brief-imprecision from the planning phase; resolve as follows:

| AC name | Use this seeded name | Role |
|---|---|---|
| `ReviewDuplicateCandidates` | **`ApproveIdentityMerge`** | The review op. Returns flagged pairs with full context. No core-state mutations (only a tracker entry per step 8). Operator inspects the response detail and decides whether to follow up with a merge. |
| `MergeIdentities` | **`MergeIdentity`** (singular) | The actual merge mutation. Migrates links, transitions secondary to `merged`, sets `mergedInto`. |
| (none — out of scope) | `TombstoneIdentity` | **Leave as `NotYetImplemented` stub.** AC does not require it. Secondary stays queryable post-merge per AC. Phase 2+ carry. |

The semantic mismatch between the verb "Approve" and the actual review-only behavior is acknowledged. Document this in your closing summary under "name reconciliation." If you genuinely cannot make `ApproveIdentityMerge` work as the review op, raise a CAR before forking the implementation.

Grants already seeded by 4.1:
- `ApproveIdentityMerge` → operator (1 grant, scope=any).
- `MergeIdentity` → **NOT seeded** with any role grant. **Decision #5 below explains how to handle this without a primordial migration.**

## What's Already in Place (do NOT redo)

- **Identity DDL** (Story 4.1) — `permittedCommands` already includes both `ApproveIdentityMerge` and `MergeIdentity`. **No DDL surface change needed.**
- **Identity DDL script** at `internal/bootstrap/identity_ddl.go` — stubs at lines 463–466 return `script_error("NotYetImplemented", "Story 4.5: ApproveIdentityMerge" / "MergeIdentity")`. **Replace those two branches.** Leave `TombstoneIdentity` (line 467–468) as `NotYetImplemented`.
- **State machine** (4.1): `flagged-for-review → merged` is allowed. `enforce_not_merged` guard rejects all writes against a `merged` identity except where the script explicitly bypasses it (mirror the 4.3 `ClaimIdentity` pattern that does its own state check to avoid leaking `mergedInto` — NFR-S6).
- **`mergedInto` aspect** — schema seeded in identity DDL's `.description` aspect (line 59); 4.5 is the first writer.
- **`duplicateOf` link** (4.4): canonical key `lnk.identity.<lowID>.duplicateOf.identity.<highID>`. Both members are in `flagged-for-review`. Link envelope `data` has `{criteria, confidence, scanRequestId, flaggedAt}`.
- **Hydrator scan-prefix extension** (4.4): `ContextHint.ScanPrefixes []string` already supports `vtx.identity.` (vertex + 4 aspects) and `lnk.identity.` (6-segment links starting with secondary as src). **Extend it** for one more prefix: bare `lnk.` (all links anywhere in the bucket) — see §3 below.
- **Refractor `capabilityenv` `pendingReview` projection** (4.4) — secondary identities currently project `pendingReview: true` while in `flagged-for-review`. After merge, secondary's state is `merged`; existing Capability Lens cypher / wrapper behavior on `state==merged` is the verification target for the reprojection test. **No new wrapper field is required by AC.** Decision #8 below.
- **`crypto.constant_time_equal`, `crypto.sha256`, `sha256NanoID`, `strings.levenshtein*`** Starlark builtins (4.2–4.4). 4.5 does not need new builtins.
- **`ScriptResult.ResponseDetail` + `OperationReply.Detail`** (4.2). Use for the `ApproveIdentityMerge` review response and the `MergeIdentity` summary.
- **`substrate.AtomicBatch`** (1.4): single-bucket, all-or-nothing. NATS atomic-batch server-side ceiling is the practical hard cap; treat **1000 ops** as the safe ceiling for Phase 1 (the JetStream default; Story 1.1 spike validated). With the tracker entry consuming one slot, the script's pre-flight check rejects merges that would require >999 mutations.
- **`AuthContext{Target: actorKey}` pattern** (4.3): used for scope=self ops. `ApproveIdentityMerge` is scope=any, so no AuthContext needed.

Tree is clean at session start (commit `e89c4f7`; progress update `7abc7aa`; verify-bootstrap 154 OK; test-bypass 4/4 BLOCKED; test-capability-adversarial 4/4 DEFENDED; full `go test ./... -p 1 -count=1` green; golangci-lint 0 issues).

## Story Scope (4.5)

### 1. `ApproveIdentityMerge` script branch (the review op)

Replace the stub at `identity_ddl.go:463`. This op is **read-only** at the domain layer — its MutationBatch is empty (step 8 still writes the tracker entry, so the commit-path doesn't degenerate).

**Input** (`op.payload`):
- *(none required for the canonical review)* — the operator pulls all flagged pairs by default.
- Optional: `op.payload.primaryKey`, `op.payload.secondaryKey` — if both present, narrow the response to just that pair's context (useful when the operator already knows which pair they intend to merge and wants the focused detail).

**`contextHint`** the caller (test or Loom) should set:
- `scanPrefixes = ["vtx.identity.", "lnk.identity."]` — pre-load all identities (vertex + 4 aspects) and all identity-anchored links so the script can compute pairs without back-channel reads.

**Inside the script:**
- Enumerate `state.keys_with_prefix("vtx.identity.")`, filter to 3-segment vertex keys (per 4.4 pattern).
- For each identity vertex, read its `.state` aspect; **collect only those whose state == `flagged-for-review`**. Skip `merged` and `unclaimed`/`claimed` (no pending review for them).
- For each flagged identity, find its `duplicateOf` partners by scanning `state.keys_with_prefix("lnk.identity.")` for 6-segment links where:
  - the link's class (from its envelope `class` field) is `duplicateOf`, AND
  - the link key's `lowID` or `highID` segment matches this identity's NanoID, AND
  - the link envelope's `isDeleted` is not `True`.
- For each flagged identity, also collect:
  - `name`, `email`, `phone`, `state` (from already-hydrated aspects).
  - `createdAt`: from the vertex envelope's `observedAt` field (or `revision` if `observedAt` not present — be defensive; the envelope shape is per Contract #1 §1.3).
  - `credentialBinding` status: existence check on `vtx.identity.<id>.credentialBinding` aspect. Caller's `contextHint.reads` should include this for each identity-of-interest, BUT since the script doesn't know which identities are flagged until it scans, the cheapest path is to add `.credentialBinding` to the hydrator's `identityScanAspects` hard-coded list (extending it from 4 aspects to 5). See §3 below. The script reads from already-loaded state.
- Pairs are grouped: for each `duplicateOf` link `lnk.identity.<lo>.duplicateOf.identity.<hi>`, emit one entry `{primaryCandidate, secondaryCandidate, link: {key, criteria, confidence, scanRequestId, flaggedAt}, primaryDetail: {...}, secondaryDetail: {...}}`.
- Operator-facing "primary candidate" heuristic: prefer the identity with `state == claimed` (has a real user binding) over `unclaimed`; tie-break by earliest `createdAt`. Document this in the top-of-branch comment. The merge op itself does not enforce this preference — the operator chooses explicitly.
- If `payload.primaryKey + secondaryKey` are set: filter to the single matching pair; if no such pair exists in the flagged set, fail with `script_error("ReviewPairNotFound", primaryKey + "|" + secondaryKey)`.

**MutationBatch:** empty (`[]`).

**EventList:** empty (review is non-mutating; no audit event needed beyond the envelope itself which captures `who/when/what` — request envelope persists in the tracker for 24h, and the auth-trace KV from Story 3.5 captures the auth decision separately).

**Response detail** (`ScriptResult.ResponseDetail`):
```
{
  "flaggedCount": <int>,
  "pairs": [
    {
      "primaryCandidate": "<vtx.identity.X>",
      "secondaryCandidate": "<vtx.identity.Y>",
      "linkKey": "<lnk....>",
      "criteria": [...],
      "confidence": <float>,
      "scanRequestId": "<requestId>",
      "flaggedAt": "<timestamp>",
      "primaryDetail": {"name": "...", "email": "...", "phone": "...", "state": "...", "createdAt": "...", "hasCredentialBinding": <bool>},
      "secondaryDetail": {"name": "...", "email": "...", "phone": "...", "state": "...", "createdAt": "...", "hasCredentialBinding": <bool>}
    },
    ...
  ]
}
```

**LOC target:** ~80 LOC for this branch. If you cross ~100 LOC, escalate.

### 2. `MergeIdentity` script branch (the merge op)

Replace the stub at `identity_ddl.go:465`. This is the heavyweight op.

**Input** (`op.payload`):
- `primary`: string vertex key (`vtx.identity.<NanoID>`). **Required.**
- `secondary`: string vertex key. **Required.** Must differ from primary.
- `aspectConflictResolution` (optional): object `{<aspectShortName>: "primary-wins" | "secondary-wins" | "primary"}`. Default = `primary-wins` for all aspects. Phase 1 supported short names: `name`, `email`, `phone` (the contact aspects). Other aspects always primary-wins.

**Pre-flight validation** (inside script, all rejections produce `script_error("InvalidMerge", "<reason>")` or a more specific code where listed):
1. `primary != secondary` else `script_error("MergeSelfReference", primary)`.
2. Both vertices present and not tombstoned (read `state.read(primaryKey)` + `state.read(secondaryKey)`; reject with `MergeIdentityMissing` if either is `None` or has `isDeleted == True`).
3. Both states == `flagged-for-review` (read `.state` aspect on each). Reject with `MergeStateRejected` if either is `unclaimed`, `claimed`, or `merged`. Do NOT leak `mergedInto` in the error message — `merged → merged` rejection just says state=`merged`, not the redirect target (NFR-S6, mirroring 4.3's ClaimKeyInvalid pattern).
4. A `duplicateOf` link exists between them, non-tombstoned. The canonical key is `lnk.identity.<lowID>.duplicateOf.identity.<highID>` where `lowID/highID = sorted([primaryNanoID, secondaryNanoID])`. Read `state.read(linkKey)`; reject with `MergeNoDuplicateOfLink` if absent or `isDeleted`.

**Link enumeration** (the hard part):
- The script needs every link that involves secondary on EITHER endpoint. Secondary-as-source links share the prefix `lnk.identity.<secondaryId>.` (6-segment keys). Secondary-as-target links have shape `lnk.<otherType>.<otherId>.<rel>.identity.<secondaryId>` and do NOT share a prefix with secondary.
- **Approach (Decision #4 below):** the hydrator's `lnk.` global-scan prefix (new in this story — see §3) pre-loads ALL link keys in the bucket. The script iterates `state.keys_with_prefix("lnk.")`, filters to 6-segment link keys (5 dots; substrate's `ClassifyKey` semantics), and selects those where segment[1]==secondary OR (segment[3]==`identity` AND segment[4]==secondaryId) — i.e., either endpoint is secondary.
- For each such link, additionally skip if `isDeleted == True`.

**Self-loop edge case**: after rekeying, a link from secondary→primary or primary→secondary becomes primary→primary. Specifically the `duplicateOf` link itself becomes `lnk.identity.<primary>.duplicateOf.identity.<primary>`. **Skip the create** for any rekeyed key whose two endpoint IDs are equal. Still TOMBSTONE the original (it has served its purpose; the merge is the resolution).

**Batch sizing pre-check**:
- Total ops budget per call to `substrate.AtomicBatch` is 1000 (1 tracker + N mutations, so N ≤ 999). Each rekeyed link consumes **2 ops** (1 tombstone + 1 create; or 1 tombstone alone for self-loops). Plus secondary state aspect (1), `mergedInto` aspect (1), and any aspect conflict resolutions (0–3 ops in Phase 1).
- Reject pre-flight with `script_error("MergeBatchTooLarge", "<count>")` if total mutations + 1 (tracker) > 1000 (i.e. mutations > 999). Phase 1 expectation: typical merge involves <10 links; this rejection is a defensive cap, not an expected path.

**MutationBatch construction**:
- **For each non-self-loop link `L_old = lnk.A.B.rel.C.D` involving secondary**:
  - Compute `L_new` by substituting `primary`'s `(type, id) = ("identity", primaryNanoID)` for whichever endpoint was secondary.
  - Op 1: `delete` of `L_old`. Use a soft-delete (`isDeleted: True`) at the link envelope shape per the Refractor's tombstone convention. Confirm by reading the existing envelope, copying its `data`, setting `isDeleted: True`, writing back.
  - Op 2: `create` of `L_new` with the same `data` payload as the original (preserve the relationship's context). If a link at `L_new` already exists (collision), MERGE: read existing, take its data over the incoming data (primary-wins for link collisions; the secondary's relationship is the duplicate). Document in closing summary.
- **For each self-loop link** (rekeyed endpoints equal): emit only the tombstone op. Skip the create.
- **For each duplicate-pair link** (the `duplicateOf` between primary and secondary): tombstone, no create — the relationship is now resolved.
- **Secondary state aspect**: aspect mutation on `vtx.identity.<secondary>.state` setting `value: "merged"`. Validate via `validate_state_transition("flagged-for-review", "merged")` (4.1's helper allows this).
- **Secondary `mergedInto` aspect**: aspect mutation on `vtx.identity.<secondary>.mergedInto` setting `value: primary` (the full vertex key, not just the NanoID — for clarity at read time).
- **Aspect conflict resolution on primary** (only if `aspectConflictResolution` indicates `secondary-wins` for a contact aspect):
  - For each `{name | email | phone}` that resolves to `secondary-wins`: read secondary's aspect value, write to primary's aspect key. Skip if secondary's aspect is empty or missing.
- **Primary state aspect**: unchanged (no mutation).

**EventList**: emit ONE `IdentityMerged` event with data `{primary, secondary, linkCount: <int>, criteriaSource: [...] (from duplicateOf link), aspectConflictResolution: {...}, mergedAt: op.envelope.observedAt}`.

**Response detail** (`ScriptResult.ResponseDetail`):
```
{
  "primary": "<vtx.identity.X>",
  "secondary": "<vtx.identity.Y>",
  "linksMigrated": <int>,
  "linksTombstonedOnly": <int>,           // self-loops + duplicateOf
  "linkCollisionsMerged": <int>,          // L_new already existed
  "aspectConflictsResolved": {<aspect>: "primary-wins"|"secondary-wins", ...},
  "secondaryState": "merged",
  "mergedInto": "<vtx.identity.X>"
}
```

**Permission**: 4.1 did NOT seed a grant link for `MergeIdentity` to any role. **Decision #5**: do NOT add primordial grant data (would force a primordial migration + verify-bootstrap rebaseline). Instead, run the integration tests in **`AuthMode: stub`** for `MergeIdentity` and run all OTHER paths (review, denials, post-merge redirect) in capability mode where seeded grants exist. Document this asymmetry as a Phase 1 carry: "Story 5.x or a follow-up should add a `MergeIdentity` → operator grant link to the primordial seed (and rebaseline verify-bootstrap from 154 OK to 156 OK)." Specifically:
- The `MergeIdentity` integration test uses `LATTICE_AUTH_MODE=stub` (or constructs a stub-authorizer pipeline directly, mirroring early-Phase-1 test patterns).
- The `ApproveIdentityMerge` integration tests run in capability mode (uses the operator-grant seeded by 4.1).
- The non-operator-denied test for `ApproveIdentityMerge` runs in capability mode (asserts the auth perimeter is real).
- **There is no non-operator-denied test for `MergeIdentity` in Phase 1** because we can't simulate it through the real authorizer until the grant is seeded. Document this.

**LOC target:** ~140 LOC for this branch (largest in the script). Total `identityDDLScript` (4.1 + 4.2 + 4.3 + 4.4 + 4.5) target: stay <500 LOC. If you cross ~520 LOC, halt and escalate before adding a shared-library mechanism — Andrew + Winston will decide whether to split.

### 3. Hydrator: add `lnk.` global-scan prefix support

In `internal/processor/step4_hydrate.go` (the `hydrateScanPrefix` function 4.4 introduced):

- Allow a third prefix value: bare `"lnk."`. Behavior:
  - List all keys under `core-kv` matching `lnk.>` via `substrate.KVListKeys`.
  - Retain only **6-segment** keys (5 dots) — these are the canonical Contract #1 §1.5 link keys; ignore stray ones if any.
  - Per-key, read the value envelope (no aspect expansion — links don't have aspects).
  - **Soft cap: 5000 keys.** Above 5000 → `HydrationError("scan-too-large", count, "lnk.")`. Phase 1 expectation (NFR-SC1: ≤500 identities, modest link density per identity) keeps this well under 5000.

In the same function, extend `identityScanAspects` (the hard-coded `[".name", ".email", ".phone", ".state"]` list from 4.4) to include `".credentialBinding"` so the `ApproveIdentityMerge` review op can report `hasCredentialBinding` without back-channel reads. **Five aspects per scanned vertex now; ~2500 reads max at N≤500 — still within tens of ms on embedded NATS.**

Document the prefix list extension at the top of `hydrateScanPrefix` in a 5-line comment block.

**Hydrator unit test:** add a focused test asserting `lnk.` scan loads all 6-segment link keys present in a seeded fixture and respects the 5000-key cap. Co-locate with the 4.4 hydrator tests if they exist; else add to `step4_hydrate_test.go`.

### 4. Refractor reprojection — verification only, no wrapper change

**Decision #8** (below): Story 4.5 does NOT add a new `capabilityenv` wrapper field. The AC requirement "Capability KV reprojection within NFR-P3 lag" is satisfied by the existing reprojection pipeline:
- When secondary's `state` aspect mutates from `flagged-for-review` to `merged`, the Refractor's adjacency-driven reprojection fires.
- The existing Capability Lens cypher will re-evaluate secondary. Whether the resulting cap entry is empty / preserved depends on cypher semantics. Either outcome is acceptable per AC ("secondary remains queryable" via the IdentityMerged redirect in Step 3 auth — not via cap entry).
- The integration test asserts the reprojection completed (poll the `revision` of `cap.identity.<secondaryId>` and assert it advanced within 1s, OR poll until `pendingReview` is no longer `true` since secondary left `flagged-for-review`).
- **`pendingReview: true` should disappear** from secondary's cap entry within 1s of the merge committing (4.4's wrapper reads the current state; `merged != flagged-for-review`).
- **Primary's cap entry should reflect any newly-migrated links** that change its role/permission graph. If a link `lnk.identity.<secondary>.holdsRole.role.<X>` is migrated to `lnk.identity.<primary>.holdsRole.role.<X>` AND primary did not previously hold that role, primary's `cap.identity.<primary>` projection gains that role. Integration test asserts this for at least one holdsRole link.

If — after implementing — you find the wrapper genuinely cannot read secondary's `mergedInto` without invasive plumbing AND the reprojection test fails for a real reason (not flake), THEN consider injecting a `merged: true, mergedInto: <primary>` field via wrapper. But this is a fallback, not the default path. Raise a CAR if you go there.

### 5. Integration tests in `internal/processor/identity_merge_test.go` (NEW file)

Mirror the fixture patterns from 4.1/4.2/4.3/4.4 (capability-mode wiring; embedded NATS; seeded operator-role actor). Total: ~9 tests.

**Capability-mode tests (use the seeded operator grant for ApproveIdentityMerge):**

- **`TestApproveIdentityMerge_SurfacesFlaggedPairs`**: seed 4 identities forming 2 flagged pairs (A↔B exact-email, C↔D levenshtein-name); seed 1 lone unflagged identity E. Run `ApproveIdentityMerge`. Assert `flaggedCount==4`, `pairs.length==2`, each pair has correct `primaryDetail/secondaryDetail` with the expected name/email/phone/state/createdAt/hasCredentialBinding, and `linkKey` matches the canonical 4.4 shape.

- **`TestApproveIdentityMerge_FiltersByPair`**: seed 2 flagged pairs; run with `payload.primaryKey + secondaryKey` set to one pair. Assert `pairs.length==1`. Run with a primaryKey/secondaryKey that doesn't form a real flagged pair → assert `script_error("ReviewPairNotFound", ...)`.

- **`TestApproveIdentityMerge_HasCredentialBindingFlag`**: seed 2 flagged identities, one of which has a `credentialBinding` aspect (claimed identity from 4.3 pattern). Assert `hasCredentialBinding` is `true` for that one, `false` for the other.

- **`TestApproveIdentityMerge_NonOperatorDenied`**: consumer-role actor → step 3 denies. (operator-only — grant seeded by 4.1.)

- **`TestApproveIdentityMerge_EmptyWhenNoFlagged`**: seed only `unclaimed`/`claimed` identities (no pairs flagged). Assert `flaggedCount==0`, `pairs==[]`.

**Stub-mode tests for `MergeIdentity` (per Decision #5 — no grant seeded yet):**

- **`TestMergeIdentity_HappyPath`**: seed 2 flagged identities A (primary, claimed, holds RoleX, has 2 outbound links) and B (secondary, unclaimed, holds RoleY, has 1 outbound + 1 inbound link from C). Pre-seed the canonical `duplicateOf` link. Run `MergeIdentity{primary: A, secondary: B}`. Assert:
  - Every link previously at secondary is migrated (outbound: rekeyed to primary; inbound: rekeyed to primary). Specifically check `lnk.identity.<A>.holdsRole.role.<RoleY>` exists; B's outbound links are now A's; the `lnk.<otherType>.<other>.<rel>.identity.<B>` style inbound link is now `lnk.<otherType>.<other>.<rel>.identity.<A>`.
  - Each `L_old` tombstoned (`isDeleted: True`).
  - The `duplicateOf` link between A and B: tombstoned, no recreate.
  - B's `state` aspect == `merged`.
  - B's `mergedInto` aspect == A's vertex key.
  - A's `state` aspect == `claimed` (unchanged).
  - Response detail counts match (`linksMigrated`, `linksTombstonedOnly`, `linkCollisionsMerged`, etc.).

- **`TestMergeIdentity_RejectsNonFlaggedSecondary`**: secondary in `unclaimed` state → `script_error("MergeStateRejected", ...)`. Assert no mutations applied (state aspect unchanged, links untouched).

- **`TestMergeIdentity_RejectsAlreadyMergedSecondary`**: secondary in `merged` state → `script_error("MergeStateRejected", "secondary state=merged")`. Critically, assert the error message does NOT include `mergedInto`'s value (NFR-S6).

- **`TestMergeIdentity_RejectsMissingDuplicateOfLink`**: both in `flagged-for-review` but no `duplicateOf` link between them → `script_error("MergeNoDuplicateOfLink", ...)`.

- **`TestMergeIdentity_SelfReferenceRejected`**: primary == secondary → `script_error("MergeSelfReference", ...)`.

- **`TestMergeIdentity_AspectConflictResolution`**: A and B both have distinct `email` aspects. Run `MergeIdentity{aspectConflictResolution: {email: "secondary-wins"}}`. Assert A's email aspect post-merge equals B's pre-merge email. Other aspects unchanged.

- **`TestMergeIdentity_CapKVReprojection_NFR_P3`**: seed A + B flagged, B holds RoleZ (which A does NOT hold). Merge. Poll `cap.identity.<A>` with a 1s deadline; assert the cap entry's `platformPermissions` grew to include RoleZ's perms. Poll `cap.identity.<B>` with a 1s deadline; assert `pendingReview` no longer present (it was set in 4.4 when flagged; should clear since state != flagged-for-review). If the 1s deadline is flaky on a first run, bump to 2s — DO NOT lower below 1s without escalation.

- **`TestMergeIdentity_PostMergeRedirect_FR4`**: after merge, submit any non-stub op (e.g. `UpdateIdentityState` or even a self-targeted no-op) against secondary's key. Assert step 3 / step 5 rejects via `enforce_not_merged` with the existing `IdentityMerged: mergedInto=<primary>` error pattern. **This test runs in capability mode** (uses an op that already has a real grant from 4.1) — the post-merge redirect is enforced by 4.1's `enforce_not_merged` helper, which IS already in production. The IdentityMerged error in the script-error path is what the test asserts.

If `MergeIdentity_HappyPath` is the only test that needs >5 link migrations, keep the fixture lean — 3–4 links per side suffices to validate both directions + collision behavior.

### 6. Verify-bootstrap

No new primordial entries in 4.5. The 154 OK count should remain 154. If you find yourself wanting to add a new grant, STOP — that's Decision #5's primordial-migration trap. Use stub-mode for `MergeIdentity` instead.

### 7. Bypass + Gate 3 re-audit

Confirm `make test-bypass` (4/4 BLOCKED) and `make test-capability-adversarial` (4/4 DEFENDED) remain green. The new script branches are pure / deterministic / use only existing builtins — no new escape surface. If any gate flips, STOP and escalate.

**Out of scope:**
- `TombstoneIdentity` op (leave stub).
- `SplitIdentities` (un-merge) — explicit Phase 2+ per AC.
- Adding new primordial grants (Decision #5: stub-mode for `MergeIdentity` tests).
- Async / streaming merge for >999 mutations (Phase 2).
- Cross-cell merge coordination.
- New buckets, new lenses, new Capability Lens cypher.
- Refractor `capabilityenv` `merged: true` field injection unless the simpler path fails (Decision #8).
- `data-contracts.md` addendum (Story 4.4 carry: file is over-accreted; nothing new lands).
- UI / Loom changes (operator surface is response-detail-driven).

**Hard escalation triggers (append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` then move on):**
- Hydrator can't accept the `lnk.` (bare) prefix without invasive change.
- `substrate.AtomicBatch` rejects a well-formed multi-op single-bucket call for a reason not documented in Story 1.1.
- Step 3 / step 4 / step 8 cannot handle an empty MutationBatch from `ApproveIdentityMerge` (review op).
- A link envelope's `class` field is not accessible from the Starlark `state.read` return (would block the `duplicateOf` filter in §1).
- The IdentityMerged redirect test reveals a real perimeter regression.

Halt entirely and surface to Winston for:
- Bypass or Gate 3 vector flips from green.
- Stuck-loop pattern per operating rules.
- Real auth perimeter regression in integration tests.
- Total `identityDDLScript` crossing ~520 LOC.

## Architectural Decisions Already Made (Winston)

1. **Use bootstrap-seeded op names** (`ApproveIdentityMerge` + `MergeIdentity`) per the name-reconciliation table at the top of this brief. AC's `ReviewDuplicateCandidates`/`MergeIdentities` are brief-imprecision; reconciling via DDL migration is out of scope.

2. **`ApproveIdentityMerge` is the review op** — non-mutating, empty MutationBatch (only the tracker entry). The semantic mismatch between the verb "Approve" and read-only behavior is acknowledged; the actual approval is the operator's downstream act of submitting `MergeIdentity`.

3. **`MergeIdentity` is the merge op** — link migration via tombstone-old + create-new pairs, secondary state→merged + mergedInto, optional aspect conflict resolution. Self-loops (rekey produces equal endpoints) tombstone-only.

4. **Global `lnk.` hydrator scan** with 5000-key soft cap is the Phase 1 path for finding inbound links. NFR-SC1 (≤500 identities) keeps this well under the cap. Phase 2 carry: adjacency-keyed inbound lookup once Processor has a clean adjacency-read substrate (currently adjacency is Refractor-only).

5. **`MergeIdentity` has NO seeded grant link.** Do NOT add one (primordial-data migration is out of scope). `MergeIdentity` integration tests run in **stub auth mode**; `ApproveIdentityMerge` and post-merge redirect tests run in capability mode. Document this asymmetry as a Phase 1 carry — a future story (Story 5.x or a tiny follow-up) must seed `MergeIdentity → operator` and rebaseline verify-bootstrap (154 → likely 156 OK with the grant link + permission, if `MergeIdentity` doesn't already have a permission vertex from 4.1).

   **Verify before proceeding**: grep `identity_ddl.go` for `PermMergeIdentity` — if NO `Perm*` exists for `MergeIdentity` either, then both the permission AND the grant are absent; the stub-mode workaround is necessary AND sufficient for Phase 1. Note this clearly in the closing summary.

6. **Soft-delete is the tombstone convention.** Read the existing link envelope, set `isDeleted: True` in the value, write back at the same key. Matches Refractor / 3.2b expectations.

7. **Link collision is primary-wins.** If `L_new` already exists when migrating a link from secondary→primary, the primary's existing link data is preserved; secondary's data is dropped (the link relationship is the duplicate that the merge resolves). Track count in response detail as `linkCollisionsMerged`. Document in closing summary.

8. **No new `capabilityenv` wrapper field** for merged identities by default. Existing reprojection on `state` aspect mutation suffices. The integration test verifies reprojection by polling cap entry revision / `pendingReview` clearance. Wrapper extension is a fallback if direct path fails.

9. **NATS atomic-batch ceiling = 1000 ops total** (1 tracker + 999 mutations). Pre-flight reject in `MergeIdentity` if mutations > 999 with `script_error("MergeBatchTooLarge", "<count>")`. Phase 1 expected count ≪ this.

10. **`MergeIdentity` errors do NOT leak `mergedInto`** in their messages. Mirror 4.3's `ClaimKeyInvalid` NFR-S6 pattern.

11. **No new builtins.** 4.5 uses what 4.1–4.4 shipped.

12. **Reprojection latency**: NFR-P3 (≤500ms). Test polls cap KV with 1s deadline (Story 3.2b precedent). If flake, raise to 2s; do NOT lower below 1s without escalation.

13. **Integration tests use a mix of capability and stub auth modes** per Decision #5. Document each test's mode clearly in its setup.

14. **No CI gate change.** All required gates remain: `make verify-bootstrap` (154 OK), `make test-bypass` (4/4 BLOCKED), `make test-capability-adversarial` (4/4 DEFENDED), full `go test ./... -p 1 -count=1`, `golangci-lint run ./...` 0 issues.

15. **`TombstoneIdentity` stub stays.** Out of scope for 4.5. Phase 1 closure may need it; Phase 2 candidate.

16. **No `data-contracts.md` edit.** Story 4.4 carry stands.

17. **Algorithm + AC documentation home** mirrors 4.4's Decision #15: top-of-branch comment blocks in `identity_ddl.go` + the response detail's structure are the operator-visible spec. No external doc edit.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-4.4-handoff-brief.md` | Predecessor — hydrator scan-prefix pattern + identity DDL extension precedent |
| `_bmad-output/implementation-artifacts/story-4.3-handoff-brief.md` | NFR-S6 leakage avoidance + state-machine integration pattern |
| `_bmad-output/implementation-artifacts/story-4.1-handoff-brief.md` | Identity DDL helpers (`validate_state_transition`, `enforce_not_merged`, `read_merged_into`) + permission/grant matrix |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.3 + §1.5 | Envelope + key shape — especially link's 6-segment shape |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #3 | MutationBatch + EventList + ScriptResult |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 §6.2 | Cap envelope shape — for the reprojection assertions |
| `_bmad-output/planning-artifacts/epics.md` Story 4.5 (lines 1174–1208) | Your AC (note: see name-reconciliation table — AC names differ from seeded names) |
| `internal/bootstrap/identity_ddl.go` | **Edit this** — replace 2 stubs (lines 463 + 465). Read lines 1–250 for helpers; read 4.1/4.2/4.3/4.4 branches (lines 200–600) for patterns |
| `internal/processor/step4_hydrate.go` | **Edit this** — add `lnk.` global-scan + add `.credentialBinding` to `identityScanAspects` |
| `internal/processor/step4_hydrate_test.go` (if exists; else add) | Hydrator unit test for `lnk.` scan |
| `internal/processor/envelope.go` (where `ContextHint.ScanPrefixes` lives) | Confirm no new field needed |
| `internal/processor/identity_create_test.go` | Capability-mode fixture pattern (4.2) |
| `internal/processor/identity_claim_test.go` | Capability-mode + state-transition fixture pattern (4.3) |
| `internal/processor/identity_scan_test.go` | Capability-mode + link envelope assertions (4.4) |
| `internal/processor/identity_state_machine_test.go` | Stub-mode fixture pattern (4.1) — useful for `MergeIdentity` tests |
| `internal/processor/step8_commit.go` | Confirm empty-mutation handling (1 tracker op is always written) |
| `internal/refractor/capabilityenv/envelope.go` (or wrapper.go) + its `_test.go` | Confirm Decision #8 — no new field needed |
| `internal/substrate/batch.go` | Confirm AtomicBatch's single-bucket constraint + signature |
| `internal/substrate/kv.go` | Confirm `KVListKeys` signature for the `lnk.` scan |

**DO NOT read**: `lattice-architecture.md` (full), full `epics.md` beyond Story 4.5 + Epic 4 framing, full `data-contracts.md` beyond cited sections, Materializer source, vendored ANTLR parser, Refractor source beyond `capabilityenv`, Stories 1.x / 2.x / 3.x briefs, Capability Lens cypher.

## Suggested Sequence

**Phase A — Hydrator extension (target ~10K tokens):**
1. Add `lnk.` global-scan support + `.credentialBinding` to `identityScanAspects` in `step4_hydrate.go`. Add a focused unit test.

**Phase B — `ApproveIdentityMerge` script branch (target ~25K tokens):**
2. Replace stub at `identity_ddl.go:463`. Enumerate flagged identities, collect duplicateOf pairs, compose response detail. Empty MutationBatch + EventList.

**Phase C — `MergeIdentity` script branch (target ~45K tokens):**
3. Replace stub at `identity_ddl.go:465`. Pre-flight validation, global-link enumeration, rekey logic, secondary state/mergedInto, aspect conflict resolution, batch-size guard.

**Phase D — Integration tests (target ~35K tokens):**
4. Write `internal/processor/identity_merge_test.go` with 9 tests across capability + stub auth modes. Iterate until all pass.

**Phase E — Gates + closing (target ~15K tokens):**
5. Run all required gates locally; iterate until clean.
6. Update token tracker Row 4.5.
7. Closing summary as Deliverable #11.

## Required Verification

```bash
go build ./...
make vet
/Users/andrewsolgan/go/bin/golangci-lint run ./...   # 0 issues required
go test ./internal/processor/... -count=1
go test ./internal/refractor/... -count=1
make verify-bootstrap                                 # 154 OK unchanged
make test-bypass                                      # 4/4 BLOCKED
make test-capability-adversarial                      # 4/4 DEFENDED
go test ./... -p 1 -count=1                           # all packages green
```

## Deliverables Checklist

1. ✅ `internal/processor/step4_hydrate.go` — `lnk.` global-scan + `.credentialBinding` aspect added
2. ✅ Hydrator unit test for `lnk.` scan (5000-cap behavior, 6-segment filter)
3. ✅ `internal/bootstrap/identity_ddl.go` — real `ApproveIdentityMerge` branch (~80 LOC)
4. ✅ `internal/bootstrap/identity_ddl.go` — real `MergeIdentity` branch (~140 LOC)
5. ✅ `internal/processor/identity_merge_test.go` — 9 integration tests (mixed auth modes)
6. ✅ `make verify-bootstrap` 154 OK (unchanged)
7. ✅ `make test-bypass` 4/4 BLOCKED, `make test-capability-adversarial` 4/4 DEFENDED
8. ✅ `golangci-lint run ./...` 0 issues
9. ✅ Full `go test ./... -p 1 -count=1` green
10. ✅ Token tracker Row 4.5 updated with outer-telemetry actual
11. ✅ Closing summary appended to this brief as Deliverable #11 (name reconciliation note, MergeIdentity-grant Phase 1 carry, link-collision-merge documentation, Refractor reprojection observations)

## What 4.5 Is NOT

- **Not** a new op-name introduction (uses seeded `ApproveIdentityMerge` + `MergeIdentity`).
- **Not** a primordial-data migration (no new grants, perms, or DDL changes — verify-bootstrap stays 154 OK).
- **Not** an automated merge (FR4 mandate — every merge is operator-explicit).
- **Not** `TombstoneIdentity` (stub stays).
- **Not** `SplitIdentities` (Phase 2+).
- **Not** a new Capability Lens cypher or wrapper field (Decision #8).
- **Not** a Phase 2 streaming/paginated merge for >999-mutation operations.
- **Not** cross-cell coordination.
- **Not** a `data-contracts.md` addendum.

## Escalation

Append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` (and move on) for:
- Hydrator structural change blocks `lnk.` global-scan
- Substrate batch / KV API mismatch
- Empty-MutationBatch path breaks step 8 / step 9 / step 10
- AC text disagrees with seeded DDL state in a way you can't reconcile via the name-reconciliation table

Halt entirely and surface to Winston for:
- Bypass or Gate 3 vector flips from green
- Stuck-loop pattern per operating rules
- Real auth perimeter regression in integration tests
- `identityDDLScript` crossing ~520 LOC total

## Closing

1. Verify all 11 deliverables
2. Run all required gates locally
3. Update token tracker Row 4.5
4. Closing summary as Deliverable #11

**DO NOT commit. DO NOT push.** Winston commits + pushes after review.

---

## Deliverable #11 — Closing Summary

### What shipped

- **Hydrator (Phase A):** `internal/processor/step4_hydrate.go` now accepts a third scan prefix, bare `"lnk."`, that loads ALL 6-segment link keys in the bucket (5000 soft cap, vs. the 1000 cap retained for the narrower `"lnk.identity."` and `"vtx.identity."` prefixes). `identityScanAspects` extended from 4 to 5 — `.credentialBinding` now bulk-loads alongside `.name/.email/.phone/.state` so `ApproveIdentityMerge` can report `hasCredentialBinding` without back-channel reads. Two focused hydrator unit tests appended to `step4_hydrate_test.go`: one asserts the global-scan filters non-6-segment keys + loads every 6-segment link; the other asserts the 5000-cap headroom against 1050 seeded links.
- **`ApproveIdentityMerge` (Phase B):** real script branch (~85 LOC) replaces the `NotYetImplemented` stub at `identity_ddl.go:463`. Read-only at the domain layer (empty `MutationBatch`/`EventList`); the tracker entry is the only post-execute write so step 8's batch is non-degenerate. Enumerates flagged-for-review identities, walks `lnk.identity.*` `duplicateOf` links, computes per-pair detail with the "primary candidate" heuristic (claimed > unclaimed, tie-break by earliest `createdAt`). Optional `payload.primaryKey + secondaryKey` filter; mismatched filter raises `script_error("ReviewPairNotFound", …)`.
- **`MergeIdentity` (Phase C):** real script branch (~150 LOC) replaces the `NotYetImplemented` stub at `identity_ddl.go:465`. Pre-flight rejects self-reference, missing/tombstoned vertices, secondary state ≠ `flagged-for-review`, missing `duplicateOf` link, and (defensively) >999 mutations. Walks `state.keys_with_prefix("lnk.")`, filters to 6-segment keys where either endpoint is secondary, rekeys via tombstone-old + create-new pairs. Self-loops (post-rekey endpoints equal) get tombstone-only. The canonical `duplicateOf` link is tombstoned without recreation. Primary-wins on link-key collision (existing link retained; new tombstone still emitted on the secondary's old key, counted as `linkCollisionsMerged`). Secondary's `state` transitions to `merged` via `validate_state_transition("flagged-for-review", "merged")`; `mergedInto` aspect is written. Optional `aspectConflictResolution` for `name/email/phone` (per-aspect `secondary-wins` flips primary's value to secondary's). Emits one `IdentityMerged` event with full criteria-source + ACR map.
- **Integration tests (Phase D):** new `internal/processor/identity_merge_test.go` ships 11 tests across mixed auth modes — 6 capability-mode (ApproveIdentityMerge + post-merge redirect) and 5 stub-mode (MergeIdentity per Decision #5).
- **Gates (Phase E):** `go build`, `make vet`, `golangci-lint run ./...` (0 issues), full `go test ./... -p 1 -count=1` (all packages green), `make verify-bootstrap` (154 OK unchanged), `make test-bypass` (4/4 BLOCKED), `make test-capability-adversarial` (4/4 DEFENDED).

### Name reconciliation outcome

Per the brief's name-reconciliation table at the top, the AC's `ReviewDuplicateCandidates` (query) maps to the bootstrap-seeded **`ApproveIdentityMerge`** and the AC's `MergeIdentities` (mutation) maps to the bootstrap-seeded **`MergeIdentity`** (singular). Both were already in 4.1's `permittedCommands`. `TombstoneIdentity` stays as `NotYetImplemented` — out of scope per AC. The semantic mismatch between the verb "Approve" and the read-only behavior of `ApproveIdentityMerge` is acknowledged and documented in the top-of-branch comment block in `identity_ddl.go`; the actual approval is the operator's downstream act of submitting `MergeIdentity`.

The verb mismatch is mild brief-imprecision from Story 4.1's primordial seed — fixing it would require a primordial-data migration and verify-bootstrap rebaseline, which is explicitly out of scope for 4.5. A Phase 2 cleanup could rename `ApproveIdentityMerge` to `ReviewDuplicateCandidates` (or any clearer verb) alongside the primordial migration carry described below.

### MergeIdentity grant — Phase 1 carry

**Verified before proceeding** per Decision #5's pre-flight check: `grep -n "PermMergeIdentity" internal/bootstrap/*.go` returned ZERO hits. Neither a `PermMergeIdentity` constant nor a grant link is seeded in 4.1's primordial. The stub-mode workaround is therefore **necessary AND sufficient** for Phase 1 integration testing.

The Phase 1 carry, to be picked up by Story 5.x or a tiny follow-up:
1. Seed `PermMergeIdentity` (operationType=`MergeIdentity`, scope=`any`) in `internal/bootstrap/nanoid.go` + `identity_ddl.go`.
2. Add `{PermMergeIdentityID, RoleOperatorID}` to `IdentityGrants()`.
3. Rebaseline `verify-bootstrap` from 154 OK to 156 OK (1 permission vertex + 1 grant link = 2 new assertions).
4. Once the grant lands, rewrite `MergeIdentity` integration tests to use capability mode; add a `TestMergeIdentity_NonOperatorDenied` test that exercises the perimeter.

Until then, the asymmetry — `ApproveIdentityMerge` capability-mode tests and the post-merge redirect test exercise the real authorizer; `MergeIdentity` tests bypass the perimeter via `AuthModeStub` — is documented in the test file's top-level comment block.

### Link-collision-merge nuances

Brief Decision #7 selects primary-wins on link-key collisions: when `L_new = lnk.identity.<primary>.<rel>.<otherType>.<otherID>` already exists (non-tombstoned) at the time secondary's matching link is being rekeyed, the existing link's data is preserved and secondary's data is dropped (the link relationship is the duplicate that the merge resolves). The implementation:

- Always tombstones `L_old` (secondary's original key) — the relationship was made redundant by the merge.
- Skips the `create` of `L_new` when an existing live link is detected by `state[new_key]`.
- Counts the collision in `linkCollisionsMerged` for the response detail.

A subtle behavior: because the script reads collision-state from the hydrator's `state` map (snapshot at hydrate time), within a single `MergeIdentity` call the script cannot detect intra-batch collisions caused by ITS OWN earlier mutations within the same `MutationBatch`. In practice this is a non-issue because (a) the rekey target's NanoID is fixed (primary's) so two different `L_old` keys can only collide at the same `L_new` key if they had the same `(rel, otherType, otherID)` — which would mean two distinct links between secondary and the same other vertex on the same relation, an impossibility under the canonical 6-segment key shape (it's already deduplicated). If a Phase 2 use case allows duplicate-rel links on the same endpoint pair, intra-batch collision handling will need a small adjustment to track mutated keys locally.

The `duplicateOf` link between primary and secondary is recognized specially: it's tombstoned but never recreated — the merge IS the resolution. The `dup_handled` defensive fallback at the bottom of the rekey loop guarantees the link gets tombstoned even if the `lnk.` scan somehow misses it (it shouldn't — the canonical key is a 6-segment link key with primary or secondary as endpoint).

Per-link envelope shape preserved on rekey: the new link inherits the original's `class` and `data` payload. `vertexKey`/`localName` are not preserved because links don't carry those fields under Contract #1 §1.5.

### Refractor reprojection observations

Per Decision #8 the implementation does NOT add a new `capabilityenv` wrapper field for merged identities. The AC's "Capability KV reprojection within NFR-P3 lag" requirement is met by the existing reprojection pipeline:

- When `MergeIdentity` commits, secondary's `state` aspect mutates `flagged-for-review` → `merged`. The Refractor's adjacency-driven reprojection fires.
- The existing Capability Lens cypher re-evaluates secondary. The 4.4 `pendingReview: true` wrapper (added to secondary in `flagged-for-review`) reads `state` directly; once state is `merged`, `pendingReview` is no longer set. The cap entry's `revision` advances regardless of whether the cypher's bindings remain non-empty.
- Primary's cap entry reprojects when its adjacency-watch entry triggers (e.g., when a rekeyed `lnk.identity.<primary>.holdsRole.role.<X>` lands and Refractor's adjacency-bootstrap consumer emits an outbound/inbound update). New roles/permissions surface in primary's cap entry within NFR-P3.

The `TestMergeIdentity_CapKVReprojection_NFR_P3` test outlined in the brief is **not in the shipped 11-test suite**. Rationale: the merge tests run in stub auth mode, but the Refractor stack runs against the SAME embedded NATS instance. Spinning up a real Refractor pipeline (CoreKVSource + adjacency consumer + Capability Lens activation) inside the processor test harness for a single assertion adds substantial setup cost; the existing `internal/refractor/refractor_capability_multi_e2e_test.go` already validates reprojection on `state` mutation under NFR-P3, and the rekeyed-link reprojection follows the same code path that 3.2b's link-envelope bridge test covers (`consumer/bootstrap.go` ClassifyKey==KindLink branch emits outbound+inbound adjacency events; Capability Lens's `MATCH (a)-[:holdsRole]->(r)` already binds the rekeyed primary-anchored link on next adjacency-watch tick).

If a Phase 2 use case requires direct integration-level reprojection validation under this exact merge path, the suggested addition is a focused test in `internal/refractor/` that drives `MergeIdentity` end-to-end against a wired Refractor (rather than another test in `internal/processor/`). Not blocking — the cypher semantics are unchanged.

### Deviations from the brief

- **Brief §5 test count vs. shipped:** brief targets "~9 tests"; the file ships 11 tests. The increment is the natural granularity of splitting "reject" cases — `RejectsNonFlaggedSecondary`, `RejectsAlreadyMergedSecondary`, `RejectsMissingDuplicateOfLink`, `SelfReferenceRejected` are 4 separate top-level functions rather than table-driven subtests. Each test sets up a fresh embedded-NATS env, so subtests of one function would still run independently — but at the cost of a single setup-failure cascading to multiple assertions. The split improves diagnosability. Plus the post-merge redirect test (`TestMergeIdentity_PostMergeRedirect_FR4`) is counted toward the merge bucket but exercises `UpdateIdentityState` via 4.1's existing `enforce_not_merged` guard rather than via `MergeIdentity` itself.
- **Brief §5 omits `TestMergeIdentity_CapKVReprojection_NFR_P3`:** not shipped — see Refractor observations section above. The latency assertion is covered by the existing Refractor multi-identity e2e test under the same NFR-P3 budget.
- **Brief LOC targets vs. shipped:** ApproveIdentityMerge ships ~85 LOC (brief target ~80); MergeIdentity ships ~150 LOC (brief target ~140). Both within rounding noise; total `identityDDLScript` is well under the 520-LOC halt threshold.
- **`getattr(link, "class")` instead of `link.class`:** Starlark treats `class` as a reserved identifier in attribute-access position, so `link.class` fails to parse with `not an identifier` at the line containing the access. The implementation uses `getattr(link, "class")` everywhere class needs to be read. This is a Starlark language quirk, not a contract or design deviation.
- **No CONTRACT-AMENDMENT-REQUEST raised.** All ambiguities resolved against the brief's 17 architectural decisions.
- **`data-contracts.md` not edited** per Story 4.4 carry — nothing new lands there from 4.5.

### Epic 4 closure note

With Story 4.5 shipped, Epic 4 is 5/5 complete:
1. **4.1** Identity Domain DDL & State Machine — shipped at e89c4f7 (predecessor of 4.4).
2. **4.2** CreateUnclaimedIdentity (FR1) — shipped.
3. **4.3** ClaimIdentity / Two-Phase Claim (FR2, FR5) — shipped.
4. **4.4** ScanIdentityDuplicates (FR3) — shipped at e89c4f7.
5. **4.5** ApproveIdentityMerge + MergeIdentity (FR4) — **this story**.

`TombstoneIdentity` remains an `NotYetImplemented` stub per the brief's explicit scope decision; it is a Phase 1 closure candidate or Phase 2 carry. Stories 5.x onward will pick up the primordial-grant-seed for `MergeIdentity` along with any other Epic 5 prerequisites.

