// Package bridge is the platform's generic, trusted-infra egress — the one
// component that makes OUTBOUND calls to external systems (payment, background
// check, …). It is a durable consumer on events.external.> that, for each event,
// dispatches to a named adapter, calls it idempotently, and posts a result op
// back to core-operations. It owns the adapter registry. See
// docs/components/bridge.md for the full design.
//
// The bridge is vertex-type-agnostic: it treats instanceKey/externalRef/
// idempotencyKey as a single OPAQUE correlation token and never parses or
// assumes the claim vertex's type. The only Core KV read it makes is the generic
// Contract #4 op-tracker GET (vtx.op.<requestId>) — the same key shape for every
// op, never a typed claim-vertex read.
//
// External calls are at-most-once-effective under at-least-once event
// redelivery and crash/retry, via three mechanisms:
//
//  1. Deterministic result-op requestId (pinned, Contract #10 §10.3). The
//     result op's requestId is deriveReplyRequestID(instanceKey), so a
//     redelivered external.* event re-submits the SAME requestId and collapses
//     on the Contract #4 vtx.op.<requestId> tracker — exactly one mutation.
//  2. Adapter idempotencyKey dedup. The adapter is called with
//     idempotencyKey = instanceKey and dedups the real external action on it, so
//     even a redelivered event that re-reaches the adapter performs no duplicate
//     external action. Correctness holds via (1) + (2) without any vertex read.
//  3. (Optional) skip-on-redelivery. Before dispatching, the bridge MAY GET the
//     generic op tracker for its deterministic requestId and ack without
//     re-calling if the result already landed — a pure optimization that (2)
//     would dedup anyway.
//
// State & crash-safety: the bridge keeps no durable outbox of its own. Its
// durable state is the events.external.> consumer ack floor and the Contract #4
// op tracker (owned by the Processor). The ack is the commit point, and the
// deterministic reply requestId makes redelivery idempotent; an un-acked event
// is simply redelivered. The bridge persists no cursor to keep atomic with the
// publish, so there is no dual write to break.
//
// P2 / module boundary: the bridge writes NOTHING to Core KV directly (the
// result mutation is the Processor's, applied when the replyOp commits). It only
// SUBMITS the replyOp (a publish to ops.<lane>) and READS Core KV (the optional
// generic tracker GET); its only write is the Contract #5 heartbeat to the
// SEPARATE health-kv bucket. This package imports only internal/substrate; all
// cross-component interaction is over NATS.
package bridge
