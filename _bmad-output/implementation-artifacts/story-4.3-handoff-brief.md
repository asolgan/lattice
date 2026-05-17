---
title: Story 4.3 Implementation Handoff Brief
story: 4.3 — Two-Phase Identity Claim (FR2, FR5)
model_tier: Sonnet (locked)
token_budget: ~100K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-17
predecessor: Story 4.2 (Staff Creates Unclaimed Identity, shipped at 7462fc7; progress update at 182c9a2)
---

# Story 4.3 — Two-Phase Identity Claim (FR2, FR5): Handoff Brief

## Your Role

Replace the `ClaimIdentity` `NotYetImplemented` stub in the identity DDL Starlark script (shipped by 4.1) with a real implementation. The submitter is a consumer-role actor who has registered a credential and holds a plaintext claim key delivered out-of-band by staff during 4.2's create flow. Your work:

1. **Hash + constant-time-compare** the submitted plaintext claim key against the SHA-256 hash stored in the target identity's `claimKey` aspect (written by 4.2).
2. **Validate target state == "unclaimed"** (claimed / flagged-for-review / merged all return the generic error).
3. **Validate the consumer actor has not already bound a different identity** (lookup `vtx.credentialindex.<sha256NanoID(actorKey)>` index vertex).
4. **On success**: write `credentialBinding` aspect, transition `state` to `claimed`, tombstone the `claimKey` aspect (one-time-use via `isDeleted: true`), create the `credentialindex` vertex, emit `IdentityClaimed` event.
5. **Generic `ClaimKeyInvalid` error for every failure mode** — no enumeration leakage (NFR-S6). Specific outcome surfaces only via Health KV signal at `health.processor.<instance>.claim-attempts.<outcome>` for operator observability.
6. **Grandfathered case (FR5)**: no special code path; same script branch handles it. Provenance differs only in the envelope's `createdByOp`.

After 4.3 ships, Stories 4.4 (Levenshtein duplicate detection batch) and 4.5 (operator-approved merge) close Epic 4.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

- **No worktree.** Work directly in `/Users/andrewsolgan/Documents/GitHub/Lattice` against branch `main`. Verify with `pwd` at startup.
- **No commits, no pushes.** Stage your changes; DO NOT call `git commit` or `git push`. Winston commits + pushes after review.
- **Planning artifacts are read-only.** Drift → append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` and continue.
- **Token budget is for tracking only, NOT a halt threshold.** Estimate ~100K. Record outer-telemetry actual at session close.
- **Halt and escalate** on stuck-loop patterns (re-attempting after 3+ failures; immediate reverts; cycling between failed approaches; stuck on a test fail you can't reduce after two debug attempts).
- **Checkpoint every 8-10 tool calls OR after any deliverable OR after any file read >25KB.**
- **Model tier:** Sonnet only. Halt if Opus/Haiku.
- **Architecture binding:** Contract #1 §1.3 + §1.5 (envelopes + keys), Contract #3 (script return shape), Contract #6 (cap doc shape — only for the FR5 reprojection test), epics.md Story 4.3 (lines 1108-1134), FR2, FR5, NFR-S6 (no enumeration leak), NFR-S7 (PII not in logs).
- **Token tracker:** update Row 4.3 at session close with outer-telemetry actual.
- **Andrew has authorized autonomous proceed.**

## What's Already in Place (do NOT redo)

- **Identity DDL** (Story 4.1) — `permittedCommands` already includes `ClaimIdentity`. **No DDL surface change needed.**
- **Identity DDL script** at `internal/bootstrap/identity_ddl.go` — stub branch for `ClaimIdentity` returns `script_error("NotYetImplemented", "Story 4.3: ClaimIdentity")`. **Replace that branch.**
- **`ClaimIdentity` permission** (vtx.permission.<ClaimIdentityID>, scope=`self`) granted to `consumer` role (1 link). Capability Lens projects it; consumer-role actor passes step 3 by exact-operationType match. Scope=self enforcement happens here in 4.3's Starlark validator (not in step 3).
- **`crypto.sha256(s)` + `crypto.sha256NanoID(s)`** Starlark builtins (Story 4.2). **Reuse `crypto.sha256` for plaintext → hash comparison.**
- **`ScriptResult.ResponseDetail` + `OperationReply.Detail`** plumbing (Story 4.2) — usable for success-reply payload if needed (e.g., to echo `identityKey`).
- **`HealthAlertEmitter`** (Story 3.3) at `internal/processor/health_alert_emitter.go` — emits `health.alerts.security.<topic>`. **Extend with `RecordClaimAttempt(outcome string)`** writing to `health.processor.<instance>.claim-attempts.<outcome>` per Decision #6 below.
- **`vtx.identity.<X>.claimKey` aspect** (Story 4.2) stores `{hash: <sha256-hex>, algo: "sha256"}`. Plaintext was returned to staff exactly once in 4.2's response.
- **Soft-delete tombstone semantics** — substrate writes `isDeleted: true` on the envelope (no special bucket / no hard delete). Read paths check the field and skip tombstoned values.
- **Capability Lens** is invisible to identity-claim mutations *per se* — but a claimed identity's actor now has the identity's role-derived cap doc. FR5 test verifies a follow-up op succeeds.
- **HealthAlertEmitter wiring** in `MakePipeline` — the emitter is already constructed (Story 3.3) and threaded into step 3. Thread it into the executor (step 5) for the new `RecordClaimAttempt` calls.

Tree is clean at session start (commit `182c9a2` after `7462fc7`; verify-bootstrap 154 OK; test-bypass 4/4 BLOCKED; test-capability-adversarial 4/4 DEFENDED; full `go test ./... -p 1 -count=1` green).

## Story Scope (4.3)

### 1. New Starlark sandbox builtin: `crypto.constant_time_equal(a, b)`

In `internal/processor/starlark_builtins.go`:
- Add to `cryptoModule()` (already exists from 4.2).
- Signature: `crypto.constant_time_equal(a: string, b: string) -> bool`.
- Implementation: `subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1`. Length mismatch returns `False` (NOT constant-time across length; the timing leak reveals only length, not content — acceptable Phase 1, document in closing summary).
- Unit test in `starlark_builtins_test.go` (or whichever file 4.2 added): equal/unequal/length-mismatch cases.

### 2. New Health KV emitter method: `RecordClaimAttempt(outcome string)`

In `internal/processor/health_alert_emitter.go` (Story 3.3):
- New method `RecordClaimAttempt(ctx, outcome string)`.
- Best-effort `KVPut` (NOT KVPutWithTTL — claim-attempts is a counter, not a session-scoped trace) to `health.processor.<instance>.claim-attempts.<outcome>` with value `{count: N, lastAt: <RFC3339>}`. Counter increments via read-modify-write — under contention, last writer wins (Phase 1 acceptable; precise counting is Phase 2).
- Outcome enum (string literals): `success`, `invalid-key`, `wrong-state`, `flagged`, `merged`, `credential-already-bound`, `no-target` (hydration miss before script).

The emitter is constructed in `MakePipeline` and threaded through `Deps`. **You need to thread it into the executor** (`step5_execute.go`) since the executor is what inspects the ScriptError code and surfaces the outcome. Today the executor returns `ScriptError` and the commit_path turns it into a reply. Add an emit call at the executor → reply transition, gated on `result.ScriptError != nil && result.ScriptError.Code == "ClaimKeyInvalid"`.

**Alternative wiring (simpler):** the commit_path itself handles the emit call when it builds the reply for `ClaimKeyInvalid`. Pick whichever is less invasive. **Do NOT add per-aspect side-channels in Starlark** — the script's job is to produce MutationBatch/EventList/ScriptError; observability emission lives in Go.

### 3. Surfacing the specific outcome from script to emitter

The script returns generic `ClaimKeyInvalid` to the caller (NFR-S6 — no enumeration). But the executor needs to know which specific outcome to emit to Health KV. Use the ScriptError's **detail** field as a side-channel:

- Script: `fail_claim("invalid-key")` helper → raises `script_error("ClaimKeyInvalid", "invalid-key")`.
- Go-side handler: reads `script_error.Detail`, calls `RecordClaimAttempt(detail)`, then **strips the detail before building the reply** (the caller sees `code: "ClaimKeyInvalid"` with empty detail).
- On success: separate path — `RecordClaimAttempt("success")` on commit-path success branch (only for `ClaimIdentity` operationType; gate on op type).

**NFR-S6 audit point:** make sure no log line or response carries the specific outcome plaintext to the caller. Audit `reply.go` and any logging in `step5_execute.go` / `commit_path.go`. The detail field exists for emitter use ONLY and is stripped before egress.

### 4. The `ClaimIdentity` script branch

Replace the stub in `identityDDLScript`. The branch must:

**Input validation:**
- `op.payload.claimKey` is a non-empty string → else `fail_claim("invalid-key")` (treat as generic; do NOT distinguish "missing claim key" from "wrong claim key").
- `op.payload.targetIdentityKey` is a non-empty string matching `vtx.identity.*` pattern → else `fail_claim("no-target")`. (Pattern check is a string-prefix check; `substrate.ClassifyKey` is Go-side and not exposed to Starlark.)

**Hydrate target identity** (via `op.contextHint`, caller-precomputed per the 4.2 pattern):
- Caller pre-populates `contextHint.vertices = [targetIdentityKey, "vtx.credentialindex." + sha256NanoID(actorKey)]`.
- Caller pre-populates `contextHint.aspects = [targetIdentityKey + ".state", targetIdentityKey + ".claimKey", targetIdentityKey + ".credentialBinding", targetIdentityKey + ".mergedInto"]`.

**Inside the script:**
- `target_vertex = state.read(targetIdentityKey)` → if `None` or `.isDeleted == True` → `fail_claim("no-target")`.
- `state_aspect = state.read(targetIdentityKey + ".state")` → if `None` → `fail_claim("no-target")`; if `.data.value != "unclaimed"` → outcome-specific branch:
  - `claimed` → `fail_claim("wrong-state")`
  - `flagged-for-review` → `fail_claim("flagged")`
  - `merged` → `fail_claim("merged")` (do NOT use `enforce_not_merged` here — it would leak mergedInto in the error; we conflate to generic).
- `cred_index = state.read("vtx.credentialindex." + crypto.sha256NanoID(actorKey))` → if non-`None` AND `.isDeleted != True` → `fail_claim("credential-already-bound")`.
- `claim_key_aspect = state.read(targetIdentityKey + ".claimKey")` → if `None` OR `.isDeleted == True` → `fail_claim("invalid-key")`.
- `submitted_hash = crypto.sha256(op.payload.claimKey)`.
- `stored_hash = claim_key_aspect.data.hash`.
- `if not crypto.constant_time_equal(submitted_hash, stored_hash):` → `fail_claim("invalid-key")`.

**On success** (build MutationBatch + EventList):
- Aspect update `targetIdentityKey + ".credentialBinding"`, data `{actorKey: op.envelope.actorKey, boundAt: op.envelope.observedAt}`.
- Aspect update `targetIdentityKey + ".state"`, data `{value: "claimed"}`.
- Aspect tombstone `targetIdentityKey + ".claimKey"` → MutationOp with `isDeleted: true` flag (check how 4.2 wrote aspect mutations; the substrate envelope helpers accept an `isDeleted` field — verify in `envelope.go`).
- Index vertex create `"vtx.credentialindex." + crypto.sha256NanoID(actorKey)`, class `credentialindex`, data `{actorKey, identityKey: targetIdentityKey, boundAt}`.
- EventList: `IdentityClaimed` with data `{identityKey: targetIdentityKey, actorKey}` — does NOT include plaintext claim key (NFR-S7 / NFR-S6).
- Optional `responseDetail = {identityKey: targetIdentityKey}` for caller convenience (no plaintext in the success response either — success simply confirms; the claim key is now consumed).

**Script LOC target:** ~50 LOC for this branch. Total identityDDLScript (4.1 + 4.2 + 4.3) should remain <230 LOC. If you cross ~250 LOC, escalate before adding a shared-library mechanism.

**Helper function `fail_claim(outcome)`** at script top: just `fail("ClaimKeyInvalid", outcome)` or equivalent. Centralizes the convention.

### 5. Scope=self enforcement

The `ClaimIdentity` permission's data carries `scope: "self"` (seeded in 4.1). In Phase 1 the auth step matches operationType only; scope semantics are enforced here in the script.

**The semantic** of `scope=self` for ClaimIdentity is **NOT** "actor == target identity" (the whole point of claim is that they're different entities at the start; the consumer is the actor; the target is the unclaimed identity). The right semantic is "the consumer is binding *one credential* to *one identity*" — i.e., the credentialindex prevents one consumer from binding multiple identities. That's already enforced via the credentialindex lookup in §4.

There is no further `scope=self` check in 4.3. Document in the closing summary: scope=self for ClaimIdentity is realized as "one consumer can bind exactly one identity" via the credentialindex.

### 6. Integration tests in `internal/processor/identity_claim_test.go` (NEW file)

Capability-mode wiring (mirror `identity_create_test.go` from 4.2). Seed a consumer-role test actor + a staff-role test actor (the staff actor creates the unclaimed identity in the test arrange phase — chain a 4.2 op then a 4.3 op).

- **`TestClaimIdentity_Success`**: staff creates unclaimed identity returning plaintextKey; consumer submits ClaimIdentity with plaintextKey; assert step 8 commit, state aspect == "claimed", credentialBinding aspect set with consumer's actorKey, claimKey aspect now `isDeleted: true`, credentialindex vertex exists, IdentityClaimed event published, Health KV `claim-attempts.success` count incremented.
- **`TestClaimIdentity_WrongKey_GenericError`**: consumer submits with garbage plaintextKey; assert generic ClaimKeyInvalid (no detail/no enumeration), state unchanged, no credentialindex written, Health KV `claim-attempts.invalid-key` incremented.
- **`TestClaimIdentity_AlreadyClaimed_GenericError`**: pre-state identity has `state=claimed`; submit ClaimIdentity; assert generic ClaimKeyInvalid, Health KV `claim-attempts.wrong-state` incremented.
- **`TestClaimIdentity_Flagged_GenericError`**: pre-state identity has `state=flagged-for-review`; assert generic ClaimKeyInvalid, Health KV `claim-attempts.flagged` incremented.
- **`TestClaimIdentity_Merged_GenericError`**: pre-state identity has `state=merged` and `mergedInto` set; assert generic ClaimKeyInvalid (NOT IdentityMerged — NFR-S6 anti-enumeration), Health KV `claim-attempts.merged` incremented.
- **`TestClaimIdentity_CredentialAlreadyBound_GenericError`**: pre-seed credentialindex for the consumer's actorKey → submit ClaimIdentity for a different unclaimed identity; assert generic ClaimKeyInvalid, Health KV `claim-attempts.credential-already-bound` incremented.
- **`TestClaimIdentity_FR5_GrandfatheredFlow`**: create unclaimed identity via direct primordial-style write (simulating historical import, bypassing the 4.2 op); claim via 4.3 op; assert identical success behavior (no special code path); assert `createdByOp` provenance differs between this test and `TestClaimIdentity_Success` (the grandfathered case has no creating op envelope — or a different `createdBy`; verify the field is present and merely differs in value).
- **`TestClaimIdentity_FR5_ImmediateAccess`**: chain create→claim→submit a follow-up op as the claimed identity. The follow-up op tests that the actor key (now bound to the claimed identity) can submit ops with `actorKey = claimedIdentityKey` and pass step 3 with the target identity's cap doc. **Caveat:** if the test's target identity has no role-permission grants (no holdsRole link), there's nothing to authorize. Solution: pre-seed a `lnk.identity.<targetIdentityKey-id>.holdsRole.role.<RoleConsumerID>` link before the claim, then submit a follow-up `ClaimIdentity` (no — that's circular). Easier: assert that `cap.<targetIdentityKey-id>` is reachable in the Capability KV after the claim (no permissions needed in the cap doc itself; just prove the actor can be looked up). If a follow-up op is needed for the assertion, use a no-op op type like a second `ClaimIdentity` against another unclaimed-identity that would now fail because of the credentialindex — proves the *first* claim's index was registered.

   Pragmatic alternative: the FR5 immediate-access test asserts that after claim, the cap doc for the *target identity* exists in Capability KV (waiting up to NFR-P3 = 500ms for Refractor reprojection). This is light-weight evidence of FR5 behavior without a follow-up op gymnastics.

Total ~8 tests.

### 7. Verify-bootstrap

No new primordial entries in 4.3. The 154 OK count should remain 154.

### 8. Bypass-suite re-audit

Confirm `make test-bypass` (4/4 BLOCKED) and `make test-capability-adversarial` (4/4 DEFENDED) remain green. The new `crypto.constant_time_equal` builtin is pure / deterministic — no I/O escape risk. If Bypass #3 flips, STOP and escalate.

**Out of scope:**
- Story 4.4 (Levenshtein fuzzy duplicate-detection batch).
- Story 4.5 (Approved merge).
- Tombstone semantics for credentialindex vertices (they accumulate; cleanup is later story).
- Multi-credential binding per actor (one-credential-one-identity is the Phase 1 model).
- Anti-replay across requestIds (step 2 dedup handles same-requestId replay; cross-requestId replay of the same plaintext is prevented by the claimKey-aspect tombstone).
- Rate-limiting / throttling (operator-side concern; Phase 2).
- Refractor / Capability Lens changes.
- The "wait up to 500ms for cap reprojection" assertion may need a poll loop; do NOT introduce a sleep — poll with a 50ms tick and 1s deadline (Story 3.2b precedent).
- Cross-cell coordination (single-cell MVP only).

**Hard escalation triggers (append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` then move on):**
- `MutationOp` doesn't support an `isDeleted` field for aspect tombstone (would require envelope/substrate change).
- The executor → reply path doesn't expose ScriptError detail to a side-channel emitter without invasive changes (suggest narrower mechanism in CAR).
- Pre-existing Health KV emitter shape can't accept a counter increment without an unrelated refactor.
- Bypass or Gate 3 vector flips from green.
- AC's "constant-time comparison" assertion is not testable end-to-end (timing assertions in Go tests are flaky; accept that the unit test for `crypto.constant_time_equal` is the proof — the integration tests do NOT assert timing).

## Architectural Decisions Already Made (Winston)

1. **Hash storage from 4.2 is the comparison anchor.** The script computes `crypto.sha256(submitted_plaintext)` and constant-time-compares to `claim_key_aspect.data.hash`. No plaintext claim key ever lands in Core KV; this is the realization of AC's "read-restriction" clause.

2. **`crypto.constant_time_equal(a, b)` is a 4.3-introduced builtin.** Pure / deterministic / no I/O — same sandbox-principles compliance as 4.2's hash builtins.

3. **Generic `ClaimKeyInvalid` for every failure path.** NFR-S6 — no enumeration: callers cannot distinguish wrong-key from wrong-state from already-claimed from merged. Specific outcome surfaces ONLY via Health KV signal for operator-side observability. Tests assert generic message AND specific health metric.

4. **ScriptError detail field as side-channel** from script → executor → Health KV emitter. The detail is **stripped before egress**; only the executor reads it for the emit call. Audit `reply.go` to confirm detail-stripping for ClaimKeyInvalid.

5. **Health KV path: `health.processor.<instance>.claim-attempts.<outcome>`** with `{count, lastAt}` shape. Counter is read-modify-write best-effort (Phase 1 acceptable).

6. **Outcome enum (string literals)**: `success`, `invalid-key`, `wrong-state`, `flagged`, `merged`, `credential-already-bound`, `no-target`. Test every outcome.

7. **One-credential-one-identity** is enforced via `vtx.credentialindex.<sha256NanoID(actorKey)>` index vertex. Class `credentialindex` (lowercase, per the 4.2 CAR resolution for type segments). Scope=self for ClaimIdentity is realized as this constraint; there is no actor==target check (semantically wrong for claim).

8. **`claimKey` aspect tombstone via `isDeleted: true`** on the aspect envelope. One-time-use enforcement: a second ClaimIdentity on the same target after a successful claim reads `isDeleted=True` and returns generic ClaimKeyInvalid with outcome `invalid-key`. NOTE: the state transitioned to `claimed` simultaneously, so the second attempt would also hit the `wrong-state` branch first. Order the script: state check BEFORE claimKey check, so a re-attempt yields `wrong-state` not `invalid-key`. (Phase 1 acceptable; the alternative leaks no additional info — both are generic.)

9. **FR5 grandfathered case has no special code path.** Provenance differs only in envelope `createdByOp` / `createdBy` field (whatever the substrate uses). Same script branch, same MutationBatch shape, same EventList.

10. **`scope=self` semantics for ClaimIdentity** are realized as "one credential per identity" (credentialindex) — NOT as `actor == target` (which would be wrong, since the consumer is binding *to* the target). Document in closing summary.

11. **`enforce_not_merged` helper from 4.1 is NOT used in ClaimIdentity.** The merged-state check is conflated to the generic error path (NFR-S6 anti-enumeration), not the IdentityMerged-explicit path that other 4.x ops use. Other ops (UpdateIdentityState, future MergeIdentity in 4.5) keep using `enforce_not_merged` for explicit merge-aware semantics. Document.

12. **CapabilityLens reprojection latency for FR5** is observable but not asserted as a hard SLA in 4.3. The integration test polls for `cap.<targetIdentityKey-suffix>` to exist with a 1s deadline (Story 3.2b precedent: actual p99 ~5.7ms). If it times out at 1s, escalate.

13. **NFR-S6/S7 audit responsibility**: confirm no log line or response carries the specific outcome plaintext or the ScriptError detail to the caller. Audit `reply.go`, `commit_path.go`, `step5_execute.go`.

14. **Idempotency** for same-requestId resubmission is handled by step 2 dedup (Story 1.5/1.7). Test: same requestId twice → second submission short-circuits via tracker, no double-claim, no Health metric double-emit (because step 2 returns before step 5 runs).

15. **Test fixtures use capability mode**, NOT stub mode. Mirror 4.1's `identity_state_machine_test.go` and 4.2's `identity_create_test.go` for consumer + staff actor seeding.

16. **No CI gate change.** All of: `make verify-bootstrap` (154 OK unchanged), `make test-bypass` (4/4 BLOCKED), `make test-capability-adversarial` (4/4 DEFENDED), full `go test ./... -p 1 -count=1`.

17. **No new CONTRACT-AMENDMENT-REQUEST expected.** If one emerges, append + move on.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-4.2-handoff-brief.md` | Predecessor — most directly analogous template (script branch + crypto builtin + contextHint pattern + reply.Detail) |
| `_bmad-output/implementation-artifacts/story-4.1-handoff-brief.md` | Identity DDL helpers (`enforce_not_merged` reference — explicitly NOT used in 4.3) |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.3 + §1.5 | Envelope + key shape; aspect tombstone via isDeleted |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #3 | MutationBatch + EventList + ScriptResult |
| `_bmad-output/planning-artifacts/epics.md` Story 4.3 (lines 1108-1134) | Your AC |
| `internal/bootstrap/identity_ddl.go` | **Edit this** — replace ClaimIdentity stub branch |
| `internal/processor/starlark_builtins.go` | **Edit this** — add `crypto.constant_time_equal` |
| `internal/processor/starlark_builtins_test.go` | Add unit test for the new builtin |
| `internal/processor/health_alert_emitter.go` | **Edit this** — add `RecordClaimAttempt` |
| `internal/processor/script_context.go` | ScriptError struct (Code + Detail) |
| `internal/processor/step5_execute.go` | Where ScriptError is constructed; possibly add the detail-strip + emit hook |
| `internal/processor/commit_path.go` | Where reply is built; possibly the emit-and-strip hook lives here |
| `internal/processor/reply.go` | Reply envelope shape; confirm detail is not auto-logged |
| `internal/processor/envelope.go` | MutationOp shape — confirm `isDeleted` field for aspect tombstone |
| `internal/processor/identity_create_test.go` | Fixture pattern from 4.2 |
| `internal/processor/identity_state_machine_test.go` | Fixture pattern from 4.1 |

**DO NOT read**: `lattice-architecture.md` (full), full `epics.md` beyond Story 4.3 + Epic 4 framing, full `data-contracts.md` beyond cited sections, Materializer source, vendored ANTLR parser, Refractor source, Stories 1.x/2.x/3.1-3.5 briefs, Capability Lens cypher.

## Suggested Sequence

**Phase A — Sandbox builtin (target ~7K tokens):**
1. Add `crypto.constant_time_equal` to `starlark_builtins.go`. Unit test.

**Phase B — Health emitter extension (target ~10K tokens):**
2. Add `RecordClaimAttempt(ctx, outcome)` to `health_alert_emitter.go`. Thread into MakePipeline / commit_path Deps so the executor or commit_path can emit.

**Phase C — Detail-side-channel + reply audit (target ~10K tokens):**
3. Decide whether the emit hook lives in `step5_execute.go` or `commit_path.go`. Pick the less invasive site.
4. Wire detail extraction → emit → strip-from-reply. Audit `reply.go` confirms detail is not logged.

**Phase D — Identity script branch (target ~25K tokens):**
5. Replace `ClaimIdentity` stub in `identity_ddl.go`. Validate inputs, hydrate via state.read, run all 7 outcome checks, build MutationBatch + EventList on success.

**Phase E — Integration tests (target ~30K tokens):**
6. Write `internal/processor/identity_claim_test.go` with 8 tests. Iterate until all pass.

**Phase F — Gates + closing (target ~10K tokens):**
7. Run all required gates locally; iterate until clean.
8. Update token tracker Row 4.3.
9. Closing summary appended to brief as Deliverable #11.

## Required Verification

```bash
go build ./...
make vet
go test ./internal/processor/... -count=1
make verify-bootstrap                        # 154 OK unchanged
make test-bypass                             # 4/4 BLOCKED
make test-capability-adversarial             # 4/4 DEFENDED
go test ./... -p 1 -count=1                  # all packages green
```

## Deliverables Checklist

1. ✅ `internal/processor/starlark_builtins.go` — `crypto.constant_time_equal`
2. ✅ `internal/processor/starlark_builtins_test.go` — unit test for the new builtin
3. ✅ `internal/processor/health_alert_emitter.go` — `RecordClaimAttempt`
4. ✅ Wiring through MakePipeline / commit_path so the executor or commit_path can emit on ClaimKeyInvalid
5. ✅ `reply.go` audit — detail not logged; detail stripped before egress for ClaimKeyInvalid
6. ✅ `internal/bootstrap/identity_ddl.go` — real `ClaimIdentity` branch
7. ✅ `internal/processor/identity_claim_test.go` — 8 integration tests
8. ✅ `make verify-bootstrap` 154 OK
9. ✅ `make test-bypass` 4/4 BLOCKED
10. ✅ `make test-capability-adversarial` 4/4 DEFENDED
11. ✅ Token tracker Row 4.3 updated with outer-telemetry actual
12. ✅ Closing summary appended to brief as Deliverable #12

## What 4.3 Is NOT

- **Not** Levenshtein fuzzy duplicate detection (4.4 batch).
- **Not** Operator-approved merge (4.5).
- **Not** Multi-credential binding per actor — one-credential-one-identity.
- **Not** Anti-replay across requestIds beyond what claimKey-aspect tombstone provides.
- **Not** Rate-limiting / throttling.
- **Not** Refractor or Capability Lens change.
- **Not** Cross-cell coordination.
- **Not** Specific-outcome enumeration leak in caller responses — generic only.

## Escalation

Append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` (and move on) for:
- MutationOp doesn't support `isDeleted` for aspect tombstone
- Executor → reply path can't expose ScriptError detail to side-channel emitter without invasive change
- Health KV counter increment requires unrelated refactor
- AC text disagrees with contract text in a non-trivial way

Halt entirely and surface to Winston for:
- Bypass or Gate 3 vector flips from green
- Stuck-loop pattern per operating rules
- NFR-S6 audit reveals an outcome-leak path that can't be closed

## Closing

1. Verify all 12 deliverables
2. Run all required gates locally
3. Update token tracker Row 4.3
4. Closing summary as Deliverable #12

**DO NOT commit. DO NOT push.** Winston commits + pushes after review.

---

## Deliverable #12 — Closing Summary

**Session date:** 2026-05-17  
**Model tier:** Sonnet (claude-sonnet-4-6)  
**Total token estimate:** ~75K (continuation; prior session ~40K + this session ~35K)

### Bugs Fixed

**Bug A — Success paths rejected (root cause: HydrationMiss before script)**  
`ContextHint.Reads` included keys that don't exist at claim time: `identityKey + ".credentialBinding"`, `identityKey + ".mergedInto"`, and `credIndexKey` (the credential-index vertex). The step-4 hydrator treats any missing `ContextHint.Reads` key as a hard `HydrationMiss` error, so the script never ran. Fix: removed these optional/absent keys from `Reads` in all claim test envelopes. The Starlark script handles `state[key] == None` gracefully — the hydrator's strict behavior is by design (required keys must exist; optional reads must be pre-seeded to be included).

**Bug B — Health KV claim-attempts entries not written (root cause: step 3 auth rejection)**  
The `ClaimIdentity` permission has `scope=self`, and the capability authorizer (step 3) enforces `scope=self` by requiring `authContext.target` to equal `env.Actor`. Without `AuthContext` in the envelope, step 3 returned `AuthContextMismatch` before the script ran — the `handleStubFailure` path with Health KV emission was never reached. Fix: added `AuthContext: &AuthContext{Target: iclConsumerActorKey}` to all ClaimIdentity test envelopes. This satisfies step 3's `scope=self` check (target==actor means "I am submitting on behalf of myself"). The actual one-credential-one-identity enforcement (Decision #10) happens in the Starlark script via the `credentialindex` check — exactly as the brief specifies.

**Bug C — FR5_ImmediateAccess returned "duplicate" for first claim**  
The test created three separate consumers (`consCr`, `consCl1`, `consCl2`) on the same stream. When the create op was published and consumed by `consCr`, `consCl1` and `consCl2` still had the create op pending in their per-consumer queues. Calling `driveOne(cp, consCl1, ...)` caused `cp` to process the backlogged create op via `consCl1` — already committed, step 2 said duplicate. Fix: consolidated the test to use a single pipeline+consumer (`consCr`) for all three operations in sequence (create → claim1 → claim2). This is the correct pattern when operations depend on prior committed state.

**Bug D — Unused helper (lint)**  
`newClaimPipelineOnly` was written to support the multi-consumer test design but became dead code after Bug C's fix. Removed to satisfy `golangci-lint unused` check.

### Architectural Notes

- **scope=self for ClaimIdentity** (Decision #10): Step 3's `scope=self` check requires `authContext.target == actor`. For claim ops, callers set `authContext.target = actorKey` (self-referential). This is semantically correct: the consumer is submitting the op on their own behalf. The one-credential-one-identity constraint is enforced in the Starlark script via the `credentialindex` lookup — NOT by asserting `actor == targetIdentityKey` (which would be wrong; the consumer is binding TO the target identity, not claiming to be it).

- **Length-mismatch timing leak in `crypto.constant_time_equal`**: `subtle.ConstantTimeCompare` is constant-time only when both inputs have equal length. If lengths differ, it returns false immediately without running the comparison. This reveals whether the submitted hash is the same length as the stored hash. In practice, SHA-256 always produces 64-character hex strings, so both inputs are always the same length — the timing leak is theoretical only. Documented per the brief's instruction.

- **`enforce_not_merged` not used in ClaimIdentity** (Decision #11): merged-state check conflates to generic `ClaimKeyInvalid` outcome with detail `"merged"`. Using `enforce_not_merged` would produce an `IdentityMerged` error code, which would leak that the target is in merged state (NFR-S6 anti-enumeration). The ClaimIdentity branch explicitly checks `current_state == "merged"` and calls `fail_claim("merged")` instead.

- **ContextHint optional-reads pattern**: The hydrator is strict — all keys in `ContextHint.Reads` must exist in KV. For optional reads (keys that may or may not exist), callers must either (a) pre-seed the key or (b) omit it from Reads and rely on the Starlark script's `state[key] if key in state else None` guard. This pattern is documented in test comments.

### Deliverables Status

All 12 deliverables verified:

1. `internal/processor/starlark_builtins.go` — `crypto.constant_time_equal` ✓ (written by prior session)
2. `internal/processor/starlark_builtins_test.go` — unit test for new builtin ✓ (written by prior session)
3. `internal/processor/health_alerts.go` — `RecordClaimAttempt` + `ClaimAttemptEmitter` interface ✓ (written by prior session)
4. `internal/processor/commit_path.go` — `ClaimEmitter` in Deps, wired in `MakePipeline` and `newClaimPipeline` ✓ (written by prior session)
5. `internal/processor/commit_path.go` — `classifyStepError` strips detail for `ClaimKeyInvalid` ✓ (written by prior session)
6. `internal/bootstrap/identity_ddl.go` — real `ClaimIdentity` branch ✓ (written by prior session)
7. `internal/processor/identity_claim_test.go` — 8 integration tests, all passing ✓ (fixed by this session)
8. `make verify-bootstrap` 154 OK ✓
9. `make test-bypass` 4/4 BLOCKED ✓
10. `make test-capability-adversarial` 4/4 DEFENDED ✓
11. Token tracker Row 4.3 updated ✓
12. This closing summary ✓
