package capabilitykv

import "testing"

func TestParseCapabilityDoc(t *testing.T) {
	raw := []byte(`{
		"key": "cap.roles.identity.ABC",
		"actor": "vtx.identity.ABC",
		"version": "1",
		"lanes": ["default"],
		"platformPermissions": [{"operationType": "ctrl.weaver.disable", "scope": "any"}],
		"roles": ["operator"]
	}`)
	doc, err := ParseCapabilityDoc(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Actor != "vtx.identity.ABC" {
		t.Fatalf("actor: got %q", doc.Actor)
	}
	if len(doc.PlatformPermissions) != 1 || doc.PlatformPermissions[0].OperationType != "ctrl.weaver.disable" {
		t.Fatalf("platformPermissions: got %+v", doc.PlatformPermissions)
	}
	if len(doc.Roles) != 1 || doc.Roles[0] != "operator" {
		t.Fatalf("roles: got %+v", doc.Roles)
	}
}

func TestParseCapabilityDoc_Malformed(t *testing.T) {
	if _, err := ParseCapabilityDoc([]byte(`{not json`)); err == nil {
		t.Fatalf("expected error for malformed JSON")
	}
}
