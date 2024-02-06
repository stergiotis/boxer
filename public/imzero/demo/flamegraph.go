//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

func MakeFlameGraphDemo() func() {
	starts := []float32{0.0, 0.2, 0.8, 0.1}
	stops := []float32{0.2, 0.4, 1.0, 0.15}
	levels := []uint8{0, 0, 0, 1}
	captions := []string{"a", "b", "c", "d"}
	return func() {
		imgui.PlotFlameV("flamegraph", starts, stops, levels, captions, "my overlay text", 0.0, 1.0, imgui.MakeImVec2(200.0, 100.0))
	}
}
