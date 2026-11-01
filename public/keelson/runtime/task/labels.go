package task

import (
	"context"
	"runtime/pprof"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Profiler labels name the window a goroutine works for (ADR-0351 §SD1), so
// a CPU profile can be split by window. The window host sets them on the
// render goroutine around a window's Mount, Frame and Unmount; a goroutine
// inherits its starter's labels; and a task's work runs under its owner's,
// wherever it runs.
const (
	// LabelWindow is the window key, in decimal.
	LabelWindow = "boxer.window"
	// LabelApp is the window's app id.
	LabelApp = "boxer.app"
)

// LabelContext is a context whose profiler labels name the window key of
// appId, for pprof.SetGoroutineLabels. Build it once per window.
func LabelContext(appId app.AppIdT, key uint64) (ctx context.Context) {
	ctx = pprof.WithLabels(context.Background(), pprof.Labels(LabelWindow, strconv.FormatUint(key, 10), LabelApp, string(appId)))
	return
}

// LabelGoroutine gives the calling goroutine the profiler labels of api's
// owner, when api has one, and leaves its labels alone otherwise. The
// goroutine keeps them: the runtime offers no way to read the labels it had,
// and a goroutine running a window's task most often works for that window.
func LabelGoroutine(api TaskApiI) {
	if api == nil || api.InstanceKey() == 0 {
		return
	}
	pprof.SetGoroutineLabels(LabelContext(api.AppId(), api.InstanceKey()))
}
