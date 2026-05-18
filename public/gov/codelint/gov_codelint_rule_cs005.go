//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// RuleCS005 — declared interface names must end with capital 'I'.
//
// CODINGSTANDARDS.md "Naming & Style → Interface Naming" requires the
// suffix so interface vs concrete is visible at every use site. Only
// direct interface declarations are checked; anonymous inline
// interfaces (e.g. in a function parameter list) have no name, and
// type aliases to an interface are deliberately out of scope here
// because CS008 will reject the alias outright.
type RuleCS005 struct{}

func NewRuleCS005() (inst *RuleCS005) {
	inst = &RuleCS005{}
	return
}

func (inst *RuleCS005) Id() (id string) {
	id = "CS005"
	return
}

func (inst *RuleCS005) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS005) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs005",
		Doc:  "CS005: declared interface names must end with capital 'I'",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS005) run(pass *analysis.Pass) (res any, err error) {
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
				if _, isIface := ts.Type.(*ast.InterfaceType); !isIface {
					continue
				}
				name := ts.Name.Name
				if endsWithCapitalI(name) {
					continue
				}
				pass.Report(analysis.Diagnostic{
					Pos:     ts.Name.Pos(),
					End:     ts.Name.End(),
					Message: fmt.Sprintf("CS005: interface name %q should end with capital 'I'", name),
				})
			}
		}
	}
	return
}

func endsWithCapitalI(name string) (ok bool) {
	if name == "" {
		return
	}
	ok = name[len(name)-1] == 'I'
	return
}
