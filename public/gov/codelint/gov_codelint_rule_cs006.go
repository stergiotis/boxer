//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RuleCS006 — enum type names must end with capital 'E'.
//
// CODINGSTANDARDS.md "Naming & Style → Enum Naming" requires the suffix
// so enum vs scalar is visible at every use site. Enums are detected
// structurally: a named type that appears as the declared type of two
// or more constants inside the same `const (...)` block is treated as
// an enum. iota-chained specs without an explicit Type are resolved
// via go/types, so the entire chain is counted.
//
// Single-value `const Foo BarE = …` declarations are not classified as
// enums (insufficient evidence). External-package types are not
// flagged — they are not ours to rename.
type RuleCS006 struct{}

func NewRuleCS006() (inst *RuleCS006) {
	inst = &RuleCS006{}
	return
}

func (inst *RuleCS006) Id() (id string) {
	id = "CS006"
	return
}

func (inst *RuleCS006) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS006) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs006",
		Doc:  "CS006: enum types (named types with 2+ values in a const block) must end with capital 'E'",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS006) run(pass *analysis.Pass) (res any, err error) {
	flagged := make(map[*types.TypeName]struct{})
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			counts := make(map[*types.TypeName]int)
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					obj := pass.TypesInfo.Defs[name]
					if obj == nil {
						continue
					}
					named, isNamed := obj.Type().(*types.Named)
					if !isNamed {
						continue
					}
					tn := named.Obj()
					if tn == nil || tn.Pkg() != pass.Pkg {
						continue
					}
					counts[tn]++
				}
			}
			for tn, c := range counts {
				if c < 2 {
					continue
				}
				if endsWithCapitalE(tn.Name()) {
					continue
				}
				if _, already := flagged[tn]; already {
					continue
				}
				flagged[tn] = struct{}{}
				pass.Report(analysis.Diagnostic{
					Pos:     tn.Pos(),
					Message: fmt.Sprintf("CS006: enum type %q should end with capital 'E' (e.g. %sE)", tn.Name(), tn.Name()),
				})
			}
		}
	}
	return
}

func endsWithCapitalE(name string) (ok bool) {
	if name == "" {
		return
	}
	ok = name[len(name)-1] == 'E'
	return
}
