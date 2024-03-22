//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

func RenderAssertDemo() {
	if imgui.Button("trigger assertion in ImGui::EndFrame(...)") {
		imgui.End()
	}
	if imgui.Button("trigger immediate assertion") {
		imgui.PopStyleColor()
	}
}
