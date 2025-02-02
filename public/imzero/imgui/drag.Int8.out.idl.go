//go:build fffi_idl_code

package imgui

func DragInt8(label string, vP int8) (v int8, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S8,(void*)&vP);
v = vP;
`
	return
}
func DragInt8V(label string, vP int8, v_speed float32, p_min int8, p_max int8, format string, flags ImGuiSliderFlags) (v int8, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S8,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragInt8NV(label string, vP []int8, v_speed float32, v_min int8, v_max int8, format string, flags ImGuiSliderFlags) (v []int8, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S8,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragInt8N(label string, vP []int8) (v []int8, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S8,(void*)vP,(int)v_len);
v = vP;
`
	return
}
