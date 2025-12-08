package egui

import (
	"iter"

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
	runtime.AddBoolArg(_f,v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst ScrollAreaBuilder) VerticalScroll(v bool) ScrollAreaBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(ScrollAreaBuilderIdVerticalScroll)
	runtime.AddBoolArg(_f,v)
	_f.CallProcedureNoThrow()
	return inst
}
func (inst ScrollAreaBuilder) Animate(v bool) ScrollAreaBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(ScrollAreaBuilderIdAnimate)
	runtime.AddBoolArg(_f,v)
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
