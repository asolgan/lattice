package main

import "testing"

// TestComputeIdentities_ReshapesAndSortsByName proves the picker assembler
// reshapes applicantRoster rows to {key,name,state}, sorts by name, and skips
// rows missing a key or name (and undecodable rows) without panicking.
func TestComputeIdentities_ReshapesAndSortsByName(t *testing.T) {
	entries := map[string]string{
		"vtx.identity.b": `{"identityKey":"vtx.identity.b","name":"Bob Tenant","state":"unclaimed"}`,
		"vtx.identity.a": `{"identityKey":"vtx.identity.a","name":"Alice Renter","state":"claimed"}`,
		// no name — skipped (a service/unnamed actor that slipped through)
		"vtx.identity.c": `{"identityKey":"vtx.identity.c","name":"","state":"unclaimed"}`,
		// no key — skipped
		"vtx.identity.d": `{"name":"Ghost","state":"unclaimed"}`,
		// undecodable — skipped
		"vtx.identity.e": `{`,
	}
	ids := computeIdentities(keysOf(entries), fakeKV(entries))
	if len(ids) != 2 {
		t.Fatalf("want 2 named identities, got %d: %+v", len(ids), ids)
	}
	if ids[0].Name != "Alice Renter" || ids[0].Key != "vtx.identity.a" {
		t.Errorf("want Alice first (name-sorted), got %+v", ids[0])
	}
	if ids[1].Name != "Bob Tenant" || ids[1].State != "unclaimed" {
		t.Errorf("want Bob second, got %+v", ids[1])
	}
}
