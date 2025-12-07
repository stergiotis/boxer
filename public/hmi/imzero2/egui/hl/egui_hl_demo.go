package hl

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/hmi/imzero2/egui"
)

var n = 0

func RenderLoopHandler(marshaller *runtime.Marshaller) error {
	egui.WidgetLabel(time.Now().GoString()).Build()
	r := egui.WidgetButton("okay?").Build().Get()
	if r.HasPrimaryClicked() {
		n++
	} else if r.HasSecondaryClicked() {
		n--
	}
	egui.WidgetLabel(fmt.Sprintf("%d", n)).Selectable(false).Build()
	for range LayoutHorizontal() {
		egui.WidgetLabel("a").Build()
		egui.WidgetLabel("b").Build()
		egui.WidgetLabel("c").Build()
	}
	//egui.WidgetTree()
	return nil
}
