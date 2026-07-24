//go:build ignore

// lint-lens-anchors — structural guard for the read-grant / lens
// dual-enumeration seam (the "footgun"). A non-self-anchored Personal
// (nats-subject) Lens projects a row keyed on some vertex OTHER than the
// recipient identity, and Refractor's D1 readableAnchors gate
// (internal/refractor/projection/personal.go → capabilityread.IsReadable)
// SILENTLY drops that row unless a read-grant PRODUCER lens in the SAME package
// grants the anchor's kind. The data walk and the grant walk are hand-authored
// twice and nothing compiles one from the other, so a producer that forgets a
// slice fails closed with nothing reporting why — the Fire-1 bug that left only
// edgeIdentity's self-anchor reaching a live tenant.
//
// This is the Stage-1 structural half of the dual-enumeration hardening (the
// runtime half is packages/edge-manifest/coverage_proof_test.go). It does NOT
// derive grants from the data lenses — that would make D1's gate a tautology
// and delete the boundary. It only asserts the two independent enumerations
// STRUCTURALLY agree: every anchor KIND a Personal lens projects has a matching
// producer branch.
//
// The invariant, precisely:
//   - A Personal lens's anchor kind is the LABEL of the vertex its
//     `RETURN <var>.key AS anchor` binds — `(wo:workorder)` → "workorder".
//   - A self-anchored lens (its anchor var bound `{key: $actorKey}`) is exempt:
//     the platform base cap-read self-grant already covers the actor's own key.
//   - A producer branch is an `anchorType: '<kind>'` element of an
//     actorAggregate lens's readableAnchors, in the same package.
//   - Every non-self anchor kind must appear as some producer's anchorType.
//
// Runs per package under packages/. STRICT=1 exits non-zero on any issue.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type lens struct {
	name           string // CanonicalName
	adapter        string
	personal       bool
	projectionKind string
	spec           string // resolved cypher
	pos            token.Position
}

var (
	reAnchor   = regexp.MustCompile(`(\w+)\.key\s+AS\s+anchor\b`)
	reAnchorTy = regexp.MustCompile(`anchorType:\s*'([A-Za-z0-9_]+)'`)
)

// nodeLabel finds the label the pattern binds to variable v — the first
// `(v:label` occurrence in the cypher. "" if none.
func nodeLabel(cypher, v string) string {
	re := regexp.MustCompile(`\(\s*` + regexp.QuoteMeta(v) + `\s*:\s*([A-Za-z0-9_]+)`)
	if m := re.FindStringSubmatch(cypher); m != nil {
		return m[1]
	}
	return ""
}

// isSelfAnchored reports whether v is bound with `{key: $actorKey}` — the actor
// itself, covered by the base self-grant.
func isSelfAnchored(cypher, v string) bool {
	re := regexp.MustCompile(`\(\s*` + regexp.QuoteMeta(v) + `\s*:\s*[A-Za-z0-9_]+\s*\{[^}]*key\s*:\s*\$actorKey`)
	return re.MatchString(cypher)
}

func main() {
	strict := os.Getenv("STRICT") == "1"
	for _, a := range os.Args[1:] {
		if a == "--strict" {
			strict = true
		}
	}

	dirs, _ := filepath.Glob("packages/*")
	issues, warns := 0, 0
	for _, dir := range dirs {
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			continue
		}
		i, w := checkPackage(dir)
		issues += i
		warns += w
	}

	if issues == 0 && warns == 0 {
		fmt.Println("lint-lens-anchors: 0 issues — every Personal-lens anchor kind has a producer branch")
		return
	}
	fmt.Printf("lint-lens-anchors: %d issue(s), %d advisory warning(s)\n", issues, warns)
	if strict && issues > 0 {
		os.Exit(1)
	}
}

// checkPackage parses one package directory and enforces the anchor-coverage
// invariant over its Personal lenses and producers.
func checkPackage(dir string) (issues, warns int) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		// A package that does not parse is another gate's problem, not ours.
		return 0, 0
	}

	consts := map[string]string{}
	var lenses []lens
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			collectConsts(f, consts)
		}
	}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			lenses = append(lenses, collectLenses(fset, f, consts)...)
		}
	}

	// Only packages that ship a Personal (nats-subject) lens participate.
	var personal []lens
	providedKinds := map[string]bool{}
	for _, l := range lenses {
		if l.adapter == "nats-subject" && l.personal {
			personal = append(personal, l)
		}
		if l.projectionKind == "actorAggregate" {
			for _, m := range reAnchorTy.FindAllStringSubmatch(l.spec, -1) {
				providedKinds[m[1]] = true
			}
		}
	}
	if len(personal) == 0 {
		return 0, 0
	}

	for _, l := range personal {
		m := reAnchor.FindStringSubmatch(l.spec)
		if m == nil {
			// A Personal lens with no `.key AS anchor` cannot be classified —
			// surface it rather than pass silently, but do not block CI on a
			// shape this lint does not model.
			fmt.Printf("%s: warn: Personal lens %s has no `<var>.key AS anchor` — cannot verify its read-grant coverage\n", posOf(l), l.name)
			warns++
			continue
		}
		anchorVar := m[1]
		if isSelfAnchored(l.spec, anchorVar) {
			continue // self-anchored — base cap-read self-grant covers it
		}
		kind := nodeLabel(l.spec, anchorVar)
		if kind == "" {
			fmt.Printf("%s: warn: Personal lens %s anchors on `%s` but its node label is undeterminable — cannot verify coverage\n", posOf(l), l.name, anchorVar)
			warns++
			continue
		}
		if !providedKinds[kind] {
			fmt.Printf("%s: Personal lens %s projects a '%s' anchor but NO read-grant producer in package %s grants anchorType '%s' — Refractor's D1 gate would silently drop every row (the 'forgot the slice' dual-enumeration bug). Producer kinds present: %v\n",
				posOf(l), l.name, kind, filepath.Base(dir), kind, sortedKeys(providedKinds))
			issues++
		}
	}
	return issues, warns
}

func posOf(l lens) string {
	return fmt.Sprintf("%s:%d", l.pos.Filename, l.pos.Line)
}

// collectConsts records every string const's value (backtick or quoted).
func collectConsts(f *ast.File, out map[string]string) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if v, err := strconv.Unquote(bl.Value); err == nil {
						out[name.Name] = v
					}
				}
			}
		}
	}
}

// collectLenses finds every `[]pkgmgr.LensSpec{...}` composite literal and reads
// each element's fields, resolving the Spec const to its cypher.
func collectLenses(fset *token.FileSet, f *ast.File, consts map[string]string) []lens {
	var out []lens
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		at, ok := cl.Type.(*ast.ArrayType)
		if !ok || !isLensSpecSelector(at.Elt) {
			return true
		}
		for _, e := range cl.Elts {
			el, ok := e.(*ast.CompositeLit)
			if !ok {
				continue
			}
			l := lens{pos: fset.Position(el.Pos())}
			for _, fe := range el.Elts {
				kv, ok := fe.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "CanonicalName":
					l.name = stringLit(kv.Value)
				case "Adapter":
					l.adapter = stringLit(kv.Value)
				case "ProjectionKind":
					l.projectionKind = stringLit(kv.Value)
				case "Personal":
					if id, ok := kv.Value.(*ast.Ident); ok {
						l.personal = id.Name == "true"
					}
				case "Spec":
					switch v := kv.Value.(type) {
					case *ast.Ident:
						l.spec = consts[v.Name]
					case *ast.BasicLit:
						if s, err := strconv.Unquote(v.Value); err == nil {
							l.spec = s
						}
					}
				}
			}
			out = append(out, l)
		}
		return true
	})
	return out
}

func isLensSpecSelector(e ast.Expr) bool {
	se, ok := e.(*ast.SelectorExpr)
	return ok && se.Sel != nil && se.Sel.Name == "LensSpec"
}

func stringLit(e ast.Expr) string {
	if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
		if v, err := strconv.Unquote(bl.Value); err == nil {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
