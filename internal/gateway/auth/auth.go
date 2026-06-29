// Package auth is the read-path actor-authentication seam (D1 increment 2).
//
// A reader presents a JWT signed by an external IdP/KMS. Lattice holds only the
// IdP's PUBLIC key(s) and never signs — the actor signing keys live outside the
// platform (lattice-architecture.md "External IdP for actor signing keys";
// brainstorm #118 "does NOT own actor signing keys"). The Verifier checks the
// signature, validates the standard time/issuer/audience claims, and extracts
// the Identity vertex id from the `sub` claim, returning the full vertex key
// `vtx.identity.<sub>` as the actor id — the read analog of write-path
// `Lattice-Actor` stamping: it AUTHENTICATES, it does NOT filter rows
// (filtering is read-path authorization, D1.3).
//
// Security posture (enforced + tested):
//   - asymmetric verification only — RS256 / ES256; the symmetric HS* family and
//     the unsigned `none` algorithm are refused before any key is consulted, so
//     the classic alg-confusion and alg-none bypasses cannot land;
//   - the JWT header `kid` selects the trusted public key; an unknown/absent kid
//     is rejected (no implicit single-key fallback that a forged kid could dodge);
//   - `exp` is required and enforced with a bounded clock-skew allowance; `nbf`
//     and `iat` (when present) are enforced under the same skew;
//   - `sub` is required and non-empty.
//
// The Authenticator composes the Verifier with a revocation kill-switch
// (internal/gateway/revocation) so a compromised actor can be cut off
// out-of-band even while holding a structurally-valid, unexpired token.
package auth

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IdentityKeyPrefix is the canonical prefix of an identity vertex key
// (Contract #1 §1.1 — `vtx.identity.<id>`). The verified `sub` claim carries the
// bare identity id; the actor id surfaced to the read boundary is the full key,
// consistent with the cap-read doc's `actor` field (§6.14) and the write-path
// actor (`vtx.identity.<id>`). This is distinct from §6.14's
// `readableAnchors[].anchorId`, which is the resource's bare NanoID (the opaque
// match token via `nanoIdFromKey`), not a full key.
const IdentityKeyPrefix = "vtx.identity."

// allowedMethods is the closed set of accepted signing algorithms. Asymmetric
// only: Lattice verifies with a public key it does not own the private half of.
// HS* (symmetric) and `none` are absent by construction — a token presenting any
// other alg is rejected by jwt.WithValidMethods before the keyfunc runs.
var allowedMethods = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}

// Sentinel errors. Callers branch on these to map an authentication failure to a
// read-boundary response; all of them mean "deny" — none should ever serve data.
var (
	// ErrMalformedToken — the token is not a well-formed, parseable JWT.
	ErrMalformedToken = errors.New("auth: malformed token")
	// ErrUnsupportedAlgorithm — the token's alg is outside the asymmetric
	// allow-list (HS*, none, or anything unexpected).
	ErrUnsupportedAlgorithm = errors.New("auth: unsupported signing algorithm")
	// ErrUnknownKey — the token's `kid` does not match any trusted public key.
	ErrUnknownKey = errors.New("auth: unknown signing key")
	// ErrInvalidSignature — the signature did not verify against the trusted key.
	ErrInvalidSignature = errors.New("auth: invalid signature")
	// ErrTokenExpired — `exp` is in the past (beyond the skew allowance).
	ErrTokenExpired = errors.New("auth: token expired")
	// ErrTokenNotYetValid — `nbf`/`iat` is in the future (beyond the skew allowance).
	ErrTokenNotYetValid = errors.New("auth: token not yet valid")
	// ErrMissingSubject — the `sub` claim is absent or empty.
	ErrMissingSubject = errors.New("auth: missing subject claim")
	// ErrUntrustedIssuer — `iss` does not match the configured issuer.
	ErrUntrustedIssuer = errors.New("auth: untrusted issuer")
	// ErrWrongAudience — `aud` does not include the configured audience.
	ErrWrongAudience = errors.New("auth: wrong audience")
	// ErrTokenRevoked — the actor's identity is on the revocation kill-switch.
	ErrTokenRevoked = errors.New("auth: token revoked")
	// ErrNoTrustedKeys — the Verifier was constructed with no public keys; it
	// fails closed (every token is rejected) rather than trusting nothing.
	ErrNoTrustedKeys = errors.New("auth: no trusted keys configured")
)

// Config configures a Verifier.
type Config struct {
	// Keys maps a key id (`kid`) to the trusted IdP public key (*rsa.PublicKey
	// or *ecdsa.PublicKey). At least one entry is required.
	Keys map[string]crypto.PublicKey
	// ClockSkew is the symmetric tolerance applied to exp/nbf/iat. A short JWT
	// TTL plus a small skew is the D1 freshness backstop (design §3.4 / M6).
	// Defaults to DefaultClockSkew when zero.
	ClockSkew time.Duration
	// Issuer, when non-empty, is required to match the token `iss` claim.
	Issuer string
	// Audience, when non-empty, is required to be present in the token `aud`.
	Audience string
	// now overrides the clock for tests; nil uses time.Now.
	now func() time.Time
}

// DefaultClockSkew is the time tolerance applied when Config.ClockSkew is zero.
const DefaultClockSkew = 60 * time.Second

// VerifiedActor is the outcome of a successful verification — an authenticated,
// non-filtered actor identity.
type VerifiedActor struct {
	// ActorID is the full identity vertex key (`vtx.identity.<sub>`).
	ActorID string
	// Subject is the raw `sub` claim (the bare identity id).
	Subject string
	// TokenID is the `jti` claim if present (used by per-token revocation; empty
	// when the IdP omits it).
	TokenID string
	// ExpiresAt is the token `exp`.
	ExpiresAt time.Time
}

// Verifier verifies IdP-signed JWTs and extracts the actor. It is safe for
// concurrent use (its key set and config are read-only after construction).
type Verifier struct {
	keys      map[string]crypto.PublicKey
	clockSkew time.Duration
	issuer    string
	audience  string
	now       func() time.Time
	parser    *jwt.Parser
}

// NewVerifier builds a Verifier from cfg. It returns ErrNoTrustedKeys if no
// public keys are configured (fail closed — a keyless verifier would reject
// every token, which is correct, but the misconfiguration is worth surfacing at
// construction rather than silently denying all reads).
func NewVerifier(cfg Config) (*Verifier, error) {
	if len(cfg.Keys) == 0 {
		return nil, ErrNoTrustedKeys
	}
	keys := make(map[string]crypto.PublicKey, len(cfg.Keys))
	for kid, k := range cfg.Keys {
		keys[kid] = k
	}
	skew := cfg.ClockSkew
	if skew == 0 {
		skew = DefaultClockSkew
	}
	nowFn := cfg.now
	if nowFn == nil {
		nowFn = time.Now
	}
	// jwt.WithValidMethods rejects any token whose alg is outside the allow-list
	// before the keyfunc is invoked — the structural guard against alg confusion
	// (a forged HS256 that tries to verify the public key as an HMAC secret) and
	// the `none` bypass. WithoutClaimsValidation hands time validation to the
	// explicit skew-aware checks below (the library's default leeway is 0).
	parser := jwt.NewParser(
		jwt.WithValidMethods(allowedMethods),
		jwt.WithoutClaimsValidation(),
	)
	return &Verifier{
		keys:      keys,
		clockSkew: skew,
		issuer:    cfg.Issuer,
		audience:  cfg.Audience,
		now:       nowFn,
		parser:    parser,
	}, nil
}

// Verify checks tokenString and returns the authenticated actor, or one of the
// sentinel errors above on any failure. It never returns a VerifiedActor on
// error.
func (v *Verifier) Verify(tokenString string) (VerifiedActor, error) {
	claims := jwt.RegisteredClaims{}
	_, err := v.parser.ParseWithClaims(tokenString, &claims, v.keyfunc)
	if err != nil {
		return VerifiedActor{}, mapParseError(err)
	}

	now := v.now()
	if err := v.checkTime(&claims, now); err != nil {
		return VerifiedActor{}, err
	}
	if v.issuer != "" && claims.Issuer != v.issuer {
		return VerifiedActor{}, ErrUntrustedIssuer
	}
	if v.audience != "" && !containsString(claims.Audience, v.audience) {
		return VerifiedActor{}, ErrWrongAudience
	}

	sub := strings.TrimSpace(claims.Subject)
	if sub == "" {
		return VerifiedActor{}, ErrMissingSubject
	}

	var exp time.Time
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	}
	return VerifiedActor{
		ActorID:   IdentityKeyPrefix + sub,
		Subject:   sub,
		TokenID:   claims.ID,
		ExpiresAt: exp,
	}, nil
}

// keyfunc resolves the trusted public key for a token by its `kid` header. The
// signing method is re-asserted here as defense in depth even though
// WithValidMethods already gates the alg — so a future relaxation of the parser
// options cannot silently re-open the symmetric/none surface.
func (v *Verifier) keyfunc(token *jwt.Token) (any, error) {
	switch token.Method.(type) {
	case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA:
		// asymmetric — expected.
	default:
		return nil, ErrUnsupportedAlgorithm
	}
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, ErrUnknownKey
	}
	key, ok := v.keys[kid]
	if !ok {
		return nil, ErrUnknownKey
	}
	return key, nil
}

// checkTime enforces exp (required) and nbf/iat (when present) under the
// configured clock skew.
func (v *Verifier) checkTime(c *jwt.RegisteredClaims, now time.Time) error {
	if c.ExpiresAt == nil {
		// No expiry = an unbounded token. Reject: the design rests on short TTLs
		// as the revocation backstop (M6).
		return ErrTokenExpired
	}
	if now.After(c.ExpiresAt.Add(v.clockSkew)) {
		return ErrTokenExpired
	}
	if c.NotBefore != nil && now.Before(c.NotBefore.Add(-v.clockSkew)) {
		return ErrTokenNotYetValid
	}
	if c.IssuedAt != nil && now.Before(c.IssuedAt.Add(-v.clockSkew)) {
		return ErrTokenNotYetValid
	}
	return nil
}

// mapParseError collapses golang-jwt's error tree into our sentinel set so
// callers branch on a stable surface, never on the library's internal types.
// Ordered most-specific first: the keyfunc-wrapped sentinels (joined via %w, so
// errors.Is reaches them) precede the broad library categories.
func mapParseError(err error) error {
	switch {
	case errors.Is(err, ErrUnknownKey):
		return ErrUnknownKey
	case errors.Is(err, ErrUnsupportedAlgorithm):
		return ErrUnsupportedAlgorithm
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrMalformedToken
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		// Either the WithValidMethods alg rejection ("signing method <alg> is
		// invalid" — an HS*/none/unexpected token) or a genuine signature
		// mismatch. Both deny; distinguish so the caller gets a precise sentinel.
		if strings.Contains(err.Error(), "signing method") {
			return ErrUnsupportedAlgorithm
		}
		return ErrInvalidSignature
	case errors.Is(err, jwt.ErrTokenUnverifiable):
		// alg unspecified (no `alg` header) or unavailable (an unknown method
		// name the library cannot resolve) — both are an unusable algorithm.
		return ErrUnsupportedAlgorithm
	default:
		return fmt.Errorf("%w: %v", ErrMalformedToken, err)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// RevocationChecker is the kill-switch surface the Authenticator consults after a
// token verifies. internal/gateway/revocation provides the substrate-backed
// implementation; the interface keeps this package free of a substrate import
// and lets the Authenticator be tested with a fake.
type RevocationChecker interface {
	// IsRevoked reports whether actorID (the full identity vertex key) has been
	// revoked. A transport/KV error is returned as-is so the caller can fail
	// closed.
	IsRevoked(ctx context.Context, actorID string) (bool, error)
}

// Authenticator is the full read-actor seam: verify the JWT, then consult the
// revocation kill-switch. It is the entry point a read boundary calls (D1.3).
type Authenticator struct {
	verifier   *Verifier
	revocation RevocationChecker
}

// NewAuthenticator composes a Verifier with a RevocationChecker. A nil checker
// is allowed (verification only, no kill-switch) for deployments that have not
// provisioned the revocation bucket yet.
func NewAuthenticator(v *Verifier, rc RevocationChecker) *Authenticator {
	return &Authenticator{verifier: v, revocation: rc}
}

// Authenticate verifies tokenString and, on success, checks the revocation
// kill-switch. It returns ErrTokenRevoked if the actor is revoked, the
// verifier's sentinel on a verification failure, or a wrapped error if the
// revocation check itself fails (fail closed — a read boundary must deny when it
// cannot confirm the actor is live).
func (a *Authenticator) Authenticate(ctx context.Context, tokenString string) (VerifiedActor, error) {
	actor, err := a.verifier.Verify(tokenString)
	if err != nil {
		return VerifiedActor{}, err
	}
	if a.revocation == nil {
		return actor, nil
	}
	revoked, err := a.revocation.IsRevoked(ctx, actor.ActorID)
	if err != nil {
		return VerifiedActor{}, fmt.Errorf("auth: revocation check failed: %w", err)
	}
	if revoked {
		return VerifiedActor{}, ErrTokenRevoked
	}
	return actor, nil
}
