package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/operatinggraph/lattice/internal/healthkv"
	"github.com/operatinggraph/lattice/internal/substrate"
	"github.com/operatinggraph/lattice/internal/testutil"
)

func hasIssue(issues []healthkv.Issue, code string) bool {
	for _, iss := range issues {
		if iss.Code == code {
			return true
		}
	}
	return false
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHealthProbe_HostConnDown proves a nil (or disconnected) health
// connection is an error-severity issue — the reporter's own dependency
// signal, same fold as every other component's NatsUnreachable.
func TestHealthProbe_HostConnDown(t *testing.T) {
	s := &server{logger: discardLogger(), engines: newEngineManager(context.Background(), engineManagerDeps{})}
	snap := s.healthProbe(context.Background(), nil)
	if snap.Status != healthkv.StatusUnhealthy {
		t.Fatalf("status = %v, want unhealthy", snap.Status)
	}
	if !hasIssue(snap.Issues, "NatsUnreachable") {
		t.Errorf("issues = %+v, want NatsUnreachable", snap.Issues)
	}
}

// TestHealthProbe_BrowserNativeModeNoFabricatedCounts proves browser-native
// mode reports its mode metric and never fabricates engine-fleet counts it
// cannot see (design §4.3/§7 non-goal) — engines_active etc. must be absent,
// not zero, since zero would misleadingly claim visibility.
func TestHealthProbe_BrowserNativeModeNoFabricatedCounts(t *testing.T) {
	s := &server{
		logger:        discardLogger(),
		browserEngine: &browserEngineConfig{},
	}
	url := testutil.StartEmbeddedNATS(t)
	conn, err := substrate.Connect(context.Background(), substrate.ConnectOpts{URL: url})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	snap := s.healthProbe(context.Background(), conn)
	if snap.Metrics["mode"] != "browser-native" {
		t.Errorf("metrics[mode] = %v, want browser-native", snap.Metrics["mode"])
	}
	for _, key := range []string{"engines_active", "engines_pinned", "engines_sync_degraded", "engines_nats_disconnected"} {
		if _, ok := snap.Metrics[key]; ok {
			t.Errorf("metrics[%s] present in browser-native mode, want absent (host cannot see in-page engines)", key)
		}
	}
}

// TestHealthProbe_EngineSyncDegraded proves a crash-looping engine's sticky
// syncDegraded bit surfaces as the row's demanded signal — degraded status,
// EngineSyncDegraded issue, correct aggregate count — and that the marshaled
// heartbeat carries no per-identity detail (design §8 finding #2/§9).
func TestHealthProbe_EngineSyncDegraded(t *testing.T) {
	url := testutil.StartEmbeddedNATS(t)
	conn, err := substrate.Connect(context.Background(), substrate.ConnectOpts{URL: url})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	healthConn, err := substrate.Connect(context.Background(), substrate.ConnectOpts{URL: url})
	if err != nil {
		t.Fatalf("connect health: %v", err)
	}
	defer healthConn.Close()

	m := &engineManager{entries: make(map[string]*engineEntry)}
	degradedFeed := newFeed(nil)
	degradedFeed.setSyncDegraded(true)
	healthyFeed := newFeed(nil)

	m.entries["identity-with-a-very-real-nanoid1"] = &engineEntry{eng: &engine{conn: conn, feed: degradedFeed}}
	m.entries["identity-with-a-very-real-nanoid2"] = &engineEntry{eng: &engine{conn: conn, feed: healthyFeed}}

	s := &server{logger: discardLogger(), engines: m}
	snap := s.healthProbe(context.Background(), healthConn)

	if !hasIssue(snap.Issues, "EngineSyncDegraded") {
		t.Errorf("issues = %+v, want EngineSyncDegraded", snap.Issues)
	}
	if snap.Status != healthkv.StatusDegraded {
		t.Errorf("status = %v, want degraded", snap.Status)
	}
	if got := snap.Metrics["engines_sync_degraded"]; got != 1 {
		t.Errorf("metrics[engines_sync_degraded] = %v, want 1", got)
	}
	if got := snap.Metrics["engines_active"]; got != 2 {
		t.Errorf("metrics[engines_active] = %v, want 2", got)
	}

	doc, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for id := range m.entries {
		if strings.Contains(string(doc), id) {
			t.Errorf("marshaled heartbeat contains an identity id %q — must be aggregate-only", id)
		}
	}
}

// TestHealthProbe_EngineNatsDisconnected proves the connectivity axis is
// counted distinctly from the sync-loop-crash axis (ce050a7 deliberately
// separated them) — a closed per-identity connection with a HEALTHY feed
// still surfaces as EngineNatsDisconnected, not EngineSyncDegraded.
func TestHealthProbe_EngineNatsDisconnected(t *testing.T) {
	url := testutil.StartEmbeddedNATS(t)
	closedConn, err := substrate.Connect(context.Background(), substrate.ConnectOpts{URL: url})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closedConn.Close()

	healthConn, err := substrate.Connect(context.Background(), substrate.ConnectOpts{URL: url})
	if err != nil {
		t.Fatalf("connect health: %v", err)
	}
	defer healthConn.Close()

	m := &engineManager{entries: make(map[string]*engineEntry)}
	m.entries["identity1"] = &engineEntry{eng: &engine{conn: closedConn, feed: newFeed(nil)}}

	s := &server{logger: discardLogger(), engines: m}
	snap := s.healthProbe(context.Background(), healthConn)

	if !hasIssue(snap.Issues, "EngineNatsDisconnected") {
		t.Errorf("issues = %+v, want EngineNatsDisconnected", snap.Issues)
	}
	if hasIssue(snap.Issues, "EngineSyncDegraded") {
		t.Errorf("issues = %+v, want no EngineSyncDegraded (feed is healthy)", snap.Issues)
	}
	if got := snap.Metrics["engines_nats_disconnected"]; got != 1 {
		t.Errorf("metrics[engines_nats_disconnected] = %v, want 1", got)
	}
}
