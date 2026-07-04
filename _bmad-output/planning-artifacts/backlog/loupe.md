# Backlog — Loupe (Stream 3): the operator console

Stream 3 = the Loupe console (`cmd/loupe`: Go handlers + `web/` UI). Pipeline: **PO review** files the
program → **Sally** (bmad-agent-ux-designer) produces the UX design → **Winston adjudicates**
(Andrew delegated design ratification for this program, 2026-07-02 — no 📐-awaiting-Andrew gate here) →
the **Loupe Steward** builds fires UX-then-FE. Index + cross-lane rules: [../backlog.md](../backlog.md);
row discipline: [lattice.md → "How this board works"](lattice.md) (lint-board covers this file).

**Lane boundaries.** Code scope is `cmd/loupe/**` (+ its tests). A needed platform primitive
(engine/op/substrate) or deploy/contract change routes per the cross-lane rules — file to
[lattice.md](lattice.md) and `🚧 blocked-on:` it (trivial established-pattern mirrors excepted).
**Concurrency:** this lane runs in PARALLEL with both other streams (Andrew, 2026-07-02) — it does NOT
take the shared build lock; Loupe fires serialize among themselves on `/tmp/lattice-loupe-build.lock`.

## Loupe 2.0 — "the map is the console" (the program)

PO review 2026-07-01 (Andrew session); UX design **adjudicated 2026-07-02** (Winston, Andrew-delegated):
[loupe-2-ux-design.md](../../implementation-artifacts/loupe-2-ux-design.md) — build fires per its §14;
one FE fire at a time; each fire retires a tab only in the same fire as its replacement.
**Extended 2026-07-02** with the platform-edges fires F10–F13 (Gateway/Vault/Chronicler onto the curated map +
the Chronicler Time Machine) — brief:
[loupe-platform-edges.md](../../implementation-artifacts/loupe-platform-edges.md); UX **adjudicated 2026-07-02**
(Winston): [loupe-platform-edges-ux.md](../../implementation-artifacts/loupe-platform-edges-ux.md) — F10
buildable-first; F11–F13 gated on lattice cross-lane asks (§6 there).

| Item | What it is | Imp | Size | State |
|---|---|---|---|---|
| **F1 — Console shell** | Hash router + route table, ES-module `logic/` split (strip-export convention), goja harness + dep + vendors row, `keyLink` resolver seed (link rows far-end-clickable + provenance chips), breadcrumbs. | ★★★ | M | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F2 — Graph explorer** | Faceted/grouped/paged `#/graph` list, linkifying doc renderer, detail re-plumb, ego-graph hood mode; retires Core KV tab. | ★★★ | L | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F3 — Component pages** | `#/component/<id>` ×6, plural instances (fixes LWW collapse), row-level control actions, refractor roster; retires Control tab. | ★★★ | L | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F4 — Health absorption + status vocabulary** | Global alert strip (verbatim `health.alerts.*` incl. stub-auth-active), gates panel + rail (preserves `#sysmap-console` slot), `renderedState` incl. `pending-readpath` (the "7 degraded" fix); retires Health tab. | ★★★ | M | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F5 — Lens page** | Four panels: definition (DDL) · state (+freshness slot) · control (delete behind typed confirm) · contents (nats_kv now, pg-pending state). | ★★★ | L | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F6 — Live pulse** | SSE tail of core-events (deliver-new, bounded), rail feed, map edge pulse animation, topbar LED, degraded modes. | ★★ | M | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F7 — Submit-Op follow-through** | Structured accepted panel (committed keys linkified), `#/op?type=` prefill, session op log, ~12s requestId-filtered follow-through riding the F6 feed. | ★★ | S | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F8 — Packages first-class** | `#/package/<key>` graph-resolved contents + install/upgrade/uninstall behind typed confirms (F-004 mechanics). | ★★ | M | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) |
| **F9 — Postgres read seam (lens contents)** | Read-only PG connector (`LOUPE_PG_DSN`, SELECT-only role) lighting up the §6.4 panel for protected lenses + grant tables. Adjudicated in principle (design §15 Q6); role provisioning files to lattice lane if deploy/bootstrap-touching. | ★★ | M | ✅ shipped · checkpoint in [design §14](../../implementation-artifacts/loupe-2-ux-design.md) · full value needs the read role (lattice) |
| **F10 — Curated topology + Gateway node** | `declaredComponents`/`skeletonEdges`/`sysmapTier` for all three (Gateway top-of-map external door · Vault side of Core-KV · Chronicler mirror of Refractor); design-ahead render until live. | ★★★ | M | ✅ shipped · checkpoint in [UX doc](../../implementation-artifacts/loupe-platform-edges-ux.md) · flip Gateway `designAhead` off when up-full starts it (lattice) |
| **F11 — Gateway security console** | `#/component/gateway` page (auth metrics + JWKS key set) + the token-revoke surface (arch-review gap). | ★★ | M | ✅ shipped · checkpoint in [UX doc](../../implementation-artifacts/loupe-platform-edges-ux.md) · JWKS panel lights up on the heartbeat `jwks` block; live e2e needs Gateway up-full + fresh bootstrap (lattice) |
| **F12 — Vault surface + crypto-shred proof** | Node + page + Reveal (decrypt RPC on `sensitive` aspects) + `ShredIdentityKey` before/after proof. | ★★★ | L | 🚧 blocked-on: Vault→Loupe enablers (lattice) · [UX §3](../../implementation-artifacts/loupe-platform-edges-ux.md) |
| **F13 — Chronicler Time Machine** | Flow-history browser + map scrubber + ledger browser (platform-edges brief §4 L1–L3); overrides the Chronicler design's "rides F6" display note (Loupe scope). | ★★★ | L | 🚧 blocked-on: Chronicler build (lattice) · [UX §4](../../implementation-artifacts/loupe-platform-edges-ux.md) |

## Component maintenance

| Item | What it is | Imp | Size | State |
|---|---|---|---|---|
| **[Loupe] Same-origin gate console-wide** | Extend F8's `crossOriginBlocked` to the pre-existing mutating endpoints (`/api/op`, `/api/control/*`, `/api/objects`) — the loopback console's cheap CSRF gate, applied uniformly. | ★★ | XS | ✅ done (shipped with F9, + DNS-rebinding hardening) |
| **[Loupe] Static-UI serving (`go:embed web`) untested** | The embedded operator-UI mount has no coverage. | ★ | XS | ✅ done (shipped with F1) |
| **[Loupe] Operator UI has no automated coverage** | goja logic-tier harness for the pure `logic/*.js` seam. Fire 2 (chromedp browser e2e) stays 🗄️ designed-shelved. | ★★ | S | ✅ done (shipped with F1) · [design](../../implementation-artifacts/loupe-fe-test-strategy-design.md) |

## Parked

| Item | Why it's parked | Imp | Size | State |
|---|---|---|---|---|
| Loupe agent-activity console | The ops layer atop the live system map (Steward queue, L3 review queue, per-agent Health). Read-seam options rejected. The L1 map keeps its `#sysmap-console` mount reserved. | ★★★ | M | 🚧 Andrew-gated (shelved 2026-06-25; design retained, do not build) |

## PO notes (rotation memory — capped, dated one-liners)

- Cross-lane feeds: lens freshness (F5's slot) ← lattice.md "silent lens-projection stall" (📐); durable
  event history (beyond F6's live tail) ← lattice.md "Loom/Weaver control-API surfacing" (📐).
- 2026-07-01 PO review (Andrew session) — filed the program; found+fixed the control-plane lockout.
- 2026-07-02 UX design adjudicated (2 premises corrected against live stack — see design §15).
- 2026-07-02 PO review (Andrew session) — **extended 2.0** with platform-edges fires F10–F13 (Gateway/Vault/Chronicler onto the curated map + the Time Machine); map stays curated, agent-console stays shelved, design-ahead all three.
- 2026-07-02 — F10–F13 UX **adjudicated** (Winston): [platform-edges-ux](../../implementation-artifacts/loupe-platform-edges-ux.md); Andrew grants `ShredIdentityKey`+`RevokeActor`, map shows design-ahead, revoke = op→event→Gateway-internal-KV (refined lattice revocation row → Designer). Cross-lane asks filed to lattice (Gateway up-full+jwks, Vault→Loupe enablers).
- 2026-07-02 — removed the phase-gates chips from the map (Andrew): the security proofs (bypass g2 / capability g3) become a new Lattice component (human-named, periodic + "check now", isolated runner) — [security-proof-watchdog](../../implementation-artifacts/security-proof-watchdog-brief.md), filed Designer on lattice.
- 2026-07-03 — **Loupe 2.0 core COMPLETE** (F1–F9 all shipped). F9's full value (protected-table rows) needs the read role — filed to lattice ("[Refractor/deploy] Loupe read-only PG role").
- 2026-07-04 — F11 built against the shipped op model (revocation kill-switch Fires 1+2, lattice); review found the materializer poison-pill (invalid actor key → forever-redelivery) — filed to lattice.md.
- **Next:** F12/F13 stay gated on Vault/Chronicler. On the Gateway up-full ship: flip its `designAhead` flag off + verify the F11 revoke loop live (XS).

## Done log — loupe (newest first)

One line per shipped item (`date · SHA · [tag] title`). Oldest roll to `archive/` past ~25.

- 2026-07-04 · `1b19838` · [Loupe/F11] Gateway security console — auth-failure headline + JWKS panel (empty until the heartbeat `jwks` block) + typed-confirm revoke surface over the op model; 3-layer review fixed forward
- 2026-07-03 · `1c77a6c` · [Loupe/F10] Curated topology — Gateway/Vault/Chronicler on the map (design-ahead state, ingress band, lateral Vault, object-store plane); verify + 3-layer review fixes through `6e6d0f4`
- 2026-07-03 · `d5617db` · [Loupe/F9] Postgres read seam — `LOUPE_PG_DSN` connector + `/api/lens/<id>/rows` pg path; also ships the console-wide same-origin gate (rebinding-hardened)
- 2026-07-03 · `f8b09c6` · [Loupe/F7] Submit-Op follow-through — structured accepted panel + ~12s pulse follow-through + session op log + `#/op?type=` prefill; Files/vertex attach polish
- 2026-07-03 · `73a3146` · [Loupe/F8] Packages first-class — `#/package/<key>` graph-resolved contents + install/upgrade/uninstall wrapping pkgmgr (dry-run delta as the confirm, typed uninstall, same-origin gate); keyTarget owns package vertices
- 2026-07-03 · `0821a36` · [Loupe/F6] Live pulse — SSE tail of core-events + map rail feed w/ poll-diff derived rows + edge pulse animation + topbar LED; §8.2 activeSequence premise corrected
- 2026-07-02 · `23a994e` · [Loupe] Phase-gates panel removed from the System Map — gate chips retired ahead of the security-proof-watchdog component (lattice); server computeGates left dormant
- 2026-07-02 · `7f724c5` · [Loupe/F5] Lens page — `#/lens/<id>` four panels + `/api/lens` detail/rows (pg-pending state); typed-confirm delete; map/roster/graph lens links re-pointed
- 2026-07-02 · `24768e8` · [Loupe/F4] Health absorption + status vocabulary — renderedState + pending-readpath rollup exclusion, shell pill+alert strip, map rail gates panel; Health tab retired
- 2026-07-02 · `5865e0e` · [Loupe/F3] Component pages + Control dissolution — `#/component/<id>` plural instances + row-level control + lens roster; Control tab retired
- 2026-07-02 · `976a18f` · [Loupe/F2] Graph explorer — faceted/paged list + linkifying renderer + ego-graph hood mode; Core KV tab retired
- 2026-07-02 · `e6a8a46` · [Loupe/F1] Console shell — hash router + ES-module split + goja logic tier (also closes: static-UI serving test, operator-UI coverage Fire 1)
- 2026-07-02 · `4b8743f` · [Loupe/deploy] Control planes restored for operator surfaces — `lattice.ctrl.>` grant (write-restriction lockout) + natsperm positive round-trip pin
