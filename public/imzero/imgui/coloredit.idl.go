//go:build fffi_idl_code

package imgui

func ColorEdit3(label string, colP [3]float32, flags ImGuiColorEditFlags) (col [3]float32, changed bool) {
	_ = `changed = ImGui::ColorEdit3(label,colP,flags);
col = colP;`
	return
}

func ColorEdit4(label string, colP ImVec4, flags ImGuiColorEditFlags) (col ImVec4, changed bool) {
	_ = `changed = ImGui::ColorEdit4(label,colP,flags);
col = colP;`
	return
}
