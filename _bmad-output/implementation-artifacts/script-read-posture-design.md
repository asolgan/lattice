# Design — Script-read posture: declared+hydrated reads as the norm; live `kv.Read`/`kv.Links` classified and bounded

**Status: 📐 awaiting-Andrew (ratification).**
**Author: Winston (Designer fire, 2026-06-30).**
**Backlog row:** `planning-artifacts/backlog/lattice.md` → *Refinements & ops* → "Script-read posture — declared+hydrated vs live `kv.get`/`kv.Links`".
**Origin:** the Edge Lattice party-mode finding **F8** ("scripts read Core KV is the root smell") — flagged cross-cutting for Andrew, then filed as this umbrella (commit c6b913e).
**Builds on / subsumes:** Contract #2 §2.5 (context-hint semantics) + §2.5.1 (`kv.Links`, ✅ ratified 2026-06-28) · the **shelved** [Loom effect-guard](loom-guardless-step-recovery-effect-guard-design.md) §9 redirection (the "make the read Processor-side" reflex) · the [Edge Lattice](edge-lattice-full-design.md) A′ F3/F4 gating rules · the shipped externalTask **Mechanism 2** ("Loom declares the read-set, the Processor hydrates" — `packages/orchestration-base/external_params.go`, `internal/loom/externaltask_params.go`).

---

## For Andrew (one-look ratification)

**What it does (two lines).** Establishes the platform **read posture** for write-path Starlark: a script's reads are *partitioned and named* so that a script is, **to the extent its reads are declared, a pure function of `(op payload, declared+hydrated read-set)`** — the property that makes the cloud commit replay-stable, makes the Edge A′ predictor exact, and lets the **Loom guard read move off the engine and into the Processor**. It then sequences the concrete migrations that realize the posture.

**The one headline decision (a platform-direction call, NOT a Gateway/Vault-class architectural fork).** Ratify the posture + its read classification, and with it the direction that **Loom's `evalGuard` Core-KV read is to be retired** (moved Processor-side), not held forever as "the one tolerated exception." Your standing position when you shelved the effect-guard was exactly this — *"guards are the only exception, waiting for a better architectural approach … I don't like that we keep piling on."* This design is that approach: it names the line and charts the retirement, while **holding (not widening)** the guard read until the retirement fire (Fire 3) ships. The grounding **refined the row's thesis** — see the box below; "deprecate live `kv.Read`" is too strong, and saying so is the point.

> **A grounding correction (please read — it changes the row's framing).** The backlog row said *"live `kv.get` as deprecatable debt."* Grounding the actual shipped uses showed that is **too strong**. `kv.Read` on a **declared** key is already a pure hydrated-cache hit (the externalTask-params resolver does exactly this, `external_params.go:54`). Live/undeclared reads split into **four** reasons, only **one** of which is debt: (b) declarable-but-undeclared-for-no-reason = the real debt to lint away; (c) **deliberately** unsnapshotted config reads (clinic `.hours`/`.timeOff` — kept *out* of the OCC set so a config edit doesn't conflict with a booking) = sanctioned; (d) read-before-create / dedup absence-tolerance (forced to lazy today because `contextHint.reads` *faults* on a missing key) = fixed by a new **`contextHint.optionalReads`**; (e) `kv.Links` enumeration + its data-dependent per-element follow-up reads (the key set is unknown at submit time) = irreducible. The posture maximizes the declared class and **names** (c)/(e) as bounded exceptions rather than pretending to eliminate them. Full classification: §2.

**Frozen-contract change (staged UNCOMMITTED in `main` — the diff is the proposal).** `docs/contracts/02-operation-envelope.md` §2.5 + the §2.2 field table:
- a new **"Read posture"** subsection in §2.5 stating the declared-read norm, the read classification, and the determinism property;
- a new **`contextHint.optionalReads: string[]`** field (declared, *hydrate-if-present / absent-sentinel-if-missing*, no `HydrationMiss`) — the primitive that lets the read-before-create/dedup pattern (reason d) become a **declared** read instead of a live one.

Affected consumers: the Processor Hydrator (new field), package authors using read-before-create (a new authoring form; `orchestration-base` CreateTask is the first migration target), and the Edge A′ predictor (gains a declared, predictable read it can gate on). No other contract section changes for the staged edit.

**A second, fork-dependent contract change I designed-through but did NOT stage** (Fire 3): the Loom-guard-Processor-side migration needs a Contract #10 §10.5/§10.6 refinement (where a guard is *evaluated*) + a Contract #2 reply outcome (`decision: "guard-unmet"`). Because Fire 3 carries a real mechanism fork (**G1 guarded-op-with-unmet-outcome** vs **G2 guard-eval read-seam** — §6) whose resolution determines the exact contract text, I did **not** pre-stage that edit; I describe both options with a recommendation and stage it at Fire-3 ratification. The settled core (§2.5 posture + `optionalReads`) is staged now.

**No architectural fork** (Gateway / read-path-auth D1 / Vault / multi-cell / HA-NATS untouched). No auth-surface change: every read discussed is the **write path reading its own Core KV** (P2/P5 place no bar on that); the posture *removes* a non-Processor reader (Loom), it does not add one.

---

## 1. Problem & intent

### 1.1 The two symptoms with one root (F8)

Two ratified designs hit the same wall from opposite sides:

- **The Loom-guard Core-KV read.** Loom's `evalGuard` (`internal/loom/guard_eval.go:204`) point-reads Core KV to decide whether a guarded step runs. This is the **single** non-Processor Core-KV *business-state* read on the orchestration path (confirmed by inventory — §1.3). When the [effect-guard](loom-guardless-step-recovery-effect-guard-design.md) proposed *adding another* engine read, you shelved it: *"I don't like Loom or any other non-Processor component reading from Core-KV. Guards are the only exception (waiting for a better architectural approach) … I don't like that we keep piling on to it."* The effect-guard §9 redirection already named the fix — *"make the read Processor-side"* — but left the discrimination problem open.

- **The Edge A′-prediction partiality.** The [Edge](edge-lattice-full-design.md) node predicts an op's result by running its Starlark locally against a **partial** mirror of the user's authorized slice. Finding **F3** (load-bearing): *predict an op iff its declared read-set (`contextHint.reads`) ⊆ the local mirror, else degrade to pending.* Finding **F4**: a `kv.Links`-bearing op is generally **not** locally predictable (an open enumeration — the mirror cannot know it holds *all* of a relation's links). The accuracy of A′ is therefore exactly bounded by **how much of a script's read-set is declared**.

**F8 connected them:** both are the symptom of *scripts performing reads the platform cannot see ahead of execution*. If a script were a **pure function of its declared read-set + payload**, the cloud Processor could hydrate that set and evaluate (including the guard), and the Edge could check that set against its mirror and predict exactly. The live, undeclared read is the smell — but, per the grounding box above, not *every* live read is the same kind of smell.

### 1.2 Intent

Make the **declared+hydrated read** the platform norm for write-path scripts, **classify** every legitimate reason a read is *not* declared, give the one missing primitive (`optionalReads`) so a whole class stops needing live reads, lint the genuine debt, and **retire the Loom guard's engine read** by evaluating the guard against Processor-hydrated declared state. The deliverable is a posture + the contract that encodes it + a fire sequence that realizes it — each fire independently valuable.

### 1.3 Inventory — what actually reads live, today (grounded, not assumed)

`grep` over `internal/loom`, `internal/weaver`, and `packages/*` for Core-KV reads on the write/orchestration path:

| Surface | Reads | Class (per §2) | Verdict |
|---|---|---|---|
| **Loom `evalGuard`** (`guard_eval.go:204`) | subject root + guard-path aspects (all keys **known** from the §10.5 guard grammar) | **(a) declarable** | The lone engine business read → **retire** (Fire 3). |
| Loom `engine.go:1352/1367` | `vtx.op.<reqId>` / `vtx.task.<id>` dedup-tracker existence | (d) dedup absence-tolerance | Idempotency dedup, not business state. Out of scope (engine-internal idempotency; not a script read). Noted, not migrated. |
| Loom `externaltask_params` + `orchestration-base/external_params.go:54` | subject aspect, key declared via `inferExternalTaskReads` | **(a) declared** | Already the posture working: `kv.Read` on a declared key = hydrated cache hit. **The model to mirror.** |
| Weaver `reconciler`/`temporal` | `weaver-targets` (a **lens** read-model) + `weaver-state` (own state) | — | P5-clean (reads a lens, not Core KV) + operational state (P1). **No Core-KV business read.** Weaver is already clean. |
| clinic `kv.Read(cand_key …)` per-candidate (`ddls.go:1339…`) | candidate vertex/`.status`/`.schedule`, keys derived by iterating an index / `kv.Links` | **(e) data-dependent / enumeration follow-up** | Irreducible — key set unknown at submit time. **Keep.** |
| clinic `kv.Read(provider+".hours" / ".timeOff")` | config aspects, keys **known** but read live **on purpose** | **(c) deliberately unsnapshotted** | Kept out of OCC so config edits don't conflict with bookings. **Sanctioned.** |
| `orchestration-base` CreateTask `kv.Read(task_key)` (`ddls.go:223`) | the to-be-created task key — **known**, read live only because `contextHint.reads` faults on absence | **(d) read-before-create / dedup** | **Migrate to `optionalReads`** (Fire 1's real consumer). |

The inventory is the design's spine: it proves the engine-read problem is *exactly* `evalGuard` (Weaver is already lens-clean), and it gives Fire 1 a **real shipped consumer** (CreateTask), so nothing here is dead scaffolding.

---

## 2. The shape — the read classification (the posture)

Every read a write-path script performs falls into one of five cells. The posture is: **drive everything toward (a); fold (d) into (a) via a new primitive; name (c) and (e) as bounded exceptions; lint (b) as debt.**

| # | Class | Key known at submit time? | OCC-snapshotted? | Replay-stable? | Edge-predictable? | Disposition |
|---|---|---|---|---|---|---|
| **(a)** | **Declared exact-key read** (`contextHint.reads` → hydrated; `kv.Read` hits the cache) | yes | yes (step-4 snapshot is the OCC condition) | **yes** | **yes** (in mirror ⇒ exact) | **The norm.** Maximize. |
| **(b)** | Declarable-but-undeclared `kv.Read` (live GET of a knowable key for no reason) | yes | no | no | no | **The only real debt.** Lint → move to (a). |
| **(c)** | Deliberately-unsnapshotted live read (config: `.hours`, `.timeOff`) | yes | **deliberately not** | no | no (degrade) | **Sanctioned exception** — author opts a knowable key *out* of OCC to avoid false contention with config edits. Must be a *documented* choice, not a slip. |
| **(d)** | Read-before-create / dedup absence-tolerance | yes | yes (with `optionalReads`) | **yes** (snapshot + CreateOnly backstop) | **yes** | Today forced to live `kv.Read` because `reads` *faults* on absence → **new `optionalReads`** folds it into (a). |
| **(e)** | Enumeration (`kv.Links`) + data-dependent per-element follow-up reads | **no** (key set is data-derived) | no | no | no (degrade; F4) | **Irreducible exception.** `kv.Links` is the one sanctioned enumeration (§2.5.1); its follow-up per-element `kv.Read`s inherit the same un-declarability. |

### 2.1 The determinism property (what "pure" buys, precisely)

A script that reads **only class-(a)/(d) keys** is **replay-stable**: re-running the same `requestId` against the same step-4 hydrated snapshot produces byte-identical `{mutations, events}`, and the snapshot revisions are exactly the OCC conditions the step-8 commit asserts. A script that performs **any (b)/(c)/(e)** read is **not** replay-stable — it reads live state and may branch differently on replay (this is the documented `kv.Read`/`kv.Links` posture: the Processor, via deterministic-id + OCC + the `CreateOnly` backstop, is the idempotency authority, *not* replay determinism — `internal/processor/starlark_kv.go:46`). The posture does not make every script replay-stable; it makes the *non-stable* reads **few, named, and intentional**, and it makes their presence **statically visible** (§3.3).

### 2.2 Why this is the right shape (mirrors what already works)

The externalTask path (`external_params.go` + `inferExternalTaskReads`) is **the posture, already shipped**: Loom computes the read-set by *pure string parsing* of the step's params (no engine read), declares it in the dispatched op's `contextHint.reads`, and the instanceOp DDL resolves the template via `kv.Read` on the **hydrated** key (a cache hit). Mechanism 2's own comment states the line verbatim: *"Core-KV reads stay inside the Processor, and guard evaluation remains the lone Core-KV-read exception."* This design's Fire 3 closes that lone exception **by the same move** — Loom parses the guard's declared paths (it already parses them, `guard.go`), declares them, and the **Processor** hydrates + evaluates. We are extending a proven, decomposed pattern, not inventing a parallel one.

---

## 3. Read path / write path / orchestration

- **Read path (P5).** Unchanged and *strengthened*: applications still read lenses; the write path still reads its own Core KV; and after Fire 3 the write path's Core-KV reads are **all inside the Processor** (the engine reads nothing of Core KV business state). `kv.Links` (§2.5.1) remains the one bounded write-path enumeration of Core-KV canonical links — never a lens, never the Adjacency KV.
- **Write path (P2).** Unchanged: the Processor remains the sole Core-KV writer; ops carry their declared read-set; the step-4 snapshot is the OCC condition.
- **Orchestration.** Fire 3 changes *where a Loom guard is evaluated*, not the orchestration shape: Loom still drives the wait-loop (re-attempt advancement on a trigger), but each attempt's read happens Processor-side. This mirrors the externalTask dispatch precedent — Loom declares, the Processor reads.

### 3.1 `contextHint.optionalReads` (the new primitive — Fire 1)

```jsonc
"contextHint": {
  "reads":         [ "vtx.identity.<id>" ],          // REQUIRED — absent ⇒ HydrationMiss (fail-closed, unchanged)
  "optionalReads": [ "vtx.task.<derivedTaskId>" ]     // TOLERATED — absent ⇒ hydrated as the absent sentinel (None)
}
```

- The Hydrator (`step4_hydrate.go`) hydrates `optionalReads` keys the same way as `reads`, **except** a `ErrKeyNotFound` is **not** a `HydrationMiss`: the key is recorded as *known-absent*, so `kv.Read(key)` returns `None` from the cache (no live GET).
- **Replay-stable + OCC-coherent.** An `optionalReads` key resolves at the step-4 snapshot; a key that was absent at snapshot is conditioned as create-able, so a concurrent create that wins between step 4 and step 8 is caught by the existing `CreateOnly` backstop at commit (`RevisionConflict` → re-hydrate → now present → no-op). This is exactly the CreateTask dedup's current correctness, now **declared** (and therefore Edge-predictable).
- **Authoring rule (fail-closed discipline).** A key whose *absence is a correctness error* MUST go in `reads` (fail-closed). `optionalReads` is **only** for a read whose absence is a *legitimate branch* (read-before-create, dedup). This is stated in the contract so `optionalReads` cannot be used to silently soften a required read. (Absence-tolerance here is not a security boundary — it is an idempotency branch — so the §6.8-style "omission ⇒ deny" reflex does not apply; the relevant guard is that a *required* read stays required.)

### 3.2 The Loom-guard migration shape (Fire 3) — designed-through; the fork is §6

Today: a Loom trigger fires → `advanceToRunnableStep` → `evalGuard` **reads Core KV** → true ⇒ dispatch the step op; false ⇒ stay pending.

After: a Loom trigger fires → `advanceToRunnableStep` computes the guard's declared read-set by **pure parse** of the guard paths (the `guard.go` parser already yields these paths; `inferExternalTaskReads` is the precedent for turning a parsed shape into a read-set) → Loom **dispatches a guarded op carrying the guard + its declared reads** → the **Processor** hydrates the reads and **evaluates the guard against the hydrated working set** (the `evalGuard` logic *moves package* from `internal/loom` to `internal/processor`, now reading the hydrated map instead of live Core KV) → guard true ⇒ the op runs and commits (Loom advances on the accepted reply); guard false ⇒ a **non-commit `guard-unmet` outcome** (commits nothing, no tracker) ⇒ Loom keeps the step pending and re-attempts on the next trigger. The **wait-loop stays in Loom** (an engine concern); the **read + predicate evaluation move to the Processor** (a write-path-read concern).

Cost note (honest): the guard re-check is now a Processor round-trip rather than a Loom-local GET — but at the **same frequency** (one per trigger-driven advancement attempt, exactly as today), so this **relocates** the read, it does **not multiply op volume**. The mechanism choice (full op vs a lighter eval seam) is the §6 fork.

### 3.3 Static read-classification (the conformance hook — Fire 2)

Contract #2 §2.5 already reserves *"Static analysis of Starlark scripts may auto-derive read sets."* This design **operationalizes** it minimally: a script's reads are statically classifiable — a `kv.Read(<string-literal>)` whose literal is in `reads`/`optionalReads` is class-(a); a `kv.Read` of a knowable literal **not** declared is class-(b) **debt**; a `kv.Read` of a non-literal (data-dependent) expression or a `kv.Links` call is class-(e) irreducible; a config read flagged by an author annotation is class-(c). Fire 2 adds a `lint-conventions` check that flags **class-(b)** (the only debt) and requires class-(c) to be **explicitly annotated** (so a deliberate unsnapshotted read is distinguishable from a slip). This is the same posture the existing `TestPackage_NoScans` gate embodies, extended from "no raw scans" to "declare your declarable reads."

The same classification yields the **Edge A′ per-op predictability flag** for free: an op is locally predictable iff its script is class-(a)/(d)-only **and** its declared reads ⊆ mirror (F3); any class-(c)/(e) read ⇒ degrade to pending (F4 generalized). No new Edge mechanism — the Edge reads the same static classification.

---

## 4. Contract surface (exactly which §§ change vs build-to)

| Doc / § | Change vs build-to | What |
|---|---|---|
| **Contract #2 §2.5** + §2.2 field table | **CHANGE — staged UNCOMMITTED** (Fire 1) | New **"Read posture"** subsection (the declared-read norm, the §2 classification, the §2.1 determinism property); new **`contextHint.optionalReads`** field (declared, hydrate-if-present / absent-sentinel; the authoring rule that a correctness-required read stays in `reads`). |
| **Contract #2 §2.5.1** (`kv.Links`) | **build-to** (read only) | Already ratified/committed (2026-06-28). Reaffirmed as class-(e) — the design adds no change; it *names* it within the posture. |
| **Contract #2 §2.4 / §2.6** (reply / error codes) | **CHANGE — Fire 3, NOT staged** (fork-dependent) | A guard-unmet non-commit outcome: a `decision: "guard-unmet"` on an accepted-but-empty reply (G1), **or** a dedicated guard-eval seam reply (G2). Exact shape set by the §6 fork → staged at Fire-3 ratification. |
| **Contract #10 §10.5 / §10.6** (Loom guard semantics) | **CHANGE — Fire 3, NOT staged** (fork-dependent) | Refine: a guard is *evaluated against the Processor's JIT-hydrated declared-read working set*, not a Loom Core-KV read; Loom *declares* the guard's read-set (pure parse). The §10.5 guard grammar is unchanged (same `{absent|present|equals|allOf|anyOf|not}`); only the **evaluation locus** moves. |
| `internal/processor` Hydrator + `kv.Read` cache | build-to (code) | Fire 1: `optionalReads` hydration + known-absent sentinel. Fire 3: host the moved `evalGuard` against the hydrated map. |
| `internal/loom` engine | build-to (code) | Fire 3: replace the `evalGuard` live read with declare-and-dispatch; keep the wait-loop. |
| `packages/orchestration-base` CreateTask | build-to (code) | Fire 1: migrate `kv.Read(task_key)` dedup → `optionalReads`. |
| `lint-conventions` / package tests | build-to (code) | Fire 2: the class-(b) debt gate + class-(c) annotation requirement. |
| `docs/contracts`/`docs/components/{loom,refractor}.md` + `edge-lattice-full-design.md` | build-to (doc) | At each fire: document the posture; Fire 3 retires loom.md's "guards are the lone Core-KV-read exception" line. |

**Convention friction flagged for Andrew (per §3 of the skill).** §2.5 today calls `contextHint` a pure *optimization* — *"The platform does not enforce its presence."* The posture **elevates** declared exact-key reads from optional optimization to the **expected norm** (with class-(c)/(e) the named exceptions and lazy class-(b) the lint target). That is a genuine stance change in §2.5, not a behavior break (undeclared reads still *work*; they're just flagged debt). It is the heart of the staged edit — review the diff.

---

## 5. Reconciliation with the existing mental model ("but didn't we…?")

- **"Didn't `contextHint` + `kv.Read` already cover this?"** They cover the *mechanism*; they never stated the *posture* (declared = norm) nor closed the read-before-create gap (`reads` faults on absence — that's *why* CreateTask uses live `kv.Read`). This design states the posture and adds `optionalReads` to close that gap.
- **"Didn't we ratify `kv.Links` as the bounded exception already?"** Yes — and this design **keeps it exactly**, slotting it as class-(e) within the full classification. The umbrella's contribution is the *other four* classes and the determinism/Edge framing that `kv.Links` alone didn't give.
- **"Isn't this just the shelved effect-guard, re-opened?"** No. The effect-guard *added* a Loom read (you shelved it). This design *removes* the Loom read entirely (Fire 3) and was the redirection §9 itself pointed to ("make the read Processor-side"). The effect-guard's recovery-idempotency problem is **out of scope here** but becomes *re-expressible* once the guard is Processor-evaluated — noted as a downstream beneficiary, not solved here.
- **"Does this duplicate the externalTask Mechanism 2?"** It **generalizes** it. Mechanism 2 proved "Loom declares, Processor hydrates" for *params*; Fire 3 applies the identical move to *guards*. Same pattern, second consumer — the decomposition the architecture already chose, not a new one.
- **"Does this introduce new state?"** No new Core-KV state. `optionalReads` is an envelope field; the guard's hydrated state is the working set the Processor already builds. Fire 3 *removes* a state-access path (the engine→Core-KV read).
- **"Does it touch auth / P5 / P2?"** No. Every read here is the write path reading its own Core KV; the posture **removes** a non-Processor reader. P5 (apps read lenses) and P2 (Processor sole writer) are untouched and reinforced.

---

## 6. The Fire-3 mechanism fork (designed-through; recommendation given)

How does the Processor evaluate the guard and tell Loom run-vs-wait, without a Loom Core-KV read?

**G1 — guarded op with a `guard-unmet` non-commit outcome (RECOMMENDED).** Loom dispatches the step's op normally, carrying the declared guard + its reads; the Processor hydrates, evaluates the guard against the working set, and — if false — returns `decision: "guard-unmet"` having committed **nothing** (no mutations, no event, **no `vtx.op` tracker**, so no tracker churn across re-checks). Loom treats `guard-unmet` as "stay pending, re-attempt on next trigger" (its existing wait behavior); a true guard runs the op as today.
- *Pros:* reuses the entire op path (hydration, OCC, dispatch, reply) — minimal new surface; one contract reply outcome; mirrors externalTask end-to-end.
- *Cons:* a guard-unmet still pays a full op round-trip (consume + hydrate + reply) per re-check; needs the Processor to support a clean "accepted-but-committed-nothing, not an error" path.

**G2 — dedicated guard-eval read-seam.** A small Processor request-reply (`lattice.eval.guard`) that takes `{subjectKey, guard}`, hydrates the guard's declared reads, evaluates, and returns just the boolean — no commit, no tracker, no event ever.
- *Pros:* cheapest per re-check (a read round-trip ≈ today's Loom GET cost); zero commit/tracker overhead; the purest embodiment of "the guard is a pure predicate over Processor-hydrated state."
- *Cons:* a **new** Processor surface (a read-only eval endpoint that returns a data-derived boolean to an engine) — small, but it is *a* new surface, and it sits slightly outside the op path it would otherwise reuse.

**Recommendation: G1.** It adds no new Processor *surface* — only a new *reply outcome* on the path that already exists — and it mirrors the shipped externalTask dispatch exactly (Loom dispatches an op; the Processor decides). The per-check op-round-trip cost is real but bounded (same frequency as today's check, on the `system` lane built for internal automation), and it avoids a parallel eval channel. Choose **G2** only if guard re-check frequency proves a measured hotspot — at which point the eval-seam is a clean optimization of G1, not a different design. (Either way the §10.5 guard *grammar* and the wait-loop are identical; the fork is purely the run/wait transport.)

---

## 7. Migration / compatibility & test strategy

**Migration.** Each fire is additive + backward-compatible:
- **Fire 1.** `optionalReads` is a new optional field; existing envelopes (no `optionalReads`) are byte-identical. The CreateTask migration is a package edit (F-004 in-place refresh / version bump picks it up); old envelopes keep working via lazy `kv.Read` until migrated.
- **Fire 2.** The lint gate flags class-(b); it is introduced **non-blocking (warn)** first, flipped to blocking once the tree is clean (the existing `lint-conventions` precedent). No runtime behavior change.
- **Fire 3.** Guards keep their grammar; a pattern's `Step.Guard` is unchanged on the wire. The change is internal (evaluation locus) + the new reply outcome; a pattern built pre-Fire-3 evaluates identically post-Fire-3. Disaster-recovery / crash-safety invariants (Contract #10 §10.6) are preserved (the guard is still evaluated on each advancement attempt).

**Test strategy.**
- **Fire 1 (Processor + package).** Hydrator unit: an `optionalReads` key present ⇒ hydrated cache hit; absent ⇒ known-absent sentinel, `kv.Read` returns `None`, **no** `HydrationMiss`; a key in `reads` still faults on absence (the fail-closed boundary holds). `orchestration-base` package + integration: CreateTask dedup migrated to `optionalReads` passes the existing cross-reclaim-dedup + same-commit-race suites (the `CreateOnly` backstop still catches the step-4→step-8 race). A concurrency assertion: two CreateTask for the same stable `taskId` → exactly one task.
- **Fire 2 (lint).** A script with a declarable-undeclared `kv.Read(<literal>)` ⇒ flagged class-(b); a `kv.Read(<expr>)` / `kv.Links` ⇒ not flagged (class-(e)); a config read without the class-(c) annotation ⇒ flagged; with it ⇒ allowed. `TestPackage_NoScans` stays green.
- **Fire 3 (Loom + Processor).** The moved `evalGuard` over a Processor-hydrated map reproduces the existing `guard_eval_test.go` table (absent/present/equals/composites/tombstone-safe). Engine: a guard-false step yields `guard-unmet` (no commit, no tracker) and stays pending; a trigger that flips the guard advances it. The **regression proof** is the existing loom guard e2e suites passing with **zero** Loom Core-KV reads (assert `evalGuard` is gone from `internal/loom`). Ephemeral-stack e2e: a guarded Loom pattern converges end-to-end with the guard evaluated Processor-side.
- **Gates (all fires):** `go build ./...`, `make vet`, `golangci-lint run ./...`, STRICT `lint-conventions`, the relevant `go test -race` packages, `make verify-kernel`, and (Fire 3) the loom guard/external e2e suites.

---

## 8. Risks & alternatives

**Risks.**
1. **`optionalReads` misused to soften a required read** (reason-d creep into reason-a-required). Contained by the §3.1 authoring rule + the Fire-2 lint (a key whose absence faults a downstream invariant belongs in `reads`) + review. Same posture as today's `reads` discipline.
2. **Fire-3 guard re-check cost** on a high-trigger pattern (G1 op-round-trip per check). Bounded — same frequency as today, `system` lane; the G2 eval-seam is the escape hatch if measured. Stated, not hidden.
3. **Over-classification churn** — authors annotating every read. Mitigated by docs: declare what's declarable (the default), annotate class-(c) only for deliberate config-isolation, leave class-(e) to enumeration. The lint guides, it doesn't demand annotations everywhere.
4. **A guard that needs an enumeration** (`kv.Links` inside a guard predicate). The §10.5 grammar has no enumeration atom today, so guards are class-(a) by construction — but if a future `linkPresent` atom (the shelved effect-guard Fire 2) lands, that guard becomes class-(e) and is **not** declarable/predictable. Noted as a boundary: an enumeration-bearing guard cannot move fully Processor-side via the declared-reads path and would need the `kv.Links`-in-Processor seam — out of scope here, flagged for whoever revives that atom.

**Alternatives considered.**
- **A0 — Do nothing; keep the Loom guard read as the permanent tolerated exception, ship only `optionalReads`.** Defensible (you *tolerate* the guard read today). Rejected as the *whole* answer because the umbrella's value is precisely to chart the retirement you asked for ("a better architectural approach"), not to enshrine the exception. But A0 is the **right interim**: Fires 1–2 ship the cheap wins, and the guard read stays *held (not widened)* until Fire 3 — so A0 is folded in as the sequencing, not rejected outright.
- **A1 — "Deprecate live `kv.Read`" wholesale (the row's literal thesis).** Rejected on grounding (§1 box): classes (c)/(e) are legitimate and irreducible; a blanket deprecation would force the clinic per-candidate reads and config reads into contortions for no benefit. The honest posture deprecates only class-(b).
- **A2 — Static auto-derivation of the full read-set (eliminate `contextHint` authoring entirely)** — the §2.5 "future evolution." Rejected as the *mechanism* here: full auto-derivation can't see class-(e) data-dependent keys at all (that's their definition), so it cannot replace declaration for the irreducible class; this design uses static analysis for *classification/lint* (what it can do soundly), not for *eliminating declaration*. (A richer auto-derivation remains a future ergonomic, additive to this posture.)
- **A3 — Evaluate the Loom guard against a lens projection** (P5-style) instead of Processor-hydrated Core KV. Rejected: lenses lag (eventual); a guard needs *current* state at decision time, and the OCC-coherent hydrated snapshot is exactly that. The guard is a write-path read, and write-path reads read Core KV (in the Processor), not a lagging lens.
- **A4 — Generation-independent guard identity** (the effect-guard §2.1 token route) — out of scope (that's recovery-idempotency, a different problem) and already rejected as unsound there.

**Dead-scaffolding test.** Fire 1 (`optionalReads`) realizes value immediately — its consumer (`orchestration-base` CreateTask) ships **in the same fire**, replacing a live read with a declared one. Fire 2 (lint) acts on the tree as it stands. Fire 3 (guard migration) realizes value standalone (retires the engine read). None is built dark; none waits on an absent consumer. The Edge A′ predictor is a **beneficiary** (it reads the static classification), not a gating dependency — Fires 1–3 stand without Edge being built.

---

## 9. Open questions — resolved (decide-don't-defer)

- **Is live `kv.Read` "deprecatable debt"?** Only class-(b). Classes (c)/(d)/(e) are legitimate; (d) gets `optionalReads`. (§1 box / §2.)
- **New field or overload `reads`?** A distinct **`optionalReads`** — overloading `reads` would make the fail-closed-vs-tolerant semantics ambiguous (and a forgotten "is this one required?" is a silent correctness hole). (§3.1.)
- **Does the Loom guard read get retired or held?** **Retired** (Fire 3) — held *only* as the interim until Fire 3, never widened. (For-Andrew / A0.)
- **G1 vs G2 for Fire 3?** **G1** (guarded-op with `guard-unmet` outcome), G2 as the measured-hotspot optimization. (§6.)
- **Does this make every script replay-stable?** No — it makes the non-stable reads few, named, and statically visible; the Processor stays the idempotency authority. (§2.1.)
- **Where does the Edge predictability flag come from?** The same static read-classification (§3.3) — no new Edge mechanism.
- **Stage the §10.x/reply edit now?** No — it is fork-dependent (G1/G2); stage at Fire-3 ratification. Stage only the settled §2.5 posture + `optionalReads` now. (For-Andrew.)

---

## 10. Adversarial self-review (recorded — folded in)

This is a substantial, cross-cutting (Contract #2 + Loom + Edge + determinism) design, so per the skill it carries an adversarial pass. I red-teamed the posture against its own claims; findings folded into the body:

1. **"`optionalReads` breaks determinism like lazy `kv.Read`."** Refuted — it resolves at the step-4 snapshot (OCC-conditioned) and the `CreateOnly` backstop closes the snapshot→commit race; it is **more** stable than lazy `kv.Read` *and* declared (Edge-predictable). (§3.1.)
2. **"`kv.Links` contradicts the posture."** Accepted and *named* as class-(e), not hidden — the posture is "all *declarable* reads declared; enumeration is the one sanctioned live read," which is exactly §2.5.1. (§2.)
3. **"Moving the guard Processor-side multiplies op volume."** Refuted — same check frequency, relocated read; G1 commits nothing on unmet (no tracker churn), G2 is the cheaper escape hatch. (§3.2/§6.)
4. **"Deprecating lazy `kv.Read` breaks read-before-create."** This was the load-bearing trap (the `starlark_kv.go` comment says `contextHint` *can't* express tolerate-absence). Resolved by sequencing: **`optionalReads` (Fire 1) lands before any lint of class-(b) (Fire 2)** — firm order. (§7.)
5. **"Config reads (`.hours`) would get wrongly flagged as debt."** Caught — hence class-(c) is a *sanctioned, annotated* exception (deliberate OCC-isolation), distinguished from class-(b) by the annotation the lint requires. (§2/§3.3.)
6. **"This is just a doc/posture with no build."** Refuted — `optionalReads` (Processor + a real package consumer), the lint gate, and the guard migration are concrete code. (§1.3/§7/§8.)
7. **Fail-closed check.** `optionalReads` tolerates absence by design (idempotency branch, not an authz boundary); the fail-closed reflex is preserved by keeping correctness-required reads in `reads` (the §3.1 authoring rule). No default-open surface introduced.

(I did not spawn `bmad-party-mode` for this unattended fire; the design *originates* from the Edge party-mode F8 finding, which already adversarially surfaced the root, and the inline pass above discharges the cross-cutting review obligation. If Andrew wants a full party pass before Fire 3 — the Loom-redesign fire — I'll run it then.)

---

## 11. Decomposition for the Lattice Steward (fire-by-fire, each independently shippable + green)

**Build only after ✅ Andrew-ratified.** Fire 1 → Fire 2 order is **firm** (`optionalReads` must precede any class-(b) lint, per §10.4). Fire 3 is independent of 1–2 and is the heavy Loom-redesign fire — sequence it when prioritized (no external dependency; gated only on ratification + the §6 fork decision).

**Fire 1 — `contextHint.optionalReads` + the first consumer (S–M; full review — it's a contract + idempotency-path change).**
- Contract #2 §2.5/§2.2 edit committed by Andrew at ratification (staged uncommitted now).
- Hydrator (`step4_hydrate.go`): hydrate `optionalReads`, known-absent sentinel, `kv.Read` cache serves `None`.
- `orchestration-base` CreateTask: migrate `kv.Read(task_key)` dedup → `optionalReads`; bump package version.
- Tests per §7. *Green:* the cross-reclaim-dedup + same-commit-race suites pass with the declared read.

**Fire 2 — the read-classification lint (S; thorough lead review).**
- `lint-conventions`: flag class-(b) (declarable-undeclared `kv.Read(<literal>)`); require a class-(c) annotation for deliberately-unsnapshotted config reads. Introduce warn-only, flip to blocking once clean.
- Documents the posture in the contract/component docs. *Green:* the tree passes; `TestPackage_NoScans` unaffected.

**Fire 3 — retire the Loom guard's Core-KV read (M; FULL 3-layer adversarial review — engine + Processor + a frozen-contract reply/grammar-locus change).**
- §6 fork resolved (recommend G1). Contract #10 §10.5/§10.6 + Contract #2 reply edit staged uncommitted at this fire's ratification.
- Move `evalGuard` `internal/loom` → `internal/processor` (evaluate against the hydrated map); Loom computes the guard read-set by pure parse + dispatches a guarded op; the `guard-unmet` non-commit outcome + Loom's stay-pending handling.
- Tests per §7, incl. the assertion that **no Core-KV business read remains in `internal/loom`** and the guard e2e suites pass Processor-side. *Green:* the loom guard e2e converges with the engine reading nothing of Core KV.

**Fire 4 (optional, XS) — Edge A′ predictability flag wiring** (only once Edge EDGE.1/2 is in build): the predictor consumes the §3.3 static classification to gate F3/F4. Listed for completeness; it is a *beneficiary*, built with Edge, not part of this umbrella's critical path.

---

## 12. Ratification checklist (for Andrew)

1. **Ratify the read posture + the §2 classification** — the declared-read norm, with (c)/(e) named exceptions and (b) the lint target. *(The headline platform-direction decision.)*
2. **Ratify the direction that the Loom guard read is retired (Fire 3), held-not-widened until then** — closing the "lone tolerated exception" with the "better architectural approach."
3. **Confirm the §2.5 + §2.2 `optionalReads` contract edit** (staged uncommitted in `main`) — review the diff.
4. **Pick the Fire-3 mechanism** — G1 (recommended) vs G2 — or defer to Fire-3 ratification (the §10.x/reply edit is staged then, not now).
5. **Confirm `optionalReads` over overloading `reads`** (§9) and the authoring rule that correctness-required reads stay in `reads`.

Once ✅ Andrew-ratified, the Lattice Steward builds Fire 1 → Fire 2; Fire 3 when prioritized (full 3-layer review).
