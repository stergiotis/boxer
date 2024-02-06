//go:build fffi_idl_code

package imgui

func SliderInt8(label string, vP int8, p_min int8, p_max int8) (v int8, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S8,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderInt8V(label string, vP int8,  p_min int8, p_max int8, format string, flags ImGuiSliderFlags) (v int8, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S8,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderInt8NV(label string, vP []int8,  v_min int8, v_max int8, format string, flags ImGuiSliderFlags) (v []int8, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S8,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderInt8N(label string, vP []int8, v_min int8, v_max int8) (v []int8, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S8,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
