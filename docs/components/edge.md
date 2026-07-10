# Edge

**Component reference** | Audience: operators + implementers

> Edge is an **application** (`internal/edge/*`, eventually `cmd/edge`), not a platform engine — it has
> no frozen interface contract of its own. Its framing of record is
> `_bmad-output/implementation-artifacts/edge-lattice-full-design.md` (✅ Andrew-ratified) and the
> *Edge & personal lenses* row of `_bmad-output/planning-artifacts/backlog/lattice.md`. Update this page
> in the same commit as the code; drift between page and code is a documentation bug.

---

## Overview

Edge is the sovereign per-user node design's Go reference implementation: a device holds a **local VAL
mirror** of just its authorized slice, kept fresh by the Personal Lens delta stream (`refractor.md`,
`lattice.sync.user.<id>`), and reconciles by revision rather than trusting a local authoritative writer —
the cloud Processor remains the platform's **sole authority** (P2 is untouched; see the design's FORK-A
resolution). Edge composes five sub-components (design §3); each maps to its own `internal/edge/*`
package, built incrementally per the design's §7 Steward decomposition (EDGE.1 → EDGE.6).

## Status

**EDGE.1 in progress.** Shipped so far:

- **`internal/edge/store`** — the Local VAL Store (design §3.1): an embedded, transactional local KV
  (`bbolt`) keyed by the exact Contract #1 key strings (`vtx.<type>.<id>`, `vtx.<type>.<id>.<localName>`,
  `lnk.<typeA>.<idA>.<rel>.<typeB>.<idB>`). Each entry carries the projected fragment plus the cloud
  revision that produced it. `ApplyUpsert`/`ApplyDelete` implement **last-writer-wins by revision** — a
  write applies iff its revision is ≥ the currently-stored one, so a stale/duplicate/reordered delta
  (JetStream is at-least-once and can reorder) is dropped, never applied out of order. A `Cursor`/
  `SetCursor` pair persists the Sync Manager's last-applied stream sequence across restarts. A separate
  `local:` bbolt bucket (`PutLocal`/`GetLocal`) scaffolds the design's **sovereign, device-only**
  namespace — entries a user creates locally that are never uploaded — kept in its own bucket so the
  mirror's apply path can never reach it.

**Not yet built** (see the design doc §7 for the full fire-by-fire plan):

- **`internal/edge/sync`** — subscribing the Personal-Lens `SYNC` stream and driving `store.ApplyUpsert`/
  `ApplyDelete` from inbound delta envelopes; the gap→re-hydrate trigger on a long disconnect; the
  `personal.hydrate`/`personal.register` control-RPC calls (cold start).
- **`cmd/edge`** — the binary wiring `store` + `sync` (+ later `overlay`/`agent`/`vault`) together.
- **`internal/edge/overlay`** (EDGE.2) — the optimistic local-apply + intent queue write path.
- **`internal/edge/agent`** (EDGE.2) — the intent uploader + reconcile-by-revision conflict handling.
- **`internal/edge/vault`** (EDGE.4) — the transient session-key Vault Proxy for sensitive aspects.

**Trusted single identity only, no security filter** — the same carve-out Loupe + Personal Lens PL.1/
PL.2 use. Untrusted multi-identity exposure is EDGE.3, explicitly gated on D1 (Personal Lens PL.3) +
the Gateway + NATS-account-auth (see the design doc); Edge must not accept an untrusted connection before
that fire lands.

## Grounding

- `_bmad-output/implementation-artifacts/edge-lattice-full-design.md` — the full design, forks, and
  §7 Steward decomposition.
- `_bmad-output/implementation-artifacts/personal-secure-lens-design.md` — the cloud-side producer
  (`nats_subject` adapter, `SYNC` stream, delta envelope, hydration/register control RPCs) Edge consumes.
- `docs/contracts/01-addressing-and-envelope.md` §1.1 — the key shapes the local store mirrors
  byte-for-byte.
- `docs/vendors.md` — `go.etcd.io/bbolt`, the local store's embedded KV.
