package typed

import (
	"errors"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
)

var currentFffiVar *runtime.Fffi2[*runtime.Unmarshaller]

var errs = make([]error, 0, 128)

var currentFffiErrorHandler func(err error) = func(err error) {
	if err == nil {
		return
	}
	errs = append(errs, err)
	log.Error().Err(err).Msg("fffi runtime error, ignoring")
}

func HasErrors() bool {
	return len(errs) > 0
}
func GetError() (err error) {
	err = errors.Join(errs...)
	return
}
func ResetErrors() {
	clear(errs)
	errs = errs[:0]
}

// SetCurrentFffiVar installs the channel generated code sends through. A nil
// fffi is a programming error and panics.
func SetCurrentFffiVar(fffi *runtime.Fffi2[*runtime.Unmarshaller]) {
	if fffi == nil {
		panic("SetCurrentFffiVar called with a nil Fffi2 — install the channel the application established")
	}
	currentFffiVar = fffi
}
func GetCurrentFffiVar() (fffi *runtime.Fffi2[*runtime.Unmarshaller]) {
	fffi = currentFffiVar
	return
}

// SetCurrentFffiErrorHandler replaces the handler runtime errors go to. A nil
// handler is a programming error and panics.
func SetCurrentFffiErrorHandler(handler func(err error)) {
	if handler == nil {
		panic("SetCurrentFffiErrorHandler called with a nil handler")
	}
	currentFffiErrorHandler = handler
}

// GetCurrentFffiCapture returns the current Fffi2 as a FffiCaptureI for deferred
// block capture, or nil when none is set. Used by generated code to create
// DeferredBlockScope instances.
//
// The explicit nil branch is load-bearing, not defensive: returning the typed
// nil pointer would hand the caller a non-nil interface wrapping it, so a
// `!= nil` guard passes and the first method call dereferences nil. The
// fetcher guard in egui2's bindings is written that way.
func GetCurrentFffiCapture() runtime.FffiCaptureI {
	if currentFffiVar == nil {
		return nil
	}
	return currentFffiVar
}
