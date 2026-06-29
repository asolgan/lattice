package augur

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the package's permission vertices + grants.
//
// RecordProposal is posted by the bridge's replyOp on behalf of the augur
// externalTask. The bridge's service actor (identity:bridge) is
// operator-equivalent (holdsRole → operator, exactly like the Loom / Weaver
// service actors), so RecordProposal is granted to operator at scope:any — the
// same operator-grant idiom orchestration-base uses for the Loom lifecycle ops
// and the externalTask reply ops. The op is target-less for auth purposes (the
// directOp/replyOp posture, Contract #10 §10.4): auth keys on operationType +
// actor, so the operator grant authorizes the bridge's submit.
func Permissions() []pkgmgr.PermissionSpec {
	return []pkgmgr.PermissionSpec{
		{
			OperationType: "RecordProposal",
			Scope:         "any",
			Note:          "Authorizes the bridge replyOp (identity:bridge, operator-equivalent) to record an Augur proposal vertex (design §3.2 / §5).",
			GrantsTo:      []string{"operator"},
		},
	}
}
