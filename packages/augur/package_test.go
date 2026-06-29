package augur_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asolgan/lattice/internal/pkgmgr"
	"github.com/asolgan/lattice/packages/augur"
)

// TestPackage_ManifestMatchesDefinition catches drift between manifest.yaml and
// the in-code Definition (DDL / lens / permission counts + canonicalNames). The
// installer parses the manifest for the install plan; a mismatch would install a
// shape the package author did not declare.
func TestPackage_ManifestMatchesDefinition(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	m, err := pkgmgr.ParseManifest(filepath.Join(wd, "manifest.yaml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := m.VerifyAgainstDefinition(augur.Package); err != nil {
		t.Fatalf("manifest <-> Definition drift: %v", err)
	}
}

// TestPackage_AugurProposalsLens pins the read-model review surface: exactly one
// lens, a FLAT (no ProjectionKind) nats-kv projection into the augur-proposals
// bucket, and — the trusted-tool posture — NOT protected and NOT a weaver-target
// convergence lens. (The lens cypher itself is exercised end-to-end by the
// install path in the integration tests + the live verify-package-augur run.)
func TestPackage_AugurProposalsLens(t *testing.T) {
	lenses := augur.Lenses()
	if len(lenses) != 1 {
		t.Fatalf("want exactly 1 lens, got %d", len(lenses))
	}
	l := lenses[0]
	if l.CanonicalName != "augurProposals" {
		t.Errorf("CanonicalName = %q, want augurProposals", l.CanonicalName)
	}
	if l.Class != "meta.lens" {
		t.Errorf("Class = %q, want meta.lens", l.Class)
	}
	if l.Adapter != "nats-kv" {
		t.Errorf("Adapter = %q, want nats-kv", l.Adapter)
	}
	if augur.AugurProposalsBucket != "augur-proposals" {
		t.Errorf("AugurProposalsBucket = %q, want augur-proposals", augur.AugurProposalsBucket)
	}
	if l.Bucket != augur.AugurProposalsBucket {
		t.Errorf("Bucket = %q, want %q", l.Bucket, augur.AugurProposalsBucket)
	}
	if l.Engine != "full" {
		t.Errorf("Engine = %q, want full", l.Engine)
	}
	if l.Protected {
		t.Error("read-model review lens must NOT be protected (trusted-tool posture)")
	}
	if l.ProjectionKind != "" {
		t.Errorf("ProjectionKind = %q, want empty (flat one-row-per-proposal projection)", l.ProjectionKind)
	}
	if l.Output != nil {
		t.Error("flat read-model lens carries no actorAggregate OutputDescriptor")
	}

	// The Package definition wires the lens.
	if got := len(augur.Package.Lenses); got != 1 {
		t.Fatalf("Package.Lenses count = %d, want 1", got)
	}
	if augur.Package.Lenses[0].CanonicalName != "augurProposals" {
		t.Errorf("Package.Lenses[0] = %q, want augurProposals", augur.Package.Lenses[0].CanonicalName)
	}
}
