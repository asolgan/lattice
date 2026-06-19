package simple

import (
	"context"
	"fmt"

	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
	"github.com/asolgan/lattice/internal/substrate"
)

// MaxBranchLen caps the length of any compiled invalidation branch. It mirrors
// pipeline.DefaultActorMaxDepth (10) and serves as the cycle guard: a pattern
// that chains more than this many hops off the anchor is truncated rather than
// walked unbounded.
const MaxBranchLen = 10

// InvalidationForest is the compiled, reverse-walkable representation of an
// actor-aggregate lens's MATCH patterns. It is a FOREST of per-branch linear
// anchor→leaf chains, each a single *QueryPlan whose Steps form one contiguous
// chain from the anchor variable to a leaf.
//
// A forest — never a single flat Steps slice — is mandatory. The reverse walk
// (walkBackToAnchor) assumes plan.Steps is one linear chain and recurses
// contiguously from a matched leaf at index i back through i→i-1→…→0. A lens
// with two disjoint branches off the anchor (capabilityEphemeral's direct
// assignedTo path and its reportsTo→assignedTo delegation path) flattened into
// one Steps slice would interleave the branches: a leaf in the delegation
// branch reverse-walks back through the direct branch's steps and drops the
// manager anchor — a missed revocation on the auth plane. Segmenting the AST
// into per-branch chains and running the reverse walk over EACH branch keeps
// every branch contiguous, so walkBackToAnchor is correct within it; unioning
// the per-branch anchor sets yields the complete affected-anchor set.
type InvalidationForest struct {
	// AnchorLabel is the label of the anchor node (first node of the first
	// required MATCH), shared by every branch.
	AnchorLabel string
	// AnchorVariable is the query variable bound to the anchor node.
	AnchorVariable string
	// Branches are the per-branch linear plans. Each branch's Steps form a
	// contiguous anchor→leaf chain.
	Branches []*QueryPlan
}

// CompileInvalidationForest parses a lens cypher body through the production
// full engine and compiles the resulting AST into an InvalidationForest of
// per-branch linear *QueryPlans. It calls the full engine to walk the AST and
// emits simple plans; the forest is then reverse-walked by AffectedAnchors.
//
// WHERE clauses are deliberately ignored: every WHERE on an actor-aggregate
// lens only NARROWS the matched set, so dropping it can only enlarge the
// affected-anchor set toward (never past) the broad BFS superset — it can never
// miss an anchor. collect()/+ aggregation in RETURN is irrelevant to
// invalidation (it shapes output, not reachability).
func CompileInvalidationForest(body string) (*InvalidationForest, error) {
	eng := full.New()
	cr, err := eng.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("compile invalidation: parse: %w", err)
	}
	compiled, ok := cr.(*full.CompiledRule)
	if !ok {
		return nil, fmt.Errorf("compile invalidation: expected *full.CompiledRule, got %T", cr)
	}
	q := compiled.Query
	if q == nil {
		return nil, fmt.Errorf("compile invalidation: nil query")
	}
	return compileForestFromQuery(q)
}

// compileForestFromQuery is the AST→forest core, factored out so callers that
// already hold a parsed *full.Query (e.g. a lens whose CompiledRule is the full
// engine's) can compile without re-parsing.
func compileForestFromQuery(q *full.Query) (*InvalidationForest, error) {
	var steps []TraversalStep
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
				dir, derr := mapFullDirection(rel.Direction)
				if derr != nil {
					return nil, derr
				}
				steps = append(steps, TraversalStep{
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
		return nil, fmt.Errorf("compile invalidation: query has no required MATCH to anchor on")
	}

	// Resolve each variable's label from the binding that introduced it: a
	// re-referenced anchor (e.g. `(identity)` in capabilityEphemeral's
	// delegation MATCH) carries no inline label, so its label must be
	// recovered or the reverse walk's label/direction matching is wrong.
	labelOf := map[string]string{anchorVar: anchorLabel}
	for _, s := range steps {
		if s.ToLabel != "" {
			labelOf[s.ToVariable] = s.ToLabel
		}
		if s.FromLabel != "" {
			labelOf[s.FromVariable] = s.FromLabel
		}
	}

	// An anchor-only lens (a bound anchor MATCH with no relationships) is sound:
	// only the anchor's own vertex change matters, and that is handled by the
	// Execution half, not the reverse walk. It compiles to a forest with zero
	// branches (AffectedAnchors yields the empty set; the anchor's own change is
	// projected directly). A lens that DOES declare relationships but whose steps
	// reach no anchor-rooted branch is rejected by the coverage connectivity
	// check before this point.
	branches := buildInvalidationBranches(steps, anchorVar, labelOf)

	plans := make([]*QueryPlan, 0, len(branches))
	for _, b := range branches {
		plans = append(plans, &QueryPlan{
			AnchorLabel:    anchorLabel,
			AnchorVariable: anchorVar,
			Steps:          b,
		})
	}
	return &InvalidationForest{
		AnchorLabel:    anchorLabel,
		AnchorVariable: anchorVar,
		Branches:       plans,
	}, nil
}

// buildInvalidationBranches expands the flat step list into linear anchor→leaf
// chains. Each branch is a contiguous chain whose first step starts at the
// anchor variable and each subsequent step's FromVariable equals the previous
// step's ToVariable. A fork (two steps sharing a FromVariable) yields two
// branches. FromLabel is backfilled from labelOf so reverse walks on
// re-referenced anchors match the right label/direction.
func buildInvalidationBranches(steps []TraversalStep, anchorVar string, labelOf map[string]string) [][]TraversalStep {
	byFrom := map[string][]TraversalStep{}
	for _, s := range steps {
		s.FromLabel = labelOf[s.FromVariable]
		if s.ToLabel == "" {
			s.ToLabel = labelOf[s.ToVariable]
		}
		byFrom[s.FromVariable] = append(byFrom[s.FromVariable], s)
	}

	var branches [][]TraversalStep
	var walk func(tipVar string, prefix []TraversalStep)
	walk = func(tipVar string, prefix []TraversalStep) {
		if len(prefix) >= MaxBranchLen {
			if len(prefix) > 0 {
				branch := make([]TraversalStep, len(prefix))
				copy(branch, prefix)
				branches = append(branches, branch)
			}
			return
		}
		nexts := byFrom[tipVar]
		if len(nexts) == 0 {
			if len(prefix) > 0 {
				branch := make([]TraversalStep, len(prefix))
				copy(branch, prefix)
				branches = append(branches, branch)
			}
			return
		}
		for _, s := range nexts {
			extended := make([]TraversalStep, len(prefix), len(prefix)+1)
			copy(extended, prefix)
			extended = append(extended, s)
			walk(s.ToVariable, extended)
		}
	}
	walk(anchorVar, nil)
	return branches
}

// AffectedAnchors runs the production reverse walk over EACH branch of the
// forest and unions the affected-anchor Core KV keys. Running per-branch is the
// soundness guarantee: each branch is a contiguous chain, so the reverse walk
// (the same reverseTraverse the live evaluator uses) is correct within it.
func (forest *InvalidationForest) AffectedAnchors(ctx context.Context, entry NodeEntry, adjKV *substrate.KV) ([]string, error) {
	seen := map[string]struct{}{}
	for _, branch := range forest.Branches {
		keys, err := reverseTraverse(ctx, branch, entry, adjKV)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out, nil
}

// mapFullDirection maps a full.Direction onto the simple engine's
// EdgeDirection. The mapping is load-bearing: the reverse walk reverses it, so
// an inverted hop silently yields an empty affected set (a false subset pass).
// full.DirBoth maps to Both (undirected), which the activation policy flags as
// not subset-safe for an auth-plane lens.
func mapFullDirection(d full.Direction) (EdgeDirection, error) {
	switch d {
	case full.DirOut:
		return Outbound, nil
	case full.DirIn:
		return Inbound, nil
	case full.DirBoth:
		return Both, nil
	default:
		return Both, fmt.Errorf("compile invalidation: unknown direction %v", d)
	}
}
