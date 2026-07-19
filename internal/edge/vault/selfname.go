package vault

import (
	"context"
	"encoding/json"
	"time"
)

// manifestMeKey is the Personal Lens row carrying the signed-in identity's
// own presentation fields (packages/edge-manifest's edgeIdentitySpec).
const manifestMeKey = "manifest.me"

// decryptTimeout bounds one row decoration. A session-key request is a
// control-plane round trip, and Decorate runs on the sync manager's delivery
// goroutine — an unreachable control plane must degrade the label, never
// wedge manifest delivery.
const decryptTimeout = 5 * time.Second

// SelfName fills a `manifest.me` row's `displayName` by decrypting the
// sealed `name` envelope the lens projects as `sealedName`
// (display-name-convention-design.md §3 N3, class 3 "self").
//
// Identity names are PII: sealed at rest and never projected as plaintext
// into a broadcast KV row (Contract #3 §3.10; the design's D3). The lens
// therefore carries the { ct, nonce, keyId } envelope and the plaintext
// exists only here, in the memory of the engine running as that identity —
// the decorated row is handed straight to the renderer and is never written
// back to the Local VAL Store.
//
// Every failure mode degrades to "leave the row alone": a shredded identity,
// a refused session key, an unreachable control plane, or a row that simply
// has no sealed name all leave `displayName` absent, which the renderer's
// floor rule (§2) paints as the typed fallback rather than a bare NanoID.
// That is what keeps the shred story true at the display surface — the name
// stops rendering the moment the key is gone.
type SelfName struct {
	client *Client
}

// NewSelfName builds a decorator over client. A nil client yields a
// decorator that passes every row through unchanged — the posture for an
// engine wired without a Vault control plane.
func NewSelfName(client *Client) *SelfName {
	if client == nil {
		return nil
	}
	return &SelfName{client: client}
}

// Decorate returns data with `displayName` filled from `sealedName`, or data
// unchanged when it cannot be. Safe on a nil receiver.
//
// A row that already carries a non-null `displayName` is passed through
// untouched: the lens projects `identity.name.data.value` as well, which
// resolves on a stack whose sensitive aspects were never sealed (an
// in-process test harness with no Vault), and a plaintext name that is
// already there needs no key.
func (s *SelfName) Decorate(ctx context.Context, key string, data json.RawMessage) json.RawMessage {
	if s == nil || key != manifestMeKey || len(data) == 0 {
		return data
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil {
		return data
	}
	if isNonNull(row["displayName"]) {
		return data
	}
	ct, ok := ciphertextEnvelope(row["sealedName"])
	if !ok {
		return data
	}

	ctx, cancel := context.WithTimeout(ctx, decryptTimeout)
	defer cancel()
	plaintext, err := s.client.Decrypt(ctx, ct)
	if err != nil {
		s.client.logger.Debug("edge/vault: self-name decrypt failed; renderer falls back to the typed label", "err", err)
		return data
	}

	// The sealed plaintext is the aspect's whole `data` object as the
	// Processor's step 6.5 marshalled it (internal/processor/step65_encrypt.go),
	// i.e. identity-domain's name aspect shape {"value": "..."}.
	var aspect struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(plaintext, &aspect); err != nil || aspect.Value == "" {
		return data
	}
	name, err := json.Marshal(aspect.Value)
	if err != nil {
		return data
	}
	row["displayName"] = name
	decorated, err := json.Marshal(row)
	if err != nil {
		return data
	}
	return decorated
}

// isNonNull reports whether a raw field is present and is not JSON null.
func isNonNull(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}
