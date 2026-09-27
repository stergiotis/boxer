//go:build wasip1

package main

import (
	"runtime"
	"strings"
	"unsafe"
)

// The reactor entry points (ADR-0077 O2). A host that built this command with
// -buildmode=c-shared calls _initialize (package inits only: Go runs neither
// main nor sees argv in that mode), writes the arguments NUL-separated into
// the buffer argbuf names, calls setup with their length — which runs main's
// body, and with -reactor returns once the application is set up — and then
// frame per tick.

// argBuf is a fixed, non-moving home for the arguments the host writes; Go's
// collector does not move globals, so its address is stable.
var argBuf [1 << 16]byte

// argbuf returns the address and capacity of the argument buffer.
//
//go:wasmexport argbuf
func argbuf() int32 {
	return int32(uintptr(unsafe.Pointer(&argBuf[0])))
}

//go:wasmexport argcap
func argcap() int32 {
	return int32(len(argBuf))
}

// setup runs main's body on the n bytes of NUL-separated arguments in the
// buffer. Returns 0 when the reactor loop is ready (frame may be called), 1
// when the arguments ran a one-shot command instead.
//
//go:wasmexport setup
func setup(n int32) int32 {
	var args []string
	if n > 0 {
		args = strings.Split(string(argBuf[:n]), "\x00")
	}
	run(args)
	if stepFn == nil {
		return 1
	}
	return 0
}

// frame is the reactor's tick: one Step of the application. Returns 0 while
// the loop continues and 1 once it has stopped and reported; -1 before setup.
//
// Goroutines other than the render goroutine run only while an export is
// executing, and the render goroutine never parks: its reads are answered
// synchronously by the host's shim. So the tick yields to the scheduler
// before and after the frame — that is when an expired timer fires and a
// background goroutine (a task producer, a bus handler) gets its turn.
// A few rounds let a wake-up chain (timer → publish → observer) complete
// within one tick.
//
//go:wasmexport frame
func frame() int32 {
	if stepFn == nil {
		return -1
	}
	yieldToOthers()
	rc := stepFn()
	yieldToOthers()
	return rc
}

const yieldRounds = 4

func yieldToOthers() {
	for range yieldRounds {
		runtime.Gosched()
	}
}
