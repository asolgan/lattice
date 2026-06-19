package weaver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/asolgan/lattice/internal/substrate"
)

// handleRow is the lane-1 handler: one KV-CDC message = the current state of
// one weaver-targets row (value = the §10.2 row JSON; an empty body is the
// entity-deletion tombstone). The handler is level-driven and idempotent —
// at-least-once redelivery re-evaluates the same row against the same durable
// marks and converges to the same dispatch set.
func (e *Engine) handleRow(ctx context.Context, msg substrate.Message) substrate.Decision {
	key := strings.TrimPrefix(msg.Subject, e.rowSubjectPrefix)
	targetID, entityID, ok := splitRowKey(key)
	if !ok {
		// Redelivery cannot fix a malformed key; drop it loudly.
		e.logger.Warn("weaver: row key is not <targetId>.<entityId>; dropping", "key", key)
		return substrate.Ack
	}
	target, ok := e.source.target(targetID)
	if !ok {
		// The target was removed/rejected but its consumer has not been torn
		// down yet (the reconcile runs on registry callbacks). Drop.
		e.logger.Debug("weaver: row for unregistered target; dropping", "targetId", targetID)
		return substrate.Ack
	}

	// An empty body is the entity-deletion tombstone (§10.2 IsDeleted path):
	// no row columns remain true, so the level reconcile clears every mark.
	var row map[string]any
	if len(msg.Body) != 0 {
		if err := json.Unmarshal(msg.Body, &row); err != nil {
			e.logger.Warn("weaver: row value unparseable; dropping", "key", key, "err", err)
			return substrate.Ack
		}
	}

	// Level-reconciled mark-clearing runs on EVERY row update first, violating
	// or not (§10.3: never edge-triggered — a coalescing watch can drop the
	// transitional flip). A mark only ever exists at a gap column the playbook
	// names, so the candidate set is the union of the playbook's gaps keys and
	// the row's missing_* columns; any candidate whose missing_<col> is not
	// currently true has its mark deleted. This single code path also clears
	// the marks of a closed gap and of a deleted entity. A clearing failure is
	// retried on a delayed cadence so a persistent KV failure cannot hot-loop.
	if !e.clearClosedMarks(ctx, target, targetID, entityID, row) {
		return substrate.NakWithDelay
	}

	if row == nil {
		return substrate.Ack
	}

	// Lane-3 scheduling leg: a row carrying a future freshUntil (re-)arms its
	// per-target-per-entity @at timer on EVERY delivery, violating or not —
	// level-driven, idempotent under one-schedule-per-subject replace. Runs
	// even for a disabled target: arming the timer is state-recording
	// bookkeeping, so an instant re-enable loses no deadline. Only a
	// schedule-publish failure defers the row.
	if !e.scheduleFreshness(ctx, targetID, entityID, key, row) {
		return substrate.NakWithDelay
	}

	// Dispatch-skip: a target carrying the `<targetId>.__control`
	// disabled marker (reflected in the in-memory disabled-set) Acks
	// here — mark-clearing (above) and freshness arming (above) still ran (a
	// disabled target keeps its violation-detection bookkeeping current), but
	// no NEW in-flight mark is created and no remediation
	// (Strategist/Actuator: triggerLoom/assignTask/directOp) runs for
	// this row. On enable, remediation resumes for whatever is still violating.
	if e.isTargetDisabled(targetID) {
		return substrate.Ack
	}

	if !e.boolColumn(targetID, row, "violating") {
		// L1: not violating — clearing already ran; nothing to dispatch.
		return substrate.Ack
	}

	entityKey, _ := row["entityKey"].(string)
	if entityKey == "" {
		// §10.2 requires the entityKey echo; without it the mark and the
		// remediation cannot name the candidate. Data error — surface, do not
		// fire (redelivery cannot fix the projected row).
		e.alert(issueKeyData(targetID, "entityKey"), "error", "RowDataError",
			"weaver-targets row "+key+" is violating but carries no entityKey")
		return substrate.Ack
	}

	nak := false
	delayed := false
	for _, col := range e.openGapColumns(targetID, row) {
		switch e.dispatchGap(ctx, target, targetID, entityID, entityKey, col, row, msg) {
		case substrate.Nak:
			nak = true
		case substrate.NakWithDelay:
			delayed = true
		default:
		}
	}
	if nak {
		// At least one gap needs an immediate retry; redelivery re-evaluates
		// every gap idempotently (existing marks re-fire the same episode
		// requestId).
		return substrate.Nak
	}
	if delayed {
		// Only delayed-retry gaps (unresolved references, metadata gaps) —
		// redeliver on the bounded cadence, never a hot loop.
		return substrate.NakWithDelay
	}
	return substrate.Ack
}

// dispatchGap runs Evaluator L2 + Strategist + Actuator for one open gap.
//
// Dispatch OCC (§10.8): the weaver-state CAS-create is the anti-storm gate —
// create wins → dispatch; create loses → the winner dispatched, drop. The
// in-flight skip applies to FIRST deliveries only: on a redelivery
// (msg.NumDelivered > 1, i.e. a prior delivery Nak'd or crashed before ack)
// EVERY in-flight gap on the row re-fires its episode requestId — the
// redelivery signal is per-message, not per-gap, so the retry is a blanket
// re-fire across the row's in-flight gaps. Each re-fire derives the same
// requestId from its mark's create revision and collapses on the Contract #4
// tracker, so the blanket retry never double-acts and a lost publish is not
// wedged behind its own mark.
func (e *Engine) dispatchGap(ctx context.Context, target *Target, targetID, entityID, entityKey, col string,
	row map[string]any, msg substrate.Message) substrate.Decision {

	ga, ok := target.Gaps[col]
	if !ok {
		// A true missing_* column with no playbook entry is a config error →
		// alert, never silently skipped (FR29 discipline).
		e.alert(issueKeyGap(targetID, col), "error", "GapWithoutPlaybook",
			"target "+targetID+": row column "+col+" is true but the playbook defines no gaps entry for it")
		return substrate.Ack
	}

	// The row's substrate per-key revision arrives free on the CDC message
	// (the backing-stream sequence IS the KV revision) — the op payload's OCC
	// revision-condition. A zero sequence means JetStream metadata is
	// unavailable: never publish expectedRevision 0 (the "must not exist" OCC
	// sentinel) — defer to a delayed redelivery, which carries metadata.
	if msg.Sequence == 0 {
		e.logger.Warn("weaver: message metadata unavailable (sequence 0); deferring gap dispatch",
			"targetId", targetID, "entityId", entityID, "gap", col)
		return substrate.NakWithDelay
	}
	pl, dec := e.planGap(targetID, entityID, col, ga, row, msg.Sequence)
	if pl == nil {
		return dec
	}

	// redelivered classifies this delivery for the in-flight branch: only
	// NumDelivered 1 is a definitively FRESH delivery (the anti-storm drop).
	// NumDelivered 0 (metadata unavailable) deliberately counts as a
	// redelivery: it may be a retry whose prior delivery never published, and
	// re-firing is the safe side (the same episode requestId collapses on the
	// Contract #4 tracker; a drop could wedge a lost publish behind its own
	// mark).
	return e.fireEpisode(ctx, targetID, entityID, entityKey, col, ga.Action, pl, msg.NumDelivered != 1)
}

// planGap resolves one gap's plan (Evaluator L2 + Strategist), routing a
// failure by its class: an unresolved reference defers on the bounded
// redelivery cadence; a config/data error is alerted and the gap skipped
// (retrying cannot fix it). pl == nil means do not dispatch — the returned
// Decision is the caller's disposition for this gap.
func (e *Engine) planGap(targetID, entityID, col string, ga GapAction, row map[string]any,
	rowRevision uint64) (*plan, substrate.Decision) {

	pl, perr := buildPlan(e.source, targetID, entityID, col, ga, row, rowRevision)
	if perr != nil {
		switch perr.kind {
		case errTransient:
			// An unresolved reference may be replay lag or a permanent config
			// error (a typo'd pattern, an uninstalled package) — retry on the
			// bounded redelivery cadence (never a hot loop) and surface to
			// Health until it resolves; the issue clears on the first
			// successful plan.
			e.logger.Warn("weaver: gap dispatch deferred; nak with delay for redelivery",
				"targetId", targetID, "entityId", entityID, "gap", col, "reason", perr.msg)
			e.issues.set(issueKeyGap(targetID, col), "warning", "UnresolvedReference",
				"target "+targetID+" gap "+col+": "+perr.msg)
			return nil, substrate.NakWithDelay
		case errData:
			e.alert(issueKeyData(targetID, col), "error", "TemplateDataError",
				"target "+targetID+" gap "+col+": "+perr.msg)
			return nil, substrate.Ack
		default:
			e.alert(issueKeyGap(targetID, col), "error", "PlaybookConfigError",
				"target "+targetID+" gap "+col+": "+perr.msg)
			return nil, substrate.Ack
		}
	}
	e.issues.clear(issueKeyGap(targetID, col))
	e.issues.clear(issueKeyData(targetID, col))
	return pl, substrate.Ack
}

// fireEpisode is the lane-1 dispatch core: resolve the in-flight mark,
// CAS-create on absence (the dispatch OCC), and fire the episode op.
// redelivered selects the in-flight disposition — false drops (the anti-storm
// gate: another episode is in flight), true re-publishes the SAME episode
// requestId (idempotent at the Contract #4 tracker). The reconciler sweep
// does not pass through here: its reclaim replaces the expired mark in place
// under a revision condition and fires directly. action is recorded on the
// mark (the §10.3 value shape) so the sweep can re-dispatch the right episode.
func (e *Engine) fireEpisode(ctx context.Context, targetID, entityID, entityKey, col, action string,
	pl *plan, redelivered bool) substrate.Decision {

	_, markRev, inFlight, err := e.marks.get(ctx, targetID, entityID, col)
	if err != nil {
		e.logger.Error("weaver: mark read failed; nak with delay", "targetId", targetID, "entityId", entityID, "gap", col, "err", err)
		return substrate.NakWithDelay
	}
	if inFlight {
		if !redelivered {
			// A fresh delivery while the episode is in flight — the anti-storm
			// drop.
			return substrate.Ack
		}
		// Redelivery retry path: re-publish the same episode.
		return e.fire(ctx, targetID, entityID, col, markRev, pl)
	}

	rev, lost, err := e.marks.create(ctx, targetID, entityID, col, entityKey, action)
	if err != nil {
		e.logger.Error("weaver: mark create failed; nak with delay",
			"targetId", targetID, "entityId", entityID, "gap", col, "err", err)
		return substrate.NakWithDelay
	}
	if lost {
		// A concurrent evaluation won the CAS — the winner dispatched.
		return substrate.Ack
	}
	return e.fire(ctx, targetID, entityID, col, rev, pl)
}

// fire materializes one episode's op and fire-and-forget publishes it. A
// publish failure Naks: the mark already exists, so the redelivery re-derives
// the SAME requestId and re-publishes (idempotent at the Processor).
func (e *Engine) fire(ctx context.Context, targetID, entityID, col string, markRevision uint64, pl *plan) substrate.Decision {
	requestID := deriveEpisodeRequestID(targetID, entityID, col, markRevision)
	if err := e.act.submit(ctx, requestID, pl.operationType, pl.payload(markRevision), pl.authTarget, pl.reads); err != nil {
		e.logger.Error("weaver: op publish failed; nak for retry",
			"targetId", targetID, "entityId", entityID, "gap", col, "requestId", requestID, "err", err)
		return substrate.Nak
	}
	return substrate.Ack
}

// clearClosedMarks is the level-reconciled mark-clearing pass. Returns false
// when a delete failed (the caller Naks with delay so the reconcile re-runs
// without hot-looping). A nil row (entity deleted) clears every candidate.
func (e *Engine) clearClosedMarks(ctx context.Context, target *Target, targetID, entityID string, row map[string]any) bool {
	ok := true
	for _, col := range markCandidateColumns(target, row) {
		if row != nil && e.boolColumn(targetID, row, col) {
			continue
		}
		if err := e.marks.delete(ctx, targetID, entityID, col); err != nil {
			e.logger.Error("weaver: mark clear failed",
				"targetId", targetID, "entityId", entityID, "gap", col, "err", err)
			ok = false
		}
	}
	return ok
}

// boolColumn reads a §10.2 bool column off a row. A present value of any other
// type is a Lens data error: surfaced (Warn log + Health KV issue) and treated
// conservatively as not actionable — never silently inverted into a clean
// false.
func (e *Engine) boolColumn(targetID string, row map[string]any, col string) bool {
	v, ok := row[col]
	if !ok || v == nil {
		return false
	}
	b, isBool := v.(bool)
	if !isBool {
		msg := fmt.Sprintf("target %s: row column %q is %T, not the §10.2 bool; treated as not actionable", targetID, col, v)
		e.logger.Warn("weaver: " + msg)
		e.issues.set(issueKeyData(targetID, col), "warning", "RowDataError", msg)
	}
	return b
}

// markCandidateColumns is the union of the playbook's gaps keys and the row's
// missing_* columns — every column a mark could exist at — in deterministic
// order.
func markCandidateColumns(target *Target, row map[string]any) []string {
	set := make(map[string]struct{}, len(target.Gaps))
	for col := range target.Gaps {
		set[col] = struct{}{}
	}
	for col := range row {
		if strings.HasPrefix(col, gapColumnPrefix) {
			set[col] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for col := range set {
		out = append(out, col)
	}
	sort.Strings(out)
	return out
}

// openGapColumns returns the row's missing_* columns whose value is true, in
// deterministic order. Gaps fire in parallel-safe sequence (independent
// marks); gap dependencies are the Lens's problem, not Weaver's (§10.8). A
// non-bool column value is surfaced and reads as not-open (boolColumn).
func (e *Engine) openGapColumns(targetID string, row map[string]any) []string {
	var out []string
	for col := range row {
		if !strings.HasPrefix(col, gapColumnPrefix) {
			continue
		}
		if e.boolColumn(targetID, row, col) {
			out = append(out, col)
		}
	}
	sort.Strings(out)
	return out
}

// splitRowKey splits a weaver-targets key <targetId>.<entityId> (§10.2: the
// entity segment is the bare NanoID, so exactly one dot separates the
// segments).
func splitRowKey(key string) (targetID, entityID string, ok bool) {
	i := strings.IndexByte(key, '.')
	if i <= 0 {
		return "", "", false
	}
	targetID, entityID = key[:i], key[i+1:]
	if !substrate.IsValidNanoID(entityID) {
		return "", "", false
	}
	return targetID, entityID, true
}

// alert records a Health KV issue and logs it at Error — the FR29 loud-failure
// pair.
func (e *Engine) alert(key, severity, code, message string) {
	e.logger.Error("weaver: " + message)
	e.issues.set(key, severity, code, message)
}

func issueKeyGap(targetID, col string) string  { return "gap:" + targetID + "." + col }
func issueKeyData(targetID, col string) string { return "data:" + targetID + "." + col }
