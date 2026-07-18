package cafedomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// DDLs returns the package's DDL meta-vertex declarations: `tab` (OpenTab,
// Charge, Settle), the `tabStatus` aspect-type declaration (the step-6 write
// gate for the .status aspect the tab vertexType DDL's own script writes),
// and the `cafeOpenTabGuard` aspect-type declaration (the step-6 write gate
// for the per-lease open-tab dedup guard OpenTab/Settle maintain). Mirrors
// the known-key discipline of location-domain / loftspace-domain /
// clinic-domain: every op reads ONLY by known key, no prefix scans, no
// adjacency lookups, no lens-output reads.
//
// A tab is a short-lived POS session against a resident lease, settled into
// cafe-ledger's append-only cafeaccount/cafetransaction ledger (Café Inc 1,
// cafe-ledger-design.md) via the cafeTabSettlement Weaver target (targets.go)
// — cafe-domain's own op scripts never write a cafeaccount/cafetransaction
// mutation directly (the step-6 gate keys PermittedCommands by (operationType,
// class); only cafe-ledger's own DDLs permit CreateAccount/DebitAccount for
// those classes).
func DDLs() []pkgmgr.DDLSpec {
	return []pkgmgr.DDLSpec{
		tabVertexTypeDDL(),
		tabStatusAspectTypeDDL(),
		openTabGuardAspectTypeDDL(),
	}
}

func tabVertexTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "tab",
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"OpenTab", "Charge", "Settle"},
		Description: "Café house-tab session DDL. Vertex shape: vtx.tab.<NanoID>, class=tab, root data = {} " +
			"(minimal, D5 — the running total lives on the .status aspect). OpenTab{leaseAppKey} validates the lease " +
			"is alive, rejects OpenTabAlreadyExists if the lease already has an open tab (the per-lease " +
			"cafeOpenTabGuard aspect on the leaseapp, mirroring cafe-ledger's cafeLedgerAccountGuard: a class-(d) " +
			"optionalReads dedup — create the guard fresh on a lease's first-ever tab, OCC-revive it from its prior " +
			"tombstone on a later one), mints the tab, writes .status {value: open, totalCents: 0, openedAt, " +
			"leaseAppKey} (leaseAppKey denormalized onto .status so Charge/Settle never need a second declared read " +
			"for the link target) and the openFor link (tab→leaseapp). Charge{tabKey, amountCents} adds a positive " +
			"amount to an OPEN tab's running total — an OCC-conditioned upsert of .status keyed on the aspect's own " +
			"current revision (the providerSlotClaim precedent: two concurrent charges racing the same tab must not " +
			"lose an update, so totalCents is a real accumulator, not an idempotent set). Settle{tabKey} closes an " +
			"OPEN tab (.status.value → settled, settledAt stamped, totalCents frozen), also OCC-conditioned, and " +
			"tombstones the lease's cafeOpenTabGuard so a later OpenTab can claim it again. Settling emits tab.settled " +
			"— the cafeTabSettlement lens (lenses.go) picks up a settled tab with totalCents>0 and dispatches the " +
			"resident's café-ledger posting (opening a cafeaccount via CreateAccount on first use, then " +
			"DebitAccount{tabRef}) through Weaver, never a direct cross-package write from this script. Both Charge " +
			"and Settle reject a tab that is not currently open (TabNotOpen) — a settled tab cannot be charged again " +
			"or double-settled. OpenTab and Settle also grant scope=self to consumer: a resident may open or " +
				"settle a tab for their OWN lease only, verified via the lease's applicationFor→identity link " +
				"(AuthDenied otherwise); Charge stays operator-only (no catalog to bound a self-submitted amount).",
		Script: tabDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"leaseAppKey":{"type":"string","description":"vtx.leaseapp.<NanoID> the tab is opened for (OpenTab; required, validated alive)."},` +
			`"tabId":{"type":"string","description":"Optional bare NanoID for the new tab vertex (OpenTab); absent → minted."},` +
			`"tabKey":{"type":"string","description":"vtx.tab.<NanoID> of an existing tab (Charge/Settle; required, validated alive + open)."},` +
			`"amountCents":{"type":"number","description":"The charge amount in integer cents; required, must be > 0 (Charge)."}},` +
			`"required":[]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.tab.<NanoID> the operation wrote."}}}`,
		FieldDescription: map[string]string{
			"leaseAppKey": "Full vtx.leaseapp.<NanoID> key of the resident lease the tab is opened for (OpenTab; required, validated alive). Denormalized onto the tab's own .status aspect so Charge/Settle need no extra declared read to recover it.",
			"tabId":       "Optional bare NanoID (no dots / key segments) for the new tab vertex (vtx.tab.<tabId>). Absent → minted with nanoid.new() (OpenTab).",
			"tabKey":      "Full vtx.tab.<NanoID> key of an existing tab (Charge/Settle; required, validated alive + class=tab + currently open).",
			"amountCents": "The charge amount in integer cents; required, must be a positive number (Charge). Added to the tab's running .status.totalCents.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:    "OpenTab — start a house tab for a resident",
				Payload: map[string]any{"leaseAppKey": "vtx.leaseapp.<NanoID>"},
				ExpectedOutcome: "Validates the lease is alive. Mints vtx.tab.<NanoID> (root {}) + .status " +
					"{value: open, totalCents: 0, openedAt, leaseAppKey} + the openFor link (tab→leaseapp) + claims " +
					"the lease's cafeOpenTabGuard. Returns primaryKey (the tab key). Rejects UnknownLeaseApplication " +
					"if the lease is absent, or OpenTabAlreadyExists if the lease already has an open tab.",
			},
			{
				Name:    "Charge — ring up an item on an open tab",
				Payload: map[string]any{"tabKey": "vtx.tab.<NanoID>", "amountCents": 850},
				ExpectedOutcome: "Validates the tab is alive + open, adds 850 to .status.totalCents (OCC-conditioned " +
					"on the aspect's current revision). Returns primaryKey. Rejects TabNotOpen if the tab is already " +
					"settled, or InvalidArgument if amountCents <= 0.",
			},
			{
				Name:    "Settle — close a tab for house-account posting",
				Payload: map[string]any{"tabKey": "vtx.tab.<NanoID>"},
				ExpectedOutcome: "Validates the tab is alive + open, sets .status.value to settled and stamps " +
					"settledAt (OCC-conditioned; totalCents/leaseAppKey carried over unchanged). Emits tab.settled" +
					"{tabKey, leaseAppKey, totalCents}. Returns primaryKey. Rejects TabNotOpen if already settled.",
			},
		},
	}
}

// tabStatusAspectTypeDDL declares the .status aspect (class tabStatus) — the
// step-6 write gate for OpenTab (mints)/Charge (accumulates)/Settle (closes),
// all owned by the tab vertexType DDL's own script. Declaration-only.
func tabStatusAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "tabStatus",
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"OpenTab", "Charge", "Settle"},
		Description: "Tab status aspect (café). Stored as vtx.tab.<NanoID>.status (class tabStatus) = " +
			"{value: open|settled, totalCents, openedAt, leaseAppKey, settledAt?}. Non-sensitive. Written by OpenTab " +
			"(mints, value=open, totalCents=0), Charge (OCC-conditioned accumulate onto totalCents), and Settle " +
			"(OCC-conditioned close, value=settled, settledAt stamped) — all owned by the tab vertexType DDL's script. " +
			"Declaration-only: no op handler of its own.",
		Script: aspectDeclarationOnlyScript,
		InputSchema: `{"type":"object","properties":` +
			`{"value":{"type":"string","enum":["open","settled"]},"totalCents":{"type":"number"},"openedAt":{"type":"string"},"leaseAppKey":{"type":"string"},"settledAt":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"value":       "open | settled.",
			"totalCents":  "The tab's running total in integer cents, accumulated by Charge.",
			"openedAt":    "When the tab was opened (RFC3339, = OpenTab's op.submittedAt).",
			"leaseAppKey": "The resident lease this tab belongs to (denormalized from OpenTab's payload).",
			"settledAt":   "When the tab was settled (RFC3339, = Settle's op.submittedAt). Absent while open.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "tab status aspect",
				Payload:         map[string]any{"value": "open", "totalCents": 850, "openedAt": "2026-07-07T12:00:00Z", "leaseAppKey": "vtx.leaseapp.<NanoID>"},
				ExpectedOutcome: "Stored as vtx.tab.<NanoID>.status; written by OpenTab/Charge/Settle.",
			},
		},
	}
}

// openTabGuardAspectTypeDDL declares the .cafeOpenTab aspect (class
// cafeOpenTabGuard) OpenTab writes on the PRE-EXISTING leaseapp — the
// deterministic per-lease guard that enforces "at most one OPEN tab per
// lease at a time" (unlike cafe-ledger's cafeLedgerAccountGuard, which is a
// one-time-forever guard: a lease's café account never goes away, but its
// tab is a repeatable session, so this guard is claimed by OpenTab and
// released by Settle, over and over across the lease's life). The local
// name is vertical-prefixed (cafeOpenTab, not openTab) for the same reason
// cafeLedgerAccountGuard is: this leaseapp may carry other packages' own
// guard aspects, and a bare local name risks colliding key-for-key.
// Declaration-only: the aspect is written by OpenTab and tombstoned by
// Settle, never has its own operationType.
func openTabGuardAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "cafeOpenTabGuard",
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"OpenTab", "Settle"},
		Description: "Per-lease open-tab uniqueness guard aspect. Stored as vtx.leaseapp.<NanoID>.cafeOpenTab " +
			"(class cafeOpenTabGuard) = {tabKey: <vtx.tab.<NanoID>>}. Non-sensitive. Claimed by OpenTab: a class-(d) " +
			"optionalReads dedup declared as <leaseAppKey>.cafeOpenTab — absent (the lease's first-ever tab, or any " +
			"prior tab already settled and its guard tombstoned) mints the guard fresh (create-only, the concurrent-" +
			"race backstop); present-but-tombstoned OCC-revives it keyed on its own current revision; present-and-" +
			"alive rejects the new OpenTab with OpenTabAlreadyExists. Released by Settle: an unconditioned tombstone " +
			"(mirrors clinic-domain's slot-cell release — a stale-tombstone race can only free the guard early, " +
			"never leave two tabs open) the moment the tab it names closes, so the very next OpenTab for this lease " +
			"finds it absent-or-tombstoned again.",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{"tabKey":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"tabKey": "The vtx.tab.<NanoID> currently holding this lease's open-tab slot.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "lease open-tab guard aspect",
				Payload:         map[string]any{"tabKey": "vtx.tab.<NanoID>"},
				ExpectedOutcome: "Stored as vtx.leaseapp.<NanoID>.cafeOpenTab; claimed by OpenTab, tombstoned by Settle.",
			},
		},
	}
}

// aspectDeclarationOnlyScript is the declaration-only Starlark for
// tabStatus / cafeOpenTabGuard — written by the tab vertexType DDL's own
// script, never dispatched as an operation in its own right.
const aspectDeclarationOnlyScript = `
def execute(state, op):
    fail("aspect-type DDL: not an operation handler: " + op.operationType)
`

// tabDDLScript handles OpenTab, Charge, Settle. Known-key reads only: Charge
// and Settle both declare tabKey + tabKey+".status" in ContextHint.Reads so
// the current .status revision is hydrated for OCC conditioning (the
// providerSlotClaim precedent — an accumulator must not lose a concurrent
// update, unlike an idempotent status flip's unconditioned upsert). OpenTab
// declares <leaseAppKey>.cafeOpenTab in ContextHint.OptionalReads (Contract
// #2 §2.5 class-(d) read-before-create/dedup) so the per-lease open-tab
// guard's current state — absent, tombstoned, or alive — is hydrated
// without a live GET. A scope=self caller (OpenTab, Settle) additionally
// declares the lease's applicationFor→identity link in OptionalReads (also
// class-(d)) so the resident-self authorization check below can confirm the
// lease belongs to them without a live GET.
const tabDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(vtx_key, local_name, cls, data):
    return {"op": "create", "key": vtx_key + "." + local_name,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

def make_aspect_upsert_occ(vtx_key, local_name, cls, data, expected_revision):
    m = {"op": "update", "key": vtx_key + "." + local_name,
         "document": {"class": cls, "isDeleted": False,
                      "vertexKey": vtx_key, "localName": local_name, "data": data}}
    m["expectedRevision"] = expected_revision
    return m

def make_link(key, source, target, cls, local_name, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False,
                         "sourceVertex": source, "targetVertex": target,
                         "localName": local_name, "data": data}}

def make_tombstone(key):
    return {"op": "tombstone", "key": key,
            "document": {"isDeleted": True, "data": {}}}

def required_string(p, name):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if v == None or type(v) != type("") or len(v.strip()) == 0:
        fail("InvalidArgument: " + name + ": required non-empty string")
    return v.strip()

def require_number(p, name):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if v == None or (type(v) != type(0) and type(v) != type(0.0)):
        fail("InvalidArgument: " + name + ": required number")
    return v

def bare_nanoid_or_mint(p, name):
    if not hasattr(p, name):
        return nanoid.new()
    v = getattr(p, name)
    if v == None:
        return nanoid.new()
    if type(v) != type("") or len(v.strip()) == 0:
        fail("InvalidArgument: " + name + ": must be a non-empty id string")
    v = v.strip()
    for bad in [".", "*", ">", " ", "\t", "\n"]:
        if bad in v:
            fail("InvalidArgument: " + name + ": must carry no dots / key segments, wildcards, or whitespace; got " + v)
    return v

def parts_of(key, name, want_type):
    parts = key.split(".")
    if len(parts) != 3 or parts[0] != "vtx":
        fail("InvalidArgument: " + name + ": required vtx.<type>.<NanoID> (exactly 3 segments); got " + key)
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

def class_of(state, key):
    if key not in state:
        return None
    doc = state[key]
    if doc == None or not hasattr(doc, "class"):
        return None
    return getattr(doc, "class")

def require_open_status(state, tab_key):
    # Every live tab carries a .status aspect (OpenTab writes it atomically
    # with the vertex), so absence here means the caller failed to declare it
    # in ContextHint.Reads, not a legitimately-missing aspect.
    status_key = tab_key + ".status"
    if status_key not in state:
        fail("InvalidArgument: tabKey: caller must declare " + status_key + " in contextHint.reads")
    existing = state[status_key]
    if existing == None or (hasattr(existing, "isDeleted") and existing.isDeleted):
        fail("UnknownTab: " + tab_key + ": no .status aspect")
    if existing.data.get("value") != "open":
        fail("TabNotOpen: " + tab_key + " is " + str(existing.data.get("value")))
    return existing

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "OpenTab":
        lease_key = required_string(p, "leaseAppKey")
        _, lease_id = parts_of(lease_key, "leaseAppKey", "leaseapp")
        if not vertex_alive(state, lease_key):
            fail("UnknownLeaseApplication: " + lease_key)

        # Resident-self (consumer's scope=self grant only): step 3 authorizes
        # scope=self by checking authContext.target == actor (Contract #6),
        # but the op's endpoint is the LEASEAPP, not an identity — step 3
        # never sees the payload and has no notion of "this lease's
        # applicant" anyway. The script closes the gap by requiring the
        # target identity to be the lease's own applicant (lease-signing's
        # applicationFor link, the same patient/identifiedBy indirection
        # clinic-domain's CreateAppointment uses). Empty for the standing
        # operator grant (scope=any never sets authContext), so this check is
        # a no-op there — operator keeps opening tabs on behalf of any lease.
        if op.authContextTarget != "":
            _, target_identity_id = parts_of(op.authContextTarget, "authContextTarget", "identity")
            application_for_lnk = "lnk.leaseapp." + lease_id + ".applicationFor.identity." + target_identity_id
            # read-posture: (d) declared in contextHint.optionalReads by the
            # self-service caller — it already knows both its own leaseAppKey
            # and its own authContext.target before submitting, so it
            # computes this key client-side and declares it.
            if kv.Read(application_for_lnk) == None:
                fail("AuthDenied: a resident may only open a tab for their own lease")

        # One open tab per lease, guarded by a deterministic aspect on the
        # LEASEAPP (not the tab — the tab's own id is independent and
        # unknown until minted below). A class-(d) optionalReads dedup: the
        # caller always declares <leaseAppKey>.cafeOpenTab in
        # contextHint.optionalReads (absence-tolerant, unlike the
        # cafeLedgerAccountGuard precedent's required reads — here a repeat
        # OpenTab across the lease's life is the NORMAL flow, not just a
        # racing retry, so the guard key legitimately may or may not exist
        # yet). Absent → mint the guard fresh (create-only write is the
        # concurrent-race backstop for a genuine first-ever race). Present
        # but tombstoned (a prior tab already settled and released it) →
        # OCC-revive it keyed on its own current revision. Present and
        # alive → this lease already has an open tab, reject cleanly.
        guard_key = lease_key + ".cafeOpenTab"
        if guard_key in state:
            if vertex_alive(state, guard_key):
                fail("OpenTabAlreadyExists: " + lease_key)
            guard_revision = state[guard_key].revision
        else:
            guard_revision = None

        tab_id = bare_nanoid_or_mint(p, "tabId")
        tab_key = "vtx.tab." + tab_id
        opened_at = time.rfc3339_utc(op.submittedAt)

        # openFor: the tab (later-arriving) is the source, the pre-existing
        # lease is the target (Contract #1 §1.1). Reads as "tab openFor lease."
        open_for_lnk = "lnk.tab." + tab_id + ".openFor.leaseapp." + lease_id

        if guard_revision == None:
            guard_mut = make_aspect(lease_key, "cafeOpenTab", "cafeOpenTabGuard", {"tabKey": tab_key})
        else:
            guard_mut = make_aspect_upsert_occ(lease_key, "cafeOpenTab", "cafeOpenTabGuard",
                                                {"tabKey": tab_key}, guard_revision)

        mutations = [
            make_vtx(tab_key, "tab", {}),
            make_aspect(tab_key, "status", "tabStatus",
                        {"value": "open", "totalCents": 0, "openedAt": opened_at, "leaseAppKey": lease_key}),
            make_link(open_for_lnk, tab_key, lease_key, "openFor", "openFor", {}),
            guard_mut,
        ]
        events = [{"class": "tab.opened", "data": {"tabKey": tab_key, "leaseAppKey": lease_key}}]
        return {"mutations": mutations, "events": events,
                "response": {"primaryKey": tab_key}}

    if ot == "Charge":
        tab_key = required_string(p, "tabKey")
        parts_of(tab_key, "tabKey", "tab")
        if not vertex_alive(state, tab_key):
            fail("UnknownTab: " + tab_key)
        if class_of(state, tab_key) != "tab":
            fail("WrongClass: tabKey: " + tab_key)
        amount_cents = require_number(p, "amountCents")
        if amount_cents <= 0:
            fail("InvalidArgument: amountCents: required positive number")

        existing = require_open_status(state, tab_key)
        new_total = existing.data.get("totalCents") + amount_cents
        status_data = {"value": "open", "totalCents": new_total,
                        "openedAt": existing.data.get("openedAt"),
                        "leaseAppKey": existing.data.get("leaseAppKey")}
        mutations = [make_aspect_upsert_occ(tab_key, "status", "tabStatus", status_data, existing.revision)]
        events = [{"class": "tab.charged", "data": {"tabKey": tab_key, "amountCents": amount_cents, "totalCents": new_total}}]
        return {"mutations": mutations, "events": events,
                "response": {"primaryKey": tab_key}}

    if ot == "Settle":
        tab_key = required_string(p, "tabKey")
        parts_of(tab_key, "tabKey", "tab")
        if not vertex_alive(state, tab_key):
            fail("UnknownTab: " + tab_key)
        if class_of(state, tab_key) != "tab":
            fail("WrongClass: tabKey: " + tab_key)

        existing = require_open_status(state, tab_key)
        settled_at = time.rfc3339_utc(op.submittedAt)
        total_cents = existing.data.get("totalCents")
        lease_key = existing.data.get("leaseAppKey")

        # Resident-self (consumer's scope=self grant only): same closure as
        # OpenTab above, but the lease is recovered from the tab's OWN
        # .status aspect (already declared/read for require_open_status),
        # never from caller-supplied payload — a caller declaring the wrong
        # leaseAppKey simply won't have the right composite key pre-hydrated,
        # so the read below returns None and this fails closed regardless.
        if op.authContextTarget != "":
            _, target_identity_id = parts_of(op.authContextTarget, "authContextTarget", "identity")
            lease_id = lease_key.split(".")[2]
            application_for_lnk = "lnk.leaseapp." + lease_id + ".applicationFor.identity." + target_identity_id
            # read-posture: (d) declared in contextHint.optionalReads by the
            # self-service caller (it knows its own tabKey + leaseAppKey +
            # authContext.target before submitting).
            if kv.Read(application_for_lnk) == None:
                fail("AuthDenied: a resident may only settle their own tab")

        status_data = {"value": "settled", "totalCents": total_cents,
                        "openedAt": existing.data.get("openedAt"),
                        "leaseAppKey": lease_key, "settledAt": settled_at}
        # Release the lease's open-tab guard so its next OpenTab can claim it
        # again — unconditioned, mirroring clinic-domain's slot-cell release
        # (a stale-tombstone race can only free the guard early, never leave
        # two tabs open; OpenTab's own OCC-revive is what actually
        # serializes a genuine race on the next claim).
        mutations = [
            make_aspect_upsert_occ(tab_key, "status", "tabStatus", status_data, existing.revision),
            make_tombstone(lease_key + ".cafeOpenTab"),
        ]
        events = [{"class": "tab.settled", "data": {"tabKey": tab_key, "leaseAppKey": lease_key, "totalCents": total_cents}}]
        return {"mutations": mutations, "events": events,
                "response": {"primaryKey": tab_key}}

    fail("tab DDL: unknown operationType: " + ot)
`
