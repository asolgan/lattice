package bridge

import (
	"context"
	"fmt"
	"sync"
)

// BackgroundCheckDeclineSubject is the designated trigger that makes
// FakeBackgroundCheck return a terminal OutcomeFailed (a rejected check) instead
// of clearing — exercising the failed-outcome path end-to-end. Any other subject
// clears (OutcomeCompleted). It is the instanceKey the bridge passes as
// Request.Subject (the opaque handle), so a test selects the failed path by
// minting the instance with this handle.
const BackgroundCheckDeclineSubject = "decline-background-check"

// FakeBackgroundCheck is a reference Adapter that proves the bridge end-to-end
// without real I/O. It is the literal demonstration of external idempotency: it
// records every idempotencyKey it has executed and, on a repeat key, returns
// the SAME Result WITHOUT a second side-effect (the per-key side-effect counter
// does not increment). Demo / Phase-2 adapters are mocked like this; the real
// Stripe / background-check integrations are Phase 3 (docs/components/bridge.md).
type FakeBackgroundCheck struct {
	mu sync.Mutex
	// results memoizes the Result returned for each idempotencyKey, so a repeat
	// key returns the first call's result verbatim.
	results map[string]Result
	// calls counts the side-effects actually performed per idempotencyKey — the
	// idempotency assertion: a repeat key must leave its count at 1.
	calls map[string]int
	// declineAll, when set, makes EVERY subject decline (terminal OutcomeFailed),
	// not only BackgroundCheckDeclineSubject — the operator-driven demo affordance
	// (SetDeclineAll, wired to the bridge's BRIDGE_FAKE_DECLINE env) for exercising
	// the declined-application experience live, where the instanceKey-based subject
	// trigger is not reachable from the applicant flow.
	declineAll bool
}

// NewFakeBackgroundCheck returns a fresh in-memory reference adapter.
func NewFakeBackgroundCheck() *FakeBackgroundCheck {
	return &FakeBackgroundCheck{
		results: make(map[string]Result),
		calls:   make(map[string]int),
	}
}

// Execute performs the (mocked) external action exactly once per
// idempotencyKey. It is synchronous: it always returns a Resolved Dispatch (a
// terminal Result inline, never Pending). The first call for a key records the
// side-effect and a deterministic Result; any later call with the same key
// returns that Result and performs NO further side-effect. A Request whose
// Subject is BackgroundCheckDeclineSubject yields a terminal OutcomeFailed (a
// rejected check, err == nil — a definitive verdict, not a transient error);
// every other subject clears (OutcomeCompleted). No network, no real I/O.
func (f *FakeBackgroundCheck) Execute(_ context.Context, req Request) (Dispatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if res, seen := f.results[req.IdempotencyKey]; seen {
		return Dispatch{Disposition: Resolved, Result: res}, nil
	}
	f.calls[req.IdempotencyKey]++
	res := Result{Status: OutcomeCompleted, Detail: "background-check cleared for " + req.Subject}
	if f.declineAll || req.Subject == BackgroundCheckDeclineSubject {
		res = Result{Status: OutcomeFailed, Detail: "background-check declined for " + req.Subject}
	}
	f.results[req.IdempotencyKey] = res
	return Dispatch{Disposition: Resolved, Result: res}, nil
}

// SetDeclineAll arms (or disarms) blanket-decline mode: every subject yields a
// terminal OutcomeFailed instead of clearing. It is the demo affordance the bridge
// wires from BRIDGE_FAKE_DECLINE so an operator can drive the declined-application
// experience end-to-end (the per-subject BackgroundCheckDeclineSubject trigger is
// test-only — the live subject is the minted instanceKey handle, not applicant
// data). Set once at construction, before Start.
func (f *FakeBackgroundCheck) SetDeclineAll(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.declineAll = v
}

// Poll is unreachable for this synchronous adapter (Execute never returns
// Pending, so the bridge never holds a Ref to poll). It returns a clear error so
// a wiring mistake surfaces rather than silently resolving.
func (f *FakeBackgroundCheck) Poll(_ context.Context, ref string) (Dispatch, error) {
	return Dispatch{}, fmt.Errorf("bridge: FakeBackgroundCheck is synchronous: Poll unsupported (ref %q)", ref)
}

// SideEffects reports how many times the real external action was performed for
// idempotencyKey — 0 before the first Execute, and exactly 1 no matter how many
// repeat Executes follow (the idempotency proof tests assert this).
func (f *FakeBackgroundCheck) SideEffects(idempotencyKey string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[idempotencyKey]
}
