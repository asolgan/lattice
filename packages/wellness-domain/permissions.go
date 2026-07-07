package wellnessdomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions grants every wellness-domain op to the `operator` role (scope
// any). Mirrors clinic-domain / cafe-domain exactly: the trusted-tool
// operator already holds standing permission, no new capability surface.
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
		mk("CreateStudio"),
		mk("TombstoneStudio"),
		mk("CreateSession"),
		mk("TombstoneSession"),
		mk("CreateBooking"),
		mk("CancelBooking"),
	}
}
