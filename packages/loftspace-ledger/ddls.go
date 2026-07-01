package loftspaceledger

import "github.com/asolgan/lattice/internal/pkgmgr"

// DDLs returns the package's DDL meta-vertex declarations: `account`
// (CreateAccount) and `transaction` (DebitAccount, CreditAccount).
func DDLs() []pkgmgr.DDLSpec {
	return []pkgmgr.DDLSpec{
		accountDDL(),
		transactionDDL(),
	}
}

func accountDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "account",
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"CreateAccount"},
		Description: "Ledger account DDL. Vertex shape: vtx.account.<NanoID>, class=account, root data = {} " +
			"(minimal, D5 — the balance is LENS-derived by summing transactions, never stored). CreateAccount{leaseAppKey} " +
			"mints exactly one account per lease: the account's NanoID is the SAME bare id as the leaseapp's own " +
			"(a deterministic key, not minted), so a second CreateAccount for the same lease conflicts on the account's " +
			"already-existing key (AccountAlreadyExists) rather than needing a separate guard link. Writes the heldFor " +
			"link (account→leaseapp, the account is the later-arriving vertex so it is the source — Contract #1 §1.1). " +
			"Requires the leaseAppKey be a live leaseapp (no orphan accounts).",
		Script: accountDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"leaseAppKey":{"type":"string","description":"vtx.leaseapp.<NanoID> of the lease this account is for (CreateAccount; required, validated alive). The account's own id is derived from this key's NanoID — one account per lease."}},` +
			`"required":["leaseAppKey"]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.account.<NanoID> of the created account (the operation's principal key)."}}}`,
		FieldDescription: map[string]string{
			"leaseAppKey": "Full vtx.leaseapp.<NanoID> key of the lease the account is opened for. CreateAccount validates it is alive, derives the account's id from the same NanoID (one account per lease — a second call for the same lease conflicts on the deterministic key), and writes the heldFor link (account→leaseapp).",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:    "CreateAccount — open the ledger account for a signed lease",
				Payload: map[string]any{"leaseAppKey": "vtx.leaseapp.<NanoID>"},
				ExpectedOutcome: "Validates the leaseapp is alive. Atomically commits vtx.account.<sameNanoID> (root data {} — D5) " +
					"+ the heldFor link (account→leaseapp). Emits account.created{accountKey, leaseAppKey}. Returns primaryKey " +
					"(the account key). Rejects with UnknownLeaseApplication if the lease is absent, or AccountAlreadyExists " +
					"if an account already exists for this lease.",
			},
		},
	}
}

func transactionDDL() pkgmgr.DDLSpec {
	return pkgmgr.DDLSpec{
		CanonicalName:     "transaction",
		Class:             "meta.ddl.vertexType",
		PermittedCommands: []string{"DebitAccount", "CreditAccount"},
		Description: "Ledger transaction DDL. Vertex shape: vtx.transaction.<NanoID>, class=transaction, root data = {} " +
			"(minimal, D5 — the entry detail is a .entry aspect). DebitAccount{accountKey, amountCents, memo?} records a " +
			"charge (rent, a late fee, a deposit); CreditAccount{accountKey, amountCents, memo?} records a payment received. " +
			"Each mints a fresh vtx.transaction.<NanoID> + a .entry aspect {type (debit|credit), amountCents, memo?, postedAt} " +
			"+ the postedTo link (transaction→account, the transaction is the later-arriving vertex so it is the source — " +
			"Contract #1 §1.1). The ledger is APPEND-ONLY — no balance is stored or mutated on the account; the ledgerHistory " +
			"lens derives a balance by summing entries, so concurrent debits/credits never race a read-modify-write. Requires " +
			"the accountKey be a live account and amountCents be a positive number.",
		Script: transactionDDLScript,
		InputSchema: `{"type":"object","properties":` +
			`{"accountKey":{"type":"string","description":"vtx.account.<NanoID> the transaction posts to (DebitAccount/CreditAccount; required, validated alive)."},` +
			`"amountCents":{"type":"number","description":"The transaction amount in integer cents; required, must be > 0. A debit is a charge (increases what the tenant owes); a credit is a payment (decreases it)."},` +
			`"memo":{"type":"string","description":"Optional free-text description of the charge or payment (e.g. \"June rent\", \"Late fee\"). Optional."}},` +
			`"required":["accountKey","amountCents"]}`,
		OutputSchema: `{"type":"object","properties":` +
			`{"primaryKey":{"type":"string","description":"vtx.transaction.<NanoID> of the minted transaction (the operation's principal key)."}}}`,
		FieldDescription: map[string]string{
			"accountKey":  "Full vtx.account.<NanoID> key the transaction posts to. DebitAccount/CreditAccount validate it is alive and write the postedTo link (transaction→account) the ledgerHistory lens walks.",
			"amountCents": "The transaction amount in integer cents; required, must be a positive number. Stored on the .entry aspect and projected verbatim by the ledgerHistory lens.",
			"memo":        "Optional free-text description of the charge or payment (e.g. \"June rent\", \"Late fee — 5 days\"). Stored on the .entry aspect when supplied; projected by the ledgerHistory lens.",
		},
		Examples: []pkgmgr.ExampleSpec{
			{
				Name:    "DebitAccount — charge rent",
				Payload: map[string]any{"accountKey": "vtx.account.<NanoID>", "amountCents": 150000, "memo": "June rent"},
				ExpectedOutcome: "Validates the account is alive and amountCents > 0. Atomically commits vtx.transaction.<NanoID> " +
					"(root data {} — D5) + the .entry aspect {type: debit, amountCents: 150000, memo: \"June rent\", postedAt} " +
					"+ the postedTo link (transaction→account). Emits account.debited{accountKey, transactionKey, amountCents}. " +
					"Returns primaryKey. Rejects UnknownAccount if the account is absent, or InvalidArgument if amountCents <= 0.",
			},
			{
				Name:    "CreditAccount — record a rent payment",
				Payload: map[string]any{"accountKey": "vtx.account.<NanoID>", "amountCents": 150000, "memo": "Rent payment — check #1042"},
				ExpectedOutcome: "Same shape as DebitAccount, but writes .entry{type: credit, ...} and emits " +
					"account.credited{accountKey, transactionKey, amountCents}. A payment reduces what the tenant owes " +
					"(the ledgerHistory-derived balance = sum(debits) − sum(credits)).",
			},
		},
	}
}
