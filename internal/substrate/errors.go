package substrate

import (
	"errors"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Sentinel errors returned by KV and AtomicBatch operations.
//
// ErrKeyNotFound is returned by KVGet when the requested key does not exist
// (and by KVUpdate/KVDelete when revision conditions reference a key the
// underlying store has no record of).
//
// ErrRevisionConflict is returned by KVCreate / KVUpdate / KVDelete when the
// caller's expected revision does not match the current revision. For
// KVCreate this means the key already exists (create-if-absent failed).
//
// ErrAtomicBatchRejected wraps NATS atomic-batch publish failures (any
// revision-condition rejection, header malformation, or stream-level
// rejection inside the batch). Callers can use errors.Is and the underlying
// error from a wrapping fmt.Errorf chain to extract specifics.
//
// ErrBucketNotFound is returned by KVStatus when the named bucket (or its
// backing stream) does not exist. It is the substrate-typed equivalent of
// jetstream.ErrBucketNotFound / ErrStreamNotFound, letting callers classify a
// missing target as a structural fault without importing jetstream.
//
// ErrBatchTooLarge is returned by AtomicBatch/PublishBatch when the op count
// exceeds MaxBatchMessages. NOT wrapped in ErrAtomicBatchRejected — it is a
// pre-flight guard (Contract #3 §3.9.1), never a NATS-reported rejection.
//
// ErrValueTooLarge is returned by AtomicBatch/PublishBatch when a single op's
// value exceeds the per-message payload ceiling (the connection's negotiated
// max_payload, less ValueHeadroomBytes).
var (
	ErrKeyNotFound         = errors.New("substrate: key not found")
	ErrRevisionConflict    = errors.New("substrate: revision conflict")
	ErrAtomicBatchRejected = errors.New("substrate: atomic batch rejected")
	ErrBucketNotFound      = errors.New("substrate: bucket not found")
	ErrBatchTooLarge       = errors.New("substrate: atomic batch exceeds message-count ceiling")
	ErrValueTooLarge       = errors.New("substrate: batch op value exceeds payload ceiling")
)

// IsConnectionError reports whether err is (or wraps) a NATS transport-level
// connection failure — the server is unreachable, the connection was lost, or is
// draining/disconnected. Callers classify these as infrastructure faults
// (pause + probe) without importing nats.go to name the sentinels.
func IsConnectionError(err error) bool {
	return errors.Is(err, nats.ErrConnectionClosed) ||
		errors.Is(err, nats.ErrConnectionDraining) ||
		errors.Is(err, nats.ErrDisconnected) ||
		errors.Is(err, nats.ErrNoServers)
}

// IsInvalidKeyError reports whether err is (or wraps) a NATS KV rejection of
// a malformed key (outside the allowed key charset) from KVPut/KVCreate/
// KVUpdate/KVDelete. Unlike a transient infra fault, this can never succeed
// on retry — the key itself is unwritable — so callers classify it as
// permanently-undeliverable (e.g. Term a poison message) rather than
// redelivering forever. Named without requiring callers to import jetstream.
func IsInvalidKeyError(err error) bool {
	return errors.Is(err, jetstream.ErrInvalidKey)
}
