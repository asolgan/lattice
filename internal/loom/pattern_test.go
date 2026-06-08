package loom

import (
	"encoding/json"
	"testing"
)

func TestPatternValidate_AcceptsSystemOpAndUserTask_RejectsGuardsAndUnknownKinds(t *testing.T) {
	cases := []struct {
		name    string
		pattern Pattern
		wantErr bool
	}{
		{
			name:    "valid two-systemOp pattern",
			pattern: Pattern{PatternID: "p1", SubjectType: "identity", Steps: []Step{{Kind: "systemOp", Operation: "SetName"}, {Kind: "systemOp", Operation: "SetPhone"}}},
			wantErr: false,
		},
		{
			name:    "userTask accepted",
			pattern: Pattern{PatternID: "p2", SubjectType: "identity", Steps: []Step{{Kind: "userTask", Operation: "SetName"}}},
			wantErr: false,
		},
		{
			name:    "userTask without operation rejected",
			pattern: Pattern{PatternID: "p2b", SubjectType: "identity", Steps: []Step{{Kind: "userTask", Operation: ""}}},
			wantErr: true,
		},
		{
			name:    "unknown kind rejected",
			pattern: Pattern{PatternID: "p2c", SubjectType: "identity", Steps: []Step{{Kind: "decision", Operation: "X"}}},
			wantErr: true,
		},
		{
			name:    "guarded systemOp rejected",
			pattern: Pattern{PatternID: "p3", SubjectType: "identity", Steps: []Step{{Kind: "systemOp", Operation: "SetName", Guard: json.RawMessage(`{"absent":"subject.data.name"}`)}}},
			wantErr: true,
		},
		{
			name:    "guarded userTask rejected",
			pattern: Pattern{PatternID: "p3b", SubjectType: "identity", Steps: []Step{{Kind: "userTask", Operation: "SetName", Guard: json.RawMessage(`{"absent":"subject.data.name"}`)}}},
			wantErr: true,
		},
		{
			name:    "empty subjectType rejected",
			pattern: Pattern{PatternID: "p4", Steps: []Step{{Kind: "systemOp", Operation: "SetName"}}},
			wantErr: true,
		},
		{
			name:    "no steps rejected",
			pattern: Pattern{PatternID: "p5", SubjectType: "identity"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.pattern.validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestPatternDomains_DefaultsToSubjectTypeWhenOmitted(t *testing.T) {
	p := Pattern{SubjectType: "identity"}
	got := p.Domains()
	if len(got) != 1 || got[0] != "identity" {
		t.Fatalf("Domains() with no completionDomains = %v, want [identity]", got)
	}
}

func TestPatternDomains_UsesDeclaredSetVerbatim(t *testing.T) {
	// When completionDomains is present it is used as-is (NOT unioned with
	// subjectType): a cross-domain flow lists exactly the domains it completes on.
	p := Pattern{SubjectType: "identity", CompletionDomains: []string{"org", "org", " "}}
	got := p.Domains()
	if len(got) != 1 || got[0] != "org" {
		t.Fatalf("Domains()=%v, want [org] (declared set, deduped, subjectType not unioned)", got)
	}
}

func TestBindingRegistry_DedupesDomainsAcrossPatterns(t *testing.T) {
	patterns := []*Pattern{
		{SubjectType: "identity", Steps: []Step{{Kind: "systemOp", Operation: "A"}}},
		{SubjectType: "identity", CompletionDomains: []string{"org"}},
		{SubjectType: "lease"},
	}
	got := bindingRegistry(patterns)
	for _, d := range []string{"identity", "org", "lease"} {
		if _, ok := got[d]; !ok {
			t.Fatalf("expected domain %q in registry %v", d, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("registry should dedupe to 3 domains, got %d: %v", len(got), got)
	}
}

func TestUserTaskCompletionUnobservable(t *testing.T) {
	cases := []struct {
		name string
		p    Pattern
		want bool
	}{
		{
			name: "userTask omitting orchestration domain is unobservable",
			p: Pattern{SubjectType: "identity", CompletionDomains: []string{"identity"},
				Steps: []Step{{Kind: StepKindUserTask, Operation: "SetName"}}},
			want: true,
		},
		{
			name: "userTask defaulting to subjectType (no orchestration domain) is unobservable",
			p: Pattern{SubjectType: "identity",
				Steps: []Step{{Kind: StepKindUserTask, Operation: "SetName"}}},
			want: true,
		},
		{
			name: "userTask listing orchestration domain is observable",
			p: Pattern{SubjectType: "identity", CompletionDomains: []string{"orchestration"},
				Steps: []Step{{Kind: StepKindUserTask, Operation: "SetName"}}},
			want: false,
		},
		{
			name: "systemOp-only pattern is never flagged",
			p: Pattern{SubjectType: "identity", CompletionDomains: []string{"identity"},
				Steps: []Step{{Kind: StepKindSystemOp, Operation: "StepA"}}},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.userTaskCompletionUnobservable(); got != c.want {
				t.Fatalf("userTaskCompletionUnobservable()=%v, want %v", got, c.want)
			}
		})
	}
}

func TestPatternIDFromRef(t *testing.T) {
	if got := patternIDFromRef("vtx.meta.abc"); got != "abc" {
		t.Fatalf("patternIDFromRef(vtx.meta.abc)=%q, want abc", got)
	}
	if got := patternIDFromRef("abc"); got != "abc" {
		t.Fatalf("patternIDFromRef(abc)=%q, want abc", got)
	}
}
