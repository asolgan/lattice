// Package servicedomain is the service-domain Capability Package — the
// instance/template foundation the Loftspace lease-application reference
// vertical converges over.
//
// It declares:
//
//   - One DDL (`service`) defining the generic service vertex type + three
//     lifecycle ops. A service vertex is either a TEMPLATE (an offering) or
//     an INSTANCE (a run of an offering), discriminated by a `.class` aspect
//     value (`service.<x>.template` / `service.<x>.instance`); the service
//     family `<x>` ∈ {backgroundCheck, payment}. Root data is minimal ({});
//     relationships are LINKS:
//
//     lnk.service.<tplId>.availableAt.<locType>.<locId>      # offering's location topology
//     lnk.service.<tplId>.providedBy.<provType>.<provId>     # offering's provider
//     lnk.service.<instId>.instanceOf.service.<tplId>        # run → its template
//     lnk.service.<instId>.providedTo.identity.<applicantId> # run → the applicant
//
//     A run records its external-call OUTCOME (status + completedAt) as an
//     `.outcome` aspect on the instance vertex (D5 — descriptive business
//     data in aspects, root minimal); no outcome aspect exists until
//     RecordServiceOutcome writes it (absence = not-yet-complete).
//
//   - Permissions granting the three lifecycle ops to `operator`
//     (scope: any) — the vertical's installer/orchestrator submits them.
//
//   - Op-metas making CreateServiceInstance and RecordServiceOutcome
//     `forOperation`-resolvable, so a downstream Loom externalTask step can
//     bind them.
//
// It declares NO lens: the serviceAccess / cap.svc read-path auth plane is
// deferred to Phase 3. The convergence lens that reads an instance's outcome
// across the providedTo link is a separate downstream concern.
//
// Depends on identity-domain (the providedTo link points at an identity) and
// orchestration-base (the demo's task/loom substrate). Install via the
// InstallPackage kernel op. See docs/components/_packages.md.
package servicedomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// Package is the static, install-time bundle.
var Package = pkgmgr.Definition{
	Name:        "service-domain",
	Version:     "0.1.0",
	Description: "Service template + instance vertex type (service DDL + lifecycle ops); the instance records its external-call outcome as aspects (D5). No read-path lens (Phase-3 deferred).",
	Depends:     []string{"identity-domain", "orchestration-base"},
	DDLs:        DDLs(),
	Permissions: Permissions(),
	OpMetas:     OpMetas(),
}
