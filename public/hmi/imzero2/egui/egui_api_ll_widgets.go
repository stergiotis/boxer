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

func WidgetSeparator() SeparatorBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdWidgetSeparator)
	_f.CallFunctionNoThrow()
	return SeparatorBuilder{}
}
