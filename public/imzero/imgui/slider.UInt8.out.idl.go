//go:build fffi_idl_code

package imgui

func SliderUInt8(label string, vP uint8, p_min uint8, p_max uint8) (v uint8, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U8,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}

func SliderUInt8V(label string, vP uint8, p_min uint8, p_max uint8, format string, flags ImGuiSliderFlags) (v uint8, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U8,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}

func SliderUInt8NV(label string, vP []uint8, v_min uint8, v_max uint8, format string, flags ImGuiSliderFlags) (v []uint8, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U8,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}

func SliderUInt8N(label string, vP []uint8, v_min uint8, v_max uint8) (v []uint8, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U8,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
