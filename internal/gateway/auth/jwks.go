package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// ErrEmptyJWKS is returned when a JWKS document parses as valid JSON but
// yields zero usable keys (every entry unsupported/malformed, or the `keys`
// array itself is empty). Callers treat this as a fetch failure, not a valid
// "trust nothing" response — an IdP does not intentionally publish an empty
// key set (RFC 7517 §5).
var ErrEmptyJWKS = errors.New("auth: JWKS document contains no usable keys")

// jwkSet is the RFC 7517 §5 JWK Set wire shape.
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// jwk is the subset of RFC 7517/7518 JWK fields this package understands:
// RSA (`kty":"RSA"`, §6.3.1) and EC (`kty":"EC"`, §6.2.1) public keys. Private
// key fields (`d`, ...) are never read — Lattice holds only IdP public keys.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	// Alg is the JWK's optional (RFC 7517 §4.4) advisory signing algorithm.
	// Never consulted for a trust decision (jwt.WithValidMethods is the actual
	// alg gate) — read only by jwksAlgs, for the jwks health block's
	// provenance display.
	Alg string `json:"alg"`
	// RSA
	N string `json:"n"`
	E string `json:"e"`
	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// ParseJWKS parses an RFC 7517 JWK Set document into a kid→public-key map
// usable as auth.Config.Keys / Verifier.SetKeys. Entries this package cannot
// or should not trust are individually skipped (not fatal): a non-signature
// `use` (e.g. `"enc"`), an unsupported `kty` (only RSA/EC are asymmetric
// signature families a JWT can use here — the closed algorithm allow-list in
// keyfunc already refuses anything else), a missing/duplicate `kid`, or a
// malformed coordinate. skipped carries a human-readable reason per skipped
// entry for the caller to log. Returns ErrEmptyJWKS if no entry survives —
// an empty trust set is always a fetch failure, never propagated to the
// Verifier (which would silently reject every token).
func ParseJWKS(data []byte) (keys map[string]crypto.PublicKey, skipped []string, err error) {
	var set jwkSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, nil, fmt.Errorf("auth: parse JWKS: %w", err)
	}
	keys = make(map[string]crypto.PublicKey, len(set.Keys))
	for i, k := range set.Keys {
		if k.Use != "" && k.Use != "sig" {
			skipped = append(skipped, fmt.Sprintf("key[%d] kid=%q: use=%q, not a signature key", i, k.Kid, k.Use))
			continue
		}
		if k.Kid == "" {
			skipped = append(skipped, fmt.Sprintf("key[%d]: missing kid", i))
			continue
		}
		if _, dup := keys[k.Kid]; dup {
			skipped = append(skipped, fmt.Sprintf("key[%d] kid=%q: duplicate kid, keeping the first", i, k.Kid))
			continue
		}
		pub, perr := parseJWKKey(k)
		if perr != nil {
			skipped = append(skipped, fmt.Sprintf("key[%d] kid=%q: %v", i, k.Kid, perr))
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, skipped, ErrEmptyJWKS
	}
	return keys, skipped, nil
}

func parseJWKKey(k jwk) (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAJWK(k)
	case "EC":
		return parseECJWK(k)
	default:
		return nil, fmt.Errorf("unsupported kty %q (only RSA/EC are trusted)", k.Kty)
	}
}

func parseRSAJWK(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64URLDecode(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64URLDecode(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("empty n or e")
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() {
		return nil, errors.New("e out of range")
	}
	// A public exponent of 0 or 1 is not a usable RSA key (e=0 makes every
	// signature's verification exponentiation trivial; e=1 makes it the
	// identity function) — reject both rather than silently building a
	// degenerate *rsa.PublicKey a caller could mistake for a real trust
	// anchor. 3 is the smallest exponent ever used in practice.
	if e.Int64() < 3 {
		return nil, errors.New("e is degenerate (must be >= 3)")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// ecCurve maps the JWK `crv` name (RFC 7518 §6.2.1.1) to its Go curve. Only
// the NIST curves golang-jwt's ES256/ES384/ES512 methods sign with are
// supported — matching auth.allowedMethods exactly, so a JWKS entry can
// never introduce a curve the verifier would refuse anyway.
func ecCurve(crv string) (elliptic.Curve, error) {
	switch crv {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("unsupported crv %q", crv)
	}
}

func parseECJWK(k jwk) (*ecdsa.PublicKey, error) {
	curve, err := ecCurve(k.Crv)
	if err != nil {
		return nil, err
	}
	xBytes, err := base64URLDecode(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yBytes, err := base64URLDecode(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}
	if len(xBytes) == 0 || len(yBytes) == 0 {
		return nil, errors.New("empty x or y")
	}
	size := (curve.Params().BitSize + 7) / 8
	if len(xBytes) > size || len(yBytes) > size {
		return nil, errors.New("coordinate too large for curve")
	}
	// Assemble the SEC1 uncompressed point (0x04 || X || Y, each coordinate
	// left-zero-padded to the curve's field size) and hand it to
	// ParseUncompressedPublicKey, which validates the point is actually on
	// the curve (and not the point at infinity) — the maintained replacement
	// for the deprecated elliptic.Curve.IsOnCurve.
	uncompressed := make([]byte, 1+2*size)
	uncompressed[0] = 0x04
	copy(uncompressed[1+size-len(xBytes):1+size], xBytes)
	copy(uncompressed[1+2*size-len(yBytes):], yBytes)
	return ecdsa.ParseUncompressedPublicKey(curve, uncompressed)
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// jwksAlgs extracts the optional per-kid `alg` member from a JWK Set document,
// parsed independently of ParseJWKS so every existing ParseJWKS/FetchJWKS
// caller keeps its current signature and arity. Best-effort only: a
// malformed document yields a nil map (the caller already has the real
// ParseJWKS error to act on) and a kid with no `alg` member is simply absent
// from the result — both read as "unknown" by the jwks health block, never
// as a trust decision.
func jwksAlgs(data []byte) map[string]string {
	var set jwkSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil
	}
	algs := make(map[string]string, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kid != "" && k.Alg != "" {
			algs[k.Kid] = k.Alg
		}
	}
	return algs
}
