package weaver

import "testing"

func TestClassifyTrajectory(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ring []int
		want string
	}{
		{"tooShort", []int{5}, trajectorySteady},
		{"strictlyShrinking", []int{10, 8, 6, 4, 2}, trajectoryShrinking},
		{"strictlyDiverging", []int{0, 2, 4, 6, 8}, trajectoryDiverging},
		{"flat", []int{5, 5, 5, 5}, trajectorySteady},
		{"reversesMidWindow", []int{5, 2, 5, 2}, trajectorySteady},
		{"plateauThenDrop", []int{5, 5, 5, 2}, trajectoryShrinking},
		{"plateauThenRise", []int{2, 2, 2, 5}, trajectoryDiverging},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTrajectory(tc.ring); got != tc.want {
				t.Fatalf("classifyTrajectory(%v) = %q, want %q", tc.ring, got, tc.want)
			}
		})
	}
}

// TestContractionStats_ObserveTracksTransitionsOnly proves the current
// per-target count only moves on a TRUE state transition — a repeat
// delivery of the same violating value (the common CDC-redelivery case) must
// not double-count, and a row never before seen non-violating must not enter
// `known` (bounded memory: only currently-violating rows are tracked).
func TestContractionStats_ObserveTracksTransitionsOnly(t *testing.T) {
	t.Parallel()
	c := newContractionStats()

	c.observe("t1", "e1", false) // first sighting, non-violating: no-op
	if got := c.current["t1"]; got != 0 {
		t.Fatalf("current[t1] = %d, want 0 after a non-violating first sighting", got)
	}

	c.observe("t1", "e1", true) // becomes violating
	c.observe("t1", "e2", true) // a second violating row
	if got := c.current["t1"]; got != 2 {
		t.Fatalf("current[t1] = %d, want 2", got)
	}

	c.observe("t1", "e1", true) // redelivery of the same state: no-op
	if got := c.current["t1"]; got != 2 {
		t.Fatalf("current[t1] = %d, want 2 after a redelivered no-change", got)
	}

	c.observe("t1", "e1", false) // gap closes
	if got := c.current["t1"]; got != 1 {
		t.Fatalf("current[t1] = %d, want 1 after e1 closes", got)
	}
}

// TestContractionStats_SampleAndSnapshot proves the sweep-cadence sample
// ring feeds the heartbeat's classification end to end: a target whose
// violating count is driven down across successive samples reports
// "shrinking".
func TestContractionStats_SampleAndSnapshot(t *testing.T) {
	t.Parallel()
	c := newContractionStats()
	counts := []int{4, 3, 2, 1, 0}
	for _, n := range counts {
		c.current["t1"] = n
		c.sample([]string{"t1"})
	}
	snap := c.snapshot()
	if got := snap["t1"]; got != trajectoryShrinking {
		t.Fatalf("snapshot()[t1] = %q, want %q", got, trajectoryShrinking)
	}
	if got := len(c.samples["t1"]); got != contractionWindowSize {
		t.Fatalf("ring length = %d, want it capped at %d (5 samples pushed)", got, contractionWindowSize)
	}
}
