package bespokecontracts

// clauseDDLScript handles CreateClause + InspectPremises. Known-key reads
// only (validates the lease/account/inspector/conditionedOn vertex by the
// keys the caller lists in ContextHint.Reads). Root data stays {} on the
// clause (D5): the prose/terms/status/inspection are aspects, the governed
// lease, charged account, assigned inspector, and condition are links.
const clauseDDLScript = `
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

def optional_string(p, name):
    if not hasattr(p, name):
        return None
    v = getattr(p, name)
    if v == None or type(v) != type(""):
        return None
    v = v.strip()
    if len(v) == 0:
        return None
    return v

def require_number(p, name):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if v == None or (type(v) != type(0) and type(v) != type(0.0)):
        fail("InvalidArgument: " + name + ": required number")
    return v

def parts_of(key, name, want_type):
    parts = key.split(".")
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

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "CreateClause":
        lease_key = required_string(p, "leaseAppKey")
        _, lease_id = parts_of(lease_key, "leaseAppKey", "leaseapp")
        prose = required_string(p, "prose")
        kind = optional_string(p, "kind")
        if kind == None:
            kind = "computational"
        if kind != "computational" and kind != "judgment":
            fail("InvalidArgument: kind: must be computational or judgment, got " + kind)

        if not vertex_alive(state, lease_key):
            fail("UnknownLeaseApplication: " + lease_key)

        # period (Fire V3): computational-only recurrence selector. A
        # prorated amount (rateCents/periodDays/daysOccupied) is one-time-only
        # — recurring proration is not a shape this fire builds.
        period = optional_string(p, "period")
        if period == None:
            period = "oneTime"
        if period != "oneTime" and period != "monthly":
            fail("InvalidArgument: period: must be oneTime or monthly, got " + period)
        if period == "monthly" and kind != "computational":
            fail("InvalidArgument: period: monthly recurrence is computational-only")

        cond_key = optional_string(p, "conditionedOnKey")
        cond_type = None
        cond_id = None
        if cond_key != None:
            cond_type, cond_id = parts_of(cond_key, "conditionedOnKey", "")
            if not vertex_alive(state, cond_key):
                fail("UnknownConditionVertex: " + cond_key)

        # conditioned is an explicit data flag, not inferred from link/target
        # liveness: a tombstoned conditionedOn TARGET makes the lens's cond
        # match resolve null exactly like "never conditioned" would, so only
        # this flag lets the lens tell the two apart (see lenses.go).
        terms_data = {"kind": kind, "period": period, "conditioned": (cond_key != None)}
        acct_key = None
        acct_id = None
        amount_cents = None
        insp_key = None
        insp_id = None

        if kind == "computational":
            acct_key = required_string(p, "accountKey")
            _, acct_id = parts_of(acct_key, "accountKey", "account")
            if not vertex_alive(state, acct_key):
                fail("UnknownAccount: " + acct_key)

            # Fire V3 proration: rateCents+periodDays+daysOccupied replace a
            # flat amountCents. int(...) forces genuine Starlark bignum
            # integers (never floats) before the multiply/floor-divide, so the
            # result is EXACT — no float64 rounding hazard (the design's §7/R2
            # money-precision rule; this is the "compute Processor-side"
            # option, done once here rather than per-debit).
            has_rate = hasattr(p, "rateCents") and getattr(p, "rateCents") != None
            if has_rate:
                if period != "oneTime":
                    fail("InvalidArgument: rateCents: proration is one-time only; do not combine with a recurring period")
                rate_cents = int(require_number(p, "rateCents"))
                period_days = int(require_number(p, "periodDays"))
                days_occupied = int(require_number(p, "daysOccupied"))
                if rate_cents <= 0:
                    fail("InvalidArgument: rateCents: required positive number")
                if period_days <= 0:
                    fail("InvalidArgument: periodDays: required positive number")
                if days_occupied <= 0 or days_occupied > period_days:
                    fail("InvalidArgument: daysOccupied: required positive number, at most periodDays")
                amount_cents = (rate_cents * days_occupied) // period_days
                if amount_cents <= 0:
                    fail("InvalidArgument: rateCents/periodDays/daysOccupied: computed amountCents must be positive")
                terms_data["basis"] = "daysOccupied"
                terms_data["rateCents"] = rate_cents
                terms_data["periodDays"] = period_days
                terms_data["daysOccupied"] = days_occupied
            else:
                amount_cents = require_number(p, "amountCents")
                if amount_cents <= 0:
                    fail("InvalidArgument: amountCents: required positive number")
            terms_data["amountCents"] = amount_cents
        else:
            insp_key = required_string(p, "inspectorKey")
            _, insp_id = parts_of(insp_key, "inspectorKey", "identity")
            if not vertex_alive(state, insp_key):
                fail("UnknownIdentity: " + insp_key)

        clause_id = nanoid.new()
        clause_key = "vtx.clause." + clause_id

        # Every link the clause writes has the clause as source: it is the
        # later-arriving vertex in each pair (Contract #1 §1.1).
        governs_lnk = "lnk.clause." + clause_id + ".governs.lease." + lease_id

        mutations = [
            make_vtx(clause_key, "clause", {}),
            make_aspect(clause_key, "prose", "clauseProse", {"text": prose}),
            make_aspect(clause_key, "terms", "clauseTerms", terms_data),
            make_aspect(clause_key, "status", "clauseStatus", {"state": "active"}),
            make_link(governs_lnk, clause_key, lease_key, "governs", "governs", {}),
        ]
        event_data = {"clauseKey": clause_key, "leaseAppKey": lease_key, "kind": kind}

        if kind == "computational":
            charges_lnk = "lnk.clause." + clause_id + ".chargesTo.account." + acct_id
            mutations.append(make_link(charges_lnk, clause_key, acct_key, "chargesTo", "chargesTo", {}))
            event_data["accountKey"] = acct_key
            event_data["amountCents"] = amount_cents
        else:
            insp_lnk = "lnk.clause." + clause_id + ".requiresInspectionBy.identity." + insp_id
            mutations.append(make_link(insp_lnk, clause_key, insp_key, "requiresInspectionBy", "requiresInspectionBy", {}))
            event_data["inspectorKey"] = insp_key

        if cond_key != None:
            cond_lnk = "lnk.clause." + clause_id + ".conditionedOn." + cond_type + "." + cond_id
            mutations.append(make_link(cond_lnk, clause_key, cond_key, "conditionedOn", "conditionedOn", {}))
            event_data["conditionedOnKey"] = cond_key

        events = [{"class": "clause.created", "data": event_data}]
        return {"mutations": mutations, "events": events,
                "response": {"primaryKey": clause_key}}

    if ot == "InspectPremises":
        clause_key = required_string(p, "clauseKey")
        parts_of(clause_key, "clauseKey", "clause")

        if not vertex_alive(state, clause_key):
            fail("UnknownClause: " + clause_key)

        # Inspect once: the .inspection aspect is written CreateOnly, so a
        # second InspectPremises with a different requestId conflicts and is
        # rejected (mirrors SignLease's AlreadySigned check).
        insp_aspect_key = clause_key + ".inspection"
        if vertex_alive(state, insp_aspect_key):
            fail("AlreadyInspected: " + clause_key)

        inspected_at = time.rfc3339_utc(op.submittedAt)
        mutations = [
            make_aspect(clause_key, "inspection", "clauseInspection",
                        {"completed": True, "completedAt": inspected_at}),
        ]
        events = [{"class": "clause.inspected", "data": {"clauseKey": clause_key}}]
        return {"mutations": mutations, "events": events,
                "response": {"primaryKey": clause_key}}

    fail("clause DDL: unknown operationType: " + ot)
`

// aspectDeclarationOnlyScript is the declaration-only Starlark for
// clauseProse / clauseTerms / clauseStatus / clauseInspection — written by
// CreateClause's (and, for clauseStatus, DebitAccount's; for
// clauseInspection, InspectPremises's) own op handler, never dispatched as
// an operation in its own right.
const aspectDeclarationOnlyScript = `
def execute(state, op):
    fail("aspect-type DDL: not an operation handler: " + op.operationType)
`
