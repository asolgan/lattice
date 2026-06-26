package main

import (
	"encoding/json"
	"testing"
	"time"
)

// fakeKV builds a kvGetter over an in-memory map for the compute* seam tests —
// the same headless seam loftspace-app's computeListings tests use. A key absent
// from the map reports (nil, false), exercising the tombstone-skip path.
func fakeKV(entries map[string]any) (keys []string, get kvGetter) {
	raw := map[string][]byte{}
	for k, v := range entries {
		b, _ := json.Marshal(v)
		raw[k] = b
		keys = append(keys, k)
	}
	get = func(key string) ([]byte, bool) {
		b, ok := raw[key]
		return b, ok
	}
	return keys, get
}

func TestComputeProviders_SortsAndSkips(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.provider.B": map[string]any{"providerKey": "vtx.provider.B", "name": "Dr. Sam Okafor", "specialty": "Cardiology", "credentials": "MD"},
		"vtx.provider.A": map[string]any{"providerKey": "vtx.provider.A", "name": "Dr. Lee", "specialty": "Dermatology"},
		// A tombstoned projection row with no providerKey must be skipped.
		"vtx.provider.X": map[string]any{"name": "Ghost"},
	})
	rows := computeProviders(keys, get)
	if len(rows) != 2 {
		t.Fatalf("expected 2 providers (the keyless row skipped), got %d", len(rows))
	}
	if rows[0].Name != "Dr. Lee" || rows[1].Name != "Dr. Sam Okafor" {
		t.Fatalf("providers not sorted by name: %+v", rows)
	}
	if rows[1].Credentials != "MD" {
		t.Fatalf("credentials lost: %+v", rows[1])
	}
}

func TestComputePatients_NameOnlySortedSkips(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.patient.B": map[string]any{"patientKey": "vtx.patient.B", "name": "Bob Tenant"},
		"vtx.patient.A": map[string]any{"patientKey": "vtx.patient.A", "name": "Alice Rivera"},
		"vtx.patient.X": map[string]any{"name": "Keyless"}, // skipped
	})
	rows := computePatients(keys, get)
	if len(rows) != 2 {
		t.Fatalf("expected 2 patients, got %d", len(rows))
	}
	if rows[0].Name != "Alice Rivera" || rows[1].Name != "Bob Tenant" {
		t.Fatalf("patients not sorted by name: %+v", rows)
	}
}

func TestComputeAppointments_ScopeByPatient(t *testing.T) {
	keys, get := apptFixture()
	rows := computeAppointments(keys, get, "vtx.patient.alice", "")
	if len(rows) != 2 {
		t.Fatalf("expected 2 appointments for alice, got %d", len(rows))
	}
	// Sorted by startsAt: the 09:00 before the 15:00.
	if rows[0].StartsAt != "2026-07-01T09:00:00Z" || rows[1].StartsAt != "2026-07-01T15:00:00Z" {
		t.Fatalf("appointments not sorted by startsAt: %+v", rows)
	}
	if rows[1].ProviderName != "Dr. Sam Okafor" {
		t.Fatalf("provider join lost: %+v", rows[1])
	}
}

func TestComputeAppointments_ScopeByProvider(t *testing.T) {
	keys, get := apptFixture()
	rows := computeAppointments(keys, get, "", "vtx.provider.sam")
	if len(rows) != 2 {
		t.Fatalf("expected 2 appointments on Dr. Sam's schedule, got %d", len(rows))
	}
	for _, r := range rows {
		if r.ProviderKey != "vtx.provider.sam" {
			t.Fatalf("provider scope leaked a foreign row: %+v", r)
		}
	}
}

func TestComputeAppointments_NoScopeReturnsAll(t *testing.T) {
	keys, get := apptFixture()
	rows := computeAppointments(keys, get, "", "")
	if len(rows) != 3 {
		t.Fatalf("expected all 3 appointments with no scope, got %d", len(rows))
	}
}

func TestComputeAppointments_BothScopesIntersect(t *testing.T) {
	keys, get := apptFixture()
	// alice + Dr. Lee: only the 09:00 bob/lee... no — alice sees sam(15:00) and lee(09:00).
	rows := computeAppointments(keys, get, "vtx.patient.alice", "vtx.provider.lee")
	if len(rows) != 1 || rows[0].ProviderKey != "vtx.provider.lee" || rows[0].PatientKey != "vtx.patient.alice" {
		t.Fatalf("patient+provider intersection wrong: %+v", rows)
	}
}

// apptFixture builds three appointments: alice has one with Dr. Sam (15:00) and
// one with Dr. Lee (09:00); bob has one with Dr. Sam (10:00). Plus a keyless
// tombstone row that must be skipped.
func apptFixture() ([]string, kvGetter) {
	return fakeKV(map[string]any{
		"vtx.appointment.1": map[string]any{
			"appointmentKey": "vtx.appointment.1", "startsAt": "2026-07-01T15:00:00Z", "endsAt": "2026-07-01T15:30:00Z",
			"status": "scheduled", "patientKey": "vtx.patient.alice", "patientName": "Alice Rivera",
			"providerKey": "vtx.provider.sam", "providerName": "Dr. Sam Okafor", "providerSpecialty": "Cardiology",
		},
		"vtx.appointment.2": map[string]any{
			"appointmentKey": "vtx.appointment.2", "startsAt": "2026-07-01T09:00:00Z", "endsAt": "2026-07-01T09:20:00Z",
			"status": "confirmed", "patientKey": "vtx.patient.alice", "patientName": "Alice Rivera",
			"providerKey": "vtx.provider.lee", "providerName": "Dr. Lee", "providerSpecialty": "Dermatology",
		},
		"vtx.appointment.3": map[string]any{
			"appointmentKey": "vtx.appointment.3", "startsAt": "2026-07-01T10:00:00Z", "endsAt": "2026-07-01T10:30:00Z",
			"status": "scheduled", "patientKey": "vtx.patient.bob", "patientName": "Bob Tenant",
			"providerKey": "vtx.provider.sam", "providerName": "Dr. Sam Okafor", "providerSpecialty": "Cardiology",
		},
		"vtx.appointment.x": map[string]any{"startsAt": "2026-07-01T08:00:00Z"}, // keyless → skipped
	})
}

// TestBuildEnvelope_Defaults pins the op-envelope seam the FE relies on: a missing
// lane defaults to "default", an empty payload to "{}", and reads carry through.
func TestBuildEnvelope_Defaults(t *testing.T) {
	env, err := buildEnvelope(opRequest{OperationType: "CreateAppointment", Reads: []string{"vtx.patient.a", "vtx.provider.b", "", "vtx.patient.a"}}, "req1", "vtx.identity.admin", time.Time{})
	if err != nil {
		t.Fatalf("buildEnvelope: %v", err)
	}
	if env.Lane != "default" {
		t.Fatalf("lane default = %q, want default", env.Lane)
	}
	if string(env.Payload) != "{}" {
		t.Fatalf("payload default = %q, want {}", string(env.Payload))
	}
	if env.ContextHint == nil || len(env.ContextHint.Reads) != 2 {
		t.Fatalf("reads not cleaned/deduped: %+v", env.ContextHint)
	}
}

func TestBuildEnvelope_RequiresOperationType(t *testing.T) {
	if _, err := buildEnvelope(opRequest{}, "req", "actor", time.Time{}); err == nil {
		t.Fatalf("expected an error for a missing operationType")
	}
}
