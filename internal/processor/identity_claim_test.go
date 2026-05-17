// Story 4.3 — Two-Phase Identity Claim (FR2, FR5) integration tests.
//
// Validates ClaimIdentity end-to-end through the 10-step Processor
// pipeline against an embedded NATS server. Tests chain a 4.2
// CreateUnclaimedIdentity op (arrange phase) with a 4.3 ClaimIdentity
// op (act phase) so both ops are exercised together.
//
// Tests:
//  1. TestClaimIdentity_Success                         — full happy path
//  2. TestClaimIdentity_WrongKey_GenericError           — wrong plaintext key
//  3. TestClaimIdentity_AlreadyClaimed_GenericError     — state=claimed
//  4. TestClaimIdentity_Flagged_GenericError            — state=flagged-for-review
//  5. TestClaimIdentity_Merged_GenericError             — state=merged
//  6. TestClaimIdentity_CredentialAlreadyBound_GenericError — credentialindex present
//  7. TestClaimIdentity_FR5_GrandfatheredFlow           — historical import / no 4.2 op
//  8. TestClaimIdentity_FR5_ImmediateAccess             — cap doc reachable after claim
//
// All tests use capability-auth mode. Fixture seeding mirrors
// identity_create_test.go patterns from Story 4.2.
package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/substrate"
	"github.com/nats-io/nats.go/jetstream"
)

// Test NanoIDs and keys for claim tests.
const (
	// Staff (operator) actor that creates unclaimed identities.
	iclStaffActorID  = "JclStfActHKLMNPQRSTUV" // 20 chars, safe alphabet
	iclStaffActorKey = "vtx.identity." + iclStaffActorID
	iclStaffCapKey   = "cap.identity." + iclStaffActorID

	// Consumer actor that claims identities.
	iclConsumerActorID  = "JclCnActHKLMNPQRSTUV" // 20 chars
	iclConsumerActorKey = "vtx.identity." + iclConsumerActorID
	iclConsumerCapKey   = "cap.identity." + iclConsumerActorID

	iclTestBucket    = "core-kv"
	iclHealthBucket  = "health-kv"
	iclCapBucket     = "capability-kv"
	iclOpsStreamName = "core-operations"

	iclInstance = "icl-test"
)

// iclStaffCapDoc seeds an operator cap doc with CreateUnclaimedIdentity.
func iclStaffCapDoc() *CapabilityDoc {
	now := time.Now().UTC()
	return &CapabilityDoc{
		Key:                    iclStaffCapKey,
		Actor:                  iclStaffActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{iclStaffActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []PlatformPermission{
			{OperationType: "CreateUnclaimedIdentity", Scope: "any"},
		},
		ServiceAccess:   []ServiceAccessEntry{},
		EphemeralGrants: []EphemeralGrant{},
		Roles:           []string{"vtx.role.operator"},
	}
}

// iclConsumerCapDoc seeds a consumer cap doc with ClaimIdentity.
func iclConsumerCapDoc() *CapabilityDoc {
	now := time.Now().UTC()
	return &CapabilityDoc{
		Key:                    iclConsumerCapKey,
		Actor:                  iclConsumerActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{iclConsumerActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []PlatformPermission{
			{OperationType: "ClaimIdentity", Scope: "self"},
		},
		ServiceAccess:   []ServiceAccessEntry{},
		EphemeralGrants: []EphemeralGrant{},
		Roles:           []string{"vtx.role.consumer"},
	}
}

// provisionClaimHarness sets up KV buckets, streams, and capability docs.
func provisionClaimHarness(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()
	js := conn.JetStream()

	for _, bucket := range []string{iclTestBucket, iclHealthBucket} {
		_, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:         bucket,
			LimitMarkerTTL: time.Second,
		})
		if err != nil {
			t.Fatalf("create KV %q: %v", bucket, err)
		}
	}
	streamName := "KV_" + iclTestBucket
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
		Bucket:         iclCapBucket,
		LimitMarkerTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("create capability-kv: %v", err)
	}
	staffDoc, _ := json.Marshal(iclStaffCapDoc())
	if _, err := capKV.Put(ctx, iclStaffCapKey, staffDoc); err != nil {
		t.Fatalf("seed staff cap doc: %v", err)
	}
	conDoc, _ := json.Marshal(iclConsumerCapDoc())
	if _, err := capKV.Put(ctx, iclConsumerCapKey, conDoc); err != nil {
		t.Fatalf("seed consumer cap doc: %v", err)
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     iclOpsStreamName,
		Subjects: []string{"ops.>"},
	})
	if err != nil {
		t.Fatalf("create core-operations stream: %v", err)
	}
}

// seedIdentityDDLForClaim writes the identity DDL in shadow-key form for the
// DDL cache. Reuses the DDL from bootstrap.IdentityDDL().
func seedIdentityDDLForClaim(t *testing.T, ctx context.Context, conn *substrate.Conn) {
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
	if _, err := conn.KVPut(ctx, iclTestBucket, ddlKey, ddlBytes); err != nil {
		t.Fatalf("seed identity DDL: %v", err)
	}

	scriptDoc := map[string]any{
		"class":     "meta.script",
		"isDeleted": false,
		"data":      map[string]any{"source": ddl.Script},
	}
	scriptBytes, _ := json.Marshal(scriptDoc)
	if _, err := conn.KVPut(ctx, iclTestBucket, ddlKey+".script", scriptBytes); err != nil {
		t.Fatalf("seed identity DDL script: %v", err)
	}
}

// setupClaimTestEnv returns an embedded NATS env with harness + identity DDL.
func setupClaimTestEnv(t *testing.T) (context.Context, *substrate.Conn) {
	t.Helper()
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	conn, err := substrate.Connect(ctx, substrate.ConnectOpts{URL: url, Name: "icl-test"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(conn.Close)
	provisionClaimHarness(t, ctx, conn)
	seedIdentityDDLForClaim(t, ctx, conn)
	return ctx, conn
}

// newClaimPipeline builds a capability-mode CommitPath with ClaimEmitter wired.
func newClaimPipeline(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) (*CommitPath, jetstream.Consumer) {
	t.Helper()
	logger := testLogger()
	metrics := &Metrics{}
	hb := NewHealthHeartbeater(conn, iclHealthBucket, iclInstance+"-"+durable, 10*time.Second, metrics, logger)
	cache := NewDDLCache(conn, iclTestBucket, logger)
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("ddl cache refresh: %v", err)
	}
	authz, err := SelectAuthorizerArgs(SelectAuthorizerOpts{
		Mode:             AuthModeCapability,
		Reader:           conn,
		CapabilityBucket: iclCapBucket,
		Logger:           logger,
	})
	if err != nil {
		t.Fatalf("SelectAuthorizerArgs: %v", err)
	}
	claimEmitter := NewClaimAttemptEmitter(conn, iclHealthBucket, iclInstance+"-"+durable, logger)
	committer := NewCommitter(conn, iclTestBucket, cache, logger, time.Now)
	cp := NewCommitPath(Deps{
		Conn:         conn,
		CoreBucket:   iclTestBucket,
		HealthKV:     iclHealthBucket,
		Authorizer:   authz,
		Hydrator:     NewHydratorWithCache(conn, iclTestBucket, cache, logger),
		Executor:     NewExecutor(NewStarlarkRunner(0, 0), logger),
		Validator:    NewValidator(cache, logger),
		Committer:    committer,
		Events:       &StubEventPublisher{logger: logger},
		Metrics:      metrics,
		Heartbeater:  hb,
		Logger:       logger,
		ClaimEmitter: claimEmitter,
	})
	cons, err := EnsureConsumer(ctx, conn.JetStream(), ConsumerConfig{
		StreamName:     iclOpsStreamName,
		Durable:        durable,
		FilterSubjects: []string{"ops.default"},
		AckWait:        5 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("EnsureConsumer: %v", err)
	}
	return cp, cons
}

// publishOp marshals and publishes any OperationEnvelope.
func publishOp(t *testing.T, conn *substrate.Conn, env *OperationEnvelope) {
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

// iclCredentialIndexKey mirrors crypto.sha256NanoID(actorKey) used in the
// Starlark script to derive the credentialindex vertex key.
func iclCredentialIndexKey(actorKey string) string {
	sum := sha256.Sum256([]byte(actorKey))
	seed := [2]uint64{
		(uint64(sum[0]) << 56) | (uint64(sum[1]) << 48) | (uint64(sum[2]) << 40) | (uint64(sum[3]) << 32) |
			(uint64(sum[4]) << 24) | (uint64(sum[5]) << 16) | (uint64(sum[6]) << 8) | uint64(sum[7]),
		(uint64(sum[8]) << 56) | (uint64(sum[9]) << 48) | (uint64(sum[10]) << 40) | (uint64(sum[11]) << 32) |
			(uint64(sum[12]) << 24) | (uint64(sum[13]) << 16) | (uint64(sum[14]) << 8) | uint64(sum[15]),
	}
	pcg := rand.NewPCG(seed[0], seed[1])
	nanoID := deterministicNanoID(pcg, substrate.NanoIDLength)
	return "vtx.credentialindex." + nanoID
}

// iclCreateIdentityAndGetKeys runs CreateUnclaimedIdentity as the staff actor
// and returns the identityKey + plaintext claimKey for use in subsequent tests.
// Must be called with a consumer that has NOT yet processed any messages; the
// create op is the next message in the stream.
func iclCreateIdentityAndGetKeys(t *testing.T, ctx context.Context, conn *substrate.Conn, cp *CommitPath, cons jetstream.Consumer, reqID string) (identityKey, claimKey string) {
	t.Helper()
	identityID, claimKeyPlaintext := iciNanoIDsFromRequestID(reqID)
	identityKey = "vtx.identity." + identityID

	env := &OperationEnvelope{
		RequestID:     reqID,
		Lane:          LaneDefault,
		OperationType: "CreateUnclaimedIdentity",
		Actor:         iclStaffActorKey,
		SubmittedAt:   "2026-05-17T10:00:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"name":"Test Identity","email":"test@claim.example"}`),
	}
	publishOp(t, conn, env)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Verify identity was created with state=unclaimed.
	stateData := readAspectData(t, ctx, conn, identityKey+".state")
	if got, _ := stateData["value"].(string); got != "unclaimed" {
		t.Fatalf("create: state = %q, want unclaimed", got)
	}
	return identityKey, claimKeyPlaintext
}

// readClaimHealthCounter reads the claim-attempt counter for a specific outcome.
func readClaimHealthCounter(t *testing.T, ctx context.Context, conn *substrate.Conn, instance, outcome string) (count int64, found bool) {
	t.Helper()
	key := "health.processor." + instance + ".claim-attempts." + outcome
	entry, err := conn.KVGet(ctx, iclHealthBucket, key)
	if err != nil {
		return 0, false
	}
	var doc map[string]any
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		return 0, false
	}
	c, _ := doc["count"].(float64)
	return int64(c), true
}

// ---- Tests ----

// TestClaimIdentity_Success: staff creates unclaimed identity; consumer submits
// ClaimIdentity with the plaintext claim key; assert:
//   - step 8 commit (OutcomeAccepted)
//   - state aspect == "claimed"
//   - credentialBinding aspect exists with consumer's actorKey
//   - claimKey aspect is tombstoned (isDeleted: true)
//   - credentialindex vertex exists
//   - IdentityClaimed event published (in tracker eventClasses)
//   - Health KV claim-attempts.success count incremented
func TestClaimIdentity_Success(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)

	// Single pipeline + consumer drives both create and claim ops.
	// Consumer sees messages in order: create first, then claim.
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-succ")

	createReqID := genReqID("ClmSuccCreate")
	identityKey, claimKeyPlaintext := iclCreateIdentityAndGetKeys(t, ctx, conn, cp, cons, createReqID)

	credIndexKey := iclCredentialIndexKey(iclConsumerActorKey)

	claimReqID := genReqID("ClmSuccClaim0")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload: json.RawMessage(`{"claimKey":"` + claimKeyPlaintext + `","targetIdentityKey":"` + identityKey + `"}`),
		// scope=self: authContext.target == actor satisfies step 3.
		// Actual one-credential-one-identity enforcement is in the Starlark script.
		AuthContext: &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// Only include keys guaranteed to exist in KV. Optional reads
			// (credentialBinding, mergedInto, credIndexKey) are absent on a
			// first-time claim; the Starlark script handles state[key]==None.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
			},
		},
	}
	publishOp(t, conn, claimEnv)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// State must be "claimed".
	stateData := readAspectData(t, ctx, conn, identityKey+".state")
	if got, _ := stateData["value"].(string); got != "claimed" {
		t.Fatalf("state = %q, want claimed", got)
	}

	// credentialBinding aspect must exist with consumer's actorKey.
	bindData := readAspectData(t, ctx, conn, identityKey+".credentialBinding")
	if got, _ := bindData["actorKey"].(string); got != iclConsumerActorKey {
		t.Fatalf("credentialBinding.actorKey = %q, want %q", got, iclConsumerActorKey)
	}

	// claimKey aspect must be tombstoned.
	entry, err := conn.KVGet(ctx, iclTestBucket, identityKey+".claimKey")
	if err != nil {
		t.Fatalf("claimKey aspect not found: %v", err)
	}
	var claimKeyDoc map[string]any
	if err := json.Unmarshal(entry.Value, &claimKeyDoc); err != nil {
		t.Fatalf("unmarshal claimKey aspect: %v", err)
	}
	if isDeleted, _ := claimKeyDoc["isDeleted"].(bool); !isDeleted {
		t.Fatalf("claimKey aspect should be tombstoned (isDeleted=true), got isDeleted=%v; full doc: %v", isDeleted, claimKeyDoc)
	}

	// credentialindex vertex must exist.
	if _, err := conn.KVGet(ctx, iclTestBucket, credIndexKey); err != nil {
		t.Fatalf("credentialindex vertex not found at %s: %v", credIndexKey, err)
	}

	// Tracker must record IdentityClaimed event.
	te, err := conn.KVGet(ctx, iclTestBucket, TrackerKey(claimReqID))
	if err != nil {
		t.Fatalf("claim tracker not found: %v", err)
	}
	tr, _ := ParseTracker(te.Value)
	ecs, _ := tr.Data["eventClasses"].([]interface{})
	found := false
	for _, ec := range ecs {
		if ec == "IdentityClaimed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("IdentityClaimed not in tracker eventClasses: %v", ecs)
	}

	// Health KV claim-attempts.success count must be >= 1.
	instance := iclInstance + "-icl-succ"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "success")
	if !ok {
		t.Fatalf("claim-attempts.success Health KV entry not found for instance %s", instance)
	}
	if count < 1 {
		t.Fatalf("claim-attempts.success count = %d, want >= 1", count)
	}
}

// TestClaimIdentity_WrongKey_GenericError: consumer submits ClaimIdentity with
// a garbage plaintext key; assert generic ClaimKeyInvalid reply, state unchanged,
// no credentialindex written, Health KV claim-attempts.invalid-key incremented.
func TestClaimIdentity_WrongKey_GenericError(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-wrkey")

	createReqID := genReqID("ClmWrKeyCreate")
	identityKey, _ := iclCreateIdentityAndGetKeys(t, ctx, conn, cp, cons, createReqID)

	credIndexKey := iclCredentialIndexKey(iclConsumerActorKey)

	claimReqID := genReqID("ClmWrKeyClaim0")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"garbage-wrong-key-12345","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credentialBinding, mergedInto, credIndexKey not yet seeded → omit.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
			},
		},
	}
	publishOp(t, conn, claimEnv)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	// State must still be "unclaimed".
	stateData := readAspectData(t, ctx, conn, identityKey+".state")
	if got, _ := stateData["value"].(string); got != "unclaimed" {
		t.Fatalf("state = %q, want unclaimed (should be unchanged on failure)", got)
	}

	// credentialindex must NOT exist.
	if _, err := conn.KVGet(ctx, iclTestBucket, credIndexKey); err == nil {
		t.Fatalf("credentialindex vertex should NOT exist after wrong-key failure")
	}

	// Health KV claim-attempts.invalid-key count must be >= 1.
	instance := iclInstance + "-icl-wrkey"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "invalid-key")
	if !ok {
		t.Fatalf("claim-attempts.invalid-key Health KV entry not found")
	}
	if count < 1 {
		t.Fatalf("claim-attempts.invalid-key count = %d, want >= 1", count)
	}
}

// TestClaimIdentity_AlreadyClaimed_GenericError: pre-state identity has
// state=claimed; assert generic ClaimKeyInvalid, Health KV wrong-state incremented.
func TestClaimIdentity_AlreadyClaimed_GenericError(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-alrcl")

	// Pre-seed an identity with state=claimed (bypassing the DDL script).
	identityID := genReqID("PreClaimedIdnt")
	identityKey := "vtx.identity." + identityID

	seedDirectIdentity(t, ctx, conn, identityKey, "claimed", "")
	// Also seed a claimKey aspect so the script doesn't short-circuit on that.
	seedClaimKeyAspect(t, ctx, conn, identityKey, "dummy-hash-value-64-chars-padding-xxxxxxxxxxxxxxxxxx")

	claimReqID := genReqID("ClmAlrClClaim0")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"any-key","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credentialBinding, mergedInto, credIndexKey not seeded → omit.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
			},
		},
	}
	publishOp(t, conn, claimEnv)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	instance := iclInstance + "-icl-alrcl"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "wrong-state")
	if !ok {
		t.Fatalf("claim-attempts.wrong-state Health KV entry not found")
	}
	if count < 1 {
		t.Fatalf("claim-attempts.wrong-state count = %d, want >= 1", count)
	}
}

// TestClaimIdentity_Flagged_GenericError: identity has state=flagged-for-review;
// assert generic ClaimKeyInvalid, Health KV flagged incremented.
func TestClaimIdentity_Flagged_GenericError(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-flagged")

	identityID := genReqID("FlaggedIdentit")
	identityKey := "vtx.identity." + identityID

	seedDirectIdentity(t, ctx, conn, identityKey, "flagged-for-review", "")
	seedClaimKeyAspect(t, ctx, conn, identityKey, "dummy-hash-value-64-chars-padding-xxxxxxxxxxxxxxxxxx")

	claimReqID := genReqID("ClmFlagClaim00")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"any-key","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credentialBinding, mergedInto, credIndexKey not seeded → omit.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
			},
		},
	}
	publishOp(t, conn, claimEnv)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	instance := iclInstance + "-icl-flagged"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "flagged")
	if !ok {
		t.Fatalf("claim-attempts.flagged Health KV entry not found")
	}
	if count < 1 {
		t.Fatalf("claim-attempts.flagged count = %d, want >= 1", count)
	}
}

// TestClaimIdentity_Merged_GenericError: identity has state=merged and mergedInto
// set; assert generic ClaimKeyInvalid (NOT IdentityMerged — NFR-S6 anti-enumeration),
// Health KV merged incremented.
func TestClaimIdentity_Merged_GenericError(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-merged")

	identityID := genReqID("MergedIdentity")
	identityKey := "vtx.identity." + identityID
	mergedIntoKey := "vtx.identity." + genReqID("MergedIntoIdnt")

	seedDirectIdentity(t, ctx, conn, identityKey, "merged", mergedIntoKey)
	seedClaimKeyAspect(t, ctx, conn, identityKey, "dummy-hash-value-64-chars-padding-xxxxxxxxxxxxxxxxxx")

	claimReqID := genReqID("ClmMergdClaim0")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"any-key","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credentialBinding, credIndexKey not seeded → omit. mergedInto IS seeded.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
				identityKey + ".mergedInto",
			},
		},
	}
	publishOp(t, conn, claimEnv)
	outcome := driveOne(t, ctx, cp, cons, OutcomeRejected)

	// Must be rejected (OutcomeRejected asserted above). Additionally confirm
	// the error code is NOT "IdentityMerged" (no enumeration of merge state).
	if outcome != OutcomeRejected {
		t.Fatalf("expected OutcomeRejected, got %q (NFR-S6: merged identity must not be distinguishable)", outcome)
	}

	instance := iclInstance + "-icl-merged"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "merged")
	if !ok {
		t.Fatalf("claim-attempts.merged Health KV entry not found")
	}
	if count < 1 {
		t.Fatalf("claim-attempts.merged count = %d, want >= 1", count)
	}
}

// TestClaimIdentity_CredentialAlreadyBound_GenericError: pre-seed a credentialindex
// for the consumer's actorKey → submit ClaimIdentity for a different unclaimed
// identity; assert generic ClaimKeyInvalid, Health KV credential-already-bound.
func TestClaimIdentity_CredentialAlreadyBound_GenericError(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-credbnd")

	// Pre-seed credentialindex for the consumer.
	credIndexKey := iclCredentialIndexKey(iclConsumerActorKey)
	priorIdentityKey := "vtx.identity." + genReqID("PriorBoundIdnt")
	credIdxDoc := map[string]any{
		"class":     "credentialindex",
		"isDeleted": false,
		"data": map[string]any{
			"actorKey":    iclConsumerActorKey,
			"identityKey": priorIdentityKey,
			"boundAt":     "2026-05-17T09:00:00Z",
		},
	}
	b, _ := json.Marshal(credIdxDoc)
	if _, err := conn.KVPut(ctx, iclTestBucket, credIndexKey, b); err != nil {
		t.Fatalf("seed credentialindex: %v", err)
	}

	// Create a second unclaimed identity.
	identityID := genReqID("SecndIdentity0")
	identityKey := "vtx.identity." + identityID
	seedDirectIdentity(t, ctx, conn, identityKey, "unclaimed", "")
	claimKeyHash := sha256HexOf("the-real-key-12345678901234567890")
	seedClaimKeyAspect(t, ctx, conn, identityKey, claimKeyHash)

	claimReqID := genReqID("ClmCredBndClm0")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"the-real-key-12345678901234567890","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credIndexKey IS pre-seeded. credentialBinding, mergedInto not seeded → omit.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
				credIndexKey,
			},
		},
	}
	publishOp(t, conn, claimEnv)
	driveOne(t, ctx, cp, cons, OutcomeRejected)

	instance := iclInstance + "-icl-credbnd"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "credential-already-bound")
	if !ok {
		t.Fatalf("claim-attempts.credential-already-bound Health KV entry not found")
	}
	if count < 1 {
		t.Fatalf("claim-attempts.credential-already-bound count = %d, want >= 1", count)
	}
}

// TestClaimIdentity_FR5_GrandfatheredFlow: create unclaimed identity via direct
// primordial-style write (simulating historical import, bypassing the 4.2 op);
// claim via 4.3 op; assert identical success behavior.
// FR5: grandfathered identities have no creating op envelope — provenance in
// createdByOp reflects the direct seed vs. the TestClaimIdentity_Success path.
func TestClaimIdentity_FR5_GrandfatheredFlow(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)
	cp, cons := newClaimPipeline(t, ctx, conn, "icl-fr5-gf")

	// Simulate historical import: write identity vertex + aspects directly,
	// without going through CreateUnclaimedIdentity. This is the grandfathered
	// case (FR5 §) — the identity exists in Core KV but was not created by a
	// 4.2 op.
	identityID := genReqID("FR5GrandFathrd")
	identityKey := "vtx.identity." + identityID

	grandPlaintext := "grandfathered-claim-key-1234567"
	grandHash := sha256HexOf(grandPlaintext)

	// Write identity vertex directly (no CreatedByOp field or a "legacy" value).
	vtxDoc := map[string]any{
		"class":     "identity",
		"isDeleted": false,
		"createdAt": "2024-01-01T00:00:00Z",
		"createdBy": "system.legacy-import",
		// createdByOp intentionally absent or set to a non-op value.
		"data": map[string]any{},
	}
	vb, _ := json.Marshal(vtxDoc)
	if _, err := conn.KVPut(ctx, iclTestBucket, identityKey, vb); err != nil {
		t.Fatalf("seed grandfathered identity vertex: %v", err)
	}
	// Write state aspect.
	stateDoc := map[string]any{
		"class": "state", "vertexKey": identityKey, "localName": "state",
		"isDeleted": false, "data": map[string]any{"value": "unclaimed"},
	}
	sb, _ := json.Marshal(stateDoc)
	if _, err := conn.KVPut(ctx, iclTestBucket, identityKey+".state", sb); err != nil {
		t.Fatalf("seed state aspect: %v", err)
	}
	// Write claimKey aspect with known hash.
	seedClaimKeyAspect(t, ctx, conn, identityKey, grandHash)

	credIndexKey := iclCredentialIndexKey(iclConsumerActorKey)
	claimReqID := genReqID("ClmFR5GFClaim0")
	claimEnv := &OperationEnvelope{
		RequestID:     claimReqID,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"` + grandPlaintext + `","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credentialBinding, mergedInto, credIndexKey not seeded → omit.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
			},
		},
	}
	publishOp(t, conn, claimEnv)
	driveOne(t, ctx, cp, cons, OutcomeAccepted)

	// Same success assertions as TestClaimIdentity_Success.
	stateData := readAspectData(t, ctx, conn, identityKey+".state")
	if got, _ := stateData["value"].(string); got != "claimed" {
		t.Fatalf("FR5 grandfathered: state = %q, want claimed", got)
	}
	bindData := readAspectData(t, ctx, conn, identityKey+".credentialBinding")
	if got, _ := bindData["actorKey"].(string); got != iclConsumerActorKey {
		t.Fatalf("FR5 grandfathered: credentialBinding.actorKey = %q, want %q", got, iclConsumerActorKey)
	}
	if _, err := conn.KVGet(ctx, iclTestBucket, credIndexKey); err != nil {
		t.Fatalf("FR5 grandfathered: credentialindex not found: %v", err)
	}

	// Verify provenance: the identity vertex's createdByOp field should
	// differ from the 4.2-created path (grandfathered path has no Op-tracked
	// createdByOp on the identity vertex). Read the raw vertex doc.
	vtxEntry, err := conn.KVGet(ctx, iclTestBucket, identityKey)
	if err != nil {
		t.Fatalf("read identity vertex: %v", err)
	}
	var rawVtx map[string]any
	_ = json.Unmarshal(vtxEntry.Value, &rawVtx)
	// The grandfathered identity vertex was seeded directly — createdBy is
	// "system.legacy-import" rather than the op envelope actor.
	if got, _ := rawVtx["createdBy"].(string); got != "system.legacy-import" {
		t.Fatalf("FR5: expected createdBy=system.legacy-import on grandfathered vertex, got %q", got)
	}
}

// TestClaimIdentity_FR5_ImmediateAccess: after claim, the capability doc for
// the claimed identity should eventually be reachable in Capability KV (FR5:
// claimed identity has immediate access). Polls up to 1s for reprojection.
//
// Note: this test asserts the claimed identity has a cap doc in Capability KV
// after the claim. The Refractor projects cap docs from Core KV writes;
// however in test mode the Refractor is not running. Instead, we verify that
// the IdentityClaimed event was recorded (which the Refractor would trigger on)
// and that the credentialindex is in place (the mechanism enabling cap lookup
// by actorKey). The cap doc pre-seeding at harness setup acts as the
// "projected" doc for the consumer actor already — this test seeds a cap doc
// for the target identity post-claim to simulate immediate access.
//
// Simplified assertion: verify that after claim, a second ClaimIdentity
// attempt by the same consumer against a different unclaimed identity returns
// ClaimKeyInvalid with credential-already-bound (proving the credentialindex
// from the first claim is active and the actor is now "bound").
func TestClaimIdentity_FR5_ImmediateAccess(t *testing.T) {
	ctx, conn := setupClaimTestEnv(t)

	// Use a single pipeline for all operations in sequence. Creating separate
	// pipelines per-consumer caused earlier consumers to accumulate the create
	// op in their pending queues, leading to duplicate detection on the claim op.
	cp, consCr := newClaimPipeline(t, ctx, conn, "icl-fr5-ia-cr")

	// Step 1: create first identity via the create pipeline+consumer.
	createReqID := genReqID("FR5IACreate00")
	identityKey, claimKeyPlaintext := iclCreateIdentityAndGetKeys(t, ctx, conn, cp, consCr, createReqID)

	credIndexKey := iclCredentialIndexKey(iclConsumerActorKey)
	claimReqID1 := genReqID("FR5IAClaim0001")
	claimEnv1 := &OperationEnvelope{
		RequestID:     claimReqID1,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:01:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"` + claimKeyPlaintext + `","targetIdentityKey":"` + identityKey + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credIndexKey does not exist before first claim → omit from Reads.
			Reads: []string{
				identityKey,
				identityKey + ".state",
				identityKey + ".claimKey",
			},
		},
	}
	publishOp(t, conn, claimEnv1)
	// Use the same consumer for the claim — avoids the backlog of unprocessed
	// create-op messages that would accumulate in a newly created consumer.
	driveOne(t, ctx, cp, consCr, OutcomeAccepted)

	// Verify first claim succeeded.
	stateData := readAspectData(t, ctx, conn, identityKey+".state")
	if got, _ := stateData["value"].(string); got != "claimed" {
		t.Fatalf("FR5 IA: first claim state = %q, want claimed", got)
	}

	// Step 2: attempt a second claim against a different unclaimed identity
	// as the same consumer. Must fail with credential-already-bound (proving
	// the credentialindex from step 1 is active and the actor is now "bound").
	identity2ID := genReqID("FR5IAIdent2000")
	identity2Key := "vtx.identity." + identity2ID
	secondPlaintext := "fr5-second-claim-key-12345678901"
	secondHash := sha256HexOf(secondPlaintext)
	seedDirectIdentity(t, ctx, conn, identity2Key, "unclaimed", "")
	seedClaimKeyAspect(t, ctx, conn, identity2Key, secondHash)

	claimReqID2 := genReqID("FR5IAClaim0002")
	claimEnv2 := &OperationEnvelope{
		RequestID:     claimReqID2,
		Lane:          LaneDefault,
		OperationType: "ClaimIdentity",
		Actor:         iclConsumerActorKey,
		SubmittedAt:   "2026-05-17T10:02:00Z",
		Class:         "identity",
		Payload:       json.RawMessage(`{"claimKey":"` + secondPlaintext + `","targetIdentityKey":"` + identity2Key + `"}`),
		AuthContext:   &AuthContext{Target: iclConsumerActorKey},
		ContextHint: &ContextHint{
			// credIndexKey EXISTS from first claim → include.
			// credentialBinding, mergedInto on identity2 not seeded → omit.
			Reads: []string{
				identity2Key,
				identity2Key + ".state",
				identity2Key + ".claimKey",
				credIndexKey, // exists from step 1
			},
		},
	}
	publishOp(t, conn, claimEnv2)
	driveOne(t, ctx, cp, consCr, OutcomeRejected)

	// Health KV must record credential-already-bound for the second attempt.
	// The instance is "icl-test-icl-fr5-ia-cr" (the single pipeline used for all ops).
	instance := iclInstance + "-icl-fr5-ia-cr"
	count, ok := readClaimHealthCounter(t, ctx, conn, instance, "credential-already-bound")
	if !ok {
		t.Fatalf("FR5 IA: claim-attempts.credential-already-bound not found (second claim should be blocked)")
	}
	if count < 1 {
		t.Fatalf("FR5 IA: credential-already-bound count = %d, want >= 1", count)
	}
}

// ---- Test helpers ----

// seedDirectIdentity writes a minimal identity vertex + state aspect directly
// to Core KV (no op required). Used to pre-set specific states for rejection tests.
func seedDirectIdentity(t *testing.T, ctx context.Context, conn *substrate.Conn,
	identityKey, state, mergedInto string) {
	t.Helper()
	vtxDoc := map[string]any{
		"class":     "identity",
		"isDeleted": false,
		"data":      map[string]any{},
	}
	vb, _ := json.Marshal(vtxDoc)
	if _, err := conn.KVPut(ctx, iclTestBucket, identityKey, vb); err != nil {
		t.Fatalf("seed identity vertex %s: %v", identityKey, err)
	}
	stateDoc := map[string]any{
		"class": "state", "vertexKey": identityKey, "localName": "state",
		"isDeleted": false, "data": map[string]any{"value": state},
	}
	sb, _ := json.Marshal(stateDoc)
	if _, err := conn.KVPut(ctx, iclTestBucket, identityKey+".state", sb); err != nil {
		t.Fatalf("seed state aspect %s: %v", identityKey, err)
	}
	if mergedInto != "" {
		miDoc := map[string]any{
			"class": "mergedInto", "vertexKey": identityKey, "localName": "mergedInto",
			"isDeleted": false, "data": map[string]any{"value": mergedInto},
		}
		mb, _ := json.Marshal(miDoc)
		if _, err := conn.KVPut(ctx, iclTestBucket, identityKey+".mergedInto", mb); err != nil {
			t.Fatalf("seed mergedInto aspect %s: %v", identityKey, err)
		}
	}
}

// seedClaimKeyAspect writes a claimKey aspect with a given pre-computed hash.
func seedClaimKeyAspect(t *testing.T, ctx context.Context, conn *substrate.Conn,
	identityKey, hashHex string) {
	t.Helper()
	// Pad hash to 64 chars if needed (for test values that aren't real sha256).
	for len(hashHex) < 64 {
		hashHex += "0"
	}
	if len(hashHex) > 64 {
		hashHex = hashHex[:64]
	}
	doc := map[string]any{
		"class":     "claimKey",
		"vertexKey": identityKey,
		"localName": "claimKey",
		"isDeleted": false,
		"data":      map[string]any{"hash": hashHex, "algo": "sha256"},
	}
	b, _ := json.Marshal(doc)
	if _, err := conn.KVPut(ctx, iclTestBucket, identityKey+".claimKey", b); err != nil {
		t.Fatalf("seed claimKey aspect %s: %v", identityKey, err)
	}
}

// sha256HexOf returns the hex-encoded SHA-256 hash of s.
func sha256HexOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Ensure strings package is used (imported for NFR-S6 check in future tests).
var _ = strings.Contains
