//go:build fffi_idl_code

package imgui

func InputText(label string, textIn string, maxLength Size_t) (textOut string, changed bool) {
	_ = `
auto textOut = (char *)arenaMalloc(maxLength);
memcpy(textOut,textIn,getStringLength(textIn)+1);
changed = ImGui::InputText(label,textOut,maxLength);
auto textOut_len = strlen(textOut);
`
	return
}
func InputTextV(label string, textIn string, maxLength Size_t, flags ImGuiInputTextFlags) (textOut string, changed bool) {
	_ = `
auto textOut = (char *)arenaMalloc(maxLength);
memcpy(textOut,textIn,getStringLength(textIn)+1);
changed = ImGui::InputText(label,textOut,maxLength,flags);
auto textOut_len = strlen(textOut);
`
	return
}
func InputTextWithHint(label string, hint string, textIn string, maxLength Size_t) (textOut string, changed bool) {
	_ = `
auto textOut = (char *)arenaMalloc(maxLength);
memcpy(textOut,textIn,getStringLength(textIn)+1);
changed = ImGui::InputTextWithHint(label,hint,textOut,maxLength);
auto textOut_len = strlen(textOut);
`
	return
}
func InputTextWithHintV(label string, hint string, textIn string, maxLength Size_t, flags ImGuiInputTextFlags) (textOut string, changed bool) {
	_ = `
auto textOut = (char *)arenaMalloc(maxLength);
memcpy(textOut,textIn,getStringLength(textIn)+1);
changed = ImGui::InputTextWithHint(label,hint,textOut,maxLength,flags);
auto textOut_len = strlen(textOut);
`
	return
}
