package nudge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// ClaimState is the lifecycle state of a weaver-claims record (Contract #10
// §10.3): claimed → executing → resolved | failed.
type ClaimState string

const (
	// StateClaimed is the visible intent written BEFORE any external call
	// (NFR-S11): the claim record exists at weaver-claims.<claimId> before the
	// adapter runs.
	StateClaimed ClaimState = "claimed"
	// StateExecuting is set just before the adapter Execute call.
	StateExecuting ClaimState = "executing"
	// StateResolved is the terminal success state: the resolve op published and
	// the record carries resolvedAt + resolveRef.
	StateResolved ClaimState = "resolved"
	// StateFailed is the terminal failure state: the adapter hard-failed.
	StateFailed ClaimState = "failed"
)

// Claim is the frozen §10.3 weaver-claims record. The key is <claimId>; the
// JSON field names are the §10.3 value shape exactly. IdempotencyKey == ClaimID
// is an invariant: both are set from the one minted NanoID, never two inputs.
type Claim struct {
	ClaimID        string            `json:"claimId"`
	Adapter        string            `json:"adapter"`
	Operation      string            `json:"operation"`
	Subject        string            `json:"subject"`
	Params         map[string]string `json:"params,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey"`
	State          ClaimState        `json:"state"`
	ClaimedAt      string            `json:"claimedAt"`
	ResolvedAt     string            `json:"resolvedAt,omitempty"`
	ResolveRef     string            `json:"resolveRef,omitempty"`
}

// terminal reports whether the claim has reached a state no transition leaves.
func (c ClaimState) terminal() bool {
	return c == StateResolved || c == StateFailed
}

// ClaimStore is the weaver-claims accessor (Contract #10 §10.3). Each claim is
// keyed by its claimId and written with a per-key TTL = retention (the bucket is
// primordial and TTL-capable; the simplest contract-faithful 90d-retention
// mechanism, mirroring markStore's TTL discipline). The lifecycle transitions
// (Advance/Resolve/reopen) are plain read-modify-write with no CAS.
//
// Concurrency: within one Weaver instance the reconciler sweep and the lane-1
// redelivery handler are independent goroutines that can BOTH drive recovery for
// the same claimId at once, so these read-modify-writes are not strictly
// single-writer. That is accepted (no CAS in Phase 2): duplicate-safety does NOT
// rest on a single writer. It rests on (a) the adapter de-duping two Execute calls
// on the SAME idempotencyKey (=claimId) to at most one external side-effect, and
// (b) the resolve op collapsing on its deterministic Contract #4 requestId (derived
// from the claimId) to exactly one Core KV mutation. A concurrent transition can
// only flip the claim record's state field between executing/failed/resolved; that
// transient is self-healing on the next recovery (recovery short-circuits on the
// terminal resolved state and otherwise re-drives idempotently), and it never
// causes a second side-effect. The initial claim Write is still create-semantic
// (create-on-absent) so a redelivery routed to a FRESH dispatch cannot clobber a
// live claim — it lands ErrClaimExists and is bounced to Recover instead.
type ClaimStore struct {
	conn      *substrate.Conn
	bucket    string
	retention time.Duration
}

// NewClaimStore constructs a ClaimStore over the weaver-claims bucket. retention
// is the per-key TTL applied to every claim record (default 90d, Config-tunable
// — see internal/weaver engine Config); a retention <= 0 writes without expiry,
// mirroring the substrate KVPutWithTTL fallback.
func NewClaimStore(conn *substrate.Conn, bucket string, retention time.Duration) *ClaimStore {
	return &ClaimStore{conn: conn, bucket: bucket, retention: retention}
}

// ErrClaimExists reports that a claim record already exists at the claimId — the
// create-semantic Write lost the race with a live claim. The caller must route an
// existing claim to Recover (read-before-act), never re-run a fresh dispatch over
// it: re-running would reset a resolved/executing/failed claim to claimed and
// re-call the adapter (a duplicate side-effect, FR58).
var ErrClaimExists = errors.New("nudge: claim already exists for claimId")

// Write records a new claim at state=claimed, with create semantics: it fails
// with ErrClaimExists if a claim already exists at claimId, so a fresh dispatch
// can never clobber a live claim. This is the NFR-S11 "visible claim state
// before executing any external call": it MUST run before the adapter. claimId
// is the NanoID minted atomically with the weaver-state mark (§10.3);
// idempotencyKey is set equal to it. The create-on-absent CAS mirrors the
// weaver-state mark create (markStore.createNudge): a redelivery that minted no
// fresh id lands on an existing key and is bounced to Recover, which reuses the
// same id rather than re-claiming.
func (s *ClaimStore) Write(ctx context.Context, claimID, adapter, operation, subject string, params map[string]string) (*Claim, error) {
	if claimID == "" {
		return nil, fmt.Errorf("nudge: claimId is required to write a claim")
	}
	rec := &Claim{
		ClaimID:        claimID,
		Adapter:        adapter,
		Operation:      operation,
		Subject:        subject,
		Params:         params,
		IdempotencyKey: claimID,
		State:          StateClaimed,
		ClaimedAt:      substrate.FormatTimestamp(time.Now()),
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("nudge: marshal claim %s: %w", claimID, err)
	}
	if _, err := s.conn.KVCreateWithTTL(ctx, s.bucket, claimID, body, s.retention); err != nil {
		if errors.Is(err, substrate.ErrRevisionConflict) {
			return nil, fmt.Errorf("%w: %s", ErrClaimExists, claimID)
		}
		return nil, fmt.Errorf("nudge: write claim %s: %w", claimID, err)
	}
	return rec, nil
}

// Get reads a claim by claimId. found=false (nil, false, nil) means no claim
// record exists for that id.
func (s *ClaimStore) Get(ctx context.Context, claimID string) (rec *Claim, found bool, err error) {
	entry, err := s.conn.KVGet(ctx, s.bucket, claimID)
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var c Claim
	if err := json.Unmarshal(entry.Value, &c); err != nil {
		return nil, false, fmt.Errorf("nudge: unmarshal claim %s: %w", claimID, err)
	}
	return &c, true, nil
}

// Advance transitions a claim to next, preserving the immutable identity fields.
// Transitions are idempotent: re-advancing to a state already reached (or
// advancing a terminal claim to its own terminal state) is a no-op that returns
// the current record. A transition out of a DIFFERENT terminal state is
// rejected — a resolved claim never becomes failed and vice versa.
func (s *ClaimStore) Advance(ctx context.Context, claimID string, next ClaimState) (*Claim, error) {
	rec, found, err := s.Get(ctx, claimID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("nudge: advance claim %s to %s: claim not found", claimID, next)
	}
	if rec.State == next {
		return rec, nil
	}
	if rec.State.terminal() {
		return nil, fmt.Errorf("nudge: claim %s is %s (terminal); cannot advance to %s", claimID, rec.State, next)
	}
	rec.State = next
	if err := s.put(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// reopen moves a claim into state=executing for a recovery re-attempt. Unlike
// Advance it accepts a failed claim (failed → executing): §10.3 keeps the gap
// converging, so a prior failed attempt is re-driven on the SAME idempotencyKey
// (the adapter dedups). A resolved claim is rejected — recovery short-circuits on
// resolved before ever calling reopen, so reaching here for a resolved claim is a
// bug, surfaced. Re-entering executing from executing is an idempotent no-op.
func (s *ClaimStore) reopen(ctx context.Context, claimID string) (*Claim, error) {
	rec, found, err := s.Get(ctx, claimID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("nudge: reopen claim %s: claim not found", claimID)
	}
	if rec.State == StateResolved {
		return nil, fmt.Errorf("nudge: claim %s is resolved (terminal); cannot reopen", claimID)
	}
	if rec.State == StateExecuting {
		return rec, nil
	}
	rec.State = StateExecuting
	if err := s.put(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Resolve advances a claim to state=resolved, recording resolvedAt (now) and
// resolveRef (the resolve op's requestId — the Core KV audit-join key per
// §10.3). Idempotent: a claim already resolved with the same resolveRef is a
// no-op. A claim already resolved with a DIFFERENT resolveRef is rejected —
// re-resolving the same claim against a different op is a bug, surfaced.
//
// A failed claim CAN resolve: recovery re-drives a failed claim on the same
// idempotencyKey, and a successful re-attempt (or a probe that finds the resolve
// already landed in authoritative Core KV) settles it to resolved. failed is a
// recoverable state, not a dead end (§10.3 — the gap keeps converging).
func (s *ClaimStore) Resolve(ctx context.Context, claimID, resolveRef string) (*Claim, error) {
	rec, found, err := s.Get(ctx, claimID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("nudge: resolve claim %s: claim not found", claimID)
	}
	if rec.State == StateResolved {
		if rec.ResolveRef != resolveRef {
			return nil, fmt.Errorf("nudge: claim %s already resolved with resolveRef %q; refusing to overwrite with %q",
				claimID, rec.ResolveRef, resolveRef)
		}
		return rec, nil
	}
	rec.State = StateResolved
	rec.ResolvedAt = substrate.FormatTimestamp(time.Now())
	rec.ResolveRef = resolveRef
	if err := s.put(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// put serializes and writes rec at key=claimId with the retention TTL.
func (s *ClaimStore) put(ctx context.Context, rec *Claim) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("nudge: marshal claim %s: %w", rec.ClaimID, err)
	}
	if _, err := s.conn.KVPutWithTTL(ctx, s.bucket, rec.ClaimID, body, s.retention); err != nil {
		return fmt.Errorf("nudge: write claim %s: %w", rec.ClaimID, err)
	}
	return nil
}
