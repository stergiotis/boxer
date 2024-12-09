//go:build fffi_idl_code

package imgui

func SliderUInt16(label string, vP uint16, p_min uint16, p_max uint16) (v uint16, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U16,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}

func SliderUInt16V(label string, vP uint16, p_min uint16, p_max uint16, format string, flags ImGuiSliderFlags) (v uint16, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U16,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}

func SliderUInt16NV(label string, vP []uint16, v_min uint16, v_max uint16, format string, flags ImGuiSliderFlags) (v []uint16, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U16,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}

func SliderUInt16N(label string, vP []uint16, v_min uint16, v_max uint16) (v []uint16, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U16,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
