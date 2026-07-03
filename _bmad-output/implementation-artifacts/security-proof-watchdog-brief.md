# Continuous security-proof watchdog — a new Lattice component (Designer brief)

**Author:** Winston (owner/PO hand-off) · **For:** the Lattice Designer · **Status: needs-design (filed 2026-07-02, Andrew).**
**Altitude:** problem + grounding + Andrew's decisions + constraints — the definition-of-ready. The *design*
(mechanism, contracts, fires) is the Designer's output; this is the hand-off, not the solution.

---

## The problem

The platform's two live **security proofs** are the Phase-1 adversarial gates:

- **gate2** (`make test-bypass`, [Makefile:619](../../Makefile)) — every attempt to **bypass** the write-path /
  authorization model is BLOCKED (direct-KV write, DDL-schema tamper, Starlark-I/O escape, stream publish).
- **gate3** (`make test-capability-adversarial`, [Makefile:633](../../Makefile)) — every attack on the
  **Capability Lens** (access control) is DEFENDED (cross-target bleed, read-bypass, projection resurrection,
  lane-unauthorized, …).

Each suite writes a `health.gates.phase1.<gate>` marker `{passed, timestamp, commit}` to Health KV, and Loupe's
System Map renders them as chips. Two problems make this unfit as-is:

1. **They're only run manually**, so a green chip means "proven whenever someone last ran it," not "proven
   recently." There is no continuous re-verification.
2. **Running them is DESTRUCTIVE to a live stack.** Both targets begin with `make down` then `make up` —
   and [`make up`](../../Makefile) is **kernel-only** (NATS, Postgres, bootstrap, Refractor, Processor); it does
   **not** start the orchestration tier (that's `make orchestration`, only `up-full` runs it). So running either
   gate against a live `up-full` stack **tears everything down and restores only the kernel** — Processor +
   Refractor come back green, Weaver/Loom/Bridge/Object-Store stay red, and Loupe itself is killed. This is the
   "stack goes down and only partially comes back" Andrew observed. It's destructive by design (a clean stack
   makes the proof deterministic — fine for CI, hostile to an operator).

## Andrew's decisions (2026-07-02)

- **A new Lattice component owns continuous security verification** — "like Sentinel, but a Lattice-family name."
  Working name **Warden** (alts for the Designer/ratification to weigh: Vigil, Assayer, Bulwark, Proctor). It is
  the platform's standing red-team: it re-proves the defenses on a cadence and reports their status.
- **The raw gate chips come off the Loupe map now** (removed ahead of the redesign — done in the Loupe lane);
  the security-proof surface returns as *this component's* surface once designed.
- **Only the two security gates (g2/g3)** are in scope for the surface — gate4 (embedded-only, can never write a
  live marker) and gate5 (30-min end-to-end) are not live security proofs.

## What it must do (the shape, for the Designer to design *through*)

- **Run g2 + g3 in an ISOLATED ephemeral context** — its own throwaway stack (separate compose project / ports /
  volumes) or embedded NATS — that it stands up, proves against, and tears down **without ever touching the
  operator's live stack.** This is the core requirement and the whole reason it's a component, not a button that
  shells out to the destructive Makefile targets. (gate3's per-vector `TestCapAdv_*` already run embedded/
  self-contained — [Makefile:630](../../Makefile) — so isolation is partly precedented.)
- **On a schedule** (loose cadence — Andrew: "doesn't have to be often") **and on-demand** ("check now").
- **Report to Health KV** — pass/fail + freshness (last-run time) + the commit proven, so a console surface can
  show "Bypass Defense ✓ · verified 3h ago" and go stale past the cadence. Its own `health.<component>.*`
  heartbeat plus the gate markers.
- **Be the operator-facing path**, superseding the destructive `make test-bypass`/`-capability-adversarial` for
  anything but CI (the down+up-fresh recipe can stay the CI path, or the component subsumes it).

## Constraints the design must honor

- **Loupe cannot run `make`** — it does NATS I/O, not shell, and never reads the repo. So "check now" is a
  **request to this component** (a NATS/control-plane request), not a Loupe shell-out. The component is the
  runner; Loupe only triggers + displays.
- **P1/P5** — the component reports via Health KV (operational self-report, the sanctioned direct-KV-write class),
  not Core KV; any console read is a lens/Health read, not a repo read.
- The **isolated stack must be genuinely isolated** (own JetStream store dir / compose project) so a proof run
  can never corrupt or contend with the live stack's state.

## Open questions for the Designer

1. **Isolation mechanism** — separate `docker compose -p` project (real stack, higher fidelity, heavier) vs
   embedded NATS + in-process engines (lighter, already precedented for g3 vectors) vs ephemeral containers.
2. **Scheduler** — a CI cron writing to a shared Health KV, a small local scheduled task, or a platform
   primitive (the `@every` recurring-schedule substrate exists, but running a *test suite* isn't a platform op —
   likely an ops-plane scheduler, not `@every`).
3. **The Loupe surface** — a map node for the component + a security-proof panel (human-named chips, freshness,
   "check now"); design it as this component's surface, filed to the Loupe lane once the component's read/trigger
   seams exist. (The removed chips are the placeholder this replaces.)
4. **Extensibility** — is the marker/report shape generic enough that a future proof (a new gate, a fuzz suite)
   joins without a component change?
5. **Name** — confirm the Lattice-family name at ratification.

## Loupe-lane companion (already actioned / to file)

- **Now:** the raw gate chips are removed from the System Map (`cmd/loupe`, gates panel) — done in the Loupe lane.
- **Later:** the console security-proof surface returns as this component's surface (map node + panel + "check
  now"), filed to the Loupe board once this component exposes its read/trigger seams.
