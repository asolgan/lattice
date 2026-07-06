package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestComputeFlows(t *testing.T) {
	store := map[string][]byte{
		// A completed flow.
		"complete0000000000": []byte(`{"instance_id":"complete0000000000","pattern_ref":"onboarding","subject_key":"vtx.identity.id1","status":"complete","started_at":"2026-07-05T10:00:00Z","ended_at":"2026-07-05T10:05:00Z","last_event_seq":42}`),
		// A failed flow.
		"failed00000000000": []byte(`{"instance_id":"failed00000000000","pattern_ref":"onboarding","subject_key":"vtx.identity.id2","status":"failed","started_at":"2026-07-05T09:00:00Z","ended_at":"2026-07-05T09:02:00Z","failure_reason":"adapter timeout","last_event_seq":10}`),
		// A running flow the live control read still reports.
		"running0000000000": []byte(`{"instance_id":"running0000000000","pattern_ref":"onboarding","subject_key":"vtx.identity.id3","status":"running","started_at":"2026-07-05T11:00:00Z","last_event_seq":5}`),
		// A running flow with NO matching live instance — orphaned.
		"orphan00000000000": []byte(`{"instance_id":"orphan00000000000","pattern_ref":"onboarding","subject_key":"vtx.identity.id4","status":"running","started_at":"2026-07-05T08:00:00Z","last_event_seq":3}`),
		// A poison entry that fails to decode — must be skipped, not fatal.
		"poison00000000000": []byte(`not json`),
	}
	get := func(key string) ([]byte, bool) { b, ok := store[key]; return b, ok }
	keys := make([]string, 0, len(store))
	for k := range store {
		keys = append(keys, k)
	}
	liveIDs := map[string]bool{"running0000000000": true}

	t.Run("all rows, poison skipped, newest-started first", func(t *testing.T) {
		rows := computeFlows(keys, get, liveIDs, true, "")
		if len(rows) != 4 {
			t.Fatalf("want 4 flows (poison entry skipped), got %d: %+v", len(rows), rows)
		}
		if rows[0].InstanceID != "running0000000000" {
			t.Errorf("newest-started flow should sort first, got %q", rows[0].InstanceID)
		}
	})

	t.Run("status filter limits to one status", func(t *testing.T) {
		rows := computeFlows(keys, get, liveIDs, true, "failed")
		if len(rows) != 1 {
			t.Fatalf("want 1 failed flow, got %d", len(rows))
		}
		if rows[0].FailureReason != "adapter timeout" {
			t.Errorf("failure reason not decoded: %q", rows[0].FailureReason)
		}
	})

	t.Run("running row badged live when the control read reports it", func(t *testing.T) {
		rows := computeFlows(keys, get, liveIDs, true, "running")
		if len(rows) != 2 {
			t.Fatalf("want 2 running flows, got %d", len(rows))
		}
		byID := map[string]flowRow{}
		for _, r := range rows {
			byID[r.InstanceID] = r
		}
		if byID["running0000000000"].Live == nil || !*byID["running0000000000"].Live {
			t.Errorf("running0000000000 should be badged live")
		}
		if byID["orphan00000000000"].Live == nil || *byID["orphan00000000000"].Live {
			t.Errorf("orphan00000000000 has no matching live instance; should be badged confirmed-not-live")
		}
	})

	t.Run("terminal row is never badged live even if liveIDs somehow names it", func(t *testing.T) {
		rows := computeFlows(keys, get, map[string]bool{"complete0000000000": true}, true, "complete")
		if len(rows) != 1 || rows[0].Live != nil {
			t.Fatalf("a terminal row must never be badged live, got %+v", rows)
		}
	})

	t.Run("running row stays unbadged (nil), not falsely orphaned, when the control read failed", func(t *testing.T) {
		rows := computeFlows(keys, get, nil, false, "running")
		for _, r := range rows {
			if r.Live != nil {
				t.Errorf("row %q should be unbadged when liveKnown=false, got %+v", r.InstanceID, *r.Live)
			}
		}
	})
}

func TestComputeTimeline(t *testing.T) {
	rfc := func(s string) time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm }
	store := map[string][]byte{
		// Fully inside the window.
		"inside000000000000": []byte(`{"instance_id":"inside000000000000","pattern_ref":"onboarding","status":"complete","started_at":"2026-07-05T10:10:00Z","ended_at":"2026-07-05T10:20:00Z"}`),
		// Ends before the window starts — no overlap.
		"before0000000000000": []byte(`{"instance_id":"before0000000000000","pattern_ref":"onboarding","status":"complete","started_at":"2026-07-05T09:00:00Z","ended_at":"2026-07-05T09:30:00Z"}`),
		// Starts after the window ends — no overlap.
		"after00000000000000": []byte(`{"instance_id":"after00000000000000","pattern_ref":"onboarding","status":"complete","started_at":"2026-07-05T11:30:00Z","ended_at":"2026-07-05T11:45:00Z"}`),
		// Still running (no ended_at) and started before the window — live
		// through the window's own end (treated as open).
		"running0000000000000": []byte(`{"instance_id":"running0000000000000","pattern_ref":"onboarding","status":"running","started_at":"2026-07-05T10:55:00Z"}`),
		// Unparsable started_at — skipped, never fatal to the rest.
		"badstart000000000000": []byte(`{"instance_id":"badstart000000000000","pattern_ref":"onboarding","status":"complete","started_at":"not-a-time","ended_at":"2026-07-05T10:15:00Z"}`),
	}
	get := func(key string) ([]byte, bool) { b, ok := store[key]; return b, ok }
	keys := make([]string, 0, len(store))
	for k := range store {
		keys = append(keys, k)
	}
	from, to := rfc("2026-07-05T10:00:00Z"), rfc("2026-07-05T11:00:00Z")

	rows := computeTimeline(keys, get, from, to)
	if len(rows) != 2 {
		t.Fatalf("want 2 overlapping flows, got %d: %+v", len(rows), rows)
	}
	byID := map[string]timelineFlow{}
	for _, r := range rows {
		byID[r.InstanceID] = r
	}
	if _, ok := byID["inside000000000000"]; !ok {
		t.Error("fully-inside flow missing from timeline")
	}
	if _, ok := byID["running0000000000000"]; !ok {
		t.Error("still-running flow missing from timeline")
	}
	if _, ok := byID["before0000000000000"]; ok {
		t.Error("flow that ended before the window should not overlap")
	}
	if _, ok := byID["after00000000000000"]; ok {
		t.Error("flow that started after the window should not overlap")
	}
	if _, ok := byID["badstart000000000000"]; ok {
		t.Error("unparsable started_at should be skipped")
	}
}

// TestHandleHistoryTimelineValidation pins that query validation runs BEFORE
// the requireConn guard: a malformed/inverted window answers 400 even with no
// NATS connection (testServer's nil-conn posture), never the misleading 502
// a conn-first check would give.
func TestHandleHistoryTimelineValidation(t *testing.T) {
	mux := testServer()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/history/timeline?from=garbage&to=2026-07-05T11:00:00Z", nil))
	if rec.Code != 400 {
		t.Errorf("malformed from = %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/history/timeline?from=2026-07-05T11:00:00Z&to=2026-07-05T10:00:00Z", nil))
	if rec.Code != 400 {
		t.Errorf("to before from = %d, want 400", rec.Code)
	}

	// Well-formed params fall through to requireConn — nil conn = 502.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/history/timeline?from=2026-07-05T10:00:00Z&to=2026-07-05T11:00:00Z", nil))
	if rec.Code != 502 {
		t.Errorf("valid params, nil conn = %d, want 502", rec.Code)
	}
}

func TestLiveLoomInstances(t *testing.T) {
	t.Run("decodes instanceId set from a raw list reply", func(t *testing.T) {
		raw := []byte(`{"instances":[{"instanceId":"a"},{"instanceId":"b"}]}`)
		ids := liveLoomInstances(raw)
		if !ids["a"] || !ids["b"] || len(ids) != 2 {
			t.Fatalf("unexpected ids: %+v", ids)
		}
	})

	t.Run("malformed or empty reply yields an empty set, never a panic", func(t *testing.T) {
		if ids := liveLoomInstances(nil); len(ids) != 0 {
			t.Errorf("nil reply should yield empty set, got %+v", ids)
		}
		if ids := liveLoomInstances([]byte(`not json`)); len(ids) != 0 {
			t.Errorf("malformed reply should yield empty set, got %+v", ids)
		}
	})
}
