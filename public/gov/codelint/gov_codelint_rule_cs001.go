//go:build llm_generated_opus47

package codelint

import (
	"go/ast"
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
// implementation itself.
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
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			call, ok := n.(*ast.CallExpr)
			if !ok {
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
				Message: "CS001: fmt.Errorf outside public/observability/eh — use eh.Errorf",
			})
			return
		})
	}
	return
}
