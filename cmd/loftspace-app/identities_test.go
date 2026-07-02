package main

import "testing"

// TestReshapeRoster_DropsUnusableRowsKeepsOrder proves the protected-model
// reshaper keeps {key,name,state} rows in query order and drops rows missing a
// key or carrying a blank name (e.g. a row whose secure name column projected
// NULL for a shredded identity and slipped past the SQL filter) without
// panicking.
func TestReshapeRoster_DropsUnusableRowsKeepsOrder(t *testing.T) {
	rows := []protectedIdentityRow{
		{IdentityKey: "vtx.identity.a", Name: "Alice Renter", State: "claimed"},
		{IdentityKey: "vtx.identity.b", Name: "Bob Tenant", State: "unclaimed"},
		// blank name — skipped (shredded/unnamed)
		{IdentityKey: "vtx.identity.c", Name: "   ", State: "unclaimed"},
		// no key — skipped
		{Name: "Ghost", State: "unclaimed"},
	}
	ids := reshapeRoster(rows)
	if len(ids) != 2 {
		t.Fatalf("want 2 usable identities, got %d: %+v", len(ids), ids)
	}
	if ids[0].Name != "Alice Renter" || ids[0].Key != "vtx.identity.a" {
		t.Errorf("want Alice first (query order preserved), got %+v", ids[0])
	}
	if ids[1].Name != "Bob Tenant" || ids[1].State != "unclaimed" {
		t.Errorf("want Bob second, got %+v", ids[1])
	}
}
