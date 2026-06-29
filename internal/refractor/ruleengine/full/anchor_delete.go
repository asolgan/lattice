package full

import (
	"context"

	"github.com/asolgan/lattice/internal/refractor/ruleengine"
)

// AnchorDeleteResult reports the projection (delete) key that a now-tombstoned
// event vertex previously projected to, for a root-tombstone CDC event on a
// plain (non-actor-aware) projection lens. It mirrors the simple engine's
// deleteResult and the actor-aware pipeline's tombstone shortcut: a soft-deleted
// anchor must retract the row it projected, which the upsert-only full-engine
// re-scan path otherwise leaves stale (the scan returns zero rows for the
// tombstoned anchor but never a Delete).
//
// eventKey/eventType/eventProps describe the tombstoned vertex (the CDC event):
// eventKey is its Core KV key, eventType its Contract #1 vertex type, eventProps
// its stored root body.
//
// The delete key is resolved over EVERY key column (the rule's threaded
// Into.Key, mirroring the upsert path; the legacy single first-RETURN-item key
// when no columns are threaded), evaluated against a read-free binding of the
// tombstoned anchor — so a composite-key lens (e.g. a GrantTable lens keyed on
// (actor_id, anchor_id, grant_source)) retracts the exact row it projected, and
// a function-call key like nanoIdFromKey(identity.key) resolves identically to
// the upsert path with no re-scan of the now-deleted vertex.
//
//	ok == false → the event vertex is NOT this rule's anchor label (a
//	              secondary-node tombstone — the caller must re-execute so
//	              dependent rows refresh), the rule lacks a resolvable
//	              anchor/RETURN, or some key column cannot be resolved without a
//	              Core-KV read (e.g. an aspect field absent from a root-tombstone
//	              payload — an anti-pattern) or resolves to a node rather than a
//	              scalar. No Delete is emitted; the caller falls through to a
//	              re-execute.
//	ok == true  → keys is the complete Keys map to hand to a Delete EvalResult,
//	              mirroring the upsert key map (every key column → its value).
func (*Engine) AnchorDeleteResult(
	cr ruleengine.CompiledRule, eventKey, eventType string, eventProps map[string]any,
) (keys map[string]any, ok bool) {
	compiled, isFull := cr.(*CompiledRule)
	if !isFull || compiled == nil || compiled.Query == nil {
		return nil, false
	}
	q := compiled.Query

	// Anchor = the first MATCH pattern's first node. Its label discriminates an
	// anchor tombstone (retract) from a secondary-node tombstone (re-execute):
	// a provider/appointment tombstone is the anchor; a patient tombstone
	// reaching the appointment lens via forPatient is a secondary node.
	anchorVar, anchorLabel, found := anchorNode(q)
	if !found || anchorLabel == "" || eventType != anchorLabel {
		return nil, false
	}

	// Key columns: the threaded Into.Key (multi-column composite), else the
	// legacy first-RETURN-item alias (single-key behaviour, unchanged for any
	// un-threaded caller). Mirrors applyReturn's key construction.
	cols := compiled.KeyColumns
	if len(cols) == 0 {
		first, ok := firstReturnItem(q)
		if !ok {
			return nil, false
		}
		alias := first.Alias
		if alias == "" {
			alias = projectionAutoAlias(first.Expr, 0)
		}
		cols = []string{alias}
	}

	exprByAlias := returnExprByAlias(q)

	// A read-free executor binding the anchor var to its tombstoned vertex. A nil
	// coreKV makes any key expression that needs an aspect point-read report
	// unresolvable (errCoreKVReadDisabled) instead of re-scanning the now-deleted
	// vertex; every other shape (literal, anchor .key / root field, pure function
	// over them — e.g. nanoIdFromKey) resolves exactly as the upsert path does.
	ex := &executor{ctx: context.Background()}
	b := binding{anchorVar: &nodeRef{key: eventKey, props: eventProps}}

	out := make(map[string]any, len(cols))
	for _, col := range cols {
		expr, present := exprByAlias[col]
		if !present {
			// A key column that is not a RETURN alias is an anti-pattern caught at
			// activation; defensively fall through rather than emit a partial key.
			return nil, false
		}
		v, err := ex.evalExpr(b, expr)
		if err != nil {
			// Needs a Core-KV read (aspect access) or otherwise unresolvable —
			// conservative fall-through to a re-execute, never a wrong Delete.
			return nil, false
		}
		if _, isNode := v.(*nodeRef); isNode {
			// A bare node variable is not a scalar key value (the upsert path would
			// project a degenerate key) — fall through.
			return nil, false
		}
		out[col] = v
	}
	return out, true
}

// anchorNode returns the variable + label of the first MATCH clause's first
// node — the lens's anchor. ok is false when the query has no MATCH or its
// first pattern carries no node (neither occurs for a compiled lens).
func anchorNode(q *Query) (variable, label string, ok bool) {
	for _, c := range q.Clauses {
		m, isMatch := c.(*Match)
		if !isMatch {
			continue
		}
		if len(m.Patterns) == 0 || len(m.Patterns[0].Nodes) == 0 {
			return "", "", false
		}
		n := m.Patterns[0].Nodes[0]
		return n.Variable, n.Label, true
	}
	return "", "", false
}

// firstReturnItem returns the first projection item of the RETURN clause — the
// item the executor treats as the output key column when no key columns are
// threaded.
func firstReturnItem(q *Query) (ProjectionItem, bool) {
	for _, c := range q.Clauses {
		r, isReturn := c.(*Return)
		if !isReturn {
			continue
		}
		if len(r.Items) == 0 {
			return ProjectionItem{}, false
		}
		return r.Items[0], true
	}
	return ProjectionItem{}, false
}

// returnExprByAlias maps each RETURN item's effective output alias (the explicit
// alias, else the auto-alias — matching applyReturn/projectItems) to its
// expression, so a key column named in Into.Key can be resolved to the
// expression that produces it.
func returnExprByAlias(q *Query) map[string]Expr {
	for _, c := range q.Clauses {
		r, isReturn := c.(*Return)
		if !isReturn {
			continue
		}
		out := make(map[string]Expr, len(r.Items))
		for i, item := range r.Items {
			alias := item.Alias
			if alias == "" {
				alias = projectionAutoAlias(item.Expr, i)
			}
			out[alias] = item.Expr
		}
		return out
	}
	return map[string]Expr{}
}
