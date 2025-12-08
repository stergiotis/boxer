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
	{
		for range egui.R3NodeDirPush(0).Label("dir 0").BuildAndClose() {
			for range egui.R3NodeDirPush(1).Label("dir 1").BuildAndClose() {
				egui.R3NodeLeafPush(2).Label("leaf 0").Build()
				egui.R3NodeLeafPush(3).Label("leaf 1").Build()
				egui.R3NodeLeafPush(4).Label("leaf 3").Build()
			}
			for range egui.R3NodeDirPush(5).Label("dir 2").BuildAndClose() {
				egui.R3NodeLeafPush(7).Label("leaf 2").Build()
				egui.R3NodeLeafPush(7).Label("leaf 3").Build()
			}
		}
		egui.WidgetTree()
	}
	return nil
}
