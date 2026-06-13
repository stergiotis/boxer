package codelint

import (
	"fmt"
	"go/ast"
	"go/token"
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

// RuleCS010 — single-iterator types must use a canonical iterator
// method name.
//
// The standard's quartet (All/Values/Keys/Backward) describes the
// single-collection-per-receiver case. Types that legitimately expose
// multiple distinct iterations (e.g. graggle's LiveChildren,
// ForwardEdges, DeletedPartitionMembers) use domain-describing names
// and are out of scope — this rule only fires when a receiver has
// exactly one iter-returning method whose name isn't in the quartet.
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

type cs010Method struct {
	name string
	pos  token.Pos
}

func (inst *RuleCS010) run(pass *analysis.Pass) (res any, err error) {
	byReceiver := make(map[*types.TypeName][]cs010Method)
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
			recv := receiverTypeName(fd, pass.TypesInfo)
			if recv == nil {
				continue
			}
			byReceiver[recv] = append(byReceiver[recv], cs010Method{
				name: fd.Name.Name,
				pos:  fd.Name.Pos(),
			})
		}
	}
	for _, methods := range byReceiver {
		if len(methods) != 1 {
			continue
		}
		m := methods[0]
		if _, allowed := allowedIterMethodNames[m.name]; allowed {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:     m.pos,
			Message: fmt.Sprintf("CS010: sole iterator method %q on its type should be named All / Values / Keys / Backward", m.name),
		})
	}
	return
}

// receiverTypeName returns the *types.TypeName for the receiver of a
// method, normalising pointer receivers, or nil if anything in the
// chain is missing.
func receiverTypeName(fd *ast.FuncDecl, info *types.Info) (tn *types.TypeName) {
	obj := info.Defs[fd.Name]
	if obj == nil {
		return
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return
	}
	t := sig.Recv().Type()
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = ptr.Elem()
	}
	named, isNamed := t.(*types.Named)
	if !isNamed {
		return
	}
	tn = named.Obj()
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
