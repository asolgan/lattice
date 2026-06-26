# clinic-domain — design proposal (Increment 1: the bookable domain)

> **Status:** ✅ **BUILT + 3-layer-reviewed + gates green (Increment 1).** No frozen-contract change, no
> Andrew gate. Authored + built 2026-06-25 (Steward). The 3-layer adversarial review returned **Blind
> Hunter: SHIP IT · Edge Case Hunter: SHIP IT · Acceptance Auditor: ACCEPT** (no CRITICAL/MAJOR defects).
> Triage (Winston): the two Edge-Case flags are intentional — (1) **non-cascading tombstone** is the
> platform-wide deferred owner-cascade ("Andrew's GC domain", objects-base §19); the lens hides orphans by
> anchoring on the live root → documented in `package.go`, no bespoke cascade built; (2) the **lens 0..1
> cardinality** is op-enforced (CreateOnly links, no second-link op) → documented in `lenses.go`, no cypher
> fan-out guard (it would reintroduce the avoided WITH-drop for an op-impossible state). `patient!=provider`
> is structurally impossible (distinct type segments) → no dead-code guard. The `endsAt` commit-level
> assertion was added. Gates: `go build ./...`, `go vet`, `golangci-lint` (0), STRICT `lint-conventions`
> (0/P5), `gofmt`, `go test ./packages/clinic-domain/...` (all green). The PO filed the gap (Vertical demand backlog — *`clinic-domain`
> package — patient / provider / appointment / visit model*, Clinic, pkg + FE, ★★★ **L**); every design
> question here is implementation-level and resolved by Winston grounded in the proven domain-package
> precedents (`location-domain`, `loftspace-domain`, `lease-signing`). The PHI/Vault, temporal-availability,
> and `@every` concerns are the *separate* deferred items this vertical forces — explicitly OUT of scope
> here (§3 D5/D6/D7), exactly as `loftspace-domain` deferred availability/double-book (its D6).

---

## 1. The gap (PO-verified)

The clinic vertical has **zero domain** (`ls packages/` carries no clinic package). Nothing is bookable:
no patient, no provider, no appointment/slot, no visit/encounter, no scheduling. Like LoftSpace's
once-missing property domain, "what am I booking, with whom, when?" is unanswerable, and a clinic FE
(patient self-booking, provider schedule) has nothing concrete to render. This is the **foundation for
every clinic flow** — the second showcase vertical and the platform's forcing function for the deferred
`@every` / temporal-availability / Vault work (agentic-ops-design §5).

## 2. Grounding — the proven domain-package idioms (do not reinvent)

- **`location-domain`** (`packages/location-domain/ddls.go`): a vertexType DDL whose Starlark **mints** a
  vertex (`make_vtx`, `nanoid.new()` or a bare-NanoID seam) and **wires links** (`make_link`) after
  validating both endpoints alive + class by the keys the caller lists in `ContextHint.Reads`
  (known-key-reads discipline — no prefix scans). Link direction = the **later-arriving vertex is the
  source** (Contract #1 §1.1).
- **`loftspace-domain`** (`packages/loftspace-domain/`): **aspectType DDLs** as step-6 write gates +
  `node.<aspect>.data.<field>` aspect-hop projection + a **projection lens** into a NATS-KV read-model
  bucket (the **P5** application query surface — `availableListings` / `applicantRoster`).
- **`lease-signing`** (`packages/lease-signing/scripts.go`): one op **atomically mints a vertex + writes
  an aspect + writes links** in a single mutation batch (`CreateLeaseApplication` → leaseapp vertex +
  `applicationFor` + `appliesToUnit` + optional `.terms`). The precise template for `CreateAppointment`.
- **Step-6 gating (verified, `internal/processor/step6_validate.go`):** *permissive by default* (Contract
  #1 §1.5/§1.6) — a mutation whose `class` has **no DDL** passes freely. A class DDL is declared only
  where we want to **gate** the write (permittedCommands) or **document** the shape. lease-signing writes
  `.signature`/`.terms` with NO aspect DDL (permissive); loftspace **chose** to declare `.listing`/`.address`
  aspect DDLs (gate + self-doc). clinic-domain follows the **loftspace convention** (declare the aspect
  DDLs) for rigor + self-documentation.
- **Sensitive-aspect scope (verified, step-6):** a `sensitive: true` aspect may attach **only to an
  `identity`-typed vertex**. A sensitive aspect on a `vtx.patient` is *rejected*. ⇒ all Increment-1
  clinic aspects are **non-sensitive** (D5), which is exactly right because PHI handling is the *deferred
  Vault plane*, not this increment.
- **Cypher node label (verified, `executor.go` `nodeMatches`):** resolves from the **key type segment**
  first (`vtx.<type>.<id>`), then `class`, then a `label` prop. So `MATCH (a:appointment)` matches the
  `vtx.appointment.<id>` type segment — the entities get **distinct type segments** the lens anchors on.

## 3. Design decisions (all implementation-level — resolved by Winston, no Andrew gate)

**D1 — `clinic-domain` is a self-contained domain package owning THREE vertex types** (`patient`,
`provider`, `appointment`), mirroring `location-domain`'s "own your domain's vertex types." It does **not**
reuse `vtx.identity` for the people. Rationale: matches the PO's explicit ask; avoids premature
identity-domain / rbac-domain coupling; under the trusted-tool model (no read-path auth) a patient/provider
need not be a system actor. Trade-off: PHI can't be a `sensitive` aspect on a non-identity vertex (D5) —
acceptable because PHI is the deferred Vault item. *If a patient later must log in, that is an additive
`vtx.identity` + an `identifiedBy` link — not a rework.*

**D2 — vertex shapes (Contract #1 §1.1 + D5 minimal root).**
`vtx.patient.<NanoID>` class=`patient`, `vtx.provider.<NanoID>` class=`provider`,
`vtx.appointment.<NanoID>` class=`appointment` — all root data `{}` (data lives in aspects/links). The
class equals the type name (one vertexType DDL per entity; step-6 keys permittedCommands on the mutation's
class). Each create op accepts an optional bare-NanoID write-ahead seam (`patientId`/`providerId`/
`appointmentId`), mirroring `leaseAppId`.

**D3 — aspects (declared aspectType DDLs = step-6 write gates, loftspace convention).**
- patient `.demographics` `{fullName, dob? (RFC3339 date), email?, phone?}` — written by `CreatePatient`.
- provider `.profile` `{fullName, specialty, credentials?, bio?}` — written by `CreateProvider`.
- appointment `.schedule` `{startsAt (RFC3339), endsAt (RFC3339), reason?}` — written by `CreateAppointment`.
- appointment `.status` `{value ∈ scheduled|confirmed|completed|cancelled|noShow}` — written by
  `CreateAppointment` (initial `scheduled`) AND `SetAppointmentStatus` (transitions). **Status is its own
  aspect** (not folded into `.schedule`) so `SetAppointmentStatus` is a clean unconditioned upsert with no
  read-merge — the `SetListing` idiom.

**D4 — links (Contract #1 §1.1 — the later-arriving appointment is the source).** The appointment is
created after the patient + provider exist, so it is the source of both:
- `appointment -[forPatient]-> patient` (sentence test: "appointment forPatient patient" ✓), 0..1.
- `appointment -[withProvider]-> provider` ("appointment withProvider provider" ✓), 0..1.
Both 0..1 from the appointment ⇒ the projection lens stays **one row per appointment** (0..1 × 0..1 = 1),
no fan-out, no output-key collision. `CreateAppointment` validates both endpoints alive + class (a non-live
or wrong-class patient/provider is never wired — the `location-domain` endpoint-class guard).

**D5 — PHI / sensitivity is DEFERRED to the Vault plane (OUT of this increment).** All Increment-1
aspects are **non-sensitive**. Patient DOB / contact are stored plain. This is consistent with the board
framing clinic as *the demand driver for the deferred Vault / crypto-shred plane* (not its implementation),
with the trusted-tool posture (no read-path auth — everything is readable regardless), and with step-6
forbidding a sensitive aspect on a non-identity vertex (D1). **Flagged loudly in code + docs:** real PHI
(DOB, diagnoses, medical history) handling + right-to-be-forgotten = the Vault item, and clinic
patient-record deletion is its validating flow.

**D6 — availability / double-book / provider-hours is OUT of scope here** (the separate ★★★ "Appointment
scheduling — conflict + temporal availability" item; Capability-KV §06 L354–359 explicitly defers
temporal/uniqueness). `CreateAppointment` records a *requested* time; it does **NOT** reject an overlapping
slot or an out-of-hours time. `SetAppointmentStatus` is a hand-driven transition. *Guardrail (mirrors
loftspace D6): do not imply this design enforces single-occupancy / no-double-book.*

**D7 — recurring `@every` schedules OUT** (the separate ★★★ platform item — `@every` has no consumer;
§10.4 ships `@at` one-shot). Appointment reminders / recurring availability are a follow-on that forces
that platform work.

**D8 — Increment-1 lens is a PROJECTION, not a Weaver convergence lens.** The PO asked for "a lens that
walks patient↔appointment↔provider" — Increment 1 ships that as a **projection** read model
(`clinicAppointments`), the **P5** query surface a future clinic FE reads (one row per appointment, joined
to patient + provider for display: "my appointments" / "provider schedule"). A Weaver-driven **convergence**
lens (gaps → dispatch, e.g. appointment-confirmation or reminder orchestration) is a follow-on once the
clinic has an orchestrated workflow (it ties into D7's `@every`). A second projection lens
`clinicProviders` (the provider roster) feeds the "pick a provider" booking UI — the `applicantRoster`
idiom.

## 4. Ops (Increment 1)

| Op | DDL | Writes | Validates |
|---|---|---|---|
| `CreatePatient` | patient | `vtx.patient.<id>` (`{}`) + `.demographics` | fullName required |
| `TombstonePatient` | patient | tombstone the patient vertex | alive |
| `CreateProvider` | provider | `vtx.provider.<id>` (`{}`) + `.profile` | fullName + specialty required |
| `TombstoneProvider` | provider | tombstone the provider vertex | alive |
| `CreateAppointment` | appointment | `vtx.appointment.<id>` (`{}`) + `.schedule` + `.status{scheduled}` + `forPatient` + `withProvider` | startsAt+endsAt required; patient+provider alive + class |
| `SetAppointmentStatus` | appointment | upsert `.status{value}` | appointment alive + class; value ∈ enum |
| `TombstoneAppointment` | appointment | tombstone the appointment vertex | alive |

Known-key reads only (the caller lists endpoints in `ContextHint.Reads`; the ops alive-check by those
keys — no prefix scans, the `TestPackage_NoScans` guard). Creates use `"op": "create"` (CreateOnly — a
crash-retry with the same id collapses on the Contract #4 tracker); `SetAppointmentStatus` uses
`"op": "update"` (unconditioned upsert — re-publishable).

## 5. Lenses (Increment 1 — projection read models, P5 query surface)

```cypher
-- clinicAppointments → bucket "clinic-appointments" (one row per appointment)
MATCH (a:appointment)
OPTIONAL MATCH (a)-[:forPatient]->(p:patient)
OPTIONAL MATCH (a)-[:withProvider]->(pr:provider)
RETURN
  a.key AS key,                            -- IntoKey default: keyed by appointment key
  a.key AS appointmentKey,
  a.schedule.data.startsAt AS startsAt,
  a.schedule.data.endsAt   AS endsAt,
  a.schedule.data.reason   AS reason,
  a.status.data.value      AS status,
  p.key AS patientKey,
  p.demographics.data.fullName AS patientName,   -- neighbor aspect-hop (lease-signing reads id.ssn.data.value the same way)
  pr.key AS providerKey,
  pr.profile.data.fullName  AS providerName,
  pr.profile.data.specialty AS providerSpecialty
```
```cypher
-- clinicProviders → bucket "clinic-providers" (one row per named provider — the booking picker)
MATCH (pr:provider)
WHERE pr.profile.data.fullName <> null
RETURN
  pr.key AS key,
  pr.key AS providerKey,
  pr.profile.data.fullName  AS name,
  pr.profile.data.specialty AS specialty,
  pr.profile.data.credentials AS credentials
```

- **No `WITH`** in either lens (flat projections, no aggregation) — so the §4-B1 loftspace "WITH-drop"
  hazard does not apply; OPTIONAL-matched neighbor bindings (`p`, `pr`) are live directly in `RETURN`.
  The `lens_cypher_test` (real engine) pins this end-to-end.
- One row per appointment (0..1 links) — the §10.2 one-row-per-anchor shape holds. Null columns when a
  patient/provider link is absent (the reader treats them as absent).
- The bucket names (`clinic-appointments`, `clinic-providers`) are exported Go consts (the P5 contract the
  future clinic FE reads via `KVGet`/`KVListKeys`, never Core KV).

## 6. Files (mirrors loftspace-domain layout)

`packages/clinic-domain/`: `package.go` (Definition), `ddls.go` (3 vertexType + 4 aspectType DDLs +
scripts), `lenses.go` (2 projection lenses + bucket consts), `permissions.go` (7 ops → operator),
`manifest.yaml`, `package_test.go` (manifest↔Definition drift, DDL pinning, non-sensitive assertions,
no-scans, script guards), `integration_test.go` (install + drive each op through the real Processor +
assert the lens projects). Plus `scripts/verify-package-clinic-domain.go` + a `make
verify-package-clinic-domain` target + the package registered for install.

## 7. Contract / frozen-surface check

- **No frozen-contract edit.** Three new vertex types + four aspect types + two links — all
  **package-authored, permissive-by-default** (Contract #1 §1.5/§1.6). New links follow the 6-segment
  shape + the sentence/direction rule (D4). New ops are package-local Starlark.
- **Permissions** grant the 7 ops to the `operator` role (scope `any`) — identical to loftspace-domain;
  no new capability surface, no authz hole (the trusted-tool operator already holds standing permission).
- **No P5/P2 violation:** the package writes via ops (P2 — Processor is the sole writer) and exposes its
  query surface as **lens projections** (P5). No Core-KV reads anywhere.
- **Nothing here needs Andrew** — package-owned, additive, revertible. The PHI/Vault, temporal-availability,
  and `@every` items it *forces* are separate deferred backlog rows (§3 D5/D6/D7), each its own future
  fire.

## 8. Not in scope (guardrails)

- PHI/sensitive aspects + Vault/crypto-shred (D5 — the deferred Privacy/Vault item; clinic is its forcing
  function, not its implementation).
- Availability / double-book / slot-uniqueness / provider-hours (D6 — separate ★★★ item; §06 defers it).
- Recurring `@every` reminders / availability (D7 — separate platform item).
- A Weaver convergence lens / orchestrated clinic workflow (D8 — Increment-1 ships projection lenses).
- The clinic FE (the separate ★★★ L item, now unblocked by this foundation — UX-then-FE, a later fire).
- No read-path auth / per-user scoping (trusted-tool posture holds).

## 9. Review plan

L-sized net-new package with DDLs (capability-plane-adjacent) → **3-layer adversarial `bmad-code-review`**
(Blind Hunter + Edge Case Hunter + Acceptance Auditor) after build, per the loop. Gates:
`go build ./...`, `make vet`, `golangci-lint run ./...`, `STRICT=1 go run ./scripts/lint-conventions.go`
(P5 + conventions), `go test ./packages/clinic-domain/...`, `make verify-package-clinic-domain`.
