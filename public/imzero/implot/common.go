package implot

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/runtime"
)

type ImPlot struct {
	fffi *runtime.Fffi2
	errs []error
}

func NewImPlot(fffi *runtime.Fffi2) *ImPlot {
	return &ImPlot{fffi: fffi, errs: make([]error, 0, 8)}
}
func (inst *ImPlot) HasErrors() bool {
	return len(inst.errs) > 0
}
func (inst *ImPlot) Errors() []error {
	return inst.errs
}
func (inst *ImPlot) ResetErrors() {
	inst.errs = inst.errs[:0]
}
func (inst *ImPlot) getFffi() *runtime.Fffi2 {
	return inst.fffi
}
func (inst *ImPlot) handleError(err error) {
	inst.errs = append(inst.errs, err)
}

var currentFffiVar *runtime.Fffi2
var currentFffiErrorHandler func(err error) = func(err error) {
	log.Error().Err(err).Msg("fffi runtime error")
}

func SetCurrentFffiVar(fffi *runtime.Fffi2) {
	if fffi == nil {
		log.Fatal().Msg("fffi is nil")
	}
	currentFffiVar = fffi
}
func SetCurrentFffiErrorHandler(handler func(err error)) {
	if handler == nil {
		log.Fatal().Msg("handler is nil")
	}
	currentFffiErrorHandler = handler
}
