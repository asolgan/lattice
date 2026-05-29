# Code Review — Story 1.5.1: Substrate Write-Path Contracts

**Reviewer:** Claude (CR sub-agent) · **Date:** 2026-05-29
**Scope:** Working-tree diff for Story 1.5.1 (context-aware batch helpers + committed-revision propagation).
**Verdict:** APPROVE WITH CHANGES — 1 Major (latent hang regression in bootstrap), 0 Critical. Implementation is otherwise correct, faithful to the locked design, and well-tested.

---

## Files reviewed

`internal/substrate/batch.go`, `internal/processor/{step8_commit,step9_publish,steps_4_10_stub,reply,commit_path}.go`, `internal/bootstrap/primordial.go`, `internal/pkgmgr/installer.go`, `packages/identity-domain/seed.go`, `cmd/bootstrap/main.go` (call-site context), the three migrated test files, and `docs/contracts/03-mutation-batch-event-list.md`.

Independent verification this session: `go build ./...` clean; `go vet` clean across all touched packages. NATS was down, so the live empirical revision==stream-sequence assertion (`TestAtomicBatch_Commits`) could not be re-run here — relying on the implementer's reported live pass plus inspection of the (correct) assertion code.

---

## Findings

### MAJOR

**M-1 — Primordial seeding lost its timeout bound; the batch commit is now unbounded.**
`internal/bootstrap/primordial.go:230` now passes the inherited `ctx` straight into `AtomicBatch(ctx, ops)`, and the 30s hardcode was removed per design §3.1. The design justified this by asserting that `cmd/bootstrap/main.go` "already builds `readyCtx` from `BOOTSTRAP_READY_TIMEOUT_SEC` … that deadline now governs the batch."

That premise is factually wrong. In `cmd/bootstrap/main.go`:
- line 82: `ctx := context.Background()` (no deadline)
- line 93: `seeder.SeedPrimordial(ctx)` — called with the deadline-less `ctx`
- line 121: `readyCtx, cancel := context.WithTimeout(ctx, …)` — created *after* seeding, and only used for `WaitForBootstrapComplete` (line 124).

So `SeedPrimordial` receives a context with **no deadline**, and its `AtomicBatch` commit now goes through `nc.RequestMsgWithContext(ctx, m)` with no timeout. Previously the 30s literal bounded that round trip. Net effect: if NATS stalls during primordial seeding, bootstrap blocks indefinitely instead of failing after 30s. Tests don't catch it because live NATS answers fast.

This is a behavioral regression, not a style nit. The previous code's stale comment was even correct about the risk it was guarding against.

**Recommended fix (pick one):**
- Preferred: in `main.go`, wrap the seeding call: `seedCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second); defer cancel(); seeder.SeedPrimordial(seedCtx)`. This honors the spec's *intent* (caller owns the deadline, value driven by `BOOTSTRAP_READY_TIMEOUT_SEC`) and keeps primordial.go free of a hardcode.
- Alternative: re-introduce a bounded wrapper inside `SeedPrimordial` (e.g. `context.WithTimeout(ctx, 30*time.Second)`), but this re-adds the hardcode the story wanted gone.

Either way the locked design's stated rationale should be corrected — this warrants a note appended to `CONTRACT-AMENDMENT-REQUEST.md` since it contradicts a Winston-LOCKED decision.

---

### MINOR / OBSERVATIONS (no change required)

**O-1 — Revision derivation, guard, and filtering are correct.** `deriveRevisions` (batch.go) guards against both the size-mismatch case (`batchSize != len(ops)` → nil) and uint64 underflow (`lastSeq+1 < batchSize` → nil), and fabricates nothing. `mutationRevisions` (step8) correctly filters to `result.Mutations` keys (which equal the `BatchOp.Key`s set at step8_commit.go:121), so the idempotency tracker key — appended last at line 141 — is excluded as required by §3.3. Duplicate-key last-write-wins is acceptable per design.

**O-2 — step9 per-attempt deadline handling is correct.** `step9_publish.go:141-143` creates a fresh `WithTimeout` per retry attempt and calls `cancel()` immediately after each `PublishBatch` (not `defer`), so no context accumulates across the retry loop and each attempt gets the full `p.Timeout` — matching prior behavior. Good.

**O-3 — Reply wiring fulfills the previously-empty schema promise.** `OperationReply.Revisions` (envelope.go:130) was declared-but-always-nil; `commit_path.go:315` now feeds `commitAck.Revisions` through the new `BuildAcceptedReplyWithRevisions`, which composes `BuildAcceptedReplyWithDetail` and only sets the field when non-empty (omitempty preserved). Duplicate replies correctly carry no revisions (short-circuit before commit).

**O-4 — Fire-and-forget cancellation is best-effort, as designed.** The pre-publish `ctx.Err()` check in `publishAtomicBatch` (batch.go:278) only catches cancellation *between* sends, not mid-send — but nats.go offers no `PublishMsgWithContext`, so this is the available mitigation and matches the design note. Acceptable.

**O-5 — Doc cross-references verified.** New §3.9 in `03-mutation-batch-event-list.md` correctly cites Contract #2 §2.4 (Reply Envelope exists at that location) and accurately describes the derivation formula and the no-revisions-for-events stance.

**O-6 — All call sites and timeouts preserved.** installer (×2, `DefaultBatchTimeout`), seed (15s), stub (5s), step8 (`c.Timeout`) all wrap with `WithTimeout` + `defer cancel()` correctly. Test migrations are clean; the unused `time` import was removed from `state_machine_test.go` only (still used in the other two test files — verified). No history comments introduced.

---

## Triage summary

| Severity | Count | Items |
|----------|-------|-------|
| Critical | 0 | — |
| Major    | 1 | M-1 primordial unbounded-commit regression (design premise wrong) |
| Minor    | 0 | — |
| Observations | 6 | O-1..O-6, all confirming correct behavior |

**Disposition:** The revision-derivation and reply-wiring core of the story is correct and verified. Approve once M-1 is addressed — it is a small, localized fix (one `WithTimeout` wrap in `cmd/bootstrap/main.go`) but it restores a real safety bound the diff silently dropped, and the locked design's justification for dropping it does not hold against the actual `main.go` control flow.
