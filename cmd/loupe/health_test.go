package main

import (
	"testing"
	"time"
)

func TestClassifyHealthKey(t *testing.T) {
	cases := []struct {
		key       string
		wantGroup string
		wantKind  string
	}{
		{"health.processor.proc-1", "processor", kindComponent},
		{"health.refractor.rfx-1", "refractor", kindComponent},
		{"health.loom.loom-1", "loom", kindComponent},
		{"health.weaver.weaver-1", "weaver", kindComponent},
		{"health.bridge.bridge-1", "bridge", kindComponent},
		{"health.object-store-manager.objmgr-1", "object-store-manager", kindComponent},
		{"health.bootstrap.complete", "bootstrap", kindBootstrap},
		{"health.gates.phase1.gate1", "gate", kindGate},
		{"health.alerts.security.stub-auth-active", "alert", kindAlert},
		{"health.processor.proc-1.event.deep", "processor", kindEvent},
		{"5BNztfjCmcyLcu9Js9XT", "lens", kindLens},
	}
	for _, c := range cases {
		g, k := classifyHealthKey(c.key)
		if g != c.wantGroup || k != c.wantKind {
			t.Errorf("classifyHealthKey(%q) = (%q,%q), want (%q,%q)", c.key, g, k, c.wantGroup, c.wantKind)
		}
	}
}

func TestComputeHealthComponentAndLens(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	docs := map[string]map[string]any{
		"health.processor.proc-1":              {"component": "processor", "instance": "proc-1", "heartbeatAt": now},
		"health.loom.loom-1":                   {"component": "loom", "instance": "loom-1", "heartbeatAt": now},
		"health.object-store-manager.objmgr-1": {"component": "object-store-manager", "instance": "objmgr-1", "updatedAt": now},
		"uhBwnSgiVAtRTswWuhBw":                 {"status": "active"},
		"health.bootstrap.complete":            {},
		"health.alerts.security.stub":          {"severity": "warning", "message": "stub auth"},
	}
	keys := make([]string, 0, len(docs))
	for k := range docs {
		keys = append(keys, k)
	}
	read := func(k string) (map[string]any, bool) { d, ok := docs[k]; return d, ok }
	resolve := func(id string) (string, string) {
		if id == "uhBwnSgiVAtRTswWuhBw" {
			return "objectLiveness", "object→owner liveness"
		}
		return "", ""
	}

	got := computeHealth(keys, read, resolve, 60*time.Second)

	byName := map[string]healthComponent{}
	for _, c := range got.Components {
		byName[c.Name] = c
	}

	// loom must now be its own component (not "lens"/"unknown"), green via heartbeat.
	loom, ok := byName["loom"]
	if !ok {
		t.Fatalf("loom component missing; components: %+v", got.Components)
	}
	if loom.Group != "loom" || loom.Status != "green" || loom.Detail != "loom-1" {
		t.Errorf("loom card = %+v, want group=loom status=green detail=loom-1", loom)
	}

	// object-store-manager uses updatedAt (not heartbeatAt) — still green.
	if objmgr := byName["object-store-manager"]; objmgr.Status != "green" {
		t.Errorf("object-store-manager status = %q, want green (via updatedAt)", objmgr.Status)
	}

	// the lens card resolves to a descriptive name + detail.
	lens, ok := byName["objectLiveness"]
	if !ok {
		t.Fatalf("resolved lens card missing; components: %+v", got.Components)
	}
	if lens.Group != "lens" || lens.Status != "active" || lens.Key != "uhBwnSgiVAtRTswWuhBw" {
		t.Errorf("lens card = %+v, want group=lens status=active key=<id>", lens)
	}
	if lens.Detail != "lens · object→owner liveness" {
		t.Errorf("lens detail = %q", lens.Detail)
	}

	// bootstrap present → not red; the warning alert → yellow rollup.
	if got.Overall != "yellow" {
		t.Errorf("overall = %q, want yellow (warning alert present)", got.Overall)
	}
	if len(got.Alerts) != 1 {
		t.Errorf("alerts = %v, want 1", got.Alerts)
	}
}

func TestComputeHealthLensFallsBackToID(t *testing.T) {
	docs := map[string]map[string]any{"AbcLensId0000000000": {"status": "active"}}
	read := func(k string) (map[string]any, bool) { d, ok := docs[k]; return d, ok }
	// resolve returns "" → card keeps the id as its name.
	got := computeHealth([]string{"AbcLensId0000000000"}, read, func(string) (string, string) { return "", "" }, time.Minute)
	if len(got.Components) != 1 || got.Components[0].Name != "AbcLensId0000000000" {
		t.Errorf("expected id fallback name; got %+v", got.Components)
	}
}

func TestComputeHealthMissingBootstrapIsRed(t *testing.T) {
	got := computeHealth(nil, func(string) (map[string]any, bool) { return nil, false }, nil, time.Minute)
	if got.Overall != "red" {
		t.Errorf("overall = %q, want red (no bootstrap marker)", got.Overall)
	}
}
