//go:build llm_generated_opus47

// Package l3spacing implements IDS Tier 1 rule L3 — flag literal floats
// in spacing-aware API calls outside the styletokens allowlist.
//
// Rule rationale: ADR-0032 §SD2 — spacing tokens drive density resolution.
// A raw literal in c.AddSpace / Frame.InnerMargin / Frame.OuterMargin
// bypasses the density preset, so a panel that should adapt to Tight
// stays at Standard.
//
// v1 implementation note: detection is syntactic. We trigger on selector
// names matching the spacing-aware API surface (AddSpace / InnerMargin /
// OuterMargin) and inspect the first positional argument; a numeric
// literal (INT or FLOAT) that is not in the hairline allowlist (0 / 0.0 /
// 1 / 1.0) raises the finding. Variable-bound arguments (the canonical
// `styletokens.PaddingDefault(d)` form) never trigger because they're
// not BasicLit nodes.
//
// Allowlist files: the styletokens module itself, where the PX_TABLE
// numeric literals legitimately live. Line-level ignore via
// `// designlint:ignore=L3 (reason)` handles the rare case where a
// non-token-source literal is intentional.
package l3spacing

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// allowlistedPkgPathSuffixes are package-path tails where literal spacing
// values legitimately live — the styletokens module's PX_TABLE and the
// purpose-based helper bodies.
var allowlistedPkgPathSuffixes = []string{
	"/public/keelson/designsystem/styletokens",
}

// triggerSelectors are the spacing-aware method names that take a numeric
// arg representing px. Detection is by suffix selector name only — works
// regardless of the c.* / ui.* receiver and import-alias variations.
var triggerSelectors = map[string]bool{
	"AddSpace":    true,
	"InnerMargin": true,
	"OuterMargin": true,
}

// allowedLiterals are the values that may appear raw in spacing positions
// without referring to a token — 0 (no-op) and 1 (hairline strokes per
// ADR-0029 §SD6 / ADR-0032 §SD4).
var allowedLiterals = map[string]bool{
	"0":   true,
	"0.0": true,
	"1":   true,
	"1.0": true,
}

// Analyzer is the L3 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l3spacing",
	Doc:      "L3: flag literal floats in spacing-aware API calls outside the styletokens module (ADR-0032 §SD2).",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (result interface{}, err error) {
	if pkgIsAllowlisted(pass.Pkg.Path()) {
		return
	}
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
		if !triggerSelectors[sel.Sel.Name] {
			return
		}
		if len(call.Args) == 0 {
			return
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return
		}
		if lit.Kind != token.INT && lit.Kind != token.FLOAT {
			return
		}
		if allowedLiterals[lit.Value] {
			return
		}

		file := findFile(stack)
		idx, ok := ignoreByFile[file]
		if !ok {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		if idx.Suppressed(call.Pos(), "L3") {
			return
		}

		pass.ReportRangef(call,
			"L3: raw literal %s in spacing-aware call .%s(); use a styletokens accessor (PaddingDefault / GapItems / etc. — ADR-0032 §SD2); annotate with // designlint:ignore=L3 (reason) if intentional",
			lit.Value, sel.Sel.Name)
		return
	})
	return
}

func pkgIsAllowlisted(path string) (ok bool) {
	for _, suffix := range allowlistedPkgPathSuffixes {
		if strings.HasSuffix(path, suffix) {
			ok = true
			return
		}
	}
	return
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
