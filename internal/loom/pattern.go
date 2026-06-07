package loom

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Step kinds (Contract #10 §10.5). Only systemOp is interpreted in the
// Phase-8.1 walking skeleton; userTask arrives with the task stories.
const (
	StepKindSystemOp = "systemOp"
	StepKindUserTask = "userTask"
)

// Step is one entry in a pattern's linear step list (Contract #10 §10.5
// shape `{kind, operation, guard?}`). The walking skeleton interprets
// systemOp steps and carries no guard handling.
type Step struct {
	Kind      string          `json:"kind"`
	Operation string          `json:"operation"`
	Guard     json.RawMessage `json:"guard,omitempty"`
}

// Pattern is the in-engine view of a meta.loomPattern definition. A pattern
// declares a single subjectType (the vertex type an instance runs for) and a
// linear list of steps. patternId is the meta-vertex NanoID.
//
// completionDomains is the set of events.<domain>.> the engine reconciles a
// durable per-domain completion consumer for (D2). It defaults to the pattern's
// subjectType: a pattern over `identity` subjects completes on
// `events.identity.>`. A flow whose steps complete in a domain other than the
// subject's lists it explicitly. The engine reads completionDomains — it does
// not infer domains from operation names; correlation is domain-independent
// (Contract #10 §10.6), so the SET of domains is sufficient.
type Pattern struct {
	PatternID         string   `json:"patternId"`
	SubjectType       string   `json:"subjectType"`
	Steps             []Step   `json:"steps"`
	CompletionDomains []string `json:"completionDomains,omitempty"`
}

// Domains returns the deduped set of completion domains this pattern's systemOp
// steps complete on. A domain is the FIRST segment of an event class (the
// `<domain>` in `events.<domain>.>`), so it is always a single dot-free token
// — the per-domain consumer's durable name (loom-<domain>) requires this.
// Defaults to {subjectType} when completionDomains is omitted; otherwise the
// declared set (each reduced to its first segment) is used verbatim.
func (p *Pattern) Domains() []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(d string) {
		d = firstSegment(strings.TrimSpace(d))
		if d == "" {
			return
		}
		if _, ok := seen[d]; ok {
			return
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	if len(p.CompletionDomains) == 0 {
		add(p.SubjectType)
		return out
	}
	for _, d := range p.CompletionDomains {
		add(d)
	}
	return out
}

// firstSegment returns the part of s before the first dot.
func firstSegment(s string) string {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return s
}

// validate rejects a pattern the walking skeleton cannot run. Guards and
// userTask steps are out of scope for Phase 8.1: a guarded or userTask step
// is rejected so a half-understood pattern never partially executes.
func (p *Pattern) validate() error {
	if strings.TrimSpace(p.SubjectType) == "" {
		return fmt.Errorf("pattern %q: subjectType required", p.PatternID)
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("pattern %q: at least one step required", p.PatternID)
	}
	for i, s := range p.Steps {
		if s.Kind != StepKindSystemOp {
			return fmt.Errorf("pattern %q step %d: kind %q unsupported in Phase 8.1 (systemOp only)",
				p.PatternID, i, s.Kind)
		}
		if strings.TrimSpace(s.Operation) == "" {
			return fmt.Errorf("pattern %q step %d: operation required", p.PatternID, i)
		}
		if len(s.Guard) != 0 {
			return fmt.Errorf("pattern %q step %d: guards are out of scope in Phase 8.1", p.PatternID, i)
		}
	}
	return nil
}

// StartLoomPattern is the payload of the op that triggers a new instance
// (Contract #10 §10.5). subjectKey must be a vertex of the pattern's
// subjectType; patternRef is the meta.loomPattern vertex key
// (vtx.meta.<patternId>) or the bare patternId.
type StartLoomPattern struct {
	PatternRef string `json:"patternRef"`
	SubjectKey string `json:"subjectKey"`
}

// patternIDFromRef accepts either a bare patternId or a vtx.meta.<id> key and
// returns the patternId.
func patternIDFromRef(ref string) string {
	if id, ok := strings.CutPrefix(ref, "vtx.meta."); ok {
		return id
	}
	return ref
}
