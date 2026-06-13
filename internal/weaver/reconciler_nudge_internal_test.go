package weaver

import (
	"context"
	"testing"
	"time"
)

// TestSweep_CorruptEmptyClaimIDNudgeMark proves the §10.3 corrupt-claim guard:
// an expired nudge mark over a still-violating row carrying an EMPTY claimId is
// corrupt → it is alerted (CorruptMark Health issue) and deleted, and the sweep
// NEVER mints a fresh claimId for it (a fresh id would be a second
// idempotencyKey → a duplicate external call). No op is published.
func TestSweep_CorruptEmptyClaimIDNudgeMark(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newSweepHarness(t, ctx)

	const targetID = "fixtureNudgeCorrupt"
	h.seedTarget(&Target{
		TargetID: targetID,
		Gaps:     map[string]GapAction{"missing_check": {Action: actionNudge, Adapter: "backgroundCheck", Operation: "ResolveCheck"}},
	})
	entityID := testNanoID(t)
	key := markKey(targetID, entityID, "missing_check")

	// A nudge mark with an EMPTY claimId — the impossible-by-construction shape
	// that, if ever observed, is corrupt.
	corrupt := fixtureMark(targetID, entityID, "missing_check", actionNudge, pastLease())
	corrupt.ClaimID = ""
	h.putMark(t, ctx, key, corrupt)
	h.putRow(t, ctx, targetID, entityID, map[string]any{
		"entityKey": "vtx.leaseApp." + entityID, "violating": true, "missing_check": true,
	})

	h.pass(ctx)

	if h.markExists(t, ctx, key) {
		t.Fatal("corrupt empty-claimId nudge mark must be deleted")
	}
	if !hasIssueCode(h.engine.issues.snapshot(), "CorruptMark") {
		t.Fatal("corrupt empty-claimId nudge mark must raise a CorruptMark Health issue")
	}
	if _, _, corruptCount, _ := h.engine.sweep.metrics(); corruptCount != 1 {
		t.Fatalf("sweepCorrupt = %d, want 1", corruptCount)
	}
	// No op (no nudge dispatch, no resolve) — the guard fired before any plan.
	h.requireNoOp(t)
}

// TestSweep_NudgeMarkWithClaimIDNotCorrupt proves the guard is precise: a nudge
// mark that DOES carry a claimId is not treated as corrupt by the empty-claimId
// guard. (Its live-nudge reclaim/dispatch is Story 10.2 — buildPlan's actionNudge
// still returns a planError here, so the mark is left for the next sweep, not
// deleted as corrupt and not re-fired.)
func TestSweep_NudgeMarkWithClaimIDNotCorrupt(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newSweepHarness(t, ctx)

	const targetID = "fixtureNudgeOK"
	h.seedTarget(&Target{
		TargetID: targetID,
		Gaps:     map[string]GapAction{"missing_check": {Action: actionNudge, Adapter: "backgroundCheck", Operation: "ResolveCheck"}},
	})
	entityID := testNanoID(t)
	key := markKey(targetID, entityID, "missing_check")

	withClaim := fixtureMark(targetID, entityID, "missing_check", actionNudge, pastLease())
	withClaim.ClaimID = "claimIDpresent012345"
	h.putMark(t, ctx, key, withClaim)
	h.putRow(t, ctx, targetID, entityID, map[string]any{
		"entityKey": "vtx.leaseApp." + entityID, "violating": true, "missing_check": true,
	})

	h.pass(ctx)

	if !h.markExists(t, ctx, key) {
		t.Fatal("a nudge mark carrying a claimId must NOT be deleted as corrupt")
	}
	if hasIssueCode(h.engine.issues.snapshot(), "CorruptMark") {
		t.Fatal("a nudge mark carrying a claimId must not raise CorruptMark")
	}
	if _, _, corruptCount, _ := h.engine.sweep.metrics(); corruptCount != 0 {
		t.Fatalf("sweepCorrupt = %d, want 0", corruptCount)
	}
	h.requireNoOp(t)
}
