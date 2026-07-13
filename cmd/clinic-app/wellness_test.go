package main

import "testing"

func TestComputeWellnessSessions_JoinsBookedCountAndSorts(t *testing.T) {
	cap10 := 10.0
	cap4 := 4.0
	keys, get := fakeKV(map[string]any{
		"vtx.session.B": map[string]any{"sessionKey": "vtx.session.B", "name": "Yoga", "startsAt": "2026-07-20T09:00:00Z", "endsAt": "2026-07-20T10:00:00Z", "capacity": cap10, "studioName": "Studio A"},
		"vtx.session.A": map[string]any{"sessionKey": "vtx.session.A", "name": "Pilates", "startsAt": "2026-07-15T09:00:00Z", "endsAt": "2026-07-15T10:00:00Z", "capacity": cap4, "studioName": "Studio B"},
		// A tombstoned projection row with no sessionKey must be skipped.
		"vtx.session.X": map[string]any{"name": "Ghost"},
	})
	bookedCounts := map[string]int{"vtx.session.A": 2}
	rows := computeWellnessSessions(keys, get, bookedCounts)
	if len(rows) != 2 {
		t.Fatalf("expected 2 sessions (the keyless row skipped), got %d", len(rows))
	}
	if rows[0].SessionKey != "vtx.session.A" || rows[1].SessionKey != "vtx.session.B" {
		t.Fatalf("sessions not sorted by startsAt: %+v", rows)
	}
	if rows[0].BookedCount != 2 || rows[0].Capacity != 4 {
		t.Fatalf("expected session A bookedCount=2 capacity=4, got %+v", rows[0])
	}
	if rows[1].BookedCount != 0 || rows[1].Capacity != 10 {
		t.Fatalf("expected session B bookedCount=0 capacity=10, got %+v", rows[1])
	}
}

func TestComputeWellnessBookedCounts_TalliesLiveRowsOnlySkipsGhosts(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.booking.1": map[string]any{"bookingKey": "vtx.booking.1", "sessionKey": "vtx.session.A"},
		"vtx.booking.2": map[string]any{"bookingKey": "vtx.booking.2", "sessionKey": "vtx.session.A"},
		"vtx.booking.3": map[string]any{"bookingKey": "vtx.booking.3", "sessionKey": "vtx.session.B"},
		// A tombstoned projection row with no bookingKey must be skipped.
		"vtx.booking.X": map[string]any{"sessionKey": "vtx.session.A"},
	})
	counts := computeWellnessBookedCounts(keys, get)
	if counts["vtx.session.A"] != 2 {
		t.Fatalf("expected session A count 2, got %d", counts["vtx.session.A"])
	}
	if counts["vtx.session.B"] != 1 {
		t.Fatalf("expected session B count 1, got %d", counts["vtx.session.B"])
	}
}
