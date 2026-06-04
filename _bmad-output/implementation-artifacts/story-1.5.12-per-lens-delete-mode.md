# Story 1.5.12 — Per-lens delete-projection mode (default hard)

**Status:** SPEC — Winston-authored + adjudicated (orientation done in the parent session; §0 is the build target).
**Tier:** Opus (cross-adapter + pipeline-wiring + LensSpec contract + a security-plane decision).
**Epic spec:** `_bmad-output/planning-artifacts/epics/phase-1-epics.md` → "Story 1.5.12" (line ~1300). Read that block for the user story + acceptance criteria; THIS file is the binding build target where it elaborates or overrides.
**Depends on:** none (refactor of the existing adapter delete path).
**Workflow:** you are the DS sub-agent. Work in the repo root (`/Users/andrewsolgan/Documents/GitHub/Lattice`), NOT a worktree. Do **NOT** commit, push, or edit planning artifacts (`epics/*.md`, `data-contracts.md`, `lattice-architecture.md`, `MORPH-DEVIATIONS.md`). Questions back to Winston → append to `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` and continue with a different deliverable.

---

## 0. ADJUDICATION — FINAL (Winston). DS builds to THIS.

### 0.1 What "delete mode" is and where it lives

Delete mode is a **per-lens, construction-time** property — NOT a per-call argument. The `Adapter` interface (`internal/refractor/adapter/adapter.go`) `Delete(ctx, keys)` signature stays **UNCHANGED**. Each adapter is *constructed* with its mode (read from the lens spec by `cmd/refractor/main.go buildAdapter`) and stores it on its struct. This satisfies the AC's "the pipeline delete path passes the lens's delete-mode through to the adapter" (the adapter the pipeline calls already carries its mode) without churning the interface or the ~7 mock adapters in `pipeline_test.go`. **This is a deliberate Winston correction of the AC's literal "passes through per call" reading — record it as such; do not add a mode param to `Adapter.Delete`.**

Representation:
- New typed string in package `adapter`: `type DeleteMode string` with `DeleteModeHard DeleteMode = "hard"` and `DeleteModeSoft DeleteMode = "soft"`. Add a helper `ParseDeleteMode(s string) (DeleteMode, error)` that maps `""`→`DeleteModeHard` (default), `"hard"`/`"soft"`→themselves, anything else→error. Put it in a small new file `internal/refractor/adapter/deletemode.go` (keeps `adapter.go` the pure interface file).
- `IntoConfig` (`internal/refractor/lens/schema.go`) gains `DeleteMode string` with `yaml:"delete_mode"`. `Parse()`/validation defaults absent→`"hard"` and rejects values outside {`hard`,`soft`} (structural error, consistent with the other Into validation).
- `TargetPostgresConfig` + `TargetNATSKVConfig` (`internal/refractor/lens/corekv_source.go`) each gain `DeleteMode string` with `json:"deleteMode,omitempty"`. The spec→Rule conversion (`corekv_source.go` ~line 355/371) sets `IntoConfig.DeleteMode`, defaulting absent→`"hard"`, validating the value (reuse `adapter.ParseDeleteMode` for the validation, or a local equivalent — your call, but ONE source of truth for the allowed set).

### 0.2 Adapter behavior

**PostgresAdapter** (`internal/refractor/adapter/postgres.go`):
- `NewPostgresAdapter` gains a `deleteMode adapter.DeleteMode` param (or `DeleteMode` since same package). Store on the struct.
- `buildDeleteSQL`: branch on mode.
  - HARD → `DELETE FROM "<table>" WHERE <clauses>`.
  - SOFT → the current `UPDATE "<table>" SET is_deleted=true, deleted_at=NOW() WHERE <clauses>`.
- Idempotency: `pool.Exec` of a DELETE matching zero rows returns nil — already idempotent. Keep the "zero rows is not an error" guarantee for BOTH modes.

**NatsKVAdapter** (`internal/refractor/adapter/natskv.go`):
- `New` gains a `deleteMode DeleteMode` param. Store on the struct.
- `Delete`: branch on mode.
  - HARD → `a.kv.Delete(ctx, key)`. **Verify idempotency on a never-existed key**: if `jetstream` returns an error for deleting an absent key, swallow `jetstream.ErrKeyNotFound` (and `nats.ErrKeyNotFound` if distinct) → return nil. (The existing `TestNatsKVAdapter_Delete_NeverExisted` currently exercises the soft path; add a hard-mode twin — see §0.5.)
  - SOFT → the current tombstone `Put({isDeleted:true, projectedAt:...})`.
- The stale comment block on the current `Delete` ("Soft-delete semantics per Contract #1 ensure downstream auth-freshness readers see a current timestamp") must be **rewritten**: freshness-ceiling comparison was removed in Story 1.5.4 (see `internal/processor/step3_auth_capability.go:176-182` — "no longer compared against any freshness ceiling"). The new comment documents the two modes and that absence and tombstone are BOTH treated as denial by the capability authorizer.

### 0.3 Wiring

- `cmd/refractor/main.go buildAdapter` (~line 160-186): parse `r.Into.DeleteMode` (already defaulted to "hard" upstream) via `adapter.ParseDeleteMode` and pass it to `adapter.New(...)` and `adapter.NewPostgresAdapter(...)`.
- `internal/refractor/fixture/runner.go:54` `adapter.New(targetKV, ...)` callsite: pass the fixture rule's mode (`fix.Rule.Into.DeleteMode` → `ParseDeleteMode`); fixtures with no `delete_mode` default hard.
- Any other `adapter.New` / `NewPostgresAdapter` callsite the build surfaces (`go build ./...` is the gate) gets the new arg; default hard unless the source spec says otherwise.

### 0.4 Capability / auth-plane lenses — HARD (Andrew's decision, 2026-06-03)

The primordial capability lenses (`internal/bootstrap/primordial.go` ~line 836 `targetConfig`: the per-identity capability lens and `capabilityRoleIndex`) **also use the default HARD delete.** Andrew confirmed: the capability KV is a cache/plane, not an audit target; the capability authorizer already treats an absent key (`ErrKeyNotFound`→`NoCapabilityEntry`) and a tombstone doc identically as denial, and the freshness gate that originally justified soft-delete on this plane was removed in Story 1.5.4. Hard-delete is the correct, contract-aligned semantics (Contract #6 §6.8 "absence equals denial") and avoids indefinite tombstone accumulation in the capability KV.

**Do NOT pin a `deleteMode` in `primordial.go` — let the capability lenses inherit the default (hard).** No `"deleteMode"` key in the `targetConfig` map (absent → hard).

**Watch the security regression gates closely.** Because this changes the capability plane's delete behavior from tombstone→physical-delete, if `make verify-bootstrap`, `make test-bypass` (Gate 2), `make test-capability-adversarial` (Gate 3), or any capability test asserts the *presence* of an `isDeleted` tombstone on the capability KV after a revoke/delete, that assertion must be **updated to assert absence** (the key is gone) rather than a tombstone — the new behavior is correct, so the test was encoding the old soft-delete behavior. Flag any such test change explicitly in your closing summary so Winston can verify it's a semantics update, not a masked regression. If a gate fails in a way that is NOT a simple tombstone→absence assertion swap (e.g. an actual auth bypass), STOP and file a CONTRACT-AMENDMENT-REQUEST.

### 0.5 Docs (you MAY edit these — they are under /docs, not planning artifacts)

- `docs/components/refractor.md:62` — the Postgres-rows row: soft-delete columns are now required ONLY for `deleteMode: soft` targets; default is hard `DELETE`. State the mode-dependence.
- `docs/components/refractor-failure-tiers.md` "Tombstone semantics" (~line 37-44) — rewrite: delete semantics are mode-dependent, **default hard** (`DELETE FROM` / `kv.Delete`); `deleteMode: soft` opts into the tombstone behavior for audit/forensic targets; note the capability plane is pinned soft.
- `docs/hello-lattice.md:156` — the Milestone-4 `CREATE TABLE` currently includes `is_deleted BOOLEAN ... , deleted_at TIMESTAMPTZ`. A default-hard target needs neither → drop those two columns so the table is `CREATE TABLE IF NOT EXISTS books (book_id TEXT PRIMARY KEY, title TEXT);`. (Note: `internal/hellolattice/hellolattice_test.go:164` ALREADY creates the book table without soft columns — no change needed there; it is now consistent with the default.)

### 0.6 Gates (all must pass before you hand back)

`go build ./...` · `make vet` · `golangci-lint run ./...` (watch for unused helpers) · `make verify-bootstrap` · `make test-bypass` (Gate 2) · `make test-capability-adversarial` (Gate 3) · `go test ./internal/refractor/... -count=1` · the adapter integration tests (`go test ./internal/refractor/adapter/... -count=1`; Postgres integration tests are build-tagged — run them if the docker stack is up, otherwise note they were not run). Flake retry per Deviation 14 is allowed; a flake claim without a re-run is a drift signal.

---

## 1. Test plan (the AC's "tests cover" line, made concrete)

Both adapters × both modes; pipeline delete routing; e2e create→tombstone→gone (hard) + retained (soft).

1. **Postgres `buildDeleteSQL`** (unit, no DB): hard mode → `DELETE FROM "books" WHERE "book_id" = $1`; soft mode → the existing `UPDATE ... SET is_deleted=true`. Extend the existing `TestBuildDeleteSQL_*` tests with a mode dimension.
2. **Postgres integration** (build-tagged): hard-delete removes the row (`SELECT` returns 0 rows); hard-delete of an absent row is a no-op (no error); soft-delete still sets `is_deleted=true`. Mirror the existing `TestPostgresAdapter_Delete_*Integration`.
3. **NatsKV** (`natskv_test.go`): hard mode → key is gone after Delete (`Get`→ErrKeyNotFound); hard-delete of never-existed key → nil; soft mode → tombstone doc present with `isDeleted:true`. Add hard twins of `TestNatsKVAdapter_Delete` + `TestNatsKVAdapter_Delete_NeverExisted`.
4. **Spec→Rule conversion** (`corekv_source` test): `deleteMode` absent → `IntoConfig.DeleteMode == "hard"`; `"soft"` → soft; invalid value → error. Both postgres + nats_kv targetConfig shapes.
5. **YAML schema** (`schema` test, if present): `delete_mode` absent → hard; invalid → parse error.
6. **End-to-end** (pipeline or fixture level, whichever the existing delete e2e uses — see `pipeline_test.go:267 TestPipeline_Delete` and `internal/refractor/fixture`): a create→tombstone cycle on a default (hard) lens leaves the target with NO row/key; the same on a `deleteMode: soft` lens leaves a tombstone. Reuse existing harness; don't build a new one.

Count your delivered tests from the diff (not memory) in the closing summary.

## 2. Closing summary (append here when done)

When complete, append: deliverables vs this spec's checklist, the exact files changed (from `git status`), test count (from the diff), which gates ran + their result (and any not run, with reason), and any CONTRACT-AMENDMENT-REQUEST you filed. Do NOT commit.

---

### DS closing summary (2026-06-03)

**Deliverables vs §0 checklist — all done:**

- §0.1 Representation: new `internal/refractor/adapter/deletemode.go` with `type DeleteMode string`, `DeleteModeHard`/`DeleteModeSoft`, and `ParseDeleteMode` (`""`→hard, hard/soft→themselves, else error — the single source of truth). `IntoConfig.DeleteMode` (`yaml:"delete_mode"`) added; `Parse()` defaults absent→hard and rejects out-of-set via `adapter.ParseDeleteMode`. `TargetPostgresConfig`/`TargetNATSKVConfig` each gained `DeleteMode` (`json:"deleteMode,omitempty"`); `translateSpec` defaults+validates both shapes. `Adapter.Delete` signature UNCHANGED (mode is construction-time).
- §0.2 PostgresAdapter: `NewPostgresAdapter` gained `deleteMode` param, stored on struct; `buildDeleteSQL` branches hard→`DELETE FROM` / soft→`UPDATE … SET is_deleted=true`. NatsKvAdapter: `New` gained `deleteMode`; `Delete` branches hard→`kv.Delete` (swallows `jetstream.ErrKeyNotFound` for idempotency) / soft→tombstone Put. The stale Contract-#1 freshness comment was rewritten to document both modes and absence==tombstone==denial.
- §0.3 Wiring: `cmd/refractor/main.go buildAdapter` parses `r.Into.DeleteMode` and passes typed mode to both constructors; `internal/refractor/fixture/runner.go` passes `fix.Rule.Into.DeleteMode`. `go build ./...` clean — no other callsites.
- §0.4 Capability plane: confirmed `internal/bootstrap/primordial.go` targetConfig (~836) has NO `deleteMode` key → capability + capabilityRoleIndex lenses INHERIT the default HARD. No pin added.
- §0.5 Docs: `docs/components/refractor.md` (Postgres-rows row now mode-dependent), `docs/components/refractor-failure-tiers.md` ("Tombstone semantics" → "Delete-projection semantics", default hard, capability plane hard), `docs/hello-lattice.md` (Milestone-4 `CREATE TABLE books` drops `is_deleted`/`deleted_at`).
  - NOTE: §0.5's parenthetical "note the capability plane is pinned soft" CONTRADICTS §0.4 (final, which says HARD). Per the adjudication precedence (§0 overrides) and the DS brief, the docs were written to document the capability plane as HARD. Flagging for Winston.

**Capability-KV tombstone→absence test change (per §0.4 — flagged for Winston):**

- `internal/refractor/refractor_capability_multi_e2e_test.go` subtest "tombstone identity deletes cap entry": the old assertion accepted absence OR an `isDeleted` tombstone on `cap.identity.<C>`. Tightened to assert ABSENCE only (`errors.Is(err, jetstream.ErrKeyNotFound)`), with a comment explaining the capability plane now hard-deletes. This is a semantics update (old soft behavior → new hard behavior), NOT a masked regression — the capability authorizer treats absence == denial. The 6 `adapter.New(...)` callsites across the capability e2e tests were given `adapter.DeleteModeHard` (matching the primordial inheritance).
- No bypass/capadv (`internal/bypass`) test asserted tombstone PRESENCE on the capability KV; none required swapping. Gate 2 + Gate 3 passed unmodified.

**Files changed (from `git status`):**

Source (non-test): `cmd/refractor/main.go`, `internal/refractor/adapter/natskv.go`, `internal/refractor/adapter/postgres.go`, `internal/refractor/adapter/deletemode.go` (new), `internal/refractor/fixture/runner.go`, `internal/refractor/lens/corekv_source.go`, `internal/refractor/lens/schema.go`.
Tests: `internal/refractor/adapter/natskv_test.go`, `internal/refractor/adapter/postgres_test.go`, `internal/refractor/adapter/deletemode_test.go` (new), `internal/refractor/lens/schema_test.go`, `internal/refractor/lens/deletemode_translate_test.go` (new), `internal/refractor/pipeline/pipeline_test.go`, `internal/refractor/refractor_capability_aspectfanout_e2e_test.go`, `internal/refractor/refractor_capability_e2e_test.go`, `internal/refractor/refractor_capability_linkfanout_e2e_test.go`, `internal/refractor/refractor_capability_multi_e2e_test.go`, `internal/refractor/refractor_e2e_test.go`.
Docs: `docs/components/refractor.md`, `docs/components/refractor-failure-tiers.md`, `docs/hello-lattice.md`.
Artifacts regenerated by the gate runs (not hand-edited): `_bmad-output/implementation-artifacts/gate2-report.txt`, `gate3-report.txt`.

**Test count (from the diff):** 16 new top-level `Test*` functions:
- adapter: `TestParseDeleteMode`, `TestNatsKVAdapter_Delete_Hard`, `_Delete_Soft`, `_Delete_NeverExisted_Hard`, `_Delete_NeverExisted_Soft`, `TestBuildDeleteSQL_{SingleKey,CompositeKey}_{Hard,Soft}` (4), `TestPostgresAdapter_Delete_Soft_Integration` (build-tagged path / DSN-gated).
- lens: `TestParse_DeleteMode_{DefaultHard,Soft,Invalid}` (3), `TestTranslateSpec_{Postgres,NatsKV}_DeleteMode` (2, each with 3 subtests covering absent/soft/invalid).
- pipeline: `TestPipeline_Delete_SoftMode` (new); `TestPipeline_Delete` was rewritten from soft-tombstone assertion to hard key-gone assertion (default mode).

**Gate results (§0.6):**

- `go build ./...` — PASS
- `make vet` — PASS
- `golangci-lint run ./...` — PASS (0 issues; no unused helpers)
- `make verify-kernel` (the renamed `verify-bootstrap` — no `verify-bootstrap` target exists in the current Makefile) — PASS (ALL ASSERTIONS PASSED)
- `make test-bypass` (Gate 2) — PASS (4/4 BLOCKED; gate2-report.txt regenerated, Health KV marker written)
- `make test-capability-adversarial` (Gate 3) — PASS (4/4 cleared: 3 DEFENDED, 1 ACCEPTED-WINDOW; gate3-report.txt + marker written)
- `go test ./internal/refractor/... -count=1` — PASS (all packages green)
- `go test ./internal/refractor/adapter/... -count=1` — PASS
- Postgres integration tests (build-tagged / DSN-gated, incl. the new `TestPostgresAdapter_Delete_Soft_Integration`) — NOT RUN: `POSTGRES_TEST_DSN` unset in this environment; they `skipIfNoPostgres`. NatsKV adapter tests DID run (they use an in-process embedded NATS server, not Docker).

**CONTRACT-AMENDMENT-REQUEST:** none filed.

**Open question for Winston:** the §0.5 docs bullet's "capability plane is pinned soft" parenthetical contradicts §0.4's final HARD decision. Docs were written per §0.4 (hard). Please confirm.

---

## 3. Winston review + CR (inline) — 2026-06-03

Independent CR sub-agent was cut off by a session limit before reporting; Winston completed the adversarial pass inline (Andrew's call).

**Drift-detection: clean.** Deliverables match §0; no planning artifacts touched; `primordial.go` untouched (capability lenses inherit hard); defaults resolve to HARD in every path (`ParseDeleteMode`, `translateSpec`, YAML `Parse`, `buildAdapter`, fixture runner); single source of truth for the allowed set; no import cycle (lens→adapter only); no anti-patterns. Reverted gate-report timestamp churn (regeneration noise; results unchanged).

**Security-plane (highest risk) — cleared.** Hard-deleting the capability KV opens no deny-by-absence bypass. Both readers are safe: (1) the authorizer maps `ErrKeyNotFound`→`NoCapabilityEntry` denial (`step3_auth_capability.go:152`); (2) `fetchRolesCarryingPermission` is denial-path-only and returns `[]` on a missing key (`step3_denial_response.go:203`) — observability enrichment, never an allow. No reader interprets an absent capability key as authorization. The `refractor_capability_multi_e2e` assertion was correctly tightened to absence-only and passes against live NATS.

**Gates re-run by Winston (docker stack up):** `go build ./...` ✓ · `make vet` ✓ · lens unit tests ✓ · adapter unit + **Postgres integration with `POSTGRES_TEST_DSN` set** (hard/soft/composite/idempotent/soft-variant) ✓ · natskv hard/soft/never-existed ✓ · `TestRefractor_CapabilityLens_MultiIdentity_E2E` ✓. Plus DS-run: lint (0), verify-kernel, Gate 2 (4/4 BLOCKED), Gate 3 (4/4).

**Doc note resolved:** §0.5's stale "capability plane is pinned soft" phrase predated the §0.4 flip to HARD; DS correctly documented the plane as hard. No action.

**Verdict: no blockers. Committing to main + watching CI.**
