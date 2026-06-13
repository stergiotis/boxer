package registry

import (
	"runtime"
	"strconv"
	"strings"
)

// getOrigin identifies the registration call site — the first frame outside
// this registry package that drove a Begin. Previously it returned the entire
// debug.Stack(), so the same registration line reached via different call paths
// looked like different code locations and a legitimate re-registration was
// rejected as a collision (review G-5).
func getOrigin() string {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:]) // skip runtime.Callers + getOrigin
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function != "" && !strings.Contains(frame.File, "/semistructured/leeway/stopa/registry/") {
			return frame.File + ":" + strconv.Itoa(frame.Line)
		}
		if !more {
			break
		}
	}
	return "<unknown origin>"
}
func getModuleInfo(skip int) string {
	pc := [1]uintptr{0}
	runtime.Callers(skip+2, pc[:])

	caller := runtime.FuncForPC(pc[0])
	if caller == nil {
		return "<unknown module>"
	}
	callerName := caller.Name()
	idx := strings.LastIndex(callerName, ".")
	if idx >= 0 {
		return callerName[:idx]
	} else {
		return callerName
	}
}
