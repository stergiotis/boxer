package imcolortextedit

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/runtime"
)

type ImColorEditor struct {
	fffi *runtime.Fffi2
	errs []error
}

func NewImColorEditor(fffi *runtime.Fffi2) *ImColorEditor {
	return &ImColorEditor{fffi: fffi, errs: make([]error, 0, 8)}
}
func (inst *ImColorEditor) HasErrors() bool {
	return len(inst.errs) > 0
}
func (inst *ImColorEditor) Errors() []error {
	return inst.errs
}
func (inst *ImColorEditor) ResetErrors() {
	inst.errs = inst.errs[:0]
}
func (inst *ImColorEditor) getFffi() *runtime.Fffi2 {
	return inst.fffi
}
func (inst *ImColorEditor) handleError(err error) {
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
func (ImColorEditorForeignPtr) getFffi() *runtime.Fffi2 {
	return currentFffiVar
}
func (ImColorEditorForeignPtr) handleError(err error) {
	currentFffiErrorHandler(err)
}
