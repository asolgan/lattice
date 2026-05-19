---
title: Phase 1 Course Correction — Audit Findings & Proposal
date: 2026-05-18
status: DRAFT — awaiting Andrew's review
author: Winston (architecture lead)
trigger: Andrew paused implementation after Story 4.5 ship; requested 7-concern audit + redesign proposal.
scope: Concerns 1–7 from /bmad-help invocation 2026-05-18.
---

# Phase 1 Course Correction — Audit Findings & Proposal

## TL;DR

Five of the seven concerns are confirmed real and material. Two carry actionable fixes that fit a single corrective story; the others need either a documentation refactor (per-component sharding) or a multi-step plan (Materializer-token eviction + control-plane migration to NATS Services). The biggest finding is **architectural**: Epic 4 drifted from Lattice's "operations write, lenses read" model. The corrective story (proposed: Story 4.6 + Story 2.4) is scoped here, not started.

| # | Concern | Verdict | Severity | Fix vehicle |
|---|---|---|---|---|
| 1 | Progress tracking accuracy | **CONFIRMED** — three drifted totals across two files | LOW | One pass to reconcile + recompute (this doc proposes a fix-in-place) |
| 2 | `data-contracts.md` scope | **CONFIRMED** — §6.13 Story 3.6 dump is the worst offender (~59 lines); also duplicate `### 6.13` heading | MEDIUM | Evict §6.13 (Story-specific); relocate generic link-ordering rule into Contract #1 §1.4 where it already conceptually lives. |
| 3 | `epics.md` story descriptions | **CONFIRMED** — wrong key shapes (`asp.<vtxId>.<name>`, `lnk.<youngerId>.<name>.<olderId>`), instructions to "add to data-contracts as addendum", wrong binary name `lattice-refractor`, several `materializer.*` references | MEDIUM | Targeted sweep (scope below) |
| 4 | Architecture sharding for component context | **CONFIRMED** — the architecture document itself promised component-level reference pages at line 23 ("Implementers should consult the component page first"); they were never authored | MEDIUM | Author per-component reference pages — proposal in §C4 |
| 5 | Materializer→Refractor morph completeness | **CONFIRMED, MULTI-PART** — "materializer" token is pervasive (subjects, durables, streams, KV buckets, config defaults); control plane still uses `QueueSubscribe` not `micro.AddService`; lens-definition source uses `kv.Watch` not durable consumer on the Core KV backing stream | HIGH | Story 2.4 — Refractor Morph Completion + Lattice-Native Source. Scope in §C5 |
| 6 | openCypher full engine operational | **VERIFIED OK** — bootstrap seeds `engine:"full"` for Capability Lens; main.go routes to `UseFullEngine`; pipeline `evaluate.go` calls `fullEngine.ExecuteWith`. Story 3.2b multi-id e2e p99=5.7ms is direct evidence the production path runs full engine. | (none) | (none — close-out evidence noted) |
| 7 | Epic 4 design (read-only ops, contract drift) | **CONFIRMED** — `ApproveIdentityMerge` is a read-as-op anti-pattern; `ScanIdentityDuplicates` is read-heavy logic dressed as a write op; hydrator `ScanPrefixes` opens an unbounded read surface in Starlark; data-contracts/epics drift was triggered by these. | HIGH | Story 4.6 — Epic 4 Architectural Realignment. Design in §C7 |

---

## A. Evidence

### A.1 Progress tracking (Concern 1)

Three sources, three numbers, drifted:

| Source | Stories shipped | Token total |
|---|---|---|
| `token-usage-tracker.md` per-row re-sum | 28 with actuals | **~4,151K** |
| `token-usage-tracker.md` rolling totals block | 22 / 32+ | **3,306K** |
| `phase-1-progress.md` "Current State" | 27 / 32+ | **3,968K** |

True count (story rows shipped, including parents `3.1`, `3.1b`, `3.2` marked SPLIT excluded): **28 stories shipped** (10 in Epic 1, 3 in Epic 2 incl. 2.3, 10 in Epic 3 incl. 3.1a/b-i/b-ii + 3.2a/b, 5 in Epic 4). True actual-token total: **~4,151K** based on row-level sums. Both `phase-1-progress.md` and the rolling-totals block under-report. The rolling-totals block was self-flagged in my recent edit ("may drift from a clean re-sum") but the drift is larger than a few-percent residual — it's missing whole stories' worth.

### A.2 `data-contracts.md` scope (Concern 2)

- Two `### 6.13` headings co-exist: line 1385 ("Implementation Notes") and line 1406 ("Role / Permission Domain DDL — Story 3.6"). Markdown hygiene issue.
- Lines 1406–1463 of `data-contracts.md` are the Story 3.6 addendum. They contain:
  - A "decision record" on `assignedRole` vs `holdsRole` (Story-internal disambiguation).
  - The 5 Phase-1 role-NanoID variable names (internal-to-`internal/bootstrap` naming).
  - The 12 per-operation permission variable list (internal to identity-domain DDL seeding).
  - A "Link Key Alphabetical-Ordering Convention" subsection that IS architectural — but it duplicates and refines Contract #1 §1.4. Eviction target unless §1.4 is updated to inline this language.
- Story 4.4 brief explicitly skipped adding §6.14 with a note: "data-contracts.md has accreted internal-to-component / story-specific decisions (especially §6.13 from 3.6) that conceptually belong with the component code or domain docs, not next to cross-component interface contracts. Plan a documentation refactor pass after Epic 4 closes." That carry is exactly this work.

### A.3 `epics.md` story descriptions (Concern 3)

Concrete drift items spot-checked:

- Line 151 (Epic 1 framing): describes key shapes as `vtx.<type>.<id>`, `asp.<vtxId>.<name>`, `lnk.<youngerId>.<name>.<olderId>` — **wrong**. Contract #1 §1.5 mandates aspects as 4-segment `vtx.<type>.<id>.<localName>` and links as 6-segment `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>`. The `asp.*` prefix is not architectural (Deviation 2 already established this).
- Line 286 (Story 2.3 framing): uses `asp.*` again ("`vtx.*` → vertex mutation, `asp.*` → aspect mutation, `lnk.*` → link mutation").
- Line 628 (Story 2.1 AC): `asp.*` again in the CDC classification description.
- Line 624 (Story 2.1 AC): "the binary is `lattice-refractor`" — Deviation 1 already resolved this to bare `refractor`.
- Line 624 (Story 2.1 AC): "all packages previously named `materializer/*` are renamed `refractor/*`" — the rename happened at package level only; subject namespaces and durables retain `materializer.*` (concern 5).
- Line 979 (Story 3.6 AC): "the role/permission domain DDL is **documented in `data-contracts.md` as an addendum to Contract #6**" — this is the AC text that triggered §6.13. Bad-by-construction.
- Lines 329, 356 (Stories 2.2, 2.5 / gap analysis): "if any finding contradicts the architecture contracts in `data-contracts.md`, the specific contract section and recommended amendment are called out explicitly" — fine in spirit, but pairs with the line-979 pattern to encourage contract-doc dumping.

The pattern is consistent: epics.md treats `data-contracts.md` as the documentation-of-record for any architectural detail, regardless of whether it's a cross-component contract or an internal-to-component implementation choice.

### A.4 Architecture sharding (Concern 4)

`lattice-architecture.md:23` — direct quote from the document itself:

> Later sections will provide **component-level reference pages** for each major component (Processor, Refractor, Gateway, Loom, Weaver, Vault) that collect: what it owns, what it reads/writes, its contracts in/out, failure modes, and which principles/constraints apply. Implementers should consult the component page first, then trace back to this section for rationale.

**Those pages do not exist.** The document is 1,040 lines of foundation + cross-cutting + decisions, but no component reference pages. As Winston I cannot "consult the component page first" because there is no per-component page to consult. This is why every story brief I author has to inline the architectural framing inside the brief itself — I am pre-loading what should have been ambient. That's wasted tokens and risks brief-vs-doc drift.

### A.5 Morph completeness (Concern 5)

**Three sub-findings:**

**5a — "Materializer" token is pervasive in production code (not just comments):**

| Surface | Examples (selected) |
|---|---|
| JetStream subjects | `materializer.metrics.<ruleId>` · `materializer.audit.<ruleId>` · `materializer.rules.<team>.<rule-id>` · `materializer.dlq.<team>.<rule-id>` · `materializer.health.<ruleId>` · `materializer.control` |
| Durable consumer names | `materializer-rule-infra` · `materializer-rule-loader` · `materializer-adjacency` · `materializer-rule-resume-infra` |
| JetStream streams | `MATERIALIZER_RULES` · `MATERIALIZER_DLQ_RULE-TERMINAL` |
| KV buckets | `materializer-health` (default in `internal/refractor/config/config.go:51`) |
| Source comments | `lens/schema.go:83` "Rule is the parsed, validated representation of a Materializer rule" · `pipeline.go:1050` "the Materializer-owned adjacency KV" · `lens/corekv_source.go:29` "REPLACES the MATERIALIZER_RULES JetStream loader" |

This is a deployment-visible identity leak. Operators reading metrics/health/audit subjects see "materializer.*" even though the deployed binary is `refractor`. CI / monitoring dashboards keyed on these subject names would need to be rewritten anyway if a real production system spun up Lattice today.

**5b — Control plane is still raw NATS, not NATS Services framework:**

`internal/refractor/control/service.go:293`:
```go
sub, err := nc.QueueSubscribe(subjects.Control(), "materializer-control", s.handleControlMsg)
```

Deviation 6 documented this as deferred. The Lattice architecture principle (per the morph plan §3.3 and the architecture's commitment to NATS Services) is that the control plane is a `micro.AddService` endpoint with `lattice.ctrl.refractor.<lensId>.<op>` subjects. Today it is the original Materializer `QueueSubscribe("materializer.control", ...)` pattern with the queue name keeping the "materializer" token.

**5c — Lens-definition source uses `kv.Watch`, not a durable JetStream consumer on the Core KV backing stream:**

`internal/refractor/lens/corekv_source.go:134`:
```go
watcher, err := kv.Watch(ctx, "vtx.meta.>", jetstream.IncludeHistory())
```

vs. the prior Materializer pattern (`internal/refractor/lens/loader.go:88-108`) which uses `CreateOrUpdateConsumer` on a dedicated `MATERIALIZER_RULES` stream.

Andrew's exact framing (verbatim from /bmad-help): *"the rule definitions, while moved from dedicated stream to Core KV vertices, are still being tracked by durable consumer, not the (ephemeral) KV watcher."*

The architecturally-correct path: a durable JetStream consumer on the `KV_<core-bucket>` backing stream with `FilterSubject = $KV.<core-bucket>.vtx.meta.>` (or however JetStream maps KV subjects). This preserves: (a) cross-restart sequence-position durability, (b) consistent CDC semantics with the rest of the Refractor (the same pattern the adjacency consumer uses), (c) no replay-of-history on every restart. KV Watch's `IncludeHistory()` works but is wasteful at scale and conflates Lattice's "everything is a stream + durable consumer" model with KV's higher-level abstraction. Story 2.1 took the KV-watch shortcut because the substrate package didn't (and still doesn't) expose Watch-like semantics over a durable consumer.

### A.6 openCypher operational (Concern 6) — VERIFIED OK

Evidence trail:
- `internal/bootstrap/primordial.go:614` — Capability Lens spec aspect carries `"engine": "full"`.
- `cmd/refractor/main.go:241` — `if r.ResolvedEngine == ruleengine.EngineFull { ... p.UseFullEngine(fullEngine, r.CompiledRule) }`.
- `internal/refractor/pipeline/evaluate.go:130` — production path calls `p.fullEngine.ExecuteWith(ctx, p.fullCR, ...)`.
- Hot-reload path (main.go:370) routes `EngineFull` correctly on lens-spec change.
- Story 3.2b multi-id e2e (`refractor_capability_multi_e2e_test.go`) projects through the production pipeline against bootstrap-seeded Capability Lens at p99=5.7ms. That latency is achievable only with the full engine running end-to-end through `vtx.meta.<lens>`-driven activation, adapter writes to `capability-kv`, and the Capability KV envelope per Contract #6 §6.2.

Concern 6 closes with no action item.

### A.7 Epic 4 design drift (Concern 7)

**The drifts:**

1. **`ApproveIdentityMerge` is a read-only "op"** — empty `MutationBatch + EventList`, response data carried back via `OperationReply.Detail`. This is the most explicit anti-pattern: it puts a query through the write path solely because the write path is the only Phase 1 capability-authorized entry surface. The Capability KV authorization model is reused — but at the cost of running a Starlark script that "examines Core KV and returns data as a map", surfaced verbatim in `internal/processor/commit_path.go:309-313`:
   ```go
   // Surface script ResponseDetail in the success reply (Story 4.2).
   // NFR-S6/S7: we do NOT log it.
   cp.replyTo(msg, BuildAcceptedReplyWithDetail(env.RequestID, now, result.ResponseDetail))
   ```
   The "do NOT log it" comment is the smell Andrew flagged. The need for it arises because the response payload now carries business data (name, email, phone, hasCredentialBinding) rather than commit-trace data (mutation count, revision). The original architecture: operations commit; reads come from lenses.

2. **`ScanIdentityDuplicates` (4.4) is a read-heavy operation dressed as a write op** — its real work is pairwise scanning all `vtx.identity.*` (and all `lnk.identity.*` for idempotency) and emitting `duplicateOf` link writes. The mutations are a fraction of the operation's true work; the scan is the dominant cost. The architecture for "continuous derivation across the whole identity space" is a Refractor lens, not a Processor op.

3. **`ContextHint.ScanPrefixes` hydrator extension** opens an unbounded read surface to Starlark scripts. `internal/processor/envelope.go:42`:
   ```go
   ScanPrefixes []string `json:"scanPrefixes,omitempty"`
   ```
   Today Phase 1 hard-limits the accepted prefixes to `vtx.identity.`, `lnk.identity.`, and (4.5) bare `lnk.`. But the surface itself is general — a future story could ask for `vtx.*` or `cap.*` — and the script becomes a generic data-query language with no read-side auth check. This contradicts the principle that hydrator reads are surgical, declared as key-list `Reads`, and explicit at the caller (Loom/Gateway) layer.

4. **`state.keys_with_prefix(prefix)` Starlark builtin** is the script-side enumerator over the scan-hydrated state. It exists only to support pairwise scans. Once `ScanPrefixes` goes, this goes.

5. **`crypto.*` / `strings.*` Starlark builtin additions** are reasonable in principle (pure deterministic functions) but their need is symptomatic — Levenshtein is a search-engine feature, not a Lattice DDL-script feature. It belongs in the cypher executor as a UDF, not in the Starlark commit-path sandbox.

6. **State machine acquired a "flagged-for-review" state** (4 states: `unclaimed | claimed | flagged-for-review | merged`) so that the scanner can mark identities. With detection as a lens, the state machine collapses back to 3 states (`unclaimed → claimed → merged`); "flagged" is an emergent property the operator reads from the Duplicate Candidates Lens KV, not a stored flag on the identity.

7. **Refractor `capabilityenv` wrapper acquired a `pendingReview: true` projection field** (Story 4.4 §5) to surface the `flagged-for-review` state through the cap entry. With the state gone, this field is no longer needed.

8. **`data-contracts.md` §6.13** (already covered) — and Story 4.4 was held back from adding §6.14 specifically *because* this drift was already visible.

The pattern is consistent: Epic 4 reached for write-path infrastructure to do read-path work, and each accommodation added a small new surface (Detail, ScanPrefixes, keys_with_prefix, Levenshtein, flagged state, pendingReview field, §6.13). Individually each is small; cumulatively they re-shape the contracts.

---

## B. Cost vs. accept

For each finding, the question is whether to spend tokens correcting it now or accept it as Phase 1 carry. My read:

| Concern | Carry Phase 1? | Why |
|---|---|---|
| C1 progress tracking | **Fix now** (cheap) — one pass over tracker + progress | Stale numbers across artifacts undermines every "we're 84% done" conversation; cost is ~5K tokens |
| C2 §6.13 eviction | **Fix now** — small mechanical edit | The right home (Contract #1 §1.4) is already there; we just relocate the generic part and delete the Story-specific part |
| C3 epics.md sweep | **Fix now** — targeted | Wrong key shapes will keep poisoning story briefs; let me sweep them now while my context is loaded. Scope: the dozen lines identified, plus a structural rule appended near the top of epics.md |
| C4 component sharding | **Fix now BUT phased** — author the most-loaded components first | I propose authoring 3 component pages now (Processor, Refractor, Substrate); defer Gateway/Loom/Weaver/Vault until those components have code |
| C5 morph completion + Lattice-native | **One new story (2.4)** | Token-rename + control-plane migration + lens-source migration is real work, not a sweep |
| C6 openCypher | (none) | Verified OK |
| C7 Epic 4 redesign | **One new story (4.6)** OR a partial sequence | Requires real code work — Duplicate Candidates Lens authoring + Refractor-side Levenshtein UDF + MergeIdentity rework to use adjacency lookup. Scope below. |

**Net new sequence proposal:**

```
4.6   Epic 4 Architectural Realignment              Opus      ~180K
2.4   Refractor Morph Completion + Lattice-Native   Opus      ~150K
6.0   Component Reference Pages (Processor,         (docs)    ~30K
      Refractor, Substrate)                         + handoff
```

Plus three in-this-session edits I'll execute right after you approve this proposal:
- Tracker + progress-md reconciliation (C1)
- `data-contracts.md` §6.13 eviction + §1.4 absorption (C2)
- `epics.md` sweep (C3)

---

## C. Detailed proposals

### C1. Tracker reconciliation

Recompute rolling totals from per-row Actuals; bump `phase-1-progress.md` story count from 27 → 28 and total tokens to ~4,151K; add a "as-of commit `b314677`" timestamp anchor; add a note that the rolling-totals block is recomputed at every Epic close (not spot-incremented).

### C2. `data-contracts.md` §6.13 eviction

1. Delete lines 1406–1463 (`### 6.13 Role / Permission Domain DDL`).
2. Relocate the link-ordering convention into Contract #1 §1.4 ("Reserved Underscore-Prefixed Local Names") as a new subsection §1.4.b or extend §1.4 with the alphabetical-younger-vertex-first rule — this IS architectural and cross-component.
3. Rename the line-1385 heading from `### 6.13 Implementation Notes` → `### 6.13 Implementation Notes` is fine — the duplicate goes away once §6.13-the-second is deleted.
4. Story-3.6's role/permission inventory (the 5 roles, the 12 permissions, the NanoID variable names) **moves to** the proposed `_bmad-output/components/identity-domain.md` (or wherever the per-component doc lands for the identity domain). Internal-to-bootstrap-implementation; not contract-level.

**Net diff size:** ~60 lines deleted from `data-contracts.md`; ~12 lines added to Contract #1 §1.4; ~50 lines moved to the identity-domain component page.

### C3. `epics.md` sweep

Edits, line-by-line:

| Line | Current | Proposed |
|---|---|---|
| 151 | `vtx.<type>.<id>`, `asp.<vtxId>.<name>`, `lnk.<youngerId>.<name>.<olderId>` | `vtx.<type>.<id>` (3-segment vertex), `vtx.<type>.<id>.<localName>` (4-segment aspect), `lnk.<typeA>.<idA>.<relation>.<typeB>.<idB>` (6-segment link) per Contract #1 §1.5 |
| 286 | `node_<label>_<id>` legacy reference | Keep (correctly describes the Materializer legacy); already RESOLVED in Deviation 13 |
| 286 | `vtx.<type>.<id>` and `lnk.*` | OK as-is |
| 624 | "the binary is `lattice-refractor`" | "the binary is `refractor`" (Deviation 1) |
| 624 | "all packages previously named `materializer/*` are renamed `refractor/*`" | "all packages renamed `refractor/*`; subject namespaces, durables, and KV buckets retain `materializer.*` tokens — full token eviction is Story 2.4 scope" |
| 628 | `asp.*` → aspect mutation | `vtx.<type>.<id>.<localName>` (4-segment) → aspect mutation |
| 979 (Story 3.6 AC) | "the role/permission domain DDL is documented in `data-contracts.md` as an addendum to Contract #6" | "the role/permission domain DDL is documented in the identity-domain component reference page; cross-component contract impact (if any) is summarised in `data-contracts.md` Contract #1 §1.4." |
| (new, near top of Epic framings) | (none) | Add a 6-line "Documentation Layering Rule" block: *cross-component contracts live in `data-contracts.md`; per-component implementation choices (DDL inventories, internal helper signatures, internal naming) live in component reference pages; ACs MUST NOT direct edits to `data-contracts.md` for per-component details* |

**Net diff size:** ~15 lines changed; ~10 lines added.

### C4. Component reference pages

Location proposal: `_bmad-output/planning-artifacts/components/<name>.md` (one per component). Audience: implementers + Winston + future architects. NOT in `internal/<component>/README.md` (audience there is Go consumers reading source; doesn't fit "what does this component own, in/out contracts, failure modes, principles").

Phase 1 set (just three pages, ~200–300 lines each):

1. **`components/processor.md`** — covers `internal/processor`, `cmd/processor`. Owns: write path steps 1–10, idempotency tracker, Starlark sandbox, DDL cache, fault-injection harness, denial response builder, auth trace emitter, capability authorizer. In: operation envelopes (Contract #2). Out: Core KV mutations + events (Contract #3). Sub-system principles: write-only via Starlark; no read-as-op pattern; ContextHint is surgical not bulk; Detail field carries audit/trace, not business data.
2. **`components/refractor.md`** — covers `internal/refractor`, `cmd/refractor`. Owns: lens-definition source, rule engine (simple + full openCypher), pipeline, adapters, control plane, observability. In: CDC events from Core KV backing stream. Out: lens-projected KV buckets (Capability KV, future Personal Lens / Secure Lens / etc.). Principles: lenses are the read path; lens definitions live in Core KV vertices; consumer is durable JetStream (post-2.4 morph completion). Pin: openCypher full engine sourcing (jtejido/go-opencypher vendored grammar + antlr4-go v4.13.1).
3. **`components/substrate.md`** — covers `internal/substrate`. Owns: NATS connection, NanoID generation, key shapes (`ClassifyKey`, `ParseVertexKey`, `VertexKey`, `AspectKey`, `LinkKey`), atomic batch, publish batch, KVPutWithTTL, KVListKeys. In: caller-provided NATS handles + connection options. Out: typed key/envelope/batch APIs. Principles: substrate exposes only what's architecturally common; Watch/durable-consumer helpers TBD (gap is Refractor's Lattice-native source story).

Plus an **`components/_index.md`** that points to the three pages + reserves entries for Gateway/Loom/Weaver/Vault (placeholders until they have code).

Sharding the architecture doc itself (`bmad-shard-doc`) is a downstream option but not necessary if the per-component pages are authored — the architecture doc then stays as it is, with the per-component pages providing the "consult first" layer.

### C5. Story 2.4 — Refractor Morph Completion + Lattice-Native Source

**Scope:**

Phase A — Token eviction. Rename across `internal/refractor/` and `cmd/refractor/`:
- Subject namespaces: `materializer.metrics.*` → `lattice.refractor.metrics.*`; `materializer.audit.*` → `lattice.refractor.audit.*`; `materializer.rules.*` → DELETED (lens defs now live in Core KV; the `MATERIALIZER_RULES` stream stops being authoritative); `materializer.dlq.*` → `lattice.refractor.dlq.*`; `materializer.health.*` → DELETED (Health KV per Contract #5 is canonical; Refractor doesn't need a side-channel); `materializer.control` → `lattice.ctrl.refractor` (handled in Phase B).
- Durable consumer names: prefix → `refractor-*` (also covered in Phase C — the lens-source durable is new).
- JetStream streams: `MATERIALIZER_RULES` → DELETED (no longer authoritative); `MATERIALIZER_DLQ_RULE-TERMINAL` → `REFRACTOR_DLQ_TERMINAL` (or per-lens variants per current pattern).
- KV bucket default `materializer-health` → REMOVE the bucket entirely (use Health KV per Contract #5; `internal/refractor/config/config.go` cleanup).
- Source comments: bulk rewrite "Materializer" → "Refractor" preserving historical context where it's about morph lineage (e.g., "simple engine — Materializer-derived" comments are fine because they're factual provenance).

Phase B — NATS Services control plane. Migrate `internal/refractor/control/service.go` from `nc.QueueSubscribe` to `micro.AddService` with endpoints at `lattice.ctrl.refractor.<lensId>.<op>`. Auth swap (already on `StubCapabilityChecker`) becomes the real Capability-KV-backed checker once read auth is in scope (Phase 1 keeps stub; full real-read-auth is Phase 2 work that this story does not own).

Phase C — Lens-definition source: durable JetStream consumer on the Core KV backing stream filtered to `vtx.meta.*` subject (the existing CoreKVSource is the integration seam; replace its KV Watch internals with substrate-provided durable-consumer helpers; substrate gains a new `SubscribeKVChanges(bucket, keyPrefix, durableName) → <-chan KVEvent` helper).

Phase D — `team` field cleanup (Deviation 4 carry): finish removing the vestigial `team` field from the Lens struct + subject builders. Subjects collapse from `lattice.refractor.<facet>.<team>.<lens-id>` to `lattice.refractor.<facet>.<lens-id>`.

Phase E — Verification: full suite green, Capability Lens still p99 < 50ms, all bypass + Gate 3 gates green. New verification: a deployment-grep audit that `materializer` appears 0 times in `internal/`, `cmd/` (allowed: comments explicitly tracing morph provenance, e.g., "the simple engine is Materializer-derived").

**Out of scope for 2.4:** real read-auth integration; multi-cell control plane; full migration of control-plane Op handlers — only the framework migration.

**Estimate:** Opus, ~150K. Bigger than 4.5 because it touches ~30 files and three sub-systems. Splittable into 2.4a (token eviction, mechanical) and 2.4b (control plane + lens-source, design-bearing) if budget pressure.

### C6. (none)

### C7. Story 4.6 — Epic 4 Architectural Realignment

**Premise:** Epic 4 shipped working code; the realignment is *not* a revert. The shipped Starlark-based scan + review path stays runnable through 4.6's implementation, then gets removed once the lens-based replacement is verified.

**Scope:**

Phase A — Duplicate Candidates Lens. A new bootstrap-seeded lens definition (`vtx.meta.<NanoID>` with `.canonicalName: "duplicate-candidates"` and `.spec` aspect with the cypher rule body and `adapter: nats-kv`, `bucket: duplicate-candidates`). Cypher pseudocode:
```cypher
MATCH (a:identity), (b:identity)
WHERE id(a) < id(b)
  AND a.state <> 'merged' AND b.state <> 'merged'
  AND (
    (a.email = b.email AND a.email IS NOT NULL)
    OR (a.phone = b.phone AND a.phone IS NOT NULL)
    OR levenshteinRatio(a.name, b.name) >= 0.85
  )
RETURN a.key, b.key, a.{name, email, phone, state, createdAt, hasCredentialBinding},
       b.{name, email, phone, state, createdAt, hasCredentialBinding},
       criteria_list(a, b) AS criteria
```
Projects to `duplicate-candidates` KV keyed by canonical pair-key (`flagged.identity.<lowID>.identity.<highID>`). Refractor reprojects on every identity mutation (existing behavior).

Phase B — `levenshteinRatio(string, string) -> float` and `levenshteinDist(string, string) -> int` as Refractor cypher-executor UDFs. Pure / deterministic / O(N²). Moved from the Starlark `strings.*` module to the cypher executor. The Starlark builtins can be retired in 4.6 (no caller after 4.6 ships) or left as dead code if simpler — the script-side `strings` module hasn't been called outside 4.4's `ScanIdentityDuplicates` branch.

Phase C — `ScanIdentityDuplicates` op DELETED. The DDL `permittedCommands` removes the entry; the script branch deletes; the permission vertex + grant link tombstoned. `ContextHint.ScanPrefixes` + `state.keys_with_prefix` Starlark builtin removed (no callers after 4.6).

Phase D — `ApproveIdentityMerge` op DELETED. Replaced by a CLI `lattice candidates list` verb (Story 6.1 absorbs this) reading `duplicate-candidates` KV directly. Phase 1 minimum: ship an operator-script (`scripts/lattice-candidates.go`) that does the KV read; full CLI integration is 6.1.

Phase E — `MergeIdentity` op refactor. Eliminate the `lnk.` global scan in favor of:
- Outbound links: `ContextHint.LinkScans []string` — a NEW narrow scan field, accepting only `lnk.<type>.<id>.` 4-segment prefixes (specific source vertex's outbound). The hydrator enforces this narrowing; max-per-prefix cap 100 (one identity has bounded out-degree in Phase 1 single-cell).
- Inbound links: read from Refractor's adjacency KV. Substrate exposes `AdjacencyForNode(nodeKey) -> []EdgeID` (the EdgeID *is* the link key per Story 3.2b). Processor reads adjacency directly. This is the architecturally-right seam.

Phase F — State machine simplification. `flagged-for-review` state REMOVED. Three-state machine: `unclaimed → claimed → merged`. `pendingReview` cap-entry field REMOVED. The `capabilityenv` wrapper extension from Story 4.4 reverts; the Capability Lens cypher's `state <> 'merged'` filter already excludes merged identities from cap entries.

Phase G — `ScriptResult.ResponseDetail` and `OperationReply.Detail` semantics tightened. The field stays but is restricted by **convention** (enforced via brief + lint comment) to commit-trace data: `{mutationCount, eventCount, revision, traceId}`. No business data ever. The "NFR-S6: do NOT log it" comment goes away because there's nothing sensitive to leak.

Phase H — Identity DDL Starlark script shrinks from ~720 LOC back to ~400 LOC (removes `ScanIdentityDuplicates`, `ApproveIdentityMerge`, narrows `MergeIdentity` to adjacency-based link enum). The previously-deferred `MergeIdentity → operator` grant gets seeded in the primordial bootstrap (verify-bootstrap goes 154 OK → ~156 OK), closing the Phase-1 carry. `MergeIdentity` integration tests move from stub mode back to capability mode.

Phase I — Bypass / Gate 3 re-audit. New attack vector to consider: a `duplicate-candidates` Lens entry that is fabricated by a direct KV write — defeated by the same Refractor-reprojection-overwrites mechanism Story 3.7 Vector #1 already covers.

**Estimate:** Opus, ~180K. Larger than 4.5 because it touches: Refractor (new lens), cypher executor (Levenshtein UDF), Processor (op deletions + narrow LinkScans), bootstrap (new lens seed + grant seed + removed permissions), substrate (adjacency reader), CLI (Story 6.1 partial), and the test suite (rewrite the 12 identity_merge_test.go tests against the new shape).

**Out of scope for 4.6:** PHI/PII normalization (still Phase 2); SplitIdentities (Phase 2); the operator-UI surface (read pattern is "read the KV"; UI is Loom-or-later).

**Implication for Epic 4 closure:** Epic 4 was declared closed at Story 4.5. After 4.6, Epic 4 is *architecturally* closed — until then, the closure is conditional ("works, but architecturally drifted; correction in 4.6"). I propose noting this in `phase-1-progress.md` as a status caveat rather than reopening Epic 4 numerically.

---

## D. Recommended sequence

If you accept this proposal, the work order is:

1. **NOW** (this session, post-approval, ~30 min of Winston time): execute C1 (tracker reconciliation), C2 (data-contracts §6.13 eviction + §1.4 absorption), C3 (epics.md sweep). Single commit per artifact group.
2. **Next sub-agent session** — Story 6.0 (component pages — Processor, Refractor, Substrate). Estimate ~30K Opus. Docs-only.
3. **Next sub-agent session** — Story 4.6 (Epic 4 realignment). Estimate ~180K Opus.
4. **Next sub-agent session** — Story 2.4 (morph completion + Lattice-native source). Estimate ~150K Opus. Can be split if budget pressure.
5. **Resume Epic 5** per the planned sequence (5.1 → 5.2 → 5.3).

Alternative ordering: if you want to defer the morph completion (Story 2.4) and Epic 4 realignment (Story 4.6) until after Epic 5 ships, the component pages (6.0) and in-this-session edits (C1/C2/C3) are still worth doing now — they are cheap and they unblock cleaner story briefs.

A third option: bundle 4.6 + 2.4 into a single "Phase 1 architectural correction" mega-story (~250K Opus). Risk: too big for a single session; can split mid-flight if necessary. Not recommended unless you have a budget constraint that forces it.

---

## E. What I am NOT proposing

- **No rework of Stories 4.2 / 4.3 / 4.4 / 4.5 individually.** Their shipped code stays runnable until 4.6 deletes the now-anti-pattern ops. No revert.
- **No change to Contracts #1–#5 or #7.** Only Contract #6 §6.13 (the Story-3.6 dump) and a small absorption into Contract #1 §1.4. Contracts #2 (Operation Envelope), #3 (MutationBatch + EventList), #4 (Idempotency Tracker), #5 (Health KV), #7 (Primordial Bootstrap) are untouched.
- **No change to the Capability Lens cypher** or its production wiring. Concern 6 verified clean.
- **No change to the openCypher full-engine selection or vendored grammar.** The dependency pin (`antlr4-go/antlr/v4 v4.13.1`, `jtejido/go-opencypher` grammar as of 2026-05-15) stays.
- **No change to the bypass suite or Gate 3 vectors.** They re-run as-is post-4.6 and post-2.4.

---

## F. Open questions for Andrew

1. **Component-page audience and location.** I proposed `_bmad-output/planning-artifacts/components/<name>.md`. Alternative: `docs/components/<name>.md` (closer to source). Or `internal/<component>/ARCHITECTURE.md` (next to code, but mixes audiences with the package's own doc.go). Your call.
2. **Story 4.6 vs deferring it to Phase 2.** Phase 1 ships with the anti-pattern intact if 4.6 is deferred. I lean strongly toward doing 4.6 in Phase 1 (architecture-integrity-of-Phase-1-closure argument). If you'd rather declare Phase 1 done at Epic 6 and run the realignment as a Phase 1.5 cleanup, that's a defensible call too.
3. **Story 2.4 splitting.** If you'd rather not have an Opus-150K story, split 2.4a (token eviction + comments — Sonnet ~90K) and 2.4b (control plane + lens source migration — Opus ~100K). Same total work; smaller per-session footprint.
4. **`materializer` in test-only files.** Several `*_test.go` files use `materializer.*` subject literals as fixtures of the legacy shape. Do those get cleaned up in 2.4 (consistency-wins) or left as morph-provenance tests (history-wins)? I lean cleanup but it's mostly cosmetic.
5. **`epics.md` sweep timing.** I propose doing it in this session. If you'd rather have Bob (the SM) or a separate review pass before the edit, that adds a round but is more cautious. C3 isn't unsafe — it's just text changes — so my default is to do it now.

---

## G. What I'll do once you respond

If you approve (or approve with modifications):
- I execute C1, C2, C3 inline in this session (small commits, gates skipped — they're docs-only edits).
- I author the Story 6.0 + Story 4.6 + Story 2.4 handoff briefs but DO NOT launch sub-agents until you give the go.
- I update `phase-1-progress.md`'s Upcoming Sequence to reflect the new ordering (4.6, 2.4, 6.0 ahead of 5.1).

If you reject or want major changes:
- I revise this proposal and we iterate before any artifacts change.

**Status: awaiting your direction.**
