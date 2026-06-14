package projection

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/refractor/adapter"
	"github.com/asolgan/lattice/internal/refractor/lens"
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

// InstallActorAggregate wires an actor-aggregate lens through the compiled
// ProjectionPlan: the §6.13 Output descriptor drives the on-wire envelope, the
// per-actor cross-vertex fan-out, the empty/delete-key behavior, and the §6.2
// guard predicate — all from lens-definition data, with no canonical-name
// knowledge. Returns false when the lens must NOT be registered (a fail-closed
// descriptor error), true once the components are installed.
//
// Fan-out uses the broad adjacency ActorEnumerator (the sound superset that can
// never miss an affected anchor). The compiled invalidation forest is the more
// precise alternative the plan also carries; the live pipeline does not yet
// consume it, so an auth-plane lens whose MATCH the forest compiler cannot prove
// subset-safe (e.g. the primary capability cypher's variable-length
// `containedIn*0..`) is still wired with the BFS enumerator rather than refused —
// BFS over-reprojects, never under-reprojects, so no anchor is ever missed.
func InstallActorAggregate(
	p *pipeline.Pipeline,
	adpt adapter.Adapter,
	r *lens.Rule,
	projectionRevision func(string) uint64,
	adjKV, coreKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	desc, err := ParseOutputDescriptor(r.Output)
	if err != nil {
		logger.Error("actor-aggregate output descriptor invalid — refusing registration",
			"lensId", r.ID, "err", err)
		return false
	}

	authPlane := IsAuthPlane(r)
	if _, cErr := Compile(r, logger); cErr != nil {
		var ce *CompileError
		if errors.As(cErr, &ce) {
			// Auth-plane lens whose MATCH the forest compiler cannot prove
			// subset-safe. The live fan-out is the broad BFS enumerator (the
			// sound superset), so registering with BFS is safe; refusing would
			// regress the security-plane projection. Log loudly and proceed.
			logger.Warn("actor-aggregate invalidation forest not subset-safe; using broad BFS fan-out",
				"lensId", r.ID, "reason", ce.Reason)
		} else {
			logger.Error("actor-aggregate plan compile failed — refusing registration",
				"lensId", r.ID, "err", cErr)
			return false
		}
	}

	lensDefKey := "vtx.meta." + r.ID
	p.SetEnvelopeFn(desc.EnvelopeFn(lensDefKey, projectionRevision))
	p.SetActorEnumerator(pipeline.NewActorEnumerator(adjKV, coreKV, desc.AnchorType))
	p.SetActorDeleteKey(desc.BuildKey)
	p.SetLatencyBuffer(pipeline.NewLatencyRingBuffer(pipeline.DefaultLatencyBufferSize))

	guarded := authPlane || desc.RequiresGuardedTombstone()
	if guarded {
		if gErr := EnableProjectionGuard(adpt, r.ID); gErr != nil {
			logger.Error("actor-aggregate guard", "lensId", r.ID, "err", gErr)
			return false
		}
	}

	logger.Info("actor-aggregate envelope + fan-out + delete-key + latency installed",
		"lensId", r.ID, "lensDefKey", lensDefKey,
		"anchorType", desc.AnchorType, "guarded", guarded, "authPlane", authPlane)
	return true
}

// EnableProjectionGuard turns on the monotonic projection-write guard for a
// NATS-KV-backed lens. The caller decides which lenses are guarded from the
// compiled plan predicate (auth-plane or empty-delete tombstone) and flips the
// flag here. The guarded lenses are security/correctness-plane, so an adapter
// that cannot enforce the guard (e.g. a Postgres target) is a fail-closed error,
// not a silent downgrade: a guarded lens running unguarded re-opens the
// resurrection window the guard exists to close.
func EnableProjectionGuard(adpt adapter.Adapter, lensID string) error {
	nkv, ok := adpt.(*adapter.NatsKVAdapter)
	if !ok {
		return fmt.Errorf("projection-write guard required for lens %s but target adapter cannot enforce it (not NATS-KV)", lensID)
	}
	nkv.SetGuarded(true)
	return nil
}
