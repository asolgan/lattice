//go:build ignore

// seed-classic-demo.go — dev-seed for `make seed-classic-demo`.
//
// verticals.md "Classic vertical demo data has no seed path": a fresh dev
// stack's Core KV holds zero leaseapp/listing/appointment/tab vertices —
// seed-edge-demo and seed-showcase both mint Facet catalog scaffolding only,
// nothing that exercises the classic (non-Facet) LoftSpace/Clinic/Café
// flows. This seeds one of each: a LoftSpace unit + available listing + a
// consumer's lease application, a Clinic patient + provider + appointment
// (linked to the lease so PO discovery can walk resident->tenant->patient),
// and a Café tab opened against that same lease.
//
// Requires `make install-showcase-domains` (loftspace/clinic/cafe/wellness
// domains) already applied to the target stack — the domain ops below FATAL
// on an uninstalled package.
//
// Every op below is submitted directly over NATS as the bootstrap admin
// actor (already `operator` via the primordial seed) — mirroring
// seed-edge-demo.go / verify-real-actor-write-auth.go's seedListing helper,
// not the Gateway.
//
// NOT idempotent: mints fresh vertices on every run (no dedup key), matching
// seed-edge-demo.go's own dev-loop convention.
//
// Run via: make seed-classic-demo (== go run ./scripts/seed-classic-demo.go)
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/asolgan/lattice/cmd/lattice/output"
	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/pkgmgr"
	"github.com/asolgan/lattice/internal/processor"
	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/scripts/pkgverify"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	natsURL := pkgverify.EnvOrDefault("NATS_URL", "nats://localhost:4222")
	bootstrapPath := pkgverify.EnvOrDefault("BOOTSTRAP_JSON_PATH", "./lattice.bootstrap.json")

	must(bootstrap.Load(bootstrapPath), "load bootstrap JSON")

	conn, err := output.Connect(ctx, natsURL)
	must(err, "connect to NATS")
	defer conn.Close()

	adminKey := bootstrap.BootstrapIdentityKey
	consumerRoleKey := "vtx.role." + pkgmgr.RoleID("identity-domain", "consumer")

	// --- LoftSpace: unit + listing + consumer + lease application -----------

	unitReply := submitOp(ctx, conn, adminKey, "CreateLocation", "location",
		map[string]any{"locationType": "unit"}, nil)
	unitKey := unitReply.PrimaryKey
	fmt.Printf("==> unit:            %s\n", unitKey)

	submitOp(ctx, conn, adminKey, "SetUnitAddress", "loftspaceListing",
		map[string]any{"unit": unitKey, "line1": "12 Classic Demo Ave", "city": "Springfield", "region": "OR", "postal": "97477"},
		&processor.ContextHint{Reads: []string{unitKey}})
	fmt.Println("==> wired:           unit address")

	submitOp(ctx, conn, adminKey, "SetListing", "loftspaceListing",
		map[string]any{"unit": unitKey, "rentAmount": 2200, "rentCurrency": "USD", "bedrooms": 1,
			"availableFrom": "2026-08-01T00:00:00Z", "leaseTermMonths": 12, "status": "available"},
		&processor.ContextHint{Reads: []string{unitKey}})
	fmt.Printf("==> listing:         %s (available)\n", unitKey)

	salt, err := substrate.NewNanoID()
	must(err, "generate consumer email salt")
	claimSum := mustSHA256Hex("classic-demo-consumer-" + salt)
	consumerReply := submitOp(ctx, conn, adminKey, "CreateUnclaimedIdentity", "identity",
		map[string]any{
			"name":         "Classic Demo Resident " + salt[:8],
			"email":        "resident-" + salt[:8] + "@dev.lattice.local",
			"claimKeyHash": claimSum,
		}, nil)
	consumerKey := consumerReply.PrimaryKey
	fmt.Printf("==> resident:        %s\n", consumerKey)

	submitOp(ctx, conn, adminKey, "AssignRole", "",
		map[string]any{"actorKey": consumerKey, "roleKey": consumerRoleKey},
		&processor.ContextHint{Reads: []string{consumerKey, consumerRoleKey}})
	fmt.Printf("==> assigned:        %s holds consumer (%s)\n", consumerKey, consumerRoleKey)

	leaseReply := submitOp(ctx, conn, adminKey, "CreateLeaseApplication", "leaseapp",
		map[string]any{"applicant": consumerKey, "unit": unitKey},
		&processor.ContextHint{Reads: []string{consumerKey, unitKey}})
	leaseAppKey := leaseReply.PrimaryKey
	fmt.Printf("==> lease app:       %s\n", leaseAppKey)

	// --- Clinic: patient + provider + appointment ----------------------------

	patientReply := submitOp(ctx, conn, adminKey, "CreatePatient", "patient",
		map[string]any{"fullName": "Classic Demo Patient"}, nil)
	patientKey := patientReply.PrimaryKey
	fmt.Printf("==> patient:         %s\n", patientKey)

	providerReply := submitOp(ctx, conn, adminKey, "CreateProvider", "provider",
		map[string]any{"fullName": "Dr. Classic Demo", "specialty": "Family Medicine"}, nil)
	providerKey := providerReply.PrimaryKey
	fmt.Printf("==> provider:        %s\n", providerKey)

	// 24h out, truncated to the clinic's mandatory 15-minute booking grid
	// (:00/:15/:30/:45 — Unix-epoch-aligned truncation lands on a grid cell).
	startsAt := time.Now().UTC().Add(24 * time.Hour).Truncate(15 * time.Minute)
	endsAt := startsAt.Add(30 * time.Minute)

	apptReply := submitOp(ctx, conn, adminKey, "CreateAppointment", "appointment",
		map[string]any{
			"patient":     patientKey,
			"provider":    providerKey,
			"startsAt":    startsAt.Format(time.RFC3339),
			"endsAt":      endsAt.Format(time.RFC3339),
			"reason":      "Annual checkup",
			"leaseAppKey": leaseAppKey,
		},
		&processor.ContextHint{Reads: []string{patientKey, providerKey}})
	fmt.Printf("==> appointment:     %s (%s)\n", apptReply.PrimaryKey, startsAt.Format(time.RFC3339))

	// --- Café: tab opened against the same lease ------------------------------

	tabReply := submitOp(ctx, conn, adminKey, "OpenTab", "tab",
		map[string]any{"leaseAppKey": leaseAppKey},
		&processor.ContextHint{
			Reads:         []string{leaseAppKey},
			OptionalReads: []string{leaseAppKey + ".cafeOpenTab"},
		})
	fmt.Printf("==> tab:             %s (open)\n", tabReply.PrimaryKey)

	fmt.Println()
	fmt.Println("==> classic vertical demo data seeded.")
	fmt.Printf("    resident:    %s\n", consumerKey)
	fmt.Printf("    lease app:   %s\n", leaseAppKey)
	fmt.Printf("    listing:     %s\n", unitKey)
	fmt.Printf("    appointment: %s\n", apptReply.PrimaryKey)
	fmt.Printf("    tab:         %s\n", tabReply.PrimaryKey)
}

// submitOp submits an operation as actorKey over NATS (the bootstrap-actor
// setup path, not the Gateway) and fatals on a transport error or a rejected
// reply, mirroring seed-edge-demo.go's helper of the same name.
func submitOp(ctx context.Context, conn *substrate.Conn, actorKey, operationType, class string, payload map[string]any, hint *processor.ContextHint) *processor.OperationReply {
	reqID, err := substrate.NewNanoID()
	must(err, "generate requestId")
	payloadBytes, err := json.Marshal(payload)
	must(err, "marshal payload")
	env := &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          processor.LaneDefault,
		OperationType: operationType,
		Actor:         actorKey,
		Class:         class,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Payload:       payloadBytes,
		ContextHint:   hint,
	}
	reply, err := output.SubmitOp(ctx, conn, env)
	must(err, "submit "+operationType)
	mustAccepted(reply, operationType)
	return reply
}

func mustAccepted(reply *processor.OperationReply, context string) {
	if reply.Status == processor.ReplyStatusAccepted {
		return
	}
	if reply.Error != nil {
		fmt.Fprintf(os.Stderr, "FATAL %s: rejected code=%s message=%s\n", context, reply.Error.Code, reply.Error.Message)
	} else {
		fmt.Fprintf(os.Stderr, "FATAL %s: status=%s (no error detail)\n", context, reply.Status)
	}
	os.Exit(1)
}

func must(err error, context string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL %s: %v\n", context, err)
		os.Exit(1)
	}
}

func mustSHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
