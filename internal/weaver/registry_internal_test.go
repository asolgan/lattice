package weaver

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestSource(t *testing.T) *targetSource {
	t.Helper()
	return newTargetSource(nil, "core-kv", "test", newIssueCache(), discardLogger())
}

func testNanoID(t *testing.T) string {
	t.Helper()
	id, err := substrate.NewNanoID()
	if err != nil {
		t.Fatalf("NewNanoID: %v", err)
	}
	return id
}

func vertexEvent(t *testing.T, id, class string) substrate.KVEvent {
	t.Helper()
	body, err := json.Marshal(map[string]any{"class": class, "data": map[string]any{}})
	if err != nil {
		t.Fatalf("marshal vertex: %v", err)
	}
	return substrate.KVEvent{Key: "vtx.meta." + id, Value: body}
}

func specEvent(t *testing.T, id string, spec map[string]any) substrate.KVEvent {
	t.Helper()
	body, err := json.Marshal(map[string]any{"class": "spec", "data": spec})
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	return substrate.KVEvent{Key: "vtx.meta." + id + ".spec", Value: body}
}

func targetSpecFixture(targetID string) map[string]any {
	return map[string]any{
		"targetId": targetID,
		"lensRef":  "lensFixture",
		"gaps": map[string]any{
			"missing_a": map[string]any{"action": "directOp", "operation": "FixA"},
		},
	}
}

func hasIssueCode(issues []healthIssue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// TestRegistry_SpecAspectDeleteThenRecreate proves the spec-aspect lifecycle:
// deleting a spec ASPECT unregisters the target but keeps the vertex's class
// entry (the vertex still exists), so a re-created spec registers immediately
// instead of buffering forever.
func TestRegistry_SpecAspectDeleteThenRecreate(t *testing.T) {
	s := newTestSource(t)
	id := testNanoID(t)
	const targetID = "fixtureLifecycle"

	s.handle(vertexEvent(t, id, weaverTargetClass))
	s.handle(specEvent(t, id, targetSpecFixture(targetID)))
	if _, ok := s.target(targetID); !ok {
		t.Fatalf("target %q must register after vertex+spec", targetID)
	}

	// Delete the spec ASPECT (not the vertex).
	s.handle(substrate.KVEvent{Key: "vtx.meta." + id + ".spec", IsDeleted: true})
	if _, ok := s.target(targetID); ok {
		t.Fatalf("target %q must unregister when its spec aspect is deleted", targetID)
	}
	s.mu.Lock()
	class, classKept := s.classes[id]
	pending := len(s.pendingSpecs)
	s.mu.Unlock()
	if !classKept || class != weaverTargetClass {
		t.Fatalf("a spec-aspect delete must keep the vertex's class entry (got %q, kept=%v)", class, classKept)
	}
	if pending != 0 {
		t.Fatalf("a spec-aspect delete must evict any pending buffer, got %d entries", pending)
	}

	// Re-create the spec: it must register immediately (no pending buffer).
	s.handle(specEvent(t, id, targetSpecFixture(targetID)))
	if _, ok := s.target(targetID); !ok {
		t.Fatalf("a re-created spec under a live vertex must register, not buffer")
	}
	s.mu.Lock()
	pending = len(s.pendingSpecs)
	s.mu.Unlock()
	if pending != 0 {
		t.Fatalf("re-created spec must not buffer, got %d pending entries", pending)
	}
}

// TestRegistry_PendingSpecBounds proves the pending-spec buffer is bounded: a
// spec buffered ahead of its vertex is dropped once the class is learned to be
// non-routed, a spec for a known non-routed class is never buffered, and a
// vertex delete evicts the buffer.
func TestRegistry_PendingSpecBounds(t *testing.T) {
	s := newTestSource(t)

	// Spec arrives before its vertex → buffered.
	id := testNanoID(t)
	s.handle(specEvent(t, id, map[string]any{"some": "lensSpec"}))
	s.mu.Lock()
	pending := len(s.pendingSpecs)
	s.mu.Unlock()
	if pending != 1 {
		t.Fatalf("spec-before-vertex must buffer, got %d pending entries", pending)
	}

	// The vertex turns out non-routed → the buffer drops.
	s.handle(vertexEvent(t, id, "meta.lens"))
	s.mu.Lock()
	pending = len(s.pendingSpecs)
	s.mu.Unlock()
	if pending != 0 {
		t.Fatalf("learning a non-routed class must drop the pending spec, got %d entries", pending)
	}

	// A later spec write for the known non-routed vertex is never buffered.
	s.handle(specEvent(t, id, map[string]any{"some": "lensSpec2"}))
	s.mu.Lock()
	pending = len(s.pendingSpecs)
	s.mu.Unlock()
	if pending != 0 {
		t.Fatalf("a spec for a known non-routed class must not buffer, got %d entries", pending)
	}

	// Vertex delete evicts a pending spec.
	id2 := testNanoID(t)
	s.handle(specEvent(t, id2, map[string]any{"some": "spec"}))
	s.handle(substrate.KVEvent{Key: "vtx.meta." + id2, IsDeleted: true})
	s.mu.Lock()
	pending = len(s.pendingSpecs)
	s.mu.Unlock()
	if pending != 0 {
		t.Fatalf("a vertex delete must evict its pending spec, got %d entries", pending)
	}
}

// TestRegistry_OrphanedSpecHealthIssue proves a spec stuck pending past the
// bound surfaces as a Health issue (never silent) and that the issue clears
// once the parent vertex's class arrives.
func TestRegistry_OrphanedSpecHealthIssue(t *testing.T) {
	s := newTestSource(t)
	id := testNanoID(t)
	s.handle(specEvent(t, id, targetSpecFixture("fixtureOrphan")))

	// Not yet past the bound: no issue.
	s.flagOrphanedSpecs()
	if hasIssueCode(s.issues.snapshot(), "OrphanedSpec") {
		t.Fatalf("a freshly-buffered spec must not be flagged as orphaned")
	}

	// Backdate the pending entry past the bound.
	s.mu.Lock()
	s.pendingSince[id] = time.Now().Add(-pendingSpecWarnAfter - time.Minute)
	s.mu.Unlock()
	s.flagOrphanedSpecs()
	if !hasIssueCode(s.issues.snapshot(), "OrphanedSpec") {
		t.Fatalf("a spec pending past the bound must surface an OrphanedSpec Health issue")
	}

	// The vertex finally arrives: the spec drains and the issue clears.
	s.handle(vertexEvent(t, id, weaverTargetClass))
	if hasIssueCode(s.issues.snapshot(), "OrphanedSpec") {
		t.Fatalf("the OrphanedSpec issue must clear once the spec drains")
	}
	if _, ok := s.target("fixtureOrphan"); !ok {
		t.Fatalf("the drained spec must register its target")
	}
}

// TestValidateTarget_GapColumnCharsetAndReservedParam proves the install-time
// validations: a gaps key with characters invalid in a KV key segment is
// rejected (it becomes a mark-key segment), and a playbook param named
// expectedRevision (the engine-owned payload field) is rejected instead of
// silently clobbered at dispatch.
func TestValidateTarget_GapColumnCharsetAndReservedParam(t *testing.T) {
	valid := &Target{
		TargetID: "fixtureValid",
		Gaps: map[string]GapAction{
			"missing_a": {Action: actionDirectOp, Operation: "FixA", Params: map[string]string{"note": "x"}},
		},
	}
	if err := validateTarget(valid); err != nil {
		t.Fatalf("valid target must pass: %v", err)
	}

	for _, col := range []string{"missing_bg check", "missing_bg.check", "missing_bg*", "missing_bg>"} {
		bad := &Target{
			TargetID: "fixtureBadCol",
			Gaps:     map[string]GapAction{col: {Action: actionDirectOp, Operation: "Fix"}},
		}
		err := validateTarget(bad)
		if err == nil {
			t.Fatalf("gaps key %q must be rejected (invalid KV key segment)", col)
		}
		if !strings.Contains(err.Error(), "invalid in a KV key segment") {
			t.Fatalf("gaps key %q: unexpected rejection reason: %v", col, err)
		}
	}

	reserved := &Target{
		TargetID: "fixtureReserved",
		Gaps: map[string]GapAction{
			"missing_a": {Action: actionDirectOp, Operation: "FixA",
				Params: map[string]string{"expectedRevision": "row.someRev"}},
		},
	}
	err := validateTarget(reserved)
	if err == nil {
		t.Fatalf("a param named expectedRevision must be rejected at install time")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("unexpected rejection reason for reserved param: %v", err)
	}
}
