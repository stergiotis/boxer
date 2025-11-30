package egui

import (
	"github.com/stergiotis/boxer/public/fffi/runtime"
)

func WidgetLabel(label string) {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdWidgetLabel)
	runtime.AddStringArg(_f, label)
	_f.CallProcedureNoThrow()
}
func WidgetButton() {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdWidgetButton)
	_f.CallFunctionNoThrow()
}
