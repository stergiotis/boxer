package ir

import (
	"bytes"
	"iter"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/compiletimeflags"
)

func (inst AbstractType) IsAbstract() bool {
	return true
}

func (inst AbstractType) GetName() naming.StylableName {
	return inst.name
}

func (inst AbstractType) ImplementedAbstractTypes() iter.Seq[AbstractType] {
	return functional.MakeSingleValueIterator1(inst)
}

func NewAbstractType(name naming.StylableName) AbstractType {
	if compiletimeflags.ExtraChecks && !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("invalid name")
	}
	return AbstractType{
		name: name,
	}
}

func (inst ConcreteType) IsAbstract() bool {
	return false
}

func (inst ConcreteType) ImplementedAbstractTypes() iter.Seq[AbstractType] {
	return slices.Values(inst.implementedAbstractTypes)
}
func (inst ConcreteType) GetName() naming.StylableName {
	return inst.name
}

func (inst *BuilderFactoryNode) GetName() naming.StylableName {
	return inst.Name
}
func (inst *ProceduralNode) GetName() naming.StylableName {
	return inst.Name
}
func (inst EvaluatedArgumentSpec) IsEmpty() bool {
	return inst.Len() == 0
}
func (inst EvaluatedArgumentSpec) Len() int {
	return len(inst.Names)
}
func (inst EvaluatedArgumentSpec) Iterate() iter.Seq2[naming.StylableName, TypeI] {
	return func(yield func(naming.StylableName, TypeI) bool) {
		for i, n := range inst.Names {
			if !yield(n, inst.AcceptedTypes[i]) {
				return
			}
		}
	}
}
func (inst PlainArgumentSpec) Iterate() iter.Seq2[naming.StylableName, canonicaltypes.PrimitiveAstNodeI] {
	return func(yield func(naming.StylableName, canonicaltypes.PrimitiveAstNodeI) bool) {
		for i, n := range inst.Names {
			if !yield(n, inst.Types[i]) {
				return
			}
		}
	}
}
func (inst PlainArgumentSpec) IsEmpty() bool {
	return inst.Len() == 0
}
func (inst PlainArgumentSpec) Len() int {
	return len(inst.Names)
}

func (inst *FetcherNode) GetName() naming.StylableName {
	return inst.Name
}

func (inst *StringVerbatimCode) UseDefaultCode() bool {
	return inst.Default
}

func (inst *StringVerbatimCode) GetVerbatimCode() string {
	return inst.VerbatimCode
}
func MergeVerbatimCode(code ...VerbatimCodeI) VerbatimCodeI {
	switch len(code) {
	case 0:
		log.Panic().Msg("unable to merge: no argument supplied")
	case 1:
		return code[0]
	}
	def := true
	s := bytes.NewBuffer(make([]byte, 0, 1024))
	for i, c := range code {
		def = def && c.UseDefaultCode()
		if i > 0 {
			b := s.String()
			if !strings.HasSuffix(b, "\n") {
				s.WriteRune('\n')
			}
		}
		s.WriteString(c.GetVerbatimCode())
	}
	return &StringVerbatimCode{
		Default:      def,
		VerbatimCode: s.String(),
	}
}
