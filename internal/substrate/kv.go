package substrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// KVEntry is the typed result of a KV read. Echoes the durable
// properties substrate clients need without exposing the underlying
// jetstream.KeyValueEntry interface.
type KVEntry struct {
	Bucket    string
	Key       string
	Value     []byte
	Revision  uint64
	Timestamp time.Time
}

// KVGet reads the named key from bucket. Returns ErrKeyNotFound if the key
// does not exist (wrapped, so callers should use errors.Is).
func (c *Conn) KVGet(ctx context.Context, bucket, key string) (*KVEntry, error) {
	kv, err := c.bucket(ctx, bucket)
	if err != nil {
		return nil, err
	}
	entry, err := kv.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, fmt.Errorf("%w: bucket=%s key=%s", ErrKeyNotFound, bucket, key)
		}
		return nil, fmt.Errorf("substrate: KV get %s/%s: %w", bucket, key, err)
	}
	return &KVEntry{
		Bucket:    bucket,
		Key:       entry.Key(),
		Value:     entry.Value(),
		Revision:  entry.Revision(),
		Timestamp: entry.Created(),
	}, nil
}

// KVPut unconditionally writes value to key. Returns the new revision.
//
// Use KVCreate when "must not already exist" is required and KVUpdate when
// a revision-condition is required. KVPut is the right choice only for
// "I don't care about pre-state" scenarios (rare in Lattice — Processor
// always uses create-or-conditional-update inside an atomic batch).
func (c *Conn) KVPut(ctx context.Context, bucket, key string, value []byte) (uint64, error) {
	kv, err := c.bucket(ctx, bucket)
	if err != nil {
		return 0, err
	}
	rev, err := kv.Put(ctx, key, value)
	if err != nil {
		return 0, fmt.Errorf("substrate: KV put %s/%s: %w", bucket, key, err)
	}
	return rev, nil
}

// KVCreate writes value to key only if the key does not already exist.
// Returns ErrRevisionConflict if the key exists.
func (c *Conn) KVCreate(ctx context.Context, bucket, key string, value []byte) (uint64, error) {
	kv, err := c.bucket(ctx, bucket)
	if err != nil {
		return 0, err
	}
	rev, err := kv.Create(ctx, key, value)
	if err != nil {
		if isRevisionConflict(err) {
			return 0, fmt.Errorf("%w: bucket=%s key=%s (create requires absent): %v",
				ErrRevisionConflict, bucket, key, err)
		}
		return 0, fmt.Errorf("substrate: KV create %s/%s: %w", bucket, key, err)
	}
	return rev, nil
}

// KVUpdate writes value to key only if the current revision matches
// expectedRevision. Returns ErrRevisionConflict if revisions disagree, or
// ErrKeyNotFound if the key was purged out from under the caller.
func (c *Conn) KVUpdate(ctx context.Context, bucket, key string, value []byte, expectedRevision uint64) (uint64, error) {
	kv, err := c.bucket(ctx, bucket)
	if err != nil {
		return 0, err
	}
	rev, err := kv.Update(ctx, key, value, expectedRevision)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return 0, fmt.Errorf("%w: bucket=%s key=%s", ErrKeyNotFound, bucket, key)
		}
		if isRevisionConflict(err) {
			return 0, fmt.Errorf("%w: bucket=%s key=%s expected=%d: %v",
				ErrRevisionConflict, bucket, key, expectedRevision, err)
		}
		return 0, fmt.Errorf("substrate: KV update %s/%s: %w", bucket, key, err)
	}
	return rev, nil
}

// KVDelete soft-deletes key (writes a delete marker). Subsequent reads
// return ErrKeyNotFound.
func (c *Conn) KVDelete(ctx context.Context, bucket, key string) error {
	kv, err := c.bucket(ctx, bucket)
	if err != nil {
		return err
	}
	if err := kv.Delete(ctx, key); err != nil {
		return fmt.Errorf("substrate: KV delete %s/%s: %w", bucket, key, err)
	}
	return nil
}

// isRevisionConflict matches the NATS revision-condition rejection both
// from explicit error sentinels (when nats.go exposes them) and from raw
// API error strings (the underlying mechanism is the "wrong last
// sequence" reply, err_code=10071).
func isRevisionConflict(err error) bool {
	if err == nil {
		return false
	}
	// nats.go does export jetstream.ErrKeyExists for kv.Create conflicts.
	if errors.Is(err, jetstream.ErrKeyExists) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "wrong last sequence") ||
		strings.Contains(s, "key exists") ||
		strings.Contains(s, "10071")
}
