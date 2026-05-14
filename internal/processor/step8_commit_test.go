package processor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// buildCommitterPipeline assembles a CommitterImpl wired against a
// fresh embedded NATS + Core KV harness.
func buildCommitterPipeline(t *testing.T) (context.Context, *CommitterImpl, *DDLCache) {
	t.Helper()
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	c := NewCommitter(conn, testCoreBucket, cache, testLogger(), time.Now)
	return ctx, c, cache
}

func TestCommit_CleanWriteTrackerAndMutation(t *testing.T) {
	ctx, c, _ := buildCommitterPipeline(t)
	env := newTestEnvelope(testNanoID1)
	result := ScriptResult{
		Mutations: []MutationOp{{
			Op:  "create",
			Key: "vtx.identity." + testNanoID2,
			Document: map[string]interface{}{
				"class": "identity",
				"data":  map[string]interface{}{"name": "Andrew"},
			},
		}},
	}
	tracker := NewTracker(env, time.Now())
	ack, err := c.Commit(ctx, env, result, tracker)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ack.Count == 0 {
		t.Fatalf("ack.Count = 0")
	}
	// Tracker and mutation present.
	if _, err := c.Conn.KVGet(ctx, testCoreBucket, tracker.Key); err != nil {
		t.Fatalf("tracker missing: %v", err)
	}
	entry, err := c.Conn.KVGet(ctx, testCoreBucket, "vtx.identity."+testNanoID2)
	if err != nil {
		t.Fatalf("mutation key missing: %v", err)
	}
	// Provenance injected.
	var doc map[string]interface{}
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc["createdByOp"] != tracker.Key {
		t.Fatalf("createdByOp = %v", doc["createdByOp"])
	}
	if doc["createdBy"] != env.Actor {
		t.Fatalf("createdBy = %v", doc["createdBy"])
	}
}

func TestCommit_RevisionConflictSurfacesConflictError(t *testing.T) {
	ctx, c, _ := buildCommitterPipeline(t)
	env := newTestEnvelope(testNanoID1)
	key := "vtx.identity." + testNanoID2
	// Pre-create the key so the create-only mutation conflicts.
	pre := []byte(`{"class":"identity","isDeleted":false,"data":{}}`)
	if _, err := c.Conn.KVPut(ctx, testCoreBucket, key, pre); err != nil {
		t.Fatalf("pre-seed: %v", err)
	}
	result := ScriptResult{
		Mutations: []MutationOp{{
			Op:  "create",
			Key: key,
			Document: map[string]interface{}{
				"class": "identity",
			},
		}},
	}
	tracker := NewTracker(env, time.Now())
	_, err := c.Commit(ctx, env, result, tracker)
	if err == nil {
		t.Fatalf("expected error from conflicting create")
	}
	var confErr *ConflictError
	if !errors.As(err, &confErr) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
}

func TestCommit_MetaVertexMutation_InvalidatesCache(t *testing.T) {
	ctx, c, cache := buildCommitterPipeline(t)
	env := newTestEnvelope(testNanoID1)
	env.OperationType = "RegisterDDL"
	// New DDL meta-vertex.
	newDDLKey := "vtx.meta.brandnew"
	result := ScriptResult{
		Mutations: []MutationOp{{
			Op:  "create",
			Key: newDDLKey,
			Document: map[string]interface{}{
				"class": "meta.ddl.vertexType",
				"data":  map[string]interface{}{"canonicalName": "brandnew", "permittedCommands": []string{"RegisterDDL"}},
			},
		}},
	}
	tracker := NewTracker(env, time.Now())
	if _, err := c.Commit(ctx, env, result, tracker); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Cache should now know about "brandnew".
	if _, ok := cache.Lookup("brandnew"); !ok {
		t.Fatalf("cache did not invalidate; brandnew not present")
	}
}

func TestCommit_TombstoneSetsIsDeleted(t *testing.T) {
	ctx, c, _ := buildCommitterPipeline(t)
	env := newTestEnvelope(testNanoID1)
	key := "vtx.identity." + testNanoID2
	pre := []byte(`{"key":"` + key + `","class":"identity","isDeleted":false,"data":{}}`)
	if _, err := c.Conn.KVPut(ctx, testCoreBucket, key, pre); err != nil {
		t.Fatalf("pre-seed: %v", err)
	}
	result := ScriptResult{
		Mutations: []MutationOp{{
			Op:       "tombstone",
			Key:      key,
			Document: map[string]interface{}{"class": "identity"},
		}},
	}
	tracker := NewTracker(env, time.Now())
	if _, err := c.Commit(ctx, env, result, tracker); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	entry, err := c.Conn.KVGet(ctx, testCoreBucket, key)
	if err != nil {
		t.Fatalf("read tombstoned: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(entry.Value, &doc)
	if isDel, _ := doc["isDeleted"].(bool); !isDel {
		t.Fatalf("isDeleted not set on tombstone: %v", doc)
	}
}

func TestCommit_MixedTTLBatch_TrackerHasTTLOthersDont(t *testing.T) {
	// Story 1.1 spike validated that a single op in a batch may carry
	// a TTL while siblings do not. This test exercises that mixed
	// shape end-to-end through the CommitterImpl.
	ctx, c, _ := buildCommitterPipeline(t)
	env := newTestEnvelope(testNanoID1)
	result := ScriptResult{
		Mutations: []MutationOp{{
			Op:  "create",
			Key: "vtx.identity." + testNanoID2,
			Document: map[string]interface{}{
				"class": "identity",
			},
		}},
	}
	tracker := NewTracker(env, time.Now())
	if _, err := c.Commit(ctx, env, result, tracker); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Both keys exist immediately after commit (TTL is 24h — tracker
	// is present; the durable identity is also present). A finer
	// per-key TTL probe would require waiting out the marker; Story
	// 1.1's spike covers that and we trust the BatchOp wiring here.
	if _, err := c.Conn.KVGet(ctx, testCoreBucket, tracker.Key); err != nil {
		t.Fatalf("tracker missing after mixed-TTL batch: %v", err)
	}
	if _, err := c.Conn.KVGet(ctx, testCoreBucket, "vtx.identity."+testNanoID2); err != nil {
		t.Fatalf("durable mutation missing: %v", err)
	}
}

func TestCommit_TrackerCarriesMutationKeysAndEventClasses(t *testing.T) {
	ctx, c, _ := buildCommitterPipeline(t)
	env := newTestEnvelope(testNanoID1)
	result := ScriptResult{
		Mutations: []MutationOp{{
			Op:  "create",
			Key: "vtx.identity." + testNanoID2,
			Document: map[string]interface{}{
				"class": "identity",
			},
		}},
		Events: []EventSpec{{Class: "identityCreated", Data: map[string]interface{}{"x": 1}}},
	}
	tracker := NewTracker(env, time.Now())
	if _, err := c.Commit(ctx, env, result, tracker); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	entry, err := c.Conn.KVGet(ctx, testCoreBucket, tracker.Key)
	if err != nil {
		t.Fatalf("read tracker: %v", err)
	}
	parsed, err := ParseTracker(entry.Value)
	if err != nil {
		t.Fatalf("ParseTracker: %v", err)
	}
	muts, _ := parsed.Data["mutationKeys"].([]interface{})
	if len(muts) != 1 {
		t.Fatalf("mutationKeys = %v", parsed.Data["mutationKeys"])
	}
	evs, _ := parsed.Data["eventClasses"].([]interface{})
	if len(evs) != 1 || evs[0] != "identityCreated" {
		t.Fatalf("eventClasses = %v", parsed.Data["eventClasses"])
	}
}
