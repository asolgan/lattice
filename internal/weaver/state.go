package weaver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// createNudge CAS-creates a nudge mark, minting a fresh NanoID claimId and
// writing it into the mark's ClaimID field in the SAME KVCreateWithTTL that
// creates the key — the §10.3 invariant that a nudge mark ALWAYS carries a
// non-empty claimId, impossible-by-construction. It returns the minted claimId
// (the join key the weaver-claims record reuses as both its key and its
// idempotencyKey) alongside the create revision. exists=true means the create
// lost the dispatch-OCC race; the returned claimId is empty in that case (no
// new claim was minted). The non-nudge create stays claimId-free: only the
// nudge action mints one.
func (m *markStore) createNudge(ctx context.Context, targetID, entityID, gapColumn, entityKey, action string) (claimID string, revision uint64, exists bool, err error) {
	claimID, err = substrate.NewNanoID()
	if err != nil {
		return "", 0, false, fmt.Errorf("weaver: mint nudge claimId: %w", err)
	}
	now := time.Now()
	rec := mark{
		TargetID:       targetID,
		EntityKey:      entityKey,
		Gap:            gapColumn,
		Action:         action,
		ClaimID:        claimID,
		ClaimedAt:      substrate.FormatTimestamp(now),
		LeaseExpiresAt: substrate.FormatTimestamp(now.Add(m.lease)),
		HeldBy:         m.instance,
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return "", 0, false, fmt.Errorf("weaver: marshal nudge mark: %w", err)
	}
	rev, err := m.conn.KVCreateWithTTL(ctx, m.bucket, markKey(targetID, entityID, gapColumn), body,
		markTTLBackstopFactor*m.lease)
	if err != nil {
		if errors.Is(err, substrate.ErrRevisionConflict) {
			return "", 0, true, nil
		}
		return "", 0, false, err
	}
	return claimID, rev, false, nil
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
//
// This is the non-nudge reclaim: it writes no claimId. A nudge mark reclaim
// MUST carry forward the existing mark's claimId (never mint a new one) — see
// replaceCarryingClaim.
func (m *markStore) replace(ctx context.Context, targetID, entityID, gapColumn, entityKey, action string,
	expectedRevision uint64) (revision uint64, conflict bool, err error) {
	return m.replaceCarryingClaim(ctx, targetID, entityID, gapColumn, entityKey, action, "", expectedRevision)
}

// replaceCarryingClaim re-arms an expired mark in place, carrying forward the
// supplied claimId. For a nudge reclaim the caller passes the EXISTING mark's
// claimId so the re-armed mark keeps the same join key — recovery resumes the
// SAME claim (same idempotencyKey) rather than minting a fresh id (a fresh id
// would mean a second idempotencyKey → a duplicate external call, §10.3). A
// blank claimId reproduces the non-nudge reclaim (no claimId on the mark). All
// other re-arm semantics match replace.
func (m *markStore) replaceCarryingClaim(ctx context.Context, targetID, entityID, gapColumn, entityKey, action, claimID string,
	expectedRevision uint64) (revision uint64, conflict bool, err error) {

	now := time.Now()
	rec := mark{
		TargetID:       targetID,
		EntityKey:      entityKey,
		Gap:            gapColumn,
		Action:         action,
		ClaimID:        claimID,
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

// countInFlight reports how many in-flight marks exist in the bucket, scanned
// on the heartbeat cadence (never per-message). Reserved `<targetId>.__control`
// dispatch-skip markers are skipped — they are not §10.3 marks (same guard the
// reconciler sweep applies), so the marksInFlight gauge counts only real
// in-flight dispatch.
func (m *markStore) countInFlight(ctx context.Context) (int, error) {
	keys, err := m.conn.KVListKeys(ctx, m.bucket)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, key := range keys {
		if strings.HasSuffix(key, controlKeySuffix) {
			continue
		}
		n++
	}
	return n, nil
}

// controlKeySuffix names the reserved per-target dispatch-skip marker
// : `<targetId>.__control`. The marker is matched by suffix
// (seedDisabledTargets, the reconciler sweep), so the collision guard is the
// LAST key segment, not the entityId: a real mark's last segment is a
// `missing_*` gap column (validateTarget forces it), and "__control" does not
// start with "missing_". Combined with targetId being a single dot-free token,
// a 2-segment `<targetId>.__control` key can never equal a 3-segment
// `<targetId>.<entityId>.<gapColumn>` mark key.
const controlKeySuffix = ".__control"

// controlMark is the JSON body of the `<targetId>.__control` dispatch-skip
// marker.
type controlMark struct {
	Disabled   bool   `json:"disabled"`
	DisabledAt string `json:"disabledAt,omitempty"`
}

// controlKey builds the reserved per-target dispatch-skip marker key.
func controlKey(targetID string) string {
	return targetID + controlKeySuffix
}

// setDisabled writes or clears the `<targetId>.__control` dispatch-skip
// marker. disabled=true CAS-free-writes `{"disabled":true,
// "disabledAt":<now>}`; disabled=false deletes the key (missing-key-is-success,
// mirroring delete's missing-key posture — enable/resume on an already-enabled
// target is idempotent).
func (m *markStore) setDisabled(ctx context.Context, targetID string, disabled bool) error {
	if !disabled {
		err := m.conn.KVDelete(ctx, m.bucket, controlKey(targetID))
		if err != nil && !errors.Is(err, substrate.ErrKeyNotFound) {
			return err
		}
		return nil
	}
	body, err := json.Marshal(controlMark{Disabled: true, DisabledAt: substrate.FormatTimestamp(time.Now())})
	if err != nil {
		return fmt.Errorf("weaver: marshal control mark: %w", err)
	}
	if _, err := m.conn.KVPut(ctx, m.bucket, controlKey(targetID), body); err != nil {
		return err
	}
	return nil
}

// isDisabled reads the `<targetId>.__control` dispatch-skip marker. A
// missing key means active (not disabled) — never an error.
func (m *markStore) isDisabled(ctx context.Context, targetID string) (bool, error) {
	return m.isDisabledKey(ctx, controlKey(targetID))
}

// isDisabledKey reads the disabled flag from an already-known `__control` key
// (the key seedDisabledTargets already listed) — one KV read, no rebuild of a
// key it just parsed off the listing. A missing key means active (not
// disabled) — never an error.
func (m *markStore) isDisabledKey(ctx context.Context, key string) (bool, error) {
	entry, err := m.conn.KVGet(ctx, m.bucket, key)
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	var cm controlMark
	if err := json.Unmarshal(entry.Value, &cm); err != nil {
		return false, fmt.Errorf("weaver: unmarshal control mark %s: %w", entry.Key, err)
	}
	return cm.Disabled, nil
}

// deleteByTargetPrefix deletes every weaver-state key with prefix
// "<targetID>." — every `<targetId>.<entityId>.<gapColumn>` in-flight mark AND
// the `<targetId>.__control` dispatch-skip marker. The trailing
// "." in the prefix means "t1." never matches a key under "t10." — no
// accidental cross-target overlap from a shared numeric prefix. Tolerates
// ErrKeyNotFound mid-scan (mirrors the reconciler sweep's scan-tolerance
// posture: a key deleted between the list and the delete is not an error).
func (m *markStore) deleteByTargetPrefix(ctx context.Context, targetID string) (deleted int, err error) {
	keys, err := m.conn.KVListKeys(ctx, m.bucket)
	if err != nil {
		return 0, err
	}
	prefix := targetID + "."
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if delErr := m.conn.KVDelete(ctx, m.bucket, key); delErr != nil {
			if errors.Is(delErr, substrate.ErrKeyNotFound) {
				continue
			}
			return deleted, delErr
		}
		deleted++
	}
	return deleted, nil
}
