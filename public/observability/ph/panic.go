// Package ph turns a recovered panic into an error, and holds the panic
// boundaries a runtime-owned entry point is built from (ADR-0386).
package ph

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrPanic is what every error from ConvertPanicToError wraps, so a caller
// can tell a recovered panic from an error the code returned:
// errors.Is(err, ErrPanic).
var ErrPanic = errors.New("panic")

// ConvertPanicToError turns a value returned by recover into an error. Call
// it from the deferred function that recovered: the error's stack is taken
// there, and during the unwind that stack still holds the frames that
// panicked, so the error names the panic site. The full text of the stack
// travels as well, under "stacktrace", since the error's own stack is cut at
// a fixed depth. A panic value that is an error is wrapped, so errors.Is and
// errors.As reach it.
func ConvertPanicToError(panicErr any) error {
	e, ok := panicErr.(error)
	trace := strings.Split(string(debug.Stack()), "\n")
	if ok {
		return eb.Build().Strs("stacktrace", trace).Errorf("recovering from %w: %w", ErrPanic, e)
	} else {
		return eb.Build().Strs("stacktrace", trace).Str("panic", fmt.Sprintf("%+v", panicErr)).Errorf("recovering from %w", ErrPanic)
	}
}

// Guard calls fn and returns its error, or, when fn panics, the panic as an
// error from ConvertPanicToError. It is the boundary a runtime-owned entry
// point puts around code it does not own. runtime.Goexit is not a panic and
// passes through.
func Guard(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ConvertPanicToError(r)
		}
	}()
	err = fn()
	return
}

func PanicHandler(exitCode int, afterPanic func(), ensure func()) {
	panicErr := recover()
	if panicErr != nil {
		e := ConvertPanicToError(panicErr)
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
