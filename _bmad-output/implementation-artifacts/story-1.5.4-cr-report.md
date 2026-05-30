# Story 1.5.4 — Capability auth freshness coherence (B4) — Adversarial CR Report

**Reviewer:** CR sub-agent · **Date:** 2026-05-29 · **Mode:** review-only (no commits/edits)
**Verdict:** **APPROVE WITH ONE P1** (stale observability doc). Core removal is complete, correct, honest, and did not weaken anything it shouldn't. Build + vet + the affected test packages were run and pass.

---

## Triage summary

| ID | Sev | Area | One-line |
|----|-----|------|----------|
| F-1 | **P1** | Docs completeness | `docs/observability/health-kv-schema.md` still documents the removed `cap-staleness` key + `auth-freshness-exceeded` alert code (5 sites). The wrong health doc was checked (`05-health-kv.md`) — this sibling was missed. |
| F-2 | **P2** | Stale comment | `internal/processor/latency_ring.go:14-16` doc comment references "emission for staleness (Decision #4)" — a concept that no longer exists. Misleading but harmless. |

No P0. Nothing material found in: removal completeness (code), preserved behavior, provenance derivation, fail-loud reachability, test-weakening, Gate-3 honesty, or component-doc accuracy. Details below.

---

## 1. Removal completeness — PASS (code), P1 (one doc)

- `go build ./...` clean. Grep across `internal/ cmd/ packages/` for `cap-staleness|auth-freshness-exceeded|AuthFreshnessExceeded|StalenessExceeding|exceedingNFRP3|StaleCeiling|checkFreshness` returns **zero code references**.
- `internal/processor/step3_auth_capability.go`: `checkFreshness`, `staleness *latencyRing`, `StalenessStats()`, `StalenessExceedingNFRP3()`, `StaleCeiling` config field, and the freshness-alert emit are all gone. The struct now holds only `latency *latencyRing`.
- `internal/processor/envelope.go`: `ErrCodeAuthFreshnessExceeded` removed cleanly. The denial-response builder (`step3_denial_response.go`) is coherent without it — its `switch dec.Code` handles only `AuthContextMismatch` then falls through to the AuthDenied path; no dangling reference, no orphaned branch.
- `health_alerts.go`: `auth-freshness-exceeded` severity case + doc comment removed; `alertSeverity` still handles `stub-auth-active`.
- `step3_auth.go`: `cfg.StaleCeiling` dropped from the default-config guard; `NewCapabilityAuthorizer` lost its `emitter` param; `opts.Emitter` is still legitimately consumed for the `stub-auth-active` path (not dead).

**F-1 (P1):** `docs/observability/health-kv-schema.md` is now inaccurate. It still lists, for a key that is no longer emitted and an alert code no longer fired:
- L40 — `health.processor.<instance>.cap-staleness` row in the Processor key-inventory table
- L51 — `<alertCode>` enum still includes `auth-freshness-exceeded`
- L58 — event-driven-keys bullet for `cap-staleness`
- L191-207 — the full `### health.processor.<instance>.cap-staleness` schema block
- L354 — the rollup note example list (`step3-latency, cap-staleness, ...`)

Why it matters: this is the consumer-facing Health KV schema reference; a dashboard/operator author would expect a key the system never writes. The story §2 pointed the dev at `docs/contracts/05-health-kv.md` (which correctly never enumerated `cap-staleness`, so no edit there was right) but the actual stale doc lives under `docs/observability/`. Honest-but-incomplete: the closing summary's claim "build clean with no dangling refs" is true for code; the doc surface was missed.
Fix: remove the 5 sites above (delete the row, the schema block, the enum entry, the two list mentions).

---

## 2. Preserved behavior — PASS (all four intact)

Verified in `step3_auth_capability.go` / `health.go`:
- **`ephemeralGrants[].expiresAt` expiry** — `matchEphemeralGrant` L236-247 still parses `expiresAt` and `continue`s on `!now.Before(expiresAt)`. Untouched.
- **cap-doc read + `NoCapabilityEntry` fail-safe** — L150-160: `ErrKeyNotFound` → `Decision{Authorized:false, Reason:"NoCapabilityEntry"}`. Intact.
- **Authorize latency ring + `step3-latency` Health signal** — ring recorded in the `Authorize` deferred close (L134-136); `health.go emitCapabilityAuthSignals` still emits `step3-latency` (always-emit, zero-sample liveness). The `cap-staleness` emission is the only thing removed there.
- **auth-trace records `projectedAt`** — `Authorize` threads `doc.ProjectedAt` into `ResolvedPermission.ProjectedAt` (L179-182) as provenance.

---

## 3. Provenance derivation (`evaluate.go projectedAtFromProvenance`) — PASS

- **Right field:** `lastModifiedAt` with `createdAt` fallback. Confirmed `lastModifiedAt` IS the committing op's timestamp: `step8_commit.go buildMutationValue` writes `lastModifiedAt = stamp` on create/update/tombstone (and `createdAt` on create). This is the correct deterministic provenance ("as-of input state"); using `createdAt` as the primary would freeze provenance at first-write and miss subsequent commits, so `lastModifiedAt`-primary is right.
- **Deterministic across replay:** pure function of `nodeProps` — no `time.Now()`. Same input vertex → identical value. F-009 closed.
- **Both engine paths covered:** full path `executeFullForActor` L142-145; simple path L105-108 (under `envelopeFn`); fan-out reuses `executeFullForActor`. All three derive via the same helper and propagate the error.
- **Fail-loud correctness + production reachability:** `ErrNoProvenanceTimestamp` (no wall-clock fallback) is correct per §3.2. **Reachability check passed** — it cannot break live projection for a real actor vertex:
  - Runtime-committed vertices always carry `lastModifiedAt` (`step8_commit.go`, all three ops).
  - The primordial admin identity (the only actor projected before any user op) is built via `bootstrap/envelope.go MakeVertexEnvelope` → `substrate.NewDocumentEnvelopeAt`, which sets `CreatedAt`/`LastModifiedAt` (JSON tags `createdAt`/`lastModifiedAt`, confirmed `substrate/envelope.go`).
  - So every real Core KV actor vertex carries the field; the guard fires only on a genuinely malformed/non-enveloped body, which is exactly the loud-failure case the design wants.

---

## 4. Test-weakening check — PASS (changes legitimate; one actually strengthens)

- **e2e fixtures (`refractor_capability_{e2e,multi_e2e}_test.go`):** both now seed actor vertices with `createdAt`/`lastModifiedAt`. This **legitimately mirrors real vertex shape** (real Core KV vertices always carry the universal envelope — confirmed §3). It is NOT masking the fail-loud guard: the guard's failure path remains reachable for genuinely malformed bodies; the fixtures simply stopped being unrealistic.
  - `refractor_capability_e2e_test.go` additionally **upgrades** the assertion from `require.NotEmpty(env["projectedAt"])` to `require.Equal(provenanceAt, env["projectedAt"])` — a strictly stronger check that proves deterministic derivation and would catch any wall-clock regression. This is the opposite of weakening.
  - `multi_e2e` adds the fields but does not assert `projectedAt` (only a comment). Acceptable — determinism is asserted in the single-identity test; the multi test just needs the fixture to pass the guard.
- **Removed processor tests** (`Freshness_AboveCeiling_DeniesAndAlerts`, `Freshness_AboveNFRP3_AllowsAndRecords`, `FreshnessDenialThreadsDoc`, `DenialBuilder_AuthFreshnessExceeded`) test behavior that no longer exists — correct to delete, not a coverage loss.
- **Converted tests preserve coverage:**
  - `StaleProjection_StillAllows` is a genuine positive assertion (10s-stale doc must allow + thread provenance + emit no alert).
  - `AuthTrace_DenialWithDoc_PlanesPopulated` retargets `AuthFreshnessExceeded`→`AuthDenied` to keep the "denial-with-doc populates planes 2+3" coverage.
- No other test was loosened to pass. `go test ./internal/processor/... -run 'TestCapabilityAuthorizer|TestAuthTrace|TestDenialBuilder'` → `ok`.

---

## 5. Gate-3 honesty — PASS (verified live)

Ran `go test ./internal/bypass -run 'TestCapAdv_V2|TestGate3_Report' -v`:
- **Vector #2 is non-vacuous.** `ExcessiveLag_NoLongerDenies` seeds a **1-hour-stale** `projectedAt` and asserts `dec.Authorized == true` — this would FAIL if any age-gate remained. It also asserts `projectedAt` is threaded as provenance and (separate test) that the auth-trace ALLOW record is written + observable with `plane1.projectedAt`. Genuinely asserts ALLOWED + observable.
- **Vectors #1/#3/#4 unchanged + DEFENDED** (the `capadv_*` diffs for those were the mechanical `NewCapabilityAuthorizer` emitter-arg drop only).
- **Roll-up honest:** `gate3_test.go` counts `DEFENDED` and `ACCEPTED-WINDOW` separately, prints `3 DEFENDED, 1 ACCEPTED-WINDOW`, and Vector #2's row reads `ACCEPTED-WINDOW | bounded; operational + Gateway enforcement (1.5.4)`. It is NOT silently counted as DEFENDED. Live output: `PHASE 1 GATE 3: PASSED (4/4 cleared — 3 DEFENDED, 1 ACCEPTED-WINDOW)`.

---

## 6. Health KV schema — PASS (code + completeness test), see F-1 for the doc

- `health.go` no longer emits `cap-staleness`.
- `internal/healthkv/completeness_test.go` correctly drops the `cap-staleness` line from its enumerated event-driven keys.
- No code consumer of `cap-staleness` remains (grep clean).
- The only remaining `cap-staleness` consumer is the observability doc — captured as **F-1 (P1)**.

---

## 7. Docs accuracy — PASS (component + contract docs); F-1 is the exception

- `docs/components/processor.md`: ingest/emit tables, step-3 row, error table, and auth-mode table all updated to the no-freshness-gate posture; `AuthFreshnessExceeded` replaced with `AuthContextMismatch`; `ephemeralGrants` expiry called out as preserved. Accurate.
- `docs/contracts/06-capability-kv.md`: `projectedAt` redefined as deterministic provenance (anchor `lastModifiedAt`), explicitly "not a freshness ceiling." Accurate.
- `docs/components/refractor.md`: `projectedAt`/`projectedFromRevisions` rows corrected; new "Capability-Lens health (operational backstop) — current state" subsection added.
- **§3.5 survey accuracy verified against `internal/refractor/health`:** the documented finding — Capability Lens flows through the generic per-lens path; **no Capability-Lens-specific liveness/lag signal and no alert/threshold** — is accurate. The only `capability` token in that package is a doc-comment example in `lattice_heartbeater.go`. Grep for `alert|threshold|exceed|breach` in `internal/refractor/health/*.go` (non-test) returns nothing. The four documented emitters (Reporter status, LagPoller consumerLag, LensLatency p95/p99, instance heartbeat, AuditWriter) match the actual files. The "known gap, do not fix here" framing is honest and matches §3.5's survey-only mandate.

---

## 8. Minor — F-2 (P2)

`internal/processor/latency_ring.go:14-16` — the `LatencyStats` doc comment says callers "may use that to skip emission for staleness (Decision #4)". Staleness emission no longer exists. Stale/misleading comment; no behavioral impact. Fix: drop the staleness clause.

---

## Gate checks run by reviewer
- `go build ./...` — clean.
- `go vet ./internal/processor/... ./internal/refractor/... ./internal/bypass/... ./internal/aiagent/...` — only pre-existing `cypher_parser.go` unreachable-code warnings (generated parser, unrelated).
- `go test ./internal/processor/... -run 'TestCapabilityAuthorizer|TestAuthTrace|TestDenialBuilder'` — `ok`.
- `go test ./internal/bypass -run 'TestCapAdv_V2|TestGate3_Report' -v` — PASS; Gate-3 4/4 (3 DEFENDED, 1 ACCEPTED-WINDOW).
