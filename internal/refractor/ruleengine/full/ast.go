// Package full's AST is the Refractor-native representation of an openCypher
// rule produced by the visitor in visitor.go. It deliberately holds NO ANTLR
// types — everything below is pure Go so the rest of Refractor can consume
// the AST without leaking the vendored parser.
//
// The executor walks these nodes against Core/Adjacency KV to produce
// projections.
package full

import (
	"errors"
	"fmt"
)

// Direction names the orientation of a RelPattern.
type Direction int

const (
	// DirOut is `-[:t]->` (left → right).
	DirOut Direction = iota
	// DirIn is `<-[:t]-` (right → left).
	DirIn
	// DirBoth is `-[:t]-` (no arrowhead on either side; either direction
	// satisfies the pattern). Bootstrap query does not currently use this
	// form, but the visitor accepts it because the grammar permits it.
	DirBoth
)

func (d Direction) String() string {
	switch d {
	case DirOut:
		return "out"
	case DirIn:
		return "in"
	case DirBoth:
		return "both"
	default:
		return "unknown"
	}
}

// Query is the top-level AST node. Clauses appear in source order.
type Query struct {
	Clauses []Clause
}

// Clause is one of Match, With, Return.
type Clause interface{ isClause() }

// Match models `MATCH …` and `OPTIONAL MATCH …` (Optional=true) with an
// optional WHERE expression. Pattern carries the alternating node/rel chain.
type Match struct {
	Optional bool
	Patterns []PathPattern // a MATCH can list multiple comma-separated patterns
	Where    Expr          // nil if absent
}

func (*Match) isClause() {}

// With carries forward named bindings from the preceding read clauses into
// the next clause group. WITH also accepts a WHERE filter.
type With struct {
	Distinct bool
	Items    []ProjectionItem
	Where    Expr
}

func (*With) isClause() {}

// Return is the projection emitted as the rule's output.
type Return struct {
	Distinct bool
	Items    []ProjectionItem
}

func (*Return) isClause() {}

// PathPattern is an alternating chain of node patterns and relationship
// patterns. len(Rels) == len(Nodes)-1. The first element is always a node.
type PathPattern struct {
	Nodes []NodePattern
	Rels  []RelPattern
}

// NodePattern is `(var:Label {props})`. Any field may be empty.
type NodePattern struct {
	Variable   string
	Label      string
	Properties map[string]Expr
}

// RelPattern is the relationship segment of a path pattern.
//
// MinHops/MaxHops carry variable-length quantifier info:
//
//	no `*`              → MinHops=1, MaxHops=1
//	`*0..`              → MinHops=0, MaxHops=-1 (unbounded)
//	`*0..2`             → MinHops=0, MaxHops=2
//	`*N..M`             → MinHops=N, MaxHops=M
//
// MaxHops=-1 means "unbounded".
type RelPattern struct {
	Variable   string
	Type       string
	Direction  Direction
	MinHops    int
	MaxHops    int
	Properties map[string]Expr
}

// ProjectionItem is one entry in a WITH or RETURN list.
type ProjectionItem struct {
	Expr  Expr
	Alias string // "" when no AS provided
}

// Expr is the marker interface for all expression nodes.
type Expr interface{ isExpr() }

// Literal holds a primitive value: bool, int64, float64, string, or nil.
type Literal struct {
	Value any
}

func (*Literal) isExpr() {}

// ParameterRef captures `$name` references. Bound to actual values by the
// executor in 3.1b-ii from EventContext.Parameters.
type ParameterRef struct {
	Name string
}

func (*ParameterRef) isExpr() {}

// VariableRef is a bare variable, e.g. `identity` or `perm`.
type VariableRef struct {
	Name string
}

func (*VariableRef) isExpr() {}

// PropertyAccess is `target.key`. Nested access (`a.b.c`) chains via Target
// being another PropertyAccess.
type PropertyAccess struct {
	Target Expr
	Key    string
}

func (*PropertyAccess) isExpr() {}

// BinaryOp covers comparison ops (=, <>, <, >, <=, >=) and arithmetic ops
// (+, -, *, /, %). For boolean AND/OR see AndOr.
type BinaryOp struct {
	Op    string
	Left  Expr
	Right Expr
}

func (*BinaryOp) isExpr() {}

// AndOr models n-ary boolean combinators. Op is "AND" or "OR".
type AndOr struct {
	Op       string
	Operands []Expr
}

func (*AndOr) isExpr() {}

// Not is logical negation of any boolean expression. Used both for plain
// `NOT x` and for the anti-pattern `NOT (a)-[:b]->(c)` (the operand is a
// PatternExpr in that case).
type Not struct {
	Operand Expr
}

func (*Not) isExpr() {}

// PatternExpr wraps a pattern used as an existence test inside expressions
// (most commonly inside `WHERE NOT (...)`).
type PatternExpr struct {
	Pattern PathPattern
}

func (*PatternExpr) isExpr() {}

// FunctionCall captures any function invocation. The `collect()` and
// `collect(DISTINCT ...)` calls land here with Name=="collect" and
// Distinct=true when applicable.
type FunctionCall struct {
	Namespace []string
	Name      string
	Distinct  bool
	Args      []Expr
}

func (*FunctionCall) isExpr() {}

// MapLiteral is `{key: expr, ...}` — preserves insertion order via Keys.
type MapLiteral struct {
	Keys   []string
	Values map[string]Expr
}

func (*MapLiteral) isExpr() {}

// ListLiteral is `[expr, expr, ...]`.
type ListLiteral struct {
	Elements []Expr
}

func (*ListLiteral) isExpr() {}

// PatternComprehension is `[pattern | projection]` or
// `[pattern WHERE pred | projection]`. The bootstrap query uses this form
// inside the `serviceAccess` map literal's `allowedOperations` field.
type PatternComprehension struct {
	Variable   string // optional named binding
	Pattern    PathPattern
	Where      Expr
	Projection Expr
}

func (*PatternComprehension) isExpr() {}

// CaseWhenThen is one `WHEN <cond> THEN <result>` alternative of a generic
// CASE expression.
type CaseWhenThen struct {
	When Expr
	Then Expr
}

// CaseExpr is the generic (no test-expression) form of a CASE expression:
// `CASE (WHEN cond THEN result)+ (ELSE default)? END`. Each WHEN condition
// is evaluated in order and is truthy-tested; the first match's THEN value
// is returned. Else is nil when absent (matching Cypher's implicit
// `ELSE NULL`). The simple (test-expression) form `CASE expr WHEN val ...`
// is not supported.
type CaseExpr struct {
	Alternatives []CaseWhenThen
	Else         Expr
}

func (*CaseExpr) isExpr() {}

// CompiledRule satisfies ruleengine.CompiledRule. It is the opaque value
// full.Engine.Parse returns; full.Engine.Execute (3.1b-ii) will consume it.
type CompiledRule struct {
	Query *Query

	// KeyColumns are the RETURN aliases designated as the projection's output
	// key, threaded from Rule.Into.Key at activation. When set, the executor
	// builds the complete multi-column key map (mirroring the simple engine) so
	// a composite-key lens — e.g. a GrantTable lens keyed on
	// (actor_id, anchor_id, grant_source) — projects every key column the
	// adapter requires. Empty/unset keeps the legacy first-RETURN-item key, so
	// single-key lenses and directly-constructed test rules are unchanged.
	KeyColumns []string
}

// EngineName implements ruleengine.CompiledRule.
func (*CompiledRule) EngineName() string { return "full" }

// returnAliases returns the effective output aliases of the rule's RETURN
// clause (the explicit alias, else the auto-alias) in declaration order, and
// whether a RETURN clause was found.
func (cr *CompiledRule) returnAliases() ([]string, bool) {
	if cr == nil || cr.Query == nil {
		return nil, false
	}
	for _, c := range cr.Query.Clauses {
		r, isReturn := c.(*Return)
		if !isReturn {
			continue
		}
		aliases := make([]string, 0, len(r.Items))
		for i, it := range r.Items {
			a := it.Alias
			if a == "" {
				a = projectionAutoAlias(it.Expr, i)
			}
			aliases = append(aliases, a)
		}
		return aliases, true
	}
	return nil, false
}

// ValidateKeyColumns fails closed when a declared key column is not a RETURN
// alias of the compiled query — the column's value would otherwise be silently
// absent from the projection key map at write time. This mirrors the simple
// engine's compile-time key-field validation, keeping the §6.13 fail-closed
// activation posture for composite-key full-engine lenses (e.g. GrantTable).
// With no key columns declared the engine keeps its first-RETURN-item key, so
// there is nothing to validate.
func (cr *CompiledRule) ValidateKeyColumns() error {
	if cr == nil || len(cr.KeyColumns) == 0 {
		return nil
	}
	aliases, ok := cr.returnAliases()
	if !ok {
		return errors.New("full engine: compiled rule has no RETURN clause")
	}
	have := make(map[string]struct{}, len(aliases))
	for _, a := range aliases {
		have[a] = struct{}{}
	}
	for _, col := range cr.KeyColumns {
		if _, present := have[col]; !present {
			return fmt.Errorf("full engine: key column %q is not a RETURN alias", col)
		}
	}
	return nil
}

// ValidateReturnAliases fails closed when any of names is not a RETURN alias
// of the compiled query. Activation-time counterpart of ValidateKeyColumns
// for columns the caller consumes off the row map (e.g. a Secure Lens's
// secure + identity-key columns) — a missing alias would otherwise be
// silently null on every row, indistinguishable from legitimately-absent
// data.
func (cr *CompiledRule) ValidateReturnAliases(names ...string) error {
	if cr == nil || len(names) == 0 {
		return nil
	}
	aliases, ok := cr.returnAliases()
	if !ok {
		return errors.New("full engine: compiled rule has no RETURN clause")
	}
	have := make(map[string]struct{}, len(aliases))
	for _, a := range aliases {
		have[a] = struct{}{}
	}
	for _, n := range names {
		if _, present := have[n]; !present {
			return fmt.Errorf("full engine: column %q is not a RETURN alias", n)
		}
	}
	return nil
}
