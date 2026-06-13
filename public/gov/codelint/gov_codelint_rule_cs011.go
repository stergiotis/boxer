package codelint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// envPkgPath is the only package whose own calls to os.Getenv /
// os.LookupEnv / os.Environ / syscall.Getenv are exempt: it is the
// project's sanctioned env-var registry.
const envPkgPath = "github.com/stergiotis/boxer/public/config/env"

// cs011Banned maps "<pkg>.<func>" to the suggestion text. Membership
// is the ban; the suggestion is shown in the diagnostic.
var cs011Banned = map[string]string{
	"os.Getenv":      "declare via public/config/env (env.NewString / env.NewInt / …) and read with .Get(ctx)",
	"os.LookupEnv":   "declare via public/config/env and read with .Lookup(ctx)",
	"os.Environ":     "enumerate registered env vars via the public/config/env registry, not the raw process environment",
	"syscall.Getenv": "declare via public/config/env (env.NewString / env.NewInt / …) and read with .Get(ctx)",
}

// RuleCS011 — direct process-environment access is prohibited.
//
// CODINGSTANDARDS.md "Configuration → Environment Variables" and
// ADR-0009 require every env var to flow through public/config/env so
// declarations are discoverable, typed, doc-generated, and protected
// from lowercase-name / typo defects. The env package itself is the
// only sanctioned implementer of these calls.
//
// Subsumes the original env/lint_test.go enforcer and additionally
// covers os.Environ (which the test had not modelled).
type RuleCS011 struct{}

func NewRuleCS011() (inst *RuleCS011) {
	inst = &RuleCS011{}
	return
}

func (inst *RuleCS011) Id() (id string) {
	id = "CS011"
	return
}

func (inst *RuleCS011) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS011) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs011",
		Doc:  "CS011: direct env-var access (os.Getenv / os.LookupEnv / os.Environ / syscall.Getenv) is prohibited; use public/config/env",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS011) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil {
		path := pass.Pkg.Path()
		if path == envPkgPath || strings.HasPrefix(path, envPkgPath+"/") {
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
			obj := pass.TypesInfo.Uses[sel.Sel]
			if obj == nil {
				return
			}
			fn, isFn := obj.(*types.Func)
			if !isFn || fn.Pkg() == nil {
				return
			}
			key := fn.Pkg().Path() + "." + fn.Name()
			suggestion, banned := cs011Banned[key]
			if !banned {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: fmt.Sprintf("CS011: %s — %s", key, suggestion),
			})
			return
		})
	}
	return
}
