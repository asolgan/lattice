package projection

import (
	"errors"
	"strings"
	"testing"

	"github.com/asolgan/lattice/internal/refractor/lens"
	"github.com/asolgan/lattice/internal/refractor/ruleengine"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
)

// --- Output descriptor parsing / validation (AC4) ---

func validDescriptor() *lens.OutputDescriptorSpec {
	return &lens.OutputDescriptorSpec{
		AnchorType:       "identity",
		OutputKeyPattern: "cap.ephemeral.{actorSuffix}",
		BodyColumns:      []string{"ephemeralGrants"},
		EmptyBehavior:    "delete",
		RealnessFilter:   "taskKey",
		Freshness:        "auto",
	}
}

func TestParseOutputDescriptor_Valid(t *testing.T) {
	d, err := ParseOutputDescriptor(validDescriptor())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.AnchorType != "identity" || d.EmptyBehavior != EmptyDelete {
		t.Fatalf("descriptor mismatch: %+v", d)
	}
	if got := d.BuildKey("vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"); got != "cap.ephemeral.identity.Hj4kPmRtw9nbCxz5vQ2y" {
		t.Fatalf("BuildKey: got %q", got)
	}
}

func TestParseOutputDescriptor_RejectsUnknownPlaceholder(t *testing.T) {
	spec := validDescriptor()
	spec.OutputKeyPattern = "cap.ephemeral.{actorSuffix}.{lensId}"
	_, err := ParseOutputDescriptor(spec)
	if err == nil || !strings.Contains(err.Error(), "unknown placeholder") {
		t.Fatalf("expected unknown-placeholder rejection, got %v", err)
	}
}

func TestParseOutputDescriptor_RejectsMissingActorSuffix(t *testing.T) {
	spec := validDescriptor()
	spec.OutputKeyPattern = "cap.ephemeral.static"
	_, err := ParseOutputDescriptor(spec)
	if err == nil || !strings.Contains(err.Error(), "actorSuffix") {
		t.Fatalf("expected missing-actorSuffix rejection, got %v", err)
	}
}

func TestParseOutputDescriptor_RejectsBadEmptyBehavior(t *testing.T) {
	spec := validDescriptor()
	spec.EmptyBehavior = "purge"
	_, err := ParseOutputDescriptor(spec)
	if err == nil || !strings.Contains(err.Error(), "emptyBehavior") {
		t.Fatalf("expected emptyBehavior rejection, got %v", err)
	}
}

func TestParseOutputDescriptor_RejectsBadFreshness(t *testing.T) {
	spec := validDescriptor()
	spec.Freshness = "manual"
	_, err := ParseOutputDescriptor(spec)
	if err == nil || !strings.Contains(err.Error(), "freshness") {
		t.Fatalf("expected freshness rejection, got %v", err)
	}
}

func TestParseOutputDescriptor_RejectsEmptyBodyColumns(t *testing.T) {
	spec := validDescriptor()
	spec.BodyColumns = nil
	_, err := ParseOutputDescriptor(spec)
	if err == nil || !strings.Contains(err.Error(), "bodyColumns") {
		t.Fatalf("expected bodyColumns rejection, got %v", err)
	}
}

func TestParseOutputDescriptor_NilSpec(t *testing.T) {
	_, err := ParseOutputDescriptor(nil)
	if err == nil {
		t.Fatalf("expected error for nil descriptor")
	}
}

// --- realness filter (AC4) ---

func TestRealnessFiltered(t *testing.T) {
	d := OutputDescriptor{RealnessFilter: "taskKey"}
	in := []any{
		map[string]any{"taskKey": "vtx.task.x", "v": 1},
		map[string]any{"taskKey": nil},                 // degenerate
		map[string]any{"taskKey": ""},                  // degenerate
		map[string]any{"other": "no key"},              // degenerate
	}
	out := d.RealnessFiltered(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 real entry, got %d: %v", len(out), out)
	}
}

func TestRealnessFiltered_NoFilterPassesThrough(t *testing.T) {
	d := OutputDescriptor{}
	in := []any{map[string]any{"a": 1}, map[string]any{"b": 2}}
	if got := d.RealnessFiltered(in); len(got) != 2 {
		t.Fatalf("expected pass-through, got %d", len(got))
	}
}

// A non-string value at the realness field must NOT silently zero the
// projection (over-revocation). A present non-nil value is treated as real and
// kept; only nil / missing / empty / whitespace-only strings drop the entry.
func TestRealnessFiltered_NonStringFieldKept(t *testing.T) {
	d := OutputDescriptor{RealnessFilter: "taskKey"}
	in := []any{
		map[string]any{"taskKey": float64(42)},      // non-string but present → real
		map[string]any{"taskKey": true},             // non-string but present → real
		map[string]any{"taskKey": "vtx.task.x"},     // string non-empty → real
		map[string]any{"taskKey": nil},              // degenerate → dropped
		map[string]any{"taskKey": "   "},            // whitespace string → dropped
		map[string]any{"other": "no realness field"}, // missing → dropped
	}
	out := d.RealnessFiltered(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 real entries (non-string kept, degenerate dropped), got %d: %v", len(out), out)
	}
}

// --- empty behavior → action + tombstone reuse signal (AC7) ---

func TestEmptyAction_Mapping(t *testing.T) {
	cases := map[EmptyBehavior]EmptyAction{
		EmptyDelete:     ActionDelete,
		EmptySoftDelete: ActionSoftDelete,
		EmptyDoc:        ActionWriteEmptyDoc,
		EmptySkip:       ActionSkip,
	}
	for eb, want := range cases {
		got := OutputDescriptor{EmptyBehavior: eb}.EmptyAction()
		if got != want {
			t.Fatalf("emptyBehavior %q → action %v, want %v", eb, got, want)
		}
	}
}

func TestRequiresGuardedTombstone(t *testing.T) {
	if !(OutputDescriptor{EmptyBehavior: EmptySoftDelete}).RequiresGuardedTombstone() {
		t.Fatalf("softDelete must require the guarded tombstone")
	}
	if !(OutputDescriptor{EmptyBehavior: EmptyDelete}).RequiresGuardedTombstone() {
		t.Fatalf("delete on a guarded adapter reuses the same tombstone mechanism")
	}
	if (OutputDescriptor{EmptyBehavior: EmptySkip}).RequiresGuardedTombstone() {
		t.Fatalf("skip must NOT write a tombstone")
	}
}

// --- activation policy: auth vs non-auth fail-closed-vs-warn (AC3e, AC6) ---

type capturingLogger struct{ warned int }

func (l *capturingLogger) Warn(string, ...any) { l.warned++ }

// uncoveredAuthRule builds an actorAggregate auth-plane (capability-kv) rule
// whose MATCH uses an uncovered construct (undirected hop).
func makeRule(t *testing.T, bucket, match string) *lens.Rule {
	t.Helper()
	eng := full.New()
	cr, err := eng.Parse(match)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &lens.Rule{
		ID:             "lens-test",
		CanonicalName:  "testLens",
		Match:          match,
		ProjectionKind: ActorAggregateKind,
		ResolvedEngine: ruleengine.EngineFull,
		CompiledRule:   cr,
		Into:           lens.IntoConfig{Target: "nats_kv", Bucket: bucket, Key: lens.KeyField{"key"}},
		Output:         validDescriptor(),
	}
}

const coveredMatch = `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task)
OPTIONAL MATCH (task)-[:forOperation]->(op)
RETURN identity.key AS actorKey, collect(task.key) AS tasks
`

const uncoveredMatch = `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)-[:assignedTo]-(task:task)
RETURN identity.key AS actorKey, collect(task.key) AS tasks
`

func TestCompile_AuthPlaneUncovered_FailsActivation(t *testing.T) {
	r := makeRule(t, AuthPlaneBucket, uncoveredMatch)
	log := &capturingLogger{}
	_, err := Compile(r, log)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CompileError (fail closed), got %v", err)
	}
	if log.warned != 0 {
		t.Fatalf("auth-plane lens must FAIL, not warn-and-fallback")
	}
}

func TestCompile_NonAuthUncovered_WarnsAndFallsBack(t *testing.T) {
	r := makeRule(t, "my-tasks", uncoveredMatch)
	log := &capturingLogger{}
	plan, err := Compile(r, log)
	if err != nil {
		t.Fatalf("non-auth uncovered must not fail activation: %v", err)
	}
	if !plan.Invalidation.FallbackToBFS {
		t.Fatalf("expected BFS-fallback plan for non-auth uncovered lens")
	}
	if log.warned == 0 {
		t.Fatalf("expected a fallback-to-BFS warning")
	}
}

func TestCompile_CoveredAuthPlane_CompilesForest(t *testing.T) {
	r := makeRule(t, AuthPlaneBucket, coveredMatch)
	plan, err := Compile(r, &capturingLogger{})
	if err != nil {
		t.Fatalf("covered auth-plane lens must compile: %v", err)
	}
	if !plan.AuthPlane {
		t.Fatalf("expected auth-plane classification for capability-kv bucket")
	}
	if plan.Invalidation.FallbackToBFS || plan.Invalidation.Forest == nil {
		t.Fatalf("expected a compiled forest, got fallback")
	}
	if len(plan.Invalidation.Forest.Branches) == 0 {
		t.Fatalf("forest has no branches")
	}
}

func TestCompile_NonActorAggregate_Rejected(t *testing.T) {
	r := makeRule(t, "my-tasks", coveredMatch)
	r.ProjectionKind = ""
	if _, err := Compile(r, nil); err == nil {
		t.Fatalf("expected Compile to reject a non-actorAggregate lens")
	}
}

func TestIsAuthPlane(t *testing.T) {
	if !isAuthPlane(&lens.Rule{Into: lens.IntoConfig{Target: "nats_kv", Bucket: AuthPlaneBucket}}) {
		t.Fatalf("capability-kv bucket must classify as auth-plane")
	}
	if isAuthPlane(&lens.Rule{Into: lens.IntoConfig{Target: "nats_kv", Bucket: "my-tasks"}}) {
		t.Fatalf("my-tasks bucket must NOT be auth-plane")
	}
}

// --- contributing-source provenance widening (AC5) ---

func TestContributingSources_WidensToBoundGraphKeys(t *testing.T) {
	actor := "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"
	lensDef := "vtx.meta.Lk2Pn6mQrtwzKbcXvP3T"
	rows := []map[string]any{
		{
			"actorKey": actor,
			"openTasks": []any{
				map[string]any{
					"taskKey":      "vtx.task.Rm7q3pntwzkfbcxv5p9j",
					"forOperation": "vtx.op.Qp4Nb2mPq6rTwzKxVyP7",
					"scopedTo":     "vtx.lease.Zz9q3pntwzkfbcxv5p9k",
				},
			},
		},
	}
	revs := map[string]uint64{
		actor:                          47,
		lensDef:                        12,
		"vtx.task.Rm7q3pntwzkfbcxv5p9j": 8,
		"vtx.op.Qp4Nb2mPq6rTwzKxVyP7":   3,
		"vtx.lease.Zz9q3pntwzkfbcxv5p9k": 5,
	}
	got := ContributingSources(actor, lensDef, rows, func(k string) uint64 { return revs[k] })

	// v1 must include actor + lens-def + every bound task/op/scopedTo key.
	for k := range revs {
		if _, ok := got[k]; !ok {
			t.Fatalf("contributing-source set missing bound key %q: %v", k, got)
		}
	}
	if got[actor] != 47 || got[lensDef] != 12 {
		t.Fatalf("revisions not stamped from revisionOf: %v", got)
	}
}

func TestContributingSources_OmitsAbsentRevisions(t *testing.T) {
	actor := "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"
	got := ContributingSources(actor, "", nil, func(string) uint64 { return 0 })
	if len(got) != 0 {
		t.Fatalf("expected no entries when revisionOf returns 0, got %v", got)
	}
}
