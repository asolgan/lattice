# Adapter terminal-outcome (the "failed" producer) — build brief

**Status:** 📋 Design (ready for review). Implements the backlog row **"Structured adapter result"**
(`backlog.md`, External-I/O maturity, ★★ S–M).

**Backlog framing (corrected).** The backlog said *"thread a structured pass/fail/detail status onto
the reply; lens `missing_*` predicates key off the real status."* That over-states the bridge's role.
Andrew's challenge — *"why does the result have to be structured? Why does anyone but the replyOp need
to understand the structure? Even the Detail is internal to the leaseApp / backgroundCheck op"* — is
correct, and the code already agrees:

- The **bridge already treats the adapter's result as fully opaque.** `adapter.go`: *"Detail is an
  adapter-defined opaque outcome string … never interpreted by the bridge."* `dispatch.go:164` posts
  `payload = {externalRef, result: Detail}` and inspects neither field.
- The **lens-readable structured part already exists and is package-owned** — the `.outcome` aspect
  `{status ∈ completed|failed, completedAt}` on `vtx.service.<handle>`. The lens keys off
  `.outcome.status` today. `service-domain`'s direct `RecordServiceOutcome` op *already* reads and
  validates `status ∈ {completed, failed}` from its payload (`service-domain/ddls.go:109,235`).
- The **free-form `Detail` stays opaque + off the projection plane** (it can carry PII / payment data;
  it rides the `service.outcomeRecorded` provenance event, never the `.outcome` aspect). It is
  *deliberately* not parsed for pass/fail. That does not change.

So this is **not** "make the result structured for everyone to read." The bridge stays a dumb pipe.

## The actual gap (narrow, and self-documented as the Phase-3 plug-in point)

An adapter has exactly two ways to resolve today, and neither expresses *terminal business failure*:

| Adapter returns | Bridge does | Meaning |
|---|---|---|
| `Result{Detail}`, `err == nil` | post replyOp | terminal **success** |
| `error` | `NakWithDelay` (redeliver on same idempotencyKey) | **transient** retry |
| *(missing)* terminal **failure** | — | a rejected check / declined payment has **no representation** |

A declined payment is **not** a Go error (it is a definitive answer, not a retry) and **not** a
success. With no third channel, the bridge-wrapper replyOp `RecordLeaseServiceOutcome` hard-derives
the verdict — `lease-signing/scripts.go:347`:

```python
status = "completed"   # the bridge only posts a reply on adapter success …
completed_at = time.rfc3339_utc(op.submittedAt)
```

and its DDL flags this verbatim: *"a failed outcome has no Phase-2 producer on the bridge path — the
deliberate demo simplification, the Phase-3 plug-in point"* (`lease-signing/ddls.go:155`). This is
that plug-in point.

## Design — one opaque discriminator the bridge forwards verbatim

The adapter's *verdict* must reach the replyOp, and the bridge is the only adapter→replyOp path — so
exactly **one** new field rides through, carried opaquely (identically to how `result` rides today).
The bridge interprets nothing; the `{completed, failed}` vocabulary is a contract between an adapter
and *its* paired replyOp, not bridge knowledge.

- **D1 — `Result` gains a closed-enum terminal status; the bridge copies it verbatim.**
  Add `Status` to `bridge.Result` (a small closed set — `OutcomeCompleted` / `OutcomeFailed`, or the
  bare strings `"completed"` / `"failed"`). `dispatch.go` adds one line:
  `payload["status"] = result.Status`. The bridge does **not** branch on it (same posture as `result`).
  A terminal failure is `(Result{Status: failed, Detail: "…"}, nil)` — **no error** (errors stay
  reserved for transient retry). Rejected: parsing the free-form `Detail` for pass/fail (fragile, and
  `Detail` may carry PII — the DDL forbids it on the projection plane).
- **D2 — `RecordLeaseServiceOutcome` reads `payload.status` instead of hard-deriving; status is REQUIRED.**
  `scripts.go:347` becomes `status = required_status(p)` — the exact helper `service-domain` already has
  (`service-domain/ddls.go:233-238`: validate `status ∈ {completed, failed}`, else reject
  `InvalidArgument`). **No default, no back-compat.** Lattice is internal and the bridge + replyOp ship
  together, so a reply that omits a valid status is a wiring bug — surface it (reject), never silently
  coerce. The `.outcome` write, the create-only once-only guard, `completedAt`/`validUntil` arithmetic,
  and the `externalTaskCompleted` emit are **unchanged**: a *failed* outcome is still a completed *task*
  (Loom closes the token), but `.outcome.status = failed` keeps the lens gap-predicate violating, so the
  application stays blocked — the correct semantics, already wired.
- **D3 — fakes exercise the failed path.** `fake_background_check.go` returns `failed` for a configured
  subject; `fake_stripe.go` returns `failed` for a configured decline. Demonstrates end-to-end that a
  failed external check holds the lease application open.

**Lens: no change.** It already reads `.outcome.status`; it simply starts seeing `failed`.

## Change list
- `internal/bridge/adapter.go` — `Result.Status` (closed enum) + doc (still opaque to the bridge).
- `internal/bridge/dispatch.go` — `payload["status"] = result.Status` (one line; no branching).
- `internal/bridge/fake_background_check.go`, `fake_stripe.go` — return `failed` for a configured input.
- `packages/lease-signing/scripts.go` — `RecordLeaseServiceOutcome` reads/validates `payload.status`
  via a `required_status` helper (reject `InvalidArgument` on absent/out-of-enum) — mirror
  `service-domain/scripts.go`'s `OUTCOME_STATUSES` + `required_status`.
- `packages/lease-signing/ddls.go` — replyOp payload schema makes `status` **required**
  (enum `completed|failed`); refresh the doc-comment (drop "no failed producer / Phase-3 plug-in point"
  now that it exists, and the "status is derived completed" line on the `result` field); add a failed
  example.
- Tests — bridge dispatch/e2e (a `failed` Result → `payload.status=failed`); lease-signing lens test
  (a failed `.outcome` → applicant stays unsatisfied / gap stays violating).

## Contract & gates
- **No frozen contract touched.** The op-envelope (Contract #2 §2.1 / event #3 §3.4) is unchanged; the
  *payload* schema is the replyOp's own (package-owned) — adding an optional `status` is a package
  change, not an envelope change. `externalRef`/`result` semantics are unchanged.
- DDL changed ⇒ **`make verify-package-lease-signing`** (out-of-band gate — see the verify-package
  memory). Plus `go build ./…`, `make vet`, `golangci-lint run ./…`, and
  `go test ./internal/bridge/... ./packages/lease-signing/...`.
- Review depth: substantial cross-plane (adapter SPI + package DDL + lens semantics) → full 3-layer.
