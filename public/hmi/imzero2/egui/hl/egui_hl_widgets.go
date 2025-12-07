package hl

import (
	"iter"

	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/hmi/imzero2/egui"
)

func LayoutHorizontal() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		egui.BeginHorizontal()
		yield(functional.NilIteratorValue)
		egui.End()
	}
}
func Button(label string) (flags egui.ResponseFlags) {
	//egui.R0AtomPushText(label)
	egui.WidgetButton(label).Build()
	flags = egui.R1Get()
	return
}
func Label(label string) (flags egui.ResponseFlags) {
	//egui.R0AtomPushText(label)
	egui.WidgetButton(label).Build()
	flags = egui.R1Get()
	return
}
