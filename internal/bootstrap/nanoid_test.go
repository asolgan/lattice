package bootstrap_test

import (
	"path/filepath"
	"testing"

	"github.com/asolgan/lattice/internal/bootstrap"
)

// TestLoad_AbsentFile verifies the strict loader errors when the bootstrap
// file does not exist, rather than minting fresh primordial IDs. Engine
// binaries (cmd/loom, cmd/weaver) rely on this to fail fast instead of
// running with an unrecognized actor identity.
func TestLoad_AbsentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lattice.bootstrap.json")

	if err := bootstrap.Load(path); err == nil {
		t.Fatalf("Load(%s): expected error for absent file, got nil", path)
	}
}
