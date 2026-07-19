package main

import "testing"

// F20.2 §7.2 — the affordance-suppression decision, against the shipped
// embedded asset. The governing assertion is that the client never restates
// the server's read-only classification: it reads it off /api/demo, and any
// shape it does not recognize hides the button rather than showing one whose
// only outcome is a 403.

func TestDemoPostureOnRequiresExplicitTrue(t *testing.T) {
	vm := logicVM(t, "demo.js")

	for _, p := range []any{
		nil,
		map[string]any{},
		map[string]any{"demoMode": false},
		map[string]any{"demoMode": "true"},
		map[string]any{"demoMode": 1},
		map[string]any{"error": "request failed: offline"},
	} {
		if got := call(t, vm, "demoPostureOn", p); got != false {
			t.Errorf("demoPostureOn(%v) = %v, want false", p, got)
		}
	}
	if got := call(t, vm, "demoPostureOn", map[string]any{"demoMode": true}); got != true {
		t.Errorf("demoPostureOn({demoMode:true}) = %v, want true", got)
	}
}

// demoPayload is the shape the server actually sends (pinned Go-side by
// TestDemoPayloadCarriesReadOnlyOps).
func demoPayload() map[string]any {
	return map[string]any{
		"demoMode": true,
		"readOnlyControlOps": map[string]any{
			"loom":      []any{"inspect"},
			"weaver":    []any{},
			"refractor": []any{"health", "validate"},
		},
	}
}

func TestDemoControlOpHiddenMatchesServerClassification(t *testing.T) {
	vm := logicVM(t, "demo.js")
	payload := demoPayload()

	// The three inspect-only reads stay on screen — they work in demo mode.
	for _, tc := range []struct{ comp, op string }{
		{"loom", "inspect"},
		{"refractor", "health"},
		{"refractor", "validate"},
	} {
		if got := call(t, vm, "demoControlOpHidden", payload, tc.comp, tc.op); got != false {
			t.Errorf("demoControlOpHidden(%s/%s) = %v, want false (an inspect-only read)", tc.comp, tc.op, got)
		}
	}

	// Everything the server refuses is hidden — including an op name borrowed
	// from a different component's read set.
	for _, tc := range []struct{ comp, op string }{
		{"loom", "pause"},
		{"loom", "resume"},
		{"loom", "health"},
		{"weaver", "disable"},
		{"weaver", "enable"},
		{"weaver", "revoke"},
		{"weaver", "inspect"},
		{"refractor", "rebuild"},
		{"refractor", "pause"},
		{"refractor", "resume"},
		{"refractor", "delete"},
		{"gateway", "anything"}, // a component with no entry at all
	} {
		if got := call(t, vm, "demoControlOpHidden", payload, tc.comp, tc.op); got != true {
			t.Errorf("demoControlOpHidden(%s/%s) = %v, want true (a mutate)", tc.comp, tc.op, got)
		}
	}
}

func TestDemoControlOpHiddenIsInertOutsideDemoMode(t *testing.T) {
	vm := logicVM(t, "demo.js")

	// The ordinary operator console must be untouched: nothing is hidden, and
	// a missing classification never leaks into it.
	for _, p := range []any{
		nil,
		map[string]any{"demoMode": false},
		map[string]any{"demoMode": false, "readOnlyControlOps": map[string]any{"loom": []any{}}},
	} {
		for _, op := range []string{"pause", "inspect", "revoke"} {
			if got := call(t, vm, "demoControlOpHidden", p, "loom", op); got != false {
				t.Errorf("demoControlOpHidden(%v, loom/%s) = %v, want false outside demo mode", p, op, got)
			}
		}
	}
}

func TestDemoControlOpHiddenOmissionDenies(t *testing.T) {
	vm := logicVM(t, "demo.js")

	// In demo mode, any classification shape the client cannot read hides the
	// button. Degrading to "too little shown" keeps the suppression honest;
	// degrading the other way would put a button on screen that can only 403.
	for _, p := range []any{
		map[string]any{"demoMode": true},                                                          // field absent entirely
		map[string]any{"demoMode": true, "readOnlyControlOps": nil},                               // null
		map[string]any{"demoMode": true, "readOnlyControlOps": "loom:inspect"},                    // wrong type
		map[string]any{"demoMode": true, "readOnlyControlOps": map[string]any{}},                  // no components
		map[string]any{"demoMode": true, "readOnlyControlOps": map[string]any{"loom": "inspect"}}, // not a list
		map[string]any{"demoMode": true, "readOnlyControlOps": map[string]any{"loom": nil}},
	} {
		if got := call(t, vm, "demoControlOpHidden", p, "loom", "inspect"); got != true {
			t.Errorf("demoControlOpHidden(%v, loom/inspect) = %v, want true (omission denies)", p, got)
		}
	}
}
