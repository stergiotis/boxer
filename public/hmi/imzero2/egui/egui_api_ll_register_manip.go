package egui

import (
	"github.com/stergiotis/boxer/public/fffi/runtime"
)

func R1Get() (flags ResponseFlags) {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdR1Get)
	_f.CallProcedureNoThrow()
	flags = runtime.GetUint32Retr[ResponseFlags](_f)
	return
}
func R2Get() (flags ResponseFlags) {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdR2Get)
	_f.CallProcedureNoThrow()
	flags = runtime.GetUint32Retr[ResponseFlags](_f)
	return
}
func R0AtomPushText(text string) {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdR0AtomPush)
	runtime.AddStringArg(_f, text)
	_f.CallFunctionNoThrow()
}
func R2FromR1Masked(mask ResponseFlags) {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdR2FromR1Masked)
	runtime.AddUint32Arg(_f, mask)
	_f.CallFunctionNoThrow()
}
