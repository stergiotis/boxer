package eh

import (
	"runtime"
	"strconv"
)

// Frame represents a single stack frame with resolved file, line, and function information.
type Frame struct {
	PC       uintptr
	File     string
	Line     int
	Function string
}

// ShortFunction returns the short (unqualified) function name.
func (inst Frame) ShortFunction() string {
	fn := inst.Function
	for i := len(fn) - 1; i >= 0; i-- {
		if fn[i] == '.' {
			return fn[i+1:]
		}
	}
	return fn
}

// Location returns "file:line".
func (inst Frame) Location() string {
	return inst.File + ":" + strconv.Itoa(inst.Line)
}

// StackTrace is a slice of resolved stack frames, ordered from innermost (callee) to outermost (caller).
type StackTrace []Frame

type stack []uintptr

// resolveStack converts raw program counters into resolved stack frames
// using runtime.CallersFrames.
func resolveStack(s *stack) StackTrace {
	if s == nil || len(*s) == 0 {
		return nil
	}
	frames := runtime.CallersFrames([]uintptr(*s))
	result := make(StackTrace, 0, len(*s))
	for {
		frame, more := frames.Next()
		if frame.PC == 0 {
			break
		}
		result = append(result, Frame{
			PC:       frame.PC,
			File:     frame.File,
			Line:     frame.Line,
			Function: frame.Function,
		})
		if !more {
			break
		}
	}
	return result
}

type stackTracer interface {
	StackTrace() StackTrace
}
type unwrapableSingle interface {
	error
	Unwrap() error
}
type unwrapableMulti interface {
	error
	Unwrap() []error
}
type ErrorWithStructuredData interface {
	error
	SetCBORStructuredData(p []byte)
	GetCBORStructuredData() []byte
}
