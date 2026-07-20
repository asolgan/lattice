package clinicdomain

import (
	"strings"
	"testing"
)

// TestWorkplaceAnchor_AppointmentsUseComprehension mirrors lease-signing's
// vector: the workplace token must be a pattern comprehension, because
// withProvider is OPTIONAL here and a provider-less appointment would otherwise
// put a NULL element in authz_anchors — which the Protected adapter rejects,
// failing the row's upsert and hiding the appointment from its own PATIENT.
func TestWorkplaceAnchor_AppointmentsUseComprehension(t *testing.T) {
	spec := clinicAppointmentsReadSpec

	if !strings.Contains(spec, "[(pr)-[:practicesAt]->(b:building) | nanoIdFromKey(b.key)]") {
		t.Fatal("the workplace anchor must be a pattern comprehension over the provider's building")
	}
	if !strings.Contains(spec, "[nanoIdFromKey(p.key)] +") {
		t.Error("the patient anchor must remain the first, unconditional element")
	}
	if strings.Contains(spec, "[nanoIdFromKey(p.key), nanoIdFromKey(") {
		t.Error("two-element array literal reintroduces the null-element hazard")
	}
}

// TestWorkplaceAnchor_ProviderLensUnchanged holds the v1 boundary: only the
// two tables the design names are workplace-scoped. The provider schedule and
// the patient roster are not — clinicPatientsRead in particular is wildcard-only
// (empty authz_anchors), so a building token must not start opening it.
func TestWorkplaceAnchor_ProviderLensUnchanged(t *testing.T) {
	if strings.Contains(providerAppointmentsReadSpec, "practicesAt]->(b:building)") {
		t.Error("providerAppointmentsRead must not gain a workplace anchor in v1")
	}
	if !strings.Contains(providerAppointmentsReadSpec, "[nanoIdFromKey(pr.key)]") {
		t.Error("the provider lens must keep its self-anchor exactly as shipped")
	}
}
