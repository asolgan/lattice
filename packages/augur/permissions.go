package augur

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the package's permission vertices + grants.
//
// Both ops are operator-driven — Weaver (the directOp dispatcher) and the
// bridge service actor are operator-equivalent (holdsRole → operator, exactly
// like the orchestration-base service actors), so each op is granted to operator
// at scope:any — the same operator-grant idiom orchestration-base / lease-signing
// use for their instanceOp/replyOp pairs:
//
//   - CreateAugurReasoningClaim — Weaver submits this directOp (Option F) when a
//     gap escalates; it mints the claim vertex write-ahead of the reasoning call
//     and emits external.augur for the bridge.
//   - RecordProposal — the bridge's service actor submits the replyOp that
//     records the proposal verdict.
//
// Both are target-less for auth (the directOp/replyOp posture, Contract #10
// §10.4): auth keys on operationType + actor, so the operator grant authorizes
// the submit. Weaver also holds the `system` lane (the protected kernel-actor
// lane grant, Contract #2 §2.3) under which it dispatches the directOp.
func Permissions() []pkgmgr.PermissionSpec {
	return []pkgmgr.PermissionSpec{
		{
			OperationType: "CreateAugurReasoningClaim",
			Scope:         "any",
			Note:          "Authorizes Weaver (operator-equivalent, holdsRole → operator) to submit the directOp that mints the Augur claim vertex + emits external.augur (Option F; escalation-dispatch addendum §7).",
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
