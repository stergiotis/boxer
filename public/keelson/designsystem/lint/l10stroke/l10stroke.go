//go:build llm_generated_opus47

// Package l10stroke implements IDS Tier 1 rule L10 — flag literal numeric
// stroke-width values in stroke-aware API calls outside the styletokens
// allowlist.
//
// Rule rationale: ADR-0032 §SD4 — the stroke ladder is fixed at
// 1.0 / 1.5 / 2.0 px (StrokeHair / StrokeRegular / StrokeStrong). Unlike
// L3 (spacing), strokes are density-independent perceptual constants —
// any thinner and they vanish on HiDPI displays. A raw literal in
// .Stroke() bypasses the ladder, so a "subtle divider" gets typed as
// 1.5 here, 1.3 over there, and the fleet drifts.
//
// v1 implementation note: detection is syntactic. We trigger on selector
// names matching the stroke-aware API surface (Stroke) and inspect
// **both** positional args because the binding surface has an overloaded
// signature: FrameFluid.Stroke(width, col) is width-first, whereas
// H3RegionFluid.Stroke(col, width) / MapPolylineFluid.Stroke(col, width)
// are color-first. A numeric BasicLit (INT or FLOAT) in either position
// is always the width because color args are color.Hex(...) CallExprs,
// not bare numbers — so the lit-vs-not-lit disambiguation is reliable
// without type info. Allowlist: 0 / 0.0 (sentinel "no stroke" mirror of
// L4's sharp-corners exemption); all other values must reach for
// styletokens.StrokeHair / StrokeRegular / StrokeStrong by name.
//
// Out of scope for v1: c.PaintRectStroke(...) / c.PaintCircleStroke(...)
// free functions. The positional shape (x1,y1,x2,y2,radius,color,width
// vs cx,cy,r,color,width) requires receiver-specific arg-position
// knowledge that the syntactic walker doesn't have. Defer to a v2 rule
// or a Painter-API simplification first.
//
// Allowlist files: the styletokens module itself, where the ladder
// constants legitimately live. Line-level ignore via
// `// designlint:ignore=L10 (reason)` handles the rare case where a
// non-token-source literal is intentional.
package l10stroke

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// allowlistedPkgPathSuffixes are package-path tails where literal stroke
// values legitimately live — the styletokens module's ladder constants.
var allowlistedPkgPathSuffixes = []string{
	"/public/keelson/designsystem/styletokens",
}

// triggerSelectors are the stroke-aware method names that take a numeric
// arg representing stroke width in px. Detection is by suffix selector
// name only — works regardless of the FrameFluid / H3RegionFluid /
// MapPolylineFluid / TintedScopeFluid receiver and import-alias
// variations.
var triggerSelectors = map[string]bool{
	"Stroke": true,
}

// allowedLiterals are the values that may appear raw in stroke positions
// without referring to a token — 0 (sentinel "no stroke") mirrors L4's
// sharp-corner exemption. The ladder values 1.0 / 1.5 / 2.0 must reach
// for styletokens.StrokeHair / StrokeRegular / StrokeStrong by name.
var allowedLiterals = map[string]bool{
	"0":   true,
	"0.0": true,
}

// Analyzer is the L10 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l10stroke",
	Doc:      "L10: flag literal numeric stroke-width values in stroke-aware API calls outside the styletokens module (ADR-0032 §SD4).",
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

		var (
			file *ast.File
			idx  *ignoreann.Index
		)
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok {
				continue
			}
			if lit.Kind != token.INT && lit.Kind != token.FLOAT {
				continue
			}
			if allowedLiterals[lit.Value] {
				continue
			}
			if file == nil {
				file = findFile(stack)
				cached, hit := ignoreByFile[file]
				if !hit {
					cached = ignoreann.Build(pass.Fset, file)
					ignoreByFile[file] = cached
				}
				idx = cached
			}
			if idx.Suppressed(call.Pos(), "L10") {
				continue
			}

			pass.ReportRangef(call,
				"L10: raw literal %s in stroke-aware call .%s(); use a styletokens accessor (StrokeHair / StrokeRegular / StrokeStrong — ADR-0032 §SD4); annotate with // designlint:ignore=L10 (reason) if intentional",
				lit.Value, sel.Sel.Name)
		}
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
