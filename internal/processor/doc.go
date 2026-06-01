// Package processor implements the Lattice Processor — the single
// sanctioned writer to Core KV (architecture principle P2). It consumes
// operation envelopes from the `core-operations` JetStream, runs them
// through the 9-step commit path, and atomically commits mutations +
// idempotency tracker to Core KV.
//
// The 9-step commit path:
//
//	step 1: consume an operation envelope (JetStream pull consumer)
//	step 2: dedup against the idempotency tracker (Core KV vtx.op.<requestId>)
//	step 3: authorize via the Authorizer interface (CapabilityAuthorizer or StubAuthorizer)
//	step 4: hydrate the ScriptContext from Core KV
//	step 5: execute the class Starlark script
//	step 6: validate the ScriptResult against DDL constraints
//	step 7: build the EventList
//	step 8: atomically commit mutations + tracker to Core KV
//	step 9: ack the JetStream message
//
// Event publishing is NOT a commit step: the faithful EventList is persisted
// in the step-8 atomic batch (vtx.op.<id>.events) and published asynchronously
// by the durable outbox consumer (internal/processor/outbox).
//
// Wire layout:
//
//	cmd/processor/main.go                – binary entry point
//	internal/processor/envelope.go       – OperationEnvelope + Reply types per Contract #2
//	internal/processor/step1_consume.go  – pull consumer + envelope parse
//	internal/processor/step2_dedup.go    – tracker lookup
//	internal/processor/step3_auth.go     – Authorizer interface + StubAuthorizer
//	internal/processor/step_interfaces.go – interfaces for downstream steps
//	internal/processor/commit_path.go    – top-level driver wiring 1-9
//	internal/processor/reply.go          – Reply envelope construction
//	internal/processor/tracker.go        – tracker entry shape + atomic batch
//	internal/processor/health.go         – periodic health heartbeat
//	internal/processor/outbox/          – durable outbox consumer + event publisher
//
// All KV / atomic-batch operations go through internal/substrate. The
// JetStream pull consumer is the one place processor talks directly to the
// jetstream package (substrate does not yet wrap stream consumers).
package processor
