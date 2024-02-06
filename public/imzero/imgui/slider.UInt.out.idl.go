//go:build fffi_idl_code

package imgui

func SliderUInt(label string, vP uint, p_min uint, p_max uint) (v uint, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U32,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderUIntV(label string, vP uint, p_min uint, p_max uint, format string, flags ImGuiSliderFlags) (v uint, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U32,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderUIntNV(label string, vP []uint, v_min uint, v_max uint, format string, flags ImGuiSliderFlags) (v []uint, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderUIntN(label string, vP []uint, v_min uint, v_max uint) (v []uint, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
