# Vault + crypto-shredding — design

**Status: 📐 awaiting-Andrew (ratification).** Author: Winston (Designer fire, 2026-06-27).
Backlog row: `planning-artifacts/backlog/lattice.md` → *Privacy / Vault → Vault + crypto-shredding*
(★★★, L). Grounds in `lattice-architecture.md` Items 5 & 6 (the pre-written PII rubric) + the Vault
SPOF / KMS decisions, Contracts #1/#2/#3/#5/#7 (the sensitivity hooks already wired), the Obsidian
*Brainstorm PII and Crypto-Shredding* subdoc, brainstorming inventory items #56–#62 / #120, and the
read-path-authorization (D1) design (the dependency for Phase B).

---

## For Andrew (one-look ratification block)

**What it does, in two lines.** Makes "right to be forgotten" real on an append-only ledger: aspects
marked `sensitive: true` are stored as **ciphertext** in Core KV (encrypted on write with a per-identity
key held outside Core KV), and a `ShredIdentityKey` operation **destroys the key** so the ciphertext —
in live KV *and* in the immutable JetStream history — becomes permanent gibberish, instantly removing the
PII from every downstream projection. The **sensitivity boundary already ships** (the `sensitive` DDL
flag, identity-anchoring at commit step 6, `SensitivityViolation`, the reserved Vault health metrics +
the privacy-critical failure tier); this design adds the **crypto layer** those hooks were built for.

**The safety insight that makes this shippable now (no D1 / no Edge):** decryption is **opt-in**. The
Refractor's default projection path copies the *ciphertext* it reads from Core KV — so sensitive aspects
are unreadable at every general lens target **by construction**, no read-path auth required. Plaintext is
produced in exactly two places: the **Processor** (decrypt-on-read into the Starlark context, so business
logic still works) and **trusted tools** (Loupe, via a Vault decrypt RPC — the same trusted-single-identity
model Loupe already runs under). The queryable-plaintext "Secure Lens" (the architecture's *blind
projection* rule) is the one piece that needs read-path auth — so it is cleanly deferred as **Phase B**
behind D1, and **Phase A delivers the full crypto-shred guarantee on its own.**

**Two forks I designed through — your call on both:**

1. **Vault backend (the KMS choice the architecture flagged for "week 1").** Key material must never live
   in Core KV (`lattice-architecture.md` line 988). Options:
   - **Path A — pluggable `Vault` interface + a local envelope-encryption backend first (my
     recommendation).** Define `internal/vault.Vault` (`CreateIdentityKey / Encrypt / Decrypt / ShredKey`)
     and ship a **local backend** using **envelope encryption** — a per-identity DEK wrapped by a single
     master KEK, the KEK sealed in config (env / file) for the dev + trusted-tool deployment, exactly the
     posture Loupe already runs under. Production **KMS adapters** (HashiCorp Vault Transit, AWS/GCP KMS)
     are pluggable backends added later — mirrors Refractor's adapter pattern (NATS-KV / Postgres). This
     unblocks the whole feature **without** committing to a cloud KMS now and keeps the crypto-shred
     guarantee real (shred = destroy the wrapped DEK + evict caches; for a real KMS, also destroy the KMS
     key version).
   - **Path B — commit to an external KMS/HSM now (HashiCorp Transit or AWS KMS).** Strongest custody
     immediately, but pulls a heavy integration dependency + ops surface into Phase 3 and contradicts the
     "trusted-tool, binds to one identity, runs on 127.0.0.1" framing the rest of Phase 3 lives in.
   - **My recommendation: Path A.** Same interface either way; the backend is swappable; the *fork* is only
     *which production KMS and when*, not the architecture.

2. **Phasing vs. D1 — Phase A now, Phase B behind D1 (my recommendation).** Phase A (crypto layer +
   shred, ciphertext-safe everywhere) is independently valuable and **unblocked today**. Phase B (the
   Refractor **Secure Lens** that decrypts sensitive aspects into RLS-protected, *queryable* read models —
   the architecture's decision-of-record) needs read-path authorization to protect those plaintext rows,
   and D1 is itself only 📐 awaiting-Andrew. I recommend **ratifying + building Phase A now** and gating
   Phase B on D1's ratification — they compose, and Phase B's input (an authz-anchored protected lens) is
   exactly what D1 produces. The alternative is to hold all of crypto-shred until D1 lands; I think that
   needlessly delays the right-to-erasure guarantee, which stands alone.

**Frozen-contract change (uncommitted, staged as the proposal-diff).** One genuine change:
`docs/contracts/03-mutation-batch-event-list.md` gets a new **§3.10 — Sensitive-aspect encryption at
rest**. Today the contract is silent on storage format, so a `sensitive: true` aspect would land in Core
KV as **plaintext** (only its *anchoring* is enforced). §3.10 makes the observable invariant explicit:
the `data` of a sensitive aspect is stored **ciphertext** (encrypt-on-write after step-6 validation,
before the step-8 atomic commit), with the encryption envelope referenced by the anchoring identity's
`piiKey` aspect. Edited in `main`, **left uncommitted** for your ratification (the diff *is* the
proposal). Affected consumers: every direct Core-KV reader of sensitive aspects (Refractor — projects
ciphertext as-is; Loupe — decrypts via Vault RPC; the platform binaries). Everything else is **build-to**:
Contracts #1 §(sensitivity lookup), #2 (`SensitivityViolation`), #5 §5.4/§5.5 (`vault_calls_total`,
`keyshredded_handled_total`, `VaultUnreachable`), #7 (`sensitive` reserved aspect-type DDL) are **already
written** for this feature — no change.

**Review.** Self-adversarial pass run (Vault-in-commit-path SPOF, encrypt/OCC/idempotency interaction,
shred atomicity, cache coherency after shred, lens-target plaintext leakage) — findings folded into §7
(risks) and the resolutions below. A full `bmad-party-mode` pass is warranted at build time on the Fire-2
commit-path wiring (the security-plane change).

---

## 1. Problem & intent

**The gap (NFR-Privacy / GDPR right-to-erasure).** Lattice's ledger is **immutable** — Core KV is backed
by JetStream history; "deleting" a KV key (tombstone) leaves every prior value in the stream forever. So
a literal delete **cannot** satisfy right-to-be-forgotten for PII (SSN, DOB, …). The only sound mechanism
on an append-only substrate is **crypto-shredding**: store the PII encrypted, and "forget" by destroying
the key — the ciphertext (live + historical) becomes unrecoverable. The Obsidian *Brainstorm PII and
Crypto-Shredding* subdoc states it directly: *"Crypto-shredding is the only way to achieve true Right to
be Forgotten in a system backed by an immutable ledger."*

**What already exists (verified in code — this is a crypto-layer addition, not greenfield).** The
*sensitivity boundary* is built and shipping:

- **DDL flag** — `pkgmgr` emits a `.sensitive` aspect on an aspect-type DDL when `DDLSpec.Sensitive`
  (`internal/pkgmgr/build.go:127`, `definition.go:270`); the DDL cache reads `<root>.sensitive`
  (opt-in, absent ⇒ non-sensitive). Contract #7 reserves `sensitive` as a meta-layer aspect type.
- **Identity-anchoring** — Processor commit **step 6** rejects a `sensitive` aspect on a non-identity
  vertex with `SensitivityViolation` / `sensitiveAspectScope` (`internal/processor/step6_validate.go:125`).
  Contract #2 documents the `SensitivityViolation` error; Contracts #1 §+ #3 §3.8 document the
  "apply sensitivity constraints" commit-path rule.
- **Health hooks** — Contract #5 §5.4/§5.5 already define `vault_calls_total`, `keyshredded_handled_total`,
  and the `VaultUnreachable` issue (today "Phase 1 stub may report 0").
- **Failure tier** — `docs/components/refractor-failure-tiers.md` reserves the **privacy-critical
  crypto-shred tier** (a shredded-but-still-decrypting row ⇒ halt, no retry, page on-call) as
  "designed-but-not-built, dependency: Vault/Phase 3."

**What is missing (this design).** The cryptography itself: a **Vault** (per-identity key custody),
**encrypt-on-write / decrypt-on-read** in the Processor, the **`ShredIdentityKey` operation +
`KeyShredded` event**, and the **Refractor `KeyShredded` nullification handler** + the privacy-critical
tier. `lattice-architecture.md` Items 5 & 6 are the rubric; this design resolves the open mechanics.

**Intent.** Deliver true right-to-erasure for PII **now**, on the existing single-cell trusted-tool
platform, without waiting on the Edge node or read-path auth — and structured so the queryable Secure
Lens drops in cleanly once D1 lands.

## 2. The shape

### 2.1 Data model (Contract #1 key-shapes)

- **`vtx.identity.<id>.piiKey`** — a non-sensitive aspect on the identity vertex holding the **encryption
  envelope reference**, not key material: `{ wrappedDEK, keyId, kekVersion, alg: "AES-256-GCM",
  createdAt, shredded: false }`. Created **lazily** — on the first sensitive-aspect write for an identity
  with no `piiKey`, the Processor calls `Vault.CreateIdentityKey`, receives the wrapped DEK, and writes
  `piiKey` in the **same atomic batch** (non-PII identities never get one). "Key material never in Core
  KV" holds: only the *wrapped* DEK (ciphertext, openable solely by the master KEK / KMS) lands here.
- **Sensitive aspect** (e.g. `vtx.identity.<id>.ssn`, declared `sensitive: true` in its DDL) — stored
  with `data` = **ciphertext** (`{ ct, nonce, keyId }`), AES-256-GCM under the identity's DEK. Anchoring
  to an identity vertex is already enforced (step 6), so the key is always resolvable: it is the host
  identity's `piiKey`.
- **No new vertex types.** Reuses the existing identity vertex + aspect model; `piiKey` is just another
  aspect. Aligns with architecture D5 (minimum in vertex root; business/sensitive data in aspects).

**Granularity — aspect-level, resolved.** The architecture (Item 6) and the brainstorm subdoc conflict:
the subdoc proposes *field-level* `encrypted: true` per JSON property; Item 6 (the later, considered
decision-of-record) chose *aspect-level* `sensitive: true`. **I resolve to aspect-level** — the aspect is
the atomic unit of encryption *and* of shredding; field-level partial-JSON encryption makes crypto-shred
non-atomic and complicates every read/write path. "Some fields sensitive, others not" ⇒ split into
separate aspects. (Field-level is recorded as a considered-and-rejected alternative, §8.)

### 2.2 Write path (P2 — operations only; the Processor is the sole Core-KV writer)

Encryption is a **Processor commit-path middleware**, not script logic (so Starlark stays pure — it
returns plaintext; the engine guarantees ciphertext-at-rest, per the brainstorm's "Security is
Guaranteed by the DDL"):

```
step 4 (hydrate)   → decrypt-on-read: for each sensitive aspect pulled into the Starlark context,
                     Vault.Decrypt(ct, DEK) → plaintext. Starlark sees strings/numbers, never AES.
step 5 (execute)   → Starlark runs on plaintext, returns a MutationBatch with plaintext sensitive data.
step 6 (validate)  → unchanged: schema + permittedCommands + sensitiveAspectScope validated on PLAINTEXT.
step 6.5 (NEW —    → encrypt-on-write: for each mutation whose DDL is sensitive, lazily ensure piiKey
  encrypt)           (CreateIdentityKey + add the piiKey mutation to the batch if absent), then replace
                     mutation.data with Vault.Encrypt(plaintext, DEK) = { ct, nonce, keyId }.
step 8 (commit)    → atomic batch lands CIPHERTEXT (+ piiKey) in Core KV. Plaintext never touches KV.
```

Validation **before** encryption is deliberate: schema is validated against the plaintext shape; the
stored bytes are opaque ciphertext. Encryption is non-deterministic (random GCM nonce) — harmless under
last-writer-wins-by-revision, and idempotency keys on `requestId` (step 2 dedup) not on content, so a
resubmit returns the prior commit without re-encrypting.

**`ShredIdentityKey` operation.** A system/kernel op (lane `ops.urgent.>` — Contract #2 names urgent for
"emergency revocations") whose Starlark marks `piiKey.shredded = true` (an aspect update, P2) and emits a
**`KeyShredded` event** (`class: "privacy.keyShredded"`, payload `{ identityKey }`). The op records
*intent* in Core KV; the irreversible KMS destruction + projection nullification happen in the async
listeners (§2.4) so the commit path never blocks on an external KMS round-trip.

### 2.3 Read path (P5 — apps read lens projections; ciphertext-safe by construction)

- **Refractor (default lenses):** projects sensitive-aspect `data` **as-is = ciphertext**. No decrypt, no
  Vault call on the hot projection path. General lens targets (the NATS-KV read models, Postgres) hold
  **unreadable ciphertext** — so a sensitive aspect is safe at every shared read surface **without**
  read-path auth. This is the property that lets Phase A ship before D1.
- **Trusted tools (Loupe):** to display PII to the trusted operator, Loupe calls a **Vault decrypt RPC**
  (`lattice.vault.decrypt`, micro.Service responder) — acceptable under the trusted-single-identity model
  (no per-user read-path auth in Phase 3; same posture as Loupe reading full Core KV today). Optional;
  not required for correctness.
- **Secure Lens (Phase B, D1-gated):** the Refractor Secure Lens adapter decrypts sensitive aspects into
  **RLS-protected, queryable** read models (the architecture's *blind projection* rule). This is the only
  consumer that produces queryable plaintext, and it is exactly the surface D1's read-path auth protects —
  hence deferred behind D1.

### 2.4 Orchestration — shred finalization (mirrors the Weaver convergence-lens / `freshnessExpiry` precedent)

Shred is a multi-step, must-not-silently-fail flow. After the `ShredIdentityKey` commit + `KeyShredded`
event:

1. **Vault key destruction** — a **privacy worker** (a thin listener; co-located with Refractor's CDC
   path or a standalone `cmd/privacy-worker` — see §3) calls `Vault.ShredKey(identityKey)` to destroy the
   wrapped DEK (and, for a real KMS backend, the KMS key version). After this, `Vault.Decrypt` for that
   identity **fails permanently** — the ciphertext in live KV *and* JetStream history is gibberish.
2. **Cache eviction** — the **Processor** subscribes to `KeyShredded` and evicts that identity's DEK from
   its in-memory cache (architecture Item 5: "Cache invalidation via `KeyShredded` event"), so a cached
   DEK can't outlive the shred.
3. **Projection nullification** — the **Refractor `KeyShredded` listener** nullifies/removes projected
   rows derived from the shredded identity's sensitive aspects (belt-and-suspenders in Phase A, where rows
   already hold now-garbage ciphertext; **load-bearing** in Phase B, where Secure-Lens rows hold
   plaintext). On nullification failure the **privacy-critical tier** fires — halt the lens, no automatic
   retry, page on-call (the reserved tier in `refractor-failure-tiers.md`).
4. **Convergence guarantee (orphaned-PII reconcile, brainstorm #5/#62).** A **Weaver convergence marker**
   tracks "shred not yet finalized" — it stays *violating* until the Vault key is destroyed **and**
   projections are nullified, and re-drives the steps until convergence (mirrors the
   `orchestration-base` freshness/`MarkExpired` convergence-lens pattern). This catches the
   crash-after-commit-before-destroy window: the marker survives a restart and finalizes the shred.

This keeps the irreversible work **out of the synchronous commit path** (availability) while making it
**guaranteed-eventual** (Weaver convergence) and **loud on failure** (privacy-critical tier) — the right
posture for a confidentiality operation.

### 2.5 Vault backend (envelope encryption — the recommended Path A)

```
internal/vault (the interface + the local backend)
  type Vault interface {
    CreateIdentityKey(ctx, identityKey) (Envelope, error)  // new wrapped DEK
    Encrypt(ctx, identityKey, plaintext) (Ciphertext, error)
    Decrypt(ctx, identityKey, Ciphertext) (plaintext, error)
    ShredKey(ctx, identityKey) error                       // destroy DEK; irreversible
  }
```

- **Envelope encryption.** Per-identity **DEK** (random 256-bit) encrypts the aspects; the DEK is stored
  **wrapped** by a single **master KEK** in `piiKey.wrappedDEK`. To encrypt/decrypt, the backend unwraps
  the DEK once and caches the **plaintext DEK** in memory (short TTL, per-identity) — so steady-state
  encrypt/decrypt make **zero** external calls; only `CreateIdentityKey` / `ShredKey` touch the KEK
  custody. This collapses the "Vault SPOF in the write path" risk (architecture line 88 / brainstorm
  #120) to "KEK reachable at key-create / cold-cache time."
- **Local backend (dev + trusted-tool deployment):** the master KEK is sealed in config (env var / file,
  `make`-provisioned), matching Loupe's 127.0.0.1 trusted-tool posture. Shred = delete the wrapped DEK +
  evict caches (the wrapped DEK is the only opener; destroying it shreds).
- **Production KMS adapters (later, Andrew's backend choice):** HashiCorp Vault *Transit* or AWS/GCP KMS
  implement the same interface; the DEK is wrapped/unwrapped by the KMS, `ShredKey` destroys the KMS key
  version. Pluggable like Refractor's target adapters — no change above the interface.

## 3. Component & package layout

- **`internal/vault`** (new) — the `Vault` interface + envelope-encryption local backend + the
  `lattice.vault.decrypt` micro.Service responder (for trusted-tool reads). Substrate-only (no raw NATS
  outside the `micro.Service` responder, per the accepted exception).
- **`internal/processor`** — step-4 decrypt hook, step-6.5 encrypt hook, lazy `piiKey` creation, the DEK
  cache + `KeyShredded` eviction subscription. The Vault is injected (interface), so the Processor stays
  testable with a fake.
- **Privacy worker** — the `KeyShredded` → `Vault.ShredKey` + nullification listener. **Decision:** ship
  it **inside Refractor's CDC runtime** first (Refractor already owns the row-nullification handler per
  the architecture's Stream-2 ownership and already consumes Core-KV CDC), not a separate binary — fewer
  moving parts; a standalone `cmd/privacy-worker` is a later extraction if the privacy plane grows.
- **`packages/privacy-base`** (new package, P5/decision-#10 "everything-is-a-package") — ships the DDL:
  the `piiKey` aspect-type DDL, the `ShredIdentityKey` operation DDL + its Starlark, the
  `privacy.keyShredded` event-type DDL, the Weaver shred-finalization convergence lens, and permissions.
  A reference sensitive aspect (e.g. `ssn`) lives in a test/demo package, not here.
- **No app reads Core KV** for PII (P5): apps that need PII go through the Secure Lens (Phase B) or, in
  Phase A, simply don't surface it (ciphertext). Loupe (the inspector exception) uses the Vault RPC.

## 4. Contract surface

| Contract | § | Change vs build-to |
|---|---|---|
| #3 MutationBatch | **§3.10 — Sensitive-aspect encryption at rest (NEW)** | **CHANGE** — staged uncommitted. Makes ciphertext-at-rest the observable invariant + names the commit-path placement (validate plaintext → encrypt → commit). |
| #1 Addressing | §(DDL lookup / sensitivity constraints) | build-to (already written) |
| #2 Operation envelope | `SensitivityViolation` (§errors); `ShredIdentityKey` as a normal op | build-to (error already documented; the op is package DDL) |
| #5 Health KV | §5.4 `vault_calls_total` / `keyshredded_handled_total`; §5.5 `VaultUnreachable` | build-to (already written; wire the real counters) |
| #7 Primordial bootstrap | `sensitive` reserved aspect-type DDL | build-to (already reserved) |
| #10 Orchestration | convergence-lens / marker pattern for shred finalization | build-to (reuse `orchestration-base` precedent) |

The `KeyShredded` event is a **registered event-type DDL** shipped by `privacy-base` (Contract #3 §3.4
typed-event model) — package work, **not** a contract change. Only §3.10 is a frozen-contract edit, and
it is staged **uncommitted** in `main` as the proposal.

## 5. Migration / compatibility

**Zero data migration.** No package ships a `sensitive: true` aspect today (verified — no `.sensitive`
DDL in any installed package; the only references are the test fixtures + the boundary code). So
encrypt-at-rest is **purely additive**: every existing aspect is non-sensitive and untouched; the first
sensitive DDL + sensitive write exercises the new path. `piiKey` is created lazily, so existing
identities are unaffected until they receive PII. Backward compatible across the board.

## 6. Test strategy

- **Unit (`internal/vault`):** envelope encrypt/decrypt round-trip; DEK wrap/unwrap under the master KEK;
  `ShredKey` ⇒ subsequent `Decrypt` fails (the shred guarantee); cache TTL + `KeyShredded` eviction.
- **Unit (`internal/processor`):** step-6.5 produces `{ct,nonce,keyId}` (never plaintext) in the
  committed batch; step-4 returns plaintext to the Starlark context; lazy `piiKey` creation lands in the
  same batch; non-sensitive aspects bypass the crypto path entirely (no Vault call).
- **e2e (ephemeral stack, a new `make test-crypto-shred`):** install a package with a `sensitive: true`
  aspect → write PII → assert **Core KV holds ciphertext** (raw `KVGet` shows no plaintext) → assert the
  Vault decrypt RPC returns plaintext → submit `ShredIdentityKey` → assert `Vault.Decrypt` fails, the
  Refractor projection rows are nullified, `keyshredded_handled_total` increments, and the Weaver marker
  converges. Mirrors `make test-object-gc` (the Loop-A/B convergence e2e precedent).
- **Gate 3 (adversarial, all DEFENDED):** add a vector — *read PII after shred* must be DEFENDED
  (decrypt fails; no plaintext anywhere); *write a sensitive aspect to a non-identity vertex* stays
  DEFENDED (existing `SensitivityViolation`).
- **Failure-tier test:** a forced nullification failure raises the privacy-critical tier (lens halts, no
  retry, alert emitted) — not a silent DLQ.

## 7. Risks & resolutions (from the adversarial pass)

| Risk | Resolution |
|---|---|
| **Vault as a write-path SPOF** (architecture line 88) | Envelope encryption + in-Processor plaintext-DEK cache ⇒ steady-state encrypt/decrypt make **no** external calls. Degradation: if the DEK is uncacheable (cold cache + KEK unreachable), **sensitive** writes fail-closed (Terminal/retry); non-sensitive writes proceed. Health: `VaultUnreachable` (Contract #5 §5.5). |
| **Plaintext leaks to a general lens target** | Refractor projects ciphertext as-is on the default path; decryption is opt-in (Secure Lens / Vault RPC only). Safe by construction in Phase A. |
| **Crash after `ShredIdentityKey` commit, before KMS destroy** (orphaned PII) | Weaver shred-finalization convergence marker re-drives destroy + nullify until converged (survives restart); brainstorm #5's "orphaned-PII nudge." |
| **Cached DEK outlives a shred** | Processor subscribes to `KeyShredded` and evicts immediately (TTL is the fallback, not the primary invalidation). |
| **Encryption breaks OCC / idempotency** | Validation runs on plaintext (step 6); idempotency keys on `requestId` (step 2). Non-deterministic GCM nonce is fine under last-writer-wins-by-revision. |
| **Nullification silently DLQ'd** | Privacy-critical failure tier: halt + page, no auto-retry (reserved tier, now built). |
| **`piiKey` itself leaking key material** | `piiKey` holds only the **wrapped** DEK (openable solely by the KEK/KMS) — never plaintext key material; satisfies "key material never in Core KV." |

## 8. Alternatives considered

- **Field-level `encrypted: true`** (brainstorm subdoc) — rejected per architecture Item 6: the aspect is
  the atomic encrypt/shred unit; partial-JSON encryption makes shred non-atomic and burdens every path.
- **Shadow aspects** (decrypted copy beside the encrypted original) — rejected per architecture Item 5:
  second source of truth, stale/failed-write consistency hazard.
- **Plaintext + access control only** — rejected: cannot satisfy right-to-erasure on an immutable ledger
  (the whole premise).
- **Encrypt in Starlark** — rejected: pushes AES/KMS into every script, breaks the "Starlark stays pure"
  guarantee, and makes the invariant un-enforceable. Encryption must be engine middleware.
- **Commit to a cloud KMS now (Path B above)** — viable, deferred: the pluggable interface lets it drop in
  later without rework; Path A keeps Phase 3 in its trusted-tool posture.

## 9. Decomposition for the Steward (fire-by-fire, each independently shippable + green)

1. **Fire 1 — `internal/vault` + local envelope backend.** The `Vault` interface, envelope encryption
   (DEK wrap/unwrap under a sealed master KEK), `CreateIdentityKey/Encrypt/Decrypt/ShredKey`, the
   `lattice.vault.decrypt` responder, unit tests. No commit-path wiring. Ships green standalone.
2. **Fire 2 — Processor encrypt-on-write + decrypt-on-read.** Step-4 decrypt hook, step-6.5 encrypt hook,
   lazy `piiKey`, DEK cache. `privacy-base` ships the `piiKey` DDL; a test package ships a `sensitive`
   aspect. e2e: Core KV holds ciphertext, Starlark sees plaintext. *(Security-plane change — run the full
   3-layer review + a party-mode pass here.)*
3. **Fire 3 — `ShredIdentityKey` op + `KeyShredded` event + Vault destruction + cache eviction.**
   `privacy-base` ships the op + event DDL; the privacy listener calls `Vault.ShredKey`; Processor evicts
   on `KeyShredded`. e2e: shred ⇒ decrypt fails.
4. **Fire 4 — Refractor `KeyShredded` nullification handler + privacy-critical failure tier + health
   counters + Weaver shred-finalization convergence lens.** Completes the guarantee + the orphaned-PII
   reconcile. `make test-crypto-shred` + the Gate-3 vector go green here.
5. **Fire 5 (gated on D1 ✅ ratified) — Refractor Secure Lens decrypt-at-projection.** Queryable PII into
   RLS-protected read models (the *blind projection* rule). Deferred behind read-path auth.

Production KMS adapters (HashiCorp Transit / AWS KMS) are a follow-on after Fire 1, pending Andrew's
backend choice (Fork 1).

---

*Designer fire, 2026-06-27. Phase A (Fires 1–4) is the ratify-and-build target now; Phase B (Fire 5)
composes on D1. The §3.10 contract edit is staged uncommitted in `main` as the proposal.*
