# Materializer → Refractor Morph Plan
**Researched:** 2026-04-08
**Researcher:** Subagent commissioned by Lattice brainstorming session 2026-04-08

---

## 1. Executive Summary

**Verdict: MEDIUM morph (estimated 4–7 weeks for one engineer to land all delta items).**

> **POST-RESEARCH UPDATE (2026-04-09):** Andrew clarified several architectural facts that reduce the morph's severity:
> 1. **Link data lives in the JSON value, not only the key.** Lattice links carry `{nodeId, otherNodeId, name, direction, isDeleted}` in the value body — same shape as Materializer. The key (`lnk.<younger>.<name>.<older>`) is an addressing convention, but the data is in the document. This **largely dissolves item §3.1** (Core KV event-shape mismatch), reducing it from BLOCKER to minor adaptation.
> 2. **Refractor consumes Core KV change feed (CDC), NOT `lattice-events`.** Lattice-events are intentional business events (like `LeaseSigned`), not CDC. Materializer already works this way — no change needed. This **dissolves item §3.4** (lattice-events envelope plumbing).
> 3. **Vertices and aspects under separate keys is already how Materializer works.** Aspect key prefix starts with the node's prefix in Materializer too. The vtx/asp split is compatible, not a morph item.
> 4. **Parser strategy change:** Instead of ANTLR4 (Java toolchain dependency), import a ready-made Go openCypher parser from an open-source repo such as `github.com/jtejido/go-opencypher`. This may eliminate the entire Phase 4 risk if the parser already supports WHERE.
> 5. **Capability Lens likely needs NO WHERE clause** — just structural path traversal. Spike still critical to confirm, but expected severity of G3 drops from "stream-derailing" to "likely a non-issue."
> 6. **Variable-length paths not required for MVP.** Confirmed.
> 7. **Personal Lens target is a NATS stream** (subject `lattice.sync.user.<user-id>`), payload is a small hint/pointer, actual data lives in a multi-tenant master projection. Subject to further design with the architect.
> 8. **Lens-of-lenses:** The Refractor.md is contradictory (meta vertex in Core KV vs Rules-as-Stream). Andrew's resolution: if we can create a durable consumer to consume meta vertices in Core KV, no separate stream needed. Hardcoded bootstrap lens for MVP.

The good news is bigger than expected: Materializer's evaluator already does multi-hop fixed-length traversal, the Postgres + NATS-KV adapters are clean and pluggable, the 4-tier failure model is mature, the rule hot-reload + zero-downtime migration story is fully shipped, and the in-memory NATS test harness is well-isolated and lift-able as a Go module. The MVP vertical slice (one op → ledger → Processor → Core KV → Refractor → Postgres) can run on Materializer **almost as-is** — the Core KV data shapes are compatible.

The parser situation is still the key variable: **Materializer's parser is hand-rolled, not ANTLR4 as the architecture document claims.** However, the plan to import a ready-made Go openCypher parser (e.g., `github.com/jtejido/go-opencypher`) may eliminate this as a risk entirely, pending evaluation of that library's maturity. And if the Capability Lens turns out to need only structural traversal (no WHERE), the v1 parser works today.

**Single biggest risk:** whether the open-source Go openCypher parser is production-ready and covers the required subset. If not, hand-rolling WHERE support is still 1–2 weeks.

**Single biggest win:** the morph for the MVP vertical slice is tiny — essentially "rename subjects, point at Lattice's Core KV, ship". Most morph delta items can be deferred until after the slice proves the architecture.

---

## 2. Verdict on the Six Known Morph Delta Items

### 2.1 Path Projection / RLS link mirroring

**VERDICT: Confirmed — net new feature. Effort: M (one target store at a time).**

Today the evaluator emits one `EvalResult` per matched path with `Keys` and `Row` columns derived from `var.prop` projections. The `RETURN` clause cannot reference an edge variable's properties, only node properties (see `internal/engine/compiler.go:74-90`, which splits every RETURN expression on `.` and assumes `variable.property` where the variable maps to a node binding). The Postgres adapter writes flat rows via `INSERT ... ON CONFLICT DO UPDATE`; there is no concept of mirroring an edge as a row in a separate join table or as an RLS policy.

There is **no RLS-aware projection logic anywhere** in Materializer. The brainstorming pre-mortem already flagged this: per-target-store strategies are required (Postgres RLS quirks; ES has none; NATS KV has none). Three implementations, plus a new "edge projection" capability in the evaluator and compiler.

Evidence:
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/compiler.go:74-90` — RETURN expressions parsed strictly as `var.prop` for node bindings only.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/evaluator.go:144-166` — projection loop reads `b.varProps[col.Variable]`; edges have no `varProps` analog.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/postgres.go` — pure flat upsert, no RLS hooks.

### 2.2 Personal Lens (per-user NATS subject targets)

**VERDICT: Confirmed — net new adapter, NOT a config tweak. Effort: S–M for one adapter, M–L if you also need parameterised projections.**

The current adapter interface (`internal/adapter/adapter.go:9-18`) is `Upsert(keys, row) / Delete(keys) / Probe / Close`. Both Postgres and NATS-KV adapters are constructed at rule load time with pre-bound destinations (table or bucket). There is no notion of a per-user fan-out, no notion of a "subject template" with variables substituted from the row, and the rule schema's `Into` block has only `target`, `bucket`, `dsn`, `table`, `key` (`internal/rule/schema.go:45-53`).

Adding a "NATS subject target" that publishes to `lattice.sync.user.<id>` is mechanically straightforward as a third adapter implementation — the interface is small and the existing NATS-KV adapter in `internal/adapter/natskv.go` is 88 lines and reads as a clean reference. The harder part is the **parameterisation**: you need a way to say "the destination subject is derived from the row's `userId` column", which is novel and currently has no rule-schema slot. Either:
- (a) bake it into the new adapter's constructor as a Go template string (simple, low-flexibility), or
- (b) add a `subjectTemplate` field to `IntoConfig` and a substitutor (more general, more morph surface).

The Edge Lattice brainstorming notes imply (b) is what's wanted (because each Personal Lens projects a different per-user view), so plan on extending `IntoConfig`.

### 2.3 Self-projecting configuration (Lens definitions sourced from `vtx.meta.lens.<id>`)

**VERDICT: Refined — Materializer loads rules from a NATS JetStream stream, not a KV bucket. The morph is real but not as bad as "rewrite the rule loader".**

Today rules come from a dedicated JetStream stream `MATERIALIZER_RULES` on subject `materializer.rules.<team>.<ruleID>` with `MaxMsgsPerSubject=1` (i.e., latest-version-per-rule semantics) and a durable consumer using `DeliverLastPerSubjectPolicy`. Hot reload is built in — see `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/loader.go:16-100`. Rule update classification (INTO-only vs MATCH change) is in `internal/rule/update.go`, and the lifecycle is fully wired (`SetUpdateCallback`, `HotReloadInto`, `HotReloadPlan`).

The morph: Lattice wants Lens definitions to live in the Core KV as `vtx.meta.lens.<id>` vertices, written via the normal Processor `ops.meta.>` lane. Two reasonable approaches:
1. **Adapter approach (preferred):** keep the Loader's existing interface, replace the JetStream consumer with a `WatchAll` against the Core KV bucket filtered to `vtx.meta.lens.>`. Each watch event becomes a "rule loaded/updated/deleted" callback. The downstream `Loader` machinery is reusable verbatim.
2. **Bridge approach (faster to ship, ugly):** keep the Loader as-is and add a tiny "lens replicator" that watches `vtx.meta.lens.*` in Core KV and republishes to `materializer.rules.>`. Two sources of truth, but zero touch to the morph-critical Loader.

**Chicken-and-egg:** the brainstorming session already identified this (G4) — the Refractor needs to know about a "lens-of-lenses" before it can load any lens. Mitigation: hardcoded bootstrap lens shipping in the Refractor binary (just read `vtx.meta.lens.>` literally with a hardcoded subject filter; the bootstrap "lens" is not a lens at all but a constant).

**Effort: M.** The Loader is not the hard part — it's well-factored. The hard part is the rule schema. Today rules are YAML; in Lattice, lens definitions will be JSON aspect bodies on a Core KV vertex with a different shape (DDL-validated, with Lattice's `vtx.meta.lens` schema). You'll need a translator that converts the Core KV aspect into a `*rule.Rule`. That's bounded work — maybe 200 lines.

Evidence:
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/loader.go:16-25` — hard-coded `MATERIALIZER_RULES` stream + `materializer.rules.>` filter.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/schema.go:62-75` — Rule struct (id, team, match, into, retry).
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/update.go` — well-factored update classification you should preserve.

### 2.4 Crypto-shred propagation (KeyShredded → row nullification)

**VERDICT: Confirmed — entirely net-new. Effort: S, but only after Stream 6's event schema is locked.**

Materializer has no listener for any event other than Core KV change events on the rule consumer. There is no `KeyShredded` handler, no row-nullification path, no concept of "wipe these fields but keep this row" vs "delete this row entirely". The control plane has `delete` (deletes a rule + all its consumers/health/etc.) but not "delete one row from a target store because a key was shredded".

This is genuinely small work mechanically — subscribe to a NATS event subject, look up affected rule projections via the rule index, issue UPDATE/DELETE through the existing adapter interface — but it's blocked on Stream 6 publishing the `KeyShredded` event schema (this is the cross-stream contract risk in the pre-mortem). It also requires Refractor to maintain a reverse index "which target rows derive from this identity vertex", which today does not exist (the evaluator is forward-only and stateless w.r.t. anchor → output row mapping).

### 2.5 Secure Lens type (vault-decrypted aspects)

**VERDICT: Confirmed — entirely net-new. Effort: S in Refractor itself; the cost is the Vault interface contract (Stream 1) and implementation (Stream 6), not Stream 2.**

Materializer has no notion of "encrypted aspect" or "decrypt-on-read". Core KV is consumed as opaque JSON and properties are pulled into the projection by name. To support a Secure Lens:
1. The evaluator needs a "decrypt this property" hook keyed off a DDL marker (which does not exist yet — this is a Stream 1 thing).
2. Refractor needs to know the per-identity key-id to fetch from Vault, which means walking from the aspect to its owning identity (a one-hop link traversal — fine, the evaluator already does this).
3. Vault calls need to be cached aggressively or every projection is a key fetch.

Refractor's part of this is small once Stream 1 has defined the "sensitive field" DDL marker and the Vault interface, and once Stream 6 has the actual Vault running. Plan this as a late-phase morph item, blocked on contracts owned by other streams.

### 2.6 Multi-hop ReBAC cypher expressiveness ← spike on this hardest

**VERDICT: Refined and worse than feared. Effort: L for full WHERE-and-literals; XL if variable-length paths are required.**

Spike answer to question A.3: Yes, **Materializer today, as-is, can evaluate** the query
```
MATCH (i:Identity)-[:memberOf]->(r:Role)-[:canExecute]->(c:Command)
RETURN i.id AS iid, c.id AS cid
```
and produce one row per `(identity, command)` pair. This works because:
- The hand-rolled parser at `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/parser.go:299-325` parses arbitrarily long node-edge-node-edge-node patterns in `parsePattern()`.
- The compiler at `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/compiler.go:38-52` builds one `TraversalStep` per edge.
- The evaluator at `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/evaluator.go:89-142` loops over steps, fanning out one new binding per matching neighbour edge per step. No code path limits the number of hops.

So the *hop count* is fine. **What is not fine** is everything else needed for real-world ReBAC capability lenses:

| Cypher feature | Materializer parser | Materializer evaluator | Evidence |
|---|---|---|---|
| WHERE clause | NOT supported (no `WHERE` keyword in lexer at all) | Not implemented | `parser.go:31-40, 147` — only MATCH/OPTIONAL/RETURN/AS recognised as keywords |
| Comparison operators (`=`, `<>`, `<`, `>`) | NOT tokenised | n/a | `parser.go:99-155` — no `=`, `>`, `<` lexing |
| String/number literals | NOT tokenised | n/a | same |
| Property maps `{k: v}` on node patterns | NOT supported | n/a | `parser.go:327-351` — only `(var:Label)` |
| Variable-length paths `[:R*1..5]` | NOT supported | NOT implemented | `parser.go:390-413` — no `*` token, no range syntax |
| Predicates on relationships | NOT supported | n/a | edge AST has only `Type` and `Direction` |
| OPTIONAL MATCH | Supported | Supported (NULL semantics) | `parser.go:272-297`, `evaluator.go:113-122` |
| Multi-hop fixed-length | Supported | Supported | (above) |
| Multi-MATCH | Supported | Supported | `parser.go:228-244` |

**Implications for the Capability Lens:** if the ReBAC capability cypher is expressible as multiple fixed-length MATCH clauses with no per-step filtering and no equality/role-name predicates, you can ship it on Materializer's parser today. If it needs anything like `WHERE r.scope = 'tenant_42'` or `WHERE c.deprecated = false`, the parser must grow:
- New keyword tokens: `WHERE`, `AND`, `OR`, `NOT`, `IS`, `NULL`, `TRUE`, `FALSE`.
- New operators: `=`, `<>`, `<`, `>`, `<=`, `>=`.
- Literal tokenisation: strings (`'...'` and `"..."`), integers, decimals, booleans.
- A WHERE-clause parser node and a small expression AST (`BinaryOp`, `Literal`, `PropertyAccess`).
- An evaluator predicate-filter pass after each step (or — better — pushed down into the step).

Realistic budget for "WHERE with literals + AND/OR + comparison ops, no functions": **5–10 days of focused work** if hand-rolled.

> **POST-RESEARCH UPDATE:** Andrew's revised strategy for the parser:
> 1. **Do NOT use ANTLR4** (avoids Java toolchain dependency in CI).
> 2. **Import a ready-made Go openCypher parser** from an open-source repo such as `github.com/jtejido/go-opencypher`.
> 3. **Andrew's hunch: the Capability Lens likely needs NO WHERE clause** — just structural path traversal between vertices. If confirmed by the spike, the v1 hand-rolled parser ships the Capability Lens today with zero changes.
> 4. **Spike remains critical** (week 1 of Stream 2): write the actual Capability Lens cypher query. If it parses on the v1 parser → done. If it needs WHERE → evaluate the open-source Go parser's maturity before hand-rolling.
>
> This dramatically reduces the expected severity of G3. The open-source parser approach may eliminate the entire Phase 4 risk.

**Variable-length paths** (`[:R*1..5]`) **not required for MVP** (confirmed by Andrew). If they're needed in v2, unroll depth-bounded (≤3 hops) paths as fixed-length MATCH clauses first.

Evidence: see file references throughout this section, especially `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/parser.go:31-155` for the lexer's keyword/operator coverage.

---

## 3. New Morph Delta Items Discovered

### 3.1 Core KV event-shape adaptation (REDUCED severity post-clarification)

**Effort: S. Severity: minor adaptation, NOT a blocker.**

> **POST-RESEARCH UPDATE:** Andrew clarified that Lattice links DO carry `{nodeId, otherNodeId, name, direction, isDeleted}` in the JSON value body — the key (`lnk.<younger>.<name>.<older>`) is an addressing convention, but the data document contains the full topology. **This means Materializer's Adjacency Builder can consume Lattice link values with minimal adaptation** — the JSON shape is compatible.

Remaining adaptation work:
- Lattice uses three separate KV buckets (`vtx.*`, `asp.*`, `lnk.*`) rather than one intermingled bucket. The Adjacency Builder needs to subscribe to `lnk.>` specifically (rather than filtering from a mixed stream).
- The `coreKvKey` field in Materializer's `CoreKVEvent` struct will need to map to Lattice's key naming convention. Minor struct adaptation.
- Node property access: Materializer reads `entry.NodeLabel` and `entry.Properties` expecting a flattened JSON object per node. Lattice stores aspects under separate `asp.*` keys. The evaluator needs an aspect-fetch step (walk from `vtx.<id>` to `asp.<id>.*`) — but Materializer already does multi-key KV reads during traversal, so the pattern exists.

This is bounded adaptation, not an architectural rework. Plan ~2 days including tests.

### 3.2 Subject taxonomy rename (operational, not architectural)

**Effort: S. Severity: cleanup, easily deferred.**

Every Materializer subject is `materializer.<...>`:
- `materializer.rules.<team>.<ruleID>`
- `materializer.health.<ruleID>`
- `materializer.dlq.<team>.<ruleID>`
- `materializer.metrics.<ruleID>`
- `materializer.audit.<ruleID>`
- `materializer.control` (the entire control plane on a single subject)
- `adj.<nodeID>` (adjacency KV — actually fine, no `materializer` prefix)

The `subjects` package (`/Users/andrewsolgan/Documents/GitHub/materializer/internal/subjects/subjects.go`) is the **single point of change** for all of this — clean architectural decision that pays off here. Rename to Lattice-conformant `refractor.health.<lensId>`, `lattice.dlq.refractor.<lensId>`, etc., and the rest of the codebase follows mechanically. Total work: ~2 hours including tests.

### 3.3 Control plane idiom mismatch

**Effort: M. Severity: noticeable; not a blocker but it's a contract surface.**

Materializer's control plane is `nc.QueueSubscribe("materializer.control", "materializer-control", handler)` — one subject, one queue group, op-in-payload (`/Users/andrewsolgan/Documents/GitHub/materializer/internal/control/service.go:280-305`). Lattice's spec mandates **NATS Service Framework endpoints** at `lattice.ctrl.<type>.<id>.<op>` per the brainstorming Stream 0 inventory item #93. These are different concepts:
- NATS Service Framework (`micro` API) provides automatic discovery, ping, stats, schema endpoints, and per-instance metadata. It's a higher-level abstraction than `QueueSubscribe`.
- The op-in-payload pattern works fine but has no discovery/introspection support.

Migration is bounded: rewrite `StartNATSListener` to use `micro.AddService`, register one endpoint per op (`pause`, `resume`, `rebuild`, `delete`, `health`, `validate`), and either keep the old subject as a compatibility shim or hard-cut. The handler bodies (`handlePause`, `handleRebuild`, etc.) all stay the same.

Recommend: do this migration **after** the MVP vertical slice (the simple `QueueSubscribe` works for one-instance dev), but do not let it slip past Stream 2's "Refractor NATS Control Service IDL" deliverable in the Phase 3 boundary contract.

### 3.4 ~~Idempotency / dedupe semantics for Lattice operation events~~ — DISSOLVED

> **POST-RESEARCH UPDATE:** Andrew confirmed that Refractor consumes **Core KV change feed (CDC via NATS KV watcher)**, NOT `lattice-events`. `lattice-events` are intentional business events (e.g., `LeaseSigned`) used for orchestration (Loom/Weaver), NOT for projection. Core KV CDC is exactly what Materializer already does. **No morph work needed here.**

---

## 4. What Materializer Already Has That Lattice Will Use Unchanged

(KEEP AS-IS — these are direct hits.)

- **Anchor-first evaluator with multi-hop fixed-length traversal**, including OPTIONAL MATCH NULL semantics and reverse traversal from non-anchor nodes back to anchor nodes. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/evaluator.go` (whole file).
- **Compiled query plans** built once at rule load time, cached on the rule, reused per message. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/compiler.go`.
- **Adapter interface** — clean, three-method interface (`Upsert`, `Delete`, `Probe`, `Close`) with optional `Truncater`. The adapter package imports nothing else from `internal/`. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/adapter.go`.
- **Postgres adapter** with `pgxpool` shared per DSN, composite-key support, `ON CONFLICT` upsert, parameterised queries, identifier quoting (post-CR fix), per-rule query timeout. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/postgres.go`.
- **NATS KV adapter** — 88 lines, the model for any future NATS-based target adapter. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/natskv.go`.
- **4-tier failure model** — `Infrastructure | Structural | Terminal | Transient` with explicit constructor functions and centralised `Classify`. Routes correspond to `pause-fetch / pause-rule / DLQ / retry-queue`. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/failure/classify.go`. The four tiers map cleanly onto Lattice's failure modes:
    - Atomic batch rejection → Terminal (DLQ for forensics)
    - DDL validation failure → Structural (pause the lens until DDL is reconciled)
    - Vault decryption failure → Transient (key may be re-fetchable)
    - Target store outage → Infrastructure (fetch-loop pause + buffer in NATS)
    - Malformed event from Core → Terminal (but a structurally-broken Core is a `Structural` for the whole lens; classify per-event)
- **Per-rule durable consumer manager** with create/remove/queue-group support. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/consumer/manager.go`.
- **Adjacency bootstrap protocol (ADR-7/8)** — wait for adjacency consumer lag = 0 before activating rule consumers. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/consumer/bootstrap.go`.
- **Adjacency live-event watch (ADR-16)** — pipeline watches its own adjacency KV with `UpdatesOnly` to fix the post-bootstrap race. This is hard-won (see Epic 7 retro B4) and architecturally important. Lattice will need it for the same reason. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/pipeline/pipeline.go` (the `runAdjWatch` goroutine).
- **Adjacency KV CAS-with-retry writes** for multi-instance safety. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adjacency/builder.go:52-103`.
- **Rule hot-reload classifier** — `ClassifyUpdate(old, new)` distinguishes INTO-only changes (swap adapter) from MATCH changes (swap query plan + rebuild). `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/update.go`.
- **HotReloadInto + HotReloadPlan with mutex protection** — proven pattern from Epic 7 (see retro B7). Both are in `pipeline/pipeline.go`.
- **Health KV reporter** with restart-preserving `errorCount` and `consumerLag`, and `*string` null-discipline for `pauseReason`/`lastError`. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/health/reporter.go`. Schema is camelCase, matches Lattice's brainstorming Phase 1.5 standard.
- **Lag poller** — periodic NumPending-derived consumer lag publisher. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/health/lag_poller.go`.
- **Audit stream writer** for append-only operational audit log. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/health/audit_writer.go`.
- **DLQ publisher** with structured error context. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/failure/dlq.go`.
- **Deferred retry queue** for transient failures. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/failure/retry.go`.
- **Pull-consumer pause + resume** with `PullHeartbeat(5s)` to avoid the 30-second silent reconnect bug (Epic 7 retro B6). `pipeline/pipeline.go`.
- **Validate / Rebuild (truncate optional) / Pause / Resume / Delete control operations**, plus `health` query op. `/Users/andrewsolgan/Documents/GitHub/materializer/internal/control/service.go`. The handler bodies survive the control plane idiom rewrite (item 3.3 above) unchanged.
- **Zero-downtime migration via parallel rule IDs** — two rules with different IDs targeting different tables, each with its own consumer/health/pipeline. Documented same-table-undefined constraint. Story 5.6.
- **Sample developer tool** for end-to-end validation. `/Users/andrewsolgan/Documents/GitHub/materializer/cmd/sample/` (per Story 7.2).
- **In-memory NATS + YAML fixture test harness** — `/Users/andrewsolgan/Documents/GitHub/materializer/internal/fixture/runner.go` and `runner_test.go`. Uses `nats-server` Go API directly with `t.TempDir()` StoreDir for isolation. **This is liftable as a Go module** (see §8.H below).
- **`subjects` package** — every NATS subject built through one type-checked function. Single point of rename for §3.2.
- **Pipeline lifecycle (`Pipeline.Run`)** — orchestrates fetch, evaluate, project, write, observe, with graceful drain on context cancellation.

That is roughly 80% of what the Refractor needs to do. The Phase 1.5 brainstorming statement that "Materializer ≈ Refractor at MVP grade" is, if anything, **understated** with respect to operational maturity — the failure model, hot reload, and zero-downtime story are remarkable for a 39-story project.

---

## 5. What Materializer Has That Lattice Will DISCARD

- **`team` field on every rule.** Lattice has no team concept (it has identities and DDLs); Materializer's `materializer.rules.<team>.<ruleID>` and `materializer.dlq.<team>.<ruleID>` baked the team in for tenant separation. In Lattice, all multi-tenancy is via identity scoping, not URL-path partitioning. `team` should be removed from `IntoConfig`, the `Rule` struct, and the `subjects` package. Touches `internal/rule/schema.go`, `internal/subjects/subjects.go`, `internal/health/`, every test fixture.
- **YAML rule format.** Lattice will use JSON aspect bodies on `vtx.meta.lens.<id>` validated against a DDL schema. The `gopkg.in/yaml.v3` dependency goes away (or stays for fixture loading only).
- **`config.yaml` + env-var configuration story.** Lattice runs as an ops-managed service; YAML config is fine for dev, but the file format and env vars should be revisited against Lattice's deployment story (probably similar, but needs review).
- **The `materializer.control` queue-subscribe pattern** (replaced by NATS Services framework — see §3.3).
- **`MATERIALIZER_RULES` JetStream stream** (replaced by Core KV `vtx.meta.lens.>` watch — see §2.3).
- **The "rules can target NATS KV bucket as a write destination" pattern as currently named.** Functionally this is exactly the Capability Lens use case and stays — but the rule schema field `into.target: nats_kv` should become something like `into.target: lattice-kv` to match Lattice's namespace.
- **The hand-rolled v1 parser as the long-term parser.** It's good enough to ship the MVP slice but should be replaced by ANTLR4-generated code as part of the WHERE/literals expansion.
- **Module path `github.com/asolgan/materializer`** — obvious rename, but worth noting for the import-rewrite step.

---

## 6. Renames Required

| Old (Materializer) | New (Lattice/Refractor) |
|---|---|
| `materializer.rules.<team>.<ruleID>` (JetStream subject) | n/a — lens definitions move to Core KV `vtx.meta.lens.<id>` |
| `materializer.health.<ruleID>` | `refractor.health.<lensId>` |
| `materializer.dlq.<team>.<ruleID>` | `lattice.dlq.refractor.<lensId>` (or a Stream 0 standard) |
| `materializer.metrics.<ruleID>` | `refractor.metrics.<lensId>` |
| `materializer.audit.<ruleID>` | `refractor.audit.<lensId>` |
| `materializer.control` (single subject) | `lattice.ctrl.refractor.<lensId>.<op>` (NATS Services) |
| `materializer-<rule-id>` (durable consumer name) | `refractor-<lens-id>` |
| `materializer-adjacency` (adjacency consumer name) | `refractor-adjacency` |
| `materializer-rule-loader` (loader consumer name) | n/a — replaced by Core KV watch |
| `MATERIALIZER_RULES` (stream name) | n/a — eliminated |
| `Rule` (Go type) | `Lens` |
| `RuleID` field | `LensID` |
| `team` field | removed |
| `ruleId` JSON field in health/DLQ | `lensId` |
| `adj.<nodeID>` (adjacency KV key) | `adj.<vtxId>` (semantic only — same shape) |
| `internal/rule/` package | `internal/lens/` |
| Module path `github.com/asolgan/materializer` | `github.com/<org>/lattice/refractor` (TBD) |
| Binary `materializer` | `refractor` |

The `subjects` package is the chokepoint — most of the rename burden lives there. Plan ~2 days for the rename including test updates.

---

## 7. Recommended Morph Sequence

The MVP vertical slice is "one op → ledger → Processor → Core KV → Refractor → Postgres → query". Refractor's part is "subscribe to Core KV change events, evaluate one lens, write to Postgres". This is what Materializer already does, modulo the Core KV event-shape adapter (§3.1).

### Phase 1 — Get the MVP slice running (week 1)

1. **Day 1: Fork & rename.** Clone Materializer; rename module path; rename binary; rename `Rule`→`Lens`, `internal/rule`→`internal/lens`, `RuleID`→`LensID` throughout. Strip `team`. Touch the `subjects` package to use `refractor.*` subjects. Compile cleanly. (1 day)
2. **Day 2: Core KV bucket adaptation (§3.1 — reduced severity).** Lattice link values carry the same JSON shape as Materializer expects (`{nodeId, otherNodeId, name, direction, isDeleted}`), so the adjacency builder's data parsing works as-is. Adaptation needed: subscribe to `lnk.>` specifically instead of a mixed bucket, and wire up aspect fetching for node properties (walk from `vtx.<id>` to `asp.<id>.*`). (1 day)
3. **Day 3: Hardcoded bootstrap lens.** Skip the dynamic Core KV lens-loading — instead ship a single `vtx.meta.lens.bootstrap` lens hardcoded in Go that says "watch all `vtx.contract.*`, project to Postgres table `contract_view`". This is enough to prove the MVP slice. (0.5 days)
4. **Day 4: Wire up against Stream 0's substrate + Stream 1's first Processor commit.** Bring up `make up` (Lattice's harness), submit the "create one contract" op via the CLI, watch it land in Core KV, watch the Refractor consume it via the shim, watch the Postgres row appear. Iterate until green. (1–2 days)

**Exit criteria for Phase 1:** the brainstorming session's "Hello Lattice" minimal end-to-end is observable on a developer laptop.

### Phase 2 — Self-projecting configuration (weeks 2–3)

5. Replace the hardcoded bootstrap lens with a Core KV watch on `vtx.meta.lens.>`. Build the JSON-aspect → `*Lens` translator. Keep the Loader's downstream interface intact. Write integration tests. (1 week)
6. Migrate the rule update lifecycle (`ClassifyUpdate`, `HotReloadInto`, `HotReloadPlan`) to work off Core KV watch events instead of JetStream messages. Most of this is already abstracted behind the `Loader.SetUpdateCallback` interface — the underlying source changes; the callbacks stay. (2–3 days)

### Phase 3 — Capability Lens spike (week 3, in parallel with Stream 3) ★

7. **Spike (G3 from the brainstorming risks):** write the Capability Lens cypher *as if Refractor existed*. If it parses on the v1 parser, ship it. If it needs WHERE/literals/predicates (likely), commit to the parser expansion in Phase 4. **DO NOT skip this spike** — Stream 3 is blocked on it.

### Phase 4 — Parser expansion (weeks 3–5) ★ CONDITIONAL — may be unnecessary

> **POST-RESEARCH UPDATE:** This phase is only needed IF the Capability Lens spike (Phase 3) reveals WHERE clause requirements AND the open-source Go openCypher parser (e.g., `github.com/jtejido/go-opencypher`) doesn't meet the need. Andrew's hunch: Capability Lens is pure structural traversal → Phase 4 is skipped entirely.

8. **If WHERE is needed:** evaluate `github.com/jtejido/go-opencypher` (or similar) for maturity, test coverage, and compatibility with Materializer's evaluator. If mature → import and adapt the evaluator. If not → hand-roll WHERE + comparison ops (5–10 days).
9. Implement evaluator predicate-filter pass per step (needed regardless of parser choice).
10. Add comprehensive unit tests.

**Exit criteria for Phase 4:** the Capability Lens cypher from Phase 3 evaluates correctly end-to-end and produces the expected NATS KV output.

### Phase 5 — Net-new morph deltas (weeks 5–7)

12. **Personal Lens NATS-subject adapter** (§2.2) — including `subjectTemplate` extension to `IntoConfig`. (3 days)
13. **Path Projection / RLS link mirroring** (§2.1) — Postgres-only initially. Add an "edge projection" capability to compiler + evaluator. Generate Postgres RLS policies as a side effect. (1 week)
14. **Crypto-shred listener** (§2.4) — listen for `KeyShredded` events from Stream 6, look up affected projections, issue UPDATE/DELETE. Blocked on Stream 6's event schema. (3 days)
15. **Secure Lens type** (§2.5) — wire the Vault decryption hook into the projection step. Blocked on Stream 1's Vault interface contract + Stream 6's Vault implementation. (3 days)

### Phase 6 — Operational polish (week 7+)

16. **Control plane migration to NATS Services framework** (§3.3). Keep handler bodies; rewrite the listener. (2–3 days)
17. **`lattice-events` operation envelope plumbing** (§3.4) — pull request-id, actor, op-id into the pipeline context for tracing. (2–3 days)
18. **Subject taxonomy final scrub** (§3.2) — make sure every emitted subject conforms to Stream 0's published taxonomy.
19. **ES adapter** if Stream 2 is asked for it (already in the inventory but not in the original morph delta).

---

## 8. Critical Risks for Stream 2 Timeline

### R1. Parser expansion is bigger than 6 tokens (G3 in the brainstorming, confirmed)

Materializer's parser is hand-rolled, not ANTLR4-generated as the architecture document claims. WHERE support is not "flip a flag", it's "build a real parser". If the Capability Lens needs WHERE, this is a 1–2 week sub-project that can derail Stream 2's calendar. **Mitigation:** run the Phase 3 spike in week 1 of Stream 2 — write the Capability Lens cypher first, before any other Stream 2 work, and confirm exactly what grammar features are required. If literals + comparison ops + AND are sufficient, stay hand-rolled. If anything more (functions, list comprehensions, parameters, WITH, ORDER BY) is needed, adopt ANTLR4 and absorb the toolchain cost.

### R2. The Core KV event shape mismatch (§3.1) is an unbudgeted morph item

Materializer's adjacency builder assumes Core KV entries carry `nodeId`/`otherNodeId` in the value body. Lattice's `lnk.<youngerId>.<name>.<olderId>` puts that in the key. The shim is small (Phase 1 step 2), but if it gets skipped or under-spec'd, the Refractor produces zero output and looks broken. **Mitigation:** make the shim a named, AC-tracked deliverable in Phase 1, not a hidden helper.

### R3. Vault interface contract drift (cross-stream, well-known but reiterated)

Stream 2's Secure Lens (§2.5) is small in Refractor itself, but it depends on Stream 1's Vault interface contract and Stream 6's implementation. The brainstorming pre-mortem (Stream 1, "Vault and Versioning") already flagged this. **Mitigation:** Refractor's Secure Lens work goes into Phase 5, not Phase 1, and Stream 2 publishes a "what we need from the Vault interface" doc in week 2.

### R4. KeyShredded event schema contested between Stream 2 and Stream 6

Pre-mortem already noted this. **Mitigation:** Stream 2 publishes a strawman event schema in week 2 even though Stream 6 owns the contract. Force the conversation early.

### R5. The Lens-of-Lenses bootstrap (G4)

Confirmed real. Without a hardcoded bootstrap lens, the Refractor can't load any lens, including the lens that defines other lenses. **Mitigation:** ship the hardcoded bootstrap as a constant Go struct, not a YAML file or KV value. Name it explicitly (`internal/lens/bootstrap.go`) so it's findable.

### R6. Live-event adjacency race (already solved by Materializer — keep the solution)

Epic 7 retro B4 documents a hard-won fix where the pipeline watches its own adjacency KV with `UpdatesOnly` to handle the post-bootstrap race when nodes and edges arrive concurrently. **Risk:** during the morph, an over-eager refactor of the pipeline could remove this. **Mitigation:** add a regression test that asserts `runAdjWatch` exists, or pin ADR-16 in Refractor's architecture as load-bearing.

### R7. Read-Modify-Write health KV bugs (Epic 7 retros B1, B2, B3, B7)

Materializer shipped four bugs in one retro related to RMW health writes that didn't propagate cached fields. The lesson (L2 in the retro) is "every RMW method needs a Fields Carried From Cache list in its spec". **Mitigation:** when porting `health/reporter.go` to Refractor, port it verbatim if possible. Do not "improve" it. The bug-fix history is the documentation.

### R8. ANTLR4 toolchain decision uncertainty

If Phase 4 commits to ANTLR4, the Refractor build now needs Java + the ANTLR4 jar in CI. This is a non-trivial cost for a Go project. **Mitigation:** if ANTLR4 wins, generate the parser once, commit the generated Go code, and don't regenerate in CI. The Java toolchain is only needed when grammar changes — once a quarter at most.

### R9. Postgres RLS strategy is per-target-store, not generic

The brainstorming pre-mortem already noted: "Postgres RLS quirks, ES has none, NATS-KV has none. Three implementations." Path Projection / RLS is not portable. **Mitigation:** ship Postgres RLS first, document ES and NATS-KV as no-RLS targets, and don't try to abstract over them.

### R10. The morph plan exceeds one engineer's capacity

If Phase 4 (parser expansion) hits the L estimate, the full morph is ~7–8 weeks for one engineer. The brainstorming session's stream cut implicitly assumes all streams ship MVP in ~6 weeks. **Mitigation:** the MVP slice (Phase 1 only) is one week. Most of the morph delta items can be deferred until after the slice proves the architecture and Streams 3–6 catch up.

---

## 9. Specific Questions That Still Need Human Decision

1. ~~**Is ANTLR4 acceptable as a build dependency?**~~ → **RESOLVED: No.** Import a ready-made Go openCypher parser from open source (e.g., `github.com/jtejido/go-opencypher`) instead. No Java toolchain.
2. **What does the Capability Lens cypher actually look like?** → Still open. Spike in week 1. Andrew's hunch: pure structural path traversal, no WHERE. Architecture session will clarify further.
3. ~~**How are vertices + aspects fed to the evaluator?**~~ → **RESOLVED (partially).** Andrew clarified that Materializer already stores aspects under separate keys with node-prefix-based keying — same pattern as Lattice. Not a fundamental mismatch. The evaluator needs an aspect-fetch step but the pattern already exists in the codebase. Detailed wiring to be resolved in Stream 2 week 1 alongside the Capability Lens spike.
4. **Is the `team` field truly unwanted in Lattice?** → Still open but leaning yes. Andrew to confirm whether Cells revive multi-tenancy.
5. ~~**Should the bootstrap lens be hardcoded?**~~ → **RESOLVED: Yes, hardcoded for MVP.** A durable consumer on Core KV `vtx.meta.lens.>` meta vertices eliminates the need for a separate lens stream. The bootstrap lens is a constant in Go that kick-starts this watch.
6. ~~**What event source does Refractor consume?**~~ → **RESOLVED: Core KV change feed (CDC via NATS KV watcher).** NOT `lattice-events`. `lattice-events` are intentional business events for orchestration (Loom/Weaver). Materializer already works on Core KV CDC — no change needed. **The brainstorming session's boundary contract for Stream 2 needs correction** (currently says "consumes from Stream 1 (`lattice-events` envelope)").
7. ~~**Variable-length paths?**~~ → **RESOLVED: Not required for MVP.** Unroll as fixed-length MATCH clauses if ever needed.
8. **Does Refractor need to consume `vtx.op.*`?** → Still open. Low priority.
9. ~~**Personal Lens target shape?**~~ → **RESOLVED: NATS stream** with subject `lattice.sync.user.<user-id>`. Payload is a small hint/pointer; actual data lives in a multi-tenant master projection. Subject to further design with the architect.
10. ~~**Source language?**~~ → **RESOLVED: Go.** (Implicit from all decisions.)

---

## 10. Evidence Index

### Brainstorming and specs
- `/Users/andrewsolgan/Documents/GitHub/Lattice/_bmad-output/brainstorming/brainstorming-session-2026-04-08.md` — full session, especially Phase 1.5 (Materializer ≈ Refractor at MVP grade), Stream 2 boundary contract, global risks G2/G3/G4.
- `/Users/andrewsolgan/Documents/Obsidian Vault/Lattice/Lens and Refractor/The Refractor.md` — the Refractor target spec.
- `/Users/andrewsolgan/Documents/Obsidian Vault/Lattice/Lattice System Spec.md` — `lnk.<youngerId>.<name>.<olderId>` link convention, `vtx.meta.lens.<id>` lens convention, crypto-shred narrative.

### Materializer planning
- `/Users/andrewsolgan/Documents/GitHub/materializer/_bmad-output/planning-artifacts/architecture.md` — ADRs 1–16, especially ADR-2 (parser strategy), ADR-6 (parser isolation), ADR-7 (dedicated adjacency consumer), ADR-8 (bootstrap protocol), ADR-15 (DeliverLastPerSubjectPolicy), ADR-16 (live adjacency watch).
- `/Users/andrewsolgan/Documents/GitHub/materializer/_bmad-output/planning-artifacts/prd.md` — context only (not deeply read).
- `/Users/andrewsolgan/Documents/GitHub/materializer/_bmad-output/planning-artifacts/epics.md` — confirmed no WHERE in v1 or v2 epics; multi-rule migration in Story 5.6.

### Materializer implementation artifacts (read for verdicts)
- `1-3-rule-schema-hot-reload.md` — rule schema and hot-reload lifecycle.
- `1-4-opencypher-parser.md` — **critical:** confirms hand-rolled recursive descent parser, not ANTLR4 (Story 1.4 dev notes).
- `1-7-adjacency-bootstrap-consumer-manager.md` — confirms `$KV.<bucket>.>` filter, `materializer-adjacency` durable, NATS KV tombstone handling.
- `3-4-centralized-failure-classification.md` — 4 tiers and `failure.Classify`/constructor pattern.
- `5-1-nats-service-control-endpoint.md` — confirms `materializer.control` single-subject pattern (NOT NATS Services framework).
- `5-6-zero-downtime-rule-migration.md` — parallel rules with different IDs and tables.
- `6-2-in-memory-nats-fixture-execution.md` — in-memory NATS fixture harness in `internal/fixture/`.
- `epic-2-retro-2026-03-26.md` — hot-reload concurrency lessons.
- `epic-7-retro-2026-04-08.md` — **critical:** seven bugs from integration testing including B4 (live adjacency race) and B7 (HotReloadPlan), L1/L2 lessons on RMW and wiring ACs.

### Materializer source code (read for ground truth)
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/parser.go` — **the truth on parser scope.** Lexer at lines 18-32 shows only `MATCH/OPTIONAL/RETURN/AS` keywords; no `WHERE`, `WITH`, `*`, comparison operators, or literals. Recursive-descent parser at lines 226-499.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/compiler.go` — confirms `var.prop` projection only (lines 74-90); fixed-length multi-hop traversal (lines 38-52); first required MATCH defines anchor (lines 56-65).
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/engine/evaluator.go` — multi-hop fan-out (lines 89-142); reverse traversal (lines 169-193); OPTIONAL NULL semantics (lines 95-122).
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adjacency/builder.go` — **critical:** Core KV event shape (`CoreKVEvent` at lines 28-37) — explicit `nodeId`/`otherNodeId`/`name`/`direction`/`isDeleted` fields in JSON value body. CAS-with-retry write at lines 52-103.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/loader.go` — JetStream rule loader with hot reload (lines 16-100); `MATERIALIZER_RULES` stream and `materializer.rules.>` filter hardcoded.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/rule/schema.go` — Rule struct (lines 62-75); YAML parsing; team field; integration with `engine.Parse`/`engine.Compile`.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/adapter.go` — three-method interface (lines 9-18) plus optional `Truncater`.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/natskv.go` — full 88-line NATS KV adapter; the model for any future NATS-target adapter (Personal Lens).
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/adapter/postgres.go` — pgxpool, ON CONFLICT, identifier quoting, per-rule timeouts.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/control/service.go` — single-subject `materializer.control` queue subscribe (lines 280-305); per-op handlers; `Resumer`/`Pauser`/`Rebuilder`/`Deleter`/`RuleGetter` boundary interfaces.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/health/reporter.go` — `Entry` struct with camelCase JSON tags; restart-preserving fields.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/subjects/subjects.go` — single-source-of-truth subject builders. The chokepoint for the rename in §6.

### Files NOT read in depth (would help if reviewed before kicking off)
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/pipeline/pipeline.go` — referenced via stories and retros; the `runAdjWatch` goroutine in particular is load-bearing.
- `/Users/andrewsolgan/Documents/GitHub/materializer/internal/fixture/runner.go` — the lift target for §8.H reuse.
- Stories 1-1, 1-2, 1-5, 1-6, 1-8, 1-9, 1-10, 1-11 (Epic 1 substrate).
- Stories 2-1 through 2-6 (Postgres adapter detail).
- Stories 3-1, 3-2, 3-3 (failure handling detail beyond 3-4).
- Stories 4-1 through 4-4 (full health KV/lag/audit detail).
- Stories 5-2 through 5-5 (validate, rebuild, pause/resume, delete).
- Stories 6-1, 6-3, 7-1, 7-2, 7-3 (test harness + binary entry + sample tool).

A second-pass review of the pipeline file and the test harness runner would tighten the H-section verdict and would reveal whether the test harness has dependencies on the `subjects` package or other Materializer internals that complicate the lift.
