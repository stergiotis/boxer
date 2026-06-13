package codelint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RuleCS002 — context.Context must be the first parameter.
//
// CODINGSTANDARDS.md "Concurrency Patterns → Context" mandates that any
// function or method taking a context.Context places it as the first
// argument (receiver excluded). The check visits every *ast.FuncType
// so FuncDecl, FuncLit, interface methods, and function-typed fields
// are all covered with one walk.
//
// The "must have a ctx for I/O-bound work" half of the standard is
// judgment-based and not enforced here.
type RuleCS002 struct{}

func NewRuleCS002() (inst *RuleCS002) {
	inst = &RuleCS002{}
	return
}

func (inst *RuleCS002) Id() (id string) {
	id = "CS002"
	return
}

func (inst *RuleCS002) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS002) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs002",
		Doc:  "CS002: context.Context must be the first parameter",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS002) run(pass *analysis.Pass) (res any, err error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			ft, ok := n.(*ast.FuncType)
			if !ok {
				return
			}
			if ft.Params == nil {
				return
			}
			flatIdx := 0
			for _, field := range ft.Params.List {
				count := len(field.Names)
				if count == 0 {
					count = 1
				}
				t := pass.TypesInfo.TypeOf(field.Type)
				if isContextContext(t) && flatIdx > 0 {
					pass.Report(analysis.Diagnostic{
						Pos:     field.Pos(),
						End:     field.End(),
						Message: fmt.Sprintf("CS002: context.Context must be the first parameter, found at position %d", flatIdx),
					})
				}
				flatIdx += count
			}
			return
		})
	}
	return
}

// isContextContext reports whether t is the standard library
// context.Context named type (or a transparent alias for it).
func isContextContext(t types.Type) (ok bool) {
	if t == nil {
		return
	}
	named, isNamed := t.(*types.Named)
	if !isNamed {
		return
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return
	}
	ok = obj.Pkg().Path() == "context" && obj.Name() == "Context"
	return
}
