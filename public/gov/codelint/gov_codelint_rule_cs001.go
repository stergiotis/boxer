package codelint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ehPkgPath is the package whose own uses of fmt.Errorf are exempted —
// eh is the standard's sanctioned Errorf wrapper, and its implementation
// is allowed to call fmt directly. Sub-packages (eg eh/eb) are exempted
// by prefix.
const ehPkgPath = "github.com/stergiotis/boxer/public/observability/eh"

// RuleCS001 — fmt.Errorf outside the eh package.
//
// CODINGSTANDARDS.md "Error Handling → Simple Wrapping" requires
// eh.Errorf for error construction so that stack traces and structured
// context are preserved. fmt.Errorf is allowed only inside the eh
// implementation itself, and for a wrapped sentinel: the value of a
// package-level variable, `var ErrX = fmt.Errorf("…: %w", ErrBase)`, whose
// format is a literal with a %w and whose every other argument is a
// package-level error variable. That form records no stack — eh would
// record the init stack (CS019) — and keeps errors.Is(ErrX, ErrBase),
// which errors.New would lose.
type RuleCS001 struct{}

func NewRuleCS001() (inst *RuleCS001) {
	inst = &RuleCS001{}
	return
}

func (inst *RuleCS001) Id() (id string) {
	id = "CS001"
	return
}

func (inst *RuleCS001) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS001) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs001",
		Doc:  "CS001: use eh.Errorf instead of fmt.Errorf outside the eh package",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS001) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil {
		path := pass.Pkg.Path()
		if path == ehPkgPath || strings.HasPrefix(path, ehPkgPath+"/") {
			return
		}
	}
	for _, file := range pass.Files {
		sentinels := wrappedSentinelCalls(pass, file)
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return
			}
			if sentinels[call] {
				return
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return
			}
			if sel.Sel.Name != "Errorf" {
				return
			}
			obj := pass.TypesInfo.Uses[sel.Sel]
			if obj == nil || obj.Pkg() == nil {
				return
			}
			if obj.Pkg().Path() != "fmt" {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: "CS001: fmt.Errorf outside public/observability/eh — use eh.Errorf (a package-level sentinel wrapping other sentinels may use fmt.Errorf with %w)",
			})
			return
		})
	}
	return
}

// wrappedSentinelCalls are the calls in file that are the whole value of a
// package-level variable and wrap only package-level error variables
// under a literal format holding %w.
func wrappedSentinelCalls(pass *analysis.Pass, file *ast.File) (calls map[*ast.CallExpr]bool) {
	errorType := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				call, ok := v.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					continue
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				format, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.Contains(format, "%w") {
					continue
				}
				all := true
				for _, a := range call.Args[1:] {
					if !isPackageLevelError(pass, a, errorType) {
						all = false
						break
					}
				}
				if !all {
					continue
				}
				if calls == nil {
					calls = make(map[*ast.CallExpr]bool)
				}
				calls[call] = true
			}
		}
	}
	return
}

// isPackageLevelError reports an identifier or a qualified identifier
// naming a package-level variable of a type implementing error.
func isPackageLevelError(pass *analysis.Pass, e ast.Expr, errorType *types.Interface) (ok bool) {
	var id *ast.Ident
	switch x := e.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return
	}
	v, isVar := pass.TypesInfo.Uses[id].(*types.Var)
	if !isVar || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
		return
	}
	return types.Implements(v.Type(), errorType)
}
