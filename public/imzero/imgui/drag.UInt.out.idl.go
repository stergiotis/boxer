//go:build fffi_idl_code

package imgui

func DragUInt(label string, vP uint) (v uint, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U32,(void*)&vP);
v = vP;
`
	return
}
func DragUIntV(label string, vP uint, v_speed float32, p_min uint, p_max uint, format string, flags ImGuiSliderFlags) (v uint, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U32,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragUIntNV(label string, vP []uint, v_speed float32, v_min uint, v_max uint, format string, flags ImGuiSliderFlags) (v []uint, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragUIntN(label string, vP []uint) (v []uint, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len);
v = vP;
`
	return
}
