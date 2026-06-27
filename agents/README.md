# agents/ — canonical agentic-ops role-skill definitions

Version-controlled source of truth for the **Agentic Operating Model** role-skills. Design docs:

- **Execution model:** [`agentic-ops-swimlanes-design.md`](../_bmad-output/implementation-artifacts/agentic-ops-swimlanes-design.md)
  — two parallel streams, per-lane backlog, bounded budget-blind fires (current).
- **Org / roles / flywheel:** [`agentic-ops-design.md`](../_bmad-output/implementation-artifacts/agentic-ops-design.md)
  (its §5/§6.1.1 execution detail is superseded by the swimlanes doc).

## How these are used

- **Unattended scheduled fires read `agents/<role>/SKILL.md` directly** from the working tree — no install
  step per fire. (The fleet is below.)
- For invoking a role as `/​<role>` in a human session, the harness discovers skills under `.claude/skills/`,
  which is gitignored — so install the canonical copies with:

  ```
  make install-skills
  ```

**Edit the copies under `agents/`**, then re-run `make install-skills` if you want the `/role` form refreshed.
Do not edit `.claude/skills/<role>/` directly — those are install artifacts and get overwritten.

## The model in one breath

Two **parallel streams** split along the no-collision code seam — **Verticals** (apps: package + FE) ∥
**Lattice** (platform features + component maintenance, round-robin across components). Each stream has an
**advancer** (builds, commits at L2) fed by a **hydrator** (files scored demand, L0/L1). Code builds in
worktrees; docs (the backlog, design docs, contracts) are edited directly in `main` (contracts uncommitted).
Fires are **bounded** — no budget guessing; the rate-limiter governs.

## Roles

| Role | Function | What it does |
|---|---|---|
| `steward/` | **Advancer** (L1→L2) | Stream-parameterized (the caller names **Verticals** *or* **Lattice**): sense the lane file → select (verticals: importance×readiness; lattice: round-robin across components) → activate the owner/FE → admit/commit a **bounded batch** → exit. |
| `vertical-po/` | **Hydrator** — Verticals demand (L0/L1) | Exercise a vertical's app/packages on a running stack, file scored items to `backlog/verticals.md`, and **route discovered platform-*primitive* gaps to `backlog/lattice.md`**. Never builds. |
| `surveyor/` | **Hydrator** — Lattice demand (L0/L1) | Survey a component / the deferred-capabilities backlog (round-robin) + Health/CI signals → file scored, ready items to `backlog/lattice.md`. The platform analog of the PO; never builds. |
| `owner/` | **Builder** (invoked by an advancer) | Advance one component **or** package by one unit via the hardened story loop (ground → design → dev → review → gates). Code in a worktree; docs in `main`. |
| `fe-engineer/` | **Builder** (invoked by an advancer) | Build web front-ends from a UX design — **Loupe's operator UI *and* the vertical apps** — vanilla HTML/CSS/JS + Go handlers; verifies in-browser. Reads lens projections (P5), never Core KV (Loupe excepted). |
| `lamplighter/` | **Cross-cutting ops** (L0/L1) | Observability watch — read Health KV → classify anomalies → surface remediation candidates. Never silently fixes. |

The **UX Designer (Sally)** is the bmad skill **`bmad-agent-ux-designer`** (not tracked here); she designs the
experience, the FE Engineer builds it (UX-then-FE).

## The scheduled fleet

| Task | Role (stream) | Cadence |
|---|---|---|
| `steward-autonomous` | `steward` — **Lattice** advancer | even hours (`0 */2`) |
| `steward-verticals` | `steward` — **Verticals** advancer | odd hours (`0 1-23/2`), interleaved |
| `platform-surveyor` | `surveyor` — Lattice hydrator | 3×/day (`0 7,15,23`) |
| `vertical-po-discovery` | `vertical-po` — Verticals hydrator | 3×/day (`0 5,13,21`) |

`owner`, `fe-engineer`, and `lamplighter` are **invoked by** the advancers (or run directly), not scheduled on
their own. The bmad tooling skills stay local and are intentionally not tracked here — this directory is only
the agentic-ops roles.
