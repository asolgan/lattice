package projection

import (
	"fmt"

	"github.com/asolgan/lattice/internal/refractor/pipeline"
	"github.com/asolgan/lattice/internal/substrate"
)

// EnvelopeFn builds the pipeline envelope wrapper that turns each per-actor
// RETURN row into the on-wire document the descriptor describes. It is the
// single data-driven replacement for the per-canonical-name capability
// envelope wrappers: one path, parameterized by the compiled OutputDescriptor.
//
// lensDefKey is the meta-lens vertex key (vtx.meta.<id>); revisionOf returns the
// current Core KV revision of a key (0 = unknown/absent). Both feed
// projectedFromRevisions via ContributingSources (§6.3, freshness: auto).
//
// Behavior, reproducing the built-in wrappers exactly:
//   - A row whose anchor actorKey is empty is declined (ErrSkipProjection) — it
//     is the degenerate aggregation row a cypher produces over zero anchor
//     bindings. The my-tasks wrapper additionally falls back to the bound
//     params["actorKey"] before declining, so a last-task-closed actor deletes
//     its key rather than leaving it stale; this driver does the same.
//   - A row whose anchor is not the descriptor's AnchorType is declined.
//   - The realness filter drops degenerate null-key collect entries from every
//     body column it applies to. When the empty behavior is delete/softDelete
//     and every realness-filtered body column is empty, the row is declined with
//     ErrDeleteProjection keyed at BuildKey(actorKey).
//   - Otherwise the envelope is {key, <actorField>: actorKey, version,
//     projectedAt, projectedFromRevisions, [lanes], <bodyColumns...>,
//     <staticEmptyColumns...: []>}.
func (d OutputDescriptor) EnvelopeFn(lensDefKey string, revisionOf func(string) uint64) pipeline.EnvelopeFn {
	return func(row map[string]any, keys map[string]any, params map[string]any) (map[string]any, map[string]any, error) {
		actorKey, _ := row["actorKey"].(string)
		if actorKey == "" {
			actorKey, _ = params["actorKey"].(string)
		}
		if actorKey == "" {
			return nil, nil, pipeline.ErrSkipProjection
		}
		vtxType, _, ok := substrate.ParseVertexKey(actorKey)
		if !ok {
			return nil, nil, fmt.Errorf("projection: actorKey %q is not a Contract #1 vertex key", actorKey)
		}
		if vtxType != d.AnchorType {
			return nil, nil, pipeline.ErrSkipProjection
		}

		outKey := d.BuildKey(actorKey)

		// Realness-filter each body column and decide the empty-result action.
		filtered := make(map[string]any, len(d.BodyColumns))
		anyReal := false
		for _, col := range d.BodyColumns {
			vals := d.RealnessFiltered(row[col])
			if vals == nil {
				vals = []any{}
			}
			filtered[col] = vals
			if len(vals) > 0 {
				anyReal = true
			}
		}

		if !anyReal && d.RealnessFilter != "" {
			switch d.EmptyAction() {
			case ActionDelete, ActionSoftDelete:
				return nil, map[string]any{"key": outKey}, pipeline.ErrDeleteProjection
			case ActionSkip:
				return nil, nil, pipeline.ErrSkipProjection
			case ActionWriteEmptyDoc:
				// Fall through to build the envelope with every body column
				// already empty-after-realness — the key stays present with an
				// empty body, which is exactly the empty-doc behavior.
			}
		}

		envelope := map[string]any{
			"key":                    outKey,
			d.ActorField:             actorKey,
			"version":                Version,
			"projectedAt":            params["projectedAt"],
			"projectedFromRevisions": ContributingSources(actorKey, lensDefKey, []map[string]any{row}, revisionOf),
		}
		if len(d.Lanes) > 0 {
			envelope["lanes"] = append([]string(nil), d.Lanes...)
		}
		for _, col := range d.BodyColumns {
			envelope[col] = filtered[col]
		}
		for _, col := range d.StaticEmptyColumns {
			envelope[col] = []any{}
		}

		return envelope, map[string]any{"key": outKey}, nil
	}
}

// Version is the Capability KV envelope schema version (Contract #6 §6.3),
// pinned to "1.0" for Phase 1. Every actor-aggregate document carries it.
const Version = "1.0"
