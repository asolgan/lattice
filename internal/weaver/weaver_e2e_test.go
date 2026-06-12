package weaver_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/weaver"
)

// --- Embedded NATS + provisioning -------------------------------------------

func startNATS(t *testing.T) *nats.Conn {
	t.Helper()
	opts := &natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()}
	s := natstest.RunServer(opts)
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

const (
	coreKVBucket        = "core-kv"
	weaverTargetsBucket = "weaver-targets"
	weaverStateBucket   = "weaver-state"
	healthKVBucket      = "health-kv"
	opsStream           = "core-operations"
	weaverActorKey      = "vtx.identity.WeaverServiceActor1abc" // fixture actor key (no Processor in these tests)
)

func provision(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()
	js := conn.JetStream()
	// TTL-capable buckets mirror bootstrap provisioning; weaver-targets stays
	// plain (durable projections, no per-key TTLs), history 1.
	for _, b := range []string{coreKVBucket, weaverStateBucket, healthKVBucket} {
		_, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: b, LimitMarkerTTL: time.Second})
		require.NoError(t, err)
	}
	_, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: weaverTargetsBucket})
	require.NoError(t, err)
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: opsStream, Subjects: []string{"ops.>"},
	})
	require.NoError(t, err)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// --- Fixture installs ---------------------------------------------------------
//
// The "fixture target Lens" of AC 1 is the TEST writing §10.2-shaped rows
// directly into weaver-targets; Refractor wiring of a real target Lens is the
// lease-signing package's job (Epic 11). Meta-vertices are written the way the
// Processor write path lands them (vertex envelope + spec aspect envelope), so
// the registry CDC source loads them exactly as in production.

func installWeaverTarget(t *testing.T, ctx context.Context, conn *substrate.Conn, vertexID string, target map[string]any) {
	t.Helper()
	vtxKey := "vtx.meta." + vertexID
	vtxBody, _ := json.Marshal(map[string]any{"class": "meta.weaverTarget", "data": map[string]any{}})
	_, err := conn.KVPut(ctx, coreKVBucket, vtxKey, vtxBody)
	require.NoError(t, err)

	specEnvelope, _ := json.Marshal(map[string]any{"class": "weaverTargetSpec", "data": target})
	_, err = conn.KVPut(ctx, coreKVBucket, vtxKey+".spec", specEnvelope)
	require.NoError(t, err)
}

func tombstoneMetaVertex(t *testing.T, ctx context.Context, conn *substrate.Conn, vertexID string) {
	t.Helper()
	require.NoError(t, conn.KVDelete(ctx, coreKVBucket, "vtx.meta."+vertexID))
}

func installLoomPattern(t *testing.T, ctx context.Context, conn *substrate.Conn, vertexID, patternID string) {
	t.Helper()
	vtxKey := "vtx.meta." + vertexID
	vtxBody, _ := json.Marshal(map[string]any{"class": "meta.loomPattern", "data": map[string]any{}})
	_, err := conn.KVPut(ctx, coreKVBucket, vtxKey, vtxBody)
	require.NoError(t, err)
	spec := map[string]any{
		"patternId":   patternID,
		"subjectType": "identity",
		"steps":       []map[string]any{{"kind": "systemOp", "operation": "StepA"}},
	}
	specEnvelope, _ := json.Marshal(map[string]any{"class": "loomPatternSpec", "data": spec})
	_, err = conn.KVPut(ctx, coreKVBucket, vtxKey+".spec", specEnvelope)
	require.NoError(t, err)
}

func installOpMeta(t *testing.T, ctx context.Context, conn *substrate.Conn, vertexID, operationType string) {
	t.Helper()
	vtxKey := "vtx.meta." + vertexID
	vtxBody, _ := json.Marshal(map[string]any{
		"class": "meta.ddl.vertexType",
		"data":  map[string]any{"operationType": operationType},
	})
	_, err := conn.KVPut(ctx, coreKVBucket, vtxKey, vtxBody)
	require.NoError(t, err)
}

// putRow writes one §10.2-shaped row into weaver-targets under
// <targetId>.<entityId>.
func putRow(t *testing.T, ctx context.Context, conn *substrate.Conn, targetID, entityID string, row map[string]any) {
	t.Helper()
	if _, ok := row["projectedAt"]; !ok {
		row["projectedAt"] = substrate.FormatTimestamp(time.Now())
	}
	body, _ := json.Marshal(row)
	_, err := conn.KVPut(ctx, weaverTargetsBucket, targetID+"."+entityID, body)
	require.NoError(t, err)
}

// --- Engine + observation helpers --------------------------------------------

func newEngine(conn *substrate.Conn, instance string, opts ...func(*weaver.Config)) *weaver.Engine {
	cfg := weaver.Config{
		CoreKVBucket:        coreKVBucket,
		WeaverTargetsBucket: weaverTargetsBucket,
		WeaverStateBucket:   weaverStateBucket,
		HealthKVBucket:      healthKVBucket,
		ActorKey:            weaverActorKey,
		Lane:                "system",
		Instance:            instance,
		Logger:              testLogger(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return weaver.NewEngine(conn, cfg)
}

func mustNanoID(t *testing.T) string {
	t.Helper()
	id, err := substrate.NewNanoID()
	require.NoError(t, err)
	return id
}

// capturedOp is the decoded view of one ops.<lane> publish.
type capturedOp struct {
	RequestID     string         `json:"requestId"`
	Lane          string         `json:"lane"`
	OperationType string         `json:"operationType"`
	Actor         string         `json:"actor"`
	Payload       map[string]any `json:"payload"`
	AuthContext   struct {
		Target string `json:"target"`
	} `json:"authContext"`
}

func subscribeOps(t *testing.T, nc *nats.Conn) *nats.Subscription {
	t.Helper()
	sub, err := nc.SubscribeSync("ops.system")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return sub
}

func nextOp(t *testing.T, sub *nats.Subscription, timeout time.Duration) *capturedOp {
	t.Helper()
	msg, err := sub.NextMsg(timeout)
	require.NoError(t, err, "expected an op on ops.system")
	var op capturedOp
	require.NoError(t, json.Unmarshal(msg.Data, &op))
	return &op
}

func requireNoOp(t *testing.T, sub *nats.Subscription, window time.Duration) {
	t.Helper()
	msg, err := sub.NextMsg(window)
	if err == nil {
		t.Fatalf("expected no op on ops.system, got: %s", string(msg.Data))
	}
	require.ErrorIs(t, err, nats.ErrTimeout)
}

func waitConsumer(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) {
	t.Helper()
	js := conn.JetStream()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := js.Consumer(ctx, "KV_"+weaverTargetsBucket, durable); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("durable %q never appeared on KV_%s", durable, weaverTargetsBucket)
}

func waitConsumerGone(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) {
	t.Helper()
	js := conn.JetStream()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := js.Consumer(ctx, "KV_"+weaverTargetsBucket, durable); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("durable %q was never deleted from KV_%s", durable, weaverTargetsBucket)
}

func consumerExists(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) bool {
	t.Helper()
	_, err := conn.JetStream().Consumer(ctx, "KV_"+weaverTargetsBucket, durable)
	return err == nil
}

func waitMark(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := conn.KVGet(ctx, weaverStateBucket, key); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("mark %q never appeared in %s", key, weaverStateBucket)
}

func waitMarkGone(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := conn.KVGet(ctx, weaverStateBucket, key); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("mark %q was never cleared from %s", key, weaverStateBucket)
}

// --- Tests --------------------------------------------------------------------

// TestWeaverE2E_HappyPath proves AC 1/3/4/5/6/7: a meta.weaverTarget install
// brings up the per-target lane-1 durable; a violating fixture row CAS-creates
// the §10.3 mark and fires the remediation op (triggerLoom → StartLoomPattern
// with pattern-as-target auth, the deterministic episode requestId, and the
// payload-carried expectedRevision); flipping the row to violating:false
// level-reconciles the mark away with no further ops.
func TestWeaverE2E_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	ops := subscribeOps(t, nc)

	patternVtx := mustNanoID(t)
	installLoomPattern(t, ctx, conn, patternVtx, "onboarding")

	targetID := "fixtureComplete"
	targetVtx := mustNanoID(t)
	installWeaverTarget(t, ctx, conn, targetVtx, map[string]any{
		"targetId": targetID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_onboarding": map[string]any{
				"action": "triggerLoom", "pattern": "onboarding", "subject": "row.applicant",
			},
		},
	})

	engine := newEngine(conn, "e2e-happy-"+mustNanoID(t))
	engCtx, engCancel := context.WithCancel(ctx)
	defer engCancel()
	go func() { _ = engine.Start(engCtx) }()

	durable := "weaver-target-" + targetID
	waitConsumer(t, ctx, conn, durable)

	entityID := mustNanoID(t)
	applicant := "vtx.identity." + mustNanoID(t)
	entityKey := "vtx.leaseApp." + entityID
	putRow(t, ctx, conn, targetID, entityID, map[string]any{
		"entityKey":          entityKey,
		"violating":          true,
		"missing_onboarding": true,
		"applicant":          applicant,
	})

	op := nextOp(t, ops, 15*time.Second)
	require.Equal(t, "StartLoomPattern", op.OperationType)
	require.Equal(t, "system", op.Lane)
	require.Equal(t, weaverActorKey, op.Actor)
	require.True(t, substrate.IsValidNanoID(op.RequestID), "episode requestId must be a 20-char NanoID")
	require.Equal(t, "vtx.meta."+patternVtx, op.AuthContext.Target,
		"authContext.target must be the resolved pattern meta-vertex (pattern-as-target)")
	require.Equal(t, "vtx.meta."+patternVtx, op.Payload["patternRef"])
	require.Equal(t, applicant, op.Payload["subjectKey"])
	require.NotZero(t, op.Payload["expectedRevision"], "payload must carry the row's OCC revision-condition")

	markKey := targetID + "." + entityID + ".missing_onboarding"
	waitMark(t, ctx, conn, markKey)
	entry, err := conn.KVGet(ctx, weaverStateBucket, markKey)
	require.NoError(t, err)
	var mk map[string]any
	require.NoError(t, json.Unmarshal(entry.Value, &mk))
	require.Equal(t, targetID, mk["targetId"])
	require.Equal(t, entityKey, mk["entityKey"])
	require.Equal(t, "missing_onboarding", mk["gap"])
	require.Equal(t, "triggerLoom", mk["action"])
	require.NotEmpty(t, mk["claimedAt"])

	// Gap closes: the Lens flips the flags via upsert; Weaver stops acting and
	// the mark is cleared (level-reconciled, AC 7).
	putRow(t, ctx, conn, targetID, entityID, map[string]any{
		"entityKey":          entityKey,
		"violating":          false,
		"missing_onboarding": false,
		"applicant":          applicant,
	})
	waitMarkGone(t, ctx, conn, markKey)
	requireNoOp(t, ops, 2*time.Second)
}

// TestWeaverE2E_AssignTask proves the assignTask action contract: the gap
// resolves CreateTask with forOperation resolved from the live op meta-vertex
// index, the templated assignee/target substituted from the row, and the
// episode-deterministic taskId supplied in the payload.
func TestWeaverE2E_AssignTask(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	ops := subscribeOps(t, nc)

	opVtx := mustNanoID(t)
	installOpMeta(t, ctx, conn, opVtx, "SignFixture")

	targetID := "fixtureSign"
	installWeaverTarget(t, ctx, conn, mustNanoID(t), map[string]any{
		"targetId": targetID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_signature": map[string]any{
				"action": "assignTask", "operation": "SignFixture",
				"assignee": "row.applicant", "target": "row.entityKey",
			},
		},
	})

	engine := newEngine(conn, "e2e-task-"+mustNanoID(t))
	engCtx, engCancel := context.WithCancel(ctx)
	defer engCancel()
	go func() { _ = engine.Start(engCtx) }()
	waitConsumer(t, ctx, conn, "weaver-target-"+targetID)

	entityID := mustNanoID(t)
	applicant := "vtx.identity." + mustNanoID(t)
	entityKey := "vtx.leaseApp." + entityID
	putRow(t, ctx, conn, targetID, entityID, map[string]any{
		"entityKey":         entityKey,
		"violating":         true,
		"missing_signature": true,
		"applicant":         applicant,
	})

	op := nextOp(t, ops, 15*time.Second)
	require.Equal(t, "CreateTask", op.OperationType)
	require.Equal(t, applicant, op.Payload["assignee"])
	require.Equal(t, "vtx.meta."+opVtx, op.Payload["forOperation"],
		"forOperation must resolve to the live op meta-vertex")
	require.Equal(t, entityKey, op.Payload["scopedTo"])
	require.Equal(t, entityKey, op.AuthContext.Target)
	taskID, _ := op.Payload["taskId"].(string)
	require.True(t, substrate.IsValidNanoID(taskID), "taskId must be a 20-char NanoID")
	require.NotEmpty(t, op.Payload["expiresAt"])
	require.NotZero(t, op.Payload["expectedRevision"])
}

// TestWeaverE2E_AntiStorm proves the §10.8 anti-storm OCC: re-upserting the
// SAME violating row (a fresh CDC delivery) finds the in-flight mark and
// fires no second op.
func TestWeaverE2E_AntiStorm(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	ops := subscribeOps(t, nc)

	patternVtx := mustNanoID(t)
	installLoomPattern(t, ctx, conn, patternVtx, "fixtureFlow")

	targetID := "fixtureStorm"
	installWeaverTarget(t, ctx, conn, mustNanoID(t), map[string]any{
		"targetId": targetID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_step": map[string]any{
				"action": "triggerLoom", "pattern": "fixtureFlow", "subject": "row.applicant",
			},
		},
	})

	engine := newEngine(conn, "e2e-storm-"+mustNanoID(t))
	engCtx, engCancel := context.WithCancel(ctx)
	defer engCancel()
	go func() { _ = engine.Start(engCtx) }()
	waitConsumer(t, ctx, conn, "weaver-target-"+targetID)

	entityID := mustNanoID(t)
	row := map[string]any{
		"entityKey":    "vtx.leaseApp." + entityID,
		"violating":    true,
		"missing_step": true,
		"applicant":    "vtx.identity." + mustNanoID(t),
	}
	putRow(t, ctx, conn, targetID, entityID, row)
	first := nextOp(t, ops, 15*time.Second)
	waitMark(t, ctx, conn, targetID+"."+entityID+".missing_step")

	// CDC re-delivery of the same violating state: the mark exists → no second op.
	putRow(t, ctx, conn, targetID, entityID, row)
	requireNoOp(t, ops, 2*time.Second)

	// And the first op was a real dispatch.
	require.Equal(t, "StartLoomPattern", first.OperationType)
}

// TestWeaverE2E_ReconcileTeardownAndReinstall proves AC 3's reconcile
// semantics: tombstoning the meta.weaverTarget Removes the consumer AND
// deletes its JetStream durable; a re-install brings up a fresh consumer that
// replays existing rows via DeliverLastPerSubject.
func TestWeaverE2E_ReconcileTeardownAndReinstall(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	ops := subscribeOps(t, nc)

	patternVtx := mustNanoID(t)
	installLoomPattern(t, ctx, conn, patternVtx, "fixtureFlow")

	targetID := "fixtureCycle"
	target := map[string]any{
		"targetId": targetID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_step": map[string]any{
				"action": "triggerLoom", "pattern": "fixtureFlow", "subject": "row.applicant",
			},
		},
	}
	targetVtx := mustNanoID(t)
	installWeaverTarget(t, ctx, conn, targetVtx, target)

	engine := newEngine(conn, "e2e-cycle-"+mustNanoID(t))
	engCtx, engCancel := context.WithCancel(ctx)
	defer engCancel()
	go func() { _ = engine.Start(engCtx) }()

	durable := "weaver-target-" + targetID
	waitConsumer(t, ctx, conn, durable)

	// Tombstone the registry vertex: the consumer is Removed and the JetStream
	// durable deleted (assert via consumer-info absence).
	tombstoneMetaVertex(t, ctx, conn, targetVtx)
	waitConsumerGone(t, ctx, conn, durable)

	// A violating row lands while the target is uninstalled.
	entityID := mustNanoID(t)
	putRow(t, ctx, conn, targetID, entityID, map[string]any{
		"entityKey":    "vtx.leaseApp." + entityID,
		"violating":    true,
		"missing_step": true,
		"applicant":    "vtx.identity." + mustNanoID(t),
	})

	// Re-install: a fresh consumer replays the row (DeliverLastPerSubject) and
	// dispatches.
	installWeaverTarget(t, ctx, conn, mustNanoID(t), target)
	waitConsumer(t, ctx, conn, durable)
	op := nextOp(t, ops, 15*time.Second)
	require.Equal(t, "StartLoomPattern", op.OperationType)
}

// TestWeaverE2E_InstallValidations proves the §10.8 install-time validations
// (AC 1) and the FR29 config-error alert path: a gaps key without the
// missing_ prefix rejects the target (no consumer); a duplicate targetId
// rejects the later registration; a true missing_* column with no playbook
// entry alerts via Health KV and dispatches nothing.
func TestWeaverE2E_InstallValidations(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	ops := subscribeOps(t, nc)

	// (a) gaps key without the missing_ prefix → rejected.
	installWeaverTarget(t, ctx, conn, mustNanoID(t), map[string]any{
		"targetId": "fixtureBadGaps",
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"signature_missing": map[string]any{"action": "directOp", "operation": "NoOp"},
		},
	})

	// (b) duplicate targetId → keep the first, reject the later.
	dupID := "fixtureDup"
	firstVtx := mustNanoID(t)
	installWeaverTarget(t, ctx, conn, firstVtx, map[string]any{
		"targetId": dupID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_a": map[string]any{"action": "directOp", "operation": "FixA"},
		},
	})

	// (c) a valid target whose row will carry an unmapped gap column.
	noPlaybookID := "fixtureNoPlaybook"
	installWeaverTarget(t, ctx, conn, mustNanoID(t), map[string]any{
		"targetId": noPlaybookID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_known": map[string]any{"action": "directOp", "operation": "FixKnown"},
		},
	})

	instance := "e2e-valid-" + mustNanoID(t)
	engine := newEngine(conn, instance, func(c *weaver.Config) { c.HeartbeatEvery = 200 * time.Millisecond })
	engCtx, engCancel := context.WithCancel(ctx)
	defer engCancel()
	go func() { _ = engine.Start(engCtx) }()

	waitConsumer(t, ctx, conn, "weaver-target-"+dupID)
	waitConsumer(t, ctx, conn, "weaver-target-"+noPlaybookID)
	require.False(t, consumerExists(t, ctx, conn, "weaver-target-fixtureBadGaps"),
		"a target with a non-missing_ gaps key must be rejected (no consumer)")

	// The duplicate arrives while the first is registered: rejected + alerted.
	dupVtx := mustNanoID(t)
	installWeaverTarget(t, ctx, conn, dupVtx, map[string]any{
		"targetId": dupID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_b": map[string]any{"action": "directOp", "operation": "FixB"},
		},
	})

	// (c) row with a true missing_* column that has no playbook entry.
	entityID := mustNanoID(t)
	putRow(t, ctx, conn, noPlaybookID, entityID, map[string]any{
		"entityKey":       "vtx.leaseApp." + entityID,
		"violating":       true,
		"missing_unknown": true,
	})

	// No dispatch for any of the three conditions.
	requireNoOp(t, ops, 2*time.Second)

	// The first dup registration still owns the consumer; the duplicate and the
	// unmapped gap surfaced as Contract #5 issues on the heartbeat doc.
	deadline := time.Now().Add(15 * time.Second)
	var issues []map[string]any
	for time.Now().Before(deadline) {
		entry, err := conn.KVGet(ctx, healthKVBucket, "health.weaver."+instance)
		if err == nil {
			var doc struct {
				Issues []map[string]any `json:"issues"`
			}
			if json.Unmarshal(entry.Value, &doc) == nil {
				issues = doc.Issues
				if hasIssue(issues, "TargetRejected") && hasIssue(issues, "GapWithoutPlaybook") {
					break
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	require.True(t, hasIssue(issues, "TargetRejected"),
		"rejected targets must surface a Health KV issue, got: %v", issues)
	require.True(t, hasIssue(issues, "GapWithoutPlaybook"),
		"a true gap with no playbook entry must surface a Health KV issue, got: %v", issues)
}

func hasIssue(issues []map[string]any, code string) bool {
	for _, i := range issues {
		if i["code"] == code {
			return true
		}
	}
	return false
}

// TestWeaverE2E_NudgeStub proves adjudicated decision 6: a nudge gap is
// recognised, routed to the loud stub (Health KV issue), and never silently
// dropped — and no mark or op is produced.
func TestWeaverE2E_NudgeStub(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := startNATS(t)
	conn, err := substrate.Wrap(nc)
	require.NoError(t, err)
	provision(t, ctx, conn)
	ops := subscribeOps(t, nc)

	targetID := "fixtureNudge"
	installWeaverTarget(t, ctx, conn, mustNanoID(t), map[string]any{
		"targetId": targetID,
		"lensRef":  mustNanoID(t),
		"gaps": map[string]any{
			"missing_check": map[string]any{
				"action": "nudge", "adapter": "fixtureAdapter", "subject": "row.applicant",
			},
		},
	})

	instance := "e2e-nudge-" + mustNanoID(t)
	engine := newEngine(conn, instance, func(c *weaver.Config) { c.HeartbeatEvery = 200 * time.Millisecond })
	engCtx, engCancel := context.WithCancel(ctx)
	defer engCancel()
	go func() { _ = engine.Start(engCtx) }()
	waitConsumer(t, ctx, conn, "weaver-target-"+targetID)

	entityID := mustNanoID(t)
	putRow(t, ctx, conn, targetID, entityID, map[string]any{
		"entityKey":     "vtx.leaseApp." + entityID,
		"violating":     true,
		"missing_check": true,
		"applicant":     "vtx.identity." + mustNanoID(t),
	})

	requireNoOp(t, ops, 2*time.Second)
	_, err = conn.KVGet(ctx, weaverStateBucket, targetID+"."+entityID+".missing_check")
	require.ErrorIs(t, err, substrate.ErrKeyNotFound, "a nudge stub must not claim a mark")

	deadline := time.Now().Add(15 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		entry, err := conn.KVGet(ctx, healthKVBucket, "health.weaver."+instance)
		if err == nil {
			var doc struct {
				Issues []map[string]any `json:"issues"`
			}
			if json.Unmarshal(entry.Value, &doc) == nil && hasIssue(doc.Issues, "PlaybookConfigError") {
				found = true
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	require.True(t, found, "the nudge stub must surface 'not yet implemented' as a Health KV issue")
}
