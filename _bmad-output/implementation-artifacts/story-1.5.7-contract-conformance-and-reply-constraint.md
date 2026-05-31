# Story 1.5.7 — Contract conformance suite + reply-constraint freeze

**Phase 1.5 hardening · Wave C · depends on 1.5.1, 1.5.3 (both shipped).**
Author: Winston (CS). Implementer: DS sub-agent (does NOT commit). CR follows.

---

## 1. Why this story exists

Two jobs, fused because the second freezes the first:

1. **Close the write-path-as-read-channel escape hatch (Andrew directive, BINDING).**
   `OperationReply.Detail map[string]any` is populated verbatim from a Starlark
   script's `response` return key (`internal/processor/starlark_runner.go`
   `parseScriptResult` → `ScriptResult.ResponseDetail` → `commit_path.go`
   `BuildAcceptedReplyWithRevisions` → `OperationReply.Detail`). It is an
   arbitrary, unvalidated, "MUST NOT be logged — may contain sensitive tokens"
   map. A reply field that needs a do-not-log warning is carrying data it
   shouldn't. Epic-4/Story-4.6 claimed to walk this back but didn't (field +
   comments remain; "reviewers maintain compliance, processor does not enforce"
   = not enforced). This story REMOVES the arbitrary-data hatch and ENFORCES the
   constraint in code — not just freezes the shape.

2. **Freeze the contract shapes behind a conformance suite** (envelope / reply /
   contextHint, Core KV key shapes, DDL aspect set), with the now-constrained
   reply shape as the centerpiece. This is the Phase 2 readiness gate.

### What `Detail` carries today (full codebase inventory — all must be re-homed)
- **① Commit-trace identifiers** — `identityKey`, `roleKey`, `permissionKey`,
  `linkKey`, `metaKey`, `declaredKeys`, `tombstonedKeys`, `name`/`version`.
  Every one is a key the Processor itself just wrote. The Processor already
  holds the mutation set and already returns `Revisions` (a `map[string]uint64`
  keyed by every committed key). Script echo is redundant.
- **② Read-derived signals** — `possibleDuplicateFlag` (bool, from reading
  identity indexes), `alreadyAssigned`/`alreadyGranted` (bool). These surface a
  *read result* through the write reply — exactly the targeted anti-pattern.
- **③ One genuine one-time secret** — `claimKey` plaintext from
  `CreateUnclaimedIdentity` (`packages/identity-domain/ddls.go`).

---

## 2. LOCKED design decisions (settled with Andrew 2026-05-30 — do NOT relitigate)

### D1 — Remove `OperationReply.Detail` entirely.
Delete the `Detail map[string]any` field, its do-not-log comment block, and the
`ScriptResult.ResponseDetail` → reply plumbing for arbitrary maps.

### D2 — Commit-trace identifiers → typed, Processor-validated `PrimaryKey`.
Add `OperationReply.PrimaryKey string` (`json:"primaryKey,omitempty"`). A script
MAY return a **closed** `response` dict whose ONLY permitted key is
`primaryKey` (a string). The Processor **validates that `primaryKey` is a member
of the committed mutation set** (i.e. a key the op actually wrote) and rejects
the operation otherwise. Any other key in `response` is a fail-closed error
(`ScriptFailed`/`InvalidReturnShape` — pick the existing typed code that fits;
prefer rejecting at parse/validate time before commit). Net effect: the script
can only point at a key it really committed; it cannot smuggle arbitrary data.
The full committed key set is already available to clients via `Revisions` map
keys — **no new `CommittedKeys` field**.

- Ops with no single principal entity (InstallPackage / UninstallPackage —
  multi-key) simply omit `primaryKey`; clients read the key set from
  `Revisions`. Do NOT invent a synthetic primary for them.

### D3 — Drop read-derived signals from the reply.
`possibleDuplicateFlag`, `alreadyAssigned`, `alreadyGranted` leave the
synchronous reply entirely. Duplicate detection already rides the
`IdentityCreated` event (`data.duplicate`); idempotency "already X" outcomes are
observable via the link key being present in `Revisions` + the op being a
`duplicate`-status replay. No typed boolean fields are added.

### D4 — claimKey = client supplies the hash, no return channel (Option C).
The **client** mints the plaintext claim secret, computes `sha256`, and submits
**only the hash** in the op payload. `CreateUnclaimedIdentity` stores it
verbatim; the reply returns nothing sensitive.

- Payload gains `claimKeyHash` (required, non-empty; lowercase hex sha256 —
  validate shape) and `claimKeyAlgo` (optional; default `"sha256"`, the only
  accepted value for now).
- `CreateUnclaimedIdentity` script (`packages/identity-domain/ddls.go`):
  - DELETE `claim_key_plaintext = nanoid.new()` and
    `claim_key_hash = crypto.sha256(...)`.
  - Read `p.claimKeyHash` (+ algo), validate, store **verbatim** in the
    `.claimKey` aspect: `data: {"hash": <claimKeyHash>, "algo": <algo>}`.
  - DROP `claimKey` and `possibleDuplicateFlag` from `response`; return
    `response: {"primaryKey": identity_key}` only.
  - Keep the duplicate-detection read + `IdentityCreated` `data.duplicate`
    event (D3 — the signal lives on the event, not the reply).
- `ClaimIdentity` is UNCHANGED in mechanism (still compares
  `sha256(presented) == stored hash`); it already takes `claimKey` plaintext in
  its payload.
- **Plaintext never enters Lattice** — not the persisted `core-operations`
  stream, not the reply. No `OneTimeSecret` field, no `secret.mint()` builtin.
  The Processor stays fully generic (zero op coupling).

### D5 — Keep the SEPARATE `ScriptError.Detail` ClaimKeyInvalid side-channel.
`internal/processor/script_context.go` `ScriptError.Detail` (the internal
"invalid-key"/"wrong-state" outcome string read by the ClaimEmitter for Health
KV, then stripped before reply egress) is a DIFFERENT field and is **unaffected
and correct** — do not touch it. Only `OperationReply.Detail` and the
`ScriptResult.ResponseDetail`-arbitrary-map flow change.

---

## 3. Work items

### A. Reply shape + Processor enforcement (`internal/processor/`)
1. `envelope.go`: delete `OperationReply.Detail` + its comment block; add
   `PrimaryKey string \`json:"primaryKey,omitempty"\``.
2. `script_context.go`: replace `ScriptResult.ResponseDetail map[string]any`
   with `ScriptResult.PrimaryKey string` (or equivalent typed single value).
   Update the doc comment (no more "may carry sensitive tokens").
3. `starlark_runner.go` `parseScriptResult`: parse `response` as a **closed
   schema** — only `primaryKey` (string) permitted; any other key → typed
   fail-closed error. Absent `response` / absent `primaryKey` = empty
   (allowed). Remove `starlarkDictToGoMap` usage for `response` (check if the
   helper is now dead; remove if so).
4. Add Processor validation that `PrimaryKey`, when set, is a member of the
   committed mutation key set. Place it where the mutation set is known and the
   op can still be rejected cleanly (validate step or commit path before
   `replyTo`). Rejection uses an existing typed code (`DDLViolation` or
   `ScriptFailed` — choose the closest; document the choice).
5. `reply.go`: replace `BuildAcceptedReplyWithDetail` /
   `...WithRevisions(detail, revisions)` with builders that take
   `primaryKey string` + `revisions`. Update `commit_path.go:336` call site.
6. Remove the now-obsolete NFR-S6/S7 "MUST NOT be logged" comments that existed
   only because of the arbitrary map (`script_context.go:80`,
   `starlark_runner.go:221`, `envelope.go` block). Leave genuine sensitive-aspect
   logging guards elsewhere intact.

### B. claimKey Option-C migration (`packages/identity-domain/`)
7. `ddls.go` `CreateUnclaimedIdentity`: per D4.
8. `create_test.go`: update `TestCreateUnclaimed_ClaimKeyHashOnly` and any test
   using `nanoIDsFromRequestID`-derived claim keys to the client-supplied-hash
   model (test computes `sha256(plaintext)`, submits hash, asserts the stored
   aspect hash equals it and that plaintext appears nowhere — aspect or reply).
   Update tests asserting `response.claimKey` / `possibleDuplicateFlag`.
9. `ddls.go` self-description (input schema / field docs / examples): add
   `claimKeyHash`/`claimKeyAlgo`; remove `claimKey` plaintext + duplicate-flag
   from the output schema; output schema now `{primaryKey}`.

### C. Migrate every `response` builder to the closed `{primaryKey}` shape
10. `packages/rbac-domain/ddls.go`: `roleKey`/`permissionKey`/`linkKey` →
    `primaryKey`; DROP `alreadyAssigned`/`alreadyGranted`.
11. `internal/bootstrap/meta_ddl.go`: `metaKey` → `primaryKey` (4 sites).
12. `internal/bootstrap/install_ddl.go`: Install/Uninstall return no
    `primaryKey` (multi-key); drop `declaredKeys`/`tombstonedKeys`/`name`/
    `version` from `response` (events already carry name/version/keyCount;
    clients use `Revisions` for keys). Verify the installer
    (`internal/pkgmgr/installer.go`) does not read those response fields — if it
    does, re-source from `Revisions`/events.
13. `packages/identity-hygiene/ddls.go`: migrate its `response` block to
    `{primaryKey}` or drop if no principal key; update its convention comment.
14. `packages/identity-domain/ddls.go` `ClaimIdentity` (`response:{identityKey}`)
    → `{primaryKey: target_identity_key}`.

### D. Consumers (CLIs + tests)
15. `cmd/lattice/op/op.go`: replace the `reply.Detail` range-printer with
    `PrimaryKey` + `Revisions` keys.
16. `cmd/lattice/lens/lens.go`: `Detail["metaKey"]` → `reply.PrimaryKey`.
17. `cmd/lattice/identity/identity.go`: the CLI must now **generate** the claim
    secret locally, print the plaintext it generated, compute sha256, and put
    the hash in the submitted payload. It no longer reads `claimKey` from the
    reply. Read the created key from `PrimaryKey`.
18. `internal/hellolattice/hellolattice_test.go`: every `reply.Detail[...]`
    (metaKey/bookKey/identityKey/permKey) → `reply.PrimaryKey`. (Touch lightly —
    1.5.6 re-enables this suite; just keep it compiling/correct here.)

### E. Conformance suite (the freeze)
19. Add a contract-conformance test package (suggest
    `internal/conformance/` or `internal/processor/conformance_test.go` — pick
    one, document why) that asserts the FROZEN shapes:
    - **OperationReply**: exact JSON field set — `requestId`, `opTrackerKey`,
      `status`, `committedAt`/`originalCommittedAt`, `error`, `decision`,
      `revisions`, `primaryKey`. **Assert NO `detail` field exists** (a
      regression guard: marshal a reply and assert the wire bytes never contain
      `"detail"`). Assert `error.code` ∈ the closed `ErrorCode` enum.
    - **OperationEnvelope / ContextHint / AuthContext**: required-field +
      shape conformance (reuse `ParseEnvelope` rules).
    - **Core KV key shapes** (Contract #1): `vtx.<type>.<id>[.aspect]`,
      `lnk.<...>`, `vtx.meta.<NanoID>` — assert validators accept canonical /
      reject malformed.
    - **DDL aspect set** (Contract for meta-vertices): the frozen aspect names.
    - **Reply-constraint enforcement**: a Processor-level test proving a script
      returning a `response` with a non-`primaryKey` key, or a `primaryKey` not
      in the committed mutation set, is REJECTED (fail-closed) — this is the
      in-code enforcement proof Andrew asked for, not just a shape freeze.
20. Add a `make` target (e.g. `verify-conformance`) and wire it into the gate
    set if the repo's Makefile pattern expects it (mirror existing
    `verify-*`/`test-*` targets). Document in the brief's §5 gates.

### F. Docs (`docs/contracts/`)
21. Contract #2 (envelope/reply) doc: remove `detail`; document `primaryKey`
    (typed, commit-set-validated) and that the full committed key set is in
    `revisions`. Document the closed `response` script-return schema.
22. Identity claim-flow doc (Contract for identity-domain or
    `docs/contracts/0x-*`): document Option C — client mints, submits
    `claimKeyHash`; Lattice never holds plaintext; reply returns no secret.
23. Remove/replace the `OperationReply.Detail` "convention" comment blocks in
    `packages/identity-hygiene/ddls.go` and anywhere else that documents the old
    allowed/forbidden map convention.

---

## 4. Non-goals / out of scope
- F-004 package version upgrade (deferred to its own story).
- UninstallPackage per-key OCC follow-up (open low CAR — not here).
- Re-enabling Hello Lattice M4–M6 / flipping Gate 5 (that is **1.5.6**, next).
- Any change to `ScriptError.Detail` (the ClaimKeyInvalid side-channel — D5).
- Any new server-side secret minting / `secret.mint()` builtin (rejected; D4).

## 5. Gates (all must pass before Winston commits)
- `go build ./...`, `make vet`, `golangci-lint run ./...`
- `make verify-kernel`
- `make verify-package-rbac`, `make verify-package-identity`,
  `make verify-package-identity-hygiene`
- `make test-bypass` (Gate 2), `make test-capability-adversarial` (Gate 3)
- New: `make verify-conformance` (or chosen target) green
- Identity create/claim E2E still green under the new hash-supplied model
- **grep guard**: no `OperationReply.Detail` references remain; no `"detail"`
  key emitted on any accepted reply; no `response` builder emits a non-
  `primaryKey` key.
- Deviation 14 applies: if shared-NATS flakes appear, `make down && make up`,
  re-run flaky packages in isolation; CI on the clean stack is authoritative.

## 6. Workflow constraints (binding)
- DS sub-agent implements but **does NOT commit or push**. Winston drift-reviews,
  runs CR, adjudicates P0/P1/P2, commits when green, watches CI.
- Sub-agents **never** edit `_bmad-output/planning-artifacts/*` (Winston-only).
- No history comments in code (`// Story 1.5.7`, `// Replaces`, `// Previously`).
- New docs land in `/docs`, not `_bmad-output/`.
