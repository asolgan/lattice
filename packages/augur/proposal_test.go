// RecordProposal integration tests — the design §5 record-time
// deterministic-validation boundary (the safety core), exercised end-to-end
// through the real Processor.
//
// These tests live in an external test package (augur_test) so they exercise the
// public Lattice surface a real Capability Package sees: seed the kernel, install
// the dependency chain + orchestration-base + augur through the Processor, then
// submit RecordProposal ops and assert outcomes (pending vs invalid vs rejected).
package augur_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/pkgmgr"
	"github.com/asolgan/lattice/internal/processor"
	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/testutil"
	augur "github.com/asolgan/lattice/packages/augur"
	orchestrationbase "github.com/asolgan/lattice/packages/orchestration-base"
)

const (
	apStaffActorID  = "BBstaffActHJKMNPQRST"
	apStaffActorKey = "vtx.identity." + apStaffActorID
	apStaffCapKey   = "cap.identity." + apStaffActorID
)

// staffCapDoc grants the staff actor RecordProposal (scope any) — the bridge
// replyOp's authority, modeled here as an operator-equivalent staff actor.
func staffCapDoc() *processor.CapabilityDoc {
	now := time.Now().UTC()
	return &processor.CapabilityDoc{
		Key:                    apStaffCapKey,
		Actor:                  apStaffActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{apStaffActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []processor.PlatformPermission{
			{OperationType: "RecordProposal", Scope: "any"},
		},
		ServiceAccess:   []processor.ServiceAccessEntry{},
		EphemeralGrants: []processor.EphemeralGrant{},
		Roles:           []string{bootstrap.RoleOperatorKey},
	}
}

func setupAugurEnv(t *testing.T) (context.Context, *substrate.Conn) {
	t.Helper()
	ctx, conn := testutil.SetupPackageTestEnv(t) // installs rbac+identity+hygiene
	installPkg(t, ctx, conn, orchestrationbase.Package)
	installPkg(t, ctx, conn, augur.Package)
	testutil.SeedCapDoc(t, ctx, conn, staffCapDoc())
	return ctx, conn
}

func installPkg(t *testing.T, ctx context.Context, conn *substrate.Conn, pkg pkgmgr.Definition) {
	t.Helper()
	stop := testutil.RunMetaInstallPipeline(t, ctx, conn)
	defer stop()
	inst := pkgmgr.NewInstaller(conn, bootstrap.BootstrapIdentityKey)
	inst.RoleIDs = map[string]string{"operator": bootstrap.RoleOperatorID}
	if _, err := inst.Install(ctx, pkg); err != nil {
		t.Fatalf("install %s: %v", pkg.Name, err)
	}
}

func newProposalPipeline(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) (*processor.CommitPath, jetstream.Consumer) {
	t.Helper()
	return testutil.CapabilityPipeline(t, ctx, conn, testutil.PipelineConfig{
		Durable:  durable,
		Instance: "ap-" + durable,
	})
}

// proposalIDFromRequestID predicts the proposal NanoID the DDL's first
// nanoid.new() mints (deterministic from the requestId, same as the task DDL).
func proposalIDFromRequestID(requestID string) string {
	seed := processor.SeedFromRequestID(requestID)
	pcg := rand.NewPCG(seed[0], seed[1])
	return processor.DeterministicNanoID(pcg, substrate.NanoIDLength)
}

func seedVertex(t *testing.T, ctx context.Context, conn *substrate.Conn, key, class string, data map[string]any) {
	t.Helper()
	if data == nil {
		data = map[string]any{}
	}
	doc := map[string]any{"class": class, "isDeleted": false, "data": data}
	b, _ := json.Marshal(doc)
	if _, err := conn.KVPut(ctx, testutil.HarnessCoreBucket, key, b); err != nil {
		t.Fatalf("seed vertex %s: %v", key, err)
	}
}

func readDoc(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) map[string]any {
	t.Helper()
	entry, err := conn.KVGet(ctx, testutil.HarnessCoreBucket, key)
	if err != nil {
		t.Fatalf("KVGet %s: %v", key, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", key, err)
	}
	return doc
}

// seedEscalation seeds the two RecordProposal link endpoints (the weaver target
// meta + the candidate entity) and returns their keys.
func seedEscalation(t *testing.T, ctx context.Context, conn *substrate.Conn) (targetKey, entityKey string) {
	t.Helper()
	targetKey = "vtx.meta.BBtargetMtHJKMNPQRST"
	entityKey = "vtx.leaseapp.BBcandidateHJKMNPQRS"
	seedVertex(t, ctx, conn, targetKey, "meta", map[string]any{"canonicalName": "leaseapprovalTarget"})
	seedVertex(t, ctx, conn, entityKey, "leaseapp", map[string]any{"state": "pending"})
	return targetKey, entityKey
}

func recordProposalEnv(reqID, targetKey, entityKey, action string, confidence float64, params map[string]any) *processor.OperationEnvelope {
	payload := map[string]any{
		"targetId":   targetKey,
		"entityId":   entityKey,
		"gapColumn":  "missing_approval",
		"trigger":    "unplannable",
		"action":     action,
		"confidence": confidence,
	}
	if params != nil {
		payload["params"] = params
	}
	b, _ := json.Marshal(payload)
	return &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "RecordProposal",
		Actor:         apStaffActorKey,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "augurproposal",
		Payload:       json.RawMessage(b),
		ContextHint:   &processor.ContextHint{Reads: []string{targetKey, entityKey}},
	}
}

// reviewState reads vtx.augurproposal.<id>.review.data.state.
func reviewState(t *testing.T, ctx context.Context, conn *substrate.Conn, proposalKey string) string {
	t.Helper()
	doc := readDoc(t, ctx, conn, proposalKey+".review")
	data, _ := doc["data"].(map[string]any)
	s, _ := data["state"].(string)
	return s
}

// TestRecordProposal_ValidPending: a well-formed in-vocabulary proposal whose
// proposed scope matches the escalated candidate is stored review.state=pending
// (dispatchable), with the proposal vertex, the .gap/.proposed/.review aspects,
// and the forCandidate/forTarget links committed atomically.
func TestRecordProposal_ValidPending(t *testing.T) {
	ctx, conn := setupAugurEnv(t)
	cp, cons := newProposalPipeline(t, ctx, conn, "ap-pending")
	targetKey, entityKey := seedEscalation(t, ctx, conn)

	reqID := testutil.GenReqID("APPending0001")
	proposalKey := "vtx.augurproposal." + proposalIDFromRequestID(reqID)
	env := recordProposalEnv(reqID, targetKey, entityKey, "assignTask", 0.82,
		map[string]any{"scopedTo": entityKey, "forOperation": "ApproveLeaseApplication"})
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	if got := reviewState(t, ctx, conn, proposalKey); got != "pending" {
		t.Fatalf("review.state = %q, want pending", got)
	}
	// Root data is minimal (D5).
	root := readDoc(t, ctx, conn, proposalKey)
	if data, _ := root["data"].(map[string]any); len(data) != 0 {
		t.Fatalf("proposal root data must be {} (D5); got %v", data)
	}
	// The .gap aspect carries the escalation context.
	gap := readDoc(t, ctx, conn, proposalKey+".gap")
	gd, _ := gap["data"].(map[string]any)
	if got, _ := gd["gapColumn"].(string); got != "missing_approval" {
		t.Fatalf(".gap.gapColumn = %q, want missing_approval", got)
	}
	// Both links: proposal is the source.
	pid := proposalIDFromRequestID(reqID)
	forCand := "lnk.augurproposal." + pid + ".forCandidate.leaseapp.BBcandidateHJKMNPQRS"
	forTarget := "lnk.augurproposal." + pid + ".forTarget.meta.BBtargetMtHJKMNPQRST"
	for name, lnk := range map[string]string{"forCandidate": forCand, "forTarget": forTarget} {
		doc := readDoc(t, ctx, conn, lnk)
		if got, _ := doc["sourceVertex"].(string); got != proposalKey {
			t.Fatalf("%s link sourceVertex = %q, want %q (proposal is source)", name, got, proposalKey)
		}
	}
}

// TestRecordProposal_BadAction_Invalid: an action outside the allowed escalation
// vocabulary stores the proposal review.state=invalid (auditable, never
// dispatchable) — the op still ACCEPTS (the proposal is recorded), but the
// verdict is invalid.
func TestRecordProposal_BadAction_Invalid(t *testing.T) {
	ctx, conn := setupAugurEnv(t)
	cp, cons := newProposalPipeline(t, ctx, conn, "ap-badaction")
	targetKey, entityKey := seedEscalation(t, ctx, conn)

	reqID := testutil.GenReqID("APBadAct00001")
	proposalKey := "vtx.augurproposal." + proposalIDFromRequestID(reqID)
	env := recordProposalEnv(reqID, targetKey, entityKey, "DROP TABLE", 0.99, nil)
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	if got := reviewState(t, ctx, conn, proposalKey); got != "invalid" {
		t.Fatalf("review.state = %q, want invalid", got)
	}
}

// TestRecordProposal_ScopeEscape_Invalid: a proposed action whose entity-naming
// param references a candidate OTHER than the escalated one is stored invalid
// (the §5 scope-escape check — the model cannot propose acting on a different
// entity than the gap it reasoned about).
func TestRecordProposal_ScopeEscape_Invalid(t *testing.T) {
	ctx, conn := setupAugurEnv(t)
	cp, cons := newProposalPipeline(t, ctx, conn, "ap-escape")
	targetKey, entityKey := seedEscalation(t, ctx, conn)

	reqID := testutil.GenReqID("APEscape00001")
	proposalKey := "vtx.augurproposal." + proposalIDFromRequestID(reqID)
	env := recordProposalEnv(reqID, targetKey, entityKey, "directOp", 0.95,
		map[string]any{"scopedTo": "vtx.leaseapp.BBotherEntyHJKMNPQRS"})
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	if got := reviewState(t, ctx, conn, proposalKey); got != "invalid" {
		t.Fatalf("review.state = %q, want invalid (scope escape)", got)
	}
}

// TestRecordProposal_ConfidenceOutOfRange_Invalid: a confidence outside [0,1]
// stores the proposal invalid.
func TestRecordProposal_ConfidenceOutOfRange_Invalid(t *testing.T) {
	ctx, conn := setupAugurEnv(t)
	cp, cons := newProposalPipeline(t, ctx, conn, "ap-conf")
	targetKey, entityKey := seedEscalation(t, ctx, conn)

	reqID := testutil.GenReqID("APConf000001")
	proposalKey := "vtx.augurproposal." + proposalIDFromRequestID(reqID)
	env := recordProposalEnv(reqID, targetKey, entityKey, "assignTask", 1.5,
		map[string]any{"scopedTo": entityKey})
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	if got := reviewState(t, ctx, conn, proposalKey); got != "invalid" {
		t.Fatalf("review.state = %q, want invalid (confidence out of range)", got)
	}
}

// TestRecordProposal_AbsentCandidate_Rejected: the no-orphan invariant — a
// proposal pointing at a non-existent candidate is never committed (the op is
// rejected with a structured ScriptError, distinct from the invalid verdict).
func TestRecordProposal_AbsentCandidate_Rejected(t *testing.T) {
	ctx, conn := setupAugurEnv(t)
	cp, cons := newProposalPipeline(t, ctx, conn, "ap-absent")
	targetKey, _ := seedEscalation(t, ctx, conn)
	missingEntity := "vtx.leaseapp.BBmissingEnHJKMNPQRS"

	reqID := testutil.GenReqID("APAbsent00001")
	env := recordProposalEnv(reqID, targetKey, missingEntity, "assignTask", 0.8,
		map[string]any{"scopedTo": missingEntity})
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}
