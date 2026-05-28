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

func SetCurrentFffiVar(fffi *runtime.Fffi2[*runtime.Unmarshaller]) {
	if fffi == nil {
		log.Fatal().Msg("fffi is nil")
	}
	currentFffiVar = fffi
}
func GetCurrentFffiVar() (fffi *runtime.Fffi2[*runtime.Unmarshaller]) {
	fffi = currentFffiVar
	return
}

func SetCurrentFffiErrorHandler(handler func(err error)) {
	if handler == nil {
		log.Fatal().Msg("handler is nil")
	}
	currentFffiErrorHandler = handler
}

// GetCurrentFffiCapture returns the current Fffi2 as a FffiCaptureI for deferred block capture.
// Used by generated code to create DeferredBlockScope instances.
func GetCurrentFffiCapture() runtime.FffiCaptureI {
	return currentFffiVar
}
