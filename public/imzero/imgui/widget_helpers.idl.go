//go:build fffi_idl_code

package imgui

// InvisibleButtonP flexible button behavior without the visuals, frequently useful to build custom behaviors using the public api (along with IsItemActive, IsItemHovered, etc.)
func InvisibleButtonP(str_id string, size ImVec2) {
	_ = `ImGui::InvisibleButton(str_id, size)`
}

// InvisibleButtonVP flexible button behavior without the visuals, frequently useful to build custom behaviors using the public api (along with IsItemActive, IsItemHovered, etc.)
// * flags ImGuiButtonFlags = 0
func InvisibleButtonVP(str_id string, size ImVec2, flags ImGuiButtonFlags /* = 0*/) {
	_ = `ImGui::InvisibleButton(str_id, size, flags)`
}

// Selectable "bool selected" carry the selection state (read-only). Selectable() is clicked is returns true so you can modify your selection state. size.x==0.0: use remaining width, size.x>0.0: specify width. size.y==0.0: use label height, size.y>0.0: specify height
func SelectableP(label string) {
	_ = `ImGui::Selectable(label)`
	return
}

// SelectableV "bool selected" carry the selection state (read-only). Selectable() is clicked is returns true so you can modify your selection state. size.x==0.0: use remaining width, size.x>0.0: specify width. size.y==0.0: use label height, size.y>0.0: specify height
// * selected bool = false
// * flags ImGuiSelectableFlags = 0
// * size const ImVec2 & = ImVec2(0, 0)
func SelectableVP(label string, selected bool /* = false*/, flags ImGuiSelectableFlags /* = 0*/, size ImVec2 /* = ImVec2(0, 0)*/) {
	_ = `ImGui::Selectable(label, selected, flags, size)`
	return
}

// Button button
func ButtonP(label string) {
	_ = `ImGui::Button(label)`
	return
}

// ButtonV button
// * size const ImVec2 & = ImVec2(0, 0)
func ButtonVP(label string, size ImVec2 /* = ImVec2(0, 0)*/) {
	_ = `ImGui::Button(label, size)`
	return
}

// SmallButton button with (FramePadding.y == 0) to easily embed within text
func SmallButtonP(label string) {
	_ = `ImGui::SmallButton(label)`
	return
}
