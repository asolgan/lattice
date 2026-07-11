package main

import "testing"

func TestComputeResidents_FiltersPrefixSortsAndSkips(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"leaseApplicationComplete.b": map[string]any{
			"entityKey": "vtx.leaseapp.b", "applicant": "vtx.identity.bob", "landlordApproved": true,
		},
		"leaseApplicationComplete.a": map[string]any{
			"entityKey": "vtx.leaseapp.a", "applicant": "vtx.identity.alice", "landlordApproved": false,
		},
		// no applicant yet (projection hasn't reached that stage) — skipped
		"leaseApplicationComplete.pending": map[string]any{"entityKey": "vtx.leaseapp.pending"},
		// a row from a different convergence lens sharing the bucket — skipped by prefix
		"cafeTabSettlement.x": map[string]any{"entityKey": "vtx.leaseapp.c", "applicant": "vtx.identity.carol"},
	})

	rows := computeResidents(keys, get)
	if len(rows) != 2 {
		t.Fatalf("expected 2 residents (pending + wrong-prefix rows skipped), got %d (%+v)", len(rows), rows)
	}
	if rows[0].BookerKey != "vtx.identity.alice" || rows[1].BookerKey != "vtx.identity.bob" {
		t.Fatalf("residents not sorted by bookerKey: %+v", rows)
	}
	if rows[1].LeaseAppKey != "vtx.leaseapp.b" || !rows[1].Approved {
		t.Fatalf("unexpected resident row: %+v", rows[1])
	}
}
