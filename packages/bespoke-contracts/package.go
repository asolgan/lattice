// Package bespokecontracts is the LoftSpace "Executable Paper" reference
// package (Fires V1+V2 of the bespoke-contracts-executable-paper design):
// fixed/one-time computational clauses, conditioned fees, and judgment
// clauses, all riding the convergence machinery the platform already ships —
// no new engine, no Weaver runtime.
//
// It declares:
//
//   - The `clause` vertex type (DDL `clause`) — CreateClause mints
//     vtx.clause.<NanoID> (root data {} per D5) governing a lease: a .prose
//     aspect (the legal paragraph), a .terms aspect ({kind, conditioned,
//     amountCents?, period: "oneTime"}), and a .status aspect ({state:
//     "active"}). `kind=computational` (default, Fire V1) charges a ledger
//     account (chargesTo link); `kind=judgment` (Fire V2) assigns an
//     inspector (requiresInspectionBy link) instead, closed by a
//     .clauseInspection aspect. Either kind may carry an optional
//     conditionedOn link (any live vertex, e.g. a pet record) gating the
//     charge on that vertex staying alive. Always writes the governs link
//     (clause→lease).
//
//   - The `clauseSatisfaction` actorAggregate convergence lens (§10.2),
//     anchored on the clause: `missing_charge` is true while the clause
//     charges an account, its condition (if any) still holds, and no
//     transaction `authorizedBy` it exists yet; `missing_inspection` is true
//     while the clause has an assigned inspector and no .inspection aspect
//     yet — the shipped upsert-retraction idiom (either gap closes and its
//     row simply stops violating, per the design's R3 v1 constraint — no
//     filter-retraction dependency).
//
//   - The §10.8 playbook (meta.weaverTarget clauseSatisfaction) —
//     missing_charge → directOp(DebitAccount), row-templating the account to
//     charge + the clause to authorize against; missing_inspection →
//     assignTask(InspectPremises) to the assigned inspector.
//
// loftspace-ledger's DebitAccount op is extended (Fire V1) to accept an
// optional clauseRef: when present it writes the
// lnk.transaction.authorizedBy.clause audit link and marks the clause
// .status completed — the "why was I charged this?" chain of custody.
//
// See _bmad-output/implementation-artifacts/bespoke-contracts-executable-paper-design.md
// §3, §4.1, §10 (Fires V1+V2). Depends lease-signing (the leaseapp a clause
// governs) + loftspace-ledger (the account a clause charges + the
// DebitAccount op the playbook dispatches).
package bespokecontracts

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:    "bespoke-contracts",
	Version: "0.2.0",
	Description: "LoftSpace 'Executable Paper' reference package (Fires V1+V2 — fixed/one-time computational, " +
		"conditioned, and judgment clauses): the clause vertex type (CreateClause, .prose/.terms/.status/" +
		".clauseInspection aspects, governs + chargesTo/requiresInspectionBy/conditionedOn links) + the " +
		"clauseSatisfaction actorAggregate convergence lens (§10.2, missing_charge/missing_inspection) + the " +
		"§10.8 playbook dispatching directOp(DebitAccount)/assignTask(InspectPremises) on the gaps. Depends " +
		"lease-signing + loftspace-ledger.",
	Depends:       []string{"lease-signing", "loftspace-ledger"},
	DDLs:          DDLs(),
	Lenses:        Lenses(),
	Permissions:   Permissions(),
	WeaverTargets: WeaverTargets(),
	OpMetas:       OpMetas(),
}
