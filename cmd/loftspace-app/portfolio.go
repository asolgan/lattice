package main

import (
	"context"
	"net/http"
)

// Portfolio-pulse occupancy (mixed-use-composition-design.md Inc 2): the
// landlord-facing "how full is my portfolio" view, read from the protected
// landlordUnitsRead Postgres model. Sibling of handleLandlordApplications —
// identical verified-JWT -> per-request txn -> SET LOCAL lattice.actor_id ->
// RLS path, but this reads EVERY unit the landlord manages, independent of
// whether it has ever had a lease application (landlordLeaseApplicationsRead
// requires a leaseapp to exist at all, so a never-applied-to unit is invisible
// to it).
//
// Service-attach-rate (occupancy's cross-package sibling — does a resident
// have a live wellness booking / open café tab) is deliberately deferred: it
// needs a cross-package KV fan-in this app doesn't otherwise do, and its own
// grounding pass (see mixed-use-composition-design.md Deferred).

// portfolioPulseUnit is one row of the occupancy breakdown: a unit the
// landlord manages, plus its coarse listing status. UnitStatus is empty when
// the unit was never listed (landlordUnitsRead projects unit_status null) —
// a distinct bucket from any of the four listed statuses.
type portfolioPulseUnit struct {
	UnitKey    string   `json:"unitKey"`
	UnitStatus string   `json:"unitStatus"`
	UnitRent   *float64 `json:"unitRent"`
}

// portfolioPulseResult is the GET /api/portfolio-pulse response: the flat
// per-unit rows plus the aggregate occupancy counts the FE renders as the
// pulse card. OccupancyRate is leased/total, 0 when the landlord manages no
// units (never divides by zero).
type portfolioPulseResult struct {
	Units         []portfolioPulseUnit `json:"units"`
	TotalUnits    int                  `json:"totalUnits"`
	Leased        int                  `json:"leased"`
	Available     int                  `json:"available"`
	Pending       int                  `json:"pending"`
	Withdrawn     int                  `json:"withdrawn"`
	NotListed     int                  `json:"notListed"`
	OccupancyRate float64              `json:"occupancyRate"`
}

// selectLandlordUnitsSQL reads the protected occupancy model. No auth WHERE —
// RLS scopes the rows to the requesting landlord via the txn-local
// lattice.actor_id session variable, same as selectLandlordApplicationsSQL.
const selectLandlordUnitsSQL = `
SELECT unit_key, COALESCE(unit_status, ''), unit_rent
FROM read_landlord_units
ORDER BY unit_key`

// queryLandlordUnits runs the protected landlord occupancy read inside a
// per-request transaction with a txn-local actor session variable — the same
// pooling-safety pattern as queryLandlordApplications.
func queryLandlordUnits(ctx context.Context, pool pgxBeginner, actorID string) ([]portfolioPulseUnit, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('lattice.actor_id', $1, true)", actorID); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, selectLandlordUnitsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]portfolioPulseUnit, 0)
	for rows.Next() {
		var u portfolioPulseUnit
		if err := rows.Scan(&u.UnitKey, &u.UnitStatus, &u.UnitRent); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// summarizePortfolioPulse folds the flat per-unit rows into the aggregate
// counts the FE card renders. A pure function of the RLS-scoped rows — no
// auth logic (RLS already guaranteed every row belongs to the requesting
// landlord).
func summarizePortfolioPulse(units []portfolioPulseUnit) portfolioPulseResult {
	res := portfolioPulseResult{Units: units, TotalUnits: len(units)}
	for _, u := range units {
		switch u.UnitStatus {
		case "leased":
			res.Leased++
		case "available":
			res.Available++
		case "pending":
			res.Pending++
		case "withdrawn":
			res.Withdrawn++
		default:
			res.NotListed++
		}
	}
	if res.TotalUnits > 0 {
		res.OccupancyRate = float64(res.Leased) / float64(res.TotalUnits)
	}
	return res
}

func (s *server) handlePortfolioPulse(w http.ResponseWriter, r *http.Request) {
	actor, err := s.authenticateRead(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "authentication required: "+err.Error())
		return
	}
	if s.pgPool == nil {
		s.logger.Error("portfolio-pulse protected read requested but pgPool is nil (set LOFTSPACE_APP_PG_DSN + ensure Postgres and the loftspace-domain protected lens are up)")
		s.writeError(w, http.StatusBadGateway, "protected read model unavailable")
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	units, err := queryLandlordUnits(ctx, s.pgPool, actor.Subject)
	if err != nil {
		s.logger.Error("read protected landlord units", "error", err)
		s.writeError(w, http.StatusBadGateway, "could not read the protected landlord-units model")
		return
	}
	s.writeJSON(w, http.StatusOK, summarizePortfolioPulse(units))
}
