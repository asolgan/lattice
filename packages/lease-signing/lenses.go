package leasesigning

import (
	"fmt"

	"github.com/asolgan/lattice/internal/pkgmgr"
)

// Lenses returns the package's Lens declarations: the single
// `leaseApplicationComplete` actorAggregate convergence lens (Contract #10
// §10.2). It is anchored on the leaseapp candidate and reprojects on a change
// to any LINKED constituent (the applicant identity's aspects, a providedTo
// service instance's outcome aspect) — the actorAggregate adjacency
// reprojection, which a plain nats_kv projection would miss. It emits the
// bare-NanoID convergence key via 14.2's keyColumn so the row key stays
// <targetId>.<entityId> and Weaver's splitRowKey accepts it.
//
// The lens is ONE ROW PER ANCHOR (Contract #10 §10.2 + the chip-#2 guard
// guardOutputKeyCollision, which fails the projection closed on a multi-row
// anchor). The service-instance fan-out is collapsed inside the aggregator:
// each family's fresh-completed instances are counted with
// count(DISTINCT CASE WHEN <family + completed> THEN inst.key ELSE null END),
// so the OPTIONAL MATCH carries no filtering WHERE (a filtering WHERE that
// removes the only match collapses the upstream anchor to null in the grouped
// projection — the documented full-engine grouping behavior) and the row count
// stays exactly one per leaseapp even with several instances.
//
// Bucket: the shared primordial weaver-targets convergence bucket (§10.2).
//
// The §10.2 convergence row carries SCALAR columns (violating / missing_* bools,
// entityKey / applicant strings). The actorAggregate projection EnvelopeFn
// projects each body column by the shape of its RETURN value: a list / collect
// column is realness-filtered (the roster behavior — my-tasks /
// capabilityEphemeral), and a scalar column projects verbatim so Weaver's
// boolColumn reads a Go bool and the §10.8 row.<col> params resolve as strings
// (Contract #6 §6.13 scalar-passthrough amendment). With 14.2's keyColumn (the
// bare-NanoID row key) the row is Weaver-readable end-to-end.
func Lenses() []pkgmgr.LensSpec {
	return []pkgmgr.LensSpec{
		{
			CanonicalName:  "leaseApplicationComplete",
			Class:          "meta.lens",
			Adapter:        "nats-kv",
			Bucket:         "weaver-targets",
			Engine:         "full",
			Spec:           leaseApplicationCompleteSpec,
			ProjectionKind: "actorAggregate",
			Output: &pkgmgr.OutputDescriptorSpec{
				AnchorType:       "leaseapp",
				OutputKeyPattern: "leaseApplicationComplete.{actorSuffix}",
				BodyColumns:      []string{"violating", "missing_onboarding", "missing_bgcheck", "missing_payment", "missing_signature", "applicant", "entityKey", "freshUntil", "inflight_bgcheck", "inflight_payment", "declined_bgcheck", "declined_payment", "declined", "maxretries_bgcheck", "maxretries_payment", "unitKey", "unitAddress", "unitRent"},
				EmptyBehavior:    "delete",
				KeyColumn:        "entityId",
				Freshness:        "auto",
			},
		},
	}
}

// leaseApplicationCompleteSpec is the one-row-per-anchor convergence cypher.
//
// It anchors on the leaseapp candidate (a required MATCH), OPTIONAL-walks the
// applicationFor link to the applicant identity, OPTIONAL-walks the appliesToUnit
// link to the leased unit, and OPTIONAL-walks the applicant's providedTo service
// instances. Each gap is a per-anchor scalar:
//
//   - missing_onboarding — the applicant has not recorded PII (no .ssn aspect).
//     RecordIdentityPII (the onboarding pattern's userTask) writes .ssn/.dob,
//     flipping this false.
//   - missing_bgcheck / missing_payment — keyed on a completed service instance
//     of that family providedTo the applicant. The family is discriminated by the
//     instance's .family aspect (read as a distinct aspect because the vertex
//     envelope `class` field shadows the .class aspect on the read path); the
//     completed test reads the .outcome aspect status. The replyOp writing the
//     .outcome aspect flips the matching gap false. bgcheck additionally requires
//     freshness (see FRESHNESS below); payment is ever-completed.
//   - missing_signature — the application has no .signature aspect. SignLease
//     writes it, flipping this false.
//
// violating is the explicit OR of the four gaps (Contract #10 §10.2: violating
// is lens-projected, not an implicit OR; for this target the natural rule is
// "any gap → violating").
//
// unitKey / unitAddress / unitRent are INFORMATIONAL columns carried from the
// appliesToUnit walk (the unit's key, its .address.line1, its .listing.rentAmount
// — aspect-hops off the live node, read inside the aggregating WITH so they
// survive the grouping). They answer "applying to lease Unit X at $Y/mo" for the
// operator / applicant FE; they are NOT in the violating OR-clause — `unit` is
// required at CreateLeaseApplication, so there is no missing_unit gap (§3 D5).
// appliesToUnit is 0..1, so these stay scalar and one-row-per-anchor holds.
//
// applicant + entityKey are the param columns the §10.8 playbook templates name
// (row.applicant, row.entityKey). They stay non-null even when gaps are open
// because the single providedTo OPTIONAL MATCH carries NO filtering WHERE: it
// binds every service neighbor and the family/freshness discrimination happens
// inside the count CASE, so no row is ever dropped to null by a fully-filtered
// optional.
//
// FRESHNESS (the freshness PREDICATE — bgcheck-only; payment ever-completed).
//
//   - missing_bgcheck counts a completed bgcheck toward convergence ONLY while
//     its op-stamped validUntil is still in the future
//     (inst.outcome.data.validUntil > $now). A STALE bgcheck (validUntil ≤ $now)
//     stops counting and missing_bgcheck re-opens whenever the row is
//     (re)evaluated — a stale background check IS a missing background check. The
//     freshness test lives inside the count CASE on the single providedTo fan
//     (no second match, no WHERE), so it cannot drop the anchor. validUntil is
//     computed by the replyOp as completedAt + bgcheckFreshnessWindow (Starlark
//     time.rfc3339_add — no clock read), the §10.2 "the freshness rule lives in
//     the cypher" convention. The `>` on these canonical-UTC RFC3339 strings is
//     lexicographic = chronological (ruleengine/full executor.go compareAny
//     string branch); $now is the projection-supplied param (Refractor's
//     executeFullForActor sets params["now"] = time.Now().UTC().Format(time.RFC3339)).
//   - missing_payment is ever-completed: a completed payment counts forever,
//     validUntil ignored.
//
// EAGER auto-reopen-at-expiry — the §10.2 freshUntil column.
//
//   - The lens projects a single scalar freshUntil per anchor: the LATEST
//     validUntil among the applicant's completed, still-fresh bgchecks. Weaver's
//     temporal lane reads it (freshUntilColumn) and schedules an @at one-shot at
//     that instant; when the timer fires it marks the row expired, the row
//     reprojects, and the freshness predicate re-opens missing_bgcheck the moment
//     freshness lapses — eagerly, not waiting for an incidental CDC touch.
//   - freshUntil is a max() aggregator on the SAME single no-WHERE providedTo fan
//     that drives the missing_* counts — max(validUntil) over the completed-fresh
//     bgcheck CASE, folded inside the aggregation WITH. So it is aggregated, not
//     re-expanded: an applicant with N completed-fresh bgchecks (multiple
//     applications on one identity, or accumulated freshness re-dispatches —
//     providedTo is on the identity, not the application) yields exactly one row,
//     not N (guardOutputKeyCollision stays satisfied — no separate, unaggregated
//     match to multiply the anchor). When no fresh bgcheck exists every CASE is
//     null and max() folds to null, so freshUntil projects as a genuine null
//     (Weaver clears any standing @at — no deadline to arm) and the anchor never
//     drops. Picking the LATEST (max, not min/first) is required: the @at re-open
//     timer must not fire while a later-expiring fresh bgcheck still counts toward
//     missing_bgcheck. max() over canonical-UTC RFC3339 strings is lexicographic =
//     chronological (ruleengine/full executor.go reduceExtreme → compareAny).
//
// DISPATCH SUPPRESSION — the per-gap inflight_<g> companion + maxretries_<g> cap.
//
//	inflight_<g> is a §10.2 BodyColumn Weaver reads as a dispatch-suppression
//	companion of the gap missing_<g> (the prefix-swap convention, like freshUntil):
//	while it is true Weaver does NOT (re-)dispatch the externalTask, but the gap
//	stays missing_<g>=true / violating — only re-dispatch is suppressed. It is
//	counted on the SAME single no-WHERE providedTo fan as the missing_* counts, so
//	it adds no filtered optional that could drop the anchor.
//
//	- inflight_<g> — a call of that family is legitimately in flight: a service
//	  instance with a .dispatch marker present (inst.dispatch.data.vendorRef <>
//	  null — the bridge wrote .dispatch on a Pending Execute, and vendorRef is true
//	  iff the .dispatch aspect exists) and NO .outcome yet (status = null — the
//	  create-only outcome has not landed). The predicate is presence-based, not
//	  deadline-bounded: an in-flight call is one whose dispatch landed and whose
//	  outcome has not, regardless of its give-up horizon. A dead/slow bridge that
//	  never posts the timeout outcome therefore keeps inflight_<g>=true rather than
//	  flipping it false at the deadline — closing the double-dispatch window where
//	  Weaver would re-call the vendor while the original call is still pending.
//	  Re-dispatch resumes only when the call resolves: a failed outcome lands
//	  (status != null) → inflight_<g> false → Weaver dispatches a fresh call
//	  (a new claim vertex / vendorRef — never a silent resubmit of the same one).
//	- maxretries_<g> — the per-gap retry cap, a CONSTANT integer column baked from
//	  retry_budget.go (maxBgcheckRetries / maxPaymentRetries) onto every row. The
//	  budget itself is NOT a lens predicate (a lifetime failed-count never resets on
//	  success): Weaver keeps a per-(target, entity, gap) dispatch-count in
//	  weaver-state, reads this cap off the row, and stops auto-dispatching once the
//	  count reaches it — the operator-visible "needs human escalation" terminal. The
//	  count is deleted when the gap closes, so a later renewal starts a fresh budget.
//	  Keeping the cap a package-owned column (like freshUntil) leaves the policy in
//	  the package with no contract change.
//
// DECLINED DISPOSITION — the per-family declined_<g> column + the top-level declined.
//
//	A FAILED outcome (inst.outcome.data.status = 'failed' — a definitive business
//	rejection, distinct from a transient error) keeps the gap missing_<g> open the
//	same as a never-run check, so without a dedicated column a declined application
//	is indistinguishable from one still "in progress" — it reads as blocked
//	forever. declined_<g> is the honest terminal disposition the operator / applicant
//	FE renders instead:
//
//	- declined_bgcheck — a failed bgcheck instance exists AND no completed-fresh
//	  bgcheck supersedes it ((bgFailed > 0) AND (freshBgComplete = 0)). A later
//	  retry that clears (Weaver re-dispatches a FRESH instance on a failed outcome,
//	  see inflight_<g>) flips declined_bgcheck back to false — the disposition
//	  tracks the CURRENT verdict, not a historical one.
//	- declined_payment — symmetric on the payment family ((payFailed > 0) AND
//	  (payComplete = 0)); payment is ever-completed so no freshness term.
//	- declined — the OR of the two: the application carries at least one standing
//	  rejection. It is NOT in the violating clause (declined ⊂ violating already —
//	  a declined gap is still a missing gap); it is a presentation column, like
//	  freshUntil / unitAddress. The lens cannot see Weaver's per-gap dispatch count,
//	  so declined is "a rejection stands right now," not "retries are terminally
//	  exhausted"; while a retry is in flight inflight_<g> is true and the FE prefers
//	  that ("re-checking") over the standing-rejection read.
//
// '= null' (not IS NULL) is the full engine's null test (ruleengine/full
// executor.go equalsAny treats null = null as true and any value = null as
// false). Do not "correct" it to unsupported IS NULL.
//
// leaseApplicationCompleteSpec is built once at package init: the retry caps
// (maxBgcheckRetries / maxPaymentRetries) bake into the constant maxretries_<g>
// columns Weaver bounds its dispatch-count against, the §10.2 "the policy lives in
// the cypher" convention (same posture as bgcheckFreshnessWindow). The cypher
// carries no literal '%'.
var leaseApplicationCompleteSpec = fmt.Sprintf(`
MATCH (app:leaseapp {key: $actorKey})
OPTIONAL MATCH (app)-[:applicationFor]->(id:identity)
OPTIONAL MATCH (app)-[:appliesToUnit]->(u:unit)
OPTIONAL MATCH (id)<-[:providedTo]-(inst:service)
WITH
  app.key AS entityKey,
  id.key  AS applicant,
  app.signature.data.signedAt AS signedAt,
  id.ssn.data.value AS ssnVal,
  u.key                     AS unitKey,
  u.address.data.line1      AS unitAddress,
  u.listing.data.rentAmount AS unitRent,
  count(DISTINCT CASE WHEN inst.family.data.value = 'backgroundCheck' AND inst.outcome.data.status = 'completed' AND inst.outcome.data.validUntil > $now THEN inst.key ELSE null END) AS freshBgComplete,
  count(DISTINCT CASE WHEN inst.family.data.value = 'payment' AND inst.outcome.data.status = 'completed' THEN inst.key ELSE null END) AS payComplete,
  count(DISTINCT CASE WHEN inst.family.data.value = 'backgroundCheck' AND inst.dispatch.data.vendorRef <> null AND inst.outcome.data.status = null THEN inst.key ELSE null END) AS bgInflight,
  count(DISTINCT CASE WHEN inst.family.data.value = 'payment' AND inst.dispatch.data.vendorRef <> null AND inst.outcome.data.status = null THEN inst.key ELSE null END) AS payInflight,
  count(DISTINCT CASE WHEN inst.family.data.value = 'backgroundCheck' AND inst.outcome.data.status = 'failed' THEN inst.key ELSE null END) AS bgFailed,
  count(DISTINCT CASE WHEN inst.family.data.value = 'payment' AND inst.outcome.data.status = 'failed' THEN inst.key ELSE null END) AS payFailed,
  max(CASE WHEN inst.family.data.value = 'backgroundCheck' AND inst.outcome.data.status = 'completed' AND inst.outcome.data.validUntil > $now THEN inst.outcome.data.validUntil ELSE null END) AS freshUntil
RETURN
  entityKey AS actorKey,
  entityKey,
  applicant,
  unitKey,
  unitAddress,
  unitRent,
  freshUntil,
  (ssnVal = null)        AS missing_onboarding,
  (freshBgComplete = 0)  AS missing_bgcheck,
  (payComplete = 0)      AS missing_payment,
  (signedAt = null)      AS missing_signature,
  (bgInflight > 0)       AS inflight_bgcheck,
  (payInflight > 0)      AS inflight_payment,
  ((bgFailed > 0) AND (freshBgComplete = 0))  AS declined_bgcheck,
  ((payFailed > 0) AND (payComplete = 0))     AS declined_payment,
  (((bgFailed > 0) AND (freshBgComplete = 0)) OR ((payFailed > 0) AND (payComplete = 0))) AS declined,
  %d                     AS maxretries_bgcheck,
  %d                     AS maxretries_payment,
  ((ssnVal = null) OR (freshBgComplete = 0) OR (payComplete = 0) OR (signedAt = null)) AS violating
`, maxBgcheckRetries, maxPaymentRetries)
