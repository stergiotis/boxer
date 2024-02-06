//go:build !bootstrap

package widget

import (
	"fmt"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/nerdfont"
)

func MakeIconPickRender(iconFont imgui.ImFontPtr) func() {
	//indices := make([]int, 100, 100)
	//nResults := -1
	return func() {
		if imgui.BeginTableV("nerdfont", 4, imgui.ImGuiTableFlags_None, 0, 0.0) {
			imgui.TableSetupColumnV("Name", imgui.ImGuiTableColumnFlags_None, 0, 0)
			imgui.TableSetupColumnV("Original Name", imgui.ImGuiTableColumnFlags_None, 0, 0)
			imgui.TableSetupColumnV("Symbol/Glyph", imgui.ImGuiTableColumnFlags_None, 0, 1)
			imgui.TableSetupColumnV("Codepoint", imgui.ImGuiTableColumnFlags_None, 0, 2)
			imgui.TableSetupScrollFreeze(0, 1)
			imgui.TableHeadersRow()
			for row := 0; row < 10; row++ {
				imgui.TableNextRow()
				imgui.TableNextColumn()
				imgui.TextUnformatted(nerdfont.Names[row])
				imgui.TableNextColumn()
				imgui.TextUnformatted(nerdfont.OriginalNames[row])
				imgui.TableNextColumn()
				imgui.PushFont(iconFont)
				imgui.TextUnformatted(string(nerdfont.CodePoints[row]))
				imgui.PopFont()
				imgui.TableNextColumn()
				imgui.TextUnformatted(fmt.Sprintf("0x%08x", nerdfont.CodePoints[row]))
			}
			imgui.EndTable()
		}
	}
}
