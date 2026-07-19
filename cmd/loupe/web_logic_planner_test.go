package main

import (
	"testing"
)

// The Weaver planner-diagnostics logic tier (F18): the contraction roll-up, the
// shadow-comparison rows, the admission pacing line, the effect-mismatch
// counter, and the planner-issue split — asserted against the shipped embedded
// asset via the goja harness.
//
// The recurring assertion across these: a diagnostic the heartbeat OMITS must
// read as unknown (null), never as a clean zero. The Weaver drops
// plannerShadow/contractionTrajectory/the admission counters entirely when
// nothing has been recorded, so a fabricated zero would tell an operator "no
// divergence" when the truth is "no comparison ran".

// numVal reads a JS number regardless of whether goja exported it as an
// integer or a float — an integer-valued ratio (0, 1) comes back as int64.
func numVal(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	default:
		t.Fatalf("value %v (%T) is not a number", v, v)
		return 0
	}
}

func TestContractionRoll(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// Absent / empty / mistyped block → null, not an all-clear zero roll.
	for _, m := range []any{
		map[string]any{},
		nil,
		map[string]any{"contractionTrajectory": map[string]any{}},
		map[string]any{"contractionTrajectory": "diverging"},
		map[string]any{"contractionTrajectory": []any{"a"}},
	} {
		if got := call(t, vm, "contractionRoll", m); got != nil {
			t.Errorf("contractionRoll(%v) = %v, want nil", m, got)
		}
	}

	roll := call(t, vm, "contractionRoll", map[string]any{
		"contractionTrajectory": map[string]any{
			"t-zeta":  "diverging",
			"t-alpha": "diverging",
			"t-beta":  "shrinking",
			"t-gamma": "steady",
			// An unrecognized classification buckets as steady (the least
			// alarming default) rather than vanishing from the total.
			"t-delta": "wobbling",
		},
	}).(map[string]any)

	if numVal(t, roll["total"]) != 5 {
		t.Errorf("total = %v, want 5", roll["total"])
	}
	if numVal(t, roll["steady"]) != 2 {
		t.Errorf("steady = %v, want 2 (steady + unrecognized)", roll["steady"])
	}
	div := roll["diverging"].([]any)
	if len(div) != 2 || div[0] != "t-alpha" || div[1] != "t-zeta" {
		t.Errorf("diverging = %v, want sorted [t-alpha t-zeta]", div)
	}
	if shr := roll["shrinking"].([]any); len(shr) != 1 || shr[0] != "t-beta" {
		t.Errorf("shrinking = %v, want [t-beta]", shr)
	}
}

func TestDivergenceRate(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// No comparisons at all → null ("—"), never a green 0%.
	if got := call(t, vm, "divergenceRate", 0, 0); got != nil {
		t.Errorf("divergenceRate(0,0) = %v, want nil", got)
	}
	if got := numVal(t, call(t, vm, "divergenceRate", 3, 1)); got != 0.25 {
		t.Errorf("divergenceRate(3,1) = %v, want 0.25", got)
	}
	if got := numVal(t, call(t, vm, "divergenceRate", 0, 4)); got != 1 {
		t.Errorf("divergenceRate(0,4) = %v, want 1", got)
	}
}

func TestShadowRows(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// Absent block → null: no target declares shadow mode, which is not the
	// same claim as "every comparison agreed".
	for _, m := range []any{
		map[string]any{}, nil,
		map[string]any{"plannerShadow": map[string]any{}},
		map[string]any{"plannerShadow": []any{}},
	} {
		if got := call(t, vm, "shadowRows", m); got != nil {
			t.Errorf("shadowRows(%v) = %v, want nil", m, got)
		}
	}

	rows := call(t, vm, "shadowRows", map[string]any{
		"plannerShadow": map[string]any{
			"t-quiet": map[string]any{"agree": 10, "diverge": 0},
			"t-loud": map[string]any{
				"agree": 1, "diverge": 9,
				"recentDivergences": []any{
					map[string]any{"gapColumn": "gap_a", "entityId": "e1", "pickedRef": "act.x", "actualRef": "act.y", "at": "2026-07-19T01:00:00Z"},
					map[string]any{"gapColumn": "gap_b", "entityId": "e2", "pickedRef": "act.p", "actualRef": "act.q", "at": "2026-07-19T02:00:00Z"},
				},
			},
			// Same diverge count as t-loud's neighbour tier but a lower rate —
			// exercises the rate tie-break below the count tie-break.
			"t-mid": map[string]any{"agree": 100, "diverge": 9},
		},
	}).([]any)

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	first := rows[0].(map[string]any)
	if first["targetId"] != "t-loud" {
		t.Errorf("worst-first ordering put %v first, want t-loud", first["targetId"])
	}
	if rows[1].(map[string]any)["targetId"] != "t-mid" {
		t.Errorf("second row = %v, want t-mid (same diverge count, lower rate)",
			rows[1].(map[string]any)["targetId"])
	}
	if last := rows[2].(map[string]any); last["targetId"] != "t-quiet" || numVal(t, last["rate"]) != 0 {
		t.Errorf("last row = %+v, want t-quiet at rate 0", last)
	}

	// The divergence ring renders newest-first.
	recent := first["recent"].([]any)
	if len(recent) != 2 {
		t.Fatalf("recent = %d, want 2", len(recent))
	}
	if got := recent[0].(map[string]any)["gapColumn"]; got != "gap_b" {
		t.Errorf("newest divergence = %v, want gap_b", got)
	}

	// A missing pick/actual reads as "(none)" — the shadow comparison records
	// an empty actualRef when the gap carried no explicit action, and that must
	// not render as a blank cell an operator misreads as a missing field.
	rows = call(t, vm, "shadowRows", map[string]any{
		"plannerShadow": map[string]any{
			"t": map[string]any{"agree": 0, "diverge": 1,
				"recentDivergences": []any{map[string]any{"gapColumn": "g"}}},
		},
	}).([]any)
	d := rows[0].(map[string]any)["recent"].([]any)[0].(map[string]any)
	if d["pickedRef"] != "(none)" || d["actualRef"] != "(none)" || d["entityId"] != "?" {
		t.Errorf("sparse divergence = %+v, want (none)/(none)/?", d)
	}
}

func TestAdmissionState(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// Neither counter → null: no target declares an admission block, which is
	// not "nothing was deferred".
	for _, m := range []any{map[string]any{}, nil, map[string]any{"admissionAdmitted": "7"}} {
		if got := call(t, vm, "admissionState", m); got != nil {
			t.Errorf("admissionState(%v) = %v, want nil", m, got)
		}
	}

	a := call(t, vm, "admissionState", map[string]any{
		"admissionAdmitted": 90, "admissionDeferred": 10,
	}).(map[string]any)
	if numVal(t, a["rate"]) != 0.1 || a["cls"] != "ok" {
		t.Errorf("10%% deferred = %+v, want 0.1/ok", a)
	}

	// At the 20% floor the pacing line yellows.
	a = call(t, vm, "admissionState", map[string]any{
		"admissionAdmitted": 80, "admissionDeferred": 20,
	}).(map[string]any)
	if a["cls"] != "warn" {
		t.Errorf("20%% deferred = %+v, want warn", a)
	}

	// One counter present, the other absent → the present one still reports
	// (the Weaver emits both only when at least one is nonzero).
	a = call(t, vm, "admissionState", map[string]any{"admissionDeferred": 5}).(map[string]any)
	if numVal(t, a["admitted"]) != 0 || numVal(t, a["deferred"]) != 5 || a["cls"] != "warn" {
		t.Errorf("deferred-only = %+v, want 0 admitted / 5 deferred / warn", a)
	}
}

func TestEffectMismatchCount(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// Absent counter means the scan FAILED this tick (the Weaver logs and skips
	// the key) — null, so the panel says "scan unavailable", not "0 mismatches".
	if got := call(t, vm, "effectMismatchCount", map[string]any{}); got != nil {
		t.Errorf("absent counter = %v, want nil", got)
	}
	if got := numVal(t, call(t, vm, "effectMismatchCount", map[string]any{"effectMismatches": 0})); got != 0 {
		t.Errorf("reported zero = %v, want 0 (a real all-clear)", got)
	}
	if got := numVal(t, call(t, vm, "effectMismatchCount", map[string]any{"effectMismatches": 3})); got != 3 {
		t.Errorf("count = %v, want 3", got)
	}
}

func TestPlannerIssues(t *testing.T) {
	vm := logicVM(t, "planner.js")

	got := call(t, vm, "plannerIssues", map[string]any{
		"issues": []any{
			map[string]any{"code": "TargetOscillation", "severity": "error",
				"message": "targets a and b are alternately dispatching against subject.s.data.f", "since": "T1"},
			map[string]any{"code": "LensEffectMismatch", "severity": "warning", "message": "target t gap g"},
			// Non-planner issues stay out of this panel — they already render
			// on the instance card.
			map[string]any{"code": "ConsumerPaused", "severity": "warning", "message": "paused"},
		},
	}).(map[string]any)

	osc := got["oscillation"].([]any)
	if len(osc) != 1 {
		t.Fatalf("oscillation = %d, want 1", len(osc))
	}
	// The Weaver's message is passed through verbatim — the panel never
	// re-parses the target pair back out of it.
	if o := osc[0].(map[string]any); o["since"] != "T1" ||
		o["message"] != "targets a and b are alternately dispatching against subject.s.data.f" {
		t.Errorf("oscillation row = %+v, want verbatim message + since", o)
	}
	if em := got["effectMismatch"].([]any); len(em) != 1 {
		t.Errorf("effectMismatch = %d, want 1 (ConsumerPaused excluded)", len(em))
	}

	// A doc with no issues array (or a mistyped one) yields empty buckets, not a throw.
	for _, doc := range []any{map[string]any{}, nil, map[string]any{"issues": "none"}} {
		res := call(t, vm, "plannerIssues", doc).(map[string]any)
		if len(res["oscillation"].([]any)) != 0 || len(res["effectMismatch"].([]any)) != 0 {
			t.Errorf("plannerIssues(%v) = %+v, want empty buckets", doc, res)
		}
	}
}

func TestPlannerPanelActive(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// A kernel-only Weaver with no planned/shadow target reports the counter at
	// zero and omits every optional block — the panel must collapse to its
	// quiet "nothing recorded" line rather than render five empty boxes.
	p := call(t, vm, "plannerPanel", map[string]any{
		"instance": "w1",
		"doc":      map[string]any{"metrics": map[string]any{"targets": 4, "effectMismatches": 0}},
	}).(map[string]any)
	if p["active"].(bool) {
		t.Errorf("idle planner panel = %+v, want active false", p)
	}
	if p["instance"] != "w1" {
		t.Errorf("instance = %v, want w1", p["instance"])
	}

	// A reported-zero mismatch counter alone is not activity, but a nonzero one is.
	p = call(t, vm, "plannerPanel", map[string]any{
		"doc": map[string]any{"metrics": map[string]any{"effectMismatches": 2}},
	}).(map[string]any)
	if !p["active"].(bool) {
		t.Errorf("nonzero mismatches = %+v, want active", p)
	}

	// An oscillation issue alone activates the panel even with no metrics block —
	// the loudest planner state must never be gated on a metric being present.
	p = call(t, vm, "plannerPanel", map[string]any{
		"doc": map[string]any{"issues": []any{
			map[string]any{"code": "TargetOscillation", "severity": "error", "message": "m"}}},
	}).(map[string]any)
	if !p["active"].(bool) {
		t.Errorf("oscillation-only panel = %+v, want active", p)
	}
	// With no metrics block at all the mismatch counter is unknown, not zero.
	if p["effectMismatches"] != nil {
		t.Errorf("effectMismatches = %v, want nil (no metrics block)", p["effectMismatches"])
	}

	// A malformed instance must not throw the whole page.
	if got := call(t, vm, "plannerPanel", nil).(map[string]any); got["active"].(bool) {
		t.Errorf("plannerPanel(nil) = %+v, want inactive", got)
	}
}

func TestPlannerPanelsPerInstance(t *testing.T) {
	vm := logicVM(t, "planner.js")

	// Two instances are never merged: the shadow counters are per-process
	// in-memory state, so a summed "diverge" would be a number no Weaver holds.
	panels := call(t, vm, "plannerPanels", []any{
		map[string]any{"instance": "w1", "doc": map[string]any{"metrics": map[string]any{
			"plannerShadow": map[string]any{"t": map[string]any{"agree": 1, "diverge": 2}}}}},
		map[string]any{"instance": "w2", "doc": map[string]any{"metrics": map[string]any{
			"plannerShadow": map[string]any{"t": map[string]any{"agree": 5, "diverge": 0}}}}},
	}).([]any)

	if len(panels) != 2 {
		t.Fatalf("panels = %d, want 2 (one per instance, never merged)", len(panels))
	}
	w1 := panels[0].(map[string]any)["shadow"].([]any)[0].(map[string]any)
	w2 := panels[1].(map[string]any)["shadow"].([]any)[0].(map[string]any)
	if numVal(t, w1["diverge"]) != 2 || numVal(t, w2["diverge"]) != 0 {
		t.Errorf("per-instance diverge = %v / %v, want 2 / 0", w1["diverge"], w2["diverge"])
	}

	if got := call(t, vm, "plannerPanels", nil).([]any); len(got) != 0 {
		t.Errorf("plannerPanels(nil) = %v, want empty", got)
	}
}
