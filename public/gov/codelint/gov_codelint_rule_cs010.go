//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// allowedIterMethodNames are the canonical iterator-method names per
// CODINGSTANDARDS.md "Iteration → Naming". Free functions returning
// iter.Seq[T] are not checked: the standard expects iterators to be
// implemented as methods, which is a separate concern.
var allowedIterMethodNames = map[string]struct{}{
	"All":      {},
	"Values":   {},
	"Keys":     {},
	"Backward": {},
}

// RuleCS010 — iter-returning methods must use canonical names.
type RuleCS010 struct{}

func NewRuleCS010() (inst *RuleCS010) {
	inst = &RuleCS010{}
	return
}

func (inst *RuleCS010) Id() (id string) {
	id = "CS010"
	return
}

func (inst *RuleCS010) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS010) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs010",
		Doc:  "CS010: methods returning iter.Seq[T] / iter.Seq2[K,V] must be named All / Values / Keys / Backward",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS010) run(pass *analysis.Pass) (res any, err error) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv == nil {
				continue
			}
			if fd.Type == nil || fd.Type.Results == nil {
				continue
			}
			if !returnsIterSeq(fd.Type.Results, pass.TypesInfo) {
				continue
			}
			if _, allowed := allowedIterMethodNames[fd.Name.Name]; allowed {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:     fd.Name.Pos(),
				End:     fd.Name.End(),
				Message: fmt.Sprintf("CS010: iterator method %q should be named All / Values / Keys / Backward", fd.Name.Name),
			})
		}
	}
	return
}

// returnsIterSeq reports whether any of the result fields resolves to
// iter.Seq[…] or iter.Seq2[…]. Returning iter.Seq alongside other
// values still counts — the method is exposing iteration.
func returnsIterSeq(results *ast.FieldList, info *types.Info) (ok bool) {
	for _, field := range results.List {
		if isIterSeqType(info.TypeOf(field.Type)) {
			ok = true
			return
		}
	}
	return
}

func isIterSeqType(t types.Type) (ok bool) {
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
	if obj.Pkg().Path() != "iter" {
		return
	}
	name := obj.Name()
	ok = name == "Seq" || name == "Seq2"
	return
}
