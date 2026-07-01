// Package loftspaceledger is the Loftspace tenant payment ledger: a per-lease
// financial account that records charges (rent, fees, deposits) and payments
// as an append-only transaction history, never a mutable running total.
//
// It declares:
//
//   - The `account` vertex type (DDL `account`) — CreateAccount mints
//     vtx.account.<NanoID> (root data {} per D5) with a deterministic id equal
//     to the leaseapp's own bare NanoID (one account per lease), linked to the
//     leaseapp via heldFor. At most one account per lease (the deterministic key
//     makes a second CreateAccount for the same lease conflict).
//
//   - The `transaction` vertex type (DDL `transaction`) — DebitAccount (a
//     charge: rent, a late fee, a deposit) and CreditAccount (a payment
//     received) each mint vtx.transaction.<NanoID> (root data {} per D5) with a
//     .entry aspect {type, amountCents, memo?, postedAt}, linked to the account
//     via postedTo. The ledger is append-only: a balance is derived by summing
//     entries (the ledgerHistory lens), never stored as a mutable aspect — so
//     concurrent debits/credits never race a read-modify-write.
//
//   - The `ledgerHistory` lens (§10.2-style read model, one row per
//     transaction) the payment-history FE reads (P5).
//
// This is the ledger the parallel bespoke-contracts-executable-paper design
// (Lattice lane) builds to: vtx.account.<id> + Debit/CreditAccount +ledger
// entries linked back to their authorizing source.
//
// Depends lease-signing (the leaseapp vertex type an account is heldFor).
package loftspaceledger

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:    "loftspace-ledger",
	Version: "0.1.0",
	Description: "Loftspace tenant payment ledger: the account vertex type (CreateAccount, one per lease, " +
		"deterministic id) + the transaction vertex type (DebitAccount/CreditAccount, append-only entries " +
		"linked to the account via postedTo) + the ledgerHistory read-model lens (one row per transaction). " +
		"Depends lease-signing.",
	Depends:     []string{"lease-signing"},
	DDLs:        DDLs(),
	Lenses:      Lenses(),
	Permissions: Permissions(),
}
