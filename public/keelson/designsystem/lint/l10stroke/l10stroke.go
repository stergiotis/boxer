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
// Painter free functions are a second trigger class: their coordinate
// args are bare numeric literals too, so the inspect-every-arg approach
// above would false-positive on cx/cy/rx/ry. Instead each registered
// function carries its strokeWidth arg index (the per-name table idiom
// L11 uses for durSecs). All three painter strokes are registered —
// PaintCircleStroke / PaintEllipseStroke / PaintRectStroke. A new
// painter surface with a stroke width joins the table the same way;
// registration expands the trigger surface, so sweep the new callers
// in the same change.
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

// triggerFunctions maps painter free-function names to the 0-indexed
// arg position where the strokeWidth float32 sits. Unlike the method
// surface, the surrounding args are coordinates — also bare numeric
// literals — so only the registered width position is inspected.
// Registering a new function here expands the trigger surface: sweep
// its callers in the same change.
var triggerFunctions = map[string]int{
	"PaintCircleStroke":  4, // (cx, cy, radius, col, strokeWidth)
	"PaintEllipseStroke": 5, // (cx, cy, rx, ry, col, strokeWidth)
	"PaintRectStroke":    6, // (minX, minY, maxX, maxY, rounding, col, strokeWidth)
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

func run(pass *analysis.Pass) (result any, err error) {
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
		name := sel.Sel.Name
		var (
			candidates []ast.Expr
			display    string
		)
		switch widthIdx, isFn := triggerFunctions[name]; {
		case triggerSelectors[name]:
			candidates = call.Args
			display = "." + name
		case isFn:
			if widthIdx >= len(call.Args) {
				return
			}
			candidates = call.Args[widthIdx : widthIdx+1]
			display = name
		default:
			return
		}

		var (
			file *ast.File
			idx  *ignoreann.Index
		)
		for _, arg := range candidates {
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
				"L10: raw literal %s in stroke-aware call %s(); use a styletokens accessor (StrokeHair / StrokeRegular / StrokeStrong — ADR-0032 §SD4); annotate with // designlint:ignore=L10 (reason) if intentional",
				lit.Value, display)
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
