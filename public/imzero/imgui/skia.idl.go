//go:build fffi_idl_code

package imgui

func GetSkiaFontDyFudge() (fudge float32) {
	_ = `fudge = ImGui::skiaFontDyFudge;`
	return
}
func SetSkiaFontDyFudge(fudge float32) {
	_ = `ImGui::skiaFontDyFudge = fudge;`
	return
}
