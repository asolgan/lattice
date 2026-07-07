// Package consoleoperator is the scoped console-operator Capability Package
// (loupe-operator-auth-lift-design.md mechanism B,
// scoped-privileged-lane-grants-design.md §3.4 "consoleOperator stays an
// ordinary actor"). It declares a `consoleOperator` role and grants it the
// default-lane console ops (ShredIdentityKey, RevokeActor, UnrevokeActor,
// AttachObject, DetachObject) plus the ctrl.<component>.<verb> control-plane
// ops — everything a Loupe operator needs for routine console use, at zero
// privileged-lane exposure.
//
// `consoleOperator` is a package-installed role deliberately DISTINCT from
// the kernel-primordial `operator` role (`vtx.role.<RoleOperatorID>`,
// canonicalName "operator" — internal/bootstrap/primordial.go): granting to
// the primordial role would make every console operator indistinguishable
// from root-equivalent kernel-meta privilege, and `bootstrap.SystemActorKeys`
// specifically discovers holders of THAT role to route them to the
// system-actor union read — a scoped console identity must stay an ordinary
// (non-system) actor so it reads cap.roles.<actor> alone, at the `default`
// lane only, exactly like any other rbac-projected grant. This also removes
// the boot-snapshot dependency the primordial `operator` anchor carries: a
// consoleOperator seeded after Processor boot authorizes immediately.
//
// Package installers cannot seed an identity vertex or a holdsRole link
// (only Definition.Roles/Permissions — role + permission + grantedBy data —
// are installer-native); provisioning the actual console-operator identity
// (or re-scoping Loupe's existing seeded operator identity onto this role)
// is a separate Loupe-lane wiring step
// (loupe-operator-auth-lift-design.md §7 decomposition items 4-6), not this
// package's scope. This package ships the grant data only.
//
// The pkg-lifecycle ops (InstallPackage/UninstallPackage/UpgradePackage) are
// deliberately NOT granted here — those stay meta-lane, anchor-only (root)
// per mechanism B; scoped-privileged-lane-grants-design.md (mechanism C) is
// the follow-on that would let consoleOperator run them without root.
//
// Install via `lattice-pkg install packages/console-operator` (after
// rbac-domain, identity-domain, privacy-base, objects-base). No DDLs or
// lenses — a grant-only package (mirrors packages/control-authz's shape).
package consoleoperator

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:        "console-operator",
	Version:     "0.1.0",
	Description: "Grants a scoped consoleOperator role the default-lane console ops + ctrl.* control-plane ops, without root.",
	Depends:     []string{"rbac-domain", "identity-domain", "privacy-base", "objects-base"},
	Permissions: Permissions(),
	Roles: []pkgmgr.RoleSpec{
		{
			CanonicalName: "consoleOperator",
			Description:   "Scoped Loupe console operator: default-lane console ops (shred/revoke/object) + ctrl.* control-plane ops. Not root — no privileged lane, no anchor.",
		},
	},
}
