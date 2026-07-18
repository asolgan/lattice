package controlwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestControlRequest_CursorSerializesAtZero pins edge-syncgap-control-rpc-
// design.md §3.1: the "syncgap" op's Cursor field must serialize even when 0
// (no deltas ever applied) — a maximally-conservative value the server needs
// so it can answer gapped=true and the device re-hydrates. An accidental
// `omitempty` would drop `cursor` from the wire; this test is the guard.
func TestControlRequest_CursorSerializesAtZero(t *testing.T) {
	data, err := json.Marshal(ControlRequest{IdentityID: "AAAAAAAAAAAAAAAAAAAA", DeviceID: "D1", Cursor: 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"cursor":0`) {
		t.Fatalf("Cursor=0 must serialize as \"cursor\":0 (no omitempty); got %s", data)
	}
}

// TestPersonalSyncGapResult_RoundTrip is the RR-4 drift guard
// (edge-lattice-full-design.md §8.1) for the syncgap op's response: the shared
// controlwire struct must round-trip both gapped states through JSON exactly.
func TestPersonalSyncGapResult_RoundTrip(t *testing.T) {
	for _, gapped := range []bool{true, false} {
		in := ControlResponse{PersonalSyncGap: &PersonalSyncGapResult{Gapped: gapped}}
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal gapped=%v: %v", gapped, err)
		}
		var out ControlResponse
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("unmarshal gapped=%v: %v", gapped, err)
		}
		if out.PersonalSyncGap == nil {
			t.Fatalf("gapped=%v: PersonalSyncGap dropped on round-trip: %s", gapped, data)
		}
		if out.PersonalSyncGap.Gapped != gapped {
			t.Fatalf("gapped=%v: round-tripped to %v", gapped, out.PersonalSyncGap.Gapped)
		}
	}
}
