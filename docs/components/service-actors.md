# Internal service actors — bootstrap provisioning

**Component reference** | Audience: implementers + architects

> Implementation page for the Loom + Weaver + Bridge service-actor identities seeded into the
> primordial bootstrap. Grounding of record: `docs/contracts/07-primordial-bootstrap.md`
> §7.1/§7.2/§7.5/§7.7, `docs/contracts/06-capability-kv.md` §6.4/§6.8, and arch §92. Update this page
> in the same commit as `internal/bootstrap` changes; drift is a documentation bug.

---

## What is provisioned

The primordial bootstrap (`internal/bootstrap/primordial.go` → `buildPrimordialEntries`) seeds three
internal service-actor identities in the **same atomic batch** that seeds the admin identity:

| Key | Class | Topology |
|-----|-------|----------|
| `vtx.identity.<loomId>` | `identity.system.loom` | `lnk.identity.<loomId>.holdsRole.role.<operatorId>` |
| `vtx.identity.<weaverId>` | `identity.system.weaver` | `lnk.identity.<weaverId>.holdsRole.role.<operatorId>` |
| `vtx.identity.<bridgeId>` | `identity.system.bridge` | `lnk.identity.<bridgeId>.holdsRole.role.<operatorId>` |

All three are `protected: true` (a package uninstall must never tombstone a kernel service actor),
and their NanoIDs persist to `lattice.bootstrap.json` (bootstrap-file version `9`) so post-restart
code resolves "the loom identity" without a class query.

**Root-equivalent capability is established purely by the `holdsRole → operator` edge** — nothing
else. The operator role already carries the only `scope: "any"` permissions
(CreateMetaVertex / UpdateMetaVertex / TombstoneMetaVertex / InstallPackage / UninstallPackage) via
its `grantedBy` links, and the Capability Lens already walks `holdsRole → operator → grantedBy →
permission` into `platformPermissions[].scope:"any"` for **any** holder. The service actors add
**no new role, permission, grantedBy link, cypher branch, or step-3 code** — they reuse the
admin's exact topology. Their `cap.identity.<id>` docs are produced by the Refractor projecting
that topology, identical to the admin's (Contract #7 §7.1 — no direct `cap.*` seeding).

## Class never gates capability (Contract #7 §7.7)

The admin identity is plain `class: "identity"`; the service actors are `identity.system.loom` /
`identity.system.weaver` / `identity.system.bridge`. This difference is **inert** for capability:

- The full cypher engine's `nodeMatches` resolves the `:identity` label from the **key type
  segment first** (`vtx.identity.<id>`), so `MATCH (identity:identity {key: $actorKey})` binds the
  service actors despite their non-plain class.
- The Refractor actor enumerator and the `cap.*` envelope wrapper anchor on
  `substrate.ParseVertexKey(actorKey)` returning the `identity` type segment — never on the `class`
  field.
- Processor step-3 authorizes on `env.Actor` (a string) → `cap.identity.<id>` with no `class`
  check.

So a `identity.system.loom` identity **with** the `holdsRole` edge projects root-equivalent caps,
and one **without** it projects nothing. Capability is topology, not class. (Proved by
`internal/refractor/ruleengine/full/service_actor_class_test.go` and the auth-parity tests in
`internal/processor/service_actor_auth_parity_test.go`.)

## "Pre-provisioned signing keys" = NATS transport credentials (deferred)

Arch §92 says service actors operate "using pre-provisioned signing keys." In Phase 2 this is **not
graph material**:

- The Processor performs **no signature verification** — the operation envelope
  (`internal/processor/envelope.go`) has no `signature` field, step-3 does no crypto, and there is
  no Gateway. Authentication at the commit-path boundary is *being* `identity:<service>` in the
  `actor` field and *having* a `cap.identity.<id>` projection — identical to a human operator.
- The "signing key" is therefore the **engine process's NATS transport credential** (the
  account / nkey / creds it uses to publish to `ops.system.>`), an arch-explicitly-deferred-to-
  Stream-3 deployment concern (arch lines 285 / 325) — provisioned at deployment time, not as graph
material. The per-component NKey/creds seam this requires is now 🔭 Designed (the ratified NATS account
write-restriction hardening) — `substrate.Connect`'s `NKeySeedFile`/`CredsFile` credential seam shipped
(`75e9acc`), dark by default; the per-component permission matrix + enforcement turn-on is pending.

This story seeds **no key material** on the identity vertices (unused load-bearing-looking crypto
would be a smell). **When envelope-signature verification is ever added, these actors receive key
material at that time** — the "signing keys" requirement is satisfied as transport-level creds, not
dropped.

## `system` lane — deferred (Contract #2 §2.3)

Contract #2 §2.3 reserves the `system` lane for internal service actors, but the live capability
projection hardcodes `lanes: ["default"]` for every actor and `LaneUnauthorized` is unenforced in
the live commit path. All three service actors' projections therefore say `["default"]` today.

**When lane enforcement lands, the service-actor capability projection must include the `system`
lane** (so the engines can submit to `ops.system.>`). This applies equally to Loom, Weaver, and the
Bridge — the Bridge posts its result-ops on the `system` lane, so its capability projection must
carry the `system` lane once enforcement is live. This is out of scope for the bootstrap topology
and is tracked here so it is not lost.

A **lane authorization enforcement** design is now proposed (📐 awaiting Andrew's ratification) — a
step-3 gate checking `env.Lane ∈ doc.lanes` + emitting `LaneUnauthorized`. It is **order-dependent**:
the service-actor `system` grant converges *first* (dark), *then* enforcement turns on — so flipping
the gate cannot break the engines.

## Readiness gate (Contract #7 §7.5)

`make up` blocks until the admin, Loom, Weaver, and Bridge `cap.*` projections all exist, not just
the Health-KV `bootstrap.complete` marker (`WaitForBootstrapComplete` in `internal/bootstrap`). Because
those projections are produced by the Refractor — which `make up` starts *after* seeding — the
bootstrap binary runs in two phases: a seed pass (invoked with the explicit `-skip-ready-wait`
flag, no wait), then Refractor starts, then an idempotent second pass (no flag) runs the readiness
gate. The skip is an explicit per-invocation CLI flag carried only by the seed pass — never an
ambient env var — so an exported variable in an operator/CI shell cannot leak into the wait pass and
silently bypass the gate. The single timeout bounds the whole wait: a missing projection times out
cleanly with a named-key error and never hangs.

## Bootstrap-file version bumps require a full teardown

`lattice.bootstrap.json` carries a `version` field. Any version bump is a hard
mismatch on an older file: `checkVersion` fails fast and the operator must run `make down && make up`. There is no in-place migration — `make down` wipes the
ephemeral NATS/Postgres volumes and removes the JSON, so the next `make up` reseeds the whole
primordial set with fresh NanoIDs. This is intentional for the single-cell MVP; do not expect
or build an upgrade-in-place path.
