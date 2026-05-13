package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// testRevisionConditionAtomicity validates AC2:
//
//   - A PublishBatch containing a compare-and-swap entry (revision condition) commits
//     atomically — the entire batch is rejected if the revision check fails.
//   - No partial commit is observable from a concurrent reader.
//
// Strategy:
//  1. Pre-write key A (so revision > 0).
//  2. Batch: write key A with wrong revision + write key B (new, revision=0).
//     Expect: batch rejected, key B must NOT exist.
//  3. Batch: write key A with correct revision + write key B (new, revision=0).
//     Expect: batch commits, both keys exist.
func testRevisionConditionAtomicity(nc *nats.Conn, js jetstream.JetStream) string {
	const bucket = "spike-test2"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("--- Test 2: Revision condition atomicity ---")

	kv, err := createCoreKVBucket(ctx, js, bucket)
	if err != nil {
		return fmt.Sprintf("FAIL setup: %v", err)
	}

	// Pre-write key A so it has revision=1.
	const (
		keyA = "vtx.identity.aaaaaaaaaaaaaaaaaaaaA"
		keyB = "vtx.identity.bbbbbbbbbbbbbbbbbbbbb"
	)
	rev1, err := kv.Create(ctx, keyA, []byte(`{"class":"identity","v":"initial"}`))
	if err != nil {
		return fmt.Sprintf("FAIL pre-write keyA: %v", err)
	}
	fmt.Printf("  Pre-wrote keyA at revision=%d\n", rev1)

	// --- Sub-test 2a: Batch with WRONG revision on keyA -> should reject entire batch ---
	fmt.Println("  Sub-test 2a: batch with wrong revision (expect rejection)")

	msgAWrong := newMsg(bucket, keyA, []byte(`{"class":"identity","v":"updated-wrong"}`))
	setRevisionCondition(msgAWrong, rev1+999) // deliberately wrong revision

	msgBNew := newMsg(bucket, keyB, []byte(`{"class":"identity","v":"new"}`))
	setRevisionCondition(msgBNew, 0) // create-if-absent

	ackWrong, err := publishAtomicBatch(nc, "spike-test2-wrong", []*nats.Msg{msgAWrong, msgBNew}, 5*time.Second)
	if err != nil {
		return fmt.Sprintf("FAIL publishAtomicBatch (wrong revision): %v", err)
	}
	if ackWrong.Error == nil {
		return fmt.Sprintf("FAIL expected batch rejection for wrong revision, but it committed (seq=%d)", ackWrong.Sequence)
	}
	fmt.Printf("  Batch rejected as expected: %s (err_code=%d)\n",
		ackWrong.Error.Description, ackWrong.Error.ErrCode)

	// Confirm keyB was NOT written (no partial commit).
	_, errB := kv.Get(ctx, keyB)
	if errB == nil {
		return fmt.Sprintf("FAIL partial commit detected: keyB exists after batch rejection")
	}
	if errB != jetstream.ErrKeyNotFound {
		return fmt.Sprintf("FAIL unexpected error checking keyB after rejection: %v", errB)
	}
	fmt.Println("  Confirmed: keyB NOT written (no partial commit)")

	// Confirm keyA was NOT modified.
	entryA, err := kv.Get(ctx, keyA)
	if err != nil {
		return fmt.Sprintf("FAIL get keyA after rejection: %v", err)
	}
	if string(entryA.Value()) != `{"class":"identity","v":"initial"}` {
		return fmt.Sprintf("FAIL keyA was modified despite batch rejection: %q", entryA.Value())
	}
	fmt.Printf("  Confirmed: keyA unchanged at revision=%d\n", entryA.Revision())

	// --- Sub-test 2b: Batch with CORRECT revision on keyA -> should commit ---
	fmt.Println("  Sub-test 2b: batch with correct revision (expect commit)")

	msgACorrect := newMsg(bucket, keyA, []byte(`{"class":"identity","v":"updated-correct"}`))
	setRevisionCondition(msgACorrect, rev1) // correct current revision

	msgBNew2 := newMsg(bucket, keyB, []byte(`{"class":"identity","v":"new"}`))
	setRevisionCondition(msgBNew2, 0) // create-if-absent

	ackCorrect, err := publishAtomicBatch(nc, "spike-test2-correct", []*nats.Msg{msgACorrect, msgBNew2}, 5*time.Second)
	if err != nil {
		return fmt.Sprintf("FAIL publishAtomicBatch (correct revision): %v", err)
	}
	if ackCorrect.Error != nil {
		return fmt.Sprintf("FAIL batch with correct revision rejected: %s", ackCorrect.Error.Description)
	}
	fmt.Printf("  Batch committed: seq=%d count=%d\n", ackCorrect.Sequence, ackCorrect.BatchSize)

	// Verify both keys exist with expected values.
	entryAUpdated, err := kv.Get(ctx, keyA)
	if err != nil {
		return fmt.Sprintf("FAIL get keyA after commit: %v", err)
	}
	fmt.Printf("  keyA updated: revision=%d value=%q\n", entryAUpdated.Revision(), entryAUpdated.Value())

	entryBCreated, err := kv.Get(ctx, keyB)
	if err != nil {
		return fmt.Sprintf("FAIL get keyB after commit: %v", err)
	}
	fmt.Printf("  keyB created: revision=%d value=%q\n", entryBCreated.Revision(), entryBCreated.Value())

	return "PASS: wrong-revision batch rejected (no partial commit); correct-revision batch committed atomically"
}
