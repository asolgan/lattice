# Clinic multi-site — design note

**Status:** Increment 1 (backend) shipped. Increment 2 (site directory FE +
booking site filter + appointment→site association) shipped.
**Board row:** [verticals.md](../planning-artifacts/backlog/verticals.md) — "Clinic is a single-location, single-specialty silo".

## Problem

`location-domain` was unused by `clinic-domain` (unlike `loftspace-domain`, which
already integrates it): a provider had exactly one `specialty` and no site. A real
multi-location practice group needs provider↔location association and (eventually)
per-location scheduling.

## Pattern mirrored

`loftspace-domain`'s two-DDL cross-package-contribution shape onto `location-domain`'s
place graph (`loftspaceListing` aspect-contribution + `loftspaceOwnership`
link-contribution), applied 1:1 to clinic:

- **`clinicSite`** (vertexType DDL, owns `SetSiteProfile`) writes a `.site` aspect
  `{name}` onto a `vtx.building` (validated alive + `class=location`). `clinicSiteProfile`
  is the paired aspectType (step-6 write gate) DDL — mirrors `loftspaceListing`/`listing`.
- **`clinicSiteAssignment`** (vertexType DDL, owns `AssignProviderSite` /
  `RemoveProviderSite`) writes/tombstones the `practicesAt` LINK
  `lnk.provider.<id>.practicesAt.building.<id>` — source = provider (the later-arriving
  fact — a provider is *assigned* to a site), target = building. Reads as "provider
  practicesAt building." Create/revive-CAS/no-op idempotency, deterministic per-pair
  key read on demand (`kv.Read`, declared `optionalReads`) — mirrors
  `loftspaceOwnership`'s `AssignUnitOwner`/`RemoveUnitOwner` exactly, including the
  many-to-many shape (a provider may practice at many sites; a site may host many
  providers — no list needed, the pair key alone is the uniqueness constraint).

`clinic-domain` now `depends: [location-domain]` (previously self-contained).

## Lenses

- **`clinicSites`** (nats-kv, one row per named building) — mirrors `availableListings`'s
  flat single-MATCH shape. Site directory read model.
- **`providerSites`** (nats-kv, one row per `(provider, site)` pair, composite
  `IntoKey: [provider_id, site_id]`, `DiffRetraction: true`) — mirrors
  identity-hygiene's `duplicateCandidates` exactly. A provider×site join was
  deliberately NOT folded as an array column into `clinicProviders` (a `collect()`
  aggregation's grouping semantics inside a non-`$actorKey`-anchored "full" multi-row
  lens is unproven in this codebase — every existing `collect()` use anchors on a
  single actor key. A separate one-row-per-pair lens with a composite key sidesteps
  the question entirely and has a proven precedent.)

## Increment 2 (shipped)

- **FE**: `cmd/clinic-app` — a "Sites" admin tab (`SetSiteProfile` via a
  `CreateLocation → SetSiteProfile` chain, mirroring `loftspace-app`'s
  `submitPostListing` chain; `AssignProviderSite`/`RemoveProviderSite` forms) and a
  `#book-site` filter in the booking picker (mirrors `8315a88`'s specialty filter,
  including the soonest-opening panel). New read handlers `GET /api/sites` /
  `GET /api/provider-sites` serve the `clinicSites`/`providerSites` lenses (P5).
- **Appointment→site association**: `CreateAppointment` accepts an optional `site`
  (`vtx.building.<NanoID>`). Once supplied it is HARD-validated — unlike `leaseAppKey`'s
  silent fall-through — the building must be alive + `class=location` AND the provider
  must `practicesAt` it (`require_site_membership`, `ddls.go`), or the whole booking
  rejects (`UnknownSite` / `NotALocation` / `ProviderNotAtSite`). On success an `atSite`
  link (`lnk.appointment.<id>.atSite.building.<id>`, appointment→building) is written.
  Omitted → no site recorded, fully backward-compatible with every pre-Inc2 appointment.

**Per-location scheduling hours** (deferred, unchanged from Increment 1's note): `.hours`/
`.timeOff` are still keyed only on the provider — a provider practicing at two sites has
ONE availability set shared across both. Left as a follow-up design decision; the
recommendation (keep the slot-claim provider-scoped, add an optional per-site `.hours`
override read at booking time) still stands and is not blocked by Increment 2.

## Verification

`packages/clinic-domain/site_integration_test.go` — full lifecycle through the real
Processor (create/idempotent-reassign/revive/reject-dead-provider/multi-site, plus
Increment 2's `TestClinic_CreateAppointment_WithValidSite` /
`_RejectsProviderNotAtSite` / `_RejectsNonLocationSite`). `cmd/clinic-app/handlers_test.go`
covers `computeSites`/`computeProviderSites`. Makefile install-order (`install-clinic`,
`verify-package-clinic-domain`, `verify-package-clinic-reminders`, `refresh-clinic`) and
`scripts/verify-package-clinic-domain.go` updated for the new dependency + DDLs + ops.
Live-verified on the shared dev stack via direct Gateway `submitOp` calls (reject/assign/
accept + `atSite` link committed) — the `clinicSites`/`providerSites` lens READ SIDE is
not yet visible on that particular stack because its long-running Refractor process
never picked up the two new lenses (pre-existing gap from Increment 1, flagged
separately; a Refractor restart resolves it).
