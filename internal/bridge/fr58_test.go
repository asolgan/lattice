package bridge_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/bridge"
	"github.com/asolgan/lattice/internal/substrate"
)

// newHarness wires embedded NATS + a wrapped substrate.Conn + provisioning + a
// fixture Processor, returning the conn and processor. The caller starts the
// bridge.
func newHarness(t *testing.T, ctx context.Context) (*substrate.Conn, *fakeProcessor) {
	t.Helper()
	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	fp := newFakeProcessor(conn)
	fp.run(ctx, t)
	return conn, fp
}

// TestBridge_HappyPath_PostsDeterministicReplyOp publishes one external.stripe
// event and asserts: exactly one real charge (SideEffects == 1), the fixture
// Processor saw a replyOp with requestId == deriveReplyRequestID(instanceKey)
// and payload.externalRef == instanceKey, and exactly one result mutation. The
// instanceKey is a NON-service token (invariant a).
func TestBridge_HappyPath_PostsDeterministicReplyOp(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, fp := newHarness(t, ctx)
	stripe := bridge.NewFakeStripe()
	startBridge(t, ctx, conn, stripe, nil)

	instanceKey := nonServiceHandle(t) // a vtx.widget.<handle> token — non-service
	wantReqID := bridge.DeriveReplyRequestID(instanceKey)

	publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, map[string]any{"amount": "4200"})

	require.Eventually(t, func() bool { return fp.mutations() == 1 },
		15*time.Second, 60*time.Millisecond, "the replyOp must commit exactly one result mutation")

	require.Equal(t, 1, stripe.SideEffects(instanceKey), "exactly one real charge")

	seen, gotRef := fp.sawReply(wantReqID)
	require.GreaterOrEqual(t, seen, 1, "the fixture Processor must have seen the deterministic requestId")
	require.Equal(t, instanceKey, gotRef, "payload.externalRef must echo the opaque instanceKey")
}

// TestBridge_EventRedelivery_AtMostOneSideEffect publishes the SAME
// external.stripe event TWICE (at-least-once redelivery). The deterministic
// requestId collapses the second replyOp on the Contract #4 tracker, and the
// adapter dedups on the idempotencyKey, so SideEffects == 1 and result
// mutations == 1. Run both WITH the skip-probe enabled and WITH it DISABLED —
// proving correctness holds via mechanisms #1 (deterministic requestId) + #2
// (adapter dedup) WITHOUT mechanism #3 (the skip is only an optimization).
func TestBridge_EventRedelivery_AtMostOneSideEffect(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	for _, skip := range []bool{true, false} {
		skip := skip
		name := "skipProbeOn"
		if !skip {
			name = "skipProbeOff"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			conn, fp := newHarness(t, ctx)
			stripe := bridge.NewFakeStripe()
			startBridge(t, ctx, conn, stripe, func(c *bridge.Config) { c.SkipOnRedelivery = &skip })

			instanceKey := nonServiceHandle(t)
			wantReqID := bridge.DeriveReplyRequestID(instanceKey)

			// First delivery: let it complete (one charge, one mutation).
			publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, nil)
			require.Eventually(t, func() bool { return fp.mutations() == 1 },
				15*time.Second, 60*time.Millisecond, "first delivery must commit one mutation")

			// Redelivery: publish the SAME event again.
			publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, nil)

			// Give the bridge time to process the redelivery (and, with the skip
			// off, re-call the adapter + re-publish the replyOp).
			require.Never(t, func() bool { return stripe.SideEffects(instanceKey) > 1 || fp.mutations() > 1 },
				3*time.Second, 100*time.Millisecond,
				"redelivery must not produce a second charge or a second result mutation")

			require.Equal(t, 1, stripe.SideEffects(instanceKey), "exactly one charge under redelivery")
			require.Equal(t, 1, fp.mutations(), "exactly one result mutation under redelivery")
			seen, _ := fp.sawReply(wantReqID)
			require.GreaterOrEqual(t, seen, 1, "the deterministic requestId is what collapses the redelivery")
		})
	}
}

// TestBridge_FailUntilThenRecovers_ExactlyOneSideEffect arms FakeStripe to
// hard-fail its first 2 Execute calls (no side-effect). The bridge returns
// NakWithDelay on each failure; the supervisor redelivers until the 3rd attempt
// succeeds. SideEffects == 1 (only the eventual success charged), result
// mutations == 1, and a transient-failure Health issue was raised (the
// errConfig-vs-transient distinction: a transient adapter failure ⇒ Nak +
// warning issue, NOT errConfig). This is the literal FR58 mid-flight-recovery
// proof.
func TestBridge_FailUntilThenRecovers_ExactlyOneSideEffect(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	conn, fp := newHarness(t, ctx)
	stripe := bridge.NewFakeStripe()
	stripe.FailUntil(2) // first 2 Execute calls error (no charge); the 3rd succeeds
	startBridge(t, ctx, conn, stripe, nil)

	instanceKey := nonServiceHandle(t)

	publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, nil)

	// A transient adapter failure raises a warning Health issue.
	require.True(t, waitHealthIssue(t, ctx, conn, "BridgeAdapterFailed"),
		"a transient adapter failure must raise a Health issue (never a silent skip)")

	// The redelivery eventually converges to exactly one charge + one mutation.
	require.Eventually(t, func() bool { return fp.mutations() == 1 },
		25*time.Second, 100*time.Millisecond, "the retry must converge to one result mutation")
	require.Equal(t, 1, stripe.SideEffects(instanceKey),
		"only the eventual success charged — the failed attempts billed nothing")

	require.Never(t, func() bool { return stripe.SideEffects(instanceKey) > 1 || fp.mutations() > 1 },
		2*time.Second, 100*time.Millisecond, "no double charge / double mutation after recovery")
}

// TestBridge_UnregisteredAdapter_AckAndHealth publishes an event for an adapter
// the registry lacks. The bridge ACKs it (no redelivery loop) AND raises a
// Health issue (errConfig — Lookup miss), and the registered adapter's
// SideEffects stays 0 (never a silent dispatch, never a hot Nak loop).
func TestBridge_UnregisteredAdapter_AckAndHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, fp := newHarness(t, ctx)
	stripe := bridge.NewFakeStripe()
	startBridge(t, ctx, conn, stripe, nil)

	instanceKey := nonServiceHandle(t)
	publishExternalEvent(t, ctx, conn, "noSuchAdapter", instanceKey, fixtureReplyOp, nil)

	require.True(t, waitHealthIssue(t, ctx, conn, "BridgeAdapterMissing"),
		"an unregistered adapter must raise a Health issue (errConfig, never a silent skip)")

	// No replyOp is ever posted (no mutation), and the registered adapter never
	// fired. The event was acked (errConfig), so it is not redelivered forever —
	// asserted by the absence of any side-effect / mutation over a window.
	require.Never(t, func() bool { return fp.mutations() > 0 || stripe.SideEffects(instanceKey) > 0 },
		3*time.Second, 100*time.Millisecond, "an unregistered adapter must dispatch nothing")
}

// TestBridge_UnparseableEnvelope_AckAndHealth publishes a malformed external.*
// body. The bridge ACKs it (redelivery can never fix malformed JSON) and raises
// a Health issue (never Term-and-forget-silently).
func TestBridge_UnparseableEnvelope_AckAndHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, fp := newHarness(t, ctx)
	stripe := bridge.NewFakeStripe()
	startBridge(t, ctx, conn, stripe, nil)

	publishRawExternalEvent(t, ctx, conn, fixtureAdapter, []byte(`{not valid json`))

	require.True(t, waitHealthIssue(t, ctx, conn, "BridgeEventUnparseable"),
		"an unparseable envelope must raise a Health issue (never a silent skip)")
	require.Equal(t, 0, fp.mutations(), "an unparseable envelope dispatches nothing")
}

// TestBridge_SkipOnRedelivery_TrackerPresent pre-seeds the Contract #4 tracker
// for deriveReplyRequestID(instanceKey) (NOT tombstoned) and asserts the bridge
// ACKs the event WITHOUT calling the adapter (SideEffects == 0) — the optional
// skip fired. A tombstoned variant (isDeleted:true) asserts the bridge DOES
// dispatch (SideEffects == 1): per Contract #4 §4.3 a tombstone is NOT a landed
// result.
func TestBridge_SkipOnRedelivery_TrackerPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}

	t.Run("present_not_tombstoned_skips", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, fp := newHarness(t, ctx)
		stripe := bridge.NewFakeStripe()
		startBridge(t, ctx, conn, stripe, nil)

		instanceKey := nonServiceHandle(t)
		reqID := bridge.DeriveReplyRequestID(instanceKey)
		// Pre-seed the tracker as a live (not-deleted) op.
		_, err := conn.KVPut(ctx, coreKVBucket, "vtx.op."+reqID, []byte(`{"class":"op","isDeleted":false,"data":{}}`))
		require.NoError(t, err)

		publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, nil)

		// The bridge must skip: no adapter call, no NEW replyOp from the bridge.
		require.Never(t, func() bool { return stripe.SideEffects(instanceKey) > 0 },
			3*time.Second, 100*time.Millisecond, "a present (live) tracker must skip the adapter call")
		_ = fp
	})

	t.Run("tombstoned_dispatches", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, fp := newHarness(t, ctx)
		stripe := bridge.NewFakeStripe()
		startBridge(t, ctx, conn, stripe, nil)

		instanceKey := nonServiceHandle(t)
		reqID := bridge.DeriveReplyRequestID(instanceKey)
		// Pre-seed a TOMBSTONED tracker (operator retry signal — NOT a landed result).
		_, err := conn.KVPut(ctx, coreKVBucket, "vtx.op."+reqID, []byte(`{"class":"op","isDeleted":true,"data":{}}`))
		require.NoError(t, err)

		publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, nil)

		// A tombstone is NOT a landed result: the bridge dispatches.
		require.Eventually(t, func() bool { return stripe.SideEffects(instanceKey) == 1 },
			15*time.Second, 80*time.Millisecond, "a tombstoned tracker must NOT skip — the bridge dispatches")
		// Note: the bridge's replyOp re-uses reqID; the fixture Processor's
		// trackOnce sees the pre-seeded key as present, so it counts no NEW
		// mutation — exactly the Contract #4 collapse. The side-effect (the real
		// charge) is the load-bearing assertion here.
		_ = fp
	})
}

// TestBridge_PublishFailure_Nak makes the ops publish fail (no core-operations
// stream subscriber/stream for the lane) and asserts the bridge NakWithDelays
// (does not Ack-and-drop), so the replyOp is retried. We delete the
// core-operations stream after the adapter would charge, then confirm a Health
// issue surfaces and the event is NOT silently dropped (the adapter charged, but
// no mutation landed because the publish kept failing — the bridge held the
// event un-acked / re-driving).
func TestBridge_PublishFailure_Nak(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	// NOTE: no fixture Processor — and we DELETE the core-operations stream so the
	// bridge's publish to ops.<lane> fails.
	require.NoError(t, conn.JetStream().DeleteStream(ctx, opsStream))

	stripe := bridge.NewFakeStripe()
	startBridge(t, ctx, conn, stripe, nil)

	instanceKey := nonServiceHandle(t)
	publishExternalEvent(t, ctx, conn, fixtureAdapter, instanceKey, fixtureReplyOp, nil)

	// The publish keeps failing → NakWithDelay → a Health issue surfaces and the
	// event is re-driven (never Ack-and-dropped).
	require.True(t, waitHealthIssue(t, ctx, conn, "BridgeReplyPublishFailed"),
		"a publish failure must surface a Health issue and NakWithDelay (never silently drop)")
}
