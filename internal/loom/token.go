package loom

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/asolgan/lattice/internal/substrate"
)

// deriveRequestID returns a deterministic 20-char NanoID (over the canonical
// Lattice alphabet, Contract #1) for the step at cursor within instance. It is
// the step's write-ahead pendingToken AND the op's requestId — the single
// token that makes systemOp submission idempotent: a re-attempt after a crash
// reuses the same requestId and collapses on the Contract #4 vtx.op.<requestId>
// tracker (Crash-safety invariant 1; exactly-once, AC #6).
func deriveRequestID(instanceID string, cursor int) string {
	var seed [8]byte
	binary.BigEndian.PutUint64(seed[:], uint64(cursor))
	sum := sha256.Sum256(append([]byte(instanceID+":"), seed[:]...))
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
