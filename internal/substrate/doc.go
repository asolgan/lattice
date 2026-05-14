// Package substrate is the lowest-level shared package in Lattice. It
// provides the primitives every component needs: NanoID generation, key
// builders, document envelope construction, opinionated NATS/JetStream KV
// helpers, and the atomic-batch publish helper.
//
// All Lattice components (Processor, Refractor, bootstrap, identity ops,
// CLI tooling) must use substrate rather than duplicating low-level NATS
// code or re-deriving the Contract #1 addressing primitives.
//
// Design principles:
//
//   - Lattice-specific, not generic. The NanoID alphabet, key shape, and
//     envelope structure are dictated by Contract #1 of the architecture's
//     data contracts. Generic abstractions are intentionally avoided.
//
//   - Hide layered NATS APIs behind *Conn. Callers see KV operations and
//     atomic batches; they do not manage nats.Conn, jetstream.JetStream
//     or jetstream.KeyValue separately.
//
//   - Programmer errors panic. Key-builder validators reject malformed
//     inputs by panicking — keys are never constructed from untrusted
//     input directly; the upstream parser is the trust boundary.
//
//   - Operational failures return typed sentinel errors so callers can
//     branch on them: ErrKeyNotFound, ErrRevisionConflict,
//     ErrAtomicBatchRejected.
package substrate
