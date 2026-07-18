package cafedomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the package's permission vertices + grants. Every op
// keeps its orchestrator-submitted grant (the trusted-tool app — POS /
// front-desk FE — submits OpenTab when a resident's visit starts, Charge per
// rung-up item, and Settle at checkout). OpenTab and Settle ALSO grant
// `consumer`, scope=self (real-actor-write-auth-e2e idiom, clinic-domain's
// CreateAppointment/wellness-domain's CreateBooking precedent): a resident
// may start or close their OWN house tab. `authContext.target == actor` is
// checked at step 3 (Contract #6); the Starlark script separately requires
// the tab's lease to be identified-by that target identity (via the lease's
// applicationFor link, lease-signing's own convergence-link shape) — the
// same patient/identifiedBy indirection clinic-domain uses, since a café tab
// is anchored to a lease, not an identity, directly. Charge stays
// operator-only: it takes a raw amountCents with no catalog/menu to bound a
// self-submitted charge against, unlike CreateBooking/CreateAppointment
// which book an already-existing, provider-defined slot.
func Permissions() []pkgmgr.PermissionSpec {
	return []pkgmgr.PermissionSpec{
		{
			OperationType: "OpenTab",
			Scope:         "any",
			Note:          "Grants the operator the right to submit OpenTab (starts a café house-tab session for a resident lease).",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "OpenTab",
			Scope:         "self",
			Note:          "Grants a consumer the right to open a house tab for THEMSELVES (the tab's lease must be identified-by the caller's own identity).",
			GrantsTo:      []string{"consumer"},
		},
		{
			OperationType: "Charge",
			Scope:         "any",
			Note:          "Grants the operator the right to submit Charge (rings up an item on an open tab).",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "Settle",
			Scope:         "any",
			Note:          "Grants the operator the right to submit Settle (closes a tab for house-account posting).",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "Settle",
			Scope:         "self",
			Note:          "Grants a consumer the right to settle THEIR OWN house tab (the tab's lease must be identified-by the caller's own identity).",
			GrantsTo:      []string{"consumer"},
		},
	}
}
