package full

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/refractor/ruleengine"
	"github.com/asolgan/lattice/internal/substrate"
	servicelocation "github.com/asolgan/lattice/packages/service-location"
)

// service_location_lens_test.go — the §6.10 executor proof for the
// service-location capabilityServiceAccess lens. It runs the LITERAL lens spec
// (the bytes the package ships) against the REAL full engine over a
// deterministically seeded graph, proving the three Contract #6 §6.10
// behaviors the cypher must enforce:
//
//   - §6.10 item 1 (MULTI-LEVEL EXCLUSION): an unavailableAt at a CLOSER level
//     of the actor's residence→containment chain beats an availableAt higher
//     up. This behavior has NEVER been tested in this codebase — it is the
//     load-bearing proof that the exclusion existential's FRESH ex0/exLoc
//     re-walk the actor's whole chain rather than pinning the matched location.
//   - §6.10 item 2 (TRANSITIVE AVAILABILITY): a resident of a unit inside a
//     building gets a service availableAt the building (the containedIn*0.. hop).
//   - INSTANCE-NOT-SWEPT: a service instance (carries instanceOf, never
//     availableAt) is never projected — the instanceOf-absence template guard.

// slServiceAccess runs the LITERAL capabilityServiceAccess lens spec for the
// given actor and returns the serviceAccess[] entries (the per-service maps).
func slServiceAccess(t *testing.T, adjKV, coreKV *substrate.KV, body, actorKey string) []map[string]any {
	t.Helper()
	out := parseExec(t, body, ruleengine.EventContext{Parameters: map[string]any{
		"actorKey":    actorKey,
		"now":         float64(time.Now().Unix()),
		"projectedAt": time.Now().UTC().Format(time.RFC3339),
	}}, adjKV, coreKV)
	require.Len(t, out, 1, "actor must project exactly one (anchored) row")
	raw, ok := out[0].Values["serviceAccess"].([]any)
	if !ok {
		// An actor reaching no service yields a single degenerate collect entry;
		// normalize to an empty slice of maps.
		return nil
	}
	entries := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		// The degenerate (no-match) collect entry has a null service.
		if m["service"] == nil {
			continue
		}
		entries = append(entries, m)
	}
	return entries
}

// serviceKeys extracts the projected service keys from the serviceAccess set.
func serviceKeys(entries []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		if s, ok := e["service"].(string); ok {
			out[s] = true
		}
	}
	return out
}

// slClassAspect writes a service .class aspect (template/instance discriminator).
func slClassAspect(t *testing.T, coreKV interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
}, vtxKey, value string) {
	t.Helper()
	aspKey := vtxKey + ".class"
	props := map[string]any{
		"key": aspKey, "class": "class", "vertexKey": vtxKey, "localName": "class",
		"data": map[string]any{"value": value},
	}
	raw, err := json.Marshal(props)
	require.NoError(t, err)
	_, err = coreKV.Put(context.Background(), aspKey, raw)
	require.NoError(t, err)
}

// TestServiceLocationLens_MultiLevelExclusion is the load-bearing §6.10 item 1
// proof: an actor residesIn a penthouse; the penthouse containedIn a building;
// a laundry service availableAt the building (so it would reach the actor
// transitively); BUT the laundry is unavailableAt the penthouse (a CLOSER
// exclusion). The laundry must NOT appear in serviceAccess — the closer
// unavailableAt beats the higher-up availableAt.
//
// This proves the exclusion existential's FRESH ex0/exLoc seed independently
// and re-walk the actor's whole residence→containment chain for the bound svc,
// rather than pinning the matched availableAt location (which would never see
// the penthouse-level exclusion and would silently over-grant).
func TestServiceLocationLens_MultiLevelExclusion(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "resident", "identity", "identity", map[string]any{"name": "resident"})
	penthouseKey := putRawVertex(t, reg, coreKV, "penthouse", "unit", "location", map[string]any{})
	buildingKey := putRawVertex(t, reg, coreKV, "building", "building", "location", map[string]any{})
	laundryKey := putRawVertex(t, reg, coreKV, "laundry", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, laundryKey, "service.laundry.template")
	opKey := putRawVertex(t, reg, coreKV, "bookLaundryOp", "meta", "meta", map[string]any{"operationType": "BookLaundry"})

	// residence + containment chain.
	putEdge(t, reg, adjKV, "residesIn", "resident", "penthouse")
	putEdge(t, reg, adjKV, "containedIn", "penthouse", "building")
	// availableAt the BUILDING (higher up).
	putEdge(t, reg, adjKV, "availableAt", "laundry", "building")
	// unavailableAt the PENTHOUSE (closer) — the exclusion.
	putEdge(t, reg, adjKV, "unavailableAt", "laundry", "penthouse")
	// the op the laundry permits.
	putEdge(t, reg, adjKV, "permitsOperation", "laundry", "bookLaundryOp")

	got := serviceKeys(slServiceAccess(t, adjKV, coreKV, body, actorKey))
	require.NotContainsf(t, got, laundryKey,
		"§6.10 item 1: laundry availableAt building but unavailableAt the closer penthouse MUST be excluded; got serviceAccess=%v",
		got)
	require.Empty(t, got, "the only candidate service is excluded, so serviceAccess must be empty")
	_ = penthouseKey
	_ = buildingKey
	_ = opKey
}

// TestServiceLocationLens_NoExclusion_StillAvailable is the control for the
// exclusion proof: the SAME building-availableAt laundry, with NO penthouse
// unavailableAt, MUST appear (the exclusion is what removes it, not a structural
// miss). Confirms the multi-level test fails for the right reason.
func TestServiceLocationLens_NoExclusion_StillAvailable(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "resident", "identity", "identity", map[string]any{})
	putRawVertex(t, reg, coreKV, "penthouse", "unit", "location", map[string]any{})
	putRawVertex(t, reg, coreKV, "building", "building", "location", map[string]any{})
	laundryKey := putRawVertex(t, reg, coreKV, "laundry", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, laundryKey, "service.laundry.template")
	putRawVertex(t, reg, coreKV, "bookLaundryOp", "meta", "meta", map[string]any{"operationType": "BookLaundry"})

	putEdge(t, reg, adjKV, "residesIn", "resident", "penthouse")
	putEdge(t, reg, adjKV, "containedIn", "penthouse", "building")
	putEdge(t, reg, adjKV, "availableAt", "laundry", "building")
	putEdge(t, reg, adjKV, "permitsOperation", "laundry", "bookLaundryOp")
	// NO unavailableAt.

	entries := slServiceAccess(t, adjKV, coreKV, body, actorKey)
	got := serviceKeys(entries)
	require.Containsf(t, got, laundryKey,
		"without the penthouse exclusion the building-availableAt laundry MUST be projected; got %v", got)
	// allowedOperations must carry the permitsOperation op.
	for _, e := range entries {
		if e["service"] == laundryKey {
			ops, _ := e["allowedOperations"].([]any)
			require.NotEmpty(t, ops, "laundry must carry its permitsOperation allowedOperations")
			found := false
			for _, o := range ops {
				om, _ := o.(map[string]any)
				if om != nil && om["operationType"] == "BookLaundry" {
					found = true
				}
			}
			require.True(t, found, "allowedOperations must include the permitsOperation op BookLaundry; got %v", ops)
		}
	}
}

// TestServiceLocationLens_TransitiveAvailability is the §6.10 item 2 proof: a
// resident of a UNIT inside a building gets a service availableAt the BUILDING
// (the containedIn*0.. hop walks residence→ancestors).
func TestServiceLocationLens_TransitiveAvailability(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "unitResident", "identity", "identity", map[string]any{})
	putRawVertex(t, reg, coreKV, "unit", "unit", "location", map[string]any{})
	putRawVertex(t, reg, coreKV, "building", "building", "location", map[string]any{})
	cleaningKey := putRawVertex(t, reg, coreKV, "cleaning", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, cleaningKey, "service.cleaning.template")

	// resident → unit → building.
	putEdge(t, reg, adjKV, "residesIn", "unitResident", "unit")
	putEdge(t, reg, adjKV, "containedIn", "unit", "building")
	// cleaning availableAt the building (an ancestor of the unit).
	putEdge(t, reg, adjKV, "availableAt", "cleaning", "building")

	got := serviceKeys(slServiceAccess(t, adjKV, coreKV, body, actorKey))
	require.Containsf(t, got, cleaningKey,
		"§6.10 item 2: a unit resident must get a building-availableAt service via containedIn; got %v", got)
}

// TestServiceLocationLens_DirectAvailability proves the *0.. depth-0 case: a
// service availableAt the actor's DIRECT residence location is projected.
func TestServiceLocationLens_DirectAvailability(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "directResident", "identity", "identity", map[string]any{})
	putRawVertex(t, reg, coreKV, "unit", "unit", "location", map[string]any{})
	svcKey := putRawVertex(t, reg, coreKV, "directSvc", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, svcKey, "service.cleaning.template")

	putEdge(t, reg, adjKV, "residesIn", "directResident", "unit")
	putEdge(t, reg, adjKV, "availableAt", "directSvc", "unit") // availableAt the DIRECT unit (depth-0)

	got := serviceKeys(slServiceAccess(t, adjKV, coreKV, body, actorKey))
	require.Containsf(t, got, svcKey,
		"a service availableAt the actor's direct residence (containedIn*0.. depth-0) must be projected; got %v", got)
}

// TestServiceLocationLens_InstanceNotSwept proves the template guard: a service
// INSTANCE (carries an instanceOf link, never an availableAt) is never
// projected. Two instances are seeded:
//   - one with NO availableAt (structurally unreachable) — must be absent.
//   - one that ALSO carries an availableAt link to the actor's location (the
//     adversarial case: a claim/instance vertex that somehow acquired an
//     availability edge) — the instanceOf-absence guard MUST still exclude it.
func TestServiceLocationLens_InstanceNotSwept(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "instResident", "identity", "identity", map[string]any{})
	putRawVertex(t, reg, coreKV, "unit", "unit", "location", map[string]any{})
	templateKey := putRawVertex(t, reg, coreKV, "tpl", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, templateKey, "service.cleaning.template")
	// A bare instance (no availableAt).
	instKey := putRawVertex(t, reg, coreKV, "inst", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, instKey, "service.cleaning.instance")
	// An adversarial instance that carries an availableAt edge to the unit.
	badInstKey := putRawVertex(t, reg, coreKV, "badInst", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, badInstKey, "service.cleaning.instance")

	putEdge(t, reg, adjKV, "residesIn", "instResident", "unit")
	// The legit template is availableAt the unit → should project.
	putEdge(t, reg, adjKV, "availableAt", "tpl", "unit")
	// The instances carry instanceOf (instance→template) — the structural marker.
	putEdge(t, reg, adjKV, "instanceOf", "inst", "tpl")
	putEdge(t, reg, adjKV, "instanceOf", "badInst", "tpl")
	// The adversarial instance ALSO carries an availableAt edge to the unit.
	putEdge(t, reg, adjKV, "availableAt", "badInst", "unit")

	got := serviceKeys(slServiceAccess(t, adjKV, coreKV, body, actorKey))
	require.Containsf(t, got, templateKey, "the legit template must project; got %v", got)
	require.NotContainsf(t, got, instKey, "a bare instance (no availableAt) must never project; got %v", got)
	require.NotContainsf(t, got, badInstKey,
		"the template guard (instanceOf-absence) MUST exclude an instance even if it carries an availableAt edge; got %v", got)
}

// TestServiceLocationLens_MultiResidence_PartialExclusion proves the exclusion
// is PER RESIDENCE CHAIN. The actor resides in unitA AND unitB, both contained
// in a building where a gym is availableAt. The gym is unavailableAt unitA only.
// It must STILL be granted via the unexcluded unitB chain — an unavailableAt on
// one residence must not suppress access reached through another.
func TestServiceLocationLens_MultiResidence_PartialExclusion(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "multiResident", "identity", "identity", map[string]any{})
	putRawVertex(t, reg, coreKV, "unitA", "unit", "location", map[string]any{})
	putRawVertex(t, reg, coreKV, "unitB", "unit", "location", map[string]any{})
	putRawVertex(t, reg, coreKV, "building", "building", "location", map[string]any{})
	gymKey := putRawVertex(t, reg, coreKV, "gym", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, gymKey, "service.gym.template")

	// the actor resides in BOTH units; both are contained in the building.
	putEdge(t, reg, adjKV, "residesIn", "multiResident", "unitA")
	putEdge(t, reg, adjKV, "residesIn", "multiResident", "unitB")
	putEdge(t, reg, adjKV, "containedIn", "unitA", "building")
	putEdge(t, reg, adjKV, "containedIn", "unitB", "building")
	// gym availableAt the building; unavailableAt unitA ONLY.
	putEdge(t, reg, adjKV, "availableAt", "gym", "building")
	putEdge(t, reg, adjKV, "unavailableAt", "gym", "unitA")

	got := serviceKeys(slServiceAccess(t, adjKV, coreKV, body, actorKey))
	require.Containsf(t, got, gymKey,
		"per-chain exclusion: a gym unavailableAt unitA MUST still be granted via the unexcluded unitB residence; got %v", got)
}

// TestServiceLocationLens_MultiResidence_FullExclusion is the control: the SAME
// two-residence graph with the gym unavailableAt BOTH units is excluded
// entirely — every residence chain that reaches the gym carries a closer
// exclusion.
func TestServiceLocationLens_MultiResidence_FullExclusion(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	reg := newFixtureRegistry()
	body := servicelocation.Lenses()[0].Spec

	actorKey := putRawVertex(t, reg, coreKV, "multiResident", "identity", "identity", map[string]any{})
	putRawVertex(t, reg, coreKV, "unitA", "unit", "location", map[string]any{})
	putRawVertex(t, reg, coreKV, "unitB", "unit", "location", map[string]any{})
	putRawVertex(t, reg, coreKV, "building", "building", "location", map[string]any{})
	gymKey := putRawVertex(t, reg, coreKV, "gym", "service", "service", map[string]any{})
	slClassAspect(t, coreKV, gymKey, "service.gym.template")

	putEdge(t, reg, adjKV, "residesIn", "multiResident", "unitA")
	putEdge(t, reg, adjKV, "residesIn", "multiResident", "unitB")
	putEdge(t, reg, adjKV, "containedIn", "unitA", "building")
	putEdge(t, reg, adjKV, "containedIn", "unitB", "building")
	putEdge(t, reg, adjKV, "availableAt", "gym", "building")
	// unavailableAt BOTH residences — every chain is excluded.
	putEdge(t, reg, adjKV, "unavailableAt", "gym", "unitA")
	putEdge(t, reg, adjKV, "unavailableAt", "gym", "unitB")

	got := serviceKeys(slServiceAccess(t, adjKV, coreKV, body, actorKey))
	require.NotContainsf(t, got, gymKey,
		"a gym unavailableAt both residences must be excluded entirely; got %v", got)
}
