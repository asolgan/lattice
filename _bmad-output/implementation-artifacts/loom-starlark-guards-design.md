# Loom Starlark guards — the verified-pure predicate sandbox

**Status: ✅ Andrew-ratified (SPLIT, 2026-06-29).** Piece 1 (shared `internal/starlarksandbox` leaf) = **ratified, built WITH its first consumer (⑦ AI-authored Fire 4), not speculatively.** Piece 2 (Loom-side Starlark guard evaluation) = **⏸️ HELD, gated on the guard-evaluation-location decision** (#3's "better architectural approach for guards"). See the *Ratified* block. · Designer (Winston), 2026-06-29 · Lattice lane (Stream 2)
Backlog row: *"Starlark guards (Loom)"* — `_bmad-output/planning-artifacts/backlog/lattice.md` → Lattice feature
backlog → AI-native.

---

## For Andrew

**What it does (two lines).** Lights up the `{reads, starlark}` guard escape hatch that Loom's grammar has
*reserved-and-rejected* since Story 8.3 (Contract #10 §10.5): a guard may be a **pure Starlark predicate**
`guard(subject) -> bool` for the conditions the declarative `absent/present/equals` grammar can't express —
evaluated by the **same verified-pure, deterministic, side-effect-free sandbox the Processor already runs**, so
the instance cursor stays rebuildable by guard-replay. The predicate reads **only** the subject aspects named in
`reads` (pre-hydrated, frozen) — no live reads, no clock, no I/O, no mutations.

**The one decision for you (a module-boundary fork, designed through).** The contract calls for "the *same*
verified-pure sandbox the Processor uses" + a "**shared** pure-evaluator extraction." The cleanest way to honor
that is a new **leaf package `internal/starlarksandbox`** (imports only `go.starlark.net` + stdlib — zero internal
deps, not an engine) that both the Processor *and* Loom import, so the security boundary has **one source of
truth**. But Loom's module boundary is **AC-asserted** — Story 8.1 AC #8: *"loom imports ONLY `internal/substrate`
+ stdlib"* (`internal/loom/doc.go:14`, enforced in spirit by `actuator.go:16` carrying its own envelope copy "to
keep the module boundary clean"). Admitting `internal/starlarksandbox` widens that rule by one leaf utility.

- **Recommendation: Option A — extract the shared leaf package, and widen Loom's boundary to "`internal/substrate`
  + `internal/starlarksandbox` + stdlib."** A sandbox is a *security boundary*; duplicating it across Processor +
  Loom (+ the AI-authored-capabilities Fire-4 dry-run validator, which the ratified
  [`ai-authored-capabilities-design.md`](ai-authored-capabilities-design.md) §5 *explicitly names this same item*
  as its prerequisite) is precisely how an escape-hatch fix lands in one copy and not the others. The boundary's
  *intent* (per `actuator.go`) is "**no `internal/processor` import**" — i.e. no coupling to other *engines*; a
  dependency-free leaf is in the same category as `substrate` itself, so the intent is preserved.
- **Alternative: Option B — Loom re-implements its own sandbox wrapper** (importing only `go.starlark.net`, no new
  internal dep), exactly as `guard_eval.go` re-implements Refractor's `resolveProperty` rather than importing it.
  100 % boundary-pure and precedented — but it duplicates a *security-critical* sandbox across two-to-three places.

This is the only call that touches a standing principle. **No frozen-contract change** is needed either way — §10.5
already specifies the escape-hatch shape; this *builds to* it. **No architectural fork** beyond the boundary call.

**Build sequencing.** Independently shippable now; it does **not** wait on D1/Vault. The first real consumer is a
Loom pattern that needs a non-declarative guard (today's flows don't — so this can ship "the mechanism + a golden
fixture" without a vertical, like the contract anticipates). It *unblocks* AI-authored-capabilities Fire 4. Ratify →
the Lattice Steward builds (Fire 1 = the shared sandbox extraction, full 3-layer security review).

**Ratified (Andrew, 2026-06-29) — SPLIT.** The design splits into two pieces on opposite sides of the
guard-architecture concern (see #3 / [[feedback_no_new_engine_corekv_reads]]):
- **Piece 1 — the shared `internal/starlarksandbox` leaf: ✅ ratified, but built WITH its first real consumer,
  not speculatively.** Today the Processor is its only user, so there is no duplication to remove *yet*; extract
  the leaf when the **second** consumer is built — most likely **⑦ AI-authored-capabilities Fire 4** (which names
  this leaf as its prerequisite). Architecture-neutral + forward-compatible: if guard evaluation later moves
  Processor-side, the Processor evaluates Starlark guards with this same leaf.
- **Piece 2 — lighting up Loom-side Starlark guard evaluation: ⏸️ HELD, gated on the guard-evaluation-location
  decision** (#3's "better architectural approach for guards"). It enriches *Loom's* guard machinery — exactly
  what would be reworked if guards move Processor-side — so it waits on that call. The module-boundary fork
  (Option A vs B) is **moot until then** (if guards go Processor-side, Loom never imports the sandbox).
- **No frozen-contract change** either way (§10.5 already reserves the hatch). Restraint confirmed: don't extract
  a shared abstraction before its 2nd consumer exists; don't enrich a mechanism that may move.

---

## 1. Problem & intent

Loom guards are **on/off predicates** over a step's subject; an absent guard means "always run," a true guard means
"run this step," a false guard means "skip" (Contract #10 §10.5). Today the grammar is **declarative-only**: atoms
`{absent|present: <path>}` / `{equals: {path, value}}`, composed with `{allOf|anyOf|not}` into one boolean
(`internal/loom/guard.go`). That covers field-presence and scalar-equality — the "collect vs verify" reuse the
current LoftSpace/Clinic flows need.

It cannot express a predicate that is still *pure over the subject* but needs computation the grammar lacks:
a **range/threshold** comparison (`age >= 18`), a **cross-field** relation (`startDate < endDate`), a **string
shape** test (prefix/suffix/format), or a **small boolean expression** over several aspect fields. The contract
anticipated exactly this and **reserved** an escape hatch — `{ "reads": [...], "starlark": "def guard(subject):
return ..." }` — but left it *recognized-and-rejected* (`guard.go:29` `errStarlarkReserved`,
`loom.md:111`): *"The pure-evaluator extraction lands only when the first Starlark guard is authored."*

**Vision grounding.** Brainstorming #16 — *"Starlark sandbox embedding + determinism guard (no I/O, no time, no
random)"* — and #72 — *"deterministic-replay golden tests for Starlark scripts."* The platform already proved this
exact discipline in the **Processor's script sandbox** (`internal/processor/starlark_runner.go`, CI-gated, the
`internal/spike/starlark` correctness spike). This feature **extracts that proven discipline into a shared leaf**
and gives Loom a strict *predicate* mode of it.

**Why now / grounded demand.** Two **already-ratified** designs name this item as a prerequisite:
- [`ai-authored-capabilities-design.md`](ai-authored-capabilities-design.md) §5 / Fire 4 gates AI-authored
  Starlark DDL behind *"the verified-pure Starlark sandbox that is already a separate backlog item (Starlark
  guards (Loom))"* — it needs the same sandbox for a **dry-run validation** of generated scripts.
- The Loom grammar itself reserves it (§10.5) as the documented completion of the guard model.

It is the **shared, security-meaty primitive** under both — designing it deepens the readiness of two flagship
features at once.

---

## 2. The shape

This is an **engine-plane** feature — it adds **no vertices, aspects, links, lenses, or ops**. It extends the
*guard evaluator* inside Loom and extracts a shared sandbox leaf. P1/P2/P5 are untouched: a guard only **reads**
the subject (point-reads, the same as the declarative path resolver), never writes, never scans, never reads a lens.

### 2.1 The escape-hatch grammar (already contract-specified)

```jsonc
{
  "reads":    ["profile", "lease"],                 // subject aspect localNames to hydrate (root always hydrated)
  "starlark": "def guard(subject):\n    return subject.profile.data.age >= 18"
}
```

- **`starlark`** — a script that must define `def guard(subject)` returning a **Starlark `bool`**. Distinct entry
  point from the Processor's `def execute(state, op)`: a guard is a *predicate*, not a mutation producer.
- **`reads`** — the explicit list of subject **aspect localNames** the predicate may read. The engine hydrates
  exactly `{subjectKey}` (root, always) + `{subjectKey}.<aspect>` for each entry, builds the frozen `subject`
  value, and the predicate sees **only those**. An aspect not in `reads` is simply not in `subject` → reads as
  absent (`None`). This is the **no-scans / bounded-read-set guarantee**, mechanically identical to how the
  declarative grammar's parsed paths bound the read-set (`parseGuardPath` → `guardResolver.envelope`). `reads` is
  the Starlark analog of the declarative path's self-declared aspects.

### 2.2 The `subject` value (mirrors the declarative path grammar, not the Processor's `state`)

The contract says the predicate "gets `subject` exactly as a script gets `state`" — *structurally* (a frozen,
`.data`/`.class`/`.isDeleted`-shaped value), but **subject-rooted**, because a guard is always about one subject.
So `subject` is **not** the Processor's flat `key→doc` map; it is the root vertex projected so the declarative
paths translate one-to-one:

| Declarative path | Starlark access |
|---|---|
| `subject.data.<field>` | `subject.data.<field>` (or `subject.data["<field>"]`) |
| `subject.<aspect>.data.<field>` | `subject.<aspect>.data.<field>` |

- `subject.data` → the root vertex's `data` dict; `subject.class`, `subject.isDeleted`, `subject.revision` exposed.
- `subject.<aspect>` → that aspect's projected struct (`.data`, `.isDeleted`, `.revision`), for each `aspect` in
  `reads`. **A soft-deleted (`isDeleted`) or missing root/aspect projects as `None`** — matching the declarative
  `absent` semantics exactly (`guard_eval.go:envelope` returns `nil` for a tombstoned/missing envelope). So
  `subject.profile` is `None` when `profile` is soft-deleted, and `subject.profile.data.name` would raise — a
  guard author tests `subject.profile != None and subject.profile.data.get("name")`. (Rationale: keep Starlark
  absence *identical* to declarative absence, so the two grammars never disagree about "absent." A guard wanting
  tombstone-awareness is an edge case; documented, not defaulted-in.)
- The value is **`Freeze()`d** before `guard` is called — Starlark frozen values are immutable, so the predicate
  physically cannot mutate the snapshot.

### 2.3 The determinism contract (the binding property)

A Loom guard MUST be a **pure function** `guard(subject) -> bool`: same `subject` → same `bool`, **always**. This is
*stricter* than the Processor's `execute` (which legitimately mutates and does a non-deterministic live
`kv.Read`). The cursor-rebuild invariant (§10.6, `loom.md` "guard purity is binding, not a preference") **requires**
it: recovery re-runs guards from cursor 0 against current Core KV state, and a guarded step is "recovery-idempotent
by construction" *only if* the guard is a deterministic function of the state it reads. The sandbox enforces this
structurally — the predicate-mode globals are a **strict subset** of the Processor's:

| Capability | Processor `execute` | **Loom `guard`** | Why |
|---|---|---|---|
| `subject` / `state` (frozen) | ✅ `state` | ✅ `subject` (frozen) | the read surface |
| pure modules (`crypto.sha256`, `json`, `time.rfc3339_utc`) | ✅ | ✅ (same leaf set) | deterministic, side-effect-free |
| `kv.Read(key)` — **live** Core KV read | ✅ (opt-in, non-deterministic) | ❌ **excluded** | a live read breaks replay determinism |
| host clock / `$now` | ❌ (already) | ❌ | a clock breaks replay; **time-relative predicates are a Weaver concern** (`loom.md`: branching/temporal ⇒ Weaver) |
| `nanoid` mint | ✅ (requestId-seeded) | ❌ | a guard mints nothing |
| mutations / events output | ✅ `{mutations, events}` | ❌ — returns **`bool`** | a guard is a predicate, not a writer |
| `load(...)` / `os` / `http` / random | ❌ (Load nil; predeclared-probe) | ❌ (same) | sandbox escape / non-determinism |

`time.rfc3339_utc(s)` is included because it is **pure** (output = f(input), "does NOT expose the host clock" —
`starlark_runner.go:75`): it lets a guard normalize two *subject-supplied* timestamps for lexical comparison. It is
**not** a clock. There is **no `now`** — so "is the lease expired *as of today*" is **not** a Loom guard; it is a
Weaver convergence target. This boundary is doctrine, not omission, and I state it in the design + the doc.

### 2.4 The sandbox architecture (the shared leaf)

```
internal/starlarksandbox/            ← NEW leaf: imports only go.starlark.net + stdlib (zero internal deps)
  ├─ sandbox.go     Execute(ctx, source, entrypoint, args, globals, budget) → (starlark.Value, *SandboxError)
  │                 • SourceProgram(... globals.Has)  → unbound name (os/http) = SandboxViolation at COMPILE
  │                 • Thread{Load: nil}               → load(...) fails
  │                 • WithTimeout(ctx, WallBudget) + thread.Cancel on ctx; SetMaxExecutionSteps
  │                 • classifyError → SandboxViolation | ScriptError | ScriptTimeout | InvalidReturnShape
  ├─ modules.go     the PURE module set: cryptoModule(), timeModule(), json (the deterministic, I/O-free builtins)
  └─ convert.go     goValue↔starlark conversion helpers (the pure ones moved out of starlark_runner.go)

internal/processor/                  ← REFACTORED to consume the leaf
  starlark_runner.go   Run() builds {state, op, ddl, nanoid, crypto, time, json, kv} and calls sandbox.Execute
                       with entrypoint "execute"; parseScriptResult stays here. The IMPURE kv builtin
                       (starlark_kv.go — needs NATS/ScriptContext) STAYS Processor-side, passed in as a global.

internal/loom/                       ← NEW guard mode
  guard.go        parseGuard: the {reads, starlark} branch now COMPILES the script at pattern-load
                  (validate()), replacing errStarlarkReserved with a parsed starlarkGuard node.
  guard_eval.go   evalGuard: a starlarkGuard hydrates `reads` via the EXISTING guardResolver (one
                  snapshot per key), builds the frozen `subject`, calls sandbox.Execute(entrypoint
                  "guard"), and requires a bool result (else InvalidReturnShape → load/eval error).
```

The Processor keeps **byte-identical behavior** — its globals, its `execute` entry point, its
`{mutations, events, response}` parser, its `kv`/`nanoid` impure builtins are unchanged; only the *sandbox harness
+ pure modules + pure converters* move behind `sandbox.Execute`. The refactor is **behavior-preserving** and is
proven so by the Processor's existing `starlark_runner_test.go` + `starlark_builtins_test.go` +
`bypass/bypass_starlark_io_test.go` (the I/O-denial gate) passing **unchanged**.

### 2.5 Read path (P5) / write path (P2)

Untouched. A guard **reads** Core KV via point-reads (`guardResolver.fetchEnvelope` → `conn.KVGet`) — the same
known-key reads the declarative grammar already does; the engine, not a lens. It **writes** nothing and **emits**
nothing (it returns a bool to the cursor logic). The `internal/loom` substrate-only NATS posture is unchanged
(Starlark is a vendor lib, not a NATS handle). No orchestration precedent (Loom pattern / Weaver lens / `@every`)
is invoked — this is *inside* the existing Loom guard-eval path.

---

## 3. Contract surface

**No frozen-contract change.** Contract #10 §10.5 already specifies the escape hatch verbatim — the `{reads,
starlark}` shape, the `def guard(subject) -> bool` signature, "the same verified-pure sandbox the Processor uses
(`Load` nil; no I/O / env / NATS; deterministic)," and "`reads` is the read-hint." This design **builds to** that
text; it changes nothing in `docs/contracts/*`.

Two **doc** updates (not contracts), landed in the same commits as the code (the `loom.md` drift doctrine):
- `docs/components/loom.md` — "Guard grammar" §: the Starlark bullet flips from *reserved/rejected* to *supported*;
  the determinism table (§2.3 above) and the "no `now` — time-relative ⇒ Weaver" boundary are stated; the
  "Deferred (Phase 3+) — Starlark guard evaluation" entry under Implementation status moves to "Built (Phase 3)."
- A short `internal/starlarksandbox/doc.go` stating the leaf's purity charter + its zero-internal-dep rule.

If Andrew picks **Option A**, the Loom module-boundary statement (`internal/loom/doc.go:14`) is edited to admit the
leaf — a *doc/comment* change inside Loom, not a contract.

---

## 4. Migration & test strategy

**Migration is nil for existing flows.** Every shipped pattern is declarative — none carries a `{reads, starlark}`
guard, so the parse path for them is unchanged (the `set != 1` / declarative branches run exactly as today). The
Processor refactor is behavior-preserving (§2.4). There is no data migration, no key change, no bootstrap bump.

**Tests** (mirroring brainstorm #72 "deterministic-replay golden tests" + the §sandbox-correctness spike):

1. **Sandbox-correctness reuse** — port the `internal/spike/starlark/sandbox_correctness.go` forbidden-op battery
   (load/open/os/time/random rejected; arithmetic/string/comprehension permitted) into `internal/starlarksandbox`
   as a **real CI test** over the shared `Execute`, run in **both** entry-point modes. (Retire the spike, or keep
   it as a thin demo over the leaf — recommend retire: a spike that duplicates a shipped gate is debt.)
2. **Processor parity** — `internal/processor/starlark_runner_test.go` + builtins + the `bypass` I/O-denial gate
   pass **unchanged** against the refactored runner. This is the behavior-preservation proof.
3. **Loom guard unit table** (`guard_test.go` / `guard_eval_test.go`): a `{reads, starlark}` guard —
   - **parse/load:** a malformed script (syntax error) and a sandbox-violating script (`def guard(subject):
     return os.getenv("X")`) are rejected at `validate()` (pattern-load), same doctrine as a malformed declarative
     guard; a well-formed `def guard(subject)` compiles; a script missing `def guard` or with the wrong arity is a
     load error; a non-string `reads` entry is a load error.
   - **eval:** a range predicate (`age >= 18`) true/false; a cross-field predicate (`startDate < endDate`); a
     guard over a soft-deleted aspect sees `None` (absence parity with declarative); a non-bool return →
     InvalidReturnShape; a guard reading an aspect **not** in `reads` sees `None` (read-set bound proven); the
     wall-budget/step-limit fences fire (an infinite-loop script → ScriptTimeout, never a hung engine).
4. **Determinism / replay golden test** (the load-bearing property): the SAME `subject` snapshot evaluated twice
   yields the SAME bool; and a recovery-replay scenario (guard re-run from cursor 0 against an unchanged subject
   re-skips identically) — the executable proof that the cursor stays rebuildable. A frozen-`subject` mutation
   attempt inside the predicate fails (immutability proof).
5. **Hydration dedup** — a composite Starlark guard referencing two fields of the same aspect fetches it **once**
   (the §guardResolver one-snapshot-per-key property holds through the Starlark path).

Gates: `go build ./...`, `make vet`, `golangci-lint run ./...`, STRICT `lint-conventions`, `go test -race
./internal/starlarksandbox/... ./internal/processor/... ./internal/loom/...`, `make verify-kernel` (the Processor
script path is kernel-exercised), `make test-bypass` (Gate 2 — the I/O-denial bypass test is the security gate that
MUST stay green through the refactor).

---

## 5. Risks & alternatives

| # | Risk | Mitigation |
|---|---|---|
| R1 | **Refactor regresses the Processor's security sandbox** (the highest-stakes risk — the write-path script engine). | The refactor is *pure extraction*: same `SourceProgram(globals.Has)`, same `Load:nil`, same budgets, same classify. Proven by the Processor's existing tests + the `bypass` I/O-denial Gate-2 test passing **unchanged**. Fire 1 = **full 3-layer adversarial review**. |
| R2 | **A Starlark guard sneaks non-determinism** (host clock, random, a live read) and silently breaks cursor-rebuild. | Structurally impossible in predicate mode: no `kv`, no `now`, no `nanoid`, no `load`, Load nil, predeclared-probe rejects unbound names. The determinism golden test (§4.4) is the executable guard. |
| R3 | **A guard reads beyond `reads`** (an undeclared aspect), escaping the bounded read-set. | `subject` is built from the hydrated `reads` set only; an undeclared aspect is absent (`None`), never a live fetch. Tested (§4.3). Same bound the declarative grammar enforces. |
| R4 | **Module-boundary erosion** — admitting `internal/starlarksandbox` into Loom opens the door to importing engines. | The leaf is **dependency-free** (only `go.starlark.net` + stdlib) — a *primitive*, not an engine; same category as `substrate`. The boundary edit names *exactly* this one leaf. Flagged for Andrew (the fork). Option B avoids it entirely if he prefers. |
| R5 | **Author writes an expensive predicate** (heavy loop) on a hot step. | The wall budget (default 250ms, `PROCESSOR_SCRIPT_WALL_MS` analog) + `MaxExecutionSteps` fence each evaluation; ScriptTimeout fails the guard load-loudly, never hangs. Guard hydration is already per-step (no new GET volume beyond the declared `reads`). Brainstorm #107 (perf-budget harness) is the follow-on if it ever shows as a hotspot. |
| R6 | **Determinism of `time.rfc3339_utc`** — is normalization truly pure? | Yes — it parses+canonicalizes its *input* string; it does not read the clock (`starlark_runner.go:75` + the Processor's own determinism posture). It is the only "time" surface and it is input-only. If Andrew wants the guard surface *minimal*, drop it (a guard then compares raw ISO strings lexically) — noted as a trim option. |

**Alternatives considered & rejected:**
- **Loom-local re-implementation (Option B)** — fully designed in "For Andrew." Rejected as the *recommendation*
  (duplicates a security boundary) but presented as Andrew's choice; if picked, Fire 1's scope is Loom-only (no
  Processor refactor), at the cost of two sandbox copies (three once AI Fire 4 lands).
- **Generalize the declarative grammar** (add comparison operators `>=`/`<`, arithmetic) instead of Starlark.
  Rejected: it re-invents an expression language the platform already has (Starlark), grows the grammar
  unboundedly toward Turing-completeness without the sandbox's resource fences, and the contract already chose the
  Starlark escape hatch. The declarative grammar stays deliberately small (the 80 % case); Starlark is the 20 %.
- **Inject `$now` for time-relative guards** (mirroring Refractor). Rejected: it breaks replay determinism
  outright — a guard that depends on wall-clock is non-rebuildable. Time-relative convergence is Weaver's job by
  design (`loom.md`). Stated as doctrine.
- **Expose `kv.Read` to guards** (let a guard read related state). Rejected: non-deterministic + it is the
  "guard needs related state ⇒ Weaver signal" boundary (§10.5). A guard is pure over *its subject*, full stop.

---

## 6. Fire-by-fire decomposition (for the Lattice Steward)

Each fire is independently shippable and green. **Build only after ✅ Andrew-ratified** (and after he picks A vs B).
**Ratified sequencing (Andrew, 2026-06-29):** Fire 1 (the shared sandbox) builds **WITH its first consumer
(⑦ AI-authored Fire 4)**, not standalone/speculatively (the Processor is its only user today — no duplication to
remove yet); **Fire 2 (Loom-side Starlark guard) is HELD**, gated on the guard-evaluation-location decision
(#3's "better approach for guards"); the A-vs-B module-boundary fork is **moot until then**.

- **Fire 1 — extract the shared sandbox leaf (Option A) / or the Loom-local wrapper (Option B).** Create
  `internal/starlarksandbox` (`Execute` harness + pure modules + pure converters); refactor
  `internal/processor/starlark_runner.go` to consume it with **zero behavior change** (its `kv`/`nanoid`/`execute`
  parser stay Processor-side). Port the spike's forbidden-op battery into a real CI test. **Gate: the full
  Processor + bypass test suite passes unchanged. Full 3-layer adversarial review** (security plane — this touches
  the write-path script sandbox). *Ships value on its own: a single-source security sandbox + a CI'd correctness
  battery, even before any Starlark guard exists.* If Andrew picks B, Fire 1 is the Loom-local wrapper only and
  the Processor is untouched.

- **Fire 2 — light up the Loom guard escape hatch.** `parseGuard`'s `{reads, starlark}` branch compiles the
  script at `validate()` (replacing `errStarlarkReserved`); `evalGuard` hydrates `reads` via the existing
  `guardResolver`, builds+freezes `subject`, calls `sandbox.Execute(entrypoint "guard")`, requires a bool. Full
  Loom guard unit table + the **determinism/replay golden test** (§4.4) + hydration-dedup test. Update `loom.md`.
  **Full 3-layer review** (the cursor-rebuild invariant is correctness-critical). *Today-consumer note:* if no
  shipped flow needs a Starlark guard yet, Fire 2 lands the mechanism + a golden fixture pattern (the contract
  explicitly sanctions "lands when the first is authored" — the fixture *is* the first author); a real
  vertical predicate can follow from either stream.

- **Fire 3 (optional) — retire the spike + perf-budget follow-on.** Delete `internal/spike/starlark` (superseded by
  the Fire-1 CI test); if a guard hotspot ever appears, add the brainstorm-#107 perf-budget benchmark. Thorough
  lead review (cleanup). *Skippable / low marginal value — fold into Fire 1 if trivial.*

**Downstream unblock (no work here):** with Fire 1 landed, AI-authored-capabilities **Fire 4** can reuse
`internal/starlarksandbox` for its generated-script dry-run validation — the dependency that design names is
satisfied.

---

## 7. Self-adversarial pass (Designer, folded in)

Run as a solo adversarial sweep (the substantial-design rigor; a `bmad-party-mode` pass on the determinism/replay
boundary is **recommended before Fire 2** at build time, per the established pattern for these designs).

- **"Is `subject` really replay-deterministic given Core KV mutates between gen-1 and recovery?"** — Yes, with the
  same caveat the declarative grammar already accepts: a guard is deterministic *given the state it reads at
  evaluation time*. Recovery re-reads *current* state and re-evaluates; "recovery-idempotent by construction" means
  a guard that was satisfied stays satisfied (monotone-ish presence predicates) → re-skip. A Starlark guard has the
  **same** property as a declarative one — it is no weaker. The risk that a *non-monotone* Starlark predicate
  (true→false as state evolves) double-runs a guardless-adjacent step is the **pre-existing** guardless-recovery
  bound (the separate 📋 "[Loom] guardless-step recovery probe" row), not introduced here. **Folded:** the design
  states this and points authors to "give a guard whose satisfaction is monotone" — the same authoring guidance the
  declarative grammar carries.
- **"Does freezing `subject` actually block mutation?"** — `go.starlark.net` `Freeze()` makes values deeply
  immutable; a write raises at eval. The immutability test (§4.4) proves it. The predicate also returns a *new*
  bool, never the snapshot.
- **"Can a script define `guard` but also do top-level I/O at Init?"** — Top-level statements run in `prog.Init`
  under the same sandbox (Load nil, predeclared-probe), so a top-level `os.getenv` fails at compile/resolve exactly
  like the body. Tested via a top-level-violation case.
- **"Wrong arity / shadowing `guard`?"** — `defined["guard"]` must exist and be callable with one positional arg;
  a missing/2-arg/redefined-to-non-callable `guard` is a load-time InvalidReturnShape. Tested.
- **"Does the Processor refactor change error codes consumers depend on?"** — The classify logic moves verbatim
  (SandboxViolation/ScriptError/ScriptTimeout/InvalidReturnShape, incl. the `ClaimKeyInvalid` Processor-specific
  branch). The `ClaimKeyInvalid` branch is **Processor-domain** (auth claim flow), so it stays in the Processor's
  result-parser, NOT the leaf — the leaf classifies only sandbox/syntax/timeout; domain `fail("X: ...")` parsing
  stays caller-side. **Folded** into §2.4 (the leaf is domain-agnostic; each caller owns its result parser).
- **"Module-boundary AC test"** — if Story 8.1 has a literal import-assertion test for Loom, Option A updates it to
  allow the one leaf; Option B leaves it untouched. Flagged for the Steward to check at build (`grep` for an
  import-allowlist test in `internal/loom`).

---

## 8. Summary

Light up the contract's reserved `{reads, starlark}` guard hatch as a **pure, deterministic, frozen-subject
predicate** evaluated by the **same verified-pure sandbox the Processor proved** — extracted into a dependency-free
leaf so the security boundary has one source of truth (serving Processor + Loom + AI-authored Fire 4). No
frozen-contract change; the one call for Andrew is the module-boundary widening (Option A, recommended) vs a
Loom-local re-implementation (Option B). Independently shippable, unblocks the AI-native flagship's gated tier.

**Ratification state: 📐 awaiting-Andrew → ✅ Andrew-ratified** (then the Lattice Steward builds Fire 1).
