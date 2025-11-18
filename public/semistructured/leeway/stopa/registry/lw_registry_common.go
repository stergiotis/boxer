package registry

import (
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

func getOrigin() string {
	return unsafeperf.UnsafeBytesToString(debug.Stack())
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
