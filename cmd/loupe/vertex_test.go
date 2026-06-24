package main

import (
	"testing"
)

func TestLinkForVertex(t *testing.T) {
	const link = "lnk.identity.Y.holdsRole.role.X"

	t.Run("vertex is target → in", func(t *testing.T) {
		lr, ok := linkForVertex(link, "vtx.role.X")
		if !ok {
			t.Fatal("expected match")
		}
		if lr.Direction != "in" || lr.Relation != "holdsRole" || lr.OtherKey != "vtx.identity.Y" || lr.OtherType != "identity" {
			t.Errorf("got %+v", lr)
		}
	})

	t.Run("vertex is source → out", func(t *testing.T) {
		lr, ok := linkForVertex(link, "vtx.identity.Y")
		if !ok {
			t.Fatal("expected match")
		}
		if lr.Direction != "out" || lr.OtherKey != "vtx.role.X" || lr.OtherType != "role" {
			t.Errorf("got %+v", lr)
		}
	})

	t.Run("unrelated vertex → no match", func(t *testing.T) {
		if _, ok := linkForVertex(link, "vtx.role.Z"); ok {
			t.Error("expected no match")
		}
	})

	t.Run("malformed link key → no match", func(t *testing.T) {
		if _, ok := linkForVertex("lnk.identity.Y.holdsRole.role", "vtx.role.X"); ok {
			t.Error("expected no match for 5-segment key")
		}
	})
}

func TestBuildVertexList(t *testing.T) {
	store := map[string][]byte{
		"vtx.role.R1":               []byte(`{"class":"role","isDeleted":false,"data":{"protected":true}}`),
		"vtx.role.R1.canonicalName": []byte(`{"data":{"value":"operator"}}`),
		"vtx.package.P1":            []byte(`{"class":"package","data":{"name":"rbac-domain","version":"0.1.0"}}`),
		"vtx.op.O1":                 []byte(`{"class":"op","data":{"operationType":"CreateRole"}}`),
		"vtx.identity.I1":           []byte(`{"class":"identity","isDeleted":true,"data":{"note":"long note"}}`),
		"vtx.meta.M1":               []byte(`{"class":"meta.ddl.vertexType","data":{}}`),
		"vtx.meta.M1.canonicalName": []byte(`{"data":{"value":"rbac"}}`),
		// non-roots that must be excluded from the vertex list:
		"vtx.role.R1.description":           []byte(`{"data":{"value":"d"}}`),
		"lnk.identity.I1.holdsRole.role.R1": []byte(`{}`),
	}
	get := func(k string) ([]byte, bool) { b, ok := store[k]; return b, ok }
	keys := []string{
		"vtx.role.R1", "vtx.role.R1.canonicalName", "vtx.role.R1.description",
		"vtx.package.P1", "vtx.op.O1", "vtx.identity.I1",
		"vtx.meta.M1", "vtx.meta.M1.canonicalName",
		"lnk.identity.I1.holdsRole.role.R1",
	}

	rows, truncated := buildVertexList(keys, get, "", 100)
	if truncated {
		t.Error("did not expect truncation")
	}
	byKey := map[string]vertexRow{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	if len(rows) != 5 {
		t.Fatalf("got %d vertices, want 5 (R1,P1,O1,I1,M1); %+v", len(rows), rows)
	}
	if byKey["vtx.role.R1"].Label != "operator" {
		t.Errorf("role label = %q, want operator (from .canonicalName fallback)", byKey["vtx.role.R1"].Label)
	}
	if byKey["vtx.package.P1"].Label != "rbac-domain" {
		t.Errorf("package label = %q, want rbac-domain", byKey["vtx.package.P1"].Label)
	}
	if byKey["vtx.op.O1"].Label != "CreateRole" {
		t.Errorf("op label = %q, want CreateRole (operationType)", byKey["vtx.op.O1"].Label)
	}
	if byKey["vtx.meta.M1"].Label != "rbac" || byKey["vtx.meta.M1"].Type != "meta" {
		t.Errorf("meta row = %+v, want label=rbac type=meta", byKey["vtx.meta.M1"])
	}
	i1 := byKey["vtx.identity.I1"]
	if i1.Label != "" || !i1.IsDeleted {
		t.Errorf("identity row = %+v, want empty label + isDeleted", i1)
	}

	t.Run("prefix filters", func(t *testing.T) {
		rows, _ := buildVertexList(keys, get, "vtx.role.", 100)
		if len(rows) != 1 || rows[0].Key != "vtx.role.R1" {
			t.Errorf("prefix filter = %+v", rows)
		}
	})

	t.Run("limit truncates", func(t *testing.T) {
		rows, truncated := buildVertexList(keys, get, "", 1)
		if len(rows) != 1 || !truncated {
			t.Errorf("limit=1 → rows=%d truncated=%v", len(rows), truncated)
		}
	})
}

func TestBuildVertexDetail(t *testing.T) {
	root := []byte(`{"class":"role","isDeleted":false,"data":{"protected":true}}`)
	allKeys := []string{
		"vtx.role.R1",
		"vtx.role.R1.canonicalName",
		"vtx.role.R1.description",
		"vtx.role.R1.deep.nested",              // 5-seg → not a direct aspect, excluded
		"lnk.identity.I1.holdsRole.role.R1",    // in
		"lnk.permission.P9.grantedBy.role.R1",  // in
		"lnk.role.R1.governs.identity.Z",       // out
		"lnk.identity.I2.holdsRole.role.OTHER", // unrelated
	}

	vd := buildVertexDetail("vtx.role.R1", root, 7, allKeys)

	if vd.Class != "role" || vd.Revision != 7 || vd.IsDeleted {
		t.Errorf("header = class %q rev %d deleted %v", vd.Class, vd.Revision, vd.IsDeleted)
	}
	if len(vd.Aspects) != 2 || vd.Aspects[0].LocalName != "canonicalName" || vd.Aspects[1].LocalName != "description" {
		t.Errorf("aspects = %+v, want [canonicalName description]", vd.Aspects)
	}
	if len(vd.Links) != 3 {
		t.Fatalf("links = %d, want 3; %+v", len(vd.Links), vd.Links)
	}
	// sorted by relation then otherKey (lexicographic): governs, grantedBy, holdsRole.
	if vd.Links[0].Relation != "governs" || vd.Links[0].Direction != "out" || vd.Links[0].OtherKey != "vtx.identity.Z" {
		t.Errorf("link[0] = %+v", vd.Links[0])
	}
	if vd.Links[1].Relation != "grantedBy" || vd.Links[1].Direction != "in" || vd.Links[1].OtherKey != "vtx.permission.P9" {
		t.Errorf("link[1] = %+v", vd.Links[1])
	}
	if vd.Links[2].Relation != "holdsRole" || vd.Links[2].Direction != "in" {
		t.Errorf("link[2] = %+v", vd.Links[2])
	}
}
