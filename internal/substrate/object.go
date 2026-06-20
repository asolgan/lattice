package substrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Object-store sentinels, parallel to the KV sentinels in errors.go.
//
// ErrObjectNotFound is returned by ObjectGet / ObjectGetInfo / when the named
// object does not exist in the store. ErrObjectTooLarge is returned by
// ObjectPut when the streamed payload exceeds the caller-supplied byte cap; the
// partial object is removed before returning so a rejected Put leaves no bytes.
var (
	ErrObjectNotFound = errors.New("substrate: object not found")
	ErrObjectTooLarge = errors.New("substrate: object exceeds size cap")
)

// ObjectInfo echoes the durable properties object-store clients need (the
// off-graph blob plane's analog of KVEntry). It carries no jetstream type on
// its surface so callers never import jetstream.
//
//   - Digest is the NATS-computed SHA-256 integrity digest in the exact
//     "SHA-256=<base64url>" form NATS stores and verifies on read.
//   - Name is the store object name the caller chose before streaming (a
//     provisional NanoID in the object plane — content-addressing lives at the
//     graph layer, not the store key).
//   - Size / Chunks describe the streamed payload.
type ObjectInfo struct {
	Bucket  string
	Name    string
	Digest  string
	Size    uint64
	Chunks  uint32
	ModTime time.Time // last modification time — the grace basis for the GC reconcile
}

func objectInfoFrom(bucket string, in *jetstream.ObjectInfo) ObjectInfo {
	return ObjectInfo{
		Bucket:  bucket,
		Name:    in.Name,
		Digest:  in.Digest,
		Size:    in.Size,
		Chunks:  in.Chunks,
		ModTime: in.ModTime,
	}
}

// objectStore returns a cached jetstream.ObjectStore handle for bucket. The
// store must already exist (provisioned via the bootstrap path); the handle is
// opened, not created — mirroring Conn.bucket for KV. A missing store maps to
// ErrBucketNotFound so callers classify it as a structural fault without
// importing jetstream.
func (c *Conn) objectStore(ctx context.Context, bucket string) (jetstream.ObjectStore, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if os, ok := c.objectStores[bucket]; ok {
		return os, nil
	}
	os, err := c.js.ObjectStore(ctx, bucket)
	if err != nil {
		if errors.Is(err, jetstream.ErrBucketNotFound) || errors.Is(err, jetstream.ErrStreamNotFound) {
			return nil, fmt.Errorf("%w: object store=%s", ErrBucketNotFound, bucket)
		}
		return nil, fmt.Errorf("substrate: open object store %q: %w", bucket, err)
	}
	c.objectStores[bucket] = os
	return os, nil
}

// cappedReader wraps an io.Reader and trips ErrObjectTooLarge once more than
// `remaining` bytes have been read. It is the size guard ObjectPut owns so the
// cap is enforced at the stream layer (not only at a caller's reader) and a
// rejected stream aborts mid-flight instead of buffering the whole payload.
type cappedReader struct {
	r         io.Reader
	remaining int64
	tripped   bool
}

func (cr *cappedReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.remaining -= int64(n)
	if cr.remaining < 0 {
		cr.tripped = true
		return n, ErrObjectTooLarge
	}
	return n, err
}

// ObjectPut streams r into bucket under name (NATS chunks at its default size,
// constant memory regardless of object size) and returns the NATS-computed
// digest + size. name is caller-chosen and must be unique within the store.
//
// maxBytes caps the payload: a stream exceeding it is aborted and the partial
// object removed, returning ErrObjectTooLarge — the size guard lives here, at
// the stream owner, so it is not bypassable by a non-Loupe writer (CC9). A
// maxBytes <= 0 disables the cap (unlimited); trusted callers pass their
// configured limit.
func (c *Conn) ObjectPut(ctx context.Context, bucket, name string, r io.Reader, maxBytes int64) (ObjectInfo, error) {
	store, err := c.objectStore(ctx, bucket)
	if err != nil {
		return ObjectInfo{}, err
	}
	reader := r
	var cr *cappedReader
	if maxBytes > 0 {
		cr = &cappedReader{r: r, remaining: maxBytes}
		reader = cr
	}
	info, err := store.Put(ctx, jetstream.ObjectMeta{Name: name}, reader)
	if err != nil {
		// A rejected or oversized Put must leave no bytes behind — the GC
		// reclaim plane (v1b) handles successful-but-orphaned uploads, not
		// half-written ones. Delete is best-effort idempotent cleanup.
		_ = store.Delete(ctx, name)
		if cr != nil && cr.tripped {
			return ObjectInfo{}, fmt.Errorf("%w: bucket=%s name=%s cap=%d", ErrObjectTooLarge, bucket, name, maxBytes)
		}
		return ObjectInfo{}, fmt.Errorf("substrate: object put %s/%s: %w", bucket, name, err)
	}
	return objectInfoFrom(bucket, info), nil
}

// ObjectGet streams the bytes back. The returned ReadCloser is the chunk stream
// (NATS verifies the digest as it streams, so a corrupt blob surfaces as a read
// error); the caller must Close it. ObjectInfo carries the durable metadata.
// Returns ErrObjectNotFound if the object is absent.
func (c *Conn) ObjectGet(ctx context.Context, bucket, name string) (io.ReadCloser, ObjectInfo, error) {
	store, err := c.objectStore(ctx, bucket)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	res, err := store.Get(ctx, name)
	if err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			return nil, ObjectInfo{}, fmt.Errorf("%w: bucket=%s name=%s", ErrObjectNotFound, bucket, name)
		}
		return nil, ObjectInfo{}, fmt.Errorf("substrate: object get %s/%s: %w", bucket, name, err)
	}
	info, err := res.Info()
	if err != nil {
		_ = res.Close()
		return nil, ObjectInfo{}, fmt.Errorf("substrate: object get info %s/%s: %w", bucket, name, err)
	}
	return res, objectInfoFrom(bucket, info), nil
}

// ObjectGetInfo reads an object's metadata without streaming its bytes — the
// existence + digest probe. Returns ErrObjectNotFound if absent.
func (c *Conn) ObjectGetInfo(ctx context.Context, bucket, name string) (ObjectInfo, error) {
	store, err := c.objectStore(ctx, bucket)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := store.GetInfo(ctx, name)
	if err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			return ObjectInfo{}, fmt.Errorf("%w: bucket=%s name=%s", ErrObjectNotFound, bucket, name)
		}
		return ObjectInfo{}, fmt.Errorf("substrate: object get info %s/%s: %w", bucket, name, err)
	}
	return objectInfoFrom(bucket, info), nil
}

// ObjectDelete removes the object. Deleting an absent object is a no-op (nil) so
// the GC byte-reclaim path (v1b) need not race a concurrent delete — idempotent
// like KVPurge.
func (c *Conn) ObjectDelete(ctx context.Context, bucket, name string) error {
	store, err := c.objectStore(ctx, bucket)
	if err != nil {
		return err
	}
	if err := store.Delete(ctx, name); err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			return nil
		}
		return fmt.Errorf("substrate: object delete %s/%s: %w", bucket, name, err)
	}
	return nil
}

// ObjectList enumerates the live objects in bucket (excluding deleted ones).
// It is the GC reconcile's never-attached backstop primitive: list the store,
// then for each object check whether a live vertex still references its
// storeName. An empty store returns an empty slice, not an error.
func (c *Conn) ObjectList(ctx context.Context, bucket string) ([]ObjectInfo, error) {
	store, err := c.objectStore(ctx, bucket)
	if err != nil {
		return nil, err
	}
	infos, err := store.List(ctx)
	if err != nil {
		// An empty store surfaces as ErrNoObjectsFound in nats.go; normalize to
		// an empty result so callers don't special-case it.
		if errors.Is(err, jetstream.ErrNoObjectsFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("substrate: object list %s: %w", bucket, err)
	}
	out := make([]ObjectInfo, 0, len(infos))
	for _, in := range infos {
		out = append(out, objectInfoFrom(bucket, in))
	}
	return out, nil
}

// ObjectStoreExists probes whether the named object store is reachable,
// returning nil when it is and ErrBucketNotFound when it is gone — the
// object-store analog of KVStatus, used by the kernel-verify surfaces to assert
// the core-objects store was provisioned.
func (c *Conn) ObjectStoreExists(ctx context.Context, bucket string) error {
	if _, err := c.objectStore(ctx, bucket); err != nil {
		return err
	}
	return nil
}
