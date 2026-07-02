package bespokecontracts

import "github.com/asolgan/lattice/internal/pkgmgr"

// WeaverTargets returns the package's meta.weaverTarget playbook (Contract
// #10 §10.8). Two independent gaps → remediation:
//
//   - missing_charge → directOp(DebitAccount) over the charged account
//     (row.accountKey). Params route the computed amount (row.amountCents,
//     type-preserved — resolveParam returns the row value verbatim) and the
//     authorizing clause (row.clauseKey, the new clauseRef param
//     loftspace-ledger's DebitAccount reads this fire) into the op's
//     payload; Reads routes both keys into ContextHint.Reads so the
//     Processor hydrates the account + the clause. The `directOp`-must-be-
//     literal guard is satisfied — DebitAccount is a literal operation name,
//     only params/target/reads are row-templated (the objectLiveness →
//     TombstoneObject / appointmentReminders → RecordAppointmentReminder
//     precedent, granted to operator, which Weaver's service actor holds).
//   - missing_inspection → assignTask(InspectPremises) to the assigned
//     inspector (row.inspectorKey), scoped to the clause (row.clauseKey) —
//     the same shape as lease-signing's missing_signature → assignTask
//     SignLease. Opens a stable-id Task; the inspector completes it by
//     submitting InspectPremises, which the clause DDL's own script handles
//     (mirrors SignLease acting on its own leaseapp).
//
// Every row.<col> template is a clauseSatisfaction BodyColumn — the
// §10.2↔§10.8 column seam, cross-checked by
// TestBespokeContracts_PlaybookColumnsMatchLens.
func WeaverTargets() []pkgmgr.WeaverTargetSpec {
	return []pkgmgr.WeaverTargetSpec{
		{
			TargetID: ClauseSatisfactionTarget,
			LensRef:  ClauseSatisfactionTarget,
			Gaps: map[string]pkgmgr.GapActionSpec{
				"missing_charge": {
					Action:    "directOp",
					Operation: "DebitAccount",
					Target:    "row.accountKey",
					Params:    map[string]string{"amountCents": "row.amountCents", "clauseRef": "row.clauseKey"},
					Reads:     []string{"row.accountKey", "row.clauseKey"},
				},
				"missing_inspection": {
					Action:    "assignTask",
					Operation: "InspectPremises",
					Assignee:  "row.inspectorKey",
					Target:    "row.clauseKey",
				},
			},
		},
	}
}
