package egui

import (
	"iter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/functional"
)

type ResponseGetter struct {
}

func (inst ResponseGetter) Get() (flags ResponseFlags) {
	flags = R1Get()
	return
}

type LabelBuilder struct {
}

func (inst LabelBuilder) Selectable(v bool) LabelBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(LabelBuilderMethodIdSelectable)
	runtime.AddBoolArg(_f, v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst LabelBuilder) Build() ResponseGetter {
	_f := currentFffiVar
	_f.AddFunctionId(LabelBuilderMethodIdBuild)
	_f.CallProcedureNoThrow()
	return ResponseGetter{}
}

type ButtonBuilder struct {
}

func (inst ButtonBuilder) Frame(v bool) ButtonBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(ButtonBuilderMethodFrame)
	runtime.AddBoolArg(_f, v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst ButtonBuilder) Build() ResponseGetter {
	_f := currentFffiVar
	_f.AddFunctionId(LabelBuilderMethodIdBuild)
	_f.CallProcedureNoThrow()
	return ResponseGetter{}
}

type TreeNodeBuilder struct {
}

func (inst TreeNodeBuilder) Label(label string) TreeNodeBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(TreeNodeBuilderIdLabelText)
	runtime.AddStringArg(_f, label)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst TreeNodeBuilder) Build() {
	_f := currentFffiVar
	_f.AddFunctionId(TreeNodeBuilderIdBuild)
	_f.CallProcedureNoThrow()
}
func (inst TreeNodeBuilder) BuildAndClose() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.Build()
		defer R3NodeDirClosePush(0)
		yield(functional.NilIteratorValue)
	}
}

type ScrollAreaBuilder struct {
}

func (inst ScrollAreaBuilder) Build() {
	_f := currentFffiVar
	_f.AddFunctionId(ScrollAreaBuilderIdBuild)
	_f.CallProcedureNoThrow()
}
func (inst ScrollAreaBuilder) HorizontalScroll(v bool) ScrollAreaBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(ScrollAreaBuilderIdHorizontalScroll)
	runtime.AddBoolArg(_f, v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst ScrollAreaBuilder) VerticalScroll(v bool) ScrollAreaBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(ScrollAreaBuilderIdVerticalScroll)
	runtime.AddBoolArg(_f, v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst ScrollAreaBuilder) Animate(v bool) ScrollAreaBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(ScrollAreaBuilderIdAnimate)
	runtime.AddBoolArg(_f, v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst ScrollAreaBuilder) BuildAndEnd() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.Build()
		defer End()
		yield(functional.NilIteratorValue)
	}
}

type SeparatorBuilder struct {
}

func (inst SeparatorBuilder) Build() {
	_f := currentFffiVar
	_f.AddFunctionId(SeparatorBuilderIdBuild)
	_f.CallProcedureNoThrow()
}
func (inst SeparatorBuilder) Horizontal() SeparatorBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(SeparatorBuilderIdHorizontal)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst SeparatorBuilder) Vertical() SeparatorBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(SeparatorBuilderIdVertical)
	_f.CallProcedureNoThrow()
	return inst
}

type CollapsingHeaderBuilder struct {
	callSiteCull bool
}

func (inst CollapsingHeaderBuilder) DefaultOpen(open bool) CollapsingHeaderBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(CollapsingHeaderBuilderIdDefaultOpen)
	runtime.AddBoolArg(_f, open)
	//_f.CallProcedureNoThrow()
	return inst
}
func (inst CollapsingHeaderBuilder) Open() CollapsingHeaderBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(CollapsingHeaderBuilderIdOpen)
	//_f.CallProcedureNoThrow()
	return inst
}
func (inst CollapsingHeaderBuilder) Close() CollapsingHeaderBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(CollapsingHeaderBuilderIdClose)
	//_f.CallProcedureNoThrow()
	return inst
}
func (inst CollapsingHeaderBuilder) CallSiteCull() CollapsingHeaderBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(CollapsingHeaderBuilderIdCallSiteCull)
	//_f.CallProcedureNoThrow()
	inst.callSiteCull = true
	return inst
}
func (inst CollapsingHeaderBuilder) Build() {
	_f := currentFffiVar
	_f.AddFunctionId(CollapsingHeaderBuilderIdBuild)
	_f.CallProcedureNoThrow()
}
func (inst CollapsingHeaderBuilder) BuildAndEnd() iter.Seq[functional.NilIteratorValueType] {
	if inst.callSiteCull {
		return inst.buildAndEndCulled()
	} else {
		return inst.buildAndEnd()
	}
}
func (inst CollapsingHeaderBuilder) buildAndEnd() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.Build()
		defer End()
		yield(functional.NilIteratorValue)
	}
}
func (inst CollapsingHeaderBuilder) buildAndEndCulled() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		_f := currentFffiVar
		inst.Build()
		defer End()
		if runtime.GetBoolRetr[bool](_f) {
			yield(functional.NilIteratorValue)
		} else {
			// collapsed --> culling
			log.Info().Msg("culled")
		}
	}
}
