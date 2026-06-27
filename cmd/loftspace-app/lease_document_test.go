package main

import (
	"net/http"
	"strings"
	"testing"
)

// a fully-signed application row with the common terms populated.
func signedRow() applicationRow {
	return applicationRow{
		EntityKey:          "vtx.leaseapp.app1234567",
		Applicant:          "vtx.identity.priya",
		MissingSignature:   false,
		SignedAt:           "2026-06-10T00:00:00Z",
		UnitKey:            "vtx.unit.u1",
		UnitAddress:        "1 Market St",
		UnitCity:           "San Francisco",
		UnitRegion:         "CA",
		UnitRent:           f64(2400),
		UnitCurrency:       "USD",
		UnitBedrooms:       f64(2),
		UnitBathrooms:      f64(1),
		UnitLeaseTerm:      f64(12),
		UnitAvailableFrom:  "2026-07-01",
		TermsMoveInDate:    "2026-07-15",
		TermsLeaseTerm:     f64(18),
		TermsRequestedRent: f64(2300),
	}
}

func TestAssembleLeaseDocument_SignedRendersExecutedLease(t *testing.T) {
	doc, status, msg := assembleLeaseDocument(signedRow(), "Priya Patel")
	if doc == nil {
		t.Fatalf("signed application should produce a document; status=%d msg=%q", status, msg)
	}
	if status != http.StatusOK {
		t.Errorf("status: want 200, got %d", status)
	}
	body := string(doc.content)
	for _, want := range []string{
		"RESIDENTIAL LEASE AGREEMENT",
		"Priya Patel",                       // tenant name (not the raw key)
		"vtx.identity.priya",                // tenant id (name + key both present)
		"1 Market St, San Francisco, CA",    // joined address
		"Signed on:",                        // execution stamp label
		"2026-06-10T00:00:00Z",              // the projected signedAt
		"vtx.leaseapp.app1234567",           // application reference
		"18 months",                         // the applicant's requested term wins over the listing's 12
		"2026-07-15",                        // requested move-in wins over availableFrom
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered lease missing %q:\n%s", want, body)
		}
	}
	// rent shows the listing ask AND the applicant's differing offer.
	if !strings.Contains(body, "$2400") || !strings.Contains(body, "2300") {
		t.Errorf("rent line should show ask + offer:\n%s", body)
	}
	if doc.filename != "signed-lease-leaseapp.app12345.txt" {
		t.Errorf("filename: got %q", doc.filename)
	}
}

func TestAssembleLeaseDocument_UnsignedRejected(t *testing.T) {
	cases := map[string]applicationRow{
		"missing_signature true": {EntityKey: "vtx.leaseapp.x", MissingSignature: true, SignedAt: ""},
		"blank signedAt":         {EntityKey: "vtx.leaseapp.x", MissingSignature: false, SignedAt: "  "},
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			doc, status, msg := assembleLeaseDocument(row, "Anyone")
			if doc != nil {
				t.Fatalf("unsigned application must not produce a document, got %q", doc.content)
			}
			if status != http.StatusConflict {
				t.Errorf("status: want 409, got %d", status)
			}
			if !strings.Contains(msg, "not signed yet") {
				t.Errorf("message should explain the unsigned guard, got %q", msg)
			}
		})
	}
}

// Determinism is the basis for the idempotent, orphan-free attach (deterministic
// store name + content-derived requestId): the same signed application must render
// byte-identical bytes on every build, with no clock read.
func TestRenderLeaseDocument_Deterministic(t *testing.T) {
	row := signedRow()
	a := renderLeaseDocument(row, "Priya Patel")
	b := renderLeaseDocument(row, "Priya Patel")
	if a != b {
		t.Errorf("render is not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

func TestRenderLeaseDocument_NameFallsBackToKey(t *testing.T) {
	row := signedRow()
	body := renderLeaseDocument(row, "")
	if !strings.Contains(body, "Tenant:") || !strings.Contains(body, "vtx.identity.priya") {
		t.Errorf("with no name, the tenant line should fall back to the applicant key:\n%s", body)
	}
	// with no name, the key IS the tenant — no redundant "Tenant ID" line is added.
	if strings.Contains(body, "Tenant ID:") {
		t.Errorf("no separate Tenant ID line should appear when the name is absent:\n%s", body)
	}
}

// An application with no optional .terms (moveInDate omitted at apply) degrades to
// the listing's terms and omits absent fields rather than printing blanks.
func TestRenderLeaseDocument_DegradesWithoutTerms(t *testing.T) {
	row := applicationRow{
		EntityKey:        "vtx.leaseapp.bare",
		Applicant:        "vtx.identity.dana",
		MissingSignature: false,
		SignedAt:         "2026-06-11T12:00:00Z",
		UnitKey:          "vtx.unit.u9",
		UnitAddress:      "9 Mission St",
		UnitRent:         f64(1800),
		UnitLeaseTerm:    f64(12),
		UnitAvailableFrom: "2026-08-01",
	}
	body := renderLeaseDocument(row, "Dana Tester")
	if !strings.Contains(body, "12 months") {
		t.Errorf("term should fall back to the listing's 12:\n%s", body)
	}
	if !strings.Contains(body, "2026-08-01") {
		t.Errorf("move-in should fall back to availableFrom:\n%s", body)
	}
	if strings.Contains(body, "Bedrooms:") || strings.Contains(body, "Bathrooms:") {
		t.Errorf("absent bedroom/bathroom fields should be omitted, not blank:\n%s", body)
	}
}

func TestRenderRent(t *testing.T) {
	cases := []struct {
		name string
		row  applicationRow
		want string
	}{
		{"usd no offer", applicationRow{UnitRent: f64(2400), UnitCurrency: "USD"}, "$2400"},
		{"blank currency", applicationRow{UnitRent: f64(2400)}, "$2400"},
		{"non-usd", applicationRow{UnitRent: f64(1500), UnitCurrency: "EUR"}, "1500 EUR"},
		{"offer differs", applicationRow{UnitRent: f64(2400), UnitCurrency: "USD", TermsRequestedRent: f64(2300)}, "applicant offered 2300"},
		{"no listing rent, has offer", applicationRow{TermsRequestedRent: f64(2200)}, "2200 (applicant offer)"},
		{"no rent at all", applicationRow{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderRent(c.row)
			if c.want == "" {
				if got != "" {
					t.Errorf("want empty, got %q", got)
				}
				return
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("want contains %q, got %q", c.want, got)
			}
		})
	}
}

func TestTrimFloat(t *testing.T) {
	if got := trimFloat(12); got != "12" {
		t.Errorf("whole number should drop the decimal: got %q", got)
	}
	if got := trimFloat(1.5); got != "1.5" {
		t.Errorf("fraction preserved: got %q", got)
	}
}

func TestShortKeyServer(t *testing.T) {
	if got := shortKeyServer("vtx.leaseapp.abcdefghijklmnop"); got != "leaseapp.abcdefgh" {
		t.Errorf("got %q", got)
	}
	if got := shortKeyServer("not-a-key"); got != "not-a-key" {
		t.Errorf("non-key passes through: got %q", got)
	}
}
