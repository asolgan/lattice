# Story 1.5.6 — Re-enable Hello Lattice M4–M6 + flip Gate 5 to a full pass

> **STATUS: SHIPPED AS A SPLIT (Andrew's call).** M4 + M6 re-enabled and passing;
> the capability-lens link fan-out (§7) is implemented and correct; the honesty
> marker keeps Gate 5 partial. **M5 remains DEFERRED** — its reprojection is
> starved by the events-publish atomic-publish storm (NATS err 10174). Story
> **1.5.8** makes step-9 event publish non-atomic, then re-enables M5 for a true
> Gate 5 full pass. See the "1.5.6 note" in `phase-1-progress.md`.

**Phase 1.5 hardening · Wave D (final story) · depends on 1.5.2, 1.5.5 (shipped).**
Author: Winston (CS). Implementer: DS sub-agent (does NOT commit). CR follows.

This is the Phase 1.5 capstone. When it ships, the Phase 2 readiness gate is met
(all 7 stories done, Gate 5 `passed:true`, conformance suite green, substrate-
direct install grep-clean).

---

## 1. Why this story exists

The Hello Lattice integration suite (`internal/hellolattice/hellolattice_test.go`,
`//go:build integration`, run in CI via `make test-hello-lattice` → ci.yml:64)
has three milestones skipped behind `t.Skip(...)`. Their blockers were all
architectural gaps that **later Phase 1.5 stories have since closed**. This story
re-enables them and flips the Gate 5 Health-KV marker from `passed:false /
partial` to a full `passed:true` (M1–M6).

### The three deferred milestones and why each is now unblocked
- **M5** `TestHelloLattice_Milestone5_AITraversal` — was deferred because the
  Processor DDL cache was stale w.r.t. substrate-direct package installs
  (`CreateUnclaimedIdentity` → `NoDDLForClass: vtx.meta.<identity>`).
  **FIXED by 1.5.5**: installs route through the write-path as `InstallPackage`
  ops and the DDL cache is invalidated in-commit. → un-skip + verify.
- **M6** `TestHelloLattice_Milestone6_RollbackBookDDL` — was deferred because the
  Processor DDL cache was not evicted when `TombstoneMetaVertex` committed (a
  later `CreateBook` was wrongly accepted). **FIXED by 1.5.2** (DDL tombstone
  coherence: `loadMetaVertex` returns absent when the root doc `IsDeleted`).
  → un-skip + verify.
- **M4** `TestHelloLattice_Milestone4_LensProjection` — was deferred because the
  Refractor Postgres adapter does not auto-manage the target table schema (the
  lens projection fires but every upsert returns `column "title" of relation
  "books" does not exist`). **This is BY DESIGN, not a gap to fill with
  auto-DDL** — see §2.

---

## 2. LOCKED design decision for M4 (settled with Andrew 2026-05-31 — do NOT relitigate)

**No auto-DDL. Postgres target schema is managed OUT OF BAND.** This is the
canonical Materializer design (confirmed in the Materializer repo brainstorming:
`_bmad-output/implementation-artifacts/2-5-schema-contract-enforcement.md` and
`3-3-structural-failure-detection-rule-pause.md`): a missing target table/column
(Postgres SQLSTATE `42P01` etc.) is a **Structural failure** → the projector
**pauses the rule** (`status:paused`, `pauseReason:structural`, `lastError`),
buffers safely in the durable consumer, and resumes via the control API after an
operator fixes the schema out of band. No DLQ flood, no auto-CREATE.

The Lattice in-repo Refractor **already ports this behavior**
(`internal/refractor/pipeline/pipeline.go` — "On structural failure: pauses until
ctx is cancelled OR Resume() is called (FR19a)", `resumeCh`, infra/manual/
structural pause). So M4 fails today **only because nothing provisions the
`books` table** — the projector correctly pauses, the row never lands, and the
test's Postgres poll times out.

**Therefore M4's fix is purely out-of-band provisioning in the test harness.**
Create the table before the lens projects:

```sql
CREATE TABLE IF NOT EXISTS books (book_id TEXT PRIMARY KEY, title TEXT)
```

These columns match the lens's cypher projection
(`MATCH (b:book) RETURN b.key AS book_id, b.title AS title`) and its
`key:["book_id"]`. **No change to `internal/refractor/adapter/postgres.go` or the
Refractor pipeline. No auto-DDL. No `outputSchema`/LensSpec extension.**

---

## 3. Work items (all in `internal/hellolattice/hellolattice_test.go` unless noted)

### A. Out-of-band schema provisioning (for M4)
1. In `TestMain` (after the NATS connect, before `m.Run()`), connect to Postgres
   using `POSTGRES_URL` (fall back to the same default M4 uses,
   `defaultPostgresURL`) and run `CREATE TABLE IF NOT EXISTS books
   (book_id TEXT PRIMARY KEY, title TEXT)`. This models a DBA/operator
   provisioning the projection target out of band. If the connect fails, log
   loudly (the PG-dependent milestones will then fail with a clear message) — do
   not silently swallow. Close the PG connection on teardown. Add a short comment
   citing out-of-band schema management (Materializer 2.5/3.3); NO auto-DDL in
   the Refractor.
   - Use the pgx pool already vendored (`github.com/jackc/pgx/v5`), consistent
     with `internal/refractor/adapter/postgres.go`.

### B. Un-skip the three milestones
2. M4 `TestHelloLattice_Milestone4_LensProjection`: remove the leading
   `t.Skip("Milestone 4 deferred ...")`. Leave the rest of the body intact (it
   builds the books lens via `CreateMetaVertex` and polls Postgres for the row).
3. M5 `TestHelloLattice_Milestone5_AITraversal`: remove the leading
   `t.Skip("Milestone 5 deferred ...")`. (The create payload already supplies
   `claimKeyHash` per 1.5.7 — confirm it still does after un-skipping.)
4. M6 `TestHelloLattice_Milestone6_RollbackBookDDL`: remove the leading
   `t.Skip("Milestone 6 deferred ...")`.
   - Keep the inner `t.Skip` guards that depend on shared state being set
     (e.g. `bookDDLKey not set — run Milestone2 first`) — those are correct
     ordering guards, not deferrals.

### C. Fix milestone ordering so the marker is written LAST
5. Go runs in-file tests in **source order** under `-p 1`. The marker writer
   `TestHelloLattice_WriteGate5Marker` currently sits at ~L653, **before** M6
   (~L696). With M6 un-skipped, the marker would be written before M6 runs.
   **Physically move `TestHelloLattice_WriteGate5Marker` to be the LAST test
   function in the file**, after `TestHelloLattice_Milestone6_RollbackBookDDL`,
   so the marker reflects all six milestones passing. Resulting order:
   M1 → M2 → M3 → M4 → M5 → M6 → WriteGate5Marker.
   - Sanity-check shared-state flow across the new order: M2 sets
     `bookDDLKey`/`bookDDLRevision`; M3 sets `bookVertexKey`; M4 sets
     `lensMetaKey`; M5 uses `lensMetaKey` for the agent-book Postgres assertion;
     M6 uses `bookDDLKey`/`bookDDLRevision` and tombstones the DDL. This order is
     consistent (M6's tombstone happens after everything that needs the DDL).

### D. Flip the Gate 5 marker to a full pass
6. In `TestHelloLattice_WriteGate5Marker`, change the marker payload to:
   - `"passed": true`
   - `"milestonesPassed": []int{1, 2, 3, 4, 5, 6}`
   - REMOVE `"partial"`, `"milestonesDeferred"`, and `"deferredReason"`.
   - Keep `completedAt`, `commit` (GITHUB_SHA), and the elapsed-time log.
   - The marker is `health.gates.phase1.gate5`; consumers read `passed`.

### E. Comments / docs
7. Remove the now-obsolete deferral rationale text from the three milestones'
   skip messages (deleted with the `t.Skip`). Do NOT leave history comments
   (`// was deferred`, `// Story 1.5.x`) — git blame is the record.
8. The file header (L6–L11) already lists all six milestones — verify it still
   reads correctly (it says "five milestones" in one place — `make test-hello-
   lattice` help text and any "five" references should become "six").
   Check `Makefile` L163 ("all five milestones pass") and ci.yml:61 comment.
9. Docs: update `_bmad-output/implementation-artifacts/phase-1-progress.md`
   (Gate 5 status → full pass; mark 1.5.6 shipped) — **Winston edits the progress
   tracker, not DS.** DS may update any `/docs` Gate-5 or hello-lattice notes if
   present, and the hello-lattice package README if it states M4–M6 are deferred.
   DS must NOT edit `_bmad-output/planning-artifacts/*`.

---

## 4. Non-goals / out of scope
- **No auto-DDL** in the Refractor Postgres adapter (locked, §2).
- No `outputSchema`/LensSpec schema-declaration work (Phase 2 territory).
- The events-stream `atomic publish is disabled` flake (NATS err_code 10174) is a
  SEPARATE follow-up already flagged — do NOT fix it here. Just be aware it can
  add step-9 redelivery noise under the 30m integration run; the suite tolerates
  it (Refractor projects from Core KV CDC, not the events stream).
- Refractor `failure.Classify` coverage of `42703` (undefined_column) vs only
  `42P01` (undefined_table) — a possible robustness follow-up, NOT this story
  (the out-of-band table has the right columns, so it does not arise on the happy
  path).
- F-004 package version upgrade; UninstallPackage per-key OCC CAR — unrelated.

## 5. Gates (all must pass before Winston commits)
- `go build ./...`, `make vet`, `golangci-lint run ./...`
- The file must compile under BOTH default and `-tags integration`
  (`go vet -tags=integration ./internal/hellolattice/...`).
- **`make test-hello-lattice`** on a clean stack (`make down && make up`) — all
  SIX milestones pass and the marker is written with `passed:true`,
  `milestonesPassed:[1,2,3,4,5,6]`. This is the authoritative gate.
- Re-run the standard suite to confirm no regression: `make verify-kernel`,
  `make verify-package-{rbac,identity,identity-hygiene}`, `make verify-conformance`,
  `make test-bypass`, `make test-capability-adversarial`.
- Deviation 14: if shared-NATS flakes appear, `make down && make up`, re-run; CI
  on the clean stack is authoritative. The events-publish flake (§4) may require a
  CI `gh run rerun --failed`.

## 6. Workflow constraints (binding)
- DS sub-agent implements but **does NOT commit or push**. Winston drift-reviews,
  runs CR, adjudicates, commits when green, watches CI.
- Sub-agents **never** edit `_bmad-output/planning-artifacts/*` (Winston-only).
- No history comments in code. New docs land in `/docs`, not `_bmad-output/`.

---

## 7. ADDENDUM (2026-05-31, Andrew: BUNDLE) — M5 capability-lens link fan-out

Re-enabling M5 surfaced a SECOND, real blocker beyond the items above. M5 steps
1–4 already pass (1.5.5 fixed `NoDDLForClass`; Winston added the missing RBAC
`ContextHint.Reads` for GrantPermission `[permKey, roleKey]` and AssignRole
`[actorKey, roleKey]`, matching `packages/rbac-domain/integration_test.go`). M5
step 5 fails: the agent's capability doc projects but never gains `CreateBook`.

### Root cause (confirmed)
`internal/refractor/pipeline/pipeline.go` `processMsg` ACK-and-SKIPs every link
and aspect CDC event ("…mutation observed but no handler registered"); only
`KindVertex` events reach `evaluateForEntry` (where the cross-vertex fan-out
lives, evaluate.go:66). But `GrantPermission`/`AssignRole` are **pure link
mutations** (`grantedBy`, `holdsRole`) with **no vertex change**, so the fan-out
never fires and role-holders are never reprojected. The installed
`ActorEnumerator` (cmd/refractor/main.go:259) only triggers on a non-actor
**vertex** event. The agent was projected once at identity creation (before it
had any role) and nothing reprojects it when its topology changes.

### Supporting facts (already verified — don't re-investigate)
- Adjacency (`refractor-adjacency` KV) IS maintained from link CDC by a separate
  consumer: `internal/refractor/consumer/bootstrap.go:157` → `adjacency.Build`.
- The full engine resolves `holdsRole`/`grantedBy` edges via that adjacency index
  (`executeFullForActor` passes `p.adjKV, p.coreKV`).
- `ActorEnumerator.Enumerate(eventVertexKey, eventVertexType)` does undirected
  adjacency BFS from a **vertex** seed and returns affected actor keys; for an
  actor-typed seed it returns the seed as a singleton.
- `evaluateFanOut` already reprojects a slice of actors (fetch props → 
  `executeFullForActor` per actor; missing actor → emit cap Delete).

### Fix (bundled into 1.5.6)
When an `ActorEnumerator` is installed on the pipeline, **link CDC events must
drive a fan-out reprojection instead of being skipped**:

1. **Dispatch (`processMsg`)**: for `KindLink`, when `p.actorEnumerator != nil`,
   route to a new link-fan-out path instead of ack-and-skip. Other lenses (no
   enumerator) keep the current skip behavior. Also handle **link tombstones**:
   the empty-body short-circuit (msg.Data()==0) currently skips ALL deletes
   before classification — restructure so a link tombstone under a capability
   lens still extracts its key (from the subject) and fans out (revocation must
   shrink cap docs). Vertex/aspect tombstones keep current behavior.
2. **Endpoint seeding**: parse the link key
   `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>` into its two endpoint vertex keys
   `vtx.<typeA>.<idA>` and `vtx.<typeB>.<idB>`. Enumerate affected actors from
   BOTH endpoints (union); reproject each (reuse the `evaluateFanOut` per-actor
   machinery — factor a shared helper so vertex-fan-out and link-fan-out share
   the reprojection loop).
3. **Adjacency consistency (the subtle part)**: the per-lens pipeline and the
   dedicated adjacency consumer both react to the same link event with no cross-
   consumer ordering. Before reprojecting, the pipeline must ensure adjKV
   reflects this link so the reprojection cypher sees a consistent edge set.
   Recommended: idempotently apply the link to adjKV via the existing
   `adjacency.Build` (it upserts; the dedicated consumer's later Build for the
   same edge is a no-op) — for a create, add the edge; for a tombstone, remove
   it — THEN enumerate + reproject. Confirm `adjacency.Build`'s event shape
   (`CoreKVEvent`) can be constructed from the link key + isDeleted; mirror how
   `consumer/bootstrap.go` builds it. If a cleaner mechanism exists, use it, but
   the reprojection MUST NOT race ahead of the edge it was triggered by.
4. **Caps/safety**: reuse the enumerator's existing depth/actor-set caps. A link
   whose endpoints reach no actors (e.g. a book→author link) enumerates empty →
   no-op (correct).

### Acceptance
- M5 passes end-to-end on a clean stack + faithful CI preamble (see §5 note
  below): the agent's `cap.identity.<id>` doc gains `CreateBook` within the
  NFR-P3 500ms window after AssignRole, and the cold-start traversal succeeds.
- A link-fan-out reprojects on **revocation** too: add focused pipeline tests
  (Go, in `internal/refractor/pipeline/`) for (a) holdsRole create → actor cap
  gains the role's permissions; (b) grantedBy create → all role-holders gain the
  permission; (c) holdsRole/grantedBy tombstone → affected actors lose it. Follow
  the existing capability E2E test patterns
  (`internal/refractor/refractor_capability_*_e2e_test.go`).
- No regression in the existing capability E2E suites or the vertex fan-out path.

### IMPORTANT — faithful local verification of the integration suite
`make test-hello-lattice` depends on the CI preamble. Reproduce CI EXACTLY (the
`make test-bypass`/`make test-capability-adversarial` targets do `make down && up`
and WIPE the installed packages — do NOT use them):
```
make down && make up
make verify-package-rbac && make verify-package-identity && make verify-package-identity-hygiene
go test ./internal/bypass/... -run "TestBypass|TestGate2" -count=1
go test ./internal/bypass/... -run TestCapAdv -count=1
go test ./internal/bypass/... -run TestGate3_Report -count=1
make test-hello-lattice          # all six milestones + marker passed:true
```
The Refractor runs as a background host process from `make up` (`bin/refractor`,
logs to `refractor.log`) — not a container; check `refractor.log` if projections
stall. Note the recurring non-fatal `atomic publish is disabled` step-9 events
flake (separate follow-up) — tolerate it.

### Scope notes
- The marker (§3.D) already flips to `passed:true` / `[1,2,3,4,5,6]`; with M5
  fixed, the honesty sentinels make all six true and the suite certifies a real
  full pass. Do NOT add a "deferred" path — we are fixing M5, not deferring it.
- Keep the two M5 RBAC `ContextHint` fixes and the M6 `ErrCompensationAspectMissing`
  assertion and the M4 out-of-band table — all verified correct, already in the
  working tree.
