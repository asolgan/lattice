# Clinic no-show fee — design note

**Item:** "No-show doesn't cost anything" (`_bmad-output/planning-artifacts/backlog/verticals.md`).
`SetAppointmentStatus(status=noShow)` is a pure status flip today — no financial consequence.
This note makes the two decisions the board row flagged as needed before building: **package
boundary** and **fee-amount source**. Non-contract, package-level design — decided by the Vertical
Steward (Winston), not a Lattice-primitive or frozen-contract question.

## Package boundary

The board row assumed "a new package spanning clinic-domain + clinic-ledger," mirroring
`cafe-domain`'s `missing_charge → directOp(DebitAccount)` shape. Grounding in the actual code shows
a new package isn't needed and would in fact create a dependency cycle:

- `cafe-domain` owns the `WeaverTargets()`/lens and depends on `cafe-ledger` (`cafe-domain/package.go`
  `Depends: []string{"lease-signing", "cafe-ledger"}`).
- Clinic's dependency runs the **other direction**: `clinic-ledger` depends on `clinic-domain`
  (`clinic-ledger/manifest.yaml` `depends: [clinic-domain]`, for patientKey validation).
  `clinic-domain` depends on `location-domain` only, not `clinic-ledger`.
- Adding the Weaver playbook to `clinic-domain` would require it to also depend on
  `clinic-ledger` → cycle (`clinic-domain → clinic-ledger → clinic-domain`).

**Decision:** put the new `WeaverTargets()` + the new actorAggregate lens directly in
**`clinic-ledger`**, which already depends on `clinic-domain` and can read appointment data (the
Cypher engine queries the projected graph regardless of package Go-import boundaries; the
`depends:` field only governs install order). `clinic-ledger` also already owns `DebitAccount`, so
no cross-package dispatch is needed at all — the new mechanism is entirely self-contained inside
one package. No new package.

## Fee-amount source

Every existing `amountCents` gap-action param in the repo (`cafe-domain`'s `missing_charge`,
`semantic-contracts`' clause `amountCents`) traces back to a real, previously-stored data point —
never a literal baked into the lens/Weaver spec. There is no precedent for a system-wide hardcoded
fee constant.

**Decision:** mirror `semantic-contracts`' "amount captured once at authoring time, flows through
unmodified" shape. `SetAppointmentStatus` accepts an optional `noShowFeeCents` param, applied only
when `status == "noShow"`; if omitted, defaults to **2500** (a placeholder in line with the clinic's
existing example copay amount in `CreateAppointment`'s own docs — not a real billing-policy figure;
adjustable later via a real per-provider/site fee config if that becomes a product need). The value
is stored on the appointment's own `.status` aspect (`noShowFeeCents`), the same aspect that already
carries the optional cancel/no-show `note` — same idiom, same write.

## Mechanics (mirrors cafe-domain/cafe-ledger's `missing_charge` shape exactly, one gap only)

- **No `missing_account` gap.** Cafe's tab-settlement lens has two gaps because a café tab can settle
  before any café-ledger account exists. Clinic's existing billing docs already assume a registered
  patient has (or gets) a `clinicaccount` via the standing `CreateAccount` flow
  (`clinic-ledger/permissions.go`: "the trusted-tool app submits CreateAccount when a patient is
  registered") — the same assumption today's copay/insurance billing already relies on. If a
  no-show'd patient has no account yet, the gap simply doesn't converge until one is opened —
  acceptable for v1, not a regression versus today's billing.
- **New lens** `clinicNoShowSettlement` (actorAggregate, anchored on `appointment`), joining
  `appointment -[:forPatient]-> patient <-[:heldFor]- clinicaccount`, with an `OPTIONAL MATCH
  (appt)<-[:settles]-(tx:clinictransaction)` existence check (mirrors `cafeTabSettlement`'s
  `txCount` idiom). `missing_charge` = `status='noShow' AND feeCents>0 AND accountKey<>null AND
  txCount=0`.
- **New param** `appointmentRef` on `DebitAccount` (clinic-ledger), mirroring `cafe-ledger`'s
  `tabRef` exactly: optional string, `parts_of`-validated `vtx.appointment.<NanoID>`, alive-checked
  (`UnknownAppointment` on failure), writes the audit link
  `lnk.clinictransaction.<id>.settles.appointment.<id>` the lens's `OPTIONAL MATCH` walks. A plain
  human-submitted `DebitAccount` (no `appointmentRef`) is byte-for-byte unaffected.
- **New Weaver gap action**: `missing_charge → directOp(DebitAccount)` (`Class: "clinictransaction"`,
  pinned per the existing multi-DDL-claims-`DebitAccount` rule), `Params: {accountKey:
  row.accountKey, amountCents: row.feeCents, appointmentRef: row.appointmentKey}`.

## Scope of this fire

Design + full implementation (DDL param + script changes in both packages, the new lens +
Weaver target in `clinic-ledger`, manifest sync, unit/integration tests). No FE change — the
no-show fee posts automatically via Weaver; there is no consumer-facing surface to build for v1
(a "no-show fee" line will already show up in the existing ledger-history FE once
`clinicNoShowSettlement`/`DebitAccount` post it, via the existing `ledgerHistorySpec` projection —
no new FE code needed).
