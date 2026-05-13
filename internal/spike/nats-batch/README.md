# Story 1.1 — NATS Atomic Batch Spike: Findings Report

## Go/No-Go Recommendation

**GO** — The NATS JetStream atomic batch feature (2.12+) behaves exactly as the Lattice Processor architecture assumes. All four behavioral hypotheses validated cleanly on NATS 2.14.0. No contract amendments are required. The atomic batch commit path is safe to build on.

### Rationale

All four acceptance criteria passed in a single embedded-server run. Per-key TTL composes correctly with atomic batch. Revision conditions provide true all-or-nothing atomicity (no partial commits were observable under concurrent writes). Multi-subject batches within a single KV bucket commit atomically for the exact key shapes Lattice Core KV requires (vertex + aspect + link + op-tracker). TTL expiry delivers a distinct `KeyValuePurge` marker (not a `KeyValueDelete`) to KV watchers in correct stream-sequence order. The architecture's "24h idempotency horizon via per-key TTL" is mechanically sound.

One **implementation surprise** worth documenting (not blocking): the atomic batch API is a raw NATS protocol feature accessed via headers (`Nats-Batch-Id`, `Nats-Batch-Sequence`, `Nats-Batch-Commit`). The `nats.go` v1.52.0 client does **not** expose a high-level `PublishBatch()` Go function. Story 1.7 (Processor — DDL Validation & Atomic Batch) will need to build a thin helper that drives these headers directly via `nc.PublishMsg` / `nc.RequestMsg`. This is straightforward but worth noting for the implementing engineer's planning.

---

## Test Environment

- NATS server: embedded in-process via `github.com/nats-io/nats-server/v2 v2.14.0` (no Docker)
- NATS client: `github.com/nats-io/nats.go v1.52.0`
- Go: 1.26.1
- Spike code: `internal/spike/nats-batch/`

---

## Behavioral Test 1 — TTL-in-Batch

**AC reference:** Contract #4 §4.3 — idempotency tracker written with 24h TTL inside the same atomic batch as business mutations.

### Test Description

An atomic batch of 3 messages was published to a single Core KV bucket (`$KV/spike-test1`):

| Msg # | KV Key | TTL |
|-------|--------|-----|
| 1 | `vtx.identity.Hj4kPmRtw9nbCxz5vQ2y` (vertex) | none |
| 2 | `vtx.identity.Hj4kPmRtw9nbCxz5vQ2y.email` (aspect) | none |
| 3 | `vtx.op.Rm7q3pntwzkfbcxv5p9j` (op-tracker) | 3s |

All three used revision condition 0 (create-if-absent). The tracker had `Nats-TTL: 3s`. After batch commit, the test waited 4 seconds and verified that key 3 had expired while keys 1 and 2 were still present.

### Observed Behavior

```
Batch committed: stream=KV_spike-test1 seq=3 batch_id="spike-test1-batch1" count=3
Key "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"          revision=1 op=KeyValuePutOp
Key "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y.email"     revision=2 op=KeyValuePutOp
Key "vtx.op.Rm7q3pntwzkfbcxv5p9j"                 revision=3 op=KeyValuePutOp
[4s sleep]
Tracker "vtx.op.Rm7q3pntwzkfbcxv5p9j" expired as expected (ErrKeyNotFound)
Non-TTL key "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y"      still present at revision=1
Non-TTL key "vtx.identity.Hj4kPmRtw9nbCxz5vQ2y.email" still present at revision=2
```

**Outcome: PASS**

### Interpretation

Per-key TTL (`Nats-TTL` header) composes correctly with atomic batch. The TTL clock starts at commit time (not at message publish time) and ticks independently for each key. Non-TTL keys are unaffected by a sibling's expiry.

### Contract Implication

None. Contract #4 §4.3 is mechanically sound. The `Core KV bucket MUST be provisioned with `allow_msg_ttl: true`; this is ensured by setting `LimitMarkerTTL` on `KeyValueConfig` (which the client translates to the stream's `AllowMsgTTL` flag). Story 1.4 must include this provisioning step.

---

## Behavioral Test 2 — Revision Condition Atomicity

**AC reference:** Contract #3 §3.3 — `create`/`update`/`tombstone` submit revision conditions; batch is rejected entirely if any condition fails.

### Test Description

**Sub-test 2a (rejection):** A 2-message batch attempted to update key A with a deliberately wrong revision (`current + 999`) while simultaneously creating key B (revision=0). Expected: batch rejected, key B not written.

**Sub-test 2b (commit):** The same 2-message batch with the correct revision on key A. Expected: both keys committed atomically.

### Observed Behavior

```
--- Sub-test 2a (wrong revision) ---
Batch rejected as expected: wrong last sequence: 1 (err_code=10071)
Confirmed: keyB NOT written (no partial commit)
Confirmed: keyA unchanged at revision=1

--- Sub-test 2b (correct revision) ---
Batch committed: seq=3 count=2
keyA updated: revision=2 value={"class":"identity","v":"updated-correct"}
keyB created: revision=3 value={"class":"identity","v":"new"}
```

**Outcome: PASS**

### Interpretation

NATS atomic batch enforces revision conditions atomically. A single condition failure causes the entire batch to be rejected — no partial writes are applied. Error code `10071` corresponds to `JSWrongLastSequence` (wrong last sequence per subject). The error is returned on the commit message's reply; non-commit messages do not produce per-message acks.

### Contract Implication

None. The `create` → `revision=0`, `update` → `hydrated revision`, `tombstone` → `hydrated revision` pattern described in Contract #3 §3.3 and the Processor step 8 notes works exactly as specified.

---

## Behavioral Test 3 — Multi-Subject Batches Within Single KV Bucket

**AC reference:** Story 1.1 AC3 — multi-subject batch within one bucket; cross-bucket non-atomicity documented.

### Test Description

**Sub-test 3a:** A 4-message atomic batch to a single Core KV bucket, targeting the full Lattice key-shape suite:

| Msg # | KV Key | Shape |
|-------|--------|-------|
| 1 | `vtx.identity.Hj4kPmRtw9nbCxz5vQ2y` | vertex (3 segments) |
| 2 | `vtx.identity.Hj4kPmRtw9nbCxz5vQ2y.email` | aspect (4 segments) |
| 3 | `lnk.identity.Hj4kPmRtw9nbCxz5vQ2y.assignedRole.role.Rk2Pn6mQrtwzKbcXvP4U` | link (6 segments) |
| 4 | `vtx.op.Rm7q3pntwzkfbcxv5p9j` | op-tracker |

**Sub-test 3b:** Concurrent conflict. Pre-wrote `conflictKey` at revision=1. Published a batch with `conflictKey` (revision=1) as the first (non-commit) message, then a concurrent writer updated `conflictKey` from outside the batch (to revision=2), then published the commit message. Expected: batch rejected, `sideKey` not written.

**Sub-test 3c:** Structural verification that Health KV and Core KV are on separate streams (`KV_spike-test3` vs `KV_spike-health`), confirming cross-bucket atomicity is architecturally impossible.

### Observed Behavior

```
Sub-test 3a:
  Multi-subject batch committed: seq=4 count=4

Sub-test 3b:
  Published batch msg1 (non-commit) for conflictKey
  Concurrent writer updated conflictKey (changed per-subject seq)
  Batch rejected: wrong last sequence: 2 (err_code=10071)
  Confirmed: sideKey NOT written (atomic rejection, no partial commit)

Sub-test 3c:
  Core KV stream: "KV_spike-test3", Health KV stream: "KV_spike-health"
  Separate streams confirmed — atomic batches cannot span them.
```

**Outcome: PASS**

### Interpretation

NATS KV keys use dot-separated subject names. The atomic batch has no restriction on the number of distinct subjects within a single stream — all four Lattice key shapes (vertex, aspect, link, op-tracker) coexist cleanly in one batch. NATS evaluates revision conditions on each subject independently at commit time; a concurrent write that changes a subject's sequence after the batch message was staged but before commit causes the batch to be rejected atomically.

The cross-bucket constraint is structural: NATS atomic batch is scoped to a single stream, and each KV bucket is its own stream. Health KV, Capability KV, and Core KV are on separate streams. No Processor batch will ever span them — this is not a NATS limitation for Lattice; it aligns perfectly with the architecture's design.

### Contract Implication

None. Architecture is confirmed: Health KV writes (heartbeats) and Capability KV writes (Refractor projections) are not part of the Processor's atomic batch. The batch covers Core KV mutations only.

---

## Behavioral Test 4 — TTL Marker Delivery

**AC reference:** Contract #4 §4.3 — "NATS publishes a `PURGE` marker for the tracker's key with header `Nats-Marker-Reason: MaxAge`, which Refractor and other CDC consumers observe as an explicit expiry event."

### Test Description

A 2-message atomic batch committed to `spike-test4`:
- Key 1 (`vtx.op.TrackerExpiry...`): TTL=3s (simulates idempotency tracker)
- Key 2 (`vtx.identity.DurableVtxX...`): no TTL (immediately deleted via `kv.Delete()` to create a normal delete marker for comparison)

A `WatchAll` subscription was started. Initial-values phase consumed existing keys; the live-updates phase captured the TTL expiry event.

### Observed Behavior

```
Watcher event: key="vtx.op.TrackerExpiry..." op=KeyValuePutOp  revision=1 draining=true
Watcher event: key="vtx.identity.DurableVtx..." op=KeyValueDeleteOp revision=3 draining=true
  -> Normal delete marker for key2 at revision=3
Watcher: initial values consumed, watching for live updates
Watcher event: key="vtx.op.TrackerExpiry..." op=KeyValuePurgeOp revision=4 draining=false
  -> TTL expiry (PURGE) marker for key1 at revision=4

Expiry marker op=KeyValuePurgeOp (PURGE, distinct from normal DELETE)
Expiry marker seq=4 > committed revision=1 (correct ordering)
Normal delete marker seq=3, expiry seq=4 (ordered correctly)
key1 not found via Get after expiry (correct)
Normal delete marker op=KeyValueDelete, expiry marker op=KeyValuePurge — distinct
```

**Outcome: PASS**

### Interpretation

The TTL expiry marker is delivered as `KeyValuePurge` (not `KeyValueDelete`). The `nats.go` client maps `Nats-Marker-Reason: MaxAge` to `KeyValuePurge` internally. Refractor's CDC watcher can distinguish TTL expiry from operator-initiated deletes by checking `entry.Operation() == jetstream.KeyValuePurge`. The expiry marker's stream sequence is strictly greater than the committed write sequence (revision 4 > revision 1), confirming correct monotonic ordering.

### Contract Implication

None. The architecture's description in Contract #4 §4.3 is accurate: NATS delivers a `PURGE` marker with `Nats-Marker-Reason: MaxAge`. The Refractor agent implementing TTL expiry handling should use `entry.Operation() == jetstream.KeyValuePurge` to detect expirations.

---

## API Discovery Note (for Story 1.7 implementing engineer)

The `nats.go` v1.52.0 client does NOT expose a high-level `PublishBatch()` or `AtomicBatch()` function. The atomic batch feature is driven entirely through raw message headers:

| Header | Description |
|--------|-------------|
| `Nats-Batch-Id` | Unique ID per batch attempt (any unique string) |
| `Nats-Batch-Sequence` | 1-based sequence counter within the batch |
| `Nats-Batch-Commit` | Set to `"1"` on the final message only |

Messages 1 through N-1 are published fire-and-forget (`nc.PublishMsg`). Message N includes `Nats-Batch-Commit: 1` and is sent via `nc.RequestMsg` to receive the all-or-nothing ack. The revision condition header (`Nats-Expected-Last-Subject-Sequence`) and per-key TTL header (`Nats-TTL`) are compatible with batch headers and may be combined freely.

The KV bucket's underlying stream must have `AllowAtomicPublish: true` (set via `js.UpdateStream`). The `CreateKeyValue` call does not set this automatically.

See `internal/spike/nats-batch/main.go` (`publishAtomicBatch`) and the `createCoreKVBucket` helper for the minimal implementation pattern.

---

## No Contract Amendment Required

No contradictions between the spike findings and `data-contracts.md` or `lattice-architecture.md` were discovered. No `CONTRACT-AMENDMENT-REQUEST.md` is filed.
