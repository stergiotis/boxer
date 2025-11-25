package egui

import (
	"fmt"
	"os"
	"time"

	"github.com/stergiotis/boxer/public/fffi/runtime"
)

func Label(label string) {
	_f := currentFffiVar
	_f.AddFunctionId(0x00000000)
	runtime.AddStringArg(_f, label)
	_f.CallProcedureNoThrow()
	fmt.Fprintf(os.Stderr, "%d\n", time.Now().UnixNano())
}
func Button(label string) (clicked bool) {
	_f := currentFffiVar
	_f.AddFunctionId(0x00000001)
	runtime.AddStringArg(_f, label)
	_f.CallFunctionNoThrow()
	clicked = runtime.GetBoolRetr[bool](_f)
	return
}
