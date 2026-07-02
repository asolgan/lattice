package bespokecontracts

import "github.com/asolgan/lattice/internal/pkgmgr"

// ClauseSatisfactionTarget is the §10.8 TargetID == the clauseSatisfaction
// lens's OutputKeyPattern prefix — the §10.2↔§10.8 binding Weaver reads.
const ClauseSatisfactionTarget = "clauseSatisfaction"

// Lenses returns the package's Lens declarations: the single
// `clauseSatisfaction` actorAggregate convergence lens (§10.2), Fire V1's
// fixed/one-time computational archetype.
func Lenses() []pkgmgr.LensSpec {
	return []pkgmgr.LensSpec{
		{
			CanonicalName:  ClauseSatisfactionTarget,
			Class:          "meta.lens",
			Adapter:        "nats-kv",
			Bucket:         "weaver-targets",
			Engine:         "full",
			Spec:           clauseSatisfactionSpec,
			ProjectionKind: "actorAggregate",
			Output: &pkgmgr.OutputDescriptorSpec{
				AnchorType:       "clause",
				OutputKeyPattern: ClauseSatisfactionTarget + ".{actorSuffix}",
				BodyColumns: []string{"violating", "missing_charge", "missing_inspection", "entityKey", "clauseKey",
					"accountKey", "amountCents", "inspectorKey"},
				EmptyBehavior: "delete",
				KeyColumn:     "entityId",
			},
		},
	}
}

// clauseSatisfactionSpec is the one-row-per-clause satisfaction cypher (§3.2
// of the design). Two independent gaps, never both live on the same clause
// (CreateClause writes exactly one of accountKey/amountCents (computational)
// or an inspector link (judgment)):
//
//   - `missing_charge` — true while the clause charges an account, is either
//     unconditioned or its conditionedOn target is still live, and no
//     transaction `authorizedBy` it exists yet. count(t.key) collapses the
//     fan to a single existence check (the objectLiveness liveOwners idiom).
//     "Conditioned" is a `terms.conditioned` data flag set at CreateClause
//     time (not inferred from link/target liveness — a tombstoned
//     conditionedOn TARGET makes condKey resolve null exactly like "never
//     conditioned" would, so only an explicit flag can tell them apart; the
//     flag is true only when CreateClause received a conditionedOnKey). The
//     gate reads `conditioned <> true`, not `conditioned = false`: a
//     pre-this-fire clause's `.terms` aspect has no `conditioned` key at all
//     (Fire V1's shape), so `conditioned` resolves to null — `null = false`
//     is false (equalsAny only equals nil to nil), which would wrongly
//     collapse the whole OR to false and permanently suppress the charge for
//     every legacy clause. `<> true` correctly treats both `false` and
//     absent (null) as "not conditioned."
//   - `missing_inspection` — true while the clause has an assigned inspector
//     (judgment) and no .inspection aspect has been written yet.
//
// Null comparisons use the shipped `= null` / `<> null` idiom (lease-signing
// precedent), not `IS NULL`/`IS NOT NULL`: this grammar's
// oC_StringListNullOperatorExpression visitor deliberately passes those
// suffixes through unevaluated (full/visitor.go), so `IS NOT NULL` silently
// no-ops to the bare operand rather than a boolean. Every null-tested column
// here is itself a `.key`/aspect PROPERTY access (never a bare MATCH node
// variable): resolveProperty converts an unmatched OPTIONAL MATCH node's
// typed-nil `*nodeRef` to a clean interface nil via a direct pointer check,
// so `= null`/`<> null` sees a real nil — a bare node variable would still be
// a non-nil interface (Go's typed-nil-in-interface trap) and compare unequal
// to null even when unmatched.
//
// Deliberately does NOT gate on `.status.data.state = 'active'`: per the
// design's R3, a status-flip that removes the anchor from a WHERE-filtered
// match is the deferred negative/filter-retraction primitive (Fires 1+2
// shipped the plain-lens retraction transport 2026-07-02, but wiring it into
// actorAggregate lenses like this one is Fire 3 target-diff, not yet done —
// see the design's R3 v1 constraint). This lens instead relies purely on the
// upsert-safe signal — once the authorizing transaction / inspection exists,
// the gap flips false and STAYS false (the row lingers non-violating, which
// is harmless). The .status aspect DebitAccount writes is audit/display
// bookkeeping only, never the convergence gate.
const clauseSatisfactionSpec = `
MATCH (c:clause {key: $actorKey})
OPTIONAL MATCH (c)-[:chargesTo]->(a:account)
OPTIONAL MATCH (c)-[:conditionedOn]->(cond)
OPTIONAL MATCH (c)-[:requiresInspectionBy]->(insp:identity)
OPTIONAL MATCH (c)<-[:authorizedBy]-(t:transaction)
WITH
  c.key AS entityKey,
  a.key AS accountKey,
  cond.key AS condKey,
  insp.key AS inspectorKey,
  c.terms.data.amountCents AS amountCents,
  c.terms.data.conditioned AS conditioned,
  c.inspection.data.completed AS inspectionCompleted,
  count(t.key) AS chargeCount
RETURN
  entityKey AS actorKey,
  entityKey,
  entityKey AS clauseKey,
  accountKey,
  amountCents,
  inspectorKey,
  ((accountKey <> null) AND (chargeCount = 0) AND ((conditioned <> true) OR (condKey <> null))) AS missing_charge,
  ((inspectorKey <> null) AND (inspectionCompleted = null)) AS missing_inspection,
  (((accountKey <> null) AND (chargeCount = 0) AND ((conditioned <> true) OR (condKey <> null)))
   OR ((inspectorKey <> null) AND (inspectionCompleted = null))) AS violating
`
