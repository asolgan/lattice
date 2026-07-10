# Mixed-use composition surfaces — design + checkpoint

**Status:** 🏗️ building (Inc 1 shipped). Board row: [verticals.md](../planning-artifacts/backlog/verticals.md).

## Goal

The backlog item names two views that exist only because LoftSpace/Clinic/Café/Wellness share one
graph:

- **Front-desk unified resident context** — lease + visit + open tab + booked class, in one lookup,
  surfaced before asked.
- **Operations portfolio-pulse aggregate** — occupancy + service-attach-rate across packages.

## Grounding

- **Linking model is LINK-based, not shared-vertex-aspect.** All four verticals converge on one
  `vtx.identity.<NanoID>`, but each vertical's own vertex (leaseapp/patient/booking/tab) stays a
  separate vertex connected by an explicit link:
  - `packages/lease-signing/ddls.go` — leaseapp→identity via `applicationFor`.
  - `packages/clinic-domain/ddls.go` — patient→identity via `identifiedBy`.
  - `packages/wellness-domain/ddls.go` (`bookingVertexTypeDDL`) — booking→identity via `bookedBy`,
    plus an *emergent* `residentRate` link (booking→leaseapp) written ONLY when a supplied
    `leaseAppKey`'s `applicationFor` link resolves to the same identity as the booker, AND the
    leaseapp carries a `.tenancy` aspect (CreateOnly-stamped on the first `DecideLeaseApplication`
    approve) — a mismatch or unapproved lease falls through to `rate=standard`, never a hard failure.
  - `packages/cafe-domain/ddls.go` (`tabVertexTypeDDL`) — tab carries `leaseAppKey` denormalized onto
    its own `.status` aspect (not a fresh link) and the `openFor` link (tab→leaseapp).
- **Precedent: `packages/one-bill`** (Café Inc 3) — a lens-only package with no DDLs of its own,
  re-projecting two OTHER packages' data (loftspace-ledger + cafe-ledger transactions) into one shared
  bucket, tagged by `source`, because the cypher engine has no UNION. `front-desk` (below) mirrors this
  shape exactly.

## Increment 1 (shipped this fire)

**Front-desk: café open tab + resident's booked wellness class**, scoped down from the full 4-way +
operations aggregate (too large for one fire — see Deferred below):

- New lens-only package `packages/front-desk` (mirrors `one-bill`): one Lens, `frontDeskBookings`,
  re-projecting wellness-domain's `residentRate`-linked, `booked`-status bookings into
  `front-desk-bookings`, keyed by `leaseAppKey`. A booking with no `residentRate` link (standard rate,
  or an unclaimed/unapproved lease) never projects — front-desk shows only a resident's OWN booking,
  never every booking in the building.
- The café half (open tabs) needed **no new lens** — `cafe-domain`'s own `cafeTabSettlement`
  convergence lens already serves it keyed by `leaseAppKey`; the FE joins the two client-side by
  `leaseAppKey`, the same composition idiom `cmd/cafe-app`'s `computeTabs` and wellness-domain's own
  deliberately-uncounted `bookedCount` already use.
- `cmd/cafe-app`: new `GET /api/frontdesk-bookings` handler (`frontdesk.go`), wired into the existing
  Front Desk view (`web/app.js` `loadFrontDesk`/`frontDeskCard`) — each open-tab card now shows a
  "🧘 Booked: `<session>` · `<time>`" line when the resident has a live resident-rate booking.
  Best-effort: an unreachable/uninstalled `front-desk` bucket degrades to "no badge," not a page error.
- Registries: `cmd/lattice-pkg/main.go`, `cmd/loupe/pkg.go` (`packageRegistry`); `Makefile`
  `install-frontdesk` (mirrors `install-onebill`, depends on `wellness-domain` being installed first).
- Tests: `packages/front-desk/lens_cypher_test.go` (real rule-engine proof against the production
  spec — resident-rate row projects, standard-rate row doesn't, a cancelled/soft-deleted booking
  doesn't), `package_test.go` (manifest/definition parity), `cmd/cafe-app/frontdesk_test.go`
  (tombstoned-row skip).
- Live-verified: installed on the running dev stack (`front-desk` package, `writeCount=8`); cycled
  `cafe-app` to the rebuilt binary; the Front Desk view fires `GET /api/tabs` + `GET
  /api/frontdesk-bookings` (both 200), no console errors. **Not** live-verified: an actual populated
  booking badge — the one pre-existing lease on the shared stack has no `.tenancy` aspect (never
  signed/approved), and mutating that pre-existing, not-created-this-session vertex to force it
  through sign+approve was correctly blocked by the auto-mode safety classifier (modifying shared
  state without user authorization). The positive-projection case is instead proven by
  `lens_cypher_test.go` against the real rule engine using the exact production `bookingsSpec`
  constant — the strongest available proof short of a live click-through.

## Deferred (Inc 2+, not yet scoped in detail)

- **Clinic visit in the unified context** — deliberately excluded from Inc 1 per the PHI-sensitivity
  note on the *separate* "Clinical notes are write-only" backlog row (clinic patient data has its own
  Secure-Lens/Vault posture, `identifiedBy` claim semantics differ from `residentRate`'s optional/
  best-effort link) — needs its own grounding pass, not a copy-paste of this pattern.
- **Operations portfolio-pulse aggregate** (occupancy + service-attach-rate across packages) — needs
  an occupancy data source not yet identified (LoftSpace's `.listing` economics project availability,
  not a live occupancy count); a fresh grounding pass, likely its own lens-only package alongside
  `front-desk`.
- **Lease details on the front-desk card** (term/rent, not just the `leaseAppKey` short-key already
  shown) — small, no new lens needed (loftspace-ledger's existing lens already carries it), just FE
  wiring; picked up whenever `front-desk` gets its next increment.

**Next fire on this item:** pick up operations portfolio-pulse OR the clinic-visit tail — whichever
grounds cleanest; re-read this doc's Deferred section first.
