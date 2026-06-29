# Design — Full-engine multi-column key construction: the GrantTable producer + anchor-tombstone retraction

**Status: 📐 awaiting-Andrew (ratification)** · Author: Winston (Designer fire, 2026-06-29) ·
**Component:** Refractor (`internal/refractor/ruleengine/full` + `internal/refractor/pipeline` + a one-line activation thread in `cmd/refractor` and the bootstrap seeder) ·
**Backlog row:** `lattice.md` → *Read-model / projection maturity* → "[Refractor] GrantTable lens grant-retraction on anchor tombstone — the composite-keyed delete path" (★★, S–M, filed by the Lattice Steward 2026-06-29 from the D1.3 Increment-1 self-review).
**Frozen-contract change:** none (build-to Contract #6 §6.14; `docs/components/refractor.md` note only).

---

## For Andrew (one-look ratification)

**What it does (two lines).** A GrantTable lens (the D1 `capabilityReadGrants` self-grant producer) projects a
**3-column composite key** `(actor_id, anchor_id, grant_source)`, but the full openCypher engine builds a
projection's key map from only its **first** RETURN item — a single column. This makes the full engine build the
**complete multi-column key** (mirroring the simple engine, which already does), so the grant lens's rows write
correctly (upsert) **and** retract correctly when an identity is tombstoned (the filed gap).

**The grounding surfaced a bigger, more urgent problem than the filed row — please read this.** The backlog
filed only the *retraction* gap (grants accumulate monotonically). Grounding the engine shows the same
single-column-key limitation breaks the **producer** too: the live `capabilityReadGrants` projection emits a key
map of just `{actor_id: …}`, but `GrantWriterAdapter.Upsert` requires all three key columns and **errors
`grant writer adapter: key "anchor_id" absent`** on every row. So — by code analysis — **the live lens almost
certainly never populates `actor_read_grants` at all.** D1 Increment 1's green proofs are an engine-projection
test that asserts `r.Values` (which *does* carry all three columns) and a Fire-1b integration test that hands the
adapter a **manually-built** 3-column keys map — **neither exercises the full-engine → projection-key →
`GrantWriterAdapter` path**, so the gap was invisible. This is the *next* D1 bounce the chain-grounding caught,
and it is **likely a D1.3 Fire-3 blocker**, not the "does not block" the row assumed: with an empty grant table,
RLS denies **all** protected reads (the D1 design's R2 "empty grant table = total read outage"), so "A sees only
A" can't be proven — A sees nothing. **Action for you/the Steward:** Fire 1 of this design first *verifies the
live populate fails today* (a `make up` + `REFRACTOR_PG_DSN` check), then fixes the producer; the retraction
(the filed row) is Fire 2. *(Lesson, again: "make up populates live" was an assertion, not a verification — the
test only proved the cypher derives the right `Values`, not that the pipeline writes them as a composite key.)*

**No architectural fork.** A localized correctness fix on the established full-engine seam — no Gateway / D1 /
Vault / multi-cell surface. The security-plane sensitivity (it is the grant source RLS trusts) argues for the
**full 3-layer review at build**, not a fork decision.

**No frozen-contract change.** Contract #6 §6.14 already mandates the dual NATS-KV+Postgres projection, the
`(actor_id, anchor_id, grant_source)` composite key, the seq-guarded soft-tombstone, and **per-`grant_source`
retraction** ("a revoke from one package deletes only that package's rows"). This *realizes* that contract on the
full-engine path; it does not change it. Only `docs/components/refractor.md` (a freely-editable component doc)
gets a note. Nothing under `docs/contracts/` is touched.

---

## 1. Problem & intent

### 1.1 The filed gap (retraction)

`actor_read_grants` accumulates **monotonically**: a tombstoned identity's `cap-read` self-grant is never
auto-revoked. The full-engine anchor-tombstone path `full.Engine.AnchorDeleteResult` (`679fe25`) resolves only a
`<anchor>.key` / root-field key into a **single** output column, but a GrantTable lens (a) keys its first RETURN
on a `nanoIdFromKey(identity.key)` **function call** (`AnchorDeleteResult` can't resolve a non-property-access
expression), and (b) needs a **3-column composite** Delete the single-column path can't produce. So no Delete
fires on an identity tombstone. The backlog correctly notes the lingering self-grant is *inert, not a leak* —
it's self-only (`actor == anchor`), and a revoked identity is denied at the D1.2 JWT boundary before RLS — but
unbounded table growth + a stale grant surviving a re-activated-NanoID collision is real hygiene/correctness debt
§6.14 mandates be closed.

### 1.2 The bigger gap grounding surfaced (the producer)

Both gaps share **one root cause**: the full engine does not build a complete multi-column projection key. In
`applyReturn` (`internal/refractor/ruleengine/full/executor.go:1006-1023`):

```go
// Use the first projection item as the key when present, mirroring the simple
// engine's "alias becomes the key column" convention.
keyMap := map[string]any{}
if len(r.Items) > 0 {
    alias := r.Items[0].Alias // …
    keyMap[alias] = values[alias]
}
out = append(out, ruleengine.ProjectionResult{Key: keyMap, Values: values})
```

Only the **first** RETURN item lands in `keyMap`. The `Values` map carries every column (so the engine-only test
passes), but the **key** map — what the pipeline hands to the adapter — has one entry. For the grant lens that
key is `{actor_id: <NanoID>}`. The pipeline does no key completion (the `Pipeline` struct holds no key columns;
`cmd/refractor` passes `r.Into.Key` only to `simple.Compile` and the adapter constructors, **never to the full
engine**). The adapter is authoritative on keys and does **not** fall back to the row:

- `GrantWriterAdapter.Upsert` → `grantKeyFields(keys)` reads `keys["actor_id"]`, `keys["anchor_id"]`,
  `keys["grant_source"]` and **errors** on the first absent one (`read_path_adapters.go:35-58`).
- `PostgresAdapter.buildUpsertSQL` likewise errors `key field %q absent from keys map` for any configured key
  column missing from `keys` (`postgres.go:99-101`).

**Why this was never hit before.** Every other full-engine lens is one of: (a) **single-key** plain lenses
(clinic/loftspace/lease business models — `Into.Key` has one column, so first-item = the whole key, identical);
or (b) **actor-aggregate** lenses whose envelope (`EnvelopeFn`) rewrites the key to `newKeys`, discarding
`applyReturn`'s key map entirely. `capabilityReadGrants` is the **first plain full-engine lens with a composite
key** — so it's the first to expose the gap, and it's dormant (no live Postgres assertion beyond the
engine-`Values` test), which is why CI stayed green.

**Consequence.** The live `capabilityReadGrants` projection write fails on every row (`anchor_id absent`,
classified and logged by `writeResults`), so `actor_read_grants` stays **empty**. D1.3 read enforcement, when
turned on (Verticals Fire 3), would then deny **all** protected reads (FORCE-RLS deny-all over an empty grant
table). The milestone "A's JWT sees only A's applications" degrades to "A sees nothing." This contradicts the
filed row's "does not block D1.3 Fire 3."

### 1.3 The established precedent — the simple engine already does this right

The **simple** engine compiles against `Into.Key` (`simple.Compile(q, keyFields)`) and builds a **multi-column**
key on both paths:

- normal evaluate: the plan marks each key column (`col.IsKey`) and the evaluator populates all of them;
- `deleteResult` (`simple/evaluator.go:313-325`) iterates **every** key column and extracts each from the
  anchor's props.

The full engine never received the key-column list, so it fell back to the first-item heuristic. **The fix is to
bring the full engine to parity with the simple engine: give it the key columns, and let it build the complete
key map** — for the upsert (from the projected row's values) and for the anchor-tombstone delete (resolved
against the tombstoned anchor). This is the smallest change that closes both gaps, and it mirrors code that
already exists and is proven.

### 1.4 Intent

Make the full engine obey the same multi-column-key invariant the simple engine already obeys, so:

1. a multi-column-key plain full-engine lens **writes** its rows (the grant producer works — D1.3 unblocked); and
2. a soft-deleted anchor **retracts** its composite-keyed row (the filed gap — §6.14 per-`grant_source`
   retraction realized on the anchor-tombstone trigger).

Minimal, localized, mirrors the simple engine and the existing `AnchorDeleteResult`/actor-aware-shortcut
precedents, no new concepts.

---

## 2. The shape

### 2.1 Thread the key columns to the full engine (the one new wire)

The full engine learns its key columns the same way the simple engine does — from `Rule.Into.Key`. Carry them on
the compiled artifact so they travel with the rule and need no signature churn on the hot `ExecuteWith` path:

```go
// internal/refractor/ruleengine/full/ast.go
type CompiledRule struct {
    Query      *Query
    KeyColumns []string // RETURN aliases designated as the output key (from Rule.Into.Key); set at activation.
}
```

Set it at activation, in the two places a full lens is wired:

- **`cmd/refractor/main.go`** — after `p.UseFullEngine(fullEngine, r.CompiledRule)`, set
  `r.CompiledRule.(*full.CompiledRule).KeyColumns = []string(r.Into.Key)` (a tiny, type-asserted assignment at
  the existing full-engine branch, `main.go:~325`).
- **the bootstrap primordial path** — the seeded `capabilityReadGrants` lens flows through the same
  `translateSpec` → `r.Into.Key = [actor_id, anchor_id, grant_source]` (defaulted from `adapter.GrantKeyColumns`
  when `GrantTable: true`, `corekv_source.go:455`), so the same activation thread in `cmd/refractor` covers it —
  **no separate bootstrap edit** (the kernel grant lens is activated by Refractor like any other).

`KeyColumns` empty/unset (e.g. tests that build a `CompiledRule` directly) → the engine keeps **today's exact
first-item fallback**, so nothing regresses.

### 2.2 Upsert — build the complete key map (Fire 1, the producer fix)

In `applyReturn`, when `KeyColumns` is non-empty, build `keyMap` from **all** key columns, each value pulled from
the projected row (`values`) by alias — instead of the first-item-only heuristic:

```go
keyMap := map[string]any{}
if len(ex.keyColumns) > 0 {
    for _, col := range ex.keyColumns {
        keyMap[col] = values[col] // every key column is a RETURN alias; validated at activation (see §2.5)
    }
} else if len(r.Items) > 0 { // unchanged fallback for un-threaded / single-key callers
    alias := r.Items[0].Alias
    if alias == "" { alias = projectionAutoAlias(r.Items[0].Expr, 0) }
    keyMap[alias] = values[alias]
}
```

(`ex.keyColumns` is set from `compiled.KeyColumns` when `ExecuteWith` builds the executor.) For a single-key lens
`KeyColumns = [k]` ⇒ `{k: values[k]}` — **byte-identical** to today (the first RETURN item *is* the key for those
lenses). For the grant lens ⇒ `{actor_id, anchor_id, grant_source}` all present ⇒ `GrantWriterAdapter.Upsert`
succeeds ⇒ `actor_read_grants` populates. **This is the change that unblocks D1.3.**

### 2.3 Delete — resolve the composite key against the tombstoned anchor (Fire 2, the filed retraction)

Generalize `AnchorDeleteResult` from "resolve the first RETURN item" to "resolve **every** key column," evaluated
against a **Core-KV-free** binding of the tombstoned anchor — so a function-call key like `nanoIdFromKey(id.key)`
resolves exactly as it does on the upsert path, with no re-scan (the anchor is gone):

```go
func (e *Engine) AnchorDeleteResult(cr ruleengine.CompiledRule, eventKey, eventType string,
    eventProps map[string]any) (keys map[string]any, ok bool) {

    compiled := /* type-assert *CompiledRule, nil-guard (unchanged) */
    anchorVar, anchorLabel, found := anchorNode(compiled.Query)
    if !found || anchorLabel == "" || eventType != anchorLabel { return nil, false } // secondary tombstone → re-execute

    cols := compiled.KeyColumns
    if len(cols) == 0 { cols = /* first-RETURN-item alias — today's single-key behavior */ }

    // Map each key column alias → its RETURN expression.
    exprByAlias := returnExprByAlias(compiled.Query) // alias (auto-aliased) → Expr

    // A binding where the anchor var resolves to the tombstoned vertex, with NO Core KV.
    tomb := tombstoneBinding(anchorVar, eventKey, eventProps) // nodeRef{key: eventKey, props: eventProps(+["key"]=eventKey)}

    out := make(map[string]any, len(cols))
    for _, col := range cols {
        expr, present := exprByAlias[col]
        if !present { return nil, false }                 // key column not a RETURN alias — anti-pattern, fall through
        v, resolved := evalKeyExprNoRead(tomb, expr)      // read-free evalExpr (literals, anchor .key/root field, pure fns)
        if !resolved { return nil, false }                // needs a Core-KV read (aspect-keyed) → conservative fall-through
        out[col] = v
    }
    return out, true
}
```

**`evalKeyExprNoRead`** is the existing executor `evalExpr` restricted to **read-free** resolution: `Literal`
(`'cap-read'`), `VariableRef` (the anchor → its `nodeRef`), `PropertyAccess` on the anchor for `.key`
(→ `eventKey`) or a **root-body** field (→ `eventProps[field]`), and `FunctionCall` over resolvable args
(`nanoIdFromKey`, the other pure scalar/string fns — recursively). Any expression that would require a Core-KV
point-read (an **aspect** access `<anchor>.<aspect>.data.<f>`, whose `PropertyAccess.Target` is itself a
`PropertyAccess`) is **unresolvable from a root-tombstone payload** ⇒ `ok=false` ⇒ the caller falls through to a
normal re-execute (no Delete). Implementation note for the Steward: the lowest-churn realization is an executor
with `coreKV == nil` plus a guard in `resolveProperty` that returns "unresolvable" instead of calling `fetchNode`
when `coreKV` is nil — this reuses **all** the pure-function code (including the exact `nanoIdFromKey` the upsert
uses) with zero duplication. For the self-anchor grant lens, all three key columns
(`nanoIdFromKey(identity.key) ×2` + literal `'cap-read'`) resolve from `eventKey` alone — fully read-free.

The pipeline branch (`evaluate.go:105-111`) is **unchanged in shape** — it already calls `AnchorDeleteResult` and
hands the result keys to a `Delete` EvalResult. The only difference is the keys map now carries all three
columns, which `GrantWriterAdapter.Delete` maps to `RevokeGrant(actor, anchor, source, seq)`.

### 2.4 The seq is already correct (no new plumbing for the guard)

`writeResults` stamps `results[i].ProjectionSeq = msg.Sequence` (the **tombstone** CDC message's stream sequence)
before the adapter call (`pipeline.go:658`). That seq is strictly greater than the original grant's upsert seq,
so `RevokeGrant`'s `EXCLUDED.projection_seq > stored` guard **accepts** the revoke and **retains** the watermark
— a later stale re-upsert at the old (lower) seq cannot resurrect the grant (§6.14, `rls.go:257-270`). The
seq-guarded soft-tombstone the contract requires is realized with **no new state and no new field**.

### 2.5 Activation-time validation (fail-closed, mirrors the simple engine)

The simple compiler validates "all keyFields are present as column aliases" (`compiler.go:91-96`). Add the parity
check for the full engine at activation: every `Into.Key` column must be a RETURN alias of the compiled query;
otherwise the lens **fails activation** (logged, not projected) rather than silently dropping a key column at
write time. This catches a mis-declared grant lens up front and keeps the §6.13 fail-closed-activation posture.

### 2.6 Read path (P5) & write path (P2) — honored, unchanged

- **P5 (read):** apps read lens targets; this fix makes the grant lens's target (`actor_read_grants`) *correct*
  (populated, and retracted on tombstone). It adds no app-facing read surface. RLS — the thing that reads
  `actor_read_grants` — is the platform read boundary, not an app Core-KV read.
- **P2 (write):** the Refractor writes only its **own lens target** — `actor_read_grants` **is** the
  `capabilityReadGrants` lens's target. Upserting/revoking a grant row is the Refractor's sanctioned write to its
  own projection, identical to how it already writes every lens target and how the simple/actor-aware paths
  already delete. The Processor stays the sole Core-KV writer; nothing here submits an op.
- **Contract #1 tombstone filter:** the Refractor *acting on* a source (identity) tombstone to retract its
  derived grant row is the projection-layer half of the universal "readers filter tombstones" rule — exactly the
  precedent `679fe25` set for plain lenses.

### 2.7 Orchestration

None. A pure CDC reaction inside the Refractor's existing per-event evaluate→write loop — the same loop the
simple engine, the actor-aware shortcut, and `679fe25` already drive. The precedents mirrored are **`simple/
evaluator.go:313` (`deleteResult`, multi-column key)** and **`full/anchor_delete.go` (`AnchorDeleteResult`,
the single-column path this generalizes)**.

---

## 3. Contract surface

**No `docs/contracts/*` change.** Build-to:

- **Contract #6 §6.14** — already specifies the `(actor_id, anchor_id, grant_source)` composite, the
  seq-guarded soft-tombstone, and per-`grant_source` retraction (`06-capability-kv.md:550-566`). This realizes
  them on the full-engine path. The §6.14 representation note (anchors are bare NanoIDs via `nanoIdFromKey`) is
  honored verbatim on both upsert and revoke.
- **Contract #1** — universal `isDeleted` tombstone / "readers filter tombstones": honored (the Refractor
  propagates a source tombstone into its derived grant view).

**Doc change (allowed, `/docs`):** `docs/components/refractor.md` — under the full-engine / anchor-tombstone
note added by `679fe25`, record that the full engine now builds the **complete multi-column** projection key
(matching the simple engine) for both upsert and anchor-tombstone retraction, and that a GrantTable lens's
self-grant is revoked on its identity's tombstone (seq-guarded). Replace any residual implication that the full
engine only ever single-column-keys.

---

## 4. Migration / compatibility

- **No data migration, no key-shape change, no DDL change, no package change.** Purely Refractor-internal.
- **Existing lenses byte-identical.** Single-key plain lenses: `KeyColumns=[k]` ⇒ same `{k: v}` key. Actor-
  aggregate lenses: their envelope rewrites the key, so `applyReturn`'s key map is discarded as before. The
  delete generalization is gated on `KeyColumns` and on read-free resolvability; any miss falls through to
  today's exact behavior. **A wrong-delete is structurally impossible** (gated on `IsDeleted` + `eventType ==
  anchorLabel` + every key column resolving read-free).
- **The grant table heals forward.** Once Fire 1 ships, the live lens populates `actor_read_grants` on the next
  identity event / a `Pipeline.Rebuild` replay (the existing path) — no bespoke backfill. Rows already leaked by
  the monotonic accumulation (none exist yet — the table is empty today) are likewise healed by rebuild after
  Fire 2.
- **Performance:** strictly better on a tombstone event (a single composite Delete replaces the prior
  full-bucket re-scan-and-reproject that emitted nothing); the upsert path gains an O(keyColumns) map fill (2–3
  entries) per row — negligible.

---

## 5. Test strategy

**Fire 1 (producer / upsert composite key) — the D1.3 unblock:**

1. **Live-verify the gap first (the grounding proof).** On `make up` with `REFRACTOR_PG_DSN`, confirm
   `SELECT count(*) FROM actor_read_grants` is **0** and the Refractor log shows the `anchor_id absent` upsert
   error — establishing the producer is broken today (the claim this design rests on), then confirm it's >0
   after the fix. (A short documented manual/e2e step; the unit + integration tests below are the regression
   guard.)
2. **Engine unit (`full/…_test.go`):** a multi-column-key compiled rule with `KeyColumns=[actor_id, anchor_id,
   grant_source]` ⇒ `ProjectionResult.Key` carries **all three**; a single-key rule ⇒ unchanged single-column
   key; an un-threaded (`KeyColumns=nil`) rule ⇒ today's first-item fallback (regression pin).
3. **Pipeline → adapter integration (`POSTGRES_TEST_DSN`):** drive the **literal `capabilityReadGrants` cypher**
   through the full engine **and the `GrantWriterAdapter`** (the path the existing tests skip) → assert
   `actor_read_grants` holds `(idNanoID, idNanoID, 'cap-read')` per identity. This is the missing end-to-end
   proof.
4. **Activation validation:** a full lens declaring an `Into.Key` column absent from its RETURN fails activation
   (§2.5).

**Fire 2 (anchor-tombstone composite retraction) — the filed gap:**

5. **Engine unit (`anchor_delete_test.go`):** anchor-label tombstone on a GrantTable lens ⇒ `ok==true`,
   `keys == {actor_id, anchor_id, grant_source}` resolved from `eventKey` (function-call key resolves read-free);
   a literal column resolves to its constant; a secondary-type tombstone ⇒ `ok==false`; an aspect-keyed column ⇒
   `ok==false` (read-free fall-through). Single-key plain lens ⇒ unchanged single-column delete (regression pin).
6. **Pipeline unit:** a GrantTable lens; live identity event ⇒ grant upsert; identity **tombstone** ⇒ a
   composite-keyed **Delete** at the tombstone's seq.
7. **Integration (`POSTGRES_TEST_DSN`):** grant present → identity tombstone → row `is_deleted=true`; a stale
   lower-seq re-upsert does **not** resurrect it (the §6.14 H4 guard, end-to-end through the engine).

**Fire 3 (security e2e — the enforcement proof):** on the ephemeral stack, an identity whose grant is revoked by
tombstone can no longer pass the RLS membership check for its anchor (composed with the D1.3 read boundary once
it lands) — the executable proof that retraction reaches RLS.

**Gates (every fire):** `go build ./...`, `make vet`, `golangci-lint run ./...`, STRICT `lint-conventions`,
`go test ./internal/refractor/...`, `verify-kernel` (Fire 1 touches the bootstrap-seeded grant lens activation),
and the relevant package e2e. No bypass/capability-write surface is touched (read-model projection path only).

---

## 6. Risks & alternatives

### 6.1 Risks

- **R1 — the producer-gap claim is code-analysis, not yet run live.** *Mitigation:* Fire 1 step 1 is *verify the
  failure on a live stack first*. If (unexpectedly) the live populate already works — i.e. some path I didn't
  find completes the key — Fire 1's engine change is still correct and harmless (it makes the key construction
  explicit and tested), and Fire 2 (retraction) is unaffected. The change is safe under either reading; the
  finding only raises its **priority**.
- **R2 — changing `applyReturn` could perturb an existing lens.** *Mitigation:* the multi-column build activates
  only when `KeyColumns` is threaded; single-key lenses get an identical key; actor-aggregate lenses discard the
  key map via their envelope. Pinned by the regression tests (§5.2, §5.5). The fallback preserves the exact
  current behavior for any un-threaded caller.
- **R3 — the read-free delete evaluator silently mis-resolves a key.** *Mitigation:* it's **conservative** — any
  column it can't resolve read-free yields `ok=false` and falls through to today's behavior (no Delete, linger),
  never a wrong Delete. The grant lens keys read-free by construction. Security-plane ⇒ **full 3-layer at
  build**, with explicit adversarial attention to "could a crafted lens make this delete the wrong grant row"
  (answer by construction: gated on anchor-label match + read-free resolution of every key column).
- **R4 — package `cap-read.<domain>` grant lenses (later) may key on a relationship, not a vertex.** Out of
  scope here: this fix covers **vertex root-tombstone** anchors (the self-anchor case the milestone needs). A
  grant keyed off a *link* (e.g. landlord→unit ownership) retracts on a **link** tombstone — the existing
  **"Link-tombstone re-projection"** backlog row owns that, and the residence slice (D1 Increment 3) is itself
  post-milestone. Named so it's not a silent gap; not built here.

### 6.2 Alternatives considered

- **Alt A — complete the keys map in the *pipeline* from `Values` (don't touch the engine).** Works for the
  **upsert** (the row has every column) but **not** for the **delete** — on a tombstone there is no row, so the
  pipeline has nothing to complete from; only the engine can resolve `nanoIdFromKey(eventKey)`. Splitting the fix
  (pipeline for upsert, engine for delete) duplicates the key-column logic in two places and diverges from the
  simple engine's single-owner model. **Rejected** — one owner (the engine), mirroring the simple engine, is
  cleaner and covers both.
- **Alt B — a dedicated `GrantAnchorDeleteResult` hard-coded to `GrantKeyColumns`.** Solves only the grant case
  and bakes in the grant-specific 3 columns. **Rejected** — the root cause is *general* multi-column-key
  construction; the general fix (resolve all configured key columns) costs no more and serves any future
  multi-key full-engine lens, with the grant lens as the first consumer.
- **Alt C — make the `GrantWriterAdapter` read the missing key columns from the `row`/Values.** Papers over the
  symptom on the adapter for the upsert, still leaves the delete (no row) broken, and makes the adapter's "keys
  is authoritative" contract (`postgres.go:93`) inconsistent across adapters. **Rejected** — fix the engine, not
  the adapter.
- **Alt D — leave the producer "as-is, populated by a future package lens."** **Rejected** — the base
  self-anchor producer is exactly what the applicant-self milestone needs (D1 Increment 1's whole point); a
  broken producer means D1.3 enforcement deny-alls. The keystone must actually write grants.

---

## 7. Decomposition for the Steward (fire-by-fire)

Each fire is independently shippable + green. **Sequence Fire 1 first — it is the D1.3 producer the read boundary
depends on; the filed retraction (Fire 2) is moot until grants are actually written.**

### Fire 1 — the producer: full-engine composite key construction + live-verify

`full.CompiledRule.KeyColumns` + the `cmd/refractor` activation thread (§2.1) + the `applyReturn` multi-column key
build (§2.2) + activation-time key-column validation (§2.5). **Verify the live populate fails today, then passes
(§5.1).** Engine unit + the missing pipeline→`GrantWriterAdapter` integration test (§5.2-5.4). This makes the
live `capabilityReadGrants` lens populate `actor_read_grants` — **the D1.3 Fire-3 unblock.** *Full 3-layer
(security-plane: the grant source RLS trusts).* Independently shippable.

### Fire 2 — the retraction: composite-keyed anchor-tombstone delete (the filed row)

Generalize `AnchorDeleteResult` to resolve every key column read-free against the tombstoned anchor (§2.3) +
the read-free evaluator seam + engine/pipeline/integration tests (§5.5-5.7) + the `refractor.md` note (§3). On an
identity tombstone the self-grant is now `RevokeGrant`'d (seq-guarded soft-tombstone). *Full 3-layer.*
Independently shippable.

### Fire 3 — security e2e (optional but recommended)

The ephemeral-stack proof that a revoked-by-tombstone grant no longer passes RLS for its anchor (§5 Fire 3),
composed with the D1.3 read boundary. Lands from this lane or Verticals depending on where the live protected
read model sits when it's written.

---

## 8. Open questions — resolved (decide-don't-defer)

1. **Engine-owned multi-column key (not pipeline-completion, not a grant-specific path).** *Resolved* — §6.1
   (Alt A/B). One owner, mirroring the simple engine; serves any future multi-key full-engine lens.
2. **Key columns travel on `CompiledRule.KeyColumns`, set at activation from `Into.Key`** — no `ExecuteWith`
   signature churn. *Resolved* (§2.1).
3. **The delete evaluates key exprs read-free** (reusing `evalExpr` with a nil-coreKV guard); aspect-keyed
   columns fall through conservatively. *Resolved* (§2.3).
4. **Activation fails closed on a key column absent from RETURN** (simple-engine parity). *Resolved* (§2.5).
5. **The revoke seq is the tombstone CDC message's stream seq** (already stamped by `writeResults`) — no new
   state. *Resolved* (§2.4).
6. **Fire 1 (producer) before Fire 2 (retraction)** — retraction is moot until grants are written; Fire 1 is the
   D1.3 unblock. *Resolved* (§7).
7. **Link-keyed package grant lenses (landlord/residence) are out of scope** — they retract on a *link*
   tombstone (the existing Link-tombstone re-projection row), and are post-milestone. *Resolved* (R4).

---

## 9. Self-adversarial pass (folded in)

- *"Is the producer actually broken, or did you assert it like the last bounce?"* → Grounded the code, not the
  claim: `applyReturn` puts only `r.Items[0]` in the key map; the `Pipeline` struct holds no key columns;
  `cmd/refractor` passes `Into.Key` only to `simple.Compile` + adapter constructors, never to the full engine;
  `GrantWriterAdapter.Upsert`/`grantKeyFields` and `PostgresAdapter.buildUpsertSQL` both **error** on an absent
  key column and do not read the row. The two green proofs (`capability_read_grants_lens_contract_test.go`
  asserts `r.Values`; `read_path_adapters_test.go:180` hands the adapter a manual 3-column map) skip the joining
  path. The conclusion is code-derived — and Fire 1 step 1 still *verifies it live first*, so a wrong premise
  costs nothing.
- *"Could the multi-column upsert change break a live lens?"* → No: gated on `KeyColumns` non-empty; single-key
  lenses get the same key; actor-aggregate lenses discard the key map via the envelope. Regression-pinned.
- *"Could the read-free delete delete the wrong grant?"* → No: gated on `IsDeleted` + `eventType==anchorLabel` +
  **every** key column resolving read-free; any miss → `ok=false` → no Delete. A false-delete is structurally
  impossible.
- *"Does deleting/revoking a grant row violate P2?"* → No: `actor_read_grants` is the lens's own target;
  upsert/revoke of it is the Refractor's sanctioned projection write (same class as the cap-KV delete and the
  simple-engine plain-lens delete). No Core-KV write, no op.
- *"Does the §6.14 seq-guard hold under retraction?"* → Yes: the revoke carries the tombstone message's seq
  (> the grant's upsert seq), so the guard accepts it and retains the watermark; a stale re-upsert can't
  resurrect (§2.4).
- *"Is this really S–M, or did the producer finding balloon it?"* → Still S–M. Fire 1 is a one-field struct
  addition + a ~10-line `applyReturn` branch + a one-line activation thread + tests. Fire 2 is generalizing one
  existing engine method + a read-free evaluator guard + tests. No new subsystem, no contract, no orchestration.

---

*Designer: Winston (lattice-designer) · 2026-06-29 · grounds: the engine (`full/executor.go:1006-1023`
applyReturn, `:467` fetchNode, `:1029` evalExpr, `:1249` nanoIdFromKey; `full/anchor_delete.go` AnchorDeleteResult;
`full/ast.go:242` CompiledRule), the simple-engine precedent (`simple/evaluator.go:313` deleteResult,
`simple/compiler.go:14,91` Compile+key validation), the adapters (`adapter/read_path_adapters.go:35-76`
grantKeyFields/Upsert/Delete, `adapter/rls.go:236-270` Upsert/RevokeGrant seq-guard, `adapter/postgres.go:93-101`
keys-authoritative), the wiring (`cmd/refractor/main.go:247,261,297,325` full-engine activation;
`internal/refractor/lens/corekv_source.go:449-456` grant-table key default; the key-less `Pipeline` struct), the
bootstrap grant lens (`internal/bootstrap/lenses.go:290` CapabilityReadGrantsLensDefinition + its own
RETRACTION-scope comment), the green-but-non-joining tests
(`capability_read_grants_lens_contract_test.go`, `read_path_adapters_test.go:180`), Contract #6 §6.14
(`docs/contracts/06-capability-kv.md:550-566`), the ratified D1.3 keystone design
(`d1-grant-population-and-sequencing-design.md` §3.1's flagged retraction correction), and the `679fe25`
anchor-tombstone-retraction design.*
