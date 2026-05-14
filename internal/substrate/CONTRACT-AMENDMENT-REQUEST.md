---
title: Contract Amendment Request — Story 1.3 primordial fixed-ID compliance with Contract #1
raised_by: Story 1.4 implementing engineer
raised_at: 2026-05-13
resolved_at: 2026-05-13
status: RESOLVED — Option A applied by Winston (parent session)
resolution: Primordial fixed IDs regenerated to be Contract #1-compliant
severity: was BLOCKING (for partial Story 1.4 refactor scope)
---

# Contract Amendment Request — Primordial Fixed-ID Compliance with Contract #1

## Resolution (Winston, 2026-05-13)

**Option A applied.** All twelve primordial fixed NanoIDs in
`internal/bootstrap/nanoid.go` were regenerated via `substrate.NewNanoID()`
(the runtime generator that produces Contract #1-compliant 20-char IDs
from the canonical 58-char alphabet). The new IDs are committed in the
same change as the Story 1.4 substrate package.

**Verification after fix:**
- `go build ./...` → 0
- `go vet ./...` → 0
- `go test ./internal/substrate/...` → 0 (10K NanoID test still passes)
- `make down && make up && make verify-bootstrap && make down` → 29/29 assertions PASS

**Link directionality convention shift:** With the OLD IDs, Story 1.3's
code commented that "alphabetical NanoID order" was the tiebreaker for
"younger first" when all primordial entries share the same createdAt.
That alphabetical-tiebreaker convention was a Story 1.3 invention not
present in Contract #1. With the new randomly-generated IDs the
alphabetical orderings would have flipped some links (e.g.,
PlatformActorID `49GPDXyb…` is alphabetically before
RolePlatformIntlID `Htdzfjz…`).

To keep link construction stable and readable, the convention was
shifted to a **category-based tiebreaker**: identities and permissions
are conventionally "younger" than roles for primordial entities. This
is documented in `internal/bootstrap/nanoid.go` package doc. Real
entities will have distinct `createdAt` timestamps and won't need any
tiebreaker.

**Note on bootstrap key construction migration to substrate helpers:**
With Contract #1-compliant primordial IDs, the bootstrap could now
adopt `substrate.VertexKey`/`AspectKey`/`LinkKey` without triggering
the validator panic. That migration was *not* done in this fix to keep
the change minimal and the diff reviewable. It is a clean follow-up
candidate — recommended as part of a future "bootstrap-substrate
alignment" cleanup commit, or absorbed by a downstream story that
touches bootstrap key construction.

---

## Original Request (preserved for audit)

### Summary

The fixed primordial NanoIDs declared in `internal/bootstrap/nanoid.go`
(Story 1.3) did not comply with Contract #1's NanoID specification.
Story 1.4's substrate package was written to match Contract #1; its
`IsValidNanoID` validator correctly rejected all twelve primordial IDs,
and `substrate.VertexKey(...)` (which panics on invalid IDs) could not
be used to construct keys from those constants.

### The contract

Contract #1 §1.1: "`<id>` — a NanoID generated per the architecture's
locked specification … **20 characters drawn from a custom 58-character
alphabet that excludes visually ambiguous characters** (`I`, `l`, `O`,
`0`)."

### The violation (now resolved)

Twelve constants were audited. All twelve violated the alphabet
constraint; seven of twelve also violated the 20-character length
constraint. Detailed table preserved in commit history; the offending
constants have been replaced by the compliant generated IDs now
present in `internal/bootstrap/nanoid.go`.

### Process note

The Story 1.4 implementing agent correctly raised this amendment rather
than silently fixing the constants (which would have violated the brief's
"do NOT replace the bootstrap's fixed-ID constants" instruction). This
is the right process; Winston ratified Option A and applied the fix
directly in the parent session.
