package processor

import (
	"testing"
	"time"
)

func TestTracker_RoundTripJSON(t *testing.T) {
	env := &OperationEnvelope{
		RequestID:     testNanoID1,
		Lane:          LaneDefault,
		OperationType: "CreateIdentity",
		Actor:         "vtx.identity." + testNanoID2,
		SubmittedAt:   "2026-05-13T10:00:00Z",
		Payload:       []byte(`{}`),
	}
	now := time.Date(2026, 5, 13, 10, 0, 1, 0, time.UTC)
	tr := NewTracker(env, now)
	if tr.Key != "vtx.op."+testNanoID1 {
		t.Fatalf("tracker key wrong: %s", tr.Key)
	}
	if tr.Class != "op-tracker" {
		t.Fatalf("class wrong: %s", tr.Class)
	}
	if tr.IsDeleted {
		t.Fatalf("isDeleted should be false on a fresh tracker")
	}
	if tr.CreatedByOp != tr.Key {
		t.Fatalf("tracker should be self-referential")
	}
	b, err := tr.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	tr2, err := ParseTracker(b)
	if err != nil {
		t.Fatalf("ParseTracker: %v", err)
	}
	if tr2.CommittedAt() != tr.CommittedAt() {
		t.Fatalf("committedAt did not round-trip: %q vs %q", tr2.CommittedAt(), tr.CommittedAt())
	}
}
