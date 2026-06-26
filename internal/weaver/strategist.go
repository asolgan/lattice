package weaver

import (
	"fmt"
	"strings"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// Action names of the Contract #10 §10.8 action table.
const (
	actionTriggerLoom = "triggerLoom"
	actionAssignTask  = "assignTask"
	actionDirectOp    = "directOp"
)

// Operation types the Actuator submits.
const (
	opStartLoomPattern = "StartLoomPattern"
	opCreateTask       = "CreateTask"
	opMarkExpired      = "MarkExpired"
)

// assignTaskGrantTTL is the expiry horizon set on an assignTask grant. The
// human response window is unbounded by design; the grant outlives any
// realistic response, and the bound op's commit auto-completes the task.
const assignTaskGrantTTL = 30 * 24 * time.Hour

// rowTemplatePrefix marks a templated param value: row.<column> substitutes
// that column's value from the violation row (Contract #10 §10.8 Templating).
const rowTemplatePrefix = "row."

// errKind classifies a plan failure so the evaluator can route it: a config or
// data error is alerted and the gap skipped (redelivery cannot fix it); a
// transient error (a pattern/op reference the registry has not resolved) is
// surfaced to Health and retried on a delayed redelivery cadence — bounded
// cadence, unbounded count, so a reference that resolves later (replay lag, a
// pattern installed after the target) still recovers.
type errKind int

const (
	errConfig errKind = iota
	errData
	errTransient
)

// planError is a gap-planning failure with its routing class.
type planError struct {
	kind errKind
	msg  string
}

func (e *planError) Error() string { return e.msg }

// plan is one gap's fully-resolved dispatch, pending only the per-open-episode
// claimId: payload(claimID) materializes the op payload at fire time. The
// userTask actions fold the claimId into a STABLE artifact identity (assignTask's
// taskId, triggerLoom's Loom instanceId) so re-dispatch of the same open gap
// collapses on the existing task/instance; the op's own requestId stays
// episode-scoped (the mark create/replace revision) and collapses a same-episode
// re-fire on the Contract #4 tracker. Idempotency rests on these derived ids, not
// payload equality (time-derived fields such as assignTask's expiresAt differ per
// fire).
type plan struct {
	operationType string
	authTarget    string
	payload       func(claimID string) map[string]any
	// reads is the dispatched op's ContextHint.Reads: the BARE vertex keys the
	// op's DDL script hydrates + validates (vertex_alive). The dispatcher
	// declares them because it builds the payload and so knows the exact keys
	// the op touches. Empty for read-free ops (StartLoomPattern, MarkExpired,
	// most directOps). NO `.state` suffixes — the DDLs read bare keys.
	reads []string
}

// buildPlan resolves one open gap against its playbook entry: templated params
// are substituted from the row, and action-specific references (pattern → the
// live meta.loomPattern vertex; operation → the live op meta-vertex) resolve
// against the registry at dispatch time. expectedRevision is the candidate
// row's substrate per-key revision off the CDC message; every remediation op's
// payload carries it as the OCC revision-condition.
func buildPlan(source *targetSource, targetID, entityID, gapColumn string,
	ga GapAction, row map[string]any, expectedRevision uint64) (*plan, *planError) {

	switch ga.Action {
	case actionTriggerLoom:
		subject, perr := resolveStringParam("subject", ga.Subject, row)
		if perr != nil {
			return nil, perr
		}
		patternRef, perr := resolveStringParam("pattern", ga.Pattern, row)
		if perr != nil {
			return nil, perr
		}
		metaKey, ok := source.patternMetaKey(patternRef)
		if !ok {
			// The pattern meta-vertex may not have replayed yet (the CDC registry
			// is asynchronous) or the reference may never resolve (a typo, a
			// pattern not installed) — indistinguishable here, so the evaluator
			// retries on a delayed cadence and surfaces the condition to Health
			// until it resolves.
			return nil, &planError{kind: errTransient,
				msg: fmt.Sprintf("pattern %q has no loaded meta.loomPattern vertex", patternRef)}
		}
		return &plan{
			operationType: opStartLoomPattern,
			// Pattern-as-target (§10.8): Weaver holds StartLoomPattern @ scope: any
			// via the operator role, and per-pattern auth anchors on the pattern
			// definition vertex.
			authTarget: metaKey,
			payload: func(claimID string) map[string]any {
				return map[string]any{
					"patternRef":       metaKey,
					"subjectKey":       subject,
					"expectedRevision": expectedRevision,
					// A STABLE Loom instanceId (claimId-seeded, §10.3): every reclaim
					// re-supplies the same id, so the re-emitted loom.patternStarted
					// collapses on Loom's existing instance.<id> (no duplicate pattern,
					// hence no duplicate onboarding userTask). Absent claimId (a
					// pre-claimId mark mid-migration) yields a stable empty-seed id —
					// still consistent across that episode's reclaims.
					"instanceId": deriveStableInstanceID(targetID, entityID, gapColumn, claimID),
				}
			},
		}, nil

	case actionAssignTask:
		operation, perr := resolveStringParam("operation", ga.Operation, row)
		if perr != nil {
			return nil, perr
		}
		assignee, perr := resolveStringParam("assignee", ga.Assignee, row)
		if perr != nil {
			return nil, perr
		}
		taskTarget, perr := resolveStringParam("target", ga.Target, row)
		if perr != nil {
			return nil, perr
		}
		forOperation, ok := source.opMetaKey(operation)
		if !ok {
			return nil, &planError{kind: errTransient,
				msg: fmt.Sprintf("operation %q has no loaded op meta-vertex (forOperation unresolved)", operation)}
		}
		return &plan{
			operationType: opCreateTask,
			authTarget:    taskTarget,
			// The task DDL validates all three link endpoints with vertex_alive
			// (orchestration-base/ddls.go) — the caller MUST hydrate them. They are
			// the BARE keys (assignee/forOperation/scopedTo); the DDL reads no
			// `.state` aspect, so none is listed (a non-existent .state key would be
			// a HydrationMiss). Cross-checked against the script by
			// TestCreateTaskReads_MatchDDLScript.
			reads: []string{assignee, forOperation, taskTarget},
			payload: func(claimID string) map[string]any {
				return map[string]any{
					"assignee":     assignee,
					"forOperation": forOperation,
					"scopedTo":     taskTarget,
					"expiresAt":    substrate.FormatTimestamp(time.Now().Add(assignTaskGrantTTL)),
					// A STABLE taskId (claimId-seeded, §10.3): every reclaim re-supplies
					// the same id, so a re-dispatched CreateTask collapses on the existing
					// task (the CreateTask script's kv.Read no-op + the CreateOnly
					// backstop) instead of spawning a duplicate per mark-lease expiry.
					"taskId":           deriveStableTaskID(targetID, entityID, gapColumn, claimID),
					"expectedRevision": expectedRevision,
				}
			},
		}, nil

	case actionDirectOp:
		if ga.Operation == "" {
			return nil, &planError{kind: errConfig, msg: "directOp requires an operation"}
		}
		if strings.HasPrefix(ga.Operation, rowTemplatePrefix) {
			return nil, &planError{kind: errConfig, msg: "directOp operation must be a literal operationType"}
		}
		authTarget := ""
		if ga.Target != "" {
			t, perr := resolveStringParam("target", ga.Target, row)
			if perr != nil {
				return nil, perr
			}
			authTarget = t
		}
		params := make(map[string]any, len(ga.Params)+1)
		for name, v := range ga.Params {
			resolved, perr := resolveParam(name, v, row)
			if perr != nil {
				return nil, perr
			}
			params[name] = resolved
		}
		params["expectedRevision"] = expectedRevision
		// The dispatched op's reads: each is a literal or a row.<column> template
		// (e.g. row.entityKey to hand the op its candidate vertex). The candidate
		// key is already in the lens row, so this just routes it into the op's
		// ContextHint.Reads so its DDL can hydrate + validate it.
		var reads []string
		for i, rt := range ga.Reads {
			r, perr := resolveStringParam(fmt.Sprintf("reads[%d]", i), rt, row)
			if perr != nil {
				return nil, perr
			}
			reads = append(reads, r)
		}
		return &plan{
			operationType: ga.Operation,
			authTarget:    authTarget,
			payload:       func(string) map[string]any { return params },
			reads:         reads,
		}, nil

	default:
		return nil, &planError{kind: errConfig, msg: fmt.Sprintf("unknown action %q", ga.Action)}
	}
}

// resolveParam resolves one playbook param value: either a literal or the
// token row.<column> substituted from the violation row. A row.<column> that
// resolves null/absent is a data error — surface, do not fire a malformed
// remediation (§10.8 Templating).
func resolveParam(name, value string, row map[string]any) (any, *planError) {
	if value == "" {
		return nil, &planError{kind: errConfig, msg: fmt.Sprintf("param %q is required", name)}
	}
	col, templated := strings.CutPrefix(value, rowTemplatePrefix)
	if !templated {
		return value, nil
	}
	v, ok := row[col]
	if !ok || v == nil {
		return nil, &planError{kind: errData,
			msg: fmt.Sprintf("param %q references row.%s, which is null/absent in the row", name, col)}
	}
	return v, nil
}

// resolveStringParam resolves a param that must produce a non-empty string
// (keys, operation types, pattern refs).
func resolveStringParam(name, value string, row map[string]any) (string, *planError) {
	v, perr := resolveParam(name, value, row)
	if perr != nil {
		return "", perr
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", &planError{kind: errData,
			msg: fmt.Sprintf("param %q must resolve to a non-empty string (got %T)", name, v)}
	}
	return s, nil
}
