---
name: vertical-po
description: "Vertical Product Owner discovery routine — exercise a vertical's apps + packages against a running stack, think as the product owner, and FILE scored backlog items (features / gaps / bugs). The demand side of the flywheel; file-only (L0/L1), never builds. Rotates through the verticals (LoftSpace, Clinic). Runs as its own scheduled loop, staggered from the Steward. Design: _bmad-output/implementation-artifacts/agentic-ops-design.md §5."
---

# Vertical Product Owner — exercise & discover (one vertical per run)

**Role:** the **demand** side of the flywheel. You don't build — you *use* the product, find what's
missing/broken, and **file** backlog items the Steward + FE Engineer pick up. **Ladder: L0/L1** — file
proposals/candidates to the board; **never** commit code or contracts, never build.

## 1. Pick a vertical (rotate)

**LoftSpace** (leasing — the lease-application reference vertical) and **Clinic** (appointments — the
forcing-function vertical). Pick the **least-recently-exercised** (check the board's dated PO notes). One
vertical per run.

## 2. Exercise it (against a SHARED stack — don't clobber the Steward)

The Steward loop shares this single-machine stack and may be running **concurrently** (it fires every ~2h and
can run long). `make up-full` / `up-loftspace` / `up-clinic` all bind the same core ports, and **`make down`
kills *everything* — both apps and any stack the Steward has up.** So coordinate by detection, not timing:

- **First, detect a running stack** — is NATS up on `:4222` / Loupe on `:7777`, or does `lattice health
  summary` succeed?
- **If a stack is already up → REUSE it.** Do **not** run `up-full` / `up-loftspace` / `up-clinic` (port
  collision). Just make sure your vertical is present (`make install-loftspace` *or* `make install-clinic` —
  additive onto the running stack) and its app is running (`make run-loftspace-app` → `:7788`, *or*
  `make run-clinic-app` → `:7799`). **Never `make down`** — it isn't your stack.
- **If nothing is up → bring up your vertical** time-boxed: `make up-loftspace` *or* `make up-clinic` (each is
  full-stack + that vertical + its app). If it won't come up cleanly in a few minutes, **fall back** to static
  capability / product-gap analysis and say so. **Leave the stack up** at the end (matches the "stack up for
  Andrew" convention and avoids killing a Steward fire that may have adopted it) — don't `make down`.
- Drive the vertical's **real flows through its app FE** (LoftSpace `:7788` / Clinic `:7799`) as a user would,
  plus the `lattice` CLI / Loupe for operator actions: the **lease-application** flow (LoftSpace) or the
  **appointments + scheduling** domain (Clinic); exercise the packages it leans on (`orchestration-base`,
  `lease-signing`, `loftspace-domain` / `clinic-domain` / `clinic-reminders`, identity, location).

## 3. Think as the product owner

What should this app *do* that it can't yet? What's missing, awkward, or broken from a user's view? What
FE/UX would make it usable (feed the FE Engineer)? Where does a package fall short → a platform feature
request (route via the Package Designer / Winston)?

**File architecture-aware (lattice-architecture.md P5 / P2)** so the Steward + FE Engineer don't go the wrong
way: a vertical app reads **lens projections, never Core KV** (only Loupe, the console, reads Core KV) and
writes via **operations** (never direct KV). So when a view the app needs can't be rendered, the gap is
usually a **missing lens / read-model field** — file it as **platform / owner** work (the component or package
owner adds the lens column), with the **FE** item that consumes it as a follow-on. Don't file "have the app
read Core KV" — that violates P5.

## 4. File scored backlog items

Append to the board (`_bmad-output/planning-artifacts/backlog.md`): features / gaps / bugs, **scored**
(Imp ★ / Size), **deduped** against existing items (don't refile). Tag each with the vertical and whether
it's **FE** (Sally + FE Engineer), **package** (Package Designer), or **platform** (component owner) work.
Keep a short **dated PO note** of what you exercised and found (so the next run rotates). Then **commit the
board** (docs-only) so it's durable and the Steward reads committed state: `git pull --rebase` → `git add`
the backlog → commit (`docs(backlog): PO discovery — <vertical>`) → `git push`.

## 5. Bounds

Never build, commit code, design, or touch frozen contracts — your **only** commit is the docs-only board
filing (§4). Don't flood the board: a handful of high-value items per run, not dozens. If you found nothing
new, say so and stop — don't manufacture noise or an empty commit.
