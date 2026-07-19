package main

import "testing"

// The demo banner's shaping (F20) against the shipped embedded asset. The
// governing assertion: only an explicit demoMode:true renders a banner — a
// failed or malformed /api/demo read must leave the ordinary console
// unbannered rather than invent a posture from a missing field.

func TestDemoBannerOnlyOnExplicitTrue(t *testing.T) {
	vm := logicVM(t, "demo.js")

	for _, payload := range []any{
		nil,
		map[string]any{},
		map[string]any{"demoMode": false},
		map[string]any{"demoMode": "true"},
		map[string]any{"demoMode": 1},
		map[string]any{"error": "request failed: offline"},
	} {
		if got := call(t, vm, "demoBanner", payload); got != nil {
			t.Errorf("demoBanner(%v) = %v, want nil", payload, got)
		}
	}
}

func TestDemoBannerUsesServerNotice(t *testing.T) {
	vm := logicVM(t, "demo.js")

	got, ok := call(t, vm, "demoBanner", map[string]any{
		"demoMode": true,
		"notice":   "reads only, writes refused",
	}).(map[string]any)
	if !ok {
		t.Fatal("demoBanner should return an object for demoMode:true")
	}
	if got["text"] != "reads only, writes refused" {
		t.Errorf("text = %v, want the server's own notice (so the banner and the 403 agree)", got["text"])
	}
	if got["title"] == "" {
		t.Error("banner needs a title")
	}

	// No/blank notice → the built-in copy, never an empty banner.
	for _, p := range []map[string]any{
		{"demoMode": true},
		{"demoMode": true, "notice": "   "},
		{"demoMode": true, "notice": 42},
	} {
		b, ok := call(t, vm, "demoBanner", p).(map[string]any)
		if !ok {
			t.Fatalf("demoBanner(%v) should still return a banner", p)
		}
		if s, _ := b["text"].(string); s == "" {
			t.Errorf("demoBanner(%v) produced an empty banner body", p)
		}
	}
}

// The title already says "Read-only demo", so the server notice's matching
// lead-in is dropped rather than rendered twice in a row.
func TestDemoBannerDropsDuplicateLeadIn(t *testing.T) {
	vm := logicVM(t, "demo.js")
	for _, notice := range []string{
		"read-only demo: this console accepts reads only",
		"Read-Only Demo:   this console accepts reads only",
	} {
		b := call(t, vm, "demoBanner", map[string]any{"demoMode": true, "notice": notice}).(map[string]any)
		if b["text"] != "this console accepts reads only" {
			t.Errorf("demoBanner(%q) text = %v, want the lead-in stripped", notice, b["text"])
		}
	}
	// A notice that merely mentions the phrase mid-sentence keeps it.
	b := call(t, vm, "demoBanner", map[string]any{"demoMode": true, "notice": "you are in a read-only demo: relax"}).(map[string]any)
	if b["text"] != "you are in a read-only demo: relax" {
		t.Errorf("only a LEADING duplicate should be stripped, got %v", b["text"])
	}
}
