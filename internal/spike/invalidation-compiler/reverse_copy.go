// VERBATIM copy of internal/refractor/ruleengine/simple/{evaluator,plan}.go
// (snapshot 2026-06-14) — spike only; 12.3 wires the real functions.
//
// reverseTraverse / walkBackToAnchor / filterEdges / reverseDirection and the
// helpers they call (adjLookupID, otherCoreKey) are copied byte-for-byte from
// package simple so the spike exercises the EXACT production reverse-walk, not
// a reimplementation. Divergence would make the spike prove the wrong thing.
//
// The copied functions reference simple.EdgeDirection / simple.Outbound etc.
// and simple.QueryPlan via the import below, so the compiled plan type is the
// real production type — only the unexported function bodies are duplicated.
// 12.3's production wiring exports/relocates these real functions (or moves the
// compiler into package simple) and does NOT ship this copy.
package invalidationcompiler

import (
	"context"
	"fmt"

	"github.com/asolgan/lattice/internal/refractor/adjacency"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/simple"
	"github.com/asolgan/lattice/internal/substrate"
)

// NodeEntry is the changed-node input to the reverse walk. VERBATIM field set
// from simple.NodeEntry (the fields the reverse walk reads).
type NodeEntry struct {
	CoreKVKey string
	NodeLabel string
}

// adjLookupID extracts the bare NodeID from a Core KV key when it is a
// valid Contract #1 vtx.<type>.<id> shape; otherwise it returns the key
// verbatim so legacy / Materializer-style fixtures still work.
func adjLookupID(key string) string {
	if _, id, ok := substrate.ParseVertexKey(key); ok {
		return id
	}
	return key
}

// otherCoreKey reconstructs the OTHER endpoint's Core KV key from an
// EdgeEntry. If the edge carries an OtherType (Contract #1 link
// convention), it builds the full vtx key; otherwise it returns the bare
// OtherNodeID (Materializer-style legacy path).
func otherCoreKey(otherType, otherNodeID string) string {
	if otherType != "" {
		return substrate.VertexPrefix + "." + otherType + "." + otherNodeID
	}
	return otherNodeID
}

// reverseTraverse finds all anchor Core KV keys affected by a change to a non-anchor node.
// It walks backward through the traversal steps from the changed node's label to the anchor.
func reverseTraverse(ctx context.Context, plan *simple.QueryPlan, entry NodeEntry, adjKV *substrate.KV) ([]string, error) {
	seen := map[string]struct{}{}

	for stepIdx, step := range plan.Steps {
		if step.ToLabel != entry.NodeLabel {
			continue
		}
		// Walk backward from entry through steps [stepIdx, ..., 0] to reach the anchor.
		keys, err := walkBackToAnchor(ctx, plan, stepIdx, []string{entry.CoreKVKey}, adjKV)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			seen[k] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	return result, nil
}

// walkBackToAnchor recursively walks backward through plan.Steps[0..stepIdx] starting
// from startKeys, reversing each hop, until it reaches the anchor nodes at step 0.
func walkBackToAnchor(ctx context.Context, plan *simple.QueryPlan, stepIdx int, startKeys []string, adjKV *substrate.KV) ([]string, error) {
	step := plan.Steps[stepIdx]
	reverseDir := reverseDirection(step.Direction)

	var prevKeys []string
	for _, nodeKey := range startKeys {
		adjID := adjLookupID(nodeKey)
		neighbors, err := adjacency.Neighbors(ctx, adjKV, adjID)
		if err != nil {
			return nil, fmt.Errorf("walkBack: neighbors(%s): %w", adjID, err)
		}
		matching := filterEdges(neighbors, step.EdgeType, reverseDir)
		for _, edge := range matching {
			prevKeys = append(prevKeys, otherCoreKey(edge.OtherType, edge.OtherNodeID))
		}
	}

	if len(prevKeys) == 0 {
		return nil, nil
	}

	if stepIdx == 0 {
		// Reached anchor level.
		return prevKeys, nil
	}

	return walkBackToAnchor(ctx, plan, stepIdx-1, prevKeys, adjKV)
}

// filterEdges returns only the edges matching edgeType and direction.
func filterEdges(edges []adjacency.EdgeEntry, edgeType string, dir simple.EdgeDirection) []adjacency.EdgeEntry {
	var result []adjacency.EdgeEntry
	for _, e := range edges {
		if e.Name != edgeType {
			continue
		}
		switch dir {
		case simple.Outbound:
			if e.Direction == "outbound" {
				result = append(result, e)
			}
		case simple.Inbound:
			if e.Direction == "inbound" {
				result = append(result, e)
			}
		case simple.Both:
			result = append(result, e)
		}
	}
	return result
}

// reverseDirection returns the logical opposite direction for reverse-edge lookup.
func reverseDirection(dir simple.EdgeDirection) simple.EdgeDirection {
	switch dir {
	case simple.Outbound:
		return simple.Inbound
	case simple.Inbound:
		return simple.Outbound
	default:
		return simple.Both
	}
}
