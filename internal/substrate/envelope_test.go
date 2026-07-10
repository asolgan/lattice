package substrate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewDocumentEnvelope_MandatoryFields(t *testing.T) {
	actor := VertexKey("identity", testNanoID2)
	op := VertexKey("op", testNanoID3)
	env := NewDocumentEnvelope("identity", actor, op)

	if env.Class != "identity" {
		t.Fatalf("Class = %q", env.Class)
	}
	if env.IsDeleted {
		t.Fatalf("IsDeleted should default false")
	}
	if env.CreatedBy != actor || env.LastModifiedBy != actor {
		t.Fatalf("actor wiring wrong: createdBy=%q lastModifiedBy=%q", env.CreatedBy, env.LastModifiedBy)
	}
	if env.CreatedByOp != op || env.LastModifiedByOp != op {
		t.Fatalf("opTracker wiring wrong: createdByOp=%q lastModifiedByOp=%q", env.CreatedByOp, env.LastModifiedByOp)
	}
	if env.CreatedAt == "" || env.LastModifiedAt == "" {
		t.Fatalf("timestamps must be set")
	}
	if env.CreatedAt != env.LastModifiedAt {
		t.Fatalf("on creation, createdAt and lastModifiedAt must equal")
	}
	if env.Data != nil {
		t.Fatalf("data must be nil until caller sets it")
	}
}

func TestEnvelopeJSON_AllRequiredFields(t *testing.T) {
	actor := VertexKey("identity", testNanoID2)
	op := VertexKey("op", testNanoID3)
	ts := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	env := NewDocumentEnvelopeAt("identity", actor, op, ts)
	env.Key = VertexKey("identity", testNanoID1)
	env.Data = map[string]any{} // explicit empty so json emits {} not null

	b, err := env.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []string{
		"key", "class", "isDeleted",
		"createdAt", "createdBy", "createdByOp",
		"lastModifiedAt", "lastModifiedBy", "lastModifiedByOp",
		"data",
	}
	for _, f := range required {
		if _, ok := m[f]; !ok {
			t.Fatalf("missing required field %q in JSON: %s", f, b)
		}
	}
	if m["isDeleted"].(bool) != false {
		t.Fatalf("isDeleted should serialize as false")
	}
	if !strings.Contains(string(b), "2026-05-13T12:00:00Z") {
		t.Fatalf("createdAt should reflect UTC ISO8601: %s", b)
	}
}

func TestUpdate_TripletOnly(t *testing.T) {
	actor1 := VertexKey("identity", testNanoID1)
	op1 := VertexKey("op", testNanoID3)
	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	env := NewDocumentEnvelopeAt("identity", actor1, op1, ts1)
	origCreatedAt := env.CreatedAt
	origCreatedBy := env.CreatedBy
	origCreatedByOp := env.CreatedByOp

	actor2 := VertexKey("identity", testNanoID2)
	op2 := VertexKey("op", testNanoID1)
	ts2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	env.UpdateAt(actor2, op2, ts2)

	if env.CreatedAt != origCreatedAt {
		t.Fatalf("CreatedAt mutated: %q -> %q", origCreatedAt, env.CreatedAt)
	}
	if env.CreatedBy != origCreatedBy {
		t.Fatalf("CreatedBy mutated")
	}
	if env.CreatedByOp != origCreatedByOp {
		t.Fatalf("CreatedByOp mutated")
	}
	if env.LastModifiedBy != actor2 || env.LastModifiedByOp != op2 {
		t.Fatalf("lastModified actor/op not updated")
	}
	if !strings.Contains(env.LastModifiedAt, "2026-06-01") {
		t.Fatalf("lastModifiedAt not updated: %q", env.LastModifiedAt)
	}
}

// Update is the time.Now() convenience form of UpdateAt — it must stamp the
// same lastModified* triplet and leave the created* triplet untouched.
func TestUpdate_UsesCurrentTime(t *testing.T) {
	actor1 := VertexKey("identity", testNanoID1)
	op1 := VertexKey("op", testNanoID3)
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	env := NewDocumentEnvelopeAt("identity", actor1, op1, past)
	origCreatedAt := env.CreatedAt

	before := time.Now().UTC()
	actor2 := VertexKey("identity", testNanoID2)
	op2 := VertexKey("op", testNanoID1)
	env.Update(actor2, op2)
	after := time.Now().UTC()

	if env.CreatedAt != origCreatedAt {
		t.Fatalf("CreatedAt mutated: %q -> %q", origCreatedAt, env.CreatedAt)
	}
	if env.LastModifiedBy != actor2 || env.LastModifiedByOp != op2 {
		t.Fatalf("lastModified actor/op not updated")
	}
	got, err := time.Parse(time.RFC3339Nano, env.LastModifiedAt)
	if err != nil {
		t.Fatalf("LastModifiedAt not parseable: %v", err)
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("LastModifiedAt %v not within [%v, %v]", got, before, after)
	}
}

// Update panics on an empty actor/opTracker exactly like UpdateAt — it must
// not silently stamp an unattributed modification.
func TestUpdate_EmptyActor_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Update with empty actor to panic")
		}
	}()
	env := NewDocumentEnvelope("identity", VertexKey("identity", testNanoID1), VertexKey("op", testNanoID3))
	env.Update("", VertexKey("op", testNanoID1))
}

func TestAspectAndLinkEnvelopes_Marshal(t *testing.T) {
	actor := VertexKey("identity", testNanoID2)
	op := VertexKey("op", testNanoID3)
	asp := AspectEnvelope{
		DocumentEnvelope: NewDocumentEnvelope("email", actor, op),
		VertexKey:        VertexKey("identity", testNanoID1),
		LocalName:        "email",
	}
	asp.Key = AspectKey(asp.VertexKey, "email")
	b, err := asp.Marshal()
	if err != nil {
		t.Fatalf("asp marshal: %v", err)
	}
	if !strings.Contains(string(b), `"vertexKey":`) || !strings.Contains(string(b), `"localName":"email"`) {
		t.Fatalf("aspect envelope missing extension fields: %s", b)
	}

	lnk := LinkEnvelope{
		DocumentEnvelope: NewDocumentEnvelope("heldBy", actor, op),
		SourceVertex:     VertexKey("lease", testNanoID3),
		TargetVertex:     VertexKey("identity", testNanoID1),
		LocalName:        "heldBy",
	}
	lnk.Key = LinkKey("lease", testNanoID3, "heldBy", "identity", testNanoID1)
	bl, err := lnk.Marshal()
	if err != nil {
		t.Fatalf("lnk marshal: %v", err)
	}
	if !strings.Contains(string(bl), `"sourceVertex":`) || !strings.Contains(string(bl), `"targetVertex":`) {
		t.Fatalf("link envelope missing extension fields: %s", bl)
	}
}
