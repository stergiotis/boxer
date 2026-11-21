package codelint

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// RuleCS019 — a package-level error built with eh or eb.
//
// eh.Errorf, eh.New and eb.Build capture a stack trace where they run. A
// sentinel (`var ErrX = eh.Errorf("…")`) runs during package
// initialisation, so every error that wraps it prints `runtime.doInit →
// init` instead of the failing path. A sentinel is errors.New: errors.Is
// matches it by identity, and the code that returns it wraps it with
// eh.Errorf / eb.Build at the site where the stack is worth recording.
type RuleCS019 struct{}

func NewRuleCS019() (inst *RuleCS019) {
	inst = &RuleCS019{}
	return
}

func (inst *RuleCS019) Id() (id string) {
	id = "CS019"
	return
}

func (inst *RuleCS019) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS019) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs019",
		Doc:  "CS019: build a package-level sentinel error with errors.New, not eh / eb",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS019) run(pass *analysis.Pass) (res any, err error) {
	for _, file := range pass.Files {
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
					ast.Inspect(v, func(n ast.Node) (cont bool) {
						cont = true
						if _, isLit := n.(*ast.FuncLit); isLit {
							// A function literal runs when called, not at init.
							return false
						}
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return
						}
						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return
						}
						obj := pass.TypesInfo.Uses[sel.Sel]
						if obj == nil || obj.Pkg() == nil {
							return
						}
						path := obj.Pkg().Path()
						if path != ehPkgPath && path != ehPkgPath+"/eb" {
							return
						}
						switch sel.Sel.Name {
						case "Errorf", "New", "ErrorfWithData", "Build":
						default:
							return
						}
						pass.Report(analysis.Diagnostic{
							Pos:     call.Pos(),
							End:     call.End(),
							Message: "CS019: package-level error built with " + obj.Pkg().Name() + "." + sel.Sel.Name + " records the init stack — use errors.New and wrap it where it is returned",
						})
						return false
					})
				}
			}
		}
	}
	return
}
