//go:build fffi_idl_code

package imgui

func DragUInt16(label string, vP uint16) (v uint16, r bool) {
_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U16,(void*)&vP);
v = vP;
`
	return
}
func DragUInt16V(label string, vP uint16, v_speed float32, p_min uint16, p_max uint16, format string, flags ImGuiSliderFlags) (v uint16, r bool) {
_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U16,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragUInt16NV(label string, vP []uint16, v_speed float32, v_min uint16, v_max uint16, format string, flags ImGuiSliderFlags) (v []uint16, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U16,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragUInt16N(label string, vP []uint16) (v []uint16, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U16,(void*)vP,(int)v_len);
v = vP;
`
	return
}
