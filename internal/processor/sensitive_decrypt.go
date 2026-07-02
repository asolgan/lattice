package processor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/vault"
)

// decryptSensitiveDoc replaces doc.Data with its decrypted plaintext when
// doc's class resolves to a sensitive DDL (Contract #3 §3.10) — the
// decrypt-on-read half of commit-path step 6.5's encrypt-on-write, shared by
// step 4's contextHint.reads hydration and the lazy kv.Read() seam
// (connKVReader). ddls or v nil, or doc's class not found / not sensitive,
// leaves doc untouched: the aspect's ciphertext shape passes through as
// opaque data, the safe default for a pipeline that never wired a Vault
// (most test harnesses that do not exercise PII).
func decryptSensitiveDoc(ctx context.Context, conn *substrate.Conn, bucket string, ddls *DDLCache, v vault.Vault, doc *VertexDoc) error {
	if ddls == nil || v == nil || doc == nil {
		return nil
	}
	ref, ok := ddls.Lookup(doc.Class)
	if !ok || !ref.Sensitive {
		return nil
	}
	vertexKey, vertexType, _, _, ok := substrate.ParseAspectKey(doc.Key)
	if !ok || vertexType != "identity" {
		// A malformed or non-identity-anchored sensitive aspect should never
		// have committed (step 6 rejects it at write time) — decrypt-on-read
		// is not the place to re-litigate that; leave the document as-is.
		return nil
	}
	envelope, err := readPiiKeyEnvelope(ctx, conn, bucket, vertexKey)
	if err != nil {
		return fmt.Errorf("read piiKey for %s: %w", doc.Key, err)
	}
	ct, err := ciphertextFromData(doc.Data)
	if err != nil {
		return fmt.Errorf("parse ciphertext for %s: %w", doc.Key, err)
	}
	plaintext, err := v.Decrypt(ctx, vertexKey, envelope, ct)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", doc.Key, err)
	}
	var value map[string]interface{}
	if err := json.Unmarshal(plaintext, &value); err != nil {
		return fmt.Errorf("unmarshal decrypted %s: %w", doc.Key, err)
	}
	doc.Data = value
	return nil
}

// readPiiKeyEnvelope reads and parses vertexKey's piiKey aspect. Internal
// Processor bookkeeping — never declared in a script's contextHint.reads;
// Starlark never sees the envelope, only the decrypted plaintext (design
// §2.2's "Starlark stays pure" guarantee).
func readPiiKeyEnvelope(ctx context.Context, conn *substrate.Conn, bucket, vertexKey string) (vault.Envelope, error) {
	entry, err := conn.KVGet(ctx, bucket, vertexKey+".piiKey")
	if err != nil {
		return vault.Envelope{}, err
	}
	var doc struct {
		Data vault.Envelope `json:"data"`
	}
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		return vault.Envelope{}, err
	}
	return doc.Data, nil
}

// ciphertextFromData re-parses an aspect's generically-decoded Data map back
// into a vault.Ciphertext with proper []byte fields. The first json.Unmarshal
// (into VertexDoc.Data map[string]interface{}) decodes CT/Nonce as base64
// strings rather than bytes; round-tripping through JSON a second time, this
// time into the typed struct, is the simplest way to recover the []byte
// shape without threading raw bytes through VertexDoc's generic map.
func ciphertextFromData(data map[string]interface{}) (vault.Ciphertext, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return vault.Ciphertext{}, err
	}
	var ct vault.Ciphertext
	if err := json.Unmarshal(raw, &ct); err != nil {
		return vault.Ciphertext{}, err
	}
	return ct, nil
}
