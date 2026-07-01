# Loupe operator-UI test strategy — a Go-native front-end regression net

**Status: 📐 awaiting-Andrew (ratification).** Design/doc-only; the Lattice Steward builds Fire 1 only after ✅ Andrew-ratified.

> **For Andrew (one-look decision)**
>
> **What it does (2 lines):** Loupe's 1142-LOC vanilla `app.js` has *zero* automated coverage — the only JS gate today is `node --check` (syntax) plus a manual `claude-in-chrome` pass. This closes the `fe-engineer` skill's own standing escalation (*"don't introduce a build step or framework without a design decision (escalate that to Winston)"*, `agents/fe-engineer/SKILL.md` §3, §Notes) by giving the FE lane a **Go-native** regression net: extract `app.js`'s pure logic into a testable seam and cover it with **goja** (a pure-Go JS interpreter) inside `go test ./cmd/loupe/...` — **no Node, no `package.json`, no `node_modules`, no build step.**
>
> **The one thing to bless (dependency fork — not a contract fork):** adopt **`github.com/dop251/goja`** as a **test-only, pure-Go** dependency (add a `docs/vendors.md` row). The alternatives are a **Node + Vitest + jsdom** toolchain (a JS build-chain in a pure-Go repo — my recommend-against) or **status-quo** (accept the debt). **My recommendation: goja.** See §4 for the matrix and the "jsdom is the dominated middle" argument.
>
> **A second, explicitly-deferred call:** whether Loupe ever gets a *real-browser* behavioral e2e (Fire 2, `chromedp`) for the ~93% of `app.js` that is DOM-render glue. It is **designed but sequenced behind your acceptance of the CI-browser flake weight** (the Whetstone is actively fighting flake) **and a real regression driver** — dead-scaffolding-tested out of Fire 1. Ratifying this design ratifies Fire 1 (goja logic tier); Fire 2 stays 🗄️ designed-shelved until you say the browser tier is worth it.
>
> **No frozen-contract change.** Loupe is an application with no interface contract of its own (`docs/components/loupe.md` §header); no `docs/contracts/*` section is touched. Nothing is staged uncommitted.

---

## 1. Problem + intent

Loupe (`cmd/loupe`) is the internal view-and-control client for a running Lattice — the operator's window onto the platform, and the **first Edge-Lattice prototype** (`docs/components/loupe.md`). Its Go server is *well* covered: every handler (`corekv`, `vertex`, `ops`, `health`, `control`, `objects`, `systemmap`, `tasks`, `server`, `main`, `op`) ships a `_test.go` that drives the route mux through `httptest` with a nil NATS conn (the pattern in `cmd/loupe/server_test.go`). The **front-end has none.** `cmd/loupe/web/app.js` is 1142 lines of vanilla JS (`"use strict"`, no framework, no modules) and the *only* automated check that touches it is `node --check` in the `fe-engineer` verify loop — a **syntax parse**, not a behavioral assertion. Everything else is a human opening the browser.

That gap matters more over time, not less. The **Agentic Operating Model** runs a dedicated `fe-engineer` role (paired with the UX Designer, Sally) that builds Loupe's operator UI *and* every vertical app's FE (`agents/fe-engineer/SKILL.md`; `agentic-ops-design.md` §2, §4). As that lane keeps extending `app.js`, an autonomous builder has **no regression net**: it can only re-run a syntax check and eyeball a screenshot. The board rates this **★★ / L** and flags it `🔭 flag-for-Andrew` precisely because the fix — standing up *any* JS test capability — is an architectural call the FE skill defers to Winston. **This design is that decision.**

The intent is not "maximize JS coverage." Loupe is a **trusted, loopback-only, single-identity prototype** whose substantive logic (all NATS I/O, all authz-relevant behavior) lives in the *already-tested* Go handlers; the browser is deliberately a *thin view* that "only ever calls `/api/*`" (`docs/components/loupe.md` §Overview). The intent is a **proportionate, Go-native, flake-free regression net** for the part of the FE that is genuinely *logic* — and a clear, sequenced answer for the DOM-render part — that the `fe-engineer` can lean on going forward.

## 2. Grounding — what is and isn't tested, and the shape of `app.js`

**Repo baseline (verified this fire):** pure Go 1.26.1; **no** `package.json` / `node_modules` / Vitest / Jest / Playwright / goja / chromedp anywhere (`go.mod`, `go.sum` clean). CI (`.github/workflows/ci.yml`) runs `go test ./... -p 4` (embedded NATS, no Docker) as the `unit` job and a Docker `verify-package-*` integration job; **no JS step of any kind.** Loupe assets are `go:embed`'d (`cmd/loupe/server.go` `//go:embed web` → `http.FileServer`).

**The critical measurement — `app.js` is ~93% DOM glue, ~7% logic.** Across 1142 lines: **26** `document.` references, but only **2** `fetch(` and **2** `URLSearchParams`. The functions cleave cleanly:

- **Pure logic (no DOM / no `fetch` / no `async`) — the valuable, breakable core:**
  - `deriveReads(payload)` — walks an op payload collecting every `vtx.*`/`lnk.*` key-shaped string to auto-populate `ContextHint.Reads`. This is **correctness-bearing** (a miss = a rejected or under-declared op) and pure.
  - `collectOpForm()`'s per-field **coercion** (boolean/number/array-object-JSON/required-missing → typed value or thrown error) — currently entangled with a DOM iteration, but the coercion rule is a pure function of `(name, type, raw, required)`.
  - `schemaTypeLabel(p)` — JSON-Schema property → type label (`enum` / `t1|t2` / `t` / `any`).
  - `shortId(key)` — drops the `vtx.<type>.` prefix (mirrors the Go `shortId`).
  - `pretty(v)` — safe `JSON.stringify` with a `catch`.
  - `sysmapTier(node)` — node kind/id → tier 0–4 (drives the system-map layout).
  - `issueClass(text)` — `[error]`-prefixed issue → red vs. yellow class.
  - the classifier lookup maps: `componentStatusClass`, `lensDotClass`, `lensGlyph`, `sysmapControlComponents`.
- **DOM-render / fetch orchestration (needs a DOM or a browser):** everything else — the `load*` fetchers, the `render*` builders, `el`/`$`/`$all`, `switchTab`/`lazyLoad`, `buildInput`, and the whole system-map SVG-edge drawing (`drawSysmapEdges`, ~100 lines).

**Consequence for the fork:** the pure-logic seam is *small but real and correctness-bearing*; the bulk is thin DOM glue. A logic-tier test runner (goja) covers the first for near-zero cost and no browser; the second is only faithfully testable in a **real** DOM (a browser). A *simulated* DOM (jsdom) sits between — more cost than goja for the logic, less fidelity than a browser for the rendering. That observation drives the recommendation.

**goja's authoritative capabilities (from `github.com/dop251/goja` README, this fire — vendor-docs rule):** full **ECMAScript 5.1** + *most of ES6*; **pure Go, no cgo, no external deps**; **no** `document`/`window`/`fetch`/`setTimeout` (host must provide any of those); minimum Go **1.25** (we are on 1.26.1 ✓). It is the pure-Go JS engine used by k6/Grafana and others. Confirmed sufficient for the pure functions above (all ES5.1/ES6, no host objects). One caveat handled in §7 (risk R1): `deriveReads` uses `Object.values`; if goja's ES-subset lacks it, the one-line rewrite `Object.keys(v).map(k => v[k])` removes the dependency — a build-time check, not a design blocker.

## 3. The shape (Fire 1 — the ratifiable increment)

**Mirror the established pattern, don't invent one.** Loupe's Go tests already run pure helpers through `go test` in `package main` (`cmd/loupe/server_test.go`). Fire 1 adds a sibling test file in the *same package* that runs the FE's pure helpers through goja — one harness, one `go test ./cmd/loupe/...`, one CI job (the existing `unit` job). No new pipeline, no framework, no build step, honoring the `fe-engineer` skill's "never reframework a vanilla-JS surface."

**3.1 — Make `app.js` load side-effect-free (a small, honest refactor).** Today `app.js` wires DOM listeners at top level (e.g. `$all(".tab").forEach(btn => btn.addEventListener(...))`, line 65), so *loading* the file touches `document` — it would throw under goja. The refactor:

- Move every top-level DOM-wiring statement into a single `function init() { … }`, invoked only in a browser: `if (typeof document !== "undefined") init();` (or a `DOMContentLoaded` hook — matching how the file already gates panels). Pure function *declarations* stay at top level.
- At the file bottom, add a test-only export shim, guarded so the browser never sees it:
  ```js
  if (typeof module !== "undefined" && module.exports) {
    module.exports = { deriveReads, schemaTypeLabel, shortId, pretty, sysmapTier,
                       issueClass, coerceField, componentStatusClass, lensDotClass };
  }
  ```
  goja is `CommonJS`-shaped enough that the harness can define a `module` object, `RunScript(app.js)`, and read `module.exports` back (§3.2). This is **zero framework** — a 3-line vanilla idiom, invisible to the served page.
- Extract the coercion body of `collectOpForm()` into a pure `coerceField(name, type, raw, isRequired) → {value}|throw`, and have `collectOpForm` call it per field. This turns the highest-value untested logic (typed op-form coercion, the thing that silently mis-submits an op) into a unit-testable function without changing behavior.

This refactor is *itself* the "separate logic from wiring" cleanup that makes the file importable by **any** future runner (goja now, jsdom/browser later) — it is not throwaway scaffolding; it is a durable structural improvement that pays off regardless of which tier(s) exist.

**3.2 — The goja harness (`cmd/loupe/web_logic_test.go`, `package main`).** A single Go test file that:
1. Reads `web/app.js` via the same `embed.FS` the server uses (or `os.ReadFile` at test time — the file is in-tree), so the test asserts the *shipped* asset, never a copy.
2. Constructs a `goja.Runtime`, defines a minimal `module = {exports:{}}` host object, `RunString`s the source, and `Export()`s the functions.
3. Table-drives assertions against Go-authored expectations — e.g. `deriveReads({target:"vtx.role.r1", nested:{k:"lnk.a.b.c.d.e"}, n:3})` → `["vtx.role.r1","lnk.a.b.c.d.e"]`; `coerceField("age","integer","x",true)` throws; `schemaTypeLabel({enum:[…]})` == `"enum"`; `sysmapTier({kind:"lens"})` == 4; `issueClass("[error] x")` == `"card-issue bad"`.

The harness lives entirely in Go; the assertions are Go test cases; goja is an in-process library call. It runs under `-p 4` with the rest of the unit suite, adds no Docker, no browser, no network.

**3.3 — Read/write-path invariants: N/A but confirmed.** This is test-only and touches no data path. P5/P2 are unaffected (no new Core-KV read/write, no lens, no op). Contract #1 key-shapes are *asserted* by the `deriveReads` test (it verifies the `vtx.`/`lnk.` prefix discrimination), never changed. No orchestration.

**3.4 — CI + gates.** Fire 1 changes nothing in `ci.yml`: the new `_test.go` is picked up by the existing `go test ./... -p 4`. Local gates from the `fe-engineer` skill still pass unchanged (`go build`, `make vet`, `golangci-lint`, `lint-conventions`, `go test ./cmd/loupe/...`). Add one `docs/vendors.md` row for goja (role: *test-only pure-Go JS interpreter running `cmd/loupe/web/app.js`'s pure logic under `go test`*; authoritative source: `github.com/dop251/goja` README; pin: the `go.mod` version chosen at build). vendors.md is a regular doc (not a frozen contract) — the Steward adds the row in the build commit.

## 4. The fork — FE test strategy (design-through + recommendation)

Four options, judged against the repo's stated values (pure-Go, lean, *no build step / no framework without a decision*, and an **active CI-flake-reduction program** — the Whetstone):

| Option | Covers | New deps / weight | CI-flake risk | Verdict |
|---|---|---|---|---|
| **A. goja (pure-Go engine)** | the extracted pure-logic seam | 1 pure-Go test-only module; **no** toolchain, no build step, folds into `go test` | ~none (in-process, deterministic) | **✅ recommended (Fire 1)** |
| **B. Node + Vitest + jsdom** | logic **+ simulated** DOM | `package.json` + `node_modules` + Vitest cfg + a **new CI job** (Node setup); makes `app.js` importable | low–med (Node install + jsdom quirks) | ✗ **the dominated middle** (see below) |
| **C. chromedp (real headless Chrome, Go-native)** | logic **+ real** DOM/interaction | 1 Go module, but needs a **Chromium binary** in CI; `httptest`-served UI + stubbed `/api/*` | **high** (real browser = the classic flake source) | 🗄️ **designed, deferred (Fire 2)** |
| **D. Status quo** (`node --check` + manual browser) | nothing (syntax only) | none | none | ✗ leaves a growing FE with no net; board ★★ says address it |

**Recommendation: A now (Fire 1), C designed-and-deferred (Fire 2), reject B, reject D.**

**Why B (jsdom) is the dominated middle — the load-bearing argument.** jsdom's whole value is a *simulated* DOM. But for Loupe:
- For the **logic** tier, jsdom buys nothing over goja *and* costs a full Node toolchain in a pure-Go repo (against `fe-engineer` §Notes and CLAUDE.md's leanness) plus a new CI job the Whetstone would then have to keep fast.
- For the **DOM** tier, a *simulated* DOM is strictly less faithful than a *real* one — jsdom notoriously diverges on layout, SVG (Loupe draws the system-map edges as SVG), and event quirks. If we are going to pay to test rendering, a real browser (C) is the honest test; jsdom would give false confidence on exactly the SVG/layout code most likely to break.
- So the Go-native path **dominates**: goja is *cheaper* than Node for the logic, and a real browser is *more faithful* than jsdom for the DOM — and the DOM tier can be **deferred** until it's worth the flake, which jsdom-in-one-toolchain cannot cleanly be.

**Why C is deferred, not dropped.** A real-browser e2e is the *right* tool for the DOM-render 93% — but it is **dead scaffolding today**: the dead-scaffolding test asks *"does this increment realize value before its dependency/consumer exists?"* The consumer (a concrete regression it would have caught) is speculative, and it imports the single biggest CI-flake liability into a repo whose Whetstone lane exists to *remove* flake. Building it now trades a real, active anti-flake investment for hypothetical coverage of thin glue that the `fe-engineer`'s headless-first + `claude-in-chrome` pass already eyeballs. **Correct output: ratify the design, shelve the build behind (a) Andrew accepting the CI-browser weight and (b) a real regression driver.** When built, it is `//go:build loupe_e2e` (opt-in, *not* in `-p 4`) in its own CI step, serving the embedded UI via `httptest.Server` with a stubbed `/api/*`, driven by `chromedp` (Go-native — no Node) — sub-fork noted: chromedp vs. Playwright-Node; recommend chromedp to keep the repo Node-free.

## 5. Reconciliation with the existing mental model

- **"Didn't we already test Loupe?"** Yes — the *Go server*. Every handler has a `_test.go` (`server_test.go` et al.). What has **no** test is the *browser JS* (`app.js`); the only JS gate is `node --check` (syntax). This closes exactly that gap, and only that gap.
- **"Doesn't this contradict 'no build step / no framework'?"** No — it *honors* it. goja is a Go library; there is no JS build step, no framework, no `node_modules`. That constraint is precisely *why* the recommendation is goja over Node (§4). The refactor keeps `app.js` vanilla.
- **"New state / new toolchain?"** One new **test-only, pure-Go** dependency (goja), recorded in `docs/vendors.md`. No runtime dep, no CI-topology change, no new state in the platform.
- **"Is this a Phase-1 simplification we're reversing?"** No. Loupe's FE was never *designed* untested — coverage simply wasn't stood up because doing so was an architectural call (which runner). This *is* that call, made once.

## 6. Migration / compatibility

- **Behavior-preserving refactor.** §3.1 moves DOM wiring into `init()` and extracts `coerceField`; the served page behaves identically (the browser still calls `init()`; the export shim is `typeof module`-guarded and never runs in-browser). The `fe-engineer`'s existing verify loop (`node --check` + a `claude-in-chrome` smoke on the running UI) validates no visible regression before admit.
- **No data migration, no DDL, no contract, no bootstrap-version bump.** Test-only.
- **Backwards-compatible with the FE skill.** After Fire 1, `agents/fe-engineer/SKILL.md` §3 gains a line: JS logic is now covered by `go test ./cmd/loupe/...` (the goja tier), so a FE change to a pure helper must extend that table-test — turning `node --check` from "the only JS gate" into "the syntax floor beneath a logic net." (Skill edit is the Steward's build step, per the improvement-loop discipline, not a design artifact.)

## 7. Risks + alternatives

- **R1 — goja ES-subset gaps.** goja is ES5.1 + *most* ES6; a pure helper might use a feature it lacks (`deriveReads` uses `Object.values`; `coerceField` uses `Number.isNaN`). *Mitigation:* these have trivial ES5.1 rewrites (`Object.keys(v).map(k=>v[k])`; `isNaN`/`n!==n`) applied only if the build surfaces a gap; the harness fails loudly at `RunString` if the engine can't parse, so a gap is caught at build, never shipped. Not a design blocker.
- **R2 — the export-shim / `init()` refactor introduces a regression in the shipped page.** *Mitigation:* the change is mechanical (wrap existing top-level statements; extract one pure function); covered by the `fe-engineer`'s mandatory browser smoke before admit; and small enough for a single reviewed fire.
- **R3 — goja drift from browser JS semantics.** The functions under test are pure ECMAScript with no host objects, so browser/goja semantics coincide by construction; anything requiring host objects is *out of scope for the logic tier by definition* (it belongs to the deferred browser tier). This is a feature, not a risk — it enforces the logic/render split.
- **R4 — "why not just accept the debt (D)?"** Re-asked per alternatives discipline: could status-quo *beat* the recommendation? For a throwaway prototype, arguably — but the FE is *actively grown by an autonomous `fe-engineer`* with no net, the board rates it ★★, and the recommended increment is genuinely cheap (one Go dep, one test file, no CI change). The cost/benefit favors the small net over indefinite debt. D loses.
- **Alternative — extract *more* logic to shrink the untested DOM %.** Each `load*`/`render*` could be split into a pure `shape(json)→viewModel` + a thin `render(viewModel)`, growing the goja-testable seam. *Deferred, not now:* it's an invasive rewrite of an untested file (chicken-and-egg) for diminishing returns, and the render layer still needs the browser tier for true fidelity. Fire 1 extracts only the clearly-pure, clearly-valuable functions; wider extraction is an opportunistic follow-on the `fe-engineer` can do incrementally as it touches each renderer.

## 8. Contract surface

**None.** Loupe is an application with no frozen interface contract (`docs/components/loupe.md` §header: *"it has no frozen interface contract of its own"*). No `docs/contracts/*` section is touched; nothing is staged uncommitted. The only doc touched at build time is `docs/vendors.md` (a regular doc — a new vendor row), plus `docs/components/loupe.md` gaining an "Implementation status → testing" note and `agents/fe-engineer/SKILL.md` gaining the goja-tier line (both Steward build-time doc updates, not contract changes).

## 9. Fire-by-fire decomposition (for the Lattice Steward)

**Fire 1 — goja logic tier (the ratifiable, independently-shippable increment).**
1. Refactor `cmd/loupe/web/app.js`: wrap top-level DOM wiring in `init()` gated on `typeof document`; extract `coerceField` from `collectOpForm`; add the `typeof module`-guarded export shim (§3.1). Behavior-preserving.
2. Add `github.com/dop251/goja` (test-only) to `go.mod`; add its `docs/vendors.md` row.
3. Add `cmd/loupe/web_logic_test.go` (`package main`): load the shipped `app.js` via goja, table-test `deriveReads`, `coerceField`, `schemaTypeLabel`, `shortId`, `pretty`, `sysmapTier`, `issueClass`, and the classifier maps (§3.2).
4. Update `docs/components/loupe.md` (testing note) and `agents/fe-engineer/SKILL.md` §3 (the goja-tier line). Gates: `go build`, `make vet`, `golangci-lint`, `lint-conventions`, `go test ./cmd/loupe/...` green; browser smoke per FE skill. Independently green; no CI-topology change.

**Fire 2 — real-browser behavioral e2e (🗄️ designed-shelved; build only after Andrew accepts the CI-browser flake weight + a real driver).**
1. Add `github.com/chromedp/chromedp` (test-only).
2. `cmd/loupe/web_e2e_test.go` behind `//go:build loupe_e2e`: `httptest.Server` serving the embedded UI + a stubbed `/api/*` fixture; drive headless Chrome; assert each tab (`data-tab` in `index.html`: systemmap/corekv/health/tasks/control/packages/files/op) loads, renders the stub JSON, and error paths surface inline.
3. A **separate** CI step (not `-p 4`), so a browser flake can never red the main unit gate — and hand the flake-budget to the Whetstone to own.
   *Not started until ratified-to-build; this fire realizes value only once the DOM-render regression risk is real and the flake weight is accepted.*

## 10. Adversarial pre-build gate

This design self-flags no deferred party-mode/adversarial pass as a build precondition — Fire 1 is a small, test-only, no-contract, no-data-path increment (one Go dep + one test file + a behavior-preserving refactor), which the CLAUDE.md rubric places below the "substantial/security-plane" bar that mandates the 3-layer pass; a thorough lead review at build (the `fe-engineer` gates in §9) is sufficient and is stated as such. The one *decision* worth adversarial attention — the runner fork and the jsdom-dominance argument — is resolved in-doc (§4) with the trade-off table and the deferral rationale, and is Andrew's ratification call, not a Steward-time gate. Fire 2, if greenlit, is where a browser-flake review belongs and is handed to the Whetstone (§9).
