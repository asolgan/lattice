package nudge

import (
	"context"
	"fmt"
	"sync"
)

// FakeStripe is a reference Adapter modeling an idempotent external payment
// call (the canonical FR58 example: a charge that must never double-bill). Like
// FakeBackgroundCheck it is deterministic, in-memory, and performs NO real I/O;
// it records every idempotencyKey it has charged and, on a repeat key, returns
// the SAME Result with NO second side-effect (the per-key side-effect counter
// does not increment). Demo / Phase-2 adapters are mocked like this; the real
// Stripe integration is Phase 3 (docs/components/weaver.md Two-Phase Nudge).
//
// FailUntil configures a fail-once / fail-n mode for the idempotency proof: the
// first failUntil Execute calls (across ALL keys) return an error, and crucially
// a FAILED attempt records NO side-effect — a charge that errored did not charge.
// So a claim that fails its first attempt and is later re-driven on the SAME
// idempotencyKey converges to exactly one side-effect: the eventual success. A
// failing attempt does not memoize a Result either, so the retry runs the real
// (now-succeeding) charge path rather than replaying a phantom success.
type FakeStripe struct {
	mu sync.Mutex
	// results memoizes the Result returned for each successfully-charged
	// idempotencyKey, so a repeat key returns the first successful call verbatim.
	results map[string]Result
	// calls counts the side-effects actually performed per idempotencyKey — the
	// idempotency assertion: a repeat key must leave its count at 1.
	calls map[string]int
	// failRemaining is the number of upcoming Execute calls that will hard-fail
	// before any side-effect; FailUntil sets it, each failed attempt decrements
	// it. A failed attempt records no side-effect and no memoized Result.
	failRemaining int
}

// NewFakeStripe returns a fresh in-memory reference payment adapter.
func NewFakeStripe() *FakeStripe {
	return &FakeStripe{
		results: make(map[string]Result),
		calls:   make(map[string]int),
	}
}

// FailUntil arms the adapter to hard-fail its next n Execute calls (across all
// keys) before performing any side-effect. n <= 0 disarms the failure mode. A
// failed attempt records no side-effect and no memoized Result, so a later retry
// on the same idempotencyKey runs the real charge and the eventual single
// success is the only side-effect.
func (f *FakeStripe) FailUntil(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failRemaining = n
}

// FailNext arms the adapter to hard-fail exactly the next Execute call.
func (f *FakeStripe) FailNext() { f.FailUntil(1) }

// Execute performs the (mocked) charge exactly once per idempotencyKey. While
// the failure mode is armed it returns an error WITHOUT charging (no side-effect,
// no memoized Result). Otherwise the first call for a key records the side-effect
// and a deterministic Result; any later call with the same key returns that
// Result and performs NO further side-effect. No network, no real I/O.
func (f *FakeStripe) Execute(_ context.Context, req Request) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRemaining > 0 {
		f.failRemaining--
		return Result{}, fmt.Errorf("nudge: FakeStripe injected failure (no charge performed) for key %s", req.IdempotencyKey)
	}
	if res, seen := f.results[req.IdempotencyKey]; seen {
		return res, nil
	}
	f.calls[req.IdempotencyKey]++
	res := Result{Detail: "charge confirmed for " + req.Subject}
	f.results[req.IdempotencyKey] = res
	return res, nil
}

// SideEffects reports how many times the real charge was performed for
// idempotencyKey — 0 before the first successful Execute, and exactly 1 no
// matter how many repeat (or post-failure retry) Executes follow on the same key
// (the FR58 idempotency proof asserts this is at most 1).
func (f *FakeStripe) SideEffects(idempotencyKey string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[idempotencyKey]
}
