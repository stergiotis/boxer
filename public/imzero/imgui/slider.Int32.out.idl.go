//go:build fffi_idl_code

package imgui

func SliderInt32(label string, vP int32, p_min int32, p_max int32) (v int32, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S32,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderInt32V(label string, vP int32,  p_min int32, p_max int32, format string, flags ImGuiSliderFlags) (v int32, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_S32,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderInt32NV(label string, vP []int32,  v_min int32, v_max int32, format string, flags ImGuiSliderFlags) (v []int32, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderInt32N(label string, vP []int32, v_min int32, v_max int32) (v []int32, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
