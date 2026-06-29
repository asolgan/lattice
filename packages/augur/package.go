// Package augur is the Augur Capability Package — the data + safety foundation
// for Weaver's AI-assisted reasoning tier (the L3 evaluator).
//
// The Augur turns a Weaver convergence gap the package playbook cannot plan
// (an unplannable / retry-exhausted gap) into an AI-reasoned, human-reviewable
// PROPOSAL: Weaver escalates over the existing triggerLoom → externalTask →
// bridge path (a dedicated `augur` bridge adapter — Weaver never calls the
// model directly), the model proposes a remediation within the installed action
// catalog, and the bridge's replyOp records a `vtx.augurproposal` vertex pending
// human approval. The AI proposes; the human decides; the Processor stays the
// sole writer (P2).
//
// This package declares:
//
//   - The `augurproposal` DDL — the proposal vertex type + the externalTask
//     matched pair that drives one reasoning episode against the bridge's
//     standard {externalRef, status, result} reply contract:
//
//       - CreateAugurReasoningClaim (the Loom instanceOp) mints the claim vertex
//         write-ahead with the TRUSTED gap context + the links.
//       - RecordProposal (the bridge replyOp) reads that trusted context back,
//         decodes the model's structured proposal from the opaque result, and
//         records the verdict.
//
//     Proposal shape (D5 — minimal root, business data in aspects; Contract #1
//     key shapes; handle = the escalation episode's instanceKey):
//
//	vtx.augurproposal.<handle>   root data = {}
//	  .gap         { targetId, entityId, gapColumn, trigger }   instanceOp — TRUSTED, what was stuck
//	  .proposed    { action, params }                           replyOp — the remediation
//	  .rationale   { text }                                     replyOp — the reasoning (audit)
//	  .confidence  { score }                                    replyOp — 0..1 self-reported
//	  .provenance  { model, promptHash, catalogHash, reasonedAt }  replyOp
//	  .review      { state, invalidReason, reviewedAt, dispatchedAt }  replyOp — verdict
//	               state ∈ {pending, approved, rejected, dispatched, invalid, superseded}
//	lnk.augurproposal.<handle>.forCandidate.<type>.<entityId>   proposal forCandidate candidate
//	lnk.augurproposal.<handle>.forTarget.meta.<weaverTargetId>  proposal forTarget target
//
//     Both links: the proposal is the later-arriving SOURCE, the candidate and
//     the weaver target pre-exist = the TARGETs (Contract #1 §1.1); the names
//     pass the sentence test.
//
//   - RecordProposal carries the deterministic-validation safety boundary
//     (design §5, record-time leg): the entity/target identity is read from the
//     instanceOp-minted claim — NEVER the model's reply (the load-bearing safety
//     split). A proposal is stored `pending` ONLY when its proposed action is in
//     the allowed escalation vocabulary, its confidence is a real 0..1 score, and
//     it does not escape the escalated candidate's scope. A proposal that fails
//     any of these — and a modeled refusal (status=failed) — is stored `invalid`
//     with an auditable reason, never `pending`, never dispatchable. The AI never
//     produces a side effect that was not deterministically validated, and can
//     never name the entity it acts on.
//
//   - Permissions granting CreateAugurReasoningClaim + RecordProposal to
//     `operator` (the Loom relay + bridge service actors are operator-equivalent).
//
// Install via the InstallPackage kernel op. See docs/components/_packages.md and
// _bmad-output/implementation-artifacts/augur-design.md.
package augur

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:        "augur",
	Version:     "0.1.0",
	Description: "Augur (Weaver L3 reasoning tier) data + safety foundation: the augurproposal vertex type + RecordProposal op with the record-time deterministic-validation boundary.",
	Depends:     []string{"orchestration-base"},
	DDLs:        DDLs(),
	Permissions: Permissions(),
}
