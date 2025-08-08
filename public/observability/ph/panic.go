package ph

import (
	"os"
	"runtime/debug"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func ConvertPanicToError(panicErr any) error {
	e, ok := panicErr.(error)
	// NOTE debug.Stack does indeed give back the stacktrace of the panic error
	trace := strings.Split(string(debug.Stack()), "\n")
	if ok {
		// TODO attach regular stack trace to e
		return eb.Build().WithoutStack().Strs("stacktrace", trace).Errorf("recovering from panic: %w", e)
	} else {
		return eb.Build().WithoutStack().Strs("stacktrace", trace).Errorf("recovering from panic: %+v", panicErr)
	}
}
func PanicHandler(exitCode int, afterPanic func(), ensure func()) {
	if err := recover(); err != nil {
		e := ConvertPanicToError(err)
		log.Error().Err(e).Msg("program panicked")
		if ensure != nil {
			ensure()
		}
		if afterPanic != nil {
			afterPanic()
		}
		os.Exit(exitCode)
		return
	}
	if ensure != nil {
		ensure()
	}
}
