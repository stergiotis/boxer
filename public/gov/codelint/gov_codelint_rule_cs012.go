package codelint

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// extbinPkgPath is the one package allowed to call os/exec directly — it is
// boxer's sanctioned external-process chokepoint. ehPkgPath is exempt too: eh
// sits below extbin in the import graph (extbin imports eh), so eh's own
// os/exec use (go env GOROOT for stack-trace shortening) cannot route through
// extbin without a cycle.
const extbinPkgPath = "github.com/stergiotis/boxer/public/extbin"

// execBannedFuncs are the os/exec entry points that resolve or spawn an
// external process. Routing them through extbin makes boxer's host-binary
// surface one auditable registry (see ADR-0118).
var execBannedFuncs = map[string]struct{}{
	"Command":        {},
	"CommandContext": {},
	"LookPath":       {},
}

// RuleCS012 — os/exec.Command/CommandContext/LookPath outside package extbin.
//
// Every external program boxer spawns must be resolved through the extbin
// registry so the set of host binaries the toolkit can invoke stays
// enumerable — a supply-chain concern for a toolkit that ships airgapped.
// Test files are exempt: fixtures may shell out freely, and tests are not part
// of the shipped runtime surface.
type RuleCS012 struct{}

func NewRuleCS012() (inst *RuleCS012) {
	inst = &RuleCS012{}
	return
}

func (inst *RuleCS012) Id() (id string) {
	id = "CS012"
	return
}

func (inst *RuleCS012) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS012) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs012",
		Doc:  "CS012: resolve external programs via extbin, not os/exec directly",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS012) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil {
		path := pass.Pkg.Path()
		if path == extbinPkgPath || strings.HasPrefix(path, extbinPkgPath+"/") ||
			path == ehPkgPath || strings.HasPrefix(path, ehPkgPath+"/") {
			return
		}
	}
	for _, file := range pass.Files {
		// Test fixtures may shell out directly; they are not shipped runtime.
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
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
			if _, banned := execBannedFuncs[sel.Sel.Name]; !banned {
				return
			}
			obj := pass.TypesInfo.Uses[sel.Sel]
			if obj == nil || obj.Pkg() == nil {
				return
			}
			if obj.Pkg().Path() != "os/exec" {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: "CS012: os/exec." + sel.Sel.Name + " outside public/extbin — resolve external programs through the extbin registry",
			})
			return
		})
	}
	return
}
