---
title: Refractor Gap Analysis (Epic 2 Closing Artifact)
story: 2.2
date: 2026-05-14
status: DRAFT for Winston + Andrew review
inputs:
  - _bmad-output/planning-artifacts/MORPH-DEVIATIONS.md (15 entries)
  - cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md (3 requests, all resolved)
  - _bmad-output/planning-artifacts/data-contracts.md Contract #6
  - _bmad-output/planning-artifacts/epics.md Stories 3.1–3.7
  - internal/refractor/ (package-level docs + e2e perf)
---

# Refractor Gap Analysis (Epic 2 Closing Artifact)

This is the exit artifact for Epic 2. It compares the morphed Refractor's
**actual capabilities today** against (a) what Epic 3 needs to start, and
(b) what Phase 2+ needs to ship. Every Epic 3 story has an unambiguous
prerequisite reference in Appendix A.

The Refractor today is a faithful morph of the Materializer codebase with
the Lattice integration seams cut at `cmd/refractor` plus four new files
(`lens/corekv_source.go`, `lens/bootstrap.go`, `health/lattice_heartbeater.go`,
`control/capability.go`). The inner packages (`adapter`, `adjacency`,
`consumer`, `engine`, `failure`, `health`, `pipeline`) retain their
Materializer shape and consume raw `nats-io/nats.go` / `jetstream`
handles. This is the dominant texture to keep in mind when reading the
GAP/PARTIAL/READY calls below.

---

## 1. Capabilities As-Shipped

### 1.1 Rule / Expression Language (Parser)

`internal/refractor/engine/parser.go` is a **hand-rolled** v1 parser:
- Supports: `MATCH`, `OPTIONAL MATCH`, `RETURN`, `AS` aliases,
  parenthesized node patterns `(:Label)`, bracketed edge patterns
  `[:rel]`, directional arrows `->` / `<-`, dot-property access on
  identifiers, comma-separated returns.
- **Does NOT support:** `WHERE` clauses, variable-length path quantifiers
  (`*`, `*1..n`), map literals in `RETURN`, `WITH` clauses,
  aggregations (`collect()`, `count()`, `DISTINCT`), list expressions,
  function calls.
- ANTLR4 toolchain dependency was deliberately dropped (Deviation 3).
  Replacement under Story 3.1 will be `github.com/jtejido/go-opencypher`
  with a Refractor-native visitor translating ANTLR's parse tree into
  Refractor's existing AST.
- The parser is a pure function (no I/O), and its tests run with no
  infrastructure (`internal/refractor/engine/parser_test.go`).

### 1.2 Target Adapters

Two adapters implement `adapter.Adapter` (`Upsert`, `Delete`, `Probe`,
`Close`):

- **`adapter.NatsKVAdapter`** (`adapter/natskv.go`) — writes to a NATS KV
  bucket. Constructed with a `jetstream.KeyValue` handle and an ordered
  `keyOrder` slice; composite keys are built by joining values with `/`.
  Marshals the projected row as a JSON document. No partial-update / merge
  semantics; each `Upsert` is a full document replace.
- **`adapter.PostgresAdapter`** (`adapter/postgres.go`) — writes to a
  Postgres table via `pgxpool.Pool` (shared per DSN by `PoolManager`,
  ADR-9). Generates `INSERT ... ON CONFLICT ... DO UPDATE` and `DELETE`
  SQL. Query timeout configurable per adapter.

An optional `adapter.Truncater` interface (FR29 rebuild) exists; both
adapters can be inspected to confirm support.

There is **no NATS-subject (publish-events) adapter, no Elasticsearch
adapter, and no Postgres-RLS-aware write path** — all called out in
§3 below.

### 1.3 Lens Lifecycle Operations

`internal/refractor/lens/` provides activate / deactivate / update flows:
- **Source of definitions:** `lens.CoreKVSource` (`corekv_source.go`)
  watches `vtx.meta.>` on Core KV, parses the envelope `class` field,
  and routes only `meta.lens`-class records to the loader. Aspects
  arriving before the parent vertex's class is observable are briefly
  buffered and replayed (CDC ordering is not guaranteed).
- **Bootstrap:** when `REFRACTOR_BOOTSTRAP_LENS` is set and no
  `meta.lens` records exist, `lens.BootstrapLens()` activates a fixed
  development-mode lens with NanoID `RfxBootstrap12345678`
  (`bootstrap.go`).
- **Translator:** for Story 2.1, the translator reads a **single
  `spec` aspect** carrying all five lens fields (Deviation 11). Per-aspect
  versioning of `outputSchema`, `cypherRule`, etc. is deferred.
- **Re-materialization on modify:** preserved from Materializer. A lens
  update tears down the old durable consumer and reprovisions a new one;
  the rebuild flow truncates the target via `Truncater` if requested
  (`pipeline.Rebuild`).
- **Legacy `Loader.Start` (JetStream `MATERIALIZER_RULES` consumer)** is
  retained as dead code so the legacy `loader_test.go` suite keeps
  passing under the "import-path updates only" rule (Deviation 10). It
  is never invoked by `cmd/refractor`.

### 1.4 Control Surface

`internal/refractor/control/service.go`:
- One subject: `refractor.control` (renamed from `materializer.control`;
  NATS Services framework migration deferred — Deviation 6).
- Six operations (string-matched on the inbound payload's `op` field):
  `health`, `validate`, `rebuild`, `pause`, `resume`, `delete`.
- Queue subscription with queue group `materializer-control` (group name
  not renamed; not load-bearing).
- Authorization is `control.CapabilityChecker`; default implementation is
  `StubCapabilityChecker` which logs and allows every call. Real
  Capability KV integration is Epic 3.
- Registry-based dispatch: pipelines register themselves as
  `Resumer`/`Pauser`/`Rebuilder`/`Deleter`/`Reporter` when they start
  and unregister on stop.

### 1.5 Observability Surface (Health KV + Audit + Metrics)

- **Per-lens Health KV entry** (`health.Reporter`, `health/reporter.go`):
  one entry per lens at `<healthKVBucket>/<ruleID>` with fields
  `ruleId, team, status, pauseReason, activeSequence, consumerLag,
  errorCount, lastError, lastUpdated` — status values `active`,
  `paused`, `rebuilding`; pause reasons `infra`, `structural`, `manual`.
- **Per-instance Refractor heartbeat** (`health/lattice_heartbeater.go`):
  writes `health.refractor.<instance>` per Contract #5 §5.2 at NFR-O1's
  10-second floor. Carries `component, instance, version, status,
  heartbeatAt, startedAt, uptime, metrics, issues` and (via
  `LagProvider`) per-lens `stream_last_seq - consumer_acked_seq` lag.
- **Per-lens lag metric** (`health/lag_poller.go`): publishes
  `materializer.metrics.<ruleId>` every `MetricsInterval` (default 5s)
  with `ruleId, team, consumerLag, timestamp` and refreshes the health
  KV `consumerLag` field as a side effect.
- **Per-write audit append** (`health/audit_writer.go`): on every
  successful adapter write, an `AuditEntry`
  (`entityId, operation, outputRowHash, timestamp`) is appended to a
  per-lens JetStream audit stream subject
  `materializer.audit.<ruleId>` (LimitsPolicy, 7-day MaxAge).
- **No FR21-compliant alert flags / structured denial events emitted yet
  by Refractor itself** — alert authorship for security signals is
  Processor-side at MVP (per `health.alerts.security.*` references in
  Story 3.5).
- **Subject prefixes still contain `materializer.`** for metrics, audit,
  and DLQ (Deviations 4 + 6) — pending cleanup pass.

### 1.6 Failure Tier Handling

`internal/refractor/failure/classify.go` defines four routing tiers and
their explicit constructors `Transient`, `Terminal`, `Infra`,
`Structural`. The pipeline routes each by tier:
- **Transient** → `Nak` with exponential backoff (`failure/retry.go`).
- **Terminal** → write to per-lens DLQ via `failure/dlq.go`; `Ack` the
  source message; increment `errorCount` in Health KV.
- **Infra** → pause the lens with `pauseReason: infra`; start a probe
  loop (`pipeline.ProbeInterval`, 10s default) that re-tests the target
  via `adapter.Probe`; resume on first success (FR16, FR17).
- **Structural** → pause with `pauseReason: structural`; no probe; lens
  stays paused until operator intervention via control endpoint (FR19a,
  NFR3).

Empirical anchor: the Story 2.1b AC #10 end-to-end test
(`refractor_e2e_test.go`) measured **p99 = 10.3ms vs the 500ms NFR-P3
budget — ~46× headroom** on a single-lens single-Postgres-target
workload. This is the only published empirical perf measurement on
morphed Refractor. It is sufficient to clear NFR-P3 for the trivial
case; it does NOT generalize to Capability KV write load (multiple
lenses, large fan-out per CDC event) until tested explicitly under
Story 3.2.

---

## 2. Required for Epic 3 (Authorization & Security Perimeter)

### 2.1 openCypher Parser Integration — **GAP** (epic-sized)

Refractor's `engine/parser.go` cannot parse the bootstrap-seeded
Capability Lens cypher query: it requires `WHERE` clauses, `OPTIONAL
MATCH` (supported), variable-length path quantifiers `*` (not
supported), map literals in `RETURN` (not supported), `WITH` clauses
(not supported), `collect()` aggregation (not supported), list
concatenation (not supported), inbound traversal syntax `<-[:rel]-`
(syntactically supported but only as part of a node-pattern walk, not
as standalone reverse-direction traversal in path patterns).

This is exactly Story 3.1's scope: introduce a `full` engine alongside
the existing `simple` engine, both behind a common `RuleEngine`
interface. Open-source library `github.com/jtejido/go-opencypher` is
the chosen path (ANTLR4-generated; Refractor writes its own visitor).
ANTLR4 module dependency was deliberately dropped from go.mod in 2.1
(Deviation 3); Story 3.1 reintroduces it scoped to the new `full`
package.

### 2.2 Capability Lens Cypher Rule Semantics — **GAP** (epic-sized)

The four traversal patterns Story 3.2 requires are all unimplemented
in `simple`:
- **Role-based:** `assignedRole → role → grantsPermission → permission`
  — straightforward MATCH chain, ports to `simple` IF `WITH`/`collect()`
  land; impossible otherwise.
- **Task-derived (FR56):** task assignment plus manager-via-reporting-
  chain delegation. Requires variable-length `reportsTo*` traversal.
- **Manager-via-reporting-chain:** variable-length path. **Hard
  requirement on `full` engine.**
- **Service-access topology:** `identity → containedIn* → location →
  availableAt → service` with multi-level `unavailableAt` exclusion
  (Contract #6 §6.7). Variable-length path **and** exclusion semantics —
  requires `WHERE NOT EXISTS` or anti-pattern matching. Definitely
  `full`-engine.

All four are blocked behind §2.1.

### 2.3 Capability KV Target Adapter — **PARTIAL** (story-sized extension)

The existing `adapter.NatsKVAdapter` writes JSON documents to a NATS KV
bucket with composite keys. Contract #6 §6.1's primary key pattern
`cap.<actor-vertex-key-suffix>` and §6.2's three-section flat document
(`platformPermissions[]`, `serviceAccess[]`, `ephemeralGrants[]`) fit
the existing adapter's data model — a single document per key.

What is missing or unverified:
1. **`projectedAt` + `projectedFromRevisions`** are envelope fields the
   adapter has to populate from the cypher engine's execution context;
   the current adapter does not have access to source-revision metadata.
   The cypher engine layer needs to thread this through (Story 3.2).
2. **Secondary key space** `cap.role-by-operation.<operationType>`
   produced by a *second* Lens (`vtx.meta.lens.capabilityRoleIndex`)
   targeting the same KV bucket — this is the Lattice "one Lens = one
   RETURN" pattern (data-contracts §6.1 architectural note). The
   adapter handles this transparently (each Lens has its own adapter
   instance) but operators must understand that two Lenses share one
   bucket with disjoint prefixes.
3. **Sole-writer enforcement** (Contract #6 §6.1: Refractor is sole
   writer; Processor reads only) is currently a *convention*. NATS
   account-level write restriction on the capability bucket is a Phase
   2+ operational hardening item (deployment concern, not adapter
   concern).
4. **Document-envelope reshape** to align adapter outputs with
   Contract #1's envelope shape was deferred in 2.1 (open carry from
   2.1's Deliverable #12). This affects the `projectedAt` / class /
   revision fields. Story 3.2 will either land it or formally accept
   the document-only shape for Capability KV.

Scope to close: extending the engine→adapter interface to carry
projection metadata is a localized story-sized change.

### 2.4 Read-After-Write Coherence for Capability KV — **PARTIAL** (with caveat)

The Processor's commit step 3 reads `cap.<actor>` from Capability KV
with O(1) latency. NFR-P3 mandates < 500ms p99 end-to-end
CDC-to-projection lag. The 2.1b e2e measurement (§1.6: p99 = 10.3ms,
46× headroom) is *direct evidence* the projection commit pattern is
fast enough — **but it was measured on a one-lens, one-Postgres-target,
one-mutation workload with no per-event fan-out and no graph
traversal.**

The Capability Lens fan-out profile is fundamentally different:
- A single mutation on a role-permission link can affect every
  identity that has that role — a fan-out of O(n_actors_with_role).
- A single mutation on a containment link can affect every actor
  contained at or below that vertex — variable-length traversal
  amplifies per-event cost.

For Story 3.3's hot-path authorization read, the bound that matters is
**how stale can Capability KV be when Processor reads it.** Phase 1
treats the answer as "< 500ms p99 under load TBD" and Story 3.2 is
chartered to measure this on the real Capability Lens workload. Story
3.3 also wires the `AuthFreshnessExceeded` hard-ceiling (5× NFR-P3)
against the entry's `projectedAt` so a regression here is observable.

Calibration plan: Story 3.2's NFR-P3 conformance assertion runs the
bootstrap cypher query against a representative seeded graph; if p95
exceeds budget the gap analysis is amended with the specific
bottleneck. This document records the *currently-measured* state, not
the expected scale-out state.

### 2.5 Pipeline Key-Shape Adaptation — **GAP** (story-sized, blocking)

Hidden in the inventory but uncovered during 2.1b's e2e authoring:
`internal/refractor/pipeline/pipeline.go` still calls
`parseCoreKVKey(key)` which recognizes **only** the legacy Materializer
shape `node_<label>_<id>`. All Contract-correct Lattice keys
(`vtx.<type>.<id>`, `vtx.<type>.<id>.<localName>`, `lnk.<...>`) are
returned as "unrecognized" and skipped (Deviation 13). The morphed
lens-source code path (`corekv_source.go`) handles `vtx.meta.<id>`
correctly because it does its own classification; the **projection
pipeline does not.**

The 2.1b e2e test honors this constraint by writing legacy-shape keys
to Core KV — which is why the test could clear NFR-P3 while the
pipeline cannot project any real Lattice domain entity. **This blocks
projecting any domain vertex beyond meta-lenses end-to-end.** Refactor
`parseCoreKVKey` → `substrate.ClassifyKey` + `substrate.ParseVertexKey`
and rewrite ~12 pipeline test fixtures.

This is spotlit in §4 (Deviation 13) and §5 (risk register) because
its downstream blast radius is larger than the single-package change
suggests. **Story 3.1 or 3.2 cannot meaningfully exercise the
Capability Lens against a real seeded graph until §2.5 closes.**

---

## 3. Required for Phase 2+

### 3.1 Historical State Query Support (FR51) — **GAP**

Substrate (immutable `core-operations` stream, NFR-R5) exists. No
operator-facing replay-into-temporary-Lens machinery exists in
Refractor. Phase 2+ work; epic-sized.

### 3.2 Read-Path Authorization for Lens Targets — **GAP**

Direct reads from Postgres-backed Lens targets bypass the Capability
Lens write-path boundary (NFR-S2). Phase 1 assumes trusted operator
readers only. Phase 2 needs either Postgres RLS (driven by actor
identity) or a Gateway read-proxy pattern. Architectural decision
deferred to Phase 2 sprint. Multi-epic.

### 3.3 Elasticsearch Target Adapter — **GAP**

Two adapters exist (NATS KV, Postgres). ES not implemented; would
extend `adapter.Adapter` plus a new package. Story-sized but blocked on
Phase 2 read-surface decisions.

### 3.4 NATS Streams (Publish-Events) Target Adapter — **GAP**

Personal Lens fan-out (morph plan §2.1) requires a publish-events
adapter (Lens output → NATS subjects, not a KV store). Currently
unimplemented. Story-sized.

### 3.5 Multi-Cell Scale-Out Adjustments — **GAP**

Single-cell MVP per Lattice's architectural decision #8. No cell-aware
routing in `cmd/refractor`. Phase 2+ — epic-sized.

### 3.6 Personal Lens / Secure Lens / Crypto-Shred Listener / Path
Projection (Postgres RLS) — **GAP** (all)

Morph plan Phase 5 items, explicitly deferred from 2.1
(Deviation 7). Each is a separate Phase 2+ scope chunk.

### 3.7 Substrate Inner-Package Migration — **PARTIAL**

`internal/refractor/` inner packages (`adapter`, `adjacency`, `consumer`,
`health` partly, `pipeline`, `lens/loader.go`) still consume raw
`nats-io/nats.go` and `jetstream` handles. 30 files (15 prod + 15 test)
to migrate; substrate's KV API needs Watch / UpdatesOnly / NumPending
helpers added before this can proceed (Deviation 5). Cross-cutting; if
Phase 2 adds centralized observability hooks via substrate, this
becomes blocking.

### 3.8 NATS Services Framework Migration (Control Plane) — **GAP**

Refractor's control service still uses `QueueSubscribe` on
`refractor.control` rather than `micro.AddService`-based endpoints on
`lattice.ctrl.refractor.<lensId>.<op>` (Deviation 6). Operational-
polish item. Morph plan Phase 6.

### 3.9 Adapter Document-Envelope Reshape — **GAP** (open carry from 2.1)

Adapter `Upsert` signature accepts `keys` + `row` maps. Aligning
adapter outputs with Contract #1's envelope shape (so adapters emit
`{key, class, revision, ...payload}`-shaped documents) requires
changing the adapter interface and every caller. Story-sized.

### 3.10 Full `Rule → Lens` Rename + `ruleId → lensId` JSON Migration — **PARTIAL**

Package was renamed `rule → lens`; the Go type `Rule` was NOT renamed
inside the package (Deviation 11a) due to a BSD-sed limitation in 2.1.
Health/audit JSON documents retain `ruleId` for backward compatibility.
Cleanup pass needed; subject patterns like `lattice.dlq..<lensId>`
(double-dot, from empty `team` segment — Deviation 4) should be
addressed in the same pass.

### 3.11 Shared-Fixture Helper for Tests (revert `-p 1`) — **PARTIAL**

`go test ./...` requires `-p 1` (Deviation 14) because embedded NATS
servers in many packages concurrently exhaust file descriptors.
Resolution path: one embedded NATS per test binary at TestMain reused
across `t.Run` subtests. Future Phase 2 operability improvement; not
blocking any feature.

### 3.12 Cleanup of Dead `Loader.Start` JetStream Path — **PARTIAL**

`lens.Loader.Start` (the JetStream-backed `MATERIALIZER_RULES`
consumer) is preserved as dead code (Deviation 10) to keep
`loader_test.go` green. Delete once Story 2.3 or the first Epic 3 story
stabilizes the Core KV watch path with equivalent test coverage.

### 3.13 Replace Legacy Pipeline Key Parser — **GAP** (cross-listed with §2.5)

This is also a Phase 2 production-readiness blocker (Deviation 13).
Listed here for the deferred-items section of epics.md so it appears
in both Epic 3 prerequisites and the long-running backlog.

---

## 4. Deviations from Morph Plan

All 15 entries from `MORPH-DEVIATIONS.md` reformatted with status. Each
references the source-of-truth file and section. RESOLVED = closed in
2.1b. OPEN = open work item. OPEN-deferred = explicitly deferred to a
later epic.

| # | Title | Morph Plan §  | Status | Section Cross-Reference |
|---|---|---|---|---|
| 1 | Binary name `refractor`, not `lattice-refractor` | §6 | **RESOLVED** (planning artifact) | Cleared by amendment Request 1 |
| 2 | `asp.*` prefix non-existent; aspects classified by key shape + class | §3.1 | **RESOLVED** (substrate.ClassifyKey) | Cleared by amendment Request 2 |
| 3 | ANTLR4 dependency dropped | §5 | **OPEN-deferred** (Story 3.1) | §2.1 — GAP |
| 4 | `team` field vestigial (empty string) | §5 | **OPEN** (cleanup pass) | §3.10 |
| 5 | Substrate-based NATS access — boundary only | "MANDATORY OPERATING RULES" | **PARTIAL** (boundary set; deep refactor deferred) | §3.7 — PARTIAL |
| 6 | NATS Services framework migration deferred | §3.3 | **OPEN-deferred** (Phase 6 morph) | §3.8 — GAP |
| 7 | Crypto-shred / Personal / Secure / Path Projection out of scope | §2.1, §2.2, §2.4, §2.5 | **OPEN-deferred** (Phase 5 morph) | §3.6 — GAP |
| 8 | `testdata/` moved up one level | n/a | **RESOLVED** (acceptable layout) | — |
| 9 | `cmd/refractor-stub/` deleted | §5 (analog) | **RESOLVED** | — |
| 10 | Lens source via adapter approach (Core KV watch) | §2.3 | **RESOLVED** (with dead-code carryover) | §3.12 |
| 11a | Go type `Rule` NOT renamed to `Lens` | §6 | **OPEN** (cleanup pass) | §3.10 |
| 11 | Lens spec simplified to single `spec` aspect | §2.3 | **PARTIAL** (operational choice; revisit Phase 2) | §1.3 |
| 12 | Lens key shape `vtx.meta.<NanoID>` + class `meta.lens` | brief Decision #5 | **RESOLVED** (per data-contracts §1.2 line 70) | Cleared by amendment Request 3 |
| 13 | **Pipeline still parses legacy `node_<label>_<id>` keys** | §3.1, AC #2 | **OPEN — highest-priority carry** | §2.5 + §3.13 + §5 (risk) |
| 14 | `go test ./... -p 1` required | n/a | **OPEN** (operability) | §3.11 |
| 15 | `cmd/bootstrap` writes `health.bootstrap.complete` | §5 (interaction w/ Dev 9) | **RESOLVED** | — |

**Spotlight — Deviation 13.** Of the 15 deviations, Deviation 13 is the
only one that can block an Epic 3 story from succeeding end-to-end: the
pipeline currently cannot project any Lattice-shaped vertex. Story 3.2's
"activate Capability Lens against a representative seeded graph" cannot
demonstrate NFR-P3 against domain entities until 13 closes. Three other
deviations (3, 5, 6) are properly deferred to defined later work, and
the remaining eleven are either resolved or pure cleanup.

---

## 5. Risk Register

Substantive risks observed during the inventory. Each names the file
or behavior involved. **No fixes attempted in 2.2** — every entry is a
candidate for a hardening story.

### R1 — Pipeline Legacy Key Parser Blocks Domain Projection
**Where:** `internal/refractor/pipeline/pipeline.go:577, 1026, 1086`
**Issue:** `parseCoreKVKey` only recognizes `node_<label>_<id>`. Every
Contract-correct `vtx.*` / `lnk.*` key is "unrecognized" and skipped.
**Blast radius:** All Epic 3 projection work; Phase 2 production
readiness gate.
**Mitigation:** Replace with `substrate.ClassifyKey` +
`substrate.ParseVertexKey`; rewrite the ~12 pipeline test fixtures.
**Owner candidate:** Story 3.1 or 3.2 prep work (or pre-Epic-3
hardening Story 2.3).

### R2 — Capability KV Fan-Out Latency is Unmeasured
**Where:** §2.4 above; empirical evidence is single-lens single-target.
**Issue:** A role-permission link mutation can fan out to every actor
holding the role. Variable-length containment traversals amplify per-
event work. The 46× NFR-P3 headroom from 2.1b does NOT generalize.
**Mitigation:** Story 3.2 NFR-P3 conformance assertion is the
measurement vehicle; build it deliberately under representative graph
load before declaring Epic 3 done.

### R3 — Adjacency KV CAS-with-Retry Consistency Under Crash
**Where:** `internal/refractor/adjacency/builder.go` (CAS-with-retry
loop in `Build`)
**Issue:** Adjacency KV is mutated outside the Processor's atomic
commit boundary — it is a Refractor-side projection. If Refractor
crashes between processing a CDC link-create event and writing
`adj.<NodeID>`, the link is in Core KV but absent from Adjacency KV
until consumer redelivery. The consumer redelivery semantics (Story
2.1 retained) should re-emit on restart but this has never been
explicitly tested.
**Mitigation:** Add a crash-recovery test under `adjacency/`. If gaps
discovered, the rebuild path (`pipeline.Rebuild`) is the operator
recovery handle.

### R4 — Concurrent Lens Modification
**Where:** `internal/refractor/lens/corekv_source.go` (buffered-aspect
replay) + `internal/refractor/control/service.go` (registry mutations)
**Issue:** Two simultaneous control-plane operations on the same lens
(e.g., `rebuild` and `delete`) race on the registry maps. The registry
uses `sync.RWMutex` per access but the *operation lifecycles* are not
serialized — a `delete` mid-rebuild can unregister the rebuilder before
the rebuild goroutine completes. No observed failure, but no test
asserts the ordering. Similarly, a lens update arriving on the
CoreKVSource while a previous activation is still buffering aspects
can interleave.
**Mitigation:** Per-lens lifecycle mutex in `control.Service` would
serialize operations on one lens; consider for the Story 3.x lifecycle
hardening pass.

### R5 — Target Adapter Failure Cascade Across Shared Postgres Pool
**Where:** `internal/refractor/adapter/pool.go`
**Issue:** `PoolManager` shares one `pgxpool.Pool` across all lenses
targeting the same DSN. A misbehaving lens that exhausts the pool (long
queries, slow transactions) starves all other lenses' writes.
`PostgresAdapter.queryTimeout` bounds per-op time, but the pool itself
has no per-lens fairness.
**Mitigation:** Per-lens pool partition or pgx-level statement timeout
configuration; track as a Phase 2 scale concern.

### R6 — Control Plane Auth is Stub
**Where:** `internal/refractor/control/capability.go`
**Issue:** `StubCapabilityChecker.Authorize` logs and allows every
control call. Any actor reachable on the control subject can pause,
rebuild, or **delete** any lens. Default-allow in stub mode is
documented but operationally dangerous if shipped beyond MVP.
**Mitigation:** Story 3.x replaces with the real Capability KV
checker; until then, NATS account-level subject ACL on
`refractor.control` is the deployment-time mitigation.

### R7 — Subject Namespace Inconsistency (`materializer.*` prefix)
**Where:** `internal/refractor/subjects/` (DLQ, metrics, audit)
**Issue:** Three subject namespaces — `materializer.dlq.<ruleId>`,
`materializer.metrics.<ruleId>`, `materializer.audit.<ruleId>` — still
use the legacy prefix. Operator dashboards or future federation across
multiple Refractor instances will need namespace discipline.
**Mitigation:** Rename in the cleanup pass that closes Deviations 4 +
11a (§3.10).

### R8 — Dead Code Path in `lens.Loader.Start` Could Be Reactivated
**Where:** `internal/refractor/lens/loader.go` (JetStream
`MATERIALIZER_RULES` consumer)
**Issue:** The JetStream rule-source path is preserved (Deviation 10)
solely for `loader_test.go` parity. A future refactor could accidentally
re-enable both sources and produce duplicate lens activations.
**Mitigation:** Delete once Core KV watch path has equivalent test
coverage (§3.12).

---

## Appendix A: Epic 3 Story Prerequisite Mapping

Every Epic 3 story's Refractor prerequisite is named here. "No Refractor
prerequisite" is explicit when applicable.

| Epic 3 Story | Prerequisite from Gap Analysis |
|---|---|
| **3.1** openCypher `full` engine integration | §2.1 (parser GAP, epic-sized — explicit work item: introduce `full` engine alongside `simple`, both behind `RuleEngine` interface). §3.13 / §2.5 (Deviation 13 pipeline key parser must close so the engine's outputs reach domain entities). |
| **3.2** Capability Lens activation & Capability KV projection | §2.2 (cypher semantics — blocked on 3.1's full engine), §2.3 (Capability KV adapter PARTIAL — extend engine→adapter interface to thread `projectedAt`/`projectedFromRevisions`), §3.9 (adapter envelope reshape) and §2.4 (NFR-P3 conformance measurement on real fan-out workload — Story 3.2 is the measurement). §2.5 (Deviation 13) must close before this story can demonstrate against a seeded graph. |
| **3.3** Processor step 3 — Capability KV authorization | §2.4 (read-after-write coherence; Story 3.3 wires the `AuthFreshnessExceeded` ceiling against §1.6 e2e empirics). No Refractor work in 3.3 itself — it consumes the Capability KV state that 3.2 produces. |
| **3.4** Structured denial response (FR22) | No Refractor prerequisite. (Consumes `cap.role-by-operation.<operationType>` produced by the secondary Lens in §2.3 / Story 3.2.) |
| **3.5** Three-plane auth failure traceability (FR23) | No Refractor prerequisite. (Trace record sources `projectedAt` / `projectedFromRevisions` from the Capability KV entry; both fields land via §2.3 / Story 3.2.) |
| **3.6** Role-scoped access domain & audit (FR24, FR25) | No Refractor prerequisite beyond 3.2 (role/permission domain mutations re-project via the Capability Lens; no new adapter or engine work). |
| **3.7** Capability Lens adversarial test suite (Phase 1 Gate 3) | No Refractor prerequisite. (Tests the integrated 3.1–3.6 stack; relies on R6 / §3.x having converted `StubCapabilityChecker` to the real path so attack vector #3 — lens definition mutation — has a real auth gate to bypass.) |

---

## Closing Note

This is the Epic 2 exit artifact. The morphed Refractor is **fit for
purpose as a substrate for Epic 3 — for one cleanup story.** The one
critical pre-Epic-3 carry is Deviation 13 / §2.5 / R1 (pipeline key
parser); without it Story 3.2's "real graph" assertions degenerate to
the meta-lens slice. Everything else is either properly deferred to a
defined later story or already resolved in 2.1b.
