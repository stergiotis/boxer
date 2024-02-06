//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/imcolortextedit"
	"github.com/stergiotis/boxer/public/imzero/utils"
)

func MakeColorTextEditDemo() func() {
	var ptr imcolortextedit.ImColorEditorForeignPtr
	var ptrWrapper *utils.FinalizeWrapper[imcolortextedit.ImColorEditorForeignPtr]
	return func() {
		if ptr == 0 {
			ptr = imcolortextedit.NewImColorEditorForeignPtr()
			ptrWrapper = utils.NewFinalizeWrapper(ptr, func(ptr imcolortextedit.ImColorEditorForeignPtr) { ptr.Destroy() })
			ptr.SetText("hello\nworld!")
		}
		ptrWrapper.Get().Render("demo")
	}
}
