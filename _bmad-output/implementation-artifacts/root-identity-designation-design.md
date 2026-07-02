# Root-identity designation — design

**Status: 📐 awaiting-Andrew (ratification).** · Designer: Winston (architect) · 2026-07-02
**Revised 2026-07-02 after Andrew's grounding challenge** ("the protected bit is not what gives it root
— dive deeper"). The revision corrects the premise: the frozen contract designates root by
`holdsRole → operator` **topology**, and the shipped core `protected`-bit anchor is an Epic-12
implementation drift, not the contract's model. The recommendation changed accordingly (see §4). The
superseded first-draft shape (invent a reserved `rootActor` field) is retired to §4 Option C with the
reason it lost.
Backlog row: `planning-artifacts/backlog/lattice.md` → *Arch-review intake* →
"protected-flag-create-guard-vector". Source: `docs/reviews/arch-review-2026-07-02.md` ranked
correction #1 + the Epic-12 carried obligation (`docs/decisions/projection-plane-decomposition.md`).

---

## For Andrew (the one-look)

**The finding (why the premise moved).** Root is designated **two different ways at once**, and they
disagree:
- **Contract #7 §7.7 (frozen, of-record)** says root = **`holdsRole → operator` topology**, verbatim:
  *"root capability is established by graph topology, not by class-based special-casing"* + a mandated
  test that an identity *without `holdsRole` topology does NOT get root*. The `protected` bit is **not
  in §7.7 at all**.
- **The shipped core code** designates root by `WHERE identity.data.protected = true` in three places —
  the write anchor (`CapabilityLensDefinition`, `lenses.go:124`), the read wildcard grant
  (`CapabilityReadWildcardGrantsLensDefinition`, `lenses.go:342`), and `SystemActorKeys`
  (`system_actors.go:60`). This is an **Epic-12 drift**: to keep core authorizable without rbac-domain,
  the god-cypher's `holdsRole` walk was replaced by the `protected` literal and *moved* to rbac-domain's
  `cap.roles.<actor>` lens. The 7 kernel actors now carry **both** markers; at runtime they authorize
  via the `protected` anchor (their `holdsRole → operator` is projected-but-unread).

**Why the bit is "easily compromisable" and the link is not.** `protected` is a boolean inside the
vertex's own `data`, set at create time, and identity-create is **unguarded** (step-8 exempts create).
The read wildcard is **unconditionally** forgeable (a plain projection, ungated by routing: forge a
`protected:true` identity → `*` read grant → read every RLS-protected row). The `holdsRole → operator`
topology, by contrast, is **self-protecting**: `AssignRole`/`GrantPermission`/`CreateRole` are granted
only to `operator` at `scope:any`, so **you must already be root to grant root** — it cannot be
bootstrapped from nothing.

**The fork (§4) — my recommendation flipped to A.** **(A) Re-converge core on the topological model**
the contract already mandates: designate root by a bounded `holdsRole → operator` existence check in the
three core sites, and **retire `protected` as a capability designator** (it keeps only its unrelated
anti-brick meaning). Closes both escalations *by construction*, uses the already-seeded self-protecting
topology, invents nothing. **(B)** keep + harden the `protected` anchor (create-guard + a reserved
field) and reconcile §7.7 to the bit. I recommend **A**: it is unforgeable-by-construction and matches
the frozen contract; B doubles down on the data-derived mechanism you flagged.

**Frozen-contract change (either fork, flagged — staged uncommitted at build time):** **Contract #7
§7.7 contradicts the shipped code today** and must change regardless. Under A it is *restored* (core
re-adopts the topological anchor); under B it is *rewritten* to document the `protected` marker. This is
the ratification crux.

**Interim floor, valuable under both (Fire 1):** a Processor **create-guard** that no non-root op can
mint a root-designating marker, + Gate-2/Gate-3 vectors. Discharges the Epic-12 obligation now, no
contract change, ships alone.

---

## 1. Problem + intent

### 1.1 What confers root — the two layers, precisely

Root capability = six platform grants at `scope:any` (`CreateMetaVertex`, `UpdateMetaVertex`,
`TombstoneMetaVertex`, `InstallPackage`, `UninstallPackage`, `UpgradePackage`) **plus** the wildcard
`*` read anchor. The primordial seed establishes this for 7 identities (the admin + Loom / Weaver /
Bridge / object-store-manager / privacy service actors) via **two redundant mechanisms**, both seeded
in `internal/bootstrap/primordial.go`:

1. **Topological (the contract's model, §7.7).** A full operator role is seeded: the `operator` role
   vertex, six `permission` vertices (the six ops above), six `grantedBy` links (permission → operator),
   and a `holdsRole → operator` link from each of the 7 identities (`primordial.go:704-766`, entries
   10 + 10a). rbac-domain's `capabilityRoles` lens walks `identity -[:holdsRole]-> role <-[:grantedBy]-
   permission` (`packages/rbac-domain/lenses.go:72`) and projects the grants into `cap.roles.<actor>`.
2. **The `protected` literal (the Epic-12 core anchor).** Post-Epic-12, core's own three sites key on
   `data.protected = true` so core is self-sufficient without rbac-domain installed:
   - `CapabilityLensDefinition` (`lenses.go:124`) → `cap.<actor>` write grants (actor-aggregate,
     `WHERE identity.data.protected = true`).
   - `CapabilityReadWildcardGrantsLensDefinition` (`lenses.go:342`) → the `*` read anchor (a **plain**
     full-graph projection, `WHERE identity.data.protected = true`, one row per matching identity).
   - `SystemActorKeys` (`system_actors.go:60`) → a **startup scan** for `protected==true` identities,
     wired once into step-3 routing (`cmd/processor/main.go:124`).

**Runtime routing** (`step3_auth.go:183`, `classAwarePlatformKey(SystemActorKeys)`): when rbac-domain is
installed (the standard stack — `Makefile:449`), the actors in the startup `SystemActorKeys` snapshot
read `cap.<actor>` (the `protected` anchor); every other actor reads `cap.roles.<actor>`. When
rbac-domain is absent, **all** actors read `cap.<actor>`. So the seeded actors always authorize via the
`protected` anchor; their `holdsRole → operator` topology feeds a `cap.roles.<actor>` doc that is
**projected but never read** for them.

### 1.2 The threat model (corrected — conditional writes, unconditional reads)

The escalation is "cause a `protected:true` identity to exist." Its power splits:

- **Read path — unconditional.** The read wildcard lens is a plain projection keyed on `protected=true`,
  with **no routing gate**. A forged `protected:true` identity gets a `*` wildcard read grant →
  every RLS-protected read-model row is readable. Immediate, independent of rbac, independent of restart.
  *(This is the clean, always-live exploit — the first draft under-weighted it.)*
- **Write path — conditional.** `SystemActorKeys` is a **startup snapshot**, so a runtime-forged
  protected identity is not in it: with rbac installed it routes to `cap.roles.<forged>` → no
  `holdsRole` → **denied**. But it escalates (a) with rbac **absent** (all actors read `cap.<actor>` →
  the `protected` anchor projects root), or (b) after a **Processor restart** re-snapshots
  `SystemActorKeys` and pulls the forged identity into the `cap.<actor>`-routed set.

### 1.3 Why the bit is the weak designator (and the link is not)

- **In-band + create-unguarded.** `protected` sits in the vertex's own `data`; `rejectProtectedMutations`
  (`step8_commit.go:135`) guards **update/tombstone** but **exempts create** (`step8_commit.go:134`) —
  sound for overwriting an existing root, vacuous for minting a new identity. Only identity-domain's
  `data:{}` convention (`ddls.go:399`) stops it today — the Epic-12 carried obligation.
- **The topology is self-protecting.** Forging root topologically means creating
  `lnk.identity.<x>.holdsRole.role.<operatorId>`, which only `AssignRole` writes — and `AssignRole`,
  `GrantPermission`, `CreateRole` are all `GrantsTo:["operator"] scope:any`
  (`packages/rbac-domain/permissions.go:25`). **You must already hold operator to grant operator.** Root
  cannot be bootstrapped from nothing. This is the property `protected` structurally lacks.

### 1.4 Intent

Designate root by a mechanism that is (a) **out-of-band** from business data, (b) **unforgeable without
already being root**, and (c) **consistent with the frozen contract**. The contract already describes
such a mechanism (§7.7 topology); the work is to make the shipped code match it, and to guard the mint
path in the interim.

---

## 2. Grounding — contract, drift, and the invariant tension

- **Contract #7 §7.7 is the of-record designation and it is topological.** It instructs the capability
  cypher to *"Walk identity → `holdsRole` → role"* and *"walk inbound `grantedBy` links from the role to
  discover permission vertices"*, and states *"root capability is established by graph topology, not by
  class-based special-casing."* The shipped `protected` anchor contradicts this — so **§7.7 vs. the code
  is a live, untracked contract-vs-code drift** that this design must resolve either way.
- **Epic-12's reason for the drift was real but narrower than it looks.** The decomposition removed the
  `holdsRole/role/permission/grantedBy` walk from **core's** cypher so core "references no rbac
  vocabulary" and stays authorizable when the rbac-domain **package** is absent
  (`06-capability-kv.md` §6.1). But the operator-role topology is **primordial (core-seeded)** — it
  exists in the graph with or without the rbac-domain package. So a core anchor that walks the
  *primordial* topology is **package-independent**; what Epic-12 actually bought was cypher-vocabulary
  cleanliness, not a genuine dependency break. That reframes Fork A's cost as *nominal vocabulary
  coupling to primordial concepts*, not *a package dependency* (§4).
- **The self-predicate is cheap to preserve as a bounded check.** Epic-12 feared an "unbounded whole-type
  scan." "Does identity X hold the operator role" is not that — it is a **single deterministic outbound
  link check** from one anchor (`MATCH (i:identity {key:$actorKey})-[:holdsRole]->(r:role {…operator})`),
  the same bounded traversal the full engine already runs and the same class of bounded op-time link read
  the write-path-read posture now sanctions. No scan.
- **Invariants unaffected:** the seed stays the sanctioned direct-write (P2); every runtime mutation
  flows through the Processor; no engine gains a new Core-KV read (the check runs in the Refractor
  projection, where the capability lens already runs); key shapes (Contract #1) unchanged.

---

## 3. The shape

### 3.1 Fire 1 — the create-guard floor (no contract change, ships alone, fork-independent)

Extend step-8 to reject a **create** that mints a root-designating marker on an identity. Concretely,
reject a `create` mutation that is identity-labelled (key-type `identity`, or `class`/`label ==
"identity"` — the three ways the capability lens's `:identity` matches, `executor.go:352-372`) **and**
carries `data.protected == true`. Surface as the existing `ProtectedKey` reply.

- Breaks nothing: package install creates protected **meta** vertices (not identity-labelled);
  `CreateIdentity` creates `data:{}` identities. No op legitimately mints a protected identity.
- Unconditional — no actor-tier lookup in step 8.
- Discharges the Epic-12 obligation immediately, under **either** fork, and independently of the read
  wildcard (which Fire 2 addresses structurally).

### 3.2 Fire 2 (Fork A — recommended) — re-converge core on the topology, retire `protected` as designator

Replace the `protected` predicate with a bounded `holdsRole → operator` check in the three core sites:

1. **`CapabilityLensDefinition`** (`lenses.go:124`) — anchor the root grant set on
   `MATCH (identity:identity {key:$actorKey})-[:holdsRole]->(:role {canonicalName:'operator'})` instead
   of `WHERE identity.data.protected = true`. (Grants stay a literal set — this only changes the *gate*,
   not the projected shape.)
2. **`CapabilityReadWildcardGrantsLensDefinition`** (`lenses.go:342`) — same gate swap; the `*` read
   grant now flows only to operator-holders. **This is the fix that closes the unconditional read
   escalation.**
3. **`SystemActorKeys`** (`system_actors.go:60`) — discover the root set by the `holdsRole → operator`
   topology instead of scanning `data.protected`. (Still a startup discovery; still core-owned and
   package-independent because the topology is primordial.)

`data.protected` is **retired as a capability designator** and keeps **only** its anti-brick meaning
(the `rejectProtectedMutations` update/tombstone guard, unchanged). After Fire 2, forging `protected:true`
grants **nothing** — capability is conferred solely by operator-role topology, which is self-protecting.

**Contract:** #7 §7.7 is *restored* (core re-adopts the topological anchor, scoped to the operator role);
a §6.1 note records that core's anchor walks the **primordial** operator topology (not a package
dependency). No new envelope field, no new vertex/aspect/link type, no new op.

### 3.3 Fire 2 (Fork B — alternative) — keep + harden the `protected` anchor

If Andrew prefers to preserve Epic-12's zero-rbac-vocabulary core: keep the `protected` anchor but make
it unforgeable and reconcile the contract. Introduce a reserved **seed-only** marker distinct from the
anti-brick bit (a top-level envelope field `rootActor`, or the bit itself under a generalized guard),
and make the Processor reject any op that sets it (unconditional — nothing but the direct seed sets it).
Migrate the three sites to the new marker; rewrite Contract #7 §7.7 to document the marker model
(abandoning the topological text). This keeps the decoupling but keeps a data-derived designation and
requires amending #1 §1.3 (new envelope field) + rewriting §7.7. See §4 for why A is preferred.

### 3.4 Read/write paths, key shapes — unchanged

Under both forks the `cap.<actor>` / `cap-read.root` doc shapes are byte-identical; only the **gate
predicate** changes. Step-3's hot path (one KVGet by actor class) is untouched — the gate is evaluated
at projection time, not on the authz hot path.

---

## 4. The fork — topological re-convergence (A) vs. hardened marker (B)

| | **A. Re-converge on `holdsRole → operator`** (rec.) | **B. Harden the `protected` marker** | **C. New `rootActor` field** (first draft — retired) |
|---|---|---|---|
| Designation | Link topology (contract §7.7) | Reserved seed-only bit/field | Reserved seed-only envelope field |
| Forgeable? | **No — self-protecting** (grant-role is root-gated) | No, once guarded (but create-guard is the load-bearer) | No, once guarded |
| Read escalation | Closed **by construction** | Closed by the guard | Closed by the guard |
| Invents mechanism? | **No** — already seeded | A new marker | A new marker (a *third* one) |
| Contract #7 §7.7 | **Restored** to as-built | **Rewritten** away from topology | Rewritten away from topology |
| Other contracts | §6.1 note | #1 §1.3 (new field) + §6 | #1 §1.3 (new field) + §6 |
| Cost | Nominal rbac-*vocabulary* in core's cypher (primordial concepts; not a package dep) | Keeps a data-derived root-of-trust; two markers to keep coherent | Same as B, plus it ignores the contract's existing link model |

**Recommendation: A.** It is **unforgeable by construction** (you cannot grant yourself operator without
already holding it), it **matches the frozen contract** instead of rewriting it away, and it **invents
nothing** — the topology is already seeded and already walked by rbac-domain. Its only real cost is
re-admitting `holdsRole`/`operator` vocabulary into core's cypher, and §2 shows that coupling is to
**primordial** concepts, not to the rbac-domain package — so core stays authorizable standalone. B and C
both double down on the data-derived designation Andrew flagged as easily compromisable, and both force
§7.7 to be rewritten *away* from the model it correctly specifies.

**Why C (my first draft) lost.** It proposed a new reserved `rootActor` envelope field — a *third*
designation mechanism, data-derived, requiring a Contract #1 envelope amendment — while the contract
already specifies a link-based, self-protecting mechanism that is already in the seed. It was the
"greenfield a new mechanism where the codebase already decomposed" reflex; Andrew's grounding challenge
surfaced it. Retained here only to record the rejection.

---

## 5. Reconciliation with the existing mental model

- **"Isn't root already `holdsRole → operator`?"** Yes — per Contract §7.7 and the seed, and that is
  exactly the point of Fork A. The shipped `protected` anchor is an Epic-12 drift *away* from that model;
  A restores it. (This is the question Andrew's challenge raised, and it reframed the whole design.)
- **"Didn't Epic-12 deliberately remove the graph walk from core?"** It removed it to avoid a **package**
  dependency on rbac-domain and to keep an unbounded scan out of the hot path. §2 shows the operator
  topology is **primordial** (no package dep) and the check is a **bounded** one-key traversal (no scan),
  so A recovers the contract model without reincurring what Epic-12 was actually avoiding. What A does
  reverse is the narrower "core's cypher names no rbac vocabulary" preference — which is the crux for
  Andrew to ratify.
- **"Are we adding state?"** Fork A adds **none** — it re-gates on topology that is already seeded. Fire
  1 adds none. (Only Fork B/C would add a marker.)
- **Stale-`cap.<actor>` eviction** (the Epic-12 sibling obligation) is out of scope — a downgrade/realness
  concern, moot on the store-reset upgrade path, orthogonal to designation.
- **Redundancy note.** Under A, the seeded actors' `cap.<actor>` (core anchor) and `cap.roles.<actor>`
  (rbac projection) would both derive from the same `holdsRole → operator` topology — consistent, not
  conflicting. A later simplification *could* drop the core anchor for actors once rbac is guaranteed
  present, but core self-sufficiency argues for keeping the core-owned anchor; that trade is notable but
  not in this scope.

---

## 6. Decomposition for the Steward

**Fire 1 — create-guard floor + adversarial vectors. Size S. No contract change. Fork-independent.**
- Reject a create of a `data.protected:true` identity-labelled vertex (§3.1); typed `ProtectedKey` reply.
- **Gate-2 (bypass) BLOCKED:** an ordinary actor's op emitting a `create` of `vtx.identity.<x>` (and a
  `class:"identity"` non-`vtx.identity` key) with `data.protected:true` → rejected.
- **Gate-3 (capadv) DEFENDED:** the full escalation — attempt the forged create as a non-root actor,
  assert no `cap.<actor>` and **no `*` read grant** materialize. Add as the next capadv vector #.
- Discharges the Epic-12 obligation. Ships regardless of the fork decision.

**Fire 2 — behind Andrew's fork choice.**
- *If A:* swap the three core predicates to the `holdsRole → operator` check (§3.2); restore Contract #7
  §7.7 + a §6.1 note (staged uncommitted). Tests: unit — the anchor + read wildcard project root iff the
  identity holds operator (and **not** for a `protected:true`-only identity — the inverse of the old
  test, proving the drift is closed); `SystemActorKeys` discovers by topology. e2e — the ephemeral stack
  still authorizes the admin + service actors for their root ops after the swap (behavior-preserving);
  a forged `protected:true` identity gets **neither** write nor read root. Re-point the Fire-1 Gate-2/3
  vectors at the topological gate.
- *If B:* introduce the reserved seed-only marker + generalized guard; migrate the three sites; rewrite
  §7.7 + amend #1 §1.3 (staged uncommitted). Tests mirror A with the marker in place of the topology.

Sequencing: Fire 1 is the urgent ★★★ floor with no contract dependency and ships immediately; Fire 2 is
the contract-touching re-convergence gated on the fork ratification. Principled split (independent value
+ distinct ratification gates), not size-padding.

---

## 7. Migration / compatibility

- **Fire 1** is additive enforcement — no data migration; existing seeds already satisfy it.
- **Fire 2 (A)** changes only lens **predicates** + the `SystemActorKeys` discovery query; the seed
  already carries the operator topology, so **no re-seed is required for correctness** (the topology
  exists). A bootstrap-version bump is still the clean path if the seed drops the now-vestigial
  `protected:true` from the 7 identities — but note `protected` is **retained** on them for anti-brick,
  so the seed need not change at all; only the code predicates move. This makes A a notably low-risk
  migration (no store reset strictly required — a projection rebuild suffices, since the topology is
  already present).
- **Contract review is the ratification gate** — the three predicate sites move together; there is no
  window where one site reads topology while another reads the bit.

---

## 8. Risks + alternatives considered

- **Risk (A): a seeded actor lacks the `holdsRole → operator` link** → it loses root (fail-closed — a
  denial, caught by the Fire-2 e2e that authorizes every seeded root actor). All 7 are seeded with it
  (`primordial.go:704-766`), so this is a regression guard, not a live gap.
- **Risk (A): the operator role canonicalName resolution.** The check keys on the operator role; pin it
  by the seeded role NanoID or its `canonicalName` aspect (`role.canonicalName.data.value == "operator"`)
  — the same resolution rbac-domain's lens already uses — not a brittle string in the vertex root.
- **Alternative — keep the bit, add only the create-guard (Fire 1 alone).** Closes the write-mint hole
  but leaves the **unconditional read wildcard** open and does not answer "something other than the bit."
  Fire 1 ships as the floor; Fire 2 (A) is the actual answer.
- **Alternative — enumerated kernel set as a lens param.** Rejected: couples the lens to a
  bootstrap-injected list; buys nothing over the topology.
- **Fork B / C** — §4.

---

## 9. Open questions — resolved (except the one fork for Andrew)

- *Which mechanism actually confers root?* → Two, redundantly; the seeded actors authorize via the
  `protected` anchor at runtime, but the **contract designates the `holdsRole → operator` topology** and
  the `protected` anchor is an Epic-12 drift (§1.1).
- *Is the bit really compromisable?* → **Yes, unconditionally on the read path** (wildcard grant),
  conditionally on writes (rbac-absent or post-restart) (§1.2).
- *Shape?* → **Fork for Andrew** (§4). Recommendation: **A** (re-converge on the self-protecting
  topology; retire `protected` as designator). B/C are the data-marker alternatives.
- *Contract surface?* → Fire 1 none. Fire 2 changes **Contract #7 §7.7 either way** (restore under A,
  rewrite under B) — the ratification crux; A also adds a §6.1 note, B also amends #1 §1.3.
- *Migration?* → Fire 1 none; Fire 2 (A) is predicate-only, no re-seed required (topology already
  seeded) — a projection rebuild suffices.

**Pre-build gate:** none self-imposed — an S / S–M security-plane change; the build's standard full
3-layer adversarial review is the gate (run at build time), given the create-guard + capability-anchor
surface.
