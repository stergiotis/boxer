//go:build !bootstrap

package demo

import (
	"reflect"

	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"github.com/yassinebenaid/godump"
)

var passColor = imgui.Color32U8(0, 255, 0, 255)

var failColor = imgui.Color32U8(255, 0, 0, 255)

func MakeRenderImGuiStyleDemo() func() {
	style1 := &imgui.ImGuiStyle{}
	style2 := &imgui.ImGuiStyle{}
	tested := false
	passed := false
	dumper := godump.Dumper{
		Indentation:             "  ",
		ShowPrimitiveNamedTypes: false,
		HidePrivateFields:       false,
		Theme:                   godump.Theme{},
	}
	return func() {
		ptr := imgui.GetStyle()
		style1.Load(ptr)
		s1 := dumper.Sprint(style1)
		if !tested && imgui.Button("Test Load/Dump") {
			style1.Dump(ptr)
			style2.Load(ptr)
			passed = reflect.DeepEqual(style1, style1)
			tested = true
		}
		if tested {
			if passed {
				imgui.PushStyleColor(imgui.ImGuiCol_Text, passColor)
				imgui.TextUnformatted("PASS")
				imgui.PopStyleColor()
			} else {
				imgui.PushStyleColor(imgui.ImGuiCol_Text, failColor)
				imgui.TextUnformatted("FAIL")
				imgui.PopStyleColor()
			}
		}
		imgui.TextUnformatted(s1)
	}
}

func MakeRenderImPlotStyleDemo() func() {
	style1 := &implot.ImPlotStyle{}
	style2 := &implot.ImPlotStyle{}
	tested := false
	passed := false
	dumper := godump.Dumper{
		Indentation:             "  ",
		ShowPrimitiveNamedTypes: false,
		HidePrivateFields:       false,
		Theme:                   godump.Theme{},
	}
	return func() {
		ptr := implot.GetStyle()
		style1.Load(ptr)

		s1 := dumper.Sprint(style1)
		if !tested && imgui.Button("Test Load/Dump") {
			style1.Dump(ptr)
			style2.Load(ptr)
			passed = reflect.DeepEqual(style1, style1)
			tested = true
		}
		if tested {
			if passed {
				imgui.PushStyleColor(imgui.ImGuiCol_Text, passColor)
				imgui.TextUnformatted("PASS")
				imgui.PopStyleColor()
			} else {
				imgui.PushStyleColor(imgui.ImGuiCol_Text, failColor)
				imgui.TextUnformatted("FAIL")
				imgui.PopStyleColor()
			}
		}
		imgui.TextUnformatted(s1)
	}
}
