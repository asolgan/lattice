# `internal/spike` — non-shipping spike & reference harnesses

This directory holds throwaway spike programs written to de-risk a design
decision by exercising real vendor behavior (NATS JetStream atomic batches,
Starlark ergonomics/perf). **Nothing under `cmd/` or the rest of `internal/`
imports these packages** — they ship nothing.

They are kept (not deleted) because two records cite their code as the
verification artifact behind a ratified decision:

- `nats-batch/` — the JetStream atomic-batch / TTL-marker probes cited by the
  Contract #10 §10.3/§10.6 command-outbox ratification
  (`docs/contracts/10-orchestration-surfaces.md` revision history →
  `nats-batch/test_ttl_marker_delivery.go`).
- `starlark/` — the Starlark runner/perf/ergonomics spikes behind the parser
  strategy decision (`docs/decisions/`).

Posture: these packages compile so `go build ./...` stays green, but they are
**excluded from the shipping surface** — no production code path reaches them,
and they carry no `_test.go` gate coverage. Treat them as read-only reference
artifacts. A spike whose decision has fully landed in shipping code and docs may
be removed outright; leave one in place while a contract or decision doc still
points at it.
