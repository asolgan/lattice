package bridge

import (
	"crypto/sha256"

	"github.com/asolgan/lattice/internal/substrate"
)

// replyRequestNamespace prefixes the hash input so a bridge result-op requestId
// can never collide with a Loom-derived id (Loom namespaces its derivations
// "", "task:", "instance:") for the same opaque value.
const replyRequestNamespace = "bridge:reply:"

// deriveReplyRequestID returns the deterministic result-op requestId for an
// external call, derived solely from the opaque instanceKey. A redelivered
// external.* event therefore yields the SAME requestId, so the re-submitted
// replyOp collapses on the Contract #4 vtx.op.<requestId> tracker — exactly one
// result mutation (the pinned FR58 invariant, Contract #10 §10.3). The
// instanceKey is treated as an opaque token: its type segment, if any, is never
// parsed. The derivation is pure (no stored map), so a fresh replica or a
// restart computes the identical id from the same instanceKey, which is what
// makes redelivery-after-crash collapse correctly without shared state.
//
// The output is a bare NanoID over the canonical Lattice alphabet (Contract #1),
// so it is a valid dot-free op requestId.
func deriveReplyRequestID(instanceKey string) string {
	return deriveID(replyRequestNamespace, instanceKey)
}

// deriveID is the shared deterministic NanoID derivation: a stable hash over
// namespace+input expanded across the canonical alphabet. The namespace prefix
// keeps disjoint derivations from colliding for the same input.
func deriveID(namespace, input string) string {
	sum := sha256.Sum256([]byte(namespace + input))
	id := make([]byte, substrate.NanoIDLength)
	// Expand the 32-byte digest across the id by re-hashing as needed.
	digest := sum[:]
	di := 0
	for i := 0; i < substrate.NanoIDLength; i++ {
		if di >= len(digest) {
			next := sha256.Sum256(digest)
			digest = next[:]
			di = 0
		}
		id[i] = substrate.Alphabet[int(digest[di])%len(substrate.Alphabet)]
		di++
	}
	return string(id)
}
