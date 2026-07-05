# Bootstrap

**Component reference** | Audience: implementers + architects | Contract authority: `docs/contracts/07-primordial-bootstrap.md` (primordial seeding, readiness gate)

---

## Overview

Bootstrap is the **one-shot provisioning binary** that turns an empty NATS/JetStream server into a
running Lattice kernel. It runs once per environment stand-up (invoked by `make up` after NATS and
Postgres containers are healthy), provisions every KV bucket / stream / object store the platform
needs, writes the primordial Core KV entries (Contract #7 §7.2 — the meta-meta DDL, the Capability
Lens anchor, the internal service-actor identities, the operator role + its grants), then exits 0.
It is **not** an always-on component — no process stays resident after a successful run — but it is
the seam every other component depends on existing before they can start.

Bootstrap is the **sole sanctioned non-Processor writer to Core KV** (Contract #7 §7.1): the kernel
must exist before the Processor can enforce anything, so bootstrap writes directly, once, under a
fixed provenance (`BootstrapIdentityKey`/`BootstrapOpKey`, a fixed `BootstrapTime` for deterministic
output) — never a channel any other component reuses.

---

## Two-phase commit + readiness phasing

Bootstrap solves two ordering problems that `make up` alone cannot:

1. **Crash-safe primordial IDs.** `LoadOrGenerate` implements a two-phase commit against
   `lattice.bootstrap.json`: no file → generate fresh NanoIDs, write the file with
   `status="in-progress"`, then seed; file with `status="in-progress"` → crash recovery, reuse the
   same IDs and re-run seeding (idempotent — `SeedPrimordial`'s own guard skips a key that already
   landed); file with `status="committed"` → load IDs, skip seeding entirely. This keeps the NanoID
   set stable across restarts regardless of where a prior run crashed.
2. **The readiness-gate deadlock.** The §7.5 readiness gate blocks until the admin/Loom/Weaver
   `cap.*` projections exist — but those are produced by Refractor, which `make up` starts *after*
   seeding. Bootstrap runs in two invocations to avoid the deadlock: a seed pass with
   `-skip-ready-wait` (provision + seed + mark, no wait) runs first, Refractor starts, then a second
   idempotent pass (no flag, seeding already done) runs the readiness gate. The skip is an explicit
   CLI flag on the seed pass only — never an ambient env var — so an exported variable in an
   operator/CI shell can never leak into the second pass and silently defeat the gate.

---

## What this component owns

| Path | Role |
|------|------|
| `internal/bootstrap/primordial.go` | `Seeder` — `ProvisionBuckets` (KV buckets, the object store, the three JetStream streams) + `SeedPrimordial` (the ~75-entry primordial Core-KV batch, atomic) + the readiness marker (`MarkBootstrapComplete`/`WaitForBootstrapComplete`) |
| `internal/bootstrap/nanoid.go` | The stable primordial NanoID set + `lattice.bootstrap.json` two-phase-commit load/generate/persist |
| `internal/bootstrap/meta_ddl.go` | `MetaRootDDLScript` — the kernel's one DDL (Starlark), governing all `vtx.meta.*` mutations |
| `internal/bootstrap/install_ddl.go` | `InstallPackageDDLScript`/`UninstallPackageDDLScript` — the two DDLs that route Capability-Package install/uninstall through the Processor |
| `internal/bootstrap/lenses.go` | `LensDefinition` — the primordial Capability Lens (+ any other bootstrap-seeded Lens) payload shape |
| `internal/bootstrap/system_actors.go` | `SystemActorKeys`/`PrivacyActorKey` — discovers kernel-seeded service actors from the graph (root-designation topology: `holdsRole → operator`, not `data.protected`) |
| `internal/bootstrap/envelope.go` | `MakeVertexEnvelope`/`MakeAspectEnvelope` — deterministic envelope construction under the fixed bootstrap provenance |
| `internal/bootstrap/verify.go` | `VerifyKernel` — the callable assertion set `scripts/verify-kernel.go` and `lattice bootstrap verify` share |
| `cmd/bootstrap/main.go` | Binary entry point: connects to NATS, runs `ProvisionBuckets` → `SeedPrimordial` → `MarkBootstrapComplete` → the readiness wait |

---

## Kernel composition (what gets seeded)

Per Contract #7 §7.2/§7.7, one atomic batch (`substrate.AtomicBatch` — all-or-nothing) writes, in
order: op tracker → identities → meta DDLs → Lens definitions → roles → permissions → links. Roughly:

- 1 bootstrap op tracker
- 1 primordial admin identity + 3 internal service-actor identities (Loom / Weaver / Bridge —
  arch §92) — later additions (object-store-manager, privacy) follow the same shape
- 1 meta-meta DDL vertex (`canonicalName="root"`) + 9 aspects
- 1 Capability Lens meta-vertex (the primordial-identity anchor) + 5 aspects
- 5 aspect-type meta-vertices × 7 aspects each
- 1 operator role vertex + 2 aspects
- 3 meta-permission vertices (`CreateMetaVertex`/`UpdateMetaVertex`/`TombstoneMetaVertex`, scope=any)
  + their `grantedBy → operator` links
- 1 admin→operator `holdsRole` link + 1 per internal service actor

Everything else — roles like `consumer`/`frontOfHouse`/`backOfHouse`, the identity DDL, RoleMgmt —
lives in packages (`rbac-domain`, `identity-domain`), not here: the kernel stays minimal
(Decision #10), packages carry business shape.

---

## In / Out contracts

| Direction | Contract | Notes |
|-----------|----------|-------|
| Out | KV buckets: `core-kv`, `health-kv`, `capability-kv`, `weaver-state`, `loom-state`, `weaver-targets`, `refractor-adjacency`, `personal-lens-interest`, `token-revocation` | idempotent `CreateOrUpdateKeyValue`; `AllowAtomicPublish` enabled on `core-kv` + `loom-state`'s underlying streams |
| Out | Object store `core-objects` | the off-graph blob plane (Contract #7 §7.2) |
| Out | Streams `core-operations`, `core-events`, `core-schedules` | Processor input, event outbox output, and the `@at`/`@every` scheduling stream (ADR-51) respectively |
| Out | Core KV primordial entries | the ~75-entry batch above, written directly (the one sanctioned non-Processor write, Contract #7 §7.1) |
| Out | `lattice.bootstrap.json` | the local two-phase-commit marker recording the stable NanoID set + committed/in-progress status |
| Out | readiness marker (NATS, `MarkBootstrapComplete`) | polled by `WaitForBootstrapComplete` / downstream readiness consumers |

---

## Key invariants

- **Idempotent by construction.** `ProvisionBuckets` always re-runs safely (`CreateOrUpdate*`);
  `SeedPrimordial` probes the op-tracker key first and skips the whole batch if it already exists.
- **All-or-nothing seeding.** The primordial batch is one `AtomicBatch` — a partial crash can never
  leave a half-seeded kernel visible to the Processor.
- **Deterministic output.** A fixed `BootstrapTime` + the stable NanoID set from
  `lattice.bootstrap.json` make every successful run produce byte-identical primordial envelopes.
- **The explicit-flag readiness skip.** `-skip-ready-wait` is a CLI flag, never an env var — the one
  invariant that keeps the readiness gate from being silently defeated by shell state.

---

## Failure modes

| Mode | Behavior |
|------|----------|
| Crash mid-seed (before `status="committed"`) | next run reuses the same NanoIDs (file says `in-progress`), re-runs `SeedPrimordial`; its idempotency guard skips already-committed keys |
| NATS not yet accepting connections | `connectNATSWithRetry` retries (20 attempts, 1s delay) before failing |
| Readiness gate times out (`cap.*` projections never appear) | seed pass exits 1 with `try make down && make up` — Refractor never came up or never projected |
| **`lattice.bootstrap.json` stale vs. a recreated Core KV** | `make up`'s reuse branch skips re-seeding when kernel processes are already running, even if Core KV itself was recreated underneath — **reads silently return empty while writes still succeed**. Recurred 3× (2026-07-03/04); tracked as an open board item (`[bootstrap] Stale lattice.bootstrap.json …`, `lattice.md`). Repro: `lattice bootstrap verify`. |

---

## Principles that apply

- **P2 exception, by design.** Bootstrap is the sole non-Processor Core-KV writer — a narrow,
  contract-named exception (Contract #7 §7.1) that exists only because the kernel must be seeded
  before the Processor has anything to enforce.
- **Decision #10 / minimal core.** The primordial set is deliberately small (~75 entries); role
  vocabulary, the identity DDL, and RoleMgmt all move to packages.
- **Determinism over cleverness.** Fixed timestamps + stable NanoIDs make the seeded kernel
  reproducible and diffable across environments, which is what makes `VerifyKernel` a meaningful gate.

---

## Implementation status

**Built and CI-gated.** `make verify-kernel` runs `VerifyKernel`'s assertions in CI; `go test
./internal/bootstrap/...` covers the seeder, the meta/install DDLs, and the two-phase-commit file
handling.

**Known gap:** the stale-`lattice.bootstrap.json`-vs-recreated-Core-KV failure mode above has no
freshness check or Health-KV signal today — it is caught only by the operator noticing empty reads
or running `lattice bootstrap verify` by hand.
