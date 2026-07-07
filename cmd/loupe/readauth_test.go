package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestMain points the dev-auth posture's shared-dev-key loader at the repo
// root (deploy/gateway-dev-key/), since a test binary's CWD is this package's
// directory, not the repo root the production default path assumes.
func TestMain(m *testing.M) {
	os.Setenv("LOUPE_DEV_PRIVATE_KEY_PATH", "../../deploy/gateway-dev-key/dev-private.pem")
	os.Setenv("LOUPE_DEV_PUBLIC_KEY_PATH", "../../deploy/gateway-dev-key/dev-public.pem")
	os.Exit(m.Run())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"Bearer abc.def.ghi", "abc.def.ghi"},
		{"bearer abc.def.ghi", "abc.def.ghi"}, // case-insensitive scheme
		{"Bearer   spaced  ", "spaced"},
		{"Basic abc", ""},
		{"abc.def.ghi", ""},
		{"", ""},
		{"Bearer ", ""}, // scheme only, no token
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/systemmap", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		if got := bearerToken(r); got != c.want {
			t.Errorf("bearerToken(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}

func TestIsTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on", " On "} {
		if !isTruthy(v) {
			t.Errorf("isTruthy(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no", "off", "x"} {
		if isTruthy(v) {
			t.Errorf("isTruthy(%q) = true, want false", v)
		}
	}
}

// TestSetupOperatorAuth_DevPosture proves the demo posture wires a verifier
// whose trust matches the minter — a token the signer mints verifies.
func TestSetupOperatorAuth_DevPosture(t *testing.T) {
	t.Setenv("LOUPE_DEV_AUTH", "1")
	authn, signer, err := setupOperatorAuth(discardLogger(), true)
	if err != nil {
		t.Fatalf("setupOperatorAuth: %v", err)
	}
	if authn == nil || signer == nil {
		t.Fatalf("dev posture must return non-nil authn (%v) and signer (%v)", authn, signer)
	}

	const sub = "Hj4kPmRtw9nbCxz5vQ2y"
	tok, _, err := signer.mint(sub)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	actor, err := authn.Authenticate(t.Context(), tok)
	if err != nil {
		t.Fatalf("authenticate minted token: %v", err)
	}
	if actor.Subject != sub {
		t.Errorf("subject = %q, want %q", actor.Subject, sub)
	}
}

// TestSetupOperatorAuth_DevAuth_RefusesNonLoopback pins the defense-in-depth
// guard: dev-auth trusts any caller-asserted subject, so it must never be
// reachable off-host even if an operator misconfigures a non-loopback bind.
func TestSetupOperatorAuth_DevAuth_RefusesNonLoopback(t *testing.T) {
	t.Setenv("LOUPE_DEV_AUTH", "1")
	if _, _, err := setupOperatorAuth(discardLogger(), false); err == nil {
		t.Fatal("expected an error enabling dev-auth on a non-loopback bind")
	}
}

// TestSetupOperatorAuth_NoPosture: neither env set ⇒ no authenticator (fail
// closed) — the correct default for a console whose operator login is not
// provisioned.
func TestSetupOperatorAuth_NoPosture(t *testing.T) {
	t.Setenv("LOUPE_DEV_AUTH", "")
	t.Setenv("LOUPE_JWT_PUBLIC_KEY", "")
	authn, signer, err := setupOperatorAuth(discardLogger(), true)
	if err != nil {
		t.Fatalf("setupOperatorAuth: %v", err)
	}
	if authn != nil || signer != nil {
		t.Fatalf("no posture must return nil authn/signer, got authn=%v signer=%v", authn, signer)
	}
}

// TestSetupOperatorAuth_BadPublicKey: a configured but unparseable key is a
// hard misconfiguration, not a silent deny-all.
func TestSetupOperatorAuth_BadPublicKey(t *testing.T) {
	t.Setenv("LOUPE_DEV_AUTH", "")
	t.Setenv("LOUPE_JWT_PUBLIC_KEY", "not a pem")
	if _, _, err := setupOperatorAuth(discardLogger(), true); err == nil {
		t.Fatal("expected an error for an unparseable public key")
	}
}

// devAuthServer builds a server with the demo auth posture wired, an operator
// actor key set, and a nil NATS conn — for gate/handler tests that don't need
// a live connection.
func devAuthServer(t *testing.T) *server {
	t.Helper()
	t.Setenv("LOUPE_DEV_AUTH", "1")
	authn, signer, err := setupOperatorAuth(discardLogger(), true)
	if err != nil {
		t.Fatalf("setupOperatorAuth: %v", err)
	}
	return &server{
		logger:           discardLogger(),
		authn:            authn,
		devSigner:        signer,
		operatorActorKey: "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y",
		natsTimeout:      time.Second,
	}
}

func TestRequireOperator_NoAuthConfigured_401(t *testing.T) {
	s := &server{logger: discardLogger(), natsTimeout: time.Second} // authn nil
	called := false
	gate := s.requireOperator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/systemmap", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("next handler must not run without a valid operator token")
	}
}

func TestRequireOperator_NoToken_401(t *testing.T) {
	s := devAuthServer(t)
	called := false
	gate := s.requireOperator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/systemmap", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no bearer)", rec.Code)
	}
	if called {
		t.Fatal("next handler must not run without a bearer token")
	}
}

func TestRequireOperator_ForgedToken_401(t *testing.T) {
	s := devAuthServer(t)
	gate := s.requireOperator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not run with a forged token")
	}))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/systemmap", nil)
	r.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	gate.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (forged token)", rec.Code)
	}
}

func TestRequireOperator_ValidToken_PassesThrough(t *testing.T) {
	s := devAuthServer(t)
	tok, _, err := s.devSigner.mint("Hj4kPmRtw9nbCxz5vQ2y")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	called := false
	gate := s.requireOperator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/systemmap", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	gate.ServeHTTP(rec, r)
	if !called {
		t.Fatal("next handler must run with a valid operator token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestRequireOperator_DevTokenPath_Bypasses proves the one deliberate
// exemption: the minting endpoint itself must be reachable without a token
// (a caller has none yet), even when no auth is configured elsewhere.
func TestRequireOperator_DevTokenPath_Bypasses(t *testing.T) {
	s := &server{logger: discardLogger(), natsTimeout: time.Second} // authn nil
	called := false
	gate := s.requireOperator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil))
	if !called {
		t.Fatal("the dev-token path must bypass the operator gate")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleOperatorDevToken_Disabled_404(t *testing.T) {
	s := &server{logger: discardLogger(), natsTimeout: time.Second} // devSigner nil
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil)
	s.handleOperatorDevToken(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (dev-token disabled)", rec.Code)
	}
}

func TestHandleOperatorDevToken_WrongMethod_405(t *testing.T) {
	s := devAuthServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, operatorDevTokenPath, nil)
	s.handleOperatorDevToken(rec, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleOperatorDevToken_CrossOrigin_403(t *testing.T) {
	s := devAuthServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil)
	r.Header.Set("Origin", "https://evil.example")
	s.handleOperatorDevToken(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-origin mint request blocked)", rec.Code)
	}
}

func TestHandleOperatorDevToken_WrongVertexType_500(t *testing.T) {
	s := devAuthServer(t)
	s.operatorActorKey = "vtx.meta.Hj4kPmRtw9nbCxz5vQ2y" // not a vtx.identity.<id> key
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil)
	s.handleOperatorDevToken(rec, r)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (operator actor key is not a vtx.identity key)", rec.Code)
	}
}

func TestHandleOperatorDevToken_NoOperatorActor_502(t *testing.T) {
	t.Setenv("LOUPE_DEV_AUTH", "1")
	authn, signer, err := setupOperatorAuth(discardLogger(), true)
	if err != nil {
		t.Fatalf("setupOperatorAuth: %v", err)
	}
	s := &server{logger: discardLogger(), authn: authn, devSigner: signer, natsTimeout: time.Second} // operatorActorKey empty
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil)
	s.handleOperatorDevToken(rec, r)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (no operator actor configured)", rec.Code)
	}
}

func TestHandleOperatorDevToken_Mint_RoundTrips(t *testing.T) {
	s := devAuthServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil)
	s.handleOperatorDevToken(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" || resp.ExpiresAt == "" {
		t.Fatalf("empty token/expiresAt: %+v", resp)
	}
	actor, err := s.authn.Authenticate(t.Context(), resp.Token)
	if err != nil {
		t.Fatalf("authenticate minted token: %v", err)
	}
	if actor.Subject != "Hj4kPmRtw9nbCxz5vQ2y" {
		t.Errorf("subject = %q, want the bare operator-actor NanoID", actor.Subject)
	}
}

// TestFullMux_GatedEndToEnd proves the actual production wiring — mux wrapped
// by requireOperator, exactly as main.go builds it — denies an unauthenticated
// request to the static UI and to an API route, and admits one bearing a
// dev-minted operator token.
func TestFullMux_GatedEndToEnd(t *testing.T) {
	s := devAuthServer(t)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	gated := s.requireOperator(mux)

	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated static UI request: status = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/systemmap", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated API request: status = %d, want 401", rec.Code)
	}

	mintRec := httptest.NewRecorder()
	gated.ServeHTTP(mintRec, httptest.NewRequest(http.MethodPost, operatorDevTokenPath, nil))
	if mintRec.Code != http.StatusOK {
		t.Fatalf("dev-token mint through the gated mux: status = %d, want 200", mintRec.Code)
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(mintRec.Body.Bytes(), &minted); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}

	rec = httptest.NewRecorder()
	authed := httptest.NewRequest(http.MethodGet, "/api/systemmap", nil)
	authed.Header.Set("Authorization", "Bearer "+minted.Token)
	gated.ServeHTTP(rec, authed)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("authenticated API request still 401'd: body=%s", rec.Body.String())
	}
}
