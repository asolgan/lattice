package appsession

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/operatinggraph/lattice/internal/gateway/auth"
)

// devKeyEnv points the shared-dev-key loaders at the repo's checked-in pair —
// a test binary's CWD is this package's directory, not the repo root the
// production default path assumes.
func devKeyEnv(t *testing.T, envPrefix string) {
	t.Helper()
	t.Setenv(envPrefix+"_DEV_PRIVATE_KEY_PATH", "../../deploy/gateway-dev-key/dev-private.pem")
	t.Setenv(envPrefix+"_DEV_PUBLIC_KEY_PATH", "../../deploy/gateway-dev-key/dev-public.pem")
}

// idpKeyPair generates a throwaway external-IdP key pair, returning the
// signing half and its PKIX public key in the PEM form <PREFIX>_JWT_PUBLIC_KEY
// carries.
func idpKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	return priv, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// idpToken signs an external-IdP-shaped token: an IdP-native subject under a
// pinned issuer, which is what opaque-mode verification expects.
func idpToken(t *testing.T, priv *rsa.PrivateKey, kid, issuer, subject string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	require.NoError(t, err)
	return signed
}

// revokedChecker is the kill-switch fully engaged: every actor is revoked.
type revokedChecker struct{}

func (revokedChecker) IsRevoked(context.Context, string) (bool, error) { return true, nil }

func TestNewDevSigner_DisabledByDefault(t *testing.T) {
	signer, err := NewDevSigner(slog.Default(), "TESTAPP", true)
	require.NoError(t, err)
	require.Nil(t, signer, "no minter without an explicit opt-in")
}

// TestNewDevSigner_RefusesNonLoopbackBind pins the defence in depth: an
// in-process minter signs whatever subject its caller names, so it must never
// be reachable from off-box even when someone sets the env flag.
func TestNewDevSigner_RefusesNonLoopbackBind(t *testing.T) {
	t.Setenv("TESTAPP_DEV_AUTH", "1")
	signer, err := NewDevSigner(slog.Default(), "TESTAPP", false)
	require.Error(t, err)
	require.Nil(t, signer)
	require.Contains(t, err.Error(), "loopback")
}

func TestSigner_MintCarriesSubjectAndExpiry(t *testing.T) {
	signer := testSigner(t)
	subject := testNanoID(t)
	token, exp, err := signer.Mint(subject)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	authn, err := buildTestVerifier(&signer.priv.PublicKey, signer.kid)
	require.NoError(t, err)
	actor, err := authn.Authenticate(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, subject, actor.Subject)
	require.WithinDuration(t, exp, actor.ExpiresAt, 2*time.Second)
}

// TestNewAuthenticators_NoPosture_AllNil: no minter and no IdP key ⇒ nothing
// to verify against, so a session-gated request fails closed.
func TestNewAuthenticators_NoPosture_AllNil(t *testing.T) {
	t.Setenv("TESTAPP_JWT_PUBLIC_KEY", "")
	strict, refresh, err := NewAuthenticators(slog.Default(), "TESTAPP", nil, nil)
	require.NoError(t, err)
	require.Nil(t, strict)
	require.Nil(t, refresh)
}

// TestNewAuthenticators_IdPPosture_VerifiesExternalToken proves the
// production posture: an external IdP's PEM public key + pinned issuer build
// one verify-only authenticator, and no refresh sibling — no minter here ever
// issued the cookie, so nothing here can renew it.
func TestNewAuthenticators_IdPPosture_VerifiesExternalToken(t *testing.T) {
	priv, pubPEM := idpKeyPair(t)
	t.Setenv("TESTAPP_JWT_PUBLIC_KEY", pubPEM)
	t.Setenv("TESTAPP_JWT_ISSUER", "https://idp.example")

	strict, refresh, err := NewAuthenticators(slog.Default(), "TESTAPP", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, strict)
	require.Nil(t, refresh)

	actor, err := strict.Authenticate(t.Context(), idpToken(t, priv, defaultIdPKeyID, "https://idp.example", "idp-native-subject"))
	require.NoError(t, err)
	require.Equal(t, "https://idp.example", actor.Issuer)
	require.Equal(t, "idp-native-subject", actor.RawSubject)
	require.NotEmpty(t, actor.Subject)
}

// TestNewAuthenticators_IdPPosture_ForeignIssuerRejected pins that the issuer
// really is enforced, not merely recorded — a token from another issuer,
// signed by the same trusted key, is refused.
func TestNewAuthenticators_IdPPosture_ForeignIssuerRejected(t *testing.T) {
	priv, pubPEM := idpKeyPair(t)
	t.Setenv("TESTAPP_JWT_PUBLIC_KEY", pubPEM)
	t.Setenv("TESTAPP_JWT_ISSUER", "https://idp.example")

	strict, _, err := NewAuthenticators(slog.Default(), "TESTAPP", nil, nil)
	require.NoError(t, err)

	_, err = strict.Authenticate(t.Context(), idpToken(t, priv, defaultIdPKeyID, "https://other.example", "idp-native-subject"))
	require.ErrorIs(t, err, auth.ErrIssuerMismatch)
}

// TestNewAuthenticators_IdPPosture_IssuerRequired: a configured external
// source with no pinned iss is a hard misconfiguration, not a silent
// unpinned-issuer posture (Contract #11 §3.2).
func TestNewAuthenticators_IdPPosture_IssuerRequired(t *testing.T) {
	_, pubPEM := idpKeyPair(t)
	t.Setenv("TESTAPP_JWT_PUBLIC_KEY", pubPEM)
	t.Setenv("TESTAPP_JWT_ISSUER", "")

	strict, refresh, err := NewAuthenticators(slog.Default(), "TESTAPP", nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "TESTAPP_JWT_ISSUER")
	require.Nil(t, strict)
	require.Nil(t, refresh)
}

// TestNewAuthenticators_IdPPosture_BadPublicKey: a configured but
// unparseable key fails startup rather than degrading to a deny-all.
func TestNewAuthenticators_IdPPosture_BadPublicKey(t *testing.T) {
	t.Setenv("TESTAPP_JWT_PUBLIC_KEY", "not a pem")
	t.Setenv("TESTAPP_JWT_ISSUER", "https://idp.example")
	_, _, err := NewAuthenticators(slog.Default(), "TESTAPP", nil, nil)
	require.Error(t, err)
}

// TestNewAuthenticators_IdPPosture_RevocationCheckerWired proves the checker
// reaches the Authenticator the IdP posture builds: a revoked actor's
// otherwise-valid token is denied, not merely verified.
func TestNewAuthenticators_IdPPosture_RevocationCheckerWired(t *testing.T) {
	priv, pubPEM := idpKeyPair(t)
	t.Setenv("TESTAPP_JWT_PUBLIC_KEY", pubPEM)
	t.Setenv("TESTAPP_JWT_ISSUER", "https://idp.example")

	strict, _, err := NewAuthenticators(slog.Default(), "TESTAPP", nil, revokedChecker{})
	require.NoError(t, err)

	_, err = strict.Authenticate(t.Context(), idpToken(t, priv, defaultIdPKeyID, "https://idp.example", "idp-native-subject"))
	require.ErrorIs(t, err, auth.ErrTokenRevoked)
}

// TestNewAuthenticators_DevPosture_RevocationCheckerWired proves the same for
// the demo posture, on BOTH verifiers — the refresh endpoint must not be a
// way around the kill-switch.
func TestNewAuthenticators_DevPosture_RevocationCheckerWired(t *testing.T) {
	t.Setenv("TESTAPP_DEV_AUTH", "1")
	devKeyEnv(t, "TESTAPP")

	signer, err := NewDevSigner(slog.Default(), "TESTAPP", true)
	require.NoError(t, err)
	require.NotNil(t, signer)

	strict, refresh, err := NewAuthenticators(slog.Default(), "TESTAPP", signer, revokedChecker{})
	require.NoError(t, err)

	token, _, err := signer.Mint(testNanoID(t))
	require.NoError(t, err)
	_, err = strict.Authenticate(t.Context(), token)
	require.ErrorIs(t, err, auth.ErrTokenRevoked)
	_, err = refresh.Authenticate(t.Context(), token)
	require.ErrorIs(t, err, auth.ErrTokenRevoked)
}
