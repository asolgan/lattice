// Package bootstrap contains primordial bootstrap logic for Story 1.3.
// NanoID generation: inline minimal implementation per the handoff brief decision.
// Uses deterministic FIXED IDs for all primordial entities (not random) because
// make down/make up cycles must produce the same lattice.bootstrap.json across
// runs for reproducibility.  Story 1.4 will introduce internal/substrate.NewNanoID()
// for runtime random IDs; the fixed primordial set is the sole exception.
//
// Fixed IDs follow Contract #1: 20 chars from the custom 58-char alphabet
// (A-Z, a-z, 0-9 minus I, l, O, 0).  They were generated offline and are
// embedded here as named constants so every component can reference them by
// name rather than by string literal.
package bootstrap

// Primordial fixed NanoIDs — 20 chars, custom 58-char alphabet, manually
// verified to contain no I, l, O, or 0.
//
// Decision: fixed IDs rather than generated-at-first-run IDs.
// Rationale: primordial entities are platform-version-pinned; their keys are
// referenced in architecture docs and bypass test oracles.  A stable key
// simplifies grep, avoids a lattice.bootstrap.json divergence hazard, and
// lets downstream stories hard-code expectations in their own test fixtures.
// The tradeoff (two deployments share the same primordial key space) is
// acceptable at Phase 1 single-cell scope (NFR-R6).
const (
	// --- bootstrap operation tracker ---
	// The synthetic op tracker that "authors" all primordial entries.
	BootstrapOpID  = "bsopPrimordialV1aa00"
	BootstrapOpKey = "vtx.op." + BootstrapOpID

	// --- system identity vertices ---
	BootstrapIdentityID  = "bsidPrimordialV1aa01"
	BootstrapIdentityKey = "vtx.identity." + BootstrapIdentityID

	PlatformActorID  = "bsidPlatformActorV1a02"
	PlatformActorKey = "vtx.identity." + PlatformActorID

	// --- root DDL meta-vertex ---
	MetaRootID  = "bsmetaRootDDLV1aa03c"
	MetaRootKey = "vtx.meta." + MetaRootID

	// --- Capability Lens definitions ---
	CapabilityLensID  = "bsmetaCapLensV1aa04d"
	CapabilityLensKey = "vtx.meta." + CapabilityLensID

	CapabilityRoleIndexLensID  = "bsmetaCapRoleLensV105"
	CapabilityRoleIndexLensKey = "vtx.meta." + CapabilityRoleIndexLensID

	// --- five canonical role vertices ---
	RoleConsumerID       = "bsroleConsumerV1aa06f"
	RoleFrontOfHouseID   = "bsroleFrontOfHouseV107"
	RoleBackOfHouseID    = "bsroleBackOfHouseV1a08"
	RoleOperatorID       = "bsroleOperatorV1aa09g"
	RolePlatformIntlID   = "bsrolePlatformIntlV10h"

	RoleConsumerKey     = "vtx.role." + RoleConsumerID
	RoleFrontOfHouseKey = "vtx.role." + RoleFrontOfHouseID
	RoleBackOfHouseKey  = "vtx.role." + RoleBackOfHouseID
	RoleOperatorKey     = "vtx.role." + RoleOperatorID
	RolePlatformIntlKey = "vtx.role." + RolePlatformIntlID

	// --- permission vertices (for platformInternal role) ---
	PermPlatformAnyID  = "bspermPlatformAnyV11j"
	PermPlatformAnyKey = "vtx.permission." + PermPlatformAnyID

	// --- holdsRole links (bootstrap identity + platform actor → platformInternal role) ---
	// Link keys: lnk.<type1>.<id1>.<localName>.<type2>.<id2>
	// Younger = the identity (created at bootstrap, same time), older = the role
	// (also created at bootstrap, same time — we treat identities as older for
	//  link ordering since they're seeded first in the write sequence).
	// Per Contract #1: id1 is younger (later createdAt); since all primordial
	// entries share the same createdAt timestamp, we use alphabetical NanoID
	// order as the tiebreaker — whichever sorts higher is "younger".
	// Bootstrap identity NanoID: bsidPrimordialV1aa01 > bsrolePlatformIntlV10h → identity is younger.
	BootstrapHoldsRoleLinkKey = "lnk.identity." + BootstrapIdentityID + ".holdsRole.role." + RolePlatformIntlID
	PlatformHoldsRoleLinkKey  = "lnk.identity." + PlatformActorID + ".holdsRole.role." + RolePlatformIntlID

	// --- grantsPermission link (platformInternal role → permission vertex) ---
	// role NanoID: bsrolePlatformIntlV10h, permission NanoID: bspermPlatformAnyV11j
	// permission > role alphabetically → permission is younger
	GrantsPermissionLinkKey = "lnk.permission." + PermPlatformAnyID + ".grantsPermission.role." + RolePlatformIntlID

	// Health KV key written by refractor-stub when all primordial keys land.
	HealthBootstrapCompleteKey = "health.bootstrap.complete"
)

// PrimordialVertexKeys returns the complete ordered list of Core KV keys that
// the verify-bootstrap command checks for.
func PrimordialVertexKeys() []string {
	return []string{
		// bootstrap op tracker
		BootstrapOpKey,
		// system identities
		BootstrapIdentityKey,
		PlatformActorKey,
		// meta vertices
		MetaRootKey,
		CapabilityLensKey,
		CapabilityRoleIndexLensKey,
		// roles
		RoleConsumerKey,
		RoleFrontOfHouseKey,
		RoleBackOfHouseKey,
		RoleOperatorKey,
		RolePlatformIntlKey,
		// permission
		PermPlatformAnyKey,
		// links
		BootstrapHoldsRoleLinkKey,
		PlatformHoldsRoleLinkKey,
		GrantsPermissionLinkKey,
	}
}
