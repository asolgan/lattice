# Story 12.2 — Invalidation-compiler spike (NON-SHIPPING)

**Date:** 2026-06-14
**Status:** spike complete — GO with one mandatory design correction (see report)
**Package:** `internal/spike/invalidation-compiler` (`package invalidationcompiler`)

This is a throwaway, build-to-throw spike. It is **not** wired into the live
projection path and ships **no** production code. It imports production packages
read-only and modifies none of them.

## What it proves

A narrow invalidation compiler can derive the affected-anchor set from the real
full-engine openCypher AST of the two actor-aggregate lenses
(`capabilityEphemeral`, `myTasks`), and that derived set is a **sound subset** of
the broad `pipeline.ActorEnumerator` BFS — for vertex, link, and aspect CDC
events, on both lenses.

The correctness oracle (`equivalence_test.go`):

- **(a) subset** — the compiled affected-anchor set ⊆ the BFS set, strict
  (`compiled ⊊ BFS`) on at least one event per lens (a real over-reprojection
  eliminated);
- **(b) no missed anchor** — REAL reproject-and-diff: every actor in the BFS
  superset is reprojected through the production `full.Engine.ExecuteWith`
  before/after each fixture mutation; every actor whose projected output actually
  changes must be in the compiled set;
- **(c) the win** — `len(BFS) − len(compiled)` is recorded per event.

## The headline finding

The naive approach — flatten the full AST into one `simple.QueryPlan.Steps`
slice and reuse the verbatim simple-engine reverse walk — is **UNSOUND for
multi-branch lenses** (`capabilityEphemeral`). The simple reverse walk assumes a
single linear anchor→leaf chain; the delegation branch (`reportsTo` 2-hop)
interleaves with the direct branch in the flat slice, and a delegated-task
change drops the manager anchor (a **missed revocation**). The spike caught this
empirically (the no-missed-anchor diff failed), then fixed it in the **compiler**
layer (not the verbatim walk) by segmenting the AST into per-branch linear chains
and running the unchanged reverse walk over each. See the report's go/no-go.

## Files

- `compiler.go` — the NEW bit being proven: full AST → forest of per-branch
  linear `simple.QueryPlan`s.
- `reverse_copy.go` — VERBATIM copy of the simple engine's
  `reverseTraverse`/`walkBackToAnchor`/`filterEdges`/`reverseDirection` (spike
  only; 12.3 wires the real functions).
- `specs.go` — the two lens cypher strings, pinned VERBATIM from
  `packages/orchestration-base/lenses.go` (snapshot 2026-06-14).
- `equivalence_test.go` — the correctness oracle.

## Decision report + go/no-go

`docs/decisions/12.2-invalidation-compiler-spike-report.md`

## Run

    go test ./internal/spike/invalidation-compiler/... -count=1 -v
