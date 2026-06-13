# Story 9.4 — Blind Hunter Code Review (Weaver control-API/CLI, FR30)

Reviewer: Blind Hunter (`bmad-review-adversarial-general`), diff-only.
Evidence: `git status --porcelain` + `git diff` + full content of untracked files
(`internal/weaver/control.go`, `internal/weaver/control/*.go`, `cmd/lattice/weaver/weaver.go`,
internal tests). Build: `go build ./...` passes.

Severity legend: **HIGH** = correctness bug / data loss / contradicts shipped behavior;
**MED** = latent failure under plausible conditions; **LOW** = robustness/consistency nit
or lying comment.

---

## 1. [HIGH] `Revoke` desyncs `e.targets` from the supervisor — reconcile will NOT re-Add, contradicting the documented bound

`internal/weaver/control.go` — `Revoke` (≈ lines 158-200); cross-ref
`internal/weaver/engine.go:390-454` (`reconcileConsumers`).

`Revoke` calls `e.supervisor.Remove(ctx, name)` (deletes the durable, removes it from the
supervisor's `managed` map) but **never deletes `e.targets[targetID]`** — the engine's
last-applied lane-1 fingerprint map that `reconcileConsumers` keys its Add/Reset/Remove
decision on.

The doc/comment (control.go Revoke doc, weaver.md "revoke" section, `disabledTargetSet` comment)
all assert: *"if a later `reconcileConsumers` pass re-adds the target's consumer … dispatch
stays inert until an explicit enable."* That re-add **cannot happen**:

- After Revoke: durable gone from supervisor; `e.targets[id]` still holds the old fingerprint;
  target still registered in `targetSource` (Revoke does not unregister), so it stays in
  `desired`.
- Next `reconcileConsumers`: `applied, running := e.targets[id]` → `running == true`,
  `applied == fp` → hits `if applied == fp { continue }` (engine.go:416-418). **No `Add`,
  no Reset.** The durable stays permanently removed.
- The removal branch (engine.go:435) also never fires, because the id is still in `desired`.

Net effect: a revoked-then-`enable`d target is **dead** — no lane-1 durable exists, and nothing
recreates it. The `__control` marker / `disabledTargetSet` machinery that "keeps it inert until
enable" guards a consumer that is gone for good. The whole "strict superset of disable +
survives reconcile re-add" contract (AC #4) is broken on the re-add half.

Failure scenario: operator `revoke t1` (intending temporary teardown), later `enable t1`.
Expected (per docs): dispatch resumes. Actual: `Enable` calls `supervisor.Resume` on an
unmanaged name (no-op), clears the marker — but there is no consumer, so `t1` never processes
another row. Silent permanent outage of that target until the Weaver process restarts (Start →
reconcileConsumers with a fresh empty `e.targets` re-Adds it).

Fix: `Revoke` must `e.mu.Lock(); delete(e.targets, targetID); e.mu.Unlock()` (mirroring
reconcile's removal branch at engine.go:447) so the next reconcile sees `running == false` and
re-Adds. Note this also introduces a needed lock interaction with `reconcileConsumers` that the
current lock-free Revoke does not have.

**Untested:** the harness (`control_internal_test.go:105-121`) seeds targets straight into
`source.targets` and the supervisor via `supervisor.Add`, never populating `e.targets`, so the
reconcile re-Add path the docs promise is never exercised. Add a test that drives a real
reconcile after Revoke.

---

## 2. [MED] `Disable` partial failure leaves the durable paused but the engine reporting "active" (set + marker not written)

`internal/weaver/control.go` — `Disable` (≈ lines 95-115).

Order of operations: `(1) e.supervisor.Pause(...)` → `(2) e.marks.setDisabled(ctx, true)` →
`(3) e.disabled.set(true)`. `Pause` is void (cannot report failure), but `setDisabled` does a
KV `Put` that can fail (KV unavailable, context deadline). On a step-2 failure, `Disable`
returns an error **after** the pump is already paused, with neither the `__control` marker
written nor the in-memory set updated.

Resulting inconsistency:
- Pump is paused → no dispatch (this part is fail-safe).
- `ListTargets` reports `state: "active"` (in-memory set wasn't updated).
- On process restart, `seedDisabledTargets` finds no marker → seeds active; but the
  lane-1 PauseManual state restores via HealthSink → pump stays paused. Operator sees "active"
  while the target silently processes nothing, with no `enable` able to be reasoned about from
  the reported state.

Same shape in `Revoke`: if `deleteByTargetPrefix` (step b) or the final `setDisabled` (step d)
fails after `Remove` (step a) succeeded, the durable is gone but the target reports active and
the disabled-set is unset. Operator retry is the only recovery (idempotent, but undocumented as
the required recovery).

This is order-of-operations fragility, not a hard bug — but the "marker and set never disagree
mid-process" claim in the `disabledTargetSet` doc (engine.go) is **only true on the success
path**. Worth either writing the marker first (so durable-truth precedes the side effect) or
documenting the partial-failure recovery contract.

---

## 3. [MED] Control handlers run on `context.Background()` with no timeout — a slow/blocked engine op wedges the responder and never replies

`internal/weaver/control/service.go` — `handleList` and `dispatchEndpoint`
(both `ctx := context.Background()`).

Every endpoint handler builds `context.Background()` and passes it into `ListTargets` /
`Disable` / `Enable` / `Revoke`, several of which do unbounded KV list/get/put/delete loops
(`deleteByTargetPrefix` lists the whole bucket then deletes key-by-key; `ListTargets` iterates
all targets). If the underlying KV is slow or unavailable, the handler blocks indefinitely with
no deadline; `req.Respond` is never called; the operator's CLI request
(`weaver.go:request`, which DOES have `output.DefaultTimeout`) times out client-side with no
server-side cancellation, and the handler goroutine stays blocked. Under repeated operator
retries this leaks blocked goroutines on the Weaver.

Refractor's control plane may share this, but diff-only: this is a standalone robustness gap.
Derive the handler ctx from a bounded `context.WithTimeout` (or from a service ctx tied to the
micro service lifecycle) so a stuck engine op fails the request instead of hanging.

---

## 4. [LOW] Lying/over-confident comment: the `__control` collision-safety justification cites the wrong key segment

`internal/weaver/state.go:183-187` (`controlKeySuffix` doc) and weaver.md "Dispatch-skip marker"
section.

The comment argues `__control` "can never collide with a real
`<targetId>.<entityId>.<gapColumn>` mark, because `entityId`s are NanoIDs and
`substrate.Alphabet` contains no underscore." But the marker is matched by **suffix**
(`seedDisabledTargets` does `strings.CutSuffix(key, ".__control")`; the reconciler does
`strings.HasSuffix(key, ".__control")`). The segment that determines a suffix match is the
**last** one — `gapColumn` — not `entityId`. The actual reason no collision occurs is that
`gapColumn` is forced to match `missing_*` (`validateTarget`, registry.go:358-365), and
`__control` does not start with `missing_`; combined with `targetID` being a single dot-free
token (so a 2-segment `<targetId>.__control` key can never equal a 3-segment mark key).

The safety holds, but the stated reasoning is wrong and would mislead anyone relying on it if
the gap-column convention ever changed (e.g. a future gap column literally named `__control`
would defeat the suffix match the comment claims is impossible-by-NanoID). Correct the
justification to cite the `missing_` prefix + dot-free targetID, not the entityId NanoID.

---

## 5. [LOW] `seedDisabledTargets` does a redundant second KV read per marker

`internal/weaver/control.go` — `seedDisabledTargets` (≈ lines 35-58).

The loop `KVListKeys` → `CutSuffix(key, ".__control")` to recover targetID → then calls
`e.marks.isDisabled(ctx, targetID)`, which **rebuilds the identical key** (`controlKey(targetID)
== key`) and issues a second `KVGet`. Every `__control` key is read twice (once implicitly via
the listed key, once via isDisabled). Functionally correct but doubles the seed-time KV round
trips. Either `KVGet(key)` directly, or accept the list+parse only and trust presence (the
marker is only ever written with `disabled:true`; `setDisabled(false)` deletes it, so a present
`__control` key always means disabled — the value re-check guards only a corrupt body).

---

## 6. [LOW] `ControlRequest` is a documented dead type; `respondMicro` swallows marshal errors silently to the client

`internal/weaver/control/service.go`.

- `ControlRequest struct{}` is never read by any handler (targetID comes from the subject). The
  doc admits it's "retained for forward compatibility and symmetry" — acceptable, but it is dead
  surface today; a reader may assume the body is parsed.
- `respondMicro` logs a marshal failure and **returns without calling `req.Respond`** — the
  client then sees only a request timeout, not an error. Marshal of the response types here
  cannot realistically fail (plain structs), so this is theoretical, but the failure mode is a
  silent client-side timeout rather than a structured error. Low.

---

## Notes checked and found OK (no finding)

- **Subject parsing** (`targetIDFromSubject`): exactly-5-token check + literal
  `lattice/ctrl/weaver` guard + non-empty targetID is correct. Dotted targetIDs from the CLI
  produce >5 tokens and are rejected; registered targetIDs are dot-free by `validateTarget`.
- **Wildcard endpoint subjects** `lattice.ctrl.weaver.*.{disable,enable,revoke}` are mutually
  disjoint (distinct literal last token) and disjoint from the 4-token exact `…weaver.list`.
  A target literally named `list` does not collide (`…list.disable` is 5-token).
- **Reconciler `__control` skip** (reconciler.go:120-126) correctly `continue`s before
  `sweepMark`, so the marker is never enumerated/reclaimed/deleted as a corrupt mark.
- **`deleteByTargetPrefix`** uses the trailing-dot prefix `"<targetID>."`, so `t1.` does not
  match `t10.` keys — no cross-target over-deletion.
- **`disabledTargetSet`** is a correctly-locked `sync.RWMutex` map; the check-then-act gap
  between `isTargetDisabled` and a concurrent `Disable` is an inherent best-effort
  "takes-effect-next-delivery" semantic (in-flight messages may still dispatch), not a data
  race — acceptable by design, and Pause stops the pump for subsequent deliveries.
- **`clearClosedMarks` before the disabled-skip** (evaluator.go:52-67): intentional and
  correct — a disabled target still clears resolved marks; only NEW schedule/mark/Actuator work
  is skipped.
