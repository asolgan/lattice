package full

// ReferencedLabels returns the set of vertex-type labels a compiled query's
// patterns can bind — the types whose vertices, aspects, or links can affect
// the query's output. The plain pipeline uses it to skip aspect/link
// reprojection for events on types the lens cannot read (e.g. a `meta` aspect
// mutation never changes a `MATCH (b:book)` lens's rows), bounding the
// per-event re-execute cost to the lenses that can actually be affected.
//
// exhaustive == false means the set is NOT authoritative and the caller must
// treat every type as potentially relevant: an unlabeled node pattern that is
// not a re-reference to a variable labeled elsewhere binds any type, and a
// variable-length relationship traverses intermediate nodes of arbitrary
// type. Conservative by construction — when in doubt, reproject.
func (cr *CompiledRule) ReferencedLabels() (labels map[string]struct{}, exhaustive bool) {
	if cr == nil || cr.Query == nil {
		return nil, false
	}
	labels = make(map[string]struct{})
	exhaustive = true

	// Pass 1: every variable that carries a label ANYWHERE in the query —
	// an unlabeled `(u)` later is a re-reference to that binding, not a new
	// any-type node (`MATCH (u:unit)` … `MATCH (u)<-[:manages]-(l:identity)`).
	labeledVars := make(map[string]struct{})
	collectVars := func(p PathPattern) {
		for _, n := range p.Nodes {
			if n.Label != "" && n.Variable != "" {
				labeledVars[n.Variable] = struct{}{}
			}
		}
	}
	var collectVarsExpr func(e Expr)
	collectVarsExpr = func(e Expr) {
		switch x := e.(type) {
		case *PatternExpr:
			collectVars(x.Pattern)
		case *PatternComprehension:
			collectVars(x.Pattern)
			collectVarsExpr(x.Where)
			collectVarsExpr(x.Projection)
		case *Not:
			collectVarsExpr(x.Operand)
		case *AndOr:
			for _, op := range x.Operands {
				collectVarsExpr(op)
			}
		case *BinaryOp:
			collectVarsExpr(x.Left)
			collectVarsExpr(x.Right)
		}
	}
	for _, c := range cr.Query.Clauses {
		if m, isMatch := c.(*Match); isMatch {
			for _, p := range m.Patterns {
				collectVars(p)
			}
			collectVarsExpr(m.Where)
		}
	}

	// Pass 2: build the label set; an unlabeled node is exhaustive only as a
	// re-reference to a labeled variable.
	addPattern := func(p PathPattern) {
		for _, n := range p.Nodes {
			if n.Label == "" {
				if n.Variable == "" {
					exhaustive = false
					continue
				}
				if _, isRef := labeledVars[n.Variable]; !isRef {
					exhaustive = false
				}
				continue
			}
			labels[n.Label] = struct{}{}
		}
		for _, r := range p.Rels {
			if r.MinHops != 1 || r.MaxHops != 1 {
				// Variable-length: intermediate hops bind arbitrary types.
				exhaustive = false
			}
		}
	}
	var addExpr func(e Expr)
	addExpr = func(e Expr) {
		switch x := e.(type) {
		case nil:
		case *PropertyAccess:
			addExpr(x.Target)
		case *BinaryOp:
			addExpr(x.Left)
			addExpr(x.Right)
		case *AndOr:
			for _, op := range x.Operands {
				addExpr(op)
			}
		case *Not:
			addExpr(x.Operand)
		case *PatternExpr:
			addPattern(x.Pattern)
		case *PatternComprehension:
			addPattern(x.Pattern)
			addExpr(x.Where)
			addExpr(x.Projection)
		case *FunctionCall:
			for _, a := range x.Args {
				addExpr(a)
			}
		case *MapLiteral:
			for _, v := range x.Values {
				addExpr(v)
			}
		case *ListLiteral:
			for _, el := range x.Elements {
				addExpr(el)
			}
		case *CaseExpr:
			for _, alt := range x.Alternatives {
				addExpr(alt.When)
				addExpr(alt.Then)
			}
			addExpr(x.Else)
		}
	}
	for _, c := range cr.Query.Clauses {
		switch cl := c.(type) {
		case *Match:
			for _, p := range cl.Patterns {
				addPattern(p)
			}
			addExpr(cl.Where)
		case *With:
			for _, it := range cl.Items {
				addExpr(it.Expr)
			}
			addExpr(cl.Where)
		case *Return:
			for _, it := range cl.Items {
				addExpr(it.Expr)
			}
		}
	}
	return labels, exhaustive
}
