//go:build fffi_idl_code

package imgui

// TextUnformatted
func TextUnformatted(text string) {
	_ = `ImGui::TextUnformatted(text,text+getStringLength(text))`
}

// TextUnformatted
func LabelText(label string, text string) {
	_ = `ImGui::LabelText(label,"%.*s",(int)getStringLength(text),text)`
}

// BulletText
func BulletText(text string) {
	_ = `ImGui::BulletText("%.*s",(int)getStringLength(text),text)`
}

func GetIoDeltaTime() (dt float32) {
	_ = `dt = ImGui::GetIO().DeltaTime`
	return
}

func CalcTextWidth(text string) (r ImVec2) {
	_ = `auto r = ImGui::CalcTextSize(text,text+getStringLength(text))`
	return
}
func CalcTextWidthV(text string, hideTextAfterDoubleHash bool, floatWrapWidth float32) (r ImVec2) {
	_ = `auto r = ImGui::CalcTextSize(text,text+getStringLength(text),hideTextAfterDoubleHash,floatWrapWidth)`
	return
}
