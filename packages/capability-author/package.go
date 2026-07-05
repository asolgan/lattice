// Package capabilityauthor is the AI-authored-capabilities data + safety
// foundation (ai-authored-capabilities-design.md) — Fire 1: capture + the
// escalation dispatch.
//
// A Lattice-aware agent turns a capability REQUEST ("a lens listing active
// providers by specialty") into a proposed package artifact, deterministically
// validated, and applied only after a human approves — lifting the Augur
// pattern (AI proposes → validate → human gate → Processor writes) from
// arranging existing ops to authoring new package capabilities.
//
// This package declares:
//
//   - The `capabilityproposal` DDL — the proposal vertex type + the capture
//     pair for one authoring episode:
//
//   - RequestCapabilityAuthoring mints the proposal vertex write-ahead with
//     the requester + intent (no artifact yet).
//
//   - RecordCapabilityProposal carries a proposed artifact + its
//     ALREADY-COMPUTED §5 deterministic-validation verdict (in the full
//     design, computed by the bridge via pkgmgr.ValidateCapabilityArtifact
//     before submission) and stores review.state = pending | invalid.
//
//     Proposal shape (D5 — minimal root, business data in aspects):
//
//     vtx.capabilityproposal.<id>   root data = {}
//     .request     { requesterId, intent, contextRef }
//     .claim       { claimedAt, claimKey }
//     .artifact    { kind, content }
//     .target      { mode, packageName, baseVersion, newVersion }
//     .rationale   { text }
//     .confidence  { score }
//     .validation  { state, report, deltaPreview, checkedAt }
//     .provenance  { model, promptHash, catalogHash, reasonedAt }
//     .review      { state, invalidReason, reviewedAt, appliedAt, appliedByOp }
//     lnk.capabilityproposal.<id>.requestedBy.<type>.<requesterId>
//
//   - The `capabilityauthorclaim` DDL + the `capabilityAuthor` Loom pattern —
//     the escalation dispatch (design §3.4): a `capabilityAuthorPending`
//     weaver-target lens self-anchored on `capabilityproposal` triggers
//     `triggerLoom(capabilityAuthor)` while a proposal's `.claim` aspect is
//     absent; the pattern's sole externalTask step submits CreateAuthoringClaim
//     (mints the correlation-claim vertex + writes the `.claim` aspect,
//     closing the lens gap) and parks for the bridge's RecordCapabilityProposal.
//
//   - The Go-side deterministic materializer (internal/pkgmgr,
//     ValidateCapabilityArtifact) — the §5 record-time validation boundary for
//     the "lens" kind: parses the proposed cypher with the real openCypher
//     parser and runs the artifact through the same validateAll the human
//     package-authoring path uses (reused, not duplicated).
//
//   - Permissions granting RequestCapabilityAuthoring + CreateAuthoringClaim +
//     RecordCapabilityProposal to `operator` (the human requester / Loom's
//     relay actor / the trusted bridge-equivalent submitter — the same
//     operator-equivalent idiom augur's + lease-signing's capture pairs use).
//
//   - Two P5 read-model lenses (the operator/reasoning-model query surface,
//     lattice-architecture.md P5): `capabilityProposals` (flat, one row per
//     proposal — the review surface Loupe renders) and
//     `capabilityAuthorContext` (a flat scan of every installed
//     `vtx.meta.*` DDL/lens/target/pattern, the same installed-DDL
//     self-description catalog `cmd/loupe/ops.go`'s buildOpGroups computes by
//     scanning Core KV directly — this lens is the non-Loupe equivalent so
//     the bridge/reasoning adapter never needs Core KV access).
//
// Deliberately NOT yet built (the fire's remaining checkpoints, see the design
// doc): the real claude-opus-4-8-backed `capabilityAuthor` bridge adapter (only
// the deterministic `FakeCapabilityAuthor` ships — the same posture Augur's own
// adapter is still in); ReviewCapabilityProposal + the F-004 apply path; the
// `grant`/`weaverTarget`/`loomPattern`/Starlark kinds.
//
// Install via the InstallPackage kernel op. See docs/components/_packages.md
// and _bmad-output/implementation-artifacts/ai-authored-capabilities-design.md.
package capabilityauthor

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:          "capability-author",
	Version:       "0.3.0",
	Description:   "AI-authored capabilities — Fire 1 capture + escalation dispatch + P5 read models: the capabilityproposal + capabilityauthorclaim vertex types, the RequestCapabilityAuthoring/CreateAuthoringClaim/RecordCapabilityProposal ops (§5 record-time deterministic-validation boundary for the lens kind), the capabilityAuthorPending weaver-target lens, the capabilityAuthor Loom pattern, and the capabilityProposals/capabilityAuthorContext review + catalog lenses. The review/apply ops and the grant/weaverTarget/loomPattern/Starlark kinds land in later increments.",
	Depends:       []string{"orchestration-base"},
	DDLs:          DDLs(),
	Permissions:   Permissions(),
	OpMetas:       OpMetas(),
	WeaverTargets: WeaverTargets(),
	LoomPatterns:  LoomPatterns(),
	Lenses:        Lenses(),
}
