# LoftSpace applicant app — Increment D: documents (P5-clean)

**Status:** ✅ Winston-ratified — build-ready. No frozen-contract change, no architectural fork.
**Owner:** Winston (Steward fire, 2026-06-25).
**Refs:** `implementation-artifacts/loftspace-applicant-app-ux.md` §4.4, §7; `packages/objects-base/`;
`cmd/loupe/objects.go` (the inspector pattern this adapts).

## Problem

Increment D adds the **Documents** surface to the LoftSpace applicant app (`cmd/loftspace-app`):
upload an ID / proof-of-income / signed-lease PDF, see the attached documents, view them. The UX
brief says "lift `cmd/loupe/objects.go`" — but Loupe is the admin **inspector** and reads **Core KV**
directly (`vtx.object.<oid>.content` → `storeName`, plus conditional read-set probing). A vertical app
**must not** read Core KV (P5); the `lint-conventions` P5 gate fails any non-platform `cmd/*` that
references `CoreKVBucket` / `"core-kv"`. So Loupe's handler cannot be lifted verbatim.

The object **byte plane** itself is fine: bytes live in the `core-objects` Object Store
(`CoreObjectsBucket`), which is **not** Core KV and is not gated. The only P5 problem is the object
**metadata** (`oid → storeName / contentType / size`, and "which objects belong to this owner"), which
today lives only in Core KV. No lens projects it for app consumption → a genuine **platform gap**.

## Engine constraint (grounded)

The full rule engine can **filter** by relationship type (`[r:idDocument]`) but **cannot project** a
relationship's name (no `type(r)`; `traverseRel` binds only the destination node — `executor.go:548`).
So a generic lens cannot surface a link's `linkName` (the upload "slot"). This bounds v1 detach (below).

## Decision (Winston)

1. **New additive lens `objectAttachments`** in `objects-base` (NOT an extension of `objectLiveness` —
   keep the GC lens and its heavy `test-object-gc` convergence e2e untouched; the extra per-object
   projection is negligible). actorAggregate, `AnchorType: object`, bucket `weaver-targets`, one row per
   object keyed by the oid. Projects `entityKey`, `storeName`, `contentType`, `size`, and
   `owners` (`collect(DISTINCT {ownerKey: owner.key})` — the my-tasks idiom; the destination node key
   is projectable, the relationship name is not). `EmptyBehavior: delete` so a tombstoned object (which
   does not bind — `fetchNode` returns nil for a soft-deleted vertex) drops from the read model.
   - `storeName`/`contentType`/`size` come from `o.content.data.*` (the proven `objectLiveness`
     aspect-data pattern).

2. **`cmd/loftspace-app/objects.go`** — a P5-clean adaptation of Loupe's handler:
   - **Upload** (`POST /api/objects`): stream bytes to `CoreObjectsBucket` (allowed — byte plane, not
     Core KV), then submit `AttachObject` with `ContextHint.Reads = [targetKey]` **only**. No Core-KV
     probing. `targetKey` is the applicant's `vtx.leaseapp.<id>` or `vtx.identity.<id>` — already known
     to the FE, so no read is needed. Documents are fresh uploads; the dedup/replace branch (which would
     need the object/content keys in `reads`) is **out of v1** — a same-digest re-upload degrades to a
     clean op-rejection (bytes-first → the orphaned bytes are reclaimed), never corruption.
   - **View** (`GET /api/objects/<oid>`): resolve `storeName`/`contentType` from the **`objectAttachments`
     read model** (`WeaverTargetsBucket`, never Core KV), then stream from `CoreObjectsBucket`. Keep
     Loupe's CSP / `octet-stream`-attachment guard verbatim (an uploaded active document must never run
     as same-origin script).
   - **List** (`GET /api/objects?applicant=`): `KVListKeys` + filter the `objectAttachments.*` rows whose
     `owners` contains the applicant's leaseapp/identity — exactly the `applications.go` P5 pattern.
   - **Detach** (`DELETE /api/objects/<oid>?targetKey=&linkName=`): `reads = [linkKey, objKey]`, both
     deterministic + known-present. `linkName` must be supplied by the caller. The FE knows it for
     **session-uploaded** docs (it chose the slot); cross-session detach of a *listed* doc is **deferred**
     (the lens cannot project `linkName` without `type(r)` engine support or a `.content` denormalization —
     a clearly-scoped follow-up, see below). View is always available.

3. **`cmd/loftspace-app`** plumbing: add `uploadCap` to the `server` struct (`OBJECTS_MAX_UPLOAD_BYTES`,
   default 25 MiB), a local `mustJSON`, and register `/api/objects` + `/api/objects/`.

## Scope / non-goals (v1)

- **In:** upload, view, list (P5-clean), session-detach, the Documents FE tab.
- **Out (documented follow-ups):** dedup/replace upload branch; cross-session detach of listed docs;
  `linkName` in the lens (needs `type(r)` engine support or a `.content` `{linkName,filename}`
  denormalization — file as a backlog row).

## Verification

`go build ./...`, `make vet`, `golangci-lint run`, `STRICT=1 lint-conventions` (P5 — loftspace-app stays
**zero** Core-KV refs), the new `objects-base` cypher test, the `loftspace-app` assembler test,
`make verify-package-objects-base`, then in-browser against `make up-full` + `make install-loftspace`:
upload a PDF → it lists → view streams it back as an attachment.

## Outcome (2026-06-25)

**Built + shipped.** All gates green (build/vet/golangci/STRICT-P5/cypher+unit tests/verify-package-objects-base
32-OK). **Live-verified** against `make up-full` + `make install-loftspace`: the full lifecycle —
upload → `objectAttachments` lens projection → scoped list → view (forced `octet-stream` attachment +
CSP) → detach — works end-to-end through the real Processor; the Documents tab renders the scope
selector, upload form, and document cards in-browser with **no console errors**. loftspace-app stays
**zero** Core-KV reads.

**Reliability finding (filed separately) — pre-existing v1b GC concurrent-attach race.** While stress-
testing, freshly-attached objects were GC-reclaimed (3/5 under rapid uploads; a lone unhurried upload
survives). Root cause: `objectLiveness` detects orphans from the **adjacency** projection, which lags the
atomic `AttachObject` commit — exactly the failure the v1b design's **CC1** anticipated ("'0 edges in
adjacency' does not mean '0 live links' — trusting it reaps freshly-attached objects"). The shipped
Option A (lens + epoch-CAS) closes the *re-link* race but not this *attach-adjacency-lag* race (the epoch
was already bumped atomically, so the CAS matches and the tombstone proceeds). This is **orthogonal to
Increment D** (the `objectAttachments` lens correctly reflects whatever state exists; survivors list
correctly) and affects **all** object uploads (Loupe's Files tab too). Filed as a ★★ reliability bug;
the normal single-upload-at-a-time applicant flow works today. Proposed fix direction: make
`objectLiveness` liveness consistent with `linkEpoch` (read a root-data live-link count maintained
atomically by Attach/Detach) instead of the lagging adjacency count — or implement CC1's authoritative
Core-KV link scan in the `TombstoneObject` guard. Needs its own fire (GC convergence + the heavy
`make test-object-gc` e2e + 3-layer review).
