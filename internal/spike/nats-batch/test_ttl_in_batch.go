package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// testTTLInBatch validates AC1:
//
//   - A KV put with a per-key TTL issued inside a PublishBatch commits successfully.
//   - The TTL entry expires independently of other (non-TTL) batch entries.
//   - Mixed TTL and non-TTL entries in the same batch both commit correctly.
func testTTLInBatch(nc *nats.Conn, js jetstream.JetStream) string {
	const bucket = "spike-test1"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("--- Test 1: TTL-in-batch ---")

	kv, err := createCoreKVBucket(ctx, js, bucket)
	if err != nil {
		return fmt.Sprintf("FAIL setup: %v", err)
	}

	// Build an atomic batch with 3 messages:
	//   msg1: vtx.identity.<id1>           — no TTL (durable vertex)
	//   msg2: vtx.identity.<id1>.email     — no TTL (durable aspect)
	//   msg3: vtx.op.<requestId>           — TTL=3s (idempotency tracker, per Contract #4)
	//
	// All three use revision=0 (create-if-absent), matching Processor step 8 "create" ops.

	const (
		vertexKey  = "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"
		aspectKey  = "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y.email"
		trackerKey = "vtx.op.Rm7q3pntwzkfbcxv5p9j"
		ttl        = 3 * time.Second
	)

	msgVertex := newMsg(bucket, vertexKey, []byte(`{"class":"identity","isDeleted":false}`))
	setRevisionCondition(msgVertex, 0)

	msgAspect := newMsg(bucket, aspectKey, []byte(`{"class":"email","isDeleted":false,"data":{"value":"a@b.com"}}`))
	setRevisionCondition(msgAspect, 0)

	msgTracker := newMsg(bucket, trackerKey, []byte(`{"class":"op","isDeleted":false,"data":{"status":"committed"}}`))
	setRevisionCondition(msgTracker, 0)
	setTTL(msgTracker, ttl)

	batchID := "spike-test1-batch1"
	ack, err := publishAtomicBatch(nc, batchID, []*nats.Msg{msgVertex, msgAspect, msgTracker}, 5*time.Second)
	if err != nil {
		return fmt.Sprintf("FAIL publishAtomicBatch: %v", err)
	}
	if ack.Error != nil {
		return fmt.Sprintf("FAIL batch rejected: %s (err_code=%d)", ack.Error.Description, ack.Error.ErrCode)
	}
	fmt.Printf("  Batch committed: stream=%s seq=%d batch_id=%q count=%d\n",
		ack.Stream, ack.Sequence, ack.BatchID, ack.BatchSize)

	if ack.BatchSize != 3 {
		return fmt.Sprintf("FAIL expected batch size=3, got %d", ack.BatchSize)
	}

	// Verify all three keys exist immediately after commit.
	for _, k := range []string{vertexKey, aspectKey, trackerKey} {
		entry, err := kv.Get(ctx, k)
		if err != nil {
			return fmt.Sprintf("FAIL get %q after commit: %v", k, err)
		}
		fmt.Printf("  Key %q revision=%d op=%s\n", k, entry.Revision(), entry.Operation())
	}

	// Now verify that the tracker key (with TTL=3s) expires independently
	// while the non-TTL keys remain. Wait for TTL + buffer.
	fmt.Printf("  Waiting %v for tracker TTL to expire...\n", ttl+time.Second)
	time.Sleep(ttl + time.Second)

	// Tracker should be gone.
	_, err = kv.Get(ctx, trackerKey)
	if err == nil {
		return fmt.Sprintf("FAIL tracker key %q should have expired but still exists", trackerKey)
	}
	if err != jetstream.ErrKeyNotFound {
		return fmt.Sprintf("FAIL unexpected error checking expired tracker: %v", err)
	}
	fmt.Printf("  Tracker %q expired as expected (ErrKeyNotFound)\n", trackerKey)

	// Non-TTL keys should still exist.
	for _, k := range []string{vertexKey, aspectKey} {
		entry, err := kv.Get(ctx, k)
		if err != nil {
			return fmt.Sprintf("FAIL non-TTL key %q should still exist after tracker expiry: %v", k, err)
		}
		fmt.Printf("  Non-TTL key %q still present at revision=%d\n", k, entry.Revision())
	}

	return "PASS: TTL entry expired independently; non-TTL entries durable; batch committed with 3/3 entries"
}
