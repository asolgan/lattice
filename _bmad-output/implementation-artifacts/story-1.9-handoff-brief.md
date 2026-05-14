---
title: Story 1.9 Implementation Handoff Brief
story: 1.9 — Write-Scope Enforcement per DDL (FR57)
model_tier: Sonnet (locked)
token_budget: ~85K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-14
---

# Story 1.9 — FR57 Write-Scope Enforcement: Implementation Handoff Brief

## Your Role

You harden and dedicate-test the Processor's step 6 `permittedCommands` enforcement and the sensitive-aspect write-scope rule. Story 1.7 already implemented the basics in `step6_validate.go`; Story 1.9's job is to (a) audit that implementation against the AC, (b) close any gaps (especially around the **permissive-default** behavior for undeclared `permittedCommands`), and (c) ship a dedicated `write_scope_test.go` integration test file that becomes the FR57 verification artifact.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

**Pattern across Stories 1.1, 1.5, 1.6, 1.7, 1.8: sub-agents self-report tokens 30-50% under outer telemetry.** Treat your self-estimate as a LOWER BOUND, round UP.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables completed, deliverables remaining, honest token estimate.
- **Halt unconditionally if you estimate > 90K used** (5% over budget).

Other rules:
- **Model tier:** Sonnet only. Halt if Opus/Haiku.
- **No PRs.** Direct commit after Winston review.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is source of truth.
- **DO NOT silently edit planning artifacts.** Use `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md`.
- **All KV/JetStream ops through `internal/substrate`.**
- **No git commits by you.**
- **Token tracker:** update Row 1.9 at session close — HONEST estimate, round UP.

## Story Scope (from `epics.md` lines 517-541 — authoritative)

> As a platform engineer, I want the Processor's step 6 DDL validation to enforce `permittedCommands` write-scope constraints declared in DDL meta-vertices, so that no operation can mutate a data type using an operation type its DDL has not explicitly permitted.

**Recommended model tier:** Sonnet
**Estimated token budget:** ~85K

### Acceptance Criteria (verbatim)

**Given** a DDL meta-vertex for `identity` declares `permittedCommands: ["create", "update"]`
**When** an operation with `operationType: "tombstone"` targets an identity vertex
**Then** the Processor's step 6 validator rejects the MutationBatch with a `DDLViolation` error; the error message names the violated constraint (`permittedCommands`), the attempted operation type (`tombstone`), and the DDL meta-vertex key; the operation does not reach the atomic batch step.

**Given** the same DDL declares `permittedCommands: ["create", "update"]`
**When** an operation with `operationType: "create"` targets an identity vertex
**Then** the Processor's step 6 validator accepts the mutation and allows it to proceed to the atomic batch step.

**Given** a DDL meta-vertex does not declare a `permittedCommands` field (permissive-by-default per Contract #1)
**When** any operation type targets a vertex of that type
**Then** the step 6 validator accepts the mutation without write-scope enforcement (permissive default — undeclared = unrestricted).

**And** a dedicated `write_scope_test.go` integration test file covers: permitted operation (accepted), forbidden operation (DDLViolation), and missing declaration (permissive default accepted).
**And** the sensitive aspect write-scope constraint (sensitive aspects may only attach to identity vertices) is covered in the same test file with a test asserting that a sensitive aspect write to a non-identity vertex returns `DDLViolation`.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.7 (DDL/class lookup), §1.5 (key patterns), and the `permittedCommands` reference (grep for it) | Source of truth for permissive-default + `permittedCommands` semantics |
| `internal/processor/step6_validate.go` (Story 1.7's implementation — WHOLE FILE) | Your starting point. Audit it against the AC. |
| `internal/processor/step6_validate_test.go` (Story 1.7) | Existing test coverage — DO NOT duplicate; reference and extend |
| `internal/processor/ddl_cache.go` (Story 1.7) | How DDLs are loaded — relevant if you need to test permissive-default (DDL without `permittedCommands` aspect) |
| `internal/processor/envelope.go` | `DDLViolation` error type and its fields (`ViolatedConstraint`, `MutationKey`, `OperationRequestID`) — error message must include the AC-required fields |
| `internal/bootstrap/lenses.go` + `internal/bootstrap/roles.go` | Concrete DDL meta-vertex examples — useful for test fixtures |
| `internal/processor/step8_e2e_test.go` (Story 1.7) | E2E test pattern to mirror in `write_scope_test.go` |

**DO NOT read** the full `epics.md` (you have the Story 1.9 AC above) or the full `lattice-architecture.md`. The brief is self-contained.

## Architectural Decisions Already Made (Winston)

1. **Audit first, then close gaps.** Read Story 1.7's `step6_validate.go` against the AC. Story 1.7 implemented `permittedCommands` enforcement and sensitive-aspect-scope enforcement, but Story 1.9's third AC bullet (permissive default when `permittedCommands` is missing) needs explicit verification. If Story 1.7's implementation already handles this correctly, the Story 1.9 work is dominated by tests + error-message audit; if not, fix it minimally without re-architecting.

2. **`DDLViolation` error message format (REQUIRED):** the message string MUST contain the three named items from the AC: violated constraint (the literal string `permittedCommands`), the attempted operation type (e.g., `tombstone`), and the DDL meta-vertex key (the real NanoID-keyed `vtx.meta.<NanoID>`). Story 1.7 already returns a `DDLViolation` typed error — extend its `Error()` method or its formatting if any of these are missing.

3. **Permissive default:** a DDL meta-vertex that has NO `permittedCommands` aspect (or an empty array) means **all operation types are permitted** for that vertex type. The validator must NOT reject in this case. Test this with a fixture DDL that has no `permittedCommands` aspect.

4. **Sensitive aspect rule (NFR-S3):** sensitive aspects (declared via `sensitive: true` on the DDL aspect schema) may attach ONLY to `identity` vertices. Story 1.7 implemented this; Story 1.9's job is to add the dedicated test in `write_scope_test.go`. Detection: aspect's key segment 2 ≠ `identity` → `DDLViolation` with constraint name `sensitiveAspectScope`.

5. **Test file location:** `internal/processor/write_scope_test.go` (NEW). Use the existing test patterns from `step8_e2e_test.go` and `step6_validate_test.go`. The file should be runnable both as a unit test (mock substrate where helpful) AND as a Docker-backed e2e where the AC requires it. Default to unit-level for speed; add ONE end-to-end test that drives the full commit_path with a fault.

6. **FR57 verification marker:** at the top of `write_scope_test.go`, add a comment `// FR57: Write-Scope Enforcement per DDL — verification artifact for Story 1.9`. The test file's roll-up assertion prints `FR57: VERIFIED` to stdout on full pass (mirror the NFR-R1 pattern from Story 1.8 if 1.8 has landed by the time you run; if not, this is the first such pattern in the repo — copy the format Winston used in the 1.8 brief).

7. **No new substrate primitives.** Everything you need exists.

8. **No Contract amendments expected.** If you find one, append to `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md` and escalate before resolving.

9. **Story 1.7's `permittedCommands` enforcement targets the mutation's operation type — clarify which.** Per Contract #1 §1.7, `permittedCommands` is enforced against `OperationEnvelope.OperationType` (the operation-level command), NOT against the mutation's `op` field (`create`/`update`/`tombstone`). Audit Story 1.7's implementation to confirm — if it's checking the wrong field, fix it. This is the single most likely Story 1.7 gap.

## Suggested Layout

```
internal/processor/
├── write_scope_test.go      NEW: FR57 verification suite
├── step6_validate.go        EDIT (minimal): close any gaps the audit reveals
├── envelope.go              EDIT (minimal): DDLViolation.Error() message format if needed
```

## Deliverables Checklist

1. ✅ Audit memo (in your closing summary): what Story 1.7's step6_validate.go already covers, what Story 1.9 had to fix
2. ✅ `step6_validate.go` minimal fixes (if any) — touch as little as possible
3. ✅ `envelope.go` — `DDLViolation.Error()` includes constraint name, attempted op type, DDL meta-vertex key (if not already)
4. ✅ `write_scope_test.go` covering:
   - Permitted operation: DDL with `permittedCommands: ["create","update"]`, op type `create` → ACCEPTED
   - Forbidden operation: same DDL, op type `tombstone` → `DDLViolation` with all three required fields
   - Missing declaration: DDL with no `permittedCommands` aspect → ACCEPTED (permissive default)
   - Empty declaration: DDL with `permittedCommands: []` → ACCEPTED (permissive default, treat empty same as missing)
   - Sensitive aspect on identity vertex → ACCEPTED
   - Sensitive aspect on non-identity vertex → `DDLViolation` with constraint name `sensitiveAspectScope`
   - One end-to-end test driving the full commit_path with a forbidden op → reply contains `decision: "rejected"`, no mutation in Core KV, no tracker write (or tracker with `committed: false` per Story 1.7's reply shape)
5. ✅ FR57 verification line printed on full test pass
6. ✅ `make verify-bootstrap` green (regression)
7. ✅ `go build ./...`, `go vet ./...`, `go test ./internal/processor/... -count=1` exit 0
8. ✅ Token tracker Row 1.9 updated — HONEST estimate, round UP
9. ✅ Closing summary

## What Story 1.9 Is NOT

- **Not** event-DDL validation (deferred to Story 1.10 or later).
- **Not** the bypass test suite (Story 1.10).
- **Not** a re-architecting of step 6. Minimal touch.

## Escalation

Halt and escalate via Andrew if:
- Story 1.7's step6_validate.go has a structural gap that requires more than a minimal fix
- Token estimate exceeds 90K
- Any Contract amendment is required
- The permissive-default semantics conflict with what `ddl_cache.go` actually loads (e.g., it's storing a default value rather than absence)

## Closing

1. Verify all 9 deliverables
2. `make down && make up && make verify-bootstrap` green
3. Run `go test ./internal/processor/... -run WriteScope -v` and confirm `FR57: VERIFIED` printed
4. Update token tracker Row 1.9 — round UP
5. Closing summary: audit memo, deliverables present, e2e observations, FR57 status, token estimate (honest), open questions

Do NOT commit. Winston + Andrew review and commit.
