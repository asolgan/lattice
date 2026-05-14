// Package bootstrap contains primordial bootstrap logic for Story 1.3.
//
// NanoID alphabet: re-exported from internal/substrate as the single
// canonical constant (Story 1.4 refactor). Runtime NanoID generation uses
// substrate.NewNanoID(); fixed primordial IDs below are declared as
// constants and are the sole exception to the "all NanoIDs are
// runtime-generated" rule.
//
// All twelve primordial IDs below are Contract #1-compliant: exactly
// 20 characters drawn from the 58-character custom alphabet (no I, l, O,
// or 0). They were generated once via substrate.NewNanoID() during the
// Story 1.4 follow-up commit that resolved the
// internal/substrate/CONTRACT-AMENDMENT-REQUEST.md amendment (Option A).
//
// Link directionality convention for primordial entities: since all
// primordial entries share the same createdAt timestamp, Contract #1's
// "younger = later createdAt" rule needs a tiebreaker. The convention
// adopted here is category-based: identities and permissions are
// conventionally "younger" than roles. This is a primordial-specific
// rule (real entities will have distinct createdAt timestamps and
// won't need a tiebreaker).
package bootstrap

import (
	"github.com/asolgan/lattice/internal/substrate"
)

// Alphabet re-exports substrate.Alphabet as the canonical Lattice NanoID
// alphabet. Provided so callers reading from this package see the single
// source of truth.
const Alphabet = substrate.Alphabet

// Primordial fixed NanoIDs — 20 chars, custom 58-char alphabet (no I/l/O/0).
//
// Decision: fixed IDs rather than generated-at-first-run IDs.
// Rationale: primordial entities are platform-version-pinned; their keys are
// referenced in architecture docs and bypass test oracles. A stable key
// simplifies grep, avoids a lattice.bootstrap.json divergence hazard, and
// lets downstream stories hard-code expectations in their own test fixtures.
// The tradeoff (two deployments share the same primordial key space) is
// acceptable at Phase 1 single-cell scope (NFR-R6).
const (
	// --- bootstrap operation tracker ---
	// The synthetic op tracker that "authors" all primordial entries.
	BootstrapOpID  = "LLq4KP1gz7wJMUhFDaHG"
	BootstrapOpKey = "vtx.op." + BootstrapOpID

	// --- system identity vertices ---
	BootstrapIdentityID  = "c7u2zPUMBuhHpuhL3hYf"
	BootstrapIdentityKey = "vtx.identity." + BootstrapIdentityID

	PlatformActorID  = "49GPDXybnQw8mPNVkwHa"
	PlatformActorKey = "vtx.identity." + PlatformActorID

	// --- root DDL meta-vertex ---
	MetaRootID  = "w1zPDXYDvuRURwoeTevZ"
	MetaRootKey = "vtx.meta." + MetaRootID

	// --- Capability Lens definitions ---
	CapabilityLensID  = "FDZJqhn8GFEPFXYubdE9"
	CapabilityLensKey = "vtx.meta." + CapabilityLensID

	CapabilityRoleIndexLensID  = "oDi7XsqcCG39HXjsZNoR"
	CapabilityRoleIndexLensKey = "vtx.meta." + CapabilityRoleIndexLensID

	// --- five canonical role vertices ---
	RoleConsumerID     = "WxJAourMRbhnHKQfrJmB"
	RoleFrontOfHouseID = "VnUsAfD5TFY3ZRRwnfTW"
	RoleBackOfHouseID  = "6knuRrjETnaB9cE3zgJc"
	RoleOperatorID     = "J85t4c4dCnX68pf4RyWU"
	RolePlatformIntlID = "HtdzfjzMErqvqS1J1HZw"

	RoleConsumerKey     = "vtx.role." + RoleConsumerID
	RoleFrontOfHouseKey = "vtx.role." + RoleFrontOfHouseID
	RoleBackOfHouseKey  = "vtx.role." + RoleBackOfHouseID
	RoleOperatorKey     = "vtx.role." + RoleOperatorID
	RolePlatformIntlKey = "vtx.role." + RolePlatformIntlID

	// --- permission vertices (for platformInternal role) ---
	PermPlatformAnyID  = "UHxwcHGD49odFhZutboF"
	PermPlatformAnyKey = "vtx.permission." + PermPlatformAnyID

	// --- holdsRole links (bootstrap identity + platform actor → platformInternal role) ---
	// Link key form: lnk.<type1>.<id1>.<localName>.<type2>.<id2>
	// Convention for primordial entities: identities are categorically
	// "younger" than roles (see package doc). Identity goes in id1 position.
	BootstrapHoldsRoleLinkKey = "lnk.identity." + BootstrapIdentityID + ".holdsRole.role." + RolePlatformIntlID
	PlatformHoldsRoleLinkKey  = "lnk.identity." + PlatformActorID + ".holdsRole.role." + RolePlatformIntlID

	// --- grantsPermission link (platformInternal role → permission vertex) ---
	// Convention for primordial entities: permissions are categorically
	// "younger" than roles. Permission goes in id1 position.
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
