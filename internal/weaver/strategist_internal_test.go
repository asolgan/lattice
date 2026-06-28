package weaver

import "testing"

// TestBuildPlan_DirectOp_ResolvesReads pins the v1b directOp reads enhancement:
// a directOp gap action templates row.<column> params into the op payload AND
// routes row-templated reads into the dispatched op's ContextHint.Reads, so an
// op that must read its candidate vertex (TombstoneObject) is hydrated. The
// candidate id is already in the lens row (entityKey) — this just routes it.
func TestBuildPlan_DirectOp_ResolvesReads(t *testing.T) {
	t.Parallel()
	ga := GapAction{
		Action:    "directOp",
		Operation: "TombstoneObject",
		Params:    map[string]string{"objectKey": "row.entityKey", "expectedEpoch": "row.linkEpoch"},
		Reads:     []string{"row.entityKey"},
	}
	row := map[string]any{
		"entityKey": "vtx.object.AAobjHJKMNPQRSTUVWX",
		"linkEpoch": int64(7),
	}

	// directOp does not use the registry source, so nil is fine.
	pl, perr := buildPlan(nil, "objectLiveness", "AAobjHJKMNPQRSTUVWX", "missing_owner", ga, row, 99)
	if perr != nil {
		t.Fatalf("buildPlan: %v", perr)
	}
	if pl.operationType != "TombstoneObject" {
		t.Fatalf("operationType = %q want TombstoneObject", pl.operationType)
	}
	if len(pl.reads) != 1 || pl.reads[0] != "vtx.object.AAobjHJKMNPQRSTUVWX" {
		t.Fatalf("reads = %v want [vtx.object.AAobjHJKMNPQRSTUVWX] (the candidate hydrated for the op)", pl.reads)
	}
	payload := pl.payload("")
	if payload["objectKey"] != "vtx.object.AAobjHJKMNPQRSTUVWX" {
		t.Fatalf("payload objectKey = %v want the templated entityKey", payload["objectKey"])
	}
	if payload["expectedEpoch"] != int64(7) {
		t.Fatalf("payload expectedEpoch = %v (%T) want 7 (the templated linkEpoch)", payload["expectedEpoch"], payload["expectedEpoch"])
	}
	if payload["expectedRevision"] != uint64(99) {
		t.Fatalf("payload expectedRevision = %v want 99 (the row revision Weaver auto-injects)", payload["expectedRevision"])
	}
}

// TestBuildPlan_DirectOp_MissingReadColumn errors when a row-templated read
// references an absent column (a malformed playbook must not fire a read-less op).
func TestBuildPlan_DirectOp_MissingReadColumn(t *testing.T) {
	t.Parallel()
	ga := GapAction{
		Action:    "directOp",
		Operation: "TombstoneObject",
		Reads:     []string{"row.nope"},
	}
	_, perr := buildPlan(nil, "objectLiveness", "e", "missing_owner", ga, map[string]any{"entityKey": "k"}, 1)
	if perr == nil {
		t.Fatalf("expected a planError for a read referencing an absent row column")
	}
}
