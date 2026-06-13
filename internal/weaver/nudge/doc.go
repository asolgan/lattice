// Package nudge is Weaver's External Adapter framework and the mechanics of
// the Two-Phase Nudge protocol (Contract #10 §10.3 weaver-claims, FR58 / arch
// Item 3).
//
// The framework is the engine; reference adapters prove it. An Adapter is the
// unit of "call one external system idempotently": the framework hands the
// adapter an idempotencyKey (= the claimId minted atomically with the
// weaver-state mark CAS-create) and the adapter — not Weaver — is the de-dup
// boundary for the real external side-effect. Two Execute calls bearing the
// same idempotencyKey MUST produce one external action.
//
// The protocol is claim → execute → resolve: a claim record is written to
// weaver-claims.<claimId> with state=claimed BEFORE any external call (the
// NFR-S11 "visible claim state before executing"); the adapter is then called
// under state=executing with idempotencyKey=claimId; on success a resolve op is
// submitted through the Processor (a normal fire-and-forget publish, supplied
// by the caller as a callback so this package never holds an Actuator) and the
// claim advances to state=resolved recording resolveRef. Recovery is
// read-before-act: a claim found past its lease reuses the same claimId, checks
// for an already-landed resolve before re-executing, and never mints a fresh id
// for a corrupt (empty-claimId) mark.
//
// Module boundary (docs/components/weaver.md Principles): this package imports
// only internal/substrate. It never imports internal/processor and holds no raw
// nats.go/jetstream handle — the resolve goes out through the caller-supplied
// submit callback (the engine's Actuator), never a request-reply.
package nudge
