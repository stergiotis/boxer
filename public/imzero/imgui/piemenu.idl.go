//go:build fffi_idl_code

package imgui

func BeginPiePopup(name string) (r bool) {
	_ = `r = ImGui::BeginPiePopup(name)`
	return
}

func BeginPiePopupV(name string, iMouseButton int) (r bool) {
	_ = `r = ImGui::BeginPiePopup(name,iMouseButton)`
	return
}

func EndPiePopup() {
	_ = `ImGui::EndPiePopup()`
}

func PieMenuItem(name string) (r bool) {
	_ = `r = ImGui::PieMenuItem(name)`
	return
}

func PieMenuItemV(name string, bEnabled /* = true */ bool) (r bool) {
	_ = `r = ImGui::PieMenuItem(name,bEnabled)`
	return
}

func BeginPieMenu(name string) (r bool) {
	_ = `r = ImGui::BeginPieMenu(name)`
	return
}

func BeginPieMenuV(name string, bEnabled /* = true */ bool) (r bool) {
	_ = `r = ImGui::BeginPieMenu(name,bEnabled)`
	return
}

func EndPieMenu() {
	_ = `ImGui::EndPieMenu()`
}
