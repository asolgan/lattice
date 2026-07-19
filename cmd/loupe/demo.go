package main

import (
	"fmt"
	"net/http"
	"strings"
)

// The hosted-demo read-only posture (LOUPE_DEMO_MODE,
// loupe-f20-demo-operator-ux.md). In the demo deployment the console is served
// to anonymous visitors, and the one-tap login hands every one of them the
// configured operator credential — that is intended, and what makes it safe is
// that the credential names an identity whose platform capability grants permit
// nothing but reads.
//
// For the WRITE surface that is exact: every op-submit relays through the
// Gateway under the visitor's own operator token, and control-plane mutates
// carry the operator actor, so the platform decides and demoReadOnly is
// defense in depth. demoOperatorGuard is what makes that non-optional — it
// refuses to boot a demo posture running as the bootstrap admin, so the grant
// scoping cannot be silently skipped.
//
// Two carve-outs where this process IS the only control, so do not relax them
// believing the grants have your back:
//
//   - REVEALS ride reads. A decrypt is a GET, so the method rule below never
//     sees it, and the vault unwrap RPC travels on Loupe's own NATS credentials
//     with no Lattice-Actor — the demo identity's grants are not consulted at
//     all. POST /api/vault/decrypt is denied by the method rule; the sensitive
//     object reveal (GET .../objects/<oid>?decrypt=true) is denied at its own
//     call site in objects.go.
//   - The demo's READ surface is Loupe's ordinary admin read surface (all of
//     Core KV, every vertex, the shred roster), not a grant-narrowed one. The
//     banner therefore promises only that writes and reveals are refused, which
//     is what this file actually delivers.

// demoAllowedWritePaths are the only non-read routes a demo visitor may reach:
// the credential exchange. Without these a visitor could neither log in nor log
// out. None of them mutates platform state — they mint and move the console's
// own session credential.
var demoAllowedWritePaths = map[string]bool{
	operatorDevTokenPath: true,
	operatorSessionPath:  true,
	operatorLogoutPath:   true,
}

// demoWriteDenied reports whether demo mode refuses this request. The rule is
// default-deny by METHOD — anything that is not GET/HEAD is refused — rather
// than a list of known write paths, because a method rule is fail-closed for
// routes that do not exist yet. A path list would fail OPEN the day a fire adds
// a write endpoint and forgets to update it.
//
// It over-denies, deliberately. The control plane tunnels three pure reads
// through POST (loom `inspect`, refractor `health` and `validate`), so a demo
// visitor loses those inspection replies. Restoring them means teaching this
// rule which control ops are reads — a classification that lives in control.go
// and would fail OPEN if it ever drifted — so the collateral is accepted here
// and tracked as F20.2 rather than traded for a stale allowlist.
func demoWriteDenied(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead:
		return false
	}
	return !demoAllowedWritePaths[path]
}

// demoReadOnly wraps next with the demo posture's write denial. It is a no-op
// when demo mode is off, so the ordinary operator console is untouched.
//
// It sits INSIDE requireOperator (main.go), so authentication is decided first:
// an unauthenticated caller sees 401 rather than a demo 403 that would leak the
// posture to anyone who can reach the port. The credential-exchange paths are
// exempt from requireOperator and reach here directly, which is why they need
// the explicit allowlist above.
func (s *server) demoReadOnly(next http.Handler) http.Handler {
	if !s.demoMode {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if demoWriteDenied(r.Method, r.URL.Path) {
			s.writeError(w, http.StatusForbidden, demoDenialMessage)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// demoDenialMessage is what a visitor sees when they try a write or a reveal.
// It states what this console does, not what the platform's grants are: the
// grant scoping is provisioned separately (F20.3) and nothing in this process
// can verify it, so claiming it here would be an assertion the code cannot
// back.
const demoDenialMessage = "read-only demo: this console accepts reads only — write actions and PII reveals are refused"

// demoModeEnabled parses LOUPE_DEMO_MODE. A value that is SET but not
// recognizable is an error rather than a silent false: the failure mode of
// misreading it is a fully writable admin console on a public URL, so a typo
// ("LOUPE_DEMO_MODE=enabled") must stop the process, not quietly disable the
// posture — and with it demoOperatorGuard, which returns early when demo mode
// is off.
func demoModeEnabled(raw string) (bool, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return false, nil
	}
	if isTruthy(v) {
		return true, nil
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("LOUPE_DEMO_MODE=%q is not a recognized boolean; "+
		"use 1/true/yes/on to enable the read-only demo posture, or unset it", raw)
}

// handleDemo implements GET /api/demo — the posture the shell reads to decide
// whether to render the visitor banner.
func (s *server) handleDemo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"demoMode": s.demoMode,
		"notice":   demoDenialMessage,
	})
}

// demoOperatorGuard validates the demo posture's one precondition at boot:
// the console must be configured to run as an explicitly-named identity that
// is NOT the bootstrap admin.
//
// This is a refusal, not a warning, on purpose. LOUPE_DEMO_MODE=1 on a stock
// stack would otherwise run the demo posture as the primordial admin — read-only
// within this process and omnipotent to anything reaching the platform by any
// other path — which is precisely the confinement guarantee resting on an
// advisory precondition that this codebase does not allow.
//
// It proves the operator identity is distinct and deliberate. It cannot prove
// the identity's grants are actually narrow — only the platform knows that — so
// it closes the stock-stack footgun, not every misconfiguration.
func demoOperatorGuard(demoMode bool, configuredKey, adminActor string) error {
	if !demoMode {
		return nil
	}
	configuredKey = strings.TrimSpace(configuredKey)
	if configuredKey == "" {
		return fmt.Errorf("LOUPE_DEMO_MODE requires LOUPE_OPERATOR_ACTOR_KEY naming the scoped demo operator identity")
	}
	if adminActor != "" && configuredKey == adminActor {
		return fmt.Errorf("LOUPE_DEMO_MODE refuses to run as the bootstrap admin actor (%s): "+
			"set LOUPE_OPERATOR_ACTOR_KEY to a demo identity whose capability grants permit reads only", adminActor)
	}
	return nil
}
