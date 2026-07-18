// cafe-domain integration tests through the real install + Processor
// pipeline. External test package (cafedomain_test) so they exercise the
// public Lattice surface: seed the kernel, install rbac + identity + hygiene
// + orchestration-base + service-domain + lease-signing + cafe-ledger +
// cafe-domain through the Processor, then submit the ops and assert the
// committed Core-KV shape + the emitted events.
package cafedomain_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/pkgmgr"
	"github.com/asolgan/lattice/internal/processor"
	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/testutil"
	cafedomain "github.com/asolgan/lattice/packages/cafe-domain"
	cafeledger "github.com/asolgan/lattice/packages/cafe-ledger"
	leasesigning "github.com/asolgan/lattice/packages/lease-signing"
	orchestrationbase "github.com/asolgan/lattice/packages/orchestration-base"
	servicedomain "github.com/asolgan/lattice/packages/service-domain"
)

const (
	domainActorID  = "BBCAFEDMANACTHJKMNPQ"
	domainActorKey = "vtx.identity." + domainActorID
	domainCapKey   = "cap.identity." + domainActorID

	domainConsumerRoleID = "BBConsumerRoZeCafeDo"

	// domainConsumerID stands in for identity-domain's real `consumer` role
	// grant flow (mirrors wellness-domain's domainConsumerID) — the
	// self-service caller's own identity, distinct from the operator actor
	// above.
	domainConsumerID  = "BBCAFEDMANCQNSHJKMNP"
	domainConsumerKey = "vtx.identity." + domainConsumerID
	domainConsumerCap = "cap.identity." + domainConsumerID
)

// domainConsumerCapDoc grants the consumer role's scope=self OpenTab /
// Settle permissions — the real-actor-write-auth-e2e self-service caller,
// mirrors wellness-domain's domainConsumerCapDoc.
func domainConsumerCapDoc() *processor.CapabilityDoc {
	now := time.Now().UTC()
	return &processor.CapabilityDoc{
		Key:                    domainConsumerCap,
		Actor:                  domainConsumerKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{domainConsumerKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []processor.PlatformPermission{
			{OperationType: "OpenTab", Scope: "self"},
			{OperationType: "Charge", Scope: "self"},
			{OperationType: "Settle", Scope: "self"},
		},
		ServiceAccess:   []processor.ServiceAccessEntry{},
		EphemeralGrants: []processor.EphemeralGrant{},
		Roles:           []string{"vtx.role.consumer"},
	}
}

func domainCapDoc() *processor.CapabilityDoc {
	now := time.Now().UTC()
	return &processor.CapabilityDoc{
		Key:                    domainCapKey,
		Actor:                  domainActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{domainActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions: []processor.PlatformPermission{
			{OperationType: "CreateLeaseApplication", Scope: "any"},
			{OperationType: "CreateAccount", Scope: "any"},
			{OperationType: "DebitAccount", Scope: "any"},
			{OperationType: "CreditAccount", Scope: "any"},
			{OperationType: "OpenTab", Scope: "any"},
			{OperationType: "Charge", Scope: "any"},
			{OperationType: "Settle", Scope: "any"},
			{OperationType: "CreateMenuItem", Scope: "any"},
			{OperationType: "RetireMenuItem", Scope: "any"},
		},
		ServiceAccess:   []processor.ServiceAccessEntry{},
		EphemeralGrants: []processor.EphemeralGrant{},
		Roles:           []string{bootstrap.RoleOperatorKey},
	}
}

func setupDomainEnv(t *testing.T) (context.Context, *substrate.Conn) {
	t.Helper()
	ctx, conn := testutil.SetupPackageTestEnv(t) // rbac + identity + hygiene
	stop := testutil.RunMetaInstallPipeline(t, ctx, conn)
	defer stop()
	inst := pkgmgr.NewInstaller(conn, bootstrap.BootstrapIdentityKey)
	inst.RoleIDs = map[string]string{"operator": bootstrap.RoleOperatorID, "consumer": domainConsumerRoleID}
	if _, err := inst.Install(ctx, orchestrationbase.Package); err != nil {
		t.Fatalf("install orchestration-base: %v", err)
	}
	if _, err := inst.Install(ctx, servicedomain.Package); err != nil {
		t.Fatalf("install service-domain: %v", err)
	}
	if _, err := inst.Install(ctx, leasesigning.Package); err != nil {
		t.Fatalf("install lease-signing: %v", err)
	}
	if _, err := inst.Install(ctx, cafeledger.Package); err != nil {
		t.Fatalf("install cafe-ledger: %v", err)
	}
	if _, err := inst.Install(ctx, cafedomain.Package); err != nil {
		t.Fatalf("install cafe-domain: %v", err)
	}
	testutil.SeedCapDoc(t, ctx, conn, domainCapDoc())
	return ctx, conn
}

func newDomainPipeline(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) (*processor.CommitPath, jetstream.Consumer) {
	t.Helper()
	return testutil.CapabilityPipeline(t, ctx, conn, testutil.PipelineConfig{
		Durable:  durable,
		Instance: "cd-" + durable,
	})
}

func nanoIDFromRequestID(requestID string) string {
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

func keyExists(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) bool {
	t.Helper()
	entry, err := conn.KVGet(ctx, testutil.HarnessCoreBucket, key)
	if err != nil {
		return false
	}
	var doc map[string]any
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		return false
	}
	if del, _ := doc["isDeleted"].(bool); del {
		return false
	}
	return true
}

func seedLease(t *testing.T, ctx context.Context, conn *substrate.Conn, id string) string {
	t.Helper()
	key := "vtx.leaseapp." + id
	seedVertex(t, ctx, conn, key, "leaseapp", map[string]any{})
	return key
}

func seedIdentity(t *testing.T, ctx context.Context, conn *substrate.Conn, id string) string {
	t.Helper()
	key := "vtx.identity." + id
	seedVertex(t, ctx, conn, key, "identity", map[string]any{})
	return key
}

func seedLink(t *testing.T, ctx context.Context, conn *substrate.Conn, key, source, target, class, localName string) {
	t.Helper()
	doc := map[string]any{
		"class": class, "isDeleted": false,
		"sourceVertex": source, "targetVertex": target,
		"localName": localName, "data": map[string]any{},
	}
	b, _ := json.Marshal(doc)
	if _, err := conn.KVPut(ctx, testutil.HarnessCoreBucket, key, b); err != nil {
		t.Fatalf("seed link %s: %v", key, err)
	}
}

// seedLeaseWithApplicant seeds a leaseapp vertex + its applicationFor link
// to applicantID — the residency check OpenTab/Settle's self-scope guard
// reads (mirrors wellness-domain's seedLease(..., applicantID, ...)).
func seedLeaseWithApplicant(t *testing.T, ctx context.Context, conn *substrate.Conn, leaseID, applicantID string) string {
	t.Helper()
	key := "vtx.leaseapp." + leaseID
	seedVertex(t, ctx, conn, key, "leaseapp", map[string]any{})
	lnk := "lnk.leaseapp." + leaseID + ".applicationFor.identity." + applicantID
	seedLink(t, ctx, conn, lnk, key, "vtx.identity."+applicantID, "applicationFor", "applicationFor")
	return key
}

// openTab submits OpenTab{leaseAppKey}, declaring the per-lease
// cafeOpenTab guard in OptionalReads (Contract #2 §2.5 class-(d) — the
// guard legitimately may or may not exist yet), and returns the tab key.
// The caller drives the expected outcome (a lease with an already-open tab
// must reject).
func openTabExpect(t *testing.T, ctx context.Context, conn *substrate.Conn, cp *processor.CommitPath, cons jetstream.Consumer, label, leaseAppKey string, outcome processor.MessageOutcome) string {
	t.Helper()
	reqID := testutil.GenReqID(label)
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "OpenTab",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T12:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"leaseAppKey":"` + leaseAppKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{leaseAppKey},
			OptionalReads: []string{leaseAppKey + ".cafeOpenTab"},
		},
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, outcome)
	return "vtx.tab." + nanoIDFromRequestID(reqID)
}

// openTab submits OpenTab{leaseAppKey} expecting acceptance and returns the
// tab key.
func openTab(t *testing.T, ctx context.Context, conn *substrate.Conn, cp *processor.CommitPath, cons jetstream.Consumer, label, leaseAppKey string) string {
	t.Helper()
	return openTabExpect(t, ctx, conn, cp, cons, label, leaseAppKey, processor.OutcomeAccepted)
}

func TestOpenTab_MintsTabOpenForLease(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "opentab")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNLEASEHJKMNP")
	leaseID := "BBCAFEDMNLEASEHJKMNP"

	tabKey := openTab(t, ctx, conn, cp, cons, "cdopentab000000001", leaseKey)
	tabID := tabKey[len("vtx.tab."):]

	tabDoc := readDoc(t, ctx, conn, tabKey)
	if d, _ := tabDoc["data"].(map[string]any); len(d) != 0 {
		t.Fatalf("tab root data must stay minimal ({}) after OpenTab, got %v", d)
	}

	statusDoc := readDoc(t, ctx, conn, tabKey+".status")
	statusData, _ := statusDoc["data"].(map[string]any)
	if got, _ := statusData["value"].(string); got != "open" {
		t.Fatalf("status.value = %q, want open", got)
	}
	if got, _ := statusData["totalCents"].(float64); got != 0 {
		t.Fatalf("status.totalCents = %v, want 0", got)
	}
	if got, _ := statusData["leaseAppKey"].(string); got != leaseKey {
		t.Fatalf("status.leaseAppKey = %q, want %q", got, leaseKey)
	}

	openForLnk := "lnk.tab." + tabID + ".openFor.leaseapp." + leaseID
	if !keyExists(t, ctx, conn, openForLnk) {
		t.Fatalf("openFor link must exist: %s", openForLnk)
	}
}

func TestOpenTab_UnknownLease(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "unknownlease")

	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdopenunknown0000001"),
		Lane:          processor.LaneDefault,
		OperationType: "OpenTab",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T12:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"leaseAppKey":"vtx.leaseapp.BBABSENTLEASEHJKMNPQ"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{"vtx.leaseapp.BBABSENTLEASEHJKMNPQ"}},
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}

func TestCharge_AccumulatesTotalCents(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "chargeaccum")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNCHGLEASEHJK")
	tabKey := openTab(t, ctx, conn, cp, cons, "cdopentabchg00000001", leaseKey)

	charge := func(reqLabel string, amountCents int) {
		reqID := testutil.GenReqID(reqLabel)
		env := &processor.OperationEnvelope{
			RequestID:     reqID,
			Lane:          processor.LaneDefault,
			OperationType: "Charge",
			Actor:         domainActorKey,
			SubmittedAt:   "2026-07-07T12:05:00Z",
			Class:         "tab",
			Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","amountCents":` + strconv.Itoa(amountCents) + `}`),
			ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
		}
		testutil.PublishOp(t, conn, env)
		testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)
	}
	charge("cdchargeone00000001", 450)
	charge("cdchargetwo00000001", 300)

	statusDoc := readDoc(t, ctx, conn, tabKey+".status")
	statusData, _ := statusDoc["data"].(map[string]any)
	if got, _ := statusData["totalCents"].(float64); got != 750 {
		t.Fatalf("status.totalCents = %v, want 750 (450+300)", got)
	}
	if got, _ := statusData["value"].(string); got != "open" {
		t.Fatalf("status.value = %q, want open (still charging)", got)
	}
}

func TestCharge_RejectsNonPositiveAmount(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "chargebad")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNBADLEASEHJK")
	tabKey := openTab(t, ctx, conn, cp, cons, "cdopentabbad00000001", leaseKey)

	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdchargebadamt000001"),
		Lane:          processor.LaneDefault,
		OperationType: "Charge",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T12:05:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","amountCents":0}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}

func TestSettle_ClosesTabFreezesTotal(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "settle")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNSETLEASEHJK")
	tabKey := openTab(t, ctx, conn, cp, cons, "cdopentabset00000001", leaseKey)

	chargeReqID := testutil.GenReqID("cdchargesettle000001")
	chargeEnv := &processor.OperationEnvelope{
		RequestID:     chargeReqID,
		Lane:          processor.LaneDefault,
		OperationType: "Charge",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T12:05:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","amountCents":1200}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, chargeEnv)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	settleEnv := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdsettletab000000001"),
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T13:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, settleEnv)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	statusDoc := readDoc(t, ctx, conn, tabKey+".status")
	statusData, _ := statusDoc["data"].(map[string]any)
	if got, _ := statusData["value"].(string); got != "settled" {
		t.Fatalf("status.value = %q, want settled", got)
	}
	if got, _ := statusData["totalCents"].(float64); got != 1200 {
		t.Fatalf("status.totalCents = %v, want 1200 (frozen)", got)
	}
	if _, ok := statusData["settledAt"]; !ok {
		t.Fatalf("status.settledAt must be stamped on settle")
	}
}

func TestSettle_RejectsDoubleSettle(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "doublesettle")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNDBLLEASEHJK")
	tabKey := openTab(t, ctx, conn, cp, cons, "cdopentabdbl00000001", leaseKey)

	settleOnce := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdsettledbl000000001"),
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T13:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, settleOnce)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	settleTwice := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdsettledbl000000002"),
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T13:05:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, settleTwice)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}

func TestCharge_RejectsAfterSettle(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "chargeaftersettle")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNCASLEASEHJK")
	tabKey := openTab(t, ctx, conn, cp, cons, "cdopentabcas00000001", leaseKey)

	settleEnv := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdsettlecas000000001"),
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T13:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, settleEnv)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	chargeEnv := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdchargecas000000001"),
		Lane:          processor.LaneDefault,
		OperationType: "Charge",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T13:05:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","amountCents":500}`),
		ContextHint:   &processor.ContextHint{Reads: []string{tabKey, tabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, chargeEnv)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}

// TestOpenTab_RejectsSecondConcurrentTab proves the fix for the no-guard
// bug: a lease with an already-open tab must reject a second OpenTab
// (verticals.md — "Café tab: no guard against a 2nd concurrent open tab per
// lease").
func TestOpenTab_RejectsSecondConcurrentTab(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "opentabguard")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNGRDLEASEHJK")
	firstTabKey := openTab(t, ctx, conn, cp, cons, "cdopentabgrd00000001", leaseKey)

	secondTabKey := openTabExpect(t, ctx, conn, cp, cons, "cdopentabgrd00000002", leaseKey, processor.OutcomeRejected)

	guardDoc := readDoc(t, ctx, conn, leaseKey+".cafeOpenTab")
	guardData, _ := guardDoc["data"].(map[string]any)
	if got, _ := guardData["tabKey"].(string); got != firstTabKey {
		t.Fatalf("guard tabKey = %q, want %q (first tab, unaffected by rejected second)", got, firstTabKey)
	}
	if keyExists(t, ctx, conn, secondTabKey) {
		t.Fatalf("rejected second OpenTab must not have minted a tab: %s", secondTabKey)
	}
}

// TestOpenTab_AllowsReopenAfterSettle proves the guard is released (not a
// one-time-forever guard like cafe-ledger's account guard): once the first
// tab is settled, the same lease can open a new one.
func TestOpenTab_AllowsReopenAfterSettle(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "opentabreopen")

	leaseKey := seedLease(t, ctx, conn, "BBCAFEDMNRPNLEASEHJK")
	firstTabKey := openTab(t, ctx, conn, cp, cons, "cdopentabrpn00000001", leaseKey)

	settleEnv := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdsettlerpn000000001"),
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-07T13:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + firstTabKey + `"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{firstTabKey, firstTabKey + ".status"}},
	}
	testutil.PublishOp(t, conn, settleEnv)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	if keyExists(t, ctx, conn, leaseKey+".cafeOpenTab") {
		t.Fatalf("guard must be tombstoned once its tab is settled")
	}

	secondTabKey := openTab(t, ctx, conn, cp, cons, "cdopentabrpn00000002", leaseKey)
	if secondTabKey == firstTabKey {
		t.Fatalf("second tab must be a distinct vertex")
	}

	guardDoc := readDoc(t, ctx, conn, leaseKey+".cafeOpenTab")
	guardData, _ := guardDoc["data"].(map[string]any)
	if got, _ := guardData["tabKey"].(string); got != secondTabKey {
		t.Fatalf("guard tabKey = %q, want %q (revived for the second tab)", got, secondTabKey)
	}
}

// TestOpenTab_ConsumerSelfScope_Allowed proves a real resident, holding only
// the consumer scope=self grant, can open a house tab for THEIR OWN lease:
// payload.leaseAppKey names a lease identified-by their own identity and
// authContext.target matches it.
func TestOpenTab_ConsumerSelfScope_Allowed(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "opentabselfok")

	seedIdentity(t, ctx, conn, domainConsumerID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNSLFQKLEASEH", domainConsumerID)
	applicationForLnk := "lnk.leaseapp.BBCAFEDMNSLFQKLEASEH.applicationFor.identity." + domainConsumerID

	reqID := testutil.GenReqID("cdselfopentab0000001")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "OpenTab",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-07T12:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"leaseAppKey":"` + leaseKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{leaseKey},
			OptionalReads: []string{leaseKey + ".cafeOpenTab", applicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeAccepted {
		t.Fatalf("self-service OpenTab outcome = %v, want Accepted", outcome)
	}
}

// TestOpenTab_ConsumerSelfScope_RejectedForOthersLease proves the Starlark
// guard closes the gap step 3 leaves open: step 3's scope=self only checks
// authContext.target == actor, never looks at payload.leaseAppKey. A
// consumer satisfying that check but naming a lease identified-by a
// DIFFERENT identity must be rejected — self-service never lets one
// resident open a tab against another's lease.
func TestOpenTab_ConsumerSelfScope_RejectedForOthersLease(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "opentabselfother")

	seedIdentity(t, ctx, conn, domainConsumerID)
	otherApplicantID := "BBCAFEDMQTHERAPPHJKM"
	seedIdentity(t, ctx, conn, otherApplicantID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNSLFQTHLEASE", otherApplicantID)
	// The consumer declares the applicationFor link for THEIR OWN identity —
	// which does not exist for this lease (it belongs to otherApplicantID) —
	// so the declared read simply comes back absent, failing closed.
	wrongApplicationForLnk := "lnk.leaseapp.BBCAFEDMNSLFQTHLEASE.applicationFor.identity." + domainConsumerID

	reqID := testutil.GenReqID("cdselfopentab0000002")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "OpenTab",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-07T12:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"leaseAppKey":"` + leaseKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{leaseKey},
			OptionalReads: []string{leaseKey + ".cafeOpenTab", wrongApplicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeRejected {
		t.Fatalf("self-service OpenTab for another's lease outcome = %v, want Rejected (AuthDenied)", outcome)
	}
}

// TestSettle_ConsumerSelfScope_Allowed proves a real resident can settle
// THEIR OWN open tab: the tab's leaseAppKey resolves (via applicationFor) to
// the caller's own authContext.target identity.
func TestSettle_ConsumerSelfScope_Allowed(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "settleselfok")

	seedIdentity(t, ctx, conn, domainConsumerID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNSTLQKLEASEH", domainConsumerID)
	tabKey := openTab(t, ctx, conn, cp, cons, "cdselfsettlesetup0001", leaseKey)
	applicationForLnk := "lnk.leaseapp.BBCAFEDMNSTLQKLEASEH.applicationFor.identity." + domainConsumerID

	reqID := testutil.GenReqID("cdselfsettletab000001")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-07T13:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{tabKey, tabKey + ".status"},
			OptionalReads: []string{applicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeAccepted {
		t.Fatalf("self-service Settle outcome = %v, want Accepted", outcome)
	}
}

// TestSettle_ConsumerSelfScope_RejectedForOthersTab proves a consumer
// satisfying step 3 (authContext.target == actor) but naming a tab whose
// lease is NOT their own is rejected — self-service never lets one resident
// settle another's tab.
func TestSettle_ConsumerSelfScope_RejectedForOthersTab(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "settleselfother")

	seedIdentity(t, ctx, conn, domainConsumerID)
	otherApplicantID := "BBCAFEDMQTHERTABHJKM"
	seedIdentity(t, ctx, conn, otherApplicantID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNSTLQTHLEASE", otherApplicantID)
	tabKey := openTab(t, ctx, conn, cp, cons, "cdselfsettleoth0000001", leaseKey)
	wrongApplicationForLnk := "lnk.leaseapp.BBCAFEDMNSTLQTHLEASE.applicationFor.identity." + domainConsumerID

	reqID := testutil.GenReqID("cdselfsettletab000002")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "Settle",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-07T13:00:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{tabKey, tabKey + ".status"},
			OptionalReads: []string{wrongApplicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeRejected {
		t.Fatalf("self-service Settle of another's tab outcome = %v, want Rejected (AuthDenied)", outcome)
	}
}

// createMenuItem submits CreateMenuItem{name, priceCents} expecting
// acceptance and returns the new item's key.
func createMenuItem(t *testing.T, ctx context.Context, conn *substrate.Conn, cp *processor.CommitPath, cons jetstream.Consumer, label, name string, priceCents int) string {
	t.Helper()
	reqID := testutil.GenReqID(label)
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "CreateMenuItem",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-18T12:00:00Z",
		Class:         "menuitem",
		Payload:       json.RawMessage(`{"name":"` + name + `","priceCents":` + strconv.Itoa(priceCents) + `}`),
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)
	return "vtx.menuitem." + nanoIDFromRequestID(reqID)
}

func TestCreateMenuItem_MintsItemAndPriceAspect(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "createmenuitem")

	itemKey := createMenuItem(t, ctx, conn, cp, cons, "cdcreatemenuitem0001", "Latte", 450)

	itemDoc := readDoc(t, ctx, conn, itemKey)
	if d, _ := itemDoc["data"].(map[string]any); len(d) != 0 {
		t.Fatalf("menuItem root data must stay minimal ({}) after CreateMenuItem, got %v", d)
	}
	priceDoc := readDoc(t, ctx, conn, itemKey+".price")
	priceData, _ := priceDoc["data"].(map[string]any)
	if got, _ := priceData["name"].(string); got != "Latte" {
		t.Fatalf("price.name = %q, want Latte", got)
	}
	if got, _ := priceData["priceCents"].(float64); got != 450 {
		t.Fatalf("price.priceCents = %v, want 450", got)
	}
}

func TestCreateMenuItem_RejectsNonPositivePrice(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "createmenuitembad")

	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdcreatemenuitembad1"),
		Lane:          processor.LaneDefault,
		OperationType: "CreateMenuItem",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-18T12:00:00Z",
		Class:         "menuitem",
		Payload:       json.RawMessage(`{"name":"Free Sample","priceCents":0}`),
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}

func TestRetireMenuItem_Tombstones(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	cp, cons := newDomainPipeline(t, ctx, conn, "retiremenuitem")

	itemKey := createMenuItem(t, ctx, conn, cp, cons, "cdretiremenuitemsu01", "Croissant", 350)

	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdretiremenuitem0001"),
		Lane:          processor.LaneDefault,
		OperationType: "RetireMenuItem",
		Actor:         domainActorKey,
		SubmittedAt:   "2026-07-18T12:05:00Z",
		Class:         "menuitem",
		Payload:       json.RawMessage(`{"menuItemKey":"` + itemKey + `"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{itemKey}},
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	if keyExists(t, ctx, conn, itemKey) {
		t.Fatalf("RetireMenuItem must tombstone the item: %s", itemKey)
	}
}

// TestCharge_SelfOrder_DerivesAmountFromMenuItem proves a resident's
// self-service Charge binds against the menuItem catalog: amountCents comes
// from the referenced item's own .price.priceCents (450), never from any
// caller-supplied amountCents (the payload carries none here).
func TestCharge_SelfOrder_DerivesAmountFromMenuItem(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "chargeselfok")

	seedIdentity(t, ctx, conn, domainConsumerID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNCHGQKLEASEH", domainConsumerID)
	tabKey := openTab(t, ctx, conn, cp, cons, "cdselfchargesetup001", leaseKey)
	itemKey := createMenuItem(t, ctx, conn, cp, cons, "cdselfchargemenu0001", "Latte", 450)
	applicationForLnk := "lnk.leaseapp.BBCAFEDMNCHGQKLEASEH.applicationFor.identity." + domainConsumerID

	reqID := testutil.GenReqID("cdselfchargetab000001")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "Charge",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-18T12:10:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","menuItemKey":"` + itemKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{tabKey, tabKey + ".status", itemKey, itemKey + ".price"},
			OptionalReads: []string{applicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeAccepted {
		t.Fatalf("self-order Charge outcome = %v, want Accepted", outcome)
	}

	statusDoc := readDoc(t, ctx, conn, tabKey+".status")
	statusData, _ := statusDoc["data"].(map[string]any)
	if got, _ := statusData["totalCents"].(float64); got != 450 {
		t.Fatalf("status.totalCents = %v, want 450 (derived from the menu item's price)", got)
	}
}

// TestCharge_SelfOrder_RejectedForOthersTab proves a consumer satisfying
// step 3 (authContext.target == actor) but naming a tab whose lease is NOT
// their own is rejected — self-order never lets one resident charge
// another's tab.
func TestCharge_SelfOrder_RejectedForOthersTab(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "chargeselfother")

	seedIdentity(t, ctx, conn, domainConsumerID)
	otherApplicantID := "BBCAFEDMQTHERCHGHJKM"
	seedIdentity(t, ctx, conn, otherApplicantID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNCHGQTHLEASE", otherApplicantID)
	tabKey := openTab(t, ctx, conn, cp, cons, "cdselfchargeoth00001", leaseKey)
	itemKey := createMenuItem(t, ctx, conn, cp, cons, "cdselfchargeothmenu1", "Latte", 450)
	wrongApplicationForLnk := "lnk.leaseapp.BBCAFEDMNCHGQTHLEASE.applicationFor.identity." + domainConsumerID

	reqID := testutil.GenReqID("cdselfchargetab000002")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "Charge",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-18T12:10:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","menuItemKey":"` + itemKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{tabKey, tabKey + ".status", itemKey, itemKey + ".price"},
			OptionalReads: []string{wrongApplicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeRejected {
		t.Fatalf("self-order Charge of another's tab outcome = %v, want Rejected (AuthDenied)", outcome)
	}
}

// TestCharge_SelfOrder_UnknownMenuItemRejected proves a self-service Charge
// naming an absent menuItemKey is rejected, not silently zero-priced.
func TestCharge_SelfOrder_UnknownMenuItemRejected(t *testing.T) {
	ctx, conn := setupDomainEnv(t)
	testutil.SeedCapDoc(t, ctx, conn, domainConsumerCapDoc())
	cp, cons := newDomainPipeline(t, ctx, conn, "chargeselfunknownitem")

	seedIdentity(t, ctx, conn, domainConsumerID)
	leaseKey := seedLeaseWithApplicant(t, ctx, conn, "BBCAFEDMNCHGUNKLEASE", domainConsumerID)
	tabKey := openTab(t, ctx, conn, cp, cons, "cdselfchargeunksetup1", leaseKey)
	absentItemKey := "vtx.menuitem.BBABSENTMENUITEMHJKM"
	applicationForLnk := "lnk.leaseapp.BBCAFEDMNCHGUNKLEASE.applicationFor.identity." + domainConsumerID

	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("cdselfchargeunk00001"),
		Lane:          processor.LaneDefault,
		OperationType: "Charge",
		Actor:         domainConsumerKey,
		SubmittedAt:   "2026-07-18T12:10:00Z",
		Class:         "tab",
		Payload:       json.RawMessage(`{"tabKey":"` + tabKey + `","menuItemKey":"` + absentItemKey + `"}`),
		ContextHint: &processor.ContextHint{
			Reads:         []string{tabKey, tabKey + ".status", absentItemKey, absentItemKey + ".price"},
			OptionalReads: []string{applicationForLnk},
		},
		AuthContext: &processor.AuthContext{Target: domainConsumerKey},
	}
	testutil.PublishOp(t, conn, env)
	outcome := testutil.DriveOne(t, ctx, cp, cons, "")
	if outcome != processor.OutcomeRejected {
		t.Fatalf("self-order Charge against an unknown menu item outcome = %v, want Rejected", outcome)
	}
}
