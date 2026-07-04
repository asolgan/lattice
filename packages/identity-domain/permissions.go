package identitydomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the identity-domain permission vertices.
//
// Grant matrix:
//
//	CreateUnclaimedIdentity → frontOfHouse, backOfHouse, operator
//	UpdateIdentityState     → operator
//	ClaimIdentity (self)    → consumer
//	RecordIdentityPII       → frontOfHouse, backOfHouse, operator
//
// Scope `self` for ClaimIdentity: platformPermissions[] match is
// exact-operationType only; scope enforcement happens in the Starlark
// `ClaimIdentity` branch (one-credential-one-identity via credentialindex).
func Permissions() []pkgmgr.PermissionSpec {
	perms := []pkgmgr.PermissionSpec{
		{
			OperationType: "CreateUnclaimedIdentity",
			Scope:         "any",
			Note:          "Grants the right to create an unclaimed identity vertex.",
			GrantsTo:      []string{"frontOfHouse", "backOfHouse", "operator"},
		},
		{
			OperationType: "UpdateIdentityState",
			Scope:         "any",
			Note:          "Grants the right to advance an identity through its state machine.",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "ClaimIdentity",
			Scope:         "self",
			Note:          "Grants the right to claim an identity (scope=self via credentialindex).",
			GrantsTo:      []string{"consumer"},
		},
		{
			OperationType: "RecordIdentityPII",
			Scope:         "any",
			Note:          "Grants the right to record applicant PII (ssn/dob sensitive aspects) on an existing identity.",
			GrantsTo:      []string{"frontOfHouse", "backOfHouse", "operator"},
		},
	}
	return append(perms, RevocationPermissions()...)
}
