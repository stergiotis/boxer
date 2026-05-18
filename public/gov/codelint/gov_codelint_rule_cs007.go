//go:build llm_generated_opus47

package codelint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// RuleCS007 — enum values must be prefixed with the enum type name
// minus its trailing 'E'.
//
// CODINGSTANDARDS.md "Naming & Style → Enum Naming" — given a type
// WeekdayE, every value is expected to start with `Weekday`. Detection
// of *which* types are enums reuses CS006's per-block heuristic: a
// named type with 2+ constants in the same `const (...)` block is an
// enum. Once classified, every constant of that type (including
// stragglers in single-value declarations elsewhere) is checked.
//
// Types whose name does not end with 'E' are skipped here — CS006
// covers the type-name issue and double-flagging the same root cause
// is noise.
type RuleCS007 struct{}

func NewRuleCS007() (inst *RuleCS007) {
	inst = &RuleCS007{}
	return
}

func (inst *RuleCS007) Id() (id string) {
	id = "CS007"
	return
}

func (inst *RuleCS007) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS007) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs007",
		Doc:  "CS007: enum values must be prefixed with the type name minus the trailing 'E'",
		Run:  inst.run,
	}
	return
}

type cs007Value struct {
	name string
	pos  token.Pos
}

func (inst *RuleCS007) run(pass *analysis.Pass) (res any, err error) {
	allValues := make(map[*types.TypeName][]cs007Value)
	isEnum := make(map[*types.TypeName]bool)

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			blockCount := make(map[*types.TypeName]int)
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
					allValues[tn] = append(allValues[tn], cs007Value{name.Name, name.Pos()})
					blockCount[tn]++
				}
			}
			for tn, c := range blockCount {
				if c >= 2 {
					isEnum[tn] = true
				}
			}
		}
	}

	for tn, values := range allValues {
		if !isEnum[tn] {
			continue
		}
		typeName := tn.Name()
		if !endsWithCapitalE(typeName) {
			continue
		}
		prefix := typeName[:len(typeName)-1]
		if prefix == "" {
			continue
		}
		for _, v := range values {
			if strings.HasPrefix(v.name, prefix) {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:     v.pos,
				Message: fmt.Sprintf("CS007: enum value %q should be prefixed with %q (enum type %s)", v.name, prefix, typeName),
			})
		}
	}
	return
}
