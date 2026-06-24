package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/asolgan/lattice/internal/bootstrap"
)

// vertexRow is one entry in the Core KV vertex list. Type is the Contract #1
// type segment (the "what is it" hint: identity / role / meta / package / …);
// Label is a best-effort human name (a canonicalName, or a name/title/
// operationType from the root document) — empty when none is available.
type vertexRow struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	Label     string `json:"label,omitempty"`
	IsDeleted bool   `json:"isDeleted,omitempty"`
}

// aspectRow is one of a vertex's aspects (vtx.<type>.<id>.<localName>). The
// document is lazy-loaded on expand via /api/corekv/entry, so only the key and
// local name are listed here.
type aspectRow struct {
	Key       string `json:"key"`
	LocalName string `json:"localName"`
}

// linkRow is one link touching a vertex, in either direction. Direction is
// "out" when the vertex is the link source (the later-arriving vertex, Contract
// #1 §1.1) and "in" when it is the target. Relation reads as "source <relation>
// target"; OtherKey/OtherType name the vertex at the far end.
type linkRow struct {
	Key       string `json:"key"`
	Relation  string `json:"relation"`
	Direction string `json:"direction"`
	OtherKey  string `json:"otherKey"`
	OtherType string `json:"otherType"`
}

// vertexDetail is the GET /api/vertex response: a vertex's root document plus
// the keys of its aspects and links (documents lazy-loaded on expand).
type vertexDetail struct {
	Key       string          `json:"key"`
	Class     string          `json:"class"`
	Revision  uint64          `json:"revision"`
	IsDeleted bool            `json:"isDeleted"`
	Envelope  json.RawMessage `json:"envelope"`
	Aspects   []aspectRow     `json:"aspects"`
	Links     []linkRow       `json:"links"`
}

// vertexType returns the Contract #1 type segment of a key (segment 1).
func vertexType(key string) string {
	segs := strings.SplitN(key, ".", 3)
	if len(segs) >= 2 {
		return segs[1]
	}
	return ""
}

// dataLabel picks a short human label from a root document's data, trying the
// common identifying fields in priority order. Verbose fields (e.g. note) are
// deliberately excluded so the list stays scannable.
func dataLabel(data map[string]any) string {
	return dataString(data, "name", "canonicalName", "title", "operationType")
}

// buildVertexList selects the vertex/meta roots in keys (filtered by prefix,
// capped at limit) and resolves each one's label + isDeleted. The label comes
// from the root document's data; for vertices that carry their name in a
// .canonicalName aspect instead (role/meta/permission/lens), that aspect is read
// only when the root yielded no label.
func buildVertexList(keys []string, get kvGetter, prefix string, limit int) (rows []vertexRow, truncated bool) {
	keyset := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keyset[k] = struct{}{}
	}
	rows = make([]vertexRow, 0, limit)
	for _, k := range keys {
		if cls := classifyKey(k); cls != classVertex && cls != classMeta {
			continue
		}
		if prefix != "" && !strings.HasPrefix(k, prefix) {
			continue
		}
		if len(rows) >= limit {
			truncated = true
			break
		}
		row := vertexRow{Key: k, Type: vertexType(k)}
		if raw, ok := get(k); ok {
			var env struct {
				IsDeleted bool           `json:"isDeleted"`
				Data      map[string]any `json:"data"`
			}
			if json.Unmarshal(raw, &env) == nil {
				row.IsDeleted = env.IsDeleted
				row.Label = dataLabel(env.Data)
			}
		}
		if row.Label == "" {
			if _, ok := keyset[k+".canonicalName"]; ok {
				row.Label = dataString(metaData(get, k+".canonicalName"), "value", "name", "canonicalName")
			}
		}
		rows = append(rows, row)
	}
	return rows, truncated
}

// linkForVertex parses a 6-segment link key and, when vtxKey is one of its
// endpoints, returns the relation + direction + the other endpoint. It reports
// false when the key is malformed or unrelated to vtxKey.
func linkForVertex(linkKey, vtxKey string) (linkRow, bool) {
	segs := strings.Split(linkKey, ".")
	if len(segs) != 6 || segs[0] != "lnk" {
		return linkRow{}, false
	}
	relation := segs[3]
	sourceKey := "vtx." + segs[1] + "." + segs[2]
	targetKey := "vtx." + segs[4] + "." + segs[5]
	switch vtxKey {
	case sourceKey:
		return linkRow{Key: linkKey, Relation: relation, Direction: "out", OtherKey: targetKey, OtherType: segs[4]}, true
	case targetKey:
		return linkRow{Key: linkKey, Relation: relation, Direction: "in", OtherKey: sourceKey, OtherType: segs[1]}, true
	}
	return linkRow{}, false
}

// buildVertexDetail assembles a vertex's detail from its root bytes/revision and
// the full key list: the root document, its direct aspects, and every link in
// which it is the source or target. Documents for aspects/links are not read
// here — the UI lazy-loads them via /api/corekv/entry on expand.
func buildVertexDetail(rootKey string, rootRaw []byte, revision uint64, allKeys []string) vertexDetail {
	vd := vertexDetail{
		Key:      rootKey,
		Revision: revision,
		Aspects:  []aspectRow{},
		Links:    []linkRow{},
	}
	var env struct {
		Class     string `json:"class"`
		IsDeleted bool   `json:"isDeleted"`
	}
	_ = json.Unmarshal(rootRaw, &env)
	vd.Class = env.Class
	vd.IsDeleted = env.IsDeleted
	if json.Valid(rootRaw) {
		vd.Envelope = rootRaw
	}

	aspectPrefix := rootKey + "."
	for _, k := range allKeys {
		switch {
		case strings.HasPrefix(k, aspectPrefix) && classifyKey(k) == classAspect:
			localName := strings.TrimPrefix(k, aspectPrefix)
			if !strings.Contains(localName, ".") {
				vd.Aspects = append(vd.Aspects, aspectRow{Key: k, LocalName: localName})
			}
		case strings.HasPrefix(k, "lnk."):
			if lr, ok := linkForVertex(k, rootKey); ok {
				vd.Links = append(vd.Links, lr)
			}
		}
	}
	sort.Slice(vd.Aspects, func(i, j int) bool { return vd.Aspects[i].LocalName < vd.Aspects[j].LocalName })
	sort.Slice(vd.Links, func(i, j int) bool {
		if vd.Links[i].Relation != vd.Links[j].Relation {
			return vd.Links[i].Relation < vd.Links[j].Relation
		}
		return vd.Links[i].OtherKey < vd.Links[j].OtherKey
	})
	return vd
}

// handleVertices implements GET /api/vertices?prefix=&limit= — the Core KV
// vertex list (vertices + meta-vertices only, each with a type + label).
func (s *server) handleVertices(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	limit := defaultCoreKVLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	keys, err := conn.KVListKeys(ctx, bootstrap.CoreKVBucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "list core-kv: "+err.Error())
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, bootstrap.CoreKVBucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	rows, truncated := buildVertexList(keys, get, prefix, limit)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"vertices":  rows,
		"count":     len(rows),
		"truncated": truncated,
		"limit":     limit,
	})
}

// handleVertex implements GET /api/vertex?key= — a vertex's root document plus
// the keys of its aspects and bidirectional links.
func (s *server) handleVertex(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		s.writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	ctx, cancel := s.reqContext(r)
	defer cancel()

	entry, err := conn.KVGet(ctx, bootstrap.CoreKVBucket, key)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "get "+key+": "+err.Error())
		return
	}
	keys, err := conn.KVListKeys(ctx, bootstrap.CoreKVBucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "list core-kv: "+err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, buildVertexDetail(key, entry.Value, entry.Revision, keys))
}
