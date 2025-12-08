package egui

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
