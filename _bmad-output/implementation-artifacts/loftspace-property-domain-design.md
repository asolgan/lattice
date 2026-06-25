# LoftSpace property domain — design proposal

> **Status:** 📐 awaiting-ratification + **needs a team/adversarial review before build** (authored in one
> Steward fire, 2026-06-25; not yet adversarially reviewed — the console proposal's review caught a blocker,
> so this L item must get the same before it ships). Backlog item: *Property / Unit / Listing domain +
> richer application* (LoftSpace, pkg + FE, ★★★ **L** — the PO's "biggest product gap").

---

## 1. The gap (PO-verified)

`vtx.leaseapp.<id>` is a bare shell: root `{}` + one `applicationFor` link → the applicant identity, plus a
`.signature` aspect. The vertical models the **workflow** (apply → checks → sign → converge) but not the
**thing being leased**. "What am I applying to lease?" is unanswerable: there is no property / unit /
listing, no rent, lease term, move-in date, applicant income/employment, co-applicants, or guarantor. An
applicant app (the next FE) has nothing concrete to render or collect.

## 2. Grounding — the model as it stands (do not redisturb)

- **`leaseapp` vertex** (`packages/lease-signing/ddls.go`): `class=leaseapp`, root `{}` (D5 — data in
  aspects/links). Ops `CreateLeaseApplication` (mints the vertex + `applicationFor`→identity link, validates
  the applicant alive) and `SignLease` (writes the `.signature` aspect).
- **Convergence lens** `leaseApplicationComplete` (`packages/lease-signing/lenses.go`): anchored on the
  `leaseapp`, walks `app -[:applicationFor]-> identity`, then `identity <-[:providedTo]- service` (bg-check /
  payment instances). Gaps: `missing_onboarding` (ssn), `missing_bgcheck`, `missing_payment`,
  `missing_signature`; `violating` = any open. This is the Weaver-driven engine — **untouched** by this design
  except for the additive walk in §4.
- **Link direction** (Contract #1 §1.1): the later-arriving vertex is the source. A `leaseapp` arriving after
  a pre-existing `unit` ⇒ `leaseapp` is source: `leaseapp -[appliesToUnit]-> unit`.
- **Aspect-hop projection** is available (`node.<aspect>.data.<field>`), so a lens can read unit fields
  directly (e.g. `unit.rent.data.amount`). [[reference_lens_aspect_projection]]
- **Packaging:** `leaseapp` is owned by `lease-signing`. A unit/listing is a **new domain** — see §3 D1.

## 3. Design decisions

**D1 — a new `loftspace-domain` package owning the leasable thing; `lease-signing` depends on it.**
Keep `lease-signing` as the *workflow* package; put the *inventory* (units/listings) in a new
`loftspace-domain` package. `lease-signing`'s `CreateLeaseApplication` gains an optional `unit` param and
writes the `appliesToUnit` link; the convergence lens walks it. Rationale: separation mirrors
`service-domain` (capability) vs `lease-signing` (workflow); a property catalog is reusable beyond one
application flow (search, availability, a landlord view). *Alternative considered:* fold units into
`lease-signing` — rejected (conflates inventory with workflow, blocks an independent property catalog).

**D2 — vertex shape.** `vtx.unit.<NanoID>`, `class=unit`, root `{}` (D5). Business data in aspects:
- `.address` `{line1, line2?, city, region, postal}`
- `.listing` `{rentAmount, rentCurrency, bedrooms, bathrooms, sqft?, availableFrom (RFC3339 date),
  leaseTermMonths, status ∈ available|pending|leased}`
- (optional later) `.media` → object-store pointers (photos) via the existing `objects-base` plane.

*Open:* `unit` vs `listing` as the type name, and whether a `property` (building) parent vertex is worth it
now (a `unit -[:inUnitOf]-> property` hop) or deferred. *Rec:* ship `unit` flat (no `property` parent) for
v1; add `property` only when a multi-unit building view is demanded.

**D3 — richer application detail (additive aspects on `leaseapp`).**
- `.terms` `{requestedRent?, moveInDate (RFC3339 date), leaseTermMonths}` — written by
  `CreateLeaseApplication` (or a new `SetApplicationTerms`).
- applicant `.employment` `{employer, role, monthlyIncome}` and the income figure are **sensitive** — they
  ride the same sensitivity convention as `identity`'s `ssn`/`dob` (written by the onboarding op,
  `RecordIdentityPII` or a sibling). They live on the **identity**, not the leaseapp (an applicant's income
  is an attribute of the person, reused across applications) — consistent with where `ssn`/`dob` already sit.

**D4 — co-applicants / guarantor as links (not fields).**
`leaseapp -[coApplicant]-> identity` (0..n) and `leaseapp -[guarantor]-> identity` (0..1), each the
later-arriving-source direction. The convergence lens can later require each co-applicant to clear its own
checks; v1 may treat co-applicants as informational and gate only the primary. *Rec:* model the links in v1,
gate only the primary applicant; expand the lens to co-applicant checks in a follow-on.

**D5 — anchor the application to a real unit + surface "what am I leasing".**
`CreateLeaseApplication(applicant, unit)` writes `appliesToUnit`. The convergence lens adds
`OPTIONAL MATCH (app)-[:appliesToUnit]->(u:unit)` and projects `unitKey`, `unitAddress`
(`u.address.data.line1`), `unitRent` (`u.listing.data.rentAmount`) onto the row — so the operator/FE row
answers "applying to lease Unit X at $Y/mo." **Optional gap:** `missing_unit` (`unitKey = null`) — a real
application should name a unit. *Rec:* add `missing_unit` so an application without a unit is non-converging
(forces the data to exist); flag for Andrew since it changes convergence semantics for existing flows.

**D6 — availability / double-book is OUT of scope here** (it's the separate ★★★ "Appointment scheduling /
slot-uniqueness" class of problem, and Capability-KV §06 defers temporal/uniqueness enforcement). v1 sets
`listing.status` by hand; a `LeaseUnit` op flipping `available→leased` on signature is a clean follow-on.
*Flag:* do not silently imply this design enforces single-occupancy.

## 4. Convergence-lens change (additive)

```cypher
MATCH (app:leaseapp {key: $actorKey})
OPTIONAL MATCH (app)-[:applicationFor]->(id:identity)
OPTIONAL MATCH (app)-[:appliesToUnit]->(u:unit)          // NEW
OPTIONAL MATCH (id)<-[:providedTo]-(inst:service)
... (existing bg/payment/signature gap logic unchanged) ...
RETURN
  ... existing columns ...,
  u.key                  AS unitKey,                      // NEW — what is being leased
  u.address.data.line1   AS unitAddress,                  // NEW
  u.listing.data.rentAmount AS unitRent,                  // NEW
  (u.key = null)         AS missing_unit,                 // NEW (optional gap — see D5)
  (... OR (u.key = null)) AS violating                    // only if missing_unit is adopted
```
Null-safe (absent unit ⇒ null columns). Additive columns — no consumer breaks (Contract #06 value-shape rule).

## 5. Increments

1. **`loftspace-domain` package** — the `unit` vertex DDL (`.address`/`.listing` aspects), a
   `CreateUnit` / `SetListing` op, permissions, manifest, `make verify-package-loftspace-domain` + unit tests.
   (Design-ratified → L2-buildable; M.)
2. **`lease-signing` integration** — `CreateLeaseApplication` takes `unit`, writes `appliesToUnit`; `.terms`
   aspect; the convergence-lens walk + (optional) `missing_unit`. Refractor convergence e2e extended.
   (Contract-adjacent only via the lens row shape, which is additive; M.)
3. **Applicant FE** (the separate ★★★ L item, now unblocked) — intake form that picks a unit + collects
   terms, "what I'm leasing" on the status tracker, the task inbox (shipped), document upload (objects-base).

## 6. Contract / frozen-surface check

- **New vertex type `unit`** follows Contract #1 key-shape (`vtx.unit.<NanoID>`) — package-authored, **no
  frozen-contract edit**. New links (`appliesToUnit`/`coApplicant`/`guarantor`) follow the 6-segment shape +
  the sentence/direction rule.
- The convergence-lens row gains additive columns — §06 says value-shape additions are fine; the Weaver
  target row is FROZEN as *one-row-per-candidate with a `violating` flag* (§10.2), which this preserves.
- **`missing_unit` (D5) changes convergence semantics** (an application with no unit becomes non-converging)
  — the one item that wants Andrew's explicit yes/no, since it affects the existing happy path.

## 7. Open questions for Andrew

1. `unit` vs `listing` type name; flat `unit` vs a `property` (building) parent now? *Rec: flat `unit`, no
   parent, v1.*
2. Adopt `missing_unit` as a convergence gap (D5)? *Rec: yes — a real application names a unit — but it
   changes existing-flow semantics, so your call.*
3. Income/employment on the **identity** (reused across applications, sensitive like ssn/dob) vs on the
   leaseapp? *Rec: on the identity.*
4. Scope of co-applicant gating in v1 (model links, gate primary only) — confirm. *Rec: yes.*

## 8. Not in scope (guardrails)

- Availability / double-book / slot-uniqueness (separate ★★★ item; §06 defers it).
- No frozen-contract edit. No read-path auth / per-user scoping (trusted-tool posture holds).
- This proposal is **not build-ready** until it has had a team/adversarial review (the L-item bar).
