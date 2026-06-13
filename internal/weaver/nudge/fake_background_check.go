package nudge

import (
	"context"
	"sync"
)

// FakeBackgroundCheck is the reference Adapter that proves the framework
// end-to-end without real I/O. It is the literal demonstration of external
// idempotency: it records every idempotencyKey it has executed and, on a repeat
// key, returns the SAME Result WITHOUT a second side-effect (the per-key
// side-effect counter does not increment). Demo / Phase-2 adapters are mocked
// like this; the real Stripe / background-check integrations are Phase 3
// (docs/components/weaver.md Two-Phase Nudge).
type FakeBackgroundCheck struct {
	mu sync.Mutex
	// results memoizes the Result returned for each idempotencyKey, so a repeat
	// key returns the first call's result verbatim.
	results map[string]Result
	// calls counts the side-effects actually performed per idempotencyKey — the
	// idempotency assertion: a repeat key must leave its count at 1.
	calls map[string]int
}

// NewFakeBackgroundCheck returns a fresh in-memory reference adapter.
func NewFakeBackgroundCheck() *FakeBackgroundCheck {
	return &FakeBackgroundCheck{
		results: make(map[string]Result),
		calls:   make(map[string]int),
	}
}

// Execute performs the (mocked) external action exactly once per
// idempotencyKey. The first call for a key records the side-effect and a
// deterministic Result; any later call with the same key returns that Result
// and performs NO further side-effect. No network, no real I/O.
func (f *FakeBackgroundCheck) Execute(_ context.Context, req Request) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if res, seen := f.results[req.IdempotencyKey]; seen {
		return res, nil
	}
	f.calls[req.IdempotencyKey]++
	res := Result{Detail: "background-check cleared for " + req.Subject}
	f.results[req.IdempotencyKey] = res
	return res, nil
}

// SideEffects reports how many times the real external action was performed for
// idempotencyKey — 0 before the first Execute, and exactly 1 no matter how many
// repeat Executes follow (the idempotency proof tests assert this).
func (f *FakeBackgroundCheck) SideEffects(idempotencyKey string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[idempotencyKey]
}
