package weaver

import (
	"testing"

	"github.com/asolgan/lattice/internal/substrate"
)

// TestDeriveEpisodeRequestID_Deterministic proves the OCC/idempotency seam of
// AC 5: a crash-sim re-fire of the SAME dispatch episode (same target, entity,
// gap, mark create revision) reproduces the identical requestId — collapsing
// on the Contract #4 tracker — while a legitimately re-opened gap (new
// CAS-create → new revision) yields a NEW requestId.
func TestDeriveEpisodeRequestID_Deterministic(t *testing.T) {
	a := deriveEpisodeRequestID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_onboarding", 7)
	b := deriveEpisodeRequestID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_onboarding", 7)
	if a != b {
		t.Fatalf("same episode must derive the same requestId: %q vs %q", a, b)
	}
	if !substrate.IsValidNanoID(a) {
		t.Fatalf("derived requestId %q is not a canonical 20-char NanoID", a)
	}

	reopened := deriveEpisodeRequestID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_onboarding", 8)
	if reopened == a {
		t.Fatalf("a re-opened gap (new mark revision) must derive a NEW requestId")
	}

	otherGap := deriveEpisodeRequestID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_payment", 7)
	if otherGap == a {
		t.Fatalf("a different gap column must derive a different requestId")
	}
	otherTarget := deriveEpisodeRequestID("targetB", "Lk2Pn6mQrtwzKbcXvP3T", "missing_onboarding", 7)
	if otherTarget == a {
		t.Fatalf("a different target must derive a different requestId")
	}
}

// TestDeriveEpisodeTaskID_DisjointFromRequestID proves the assignTask task-id
// namespace never collides with the op requestId namespace for the same
// episode (the CreateTask submission handle and the task identity are
// distinct).
func TestDeriveEpisodeTaskID_DisjointFromRequestID(t *testing.T) {
	req := deriveEpisodeRequestID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_signature", 3)
	task := deriveEpisodeTaskID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_signature", 3)
	if req == task {
		t.Fatalf("task id and op requestId must be namespace-disjoint, both were %q", req)
	}
	if !substrate.IsValidNanoID(task) {
		t.Fatalf("derived taskId %q is not a canonical 20-char NanoID", task)
	}
	again := deriveEpisodeTaskID("targetA", "Lk2Pn6mQrtwzKbcXvP3T", "missing_signature", 3)
	if task != again {
		t.Fatalf("same episode must re-supply the same taskId: %q vs %q", task, again)
	}
}
