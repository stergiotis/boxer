//go:build wasip1

package browserhost

import (
	"strings"
	"unsafe"
)

// The reactor entry points (ADR-0263 SD1). A host that built the binary with
// -buildmode=c-shared calls _initialize (package inits only: Go runs neither
// main nor sees argv in that mode), writes the arguments NUL-separated into
// the buffer argbuf names, calls setup with their length — which runs the
// function SetMain registered — and then frame per tick.

// argBuf is a fixed, non-moving home for the arguments the host writes; Go's
// collector does not move globals, so its address is stable.
var argBuf [1 << 16]byte

// mainFn is what setup runs; the binary registers it from an init.
var mainFn func(args []string) (step func() int32)

// stepFn is the reactor's per-tick step, what mainFn returned.
var stepFn func() int32

// SetMain registers the binary's entry: given the arguments the host wrote,
// it sets the application up and returns the per-tick step, or nil when the
// arguments ran a one-shot command instead. Call it from an init of the
// main package; a module without one answers setup with 2.
func SetMain(f func(args []string) (step func() int32)) {
	mainFn = f
}

// argbuf returns the address of the argument buffer.
//
//go:wasmexport argbuf
func argbuf() int32 {
	return int32(uintptr(unsafe.Pointer(&argBuf[0])))
}

// argcap returns the capacity of the argument buffer.
//
//go:wasmexport argcap
func argcap() int32 {
	return int32(len(argBuf))
}

// setup runs the registered main on the n bytes of NUL-separated arguments
// in the buffer. Returns 0 when the reactor loop is ready (frame may be
// called), 1 when the arguments ran a one-shot command instead, 2 when no
// main was registered.
//
//go:wasmexport setup
func setup(n int32) int32 {
	if mainFn == nil {
		return 2
	}
	var args []string
	if n > 0 {
		args = strings.Split(string(argBuf[:n]), "\x00")
	}
	stepFn = mainFn(args)
	if stepFn == nil {
		return 1
	}
	return 0
}

// frame is the reactor's tick: one step of the application. Returns 0 while
// the loop continues and 1 once it has stopped; -1 before setup. Goroutines
// other than the render goroutine get their turn inside the step, which
// yields to the scheduler around the frame on wasm targets.
//
//go:wasmexport frame
func frame() int32 {
	if stepFn == nil {
		return -1
	}
	return stepFn()
}
