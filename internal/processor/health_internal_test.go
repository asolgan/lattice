package processor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// fakeBacklogReader satisfies LaneBacklogReader for the per-lane heartbeat
// lane_lag tests: PendingForConsumer returns the configured backlog for a
// durable (absent ⇒ 0, the empty-but-readable case), or an error when the
// global err is set or a per-durable error is configured.
type fakeBacklogReader struct {
	pending map[string]uint64
	err     error
	errFor  map[string]error
}

func (f fakeBacklogReader) PendingForConsumer(_ context.Context, durable string) (uint64, error) {
	if f.err != nil {
		return 0, f.err
	}
	if f.errFor != nil {
		if e := f.errFor[durable]; e != nil {
			return 0, e
		}
	}
	return f.pending[durable], nil
}

func newTestHeartbeater() *HealthHeartbeater {
	return NewHealthHeartbeater(nil, "health", "proc-test", 10*time.Second, &Metrics{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// aggregateStatus reconciles the lifecycle phase with the open issue set:
// "starting"/"shuttingDown" pass through; otherwise any error ⇒ unhealthy, any
// warning ⇒ degraded, else the lifecycle status is kept (Contract #5 §5.3).
func TestAggregateStatus(t *testing.T) {
	warn := []healthIssue{{Severity: "warning", Code: "ProcessorLaneLagging"}}
	errIss := []healthIssue{{Severity: "error", Code: "CoreKVUnwritable"}}
	both := []healthIssue{{Severity: "warning"}, {Severity: "error"}}

	cases := []struct {
		name      string
		lifecycle string
		issues    []healthIssue
		want      string
	}{
		{"healthy no issues", "healthy", nil, "healthy"},
		{"healthy with warning", "healthy", warn, "degraded"},
		{"healthy with error", "healthy", errIss, "unhealthy"},
		{"error wins over warning", "healthy", both, "unhealthy"},
		{"starting protected from warning", "starting", warn, "starting"},
		{"starting protected from error", "starting", errIss, "starting"},
		{"shuttingDown protected", "shuttingDown", errIss, "shuttingDown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateStatus(tc.lifecycle, tc.issues); got != tc.want {
				t.Fatalf("aggregateStatus(%q, %v) = %q, want %q", tc.lifecycle, tc.issues, got, tc.want)
			}
		})
	}
}

// reconcileIssues must carry an issue key's since timestamp across heartbeats
// while it stays open and drop it once it resolves (Contract #5 §5.5). The key
// is per-lane so two lanes sharing a code track since independently.
func TestReconcileIssuesSincePersistence(t *testing.T) {
	h := newTestHeartbeater()
	t1 := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(30 * time.Second)

	active := map[string]activeIssue{
		"ProcessorLaneLagging:default": {code: "ProcessorLaneLagging", severity: "warning", message: "lagging"},
	}

	first := h.reconcileIssues(active, t1)
	if len(first) != 1 {
		t.Fatalf("first tick: got %d issues, want 1", len(first))
	}
	since := first[0].Since
	if first[0].Code != "ProcessorLaneLagging" {
		t.Fatalf("emitted code = %q, want ProcessorLaneLagging", first[0].Code)
	}

	second := h.reconcileIssues(active, t2)
	if len(second) != 1 {
		t.Fatalf("second tick: got %d issues, want 1", len(second))
	}
	if second[0].Since != since {
		t.Fatalf("since not persisted: first %q, second %q", since, second[0].Since)
	}

	resolved := h.reconcileIssues(map[string]activeIssue{}, t2.Add(10*time.Second))
	if len(resolved) != 0 {
		t.Fatalf("resolved tick: got %d issues, want 0", len(resolved))
	}
	if _, ok := h.openIssues["ProcessorLaneLagging:default"]; ok {
		t.Fatalf("resolved key still tracked in openIssues")
	}

	// A re-occurrence after resolve must get a fresh since, not the stale one.
	reopened := h.reconcileIssues(active, t2.Add(time.Minute))
	if reopened[0].Since == since {
		t.Fatalf("reopened issue reused stale since %q", since)
	}
}

// laneLag returns the per-lane lane_lag map, asserting its presence and that all
// four lane keys exist.
func laneLag(t *testing.T, doc HealthDoc) map[string]any {
	t.Helper()
	lanes, ok := doc.Metrics["lane_lag"].(map[string]any)
	if !ok {
		t.Fatalf("lane_lag not a map: %T", doc.Metrics["lane_lag"])
	}
	for _, lane := range []string{"default", "meta", "urgent", "system"} {
		if _, present := lanes[lane]; !present {
			t.Fatalf("lane_lag missing lane %q", lane)
		}
	}
	return lanes
}

func laneLagTotal(t *testing.T, doc HealthDoc) any {
	t.Helper()
	v, ok := doc.Metrics["lane_lag_total"]
	if !ok {
		t.Fatalf("metrics missing lane_lag_total")
	}
	return v
}

// buildHealthDoc reports each lane's real backlog in lane_lag.<lane>, sums the
// readable lanes into lane_lag_total, raises a per-lane ProcessorLaneLagging
// (degraded) past the threshold, and reports null rather than a fabricated zero
// for any lane (or all lanes) whose backlog can't be read.
func TestBuildHealthDocLaneLag(t *testing.T) {
	ctx := context.Background()

	t.Run("no reader attached → all lanes null, total null, healthy", func(t *testing.T) {
		h := newTestHeartbeater()
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		for lane, v := range laneLag(t, doc) {
			if v != nil {
				t.Fatalf("lane_lag[%q] = %v, want nil (no reader)", lane, v)
			}
		}
		if total := laneLagTotal(t, doc); total != nil {
			t.Fatalf("lane_lag_total = %v, want nil", total)
		}
		if doc.Status != "healthy" || len(doc.Issues) != 0 {
			t.Fatalf("status=%q issues=%d, want healthy/0", doc.Status, len(doc.Issues))
		}
	})

	t.Run("all lanes readable below threshold → per-lane + summed total, healthy", func(t *testing.T) {
		h := newTestHeartbeater()
		h.AttachBacklogReader(fakeBacklogReader{pending: map[string]uint64{
			"processor-default": 5, "processor-system": 3,
		}}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		lanes := laneLag(t, doc)
		if lanes["default"] != uint64(5) || lanes["system"] != uint64(3) ||
			lanes["urgent"] != uint64(0) || lanes["meta"] != uint64(0) {
			t.Fatalf("per-lane lane_lag = %+v, want default=5 system=3 urgent=0 meta=0", lanes)
		}
		if total := laneLagTotal(t, doc); total != uint64(8) {
			t.Fatalf("lane_lag_total = %v, want 8", total)
		}
		if doc.Status != "healthy" || len(doc.Issues) != 0 {
			t.Fatalf("status=%q issues=%d, want healthy/0", doc.Status, len(doc.Issues))
		}
	})

	t.Run("one lane above threshold → that lane lagging, degraded", func(t *testing.T) {
		h := newTestHeartbeater()
		h.AttachBacklogReader(fakeBacklogReader{pending: map[string]uint64{
			"processor-default": 250,
		}}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		if lanes := laneLag(t, doc); lanes["default"] != uint64(250) {
			t.Fatalf("lane_lag[default] = %v, want 250", lanes["default"])
		}
		if total := laneLagTotal(t, doc); total != uint64(250) {
			t.Fatalf("lane_lag_total = %v, want 250", total)
		}
		if doc.Status != "degraded" {
			t.Fatalf("status = %q, want degraded", doc.Status)
		}
		if len(doc.Issues) != 1 || doc.Issues[0].Code != "ProcessorLaneLagging" || doc.Issues[0].Severity != "warning" {
			t.Fatalf("issues = %+v, want one ProcessorLaneLagging warning", doc.Issues)
		}
		if doc.Issues[0].Since == "" {
			t.Fatalf("issue missing since timestamp")
		}
	})

	t.Run("two lanes lagging → two independent issues, degraded", func(t *testing.T) {
		h := newTestHeartbeater()
		h.AttachBacklogReader(fakeBacklogReader{pending: map[string]uint64{
			"processor-default": 250, "processor-urgent": 300,
		}}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		if doc.Status != "degraded" {
			t.Fatalf("status = %q, want degraded", doc.Status)
		}
		if len(doc.Issues) != 2 {
			t.Fatalf("issues = %+v, want 2 (one per lagging lane)", doc.Issues)
		}
		for _, is := range doc.Issues {
			if is.Code != "ProcessorLaneLagging" || is.Severity != "warning" || is.Since == "" {
				t.Fatalf("issue %+v not a well-formed ProcessorLaneLagging warning", is)
			}
		}
		if total := laneLagTotal(t, doc); total != uint64(550) {
			t.Fatalf("lane_lag_total = %v, want 550", total)
		}
	})

	t.Run("all lanes unreadable → total null, no fabrication, healthy", func(t *testing.T) {
		h := newTestHeartbeater()
		h.AttachBacklogReader(fakeBacklogReader{err: errors.New("server unreachable")}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		for lane, v := range laneLag(t, doc) {
			if v != nil {
				t.Fatalf("lane_lag[%q] = %v, want nil on Info error", lane, v)
			}
		}
		if total := laneLagTotal(t, doc); total != nil {
			t.Fatalf("lane_lag_total = %v, want nil when no lane readable", total)
		}
		if doc.Status != "healthy" || len(doc.Issues) != 0 {
			t.Fatalf("status=%q issues=%d, want healthy/0 (can't assess)", doc.Status, len(doc.Issues))
		}
	})

	t.Run("partial readability → readable lanes summed, unreadable lane null", func(t *testing.T) {
		h := newTestHeartbeater()
		h.AttachBacklogReader(fakeBacklogReader{
			pending: map[string]uint64{"processor-default": 5},
			errFor:  map[string]error{"processor-urgent": errors.New("urgent unreadable")},
		}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		lanes := laneLag(t, doc)
		if lanes["default"] != uint64(5) || lanes["system"] != uint64(0) || lanes["meta"] != uint64(0) {
			t.Fatalf("readable lanes = %+v, want default=5 system=0 meta=0", lanes)
		}
		if lanes["urgent"] != nil {
			t.Fatalf("lane_lag[urgent] = %v, want nil (unreadable)", lanes["urgent"])
		}
		if total := laneLagTotal(t, doc); total != uint64(5) {
			t.Fatalf("lane_lag_total = %v, want 5 (sum of readable lanes)", total)
		}
		if doc.Status != "healthy" {
			t.Fatalf("status = %q, want healthy", doc.Status)
		}
	})

	t.Run("starting lifecycle protected even when lagging", func(t *testing.T) {
		h := newTestHeartbeater()
		h.AttachBacklogReader(fakeBacklogReader{pending: map[string]uint64{
			"processor-default": 250,
		}}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "starting", time.Now())
		if doc.Status != "starting" {
			t.Fatalf("status = %q, want starting (protected)", doc.Status)
		}
	})

	t.Run("custom threshold via SetLagThreshold", func(t *testing.T) {
		h := newTestHeartbeater()
		h.SetLagThreshold(10)
		h.AttachBacklogReader(fakeBacklogReader{pending: map[string]uint64{
			"processor-default": 20,
		}}, LaneDurables())
		doc := h.buildHealthDoc(ctx, "healthy", time.Now())
		if doc.Status != "degraded" {
			t.Fatalf("status = %q, want degraded at custom threshold 10/pending 20", doc.Status)
		}
	})
}

// emitted issues must marshal as a JSON array (never null) so a §5.5 reader sees
// an empty list when healthy.
func TestHealthDocIssuesMarshalAsArray(t *testing.T) {
	h := newTestHeartbeater()
	doc := h.buildHealthDoc(context.Background(), "healthy", time.Now())
	if doc.Issues == nil {
		t.Fatalf("Issues is nil; must be an empty slice to marshal as []")
	}
}
