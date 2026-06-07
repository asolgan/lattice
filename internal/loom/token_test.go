package loom

import (
	"testing"

	"github.com/asolgan/lattice/internal/substrate"
)

// TestDeriveRequestID_DeterministicAndValid proves the write-ahead token is a
// valid Contract #1 NanoID and is stable for a given (instanceId, cursor) —
// the property that makes systemOp re-attempt idempotent (AC #6).
func TestDeriveRequestID_DeterministicAndValid(t *testing.T) {
	id, err := substrate.NewNanoID()
	if err != nil {
		t.Fatal(err)
	}
	a := deriveRequestID(id, 0)
	b := deriveRequestID(id, 0)
	if a != b {
		t.Fatalf("deriveRequestID not deterministic: %q != %q", a, b)
	}
	if !substrate.IsValidNanoID(a) {
		t.Fatalf("deriveRequestID produced invalid NanoID: %q", a)
	}
	// Different cursors must produce different tokens.
	if deriveRequestID(id, 0) == deriveRequestID(id, 1) {
		t.Fatal("cursor 0 and 1 produced the same token")
	}
	// Different instances must produce different tokens.
	id2, _ := substrate.NewNanoID()
	if deriveRequestID(id, 0) == deriveRequestID(id2, 0) {
		t.Fatal("distinct instances produced the same token")
	}
}
