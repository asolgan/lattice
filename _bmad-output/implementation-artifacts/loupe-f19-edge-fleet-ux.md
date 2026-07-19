# Loupe F19 — Edge / Personal-Lens fleet (UX)

**Status:** ✅ adjudicated (Winston, Andrew-delegated for the Loupe program) — 2026-07-19.
Board row: [backlog/loupe.md](../planning-artifacts/backlog/loupe.md) → F19.

## 1. The gap

A whole plane was invisible. Personal Lens (PL.1–5) and Edge Lattice (EDGE.1–5) shipped
end-to-end — per-identity subscribe ACL, the `personal.{register,deregister,hydrate,sessionkey,syncgap}`
control RPCs, Interest Sets, the native WS listener, a real edge client in Facet — and the console had
**zero** surface for any of it. An operator asking "who is subscribed, and is anyone falling behind?"
had no answer at any altitude.

## 2. What is observable today — and what is not

Grounded against the shipped code, because the honest scope of this view is set by what the platform
will actually tell an operator.

| Signal | Source | Available? |
|---|---|---|
| Registered devices per identity | `KVListKeys(personal-lens-interest)` → `<identityId>.<deviceId>` | ✅ |
| Per-device Interest Set | `KVGet` → `{types, anchors, registeredAt, revisionCursor}` | ✅ |
| Per-device consumption position | JetStream durable `edge-sync-<identityId>-<deviceId>` on the SYNC stream → ack floor, pending | ✅ |
| SYNC retention window | `JetStream().Stream(…).CachedInfo().State` → FirstSeq/LastSeq | ✅ |
| **Live connection state** | — | ❌ no monitoring port, no `$SYS` user, no component reads either |
| **WS listener stats** | — | ❌ same blocker |
| **Edge-node self-reported state** | — | ❌ *structurally* impossible: an edge node's per-identity permission set admits only its own sync subject + the control RPCs; publishing to Health KV would be a permissions violation, not a missing grant |

**Consequence for the design:** F19 needs no platform primitive and no cross-lane ask. It also cannot
be a liveness view, and must not pretend to be one — see §5.

### Why `personal.syncgap` is not the source

The obvious-looking move — call the platform's own gap RPC — does not work for a console, and the
reason is by design. `personal.syncgap` is identity-bound (a verified actor's id **overwrites** the
body's `identityId`), takes a caller-supplied cursor, and returns a bare `{gapped: bool}` — the design
deliberately extracts the watermark rather than handing it out. An operator cannot ask it about
someone else's device. So the console derives gap from JetStream itself, using the same rule the
platform uses, rather than asking.

## 3. The view

A top-level **Edge** tab (`#/edge`) — its own plane, not a panel bolted onto the Refractor page,
because it spans identities rather than describing one component.

One card per **identity**, devices stacked inside it. **Exception-first and gapped-first**: an
identity with a gapped device sorts to the top and renders red, and a "gapped only" filter collapses
the roster to just the triage set. This view is triage, not a census.

Each device row carries:
1. **The gap chip** — the triage signal. `gapped · N messages aged out` (red) / `within retention
   window` (green) / `gap unknown — <why>` (neutral dim).
2. **The Interest Set**, phrased so an empty filter reads correctly (§4).
3. **The working** — attached/not, ack floor, pending, hydration cursor, registration time. An
   operator who distrusts the chip can see what it was computed from.

A retention line above the roster states the window every verdict is measured against
(`Stream SYNC retains sequences 500–600 … gapped once its cursor falls below 500`), so the verdict is
falsifiable rather than an oracle.

## 4. Adjudicated forks (Winston)

- **Derive the SYNC stream from the installed lens specs; never hardcode `"SYNC"`.** `cmd/refractor`
  takes it from the rule's own `Into.Stream` for a stated reason: a deployment whose personal lens
  targets a differently-named stream would be gap-checked against the wrong one, and a wrong-stream
  FirstSeq can yield a **false all-clear**. The console inherits that reasoning — it reads
  `targetType == "nats_subject"` + `targetConfig.personal` + `targetConfig.stream` off the lens specs
  it already joins. Zero personal lenses ⇒ a note, not a guess. More than one distinct stream ⇒
  ambiguity is reported, not resolved arbitrarily.
- **The gap verdict comes only from the device's own JetStream ack floor.** The obvious second
  cursor — `revisionCursor` from the Interest Set — is **not comparable** to a SYNC sequence, and an
  early draft of this fire got it wrong. `pipeline.Hydrate` returns `Progress().LastAppliedSeq`: the
  Refractor's position in the *Core-KV change stream*, a different sequence space entirely. Live data
  caught it — a healthy device carrying `revisionCursor: 2487` against a SYNC floor of `8355` would
  have rendered "gapped · 5,867 messages aged out". The cursor is still displayed, labelled "last
  hydrated at pipeline seq N" so it cannot be mistaken for a sync position, and it never produces a
  verdict. A device with no durable is *unknown*, not healthy.

- **Gapped means data was actually lost — a deliberate divergence from the platform's predicate.**
  The platform's syncgap responder answers `cursor < firstSeq`, which also fires at the boundary where
  the oldest retained message is exactly the next one the device wants and nothing was lost. That
  conservatism is right where it lives: the cost to a device is one redundant re-hydrate. It is wrong
  as an operator's triage metric, because the SYNC stream is MaxAge-limited — a stack idle past the
  retention window ages to empty and reports `firstSeq = lastSeq + 1`, at which point *every*
  fully-caught-up device satisfies the platform predicate and the whole fleet renders red with nothing
  wrong. The console requires at least one message strictly between the ack floor and the retention
  floor, and `BehindBy` counts exactly those.
- **One fleet-wide stream read, not one RPC per device.** The retention floor is a property of the
  stream, so it is read once and compared against every device — rather than fanning out N control
  RPCs that, per §2, could not answer for another identity anyway.
- **Read-only; no control surface.** `personal.deregister` and `personal.hydrate` are both plausible
  operator actions on a gapped device, and both are deliberately out of scope here. They are
  identity-bound RPCs (§2), so an operator-initiated call would need a distinct server-side path and
  its own authorization story — that is a separate fire with its own review depth, not a button
  grafted onto a view fire.
- **A malformed registration keeps its row.** The key alone names a real registered device, so
  dropping the row would under-count the fleet. It renders flagged instead.

## 5. Honesty conventions

Two rules carry most of the test weight.

**Unknown is never an all-clear.** `gapped` is a nullable tri-state: `null` means the question could
not be answered, and it must never collapse to "not gapped". Two ways it goes null — no readable SYNC
stream, and a device with no durable consumer to read a position from. Each renders as a neutral-dim
chip that *names its own reason*, because a bare "unknown" reads as a bug rather than as a fact about
the platform.

The counting matters as much as the chip. Unmeasured devices are counted separately and never folded
into the healthy remainder: a fleet where the stream is readable but no device has a durable reports
"gap state unknown for all 5", not "0 gapped". The "gapped only" filter carries the same rule — an
empty filtered list states how many rows it hid as undetermined, because a bare "(no gapped devices)"
over a fleet nobody could measure is the exact false all-clear this view exists to avoid. A read fault
mid-page keeps its row (flagged unreadable) and adds a note, rather than silently shortening the
roster behind a confident-looking count.

**An unreadable registration asserts nothing about its filter.** A document that failed to parse tells
us nothing, so it renders "interest set unknown" — never the empty-filter phrasing below, which would
claim the *widest* possible subscription on no evidence.

**This is a registration roster, not a liveness view.** Nothing garbage-collects a registration —
`personal.deregister` is the only removal path — so a device that vanished without deregistering keeps
its row forever, and the roster over-counts over time. The view carries that caveat inline and says
plainly that it shows who is *registered*, not who is *connected*. Given §2's finding that no
connection state is observable to any component, an operator would otherwise reasonably read this
roster as a live fleet, which it is not.

**An empty Interest Set is a wider subscription, not a narrower one.** `personalinterest`'s own rule
is "absence is never a denial": no declared types and no declared anchors admits everything the
identity is authorized for. It renders as "unfiltered — receives everything this identity is
authorized for". Rendering it as "no interests" would invert its meaning.

## 6. Build state

**F19.1 SHIPPED** — `cmd/loupe/edge.go` (`GET /api/edge/fleet`) + `logic/edge.js` + `views/edge.js` +
the `#/edge` tab, with `edge_test.go` (Go, the fleet join) and `web_logic_edge_test.go` (goja, the
render grammar). `lensSpecInfo` gained `Personal`/`Stream` so the spec join can discover the stream.

Not built, and each needs a real driver before it earns a fire:
- **Operator-initiated remediation** (force-hydrate / deregister a gapped device) — needs a
  server-side path around the identity-bound RPCs plus its own authorization story (§4).
- **Live connection state / WS listener stats** — blocked on a platform primitive that does not exist
  (§2); a genuine cross-lane ask if an operator ever needs it, not a Loupe gap.
- **Stale-registration GC** — the roster over-counts by construction (§5). A TTL or a reaper is a
  Refractor-side decision, not a console one.
