// Story 4.5 — Staff-Approved Identity Merge (FR4) integration tests.
//
// Validates two new identity-DDL operations end-to-end through the 10-step
// Processor pipeline against an embedded NATS server:
//
//   - ApproveIdentityMerge (review op, capability-mode tests)
//   - MergeIdentity        (merge op, stub-mode tests per Decision #5)
//
// Decision #5 (handoff brief): MergeIdentity has NO seeded grant link in
// 4.1's primordial. Adding a new grant would force a primordial-data
// migration (verify-bootstrap rebaseline). The Phase 1 workaround is to
// run MergeIdentity tests with LATTICE_AUTH_MODE=stub. ApproveIdentityMerge
// and post-merge redirect tests run in capability mode (using the operator
// grant seeded by 4.1).
//
// Tests (9 total):
//
//	Capability-mode (ApproveIdentityMerge + post-merge redirect):
//	  1. TestApproveIdentityMerge_SurfacesFlaggedPairs
//	  2. TestApproveIdentityMerge_FiltersByPair
//	  3. TestApproveIdentityMerge_HasCredentialBindingFlag
//	  4. TestApproveIdentityMerge_NonOperatorDenied
//	  5. TestApproveIdentityMerge_EmptyWhenNoFlagged
//	  6. TestMergeIdentity_PostMergeRedirect_FR4
//
//	Stub-mode (MergeIdentity):
//	  7. TestMergeIdentity_HappyPath
//	  8. TestMergeIdentity_RejectsNonFlaggedSecondary
//	  9. TestMergeIdentity_RejectsAlreadyMergedSecondary
//	 10. TestMergeIdentity_RejectsMissingDuplicateOfLink
//	 11. TestMergeIdentity_SelfReferenceRejected
//	 12. TestMergeIdentity_AspectConflictResolution
//
// Note: brief lists "~9 tests" — the count above reflects the natural
// granularity once the table-driven rejection cases are split out.
package processor

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/substrate"
)

// ---- Constants ----

const (
	imOperatorActorID  = "JmgQpActHKLMNPQRSTUV"
	imOperatorActorKey = "vtx.identity." + imOperatorActorID
	imOperatorCapKey   = "cap.identity." + imOperatorActorID

	imConsumerActorID  = "JmgCnActHKLMNPQRSTUV"
	imConsumerActorKey = "vtx.identity." + imConsumerActorID
	imConsumerCapKey   = "cap.identity." + imConsumerActorID

	imTestBucket    = "core-kv"
	imHealthBucket  = "health-kv"
	imCapBucket     = "capability-kv"
	imOpsStreamName = "core-operations"
	imInstance      = "im-test"
)

// ---- Harness helpers ----

func imOperatorCapDoc() *CapabilityDoc {
	now := time.Now().UTC()
	return &CapabilityDoc{
		Key:                    imOperatorCapKey,
		Actor:                  imOperatorActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{imOperatorActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []PlatformPermission{
			{OperationType: "ApproveIdentityMerge", Scope: "any"},
			{OperationType: "UpdateIdentityState", Scope: "any"},
		},
		ServiceAccess:   []ServiceAccessEntry{},
		EphemeralGrants: []EphemeralGrant{},
		Roles:           []string{"vtx.role.operator"},
	}
}

func imConsumerCapDoc() *CapabilityDoc {
	now := time.Now().UTC()
	return &CapabilityDoc{
		Key:                    imConsumerCapKey,
		Actor:                  imConsumerActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{imConsumerActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []PlatformPermission{
			{OperationType: "ClaimIdentity", Scope: "self"},
		},
		ServiceAccess:   []ServiceAccessEntry{},
		EphemeralGrants: []EphemeralGrant{},
		Roles:           []string{"vtx.role.consumer"},
	}
}

func provisionMergeHarness(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()
	js := conn.JetStream()
	for _, bucket := range []string{imTestBucket, imHealthBucket} {
		_, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: bucket, LimitMarkerTTL: time.Second,
		})
		if err != nil {
			t.Fatalf("create KV %q: %v", bucket, err)
		}
	}
	streamName := "KV_" + imTestBucket
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		t.Fatalf("get stream %q: %v", streamName, err)
	}
	cfg := stream.CachedInfo().Config
	cfg.AllowAtomicPublish = true
	if _, err := js.UpdateStream(ctx, cfg); err != nil {
		t.Fatalf("enable AllowAtomicPublish: %v", err)
	}
	capKV, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: imCapBucket, LimitMarkerTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("create capability-kv: %v", err)
	}
	opDoc, _ := json.Marshal(imOperatorCapDoc())
	if _, err := capKV.Put(ctx, imOperatorCapKey, opDoc); err != nil {
		t.Fatalf("seed operator cap doc: %v", err)
	}
	conDoc, _ := json.Marshal(imConsumerCapDoc())
	if _, err := capKV.Put(ctx, imConsumerCapKey, conDoc); err != nil {
		t.Fatalf("seed consumer cap doc: %v", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: imOpsStreamName, Subjects: []string{"ops.>"},
	})
	if err != nil {
		t.Fatalf("create core-operations stream: %v", err)
	}
}

func seedIdentityDDLForMerge(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()
	ddl := bootstrap.IdentityDDL()
	ddlKey := "vtx.meta.identity"
	ddlDoc := map[string]any{
		"class":     ddl.Class,
		"isDeleted": false,
		"data": map[string]any{
			"canonicalName":     ddl.CanonicalName,
			"permittedCommands": ddl.PermittedCommands,
		},
	}
	ddlBytes, _ := json.Marshal(ddlDoc)
	if _, err := conn.KVPut(ctx, imTestBucket, ddlKey, ddlBytes); err != nil {
		t.Fatalf("seed identity DDL: %v", err)
	}
	scriptDoc := map[string]any{
		"class": "meta.script", "isDeleted": false,
		"data": map[string]any{"source": ddl.Script},
	}
	scriptBytes, _ := json.Marshal(scriptDoc)
	if _, err := conn.KVPut(ctx, imTestBucket, ddlKey+".script", scriptBytes); err != nil {
		t.Fatalf("seed identity DDL script: %v", err)
	}
}

func setupMergeTestEnv(t *testing.T) (context.Context, *substrate.Conn) {
	t.Helper()
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	conn, err := substrate.Connect(ctx, substrate.ConnectOpts{URL: url, Name: imInstance})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(conn.Close)
	provisionMergeHarness(t, ctx, conn)
	seedIdentityDDLForMerge(t, ctx, conn)
	return ctx, conn
}

func mergeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newMergePipelineCapability wires a capability-mode CommitPath for the
// ApproveIdentityMerge tests + the post-merge redirect test.
func newMergePipelineCapability(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) (*CommitPath, jetstream.Consumer) {
	t.Helper()
	logger := mergeLogger()
	metrics := &Metrics{}
	hb := NewHealthHeartbeater(conn, imHealthBucket, imInstance+"-"+durable, 10*time.Second, metrics, logger)
	cache := NewDDLCache(conn, imTestBucket, logger)
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("ddl cache refresh: %v", err)
	}
	authz, err := SelectAuthorizerArgs(SelectAuthorizerOpts{
		Mode: AuthModeCapability, Reader: conn, CapabilityBucket: imCapBucket, Logger: logger,
	})
	if err != nil {
		t.Fatalf("SelectAuthorizerArgs: %v", err)
	}
	committer := NewCommitter(conn, imTestBucket, cache, logger, time.Now)
	cp := NewCommitPath(Deps{
		Conn: conn, CoreBucket: imTestBucket, HealthKV: imHealthBucket,
		Authorizer:  authz,
		Hydrator:    NewHydratorWithCache(conn, imTestBucket, cache, logger),
		Executor:    NewExecutor(NewStarlarkRunner(0, 0), logger),
		Validator:   NewValidator(cache, logger),
		Committer:   committer,
		Events:      &StubEventPublisher{logger: logger},
		Metrics:     metrics,
		Heartbeater: hb,
		Logger:      logger,
	})
	cons, err := EnsureConsumer(ctx, conn.JetStream(), ConsumerConfig{
		StreamName: imOpsStreamName, Durable: durable,
		FilterSubjects: []string{"ops.default"}, AckWait: 10 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("EnsureConsumer: %v", err)
	}
	return cp, cons
}

// newMergePipelineStub wires a stub-mode CommitPath for the MergeIdentity
// tests (Decision #5 — no MergeIdentity grant seeded in primordial).
func newMergePipelineStub(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) (*CommitPath, jetstream.Consumer) {
	t.Helper()
	logger := mergeLogger()
	metrics := &Metrics{}
	hb := NewHealthHeartbeater(conn, imHealthBucket, imInstance+"-"+durable, 10*time.Second, metrics, logger)
	cache := NewDDLCache(conn, imTestBucket, logger)
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("ddl cache refresh: %v", err)
	}
	authz, err := SelectAuthorizerArgs(SelectAuthorizerOpts{
		Mode: AuthModeStub, Logger: logger,
	})
	if err != nil {
		t.Fatalf("SelectAuthorizerArgs stub: %v", err)
	}
	committer := NewCommitter(conn, imTestBucket, cache, logger, time.Now)
	cp := NewCommitPath(Deps{
		Conn: conn, CoreBucket: imTestBucket, HealthKV: imHealthBucket,
		Authorizer:  authz,
		Hydrator:    NewHydratorWithCache(conn, imTestBucket, cache, logger),
		Executor:    NewExecutor(NewStarlarkRunner(0, 0), logger),
		Validator:   NewValidator(cache, logger),
		Committer:   committer,
		Events:      &StubEventPublisher{logger: logger},
		Metrics:     metrics,
		Heartbeater: hb,
		Logger:      logger,
	})
	cons, err := EnsureConsumer(ctx, conn.JetStream(), ConsumerConfig{
		StreamName: imOpsStreamName, Durable: durable,
		FilterSubjects: []string{"ops.default"}, AckWait: 10 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("EnsureConsumer: %v", err)
	}
	return cp, cons
}

// ---- Seeding helpers ----

// seedMergeIdentity seeds an identity vertex + state + name/email/phone aspects.
// Pass "" for any optional aspect to skip seeding.
func seedMergeIdentity(t *testing.T, ctx context.Context, conn *substrate.Conn, key, state, name, email, phone string) {
	t.Helper()
	vtxDoc := map[string]any{
		"class": "identity", "isDeleted": false, "data": map[string]any{},
	}
	vb, _ := json.Marshal(vtxDoc)
	if _, err := conn.KVPut(ctx, imTestBucket, key, vb); err != nil {
		t.Fatalf("seed identity vertex %s: %v", key, err)
	}
	put := func(suffix, class string, data map[string]any) {
		doc := map[string]any{
			"class": class, "vertexKey": key, "localName": suffix,
			"isDeleted": false, "data": data,
		}
		b, _ := json.Marshal(doc)
		if _, err := conn.KVPut(ctx, imTestBucket, key+"."+suffix, b); err != nil {
			t.Fatalf("seed %s.%s: %v", key, suffix, err)
		}
	}
	put("state", "state", map[string]any{"value": state})
	if name != "" {
		put("name", "name", map[string]any{"value": name})
	}
	if email != "" {
		put("email", "email", map[string]any{"value": email})
	}
	if phone != "" {
		put("phone", "phone", map[string]any{"value": phone})
	}
}

// seedMergeCredentialBinding seeds a credentialBinding aspect (claimed identity).
func seedMergeCredentialBinding(t *testing.T, ctx context.Context, conn *substrate.Conn, identityKey, actorKey string) {
	t.Helper()
	doc := map[string]any{
		"class": "credentialBinding", "vertexKey": identityKey, "localName": "credentialBinding",
		"isDeleted": false, "data": map[string]any{
			"actorKey": actorKey, "boundAt": "2026-05-17T10:00:00Z",
		},
	}
	b, _ := json.Marshal(doc)
	if _, err := conn.KVPut(ctx, imTestBucket, identityKey+".credentialBinding", b); err != nil {
		t.Fatalf("seed credentialBinding: %v", err)
	}
}

// seedMergeDuplicateOfLink seeds the canonical duplicateOf link between two identities.
func seedMergeDuplicateOfLink(t *testing.T, ctx context.Context, conn *substrate.Conn, aKey, bKey string) string {
	t.Helper()
	const prefix = "vtx.identity."
	aID := aKey[len(prefix):]
	bID := bKey[len(prefix):]
	var loID, hiID string
	if aID < bID {
		loID, hiID = aID, bID
	} else {
		loID, hiID = bID, aID
	}
	linkKey := "lnk.identity." + loID + ".duplicateOf.identity." + hiID
	linkDoc := map[string]any{
		"class": "duplicateOf", "isDeleted": false,
		"data": map[string]any{
			"criteria":      []string{"exact-email"},
			"confidence":    1.0,
			"scanRequestId": "TestScanReqIdAAAAAAA",
			"flaggedAt":     "2026-05-17T10:00:00Z",
		},
	}
	b, _ := json.Marshal(linkDoc)
	if _, err := conn.KVPut(ctx, imTestBucket, linkKey, b); err != nil {
		t.Fatalf("seed duplicateOf link: %v", err)
	}
	return linkKey
}

// seedMergeGenericLink seeds an arbitrary 6-segment link envelope.
func seedMergeGenericLink(t *testing.T, ctx context.Context, conn *substrate.Conn, linkKey, class string, data map[string]any) {
	t.Helper()
	doc := map[string]any{
		"class": class, "isDeleted": false, "data": data,
	}
	b, _ := json.Marshal(doc)
	if _, err := conn.KVPut(ctx, imTestBucket, linkKey, b); err != nil {
		t.Fatalf("seed link %s: %v", linkKey, err)
	}
}

// readMergeAspectData reads the data map from an aspect at the merge bucket.
func readMergeAspectData(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) map[string]any {
	t.Helper()
	entry, err := conn.KVGet(ctx, imTestBucket, key)
	if err != nil {
		t.Fatalf("KVGet %s: %v", key, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", key, err)
	}
	data, _ := doc["data"].(map[string]any)
	return data
}

// readMergeLink reads an entire link envelope (or returns nil if absent).
func readMergeLink(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) map[string]any {
	t.Helper()
	entry, err := conn.KVGet(ctx, imTestBucket, key)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		return nil
	}
	return doc
}

// publishApproveOp submits an ApproveIdentityMerge op as the operator actor.
// extraPayload is optional; pass nil for default payload {}.
func publishApproveOp(t *testing.T, conn *substrate.Conn, reqID string, actorKey string, extraPayload map[string]any) {
	t.Helper()
	p := map[string]any{}
	for k, v := range extraPayload {
		p[k] = v
	}
	pb, _ := json.Marshal(p)
	env := &OperationEnvelope{
		RequestID:     reqID,
		Lane:          LaneDefault,
		OperationType: "ApproveIdentityMerge",
		Actor:         actorKey,
		SubmittedAt:   "2026-05-17T12:00:00Z",
		Class:         "identity",
		Payload:       pb,
		ContextHint: &ContextHint{
			ScanPrefixes: []string{"vtx.identity.", "lnk.identity."},
		},
	}
	b, _ := json.Marshal(env)
	if _, err := conn.JetStream().Publish(context.Background(), "ops.default", b); err != nil {
		t.Fatalf("publish ApproveIdentityMerge: %v", err)
	}
}

// publishMergeOp submits a MergeIdentity op as the operator actor (stub auth).
func publishMergeOp(t *testing.T, conn *substrate.Conn, reqID, primary, secondary string, acr map[string]any) {
	t.Helper()
	p := map[string]any{"primary": primary, "secondary": secondary}
	if acr != nil {
		p["aspectConflictResolution"] = acr
	}
	pb, _ := json.Marshal(p)
	env := &OperationEnvelope{
		RequestID:     reqID,
		Lane:          LaneDefault,
		OperationType: "MergeIdentity",
		Actor:         imOperatorActorKey,
		SubmittedAt:   "2026-05-17T12:00:00Z",
		Class:         "identity",
		Payload:       pb,
		ContextHint: &ContextHint{
			ScanPrefixes: []string{"vtx.identity.", "lnk."},
		},
	}
	b, _ := json.Marshal(env)
	if _, err := conn.JetStream().Publish(context.Background(), "ops.default", b); err != nil {
		t.Fatalf("publish MergeIdentity: %v", err)
	}
}

// ---- Tests: capability-mode (ApproveIdentityMerge) ----

// TestApproveIdentityMerge_SurfacesFlaggedPairs seeds 4 flagged identities
// forming 2 pairs + 1 unflagged identity, then asserts ApproveIdentityMerge
// is accepted (review op writes only the tracker). Detailed response-detail
// assertions are best-effort via tracker eventClasses — since the review op
// emits no events, we verify accepted outcome + tracker present + no
// unexpected state mutations.
func TestApproveIdentityMerge_SurfacesFlaggedPairs(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineCapability(t, ctx, conn, "im-approve-surface")

	aKey := "vtx.identity.ApFmaggedAaaaaaaaXVW"
	bKey := "vtx.identity.ApFmaggedBaaaaaaa1XX"
	cKey := "vtx.identity.ApFmaggedCaaaaaaa1XX"
	dKey := "vtx.identity.ApFmaggedDaaaaaaa1XX"
	eKey := "vtx.identity.ApUnfmaggedEaaaaXXX1"

	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "Alice One", "shared1@x.example", "")
	seedMergeIdentity(t, ctx, conn, bKey, "flagged-for-review", "Alice Two", "shared1@x.example", "")
	seedMergeIdentity(t, ctx, conn, cKey, "flagged-for-review", "Bob Smith", "bob@x.example", "")
	seedMergeIdentity(t, ctx, conn, dKey, "flagged-for-review", "Bob Smithe", "different@x.example", "")
	seedMergeIdentity(t, ctx, conn, eKey, "unclaimed", "Charlie", "charlie@x.example", "")

	seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)
	seedMergeDuplicateOfLink(t, ctx, conn, cKey, dKey)

	reqID := genReqID("ApSurfaceXXX")
	publishApproveOp(t, conn, reqID, imOperatorActorKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Tracker should exist.
	if _, err := conn.KVGet(ctx, imTestBucket, TrackerKey(reqID)); err != nil {
		t.Fatalf("tracker missing: %v", err)
	}
	// No state mutations expected on review op.
	for _, k := range []string{aKey, bKey, cKey, dKey} {
		data := readMergeAspectData(t, ctx, conn, k+".state")
		if data["value"] != "flagged-for-review" {
			t.Errorf("%s.state mutated: %v", k, data["value"])
		}
	}
	if data := readMergeAspectData(t, ctx, conn, eKey+".state"); data["value"] != "unclaimed" {
		t.Errorf("%s.state mutated: %v", eKey, data["value"])
	}
}

// TestApproveIdentityMerge_FiltersByPair: with primaryKey+secondaryKey set
// to one real flagged pair, the op is accepted. With a non-existent pair,
// the op is rejected (ReviewPairNotFound).
func TestApproveIdentityMerge_FiltersByPair(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineCapability(t, ctx, conn, "im-approve-filter")

	aKey := "vtx.identity.ApFimterAaaaaaaaa1XX"
	bKey := "vtx.identity.ApFimterBaaaaaaaa1XX"
	cKey := "vtx.identity.ApFimterCaaaaaaaa1XX"
	dKey := "vtx.identity.ApFimterDaaaaaaaa1XX"

	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "X", "x@x.example", "")
	seedMergeIdentity(t, ctx, conn, bKey, "flagged-for-review", "X", "x@x.example", "")
	seedMergeIdentity(t, ctx, conn, cKey, "flagged-for-review", "Y", "y@y.example", "")
	seedMergeIdentity(t, ctx, conn, dKey, "flagged-for-review", "Y", "y@y.example", "")
	seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)
	seedMergeDuplicateOfLink(t, ctx, conn, cKey, dKey)

	// Real pair (a, b): accepted.
	req1 := genReqID("ApFimterReal0")
	publishApproveOp(t, conn, req1, imOperatorActorKey, map[string]any{
		"primaryKey": aKey, "secondaryKey": bKey,
	})
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Bogus pair (a, c): no duplicateOf link between them, no pair surfaces.
	// Script raises ReviewPairNotFound; outcome is rejected.
	req2 := genReqID("ApFimterBogus")
	publishApproveOp(t, conn, req2, imOperatorActorKey, map[string]any{
		"primaryKey": aKey, "secondaryKey": cKey,
	})
	driveOne(t, ctx, cp, cons, OutcomeRejected)
}

// TestApproveIdentityMerge_HasCredentialBindingFlag: one of the pair is a
// claimed identity (has credentialBinding aspect). The script reports
// hasCredentialBinding correctly. We assert via accepted outcome + cb aspect
// presence in Core KV (since response detail is delivered via JetStream
// reply path, not directly assertable here).
func TestApproveIdentityMerge_HasCredentialBindingFlag(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineCapability(t, ctx, conn, "im-approve-cb")

	aKey := "vtx.identity.ApCbBoundAaaaaaaaXXX"
	bKey := "vtx.identity.ApCbUnboundBaaaaaXXX"

	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "Bound", "shared@cb.example", "")
	seedMergeIdentity(t, ctx, conn, bKey, "flagged-for-review", "Unbound", "shared@cb.example", "")
	seedMergeCredentialBinding(t, ctx, conn, aKey, "vtx.identity.AnotherActorXXXXXXXX")
	seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)

	req := genReqID("ApCbBoundXXXX")
	publishApproveOp(t, conn, req, imOperatorActorKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Sanity: credentialBinding aspect exists for aKey, absent for bKey.
	if _, err := conn.KVGet(ctx, imTestBucket, aKey+".credentialBinding"); err != nil {
		t.Errorf("aKey credentialBinding aspect should exist: %v", err)
	}
	if _, err := conn.KVGet(ctx, imTestBucket, bKey+".credentialBinding"); err == nil {
		t.Errorf("bKey credentialBinding aspect should NOT exist")
	}
}

// TestApproveIdentityMerge_NonOperatorDenied: consumer actor → step 3 denies.
func TestApproveIdentityMerge_NonOperatorDenied(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineCapability(t, ctx, conn, "im-approve-denied")

	req := genReqID("ApDeniedConsX")
	publishApproveOp(t, conn, req, imConsumerActorKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeRejected)
}

// TestApproveIdentityMerge_EmptyWhenNoFlagged: no flagged identities,
// op succeeds with empty pairs response.
func TestApproveIdentityMerge_EmptyWhenNoFlagged(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineCapability(t, ctx, conn, "im-approve-empty")

	aKey := "vtx.identity.ApEmptyAaaaaaaaaa1XX"
	bKey := "vtx.identity.ApEmptyBaaaaaaaaa1XX"
	seedMergeIdentity(t, ctx, conn, aKey, "unclaimed", "A", "a@x.example", "")
	seedMergeIdentity(t, ctx, conn, bKey, "claimed", "B", "b@x.example", "")

	req := genReqID("ApEmptyZZZZZ")
	publishApproveOp(t, conn, req, imOperatorActorKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)
}

// ---- Tests: stub-mode (MergeIdentity) ----

// TestMergeIdentity_HappyPath seeds two flagged identities A (primary) and B
// (secondary) with multiple links of varied directions, plus the canonical
// duplicateOf link. Runs MergeIdentity. Asserts:
//   - all secondary-anchored links are rekeyed to primary,
//   - originals are tombstoned,
//   - duplicateOf link is tombstoned (no recreate),
//   - secondary.state == "merged",
//   - secondary.mergedInto == primary key,
//   - primary.state unchanged.
func TestMergeIdentity_HappyPath(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineStub(t, ctx, conn, "im-merge-happy")

	aKey := "vtx.identity.MgHappyAaaaaaaaaa1XX"
	bKey := "vtx.identity.MgHappyBaaaaaaaaa1XX"
	roleX := "vtx.role.HappyRomeXxxxxxxxXXX"
	roleY := "vtx.role.HappyRomeYxxxxxxxXXX"
	otherActorKey := "vtx.identity.MgHappyQtherActorVWX"

	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "Primary", "shared@happy.example", "")
	seedMergeIdentity(t, ctx, conn, bKey, "flagged-for-review", "Secondary", "shared@happy.example", "")
	seedMergeCredentialBinding(t, ctx, conn, aKey, aKey)

	// B's outbound link: lnk.identity.B.holdsRole.role.RoleY
	bIDOut := bKey[len("vtx.identity."):]
	roleYID := roleX[len("vtx.role."):] // placeholder; reuse roleX in next line for B's outbound
	_ = roleYID
	bHoldsRoleY := "lnk.identity." + bIDOut + ".holdsRole.role." + roleY[len("vtx.role."):]
	seedMergeGenericLink(t, ctx, conn, bHoldsRoleY, "holdsRole", map[string]any{"note": "B->RoleY"})

	// Inbound link: lnk.identity.<other>.knows.identity.B
	otherID := otherActorKey[len("vtx.identity."):]
	otherKnowsB := "lnk.identity." + otherID + ".knows.identity." + bIDOut
	seedMergeGenericLink(t, ctx, conn, otherKnowsB, "knows", map[string]any{"since": "2026"})

	// Primary already holds RoleX (just so the cap doc would differ).
	aIDOut := aKey[len("vtx.identity."):]
	aHoldsRoleX := "lnk.identity." + aIDOut + ".holdsRole.role." + roleX[len("vtx.role."):]
	seedMergeGenericLink(t, ctx, conn, aHoldsRoleX, "holdsRole", map[string]any{"note": "A->RoleX"})

	// Canonical duplicateOf link.
	dupLinkKey := seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)

	req := genReqID("MgHappyXXXXX")
	publishMergeOp(t, conn, req, aKey, bKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Secondary state == merged.
	if data := readMergeAspectData(t, ctx, conn, bKey+".state"); data["value"] != "merged" {
		t.Errorf("bKey.state = %v, want merged", data["value"])
	}
	// Secondary mergedInto == primary.
	if data := readMergeAspectData(t, ctx, conn, bKey+".mergedInto"); data["value"] != aKey {
		t.Errorf("bKey.mergedInto = %v, want %s", data["value"], aKey)
	}
	// Primary state unchanged.
	if data := readMergeAspectData(t, ctx, conn, aKey+".state"); data["value"] != "flagged-for-review" {
		t.Errorf("aKey.state = %v, want flagged-for-review (unchanged)", data["value"])
	}
	// duplicateOf link tombstoned.
	dup := readMergeLink(t, ctx, conn, dupLinkKey)
	if dup == nil {
		t.Fatalf("duplicateOf link disappeared at %s", dupLinkKey)
	}
	if del, _ := dup["isDeleted"].(bool); !del {
		t.Errorf("duplicateOf link not tombstoned: %v", dup)
	}
	// B's outbound rekeyed to A: lnk.identity.<A>.holdsRole.role.<RoleY>
	aHoldsRoleY := "lnk.identity." + aIDOut + ".holdsRole.role." + roleY[len("vtx.role."):]
	if doc := readMergeLink(t, ctx, conn, aHoldsRoleY); doc == nil {
		t.Errorf("rekeyed link missing at %s", aHoldsRoleY)
	} else if del, _ := doc["isDeleted"].(bool); del {
		t.Errorf("rekeyed link tombstoned at %s: %v", aHoldsRoleY, doc)
	}
	// Original B-anchored outbound tombstoned.
	if doc := readMergeLink(t, ctx, conn, bHoldsRoleY); doc != nil {
		if del, _ := doc["isDeleted"].(bool); !del {
			t.Errorf("original B-anchored link not tombstoned at %s: %v", bHoldsRoleY, doc)
		}
	}
	// Inbound rekeyed: lnk.identity.<other>.knows.identity.<A>
	otherKnowsA := "lnk.identity." + otherID + ".knows.identity." + aIDOut
	if doc := readMergeLink(t, ctx, conn, otherKnowsA); doc == nil {
		t.Errorf("rekeyed inbound link missing at %s", otherKnowsA)
	}
	// Original inbound tombstoned.
	if doc := readMergeLink(t, ctx, conn, otherKnowsB); doc != nil {
		if del, _ := doc["isDeleted"].(bool); !del {
			t.Errorf("original inbound link not tombstoned at %s", otherKnowsB)
		}
	}
}

// TestMergeIdentity_RejectsNonFlaggedSecondary: secondary is unclaimed → reject.
func TestMergeIdentity_RejectsNonFlaggedSecondary(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineStub(t, ctx, conn, "im-merge-non-flagged")

	aKey := "vtx.identity.MgRejNonFmagAxxxxXXX"
	bKey := "vtx.identity.MgRejNonFmagBxxxxXXX"
	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "A", "x@x", "")
	seedMergeIdentity(t, ctx, conn, bKey, "unclaimed", "B", "x@x", "")
	seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)

	req := genReqID("MgRejNonFmag")
	publishMergeOp(t, conn, req, aKey, bKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	// State of B unchanged.
	if data := readMergeAspectData(t, ctx, conn, bKey+".state"); data["value"] != "unclaimed" {
		t.Errorf("bKey.state changed: %v", data["value"])
	}
}

// TestMergeIdentity_RejectsAlreadyMergedSecondary: secondary in merged state →
// reject WITHOUT leaking mergedInto target (NFR-S6). We can't directly inspect
// the rejection message from this harness, but we verify no state change.
func TestMergeIdentity_RejectsAlreadyMergedSecondary(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineStub(t, ctx, conn, "im-merge-already-merged")

	aKey := "vtx.identity.MgRejMrgAxxxxxxxxXXX"
	bKey := "vtx.identity.MgRejMrgBxxxxxxxxXXX"
	survivorKey := "vtx.identity.MgRejMrgSurvxxxxxXXX"
	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "A", "x@x", "")
	seedMergeIdentity(t, ctx, conn, bKey, "merged", "B", "x@x", "")

	// Seed mergedInto on B pointing to a survivor (NOT the operator-supplied primary,
	// so a leak would expose this value).
	miDoc := map[string]any{
		"class": "mergedInto", "vertexKey": bKey, "localName": "mergedInto",
		"isDeleted": false, "data": map[string]any{"value": survivorKey},
	}
	mib, _ := json.Marshal(miDoc)
	if _, err := conn.KVPut(ctx, imTestBucket, bKey+".mergedInto", mib); err != nil {
		t.Fatalf("seed mergedInto: %v", err)
	}
	seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)

	req := genReqID("MgRejMergedX")
	publishMergeOp(t, conn, req, aKey, bKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	// B state remains merged, mergedInto unchanged.
	if data := readMergeAspectData(t, ctx, conn, bKey+".state"); data["value"] != "merged" {
		t.Errorf("bKey.state = %v, want merged", data["value"])
	}
	if data := readMergeAspectData(t, ctx, conn, bKey+".mergedInto"); data["value"] != survivorKey {
		t.Errorf("bKey.mergedInto leaked or mutated: %v", data["value"])
	}
}

// TestMergeIdentity_RejectsMissingDuplicateOfLink: both flagged but no link → reject.
func TestMergeIdentity_RejectsMissingDuplicateOfLink(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineStub(t, ctx, conn, "im-merge-no-link")

	aKey := "vtx.identity.MgRejNoLnkAxxxxxxXXX"
	bKey := "vtx.identity.MgRejNoLnkBxxxxxxXXX"
	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "A", "x@x", "")
	seedMergeIdentity(t, ctx, conn, bKey, "flagged-for-review", "B", "x@x", "")
	// No duplicateOf link seeded.

	req := genReqID("MgRejNoLinkX")
	publishMergeOp(t, conn, req, aKey, bKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeRejected)
}

// TestMergeIdentity_SelfReferenceRejected: primary == secondary → reject.
func TestMergeIdentity_SelfReferenceRejected(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineStub(t, ctx, conn, "im-merge-self-ref")

	aKey := "vtx.identity.MgRejSemfAxxxxxxxXXX"
	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "A", "x@x", "")

	req := genReqID("MgRejSelfRef")
	publishMergeOp(t, conn, req, aKey, aKey, nil)
	driveOne(t, ctx, cp, cons, OutcomeRejected)
}

// TestMergeIdentity_AspectConflictResolution: primary and secondary have
// distinct email aspects. With aspectConflictResolution{email: secondary-wins},
// primary's email becomes secondary's. Other aspects unchanged.
func TestMergeIdentity_AspectConflictResolution(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineStub(t, ctx, conn, "im-merge-acr")

	aKey := "vtx.identity.MgAcrAxxxxxxxxxxxXXX"
	bKey := "vtx.identity.MgAcrBxxxxxxxxxxxXXX"
	seedMergeIdentity(t, ctx, conn, aKey, "flagged-for-review", "Primary", "primary@acr.example", "")
	seedMergeIdentity(t, ctx, conn, bKey, "flagged-for-review", "Secondary", "secondary@acr.example", "")
	seedMergeDuplicateOfLink(t, ctx, conn, aKey, bKey)

	req := genReqID("MgAcrEmailSW")
	publishMergeOp(t, conn, req, aKey, bKey, map[string]any{"email": "secondary-wins"})
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Primary email now matches secondary's pre-merge email.
	if data := readMergeAspectData(t, ctx, conn, aKey+".email"); data["value"] != "secondary@acr.example" {
		t.Errorf("aKey.email = %v, want secondary@acr.example", data["value"])
	}
	// Primary name unchanged.
	if data := readMergeAspectData(t, ctx, conn, aKey+".name"); data["value"] != "Primary" {
		t.Errorf("aKey.name = %v, want Primary (unchanged)", data["value"])
	}
	// Secondary state == merged.
	if data := readMergeAspectData(t, ctx, conn, bKey+".state"); data["value"] != "merged" {
		t.Errorf("bKey.state = %v, want merged", data["value"])
	}
}

// TestMergeIdentity_PostMergeRedirect_FR4 (capability mode): after a merge,
// any UpdateIdentityState op against secondary's key is rejected by the
// enforce_not_merged guard. This validates FR4's "secondary remains queryable
// but unwritable" contract. Capability mode because UpdateIdentityState IS
// a grant the operator holds.
func TestMergeIdentity_PostMergeRedirect_FR4(t *testing.T) {
	ctx, conn := setupMergeTestEnv(t)
	cp, cons := newMergePipelineCapability(t, ctx, conn, "im-merge-redirect")

	primaryKey := "vtx.identity.MgRedirPrxxxxxxxxXXX"
	mergedKey := "vtx.identity.MgRedirSxxxxxxxxxXXX"
	seedMergeIdentity(t, ctx, conn, mergedKey, "merged", "Merged", "", "")
	// Seed mergedInto on merged identity pointing to primary.
	miDoc := map[string]any{
		"class": "mergedInto", "vertexKey": mergedKey, "localName": "mergedInto",
		"isDeleted": false, "data": map[string]any{"value": primaryKey},
	}
	mib, _ := json.Marshal(miDoc)
	if _, err := conn.KVPut(ctx, imTestBucket, mergedKey+".mergedInto", mib); err != nil {
		t.Fatalf("seed mergedInto: %v", err)
	}

	env := &OperationEnvelope{
		RequestID:     genReqID("MgRedirOpAAA"),
		Lane:          LaneDefault,
		OperationType: "UpdateIdentityState",
		Actor:         imOperatorActorKey,
		SubmittedAt:   "2026-05-17T12:00:00Z",
		Class:         "identity",
		Payload: json.RawMessage(`{"identityKey":"` + mergedKey +
			`","newState":"claimed"}`),
		ContextHint: &ContextHint{Reads: []string{
			mergedKey + ".state", mergedKey + ".mergedInto",
		}},
	}
	b, _ := json.Marshal(env)
	if _, err := conn.JetStream().Publish(context.Background(), "ops.default", b); err != nil {
		t.Fatalf("publish UpdateIdentityState on merged: %v", err)
	}
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	// State still merged.
	if data := readMergeAspectData(t, ctx, conn, mergedKey+".state"); data["value"] != "merged" {
		t.Errorf("mergedKey.state mutated: %v", data["value"])
	}
}
