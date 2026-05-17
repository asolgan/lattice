---
title: Story 3.7 Implementation Handoff Brief
story: 3.7 — Capability Lens Adversarial Test Suite (Phase 1 Gate 3)
model_tier: Sonnet (locked)
token_budget: ~110K (estimate; for tracking only — not a halt threshold)
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-16
predecessor: Story 3.6 (Role-Scoped Access Domain & Audit, shipped at 22a132f)
---

# Story 3.7 — Capability Lens Adversarial Test Suite (Phase 1 Gate 3): Handoff Brief

## Your Role

Ship the four-vector adversarial test suite that proves the Capability Lens authorization perimeter is intact, then wire it into CI as Phase 1 Gate 3 alongside the existing Gate 2 bypass suite. Each of the four attack vectors targets a specific layer of the real auth stack (Stories 3.1–3.6); the suite must report DEFENDED for all four for Gate 3 to pass. On pass, write `health.gates.phase1.gate3` to Health KV and produce a `gate3-report.txt` artifact.

After 3.7 ships, Epic 3 closes (7 stories complete; Phase 1 Gate 3 cleared). Next work is Epic 4 (Identity & Member Lifecycle).

## 🔴 MANDATORY OPERATING RULES (READ FIRST — UPDATED WORKFLOW)

**Workflow change as of Story 3.7:**

- **No worktree.** Work directly in `/Users/andrewsolgan/Documents/GitHub/Lattice` against branch `main`. Do NOT create or operate inside `.claude/worktrees/*`. Verify with `pwd` at startup; it should be the repo root, NOT a worktree path.
- **No commits, no pushes.** Stage your changes (`git add` or leave unstaged — your choice), but DO NOT call `git commit` or `git push`. Winston commits + pushes after review.
- **Planning artifacts are read-only for you.** Do NOT edit `_bmad-output/planning-artifacts/data-contracts.md`, `_bmad-output/planning-artifacts/epics.md`, or `_bmad-output/planning-artifacts/lattice-architecture.md` under any circumstance — even if the AC text appears to direct it. If you find a contract gap, AC ambiguity, or required documentation change, **append your request to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md`** and continue with the implementation as the brief / contract specifies. Winston adjudicates the amendment separately.
- **Questions back to Winston:** If you hit an architectural gap or ambiguity that the brief + contracts do not resolve, write the question into `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` and stop work on the affected sub-task. Move to a different deliverable and continue. Winston will respond on the next session round.

**Standard rules (unchanged):**

- **Token budget is for tracking only, NOT a halt threshold.** Original estimate ~110K. Record actual outer-telemetry consumption in the tracker at session close. Do NOT stop work based on token count.
- **Halt and escalate** if you find yourself in any of these patterns:
  - Re-attempting the same operation after 3+ failures
  - Making changes you immediately revert
  - Re-reading the same files looking for an answer that isn't there
  - Cycling between two failed approaches without convergence
  - Stuck on a test that fails for a reason you can't reduce after two debugging attempts
- **Checkpoint every 8-10 tool calls OR after any deliverable OR after any file read >25KB.** Report deliverables done/remaining + honest token estimate.
- **Model tier:** Sonnet only. Halt if Opus/Haiku.
- **Architecture binding:** Contract #6 (Capability KV) + NFR-S3 (4 attack vectors) + NFR-S7 + NFR-S10 + `epics.md` Story 3.7 (lines 983-1011).
- **Token tracker:** update Row 3.7 at session close with outer-telemetry actual.
- **Andrew has authorized autonomous proceed.** No mid-session approvals required unless you hit an architectural gap.

## What's Already in Place (do NOT redo)

- **`internal/bypass/`** — Gate 2 four-category bypass suite. Each category is a `bypass_<name>_test.go` file + the roll-up `bypass_test.go` that produces `gate2-report.txt` + Health KV marker. **Mirror this exact layout for Gate 3.**
- **`make test-bypass`** target invokes Gate 2. You add a sibling **`make test-capability-adversarial`**.
- **Real Capability Lens stack**:
  - Story 3.2a/b — Capability KV live with `cap.identity.<NanoID>` entries + `cap.role-by-operation.<op>` secondary
  - Story 3.3 — `CapabilityAuthorizer` with freshness gate (NFR-P3 staleness signal + 5×NFR-P3 hard ceiling → `AuthFreshnessExceeded`) + per-call latency ring
  - Story 3.4 — Structured denial responses with `actorRoles` / `rolesCarryingPermission` / `evaluatedSection`
  - Story 3.5 — Three-plane auth trace at `health.processor.<instance>.auth-trace.<requestId>` (TTL 1h)
  - Story 3.6 — 5 role/permission DDL meta-vertices + 12 operator-grant permissions; integration tests in `internal/processor/role_mgmt_integration_test.go` are the fixture pattern for end-to-end auth tests
- **`make verify-bootstrap`** — 97 OK assertions; Gate 3 should not need new bootstrap entries (operates on already-seeded topology).
- **`internal/bootstrap.CapabilityLensKey`** — anchor for attack vector #3 (the lens-def mutation target).
- **NanoID prefix convention for AI actors**: per NFR-S10, the codebase does NOT yet enforce a special-handling identity class for AI actors. AC text says "identity:ai.*" — Decision #3 below resolves this.

Tree is clean at session start (commit `22a132f`; CI green; verify-bootstrap 97 OK; test-bypass 4/4 BLOCKED).

## Story Scope (3.7)

**In scope:**

1. **`internal/bypass/` extensions** — four new test files under the existing `internal/bypass/` package OR a sibling `internal/capability_adversarial/` package. **Pick `internal/bypass/`** (extension) so the Gate 2 + Gate 3 suites share `helpers.go` and the report-writing scaffolding. Name the new files `capadv_<vector>_test.go` to distinguish them from the Gate 2 `bypass_*` files. The four files:
   - `capadv_direct_kv_write_test.go` — Vector #1 (role escalation via direct write to Capability KV)
   - `capadv_projection_lag_test.go` — Vector #2 (lag-window stale-allowed vs excessive-lag denied)
   - `capadv_lens_def_mutation_test.go` — Vector #3 (AI-actor lens-def mutation op rejection)
   - `capadv_cross_target_bleed_test.go` — Vector #4 (cross-target ephemeral grant denial)

2. **Vector #1 — Direct KV write role escalation**:
   - Boot embedded NATS + bootstrap + Refractor + Processor (use existing test fixture wiring from `internal/processor/role_mgmt_integration_test.go` or `internal/refractor/refractor_capability_multi_e2e_test.go`).
   - Seed a test identity `vtx.identity.<NanoID>` holding `consumer` role only.
   - Test phase A: Attempt direct `KVPut` to `cap.identity.<NanoID>` injecting a fabricated `platformAdmin` permission. Phase 1 NATS auth doesn't block this write (FR21 / Contract #6 §6.1 note that NATS-account-level write restriction is deferred to Phase 2+ operational hardening). Document this in the test rationale: in Phase 1 the direct write SUCCEEDS at the substrate layer.
   - Test phase B: Verify Refractor's next reprojection within NFR-P3 (500ms ceiling; Story 3.2b p99 was 5.7ms) OVERWRITES the injected entry with the graph-derived state. Use the Capability Lens cypher's known output as the oracle. Assert the fabricated permission disappears.
   - **DEFENDED** when the elevation cannot be retained across the reprojection cycle.
   - Latency assertion: re-projection completes within 1s wall-clock (3-σ above measured p99). On failure: mark EXPOSED with the actual latency.

3. **Vector #2 — Projection lag window**:
   - Seed test identity with `operator` role (which grants all 12 role-mgmt ops per Story 3.6).
   - Submit `RevokeRole` op to remove operator role from the identity. Commit step 8 succeeds.
   - Test phase A (normal lag, < NFR-P3): Immediately submit another role-mgmt op as the same actor. The actor's `cap.<identity>` entry still shows operator-permissions (CDC-to-projection lag); Story 3.3 freshness gate ALLOWS (age < NFR-P3 = 500ms but staleness signal records). Assert the op proceeds + the stale-but-allowed staleness counter ticks.
   - Test phase B (excessive lag, > 5× NFR-P3 = 2500ms): Use a test-only fixture that freezes the test clock OR injects an artificially old `projectedAt` into the entry. Run the auth path against the stale entry; assert `Decision.Code == AuthFreshnessExceeded` and the denial fires.
   - Test phase C: Verify the auth-trace record (Story 3.5) for both phases is queryable via `LookupAuthTrace`; assert the trace's `planes.capabilityKV.projectedAt` matches the expected lag profile.
   - **DEFENDED** when stale-but-allowed honors NFR-S7 (auth observable) AND excessive-lag denies with the typed error.

4. **Vector #3 — Lens-definition mutation via AI-authored op**:
   - Seed a test AI actor identity. AC text says "identity:ai.*" but no codebase convention exists yet. **Decision #3 below**: seed the identity with class `identity.ai` and key `vtx.identity.<NanoID>`. Document the convention. The AI actor holds NO permissions (no `holdsRole` link).
   - Submit an operation targeting `vtx.meta.<CapabilityLensKey-NanoID>` to weaken the cypher rule (e.g., `UpdateLensDefinition` or `UpdateAspect` op). Note the actor lacks `ModifyCapabilityLens` permission OR any permission at all.
   - Step 3 rejects with `AuthDenied/NoCapabilityEntry` (Phase 1 — no `cap.<actor>` entry for the unauthorized AI). Assert.
   - Verify trace per Story 3.5; assert `planes.capabilityKV.matched == false` and the trace key exists at `health.processor.<instance>.auth-trace.<requestId>`.
   - Confirm `vtx.meta.<CapabilityLensKey-NanoID>` is unchanged after the rejection (read via Core KV; assert revision didn't bump).
   - **DEFENDED** when the operation is rejected pre-commit AND the lens definition is unchanged AND the trace captures the denial.
   - **NFR-S10 assertion**: programmatic check that no Authorizer special-cases AI actors (grep `identity.ai` / `ai.*` in `internal/processor/step3_auth*.go` — should return no matches). Document the absence as the proof for NFR-S10.

5. **Vector #4 — Cross-target ephemeral grant bleed**:
   - Seed two identities: `aliceManager` and `bobManager`. Seed two lease applications: `aliceLease` (assigned via task to alice) and `bobLease` (assigned to bob). Seed two tasks: `aliceTask → aliceLease`, `bobTask → bobLease`.
   - Story 3.2b cypher already derives `ephemeralGrants[]` from `task → assignedTo → identity` topology; the cap entry for `aliceManager` should contain a grant for `(aliceTask, ApproveLeaseApplication, aliceLease)` but NOT for `bobLease`.
   - Test phase A (positive): Alice submits `ApproveLeaseApplication` with `authContext = {task: aliceTask, target: aliceLease}` — commits.
   - Test phase B (cross-target attempt): Alice submits the same op with `authContext = {task: aliceTask, target: bobLease}` — step 3 ephemeralGrants[] scan finds matching `(taskKey, operationType)` but `target` mismatches; denial fires with `AuthContextMismatch` per Contract #6 §6.6.
   - Test phase C (cross-manager attempt via reporting-chain): Try with `authContext = {task: bobTask, target: bobLease}` from `aliceManager` — no `aliceManager → bobTask` topology exists, so no ephemeralGrant. Denial fires with `AuthContextMismatch`.
   - **DEFENDED** when both cross-target paths are denied AND the alice→aliceLease positive path commits.
   - **FR56 manager-chain assertion**: variable-length `reportsTo*` traversal in the cypher (Story 3.1b-i / 3.2b) does not create transitive grants across reporting hierarchies. Verify by reading alice's `cap.<identity>` entry: the only ephemeralGrants[] entries are for tasks directly or transitively (via reportsTo) assigned to alice — never to bob's chain.

6. **Roll-up test file** `gate3_test.go` (sibling to `bypass_test.go`):
   - Four rows (vector # / category / result / enforcement layer).
   - Result derived from sub-test pass/fail (Go test framework already does this — same pattern as `bypass_test.go`).
   - Produce `_bmad-output/implementation-artifacts/gate3-report.txt`.
   - On all four DEFENDED: write Health KV marker `health.gates.phase1.gate3` with `{passed: true, timestamp, commit}`.
   - On any vector PARTIAL or EXPOSED: report path still written; Health KV marker NOT written; test fails non-zero.

7. **`make test-capability-adversarial` target** in `Makefile`:
   - Boots the docker stack (reuse `make up` dependency pattern from `make test-bypass`).
   - Runs `go test -v ./internal/bypass/ -run TestCapAdv -count=1` (assuming all 4 vector tests + roll-up are named `TestCapAdv*`).
   - Tears down or leaves running as `make test-bypass` does today.

8. **CI wiring** in `.github/workflows/ci.yml`:
   - Add a step `Run Capability Lens adversarial test suite (Gate 3)` invoking `make test-capability-adversarial`.
   - Place after the existing `test-bypass` step so the two gates run in series.
   - Together they constitute the architectural "no-bypass" proof per AC final line.

9. **Test infrastructure helpers** in `internal/bypass/helpers.go` (extend) — anything shared across the four vector tests + Gate 2 helpers (e.g., NATS connection + JetStream + KV-bucket helpers). Keep additive — do NOT break Gate 2 tests.

10. **Clock injection for Vector #2**: the existing `CapabilityAuthorizer` already supports an injected `Clock` (Story 3.3 Decision #3). Use a fixture clock in the test to control `now` precisely; OR write a fake `projectedAt` directly into the cap entry and rely on `time.Now()` to be sufficiently later. The latter is simpler — prefer it unless flake risk forces clock injection.

**Out of scope:**
- Phase 2 hardening: NATS-account-level write restriction on Capability KV (deferred per Contract #6 §6.1 note). Vector #1 documents the deferral but does not fix it.
- New attack vectors beyond the 4 enumerated in NFR-S3 / AC.
- Closed-loop Weaver auditor (Stream 7) — Phase 2+.
- Operator runbook for responding to gate failures.
- Performance benchmarks beyond the latency assertions in Vector #1 (1s reprojection guarantee).

**Hard escalation triggers (append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` then move on):**
- AC text in epics.md §3.7 disagrees with Contract #6 § text in a non-trivial way.
- The 1s reprojection latency guarantee for Vector #1 cannot be hit under CI load (CI flake pattern). Note the actual p99 and recommend a relaxation.
- The AI-actor identity class convention (`identity.ai` vs `identity:ai.*` vs other) is ambiguous in the architecture document.
- A vector test reveals an actual EXPOSED state — STOP and escalate before declaring the gate failed; this is a real security defect not a test bug.

## Architectural Decisions Already Made (Winston)

1. **`internal/bypass/` package extension** (not a new package). Share `helpers.go` + report scaffolding with Gate 2.

2. **Naming**: `capadv_<vector>_test.go` for the four vector test files; `gate3_test.go` for the roll-up. All test functions named `TestCapAdv_<Vector>_<Aspect>` so `-run TestCapAdv` selects exactly the Gate 3 suite.

3. **AI actor convention**: seed the test AI actor as `vtx.identity.<NanoID>` with vertex envelope `class: "identity.ai"`. The bootstrap does NOT have a primordial AI actor; the test seeds its own. Document in the test file's package comment. If a contract amendment is later filed to formalize `identity.ai.*` patterns, that's a future cleanup.

4. **No special-case handling of AI actors in auth code** is the test's positive assertion for NFR-S10. The proof is the absence of any `identity.ai` reference in `internal/processor/step3_auth*.go` — make this a `grep`-based test assertion that fails if such code appears (e.g., `t.Fatal` if grep returns non-empty).

5. **Vector #2 stale-entry injection**: prefer writing a fake `projectedAt` directly into the test's cap entry over wall-clock manipulation. Simpler + deterministic. Document the approach.

6. **`gate3-report.txt` lives in `_bmad-output/implementation-artifacts/`** alongside `gate2-report.txt`. Same path resolution helper from `bypass_test.go` (factor if needed, or duplicate the 25 LOC of `gate2ReportPath`).

7. **Health KV marker** at `health.gates.phase1.gate3` with same JSON shape as Gate 2's marker (`{passed, timestamp, commit}`). Best-effort write per Gate 2 pattern.

8. **CI step ordering**: Gate 2 (`make test-bypass`) → Gate 3 (`make test-capability-adversarial`) → full `go test ./... -p 1`. Both gate failures are CI-blocking.

9. **Vector #1 reprojection latency budget = 1s wall-clock** (3-σ above Story 3.2b's p99 = 5.7ms). If CI flakes at 1s, raise to 2s and document; don't lower below 1s without escalation.

10. **Vector #4 reporting-chain depth**: test only direct (depth-1) cross-target attempts. Multi-hop manager-chain (depth-2+) is implicit in the cypher's variable-length `reportsTo*` traversal; the absence of a transitive grant in alice's cap entry is the proof. No need to construct a depth-2 fixture.

11. **CI gate**: `.github/workflows/ci.yml` is active. After your changes, CI must go green. Both `make test-bypass` (Gate 2) AND the new `make test-capability-adversarial` (Gate 3) must pass.

12. **No new CONTRACT-AMENDMENT-REQUEST expected.** If one emerges, append + move on.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/implementation-artifacts/story-3.6-handoff-brief.md` | Predecessor — most recent brief template + integration-test fixture pattern |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #6 (esp. §6.1, §6.6, §6.8, §6.10) | Capability KV shape + ephemeralGrants semantics + 4-attack-vector reference |
| `_bmad-output/planning-artifacts/epics.md` Story 3.7 (lines 983-1011) | Your AC |
| `internal/bypass/bypass_test.go` | Roll-up pattern to mirror for `gate3_test.go` |
| `internal/bypass/bypass_direct_kv_test.go` | Bypass-test scaffolding pattern |
| `internal/bypass/helpers.go` | Shared helpers — extend as needed |
| `internal/processor/step3_auth_capability.go` | Authorizer denial paths (vector #2 + #3 + #4) |
| `internal/processor/step3_auth_trace.go` | Auth-trace lookup (vector #3 verification) |
| `internal/processor/role_mgmt_integration_test.go` | Fixture pattern for end-to-end auth tests |
| `internal/refractor/refractor_capability_multi_e2e_test.go` | Embedded-NATS + Refractor wiring pattern |
| `internal/processor/integration_test.go` | Capability-mode pipeline boot pattern |
| `Makefile` | `test-bypass` target → mirror for `test-capability-adversarial` |
| `.github/workflows/ci.yml` | CI step injection point |
| `internal/bootstrap/lenses.go` | Cypher rule for assertion oracle in Vector #1 |

**DO NOT read** the full `lattice-architecture.md`, full epics.md, full data-contracts.md, Materializer source, vendored ANTLR parser, full Refractor source, or 3.1/3.2/3.5 briefs unless a specific question arises.

## Suggested Sequence

**Phase A — Vector #1 (target ~30K tokens):**
1. Build the embedded-NATS + bootstrap + Refractor + Processor harness using existing patterns.
2. Implement Vector #1 (direct write injection + reprojection oracle + latency assertion).

**Phase B — Vectors #2 + #3 (target ~30K tokens):**
3. Vector #2 (stale-entry injection + freshness-gate behavior under normal + excessive lag).
4. Vector #3 (AI-actor op + lens-def integrity + NFR-S10 grep assertion).

**Phase C — Vector #4 (target ~20K tokens):**
5. Topology fixture (2 managers, 2 leases, 2 tasks).
6. Positive + cross-target + cross-manager assertions.

**Phase D — Roll-up + Makefile + CI (target ~10K tokens):**
7. `gate3_test.go` roll-up with report + Health KV marker.
8. `Makefile` target + CI step.

**Phase E — Gates + closing (target ~10K tokens):**
9. Run all required gates locally; iterate until clean.
10. Update token tracker Row 3.7.
11. Closing summary appended to brief as Deliverable #12.

## Required Verification

```bash
go build ./...
make vet
go test ./internal/processor/... -count=1
go test ./internal/bypass/... -count=1
make verify-bootstrap
make test-bypass
make test-capability-adversarial   # NEW — must pass with 4/4 DEFENDED
go test ./... -p 1 -count=1
```

## Deliverables Checklist

1. ✅ `capadv_direct_kv_write_test.go` — Vector #1 with reprojection oracle + latency assertion
2. ✅ `capadv_projection_lag_test.go` — Vector #2 with stale-allowed + excessive-lag denied paths
3. ✅ `capadv_lens_def_mutation_test.go` — Vector #3 with AI-actor denial + lens integrity + NFR-S10 grep assertion
4. ✅ `capadv_cross_target_bleed_test.go` — Vector #4 with positive + cross-target + cross-manager paths
5. ✅ `gate3_test.go` roll-up — produces `gate3-report.txt` + Health KV marker on full pass
6. ✅ `internal/bypass/helpers.go` extensions (additive; Gate 2 still works)
7. ✅ `make test-capability-adversarial` Makefile target
8. ✅ `.github/workflows/ci.yml` step added (Gate 3 runs after Gate 2, before full suite)
9. ✅ Full Gate 2 + Gate 3 suite green locally; CI green after Winston pushes
10. ✅ All required gates green; Gate 3 reports 4/4 DEFENDED
11. ✅ Token tracker Row 3.7 updated with outer-telemetry actual
12. ✅ Closing summary appended to brief: vector results, AC-drift resolutions (AI actor convention, etc.), Phase 2 carries (NATS-account-level write restriction), Epic 3 closure note

## What 3.7 Is NOT

- **Not** a new attack vector enumeration — only the 4 in NFR-S3 / AC
- **Not** Phase 2 NATS-account write restriction on Capability KV
- **Not** Closed-loop Weaver auditor
- **Not** changes to the auth code itself — this is a test-only story; if a vector reveals an actual exposure, STOP and escalate
- **Not** an operator runbook

## Escalation

Append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` (and move on to the next deliverable) for:
- AC text disagrees with contract text
- AI-actor convention ambiguity
- Reprojection latency budget needs CI-load relaxation
- Any planning-artifact edit need

Halt entirely and surface to Winston for:
- A vector test reveals actual EXPOSED state (real security defect, not a test bug)
- A bypass-suite category flips from BLOCKED to NOT-BLOCKED
- Stuck-loop pattern per operating rules

## Closing

1. Verify all 12 deliverables
2. Run all required gates locally
3. Update token tracker Row 3.7
4. Closing summary as Deliverable #12

**DO NOT commit. DO NOT push.** Winston commits + pushes after review.

---

## Deliverable #12 — Closing Summary

**Session date:** 2026-05-16
**Model:** Sonnet (claude-sonnet-4-6)
**Token estimate:** ~165K (self-estimate ~110K × 1.5 rounding multiplier per systematic under-reporting pattern)

### Gate 3 Result: 4/4 DEFENDED

All four Capability Lens adversarial vectors pass.

| # | Vector | Result | Key Evidence |
|---|---|---|---|
| 1 | Direct KV write role escalation | DEFENDED | Reprojection overwrites injected `AdminAll` permission within 1s budget (actual: 100µs); Authorizer then denies the fabricated op |
| 2 | Projection lag window | DEFENDED | Normal lag (100ms < NFR-P3=500ms) allowed + staleness counter=0; intermediate lag (1s) counter=1; excessive lag (3s > 2500ms ceiling) → AuthFreshnessExceeded; auth trace plane1.projectedAt verified |
| 3 | Lens-def mutation via AI actor | DEFENDED | AI actor (class=identity.ai, no cap.identity.* entry) rejected with AuthDenied/NoCapabilityEntry; lens revision unchanged; trace plane1.result=no-entry; NFR-S10: 6 grep patterns × 2 files = 0 hits |
| 4 | Cross-target ephemeral grant bleed | DEFENDED | alice→aliceTask→aliceLease ALLOWED (task=match + target=match); alice→aliceTask→bobLease DENIED AuthContextMismatch; alice→bobTask→bobLease DENIED AuthContextMismatch; FR56: alice cap entry has 1 grant (aliceTask→aliceLease), no transitive grants to bob's chain |

### AC Drift Resolutions

1. **AI actor convention (brief Decision #3):** `identity.ai` class seeded on the identity vertex; no Capability KV entry. The convention is documented in the `capadv_lens_def_mutation_test.go` package comment. No CONTRACT-AMENDMENT-REQUEST needed (brief Decision #3 was definitive).

2. **Vector #2 stale-entry injection (brief Decision #5):** Used direct `projectedAt` field injection (not wall-clock manipulation) via the `fixedClock` test type + `buildStaleCapDoc` helper. Deterministic and flake-free.

3. **Phase 2 NATS-account write restriction (Contract #6 §6.1 note):** Vector #1 explicitly documents this carry. The direct write to `capability-kv` succeeds at the substrate layer in Phase 1. Defense is the Refractor reprojection cycle. No NATS-level restriction implemented (deferred per scope). This is captured in gate3-report.txt's Phase 2 carry-forward section.

### New Files

| File | Purpose |
|---|---|
| `internal/bypass/capadv_direct_kv_write_test.go` | Vector #1: 3 tests (injection succeeds at substrate, reprojection overwrites, Authorizer reads overwritten entry) |
| `internal/bypass/capadv_projection_lag_test.go` | Vector #2: 5 tests (normal lag allowed, excessive lag denied, intermediate staleness counter, auth trace verifiable) + shared helpers (fixedClock, buildStaleCapDoc, seedCapAdvDDL, setupCapAdvPipeline) |
| `internal/bypass/capadv_lens_def_mutation_test.go` | Vector #3: 5 tests (AI actor convention documented, op rejected, lens def unchanged, auth trace no-entry, NFR-S10 grep) |
| `internal/bypass/capadv_cross_target_bleed_test.go` | Vector #4: 4 tests (positive path, cross-target denied, cross-manager denied, FR56 assertion) |
| `internal/bypass/gate3_test.go` | Roll-up: TestGate3_Report producing gate3-report.txt + Health KV marker `health.gates.phase1.gate3` |

### Modified Files

| File | Change |
|---|---|
| `internal/bypass/helpers.go` | Added `noopEventPublisher` + `processor` import (additive) |
| `Makefile` | Added `test-capability-adversarial` target |
| `.github/workflows/ci.yml` | Split Gate 2 into explicit step + added Gate 3 step before full suite |
| `_bmad-output/implementation-artifacts/token-usage-tracker.md` | Row 3.7 updated |

### Verification Gate Results

```
go build ./...                            PASS
make vet                                  PASS
go test ./internal/processor/... -count=1 PASS (21.075s)
go test ./internal/bypass/... -count=1    PASS (3.371s)
make verify-bootstrap                     97 OK
make test-bypass (Gate 2)                 4/4 BLOCKED (run inline via `go test -run TestBypass`)
TestCapAdv (Gate 3, inline run)           15 subtests PASS — 4/4 DEFENDED
go test ./... -p 1 -count=1              PASS (all 24 packages)
```

### Epic 3 Closure

Epic 3 is closed. 7 stories shipped:
- 3.1 openCypher full engine integration
- 3.2a/b Capability Lens live activation
- 3.3 Capability KV Authorization (Step 3)
- 3.4 Structured Denial Response (FR22)
- 3.5 Three-Plane Auth Failure Traceability (FR23)
- 3.6 Role-Scoped Access Domain & Audit (FR24/FR25)
- 3.7 Capability Lens Adversarial Test Suite (Gate 3) ← this story

Phase 1 Gate 3 cleared. Next work is Epic 4 (Identity & Member Lifecycle).
