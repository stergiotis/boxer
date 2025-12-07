package egui

import (
	"github.com/stergiotis/boxer/public/fffi/runtime"
)

func WidgetLabel(label string) LabelBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdWidgetLabel)
	runtime.AddStringArg(_f, label)
	_f.CallProcedureNoThrow()
	return LabelBuilder{}
}
func WidgetButton(label string) ButtonBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdWidgetButton)
	runtime.AddStringArg(_f, label)
	_f.CallFunctionNoThrow()
	return ButtonBuilder{}
}
func WidgetTree() {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdWidgetTree)
	_f.CallFunctionNoThrow()
}

type LabelBuilder struct {
}
type ResponseGetter struct {
}

func (inst ResponseGetter) Get() (flags ResponseFlags) {
	flags = R1Get()
	return
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
