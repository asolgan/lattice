package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Compile-time check that NatsKVAdapter satisfies Adapter.
var _ Adapter = (*NatsKVAdapter)(nil)

// NatsKVAdapter writes materialized rows to a NATS KV bucket.
type NatsKVAdapter struct {
	kv         jetstream.KeyValue
	keyOrder   []string   // ordered key field names; used for deterministic composite key construction
	deleteMode DeleteMode // hard (default): kv.Delete; soft: tombstone Put
}

// New creates a NatsKVAdapter that writes to kv.
// keyOrder must match the rule's into.key field list and determines the order
// in which key values are concatenated to form the KV key
// (e.g. ["account_id","agreement_id"] → "acct-001.abc123").
// deleteMode selects hard (kv.Delete) vs soft (tombstone Put) delete projection;
// it is fixed for the life of the adapter.
func New(kv jetstream.KeyValue, keyOrder []string, deleteMode DeleteMode) (*NatsKVAdapter, error) {
	if len(keyOrder) == 0 {
		return nil, errors.New("natskv: keyOrder must not be empty")
	}
	return &NatsKVAdapter{kv: kv, keyOrder: keyOrder, deleteMode: deleteMode}, nil
}

// buildKey concatenates key field values in keyOrder order, joined with ".".
// Lattice key shape convention (Contract #1) uses "." as the segment
// separator throughout — vtx.<type>.<id>.<aspect>, lnk.<…>, cap.identity.<id>.
// Returns an error if any key field is absent from keys.
func (a *NatsKVAdapter) buildKey(keys map[string]any) (string, error) {
	parts := make([]string, len(a.keyOrder))
	for i, field := range a.keyOrder {
		val, ok := keys[field]
		if !ok {
			return "", fmt.Errorf("natskv: key field %q absent from keys map", field)
		}
		parts[i] = fmt.Sprintf("%v", val)
	}
	return strings.Join(parts, "."), nil
}

// Upsert serializes row to JSON and writes it to the KV bucket under the constructed key,
// creating or overwriting unconditionally (idempotent).
func (a *NatsKVAdapter) Upsert(ctx context.Context, keys map[string]any, row map[string]any) error {
	key, err := a.buildKey(keys)
	if err != nil {
		return fmt.Errorf("natskv upsert: %w", err)
	}
	data, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("natskv upsert: marshal row: %w", err)
	}
	if _, err := a.kv.Put(ctx, key, data); err != nil {
		return fmt.Errorf("natskv upsert: put %s: %w", key, err)
	}
	return nil
}

// Delete projects a Core KV deletion into the target KV bucket. The behavior is
// fixed at construction time by the adapter's deleteMode:
//
//   - DeleteModeHard (default): physically removes the key via kv.Delete. Lineage
//     already lives in Core KV, so the derived view reflects deletions as
//     removals. Deleting a never-existed key is idempotent — jetstream's
//     ErrKeyNotFound is swallowed and nil returned.
//   - DeleteModeSoft: writes a tombstone document {isDeleted:true, projectedAt:…}
//     for audit/forensic targets that opt in. Overwriting a never-existed key is
//     naturally idempotent (Put creates).
//
// Both absence (hard) and tombstone (soft) are treated as denial by the
// capability authorizer (step3_auth_capability): an absent key resolves to
// NoCapabilityEntry and an isDeleted doc to a denied entry. The freshness-ceiling
// comparison that originally motivated soft-delete on the capability plane was
// removed in Story 1.5.4, so absence and tombstone are now equivalent for auth.
func (a *NatsKVAdapter) Delete(ctx context.Context, keys map[string]any) error {
	key, err := a.buildKey(keys)
	if err != nil {
		return fmt.Errorf("natskv delete: %w", err)
	}
	if a.deleteMode == DeleteModeSoft {
		tombstone := map[string]any{
			"isDeleted":   true,
			"projectedAt": time.Now().UTC().Format(time.RFC3339),
		}
		data, err := json.Marshal(tombstone)
		if err != nil {
			return fmt.Errorf("natskv delete: marshal tombstone: %w", err)
		}
		if _, err := a.kv.Put(ctx, key, data); err != nil {
			return fmt.Errorf("natskv delete: put tombstone %s: %w", key, err)
		}
		return nil
	}
	// Hard delete: physically remove the key. Deleting an absent key is a no-op.
	if err := a.kv.Delete(ctx, key); err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil
		}
		return fmt.Errorf("natskv delete: delete %s: %w", key, err)
	}
	return nil
}

// Probe checks whether the NATS KV bucket is reachable by calling kv.Status.
// Returns nil if the bucket is accessible; returns an infrastructure or structural
// error that failure.Classify can route appropriately.
func (a *NatsKVAdapter) Probe(ctx context.Context) error {
	_, err := a.kv.Status(ctx)
	return err
}

// Close is a no-op; the NATS KV handle lifecycle is managed by the caller.
func (a *NatsKVAdapter) Close() error {
	return nil
}
