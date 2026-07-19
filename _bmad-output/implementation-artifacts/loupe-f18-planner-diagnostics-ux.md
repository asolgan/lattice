# Loupe F18 — Weaver planner-mandate diagnostics (UX)

**Status:** ✅ adjudicated (Winston, Andrew-delegated for the Loupe program) — 2026-07-19.
Board row: [backlog/loupe.md](../planning-artifacts/backlog/loupe.md) → F18.

## 1. The gap

The Weaver's planner mandate shipped end-to-end (consumed by LoftSpace renewals) with a rich
diagnostic surface published on every heartbeat — and the console rendered none of it. The Weaver
component page showed the generic health rollup plus a one-line metrics summary (targets, marks
in flight, timers, sweep reclaims); an operator wanting to know whether the planner was oscillating,
diverging, or throttling had to expand the raw heartbeat and read JSON.

## 2. What the heartbeat already carries

Grounded in `internal/weaver/health.go` `emit()` — all of this is in the payload
`GET /api/component/weaver` already returns verbatim (`componentInstance.Doc`):

| Signal | Where | Shape |
|---|---|---|
| Oscillation freeze | `issues[]` code `TargetOscillation` (error) | pair + contested path, both targets disabled |
| Effect mismatch | `issues[]` code `LensEffectMismatch` (warning) + `metrics.effectMismatches` | per target/gap/action |
| Contraction | `metrics.contractionTrajectory` | `map[targetId] → shrinking\|steady\|diverging` |
| Shadow compare | `metrics.plannerShadow` | `map[targetId] → {agree, diverge, recentDivergences[≤10]}` |
| Admission | `metrics.admissionAdmitted` / `admissionDeferred` | counters, emitted only when either is nonzero |

**Consequence for the design:** F18 needs no server work, no new endpoint, and no new op. It is a
logic-tier + view fire.

## 3. The panel

One `Planner` section in the component page's left column (mirroring `renderGatewaySecurity`'s
placement and its `logic/` + goja-test split), **exception-first** — a healthy planner collapses to
a couple of quiet lines rather than five empty boxes.

Order is severity-descending, because that is the order an operator triages in:

1. **Frozen — oscillating targets** (red card). The only diagnostic that has already *taken an
   action*: the detector disabled both fighting targets. Renders the Weaver's message verbatim +
   `since`, and names the remediation — re-Enable from the Control column, after fixing the
   authoring conflict.
2. **Effect mismatches** — "dispatches commit but the targeted lens gap never flips"; points at a
   stale guard, a lens projecting the wrong column, or a no-op remediation.
3. **Contraction trajectory** — `diverging` targets named loudly in a yellow card; `shrinking` and
   `steady` collapse to counts.
4. **Admission control** — deferred share + raw counters, yellowing at ≥20%.
5. **Planner shadow** — per-target agree/diverge, worst-first, with the bounded divergence ring
   behind a `<details>`.

### Empty state

A kernel-only stack with no planned/shadow target omits every optional block and reports
`effectMismatches: 0`. That is the *expected* shape, so the panel renders one line: "No planner
activity recorded — no oscillation, no effect mismatch, and no target declaring shadow mode,
contraction sampling or admission control."

## 4. Adjudicated forks (Winston)

- **Reuse `/api/component/weaver`, do not add `/api/planner`.** The page already fetches the raw
  heartbeat and the control list in one request. A dedicated endpoint would duplicate the read for
  no gain and add a server surface to authorize.
- **Per-instance sections; never merge across instances.** `shadowStats`, `contractionStats` and the
  admission scheduler are per-process in-memory state that resets on restart (explicit in
  `planner_shadow.go`). A summed fleet-wide `diverge` would be a number no Weaver process holds.
  With one instance the instance heading is suppressed. This follows F3's established "plural
  instances, no last-write-wins collapse" posture for this page.
- **Shadow divergence renders neutral, not red.** The comparison is diagnostic-only and never
  altered a dispatch. The panel says so in-line, so a high diverge count reads as "inspect the
  candidate ranking", not "incident".
- **No new control button.** The frozen-pair card names the two targets and points at the Control
  column, which already carries `enable`/`disable`/`revoke` rows. A second submit path for the same
  op is duplicate surface — and keeping the fire read-only kept its review depth proportionate.
- **Read the structured `doc.issues`, not the server's flattened display strings.** The structured
  form carries `code`/`severity`/`since`; classification keys off `code`, and the message is passed
  through verbatim rather than re-parsed for the target pair (which would be brittle).

## 5. Honesty convention

The load-bearing rule, and what most of `web_logic_planner_test.go` asserts. The Weaver **omits**
`plannerShadow`, `contractionTrajectory` and the admission counters entirely when nothing has been
recorded. Those must read as *unknown* (`null` → "—" / section hidden), never as a clean zero:
"no comparison ran" is not "no divergence". Likewise an absent `effectMismatches` means the scan
itself failed that tick (the Weaver logs and skips the key), so the panel says "scan unavailable"
rather than reporting an all-clear.

## 6. Build state

**F18.1 SHIPPED** — `logic/planner.js` + `renderWeaverPlanner` in `views/component.js` +
`web_logic_planner_test.go`. No server change.

Remaining (not blocking, file a further fire only on real demand): the contraction and shadow
sections name target ids as plain text; linking each into a target-scoped view would need a
Weaver target page, which does not exist today.
