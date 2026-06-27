package loom

import "testing"

// TestAggregateStatus locks the Contract #5 §5.2/§5.3 reconciliation: a heartbeat
// carrying issues can never self-report "healthy", lifecycle phases pass through,
// and error wins over warning. Mirrors the Processor/Weaver/Refractor heartbeaters.
func TestAggregateStatus(t *testing.T) {
	warn := healthIssue{Severity: "warning", Code: "ConsumerPaused", Message: "x"}
	errIssue := healthIssue{Severity: "error", Code: "Boom", Message: "y"}

	cases := []struct {
		name      string
		lifecycle string
		issues    []healthIssue
		want      string
	}{
		{"healthy no issues stays healthy", "healthy", nil, "healthy"},
		{"healthy with warning degrades", "healthy", []healthIssue{warn}, "degraded"},
		{"healthy with error is unhealthy", "healthy", []healthIssue{errIssue}, "unhealthy"},
		{"error wins over warning", "healthy", []healthIssue{warn, errIssue}, "unhealthy"},
		{"starting passes through despite issues", "starting", []healthIssue{warn, errIssue}, "starting"},
		{"shutdown passes through despite issues", "shutdown", []healthIssue{errIssue}, "shutdown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateStatus(tc.lifecycle, tc.issues); got != tc.want {
				t.Fatalf("aggregateStatus(%q, %v) = %q, want %q", tc.lifecycle, tc.issues, got, tc.want)
			}
		})
	}
}
