# clinic-app — UX design (Increment A: book · my appointments · provider schedule)

> **Status:** ✅ **Winston-ratified — build-ready.** No frozen-contract change, no architecture fork.
> Every decision here is implementation/UX-level, resolved by Winston grounded in the shipped
> `cmd/loftspace-app` precedent and `clinic-domain` Increment 1. The UX-then-FE flow: this is the UX spec;
> the FE Engineer builds `cmd/clinic-app` from it and verifies in-browser. Pairs with the PO demand row
> *Clinic FE — patient booking + provider schedule* (Clinic, FE, ★★★, L).

---

## 1. What this is

The clinic vertical's first front-end — the demand-side patient/booking app, sibling to the LoftSpace
applicant app. It exercises `clinic-domain` Increment 1 live: a person browses providers, books an
appointment, tracks their appointments, and a clinic-desk view shows a provider's schedule. Until this
ships the clinic vertical is **headless** (the same gap LoftSpace had — everything had to be driven via
`lattice op submit`).

Like Loupe and loftspace-app it is a **trusted single-identity tool** — no per-user authN/authZ, no
Gateway, no read-path auth (Phase-3+). It binds `127.0.0.1`, connects to NATS as the primordial admin
actor, and submits ops on the user's behalf. The "who am I" context is a **patient switcher** (mirrors
loftspace's applicant switcher).

New binary `cmd/clinic-app` on **`127.0.0.1:7799`** (Loupe `:7777`, loftspace-app `:7788`, clinic-app
`:7799`).

## 2. Grounding — the platform surface (do not reinvent)

`clinic-domain` Increment 1 (the bookable domain) ships exactly what Increment A needs:

- **`clinicProviders` lens** → `clinic-providers` bucket: one row per named provider
  `{providerKey, name, specialty, credentials}` — the booking picker (P5 read model).
- **`clinicAppointments` lens** → `clinic-appointments` bucket: one row per appointment
  `{appointmentKey, startsAt, endsAt, reason, status, patientKey, patientName, providerKey, providerName, providerSpecialty}`
  — both "my appointments" (scope by `patientKey`) and "provider schedule" (scope by `providerKey`).
- **Ops:** `CreatePatient {fullName, dob?, email?, phone?}`, `CreateProvider {fullName, specialty, credentials?, bio?}`,
  `CreateAppointment {patient, provider, startsAt, endsAt, reason?}` (reads = `[patientKey, providerKey]`),
  `SetAppointmentStatus {appointmentKey, status}` (reads = `[appointmentKey]`),
  tombstones.

**One platform gap this design fills (owner work, done in the same fire):** there is **no patient roster
lens**, so the patient switcher and "my appointments" scoping have nothing P5-clean to read. Add a
**`clinicPatients`** projection lens (`clinic-patients` bucket) mirroring `clinicProviders` exactly — one
row per named patient `{patientKey, name}`. **Name only** (no DOB/email/phone): the patient switcher needs
only a human label, and DOB/contact is the PHI the deferred Vault plane owns — do not fan it into a second
read model casually. (Provider profile fields are not PHI, so `clinicProviders` projecting specialty is
fine.) This is the "no lens projects the field → file it, build it" rule (Winston files + builds).

The FE inherits **loftspace-app's design system verbatim** — `style.css` (the dark theme, cards, tabs,
badges, modal, toast, stepper), the vanilla-JS `api()`/`toast()`/`$()` helpers, the `POST /api/op` submit
path, the trusted-tool switcher pattern. **No new framework, no build step.**

## 3. The experience (three tabs + a patient context)

**Top bar:** brand "Clinic" · a **Patient** `<select>` (the `clinicPatients` roster, name labels) ·
a **＋ New patient** button. The selected patient is persisted in `localStorage` (refresh keeps context),
exactly like loftspace's applicant key.

### Tab 1 — Book (default)
The booking form:
- **Provider** `<select>` (the `clinicProviders` roster — "Dr. Sam Okafor · Cardiology"). If the roster is
  empty, an inline **Add a provider** mini-form (fullName, specialty, credentials?) so a fresh stack is
  self-contained — you don't have to drop to the CLI to seed a provider (the headless gap, closed).
- **Patient** — the top-bar context (read-only echo "Booking as <name>"; if none selected, prompt to pick
  or add one — the Book button is disabled until a patient is chosen, mirroring loftspace's "select an
  applicant first").
- **Date & time** (`datetime-local` → start) + **Duration** (a small select: 15/30/45/60 min → computes
  `endsAt`) + **Reason** (optional text).
- **Book appointment** → `CreateAppointment` with `reads:[patientKey, providerKey]`. On success: toast +
  route to **My Appointments** with the new appointment highlighted (the loftspace apply→track pattern).

**Honest deferral (mirrors loftspace's apply form):** Increment A records a **requested** time. It does
**NOT** enforce availability, provider hours, or reject a double-booked slot — that is the separate ★★★
*Appointment scheduling — conflict + temporal availability* item (`clinic-domain` D6; Capability-KV §06
defers it). A one-line note under the time field says so plainly ("Requested time — availability isn't
enforced yet").

### Tab 2 — My Appointments (scoped to the selected patient)
One card per appointment for the patient, read from `clinicAppointments` filtered by `patientKey`, sorted
by `startsAt`. Each card: provider name + specialty, the date/time (formatted), the reason, and a **status
badge** (scheduled / confirmed / completed / cancelled / noShow). Upcoming vs. past split by `startsAt`
relative to `now` (a subtle "past" dimming). Actions: **Cancel** (a `SetAppointmentStatus → cancelled`,
confirm-guarded; hidden once cancelled/completed). Empty state when the patient has no appointments. The
loftspace status-badge / card idioms apply.

### Tab 3 — Schedule (provider day view — the clinic-desk lens)
A **Provider** `<select>` (the roster) → that provider's appointments from `clinicAppointments` filtered
by `providerKey`, sorted by `startsAt`, each showing the patient name, time, reason, and status. This is the
"provider day/week schedule" the PO asked for, scoped simply to a provider's full list in Increment A
(true day/week calendar grid is a later increment — flagged, not built). Useful immediately for "what's on
Dr. Okafor's books."

## 4. API endpoints (`cmd/clinic-app`, all P5-clean — lens reads only)

| Endpoint | Source (lens read model) | Returns |
|---|---|---|
| `GET /api/providers` | `clinic-providers` bucket | `{providers:[{providerKey,name,specialty,credentials}], count}` |
| `GET /api/patients` | `clinic-patients` bucket (NEW lens) | `{patients:[{patientKey,name}], count}` |
| `GET /api/appointments?patient=<key>` | `clinic-appointments`, filtered by `patientKey` | `{appointments:[…], count}` |
| `GET /api/appointments?provider=<key>` | `clinic-appointments`, filtered by `providerKey` | `{appointments:[…], count}` |
| `POST /api/op` | — (write) | the `OperationReply` (copied verbatim from loftspace-app `op.go`) |

`computeProviders` / `computePatients` / `computeAppointments(keys, get, filter)` are the unit-testable
seams (the loftspace `computeListings(keys, get, statusFilter)` pattern). A row that fails to decode or
carries no key is skipped (tombstone-safe). No Core-KV read anywhere — the STRICT P5 lint gate must stay
clean (clinic-app **zero** `core-kv` references).

## 5. Out of scope (Increment A guardrails — say it, don't imply otherwise)

- **No availability / double-book / provider-hours enforcement** (D6 — the separate ★★★ item). The app
  records a requested time.
- **No `@every` reminders** (D7 — the separate platform item).
- **No PHI/Vault** (D5): DOB/email/phone are stored plain by `CreatePatient` and are **not** projected into
  the roster read model; the patient switcher shows name only.
- **No per-user auth / per-patient data isolation** — trusted-tool posture; the patient switcher is a
  context selector, not a login. Any browser can pick any patient (fine under the trusted model, like
  loftspace).
- **No reschedule** (cancel + re-book covers it for Increment A); no calendar grid (a flat sorted list).
- **No clinic-admin availability management** (depends on the deferred availability model).

## 6. Local-stack wiring

A **`make up-clinic`** one-command target mirroring `make up-loftspace`: `up-full` → `install-clinic` →
build + background-start `clinic-app` on `:7799` alongside Loupe (`:7777`). `make down` also reaps
`clinic-app`. `make run-clinic-app` runs the binary alone against an already-up stack. This finally lets the
clinic vertical be exercised **live** (the PO's "no vertical installed by up-full / can't drive clinic
live" observation, closed for clinic).

## 7. Review plan

The net change is the FE app (`cmd/clinic-app`, mirrors the lead-reviewed loftspace-app increments) plus
**one** new projection lens that is a **verbatim mirror of the already-3-layer-reviewed `clinicProviders`**
(pure read projection — no new ops, permissions, or DDL gates; no capability/authz surface). Treated as
**M**, **lead review** + the full gate set (`go build`, `make vet`, `golangci-lint`,
`STRICT=1 lint-conventions` P5, `go test ./packages/clinic-domain/... ./cmd/clinic-app/...`,
`make verify-package-clinic-domain`), with in-browser verification against `make up-clinic`. (Stated
explicitly so it can be overridden: if the lens addition is judged capability-plane-adjacent enough to want
the full 3-layer pass, escalate — but a same-shape read projection is the lowest-risk package change there
is.)
