package main

import (
	"encoding/json"
	"net/http"
	"sort"

	clinicdomain "github.com/asolgan/lattice/packages/clinic-domain"
)

// patientRow is one row of the clinic-domain `clinicPatients` lens read model
// (P5: an application reads the lens projection, never Core KV). It carries the
// patient NAME only — DOB / contact is the PHI the deferred Vault plane owns and
// is not projected into this read model. The top-bar switcher renders these.
type patientRow struct {
	PatientKey string `json:"patientKey"`
	Name       string `json:"name"`
}

// computePatients assembles the patient roster from the `clinicPatients` lens read
// model. A row that fails to decode or carries no patientKey (a tombstoned
// projection entry) is skipped. Rows are sorted by name for a stable switcher.
func computePatients(keys []string, get kvGetter) []patientRow {
	rows := make([]patientRow, 0, len(keys))
	for _, k := range keys {
		raw, ok := get(k)
		if !ok {
			continue
		}
		var p patientRow
		if json.Unmarshal(raw, &p) != nil || p.PatientKey == "" {
			continue
		}
		rows = append(rows, p)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].PatientKey < rows[j].PatientKey
	})
	return rows
}

// handlePatients implements GET /api/patients — the patient-context switcher,
// served from the `clinicPatients` lens read model (NOT Core KV).
func (s *server) handlePatients(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	bucket := clinicdomain.ClinicPatientsBucket
	keys, err := conn.KVListKeys(ctx, bucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway,
			"list "+bucket+": "+err.Error()+" (is clinic-domain installed and the Refractor projecting?)")
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, bucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	rows := computePatients(keys, get)
	s.writeJSON(w, http.StatusOK, map[string]any{"patients": rows, "count": len(rows)})
}
