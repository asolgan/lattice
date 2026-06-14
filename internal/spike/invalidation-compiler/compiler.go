package invalidationcompiler

import (
	"fmt"

	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/simple"
)

// CompilePlan parses a lens cypher body through the production full engine and
// compiles the resulting full AST into a FOREST of per-branch linear
// simple.QueryPlans, each a single anchor→leaf chain. This is the NEW capability
// the spike proves: the simple engine's own Compile consumes a *simple.Query,
// never the full AST.
//
// WHY A FOREST, NOT ONE FLAT PLAN (the spike's headline finding):
// The simple engine's reverse walk (walkBackToAnchor) assumes plan.Steps is a
// SINGLE linear chain — from a matched leaf at index i it recurses contiguously
// i→i-1→…→0. capabilityEphemeral has TWO disjoint branches off the anchor (the
// direct assignedTo path and the reportsTo→assignedTo delegation path). Flattened
// into one Steps slice they interleave, and a leaf in the delegation branch
// (task2) wrongly continues its reverse walk back through the direct branch's
// steps, dropping the manager anchor — a MISSED ANCHOR (a missed revocation on an
// auth lens). The fix lives in the COMPILER (the new bit), not the verbatim
// reverse walk: segment the AST into per-branch linear chains by following the
// From/To variable bindings from the anchor, then run the unchanged reverse walk
// over EACH branch and union. Each branch's Steps are contiguous, so the verbatim
// walkBackToAnchor is correct within it.
//
// WHERE clauses are deliberately ignored: every WHERE on these two lenses only
// NARROWS the matched set, so dropping it can only enlarge the affected-anchor
// set toward (never past) the BFS superset — it can never miss an anchor (see the
// spike report's WHERE subset-safety argument). collect()/+ aggregation in RETURN
// is irrelevant to invalidation (it shapes output, not reachability).
func CompilePlan(body string) ([]*simple.QueryPlan, error) {
	eng := full.New()
	cr, err := eng.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("compile: parse: %w", err)
	}
	compiled, ok := cr.(*full.CompiledRule)
	if !ok {
		return nil, fmt.Errorf("compile: expected *full.CompiledRule, got %T", cr)
	}
	q := compiled.Query
	if q == nil {
		return nil, fmt.Errorf("compile: nil query")
	}

	// 1. Flatten every MATCH pattern hop into a TraversalStep (ordered).
	var steps []simple.TraversalStep
	var anchorLabel, anchorVar string
	anchorFound := false
	for _, clause := range q.Clauses {
		m, ok := clause.(*full.Match)
		if !ok {
			continue
		}
		for _, p := range m.Patterns {
			for i := range p.Rels {
				rel := p.Rels[i]
				dir, derr := mapDirection(rel.Direction)
				if derr != nil {
					return nil, derr
				}
				steps = append(steps, simple.TraversalStep{
					FromVariable: p.Nodes[i].Variable,
					FromLabel:    p.Nodes[i].Label,
					EdgeType:     rel.Type,
					Direction:    dir,
					ToVariable:   p.Nodes[i+1].Variable,
					ToLabel:      p.Nodes[i+1].Label,
					Optional:     m.Optional,
				})
			}
			if !anchorFound && !m.Optional && len(p.Nodes) > 0 {
				anchorLabel = p.Nodes[0].Label
				anchorVar = p.Nodes[0].Variable
				anchorFound = true
			}
		}
	}
	if !anchorFound {
		return nil, fmt.Errorf("compile: query has no required MATCH to anchor on")
	}

	// 2. Resolve each step's FromLabel from the binding that introduced its
	//    FromVariable (a re-referenced anchor like `(identity)` in the delegation
	//    MATCH carries no inline label, so its label must be recovered).
	labelOf := map[string]string{anchorVar: anchorLabel}
	for _, s := range steps {
		if s.ToLabel != "" {
			labelOf[s.ToVariable] = s.ToLabel
		}
		if s.FromLabel != "" {
			labelOf[s.FromVariable] = s.FromLabel
		}
	}

	// 3. Build branches: every root-to-leaf path in the traversal forest rooted
	//    at the anchor variable, following From→To variable chaining. A step
	//    extends a branch when its FromVariable equals the branch's current tip.
	branches := buildBranches(steps, anchorVar, labelOf)
	if len(branches) == 0 {
		return nil, fmt.Errorf("compile: no traversal branch reaches the anchor")
	}

	plans := make([]*simple.QueryPlan, 0, len(branches))
	for _, b := range branches {
		plans = append(plans, &simple.QueryPlan{
			AnchorLabel:    anchorLabel,
			AnchorVariable: anchorVar,
			Steps:          b,
		})
	}
	return plans, nil
}

// buildBranches expands the flat step list into linear anchor→leaf chains. Each
// branch is a contiguous chain whose first step starts at the anchor variable and
// each subsequent step's FromVariable equals the previous step's ToVariable. A
// fork (two steps sharing a FromVariable) yields two branches. FromLabel is
// backfilled from labelOf so reverse walks on re-referenced anchors work.
func buildBranches(steps []simple.TraversalStep, anchorVar string, labelOf map[string]string) [][]simple.TraversalStep {
	// Index steps by their FromVariable so we can extend a branch by tip.
	byFrom := map[string][]simple.TraversalStep{}
	for _, s := range steps {
		s.FromLabel = labelOf[s.FromVariable]
		if s.ToLabel == "" {
			s.ToLabel = labelOf[s.ToVariable]
		}
		byFrom[s.FromVariable] = append(byFrom[s.FromVariable], s)
	}

	const maxBranchLen = 10 // mirrors pipeline.DefaultActorMaxDepth; cycle guard.

	var branches [][]simple.TraversalStep
	var walk func(tipVar string, prefix []simple.TraversalStep)
	walk = func(tipVar string, prefix []simple.TraversalStep) {
		if len(prefix) >= maxBranchLen {
			if len(prefix) > 0 {
				branch := make([]simple.TraversalStep, len(prefix))
				copy(branch, prefix)
				branches = append(branches, branch)
			}
			return
		}
		nexts := byFrom[tipVar]
		if len(nexts) == 0 {
			if len(prefix) > 0 {
				branch := make([]simple.TraversalStep, len(prefix))
				copy(branch, prefix)
				branches = append(branches, branch)
			}
			return
		}
		for _, s := range nexts {
			extended := make([]simple.TraversalStep, len(prefix), len(prefix)+1)
			copy(extended, prefix)
			extended = append(extended, s)
			walk(s.ToVariable, extended)
		}
	}
	walk(anchorVar, nil)
	return branches
}

// mapDirection maps a full.Direction to the simple engine's EdgeDirection. The
// mapping is load-bearing: the reverse walk reverses it, so an inverted hop
// silently yields an empty affected set (a false subset pass). DirBoth maps to
// simple.Both (undirected), but the report flags an undirected hop as NOT
// subset-safe for an auth lens — neither real lens uses it.
func mapDirection(d full.Direction) (simple.EdgeDirection, error) {
	switch d {
	case full.DirOut:
		return simple.Outbound, nil
	case full.DirIn:
		return simple.Inbound, nil
	case full.DirBoth:
		return simple.Both, nil
	default:
		return simple.Both, fmt.Errorf("compile: unknown direction %v", d)
	}
}
