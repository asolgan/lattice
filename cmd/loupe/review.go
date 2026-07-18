package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/asolgan/lattice/packages/augur"
	capabilityauthor "github.com/asolgan/lattice/packages/capability-author"
)

// The AI review console (loupe-f16-ai-review-console-ux.md §3, §4): two tabs
// sharing one route shape. GET /api/review/capability(/<id>) lists/fetches
// the capabilityProposals read model; GET /api/review/augur(/<id>) does the
// same over augurProposals. Both are ordinary P5 reads off their own bucket
// (KVListKeys/KVGet, exactly like vault.go's shred fleet view) — no Core-KV
// scan. Capability's reject reuses the existing POST /api/op path (approve +
// apply are F16.2); Augur's approve AND reject both reuse it (F16.3) — Augur's
// approve re-validates entirely server-side in the DDL script, so unlike
// capability it carries no client-computed validation payload.

// capabilityProposalCols is the on-the-wire shape of one capability-proposals
// bucket entry — field names mirror capabilityProposalsSpec's RETURN AS
// aliases (packages/capability-author/lenses.go) verbatim, so decoding is a
// direct json.Unmarshal with no field remapping. A row whose reasoning is
// still in flight (RecordCapabilityProposal hasn't run) projects with every
// field past claimedAt empty/zero — that is a valid row, not a decode
// failure; only a missing/empty Key marks a poison entry.
type capabilityProposalCols struct {
	Key                    string  `json:"key"`
	ProposalKey            string  `json:"proposalKey"`
	RequesterID            string  `json:"requesterId"`
	Intent                 string  `json:"intent"`
	ContextRef             string  `json:"contextRef"`
	ClaimedAt              string  `json:"claimedAt"`
	Kind                   string  `json:"kind"`
	Content                string  `json:"content"`
	TargetMode             string  `json:"targetMode"`
	TargetPackageName      string  `json:"targetPackageName"`
	TargetBaseVersion      string  `json:"targetBaseVersion"`
	TargetNewVersion       string  `json:"targetNewVersion"`
	Rationale              string  `json:"rationale"`
	Confidence             float64 `json:"confidence"`
	ValidationState        string  `json:"validationState"`
	ValidationReport       string  `json:"validationReport"`
	ValidationDeltaPreview any     `json:"validationDeltaPreview"`
	ValidationCheckedAt    string  `json:"validationCheckedAt"`
	Model                  string  `json:"model"`
	PromptHash             string  `json:"promptHash"`
	CatalogHash            string  `json:"catalogHash"`
	ReasonedAt             string  `json:"reasonedAt"`
	ReviewState            string  `json:"reviewState"`
	ReviewInvalidReason    string  `json:"reviewInvalidReason"`
	ReviewedAt             string  `json:"reviewedAt"`
	AppliedAt              string  `json:"appliedAt"`
	AppliedByOp            string  `json:"appliedByOp"`
}

// capabilityProposalRow is the GET /api/review/capability(/<id>) wire shape:
// the bucket cols verbatim plus ProposalID, the bare NanoID the UI routes and
// submits ReviewCapabilityProposal with (the bucket only carries the full
// vtx.capabilityproposal.<id> key).
type capabilityProposalRow struct {
	capabilityProposalCols
	ProposalID string `json:"proposalId"`
}

// decodeCapabilityProposalCols decodes one bucket entry, rejecting a
// poison/malformed entry or one missing the Key a well-formed row always
// carries — mirrors flows.go's decodeFlowCols poison-tolerance.
func decodeCapabilityProposalCols(raw []byte) (capabilityProposalCols, bool) {
	var cols capabilityProposalCols
	if json.Unmarshal(raw, &cols) != nil || cols.Key == "" {
		return capabilityProposalCols{}, false
	}
	return cols, true
}

// capabilityProposalIDFromKey extracts the bare NanoID from a
// vtx.capabilityproposal.<id> vertex key; ok is false for any other shape.
func capabilityProposalIDFromKey(key string) (id string, ok bool) {
	const prefix = "vtx.capabilityproposal."
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	id = strings.TrimPrefix(key, prefix)
	if id == "" || strings.Contains(id, ".") {
		return "", false
	}
	return id, true
}

// toCapabilityProposalRow pairs decoded cols with the id extracted from Key;
// ok is false when Key isn't a well-formed capabilityproposal vertex key (a
// poison entry the caller should skip).
func toCapabilityProposalRow(cols capabilityProposalCols) (capabilityProposalRow, bool) {
	id, ok := capabilityProposalIDFromKey(cols.Key)
	if !ok {
		return capabilityProposalRow{}, false
	}
	return capabilityProposalRow{capabilityProposalCols: cols, ProposalID: id}, true
}

// computeCapabilityProposals assembles the queue's row list from the bucket's
// keys. Rows are returned key-sorted for a deterministic wire order; the
// pending-first / newest-first triage sort is the goja logic tier's job
// (logic/review.js's proposalRows), per the design's "decision logic lives in
// the logic tier" rule.
func computeCapabilityProposals(keys []string, get kvGetter) []capabilityProposalRow {
	rows := make([]capabilityProposalRow, 0, len(keys))
	for _, k := range keys {
		raw, ok := get(k)
		if !ok {
			continue
		}
		cols, ok := decodeCapabilityProposalCols(raw)
		if !ok {
			continue
		}
		row, ok := toCapabilityProposalRow(cols)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ProposalID < rows[j].ProposalID })
	return rows
}

// handleReview routes GET /api/review/{capability,augur}(/<id>) to the two
// tabs' queue/detail handlers.
func (s *server) handleReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusBadRequest, "GET required")
		return
	}
	parts := splitNonEmpty(strings.TrimPrefix(r.URL.Path, "/api/review/"))
	if len(parts) == 0 || (parts[0] != "capability" && parts[0] != "augur") {
		s.writeError(w, http.StatusBadRequest, "expected GET /api/review/{capability,augur} or GET /api/review/{capability,augur}/<id>")
		return
	}
	tab := parts[0]
	switch len(parts) {
	case 1:
		if tab == "augur" {
			s.reviewAugurQueue(w, r)
		} else {
			s.reviewCapabilityQueue(w, r)
		}
	case 2:
		if tab == "augur" {
			s.reviewAugurDetail(w, r, parts[1])
		} else {
			s.reviewCapabilityDetail(w, r, parts[1])
		}
	default:
		s.writeError(w, http.StatusBadRequest, "expected GET /api/review/{capability,augur} or GET /api/review/{capability,augur}/<id>")
	}
}

func (s *server) reviewCapabilityQueue(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	keys, err := conn.KVListKeys(ctx, capabilityauthor.CapabilityProposalsBucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "list "+capabilityauthor.CapabilityProposalsBucket+": "+err.Error())
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, capabilityauthor.CapabilityProposalsBucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	rows := computeCapabilityProposals(keys, get)
	s.writeJSON(w, http.StatusOK, map[string]any{"proposals": rows, "count": len(rows)})
}

func (s *server) reviewCapabilityDetail(w http.ResponseWriter, r *http.Request, id string) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	if err := validateControlName(id); err != nil {
		s.writeError(w, http.StatusBadRequest, "proposal id: "+err.Error())
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	key := "vtx.capabilityproposal." + id
	entry, err := conn.KVGet(ctx, capabilityauthor.CapabilityProposalsBucket, key)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "capability proposal "+id+" not found: "+err.Error())
		return
	}
	cols, ok := decodeCapabilityProposalCols(entry.Value)
	if !ok {
		s.writeError(w, http.StatusBadGateway, "capability proposal "+id+": malformed read-model row")
		return
	}
	row, ok := toCapabilityProposalRow(cols)
	if !ok {
		s.writeError(w, http.StatusBadGateway, "capability proposal "+id+": row key does not resolve to this id")
		return
	}
	s.writeJSON(w, http.StatusOK, row)
}

// augurProposalCols is the on-the-wire shape of one augur-proposals bucket
// entry — field names mirror augurProposalsSpec's RETURN AS aliases
// (packages/augur/lenses.go) verbatim, so decoding is a direct
// json.Unmarshal with no field remapping. A row whose reasoning is still in
// flight (RecordProposal hasn't run) projects with reviewState empty and
// every model-derived column zero/empty — that is a valid row (the claim
// vertex, gap context only), not a decode failure; only a missing/empty Key
// marks a poison entry.
type augurProposalCols struct {
	Key            string  `json:"key"`
	ProposalKey    string  `json:"proposalKey"`
	TargetID       string  `json:"targetId"`
	EntityID       string  `json:"entityId"`
	GapColumn      string  `json:"gapColumn"`
	Trigger        string  `json:"trigger"`
	ProposedAction string  `json:"proposedAction"`
	ProposedParams any     `json:"proposedParams"`
	Rationale      string  `json:"rationale"`
	Confidence     float64 `json:"confidence"`
	Model          string  `json:"model"`
	ReasonedAt     string  `json:"reasonedAt"`
	ReviewState    string  `json:"reviewState"`
	InvalidReason  string  `json:"invalidReason"`
	ReviewedAt     string  `json:"reviewedAt"`
	DispatchedAt   string  `json:"dispatchedAt"`
}

// augurProposalRow is the GET /api/review/augur(/<id>) wire shape: the bucket
// cols verbatim plus ProposalID, the bare handle the UI routes with and
// submits ReviewProposal's externalRef with (the bucket only carries the
// full vtx.augurproposal.<handle> key).
type augurProposalRow struct {
	augurProposalCols
	ProposalID string `json:"proposalId"`
}

// decodeAugurProposalCols decodes one bucket entry, rejecting a
// poison/malformed entry or one missing the Key a well-formed row always
// carries — mirrors decodeCapabilityProposalCols.
func decodeAugurProposalCols(raw []byte) (augurProposalCols, bool) {
	var cols augurProposalCols
	if json.Unmarshal(raw, &cols) != nil || cols.Key == "" {
		return augurProposalCols{}, false
	}
	return cols, true
}

// augurProposalIDFromKey extracts the bare handle from a
// vtx.augurproposal.<handle> vertex key; ok is false for any other shape.
func augurProposalIDFromKey(key string) (id string, ok bool) {
	const prefix = "vtx.augurproposal."
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	id = strings.TrimPrefix(key, prefix)
	if id == "" || strings.Contains(id, ".") {
		return "", false
	}
	return id, true
}

// toAugurProposalRow pairs decoded cols with the id extracted from Key; ok is
// false when Key isn't a well-formed augurproposal vertex key (a poison entry
// the caller should skip).
func toAugurProposalRow(cols augurProposalCols) (augurProposalRow, bool) {
	id, ok := augurProposalIDFromKey(cols.Key)
	if !ok {
		return augurProposalRow{}, false
	}
	return augurProposalRow{augurProposalCols: cols, ProposalID: id}, true
}

// computeAugurProposals assembles the queue's row list from the bucket's
// keys. Rows are returned key-sorted for a deterministic wire order; the
// pending-first/confidence-descending triage sort is the goja logic tier's
// job (logic/review.js's augurProposalRows), per the design's "decision
// logic lives in the logic tier" rule.
func computeAugurProposals(keys []string, get kvGetter) []augurProposalRow {
	rows := make([]augurProposalRow, 0, len(keys))
	for _, k := range keys {
		raw, ok := get(k)
		if !ok {
			continue
		}
		cols, ok := decodeAugurProposalCols(raw)
		if !ok {
			continue
		}
		row, ok := toAugurProposalRow(cols)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ProposalID < rows[j].ProposalID })
	return rows
}

func (s *server) reviewAugurQueue(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	keys, err := conn.KVListKeys(ctx, augur.AugurProposalsBucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "list "+augur.AugurProposalsBucket+": "+err.Error())
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, augur.AugurProposalsBucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	rows := computeAugurProposals(keys, get)
	s.writeJSON(w, http.StatusOK, map[string]any{"proposals": rows, "count": len(rows)})
}

func (s *server) reviewAugurDetail(w http.ResponseWriter, r *http.Request, id string) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	if err := validateControlName(id); err != nil {
		s.writeError(w, http.StatusBadRequest, "proposal id: "+err.Error())
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	key := "vtx.augurproposal." + id
	entry, err := conn.KVGet(ctx, augur.AugurProposalsBucket, key)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "augur proposal "+id+" not found: "+err.Error())
		return
	}
	cols, ok := decodeAugurProposalCols(entry.Value)
	if !ok {
		s.writeError(w, http.StatusBadGateway, "augur proposal "+id+": malformed read-model row")
		return
	}
	row, ok := toAugurProposalRow(cols)
	if !ok {
		s.writeError(w, http.StatusBadGateway, "augur proposal "+id+": row key does not resolve to this id")
		return
	}
	s.writeJSON(w, http.StatusOK, row)
}
