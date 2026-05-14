---
title: Story 1.10 Implementation Handoff Brief
story: 1.10 — Attempted Bypass Test Suite (Phase 1 Gate 2)
model_tier: Sonnet (locked)
token_budget: ~105K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-14
---

# Story 1.10 — Attempted Bypass Test Suite (Phase 1 Gate 2): Implementation Handoff Brief

## Your Role

You build the adversarial test suite that closes **Phase 1 Gate 2**. The suite must prove all four architectural bypass categories are impossible against the 10-step Processor commit path delivered by Stories 1.3-1.9. You DO NOT add new commit-path logic; the four bypasses must already be blocked by code that already exists (steps 1-10, the sandbox, the DDL cache, the FR57 enforcement). If a bypass is NOT blocked, escalate — do not patch silently.

The closer artifact is a human-readable summary report and a Health KV marker `health.gates.phase1.gate2 = passed: true`.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

**Pattern across Stories 1.1, 1.5, 1.6, 1.7, 1.8: sub-agents self-report tokens 30-50% under outer telemetry.** Treat your self-estimate as a LOWER BOUND, round UP.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables completed, deliverables remaining, honest token estimate.
- **Halt unconditionally if you estimate > 110K used** (5% over budget).

Other rules:
- **Model tier:** Sonnet only. Halt if Opus/Haiku.
- **No PRs.** Direct commit after Winston review.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is source of truth.
- **DO NOT silently edit planning artifacts.** Use `cmd/processor/CONTRACT-AMENDMENT-REQUEST.md`.
- **All KV/JetStream ops through `internal/substrate`.**
- **No git commits by you.**
- **Token tracker:** update Row 1.10 at session close — HONEST estimate, round UP.
- **CRITICAL:** if any bypass test does NOT report BLOCKED, HALT and escalate. Do NOT modify production code to make a bypass pass. This story's job is to discover gaps, not paper over them.

## Story Scope (from `epics.md` lines 544-569 — authoritative)

> As a platform engineer, I want a dedicated adversarial test suite that proves all four architectural bypass categories are impossible, so that the security perimeter of the write path is validated before Epic 3 builds authorization on top of it.

**Recommended model tier:** Sonnet
**Estimated token budget:** ~105K

### Acceptance Criteria (verbatim)

**Given** the complete 10-step Processor commit path from Stories 1.3–1.9 is running
**When** the adversarial test suite executes
**Then** all four bypass categories are tested and proven impossible:

1. **Direct KV write bypass**: A test client attempts to write directly to Core KV (bypassing `core-operations` stream and Processor); the write is rejected at the NATS authorization level OR the write succeeds but produces no EventList entry, making it undetectable by downstream Refractor consumers — document which enforcement layer catches this and mark it explicitly.

2. **Stream publish outside `ops.*` namespace**: A test client publishes to a subject outside the `ops.*` namespace (e.g., `core-operations` directly without going through the Processor consumer); the Processor's durable consumer only receives messages published via the correct subject hierarchy; messages on unauthorized subjects are not consumed.

3. **Starlark I/O escape**: A malicious Starlark script attempts each of the four forbidden operations (external HTTP, filesystem read, `os.Getenv`, non-deterministic call); each attempt is caught by the sandbox; the test asserts that the `SandboxViolation` error is returned and no mutation is written to Core KV.

4. **DDL schema violation**: An operation is crafted that would write a vertex or aspect that violates the DDL schema (wrong operation type for `permittedCommands`, sensitive aspect on non-identity vertex); the Processor's step 6 validator catches and rejects the mutation; no partial state reaches Core KV.

**And** the test suite produces a human-readable summary report: one row per bypass category, result (BLOCKED / PARTIAL / ESCAPED), and the enforcement layer that caught it.
**And** all four categories must report BLOCKED for Phase 1 Gate 2 to be marked passed.
**And** the test suite is runnable standalone via `make test-bypass` and exits 0 only when all four categories are BLOCKED.
**And** Gate 2 status is written to Health KV under `health.gates.phase1.gate2` as `passed: true` with timestamp upon successful test run.

## Required Context — Read These Only

| File | Why |
|---|---|
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #5 (Health KV — full) | Gate 2 marker format; how to write `health.gates.phase1.gate2` |
| `internal/processor/step1_consume.go` | Consumer's `FilterSubjects` — Bypass #2 test verifies this |
| `internal/processor/starlark_runner.go` + `starlark_builtins.go` (Story 1.6) | Sandbox enforcement — Bypass #3 test exercises the four forbidden operations |
| `internal/processor/step6_validate.go` + `write_scope_test.go` (Story 1.9) | Bypass #4 — DDL schema violation. Story 1.9 already covers happy/sad. Story 1.10 wraps it in the bypass report format. |
| `internal/processor/health.go` (Story 1.5+) | How Health KV is written; pattern for the Gate 2 marker |
| `internal/processor/step10_e2e_test.go` (Story 1.8) | Full 10-step e2e pattern to mirror |
| `internal/bootstrap/primordial.go` | Core KV bucket name, stream configuration (relevant for Bypass #1 — direct KV write) |
| `Makefile` | Add `test-bypass` target alongside existing test targets |
| `internal/processor/step3_auth.go` (Story 1.5) | Auth stub — confirms whether a NATS-level auth rejection is in scope for Bypass #1 today (it likely is NOT in Phase 1 — see Decision #2) |

**DO NOT read** the full `epics.md`, `lattice-architecture.md`, or full Contracts unless a specific question pushes you there.

## Architectural Decisions Already Made (Winston)

1. **Test suite location:** `internal/bypass/` (NEW package) with `bypass_test.go` (and per-category files if it helps readability: `bypass_direct_kv_test.go`, `bypass_stream_publish_test.go`, `bypass_starlark_io_test.go`, `bypass_ddl_schema_test.go`). Keep `internal/bypass` minimal — it's a test-only package, no production exports.

2. **Bypass #1 (Direct KV write) — Phase 1 enforcement layer:** In Phase 1, NATS-level authorization is NOT yet in place (auth is stubbed via `step3_auth.go`'s `StubAuthorizer`). The Bypass #1 test must therefore demonstrate the **alternative enforcement**: a direct write to Core KV succeeds at the NATS layer, but produces NO EventList entry on `core-events`, NO tracker entry on Core KV at `vtx.op.*`, and NO log line from the Processor. The Refractor's invariant ("every Core KV change has a corresponding `core-events` entry") is violated, which makes the direct write **detectable by Refractor at projection time** but **not committable through the legitimate API path**. The test report row reads: `Direct KV write: BLOCKED (enforcement: undetectable-without-EventList, Refractor will diverge — flagged as Phase 1 acceptable; promotion to NATS-auth rejection deferred to Epic 3)`. **If Andrew prefers a stricter mark, escalate.**

3. **Bypass #2 (Off-namespace publish):** Publish a message to a NATS subject OUTSIDE `ops.>` (e.g., `bypass.attempt`). Assert that the Processor's durable consumer (per `step1_consume.go`'s `FilterSubjects`) does NOT receive it. Verify via: (a) the message lands SOMEWHERE (or is rejected — check both) and (b) the Processor logs show no consumption. Report row: `Off-namespace publish: BLOCKED (enforcement: JetStream consumer FilterSubjects)`.

4. **Bypass #3 (Starlark I/O escape):** Run four sub-tests, one per forbidden operation:
   - External HTTP: `http.get("http://example.com")` (or similar — confirm the actual Starlark API surface from Story 1.6's `starlark_builtins.go`; Starlark by default has no `http` module — the test crafts a script that attempts `load("http", ...)` or any pattern Story 1.6 explicitly rejects)
   - Filesystem read: `open("/etc/passwd")` — Story 1.6's brief said this is rejected
   - `os.Getenv("HOME")` — Story 1.6's brief said this is rejected
   - Non-deterministic call: `time.now()` or any explicitly-blocked time/random call

   Each: feed the script to the Processor's hot path; assert `SandboxViolation` error returned; assert NO mutation in Core KV; assert NO event in `core-events`. Report row: `Starlark I/O escape: BLOCKED (enforcement: Starlark sandbox via Story 1.6's starlark_runner.go)`.

5. **Bypass #4 (DDL schema violation):** Story 1.9's `write_scope_test.go` already covers the happy/sad paths. Story 1.10's job is to wrap two scenarios (forbidden op type per `permittedCommands`; sensitive aspect on non-identity) into the bypass report format. You may either (a) call directly into Story 1.9's test helpers if exported, OR (b) re-run the scenarios inline. Either is fine. Report row: `DDL schema violation: BLOCKED (enforcement: Processor step 6 validator from Story 1.7/1.9)`.

6. **Report format:** plain-text human-readable, printed to stdout AND written to a file at `_bmad-output/implementation-artifacts/gate2-report.txt`. Format:
   ```
   Phase 1 Gate 2 — Attempted Bypass Test Suite
   Run at: <RFC3339 timestamp>
   Commit: <git short SHA — read via `git rev-parse --short HEAD` at test start>

   | # | Category                  | Result   | Enforcement Layer                                  |
   |---|---------------------------|----------|----------------------------------------------------|
   | 1 | Direct KV write           | BLOCKED  | undetectable-without-EventList (Phase 1 acceptable)|
   | 2 | Off-namespace publish     | BLOCKED  | JetStream consumer FilterSubjects                  |
   | 3 | Starlark I/O escape       | BLOCKED  | Starlark sandbox (starlark_runner.go)              |
   | 4 | DDL schema violation      | BLOCKED  | Processor step 6 validator                         |

   PHASE 1 GATE 2: PASSED (4/4 BLOCKED)
   ```

7. **Health KV marker:** on full pass, write `health.gates.phase1.gate2` to the `health` KV bucket with value:
   ```json
   {"passed": true, "timestamp": "<RFC3339>", "commit": "<short SHA>"}
   ```
   Use `substrate.Conn.KVPut` (or whatever Story 1.5's `health.go` exposes). On any FAIL, do NOT write the marker — and exit non-zero.

8. **Makefile target:** add a `test-bypass` target that runs `go test ./internal/bypass/... -v -count=1` against a freshly-restarted Docker stack:
   ```
   .PHONY: test-bypass
   test-bypass:
   	@$(MAKE) down
   	@$(MAKE) up
   	@$(MAKE) verify-bootstrap
   	go test ./internal/bypass/... -v -count=1
   ```

9. **Exit-code contract:** the test suite exits 0 ONLY when all four are BLOCKED. Any other result exits non-zero (Go's `testing` framework already does this; just ensure your roll-up assertion fails the test rather than silently logging a PARTIAL/ESCAPED).

10. **No new Contract amendments expected.** If a bypass IS exploitable, that is a Winston-escalation event, NOT an amendment. Halt and document in the closing summary.

## Suggested Layout

```
internal/bypass/
├── bypass_test.go                NEW: roll-up + report writer + Gate 2 marker
├── bypass_direct_kv_test.go      NEW: Bypass #1
├── bypass_stream_publish_test.go NEW: Bypass #2
├── bypass_starlark_io_test.go    NEW: Bypass #3
├── bypass_ddl_schema_test.go     NEW: Bypass #4
├── helpers.go                    NEW: shared fixtures (e.g., live processor harness)

_bmad-output/implementation-artifacts/
├── gate2-report.txt              GENERATED on test run (gitignored OR committed — see Decision #11)

Makefile                          EDIT: add `test-bypass` target
```

### Decision #11 — gate2-report.txt commit policy

Commit `gate2-report.txt` to git. It is the artifact of Phase 1 Gate 2 closure and provides an audit trail. Stories 2.x and 3.x will overwrite it (or write new gateN-report.txt files). Add a brief header to the report noting "regenerated by `make test-bypass`".

## Deliverables Checklist

1. ✅ `internal/bypass/` package with the 5 test files described above
2. ✅ Bypass #1 test (direct KV write) with documented Phase-1 enforcement model
3. ✅ Bypass #2 test (off-namespace publish) BLOCKED via consumer FilterSubjects
4. ✅ Bypass #3 test (Starlark I/O escape — 4 sub-tests, one per forbidden op)
5. ✅ Bypass #4 test (DDL schema violation — permittedCommands + sensitive aspect)
6. ✅ Roll-up test that produces the human-readable report (stdout + gate2-report.txt)
7. ✅ Gate 2 Health KV marker written on full pass; NOT written on any failure
8. ✅ `Makefile` `test-bypass` target
9. ✅ `gate2-report.txt` committed
10. ✅ `make verify-bootstrap` green (regression)
11. ✅ `make test-bypass` exits 0 with all four rows BLOCKED
12. ✅ `go build ./...`, `go vet ./...`, `go test ./... -count=1` exit 0
13. ✅ Token tracker Row 1.10 updated — HONEST estimate, round UP
14. ✅ Closing summary including the full report contents pasted

## What Story 1.10 Is NOT

- **Not** a place to add NEW production enforcement. If a bypass is exploitable, escalate.
- **Not** Capability Lens / Auth (Epic 3).
- **Not** event-DDL validation (deferred).

## Escalation (READ TWICE)

Halt and escalate via Andrew if:
- Any bypass test does NOT report BLOCKED. Do NOT silently patch production code to fix it. Andrew + Winston must decide together whether to fix-in-1.10 or open a separate story.
- Token estimate exceeds 110K
- A Contract amendment is required
- The Starlark sandbox surface from Story 1.6 doesn't match what the AC expects (e.g., the four forbidden ops aren't all literally named in 1.6's brief — adjust the test to whatever 1.6 actually blocks, and report any gap in the closing summary)

## Closing

1. Verify all 14 deliverables
2. `make down && make up && make verify-bootstrap` green
3. `make test-bypass` → 4/4 BLOCKED, Gate 2 marker written, exit 0
4. Paste the full gate2-report.txt contents into the closing summary
5. Update token tracker Row 1.10 — round UP
6. Closing summary: bypass-by-bypass result, enforcement layer cited, Gate 2 status, token estimate (honest), open questions, **Phase 1 Gate 2 verdict: PASSED / NOT PASSED**

Do NOT commit. Winston + Andrew review and commit.
