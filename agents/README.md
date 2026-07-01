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
**advancer** (builds, commits at L2) fed by a **hydrator** (files scored demand, L0/L1); the Lattice stream
additionally has a **Designer** (architect) that deepens *readiness* — turning filed demand into build-ready
designs ahead of the advancer — and a cross-cutting **Whetstone** that keeps CI fast. Code builds in worktrees;
docs (the backlog, design docs, contracts) are edited directly in `main` (contracts uncommitted). Fires are
**bounded** — no budget guessing; the rate-limiter governs.

## Roles

| Role | Function | What it does |
|---|---|---|
| `steward/` | **Advancer** (L1→L2) | Stream-parameterized (the caller names **Verticals** *or* **Lattice**): sense the lane file → select (verticals: importance×readiness; lattice: round-robin across components) → activate the owner/FE → admit/commit a **bounded batch** → exit. |
| `vertical-po/` | **Hydrator** — Verticals demand (L0/L1) | Exercise a vertical's app/packages on a running stack, file scored items to `backlog/verticals.md`, and **route discovered platform-*primitive* gaps to `backlog/lattice.md`**. Never builds. |
| `surveyor/` | **Hydrator** — Lattice demand (L0/L1) | Survey a component / the feature backlog (round-robin) + Health/CI signals → file scored, ready items to `backlog/lattice.md`. The platform analog of the PO; never builds. |
| `designer/` | **Readiness** — Lattice design (L0/L1) | Winston-as-architect: take a Lattice backlog item (almost all need design), ground hard in the architecture + vision/vault, produce a design doc ahead of the Steward, and **flag it for Andrew to ratify** (forks designed-through + explained; contract edits prepared uncommitted). Never builds code; never self-ratifies. |
| `whetstone/` | **Cross-cutting CI-speed + flake-kill** (L1→L2) | Make CI faster **and eliminate flaky tests**, without weakening any gate — parallelize the pipeline, add caching, speed the suite, root-cause flakes; proves each change with a measured wall-clock drop. Commits to `main`, watches CI. |
| `owner/` | **Builder** (invoked by an advancer) | Advance one component **or** package by one unit via the hardened story loop (ground → design → dev → review → gates). Code in a worktree; docs in `main`. |
| `fe-engineer/` | **Builder** (invoked by an advancer) | Build web front-ends from a UX design — **Loupe's operator UI *and* the vertical apps** — vanilla HTML/CSS/JS + Go handlers; verifies in-browser. Reads lens projections (P5), never Core KV (Loupe excepted). |
| `lamplighter/` | **Cross-cutting ops** (L0/L1) | Observability watch — read Health KV → classify anomalies → surface remediation candidates. Never silently fixes. |

The **UX Designer (Sally)** is the bmad skill **`bmad-agent-ux-designer`** (not tracked here); she designs the
experience, the FE Engineer builds it (UX-then-FE).

## The scheduled fleet

| Task | Role (stream) | Cadence |
|---|---|---|
| `steward-autonomous` | `steward` — **Lattice** advancer | even hours (`6 */2`) |
| `steward-verticals` | `steward` — **Verticals** advancer | odd hours (`26 1-23/2`), interleaved |
| `lattice-designer` | `designer` — Lattice design (readiness) | odd hours (`6 1-23/2`), pipelines before the Lattice advancer |
| `platform-surveyor` | `surveyor` — Lattice hydrator | 3×/day (`56 7,15,23`) |
| `vertical-po-discovery` | `vertical-po` — Verticals hydrator | 3×/day (`41 5,13,21`) |
| `ci-whetstone` | `whetstone` — cross-cutting CI-speed | 2×/day (`36 6,18`) |

The Lattice stream is a three-stage pipeline: **Surveyor** (raw demand) → **Designer** (build-ready designs) →
**Lattice Steward** (builds), with the **Whetstone** as a cross-cutting CI-speed loop. `owner`, `fe-engineer`,
and `lamplighter` are **invoked by** the advancers (or run directly), not scheduled on their own. The bmad
tooling skills stay local and are intentionally not tracked here — this directory is only the agentic-ops roles.

**None of these fire on :00/:30.** Two things bit us in practice: `steward-autonomous`'s live cron had drifted
to `0 */1` (every hour) — silently doubling its frequency and erasing the even/odd interleave with
`steward-verticals`, so it collided with *every* other task's hour, not just the ones sharing its parity —
fixed back to `*/2`. Separately, every task's minute was 0 or 30, so on any hour multiple tasks share
(odd hours run `lattice-designer` + `steward-verticals` together, always; `platform-surveyor` /
`vertical-po-discovery` add a third some hours; even hours run `steward-autonomous` + `ci-whetstone` on 6/18),
they used to land within seconds of each other and only the small system-level dispatch jitter separated
them. Minutes are now spread ~15-20 apart within the hour so a same-hour pair has real separation, not just
whatever the scheduler's own jitter happens to give them. This doesn't guarantee zero lock contention — a long
fire can still be running when the next slot arrives — the mutual-exclusion lock's clean no-op-and-retry
is the actual backstop for that; jitter just makes it the exception instead of the rule.

### Concurrency: at most one fleet fire at a time

This runs on a single 16GB dev Mac — Docker + the native service binaries + the Go toolchain + browser
automation from even two concurrent fires is enough to exhaust memory and get the host to pause Claude/Chrome
(happened twice). Each of the 6 scheduled prompts above opens with a **mutual-exclusion lock**: `mkdir
/tmp/lattice-agentic-ops.lock` (atomic; fails if another fire already holds it) before doing anything else, and
`rm -rf` on that same path as its last action, success or failure alike. A lock older than 90 minutes is treated
as abandoned (a crashed/killed fire that never released) and reclaimed rather than wedging the fleet. If you add
a 7th scheduled role, give it the same guard — the lock only protects fires that ask for it, not ad-hoc/generic
worktree sessions outside this fleet. `make up` / `make orchestration` separately detect an already-healthy
stack and no-op instead of restarting it, so an ad-hoc `make up` from a stray worktree is now safe too.

### Chips need a push — an unattended fire has no one watching the session

A `spawn_task` chip only surfaces if Andrew happens to open the exact session that filed it; for one of the 6
scheduled fires, that's easy to miss entirely. Each prompt therefore also sends a `PushNotification` immediately
after any `spawn_task` call (its own, or one made by an inline sub-role like `owner`/`fe-engineer`/`lamplighter`)
— terse, one line, leads with what the chip flags. If you add a 7th scheduled role, give it the same instruction.
First real use may pause on a permission prompt nobody's there to answer since these tasks have never called
`PushNotification` before — click "Run now" once per task to pre-approve it ahead of that.
