# Lattice

**A graph-native, AI-native application platform built on NATS.**

> ⚠️ **Work in progress.** Lattice is an active, in-development project. 
> The sections below describe the platform as designed. 
> See **[Project status](#project-status)** for what's implemented today.

---

## What is Lattice?

Lattice is an experiment in what application infrastructure should look like when the system is
expected to change continuously, explain itself clearly, and safely include AI agents as
first-class actors.

Modern applications often hide their real shape behind service code, private conventions,
framework-specific models, and API glue. Humans can learn those conventions over time. AI agents
usually cannot: they guess at schemas, call APIs without enough context, and work around state
they cannot inspect or reason about directly.

Lattice takes the opposite bet: the application state itself should be the integration surface.
The system should be able to describe what exists, what may be done, who may do it, and what a
valid change looks like without requiring a hand-written SDK for every new actor.

## Why it exists

The original product pressure behind Lattice came from **experience businesses**: places like
residential communities, coworking buildings, campuses, clubs, hospitality groups, and mixed-use
properties where one person's relationship spans leases, payments, access, events, services,
staff interactions, preferences, history, and support.

Those businesses are always inventing new workflows. A new membership bundle, lease rule, access
policy, concierge service, compliance requirement, or staff process should not require weeks of
engineering coordination. But in normal software, every new capability crosses too many seams:
database schema, service code, authorization, API shape, event stream, reporting view, workflow
logic, and UI assumptions.

That is the broken promise Lattice is aimed at: "just spin up a new idea" only works if the
architecture makes the right path easier than the bypass. Lattice tries to make platform
discipline structural. A valid change must go through the same deterministic write path, the same
schema validation, the same authorization model, and the same projection machinery whether it was
initiated by a human, a service, or an AI agent.

The deeper research question is:

> What if application state were structured so humans, services, and AI agents could all reason
> over the same model safely?

The answer Lattice is testing is a living system, not a faster deployment script: capabilities
should be authorable, reviewable, reversible, observable, and evolvable inside the running
platform. Meetings are replaced by intent + review, not intent alone. Human judgment stays in the
loop; coordination overhead is what gets compressed.

## What makes it different

Lattice is built around a few opinionated choices:

- **The graph is the source of truth.** State, relationships, authorization, schemas, and
  operations share one addressable model instead of being scattered across tables, service code,
  policy engines, and integration docs.
- **Every write goes through one deterministic path.** Application behavior is submitted as an
  operation, validated by schema-aware Starlark, authorized, and committed atomically. There is no
  side door for state mutation.
- **Reads are projections, not competing truth.** Queryable views are continuously derived from
  the graph, so Postgres tables, NATS KV views, and authorization caches can be rebuilt from the
  ledgered source.
- **AI discovery is part of the architecture.** The graph is prompt context: operations and types
  carry schemas, descriptions, and examples, so agents can follow links from their identity to
  available commands instead of depending on hardcoded API knowledge.
- **AI authorship has guardrails.** A Lattice-aware agent may propose DDL, Starlark rules, lenses,
  and workflows, but those changes still pass through human review, deterministic validation,
  rollback-friendly contracts, and the same write path as business data.
- **The kernel stays small.** Identity, RBAC, orchestration, and domain behavior arrive as
  capability packages rather than being permanently baked into the core.

In implementation terms, that core is the **VAL** model: entities are **vertices**, their data
lives in **aspects**, and relationships are **links**. The **Processor** is the sole writer to
Core KV; the **Refractor** derives queryable **lenses** from Core KV change-data-capture.

On top of this core, two engines drive *action*: the **Loom** runs deterministic, imperative
procedures ("do A, then B, then C"); the **Weaver** drives declarative convergence ("this target
state must hold — make it so"), nudging external systems and AI agents to close the gap.

The longer-form vision (the Lattice Manifest and System Spec) lives in a separate design vault;
the architecture of record is in [`docs/`](docs/README.md).

---

## Built by AI agents

Lattice is **deliberately developed by AI agents** — as much an experiment in AI-driven software
development as it is a platform. **The agents write everything**: the code, the tests, the
contracts, and the documentation. The work is organized with the
[BMAD method](https://github.com/bmad-code-org/BMAD-METHOD) (a structured agentic workflow with
analyst, architect, scrum master, developer, and reviewer roles) and a session-per-story model
where each story is implemented by a fresh agent against a self-contained brief, then reviewed
by another.

My role (Andrew) is **architect and supervisor**, not implementer: I set the vision and the
binding architectural decisions, freeze the data contracts, pressure-test proposals, review and
adjudicate the agents' output, and steer course — but I don't write the implementation. The
goal is to see how far a rigorously-supervised, contract-first agentic process can carry a
genuinely complex distributed system.

---

### Why NATS

[NATS](https://nats.io) is a lightweight, open-source messaging system — a single small
Go-embeddable binary that, via its JetStream extension, doubles as a durable event log, a
key-value store, and a pub/sub fabric. Lattice needed exactly that combination: one substrate
that could be a ledger, a fast KV layer, and a message bus, without stitching together Kafka +
Redis + a broker and reconciling their consistency models by hand. JetStream gives us all three
in one dependency: ordered streams for `core-operations`/`core-events`, atomic-batch KV for Core
KV (vertices, aspects, links), and lightweight pub/sub for everything in between, all addressable
through the same key-shape and subject conventions ([Substrate](docs/components/substrate.md) is
the thin primitive layer over it).

That unification matters beyond convenience. The Processor's commit pipeline depends on **atomic
batch publish** — a write either lands as a whole (vertices + aspects + links + outbox event) or
not at all — and NATS's raw-protocol atomic batch is what makes that guarantee cheap. The
Refractor's lens projections depend on **durable CDC consumers** reading the same KV change stream
that fed Core KV, so reads can always be rebuilt from the ledger rather than drifting from it.
And because NATS is a single small embeddable binary, it's also what makes **Edge Lattice**
possible later: a sovereign client node can run the same substrate locally, offline, and reconcile
by revision when it reconnects — there's no separate "edge stack" to design.

In short: NATS isn't a queue we bolted on, it *is* the addressable, ledgered, atomically-batched
foundation the rest of the architecture is built to assume.

### Why the VAL model

Most platforms end up with state spread across a relational schema, a document store, an
authorization engine, and a search index — each with its own shape, its own migration story, and
its own blind spots when an agent (human or AI) tries to reason across them. Lattice instead
models *everything* — business entities, their data, their relationships, their types, their
operations, even authorization itself — as one graph: **vertices** (the entities), **aspects**
(their data, addressable independently so they can be versioned, encrypted, or migrated one at a
time), and **links** (typed, directional relationships that double as the authorization graph via
ReBAC).

That collapse buys three things we couldn't get from a conventional schema:

- **One traversal model for everything.** "Can this identity do this operation on this vertex?"
  is the same kind of graph walk as "what leases does this resident have?" — there's no separate
  policy engine with its own mental model to keep in sync with the data.
- **The graph can describe itself to agents.** Because types, operations, and schemas are
  themselves vertices with aspects and links, an AI agent can discover what it's allowed to do by
  *walking from its identity*, instead of depending on a hand-written SDK or out-of-band API docs.
- **Uniform mechanics for change.** Every mutation — whether it's "create a lease" or "install a
  capability package" or "propose a new Starlark rule" — goes through the same deterministic
  write path, the same schema validation, the same authorization check, and produces the same
  kind of CDC event for projections to pick up.

A fourth strength shows up once the platform is already running: **extensibility without
fragmentation**. A new business vertical doesn't have to choose between bolting onto the existing
graph (and risking entanglement with data it doesn't own) or standing up its own silo (and losing
the ability to relate to anything that already exists). Because a vertex's data lives in
independently-addressable aspects, a new capability package can attach its *own* aspects to an
*existing* vertex — a `vtx.identity.<id>` gains a `vtx.identity.<id>.loyaltyTier` aspect owned by
a new loyalty package — and link into the graph from there, immediately benefiting from
everything already known about that entity (its roles, its history, its other relationships)
without copying any of it. What's actually *protected* is the **write path** — every mutation,
including the one that creates that new aspect or link, must pass through the same Processor
pipeline, the same schema validation, and the same permission checks as everything else. So a new
vertical can extend the graph freely, but it can only ever change it through the door everyone
else uses, with the same authorization gate guarding it. The graph grows outward by accretion,
not by replication — and the blast radius of a bad actor or a bad migration stays exactly as
small as the permissions on that one write path.

The cost is real — a graph-of-everything is a less familiar shape than tables-plus-services. That's
precisely the gap the [Refractor](docs/components/refractor.md) is there to close: it continuously
projects the graph into **lenses** — openCypher-derived materialized views that can land in
ordinary Postgres tables (or NATS KV) — so the people and tools that want a familiar relational
shape get one, kept live off the same CDC stream that feeds every other reader, while the graph
underneath stays the single addressable source of truth. You don't have to give up the graph to
get the table; you get both, and the table is just a read-side projection that can be rebuilt from
the ledger at will. That's the bet the whole platform rests on: that a system AI agents can safely
co-author, and that new business ideas can safely extend, needs one addressable model underneath —
not twelve — even if the views on top of it look as familiar as ever.

---

## How it works

Lattice is a small set of cooperating components, each with a living reference page:

| Component | Role |
|-----------|------|
| [Processor](docs/components/processor.md) | The sole authorized writer — a 9-step commit pipeline that runs Starlark over Core KV, with atomic batch commit and a transactional event outbox |
| [Refractor](docs/components/refractor.md) | The read side — continuous openCypher lens projections (Postgres / NATS KV), the security-critical Capability Lens, and CDC consumers |
| [Substrate](docs/components/substrate.md) | NATS / KV / NanoID primitives — key shapes, atomic batch, durable CDC consumers |
| [Capability Packages](docs/components/_packages.md) | Installable bundles (identity, RBAC, domain logic) added through the `InstallPackage` kernel op — the kernel stays minimal |
| [Loom](docs/components/loom.md) | The procedure engine — deterministic, idempotent, linear flows (the "executive") |
| [Weaver](docs/components/weaver.md) | The convergence engine — drives a declared target state, with Two-Phase Nudge for safe external side effects (the "visionary") |

The exact wire shapes, key patterns, and behavioral rules are pinned in the data contracts under
[`docs/contracts/`](docs/contracts/README.md).

### The wider platform

The same primitives extend outward into the rest of the Lattice vision:

- **Gateway** — the trust boundary at the edge: it authenticates actors (JWT), stamps identity
  onto every operation, and enforces read-path authorization so an agent's view of the world is
  bounded by the same ReBAC links as a human's.
- **Vault & crypto-shredding** — sensitive aspects are encrypted with per-identity keys, so the
  "right to be forgotten" is *physical*: destroy the key and that identity's data — even in the
  immutable ledger — becomes permanent, unrecoverable gibberish.
- **Semantic Contracts ("Executable Paper")** — legal prose modeled as atomic **clause vertices**
  linked directly to the state they govern. The Weaver enforces each clause continuously, turning
  a contract into a live billing-and-compliance engine with a perfect chain of custody from the
  signed paragraph to every action it authorized.
- **Edge Lattice & Personal Lenses** — a sovereign client-side node (mobile / web / IoT) running
  the same VAL model and Starlark locally for offline-first, zero-latency, privacy-first
  interaction. The cloud Refractor pushes each device a **Personal Lens** — a security-filtered
  stream of just the sub-graph that identity may see (a filter, not a clone) — and the Edge node
  reconciles by revision when it reconnects.
- **Cells & sharding** — the graph scales by **cells**: a root vertex and its sub-graph are
  co-located in one bucket so writes stay atomic, while a global adjacency index and bridge links
  carry cross-cell traversal, and live data migration runs as a dual-write "shadow" dance with no
  downtime.

---

## Project status

This is the one place that distinguishes what's built from what's designed.

| Phase | Scope | State |
|-------|-------|-------|
| **Phase 1** | Trustworthy core: substrate, Processor write path, Refractor lens projections, identity/RBAC packages, Capability-Lens authorization, the Hello Lattice reference slice | ✅ Implemented + tested (CI-gated) |
| **Phase 1.5** | Hardening: kernel minimization, package installs routed through the Processor, contract conformance suite, transactional event outbox | ✅ Complete |
| **Phase 2** | Orchestration: Loom + Weaver + Two-Phase Nudge + a Loftspace lease-application reference vertical | 🔨 Contracts frozen; implementation starting |
| **Phase 3+** | Gateway (read-path auth, JWT), Vault (crypto-shredding / PII), AI-authored capabilities, Semantic Contracts, Edge Lattice + Personal Lenses, multi-cell sharding | 🔭 Designed, future work |

---

## Documentation

- **[`docs/architecture-overview.md`](docs/architecture-overview.md)** — full platform architecture diagram (as designed, all phases)
- **[`docs/`](docs/README.md)** — the documentation map (contracts, components, observability, operations)
- **[`docs/contracts/`](docs/contracts/README.md)** — the data contracts (source of truth for wire shapes)
- **[`docs/components/`](docs/components/README.md)** — living per-component reference pages
- **[`docs/hello-lattice.md`](docs/hello-lattice.md)** — the 60-minute end-to-end tutorial

---

## Quick start

```console
# Start NATS + Postgres, bootstrap primordial state, start the Refractor
make up

# Confirm everything is healthy
lattice health gates
```

Expected output:

```console
health.gates.phase1.gate1  passed: true
health.gates.phase1.gate2  passed: true
health.gates.phase1.gate3  passed: true
health.gates.phase1.gate4  passed: true
```

`make up` seeds only the primordial kernel. Identity and RBAC ship as **Capability Packages**,
so install them before using identity/role operations:

```console
lattice-pkg install packages/identity-domain
lattice-pkg install packages/rbac-domain
```

Then walk the full vertical slice — define a type, create entities, project to Postgres, drive
it with an AI agent, and roll a schema change back — in the
**[Hello Lattice tutorial](docs/hello-lattice.md)**.

### Prerequisites

- Go 1.26+
- Docker + Docker Compose
- `make`

---

## Development

```console
# Build all binaries
make build

# Run all unit + integration tests (serialised for embedded NATS stability)
make test

# Lint
golangci-lint run ./...

# Go vet (ANTLR-generated files excluded)
make vet

# Gate tests
make verify-kernel
make test-bypass
make test-capability-adversarial
make test-rollback
```
