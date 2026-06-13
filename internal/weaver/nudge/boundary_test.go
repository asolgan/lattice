package nudge_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestModuleBoundary_OnlySubstrate enforces the module-boundary rule
// (docs/components/weaver.md Principles): internal/weaver/nudge never imports
// internal/processor, internal/loom, or internal/refractor anywhere in its
// dependency tree.
func TestModuleBoundary_OnlySubstrate(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/asolgan/lattice/internal/weaver/nudge").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	forbidden := []string{
		"github.com/asolgan/lattice/internal/processor",
		"github.com/asolgan/lattice/internal/loom",
		"github.com/asolgan/lattice/internal/refractor",
	}
	for _, line := range strings.Split(string(out), "\n") {
		dep := strings.TrimSpace(line)
		for _, f := range forbidden {
			if dep == f || strings.HasPrefix(dep, f+"/") {
				t.Errorf("internal/weaver/nudge must not import %q (module boundary)", dep)
			}
		}
	}
}

// TestModuleBoundary_NoRawNATS enforces that internal/weaver/nudge carries no
// raw nats.io/jetstream handle of its own — every NATS interaction goes through
// a substrate primitive. DIRECT imports only: substrate itself legitimately
// depends on nats.go transitively, so a transitive check would false-positive.
func TestModuleBoundary_NoRawNATS(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{ join .Imports \"\\n\" }}",
		"github.com/asolgan/lattice/internal/weaver/nudge").Output()
	if err != nil {
		t.Fatalf("go list imports: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		dep := strings.TrimSpace(line)
		if strings.HasPrefix(dep, "github.com/nats-io/") {
			t.Errorf("internal/weaver/nudge must not directly import %q (no raw NATS handle — "+
				"use a substrate primitive)", dep)
		}
	}
}
