# Design — service-domain + service-location envelope-class migration (Row 112 follow-on)

**Status: ✅ self-ratified (Winston/Vertical Steward, 2026-06-29) — impl-level, no frozen-contract edit.**
Execution design for the remaining half of verticals.md Row 112 ("Service-instance modeling — envelope-class
discriminator"). The **lease-signing consumer already shipped** (`2a5087a`); this migrates the two **shared
packages** — `service-domain` (the templated model) + `service-location` (its template-reading consumer) — to
the same P7 envelope-class model. Once both land, the Lattice-lane **P7 `lint-conventions` gate can turn on
with no outliers.**

This doc exists because service-domain has **three wrinkles the lease-signing consumer never faced** (it had
no real templates and linked `instanceOf → DDL meta` directly). They are grounded in code below; the build is
otherwise a mechanical mirror of `2a5087a`.

## Why this is its own fire (not built inline)

The two packages are **atomically coupled**: dropping service-domain's `.class` aspect breaks service-location's
op-guards (which read it) **and** the premise of its lens template-guard. They must move in one commit to keep
`main` green. The code edits are concentrated (2 `ddls.go` + 1 lens) but the **test surface is large** (~100
assertion sites: `service_instance_test.go` 62, `service-location/integration_test.go` 26, `package_test.go`s,
`type_agnostic_test.go`, `verify-package-service-location.go`, and the leaseconvergence harness installs
service-domain). Security-plane (write-gate type authority) → **full 3-layer review** on the build fire.

## Grounded findings (the load-bearing decisions)

### Finding 1 — service-domain MUST use a two-hop instanceOf chain (instance → template → meta)

The step-6 resolver (`internal/processor/step6_resolve_ddl.go:152-162`) treats a vertex with **more than one
live `instanceOf` link as ambiguous → fails closed to the §1.5 permissive default** = the write-gate
regression the parent design §1.2 warns about. service-domain instances **already** carry
`instanceOf → template` (the business "this run is of that offering" link). So we **cannot** add a direct
`instance → service-DDL-meta` link (that would be a second `instanceOf` → ambiguous). The row's loose phrase
*"instanceOf → its service DDL meta"* read naively (instance→meta) is therefore **wrong for the instance**.

Instead, exactly **one `instanceOf` per vertex**, chained:

- **Template** root class `service.<fam>.template`, links **`instanceOf → ddl["service"].metaKey`** (the new
  type-authority link; templates carry none today).
- **Instance** root class `service.<fam>.instance`, **keeps `instanceOf → template`** (unchanged — the
  business link now *also* carries the type-authority chain).

Resolution (confirmed against `step6_resolve_ddl.go:106-150`, `maxInstanceOfHops=4`):

| Write | class lookup | walk | terminal | gate |
|---|---|---|---|---|
| CreateServiceTemplate (template root) | `service.<fam>.template` miss | template→meta | #1 `LookupByMetaKey` (vertexType) | `service` DDL permits CreateServiceTemplate ✓ |
| CreateServiceInstance (instance root) | `service.<fam>.instance` miss | instance→template→meta (2 hops) | #1 | permits CreateServiceInstance ✓ |
| RecordServiceOutcome (`.outcome` aspect) | class `outcome` miss → `vertexRootForResolve` = instance | instance→template→meta | #1 | permits RecordServiceOutcome ✓ |

The resolver explicitly supports the multi-hop walk (`step6_resolve_ddl.go:146` *"keep walking (instance →
template → type)"*). Hop 2 (template→meta) is resolved by the live link-reader prefix read, so it works even
though the instance-create op does not hydrate the template's `instanceOf` link.

### Finding 2 — NO aspect-type DDL is needed (unlike lease-signing)

lease-signing **added** `leaseServiceOutcome` / `leaseServiceDispatchMarker` aspect DDLs because its
`leaseServiceInstance` DDL permits **only** `CreateLeaseServiceInstance` — so an `.outcome` write walking to it
would fail closed. service-domain's **single `service` DDL permits all three** ops
(`PermittedCommands: [CreateServiceTemplate, CreateServiceInstance, RecordServiceOutcome]`,
`service-domain/ddls.go:81`). So the `.outcome` aspect write resolves via the chain to the `service` DDL,
which **permits** RecordServiceOutcome (`step6_validate.go:124-139` derives the class from the mutation
document and gates by the resolved DDL's permittedCommands). The `.outcome` write was ungated-permissive
before and is now positively gated by the `service` DDL — a strengthening, with **no new DDL**.

### Finding 3 — the service-location LENS template-guard must type-constrain the instanceOf target

`service-location/lenses.go:98` guards templates via `WHERE NOT (svc)-[:instanceOf]->(svcTpl)` — the old
premise being *"instances carry instanceOf (→template); templates never do."* **After migration templates DO
carry `instanceOf` (→meta)** → that predicate filters templates OUT and breaks the availableAt projection.

Fix: **type-constrain the target to `:service`** → `WHERE NOT (svc)-[:instanceOf]->(svcTpl:service)`. A node
label matches the **key-type OR envelope class** (`internal/refractor/ruleengine/full/executor.go:348`):

- instance → instanceOf → template (`vtx.service.*`, key-type `service`) → matches `:service` → excluded ✓
- template → instanceOf → meta (`vtx.meta.*`, key-type `meta`) → **no** match → `NOT(...)` true → included ✓

Pin with the package's lens-cypher test (a template projects serviceAccess; an instance does not).

### Enabling API (already on main — Fire E, `2a5087a`)

`ddl["<canonicalName>"].metaKey` exposes a DDL's meta-vertex key to its own script
(`starlark_runner.go:520`). A vertex's envelope class is readable as `getattr(state[key], "class")`
(`starlark_runner.go:469` — exposed on every state/`kv.Read` doc). Both are the exact mechanisms the
lease-signing consumer used (`lease-signing/scripts.go:606-608`).

## Exact edit list

### `packages/service-domain/ddls.go`
- **`CreateServiceTemplate`:** root `make_vtx(tpl_key, "service."+fam+".template", {})`; **drop** the
  `.class` aspect; **add** `instanceOf → ddl["service"].metaKey`:
  ```python
  meta_key = ddl["service"].metaKey
  _, meta_id = parts_of(meta_key, "typeAuthority", "meta")
  instance_of_lnk = "lnk.service." + tpl_id + ".instanceOf.meta." + meta_id
  # mutation: make_link(instance_of_lnk, tpl_key, meta_key, "instanceOf", "instanceOf", {})
  ```
  Keep `providedBy`. (`parts_of(..., "meta")` already enforces the 3-segment meta shape.)
- **`CreateServiceInstance`:** root `make_vtx(inst_key, "service."+fam+".instance", {})`; **drop** the
  `.class` aspect; **keep** the existing `instanceOf → template` and `providedTo` links unchanged. Switch the
  NotATemplate guard from `class_value(state, template)` → a new `vertex_class(state, template)` (envelope
  class), still `.endswith(".template")`.
- **`RecordServiceOutcome`:** switch the NotAnInstance guard `class_value(state, inst_key)` →
  `vertex_class(state, inst_key)`, still `.endswith(".instance")`. The `.outcome` aspect write + OCC are
  unchanged (Finding 2).
- **Helper:** replace `class_value(state, vtx_key)` (reads `vtx_key + ".class"` aspect) with
  `vertex_class(state, key)` returning `getattr(state[key], "class")` when alive (mirror
  `service-location/ddls.go:200-207`'s `class_of`). Remove the old `class_value`.
- **DDL self-description / Description / FieldDescription / Examples / package.go + ddls.go header comments:**
  retire every "`.class` aspect value `service.<x>.template|instance`" phrasing → "**envelope class**
  `service.<x>.template|instance`; instances link `instanceOf → template`, templates link
  `instanceOf → the service DDL meta` (the write-gate type authority)." Update the `family` field docs
  (no longer "sets the `.class` aspect value" — it sets the envelope class).

### `packages/service-location/ddls.go`
- **`require_live_service_template`:** drop the `class_aspect_value` read; require `class_of(state, key)` is
  non-None and `.endswith(".template")` (the envelope class is now the discriminator). Drop the
  `class_of != "service"` bare check (root class is now fine-grained).
- **`require_live_service`:** replace `class_of(state, key) != "service"` with
  `not class_of(state, key).startswith("service.")` (any service template/instance), None-safe.
- **Remove** the `class_aspect_value` helper (now unused). Update the header/DDL comments (the `.class` aspect
  is gone; the discriminator is the envelope class).
- **ContextHint.Reads:** these guards no longer need `<service>.class` in reads — only the service root (already
  read for liveness). Drop the `.class` aspect from the reads lists in the verify script + integration tests
  (and any caller that supplied it).

### `packages/service-location/lenses.go`
- Line 98 → `WHERE NOT (svc)-[:instanceOf]->(svcTpl:service)`. Rewrite the comment block (lines 64-94) to the
  Finding-3 reality (templates now carry `instanceOf→meta`; the guard is "no `instanceOf` to a `:service`
  target"; the `.class` aspect no longer exists).

### Tests + manifests
- `service-domain/service_instance_test.go`, `type_agnostic_test.go`, `package_test.go`: rebuild every
  fixture/assertion off the envelope-class shape — assert the instance/template carry envelope class
  `service.<fam>.{instance,template}` and **no `.class` aspect**; assert the template carries exactly one
  `instanceOf → vtx.meta.*` and the instance exactly one `instanceOf → vtx.service.<tpl>`; drive
  Create/Create/Record through the **real Processor** to prove the two-hop write-gate (this is the load-bearing
  proof — mirror `lease-signing`'s integration fixture). Keep the `type_agnostic` invariant scoped to engine
  production code (test harnesses that boot the vertical are type-aware).
- `service-location/integration_test.go`, `package_test.go`, `scripts/verify-package-service-location.go`:
  build templates via the migrated service-domain ops; drop the `.class` aspect from reads; add/keep the
  lens-cypher assertion (template projects, instance does not) pinning Finding 3.
- `internal/leaseconvergence/harness_test.go`: installs service-domain in the chain — verify it asserts nothing
  on the `.class` aspect (4 hits to inspect); the lease-signing convergence already discriminates by envelope
  class, so the harness should need only fixture-shape touch-ups if any.
- Bump `service-domain` + `service-location` manifest versions; sync `verify-package-*` exact-count assertions
  (service-domain stays 1 vertexType DDL / 3 ops — **no new DDL**, Finding 2).

## Gates (build fire)
`go build ./...`, `make vet`, `golangci-lint run ./...`, `STRICT=1 go run ./scripts/lint-conventions.go`,
`go test ./packages/service-domain/... ./packages/service-location/... ./internal/leaseconvergence/...`,
`make verify-package-service-location`, and the `make test-lease-convergence` e2e (service-domain is in its
install chain). **Full 3-layer review** (security-plane: write-gate type authority).

## Out of scope
No frozen-contract edit (Contract #1 §1.5/§1.6 + #2 §2.1 already ratified; P7 in lattice-architecture). Turning
**on** the P7 `lint-conventions` gate is **Lattice-lane** and happens after both packages land. `availableAt`
attaching to a *template* is unchanged (service-location already sources availableAt from templates).
