# Sprint Change Proposal — Event-Driven External I/O (the "Bridge")

**Date:** 2026-06-18
**Author:** Winston (Architect) via `bmad-correct-course`
**Trigger story:** 11.1 (`lease-signing` package authoring), Epic 11 (Loftspace Reference Vertical)
**Scope classification:** **MAJOR** — fundamental re-architecture of the external-I/O boundary + re-plan of Epic 11; reworks completed Epic 10 (Two-Phase Nudge).
**Status: RATIFIED 2026-06-18 (Andrew)** — the 4-amendment package (Contract #3 dropped; M2 = Option b) is approved; the CARs in `cmd/{loom,weaver,refractor}/CONTRACT-AMENDMENT-REQUEST.md` are marked RATIFIED.

### ⏭️ NEXT STEPS (fresh-session handoff — the E.1 follow-through is NOT done yet; do these in order)
1. **Apply the 4 ratified amendments to `docs/contracts/10-orchestration-surfaces.md`** + a Contract #10 revision-history entry: §10.5/§10.6 (`externalTask` + the `payload.externalRef` correlation key), §10.3 (retire `weaver-claims` + pin `requestId = deterministic(instanceKey)`), §10.8 (retire `nudge` → `triggerLoom`), §10.2 (actorAggregate explicit bare-NanoID key column — Option b). **Contract #3 needs NO edit.** Then flip each CAR's "APPLICATION PENDING" → applied.
2. **Arch/component docs:** new `docs/components/bridge.md` (the bridge + the `external.<adapter>` envelope `{instanceKey, adapter, params, replyOp, idempotencyKey, externalRef}` + the result-op determinism contract); update `docs/components/{loom,weaver}.md` (Loom +externalTask, Weaver −nudge); propose marking `lattice-architecture.md` Item 3 (Two-Phase Nudge) superseded (planning-lead-owned).
3. **Propose `epics/phase-2-epics.md` edits** (planning-lead-owned): Epic 10 superseded; new **External I/O Bridge** epic (E.1 CARs done → E.2 Loom externalTask + `external` domain → E.3 bridge + FR58 crash/retry proof → E.4 retire nudge path); Epic 11 re-plan (11.1a done; 11.2 service domain [serviceAccess auth deferred Phase 3]; 11.3 `lease-signing` redesign; 11.4 e2e + `test-lease-convergence` gate).
4. **Commit** the ratified+applied package to `main`, watch CI green.
5. **Then build E.2** (first code story) — check the credit-window gate at launch (5h05m past the prior story start; window mark was 2026-06-17T22:14:11Z).

**Working-tree at handoff:** the 4 CAR files (modified, RATIFIED/application-pending) + this proposal (untracked) are **UNCOMMITTED on `main`**. `gate2/gate3-report.txt` churn is pre-existing, not ours. First-cut `packages/lease-signing/` was discarded. Full design rationale: Sections 0–5 below + memory `project_epic11_credit_gate`.

---

**(original draft status:** for team review, then Andrew's ratification — now RATIFIED above.)

> House-rule note: this is a change-proposal *document*, not a sprint artifact. The project is session-per-story; **no `sprint-status.yaml`**. Approved epic/story changes land in `epics/phase-2-epics.md` (planning-lead-owned) and the contract amendments route through `CONTRACT-AMENDMENT-REQUEST` files. Checklist item 6.4 (update sprint-status.yaml) = **N/A**.

---

## Section 0 — Team Review Outcome & Ratified Decisions (v2, 2026-06-18)

Three independent reviews (PM / adversarial-architect / dev+QA) ran against the v1 draft. **All endorse the direction; all required revisions before ratification.** Verdicts: PM = Endorse-with-changes; Architect = Sound-with-fixes (one BLOCKER); Dev+QA = Buildable-with-changes (and *less* net mechanism than Two-Phase Nudge). The body below is revised to v2 per their findings.

### Ratified decisions (Andrew, 2026-06-18)
1. **Proceed with the bridge now, eyes open.** Acknowledging the PM's framing that generic egress is largely a Phase-3 dividend (Phase 2 has 2 mocked integrations) and that the rejected **Option B** (resolve-payload carries the candidate key, ~3 lines + a §10.3 amendment) would unblock the demo more cheaply — we still build the bridge now, inside the reference vertical whose job is to surface and fix exactly this, rather than ship a §10.3/§10.8 surface we'd retire in Phase 3. Two-Phase Nudge's placement "never sat right."
2. **Loom `externalTask` waits for the external result** (preserves the userTask symmetry), via a **new completion-correlation key** — NOT fire-and-forget.
3. **M2 key-shape → Option (b):** the actorAggregate projection honors an explicit bare-NanoID key column for weaver-targets lenses; the FROZEN §10.2 key + Weaver `splitRowKey` stay untouched (the change lands in the Epic-12 Output-descriptor machinery).
4. **Contract #3 CAR DROPPED:** the `external` event domain needs no contract amendment (ordinary domain under the open `<domain>.<eventName>` model — no Processor allowlist; realized via a package event-type DDL + the bridge consumer). **The package is 4 amendments across 3 files** (loom §10.5/§10.6, weaver §10.3 + §10.8, refractor §10.2).

### Must-fix items incorporated into v2 (from the review)
- **B1 (BLOCKER) — completion correlation re-specified.** v1's "Loom completes on the bridge result-op like userTask, no new field" is mechanically impossible: Loom's pending token is the requestId of the op *Loom itself* submitted, known write-ahead (`internal/loom/engine.go:653-712`, `token.go:20`). Per decision 2, v2 adds a **third Loom correlation key `payload.externalRef` = the instance key Loom mints write-ahead**: the `externalTask` parks on `token.<instanceKey>`; the bridge's result-op carries `externalRef` so `correlationKeys` resolves it. The "no new envelope field" claim is **struck** — this is a real §10.5/§10.6 amendment + a one-key engine extension.
- **M2 — the actorAggregate key seam is a HARD incompatibility, promoted to a gating decision** (was "small; possibly a clarification"). actorAggregate forces the row key to `{actorSuffix}` = `leaseApp.<id>` (type.id, no `IntoKey` escape — `internal/refractor/projection/output.go:157-162`, `driver.go:56`), but Weaver's `splitRowKey` **rejects** anything but a bare NanoID after the first `.` and **drops the row** (`internal/weaver/evaluator.go:508-518`) → the gap never converges. v2 makes this a **§10.2 + engine decision in the gating story** (change `splitRowKey` to accept `type.id` and re-spec §10.2's key, OR extend the actorAggregate projection to honor an explicit key column). This is a **5th contract touch.**
- **M4 — FR58 determinism pinned as a hard invariant:** the bridge result-op requestId **MUST** be `deterministic(idempotencyKey = instanceKey)`, so a redelivered `external.*` event collapses on the Contract #4 `vtx.op.<requestId>` tracker (`internal/processor/step2_dedup.go`). Without it a redelivery double-writes the result. Added to the §10.3 CAR.
- **M3 — retirement bounded:** retire `internal/weaver/nudge/` + the nudge dispatch/recovery branches + `weaver-claims`, but **KEEP the reconciler/sweeper + `weaver-state`** (they reclaim marks for the surviving `triggerLoom`/`assignTask`/`directOp` actions — `internal/weaver/reconciler.go` branches into nudge only at the `ga.Action == actionNudge` check). **Move-then-delete** the `Fake*` adapters; full teardown only **after** the convergence e2e is green. CI-cleanup landmines (the 12.4 lesson): `scripts/verify-kernel.go:275` + `internal/bootstrap/verify.go:168` assert the claims bucket — update in lockstep.
- **m7 — wording corrected:** the `external.*` event is emitted by the **`externalTask`'s instanceOp DDL** via the Processor's event outbox (so the instance commits *before* the event is publishable — NFR-S11 "visible claim before the call" holds structurally), **not** "Loom emits." Loom stays pure by construction (it only writes `loom-state`; the relay publishes ops). The event payload + the `externalRef`/`idempotencyKey` contract live in **package DDL**, raising the validation bar for the bridge-epic stories.
- **Scope honesty (PM):** the core work is **re-homed out of Epic 11** into a dedicated **External I/O Bridge epic** (the `externalTask`, the bridge, the CARs, the nudge retirement); Epic 11 *depends on* it and stays "the demo vertical." Epic 10 is **re-stamped superseded** (the protocol is retired; only the adapter shims + the idempotency principle carry forward). The **serviceAccess/`cap.svc` auth plane is DEFERRED to Phase 3** (charter-consistent — read-path auth is Phase 3); the service-domain build is **scoped to the vertices the demo traverses.** The **FR58 idempotency crash/retry proof is pulled forward** to land *with* the bridge, on a bridge-only harness, not only at the final lease e2e. A dedicated **`test-lease-convergence` CI gate** is added (Gate 2/3/5 don't cover an external-I/O idempotency loop).
- **Note (dev):** the new **bridge service actor** is a kernel/primordial change (a third system identity like Loom/Weaver) that moves the `verify-kernel` assertion count — handled in the bridge story with the verify-script updates in lockstep.

The re-plan in §4D below is the v2 (re-homed) structure. §3/§4A/§4B are revised accordingly.

---

## Section 1 — Issue Summary

**The reference vertical dogfooded the orchestration core and found that external-I/O placement is in the wrong engine.**

Epic 11's job is to prove the package model carries real orchestration content by converging a Loftspace lease application end-to-end. Implementing it (Story 11.1) and reviewing it adversarially surfaced a structural problem that the engine fixtures had masked:

1. **External idempotent calls live in Weaver's Two-Phase Nudge** (Contract #10 §10.3, FROZEN, shipped in Epic 10). The nudge dispatches a one-shot external call from Weaver's stateless lane-1 and records the outcome via a "resolve op."
2. **The resolve op cannot address a candidate entity distinct from the nudge `subject`.** Evidence (subagent investigation, file:line-anchored): the resolve-op payload is hard-coded `{claimId, result, expectedRevision}` (`internal/weaver/engine.go:517-530`); the auth target is `np.subject`, surfaced only as `authContext.target`; and **a Starlark DDL op cannot read `authContext`** — `internal/processor/starlark_runner.go:465-472` binds only `{requestId, lane, operationType, actor, submittedAt, payload}`. So the DDL that should record the result has no channel to learn which vertex to write.
3. **The "proven" nudge fixture never exercised this path.** The load-bearing test is `internal/weaver/nudge_dispatch_internal_test.go` (not the `weaver_e2e_test.go` stub); it sets `subject == candidate` AND captures resolve ops off the wire with **no Processor and no DDL in the loop**. So the nudge → DDL → candidate-write path was untested all along — the reference vertical is the first real consumer.
4. **Deeper finding (Andrew):** embedding external I/O in the *convergence* engine "never sat right." Weaver detects divergence; making it also *execute* external calls smears I/O into detection and forces it to re-implement durable-claim/idempotency/recovery machinery that the *execution* engine (Loom) already has.

**Issue type:** Technical limitation discovered during implementation + architectural-placement correction. Not a requirements change — FR58 (idempotent external calls) stands; *where and how* it is satisfied changes.

**Supporting evidence:** the three subagent investigations (reprojection semantics, nudge addressing, domain-model substrate) recorded in the 11.1 review thread; Contract #10 §10.3/§10.5/§10.6/§10.8; `internal/weaver/{engine,strategist,actuator}.go`; `internal/processor/starlark_runner.go`; `packages/service-location/CONCEPT.md`.

---

## Section 2 — Impact Analysis

### Epic Impact

| Epic | Status | Impact |
|---|---|---|
| **Epic 10 — Two-Phase Nudge (FR58)** | DONE (2 stories) | **Protocol SUPERSEDED** (re-stamp the epic + the memory index). The Two-Phase Nudge *protocol*, `weaver-claims`, the `internal/weaver/nudge/` dispatch + resolve-op machinery, and Contract #10 §10.3/§10.8's nudge surface are **retired**. What carries forward: the **adapter interface + the two `Fake*` shims** (relocated to the bridge) and the **idempotency principle** (now expressed via the instance key). The FR58/NFR-S11 *guarantee* is preserved by the bridge + the service-instance-as-claim — but "reworked, not deleted" was understated: the bulk of Epic 10's shipped protocol is discarded. (Justified: the addressing defect is structural, not a bug; and the §10.3/§10.8 surface would be retired in Phase 3 regardless.) |
| **Epic 11 — Reference Vertical** | IN PROGRESS (trigger) | **Re-planned; core work RE-HOMED out (PM).** The `externalTask`, the bridge, the CARs, and the nudge retirement move to a new **External I/O Bridge** core epic (§4D); Epic 11 *depends on* it and stays the demo vertical (service domain + redesigned `lease-signing` + e2e). 11.1a (pkgmgr install seam) shipped (`5fb3a04`, CI green) and stands. The current 11.1 package code is working-tree-only and will be substantially rewritten. |
| Epics 7–9, 12 | DONE | No direct change. Loom (8) gains a step kind; Weaver (9) loses the nudge action — additive/subtractive within existing components, no epic reopened. |

### Story Impact

- **Epic 10 stories 10.1, 10.2:** superseded in placement. The proposal does **not** roll back their commits wholesale (the adapter framework + idempotency *concepts* are reused); it re-homes them. A follow-up retires the now-dead `internal/weaver/nudge/` dispatch path + `weaver-claims` once the bridge lands.
- **Epic 11:** 11.1 (as written) is withdrawn from "review" and re-scoped; new prerequisite stories are added (see Section 4 re-plan).

### Artifact Conflicts

| Artifact | Conflict | Action |
|---|---|---|
| **Contract #10 §10.3** (operational KV: `weaver-claims`, Two-Phase Nudge) | FROZEN; the nudge claim shape is being retired | **CAR** — deprecate `weaver-claims` + the Two-Phase Nudge protocol; the claim is now the service-instance vertex (Core KV business state). |
| **Contract #10 §10.5/§10.6** (Loom step kinds + completion) | FROZEN; needs a new `externalTask` step kind + its completion correlation | **CAR** — add `externalTask` (dispatch `external.*` event + park; complete on the bridge's result op, correlated by token like userTask). |
| **Contract #10 §10.8** (playbook/GapAction: `nudge` action) | FROZEN; `nudge` action retired | **CAR** — remove/deprecate `nudge`; external remediation is `triggerLoom` of a pattern containing an `externalTask`. |
| **Contract #10 §10.2 + Refractor** (target-row key shape) | FROZEN; actorAggregate emits a `type.id` key that Weaver's `splitRowKey` rejects → row dropped (M2) | **CAR + engine** — accept `<type>.<id>` in `splitRowKey` + re-spec §10.2's key, OR extend actorAggregate to honor an explicit key column. Gating decision. |
| **Contract #3** (events: domains) | `external` is an ordinary domain under the open `<domain>.<eventName>` model — no Processor allowlist | **NO amendment** (dropped at ratification). Realized via a package `external.<adapter>` event-type DDL + the bridge's `events.external.>` consumer; envelope spec → `docs/components/bridge.md`. |
| **`lattice-architecture.md`** (orchestration core; Item 3 Two-Phase Nudge; component list) | external-I/O placement + component inventory change | Planning-lead update: add the **bridge** component; revise the Loom/Weaver external-call narrative; mark Item 3 (Two-Phase Nudge) superseded by the bridge. |
| **`docs/components/weaver.md` / `loom.md`** | component behavior changes | Update: Weaver loses nudge; Loom gains `externalTask`. New `docs/components/bridge.md`. |
| **`packages/service-location/CONCEPT.md`** | the lease type/instance ownership boundary it flagged (lines 96-98) is now being settled | Resolve the boundary: service-location owns service *templates* + spatial graph; the vertical creates *instances*. |
| **PRD** | FR58 wording ("orchestration engine records a visible claim state…") | No change required — FR58 is *satisfied* (more honestly) by the instance vertex; wording stays. Confirm with PM. |

### Technical Impact

- **New component:** `bridge` (generic, trusted-infra egress) — a durable consumer on `events.external.>` that dispatches to a named adapter, calls idempotently, and posts the result op to `core-operations`. Owns the adapter registry.
- **Loom:** new `externalTask` step kind (structurally userTask: emit + park). Loom stays pure — it emits the `external` event through the op's transactional outbox; **no raw NATS handle** (§10.3 invariant preserved).
- **Weaver:** `nudge` action + `weaver-claims` + `internal/weaver/nudge/` dispatch retired (after the bridge lands). Weaver = pure convergence detection → `triggerLoom`.
- **Refractor:** **no change** — multi-vertex convergence uses the existing `actorAggregate` + ActorEnumerator machinery (anchor-type-parameterized; BFS fallback is safe for a non-auth `weaver-targets` lens). Confirmed supported today.
- **Processor:** **no change** — cross-vertex ops, `sensitive` aspects, and event emission are all within the existing model. (Note: a Starlark op still cannot read `authContext`; this proposal *avoids* needing that, rather than adding it.)
- **CI/testing:** new bridge test surface; an e2e that drives the full event loop (Loom externalTask → bridge → result op → reproject) including a crash/retry idempotency proof (the FR58 guarantee, re-homed).

---

## Section 3 — Recommended Approach

**Hybrid: Direct Adjustment (re-plan Epic 11) + targeted Rework of Epic 10 (re-home, don't roll back) + Architecture change (new bridge component + Loom step kind) gated on Contract amendments.**

### The target architecture (the "Bridge")

External calls become **event-driven and symmetric to userTasks** — Loom dispatches to an async completer and parks; the completer is a human (userTask) or the bridge (externalTask):

1. Weaver detects a stale/absent gap (e.g. background check) → `triggerLoom` the execution pattern.
2. Loom pattern: collect inputs (userTask) → **`externalTask`**: Loom submits the step's **instanceOp**, whose **DDL** (a) creates the `service.<x>.instance` vertex (the claim, in Core KV) and (b) emits `external.<adapter> {instanceKey, adapter, params, replyOp, idempotencyKey=instanceKey, externalRef=instanceKey}` via the Processor's event outbox. The event is publishable only *after* the instance commits (NFR-S11). Loom **parks on `token.<instanceKey>`** (the instance key it minted write-ahead — the new `externalRef` correlation key).
3. **Bridge** consumes the event → calls the adapter with `idempotencyKey = instanceKey` → posts the **result op** to `core-operations` with **`requestId = deterministic(instanceKey)`** (a redelivery collapses on the Contract #4 tracker → exactly one result mutation) and `payload.externalRef = instanceKey`.
4. Result op records outcome + `freshUntil` as **aspect(s)** on the instance (D5 — not fat root `data`; see the Appendix refinement) → emits its completion event carrying `externalRef` → Loom's `correlationKeys` GET on `token.<instanceKey>` resolves → Loom **advances** (genuine wait-for-completion; a later step may branch on the outcome). The `actorAggregate` convergence lens reprojects (instance changed) → the gap clears.

**Why this is right (rationale):**
- **FR58 satisfied more honestly.** The "visible claim recorded before the call" *is* the instance vertex — created before the event is even consumed. Its key is the natural idempotency key (one instance = one call); read-before-act on redelivery prevents double-action. The instance unifies `weaver-claims` + the resolve target + the result holder into one auditable business vertex.
- **Clean separation.** Convergence detection (Weaver) vs deterministic execution incl. external I/O (Loom + bridge). External I/O isolated in one purpose-built component; Loom and Weaver stay pure and event-driven — consistent with Lattice's CDC/event-sourced spine.
- **The addressing problem is gone.** Loom created the instance, so its key rides in the event; the bridge posts straight back. No `authContext`-in-Starlark, no resolve-payload hack.
- **Generality.** The bridge is generic: `external.<adapter>` + `{adapter, params, replyOp, idempotencyKey}`. A new external integration is *just a new adapter registration + a Loom pattern* — no new component. "Everything is data/events" preserved.

**Effort:** High. **Risk:** Medium (touches frozen contracts + a completed epic; mitigated by reusing Loom's proven durable-flow machinery and the existing actorAggregate path — net *less* net-new mechanism than Two-Phase Nudge required).

**Alternatives considered & rejected:**
- *Option A (expose `authContext` to Starlark ops):* one-file change but a Contract #3 widening affecting every DDL; softens the "a script writes only keys it was handed" trust model. Rejected.
- *Option B (resolve payload carries the candidate key):* ~3-line Weaver change + §10.3 amendment; keeps external I/O in Weaver — perpetuates the placement smell. Superseded by the bridge (Andrew: "might be moot if we define the model right" — it is).
- *Keep Two-Phase Nudge, narrow lease-signing to avoid candidate≠subject:* loses the realistic domain model + the FR58 demo. Rejected.

---

## Section 4 — Detailed Change Proposals

### 4A. Architecture (new + changed components)

- **NEW component `bridge`** (`internal/bridge/` + `cmd/bridge/`): generic trusted-infra egress. Durable consumer on `events.external.>`; adapter registry (`FakeStripe`, `FakeBackgroundCheck` re-homed from `internal/weaver/nudge/`); idempotent dispatch (idempotencyKey = instance key) with read-before-act recovery; posts result ops to `core-operations` under a bootstrap-provisioned service actor (operator-equivalent, like Weaver/Loom).
- **Loom** (`internal/loom/`): add `externalTask` step kind — emits `external.<adapter>` via the op's transactional outbox + parks; completion correlated by the existing durable `token.<token>` GET on the bridge's result-op completion event; reuses the §10.6 deadline/backstop.
- **Weaver** (`internal/weaver/`): retire `nudge` action, `weaver-claims`, `internal/weaver/nudge/` dispatch (after bridge lands). Weaver = detect → `triggerLoom`.

### 4B. Contract amendments — ratify as ONE coherent package (PM risk #2); route via `CONTRACT-AMENDMENT-REQUEST`; Andrew ratifies

1. ~~**Contract #3 — new `external` event domain.**~~ **DROPPED (Andrew, ratification): no amendment needed.** `external` is an ordinary domain under the open `<domain>.<eventName>` model — the Processor derives the domain from the class's first segment with **no allowlist** (`internal/processor/step7_events.go`), so `external.<adapter>` is admitted with zero Processor code change. Realized as **package data + the bridge**: the instanceOp DDL declares + emits the `external.<adapter>` event-type, the bridge subscribes `events.external.>`, and the envelope spec lives in `docs/components/bridge.md`. **The package is 4 amendments across 3 files** (loom/weaver/refractor).
2. **Contract #10 §10.5/§10.6 — `externalTask` step kind + a 3rd completion-correlation key.** `{kind: "externalTask", adapter, params (row/subject templates), replyOp, instanceOp}`. A **two-op-shaped step** (Loom submits the instanceOp, then parks — not the single-op shape userTask/systemOp assume). Completion via a **new `payload.externalRef` correlation key** = the instance key Loom mints write-ahead (joins `requestId`/`payload.taskKey` in `correlationKeys`); the bridge's result-op carries it. The "no new envelope field" assertion is struck. Deadline backstop applies (§10.6) for a never-completing call.
3. **Contract #10 §10.3 — retire `weaver-claims` / Two-Phase Nudge; pin FR58 determinism.** The claim is the service-instance vertex (Core KV). **Hard invariant: the bridge result-op `requestId = deterministic(idempotencyKey = instanceKey)`** so a redelivered event collapses on the Contract #4 tracker (FR58/NFR-S11). Retire `WeaverClaimsBucket` from provisioning + both `verify-kernel`/bootstrap `verify.go` enumerations in lockstep. **Keep `weaver-state` + the reconciler/sweeper** (they serve the surviving actions).
4. **Contract #10 §10.8 — retire the `nudge` GapAction.** External remediation = `triggerLoom` of a pattern with an `externalTask`.
5. **Contract #10 §10.2 + Refractor — convergence-lens key shape (the M2 seam).** Resolve the actorAggregate `type.id` key vs §10.2 bare-NanoID incompatibility: either (a) change Weaver `splitRowKey` to accept `<type>.<id>` and re-spec §10.2's `<targetId>.<entityId>` key, or (b) extend the actorAggregate projection to honor an explicit key column. Decide in the gating story; do **not** start the convergence lens until this is settled.

### 4C. Domain model (package data — the redesigned vertical)

- **Service templates/instances** (service domain): `vtx.service.<id>` with `class: "service.<x>.template"` (offering; `availableAt` location, `providedBy`, how-aspects) vs `class: "service.<x>.instance"` (a run; `instanceOf` template, `providedTo` applicant; carries result + `freshUntil`). Type is `service` (dot-free); subtype in `class`. Settle service-location ownership: templates + spatial graph = service-location; instances = the vertical.
- **Applicant PII:** SSN, DOB as **separate `sensitive: true` aspect-types on the identity** (`vtx.identity.<id>.ssn`, `.dob`) — extends the proven identity-domain pattern (name/email/phone already sensitive). PRD §358 names this case.
- **Convergence lens:** `leaseApplicationComplete` becomes an **`actorAggregate`** lens, `AnchorType: leaseApp`, multi-hop `MATCH (app)-[:applicationFor]->(id), (id)<-[:providedTo]-(bg:service)`, reading identity aspects + the service instance; reprojects on any constituent change via the link-walking enumerator (BFS fallback, safe). One open seam: the actorAggregate output key expands to `type.id` vs §10.2's bare-NanoID — reconcile (small; possibly a §10.2 clarification).

### 4D. Re-plan — re-homed (PM scope-honesty): a core Bridge epic the vertical depends on

Numbers are indicative; **Andrew owns `epics/phase-2-epics.md`** and assigns the epic number/placement. The core work is **NOT** under Epic 11 — Epic 11 stays "the demo vertical" and depends on the new epic.

**NEW core epic — "External I/O Bridge" (provisional Epic E):** retires Two-Phase Nudge's placement; this is orchestration-core, not vertical.
- **E.1 — Gating contracts.** Ratify the 5 CARs (§4B) as one coherent package + the architecture/component-doc updates (add `bridge`; Weaver loses nudge; Loom gains `externalTask`; mark Item 3 superseded). Settle the M2 key-shape decision here. **Hard gate — nothing builds until the surface is agreed.**
- **E.2 — Loom `externalTask`** step kind (two-op-shaped: instanceOp + park on `token.<instanceKey>`; the new `externalRef` correlation key; deadline backstop) + the `external` event domain provisioning. Reuses the userTask park/token/deadline spine; the two-op dispatch + correlation contract are the net-new design.
- **E.3 — the `bridge` component** (`internal/bridge/` + `cmd/bridge/`): generic durable consumer on `events.external.>`; adapter registry + the `Fake*` adapters **moved** (not copied) from `internal/weaver/nudge/`; read-before-act-on-the-instance recovery (net-new — the claim-store does NOT carry over); the bridge **service actor** (a third primordial identity → moves the `verify-kernel` count, update verify scripts in lockstep). **Includes the FR58 crash/retry idempotency proof on a bridge-only harness (pulled forward — PM risk #1):** `FakeStripe.FailUntil`/`SideEffects==1` under event redelivery + mid-flight-failure recovery.
- **E.4 — retire Weaver's nudge path** (bounded per M3): remove `internal/weaver/nudge/` + the `fireNudge`/`recoverNudge` call sites + the `nudge` strategist case + `weaver-claims` provisioning/consts + both verify enumerations. **Keep the reconciler/sweeper + `weaver-state`.** **Move-then-delete** (the fakes relocate in E.3 first); full teardown only **after** Epic 11's e2e (11.4) is green — never a window where neither path works. Gate-3 convergence stays DEFENDED with the nudge gone (explicit AC). `grep -rn "weaver-claims\|nudge" scripts/ Makefile .github/ internal/bootstrap/` as an acceptance step.

**Epic 11 — Loftspace Reference Vertical (re-planned; depends on Epic E):**
- **11.1a** — pkgmgr orchestration-content install seam. **DONE** (commit `5fb3a04`, CI green). Stands.
- **11.2 — Service domain foundation.** `service` vertex type + `template`/`instance` classes + `availableAt`/`providedBy`/`instanceOf`/`providedTo` links + lifecycle ops, **scoped to the vertices the lease demo traverses** (PM cut-down). **serviceAccess/`cap.svc` auth plane DEFERRED to Phase 3** (charter — read-path auth is Phase 3). Settle the service-location ownership boundary (templates+spatial graph = service-location; instances = the vertical).
- **11.3 — `lease-signing` redesigned.** Identity `sensitive` aspects (SSN/DOB, separate aspect-types, extends identity-domain); the `actorAggregate` `leaseApplicationComplete` lens (multi-hop, reading identity aspects + the service instance); `triggerLoom` bgcheck/payment patterns (each containing an `externalTask`); signing. **The lens is testable via direct instance-vertex writes — does not serialize behind the bridge** (PM/dev note).
- **11.4 — e2e convergence harness.** Drain-then-assert; the full loop (gap → triggerLoom → externalTask → bridge → result op → reproject → gap clears, steady-state); add the **`test-lease-convergence` CI gate** (Gate 2/3/5 don't cover an external-I/O idempotency loop).

### 4E. Stories withdrawn / reworked

- Current **11.1** (`lease-signing` as written, leaseApp-scalar model, Weaver nudge) — **withdrawn from review**; superseded by 11.B–11.E. Working-tree code retained as reference only (not committed).

---

## Section 5 — Implementation Handoff

**Scope: MAJOR → fundamental replan with Architect + PM involvement.**

| Role | Responsibility |
|---|---|
| **Architect (Winston)** | Own the four CARs + architecture-doc updates; the bridge + Loom-step design; adjudicate the actorAggregate key seam. |
| **PM (John)** | Confirm FR58 wording stands; ratify the Epic 11 re-plan against Phase-2 goals; confirm no PRD MVP regression (the demo still proves FR26/27/29/30/58). |
| **Planning lead (Andrew)** | Ratify the CARs (frozen-contract amendments); update `epics/phase-2-epics.md` (Epic 10 reworked note + Epic 11 re-plan); settle service-location ownership boundary. |
| **Dev (Amelia)** | Implement 11.B–11.F per ratified surface, story-by-story, each through the 3-layer review + CI-green gate. |

**Success criteria:** the lease application converges end-to-end from `InstallPackage` on a minimal core; Loom `externalTask` → bridge → result-op → reproject closes the bgcheck/payment gaps; a retried external call does not double-act (FR58); Two-Phase Nudge is retired with no convergence regression (Gate 2/3/5 green).

**MVP impact:** none to scope — the same Phase-2 FRs are covered. The reference vertical's *cost* rises (it's now a real orchestration-core validation), but that is its purpose; the *guarantees* are unchanged or strengthened.

---

## Checklist findings (condensed)

- **§1 Trigger:** Done. Story 11.1; technical-limitation + placement correction; evidence = 3 subagent investigations + contract/code file:line refs.
- **§2 Epic impact:** Done. Epic 10 reworked (re-home, not rollback); Epic 11 re-planned/expanded; 7–9/12 additive-only.
- **§3 Artifacts:** Done. Contracts #10 (§10.3/5/6/8) + #3 → CARs; architecture + component docs; service-location CONCEPT boundary; PRD unaffected; UX = N/A (no UI surface).
- **§4 Path:** Hybrid selected (re-architect + re-plan + re-home). Options A/B/narrow-scope considered and rejected with rationale.
- **§5 Proposal components:** this document.
- **§6 Handoff:** Major → Architect/PM/planning-lead/dev; 6.4 (sprint-status.yaml) = **N/A** (no sprints).

---

## Appendix — ready-to-apply edits for planning-lead-owned artifacts (E.1 step 3)

These artifacts are **planning-lead-owned** (`epics/phase-2-epics.md`, `lattice-architecture.md`); Winston does
**not** edit them while implementing. Below is the concrete, paste-ready text for **Andrew** to apply. (E.1 steps
1–2 — the Contract #10 amendments, the CAR flips, `docs/components/bridge.md`, and the `loom.md`/`weaver.md`
updates — are **already applied** in the same working tree.) Andrew owns the epic number/placement; "Epic E" is a
placeholder.

> **Two architecture refinements (Andrew, 2026-06-18) apply to ALL story drafts below** and supersede the
> illustrative "service-instance / writes outcome onto the instance" phrasing in §3/§4 above (which used the lease
> demo's vertex): **(1) the claim vertex's type is package-chosen — the bridge is type-agnostic** (`service.<x>.instance`
> is one package's choice, not a platform constraint; it operates on the opaque `instanceKey`/`externalRef` token +
> the `replyOp` DDL). **(2) The external outcome is recorded as aspect(s) on the claim vertex per D5** (business data
> lives in aspects; vertex root `data` stays minimal — at most a justified lifecycle scalar), **never** fat root
> `data`. Bridge idempotency = the deterministic result-op `requestId` + the adapter's `idempotencyKey` dedup (an
> optional skip-on-redelivery uses the **generic op tracker**, not a typed-vertex read). The E.2–E.4 / 11.x ACs must
> reflect both.

### A. `epics/phase-2-epics.md`

> **⚠️ SUPERSEDED 2026-06-18 (post party-mode + Andrew's decisions) — the final breakdown is now APPLIED in `epics/phase-2-epics.md`.** Final decisions: **Epic 10 SUPERSEDED**; **Epic 11 CLOSED** (11.1a done; 11.1/11.2 **won't-do** — re-homed); **Epic 12 DONE**; **NEW Epic 13 — External I/O Bridge** (was "Epic E": 13.1 done → 13.2 Loom externalTask → 13.3 bridge service actor → 13.4 bridge component+FR58 proof → 13.5 retire nudge, *blocked until 14.5 green*); **NEW Epic 14 — Loftspace Lease-Application Reference Vertical** (14.1 service domain → 14.2 refractor key-column [Option b] → 14.3 identity sensitive PII aspects → 14.4 convergence lens+externalTask patterns → 14.5 e2e, *held to the end*). Party-mode recommendations R1–R8 applied. **The A1–A4 text below is the pre-review draft, retained for history.**

**A1 — Epic 10 header (line ~302): mark SUPERSEDED.** Insert directly under `## Epic 10: External Convergence — Two-Phase Nudge`:

> **⚠️ SUPERSEDED 2026-06-18 (External I/O Bridge change proposal).** The Two-Phase Nudge **protocol** is retired:
> external idempotent I/O moves out of Weaver into **Loom's `externalTask` step + the new generic `bridge`
> component**. What **carries forward**: the adapter interface + the two `Fake*` shims (relocated to the bridge) and
> the idempotency *principle* (now the service-instance key). Stories 10.1/10.2 shipped and are not rolled back;
> the `internal/weaver/nudge/` dispatch path + `weaver-claims` are torn down in the Bridge epic's E.4. See
> `docs/contracts/10-orchestration-surfaces.md` §10.3/§10.8 (amended 2026-06-18) and `docs/components/bridge.md`.

**A2 — NEW core epic (place before Epic 11; Andrew assigns the number).** Paste-ready block:

```
## Epic E: External I/O Bridge (orchestration core)

**Goal:** External idempotent I/O is event-driven and symmetric to userTasks — Loom dispatches an `externalTask`
and parks; a generic (vertex-type-agnostic) `bridge` component executes the call idempotently and posts the result
back; the FR58 visible-claim is a package-chosen claim vertex in Core KV (outcome in aspect(s), D5). Retires
Two-Phase Nudge's placement.
**FRs covered:** FR58 (+ NFR-S11), re-homed from Epic 10.

### Story E.1: Gating contracts + architecture/doc updates  — DONE 2026-06-18
Ratify + apply the External I/O Bridge amendment package (Contract #10 §10.2/§10.3/§10.5/§10.6/§10.8; Contract #3
needs no change), settle the M2 key-shape (Option (b)), and land the component-doc updates (new bridge.md; Loom
+externalTask; Weaver −nudge). **Hard gate — nothing builds until the surface is agreed.**
*Depends on: — · Grounding: sprint-change-proposal-2026-06-18.md; CARs in cmd/{loom,weaver,refractor}.*

### Story E.2: Loom `externalTask` step kind
The two-op-shaped step (submit instanceOp → park on `token.<instanceKey>`; the new `payload.externalRef`
correlation key; deadline backstop) + the `external` event-domain provisioning. The `instanceOp` creates the claim
vertex (**package-chosen type** — engine stays type-agnostic) and emits the `external.<adapter>` event via its
outbox; the `replyOp` records the outcome as **aspect(s)** (D5). Reuses the userTask park/token/deadline spine; the
two-op dispatch + correlation contract are the net-new design. **AC:** the engine hardcodes no vertex type; the
outcome is never written to root `data`.
*FRs: FR58 · Depends on: E.1 · Model: Opus · Grounding: Contract #10 §10.5/§10.6; docs/components/loom.md.*

### Story E.3: The `bridge` component
`internal/bridge/` + `cmd/bridge/`: generic, **vertex-type-agnostic** durable consumer on `events.external.>`
(treats `instanceKey`/`externalRef` as an opaque token + the `replyOp` as package DDL — **AC: no hardcoded vertex
type**); adapter registry + the `Fake*` adapters **moved** (not copied) from `internal/weaver/nudge/`; idempotency =
the deterministic result-op `requestId` + the adapter `idempotencyKey` dedup, with an **optional** skip-on-redelivery
via the **generic Contract #4 op tracker** (`vtx.op.<det-reqId>`) — **not** a typed claim-vertex read; the bridge
**service actor** (a third primordial identity → moves the verify-kernel count, update verify scripts in lockstep +
bootstrap-file version bump). **Includes the FR58 crash/retry idempotency proof on a bridge-only harness**
(`FakeStripe.FailUntil` / `SideEffects==1` under event redelivery + mid-flight-failure recovery).
*FRs: FR58, NFR-S11 · Depends on: E.2 · Model: Opus · Grounding: docs/components/bridge.md, service-actors.md.*

### Story E.4: Retire Weaver's nudge path
Remove `internal/weaver/nudge/` + the `fireNudge`/`recoverNudge` call sites + the `nudge` strategist case +
`weaver-claims` provisioning/consts + **both** verify enumerations. **Keep the reconciler/sweeper + `weaver-state`.**
Move-then-delete (the fakes relocated in E.3); full teardown only **after** Epic 11's e2e (11.4) is green — never a
window where neither path works. Gate-3 convergence stays DEFENDED with the nudge gone (explicit AC).
`grep -rn "weaver-claims\|nudge" scripts/ Makefile .github/ internal/bootstrap/` as an acceptance step.
*FRs: FR58 (no regression) · Depends on: E.3, 11.4 green · Model: Opus · Grounding: Contract #10 §10.3/§10.8.*
```

**A3 — Epic 11 re-plan (replace the current 11.1/11.2 stories, lines ~339–376).** New structure (depends on Epic E):

- **11.1a — pkgmgr orchestration-content install seam. DONE** (commit `5fb3a04`, CI green; insert as a done story).
- **11.2 — Service domain foundation.** `service` vertex type + `template`/`instance` classes +
  `availableAt`/`providedBy`/`instanceOf`/`providedTo` links + lifecycle ops, **scoped to the vertices the lease
  demo traverses**. **serviceAccess / `cap.svc` auth plane DEFERRED to Phase 3** (charter — read-path auth is
  Phase 3). Settle the service-location ownership boundary (templates + spatial graph = service-location; instances
  = the vertical). The instance carries its external-call outcome (+ any `completedAt` the freshness predicate reads)
  in **aspect(s)** per D5 — root `data` stays minimal (at most a lifecycle `status` scalar).
- **11.3 — `lease-signing` redesigned.** Identity `sensitive` aspects (SSN/DOB, separate aspect-types, extends the
  identity-domain pattern); the `actorAggregate` `leaseApplicationComplete` lens (multi-hop, reading identity
  aspects + the claim vertex's **outcome aspect**, §10.2 Option (b) key column); `triggerLoom` bgcheck/payment
  patterns (each containing an `externalTask`); signing. The lens is testable via direct writes of the instance's
  **outcome aspect** — does not serialize behind the bridge.
- **11.4 — e2e convergence harness.** Drain-then-assert; the full loop (gap → triggerLoom → externalTask → bridge →
  result op → reproject → gap clears, steady-state); add the **`test-lease-convergence` CI gate** (Gate 2/3/5 don't
  cover an external-I/O idempotency loop). Replaces the prior 11.2; updates the "proven to converge" wording from
  "Two-Phase Nudge" → "Loom `externalTask` + the bridge".

  Also update the Epic 11 stale gap-action mentions (lines ~356, ~365, ~370): "Two-Phase Nudge bgcheck / payment"
  → "`triggerLoom` of an `externalTask` bgcheck / payment pattern".

**A4 — Phase 2 Story Total table (lines ~580–582).** Re-stamp the Epic 10 row "External Convergence — Two-Phase
Nudge | 2 | **SUPERSEDED → re-homed to Epic E (bridge)**"; add the Epic E row (4 stories); change the Epic 11 row
to 4 stories (11.1a done + 11.2 service domain + 11.3 lease-signing redesign + 11.4 e2e). Note Epic E lands
**before** Epic 11's code stories (11.2+), since 11.3 depends on `externalTask`/bridge.

### B. `lattice-architecture.md`

**B1 — Item 3 (line 954): mark SUPERSEDED.** Insert under `### Item 3: Two-Phase Nudge — External Operation Idempotency (FR58)`:

> **⚠️ SUPERSEDED 2026-06-18 by the External I/O Bridge** (sprint-change-proposal-2026-06-18.md). FR58 is unchanged
> and still satisfied — but the *visible claim* is now the **service-instance vertex in Core KV** (created by the
> `externalTask`'s instanceOp before the `external.*` event is publishable), **not** a `weaver.claims.>` record;
> external I/O lives in **Loom + the bridge**, not Weaver. The `weaver.claims.>` bucket is retired (B3 below). The
> claim→execute→resolve *principle* survives as instance-create → bridge-call → result-op; the determinism is pinned
> as `result-op requestId = deterministic(instanceKey)`. See Contract #10 §10.3/§10.5/§10.6 and
> `docs/components/bridge.md`.

**B2 — OI-1 (line 1256): mark largely RESOLVED.** Insert under `### OI-1 — Async external-call result-return`:

> **Largely RESOLVED 2026-06-18 by the External I/O Bridge.** Sub-question 1 (async result-return) is answered: the
> `externalTask` → `external.*` event → bridge → **result op** → instance-reproject loop **is** an event-plane
> async-result-return mechanism (the bridge's reply op is the inbound result driving "Resolve"; a vendor webhook/poll
> becomes a bridge-internal adapter detail, not an engine change). Sub-question 2 (richer params — SSN/DOB) is
> addressed by the redesigned vertical's **identity `sensitive` aspects** (Epic 11.3) feeding the `externalTask`
> `params`. Real-vendor transport remains Phase 3, but the platform shape is no longer an open architectural item.

**B3 — KV Bucket Taxonomy (line ~77).** The `**Weaver Claims KV** | … | weaver.claims.> …` row → append
"**(RETIRED 2026-06-18 — External I/O Bridge; FR58 claim is now the service-instance vertex in Core KV).**" Keep
`weaver-state` unchanged.

**B4 — Component tree + external-action prose (lines ~676, ~1141, ~1197).** `nudge/  # Two-Phase Nudge, external
adapters` → note the adapters move to `internal/bridge/` (E.3); "external actions take the **Two-Phase Nudge**
path" / "Two-Phase Nudges (external)" → "external actions take the **`externalTask` → bridge** path"; add the
**bridge** to the component inventory.

---

**(End of E.1 follow-through. Next code story: E.2 — Loom `externalTask`. Credit-window gate cleared
2026-06-18T03:19Z.)**
