# Protected-lens provisioning: out-of-band + verify-and-pause (retire the RLS DDL-ownership exception) — design

**Status: 📐 awaiting-Andrew (ratification)** · Designer fire 2026-06-29 (Winston) · Lattice lane, Refractor
read-path (D1.3) · originated from Andrew's question "the out-of-band DDL decision was paired with Refractor
*pausing* a lens on adapter error — can we use the same approach for protected lenses?"

---

## For Andrew (one-look ratification)

**What it does (two lines).** Today Refractor **runs the DDL** for protected + grant Postgres tables
(`CREATE TABLE` / `FORCE ROW LEVEL SECURITY` / `CREATE POLICY` in `adapter/rls.go`) — the one exception to
the standing *"Postgres table creation & maintenance is out-of-band"* principle. This design **removes that
exception**: the operator provisions those tables out-of-band like every other Postgres target, and Refractor
instead **actively verifies the security posture and pauses the lens fail-closed** if it's absent — reusing
the existing `Probe → ConsumerSupervisor` pause/resume machinery that already backs the out-of-band model for
plain tables.

**Architectural fork:** **none.** It's a posture choice (provision-vs-verify), not a new boundary. It reuses
the shipped supervisor pause machinery and the shipped `Build*TableDDL` string generators (repurposed: stop
*executing* them, keep them as the operator runbook + the verifier's expected-shape reference).

**Frozen-contract change: yes — Contract #6 §6.14 (staged UNCOMMITTED in `main`).** One sentence flips from
*"Every protected table **is created with** ENABLE+FORCE ROW LEVEL SECURITY"* to *"is **provisioned
out-of-band with** … ; Refractor **verifies** the posture at activation and **pauses fail-closed** if
absent."* The **D1 H3 guarantee is preserved verbatim** — a table whose policy was never generated still
denies all rows; it becomes a *verified precondition* instead of a *provisioned fact*. The edit is the
proposal — review the diff.

**Why it's worth doing (and why it's safe — the crux).** It restores a single consistent principle (all
Postgres DDL out-of-band) and removes Refractor's only runtime-DDL footprint. It's safe — arguably *safer* —
because the **security-load-bearing check is exactly one bit: `relforcerowsecurity = true`.** With FORCE RLS
on, **every** policy/column mistake an operator can make **fails closed** (the table over-denies — a visible
outage — it can never over-share). So the worst an out-of-band mistake produces is a *paused lens + a Health
alert*, never a silent leak. And unlike create-once provisioning (which never re-checks), a posture Probe on
the periodic heartbeat **continuously re-verifies** — catching a later `ALTER TABLE … NO FORCE ROW LEVEL
SECURITY` that today's approach would miss.

**Recommendation:** ratify; build it as a revision to the in-flight D1.3 provisioning. It also cleanly
subsumes **F2** from the prior fire (the protected adapter's missing seq-guard): the verifier asserts
`projection_seq` exists, so the adapter can honor it with no DDL.

---

## 1. Problem + intent

**The exception.** `cmd/refractor/main.go buildAdapter` calls `PostgresGrantWriter.Provision()` and
`ProvisionProtectedTable()` at lens activation; `adapter/rls.go` runs `CREATE TABLE IF NOT EXISTS`,
`ALTER TABLE … ENABLE`/`FORCE ROW LEVEL SECURITY`, and `DROP/CREATE POLICY`. This is the **only** place
Refractor issues runtime DDL. Every other Postgres target is provisioned **out-of-band** — the adapter
"issues no DDL" ([refractor.md:62](docs/components/refractor.md)), and a structural mismatch (missing column)
surfaces as a **write error → the pump pauses the lens → re-probes → auto-resumes** when the operator fixes
the table. That pause-on-trouble net is the established out-of-band story for plain tables.

**Why protected was carved out** ([refractor.md:68-74](docs/components/refractor.md)): *"Refractor owns the
provisioning so schema and policy cannot drift from the projection, and **FORCE RLS is structural rather than
a checklist item**."* The fear: a forgotten `FORCE ROW LEVEL SECURITY` = silently world-readable PII.

**The intent here (Andrew's question).** Keep the security guarantee, but deliver it the *same way* the rest
of the Postgres surface works — **out-of-band provisioning + pause-on-trouble** — so there is **one**
principle, not a security-plane exception. The move that makes this sound is to upgrade the pause net from
*passive* (write-error) to **active** (posture verification), because the RLS property is invisible to the
write path (below).

---

## 2. The shape

### 2.1 The one subtlety: a missing RLS posture produces NO write error

A structural mismatch (missing/renamed column) makes an `INSERT` fail → the existing passive pause catches
it. But a **missing/disabled RLS policy or FORCE-RLS produces no write error** — writes to an unlocked table
succeed fine; the table is just world-readable on the *read* path. So a naive "pause on adapter error" would
leave a silent **fail-open**. This is precisely why protected wasn't already out-of-band — and the gap the
design must close with an **active** check.

### 2.2 The security-load-bearing assertion is `FORCE ROW LEVEL SECURITY = on`

The verification gates three things, but they are not equal:

- **`relforcerowsecurity = true` — SECURITY-critical.** With FORCE RLS on, a *missing or wrong policy*
  **deny-alls** (over-denies, never over-shares — D1 H3). So this single bit is the only thing standing
  between "projecting PII" and "a leak." If it's off → **pause fail-closed** (never project a protected row
  into a world-readable table).
- **Expected columns present** (`authz_anchors text[]`, `projection_seq bigint`, key + body cols) —
  **functional.** Their absence would fail the write anyway; verifying up front turns a per-row write error
  into a clean activation pause + actionable message. (Also the seam that lets the adapter seq-guard — F2.)
- **A `FOR SELECT` policy present** — **functional, not a leak vector.** With FORCE RLS on, a missing policy
  is a safe outage (deny-all), not a leak; we still pause so the operator learns the read model is dark.

So the posture Probe is *fail-closed on the one bit that matters* and *fail-functional on the rest* — and
crucially **no operator mistake can produce over-sharing**, only over-denial.

### 2.3 The mechanism — reuse the supervisor's probe-before-drain path (no new pump logic)

The `ConsumerSpec.Probe` seam already feeds the supervisor's pause/recovery loop
([pipeline.go:331](internal/refractor/pipeline/pipeline.go)). Two existing primitives give the fail-closed
**activation** gate for free:

1. **Start protected/grant lenses with an initial `PauseInfra`.** An infra-paused pump runs
   `waitWhilePaused → runProbeLoop` ([consumer_supervisor_pump.go:226-228](internal/substrate/consumer_supervisor_pump.go))
   — i.e. it **probes BEFORE the first drain** and only proceeds to project once the Probe passes. This is the
   load-bearing detail: the pump normally drains-then-probes, so a probe only in the recovery path would let
   the first batch project fail-open. Starting infra-paused inverts that for these lenses → **no projection
   until the posture is verified.**
2. **`PauseInfra` auto-clears on a passing Probe** ([spec.go:50-51](internal/substrate/consumer_supervisor_spec.go))
   — so the UX is exactly Andrew's: posture absent → lens paused (Health `CapabilityLensPaused`, error) →
   operator provisions the table out-of-band → next Probe passes → **auto-resume, no operator Resume, no
   Refractor restart.** (Infra, not `PauseStructural`, precisely because it self-heals on operator action.)

So the per-message path is unchanged; we only (a) make the protected/grant adapters' `Probe` do posture
verification instead of `pool.Ping`, and (b) register those lenses initially infra-paused.

### 2.4 What runs (read-only catalog queries)

`VerifyProtectedTable(pool, table, keyCols, body)` and `VerifyGrantTable(pool)` — read-only, no DDL, no
writes:

- columns + types: `information_schema.columns` (assert keys, body cols, `authz_anchors` is `ARRAY`,
  `projection_seq` is `bigint`);
- FORCE RLS: `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $table::regclass` (assert
  both true);
- policy: `SELECT 1 FROM pg_policy WHERE polrelid = $table::regclass AND polcmd IN ('r','*')` (assert a
  SELECT-applicable policy exists).

The expected shape is the **same `BuildProtectedTableDDL` / `BuildGrantTableDDL`** that exists today — kept
as the single source of truth, now consumed by the verifier (expected columns) and the operator runbook
(§3), rather than executed.

### 2.5 Continuous re-verification (the "stronger than today" part)

Fold the posture check into the periodic Refractor heartbeat (the existing `CapabilityLensProvider` /
liveness-alert machinery, which already runs per-cycle). A protected lens whose FORCE-RLS was turned off
*after* activation raises a §5.5 `issues[]` entry and **re-pauses** (infra) → re-probes → resumes when fixed.
Create-once provisioning never re-checks; this does. Optional but cheap (it's the same read-only query on a
timer) and strictly stronger.

### 2.6 P-invariants

P2 (Processor sole Core-KV writer) — untouched (Refractor writes its own lens targets). P5 — untouched
(apps still read the RLS-locked table). The verifier reads the **Postgres catalog** (operational metadata),
never Core KV or a lens. No new keys (Contract #1 N/A). The change is *removing* writes (DDL), not adding any.

---

## 3. Contract surface + dev ergonomics

**Frozen-contract: Contract #6 §6.14 — staged UNCOMMITTED in `main`.** The enforcement bullet currently
reads *"Every protected table **is created with** `ENABLE`+`FORCE ROW LEVEL SECURITY`, so a table whose
policy was never generated denies all rows."* The edit flips the **provisioning actor** while preserving the
**guarantee**: the operator provisions out-of-band; Refractor **verifies `FORCE ROW LEVEL SECURITY` (+
columns + a SELECT policy) at activation and pauses the lens fail-closed if absent**, re-verifying on the
heartbeat. The deny-all-on-missing-policy property (D1 H3) is unchanged — it is now a *verified precondition*.
No other §changes; the §6.2-guard / authz-anchor / no-public-by-omission text is untouched. **Affected
consumers:** Refractor (Provision→Verify), the operator (now owns the RLS DDL runbook), and the §6.14 author
note about provisioning.

**Operator runbook + dev ergonomics (out-of-band ≠ manual-only).** `Build{Protected,Grant}TableDDL` stop
being *executed* but are surfaced two ways so nobody hand-writes RLS SQL:
- a **`lattice refractor emit-ddl [--lens <id> | --grant-table]`** CLI that prints the exact DDL for a lens
  (operator runs it against their DB out-of-band — the migration), and
- a **dev `make` target** (`make provision-readpath` or folded into `up-full`) that applies the same DDL to
  the dev Postgres, so the local stack is one command as today.

This keeps Refractor out of *runtime* DDL while making correct provisioning a copy-paste, not a research
project — exactly the posture of the existing `deleteMode: soft` column contract (refractor.md:62), now
extended to RLS.

---

## 4. Reconciliation with the existing mental model

- *"Didn't we deliberately make Refractor own this so FORCE RLS is structural, not a checklist item?"* Yes —
  and this keeps it structural, by **verifying** it rather than **creating** it. The anti-pattern that
  rationale guarded against (a forgettable checklist item that silently leaks) is closed harder: a forgotten
  posture **pauses the lens fail-closed**, it doesn't quietly serve. "Structural" becomes "enforced by
  refusing to run," which also covers post-creation drift the create-once path never re-checked.
- *"Does this duplicate machinery?"* No — it *reuses* the supervisor pause/probe loop (the same net plain
  out-of-band lenses already ride) and the existing `Build*TableDDL`. It **removes** the bespoke runtime-DDL
  path; net-negative surface.
- *"Does this introduce new state?"* No new persistent state. The verifier is stateless read-only catalog
  queries; the pause state already exists in the supervisor/HealthSink.
- *"Is the grant table different?"* It's the shared `actor_read_grants` referenced by every protected
  policy, so the operator provisions it **first** (runbook ordering) and `VerifyGrantTable` gates any
  grant/protected lens on its presence + shape. Same approach, one ordering note.

---

## 5. Migration / interplay with the in-flight D1.3 fires

The grant-writer + protected-adapter + `rls.go` are **already built around runtime `Provision`** (D1.1–D1.4
shipped/in-flight). This is a **swap, not a rebuild**:

- `Provision` / `ProvisionProtectedTable`: replace the `pool.Exec(stmt)` execution with the `Verify*` catalog
  reads; `Build*TableDDL` stays (now feeds the verifier + the CLI).
- `buildAdapter`: keep the same call site; on a failed verify, register the lens **infra-paused** (don't hard
  fail registration) so it self-heals.
- `GrantWriterAdapter.Probe` / `ProtectedAdapter.Probe`: change from `pool.Ping` to the posture verify (Ping
  is subsumed — a dead pool fails the verify too).
- **No production data migration:** D1.3 protected tables aren't live yet (the read boundary is still being
  built), so there are no Lattice-created tables to hand back to operators. Dev stacks switch to the `make`
  target. If any dev table exists from the old path, it already has the right shape (same `Build*DDL`), so the
  verifier passes against it unchanged.
- **F2 subsumed:** the verifier asserts `projection_seq` present → the protected adapter can seq-guard
  (the prior fire's F2) reusing the grant writer's `WHERE EXCLUDED.projection_seq > …` clause. Fold F2 into
  Fire 2 here.

---

## 6. Test strategy

- **Unit (verifier, `POSTGRES_TEST_DSN`-gated, mirrors `postgres_test.go`):** a correctly-provisioned table
  → verify passes; FORCE-RLS **off** → verify fails (the security assertion); a missing `projection_seq` /
  `authz_anchors` / key col → fails with the named-column message; no SELECT policy → fails. A non-protected
  plain table is never verified (unchanged path).
- **Pipeline/integration (embedded — pause behavior):** a protected lens activated against a table with
  **FORCE RLS off** starts **infra-paused and projects ZERO rows** (the fail-closed activation gate — assert
  no write reached the table); after the test provisions FORCE RLS, the probe loop **auto-resumes** and the
  backlog drains. A drift case: FORCE RLS removed mid-run → the heartbeat re-pauses.
- **CLI/dev:** `emit-ddl` output for a sample lens equals `BuildProtectedTableDDL`; the `make` target stands
  up a verifiable table (the integration test's fixture).
- **Regression:** plain out-of-band lenses unchanged; the existing `rls_test.go` `Build*DDL` shape tests stay
  (the strings are still the source of truth) — only their *executor* is retired.
- **Gates:** build / vet / golangci / STRICT-conventions / `go test` refractor (+ the `POSTGRES_TEST_DSN`
  integration) / Gate-3 read-path vectors once a live protected model exists.

---

## 7. Risks + alternatives

- **A — keep Lattice provisioning (status quo).** Rejected per Andrew: it's the lone runtime-DDL exception,
  it never re-checks drift, and "Lattice doesn't migrate" already makes an existing/older table a silent gap.
- **B — passive write-error pause only (no active verify).** **Rejected — this is the unsafe version:** a
  missing RLS posture throws no write error, so it would fail-**open** (silent leak). The active FORCE-RLS
  check is the non-negotiable core.
- **C — verify but hard-fail registration (don't pause).** Rejected: a hard fail needs a Refractor restart
  after the operator fixes the table; infra-pause + probe-loop self-heals (better ops UX, same safety).
- **Risk — operator burden / a typo'd table.** Mitigated by the `emit-ddl` CLI + `make` target (copy-paste,
  not hand-written) and the fail-closed pause (a mistake is a visible paused lens + Health alert, never a
  leak). This is the same burden⇄simplicity trade already accepted for plain tables, and the verify-and-pause
  net is what makes it safe to extend to the security plane.
- **Risk — verify/TOCTOU drift between probe and a later `ALTER`.** The heartbeat re-verify (§2.5) bounds the
  window to one heartbeat; create-once has an *unbounded* window, so this is strictly better. (A determined
  operator disabling FORCE RLS mid-flight is outside the threat model — trusted operator — but we still
  detect it within a heartbeat.)
- **Risk — grant table ordering** (protected policy references `actor_read_grants`). The runbook documents
  "provision the grant table first"; `VerifyGrantTable` gates every dependent lens on its presence, so a
  wrong order is a clean pause, not a broken policy.

---

## 8. Fire-by-fire decomposition (for the Lattice Steward)

**Fire 1 — the verifier + the fail-closed activation gate (the core).** Add `Verify{Protected,Grant}Table`
(read-only catalog checks, §2.4); switch the protected/grant adapters' `Probe` to posture-verify; register
protected/grant lenses **infra-paused** so the probe gates the first write; remove the `pool.Exec` from
`Provision`/`ProvisionProtectedTable` (keep `Build*DDL`). Unit + pipeline pause tests (§6). **Full 3-layer
review** (security plane — this *is* the read-auth boundary). Independently shippable; D1.3 protected models
aren't live, so there's no consumer regression.

**Fire 2 — operator runbook + dev ergonomics + F2 seq-guard.** The `lattice refractor emit-ddl` CLI + the
`make provision-readpath` target (dev parity); and **seq-guard `ProtectedAdapter`** (now that the verifier
guarantees `projection_seq`) reusing the grant writer's monotonic clause — closing the prior fire's F2 with
no DDL. `refractor.md` §"Protected read-model provisioning" rewritten to the verify-and-pause model.

**Fire 3 — continuous re-verification (drift).** Fold the posture check into the heartbeat (§2.5) so a
post-activation FORCE-RLS removal re-pauses + raises a §5.5 issue. Small, additive; can ride Fire 1 if cheap.

**Sequencing note:** this revises the D1.3 provisioning the read boundary depends on, so it should land
**before** the first live protected read model (the Verticals D1.3 Fire that stands one up) — i.e. it
re-points an in-flight dependency, it doesn't wait behind anything.

---

## 9. Grounding index

- Exception: `cmd/refractor/main.go` buildAdapter (`gw.Provision`, `ProvisionProtectedTable`);
  `internal/refractor/adapter/rls.go` (`BuildProtectedTableDDL`, `BuildGrantTableDDL`, `Provision`,
  `ProvisionProtectedTable`, `Upsert/RevokeGrant` seq-guard pattern).
- Pause machinery: `internal/substrate/consumer_supervisor_pump.go` (`waitWhilePaused`→`runProbeLoop`,
  probe-before-drain when infra-paused); `consumer_supervisor_spec.go` (`PauseInfra` auto-clears on passing
  Probe; `ClassInfra`/`ClassStructural`); `pipeline.go:331` (`spec.Probe = currentAdapter().Probe`).
- Adapters: `internal/refractor/adapter/read_path_adapters.go` (`ProtectedAdapter`/`GrantWriterAdapter`
  `.Probe` delegate); `postgres.go:73` (`Probe = pool.Ping`).
- Contract: `docs/contracts/06-capability-kv.md` §6.14 (Enforcement bullet — the "is created with FORCE RLS"
  sentence is the edit site). D1 design `read-path-authorization-d1-design.md` (H3 fail-closed rationale).
- Doc: `docs/components/refractor.md:62,68-74` (out-of-band plain vs Refractor-owned protected — rewritten in
  Fire 2).
