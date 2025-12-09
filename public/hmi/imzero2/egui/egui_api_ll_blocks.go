package egui

import "github.com/stergiotis/boxer/public/fffi/runtime"

func End() {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdEnd)
	_f.CallProcedureNoThrow()
	return
}
func BeginHorizontal() {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdBeginHorizontal)
	_f.CallProcedureNoThrow()
}
func BeginScrollArea() ScrollAreaBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdBeginScrollArea)
	_f.CallProcedureNoThrow() // FIXME lazy
	return ScrollAreaBuilder{}
}

func BeginCollapsingHeader(text string) CollapsingHeaderBuilder {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdBeginCollapsingHeader)
	runtime.AddStringArg(_f, text)
	_f.CallProcedureNoThrow() // FIXME lazy
	return CollapsingHeaderBuilder{}
}
