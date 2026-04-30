// Package analyzer implements a go/analysis Analyzer that rejects
// comparisons between the untyped nil literal and any interface value
// whose method set (transitively) embeds goconst.DoNotCompare.
//
// # Rationale
//
// protoc-gen-go-const returns Message_Const interface views for every
// message-typed getter, and goconst.Slice / goconst.Map are themselves
// interfaces. Those views are produced by boxing a concrete *Message
// pointer (or a named slice / map) into an interface value, so a nil
// *Message becomes a classic Go typed-nil at the interface level:
//
//	var p *pb.Person       // nil pointer
//	view := p.AsConst()    // non-nil interface, nil dynamic value
//	_ = view == nil        // ALWAYS false — silent footgun
//
// The library already marks those interfaces by embedding
// goconst.DoNotCompare, which exposes an IsNil() bool method. This
// analyzer flags the comparison and offers a machine-applicable fix
// that rewrites `x == nil` to `x.IsNil()` and `x != nil` to `!x.IsNil()`.
//
// The analyzer is a no-op on packages that do not (transitively) import
// goconst, so it is cheap to enable repo-wide.
package analyzer

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// DoNotComparePkgPath is the import path of the package that declares
// the DoNotCompare marker interface. It is a variable (not a constant)
// so the analysistest harness can repoint it at the in-tree stub under
// testdata/src/goconst without having to ship testdata with a real
// go.mod + replace directive.
//
// Production code should never write to this variable.
var DoNotComparePkgPath = "github.com/Kybxd/goconst"

// DoNotCompareName is the exported name of the marker interface inside
// DoNotComparePkgPath.
const DoNotCompareName = "DoNotCompare"

// Analyzer is the exported go/analysis Analyzer. It is consumed by
// cmd/nilcompare/main.go via singlechecker.Main and by the
// cmd/nilcompare/plugin adapter for golangci-lint custom plugins.
var Analyzer = &analysis.Analyzer{
	Name:     "nilcompare",
	Doc:      "report comparisons between nil and interface values that embed goconst.DoNotCompare; prefer IsNil().",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	markerObj := lookupDoNotCompareObj(pass)
	if markerObj == nil {
		// Target package not imported (directly or transitively) by this
		// package; nothing here can possibly carry the marker interface.
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.BinaryExpr)(nil),
		(*ast.SwitchStmt)(nil),
	}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			checkBinary(pass, markerObj, n)
		case *ast.SwitchStmt:
			checkSwitch(pass, markerObj, n)
		}
	})
	return nil, nil
}

// lookupDoNotCompareObj locates the marker interface in the package's
// import graph. Returning nil means the current package does not
// (transitively) import DoNotComparePkgPath, so there is nothing to
// check and the analyzer exits early.
//
// We key the lookup on the named type's *types.TypeName (rather than
// the underlying *types.Interface), because the analyzer identifies
// matches by *nominal* embedding rather than structural implements(),
// see operandTypeEmbedsMarker for why.
func lookupDoNotCompareObj(pass *analysis.Pass) *types.TypeName {
	for _, imp := range pass.Pkg.Imports() {
		if imp.Path() != DoNotComparePkgPath {
			continue
		}
		obj := imp.Scope().Lookup(DoNotCompareName)
		if obj == nil {
			return nil
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return nil
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			return nil
		}
		if _, ok := named.Underlying().(*types.Interface); !ok {
			return nil
		}
		return tn
	}
	return nil
}

// checkBinary handles `x == nil`, `x != nil`, `nil == x`, `nil != x`.
func checkBinary(pass *analysis.Pass, markerObj *types.TypeName, be *ast.BinaryExpr) {
	if be.Op != token.EQL && be.Op != token.NEQ {
		return
	}

	lhsNil := isUntypedNil(pass, be.X)
	rhsNil := isUntypedNil(pass, be.Y)
	// XOR: exactly one side must be the nil literal. If neither or both
	// (pathological) are nil, there is nothing actionable here.
	if lhsNil == rhsNil {
		return
	}

	var operand ast.Expr
	if rhsNil {
		operand = be.X
	} else {
		operand = be.Y
	}

	if !operandTypeEmbedsMarker(pass, markerObj, operand) {
		return
	}

	// Build the suggested replacement: `x.IsNil()` for ==, `!x.IsNil()`
	// for !=. printer.Fprint round-trips the operand so we preserve
	// whatever it actually looks like (selector chain, call, etc.).
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, pass.Fset, operand); err != nil {
		// Still report the diagnostic — just skip the auto-fix.
		pass.ReportRangef(be, diagMessage(be.Op))
		return
	}
	repl := buf.String() + ".IsNil()"
	if be.Op == token.NEQ {
		repl = "!" + repl
	}

	pass.Report(analysis.Diagnostic{
		Pos:     be.Pos(),
		End:     be.End(),
		Message: diagMessage(be.Op),
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "replace with IsNil()",
			TextEdits: []analysis.TextEdit{{
				Pos:     be.Pos(),
				End:     be.End(),
				NewText: []byte(repl),
			}},
		}},
	})
}

// checkSwitch handles `switch x { case nil: ... }` when x carries the
// marker interface. Type switches — `switch x.(type) { case nil: ... }` —
// ask about the dynamic type and are a different, legitimate question;
// they are intentionally ignored here (they are *ast.TypeSwitchStmt, not
// *ast.SwitchStmt, so they never reach this function).
func checkSwitch(pass *analysis.Pass, markerObj *types.TypeName, sw *ast.SwitchStmt) {
	if sw.Tag == nil {
		// Tag-less switch is just an if / else chain; the individual
		// comparisons inside each case are already covered via
		// checkBinary on their *ast.BinaryExpr nodes.
		return
	}
	if !operandTypeEmbedsMarker(pass, markerObj, sw.Tag) {
		return
	}
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, e := range cc.List {
			if isUntypedNil(pass, e) {
				pass.ReportRangef(e,
					"switch case compares DoNotCompare-bearing interface to nil; use IsNil() in an if instead")
			}
		}
	}
}

func diagMessage(op token.Token) string {
	switch op {
	case token.EQL:
		return "comparing a DoNotCompare-bearing interface value to nil is almost always false; use IsNil() instead"
	case token.NEQ:
		return "comparing a DoNotCompare-bearing interface value to nil is almost always true; use !IsNil() instead"
	}
	return "comparing a DoNotCompare-bearing interface value to nil"
}

// isUntypedNil reports whether expr is the predeclared nil identifier.
// TypesInfo.Types records nil as an untyped nil value.
func isUntypedNil(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok {
		return false
	}
	return tv.IsNil()
}

// operandTypeEmbedsMarker reports whether the static type of expr is an
// interface that *nominally* embeds the marker — i.e. there is a chain
// of named-interface embeddings from expr's type down to the marker's
// declaring TypeName.
//
// We deliberately do NOT use types.Implements here. The marker is a
// one-method interface (IsNil() bool), so any interface that happens to
// declare `IsNil() bool` would structurally implement it — including
// types that have nothing to do with goconst. Nominal embedding keeps
// the check tied to the author's explicit "opt in via embedding"
// intent, which is the whole point of the DoNotCompare marker pattern.
//
// Concrete pointer types are rejected up front: a bare `*Message == nil`
// has the expected semantics and is exactly what the generated IsNil
// method body is written as.
func operandTypeEmbedsMarker(pass *analysis.Pass, markerObj *types.TypeName, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Type == nil {
		return false
	}
	T := tv.Type
	if _, isIface := T.Underlying().(*types.Interface); !isIface {
		return false
	}
	return embedsNamedInterface(T, markerObj, make(map[*types.TypeName]bool))
}

// embedsNamedInterface walks the nominal embedding graph of T and
// reports whether it reaches the interface declared by target.
//
// Embedding in Go interface types is nominal: only a *named* interface
// can be embedded by name, and the embedded element appears in
// *types.Interface.EmbeddedType(i). Anonymous (inline) interface
// literals cannot embed anything, so by definition they cannot reach
// the marker and the walk bottoms out immediately.
//
// The visited set is keyed on *types.TypeName (each Named carries one
// canonical TypeName) to terminate on cycles.
func embedsNamedInterface(T types.Type, target *types.TypeName, visited map[*types.TypeName]bool) bool {
	// Resolve to the underlying interface and — if T is named — record
	// its TypeName so we can both short-circuit on the target and guard
	// against recursion.
	var iface *types.Interface
	switch t := T.(type) {
	case *types.Named:
		tn := t.Obj()
		if tn == target {
			return true
		}
		if visited[tn] {
			return false
		}
		visited[tn] = true
		var ok bool
		iface, ok = t.Underlying().(*types.Interface)
		if !ok {
			return false
		}
	case *types.Interface:
		// Anonymous interface literal — cannot embed by name.
		iface = t
	default:
		return false
	}

	for i := 0; i < iface.NumEmbeddeds(); i++ {
		if embedsNamedInterface(iface.EmbeddedType(i), target, visited) {
			return true
		}
	}
	return false
}
