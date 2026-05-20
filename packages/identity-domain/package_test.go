package identitydomain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asolgan/lattice/internal/pkgmgr"
)

func TestPackage_ManifestMatchesDefinition(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	m, err := pkgmgr.ParseManifest(filepath.Join(wd, "manifest.yaml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := m.VerifyAgainstDefinition(Package); err != nil {
		t.Fatalf("manifest <-> Definition drift: %v", err)
	}
}

func TestPackage_HasPreInstallHook(t *testing.T) {
	if Package.PreInstall == nil {
		t.Fatal("identity-domain Package must declare a PreInstall hook to seed user-facing roles")
	}
}

func TestPackage_ThreeOps(t *testing.T) {
	if got := len(Package.DDLs); got != 1 {
		t.Fatalf("expected 1 DDL, got %d", got)
	}
	if got := len(Package.DDLs[0].PermittedCommands); got != 3 {
		t.Fatalf("permittedCommands: got %d, want 3", got)
	}
}

func TestPackage_ScriptUsesOnlyKnownKeyReads(t *testing.T) {
	src := Package.DDLs[0].Script
	for _, forbidden := range []string{"KVListKeys", "list_keys", "keys_with_prefix"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("script must not reference prefix-scan helper %q", forbidden)
		}
	}
}

func TestPackage_DependsOnRbacDomain(t *testing.T) {
	found := false
	for _, d := range Package.Depends {
		if d == "rbac-domain" {
			found = true
		}
	}
	if !found {
		t.Error("identity-domain must declare rbac-domain as a dependency")
	}
}
