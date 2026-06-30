# Design — Hard-delete mutation verb (`delete`) — true link/aspect keyspace reclaim

**Status: 📐 awaiting-Andrew (ratification).**
**Author: Winston (Designer fire, 2026-06-30)**
**Backlog row:** `planning-artifacts/backlog/lattice.md` → *Refinements & ops* → "Hard-delete mutation verb (true link/aspect keyspace reclaim)".
**Grounded demand:** surfaced by the **kv.Links Fire 2** checkpoint (`op-time-bounded-link-enumeration-design.md` §10) — soft-tombstone bounds the per-op GET fan-out but **not** the keyspace LIST cost, which grows monotonically with a hub's lifetime booking count. True reclaim was filed as this follow-on.

---

## For Andrew (one-look ratification)

**What it does (two lines).** Adds a **4th Starlark mutation verb — `delete`** — alongside `create`/`update`/`tombstone`. Where `tombstone` is a *soft* delete (a normal PUT setting `isDeleted: true` — the key stays live in Core KV and is still returned by `kv.Read`/`kv.Links`), `delete` issues a **revision-conditioned NATS `DEL` marker** within the same atomic batch, so the key **leaves the live keyspace** — `kv.Read` returns not-found, `kv.Links`/`ListKeysFiltered` no longer enumerate it. This lets a domain *reclaim* dead deterministic keys (terminal `hasBooking` links, dead guard keys) instead of accumulating them forever, bounding the `kv.Links` LIST cost the soft-tombstone left unbounded.

**The one ratification decision (relaxes a stated invariant — not a multi-plane fork).** It relaxes Contract #3 §3.3's "**tombstones are permanent; keys are not reused**" / immutable-ledger framing — but **narrowly and safely**, because of one fact: **Core KV is *materialized state derived from* the append-only `core-operations` op-ledger** (P2; the Replay tool rebuilds KV by re-running ops). `delete` removes a *derived KV key*, **not** a *ledger entry*. The truly-immutable thing — the op-log — is untouched, and a replay simply re-issues the `DEL`. Decisively, `delete` uses a NATS **`DEL` marker, never a `PURGE`**: the key's prior KV revisions **stay in the stream history** (only `PURGE` collapses history), so KV-history-based audit, FR51 historical-state-query, and CDC replay all still work. **My recommendation: ratify `delete` with `DEL`-marker (live-keyspace-removal, history-retained) semantics.** The substrate seam already exists (`BatchOp.Delete`, today unused) — this is mostly a Processor + contract-surface wiring, not new substrate.

**The sub-fork (my recommendation: NO).** Should we *also* offer a history-collapsing `purge` verb (NATS `PURGE` + rollup → reclaim the *history* storage, not just the live key)? **Recommend not.** History erasure is **crypto-shred's** domain (destroy the per-identity key → inert ciphertext, §3.10/§3.11) — a `purge` verb would be the one thing that genuinely breaks the immutable ledger (replay/FR51/audit), for a marginal storage win NATS stream-retention limits already bound. I design `delete` so a future `purge` *could* be added if a concrete history-storage driver ever appears, but I recommend against building it now. **Your call to confirm.**

**Frozen-contract change (staged UNCOMMITTED in `main`).** `docs/contracts/03-mutation-batch-event-list.md` — a new `delete` paragraph in **§3.3**, the `op` enum in **§3.2**, the step-8 mapping + the `{create, update, tombstone, delete}` enum in **§3.8**, and a cross-reference on the `tombstone` permanence sentence. **This is a different file from the uncommitted `optionalReads` edit in `02-operation-envelope.md`** (the script-read-posture design) — the two proposals do **not** overlap; I leave both unstaged. Affected consumers: the Processor (step-5 return-shape parse, step-6 validate, step-8 commit, the ProtectedKey backstop), package authors using the verb, and the clinic-domain package (first consumer). The edit is the proposal — review the diff.

**No architectural fork** (Gateway / D1 read-path-auth / Vault / multi-cell / HA-NATS untouched). **No auth-surface change** — `delete` flows through the same step-3 auth + step-6 `permittedCommands` write-gate + the step-8 `ProtectedKey` kernel backstop (extended to cover `delete`) as every other mutation.

---

## 1. Problem and intent

### 1.1 The Starlark mutation vocabulary has no honest removal

The write-path mutation vocabulary is exactly three verbs (Contract #3 §3.3), **all of which are PUTs**:

- `create` — PUT, conditioned `revision=0` (create-if-absent).
- `update` — PUT, conditioned on the hydrated revision (modify-in-place; setting `isDeleted:false` implicitly restores).
- `tombstone` — PUT setting `isDeleted:true`, conditioned on the hydrated revision (*soft* delete).

There is **no verb that removes a key**. A "deleted" entity is a key whose latest value carries `isDeleted:true` — it is a **fully live NATS KV entry** (latest op = PUT). It still occupies a key, `kv.Read` still returns it (carrying `isDeleted`), and — the load-bearing consequence — **`ListKeysFiltered` / `kv.Links` still enumerate it**. `internal/substrate/kv.go:219` states this precisely:

> *"the underlying `ListKeysFiltered`'s `IgnoreDeletes` drops only NATS **hard-delete markers**, which the Processor never writes."*

So a soft-tombstoned key is invisible to *value* readers (who check `isDeleted`) but **fully visible to the keyspace**.

### 1.2 The concrete cost (grounded — kv.Links Fire 2 surfaced it)

The just-shipped **kv.Links Fire 2** (clinic appointment double-book guard) re-authored the guard onto `hasBooking` links read via `kv.Links`, and eagerly **soft-tombstones** a `hasBooking` link when its appointment goes terminal. Its own checkpoint records the residual honestly (`op-time-bounded-link-enumeration-design.md` §10):

> *"Soft-tombstone … does **not** remove a link from `kv.Links` enumeration: a tombstoned link's key persists and is still listed. So eager tombstone bounds the per-op `kv.Read` GET fan-out (the guard's `if link.isDeleted: continue` fast-skip) — it does **not** bound the keyspace LIST cost, which grows monotonically with a hub's lifetime booking count. True keyspace reclaim needs a hard-delete mutation verb … filed as a follow-on."*

A provider who has had 10,000 appointments over two years has 10,000 `hasBooking` link keys — **9,990 of them dead** (terminal/cancelled) — and **every** double-book guard op pages through all 10,000 in the `kv.Links` LIST, fast-skipping the 9,990 dead ones. The GET fan-out is bounded (the fast-skip), but the **LIST cost is not**, and it only grows. This is the demand: a verb that lets the terminal-transition op actually *remove* the dead `hasBooking` link so the live LIST set stays small.

### 1.3 Same gap, second instance: the uniqueness guard-link key is never freed

The `appliedToUnit` lease-application uniqueness guard (commit 3704324) keys a deterministic guard link on the constrained tuple; a *soft-tombstoned* guard link is, per §3.3 `create`, **un-recreatable** ("If the key exists in any state (including tombstoned), the atomic batch is rejected"). The current restore path (`update` → `isDeleted:false`) works but leaves the keyspace monotonically growing with every withdrawn-then-cleaned guard. `delete` is the honest reclaim for a dead deterministic guard key. (Scope caveat on *re-creating the same key* — §3.4.)

### 1.4 Intent

Add **one** mutation verb — `delete` — that removes a key from the **live** Core KV keyspace (and therefore from every enumeration), conditioned on the hydrated revision, within the atomic batch, **retaining stream history** (a `DEL` marker, not a `PURGE`). Wire the existing-but-unused substrate `BatchOp.Delete` seam through the Processor commit path and the Starlark return contract, gated by the same auth/write-scope/kernel-protection backstops as every other mutation. Make the first consumer the clinic `hasBooking` reclaim that closes kv.Links Fire 2's open residual.

---

## 2. The shape

### 2.1 The verb — `delete` (key-only, no document)

A 4th value of the `mutation.op` enum, declared in a Starlark return like every other mutation:

```python
{ "op": "delete", "key": "lnk.appointment.<a>.withProvider.provider.<p>" }
# No "document": delete carries no body (nothing to validate/store — it removes the key).
# Conditioned on the step-4 hydrated revision (fail-closed under concurrency), like tombstone.
```

`delete` is the *hard* sibling of `tombstone`:

| | `tombstone` (soft) | `delete` (hard) — NEW |
|---|---|---|
| Substrate op | PUT, `isDeleted:true` | **NATS `DEL` marker** (`BatchOp.Delete`) |
| `kv.Read(key)` after | returns the doc, `isDeleted:true` | **not-found** |
| `kv.Links` / `ListKeysFiltered` after | **still enumerated** | **dropped** (IgnoreDeletes) |
| KV stream history | retained (a new PUT revision) | **retained** (`DEL` ≠ `PURGE`) |
| Restorable via `update`→`isDeleted:false`? | yes | no (key is gone; see §3.4) |
| OCC-conditioned | hydrated revision | hydrated revision |
| Provenance / `document` | doc unchanged but stored | none (no body) |
| Use | "this entity is logically deleted; keep it visible/auditable/restorable" | "this *dead* key should leave the live keyspace so enumerations stay bounded" |

The two coexist deliberately: `tombstone` is for entities whose deleted-state must remain *legible and restorable* (an identity, a lease — audit/restore matters); `delete` is for *dead, terminal, deterministic* keys whose only remaining cost is occupying the keyspace (a terminal `hasBooking` link, a dead guard link).

### 2.2 Read path (P5) / write path (P2) / orchestration

- **Write path (P2) — unchanged in shape.** `delete` is a mutation in the script's `MutationBatch`, committed by the Processor as the sole Core-KV writer via the same `Conn.AtomicBatch`. The `DEL` marker rides the *same atomic batch* as the op's other mutations (so "mark the appointment terminal **and** remove its `hasBooking` links" is one OCC-checked commit). No new write surface, no engine writing KV.
- **Read path (P5) — untouched.** `delete` is a write verb; applications still read lenses. After a `delete`, the lens-target row for that key is retracted by the Refractor's normal CDC reaction to the `DEL` marker (the materializer already handles delete markers — §4.3).
- **Orchestration — none.** Synchronous op-time mutation, no Loom pattern / Weaver convergence lens / schedule. It mirrors the *existing* `tombstone` verb exactly, swapping the substrate primitive (PUT→DEL) under the same commit machinery.

### 2.3 The substrate seam already exists

`internal/substrate/batch.go` already defines `BatchOp.Delete` and `AtomicBatch` already honors it:

```go
// BatchOp.Delete writes a NATS KV delete marker (KV-Operation: DEL) instead of a
// value put, so a key can be removed within the same atomic batch as other puts.
// Value is ignored when Delete is set; a subsequent read returns ErrKeyNotFound.
// HasRevision/Revision still apply (a revision-conditioned delete) …
```

and in `AtomicBatch`:

```go
if op.Delete {
    m.Data = nil
    m.Header.Set("KV-Operation", "DEL")
}
```

This seam is **defined and unit-tested but has zero production callers today**. `delete` wires it through. The only substrate-adjacent confirmation needed: `ListKeysFiltered`'s `IgnoreDeletes` drops the `DEL` marker (confirmed at `internal/substrate/kv.go:219`), so a `delete`'d key disappears from `kv.Links`. **No new substrate primitive** — the asymmetry note ([[no-substrate-ensurekv]]) does not apply; nothing is being *provisioned*.

### 2.4 Why `DEL`, not `PURGE` — the load-bearing safety choice

NATS KV offers two removals:

- **`DEL` marker** — removes the *live* value (latest op becomes a delete marker; reads → not-found; `ListKeys` skips it). **Prior revisions stay in the stream** until ordinary stream-retention limits.
- **`PURGE`** (+ rollup) — removes the value **and collapses all prior revisions** for that key into a single purge marker, reclaiming the history storage immediately.

`delete` uses **`DEL`**. This is the entire reason the verb is safe to ratify:

1. **The op-ledger is untouched.** Core KV is *materialized state derived from* the append-only `core-operations` ledger (P2; the Replay tool rebuilds KV by re-running ops). `delete` removes a *derived KV key*; it does not touch a single op-ledger entry. The immutable thing stays immutable.
2. **KV history is retained.** A `DEL` marker leaves the prior revisions in the KV stream — so anything reading KV *history* (FR51 historical-state-query against the reserved seams, CDC rebuild-from-sequence-0, a forensic audit) still sees the full lifecycle, ending in a delete marker. Only `PURGE` would erase that, and `PURGE` is precisely what crypto-shred-not-delete was designed to avoid.
3. **No storage regression vs. the status quo.** A `DEL` marker is one more stream message — exactly the cost of one `update`. The soft-tombstone it replaces *also* wrote a message. Net stream growth is unchanged; what changes is that the key leaves the *live* keyspace (the LIST set), which is the goal.

`PURGE` (history collapse) is therefore **out of scope and recommended against** (the For-Andrew sub-fork). If a real history-storage driver ever appears, a separate `purge` verb can be designed *then*, against the crypto-shred boundary — `delete`'s contract is written so it does not preclude that.

### 2.5 The authoring pattern (package-side)

A reusable pattern for any domain accumulating dead deterministic keys:

1. **Soft `tombstone` for entities that stay legible/restorable** (identities, leases, appointments-as-records) — unchanged.
2. **Hard `delete` for the *dead, terminal* satellite keys** whose only residual cost is enumeration: on the terminal transition, the op `delete`s the dead `hasBooking` links (not the appointment record — that stays a soft tombstone for audit). The op enumerates the links to delete via `kv.Links` (inbound) exactly as it does today to *tombstone* them; it swaps the verb.
3. **Never `delete` a key you might need to read-as-deleted or restore.** `delete` is irreversible within the live keyspace (§3.4). The authoring rule: *`delete` only a key whose deleted-state no reader needs to observe and no op needs to restore.* A terminal `hasBooking` link qualifies (the guard fast-skips it today anyway); a tombstoned identity does not.

---

## 3. Detailed semantics (the contract surface)

### 3.1 Return shape and step-5 parse

`mutation.op == "delete"` requires `key` (full Core KV key, Contract #1 shape) and **forbids** `document`/`expectedRevision`-via-document (a `delete` carries no body). A `delete` with a `document` is `InvalidReturnShape` (fail-closed at parse, mirroring the closed-shape discipline of §2.7). The existing `TestExecute_InvalidMutationOp` — which today asserts `op:"delete"` ⇒ `InvalidReturnShape` — is updated to assert a genuinely-bogus op (e.g. `op:"frobnicate"`) ⇒ `InvalidReturnShape`, since `delete` is now valid (§5 migration).

### 3.2 Step-6 validation

- **No schema/data validation** — there is no `document.data` to validate against the DDL schema (the key is being removed). The DDL is still resolved (by the key's *class* as read at step 4) **for the `permittedCommands` write-gate only**.
- **`permittedCommands` write-gate (the auth-of-write).** `delete` is a write to that key's governing DDL and is gated exactly like `tombstone`: the operation's `operationType` must be in the affected DDL's `permittedCommands`, else `WriteScopeViolation`. **A package cannot `delete` a key whose DDL does not admit the op** — so a domain can only hard-delete its own keys, never another package's.
- **No cascade (mirrors §3.5 tombstone).** `delete`-ing a vertex root does **not** auto-delete its aspects/links; the script must enumerate and include them. The §3.5 dangling-reference rule is *strengthened* by delete: a `create` of a link whose endpoint was `delete`d in a *prior* op fails `DanglingReference` at step 6 (the endpoint no longer resolves — correct).
- **Sensitivity / provenance** — N/A (no body written).

### 3.3 Step-8 commit (OCC + the kernel backstop)

- **OCC condition.** `delete` maps to `BatchOp{Delete:true, HasRevision:true, Revision:<step-4 hydrated revision>}` — a **revision-conditioned** `DEL`. If the key changed (or was restored) concurrently between step 4 and step 8, the conditioned `DEL` fails → `RevisionConflict` → the existing bounded-retry re-hydrates and re-decides. A `delete` of a key **never read at step 4** (no hydrated revision) commits an *unconditioned* `DEL` — the same posture `update`/`tombstone` already have for an undeclared key; the authoring norm is to declare the key in `contextHint.reads` so the delete is conditioned (and replay-stable).
- **ProtectedKey kernel backstop — EXTENDED to `delete`.** `internal/processor/step8_commit.go`'s `ProtectedKeyError` today fails-closed any `update`/`tombstone` whose (derived) root carries `data.protected == true` (kernel DDLs, meta-roots), regardless of what the script declared. **`delete` is added to this backstop** — a `delete` targeting a protected kernel root is rejected `ProtectedKey`, path-independently. This is the critical safety addition: it makes a hard-`delete` of a load-bearing DDL/meta-vertex **impossible**, closing the bricking hole for the destructive verb the same way it is closed for soft mutation. (`create` is exempt as today — create-only already conflicts on overwrite; `delete` is the new destructive path and must be in the backstop.)

### 3.4 The create-over-delete-marker boundary (an honest non-goal)

After a `delete`, the subject retains the `DEL` marker at some sequence N. Lattice's atomic-batch `create` (`CreateOnly` → header `Nats-Expected-Last-Subject-Sequence: 0`) is a **raw stream publish**, *not* the delete-marker-aware `nats.go` `KV.Create()` helper. So a `create` at a *previously-`delete`d key* would assert "last subject sequence == 0", find sequence N, and **`RevisionConflict`**. **Therefore: `delete` removes a key from the live keyspace but does NOT make that exact key re-`create`-able through the plain `create` verb.**

This is a **deliberate scope boundary, not a defect**:

- For the **primary demand** (terminal `hasBooking` reclaim) it is moot — appointment NanoIDs are unique, so the exact link key never recurs.
- For **random-NanoID vertices** it never arises (a fresh entity is a fresh NanoID).
- For the **deterministic guard-link recycling** case (§1.3), it means `delete` *frees the keyspace* but a genuine *re-application* still needs a fresh guard mechanism (a new tuple, or the soft-tombstone-then-restore path). **`delete` is for reclaim, not for key recycling.** The contract states this explicitly.

If a concrete key-recycling need ever appears, the forward path is a **delete-marker-aware `create`** (teach the step-8 builder to condition a `create` on the existing `DEL` marker's revision when one is present) — a separable, additive change, deferred until a real driver exists. I resolve it as out-of-scope now rather than build speculative machinery.

### 3.5 Determinism / replay

A `delete` op is replay-safe: under JetStream redelivery before step 8 it re-executes fresh and re-issues the conditioned `DEL` (idempotent); after step 8 the dedup tracker short-circuits. A **disaster-recovery Replay** (re-running the op-ledger through the Processor, P2) re-issues the `delete` in ledger order — KV converges to the same delete-marker state. History retention (DEL≠PURGE) means a from-sequence-0 CDC rebuild still observes the full lifecycle. No determinism surprise beyond what `tombstone` already has.

### 3.6 Scope guard

`delete` accepts any well-formed Core KV key (`vtx.`/aspect/`lnk.` shapes, Contract #1) — it is general (any dead key is reclaimable), bounded only by the `permittedCommands` write-gate (you can only delete keys your DDL governs) + the `ProtectedKey` backstop (you can never delete a protected kernel root). It cannot target another bucket (the batch is single-bucket, Core KV) and never touches the op-ledger, the Refractor Adjacency KV, the Object Store bytes plane, or any lens target.

---

## 4. Migration / compatibility

### 4.1 Platform (Fire 1) — additive

`delete` is a new enum value; every existing script is byte-identical and the three existing verbs are untouched. The only non-additive touch is the **one test** (`TestExecute_InvalidMutationOp`) that used `"delete"` as its example of an *invalid* op — re-pointed to a still-bogus verb. The `package mutation` Go enum (`{create, update, tombstone}`) gains `delete`. No existing package references `delete`, so no consumer breaks.

### 4.2 Clinic-domain (Fire 2) — the first consumer

The clinic terminal-transition ops (`SetAppointmentStatus`→terminal, `CancelAppointment`, `TombstoneAppointment`) **today** soft-tombstone the appointment's inbound `hasBooking` links (kv.Links Fire 2). They change to **`delete`** those links:

| Today (kv.Links Fire 2) | After (this design, Fire 2) |
|---|---|
| terminal transition `tombstone`s each `hasBooking` link (key stays, `isDeleted:true`) | terminal transition **`delete`s** each `hasBooking` link (key leaves the keyspace) |
| guard's `kv.Links` still LISTs the dead links (fast-skipped via `isDeleted`) | guard's `kv.Links` **no longer LISTs** the dead links — LIST set = live bookings only |
| LIST cost grows monotonically with lifetime bookings | LIST cost bounded by *live* bookings |

The appointment **vertex/record itself stays a soft `tombstone`** (audit/history/reschedule legibility). Only the dead *`hasBooking` satellite links* are hard-deleted. The `bookingGuard` epoch (the serialization lock) is unaffected. The guard's `if link.isDeleted: continue` fast-skip becomes dead code for `hasBooking` (deleted links no longer appear) but stays as a harmless backstop. Clinic package version bumps (`0.8.0` → `0.9.0`).

### 4.3 Refractor / lens targets

The materializer reacts to Core KV CDC including delete markers; a `delete`'d key produces a lens-target **row retraction** through the existing delete-handling path (the same path a tombstone's `isDeleted:true` reprojection triggers, but now via a genuine delete marker). For the clinic `hasBooking` links — which are **not** projected to any lens target (they're write-path topology, read only via `kv.Links`) — there is no lens consequence at all. Fire 2 verifies no lens regression; the platform Fire 1 needs no Refractor change (the materializer already handles delete markers it receives from any source).

### 4.4 Live-stack migration

Per F-004, a same-version reinstall won't hot-migrate; a clinic version bump + fresh `make up-clinic` seeds the new terminal-transition scripts. Already-soft-tombstoned `hasBooking` links from a prior version remain enumerated until a future terminal-transition op (or an optional one-shot cleanup op) `delete`s them — a *cosmetic* residual (the guard fast-skips them correctly meanwhile), never a correctness gap. Matches the migration-note pattern the kv.Links Fire 2 refactor already set.

---

## 5. Test strategy

**Fire 1 (platform):**
- **Step-5 parse** (`step5_execute_test.go`): `op:"delete"` with `key` only → accepted into the batch; `op:"delete"` *with* a `document` → `InvalidReturnShape`; re-point `TestExecute_InvalidMutationOp` to `op:"frobnicate"`.
- **Step-6 validate**: `delete` of a key whose DDL admits the op → passes; `delete` of a key whose DDL does **not** list the op in `permittedCommands` → `WriteScopeViolation`; a `create` of a link whose endpoint was `delete`d in a prior committed op → `DanglingReference`.
- **Step-8 commit** (`step8_commit_test.go`): a `delete` emits `BatchOp{Delete:true}` conditioned on the hydrated revision; a concurrent revision change → `RevisionConflict`; after commit, `kv.Read` → not-found **and** `ListKeysFiltered`/`kv.Links` no longer return the key (the LIST-bound proof — the whole point); **`delete` of a `data.protected==true` root → `ProtectedKey`** (the kernel backstop, the key security test).
- **Determinism**: redelivery before step 8 re-issues the conditioned `DEL`; dedup short-circuits after step 8.
- A **new destructive write-path verb that relaxes the permanence invariant + adds a kernel-bricking surface** ⇒ **full 3-layer adversarial review** (attack: deleting a protected/kernel root; deleting another package's key via a forged class; the create-over-delete-marker boundary; unconditioned-delete races; cascade/dangling-reference holes; lens-retraction correctness).

**Fire 2 (clinic consumer):**
- `package_test.go`: terminal transitions emit `op:"delete"` on the `hasBooking` links (not `tombstone`); `TestPackage_NoScans` still green (`delete` is not a scan helper).
- `integration_test.go`: the existing double-book/reschedule/cancel suites pass against the new mechanism; **a new assertion that after a cancel/terminal transition the dead `hasBooking` link is ABSENT from `kv.Links` enumeration** (LIST bounded), not merely flagged `isDeleted` — the executable proof this design delivers what Fire 2-of-kv.Links could not.
- **Full review** (it touches the double-booking correctness guard) — scoped to the package; the platform risk was discharged in Fire 1.

**Fire 3 (optional):** an ephemeral `make up-clinic` e2e — book N, cancel N-k, assert the provider's live `kv.Links("…","hasBooking","in")` page count reflects only the k live bookings (LIST genuinely bounded across a real stack), and a `lint-conventions` note that `delete` is used only on terminal/dead keys (never as a `tombstone` substitute where restore-ability or deleted-legibility is needed).

---

## 6. Alternatives considered (earn the recommendation)

| # | Alternative | Verdict |
|---|---|---|
| **A** | **Status quo — soft-tombstone only** | Rejected — it is the thing being fixed. Bounds the GET fan-out, never the LIST cost; the keyspace grows forever (§1.2). |
| **B** | **`purge` (NATS PURGE + rollup) instead of `DEL`** | Rejected as the *default* — collapses KV history, breaking replay/FR51/audit, colliding head-on with crypto-shred-not-delete for a marginal storage win NATS retention limits already bound. Offered to Andrew as the sub-fork; recommended against. (`DEL` achieves the LIST-bound goal without the history cost.) |
| **C** | **Background GC sweeper** (a Weaver/Refractor consumer that periodically hard-deletes dead links, like the object-store-manager byte GC) | Rejected as the *primary* mechanism — it adds a whole async convergence surface + a second writer pattern to solve what a synchronous verb solves in the terminal-transition op that already touches the link. The object-GC sweeper exists because object *bytes* live off-graph on a plane the Processor can't reach in-batch; `hasBooking` links live in Core KV *in the same batch* as the terminal transition. Use the verb. (A sweeper remains the right tool for *orphaned* keys with no triggering op — a possible far-future follow-on, not this.) |
| **D** | **Overload `tombstone` to hard-delete when a flag is set** | Rejected — conflates two genuinely distinct semantics (soft/legible/restorable vs hard/removed) behind a flag, exactly the kind of overloaded verb §3.3's "why no upsert" argues against. Two clear verbs beat one flagged verb. |
| **E** | **A "delete a vertex *and* cascade its aspects/links" verb** | Rejected — cascade is a business-logic choice the platform refuses to make for the script (§3.5's explicit stance). `delete` stays single-key; the script enumerates what to delete, exactly as `tombstone` requires. |
| **F** | **Hard-delete via direct substrate Purge from a privileged tool** (bypass the Processor) | Rejected — violates P2 (Processor is the sole Core-KV writer). A removal is a state mutation and must flow through the ledger, be auth-gated, and be replayable. The verb is the P2-honest form. |

**Could a rejected option beat the recommendation?** The honest re-test is **C** (background sweeper) — it is genuinely the right pattern for keys with *no triggering op* (true orphans). But for the grounded demand, the terminal transition *is* the triggering op and *already* touches the link; a synchronous verb is strictly simpler and avoids a second async writer. C is noted as the future orphan-collector, not the mechanism here. No other option survives re-test.

**Dead-scaffolding test.** Does `delete` realize value before a consumer exists? **Yes — the clinic `hasBooking` reclaim is the consumer and ships in the same initiative** (Fire 2 directly after Fire 1), closing kv.Links Fire 2's documented open residual. The verb is not built dark. Sequenced, not deferred.

---

## 7. Reconciliation with the existing mental model (pre-empting "but didn't we…?")

- **"Isn't `tombstone` already delete?"** No — `tombstone` is a *soft* delete (a PUT, key stays live, enumerated, restorable). `delete` is the *hard* one (a `DEL` marker, key leaves the live keyspace + enumerations). The whole point of the kv.Links Fire 2 residual is that soft-tombstone does **not** remove the key from `kv.Links`. They are different operations with different costs; both are wanted.
- **"Doesn't this break the immutable ledger / 'keys are not reused'?"** The immutable ledger is the **`core-operations` op-log**, which `delete` never touches — it removes a *derived KV key*, which the Replay tool rebuilds by re-running ops. And `delete` uses `DEL`, not `PURGE`, so even KV *history* survives. "Keys are not reused" relaxes only in the narrow, stated, non-recyclable way of §3.4. The genuinely-immutable thing stays immutable.
- **"Doesn't crypto-shred already handle deletion?"** Crypto-shred handles *PII erasure* (destroy the key → inert ciphertext) on entities that must *stay on the ledger* for audit. `delete` handles *keyspace reclaim* of *dead, non-PII, deterministic* keys whose only residual cost is enumeration. Orthogonal: shred makes content unreadable while the key persists; `delete` removes the key while history persists. Neither subsumes the other. (The §B sub-fork is exactly the line between them: a *PURGE* verb *would* trespass into shred's territory — which is why I recommend against it.)
- **"Doesn't the object-store-manager already hard-delete?"** It hard-reclaims object *bytes* on the off-graph `core-objects` plane (a plane the Processor can't write in-batch), driven by an async GC convergence loop. `delete` is the *on-graph, in-batch, synchronous* analog for Core KV keys — the same honest-reclaim instinct, applied where the Processor already owns the write and the triggering op already touches the key. Complementary, not duplicative.
- **"New substrate primitive?"** No — `BatchOp.Delete` already exists and is unit-tested; it has simply never had a production caller. This wires it. ([[no-substrate-ensurekv]] does not apply — nothing is provisioned.)

---

## 8. Risks

- **Accidental destructive use.** `delete` is irreversible in the live keyspace. Mitigated by: the `permittedCommands` write-gate (a package can only delete its own keys), the `ProtectedKey` kernel backstop (extended to `delete` — kernel roots are undeletable), the §2.5 authoring rule (delete only dead/terminal keys), and the Fire-1 adversarial review's explicit attack on "delete a protected root / another package's key."
- **Kernel-bricking via delete.** The single highest-severity risk; fully mitigated by adding `delete` to the path-independent `ProtectedKeyError` backstop (§3.3) — this is a *required* part of Fire 1, with a dedicated test.
- **Create-over-delete-marker confusion.** A package author might `delete` a key expecting to re-`create` it. Mitigated by the §3.4 contract language (explicit non-goal) + the authoring rule (`delete` is reclaim, not recycling) + the doc-comment on the verb.
- **History-retention surprise (the inverse).** An operator might assume `delete` reclaims *storage*. It does not (DEL≠PURGE) — it reclaims the *live keyspace*. Mitigated by §2.4's explicit "no storage regression / no storage reclaim" statement and the For-Andrew sub-fork framing.
- **Lens-retraction correctness.** A `delete`'d key that *was* projected must retract its lens row. Mitigated by relying on the materializer's existing delete-marker handling + a Fire-1/Fire-2 assertion; the clinic `hasBooking` links are unprojected, so the first consumer carries no lens risk.

---

## 9. Decomposition for the Lattice Steward (fire-by-fire, each independently shippable + green)

**Fire 1 — the platform verb (`delete`).** Add `delete` to the `mutation.op` enum (`package mutation`); step-5 parse (`key`-only, no `document`, else `InvalidReturnShape`); step-6 validate (`permittedCommands` write-gate, no data validation, dangling-reference strengthening); step-8 map to `BatchOp{Delete:true, HasRevision:<hydrated rev>}`; **extend `ProtectedKeyError` to cover `delete`** (the kernel backstop); re-point `TestExecute_InvalidMutationOp`. Processor unit tests (§5 Fire 1). The §3 contract edit is committed by Andrew at ratification (staged uncommitted now). **No consumer yet in this fire, but Fire 2 lands in the same initiative — not dead scaffolding.** **Full 3-layer adversarial review** (destructive verb + kernel-protection surface + invariant relaxation). Green on its own (additive verb; one test re-pointed; existing suites pass).

**Fire 2 — the clinic-domain consumer.** Re-author the terminal-transition ops (`SetAppointmentStatus`→terminal / `CancelAppointment` / `TombstoneAppointment`) to `delete` the appointment's inbound `hasBooking` links instead of soft-tombstoning them (§4.2). Keep the appointment record a soft `tombstone`. Update `package_test.go` + `integration_test.go` incl. the **LIST-bound assertion** (deleted links absent from `kv.Links`). Bump the clinic package version `0.8.0`→`0.9.0`. **Full review** (double-booking correctness guard) — scoped to the package; platform risk discharged in Fire 1. Green: clinic suites pass; the LIST is genuinely bounded.

**Fire 3 (optional) — ephemeral e2e + convention lint.** The `make up-clinic` e2e proving the live `kv.Links` page set tracks only live bookings across a real stack, plus (if Andrew wants it) a `lint-conventions` note that `delete` is reserved for terminal/dead keys. Independently shippable; green.

(Fire 1 and Fire 2 are the substance; Fire 3 is proof + optional guardrail. Order is firm — Fire 2 depends on Fire 1's verb.)

---

## 10. Ratification checklist (for Andrew)

1. **Ratify the `delete` verb** with **`DEL`-marker** (live-keyspace-removal, history-retained, NOT `PURGE`) semantics — the one substantive decision (top block + §2.4). It relaxes "tombstones are permanent / keys not reused" narrowly: KV is derived state, the op-ledger is untouched, KV history is retained.
2. **Decide the sub-fork:** do **not** add a history-collapsing `purge` verb now (my recommendation, §B/§2.4) — confirm, or ask me to design `purge` against the crypto-shred boundary.
3. **Confirm the §3 contract edit** (`03-mutation-batch-event-list.md`, staged uncommitted in `main`) — distinct file from the `optionalReads` edit in `02-…`; the two do not overlap.
4. **Confirm the kernel backstop extension** (`ProtectedKey` now covers `delete`) and the §3.4 create-over-delete-marker non-goal (delete = reclaim, not key recycling) as specified.

Once ✅ Andrew-ratified, the Lattice Steward builds Fire 1 → Fire 2 (→ Fire 3).
