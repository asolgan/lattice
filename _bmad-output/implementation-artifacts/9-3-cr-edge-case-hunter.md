# Story 9.3 — Edge Case Hunter (lane-3 temporal) — 2026-06-12

Scope: uncommitted diff in `internal/weaver/` (incl. new `temporal.go`) + `internal/substrate/publish.go`, full-repo context. Method: exhaustive path walk of the scheduling leg (`scheduleFreshness`), the fired handler (`handleFiredTimer`), engine wiring, and the NATS 2.14.0 server-side schedule semantics (verified against `nats-server@v2.14.0 server/stream.go:6384`, `server/scheduler.go:313-395`).

## Findings (unhandled paths)

```json
[
  {
    "location": "internal/weaver/temporal.go:160-205 (handleFiredTimer)",
    "trigger_condition": "Timer fires while engine down; entity re-armed with later freshUntil before fired message consumed",
    "guard_snippet": "if sched, _ := stream.GetLastMsgForSubject(ctx, scheduleSubject); sched != nil && schedAtAfter(sched, p.FireAt) { return substrate.Ack } // pending later schedule supersedes stale firing",
    "potential_consequence": "Stale MarkExpired flips a freshly-renewed entity violating; spurious remediation dispatched"
  },
  {
    "location": "internal/weaver/temporal.go:64-74 + actuator.go scheduleTimer (no TTL) + bootstrap primordial.go:170-177 (no MaxAge)",
    "trigger_condition": "weaver-temporal durable deleted/recreated; DeliverAll replays fired messages older than 24h tracker TTL",
    "guard_snippet": "header[\"Nats-Schedule-TTL\"] = \"24h\" // or MaxAge on core-schedules; fired msgs currently persist forever (limits retention, 1/subject)",
    "potential_consequence": "Mass MarkExpired re-execution past the Contract #4 24h dedup horizon; unbounded per-entity storage"
  },
  {
    "location": "internal/weaver/evaluator.go:63-65 + temporal.go:140-143",
    "trigger_condition": "Persistent schedule-publish failure (core-schedules unavailable/misnamed) on a violating row carrying freshUntil",
    "guard_snippet": "e.issues.set(issueKeyTimer(targetID+\".\"+entityID), \"warning\", \"SchedulePublishError\", msg) // and/or run scheduleFreshness AFTER gap dispatch",
    "potential_consequence": "Lane-1 remediation dispatch starved behind lane-3 NakWithDelay loop; invisible in Health KV (log-only)"
  },
  {
    "location": "internal/weaver/temporal.go:125-134",
    "trigger_condition": "Engine clock ahead of NATS server clock; freshUntil falls inside the skew window",
    "guard_snippet": "if !fireAt.After(time.Now().Add(-skewGrace)) { ... } // NATS 2.14 does NOT reject past @at — it stores and fires immediately (scheduler.go:318-325); the comment's publish-reject rationale is wrong",
    "potential_consequence": "Deadline silently skipped as past, never fired; expiry missed until an unrelated CDC touch"
  },
  {
    "location": "internal/weaver/temporal.go:86-89",
    "trigger_condition": "Lens fixes a bad freshUntil by removing the column entirely",
    "guard_snippet": "if !ok || v == nil { e.issues.clear(issueKeyData(targetID, freshUntilColumn)); return true }",
    "potential_consequence": "RowDataError warning persists in Health KV forever after the data is fixed"
  },
  {
    "location": "internal/weaver/temporal.go:164-188, 103-111 (issueKeyTimer)",
    "trigger_condition": "Good firing (or config fix) follows a once-malformed firing for the same tail/targetId",
    "guard_snippet": "e.issues.clear(issueKeyTimer(tail)) // on the success path before submit; same for the firedToken config alert",
    "potential_consequence": "TimerDataError/ScheduleConfigError issues are set-only — permanent stale alerts"
  },
  {
    "location": "internal/weaver/temporal.go:160-205 (no registry check) vs evaluator.go:26-31",
    "trigger_condition": "Timer fires for a target uninstalled (or entity deleted) while pending",
    "guard_snippet": "if _, ok := e.source.target(targetID); !ok { e.logger.Debug(...); return substrate.Ack } // or accept + alert on async rejection",
    "potential_consequence": "MarkExpired submitted and rejected silently; stated self-heal (next CDC touch) cannot occur for deleted entity"
  },
  {
    "location": "internal/weaver/temporal.go:124-125",
    "trigger_condition": "freshUntil less than 1s in the future (untruncated passes After(now), truncated header is past)",
    "guard_snippet": "fireAtT := fireAt.UTC().Truncate(time.Second); if !fireAtT.After(time.Now()) { return true } // check the same instant that is published",
    "potential_consequence": "@at published in the past; server fires immediately — expiredAt earlier than the row's deadline"
  },
  {
    "location": "internal/weaver/temporal.go:173-194",
    "trigger_condition": "Fired payload targetId disagrees with subject-derived targetId (foreign/corrupt publish in schedule.>)",
    "guard_snippet": "if p.TargetID != \"\" && p.TargetID != targetID { alert TimerDataError; return substrate.Ack }",
    "potential_consequence": "Mismatch silently masked; op payload carries subject targetId with another target's entityKey"
  }
]
```

## Checked-and-handled ledger

| Path | Verdict |
|---|---|
| Garbage `freshUntil` (non-string, non-RFC3339) | Handled — RowDataError issue + skip + Ack (`temporal.go:90-102`, tested) |
| `freshUntil` missing/null | Handled — early return, no schedule (stale-issue clear gap is finding 5) |
| Far-future `freshUntil` | Handled — schedules normally; @at absolute, no server cap hit |
| `@at` in the past (server side) | Verified nats-server 2.14.0: NOT rejected at publish; stored and fired immediately. Engine's own past-check prevents publishing them (skew margin is finding 4; sub-second truncation is finding 8) |
| Row redelivery republishing identical schedule | Handled — same subject + same `@at`; MaxMsgsPerSubject=1 rollup + server `isInflight` guard; level-correct |
| Re-publish racing the in-flight fire | Handled — duplicate firing carries the same fireAt → same deterministic requestId → tracker collapse |
| Entity tombstone leaves pending timer | Adjudicated (no cancel in Phase 2, pinned by test); the *consequence* for a deleted entity is finding 7's silent-rejection leg |
| Fired-subject extra/missing tokens, non-NanoID entity | Handled — `splitRowKey` (first-dot split + NanoID validation) drops loudly with TimerDataError (tested) |
| Unparseable/incomplete fired payload, non-RFC3339 fireAt | Handled — Ack + TimerDataError, no op (tested) |
| Deterministic requestId across redelivery | Handled — derived from reconstructed schedule subject + the STORED payload fireAt (not receive time); redelivery byte-identical (tested, incl. re-armed-new-id) |
| `targetId == "fired"` capturing the pending namespace | Handled — `firedToken` refusal + ScheduleConfigError on the publish side (clear gap is finding 6) |
| New schedule vs already-fired-unconsumed message | NOT superseded — different subjects, rollup can't reach it → finding 1. Two *firings* for one subject do roll up (only latest converts; level-correct) |
| Restart durability / fires-while-down | Handled — fixed durable, DeliverAll, ack floor; fired messages durable under limits retention (e2e-tested both legs); durable-recreation replay is finding 2 |
| Double-dispatch with 9.1 lane-1 / 9.2 sweep | None introduced — temporal leg takes no mark; CDC-flip dispatch goes through the mark CAS; sweep reclaims in place under revision condition |
| freshUntil vs boolColumn/level-clearing interplay | Handled — freshUntil read as string (never via boolColumn), distinct issue keys; clearing runs first and its NakWithDelay precedes the scheduling leg |
| Op publish failure in fired handler | Handled — Nak for retry; redelivery re-derives the same requestId |
| `json.Marshal(timerPayload)` failure branch | Unreachable (all-string struct) but routed to NakWithDelay — no gap |
| Engine.Start: temporal consumer up before registry load | No gap — fired handler does not consult the registry (registry absence itself is finding 7) |
