package egui

import "github.com/stergiotis/boxer/public/fffi/runtime"

func Label(label string) {
	_f := currentFffiVar
	_f.AddFunctionId(0x00000000)
	runtime.AddStringArg(_f, label)
	_f.CallFunctionNoThrow()
}
