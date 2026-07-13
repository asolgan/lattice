package controlauthz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asolgan/lattice/internal/controlauth"
	"github.com/asolgan/lattice/internal/pkgmgr"
)

// TestPackage_ManifestMatchesDefinition confirms the on-disk manifest.yaml
// cross-validates against the exported Definition (the most common package-
// authoring drift bug), before any NATS integration.
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

// TestPackage_GrantOnlyNoDDLsOrLenses pins the package's shape: a grant-only
// package (mirrors packages/privacy-operator-grant).
func TestPackage_GrantOnlyNoDDLsOrLenses(t *testing.T) {
	if len(Package.DDLs) != 0 || len(Package.Lenses) != 0 {
		t.Fatalf("grant-only package must declare no DDLs/lenses; got %d DDLs, %d lenses",
			len(Package.DDLs), len(Package.Lenses))
	}
	deps := map[string]bool{}
	for _, d := range Package.Depends {
		deps[d] = true
	}
	for _, want := range []string{"rbac-domain", "identity-domain"} {
		if !deps[want] {
			t.Errorf("Depends = %v, want to include %q", Package.Depends, want)
		}
	}
}

// TestPackage_DeclaresControlOperatorRoleDistinctFromPrimordialOperator pins
// the naming decision the package.go doc explains at length: the new role is
// "control-operator", never "operator" (the kernel-primordial, root-equivalent
// role every SystemActorKeys() holder is discovered by).
func TestPackage_DeclaresControlOperatorRoleDistinctFromPrimordialOperator(t *testing.T) {
	if len(Package.Roles) != 1 {
		t.Fatalf("Roles = %d, want exactly 1", len(Package.Roles))
	}
	role := Package.Roles[0]
	if role.CanonicalName != "control-operator" {
		t.Fatalf("role CanonicalName = %q, want %q (must not collide with the primordial %q role)",
			role.CanonicalName, "control-operator", "operator")
	}
}

// TestPackage_EveryControlOpHasExpectedGrantees pins the full 15-permission
// ctrl.<component>.<verb> surface (4 weaver + 3 loom + 8 refractor —
// internal/controlauth's WeaverOps/LoomOps/RefractorOps): every op grants
// scope=any, and every op grants to control-operator ALONE except the three
// identity-bound Personal Lens ops (register/deregister/hydrate), which
// additionally grant to consumer (§3.4-confined — see personalLensPermissions).
func TestPackage_EveryControlOpHasExpectedGrantees(t *testing.T) {
	wantSoleControlOperator := []string{
		"ctrl.weaver.read", "ctrl.weaver.disable", "ctrl.weaver.enable", "ctrl.weaver.revoke",
		"ctrl.loom.read", "ctrl.loom.pause", "ctrl.loom.resume",
		"ctrl.refractor.read", "ctrl.refractor.rebuild", "ctrl.refractor.pause", "ctrl.refractor.resume",
		"ctrl.refractor.delete",
	}
	wantControlOperatorAndConsumer := []string{
		"ctrl.refractor.register", "ctrl.refractor.deregister", "ctrl.refractor.hydrate",
	}
	if got, want := len(Package.Permissions), len(wantSoleControlOperator)+len(wantControlOperatorAndConsumer); got != want {
		t.Fatalf("Permissions = %d, want %d", got, want)
	}
	got := make(map[string]pkgmgr.PermissionSpec, len(Package.Permissions))
	for _, p := range Package.Permissions {
		got[p.OperationType] = p
	}
	for _, op := range wantSoleControlOperator {
		p, ok := got[op]
		if !ok {
			t.Errorf("missing permission %q", op)
			continue
		}
		if p.Scope != "any" {
			t.Errorf("%s: scope = %q, want any", op, p.Scope)
		}
		if len(p.GrantsTo) != 1 || p.GrantsTo[0] != "control-operator" {
			t.Errorf("%s: GrantsTo = %v, want [control-operator]", op, p.GrantsTo)
		}
	}
	for _, op := range wantControlOperatorAndConsumer {
		p, ok := got[op]
		if !ok {
			t.Errorf("missing permission %q", op)
			continue
		}
		if p.Scope != "any" {
			t.Errorf("%s: scope = %q, want any", op, p.Scope)
		}
		grants := map[string]bool{}
		for _, g := range p.GrantsTo {
			grants[g] = true
		}
		if len(p.GrantsTo) != 2 || !grants["control-operator"] || !grants["consumer"] {
			t.Errorf("%s: GrantsTo = %v, want [control-operator consumer]", op, p.GrantsTo)
		}
	}
}

// TestPackage_GrantedVerbsMatchControlauthOpTables is the drift guard the
// permissions.go doc comment names but doesn't enforce: it derives the
// expected ctrl.<component>.<verb> set DIRECTLY from
// internal/controlauth's WeaverOps/LoomOps/RefractorOps (the source of
// truth, read off each control service's real dispatch table) instead of a
// second hand-maintained literal, so a future op added to one table and
// forgotten in the other fails HERE — as a missing grant (an op nobody can
// ever be authorized for) or an orphaned grant (a permission no op checks) —
// rather than surfacing only at runtime as a silent permanent-deny.
func TestPackage_GrantedVerbsMatchControlauthOpTables(t *testing.T) {
	wantByComponent := map[string]map[string]controlauth.OpMeta{
		"weaver":    controlauth.WeaverOps,
		"loom":      controlauth.LoomOps,
		"refractor": controlauth.RefractorOps,
	}
	wantVerbs := map[string]struct{}{}
	for component, ops := range wantByComponent {
		seenVerbs := map[string]struct{}{}
		for _, meta := range ops {
			seenVerbs[meta.Verb] = struct{}{}
		}
		for verb := range seenVerbs {
			wantVerbs["ctrl."+component+"."+verb] = struct{}{}
		}
	}

	gotVerbs := map[string]struct{}{}
	for _, p := range Package.Permissions {
		gotVerbs[p.OperationType] = struct{}{}
	}

	for op := range wantVerbs {
		if _, ok := gotVerbs[op]; !ok {
			t.Errorf("internal/controlauth declares an op requiring verb %q, but control-authz grants no such permission (a permanent, ungrantable deny)", op)
		}
	}
	for op := range gotVerbs {
		if _, ok := wantVerbs[op]; !ok {
			t.Errorf("control-authz grants %q, but no internal/controlauth op table resolves to that verb (an orphaned, dead grant)", op)
		}
	}
}
