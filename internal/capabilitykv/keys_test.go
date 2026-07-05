package capabilitykv

import "testing"

func TestCapabilityKeyFromActor(t *testing.T) {
	got, err := CapabilityKeyFromActor("vtx.identity.ABC")
	if err != nil || got != "cap.identity.ABC" {
		t.Fatalf("got (%q,%v), want (cap.identity.ABC, nil)", got, err)
	}
	if _, err := CapabilityKeyFromActor("ABC"); err == nil {
		t.Fatalf("expected error for malformed actor")
	}
	if _, err := CapabilityKeyFromActor(""); err == nil {
		t.Fatalf("expected error for empty actor")
	}
}

func TestRolesKeyFromActor(t *testing.T) {
	got, err := RolesKeyFromActor("vtx.identity.ABC")
	if err != nil || got != "cap.roles.identity.ABC" {
		t.Fatalf("got (%q,%v), want (cap.roles.identity.ABC, nil)", got, err)
	}
	if _, err := RolesKeyFromActor("ABC"); err == nil {
		t.Fatalf("expected error for malformed actor")
	}
}

func TestClassAwarePlatformKey_OrdinaryActor(t *testing.T) {
	derive := ClassAwarePlatformKey([]string{"vtx.identity.SYS"})
	keys, err := derive("vtx.identity.ORDINARY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 || keys[0] != "cap.roles.identity.ORDINARY" {
		t.Fatalf("ordinary actor keys: got %v, want single cap.roles.* key", keys)
	}
}

func TestClassAwarePlatformKey_SystemActorUnion(t *testing.T) {
	derive := ClassAwarePlatformKey([]string{"vtx.identity.SYS"})
	keys, err := derive("vtx.identity.SYS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "cap.identity.SYS" || keys[1] != "cap.roles.identity.SYS" {
		t.Fatalf("system actor keys: got %v, want [cap.identity.SYS cap.roles.identity.SYS]", keys)
	}
}

func TestClassAwarePlatformKey_MalformedActor(t *testing.T) {
	derive := ClassAwarePlatformKey(nil)
	if _, err := derive("not-a-vertex-key"); err == nil {
		t.Fatalf("expected error for malformed actor")
	}
}

func TestClassAwarePlatformKey_EmptySystemSetIgnoresEmptyEntries(t *testing.T) {
	derive := ClassAwarePlatformKey([]string{"", "vtx.identity.SYS"})
	keys, err := derive("vtx.identity.ORDINARY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %v, want single-key ordinary route (empty system entry must not match)", keys)
	}
}
