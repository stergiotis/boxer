package codelint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// An app's task API comes from `task.ForApp(ctx)` (ADR-0038; the
// keelson-app skill's W2): the mount context supplies the app's identity,
// instance key, run id, logger and mount cancel, so a task the app spawns
// is listed under the app and cancelled with its window. `task.NewBusApi`
// built from a bus alone compiles and lists tasks, but the tasks carry no
// instance key and outlive the mount. The runtime builds its own APIs with
// `NewBusApi` where there is no mount context — the host's dialogs, the
// constructor behind `ForApp` itself — so the runtime tree is exempt as a
// whole, by package path.
const (
	taskPkgPath        = "github.com/stergiotis/boxer/public/keelson/runtime/task"
	runtimePkgPath     = "github.com/stergiotis/boxer/public/keelson/runtime"
	taskNewBusApiFunc  = "NewBusApi"
	taskForAppFunc     = "ForApp"
	cs016RemedySummary = "build the app's task API with task." + taskForAppFunc + "(ctx) from the mount context"
)

// RuleCS016 — `task.NewBusApi` used outside the keelson runtime tree. The
// runtime packages are exempt by path prefix; test files are exempt, as a
// test wires a bus by hand.
type RuleCS016 struct{}

func NewRuleCS016() (inst *RuleCS016) {
	inst = &RuleCS016{}
	return
}

func (inst *RuleCS016) Id() (id string) {
	id = "CS016"
	return
}

func (inst *RuleCS016) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS016) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs016",
		Doc:  "CS016: " + cs016RemedySummary + ", not task." + taskNewBusApiFunc,
		Run:  inst.run,
	}
	return
}

// cs016Exempt reports whether a package path lies in the keelson runtime
// tree — the runtime package itself or any package below it — where
// `NewBusApi` is the constructor the runtime wires by hand.
func cs016Exempt(path string) bool {
	return path == runtimePkgPath || strings.HasPrefix(path, runtimePkgPath+"/")
}

// isTaskNewBusApi reports whether obj is the task package's bus constructor.
func isTaskNewBusApi(obj types.Object) (is bool) {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Name() != taskNewBusApiFunc || fn.Pkg() == nil {
		return
	}
	is = fn.Pkg().Path() == taskPkgPath
	return
}

func (inst *RuleCS016) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil && cs016Exempt(pass.Pkg.Path()) {
		return
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != taskNewBusApiFunc || !isTaskNewBusApi(pass.TypesInfo.Uses[id]) {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     id.Pos(),
				End:     id.End(),
				Message: "CS016: task." + taskNewBusApiFunc + " outside the keelson runtime carries no instance key, mount cancel or lifetime — " + cs016RemedySummary,
			})
			return
		})
	}
	return
}
