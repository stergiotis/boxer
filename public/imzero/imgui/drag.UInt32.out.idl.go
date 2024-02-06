//go:build fffi_idl_code

package imgui

func DragUInt32(label string, vP uint32) (v uint32, r bool) {
_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U32,(void*)&vP);
v = vP;
`
	return
}
func DragUInt32V(label string, vP uint32, v_speed float32, p_min uint32, p_max uint32, format string, flags ImGuiSliderFlags) (v uint32, r bool) {
_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U32,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragUInt32NV(label string, vP []uint32, v_speed float32, v_min uint32, v_max uint32, format string, flags ImGuiSliderFlags) (v []uint32, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragUInt32N(label string, vP []uint32) (v []uint32, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len);
v = vP;
`
	return
}
