package loom_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestModuleBoundary_OnlySubstrate enforces AC #7/#8: internal/loom never
// imports internal/processor, internal/weaver, or internal/refractor anywhere in
// its dependency tree. The check uses `go list -deps` (transitive).
func TestModuleBoundary_OnlySubstrate(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/asolgan/lattice/internal/loom").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	forbidden := []string{
		"github.com/asolgan/lattice/internal/processor",
		"github.com/asolgan/lattice/internal/weaver",
		"github.com/asolgan/lattice/internal/refractor",
	}
	for _, line := range strings.Split(string(out), "\n") {
		dep := strings.TrimSpace(line)
		for _, f := range forbidden {
			if dep == f || strings.HasPrefix(dep, f+"/") {
				t.Errorf("internal/loom must not import %q (AC #7 module boundary)", dep)
			}
		}
	}
}

// TestModuleBoundary_NoRawNATS enforces AC #8: internal/loom carries no raw
// nats.io/jetstream handle of its own — every NATS interaction goes through a
// substrate primitive (the command-outbox relay publishes via substrate.Publish;
// consumers via RunDurableConsumer/SubscribeKVChanges). The check is on DIRECT
// imports only: substrate itself legitimately depends on nats.go transitively,
// so a transitive (`-deps`) check would false-positive.
func TestModuleBoundary_NoRawNATS(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{ join .Imports \"\\n\" }}",
		"github.com/asolgan/lattice/internal/loom").Output()
	if err != nil {
		t.Fatalf("go list imports: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		dep := strings.TrimSpace(line)
		if strings.HasPrefix(dep, "github.com/nats-io/") {
			t.Errorf("internal/loom must not directly import %q (AC #8: no raw NATS handle — "+
				"use a substrate primitive)", dep)
		}
	}
}
