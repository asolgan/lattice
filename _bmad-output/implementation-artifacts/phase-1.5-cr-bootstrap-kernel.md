# Bootstrap/Kernel CR Report — Phase 1.5

## Summary

- Files reviewed: 7 (`internal/bootstrap/primordial.go`, `internal/bootstrap/meta_ddl.go`, `internal/bootstrap/envelope.go`, `internal/bootstrap/verify.go`, `internal/bootstrap/lenses.go`, `internal/bootstrap/nanoid.go`, `cmd/bootstrap/main.go`, `scripts/verify-kernel.go`)
- P0 findings: 0
- P1 findings: 2
- P2 findings: 7
- Nit findings: 4
- History comments: 27 (across all 7 files)

---

## P0 Findings

None.

---

## P1 Findings

### [F-001] Crash-between-batch-and-persist creates duplicate primordial set on next boot

**File:** `cmd/bootstrap/main.go:82-92` / `internal/bootstrap/nanoid.go:157-171` / `internal/bootstrap/primordial.go:208-215`

**What:** If the process crashes (SIGKILL, OOM, disk-full on persist) after `SeedPrimordial` commits its atomic batch but before `Persist()` writes `lattice.bootstrap.json`, the next boot enters a silent corruption path:

1. JSON does not exist → `LoadOrGenerate` generates **new** NanoIDs (set B).
2. The idempotency guard in `SeedPrimordial` probes `BootstrapOpKey(B)` — a key derived from the newly-generated IDs, not from the prior run's IDs. Core KV holds `BootstrapOpKey(A)`, which is an entirely different key.
3. Guard misses. The atomic batch with `CreateOnly=true` is submitted with IDs B. Because B's NanoIDs are different from A's, all 69 keys are new; the batch succeeds.
4. Core KV now contains **two complete, internally consistent but mutually unaware primordial sets** (set A and set B). Set A is permanently orphaned — no GC, no recovery path.
5. `lattice.bootstrap.json` records set B. All subsequent binaries (Processor, Refractor, verify-kernel) use set B and never detect set A.

The idempotency comment in `SeedPrimordial` (`"if the op tracker already exists, the primordial set has previously committed"`) is false for this scenario because the op tracker key is ID-derived, not constant.

**Why it matters:** Single-node deployments are most vulnerable: a crash during first boot (SIGTERM from Docker, OOM during the large batch) silently populates Core KV with double the expected kernel state. This wastes storage, confuses `nats kv ls`, and poisons any future tooling that expects exactly one primordial set. The situation is unrecoverable without `make down && make up`.

**Suggested fix:** One of:

1. Write a **constant sentinel key** (e.g., `meta.bootstrap.in-progress`) to Core KV — with `CreateOnly=true` — **before** generating NanoIDs, holding the generated IDs in the sentinel value. On the next boot, if the sentinel exists, read the IDs from it rather than generating fresh ones. After `Persist()` succeeds, the sentinel can remain (harmless) or be deleted.
2. Alternatively, write the JSON file **before** calling `SeedPrimordial`, with a `"status": "in-progress"` field. On load, detect this status and reuse the same IDs rather than regenerating. After successful seeding, rewrite the file with `"status": "committed"`.

Either approach ensures that the NanoIDs are stable across restarts regardless of where the crash occurred.

---

### [F-002] `UpdateMetaVertex` can only mutate `description` and `compensation` — `script`, `permittedCommands`, and all other aspects are permanently immutable after creation

**File:** `internal/bootstrap/meta_ddl.go:213-252`

**What:** The `UpdateMetaVertex` branch of `MetaRootDDLScript` issues exactly two mutations:

```starlark
make_update(meta_key + ".description", {"text": desc}),
make_update(meta_key + ".compensation", {...}),
```

There is no mechanism to update `script`, `permittedCommands`, `inputSchema`, `outputSchema`, `fieldDescription`, or `examples` via the `ops.meta` lane. Once a DDL or Lens is created via `CreateMetaVertex`, its Starlark script is frozen — operators cannot fix bugs in it, change permitted commands, or update input/output schemas without deleting and recreating the entire meta-vertex (which invalidates any existing clients that hold the prior `metaKey`).

**Why it matters:** This is a contract violation. The `UpdateMetaVertex` operation name implies it is the canonical way to mutate kernel meta-vertices at runtime (per Story 4.7 and the kernel meta-DDL design), but it only exposes a single updatable field. Any production DDL bug requires a `TombstoneMetaVertex` + `CreateMetaVertex` cycle, which changes the `metaKey` and breaks all callers that referenced the old key. This is not documented as a deliberate constraint in the script, the `meta_ddl.go` doc block, or the `permittedCommands` aspect.

**Suggested fix:** Either:

1. Extend `UpdateMetaVertex` to accept the full set of updatable fields (`script`, `permittedCommands`, `inputSchema`, `outputSchema`, `fieldDescription`, `examples`) as optional payload fields, updating only those present; or
2. Document explicitly (in the `MetaRootDDLScript` doc comment and in the `description` aspect written to Core KV) that `UpdateMetaVertex` is intentionally restricted to `description` only, and that script changes require tombstone+recreate, and capture the `metaKey` stability implication.

Option 2 is the minimum to prevent this from being silently misunderstood; option 1 is the correct fix.

---

## P2 Findings

### [F-003] `verify.go` and `verify-kernel.go` only perform envelope validation on the 18 top-level vertex keys — the ~51 aspect keys are existence-checked only

**File:** `internal/bootstrap/verify.go:65-115` / `scripts/verify-kernel.go:118-202`

**What:** The `VerifyKernel` function and the standalone script perform full envelope field checks (class, isDeleted, createdBy, key echo, etc.) only for the keys returned by `PrimordialVertexKeys()` (18 entries). For all aspect keys — the meta-DDL's 9 aspects, each lens's 5 aspects, each aspect-type vertex's 6 aspects, the role's 2 aspects — the checks only assert that `coreKV.Get` returns without error. A silently corrupted aspect (wrong `class`, wrong `vertexKey`, wrong `data` structure, truncated JSON) would pass verification.

Specifically: the `vertexKey` field in an `AspectEnvelope` (linking the aspect back to its parent vertex) is never checked. If a bug writes an aspect with the wrong `vertexKey`, the Refractor's graph traversal and any DDL cache lookups that read aspect data would receive wrong results — and verify would report all-OK.

**Why it matters:** The verify pass is the post-bootstrap regression gate. If it does not catch envelope-shape corruption in aspect keys, partial bootstrap failures (e.g., a data race that wrote an aspect before the vertex) or a future refactor bug would be invisible to CI and to `make verify-kernel`.

**Suggested fix:** Extend both verification paths to unmarshal and validate aspect key envelopes, at minimum checking:
- JSON is valid
- `key` field echoes the expected key
- `class` field matches the expected aspect class (e.g., `canonicalName`, `script`, `lensSpec`)
- `isDeleted` is `false`
- `vertexKey` matches the parent vertex key (for `AspectEnvelope`)

---

### [F-004] `addLensAspects` silently discards the `ok` return from `strings.Cut` — malformed lens spec `id` field if `lensKey` lacks the `vtx.meta.` prefix

**File:** `internal/bootstrap/primordial.go:709-711`

**What:**
```go
_, lensID, ok := strings.Cut(lensKey, "vtx.meta.")
_ = ok
specBody, err := makeLensSpecBody(lensID, def)
```

If `lensKey` does not contain `"vtx.meta."` (e.g., if called with a zero-value key because `populate()` was not called before `buildPrimordialEntries`), `strings.Cut` returns `(lensKey, "", false)`. The suffix `lensID` is then the empty string `""`. The lens spec body emitted to Core KV would have `"id": ""`, causing Refractor's `CoreKVSource` to load a lens with a blank ID — silently, with no error.

Currently the callers use `CapabilityLensKey` and `CapabilityRoleIndexLensKey` which are always populated by `populate()` before `buildPrimordialEntries` is called. But this is a calling-convention assumption that is not enforced at the call site.

**Why it matters:** A future caller that passes a lens key without the `vtx.meta.` prefix, or that accidentally invokes `buildPrimordialEntries` before `populate()`, will produce a silently broken lens spec. The Refractor would load a lens with `id: ""`, potentially colliding with any other zero-ID lens.

**Suggested fix:** Replace `_ = ok` with an explicit error check:
```go
_, lensID, ok := strings.Cut(lensKey, "vtx.meta.")
if !ok {
    return fmt.Errorf("addLensAspects: lensKey %q does not have expected vtx.meta. prefix", lensKey)
}
```

---

### [F-005] `Load()` does not validate `BootstrapFile.Version` — stale or downgraded bootstrap JSON gives a cryptic error rather than a clear version mismatch

**File:** `internal/bootstrap/nanoid.go:203-213`

**What:** `Load()` unmarshals the JSON and calls `populate()`, which validates each NanoID for Contract #1 compliance. If a `lattice.bootstrap.json` from a previous schema version (e.g., version `"1"` or `"2"`) is present and lacks one of the newer fields (e.g., `aspectTypeDescription` added in Story 5.1), the field is silently zero-valued by `encoding/json`. Then `populate()` fails with:
```
primordial ID "aspectTypeDescription" is not Contract #1-compliant: ""
```
instead of:
```
bootstrap file version mismatch: got "1", want "3" — run `make down && make up`
```

**Why it matters:** When an operator upgrades Lattice but forgets to run `make down` first, the error message does not tell them what to do. The suggestion in `verify-kernel.go` ("run `make down && make up`") is buried behind the confusing NanoID error.

**Suggested fix:** Add a version check at the top of `Load()`:
```go
if f.Version != "3" {
    return fmt.Errorf("bootstrap file version mismatch: got %q, want \"3\" — run `make down && make up`", f.Version)
}
```

---

### [F-006] `OpsMetaStreamName` and `OpsMetaSubject` are exported constants that are never used in production code — dead code with misleading name

**File:** `internal/bootstrap/primordial.go:32` and `primordial.go:41`

**What:** `OpsMetaStreamName = "ops-meta"` is defined but never passed to `provisionStreams`. `OpsMetaSubject = "ops.meta.>"` is also exported but never referenced outside this file. The `provisionStreams` function creates only `core-operations` (which already captures `ops.>` including `ops.meta.>`) and `core-events`. There is no separate `ops-meta` JetStream stream.

The comment on `OpsMetaStreamName` ("Provision core-operations and ops.meta streams" at line 108) implies a stream named `ops-meta` should be provisioned — but it is not, and should not be, because `core-operations` already covers the `ops.meta.>` subject space per Contract #2 §2.3. The constant exists as a misleading artifact.

**Why it matters:** Any developer who reads the constant name `OpsMetaStreamName` and `provisionStreams` would expect a stream named `ops-meta` to exist. If they try to verify or interact with it (`nats stream info ops-meta`), they find nothing. If they try to add it to `provisionStreams` "to fix the discrepancy," they would create a double-binding subject conflict with `core-operations`.

**Suggested fix:** Remove `OpsMetaStreamName` and `OpsMetaSubject` entirely. The comment at line 143 already correctly documents that `OpsWildcardSubject` covers the meta lane. Update the `provisionStreams` comment at line 108 to read "Provision core-operations and core-events streams."

---

### [F-007] `TombstoneMetaVertex` tombstones only the vertex key — all aspect keys remain live in Core KV as permanently orphaned entries

**File:** `internal/bootstrap/meta_ddl.go:254-278`

**What:** The `TombstoneMetaVertex` branch produces only two mutations:
```starlark
make_tombstone(meta_key),                          # sets meta_key.isDeleted = true
make_update(meta_key + ".compensation", {...}),    # marks compensation as irreversible
```

It does not tombstone the other aspects: `meta_key + ".description"`, `meta_key + ".canonicalName"`, `meta_key + ".script"`, `meta_key + ".permittedCommands"`, `meta_key + ".inputSchema"`, `meta_key + ".outputSchema"`, `meta_key + ".fieldDescription"`, `meta_key + ".examples"`. For a Lens vertex, `meta_key + ".spec"` and `meta_key + ".targetBucket"` and `meta_key + ".cypherRule"` are also left alive.

After tombstone, the Processor's `vertex_alive(state, meta_key)` check correctly blocks further `UpdateMetaVertex` and `TombstoneMetaVertex` operations on the vertex. But the aspect keys are live indefinitely with no GC path.

**Why it matters:** This is a partial-deletion contract gap. The M5/M6 DDL cache invalidation problem (documented as a known gap) is partly rooted here: if `TombstoneMetaVertex` did tombstone all aspects, the Processor's watch on Core KV revisions would at least have a complete signal to evict the entry. With aspects left live, a cache or watch implementation that walks aspects would see a vertex where `isDeleted=true` on the root but all content aspects are still `isDeleted=false` — an inconsistent state that is not explicitly documented as intentional.

**Suggested fix:** Either:
1. Add cascade tombstone mutations for all known aspect keys in the `TombstoneMetaVertex` branch. For DDL classes this is a fixed set; for `meta.lens` a slightly different set.
2. Explicitly document in the `MetaRootDDLScript` doc block that Phase 1 tombstone is vertex-only, aspects are left live for auditability, and that a future cleanup mechanism is required. Add this note to the `compensation` aspect's note field to signal it to future developers.

---

### [F-008] `looksLikeCreateConflict` duplicates `substrate.isRevisionConflict` without using the `jetstream.ErrKeyExists` sentinel — fragile string matching

**File:** `internal/bootstrap/primordial.go:276-281` and `primordial.go:263-265`

**What:** `looksLikeCreateConflict` and the inline check in `seedPrimordialPerKey` both use `strings.Contains` matching against NATS error message text:
```go
strings.Contains(s, "wrong last sequence") ||
strings.Contains(s, "key exists") ||
strings.Contains(s, "10071")
```

The substrate package already has `isRevisionConflict()` in `internal/substrate/kv.go:168-181` which additionally checks `errors.Is(err, jetstream.ErrKeyExists)` — the typed sentinel — before falling back to string matching. The bootstrap code bypasses the typed check entirely.

If NATS changes the error message text between versions (which has happened historically), the fallback path in `SeedPrimordialPerKey` would stop recognizing concurrent-create conflicts and would return a hard error instead of skipping the already-existing key.

**Why it matters:** The per-key fallback is the crash-recovery path. If it misidentifies a concurrent-create conflict as a fatal error, a competing bootstrap process or a retry-after-partial-success will fail hard rather than completing gracefully.

**Suggested fix:** Export `substrate.IsCreateConflict` (or reuse the existing `isRevisionConflict` since `ErrKeyExists` is what `kv.Create` returns on conflict) and call it from bootstrap. At minimum, add the `errors.Is(err, jetstream.ErrKeyExists)` check to `looksLikeCreateConflict`:
```go
func looksLikeCreateConflict(err error) bool {
    if errors.Is(err, jetstream.ErrKeyExists) {
        return true
    }
    s := err.Error()
    return strings.Contains(s, "wrong last sequence") ||
        strings.Contains(s, "key exists") ||
        strings.Contains(s, "10071")
}
```

---

## Nit Findings

### [N-001] `SeedPrimordial` doc comment states "Total ≈ 34 Core KV entries" — actual count is 69

**File:** `internal/bootstrap/primordial.go:185`

The comment was accurate before Stories 5.1 and 5.3 added self-description aspects and compensation aspects. The actual seeded count is 69 (1 op tracker + 1 admin identity + 11 meta-root keys + 12 lens keys + 40 aspect-type-meta keys + 3 role keys + 3 permission vertices + 4 link keys). `scripts/verify-kernel.go:23` correctly says "Total ≈ 69 OK lines" — the primordial.go doc comment contradicts it.

**Fix:** Update the doc comment block (lines 185-186) to say "Total ≈ 69 Core KV entries" and update the kernel composition enumeration above it to include the aspect-type vertices and their aspects.

---

### [N-002] Comment at `primordial.go:108` says "Provision core-operations and ops.meta streams" — `ops.meta` is never provisioned as a separate stream

**File:** `internal/bootstrap/primordial.go:108`

`provisionStreams` creates `core-operations` (which covers `ops.>`) and `core-events`. There is no `ops.meta` stream. The comment is misleading — it implies a stream called `ops-meta` or `ops.meta` should exist, which it does not and should not (per Contract #2 §2.3, `ops.>` covers all lanes).

**Fix:** Change the comment to "Provision core-operations and core-events streams."

---

### [N-003] `WaitForBootstrapComplete` polls with a `time.Ticker` — first check is delayed 500ms even when the key is already present

**File:** `internal/bootstrap/primordial.go:809-810`

Since `MarkBootstrapComplete` is called immediately before `WaitForBootstrapComplete` in `cmd/bootstrap/main.go`, the key is always present when the wait loop starts. The ticker's first tick fires 500ms later, meaning every bootstrap run has an unnecessary 500ms floor latency on the readiness gate.

**Fix:** Either use `time.After` with a zero-duration initial check, or restructure to check once immediately before starting the poll loop.

---

### [N-004] `AtomicBatch` in `SeedPrimordial` uses a hardcoded 30s timeout and ignores the caller's `context`

**File:** `internal/bootstrap/primordial.go:237`

```go
ack, err := conn.AtomicBatch(ops, 30*time.Second)
```

`substrate.Conn.AtomicBatch` does not accept a context. The 30s timeout is hardcoded and cannot be shortened by the caller's `ctx`. If `cmd/bootstrap` is sent SIGTERM during the batch, the process does not exit cleanly until the batch times out (up to 30s). The `ctx := context.Background()` passed to `SeedPrimordial` also carries no deadline, so there is no defense-in-depth timeout on the overall seeding operation.

**Fix:** This is primarily a substrate-layer limitation (no context on AtomicBatch). Minimal fix here: make the timeout configurable via a `Seeder` option or an environment variable so that test environments can shorten it. Document the 30s value and the lack of cancellation in the function's doc comment.

---

## History Comments

The table below lists every comment that records change history (Story references, prior-behavior narration) rather than documenting current state. All are Nit severity except where flagged P2.

| File:Line | Comment excerpt | Severity | Suggested action |
|---|---|---|---|
| `primordial.go:1` | `// Package bootstrap implements the primordial seeding sequence for Story 1.3.` | Nit | Remove story reference; state purpose only |
| `primordial.go:28` | `// Story 2.1: Refractor's internal adjacency store...` | Nit | Remove story reference from const comment |
| `primordial.go:37-38` | `// Story 1.5's CONTRACT-AMENDMENT-REQUEST flagged the old single-segment...this resolves it.` | P2 (actively misleading — implies a prior bug was fixed that readers need context for) | Replace with current-state explanation of why `ops.>` is the correct pattern |
| `primordial.go:42` | `// Story 1.8: Processor step-9 event fan-out` | Nit | Remove story reference |
| `primordial.go:87` | `// Story 1.1 spike finding (nats-batch README).` | Nit | Remove story reference |
| `primordial.go:99` | `// Per Story 1.1 spike: CreateKeyValue does NOT set this automatically.` | Nit | Remove story reference; keep the factual content |
| `primordial.go:146` | `// Story 1.8: Processor step-9 event fan-out.` | Nit | Remove story reference |
| `primordial.go:195-200` | `// Story 1.4 refactor: this method now uses substrate.AtomicBatch...replacing the prior sequential-create loop.` | Nit | Remove "prior" narration; document current behavior only |
| `primordial.go:350` | `// Story 5.1: 4 additional self-description aspects for the root DDL.` | Nit | Remove story reference |
| `primordial.go:402` | `// 4a. Story 5.3: seed the .compensation aspect...` | Nit | Remove story reference |
| `primordial.go:438` | `// 6a. Story 5.1: five aspect-type meta-vertices...` | Nit | Remove story reference |
| `primordial.go:473` | `// once Story 5.3's ops-routed installer arrives.` | Nit | Remove forward story reference; state what the constraint is today |
| `primordial.go:522` | `// seedAspectTypeMeta seeds the five aspect-type meta-vertices for Story 5.1.` | Nit | Remove story reference |
| `primordial.go:678` | `// Story 3.2a: in addition to the four human-readable aspects...` | Nit | Remove story reference |
| `primordial.go:706` | `// activation watch consumes (Story 3.2a Phase D).` | Nit | Remove story reference |
| `primordial.go:745` | `// (Story 3.2b verifies the capabilityRoleIndex shape end-to-end).` | Nit | Remove story reference |
| `primordial.go:773-775` | `// Historically this was refractor-stub's job; after Story 2.1 deleted refractor-stub...Resolution: cmd/bootstrap now writes the marker itself` | P2 (describes why current code differs from deleted code — pure changelog) | Replace with "bootstrap writes this marker because it is the last step that can confirm primordial state is complete." |
| `envelope.go:12` | `// which matters for the bypass test oracle in Story 1.10.` | Nit | Remove story reference |
| `envelope.go:19-21` | `// Story 1.4 refactor: this function was the bespoke envelope formatter prior to the substrate package; it is now a 4-line adapter...` | Nit | Remove prior-behavior narration |
| `nanoid.go:27-29` | `// Story 4.7 trim: the identity-domain and rbac-domain NanoID surfaces...moved to their respective Capability Packages.` | Nit | Remove story reference |
| `nanoid.go:54` | `// .compensation sixth self-description aspect (Story 5.3).` | Nit | Remove story reference |
| `nanoid.go:82-85` | `// Story 4.7: meta-permission NanoIDs + keys. Three kernel-seeded permissions...once Story 5.3 routes installs through CreateMetaVertex ops` | Nit | Remove story references; state what they authorize |
| `nanoid.go:93-95` | `// Story 5.1: five aspect-type meta-vertex NanoIDs.` | Nit | Remove story reference |
| `nanoid.go:113-116` | `// Story 4.7 trim: identity-domain and rbac-domain NanoID fields are retired. Existing lattice.bootstrap.json files that include those extra fields parse fine...` | P2 (documents a migration concern that no longer applies once all deployments are on v3) | Remove after confirming all environments run v3; otherwise add a deadline/version note |
| `meta_ddl.go:3` | `// Story 4.7 — the kernel's sole DDL.` | Nit | Remove story reference |
| `meta_ddl.go:35-49` | `// Story 5.3 additions:...The rejected compensatingOperation OperationReply envelope field (originally proposed in epics.md §Story 5.3)...` | P2 (documents a rejected design alternative — pure changelog) | Remove the rejected-design narration; keep only the current behavior description |
| `meta_ddl.go` (inline Starlark) | `# Story 5.3: sixth self-description aspect`, `# Story 5.3: read prior description from state`, `# SC-1 (Story 5.3)`, `# MF-2 (Story 5.3)` | Nit | Remove all inline Starlark story annotations |
| `cmd/bootstrap/main.go:1` | `// cmd/bootstrap — Primordial bootstrap binary for Story 1.3.` | Nit | Remove story reference |
| `cmd/bootstrap/main.go:98-106` | `// Story 2.1b housekeeping: refractor-stub was deleted in Story 2.1 (MORPH-DEVIATIONS Deviation 9)...` | P2 (changelog entry explaining why the previous architecture was different) | Replace with: "bootstrap writes this marker itself because it is the only process guaranteed to run after primordial seeding completes." |

---

## Adversarial Coverage Notes

**Things looked for but not found:**

- **Concurrent bootstrap race (two processes, same cell):** The `CreateOnly` flag on the atomic batch plus the per-key fallback's `kv.Create` provide adequate protection. Two concurrent bootstraps will have the same NanoIDs (if both loaded from an existing JSON) or will race on independent IDs (which is the F-001 scenario above). No new race finding beyond F-001.

- **NATS partial provisioning (some KV buckets missing):** `CreateOrUpdateKeyValue` is idempotent. If NATS is partially provisioned, the next bootstrap run reprovisiones missing buckets without touching existing ones. No correctness issue found here.

- **Bootstrap JSON unreadable (permissions, truncation):** `os.ReadFile` error is wrapped and returned; `json.Unmarshal` error is wrapped and returned; `populate()` validates all NanoIDs. These all produce clear terminal errors. No gap found.

- **`context.Background()` non-cancellation for ProvisionBuckets:** Flagged as Nit (N-004). Not a correctness issue, only a graceful-shutdown quality issue.

- **Primordial bootstrap on a mid-tombstone cell:** Tombstoned kernel entries have `isDeleted=true` but still exist in Core KV. The `CreateOnly` batch would fail on those keys; the per-key fallback would skip them (they are not `ErrKeyNotFound`). This would result in the tombstoned keys remaining tombstoned and the rest of the primordial set being re-created with new IDs — the same structural scenario as F-001. Not a separate finding.

- **`verify-kernel.go` on a cell with extra unexpected keys:** The script does not enumerate all Core KV keys; it only asserts the presence of expected ones. Extra keys are silently ignored. This is the correct behavior for a forward-compatible assertion (packages install their own keys that the kernel verify pass should not know about). No finding.

- **DDL cache invalidation signals on `TombstoneMetaVertex`:** This is the documented M5/M6 known gap — the fault is in the Processor DDL cache (separate CR). From bootstrap's side, the tombstone mutation is emitted correctly via the meta lane; the Processor simply does not act on it. Flagged as context in F-007 but not a new bootstrap finding.
