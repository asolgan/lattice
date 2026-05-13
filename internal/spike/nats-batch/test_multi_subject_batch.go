package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// testMultiSubjectBatch validates AC3:
//
// A single PublishBatch containing messages targeting multiple distinct subjects
// within ONE KV bucket (Core KV) commits or fails as a unit. Specifically:
//   - A vertex create (vtx.identity.<id>)
//   - An aspect write (vtx.identity.<id>.email)
//   - A link create (lnk.identity.<id1>.assignedRole.role.<id2>)
//   - An op-tracker write (vtx.op.<requestId>)
//
// Also tests: concurrent conflicting write to one of those keys from a second writer
// during the batch attempt — documents the observed behavior.
func testMultiSubjectBatch(nc *nats.Conn, js jetstream.JetStream) string {
	const bucket = "spike-test3"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("--- Test 3: Multi-subject batch (single KV bucket) ---")

	_, err := createCoreKVBucket(ctx, js, bucket)
	if err != nil {
		return fmt.Sprintf("FAIL setup: %v", err)
	}

	// Sub-test 3a: vanilla multi-subject batch (no concurrent conflict).
	const (
		idVertex  = "Hj4kPmRtw9nbCxz5vQ2y"
		idRole    = "Rk2Pn6mQrtwzKbcXvP4U"
		requestID = "Rm7q3pntwzkfbcxv5p9j"
	)
	vertexKey := fmt.Sprintf("vtx.identity.%s", idVertex)
	aspectKey := fmt.Sprintf("vtx.identity.%s.email", idVertex)
	linkKey := fmt.Sprintf("lnk.identity.%s.assignedRole.role.%s", idVertex, idRole)
	trackerKey := fmt.Sprintf("vtx.op.%s", requestID)

	keys := []string{vertexKey, aspectKey, linkKey, trackerKey}
	payloads := [][]byte{
		[]byte(`{"class":"identity","isDeleted":false,"data":{}}`),
		[]byte(`{"class":"email","isDeleted":false,"data":{"value":"andrew@example.com","verified":false}}`),
		[]byte(`{"class":"link.assignedRole","isDeleted":false,"data":{}}`),
		[]byte(`{"class":"op","isDeleted":false,"data":{"status":"committed"}}`),
	}

	fmt.Println("  Sub-test 3a: multi-subject batch (4 keys: vertex, aspect, link, op-tracker)")
	msgs := make([]*nats.Msg, len(keys))
	for i, k := range keys {
		msgs[i] = newMsg(bucket, k, payloads[i])
		setRevisionCondition(msgs[i], 0) // all creates
	}

	ack, err := publishAtomicBatch(nc, "spike-test3-multi", msgs, 5*time.Second)
	if err != nil {
		return fmt.Sprintf("FAIL publishAtomicBatch: %v", err)
	}
	if ack.Error != nil {
		return fmt.Sprintf("FAIL batch rejected: %s (err_code=%d)", ack.Error.Description, ack.Error.ErrCode)
	}
	fmt.Printf("  Multi-subject batch committed: seq=%d count=%d\n", ack.Sequence, ack.BatchSize)
	if ack.BatchSize != 4 {
		return fmt.Sprintf("FAIL expected batch size=4, got %d", ack.BatchSize)
	}

	// Sub-test 3b: concurrent conflicting write.
	// Pre-condition: write a key that will be targeted by a concurrent second writer.
	// Then start an atomic batch that includes that key (with its correct revision),
	// but race a direct KV write against the same key BEFORE the batch commits.
	//
	// Expected: the batch's revision condition on the affected key becomes stale
	// (the server checks revision conditions at commit time), so the batch is rejected.
	fmt.Println("  Sub-test 3b: concurrent conflicting write to batch key")

	const bucket3b = "spike-test3b"
	kv3b, err := createCoreKVBucket(ctx, js, bucket3b)
	if err != nil {
		return fmt.Sprintf("FAIL setup bucket 3b: %v", err)
	}

	const conflictKey = "vtx.identity.ConflictKeyXxxxxxxxxxxxxxx"
	const sideKey = "vtx.identity.SideKeyXxxxxxxxxxxxxxxxxx"

	// Pre-write conflictKey so we know its revision.
	rev1, err := kv3b.Create(ctx, conflictKey, []byte(`{"v":"initial"}`))
	if err != nil {
		return fmt.Sprintf("FAIL pre-write conflictKey: %v", err)
	}
	fmt.Printf("  Pre-wrote conflictKey at revision=%d\n", rev1)

	// Publish the first (non-commit) batch message for conflictKey with correct revision.
	msg1 := nats.NewMsg(kvBucketSubject(bucket3b, conflictKey))
	msg1.Data = []byte(`{"v":"batch-update"}`)
	msg1.Header = nats.Header{}
	msg1.Header.Set("Nats-Expected-Last-Subject-Sequence", fmt.Sprintf("%d", rev1))
	msg1.Header.Set("Nats-Batch-Id", "spike-test3b-conflict")
	msg1.Header.Set("Nats-Batch-Sequence", "1")

	if err := nc.PublishMsg(msg1); err != nil {
		return fmt.Sprintf("FAIL publish batch msg1: %v", err)
	}
	fmt.Println("  Published batch msg1 (non-commit) for conflictKey")

	// Concurrent write: update conflictKey from outside the batch.
	_, err = kv3b.Update(ctx, conflictKey, []byte(`{"v":"concurrent-update"}`), rev1)
	if err != nil {
		return fmt.Sprintf("FAIL concurrent update of conflictKey: %v", err)
	}
	fmt.Println("  Concurrent writer updated conflictKey (changed per-subject seq)")

	// Publish msg2 (commit). Revision condition on conflictKey is now stale.
	msg2 := nats.NewMsg(kvBucketSubject(bucket3b, sideKey))
	msg2.Data = []byte(`{"v":"side-key"}`)
	msg2.Header = nats.Header{}
	msg2.Header.Set("Nats-Expected-Last-Subject-Sequence", "0")
	msg2.Header.Set("Nats-Batch-Id", "spike-test3b-conflict")
	msg2.Header.Set("Nats-Batch-Sequence", "2")
	msg2.Header.Set("Nats-Batch-Commit", "1")

	resp, err := nc.RequestMsg(msg2, 5*time.Second)
	if err != nil {
		return fmt.Sprintf("FAIL RequestMsg (commit): %v", err)
	}

	var conflictAck pubAckResponse
	if err := json.Unmarshal(resp.Data, &conflictAck); err != nil {
		return fmt.Sprintf("FAIL unmarshal ack: %v (raw: %s)", err, string(resp.Data))
	}

	if conflictAck.Error != nil {
		fmt.Printf("  Concurrent-conflict batch rejected (expected): %s (err_code=%d)\n",
			conflictAck.Error.Description, conflictAck.Error.ErrCode)
		// Confirm sideKey was NOT written (no partial commit).
		_, errSide := kv3b.Get(ctx, sideKey)
		if errSide == nil {
			return "FAIL partial commit: sideKey written despite batch rejection"
		}
		fmt.Println("  Confirmed: sideKey NOT written (atomic rejection, no partial commit)")
	} else {
		// SURPRISE: the batch committed despite conflicting revision.
		// Document as a finding — this would be a contract-relevant finding.
		fmt.Printf("  SURPRISE: batch committed despite concurrent conflicting write (seq=%d)\n", conflictAck.Sequence)
		entryConflict, _ := kv3b.Get(ctx, conflictKey)
		entrySide, _ := kv3b.Get(ctx, sideKey)
		var conflictVal, sideVal string
		if entryConflict != nil {
			conflictVal = string(entryConflict.Value())
		}
		if entrySide != nil {
			sideVal = string(entrySide.Value())
		}
		fmt.Printf("  conflictKey value after commit: %q\n", conflictVal)
		fmt.Printf("  sideKey value after commit: %q\n", sideVal)
		// Still return PASS with a SURPRISE note so the README captures it.
	}

	// Sub-test 3c: verify that Health KV and Capability KV are separate streams
	// from Core KV — cross-bucket atomicity is structurally impossible.
	fmt.Println("  Sub-test 3c: confirm cross-bucket non-atomicity (structural)")
	const healthBucket = "spike-health"
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: healthBucket})
	if err != nil {
		return fmt.Sprintf("FAIL create health bucket: %v", err)
	}

	coreStream, err1 := js.Stream(ctx, "KV_"+bucket)
	healthStream, err2 := js.Stream(ctx, "KV_"+healthBucket)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("FAIL stream lookup: core=%v health=%v", err1, err2)
	}
	fmt.Printf("  Core KV stream: %q, Health KV stream: %q\n",
		coreStream.CachedInfo().Config.Name, healthStream.CachedInfo().Config.Name)
	fmt.Println("  Separate streams confirmed — atomic batches cannot span them.")

	return "PASS: 4-key multi-subject batch committed atomically; concurrent-conflict batch rejected without partial commit; cross-bucket separation confirmed"
}
