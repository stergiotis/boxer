//go:build fffi_idl_code

package imgui

func SliderFloat32(label string, vP float32, p_min float32, p_max float32) (v float32, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_Float,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderFloat32V(label string, vP float32, p_min float32, p_max float32, format string, flags ImGuiSliderFlags) (v float32, r bool) {
	_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_Float,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderFloat32NV(label string, vP []float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v []float32, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_Float,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderFloat32N(label string, vP []float32, v_min float32, v_max float32) (v []float32, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_Float,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
