//go:build !bootstrap

package demo

import (
	"crypto/rand"

	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/utils"
)

func MakeHexEditorDemo() func() {
	var ptr imgui.ImHexEditorPtr
	var editor *utils.FinalizeWrapper[imgui.ImHexEditorPtr]
	data := make([]byte, 100, 100)
	_, _ = rand.Read(data)
	return func() {
		if ptr == 0 {
			ptr = imgui.NewHexEditor()
			editor = utils.NewFinalizeWrapper(ptr, func(ptr imgui.ImHexEditorPtr) { ptr.Destroy() })
			ptr.SetData(data)
		}
		editor.Get().DrawContents()
	}
}
