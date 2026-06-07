package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// Instance status values (Contract #10 §10.3).
const (
	StatusRunning  = "running"
	StatusComplete = "complete"
	StatusFailed   = "failed"
)

// loom-state key prefixes (Contract #10 §10.3). The four shapes share the one
// bucket under disjoint prefixes — the same one-bucket / disjoint-prefix pattern
// capability-kv §6.1 uses for cap.ephemeral.*.
const (
	instancePrefix = "instance."
	tokenPrefix    = "token."
	outboxPrefix   = "outbox."
	deadlinePrefix = "deadline."
)

func instanceKey(instanceID string) string { return instancePrefix + instanceID }
func tokenKey(token string) string         { return tokenPrefix + token }
func outboxKey(token string) string        { return outboxPrefix + token }
func deadlineKey(instanceID string) string { return deadlinePrefix + instanceID }

// Instance is the persisted per-instance cursor stored in loom-state under
// instance.<instanceId> (Contract #10 §10.3). It is the durable source of truth
// for a running pattern: the cursor (current step index), the pendingToken (the
// requestId of the step currently awaited), and status.
type Instance struct {
	InstanceID   string `json:"instanceId"`
	PatternRef   string `json:"patternRef"`
	SubjectKey   string `json:"subjectKey"`
	Cursor       int    `json:"cursor"`
	PendingToken string `json:"pendingToken"`
	Status       string `json:"status"`
	RetryCount   int    `json:"retryCount"`
}

// tokenPointer is the thin reverse index value stored under token.<pendingToken>
// (Contract #10 §10.3). Its presence is the correlation + idempotency guard.
type tokenPointer struct {
	InstanceID string `json:"instanceId"`
}

// outboxRecord is the command-outbox value stored under outbox.<token> (Contract
// #10 §10.3): the op Loom intends to submit, written in the SAME AtomicBatch as
// the cursor/token transition so submission is not a dual write. The relay
// fire-and-forget publishes it to ops.<lane> and deletes the record on
// publish-ack (re-publish idempotent via the chosen requestId + the Contract #4
// tracker).
type outboxRecord struct {
	RequestID string          `json:"requestId"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload"`
	Target    string          `json:"target,omitempty"`
	Lane      string          `json:"lane"`
	Actor     string          `json:"actor"`
}

// deadlineMark is the thin value stored under deadline.<instanceId> (Contract
// #10 §10.3). It carries a per-key TTL = the current step's deadline; its expiry
// (a KeyValuePurge/MaxAge marker) is the off-stream failed/rejected backstop
// (§10.6). The value is observability-only — the step-deadline-exceeded handler
// reconstructs everything from instance.<instanceId>.
type deadlineMark struct {
	SetAt string `json:"setAt"`
}

// stateStore reads and writes the two loom-state key shapes. loom-state is
// Loom's own operational bucket and the only place Loom writes directly (P2);
// every step transition is a single AtomicBatch on the one bucket so the cursor
// update and the reverse-pointer add/delete land all-or-nothing.
type stateStore struct {
	conn   *substrate.Conn
	bucket string
}

func newStateStore(conn *substrate.Conn, bucket string) *stateStore {
	return &stateStore{conn: conn, bucket: bucket}
}

// getInstance reads the instance record for instanceID. Returns (nil, nil) when
// the key is absent.
func (s *stateStore) getInstance(ctx context.Context, instanceID string) (*Instance, error) {
	entry, err := s.conn.KVGet(ctx, s.bucket, instanceKey(instanceID))
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("loom: read instance %q: %w", instanceID, err)
	}
	var inst Instance
	if err := json.Unmarshal(entry.Value, &inst); err != nil {
		return nil, fmt.Errorf("loom: unmarshal instance %q: %w", instanceID, err)
	}
	return &inst, nil
}

// resolveToken reads the token.<token> reverse pointer, returning the instanceId
// it points at. ok is false when the pointer is absent (already advanced, or not
// a token Loom is awaiting) — the pointer's presence is the correlation guard
// (Contract #10 §10.6).
func (s *stateStore) resolveToken(ctx context.Context, token string) (instanceID string, ok bool, err error) {
	entry, err := s.conn.KVGet(ctx, s.bucket, tokenKey(token))
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("loom: resolve token %q: %w", token, err)
	}
	var ptr tokenPointer
	if err := json.Unmarshal(entry.Value, &ptr); err != nil {
		return "", false, fmt.Errorf("loom: unmarshal token pointer %q: %w", token, err)
	}
	return ptr.InstanceID, true, nil
}

// createInstance writes the initial instance.<id> cursor as a create (the
// trigger consumer's idempotency hinges on this: a duplicate trigger for the
// same instanceId finds the key present and skips). No token is written yet —
// step 0's submission write-aheads its token via transition.
func (s *stateStore) createInstance(ctx context.Context, inst *Instance) error {
	body, err := json.Marshal(inst)
	if err != nil {
		return fmt.Errorf("loom: marshal instance %q: %w", inst.InstanceID, err)
	}
	if _, err := s.conn.KVCreate(ctx, s.bucket, instanceKey(inst.InstanceID), body); err != nil {
		return fmt.Errorf("loom: create instance %q: %w", inst.InstanceID, err)
	}
	return nil
}

// transition applies one transition as a single AtomicBatch on loom-state
// (Contract #10 §10.3): update instance.<id>; optionally write the new
// token.<newToken> reverse pointer; optionally delete the prior token.<oldToken>;
// optionally write the outbox.<outbox.RequestID> op record; and arm or disarm
// deadline.<instanceId>. All-or-nothing — so the op submission (the outbox
// record) is part of the same atomic fact as the cursor advance and is NOT a
// dual write (the command-outbox pattern, §10.3).
//
//   - newToken == "" writes no forward pointer (a terminal has no next step).
//   - oldToken == "" deletes no prior pointer (the initial step had none).
//   - outbox != nil writes the op-to-submit record (the relay publishes it).
//   - deadlineTTL > 0 arms (PUT, fresh TTL) deadline.<instanceId> (re-arm on
//     each step); deadlineTTL <= 0 deletes it (terminal).
//
// The write-ahead invariant (§10.6 invariant 1) holds by construction: the op
// record is persisted in this batch and the relay's publish is the only side
// effect, decoupled and idempotent.
func (s *stateStore) transition(ctx context.Context, inst *Instance, newToken, oldToken string, outbox *outboxRecord, deadlineTTL time.Duration) error {
	body, err := json.Marshal(inst)
	if err != nil {
		return fmt.Errorf("loom: marshal instance %q: %w", inst.InstanceID, err)
	}
	ops := []substrate.BatchOp{
		{Bucket: s.bucket, Key: instanceKey(inst.InstanceID), Value: body},
	}
	if newToken != "" {
		ptrBody, err := json.Marshal(tokenPointer{InstanceID: inst.InstanceID})
		if err != nil {
			return fmt.Errorf("loom: marshal token pointer: %w", err)
		}
		// CreateOnly is also the concurrency guard: two advancers racing the same
		// step (e.g. a live completion and a deadline-probe recovery) derive the
		// same deterministic newToken, so the loser's batch is rejected here —
		// only one advance can commit a given step. A genuine crash-retry never
		// hits this: the prior attempt's batch is all-or-nothing, so a re-GET sees
		// PendingToken already == newToken and routes to the drop branch, not a
		// re-submit.
		ops = append(ops, substrate.BatchOp{
			Bucket:     s.bucket,
			Key:        tokenKey(newToken),
			Value:      ptrBody,
			CreateOnly: true,
		})
	}
	if oldToken != "" && oldToken != newToken {
		ops = append(ops, substrate.BatchOp{
			Bucket: s.bucket,
			Key:    tokenKey(oldToken),
			Delete: true,
		})
	}
	if outbox != nil {
		obBody, err := json.Marshal(outbox)
		if err != nil {
			return fmt.Errorf("loom: marshal outbox record: %w", err)
		}
		ops = append(ops, substrate.BatchOp{
			Bucket: s.bucket,
			Key:    outboxKey(outbox.RequestID),
			Value:  obBody,
		})
	}
	if deadlineTTL > 0 {
		dlBody, err := json.Marshal(deadlineMark{SetAt: substrate.FormatTimestamp(time.Now())})
		if err != nil {
			return fmt.Errorf("loom: marshal deadline mark: %w", err)
		}
		// Re-arming the per-instance deadline by overwriting the same key relies on
		// loom-state being History:1 (the default): the new PUT evicts the prior
		// TTL'd message via the per-subject limit, so an earlier step's deadline
		// cannot fire after the cursor has advanced. Raising the bucket's history
		// would break that guarantee.
		ops = append(ops, substrate.BatchOp{
			Bucket: s.bucket,
			Key:    deadlineKey(inst.InstanceID),
			Value:  dlBody,
			TTL:    deadlineTTL,
		})
	} else {
		ops = append(ops, substrate.BatchOp{
			Bucket: s.bucket,
			Key:    deadlineKey(inst.InstanceID),
			Delete: true,
		})
	}
	if _, err := s.conn.AtomicBatch(ctx, ops); err != nil {
		return fmt.Errorf("loom: transition instance %q: %w", inst.InstanceID, err)
	}
	return nil
}

// outboxExists reports whether the command-outbox record for token is still
// present (i.e. the relay has not yet published + deleted it). Used by the
// step-deadline-exceeded probe to distinguish "not yet relayed" from "rejected"
// (§10.6).
func (s *stateStore) outboxExists(ctx context.Context, token string) (bool, error) {
	_, err := s.conn.KVGet(ctx, s.bucket, outboxKey(token))
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("loom: read outbox %q: %w", token, err)
	}
	return true, nil
}

// rearmDeadline re-arms deadline.<instanceId> with a fresh TTL outside a
// transition batch — used by the probe's "relay not yet delivered" branch to
// extend the deadline without advancing the cursor (§10.6).
func (s *stateStore) rearmDeadline(ctx context.Context, instanceID string, ttl time.Duration) error {
	body, err := json.Marshal(deadlineMark{SetAt: substrate.FormatTimestamp(time.Now())})
	if err != nil {
		return fmt.Errorf("loom: marshal deadline mark: %w", err)
	}
	if _, err := s.conn.KVPutWithTTL(ctx, s.bucket, deadlineKey(instanceID), body, ttl); err != nil {
		return fmt.Errorf("loom: rearm deadline %q: %w", instanceID, err)
	}
	return nil
}

// deleteToken removes a token.<token> reverse pointer (used when a redelivered
// completion resolves to an already-advanced instance and the stale pointer must
// be cleared). A missing pointer is not an error.
func (s *stateStore) deleteToken(ctx context.Context, token string) error {
	if err := s.conn.KVDelete(ctx, s.bucket, tokenKey(token)); err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return nil
		}
		return fmt.Errorf("loom: delete token %q: %w", token, err)
	}
	return nil
}
