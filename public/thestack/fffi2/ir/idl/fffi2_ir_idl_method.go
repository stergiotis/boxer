package idl

import (
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

type MethodBuilderStateE uint8

const (
	MethodBuilderStateInitial  MethodBuilderStateE = 0
	MethodBuilderStateInMethod MethodBuilderStateE = 1
)

type MethodBuilder struct {
	retr  []ir.Method
	state MethodBuilderStateE
	// lastArgKindWasPlain tracks which spec received the most-recent arg
	// for the current in-progress method, letting AsColor/AsColors target
	// the right parallel slice.
	lastArgKindWasPlain bool
}

func NewMethodBuilder() *MethodBuilder {
	return &MethodBuilder{
		retr:  make([]ir.Method, 0, 4),
		state: MethodBuilderStateInitial,
	}
}
func (inst *MethodBuilder) transitionState(to MethodBuilderStateE, allowed MethodBuilderStateE) {
	if inst.state != allowed {
		log.Panic().Msg("invalid state transition")
	}
	inst.state = to
}
func (inst *MethodBuilder) verifyState(allowed MethodBuilderStateE) {
	if inst.state != allowed {
		log.Panic().Msg("builder is in wrong state")
	}
}
func (inst *MethodBuilder) Merge(mths ...ir.Method) *MethodBuilder {
	for _, mth := range mths {
		inst.BeginMethod(mth.Spec.Name)
		for i, n := range mth.Spec.PlainArguments.Names {
			inst.Arg(n, mth.Spec.PlainArguments.Types[i])
			if i < len(mth.Spec.PlainArguments.ColorArgKinds) {
				switch mth.Spec.PlainArguments.ColorArgKinds[i] {
				case ir.ColorArgKindScalar:
					inst.AsColor()
				case ir.ColorArgKindSlice:
					inst.AsColors()
				}
			}
		}
		for i, n := range mth.Spec.EvaluatedArguments.Names {
			inst.EvaluatedArg(n, mth.Spec.EvaluatedArguments.AcceptedTypes[i])
			if i < len(mth.Spec.EvaluatedArguments.ColorArgKinds) {
				if mth.Spec.EvaluatedArguments.ColorArgKinds[i] == ir.ColorArgKindScalar {
					inst.AsColor()
				}
			}
		}
		inst.CodeClientRust(mth.CodeHolder.CodeClientRust)
		inst.CodeServerGo(mth.CodeHolder.CodeServerGo)
		inst.EndMethod()
	}
	return inst
}
func (inst *MethodBuilder) BeginMethod(name naming.StylableName) *MethodBuilder {
	if !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("invalid method name")
	}
	inst.transitionState(MethodBuilderStateInMethod, MethodBuilderStateInitial)
	name = name.Convert(naming.DefaultNamingStyle)
	for _, r := range inst.retr {
		if r.Spec.Name == name {
			log.Panic().Stringer("name", name).Msg("a method with this name is already registered")
		}
	}
	inst.retr = append(inst.retr, ir.Method{
		Spec: ir.MethodSpec{
			Name: name,
			PlainArguments: ir.PlainArgumentSpec{
				Names:         make([]naming.StylableName, 0, 4),
				Types:         make([]canonicaltypes.PrimitiveAstNodeI, 0, 4),
				ColorArgKinds: make([]ir.ColorArgKindE, 0, 4),
			},
			EvaluatedArguments: ir.EvaluatedArgumentSpec{
				Names:         make([]naming.StylableName, 0, 4),
				AcceptedTypes: make([]ir.TypeI, 0, 4),
				ColorArgKinds: make([]ir.ColorArgKindE, 0, 4),
			},
		},
		CodeHolder: ir.CodeHolder{
			CodeClientRust: ir.DefaultCode,
			CodeServerGo:   ir.DefaultCode,
		},
	})
	inst.lastArgKindWasPlain = false
	return inst
}
func (inst *MethodBuilder) Arg(name naming.StylableName, typ canonicaltypes.PrimitiveAstNodeI) *MethodBuilder {
	validateArgName(name)
	if !typ.IsValid() {
		log.Panic().Msg("supplied argument type is not a valid canonical type")
	}
	name = name.Convert(naming.DefaultNamingStyle)
	if slices.Contains(inst.retr[len(inst.retr)-1].Spec.PlainArguments.Names, name) {
		log.Panic().Stringer("name", name).Msg("an argument with this name is already registered")
	}
	inst.verifyState(MethodBuilderStateInMethod)
	spec := &inst.retr[len(inst.retr)-1].Spec.PlainArguments
	spec.Names = append(spec.Names, name)
	spec.Types = append(spec.Types, typ)
	spec.ColorArgKinds = append(spec.ColorArgKinds, ir.ColorArgKindNone)
	inst.lastArgKindWasPlain = true
	return inst
}
func (inst *MethodBuilder) EvaluatedArg(name naming.StylableName, typ ir.TypeI) *MethodBuilder {
	validateArgName(name)
	name = name.Convert(naming.DefaultNamingStyle)
	if slices.Contains(inst.retr[len(inst.retr)-1].Spec.EvaluatedArguments.Names, name) {
		log.Panic().Stringer("name", name).Msg("an evaluated argument with this name is already registered")
	}
	inst.verifyState(MethodBuilderStateInMethod)
	spec := &inst.retr[len(inst.retr)-1].Spec.EvaluatedArguments
	spec.Names = append(spec.Names, name)
	spec.AcceptedTypes = append(spec.AcceptedTypes, typ)
	spec.ColorArgKinds = append(spec.ColorArgKinds, ir.ColorArgKindNone)
	inst.lastArgKindWasPlain = false
	return inst
}

// AsColor annotates the last-appended arg of the in-progress method as a
// scalar color. See ArgumentsBuilder.AsColor for semantics.
func (inst *MethodBuilder) AsColor() *MethodBuilder {
	inst.verifyState(MethodBuilderStateInMethod)
	spec := &inst.retr[len(inst.retr)-1].Spec
	if inst.lastArgKindWasPlain {
		n := len(spec.PlainArguments.ColorArgKinds)
		if n == 0 {
			log.Panic().Msg("AsColor: no argument to annotate")
		}
		if spec.PlainArguments.ColorArgKinds[n-1] != ir.ColorArgKindNone {
			log.Panic().Stringer("name", spec.PlainArguments.Names[n-1]).Msg("AsColor: argument is already color-annotated")
		}
		spec.PlainArguments.ColorArgKinds[n-1] = ir.ColorArgKindScalar
		return inst
	}
	n := len(spec.EvaluatedArguments.ColorArgKinds)
	if n == 0 {
		log.Panic().Msg("AsColor: no argument to annotate")
	}
	if spec.EvaluatedArguments.ColorArgKinds[n-1] != ir.ColorArgKindNone {
		log.Panic().Stringer("name", spec.EvaluatedArguments.Names[n-1]).Msg("AsColor: argument is already color-annotated")
	}
	spec.EvaluatedArguments.ColorArgKinds[n-1] = ir.ColorArgKindScalar
	return inst
}

// AsColors annotates the last-appended arg of the in-progress method as a
// bulk color (color.Colors). Valid only on Plain-transport slice args.
func (inst *MethodBuilder) AsColors() *MethodBuilder {
	inst.verifyState(MethodBuilderStateInMethod)
	if !inst.lastArgKindWasPlain {
		log.Panic().Msg("AsColors: applies only to Plain-transport arguments (SD9: retained is scalar-only)")
	}
	spec := &inst.retr[len(inst.retr)-1].Spec.PlainArguments
	n := len(spec.ColorArgKinds)
	if n == 0 {
		log.Panic().Msg("AsColors: no argument to annotate")
	}
	if spec.ColorArgKinds[n-1] != ir.ColorArgKindNone {
		log.Panic().Stringer("name", spec.Names[n-1]).Msg("AsColors: argument is already color-annotated")
	}
	spec.ColorArgKinds[n-1] = ir.ColorArgKindSlice
	return inst
}
func (inst *MethodBuilder) CodeClientRust(code ir.VerbatimCodeI) *MethodBuilder {
	inst.verifyState(MethodBuilderStateInMethod)
	inst.retr[len(inst.retr)-1].CodeHolder.CodeClientRust = code
	return inst
}
func (inst *MethodBuilder) CodeServerGo(code ir.VerbatimCodeI) *MethodBuilder {
	inst.verifyState(MethodBuilderStateInMethod)
	inst.retr[len(inst.retr)-1].CodeHolder.CodeServerGo = code
	return inst
}
func (inst *MethodBuilder) EndMethod() *MethodBuilder {
	inst.transitionState(MethodBuilderStateInitial, MethodBuilderStateInMethod)
	return inst
}
func (inst *MethodBuilder) Build() []ir.Method {
	inst.verifyState(MethodBuilderStateInitial)
	return inst.retr
}
func (inst *MethodBuilder) BuildOne() ir.Method {
	inst.verifyState(MethodBuilderStateInitial)
	b := inst.Build()
	if len(b) != 1 {
		log.Panic().Int("should", 1).Int("actual", len(b)).Msg("unexpected number of methods")
	}
	return b[0]
}
