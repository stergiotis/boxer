//go:build llm_generated_opus47

// Package l4rounding implements IDS Tier 1 rule L4 — flag literal numeric
// values in rounding-aware API calls outside the styletokens allowlist.
//
// Rule rationale: ADR-0032 §SD3 — the rounding ladder is fixed at
// 0 / 2 / 4 / 6 px (RoundingNone / RoundingSm / RoundingMd / RoundingLg).
// A raw literal in .CornerRadius() bypasses the ladder, so a panel that
// should adopt RoundingMd (4 px cards/dialogs) instead drifts to whatever
// happened to be typed at the call site. The ladder is density-independent
// per ADR-0032 SD3, so unlike L3 there is no density-resolution argument —
// the case is purely "literal numbers fragment the visual system".
//
// v1 implementation note: detection is syntactic. We trigger on selector
// names matching the rounding-aware API surface (CornerRadius) and
// inspect the first positional argument; a numeric literal (INT or FLOAT)
// that is not in the no-op allowlist (0 / 0.0) raises the finding.
// Variable-bound arguments (the canonical `styletokens.RoundingSm` /
// `visuals.CornerRadius` forms) never trigger because they're not BasicLit
// nodes.
//
// Allowlist files: the styletokens module itself, where the ladder
// constants legitimately live. Line-level ignore via
// `// designlint:ignore=L4 (reason)` handles the rare case where a
// non-token-source literal is intentional.
package l4rounding

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// allowlistedPkgPathSuffixes are package-path tails where literal rounding
// values legitimately live — the styletokens module's ladder constants.
var allowlistedPkgPathSuffixes = []string{
	"/public/keelson/designsystem/styletokens",
}

// triggerSelectors are the rounding-aware method names that take a numeric
// arg representing corner radius in px. Detection is by suffix selector
// name only — works regardless of the FrameFluid / ProgressBarFluid /
// TintedScopeFluid receiver and import-alias variations.
var triggerSelectors = map[string]bool{
	"CornerRadius": true,
}

// allowedLiterals are the values that may appear raw in rounding positions
// without referring to a token — 0 (sharp corners, the Swiss default
// per ADR-0032 §SD3) which has no token-form replacement. All other
// ladder values must reach for styletokens.RoundingSm / RoundingMd /
// RoundingLg by name.
var allowedLiterals = map[string]bool{
	"0":   true,
	"0.0": true,
}

// Analyzer is the L4 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l4rounding",
	Doc:      "L4: flag literal numeric values in rounding-aware API calls outside the styletokens module (ADR-0032 §SD3).",
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
		if idx.Suppressed(call.Pos(), "L4") {
			return
		}

		pass.ReportRangef(call,
			"L4: raw literal %s in rounding-aware call .%s(); use a styletokens accessor (RoundingSm / RoundingMd / RoundingLg — ADR-0032 §SD3); annotate with // designlint:ignore=L4 (reason) if intentional",
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
