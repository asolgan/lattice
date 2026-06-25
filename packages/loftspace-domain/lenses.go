package loftspacedomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// LoftspaceListingsBucket is the NATS-KV read model the availableListings lens
// projects into. It is the **P5 query surface** for "what units can I lease":
// an application reads THIS projected bucket (one entry per listed unit, keyed by
// the unit key), never Core KV (lattice-architecture.md P5 — lenses are the only
// application query surface). The Refractor auto-creates the bucket on lens load.
const LoftspaceListingsBucket = "loftspace-listings"

// Lenses returns the package's Lens declarations: the single `availableListings`
// projection lens. It projects one row per LISTED unit — a location unit
// carrying a `.listing` aspect — flattening the listing economics + street
// address into a query-optimized read-model row. The lens does NOT filter on
// status (it carries the status column), so a reader can show available units by
// default and still surface pending / leased on request; the per-row key is the
// unit key (the CreateLeaseApplication target the applicant FE submits).
func Lenses() []pkgmgr.LensSpec {
	return []pkgmgr.LensSpec{
		{
			CanonicalName: "availableListings",
			Class:         "meta.lens",
			Adapter:       "nats-kv",
			Bucket:        LoftspaceListingsBucket,
			Engine:        "full",
			Spec:          availableListingsSpec,
		},
	}
}

// availableListingsSpec projects one row per listed unit. The WHERE keeps only
// units whose `.listing` aspect exists (status non-null) — a unit with no
// listing is not leasable and is excluded. Aspect fields are read by the
// documented `node.<aspect>.data.<field>` form (executor.go), the same access
// lease-signing's convergence lens uses against these exact `.listing` /
// `.address` aspects. The per-row key column is `key` (the unit key, the
// IntoKey default), so the read model is keyed by `vtx.unit.<id>`; `unitKey`
// repeats it in the body for the reader. Address columns are null when a unit
// has no `.address` aspect (the reader treats them as absent).
const availableListingsSpec = `MATCH (u:unit)
WHERE u.listing.data.status <> null
RETURN
  u.key AS key,
  u.key AS unitKey,
  u.listing.data.status AS status,
  u.listing.data.rentAmount AS rentAmount,
  u.listing.data.rentCurrency AS rentCurrency,
  u.listing.data.bedrooms AS bedrooms,
  u.listing.data.bathrooms AS bathrooms,
  u.listing.data.sqft AS sqft,
  u.listing.data.availableFrom AS availableFrom,
  u.listing.data.leaseTermMonths AS leaseTermMonths,
  u.address.data.line1 AS addrLine1,
  u.address.data.line2 AS addrLine2,
  u.address.data.city AS addrCity,
  u.address.data.region AS addrRegion,
  u.address.data.postal AS addrPostal`
