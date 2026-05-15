---
title: Story 2.3 Implementation Handoff Brief
story: 2.3 — Refractor Pipeline Key-Shape Adaptation (Epic 2 hardening)
model_tier: Sonnet (locked)
token_budget: ~75K
session: Fresh implementation session
architecture_lead: Winston
date: 2026-05-15
predecessor: Story 2.2 gap analysis (Deviation 13 / Risk R1 / §2.5 of gap analysis)
---

# Story 2.3 — Refractor Pipeline Key-Shape Adaptation: Handoff Brief

## Your Role

You close the highest-priority OPEN carry from Story 2.1+2.1b: the **pipeline still parses Materializer's legacy `node_<label>_<id>` key shape**, which means Refractor cannot project any actual Lattice domain entity beyond meta-lenses. This is a story-sized fix per the gap analysis. After 2.3, Refractor can project real `vtx.<type>.<id>` and `vtx.<type>.<id>.<localName>` writes and Story 3.2 (Capability Lens activation) has the substrate it needs.

This is an Epic 2 HARDENING story — added between 2.2 and Epic 3 specifically because the gap analysis flagged Deviation 13 as Epic-3-blocking. It exists outside the original 31-story plan and uses fresh budget.

## 🔴 MANDATORY OPERATING RULES (READ FIRST)

**Pattern across Phase 1: sub-agents have self-reported tokens 30-50% under outer telemetry.** Story 2.2's Sonnet pass came in well (23% gap, under budget). Aim for the same.

- **At every checkpoint (every 8-10 tool calls OR after any deliverable OR after any file read >25KB):** send a "checkpoint message" with deliverables done, deliverables remaining, honest token estimate (lower bound, rounded UP).
- **Halt unconditionally if you estimate > 80K used** (5% over budget).

Other rules:
- **Model tier:** Sonnet only. Halt if Opus/Haiku.
- **No PRs.** Direct commit to `main` after Winston review.
- **Architecture binding:** `_bmad-output/planning-artifacts/data-contracts.md` is source of truth.
- **DO NOT silently edit planning artifacts.** Use `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` (append).
- **All KV/JetStream ops through `internal/substrate`.** This story specifically uses `substrate.ClassifyKey` and `substrate.ParseVertexKey` / `ParseAspectKey` / `ParseLinkKey`.
- **No git commits by you.** Winston + Andrew commit.
- **Token tracker:** add Row 2.3 at session close (this is a new row between 2.2 and 3.1) — HONEST estimate, round UP.
- **MORPH-DEVIATIONS.md:** mark Deviation 13 RESOLVED with a "Resolution:" subsection.

## What "Pipeline Key-Shape Adaptation" Actually Means

**Current state (Materializer carryover):**
- `internal/refractor/pipeline/pipeline.go:1086` defines `parseCoreKVKey(key string) (nodeLabel string, ok bool)` which expects keys of the form `node_<label>_<id>` (Materializer's legacy shape).
- Two call sites: `pipeline.go:578` and `pipeline.go:1026`. Both extract a "node label" from a key to drive projection routing logic.
- Tests in `pipeline_test.go` write fixtures like `node_agreement_a1` and assert on this shape (27 occurrences of `node_` in the test file).

**Required state (Lattice contract — data-contracts.md Contract #1 §1.5):**
- Vertex: `vtx.<type>.<id>` (3 segments) — e.g., `vtx.contract.Hj4kPmRtw9nbCxz5vQ2y`
- Aspect: `vtx.<type>.<id>.<localName>` (4 segments) — e.g., `vtx.contract.Hj4kPmRtw9nbCxz5vQ2y.signedAt`
- Link: `lnk.<type>.<id>.<localName>.<type>.<id>` (6 segments)
- Classification authority: `substrate.ClassifyKey` (already exists, used by Processor step 6 from Story 1.7). Returns `KindVertex` / `KindAspect` / `KindLink` / `KindUnknown` per segment count + structural validation.

**Mapping**: the legacy `nodeLabel` is the new `<type>` segment. A document at `node_agreement_a1` becomes a document at `vtx.agreement.<NanoID>` with `class: "agreement"` (or whatever the DDL prescribes).

## Architectural Decisions Already Made (Winston)

1. **Replace `parseCoreKVKey` entirely.** Don't keep it as a fallback. Lattice's contract is the only supported shape going forward.

2. **Use `substrate.ClassifyKey` for the kind discrimination.** Don't roll your own segment-counting; substrate already validates type segments and NanoID shape correctly.

3. **For vertex-routing call sites (the two existing usages):** replace `parseCoreKVKey(key)` with `substrate.ParseVertexKey(key)` which returns `(vertexType, id, ok)`. The `vertexType` IS the new "label" semantically. If `ParseVertexKey` returns `ok=false`, the key isn't a vertex — log + skip (this is how non-vertex keys like aspects or links are filtered when the projection only operates on vertices).

4. **Aspect awareness: investigate whether the pipeline currently composes property reads from aspect keys.** Materializer kept all node properties in the value document at the node key. Lattice splits properties into aspects (`vtx.<type>.<id>.<localName>`). Two scenarios:
   - **(a) Pipeline already reads aspects:** then nothing structural to change — just the key shape parsing.
   - **(b) Pipeline expects all properties in one document:** then projection of a Lattice domain entity requires composing reads across vertex + aspects. This is a real semantic gap.

   **Investigate this in Phase A before changing anything.** If (b), DO NOT add the composition logic in 2.3 — escalate to Winston with the size estimate. 2.3's contract is "match Lattice's key shape," not "implement multi-document property composition." If composition work is needed, it becomes a follow-up story (2.4) with its own budget.

5. **Aspect handling at the pipeline boundary:**
   - If `substrate.ClassifyKey(key) == KindAspect`: the pipeline either (a) routes this to a known aspect handler, OR (b) logs+skips it. For 2.3's minimum viable scope, log+skip is acceptable — but you MUST add a structured log line at INFO level showing "aspect mutation observed but no handler registered" with the key and parent vertex key. This gives observability for the next stage of work.
   - If `substrate.ClassifyKey(key) == KindLink`: same treatment — log+skip with a marker, no panic.
   - If `KindUnknown`: log at WARN with the bad key — this is a defect signal.

6. **Test migration scope:** the 27 `node_<label>_<id>` fixtures in `pipeline_test.go` get migrated to `vtx.<type>.<NanoID>` shape. Use a fixed sentinel NanoID convention (e.g., the existing primordial-ID pattern in `internal/bootstrap/primordial.go`) so fixtures stay deterministic. Test SEMANTIC intent must be preserved — if a test was asserting "a write to an `agreement` node with id `a1` produces projection X", the migrated test asserts the same semantic outcome on `vtx.agreement.<sentinel-id-1>`.

7. **AC #10 e2e test (Story 2.1b's `internal/refractor/refractor_e2e_test.go`) must continue to pass.** That test already uses Lattice key shapes — confirm it still passes after your changes. If it fails because your pipeline changes break something, fix the cause, not the test.

8. **No new substrate primitives expected.** Everything you need (`ClassifyKey`, `ParseVertexKey`, `ParseAspectKey`, `ParseLinkKey`) already exists.

9. **No Contract amendments expected.** If you find one, append to `cmd/refractor/CONTRACT-AMENDMENT-REQUEST.md` and escalate before resolving.

10. **CI gate:** `.github/workflows/ci.yml` is active. After your changes, CI must go green. Run locally before the closing summary: `make verify-bootstrap`, `make test-bypass`, and the full `go test ./... -p 1 -count=1`. The `-p 1` is current Phase 1 reality (Deviation 14).

## Required Context — Read These Only

| File | Why |
|---|---|
| `internal/refractor/pipeline/pipeline.go` (whole file — 1,092 lines, but read selectively: lines 570-600 + lines 1020-1095 cover both call sites + the helper) | Where the work happens |
| `internal/refractor/pipeline/pipeline_test.go` (skim for `node_` occurrences via grep; read fixture setup helpers in full) | Test surface to migrate |
| `internal/substrate/keys.go` (lines 60-180 cover ClassifyKey + the parsers) | The authoritative replacement helpers |
| `_bmad-output/planning-artifacts/data-contracts.md` Contract #1 §1.5 (key patterns) + §1.6 (envelope, especially the `class` field) | Source of truth |
| `_bmad-output/planning-artifacts/refractor-gap-analysis.md` §2.5 + §5 R1 + §4 Deviation 13 | Why this story exists |
| `_bmad-output/planning-artifacts/MORPH-DEVIATIONS.md` Deviation 13 entry | What to mark RESOLVED |
| `internal/refractor/refractor_e2e_test.go` (skim) | The downstream e2e that must continue to pass |

**DO NOT read** the morph plan, the full epics.md, or other Refractor packages unless a specific question pushes you there. The brief is self-contained.

## Suggested Sequence

**Phase A — Investigation (≤ 10K tokens):**
1. Read pipeline.go around both call sites (570-600 + 1020-1095) — understand what the pipeline does with the parsed label
2. Search pipeline.go for aspect-related reads (`.canonicalName` / aspect handling / property composition)
3. Send a "Phase A checkpoint" stating which of Decision #4 scenarios (a) or (b) applies — if (b), HALT and escalate; otherwise proceed

**Phase B — Helper replacement (≤ 15K tokens):**
4. Delete `parseCoreKVKey` from pipeline.go
5. Replace both call sites with `substrate.ParseVertexKey` (and add the aspect/link/unknown log-and-skip handlers per Decision #5)
6. Verify `go build ./...` green

**Phase C — Test migration (≤ 30K tokens):**
7. Migrate the 27 `node_<label>_<id>` test fixtures to `vtx.<type>.<sentinel-id>` shape; preserve semantic intent
8. Verify `go test ./internal/refractor/pipeline/... -count=1` green

**Phase D — Regression + gates (≤ 10K tokens):**
9. Run `go test ./... -count=1 -p 1` — full suite green
10. Run `make verify-bootstrap` and `make test-bypass` — green
11. Verify `internal/refractor/refractor_e2e_test.go` still passes (it should — it uses Lattice keys natively)

**Phase E — Wrap (≤ 5K tokens):**
12. Mark Deviation 13 RESOLVED in MORPH-DEVIATIONS.md
13. Update token tracker (add Row 2.3) — round UP
14. Closing summary

## Deliverables Checklist

1. ✅ Phase-A investigation checkpoint sent (which Decision #4 scenario applies)
2. ✅ `parseCoreKVKey` removed from `pipeline.go`
3. ✅ Both call sites use `substrate.ParseVertexKey` (or appropriate substrate helper)
4. ✅ Aspect / link / unknown key kinds handled per Decision #5 (log + skip + observability marker)
5. ✅ All 27 test fixtures migrated to `vtx.<type>.<NanoID>` shape with semantic intent preserved
6. ✅ `go build ./...`, `go vet ./...`, `go test ./... -p 1 -count=1` exit 0
7. ✅ `make verify-bootstrap` green
8. ✅ `make test-bypass` green
9. ✅ `internal/refractor/refractor_e2e_test.go` still passes (regression check)
10. ✅ MORPH-DEVIATIONS.md Deviation 13 marked RESOLVED with resolution subsection
11. ✅ Token tracker Row 2.3 added — HONEST estimate, round UP
12. ✅ Closing summary

## What Story 2.3 Is NOT

- **Not** multi-document property composition across vertex + aspects (Decision #4 escalation if needed)
- **Not** new aspect/link projection handlers (just observability markers — log + skip)
- **Not** a substrate API extension
- **Not** any DDL change, processor change, or bootstrap change
- **Not** an Epic 3 story (this is Epic 2 hardening)

## Escalation

Halt and escalate via Andrew if:
- Decision #4 scenario (b) applies — pipeline composes properties across documents and the morph would require new composition logic
- Replacing `parseCoreKVKey` reveals a structural pipeline assumption that can't be cleanly substituted with `substrate.ParseVertexKey`
- A test's semantic intent isn't preservable under Lattice key shape (e.g., a test assumes a key shape that doesn't exist in Lattice)
- Token estimate exceeds 80K
- A CONTRACT-AMENDMENT-REQUEST emerges

## Closing

1. Verify all 12 deliverables
2. Run `make down && make up && make verify-bootstrap && make test-bypass && go test ./... -p 1 -count=1` — all green
3. Update token tracker (new Row 2.3) — round UP
4. Closing summary: deliverables status, Decision #4 outcome (which scenario applied), test count migrated, any observability markers that fired during e2e, token estimate (honest)

Do NOT commit. Winston + Andrew review and commit.
