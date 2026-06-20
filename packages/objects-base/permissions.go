package objectsbase

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the package's permission vertices + grants.
//
// All three ops are operator-driven: Loupe (the trusted single-identity client)
// submits AttachObject / DetachObject as admin, and the v1b GC's reclaimObject
// Loom pattern submits TombstoneObject as the operator-equivalent Weaver/Loom
// service actor. So the grants go to `operator` only (scope: any) — the same
// operator-grant idiom service-domain / orchestration-base use for their
// lifecycle ops. Tightening to additional roles later is purely additive.
func Permissions() []pkgmgr.PermissionSpec {
	return []pkgmgr.PermissionSpec{
		{
			OperationType: "AttachObject",
			Scope:         "any",
			Note:          "Grants the operator the right to submit AttachObject operations.",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "DetachObject",
			Scope:         "any",
			Note:          "Grants the operator the right to submit DetachObject operations.",
			GrantsTo:      []string{"operator"},
		},
		{
			OperationType: "TombstoneObject",
			Scope:         "any",
			Note:          "Grants the operator the right to submit TombstoneObject operations (GC-internal: the v1b reclaimObject Loom pattern).",
			GrantsTo:      []string{"operator"},
		},
	}
}
