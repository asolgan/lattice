# Refractor Failure Tiers

**Component reference** | Audience: implementers + architects

This document classifies the failure modes the Refractor can encounter and the
operational response each requires.

## Base model — four tiers

Refractor inherits the 4-tier failure model from Materializer
(`internal/refractor/failure/`):

| Tier | Source | Lattice meaning | Route |
|---|---|---|---|
| **Infrastructure** | `failure.Infrastructure` | NATS / Postgres / target store outage | fetch-loop pause, buffer in NATS |
| **Structural** | `failure.Structural` | DDL validation failure, lens spec invalid, schema mismatch | pause the affected Lens until reconciled |
| **Terminal** | `failure.Terminal` | Atomic-batch rejection, malformed Core KV event | DLQ for forensics |
| **Transient** | `failure.Transient` | Retryable target write (e.g. transient Postgres error) | deferred retry queue |

## Mapping examples

- **Postgres connection refused** → Infrastructure → fetch-loop pause
- **DDL `permittedCommands` mismatch on lens spec aspect** → Structural → pause this Lens; operator must fix the meta-vertex DDL
- **Malformed payload from CDC** → Terminal → DLQ (the lens's classify path rejected the event)
- **Postgres unique-constraint violation from a network glitch** → Transient → deferred retry per `RetryConfig`

## Health emissions and lag

- Per-instance heartbeat: `health.refractor.<instance>` every 10s
  (`internal/refractor/health/lattice_heartbeater.go`), TTL-purged (NFR-O1).
- Per-lens latency: `health.refractor.<instance>.lens.<canonicalName>` —
  p95/p99/mean/count from the `LatencyRingBuffer` (NFR-P3 instrument).
- Consumer lag: `NumPending` on the lens consumer, polled by `health.LagPoller`
  and surfaced both on `lattice.refractor.metrics.<lensId>` and as the
  `consumerLag` field on the per-lens health entry.

## Delete-projection semantics

Delete projection is **per-lens and mode-dependent** (`targetConfig.deleteMode`),
with **hard delete as the default**. Lineage already lives in Core
KV, so the derived view reflects deletions as removals unless a lens explicitly
opts into tombstones for audit/forensic targets.

- **`hard` (default)** — physically removes the row/key:
  - Postgres: `DELETE FROM "<table>" WHERE <keys>`
  - NATS-KV: `kv.Delete(key)`
- **`soft` (opt-in)** — retains a tombstone:
  - Postgres: `UPDATE ... SET is_deleted=true, deleted_at=NOW()` (requires the
    `is_deleted` / `deleted_at` columns)
  - NATS-KV: PUT a tombstone document `{"isDeleted": true}` (rather than `kv.Delete`)

Both modes are idempotent: deleting an absent row/key is a no-op, not an error.

The **capability plane uses the default hard delete**: the capability authorizer
treats an absent key (`NoCapabilityEntry`) and a tombstone doc identically as
denial (Contract #6 §6.8, "absence equals denial"), and no freshness-ceiling
comparison exists on this plane that would require a tombstone to survive. Hard
delete is the contract-aligned semantics and avoids indefinite tombstone
accumulation in the capability KV.

## Control-plane authorization (currently stubbed)

The control service authorizes control-plane operations (list lenses, force
re-project) through a `CapabilityChecker` interface
(`internal/refractor/control/capability.go`). The default implementation is
`StubCapabilityChecker` (allow-all + log). Real control-plane authorization —
checking the actor's Capability KV entry before honoring a control op — is
deferred; the data-plane Capability **Lens** that feeds Processor write-path
auth is unrelated and is live.

## Designed-but-not-built: privacy / security supersession tiers

Two supersession classifications sit above the four base tiers in the design but
have **no implementation today** — no alert subject is emitted and no listener
exists. They are recorded here so the structure is ready when their
dependencies land.

- **Security-critical — Capability Lens failure.** If the projection that feeds
  Capability KV breaks, downstream authz could fail open, so a Capability-Lens
  failure should halt the lens and page on-call rather than route through the
  base tiers. Today the Capability Lens emits only the generic per-lens health
  signals above; there is **no Capability-Lens-aware alert** (the same gap noted
  in [refractor.md](./refractor.md#capability-lens-health-operational-backstop)).

- **Privacy-critical — crypto-shred failure.** When Vault key-shred handling
  exists (Phase 3), a row whose encryption key has been shredded but whose
  projection still surfaces decrypted values is a confidentiality breach: the
  affected lens must halt with no automatic retry and page on-call. Vault /
  privacy is Phase 3, so neither the `KeyShredded` listener nor this tier is
  built.
