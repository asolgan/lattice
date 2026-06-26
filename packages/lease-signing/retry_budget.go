package leasesigning

// maxBgcheckRetries and maxPaymentRetries cap how many external-call attempts a
// gap tolerates before Weaver stops auto-dispatching it. The lens projects each
// as the constant maxretries_<gap> column on every convergence row; Weaver bounds
// its per-(target, entity, gap) dispatch-count in weaver-state against that cap.
// Once the count reaches the cap the gap stays violating but is no longer
// auto-dispatched — the terminal is "stop and escalate," not a silent reject. A
// human (or a control-plane action) resolves it; a check that completes closes the
// gap, which deletes the dispatch-count so a later renewal starts a fresh budget.
//
// The two families are capped independently. The values are baked into
// leaseApplicationCompleteSpec at package-init time (compile-time constants), the
// §10.2 "the policy lives in the cypher" convention — the same posture as
// bgcheckFreshnessWindow.
const (
	maxBgcheckRetries = 3
	maxPaymentRetries = 3
)

// maxOnboardingDispatches and maxSignatureDispatches cap how many times Weaver
// (re-)dispatches the two HUMAN userTask gaps — onboarding (the RecordIdentityPII
// task) and signing (the SignLease task). They are 1: a userTask is created ONCE
// and then left alone, because — unlike an external call that should resolve in
// minutes — a human may legitimately take days, far longer than the §10.3
// mark-lease (30m). Without a cap the reconciler's mark-lease reclaim presumes the
// dispatch dead every 30m and re-dispatches, spawning a DUPLICATE task each pass
// (the externalTask gaps are protected by their inflight_<g> companion; the
// userTask gaps had no suppression companion at all). The cap = 1 gives the
// userTask gaps the same gapSuppressed protection via the maxretries_<g> term:
// after the single dispatch the per-(target, entity, gap) dispatch-count reaches
// 1 and re-dispatch is suppressed; completing the task closes the gap and resets
// the count. Recovering a userTask that is genuinely lost (expired unfilled) is
// the deferred FR28 role-queue + escalation concern, not auto-re-creation here.
const (
	maxOnboardingDispatches = 1
	maxSignatureDispatches  = 1
)
