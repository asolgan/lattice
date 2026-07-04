package main

import (
	"testing"
)

// The Gateway-page logic tier (F11): the auth-failure ratio, JWKS shaping,
// revocation status line, and the revoke-form input rules — asserted against
// the shipped embedded asset via the goja harness.

func TestAuthFailureRate(t *testing.T) {
	vm := logicVM(t, "gateway.js")

	// No traffic → pct null (renders "—"), never a fabricated 0%.
	res := call(t, vm, "authFailureRate", map[string]any{}).(map[string]any)
	if res["pct"] != nil || res["cls"] != "muted" {
		t.Errorf("no-traffic rate = %+v, want pct nil / muted", res)
	}
	res = call(t, vm, "authFailureRate", nil).(map[string]any)
	if res["pct"] != nil {
		t.Errorf("nil metrics rate = %+v, want pct nil", res)
	}
	// requests present but failures missing/mistyped → unknown, never a
	// fabricated green 0%.
	res = call(t, vm, "authFailureRate", map[string]any{"requests_total": 100}).(map[string]any)
	if res["pct"] != nil || res["cls"] != "muted" {
		t.Errorf("missing failures counter = %+v, want pct nil / muted", res)
	}
	res = call(t, vm, "authFailureRate", map[string]any{"requests_total": 100, "auth_failures_total": "3"}).(map[string]any)
	if res["pct"] != nil {
		t.Errorf("mistyped failures counter = %+v, want pct nil", res)
	}

	// Under the 20% floor → ok; at/above → warn.
	res = call(t, vm, "authFailureRate", map[string]any{"requests_total": 100, "auth_failures_total": 5}).(map[string]any)
	if res["pct"].(float64) != 0.05 || res["cls"] != "ok" {
		t.Errorf("5%% rate = %+v, want 0.05/ok", res)
	}
	res = call(t, vm, "authFailureRate", map[string]any{"requests_total": 10, "auth_failures_total": 2}).(map[string]any)
	if res["cls"] != "warn" {
		t.Errorf("20%% rate = %+v, want warn", res)
	}

	// pctLabel renders the two shapes.
	if got := call(t, vm, "pctLabel", res); got != "20%" {
		t.Errorf("pctLabel(20%%) = %v", got)
	}
	if got := call(t, vm, "pctLabel", map[string]any{"pct": nil, "cls": "muted"}); got != "—" {
		t.Errorf("pctLabel(no traffic) = %v, want —", got)
	}
	// A nonzero ratio never rounds down to a clean "0%".
	if got := call(t, vm, "pctLabel", map[string]any{"pct": 0.0002, "cls": "ok"}); got != "<0.1%" {
		t.Errorf("pctLabel(tiny nonzero) = %v, want <0.1%%", got)
	}
}

func TestJwksRows(t *testing.T) {
	vm := logicVM(t, "gateway.js")

	// No jwks block → null (the designed empty state, not zeros).
	if got := call(t, vm, "jwksRows", map[string]any{"metrics": map[string]any{}}); got != nil {
		t.Errorf("jwksRows without block = %v, want null", got)
	}
	if got := call(t, vm, "jwksRows", nil); got != nil {
		t.Errorf("jwksRows(nil) = %v, want null", got)
	}
	// A malformed (array) block is not-reported, never a false
	// "no trusted keys" alarm.
	if got := call(t, vm, "jwksRows", map[string]any{"jwks": []any{}}); got != nil {
		t.Errorf("jwksRows(array block) = %v, want null", got)
	}

	doc := map[string]any{"jwks": map[string]any{
		"keys": []any{
			map[string]any{"kid": "zeta", "source": "url", "alg": "RS256", "addedAt": "2026-07-01T00:00:00Z"},
			map[string]any{"kid": "alpha", "source": "dev", "alg": "ES256"},
		},
		"lastPoll": map[string]any{"at": "2026-07-03T10:00:00Z", "source": "idp.example", "ok": true},
		"swaps":    []any{"s1", "s2", "s3"},
	}}
	res := call(t, vm, "jwksRows", doc).(map[string]any)
	keys := res["keys"].([]any)
	if len(keys) != 2 {
		t.Fatalf("keys = %+v, want 2", keys)
	}
	// Sorted by kid: alpha before zeta; absent addedAt renders empty.
	k0 := keys[0].(map[string]any)
	if k0["kid"] != "alpha" || k0["source"] != "dev" || k0["addedAt"] != "" {
		t.Errorf("key 0 = %+v, want alpha/dev", k0)
	}
	poll := res["poll"].(map[string]any)
	if poll["cls"] != "ok" {
		t.Errorf("healthy poll = %+v, want ok", poll)
	}
	// Swaps render newest-first.
	swaps := res["swaps"].([]any)
	if len(swaps) != 3 || swaps[0] != "s3" {
		t.Errorf("swaps = %+v, want newest-first s3", swaps)
	}

	// A failed poll warns and says last-known-good is serving.
	doc["jwks"].(map[string]any)["lastPoll"].(map[string]any)["ok"] = false
	res = call(t, vm, "jwksRows", doc).(map[string]any)
	if res["poll"].(map[string]any)["cls"] != "warn" {
		t.Errorf("failed poll = %+v, want warn", res["poll"])
	}

	// A static (dir/dev) set has no lastPoll → the restart-to-rotate line.
	res = call(t, vm, "jwksRows", map[string]any{"jwks": map[string]any{"keys": []any{}}}).(map[string]any)
	if res["poll"].(map[string]any)["cls"] != "muted" {
		t.Errorf("static poll line = %+v, want muted", res["poll"])
	}
}

func TestRevocationStatus(t *testing.T) {
	vm := logicVM(t, "gateway.js")

	// A missing (or malformed) block means an old Gateway build — say so.
	res := call(t, vm, "revocationStatus", map[string]any{}).(map[string]any)
	if res["cls"] != "muted" || res["connected"] != false {
		t.Errorf("missing block = %+v, want muted/disconnected", res)
	}
	res = call(t, vm, "revocationStatus", map[string]any{"revocation": []any{}}).(map[string]any)
	if res["cls"] != "muted" {
		t.Errorf("array block = %+v, want the not-reported line", res)
	}

	res = call(t, vm, "revocationStatus", map[string]any{"revocation": map[string]any{
		"consumerConnected": true, "revokedCount": 2, "lastSyncAt": "2026-07-03T10:00:00Z",
	}}).(map[string]any)
	if res["cls"] != "ok" || res["connected"] != true {
		t.Errorf("connected = %+v, want ok", res)
	}
	line := res["line"].(string)
	if line != "materializer connected · 2 revoked · last sync 2026-07-03T10:00:00Z" {
		t.Errorf("line = %q", line)
	}

	// Disconnected warns — the fail-safe lag window is operator-visible.
	res = call(t, vm, "revocationStatus", map[string]any{"revocation": map[string]any{
		"consumerConnected": false, "revokedCount": 0,
	}}).(map[string]any)
	if res["cls"] != "warn" {
		t.Errorf("disconnected = %+v, want warn", res)
	}
}

func TestRevokeInputRules(t *testing.T) {
	vm := logicVM(t, "gateway.js")

	valid := []string{"vtx.identity.abc123", "  vtx.identity.x  "}
	for _, v := range valid {
		if call(t, vm, "revokeActorValid", v) != true {
			t.Errorf("revokeActorValid(%q) = false, want true", v)
		}
	}
	// The id segment is NanoID-charset-only: a looser string commits as an op
	// but the Gateway materializer's KVPut refuses it and would redeliver
	// forever, so the console must reject it up front.
	invalid := []any{"", "vtx.identity.", "vtx.task.abc", "identity.abc", "vtx.identity.a.b",
		"vtx.identity.a b", "vtx.identity.*", "vtx.identity.>", "vtx.identity.a%b", nil, 42}
	for _, v := range invalid {
		if call(t, vm, "revokeActorValid", v) != false {
			t.Errorf("revokeActorValid(%v) = true, want false", v)
		}
	}

	if call(t, vm, "revokeConfirmReady", " vtx.identity.a ", "vtx.identity.a") != true {
		t.Error("trimmed exact match should confirm")
	}
	if call(t, vm, "revokeConfirmReady", "vtx.identity.b", "vtx.identity.a") != false {
		t.Error("mismatch must not confirm")
	}
	if call(t, vm, "revokeConfirmReady", "", "") != false {
		t.Error("empty actor must never confirm")
	}
}
