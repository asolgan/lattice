package wellnessdomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// Canonical names. Three vertexType DDLs own the op scripts (each op is
// admitted by EXACTLY ONE vertexType DDL — the operationType→script index
// drops an op claimed by two, so no overlap is allowed there). Aspect-type
// DDLs are step-6 write gates only, mirroring clinic-domain's split.
const (
	studioVertexDDL  = "studio"
	sessionVertexDDL = "session"
	bookingVertexDDL = "booking"

	studioProfileAspectDDL    = "studioProfile"
	sessionScheduleAspectDDL  = "sessionSchedule"
	studioSlotClaimAspectDDL  = "studioSlotClaim"
	sessionSeatClaimAspectDDL = "sessionSeatClaim"
	bookingStatusAspectDDL    = "bookingStatus"
)

// DDLs returns the package's eight DDL meta-vertex declarations:
//
//   - studio (vertexType) — owns CreateStudio + TombstoneStudio.
//   - session (vertexType) — owns CreateSession + TombstoneSession.
//   - booking (vertexType) — owns CreateBooking + CancelBooking.
//   - studioProfile / sessionSchedule / studioSlotClaim / sessionSeatClaim /
//     bookingStatus (aspectType) — step-6 write gates.
//
// Architectural rules (binding — the known-key discipline of clinic-domain /
// loftspace-domain): the scripts read ONLY by known key. No prefix scans, no
// kv.Links enumeration, no raw adjacency lookups.
func DDLs() []pkgmgr.DDLSpec {
	return []pkgmgr.DDLSpec{
		studioVertexTypeDDL(),
		sessionVertexTypeDDL(),
		bookingVertexTypeDDL(),
		studioProfileAspectTypeDDL(),
		sessionScheduleAspectTypeDDL(),
		studioSlotClaimAspectTypeDDL(),
		sessionSeatClaimAspectTypeDDL(),
		bookingStatusAspectTypeDDL(),
	}
}

func studioVertexTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     studioVertexDDL,
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"CreateStudio", "TombstoneStudio"},
		Description: "Wellness studio DDL. Vertex shape: vtx.studio.<NanoID>, class=studio, root data = {} " +
			"(minimal, D5 — the data lives in the .profile aspect). CreateStudio mints the studio + writes the " +
			".profile aspect {name (required)} atomically. TombstoneStudio soft-deletes one (no cascade onto its " +
			"sessions — the projection lenses anchor on the live root, mirroring clinic-domain's no-cascade rule).",
		Script: studioDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"name":{"type":"string","description":"The studio's display name (CreateStudio; required)."},` +
			`"studioId":{"type":"string","description":"Optional bare NanoID for the new studio vertex (CreateStudio); absent → minted."},` +
			`"studioKey":{"type":"string","description":"vtx.studio.<NanoID> of an existing studio (TombstoneStudio; required, validated alive)."}},` +
			`"required":[]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.studio.<NanoID> the operation wrote."}}}`,
		FieldDescription: map[string]string{
			"name":      "The studio's display name. Stored on the .profile aspect (CreateStudio; required).",
			"studioId":  "Optional bare NanoID (no dots / key segments) for the new studio vertex. Absent → minted with nanoid.new().",
			"studioKey": "Full vtx.studio.<NanoID> key of an existing studio vertex to tombstone (TombstoneStudio).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:    "CreateStudio — register a studio",
				Payload: map[string]any{"name": "Sunrise Yoga Room"},
				ExpectedOutcome: "Mints vtx.studio.<NanoID> (class=studio, root {}) + the .profile aspect " +
					"{name}. Returns primaryKey (the studio key).",
			},
			{
				Name:            "TombstoneStudio — remove a studio",
				Payload:         map[string]any{"studioKey": "vtx.studio.<NanoID>"},
				ExpectedOutcome: "Soft-deletes the studio vertex. Returns primaryKey. Rejects an absent / already-dead studio.",
			},
		},
	}
}

func studioProfileAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     studioProfileAspectDDL,
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateStudio"},
		Description: "Studio profile aspect (wellness). Stored as vtx.studio.<NanoID>.profile (class " +
			"studioProfile) = {name}. Non-sensitive. Written by CreateStudio (whose studio vertexType DDL owns " +
			"the script); this aspect-type DDL is the step-6 write gate. Declaration-only: no op handler.",
		Script: aspectDeclarationOnlyScript,
		InputSchema: `{"type":"object","properties":` +
			`{"name":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"name": "The studio's display name.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "studio profile aspect",
				Payload:         map[string]any{"name": "Sunrise Yoga Room"},
				ExpectedOutcome: "Stored as vtx.studio.<NanoID>.profile; written by CreateStudio.",
			},
		},
	}
}

func sessionVertexTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     sessionVertexDDL,
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"CreateSession", "TombstoneSession"},
		Description: "Wellness session DDL. Vertex shape: vtx.session.<NanoID>, class=session, root data = {} " +
			"(minimal, D5). CreateSession validates the studio is alive + class=studio, then atomically mints the " +
			"session + the .schedule aspect {name, startsAt, endsAt, capacity} + the atStudio link " +
			"(session→studio, Contract #1 §1.1 later-arriving source). The studio's booking grid is a mandatory " +
			"15-minute cadence (mirrors clinic-domain's appointment grid exactly): CreateSession discretizes " +
			"[startsAt,endsAt) into its covered 15-minute cells and CLAIMS a deterministic studioSlotClaim aspect " +
			"per cell on the studio hub (vtx.studio.<s>.slot<cellcode>) — the write-path CreateOnly/expectedRevision " +
			"conditioning on each cell key IS the double-book lock (Capability-KV §06 — the op's own Starlark " +
			"logic): a live claim on any covered cell rejects with StudioConflict (no two overlapping sessions in " +
			"the same studio). TombstoneSession requires the session's actual studio (verified via the atStudio " +
			"link) to release the held slot cells in the same atomic batch, then soft-deletes the session (no " +
			"cascade onto its bookings — they simply drop from the wellnessBookings roster's session join).",
		Script: sessionDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"studio":{"type":"string","description":"vtx.studio.<NanoID> the session runs at (CreateSession; required, validated alive + class=studio; on TombstoneSession it must be the session's actual studio, validated via the atStudio link)."},` +
			`"name":{"type":"string","description":"The session's display name, e.g. Vinyasa Flow (CreateSession; required)."},` +
			`"startsAt":{"type":"string","description":"Session start, RFC3339 (CreateSession; required). Aligned to the 15-minute booking grid (:00/:15/:30/:45; SlotGridViolation otherwise)."},` +
			`"endsAt":{"type":"string","description":"Session end, RFC3339 (CreateSession; required). Aligned to the 15-minute grid; span capped at 96 cells / 24h (SessionTooLong)."},` +
			`"capacity":{"type":"integer","description":"Maximum concurrent bookings (CreateSession; required, 1..200). Bounds the seat-claim loop CreateBooking walks."},` +
			`"sessionId":{"type":"string","description":"Optional bare NanoID for the new session vertex (CreateSession); absent → minted."},` +
			`"sessionKey":{"type":"string","description":"vtx.session.<NanoID> of an existing session (TombstoneSession; required, validated alive)."}},` +
			`"required":[]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.session.<NanoID> the operation wrote."}}}`,
		FieldDescription: map[string]string{
			"studio":     "Full vtx.studio.<NanoID> key the session runs at. CreateSession validates it is alive + class=studio, writes the atStudio link, and claims one studioSlotClaim aspect per covered 15-minute cell. TombstoneSession also requires it (the session's actual studio, validated via the atStudio link) to release the held cells.",
			"name":       "The session's display name (CreateSession; required).",
			"startsAt":   "Session start (RFC3339, canonical UTC). Stored on the .schedule aspect (CreateSession; required). Must align to the 15-minute grid (SlotGridViolation).",
			"endsAt":     "Session end (RFC3339, canonical UTC). Stored on the .schedule aspect (CreateSession; required). Must align to the 15-minute grid; span capped at 96 cells / 24h (SessionTooLong).",
			"capacity":   "Maximum concurrent bookings, an integer 1..200 (CreateSession; required). Stored on the .schedule aspect; CreateBooking reads it to bound the seat-claim loop (SessionFull once exhausted).",
			"sessionId":  "Optional bare NanoID (no dots / key segments) for the new session vertex. Absent → minted with nanoid.new().",
			"sessionKey": "Full vtx.session.<NanoID> key of an existing session (TombstoneSession releases its held studioSlotClaim cells then tombstones it).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name: "CreateSession — schedule a class",
				Payload: map[string]any{
					"studio":   "vtx.studio.<NanoID>",
					"name":     "Vinyasa Flow",
					"startsAt": "2026-07-08T09:00:00Z",
					"endsAt":   "2026-07-08T10:00:00Z",
					"capacity": 20,
				},
				ExpectedOutcome: "Validates the studio is alive + class=studio and startsAt/endsAt align to the " +
					"15-minute grid. Atomically commits vtx.session.<NanoID> (root {}) + .schedule {name, startsAt, " +
					"endsAt, capacity} + the atStudio link + one studioSlotClaim aspect per covered 15-minute cell. " +
					"Returns primaryKey. Rejects on an absent/dead/wrong-class studio, a misaligned start/end " +
					"(SlotGridViolation), or a studio double-book (StudioConflict).",
			},
			{
				Name:    "TombstoneSession — cancel a scheduled session",
				Payload: map[string]any{"sessionKey": "vtx.session.<NanoID>", "studio": "vtx.studio.<NanoID>"},
				ExpectedOutcome: "Validates the session is alive + class=session and the supplied studio is its " +
					"actual studio (via the atStudio link), releases every held studioSlotClaim cell, then soft-" +
					"deletes the session. Returns primaryKey.",
			},
		},
	}
}

func sessionScheduleAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     sessionScheduleAspectDDL,
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateSession"},
		Description: "Session schedule aspect (wellness). Stored as vtx.session.<NanoID>.schedule (class " +
			"sessionSchedule) = {name, startsAt, endsAt, capacity}. Non-sensitive. Written by CreateSession (whose " +
			"session vertexType DDL owns the script); this aspect-type DDL is the step-6 write gate. Declaration-" +
			"only: no op handler. CreateBooking reads capacity on demand (kv.Read) to bound its seat-claim loop.",
		Script: aspectDeclarationOnlyScript,
		InputSchema: `{"type":"object","properties":` +
			`{"name":{"type":"string"},"startsAt":{"type":"string"},"endsAt":{"type":"string"},"capacity":{"type":"integer"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"name":     "The session's display name.",
			"startsAt": "Session start (RFC3339).",
			"endsAt":   "Session end (RFC3339).",
			"capacity": "Maximum concurrent bookings (integer 1..200).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "session schedule aspect",
				Payload:         map[string]any{"name": "Vinyasa Flow", "startsAt": "2026-07-08T09:00:00Z", "endsAt": "2026-07-08T10:00:00Z", "capacity": 20},
				ExpectedOutcome: "Stored as vtx.session.<NanoID>.schedule; written by CreateSession.",
			},
		},
	}
}

// studioSlotClaimAspectTypeDDL declares the .slot<cellcode> aspect (class
// studioSlotClaim) — a deterministic per-15-minute-cell existence marker on
// the studio hub. The step-6 write gate for CreateSession / TombstoneSession
// (create / release). Declaration-only; NON-sensitive. Mirrors clinic-domain's
// providerSlotClaimAspectTypeDDL exactly, renamed hub (studio, not provider) —
// see wellness-vertical-design.md §1. One aspect per occupied grid cell,
// created ON DEMAND — never pre-seeded by CreateStudio.
func studioSlotClaimAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     studioSlotClaimAspectDDL,
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateSession", "TombstoneSession"},
		Description: "Studio 15-minute slot-claim aspect (wellness). Stored as vtx.studio.<NanoID>.slot<cellcode> " +
			"(class studioSlotClaim) = {} — a pure existence marker, no relationship field. <cellcode> is the " +
			"cell's canonical whole-second UTC start with '-'/':' stripped and lowercased. CreateSession claims " +
			"one per covered cell (CreateOnly — the key collision across two concurrent sessions for the same cell " +
			"IS the double-book lock: StudioConflict on commit-time rejection); TombstoneSession tombstones all " +
			"held cells on cancellation, freeing them. Non-sensitive; created on demand, no CreateStudio init " +
			"needed. Declaration-only: no op handler.",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"data": "Always {} — a pure existence marker. The claim's job is done by the KEY (hub + deterministic cellcode), never by a field in data.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "studio slot-claim aspect",
				Payload:         map[string]any{},
				ExpectedOutcome: "Stored as vtx.studio.<NanoID>.slot<cellcode>; claimed by CreateSession, released by TombstoneSession.",
			},
		},
	}
}

func bookingVertexTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     bookingVertexDDL,
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"CreateBooking", "CancelBooking"},
		Description: "Wellness booking DDL. Vertex shape: vtx.booking.<NanoID>, class=booking, root data = {} " +
			"(minimal, D5). CreateBooking validates the session is alive + class=session and the booker is alive " +
			"+ class=identity, reads the session's .schedule.capacity, and claims the first free " +
			"vtx.session.<s>.seat<n> for n in 1..capacity (SessionFull once every seat is claimed) — the SAME " +
			"CreateOnly/expectedRevision write-path idiom studioSlotClaim uses, applied over an enumerated seat-" +
			"index dimension instead of a time-cell dimension (Capability-KV §06). It then atomically mints the " +
			"booking + the .status aspect {value: booked, rate, seat} + the forSession link (booking→session) + " +
			"the bookedBy link (booking→identity). Resident-rate: an optional leaseAppKey, when supplied, " +
			"qualifies for rate=resident only when ALL THREE hold: the leaseapp is alive, its .tenancy aspect " +
			"is present (CreateOnly-stamped on the leaseapp's FIRST DecideLeaseApplication approve — the only " +
			"signal an application actually became an active tenancy, not merely pending or declined), and " +
			"lnk.leaseapp.<id>.applicationFor.identity.<bookerId> is live (known-key kv.Read, the lease-signing " +
			"renewal-verification idiom) — a match writes the residentRate link (booking→leaseapp, the " +
			"ratifying audit link a future billing composition lens can walk); failing any one check is NOT a " +
			"hard failure, it falls through to rate=standard (a booker " +
			"naming a lease they don't hold never over-grants the discount, but is still allowed to book). " +
			"CancelBooking validates the booking is alive + class=booking and the supplied session is its actual " +
			"session (via the forSession link), reads the booking's own .status.seat (stored at create time — no " +
			"stored back-reference needed to recompute it), then releases that seat cell and soft-deletes the " +
			"booking.",
		Script: bookingDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"session":{"type":"string","description":"vtx.session.<NanoID> being booked (CreateBooking; required, validated alive + class=session; on CancelBooking it must be the booking's actual session, validated via the forSession link)."},` +
			`"booker":{"type":"string","description":"vtx.identity.<NanoID> making the booking (CreateBooking; required, validated alive + class=identity)."},` +
			`"leaseAppKey":{"type":"string","description":"Optional vtx.leaseapp.<NanoID> the booker claims residency under (CreateBooking; optional). Checked against the lease's applicationFor link — a mismatch falls through to the standard rate, never a hard failure."},` +
			`"bookingId":{"type":"string","description":"Optional bare NanoID for the new booking vertex (CreateBooking); absent → minted."},` +
			`"bookingKey":{"type":"string","description":"vtx.booking.<NanoID> of an existing booking (CancelBooking; required, validated alive)."}},` +
			`"required":[]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.booking.<NanoID> the operation wrote."}}}`,
		FieldDescription: map[string]string{
			"session":     "Full vtx.session.<NanoID> key being booked. CreateBooking validates it is alive + class=session, reads its capacity, claims a free seat, and writes the forSession link. CancelBooking also requires it (the booking's actual session, validated via the forSession link) to release the held seat.",
			"booker":      "Full vtx.identity.<NanoID> key of the person booking. CreateBooking validates it is alive + class=identity and writes the bookedBy link.",
			"leaseAppKey": "Optional full vtx.leaseapp.<NanoID> key the booker claims residency under (CreateBooking). Verified via the lease's applicationFor link before granting rate=resident; a mismatch or absent lease silently falls back to rate=standard.",
			"bookingId":   "Optional bare NanoID (no dots / key segments) for the new booking vertex. Absent → minted with nanoid.new().",
			"bookingKey":  "Full vtx.booking.<NanoID> key of an existing booking to cancel (CancelBooking).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:    "CreateBooking — standard rate",
				Payload: map[string]any{"session": "vtx.session.<NanoID>", "booker": "vtx.identity.<NanoID>"},
				ExpectedOutcome: "Validates the session + booker are alive/typed, claims the first free seat " +
					"(SessionFull if none), and commits vtx.booking.<NanoID> (root {}) + .status {value: booked, " +
					"rate: standard, seat} + forSession + bookedBy links. Returns primaryKey.",
			},
			{
				Name: "CreateBooking — resident rate",
				Payload: map[string]any{
					"session":     "vtx.session.<NanoID>",
					"booker":      "vtx.identity.<NanoID>",
					"leaseAppKey": "vtx.leaseapp.<NanoID>",
				},
				ExpectedOutcome: "As above, but when the supplied leaseAppKey's applicationFor link names this " +
					"booker, .status.rate = resident and a residentRate link (booking→leaseapp) is written. A " +
					"leaseAppKey belonging to a DIFFERENT identity falls through to rate=standard, never rejected.",
			},
			{
				Name:            "CancelBooking — release a seat",
				Payload:         map[string]any{"bookingKey": "vtx.booking.<NanoID>", "session": "vtx.session.<NanoID>"},
				ExpectedOutcome: "Validates the booking is alive + class=booking and the supplied session is its actual session, releases the held seat, and soft-deletes the booking. Returns primaryKey.",
			},
		},
	}
}

func bookingStatusAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     bookingStatusAspectDDL,
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateBooking"},
		Description: "Booking status aspect (wellness). Stored as vtx.booking.<NanoID>.status (class " +
			"bookingStatus) = {value: booked, rate: standard|resident, seat}. Non-sensitive. Written by " +
			"CreateBooking (whose booking vertexType DDL owns the script); this aspect-type DDL is the step-6 " +
			"write gate. seat is internal bookkeeping (the claimed seat index) CancelBooking reads to recompute " +
			"which vtx.session.<s>.seat<n> cell to release, without a stored relationship-as-key-list (Contract " +
			"#1). Declaration-only: no op handler.",
		Script: aspectDeclarationOnlyScript,
		InputSchema: `{"type":"object","properties":` +
			`{"value":{"type":"string","enum":["booked"]},"rate":{"type":"string","enum":["standard","resident"]},"seat":{"type":"integer"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"value": "Booking status: booked (the only value this increment writes).",
			"rate":  "standard | resident, derived by CreateBooking from the optional leaseAppKey residency check.",
			"seat":  "The claimed seat index on the session (internal bookkeeping; CancelBooking reads it to release the correct seat cell).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "booking status aspect",
				Payload:         map[string]any{"value": "booked", "rate": "resident", "seat": 3},
				ExpectedOutcome: "Stored as vtx.booking.<NanoID>.status; written by CreateBooking.",
			},
		},
	}
}

// sessionSeatClaimAspectTypeDDL declares the .seat<n> aspect (class
// sessionSeatClaim) — a deterministic per-seat-index existence marker on the
// session hub. The capacity-bounded extension of studioSlotClaim's exact
// mechanism (CreateOnly key-collision at commit), applied over an enumerated
// seat-index dimension instead of a time-cell dimension — see
// wellness-vertical-design.md §1(2). Declaration-only; NON-sensitive.
func sessionSeatClaimAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     sessionSeatClaimAspectDDL,
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateBooking", "CancelBooking"},
		Description: "Session seat-claim aspect (wellness). Stored as vtx.session.<NanoID>.seat<n> (class " +
			"sessionSeatClaim) = {} — a pure existence marker, no relationship field. <n> is a 1-based seat index, " +
			"1..capacity. CreateBooking walks n=1..capacity in a bounded loop and claims the FIRST cell it reads " +
			"absent (CreateOnly — the key collision across two concurrent bookings racing for the same seat IS the " +
			"capacity lock: two callers both reading a seat absent both emit op:create for the identical key, but " +
			"CreateOnly at revision 0 commits exactly once, the loser's batch RevisionConflicts and the Processor " +
			"retries against the now-live seat). CancelBooking tombstones the ONE seat cell recorded on the " +
			"booking's own .status.seat, freeing it for a future claimant. Non-sensitive; created on demand, no " +
			"CreateSession init needed. Declaration-only: no op handler.",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"data": "Always {} — a pure existence marker. The claim's job is done by the KEY (session hub + seat index), never by a field in data.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "session seat-claim aspect",
				Payload:         map[string]any{},
				ExpectedOutcome: "Stored as vtx.session.<NanoID>.seat<n>; claimed by CreateBooking, released by CancelBooking.",
			},
		},
	}
}

// aspectDeclarationOnlyScript is the shared no-op script for every
// declaration-only aspect-type DDL — its op handler lives on the owning
// vertexType DDL, so this script never executes as a dispatch target (the
// operationType→script index always resolves to the vertexType DDL first).
// Mirrors clinic-domain's identical helper.
const aspectDeclarationOnlyScript = `
def execute(state, op):
    fail("InvalidState: this aspect-type DDL is declaration-only; its op is owned by a vertexType DDL")
`

// studioDDLScript handles CreateStudio + TombstoneStudio. Known-key reads only.
const studioDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(vtx_key, local_name, cls, data):
    return {"op": "create", "key": vtx_key + "." + local_name,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

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

    if ot == "CreateStudio":
        name = required_string(p, "name")
        sid = bare_nanoid_or_mint(p, "studioId")
        skey = "vtx.studio." + sid
        mutations = [
            make_vtx(skey, "studio", {}),
            make_aspect(skey, "profile", "studioProfile", {"name": name}),
        ]
        events = [{"class": "wellness.studioCreated", "data": {"studioKey": skey}}]
        return {"mutations": mutations, "events": events, "response": {"primaryKey": skey}}

    if ot == "TombstoneStudio":
        skey = required_string(p, "studioKey")
        if not vertex_alive(state, skey):
            fail("UnknownStudio: " + skey)
        mutations = [make_tombstone(skey)]
        return {"mutations": mutations, "events": [], "response": {"primaryKey": skey}}

    fail("UnknownOperation: " + ot)
`

// sessionDDLScript handles CreateSession + TombstoneSession, mirroring
// clinic-domain's appointment DDL's slot_cells/claim_cell double-book guard
// exactly (hub renamed provider→studio; no patient-side symmetric claim —
// see wellness-vertical-design.md §1).
const sessionDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(vtx_key, local_name, cls, data):
    return {"op": "create", "key": vtx_key + "." + local_name,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

def make_aspect_upsert_occ(vtx_key, local_name, cls, data, rev):
    return {"op": "update", "key": vtx_key + "." + local_name, "expectedRevision": rev,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

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

def required_int(p, name, lo, hi):
    if not hasattr(p, name):
        fail("InvalidArgument: " + name + ": required")
    v = getattr(p, name)
    if type(v) != type(0):
        fail("InvalidArgument: " + name + ": must be an integer; got " + type(v))
    if v < lo or v > hi:
        fail("InvalidArgument: " + name + ": must be in [" + str(lo) + ", " + str(hi) + "]; got " + str(v))
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

def class_of(state, key):
    if key not in state:
        return None
    doc = state[key]
    if doc == None:
        return None
    if not hasattr(doc, "class"):
        return None
    return getattr(doc, "class")

def require_live_typed(state, key, name, want_class):
    if not vertex_alive(state, key):
        fail("UnknownEndpoint: " + name + ": " + key + " is absent or tombstoned")
    cls = class_of(state, key)
    if cls != want_class:
        fail("WrongClass: " + name + ": " + key + " has class " + str(cls) + ", required " + want_class)

GRID_MINUTES_STR = ["00", "15", "30", "45"]
GRID_STEP = "15m"
MAX_SLOT_CELLS = 96  # 24h of 15-minute cells -- a generous backstop, not an expected ceiling

def enforce_grid(starts_at, ends_at):
    for label, t in [("startsAt", starts_at), ("endsAt", ends_at)]:
        if len(t) != 20:
            fail("SlotGridViolation: " + label + ": must be a canonical whole-second UTC instant; got " + t)
        if t[17:19] != "00" or t[14:16] not in GRID_MINUTES_STR:
            fail("SlotGridViolation: " + label + " must align to the 15-minute booking grid (:00/:15/:30/:45); got " + t)

def slot_cells(starts_at, ends_at):
    cells = []
    cur = starts_at
    for _i in range(MAX_SLOT_CELLS + 1):
        if not (cur < ends_at):
            return cells
        cells.append(cur)
        cur = time.rfc3339_add(cur, GRID_STEP)
    fail("SessionTooLong: session spans more than " + str(MAX_SLOT_CELLS) + " 15-minute slots (24h); shorten the interval")

def slot_cellcode(cell_start):
    return cell_start.replace("-", "").replace(":", "").lower()

def claim_cell(hub, cellcode, cls, conflict_code, who):
    key = hub + ".slot" + cellcode
    # read-posture: (d) declared optionalReads at CreateSession dispatch — an
    # absent cell is the common case (no existing booking), never a required read.
    existing = kv.Read(key)
    if existing != None and not existing.isDeleted:
        fail(conflict_code + ": " + who + " " + hub + " slot " + cellcode + " is already booked")
    if existing != None and existing.isDeleted:
        return make_aspect_upsert_occ(hub, "slot" + cellcode, cls, {}, existing.revision)
    return make_aspect(hub, "slot" + cellcode, cls, {})

def require_matching_studio(sess_id, studio):
    _, studio_id = parts_of(studio, "studio", "studio")
    at_studio_lnk = "lnk.session." + sess_id + ".atStudio.studio." + studio_id
    # read-posture: (a) declared reads at TombstoneSession dispatch (validation
    # link; absence means the caller named the wrong studio — WrongStudio).
    asl = kv.Read(at_studio_lnk)
    if asl == None or asl.isDeleted:
        fail("WrongStudio: studio " + studio + " is not the studio of session vtx.session." + sess_id)
    return studio_id

def release_cells_mutations(studio, sched):
    if sched == None or sched.isDeleted:
        return []
    s_starts = sched.data.get("startsAt")
    s_ends = sched.data.get("endsAt")
    if s_starts == None or s_ends == None:
        return []
    out = []
    for c in slot_cells(s_starts, s_ends):
        cc = slot_cellcode(c)
        out.append(make_tombstone(studio + ".slot" + cc))
    return out

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "CreateSession":
        studio = required_string(p, "studio")
        _, studio_id = parts_of(studio, "studio", "studio")
        require_live_typed(state, studio, "studio", "studio")

        name = required_string(p, "name")
        starts_at = time.rfc3339_utc(required_string(p, "startsAt"))
        ends_at = time.rfc3339_utc(required_string(p, "endsAt"))
        if not (starts_at < ends_at):
            fail("InvalidArgument: endsAt: must be strictly after startsAt; got startsAt=" + starts_at + " endsAt=" + ends_at)
        capacity = required_int(p, "capacity", 1, 200)

        enforce_grid(starts_at, ends_at)
        cells = slot_cells(starts_at, ends_at)

        sess_id = bare_nanoid_or_mint(p, "sessionId")
        sess_key = "vtx.session." + sess_id

        at_studio_lnk = "lnk.session." + sess_id + ".atStudio.studio." + studio_id

        sched = {"name": name, "startsAt": starts_at, "endsAt": ends_at, "capacity": capacity}

        mutations = [
            make_vtx(sess_key, "session", {}),
            make_aspect(sess_key, "schedule", "sessionSchedule", sched),
            make_link(at_studio_lnk, sess_key, studio, "atStudio", "atStudio", {}),
        ]
        for c in cells:
            cc = slot_cellcode(c)
            mutations.append(claim_cell(studio, cc, "studioSlotClaim", "StudioConflict", "studio"))
        events = [{"class": "wellness.sessionCreated", "data": {"sessionKey": sess_key, "studio": studio}}]
        return {"mutations": mutations, "events": events, "response": {"primaryKey": sess_key}}

    if ot == "TombstoneSession":
        sess_key = required_string(p, "sessionKey")
        _, sess_id = parts_of(sess_key, "sessionKey", "session")
        if not vertex_alive(state, sess_key):
            fail("UnknownSession: " + sess_key)
        cls = class_of(state, sess_key)
        if cls != "session":
            fail("WrongClass: sessionKey: " + sess_key + " has class " + str(cls) + ", required session")

        studio = required_string(p, "studio")
        require_matching_studio(sess_id, studio)

        # read-posture: (a) declared reads at TombstoneSession dispatch —
        # required for cell release.
        sched = kv.Read(sess_key + ".schedule")
        mutations = [make_tombstone(sess_key)]
        mutations.extend(release_cells_mutations(studio, sched))
        events = [{"class": "wellness.sessionCancelled", "data": {"sessionKey": sess_key}}]
        return {"mutations": mutations, "events": events, "response": {"primaryKey": sess_key}}

    fail("UnknownOperation: " + ot)
`

// bookingDDLScript handles CreateBooking + CancelBooking. The seat-claim loop
// is the SAME CreateOnly-key-collision idiom sessionDDLScript's claim_cell
// uses, applied over an enumerated seat-index dimension — see
// wellness-vertical-design.md §1(2). The residency check reads
// lease-signing's applicationFor link by known key (no cross-package write,
// no declared package dependency needed at the Starlark level — the same
// "read another package's vertex by known key" idiom loftspace-ledger's
// heldFor / cafe-domain's cafeTabSettlement already use).
const bookingDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key,
            "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(vtx_key, local_name, cls, data):
    return {"op": "create", "key": vtx_key + "." + local_name,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

def make_aspect_upsert_occ(vtx_key, local_name, cls, data, rev):
    return {"op": "update", "key": vtx_key + "." + local_name, "expectedRevision": rev,
            "document": {"class": cls, "isDeleted": False,
                         "vertexKey": vtx_key, "localName": local_name, "data": data}}

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

def class_of(state, key):
    if key not in state:
        return None
    doc = state[key]
    if doc == None:
        return None
    if not hasattr(doc, "class"):
        return None
    return getattr(doc, "class")

def require_live_typed(state, key, name, want_class):
    if not vertex_alive(state, key):
        fail("UnknownEndpoint: " + name + ": " + key + " is absent or tombstoned")
    cls = class_of(state, key)
    if cls != want_class:
        fail("WrongClass: " + name + ": " + key + " has class " + str(cls) + ", required " + want_class)

def require_matching_session(book_id, session):
    _, sess_id = parts_of(session, "session", "session")
    for_session_lnk = "lnk.booking." + book_id + ".forSession.session." + sess_id
    # read-posture: (a) declared reads at CancelBooking dispatch (validation
    # link; absence means the caller named the wrong session — WrongSession).
    fs = kv.Read(for_session_lnk)
    if fs == None or fs.isDeleted:
        fail("WrongSession: session " + session + " is not the session of booking vtx.booking." + book_id)
    return sess_id

MAX_SESSION_CAPACITY = 200

def claim_first_free_seat(session_key, capacity):
    # Bounded for-range (Starlark has no while-loop) — the SAME enumerate-then-
    # CreateOnly-claim idiom as the session DDL's claim_cell, over seat indices
    # instead of time cells. kv.Read is LAZY (§2.5 idiom): it only decides which
    # candidate to claim; the safety property is the atomic batch's CreateOnly /
    # expectedRevision conditioning at commit — two callers racing for the same
    # open seat both read it absent and both emit op:create for the identical
    # key, but CreateOnly at revision 0 commits exactly once.
    for n in range(1, MAX_SESSION_CAPACITY + 1):
        if n > capacity:
            fail("SessionFull: " + session_key + " has no open seats (capacity " + str(capacity) + ")")
        seat_key = session_key + ".seat" + str(n)
        # read-posture: (d) declared optionalReads at CreateBooking dispatch
        # (first-free-seat claim; an absent seat is the common case).
        existing = kv.Read(seat_key)
        if existing == None:
            return n, make_aspect(session_key, "seat" + str(n), "sessionSeatClaim", {})
        if existing.isDeleted:
            return n, make_aspect_upsert_occ(session_key, "seat" + str(n), "sessionSeatClaim", {}, existing.revision)
    fail("SessionFull: " + session_key + " has no open seats (capacity " + str(capacity) + ")")

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "CreateBooking":
        session = required_string(p, "session")
        _, sess_id = parts_of(session, "session", "session")
        require_live_typed(state, session, "session", "session")

        booker = required_string(p, "booker")
        _, booker_id = parts_of(booker, "booker", "identity")
        require_live_typed(state, booker, "booker", "identity")

        # read-posture: (a) declared reads at CreateBooking dispatch.
        sched = kv.Read(session + ".schedule")
        if sched == None or sched.isDeleted:
            fail("InvalidState: " + session + ".schedule is missing; cannot book")
        capacity = sched.data.get("capacity")
        if capacity == None:
            fail("InvalidState: " + session + ".schedule.capacity is missing; cannot book")

        seat_n, seat_mutation = claim_first_free_seat(session, capacity)

        rate = "standard"
        resident_mutation = None
        lease_key = optional_string(p, "leaseAppKey")
        if lease_key != None:
            _, lease_id = parts_of(lease_key, "leaseAppKey", "leaseapp")
            # read-posture: (d) declared optionalReads at CreateBooking
            # dispatch (resident-rate lookup; absent → falls through to
            # standard rate, never a hard failure).
            lease_doc = kv.Read(lease_key)
            lease_alive = lease_doc != None and not lease_doc.isDeleted
            # .tenancy is stamped CreateOnly on a leaseapp's FIRST
            # DecideLeaseApplication approve (lease-signing/scripts.go) — its
            # presence is the only signal that this application actually
            # became an active tenancy, not merely a pending or declined one.
            # Without this check a pending or declined applicant (the
            # applicationFor link stays live in both cases) would wrongly
            # qualify for the resident rate.
            # read-posture: (d) declared optionalReads at CreateBooking dispatch.
            tenancy_doc = kv.Read(lease_key + ".tenancy")
            tenancy_present = tenancy_doc != None and not tenancy_doc.isDeleted
            # read-posture: (d) declared optionalReads at CreateBooking dispatch.
            app_for_lnk = kv.Read("lnk.leaseapp." + lease_id + ".applicationFor.identity." + booker_id)
            link_live = app_for_lnk != None and not app_for_lnk.isDeleted
            if lease_alive and tenancy_present and link_live:
                rate = "resident"

        book_id = bare_nanoid_or_mint(p, "bookingId")
        book_key = "vtx.booking." + book_id

        for_session_lnk = "lnk.booking." + book_id + ".forSession.session." + sess_id
        booked_by_lnk = "lnk.booking." + book_id + ".bookedBy.identity." + booker_id

        mutations = [
            make_vtx(book_key, "booking", {}),
            make_aspect(book_key, "status", "bookingStatus", {"value": "booked", "rate": rate, "seat": seat_n}),
            make_link(for_session_lnk, book_key, session, "forSession", "forSession", {}),
            make_link(booked_by_lnk, book_key, booker, "bookedBy", "bookedBy", {}),
            seat_mutation,
        ]
        if rate == "resident":
            resident_rate_lnk = "lnk.booking." + book_id + ".residentRate.leaseapp." + lease_id
            mutations.append(make_link(resident_rate_lnk, book_key, lease_key, "residentRate", "residentRate", {}))

        events = [{"class": "wellness.bookingCreated", "data": {"bookingKey": book_key, "session": session, "booker": booker, "rate": rate}}]
        return {"mutations": mutations, "events": events, "response": {"primaryKey": book_key}}

    if ot == "CancelBooking":
        book_key = required_string(p, "bookingKey")
        _, book_id = parts_of(book_key, "bookingKey", "booking")
        if not vertex_alive(state, book_key):
            fail("UnknownBooking: " + book_key)
        cls = class_of(state, book_key)
        if cls != "booking":
            fail("WrongClass: bookingKey: " + book_key + " has class " + str(cls) + ", required booking")

        session = required_string(p, "session")
        require_matching_session(book_id, session)

        # read-posture: (a) declared reads at CancelBooking dispatch.
        status = kv.Read(book_key + ".status")
        if status == None or status.isDeleted:
            fail("InvalidState: " + book_key + ".status is missing; cannot cancel")
        seat_n = status.data.get("seat")
        if seat_n == None:
            fail("InvalidState: " + book_key + ".status.seat is missing; cannot cancel")

        mutations = [
            make_tombstone(book_key),
            make_tombstone(session + ".seat" + str(seat_n)),
        ]
        events = [{"class": "wellness.bookingCancelled", "data": {"bookingKey": book_key}}]
        return {"mutations": mutations, "events": events, "response": {"primaryKey": book_key}}

    fail("UnknownOperation: " + ot)
`
