# Clinic appointment conflict detection — op-time double-book rejection

**Status:** ✅ Winston-ratified — build-ready (no frozen-contract touch; §06 explicitly
sanctions "the operation's own Starlark logic" for temporal availability).
**Owner:** clinic-domain package. **Size:** M (Increment 1), the gated remainder is Increment 2.
**Ref board row:** "Appointment scheduling — conflict + temporal availability" (★★★, Clinic).

## Problem

`CreateAppointment` records a **requested** time and never checks it against the provider's other
appointments — `ddls.go` says so explicitly ("The booking time is REQUESTED, not slot-conflict-checked
(D6 — deferred)"). Two patients can book Dr. X at the same instant. The clinic-app FE already shows an
honest "availability isn't enforced yet" note and is ready to surface a rejection. This is the ★★★
platform gap blocking the clinic FE's slot-picking.

Capability-KV §06 (FROZEN, L354–359) defers temporal availability to "a Phase 2 mechanism **or the
operation's own Starlark logic**." We take the second, sanctioned path — **no contract amendment.**

## Why it's build-ready now (the enabler)

The original board framing called for "a platform read seam in the op path (an adjacency/lens query the
op can call to enumerate a provider's appointments)" because package Starlark is **known-key-reads only**
(no prefix scans; `TestPackage_NoScans`). The §2.5 lazy **`kv.Read()`** builtin shipped today
(`internal/processor/starlark_kv.go`, wired in `starlark_runner.go:91`, available to **every** op script
incl. package DDLs). With it we sidestep the need for a new platform scan primitive: the provider carries
a small **bookings adjacency aspect** the op reads, and `kv.Read()` validates each candidate's live
state. **Winston decision:** a provider-maintained booking index + `kv.Read` liveness beats adding a
platform prefix-scan/adjacency seam — it stays package-local, P5-clean (no read-model/lens read in the
op path), and needs no contract change.

## Data model

A new **`.bookings` adjacency aspect** on the provider vertex (class `providerBookings`):

```
vtx.provider.<id>.bookings = { "appts": ["vtx.appointment.<a>", "vtx.appointment.<b>", ...] }
```

- **Initialized empty** by `CreateProvider` (`{"appts": []}`) so the key is **always present** — a
  declared `contextHint.reads` of an absent key is a *fatal* `HydrationMiss`, so the index must exist
  before it can be a declared (OCC-snapshotted) read.
- **Appended** by `CreateAppointment` (the new appt key) — the only writer in Increment 1.
- A plain list of **appointment keys**, NOT inline intervals: an inline interval would go stale on a
  reschedule/cancel that doesn't maintain the index, and a stale-narrower interval could silently *skip*
  a real conflict. Liveness + current interval are read fresh from each candidate (below). The
  inline-interval optimization is safe only once Increment 2 maintains the index across all mutating ops.

New aspect-type DDL `providerBookingsAspectTypeDDL` (class `providerBookings`, declaration-only step-6
write gate, `PermittedCommands: [CreateProvider, CreateAppointment]`, NON-sensitive).

## CreateAppointment algorithm (Increment 1)

Caller adds `vtx.provider.<id>.bookings` to `contextHint.reads` (alongside the patient + provider keys it
already declares) — making it a **declared/OCC-snapshotted** read.

1. Validate patient + provider alive + class (unchanged).
2. Normalize `startsAt` / `endsAt` to canonical whole-second UTC (unchanged) — makes the lexical RFC3339
   overlap compare sound for any caller offset.
3. **Reject `endsAt <= startsAt`** (a new, cheap, always-correct guard — a zero/negative-length booking).
4. Read the provider's `.bookings` from `state` (declared read; always present). For each candidate appt
   key:
   - `cand = kv.Read(cand_key)`; if `cand == None or cand.isDeleted` → **drop** (tombstoned; prune).
   - `sched = kv.Read(cand_key + ".schedule")`; `st = kv.Read(cand_key + ".status")`.
   - if status ∈ {`cancelled`, `completed`, `noShow`} → **not blocking; prune** from the rebuilt list
     (bounds the index to the live future book).
   - else (status ∈ {`scheduled`, `confirmed`}) → **keep**, and test **overlap**:
     `req.startsAt < cand.endsAt AND cand.startsAt < req.endsAt` (half-open intervals; lexical compare on
     canonical UTC). On overlap → `fail("SlotConflict: provider <key> is already booked <cand.startsAt>–<cand.endsAt> (appointment <cand_key>)")`.
5. No conflict → mint the appointment exactly as today, **and** write the provider's `.bookings` =
   `{appts: kept_list + [new_appt_key]}` (the pruned-and-appended list).
6. `mutations` includes the `providerBookings` upsert; the rest is unchanged.

**Pruning** in step 4 keeps `appts` to live, non-terminal appointments, bounding both the index size and
the per-call `kv.Read` fan-out to a provider's active book. (Past-but-never-completed `scheduled` appts
linger — package scripts have no clock to prune by time — a minor, bounded leak, not a correctness issue.)

### Concurrency (the safety property — fail-closed)

Two concurrent `CreateAppointment` for the same provider+slot: both snapshot `.bookings` at revision `R`,
neither sees the other (uncommitted), both pass the conflict check, both write `.bookings` with
`expectedRevision = R`. The first commits → `R+1`; the second's commit **CAS-fails** (`RevisionConflict`)
→ rejected. The worst case is a *spurious* `RevisionConflict` on genuinely-disjoint concurrent bookings
for one provider (the client retries; concurrent same-provider bookings are rare) — **never a silent
double-book.** The `.bookings` aspect is the serialization point; this requires it to be a **declared**
read (on-demand `kv.Read` is NOT OCC-guarded at commit, per §2.5).

## Scope split

**Increment 1 (this fire):** `CreateAppointment` double-book rejection + the `.bookings` aspect +
`CreateProvider` init + the `endsAt > startsAt` guard. `RescheduleAppointment` / `SetAppointmentStatus` /
`TombstoneAppointment` are **unchanged** — the index self-heals via `kv.Read` liveness + create-time
pruning, so it never blocks a freed slot. **Known limitation:** a *reschedule* can move an appointment
INTO an occupied slot without rejection (reschedule isn't conflict-checked yet) — documented, Increment 2.

**Increment 2 (follow-up, no contract gate either):** conflict-check `RescheduleAppointment` (thread the
provider key — store `providerKey` on the `.schedule` aspect so reschedule/cancel can find it), maintain
the index inline across all mutating ops (enabling the pure inline-interval pre-filter), and **provider
availability windows** (a provider `.hours` aspect; clock-free day-of-week/time-of-day membership test on
the requested instant). Provider-hours rejection + reschedule-conflict complete the §06 story.

## Surfaces touched (Increment 1)

- `packages/clinic-domain/ddls.go` — new `providerBookings` aspect-type DDL; `CreateProvider` inits
  `.bookings`; `CreateAppointment` reads/prunes/appends + overlap+liveness check + `endsAt>startsAt`.
- `packages/clinic-domain/permissions.go` / `manifest.yaml` / `package.go` — register the new aspect DDL
  if the package wiring enumerates aspect DDLs (verify against the existing four).
- `scripts/verify-package-clinic-domain.go` — assert the new aspect DDL + `PermittedCommands`.
- `cmd/clinic-app/` — `CreateAppointment` submit adds the provider `.bookings` key to `contextHint.reads`;
  surface a `SlotConflict` rejection in the Book tab (replace the "not enforced yet" note).
- Tests — `integration_test.go` (Processor-driven: conflict rejected, disjoint allowed, cancelled frees
  the slot, tombstoned ignored, back-to-back touching is allowed, `endsAt<=startsAt` rejected),
  `package_test.go` (DDL/permission/NoScans), `lens_cypher_test.go` unaffected.

## Non-goals / explicitly deferred

- Provider availability windows / business hours → Increment 2.
- Reschedule-into-conflict rejection → Increment 2.
- Fixed slot-grid booking (we keep arbitrary-interval) — not needed; overlap math handles any duration.
- Patient double-book (a patient with two simultaneous appts) — lower value; same mechanism on the
  patient if ever wanted.
