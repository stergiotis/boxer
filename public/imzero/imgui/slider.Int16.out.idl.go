//go:build fffi_idl_code

package imgui

func SliderInt16(label string, vP int16, p_min int16, p_max int16) (v int16, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S16,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderInt16V(label string, vP int16, p_min int16, p_max int16, format string, flags ImGuiSliderFlags) (v int16, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S16,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderInt16NV(label string, vP []int16, v_min int16, v_max int16, format string, flags ImGuiSliderFlags) (v []int16, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S16,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderInt16N(label string, vP []int16, v_min int16, v_max int16) (v []int16, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S16,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
