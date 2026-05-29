# Story 1.5.2 — DDL tombstone coherence (M6): Adversarial CR Report

**Reviewer:** CR sub-agent · **Date:** 2026-05-29 · **Mode:** review-only (no edits/commits)
**Scope:** `internal/bootstrap/meta_ddl.go` (Tombstone branch), `internal/processor/ddl_cache.go` (`loadMetaVertex`), `internal/processor/step8_commit.go` (invalidation dedup), tests (`tombstone_metavertex_test.go`, `ddl_cache_test.go`, `gate4_rollback_test.go`), `docs/components/processor.md`.

---

## Triage summary

| ID | Sev | Area | One-liner |
|---|---|---|---|
| F-1 | **P1** | Cascade completeness | Primordial lens has aspects `targetBucket/cypherRule/outputSchema` (no `description`/`compensation`); cascade orphans the first three and writes two spurious tombstones for never-existent keys. Reachable, unguarded. |
| — | clean | DDL aspect set | DDL cascade set matches the Create-branch 9-aspect write exactly. No finding. |
| — | clean | Cache eviction | `isDeleted` early-return correctly positioned; proven end-to-end. No finding. |
| — | clean | step8 dedup | 3-segment root collapse correct; `< 3` skip safe. No finding. |
| — | clean | expectedRevision | Lands on `mutations[0]` (root tombstone). No finding. |
| — | clean | gate4 reconciliation | Assertion correct; imports present; passes. No finding. |
| — | clean | other `.compensation` readers | Only aiagent reads it from Core KV; correctly handles tombstone. No finding. |
| F-2 | **P2** | Doc accuracy | Lens cascade table omits the primordial-lens aspect reality (ties to F-1). |

**Bottom line:** Both LOCKED deliverables (cascade + cache eviction) are implemented correctly *for DDL classes and DDL-created lenses*. All §5 targeted gates I ran are green. The one material gap is the **primordial-lens cascade mismatch (F-1)** — a divergence between the LOCKED lens aspect set `[.canonicalName,.description,.spec,.compensation]` and the actual primordial-lens aspect set `[.canonicalName,.targetBucket,.cypherRule,.outputSchema,.spec]`. It is a real, reachable coherence hole, but its blast radius in Phase 1 is narrow (only the two seeded capability lenses, with no Phase-1 GC consumer of the orphaned keys). I rate it P1 and recommend option (c) guard, or at minimum (a) explicit documented residual.

---

## F-1 (P1) — Primordial-lens cascade orphans 3 aspects and writes 2 spurious tombstones

**Where:**
- `internal/bootstrap/meta_ddl.go:387-393` (lens aspect set in the Tombstone branch).
- vs `internal/bootstrap/primordial.go:660-704` (`addLensAspects`).
- vs `internal/bootstrap/meta_ddl.go:196-208` (CreateMetaVertex meta.lens branch).

**What.** There are **two distinct kinds of `meta.lens` vertex** with **different aspect key sets**:

| Lens origin | Aspect keys actually present in Core KV |
|---|---|
| DDL-created (`CreateMetaVertex` meta.lens) | `.canonicalName`, `.description`, `.spec`, `.compensation` |
| **Primordial** (`addLensAspects`, the two seeded capability lenses) | `.canonicalName`, `.targetBucket`, `.cypherRule`, `.outputSchema`, `.spec` |

The cascade's `is_lens` branch tombstones exactly `[.canonicalName, .description, .spec, .compensation]` — the **DDL-created** set. For a **primordial** lens (`vtx.meta.<CapabilityLensID>` / `<CapabilityRoleIndexLensID>`, keys defined at `internal/bootstrap/nanoid.go:366-367`) this is wrong in two directions:

1. **Orphans 3 live aspects:** `.targetBucket`, `.cypherRule`, `.outputSchema` are never tombstoned and stay live forever after the root is deleted — the exact orphaned-aspect/dead-root incoherence this story exists to close (F-007), just relocated to primordial lenses.
2. **Writes 2 spurious tombstones for keys that never existed:** `.description` and `.compensation`. Confirmed in `step8_commit.go:237-273` `buildMutationValue` — the `"tombstone"` case unconditionally marshals `{key, isDeleted:true, lastModified*}` and the BatchOp (`step8_commit.go:119-136`) for tombstone is **unconditioned** when no `expectedRevision` is set, so `AtomicBatch` happily *creates a brand-new deleted entry* at `vtx.meta.<lens>.description` and `…​.compensation` even though no such key existed. So `make_tombstone` on an absent key **does** create a deleted entry — directly answering the question in the brief.

**Reachability.** The flow is reachable and unguarded. There is **no protection** anywhere against tombstoning a primordial entity: I grepped `internal/processor/*.go` and `internal/bootstrap/meta_ddl.go` for any `MetaRootKey`/`CapabilityLensKey`/`protected`/`immutable` guard — none exists. An operator (or a buggy/malicious agent on the meta lane) submitting `TombstoneMetaVertex{metaKey: "vtx.meta.<CapabilityLensID>"}` passes the `vertex_alive` guard (the primordial root is live) and runs the cascade. The same hole also applies to the **primordial root DDL** at `MetaRootKey` (a `meta.ddl.*` class — its DDL aspect set largely matches, so less acute, but tombstoning the bootstrap kernel root is catastrophic and equally unguarded).

**Why it matters.** Tombstoning the capability lens decouples Refractor's `CoreKVSource` activation watch from a now-half-deleted definition: root + spec dead, but `targetBucket/cypherRule/outputSchema` live. That is precisely the inconsistent state the story forbids, and there is no GC path (story §4 explicitly defers the sweep). The spurious `.description`/`.compensation` deleted entries are lower-harm (readers like `ReadCompensation` treat `isDeleted:true` as absent, traversal.go:268) but they are litter and could confuse a future operator/diagnostic.

**Severity rationale (why P1 not P0).** Blast radius is narrow in Phase 1: only the two seeded capability lenses are primordial, routing installs through the Processor are Story 1.5.5 (so operator-driven lens tombstones are not a live Phase-1 workflow yet), and no Phase-1 consumer GC's the orphaned keys. It is not a correctness break of the *tested* paths. But it is a genuine, reachable coherence hole against the story's own thesis, on a kernel-critical entity, with zero guard — too material to ship silently as "clean."

**Recommendation (pick one; (c) preferred):**
- **(c) Guard — preferred.** Reject `TombstoneMetaVertex` on primordial keys in the Starlark branch (the kernel root + the two capability lenses are never legitimately tombstoned in Phase 1). A short deny-list check (`fail("Protected: cannot tombstone primordial meta-vertex " + meta_key)`) closes both the orphan and the kernel-root foot-gun in ~3 lines, and is squarely inside the confined `TombstoneMetaVertex` branch. This is the cleanest fit because it sidesteps the two-kinds-of-lens problem entirely rather than trying to enumerate a union.
- **(b) Union the lens set** to `[.canonicalName,.description,.spec,.compensation,.targetBucket,.cypherRule,.outputSchema]`. This stops the orphan but *increases* spurious-tombstone litter for DDL-created lenses (which lack `targetBucket/cypherRule/outputSchema`) and for primordial lenses (which lack `description/compensation`). Net: trades one orphan for more spurious deleted entries. Not recommended alone.
- **(a) Accept + document** as a known residual *only if* Winston confirms primordial-entity tombstone is out-of-scope/unsupported for Phase 1. If chosen, the residual note and the doc table (F-2) must say so explicitly. This is the minimum acceptable disposition; it leaves the unguarded kernel-root foot-gun in place, which I'd flag as undesirable.

This decision is a contract/scope call that belongs to Winston — it touches "is tombstoning a primordial meta-vertex a supported flow," which §3 does not address. Recommend appending to the CAR if (c)/(b) is chosen, since it expands behavior beyond the LOCKED aspect lists.

---

## Cleared concerns (explicit no-finding)

**2. DDL aspect-set completeness — CLEAN.** The DDL cascade set (`meta_ddl.go:390-393`) is `.canonicalName, .permittedCommands, .description, .script, .inputSchema, .outputSchema, .fieldDescription, .examples, .compensation` = 9 aspects. Cross-checked against the primordial DDL writer (`primordial.go:505-657`, `addSelfDescriptionAspects`) and the `CreateMetaVertex` DDL branch: these are exactly the self-description aspects a DDL class carries plus `.compensation`. The DDL-created lens Create branch (meta_ddl.go:196-208) writes exactly the 4 the lens cascade set covers. Match confirmed; `TestTombstoneMetaVertex_DDLCascadesAllAspects` asserts the full 10-mutation set (root + 9).

**3. Cache eviction — CLEAN.** `ddl_cache.go:160-172`: `IsDeleted` is unmarshaled from the root entry, and the `if rootDoc.IsDeleted { return ref, false, nil }` early-return is placed **after** `json.Unmarshal` and **before** `ref.Kind = deriveDDLKind(...)` and any shadow-key/canonicalName aspect read — exactly as §3.2 mandates. `Invalidate` re-runs `loadMetaVertex`, so a `false` return removes from both `byName` and `byMetaPK`. No path resolves a tombstoned vertex: `Lookup`/`LookupByMetaKey` read the maps that `Invalidate` just cleared. `TestDDLCache_Invalidate_EvictsTombstonedRoot` proves the live→tombstone→evicted transition end-to-end across both indexes; `TestDDLCache_LoadMetaVertex_TombstonedRootAbsent` proves eviction precedes name resolution (seeds a live `.canonicalName` aspect alongside the dead root and asserts absent). Both green.

**4. step8 dedup — CLEAN.** `step8_commit.go:165-187`. Root collapse: `strings.Join(strings.Split(m.Key,".")[:3],".")`. Every `vtx.meta.*`-prefixed key has ≥3 segments (`vtx`,`meta`,`<id>`); NanoIDs are dot-free (`nanoid.go`), so `[:3]` always yields the true root `vtx.meta.<id>` for both root and aspect keys. The `len(parts) < 3` skip can only fire for a degenerate `vtx.meta.` (empty id) key that the system never emits — harmless. Dedup set (`seen`) makes each root invalidated once. `Invalidate` is idempotent regardless, so correctness is preserved even if the dedup were wrong. No mutation key mis-collapses.

**5. expectedRevision — CLEAN.** `meta_ddl.go:400-412`: `mutations = [make_tombstone(meta_key)]` first, then aspects appended in the loop, so `mutations[0]` is unambiguously the **root** tombstone. `mutations[0]["expectedRevision"] = expected_rev` (only when present and not `force`). `TestTombstoneMetaVertex_ExpectedRevisionOnRootOnly` asserts `mutations[0].Key == root`, root carries rev 11, every `mutations[1:]` aspect has nil rev, `force` clears it, and non-integer is rejected. Matches §3.1.

**6. gate4 reconciliation — CLEAN.** `gate4_rollback_test.go:126-135`: the MF-2/AC3 assertion now expects `errors.Is(err, aiagent.ErrCompensationAspectMissing)` after tombstone, which is the coherent post-cascade state. `traversal.go:268-269` confirms `ReadCompensation` maps `isDeleted:true` → `ErrCompensationAspectMissing` (already true before this story per the CAR). Imports present: `"errors"` (line 33), `aiagent` package import (line 39). `go test ./internal/aiagent/ -run Gate4` passes. The CAR's reconciliation is sound and the assertion is correct. (Note: the file is outside the story's stated confine set, but the change is a pure test-assertion update with no production aiagent change — acceptable, and Winston has ratified the `.compensation`-tombstone contract.)

**7. Other post-tombstone `.compensation` readers — CLEAN.** Grepped all non-test Go for `.compensation` Core-KV reads. Only `internal/aiagent/traversal.go` (`ReadCompensation`) reads it, and it correctly returns `ErrCompensationAspectMissing` on `isDeleted:true`. Other refs are non-readers: `meta_ddl.go` (writers/comments), `primordial.go:170,387-389` (seeding + a comment explicitly stating "the Processor reads NO compensation"), `nanoid.go:53-55` (the class-name constant). The Processor's own commit/dedup paths do not read `.compensation`. No reader is broken by tombstoning it.

---

## F-2 (P2) — Doc lens-cascade table is incomplete/misleading re: primordial lenses

**Where:** `docs/components/processor.md:297-300` (cascade table).

**What.** The table lists the `meta.lens` cascade as `.canonicalName, .description, .spec, .compensation` with no acknowledgment that primordial lenses carry `.targetBucket/.cypherRule/.outputSchema` and lack `.description/.compensation`. A reader would conclude the cascade fully cleans any lens, which is false for the two seeded capability lenses (see F-1).

**Why.** Doc currently overstates coherence for the lens path.

**Fix.** Tie to the F-1 disposition: if (c) guard lands, add a line "primordial meta-vertices (kernel root DDL, capability lenses) cannot be tombstoned." If (a) accept, add an explicit residual: "the lens aspect set matches DDL-created lenses; primordial lenses additionally carry `.targetBucket/.cypherRule/.outputSchema`, which this cascade does not reach — primordial-lens tombstone is unsupported in Phase 1."

Otherwise the doc (cascade mechanics, root-only revision, eviction, step-8 dedup) is accurate and matches the implementation.

---

## Gates run (this review)

```
go build ./...                                   → OK (no output)
go test ./internal/bootstrap/... -run Tombstone  → ok 0.460s
go test ./internal/processor/ -run DDLCache -p 1 → ok 0.622s
go test ./internal/aiagent/ -run Gate4           → ok 1.638s
go vet ./internal/aiagent/                       → clean
```

Per-package green confirmed for the touched packages; CI on the clean stack is the authoritative final gate (full-suite + integration not re-run here).

---

## Verdict

Implementation correctly delivers the two LOCKED items for DDL classes and DDL-created lenses, with solid tests and accurate docs for those paths. **One P1** (primordial-lens cascade mismatch + unguarded primordial-entity tombstone) needs a Winston disposition before merge — recommend guarding primordial entities (option c). **One P2** doc follow-up tied to that disposition. Everything else is clean.
