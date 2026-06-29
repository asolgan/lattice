package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asolgan/lattice/cmd/lattice/output"
	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/processor"
	"github.com/asolgan/lattice/internal/substrate"
)

// attachReqNamespace seeds the deterministic AttachObject requestId so a
// double-submitted upload of identical bytes to the same slot collapses on the
// Contract #4 tracker.
const attachReqNamespace = "loftspace:object:attach:"

// attachmentsKeyPrefix is the OutputKeyPattern prefix of the objects-base
// `objectAttachments` display lens. The Documents tab reads these rows out of
// the shared weaver-targets read model — never Core KV (P5). Loupe scans Core KV
// because it is the admin inspector; a vertical app reads projections.
const attachmentsKeyPrefix = "objectAttachments."

// inlineImageTypes are the content types streamed back inline so the browser can
// render a thumbnail; everything else (pdf, svg, html, unknown) is forced to a
// neutral octet-stream attachment so an uploaded active document can never run
// as same-origin script.
var inlineImageTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

// attachmentRow is one projected `objectAttachments` row — the byte-plane
// metadata for a single object plus the owner keys it is linked to. owners is
// the list filter input for "documents for this applicant".
type attachmentRow struct {
	EntityKey   string `json:"entityKey"`
	StoreName   string `json:"storeName"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Owners      []struct {
		OwnerKey string `json:"ownerKey"`
	} `json:"owners"`
}

// documentView is the Documents-tab projection of one attached object: the oid
// (the stable address for view / detach), its display metadata, and the owner it
// is attached to within the applicant's scope.
type documentView struct {
	OID         string `json:"oid"`
	OwnerKey    string `json:"ownerKey"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// objectLinkKey reconstructs lnk.object.<oid>.<linkName>.<tgtType>.<tgtId> from
// the object id and the full vtx.<type>.<id> target — deterministic, no scan.
func objectLinkKey(oid, targetKey, linkName string) (string, error) {
	parts := strings.Split(targetKey, ".")
	if len(parts) != 3 || parts[0] != "vtx" {
		return "", fmt.Errorf("targetKey must be vtx.<type>.<id>: %q", targetKey)
	}
	return "lnk.object." + oid + "." + linkName + "." + parts[1] + "." + parts[2], nil
}

// handleObjects routes /api/objects:
//
//	POST   /api/objects                              → upload bytes + AttachObject
//	GET    /api/objects?owner=     (or ?applicant=)  → list objects scoped to an owner key
//	GET    /api/objects/<oid>                        → stream the bytes back
//	DELETE /api/objects/<oid>?targetKey=&linkName=   → DetachObject
func (s *server) handleObjects(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/objects")
	rest = strings.Trim(rest, "/")
	switch {
	case r.Method == http.MethodPost && rest == "":
		s.handleObjectUpload(w, r)
	case r.Method == http.MethodGet && rest == "":
		s.handleObjectList(w, r)
	case r.Method == http.MethodGet && rest != "":
		s.handleObjectGet(w, r, rest)
	case r.Method == http.MethodDelete && rest != "":
		s.handleObjectDetach(w, r, rest)
	default:
		s.writeError(w, http.StatusBadRequest,
			"expected POST /api/objects, GET /api/objects?applicant=, GET /api/objects/<oid>, or DELETE /api/objects/<oid>?targetKey=&linkName=")
	}
}

// computeDocuments assembles the Documents rows from the `objectAttachments` lens
// read model: it keeps the lens-prefixed keys, decodes each row, and — when owners
// is non-empty — keeps only objects linked to one of those owner keys (the
// applicant's identity + each of their applications; the trusted-tool view scope).
// An empty owners set lists every owned object (the operator-style view). Rows
// sort by oid for a stable view.
func computeDocuments(keys []string, get kvGetter, owners []string) []documentView {
	scope := make(map[string]bool, len(owners))
	for _, o := range owners {
		if o != "" {
			scope[o] = true
		}
	}
	out := make([]documentView, 0)
	for _, k := range keys {
		if !strings.HasPrefix(k, attachmentsKeyPrefix) {
			continue
		}
		raw, ok := get(k)
		if !ok {
			continue
		}
		var row attachmentRow
		if json.Unmarshal(raw, &row) != nil || row.EntityKey == "" {
			continue
		}
		oid := strings.TrimPrefix(row.EntityKey, "vtx.object.")
		matched := ""
		for _, o := range row.Owners {
			if o.OwnerKey == "" {
				continue // the degenerate {ownerKey:null} artifact of a zero-link object
			}
			if len(scope) == 0 || scope[o.OwnerKey] {
				matched = o.OwnerKey
				break
			}
		}
		if matched == "" {
			continue // not in this applicant's scope (or fully detached)
		}
		out = append(out, documentView{
			OID: oid, OwnerKey: matched, ContentType: row.ContentType, Size: row.Size,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OID < out[j].OID })
	return out
}

// handleObjectList implements GET /api/objects?owner= — the objects attached to
// one or more owner keys, served from the `objectAttachments` lens rows in the
// shared weaver-targets read model (NOT Core KV; P5). The owner key is generic: an
// applicant's leaseapp / identity (the Documents tab) OR a unit (listing photos).
// `owner` may repeat (`?owner=a&owner=b`) to union an applicant's identity + every
// application into one "all my documents" view. `applicant` is accepted as a
// backward-compatible single-owner alias; omit both to list every object. The lens
// projects objects by ownerKey, so the same list-and-filter path serves any owner.
func (s *server) handleObjectList(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	bucket := bootstrap.WeaverTargetsBucket
	keys, err := conn.KVListKeys(ctx, bucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway,
			"list "+bucket+": "+err.Error()+" (is objects-base installed and the Refractor projecting?)")
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, bucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	var owners []string
	for _, o := range r.URL.Query()["owner"] {
		if o = strings.TrimSpace(o); o != "" {
			owners = append(owners, o)
		}
	}
	if len(owners) == 0 {
		if a := strings.TrimSpace(r.URL.Query().Get("applicant")); a != "" {
			owners = append(owners, a)
		}
	}
	docs := computeDocuments(keys, get, owners)
	s.writeJSON(w, http.StatusOK, map[string]any{"documents": docs, "count": len(docs)})
}

// handleObjectUpload implements POST /api/objects. It streams the file part to
// the core-objects store (cap enforced in substrate), derives the
// content-addressed oid, then submits AttachObject. Bytes first, then graph: a
// failed op leaves only collectable bytes, never a partial graph. The read set
// is [targetKey] only — the owner the FE already knows — so the app never probes
// Core KV (P5).
func (s *server) handleObjectUpload(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	if s.adminActor == "" {
		s.writeError(w, http.StatusBadGateway,
			"admin actor not loaded; a valid bootstrap file (BOOTSTRAP_JSON_PATH) is required to submit ops")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.uploadCap+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		s.writeError(w, http.StatusBadRequest, "parse multipart form: "+err.Error())
		return
	}
	targetKey := strings.TrimSpace(r.FormValue("targetKey"))
	linkName := strings.TrimSpace(r.FormValue("linkName"))
	if targetKey == "" || linkName == "" {
		s.writeError(w, http.StatusBadRequest, "targetKey and linkName form fields are required")
		return
	}
	if _, err := objectLinkKey("x", targetKey, linkName); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "a 'file' part is required: "+err.Error())
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if len(contentType) > 255 {
		contentType = contentType[:255]
	}

	ctx, cancel := s.reqContext(r)
	defer cancel()

	storeName, err := substrate.NewNanoID()
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "generate store name: "+err.Error())
		return
	}
	info, err := conn.ObjectPut(ctx, bootstrap.CoreObjectsBucket, storeName, file, s.uploadCap)
	if err != nil {
		if errors.Is(err, substrate.ErrObjectTooLarge) {
			s.writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("upload exceeds the %d-byte cap", s.uploadCap))
			return
		}
		s.writeError(w, http.StatusBadGateway, "store object bytes: "+err.Error())
		return
	}
	oid := substrate.SHA256NanoID("object:" + info.Digest)

	payload := map[string]any{
		"digest": info.Digest, "size": info.Size, "contentType": contentType,
		"storeName": storeName, "targetKey": targetKey, "linkName": linkName,
	}
	if header.Filename != "" {
		payload["filename"] = header.Filename
	}

	// Deterministic requestId (CC6): content-derived so a re-submitted upload of
	// the same bytes to the same slot collapses on the tracker.
	requestID := substrate.DeriveNanoID(attachReqNamespace,
		strings.Join([]string{info.Digest, targetKey, linkName}, "\x00"))
	env := &processor.OperationEnvelope{
		RequestID:     requestID,
		Lane:          processor.LaneDefault,
		OperationType: "AttachObject",
		Actor:         s.adminActor,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "object",
		Payload:       mustJSON(payload),
		ContextHint:   &processor.ContextHint{Reads: []string{targetKey}},
	}
	reply, err := output.SubmitOp(ctx, conn, env)
	if err != nil {
		// The op never landed → our just-uploaded bytes are an orphan; reclaim.
		_ = conn.ObjectDelete(ctx, bootstrap.CoreObjectsBucket, storeName)
		s.writeError(w, http.StatusBadGateway, "submit AttachObject: "+err.Error())
		return
	}
	if reply.Status == processor.ReplyStatusRejected {
		_ = conn.ObjectDelete(ctx, bootstrap.CoreObjectsBucket, storeName)
		s.writeJSON(w, http.StatusBadRequest, reply)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"oid": oid, "linkName": linkName, "targetKey": targetKey,
		"size": info.Size, "contentType": contentType,
	})
}

// handleObjectGet implements GET /api/objects/<oid>. It resolves the storeName
// from the `objectAttachments` lens read model (NOT Core KV; P5) and streams the
// bytes (NATS verifies the digest as it streams). The Refractor is never in the
// byte path.
func (s *server) handleObjectGet(w http.ResponseWriter, r *http.Request, oid string) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	if !substrate.IsValidNanoID(oid) {
		s.writeError(w, http.StatusBadRequest, "invalid object id")
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	storeName, contentType, ok := s.resolveObject(ctx, conn, oid)
	if !ok {
		s.writeError(w, http.StatusNotFound, "object not found in the read model")
		return
	}

	rc, info, err := conn.ObjectGet(ctx, bootstrap.CoreObjectsBucket, storeName)
	if err != nil {
		if errors.Is(err, substrate.ErrObjectNotFound) {
			s.writeError(w, http.StatusNotFound, "object bytes not found")
			return
		}
		s.writeError(w, http.StatusBadGateway, "read object bytes: "+err.Error())
		return
	}
	defer rc.Close()

	// Only the raster-image allow-list is served with its declared type + inline;
	// every other type (svg / html / pdf / unknown) is forced to a neutral
	// octet-stream attachment so an uploaded active document can never run as
	// same-origin script. The CSP is the belt.
	ct := contentType
	disposition := "attachment"
	if inlineImageTypes[ct] {
		disposition = "inline"
	} else {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatUint(info.Size, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// resolveObject finds an object's storeName + contentType in the
// `objectAttachments` read model by oid (NOT Core KV; P5). It lists the lens keys
// and matches the row whose entityKey is vtx.object.<oid> — the same list-and-
// filter pattern the other vertical-app readers use.
func (s *server) resolveObject(ctx context.Context, conn *substrate.Conn, oid string) (storeName, contentType string, ok bool) {
	bucket := bootstrap.WeaverTargetsBucket
	keys, err := conn.KVListKeys(ctx, bucket)
	if err != nil {
		return "", "", false
	}
	want := "vtx.object." + oid
	for _, k := range keys {
		if !strings.HasPrefix(k, attachmentsKeyPrefix) {
			continue
		}
		entry, err := conn.KVGet(ctx, bucket, k)
		if err != nil {
			continue
		}
		var row attachmentRow
		if json.Unmarshal(entry.Value, &row) != nil {
			continue
		}
		if row.EntityKey == want && row.StoreName != "" {
			return row.StoreName, row.ContentType, true
		}
	}
	return "", "", false
}

// handleObjectDetach implements DELETE /api/objects/<oid>?targetKey=&linkName=.
// The read set is the deterministic link + object keys (both known-present for a
// document the app is detaching) — no Core KV probe (P5). linkName must be
// supplied by the caller; the FE knows it for session-uploaded documents.
func (s *server) handleObjectDetach(w http.ResponseWriter, r *http.Request, oid string) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	if s.adminActor == "" {
		s.writeError(w, http.StatusBadGateway,
			"admin actor not loaded; a valid bootstrap file is required to submit ops")
		return
	}
	if !substrate.IsValidNanoID(oid) {
		s.writeError(w, http.StatusBadRequest, "invalid object id")
		return
	}
	targetKey := strings.TrimSpace(r.URL.Query().Get("targetKey"))
	linkName := strings.TrimSpace(r.URL.Query().Get("linkName"))
	if targetKey == "" || linkName == "" {
		s.writeError(w, http.StatusBadRequest, "targetKey and linkName query params are required")
		return
	}
	linkKey, err := objectLinkKey(oid, targetKey, linkName)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	objKey := "vtx.object." + oid

	ctx, cancel := s.reqContext(r)
	defer cancel()

	requestID, err := substrate.NewNanoID()
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "generate request id: "+err.Error())
		return
	}
	env := &processor.OperationEnvelope{
		RequestID:     requestID,
		Lane:          processor.LaneDefault,
		OperationType: "DetachObject",
		Actor:         s.adminActor,
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
		Class:         "object",
		Payload:       mustJSON(map[string]any{"oid": oid, "targetKey": targetKey, "linkName": linkName}),
		ContextHint:   &processor.ContextHint{Reads: []string{linkKey, objKey}},
	}
	reply, err := output.SubmitOp(ctx, conn, env)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "submit DetachObject: "+err.Error())
		return
	}
	status := http.StatusOK
	if reply.Status == processor.ReplyStatusRejected {
		status = http.StatusBadRequest
	}
	s.writeJSON(w, status, reply)
}
