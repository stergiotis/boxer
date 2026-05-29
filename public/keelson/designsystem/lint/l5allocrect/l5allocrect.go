//go:build llm_generated_opus47

// Package l5allocrect implements IDS Tier 1 rule L5 — flag
// c.AllocateUiAtRect(...) lexically nested inside a Vertical / Horizontal /
// Grid flow container.
//
// Rule rationale: [project_imzero2_allocate_ui_at_rect] — AllocateUiAtRect
// positions its child Ui at absolute parent coordinates, silently breaking
// the enclosing flow container's layout. Hard to debug because the rendered
// output is wrong but no error fires.
//
// The detection is purely lexical: walk the AST stack upward from each
// AllocateUiAtRect call, and flag if any ancestor RangeStmt's range
// expression is a chain rooted at one of the recognised flow constructors
// (c.Vertical, c.Horizontal, c.HorizontalCentered, c.HorizontalWrapped,
// c.VerticalCentered, c.Grid). The idiom is `for range c.<Flow>().KeepIter()
// { ... }` — the chain bottoms out at c.<Flow>(...) below the .KeepIter().
//
// Suppress via `// designlint:ignore=L5 (reason)` on or above the call site
// when the absolute placement is intentional (overlay tooltips, painters
// allocating into specific rects within a layout-computed region, etc.).
package l5allocrect

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// flowContainers names the Ctx methods that start a layout flow. An
// AllocateUiAtRect call lexically nested inside any of these is the L5
// violation pattern.
var flowContainers = map[string]bool{
	"Vertical":           true,
	"Horizontal":         true,
	"VerticalCentered":   true,
	"HorizontalCentered": true,
	"HorizontalWrapped":  true,
	"Grid":               true,
}

// Analyzer is the L5 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l5allocrect",
	Doc:      "L5: flag c.AllocateUiAtRect inside a Vertical/Horizontal/Grid flow (project_imzero2_allocate_ui_at_rect).",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (result interface{}, err error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ignoreByFile := make(map[*ast.File]*ignoreann.Index)

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	insp.WithStack(nodeFilter, func(n ast.Node, push bool, stack []ast.Node) (proceed bool) {
		proceed = true
		if !push {
			return
		}
		call := n.(*ast.CallExpr)
		if !isAllocateUiAtRect(call) {
			return
		}
		container := enclosingFlowContainer(stack)
		if container == "" {
			return
		}
		file := findFile(stack)
		idx, ok := ignoreByFile[file]
		if !ok {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		if idx.Suppressed(call.Pos(), "L5") {
			return
		}
		pass.ReportRangef(call,
			"L5: AllocateUiAtRect inside c.%s(...) flow uses absolute coordinates and silently breaks the enclosing layout (project_imzero2_allocate_ui_at_rect); annotate with // designlint:ignore=L5 (reason) if intentional",
			container)
		return
	})
	return
}

func isAllocateUiAtRect(call *ast.CallExpr) (ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return
	}
	ok = sel.Sel.Name == "AllocateUiAtRect"
	return
}

// enclosingFlowContainer returns the name of the nearest enclosing flow
// container's constructor (Vertical / Horizontal / Grid / ...) if the
// AllocateUiAtRect call sits inside the body of a `for range c.<Flow>().KeepIter()`
// (or similar chained-iter) loop. Returns "" otherwise.
func enclosingFlowContainer(stack []ast.Node) (name string) {
	for i := len(stack) - 1; i >= 0; i-- {
		rs, ok := stack[i].(*ast.RangeStmt)
		if !ok {
			continue
		}
		if root := flowConstructorRoot(rs.X); root != "" {
			name = root
			return
		}
	}
	return
}

// flowConstructorRoot walks a chained CallExpr / SelectorExpr tree looking
// for a known flow-constructor name at any level. Handles patterns like
// `c.Vertical().KeepIter()` (constructor wrapped in .KeepIter) or
// `c.Horizontal(opts).SendIter()` or `c.Grid(id).KeepIter()`.
func flowConstructorRoot(expr ast.Expr) (name string) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if flowContainers[sel.Sel.Name] {
			name = sel.Sel.Name
			return
		}
		expr = sel.X
	}
}

func findFile(stack []ast.Node) (file *ast.File) {
	for i := len(stack) - 1; i >= 0; i-- {
		if f, ok := stack[i].(*ast.File); ok {
			file = f
			return
		}
	}
	return
}
