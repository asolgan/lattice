package orchestrationbase

import "github.com/asolgan/lattice/internal/pkgmgr"

// loomLifecycleDDL is the canonical name of the DDL handling the three
// event-only Loom lifecycle ops (Contract #10 §10.9).
const loomLifecycleDDL = "loomLifecycle"

// LoomLifecycleDDL returns the DDL meta-vertex declaration for the three
// event-only Loom lifecycle ops (Contract #10 §10.9):
//
//   - StartLoomPattern{patternRef, subjectKey} → emits loom.patternStarted
//     (posted by the caller — Weaver scope:any / client / fixture)
//   - CompletePattern{instanceId}             → emits loom.patternCompleted
//     (posted by Loom under identity:loom on pattern exhaustion)
//   - FailPattern{instanceId, reason?}        → emits loom.patternFailed
//     (posted by Loom under identity:loom on a rejected/timeout terminal)
//
// Each op is EVENT-ONLY: it produces NO business mutation. The script returns an
// empty `mutations` list and a single `events` entry; the Processor commits a
// tracker-only atomic batch (Contract #4 idempotency infra) and the outbox
// publishes the event. The Loom instance is operational-only — it lives solely
// in loom-state (instance.<instanceId>); there is NO Core-KV instance vertex
// (Contract #10 §10.9). BuildEventList constructs the event independent of
// mutations, so the zero-mutation path is sound (verified by
// processor.TestCommit_ZeroMutationEventOnly).
//
// The event body carries instanceId + the op's fields; instanceId for
// StartLoomPattern is the op's own requestId (a NanoID already), so no minting
// is needed and Loom's cursor presence dedups at-least-once redelivery.
func LoomLifecycleDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     loomLifecycleDDL,
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"StartLoomPattern", "CompletePattern", "FailPattern"},
		Description: "Event-only Loom lifecycle ops (Contract #10 §10.9). StartLoomPattern{patternRef, " +
			"subjectKey} emits loom.patternStarted (posted by the caller; carries authContext.target = " +
			"the loomPattern meta-vertex); CompletePattern{instanceId} emits loom.patternCompleted and " +
			"FailPattern{instanceId, reason?} emits loom.patternFailed (both posted by Loom under " +
			"identity:loom). Each op produces NO business mutation — it writes only the universal " +
			"vtx.op.<requestId> tracker and emits one event through the Processor → outbox → core-events " +
			"(P2; never a direct publish). The Loom instance is operational-only: it lives solely in " +
			"loom-state, with NO Core-KV instance vertex. instanceId for StartLoomPattern is the op's own " +
			"requestId.",
		Script: loomLifecycleScript,
		InputSchema: `{"type":"object","oneOf":[` +
			`{"properties":{"patternRef":{"type":"string"},"subjectKey":{"type":"string"}},"required":["patternRef","subjectKey"]},` +
			`{"properties":{"instanceId":{"type":"string"},"reason":{"type":"string"}},"required":["instanceId"]}` +
			`]}`,
		OutputSchema: `{"type":"object","properties":{}}`,
		FieldDescription: map[string]string{
			"patternRef": "vtx.meta.<loomPatternId> (or the bare patternId) of the pattern to start. Carried in authContext.target for per-pattern authorization (§10.8).",
			"subjectKey": "vtx.<subjectType>.<NanoID> — the subject vertex the new instance runs for (must be of the pattern's subjectType).",
			"instanceId": "The Loom instance id (= the StartLoomPattern requestId). Names the instance whose lifecycle this op announces.",
			"reason":     "Optional human-readable failure reason carried on loom.patternFailed.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name: "StartLoomPattern — trigger an onboarding flow",
				Payload: map[string]any{
					"patternRef": "vtx.meta.<onboardingPatternNanoID>",
					"subjectKey": "vtx.identity.<applicantNanoID>",
				},
				ExpectedOutcome: "Commits a tracker-only atomic batch (no business mutation) and emits " +
					"events.loom.patternStarted carrying {instanceId=requestId, patternRef, subjectKey}. " +
					"Loom's fixed trigger consumer creates the instance cursor and submits step 0.",
			},
			{
				Name: "CompletePattern — announce an exhausted flow",
				Payload: map[string]any{
					"instanceId": "<instanceNanoID>",
				},
				ExpectedOutcome: "Emits events.loom.patternCompleted carrying {instanceId}. No mutation.",
			},
		},
	}
}

// loomLifecycleScript handles the three event-only lifecycle ops. Each branch
// returns an empty mutations list and a single event; the event class is the
// loom.* class whose first segment is the `loom` domain.
const loomLifecycleScript = `
def required_string(p, name):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if v == None or type(v) != type("") or len(v.strip()) == 0:
        fail("InvalidArgument: " + name + ": required non-empty string")
    return v.strip()

def optional_string(p, name):
    if not hasattr(p, name):
        return ""
    v = getattr(p, name)
    if v == None or type(v) != type(""):
        return ""
    return v.strip()

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "StartLoomPattern":
        pattern_ref = required_string(p, "patternRef")
        subject_key = required_string(p, "subjectKey")
        events = [{"class": "loom.patternStarted",
                   "data": {"instanceId": op.requestId,
                            "patternRef": pattern_ref,
                            "subjectKey": subject_key,
                            "requestId": op.requestId}}]
        return {"mutations": [], "events": events}

    if ot == "CompletePattern":
        instance_id = required_string(p, "instanceId")
        events = [{"class": "loom.patternCompleted",
                   "data": {"instanceId": instance_id, "requestId": op.requestId}}]
        return {"mutations": [], "events": events}

    if ot == "FailPattern":
        instance_id = required_string(p, "instanceId")
        reason = optional_string(p, "reason")
        events = [{"class": "loom.patternFailed",
                   "data": {"instanceId": instance_id, "reason": reason,
                            "requestId": op.requestId}}]
        return {"mutations": [], "events": events}

    fail("loomLifecycle DDL: unknown operationType: " + ot)
`

// LoomLifecyclePermissions returns the permission grants for the lifecycle ops.
//
// StartLoomPattern is granted to operator at scope:any — the only caller Phase 2
// exercises (Weaver holds operator-equivalent root authority via its service
// actor; §10.8). CompletePattern/FailPattern are posted by Loom's identity:loom
// service actor, which is operator-equivalent (holdsRole → operator), so they
// are likewise granted to operator at scope:any. Per-pattern scope:specific for
// external callers is the documented Phase-3 carry (§10.8) and is NOT seeded.
func LoomLifecyclePermissions() []pkgmgr.PermissionSpec {
	return []pkgmgr.PermissionSpec{
		{
			OperationType: "StartLoomPattern",
			Scope:         "any",
			Note:          "Authorizes starting any Loom pattern (Weaver / operator scope:any, §10.8). Carries authContext.target = the loomPattern meta-vertex.",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "CompletePattern",
			Scope:         "any",
			Note:          "Authorizes Loom (identity:loom, operator-equivalent) to announce a completed pattern.",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "FailPattern",
			Scope:         "any",
			Note:          "Authorizes Loom (identity:loom, operator-equivalent) to announce a failed pattern.",
			GrantsTo:      []string{"operator"},
		},
	}
}
