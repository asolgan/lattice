package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/edge/overlay"
	"github.com/asolgan/lattice/internal/edge/store"
	"github.com/asolgan/lattice/internal/processor"
	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/testutil"
)

func newAgentTestConn(t *testing.T, ctx context.Context) *substrate.Conn {
	t.Helper()
	url := testutil.StartEmbeddedNATS(t)
	conn, err := substrate.Connect(ctx, substrate.ConnectOpts{URL: url})
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	_, err = conn.JetStream().CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "core-operations",
		Subjects: []string{"ops.>"},
	})
	require.NoError(t, err)
	return conn
}

// startFakeProcessor replies to every operation envelope published on
// ops.> according to decide, mimicking the Processor's synchronous
// request-reply (commit_path.go's Lattice-Reply-Inbox header path) without
// running the real pipeline.
func startFakeProcessor(t *testing.T, conn *substrate.Conn, decide func(*processor.OperationEnvelope) processor.OperationReply) {
	t.Helper()
	sub, err := conn.NATS().Subscribe("ops.>", func(msg *nats.Msg) {
		var env processor.OperationEnvelope
		if err := json.Unmarshal(msg.Data, &env); err != nil {
			return
		}
		inbox := msg.Header.Get(replyInboxHeader)
		if inbox == "" {
			return
		}
		reply := decide(&env)
		b, err := json.Marshal(reply)
		if err != nil {
			return
		}
		_ = conn.NATS().Publish(inbox, b)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

func openTestStack(t *testing.T) (*store.Store, *overlay.Overlay) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "edge.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st, overlay.New(st)
}

type fakeRehydrator struct{ calls int }

func (f *fakeRehydrator) Rehydrate(context.Context) error {
	f.calls++
	return nil
}

const testKey = "vtx.lease.Lk2Pn6mQrtwzKbcXvP3T"

func testEnv(requestID string) *processor.OperationEnvelope {
	return &processor.OperationEnvelope{
		RequestID:     requestID,
		Lane:          processor.LaneDefault,
		OperationType: "UpdateLease",
		Actor:         "vtx.identity.Ak2Pn6mQrtwzKbcXvP3T",
		SubmittedAt:   "2026-07-10T00:00:00Z",
		Payload:       json.RawMessage(`{}`),
	}
}

func TestDrain_AcceptedDequeuesWithoutTouchingOverlay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx)
	st, ov := openTestStack(t)

	startFakeProcessor(t, conn, func(env *processor.OperationEnvelope) processor.OperationReply {
		return processor.OperationReply{RequestID: env.RequestID, Status: processor.ReplyStatusAccepted, Decision: "committed"}
	})

	require.NoError(t, ov.Apply(testKey, "req1", []byte(`{"rent":150}`), false))
	a := New(conn, st, ov, nil, Config{})
	require.NoError(t, a.Enqueue(testEnv("req1"), []string{testKey}))

	require.NoError(t, a.Drain(ctx))

	intents, err := st.ListIntents()
	require.NoError(t, err)
	require.Empty(t, intents, "an accepted intent must be dequeued")

	v, ok, err := ov.Read(testKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, v.Pending, "accept alone must not clear the overlay — only a fresher confirmed value does (R3)")
}

func TestDrain_DuplicateDequeuesWithoutTouchingOverlay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx)
	st, ov := openTestStack(t)

	startFakeProcessor(t, conn, func(env *processor.OperationEnvelope) processor.OperationReply {
		return processor.OperationReply{RequestID: env.RequestID, Status: processor.ReplyStatusDuplicate}
	})

	require.NoError(t, ov.Apply(testKey, "req1", []byte(`{"rent":150}`), false))
	a := New(conn, st, ov, nil, Config{})
	require.NoError(t, a.Enqueue(testEnv("req1"), []string{testKey}))

	require.NoError(t, a.Drain(ctx))

	intents, err := st.ListIntents()
	require.NoError(t, err)
	require.Empty(t, intents)
}

func TestDrain_RevisionConflictRehydratesAndDiscardsOverlay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx)
	st, ov := openTestStack(t)

	startFakeProcessor(t, conn, func(env *processor.OperationEnvelope) processor.OperationReply {
		return processor.OperationReply{
			RequestID: env.RequestID,
			Status:    processor.ReplyStatusRejected,
			Error:     &processor.ReplyError{Code: processor.ErrCodeRevisionConflict, Message: "stale"},
		}
	})

	require.NoError(t, ov.Apply(testKey, "req1", []byte(`{"rent":150}`), false))
	rh := &fakeRehydrator{}
	var conflicts []ConflictInfo
	a := New(conn, st, ov, rh, Config{Conflict: func(c ConflictInfo) { conflicts = append(conflicts, c) }})
	require.NoError(t, a.Enqueue(testEnv("req1"), []string{testKey}))

	require.NoError(t, a.Drain(ctx))

	intents, err := st.ListIntents()
	require.NoError(t, err)
	require.Empty(t, intents)
	require.Equal(t, 1, rh.calls, "a RevisionConflict must trigger a re-hydrate")

	_, ok, err := ov.Read(testKey)
	require.NoError(t, err)
	require.False(t, ok, "the stale overlay must be discarded, with no confirmed value to fall back to")

	require.Len(t, conflicts, 1)
	require.Equal(t, "req1", conflicts[0].RequestID)
	require.Equal(t, []string{testKey}, conflicts[0].Keys)
}

func TestDrain_OtherRejectionDiscardsWithoutRehydrate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx)
	st, ov := openTestStack(t)

	startFakeProcessor(t, conn, func(env *processor.OperationEnvelope) processor.OperationReply {
		return processor.OperationReply{
			RequestID: env.RequestID,
			Status:    processor.ReplyStatusRejected,
			Error:     &processor.ReplyError{Code: processor.ErrCodeDDLViolation, Message: "bad shape"},
		}
	})

	require.NoError(t, ov.Apply(testKey, "req1", []byte(`{"rent":150}`), false))
	rh := &fakeRehydrator{}
	a := New(conn, st, ov, rh, Config{})
	require.NoError(t, a.Enqueue(testEnv("req1"), []string{testKey}))

	require.NoError(t, a.Drain(ctx))

	require.Zero(t, rh.calls, "a non-conflict rejection must not trigger a re-hydrate")
	_, ok, err := ov.Read(testKey)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDrain_TransportFailureLeavesIntentQueued(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx) // no fake processor started — no responder ever replies.
	st, ov := openTestStack(t)

	a := New(conn, st, ov, nil, Config{})
	require.NoError(t, a.Enqueue(testEnv("req1"), nil))

	drainCtx, drainCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer drainCancel()
	err := a.Drain(drainCtx)
	require.Error(t, err)

	intents, err2 := st.ListIntents()
	require.NoError(t, err2)
	require.Len(t, intents, 1, "a transport failure must leave the intent queued for a later Drain")
}

func TestDrain_MalformedIntentIsDroppedNotWedged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx)
	st, ov := openTestStack(t)
	// store.EnqueueIntent validates its argument is syntactically valid JSON
	// (json.RawMessage), so genuinely malformed bytes can never reach the
	// queue — the reachable "malformed" case is syntactically valid JSON
	// that doesn't carry an envelope (e.g. written by a future buggy path).
	_, err := st.EnqueueIntent([]byte("{}"))
	require.NoError(t, err)

	a := New(conn, st, ov, nil, Config{})
	require.NoError(t, a.Drain(ctx))

	intents, err := st.ListIntents()
	require.NoError(t, err)
	require.Empty(t, intents)
}

func TestDrain_MultipleIntentsSubmitInFIFOOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := newAgentTestConn(t, ctx)
	st, ov := openTestStack(t)

	var seen []string
	startFakeProcessor(t, conn, func(env *processor.OperationEnvelope) processor.OperationReply {
		seen = append(seen, env.RequestID)
		return processor.OperationReply{RequestID: env.RequestID, Status: processor.ReplyStatusAccepted}
	})

	a := New(conn, st, ov, nil, Config{})
	require.NoError(t, a.Enqueue(testEnv("req1"), nil))
	require.NoError(t, a.Enqueue(testEnv("req2"), nil))
	require.NoError(t, a.Enqueue(testEnv("req3"), nil))

	require.Eventually(t, func() bool {
		_ = a.Drain(ctx)
		intents, err := st.ListIntents()
		return err == nil && len(intents) == 0
	}, 10*time.Second, 50*time.Millisecond)

	require.Equal(t, []string{"req1", "req2", "req3"}, seen)
}

func TestGC_PrunesSupersededOverlays(t *testing.T) {
	st, ov := openTestStack(t)
	_, err := st.ApplyUpsert(testKey, 3, []byte(`{"rent":100}`))
	require.NoError(t, err)
	require.NoError(t, ov.Apply(testKey, "req1", []byte(`{"rent":150}`), false))
	_, err = st.ApplyUpsert(testKey, 4, []byte(`{"rent":175}`))
	require.NoError(t, err)

	a := New(nil, st, ov, nil, Config{})
	stillPending, err := a.GC()
	require.NoError(t, err)
	require.Zero(t, stillPending)

	keys, err := ov.PendingKeys()
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestGC_KeepsUnsupersededOverlay(t *testing.T) {
	st, ov := openTestStack(t)
	require.NoError(t, ov.Apply(testKey, "req1", []byte(`{"rent":150}`), false))

	a := New(nil, st, ov, nil, Config{})
	stillPending, err := a.GC()
	require.NoError(t, err)
	require.Equal(t, 1, stillPending)
}
