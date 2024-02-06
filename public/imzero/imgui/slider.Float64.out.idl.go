//go:build fffi_idl_code

package imgui

func SliderFloat64(label string, vP float64, p_min float64, p_max float64) (v float64, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_Double,(void*)&vP,(const void*)&p_min,(const void*)&p_max);
v = vP;
`
	return
}
func SliderFloat64V(label string, vP float64,  p_min float64, p_max float64, format string, flags ImGuiSliderFlags) (v float64, r bool) {
_ = `
r = ImGui::SliderScalar(label,ImGuiDataType_Double,(void*)&vP,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func SliderFloat64NV(label string, vP []float64,  v_min float64, v_max float64, format string, flags ImGuiSliderFlags) (v []float64, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_Double,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func SliderFloat64N(label string, vP []float64, v_min float64, v_max float64) (v []float64, r bool) {
_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::SliderScalarN(label,ImGuiDataType_Double,(void*)vP,(int)v_len,(const void*)&v_min,(const void*)&v_max);
v = vP;
`
	return
}
