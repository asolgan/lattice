# Hello Lattice — Example Files

This directory contains the reference implementation files for the Hello Lattice
tutorial.

See the root [README.md](../../README.md) section **Hello Lattice (60-minute tutorial)**
for the full step-by-step walkthrough, including Milestone 6 (Rollback the book DDL).

## Files

| File | Purpose |
|------|---------|
| `book-ddl.yaml` | Reference payload for the "book" DDL (Milestone 2) |
| `books-lens.yaml` | Reference payload for the "books" Lens (Milestone 4) |
| `ai-agent.go` | Standalone AI agent program for Milestone 5 |
| `Makefile` | `make demo` runs all milestones end-to-end |

## Quick start

```console
# From repo root — start infrastructure
make up

# Set actor key
export BOOTSTRAP_ACTOR_KEY=$(lattice graph keys vtx.identity. | head -1)

# Run all milestones
cd examples/hello-lattice
make demo
```

## Milestone 6: Rollback the book DDL

Milestone 6 demonstrates the compensation contract from Story 5.3. After
Milestone 5 creates a book via the AI agent, the operator rolls back the book
DDL itself:

1. Read `$BOOK_DDL_KEY.compensation` — returns `inverseOperationType: TombstoneMetaVertex`
   plus payload and revision templates.
2. Submit `TombstoneMetaVertex` with `expectedRevision` captured from the DDL's
   current Core KV entry.
3. Confirm `DiscoverDDL("book")` returns `ErrDDLNotFound`.
4. Confirm `.compensation` aspect now reads `inverseOperationType: none`.
5. Confirm a subsequent `CreateBook` is rejected.

Milestone 6 is terminal — after the book DDL is tombstoned, `CreateBook`
operations are rejected for the remainder of the session.

See the root README's **Milestone 6** section for the exact CLI commands.
