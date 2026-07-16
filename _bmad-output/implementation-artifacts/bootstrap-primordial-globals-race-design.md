# Bootstrap primordial-ID globals race — one populate per process, mirrored under test

**Status: 📐 awaiting-Andrew (ratification).** · Designer (Winston), 2026-07-16 · Lattice lane (Stream 2)
Backlog row: *"`internal/bootstrap` primordial-ID globals race"* —
`_bmad-output/planning-artifacts/backlog/lattice.md` → Refinements & ops.

---

## For Andrew

**What it does (two lines).** Fixes the confirmed `-race` data race on `internal/bootstrap`'s ~64 primordial-ID
package globals by making the **test harness populate them once per test process** — the same
populate-at-boot-then-read-only lifecycle every production binary already follows — instead of re-populating per
test. Unblocks `t.Parallel()` for the three package suites the Whetstone had to revert (`packages/lease-signing`,
`packages/clinic-domain`, `packages/identity-domain`), with a lint gate so the per-test-populate pattern can't
creep back.

**The one call for you.** The board row asked "test-scoped vs. universal fix + return-shape change." My
recommendation is the **test-scoped fix** (§4) and an explicit **rejection of the universal instance refactor**
(§5.1): grounding shows the race is a *test-harness lifecycle deviation*, not a production bug — every production
process populates once, single-threaded, before any concurrency exists (§3.2). The instance refactor would churn
~350 references across ~100 files for no functional production win and no nameable consumer of
multiple-primordial-sets-per-process. **No frozen-contract change; no architectural fork** — Contract #7 and the
NFR-SC2/FR48 per-deployment-uniqueness property are untouched (production ID generation is not modified at all).

**Build sequencing.** Two small fires, independently shippable (§7). Fire 1 is the fix + the three reverted
`t.Parallel()` re-applications with `-race` proof; Fire 2 is the mechanical migration of the ~20 suite-local
harnesses to the same helper + the lint gate. Honest payoff note: local suite wall-clock drops are real
(the same change class measured 42–70% on processor/outbox in `c22b3a6`), but the *CI shard* win is bounded by
the runner's confirmed ~2x aggregate-CPU ceiling — the correctness fix and the unblocked path are the durable
value; the wall-clock win fully materializes if/when larger runners land.

---

## 1. Problem & demand

`internal/bootstrap/nanoid.go` holds the primordial NanoID inventory as ~64 package-level `var`s
(`BootstrapIdentityID`, `LoomIdentityKey`, …), populated by `populate()` from `LoadOrGenerate` / `Load`.
`testutil.SetupPackageTestEnv` (internal/testutil/pipeline.go:276) calls `bootstrap.LoadOrGenerate` with a fresh
temp path **per test**, generating a *fresh* ID set and overwriting all ~64 globals each time.

Two tests running under `t.Parallel()` in the same test binary therefore stomp each other twice over:

- **The data race** (what `-race` reports): concurrent unsynchronized write/read of the globals. Confirmed 3×
  by the Whetstone (`c22b3a6`, 2026-07-13) when it attempted to parallelize `packages/lease-signing`,
  `packages/clinic-domain`, and `packages/identity-domain`.
- **The logical stomp** (the deeper defect a naive fix would miss): test A seeds *its own* embedded NATS server
  with ID set 1; test B then replaces the globals with ID set 2 mid-flight; test A's later reads
  (`bootstrap.BootstrapIdentityID` in cap-doc seeding, key construction, assertions) silently address a
  deployment that doesn't exist on its server. A mutex would silence the race report and leave this fully intact
  (§5.3).

Demand is grounded: Whetstone-filed after a real reproduction, and the CI-speed board row names this item as the
next `t.Parallel()` unlock. The Whetstone correctly refused to touch it per its bounds (never mask a real race).

## 2. Didn't we already handle this? (reconciliation)

- **The globals are intentional, and their contract is already written.** nanoid.go:59–66: *"MUST be populated
  via LoadOrGenerate (cmd/bootstrap) or Load (read-only callers) BEFORE any consumer accesses these variables"* —
  i.e. populate once at boot, read-only thereafter. Runtime generation per deployment is an architectural
  requirement (NFR-SC2 cell-agnostic keys, FR48 multi-deployment isolation), which is why these are `var`s and
  not constants. Nothing about that is wrong.
- **Production honors the contract everywhere.** The full entry-point census (§3.2) shows every production
  binary and one-shot script populates exactly once, at `main()` start, before spawning any goroutine.
- **The test harness is the lone violator.** It re-populates per *test*, not per *process* — a lifecycle no
  production process has. The fix is to make the harness mirror production, not to redesign the production shape
  around the harness.
- **No new state is introduced.** The fix adds only a `sync.Once` memo in `internal/testutil` (test-only by
  construction — never linked into a production binary).

## 3. Grounding

### 3.1 The mechanism

`populate(raw)` (nanoid.go:501) validates then assigns all ~64 globals (IDs, derived vertex keys, derived link
keys). Entry points: `LoadOrGenerate(path)` (generate-or-load + two-phase-commit file), `Load(path)` (read-only
load), both ending in `populate`. `SeedPrimordial`, `SystemActorKeys`, envelope construction, and every external
consumer read the globals directly.

### 3.2 Entry-point census (who populates, and when)

- **Production binaries — populate once, single-threaded, then read-only:** `cmd/bootstrap` (LoadOrGenerate →
  seed → PersistCommitted), and `Load` at `main()` start in `cmd/{processor, loom, weaver, bridge, gateway,
  loupe, object-store-manager, cafe-app, clinic-app, loftspace-app, wellness-app}` + `cmd/lattice` subcommands.
  All populate before any concurrent reader exists. **There is no production race.**
- **One-shot scripts — same shape:** `scripts/verify-kernel.go`, `verify-package-*.go`, `seed-edge-demo.go`,
  `verify-real-actor-write-auth.go`, `verify-edge-revocation-e2e.go`, `verify-loupe-operator-tier.go`.
- **Tests — the violators (per-test populate):** `testutil.SetupPackageTestEnv` (used by ~30 test files across
  `packages/*` + `internal/{aiagent, cryptoshred}` + `cmd/lattice/*`), plus ~20 suite-local harnesses calling
  `bootstrap.LoadOrGenerate` directly per test: `internal/refractor`'s 11 e2e files,
  `internal/{leaseconvergence, augurconvergence, unroutedconvergence, systemactorcapability, controlplaneauthz,
  objectgc}`, `internal/pkgmgr/installer_test.go`, `packages/{rbac-domain, location-domain}/install_flow_test.go`.
  All discard the JSON path after the call (verified — the file is never re-read or handed to a subprocess).
- **Stays as-is:** `internal/bootstrap`'s own suite (nanoid_test.go exercises regenerate/version-mismatch/crash
  -recovery semantics with deliberate repeated loads; envelope_test.go *writes* two globals directly) — it tests
  the mechanism itself, runs serially in-package, and must keep calling the real thing.
  `internal/hellolattice` `Load`s the *live stack's* bootstrap.json once per process — already correct.
- **Reference volume (the board's "~90 call sites"):** ~350 global references across ~100 files; ~20 of those
  files are production/scripts, the rest tests. This is the churn the universal refactor (§5.1) would carry.

### 3.3 Why sharing one ID set per test process is safe

Each test owns a private embedded NATS server (`StartEmbeddedNATS`: random port + per-test `jsstore.Dir(t)`
store) and seeds it independently. NFR-SC2/FR48 uniqueness is a *per-deployment* property protecting real
environments that share infrastructure/backups; throwaway per-test servers are disjoint substrates, so identical
primordial IDs across them collide with nothing. No test asserts cross-test ID distinctness (verified: the only
multi-load assertions live in `internal/bootstrap`'s own suite, which stays direct). No test outside
`internal/bootstrap` writes the globals (verified by grep). Durables, buckets, and store dirs — the per-test
uniqueness that *does* matter — come from `cfg.Durable` / `jsstore.Dir(t)`, not from primordial IDs.

## 4. The design (recommended): populate once per test process, in `internal/testutil`

A new test-only helper, `internal/testutil/primordials.go`:

```go
var (
    primordialsOnce sync.Once
    primordialsErr  error
)

// EnsurePrimordials populates internal/bootstrap's primordial ID set exactly
// once per test process — the production lifecycle (populate at boot, read-only
// thereafter). Every embedded-NATS test server in the process is seeded from
// this one set; servers are disjoint, so sharing collides with nothing.
func EnsurePrimordials(t *testing.T) {
    t.Helper()
    primordialsOnce.Do(func() {
        dir, err := os.MkdirTemp("", "lattice-test-bootstrap-*")
        if err != nil { primordialsErr = err; return }
        _, primordialsErr = bootstrap.LoadOrGenerate(filepath.Join(dir, "lattice.bootstrap.json"))
    })
    if primordialsErr != nil {
        t.Fatalf("testutil.EnsurePrimordials: %v", primordialsErr)
    }
}
```

Notes on the shape (decisions, not TBDs):

- **`sync.Once` gives the memory-model guarantee for free:** every `Do` returner happens-after the populate, so
  all subsequent global reads by any parallel test are race-free — no mutex on the globals, no change to
  `internal/bootstrap` at all.
- **Process-scoped temp dir, not `t.TempDir()`:** the first test's cleanup would delete a `t.TempDir()` mid-run.
  Nothing re-reads the file (it stays `status="in-progress"`; `PersistCommitted` is deliberately not called — no
  consumer), but the file shouldn't dangle from a deleted directory. The dir leaks for the process lifetime,
  which is what OS temp dirs are for.
- **The memoized error re-fails every subsequent test** — fail-fast for the whole binary, same posture as today's
  per-test `t.Fatalf`.
- **`SetupPackageTestEnv` swaps its `tmpPath` + `LoadOrGenerate` block for `EnsurePrimordials(t)`** — a 4-line
  diff; everything downstream (`NewSeeder`, `SeedPrimordial`, `InstallPhase1Packages`, test-body global reads) is
  unchanged.
- **Consumers keep reading `bootstrap.*` globals.** That is the point: the minimal fix restores the documented
  lifecycle instead of re-plumbing ~350 references.

**The lint gate (Fire 2)** makes the hazard structurally unrepeatable: `scripts/lint-conventions.go` gains a rule —
`bootstrap.LoadOrGenerate` in any `*_test.go` outside `internal/bootstrap/` and `internal/testutil/` is an error
pointing at `testutil.EnsurePrimordials`. (`bootstrap.Load` stays un-gated: hellolattice's live-stack load is
legitimate, and `Load` in one-shot scripts is the production pattern.) Fresh-worktree agents copy precedent; after
Fire 2 the only copyable precedent is the correct one.

## 5. Alternatives considered

### 5.1 Universal instance refactor (the board's "return-shape change") — REJECTED, with a revive condition

Make `Load`/`LoadOrGenerate` return a `Primordials` struct; thread it through `Seeder`, `testutil`, and every
consumer; delete the globals. Rejected because:

- **No functional production win.** Every production process is populate-once-then-immutable (§3.2); a struct
  changes where the bytes live, not any behavior. The race this item files is fully closed by §4.
- **No nameable consumer of the capability it adds.** The only thing instances enable that §4 doesn't is
  *multiple primordial sets in one process*. Multi-cell is process-per-cell by design; Edge nodes don't load
  primordials; no test needs per-test-distinct sets (§3.3). Building it now is dead scaffolding.
- **~350 references / ~100 files of churn in a crowded multi-fire tree** — a merge-conflict magnet touching
  every in-flight fire's test files, for the above non-win.

**Revive condition:** a real in-process multi-primordial-set consumer appears (e.g. an embedded multi-cell test
harness, or embedding Lattice as a library). The refactor is then mechanical and this doc's census (§3.2) is its
work-list. A middle path — additive `LoadOrGenerate` also *returning* the struct while keeping the globals — was
considered and rejected for the same reason: a return value nothing consumes is scaffolding.

### 5.2 Memoize inside `bootstrap.LoadOrGenerate` itself — REJECTED

First call wins process-wide, later calls no-op. Touches production semantics to serve tests (the Whetstone
bound, inverted), and breaks `internal/bootstrap`'s own suite, which deliberately loads different files
sequentially in one process (regeneration, version-mismatch, crash-recovery paths). Test policy belongs in
`testutil`, not in the mechanism under test.

### 5.3 Mutex/atomic around the globals — REJECTED

Silences the `-race` report; leaves the logical stomp (§1) fully intact — test A still reads test B's IDs
mid-flight and addresses a deployment its server never seeded. Worse than the disease: green `-race`, wrong tests.

### 5.4 Keep the suites serial — REJECTED (status quo)

Leaves a confirmed data race in the tree behind a "do not parallelize" tribal-knowledge fence, and permanently
caps the named CI unlock. The fence isn't even marked — the next agent to add `t.Parallel()` re-trips it.

## 6. Contract surface, test strategy, risks

**Contracts:** none touched. Builds to Contract #7 (bootstrap seeding untouched) and Contract #1 (no key-shape
changes). `lattice-architecture.md` NFR-SC2/FR48 preserved — production ID generation unmodified.

**Test strategy (the proof each fire carries):**

- Fire 1: `go test -race -count=3 ./packages/lease-signing ./packages/clinic-domain ./packages/identity-domain`
  green with `t.Parallel()` re-applied (the exact diff `c22b3a6` reverted); full `go test ./...` + the standard
  gates green; before/after local wall-clock recorded per suite (honest note: CI shard delta may be ~flat under
  the known 2x aggregate-CPU ceiling — record it either way).
- Fire 2: migration is behavior-neutral per suite (same helper, same downstream); full suite green;
  `STRICT=1 go run ./scripts/lint-conventions.go` green including the new rule, and the rule demonstrably fires
  on a synthetic violation.

**Risks:**

- *A migrated suite secretly depended on fresh-per-test IDs.* Mitigated by the §3.3 audit (none found: no
  cross-test ID assertions, no global writes outside `internal/bootstrap`, all harnesses discard the JSON path);
  the per-suite green bar in Fire 2 catches any miss.
- *Another latent shared-state race surfaces once these suites parallelize.* Possible and fine — that is `-race`
  doing its job on a newly-exercised schedule; Fire 1's `-count=3 -race` bar surfaces it before commit, and the
  Whetstone bound applies (file it, don't mask it).
- *The leaked process-temp dir.* Bytes of JSON in OS tmp per test binary run; not worth a cleanup hook.

**Adversarial pass (run this fire, findings folded in):** the logical-stomp-vs-data-race distinction (§1, kills
the mutex option); the `t.TempDir()` first-test-cleanup trap (§4, drove the process-scoped dir); the
`internal/bootstrap`-suite exemption incl. envelope_test.go's direct global writes (§3.2); the hellolattice
live-file exemption shaping the lint rule to `LoadOrGenerate`-only (§4); the honest CI-ceiling bound on the
payoff claim (For-Andrew block).

## 7. Fire decomposition for the Steward (after ✅ Andrew-ratified)

- **Fire 1 — the fix + the named unlock (S).** Add `testutil.EnsurePrimordials`; swap it into
  `SetupPackageTestEnv`; re-apply the reverted `t.Parallel()` to `packages/{lease-signing, clinic-domain,
  identity-domain}`; prove with `-race -count=3` + full gates; record local wall-clock deltas (and the CI shard
  delta, flat or not).
- **Fire 2 — close the pattern (S–M).** Migrate the ~20 suite-local `bootstrap.LoadOrGenerate` test call sites
  (§3.2 census is the work-list) to `EnsurePrimordials`; add the lint-conventions rule; leave
  `internal/bootstrap`'s own suite and `internal/hellolattice` untouched. Optionally re-apply `t.Parallel()`
  where a migrated suite's tests are already per-server isolated — each such application carries its own
  `-race` proof, and is skippable without weakening Fire 2.
