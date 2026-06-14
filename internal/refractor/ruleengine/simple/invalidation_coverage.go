package simple

import (
	"fmt"

	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
)

// CoverageResult reports whether an actor-aggregate lens's MATCH patterns use
// only constructs the narrow invalidation compiler can prove subset-safe. When
// Covered is false, Reason names the first uncovered construct found; the
// activation policy then fails closed (auth plane) or falls back to broad BFS
// (correctness plane).
type CoverageResult struct {
	Covered bool
	Reason  string
}

// AnalyzeInvalidationCoverage parses a lens cypher body and reports whether its
// MATCH patterns stay within the subset-safe construct set the per-branch
// forest compiler can derive a sound invalidation plan from.
//
// Covered (subset-safe): MATCH / OPTIONAL MATCH; node labels (with
// re-referenced-anchor backfill); rel names and directed hops; narrowing WHERE
// (ignored — dropping it only enlarges the affected set toward the BFS
// superset); path-preserving WITH; collect()/+ in RETURN (shapes output, not
// reachability).
//
// NOT covered (⇒ fail closed on the auth plane): undirected hops (DirBoth);
// broadening WHERE that asserts existence of OTHER paths (pattern predicates,
// e.g. `WHERE (a)-[:t]->(b)`, including negated forms); aggregation in WITH that
// could change the source set feeding a later MATCH.
//
// UNION, standalone CALL, and subqueries never reach here: the full engine's
// parser rejects them outright, so a parse error is surfaced as uncovered.
func AnalyzeInvalidationCoverage(body string) (CoverageResult, error) {
	eng := full.New()
	cr, err := eng.Parse(body)
	if err != nil {
		return CoverageResult{Covered: false, Reason: fmt.Sprintf("parse failed: %v", err)}, err
	}
	compiled, ok := cr.(*full.CompiledRule)
	if !ok {
		return CoverageResult{}, fmt.Errorf("coverage: expected *full.CompiledRule, got %T", cr)
	}
	q := compiled.Query
	if q == nil {
		return CoverageResult{}, fmt.Errorf("coverage: nil query")
	}
	return analyzeCoverage(q), nil
}

func analyzeCoverage(q *full.Query) CoverageResult {
	sawAnyMatchAfterReducingWith := false
	for _, clause := range q.Clauses {
		switch c := clause.(type) {
		case *full.Match:
			for _, p := range c.Patterns {
				for _, rel := range p.Rels {
					if rel.Direction == full.DirBoth {
						return CoverageResult{Covered: false,
							Reason: fmt.Sprintf("undirected hop on relationship %q is not subset-safe", rel.Type)}
					}
					if rel.MinHops != 1 || rel.MaxHops != 1 {
						return CoverageResult{Covered: false,
							Reason: fmt.Sprintf("variable-length hop on relationship %q (%d..%d) is not subset-safe; the reverse walk explores a single fixed hop", rel.Type, rel.MinHops, rel.MaxHops)}
					}
				}
			}
			if r := coverageOfWhere(c.Where); !r.Covered {
				return r
			}
			if sawAnyMatchAfterReducingWith {
				return CoverageResult{Covered: false,
					Reason: "aggregation in WITH feeds a later MATCH (source set may change)"}
			}
		case *full.With:
			if withReducesBindings(c) {
				sawAnyMatchAfterReducingWith = true
			}
			if r := coverageOfWhere(c.Where); !r.Covered {
				return r
			}
		case *full.Return:
			if r := coverageOfReturn(c); !r.Covered {
				return r
			}
		}
	}
	return analyzeConnectivity(q)
}

// coverageOfReturn reports a RETURN clause as uncovered when any projection item
// embeds a pattern (a pattern comprehension or pattern existence expression).
// Such an expression reads a path that appears in no MATCH, so the
// anchor-rooted reverse walk cannot see a change to that path — fail closed.
// Plain collect()/+ over MATCH-bound variables only shapes output and is safe.
func coverageOfReturn(r *full.Return) CoverageResult {
	for _, item := range r.Items {
		if exprHasPattern(item.Expr) {
			return CoverageResult{Covered: false,
				Reason: "RETURN embeds a pattern comprehension reading a path not in any MATCH — not subset-safe"}
		}
	}
	return CoverageResult{Covered: true}
}

// analyzeConnectivity verifies every traversal step is reachable from the anchor
// variable along a forward chain (FromVariable→ToVariable), and that no
// anchor-rooted chain is so long it would be truncated by the forest compiler's
// MaxBranchLen cap. A disconnected / non-anchor-rooted step means the forest
// would silently drop that pattern (its leaf changes would not invalidate the
// anchor); a truncated chain means the emitted branch under-covers. Either ⇒
// fail closed on the auth plane.
//
// An anchor-only lens (a single bound anchor MATCH with no relationships) has no
// traversal steps and is SOUND: only the anchor's own vertex change matters and
// that is handled by Execution, not by the reverse walk. It is covered.
func analyzeConnectivity(q *full.Query) CoverageResult {
	var steps []connStep
	anchorVar := ""
	anchorFound := false
	for _, clause := range q.Clauses {
		m, ok := clause.(*full.Match)
		if !ok {
			continue
		}
		for _, p := range m.Patterns {
			for i := range p.Rels {
				steps = append(steps, connStep{
					from: p.Nodes[i].Variable,
					to:   p.Nodes[i+1].Variable,
				})
			}
			if !anchorFound && !m.Optional && len(p.Nodes) > 0 {
				anchorVar = p.Nodes[0].Variable
				anchorFound = true
			}
		}
	}
	if !anchorFound {
		return CoverageResult{Covered: false,
			Reason: "query has no required MATCH to anchor on"}
	}
	if len(steps) == 0 {
		// Anchor-only lens: sound (handled by Execution), no reverse walk needed.
		return CoverageResult{Covered: true}
	}

	byFrom := map[string][]connStep{}
	for _, s := range steps {
		byFrom[s.from] = append(byFrom[s.from], s)
	}

	reached := map[string]bool{}
	truncated := false
	var walk func(tipVar string, depth int)
	walk = func(tipVar string, depth int) {
		if depth >= MaxBranchLen {
			if len(byFrom[tipVar]) > 0 {
				// More hops remain past the cap: the emitted branch is truncated.
				truncated = true
			}
			return
		}
		for _, s := range byFrom[tipVar] {
			reached[stepKey(s)] = true
			walk(s.to, depth+1)
		}
	}
	walk(anchorVar, 0)

	for _, s := range steps {
		if !reached[stepKey(s)] {
			return CoverageResult{Covered: false,
				Reason: fmt.Sprintf("MATCH step %s→%s is not reachable from the anchor variable %q (disconnected pattern)", s.from, s.to, anchorVar)}
		}
	}
	if truncated {
		return CoverageResult{Covered: false,
			Reason: fmt.Sprintf("a MATCH chain exceeds the %d-hop branch cap and would be truncated (under-covering branch)", MaxBranchLen)}
	}
	return CoverageResult{Covered: true}
}

type connStep struct{ from, to string }

func stepKey(s connStep) string { return s.from + "\x00" + s.to }

// coverageOfWhere reports a WHERE clause as uncovered when it embeds a pattern
// existence predicate (a pattern in the expression tree), which asserts the
// existence of OTHER paths and can broaden the matched set beyond the
// anchor-rooted traversal. Plain comparison/boolean predicates only narrow and
// are safe (ignored by the compiler).
func coverageOfWhere(e full.Expr) CoverageResult {
	if e == nil {
		return CoverageResult{Covered: true}
	}
	if exprHasPattern(e) {
		return CoverageResult{Covered: false,
			Reason: "WHERE asserts existence of another path (pattern predicate) — not subset-safe"}
	}
	return CoverageResult{Covered: true}
}

// exprHasPattern reports whether an expression tree embeds a PathPattern (a
// pattern predicate or pattern comprehension), which the invalidation compiler
// cannot bound.
func exprHasPattern(e full.Expr) bool {
	switch x := e.(type) {
	case *full.PatternExpr:
		return true
	case *full.PatternComprehension:
		return true
	case *full.Not:
		return exprHasPattern(x.Operand)
	case *full.AndOr:
		for _, op := range x.Operands {
			if exprHasPattern(op) {
				return true
			}
		}
	case *full.BinaryOp:
		return exprHasPattern(x.Left) || exprHasPattern(x.Right)
	case *full.FunctionCall:
		for _, a := range x.Args {
			if exprHasPattern(a) {
				return true
			}
		}
	case *full.ListLiteral:
		for _, el := range x.Elements {
			if exprHasPattern(el) {
				return true
			}
		}
	case *full.MapLiteral:
		for _, v := range x.Values {
			if exprHasPattern(v) {
				return true
			}
		}
	}
	return false
}

// withReducesBindings reports whether a WITH clause aggregates (a collect/count
// or similar in a projection item), which can collapse the bound set so a
// subsequent MATCH operates on a changed source set. A pass-through WITH that
// merely carries variables forward is path-preserving and safe.
func withReducesBindings(w *full.With) bool {
	for _, item := range w.Items {
		if exprHasAggregation(item.Expr) {
			return true
		}
	}
	return false
}

func exprHasAggregation(e full.Expr) bool {
	switch x := e.(type) {
	case *full.FunctionCall:
		switch x.Name {
		case "collect", "count", "sum", "avg", "min", "max":
			return true
		}
		for _, a := range x.Args {
			if exprHasAggregation(a) {
				return true
			}
		}
	case *full.BinaryOp:
		return exprHasAggregation(x.Left) || exprHasAggregation(x.Right)
	case *full.AndOr:
		for _, op := range x.Operands {
			if exprHasAggregation(op) {
				return true
			}
		}
	case *full.Not:
		return exprHasAggregation(x.Operand)
	case *full.ListLiteral:
		for _, el := range x.Elements {
			if exprHasAggregation(el) {
				return true
			}
		}
	case *full.MapLiteral:
		for _, v := range x.Values {
			if exprHasAggregation(v) {
				return true
			}
		}
	}
	return false
}
