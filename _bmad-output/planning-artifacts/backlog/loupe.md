# Backlog — Loupe (Stream 3): the operator console

Stream 3 = the Loupe console (`cmd/loupe`: Go handlers + `web/` UI). Pipeline: **PO review** files the
program → **Sally** (bmad-agent-ux-designer) produces the UX design → **Winston adjudicates**
(Andrew delegated design ratification for this program, 2026-07-02 — no 📐-awaiting-Andrew gate here) →
the **Loupe Steward** builds fires UX-then-FE. Index + cross-lane rules: [../backlog.md](../backlog.md);
row discipline: [lattice.md → "How this board works"](lattice.md) (lint-board covers this file).

**Lane boundaries.** Code scope is `cmd/loupe/**` (+ its tests). A needed platform primitive
(engine/op/substrate) or deploy/contract change routes per the cross-lane rules — file to
[lattice.md](lattice.md) and `🚧 blocked-on:` it (trivial established-pattern mirrors excepted).
**Concurrency:** this lane runs in PARALLEL with both other streams (Andrew, 2026-07-02) — it does NOT
take the shared build lock; Loupe fires serialize among themselves on `/tmp/lattice-loupe-build.lock`.

## Loupe 2.0 — "the map is the console" (the program)

PO review 2026-07-01 (Andrew session): eight disconnected tabs become one navigable console; System Map
is home; showcase the graph + lenses + event flow. UX design: Sally, in flight → `loupe-2-ux-design.md`.

| Item | What it is | Imp | Size | State |
|---|---|---|---|---|
| **L1 — Navigation shell, Health retired** | Hash-router, URL-addressable views; map becomes home and absorbs Health: alert strip (preserves `health.alerts.*` incl. stub-auth-active), bootstrap-absent→red, by-design-paused lenses stop yellowing the rollup. | ★★★ | M | 🚧 blocked-on: UX design (Sally, in flight) |
| **L2 — Component detail pages** | Click-through from every map node: instances (1..N — fixes the last-write-wins collapse), issues, per-component health events, control surface inline (Loom/Weaver/Refractor); the Control tab dissolves. | ★★★ | M–L | 🚧 blocked-on: UX design |
| **L3 — Lens detail pages** | Per lens: definition (meta.lens DDL), live state, control (validate/pause/resume/rebuild + delete-behind-confirm), and a CONTENTS browser of the projected read-model (nats_kv targets now; postgres targets via a new Loupe-side read-only seam). | ★★★ | L | 🚧 blocked-on: UX design |
| **L4 — Graph explorer** | Core KV becomes a navigable graph: link rows jump to the far vertex, every key-shaped string (incl. provenance `createdByOp`) is a link, breadcrumbs, type facets, visual ego-graph mode. | ★★★ | L | 🚧 blocked-on: UX design |
| **L5 — Live pulse** | core-events live tail (new SSE endpoint) animates map edges + a recent-activity feed (op → commit → events → engine reactions → lens re-projections). Durable history stays with the lattice-lane orchestration-history item. | ★★ | M–L | 🚧 blocked-on: UX design |
| **L6 — Packages first-class** | Package detail (installed entities/ops/lenses/permissions, linked into the graph explorer) + lifecycle actions install/upgrade/uninstall behind confirms (F-004 mechanics exist). | ★★ | M | 🚧 blocked-on: UX design |
| **L7 — Submit-Op follow-through** | An accepted op's consequences: committed keys as graph links, emitted events, which lenses re-projected; session op log. | ★★ | S–M | 🚧 blocked-on: UX design |

## Component maintenance

| Item | What it is | Imp | Size | State |
|---|---|---|---|---|
| **[Loupe] Static-UI serving (`go:embed web`) untested** | The embedded operator-UI mount has no coverage. | ★ | XS | 📋 |
| **[Loupe] Operator UI (`app.js`, 1142 LOC) has no automated coverage** | No JS test harness in the repo — standing one up is an architectural call; the Loupe 2.0 program multiplies the UI and leans on this landing first. | ★★ | L | 📐 awaiting-Andrew (dep-fork: adopt goja) · [design](../../implementation-artifacts/loupe-fe-test-strategy-design.md) |

## Parked

| Item | Why it's parked | Imp | Size | State |
|---|---|---|---|---|
| Loupe agent-activity console | The ops layer atop the live system map (Steward queue, L3 review queue, per-agent Health). Read-seam options rejected. The L1 map keeps its `#sysmap-console` mount reserved. | ★★★ | M | 🚧 Andrew-gated (shelved 2026-06-25; design retained, do not build) |

## PO notes (rotation memory — capped, dated one-liners)

- Cross-lane feeds: lens freshness (L3) ← lattice.md "silent lens-projection stall" (📐); durable event
  history (L5+) ← lattice.md "Loom/Weaver control-API surfacing" (📐).
- 2026-07-01 PO review (Andrew session) — filed L1–L7; found+fixed the control-plane lockout (see Done log).
- **Next:** adjudicate Sally's UX design, sequence L1 first, enable the Loupe Steward schedule.

## Done log — loupe (newest first)

One line per shipped item (`date · SHA · [tag] title`). Oldest roll to `archive/` past ~25.

- 2026-07-02 · `4b8743f` · [Loupe/deploy] Control planes restored for operator surfaces — `lattice.ctrl.>` grant (write-restriction lockout) + natsperm positive round-trip pin
