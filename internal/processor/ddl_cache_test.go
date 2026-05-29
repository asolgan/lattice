package processor

import (
	"testing"
)

func TestDDLCache_RefreshAndLookup_ShadowKey(t *testing.T) {
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// setupTestPipeline seeds vtx.meta.identity + .script.
	ref, ok := cache.Lookup("identity")
	if !ok {
		t.Fatalf("identity DDL not in cache")
	}
	if ref.MetaVertexKey != "vtx.meta.identity" {
		t.Fatalf("MetaVertexKey = %q", ref.MetaVertexKey)
	}
	if ref.ScriptSource == "" {
		t.Fatalf("ScriptSource empty")
	}
	if len(ref.PermittedCommands) == 0 || ref.PermittedCommands[0] != "CreateIdentity" {
		t.Fatalf("PermittedCommands = %v", ref.PermittedCommands)
	}
}

func TestDDLCache_Invalidate_AfterPut(t *testing.T) {
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Seed a new meta-vertex.
	newKey := "vtx.meta.newclass"
	doc := []byte(`{"class":"meta.ddl.vertexType","isDeleted":false,"data":{"canonicalName":"newclass","permittedCommands":["DoNew"]}}`)
	if _, err := conn.KVPut(ctx, testCoreBucket, newKey, doc); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := cache.Invalidate(ctx, newKey); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	ref, ok := cache.Lookup("newclass")
	if !ok || ref.MetaVertexKey != newKey {
		t.Fatalf("after invalidate, Lookup got ok=%v ref=%+v", ok, ref)
	}
}

func TestDDLCache_Lookup_MissReturnsFalse(t *testing.T) {
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, ok := cache.Lookup("nonexistent"); ok {
		t.Fatalf("expected miss for nonexistent class")
	}
}

func TestDDLCache_Invalidate_EvictsTombstonedRoot(t *testing.T) {
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Seed a live meta-vertex and pull it into the cache.
	key := "vtx.meta.tombclass"
	live := []byte(`{"class":"meta.ddl.vertexType","isDeleted":false,"data":{"canonicalName":"tombclass","permittedCommands":["DoTomb"]}}`)
	if _, err := conn.KVPut(ctx, testCoreBucket, key, live); err != nil {
		t.Fatalf("seed live: %v", err)
	}
	if err := cache.Invalidate(ctx, key); err != nil {
		t.Fatalf("Invalidate (live): %v", err)
	}
	if _, ok := cache.Lookup("tombclass"); !ok {
		t.Fatalf("tombclass should be present after live invalidate")
	}
	if _, ok := cache.LookupByMetaKey(key); !ok {
		t.Fatalf("LookupByMetaKey should resolve before tombstone")
	}

	// Tombstone the root (isDeleted=true) and re-invalidate. The entry must
	// be evicted from both indexes and not re-inserted.
	dead := []byte(`{"class":"meta.ddl.vertexType","isDeleted":true,"data":{}}`)
	if _, err := conn.KVPut(ctx, testCoreBucket, key, dead); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
	if err := cache.Invalidate(ctx, key); err != nil {
		t.Fatalf("Invalidate (tombstoned): %v", err)
	}
	if ref, ok := cache.Lookup("tombclass"); ok {
		t.Fatalf("tombclass must be evicted after tombstone, got %+v", ref)
	}
	if _, ok := cache.LookupByMetaKey(key); ok {
		t.Fatalf("LookupByMetaKey must report absent after tombstone")
	}
}

func TestDDLCache_LoadMetaVertex_TombstonedRootAbsent(t *testing.T) {
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	key := "vtx.meta.deadload"
	// A tombstoned root with a still-present canonicalName aspect must report
	// absent before any aspect read — eviction precedes name resolution.
	dead := []byte(`{"class":"meta.ddl.vertexType","isDeleted":true,"data":{}}`)
	if _, err := conn.KVPut(ctx, testCoreBucket, key, dead); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
	cn := []byte(`{"class":"canonicalName","isDeleted":false,"data":{"value":"deadload"}}`)
	if _, err := conn.KVPut(ctx, testCoreBucket, key+".canonicalName", cn); err != nil {
		t.Fatalf("seed canonicalName: %v", err)
	}
	ref, ok, err := cache.loadMetaVertex(ctx, key, nil)
	if err != nil {
		t.Fatalf("loadMetaVertex: %v", err)
	}
	if ok {
		t.Fatalf("tombstoned root must load as absent, got %+v", ref)
	}
}

func TestDDLCache_Invalidate_AspectKeyResolvesToRoot(t *testing.T) {
	ctx, conn, _, _, _ := setupTestPipeline(t)
	cache := NewDDLCache(conn, testCoreBucket, testLogger())
	if err := cache.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Invalidate via an aspect key — should derive root.
	if err := cache.Invalidate(ctx, "vtx.meta.identity.permittedCommands"); err != nil {
		t.Fatalf("Invalidate via aspect: %v", err)
	}
	if _, ok := cache.Lookup("identity"); !ok {
		t.Fatalf("identity should still be present after aspect invalidate")
	}
}
