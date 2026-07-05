# Lattice Architecture Overview

This diagram shows the full platform as designed — including components that are implemented today and those planned for later phases. See [Project status](../README.md#project-status) for what is built now.

```mermaid
flowchart TB
    subgraph Actors
        Human("Human"); AI("AI Agent"); Admin("Admin / CLI")
    end

    subgraph EdgeLattice["Edge Lattice — Phase 3+"]
        EdgeNode("Sovereign Client Node<br/>local VAL + Starlark")
    end

    subgraph GW["Gateway — Trust Boundary"]
        Proxy["Reverse Proxy<br/>NGINX/Envoy · TLS · rate-limit"]
        Trans["Translator<br/>JWT → Lattice-Actor · revocation"]
    end

    subgraph NATS["NATS Core Plane"]
        Ops[["core-operations (meta · urgent · bulk)"]]
        Evts[["core-events"]]
    end

    Boot["Bootstrap<br/>one-shot: seed kernel + exit"]
    Proc["Processor<br/>sole writer · Starlark · 9-step commit"]
    CoreKV[("Core KV<br/>vertices · aspects · links · DDL")]
    Refr["Refractor<br/>openCypher lenses · CDC · Capability Lens"]
    ObjMgr["object-store-manager<br/>blob byte-janitor"]
    Objs[("Core Objects<br/>off-graph blob store")]

    subgraph OpKV["Operational KV"]
        CapKV[("Capability KV")]; HealthKV[("Health KV")]
        TokKV[("Token Revocation KV")]; WeavKV[("Weaver KV")]
    end

    subgraph Targets["Lens Targets"]
        PG[("Postgres")]; NKV[("NATS KV")]
        PLens[("Personal Lens — Phase 3+")]
    end

    subgraph Orch["Orchestration"]
        Loom["Loom — procedure engine · externalTask"]
        Weaver["Weaver — convergence"]
        Bridge["Bridge — idempotent external I/O"]
    end

    subgraph VaultExt["Vault & Crypto — Phase 3"]
        Vault["Vault — per-identity keys · shredding<br/>(library in Processor + Refractor)"]
        PrivWorker["privacy-worker<br/>async ShredKey finalization"]
        KMS["KMS / HSM"]
    end

    subgraph External["External"]
        IdP["External IdP"]; Svc["Third-Party Services"]
    end

    Human & AI -->|HTTPS| Proxy
    Admin -->|NATS direct| Ops
    Proxy --> Trans
    Trans <-->|revocation| TokKV
    Trans -->|publish op| Ops
    IdP -.->|signing keys| Trans

    Boot -.->|one-shot seed| CoreKV

    Ops --> Proc
    Proc -->|auth check| CapKV
    Proc <-->|reads + writes| CoreKV
    Proc -->|outbox| Evts
    Proc <-->|encrypt/decrypt| Vault

    CoreKV -->|CDC per lens| Refr
    Refr -->|projects| CapKV
    Refr -->|projects| PG
    Refr -->|projects| NKV
    Refr -->|filtered stream| PLens
    Refr -->|decrypt (Secure Lens)| Vault

    Evts --> Loom & Weaver & Bridge
    Loom & Weaver & Bridge -->|submit ops| Ops
    Weaver <-->|convergence state| WeavKV
    Weaver -->|reads targets| NKV
    Loom -->|externalTask| Bridge
    Bridge -->|idempotent call| Svc
    Vault <-->|key material| KMS

    Evts -->|object.tombstoned| ObjMgr
    ObjMgr -->|byte DELETE| Objs
    ObjMgr -->|DetachObject| Ops
    Proc -->|blob CRUD| Objs

    Evts -->|privacy.keyShredded| PrivWorker
    PrivWorker -->|ShredKey| Vault
    PrivWorker -->|submit op| Ops

    Proc & Refr & Loom & Weaver & Bridge & ObjMgr -->|heartbeat| HealthKV
    PLens <-->|sync on reconnect| EdgeNode

    classDef store fill:#dbeafe,stroke:#2563eb,color:#1e3a8a
    classDef engine fill:#fefce8,stroke:#ca8a04,color:#713f12
    classDef gwStyle fill:#f0fdf4,stroke:#16a34a,color:#14532d
    classDef extNode fill:#faf5ff,stroke:#9333ea,color:#581c87
    classDef edgeNode fill:#fff7ed,stroke:#ea580c,color:#7c2d12
    classDef natsQueue fill:#ecfdf5,stroke:#059669,color:#064e3b
    classDef actor fill:#f0f9ff,stroke:#0284c7,color:#0c4a6e

    class CoreKV,CapKV,HealthKV,TokKV,WeavKV,PG,NKV,PLens,Objs store
    class Proc,Refr,Loom,Weaver,Vault,ObjMgr,PrivWorker,Boot engine
    class Proxy,Trans gwStyle
    class IdP,Svc,KMS extNode
    class EdgeNode edgeNode
    class Ops,Evts natsQueue
    class Human,AI,Admin actor
```

## Key data flows

**Write path (left side, top-down):**
Clients submit operations over HTTPS → the Gateway authenticates the actor (JWT), stamps `Lattice-Actor`, and publishes onto `core-operations`. The Processor consumes the operation, checks authorization against Capability KV, hydrates entity state from Core KV, runs the Starlark script, validates the resulting mutations and events against DDL, and commits everything atomically to Core KV. A transactional outbox consumer then publishes business events to `core-events`.

**Read path (right side, CDC-driven):**
The Refractor holds one durable JetStream consumer per active Lens. Each consumer watches Core KV's backing stream, evaluates openCypher rules, and projects results into target stores — Postgres tables for business queries, NATS KV for the Capability cache (auth) and Weaver targets, and Personal Lens streams for edge clients.

**Orchestration (bottom loop):**
Loom, Weaver, and the Bridge consume `core-events`, then submit new operations back through `core-operations` → Processor → Core KV. They never write state directly; the ledger is the only source of truth. External services are reached only by the Bridge: a Loom `externalTask` step dispatches an idempotent call (keyed on the step's instance), and the Bridge executes it, recording the outcome on a claim vertex in the ledger.

**Authorization (always-on, not a separate call):**
The Capability Lens is a Refractor projection that continuously maintains a flattened permission cache in Capability KV. The Processor reads it at O(1) in commit-path step 3. No separate auth service; auth correctness is projection correctness.

**Bootstrap (one-shot, before everything else):**
Bootstrap runs once per environment stand-up, before any other process needs to write: it provisions every KV bucket / stream / object store, then seeds the primordial Core KV entries (the meta-meta DDL, the Capability Lens anchor, the internal service-actor identities) directly — the one sanctioned non-Processor Core-KV write. It then exits; no process stays resident.

**Off-graph blob plane (byte reclaim):**
Large files (lease PDFs, ID scans) live as bytes in the Core Objects store, addressed by `vtx.object.*` vertices the Processor mints. object-store-manager is the dedicated always-on consumer that reclaims those bytes on tombstone, backstops crashed uploads, and cascades an owner's tombstone onto its attached objects — the one off-graph side effect no other engine performs.

**Crypto-shredding (Vault + privacy-worker):**
Sensitive aspects (SSN, DOB) are encrypted under a per-identity key the Processor's commit path manages via Vault (a library, not a separate binary). A right-to-be-forgotten shred is recorded synchronously as intent, then privacy-worker — co-located with the Processor so it shares the same in-memory Vault instance — asynchronously calls `Vault.ShredKey` to make every ciphertext under that key permanently unrecoverable, and records the finalization.

## Phase status

| Component | Phase |
|-----------|-------|
| Bootstrap (one-shot primordial provisioning), Substrate (NATS/KV primitives), Processor, Refractor, Capability Lens | ✅ Phase 1 — implemented |
| Identity & RBAC packages, Hello Lattice vertical slice | ✅ Phase 1 — implemented |
| Package install/uninstall, transactional event outbox, per-lens delete mode | ✅ Phase 1.5 — implemented |
| Loom, Weaver, Bridge (external I/O), object-store-manager (blob byte-janitor), `orchestration-base` + `lease-signing` (Loftspace reference vertical) packages | ✅ Phase 2 — implemented |
| Experience layer — Loupe (operator/inspector, reads Core KV; :7777) + vertical apps LoftSpace (:7788) & Clinic (:7799), which read **lens read-model projections** (P5), never Core KV; all trusted single-identity, loopback, no per-user auth yet | 🏗️ Phase 3 — implemented (building out) |
| Gateway (JWT auth, `Lattice-Actor` stamping, token revocation) | ✅ Phase 3 — implemented (write-path: JWT verify + actor stamping + live JWKS); read-path enforcement in progress |
| Vault (per-identity keys, encrypt-on-write/decrypt-on-read, crypto-shredding), privacy-worker (async `ShredKey` finalization) | ✅ Phase 3 — implemented (local envelope-encryption backend); production KMS backend deferred |
| Edge Lattice, Personal Lens, offline-first sync | 🔭 Phase 3+ — designed |
| Cells & sharding, multi-cell routing | 🔭 Phase 3+ — designed |

## Related reading

- [Component reference pages](./components/README.md) — per-component deep dives
- [Data contracts](./contracts/README.md) — wire shapes, key patterns, behavioral rules
- [Deployment isolation model](./operations/deployment-isolation.md) — per-deployment NATS and Postgres
