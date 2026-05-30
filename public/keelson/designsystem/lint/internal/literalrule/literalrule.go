// Package literalrule factors out the analyzer shape shared by the Tier-1
// design-system rules that flag a raw numeric literal in a selector-named call
// (L3 spacing, L4 rounding): trigger on a call whose function is a selector
// whose name is in Triggers, inspect the first positional argument, and report
// when it is an INT/FLOAT BasicLit whose text is not in Allowed. Findings are
// suppressed in allowlisted packages and via `// designlint:ignore=<RuleID>`.
//
// Each rule package supplies a Spec (its trigger set, allowlist, message, etc.)
// and exposes NewAnalyzer(spec) as its Analyzer; the traversal lives here once.
package literalrule

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// Spec describes one literal-in-selector-call rule.
type Spec struct {
	// Name and Doc populate the analysis.Analyzer fields.
	Name string
	Doc  string
	// RuleID is the suppression key and the `designlint:ignore=` annotation id
	// (e.g. "L3").
	RuleID string
	// Triggers is the set of selector method names that take a numeric px-style
	// argument in the first position.
	Triggers map[string]bool
	// Allowed is the set of literal texts (e.g. "0", "0.0", "1") that may appear
	// raw without referring to a token.
	Allowed map[string]bool
	// AllowlistedPkgPathSuffixes are package-path tails where literals
	// legitimately live (typically the styletokens module itself).
	AllowlistedPkgPathSuffixes []string
	// MessageFormat is the ReportRangef format string; it receives
	// (lit.Value, sel.Sel.Name) as arguments.
	MessageFormat string
}

// NewAnalyzer returns the analysis.Analyzer implementing spec.
func NewAnalyzer(spec Spec) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     spec.Name,
		Doc:      spec.Doc,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run:      spec.run,
	}
}

func (spec Spec) run(pass *analysis.Pass) (result any, err error) {
	if spec.pkgIsAllowlisted(pass.Pkg.Path()) {
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
		if !spec.Triggers[sel.Sel.Name] {
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
		if spec.Allowed[lit.Value] {
			return
		}

		file := findFile(stack)
		idx, ok := ignoreByFile[file]
		if !ok {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		if idx.Suppressed(call.Pos(), spec.RuleID) {
			return
		}

		pass.ReportRangef(call, spec.MessageFormat, lit.Value, sel.Sel.Name)
		return
	})
	return
}

func (spec Spec) pkgIsAllowlisted(path string) (ok bool) {
	for _, suffix := range spec.AllowlistedPkgPathSuffixes {
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
