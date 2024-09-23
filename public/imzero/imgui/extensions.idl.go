//go:build fffi_idl_code

package imgui

import "github.com/stergiotis/boxer/public/imzero/dto"

func PushIsParagraphText(val dto.IsParagraphText) {
	_ = `ImGui::PushIsParagraphText(val)`
}
func PopIsParagraphText() {
	_ = `ImGui::PopIsParagraphText()`
}
func PushParagraphTextLayout(align dto.TextAlignFlags, dir dto.TextDirection) {
	_ = `ImGui::PushParagraphTextLayout(align,dir)`
}
func PopParagraphTextLayout() {
	_ = `ImGui::PopParagraphTextLayout()`
}
