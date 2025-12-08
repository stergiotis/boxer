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
	_f.CallFunctionNoThrow()
	flags = runtime.GetUint32Retr[ResponseFlags](_f)
	return
}
func R0AtomPushText(text string) {
	_f := currentFffiVar
	_f.AddFunctionId(FuncProcIdR0AtomPush)
	runtime.AddStringArg(_f, text)
	_f.CallProcedureNoThrow()
}
func R2FromR1Masked(mask ResponseFlags) {
	_f := currentFffiVar
	_f.AddProcedureId(FuncProcIdR2FromR1Masked)
	runtime.AddUint32Arg(_f, mask)
	_f.CallFunctionNoThrow()
}
func R3NodeDirPush(id uint64) TreeNodeBuilder {
	_f := currentFffiVar
	_f.AddProcedureId(FuncProcIdR3NodeDirPush)
	runtime.AddUint64Arg(_f, id)
	_f.CallProcedureNoThrow()
	return TreeNodeBuilder{}
}
func R3NodeLeafPush(id uint64) TreeNodeBuilder {
	_f := currentFffiVar
	_f.AddProcedureId(FuncProcIdR3NodeLeafPush)
	runtime.AddUint64Arg(_f, id)
	_f.CallProcedureNoThrow()
	return TreeNodeBuilder{}
}
func R3NodeDirClosePush(n uint64) {
	_f := currentFffiVar
	_f.AddProcedureId(FuncProcIdR3NodeDirClosePush)
	runtime.AddUint64Arg(_f, n)
	_f.CallProcedureNoThrow()
}
func R5GetAndClear() (roaring64 []byte) {
	_f := currentFffiVar
	_f.AddProcedureId(FuncProcIdR5GetAnClear)
	_f.CallFunctionNoThrow()
	roaring64 = runtime.GetBytesRetr(_f)
	return
}
