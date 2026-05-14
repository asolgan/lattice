---
title: Contract Amendment Request — JetStream lane subject pattern
raisedBy: Story 1.5 implementation agent (claude-opus-4-7)
raisedAt: 2026-05-13
status: Open — awaits Winston adjudication
severity: Low (does not block Story 1.5 acceptance; affects future lane semantics)
---

# Contract Amendment Request — Lane Subject Pattern

## Issue

Two authoritative artifacts disagree about the JetStream subject pattern
the operation envelope must be published to:

- **Contract #2 §2.3** ("Lanes and JetStream Subject Mapping") prescribes
  per-lane multi-segment wildcards:

      | Lane     | JetStream Subject |
      |----------|-------------------|
      | default  | ops.default.>     |
      | meta     | ops.meta.>        |
      | urgent   | ops.urgent.>      |
      | system   | ops.system.>      |

- **`internal/bootstrap/primordial.go` (Story 1.3 / 1.4)** provisions the
  `core-operations` stream with `Subjects: ["ops.*", "ops.meta.>"]`.

`ops.*` is a **single-segment** match — it captures `ops.default`,
`ops.urgent`, `ops.system` but NOT `ops.default.<anything>`. As a result,
a client following Contract #2 §2.3 literally (publishing to
`ops.default.<NanoID>` or similar) would have its message rejected by the
stream with "no responders / no matching stream."

## Impact on Story 1.5

Limited. Story 1.5's Processor consumer uses `FilterSubjects:
["ops.default", "ops.urgent", "ops.system"]` (single-segment) to match
what bootstrap actually provisioned. The integration tests and the
manual e2e exercise publish to `ops.default` (single segment) and the
path works end-to-end.

The amendment becomes load-bearing once:

- A submitter wants to use multi-segment routing (e.g. `ops.default.<requestId>`
  for transparency in `nats sub` debug tooling).
- The `meta` lane needs the multi-segment shape (already provisioned —
  `ops.meta.>` is present); a future writer publishing to `ops.meta.<X>`
  works today, but a writer publishing to `ops.default.<X>` would not.

## Proposed Resolutions (pick one)

1. **Update bootstrap** (`internal/bootstrap/primordial.go`) to set
   `Subjects: ["ops.>"]` (single wildcard covers all lanes and all
   sub-segments). This brings the provisioning in line with Contract #2
   §2.3 with minimum churn.

2. **Update Contract #2 §2.3** to specify single-segment subjects
   (`ops.default`, `ops.meta`, `ops.urgent`, `ops.system`) plus an
   explicit "per-lane sub-routing is not supported in Phase 1" note. The
   trade-off: less debug-friendly `nats sub` output, no future room for
   sub-lane routing without re-provisioning the stream.

## Recommendation

Resolution **1** (broaden bootstrap to `ops.>`). The Contract #2 shape
is more general and the bootstrap change is a one-line edit. Story 1.6+
will benefit from the additional routing flexibility (e.g. `ops.<lane>.<requestId>`
in logs and traces).

## Action Taken in Story 1.5 (pending adjudication)

- Processor consumer filter defaults to the bootstrap-compatible
  single-segment list: `["ops.default", "ops.urgent", "ops.system"]`.
- Test harness publishes to `ops.default` (single segment).
- This file logged so Winston + Andrew can adjudicate at review time;
  Story 1.6+ should not start until this is resolved (one of the
  resolutions above must land in either bootstrap or the contract).

No change made to either bootstrap or `data-contracts.md` by this agent.
