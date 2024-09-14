//go:build fffi_idl_code

package imgui

func GetItemStatus() (status ItemStatusE) {
	_ = `status = GetItemStatus()`
	return
}
func GetItemStatusV(primary ImGuiHoveredFlags, secondary ImGuiHoveredFlags) (status ItemStatusE) {
	_ = `status = GetItemStatus(primary, secondary)`
	return
}

func CurrentCursorPos() (r ImVec2) {
	_ = `auto r = ImGui::GetCurrentWindow()->DC.CursorPos`
	return
}
func BeginCustomWidget() (visible bool, currentWindowDrawList ImDrawListPtr, pos ImVec2, availableRegion ImVec2, keyboardNavActive bool, seed ImGuiID) {
	_ = `ImVec2 pos;
         ImVec2 availableRegion;
         visible = BeginCustomWidget((ImDrawList**)&currentWindowDrawList,&pos,&availableRegion,&keyboardNavActive,&seed);
         `
	return
}
func SetTooltip(str string) {
	_ = `ImGui::SetTooltip("%.*s",(int)getStringLength(str),str)`
}
