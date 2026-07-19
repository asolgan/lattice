package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// F20.2 §7.3 — the /login disclaimer, injected server-side so it needs no
// unauthenticated /api/demo exemption and survives JS being off.

func TestLoginPageMarkersExistExactlyOnce(t *testing.T) {
	// The injection is a string replace, and a silent miss would serve demo
	// visitors the operator wording. Pin both markers so a copy edit in
	// login.html fails here instead of shipping quietly.
	body, err := webFS.ReadFile("web/login.html")
	if err != nil {
		t.Fatalf("read login.html: %v", err)
	}
	page := string(body)
	for _, marker := range []string{loginDemoNoticeMarker, loginDevButtonLabel} {
		if n := strings.Count(page, marker); n != 1 {
			t.Errorf("login.html contains %q %d times, want exactly 1 — "+
				"the demo injection is a string replace and silently no-ops otherwise", marker, n)
		}
	}
	// The demo label must not already be present, or the second replace could
	// hit the wrong occurrence.
	if strings.Contains(page, loginDemoButtonLabel) {
		t.Errorf("login.html already contains %q; the injected label must be unique", loginDemoButtonLabel)
	}
	// The notice is spliced before the label replace runs, so a notice that
	// happened to contain the operator label would swallow the replace and
	// leave the button reading "Log in as operator (dev)" on the demo.
	if strings.Contains(loginDemoNotice, loginDevButtonLabel) {
		t.Errorf("loginDemoNotice contains %q, which the label replace would hit "+
			"instead of the button", loginDevButtonLabel)
	}
}

func TestLoginPageDisclaimerOnlyInDemoMode(t *testing.T) {
	render := func(demo bool) string {
		s := &server{logger: discardLogger(), natsTimeout: time.Second, demoMode: demo}
		rec := httptest.NewRecorder()
		s.handleLoginPage(rec, httptest.NewRequest(http.MethodGet, loginPagePath, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		return rec.Body.String()
	}

	off := render(false)
	if strings.Contains(off, `class="demo-notice"`) || strings.Contains(off, loginDemoButtonLabel) {
		t.Error("the ordinary console's login page must carry no demo disclaimer")
	}
	if !strings.Contains(off, loginDevButtonLabel) {
		t.Error("the operator button label should be untouched when demo mode is off")
	}
	if !strings.Contains(off, loginDemoNoticeMarker) {
		t.Error("the marker should pass through inert (it is an HTML comment)")
	}

	on := render(true)
	if !strings.Contains(on, "The console accepts reads only") {
		t.Error("demo mode must inject the visitor disclaimer")
	}
	// The disclaimer must not promise the platform's grants are narrow: that
	// scoping is provisioned separately and nothing in this process can verify
	// it, so claiming it would be an assertion the code cannot back.
	for _, overclaim := range []string{
		"capability grants",
		"comes from the platform",
		"platform permit",
	} {
		if strings.Contains(on, overclaim) {
			t.Errorf("the disclaimer claims %q — it may promise only what this console enforces", overclaim)
		}
	}
	if strings.Contains(on, loginDemoNoticeMarker) {
		t.Error("the marker should have been replaced, not left sitting beside the notice")
	}
	if !strings.Contains(on, loginDemoButtonLabel) || strings.Contains(on, loginDevButtonLabel) {
		t.Error("demo mode must relabel the one-tap button, replacing the operator wording")
	}
	// The page stays self-contained — no new fetch, so it still works with JS
	// off and needs no requireOperator exemption.
	if strings.Contains(on, "/api/demo") {
		t.Error("the disclaimer must be injected, not fetched from /api/demo")
	}
}
