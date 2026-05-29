package substrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go/jetstream"
)

// startEmbeddedNATS runs a JetStream-enabled NATS server in-process and
// returns the connection URL. Cleanup is registered via t.Cleanup.
func startEmbeddedNATS(t *testing.T) (url string) {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	s := natsserver.RunServer(&opts)
	t.Cleanup(func() {
		if jsCfg := s.JetStreamConfig(); jsCfg != nil {
			defer os.RemoveAll(jsCfg.StoreDir)
		}
		s.Shutdown()
		_ = server.VERSION // silence unused
	})
	return s.ClientURL()
}

// provisionCoreBucket mirrors the bootstrap's Core KV provisioning:
// LimitMarkerTTL (=> AllowMsgTTL) and AllowAtomicPublish on the underlying
// stream.
func provisionCoreBucket(ctx context.Context, t *testing.T, c *Conn, bucket string) {
	t.Helper()
	js := c.JetStream()
	_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:         bucket,
		LimitMarkerTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("create KV bucket %q: %v", bucket, err)
	}
	streamName := "KV_" + bucket
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		t.Fatalf("get stream %q: %v", streamName, err)
	}
	cfg := stream.CachedInfo().Config
	cfg.AllowAtomicPublish = true
	if _, err := js.UpdateStream(ctx, cfg); err != nil {
		t.Fatalf("enable AllowAtomicPublish: %v", err)
	}
}

func newTestConn(t *testing.T) (*Conn, context.Context) {
	t.Helper()
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c, err := Connect(ctx, ConnectOpts{URL: url, Name: "substrate-test"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Close)
	return c, ctx
}

func TestKV_PutGetCreateUpdateDelete(t *testing.T) {
	c, ctx := newTestConn(t)
	bucket := "core-kv"
	provisionCoreBucket(ctx, t, c, bucket)

	key := VertexKey("identity", testNanoID1)
	val := []byte(`{"hello":"world"}`)

	// Get missing → ErrKeyNotFound.
	if _, err := c.KVGet(ctx, bucket, key); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}

	// Create.
	rev1, err := c.KVCreate(ctx, bucket, key, val)
	if err != nil {
		t.Fatalf("KVCreate: %v", err)
	}
	if rev1 == 0 {
		t.Fatalf("Create returned revision 0")
	}

	// Create same key again → ErrRevisionConflict.
	if _, err := c.KVCreate(ctx, bucket, key, val); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict on duplicate Create, got %v", err)
	}

	// Get back the entry.
	entry, err := c.KVGet(ctx, bucket, key)
	if err != nil {
		t.Fatalf("KVGet: %v", err)
	}
	if entry.Key != key || string(entry.Value) != string(val) || entry.Revision != rev1 {
		t.Fatalf("KVGet mismatch: %+v", entry)
	}

	// Update with wrong revision.
	if _, err := c.KVUpdate(ctx, bucket, key, []byte(`{"v":2}`), rev1+9); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict on wrong-rev Update, got %v", err)
	}

	// Update with correct revision.
	rev2, err := c.KVUpdate(ctx, bucket, key, []byte(`{"v":2}`), rev1)
	if err != nil {
		t.Fatalf("KVUpdate ok-path: %v", err)
	}
	if rev2 <= rev1 {
		t.Fatalf("revision did not advance: %d -> %d", rev1, rev2)
	}

	// Plain Put.
	rev3, err := c.KVPut(ctx, bucket, key, []byte(`{"v":3}`))
	if err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if rev3 <= rev2 {
		t.Fatalf("Put revision did not advance: %d -> %d", rev2, rev3)
	}

	// Delete -> subsequent get returns ErrKeyNotFound.
	if err := c.KVDelete(ctx, bucket, key); err != nil {
		t.Fatalf("KVDelete: %v", err)
	}
	if _, err := c.KVGet(ctx, bucket, key); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound after Delete, got %v", err)
	}
}

func TestAtomicBatch_Commits(t *testing.T) {
	c, ctx := newTestConn(t)
	bucket := "core-kv"
	provisionCoreBucket(ctx, t, c, bucket)

	keyVtx := VertexKey("identity", testNanoID1)
	keyAsp := AspectKey(keyVtx, "email")
	keyOp := VertexKey("op", testNanoID3)

	ops := []BatchOp{
		{Bucket: bucket, Key: keyVtx, Value: []byte(`{"class":"identity"}`), CreateOnly: true},
		{Bucket: bucket, Key: keyAsp, Value: []byte(`{"class":"email"}`), CreateOnly: true},
		{Bucket: bucket, Key: keyOp, Value: []byte(`{"class":"op"}`), CreateOnly: true, TTL: 3 * time.Second},
	}
	ack, err := c.AtomicBatch(ctx, ops)
	if err != nil {
		t.Fatalf("AtomicBatch: %v", err)
	}
	if ack.Count != 3 {
		t.Fatalf("ack.Count = %d, want 3", ack.Count)
	}

	// All three present, and the derived per-key revision must match the
	// revision the KV API reports on read-back. This proves the
	// contiguous-sequence + revision==stream-sequence premise behind
	// BatchAck.Revisions on live NATS.
	if ack.Revisions == nil {
		t.Fatalf("ack.Revisions is nil; expected derived per-key revisions")
	}
	for _, k := range []string{keyVtx, keyAsp, keyOp} {
		entry, err := c.KVGet(ctx, bucket, k)
		if err != nil {
			t.Fatalf("post-batch KVGet %q: %v", k, err)
		}
		got, ok := ack.Revisions[k]
		if !ok {
			t.Fatalf("ack.Revisions missing key %q", k)
		}
		if entry.Revision != got {
			t.Fatalf("revision mismatch for %q: KV API=%d ack.Revisions=%d", k, entry.Revision, got)
		}
	}
}

func TestAtomicBatch_AllOrNothing(t *testing.T) {
	c, ctx := newTestConn(t)
	bucket := "core-kv"
	provisionCoreBucket(ctx, t, c, bucket)

	keyA := VertexKey("identity", testNanoID1)
	keyB := VertexKey("identity", testNanoID2)

	// Seed keyA so its revision is known.
	revA, err := c.KVCreate(ctx, bucket, keyA, []byte(`{"v":"initial"}`))
	if err != nil {
		t.Fatalf("seed keyA: %v", err)
	}

	// Submit a batch that updates keyA with a deliberately wrong revision
	// (revA+9) and creates keyB. Whole batch must be rejected.
	ops := []BatchOp{
		{Bucket: bucket, Key: keyA, Value: []byte(`{"v":"updated"}`), HasRevision: true, Revision: revA + 9},
		{Bucket: bucket, Key: keyB, Value: []byte(`{"v":"new"}`), CreateOnly: true},
	}
	_, err = c.AtomicBatch(ctx, ops)
	if err == nil {
		t.Fatalf("expected AtomicBatch rejection")
	}
	if !errors.Is(err, ErrAtomicBatchRejected) {
		t.Fatalf("expected ErrAtomicBatchRejected, got %v", err)
	}

	// keyB must NOT exist (no partial commit).
	if _, err := c.KVGet(ctx, bucket, keyB); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("partial commit detected — keyB present after rejected batch: %v", err)
	}
	// keyA must still be at original revision.
	entry, err := c.KVGet(ctx, bucket, keyA)
	if err != nil {
		t.Fatalf("post-reject KVGet keyA: %v", err)
	}
	if entry.Revision != revA {
		t.Fatalf("keyA revision changed despite rejection: %d -> %d", revA, entry.Revision)
	}
}

func TestAtomicBatch_RejectsCrossBucket(t *testing.T) {
	c, ctx := newTestConn(t)
	ops := []BatchOp{
		{Bucket: "a", Key: "k1", Value: []byte(`x`)},
		{Bucket: "b", Key: "k2", Value: []byte(`y`)},
	}
	_, err := c.AtomicBatch(ctx, ops)
	if err == nil || !contains(err.Error(), "cross-bucket") {
		t.Fatalf("expected cross-bucket error, got %v", err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// fmt.Println sentinel — keeps unused import gone if fmt is dropped.
var _ = fmt.Println
