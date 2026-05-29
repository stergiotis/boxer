//go:build llm_generated_opus47

// Package l1labelcasing implements IDS Tier 1 rule L1 (v1 partial) — flag
// the obvious lowercase-first-letter typos in c.Label(...) call sites.
//
// Rule rationale: ADR-0029 §SD8 — fleet-wide label casing cohesion is
// the kind of consistency human reviewers tire of pointing out. The
// catalogue at tier1-mechanical.md spells out a full per-widget casing
// policy (Title Case for buttons, Sentence case for menu items, etc.);
// detecting every variant requires walking the AST to identify the
// enclosing widget call, since labels flow through atom builders
// (`c.Atoms().Text("Save").Keep()`) rather than the first string arg
// to a widget.
//
// v1 scope is intentionally narrow:
//
//   - Triggers only on `c.Label(LIT)` (direct labels) — the
//     unambiguous user-visible string surface.
//   - Flags only string literals whose first Unicode letter is
//     lowercase. Skips: empty strings, leading non-letter (digit,
//     parenthesis, symbol — common for "(loading...)" / "404 — …"
//     status fragments), already-uppercase first letters.
//   - All-uppercase / mixed-Title casing decisions are deferred to
//     v2, when the AST context walker can distinguish Button labels
//     (Title Case) from CollapsingHeader titles (Section Case) etc.
//
// Even at this narrow scope the rule catches the canonical
// "save changes" bug and seeds the v2 expansion.
package l1labelcasing

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// allowlistedPkgPathSuffixes are package-path tails where literal labels
// legitimately appear without policy enforcement — currently empty; the
// styletokens module doesn't construct UI labels.
var allowlistedPkgPathSuffixes = []string{}

// Analyzer is the L1 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l1labelcasing",
	Doc:      "L1 (v1 partial): flag c.Label(\"...\") strings whose first Unicode letter is lowercase (ADR-0029 §SD8).",
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
		if sel.Sel.Name != "Label" {
			return
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return
		}
		if ident.Name != "c" {
			return
		}
		if len(call.Args) == 0 {
			return
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return
		}
		if lit.Kind != token.STRING {
			return
		}
		text, unquoteErr := strconv.Unquote(lit.Value)
		if unquoteErr != nil {
			return
		}
		if !leadsWithLowercaseLetter(text) {
			return
		}

		file := findFile(stack)
		idx, ok := ignoreByFile[file]
		if !ok {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		if idx.Suppressed(call.Pos(), "L1") {
			return
		}

		pass.ReportRangef(call,
			"L1: label %s starts with a lowercase letter; UI labels use Sentence/Title case (ADR-0029 §SD8); annotate with // designlint:ignore=L1 (reason) if intentional",
			quoteForReport(text))
		return
	})
	return
}

// leadsWithLowercaseLetter returns true when the first non-whitespace
// character of s is a lowercase Unicode letter. Whitespace is skipped to
// tolerate accidental leading tabs/spaces. Status fragments and
// parenthetical labels such as "(loading…)" or "404 — not found" start
// with a non-letter and pass cleanly — L1 v1 deliberately doesn't police
// those.
func leadsWithLowercaseLetter(s string) (ok bool) {
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if !unicode.IsLetter(r) {
			return
		}
		ok = unicode.IsLower(r)
		return
	}
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

// quoteForReport renders a string for the diagnostic message. The
// strconv.Quote round-trip mirrors what `go/ast` would have shown
// the user in the source, including escape sequences.
func quoteForReport(s string) (q string) {
	q = strconv.Quote(s)
	return
}
