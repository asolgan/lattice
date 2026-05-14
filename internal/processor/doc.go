// Package processor implements the Lattice Processor — the single
// sanctioned writer to Core KV (architecture principle P2). It consumes
// operation envelopes from the `core-operations` JetStream, runs them
// through the 10-step commit path, and atomically commits mutations +
// idempotency tracker to Core KV.
//
// Story 1.5 scope: steps 1-3 only.
//
//	step 1: consume an operation envelope (JetStream pull consumer)
//	step 2: dedup against the idempotency tracker (Core KV vtx.op.<requestId>)
//	step 3: authorize via the Authorizer interface (StubAuthorizer always allows)
//
// Steps 4-10 (Hydrate, Execute, Validate, Atomic Commit, Event Publish,
// Ack) are defined as Go interfaces in steps_4_10_stub.go with no-op
// implementations that log "step N: stubbed" and return success. Stories
// 1.6, 1.7, 1.8 progressively replace these stubs with real logic — each
// behind the same interface, so the wiring stays stable.
//
// Wire layout:
//
//	cmd/processor/main.go                – binary entry point
//	internal/processor/envelope.go       – OperationEnvelope + Reply types per Contract #2
//	internal/processor/step1_consume.go  – pull consumer + envelope parse
//	internal/processor/step2_dedup.go    – tracker lookup
//	internal/processor/step3_auth.go     – Authorizer interface + StubAuthorizer
//	internal/processor/steps_4_10_stub.go – stub interfaces for downstream steps
//	internal/processor/commit_path.go    – top-level driver wiring 1-3 + stubbed 4-10
//	internal/processor/reply.go          – Reply envelope construction
//	internal/processor/tracker.go        – tracker entry shape + atomic batch
//	internal/processor/health.go         – periodic health heartbeat
//
// All KV / atomic-batch operations go through internal/substrate. The
// JetStream pull consumer is the one place processor talks directly to the
// jetstream package (substrate does not yet wrap stream consumers).
package processor
