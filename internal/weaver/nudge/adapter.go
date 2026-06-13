package nudge

import (
	"context"
	"fmt"
	"sync"
)

// Request is one external-call dispatch handed to an Adapter. IdempotencyKey is
// the claimId (Contract #10 §10.3): the adapter MUST treat two Requests bearing
// the same key as the same action and produce at most one external side-effect.
// Operation/Subject/Params carry the §10.8 nudge action's resolved fields so an
// adapter can shape the real external call (which external endpoint, on whose
// behalf, with what arguments).
type Request struct {
	IdempotencyKey string
	Operation      string
	Subject        string
	Params         map[string]string
}

// Result is an Adapter's response to a successful Execute. Detail is an
// adapter-defined opaque outcome string (a confirmation reference, a decision)
// carried into the resolve op's payload for the audit join; it is never
// interpreted by the framework.
type Result struct {
	Detail string
}

// Adapter is the unit of "call one external system idempotently" — the external
// integration each nudge action resolves to. The framework calls Execute under
// the claim's state=executing, having already recorded a visible claim
// (state=claimed) in weaver-claims; the adapter owns the real external action.
//
// The idempotencyKey on the Request (= the claimId) is the contract: the
// adapter is the de-dup boundary, NOT Weaver. Two Execute calls with the same
// idempotencyKey MUST yield exactly one external side-effect and the same
// Result — this is what makes a recovery re-execute (same claimId) safe. A
// returned error is a (possibly transient) failure: the framework lands the
// claim in state=failed and surfaces it; it does not retry inline.
type Adapter interface {
	Execute(ctx context.Context, req Request) (Result, error)
}

// AdapterFunc adapts a plain function to the Adapter interface — the usual
// convenience for a one-method interface (and a clean seam for tests and small
// inline adapters).
type AdapterFunc func(ctx context.Context, req Request) (Result, error)

// Execute calls the underlying function.
func (f AdapterFunc) Execute(ctx context.Context, req Request) (Result, error) {
	return f(ctx, req)
}

// Registry resolves an adapter name (the §10.8 GapAction.Adapter field) to a
// concrete Adapter at dispatch time. A nudge action naming an unregistered
// adapter is a config error, surfaced by Lookup's ok=false (never a silent
// no-op) — the caller alerts, mirroring the engine's errConfig posture.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry returns an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register binds name to adapter. A blank name or nil adapter is rejected, and
// re-registering an already-bound name is rejected — an adapter set is built
// once at engine construction, so a duplicate name is a wiring bug, surfaced
// rather than silently shadowing the prior binding.
func (r *Registry) Register(name string, adapter Adapter) error {
	if name == "" {
		return fmt.Errorf("nudge: adapter name is required")
	}
	if adapter == nil {
		return fmt.Errorf("nudge: adapter %q is nil", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[name]; exists {
		return fmt.Errorf("nudge: adapter %q already registered", name)
	}
	r.adapters[name] = adapter
	return nil
}

// Lookup resolves an adapter name to its registered Adapter. ok=false means no
// adapter is bound to that name — a nudge action's config error the caller must
// surface, never treat as a silent skip.
func (r *Registry) Lookup(name string) (adapter Adapter, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}
