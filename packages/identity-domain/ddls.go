package identitydomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// DDLs returns the package's DDL meta-vertex declarations:
//   - `identity` (meta.ddl.vertexType) — handles CreateUnclaimedIdentity,
//     UpdateIdentityState, ClaimIdentity, RecordIdentityPII. State machine:
//     unclaimed → claimed; merged is set only by identity-hygiene's
//     MergeIdentity.
//   - `ssn`, `dob`, `name`, `email`, `phone`, `claimKey`,
//     `credentialBinding` (meta.ddl.aspectType, sensitive) — declare the
//     identity domain's sensitive PII aspect types. Marking them sensitive=true
//     makes the Processor's step-6 validator anchor them to identity vertices
//     (NFR-S3 / lattice-architecture Item 6). ssn/dob are written only by
//     RecordIdentityPII and carry permittedCommands:["RecordIdentityPII"]. The
//     other five are written by multiple ops across packages
//     (CreateUnclaimedIdentity, ClaimIdentity, and identity-hygiene's
//     MergeIdentity), so they carry no permittedCommands — sensitivity
//     (identity-anchoring) is their only enforcement, deliberately leaving the
//     writer unrestricted.
//
// Architectural rules: known-key reads only. The duplicate-detection
// index lookups (vtx.identityindex.*) use crypto.sha256NanoID-derived
// known keys provided by the caller in ContextHint.Reads.
func DDLs() []pkgmgr.DDLSpec {
	return []pkgmgr.DDLSpec{
		{
			CanonicalName: "identity",
			Class:         "meta.ddl.vertexType",
			PermittedCommands: []string{
				"CreateUnclaimedIdentity",
				"UpdateIdentityState",
				"ClaimIdentity",
				"RecordIdentityPII",
			},
			Description: "Identity domain DDL. " +
				"Vertex shape: vtx.identity.<NanoID>, class=identity. " +
				"Aspects: name (sensitive, required, maxLen 200), email (sensitive, lowercase-normalized), " +
				"phone (sensitive, E.164-normalized), state (enum: unclaimed|claimed|merged), " +
				"ssn (sensitive, applicant SSN: 9 digits; any hyphens accepted and stripped; written by RecordIdentityPII), " +
				"dob (sensitive, ISO YYYY-MM-DD applicant date of birth, written by RecordIdentityPII), " +
				"claimKey (sensitive, stores the client-supplied claimKeyHash verbatim; tombstoned after claim), " +
				"credentialBinding (sensitive; null pre-claim), " +
				"mergedInto (vertex-key reference, set only by identity-hygiene package's MergeIdentity). " +
				"The client mints the claim secret, submits only claimKeyHash; Lattice never holds the plaintext. " +
				"State machine + IdentityMerged guard enforced in .script.",
			Script: identityDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"name":{"type":"string","maxLength":200,"description":"Person's display name. Required for CreateUnclaimedIdentity."},` +
				`"email":{"type":"string","description":"Email address, case-insensitive normalized. At least one of email/phone required."},` +
				`"phone":{"type":"string","description":"Phone number, E.164 digits only. At least one of email/phone required."},` +
				`"claimKeyHash":{"type":"string","description":"Lowercase hex sha256 of the client-minted claim secret (CreateUnclaimedIdentity, required). Lattice stores it verbatim; the plaintext never enters Lattice."},` +
				`"claimKeyAlgo":{"type":"string","enum":["sha256"],"description":"Hash algorithm for claimKeyHash. Optional; defaults to sha256 (the only accepted value)."},` +
				`"identityKey":{"type":"string","description":"vtx.identity.<NanoID> — target identity for UpdateIdentityState and RecordIdentityPII."},` +
				`"newState":{"type":"string","enum":["claimed"],"description":"Target state for UpdateIdentityState. Only unclaimed→claimed is permitted."},` +
				`"claimKey":{"type":"string","description":"One-time-use claim key plaintext (ClaimIdentity). Its sha256 must match the stored hash."},` +
				`"targetIdentityKey":{"type":"string","description":"vtx.identity.<NanoID> of the unclaimed identity to claim (ClaimIdentity)."},` +
				`"ssn":{"type":"string","description":"Applicant Social Security Number (RecordIdentityPII, required). 9 digits; any hyphens are accepted and stripped; stored normalized as a sensitive aspect."},` +
				`"dob":{"type":"string","description":"Applicant date of birth (RecordIdentityPII, required). ISO YYYY-MM-DD; stored as a sensitive aspect."}}}`,
			OutputSchema: `{"type":"object","properties":` +
				`{"primaryKey":{"type":"string","description":"vtx.identity.<NanoID> of the created, claimed, or PII-recorded identity (the operation's principal key)."}}}`,
			FieldDescription: map[string]string{
				"name":              "Person's display name. Required on CreateUnclaimedIdentity. Stored as sensitive aspect.",
				"email":             "Email address. Stored lowercase-normalized. Used as a deduplication index key.",
				"phone":             "Phone number. Stored as E.164 digit string. Used as a deduplication index key.",
				"claimKeyHash":      "Lowercase hex sha256 of the client-minted claim secret. Required on CreateUnclaimedIdentity. Stored verbatim; Lattice never holds the plaintext.",
				"claimKeyAlgo":      "Hash algorithm for claimKeyHash. Optional; defaults to sha256 (the only accepted value).",
				"identityKey":       "Full vtx.identity.<NanoID> key of an existing identity vertex.",
				"newState":          "Desired state after UpdateIdentityState. State machine: unclaimed → claimed only.",
				"claimKey":          "The plaintext one-time claim key the client minted at CreateUnclaimedIdentity. Used for ClaimIdentity verification (its sha256 is compared to the stored hash).",
				"targetIdentityKey": "Full vtx.identity.<NanoID> of the unclaimed identity the calling actor wants to claim.",
				"ssn":               "Applicant SSN. Required on RecordIdentityPII. 9 digits; any hyphens are accepted and stripped; stored normalized in a sensitive vtx.identity.<NanoID>.ssn aspect.",
				"dob":               "Applicant date of birth. Required on RecordIdentityPII. ISO YYYY-MM-DD; stored in a sensitive vtx.identity.<NanoID>.dob aspect.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:    "CreateUnclaimedIdentity — new customer with email",
					Payload: map[string]any{"name": "Alice Smith", "email": "alice@example.com", "claimKeyHash": "<sha256-hex-of-client-minted-secret>"},
					ExpectedOutcome: "Creates vtx.identity.<NanoID> with class=identity, writes name/email/state/claimKey aspects " +
						"(claimKey stores the supplied hash verbatim). Returns primaryKey (the identity key). " +
						"Duplicate detection rides the IdentityCreated event's data.duplicate flag, not the reply.",
				},
				{
					Name:            "ClaimIdentity — actor claims their identity",
					Payload:         map[string]any{"targetIdentityKey": "vtx.identity.<NanoID>", "claimKey": "<plaintextKey>"},
					ExpectedOutcome: "Verifies claimKey hash, writes credentialBinding aspect, transitions state unclaimed→claimed, tombstones claimKey aspect.",
				},
				{
					Name:    "RecordIdentityPII — capture applicant SSN/DOB",
					Payload: map[string]any{"identityKey": "vtx.identity.<NanoID>", "ssn": "123-45-6789", "dob": "1990-01-15"},
					ExpectedOutcome: "Validates formats, writes sensitive vtx.identity.<NanoID>.ssn (normalized to 123456789) and " +
						".dob aspects onto the existing identity; the identity vertex root data is not mutated. " +
						"A sensitive ssn/dob aspect on any non-identity vertex is rejected by the step-6 sensitiveAspectScope rule.",
				},
			},
		},
		{
			CanonicalName:     "ssn",
			Class:             "meta.ddl.aspectType",
			Sensitive:         true,
			PermittedCommands: []string{"RecordIdentityPII"},
			Description: "Applicant Social Security Number. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.ssn, " +
				"sensitive=true, identity-anchored, the crypto-shred unit. Written by RecordIdentityPII.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"ssn":{"type":"string","description":"SSN: 9 digits; any hyphens are accepted and stripped."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"ssn": "Applicant SSN: 9 digits; any hyphens are accepted and stripped; stored normalized as a sensitive aspect on the identity.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "ssn aspect",
					Payload:         map[string]any{"ssn": "123-45-6789"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.ssn; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
		{
			CanonicalName:     "dob",
			Class:             "meta.ddl.aspectType",
			Sensitive:         true,
			PermittedCommands: []string{"RecordIdentityPII"},
			Description: "Applicant date of birth. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.dob, " +
				"sensitive=true, identity-anchored, the crypto-shred unit. Written by RecordIdentityPII.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"dob":{"type":"string","description":"ISO 8601 calendar date, YYYY-MM-DD."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"dob": "Applicant date of birth, ISO YYYY-MM-DD, stored as a sensitive aspect on the identity.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "dob aspect",
					Payload:         map[string]any{"dob": "1990-01-15"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.dob; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
		{
			CanonicalName: "name",
			Class:         "meta.ddl.aspectType",
			Sensitive:     true,
			Description: "Person's display name. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.name, " +
				"sensitive=true, identity-anchored. Written by CreateUnclaimedIdentity and " +
				"overwritten by identity-hygiene's MergeIdentity aspectConflictResolution; " +
				"permittedCommands is intentionally empty so any identity-anchored writer is allowed.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"name":{"type":"string","maxLength":200,"description":"Person's display name."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"name": "Person's display name, stored as a sensitive aspect on the identity.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "name aspect",
					Payload:         map[string]any{"name": "Alice Smith"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.name; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
		{
			CanonicalName: "email",
			Class:         "meta.ddl.aspectType",
			Sensitive:     true,
			Description: "Email address. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.email, " +
				"sensitive=true, identity-anchored. Written by CreateUnclaimedIdentity and " +
				"overwritten by identity-hygiene's MergeIdentity aspectConflictResolution; " +
				"permittedCommands is intentionally empty so any identity-anchored writer is allowed.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"email":{"type":"string","description":"Email address, lowercase-normalized."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"email": "Email address, lowercase-normalized, stored as a sensitive aspect on the identity.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "email aspect",
					Payload:         map[string]any{"email": "alice@example.com"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.email; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
		{
			CanonicalName: "phone",
			Class:         "meta.ddl.aspectType",
			Sensitive:     true,
			Description: "Phone number. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.phone, " +
				"sensitive=true, identity-anchored. Written by CreateUnclaimedIdentity and " +
				"overwritten by identity-hygiene's MergeIdentity aspectConflictResolution; " +
				"permittedCommands is intentionally empty so any identity-anchored writer is allowed.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"phone":{"type":"string","description":"Phone number, E.164 digit string."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"phone": "Phone number, E.164 digit string, stored as a sensitive aspect on the identity.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "phone aspect",
					Payload:         map[string]any{"phone": "+15551234567"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.phone; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
		{
			CanonicalName: "claimKey",
			Class:         "meta.ddl.aspectType",
			Sensitive:     true,
			Description: "Client-supplied claim-key hash. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.claimKey, " +
				"sensitive=true, identity-anchored. Written by CreateUnclaimedIdentity and tombstoned " +
				"by ClaimIdentity; permittedCommands is intentionally empty so any identity-anchored " +
				"writer is allowed.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"hash":{"type":"string","description":"Lowercase hex sha256 of the client-minted claim secret, stored verbatim."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"hash": "Lowercase hex sha256 of the client-minted claim secret, stored verbatim as a sensitive aspect on the identity.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "claimKey aspect",
					Payload:         map[string]any{"hash": "<sha256-hex-of-client-minted-secret>"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.claimKey; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
		{
			CanonicalName: "credentialBinding",
			Class:         "meta.ddl.aspectType",
			Sensitive:     true,
			Description: "Actor-to-identity credential binding. Sensitive aspect-type " +
				"(lattice-architecture Item 6 / PRD §358): stored as vtx.identity.<NanoID>.credentialBinding, " +
				"sensitive=true, identity-anchored. Written by ClaimIdentity; permittedCommands is " +
				"intentionally empty so any identity-anchored writer is allowed.",
			Script: sensitiveAspectDDLScript,
			InputSchema: `{"type":"object","properties":` +
				`{"actorKey":{"type":"string","description":"Actor key bound to the identity at claim time."},` +
				`"boundAt":{"type":"string","description":"Timestamp the binding was established."}}}`,
			OutputSchema: `{"type":"object"}`,
			FieldDescription: map[string]string{
				"actorKey": "Actor key bound to the identity at claim time, stored as a sensitive aspect on the identity.",
				"boundAt":  "Timestamp the credential binding was established.",
			},
			Examples: []pkgmgr.ExampleSpec{
				{
					Name:            "credentialBinding aspect",
					Payload:         map[string]any{"actorKey": "vtx.actor.<NanoID>", "boundAt": "2026-05-22T11:00:00Z"},
					ExpectedOutcome: "Stored as sensitive vtx.identity.<NanoID>.credentialBinding; rejected on any non-identity vertex by step-6 sensitiveAspectScope.",
				},
			},
		},
	}
}

// sensitiveAspectDDLScript is the declaration-only Starlark shared by every
// sensitive aspect-type DDL in this package (ssn, dob, name, email, phone,
// claimKey, credentialBinding). An aspect-type DDL declares a sensitive
// aspect's shape and anchoring; it is not an operation handler (the identity
// DDL's operations write the aspects). No operation carries an aspect class as
// its operation class, so execute is never dispatched here — it fails closed if
// it ever is.
const sensitiveAspectDDLScript = `
def execute(state, op):
    fail("aspect-type DDL: not an operation handler: " + op.operationType)
`

// identityDDLScript is the identity DDL Starlark script. State machine:
// unclaimed -> claimed. The merged state is set only by the
// identity-hygiene package's MergeIdentity script.
const identityDDLScript = `
def make_update(key, data):
    return {"op": "update", "key": key, "document": {"isDeleted": False, "data": data}}

def read_state(state, identity_key):
    aspect_key = identity_key + ".state"
    if aspect_key in state:
        doc = state[aspect_key]
        if doc.data != None and "value" in doc.data:
            return doc.data["value"]
    return None

def read_merged_into(state, identity_key):
    aspect_key = identity_key + ".mergedInto"
    if aspect_key in state:
        doc = state[aspect_key]
        if doc.data != None and "value" in doc.data:
            return doc.data["value"]
    return None

def enforce_not_merged(current_state, merged_into):
    if current_state == "merged":
        fail("IdentityMerged: mergedInto=" + (merged_into if merged_into != None else "<unknown>"))

def validate_state_transition(current, new):
    if current == None:
        fail("InvalidStateTransition: <missing> -> " + str(new))
    allowed = {
        "unclaimed": ["claimed"],
    }
    targets = allowed.get(current)
    if targets == None or new not in targets:
        fail("InvalidStateTransition: " + str(current) + " -> " + str(new))

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "UpdateIdentityState":
        identity_key = p.identityKey
        new_state = p.newState
        current = read_state(state, identity_key)
        merged_into = read_merged_into(state, identity_key)
        enforce_not_merged(current, merged_into)
        validate_state_transition(current, new_state)
        mutations = [make_update(identity_key + ".state", {"value": new_state})]
        events = [{"class": "identity.stateChanged", "data": {
            "identityKey": identity_key,
            "oldState": current,
            "newState": new_state,
        }}]
        return {"mutations": mutations, "events": events}

    if ot == "CreateUnclaimedIdentity":
        name = p.name if hasattr(p, "name") else None
        if name == None or type(name) != type("") or len(name.strip()) == 0:
            fail("InvalidArgument: name: required, maxLen 200")
        name = name.strip()
        if len(name) > 200:
            fail("InvalidArgument: name: required, maxLen 200")

        raw_email = p.email if hasattr(p, "email") else None
        raw_phone = p.phone if hasattr(p, "phone") else None

        email = None
        if raw_email != None and type(raw_email) == type(""):
            e = raw_email.strip().lower()
            if len(e) > 0:
                email = e

        phone = None
        if raw_phone != None and type(raw_phone) == type(""):
            stripped = ""
            for ch in raw_phone.elems():
                if ch >= "0" and ch <= "9":
                    stripped += ch
                elif ch == "+":
                    stripped += ch
            if len(stripped) > 0:
                phone = stripped

        if email == None and phone == None:
            fail("InvalidArgument: email or phone: at least one required")

        claim_key_hash = p.claimKeyHash if hasattr(p, "claimKeyHash") else None
        if claim_key_hash == None or type(claim_key_hash) != type("") or len(claim_key_hash) == 0:
            fail("InvalidArgument: claimKeyHash: required non-empty lowercase hex sha256")
        if len(claim_key_hash) != 64:
            fail("InvalidArgument: claimKeyHash: must be 64-char lowercase hex sha256")
        for ch in claim_key_hash.elems():
            if not ((ch >= "0" and ch <= "9") or (ch >= "a" and ch <= "f")):
                fail("InvalidArgument: claimKeyHash: must be lowercase hex")
        claim_key_algo = p.claimKeyAlgo if hasattr(p, "claimKeyAlgo") else None
        if claim_key_algo == None or claim_key_algo == "":
            claim_key_algo = "sha256"
        if claim_key_algo != "sha256":
            fail("InvalidArgument: claimKeyAlgo: only sha256 is supported")

        duplicate = False
        if email != None:
            email_index_key = "vtx.identityindex." + crypto.sha256NanoID("email:" + email)
            email_hit = state[email_index_key] if email_index_key in state else None
            if email_hit != None and (not hasattr(email_hit, "isDeleted") or not email_hit.isDeleted):
                duplicate = True
        if phone != None:
            phone_index_key = "vtx.identityindex." + crypto.sha256NanoID("phone:" + phone)
            phone_hit = state[phone_index_key] if phone_index_key in state else None
            if phone_hit != None and (not hasattr(phone_hit, "isDeleted") or not phone_hit.isDeleted):
                duplicate = True

        identity_id = nanoid.new()
        identity_key = "vtx.identity." + identity_id

        initial_state = "unclaimed"

        mutations = [
            {"op": "create", "key": identity_key,
             "document": {"class": "identity", "isDeleted": False, "data": {}}},
            {"op": "create", "key": identity_key + ".name",
             "document": {"class": "name", "vertexKey": identity_key, "localName": "name",
                          "isDeleted": False, "data": {"value": name}}},
            {"op": "create", "key": identity_key + ".state",
             "document": {"class": "state", "vertexKey": identity_key, "localName": "state",
                          "isDeleted": False, "data": {"value": initial_state}}},
            {"op": "create", "key": identity_key + ".claimKey",
             "document": {"class": "claimKey", "vertexKey": identity_key, "localName": "claimKey",
                          "isDeleted": False, "data": {"hash": claim_key_hash, "algo": claim_key_algo}}},
        ]
        if email != None:
            mutations.append({"op": "create", "key": identity_key + ".email",
                "document": {"class": "email", "vertexKey": identity_key, "localName": "email",
                             "isDeleted": False, "data": {"value": email}}})
            if email_index_key not in state:
                mutations.append({"op": "create", "key": email_index_key,
                    "document": {"class": "identityindex", "isDeleted": False,
                                 "data": {"contactType": "email", "identityKey": identity_key}}})
        if phone != None:
            mutations.append({"op": "create", "key": identity_key + ".phone",
                "document": {"class": "phone", "vertexKey": identity_key, "localName": "phone",
                             "isDeleted": False, "data": {"value": phone}}})
            if phone_index_key not in state:
                mutations.append({"op": "create", "key": phone_index_key,
                    "document": {"class": "identityindex", "isDeleted": False,
                                 "data": {"contactType": "phone", "identityKey": identity_key}}})

        events = [{"class": "identity.created", "data": {
            "identityKey": identity_key,
            "state": initial_state,
            "duplicate": duplicate,
        }}]

        return {
            "mutations": mutations,
            "events": events,
            "response": {"primaryKey": identity_key},
        }

    if ot == "ClaimIdentity":
        def fail_claim(outcome):
            fail("ClaimKeyInvalid: " + outcome)

        claim_key_plaintext = p.claimKey if hasattr(p, "claimKey") else None
        if claim_key_plaintext == None or type(claim_key_plaintext) != type("") or len(claim_key_plaintext) == 0:
            fail_claim("invalid-key")

        target_identity_key = p.targetIdentityKey if hasattr(p, "targetIdentityKey") else None
        if target_identity_key == None or type(target_identity_key) != type("") or len(target_identity_key) == 0:
            fail_claim("no-target")
        if not target_identity_key.startswith("vtx.identity."):
            fail_claim("no-target")

        target_vtx = state[target_identity_key] if target_identity_key in state else None
        if target_vtx == None or (hasattr(target_vtx, "isDeleted") and target_vtx.isDeleted):
            fail_claim("no-target")

        state_aspect_key = target_identity_key + ".state"
        state_aspect = state[state_aspect_key] if state_aspect_key in state else None
        if state_aspect == None:
            fail_claim("no-target")
        current_state = state_aspect.data["value"] if state_aspect.data != None and "value" in state_aspect.data else None
        if current_state == None:
            fail_claim("no-target")

        if current_state == "claimed":
            fail_claim("wrong-state")
        if current_state == "flagged-for-review":
            fail_claim("flagged")
        if current_state == "merged":
            fail_claim("merged")
        if current_state != "unclaimed":
            fail_claim("wrong-state")

        actor_key = op.actor
        cred_index_key = "vtx.credentialindex." + crypto.sha256NanoID(actor_key)
        cred_index = state[cred_index_key] if cred_index_key in state else None
        if cred_index != None and not (hasattr(cred_index, "isDeleted") and cred_index.isDeleted):
            fail_claim("credential-already-bound")

        claim_key_aspect_key = target_identity_key + ".claimKey"
        claim_key_aspect = state[claim_key_aspect_key] if claim_key_aspect_key in state else None
        if claim_key_aspect == None or (hasattr(claim_key_aspect, "isDeleted") and claim_key_aspect.isDeleted):
            fail_claim("invalid-key")
        if claim_key_aspect.data == None or "hash" not in claim_key_aspect.data:
            fail_claim("invalid-key")

        submitted_hash = crypto.sha256(claim_key_plaintext)
        stored_hash = claim_key_aspect.data["hash"]
        if not crypto.constant_time_equal(submitted_hash, stored_hash):
            fail_claim("invalid-key")

        observed_at = op.submittedAt

        mutations = [
            {"op": "create", "key": target_identity_key + ".credentialBinding",
             "document": {"class": "credentialBinding", "vertexKey": target_identity_key,
                          "localName": "credentialBinding", "isDeleted": False,
                          "data": {"actorKey": actor_key, "boundAt": observed_at}}},
            {"op": "update", "key": target_identity_key + ".state",
             "document": {"class": "state", "vertexKey": target_identity_key,
                          "localName": "state", "isDeleted": False,
                          "data": {"value": "claimed"}}},
            {"op": "tombstone", "key": target_identity_key + ".claimKey"},
            {"op": "create", "key": cred_index_key,
             "document": {"class": "credentialindex", "isDeleted": False,
                          "data": {"actorKey": actor_key,
                                   "identityKey": target_identity_key,
                                   "boundAt": observed_at}}},
        ]

        events = [{"class": "identity.claimed", "data": {
            "identityKey": target_identity_key,
            "actorKey": actor_key,
        }}]

        # The identity vertex itself is not mutated by a claim; the principal
        # committed key is the state aspect (unclaimed -> claimed). primaryKey
        # names the principal entity (the identity); the Processor accepts it as
        # the 3-segment root of the committed aspects.
        return {
            "mutations": mutations,
            "events": events,
            "response": {"primaryKey": target_identity_key},
        }

    if ot == "RecordIdentityPII":
        identity_key = p.identityKey if hasattr(p, "identityKey") else None
        if identity_key == None or type(identity_key) != type("") or len(identity_key) == 0:
            fail("InvalidArgument: identityKey: required")
        if not identity_key.startswith("vtx.identity."):
            fail("InvalidArgument: identityKey: must be a vtx.identity.<NanoID> key")

        # The target identity must already exist, not be tombstoned, and not be
        # merged. The caller declares identity_key + its .state aspect in
        # ContextHint.Reads — known-key reads only. The .state aspect is always
        # present on a created identity; the merged guard keys off
        # state == "merged" (MergeIdentity sets state and mergedInto together),
        # so .mergedInto need not be hydrated here (it is absent pre-merge and
        # would otherwise be a hydration miss).
        target_vtx = state[identity_key] if identity_key in state else None
        if target_vtx == None or (hasattr(target_vtx, "isDeleted") and target_vtx.isDeleted):
            fail("InvalidArgument: identityKey: no such identity")
        current_state = read_state(state, identity_key)
        enforce_not_merged(current_state, read_merged_into(state, identity_key))

        # SSN: 9 digits; any hyphens are accepted and stripped regardless of
        # position; any other character is rejected. Stored normalized (digits
        # only). Format gate only — SSN allocation rules (area/group/serial) are
        # out of scope (the bgcheck externalTask, not this op, verifies the
        # identity).
        raw_ssn = p.ssn if hasattr(p, "ssn") else None
        if raw_ssn == None or type(raw_ssn) != type("") or len(raw_ssn) == 0:
            fail("InvalidArgument: ssn: required")
        ssn_digits = ""
        for ch in raw_ssn.elems():
            if ch >= "0" and ch <= "9":
                ssn_digits += ch
            elif ch == "-":
                continue
            else:
                fail("InvalidArgument: ssn: must be 9 digits")
        if len(ssn_digits) != 9:
            fail("InvalidArgument: ssn: must be 9 digits")

        # DOB: ISO YYYY-MM-DD. Two gates: (1) string-shape (length 10, '-' at
        # positions 4 and 7, the rest digits), then (2) a real calendar date —
        # month 1..12, day within the month's length, Feb 29 only in leap years.
        # The deterministic Starlark sandbox has no clock, so the date is NOT
        # bounded against "today" (no future-date / age check here). Stored
        # verbatim.
        dob = p.dob if hasattr(p, "dob") else None
        if dob == None or type(dob) != type("") or len(dob) != 10:
            fail("InvalidArgument: dob: must be ISO YYYY-MM-DD")
        dob_chars = dob.elems()
        idx = 0
        for ch in dob_chars:
            if idx == 4 or idx == 7:
                if ch != "-":
                    fail("InvalidArgument: dob: must be ISO YYYY-MM-DD")
            elif ch < "0" or ch > "9":
                fail("InvalidArgument: dob: must be ISO YYYY-MM-DD")
            idx += 1

        year = int(dob[0:4])
        month = int(dob[5:7])
        day = int(dob[8:10])
        if year < 1:
            fail("InvalidArgument: dob: year out of range")
        if month < 1 or month > 12:
            fail("InvalidArgument: dob: month out of range")
        days_in_month = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
        max_day = days_in_month[month - 1]
        is_leap = (year % 4 == 0 and year % 100 != 0) or (year % 400 == 0)
        if month == 2 and is_leap:
            max_day = 29
        if day < 1 or day > max_day:
            fail("InvalidArgument: dob: day out of range for month")

        # Write the PII as sensitive aspects on the identity. class MUST be
        # ssn/dob so the step-6 validator's Lookup(class) resolves the sensitive
        # aspect-type DDL and anchors the aspect to the identity. The identity
        # vertex root is NOT mutated (D5: PII lives in aspects, not vertex root).
        mutations = [
            {"op": "create", "key": identity_key + ".ssn",
             "document": {"class": "ssn", "vertexKey": identity_key, "localName": "ssn",
                          "isDeleted": False, "data": {"value": ssn_digits}}},
            {"op": "create", "key": identity_key + ".dob",
             "document": {"class": "dob", "vertexKey": identity_key, "localName": "dob",
                          "isDeleted": False, "data": {"value": dob}}},
        ]

        # The event carries only the identity key — no SSN/DOB plaintext (events
        # are not sensitive-aspect-scoped; PII stays in the anchored aspects).
        events = [{"class": "identity.piiRecorded", "data": {
            "identityKey": identity_key,
        }}]

        return {
            "mutations": mutations,
            "events": events,
            "response": {"primaryKey": identity_key},
        }

    fail("identity DDL: unknown operationType: " + ot)
`
