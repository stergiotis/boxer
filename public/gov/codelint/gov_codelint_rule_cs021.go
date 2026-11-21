package codelint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	chclientPkgPath  = "github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	storeexecPkgPath = "github.com/stergiotis/boxer/public/keelson/data/storeexec"
)

// clientInsertFuncs are the ClickHouse client's insert entry points, keyed by
// types.Func.FullName. An insert sent through one of them reaches the server
// as it is: no insert transform a deployment registered for the table is
// applied (ADR-0402 §SD2).
var clientInsertFuncs = map[string]struct{}{
	"(*" + chclientPkgPath + ".Client).InsertArrow":         {},
	"(*" + chclientPkgPath + ".Client).InsertArrowSettings": {},
	"(*" + chclientPkgPath + ".Client).InsertStream":        {},
}

// RuleCS021 — an insert through the ClickHouse client, past the insert seam.
//
// A deployment may keep a table's rows in a table of its own by registering
// an insert transform (recordstore.RegisterInsertTransform, ADR-0402). The
// transform is applied by the record-store executors — storeexec.Executor
// over HTTP, chexec.LocalExecutor over clickhouse-local — and by nothing
// below them, so an insert handed to the client directly writes the
// untransformed shape under the table's own name. Insert through an
// executor. Test files are exempt; a writer whose table is its own and is
// never a transformed one (a benchmark's scratch table, a copy of native
// blocks between servers) carries a
// `//boxer:lint disable=CS021 reason="…"` directive saying why.
//
// The rule sees calls, not SQL text: an INSERT composed as a statement and
// sent through Exec is not found by it.
type RuleCS021 struct{}

func NewRuleCS021() (inst *RuleCS021) {
	inst = &RuleCS021{}
	return
}

func (inst *RuleCS021) Id() (id string) {
	id = "CS021"
	return
}

func (inst *RuleCS021) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS021) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs021",
		Doc:  "CS021: insert through a record-store executor, not the ClickHouse client",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS021) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil {
		switch pass.Pkg.Path() {
		case chclientPkgPath, storeexecPkgPath:
			return
		}
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return
			}
			// Uses, not only calls: a method value handed on as a function
			// inserts the same way.
			fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
			if !ok {
				return
			}
			name := fn.FullName()
			if _, banned := clientInsertFuncs[name]; !banned {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     sel.Pos(),
				End:     sel.End(),
				Message: "CS021: chclient.Client." + fn.Name() + " outside public/keelson/data/storeexec — insert through storeexec.Executor or another record-store executor, which applies the insert transform a deployment registered for the table (ADR-0402)",
			})
			return
		})
	}
	return
}
