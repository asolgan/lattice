# Loupe F17 — Orchestration queue observability (UX design)

**Status:** ✅ ADJUDICATED (Winston, 2026-07-18 — Andrew-delegated for the Loupe program).
Build fires per §6. Sally drafted; Winston resolved the §5 forks inline as lead (no
frozen-contract touch, no cross-lane ask). Program board: `backlog/loupe.md` (F17 row).

## 1. Problem

Loupe's task inbox (`cmd/loupe/tasks.go` → `computeTasks`; `web/js/views/tasks.js`) renders a task's
`status / assignee / operation / scopedTo` and nothing else. The **FR28/FR29 orchestration queue plane**,
shipped in `packages/orchestration-base` since Loupe's last task work, is invisible to the operator:

- **Role-queue (pull) assignment** — `lnk.task.<id>.queuedFor.role.<roleId>`. A task can be *queued to a
  role* (any holder claims it via `ClaimTask`) rather than *assigned to an identity*. Invariant: an open
  task has **exactly one** of `assignedTo` / `queuedFor` (`orchestration-base/package.go`). Today a queued
  task renders with a blank assignee — indistinguishable from a malformed orphan.
- **Availability-gated routing** — `vtx.identity.<id>.availability` aspect, `data = { available: bool }`,
  written by `SetAvailability` (`orchestration-base/ddls.go`). CreateTask routing reads it:
  given + alive + **available** assignee wins; unavailable/absent falls back to the role queue. **Absent
  aspect == available** (byte-compatible with pre-Fire-2 callers).
- **Unrouted / stuck work (FR29)** — the `unroutedTasks` Weaver target (`orchestration-base/targets.go`)
  raises Health-KV issue `UnroutedTasks` (warning, §10.8 `surface` action) for as long as an **open,
  role-queued task stays unclaimed past its own `expiresAt`**. Surface-only — *no* auto-remediation — so
  **the operator is the remediation**. That's the load-bearing reason the console must show it.

## 2. Goal

Make the existing task inbox **queue-plane-aware** so an operator can, at a glance: tell an assigned task
from a role-queued one; see whether an assignee is available; and spot stuck/unrouted work that needs a
human. Light, in-place extension of the inbox — **no new tab**.

## 3. Surfaces (extends `/api/tasks` + the Tasks panel)

### 3.1 `computeTasks` (backend) — per row
- `queuedFor` (role vertex key) — sourced from the `queuedFor` link, same walk as `assignedTo` / `scopedTo`
  (`linkForVertex` already yields `Relation:"queuedFor", OtherKey:vtx.role.<id>`).
- `assignment` — derived kind: `"assigned"` | `"queued"` | `""` (neither; e.g. a completed task whose
  links were tombstoned). The UI renders from this rather than re-inferring.
- `available` (`*bool`, tri-state) — for an **assigned** task only: read the assignee's `.availability`
  aspect; `data.available` false → `false`, aspect absent → `true` (routing semantics). `null` for a
  queued/none task (availability is per-identity; a role queue has no single assignee).
- `stuck` (bool) — **open AND queued AND `expiresAt` in the past.** The Loupe-local, per-row mirror of the
  Weaver `missing_claim` gap: an open role-queued task past its own expiry with no claim. Computed from
  keys Loupe already lists (inspector reads Core KV — sanctioned; Loupe is the P5 exception).
  **`now` is injected** into `computeTasks` so the pure function stays deterministic/testable.

### 3.2 `views/tasks.js` (frontend)
- **Assignment badge** on every card: `assigned → <identity>` or `queued → <role>` (role keyLink). Queued
  cards stop reading as orphans.
- **Availability chip** on assigned cards: `available` (calm) / `unavailable` (amber), from `available`.
- **Stuck highlight**: `stuck` cards get a red `stuck · unrouted` badge and sort to the **very top** (above
  other open tasks). A "stuck / unrouted only" checkbox filters to them client-side.
- Empty/other statuses unchanged.

## 4. Not in this increment (deliberate scope lines)
- **No duplicate Health-KV banner in `/api/tasks`.** The authoritative FR29 `UnroutedTasks` issue already
  renders on the **Weaver component health page** (`health.go` flattens §5.5 `issues[]`). Re-reading the
  health bucket inside the tasks endpoint would duplicate a signal the operator already sees and add a
  cross-bucket walk. The inbox's per-row `stuck` flag is the actionable **drill-down** from that issue;
  the component page stays the authoritative rollup. (A future increment can cross-link the two.)
- **No claim/reassign/set-availability write actions.** Read-observability first; the write surfaces
  (`ClaimTask`, `ReAssignTask`, `SetAvailability`) are a fair follow-on but out of this increment.

## 5. Forks — resolved by Winston (lead)
- **New tab vs extend inbox →** extend the inbox. The queue plane *is* the task inbox; a separate tab
  fragments one mental model.
- **Stuck: computed locally vs read the `unroutedTasks` lens rows →** computed locally per row. Immediate,
  no dependency on the weaver-targets read-model being provisioned on a given stack, and byte-cheap on the
  key list Loupe already walks. The Weaver-authoritative signal stays on the component page (§4).
- **Availability display →** per-assignee chip only, `absent == available` (mirrors routing). No chip for
  role-queued tasks (no single assignee to attribute).
- **Time source →** inject `now` (dependency-injected) so `computeTasks` stays deterministic under test.

No frozen-contract change; no `cmd/loupe/**` boundary crossing; no cross-lane ask.

## 6. Build fire
**F17.1 — ✅ SHIPPED (`5b623837`, 2026-07-18) — queue-plane-aware inbox.** `computeTasks`: `queuedFor` + `assignment` +
`available` + `stuck` (inject `now`); `/api/tasks` passes `time.Now().UTC()`. `views/tasks.js`: assignment
badge, availability chip, stuck badge + top-sort + "stuck/unrouted only" filter. Goja/Go unit coverage for
the four new derivations; live verify against the running stack. One green increment → F17 closes (write
actions deferred as a named follow-on, not a blocker).
