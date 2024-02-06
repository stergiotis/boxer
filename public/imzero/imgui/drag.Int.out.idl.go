//go:build fffi_idl_code

package imgui

func DragInt(label string, vP int) (v int, r bool) {
_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S32,(void*)&vP);
v = vP;
`
	return
}
func DragIntV(label string, vP int, v_speed float32, p_min int, p_max int, format string, flags ImGuiSliderFlags) (v int, r bool) {
_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S32,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragIntNV(label string, vP []int, v_speed float32, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v []int, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragIntN(label string, vP []int) (v []int, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len);
v = vP;
`
	return
}
