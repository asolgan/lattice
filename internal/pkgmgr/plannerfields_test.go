package pkgmgr

import (
	"encoding/json"
	"strings"
	"testing"
)

// goalTargetDef builds a single-target Definition around one gap's Goal/
// GoalColumns/Actions, mirroring augurTargetDef's helper-per-fixture style
// (orchestrationguard_test.go).
func goalTargetDef(ga GapActionSpec) Definition {
	return Definition{
		WeaverTargets: []WeaverTargetSpec{{
			TargetID: "renewalComplete",
			LensRef:  "renewalComplete",
			Mode:     targetModePlanned,
			Gaps:     map[string]GapActionSpec{"missing_renewalComplete": ga},
		}},
	}
}

func TestValidateWeaverTargets_NoActionAndNoGoalRejected(t *testing.T) {
	err := goalTargetDef(GapActionSpec{}).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "has no action and no goal") {
		t.Fatalf("expected rejection for a gap with neither action nor goal, got: %v", err)
	}
}

func TestValidateWeaverTargets_ModeRejectsUnknown(t *testing.T) {
	def := Definition{WeaverTargets: []WeaverTargetSpec{{
		TargetID: "t1", Mode: "bogus",
		Gaps: map[string]GapActionSpec{"missing_x": {Action: "directOp", Operation: "Op"}},
	}}}
	err := def.validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "not a known planner mode") {
		t.Fatalf("expected unknown-mode rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ModeShadowAndPlannedAccepted(t *testing.T) {
	for _, mode := range []string{"", targetModeShadow, targetModePlanned} {
		def := Definition{WeaverTargets: []WeaverTargetSpec{{
			TargetID: "t1", Mode: mode,
			Gaps: map[string]GapActionSpec{"missing_x": {Action: "directOp", Operation: "Op"}},
		}}}
		if err := def.validateWeaverTargets(); err != nil {
			t.Fatalf("mode %q: expected accept, got: %v", mode, err)
		}
	}
}

// TestValidateWeaverTargets_GoalAuthoredRenewal_Valid exercises the shape the
// R1 design's renewal target actually needs: a root-mapped fact
// (bgcheckValidUntil), a goalColumns-bridged aspect fact (signature.signedAt),
// and the terminal-leg rule (signRenewal's pre entails the goal remainder).
func TestValidateWeaverTargets_GoalAuthoredRenewal_Valid(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"allOf":[
			{"present":"subject.data.bgcheckValidUntil"},
			{"present":"subject.signature.data.signedAt"}
		]}`),
		GoalColumns: map[string]string{
			"signedAt": "subject.signature.data.signedAt",
		},
		Actions: []ActionCatalogEntrySpec{
			{
				Ref: "refreshBgcheck", Action: "triggerLoom", Pattern: "backgroundCheck", Subject: "row.tenant",
				Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.bgcheckValidUntil"}`)},
			},
			{
				Ref: "signRenewal", Action: "assignTask", Operation: "SignRenewal", Assignee: "row.tenant", Target: "row.entityKey",
				Pre:     json.RawMessage(`{"present":"subject.data.bgcheckValidUntil"}`),
				Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.signature.data.signedAt"}`)},
				Cost:    1,
			},
		},
	}
	if err := goalTargetDef(ga).validateWeaverTargets(); err != nil {
		t.Fatalf("expected valid goal-authored target to pass, got: %v", err)
	}
}

func TestValidateWeaverTargets_GoalWithoutActionsRejected(t *testing.T) {
	ga := GapActionSpec{Goal: json.RawMessage(`{"present":"subject.data.x"}`)}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "actions is empty") {
		t.Fatalf("expected rejection for goal-without-actions, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionsWithoutGoalRejected(t *testing.T) {
	// An explicit top-level Action is set so the gap clears the "has no
	// action and no goal" gate and the failure isolates to the
	// actions-without-goal check specifically.
	ga := GapActionSpec{
		Action:    "directOp",
		Operation: "Fallback",
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "goal is empty") {
		t.Fatalf("expected rejection for actions-without-goal, got: %v", err)
	}
}

func TestValidateWeaverTargets_GoalMalformedRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"bogusKind":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "goal:") {
		t.Fatalf("expected malformed-goal rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_GoalColumns_RootShapedRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal:        json.RawMessage(`{"present":"subject.data.x"}`),
		GoalColumns: map[string]string{"x": "subject.data.x"},
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "root-shaped") {
		t.Fatalf("expected root-shaped goalColumns rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_GoalColumns_UnreferencedByGoalRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal:        json.RawMessage(`{"present":"subject.data.other"}`),
		GoalColumns: map[string]string{"unused": "subject.aspectA.data.field"},
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.other"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "never referenced by goal") {
		t.Fatalf("expected unreferenced-goalColumns rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_GoalColumns_DuplicatePathRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"allOf":[{"present":"subject.aspectA.data.field"},{"present":"subject.aspectA.data.field"}]}`),
		GoalColumns: map[string]string{
			"colA": "subject.aspectA.data.field",
			"colB": "subject.aspectA.data.field",
		},
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.aspectA.data.field"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "both map to path") {
		t.Fatalf("expected duplicate-path goalColumns rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionRefEmptyRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Action: "directOp", Operation: "Op", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "has no ref") {
		t.Fatalf("expected empty-ref rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionRefDuplicateRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op1", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
			{Ref: "a", Action: "directOp", Operation: "Op2", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "declared more than once") {
		t.Fatalf("expected duplicate-ref rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionEmptyActionNameRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "has no action") {
		t.Fatalf("expected empty-action rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionNegativeCostRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op", Cost: -1,
				Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "must be >= 0") {
		t.Fatalf("expected negative-cost rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionNoEffectsRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op"},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "has no effects") {
		t.Fatalf("expected no-effects rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_ActionEffectsAnyOfRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op",
				Effects: []json.RawMessage{json.RawMessage(`{"anyOf":[{"present":"subject.data.x"},{"present":"subject.data.y"}]}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil {
		t.Fatal("expected anyOf-effect rejection (concrete assertions only), got nil")
	}
}

func TestValidateWeaverTargets_ActionEffectsMalformedRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op",
				Effects: []json.RawMessage{json.RawMessage(`{"bogusKind":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "effects[0]") {
		t.Fatalf("expected malformed-effects rejection, got: %v", err)
	}
}

// TestValidateWeaverTargets_UnreachableAspectEffectRejected proves the R1
// row-reachability rule: an effect addressing an aspect path this gap's
// goalColumns never bridges could never be seen as satisfied by a row-derived
// State, so it must reject at install rather than silently never-satisfy at
// dispatch.
func TestValidateWeaverTargets_UnreachableAspectEffectRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op",
				Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.unbridgedAspect.data.field"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "not bridged by this gap's goalColumns") {
		t.Fatalf("expected unreachable-effect rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_UnreachableAspectPreRejected(t *testing.T) {
	ga := GapActionSpec{
		Goal: json.RawMessage(`{"present":"subject.data.x"}`),
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op",
				Pre:     json.RawMessage(`{"present":"subject.unbridgedAspect.data.field"}`),
				Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.data.x"}`)}},
		},
	}
	err := goalTargetDef(ga).validateWeaverTargets()
	if err == nil || !strings.Contains(err.Error(), "not bridged by this gap's goalColumns") {
		t.Fatalf("expected unreachable-pre rejection, got: %v", err)
	}
}

func TestValidateWeaverTargets_BridgedAspectEffectAccepted(t *testing.T) {
	ga := GapActionSpec{
		Goal:        json.RawMessage(`{"present":"subject.aspectA.data.field"}`),
		GoalColumns: map[string]string{"field": "subject.aspectA.data.field"},
		Actions: []ActionCatalogEntrySpec{
			{Ref: "a", Action: "directOp", Operation: "Op",
				Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.aspectA.data.field"}`)}},
		},
	}
	if err := goalTargetDef(ga).validateWeaverTargets(); err != nil {
		t.Fatalf("expected bridged aspect effect to be accepted, got: %v", err)
	}
}

// TestWeaverTargetSpecBody_EmitsGoalAuthoringFields proves the emitted body
// round-trips the R1 authoring surface into the engine's expected JSON shape
// (mode/goal/goalColumns/actions), byte-verifiable against the engine's own
// Target/GapAction/ActionCatalogEntry json tags (internal/weaver/registry.go).
func TestWeaverTargetSpecBody_EmitsGoalAuthoringFields(t *testing.T) {
	spec := WeaverTargetSpec{
		TargetID: "renewalComplete",
		Mode:     targetModePlanned,
		Gaps: map[string]GapActionSpec{
			"missing_renewalComplete": {
				Goal:        json.RawMessage(`{"present":"subject.aspectA.data.field"}`),
				GoalColumns: map[string]string{"field": "subject.aspectA.data.field"},
				Actions: []ActionCatalogEntrySpec{
					{Ref: "a", Action: "directOp", Operation: "Op", Cost: 2,
						Effects: []json.RawMessage{json.RawMessage(`{"present":"subject.aspectA.data.field"}`)}},
				},
			},
		},
	}
	body := weaverTargetSpecBody(spec, "lensNanoID")
	if body["mode"] != targetModePlanned {
		t.Fatalf("expected mode %q in body, got: %v", targetModePlanned, body["mode"])
	}
	gaps, ok := body["gaps"].(map[string]any)
	if !ok {
		t.Fatalf("expected gaps map in body, got: %T", body["gaps"])
	}
	gaBody, ok := gaps["missing_renewalComplete"].(map[string]any)
	if !ok {
		t.Fatalf("expected gap body map, got: %T", gaps["missing_renewalComplete"])
	}
	if _, ok := gaBody["goal"]; !ok {
		t.Fatal("expected goal in emitted gap body")
	}
	cols, ok := gaBody["goalColumns"].(map[string]any)
	if !ok || cols["field"] != "subject.aspectA.data.field" {
		t.Fatalf("expected goalColumns round-tripped, got: %v", gaBody["goalColumns"])
	}
	actions, ok := gaBody["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("expected one emitted action entry, got: %v", gaBody["actions"])
	}
	entry, ok := actions[0].(map[string]any)
	if !ok || entry["ref"] != "a" || entry["cost"] != 2 {
		t.Fatalf("expected entry ref/cost round-tripped, got: %v", entry)
	}
}

func TestWeaverTargetSpecBody_OmitsModeWhenEmpty(t *testing.T) {
	spec := WeaverTargetSpec{TargetID: "t1", Gaps: map[string]GapActionSpec{}}
	body := weaverTargetSpecBody(spec, "")
	if _, ok := body["mode"]; ok {
		t.Fatal("expected no mode key when Mode is empty")
	}
}
