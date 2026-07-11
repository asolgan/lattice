package orchestrationbase

// ResolveSubjectParamsHelper is a shared Starlark snippet that resolves an
// externalTask step's subject-templated params (Contract #10 §10.5) against the
// op's JIT-hydrated working set. An externalTask instanceOp DDL includes it
// (concatenated into its script source) and calls resolve_subject_params(...)
// to build the concrete event params before emitting the external.<adapter>
// event.
//
// It defines resolve_subject_params(params, subject_key): a copy of params in
// which every string value shaped subject.data.<field> or
// subject.<aspect>.data.<field> is replaced by the corresponding field read from
// the subject (root or aspect) via §2.5 kv.Read over the hydrated, OCC-snapshot
// state; every other value (a literal string, number, bool, or nested object)
// passes through verbatim. A token resolving to an absent/tombstoned vertex or
// an absent/null field is a LOUD data error (fail()) — the dispatch never sends
// a blank field to a vendor (the §10.8 discipline / FR29 never-silently-drop
// posture).
//
// Mechanism 2 (Andrew, 2026-06-28): resolution lives in the instanceOp DDL on
// the Processor side, NOT in the Loom engine — Loom only declares the read-set
// (the engine's inferExternalTaskReads), Core-KV reads stay inside the
// Processor, and guard evaluation remains the lone Core-KV-read exception.
// subject_key is supplied by the caller (the instanceOp already binds it from
// the op payload); the aspect key resolved here is subject_key + "." + <aspect>,
// matching exactly the key the engine declared in ContextHint.Reads, so the
// read is served from the hydrated snapshot with no extra round-trip.
//
// The grammar mirrors the engine's parseGuardPath (guard.go) and the resolver
// must stay in lockstep with it: subject.data.<field> reads the subject root,
// subject.<aspect>.data.<field> reads the named aspect.
//
// A subject.<aspect>.data.<field> token declares its aspect key in
// contextHint.egressReads, not contextHint.reads (Loom's inferExternalTaskReads,
// sensitive-param-egress design §3.4) — a sensitive aspect therefore hydrates
// as a `$sensitiveRef` marker rather than plaintext. The resolver checks for
// that marker BEFORE the field lookup (§3.3): it has no sensitivity knowledge
// of its own, it only recognizes the marker the Processor authored. When
// present, the token resolves to a `$sensitiveRef` dict carrying the requested
// `field` name for the bridge's post-decrypt extraction at the egress
// boundary; the plaintext absent-field check does not apply (the field is
// legitimately not there — the aspect never decrypted). A non-sensitive
// egressReads aspect hydrates exactly like a plain read, so it falls through
// to the ordinary field lookup unchanged.
const ResolveSubjectParamsHelper = `
def _resolve_subject_token(param_name, token, subject_key):
    rest = token[len("subject."):]
    segs = rest.split(".")
    aspect = ""
    field = ""
    if len(segs) == 2:
        if segs[0] != "data" or segs[1] == "":
            fail("InvalidParamTemplate: " + param_name + " = " + token + " must be subject.data.<field>")
        field = segs[1]
    elif len(segs) == 3:
        if segs[0] == "" or segs[1] != "data" or segs[2] == "":
            fail("InvalidParamTemplate: " + param_name + " = " + token + " must be subject.<aspect>.data.<field>")
        aspect = segs[0]
        field = segs[2]
    else:
        fail("InvalidParamTemplate: " + param_name + " = " + token + " is not subject.data.<field> or subject.<aspect>.data.<field>")

    key = subject_key
    if aspect != "":
        key = subject_key + "." + aspect

    # This one call site serves two dispositions depending on the token
    # shape: when aspect == "" (subject.data.<field>), key is the subject
    # root —
    # read-posture: (a) declared in contextHint.reads unconditionally by
    # Loom's inferExternalTaskReads (internal/loom/externaltask_params.go).
    # When aspect != "" (subject.<aspect>.data.<field>), key is the templated
    # aspect —
    # read-posture: (f) declared in contextHint.egressReads by the same
    # function — every such param token contributes its aspect key there.
    node = kv.Read(key)
    if node == None:
        fail("MissingSubjectData: " + param_name + " = " + token + " (key " + key + " absent)")
    if hasattr(node, "isDeleted") and node.isDeleted:
        fail("MissingSubjectData: " + param_name + " = " + token + " (key " + key + " tombstoned)")
    sref = node.data.get("$sensitiveRef")
    if sref != None:
        egress = dict(sref)
        egress["field"] = field
        return {"$sensitiveRef": egress}
    val = node.data.get(field)
    if val == None:
        fail("MissingSubjectData: " + param_name + " = " + token + " (field " + field + " absent or null)")
    return val

def resolve_subject_params(params, subject_key):
    if params == None:
        return {}
    resolved = {}
    for k in params:
        v = params[k]
        if type(v) == type("") and v.startswith("subject."):
            resolved[k] = _resolve_subject_token(k, v, subject_key)
        else:
            resolved[k] = v
    return resolved
`
