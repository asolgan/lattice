package main

import "testing"

func TestComputeFrontDeskBookings_SkipsRowsWithNoLeaseAppKey(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.booking.b1": map[string]any{
			"bookingKey": "vtx.booking.b1", "leaseAppKey": "vtx.leaseapp.a",
			"sessionName": "Sat mobility class", "startsAt": "2026-07-11T09:00:00Z",
		},
		// a tombstoned/undecodable entry — skipped (no leaseAppKey)
		"vtx.booking.bad": map[string]any{},
	})

	rows := computeFrontDeskBookings(keys, get)
	if len(rows) != 1 {
		t.Fatalf("want 1 row (tombstoned entry excluded), got %d (%+v)", len(rows), rows)
	}
	if rows[0].LeaseAppKey != "vtx.leaseapp.a" || rows[0].SessionName != "Sat mobility class" {
		t.Errorf("unexpected row: %+v", rows[0])
	}
}

func TestComputeFrontDeskLeaseDetails_SkipsRowsWithNoLeaseAppKey(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.leaseapp.a": map[string]any{
			"leaseAppKey": "vtx.leaseapp.a", "unitAddress": "123 Main St",
			"unitRent": 2500.0, "unitCurrency": "USD", "unitLeaseTermMonths": 12.0,
		},
		// a tombstoned/undecodable entry — skipped (no leaseAppKey)
		"vtx.leaseapp.bad": map[string]any{},
	})

	rows := computeFrontDeskLeaseDetails(keys, get)
	if len(rows) != 1 {
		t.Fatalf("want 1 row (tombstoned entry excluded), got %d (%+v)", len(rows), rows)
	}
	if rows[0].UnitRent != 2500.0 || rows[0].UnitCurrency != "USD" || rows[0].UnitLeaseTermMo != 12.0 {
		t.Errorf("unexpected row: %+v", rows[0])
	}
}
