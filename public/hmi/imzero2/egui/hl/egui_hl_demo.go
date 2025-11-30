package hl

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/hmi/imzero2/egui"
)

var n = 0

func RenderLoopHandler(marshaller *runtime.Marshaller) error {
	egui.WidgetLabel(time.Now().GoString())
	r := Button("okay?")
	if r.HasPrimaryClicked() {
		n++
	} else if r.HasSecondaryClicked() {
		n--
	}
	egui.WidgetLabel(fmt.Sprintf("%d", n))
	for range LayoutHorizontal() {
		egui.WidgetLabel("a")
		egui.WidgetLabel("b")
		egui.WidgetLabel("c")
	}
	return nil
}
