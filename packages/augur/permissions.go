package augur

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the package's permission vertices + grants.
//
// The two externalTask ops are operator-driven — the Loom relay actor and the
// bridge service actor are both operator-equivalent (holdsRole → operator,
// exactly like the Weaver / orchestration-base service actors), so each op is
// granted to operator at scope:any — the same operator-grant idiom
// orchestration-base / lease-signing use for the externalTask instanceOp/replyOp:
//
//   - CreateAugurReasoningClaim — Loom's relay actor submits the instanceOp that
//     mints the claim vertex write-ahead of the reasoning call.
//   - RecordProposal — the bridge's service actor submits the replyOp that
//     records the proposal verdict.
//
// Both are target-less for auth (the directOp/replyOp posture, Contract #10
// §10.4): auth keys on operationType + actor, so the operator grant authorizes
// the submit.
func Permissions() []pkgmgr.PermissionSpec {
	return []pkgmgr.PermissionSpec{
		{
			OperationType: "CreateAugurReasoningClaim",
			Scope:         "any",
			Note:          "Authorizes the Loom relay actor (operator-equivalent) to submit the externalTask instanceOp that mints the Augur claim vertex (design §3.3).",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "RecordProposal",
			Scope:         "any",
			Note:          "Authorizes the bridge replyOp (identity:bridge, operator-equivalent) to record an Augur proposal vertex (design §3.2 / §5).",
			GrantsTo:      []string{"operator"},
		},
	}
}
