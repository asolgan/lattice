package main

import "testing"

// The Reveal sealed-aspect detection tier (F12 increment 2): isSealedAspect
// gates the Reveal button in the Graph explorer, so a false positive/negative
// here is a real information-disclosure or availability bug, not cosmetic —
// asserted against the shipped embedded asset via the goja harness.

func TestIsSealedAspect(t *testing.T) {
	vm := logicVM(t, "sensitive.js")

	if call(t, vm, "isSealedAspect", nil) != false {
		t.Error("nil data = not sealed")
	}
	if call(t, vm, "isSealedAspect", map[string]any{}) != false {
		t.Error("empty object = not sealed")
	}
	if call(t, vm, "isSealedAspect", map[string]any{"value": "plain PII"}) != false {
		t.Error("ordinary plaintext aspect data = not sealed")
	}
	if call(t, vm, "isSealedAspect", map[string]any{"ct": "", "nonce": "bbb", "keyId": "k1"}) != false {
		t.Error("empty ct = not sealed")
	}
	if call(t, vm, "isSealedAspect", map[string]any{"ct": "aaa", "nonce": "bbb"}) != false {
		t.Error("missing keyId = not sealed")
	}
	if call(t, vm, "isSealedAspect", []any{"aaa", "bbb"}) != false {
		t.Error("an array is never a sealed envelope")
	}
	if call(t, vm, "isSealedAspect", map[string]any{"ct": "aaa", "nonce": "bbb", "keyId": "k1"}) != true {
		t.Error("a full { ct, nonce, keyId } envelope = sealed")
	}
}

func TestSealedSummary(t *testing.T) {
	vm := logicVM(t, "sensitive.js")

	if got := call(t, vm, "sealedSummary", map[string]any{"ct": "aaa", "nonce": "bbb", "keyId": "short-id"}); got != "encrypted at rest · short-id" {
		t.Errorf("short keyId = %v", got)
	}
	if got := call(t, vm, "sealedSummary", map[string]any{"ct": "aaa", "nonce": "bbb", "keyId": "a-very-long-key-identifier-string"}); got != "encrypted at rest · a-very-l…" {
		t.Errorf("long keyId = %v, want truncated", got)
	}
	if got := call(t, vm, "sealedSummary", nil); got != "encrypted at rest · ?" {
		t.Errorf("nil data = %v", got)
	}
}
