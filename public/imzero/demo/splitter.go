//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

var sz1 float32 = 300.0

var sz2 float32 = 300.0

func RenderSplitterDemo() {
	var h float32 = 200
	_, sz1, sz2 = imgui.SplitterV(true, 8.0, sz1, sz2, 8, 8, h)
	imgui.BeginChildV("1", imgui.ImVec2(complex(sz1, h)), imgui.ImGuiChildFlags_Border, 0)
	imgui.EndChild()
	imgui.SameLine()
	imgui.BeginChildV("2", imgui.ImVec2(complex(sz2, h)), imgui.ImGuiChildFlags_Border, 0)
	imgui.EndChild()
}
