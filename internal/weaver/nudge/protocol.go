package nudge

import (
	"context"
	"errors"
	"fmt"

	"github.com/asolgan/lattice/internal/bridge"
)

// ErrAdapterNotFound reports that a nudge dispatch named an adapter the registry
// does not know — a config error, not an in-flight failure. Run and Recover wrap
// it on a registry miss so the caller can classify the posture (Ack + a config
// Health issue): redelivery can never fix a name the registry does not hold, so
// Nak'ing would hot-loop lane-1 against a config error.
var ErrAdapterNotFound = errors.New("nudge: no adapter registered for the dispatch adapter (config error)")

// ResolveFunc submits the resolve op through the Processor and returns the
// op's requestId (the resolveRef recorded on the claim — the Core KV audit-join
// key, §10.3). The caller (the engine) supplies this so the protocol package
// never holds an Actuator: it stays substrate-only, and the live actuator.submit
// wiring is supplied at dispatch time. The callback receives the claimId so the
// resolve op's payload can carry it as the reference field (arch Item 3).
type ResolveFunc func(ctx context.Context, claimID string, result bridge.Result) (resolveRef string, err error)

// ResolveProbe reports whether a resolve op for claimId has ALREADY landed in
// Core KV, and if so its resolveRef. It is the read half of read-before-act
// recovery: before re-executing a stuck claim the protocol asks Core KV (the
// authoritative business outcome) whether the resolve already committed.
// Recover requires a non-nil probe — the landed-resolve check must never be
// skipped, or a crash between execute and record would re-submit a duplicate
// resolve op.
type ResolveProbe func(ctx context.Context, claimID string) (resolveRef string, landed bool, err error)

// Nudger runs the Two-Phase Nudge protocol over a claim store and an adapter
// registry. It owns the claim record + the adapter call + the resolve, but not
// the Actuator (the resolve goes out through a ResolveFunc) and not the mark
// (the claimId is minted with the weaver-state mark and passed in) — the seam
// the engine wires in Story 10.2.
type Nudger struct {
	claims   *ClaimStore
	registry *bridge.Registry
}

// NewNudger constructs a Nudger over the claim store and adapter registry.
func NewNudger(claims *ClaimStore, registry *bridge.Registry) *Nudger {
	return &Nudger{claims: claims, registry: registry}
}

// Dispatch is one resolved nudge ready to run: the adapter name (§10.8
// GapAction.Adapter), the resolve operation type, the subject the nudge
// concerns, the resolved params, and the claimId minted atomically with the
// weaver-state mark CAS-create (§10.3). IdempotencyKey is always the claimId.
type Dispatch struct {
	ClaimID   string
	Adapter   string
	Operation string
	Subject   string
	Params    map[string]string
}

// request builds the adapter Request for this dispatch, keyed on claimId.
func (d Dispatch) request() bridge.Request {
	return bridge.Request{
		IdempotencyKey: d.ClaimID,
		Operation:      d.Operation,
		Subject:        d.Subject,
		Params:         d.Params,
	}
}

// Run executes the Two-Phase Nudge protocol for a FRESH dispatch:
//
//  1. Claim  — create weaver-claims.<claimId> with state=claimed BEFORE the call
//     (NFR-S11 visible-claim-before-execute); the write is create-semantic, so a
//     claimId that already has a claim record is rejected with ErrClaimExists;
//  2. Execute — advance to state=executing and call the adapter (panic-contained)
//     with idempotencyKey=claimId (the adapter is the external de-dup boundary);
//  3. Resolve — on success submit the resolve op via resolve (carrying the
//     claimId) and advance to state=resolved recording resolveRef.
//
// Run NEVER clobbers a live claim: a redelivery/retry that reaches a claimId with
// an existing claim returns ErrClaimExists, and the caller MUST route it to
// Recover (which reuses the same id read-before-act) rather than re-claim it.
//
// A missing adapter is a config error (surfaced, never silent). An adapter
// hard-failure (returned error OR a contained panic) advances the claim to
// state=failed and is returned. A failure to submit the resolve leaves the claim
// in state=executing — recovery (read-before-act) re-drives it on the same
// claimId without a second external side-effect.
func (n *Nudger) Run(ctx context.Context, d Dispatch, resolve ResolveFunc) (*Claim, error) {
	adapter, ok := n.registry.Lookup(d.Adapter)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAdapterNotFound, d.Adapter)
	}

	rec, err := n.claims.Write(ctx, d.ClaimID, d.Adapter, d.Operation, d.Subject, d.Params)
	if err != nil {
		return nil, err
	}

	if _, err := n.claims.Advance(ctx, d.ClaimID, StateExecuting); err != nil {
		return rec, err
	}

	result, execErr := execute(ctx, adapter, d.request())
	if execErr != nil {
		failed, advErr := n.claims.Advance(ctx, d.ClaimID, StateFailed)
		if advErr != nil {
			return rec, fmt.Errorf("nudge: adapter %q failed (%v) and marking claim %s failed also failed: %w",
				d.Adapter, execErr, d.ClaimID, advErr)
		}
		return failed, fmt.Errorf("nudge: adapter %q execute failed for claim %s: %w", d.Adapter, d.ClaimID, execErr)
	}

	return n.resolve(ctx, d.ClaimID, result, resolve)
}

// Recover is the read-before-act recovery entry point for a claim found in
// claimed/executing/failed past its lease. It (a) reuses the SAME claimId/
// idempotencyKey — never mints a new one — and (b) checks for an already-landed
// resolve before re-executing. If the resolve already committed (Core KV is
// authoritative), the record is just advanced to resolved (no re-execute, no
// second external side-effect). Otherwise it re-executes on the SAME
// idempotencyKey (the adapter de-dups any partial side-effect) and re-resolves.
//
// Recovery short-circuits ONLY on state=resolved — the single terminal that
// needs no work. A failed claim is NOT terminal for recovery: the reclaimed mark
// carries the same claimId forward, and §10.3 ("re-fire after lease expiry is
// safe — same claimId → adapter dedups") requires the gap to keep converging, so
// a failed (or executing) claim re-attempts on the same idempotencyKey rather
// than wedging the gap forever. The adapter dedups any side-effect from the prior
// attempt, so a re-attempt is at-most-one side-effect.
//
// probe is MANDATORY (a nil probe is rejected, like a blank claimId): the
// already-landed-resolve check is the guard against a duplicate resolve op on a
// crash between execute and record, and a caller must not be able to silently
// skip it. A blank claimId is rejected too: recovery must NEVER mint a fresh
// claimId for a corrupt mark (the caller alerts on that, per §10.3 — the
// corrupt-claim guard lives at the mark layer, in internal/weaver).
func (n *Nudger) Recover(ctx context.Context, d Dispatch, probe ResolveProbe, resolve ResolveFunc) (*Claim, error) {
	if d.ClaimID == "" {
		return nil, fmt.Errorf("nudge: recover called with empty claimId — refusing (a fresh id would mean a second idempotencyKey)")
	}
	if probe == nil {
		return nil, fmt.Errorf("nudge: recover called with a nil resolve probe — refusing (the landed-resolve check must not be skipped)")
	}

	rec, found, err := n.claims.Get(ctx, d.ClaimID)
	if err != nil {
		return nil, err
	}
	if found && rec.State == StateResolved {
		return rec, nil
	}

	// Read-before-act: ask Core KV (authoritative) whether the resolve already
	// landed. If so, advance the operational record to match — never re-execute.
	resolveRef, landed, err := probe(ctx, d.ClaimID)
	if err != nil {
		return rec, err
	}
	if landed {
		return n.claims.Resolve(ctx, d.ClaimID, resolveRef)
	}

	// No landed resolve: re-drive on the SAME claimId. The claim may be absent
	// (a crash before the claim write), stuck pre-resolved, or failed by a prior
	// attempt; either way the reused idempotencyKey makes the adapter call safe.
	adapter, ok := n.registry.Lookup(d.Adapter)
	if !ok {
		return rec, fmt.Errorf("%w: %q", ErrAdapterNotFound, d.Adapter)
	}
	if !found {
		if _, err := n.claims.Write(ctx, d.ClaimID, d.Adapter, d.Operation, d.Subject, d.Params); err != nil {
			return nil, err
		}
	}
	if _, err := n.claims.reopen(ctx, d.ClaimID); err != nil {
		return rec, err
	}
	result, execErr := execute(ctx, adapter, d.request())
	if execErr != nil {
		failed, advErr := n.claims.Advance(ctx, d.ClaimID, StateFailed)
		if advErr != nil {
			return rec, fmt.Errorf("nudge: recovery adapter %q failed (%v) and marking claim %s failed also failed: %w",
				d.Adapter, execErr, d.ClaimID, advErr)
		}
		return failed, fmt.Errorf("nudge: recovery adapter %q execute failed for claim %s: %w", d.Adapter, d.ClaimID, execErr)
	}
	return n.resolve(ctx, d.ClaimID, result, resolve)
}

// execute calls the adapter under panic containment. The framework is the safety
// boundary, not the adapter: a panic inside Execute is recovered and returned as
// an ordinary error, so the claim lands in state=failed (re-drivable on the same
// idempotencyKey) instead of crashing the dispatch goroutine and stranding the
// claim in state=executing.
func execute(ctx context.Context, adapter bridge.Adapter, req bridge.Request) (result bridge.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = bridge.Result{}
			err = fmt.Errorf("nudge: adapter panicked during execute: %v", r)
		}
	}()
	return adapter.Execute(ctx, req)
}

// resolve submits the resolve op and advances the claim to resolved. Shared by
// Run and Recover so both record resolveRef identically.
func (n *Nudger) resolve(ctx context.Context, claimID string, result bridge.Result, resolve ResolveFunc) (*Claim, error) {
	resolveRef, err := resolve(ctx, claimID, result)
	if err != nil {
		// The claim stays in state=executing; recovery re-drives it (the adapter
		// de-dups the reused idempotencyKey, so no second side-effect).
		return nil, fmt.Errorf("nudge: submit resolve op for claim %s: %w", claimID, err)
	}
	return n.claims.Resolve(ctx, claimID, resolveRef)
}
