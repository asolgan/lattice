package servicelocation

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the eight permission vertices + their grants. Every
// link op is granted to the `operator` role (scope any) — the residence /
// availability topology is operator-provisioned. The role canonical name
// `operator` is resolved by cmd/lattice-pkg to the seeded NanoID from
// lattice.bootstrap.json.
func Permissions() []pkgmgr.PermissionSpec {
	mk := func(op string) pkgmgr.PermissionSpec {
		return pkgmgr.PermissionSpec{
			OperationType: op,
			Scope:         "any",
			Note:          "Grants the operator the right to submit " + op + " operations.",
			GrantsTo:      []string{"operator"},
		}
	}
	return []pkgmgr.PermissionSpec{
		mk("WireResidesIn"),
		mk("UnwireResidesIn"),
		mk("WireAvailableAt"),
		mk("UnwireAvailableAt"),
		mk("WireUnavailableAt"),
		mk("UnwireUnavailableAt"),
		mk("WirePermitsOperation"),
		mk("UnwirePermitsOperation"),
	}
}
