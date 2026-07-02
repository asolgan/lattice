package auth

import (
	"context"
	"crypto"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// jwksServer serves whatever body is currently stored in it, and can flip
// between a healthy JWKS response and a failure on demand — used to prove
// JWKSPoller's fail-safe (keep the last-known-good set) behavior.
type jwksServer struct {
	body   atomic.Pointer[[]byte]
	fail   atomic.Bool
	hits   atomic.Int64
	server *httptest.Server
}

func newJWKSServer(t *testing.T, initialBody []byte) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.body.Store(&initialBody)
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if s.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(*s.body.Load())
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *jwksServer) setBody(b []byte) { s.body.Store(&b) }
func (s *jwksServer) setFail(v bool)   { s.fail.Store(v) }
func (s *jwksServer) url() string      { return s.server.URL }

func TestJWKSPoller_FetchOnce_PopulatesVerifier(t *testing.T) {
	kp := newRSA(t)
	srv := newJWKSServer(t, marshalJWKS(t, rsaJWK("k1", kp.pub)))

	v := verifierFor(t, map[string]crypto.PublicKey{"placeholder": newRSA(t).pub})
	poller := NewJWKSPoller(srv.url(), v, nil, 0, nil)

	if err := poller.FetchOnce(context.Background()); err != nil {
		t.Fatalf("FetchOnce: %v", err)
	}
	tok := signRS256(t, kp.priv, "k1", claims())
	if _, err := v.Verify(tok); err != nil {
		t.Fatalf("Verify after FetchOnce: %v", err)
	}
	// The placeholder key from construction must be gone — FetchOnce fully
	// replaces the JWKS-sourced portion (rotation removes retired keys).
	other := signRS256(t, kp.priv, "placeholder", claims())
	if _, err := v.Verify(other); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("placeholder kid should be retired after a JWKS swap, got err=%v", err)
	}
}

func TestJWKSPoller_FetchOnce_MergesStaticKeys(t *testing.T) {
	jwksKP := newRSA(t)
	devKP := newRSA(t)
	srv := newJWKSServer(t, marshalJWKS(t, rsaJWK("idp1", jwksKP.pub)))

	v := verifierFor(t, map[string]crypto.PublicKey{"dev": devKP.pub})
	poller := NewJWKSPoller(srv.url(), v, map[string]crypto.PublicKey{"dev": devKP.pub}, 0, nil)

	if err := poller.FetchOnce(context.Background()); err != nil {
		t.Fatalf("FetchOnce: %v", err)
	}

	if _, err := v.Verify(signRS256(t, jwksKP.priv, "idp1", claims())); err != nil {
		t.Errorf("Verify(idp1) after FetchOnce: %v", err)
	}
	if _, err := v.Verify(signRS256(t, devKP.priv, "dev", claims())); err != nil {
		t.Errorf("Verify(dev) after FetchOnce: %v — static keys must survive every swap", err)
	}
}

func TestJWKSPoller_FetchOnce_FailureLeavesVerifierUnchanged(t *testing.T) {
	kp := newRSA(t)
	srv := newJWKSServer(t, marshalJWKS(t, rsaJWK("k1", kp.pub)))

	v := verifierFor(t, map[string]crypto.PublicKey{"seed": kp.pub})
	poller := NewJWKSPoller(srv.url(), v, nil, 0, nil)

	srv.setFail(true)
	if err := poller.FetchOnce(context.Background()); err == nil {
		t.Fatal("expected an error from a failing JWKS endpoint")
	}

	// The pre-existing "seed" key (from construction, not from this poller)
	// must still verify — FetchOnce's failure must not have swapped in an
	// empty or partial key set.
	tok := signRS256(t, kp.priv, "seed", claims())
	if _, err := v.Verify(tok); err != nil {
		t.Errorf("Verify(seed) after a failed FetchOnce: %v — a failed poll must not clobber the trusted set", err)
	}
}

func TestJWKSPoller_Rotation(t *testing.T) {
	kp1 := newRSA(t)
	kp2 := newRSA(t)
	srv := newJWKSServer(t, marshalJWKS(t, rsaJWK("k1", kp1.pub)))

	v := verifierFor(t, map[string]crypto.PublicKey{"seed": kp1.pub})
	poller := NewJWKSPoller(srv.url(), v, nil, 0, nil)

	if err := poller.FetchOnce(context.Background()); err != nil {
		t.Fatalf("FetchOnce (round 1): %v", err)
	}
	tok1 := signRS256(t, kp1.priv, "k1", claims())
	if _, err := v.Verify(tok1); err != nil {
		t.Fatalf("Verify(k1) round 1: %v", err)
	}

	// IdP rotates: k1 retired, k2 is now the only published key.
	srv.setBody(marshalJWKS(t, rsaJWK("k2", kp2.pub)))
	if err := poller.FetchOnce(context.Background()); err != nil {
		t.Fatalf("FetchOnce (round 2): %v", err)
	}
	tok2 := signRS256(t, kp2.priv, "k2", claims())
	if _, err := v.Verify(tok2); err != nil {
		t.Errorf("Verify(k2) round 2: %v", err)
	}
	if _, err := v.Verify(tok1); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("Verify(k1) round 2: err = %v, want ErrUnknownKey (k1 was retired)", err)
	}
}

func TestJWKSPoller_IntervalClamping(t *testing.T) {
	v := verifierFor(t, map[string]crypto.PublicKey{"seed": newRSA(t).pub})

	if p := NewJWKSPoller("https://example.test", v, nil, 0, nil); p.interval != DefaultJWKSPollInterval {
		t.Errorf("interval = %v, want default %v", p.interval, DefaultJWKSPollInterval)
	}
	if p := NewJWKSPoller("https://example.test", v, nil, MinJWKSPollInterval/2, nil); p.interval != MinJWKSPollInterval {
		t.Errorf("interval = %v, want clamped floor %v", p.interval, MinJWKSPollInterval)
	}
}
