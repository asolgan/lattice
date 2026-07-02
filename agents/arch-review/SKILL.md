---
name: arch-review
description: Deep architecture review of Lattice — the whole system and every component, from purpose to documentation to contracts to code, fanned out over read-only sub-agents and synthesized into a findings report + filed board rows. Use when Andrew says "run the architecture review" or "/arch-review" (optionally scoped to one component).
---

# Arch-Review — the full-platform architecture & component audit

**Role:** Winston (bmad-architect). **Read-only analysis** — the deliverables are a report + filed board
rows, never fixes. Designed to run in a FRESH session: everything needed is named below.

## Ground truth (load before fanning out)

- Spine: `_bmad-output/planning-artifacts/lattice-architecture.md` (P1–P5 invariants, decisions,
  deferred-capabilities rubrics) · `docs/contracts/*` (FROZEN) · `docs/vendors.md` (pins + authorities).
- Per-component mandates: `docs/components/*.md`. Boards (live state): `_bmad-output/planning-artifacts/
  backlog/{lattice,verticals,loupe}.md` + Done logs/archives. Vision: `_bmad-output/brainstorming/
  brainstorming-session-2026-04-08.md` (charters + boundary contracts, e.g. Stream-2 :573) and the
  Obsidian vault `~/Documents/Obsidian Vault/Lattice/`.
- Components: **Processor** (internal/processor — commit path steps 1–8, sole Core-KV writer) ·
  **Substrate** (internal/substrate) · **Refractor** (internal/refractor — CDC materializer, auth plane;
  charter EXCLUDES event streams) · **Loom** · **Weaver** · **Bridge** · **object-store-manager** ·
  **Gateway** · **Bootstrap** · **Loupe** (cmd/loupe — own lane) · **the Chronicler** (designed
  2026-07-02, event-ledger materializer — design-only unless built since) · the packages tier
  (packages/* — identity/rbac/orchestration-base + verticals).

## Per-component protocol (one read-only sub-agent each; scoped arg = run just one)

For each component produce, with file:line evidence:
1. **Purpose vs reality** — the doc/charter mandate vs what the code does; flag scope creep and
   charter-boundary crossings (the founding brainstorm charter is the authority for boundaries).
2. **Doc staleness** — does `docs/components/<x>.md` describe the code as it IS (no history narration)?
   List concrete drift (mechanisms renamed/replaced, features shipped but undocumented, retired ones
   still described).
3. **Contract conformance** — for each contract section the component implements: build-to, drifted
   (code ≠ frozen text — cite both sides), or dormant (contract ahead of code, e.g. a mandated behavior
   never wired). Ratification BANNERS supersede design bodies — check builds against banners.
4. **Invariant honor roll** — P2 (ops-only writes), P5 (lens reads), key shapes (§1.1 link direction:
   later-arriving = source), fail-closed defaults on auth surfaces, no-new-engine-Core-KV-reads.
5. **Health & test posture** — coverage of failure/edge arms (not just %), Health-KV self-report
   honesty (no static false-green), flake debt.
6. **Verdict** — healthy / drifted / at-risk, plus the 1–3 highest-value corrections, each shaped as a
   fileable board row (Item · What · Imp · Size).

## Cross-cutting passes (after the fan-out, synthesis-level)

- **Seam audit**: every cross-component seam (CDC, events, control planes, Health KV, adjacency,
  capability KV) — one owner? both sides agree on the shape? banner-vs-build divergences?
- **Contract sweep**: any staged-uncommitted `docs/contracts/*` diffs and whose proposal each is;
  frozen text that no code honors; code behavior no contract records.
- **Read/write-path map**: confirm apps read only lens targets (P5 gate), Processor is the only
  Core-KV writer, sanctioned exceptions still bounded (Loom guard read; Health KV; Loupe inspector).

## Output

A single report at `docs/reviews/arch-review-<date>.md` (docs live in /docs): executive summary →
per-component verdicts → cross-cutting findings → a ranked corrections table. File the top findings as
board rows on the owning lane (maintenance section) — rows, not fixes. Verify every claim against the
pinned source before it enters the report (vendor claims against `go env GOMODCACHE` + docs/vendors.md).
Commit the report + rows (docs-only, direct to main); nothing else.
