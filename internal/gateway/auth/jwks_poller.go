package auth

import (
	"context"
	"crypto"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxJWKSBodyBytes bounds a fetched JWKS document. A real IdP's key set is a
// handful of RSA/EC keys — a few KiB. 1 MiB matches the Gateway's own request
// body ceiling (internal/gateway.maxBodyBytes) as a generous, still-bounded
// backstop against a misbehaving or compromised JWKS endpoint.
const maxJWKSBodyBytes = 1 << 20

// MinJWKSPollInterval is the floor JWKSPoller enforces on its poll interval —
// a misconfigured near-zero interval must not turn into a request storm
// against the IdP's JWKS endpoint.
const MinJWKSPollInterval = 30 * time.Second

// DefaultJWKSPollInterval is used when the caller passes interval <= 0.
const DefaultJWKSPollInterval = 5 * time.Minute

// Logger is the minimal logging surface JWKSPoller needs. *slog.Logger and
// internal/gateway.Logger both satisfy it structurally.
type Logger interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// JWKSPoller periodically fetches a JWKS document and hot-swaps the trusted
// key set on a Verifier via SetKeys, so a rotated IdP signing key is picked
// up without a Gateway restart (design gateway-external-trust-boundary
// §8 Fire 2 remainder). staticKeys (e.g. a dev/break-glass key) are ALWAYS
// merged into every swap, layered under the JWKS-fetched keys — a rotating
// JWKS response can add or retire IdP keys but can never un-trust a key the
// operator configured directly.
//
// Fail-safe, not fail-closed, on a live poll: a transient fetch/parse
// failure (network blip, IdP outage, momentarily-empty response) logs and
// KEEPS the last-known-good key set rather than swapping in nothing — a live
// session must not be locked out by a passing IdP hiccup. The fail-CLOSED
// gate (no trusted keys at all) is enforced once, at startup, by the
// caller's use of FetchOnce before serving traffic (mirrors the existing
// "no trusted keys configured — refusing to start" posture in cmd/gateway).
type JWKSPoller struct {
	url        string
	client     *http.Client
	verifier   *Verifier
	staticKeys map[string]crypto.PublicKey
	interval   time.Duration
	logger     Logger
}

// NewJWKSPoller builds a poller for url, hot-swapping verifier's trusted keys
// on each successful fetch. staticKeys may be nil. interval <= 0 uses
// DefaultJWKSPollInterval; a positive interval below MinJWKSPollInterval is
// clamped up to it. logger may be nil (discards).
func NewJWKSPoller(url string, verifier *Verifier, staticKeys map[string]crypto.PublicKey, interval time.Duration, logger Logger) *JWKSPoller {
	if interval <= 0 {
		interval = DefaultJWKSPollInterval
	} else if interval < MinJWKSPollInterval {
		interval = MinJWKSPollInterval
	}
	if logger == nil {
		logger = nopLogger{}
	}
	return &JWKSPoller{
		url:        url,
		client:     &http.Client{Timeout: 10 * time.Second},
		verifier:   verifier,
		staticKeys: staticKeys,
		interval:   interval,
		logger:     logger,
	}
}

// FetchOnce fetches and parses the JWKS document and, on success, swaps it
// (merged with staticKeys) into the Verifier. It returns an error on any
// fetch/parse/empty-result failure WITHOUT touching the Verifier's current
// key set — the caller decides whether that failure is fatal (fail-closed at
// startup) or tolerable (a background Run tick, which logs and continues).
func (p *JWKSPoller) FetchOnce(ctx context.Context) error {
	keys, skipped, err := p.fetch(ctx)
	if err != nil {
		return err
	}
	for _, s := range skipped {
		p.logger.Warn("gateway: JWKS entry skipped", "reason", s)
	}
	merged := make(map[string]crypto.PublicKey, len(keys)+len(p.staticKeys))
	for kid, k := range keys {
		merged[kid] = k
	}
	// staticKeys layered last: an operator-configured key (dev/break-glass)
	// always wins over a same-kid collision from the JWKS response, though a
	// real IdP has no reason to publish the "dev" kid.
	for kid, k := range p.staticKeys {
		merged[kid] = k
	}
	p.verifier.SetKeys(merged)
	return nil
}

// Run polls at the configured interval until ctx is done. A failed poll is
// logged and does not stop the loop (fail-safe — see the type doc). Call
// FetchOnce once, synchronously, before Run for the fail-closed startup gate;
// Run itself never blocks the caller past its first tick.
func (p *JWKSPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.FetchOnce(ctx); err != nil {
				p.logger.Error("gateway: JWKS refresh failed; keeping the last trusted key set", "url", p.url, "error", err)
			}
		}
	}
}

func (p *JWKSPoller) fetch(ctx context.Context) (keys map[string]crypto.PublicKey, skipped []string, err error) {
	return FetchJWKS(ctx, p.url, p.client)
}

// defaultJWKSClient is used by FetchJWKS when client is nil — a bounded
// timeout so a one-shot startup fetch (e.g. cmd/gateway's fail-closed initial
// load) cannot hang indefinitely against an unresponsive endpoint.
var defaultJWKSClient = &http.Client{Timeout: 10 * time.Second}

// FetchJWKS fetches url and parses it as a JWKS document (ParseJWKS), bounded
// to maxJWKSBodyBytes. client may be nil to use a default 10s-timeout client.
// Exposed at package level so both JWKSPoller and a one-shot caller (e.g. an
// initial fail-closed startup fetch) share one fetch implementation.
func FetchJWKS(ctx context.Context, url string, client *http.Client) (keys map[string]crypto.PublicKey, skipped []string, err error) {
	if client == nil {
		client = defaultJWKSClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fetch JWKS: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBodyBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read JWKS response: %w", err)
	}
	if len(body) > maxJWKSBodyBytes {
		return nil, nil, fmt.Errorf("JWKS response exceeds %d bytes", maxJWKSBodyBytes)
	}

	return ParseJWKS(body)
}
