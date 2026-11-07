// Package l16hexcolor implements IDS Tier 1 rule L16 — flag the colour
// literals rule L2 does not see: a `color.Hex(0x…)` call with a numeric
// literal argument, and a `const` / `var` declaration whose name says it is
// a colour, whose type is uint32 (or a type with that underlying) or absent,
// and whose initialiser is a numeric literal.
//
// Rule rationale: ADR-0031 — every colour in IDS-conformant apps comes from
// the semantic palette, the data-encoding palette or the neutrals. L2 covers
// the `color.RGB` / `color.RGBA` constructors; the hex constructor and a
// packed uint32 passed where a colour is expected carry the same raw literal
// past it, which the keelson-app adherence survey found in several apps.
//
// Detection is syntactic for the call, as in L2: `<ident "color">.Hex(<INT
// BasicLit>)`. `color.Hex(styletokens.X.AsHex())` and any other non-literal
// argument pass, because the argument is not a BasicLit. The declaration
// check uses type information for the uint32 test and a name heuristic
// (`color`, `colour`, case-insensitive; `Hex`, `Tone`) so a uint32 that is
// a count or a bitmask does not trigger. A colour literal that reaches a
// colour parameter through an untyped or unnamed path is a known false
// negative; the rule does not follow dataflow.
//
// The fully transparent sentinel (0, 0x0, 0x00000000) is allowed raw, as
// L3 / L4 / L10 allow their disabling sentinels: it is "no colour", not a
// palette choice. Generated files and `_test.go` files are skipped: a test
// asserting a colour round-trip needs the literal. The colour package, styletokens
// and the generated data-encoding LUTs are allowlisted, as for L2.
package l16hexcolor

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// RuleID is the catalogue identifier and the `designlint:ignore=` key.
const RuleID = "L16"

// allowlistedPkgPathSuffixes mirrors L2's allowlist: the colour package
// itself, the styletokens mirror and the generated data_encoding LUTs.
var allowlistedPkgPathSuffixes = []string{
	"/public/thestack/imzero2/egui2/widgets/color",
	"/public/keelson/designsystem/styletokens",
	"/public/keelson/designsystem/styletokens/data_encoding",
}

// allowedLiterals are the literal spellings that may appear raw: the fully
// transparent sentinel, which disables a fill or a stroke rather than
// choosing a colour.
var allowedLiterals = map[string]bool{
	"0":          true,
	"0x0":        true,
	"0x00000000": true,
}

// Analyzer is the L16 analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l16hexcolor",
	Doc:      "L16: flag color.Hex(<literal>) calls and uint32 colour declarations initialised with a literal outside the token module (ADR-0031).",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (result any, err error) {
	if pkgIsAllowlisted(pass.Pkg.Path()) {
		return
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ignoreByFile := make(map[*ast.File]*ignoreann.Index)
	skipFile := make(map[*ast.File]bool)
	suppressed := func(file *ast.File, pos token.Pos) (ok bool) {
		idx, found := ignoreByFile[file]
		if !found {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		ok = idx.Suppressed(pos, RuleID)
		return
	}
	skip := func(file *ast.File) (ok bool) {
		ok, found := skipFile[file]
		if !found {
			ok = file == nil || ast.IsGenerated(file) || isTestFile(pass.Fset, file)
			skipFile[file] = ok
		}
		return
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil), (*ast.ValueSpec)(nil)}
	insp.WithStack(nodeFilter, func(n ast.Node, push bool, stack []ast.Node) (proceed bool) {
		proceed = true
		if !push {
			return
		}
		file := findFile(stack)
		if skip(file) {
			return
		}
		switch v := n.(type) {
		case *ast.CallExpr:
			lit, ok := hexCallLiteral(v)
			if !ok || suppressed(file, v.Pos()) {
				return
			}
			pass.ReportRangef(v,
				"L16: raw color.Hex(%s) literal outside token module; use a styletokens palette constant, e.g. color.Hex(styletokens.<Role>.AsHex()) (ADR-0031 §SD6); annotate with // designlint:ignore=L16 (reason) if intentional",
				lit.Value)
		case *ast.ValueSpec:
			for i, name := range v.Names {
				if i >= len(v.Values) {
					break
				}
				lit, ok := colourDeclLiteral(pass, v, name, v.Values[i])
				if !ok || suppressed(file, name.Pos()) {
					continue
				}
				pass.ReportRangef(lit,
					"L16: colour literal %s initialises %s; use a styletokens palette constant (ADR-0031 §SD6); annotate with // designlint:ignore=L16 (reason) if intentional",
					lit.Value, name.Name)
			}
		}
		return
	})
	return
}

// hexCallLiteral reports a `color.Hex(<INT literal>)` call and returns the
// literal. Any other argument shape — a token's AsHex(), a variable — passes.
func hexCallLiteral(call *ast.CallExpr) (lit *ast.BasicLit, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Hex" {
		return
	}
	ident, isIdent := sel.X.(*ast.Ident)
	if !isIdent || ident.Name != "color" || len(call.Args) != 1 {
		return
	}
	lit, ok = intLiteral(call.Args[0])
	return
}

// colourDeclLiteral reports a declared name that is a colour by name and
// is initialised with an integer literal: typed, when the declared type's
// underlying type is uint32; untyped (a const with no type, or one that
// inherits the type of an earlier spec in its group), when the literal is
// written in hex.
func colourDeclLiteral(pass *analysis.Pass, spec *ast.ValueSpec, name *ast.Ident, value ast.Expr) (lit *ast.BasicLit, ok bool) {
	if !IsColourName(name.Name) {
		return
	}
	lit, isLit := intLiteral(value)
	if !isLit {
		return
	}
	if spec.Type == nil {
		ok = isHexLiteral(lit)
		return
	}
	declType := pass.TypesInfo.TypeOf(spec.Type)
	if declType == nil {
		return
	}
	basic, isBasic := declType.Underlying().(*types.Basic)
	ok = isBasic && basic.Kind() == types.Uint32
	return
}

// IsColourName reports whether an identifier names a colour by the rule's
// heuristic: it contains `color` or `colour` in any case, or `Hex` or `Tone`
// as written (the lower-case forms match `hexdump` and `stone`).
func IsColourName(name string) (ok bool) {
	lower := strings.ToLower(name)
	ok = strings.Contains(lower, "color") || strings.Contains(lower, "colour") ||
		strings.Contains(name, "Hex") || strings.Contains(name, "Tone")
	return
}

// intLiteral reports an integer literal that is not an allowed sentinel.
func intLiteral(expr ast.Expr) (lit *ast.BasicLit, ok bool) {
	lit, ok = expr.(*ast.BasicLit)
	if !ok {
		return
	}
	ok = lit.Kind == token.INT && !allowedLiterals[strings.ToLower(lit.Value)]
	return
}

func isHexLiteral(lit *ast.BasicLit) (ok bool) {
	ok = strings.HasPrefix(lit.Value, "0x") || strings.HasPrefix(lit.Value, "0X")
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

func isTestFile(fset *token.FileSet, file *ast.File) (ok bool) {
	tf := fset.File(file.Pos())
	if tf == nil {
		return
	}
	ok = strings.HasSuffix(tf.Name(), "_test.go")
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
