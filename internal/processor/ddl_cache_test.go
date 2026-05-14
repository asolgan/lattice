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
