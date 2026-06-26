package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	loftspacedomain "github.com/asolgan/lattice/packages/loftspace-domain"
)

// identityRow is one projected `applicantRoster` row — a selectable identity with
// its human-readable name. The applicant picker renders `name`, carrying `key` as
// the value it scopes reads/writes to.
type identityRow struct {
	IdentityKey string `json:"identityKey"`
	Name        string `json:"name"`
	State       string `json:"state"`
}

// identityView is the picker's projection of one identity: the key it scopes to
// plus the human name to show.
type identityView struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// computeIdentities assembles the applicant picker from the `applicantRoster` lens
// read model: every named identity (the lens already filters out unnamed service
// actors), reshaped to {key, name, state} and sorted by name for a stable picker.
// A row that fails to decode or carries no key/name is skipped.
func computeIdentities(keys []string, get kvGetter) []identityView {
	out := make([]identityView, 0)
	for _, k := range keys {
		raw, ok := get(k)
		if !ok {
			continue
		}
		var row identityRow
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if row.IdentityKey == "" || strings.TrimSpace(row.Name) == "" {
			continue
		}
		out = append(out, identityView{Key: row.IdentityKey, Name: row.Name, State: row.State})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// handleIdentities implements GET /api/identities — the applicant picker, served
// from the `applicantRoster` lens rows in the loftspace-identities read model (NOT
// Core KV; P5). Returns every named identity so a person selects themselves by
// name instead of typing a raw vtx.identity.<id> key.
func (s *server) handleIdentities(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	bucket := loftspacedomain.LoftspaceIdentitiesBucket
	keys, err := conn.KVListKeys(ctx, bucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway,
			"list "+bucket+": "+err.Error()+" (is loftspace-domain installed and the Refractor projecting?)")
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, bucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	rows := computeIdentities(keys, get)
	s.writeJSON(w, http.StatusOK, map[string]any{"identities": rows, "count": len(rows)})
}
