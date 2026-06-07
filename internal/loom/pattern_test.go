package loom

import (
	"encoding/json"
	"testing"
)

func TestPatternValidate_RejectsNonSystemOpAndGuards(t *testing.T) {
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
			name:    "userTask rejected in 8.1",
			pattern: Pattern{PatternID: "p2", SubjectType: "identity", Steps: []Step{{Kind: "userTask", Operation: "SetName"}}},
			wantErr: true,
		},
		{
			name:    "guard rejected in 8.1",
			pattern: Pattern{PatternID: "p3", SubjectType: "identity", Steps: []Step{{Kind: "systemOp", Operation: "SetName", Guard: json.RawMessage(`{"absent":"subject.data.name"}`)}}},
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

func TestPatternIDFromRef(t *testing.T) {
	if got := patternIDFromRef("vtx.meta.abc"); got != "abc" {
		t.Fatalf("patternIDFromRef(vtx.meta.abc)=%q, want abc", got)
	}
	if got := patternIDFromRef("abc"); got != "abc" {
		t.Fatalf("patternIDFromRef(abc)=%q, want abc", got)
	}
}
