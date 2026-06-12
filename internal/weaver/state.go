package weaver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// gapColumnPrefix is the §10.2 gap-column naming convention: every gap column
// (and therefore every §10.8 gaps key and every mark key segment) is a
// missing_<gap> snake_case bool.
const gapColumnPrefix = "missing_"

// markTTLBackstopFactor sizes the mark's NATS per-key TTL relative to its
// lease: TTL = markTTLBackstopFactor × lease. The TTL must be STRICTLY longer
// than the lease — the reconciler sweep is the prompt reclaim and the TTL is
// only the backstop, and the sweep can re-attempt a gap only while the key
// still exists past leaseExpiresAt. Nothing watches weaver-state, so a raw TTL
// deletion unwedges the gap but cannot re-attempt it; a TTL equal to the lease
// would make the sweep's re-attempt leg unreachable. A constant, not a config
// knob.
const markTTLBackstopFactor = 2

// mark is the weaver-state anti-storm in-flight record (Contract #10 §10.3),
// keyed <targetId>.<entityId>.<gapColumn>. The CAS-create of this key is the
// dispatch OCC: concurrent evaluations of the same gap race the create, the
// loser drops, the winner dispatches. LeaseExpiresAt mirrors the lease the
// per-key TTL backstops (§10.3 visibility); HeldBy is the writing engine
// instance. ClaimID is declared (omitempty) per the frozen §10.3 value shape
// but stays empty until Epic 10's nudge path mints it atomically with the
// CAS-create.
type mark struct {
	TargetID       string `json:"targetId"`
	EntityKey      string `json:"entityKey"`
	Gap            string `json:"gap"`
	Action         string `json:"action"`
	ClaimID        string `json:"claimId,omitempty"`
	ClaimedAt      string `json:"claimedAt"`
	LeaseExpiresAt string `json:"leaseExpiresAt,omitempty"`
	HeldBy         string `json:"heldBy,omitempty"`
}

// markStore is the weaver-state accessor for in-flight marks. The in-flight
// check is always a KV read — never an in-memory map: durable dispatch state
// lives in the bucket so any replica resolves it. lease sizes each mark's
// leaseExpiresAt (and, scaled by markTTLBackstopFactor, its per-key TTL);
// instance is the heldBy holder tag.
type markStore struct {
	conn     *substrate.Conn
	bucket   string
	lease    time.Duration
	instance string
}

func newMarkStore(conn *substrate.Conn, bucket string, lease time.Duration, instance string) *markStore {
	return &markStore{conn: conn, bucket: bucket, lease: lease, instance: instance}
}

// markKey builds the §10.3 mark key. Entity is keyed by NanoID, never the
// dotted vertex key (the full key rides the mark's entityKey field —
// document-is-truth).
func markKey(targetID, entityID, gapColumn string) string {
	return targetID + "." + entityID + "." + gapColumn
}

// create CAS-creates the mark (KV create-on-absent — the dispatch OCC) and
// returns its create revision, the per-dispatch-episode tag the deterministic
// requestId derives from. exists=true means the create lost the race: another
// dispatch of this gap is in flight. The mark carries the §10.3 lease
// (leaseExpiresAt = now + lease, heldBy = this instance) and a NATS per-key
// TTL of markTTLBackstopFactor × lease — the backstop that bounds the mark's
// life even if no reconciler ever sweeps it.
func (m *markStore) create(ctx context.Context, targetID, entityID, gapColumn, entityKey, action string) (revision uint64, exists bool, err error) {
	now := time.Now()
	rec := mark{
		TargetID:       targetID,
		EntityKey:      entityKey,
		Gap:            gapColumn,
		Action:         action,
		ClaimedAt:      substrate.FormatTimestamp(now),
		LeaseExpiresAt: substrate.FormatTimestamp(now.Add(m.lease)),
		HeldBy:         m.instance,
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return 0, false, fmt.Errorf("weaver: marshal mark: %w", err)
	}
	rev, err := m.conn.KVCreateWithTTL(ctx, m.bucket, markKey(targetID, entityID, gapColumn), body,
		markTTLBackstopFactor*m.lease)
	if err != nil {
		if errors.Is(err, substrate.ErrRevisionConflict) {
			return 0, true, nil
		}
		return 0, false, err
	}
	return rev, false, nil
}

// get reads the mark for one gap, returning its current revision. Lane-1 only
// ever CAS-creates and deletes marks, and the sweep's reclaim replaces the
// whole value under a revision condition — so the current revision always
// identifies the episode currently holding the gap (the episode tag).
// found=false means no dispatch is in flight.
func (m *markStore) get(ctx context.Context, targetID, entityID, gapColumn string) (rec *mark, revision uint64, found bool, err error) {
	entry, err := m.conn.KVGet(ctx, m.bucket, markKey(targetID, entityID, gapColumn))
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	var rc mark
	if err := json.Unmarshal(entry.Value, &rc); err != nil {
		return nil, 0, false, fmt.Errorf("weaver: unmarshal mark %s: %w", entry.Key, err)
	}
	return &rc, entry.Revision, true, nil
}

// replace re-arms an expired mark in place — the reconciler's reclaim claim.
// The write is revision-conditioned on expectedRevision (the revision the
// sweep read this pass) and produces a fresh §10.3 value (new claimedAt and
// leaseExpiresAt, heldBy = this instance) with a re-armed per-key TTL, so the
// key is never absent across a reclaim: a crash at any point leaves either
// the old expired mark (re-swept next pass) or the fresh mark (its lease
// bounds the retry). The returned revision is the fresh dispatch-episode tag.
// conflict=true means the mark changed since the read (a fresh episode
// CAS-created it, or its TTL marker landed) — the caller must skip.
func (m *markStore) replace(ctx context.Context, targetID, entityID, gapColumn, entityKey, action string,
	expectedRevision uint64) (revision uint64, conflict bool, err error) {

	now := time.Now()
	rec := mark{
		TargetID:       targetID,
		EntityKey:      entityKey,
		Gap:            gapColumn,
		Action:         action,
		ClaimedAt:      substrate.FormatTimestamp(now),
		LeaseExpiresAt: substrate.FormatTimestamp(now.Add(m.lease)),
		HeldBy:         m.instance,
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return 0, false, fmt.Errorf("weaver: marshal mark: %w", err)
	}
	rev, err := m.conn.KVUpdateWithTTL(ctx, m.bucket, markKey(targetID, entityID, gapColumn), body,
		expectedRevision, markTTLBackstopFactor*m.lease)
	if err != nil {
		if errors.Is(err, substrate.ErrRevisionConflict) {
			return 0, true, nil
		}
		return 0, false, err
	}
	return rev, false, nil
}

// delete clears one gap's mark (gap closed — level-reconciled clearing). A
// missing key is success: the level reconcile deletes by candidate column, not
// by observed presence.
func (m *markStore) delete(ctx context.Context, targetID, entityID, gapColumn string) error {
	err := m.conn.KVDelete(ctx, m.bucket, markKey(targetID, entityID, gapColumn))
	if err != nil && !errors.Is(err, substrate.ErrKeyNotFound) {
		return err
	}
	return nil
}

// countInFlight reports how many marks exist in the bucket, scanned on the
// heartbeat cadence (never per-message).
func (m *markStore) countInFlight(ctx context.Context) (int, error) {
	keys, err := m.conn.KVListKeys(ctx, m.bucket)
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}
