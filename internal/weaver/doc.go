// Package weaver implements the Lattice convergence engine (Contract #10
// §10.2/§10.3/§10.8): it watches target-Lens violation rows in the shared
// weaver-targets bucket and remediates open gaps by submitting operations
// through the Processor.
//
// The pipeline is Sensorium → Evaluator (L1/L2) → Strategist → Actuator:
//
//   - Sensorium lane 1 is a per-target supervised KV-CDC durable on the
//     weaver-targets backing stream ($KV.<bucket>.<targetId>.>,
//     DeliverLastPerSubject), reconciled desired-vs-running against the
//     meta.weaverTarget registry (registry.go, engine.go).
//   - Evaluator L1 confirms a row is still violating and not in-flight; the
//     in-flight check is the weaver-state CAS-create mark — the dispatch OCC
//     (evaluator.go, state.go).
//   - Evaluator L2 + Strategist classify each open missing_* gap against the
//     target's playbook and resolve templated params (strategist.go).
//   - The Actuator fire-and-forget publishes the remediation op to ops.<lane>
//     with a deterministic per-dispatch-episode requestId (actuator.go).
//
// The engine carries zero domain knowledge: targets and playbooks are package
// data loaded over CDC, and the engine is a generic dispatcher. weaver imports
// only internal/substrate — never internal/processor, internal/loom, or
// internal/refractor (boundary_test.go); triggering a Loom utility is done via
// the StartLoomPattern op, not a Go call.
package weaver
