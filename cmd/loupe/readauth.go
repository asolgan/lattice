package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/asolgan/lattice/internal/gateway/auth"
	"github.com/asolgan/lattice/internal/substrate"
)

// The console's front door (loupe-operator-auth-lift-design.md §3.1). Loupe
// was auth-less: whoever reached its listen address was trusted as admin.
// This gate requires a verified operator Bearer JWT for every request — the
// static UI and every /api/* route — and answers a no-token request with a
// 401, matching the design's fail-closed intent. It is an authN gate, not an
// RLS filter: an authenticated operator still sees the whole graph (the
// inspector's job); the gate only answers "is anyone allowed in the door at
// all". Reads are otherwise unchanged; op-submissions still stamp
// operatorActorKey/operatorActorToken as they do today (the Gateway-relay
// half of the lift is a later increment).
//
// Two postures, selected by env (mirrors cmd/loftspace-app/readauth.go):
//
//   - DEMO (LOUPE_DEV_AUTH=1): loopback-only, signs with the shared checked-in
//     dev key (deploy/gateway-dev-key/) that the Gateway and every vertical app
//     already trust. POST /api/operator/dev-token mints a short-lived token for
//     Loupe's own configured operator identity (operatorActorKey) — unlike the
//     verticals' per-applicant minting, Loupe has exactly one operator subject,
//     so the endpoint takes no request body.
//   - PRODUCTION (LOUPE_JWT_PUBLIC_KEY set): trusts a real external IdP's
//     public key; Loupe never signs. The console's login page presents a real
//     Bearer token (the deferred login-UI flow; this fire ships the server-side
//     gate + the dev-auth minting stand-in it's tested against).
const operatorDevTokenTTL = 30 * time.Minute

// operatorDevTokenPath is the one route requireOperator never gates — a caller
// must be able to reach the minting endpoint before it holds a token. The
// handler itself still fails closed (404) unless LOUPE_DEV_AUTH is set, and
// setupOperatorAuth refuses to enable dev-auth off a loopback bind, so this
// exemption never opens a production surface.
const operatorDevTokenPath = "/api/operator/dev-token"

// devSigner mints short-lived JWTs for the demo posture, signing with the
// shared dev key so the token verifies both here and at the Gateway/any other
// shared-dev-IdP posture (real-actor-write-auth-e2e-design.md §3.2).
type devSigner struct {
	priv *rsa.PrivateKey
	kid  string
	ttl  time.Duration
	now  func() time.Time
}

// mint returns a signed RS256 token whose `sub` is the bare identity id.
func (d *devSigner) mint(subject string) (string, time.Time, error) {
	now := d.now()
	exp := now.Add(d.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	})
	tok.Header["kid"] = d.kid
	signed, err := tok.SignedString(d.priv)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// setupOperatorAuth builds the console's authenticator from the environment.
// It returns (nil, nil, nil) when no posture is configured — a nil
// authenticator makes requireOperator fail every request closed with 401,
// the correct default for a console whose operator login is not provisioned.
func setupOperatorAuth(logger *slog.Logger, loopback bool) (*auth.Authenticator, *devSigner, error) {
	if isTruthy(os.Getenv("LOUPE_DEV_AUTH")) {
		// Defense in depth: the dev minter signs for whatever subject Loupe is
		// configured with, so it must never be reachable off-host — a
		// misconfigured non-local bind with dev-auth would let any network
		// caller mint itself an operator token.
		if !loopback {
			return nil, nil, fmt.Errorf("LOUPE_DEV_AUTH is only permitted on a loopback bind; use LOUPE_JWT_PUBLIC_KEY for a non-local deployment")
		}
		if strings.TrimSpace(os.Getenv("LOUPE_JWT_PUBLIC_KEY")) != "" {
			logger.Warn("both LOUPE_DEV_AUTH and LOUPE_JWT_PUBLIC_KEY are set; dev-auth wins and the configured IdP public key is IGNORED")
		}
		priv, err := auth.LoadDevSigningKey(os.Getenv("LOUPE_DEV_PRIVATE_KEY_PATH"))
		if err != nil {
			return nil, nil, fmt.Errorf("dev-auth: load shared dev signing key: %w", err)
		}
		trustedKeys, err := auth.LoadTrustedKeys(auth.KeySourceConfig{
			DevMode:    true,
			DevKeyPath: os.Getenv("LOUPE_DEV_PUBLIC_KEY_PATH"),
		}, func(msg string) { logger.Warn(msg) })
		if err != nil {
			return nil, nil, fmt.Errorf("dev-auth: load shared dev trust key: %w", err)
		}
		verifier, err := auth.NewVerifier(auth.Config{Keys: trustedKeys})
		if err != nil {
			return nil, nil, fmt.Errorf("dev-auth: build verifier: %w", err)
		}
		logger.Warn("DEV-AUTH ENABLED: Loupe mints its own operator token in-process (NOT for production); the console trusts the shared dev key")
		signer := &devSigner{
			priv: priv,
			kid:  auth.DevKeyID,
			ttl:  operatorDevTokenTTL,
			now:  time.Now,
		}
		// No revocation checker: the demo posture has no revocation bucket wired
		// (NewAuthenticator permits a nil RevocationChecker — verification only).
		return auth.NewAuthenticator(verifier, nil), signer, nil
	}

	pemKey := os.Getenv("LOUPE_JWT_PUBLIC_KEY")
	if strings.TrimSpace(pemKey) == "" {
		return nil, nil, nil
	}
	pub, err := parseOperatorPublicKeyPEM(pemKey)
	if err != nil {
		return nil, nil, fmt.Errorf("LOUPE_JWT_PUBLIC_KEY: %w", err)
	}
	kid := envOrDefault("LOUPE_JWT_KID", "idp-key-1")
	verifier, err := auth.NewVerifier(auth.Config{
		Keys:     map[string]crypto.PublicKey{kid: pub},
		Issuer:   os.Getenv("LOUPE_JWT_ISSUER"),
		Audience: os.Getenv("LOUPE_JWT_AUDIENCE"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build verifier: %w", err)
	}
	logger.Info("operator console configured with external IdP public key", "kid", kid)
	return auth.NewAuthenticator(verifier, nil), nil, nil
}

func parseOperatorPublicKeyPEM(pemStr string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX public key: %w", err)
	}
	return pub, nil
}

// bearerToken extracts the token from an `Authorization: Bearer <token>`
// header, or "" when absent/malformed.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// requireOperator wraps next so every request — the static UI and every
// /api/* route alike — must carry a valid operator Bearer JWT, except the
// dev-token minting endpoint itself (a caller must be able to reach it before
// holding a token). Fails closed: no authenticator configured, no token
// presented, or verification failing all answer 401, never a silent pass.
func (s *server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == operatorDevTokenPath {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := s.authenticateConsole(r); err != nil {
			s.writeError(w, http.StatusUnauthorized, "operator login required: "+err.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticateConsole verifies the request's Bearer JWT and returns the
// verified operator. It returns an error when no authenticator is configured,
// no token is presented, or verification fails — fail closed throughout.
func (s *server) authenticateConsole(r *http.Request) (auth.VerifiedActor, error) {
	if s.authn == nil {
		return auth.VerifiedActor{}, fmt.Errorf("console auth not configured (set LOUPE_DEV_AUTH or LOUPE_JWT_PUBLIC_KEY)")
	}
	tok := bearerToken(r)
	if tok == "" {
		return auth.VerifiedActor{}, fmt.Errorf("missing bearer token")
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()
	actor, err := s.authn.Authenticate(ctx, tok)
	if err != nil {
		return auth.VerifiedActor{}, err
	}
	if strings.TrimSpace(actor.Subject) == "" {
		return auth.VerifiedActor{}, fmt.Errorf("token has no subject")
	}
	return actor, nil
}

// handleOperatorDevToken implements POST /api/operator/dev-token (no body) —
// the demo-only login stand-in. It mints for a FIXED subject (the configured
// operatorActorKey), never a caller-supplied one: unlike the verticals'
// per-applicant minting, Loupe has exactly one operator identity, so the
// client never needs to name it. Available ONLY when dev-auth is enabled; a
// production deployment wires a real operator IdP login instead.
func (s *server) handleOperatorDevToken(w http.ResponseWriter, r *http.Request) {
	if s.devSigner == nil {
		s.writeError(w, http.StatusNotFound, "dev-token minting is disabled (LOUPE_DEV_AUTH not set)")
		return
	}
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	// crossOriginBlocked here too: mint is a state-changing action (issues a
	// live credential) like every other endpoint that checks it, even though
	// it takes no body — a hostile page's blind cross-origin POST is refused
	// before touching operatorActorKey, not just left to CORS to block the
	// response.
	if s.crossOriginBlocked(w, r) {
		return
	}
	if s.operatorActorKey == "" {
		s.writeError(w, http.StatusBadGateway, "no operator actor configured (LOUPE_OPERATOR_ACTOR_KEY unset and no bootstrap admin actor loaded)")
		return
	}
	vertexType, subject, ok := substrate.ParseVertexKey(s.operatorActorKey)
	if !ok || vertexType != "identity" {
		s.writeError(w, http.StatusInternalServerError, "operator actor key is malformed (must be a vtx.identity.<id> key)")
		return
	}
	token, exp, err := s.devSigner.mint(subject)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "mint token: "+err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": exp.UTC().Format(time.RFC3339),
	})
}

// isTruthy reports whether an env value enables a flag (1/true/yes, any case).
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
