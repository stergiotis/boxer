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
		yield(functional.NilIteratorValue)
		R3NodeDirClosePush(0)
	}
}
