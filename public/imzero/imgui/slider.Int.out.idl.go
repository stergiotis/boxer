//go:build fffi_idl_code

package imgui

func SliderInt(label string, vP int, p_min int, p_max int) (v int, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S32,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderIntV(label string, vP int,  p_min int, p_max int, format string, flags ImGuiSliderFlags) (v int, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S32,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderIntNV(label string, vP []int,  v_min int, v_max int, format string, flags ImGuiSliderFlags) (v []int, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderIntN(label string, vP []int, v_min int, v_max int) (v []int, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
