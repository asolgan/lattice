# Op-status read surface — a sanctioned way to ask "did my operation land?"

**Status: 📐 awaiting-Andrew** · Designer: Winston (main session, 2026-07-11) · Origin: live incident
(bridge skip-on-redelivery probe broken by the same-day read-tightening) + Andrew's framing: read posture
was meant to stop components reading *business data* from Core KV; checking the status of an op you
submitted is a legitimate generic need everyone has, and today the only way to do it is a Core KV `Get`.

## For Andrew (one-look ratification block)

- **Confirmed incident.** The natsperm-matrix-hygiene Fire 1 read-tightening (2026-07-11, `4258180`,
  executing sensitive-param-egress §8/B2) denies the bridge `$JS.API.DIRECT.GET.KV_core-kv(.>)`. That
  correctly pins the decrypt-RPC-holding bridge away from the Core KV corpus — but it also breaks the
  bridge's month-old **skip-on-redelivery probe** (Story 13.4, `internal/bridge/dispatch.go
  resultAlreadyLanded`): a generic Contract #4 tracker `Get` on `vtx.op.<replyReqID>`, used on redelivery
  to avoid re-calling an external vendor. The probe's error path is NakWithDelay, so every affected
  external event now **redelivers every ~5s forever** (observed: 6+ ops cycling, thousands of
  `Publish Violation` log lines/day, bridge `degraded` in Health KV).
- **Proposal (Fire 1): a Processor-hosted op-status responder** on `lattice.op.status` — the exact
  pattern of the existing `lattice.vault.decrypt` responder. Request `{requestId}`; the Processor (the
  sanctioned Core KV reader) does the tracker `Get` and replies with the Contract #4 verdict
  `{found, committed, isDeleted, committedAt, class}`. The bridge probe switches from `conn.KVGet` to
  this request. Transport: pub-allow `lattice.op.status` for components that submit ops (bridge now;
  the others when they migrate) — a single-subject grant, no KV read surface.
- **Interim mitigation (named, not applied):** `BridgeConfig.SkipOnRedelivery` is an optional
  defense-in-depth mechanism (adapters already dedup on the reused idempotencyKey). Restarting the
  bridge with it disabled stops the redelivery loop and drains the stuck events today, at the cost of
  one redundant (idempotent) adapter call per redelivery. Re-enable when Fire 1 ships.
- **No contract change.** Contract #4 §4.4's dedup semantics are unchanged — this relocates WHERE the
  read runs (Processor-side, behind a subject-scoped RPC), not what it means. The responder's reply is
  a projection of the tracker, not a new state.
- **Decision asked:** ratify the `lattice.op.status` responder (Fire 1) + the bridge migration (same
  fire); approve the interim probe-off mitigation if the loop's noise matters before the fire lands.

## 1. Problem & grounding (verified against code + the live stack this session)

1. **The probe** (`internal/bridge/dispatch.go:159-177`, second call site `schedule.go:147`): on
   external-event redelivery (and on every poll/timeout schedule firing), the bridge `Get`s the generic
   op tracker `vtx.op.<replyReqID>` — deliberately the type-agnostic Contract #4 key, never a typed
   claim vertex — and skips the vendor call if the result op already landed. `SkipOnRedelivery`
   defaults to true (`engine.go:133`).
2. **The deny** (`internal/natsperm/matrix.go`, bridge `ExtraPubDeny`): `$JS.API.DIRECT.GET.KV_core-kv`
   + `.>` + `STREAM.MSG.GET` — sensitive-param-egress §8/B2's read-tightening, landed 2026-07-11 in the
   matrix-hygiene fire. nats.go serves `KVGet` on an AllowDirect bucket via DIRECT.GET, so the probe's
   read dies at the transport (5s client timeout → `context deadline exceeded` → NakWithDelay → loop).
   NATS deny-wins semantics mean the deny cannot be "excepted" for `vtx.op.>` under the same prefix.
3. **Who reads op status today:**
   - the bridge probe (broken — the incident);
   - `lattice op status <requestId>` (cmd/lattice/op/op.go:190 — a raw `KVGet` of the tracker; works
     because only the bridge carries the read-deny today, but it is the same exposure class the
     matrix-hygiene "account-wide read-side laxity" follow-up will eventually tighten);
   - submit-time callers get the reply on their `Lattice-Reply-Inbox` — fine for the synchronous case,
     gone after a process restart, which is exactly why the bridge probes on REDELIVERY.
4. **Read posture** (Andrew, this session): the Core-KV read restriction exists to keep components from
   reading *business data* — Processor reads Core KV; everyone else consumes CDC / core-events / lens
   projections. The op tracker is not business data, but it lives in the same bucket, so any KV-level
   grant that reaches it reaches everything (subject algebra can allow `$KV.core-kv.vtx.op.>` writes,
   but the READ side channel is the JS API DIRECT.GET/MSG.GET surface, which is per-STREAM, not
   per-subject-scoped — a KV-read grant cannot be narrowed to one key prefix).

## 2. The shape (Fire 1)

**`lattice.op.status` — a Processor-hosted request-reply responder**, mirroring the
`lattice.vault.decrypt` responder (`cmd/processor/main.go:265-279`, `internal/processor/
sensitive_decrypt.go`): same host loop, same subject-scoped transport gate, same "the Processor is the
only component that touches Core KV" invariant.

- **Request** `{"requestId": "<NanoID>"}`. Bare-id validated (no dots/wildcards) before key
  construction — the responder never lets a caller shape an arbitrary key.
- **Reply** `{"found": bool, "committed": bool, "isDeleted": bool, "committedAt": "...", "class": "..."}`
  — a projection of the Contract #4 tracker (`vtx.op.<requestId>` ONLY; the responder reads no other
  key shape). `found:false` after TTL expiry is the contracted §4.3 answer, same as today's raw read.
- **AuthZ:** transport-level (natsperm pub-allow `lattice.op.status` on the components that need it —
  bridge in Fire 1). No in-handler identity check, matching the vault RPC's pre-existing posture; the
  reply exposes op METADATA (status/class/timestamps), never payloads, so its blast radius is a
  traffic oracle at worst. If per-actor scoping is ever wanted ("only the submitter may ask"), the
  tracker's `createdBy` is already in the doc — a follow-on, not Fire 1.
- **Bridge migration (same fire):** `resultAlreadyLanded` gains a `statusClient` seam — the NATS
  request against `lattice.op.status` with the existing 5s timeout; `engine.go` wires it; the KVGet
  path is removed (not fallback-kept — a silent fallback would hide a broken grant again). The landed
  test stays byte-identical: `found && !isDeleted`.
- **natsperm:** bridge `ExtraPubAllow` += `lattice.op.status`; processor `AllowResponses` already
  covers the reply leg (as it does for vault.decrypt). One conformance vector: bridge can request
  op-status; a vertical app (no grant) cannot.

## 3. Alternatives considered (and why not)

- **A. Narrow the deny to spare `vtx.op.>`** — impossible: NATS deny-wins; DIRECT.GET denies are
  per-stream (`KV_core-kv`), not per-key, so any carve-out reopens the whole corpus. Enumerating
  denies for every OTHER prefix is an unmaintainable blacklist that silently reopens on every new
  vertex type.
- **B. Op-status lens (Refractor projects `vtx.op.*` to a shared read-model bucket)** — P5-shaped but
  heavy: doubles the write volume of EVERY committed op to serve an occasional probe, needs TTL
  propagation into the target bucket, and creates a second copy of the idempotency surface that can
  lag its source exactly when the probe needs truth. Reads-are-lenses is the rule for *business*
  read models; the tracker is op-machinery, and its one sanctioned reader pattern (dedup) is
  point-lookup-by-known-id — RPC-shaped, not projection-shaped.
- **C. Bridge-local event-derived state** — the bridge already consumes core-events and could track
  landed replyReqIDs itself, but the state dies with the process (the probe exists precisely for the
  restart case), and rebuilding it needs bounded replay machinery per component. Bespoke where B2's
  point was a generic surface.
- **D. Roll back the bridge read-deny** — reopens sensitive-param-egress B2 (a decrypt-RPC-holding
  bridge able to read the whole identity corpus). The deny is doing its job; the probe is the one
  legitimate read it caught in the blast radius.

## 4. Consumers & sequencing

- **Fire 1 (this design):** responder + bridge migration + natsperm vector. Unblocks the live
  degradation; the stuck events drain on their next redelivery when the probe succeeds.
- **Named follow-on consumers (not built in Fire 1):** `lattice op status` CLI (drops its raw KVGet →
  the RPC; removes the CLI's dependency on Core KV read laxity), and the matrix-hygiene "account-wide
  read-side laxity" row — once every op-status consumer is on the RPC, the DIRECT.GET deny can extend
  matrix-wide without breaking anyone (the follow-up that makes this design the standing answer
  rather than a bridge patch).

## 5. Risks & edges

- **Processor unavailability now fails the probe** (it already effectively does: the tracker is
  written by the Processor, and with the Processor down nothing new lands). The probe's existing
  NakWithDelay path handles it; the redelivery resolves when the Processor returns.
- **One more hop on the redelivery path** (~1 RTT + a KVGet, Processor-side). The probe runs only on
  redeliveries and schedule firings, never the hot dispatch path.
- **Traffic oracle:** any grant-holder can ask whether an arbitrary requestId committed. Accepted for
  Fire 1 (metadata-only, matches the vault RPC's caller-trust posture); the per-actor scoping
  follow-on is named above if it ever matters.
