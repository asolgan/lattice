package augur

import "github.com/asolgan/lattice/internal/pkgmgr"

// DDLs returns the package's DDL meta-vertex declarations.
//
// Single DDL `augurproposal` (vertex-type class) handles the RecordProposal
// operation — the bridge replyOp that records an AI-reasoned remediation
// proposal as an auditable Core-KV vertex.
//
// Architectural rules (binding — same known-key discipline as orchestration-base
// / rbac-domain):
//
//   - The script reads ONLY by known key. RecordProposal validates its two link
//     endpoints (the escalated weaver target meta + the candidate vertex) by
//     reading each by its known key — exactly the keys the caller lists in
//     ContextHint.Reads. No prefix scans, no adjacency lookups, no lens reads.
//   - No-orphan invariant (FR29 / P4): RecordProposal REQUIRES the candidate and
//     the weaver target to be alive (the forCandidate / forTarget links need live
//     targets) and rejects (structured ScriptError) if either is absent.
//   - The deterministic-validation boundary (design §5, record-time leg) is the
//     safety core: the proposal is stored `pending` (dispatchable) only when the
//     proposed action is in the allowed escalation vocabulary, the confidence is
//     a real 0..1 score, and the proposal does not escape the escalated
//     candidate's scope. Any failure stores the proposal `invalid` with an
//     auditable reason — never `pending`, never dispatchable. The proposal is
//     ALWAYS stored (auditability); the verdict decides only pending vs invalid.
//
// Proposal shape (Contract #1 key shapes; D5 — minimal root, business data in
// aspects):
//
//	vtx.augurproposal.<id>   root data = {}
//	  .gap         { targetId, entityId, gapColumn, trigger }
//	  .proposed    { action, params }
//	  .rationale   { text }
//	  .confidence  { score }
//	  .provenance  { model, promptHash, catalogHash, reasonedAt }
//	  .review      { state, invalidReason, reviewedAt, dispatchedAt }
//	lnk.augurproposal.<id>.forCandidate.<type>.<entityId>   # proposal forCandidate candidate
//	lnk.augurproposal.<id>.forTarget.meta.<weaverTargetId>  # proposal forTarget target
//
// Both links: augurproposal = the later-arriving SOURCE, the other vertex
// pre-exists = the TARGET (Contract #1 §1.1). The names pass the sentence test
// ("proposal forCandidate candidate", "proposal forTarget target").
//
// Caller's ContextHint.Reads MUST include, for RecordProposal:
//   - targetId  (vtx.meta.<weaverTargetId>)
//   - entityId  (vtx.<type>.<id>)
func DDLs() []pkgmgr.DDLSpec {
	return []pkgmgr.DDLSpec{
		augurproposalDDL(),
	}
}

func augurproposalDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "augurproposal",
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"RecordProposal"},
		Description: "Augur proposal DDL. Vertex shape: vtx.augurproposal.<NanoID>, class=augurproposal, " +
			"root data = {} (D5); business data in aspects: .gap {targetId, entityId, gapColumn, trigger}, " +
			".proposed {action, params}, .rationale {text}, .confidence {score}, .provenance {model, " +
			"promptHash, catalogHash, reasonedAt}, .review {state, invalidReason, reviewedAt, dispatchedAt}. " +
			"Relationships are LINKS: forCandidate (proposal→candidate: the escalated entity), forTarget " +
			"(proposal→weaverTarget meta: the target whose gap was stuck). Both links: proposal is the " +
			"later-arriving source, the other vertex is the pre-existing target (Contract #1 §1.1). " +
			"RecordProposal is the bridge replyOp; it requires + validates the candidate and weaver target " +
			"(no-orphan, FR29/P4) and applies the design §5 record-time deterministic-validation boundary: a " +
			"proposal is stored review.state=pending (dispatchable) only when its action is in the allowed " +
			"escalation vocabulary (triggerLoom|assignTask|directOp), its confidence is a real 0..1 score, and " +
			"it does not escape the escalated candidate's scope; otherwise it is stored review.state=invalid " +
			"with an auditable invalidReason (never pending, never dispatchable). The proposal is always " +
			"stored. Idempotent on a redelivered reply via the caller-supplied deterministic proposalId.",
		Script: augurproposalDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"targetId":{"type":"string","description":"vtx.meta.<NanoID> — the weaver target whose unplannable gap was escalated."},` +
			`"entityId":{"type":"string","description":"vtx.<type>.<NanoID> — the candidate (violating row's entity) the gap was reasoned about."},` +
			`"gapColumn":{"type":"string","description":"The missing_<g> gap column that was stuck."},` +
			`"trigger":{"type":"string","description":"The escalation trigger: unplannable | exhausted."},` +
			`"action":{"type":"string","description":"The model's proposed remediation action; must be one of triggerLoom, assignTask, directOp or the proposal is stored invalid."},` +
			`"params":{"type":"object","description":"The proposed action's params. A param naming an entity other than the escalated candidate stores the proposal invalid (scope escape)."},` +
			`"rationale":{"type":"string","description":"The model's free-form reasoning, stored on the .rationale aspect for audit."},` +
			`"confidence":{"type":"number","description":"The model's 0..1 self-reported confidence. Out of range stores the proposal invalid."},` +
			`"model":{"type":"string","description":"Provenance: the model id that reasoned (e.g. claude-opus-4-8)."},` +
			`"promptHash":{"type":"string","description":"Provenance: hash of the exact prompt reasoned over."},` +
			`"catalogHash":{"type":"string","description":"Provenance: hash of the action catalog reasoned over (detects a stale proposal when the catalog later changes)."},` +
			`"reasonedAt":{"type":"string","description":"Provenance: RFC3339 timestamp of the reasoning call."},` +
			`"proposalId":{"type":"string","description":"Optional bare NanoID for the proposal vertex; the bridge replyOp supplies one derived deterministically from the escalation episode so a redelivered reply collapses on the existing vertex. Absent → minted internally."}},` +
			`"required":["targetId","entityId","gapColumn","trigger","action","confidence"]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.augurproposal.<NanoID> of the recorded proposal (the operation's principal key). The recorded review.state (pending | invalid) is read from the proposal's .review aspect, not the op response."}}}`,
		FieldDescription: map[string]string{
			"targetId":   "Full vtx.meta.<NanoID> key of the escalated weaver target. Required; RecordProposal rejects if absent/invalid (the forTarget link needs a live target).",
			"entityId":   "Full vtx.<type>.<NanoID> key of the escalated candidate entity. Required; RecordProposal rejects if absent/invalid (the forCandidate link needs a live target).",
			"gapColumn":  "The missing_<g> gap column the target projected as violating. Stored on the .gap aspect.",
			"trigger":    "The escalation trigger that fired the reasoning call: unplannable (no playbook entry) or exhausted (retry budget spent). Stored on the .gap aspect.",
			"action":     "The model's proposed remediation action. Validated at record time against the allowed escalation vocabulary {triggerLoom, assignTask, directOp}; an out-of-vocabulary action stores the proposal invalid.",
			"params":     "The proposed action's params object. A param naming an entity other than the escalated candidate (scope escape) stores the proposal invalid — the model cannot propose acting on a different entity than the gap it reasoned about.",
			"rationale":  "The model's free-form reasoning text, stored on the .rationale aspect for the audit trail. Optional.",
			"confidence": "The model's 0..1 self-reported confidence. A value outside [0,1] stores the proposal invalid. Stored on the .confidence aspect.",
			"model":      "Provenance: the model id that produced the proposal (e.g. claude-opus-4-8). Stored on the .provenance aspect.",
			"promptHash": "Provenance: a hash of the exact prompt reasoned over, for audit + stale-proposal detection. Stored on the .provenance aspect.",
			"catalogHash": "Provenance: a hash of the action catalog reasoned over; lets a reviewer detect a proposal that reasoned over a since-changed catalog. Stored on the .provenance aspect.",
			"reasonedAt": "Provenance: RFC3339 timestamp of the reasoning call. Stored on the .provenance aspect.",
			"proposalId": "Optional bare NanoID (no dots / key segments) for the proposal vertex (vtx.augurproposal.<proposalId>). The bridge replyOp supplies one derived deterministically from the escalation episode so a redelivered reply collapses on the existing vertex (CreateOnly + the existing-key read). Absent → minted with nanoid.new().",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name: "RecordProposal — a valid assignTask proposal for an unplannable approval gap",
				Payload: map[string]any{
					"targetId":   "vtx.meta.<weaverTargetNanoID>",
					"entityId":   "vtx.leaseapp.<applicantNanoID>",
					"gapColumn":  "missing_approval",
					"trigger":    "unplannable",
					"action":     "assignTask",
					"params":     map[string]any{"scopedTo": "vtx.leaseapp.<applicantNanoID>", "forOperation": "ApproveLeaseApplication"},
					"rationale":  "No playbook entry; the closest catalog action is a human approval task scoped to the applicant.",
					"confidence": 0.82,
					"model":      "claude-opus-4-8",
					"reasonedAt": "2026-06-29T15:00:00Z",
				},
				ExpectedOutcome: "Validates the weaver target + candidate exist. The action is in the allowed vocabulary, " +
					"confidence is in [0,1], and the proposed scopedTo matches the escalated candidate, so the proposal is " +
					"stored vtx.augurproposal.<id> with review.state=pending (dispatchable) + its forCandidate/forTarget links. " +
					"Returns {primaryKey}; the .review aspect carries state=pending.",
			},
			{
				Name: "RecordProposal — a scope-escaping proposal is stored invalid (auditable, never dispatchable)",
				Payload: map[string]any{
					"targetId":   "vtx.meta.<weaverTargetNanoID>",
					"entityId":   "vtx.leaseapp.<applicantNanoID>",
					"gapColumn":  "missing_approval",
					"trigger":    "unplannable",
					"action":     "directOp",
					"params":     map[string]any{"scopedTo": "vtx.leaseapp.<aDifferentApplicantNanoID>"},
					"confidence": 0.95,
				},
				ExpectedOutcome: "The proposed scopedTo names a DIFFERENT entity than the escalated candidate, so the §5 " +
					"scope-escape check fails: the proposal is still stored (auditability) but with review.state=invalid + an " +
					"invalidReason, never pending, never dispatchable. Returns {primaryKey}; the .review aspect carries state=invalid.",
			},
		},
	}
}

// augurproposalDDLScript handles RecordProposal. Known-key reads only (validates
// the two link endpoints by the keys the caller listed in ContextHint.Reads).
// The §5 record-time deterministic-validation boundary decides pending vs invalid;
// the proposal is always stored (auditability). No-orphan by construction.
const augurproposalDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(vtx_key, local_name, cls, data):
    return {"op": "create", "key": vtx_key + "." + local_name,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

def make_link(key, source, target, cls, local_name, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False,
                         "sourceVertex": source, "targetVertex": target,
                         "localName": local_name, "data": data}}

def required_string(p, name):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if v == None or type(v) != type("") or len(v.strip()) == 0:
        fail("InvalidArgument: " + name + ": required non-empty string")
    return v.strip()

def required_number(p, name):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if v == None or (type(v) != type(0) and type(v) != type(0.0)):
        fail("InvalidArgument: " + name + ": required number")
    return v

def optional_string(p, name):
    if not hasattr(p, name):
        return ""
    v = getattr(p, name)
    if v == None or type(v) != type(""):
        return ""
    return v.strip()

def optional_dict(p, name):
    if not hasattr(p, name):
        return {}
    v = getattr(p, name)
    if v == None or type(v) != type({}):
        return {}
    return v

def split_key(k):
    return k.split(".")

def parts_of(key, name, want_type):
    parts = split_key(key)
    if len(parts) != 3 or parts[0] != "vtx":
        fail("InvalidArgument: " + name + ": required vtx.<type>.<NanoID> (exactly 3 segments); got " + key)
    if parts[1] == "":
        fail("InvalidArgument: " + name + ": empty type segment; required vtx.<type>.<NanoID>; got " + key)
    if want_type != "" and parts[1] != want_type:
        fail("InvalidArgument: " + name + ": required vtx." + want_type + ".<NanoID>; got " + key)
    return parts[1], parts[2]

def vertex_alive(state, key):
    if key not in state:
        return False
    doc = state[key]
    if doc == None:
        return False
    if hasattr(doc, "isDeleted") and doc.isDeleted:
        return False
    return True

def bare_nanoid_or_mint(p):
    # The bridge replyOp supplies a deterministic proposalId so a redelivered
    # reply collapses on the existing vertex. A bare NanoID carries no dots / key
    # segments so "vtx.augurproposal." + id is a single well-formed vertex key.
    if not hasattr(p, "proposalId"):
        return nanoid.new()
    v = getattr(p, "proposalId")
    if v == None:
        return nanoid.new()
    if type(v) != type("") or len(v.strip()) == 0:
        fail("InvalidArgument: proposalId: must be a non-empty bare NanoID string")
    v = v.strip()
    for bad in [".", "*", ">", " ", "\t", "\n"]:
        if bad in v:
            fail("InvalidArgument: proposalId: must be a bare NanoID (no dots / key segments, wildcards, or whitespace); got " + v)
    return v

# The allowed escalation action vocabulary (design §5). A proposal naming any
# other action is stored invalid — the model gains NO new authority, only the
# ability to PROPOSE arranging the actions Weaver already has.
ALLOWED_ACTIONS = ["triggerLoom", "assignTask", "directOp"]

# The param keys that name an entity. A proposed action whose entity-naming param
# references a candidate OTHER than the escalated one is a scope escape (design
# §5): the model cannot propose acting on a different entity than the gap it was
# asked to reason about. The check compares against both the full vertex key and
# the bare NanoID, since a param may carry either form.
ENTITY_PARAM_KEYS = ["scopedTo", "subject", "subjectKey", "entity", "entityKey", "candidate"]

def scope_escape(params, entity_key, entity_id):
    # Returns the offending param name (non-empty) on a scope escape, else "".
    for k in ENTITY_PARAM_KEYS:
        if k not in params:
            continue
        v = params[k]
        if v == None or type(v) != type("") or len(v.strip()) == 0:
            continue
        v = v.strip()
        if v != entity_key and v != entity_id:
            return k
    return ""

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "RecordProposal":
        # --- envelope (structural preconditions; a malformed op is rejected) ---
        target_key = required_string(p, "targetId")
        entity_key = required_string(p, "entityId")
        gap_column = required_string(p, "gapColumn")
        trigger = required_string(p, "trigger")
        action = required_string(p, "action")
        confidence = required_number(p, "confidence")
        rationale = optional_string(p, "rationale")
        params = optional_dict(p, "params")
        model = optional_string(p, "model")
        prompt_hash = optional_string(p, "promptHash")
        catalog_hash = optional_string(p, "catalogHash")
        reasoned_at = optional_string(p, "reasonedAt")

        # Validate endpoint key shapes.
        parts_of(target_key, "targetId", "meta")
        entity_type, entity_id = parts_of(entity_key, "entityId", "")

        # No-orphan invariant (FR29 / P4): both link endpoints MUST be alive, or
        # no proposal is committed (a proposal pointing at a dead target / candidate
        # is never recorded). The caller lists both in ContextHint.Reads.
        if not vertex_alive(state, target_key):
            fail("UnknownTarget: " + target_key)
        if not vertex_alive(state, entity_key):
            fail("UnknownCandidate: " + entity_key)

        # --- deterministic id + idempotency (Contract #10 §10.3 / §2.5) ---
        # A redelivered reply supplies the SAME deterministic proposalId, so the
        # key is stable; a present, ALIVE proposal means a duplicate reply — a
        # coherent no-op (the CreateOnly mutation below also guards the same-commit
        # concurrent race). kv.Read, NOT a contextHint read: the key may
        # legitimately not exist yet, and a declared-but-absent read faults.
        proposal_id = bare_nanoid_or_mint(p)
        proposal_key = "vtx.augurproposal." + proposal_id
        existing = kv.Read(proposal_key)
        if existing != None and not existing.isDeleted:
            return {"mutations": [], "events": []}

        # --- §5 record-time deterministic validation (the safety core) ---
        # The proposal is ALWAYS stored (auditability); the verdict decides only
        # whether it is pending (dispatchable) or invalid (never dispatchable).
        review_state = "pending"
        invalid_reason = ""
        if action not in ALLOWED_ACTIONS:
            review_state = "invalid"
            invalid_reason = "action not in allowed escalation vocabulary (triggerLoom|assignTask|directOp): " + action
        elif confidence < 0.0 or confidence > 1.0:
            review_state = "invalid"
            invalid_reason = "confidence out of range [0,1]: " + str(confidence)
        else:
            offending = scope_escape(params, entity_key, entity_id)
            if offending != "":
                review_state = "invalid"
                invalid_reason = "scope escape: proposed param '" + offending + "' references an entity other than the escalated candidate " + entity_key

        forcand_lnk = "lnk.augurproposal." + proposal_id + ".forCandidate." + entity_type + "." + entity_id
        target_id = split_key(target_key)[2]
        fortarget_lnk = "lnk.augurproposal." + proposal_id + ".forTarget.meta." + target_id

        mutations = [
            make_vtx(proposal_key, "augurproposal", {}),
            make_aspect(proposal_key, "gap", "augur.gap",
                        {"targetId": target_key, "entityId": entity_key,
                         "gapColumn": gap_column, "trigger": trigger}),
            make_aspect(proposal_key, "proposed", "augur.proposed",
                        {"action": action, "params": params}),
            make_aspect(proposal_key, "rationale", "augur.rationale", {"text": rationale}),
            make_aspect(proposal_key, "confidence", "augur.confidence", {"score": confidence}),
            make_aspect(proposal_key, "provenance", "augur.provenance",
                        {"model": model, "promptHash": prompt_hash,
                         "catalogHash": catalog_hash, "reasonedAt": reasoned_at}),
            make_aspect(proposal_key, "review", "augur.review",
                        {"state": review_state, "invalidReason": invalid_reason,
                         "reviewedAt": "", "dispatchedAt": ""}),
            make_link(forcand_lnk, proposal_key, entity_key, "forCandidate", "forCandidate", {}),
            make_link(fortarget_lnk, proposal_key, target_key, "forTarget", "forTarget", {}),
        ]
        events = [{"class": "augur.proposalRecorded",
                   "data": {"proposalKey": proposal_key, "targetId": target_key,
                            "entityId": entity_key, "gapColumn": gap_column,
                            "action": action, "reviewState": review_state}}]
        return {"mutations": mutations, "events": events,
                "response": {"primaryKey": proposal_key}}

    fail("augurproposal DDL: unknown operationType: " + ot)
`
