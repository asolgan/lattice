// Package clinicledger is the Clinic patient payment ledger: a per-patient
// financial account that records charges (copays, invoice lines) and payments
// as an append-only transaction history, never a mutable running total.
//
// It declares:
//
//   - The `account` vertex type (DDL `account`) — CreateAccount mints
//     vtx.account.<NanoID> (root data {} per D5) with a deterministic id equal
//     to the patient's own bare NanoID (one account per patient), linked to the
//     patient via heldFor. At most one account per patient (the deterministic
//     key makes a second CreateAccount for the same patient conflict).
//
//   - The `transaction` vertex type (DDL `transaction`) — DebitAccount (a
//     charge: a copay, an invoice line) and CreditAccount (a payment received)
//     each mint vtx.transaction.<NanoID> (root data {} per D5) with a .entry
//     aspect {type, amountCents, memo?, postedAt}, linked to the account via
//     postedTo. The ledger is append-only: a balance is derived by summing
//     entries (the ledgerHistory lens), never stored as a mutable aspect — so
//     concurrent debits/credits never race a read-modify-write.
//
//   - The `ledgerHistory` lens (one row per transaction) the billing-history FE
//     reads (P5).
//
// Mirrors packages/loftspace-ledger, with the account held for a patient
// instead of a lease — a patient may have many appointments/encounters, and
// billing tracks a single running balance across all of them.
//
// Depends clinic-domain (the patient vertex type an account is heldFor).
package clinicledger

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:    "clinic-ledger",
	Version: "0.1.0",
	Description: "Clinic patient payment ledger: the account vertex type (CreateAccount, one per patient, " +
		"deterministic id) + the transaction vertex type (DebitAccount/CreditAccount, append-only entries " +
		"linked to the account via postedTo) + the ledgerHistory read-model lens (one row per transaction). " +
		"Depends clinic-domain.",
	Depends:     []string{"clinic-domain"},
	DDLs:        DDLs(),
	Lenses:      Lenses(),
	Permissions: Permissions(),
}
