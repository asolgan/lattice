package processor

import (
	"testing"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

func TestBuildEventList_OrderAndIDs(t *testing.T) {
	env := &OperationEnvelope{RequestID: "Hj4kPmRtw9nbCxz5vQ2y"}
	result := ScriptResult{
		Mutations: []MutationOp{
			{Op: "create", Key: "vtx.identity." + testNanoID1},
		},
		Events: []EventSpec{
			{Class: "identityCreated", Data: map[string]interface{}{"x": 1}},
			{Class: "auditEntry", Data: map[string]interface{}{"y": 2}},
		},
	}
	at := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	list, err := BuildEventList(env, result, at)
	if err != nil {
		t.Fatalf("BuildEventList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].EventType != "identityCreated" || list[1].EventType != "auditEntry" {
		t.Fatalf("order broken: %+v", list)
	}
	if list[0].RequestID != env.RequestID {
		t.Fatalf("RequestID not propagated")
	}
	for _, e := range list {
		if !substrate.IsValidNanoID(e.EventID) {
			t.Fatalf("EventID %q is not a valid NanoID", e.EventID)
		}
		if e.Timestamp == "" {
			t.Fatalf("missing timestamp")
		}
	}
	// EventIDs unique.
	if list[0].EventID == list[1].EventID {
		t.Fatalf("event IDs collide")
	}
	// targetKey defaulted to mutations[0] for events[0] (1:1 mapping
	// when possible).
	if list[0].TargetKey != "vtx.identity."+testNanoID1 {
		t.Fatalf("TargetKey default failed: %q", list[0].TargetKey)
	}
}

func TestBuildEventList_EmptyOk(t *testing.T) {
	env := &OperationEnvelope{RequestID: testNanoID1}
	list, err := BuildEventList(env, ScriptResult{}, time.Now())
	if err != nil {
		t.Fatalf("BuildEventList(empty): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestBuildEventList_MissingClassError(t *testing.T) {
	env := &OperationEnvelope{RequestID: testNanoID1}
	result := ScriptResult{Events: []EventSpec{{Data: map[string]interface{}{}}}}
	if _, err := BuildEventList(env, result, time.Now()); err == nil {
		t.Fatalf("expected error for missing event class")
	}
}

func TestBuildEventList_ExplicitTargetKeyWins(t *testing.T) {
	env := &OperationEnvelope{RequestID: testNanoID1}
	result := ScriptResult{
		Events: []EventSpec{{
			Class: "x",
			Data:  map[string]interface{}{"targetKey": "vtx.identity.aaa"},
		}},
	}
	list, err := BuildEventList(env, result, time.Now())
	if err != nil {
		t.Fatalf("BuildEventList: %v", err)
	}
	if list[0].TargetKey != "vtx.identity.aaa" {
		t.Fatalf("TargetKey = %q, want explicit", list[0].TargetKey)
	}
}
