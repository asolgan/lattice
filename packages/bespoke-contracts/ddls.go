package bespokecontracts

import "github.com/asolgan/lattice/internal/pkgmgr"

// DDLs returns the package's DDL meta-vertex declarations: `clause`
// (CreateClause) plus its four aspect-type declarations (clauseProse,
// clauseTerms, clauseStatus, clauseInspection). clauseStatus permits
// DebitAccount too — the cross-package write loftspace-ledger's DebitAccount
// makes to mark a fixed/one-time clause completed (the objectLiveness →
// TombstoneObject precedent: a package's aspect DDL lists every op, in any
// package, that legitimately writes it).
func DDLs() []pkgmgr.DDLSpec {
	return []pkgmgr.DDLSpec{
		clauseDDL(),
		clauseProseAspectTypeDDL(),
		clauseTermsAspectTypeDDL(),
		clauseStatusAspectTypeDDL(),
		clauseInspectionAspectTypeDDL(),
	}
}

func clauseDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName: "clause",
		Class:         "meta.ddl.vertexType",
		// InspectPremises acts on an existing clause (writes its .inspection
		// aspect, root untouched) the same way SignLease acts on an existing
		// leaseapp — the op's env.Class routes to this DDL's Script.
		PermittedCommands: []string{"CreateClause", "InspectPremises"},
		Description: "Bespoke-contract clause DDL. Vertex shape: vtx.clause.<NanoID>, class=clause, root data = {} " +
			"(minimal, D5 — the provision text and terms are aspects). CreateClause{leaseAppKey, kind?, prose, " +
			"accountKey?, amountCents?, inspectorKey?, conditionedOnKey?} mints the clause under a fresh NanoID, " +
			"requiring the lease (and, per kind, the account or inspector, and the conditionedOn vertex if given) " +
			"all be live (no-orphan invariant). `kind` selects the archetype: \"computational\" (default, Fire V1) " +
			"requires accountKey+amountCents and writes the chargesTo link (clause→account) the DebitAccount " +
			"directOp charges; \"judgment\" (Fire V2) requires inspectorKey and writes the requiresInspectionBy " +
			"link (clause→identity) the assignTask(InspectPremises) gap targets — no charge. Either kind may carry " +
			"an optional conditionedOnKey (any live vertex, e.g. a pet record): CreateClause writes the " +
			"conditionedOn link (clause→that vertex) generically from its own key-shape (vtx.<type>.<id>); the " +
			"clauseSatisfaction lens only opens the gap while that link is live, so tombstoning the condition " +
			"stops the fee/inspection without touching the clause. Writes the governs link (clause→lease, the " +
			"state this provision governs) in every case — the clause is the later-arriving vertex on every link " +
			"it writes, so it is the source (Contract #1 §1.1). Recurring/proration archetypes are a later " +
			"increment (Fire V3 of the design).",
		Script: clauseDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"leaseAppKey":{"type":"string","description":"vtx.leaseapp.<NanoID> this clause governs (required, validated alive)."},` +
			`"kind":{"type":"string","description":"\"computational\" (default) or \"judgment\". Selects which of accountKey+amountCents vs inspectorKey is required."},` +
			`"prose":{"type":"string","description":"The human-readable provision text (the legal paragraph the signer agreed to); required, non-empty."},` +
			`"accountKey":{"type":"string","description":"vtx.account.<NanoID> this clause charges (required + validated alive when kind=computational)."},` +
			`"amountCents":{"type":"number","description":"The fixed one-time charge amount in integer cents (required, must be > 0, when kind=computational)."},` +
			`"inspectorKey":{"type":"string","description":"vtx.identity.<NanoID> assigned the InspectPremises Task (required + validated alive when kind=judgment)."},` +
			`"conditionedOnKey":{"type":"string","description":"Optional vtx.<type>.<NanoID> of any live vertex (e.g. a pet record) this clause is conditioned on; validated alive if given. Absent link ⇒ unconditional."}},` +
			`"required":["leaseAppKey","prose"]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.clause.<NanoID> of the created clause (the operation's principal key)."}}}`,
		FieldDescription: map[string]string{
			"leaseAppKey":      "Full vtx.leaseapp.<NanoID> key of the lease this clause governs. CreateClause validates it is alive and writes the governs link (clause→lease).",
			"kind":             "\"computational\" (default) or \"judgment\". computational requires accountKey+amountCents; judgment requires inspectorKey.",
			"prose":            "The legal paragraph a signer agreed to. Stored verbatim on the .prose aspect; never interpreted — the machine terms are the separate .terms aspect.",
			"accountKey":       "Full vtx.account.<NanoID> key of the ledger account this clause charges. CreateClause validates it is alive and writes the chargesTo link (clause→account); the account key also flows into the clauseSatisfaction lens as the directOp target.",
			"amountCents":      "The fixed one-time charge amount in integer cents; required (kind=computational), must be a positive number. Stored on the .terms aspect and flows type-preserved into the DebitAccount directOp's amountCents param when the clause is unsatisfied.",
			"inspectorKey":     "Full vtx.identity.<NanoID> key of the identity assigned to inspect (kind=judgment). CreateClause validates it is alive and writes the requiresInspectionBy link (clause→identity); flows into the clauseSatisfaction lens as the assignTask assignee.",
			"conditionedOnKey": "Full vtx.<type>.<NanoID> key of any live vertex this clause is conditioned on. CreateClause validates it is alive and writes the conditionedOn link (clause→that vertex). Tombstoning the target vertex retracts the link, which the clauseSatisfaction lens reads as the condition no longer holding.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name: "CreateClause — a one-time lockout fee",
				Payload: map[string]any{
					"leaseAppKey": "vtx.leaseapp.<NanoID>",
					"accountKey":  "vtx.account.<NanoID>",
					"prose":       "Tenant agrees to a $45 lockout fee for each after-hours lockout assistance request.",
					"amountCents": 4500,
				},
				ExpectedOutcome: "Validates the lease and account are alive. Atomically commits vtx.clause.<freshNanoID> " +
					"(root data {} — D5) + .prose{text} + .terms{kind:computational, amountCents:4500, period:oneTime} + " +
					".status{state:active} + the governs link (clause→lease) + the chargesTo link (clause→account). " +
					"Emits clause.created{clauseKey, leaseAppKey, kind, accountKey, amountCents}. Returns primaryKey. The " +
					"clauseSatisfaction lens immediately projects the clause as violating (missing_charge=true, no " +
					"authorizedBy transaction yet); Weaver dispatches directOp(DebitAccount) to close the gap.",
			},
			{
				Name: "CreateClause — a conditioned pet fee",
				Payload: map[string]any{
					"leaseAppKey":      "vtx.leaseapp.<NanoID>",
					"accountKey":       "vtx.account.<NanoID>",
					"prose":            "Tenant agrees to a $50 monthly pet fee for each pet on file.",
					"amountCents":      5000,
					"conditionedOnKey": "vtx.pet.<NanoID>",
				},
				ExpectedOutcome: "As the fixed-fee example, plus the conditionedOn link (clause→pet). The " +
					"clauseSatisfaction lens only opens missing_charge while the pet link is live; tombstoning the " +
					"pet vertex retracts the link and the gap stops opening.",
			},
			{
				Name: "CreateClause — a move-in inspection (judgment)",
				Payload: map[string]any{
					"leaseAppKey":  "vtx.leaseapp.<NanoID>",
					"kind":         "judgment",
					"prose":        "Landlord will inspect the premises before move-in and record any pre-existing damage.",
					"inspectorKey": "vtx.identity.<NanoID>",
				},
				ExpectedOutcome: "Validates the lease and inspector identity are alive. Commits the clause (no " +
					"chargesTo link) + .terms{kind:judgment, period:oneTime} + the requiresInspectionBy link " +
					"(clause→identity). Emits clause.created{clauseKey, leaseAppKey, kind, inspectorKey}. The " +
					"clauseSatisfaction lens projects missing_inspection=true; Weaver dispatches " +
					"assignTask(InspectPremises) to the inspector; InspectPremises closes the gap.",
			},
		},
	}
}

func clauseProseAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "clauseProse",
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateClause"},
		Description: "The clause's legal-paragraph text. Stored as vtx.clause.<NanoID>.prose (class clauseProse) " +
			"= {text}. Non-sensitive. Written exactly once by CreateClause, atomically alongside the clause vertex " +
			"it belongs to. Declaration-only: no op handler of its own.",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{"text":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"text": "The human-readable provision text, verbatim from the CreateClause payload.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "clause prose aspect",
				Payload:         map[string]any{"text": "Tenant agrees to a $45 lockout fee for each after-hours lockout assistance request."},
				ExpectedOutcome: "Stored as vtx.clause.<NanoID>.prose; created once by CreateClause alongside the clause vertex.",
			},
		},
	}
}

func clauseTermsAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "clauseTerms",
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"CreateClause"},
		Description: "The clause's machine terms — what 'fulfillment' means digitally. Stored as " +
			"vtx.clause.<NanoID>.terms (class clauseTerms) = {kind, conditioned, amountCents?, period}, kind ∈ " +
			"{computational, judgment}. Non-sensitive. computational (Fire V1, default) carries amountCents; " +
			"judgment (Fire V2) carries no amountCents — its gate is the requiresInspectionBy link + the " +
			"clauseInspection aspect, not a charge. `conditioned` is true iff CreateClause received a " +
			"conditionedOnKey — an explicit flag (not inferred from the conditionedOn link's liveness) because a " +
			"tombstoned condition TARGET makes the lens's optional match resolve null exactly like \"never " +
			"conditioned\" would; only this flag lets the lens tell the two apart. Both kinds fix period=\"oneTime\" " +
			"for now; recurring (period beyond oneTime) and prorated (rateCents/basis) terms are later increments " +
			"(Fire V3). Written exactly once by CreateClause, atomically alongside the clause vertex it belongs " +
			"to. Declaration-only: no op handler of its own.",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{"kind":{"type":"string"},"conditioned":{"type":"boolean"},"amountCents":{"type":"number"},"period":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"kind":        "\"computational\" (auto-debit, default) or \"judgment\" (open-a-Task, Fire V2). Recurring/proration kinds are a later increment.",
			"conditioned": "True iff CreateClause received a conditionedOnKey. The clauseSatisfaction lens's conditioning gate reads this flag, not the link's liveness.",
			"amountCents": "The fixed charge amount in integer cents, verbatim from the CreateClause payload. Absent for kind=judgment.",
			"period":      "Always \"oneTime\" for now. Recurring periods are a later increment (Fire V3).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "clause terms aspect — fixed one-time",
				Payload:         map[string]any{"kind": "computational", "conditioned": false, "amountCents": 4500, "period": "oneTime"},
				ExpectedOutcome: "Stored as vtx.clause.<NanoID>.terms; created once by CreateClause alongside the clause vertex.",
			},
			{
				Name:            "clause terms aspect — judgment",
				Payload:         map[string]any{"kind": "judgment", "conditioned": false, "period": "oneTime"},
				ExpectedOutcome: "Stored as vtx.clause.<NanoID>.terms; created once by CreateClause alongside the clause vertex. No amountCents.",
			},
		},
	}
}

func clauseInspectionAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "clauseInspection",
		Class:             "meta.ddl.aspectType",
		PermittedCommands: []string{"InspectPremises"},
		Description: "The judgment clause's inspection record. Stored as vtx.clause.<NanoID>.inspection (class " +
			"clauseInspection) = {completed, completedAt}. Non-sensitive. Absent while the inspection is " +
			"outstanding (missing_inspection=true); written exactly once, CreateOnly, by InspectPremises — the " +
			"assignTask target the §10.8 playbook dispatches to the clause's requiresInspectionBy identity. A " +
			"second InspectPremises against the same clause is rejected (AlreadyInspected), mirroring SignLease's " +
			"once-only .signature write.",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{"completed":{"type":"boolean"},"completedAt":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"completed":   "Always true once written — the aspect's presence, not this field, is the gate the lens reads.",
			"completedAt": "RFC3339 timestamp InspectPremises stamps when it records the inspection.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "clause inspection aspect — recorded",
				Payload:         map[string]any{"completed": true, "completedAt": "2026-07-02T12:00:00Z"},
				ExpectedOutcome: "Created (op:create, CreateOnly) by InspectPremises; the clauseSatisfaction lens flips missing_inspection false.",
			},
		},
	}
}

func clauseStatusAspectTypeDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName: "clauseStatus",
		Class:         "meta.ddl.aspectType",
		// DebitAccount (loftspace-ledger) marks a fixed/one-time clause completed
		// once it posts the authorizing charge — a cross-package write, the
		// objectLiveness → TombstoneObject precedent.
		PermittedCommands: []string{"CreateClause", "DebitAccount"},
		Description: "The clause's lifecycle state. Stored as vtx.clause.<NanoID>.status (class clauseStatus) = " +
			"{state, completedAt?}, state ∈ {active, completed, superseded}. Non-sensitive. Created active by " +
			"CreateClause; updated to completed by loftspace-ledger's DebitAccount when it posts the authorizing " +
			"charge for a fixed/one-time clause (an UNCONDITIONED update — this status is audit/display bookkeeping, " +
			"not the convergence gate itself, which the clauseSatisfaction lens derives from the authorizedBy " +
			"transaction link, not this aspect — see the design's R3).",
		Script:       aspectDeclarationOnlyScript,
		InputSchema:  `{"type":"object","properties":{"state":{"type":"string"},"completedAt":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		FieldDescription: map[string]string{
			"state":       "active (CreateClause) or completed (DebitAccount, fixed/one-time clauses).",
			"completedAt": "RFC3339 timestamp DebitAccount stamps when it marks the clause completed. Absent while active.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:            "clause status aspect — completed by a debit",
				Payload:         map[string]any{"state": "completed", "completedAt": "2026-07-02T12:00:00Z"},
				ExpectedOutcome: "Updated (op:update, unconditioned) by DebitAccount when clauseRef names this clause.",
			},
		},
	}
}
