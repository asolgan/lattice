# LoftSpace property domain — design proposal

> **Status:** ✅ **Decisions resolved (Winston + PO) · adversarially reviewed + hardened · ready for build
> (increment 1).** No frozen-contract change, no Andrew gate. Authored + reviewed 2026-06-25. The product
> questions were the PO's (resolved §7); the design questions were Winston's; an adversarial review caught a
> blocker (the §4 `WITH`-drop) + several issues, all folded below. Backlog item: *Property / Unit / Listing
> domain + richer application* (LoftSpace, pkg + FE, ★★★ **L** — the PO's "biggest product gap").
>
> **Build-grounding correction (2026-06-25, supersedes the original D1/D2):** `location-domain` **already
> owns `vtx.unit.<id>`** (class=`location` — the physical *place*, root `{}`, containment via `containedIn`,
> but NO economics). A new `vtx.unit` (class=`unit`) would **collide** on the key namespace. So
> `loftspace-domain` **depends on `location-domain`** and adds the listing as **aspects on the existing
> place-unit** — `.listing` (rent/term/availability) + `.address` — via aspect-type DDLs + `SetListing` /
> `SetUnitAddress` ops. **No new vertex type** (simpler + reuses the place graph; containment to
> building/property comes free). The unit is minted by `location-domain`'s `CreateLocation(locationType=unit)`.
> `appliesToUnit` → the location `vtx.unit`; the convergence lens matches `(u:unit)` and reads
> `u.listing.data.rentAmount` / `u.address.data.line1`. D1/D2/§4–§6 below are updated accordingly. This needs
> a quick re-review of the changed sections before build (the cypher/B1 fix still stands).
>
> **Review changelog (folded):** B1 §4 cypher dropped `u` across the existing `WITH` → rewritten to carry the
> unit fields through the `WITH` (§4). M1 a `missing_`-prefixed gap with no remediation op wedges Weaver →
> `unit` is now **required at create**, no gap column (§3 D5, §7 Q2). M2 new columns need an explicit
> `Output.BodyColumns` add (§5). M3 the `appliesToUnit` link DDL is assigned to `lease-signing` + install
> order stated (§3 D1, §5). C3 link names → `hasCoApplicant`/`hasGuarantor` (sentence test) (§3 D4). C4
> `.employment`/`.income` need a `sensitive:true` aspect DDL + must stay off the projection plane (§3 D3). C6
> `CreateUnit` gets a `unitId` write-ahead seam (§3 D2). C7 `CreateLeaseApplication` must alive-check the unit
> + add it to `Reads` (§4/§5). C5 the lens stays `unit.listing.status`-blind in v1 (§3 D6).

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

**D1 — `loftspace-domain` adds the *listing* economics to `location-domain`'s existing place-unit; it does
NOT introduce a vertex type.** (Corrected — see the build-grounding note above.) `location-domain` owns
`vtx.unit.<id>` (class=`location`): the place, with `containedIn` to building/property, but root `{}` and no
economics. `loftspace-domain` **depends on `location-domain`** and contributes the leasable facets as aspects
on that same unit:
- declares the `.listing` + `.address` aspect-type DDLs,
- provides `SetListing` / `SetUnitAddress` ops that write them on a `vtx.unit`,
- the unit itself is minted by `location-domain`'s `CreateLocation(locationType=unit)`.

`lease-signing`'s `CreateLeaseApplication` gains a **required** `unit` param (§7 Q2) → writes the
`appliesToUnit` link to that `vtx.unit`; the convergence lens walks it.

- **Link-DDL ownership + install order (review M3):** the `appliesToUnit` link DDL (canonicalName globally
  unique per Contract #1 §1.5) is declared by **`lease-signing`** — the verb is the application's. Install is
  warn-and-proceed (not order-enforcing), so the order **`location-domain` → `loftspace-domain` →
  `service-domain` → `lease-signing`** is a hard requirement, stated in the manifests' `depends` and wired
  into `make install-loftspace`. `CreateLeaseApplication` reads the `unit` key (`ContextHint.Reads`) and
  **alive-checks it** (C7).

**D2 — aspects on the existing `location` unit (no new vertex type).** The unit is `vtx.unit.<NanoID>`,
`class=location` (location-domain), root `{}` (D5). `loftspace-domain` declares two aspect-type DDLs written
by its ops onto that unit:
- `.address` `{line1, line2?, city, region, postal}` — `SetUnitAddress`.
- `.listing` `{rentAmount, rentCurrency, bedrooms, bathrooms?, sqft?, availableFrom (RFC3339 date),
  leaseTermMonths, status ∈ available|pending|leased}` — `SetListing`.
- (later) `.media` → object-store pointers (photos) via the existing `objects-base` plane.

**PO-decided (§7 Q1):** **flat** unit (no `property` parent) for v1 — and the place-unit already supports a
`property` parent for free via location-domain's `containedIn`, so a landlord/building view is a later lens,
not a schema change.

**No write-ahead seam to add here (supersedes review C6):** the unit is minted by location-domain's
`CreateLocation`, which already has the optional `locationId` bare-NanoID seam. `SetListing`/`SetUnitAddress`
operate on an existing unit key, so they need none.

**D3 — richer application detail (additive aspects on `leaseapp`).**
- `.terms` `{requestedRent?, moveInDate (RFC3339 date), leaseTermMonths}` — written by
  `CreateLeaseApplication` (or a new `SetApplicationTerms`).
- applicant `.employment` `{employer, role, monthlyIncome}` lives on the **identity**, not the leaseapp (an
  applicant's income is an attribute of the person, reused across applications) — consistent with where
  `ssn`/`dob` already sit (Winston's call, §7 Q3). **Sensitivity must be explicit (review C4):** `sensitive`
  is enforced only when a DDL declares it (Contract #1 §1.6), so `.employment` needs an aspect-type DDL with
  `sensitive: true` in **identity-domain**, and `monthlyIncome` **must not be projected** into the
  convergence row (the lease vertical's existing precedent of keeping sensitive results off the projection
  plane — cf. the `.outcome.result` field).

**D4 — co-applicants / guarantor as links (not fields).**
`leaseapp -[hasCoApplicant]-> identity` (0..n) and `leaseapp -[hasGuarantor]-> identity` (0..1), each the
later-arriving-source direction (leaseapp is source). Names pass the sentence test "application
hasCoApplicant identity" (review C3 — `coApplicant`/`guarantor` failed it). **PO-decided (§7 Q4):** v1 models
the links but gates **only the primary applicant**'s checks; co-applicants are informational. Co-applicant
gating is a follow-on. **Constraint (review C2):** do NOT project these multi-links as fan-out columns in the
convergence lens, or the row multiplies and trips the output-key-collision guard (§10.2 is one-row-per-anchor);
the v1 `appliesToUnit` is 0..1 so it is safe.

**D5 — anchor the application to a real unit + surface "what am I leasing" (PO Q2 + review B1/M1).**
`CreateLeaseApplication(applicant, unit)` **requires** `unit`, alive-checks it, and writes `appliesToUnit`.
The convergence lens walks it and projects `unitKey`, `unitAddress`, `unitRent` as **informational** columns
so the operator/FE row answers "applying to lease Unit X at $Y/mo."

**No `missing_unit` gap column.** The reviewer (M1) showed a `missing_`-prefixed column with no remediation
op bound would leave Weaver watching an eternally-`violating` row it cannot actuate — a stuck non-converging
application. And the PO (§7 Q2) ruled an application must *always* name what's being leased. Both point to
the same answer: **make `unit` mandatory at create** (Starlark `required_string` + alive-check), so a
unit-less application can never exist — no gap, no remediation op, no Weaver wedge. The unit columns feed the
row for display only; they do **not** appear in the `violating` OR-clause.

**D6 — availability / double-book is OUT of scope here** (it's the separate ★★★ "Appointment scheduling /
slot-uniqueness" class of problem, and Capability-KV §06 defers temporal/uniqueness enforcement). v1 sets
`listing.status` by hand; a `LeaseUnit` op flipping `available→leased` on signature is a clean follow-on.
*Flag:* do not silently imply this design enforces single-occupancy.

## 4. Convergence-lens change

**The blocker the review caught (B1):** the existing lens has a `WITH … AS …` aggregation clause between the
`OPTIONAL MATCH`es and the `RETURN`, and the executor's `WITH` replaces the binding set with *only* the
projected items. So a new `OPTIONAL MATCH …->(u:unit)` placed before the `WITH` is **dropped** unless `u`'s
fields are carried *in* that `WITH` — otherwise `u.*` resolves to `null` unconditionally. The aspect-hop
(`u.address.data.line1`) must also happen off the live `*nodeRef`, i.e. **inside the same `WITH`** that
aggregates, before `u` is reduced. The corrected shape:

```cypher
MATCH (app:leaseapp {key: $actorKey})
OPTIONAL MATCH (app)-[:applicationFor]->(id:identity)
OPTIONAL MATCH (app)-[:appliesToUnit]->(u:unit)              -- NEW (0..1)
OPTIONAL MATCH (id)<-[:providedTo]-(inst:service)
WITH
  app.key AS entityKey,
  id AS applicantNode, id.key AS applicant,
  app.signature.data.signedAt AS signedAt,
  id.ssn.data.value AS ssnVal,
  u.key                     AS unitKey,        -- NEW: carried THROUGH the WITH (B1 fix)
  u.address.data.line1      AS unitAddress,    -- NEW: aspect-hop off the live node, here
  u.listing.data.rentAmount AS unitRent,       -- NEW
  count(DISTINCT CASE WHEN inst.family.data.value = 'backgroundCheck' AND ... END) AS freshBgComplete,
  ... (existing bg/payment counts unchanged) ...
OPTIONAL MATCH (applicantNode)<-[:providedTo]-(bg:service) WHERE ...
RETURN
  entityKey AS actorKey, entityKey, applicant,
  unitKey, unitAddress, unitRent,              -- NEW: informational only
  (ssnVal = null)       AS missing_onboarding,
  (freshBgComplete = 0) AS missing_bgcheck,
  (payComplete = 0)     AS missing_payment,
  (signedAt = null)     AS missing_signature,
  ... (inflight + maxretries columns unchanged) ...,
  ((ssnVal = null) OR (freshBgComplete = 0) OR (payComplete = 0) OR (signedAt = null)) AS violating
  -- NOTE: unit columns are NOT in `violating` (no missing_unit gap — §3 D5). `unit` is required at create.
```

- **`Output.BodyColumns` must add `unitKey`/`unitAddress`/`unitRent` (review M2).** Additive cypher columns
  are necessary but not sufficient — `BodyColumns` is an explicit allow-list; columns absent from it never
  reach the `weaver-targets` row. This is required work, not just the cypher edit.
- Null-safe: an absent unit ⇒ null columns (but `unit` is required at create, so in practice non-null). One
  row per anchor preserved (`appliesToUnit` is 0..1) — §10.2 FROZEN shape intact.
- The lens stays **`unit.listing.status`-blind** in v1 (review C5): a `leased` unit is not rejected here;
  availability/double-book is the separate deferred item (§3 D6). Stated so it isn't mistaken for enforcement.

## 5. Increments

1. **`loftspace-domain` package** (`depends: [location-domain]`) — the `.listing` + `.address`
   **aspect-type** DDLs, `SetListing` / `SetUnitAddress` ops (operate on an existing `vtx.unit`), permissions,
   manifest, `make verify-package-loftspace-domain` + unit tests. **No vertex DDL** (the unit is
   location-domain's). **Ready to build now** (independent of the lease integration). L2; M.
   **✅ DONE (Steward, 2026-06-25).** Built as one `loftspaceListing` vertexType DDL owning both ops + two
   declaration-only aspect-type DDLs (`listing`/`address`) as step-6 write gates (the
   freshnessMarker/freshnessExpiry split). Both aspects non-sensitive (they attach to a unit, not an
   identity). Ops are unconditioned upserts (re-publishable; an operator can flip `status` by hand). Gates
   green + lead-reviewed; `make verify-package-loftspace-domain` passes 67 assertions; **live-verified** on
   the running stack (minted a unit, set listing+address through the real Processor; bad-status, non-unit-key,
   and absent-unit all rejected). `make install-loftspace` now installs `location-domain` → `loftspace-domain`
   before `service-domain`.
2. **`lease-signing` integration** — declare the `appliesToUnit` link DDL here; `CreateLeaseApplication`
   takes a **required** `unit`, adds it to `ContextHint.Reads`, **alive-checks** it (C7), writes
   `appliesToUnit`; add the `.terms` aspect; extend the convergence-lens `WITH`/`RETURN` (§4) **and**
   `Output.BodyColumns` (M2). **The Refractor convergence e2e (`make test-lease-convergence`) is the
   load-bearing gate** — it proves the cross-package `appliesToUnit` write + the lens walk + the unit columns
   project (per-package verify only proves the DDL installs). Update `make install-loftspace` to install
   `loftspace-domain` **before** `service-domain`/`lease-signing` (M3). M.
3. **Applicant FE** (the separate ★★★ L item, now unblocked) — intake form that picks a unit + collects
   terms, "what I'm leasing" on the status tracker, the task inbox (shipped), document upload (objects-base).

## 6. Contract / frozen-surface check

- **No new vertex type** (the corrected design reuses location-domain's `vtx.unit`). `loftspace-domain` adds
  only **aspect-type DDLs** (`.listing`/`.address`) + ops — package-authored, permissive-by-default, **no
  frozen-contract edit**. New links (`appliesToUnit`/`hasCoApplicant`/`hasGuarantor`) follow the 6-segment
  shape + the sentence/direction rule (C3). The cross-package aspect-contribution (loftspace aspects on a
  location-owned vertex) is the same contract-contribution pattern capability-kv uses — packages add aspects
  to vertices they don't own; the aspect-type DDL just has to be installed.
- The convergence-lens row gains additive informational columns — §06 says value-shape additions are fine;
  the Weaver target row is FROZEN as *one-row-per-candidate with a `violating` flag* (§10.2), preserved
  because `appliesToUnit` is 0..1 (review C2 — do not later project multi-links as fan-out columns).
- **No convergence-semantics change for Andrew to gate.** The earlier `missing_unit` idea was dropped (§3 D5
  / §7 Q2): `unit` is required at create instead, so existing happy-path semantics are unchanged and there is
  no new unactuatable gap. Nothing here needs Andrew — it is package-owned, additive, and revertible.

## 7. Decisions (resolved — no Andrew gate; none were architectural or contract-requiring)

These were classified, then decided by their proper owner — not punted to Andrew:

1. **Q1 (PO) — naming + shape:** `unit`, **flat** (no `property` parent) for v1. A building parent has no v1
   demand; add it when a multi-unit landlord/search view is. Type name `unit` (the concrete leased thing) over
   `listing` (a market presence — secondary).
2. **Q2 (PO + review M1) — convergence semantics:** an application must always name what it leases → `unit`
   is **required at `CreateLeaseApplication`** (not an optional `missing_unit` gap, which would wedge Weaver
   with an unactuatable gap). Unit fields are informational columns only.
3. **Q3 (Winston) — data placement:** income/employment on the **identity** (an attribute of the person,
   reused across applications), as a `sensitive:true` aspect DDL, kept off the projection plane (C4).
4. **Q4 (PO) — co-applicant scope:** model `hasCoApplicant`/`hasGuarantor` links; gate **the primary
   applicant only** in v1; co-applicant gating is a follow-on.

*(The LoftSpace PO calls were made by Winston wearing the PO hat — the `vertical-po` role-loop isn't a
spawnable agent type in this environment — grounded in the board's PO notes. If Andrew disagrees with any
product call, it's a cheap reversal; none are contract-bound.)*

## 8. Not in scope (guardrails)

- Availability / double-book / slot-uniqueness (separate ★★★ item; §06 defers it).
- No frozen-contract edit. No read-path auth / per-user scoping (trusted-tool posture holds).
- **Build-ready:** the L-item review bar is met (adversarial review done, blocker + majors folded above) and
  the PO/Winston decisions are resolved. Increment 1 (`loftspace-domain` package) can start now; increment 2
  must show the *real merged* convergence cypher (§4) and pass `make test-lease-convergence`.
