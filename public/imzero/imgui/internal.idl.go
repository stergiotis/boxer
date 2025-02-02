//go:build fffi_idl_code

package imgui

func BringCurrentWindowToDisplayFront() {
	_ = `ImGui::BringWindowToDisplayFront(ImGui::GetCurrentWindow())`
}
func GetIdPreviousFrame() (hoveredId ImGuiID, activeId ImGuiID) {
	_ = `
	ImGuiContext& g = *GImGui;
	hoveredId = g.HoveredIdPreviousFrame;
	activeId = g.ActiveId;
`
	return
}
