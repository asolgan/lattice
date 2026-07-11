# Dedup over encrypted PII (duplicateCandidates) — design

**Status: ✅ RATIFIED (Andrew, 2026-07-10).** Fork resolved as recommended: index hashes stay
**unkeyed** (consistent with the shipped `identityindex` convention + the multi-credential probe);
the keyed-HMAC upgrade is filed as the cross-cutting **keyed-identity-index-hashes** hardening row
(lattice board, Security table — revive on a production threat model, migrating all index consumers
together). Fire 4 (fuzzy sweep + gc + the CLI vault grant) stays **build-on-demand** with its named
trigger. Zero contract edits. The Lattice Steward builds Fires 1–3; Fire 2 coordinates with the
ratified multi-credential design's disjoint `MergeIdentity` edits (second-lander rebases).
Author: Winston (Designer fire, 2026-07-10).
**🏗️ Fire 1 checkpoint (Steward, 2026-07-11).** Shipped: `indexes` + `duplicateOf` link DDLs
(identity-domain); `CreateUnclaimedIdentity` script emits both on create (name index added as a
third dimension) and activates the dormant probe via a dispatcher optionalReads sweep (`lattice
identity create-unclaimed`, the clinic-app/loftspace-app "New patient/applicant" flows, and the
verify-loupe-operator-tier/verify-real-actor-write-auth dev-seed scripts) — fixes the live
duplicate-create `RevisionConflict`; `duplicateCandidates` lens re-authored minimal/PII-free with
`DiffRetraction: true`; `DiffRetraction` threaded onto the NATS-KV adapter path (was postgres-only —
`corekv_source.go` TargetNATSKVConfig, `pkgmgr/build.go`, the `bucketguard.go` install-time gate);
`cmd/lattice/candidates` re-pointed at the new `<primaryId>.<secondaryId>` row shape, criteria via a
`duplicateOf` link KVGet, merge-edge enumeration via bounded `KVListKeysFilter` excluding
duplicateOf/indexes classes. Full `go test ./...` (97 packages) + `go build`/`make vet`/
`golangci-lint`/`STRICT lint-conventions` all green. Not verified against the live shared dev stack
(`make verify-package-identity-hygiene`) — the two new link-type DDLs are new entities, which F-004's
in-place refresh does not cover, and a fresh bootstrap of the shared stack was judged too disruptive
for a bounded fire; the exhaustive local suite (including real-Processor-pipeline integration tests)
is the substitute evidence. **🏗️ Fire 2 checkpoint (Steward, 2026-07-11).** Shipped: `MergeIdentity`
now tombstones the pair's `duplicateOf` link (both directional keys probed via optionalReads — the
operator may invert primary/secondary from the link's own creation direction, §3.4/§12 finding 6);
repoints the secondary's owned `identityindex` vertices via their inbound `indexes` links (bounded
kv.Links enumeration, tombstone old link + update owner + create new link — no decryption); the edge
trust gate no longer requires `envelope.class == "link"` (real production links carry their relation
name as class, e.g. `holdsRole` — §2.4/§12 finding 7, a bug that failed every production merge on its
first edge). `cmd/lattice/candidates` declares the new optionalReads + the second (`indexes`)
enumeration hint. 4 new e2e tests (forward + inverted duplicateOf tombstone, indexes repoint with a
third-party-untouched assertion, real-class trust-gate acceptance). Full `go test ./...` (97 packages)
+ `go build`/`make vet`/`golangci-lint`/`STRICT lint-conventions` green, **and** verified against the
live shared dev stack this time (`verify-package-identity-hygiene` — CI `stack-gates` job, no new
entities this fire so no bootstrap disruption). **Next: Fire 3** (shred hygiene — `ShredIdentityKey`
in-commit erase of owned indexes + duplicateOf links).
Backlog row: `planning-artifacts/backlog/lattice.md` → *Privacy / Vault → [identity-hygiene] Dedup over
encrypted PII* (★★, M). Charter: `vault-crypto-shredding-design.md` Fire 5b-i checkpoint ("blind-index /
HMAC companion aspect at write time, or a sanctioned engine-side mechanism — routed to the Designer").

---

## For Andrew (one-look ratification block)

**What it does, in two lines.** Identity dedup stops trying to match PII *in the lens engine* (impossible
over per-identity-DEK ciphertext — and, it turns out, broken over plaintext too: the shipped lens never
even activated, §1.1) and moves to **write-time flagging on the `identityindex` hash convention the
platform already ships**: a create-time collision emits a durable `duplicateOf` link, `duplicateCandidates`
re-authors as a **minimal PII-free structural lens** over those links, and merge/shred maintain the index
via a small `indexes` ownership link — no decryption anywhere on the maintenance paths. The Levenshtein
half becomes a trusted decrypt-and-compare CLI sweep (design-complete, build-on-demand).

**No frozen-contract changes.** Everything is build-to (#1 key shapes, #2 §2.5 read posture, #3 events).
Platform code changes are two small, precedented pieces shipping with their first consumer: threading the
*already-implemented* `DiffRetraction`/`KeyLister` onto the NATS-KV adapter path, and (Fire 4 only) a
`lattice.vault.decrypt` NATS grant for the `lattice` CLI user — the latter is a named-trusted-
plaintext-consumer widening called out for your eyes (§3.6).

**One fork for your call — unkeyed vs keyed index hashes (my recommendation: stay unkeyed, file the
keyed upgrade as a follow-on).** The shipped `identityindex` convention (and the 📐 multi-credential
design's §3.4 probe) uses **unkeyed** `sha256NanoID("email:"+value)`. Unkeyed hashes of low-entropy PII
are dictionary-testable by anyone with substrate access, and they persist in JetStream history even
post-shred (§9.1). A keyed HMAC (Vault-held key) would bound that — but it needs a new Vault method, a
key-custody answer for every hash *computer* (the Gateway computes probe hashes from token claims; Starlark
can't hold secrets), and it would fork the platform into two incompatible indexes over the same email
unless the multi-credential probe migrates in the same stroke. Substrate access is already the trust
boundary (a substrate attacker in the dev posture also holds the KEK path to every non-shredded
ciphertext), so I recommend: **reuse the unkeyed convention now, keep the two designs consistent, and file
"keyed identity-index hashes" as one cross-cutting hardening row** that upgrades all consumers together
if/when a production threat model demands it (§10-C has the full analysis).

**Grounding corrections you should know (they change the premise):** the shipped dedup surface is broken
at *four* independent layers, not just "inert post-Vault" — the lens never passed activation-time
key-column validation; the engine never binds relationship variables (its edge columns were always going
to be empty); the create-time index probe is dormant because no production dispatcher declares the reads —
and as a result **a duplicate-contact create hard-fails with `RevisionConflict` today** (§1.1, §2.1). This
design is the first time any of it will have worked end-to-end, and it fixes the live create bug in
Fire 1.

**Review:** two adversarial passes were run this fire — a self-pass while drafting, then an independent
adversarial agent against the finished draft, which refuted three of the self-pass's resolutions and
surfaced the four grounding corrections above; all findings are folded (§12). The pre-build gate is
discharged; no deferred gate is left for the Steward.

---

## 1. Problem & intent

### 1.1 The gap — and its real mechanism (grounded in code, not the reported symptom)

The `duplicateCandidates` lens (`packages/identity-hygiene/lenses.go:35-56`) is written as in-engine PII
matching (`a.email = b.email … OR levenshteinRatio(a.name, b.name) >= 0.85`). The board row's premise —
"post-Vault the matching runs on ciphertext → inert" — is true but understates it. In code, the lens is
dead four ways, outermost first:

1. **It never activated.** The spec declares no `IntoKey`, so pkgmgr defaults the key columns to
   `["key"]` (`internal/pkgmgr/build.go:370-373`) — not a RETURN alias — and activation-time
   `ValidateKeyColumns` fails (`cmd/refractor/main.go:490-495`, `full/ast.go:295-338`). The documented
   output shape `flagged.identity.<lo>.identity.<hi>` is produced by nothing: the NATS-KV key is simply
   the RETURN key-alias values joined with `.` (`internal/refractor/adapter/natskv.go:88-102`); the
   `flagged.` prefix exists only in the *reader* (`cmd/lattice/candidates/candidates.go:233-238`) and
   test helpers, which seed the bucket directly.
2. **Relationship variables are never bound.** `traverseRel` binds only the destination node
   (`internal/refractor/ruleengine/full/executor.go:633-636`); an unbound variable evaluates to nil
   (`:1061-1065`), and `collect` drops nils (`:891-899`) — so `collect(DISTINCT inL.key)` /
   `secondaryInboundEdges` were always going to be `[]`. The operator CLI's merge flow has never had
   lens-provided edge data.
3. **`IN` is silently dropped at parse-translation** (`full/visitor.go:544-548` returns the left
   expression and discards the operator) — the state filters always *passed*, filtering nothing.
4. **Property sugar resolves to the envelope, not the scalar.** A first-hop vertex property yields the
   **whole aspect body map** (`executor.go:1415-1441`; the documented form is `node.<aspect>.data.<field>`,
   `:1410-1412`) — so `a.email = b.email` compared maps whose `key` fields always differ, and
   `levenshteinRatio(a.name, b.name)` would have errored on non-strings (`:1253`). Post-Vault, `data`
   is `{ct, nonce, keyId}` under a **per-identity DEK** (`internal/processor/step65_encrypt.go`), so
   even the scalar form is unmatchable — and the engine cannot decrypt for a NATS-KV target:
   SecureColumns is protected-RLS-Postgres-only, fail-closed three ways
   (`internal/refractor/lens/corekv_source.go:612-614`, `validateSecureColumns`,
   `internal/pkgmgr/bucketguard.go:119`).

The only tests on the spec are **parse-only** (`levenshtein_test.go:38-39`, `parse_test.go:267`); the four
projection tests were deferred at Surface B and never built (`identity-hygiene-surface-b-brief.md:110-122`).
The spec also projects `primaryDetail`/`secondaryDetail` — plaintext name/email/phone into a NATS-KV
bucket — read by nothing (the CLI decodes keys/edges/criterion only, `candidates.go:24-31`).

**Do not treat the old cypher as a working reference for anything.**

### 1.2 Intent

Restore real, continuous duplicate detection for the operator merge workflow (the Surface-B flow:
`duplicate-candidates` bucket → `lattice candidates list/merge` → `MergeIdentity`) such that it (a) works
over encrypted PII by construction, (b) puts **no PII and no PII-derived matchable value in any lens
target**, (c) stays consistent with the identity-index machinery the platform already ships, (d) needs no
decryption on any maintenance path (merge, shred), and (e) states the right-to-erasure residuals honestly.

### 1.3 Why in-engine matching is the wrong seam (resolve it once)

Any scheme that matches *at projection time* needs a cross-identity-comparable value in the engine's
hands. Over per-identity DEKs that value cannot be the ciphertext; it must be a deterministic companion
(a blind index). Carrying that companion **in the aspect envelope** so a lens can join on it means the
matchable value rides into Core KV *and* any lens target that projects the envelope, needs a new Vault
HMAC primitive + a step-6.5 Processor change + a Contract #3 §3.10 envelope edit, keeps the O(N²)
cross-product scan (`executor.go:85,440-475`), and still can't be erased from JetStream history at shred.
The write path, by contrast, **already holds the plaintext** (op params at create; decrypt-on-read at
hydration) and **already computes deterministic hash indexes from it** — the collision check just throws
the evidence away today (§2.1). The fix is to keep it.

## 2. Grounding — what exists (the pattern this extends)

### 2.1 The identityindex convention (shipped — but dormant in production)

`CreateUnclaimedIdentity` (`packages/identity-domain/ddls.go:471-516`) already: normalizes email
(lowercase/trim) and phone (digits + `+`) in-script; derives
`vtx.identityindex.<crypto.sha256NanoID("email:"+email)>` (and `"phone:"+…`) — the Starlark builtin
calls Go-side `substrate.SHA256NanoID` directly, byte-identical
(`internal/processor/starlark_builtins.go:125-136`, `internal/substrate/derive.go:61-77`); probes it via
the step-4 hydrated `state` cache; sets `duplicate = True` on a live hit — but records the evidence only
as a boolean on the `identity.created` event, dropping the colliding identity's key
(`email_hit.data.identityKey`) that is in the script's hands; and creates the index vertex
`{contactType, identityKey}` only when absent (owner = first live identity with that contact; **no
plaintext in key or doc**).

**Dormancy correction (adversarial finding):** the probe reads `state[...]` — the hydrated cache only
(`internal/processor/starlark_kv.go:14-33`) — and **no production dispatcher declares the index keys**
(`lattice identity create-unclaimed` sends no ContextHint at all,
`cmd/lattice/identity/identity.go:96-103`; repo-wide grep finds zero identityindex read declarations
outside package tests, which seed `state` directly). Consequences today: `duplicate` is always false,
and — since `email_index_key not in state` is then always true — a duplicate-contact create
unconditionally re-emits the index-vertex `create`, which rejects with **`RevisionConflict`,
unrecoverably** (the resubmit still never hydrates the key). Fire 1 fixes every dispatcher (§3.2) and
turns this live failure into the flag it was meant to be.

This machinery works **unchanged post-Vault**: scripts see plaintext (params at create, decrypt-on-read
elsewhere — `internal/processor/sensitive_decrypt.go`); encryption happens later, at commit step 6.5. The
📐 multi-credential design's §3.4 provision-time probe builds on exactly this convention (declaring its
probe keys, as this design requires of all dispatchers) and explicitly leaves "the Levenshtein half" to
this design (§3.6 there).

### 2.2 Write-path posture (Contract #2 §2.5)

Probe reads are **declared** (`contextHint.reads` / `optionalReads` at dispatch-known keys, absence-
tolerant, never required — `docs/contracts/02-operation-envelope.md:169-197`); **computed-key writes** are
the claim/index scripts' own idiom (multi-credential §3.3 names the verbs precisely); bounded link
enumeration is **class-(e)** — `kv.Links` declared via `EnumerationHint`, with sanctioned follow-up reads
(the merge CLI already declares one, `candidates.go:194-199`).

### 2.3 Engine + adapter facts that shape the lens (per this fire's grounding + adversarial passes)

- Nested aspect access works and is production-precedented (`i.name.data.ct <> null`,
  `packages/loftspace-domain/lenses.go:163,168`); state filters must be spelled
  `a.state.data.value = '…'` — **as `=`/`OR` equality, not `IN`** (§1.1-3).
- **Relationship variables are unusable** (§1.1-2) — the re-authored lens must be rel-var-free. (Binding
  them is implementable — the adjacency entry carries the link's `CoreKvKey`,
  `internal/refractor/adjacency/builder.go:23` — but this design deliberately needs no engine change;
  §10-G records the rejected variant.)
- **Output key = RETURN key-alias values joined with `.`**, declared via an explicit `IntoKey`
  (`natskv.go:88-102`; pkgmgr default `["key"]` is a trap, §1.1-1). Key segments must be dot-free —
  bare NanoIDs via `nanoIdFromKey` (`executor.go:1265`), not full vertex keys — or DiffRetraction's
  segment-count check breaks (`natskv.go:308-335`).
- With both pattern nodes labeled and no anonymous nodes, `ReferencedLabels` is exhaustive ⇒ the lens
  reprojects on identity-vertex events only (`pipeline.go:237-259`), riding the shipped plain-lens
  aspect/link freshness transport (`5624392`) for `duplicateOf`-link and state-aspect triggers — verified
  by a Fire-1 pipeline test.
- **Retraction:** the pair-keyed output defeats anchor-derived retraction, so the exact transport is
  **DiffRetraction** (lens-wide live-key diff — sound here because the cypher is genuinely unanchored,
  the shipped Fire-3 ruling). The NATS-KV adapter already implements `KeyLister`/`Purge`
  (`natskv.go:15-19,308-335`) and the pipeline side is interface-based (`pipeline/evaluate.go:516-532`);
  only `translateSpec`'s nats_kv branch (`corekv_source.go:592-624`), the bucketguard
  (`bucketguard.go:116-118`), and the data-driven `main.go` gate (`:518-530`) need touching — a
  deliberate "no consumer yet" hold (`corekv_source.go:200-204`) whose first consumer this is.
  `ValidateUnanchoredForDiffRetraction` only rejects `$actorKey` references (`ast.go:359-392`) — the new
  cypher passes. Delete mode stays **hard** (soft tombstones still list as live, `natskv.go:305-307`).

### 2.4 Consumers

`cmd/lattice/candidates` (list + merge construction); `packages/identity-hygiene` `MergeIdentity`;
`scripts/verify-package-identity-hygiene.go` (asserts cypher substrings incl. `levenshteinRatio` — must
be updated); no Loupe or vertical consumer. **Pre-existing merge bug (adversarial finding, folded into
Fire 2):** the merge edge trust gate requires `envelope.class == "link"`
(`packages/identity-hygiene/ddls.go:254-256`), but every production link carries its *relation name* as
class (e.g. `holdsRole`, `identity-domain/ddls.go:591`; the shared `make_link(cls=…)` idiom) — only the
hygiene tests' fixtures use class `"link"`. A production merge with real enumerated edges fails
`EdgeNotALink` on the first edge today.

## 3. The shape

### 3.1 Data model (Contract #1 key shapes — all build-to)

- **`vtx.identityindex.<sha256NanoID("<type>:"+normalized)>`** — existing vertex, unchanged doc shape
  `{contactType, identityKey}`. **New third index class:** `"name:"+normalizeName(name)` (lowercase,
  trim, collapse internal whitespace).
- **`lnk.identityindex.<hash>.indexes.identity.<id>`** — **new link type** (`indexes`): the ownership
  edge from an index vertex to the identity it points at, created in the same batch as the index vertex.
  Sentence test: *"identityindex indexes identity."* This is what makes merge repoint and shred erase
  **decrypt-free**: an identity's owned indexes are enumerable (class-(e), inbound on the identity hub)
  without knowing the plaintext the hashes derive from, and linkage **is** ownership — no conditional
  owner check, no undeclarable reads (§10-F records the rejected variants).
- **`lnk.identity.<newId>.duplicateOf.identity.<existingId>`** — **new link type**: the durable pair
  evidence. Direction honors §1.1: the later-arriving identity is the source; sentence test: *"identity
  duplicateOf identity."* Link `data`: `{criteria: ["exact-email"|"exact-phone"|"exact-name", …]}` (one
  link per distinct matched incumbent; criteria unioned). Provenance is Processor-injected as always.
- **DDL placement + permittedCommands:** both link DDLs live in **identity-domain** (the create-path
  writer's package; identity-hygiene depends on it, so commit-time class resolution works for every
  writer). Both **omit `permittedCommands`** — they are multi-writer classes
  (`CreateUnclaimedIdentity`, `MergeIdentity`, `ShredIdentityKey`, later `FlagDuplicateCandidates`),
  mirroring the contact aspects' deliberate open posture (`identity-domain/ddls.go:29-34`) with the same
  documented rationale. The existing `identityindex` aspect-type DDL's permittedCommands posture is
  re-checked in Fire 1 to admit the new writers (or stay open).
- **No new aspects, no envelope change, no new vertex types.**

### 3.2 Write path (P2 — the flag is committed evidence, not an event-side effect)

`CreateUnclaimedIdentity` extends (same script, same atomic batch):

1. Normalize email/phone (existing) + name (new). Derive the three index keys.
2. **Every dispatcher declares them as optionalReads** — dispatch-known (they derive from op params).
   Fire 1 enumerates and updates the dispatcher set (`lattice identity create-unclaimed` +
   any package/app submitter found by grep; the multi-credential Gateway probe declares its own when it
   lands). This activates the today-dormant probe and **fixes the live duplicate-create
   `RevisionConflict` failure** (§2.1).
3. For each index hit that is live: collect `hit.data.identityKey` → `matched = {identityKey → criteria}`.
4. **Emit one `create` per distinct matched incumbent:**
   `lnk.identity.<new>.duplicateOf.identity.<matched>` with `data.criteria`. (The new identity is minted
   in this batch — its outbound `duplicateOf` cannot pre-exist, so `create` is safe. A `RevisionConflict`
   on the *index* create under concurrent same-contact creates is the existing OCC posture: the loser's
   resubmit re-runs, now sees the index via its declared read, and flags.)
5. For each **absent** index: create the index vertex **and its `indexes` link** (same batch). Keep
   `duplicate` on the `identity.created` event; add `matchedIdentityKeys` to its data (audit aid — the
   link is the durable state; the event's permanence is a named §9.1 residual).

**The writer invariant (documented in both packages' READMEs + the DDL comments):** every op that writes
`.email`/`.phone`/`.name` on an identity maintains the corresponding index + `indexes` link. Verified
repo-wide: today's writer set is exactly `CreateUnclaimedIdentity` + `MergeIdentity`'s conflict
resolution (§3.4). A future writer that skips maintenance degrades detection (misses flags), never
corrupts it — detect-and-recover, with the Fire-4 sweep as backstop.

### 3.3 Read path (P5 — the lens becomes minimal, PII-free, rel-var-free)

`duplicateCandidates` re-authors in `identity-hygiene` (same canonical name, bucket, adapter):

```
MATCH (b:identity)-[:duplicateOf]->(a:identity)
WHERE (a.state.data.value = 'unclaimed' OR a.state.data.value = 'claimed')
  AND (b.state.data.value = 'unclaimed' OR b.state.data.value = 'claimed')
RETURN nanoIdFromKey(a.key) AS primaryId,
       nanoIdFromKey(b.key) AS secondaryId,
       a.key AS primaryKey,
       b.key AS secondaryKey
```

with an explicit **`IntoKey: ["primaryId", "secondaryId"]`** → bucket key `<primaryId>.<secondaryId>`
(dot-free segments; DiffRetraction-compatible, §2.3). Primary = the incumbent (merge target), secondary
= the newcomer. **Dropped:** the PII detail columns, `score`, `criterion`, the edge collections (never
worked, §1.1-2), `levenshteinRatio`, the `(a),(b)` cross product, the `a.key < b.key` ordering hack, and
the fictional `flagged.` key prefix. The seed scan is one `(b:identity)` label pass + `duplicateOf`
traversal — rows exist only for flagged pairs.

**The CLI absorbs what the lens can't express** (`cmd/lattice` is a platform binary — the sanctioned
Core-KV-read exception, per CLAUDE.md P5):

- **`candidates list`:** reads the bucket (new key/row shape); for display it KVGets the pair's
  `duplicateOf` link doc — both directional keys are derivable from the row's IDs; one hits — and shows
  `data.criteria`.
- **`candidates merge`:** enumerates the secondary's live links directly via bounded subject-filtered
  `KVListKeys` (outbound `lnk.identity.<sid>.>`; inbound `lnk.*.*.*.identity.<sid>` — mid-token
  wildcards are server-side-bounded NATS subject filters), **excluding `duplicateOf` and `indexes`
  classes** from the merge edge set, and constructs the `MergeIdentity` envelope as today. This replaces
  the lens edge columns that never carried data — a strictly more truthful source, per-identity bounded,
  not a graph scan.

**Retraction:** the lens declares `DiffRetraction: true` (hard delete mode); Fire 1 threads the flag
through the nats_kv translateSpec/bucketguard/main.go gate (§2.3). A merged pair (state filter drops it;
link tombstoned §3.4) retracts on the next evaluation via the lens-wide live-key diff. The fire ships
both halves together.

**Operator PII display:** the CLI shows keys + criteria only. An operator who wants to eyeball the
colliding values before merging uses the trusted decrypt surface (Loupe / `lattice.vault.decrypt` RPC) —
the standard post-Vault posture; no PII returns to the bucket.

### 3.4 Merge maintenance (`MergeIdentity`, identity-hygiene)

Added to the existing merge commit, mirroring the multi-credential §3.3 idiom (and **coordinating with
it** — both designs edit this script in disjoint regions; whichever builds second rebases):

- **Tombstone the pair's `duplicateOf` link — both directional keys.** The operator may pick either side
  as primary (`candidates.go:114-124`), so the script probes/tombstones
  `lnk.identity.<secondary>.duplicateOf.identity.<primary>` **and** the inverted key — both
  dispatch-derivable and declared (adversarial finding 6: tombstoning only one direction leaves a live
  `duplicateOf` into a merged identity on an inverted merge).
- **Repoint the secondary's owned indexes via the `indexes` links — no decryption.** The script
  enumerates the secondary's inbound `indexes` links (class-(e) `EnumerationHint`, declared by the CLI
  alongside its existing one), and for each: `update` the index vertex → `{contactType, identityKey:
  primary}`, tombstone the old `indexes` link, create the one pointing at primary — same batch. Linkage
  is ownership, so no conditional owner check is needed; an index owned by a third identity has no link
  to the secondary and is untouched.
- **Fix the edge trust gate** (§2.4): accept any class on a well-formed 6-segment `lnk.` key (or
  validate against link-type DDLs) instead of `class == "link"`, with a real-class merge test — without
  this, the flow this design restores still fails one step later. Other `duplicateOf` links touching the
  secondary (to third incumbents) are simply rekeyed like any other edge if the operator includes them —
  self-loops are already tombstone-only in the rekey (`identity-hygiene/ddls.go:315-317`), so no
  self-link hazard exists (the draft's earlier self-link concern was refuted by the adversarial pass) —
  but the CLI excludes them from the default edge set (§3.3) as pair-evidence, not business edges.
- With conflict resolution `secondary-wins`, an index primary previously owned (its old contact) now
  points at an identity no longer bearing that contact — at worst a future operator-judged flag against
  primary; self-correcting on the next write of that contact; documented, not engineered around.

### 3.5 Shred hygiene — erase the live index footprint *inside* the shred commit

**The adversarial pass killed the draft's worker-side erase:** `ShredIdentityKey` durably sets
`piiKey.shredded = true` in the same commit that emits `privacy.keyShredded`
(`packages/privacy-base/shred_identity_key.go:166-184`), and the Vault refuses decryption on that flag
*before* the deny-list is consulted (`internal/vault/local.go:322-332`) — so by the time the privacy
worker runs, the decrypt window is **already closed**, and there is no shred convergence marker to
re-drive an erase (the Weaver shred lens is explicitly future, `shred_identity_key.go:232-236`).

The correct seam is **the `ShredIdentityKey` commit itself** — and with the `indexes` link it needs no
decryption at all:

- The script gains two class-(e) enumerations (declared by every shred dispatcher as
  `EnumerationHint`s): the identity's inbound `indexes` links, and its `duplicateOf` links (both
  directions).
- Same atomic batch as the shred intent: tombstone each owned index vertex + its `indexes` link;
  tombstone each `duplicateOf` link. Bounded (≤3 indexes + the pair links), far under the batch ceiling.
- **No ordering windows exist**: erase and intent commit atomically; the privacy worker and the
  finalization steps are untouched. Re-shred is idempotent (enumerations of live links return nothing;
  readers filter tombstones per Contract #1).
- Fire 3 enumerates the (small) `ShredIdentityKey` dispatcher set and adds the hint declarations.

What this does **not** erase: prior revisions in JetStream history (append-only), index vertices of
identities shredded *before* this ships (no links to find them by), and stale entries left by any
erase-less path. Backstop: **`lattice candidates gc`** (rides Fire 4's CLI work, or standalone) —
enumerates `vtx.identityindex.*`, checks each owner's `piiKey.shredded` / absence, and submits tombstones
via an op; possible **without decryption**. Residuals stated in §9.1.

### 3.6 The Levenshtein half — a trusted decrypt-and-compare sweep (design-complete, build-on-demand)

Fuzzy matching is unreachable by *any* at-rest companion value (a deterministic index only does exact
buckets), and in-engine decrypt is fail-closed by design (§2.3). The sanctioned surface that may hold
plaintext for this purpose is a **trusted platform tool**:

- `lattice candidates scan` (new CLI verb, Fire 4): enumerate live identities (platform binary — Core-KV
  read sanctioned), decrypt `.name` via the Vault RPC, compute pairwise `levenshteinRatio ≥ 0.85` (reuse
  the engine's Wagner-Fischer via a small shared util), and submit one **`FlagDuplicateCandidates`** op
  (new, identity-hygiene; operator-granted) whose script creates the corresponding `duplicateOf` links
  (`criteria: ["levenshtein-name"]`; create-if-absent via declared probe reads on the exact link keys —
  dispatch-known, the CLI computed the pairs).
- **Security-plane deliverable, named for Andrew:** the `lattice` CLI NATS user has **no vault subject
  grants today** — `lattice.vault.decrypt` pubAllow is Loupe-only (`deploy/gen-dev-nkeys/main.go:214-238`).
  Fire 4 widens it to the `lattice` user: a new named trusted-plaintext consumer, the same class of change
  as the ratified loftspace wrapkey widening (`main.go:249-258`). Fires 1–3 need **no** vault grant
  (§3.4/§3.5 are decrypt-free — this is why the `indexes` link earns its keep).
- Flagged pairs flow through the *same* lens/CLI/merge machinery — the sweep is a producer, not a
  parallel surface. O(N²) pairwise cost is operator-invoked and offline, not on the CDC hot path.
- **Build trigger (dead-scaffolding gate):** built when an operator/PO actually reports a missed
  duplicate the exact criteria didn't catch, or when a production backfill (§6) is needed. Until then
  exact-name (§3.2) covers the case-/whitespace-variant mass. The design here is complete so the Steward
  can build Fire 4 cold.

## 4. Reconciliation with the existing mental model

- **"Didn't we already handle dedup?"** The pieces exist but none of them work in production: the
  create-time probe is dormant (undeclared reads — and duplicate creates hard-fail today), the pair
  evidence was never durably recorded, and the lens never activated (§1.1, §2.1). This design connects
  and repairs the shipped halves rather than adding a third mechanism.
- **"Doesn't the multi-credential design cover this?"** No — it *consumes* the same index convention for
  provision-time hinting and explicitly leaves this row the matching problem (§3.6 there). Touchpoints:
  both edit `MergeIdentity` (disjoint regions — credentialindex vs identityindex/duplicateOf; build-order
  note in §11), and both standardize on unkeyed `sha256NanoID` hashes (the fork flagged for Andrew).
- **"Is this new state we keep elsewhere?"** The `duplicateOf` link is new durable state — the *only*
  durable record of the pair (the alternative, recomputing pairs in-engine, is the thing being removed).
  The `indexes` link records ownership that today exists only implicitly (index-doc `identityKey` +
  undecryptable hashes); making it a link is what frees merge/shred from decryption.
- **"Does the adapter-read-seam 'Starlark sensitivity-detection primitive' overlap?"** Adjacent, not
  colliding: that primitive is a read-path *classifier*; this design needs no sensitivity introspection —
  scripts hash plaintext they already legitimately hold. Both consume the same `sensitive:true` DDL
  metadata; neither changes it.
- **"Why is the platform lane doing package work?"** The mechanism is package-shaped by design (P5), but
  the fire set includes genuine platform pieces: the nats-kv DiffRetraction threading, the Fire-4 NATS
  grant, and the CLI. The identity packages are platform-owned.

## 5. Contract surface

| Contract | § | Change vs build-to |
|---|---|---|
| #1 Addressing | key shapes; §1.1 link direction | **build-to** (`duplicateOf`, `indexes` pass the sentence test; later-arriving = source at creation; 6-segment shapes) |
| #2 Operation envelope | §2.5 declared reads / optionalReads / class-(e) enumerations | **build-to** (all probes dispatch-known; merge/shred enumerations declared at every dispatcher) |
| #3 MutationBatch | §3.10 envelope; events | **build-to — deliberately unchanged** (no `bi` field enters the envelope; `identity.created` data gains a field, package-owned) |
| #5 Health | — | untouched |
| #7 Bootstrap | — | no new reserved types |
| #10 Orchestration | — | untouched (the shred erase is in-commit; worker/finalization unchanged) |

**Zero frozen-contract edits.** (The rejected HMAC-envelope alternative would have required a §3.10 edit —
one of the reasons it lost, §10-B.)

## 6. Migration / compatibility

- **Forward-only detection.** Existing identities keep their email/phone index vertices; they lack
  `indexes` links, name-index entries, and `duplicateOf` links. No backfill is built: dev data is
  synthetic and reset-friendly, and the package-version bumps ride the F-004 in-place DDL hot-reload (new
  link/DDL classes; no kernel-seed change). Pre-existing index vertices without `indexes` links are
  invisible to merge repoint / shred erase — covered by the `candidates gc` sweep (§3.5) and dev resets;
  a production deployment would run the Fire-4 sweep once as backfill. Deferred with its driver.
- **CLI compatibility:** bucket key becomes `<primaryId>.<secondaryId>` and rows carry
  keys/IDs only — `deriveCandidateKey`, the row struct, and the edge sourcing all change in Fire 1
  (`cmd/lattice/candidates` is the only consumer; the old `flagged.` shape never existed in a real
  bucket, §1.1-1).
- **verify-package-identity-hygiene.go:** cypher substring assertions updated (drop `levenshteinRatio` +
  edge columns; assert `duplicateOf`, the IntoKey aliases, and a **negative** assertion that no
  `Detail`/PII column names appear).

## 7. Test strategy

- **The first real projection test for this lens** (closing the Surface-B deferral): a
  `lens_cypher_test.go` in identity-hygiene running the re-authored cypher through the **full engine**
  (the `packages/loftspace-domain/lens_cypher_test.go` 5b-i precedent) — flagged pair projects with the
  declared IntoKey shape; unflagged identities don't; merged/tombstoned pair drops; state filter (as
  `=`/`OR`) actually filters.
- **Activation test:** the lens passes `ValidateKeyColumns` with the explicit IntoKey (the exact gate the
  old spec died on).
- **Script tests (identity-domain):** create-collision emits the link with the right criteria (email /
  phone / name / multi-criteria union / distinct incumbents ⇒ distinct links); no collision ⇒ no link +
  index/`indexes`-link creation; duplicate create **no longer RevisionConflicts** with declared reads;
  name normalization; dispatcher ContextHint coverage.
- **Merge tests (extending `merge_test.go`):** both-direction pair-link tombstone; `indexes`-driven
  repoint (owned moves, third-party untouched); the trust-gate fix with **real-class** links.
- **Shred (extend `make test-crypto-shred`):** post-shred, owned index vertices + `indexes` links +
  `duplicateOf` links are tombstoned in the same commit as the intent; re-shred idempotent; a
  post-shred create with the same email produces a fresh index and no link to the shredded identity
  (Gate-3-style vector: *shredded contacts are not matchable*).
- **Retraction e2e:** with DiffRetraction threaded, a merged pair's bucket key disappears on the next
  evaluation (embedded-NATS pipeline test, the Fire-3 retraction pattern); the `duplicateOf`-link-create
  trigger reprojects via the shipped freshness transport.

## 8. Fire decomposition (for the Lattice Steward, after ratification)

1. **Fire 1 — flag + lens + retraction (M).** identity-domain: `duplicateOf` + `indexes` link DDLs, name
   index, `CreateUnclaimedIdentity` link emission, **dispatcher optionalReads sweep** (fixes the live
   duplicate-create failure); identity-hygiene: lens re-author (minimal shape, explicit IntoKey) + the
   projection/activation tests; platform: thread `DiffRetraction` onto nats_kv (translateSpec +
   bucketguard + main.go); CLI: new row/key shape, criteria display via link-doc KVGet, merge-edge
   enumeration via bounded KVListKeys; verify-script update. Internal build order: DDLs → script +
   dispatchers → lens → threading → CLI.
2. **Fire 2 — merge maintenance (S).** `MergeIdentity`: both-direction pair-link tombstone,
   `indexes`-driven repoint, **edge trust-gate fix** (real-class links); CLI enumeration-hint additions.
   **Coordinate with multi-credential §3.3** (same script, disjoint region; second-lander rebases). No
   vault grant needed.
3. **Fire 3 — shred hygiene (S).** `ShredIdentityKey` in-commit erase (two enumerations + tombstones);
   dispatcher hint sweep; crypto-shred e2e extension. No worker/finalization change.
4. **Fire 4 — fuzzy sweep + gc (S–M, build-on-demand).** `lattice candidates scan` + `gc` +
   `FlagDuplicateCandidates`; **the `lattice`-user `lattice.vault.decrypt` grant (Andrew-visible)**;
   trigger: a real missed-duplicate demand signal or a production backfill need.

Fires 2–4 consume Fire 1's DDLs; each is independently shippable and green.

## 9. Risks & residuals (stated, not hidden)

### 9.1 The permanence residuals (the fork, §For-Andrew)

`sha256NanoID("email:"+value)` is dictionary-testable for low-entropy PII by an attacker with Core-KV /
JetStream-history access, and **history is append-only**: even after Fire 3 tombstones the live
footprint, prior revisions persist. Post-shred, a substrate-level attacker can *confirm a guessed*
email/phone/name once borne by a shredded identity (the value is not recoverable except by guessing).
Also permanent, same class: the `matchedIdentityKeys` on `identity.created` events and the
`duplicateOf` link history — both durably associate a (later-shredded) identity with named incumbents;
and index vertices of identities shredded **before** Fire 3 ships stay live until the `gc` sweep runs
(§3.5). Bounding facts: (a) the unkeyed-hash class **already shipped** with `identityindex` and is relied
on by the 📐 multi-credential probe — this design adds the name class and the association records, not
the class itself; (b) substrate access is the platform trust boundary (that attacker also holds every
non-shredded identity's ciphertext + the dev-posture KEK path); (c) the keyed alternative is a
cross-cutting upgrade with real custody questions (§10-C) that should move *all* index consumers at
once. **Recommendation: accept + file the keyed-hash hardening row.**

### 9.2 Others

- **Create-time-only flagging.** A pair becomes flaggable only when the second identity's contact is
  written. Today's writer set is closed (verified repo-wide: create + merge-conflict-resolution); a
  future contact-editing op must honor the §3.2 writer invariant — a documented convention with the
  Fire-4 sweep as detect-and-recover backstop, not a structural gate (§10-E).
- **Transitive pairs flag against the index owner only** (A←B, A←C; never B–C). Correct for the merge
  workflow (all roads lead to the incumbent); noted so nobody files it as a bug.
- **Stale index owner after `secondary-wins` conflict resolution** (§3.4) — harmless, self-correcting,
  documented.
- **Trigger breadth.** The new labeled shape narrows reprojection to identity events (better than
  today's reproject-on-everything); correctness of link/aspect-event triggering rides the shipped
  freshness transport and is pinned by a Fire-1 test.
- **Name-index noise.** Exact-normalized name collisions are common. At dev/POC scale trivial; if it
  drowns operators, the criterion is package-config territory (a lens/package parameter), not a platform
  change.
- **`indexes`/`duplicateOf` links appear in Core KV link enumerations** — any `kv.Links` consumer on an
  identity hub sees them; the CLI excludes them from merge edges (§3.3), and their docs carry no PII
  (keys carry the hash — the §9.1 class).

## 10. Alternatives considered

- **A. Do nothing / retire the lens.** The merge workflow is real (Surface B, the CLI, the in-flight
  multi-credential merge hardening); leaving dedup dead orphans it — and leaves the duplicate-create
  hard-fail (§2.1) unfixed. Rejected.
- **B. HMAC blind index in the ciphertext envelope + in-engine equality (the charter's first option).**
  Needs a new Vault primitive (the interface has no MAC/derive — `internal/vault/vault.go:79-142`), a
  step-6.5 Processor change, a Contract #3 §3.10 envelope edit, a DDL `blindIndex` declaration surface —
  and, per the adversarial engine pass, *additional* engine work anyway (the `IN`/rel-var/key-column
  gaps of §1.1 bite any in-engine shape). Keeps the O(N²) cross-product on every CDC event; puts a
  deterministic PII-derived matchable value into Core KV and every envelope-projecting lens target; its
  post-shred residual is *worse* than §9.1 (the companion rides the sensitive aspect's own history,
  unstrippable). Re-checked per the "could a variant beat the recommendation?" discipline: a truncated-
  HMAC variant reduces but does not remove the residuals and keeps all the platform surface. Rejected.
- **C. Keyed HMAC for the index keys themselves.** Fixes §9.1's dictionary-testability. Costs: a Vault
  `MAC` method + key custody at every hash computer — Starlark can't hold the key (sandbox-pure builtins
  only, `starlark_builtins.go:112-170`), so derivation moves into the Processor (a new commit-path
  feature) or the dispatchers (Gateway/CLI holding a MAC key — a new secret surface); and the 📐
  multi-credential probe computes hashes in the Gateway from token claims, so a keyed scheme forks the
  two designs unless migrated together. Deferred as the cross-cutting hardening row — the ratification
  fork.
- **D. Phonetic (metaphone/soundex) index as the fuzzy substitute.** A new normalizer with a poor
  false-positive profile for marginal recall over exact-name + the Fire-4 sweep. Rejected.
- **E. Step-6.5 auto-maintained indexes (platform mechanism instead of the script convention).** Would
  make the writer invariant structural — but it is a new commit-path feature with one consumer, on the
  security-adjacent plane (dead-scaffolding test fails). Revisit if a third index-maintaining writer
  appears.
- **F. Ownership without the `indexes` link.** (i) Conditional repoint/erase by *reading* the index
  vertices — their keys derive only from plaintext, which no dispatcher can declare and post-shred no
  one can recompute (the adversarial pass proved the worker's decrypt window is closed, §3.5); (ii) a
  `contactIndexes` aspect on the identity listing owned hashes — duplicates state the link expresses
  with a divergence risk and no enumeration story. The link is the graph-native form with three in-design
  consumers (merge, shred, gc). Chosen over both.
- **G. Bind relationship variables in the engine + keep criteria/edges in the lens.** Implementable
  (adjacency carries `CoreKvKey`) and worth having eventually — but it is new executor scope this feature
  doesn't need once the CLI sources edges/criteria directly (§3.3), and shipping it here would couple a
  privacy fire to engine work. Left for a future demand-driven engine row.
- **H. Engine-side decrypt-before-match ("sanctioned engine mechanism", the charter's second option).**
  Fail-closed three ways today for exactly the right reasons (§2.3): it would decrypt every identity's
  PII into engine memory on every CDC event to serve a NATS-KV target that must never hold plaintext.
  Rejected without hedging.

## 11. Coordination notes

- **multi-credential-identity-linking (📐, same week):** shared `MergeIdentity` script (disjoint edits;
  second-lander rebases), shared unkeyed-hash convention (the §9.1 fork applies to both; ratify
  consistently), shared dispatcher-declared-probe idiom. No mechanism conflict.
- **adapter-read-seam (🚧 blocked-on Designer):** unaffected; its sensitivity-detection primitive remains
  its own item (§4).
- **negative/filter-retraction machinery:** this design *consumes* DiffRetraction (threads it to
  nats-kv), it does not extend the retraction design's scope.

## 12. Adversarial passes (pre-build gate — run this fire, findings folded)

Two passes were run 2026-07-10 (this Designer fire):

**Pass 1 (self, while drafting)** caught: the erase-after-destroy dead end (superseded by pass 2's
stronger finding), the conditional-repoint theft hazard, the retraction-availability overclaim, and the
§1.1 premise understatement.

**Pass 2 (independent adversarial agent, against the finished draft)** refuted three of pass 1's
resolutions and re-grounded the design; every finding is folded above:

1. **HIGH — rel variables never bound** → lens re-shaped rel-var-free; criteria/edges moved to the CLI
   (§3.3); engine work explicitly rejected as scope (§10-G).
2. **HIGH — `IN` silently dropped at parse** → state filters as `=`/`OR`; §1.1 mechanism corrected.
3. **HIGH — no output-key template exists; the old lens never activated** → explicit `IntoKey` +
   dot-free NanoID segments + CLI key change (§3.3); activation test added (§7).
4. **HIGH — the shred decrypt window is closed before the worker runs** (`shredded` flag commits with
   the event; Vault refuses on it) → erase moved inside the `ShredIdentityKey` commit, made decrypt-free
   via the `indexes` link (§3.5); the draft's worker sequencing + `indexesErased` step deleted.
5. **MED — the create-time probe is dormant (no dispatcher declares) and duplicate creates hard-fail
   with `RevisionConflict` today** → dispatcher sweep made Fire-1 scope; §2.1 corrected; live-bug fix
   named a deliverable.
6. **MED — inverted merge leaves the pair link live; the self-link fear was false** → both-direction
   tombstone (§3.4); rekey self-loop handling credited.
7. **MED — merge edge trust gate rejects every production link (`class == "link"`)** → trust-gate fix
   folded into Fire 2 (§2.4, §3.4).
8. **MED — the CLI has no vault-RPC grant** → Fires 1–3 redesigned decrypt-free; the grant confined to
   Fire 4 and named for Andrew (§3.6).
9. **MED — no Weaver shred marker exists to re-drive an erase** → moot under the in-commit erase (§3.5).
10. **LOW — `permittedCommands` unspecified for multi-writer link DDLs** → resolved: omitted, mirroring
    the contact-aspect posture (§3.1).
11. **LOW — §9.1 incomplete** → event/link-history association records + pre-Fire-3 live orphans added;
    `gc` sweep backstop specified (§3.5, §9.1).
12. **LOW — §1.1 understated (never activated; edges always empty)** → folded; "do not treat the old
    cypher as a reference" stated.

Confirmations from the same pass (claims that survived attack): builtin/Go hash byte-parity; Contract #1
direction + shapes; §2.5 optionalReads semantics; same-batch link-create safety; DiffRetraction threading
really is the three named touchpoints and the new cypher passes `ValidateUnanchoredForDiffRetraction`;
the writer-invariant set is complete repo-wide; the multi-credential coordination claims hold.

No open findings. The design self-flags **no deferred pre-build gate** — the Steward may build Fire 1 on
ratification.
