//go:build fffi_idl_code

package imgui

func BringCurrentWindowToDisplayFront() {
	_ = `ImGui::BringWindowToDisplayFront(ImGui::GetCurrentWindow())`
}
