//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

func RenderPieMenuDemo() {
	if imgui.IsWindowHovered() && imgui.IsMouseClickedV(1, false) {
		imgui.OpenPopupV("PieMenu", 0)
	}

	if imgui.BeginPiePopupV("PieMenu", 1) {
		if imgui.PieMenuItem("Test1") {

		}
		if imgui.PieMenuItem("Test2") {

		}
		if imgui.PieMenuItemV("Test3", false) {

		}
		if imgui.BeginPieMenu("Sub") {
			if imgui.BeginPieMenu("Sub sub\nmenu") {
				if imgui.PieMenuItem("SubSub") {

				}
				if imgui.PieMenuItem("SubSub2") {

				}
				imgui.EndPieMenu()
			}
			if imgui.PieMenuItem("TestSub") {

			}
			if imgui.PieMenuItem("TestSub2") {

			}
			imgui.EndPieMenu()
		}
		if imgui.BeginPieMenu("Sub2") {
			if imgui.PieMenuItem("TestSub") {
			}
			if imgui.BeginPieMenu("Sub sub\nmenu") {
				if imgui.PieMenuItem("SubSub") {
				}
				if imgui.PieMenuItem("SubSub2") {
				}
				imgui.EndPieMenu()
			}
			if imgui.PieMenuItem("TestSub2") {
			}
			imgui.EndPieMenu()
		}

		imgui.EndPiePopup()
	}
}
