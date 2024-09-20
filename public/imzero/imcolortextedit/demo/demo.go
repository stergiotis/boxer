//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/imcolortextedit"
	"github.com/stergiotis/boxer/public/imzero/imgui"
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
		editor := ptrWrapper.Get()
		if imgui.SmallButton("C++") {
			editor.ActivateLanguageCPlusPlus()
		}
		imgui.SameLine()
		if imgui.SmallButton("Python") {
			editor.ActivateLanguagePython()
		}
		imgui.SameLine()
		if imgui.SmallButton("C") {
			editor.ActivateLanguageC()
		}
		imgui.SameLine()
		if imgui.SmallButton("SQL") {
			editor.ActivateLanguageSQL()
		}
		imgui.SameLine()
		if imgui.SmallButton("Json") {
			editor.ActivateLanguageJson()
		}

		if imgui.SmallButton("Dark") {
			editor.ActivatePaletteDark()
		}
		imgui.SameLine()
		if imgui.SmallButton("Light") {
			editor.ActivatePaletteLight()
		}
		imgui.SameLine()
		if imgui.SmallButton("RetroBlue") {
			editor.ActivatePaletteMariana()
		}
		imgui.SameLine()
		if imgui.SmallButton("Mariana") {
			editor.ActivatePaletteMariana()
		}

		editor.Render("demo")
	}
}
