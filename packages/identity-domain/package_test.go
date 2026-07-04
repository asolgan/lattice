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

func TestPackage_DeclaresUserFacingRoles(t *testing.T) {
	want := map[string]bool{"consumer": true, "frontOfHouse": true, "backOfHouse": true}
	if got := len(Package.Roles); got != len(want) {
		t.Fatalf("expected %d declared roles, got %d", len(want), got)
	}
	for _, r := range Package.Roles {
		if !want[r.CanonicalName] {
			t.Errorf("unexpected role %q", r.CanonicalName)
		}
		if r.Description == "" {
			t.Errorf("role %q missing description", r.CanonicalName)
		}
	}
}

func TestPackage_DDLsAndOps(t *testing.T) {
	if got := len(Package.DDLs); got != 11 {
		t.Fatalf("expected 11 DDLs (identity + ssn, dob, name, email, phone, claimKey, credentialBinding + "+
			"actorRevocation, gateway.actorRevoked, gateway.actorUnrevoked), got %d", got)
	}
	identity := ddlByCanonicalName(t, "identity")
	if identity.Class != "meta.ddl.vertexType" {
		t.Fatalf("identity DDL class = %q, want meta.ddl.vertexType", identity.Class)
	}
	if got := len(identity.PermittedCommands); got != 4 {
		t.Fatalf("identity permittedCommands: got %d, want 4 "+
			"(CreateUnclaimedIdentity, UpdateIdentityState, ClaimIdentity, RecordIdentityPII)", got)
	}
}

// TestPackage_SensitivePIIAspectTypes pins the ssn/dob aspect-type DDLs as
// sensitive=true — the structural declaration that makes the step-6 validator
// anchor them to identity vertices (lattice-architecture Item 6 / NFR-S3).
// ssn/dob are written only by RecordIdentityPII, so they pin
// permittedCommands:[RecordIdentityPII].
func TestPackage_SensitivePIIAspectTypes(t *testing.T) {
	for _, name := range []string{"ssn", "dob"} {
		d := ddlByCanonicalName(t, name)
		if d.Class != "meta.ddl.aspectType" {
			t.Errorf("%s DDL class = %q, want meta.ddl.aspectType", name, d.Class)
		}
		if !d.Sensitive {
			t.Errorf("%s DDL Sensitive = false, want true", name)
		}
		if got := d.PermittedCommands; len(got) != 1 || got[0] != "RecordIdentityPII" {
			t.Errorf("%s DDL permittedCommands = %v, want [RecordIdentityPII]", name, got)
		}
	}
}

// TestPackage_LifecyclePIIAspectTypesSensitive pins the name/email/phone/
// claimKey/credentialBinding aspect-type DDLs as sensitive=true with EMPTY
// permittedCommands. They are written by multiple ops across packages
// (CreateUnclaimedIdentity, ClaimIdentity, and identity-hygiene's
// MergeIdentity), so a non-empty permittedCommands would make step-6 reject a
// legitimate writer (e.g. MergeIdentity writing name) — identity-anchoring is
// their only enforcement.
func TestPackage_LifecyclePIIAspectTypesSensitive(t *testing.T) {
	for _, name := range []string{"name", "email", "phone", "claimKey", "credentialBinding"} {
		d := ddlByCanonicalName(t, name)
		if d.Class != "meta.ddl.aspectType" {
			t.Errorf("%s DDL class = %q, want meta.ddl.aspectType", name, d.Class)
		}
		if !d.Sensitive {
			t.Errorf("%s DDL Sensitive = false, want true", name)
		}
		if got := len(d.PermittedCommands); got != 0 {
			t.Errorf("%s DDL permittedCommands = %v, want empty (multiple writers across packages)", name, d.PermittedCommands)
		}
	}
}

func TestPackage_ScriptUsesOnlyKnownKeyReads(t *testing.T) {
	for _, d := range Package.DDLs {
		for _, forbidden := range []string{"KVListKeys", "list_keys", "keys_with_prefix"} {
			if strings.Contains(d.Script, forbidden) {
				t.Errorf("DDL %q script must not reference prefix-scan helper %q", d.CanonicalName, forbidden)
			}
		}
	}
}

// ddlByCanonicalName returns the DDLSpec with the given canonicalName, failing
// the test if absent.
func ddlByCanonicalName(t *testing.T, name string) pkgmgr.DDLSpec {
	t.Helper()
	for _, d := range Package.DDLs {
		if d.CanonicalName == name {
			return d
		}
	}
	t.Fatalf("no DDL with canonicalName %q", name)
	return pkgmgr.DDLSpec{}
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
