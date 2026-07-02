# Loupe operator-UI test strategy — a Go-native front-end regression net

**Status: ✅ RATIFIED — Andrew, 2026-07-02 (Fire 1: goja logic tier). Fire 2 (chromedp browser e2e) stays 🗄️ designed-shelved as proposed.**
Ratification amendments are folded into the body below: the Loupe 2.0 program
([loupe-2-ux-design.md](loupe-2-ux-design.md) §2.3) supersedes this design's original loading mechanism —
`app.js` decomposes into ES modules with a pure `logic/` tier, so the goja harness loads `logic/*.js`
per-module via the **strip-export transform** (goja has no ES-module support — upstream-verified), not the
originally-designed `init()`/`module.exports` shim on the monolith. **Fire 1 rides Loupe-2.0-F1** (the
module split happens there anyway); it is owned by the **Loupe lane** (`backlog/loupe.md`), not the
Lattice Steward.

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

**3.1 — The testable seam is the Loupe 2.0 `logic/` module tier (supersedes the original shim refactor).**
Loupe-2.0-F1 decomposes `app.js` into ES modules under `web/js/` with a pure `logic/` tier
(`loupe-2-ux-design.md` §2.3). The convention that makes that tier goja-loadable:

- A `logic/*.js` file contains **only declarations** — no `import`, no DOM / `fetch` / timer / `async`
  references — and exactly **one trailing `export { … }` statement**.
- **Syntax stays ES6-conservative** (goja = ES5.1 + "most of ES6", upstream-verified): no optional
  chaining, no nullish coalescing, no async in `logic/` files; `Object.values`-class ES2017+ built-ins get
  their trivial ES5 spellings. The harness's parse failure is the loud enforcement — a gap is caught at
  `go test`, never shipped.
- The op-form coercion body still extracts to a pure `coerceField(name, type, raw, isRequired) →
  {value}|throw` (the highest-value untested logic — a silent mis-coercion mis-submits an op), landing in
  `logic/reads.js` per the 2.0 module map.

DOM wiring lives in the view modules and `main.js` — side-effect-free logic loading is a property of the
module split itself, no `init()` wrapper or `typeof module` shim needed.

**3.2 — The goja harness (`cmd/loupe/web_logic_test.go`, `package main`).** A single Go test file that:
1. Reads each `web/js/logic/*.js` via the same `embed.FS` the server uses, so the test asserts the
   *shipped* assets, never copies.
2. Applies the **strip-export transform** — drop the file's single trailing `export { … }` line (a 2-line
   string transform in the test; goja has no ES-module support) — then constructs one `goja.Runtime` per
   logic file and `RunString`s the stripped source; the declared functions are read back by name via
   `runtime.Get`.
3. Table-drives assertions against Go-authored expectations — e.g. `deriveReads({target:"vtx.role.r1", nested:{k:"lnk.a.b.c.d.e"}, n:3})` → `["vtx.role.r1","lnk.a.b.c.d.e"]`; `coerceField("age","integer","x",true)` throws; `parseRoute("#/graph/vtx.role.abc?view=hood")` → `{view:"graph", arg:"vtx.role.abc", params:{view:"hood"}}`; `isEntityKey` driven by the **same case table as the Go `classifyKey` tests** (the cross-language drift pin); `sysmapTier({kind:"lens"})` == 4; `issueClass("[error] x")` == `"card-issue bad"`.

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

**Fire 1 — goja logic tier (RIDES Loupe-2.0-F1, Loupe lane).**
1. The `logic/` module tier + strip-export convention land as part of Loupe-2.0-F1's decomposition
   (§3.1 — no separate refactor fire).
2. Add `github.com/dop251/goja` (test-only) to `go.mod`; add its `docs/vendors.md` row.
3. Add `cmd/loupe/web_logic_test.go` (`package main`): load each shipped `logic/*.js` via the
   strip-export transform, table-test `deriveReads`, `coerceField`, `parseRoute`, `isEntityKey` (shared
   case table with the Go `classifyKey` tests), `schemaTypeLabel`, `shortId`, `sysmapTier`, `issueClass`
   (§3.2); grow the tables as later 2.0 fires add `logic/` modules (`status.js`, `feed.js`, `hood.js`).
4. Update `docs/components/loupe.md` (testing note) and `agents/fe-engineer/SKILL.md` §3 (the goja-tier line). Gates: `go build`, `make vet`, `golangci-lint`, `lint-conventions`, `go test ./cmd/loupe/...` green; browser smoke per FE skill. Independently green; no CI-topology change.

**Fire 2 — real-browser behavioral e2e (🗄️ designed-shelved; build only after Andrew accepts the CI-browser flake weight + a real driver).**
1. Add `github.com/chromedp/chromedp` (test-only).
2. `cmd/loupe/web_e2e_test.go` behind `//go:build loupe_e2e`: `httptest.Server` serving the embedded UI + a stubbed `/api/*` fixture; drive headless Chrome; assert each tab (`data-tab` in `index.html`: systemmap/corekv/health/tasks/control/packages/files/op) loads, renders the stub JSON, and error paths surface inline.
3. A **separate** CI step (not `-p 4`), so a browser flake can never red the main unit gate — and hand the flake-budget to the Whetstone to own.
   *Not started until ratified-to-build; this fire realizes value only once the DOM-render regression risk is real and the flake weight is accepted.*

## 10. Adversarial pre-build gate

This design self-flags no deferred party-mode/adversarial pass as a build precondition — Fire 1 is a small, test-only, no-contract, no-data-path increment (one Go dep + one test file + a behavior-preserving refactor), which the CLAUDE.md rubric places below the "substantial/security-plane" bar that mandates the 3-layer pass; a thorough lead review at build (the `fe-engineer` gates in §9) is sufficient and is stated as such. The one *decision* worth adversarial attention — the runner fork and the jsdom-dominance argument — is resolved in-doc (§4) with the trade-off table and the deferral rationale, and is Andrew's ratification call, not a Steward-time gate. Fire 2, if greenlit, is where a browser-flake review belongs and is handed to the Whetstone (§9).
