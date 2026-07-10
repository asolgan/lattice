// Package frontdesk is the Café/Wellness "mixed-use composition surfaces"
// Increment 1 (+ the Inc 4 lease-details tail) — the front-desk unified
// resident context. It owns no vertex types, links, or permissions of its
// own: two Lens declarations (Lenses()) — frontDeskBookings re-projects
// wellness-domain's residentRate-linked bookings, keyed by leaseAppKey, into
// front-desk-bookings (a resident's booked class surfaced right next to
// their café tab, without asking); frontDeskLeaseDetails re-projects every
// leaseapp's applied-to unit rent/term, keyed by leaseAppKey, into
// front-desk-lease-details (the lease details — term/rent — on every
// open-tab card, not just those with a booked class).
//
// The café half of the unified context (open tabs) needs no re-projection:
// cafe-domain's own cafeTabSettlement convergence lens already serves it
// keyed by leaseAppKey, so the FE joins the two client-side, mirroring
// wellness-domain's own deliberately-uncounted bookedCount idiom.
//
// Depends on wellness-domain for the vertex/link classes its lens matches —
// declared for install-order/documentation honesty, though the cypher
// engine itself matches by class label at read time regardless (installing
// before wellness-domain just means the lens projects zero rows, not an
// error, the same one-bill precedent). frontDeskLeaseDetails matches leaseapp
// and unit the same way, without adding lease-signing/loftspace-domain to
// Depends — consistent with frontDeskBookings' own leaseapp match above.
// Install via the InstallPackage kernel op. See docs/components/_packages.md.
package frontdesk

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:        "front-desk",
	Version:     "0.2.0",
	Description: "Café/Wellness mixed-use composition Inc 1 + Inc 4 — front-desk unified resident context: wellness-domain's resident-rate bookings and every leaseapp's unit rent/term, both re-projected keyed by leaseAppKey, joined client-side with cafe-domain's open tabs.",
	Depends:     []string{"wellness-domain"},
	Lenses:      Lenses(),
}
