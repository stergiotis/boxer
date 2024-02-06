//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imgui"

var state bool

func RenderSimpleDemo() {
	if imgui.Button("button text") {
		state = !state
	}
	if state {
		imgui.TextUnformatted("my text")
	}
	imgui.Begin("an empty window")
	imgui.End()
}
