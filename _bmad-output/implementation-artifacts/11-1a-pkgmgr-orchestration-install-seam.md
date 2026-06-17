# Story 11.1a: `pkgmgr` orchestration-content install seam

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a vertical author,
I want the package installer (`internal/pkgmgr`) to ship `meta.weaverTarget`, `meta.loomPattern`, and
per-op `operationType` op-meta content as first-class package data,
so that a real capability package (the `lease-signing` vertical of Story 11.1) can install Weaver
targets + Loom patterns via `InstallPackage` — not via test helpers — with **zero changes to the four
generic engines** (`internal/{loom,weaver,processor,refractor}`).

## Context & Scope (read first — this is the spine of the story)

Epic 11 (the Loftspace reference vertical) was originally two stories: **11.1** (author the
`lease-signing` package) → **11.2** (end-to-end convergence harness). While drafting 11.1 a **BLOCKING
gap** surfaced — the package installer has no path to ship orchestration content — so Epic 11 is now
**three stories: 11.1a (THIS — the installer seam) → 11.1 (lease-signing authoring, BLOCKED on 11.1a)
→ 11.2 (e2e harness)**.

**This story is INSTALLER infrastructure only.** It teaches `pkgmgr.Definition` /
`buildInstallBatch` to emit three new meta-vertex kinds that the Weaver and Loom CDC registries already
know how to load. It ships **no `packages/` content** (that is 11.1's job) — only the seam, its
validation, the manifest schema extension, and unit tests.

**Why this is not an engine change.** `internal/pkgmgr` is the package *installer*, not one of the four
generic engines. The engines already consume `meta.weaverTarget` / `meta.loomPattern` / op-meta
vertices via their CDC registries (`internal/weaver/registry.go`, `internal/loom/source.go`). What is
missing is purely an **authoring seam** on the install side: the installer can emit DDLs, Lenses,
Permissions, Roles — but nothing for orchestration content. This story closes that gap on the installer
side, building to the **already-frozen** on-wire shapes the engines parse.

> The 11.1 story file (`_bmad-output/implementation-artifacts/11-1-lease-signing-package-authoring.md`)
> parked exactly three Open Questions that 11.1a resolves: **#1** (the meta-vertex authoring seam),
> **#2** (`assignTask` `forOperation` op-meta discoverability), **#3** (row-per-candidate lens output
> key). 11.1a must unblock precisely what 11.1 needs — read that file for downstream context.

### HARD GUARDRAIL — STOP-and-escalate on any engine edit

The four engines — `internal/loom`, `internal/weaver`, `internal/processor`, `internal/refractor` —
are generic and frozen for this story. **If the implementation ever appears to require an edit under
any of those four directories, that is a STOP-and-escalate signal — do NOT plan or make the edit.**
Record it as an Open Question and surface it to Winston. The whole point of 11.1a is that the engines
*already* parse these shapes; the seam builds to them. An apparent need to touch an engine means the
seam's emitted shape does not match what the engine parses — fix the *emitted shape*, never the parser.

> The seam lives **entirely in `internal/pkgmgr`** (`definition.go`, `build.go`, `manifest.go`, and
> their `_test.go`). No other source tree should change. `packages/` content is 11.1's job, not this
> story's.

## The verified gap (confirmed during authoring — build to it)

1. **`internal/pkgmgr/definition.go`** — `type Definition struct` carries `DDLs []DDLSpec`,
   `Lenses []LensSpec`, `Permissions []PermissionSpec`, `Roles []RoleSpec`. There is **NOTHING** for
   weaver targets, loom patterns, or per-op op-meta.
2. **`internal/pkgmgr/build.go`** — `buildInstallBatch` ranges only over Roles (~line 47),
   DDLs (~lines 60/76), Lenses (~line 115), Permissions (~line 148). The **Lens emit path** (~line 115,
   helper `lensSpecBody` at ~line 220) is the **closest template**: it writes a `vtx.meta.<NanoID>`
   vertex + a `.spec` aspect whose `data` is the spec body. Mirror it.
3. **No package has ever shipped a `meta.weaverTarget` / `meta.loomPattern`.** The proven on-wire
   shapes live ONLY in test helpers `installWeaverTarget` / `installLoomPattern` / `installOpMeta` in
   `internal/weaver/weaver_e2e_test.go` (lines 85–127). The parse targets are `Target` / `GapAction`
   in `internal/weaver/registry.go` and `Pattern` / `Step` in `internal/loom/pattern.go`.

### The canonical on-wire shapes (these are the spec — match them exactly)

From `internal/weaver/weaver_e2e_test.go` (the fixture writes exactly what the engines parse in
production), and from the CDC loaders that unwrap them:

**`meta.weaverTarget`** (fixture `installWeaverTarget`, lines 85–95):
- vertex envelope: `{"class": "meta.weaverTarget", "data": {}}` at `vtx.meta.<id>`
- spec aspect envelope: `{"class": "weaverTargetSpec", "data": <targetBody>}` at `vtx.meta.<id>.spec`
- `<targetBody>` deserializes into `registry.Target` = `{targetId, lensRef, gaps: {<col>: GapAction}}`.
  `GapAction` = `{action, pattern?, subject?, adapter?, operation?, assignee?, target?, params?}`.

**`meta.loomPattern`** (fixture `installLoomPattern`, lines 102–116):
- vertex envelope: `{"class": "meta.loomPattern", "data": {}}` at `vtx.meta.<id>`
- spec aspect envelope: `{"class": "loomPatternSpec", "data": <patternBody>}` at `vtx.meta.<id>.spec`
- `<patternBody>` deserializes into `loom.Pattern` =
  `{patternId, subjectType, completionDomains?, steps: [{kind, operation, guard?}]}`.

**op-meta vertex** (fixture `installOpMeta`, lines 118–127):
- vertex envelope: `{"class": "meta.ddl.vertexType", "data": {"operationType": "<op>"}}` at
  `vtx.meta.<id>`. **No spec aspect.** Both registries (`registry.go indexOpMeta`,
  `source.go indexOpMeta`) key `operationType → vtx.meta.<id>` off `data.operationType` on a
  *non-routed* meta-vertex.

**The `.spec` unwrap is the load-bearing detail.** Both loaders unwrap the spec body from the aspect
envelope's `data` field:
- `internal/weaver/registry.go` `unwrapSpecBody(body, sentinelField)` (line 606): if the body has the
  `sentinelField` ("gaps" for targets) at top level it is bare; otherwise it returns the `data`
  sub-object. The Processor write path produces `{class, isDeleted, data, vertexKey, localName}` — so
  the body the engine sees is the aspect envelope, and `unwrapSpecBody` pulls `.data`.
- `internal/loom/source.go` `unwrapPatternBody(body)` (line 333): same, sentinel `"steps"`.
- **This is exactly the same envelope the existing Lens `.spec` aspect uses** (`build.go docAspect`
  writes `{class, isDeleted, data, vertexKey, localName}`). So emitting a `.spec` aspect via the
  existing `docAspect` helper with the spec body under `data` is **provably CDC-loadable** — that is
  how Refractor already loads the Lens `spec` aspect.

## Acceptance Criteria

Derived from the brief + the verified code gap (NOT from the epics planning artifact, which is
planning-lead-owned and still lists Epic 11 as two stories). Each AC maps to a task below.

**AC1 — `Definition` fields + spec types (Task 1).** `pkgmgr.Definition` gains
`WeaverTargets []WeaverTargetSpec`, `LoomPatterns []LoomPatternSpec`, and `OpMetas []OpMetaSpec`. The
spec structs carry exactly what the runtime parsers expect:
- `WeaverTargetSpec` — `TargetID`, `LensRef` (the **canonicalName** of the target lens, resolved to its
  in-batch NanoID at install; see AC3), and `Gaps map[string]GapActionSpec`. `GapActionSpec` mirrors
  `registry.GapAction` field-for-field (`Action, Pattern, Subject, Adapter, Operation, Assignee,
  Target, Params map[string]string`) so the emitted body deserializes cleanly into `registry.Target`.
- `LoomPatternSpec` — `PatternID`, `SubjectType`, `CompletionDomains []string`, `Steps []StepSpec`
  where `StepSpec` = `{Kind, Operation, Guard}` (`Guard` carried as a raw/`map[string]any` so the
  §10.5 declarative shape round-trips into `loom.Step.Guard json.RawMessage`).
- `OpMetaSpec` — `OperationType` (the single field the op-meta vertex's `data.operationType` carries).

**AC2 — Emit logic in `buildInstallBatch` (Task 2).** For each spec the installer emits the canonical
shapes above, modeled on the Lens emit path:
- WeaverTarget → `vtx.meta.<NanoID>` vertex (class `meta.weaverTarget`, empty `data`) + a `.spec`
  aspect (class `weaverTargetSpec`, body under `data`).
- LoomPattern → `vtx.meta.<NanoID>` vertex (class `meta.loomPattern`, empty `data`) + a `.spec` aspect
  (class `loomPatternSpec`, body under `data`).
- OpMeta → `vtx.meta.<NanoID>` vertex (class `meta.ddl.vertexType`, `data: {operationType: <op>}`).
  **No spec aspect.**
- Every emitted key is appended to `declared` (the `declaredKeys` snapshot) so `Uninstall` reclaims it.
- NanoIDs are minted via the existing `deterministicNanoID(name, version, tag)` convention (installer.go
  line 213) with new tags (`"weaverTarget:"+targetId`, `"loomPattern:"+patternId`, `"opMeta:"+op`) so
  re-install is idempotent and produces identical keys.

**AC3 — Cross-reference resolution within the install batch (Task 3).** A `WeaverTargetSpec.LensRef`
authored as a lens **canonicalName** is resolved to that lens's in-batch deterministic NanoID before
the target body is emitted. (The package author writes a name; the engine's control surface expects an
id.) Resolution rules — confirm and implement each:
- **`lensRef` → by-NanoID.** §10.8 defines `lensRef` as "the meta.lens id of the violation Lens." In a
  package the author writes the lens's `CanonicalName`; the installer resolves it to the NanoID minted
  for that lens **in the same batch** (`deterministicNanoID(name, version, "lens:"+canonicalName)`).
  A `LensRef` that matches no declared lens is a **fail-closed install error** (pure validation, before
  any KV write). **Note (do not over-engineer):** `lensRef` is parsed and surfaced on Weaver's control
  API (`internal/weaver/control.go`) but is **NOT** used for lane-1 dispatch — Weaver watches
  `weaver-targets` under `<targetId>.>` directly. So `lensRef` resolution is correctness/control-surface
  hygiene, not a dispatch dependency. Resolve it; do not build machinery beyond name→NanoID lookup.
- **`gaps[col].pattern` → by-id-STRING (no resolution).** A `triggerLoom` playbook entry references a
  pattern by its `patternId` string (e.g. `pattern: "onboarding"`). Weaver resolves this **at runtime**
  via `registry.go patternMetaKey` (which indexes both the `patternId` and the vertex NanoID). The
  installer does **NOT** rewrite `pattern` to a NanoID — it ships the string verbatim. (Confirm against
  `registry.go indexPattern`, lines 463–486: `patternId` is indexed, so the string resolves live.)
- **`gaps[col].operation` (assignTask/nudge/directOp) → by operationType STRING (no resolution).** These
  reference an op by its `operationType`, resolved at runtime via `opMetaKey` — see AC4. Ship verbatim.

**AC4 — Op-meta discoverability for `assignTask` `forOperation` (Task 4).** `assignTask` resolves
`forOperation` from `registry.go indexOpMeta`, which keys on a `vtx.meta.*` vertex's
`data.operationType` (NOT on a DDL's `permittedCommands` sibling aspect). Confirmed:
`internal/weaver/strategist.go` line 153, `source.opMetaKey(operation)` returns an error when no op-meta
vertex exists, so an `assignTask` op (e.g. `SignLease`) MUST have a discoverable op-meta vertex. But
`buildInstallBatch` writes a DDL meta-vertex with **EMPTY `data`** and puts `permittedCommands` in a
sibling aspect — so an installed DDL is **not** discoverable by `operationType`. **Resolved
PACKAGE-SIDE** via the `OpMetaSpec` emit path (AC1/AC2): a package declares one `OpMetaSpec` per op that
must be `forOperation`-resolvable, and the installer emits a `vtx.meta.<NanoID>` op-meta vertex carrying
`data.operationType` (the `installOpMeta` shape). **No engine change** (option (a) of 11.1 Open Q #2;
options (b)/(c) are rejected — (b) is an engine change = STOP, (c) is brittle).

**AC5 — Install validation / guardrails (Task 5).** Pure validation functions (no I/O), run BEFORE any
KV write, mirroring `bucketguard.go validateLensBuckets`:
- **`targetId` uniqueness** across the package's own `WeaverTargets` (two targets in one package must
  not collide; cross-package collision is caught at runtime by `registry.go dispatchTarget` but the
  package-local check fails fast with a clear authoring error). Reject duplicates.
- **`targetId` key-shape** — must match the engine's `singleTokenPattern` (`^[A-Za-z0-9_-]+$`,
  `registry.go` line 34): it becomes a `weaver-targets` key prefix and a durable-name segment, so dots
  are forbidden. Reject otherwise.
- **`gaps` key shape** — every key matches `missing_<gap>` and `singleTokenPattern` (the engine's
  `validateTarget`, `registry.go` line 350, re-checks this at load; failing fast at install is the
  better author experience). Reject otherwise.
- **Reserved param key** — no `gaps[col].params` key may be `expectedRevision` (engine-owned; the
  engine's `validateTarget` rejects it — mirror it at install).
- **Pattern key-shape** — `patternId` / `subjectType` non-empty; `steps` non-empty; each step `kind` ∈
  {`systemOp`, `userTask`} and `operation` non-empty (mirror `loom/pattern.go validate`, lines 121–143,
  as fail-fast install validation — do NOT import the engine; re-state the rules in pkgmgr).
- **`OpMetaSpec.OperationType`** non-empty and a valid single token.
- Carry the **Epic-12 bucket-guard fail-closed lesson**: validation rejects loudly with an actionable
  error naming the offending spec index + field. No silent skips.

**AC6 — Manifest schema extension (Task 6).** Extend the `declares:` block in the manifest schema with
`weaverTargets`, `loomPatterns`, and `opMetas` (the YAML `Manifest` struct + `ManifestBlock` in
`manifest.go`), and extend `VerifyAgainstDefinition` so YAML↔Go drift is caught for the new kinds
(count + identity checks, mirroring the existing DDL/Lens/Permission cross-checks). Update the schema
narrative in `docs/components/_packages.md` accordingly (docs are NOT frozen; `_packages.md` lives under
`docs/components/`, editable).

**AC7 — Tests (Task 7).** Unit tests in `internal/pkgmgr` (model on `build_test.go`,
`bucketguard_test.go`, `manifest_test.go`):
- **Emit shape** — a `Definition` with one of each spec produces the exact vertex + `.spec` aspect
  envelopes (class strings, `data`-wrapped body, deterministic NanoID keys). Assert the emitted target
  body deserializes into `registry.Target` and the pattern body into `loom.Pattern` (import those
  structs in the test to prove CDC parse-compatibility — this is the regression that proves "no engine
  change needed").
- **lensRef resolution** — a `WeaverTargetSpec.LensRef` = a declared lens canonicalName emits a target
  body whose `lensRef` is that lens's deterministic NanoID; an unknown `LensRef` fails the install
  (error).
- **Validation** — duplicate `targetId` rejected; bad `targetId` key-shape rejected; non-`missing_*`
  gap key rejected; `expectedRevision` param rejected; bad step kind rejected; empty `OperationType`
  rejected.
- **Manifest drift** — a manifest whose `weaverTargets`/`loomPatterns`/`opMetas` counts or identities
  disagree with the Definition is rejected by `VerifyAgainstDefinition`.
- **declaredKeys** — every emitted key (vertex + spec aspect) appears in the install result's
  `DeclaredKeys` so uninstall reclaims it.
- An install-level test only if feasible without standing up the engines (the existing pkgmgr tests are
  pure-function / build-batch tests; an end-to-end install lands in 11.1's package tests + 11.2's
  harness — do NOT stand up Weaver/Loom here).

## Tasks / Subtasks

- [ ] **Task 1 — `Definition` fields + spec types.** (AC1)
  - [ ] In `internal/pkgmgr/definition.go`, add to `Definition`:
        `WeaverTargets []WeaverTargetSpec`, `LoomPatterns []LoomPatternSpec`, `OpMetas []OpMetaSpec`
        (with doc comments describing what they do NOW — NO history comments).
  - [ ] Define `WeaverTargetSpec` (`TargetID`, `LensRef`, `Gaps map[string]GapActionSpec`).
  - [ ] Define `GapActionSpec` mirroring `registry.GapAction` field-for-field
        (`Action, Pattern, Subject, Adapter, Operation, Assignee, Target, Params map[string]string`).
        Do NOT import `internal/weaver` into `pkgmgr` (installer must not depend on an engine) — define
        the parallel struct locally; the test proves byte-compatibility by round-tripping through
        `registry.Target`.
  - [ ] Define `LoomPatternSpec` (`PatternID`, `SubjectType`, `CompletionDomains []string`,
        `Steps []StepSpec`) and `StepSpec` (`Kind`, `Operation`, `Guard`). Carry `Guard` as
        `map[string]any` (or `json.RawMessage`) so the §10.5 declarative guard round-trips into
        `loom.Step.Guard`.
  - [ ] Define `OpMetaSpec` (`OperationType string`).
- [ ] **Task 2 — Emit logic in `buildInstallBatch`.** (AC2)
  - [ ] Mint deterministic NanoIDs for each WeaverTarget/LoomPattern/OpMeta in `installer.go Install`
        (alongside the existing ddl/lens/perm NanoID slices, ~lines 166–178), passing them into
        `buildInstallBatch` (extend its signature like the existing id slices).
  - [ ] In `build.go buildInstallBatch`, after the Lens loop, emit:
        WeaverTarget vertex+`.spec`, LoomPattern vertex+`.spec`, OpMeta vertex (no spec). Use the
        existing `docVertex` / `docAspect` helpers; append every key to `declared`.
  - [ ] Build the target/pattern spec bodies as `map[string]any` mirroring `lensSpecBody` (a helper
        `weaverTargetSpecBody` / `loomPatternSpecBody`), emitting only the fields the engine parses;
        omit empty optional fields (`completionDomains`, gap-action optional fields, `params`) so the
        body matches the fixture's minimal shape.
  - [ ] Keep emission order deterministic (stable iteration; sort map-derived keys as the Lens loop
        sorts aspect names) so the mutation list is reproducible.
- [ ] **Task 3 — Cross-reference resolution.** (AC3)
  - [ ] Resolve `WeaverTargetSpec.LensRef` (a lens canonicalName) → the in-batch lens NanoID via the
        same `deterministicNanoID(def.Name, def.Version, "lens:"+canonicalName)` the lens loop uses.
        Build a `canonicalName → lensNanoID` map from `def.Lenses` first; look up `LensRef` in it.
  - [ ] Fail the install (pure validation error, before any KV write) when `LensRef` matches no declared
        lens. (Do this in a validation pass; see Task 5.)
  - [ ] Confirm `gaps[col].pattern` and `gaps[col].operation` are shipped VERBATIM (no rewrite) — add a
        code comment stating they resolve live via the engine registry (patternMetaKey / opMetaKey).
- [ ] **Task 4 — Op-meta discoverability.** (AC4)
  - [ ] Emit each `OpMetaSpec` as a `vtx.meta.<NanoID>` vertex with
        `data: {operationType: <OperationType>}`, class `meta.ddl.vertexType` (the `installOpMeta`
        shape). No `.spec` aspect.
  - [ ] Document (code comment + Dev Notes) that a package declaring an `assignTask` target op (e.g.
        `SignLease`) must also declare a matching `OpMetaSpec` so `forOperation` resolves — this is the
        package author's contract, surfaced for 11.1.
  - [ ] (Investigation already done — see Dev Notes "Op-meta discoverability".) Confirm the emitted
        op-meta vertex is the shape `registry.go indexOpMeta` + `source.go indexOpMeta` read: a
        non-routed (`class != meta.weaverTarget|meta.loomPattern`) meta-vertex carrying
        `data.operationType`. Both engines index it identically.
- [ ] **Task 5 — Install validation / guardrails.** (AC5)
  - [ ] Add pure validators to a new `internal/pkgmgr/orchestrationguard.go` (mirror `bucketguard.go`):
        `validateWeaverTargets()`, `validateLoomPatterns()`, `validateOpMetas()` on `Definition`.
  - [ ] `validateWeaverTargets`: `targetId` non-empty + matches `^[A-Za-z0-9_-]+$`; no duplicate
        `targetId` within the package; every `gaps` key matches `missing_` prefix + `^[A-Za-z0-9_-]+$`;
        no `params["expectedRevision"]`; `LensRef` resolves to a declared lens (Task 3).
  - [ ] `validateLoomPatterns`: `patternId`/`subjectType` non-empty; ≥1 step; each step `kind` ∈
        {`systemOp`,`userTask`} and `operation` non-empty. (Re-state the §10.5 rules; do NOT import
        `internal/loom`.)
  - [ ] `validateOpMetas`: `OperationType` non-empty + valid single token.
  - [ ] Wire all three into `installer.go Install` alongside `validateLensBuckets` /
        `validateLensAdapters` (~lines 86–91), BEFORE the bucket-exists probe and any KV op. Errors are
        loud + actionable (name the spec index + field), fail-closed.
- [ ] **Task 6 — Manifest schema extension.** (AC6)
  - [ ] In `manifest.go`, add to `ManifestBlock`: `WeaverTargets []ManifestWeaverTarget`,
        `LoomPatterns []ManifestLoomPattern`, `OpMetas []ManifestOpMeta` (YAML tags
        `weaverTargets,omitempty` etc.). Define the three entry structs with the identity field each
        cross-checks (`targetId` / `patternId` / `operationType`).
  - [ ] Extend `VerifyAgainstDefinition` with count + identity cross-checks for the three new kinds
        (mirror the DDL/Lens/Permission blocks).
  - [ ] Update `docs/components/_packages.md` "Manifest schema" + "Field semantics" to document the new
        `declares` keys (editable doc; mirror the existing ddls/lenses/permissions entries).
- [ ] **Task 7 — Tests.** (AC7)
  - [ ] `internal/pkgmgr/orchestrationguard_test.go` — validation rejection cases (mirror
        `bucketguard_test.go`).
  - [ ] Extend/add a build test (mirror `build_test.go`) asserting emit shape + lensRef resolution +
        deterministic keys + `declaredKeys` membership; round-trip the emitted target body through
        `registry.Target` and the pattern body through `loom.Pattern` (these imports are allowed in the
        TEST file — `pkgmgr_test` package — to prove CDC parse-compatibility without the production code
        depending on an engine).
  - [ ] `manifest_test.go` additions — drift detection for the new kinds.
- [ ] **Task 8 — Verification gates (full CI set — see Dev Notes).** (all AC)

## Dev Notes

### The fixture envelopes are your spec — read them first

`internal/weaver/weaver_e2e_test.go` lines 85–139 are the authoritative on-wire reference:
- `installWeaverTarget` (85–95): vertex `{class: meta.weaverTarget, data: {}}` + spec aspect
  `{class: weaverTargetSpec, data: <target body>}`.
- `installLoomPattern` (102–116): vertex `{class: meta.loomPattern, data: {}}` + spec aspect
  `{class: loomPatternSpec, data: <pattern body>}`.
- `installOpMeta` (118–127): vertex `{class: meta.ddl.vertexType, data: {operationType: <op>}}` — no
  spec aspect.

The emitted `.spec` aspect goes through the existing `docAspect` helper, which produces
`{class, isDeleted, data, vertexKey, localName}` — the **same** envelope the Lens `spec` aspect uses,
which Refractor already CDC-loads. Both engines' unwrap functions (`registry.go unwrapSpecBody`,
`source.go unwrapPatternBody`) pull the spec body from `.data`. So matching the Lens emit pattern is
provably correct — this is the regression the round-trip test locks in.

### Op-meta discoverability (resolves 11.1 Open Q #2) — investigated, conclusion below

`assignTask`'s `forOperation` is resolved by `strategist.go` (line 153) via `source.opMetaKey(operation)`.
`opMetaKey` reads the `operationType → vtx.meta.<id>` index built by `registry.go indexOpMeta`
(lines 529–540) — which fires for **any** `vtx.meta.*` vertex whose envelope class is **not** routed
(`!= meta.weaverTarget && != meta.loomPattern`) and that carries `data.operationType`. `loom/source.go`
builds the **identical** index (lines 275–286) for userTask `forOperation` resolution.

The installed DDL meta-vertex (`build.go` line 82) is written with **empty `data`** —
`permittedCommands` lives in a *sibling aspect*, not on the vertex `data`. So the DDL vertex is NOT
discoverable by `operationType`. **Conclusion: the package must emit a dedicated op-meta vertex per
`forOperation`-resolvable op** (the `installOpMeta` shape). This is the `OpMetaSpec` seam. It is
**package-side, no engine change** — the engines already index exactly this shape. Teaching
`indexOpMeta` to read `permittedCommands` would be an engine change → **STOP-and-escalate**; do not.

### lensRef: by-NanoID, but not dispatch-load-bearing

§10.8 calls `lensRef` "the meta.lens id of the violation Lens." A package author writes the lens
**canonicalName**; the installer resolves it to the lens's in-batch deterministic NanoID. **But**:
`grep` confirms `LensRef` is consumed only by Weaver's control API (`internal/weaver/control.go` lines
21/89) — lane-1 dispatch watches `weaver-targets` under `<targetId>.>`, never via `lensRef`. So
resolve it for correctness/control-surface hygiene, but do NOT build resolution machinery beyond a
`canonicalName → NanoID` map lookup. An unresolvable `lensRef` is still a fail-closed install error
(a package shipping a dangling control-surface reference is a config bug).

### pattern/operation refs: by-id-string, resolved live (no install resolution)

- `gaps[col].pattern` (triggerLoom) → `patternId` string. Weaver's `registry.go indexPattern`
  (463–486) indexes the `patternId` string itself, so `pattern: "onboarding"` resolves live at
  dispatch. **Ship verbatim.**
- `gaps[col].operation` (assignTask/nudge/directOp) → `operationType` string. Resolved live via
  `opMetaKey` against the op-meta index. **Ship verbatim.** (The `nudge.operation` is the resolve-op
  type and is **required** per §10.8 as amended 2026-06-13 — but the installer doesn't enforce that the
  named op exists; the engine surfaces an unresolved op to Health at dispatch. The installer only
  enforces key-shape and the `expectedRevision` reserved-param rule. Do not over-reach into runtime
  resolvability.)

### Contract surfaces this seam builds to (FROZEN — build to them, never edit)

- **§10.2** (`docs/contracts/10-orchestration-surfaces.md`) — `weaver-targets` bucket, `<targetId>.<entityId>`
  key, `targetId` uniqueness install-validated. (The *lens* that projects rows is 11.1's; 11.1a only
  ships the `targetId` the rows key under, via the target spec.)
- **§10.5** — Loom pattern shape `{patternId, subjectType, completionDomains?, steps: [{kind, operation,
  guard?}]}`; step kinds `systemOp`/`userTask`; linear only; guards are pure declarative predicates.
  11.1a ships the *shape*; 11.1 authors the concrete patterns.
- **§10.8** — `meta.weaverTarget` body `{targetId, lensRef, gaps}`; `GapAction` contracts; every gap key
  `missing_<gap>`; no `expectedRevision` param; `targetId` single-token. The seam emits this; the engine
  re-validates at load (`validateTarget`).

### Key-shape & comment conventions (binding — CLAUDE.md)

- **Aspects** are 4-segment `vtx.<type>.<id>.<localName>`. The spec aspect is `vtx.meta.<id>.spec`.
- **Meta-vertices** are `vtx.meta.<NanoID>` — NEVER `vtx.meta.<canonicalName>`. Mint via
  `deterministicNanoID`.
- **NO history/changelog comments** anywhere (`// Story 11.1a …`, `// Replaces …`, `// Previously …`,
  `// renamed from …`). Comments describe what the code does NOW for a reader with no knowledge a
  change happened. git blame + the commit message are the record. (This is the single most-violated
  rule — do not reintroduce it.)

### Installer-must-not-depend-on-an-engine

`internal/pkgmgr` must NOT import `internal/weaver` or `internal/loom` in PRODUCTION code (it would
couple the installer to an engine and risk an import cycle). Define the parallel `GapActionSpec` /
`StepSpec` structs locally in `pkgmgr`. The **test file** (package `pkgmgr_test`) MAY import
`internal/weaver` + `internal/loom` to round-trip the emitted bodies through `registry.Target` /
`loom.Pattern` — that is the proof of byte-compatibility and the no-engine-change regression.

### Verification gates — run the FULL CI set (LESSON from Epic 12)

An install/spec-SHAPE change must run the full CI set, not just `go test` (the Epic-12 / 12.4 CI
gotcha — CI-only checks and `scripts/verify-package-*.go` validate install shapes outside the Go test
binary):

1. `go build ./...`
2. `make vet`
3. `golangci-lint run ./...`
4. `make verify-kernel`
5. `make test-bypass` (Gate 2 — all BLOCKED)
6. `make test-capability-adversarial` (Gate 3 — all DEFENDED)
7. `make test-hello-lattice` (Gate 5 — install/lens shapes can break the integration plane)
8. `make verify-package-rbac` / `make verify-package-identity` / `make verify-package-identity-hygiene`
   — confirm the new `Definition`/`buildInstallBatch`/`manifest` fields do **not** regress existing
   installs (the new slices are empty for those packages, so emit + validation must no-op cleanly).
9. `make verify-conformance` — confirm the frozen DDL-aspect set / reply-constraint is untouched.
10. Targeted `go test`: `./internal/pkgmgr/...` (the seam + its tests) and `./internal/weaver/...` +
    `./internal/loom/...` (the engine fixtures must STILL PASS unchanged — the proof that 11.1a made
    no engine change).
11. **grep `scripts/verify-package-*.go` for any cross-check that enumerates install shapes** — if a
    verify-package script asserts the exact declared-key set or envelope shape, the three new kinds may
    need it taught (or confirm it is package-specific and unaffected). The 12.4 lesson: an install-shape
    change can break a CI-only check that no `go test` exercises.

### Project Structure Notes

- All source changes are under `internal/pkgmgr/`:
  `definition.go` (new spec types + fields), `build.go` (emit logic + spec-body helpers),
  `installer.go` (NanoID minting + validation wiring), `manifest.go` (schema + drift cross-check),
  new `orchestrationguard.go` (pure validators), and `_test.go` files.
- One doc edit: `docs/components/_packages.md` (manifest schema narrative). Docs are editable.
- **No** `packages/` changes (11.1's job). **No** engine changes (HARD guardrail). **No** edits to
  `docs/contracts/*` (FROZEN) or `_bmad-output/planning-artifacts/*` (planning-lead-owned).

### References

- [Source: internal/pkgmgr/definition.go] — `Definition` / `DDLSpec` / `LensSpec` / `PermissionSpec` /
  `OutputDescriptorSpec`; the field shape to extend.
- [Source: internal/pkgmgr/build.go] — `buildInstallBatch` (emits Roles/DDLs/Lenses/Permissions only —
  the gap); `lensSpecBody` (~220) the closest emit template; `docVertex`/`docAspect`/`docLink` helpers;
  `sha256NanoID`.
- [Source: internal/pkgmgr/installer.go] — `Install` (validation wiring ~86–91; NanoID minting
  ~166–178; `deterministicNanoID` ~213); the InstallPackage op carries the `mutations` batch.
- [Source: internal/pkgmgr/manifest.go] — `Manifest` / `ManifestBlock` / `VerifyAgainstDefinition`
  (the drift cross-check to extend).
- [Source: internal/pkgmgr/bucketguard.go + bucketguard_test.go] — the pure-validation, fail-closed
  precedent (Epic-12 lesson) to mirror in `orchestrationguard.go`.
- [Source: internal/weaver/weaver_e2e_test.go #85-139] — `installWeaverTarget` / `installLoomPattern` /
  `installOpMeta` / `putRow`: the canonical on-wire `.spec` envelopes + op-meta shape.
- [Source: internal/weaver/registry.go] — `Target` / `GapAction` (parse target); `validateTarget`
  (~350, the install-time rules to mirror); `singleTokenPattern` (~34); `indexOpMeta` (~529) +
  `opMetaKey` (assignTask forOperation); `indexPattern`/`patternMetaKey` (triggerLoom resolution);
  `unwrapSpecBody` (~606, the `.data` unwrap).
- [Source: internal/weaver/strategist.go #153] — `assignTask` → `opMetaKey(operation)`; errors when no
  op-meta vertex — proves the op-meta discoverability requirement.
- [Source: internal/weaver/control.go #21,#89] — `lensRef` is consumed only on the control API, not in
  dispatch (so lensRef resolution is hygiene, not load-bearing).
- [Source: internal/loom/pattern.go] — `Pattern` / `Step` (parse target); `validate` (~121, step-kind
  rules to mirror); `StepKindSystemOp`/`StepKindUserTask`.
- [Source: internal/loom/source.go] — `unwrapPatternBody` (~333, the `.data` unwrap); `indexOpMeta`
  (~275, identical op-meta index for userTask forOperation).
- [Source: docs/contracts/10-orchestration-surfaces.md #10.2,#10.5,#10.8] — FROZEN orchestration
  surfaces the emitted shapes build to.
- [Source: docs/components/_packages.md #Manifest schema] — the manifest `declares` schema to extend.
- [Source: _bmad-output/implementation-artifacts/11-1-lease-signing-package-authoring.md] — the
  downstream consumer; Open Questions #1/#2/#3 that 11.1a resolves.

## Winston's Adjudication (RESOLVED — implement these; the questions below are kept for rationale)

All four parked questions are resolved with the authoring recommendation. Implement exactly this:

1. **Op-meta: explicit `OpMetaSpec` field** (NOT auto-emit from `PermittedCommands`). Author declares one
   `OpMetaSpec` per `forOperation`-resolvable op. Do NOT add a playbook→op-meta cross-validation (stay
   consistent with "the installer doesn't enforce runtime resolvability"; the engine surfaces an
   unresolved op to Health at dispatch). Note the auto-from-PermittedCommands option in a doc comment as
   a future ergonomic, nothing more.
2. **`lensRef`: fail-closed, accept BOTH forms.** If `LensRef` matches a declared lens canonicalName →
   resolve to its in-batch NanoID; else if `LensRef` is already a valid NanoID → pass through (supports a
   cross-package/already-installed lens); else → fail-closed install error.
3. **No `verify-package-*` smoke fixture in 11.1a.** The targeted gates (existing
   verify-package-rbac/identity/identity-hygiene proving no-regression + the round-trip unit test proving
   CDC-parse-compatibility) are sufficient. The package-level smoke fixture lands in 11.1/11.2.
4. **`StepSpec.Guard` carrier = `map[string]any`**, marshaled into the step's `guard` field, omitted when
   nil. (Not `json.RawMessage` — worse authoring ergonomics.)

## Open Questions for Winston

These were surfaced during authoring and parked rather than guessed. ALL RESOLVED above.

1. **Should `OpMetaSpec` be a standalone field, or folded into `DDLSpec`?** This story adds a top-level
   `OpMetas []OpMetaSpec` so a package can declare op-meta vertices independently. An arguably cleaner
   design: have `buildInstallBatch` automatically emit a per-op `operationType` op-meta vertex for each
   of a DDL's `PermittedCommands` (so authors never hand-list them, and any DDL op becomes
   `forOperation`-resolvable for free). That couples op-meta emission to DDL declaration and might emit
   op-meta vertices for ops that never need `forOperation` (harmless but noisier). **Recommendation:**
   ship the explicit `OpMetaSpec` seam in 11.1a (minimal, matches the fixture exactly, author controls
   exactly which ops are resolvable), and note the auto-from-PermittedCommands option as a possible
   future ergonomic. **Does Winston prefer explicit `OpMetaSpec`, or auto-emit from `PermittedCommands`?**
   (If auto-emit: confirm it does not collide with the DDL's own meta-vertex — they are distinct
   `vtx.meta.<id>` keys, so it is safe, but it doubles the meta-vertex count per op.)

2. **`lensRef` resolution: fail-closed on dangling, or warn-and-proceed?** A `WeaverTargetSpec.LensRef`
   naming a non-declared lens is, per AC3, a fail-closed install error. But `lensRef` is not
   dispatch-load-bearing (control-surface only), so an argument exists for warn-and-proceed (consistent
   with the `Depends` warn-and-proceed posture). **Recommendation:** fail closed — a dangling
   control-surface reference is a config bug and the Epic-12 lesson is fail-closed-by-default — but
   flagging for the call. Also: should `lensRef` permit a **literal NanoID** pass-through (for a target
   whose lens is in a *different already-installed* package), in addition to canonicalName resolution?
   The fixture writes a literal id; cross-package lens references are plausible. **Recommendation:**
   accept both — if `LensRef` matches a declared lens canonicalName, resolve it; else if it is already a
   valid NanoID, pass through; else fail. Confirm.

3. **Does 11.1a need a `verify-package-*` smoke fixture, or is unit coverage enough?** 11.1a ships no
   `packages/` content, so there is nothing for a `verify-package-lease-signing.go` to assert yet (that
   is 11.1's). But the 12.4 lesson is that install-shape changes can break CI-only checks. The targeted
   gates (existing `verify-package-rbac/identity/identity-hygiene` proving no regression + the
   round-trip unit test proving CDC-parse-compatibility) should fully cover 11.1a. **Recommendation:**
   no new `verify-package` target in 11.1a; the smoke fixture lands in 11.1 (`lease-signing`) /
   11.2 (harness). Confirm the targeted gates are sufficient.

4. **`Guard` carrier type in `StepSpec`.** §10.5 guards are a declarative shape (`{absent: ...}` /
   `{allOf: [...]}`) or a reserved Starlark escape hatch, and `loom.Step.Guard` is `json.RawMessage`.
   The cleanest authoring type in `pkgmgr` is `map[string]any` (round-trips to `json.RawMessage` on
   marshal) so package authors write a Go map literal. **Recommendation:** `map[string]any`, marshaled
   into the step's `guard` field, omitting it when nil. Confirm vs. `json.RawMessage` (which would force
   authors to write JSON string literals — worse ergonomics).

## Dev Agent Record

### Agent Model Used

Amelia / bmad-dev-story — Opus 4.8 (claude-opus-4-8), installer-seam + spec-type + validation work across pkgmgr.

### Debug Log References

- `go test ./internal/pkgmgr/...` → ok (0.7s). All new validation, emit round-trip, lensRef, manifest-drift, declaredKeys, deterministic-key tests pass.
- `go test ./internal/weaver/... ./internal/loom/...` → ok (weaver 77s, loom 40s). Engine fixtures pass UNCHANGED — the no-engine-change regression holds.
- `go build ./...`, `make vet`, `golangci-lint run ./...` → all clean (0 issues).
- `go test ./internal/processor -run TestConformance` (verify-conformance) → ok.
- Heavier infra gates (verify-kernel, verify-package-rbac/identity/identity-hygiene, test-bypass, test-capability-adversarial, test-hello-lattice) NOT run locally: NATS is up on :4222 but the worktree has no `lattice.bootstrap.json` (`make up` not run here). Deferred to Winston's CI per ANTI-HANG.

### Completion Notes List

**Implementation (all 8 tasks complete, exactly per Winston's Adjudication):**

- **Task 1 (definition.go):** Added `WeaverTargets`, `LoomPatterns`, `OpMetas` to `Definition`; defined `WeaverTargetSpec`, `GapActionSpec` (field-for-field mirror of `weaver.GapAction`), `LoomPatternSpec`, `StepSpec` (`Guard map[string]any` per Adjudication #4), `OpMetaSpec`. No engine imports in production code.
- **Task 2 (build.go):** Emit loops after the Lens loop — WeaverTarget vertex (`meta.weaverTarget`, empty data) + `.spec` aspect (`weaverTargetSpec`), LoomPattern vertex (`meta.loomPattern`) + `.spec` (`loomPatternSpec`), OpMeta vertex (`meta.ddl.vertexType`, `data.operationType`, NO spec aspect). Spec-body helpers `weaverTargetSpecBody`/`gapActionBody`/`loomPatternSpecBody` emit only engine-parsed fields, omitting empty optionals. Every key appended to `declared`. NanoIDs minted in installer.go with tags `weaverTarget:`/`loomPattern:`/`opMeta:`.
- **Task 3 (build.go `resolveLensRef`):** LensRef → declared-lens canonicalName→NanoID map; literal-NanoID pass-through (Adjudication #2); else fail-closed error (returned by `buildInstallBatch` before any KV write). `gaps[col].pattern`/`operation` shipped verbatim (code comment notes live registry resolution).
- **Task 4 (op-meta):** Explicit `OpMetaSpec` seam (Adjudication #1); no playbook→op-meta cross-validation. Emitted op-meta vertex exactly matches the `installOpMeta` shape both engines' `indexOpMeta` read. Auto-from-PermittedCommands noted as a future ergonomic in the `OpMetaSpec` doc comment only.
- **Task 5 (orchestrationguard.go):** Pure fail-closed validators `validateWeaverTargets`/`validateLoomPatterns`/`validateOpMetas` (mirror `bucketguard.go`); rules re-stated locally (no engine import). Wired into `installer.go Install` alongside `validateLensBuckets`/`validateLensAdapters`, before any KV op. Errors name the offending spec index + field.
- **Task 6 (manifest.go + docs):** `ManifestBlock` gains `weaverTargets`/`loomPatterns`/`opMetas`; entry structs `ManifestWeaverTarget`/`ManifestLoomPattern`/`ManifestOpMeta`; `VerifyAgainstDefinition` count + identity cross-checks for all three. `docs/components/_packages.md` schema example + field semantics updated.
- **Task 7 (tests):** `orchestrationguard_test.go` (validation rejections); `orchestration_emit_test.go` (external `pkgmgr_test` package — round-trips emitted bodies through real `weaver.Target` / `loom.Pattern`, proving CDC parse-compatibility + no engine change; covers lensRef canonicalName resolution, literal-NanoID pass-through, dangling fail-closed, op-meta shape/no-spec, declaredKeys membership, deterministic keys); `manifest_test.go` drift additions. Round-trip test reaches the unexported `buildInstallBatch` via a test-only `export_test.go` re-export (keeps engine imports out of production and out of the internal test package's coupling).

**Notable decisions / deviations:**

- **`export_test.go` helper:** the story's round-trip test must (a) live in `package pkgmgr_test` and import the engines, yet (b) drive the unexported `buildInstallBatch`. Resolved with the standard Go `export_test.go` idiom (a `package pkgmgr` file that re-exports `BuildInstallBatchForTest` + `DeterministicNanoIDForTest` for the external test). Production code still imports no engine. Minor addition beyond the story's literal file list; flagged for review.
- **No engine-edit pressure encountered.** Every shape was emittable in `internal/pkgmgr` to match the frozen fixture envelopes; the round-trip test confirms `weaver.Target`/`loom.Pattern` parse the emitted bodies unchanged. HARD GUARDRAIL never tripped.
- **verify-package scripts:** grepped all three (`scripts/verify-package-{rbac,identity,identity-hygiene}.go`) — they are additive presence-checks (scan `vtx.meta.*.canonicalName` for known DDL/Lens names), NOT exhaustive declared-key-set checks. The rbac/identity/identity-hygiene packages declare zero orchestration specs, so the new emit loops are no-ops for them. The 12.4 exact-set CI-break risk does not apply; no script needs teaching (confirms Adjudication #3 — no new verify-package fixture in 11.1a).

### File List

Modified:
- `internal/pkgmgr/definition.go` — new `Definition` fields + spec types.
- `internal/pkgmgr/build.go` — emit loops, spec-body helpers, `resolveLensRef`, envelope-class consts, `buildInstallBatch` signature.
- `internal/pkgmgr/installer.go` — orchestration validation wiring + deterministic-NanoID minting + extended `buildInstallBatch` call.
- `internal/pkgmgr/manifest.go` — `ManifestBlock` fields, entry structs, `VerifyAgainstDefinition` cross-checks.
- `internal/pkgmgr/manifest_test.go` — orchestration drift-detection tests.
- `docs/components/_packages.md` — manifest schema example + field semantics for the three new `declares` keys.

Created:
- `internal/pkgmgr/orchestrationguard.go` — pure fail-closed validators.
- `internal/pkgmgr/orchestrationguard_test.go` — validation rejection tests.
- `internal/pkgmgr/orchestration_emit_test.go` — external-package emit/round-trip/lensRef/declaredKeys tests.
- `internal/pkgmgr/export_test.go` — test-only re-export of `buildInstallBatch` + `deterministicNanoID`.
