// Package testutil — Story 4.7 helper. After kernel minimization,
// integration tests that previously relied on bootstrap-seeded identity
// + role DDLs now install the rbac-domain + identity-domain +
// identity-hygiene packages on top of a freshly-seeded kernel.
//
// Usage:
//
//	func TestMyIntegrationStuff(t *testing.T) {
//	    ctx, conn := startEmbeddedNATSConnection(t)
//	    bootstrap.SeedPrimordial(ctx, conn)  // kernel-only after 4.7
//	    testutil.InstallPhase1Packages(t, ctx, conn)
//	    // ...
//	}
//
// Idempotent: the installer's per-package presence check skips already-
// installed packages.
package testutil

import (
	"context"
	"testing"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/pkgmgr"
	"github.com/asolgan/lattice/internal/substrate"
	identitydomain "github.com/asolgan/lattice/packages/identity-domain"
	identityhygiene "github.com/asolgan/lattice/packages/identity-hygiene"
	rbacdomain "github.com/asolgan/lattice/packages/rbac-domain"
)

// InstallPhase1Packages installs rbac-domain, identity-domain, and
// identity-hygiene in dependency order against the given substrate
// connection. The caller is responsible for having called
// bootstrap.LoadOrGenerate + bootstrap.SeedPrimordial first so the
// kernel + admin identity exist.
//
// Each install is idempotent; calling this helper twice with the same
// connection is safe.
func InstallPhase1Packages(t *testing.T, ctx context.Context, conn *substrate.Conn) {
	t.Helper()

	inst := pkgmgr.NewInstaller(conn, bootstrap.BootstrapIdentityKey)
	inst.RoleIDs = map[string]string{
		"operator": bootstrap.RoleOperatorID,
	}

	for _, def := range []pkgmgr.Definition{
		rbacdomain.Package,
		identitydomain.Package,
		identityhygiene.Package,
	} {
		if _, err := inst.Install(ctx, def); err != nil {
			t.Fatalf("InstallPhase1Packages: install %s: %v", def.Name, err)
		}
	}
}
