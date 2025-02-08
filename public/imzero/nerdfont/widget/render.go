//go:build !bootstrap

package widget

import (
	"fmt"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/nerdfont"
	"github.com/stergiotis/boxer/public/logical"
)

func MakeIconPickRender(iconFont imgui.ImFontPtr) func() {
	//indices := make([]int, 100, 100)
	//nResults := -1
	return func() {
		if imgui.BeginTableV("nerdfont", 4, imgui.ImGuiTableFlags_None, 0, 0.0) {
			var colVisState [4]logical.Tristate
			imgui.TableSetupColumnV("Name", imgui.ImGuiTableColumnFlags_None, 0, 0)
			imgui.TableSetupColumnV("Original Name", imgui.ImGuiTableColumnFlags_None, 0, 0)
			imgui.TableSetupColumnV("Symbol/Glyph", imgui.ImGuiTableColumnFlags_None, 0, 1)
			imgui.TableSetupColumnV("Codepoint", imgui.ImGuiTableColumnFlags_None, 0, 2)
			imgui.TableSetupScrollFreeze(0, 1)
			imgui.TableHeadersRow()
			for row := 0; row < 10; row++ {
				imgui.TableNextRow()
				if imgui.TableNextColumnS(&colVisState[0]) {
					imgui.TextUnformatted(nerdfont.Names[row])
				}
				if imgui.TableNextColumnS(&colVisState[1]) {
					imgui.TextUnformatted(nerdfont.OriginalNames[row])
				}
				if imgui.TableNextColumnS(&colVisState[2]) {
					imgui.PushFont(iconFont)
					imgui.TextUnformatted(string(nerdfont.CodePoints[row]))
					imgui.PopFont()
				}
				if imgui.TableNextColumnS(&colVisState[3]) {
					imgui.TextUnformatted(fmt.Sprintf("0x%08x", nerdfont.CodePoints[row]))
				}
			}
			imgui.EndTable()
		}
	}
}
