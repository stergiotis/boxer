//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RuleCS004 — prefer typed sync/atomic over the legacy free-function API.
//
// CODINGSTANDARDS.md "Concurrency Patterns → Atomics" requires the typed
// forms introduced in Go 1.19 (atomic.Int64, atomic.Pointer[T], …) over
// the original atomic.LoadInt64(&v) / atomic.StoreInt64(&v, x) /
// atomic.AddInt64(&v, d) / atomic.SwapInt64(&v, x) /
// atomic.CompareAndSwapInt64(&v, old, new) family.
//
// Detection is by package + receiver-shape rather than a hard-coded
// function list: any call to a package-level (no-receiver) function in
// sync/atomic is, by construction, the legacy API. The typed forms are
// methods on atomic.Int64 etc. and therefore have a non-nil receiver.
type RuleCS004 struct{}

func NewRuleCS004() (inst *RuleCS004) {
	inst = &RuleCS004{}
	return
}

func (inst *RuleCS004) Id() (id string) {
	id = "CS004"
	return
}

func (inst *RuleCS004) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS004) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs004",
		Doc:  "CS004: prefer typed sync/atomic (atomic.Int64, atomic.Pointer[T]) over Load*/Store*/Add*/Swap*/CompareAndSwap* free functions",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS004) run(pass *analysis.Pass) (res any, err error) {
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
			if obj == nil || obj.Pkg() == nil {
				return
			}
			if obj.Pkg().Path() != "sync/atomic" {
				return
			}
			fn, isFn := obj.(*types.Func)
			if !isFn {
				return
			}
			sig, isSig := fn.Type().(*types.Signature)
			if !isSig {
				return
			}
			if sig.Recv() != nil {
				// Method call on a typed atomic — exactly what the standard prefers.
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: fmt.Sprintf("CS004: atomic.%s — prefer typed atomic (atomic.Int64.Load, atomic.Pointer[T].Store, etc.)", fn.Name()),
			})
			return
		})
	}
	return
}
