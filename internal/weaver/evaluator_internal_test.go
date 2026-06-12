package weaver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/substrate"
)

// handlerHarness is an Engine wired to an embedded NATS server with its
// registry seeded directly, so handleRow can be driven with constructed
// substrate.Message values (controlled Sequence/NumDelivered — the metadata
// branches a live consumer cannot script).
type handlerHarness struct {
	engine *Engine
	conn   *substrate.Conn
	ops    *nats.Subscription
}

func newHandlerHarness(t *testing.T, ctx context.Context) *handlerHarness {
	t.Helper()
	opts := &natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()}
	srv := natstest.RunServer(opts)
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	conn, err := substrate.Wrap(nc)
	if err != nil {
		t.Fatalf("substrate wrap: %v", err)
	}
	js := conn.JetStream()
	// LimitMarkerTTL mirrors bootstrap provisioning: weaver-state marks carry a
	// per-key TTL, which the server only honours on a TTL-capable bucket.
	if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "weaver-state", LimitMarkerTTL: time.Second}); err != nil {
		t.Fatalf("create weaver-state: %v", err)
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: "core-operations", Subjects: []string{"ops.>"},
	}); err != nil {
		t.Fatalf("create ops stream: %v", err)
	}
	ops, err := nc.SubscribeSync("ops.system")
	if err != nil {
		t.Fatalf("subscribe ops: %v", err)
	}
	t.Cleanup(func() { _ = ops.Unsubscribe() })

	engine := NewEngine(conn, Config{
		ActorKey: "vtx.identity.WeaverServiceActor1abc",
		Instance: "unit-" + testNanoID(t),
		Logger:   discardLogger(),
	})
	return &handlerHarness{engine: engine, conn: conn, ops: ops}
}

func (h *handlerHarness) seedTarget(target *Target) {
	h.engine.source.mu.Lock()
	h.engine.source.targets[target.TargetID] = target
	h.engine.source.mu.Unlock()
}

func (h *handlerHarness) seedPattern(ref, vertexID string) {
	h.engine.source.mu.Lock()
	h.engine.source.patternMeta[ref] = "vtx.meta." + vertexID
	h.engine.source.mu.Unlock()
}

func (h *handlerHarness) rowMessage(t *testing.T, targetID, entityID string, row map[string]any, sequence, numDelivered uint64) substrate.Message {
	t.Helper()
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	return substrate.Message{
		Subject:      h.engine.rowSubjectPrefix + targetID + "." + entityID,
		Body:         body,
		Sequence:     sequence,
		NumDelivered: numDelivered,
	}
}

func (h *handlerHarness) nextOp(t *testing.T) map[string]any {
	t.Helper()
	msg, err := h.ops.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("expected an op on ops.system: %v", err)
	}
	var op map[string]any
	if err := json.Unmarshal(msg.Data, &op); err != nil {
		t.Fatalf("unmarshal op: %v", err)
	}
	return op
}

func (h *handlerHarness) requireNoOp(t *testing.T) {
	t.Helper()
	if msg, err := h.ops.NextMsg(500 * time.Millisecond); err == nil {
		t.Fatalf("expected no op on ops.system, got: %s", string(msg.Data))
	}
}

// TestHandleRow_NumDeliveredBranches walks the in-flight-mark decision point:
// a FRESH delivery (NumDelivered 1) with an existing mark anti-storm drops; a
// REDELIVERY (NumDelivered > 1) with an existing mark re-publishes the SAME
// episode requestId; missing metadata (NumDelivered/Sequence 0) takes the
// conservative side — never the drop, never an expectedRevision of 0.
func TestHandleRow_NumDeliveredBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newHandlerHarness(t, ctx)

	const targetID = "fixtureRetry"
	h.seedTarget(&Target{
		TargetID: targetID,
		Gaps:     map[string]GapAction{"missing_x": {Action: actionDirectOp, Operation: "FixX"}},
	})
	entityID := testNanoID(t)
	row := map[string]any{
		"entityKey": "vtx.leaseApp." + entityID,
		"violating": true,
		"missing_x": true,
	}

	// Fresh delivery, no mark: dispatches (creates the mark + fires).
	dec := h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 5, 1))
	if dec != substrate.Ack {
		t.Fatalf("initial dispatch must Ack, got %v", dec)
	}
	first := h.nextOp(t)
	_, markRev, inFlight, err := h.engine.marks.get(ctx, targetID, entityID, "missing_x")
	if err != nil || !inFlight {
		t.Fatalf("mark must exist after dispatch (err=%v, inFlight=%v)", err, inFlight)
	}
	wantRequestID := deriveEpisodeRequestID(targetID, entityID, "missing_x", markRev)
	if first["requestId"] != wantRequestID {
		t.Fatalf("dispatch requestId = %v, want %v", first["requestId"], wantRequestID)
	}

	// Fresh delivery (NumDelivered 1) + existing mark: the anti-storm drop.
	dec = h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 6, 1))
	if dec != substrate.Ack {
		t.Fatalf("anti-storm drop must Ack, got %v", dec)
	}
	h.requireNoOp(t)

	// Redelivery (NumDelivered 2) + existing mark: re-fires the SAME episode
	// requestId (idempotent at the Contract #4 tracker).
	dec = h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 5, 2))
	if dec != substrate.Ack {
		t.Fatalf("redelivery re-fire must Ack, got %v", dec)
	}
	refire := h.nextOp(t)
	if refire["requestId"] != wantRequestID {
		t.Fatalf("re-fire requestId = %v, want the same episode %v", refire["requestId"], wantRequestID)
	}

	// Metadata unavailable (Sequence 0, NumDelivered 0): defer on a delayed
	// redelivery — no anti-storm drop, no expectedRevision 0 published.
	dec = h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 0, 0))
	if dec != substrate.NakWithDelay {
		t.Fatalf("metadata-less delivery must NakWithDelay, got %v", dec)
	}
	h.requireNoOp(t)

	// NumDelivered 0 with usable Sequence: not classified as fresh — the
	// possible-redelivery re-fires the same episode (the safe side).
	dec = h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 7, 0))
	if dec != substrate.Ack {
		t.Fatalf("NumDelivered-0 re-fire must Ack, got %v", dec)
	}
	refire = h.nextOp(t)
	if refire["requestId"] != wantRequestID {
		t.Fatalf("NumDelivered-0 re-fire requestId = %v, want %v", refire["requestId"], wantRequestID)
	}
}

// TestHandleRow_UnresolvedReference proves an unresolvable playbook reference
// never hot-loops and never sits silent: the gap defers on NakWithDelay with
// an UnresolvedReference Health issue, no mark is claimed, and a later-
// installed pattern recovers on redelivery (issue cleared, episode fired).
func TestHandleRow_UnresolvedReference(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newHandlerHarness(t, ctx)

	const targetID = "fixtureGhost"
	h.seedTarget(&Target{
		TargetID: targetID,
		Gaps: map[string]GapAction{
			"missing_y": {Action: actionTriggerLoom, Pattern: "ghostFlow", Subject: "row.entityKey"},
		},
	})
	entityID := testNanoID(t)
	row := map[string]any{
		"entityKey": "vtx.leaseApp." + entityID,
		"violating": true,
		"missing_y": true,
	}

	// The pattern is not installed: defer with delay + surface to Health.
	dec := h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 5, 1))
	if dec != substrate.NakWithDelay {
		t.Fatalf("unresolved pattern ref must NakWithDelay, got %v", dec)
	}
	h.requireNoOp(t)
	if !hasIssueCode(h.engine.issues.snapshot(), "UnresolvedReference") {
		t.Fatalf("an unresolved reference must surface an UnresolvedReference Health issue")
	}
	if _, _, inFlight, err := h.engine.marks.get(ctx, targetID, entityID, "missing_y"); err != nil || inFlight {
		t.Fatalf("no mark may be claimed while the reference is unresolved (err=%v, inFlight=%v)", err, inFlight)
	}

	// The pattern is installed later: the redelivery resolves, fires, and
	// clears the issue.
	patternVtx := testNanoID(t)
	h.seedPattern("ghostFlow", patternVtx)
	dec = h.engine.handleRow(ctx, h.rowMessage(t, targetID, entityID, row, 5, 2))
	if dec != substrate.Ack {
		t.Fatalf("resolved redelivery must Ack, got %v", dec)
	}
	op := h.nextOp(t)
	if op["operationType"] != "StartLoomPattern" {
		t.Fatalf("expected StartLoomPattern, got %v", op["operationType"])
	}
	if hasIssueCode(h.engine.issues.snapshot(), "UnresolvedReference") {
		t.Fatalf("the UnresolvedReference issue must clear once the reference resolves")
	}
}
