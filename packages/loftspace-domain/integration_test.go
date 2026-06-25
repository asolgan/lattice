// Listing / address integration tests for the loftspace-domain Capability
// Package.
//
// External test package (loftspacedomain_test) so the tests exercise the public
// Lattice surface a real package sees: seed the kernel, install rbac-domain +
// identity-domain + identity-hygiene + location-domain + loftspace-domain
// through the Processor, mint a location unit, then submit SetListing /
// SetUnitAddress and assert the committed Core-KV shape — the listing / address
// aspects land on the unit (class listing / address), optional fields included,
// and the unit-class + status guards reject bad input.
//
// Coverage:
//  1. TestLoftspace_SetListingAndAddress — listing + address aspects on a unit, optional fields
//  2. TestLoftspace_SetListingUpsert     — re-publish overwrites in place (status available→leased)
//  3. TestLoftspace_RejectsBadStatus     — status not in {available,pending,leased} → Rejected
//  4. TestLoftspace_RejectsNonUnit       — target alive but class≠location → Rejected (NotAUnit guard)
//  5. TestLoftspace_RejectsDeadUnit      — tombstoned unit → Rejected
//  6. TestLoftspace_UnauthorizedDenied   — consumer cap (no listing ops) → Rejected
package loftspacedomain_test

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
	locationdomain "github.com/asolgan/lattice/packages/location-domain"
	loftspacedomain "github.com/asolgan/lattice/packages/loftspace-domain"
)

const (
	lsStaffActorID   = "LSstaffActHJKMNPQRST"
	lsStaffActorKey  = "vtx.identity." + lsStaffActorID
	lsStaffCapKey    = "cap.identity." + lsStaffActorID
	lsConsumerID     = "LSconsumerHJKMNPQRST"
	lsConsumerKey    = "vtx.identity." + lsConsumerID
	lsConsumerCapKey = "cap.identity." + lsConsumerID
)

// loftspaceOps are the ops the staff actor needs: CreateLocation (to mint the
// unit it operates on) + the two loftspace ops.
var loftspaceOps = []string{"CreateLocation", "SetListing", "SetUnitAddress"}

func lsStaffCapDoc() *processor.CapabilityDoc {
	now := time.Now().UTC()
	perms := make([]processor.PlatformPermission, 0, len(loftspaceOps))
	for _, op := range loftspaceOps {
		perms = append(perms, processor.PlatformPermission{OperationType: op, Scope: "any"})
	}
	return &processor.CapabilityDoc{
		Key:                    lsStaffCapKey,
		Actor:                  lsStaffActorKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{lsStaffActorKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions:    perms,
		ServiceAccess:          []processor.ServiceAccessEntry{},
		EphemeralGrants:        []processor.EphemeralGrant{},
		Roles:                  []string{bootstrap.RoleOperatorKey},
	}
}

func lsConsumerCapDoc() *processor.CapabilityDoc {
	now := time.Now().UTC()
	return &processor.CapabilityDoc{
		Key:                    lsConsumerCapKey,
		Actor:                  lsConsumerKey,
		Version:                "1.0",
		ProjectedAt:            now.Format(time.RFC3339Nano),
		ProjectedFromRevisions: map[string]uint64{lsConsumerKey: 1},
		Lanes:                  []string{"default"},
		PlatformPermissions:    []processor.PlatformPermission{},
		ServiceAccess:          []processor.ServiceAccessEntry{},
		EphemeralGrants:        []processor.EphemeralGrant{},
		Roles:                  []string{"vtx.role.consumer"},
	}
}

// setupLoftspaceEnv seeds the kernel, installs the phase-1 packages +
// location-domain (the dependency) + loftspace-domain through the real
// meta-install pipeline, and seeds the cap docs.
func setupLoftspaceEnv(t *testing.T) (context.Context, *substrate.Conn) {
	t.Helper()
	ctx, conn := testutil.SetupPackageTestEnv(t) // installs rbac+identity+hygiene
	stop := testutil.RunMetaInstallPipeline(t, ctx, conn)
	inst := pkgmgr.NewInstaller(conn, bootstrap.BootstrapIdentityKey)
	inst.RoleIDs = map[string]string{"operator": bootstrap.RoleOperatorID}
	if _, err := inst.Install(ctx, locationdomain.Package); err != nil {
		stop()
		t.Fatalf("install location-domain: %v", err)
	}
	if _, err := inst.Install(ctx, loftspacedomain.Package); err != nil {
		stop()
		t.Fatalf("install loftspace-domain: %v", err)
	}
	stop()
	testutil.SeedCapDoc(t, ctx, conn, lsStaffCapDoc())
	testutil.SeedCapDoc(t, ctx, conn, lsConsumerCapDoc())
	return ctx, conn
}

func newLoftspacePipeline(t *testing.T, ctx context.Context, conn *substrate.Conn, durable string) (*processor.CommitPath, jetstream.Consumer) {
	t.Helper()
	return testutil.CapabilityPipeline(t, ctx, conn, testutil.PipelineConfig{
		Durable:  durable,
		Instance: "ls-" + durable,
	})
}

func lsNanoIDFromRequestID(requestID string) string {
	seed := processor.SeedFromRequestID(requestID)
	pcg := rand.NewPCG(seed[0], seed[1])
	return processor.DeterministicNanoID(pcg, substrate.NanoIDLength)
}

func lsSeedVertex(t *testing.T, ctx context.Context, conn *substrate.Conn, key, class string, isDeleted bool) {
	t.Helper()
	doc := map[string]any{"class": class, "isDeleted": isDeleted, "data": map[string]any{}}
	b, _ := json.Marshal(doc)
	if _, err := conn.KVPut(ctx, testutil.HarnessCoreBucket, key, b); err != nil {
		t.Fatalf("seed vertex %s: %v", key, err)
	}
}

func lsReadDoc(t *testing.T, ctx context.Context, conn *substrate.Conn, key string) map[string]any {
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

// createUnit submits CreateLocation(unit) and returns the minted unit key.
func createUnit(t *testing.T, ctx context.Context, conn *substrate.Conn, cp *processor.CommitPath, cons jetstream.Consumer) string {
	t.Helper()
	reqID := testutil.GenReqID("mkunit")
	unitKey := "vtx.unit." + lsNanoIDFromRequestID(reqID)
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: "CreateLocation",
		Actor:         lsStaffActorKey,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "location",
		Payload:       json.RawMessage(`{"locationType":"unit"}`),
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)
	return unitKey
}

// setListing submits SetListing on the given unit with the given payload and the
// expected outcome.
func setListing(t *testing.T, ctx context.Context, conn *substrate.Conn, cp *processor.CommitPath, cons jetstream.Consumer, label, unitKey, payload string, want processor.MessageOutcome) {
	t.Helper()
	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID(label),
		Lane:          processor.LaneDefault,
		OperationType: "SetListing",
		Actor:         lsStaffActorKey,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "loftspaceListing",
		Payload:       json.RawMessage(payload),
		ContextHint:   &processor.ContextHint{Reads: []string{unitKey}},
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, want)
}

// TestLoftspace_SetListingAndAddress mints a unit, sets a listing with optional
// fields and an address, and asserts both aspects land with the right class,
// vertexKey/localName envelope, and data.
func TestLoftspace_SetListingAndAddress(t *testing.T) {
	ctx, conn := setupLoftspaceEnv(t)
	cp, cons := newLoftspacePipeline(t, ctx, conn, "set")

	unitKey := createUnit(t, ctx, conn, cp, cons)

	setListing(t, ctx, conn, cp, cons, "setList0001", unitKey,
		`{"unit":"`+unitKey+`","rentAmount":2400,"rentCurrency":"USD","bedrooms":2,"bathrooms":1.5,"sqft":950,"availableFrom":"2026-08-01T00:00:00Z","leaseTermMonths":12,"status":"available"}`,
		processor.OutcomeAccepted)

	ldoc := lsReadDoc(t, ctx, conn, unitKey+".listing")
	if ldoc["class"] != "listing" {
		t.Fatalf("listing class = %v, want listing", ldoc["class"])
	}
	if vk, _ := ldoc["vertexKey"].(string); vk != unitKey {
		t.Fatalf("listing vertexKey = %q, want %q", vk, unitKey)
	}
	ldata, _ := ldoc["data"].(map[string]any)
	if ldata["status"] != "available" {
		t.Fatalf("listing status = %v, want available", ldata["status"])
	}
	if ldata["rentCurrency"] != "USD" {
		t.Fatalf("listing rentCurrency = %v, want USD", ldata["rentCurrency"])
	}
	// Optional fields landed.
	if _, ok := ldata["bathrooms"]; !ok {
		t.Fatalf("listing missing optional bathrooms; data=%v", ldata)
	}
	if _, ok := ldata["sqft"]; !ok {
		t.Fatalf("listing missing optional sqft; data=%v", ldata)
	}

	addrEnv := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("setAddr0001"),
		Lane:          processor.LaneDefault,
		OperationType: "SetUnitAddress",
		Actor:         lsStaffActorKey,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "loftspaceListing",
		Payload:       json.RawMessage(`{"unit":"` + unitKey + `","line1":"123 Market St","city":"San Francisco","region":"CA","postal":"94103"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{unitKey}},
	}
	testutil.PublishOp(t, conn, addrEnv)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeAccepted)

	adoc := lsReadDoc(t, ctx, conn, unitKey+".address")
	if adoc["class"] != "address" {
		t.Fatalf("address class = %v, want address", adoc["class"])
	}
	adata, _ := adoc["data"].(map[string]any)
	if adata["city"] != "San Francisco" || adata["postal"] != "94103" {
		t.Fatalf("address data = %v, want city/postal set", adata)
	}
	// Optional line2 absent → not written.
	if _, ok := adata["line2"]; ok {
		t.Fatalf("address line2 should be absent when not supplied; data=%v", adata)
	}
}

// TestLoftspace_SetListingUpsert proves the unconditioned-upsert idiom: a second
// SetListing on the same unit overwrites the .listing aspect in place (one
// aspect, status flipped available→leased), not a conflict.
func TestLoftspace_SetListingUpsert(t *testing.T) {
	ctx, conn := setupLoftspaceEnv(t)
	cp, cons := newLoftspacePipeline(t, ctx, conn, "upsert")

	unitKey := createUnit(t, ctx, conn, cp, cons)
	base := `{"unit":"` + unitKey + `","rentAmount":2400,"rentCurrency":"USD","bedrooms":2,"availableFrom":"2026-08-01T00:00:00Z","leaseTermMonths":12,`

	setListing(t, ctx, conn, cp, cons, "upAvail0001", unitKey, base+`"status":"available"}`, processor.OutcomeAccepted)
	setListing(t, ctx, conn, cp, cons, "upLeased001", unitKey, base+`"status":"leased"}`, processor.OutcomeAccepted)

	ldoc := lsReadDoc(t, ctx, conn, unitKey+".listing")
	ldata, _ := ldoc["data"].(map[string]any)
	if ldata["status"] != "leased" {
		t.Fatalf("after re-publish, status = %v, want leased (overwrite-in-place)", ldata["status"])
	}
	if del, _ := ldoc["isDeleted"].(bool); del {
		t.Fatalf("listing should be alive after upsert; got isDeleted=%v", del)
	}
}

// TestLoftspace_RejectsBadStatus proves the status enum guard.
func TestLoftspace_RejectsBadStatus(t *testing.T) {
	ctx, conn := setupLoftspaceEnv(t)
	cp, cons := newLoftspacePipeline(t, ctx, conn, "bad-status")

	unitKey := createUnit(t, ctx, conn, cp, cons)
	setListing(t, ctx, conn, cp, cons, "badStat0001", unitKey,
		`{"unit":"`+unitKey+`","rentAmount":1,"rentCurrency":"USD","bedrooms":1,"availableFrom":"2026-08-01T00:00:00Z","leaseTermMonths":12,"status":"bogus"}`,
		processor.OutcomeRejected)

	if _, err := conn.KVGet(ctx, testutil.HarnessCoreBucket, unitKey+".listing"); err == nil {
		t.Fatalf("a bad-status listing was committed on %s", unitKey)
	}
}

// TestLoftspace_RejectsNonUnit proves the class guard: a target that is alive
// and key-shaped as a unit (vtx.unit.<id>) but is NOT class=location is
// rejected — listing economics attach only to a real location unit.
func TestLoftspace_RejectsNonUnit(t *testing.T) {
	ctx, conn := setupLoftspaceEnv(t)
	cp, cons := newLoftspacePipeline(t, ctx, conn, "non-unit")

	fakeKey := "vtx.unit.LSfakeunitHJKMNPQR"
	lsSeedVertex(t, ctx, conn, fakeKey, "identity", false) // unit-shaped key, wrong class

	setListing(t, ctx, conn, cp, cons, "nonUnit0001", fakeKey,
		`{"unit":"`+fakeKey+`","rentAmount":1,"rentCurrency":"USD","bedrooms":1,"availableFrom":"2026-08-01T00:00:00Z","leaseTermMonths":12,"status":"available"}`,
		processor.OutcomeRejected)

	if _, err := conn.KVGet(ctx, testutil.HarnessCoreBucket, fakeKey+".listing"); err == nil {
		t.Fatalf("a listing was committed on a non-location vertex %s", fakeKey)
	}
}

// TestLoftspace_RejectsDeadUnit proves the alive guard: a tombstoned unit is
// rejected even though its key resolves.
func TestLoftspace_RejectsDeadUnit(t *testing.T) {
	ctx, conn := setupLoftspaceEnv(t)
	cp, cons := newLoftspacePipeline(t, ctx, conn, "dead-unit")

	deadKey := "vtx.unit.LSdeadunitHJKMNPQR"
	lsSeedVertex(t, ctx, conn, deadKey, "location", true) // alive=false

	setListing(t, ctx, conn, cp, cons, "deadUnit001", deadKey,
		`{"unit":"`+deadKey+`","rentAmount":1,"rentCurrency":"USD","bedrooms":1,"availableFrom":"2026-08-01T00:00:00Z","leaseTermMonths":12,"status":"available"}`,
		processor.OutcomeRejected)
}

// TestLoftspace_UnauthorizedDenied submits SetListing as the consumer actor (no
// listing permissions). Expects OutcomeRejected.
func TestLoftspace_UnauthorizedDenied(t *testing.T) {
	ctx, conn := setupLoftspaceEnv(t)
	cp, cons := newLoftspacePipeline(t, ctx, conn, "unauth")

	unitKey := createUnit(t, ctx, conn, cp, cons)
	env := &processor.OperationEnvelope{
		RequestID:     testutil.GenReqID("unauth0001"),
		Lane:          processor.LaneDefault,
		OperationType: "SetListing",
		Actor:         lsConsumerKey,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "loftspaceListing",
		Payload:       json.RawMessage(`{"unit":"` + unitKey + `","rentAmount":1,"rentCurrency":"USD","bedrooms":1,"availableFrom":"2026-08-01T00:00:00Z","leaseTermMonths":12,"status":"available"}`),
		ContextHint:   &processor.ContextHint{Reads: []string{unitKey}},
	}
	testutil.PublishOp(t, conn, env)
	testutil.DriveOne(t, ctx, cp, cons, processor.OutcomeRejected)
}
