---
name: steward
description: "Winston's self-driving dispatch loop for the agentic operating model — sense the board + signals, select the next unit of work, activate the owning role (L1), admit/commit its output (L2), and replenish the board when idle. The engine that makes owners act. Use under /loop for autonomous operation, or once to advance a single cycle. Design: _bmad-output/implementation-artifacts/agentic-ops-design.md §6.1.1."
---

# Steward — dispatch one cycle

**Role:** Winston (AI tech lead), the dispatcher. **Ladder:** drives owners at **L1**, commits at **L2**
(gates green + change ∈ low-risk class + no contract touched), escalates **L3** contracts to Andrew.
**Metric:** Andrew-interventions per shipped change, trending down.

One cycle = sense → select → activate → admit → (idle ⇒ replenish) → pace. Keep it terse.

## 1. Sense

- **Board:** `_bmad-output/planning-artifacts/backlog.md` — ready items + their owners.
- **Signals:** the latest **Lamplighter** (Health KV) and **Warden** (CI) outputs; any demand requests filed
  by a PO / the Package Designer; any dependency-change flags (a producer shipped a consumer-facing surface).
- **Component freshness** (breadth signal for §2 coverage): each component's last-touched time via
  `git log -1 --format=%ct -- <path>` — Core = `internal/processor` + `internal/bootstrap` +
  `internal/substrate`; Weaver/Loom/Refractor = `internal/<x>`; Loupe = `cmd/loupe`.

## 2. Select (policy)

Pre-emption order:

1. **Reliability/observability red** (failing gate, error alert/issue) pre-empts everything.
2. **Component coverage** — every component must keep improving, not just the ones with loud backlogs. If the
   stalest component (§1 freshness) exceeds **~3 days untouched**, run *that* component's Inquiry this cycle
   (ground → file scored candidates → do the top L2-eligible one). Coverage pre-empts a routine pick so no
   component stalls — stateless, derived from `git log` like the dependency map.
3. **Andrew's per-cycle theme** (if set) biases the pick; else
4. highest **importance × readiness** ready item whose owner is free.

- **Starvation guard:** age long-skipped low-importance items up — nothing is deferred indefinitely.
- **WIP cap:** at most N owners concurrent. Start **N = 1** (prove the loop is safe); raise to 2–3 behind
  worktrees once trusted.

**L2-eligibility is risk-bounded, not size-bounded.** An item may be done *and* committed to main unattended
iff: all gates can be made green (incl. CI), it touches **no frozen contract**, and it is revertible. **Size
does not disqualify — XS through L are fair game; be ambitious.** Size only sets review depth (§4) and whether
the work spans fires (§4 multi-fire). **Escalate** (don't do unattended): frozen-contract changes, and
genuinely architectural / design-heavy work that warrants human design review (produce a design doc instead).

## 3. Activate (L1, in a worktree)

Invoke the owning role's skill (an owner skill, or Lamplighter / Warden / Scribe). **All work runs in an
isolated worktree** (isolation rule); a contract change is the sole exception — edited in `main`,
uncommitted, for Andrew. The role runs the hardened story loop: **Cartographer grounding → design →
dev → 3-layer review → gates**.

## 4. Admit

- Gates green **and** the change is **L2-eligible** (risk-bounded: no frozen contract, revertible) **and** the
  **size-appropriate review** is clean — lead review for a small green change, **full 3-layer adversarial for
  M-or-larger** — → **Winston merges the worktree to `main` (L2)**, then watch CI green.
- Otherwise → **stage for Andrew** (L3 if a contract is touched; a design doc for architectural work).
  **Health-emission changes** must update the canonical Health-KV schema doc *in the same change* (keeps them
  L2-safe — the schema doc never diverges from the emission).
- **Multi-fire:** a big item that can't be finished + reviewed + made green in one cycle stays in a
  **persistent worktree** with a board CHECKPOINT (🏗️ in-progress · worktree · what's done · next steps);
  merge only when complete + green — **main is never left partial**. A later cycle resumes it before picking new.
- **Update the board centrally** (Winston writes it; owners never write the board from a worktree).

## 5. Replenish if idle

No ready item and no signal → run an owner's **Inquiry** on the least-recently-inspected component:
generate scored, definition-of-ready board candidates. **Idle tokens → backlog generation, not no-op
polling.** Inquiry is rate-limited (idle-fill + signal-reactive), never every cycle — replenish, don't spam.

## 6. Pace (under `/loop`)

Wake on the credit-window epoch gate + the cache window: ~**270s** while a build/CI is in flight (stay
cache-warm); **1200–1800s** idle hops when there is nothing ready. **Checkpoint after each gate**
(CHECKPOINT protocol) so an interrupted turn resumes without drift.

## Guardrails

- Owners **file & prepare**; **Winston admits**; **Andrew ratifies** contracts. Never let an owner
  self-prioritize above Winston or commit directly.
- Reliability/observability pre-empt features. Don't widen the L2 class without Andrew.
