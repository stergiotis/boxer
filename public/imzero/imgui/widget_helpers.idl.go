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
