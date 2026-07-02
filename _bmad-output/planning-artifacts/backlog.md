# Lattice Backlog & Roadmap (Phase 3+) — index

**Owner:** Andrew (architect / planning lead). **Status:** living document.

Work is tracked in **two swim-lane files**, advanced by **two parallel streams** (split along the
no-collision seam: app-vertical code vs platform code). Design + rationale:
`implementation-artifacts/agentic-ops-swimlanes-design.md`.

- **[backlog/verticals.md](backlog/verticals.md)** — App Verticals (LoftSpace/Clinic packages + FE). Stream:
  **Vertical Steward** + **Vertical PO**.
- **[backlog/lattice.md](backlog/lattice.md)** — Lattice features + component maintenance. Stream: **Lattice
  Steward** (round-robin across components) + **Surveyor** + **Whetstone** (CI).
- **[backlog/loupe.md](backlog/loupe.md)** — the Loupe console (Loupe 2.0 program, `cmd/loupe/**`). Stream:
  **Loupe Steward** (UX-then-FE) — runs in parallel with BOTH streams above (own build lock, not the shared
  one; Andrew 2026-07-02).

This index keeps the **scales**, the **cross-lane rules**, and the **shipped milestones**. Per-lane
*ready / in-flight / done* lives in the two lane files (each stream writes only its own file → the
single-file board-collision races go away).

**The board is an INDEX, not a journal — the row discipline.** One item = one row: `Item · What (one
line) · Imp · Size · State`. The **State** cell is a state token + a link to the design doc / commit + (if
🏗️) a one-line next step — **nothing else**. Detail (design, ratification, adversarial findings, the
fire-by-fire build journal, commit SHAs, coverage) lives in the **linked design doc + git**, never narrated
in the row (the CLAUDE.md no-changelog-comments rule, applied to the board). Shipped items collapse to a
one-line Done-log entry. The canonical statement of this is
[backlog/lattice.md → "How this board works"](backlog/lattice.md).

**Scales.** Importance: ★ low · ★★ medium · ★★★ high. Size: XS · S · M · L · XL.
**State tokens.** 📋 ready · 🏗️ building (worktree) · 📐 awaiting-Andrew (design ratification) ·
✅ ratified (signed off, not yet built) · 🚧 blocked (Andrew-gated, or `seq:`/`blocked-on:` another item) ·
🎯 top-priority pick · 🗄️ shelved-backup · 🔭 flag-for-Andrew.

## Cross-lane rules

- **No-paper-over (Andrew, 2026-06-26).** The App-Verticals stream **never** substitutes a workaround for a
  missing Lattice capability and calls the item done. A discovered platform gap is filed to **lattice.md** as
  demand (tagged with the requesting vertical + why) and the vertical item is **`🚧 blocked-on:`** it — or
  ships only its non-gap part. A vertical builds *on* real Lattice capabilities; faking one hides the gap and
  stalls Lattice. (Boundary vs **P5**: a missing **lens** is *not* a Lattice gap — adding it is **package
  work** the vertical does itself; only a missing platform **primitive** — engine / op / substrate /
  orchestration — routes to lattice.md.)
- **The PO drives Lattice demand too.** Exercising the verticals is the primary, grounded source of "what the
  platform actually lacks" — those gaps land in lattice.md, not worked around in verticals.
- **Prioritization.** Each stream picks by **importance × readiness**; the Lattice stream **round-robins across
  components** so none stalls. Reliability/observability **red pre-empts** either stream. The experience layer
  is **not** a forced priority (the apps largely exist).
- **Frozen contracts are prepare-not-skip.** A needed `docs/contracts/*` change is never a reason to skip an
  item: make the edit in `main` **uncommitted** + flag it; Andrew ratifies (commits). Only a *standing* Andrew
  block/shelve is a true leave-it.
- **Code in worktrees; docs in `main`.** Code changes build in an isolated git **worktree** (commit + push to
  main, no PR). The **backlog / lane files, design docs, and contracts** are edited **directly in `main`**
  (contracts uncommitted). Always scoped `git add <paths>`, `git pull --rebase`, detect-reuse stack.

---

## Now — balanced prioritization (experience layer no longer forced)

The initial experience-layer push has largely landed: the vertical apps now **exist** (`cmd/loftspace-app`
`:7788`, `cmd/clinic-app` `:7799`) with real flows, and the Loupe live system-map shipped. So the experience
layer is **no longer a forced top priority** (Andrew, 2026-06-25) — the Steward picks by
**importance × readiness**, balancing the experience layer against reliability / observability, component
coverage, and the **PO-filed demand backlog**. Flow for any UI/app pick: **PO scopes → Sally designs the UX
→ FE Engineer builds + verifies in-browser → Winston admits.** M/L is fine (risk-bounded L2 + multi-fire).

---

## Shipped milestones

The big landmarks. **Per-item shipped history lives in each lane's Done log**
([lattice](backlog/lattice.md#done-log--lattice-newest-first) ·
[verticals](backlog/verticals.md#done-log--verticals-newest-first)) + git — not duplicated here.

- **Phase 2 complete** (Epics 7–14 + 13.5): orchestration tier (Loom + Weaver + Refractor + the external-I/O
  bridge) + the Loftspace lease-application reference vertical. CI green.
- **Loupe v1** — the view-&-control app (`cmd/loupe`): live system-map, Core-KV inspector, package
  install/uninstall, control-plane proxy, task inbox, Files. Plus its enablers: **Loom control plane**,
  **large-file/binary** handling (Object Store v1a + v1b GC), **Refractor substrate migration**.
- **Vertical apps** — **LoftSpace applicant + landlord** FEs and **Clinic** booking/schedule/encounter FEs,
  both demoable on a clean stack (`make up-loftspace` / `up-clinic`).
- **Read-path auth (D1)** in flight — D1.1 base lens, D1.2 JWT seam, D1.3 protected-Postgres + RLS read
  boundary (applicant + landlord), D1.4 fail-closed activation guard.
- **Platform features** — F-004 package upgrade / DDL hot-reload, async external-reply, structured adapter
  result, service-location authZ, `instanceOf` op-discovery, Processor commit-OCC, the CI matrix split.

---

## Done / moot — *not backlog*

- **Per-lens delete mode (Story 1.5.12)** — built; `deleteMode` (default hard) is in use across the
  task / ephemeral lenses.
- **§10.8 nudge-`operation` CAR** — moot: 13.5 retired the nudge GapAction; external remediation is now
  `triggerLoom` of an `externalTask` via the bridge.
- **Capability-Lens god-cypher → contract-contribution** — resolved (Epic 12).

---

*Consolidates and supersedes: `lattice-architecture.md` "Open Items (Phase 3+)" (OI-1 async adapters /
OI-2 large files carried here), `epics/index.md` "Phase 2+ Deferred Architectural Capabilities", and the
per-component "Deferred (Phase 3+)" sections.*
