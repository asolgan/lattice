package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asolgan/lattice/internal/refractor/ruleengine"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/simple"
)

// ErrSkipProjection signals that an EnvelopeFn declined a row — the
// pipeline drops it without writing or erroring. Story 3.2a: used by
// the Capability envelope to suppress projections that the cypher
// produced via aggregation over zero MATCH-bindings (no real actor).
var ErrSkipProjection = errors.New("pipeline: envelope: skip projection")

// evaluateForEntry runs the per-engine evaluate path against entry and
// returns the normalised []simple.EvalResult shape the existing write
// loop expects. Story 3.2a — C1 convergence (Decision #2): the simple
// engine path delegates to simple.Evaluate; the full engine path binds
// `$actorKey`, `$now`, `$projectedAt` from the event/clock and calls
// full.Engine.ExecuteWith. When an EnvelopeFn is installed, each row
// is rewritten before being handed to the adapter.
func (p *Pipeline) evaluateForEntry(ctx context.Context, entry simple.NodeEntry) ([]simple.EvalResult, error) {
	switch p.engineKind {
	case ruleengine.EngineFull:
		if p.fullEngine == nil || p.fullCR == nil {
			return nil, fmt.Errorf("pipeline: full engine selected but engine/compiled rule unset for rule %q", p.ruleID)
		}

		// Soft-delete on the anchor short-circuits to a Delete projection
		// without invoking the engine; this matches simple.Evaluate's
		// AC behaviour for anchor deletions and avoids relying on the
		// full engine's cypher rule to produce empty rows for deleted
		// actors. The Keys map uses the anchor's full vertex key under
		// the column alias "key" — adapter wrappers (envelope or the
		// raw nats_kv path) read by alias.
		if entry.IsDeleted {
			return []simple.EvalResult{{
				Delete: true,
				Keys:   map[string]any{"key": entry.CoreKVKey},
				Row:    nil,
			}}, nil
		}

		now := time.Now().UTC()
		params := map[string]any{
			"actorKey":    entry.CoreKVKey,
			"now":         now.Format(time.RFC3339),
			"projectedAt": now.Format(time.RFC3339),
		}
		out, err := p.fullEngine.ExecuteWith(ctx, p.fullCR,
			ruleengine.EventContext{
				NodeKey:    entry.CoreKVKey,
				NodeProps:  entry.Properties,
				Parameters: params,
			}, p.adjKV, p.coreKV)
		if err != nil {
			return nil, err
		}
		results := make([]simple.EvalResult, 0, len(out))
		for _, r := range out {
			row := r.Values
			keys := r.Key
			if p.envelopeFn != nil {
				newRow, newKeys, envErr := p.envelopeFn(row, keys, params)
				if errors.Is(envErr, ErrSkipProjection) {
					continue
				}
				if envErr != nil {
					return nil, fmt.Errorf("pipeline: envelope: %w", envErr)
				}
				row = newRow
				keys = newKeys
			}
			results = append(results, simple.EvalResult{
				Delete: r.Delete,
				Keys:   keys,
				Row:    row,
			})
		}
		return results, nil

	default:
		// Simple engine — unchanged behaviour modulo optional envelope
		// wrap (Phase C may install one for capability lenses authored
		// against the simple engine; in practice the seeded capability
		// lenses use the full engine, so this path stays a no-op for
		// 3.2a).
		results, err := simple.Evaluate(ctx, p.currentPlan(), entry, p.adjKV, p.coreKV)
		if err != nil {
			return nil, err
		}
		if p.envelopeFn == nil {
			return results, nil
		}
		params := map[string]any{
			"actorKey":    entry.CoreKVKey,
			"projectedAt": time.Now().UTC().Format(time.RFC3339),
		}
		filtered := results[:0]
		for i := range results {
			if results[i].Delete {
				filtered = append(filtered, results[i])
				continue
			}
			newRow, newKeys, envErr := p.envelopeFn(results[i].Row, results[i].Keys, params)
			if errors.Is(envErr, ErrSkipProjection) {
				continue
			}
			if envErr != nil {
				return nil, fmt.Errorf("pipeline: envelope: %w", envErr)
			}
			results[i].Row = newRow
			results[i].Keys = newKeys
			filtered = append(filtered, results[i])
		}
		return filtered, nil
	}
}
