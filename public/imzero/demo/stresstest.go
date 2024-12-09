//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

func RenderFffiBestCaseDemo(n int) {
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			imgui.PushStyleColor(imgui.ImGuiCol_Text, 0xbbff9933)
		}
		imgui.PushIDInt(i)
		imgui.TextUnformatted("best!")
		imgui.PopID()
		if i%2 == 0 {
			imgui.PopStyleColorV(1)
		}
		if i%24 != 0 {
			imgui.SameLineV(0, -1.0)
		}
	}
}

func RenderFffiWorstCaseDemo(n int) {
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			imgui.PushStyleColor(imgui.ImGuiCol_Text, 0xbbff9911)
		}
		imgui.PushIDInt(i)
		imgui.TextUnformatted("worst")
		imgui.PopID()
		// NOTE: this is very bad code, the function call below prevents batching multiple imgui calls
		if imgui.IsItemHoveredV(0) {
			imgui.BeginTooltip()
			imgui.Text("hoooooover %d", i)
			imgui.EndTooltip()
		}
		if i%2 == 0 {
			imgui.PopStyleColorV(1)
		}
		if i%24 != 0 {
			imgui.SameLineV(0, -1.0)
		}
	}
}
