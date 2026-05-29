//go:build llm_generated_opus47

// Package l9radiochanged implements IDS Tier 1 rule L9 — flag
// `.HasChanged()` calls on a chain rooted at `c.RadioButton(...)`.
//
// Rule rationale: [feedback_radio_haspricked] — egui's RadioButton never
// calls `mark_changed`, so `HasChanged()` silently drops every click.
// `HasPrimaryClicked()` is the correct event accessor for RadioButton.
//
// The chain at the violation looks like
// `c.RadioButton(...).SendRespVal(&v).HasChanged()`. The detection walks
// back through the selector chain at the HasChanged call site; if any
// ancestor CallExpr's selector is "RadioButton", flag.
//
// v1 limitation: only detects the chained form. The pattern
//
//	resp := c.RadioButton(...).SendRespVal(&v)
//	if resp.HasChanged() { ... }
//
// is not detected (would require def-use analysis). v2 may add it. The
// chained form is the common case in real demos.
package l9radiochanged

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// Analyzer is the L9 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l9radiochanged",
	Doc:      "L9: flag .HasChanged() on a chain rooted at c.RadioButton(...) (feedback_radio_haspricked).",
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
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if sel.Sel.Name != "HasChanged" {
			return
		}
		if !chainContainsRadioButton(sel.X) {
			return
		}
		file := findFile(stack)
		idx, ok := ignoreByFile[file]
		if !ok {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		if idx.Suppressed(call.Pos(), "L9") {
			return
		}
		pass.ReportRangef(call,
			"L9: RadioButton.HasChanged() silently drops every click (egui RadioButton never calls mark_changed); use HasPrimaryClicked() instead (feedback_radio_haspricked)")
		return
	})
	return
}

// chainContainsRadioButton walks the chained receiver expressions looking
// for a CallExpr whose selector name is "RadioButton". Walking stops at the
// first non-Call / non-Selector boundary (typically an *ast.Ident receiver).
func chainContainsRadioButton(expr ast.Expr) (found bool) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if sel.Sel.Name == "RadioButton" {
			found = true
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
