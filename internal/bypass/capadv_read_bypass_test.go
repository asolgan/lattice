// Package bypass — Phase 1 Gate 3: read-path authorization adversarial vectors (D1.4).
//
// The write-path vectors above (V1–V8) prove the Capability-Lens *write* boundary
// (Contract #6). These read-path vectors prove the symmetric *read* boundary
// (Contract #6 §6.14, design read-path-authorization-d1) the same way: each is an
// attack against the read perimeter that must DENY, exercised against the REAL
// enforcement code — the JWT actor-authentication seam (internal/gateway/auth) and
// the generated Postgres RLS policy (internal/refractor/adapter). They map 1:1 to
// the §5 read-bypass vector list:
//
//	ReadV1 — §5.1 direct read without a valid JWT          → authn DENIES (no actor reaches RLS)
//	ReadV2 — §5.2 actor A requests actor B's anchor        → RLS filters (set-membership over grants)
//	ReadV3 — §5.3 a revoked token (kill-switch)            → authn DENIES even on a valid signature
//	ReadV4 — §5.4 cross-anchor bleed (holds X, reads Y)    → RLS filters; coarse anchors still match (H5)
//	ReadV5 — §5.5 protected store with no RLS policy        → FORCE RLS deny-alls (H3 fail-closed)
//
// Two enforcement planes, two test postures:
//   - ReadV1/ReadV3 (authentication) are pure-Go against auth.Authenticator — no
//     infra; they always run.
//   - ReadV2/ReadV4/ReadV5 (row authorization) drive a live Postgres through the
//     platform DDL/grant-writer; they require POSTGRES_TEST_DSN (set by the
//     make test-capability-adversarial gate, which brings up the Docker stack).
//     Reads run under a non-superuser role because a superuser bypasses RLS — the
//     posture a real read boundary uses.
//
// Report rows 9–13 (gate3_test.go) carry these vectors; DEFENDED when each denies.
package bypass

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/gateway/auth"
	"github.com/asolgan/lattice/internal/refractor/adapter"
)

// ── Authentication-plane vectors (ReadV1, ReadV3) — pure Go ───────────────────

const (
	readAuthKID = "gate3-read-key"
	readAuthSub = "Hj4kPmRtw9nbCxz5vQ2y" // bare identity id → vtx.identity.<sub>
	readAuthIss = "https://idp.gate3.test"
	readAuthAud = "lattice-read"
)

// fakeRevocation is a RevocationChecker stub. revoked drives the kill-switch
// outcome; err simulates a revocation-store failure (which must fail closed).
type fakeRevocation struct {
	revoked bool
	err     error
}

func (f fakeRevocation) IsRevoked(ctx context.Context, actorID string) (bool, error) {
	return f.revoked, f.err
}

// newReadRSA generates a fresh 2048-bit RSA key for a test verifier/signer.
func newReadRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

// newReadVerifier builds a Verifier trusting a freshly-generated RSA key, plus a
// signer for that key. Mirrors the production read-boundary wiring in
// cmd/loftspace-app/readauth.go. Claims are anchored on the real wall clock (the
// verifier's clock is package-private), so the windows below use time.Now().
func newReadVerifier(t *testing.T) (*auth.Verifier, *rsaSigner) {
	t.Helper()
	priv := newReadRSA(t)
	v, err := auth.NewVerifier(auth.Config{
		Keys:      map[string]crypto.PublicKey{readAuthKID: &priv.PublicKey},
		Issuer:    readAuthIss,
		Audience:  readAuthAud,
		ClockSkew: 60 * time.Second,
	})
	require.NoError(t, err)
	return v, &rsaSigner{priv: priv, kid: readAuthKID}
}

// validClaims is a structurally-valid, live, correctly-scoped claim set.
func validClaims() jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   readAuthSub,
		Issuer:    readAuthIss,
		Audience:  jwt.ClaimStrings{readAuthAud},
		ID:        "tok-gate3-read",
		IssuedAt:  jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		NotBefore: jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}
}

// TestCapAdv_ReadV1_NoValidJWT_Denied — §5.1. The read boundary authenticates
// before any row is served (cmd/loftspace-app/readauth.go: a nil/invalid token →
// 401, no lattice.actor_id set → RLS sees a NULL actor → deny-all). Every
// no-valid-credential shape must fail to produce an actor; none may pass.
func TestCapAdv_ReadV1_NoValidJWT_Denied(t *testing.T) {
	v, signer := newReadVerifier(t)
	authn := auth.NewAuthenticator(v, nil)
	ctx := context.Background()

	// Sanity anchor: the harness CAN authenticate a good token — so the denials
	// below are due to the attack shape, not a broken fixture.
	good := signer.sign(t, validClaims())
	actor, err := authn.Authenticate(ctx, good)
	require.NoError(t, err)
	require.Equal(t, auth.IdentityKeyPrefix+readAuthSub, actor.ActorID)

	t.Run("empty token", func(t *testing.T) {
		_, err := authn.Authenticate(ctx, "")
		assert.Error(t, err, "no token → no actor → boundary denies")
	})
	t.Run("malformed token", func(t *testing.T) {
		_, err := authn.Authenticate(ctx, "not.a.jwt")
		assert.ErrorIs(t, err, auth.ErrMalformedToken)
	})
	t.Run("untrusted signer", func(t *testing.T) {
		// A well-formed token signed by a key the boundary does not trust.
		forgerPriv := newReadRSA(t)
		forged := (&rsaSigner{priv: forgerPriv, kid: readAuthKID}).sign(t, validClaims())
		_, err := authn.Authenticate(ctx, forged)
		assert.ErrorIs(t, err, auth.ErrInvalidSignature)
	})
	t.Run("expired token", func(t *testing.T) {
		expired := validClaims()
		expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute))
		tok := signer.sign(t, expired)
		_, err := authn.Authenticate(ctx, tok)
		assert.ErrorIs(t, err, auth.ErrTokenExpired)
	})
	t.Run("none-alg token", func(t *testing.T) {
		// The classic alg-none bypass: an unsigned token must never authenticate.
		unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims())
		raw, signErr := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, signErr)
		_, err := authn.Authenticate(ctx, raw)
		assert.ErrorIs(t, err, auth.ErrUnsupportedAlgorithm)
	})
}

// TestCapAdv_ReadV3_RevokedToken_Denied — §5.3. A structurally-valid, unexpired,
// correctly-signed token whose actor is on the revocation kill-switch must be
// DENIED (auth.Authenticator consults the RevocationChecker after a successful
// verify). A revocation-store error must fail closed.
func TestCapAdv_ReadV3_RevokedToken_Denied(t *testing.T) {
	v, signer := newReadVerifier(t)
	ctx := context.Background()
	tok := signer.sign(t, validClaims())

	t.Run("not revoked → passes (control)", func(t *testing.T) {
		authn := auth.NewAuthenticator(v, fakeRevocation{revoked: false})
		actor, err := authn.Authenticate(ctx, tok)
		require.NoError(t, err, "the token is otherwise valid — proves the deny below is the kill-switch")
		assert.Equal(t, auth.IdentityKeyPrefix+readAuthSub, actor.ActorID)
	})
	t.Run("revoked → denied", func(t *testing.T) {
		authn := auth.NewAuthenticator(v, fakeRevocation{revoked: true})
		_, err := authn.Authenticate(ctx, tok)
		assert.ErrorIs(t, err, auth.ErrTokenRevoked,
			"a valid signature is no defense once the actor is revoked")
	})
	t.Run("revocation-store error → fail closed", func(t *testing.T) {
		authn := auth.NewAuthenticator(v, fakeRevocation{err: errors.New("kv unreachable")})
		_, err := authn.Authenticate(ctx, tok)
		require.Error(t, err, "cannot confirm the actor is live ⇒ deny")
		assert.NotErrorIs(t, err, auth.ErrTokenRevoked, "distinct from a positive revoke")
	})
}

// ── Authorization-plane vectors (ReadV2, ReadV4, ReadV5) — live Postgres RLS ───

// readRLSHarness provisions the shared actor_read_grants table (via the real
// PostgresGrantWriter) and a non-superuser reader role, returning a pool and the
// grant writer. Per-vector tables/rows are created with unique suffixes so the
// suite is safe under -p parallel CI on a shared database.
type readRLSHarness struct {
	pool   *pgxpool.Pool
	writer *adapter.PostgresGrantWriter
	role   string
}

const readRLSReaderRole = "rls_read_bypass_reader"

func setupReadRLS(t *testing.T) *readRLSHarness {
	t.Helper()
	dsn := skipReadBypassWithoutPostgres(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	w, err := adapter.NewPostgresGrantWriter(pool, 10*time.Second)
	require.NoError(t, err)
	require.NoError(t, w.Provision(ctx), "provision actor_read_grants")

	// Non-superuser reader role — a superuser bypasses RLS, so the deny vectors
	// would falsely pass under the default dev/CI superuser.
	_, err = pool.Exec(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='`+readRLSReaderRole+`') THEN
			CREATE ROLE `+readRLSReaderRole+` NOLOGIN;
		END IF; END $$;`)
	require.NoError(t, err)

	return &readRLSHarness{pool: pool, writer: w, role: readRLSReaderRole}
}

// provisionProtected creates a protected read-model table (with the generated
// FORCE-RLS set-membership policy), GRANTs SELECT to the reader role, and
// registers teardown.
func (h *readRLSHarness) provisionProtected(t *testing.T, table string) {
	t.Helper()
	stmts, err := adapter.BuildProtectedTableDDL(table, []string{"id"}, []adapter.ColumnDef{{Name: "body", Type: "text"}})
	require.NoError(t, err)
	h.execAll(t, stmts)
	h.grantAndCleanup(t, table)
}

// provisionUnpolicied creates a table with ENABLE+FORCE RLS but NO policy — the
// §5.5 "author forgot the anchor/policy" case. Under FORCE RLS this must deny all
// rows (H3), never serve them.
func (h *readRLSHarness) provisionUnpolicied(t *testing.T, table string) {
	t.Helper()
	stmts, err := adapter.BuildProtectedTableDDL(table, []string{"id"}, []adapter.ColumnDef{{Name: "body", Type: "text"}})
	require.NoError(t, err)
	h.execAll(t, stmts[:3]) // create table, enable rls, force rls — skip drop+create policy
	h.grantAndCleanup(t, table)
}

func (h *readRLSHarness) execAll(t *testing.T, stmts []string) {
	t.Helper()
	ctx := context.Background()
	for _, s := range stmts {
		_, err := h.pool.Exec(ctx, s)
		require.NoError(t, err, "exec: %s", s)
	}
}

func (h *readRLSHarness) grantAndCleanup(t *testing.T, table string) {
	t.Helper()
	ctx := context.Background()
	_, err := h.pool.Exec(ctx, fmt.Sprintf(`GRANT SELECT ON "%s" TO %s`, table, h.role))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DROP TABLE IF EXISTS "`+table+`"`)
	})
	// also let the reader role read the grant table the policy joins against
	_, _ = h.pool.Exec(ctx, fmt.Sprintf(`GRANT SELECT ON "%s" TO %s`, adapter.GrantTable, h.role))
}

// seedRow inserts a row as superuser (bypassing RLS) with the given authz anchors.
func (h *readRLSHarness) seedRow(t *testing.T, table, id string, anchors []string) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(),
		fmt.Sprintf(`INSERT INTO "%s" (id, body, authz_anchors, projection_seq) VALUES ($1,$2,$3,$4)`, table),
		id, "secret-"+id, anchors, 1)
	require.NoError(t, err)
}

// grant records a live grant for actor→anchor through the real seq-guarded writer.
func (h *readRLSHarness) grant(t *testing.T, actor, anchor, source string) {
	t.Helper()
	require.NoError(t, h.writer.UpsertGrant(context.Background(), actor, anchor, source, 1))
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM "`+adapter.GrantTable+`" WHERE actor_id=$1`, actor)
	})
}

// visibleAs counts the rows the given actor can SELECT from table, reading under
// the non-superuser reader role with lattice.actor_id set transaction-locally —
// exactly the read boundary's posture.
func (h *readRLSHarness) visibleAs(t *testing.T, table, actor string) int {
	t.Helper()
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SET LOCAL ROLE "+h.role)
	require.NoError(t, err)
	if actor != "" {
		_, err = tx.Exec(ctx, "SELECT set_config('lattice.actor_id', $1, true)", actor)
		require.NoError(t, err)
	}
	var n int
	require.NoError(t, tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, table)).Scan(&n))
	return n
}

// TestCapAdv_ReadV2_CrossActorAnchor_Filtered — §5.2. Two actors with disjoint
// grants read the same protected table. Each sees only rows anchored to a grant
// they hold; neither sees the other's rows; an ungranted actor sees nothing.
func TestCapAdv_ReadV2_CrossActorAnchor_Filtered(t *testing.T) {
	h := setupReadRLS(t)
	suffix := sanitizeReadBypass(t.Name())
	tbl := "rbv2_" + suffix
	h.provisionProtected(t, tbl)

	actorA := "actorA_" + suffix
	actorB := "actorB_" + suffix
	anchorA := "anchorA_" + suffix
	anchorB := "anchorB_" + suffix

	h.seedRow(t, tbl, "rowA", []string{anchorA}) // belongs to A
	h.seedRow(t, tbl, "rowB", []string{anchorB}) // belongs to B
	h.grant(t, actorA, anchorA, "cap-read.test")
	h.grant(t, actorB, anchorB, "cap-read.test")

	assert.Equal(t, 1, h.visibleAs(t, tbl, actorA), "A sees only A's row")
	assert.Equal(t, 1, h.visibleAs(t, tbl, actorB), "B sees only B's row")
	// The headline: A holds a grant, but NOT for B's anchor — A must not see rowB.
	// (visibleAs counts all visible rows; A==1 above already excludes rowB, but
	// assert the cross-actor isolation explicitly via an ungranted third actor.)
	assert.Equal(t, 0, h.visibleAs(t, tbl, "stranger_"+suffix), "an ungranted actor sees nothing")
	assert.Equal(t, 0, h.visibleAs(t, tbl, ""), "an unauthenticated (NULL) actor sees nothing")
}

// TestCapAdv_ReadV4_CrossAnchorBleed_Filtered — §5.4. An actor holding a grant
// for anchor X must not read a row anchored only to Y. The §6.14 set-membership
// (H5) is also asserted: a coarse grant (building.B) covers a row tagged with the
// coarse anchor, while a fine-only holder (unit.X) does not see a building-only row.
func TestCapAdv_ReadV4_CrossAnchorBleed_Filtered(t *testing.T) {
	h := setupReadRLS(t)
	suffix := sanitizeReadBypass(t.Name())
	tbl := "rbv4_" + suffix
	h.provisionProtected(t, tbl)

	unitX := "unitx_" + suffix
	unitY := "unity_" + suffix
	buildingB := "buildingb_" + suffix

	fineActor := "fine_" + suffix   // holds unit.X only
	coarseActor := "coarse_" + suffix // holds building.B only
	h.grant(t, fineActor, unitX, "cap-read.residence")
	h.grant(t, coarseActor, buildingB, "cap-read.residence")

	h.seedRow(t, tbl, "rowX", []string{unitX, buildingB})    // a unit in building B
	h.seedRow(t, tbl, "rowY", []string{unitY, buildingB})    // a different unit in building B
	h.seedRow(t, tbl, "rowOrphan", []string{unitY})          // a unit the actors have no grant for

	// fineActor (unit.X) sees rowX (holds unit.X), NOT rowY/rowOrphan (no Y grant).
	assert.Equal(t, 1, h.visibleAs(t, tbl, fineActor), "unit.X holder sees only the unit.X row — no cross-anchor bleed")
	// coarseActor (building.B) sees BOTH rowX and rowY (both tagged building.B),
	// but NOT rowOrphan (tagged unit.Y only). This is the H5 hierarchical grant.
	assert.Equal(t, 2, h.visibleAs(t, tbl, coarseActor), "building.B holder sees every unit in the building, not the orphan")
}

// TestCapAdv_ReadV5_NoAnchorPolicy_FailsClosed — §5.5. A protected store shipped
// WITHOUT its RLS policy (the "author forgot the authz anchor / policy" case)
// must deny ALL rows under FORCE RLS — a fail-closed outage (H3), never a silent
// world-publish. A correctly-policied table, by contrast, serves a granted row.
func TestCapAdv_ReadV5_NoAnchorPolicy_FailsClosed(t *testing.T) {
	h := setupReadRLS(t)
	suffix := sanitizeReadBypass(t.Name())
	policied := "rbv5ok_" + suffix
	nopolicy := "rbv5bad_" + suffix
	actor := "actor_" + suffix
	anchor := "anchor_" + suffix

	h.provisionProtected(t, policied)
	h.provisionUnpolicied(t, nopolicy)
	h.grant(t, actor, anchor, "cap-read.test")
	h.seedRow(t, policied, "row1", []string{anchor})
	h.seedRow(t, nopolicy, "row1", []string{anchor})

	// Control: the policied table serves the granted row.
	assert.Equal(t, 1, h.visibleAs(t, policied, actor), "policied table serves the granted row")
	// Headline: the policy-less protected table denies ALL rows even for the same
	// granted actor — FORCE RLS + no policy ⇒ deny-all (never serve-all).
	assert.Equal(t, 0, h.visibleAs(t, nopolicy, actor), "policy-less protected table fails closed")
}

// ── shared helpers ────────────────────────────────────────────────────────────

func skipReadBypassWithoutPostgres(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Postgres RLS read-bypass vector in short mode")
	}
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("skipping: POSTGRES_TEST_DSN not set (the make test-capability-adversarial gate sets it)")
	}
	return dsn
}

// sanitizeReadBypass maps a Go test name to a lowercase identifier-safe suffix
// for per-test table/actor names.
func sanitizeReadBypass(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// rsaSigner mints RS256 tokens for the trusted read key (the read analog of the
// dev signer in cmd/loftspace-app/readauth.go).
type rsaSigner struct {
	priv *rsa.PrivateKey
	kid  string
}

func (s *rsaSigner) sign(t *testing.T, c jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = s.kid
	raw, err := tok.SignedString(s.priv)
	require.NoError(t, err)
	return raw
}
