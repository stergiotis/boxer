//go:build fffi_idl_code

package imgui

func SliderUInt32(label string, vP uint32, p_min uint32, p_max uint32) (v uint32, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U32,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderUInt32V(label string, vP uint32,  p_min uint32, p_max uint32, format string, flags ImGuiSliderFlags) (v uint32, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_U32,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderUInt32NV(label string, vP []uint32,  v_min uint32, v_max uint32, format string, flags ImGuiSliderFlags) (v []uint32, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderUInt32N(label string, vP []uint32, v_min uint32, v_max uint32) (v []uint32, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_U32,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
