# Package version upgrade / DDL hot-reload (F-004) — design

**Status: 📐 awaiting-Andrew (ratification).**
**Component:** Core — `internal/pkgmgr` + the primordial install kernel DDLs (`internal/bootstrap`) + `cmd/lattice-pkg` + the dev-loop Makefile. No change to Refractor / Weaver / Loom / Processor-step8 (they already react).
**Backlog row:** Lattice lane → *Refinements & ops → Package version upgrade / DDL hot-reload (F-004)* (📐 prioritized for Designer, Andrew 2026-06-27; re-prioritized ★→★★ on grounded dev-loop demand).
**Author:** Winston (Designer fire, 2026-06-27).

---

## For Andrew (one-look ratification)

**What it does (two lines).** Makes a Capability-Package **upgrade in place**: a re-install at a new
version (today a hard `ErrVersionMismatch`) and a same-version edit (today silently *skipped*) both become a
**diff-and-apply** — create new entities, update changed ones, tombstone removed ones — in **one atomic
Processor batch**, after which the existing Refractor lens hot-reload + Processor DDL-cache invalidation
converge with no `make down`. Closes the recurring dev-loop tax on the implementation agents ("the shared
binary is stale; a DDL change won't hot-reload").

**No architectural fork.** Nothing here touches the Gateway / read-path-auth / Vault / multi-cell / HA-NATS
axes. There is **one frozen-contract change** and **two confirm-able product decisions** (below) — all
resolved in this doc, none left open.

**Frozen-contract change — Contract #8 (staged UNCOMMITTED in `main`):**
1. **§8.1 — entity NanoIDs become version-independent** (salt `name + tag`, not `name + version + tag`).
   This is the load-bearing decision: it makes the *same* logical lens/DDL/role keep the *same*
   `vtx.meta.<id>` key across versions, so upgrade is an in-place update of stable keys instead of a re-mint
   that would orphan old vertices **and break every NanoID cross-reference** (a WeaverTarget's `lensRef`, a
   permission's `grantedBy` link). Affected consumer: `internal/pkgmgr` derivation only — every *downstream*
   consumer already keys on canonicalName/lensID/targetId (stable identity), never the version-in-the-key.
2. **§8.5 → new §8.6 `UpgradePackage` op** (replaces "version upgrade is out of scope"): a third primordial
   kernel DDL alongside Install/Uninstall, carrying mixed `create`/`update`/`tombstone` mutations; protected
   roots are already covered authoritatively by the step-8 guard (§8.4, path-independent).

**Two product decisions I made (decide-don't-defer — flagging for your awareness, not asking):**
- **`lattice-pkg install` auto-upgrades on a version change** (logs `upgrading X 0.1.0→0.2.0: N created / M
  updated / K tombstoned`), rather than keeping the hard error. Rationale: the grounded pain *is* the hard
  error; `--dry-run` makes the delta previewable first. An explicit `lattice-pkg upgrade` verb is also
  provided for intent-explicit operators.
- **Body-equality skip:** an upgrade emits `update` only for keys whose body actually changed, so an untouched
  lens isn't needlessly rebuilt. (Minimal-churn dev loop.)

**Eviction obligation discharged.** The Epic-12 carried obligation (`lattice-architecture.md:1245` — "an
actor-aggregate 'anchor no longer matches WHERE → tombstone' eviction *if in-place upgrade is ever
supported*") is satisfied **by the existing Refractor rebuild machinery**: a lens whose MATCH changes lands
as an *update to the same lensID* (because keys are version-stable), `ClassifyUpdate` returns `MatchChange`,
and Refractor does a full truncate-and-replay rebuild — which is the eviction. The version-drop is what ties
the rebuild to the same target; one decision, two payoffs.

---

## 1. Problem + intent

### 1.1 The grounded demand (three source-verified faces)

A recurring dev-loop tax on the implementation agents — re-prioritized ★→★★ by Andrew on this demand.

1. **Same-version reinstall is silently skipped.** `internal/pkgmgr/installer.go:130` sets `Skipped=true`
   ("already installed") when a package vertex exists at the same `Version`. An edited lens/DDL that keeps
   its version string (e.g. `packages/clinic-domain/lenses.go`) **never lands** — confirmed in the backlog's
   own notes ("a fresh `make down && make up-loftspace` is needed to load the new lens — same-version
   reinstall is skipped, the F-004 upgrade gap").
2. **A changed version hard-errors.** `installer.go:136` returns `ErrVersionMismatch` — there is no upgrade
   path *over* an existing install. Net: the only way to apply **any** package change is a full
   `make down && make up-<vertical>` (teardown + fresh install).
3. **Dev-loop companion (Makefile).** A vertical's FE binary isn't auto-rebuilt/restarted by partial flows:
   `up-clinic` (`Makefile:256`) pkills+rebuilds+restarts `bin/clinic-app`, but an agent editing a handler or
   running only `install-clinic` leaves the prior `:7799` process serving stale code.

### 1.2 Vision tie

This is the concrete first realization of several brainstorming-inventory items
(`brainstorming-session-2026-04-08.md`): **#23 DDL migration tool (root-actor only)**, **#24 schema
versioning + backward-compat policy**, **#123 lens schema migration / zero-downtime evolution** ("Spec
mentions REBUILD but not the swap" — this *is* the swap). **G6** (DDL changes break in-flight Loom instances
→ "Loom Instance Vertex pins its DDL version; migration tool warns on in-flight instances before allowing
breaking DDL changes") is the forward-looking caveat, scoped as a follow-on (§7). It also closes Contract #8
§8.5's explicit "Version upgrade … is a later story," and discharges the Epic-12 carried obligation
(`lattice-architecture.md:1245`).

---

## 2. The shape

### 2.1 Reconciliation — "didn't we already handle this?"

The principal should not have to ask. The honest answers:

- **The *downstream* reaction to changed meta-vertices already works.** Refractor's `CoreKVSource` watches
  each lens `.spec` by revision and `ClassifyUpdate` (`internal/refractor/lens/update.go`) chooses a hot-swap
  (INTO-only) vs a full rebuild (MATCH change); the Processor invalidates the `vtx.meta.*` DDL cache
  **in-commit** on any step-8 batch touching those keys (Contract #8 §8.2). Refractor even *inherited*
  Materializer's "rule hot-reload + zero-downtime migration" wholesale
  (`materializer-morph-plan.md:21`). **The gap is purely upstream:** the install path is *create-only* and
  *skips/errors* — so it never **produces** the create/update/tombstone delta the convergence machinery is
  waiting for. F-004 is exactly that missing producer; nothing below it needs to change.
- **Does this duplicate uninstall?** No. Uninstall tombstones the *whole* declared set. Upgrade is a *diff*:
  create-new + update-changed + tombstone-removed, atomically.
- **Does it introduce new state?** No. The package `.manifest` aspect already records `declaredKeys` +
  `version` (`build.go:242`). The diff reads exactly that — the old key set is already persisted; no new
  tracking vertex, no new bucket.

### 2.2 The load-bearing change — version-independent entity keys

Today every entity's NanoID is `deterministicNanoID(name, version, tag)` (`installer.go:249`): **version is
in the salt**, so the same logical lens at v0.1.0 vs v0.2.0 hashes to **different** `vtx.meta.<id>` keys. A
naive version bump would therefore (a) create an entirely new vertex set and orphan the old one, and worse
(b) **break every cross-reference that addresses an entity by its NanoID** — a WeaverTarget's `lensRef`
resolves to the lens NanoID (`build.go:184`); grant links are `lnk.permission.<permID>.grantedBy.role.<roleID>`
(`build.go:230`). Re-minting would force a full reference-rewrite pass on every upgrade.

**Fix — drop version from the salt** (`deterministicNanoID(name, tag)`), mirroring how *every downstream
consumer already keys*: by canonicalName / lensID / targetId — a stable logical identity, never the
version-in-the-key. Then:

| Install scenario | Keys | Behavior |
|---|---|---|
| fresh install (not present) | all new | **create** (byte-identical to today) |
| same-version re-install (no `--force`) | identical | **skip** (preserves today's idempotency) |
| same-version `--force` (dev refresh) | identical | **update** changed bodies in place |
| **version bump (upgrade)** | surviving entities keep their key | **update** survivors · **create** new · **tombstone** removed |

The permission tag also moves from `perm:<idx>:<operationType>` (position-dependent) to
`perm:<operationType>:<scope>` (logical identity), so reordering a package's `Permissions` slice doesn't
churn keys. (A package declaring two permissions with the same `operationType+scope` is a degenerate
duplicate — add a uniqueness validator alongside the existing `validateCanonicalNameUniqueness`.)

This is the **simplest extension of the machinery that already exists** — the deterministic-NanoID function
stays; only its salt becomes identity-stable.

### 2.3 The diff-and-apply engine (`Installer.Upgrade`)

New `Installer.Upgrade(ctx, def)` (P2-clean — submits an op, never writes KV directly):

1. `findInstalledPackage(def.Name)` → the installed package vertex + its version + its
   `.manifest.declaredKeys` (the **old key set**). Not present → `ErrNotInstalled` (upgrade requires a base).
2. Rebuild the **new** manifest with the *same* `buildInstallBatch` logical-document machinery (reused, not
   reinvented) → the **new key set** + new bodies.
3. **Diff by key** (the §8.6 partition): `new \ old → create`; `old \ new → tombstone`;
   `new ∩ old → update` *iff* the new logical body differs from the old committed `data` (read each surviving
   key once; byte-equal → omit). The package `.manifest` aspect itself is updated (new `declaredKeys` + new
   version) and the package vertex `version` aspect bumped — **in the same batch**, so version and entity-set
   are never inconsistent.
4. Submit one **`UpgradePackage`** op carrying the mixed mutations → one atomic step-8 batch.

### 2.4 The `UpgradePackage` kernel op (write path, P2)

A third primordial kernel DDL (`UpgradePackageDDLScript` in `internal/bootstrap/install_ddl.go`) alongside
`InstallPackageDDLScript` / `UninstallPackageDDLScript` — mirrors the pair, reuses `installGuardrailHelpers`
(key-shape + underscore-aspect reject). It accepts `op ∈ {create, update, tombstone}`. **Unlike install it is
not create-only**, so it is not safe-by-construction — it leans entirely on the **authoritative step-8
protected-key guard** (`rejectProtectedMutations`, `step8_commit.go`), which §8.4 already states covers every
update/tombstone "regardless of whether the originating script inspected `data.protected`" and "any future
DDL." So an upgrade physically cannot rewrite/tombstone a protected kernel/auth root. OCC is **unconditional**
(same deferral reasoning as §8.3 uninstall; the batch is atomic so no partial state). Payload, guardrails,
atomicity, and the eviction note are specified in the staged **Contract #8 §8.6**.

### 2.5 Read path (P5)

Unchanged. Operators/apps continue to read lens projections; the inspector (Loupe) continues to read Core KV.
The upgrade touches only the write path (ops) + the meta-vertex CDC the platform already consumes.

### 2.6 DDL-migration semantics (the add/change/remove matrix)

| Change across versions | Entity fate | Mechanism (all existing downstream) |
|---|---|---|
| New entity (id only in new manifest) | **create** | create mutation |
| Changed body, same identity | **update** (byte-equal → skip) | update mutation |
| Removed entity (only in old manifest) | **tombstone** | tombstone mutation |
| Lens **MATCH** changed | update → Refractor **full rebuild** (truncate+replay) | `ClassifyUpdate`=`MatchChange` ⇒ **evicts** stale rows (Epic-12 obligation) |
| Lens **INTO-only** changed | update → Refractor **hot-swap** adapter | `ClassifyUpdate`=`IntoOnly` |
| DDL **script** changed | update → Processor **in-commit cache invalidation** | step-8 `vtx.meta.*` invalidation |
| Role / permission / grant changed | create/update/tombstone | the `cap-roles.*` / `cap-read.*` lenses re-project (existing) |
| WeaverTarget / LoomPattern changed | update | Weaver / Loom registry CDC reload (existing) |

**Eviction obligation explicitly discharged.** When an upgrade changes a projection lens's cypher so that
previously-projected rows no longer match, the stale rows must be evicted (`lattice-architecture.md:1245`).
Because keys are version-stable (§2.2), the change lands as an **update to the same lensID**;
`ClassifyUpdate` returns `MatchChange` → Refractor **rebuilds** (truncate target + replay) → the stale rows
are gone. The obligation is met by the *existing* rebuild path, not new code. (The recently-shipped
anchor-tombstone retraction — `679fe25` — is the orthogonal CDC-driven case; this is the spec-change rebuild
case, which already exists.)

### 2.7 CLI + dev-loop surface

- **`lattice-pkg install <path>` becomes upgrade-aware:** not-installed → create (today); same version & no
  `--force` → skip (today); **different version → auto-upgrade** (diff-apply, with the explicit log line);
  same version & `--force` → diff-apply (dev refresh). Decision: auto-upgrade on version change (the hard
  error *is* the pain); `--dry-run` previews.
- **`lattice-pkg upgrade <path>`** — explicit alias for the diff-apply path; errors if not installed.
- **`--dry-run`** (install/upgrade) — prints the computed delta (create/update/tombstone counts + keys)
  without submitting. The "warn before applying" half of brainstorm #23/G6; cheap, high-value preview for an
  agent/operator. Decision: include.
- **Makefile `refresh-<vertical>`** (`refresh-clinic`, `refresh-loftspace`): `--force`-reinstall the
  vertical's packages onto the **running** stack (no `make down`) **and** pkill+rebuild+restart the FE binary
  (`bin/clinic-app` / `bin/loftspace-app`) — one command, no teardown, killing face #3. Plus a generic
  `reinstall-package PKG=<dir>` for a single package. (Makefile + docs only — no platform code.)

---

## 3. Contract surface

| § | Change vs. build-to | Detail | Staged |
|---|---|---|---|
| Contract #8 **§8.1** | **CHANGE** | NanoID derivation → version-independent (`name + tag`); permission tag → `operationType + scope`. | UNCOMMITTED in `main` |
| Contract #8 **§8.5 → §8.6** | **CHANGE** | Remove "version upgrade out of scope"; add `UpgradePackage` op (envelope, mixed-op payload, guardrails, atomicity, eviction note, OCC deferral); renumber out-of-scope to §8.7 (+ in-flight-instance pinning caveat). | UNCOMMITTED in `main` |
| Contract #8 §8.2 / §8.4 | **build-to** | Deterministic-`requestId` pattern (upgrade derives from `name+fromVersion+toVersion`); the authoritative protected-key guard already covers update/tombstone. | no change |
| Contract #2 / #3 | **build-to** | `UpgradePackage` is a normal `meta`-lane envelope; the script emits a mutation batch the Committer applies atomically. | no change |

Affected consumers of the §8.1 change: **`internal/pkgmgr` only** (the derivation + the new `Upgrade`).
Every other consumer keys on canonicalName/lensID/targetId, not the version-in-the-key, so none is touched.

---

## 4. Migration / compatibility

- **The derivation change re-mints keys exactly once.** A long-lived dev stack holds version-salted keys;
  the first upgrade/`--force` after the change computes version-free keys, so the diff is
  `old(salted) vs new(free)` = disjoint → create-all-new + tombstone-all-old: a **one-time blue-green
  re-mint** inside one atomic batch (both land together — Refractor sees old-lens deactivate + new-lens
  activate+rebuild, no window). Thereafter keys are stable forever and upgrades are true in-place updates.
  For **CI / a fresh `make up`** (bootstrap re-seeds the kernel) this is invisible; for a long-lived stack a
  single `make down && up` also clears it. Decision: **accept the one-time re-mint** — it self-heals, and the
  platform is pre-1.0 with no durable-data guarantee. Document it in `_packages.md`.
- **Fresh installs are byte-identical** to today (create path unchanged but for the salt), so the eight
  `verify-package-*` CI gates (which install onto a clean kernel) are unaffected.
- **Provenance on update:** installed-entity provenance is stamped by the Processor at step-8. For `update`
  mutations the upgrade actor must land as `updatedBy`/`updatedByOp` (verify the step-8 update path stamps it
  — an implementation detail, not a design fork).

---

## 5. Test strategy

- **Unit (`internal/pkgmgr`):** the diff engine (create/update/tombstone partition; body-equality skip;
  permission-identity stability under a reordered `Permissions` slice); version-free derivation golden keys
  (a fixed name+tag → a fixed NanoID, and the *same* key for two different versions); upgrade `requestId`
  determinism + independence across distinct `(from,to)` pairs.
- **Kernel-script unit (`internal/bootstrap`):** `UpgradePackage` guardrails — key-shape reject, underscore
  reject, mixed-op (`create`/`update`/`tombstone`) acceptance, unknown-`op` reject, empty-mutations reject.
- **e2e (ephemeral stack, mirroring `make test-object-gc` / lease-convergence):** install v1 → edit a lens
  cypher + bump version → upgrade → assert (a) the lens `.spec` is updated **in place at the same lensID**,
  (b) Refractor re-projects (a row that should now appear/disappear does), (c) a removed entity is tombstoned,
  (d) the package `version` aspect bumped. Plus a **force-reinstall** e2e (same version, edited body,
  `--force` → re-projects).
- **Adversarial:** an upgrade attempting to `update`/`tombstone` a protected kernel/auth root is rejected at
  step-8 (reuse the Gate-style protected-key assertion) — the load-bearing safety check now that
  `UpgradePackage` is not create-only.

---

## 6. Risks + alternatives

### Risks
- **Derivation change breaks same-version idempotency on a long-lived stack** → mitigated by the one-time
  self-healing re-mint (§4) + documented `make down && up`; CI unaffected.
- **`UpgradePackage` loses install's safe-by-construction property** → mitigated: the step-8 protected-key
  guard is authoritative & path-independent (§8.4 already says "any future DDL"); the adversarial test pins
  it. The residual is exactly the trust we already place in that guard for `UpdateMetaVertex`/`Tombstone…`.
- **Botched upgrade leaves a half-migrated package** → impossible: single atomic batch (all-or-nothing), with
  the version aspect bumped in the *same* batch — version and entity-set are always consistent.

### Alternatives considered (earn the recommendation)
- **(A — rejected) Keep version in the NanoID; upgrade = create-new + tombstone-old (re-mint every time).**
  Rejected: re-mints break every NanoID cross-reference (`lensRef`, grant links) on *every* upgrade, forcing
  a reference-rewrite pass, and churn Refractor (full rebuild of *every* lens) each upgrade. *Could a variant
  beat the recommendation?* Only if cross-version key-stability were undesirable — but every consumer already
  wants stable identity, so no. The version-drop is strictly simpler **and** correct.
- **(B — rejected) Extend `InstallPackage` to accept update/tombstone (no new script).** Rejected: muddies
  install's create-only guarantee and its safety-by-construction; a dedicated `UpgradePackage` keeps each
  script's guardrails crisp and mirrors the Install/Uninstall pairing.
- **(C — rejected) Uninstall-then-reinstall.** Rejected: two non-atomic ops → a window where the package
  (including its **auth lenses**) is fully gone before re-create; and it discards anything the entities
  reference. The atomic diff is strictly safer.
- **(D — rejected, deferred) Build in-flight-Loom-instance DDL-version pinning (G6) now.** Rejected as **dead
  scaffolding**: no dev-loop consumer needs it, and production in-flight fencing is a separate concern with no
  current pressure. The *design* is acknowledged (§8.7 caveat); the *build* is a follow-on (§7).

---

## 7. Decomposition for the Steward (fire-by-fire, each shippable + green)

- **Fire 1 — version-independent keys + the diff engine + `UpgradePackage` op (the core).** Drop version from
  `deterministicNanoID` (salt `name+tag`; permission tag `operationType+scope` + a uniqueness validator); add
  `Installer.Upgrade` (diff old `declaredKeys` vs new, partition create/update/tombstone, body-equality skip);
  add `UpgradePackageDDLScript` + its self-description + bootstrap wiring; submit-op plumbing
  (`requestId = name+from+to`). Unit + kernel-script tests. **Green:** fresh install byte-identical;
  same-version reinstall still idempotent; upgrade reachable via the Go API + a `lattice-pkg upgrade` verb.
  *(Frozen-contract §8.1 + §8.6 must be Andrew-ratified before this fire.)*
- **Fire 2 — CLI ergonomics.** `install` auto-upgrades on version change; `--force` (same-version dev
  refresh); `--dry-run` (preview the delta). e2e: install→edit→bump→upgrade→assert re-projection + tombstone
  + version bump; force-reinstall e2e.
- **Fire 3 — Makefile dev-loop refresh.** `refresh-clinic` / `refresh-loftspace` (force-reinstall onto the
  running stack + pkill/rebuild/restart the FE binary) + generic `reinstall-package PKG=…`; document the dev
  loop in `docs/components/_packages.md` + README. (Makefile + docs only.)
- **Follow-on (NOT built here) — G6.** In-flight-instance DDL-version pinning + a breaking-change migration
  warning. File as a separate Lattice-lane backlog item; build behind a concrete need.

---

## 8. Open questions — resolved

- **In-key version vs stable keys?** → **Stable keys** (§2.2), `name+tag` salt. Resolved.
- **Auto-upgrade on `install`, or require an explicit `upgrade` verb?** → **Both:** `install` auto-upgrades
  (the demand is friction) with a visible log + `--dry-run`; an explicit `upgrade` verb exists too. Resolved.
- **Update-all vs body-equality skip?** → **Skip unchanged** (minimal churn). Resolved.
- **Upgrade OCC?** → **Unconditional** (mirror §8.3). Resolved.
- **In-flight Loom instances during a breaking upgrade?** → **Out of scope here**, documented caveat (§8.7),
  filed as the G6 follow-on. Resolved.
- **Migration of existing version-salted keys?** → **One-time self-healing re-mint** (§4). Resolved.
