package healthkv

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestReporter_EnvelopeShape(t *testing.T) {
	ctx, conn := setupHarness(t)
	r := New(Config{
		Conn:      conn,
		Bucket:    testHealthBucket,
		Component: "test-app",
		Instance:  "test-inst-1",
		Interval:  10 * time.Second,
		Probe: func(context.Context) Snapshot {
			return Snapshot{Status: StatusHealthy, Metrics: map[string]any{"ops_total": float64(3)}}
		},
	})

	if r.cfg.TTL != 100*time.Second {
		t.Fatalf("TTL default = %v, want 100s (Interval*10)", r.cfg.TTL)
	}

	r.tick(ctx)

	entry, err := conn.KVGet(ctx, testHealthBucket, "health.test-app.test-inst-1")
	if err != nil {
		t.Fatalf("KVGet: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range []string{"key", "component", "instance", "version", "status", "heartbeatAt", "startedAt", "uptime", "metrics", "issues"} {
		if _, ok := doc[field]; !ok {
			t.Errorf("missing required §5.2 field %q", field)
		}
	}
	if doc["key"] != "health.test-app.test-inst-1" {
		t.Errorf("key = %v, want health.test-app.test-inst-1", doc["key"])
	}
	if doc["component"] != "test-app" || doc["instance"] != "test-inst-1" {
		t.Errorf("component/instance = %v/%v", doc["component"], doc["instance"])
	}
	if doc["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", doc["status"])
	}
	metrics, _ := doc["metrics"].(map[string]any)
	if metrics["ops_total"] != float64(3) {
		t.Errorf("metrics.ops_total = %v, want 3", metrics["ops_total"])
	}
	if issues, ok := doc["issues"].([]any); !ok || len(issues) != 0 {
		t.Errorf("issues = %v, want empty array", doc["issues"])
	}
}

func TestReporter_SinceContinuity(t *testing.T) {
	ctx, conn := setupHarness(t)
	var issues []Issue
	r := New(Config{
		Conn:      conn,
		Bucket:    testHealthBucket,
		Component: "test-app",
		Instance:  "test-inst-2",
		Interval:  10 * time.Second,
		Probe: func(context.Context) Snapshot {
			return Snapshot{Status: StatusDegraded, Issues: issues}
		},
	})

	readSince := func(code string) (string, bool) {
		entry, err := conn.KVGet(ctx, testHealthBucket, "health.test-app.test-inst-2")
		if err != nil {
			t.Fatalf("KVGet: %v", err)
		}
		var doc struct {
			Issues []Issue `json:"issues"`
		}
		if err := json.Unmarshal(entry.Value, &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, iss := range doc.Issues {
			if iss.Code == code {
				return iss.Since, true
			}
		}
		return "", false
	}

	// Tick 1: issue X appears.
	issues = []Issue{{Code: "X", Severity: "warning", Message: "m1"}}
	r.tick(ctx)
	since1, ok := readSince("X")
	if !ok {
		t.Fatalf("issue X missing on tick 1")
	}

	// Tick 2: issue X persists — Since must be unchanged.
	r.tick(ctx)
	since2, ok := readSince("X")
	if !ok || since2 != since1 {
		t.Fatalf("issue X Since changed across ticks: %q -> %q", since1, since2)
	}

	// Tick 3: issue X resolves (no longer reported).
	issues = nil
	r.tick(ctx)
	if _, ok := readSince("X"); ok {
		t.Fatalf("resolved issue X still present on tick 3")
	}

	// Tick 4: issue X re-appears — must get a fresh Since (not the stale one).
	issues = []Issue{{Code: "X", Severity: "warning", Message: "m4"}}
	r.tick(ctx)
	since4, ok := readSince("X")
	if !ok {
		t.Fatalf("issue X missing on tick 4")
	}
	if since4 == since1 {
		t.Fatalf("re-appearing issue X kept the stale Since %q instead of a fresh one", since1)
	}
}

func TestReporter_StartingAndShuttingDown(t *testing.T) {
	ctx, conn := setupHarness(t)
	r := New(Config{
		Conn:      conn,
		Bucket:    testHealthBucket,
		Component: "test-app",
		Instance:  "test-inst-3",
		Interval:  10 * time.Second,
		Probe:     func(context.Context) Snapshot { return Snapshot{Status: StatusHealthy} },
	})

	readStatus := func() string {
		entry, err := conn.KVGet(ctx, testHealthBucket, "health.test-app.test-inst-3")
		if err != nil {
			t.Fatalf("KVGet: %v", err)
		}
		var doc struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(entry.Value, &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return doc.Status
	}

	r.emitFixed(ctx, StatusStarting)
	if got := readStatus(); got != "starting" {
		t.Fatalf("status after emitFixed(starting) = %q, want starting", got)
	}

	r.emitFixed(ctx, StatusShuttingDown)
	if got := readStatus(); got != "shuttingDown" {
		t.Fatalf("status after emitFixed(shuttingDown) = %q, want shuttingDown", got)
	}
}

func TestReporter_ProbePanicRecovered(t *testing.T) {
	ctx, conn := setupHarness(t)
	r := New(Config{
		Conn:      conn,
		Bucket:    testHealthBucket,
		Component: "test-app",
		Instance:  "test-inst-4",
		Interval:  10 * time.Second,
		Probe: func(context.Context) Snapshot {
			panic("boom")
		},
	})

	r.tick(ctx)

	entry, err := conn.KVGet(ctx, testHealthBucket, "health.test-app.test-inst-4")
	if err != nil {
		t.Fatalf("KVGet: %v", err)
	}
	var doc struct {
		Status string  `json:"status"`
		Issues []Issue `json:"issues"`
	}
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Status != "unhealthy" {
		t.Fatalf("status after panicking probe = %q, want unhealthy", doc.Status)
	}
	if len(doc.Issues) != 1 || doc.Issues[0].Code != "HealthProbePanicked" {
		t.Fatalf("issues = %+v, want one HealthProbePanicked issue", doc.Issues)
	}
}

func TestReporter_NilMetricsMarshalsAsEmptyObjectNotNull(t *testing.T) {
	ctx, conn := setupHarness(t)
	r := New(Config{
		Conn:      conn,
		Bucket:    testHealthBucket,
		Component: "test-app",
		Instance:  "test-inst-5",
		Interval:  10 * time.Second,
		Probe:     func(context.Context) Snapshot { return Snapshot{Status: StatusHealthy} }, // Metrics nil
	})
	r.tick(ctx)

	entry, err := conn.KVGet(ctx, testHealthBucket, "health.test-app.test-inst-5")
	if err != nil {
		t.Fatalf("KVGet: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(doc["metrics"]) != "{}" {
		t.Fatalf("metrics = %s, want {} (nil Snapshot.Metrics should not marshal as null)", doc["metrics"])
	}
}

func TestReporter_TTLResolution(t *testing.T) {
	r := New(Config{Component: "x", Instance: "y", Interval: 5 * time.Second})
	if r.cfg.TTL != 50*time.Second {
		t.Fatalf("default TTL = %v, want 50s (Interval*10)", r.cfg.TTL)
	}

	r2 := New(Config{Component: "x", Instance: "y", Interval: 5 * time.Second, TTL: -1})
	if r2.cfg.TTL != 0 {
		t.Fatalf("negative TTL should resolve to 0 (disabled), got %v", r2.cfg.TTL)
	}
}
