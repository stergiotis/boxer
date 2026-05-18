//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RuleCS003 — sync.Mutex / sync.RWMutex fields must be by value.
//
// CODINGSTANDARDS.md "Concurrency Patterns → Mutexes" requires the
// mutex to live by value so zero-valued struct usage is safe and no
// extra heap allocation happens per instance. Function parameters
// taking *sync.Mutex are not flagged — passing-by-pointer is a normal
// caller-side mechanic and the standard's concern is where the mutex
// is owned, not how it is borrowed.
//
// Both named and embedded pointer-mutex fields are flagged.
type RuleCS003 struct{}

func NewRuleCS003() (inst *RuleCS003) {
	inst = &RuleCS003{}
	return
}

func (inst *RuleCS003) Id() (id string) {
	id = "CS003"
	return
}

func (inst *RuleCS003) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS003) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs003",
		Doc:  "CS003: sync.Mutex / sync.RWMutex struct fields must be by value, not pointer",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS003) run(pass *analysis.Pass) (res any, err error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			st, ok := n.(*ast.StructType)
			if !ok {
				return
			}
			if st.Fields == nil {
				return
			}
			for _, field := range st.Fields.List {
				t := pass.TypesInfo.TypeOf(field.Type)
				name, isPtrMutex := mutexPointerName(t)
				if !isPtrMutex {
					continue
				}
				pass.Report(analysis.Diagnostic{
					Pos:     field.Pos(),
					End:     field.End(),
					Message: fmt.Sprintf("CS003: %s field should be by value, not pointer", name),
				})
			}
			return
		})
	}
	return
}

// mutexPointerName returns ("sync.Mutex"|"sync.RWMutex", true) when t
// is a pointer to one of those types, and ("", false) otherwise.
func mutexPointerName(t types.Type) (name string, ok bool) {
	if t == nil {
		return
	}
	ptr, isPtr := t.(*types.Pointer)
	if !isPtr {
		return
	}
	named, isNamed := ptr.Elem().(*types.Named)
	if !isNamed {
		return
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return
	}
	if obj.Pkg().Path() != "sync" {
		return
	}
	switch obj.Name() {
	case "Mutex", "RWMutex":
		name = "sync." + obj.Name()
		ok = true
	}
	return
}
