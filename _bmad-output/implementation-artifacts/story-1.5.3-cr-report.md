# Story 1.5.3 — UpdateMetaVertex expansion · CR Report

**Reviewer:** Adversarial CR sub-agent · **Date:** 2026-05-29
**Scope reviewed:** `internal/bootstrap/meta_ddl.go` (Update branch only), `internal/bootstrap/update_metavertex_test.go` (new), `docs/components/processor.md` (new subsection + pairing-table row).
**Verification:** `go test ./internal/bootstrap/... -run UpdateMetaVertex -count=1` → `ok` (re-ran locally, green).

## Triage summary

- **P0:** 0
- **P1:** 0
- **P2:** 2 (F-1 element-type parity gap; F-2 null-prior un-submittable rollback)
- **Nit:** 1 (F-3 doc wording)

Headline: the implementation is sound and matches the LOCKED design. `getattr(root, "class")` is correct, OCC `mutations[0]` is provably the first canonical-order field, metaKey/canonicalName immutability and the no-blanking fix all hold, and Create/Tombstone are untouched. Two non-blocking P2s: (1) Update's `permittedCommands` skips the per-element string check that Create enforces; (2) a `null` prior value captured into the compensation `payloadTemplate` would make the rollback `UpdateMetaVertex` un-submittable — low probability for Create'd aspects but reachable and demonstrated by the test suite.

---

## P0 — none

No correctness defect that breaks the happy path, the spec's locked decisions, or the existing gate4 rollback round-trip. Build/vet/tests reported green by implementer and confirmed for the new tests.

## P1 — none

No data-loss, identity-break, or OCC-soundness defect found. Specifically verified safe:

- **Concern 1 — `getattr(root, "class")`:** Correct. The `state` entry is a `starlarkstruct.Struct` whose `StringDict` always includes `"class"` (`starlark_runner.go:393-405`, set unconditionally). `class` is a Starlark keyword, so dotted `root.class` would be a parse error — `getattr(root, "class")` is the right escape hatch and resolves the struct field at runtime. `is_lens = class == "meta.lens"` correctly distinguishes lens from the four `meta.ddl.*` classes. The `class`-absent → `""` → DDL default is safe because a known-alive vertex root always carries `class` (it's a non-optional field in the conversion).
- **Concern 2 — OCC `mutations[0]`:** Provably the first *field* mutation. `add_string_field("description", …)` is always appended first (description applies to both classes), then the remaining DDL/lens fields in canonical order, then `.compensation` is appended **last**. So `mutations[0]` is the first present field in canonical order and is never `.compensation`. For lens the append order is description→spec, consistent with §3.4's canonical order (DDL-only fields can't be present on a lens because `is_lens` gates them out). `force` bypass and non-integer rejection both intact and tested.
- **Concern 4 — empty/canonicalName-only:** Both reach `len(mutations) == 0 → fail("…no updatable fields provided")`. No code path ever writes `.canonicalName`; canonicalName is never read into any `add_*` call. Tested (`EmptyUpdateRejected`, `CanonicalNameIgnored`).
- **Concern 6 — no blanking:** Absent field → `hasattr` false → skipped. The old `desc = ""` fallback is gone. No `""`-write path remains.
- **Concern 7 — scope:** Diff confirms Create and Tombstone branches are byte-identical to prior; only the Update branch + doc block changed. No `nanoid.new()` anywhere in the Update branch — metaKey is read verbatim from payload and all mutation keys are rooted at it (tested `MetaKeyPreserved`).
- **Concern 3 (lens spec round-trip):** The Create-lens `spec` validation (`json.decode` + cypherRule/targetType/targetConfig presence) is reproduced exactly in the Update lens path; decoded dict stored verbatim as `.spec`; prior `.spec` re-encoded via `json.encode` to a JSON string so the compensating op resubmits a valid `spec` string. Round-trips cleanly (tested `LensSpecAndDescription`). See F-2 below for the null-prior sub-case.

---

## P2 Findings

### [F-1] Update `permittedCommands` omits the per-element string check Create enforces — validation parity gap

**File:** `internal/bootstrap/meta_ddl.go:262-269` (`add_list_field`) used at `:313` for `permittedCommands`; cf. Create `:133-138`.

**What:** The Create branch validates `permittedCommands` is a list **and** that every element is a string (`:136-138`, "each entry must be a string"). The Update branch routes `permittedCommands` through the generic `add_list_field`, which only checks `type(v) == type([])`. An `UpdateMetaVertex` with `permittedCommands: [1, 2, {"x":1}]` would be accepted and written to the `.permittedCommands` aspect — a shape Create would reject.

**Why it matters:** Spec §3.1 / §6 require "reuses the same helpers/shapes the Create branch uses so Update and Create stay shape-identical." This is the one field where Update is strictly more permissive than Create. A malformed `permittedCommands` aspect would later be read by the Processor's DDL cache as the authoritative permitted-command set; non-string entries could cause a downstream type assertion or silently disable command gating for that meta-vertex.

**Suggested fix:** Add an element-type guard in the `permittedCommands` path (either a dedicated `add_string_list_field` or an inline loop mirroring Create's `:136-138`):
```starlark
for c in v:
    if type(c) != type(""):
        fail("InvalidArgument: permittedCommands: each entry must be a string")
```
(`examples`/`fieldDescription` have no element constraint in Create — `required_list`/`required_dict` only — so those are already at parity; no change needed there.)

### [F-2] `null` prior value in compensation `payloadTemplate` yields an un-submittable rollback op

**File:** `internal/bootstrap/meta_ddl.go:260, 269, 278, 310` (`prior_payload[field] = prior_data_field(...)` / `prior_spec` may be `None`); replay path confirmed in `internal/aiagent/gate4_rollback_test.go:304-318`.

**What:** When a changed field's prior aspect is absent or malformed in `state`, `prior_data_field` returns `None` and the field is recorded into the compensation `payloadTemplate` as JSON `null` (test `PriorValueMissingTreatedAsNil` demonstrates `payloadTemplate.description == nil`). Rollback is performed by resubmitting `UpdateMetaVertex` with that template as the payload (per the gate4 round-trip). On replay, `hasattr(p, "description")` is **true** (a JSON `null` becomes `starlark.None`, and the key is present in the payload struct — confirmed via `goValueToStarlark`/`operationEnvelopeToStarlark`), so `add_string_field` hits `v == None → fail("…required non-empty string")`. The compensating op is therefore **un-submittable** — the rollback for that field can never be applied.

**Why it matters:** It defeats the purpose of the compensation aspect for exactly the edge case it should cover. Probability is low: any aspect created via `CreateMetaVertex` always exists, so for correctly-Created vertices with the field declared in `ContextHint.Reads`, the prior is never null. But it **is** reachable if (a) the caller forgets to declare `metaKey+".<field>"` in Reads (state lacks the aspect → null), or (b) a field aspect was never created for that class. The current design silently bakes a poison payload into `.compensation` rather than failing the forward op or omitting the field.

**Suggested fix (pick one, document the choice):**
- Preferred: if `prior_data_field` is `None` for a field being changed, **fail the forward op** (e.g. `fail("FailedPrecondition: <field>: prior value not in state — declare metaKey+\".<field>\" in ContextHint.Reads")`). This converts a latent un-rollbackable mutation into an immediate, actionable error and enforces the §3.3 Reads requirement at runtime.
- Alternative: omit a field from `payloadTemplate` when its prior is `None` (rollback restores only the fields it can). Weaker — silently drops rollback coverage — and still leaves the field changed with no inverse.

Either way, add a test asserting the chosen behavior on a missing-prior compensation *replay* (current `PriorValueMissingTreatedAsNil` only asserts the null is captured, not that the resulting rollback is submittable).

---

## Nit

### [F-3] Doc: `fieldDescription` validation labeled "object" while code error says "required object" but spec table says "dict"

**File:** `docs/components/processor.md` field-set table (fieldDescription row) vs spec §3.1 ("dict").

Cosmetic only — "object" (doc) and "dict" (spec) are the same JSON shape and the code's `add_dict_field` error message says "required object". No behavioral mismatch. Optional: align terminology to the spec's "dict" or note both. Otherwise the new subsection is accurate: updatable sets, metaKey stability, canonicalName immutability, empty-update rejection, `ContextHint.Reads` requirement, the `null` guarded-read note, lens spec re-encode, and the OCC single-aspect / Phase-2 limitation all match the implementation.

---

## Coverage notes (looked for, found clean)

- **Strip parity:** Update `add_string_field` strips before write and length-checks the stripped value, matching Create's `required_string`. Consistent.
- **Lens DDL-field bleed:** A lens payload carrying `script`/`examples` etc. is correctly ignored (gated by `is_lens`), since `target_class` is read from real state, not the payload.
- **expectedRevision applied to compensation:** Explicitly excluded; tested that `.compensation.ExpectedRevision == nil`.
- **`spec` prior re-encode:** `json.encode(sd.data)` round-trips; rollback decodes back to a valid spec string (tested).
- **Tombstone untouched:** Confirmed no edits to the Tombstone branch (Story 1.5.2's territory).
