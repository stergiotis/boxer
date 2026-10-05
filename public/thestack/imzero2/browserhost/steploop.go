package browserhost

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
)

// StepLoop turns a launched application into the per-tick step the reactor
// calls: Begin once, then one Step per call, and on the first call after
// the loop stopped the closers and onStop (a report, say). Returns 1 from
// then on; err is Begin's failure, with no step.
func StepLoop(app *application.Application[*runtime.Unmarshaller], onStop func(error)) (step func() int32, err error) {
	if err = app.Begin(); err != nil {
		return
	}
	stopped := false
	step = func() int32 {
		if stopped {
			return 1
		}
		more, e := app.Step()
		if more {
			return 0
		}
		stopped = true
		app.CloseAll()
		if onStop != nil {
			onStop(e)
		}
		return 1
	}
	return
}
