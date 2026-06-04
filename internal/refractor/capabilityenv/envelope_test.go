// Unit tests for the capabilityenv wrapper.
package capabilityenv

import (
	"errors"
	"testing"

	"github.com/asolgan/lattice/internal/refractor/pipeline"
)

// makeRow builds a minimal RETURN row as produced by the capability lens cypher.
func makeRow(actorKey string) map[string]any {
	return map[string]any{
		"actorKey":            actorKey,
		"platformPermissions": []any{},
		"serviceAccess":       []any{},
		"ephemeralGrants":     []any{},
		"roles":               []any{},
	}
}

func makeParams() map[string]any {
	return map[string]any{"projectedAt": "2026-05-17T00:00:00Z"}
}

// Valid 20-char NanoID-alphabet keys used in tests.
// Alphabet: ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789
const (
	testIdentityActorID = "ABCDEFGHJKLMNPQRSTUa"
	testRoleActorID     = "ABCDEFGHJKLMNPQRSTUd"
)

// TestWrapper_BuildsEnvelopeForIdentityActor: happy path — the wrapper
// produces a Contract #6 §6.2 envelope with the expected fields populated
// from the row + lens-def key.
func TestWrapper_BuildsEnvelopeForIdentityActor(t *testing.T) {
	actorKey := "vtx.identity." + testIdentityActorID

	fn := NewWrapper("vtx.meta.test-lens", func(string) uint64 { return 0 })
	env, keys, err := fn(makeRow(actorKey), map[string]any{}, makeParams())
	if err != nil {
		t.Fatalf("wrapper returned error: %v", err)
	}
	if env == nil {
		t.Fatal("wrapper returned nil envelope")
	}
	if got, want := env["actor"], actorKey; got != want {
		t.Errorf("actor = %v, want %v", got, want)
	}
	if got, want := env["version"], Version; got != want {
		t.Errorf("version = %v, want %v", got, want)
	}
	if _, has := env["pendingReview"]; has {
		t.Error("envelope must NOT carry pendingReview after Story 4.6 walk-back")
	}
	if keys["key"] == nil {
		t.Error("keys[\"key\"] is unset")
	}
}

// TestWrapper_SkipsNonIdentityActorKey: a non-identity actorKey must be dropped
// (ErrSkipProjection), not processed.
func TestWrapper_SkipsNonIdentityActorKey(t *testing.T) {
	actorKey := "vtx.role." + testRoleActorID

	fn := NewWrapper("vtx.meta.test-lens", func(string) uint64 { return 0 })
	row := map[string]any{
		"actorKey":            actorKey,
		"platformPermissions": []any{},
	}
	_, _, err := fn(row, map[string]any{}, makeParams())
	if err != pipeline.ErrSkipProjection {
		t.Fatalf("expected ErrSkipProjection for non-identity actorKey, got %v", err)
	}
}

// realGrant builds a non-degenerate ephemeral grant collect entry.
func realGrant(taskKey string) map[string]any {
	return map[string]any{
		"source":        "task",
		"taskKey":       taskKey,
		"operationType": "approve",
		"target":        "vtx.leaseapp." + testIdentityActorID,
		"expiresAt":     "2026-06-04T14:00:00Z",
	}
}

// TestEphemeralWrapper_NoRealGrants_Deletes: a live actor whose collect
// contains only degenerate (null-taskKey) artifacts has zero real grants →
// the wrapper signals ErrDeleteProjection keyed at cap.ephemeral.<actor> so
// the key is hard-deleted (absence = denial). This is the A3 mechanism.
func TestEphemeralWrapper_NoRealGrants_Deletes(t *testing.T) {
	actorKey := "vtx.identity." + testIdentityActorID
	fn := NewEphemeralWrapper("vtx.meta.test-lens", func(string) uint64 { return 0 })

	// The cypher's OPTIONAL task matches over a grant-less actor produce a
	// degenerate collect entry with a null taskKey.
	row := map[string]any{
		"actorKey": actorKey,
		"ephemeralGrants": []any{
			map[string]any{"source": "task", "taskKey": nil, "operationType": nil, "target": nil, "expiresAt": nil},
		},
	}
	env, keys, err := fn(row, map[string]any{}, makeParams())
	if !errors.Is(err, pipeline.ErrDeleteProjection) {
		t.Fatalf("expected ErrDeleteProjection for grant-less actor, got %v", err)
	}
	if env != nil {
		t.Fatalf("delete signal must carry no envelope, got %v", env)
	}
	if got := keys["key"]; got != "cap.ephemeral.identity."+testIdentityActorID {
		t.Fatalf("delete key = %v, want cap.ephemeral.identity.%s", got, testIdentityActorID)
	}
}

// TestEphemeralWrapper_EmptyCollect_Deletes: an empty ephemeralGrants array
// (no entries at all) also yields a delete — no live grant.
func TestEphemeralWrapper_EmptyCollect_Deletes(t *testing.T) {
	actorKey := "vtx.identity." + testIdentityActorID
	fn := NewEphemeralWrapper("vtx.meta.test-lens", func(string) uint64 { return 0 })

	row := map[string]any{"actorKey": actorKey, "ephemeralGrants": []any{}}
	_, _, err := fn(row, map[string]any{}, makeParams())
	if !errors.Is(err, pipeline.ErrDeleteProjection) {
		t.Fatalf("expected ErrDeleteProjection for empty collect, got %v", err)
	}
}

// TestEphemeralWrapper_RealGrant_Projects: at least one real grant → the
// wrapper emits the envelope carrying only the real grants (degenerate
// artifacts filtered out).
func TestEphemeralWrapper_RealGrant_Projects(t *testing.T) {
	actorKey := "vtx.identity." + testIdentityActorID
	fn := NewEphemeralWrapper("vtx.meta.test-lens", func(string) uint64 { return 0 })

	taskKey := "vtx.task." + testRoleActorID
	row := map[string]any{
		"actorKey": actorKey,
		"ephemeralGrants": []any{
			realGrant(taskKey),
			// a degenerate artifact alongside the real grant must be dropped
			map[string]any{"source": "task", "taskKey": nil},
		},
	}
	env, keys, err := fn(row, map[string]any{}, makeParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := keys["key"]; got != "cap.ephemeral.identity."+testIdentityActorID {
		t.Fatalf("key = %v, want cap.ephemeral.identity.%s", got, testIdentityActorID)
	}
	grants, ok := env["ephemeralGrants"].([]any)
	if !ok {
		t.Fatalf("ephemeralGrants type = %T, want []any", env["ephemeralGrants"])
	}
	if len(grants) != 1 {
		t.Fatalf("real grants = %d, want 1 (degenerate artifact must be filtered)", len(grants))
	}
	g := grants[0].(map[string]any)
	if g["taskKey"] != taskKey {
		t.Fatalf("grant taskKey = %v, want %v", g["taskKey"], taskKey)
	}
}
