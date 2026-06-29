package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testKID  = "idp-key-1"
	testSub  = "Hj4kPmRtw9nbCxz5vQ2y"
	testIss  = "https://idp.example.test"
	testAud  = "lattice-read"
	testJTI  = "tok-abc123"
	otherKID = "idp-key-2"
)

// fixedNow anchors every time-based assertion so skew math is deterministic.
var fixedNow = time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

type rsaKeypair struct {
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
}

func newRSA(t *testing.T) rsaKeypair {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	return rsaKeypair{priv: k, pub: &k.PublicKey}
}

func newECDSA(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa keygen: %v", err)
	}
	return k
}

// claims builds a standard claim set anchored on fixedNow with a valid window.
func claims() jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Subject:   testSub,
		Issuer:    testIss,
		Audience:  jwt.ClaimStrings{testAud},
		ID:        testJTI,
		IssuedAt:  jwt.NewNumericDate(fixedNow.Add(-1 * time.Minute)),
		NotBefore: jwt.NewNumericDate(fixedNow.Add(-1 * time.Minute)),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(5 * time.Minute)),
	}
}

func signRS256(t *testing.T, priv *rsa.PrivateKey, kid string, c jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign RS256: %v", err)
	}
	return s
}

func signES256(t *testing.T, priv *ecdsa.PrivateKey, kid string, c jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign ES256: %v", err)
	}
	return s
}

// verifierFor builds a Verifier trusting the given keys, clocked at fixedNow,
// with issuer + audience checks enabled.
func verifierFor(t *testing.T, keys map[string]crypto.PublicKey) *Verifier {
	t.Helper()
	v, err := NewVerifier(Config{
		Keys:     keys,
		Issuer:   testIss,
		Audience: testAud,
		now:      func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestVerify_ValidRS256(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	got, err := v.Verify(signRS256(t, kp.priv, testKID, claims()))
	if err != nil {
		t.Fatalf("Verify: unexpected error %v", err)
	}
	if got.ActorID != IdentityKeyPrefix+testSub {
		t.Errorf("ActorID = %q, want %q", got.ActorID, IdentityKeyPrefix+testSub)
	}
	if got.Subject != testSub {
		t.Errorf("Subject = %q, want %q", got.Subject, testSub)
	}
	if got.TokenID != testJTI {
		t.Errorf("TokenID = %q, want %q", got.TokenID, testJTI)
	}
	if !got.ExpiresAt.Equal(fixedNow.Add(5 * time.Minute)) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, fixedNow.Add(5*time.Minute))
	}
}

func TestVerify_ValidES256(t *testing.T) {
	priv := newECDSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: &priv.PublicKey})

	got, err := v.Verify(signES256(t, priv, testKID, claims()))
	if err != nil {
		t.Fatalf("Verify: unexpected error %v", err)
	}
	if got.ActorID != IdentityKeyPrefix+testSub {
		t.Errorf("ActorID = %q, want %q", got.ActorID, IdentityKeyPrefix+testSub)
	}
}

// TestVerify_RejectAlgNone is the alg-none bypass: an unsigned token must never
// authenticate.
func TestVerify_RejectAlgNone(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims())
	tok.Header["kid"] = testKID
	noneToken, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	_, err = v.Verify(noneToken)
	if !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("Verify(none) error = %v, want ErrUnsupportedAlgorithm", err)
	}
}

// TestVerify_RejectHS256Confusion is the alg-confusion attack: a token signed
// HS256 using the RSA public key as the HMAC secret must be rejected (a naive
// verifier that fed the public key to an HMAC check would accept it).
func TestVerify_RejectHS256Confusion(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	pubDER, err := x509.MarshalPKIXPublicKey(kp.pub)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims())
	tok.Header["kid"] = testKID
	forged, err := tok.SignedString(pubPEM)
	if err != nil {
		t.Fatalf("sign HS256: %v", err)
	}

	_, err = v.Verify(forged)
	if !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("Verify(HS256-confusion) error = %v, want ErrUnsupportedAlgorithm", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.ExpiresAt = jwt.NewNumericDate(fixedNow.Add(-5 * time.Minute)) // well past skew
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify(expired) error = %v, want ErrTokenExpired", err)
	}
}

// TestVerify_ExpiredWithinSkew — a token just past exp but inside the skew
// allowance still authenticates (clock-skew tolerance, design §3.4).
func TestVerify_ExpiredWithinSkew(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.ExpiresAt = jwt.NewNumericDate(fixedNow.Add(-30 * time.Second)) // within DefaultClockSkew (60s)
	got, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if err != nil {
		t.Fatalf("Verify(within-skew) error = %v, want success", err)
	}
	if got.Subject != testSub {
		t.Errorf("Subject = %q, want %q", got.Subject, testSub)
	}
}

// TestVerify_NoExpiry — an unbounded token (no exp) is rejected; the design
// rests on short TTLs as the revocation backstop.
func TestVerify_NoExpiry(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.ExpiresAt = nil
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify(no-exp) error = %v, want ErrTokenExpired", err)
	}
}

func TestVerify_NotYetValid(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.NotBefore = jwt.NewNumericDate(fixedNow.Add(5 * time.Minute)) // beyond skew
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrTokenNotYetValid) {
		t.Fatalf("Verify(nbf-future) error = %v, want ErrTokenNotYetValid", err)
	}
}

func TestVerify_IssuedInFuture(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.IssuedAt = jwt.NewNumericDate(fixedNow.Add(5 * time.Minute)) // beyond skew
	c.NotBefore = nil
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrTokenNotYetValid) {
		t.Fatalf("Verify(iat-future) error = %v, want ErrTokenNotYetValid", err)
	}
}

func TestVerify_MissingSubject(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.Subject = ""
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrMissingSubject) {
		t.Fatalf("Verify(no-sub) error = %v, want ErrMissingSubject", err)
	}
}

func TestVerify_UntrustedIssuer(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.Issuer = "https://evil.example.test"
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrUntrustedIssuer) {
		t.Fatalf("Verify(bad-iss) error = %v, want ErrUntrustedIssuer", err)
	}
}

func TestVerify_WrongAudience(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	c := claims()
	c.Audience = jwt.ClaimStrings{"some-other-service"}
	_, err := v.Verify(signRS256(t, kp.priv, testKID, c))
	if !errors.Is(err, ErrWrongAudience) {
		t.Fatalf("Verify(bad-aud) error = %v, want ErrWrongAudience", err)
	}
}

// TestVerify_IssuerAudienceOptional — when issuer/audience are unset on the
// Verifier, the corresponding claims are not checked.
func TestVerify_IssuerAudienceOptional(t *testing.T) {
	kp := newRSA(t)
	v, err := NewVerifier(Config{
		Keys: map[string]crypto.PublicKey{testKID: kp.pub},
		now:  func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	c := claims()
	c.Issuer = "anything"
	c.Audience = jwt.ClaimStrings{"anything"}
	if _, err := v.Verify(signRS256(t, kp.priv, testKID, c)); err != nil {
		t.Fatalf("Verify(no-iss/aud-config) error = %v, want success", err)
	}
}

func TestVerify_UnknownKID(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	_, err := v.Verify(signRS256(t, kp.priv, otherKID, claims()))
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Verify(unknown-kid) error = %v, want ErrUnknownKey", err)
	}
}

func TestVerify_MissingKID(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	_, err := v.Verify(signRS256(t, kp.priv, "", claims()))
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Verify(no-kid) error = %v, want ErrUnknownKey", err)
	}
}

// TestVerify_KIDPointsToWrongKey — the kid resolves to a trusted key, but the
// token was signed by a different key: signature verification fails.
func TestVerify_KIDPointsToWrongKey(t *testing.T) {
	kpA := newRSA(t)
	kpB := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kpA.pub})

	// Signed by B's private key, but the header claims kid=testKID (=A's key).
	forged := signRS256(t, kpB.priv, testKID, claims())
	_, err := v.Verify(forged)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify(kid-key-mismatch) error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerify_Malformed(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})

	for _, tok := range []string{"", "not.a.jwt", "abc", "a.b"} {
		if _, err := v.Verify(tok); !errors.Is(err, ErrMalformedToken) {
			t.Errorf("Verify(%q) error = %v, want ErrMalformedToken", tok, err)
		}
	}
}

func TestNewVerifier_NoKeys(t *testing.T) {
	_, err := NewVerifier(Config{})
	if !errors.Is(err, ErrNoTrustedKeys) {
		t.Fatalf("NewVerifier(no keys) error = %v, want ErrNoTrustedKeys", err)
	}
}

func TestNewVerifier_DefaultSkew(t *testing.T) {
	kp := newRSA(t)
	v, err := NewVerifier(Config{Keys: map[string]crypto.PublicKey{testKID: kp.pub}})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if v.clockSkew != DefaultClockSkew {
		t.Errorf("clockSkew = %v, want default %v", v.clockSkew, DefaultClockSkew)
	}
}

// --- Authenticator (verify + revocation kill-switch) ---

type fakeRevocation struct {
	revoked map[string]bool
	err     error
	gotID   string
}

func (f *fakeRevocation) IsRevoked(_ context.Context, actorID string) (bool, error) {
	f.gotID = actorID
	if f.err != nil {
		return false, f.err
	}
	return f.revoked[actorID], nil
}

func TestAuthenticate_NoRevocationChecker(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})
	a := NewAuthenticator(v, nil)

	got, err := a.Authenticate(context.Background(), signRS256(t, kp.priv, testKID, claims()))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ActorID != IdentityKeyPrefix+testSub {
		t.Errorf("ActorID = %q, want %q", got.ActorID, IdentityKeyPrefix+testSub)
	}
}

func TestAuthenticate_NotRevoked(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})
	rc := &fakeRevocation{revoked: map[string]bool{}}
	a := NewAuthenticator(v, rc)

	got, err := a.Authenticate(context.Background(), signRS256(t, kp.priv, testKID, claims()))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if rc.gotID != IdentityKeyPrefix+testSub {
		t.Errorf("revocation checked id = %q, want %q", rc.gotID, IdentityKeyPrefix+testSub)
	}
	if got.Subject != testSub {
		t.Errorf("Subject = %q, want %q", got.Subject, testSub)
	}
}

func TestAuthenticate_Revoked(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})
	rc := &fakeRevocation{revoked: map[string]bool{IdentityKeyPrefix + testSub: true}}
	a := NewAuthenticator(v, rc)

	_, err := a.Authenticate(context.Background(), signRS256(t, kp.priv, testKID, claims()))
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("Authenticate(revoked) error = %v, want ErrTokenRevoked", err)
	}
}

// TestAuthenticate_RevocationError — a failing kill-switch check must fail
// closed (deny), never serve.
func TestAuthenticate_RevocationError(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})
	rc := &fakeRevocation{err: errors.New("kv down")}
	a := NewAuthenticator(v, rc)

	_, err := a.Authenticate(context.Background(), signRS256(t, kp.priv, testKID, claims()))
	if err == nil {
		t.Fatal("Authenticate(revocation-error): want error, got nil")
	}
	if errors.Is(err, ErrTokenRevoked) {
		t.Errorf("error = %v, want a wrapped check failure (not ErrTokenRevoked)", err)
	}
}

// TestAuthenticate_BadTokenSkipsRevocation — a verification failure short-
// circuits before the kill-switch is consulted.
func TestAuthenticate_BadTokenSkipsRevocation(t *testing.T) {
	kp := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{testKID: kp.pub})
	rc := &fakeRevocation{revoked: map[string]bool{}}
	a := NewAuthenticator(v, rc)

	_, err := a.Authenticate(context.Background(), "not.a.jwt")
	if !errors.Is(err, ErrMalformedToken) {
		t.Fatalf("Authenticate(bad) error = %v, want ErrMalformedToken", err)
	}
	if rc.gotID != "" {
		t.Errorf("revocation consulted (%q) on a bad token; want skipped", rc.gotID)
	}
}
