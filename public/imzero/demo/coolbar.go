//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/application"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/nerdfont"
)

func MakeCoolbarDemoNonIdiomatic() func() {
	var cfg imgui.ImCoolBarConfigForeignPtr
	var sz imgui.ImVec2
	button := func(label string) bool {
		width, scale := imgui.CoolBarItemProperties()
		_ = scale
		sz = imgui.MakeImVec2(width, width)
		return imgui.ButtonV(label, sz)
	}
	return func() {
		if cfg == 0 {
			cfg = imgui.MakeImCoolBarConfig()
			_, normalSize, hoveredSize, animStep, effectStrength := cfg.Get()
			cfg.Set(imgui.MakeImVec2(0.0, 0.0), normalSize, hoveredSize, animStep, effectStrength)
		}
		if imgui.BeginCoolBarV("##CoolBarMain", imgui.ImCoolBarFlags_Horizontal, cfg, 0) {
			if imgui.CoolBarItem() {
				if button("A") {

				}
			}
			if imgui.CoolBarItem() {
				if button("B") {

				}
			}
			if imgui.CoolBarItem() {
				if button("C") {

				}
			}
			if imgui.CoolBarItem() {
				if button("D") {

				}
			}
			if imgui.CoolBarItem() {
				if button("E") {

				}
			}
			if imgui.CoolBarItem() {
				if button("F") {

				}
			}
			if imgui.CoolBarItem() {
				if button("G") {

				}
			}
			imgui.EndCoolBar()
		}
	}
}
func MakeCoolbarDemo(app *application.Application) func() {
	var cfg imgui.ImCoolBarConfigForeignPtr
	//var buttons = []string{"A", "B", "C", "D", "E", "F"}
	var buttons = []string{nerdfont.LinuxGnuGuix, nerdfont.CodAccount, nerdfont.FaAddressBook, nerdfont.CodArrowSmallDown, nerdfont.CodArchive, nerdfont.CodArrowBoth}
	var tooltips = []string{"button A", "button B", "button C", "button D", "button E", "button F"}
	iconFont := app.IconFont
	return func() {
		if cfg == 0 {
			cfg = imgui.MakeImCoolBarConfig()
			_, normalSize, hoveredSize, animStep, effectStrength := cfg.Get()
			cfg.Set(imgui.MakeImVec2(0.0, 0.0), normalSize, hoveredSize, animStep, effectStrength)
		}
		if imgui.BeginCoolBarV("##CoolBarMain", imgui.ImCoolBarFlags_Horizontal, cfg, 0) {
			imgui.BringCurrentWindowToDisplayFront()
			imgui.PushStyleVar(imgui.ImGuiStyleVar_FrameRounding, 4.0)
			clicked, hovered := imgui.CoolBarButtons(iconFont, buttons, tooltips)
			imgui.PopStyleVar()
			_ = clicked
			_ = hovered
			imgui.EndCoolBar()
		}
	}
}
