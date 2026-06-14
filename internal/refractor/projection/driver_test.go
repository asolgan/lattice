package projection_test

import (
	"errors"
	"testing"

	"github.com/asolgan/lattice/internal/refractor/lens"
	"github.com/asolgan/lattice/internal/refractor/pipeline"
	"github.com/asolgan/lattice/internal/refractor/projection"
)

func ephemeralDesc(t *testing.T) projection.OutputDescriptor {
	t.Helper()
	d, err := projection.ParseOutputDescriptor(&lens.OutputDescriptorSpec{
		AnchorType:       "identity",
		OutputKeyPattern: "cap.ephemeral.{actorSuffix}",
		BodyColumns:      []string{"ephemeralGrants"},
		EmptyBehavior:    "delete",
		RealnessFilter:   "taskKey",
		Freshness:        "auto",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func capDesc(t *testing.T) projection.OutputDescriptor {
	t.Helper()
	d, err := projection.ParseOutputDescriptor(&lens.OutputDescriptorSpec{
		AnchorType:         "identity",
		OutputKeyPattern:   "cap.{actorSuffix}",
		BodyColumns:        []string{"platformPermissions", "serviceAccess", "roles"},
		EmptyBehavior:      "delete",
		Freshness:          "auto",
		Lanes:              []string{"default"},
		StaticEmptyColumns: []string{"ephemeralGrants"},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func myTasksDesc(t *testing.T) projection.OutputDescriptor {
	t.Helper()
	d, err := projection.ParseOutputDescriptor(&lens.OutputDescriptorSpec{
		AnchorType:       "identity",
		OutputKeyPattern: "my-tasks.{actorSuffix}",
		BodyColumns:      []string{"openTasks"},
		EmptyBehavior:    "delete",
		RealnessFilter:   "taskKey",
		Freshness:        "auto",
		ActorField:       "assignee",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

const actor = "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"

// TestDriver_PrimaryCapability_Shape asserts the primary cap.<actor> envelope
// carries the §6.2 shape: lanes, the always-empty ephemeralGrants, the three
// body columns, and actor (not assignee). It never deletes on an empty body
// (no realness filter).
func TestDriver_PrimaryCapability_Shape(t *testing.T) {
	fn := capDesc(t).EnvelopeFn("vtx.meta.cap", func(string) uint64 { return 9 })
	row := map[string]any{
		"actorKey":            actor,
		"platformPermissions": []any{map[string]any{"operationType": "read", "scope": "any"}},
		"serviceAccess":       []any{},
		"roles":               []any{"vtx.role.r1"},
	}
	env, keys, err := fn(row, nil, map[string]any{"projectedAt": "2026-05-15T10:00:00Z"})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["key"] != "cap.identity.Hj4kPmRtw9nbCxz5vQ2y" {
		t.Fatalf("key: %v", env["key"])
	}
	if keys["key"] != env["key"] {
		t.Fatalf("keys mirror: %v", keys)
	}
	if env["actor"] != actor {
		t.Fatalf("actor: %v", env["actor"])
	}
	if env["version"] != "1.0" {
		t.Fatalf("version: %v", env["version"])
	}
	lanes, ok := env["lanes"].([]string)
	if !ok || len(lanes) != 1 || lanes[0] != "default" {
		t.Fatalf("lanes: %v", env["lanes"])
	}
	eg, ok := env["ephemeralGrants"].([]any)
	if !ok || len(eg) != 0 {
		t.Fatalf("ephemeralGrants must be an always-empty array; got %v", env["ephemeralGrants"])
	}
	if _, ok := env["platformPermissions"]; !ok {
		t.Fatalf("platformPermissions missing")
	}
	revs, ok := env["projectedFromRevisions"].(map[string]uint64)
	if !ok {
		t.Fatalf("projectedFromRevisions type: %T", env["projectedFromRevisions"])
	}
	if revs[actor] == 0 || revs["vtx.meta.cap"] == 0 {
		t.Fatalf("projectedFromRevisions must include anchor + lens-def: %v", revs)
	}
}

// TestDriver_Ephemeral_RealGrant_Projects asserts a real grant projects with the
// cap.ephemeral.<actor> key and the ephemeralGrants body, actor field, no lanes.
func TestDriver_Ephemeral_RealGrant_Projects(t *testing.T) {
	fn := ephemeralDesc(t).EnvelopeFn("vtx.meta.eph", func(string) uint64 { return 0 })
	row := map[string]any{
		"actorKey": actor,
		"ephemeralGrants": []any{
			map[string]any{"taskKey": "vtx.task.t1", "operationType": "Approve"},
		},
	}
	env, keys, err := fn(row, nil, map[string]any{"projectedAt": "t"})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["key"] != "cap.ephemeral.identity.Hj4kPmRtw9nbCxz5vQ2y" {
		t.Fatalf("key: %v", env["key"])
	}
	if keys["key"] != env["key"] {
		t.Fatalf("keys mirror")
	}
	if env["actor"] != actor {
		t.Fatalf("actor field: %v", env["actor"])
	}
	if _, hasLanes := env["lanes"]; hasLanes {
		t.Fatalf("ephemeral doc must not carry lanes")
	}
	if _, hasEph := env["ephemeralGrants"].([]any); !hasEph {
		t.Fatalf("ephemeralGrants missing")
	}
}

// TestDriver_Ephemeral_NoRealGrants_Deletes asserts a degenerate null-taskKey
// collect (no real grants) drives ErrDeleteProjection keyed at the actor's key.
func TestDriver_Ephemeral_NoRealGrants_Deletes(t *testing.T) {
	fn := ephemeralDesc(t).EnvelopeFn("vtx.meta.eph", func(string) uint64 { return 0 })
	row := map[string]any{
		"actorKey":        actor,
		"ephemeralGrants": []any{map[string]any{"taskKey": nil}},
	}
	_, keys, err := fn(row, nil, map[string]any{"projectedAt": "t"})
	if !errors.Is(err, pipeline.ErrDeleteProjection) {
		t.Fatalf("expected ErrDeleteProjection, got %v", err)
	}
	if keys["key"] != "cap.ephemeral.identity.Hj4kPmRtw9nbCxz5vQ2y" {
		t.Fatalf("delete key: %v", keys["key"])
	}
}

// TestDriver_MyTasks_NullRowActor_FallsBackToParams asserts the my-tasks lens's
// last-task-closed path: a null row actorKey falls back to params["actorKey"] so
// the key is deleted (not skipped).
func TestDriver_MyTasks_NullRowActor_FallsBackToParams(t *testing.T) {
	fn := myTasksDesc(t).EnvelopeFn("vtx.meta.mt", func(string) uint64 { return 0 })
	row := map[string]any{
		"actorKey":  nil,
		"openTasks": []any{map[string]any{"taskKey": nil}},
	}
	_, keys, err := fn(row, nil, map[string]any{"actorKey": actor, "projectedAt": "t"})
	if !errors.Is(err, pipeline.ErrDeleteProjection) {
		t.Fatalf("expected ErrDeleteProjection, got %v", err)
	}
	if keys["key"] != "my-tasks.identity.Hj4kPmRtw9nbCxz5vQ2y" {
		t.Fatalf("delete key: %v", keys["key"])
	}
}

// TestDriver_MyTasks_OpenTask_Projects asserts the assignee field + my-tasks key.
func TestDriver_MyTasks_OpenTask_Projects(t *testing.T) {
	fn := myTasksDesc(t).EnvelopeFn("vtx.meta.mt", func(string) uint64 { return 0 })
	row := map[string]any{
		"actorKey":  actor,
		"openTasks": []any{map[string]any{"taskKey": "vtx.task.t1"}},
	}
	env, _, err := fn(row, nil, map[string]any{"actorKey": actor, "projectedAt": "t"})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["assignee"] != actor {
		t.Fatalf("my-tasks doc must carry assignee: %v", env)
	}
	if _, hasActor := env["actor"]; hasActor {
		t.Fatalf("my-tasks doc must not carry actor (uses assignee)")
	}
}

// TestDriver_SkipsNonIdentityAnchor asserts a non-identity anchor is declined.
func TestDriver_SkipsNonIdentityAnchor(t *testing.T) {
	fn := ephemeralDesc(t).EnvelopeFn("vtx.meta.eph", func(string) uint64 { return 0 })
	row := map[string]any{"actorKey": "vtx.role.Hj4kPmRtw9nbCxz5vQ2y", "ephemeralGrants": []any{}}
	_, _, err := fn(row, nil, map[string]any{"projectedAt": "t"})
	if !errors.Is(err, pipeline.ErrSkipProjection) {
		t.Fatalf("expected ErrSkipProjection, got %v", err)
	}
}
