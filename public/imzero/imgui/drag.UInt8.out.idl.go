//go:build fffi_idl_code

package imgui

func DragUInt8(label string, vP uint8) (v uint8, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U8,(void*)&vP);
v = vP;
`
	return
}

func DragUInt8V(label string, vP uint8, v_speed float32, p_min uint8, p_max uint8, format string, flags ImGuiSliderFlags) (v uint8, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_U8,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}

func DragUInt8NV(label string, vP []uint8, v_speed float32, v_min uint8, v_max uint8, format string, flags ImGuiSliderFlags) (v []uint8, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U8,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}

func DragUInt8N(label string, vP []uint8) (v []uint8, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_U8,(void*)vP,(int)v_len);
v = vP;
`
	return
}
