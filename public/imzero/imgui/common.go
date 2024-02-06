package imgui

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/runtime"
)

type ImGui struct {
	fffi *runtime.Fffi2
	errs []error
}

func NewImGui(fffi *runtime.Fffi2) *ImGui {
	return &ImGui{fffi: fffi, errs: make([]error, 0, 8)}
}
func (inst *ImGui) HasErrors() bool {
	return len(inst.errs) > 0
}
func (inst *ImGui) Errors() []error {
	return inst.errs
}
func (inst *ImGui) ResetErrors() {
	inst.errs = inst.errs[:0]
}
func (inst *ImGui) getFffi() *runtime.Fffi2 {
	return inst.fffi
}
func (inst *ImGui) handleError(err error) {
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
func (foreignptr ImFontPtr) getFffi() *runtime.Fffi2 {
	return currentFffiVar
}
func (foreignptr ImFontPtr) handleError(err error) {
	currentFffiErrorHandler(err)
}
func (foreignptr ImHexEditorPtr) getFffi() *runtime.Fffi2 {
	return currentFffiVar
}
func (foreignptr ImHexEditorPtr) handleError(err error) {
	currentFffiErrorHandler(err)
}
func (foreignptr ImDrawListPtr) getFffi() *runtime.Fffi2 {
	return currentFffiVar
}
func (foreignptr ImDrawListPtr) handleError(err error) {
	currentFffiErrorHandler(err)
}
