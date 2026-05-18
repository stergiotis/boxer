//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// RuleCS008 — type aliases are not allowed.
//
// CODINGSTANDARDS.md "Typing → Nominal Typing → No Aliases" prohibits
// the `type X = Y` form. Named-type declarations (`type X Y`) remain
// fine. Detection is purely positional: in an *ast.TypeSpec, the
// presence of the `=` token is recorded as a non-zero Assign position.
type RuleCS008 struct{}

func NewRuleCS008() (inst *RuleCS008) {
	inst = &RuleCS008{}
	return
}

func (inst *RuleCS008) Id() (id string) {
	id = "CS008"
	return
}

func (inst *RuleCS008) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS008) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs008",
		Doc:  "CS008: type aliases (type X = Y) are not allowed; declare a named type instead",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS008) run(pass *analysis.Pass) (res any, err error) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if ts.Assign == token.NoPos {
					continue
				}
				pass.Report(analysis.Diagnostic{
					Pos:     ts.Name.Pos(),
					End:     ts.End(),
					Message: fmt.Sprintf("CS008: type alias %q is not allowed (declare a named type instead)", ts.Name.Name),
				})
			}
		}
	}
	return
}
