# Object-store-manager

**Component reference** | Audience: implementers + architects | Contract authority: `docs/contracts/07-primordial-bootstrap.md` §7.2 (object vertices / blob plane) + `docs/contracts/10-orchestration-surfaces.md` §10.8 (`directOp.reads`)

---

## Overview

The object-store-manager is the **byte-janitor** of the off-graph blob plane — the only *new*
always-on component that plane needs. Large files (lease PDFs, ID scans, signatures) live as bytes in
a JetStream Object Store, **off** the vertex graph, addressed by `vtx.object.*` vertices the Processor
mints (Contract #7 §7.2). **Byte deletion is the one off-graph side effect** Weaver, Loom, and the
Processor cannot perform — the graph tombstone is Weaver's `directOp`; reclaiming the *bytes* needs a
dedicated consumer. That consumer is this component.

It submits **no ops that mutate business state** beyond `DetachObject` (a link decrement, below); it
never writes Core KV directly (P2). Its only privileged side effect is deleting bytes from the object
store — an off-graph action no other component is wired to take.

---

## Three responsibilities

### 1. Loop B — the byte-janitor consumer

A durable consumer (`object-store-manager`) on `core-events`, filtered to
**`events.object.tombstoned`**. For each tombstone it reads the object vertex **authoritatively from
Core KV** — never the lagging lens — and deletes the bytes **only when the vertex is gone or still
tombstoned**. A *revived* (re-attached) vertex means the tombstone was superseded → **skip** (no
delete). This is Loop B of the v1b object-GC (Loop A is the `objectLiveness` lens →
`liveLinks == 0` → Weaver `directOp(TombstoneObject)`).

### 2. Never-attached reconcile — the crash-orphan backstop

A low-cadence ticker (`defaultReconcileInterval = 1h`) that reclaims bytes whose `AttachObject` **never
landed** (uploaded, then the attach op crashed). It lists the store and deletes any object older than a
grace window (`defaultReconcileGrace = 25h`) that **no live object vertex names** on its
`.content.storeName` (the §20 exact-storeName predicate — a dedup-duplicate upload is reclaimed while
the canonical bytes are spared). The 25h grace deliberately clears the Contract #4 24h
idempotency-tracker horizon, so a retried-and-collapsed `AttachObject` can't have its bytes reaped
first.

### 3. Owner-tombstone-cascade

A **second** durable consumer (`object-store-cascade`) — over the **`core-kv` KV stream**, not
`core-events` — closes the §21.2 dead-target byte **leak**: when an *owner* vertex is tombstoned with an
object still attached, the owner's death never touches the object, so its `data.liveLinks` stays stale
`≥ 1` and the `objectLiveness` lens never flags it orphaned. The cascade reacts to the **authoritative**
owner-tombstone (the owner's Core-KV root transitioning to `isDeleted` — zero projection lag),
enumerates the dead owner's live `object → owner` links (`lnk.object.*`), and submits **`DetachObject`**
per link under its service actor. `DetachObject` decrements `liveLinks` + OCC-touches the object vertex
→ the existing Loop A+B path reclaims any now-orphaned object. The cascade adds **zero new reap path** —
it only detaches dangling links.

---

## What this component owns

| Path | Role |
|------|------|
| `internal/objectmanager/manager.go` | Loop B (the `events.object.tombstoned` byte-janitor) + the never-attached reconcile ticker + the Contract #5 heartbeat |
| `internal/objectmanager/cascade.go` | The owner-tombstone-cascade (the `core-kv` KV-stream consumer → `DetachObject` per dangling link) |
| `cmd/object-store-manager/main.go` | Binary entry point; pins the service actor + wires the substrate connection |

All NATS access is through substrate (no raw `nats.go`). The lens output bucket and Refractor-private
adjacency are **never** read — authoritative reads are Core KV point-reads.

---

## In / Out contracts

| Direction | Contract | Notes |
|-----------|----------|-------|
| In | `events.object.tombstoned` durable (`object-store-manager`) | Loop B: one message = one object tombstone to consider |
| In | `core-kv` KV-stream durable (`object-store-cascade`) | the cascade: reacts to authoritative owner-root `isDeleted` transitions |
| In | authoritative Core-KV point reads of the object vertex | a **sanctioned** Core-KV reader (like the other CDC materializers) — reads the *authoritative* vertex, never the lagging lens (zero projection lag is the correctness basis) |
| Out | object-store byte `DELETE` on `$O.<objects-bucket>` | the one privileged off-graph side effect — reclaim the bytes |
| Out | `DetachObject` op on `ops.system` (cascade only) | decrements `liveLinks` + OCC-touches the object vertex; submitted under the objmgr service actor |
| Out | Health (Contract #5) | heartbeat at `health.object-store-manager.<instance>` (every 10s) |

---

## Service actor

The component runs as a **bootstrap-provisioned service actor** —
`identity.system.object-store-manager` (`ObjmgrIdentityKey`), operator-equivalent via a
`holdsRole → operator` edge, seeded primordially alongside Loom / Weaver / the Bridge (see
[service-actors.md](./service-actors.md)). It carries the uniform root grant
(`lanes: ["default", "meta", "urgent", "system"]`) so the cascade may submit `DetachObject` on
`ops.system.>`, and `protected: true` so a package uninstall can't tombstone it.

---

## Key invariants

- **Authoritative reads, never the lens.** Every deletion decision reads the object (or owner) vertex
  from Core KV directly — the lens lags, and a lag-window read could delete live bytes. This is *why*
  the cascade watches the Core-KV stream, not a projection.
- **Off-graph only.** The graph tombstone is Weaver's `directOp(TombstoneObject)`; this component only
  reclaims bytes and detaches dangling links. It never tombstones an object vertex itself.
- **Race-hardened GC.** Epoch-CAS on the reconcile + lag-free `liveLinks` + the owner-cascade close the
  attach-lag and dead-owner leak windows; proven by layered unit tests + a self-contained CI e2e
  (`make test-object-gc`, embedded in-process NATS — never touches a shared stack).

---

## Failure modes

| Mode | Behavior |
|------|----------|
| Object vertex revived (re-attached) before Loop B acts | skip — the tombstone was superseded; no byte delete |
| `AttachObject` crashed (orphan bytes) | the never-attached reconcile reclaims them after the 25h grace |
| Owner tombstoned with object still attached | the cascade detaches the dangling link → Loop A+B reclaims |
| Byte delete fails transiently | redelivery (5s floor); the reconcile is the eventual backstop |
| Duplicate upload (dedup) | the exact-`storeName` predicate spares the canonical bytes, reclaims the duplicate |

---

## Principles that apply

- **P2** — the Processor is the sole Core-KV writer. This component mutates graph state only via
  `DetachObject` ops; its byte deletes are off-graph (not Core KV).
- **P5** — it is a **sanctioned Core-KV reader** (a CDC materializer of the blob plane), not an
  application; it reads the authoritative vertex, never a lens.
- **Decision #10 / minimal core** — byte reclamation is the one off-graph need the blob plane adds, so
  it gets exactly one dedicated always-on component, no more.

---

## Implementation status

**Built.** Loop B, the never-attached reconcile, and the owner-tombstone-cascade are all implemented
and CI-gated (`make test-object-gc`). The service actor is seeded primordially.

The heartbeat **degrades**: `emitHeartbeat` computes its status via `aggregateStatus` over the
open issue set (Contract #5 §5.2/§5.3 — issues empty iff `healthy`, a warning ⇒ `degraded`, an
error ⇒ `unhealthy`), mirroring the Loom/Weaver/Bridge heartbeaters, so a heartbeat carrying
issues can never self-report green.
