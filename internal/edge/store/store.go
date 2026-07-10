// Package store is the Edge node's Local VAL Store
// (edge-lattice-full-design.md §3.1): an embedded, transactional local KV
// (bbolt — pure-Go, single-file, no cgo) that mirrors Core KV's partitioned,
// keyed shape. Entries are addressed by the exact Contract #1 key strings
// (vtx.<type>.<id>, vtx.<type>.<id>.<localName>, lnk.<typeA>.<idA>.<rel>.
// <typeB>.<idB>) and carry the projected VAL fragment plus the cloud
// revision that produced it — the reconcile-by-revision cursor the Sync
// Manager (§3.2) applies against.
//
// The store also scaffolds the "local:" namespace for sovereign,
// device-only aspects (drafts, private notes) the Sync Manager never
// uploads (§3.1) — kept in a separate bbolt bucket so the mirror's apply
// path can never reach it.
package store

import (
	"encoding/json"
	"fmt"

	"go.etcd.io/bbolt"

	"github.com/asolgan/lattice/internal/substrate"
)

const (
	bucketVAL   = "val"   // Contract #1 keyed entries mirrored from the cloud.
	bucketLocal = "local" // sovereign, device-only entries — never uploaded.
	bucketMeta  = "meta"  // Sync Manager cursor + node-local bookkeeping.

	cursorKey = "cursor"
)

// Entry is one Local VAL Store record: the projected fragment last applied
// for a Contract #1 key, plus the cloud revision that produced it.
type Entry struct {
	Key      string          `json:"key"`
	Revision uint64          `json:"revision"`
	Data     json.RawMessage `json:"data,omitempty"`
	Deleted  bool            `json:"deleted"`
}

// Store is the Edge node's embedded local VAL mirror.
type Store struct {
	db *bbolt.DB
}

// Open opens (creating if absent) the bbolt-backed local VAL store at path.
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("edge/store: open %q: %w", path, err)
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, name := range []string{bucketVAL, bucketLocal, bucketMeta} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("edge/store: create bucket %q: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying bbolt database.
func (s *Store) Close() error {
	return s.db.Close()
}

// ApplyUpsert applies an inbound "upsert" delta (edge-lattice-full-design.md
// §3.2) under last-writer-wins-by-revision: the write lands iff revision is
// greater than or equal to the currently-stored revision for key (a
// stale/duplicate/reordered delta — JetStream delivers at-least-once and can
// reorder — is dropped). Returns applied=false for a dropped delta, with no
// error. key must be a valid Contract #1 vertex/aspect/link key.
func (s *Store) ApplyUpsert(key string, revision uint64, data json.RawMessage) (applied bool, err error) {
	if substrate.ClassifyKey(key) == substrate.KindUnknown {
		return false, fmt.Errorf("edge/store: ApplyUpsert: %q is not a Contract #1 key", key)
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketVAL))
		cur, ok, err := getEntry(b, key)
		if err != nil {
			return err
		}
		if ok && revision < cur.Revision {
			return nil // stale/duplicate — drop, not applied.
		}
		applied = true
		return putEntry(b, Entry{Key: key, Revision: revision, Data: data})
	})
	return applied, err
}

// ApplyDelete applies an inbound "delete" delta: tombstones the local key
// under the same last-writer-wins-by-revision gate as ApplyUpsert. Returns
// applied=false for a dropped (stale/duplicate) delete, with no error.
func (s *Store) ApplyDelete(key string, revision uint64) (applied bool, err error) {
	if substrate.ClassifyKey(key) == substrate.KindUnknown {
		return false, fmt.Errorf("edge/store: ApplyDelete: %q is not a Contract #1 key", key)
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketVAL))
		cur, ok, err := getEntry(b, key)
		if err != nil {
			return err
		}
		if ok && revision < cur.Revision {
			return nil // stale/duplicate — drop, not applied.
		}
		applied = true
		return putEntry(b, Entry{Key: key, Revision: revision, Deleted: true})
	})
	return applied, err
}

// Get returns the currently-stored entry for key, or ok=false if the store
// holds nothing for it (never hydrated, or evicted by local GC).
func (s *Store) Get(key string) (entry Entry, ok bool, err error) {
	err = s.db.View(func(tx *bbolt.Tx) error {
		entry, ok, err = getEntry(tx.Bucket([]byte(bucketVAL)), key)
		return err
	})
	return entry, ok, err
}

// PutLocal writes a sovereign, device-only entry under the given name (the
// "local:" namespace, §3.1) — never applied by ApplyUpsert/ApplyDelete and
// never read back by anything that would upload it. name is caller-chosen
// (not a Contract #1 key); no revision is tracked, since nothing reconciles
// this namespace against the cloud.
func (s *Store) PutLocal(name string, data json.RawMessage) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(bucketLocal)).Put([]byte(name), data)
	})
}

// GetLocal reads back a sovereign local-only entry, or ok=false if absent.
func (s *Store) GetLocal(name string) (data json.RawMessage, ok bool, err error) {
	err = s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(bucketLocal)).Get([]byte(name))
		if v == nil {
			return nil
		}
		ok = true
		data = append(json.RawMessage(nil), v...)
		return nil
	})
	return data, ok, err
}

// Cursor returns the Sync Manager's last-applied stream sequence, or
// ok=false on a fresh store (no cursor persisted yet — the node should
// hydrate, §3.3).
func (s *Store) Cursor() (seq uint64, ok bool, err error) {
	err = s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(bucketMeta)).Get([]byte(cursorKey))
		if v == nil {
			return nil
		}
		if uErr := json.Unmarshal(v, &seq); uErr != nil {
			return fmt.Errorf("edge/store: Cursor: %w", uErr)
		}
		ok = true
		return nil
	})
	return seq, ok, err
}

// SetCursor persists the Sync Manager's last-applied stream sequence, so a
// brief disconnect can resume the durable consumer from it (§3.2).
func (s *Store) SetCursor(seq uint64) error {
	v, err := json.Marshal(seq)
	if err != nil {
		return fmt.Errorf("edge/store: SetCursor: %w", err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(bucketMeta)).Put([]byte(cursorKey), v)
	})
}

func getEntry(b *bbolt.Bucket, key string) (entry Entry, ok bool, err error) {
	v := b.Get([]byte(key))
	if v == nil {
		return Entry{}, false, nil
	}
	if err := json.Unmarshal(v, &entry); err != nil {
		return Entry{}, false, fmt.Errorf("edge/store: decode entry %q: %w", key, err)
	}
	return entry, true, nil
}

func putEntry(b *bbolt.Bucket, entry Entry) error {
	v, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("edge/store: encode entry %q: %w", entry.Key, err)
	}
	return b.Put([]byte(entry.Key), v)
}
