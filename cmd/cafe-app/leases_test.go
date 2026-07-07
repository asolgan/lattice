package main

import "testing"

func TestComputeLeases_SortsAndSkipsUndecodable(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.leaseapp.b": map[string]any{"leaseAppKey": "vtx.leaseapp.b", "accountKey": "vtx.cafeaccount.1"},
		"vtx.leaseapp.a": map[string]any{"leaseAppKey": "vtx.leaseapp.a", "accountKey": ""},
		"vtx.leaseapp.x": map[string]any{},
	})
	rows := computeLeases(keys, get)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%+v)", len(rows), rows)
	}
	if rows[0].LeaseAppKey != "vtx.leaseapp.a" || rows[1].LeaseAppKey != "vtx.leaseapp.b" {
		t.Errorf("want sorted (a, b), got (%s, %s)", rows[0].LeaseAppKey, rows[1].LeaseAppKey)
	}
	if rows[1].AccountKey != "vtx.cafeaccount.1" {
		t.Errorf("want row b's accountKey preserved, got %q", rows[1].AccountKey)
	}
}
