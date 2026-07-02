# Done log archive — lattice (older shipped items, newest first)

Rolled from `lattice.md` when its live Done log passed ~25 entries. Full detail is in git.

- 2026-06-27 · `679fe25` · [Refractor] Full-engine plain-projection anchor-tombstone retraction (`AnchorDeleteResult`)
- 2026-06-30 · `44049ed` · [Core/bypass] D1.4 — Gate-3 read-path authorization adversarial vectors (§5.1–5.5: no-JWT · cross-actor · revoked · cross-anchor bleed · no-RLS-policy); Gate 3 now 13/13, gate sets `POSTGRES_TEST_DSN`
- 2026-06-30 · `<pending>` · [clinic-domain] kv.Links Fire 2 — re-author the appointment double-book guard onto `hasBooking` links + scalar `bookingGuard` epoch (drop the `.bookings` key-list aspects + DDLs); pkg 0.8.0
- 2026-06-30 · `cc2613f` · [Core/substrate] kv.Links Fire 1 — bounded op-time link enumeration primitive (+ `KVListKeysFilter` paged subject-filter seam)
- 2026-06-30 · `3dbd049` · [Augur] Fire 2a — `ReviewProposal` human-verdict op
- 2026-06-30 · [CI] Flake-hunt: mined the re-run history (attempt-aware) — found the Hello-Lattice NFR-P3 flake
- 2026-06-29 · `faa3aec` · [Refractor] GrantTable composite-keyed anchor-tombstone retraction
- 2026-06-29 · `89a9842` · [CI] Halve the leaseshortwindow freshness window (40s→25s) — convergence −33s
- 2026-06-29 · `65f4f4d` · [Loom/orchestration-base] Adapter-read-seam Fire 1 — subject-templated params
- 2026-06-29 · `f04f331` · [Core/bootstrap] D1.3 Increment 1 — base `capabilityRead` self-anchor lens
- 2026-06-29 · `c1a8901` · [Core/pkgmgr + Refractor] Package-declared protected/grant Postgres lenses
- 2026-06-29 · `d772195` · [Refractor] Full-engine multi-column projection key (GrantTable producer)
- 2026-06-29 · `d85450d` · [Refractor/Core] `nanoIdFromKey` auth-plane cypher fn (D1 prereq)
- 2026-06-29 · `97afcd2` · [Core] Processor commit OCC §3.2 update-conditioning + bounded retry + Health signal
- 2026-06-29 · `6eaabcc` · [Core] instanceOf Fire E — expose the op's own type-DDL meta key to Starlark
- 2026-06-29 · `cea0c3b` · [Core] instanceOf Fire 1 — step-6 governing-DDL chain resolver
- 2026-06-29 · `1443109` · [CI] Grounding fire: re-measured the pipeline
- 2026-06-28 · `ce2086f` · [CI] Parallelize the lease-convergence e2e gate (t.Parallel)
- 2026-06-28 · `07f3824` · [CI] Parallelize the weaver test package (t.Parallel)
- 2026-06-27 · `1443109` · [CI] Serial pipeline → 4-job parallel matrix
- 2026-06-28 · `7f98d83` · [Core/pkgmgr] F-004 Fire 3 — dev-loop refresh targets + upgrade docs
- 2026-06-28 · `cd20ce8` · [Core/pkgmgr] F-004 Fire 1a — version-independent entity keys
- 2026-06-28 · `75e9acc` · [Core/substrate] NATS write-restriction Fire 1 — credential seam (dark)
- 2026-06-28 · `04c7689` · [Weaver] Pace collapse-only userTask reclaims with a state machine
- 2026-06-27 · `d8bfa34` · [Loom] Pin the redelivery-dedup + op-meta-deregister paths
- 2026-06-27 · `4bd32f7` · [Core] Pin the Go↔Starlark value marshalling boundary
- 2026-06-27 · `8199c11` · [Weaver] Cover the control-plane authorize boundary + subject parsing
- 2026-06-27 · `a4f87ae` · [Core] Pin the substrate deterministic-id derivation invariant
- 2026-06-27 · `fd0cacd` · [Loupe] Test the object-serving anti-XSS disposition boundary
- 2026-06-27 · `f537f6b` · [Refractor] Postgres adapter `Truncater`
- 2026-06-27 · `c59a39f` · [Loom] Heartbeat status hard-coded to lifecycle string
- 2026-06-27 · `6998a39` · [Core] Bootstrap key construction → substrate key helpers
- 2026-06-27 · `f16e625` · [Core] Processor health-honesty — real `lane_lag` + status/issues
- 2026-06-26 · `4de7677` · [Weaver] Heartbeat status/issue-severity inconsistency
- 2026-06-26 · `2877a1c` · [Loupe] Surface component `status`/`issues` on health + system-map
