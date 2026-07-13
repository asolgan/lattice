package controlauthz

import "github.com/asolgan/lattice/internal/pkgmgr"

// Permissions returns the 15 ctrl.<component>.<verb> platform permissions,
// each granting `scope: any` (v1 — the only working platform scope,
// control-plane-capability-authz-design.md §2(a)) to the control-operator
// role. The op→verb tables here MUST stay in lockstep with
// internal/controlauth's WeaverOps/LoomOps/RefractorOps (both are read off
// the same source: each component's control/service.go dispatch table).
//
// The three Personal Lens ops (register/deregister/hydrate) additionally
// grant to the consumer role — per-identity-nats-subscribe-acl-design.md
// §3.3/§3.4 (Fire 2). Safe only because
// internal/refractor/control/service.go's dispatchEndpoint unconditionally
// binds these ops' body.IdentityID to the caller's own verified actor,
// confining the effect to the caller's own identity regardless of
// capability scope — see personalLensPermissions.
func Permissions() []pkgmgr.PermissionSpec {
	perms := []pkgmgr.PermissionSpec{}
	perms = append(perms, componentPermissions("weaver", []string{"read", "disable", "enable", "revoke"})...)
	perms = append(perms, componentPermissions("loom", []string{"read", "pause", "resume"})...)
	perms = append(perms, componentPermissions("refractor", []string{"read", "rebuild", "pause", "resume", "delete"})...)
	perms = append(perms, personalLensPermissions("register", "deregister", "hydrate")...)
	return perms
}

func componentPermissions(component string, verbs []string) []pkgmgr.PermissionSpec {
	perms := make([]pkgmgr.PermissionSpec, 0, len(verbs))
	for _, verb := range verbs {
		perms = append(perms, pkgmgr.PermissionSpec{
			OperationType: "ctrl." + component + "." + verb,
			Scope:         "any",
			Note:          "Authorizes the control-operator role to invoke the " + component + " control plane's " + verb + " op.",
			GrantsTo:      []string{"control-operator"},
		})
	}
	return perms
}

// personalLensPermissions grants the given refractor verbs to both
// control-operator and consumer — the identity-bound Personal Lens ops
// (§3.4 confines each to the caller's own identity server-side, so a broad
// scope=any grant to consumer never lets one identity act on another's
// interest set).
func personalLensPermissions(verbs ...string) []pkgmgr.PermissionSpec {
	perms := make([]pkgmgr.PermissionSpec, 0, len(verbs))
	for _, verb := range verbs {
		perms = append(perms, pkgmgr.PermissionSpec{
			OperationType: "ctrl.refractor." + verb,
			Scope:         "any",
			Note:          "Authorizes control-operator, or any consumer identity acting on its own Personal Lens interest set (bound server-side, §3.4), to invoke the refractor control plane's " + verb + " op.",
			GrantsTo:      []string{"control-operator", "consumer"},
		})
	}
	return perms
}
