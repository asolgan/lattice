# Contract #10 Amendment Request (Story 9.2 — Weaver mark TTL/lease)

This amendment to the **FROZEN** Contract #10 (`docs/contracts/10-orchestration-surfaces.md`)
was adjudicated during Story 9.2 (Weaver §10.3 mark lease + reconciler sweep), 2026-06-12. Per
CLAUDE.md "Frozen contracts," it is not an in-flight edit — it is raised here for ratification by
the contract owner (Andrew) + a Contract #10 revision-history entry. The implementation
(`internal/weaver/state.go`, `internal/weaver/reconciler.go`) already builds to the requested
text.

**STATUS: PENDING ratification (Andrew).**

## Request 1: §10.3 `weaver-state` — the mark's per-key TTL is 2× the lease, not a literal mirror of `leaseExpiresAt`

**Location:** §10.3 "`weaver-state` — in-flight convergence marks" (the mark value shape and the
"`leaseExpiresAt` mirrors the TTL for visibility" clause).

**Current text:** the mark carries a NATS per-key TTL and "`leaseExpiresAt` mirrors the TTL for
visibility" — read literally, TTL == lease, i.e. the key self-deletes at the moment the lease
expires.

**Requested text:** the per-key TTL is **2 × lease** (`markTTLBackstopFactor`,
`internal/weaver/state.go` — a constant, not a config knob); `leaseExpiresAt` mirrors the
**lease** (`claimedAt + lease`), and the TTL is the lease's **dead-reconciler backstop**,
strictly longer than the lease. `Config.SweepInterval` is clamped to ≤ `MarkLease` so at least
one sweep pass lands inside the lease-to-TTL window.

**Rationale:** the same §10.3 paragraph requires the active reconciler sweep to **reclaim
expired leases**. Nothing watches the weaver-state backing stream (the sweep is interval-cadence
by design), so a raw TTL deletion unwedges the gap but can never re-attempt it — the mark is the
sweep's only evidence (it enumerates marks, not rows). The sweep can therefore reclaim only
while the key still exists **past** `leaseExpiresAt`: with TTL == lease the key self-deletes at
the exact moment it becomes reclaimable, the sweep-reclaims-expired-leases clause is mechanically
unreachable, and every crash recovery degrades to the unwedge-without-re-attempt TTL path. With
TTL = 2 × lease the sweep gets a full lease-width observation window and the TTL still bounds
the mark's life when no reconciler ever runs. The two clauses of §10.3 are only satisfiable
together this way.
