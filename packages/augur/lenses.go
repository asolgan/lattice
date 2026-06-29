package augur

import "github.com/asolgan/lattice/internal/pkgmgr"

// AugurProposalsBucket is the NATS-KV read model the augurProposals lens projects
// into — the **P5 query surface** for "what reasoning proposals exist and what is
// their review verdict". Loupe (the operator inspector) reads THIS projected
// bucket (one row per proposal, keyed by the proposal key) to render the
// human-in-the-loop review surface — list pending proposals, show the model's
// proposed action + rationale + confidence, and (Fire 2) approve / reject — never
// Core KV (lattice-architecture.md P5 — lenses are the only application query
// surface). The Refractor auto-creates the bucket on lens load.
const AugurProposalsBucket = "augur-proposals"

// Lenses returns the package's single read-model lens: augurProposals. It is a
// FLAT projection (no aggregation / WITH, no link walks) — one row per
// augurproposal vertex, the same clean shape clinic-domain's clinicAppointments /
// clinicPatients use. Every display column is read off the proposal's own aspects
// by the documented node.<aspect>.data.<field> form; the candidate + target keys
// come from the TRUSTED .gap aspect (the instanceOp-minted escalation context), so
// no forCandidate / forTarget walk is needed for display and the row stays strictly
// one-per-proposal.
//
// The lens surfaces the WHOLE proposal lifecycle, not just completed verdicts: a
// claim minted by CreateAugurReasoningClaim (reasoning still in flight) projects
// its .gap context with a null reviewState, and once RecordProposal lands the
// model-derived columns + the pending|invalid verdict fill in. Loupe renders "in
// flight" for a null state and the verdict otherwise.
//
// Read-model only (the trusted-tool posture): NOT protected, NOT a weaver-target
// convergence lens. The proposal vertices it projects are Weaver/bridge-authored
// orchestration state; this is the operator's window onto them, read like any
// other P5 read-model bucket.
func Lenses() []pkgmgr.LensSpec {
	return []pkgmgr.LensSpec{
		{
			CanonicalName: "augurProposals",
			Class:         "meta.lens",
			Adapter:       "nats-kv",
			Bucket:        AugurProposalsBucket,
			Engine:        "full",
			Spec:          augurProposalsSpec,
		},
	}
}

// augurProposalsSpec projects one row per augurproposal vertex. Flat (no-WITH, no
// OPTIONAL walk) like clinicPatients: the per-row key is `key` (the proposal key,
// the IntoKey default), so the read model is keyed by vtx.augurproposal.<handle>;
// proposalKey repeats it in the body for client-side reference.
//
//   - .gap {targetId, entityId, gapColumn, trigger} — the TRUSTED escalation
//     context the CreateAugurReasoningClaim instanceOp minted write-ahead. entityId
//     / targetId are full keys (vtx.leaseapp.<id> / vtx.meta.<id>), so a reader
//     derives the candidate type + target from them without a link walk.
//   - .proposed {action, params} — the model's remediation. proposedParams is a
//     non-scalar (map) projected verbatim, stored as JSON (the same shape
//     clinicProviders uses for the timeOff / hours arrays); the reviewer reads it
//     to see exactly what would be dispatched on approval.
//   - .rationale.text / .confidence.score / .provenance.{model, reasonedAt} — the
//     reasoning audit: why the model proposed this, its self-reported 0..1
//     confidence, and the provenance the operator weighs the proposal against.
//   - .review {state, invalidReason, reviewedAt, dispatchedAt} — the verdict.
//     reviewState is null while the claim's reasoning is in flight, then
//     pending|invalid once RecordProposal records the §5-validated verdict
//     (invalidReason carries the auditable reason on an invalid). reviewedAt /
//     dispatchedAt are the Fire-2 approve / dispatch stamps (null until then).
//
// All aspect reads are null-safe by key-shape: a not-yet-written aspect projects
// null (the same null-safe discipline clinicAppointments applies to .reminder /
// .encounter), so a claim-in-flight row projects cleanly with null model columns.
const augurProposalsSpec = `MATCH (pr:augurproposal)
RETURN
  pr.key AS key,
  pr.key AS proposalKey,
  pr.gap.data.targetId AS targetId,
  pr.gap.data.entityId AS entityId,
  pr.gap.data.gapColumn AS gapColumn,
  pr.gap.data.trigger AS trigger,
  pr.proposed.data.action AS proposedAction,
  pr.proposed.data.params AS proposedParams,
  pr.rationale.data.text AS rationale,
  pr.confidence.data.score AS confidence,
  pr.provenance.data.model AS model,
  pr.provenance.data.reasonedAt AS reasonedAt,
  pr.review.data.state AS reviewState,
  pr.review.data.invalidReason AS invalidReason,
  pr.review.data.reviewedAt AS reviewedAt,
  pr.review.data.dispatchedAt AS dispatchedAt`
