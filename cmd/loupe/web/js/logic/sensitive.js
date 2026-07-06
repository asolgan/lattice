// Pure Reveal shaping (F12 §3.2): detecting a sensitive aspect's Contract #3
// §3.10 { ct, nonce, keyId } ciphertext envelope and rendering its sealed
// summary line. No DOM, no fetch — goja-tested via cmd/loupe/web_logic_test.go,
// mirroring how logic/keys.js mirrors the Go classifyKey.

// isSealedAspect reports whether an aspect's data is a ciphertext envelope
// rather than plaintext — the shape the Processor commit-path leaves for any
// aspect whose DDL declares sensitive: true.
function isSealedAspect(data) {
  return !!data && typeof data === "object" && !Array.isArray(data) &&
    typeof data.ct === "string" && data.ct.length > 0 &&
    typeof data.nonce === "string" && data.nonce.length > 0 &&
    typeof data.keyId === "string" && data.keyId.length > 0;
}

// sealedSummary renders the sealed-row headline: "encrypted at rest · <keyId>"
// with a long keyId shortened so it doesn't dominate the row.
function sealedSummary(data) {
  var keyId = (data && data.keyId) || "?";
  var shortKeyId = keyId.length > 12 ? keyId.slice(0, 8) + "…" : keyId;
  return "encrypted at rest · " + shortKeyId;
}

export { isSealedAspect, sealedSummary };
