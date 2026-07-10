package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// No-Postgres unit coverage for the portfolio-pulse reader: the fail-closed
// auth/pool paths and the pure aggregation logic, mirroring
// landlord_applications_test.go.

func TestHandlePortfolioPulse_NoAuthConfigured_401(t *testing.T) {
	s := &server{logger: discardLogger(), natsTimeout: testTimeout} // authn nil
	rec := httptest.NewRecorder()
	s.handlePortfolioPulse(rec, httptest.NewRequest(http.MethodGet, "/api/portfolio-pulse", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHandlePortfolioPulse_NoToken_401(t *testing.T) {
	s := devAuthServer(t)
	rec := httptest.NewRecorder()
	s.handlePortfolioPulse(rec, httptest.NewRequest(http.MethodGet, "/api/portfolio-pulse", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no bearer)", rec.Code)
	}
}

func TestHandlePortfolioPulse_ForgedToken_401(t *testing.T) {
	s := devAuthServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/portfolio-pulse", nil)
	r.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	s.handlePortfolioPulse(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (forged token)", rec.Code)
	}
}

// A verified actor with no read-model pool gets a clean 502, never a
// nil-pointer panic (mirrors the landlord-applications reader).
func TestHandlePortfolioPulse_ValidToken_PoolUnconfigured_502(t *testing.T) {
	s := devAuthServer(t) // authn set, pgPool nil
	tok, _, err := s.devSigner.mint("Hj4kPmRtw9nbCxz5vQ2y")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/portfolio-pulse", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	s.handlePortfolioPulse(rec, r)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (pool unconfigured)", rec.Code)
	}
}

func TestSummarizePortfolioPulse(t *testing.T) {
	rent1500 := 1500.0
	units := []portfolioPulseUnit{
		{UnitKey: "vtx.unit.a", UnitStatus: "leased", UnitRent: &rent1500},
		{UnitKey: "vtx.unit.b", UnitStatus: "leased"},
		{UnitKey: "vtx.unit.c", UnitStatus: "available"},
		{UnitKey: "vtx.unit.d", UnitStatus: "pending"},
		{UnitKey: "vtx.unit.e", UnitStatus: "withdrawn"},
		{UnitKey: "vtx.unit.f", UnitStatus: ""}, // never listed
	}
	got := summarizePortfolioPulse(units)
	if got.TotalUnits != 6 || got.Leased != 2 || got.Available != 1 || got.Pending != 1 || got.Withdrawn != 1 || got.NotListed != 1 {
		t.Fatalf("unexpected breakdown: %+v", got)
	}
	if want := 2.0 / 6.0; got.OccupancyRate != want {
		t.Fatalf("occupancyRate = %v, want %v", got.OccupancyRate, want)
	}
}

func TestSummarizePortfolioPulse_NoUnits_ZeroRateNoDivideByZero(t *testing.T) {
	got := summarizePortfolioPulse(nil)
	if got.TotalUnits != 0 || got.OccupancyRate != 0 {
		t.Fatalf("empty portfolio should be all-zero, got %+v", got)
	}
}
