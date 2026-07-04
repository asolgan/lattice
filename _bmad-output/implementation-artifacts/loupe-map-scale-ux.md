# Loupe UX — System Map at scale: lens clusters + the door band (F14)

**Status: adjudicated 2026-07-03 (Winston, Andrew-delegated per the loupe.md lane header) — build-ready
for the Loupe Steward.** PO+Sally session with Andrew 2026-07-03, prompted by the live map at ~24 lenses.
Extends `loupe-2-ux-design.md` (the map-is-the-console program) and `loupe-platform-edges-ux.md` §1 (the
ingress band); changes nothing outside the System Map view.

---

## Context — what breaks at dozens of lenses

The lens shelf renders every live lens as a flat alphabetical flex-wrap chip, with one
`refractor → lens` edge per lens, each edge carrying its own `project` label
(`systemmap.go` `computeSystemMap`, `map.js` `renderSystemMap`). At ~24 lenses this already shows four
failure modes, all of which compound linearly with lens count:

1. **Label spam** — a stack of identical `project` labels bleeding over the chips (every edge is
   labelled; the right-gutter submit-ops bus already solved this class of problem with label-once).
2. **Package interleaving + truncation** — alphabetical order puts `clinicAppo…` beside
   `capabilityRol…`, and CSS truncation erases the discriminating suffix (three chips render as the
   same `capability…`).
3. **Invisible inventory** — the shelf scrolls internally, so below-fold lenses simply vanish from a
   health surface, while their edges point into clipped overflow (the object-store column was already
   moved out of the shelf to dodge exactly this).
4. **Edge fan** — dozens of near-parallel edges from one Refractor corner carry no per-edge
   information (the pulse animation never rides them; rule-update rows ride the poll-diff feed).

The map absorbed the Health tab (F4), so the shelf must keep doing its console job: a sick lens is
visible **on the stage**, at a glance, without opening a roster.

## §1 — Package-grouped lens clusters, exception-first

**Grouping key.** The graph already records lens ownership: each installed package vertex
(`vtx.package.<id>`) carries a `.manifest` aspect whose `declaredKeys` include the lens meta-vertices
(`vtx.meta.<lensID>` — the same resolution `computePackage` does for the F8 package page). The
systemmap assembler builds the reverse index once per poll (O(#packages) reads; Loupe is the P5
inspector exception) and stamps each lens node with `pkg: <package canonicalName>`. A lens claimed by
no manifest — the bootstrap kernel-seed family (`capability`, `capabilityRead`, …,
`internal/bootstrap/lenses.go`) — falls to the curated group **`kernel`**.

**Cluster cards.** The shelf becomes one bordered card per group, sorted by group name, cards wrapping
in a grid:

- **Header** (one line): worst-of status dot · group name · `· N` lens count · `◆M` protected count
  (spec-side truth stays visible while collapsed). The header links to the owning package page
  (`#/package/vtx.package.<id>`); the `kernel` header links to the Refractor lens roster instead.
- **Body — exception-first density.** In the default `overview` density only lenses whose
  renderedState ≠ `projecting` render as chips (full chip anatomy unchanged: dot + glyph + full label +
  `lag N` / `◆` tags — `pending-readpath` keeps its accent family and tooltip copy; it is surfaced, not
  alarmed). Healthy lenses collapse into one muted `+N projecting` expander chip; clicking it (or the
  header twisty) expands the card to all chips. A shelf-level `overview | all` toggle sets every card
  at once. Expanded full labels get room precisely because healthy chips are collapsed — the
  truncation problem dissolves instead of being styled around.
- **Filter.** A `filter lenses…` text input above the shelf narrows chips live across all groups
  (substring on label + id), auto-expanding matching cards — the dozens-scale navigation path; empty
  filter restores the density rule.

**Edges.** Per-lens edges and their labels are retired. Each cluster card registers in `nodeEls` under
a synthetic id and gets **one** `refractor → <group>` edge, labelled `project` **once** across the
whole set (the `returnLabelled` label-once precedent). Nothing dynamic is lost — `pulseFlow` animates
the core-operations → processor → core-events fan, never lens edges, and per-lens state transitions
keep riding the poll-diff derived feed (nodes stay per-lens in the API).

**Scale math.** 80 lenses across ~10 packages ⇒ ~10 compact cards; with zero exceptions the shelf is
ten header lines. Height now grows with *problem count*, not lens count; the inner scrollbar and
clipped-overflow edges disappear structurally. The banner summary (`sysmapSummary`) is untouched — it
counts nodes, and nodes remain one-per-lens.

**Rejected: a single `Lenses ×N` meganode.** Maximal compression, but it hides *which* lens is sick
behind a click — undoing F4's absorption of Health into the map. Exception chips must stay first-class
on the stage; grouping gives locality (a sick clinic lens surfaces inside `clinic-domain`, one pixel
from its package drill-in).

## §2 — The verticals join the door band (beside the Gateway, not behind it)

**Architecture grounding.** The vertical apps *are* ingress: they terminate real users (JWT-verified
RLS reads, D1.5 `authenticateRead`) and publish operations to `core-operations` on behalf of those
users (`cmd/clinic-app/op.go` — today stamping the bootstrap admin actor; the filed stale-bootstrap
wart). They are **not** routed through the Gateway, and per the design of record they never will be:
`docs/components/gateway.md` — *"every sanctioned actor (the engines, the vertical apps, the CLI,
Loupe) submits ops; the Gateway is the external door, not a re-route for internal traffic."* The
Gateway is the external **API** door (Bearer JWT); the verticals are **product** doors and sanctioned
direct submitters. An `app → gateway` edge would assert a route that neither exists nor is designed.

**Placement.** A curated `declaredApps` list (`clinic-app`, `loftspace-app`) renders a new node kind
**`app`** in the ingress band — the map stays curated (Andrew's standing ruling); adding a vertical is
a one-line edit, the F10 `declaredComponents` precedent. The band splits into two lines: the
`external actors · Bearer JWT` marker centered on top, the doors row under it —
`clinic-app · Gateway · loftspace-app`. Curated edges:

- `external → clinic-app`, `external → loftspace-app` (unlabelled, like `external → gateway`);
- `clinic-app → core-operations` and `loftspace-app → core-operations`, labelled
  **`submit ops · on behalf of users`** once across the pair (label-once).

**Status semantics.** Declared apps overlay heartbeats exactly like components (both groups already
heartbeat). A declared app with **no** heartbeat renders **`offline`** — dim dot + `offline` tag,
zero rollup contribution — never absent-red: verticals are optional workloads and kernel-only
`make up` must stay green. A heartbeating-but-sick app degrades the rollup normally. Hover tip carries
the curated pointer copy: *"product front-end — verifies user JWTs for reads (RLS); submits ops on
behalf of its users directly to core-operations (sanctioned direct submitter — the Gateway is the
external API door, not a re-route)."* Click drills to `#/component/<id>` as today.

**The discovery net stays.** The bottom `clients` shelf remains for **undeclared** heartbeat groups —
an unknown reporter still surfaces honestly; promotion into the door band is a curation decision, not
automatic.

**Stretch (build only if it doesn't tangle).** One dashed left-gutter **`read models (P5)`** bus from
the lens band up to the doors row — the read-back loop that closes the P5 story, mirroring the
right-gutter submit-ops bus. Defer freely; the tooltip copy already tells the read story.

## §3 — Build notes (one fire, F14, size M)

- **Server** (`systemmap.go` + `systemmap_test.go`): lens nodes gain `pkg` (manifest reverse index,
  `kernel` fallback); per-lens `refractor → lens` edges dropped; `declaredApps` + kind `app` +
  `offline` status + the door-band edges; `handleSystemMap` wiring passes the package resolver.
- **Logic tier** (`logic/status.js` + goja tests): `sysmapTier` places `app` at the doors line;
  `componentStatusClass` gains `offline` (dim family); `sysmapSummary` ignores `offline`; new pure
  helper `groupLenses(nodes)` returns the cluster model (group → {worst, count, protected, chips}) so
  density/rollup rules are goja-tested without DOM.
- **View** (`views/map.js` + `style.css`): cluster cards, expander, filter input, two-line ingress
  band, cluster-edge registration; the lens-shelf scrollbar CSS goes away.
- **Unchanged**: `/api/lenses` roster, lens pages, component pages, pulse feed mechanics, banner
  rollup vocabulary.
- **Gates**: the standard six (`go build`, `make vet`, `golangci-lint`, `STRICT=1 lint-conventions`,
  `go test ./cmd/loupe/...`, `make verify-kernel` untouched-but-cheap) + the fe-engineer in-browser
  pass on the running stack (label-once rendering, filter, expand, offline app chip with the verticals
  stopped).
