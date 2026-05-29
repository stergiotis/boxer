//go:build llm_generated_opus47

// Package l11motion implements IDS Tier 1 rule L11 — flag literal numeric
// duration values in motion-aware (animate-*) API calls outside the
// styletokens allowlist.
//
// Rule rationale: ADR-0032 §SD5 — the motion ladder is fixed at
// 80 / 160 / 320 ms (MotionQuickMs / MotionStandardMs / MotionSlowMs;
// styletokens.MotionQuickSecs() / MotionStandardSecs() / MotionSlowSecs()
// surface them as the float32 seconds the egui binding API expects).
// Reduced-motion (OS preference / tour-capture mode) collapses every
// helper to zero — a raw literal in `.AnimateBoolWithTimeBind(...,
// 0.4, ...)` does not. The Tier 1 case is purely "literal durations
// fragment timing AND silently bypass reduced-motion".
//
// v1 implementation note: detection is syntactic. We trigger on selector
// names matching the duration-bearing animation API surface and inspect
// the per-selector durSecs arg position (index 2 for every wrapper
// today, but the table is per-name to leave room for future surfaces
// with a different signature). A numeric BasicLit (INT or FLOAT) in
// that position is the duration; anything else (variable, function call,
// type conversion) is treated as the canonical motion-token form and
// never triggers. Sentinel allowlist: 0 / 0.0 (instantaneous; matches
// the reduced-motion collapsed value).
//
// Known v1 limitation: named const fields like
// `defaultAnimDurSecs float32 = 0.28` evade detection because they're
// not BasicLit nodes inside the trigger call. Same shape as L3's
// `spacingPanelGap = 12` deferral; the motion-side cleanup is tracked
// alongside that work.
//
// Allowlist files: the styletokens module itself, where the ladder
// constants legitimately live. Line-level ignore via
// `// designlint:ignore=L11 (reason)` handles the rare case where a
// non-token-source literal is intentional.
package l11motion

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// allowlistedPkgPathSuffixes are package-path tails where literal
// duration values legitimately live — the styletokens module's ladder
// constants.
var allowlistedPkgPathSuffixes = []string{
	"/public/keelson/designsystem/styletokens",
}

// triggerSelectors maps animation-API selector names to the 0-indexed
// arg position where the durSecs float32 sits. Detection is by suffix
// selector name only — works regardless of the `c.*` / `bindings.*`
// receiver and import-alias variations. Every wrapper today has durSecs
// at index 2; the per-name table keeps room for future surfaces.
var triggerSelectors = map[string]int{
	"AnimateBoolWithTime":      2,
	"AnimateBoolWithTimeBind":  2,
	"AnimateValueWithTime":     2,
	"AnimateValueWithTimeBind": 2,
}

// allowedLiterals are the values that may appear raw in duration
// positions without referring to a token — 0 / 0.0 (instantaneous;
// matches the reduced-motion collapsed value of every motion helper).
var allowedLiterals = map[string]bool{
	"0":   true,
	"0.0": true,
}

// Analyzer is the L11 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l11motion",
	Doc:      "L11: flag literal numeric duration values in motion-aware (animate-*) API calls outside the styletokens module (ADR-0032 §SD5).",
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
		argIdx, ok := triggerSelectors[sel.Sel.Name]
		if !ok {
			return
		}
		if argIdx >= len(call.Args) {
			return
		}
		lit, ok := call.Args[argIdx].(*ast.BasicLit)
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
		if idx.Suppressed(call.Pos(), "L11") {
			return
		}

		pass.ReportRangef(call,
			"L11: raw literal %s in motion-aware call .%s() at arg %d; use a styletokens accessor (MotionQuickSecs / MotionStandardSecs / MotionSlowSecs — ADR-0032 §SD5; reduced-motion is gated through the accessor); annotate with // designlint:ignore=L11 (reason) if intentional",
			lit.Value, sel.Sel.Name, argIdx)
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
