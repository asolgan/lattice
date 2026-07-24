// Per-operation context threading for the resolved Capability KV permission
// entry. The resolved permission is carried on the Decision struct and threaded
// through steps 4-10 as a local variable on the HandleMessage stack frame —
// no context.Value indirection needed for a value scoped to a single goroutine.
// Strictly internal: never bleed into OperationEnvelope or OperationReply.
package processor

// ResolvedPermission is the auth path + matched entry pointers chosen at
// step 3. The pointers are into the parsed CapabilityDoc; lifecycle is
// the commit-path goroutine handling one envelope, so escape concerns
// are scoped to a single operation.
type ResolvedPermission struct {
	// CapKey is the Capability KV key that backed this decision.
	CapKey string
	// ProjectedAt echoes the doc's projection timestamp — observability /
	// denial response can include this without re-reading the doc.
	ProjectedAt string
	// Path is one of "platform" / "service" / "task" — the dispatch
	// branch that matched. Empty when no match (denial).
	Path string
	// Exactly one of the three is non-nil on success.
	PlatformPermission *PlatformPermission
	ServiceAccess      *ServiceAccessEntry
	AllowedOperation   *AllowedOperation
	EphemeralGrant     *EphemeralGrant
}

// authTargetValidated reports whether the resolved grant actually validated
// env.AuthContext.Target against the actor or a minted grant, making it safe to
// forward to the operation script as op.authContextTarget. Only two authorized
// paths bind the target: a platform scope=self grant (step 3 requires
// target == actor) and a task/ephemeralGrant (matchEphemeralGrant matches
// g.Target == ac.Target). The platform scope=any and service paths never inspect
// target, so a target arriving through them is unvalidated client input a
// scope=any holder can forge into a script's self/workplace exemption. Fail
// closed: a nil permission or an unrecognized path is treated as unvalidated.
func authTargetValidated(rp *ResolvedPermission) bool {
	if rp == nil {
		return false
	}
	switch rp.Path {
	case "platform":
		return rp.PlatformPermission != nil && rp.PlatformPermission.Scope == "self"
	case "task":
		return rp.EphemeralGrant != nil
	default:
		return false
	}
}
