package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The hosted-demo posture (F20). The assertions that matter are the two
// fail-closed ones: an unknown/future route is denied by METHOD rather than by
// a path list that could go stale, and the boot guard REFUSES (not warns) when
// the console would run the demo as the bootstrap admin.

func TestDemoWriteDeniedByMethodNotPath(t *testing.T) {
	// Reads pass, whatever the path.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/systemmap"},
		{http.MethodGet, "/"},
		{http.MethodHead, "/api/health"},
		{http.MethodGet, "/api/events/stream"},
	} {
		if demoWriteDenied(tc.method, tc.path) {
			t.Errorf("demoWriteDenied(%s, %s) = true, want false (a read)", tc.method, tc.path)
		}
	}

	// Every known write is denied.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/op"},
		{http.MethodPost, "/api/packages/install"},
		{http.MethodPost, "/api/packages/uninstall"},
		{http.MethodPost, "/api/vault/decrypt"},
		{http.MethodPost, "/api/review/capability/x/approve"},
		{http.MethodPost, "/api/control/weaver"},
		{http.MethodPost, "/api/objects"},
		{http.MethodDelete, "/api/objects/abc"},
	} {
		if !demoWriteDenied(tc.method, tc.path) {
			t.Errorf("demoWriteDenied(%s, %s) = false, want true (a write)", tc.method, tc.path)
		}
	}

	// The point of the method rule: a route this fire has never heard of is
	// denied too. A path allowlist would fail OPEN here.
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		if !demoWriteDenied(m, "/api/some/future/write") {
			t.Errorf("demoWriteDenied(%s, /api/some/future/write) = false, want true (fail-closed for unknown routes)", m)
		}
	}
}

func TestDemoAllowsCredentialExchange(t *testing.T) {
	// A visitor must still be able to log in and out, so exactly these three
	// POSTs pass. None mutates platform state.
	for _, p := range []string{operatorDevTokenPath, operatorSessionPath, operatorLogoutPath} {
		if demoWriteDenied(http.MethodPost, p) {
			t.Errorf("demoWriteDenied(POST, %s) = true, want false (credential exchange)", p)
		}
	}
	// A near-miss path must not inherit the exemption.
	if !demoWriteDenied(http.MethodPost, operatorSessionPath+"/elevate") {
		t.Error("a path merely prefixed by an allowed one must still be denied")
	}
}

func TestDemoReadOnlyMiddleware(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Demo off: the middleware is a pass-through, writes reach the handler.
	off := &server{demoMode: false}
	rec := httptest.NewRecorder()
	off.demoReadOnly(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/op", nil))
	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("demo off: want the write to pass through, got code=%d reached=%v", rec.Code, reached)
	}

	// Demo on: the write is refused before the handler runs.
	reached = false
	on := &server{demoMode: true}
	rec = httptest.NewRecorder()
	on.demoReadOnly(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/op", nil))
	if reached {
		t.Error("demo on: the handler ran; the write must be refused before any work")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("demo on: code = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "read-only demo") {
		t.Errorf("denial should identify the demo posture, got %q", rec.Body.String())
	}

	// Demo on: reads still work.
	reached = false
	rec = httptest.NewRecorder()
	on.demoReadOnly(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/systemmap", nil))
	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("demo on: want the read to pass, got code=%d reached=%v", rec.Code, reached)
	}
}

func TestDemoOperatorGuard(t *testing.T) {
	const admin = "vtx.identity.AAAAAAAAAAAAAAAAAAAA"
	const demo = "vtx.identity.BBBBBBBBBBBBBBBBBBBB"

	// Demo off: the guard never blocks a normal console, however it is configured.
	if err := demoOperatorGuard(false, "", admin); err != nil {
		t.Errorf("demo off must never block boot, got %v", err)
	}

	// Demo on with no explicit operator: the console would fall back to the
	// bootstrap admin, so boot is refused.
	if err := demoOperatorGuard(true, "", admin); err == nil {
		t.Error("demo mode with no LOUPE_OPERATOR_ACTOR_KEY must refuse to boot")
	}
	if err := demoOperatorGuard(true, "   ", admin); err == nil {
		t.Error("a whitespace-only operator key must refuse to boot")
	}

	// Demo on explicitly naming the bootstrap admin: refused, and the error
	// says what to do instead.
	err := demoOperatorGuard(true, admin, admin)
	if err == nil {
		t.Fatal("demo mode as the bootstrap admin must refuse to boot")
	}
	if !strings.Contains(err.Error(), "LOUPE_OPERATOR_ACTOR_KEY") {
		t.Errorf("the refusal should name the fix, got %q", err)
	}

	// Demo on with a distinct explicit identity: boots.
	if err := demoOperatorGuard(true, demo, admin); err != nil {
		t.Errorf("a distinct demo operator must boot, got %v", err)
	}
	// No bootstrap file loaded (adminActor empty): an explicit key still boots,
	// and the empty admin must not accidentally match an empty-ish key.
	if err := demoOperatorGuard(true, demo, ""); err != nil {
		t.Errorf("explicit demo operator with no bootstrap admin must boot, got %v", err)
	}
}

func TestHandleDemo(t *testing.T) {
	for _, mode := range []bool{false, true} {
		s := &server{demoMode: mode}
		rec := httptest.NewRecorder()
		s.handleDemo(rec, httptest.NewRequest(http.MethodGet, "/api/demo", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("demoMode=%v: code = %d, want 200", mode, rec.Code)
		}
		var body struct {
			DemoMode bool   `json:"demoMode"`
			Notice   string `json:"notice"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.DemoMode != mode {
			t.Errorf("demoMode = %v, want %v", body.DemoMode, mode)
		}
		if body.Notice == "" {
			t.Error("notice should always be present so the banner and the 403 agree")
		}
	}

	// A write to the posture endpoint itself is method-gated.
	rec := httptest.NewRecorder()
	(&server{}).handleDemo(rec, httptest.NewRequest(http.MethodPost, "/api/demo", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/demo = %d, want 405", rec.Code)
	}
}

func TestDemoModeEnabledFailsClosedOnGarbage(t *testing.T) {
	// Unset / explicitly-off values disable the posture quietly.
	for _, v := range []string{"", "   ", "0", "false", "no", "off", "OFF"} {
		on, err := demoModeEnabled(v)
		if err != nil || on {
			t.Errorf("demoModeEnabled(%q) = (%v, %v), want (false, nil)", v, on, err)
		}
	}
	// Recognized truthy values enable it.
	for _, v := range []string{"1", "true", "yes", "on", " TRUE "} {
		on, err := demoModeEnabled(v)
		if err != nil || !on {
			t.Errorf("demoModeEnabled(%q) = (%v, %v), want (true, nil)", v, on, err)
		}
	}
	// A SET but unrecognizable value must stop the process rather than
	// silently serving a writable admin console on a public URL.
	for _, v := range []string{"enabled", "Y", "2", "demo", "readonly"} {
		if _, err := demoModeEnabled(v); err == nil {
			t.Errorf("demoModeEnabled(%q) returned no error; a typo must fail closed", v)
		}
	}
}
