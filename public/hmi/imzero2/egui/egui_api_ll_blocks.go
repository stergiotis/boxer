package egui

func BeginHorizontal() {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdBeginHorizontal)
	_f.CallProcedureNoThrow()
	return
}
func End() {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdEnd)
	_f.CallProcedureNoThrow()
	return
}
