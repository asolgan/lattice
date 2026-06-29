// Package bypass — Phase 1 Gate 3: Capability Lens adversarial test suite.
//
// Vector #8 — Lane authorization bypass.
//
// Attack: an ordinary actor whose standing capability doc grants the `default`
// lane only attempts to submit a platform operation on a privileged lane
// (`system` / `meta` / `urgent`). The submitter declares the lane in the
// envelope; nothing but the step-3 lane gate (Contract #2 §2.3) stands between
// the actor and a privileged lane. Without enforcement, any actor could ride
// `ops.system.>` (engine plane), `ops.meta.>` (DDL/install plane), or
// `ops.urgent.>` — escalating beyond their grant.
//
// The step-3 lane gate (CapabilityAuthorizer.Authorize) checks the declared
// `env.Lane` against the platform actor's standing `doc.Lanes` BEFORE the
// operationType matcher: a lane not in the granted set → LaneUnauthorized.
//
// Both fixture docs carry the SAME platform permission (PingPlatform / any), so
// the operation is otherwise fully authorized — the ONLY thing that blocks the
// default-only actor is the lane gate. This isolates the lane authorization as
// the defense under test (not the operationType matcher).
//
// Fixture (disjoint cap.identity.<actor> entries, core's CapabilityLensDefinition):
//
//	privActor — cap.identity.<actor> lanes: [default, meta, urgent, system]   (kernel-root grant)
//	defActor  — cap.identity.<actor> lanes: [default]                          (ordinary actor)
//	  both: platformPermissions: [{ PingPlatform, any }]
//
// Test cases:
//
//	Positive:    privActor + lane=system  + PingPlatform → ALLOWED
//	Bleed:       defActor  + lane=system  + PingPlatform → DENIED/LaneUnauthorized
//	Bleed:       defActor  + lane=meta    + PingPlatform → DENIED/LaneUnauthorized
//	Bleed:       defActor  + lane=urgent  + PingPlatform → DENIED/LaneUnauthorized
//	Baseline:    defActor  + lane=default + PingPlatform → ALLOWED
//
// DEFENDED when: the privileged actor authorizes on `system` AND the default-only
// actor is denied LaneUnauthorized on every privileged lane AND the default-only
// actor still authorizes on `default` (the gate denies only the ungranted lanes).
//
// Report row:
//
//	Vector #8 | Lane authorization bypass | DEFENDED | CapabilityAuthorizer step-3 lane gate (§2.3): env.Lane ∈ doc.Lanes
package bypass

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/asolgan/lattice/internal/processor"
)

// Lane-plane fixture identifiers for Vector #8. The lane grants live in the
// actor's standing cap.identity.<actor> entry (core's CapabilityLensDefinition);
// the platform branch of step-3 reads that key and consults doc.Lanes.
const (
	laneAdvPrivID = "CAdvLanePriv12345678" // 20 chars — kernel-root actor (all four lanes)
	laneAdvDefID  = "CAdvLaneDef123456789" // 20 chars — ordinary actor (default only)
	laneAdvOp     = "PingPlatform"         // scope=any; otherwise-authorized on both docs

	laneAdvReqPrivSys = "CdV8PrivSysRq1234567" // privileged actor, system lane (positive)
	laneAdvReqDefSys  = "CdV8DefSysRq12345678" // default actor, system lane (bleed)
	laneAdvReqDefMeta = "CdV8DefMetaRq1234567" // default actor, meta lane (bleed)
	laneAdvReqDefUrg  = "CdV8DefUrgRq12345678" // default actor, urgent lane (bleed)
	laneAdvReqDefDef  = "CdV8DefDefRq12345678" // default actor, default lane (baseline)
)

// buildLaneDoc builds a cap.identity.<actor> entry carrying the given lanes plus
// the shared PingPlatform/any permission. The platform op is therefore
// otherwise-authorized on every doc — only doc.Lanes differs.
func buildLaneDoc(actorID string, lanes []string) *processor.CapabilityDoc {
	return &processor.CapabilityDoc{
		Key:         "cap.identity." + actorID,
		Actor:       "vtx.identity." + actorID,
		Version:     "1.0",
		ProjectedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Lanes:       lanes,
		PlatformPermissions: []processor.PlatformPermission{
			{OperationType: laneAdvOp, Scope: "any"},
		},
	}
}

// setupV8Harness provisions Capability KV with the privileged + default-only
// cap.identity.<actor> entries and returns a CapabilityAuthorizer over the live
// (embedded-NATS) bucket.
func setupV8Harness(t *testing.T) (context.Context, *processor.CapabilityAuthorizer) {
	t.Helper()
	ctx, conn := setupCapAdvHarness(t)

	for _, doc := range []*processor.CapabilityDoc{
		buildLaneDoc(laneAdvPrivID, []string{"default", "meta", "urgent", "system"}),
		buildLaneDoc(laneAdvDefID, []string{"default"}),
	} {
		raw, _ := json.Marshal(doc)
		if _, err := conn.KVPut(ctx, capadvCapBucket, doc.Key, raw); err != nil {
			t.Fatalf("v8: seed cap doc %q: %v", doc.Key, err)
		}
	}

	cfg := processor.DefaultCapabilityAuthorizerConfig()
	authz, err := processor.NewCapabilityAuthorizer(conn, capadvCapBucket, nil, cfg, bypassLogger())
	if err != nil {
		t.Fatalf("NewCapabilityAuthorizer: %v", err)
	}
	return ctx, authz
}

// laneAdvEnv builds a platform-path operation envelope (no service/task
// authContext) declaring the given lane.
func laneAdvEnv(reqID, actorID string, lane processor.Lane) *processor.OperationEnvelope {
	return &processor.OperationEnvelope{
		RequestID:     reqID,
		Lane:          lane,
		OperationType: laneAdvOp,
		Actor:         "vtx.identity." + actorID,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Payload:       json.RawMessage(`{}`),
	}
}

// TestCapAdv_V8_PrivilegedActor_SystemLane_Allowed is the positive baseline: a
// kernel-root actor whose doc grants the `system` lane submits a platform op on
// `system` and is authorized — the gate admits a granted lane.
func TestCapAdv_V8_PrivilegedActor_SystemLane_Allowed(t *testing.T) {
	ctx, authz := setupV8Harness(t)

	env := laneAdvEnv(laneAdvReqPrivSys, laneAdvPrivID, processor.LaneSystem)

	dec, err := authz.Authorize(ctx, env)
	if err != nil {
		t.Fatalf("v8 Positive: Authorize error: %v", err)
	}
	if !dec.Authorized {
		t.Fatalf("v8 Positive: FAILED — privileged actor on the system lane should be ALLOWED; got denied: code=%s reason=%s",
			dec.Code, dec.Reason)
	}

	t.Logf("v8 Positive: privileged actor → system lane → PingPlatform ALLOWED ✓")
}

// TestCapAdv_V8_DefaultActor_PrivilegedLanes_Blocked is the bleed defense: a
// default-only actor is denied LaneUnauthorized on every privileged lane, even
// though the operation itself is authorized (PingPlatform/any is in their doc).
func TestCapAdv_V8_DefaultActor_PrivilegedLanes_Blocked(t *testing.T) {
	ctx, authz := setupV8Harness(t)

	cases := []struct {
		name  string
		reqID string
		lane  processor.Lane
	}{
		{"system", laneAdvReqDefSys, processor.LaneSystem},
		{"meta", laneAdvReqDefMeta, processor.LaneMeta},
		{"urgent", laneAdvReqDefUrg, processor.LaneUrgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := laneAdvEnv(tc.reqID, laneAdvDefID, tc.lane)

			dec, err := authz.Authorize(ctx, env)
			if err != nil {
				t.Fatalf("v8 Bleed[%s]: Authorize error: %v", tc.name, err)
			}
			if dec.Authorized {
				t.Fatalf("v8 Bleed[%s]: EXPOSED — a default-only actor submitted on the %s lane; lane authorization bypassed",
					tc.name, tc.name)
			}
			if dec.Code != processor.ErrCodeLaneUnauthorized {
				t.Fatalf("v8 Bleed[%s]: expected LaneUnauthorized, got: code=%s reason=%s", tc.name, dec.Code, dec.Reason)
			}

			t.Logf("v8 Bleed[%s]: DEFENDED — default-only actor → %s lane denied with LaneUnauthorized ✓", tc.name, tc.name)
		})
	}
}

// TestCapAdv_V8_DefaultActor_DefaultLane_Allowed proves the gate denies ONLY the
// ungranted lanes: the same default-only actor still authorizes on `default`.
func TestCapAdv_V8_DefaultActor_DefaultLane_Allowed(t *testing.T) {
	ctx, authz := setupV8Harness(t)

	env := laneAdvEnv(laneAdvReqDefDef, laneAdvDefID, processor.LaneDefault)

	dec, err := authz.Authorize(ctx, env)
	if err != nil {
		t.Fatalf("v8 Baseline: Authorize error: %v", err)
	}
	if !dec.Authorized {
		t.Fatalf("v8 Baseline: FAILED — default-only actor on the default lane should be ALLOWED; got denied: code=%s reason=%s",
			dec.Code, dec.Reason)
	}

	t.Logf("v8 Baseline: default-only actor → default lane → PingPlatform ALLOWED ✓")
}
