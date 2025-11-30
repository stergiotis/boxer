package egui

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/fffi/runtime"
)

type Egui struct {
	fffi *runtime.Fffi2
	errs []error
}

func NewEgui(fffi *runtime.Fffi2) *Egui {
	return &Egui{fffi: fffi, errs: make([]error, 0, 8)}
}

func (inst *Egui) HasErrors() bool {
	return len(inst.errs) > 0
}

func (inst *Egui) Errors() []error {
	return inst.errs
}

func (inst *Egui) ResetErrors() {
	inst.errs = inst.errs[:0]
}

func (inst *Egui) getFffi() *runtime.Fffi2 {
	return inst.fffi
}

func (inst *Egui) handleError(err error) {
	inst.errs = append(inst.errs, err)
}

var currentFffiVar *runtime.Fffi2

var errs = make([]error, 0, 128)

var currentFffiErrorHandler func(err error) = func(err error) {
	if err == nil {
		return
	}
	errs = append(errs, err)
	log.Error().Err(err).Msg("fffi runtime error")
}

func HasErrors() bool {
	return len(errs) > 0
}

func Errors() []error {
	return errs
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
