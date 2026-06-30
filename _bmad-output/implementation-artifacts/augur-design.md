# The Augur — Weaver's AI-assisted reasoning tier for unplannable convergence gaps — design

**Status: ✅ Andrew-ratified (2026-06-27) — build now.**
**Component:** Weaver (convergence engine) · reuses the **bridge** external-I/O path + the Processor's op core
**Backlog row:** Lattice lane → *AI-native → the Augur (L3 evaluator)*
**Author:** Winston (Designer fire, 2026-06-27)

> **Naming (Andrew, 2026-06-27):** the feature is the **Augur** — it interprets an ambiguous/novel
> convergence gap and *proposes* a remediation for a human to approve (an augur counsels; the human
> decides). **"L3" stays the internal *evaluator-tier* label** (weaver.md L1/L2/L3 taxonomy); the Augur is
> the feature that *implements* the L3 tier. The marquee AI-native flagship is not called "L3."
>
> **Ratification decisions (Andrew, 2026-06-27):**
> 1. **Build now** — ratified as a strategic investment in the AI-authored-capabilities loop (AI proposes →
>    deterministic validate → human gate → Processor writes), de-risked at the smallest safe surface. (The
>    near-term *utility* in the current verticals is thin — most gaps are deterministically handle-able and a
>    known failure mode should be a deterministic fallback; the Augur is the catch-all for the *un*-enumerable
>    and the proving ground for the AI loop.)
> 2. **Autonomy fork — Option A now, Option B designed-not-built.** Ship human-in-the-loop always (Fires 1–2);
>    the `autoApply` auto-dispatch (Fire 3) is **designed but NOT built** until Andrew ratifies the autonomy
>    boundary. (Don't build disabled auto-apply code — dead scaffolding.)
> 3. **Default model `claude-opus-4-8`** (not sonnet-for-cost — don't downgrade for cost; the Augur is
>    intelligence-sensitive + low-frequency, so reasoning quality dominates). Sonnet is an explicit opt-in.
> 4. **Contract #10 §10.8 (the `augur` block) — ratified + committed** (2917e0f).
>
> **Claude-API grounding corrections folded in** (re-review against the `claude-api` reference): the
> proposal is constrained via **structured outputs** (`output_config.format`, `messages.parse()`) or strict
> tool use (`strict:true` + `additionalProperties:false` + `required`), honoring the JSON-schema limits (no
> recursive/numeric/length constraints); the reasoning call uses **adaptive thinking + `effort: high`/`xhigh`**,
> streams internally, and **handles `stop_reason:"refusal"`** → store the proposal `invalid`/escalate, never
> crash. And the tier framing below is reconciled to **weaver.md's actual tiers** (L1 = re-confirm/dedup,
> L2 = hydrate/classify/select-playbook, L3 = AI reasoning) — *not* the brainstorm "L1-cypher/L2-Starlark" gloss.

---

## Build status — in-flight checkpoint (resume here)

**Fire 1 lives in an unmerged worktree** — branch `augur-fire1` (worktree `../lattice-augur*`), **NOT merged
to `main`**. Latest HEAD **`fcc4e29`**. Resume by `cd`-ing into that worktree; merge to `main` only when the
whole of Fire 1 is green + reviewed.

Steps complete in the worktree (each its own commit on `augur-fire1`):
- foundation + adapter layer + reply-path reconciliation + target-policy parse/validate
- (3b′) the augur op + policy reshape — `2a5f3a2`
- (3b″) the Weaver escalation dispatch (`evaluator.go` `dispatchGap` `!ok` → `augurEscalation`) — `86a912b`
- step 4a — rebase current onto `main` + Option-F doc reconciliation + auth grounding — `fb30c3d`
- step 4b — `scripts/verify-package-augur.go` gate + CI wiring — `153d586`
- step 5 — the `augurProposals` flat nats-kv read-model review lens (P5 human-in-the-loop surface) — `803c092`
- step 6 — `make test-augur-convergence` ephemeral-stack e2e + adversarial gate — `fcc4e29`

**Fire 2a** — the `ReviewProposal` human-verdict op — **SHIPPED to `main`** (`3dbd049`).

**Next:** merge the `augur-fire1` worktree to `main` (Fire 1), then continue Fire 2b+ per §8.

## For Andrew (ratify in one look)

**What it does, in two lines.** Today a Weaver target that projects a `violating` gap the package
playbook can't plan (a `missing_<g>` column with no `gaps[col]` entry, or a gap whose retry budget is
spent) **fails closed** — a loud `GapWithoutPlaybook`/"needs human escalation" Health issue and the
gap just sits there. L3 turns that dead-end into a **reasoned, human-reviewable proposal**: Weaver
escalates the stuck gap to a Claude reasoning call (carried over the *existing* `triggerLoom →
externalTask → bridge` external-I/O path — Weaver never calls an LLM directly), the model proposes a
remediation **constrained to the platform's real action catalog**, and the proposal is recorded as a
`vtx.augurProposal` vertex pending **human approval** before anything dispatches. AI **proposes**;
a deterministic validator + a human gate **govern**; the Processor stays the sole writer (P2 intact).
This is the bounded, safe first step of the marquee "AI-authored capabilities" vision (brainstorm
#592 "AI Handshake protocol"). It implements Weaver's **L3 evaluator tier** — the third, escalating tier
of the evaluator per `docs/components/weaver.md`: **L1** re-confirms the row is still violating + dedups
in-flight; **L2** hydrates context, classifies the gap, and selects the playbook action; **L3** (the Augur)
is AI reasoning for the ambiguous/novel gaps L1/L2 can't classify or map to a playbook.

**Frozen-contract change: ONE — Contract #10 §10.8 (staged UNCOMMITTED in `main`).** L3 adds an
**additive, opt-in** `augur` policy block to the `meta.weaverTarget` package shape (which stuck-gap
triggers escalate; the reasoning pattern to call; the autonomy posture). The frozen `gaps`/templating
shapes are **untouched**; a target with no `augur` block behaves exactly as today (fail-closed). The diff
in `docs/contracts/10-orchestration-surfaces.md` **is** the proposal — affected consumers: the Weaver
engine (new escalation branch) and the `lease-signing`/new `augur` package data. No other
contract changes: the `external.augur` adapter + envelope are **package/bridge data** (§10.5
"the `external` domain is ordinary"), the proposal vertex + its ops are **package DDL**, and the new
Health metrics are **author-discretion** under §5.4.

**One architectural fork for your call (designed through, recommendation given).**
**The autonomy boundary — may Weaver *ever* auto-dispatch an AI-proposed remediation without a human?**
- **Option A — Human-in-the-loop always.** Every L3 proposal waits for an explicit human `approve`
  before Weaver dispatches it. Simple, maximally safe, fully auditable; the cost is operator latency on
  every novel gap.
- **Option B — Confidence-gated auto-apply, opt-in per target (RECOMMENDED as the *design*, gated on
  your ratification to *enable*).** A per-target `augur.autoApply` allow-list (specific low-risk action
  classes) + a model-confidence floor + **mandatory deterministic validation** lets a proposal
  auto-approve and dispatch with **no human gate** — still recorded, audited, and reversible. Default
  **off**; a target ships human-in-the-loop until an operator opts a specific gap class in.

**Recommendation: build A now (Fires 1–2), design B fully but ship it dark behind your sign-off
(Fire 3).** This mirrors the vault design's Phase-A-now / Phase-B-gated split. A delivers the entire
value chain — stuck gaps become reasoned, reviewable, dispatchable proposals — with zero autonomous
mutation, so it is ratifiable on its own merits. B is the autonomy dial; it is a **principal-architect
safety-posture call** (the whole AI-native direction's blast radius), so the *mechanism* is designed
here but stays disabled until you ratify the boundary. **Your two calls:** (1) ratify Fires 1–2
(human-in-the-loop L3); (2) decide whether/when to enable Fire 3's auto-apply, and with what default
caps.

Everything else is resolved in the body. Nothing here blocks the **Lattice Steward** except your
ratification + the autonomy-boundary decision.

---

## 1. Problem & intent

**The vision.** The brainstorming inventory frames Weaver's evaluator as **tiered intelligence —
`L1` cypher / `L2` Starlark / `L3` AI** (brainstorm #375), and names the **AI Handshake protocol**
as a Loom/Weaver-owned surface (#592). The feature backlog carries it as *AI-native → L3 evaluator*
(★★, M–L): *"Weaver's AI-assisted reasoning tier for ambiguous / novel convergence gaps (L1 / L2
ship today)."* `docs/components/weaver.md` lists L3 in the evaluator-tier table as **deferred →
Phase 3**, and again under "Deferred (Phase 3+)": *"L3 evaluator (AI-assisted)."* It is the bounded,
buildable on-ramp to the marquee **AI-authored capabilities** item (★★–★★★): *"A Lattice-aware agent
proposes DDL / Starlark / lenses / workflows through human review + deterministic validation +
rollback-friendly contracts."* L3 establishes exactly that pattern — **AI proposes, deterministic
validation + human review govern** — at the smallest safe surface (one stuck convergence gap),
before the platform trusts an agent to author capabilities.

**The concrete gap in the live engine.** Weaver today is **fully deterministic**. A gap is a
`missing_<g>` boolean column the target Lens projects (§10.2); the package playbook maps that column
to a fixed action via `target.Gaps[col]` (§10.8). Read the two real dead-ends in
`internal/weaver/evaluator.go` + `strategist.go`:

1. **Unplannable gap — `GapWithoutPlaybook`.** A row column `missing_*: true` with **no** `gaps[col]`
   entry is alerted as an `error` and **skipped forever** (`dispatchGap`, evaluator.go:148–155;
   §10.8 "a config error → alert, never silently skipped"). The convergence target can *detect* the
   discrepancy but has **no procedure** to close it. Today the only resolution is a human re-authoring
   the package playbook. This is precisely a "novel discrepancy the package author did not anticipate."
2. **Exhausted gap — spent retry budget.** A gap whose `weaver-state` dispatch-count reaches
   `maxretries_<g>` becomes the operator-visible **"needs human escalation"** terminal
   (`gapSuppressed`, evaluator.go:381–421; weaver.md "Dispatch suppression"). The deterministic
   playbook tried its one fixed action `maxretries` times and gave up. Today nothing reasons about
   *why* it's stuck or what to try instead.

In both cases the platform has rich context — the violation row, the candidate's subgraph, the full
catalog of installed remediation primitives (patterns, ops, their self-describing DDL schemas) — and a
person eventually reasons over it by hand. **L3 is that reasoning step, done by Claude, captured as a
reviewable proposal, and (once approved) dispatched through Weaver's existing machinery.** It does not
replace the deterministic L1/L2 path — it is the **fallback the deterministic path escalates to when
it is genuinely stuck**, opt-in per target.

**Non-goals (explicitly out).** L3 does **not**: invent new *action types* (it proposes only within
`{triggerLoom, assignTask, directOp}` against the **installed** catalog); author DDL / Starlark /
lenses (that is the larger "AI-authored capabilities" item this de-risks); make Weaver call an LLM
in-process (the call goes through the bridge); or bypass the Processor / capability boundary (every
mutation is an op). It is a **reasoning + proposal-capture** tier, with a deliberately small action
surface.

---

## 2. Why this is well-grounded (every mechanism it needs already ships)

L3 is unusual in how little *new platform mechanism* it requires — it is almost entirely **composition
of shipped primitives**:

| L3 needs | Already shipped | Reused how |
|---|---|---|
| An outbound call to an external system (the LLM) | **The bridge** — the one component that makes outbound calls, with durable claim / idempotency / recovery (`docs/components/bridge.md`) | A new `external.augur` **adapter** (package/bridge data — the `external` domain is ordinary, §10.5). Weaver dispatches it via `triggerLoom` of an `externalTask` pattern, exactly like `backgroundCheck`/`collectPayment`. |
| To dispatch that call from a convergence gap | **`triggerLoom` of an `externalTask`** (§10.8 + §10.5/§10.6) — the post-13.1 external-remediation path | The stuck gap's L3 escalation **is** a `triggerLoom` of a `augur` pattern. Anti-storm mark, OCC, lease, reconciler-sweep recovery all apply unchanged — the gap is just another dispatch. |
| To record the model's answer as durable state | **The op core + transactional outbox** (Processor, P2) | The bridge's `replyOp` (`RecordProposal`) DDL creates a `vtx.augurProposal` vertex — an op through the Processor, never a direct write. |
| Operators to review proposals | **Lens read-models (P5) + Loupe (inspector)** | A `augur-proposals` review lens (package DDL) projects pending proposals; Loupe renders + acts on them. A missing read-model is **package work**, not a platform gap. |
| To dispatch an *approved* proposal | **Weaver's existing Strategist/Actuator + mark machinery** | An approved proposal projects as a synthesized dispatch row; Weaver fires it through the same `buildPlan → fireEpisode → act.submit` path, re-validated. |
| Structured, schema-constrained model output | **Claude structured output / tool-use** (latest models) | The `augur` adapter forces the model to return `{action, params, rationale, confidence}` conforming to a JSON schema built from the action catalog. |

The **only** genuinely new platform surface is the **`augur` policy block on `meta.weaverTarget`** (the
one frozen-contract touch, §6) and the small **escalation branch** in the Weaver engine. Everything
else is a new *package* (`augur`: the adapter, the pattern, the proposal vertex type + ops,
the review lens) — the design honors the "new capability = a package" rule (architectural decision:
minimal-core + everything-is-a-package).

**Invariants honored (checked explicitly):**
- **P2** — every mutation (proposal create, review flip, approved dispatch) is an op via the
  Processor. Weaver's only direct write stays `weaver-state` (the escalation's anti-storm mark).
- **P5** — operators read proposals via the `augur-proposals` lens read-model; Loupe (the sole
  inspector exception) may read Core KV directly. No vertical app scans Core KV.
- **P1** — a proposal is **business/meta state → a Core KV vertex**. The in-flight reasoning *call*
  is **operational** → the bridge claim + the `weaver-state` escalation mark (outside Core KV).
- **"Weaver never reaches an external system itself"** (weaver.md) — the LLM call lives in the bridge.
- **Contract #1 key-shapes** — proposal vertex `vtx.augurProposal.<NanoID>`; 4-seg aspects; 6-seg
  links reading "source relation target" with the later-arriving proposal as source (§3.1).

---

## 3. The shape

### 3.1 Data model — the proposal vertex (package DDL, `augur`)

A proposal is a first-class, auditable, queryable artifact → a Core KV vertex (P1). Key shape per
Contract #1:

```
vtx.augurProposal.<NanoID>                              # the proposal (later-arriving = source)
  vtx.augurProposal.<id>.gap          { targetId, entityId, gapColumn, trigger }   # what was stuck
  vtx.augurProposal.<id>.proposed     { action, params }      # the model's remediation (constrained)
  vtx.augurProposal.<id>.rationale    { text }                # the model's reasoning (audit)
  vtx.augurProposal.<id>.confidence   { score }               # 0..1 self-reported confidence
  vtx.augurProposal.<id>.provenance   { model, promptHash, catalogHash, reasonedAt }
  vtx.augurProposal.<id>.review       { state, reviewedAt, dispatchedAt }
                                       # state ∈ {pending, approved, rejected, auto_approved,
                                       #          dispatched, invalid, superseded}

lnk.weaverProposal.<id>.forCandidate.<type>.<entityId>   # proposal forCandidate candidate
lnk.weaverProposal.<id>.forTarget.meta.<weaverTargetId>  # proposal forTarget target
lnk.weaverProposal.<id>.reviewedBy.identity.<reviewerId> # proposal reviewedBy reviewer (on review)
```

Direction follows §1.1 — the proposal arrives after the candidate and the target, so it is the
**source** of every link; the names pass the sentence test ("proposal forCandidate candidate",
"proposal reviewedBy reviewer"). Relationships are **links**, not `data` refs. Every reader filters
tombstones.

`review.state` lifecycle (each transition is an op; terminal states are auditable):

```
            ┌──────────── reject ───────────► rejected
pending ────┤
            ├── approve ──► approved ── dispatch ──► dispatched
            └── (validator fails at record time) ──► invalid
   auto_approved ── dispatch ──► dispatched          (Fire 3 only, gated)
   (a newer proposal for the same (target,entity,gap)) ──► supersedes the older ──► superseded
```

### 3.2 Write path (P2) — three new package ops

| Op | Submitted by | Effect (Starlark DDL) |
|---|---|---|
| `RecordProposal` | the bridge's `replyOp` (the `augur` externalTask result) | **Deterministic validation gate** (§5), then create `vtx.augurProposal.<id>` (`review.state = pending`, or `invalid` if validation fails) + its `forCandidate`/`forTarget` links. `id` is **deterministic** from `(targetId, entityId, gapColumn, escalation episode)` so a redelivered reply collapses on the existing vertex (Contract #4 tracker + `CreateOnly` backstop), never duplicating a proposal. |
| `ReviewProposal` | a human operator (via Loupe / `lattice weaver` CLI), authorized by capability | Flip `review.state` `pending → approved \| rejected`; write `reviewedBy` + `reviewedAt`. Re-runs the deterministic validator on approve (a catalog can change between propose and approve) → `invalid` if it no longer validates. |
| `DispatchProposal` *(internal, optional)* | Weaver's actuator on an approved proposal | Flip `review.state` `approved → dispatched`; stamps `dispatchedAt`. (Or fold the flip into the proposed op's own commit — see §3.4.) |

Op types are **package DDL** registered under `ops.meta.>` — the generic Processor accepts them; no
contract change (architectural decision: ops are package data).

### 3.3 The escalation — `triggerLoom → externalTask → bridge` (the read/dispatch path)

When the lane-1 handler hits an **unplannable** gap (today's `GapWithoutPlaybook` at evaluator.go:148)
**and** the target's `augur` policy escalates that trigger, instead of (only) alerting, Weaver dispatches
an L3 escalation — which is *just another gap dispatch*, so it inherits the anti-storm mark, OCC, lease,
and reconciler-sweep recovery unchanged:

```
unplannable gap on a violating row, target.augur enabled
  → Weaver CAS-creates the weaver-state mark <targetId>.<entityId>.<gapColumn>  (anti-storm; one
       reasoning call per stuck gap per anti-storm window — the model is not re-asked on every CDC tick)
  → Weaver fires triggerLoom of the target.augur.pattern (default: a primordial `augur`
       pattern) whose body is an externalTask:
         { kind: externalTask, adapter: "augur",
           params: { targetId, entityId, gapColumn, row, contextRef },   # resolved from the row
           replyOp: "RecordProposal", instanceOp: "CreateReasoningClaim" }
  → the instanceOp commits the claim vertex (FR58 "visible claim before the call"), emits
       external.augur, the engine PARKS  (all standard externalTask machinery, §10.6)
  → the bridge's augur adapter:
       (a) reads the ACTION CATALOG from a read-model lens (op/pattern self-description:
           inputSchema / fieldDescription / examples — the same DDL self-description Loupe renders
           op-forms from), filtered to what Weaver's service-actor may submit;
       (b) hydrates the candidate's context (the row + a bounded subgraph projection named by contextRef);
       (c) calls Claude with a structured-output schema constrained to {action ∈ catalog, params ∈
           that action's schema, rationale, confidence};
       (d) returns {action, params, rationale, confidence} as the externalTask Result.
  → the bridge posts replyOp RecordProposal → the Processor validates (§5) + creates the
       vtx.augurProposal vertex (pending) → orchestration.externalTaskCompleted closes the Loom
       instance and the gap's reasoning episode.
```

**Why the bridge, not an in-process LLM client (alternative considered + rejected).** The bridge doc
makes the argument verbatim for external calls: *"Embedding outbound calls in Weaver's convergence lane
would smear I/O into detection and force Weaver to re-implement the durable-claim / idempotency /
recovery machinery Loom already has."* An LLM call is exactly an outbound call. Putting it in the
bridge keeps Weaver pure (imports only `substrate/*`), reuses durable claim + recovery for free, and
gives the call FR58 idempotency (`idempotencyKey = instanceKey` → at most one billed model call per
escalation episode even under redelivery). The LLM call is **synchronous** (seconds), so the bridge's
synchronous `Adapter.Execute` path suffices — no async-result lane needed.

### 3.4 Dispatching an approved proposal (the loop closes — Fire 2)

An approved proposal must become a real remediation. The clean, Weaver-consistent path: the
`augur-proposals` lens projects an **approved** proposal as a synthesized §10.2-shaped dispatch row
keyed `<targetId>.<entityId>` carrying the proposed `{action, params}` as if it were a one-off playbook
entry. Weaver's lane-1 handler picks it up and fires it through the **existing**
`buildPlan → fireEpisode → act.submit` path — same OCC, same mark, same idempotency by derived id —
**re-validating** the proposed action against the live catalog at dispatch time (a catalog can drift
between approve and dispatch; fail-closed to `invalid` if it no longer resolves). The proposed op's
own commit (or a tiny `DispatchProposal` flip) advances `review.state → dispatched`, and the
underlying convergence gap closes when the remediation's downstream work lands and the target Lens
re-projects `missing_<g> = false` — identical to every other Weaver remediation.

This keeps Weaver the **sole dispatcher** (no second mutation path), reuses anti-storm + idempotency
wholesale, and means an approved AI proposal is indistinguishable downstream from a hand-authored
playbook entry — exactly the safety property we want.

### 3.5 The reasoning model

The `augur` adapter is **model-pluggable** (adapter config, not a contract). Recommended
default **`claude-sonnet-4-6`** for cost on routine novel gaps, with **`claude-opus-4-8`** selectable
per target for the hardest reasoning (`augur.model` override). Structured output is enforced via the
Messages API tool-use / structured-output schema so the model **cannot** return an action outside the
catalog or params outside the action's schema — the schema is the first line of the deterministic
boundary (§5), not the only one. The prompt is deterministic given `(row, catalog, contextRef)`;
`provenance.promptHash`/`catalogHash` record exactly what was reasoned over, for audit and for
detecting a stale proposal when the catalog has since changed.

---

## 4. Orchestration precedent mirrored

Every orchestration mechanism L3 uses is a named, shipped precedent — nothing novel in the control
plane:

- **`triggerLoom → externalTask → bridge`** — the post-13.1 external-remediation path
  (weaver.md "Actions"; §10.5/§10.6/§10.8). L3's reasoning call is one more externalTask adapter.
- **Anti-storm mark + OCC + lease + reconciler-sweep reclaim** — the standard lane-1 dispatch
  machinery (evaluator.go `fireEpisode`; reconciler.go). The L3 escalation is dispatched as a gap, so
  a crashed/lost reasoning call is reclaimed and re-asked at lease expiry, idempotent on the bridge
  `idempotencyKey`.
- **Deterministic `replyOp` → vertex create** — the externalTask result-op pattern (bridge.md;
  §10.6). `RecordProposal` is the `replyOp`.
- **Lens read-model + DDL self-description** — Loupe already renders op-forms from
  `inputSchema`/`fieldDescription`/`examples`; the action catalog the model reasons over is the **same
  self-description surface**, projected as a read-model lens. No new self-description machinery.
- **Capability-authorized human op** — `ReviewProposal` is an ordinary capability-checked op (the
  reviewer holds a `ReviewProposal` grant), exactly like any operator mutation.

---

## 5. The deterministic validation boundary (the safety core)

This is the load-bearing safety mechanism — **the AI never produces a side effect that wasn't
deterministically validated.** Validation runs at **three** points (defense in depth):

1. **At reasoning time (schema constraint).** Structured output forces `action ∈ catalog` and
   `params` conforming to that action's DDL schema. The model *cannot* emit a free-form action.
2. **At record time (`RecordProposal` DDL).** Before a proposal is even stored as `pending`, the op
   re-validates: action ∈ `{triggerLoom, assignTask, directOp}`; the referenced pattern/operation
   resolves in the live registry; every templated/literal param resolves; the proposal's candidate
   **equals the escalated `(targetId, entityId)`** (no scope escape — the model can't propose acting
   on a *different* entity); and the op type is one Weaver's service-actor is authorized to submit
   (belt-and-suspenders — the Processor enforces capability at dispatch regardless). Fail → the
   proposal is stored `invalid` with the reason (auditable), never `pending`, never dispatchable.
3. **At dispatch time (Fire 2 / Fire 3).** Re-validate against the **live** catalog (drift between
   propose/approve/dispatch) before `act.submit`. Fail-closed → `invalid`.

Plus the **Processor's own capability check** is the final, independent backstop: even a maximally
adversarial proposal that somehow passed 1–3 can only submit an op Weaver's service-actor is already
authorized to submit — L3 grants the model **no new authority**, only the ability to *propose
arranging* the authority Weaver already has. The blast radius of a bad proposal under Fires 1–2 is
**zero** (it can't dispatch without a human), and under Fire 3 is bounded to the opted-in low-risk
action allow-list, fully audited and reversible.

---

## 6. Contract surface

**One frozen-contract change — Contract #10 §10.8 — staged UNCOMMITTED in `main` (the diff is the
proposal).** Additive, opt-in `augur` block on `meta.weaverTarget`:

```
meta.weaverTarget {
  "targetId": "...", "lensRef": "...", "gaps": { ... },          # UNCHANGED, frozen
  "augur": {                                                         # NEW — additive, optional
    "escalate": ["unplannable", "exhausted"],   # which stuck-gap triggers escalate to L3
    "pattern":  "augurReasoning",              # the triggerLoom reasoning pattern (default primordial)
    "model":    "claude-sonnet-4-6",            # optional adapter model override
    "autoApply": {                              # Fire 3 ONLY — gated on Andrew; default ABSENT (= off)
      "actions": ["assignTask"],                # the low-risk action allow-list that may skip the human gate
      "minConfidence": 0.9
    }
  }
}
```

A target with **no `augur` block** behaves **exactly as today** (an unplannable gap fails closed). This
is the same additive-extension class as the 13.1 amendments to §10.2/§10.8. **Affected consumers:** the
Weaver engine (the new escalation branch + install-time validation of the `augur` block); package authors
(the `augur` package; opting a target in). The §10.8 templating rule, the `gaps` shape, and
the action table are **untouched**.

**No other contract change:**
- The `external.augur` adapter + its event envelope are **package/bridge data** — §10.5 states
  the `external` domain is ordinary (open `<domain>.<eventName>`, no Processor allowlist, no Contract
  #3 amendment). A new adapter is bridge-registry config, exactly like `backgroundCheck`.
- `vtx.augurProposal` + `RecordProposal`/`ReviewProposal`/`DispatchProposal` are **package DDL** —
  Contract #1 governs the *key shapes* (honored), but a specific vertex type / op is package data.
- The `augur-proposals` review lens is **package DDL** (a Refractor target).
- New Weaver Health metrics (`augurEscalations`, `proposalsPending`, `proposalsApproved`,
  `proposalsDispatched`, `proposalsInvalid`) are **author-discretion** under Contract #5 §5.4.

---

## 7. Migration / compatibility & test strategy

**Migration.** Purely additive. Bootstrap gains one primordial package (`augur`) on the
install list (the proposal type + ops + review lens + reasoning pattern + the `augur` adapter
registration), bumping the bootstrap version like every prior package add. Existing targets are
untouched and keep failing-closed until a package author adds an `augur` block. The bridge gains one
adapter. No data migration; no behavior change for any non-`augur` target.

**Test strategy** (each fire ships green; mirrors the existing Weaver e2e style):
- **Unit** — the deterministic validator (§5) table: every reject class (unknown action, unresolved
  pattern/op, scope-escape to a different entity, unauthorized op type, param-resolution failure) →
  `invalid`; every accept class → `pending`. Confidence-gate arithmetic (Fire 3). Proposal-id
  determinism (redelivered reply collapses, no duplicate).
- **A faked `augur` adapter** (test-only, deterministic — the same pattern as the existing
  `Fake*` bridge adapters): returns a canned `{action, params, confidence}` so the whole escalation →
  record → review → dispatch loop is exercised against the **real** Processor + bridge + Weaver on the
  ephemeral stack, with **no real model call in CI** (no network, no spend, deterministic).
- **E2e (ephemeral stack)** — a target with an unplannable gap + an `augur` block escalates → a `pending`
  proposal lands in the `augur-proposals` lens → `ReviewProposal{approve}` → Weaver dispatches the
  proposed action → the gap closes and the proposal flips `dispatched`. A second e2e for the reject and
  the `invalid` (validator-fail) paths. Fire 3: an `autoApply`-opted low-risk gap auto-approves +
  dispatches with no human op; an action **outside** the allow-list still waits for a human.
- **Adversarial** — a faked adapter that returns a malicious proposal (a `directOp` on a *different*
  entity; an unknown action; an op Weaver may not submit) is caught at the validator (`invalid`) and at
  the Processor capability check; **never** dispatches. This is the Gate-3-style "DEFENDED" assertion
  for the AI surface.

**Review.** Per the Designer mandate, this cross-cutting AI-surface design warrants adversarial
scrutiny. For an unattended single fire I ran a **focused self-adversarial pass** (§9) rather than
spawning `bmad-party-mode` (heavyweight + the sub-agent-no-commit constraint); the Steward should run
the full 3-layer review at **build** time on each fire, with explicit attention to the validator
boundary (§5) and the autonomy gate (Fire 3).

---

## 8. Decomposition for the Steward (fire-by-fire, each independently shippable + green)

- **Fire 1 — Reasoning capture (no dispatch).** The `augur` package (proposal type +
  `RecordProposal` op + `augur-proposals` review lens + the `augurReasoning` externalTask pattern) +
  the `augur` bridge adapter (with a faked adapter for CI) + the §10.8 `augur` block + the Weaver
  escalation branch (on `unplannable`, dispatch the reasoning `triggerLoom` instead of dead-ending) +
  the §5 record-time validator. **Ships value alone:** a stuck gap becomes a reasoned, human-reviewable
  `pending` proposal surfaced in Loupe. Zero autonomous mutation. *(This is the bulk; M.)*
- **Fire 2 — Approval → dispatch loop.** `ReviewProposal` op + the approved-proposal projection in the
  review lens + Weaver's dispatch-on-approval (re-validated) + the `dispatched` flip + a Loupe/CLI
  review affordance. **Closes the loop:** AI proposes → human approves → deterministic dispatch closes
  the gap. *(S–M.)*
- **Fire 3 — Autonomy dial (gated on Andrew).** The `augur.autoApply` allow-list + confidence floor +
  auto-approve-and-dispatch (full validation + audit + reversibility), default off. **Ships dark until
  the autonomy boundary is ratified.** *(S–M.)*
- **Optional follow-on — `exhausted`-trigger escalation.** Extend the escalation branch to also fire
  on the spent-retry-budget terminal (§1 case 2), so a gap the fixed playbook exhausted gets a
  reasoned alternative proposal. *(S; can fold into Fire 1 or trail it.)*

---

## 9. Risks & alternatives (self-adversarial pass)

| Risk | Mitigation |
|---|---|
| **The model proposes a harmful / out-of-scope action.** | The §5 three-point deterministic validator + the Processor capability backstop. Under Fires 1–2 a proposal **cannot** dispatch without a human; the model gains **no new authority** (only Weaver's existing service-actor authority, arranged differently). Adversarial test proves DEFENDED. |
| **Cost / runaway escalation storm.** | The escalation is dispatched as a **gap** → the anti-storm mark means **one reasoning call per stuck gap per anti-storm window**, not per CDC tick; the bridge `idempotencyKey` dedups redelivery → **at most one billed call per escalation episode**. `augur.escalate` is opt-in per target; a target with no `augur` block never escalates. Health metric `augurEscalations` makes spend operator-visible. |
| **Stale proposal (catalog drifts between propose → approve → dispatch).** | `provenance.catalogHash` records what was reasoned over; **re-validation at approve and at dispatch** fails-closed to `invalid` if the proposed action no longer resolves. A newer proposal for the same `(target,entity,gap)` **supersedes** the older. |
| **Non-determinism / replay.** | The model call lives behind the bridge's deterministic `requestId` + `idempotencyKey`, and `RecordProposal` collapses on a deterministic proposal id, so redelivery never duplicates. The *content* of a proposal is non-deterministic (it's an LLM) — but it is **inert until validated + approved**, so non-determinism never reaches state unreviewed. |
| **L3 becomes a crutch (package authors stop writing real playbooks).** | L3 is the **fallback**, not the path: a recurring `unplannable` escalation for the same gap is a Health-visible signal that the package needs a real playbook entry. A future surveyor/Lamplighter rule can flag "gap X escalated to L3 N times — author a playbook." (Noted as a follow-on, not in scope.) |
| **The bridge isn't meant to read Core KV for the catalog.** | The bridge is a **platform binary** (P5 inspector-class), so it may read lens read-models / Core KV. The catalog is a **read-model lens** (DDL self-description), read like any target row — not a vertical-app Core-KV scan. |

**Alternatives considered:**
- **In-process LLM client in Weaver** — rejected (§3.3): violates "Weaver never reaches an external
  system", re-implements durable-claim/idempotency the bridge already owns.
- **Auto-dispatch every proposal (no human gate) from day one** — rejected: the autonomy boundary is
  Andrew's call; human-in-the-loop is the safe default and the value is already complete without it.
- **A bespoke L3 mark/queue separate from the gap machinery** — rejected: dispatching the escalation
  *as a gap* reuses anti-storm/OCC/lease/recovery wholesale; a separate path would duplicate it.
- **Proposal as operational KV (not a Core vertex)** — rejected: a proposal must be auditable,
  queryable, and link to its candidate/target/reviewer → P1 says business/meta state is a Core vertex.

---

## 10. Open questions — resolved

- **Where does the LLM call live?** → The **bridge** (a new `augur` adapter), dispatched via
  `triggerLoom → externalTask`. Not in-process. (§3.3)
- **Is the proposal Core-KV or operational?** → **Core-KV vertex** `vtx.augurProposal.<NanoID>` (P1);
  the in-flight call is operational (bridge claim + escalation mark). (§3.1)
- **What triggers L3?** → The two real dead-ends — `unplannable` (no playbook entry) and `exhausted`
  (retry budget spent) — selected per target via `augur.escalate`. Fire 1 ships `unplannable`. (§1, §8)
- **How is the AI prevented from doing harm?** → The §5 three-point deterministic validator + the
  Processor capability backstop; AI **proposes**, never directly mutates. (§5)
- **Does an approved proposal need a new dispatch path?** → No — it projects as a synthesized §10.2
  row and fires through Weaver's existing machinery, re-validated. (§3.4)
- **Sync or async LLM call?** → **Sync** (the bridge's `Adapter.Execute` path); seconds, no async lane
  needed. (§3.3)
- **Which model?** → Model-pluggable adapter; default `claude-sonnet-4-6`, `claude-opus-4-8` selectable
  for hard targets. Not a contract/fork — adapter config. (§3.5)
- **The autonomy boundary (auto-apply)?** → **Designed (Fire 3) but gated on Andrew** — the one fork
  for his call (For-Andrew block). Default off; human-in-the-loop ships first.

---

## 11. What lands where

| Path | Change |
|---|---|
| `docs/contracts/10-orchestration-surfaces.md` §10.8 | **(UNCOMMITTED)** additive `augur` policy block on `meta.weaverTarget` + install-validation note |
| `packages/augur/` *(new package)* | proposal vertex type DDL; `RecordProposal`/`ReviewProposal`/`DispatchProposal` ops; `augur-proposals` review lens; `augurReasoning` externalTask pattern; the action-catalog lens; capability grants (operator → `ReviewProposal`; Weaver service-actor → the dispatch ops) |
| `internal/bridge/` (+ registry) | the `augur` adapter (real Claude client) + a deterministic `FakeAugur` adapter for CI |
| `internal/weaver/` (`evaluator.go`, registry, `strategist.go`) | the escalation branch (unplannable/exhausted → dispatch the reasoning `triggerLoom`); approved-proposal dispatch (Fire 2); auto-apply gate (Fire 3); §10.8 `augur` install validation; new Health metrics |
| `cmd/loupe/` + `cmd/lattice/weaver/` | proposal review surface (list/approve/reject) — reads the `augur-proposals` lens |
| tests | validator unit table; faked-adapter e2e (escalate→record→review→dispatch); adversarial malicious-proposal DEFENDED; Fire-3 auto-apply gating |

---

*Designer fire — Winston. This design is complete and resolved; it awaits Andrew's ratification (and
the autonomy-boundary decision in the For-Andrew block) before the Lattice Steward builds it
fire-by-fire.*
