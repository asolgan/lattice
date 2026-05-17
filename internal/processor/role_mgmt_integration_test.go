// Story 3.6 — Role-Scoped Access Domain & Audit integration tests.
//
// Tests validate that the role/permission DDL meta-vertices, Starlark
// scripts, and per-op permission grants to the operator role together
// produce correct end-to-end behaviour through the 10-step Processor
// pipeline.
//
// Test actors:
//   - operatorActorID: a test identity whose capability KV entry
//     carries all 12 role-management platformPermissions (simulating an
//     operator with the operator role's grants).
//   - consumerActorID: a test identity whose capability KV entry has no
//     role-management platformPermissions (used for the unauthorized test).
//
// All tests use the shadow-key (non-NanoID) DDL form so they can seed
// scripts inline without wiring a full bootstrap; the DDL cache
// shadow-key fallback in step4_hydrate.go supports this path for tests.
package processor

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/substrate"
)

// Test NanoIDs for role-management tests. These are distinct from
// testNanoID1/2 declared in the main suite to avoid cross-test pollution.
// NanoIDs must be exactly 20 chars from substrate.Alphabet:
// ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789
// Excluded: I (capital i), O (capital o), l (lowercase L), 0 (zero).
const (
	// 20-char valid NanoIDs using only safe characters.
	rmOperatorActorID  = "RmPActrXzBbCdEf12345" // 20 chars
	rmConsumerActorID  = "RmCnsrXzBbCdEf123456" // 20 chars
	rmOperatorActorKey = "vtx.identity." + rmOperatorActorID
	rmConsumerActorKey = "vtx.identity." + rmConsumerActorID
	rmOperatorCapKey   = "cap.identity." + rmOperatorActorID
	rmConsumerCapKey   = "cap.identity." + rmConsumerActorID

	// Test role vertex ID that the operator actor would be assigned to.
	rmTargetRoleID  = "RmTrgtRReXzBbCdEfGhi" // 20 chars
	rmTargetRoleKey = "vtx.role." + rmTargetRoleID

	rmCapBucket  = "capability-kv"
	rmTestStream = "core-operations"
	rmDurable    = "rm-proc-main"

	// Valid request IDs for role-mgmt test ops (20 chars, substrate.Alphabet).
	rmReqCreateRole  = "RmCrRq12345678912ABx"
	rmReqAssignRole  = "RmAsRq12345678912CDx"
	rmReqAssign2Role = "RmAs2q12345678912EFx"
	rmReqRevokeRole  = "RmRvRq12345678912GHx"
	rmReqUnauth      = "RmUnRq12345678912JKx"
)

// operatorCapDoc builds a CapabilityDoc that grants all 12 role-management
// operation types (simulating the operator role's grants being projected into
// Capability KV by the Capability Lens).
func operatorCapDoc() *CapabilityDoc {
	perms := []PlatformPermission{
		{OperationType: "CreateRole", Scope: "any"},
		{OperationType: "UpdateRole", Scope: "any"},
		{OperationType: "TombstoneRole", Scope: "any"},
		{OperationType: "CreatePermission", Scope: "any"},
		{OperationType: "UpdatePermission", Scope: "any"},
		{OperationType: "TombstonePermission", Scope: "any"},
		{OperationType: "AssignRole", Scope: "any"},
		{OperationType: "RevokeRole", Scope: "any"},
		{OperationType: "GrantPermission", Scope: "any"},
		{OperationType: "RevokePermission", Scope: "any"},
		{OperationType: "AssignReportingChain", Scope: "any"},
		{OperationType: "RemoveReportingChain", Scope: "any"},
	}
	now := time.Now().UTC()
	return &CapabilityDoc{
		Key:                    rmOperatorCapKey,
		Actor:                  rmOperatorActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{rmOperatorActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions:    perms,
		ServiceAccess:          []ServiceAccessEntry{},
		EphemeralGrants:        []EphemeralGrant{},
		Roles:                  []string{"vtx.role.operator"},
	}
}

// consumerCapDoc builds a CapabilityDoc with no role-management permissions.
func consumerCapDoc() *CapabilityDoc {
	now := time.Now().UTC()
	return &CapabilityDoc{
		Key:                    rmConsumerCapKey,
		Actor:                  rmConsumerActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{rmConsumerActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions:    []PlatformPermission{}, // no role-mgmt perms
		ServiceAccess:          []ServiceAccessEntry{},
		EphemeralGrants:        []EphemeralGrant{},
		Roles:                  []string{"vtx.role.consumer"},
	}
}

// provisionRMHarness sets up: Core KV, Health KV, Capability KV, and the
// core-operations stream. Seeds operator + consumer capability docs.
func provisionRMHarness(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()
	js := conn.JetStream()

	// Core KV + Health KV.
	for _, bucket := range []string{testCoreBucket, testHealthBucket} {
		_, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:         bucket,
			LimitMarkerTTL: time.Second,
		})
		if err != nil {
			t.Fatalf("create KV %q: %v", bucket, err)
		}
	}
	// AllowAtomicPublish on Core KV.
	streamName := "KV_" + testCoreBucket
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		t.Fatalf("get stream %q: %v", streamName, err)
	}
	cfg := stream.CachedInfo().Config
	cfg.AllowAtomicPublish = true
	if _, err := js.UpdateStream(ctx, cfg); err != nil {
		t.Fatalf("enable AllowAtomicPublish: %v", err)
	}

	// Capability KV (for CapabilityAuthorizer reads).
	capKV, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:         rmCapBucket,
		LimitMarkerTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("create capability-kv: %v", err)
	}

	// Seed operator capability doc.
	opDoc, _ := json.Marshal(operatorCapDoc())
	if _, err := capKV.Put(ctx, rmOperatorCapKey, opDoc); err != nil {
		t.Fatalf("seed operator cap doc: %v", err)
	}
	// Seed consumer capability doc.
	conDoc, _ := json.Marshal(consumerCapDoc())
	if _, err := capKV.Put(ctx, rmConsumerCapKey, conDoc); err != nil {
		t.Fatalf("seed consumer cap doc: %v", err)
	}

	// core-operations stream.
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     rmTestStream,
		Subjects: []string{"ops.>"},
	})
	if err != nil {
		t.Fatalf("create core-operations stream: %v", err)
	}
}

// seedRoleDDL writes the role DDL meta-vertex + script to Core KV using the
// shadow-key form so tests work without a full bootstrap. The script is the
// same one from role_mgmt_ddl.go embedded as a Go constant.
func seedRoleDDL(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()
	const script = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key, "document": {"class": cls, "isDeleted": False, "data": data}}
def make_aspect(key, cls, data):
    return {"op": "create", "key": key, "document": {"class": cls, "isDeleted": False, "data": data}}
def make_tombstone(key):
    return {"op": "tombstone", "key": key, "document": {"isDeleted": True, "data": {}}}
def execute(state, op):
    ot = op.operationType
    p = op.payload
    if ot == "CreateRole":
        role_id = nanoid.new()
        role_key = "vtx.role." + role_id
        desc = p.description if hasattr(p, "description") else ""
        mutations = [make_vtx(role_key, "role", {"name": p.name}), make_aspect(role_key + ".description", "description", {"text": desc})]
        return {"mutations": mutations, "events": [{"class": "RoleCreated", "data": {"roleKey": role_key, "name": p.name}}]}
    if ot == "AssignRole":
        id_key = p.identityKey
        r_key = p.roleKey
        parts_i = id_key.split(".")
        parts_r = r_key.split(".")
        id_id = parts_i[2] if len(parts_i) >= 3 else ""
        r_id = parts_r[2] if len(parts_r) >= 3 else ""
        lnk_key = "lnk.identity." + id_id + ".holdsRole.role." + r_id
        doc = {"class": "holdsRole", "isDeleted": False, "youngerVertex": id_key, "olderVertex": r_key, "localName": "holdsRole", "data": {}}
        return {"mutations": [{"op": "create", "key": lnk_key, "document": doc}], "events": [{"class": "RoleAssigned", "data": {"identityKey": id_key, "roleKey": r_key}}]}
    if ot == "RevokeRole":
        id_key = p.identityKey
        r_key = p.roleKey
        parts_i = id_key.split(".")
        parts_r = r_key.split(".")
        id_id = parts_i[2] if len(parts_i) >= 3 else ""
        r_id = parts_r[2] if len(parts_r) >= 3 else ""
        lnk_key = "lnk.identity." + id_id + ".holdsRole.role." + r_id
        return {"mutations": [{"op": "tombstone", "key": lnk_key, "document": {"isDeleted": True, "data": {}}}], "events": [{"class": "RoleRevoked", "data": {"identityKey": id_key, "roleKey": r_key}}]}
    fail("role DDL: unknown operationType: " + ot)
`
	ddlDoc := map[string]any{
		"class":     "meta.ddl.vertexType",
		"isDeleted": false,
		"data": map[string]any{
			"canonicalName":     "role",
			"permittedCommands": []string{"CreateRole", "UpdateRole", "TombstoneRole", "AssignRole", "RevokeRole"},
		},
	}
	ddlBytes, _ := json.Marshal(ddlDoc)
	scriptDoc := map[string]any{
		"class":     "meta.script",
		"isDeleted": false,
		"data": map[string]any{
			"source": script,
		},
	}
	scriptBytes, _ := json.Marshal(scriptDoc)

	ddlKey := "vtx.meta.role"
	if _, err := conn.KVPut(ctx, testCoreBucket, ddlKey, ddlBytes); err != nil {
		t.Fatalf("seed role DDL: %v", err)
	}
	if _, err := conn.KVPut(ctx, testCoreBucket, ddlKey+".script", scriptBytes); err != nil {
		t.Fatalf("seed role DDL script: %v", err)
	}
}

// newCapabilityPipeline builds a CommitPath wired with the real
// CapabilityAuthorizer reading from the in-process Capability KV bucket.
func newCapabilityPipeline(
	t *testing.T, ctx context.Context, conn *substrate.Conn, durable string,
) (*CommitPath, jetstream.Consumer) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	metrics := &Metrics{}
	hb := NewHealthHeartbeater(conn, testHealthBucket, "rm-proc-"+durable, 10*time.Second, metrics, logger)
	cache := NewDDLCache(conn, testCoreBucket, logger)
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("ddl cache refresh: %v", err)
	}

	authz, err := SelectAuthorizerArgs(SelectAuthorizerOpts{
		Mode:             AuthModeCapability,
		Reader:           conn,
		CapabilityBucket: rmCapBucket,
		Logger:           logger,
	})
	if err != nil {
		t.Fatalf("SelectAuthorizerArgs: %v", err)
	}

	committer := NewCommitter(conn, testCoreBucket, cache, logger, time.Now)
	cp := NewCommitPath(Deps{
		Conn:        conn,
		CoreBucket:  testCoreBucket,
		HealthKV:    testHealthBucket,
		Authorizer:  authz,
		Hydrator:    NewHydratorWithCache(conn, testCoreBucket, cache, logger),
		Executor:    NewExecutor(NewStarlarkRunner(0, 0), logger),
		Validator:   NewValidator(cache, logger),
		Committer:   committer,
		Events:      &StubEventPublisher{logger: logger},
		Metrics:     metrics,
		Heartbeater: hb,
		Logger:      logger,
	})

	cons, err := EnsureConsumer(ctx, conn.JetStream(), ConsumerConfig{
		StreamName:     rmTestStream,
		Durable:        durable,
		FilterSubjects: []string{"ops.default"},
		AckWait:        5 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("EnsureConsumer: %v", err)
	}
	return cp, cons
}

func publishRMOp(t *testing.T, conn *substrate.Conn, env *OperationEnvelope) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	_, err = conn.JetStream().Publish(context.Background(), "ops.default", b)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
}

// setupRMTestEnv returns a ready test env: NATS + harness + DDL seeded.
func setupRMTestEnv(t *testing.T) (context.Context, *substrate.Conn) {
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	conn, err := substrate.Connect(ctx, substrate.ConnectOpts{URL: url, Name: "rm-test"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(conn.Close)
	provisionRMHarness(t, ctx, conn)
	seedRoleDDL(t, ctx, conn)
	return ctx, conn
}

// TestRoleMgmt_CreateRole submits CreateRole as the operator actor,
// asserts step-8 commit, vtx.role.<NanoID> + .description aspect, and
// RoleCreated event in the tracker.
func TestRoleMgmt_CreateRole(t *testing.T) {
	ctx, conn := setupRMTestEnv(t)
	cp, cons := newCapabilityPipeline(t, ctx, conn, rmDurable+"-create")

	env := &OperationEnvelope{
		RequestID:     rmReqCreateRole,
		Lane:          LaneDefault,
		OperationType: "CreateRole",
		Actor:         rmOperatorActorKey,
		SubmittedAt:   "2026-05-16T10:00:00Z",
		Class:         "role",
		Payload:       json.RawMessage(`{"name":"TestRole","description":"A test role for integration testing"}`),
	}
	publishRMOp(t, conn, env)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Tracker must exist.
	te, err := conn.KVGet(ctx, testCoreBucket, TrackerKey(env.RequestID))
	if err != nil {
		t.Fatalf("tracker not found: %v", err)
	}
	tr, err := ParseTracker(te.Value)
	if err != nil {
		t.Fatalf("ParseTracker: %v", err)
	}

	// mutationKeys should include the role vertex + description aspect.
	mks, _ := tr.Data["mutationKeys"].([]interface{})
	if len(mks) < 2 {
		t.Fatalf("expected >= 2 mutationKeys (vtx.role.* + .description), got %v", tr.Data["mutationKeys"])
	}
	// At least one key must start with "vtx.role."
	foundRole := false
	var roleKey string
	for _, mk := range mks {
		if s, ok := mk.(string); ok && strings.HasPrefix(s, "vtx.role.") && !strings.Contains(s, ".description") {
			foundRole = true
			roleKey = s
		}
	}
	if !foundRole {
		t.Fatalf("no vtx.role.* key in mutationKeys: %v", mks)
	}

	// The role vertex must exist in Core KV.
	roleEntry, err := conn.KVGet(ctx, testCoreBucket, roleKey)
	if err != nil {
		t.Fatalf("role vertex not found at %s: %v", roleKey, err)
	}
	var roleDoc map[string]any
	if err := json.Unmarshal(roleEntry.Value, &roleDoc); err != nil {
		t.Fatalf("unmarshal role vertex: %v", err)
	}
	if roleDoc["class"] != "role" {
		t.Fatalf("role vertex class = %v, want role", roleDoc["class"])
	}

	// RoleCreated event must be in tracker eventClasses.
	ecs, _ := tr.Data["eventClasses"].([]interface{})
	if len(ecs) == 0 {
		t.Fatalf("expected eventClasses in tracker, got none")
	}
	foundEvent := false
	for _, ec := range ecs {
		if ec == "RoleCreated" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatalf("RoleCreated event not in tracker eventClasses: %v", ecs)
	}
}

// TestRoleMgmt_AssignRole submits AssignRole; asserts the holdsRole link
// vtx.identity.<X>.holdsRole.role.<Y> is written to Core KV.
func TestRoleMgmt_AssignRole(t *testing.T) {
	ctx, conn := setupRMTestEnv(t)
	cp, cons := newCapabilityPipeline(t, ctx, conn, rmDurable+"-assign")

	// We assign the operator actor to a notional target role.
	env := &OperationEnvelope{
		RequestID:     rmReqAssignRole,
		Lane:          LaneDefault,
		OperationType: "AssignRole",
		Actor:         rmOperatorActorKey,
		SubmittedAt:   "2026-05-16T10:01:00Z",
		Class:         "role",
		Payload: json.RawMessage(`{"identityKey":"` + rmOperatorActorKey +
			`","roleKey":"` + rmTargetRoleKey + `"}`),
	}
	publishRMOp(t, conn, env)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// holdsRole link must exist.
	expectedLnk := "lnk.identity." + rmOperatorActorID + ".holdsRole.role." + rmTargetRoleID
	entry, err := conn.KVGet(ctx, testCoreBucket, expectedLnk)
	if err != nil {
		t.Fatalf("holdsRole link not found at %s: %v", expectedLnk, err)
	}
	var lnkDoc map[string]any
	if err := json.Unmarshal(entry.Value, &lnkDoc); err != nil {
		t.Fatalf("unmarshal link: %v", err)
	}
	if lnkDoc["class"] != "holdsRole" {
		t.Fatalf("link class = %v, want holdsRole", lnkDoc["class"])
	}
	if isDeleted, _ := lnkDoc["isDeleted"].(bool); isDeleted {
		t.Fatalf("holdsRole link should not be deleted")
	}
}

// TestRoleMgmt_RevokeRole submits RevokeRole after AssignRole;
// asserts isDeleted=true on the link.
func TestRoleMgmt_RevokeRole(t *testing.T) {
	ctx, conn := setupRMTestEnv(t)
	cp, cons := newCapabilityPipeline(t, ctx, conn, rmDurable+"-revoke")

	// First assign.
	assignEnv := &OperationEnvelope{
		RequestID:     rmReqAssign2Role,
		Lane:          LaneDefault,
		OperationType: "AssignRole",
		Actor:         rmOperatorActorKey,
		SubmittedAt:   "2026-05-16T10:02:00Z",
		Class:         "role",
		Payload: json.RawMessage(`{"identityKey":"` + rmOperatorActorKey +
			`","roleKey":"` + rmTargetRoleKey + `"}`),
	}
	publishRMOp(t, conn, assignEnv)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Then revoke.
	revokeEnv := &OperationEnvelope{
		RequestID:     rmReqRevokeRole,
		Lane:          LaneDefault,
		OperationType: "RevokeRole",
		Actor:         rmOperatorActorKey,
		SubmittedAt:   "2026-05-16T10:03:00Z",
		Class:         "role",
		Payload: json.RawMessage(`{"identityKey":"` + rmOperatorActorKey +
			`","roleKey":"` + rmTargetRoleKey + `"}`),
	}
	publishRMOp(t, conn, revokeEnv)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// holdsRole link should now be tombstoned (isDeleted=true).
	expectedLnk := "lnk.identity." + rmOperatorActorID + ".holdsRole.role." + rmTargetRoleID
	entry, err := conn.KVGet(ctx, testCoreBucket, expectedLnk)
	if err != nil {
		t.Fatalf("holdsRole link not found after revoke: %v", err)
	}
	var lnkDoc map[string]any
	if err := json.Unmarshal(entry.Value, &lnkDoc); err != nil {
		t.Fatalf("unmarshal link: %v", err)
	}
	if isDeleted, _ := lnkDoc["isDeleted"].(bool); !isDeleted {
		t.Fatalf("holdsRole link should be tombstoned after RevokeRole; got isDeleted=%v", isDeleted)
	}
}

// TestRoleMgmt_UnauthorizedDenied submits CreateRole as the consumer actor,
// which has no role-management platformPermissions. Expects OutcomeRejected.
func TestRoleMgmt_UnauthorizedDenied(t *testing.T) {
	ctx, conn := setupRMTestEnv(t)
	cp, cons := newCapabilityPipeline(t, ctx, conn, rmDurable+"-unauth")

	env := &OperationEnvelope{
		RequestID:     rmReqUnauth,
		Lane:          LaneDefault,
		OperationType: "CreateRole",
		Actor:         rmConsumerActorKey, // consumer — no role-mgmt perms
		SubmittedAt:   "2026-05-16T10:04:00Z",
		Class:         "role",
		Payload:       json.RawMessage(`{"name":"NotAllowed","description":"consumer should not be able to create roles"}`),
	}
	publishRMOp(t, conn, env)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	// No vtx.role.* key should have been written.
	keys, _ := conn.KVListKeys(ctx, testCoreBucket)
	for _, k := range keys {
		if strings.HasPrefix(k, "vtx.role.") && k != rmTargetRoleKey {
			t.Fatalf("unexpected role vertex committed despite auth denial: %s", k)
		}
	}
}

// TestRoleMgmt_AuditViaCapKV validates that the capability KV entry for
// the operator actor contains the seeded role-management permissions in its
// platformPermissions slice. This is the "audit" assertion: the operator's
// permissions are correctly projected into Capability KV.
func TestRoleMgmt_AuditViaCapKV(t *testing.T) {
	ctx, conn := setupRMTestEnv(t)

	// The capability KV entry was seeded in provisionRMHarness.
	// Read it directly and validate it contains all 12 role-mgmt permissions.
	js := conn.JetStream()
	capKV, err := js.KeyValue(ctx, rmCapBucket)
	if err != nil {
		t.Fatalf("open capability-kv: %v", err)
	}

	entry, err := capKV.Get(ctx, rmOperatorCapKey)
	if err != nil {
		t.Fatalf("get operator cap entry: %v", err)
	}

	var doc CapabilityDoc
	if err := json.Unmarshal(entry.Value(), &doc); err != nil {
		t.Fatalf("unmarshal cap doc: %v", err)
	}

	expectedOps := []string{
		"CreateRole", "UpdateRole", "TombstoneRole",
		"CreatePermission", "UpdatePermission", "TombstonePermission",
		"AssignRole", "RevokeRole",
		"GrantPermission", "RevokePermission",
		"AssignReportingChain", "RemoveReportingChain",
	}

	permMap := make(map[string]string)
	for _, p := range doc.PlatformPermissions {
		permMap[p.OperationType] = p.Scope
	}

	for _, op := range expectedOps {
		scope, ok := permMap[op]
		if !ok {
			t.Errorf("platformPermissions missing operationType %q", op)
			continue
		}
		if scope != "any" {
			t.Errorf("platformPermissions[%q].scope = %q, want any", op, scope)
		}
	}
}
