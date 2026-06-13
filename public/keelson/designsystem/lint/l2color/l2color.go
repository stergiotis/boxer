// Package l2color implements IDS Tier 1 rule L2 — flag raw color constructor
// calls outside the token-module allowlist.
//
// Rule rationale: ADR-0031 — every color in IDS-conformant apps comes from
// the semantic palette, the data-encoding palette (Crameri / viridis), or
// the neutrals (text.* / bg.* / border.*). Raw color literals break the
// centralised palette discipline and bypass the IP-boundary check.
//
// v1 implementation note (per EXPLANATION.md): detection is syntactic —
// `<ident.Name == "color">.RGB(...)` / `.RGBA(...)`. The egui2 `color.RGBA`
// is a function call; the standard library `image/color.RGBA` is a struct
// literal, so the syntactic discriminator is unambiguous in practice. False
// positives are possible if a non-egui2 package is imported under the alias
// `color`; line-level ignore via `// designlint:ignore=L2 (reason)` handles
// these cases. v2 may strengthen with type-info resolution.
package l2color

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// allowlistedPkgPathSuffixes are package-path tails that may legitimately
// produce or reference raw color constructors — the color package itself,
// the styletokens mirror, and the generated data_encoding LUTs.
var allowlistedPkgPathSuffixes = []string{
	"/public/thestack/imzero2/egui2/widgets/color",
	"/public/keelson/designsystem/styletokens",
	"/public/keelson/designsystem/styletokens/data_encoding",
}

// triggerSelectors are the function names on the `color` package that
// raise the lint when called outside the allowlist.
var triggerSelectors = map[string]bool{
	"RGB":  true,
	"RGBA": true,
}

// Analyzer is the L2 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l2color",
	Doc:      "L2: flag raw color.RGB / color.RGBA calls outside the token module (ADR-0031).",
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
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return
		}
		if ident.Name != "color" {
			return
		}

		file := findFile(stack)
		idx, ok := ignoreByFile[file]
		if !ok {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		if idx.Suppressed(call.Pos(), "L2") {
			return
		}

		pass.ReportRangef(call,
			"L2: raw color.%s call outside token module; use a styletokens palette constant (ADR-0031 §SD6); annotate with // designlint:ignore=L2 (reason) if intentional",
			sel.Sel.Name)
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
