//go:build llm_generated_opus47

package idl

import (
	"regexp"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

// reReservedArgName matches names that are a single letter optionally followed
// by digits (e.g. "r", "w", "x1", "a0"). These are excluded because the FFFI2
// Rust code generator uses single-letter variables internally:
//
//	i = widget ID, w = widget instance, f = FuncProcId, m = MethodProcId,
//	c = egui Context, u = egui Ui (optional), d = interpreter depth,
//	r = return flag
//
// Excluding the full [a-zA-Z][0-9]* pattern leaves room for future generator
// variables without requiring definition changes.
var reReservedArgName = regexp.MustCompile(`^[a-zA-Z][0-9]*$`)

func validateArgName(name naming.StylableName) {
	if !name.IsValid() {
		log.Panic().Str("name", string(name)).Msg("invalid argument name")
	}
	lower := string(name.Convert(naming.LowerSnakeCase))
	if reReservedArgName.MatchString(lower) {
		log.Panic().Str("name", string(name)).Str("lowerSnakeCase", lower).
			Msg("argument name matches reserved pattern [a-zA-Z][0-9]* — use a name with 2+ letters (e.g. 'wi' instead of 'w')")
	}
}

type ArgumentsBuilder struct {
	retr ir.ArgumentSpec
	// lastArgKindWasPlain tracks which spec received the most-recent arg,
	// so AsColor/AsColors can target the right parallel slice without the
	// caller re-specifying which transport it annotates.
	lastArgKindWasPlain bool
}

func NewArgumentsBuilder() *ArgumentsBuilder {
	return &ArgumentsBuilder{
		retr: ir.ArgumentSpec{
			EvaluatedArguments: ir.EvaluatedArgumentSpec{
				Names:         make([]naming.StylableName, 0, 4),
				AcceptedTypes: make([]ir.TypeI, 0, 4),
				ColorArgKinds: make([]ir.ColorArgKindE, 0, 4),
			},
			PlainArguments: ir.PlainArgumentSpec{
				Names:         make([]naming.StylableName, 0, 4),
				Types:         make([]canonicaltypes.PrimitiveAstNodeI, 0, 4),
				ColorArgKinds: make([]ir.ColorArgKindE, 0, 4),
			},
		},
	}
}
func (inst *ArgumentsBuilder) PlainArg(name naming.StylableName, typ canonicaltypes.PrimitiveAstNodeI) *ArgumentsBuilder {
	validateArgName(name)
	if !typ.IsValid() {
		log.Panic().Msg("supplied argument type is not a valid canonical type")
	}
	name = name.Convert(naming.DefaultNamingStyle)
	if slices.Contains(inst.retr.PlainArguments.Names, name) {
		log.Panic().Stringer("name", name).Msg("an argument with this name is already registered")
	}
	inst.retr.PlainArguments.Names = append(inst.retr.PlainArguments.Names, name)
	inst.retr.PlainArguments.Types = append(inst.retr.PlainArguments.Types, typ)
	inst.retr.PlainArguments.ColorArgKinds = append(inst.retr.PlainArguments.ColorArgKinds, ir.ColorArgKindNone)
	inst.lastArgKindWasPlain = true
	return inst
}
func (inst *ArgumentsBuilder) EvaluatedArg(name naming.StylableName, acceptedTypes ir.TypeI) *ArgumentsBuilder {
	validateArgName(name)
	name = name.Convert(naming.DefaultNamingStyle)
	if slices.Contains(inst.retr.PlainArguments.Names, name) {
		log.Panic().Stringer("name", name).Msg("an argument with this name is already registered")
	}
	inst.retr.EvaluatedArguments.Names = append(inst.retr.EvaluatedArguments.Names, name)
	inst.retr.EvaluatedArguments.AcceptedTypes = append(inst.retr.EvaluatedArguments.AcceptedTypes, acceptedTypes)
	inst.retr.EvaluatedArguments.ColorArgKinds = append(inst.retr.EvaluatedArguments.ColorArgKinds, ir.ColorArgKindNone)
	inst.lastArgKindWasPlain = false
	return inst
}

// AsColor annotates the last-appended argument as a scalar color. The
// generator surfaces it as color.Color in Go signatures; wire transport
// is unchanged (PlainArg(U32) stays u32; EvaluatedArg(Color32) stays retained).
// Panics if no argument has been appended yet or if the last argument's
// transport is not a scalar color-shaped type.
func (inst *ArgumentsBuilder) AsColor() *ArgumentsBuilder {
	if inst.lastArgKindWasPlain {
		n := len(inst.retr.PlainArguments.ColorArgKinds)
		if n == 0 {
			log.Panic().Msg("AsColor: no argument to annotate")
		}
		if inst.retr.PlainArguments.ColorArgKinds[n-1] != ir.ColorArgKindNone {
			log.Panic().Stringer("name", inst.retr.PlainArguments.Names[n-1]).Msg("AsColor: argument is already color-annotated")
		}
		inst.retr.PlainArguments.ColorArgKinds[n-1] = ir.ColorArgKindScalar
		return inst
	}
	n := len(inst.retr.EvaluatedArguments.ColorArgKinds)
	if n == 0 {
		log.Panic().Msg("AsColor: no argument to annotate")
	}
	if inst.retr.EvaluatedArguments.ColorArgKinds[n-1] != ir.ColorArgKindNone {
		log.Panic().Stringer("name", inst.retr.EvaluatedArguments.Names[n-1]).Msg("AsColor: argument is already color-annotated")
	}
	inst.retr.EvaluatedArguments.ColorArgKinds[n-1] = ir.ColorArgKindScalar
	return inst
}

// AsColors annotates the last-appended argument as a bulk color (color.Colors).
// Valid only on Plain-transport slice arguments (ADR-0052 SD9 forbids retained
// values in arrays). Panics on misuse.
func (inst *ArgumentsBuilder) AsColors() *ArgumentsBuilder {
	if !inst.lastArgKindWasPlain {
		log.Panic().Msg("AsColors: applies only to Plain-transport arguments (SD9: retained is scalar-only)")
	}
	n := len(inst.retr.PlainArguments.ColorArgKinds)
	if n == 0 {
		log.Panic().Msg("AsColors: no argument to annotate")
	}
	if inst.retr.PlainArguments.ColorArgKinds[n-1] != ir.ColorArgKindNone {
		log.Panic().Stringer("name", inst.retr.PlainArguments.Names[n-1]).Msg("AsColors: argument is already color-annotated")
	}
	inst.retr.PlainArguments.ColorArgKinds[n-1] = ir.ColorArgKindSlice
	return inst
}
func (inst *ArgumentsBuilder) Build() ir.ArgumentSpec {
	return inst.retr
}
