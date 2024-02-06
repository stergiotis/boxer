//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

func MakeRenderSpinnerDemo() func() {
	color := imgui.Color32U8(255, 0, 0, 255)
	var nextdot float32 = 0.2
	return func() {
		nextdot -= 0.07
		imgui.SpinnerHerbertBalls3D("herbertsBalls3d", 20.0, 2.0, color, 1.0)
		imgui.SpinnerRainbow("rainbow", 20.0, 2.0, color, 1.0)
		imgui.SpinnerFadeTris("fadeTris", 20.0)
		nextdot = imgui.SpinnerDots("dots", nextdot, 20.0, 4.0)
		imgui.SeparatorText("built-in demo")
		imgui.SpinnerDemos()
	}
}
