// Package planner implements the deterministic goal-regression search that
// underlies the Weaver planner mandate (weaver-planner-mandate-design.md §3.1,
// §3.3, Fire 3): given a goal predicate, a snapshot of known facts, and a
// closed catalog of actions (each declaring a precondition and the §10.8
// guard-grammar effects its commit entails), Synthesize searches for the
// cheapest sequence of catalog actions that makes the goal hold.
//
// The package is pure — no I/O, no Core KV, no wall-clock, no engine
// dependency. It reuses the §10.5 guard grammar verbatim (internal/guardgrammar)
// for goals, preconditions, and effects, evaluated here against an in-memory
// State rather than live Core KV (internal/loom's evalGuard is the KV-backed
// sibling evaluator; this one is the regression engine's state-snapshot
// sibling — same grammar, same semantics, different backing store). Wiring a
// real lens row / op-DDL catalog into these types is later fires' concern
// (§3.2 column-seam rule); this package only proves the search itself is
// correct and deterministic.
//
// Determinism is load-bearing (§3.1): Synthesize never ranges over a Go map
// in an order-sensitive way, never reads the wall clock, and breaks ties
// canonically (total cost ascending, then the step sequence's action refs
// lexicographically) — the same (goal, state, catalog) always yields the
// same plan regardless of catalog slice order.
package planner
