//go:build fffi_idl_code

package imgui

func DragFloat64(label string, vP float64) (v float64, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_Double,(void*)&vP);
v = vP;
`
	return
}
func DragFloat64V(label string, vP float64, v_speed float32, p_min float64, p_max float64, format string, flags ImGuiSliderFlags) (v float64, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_Double,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragFloat64NV(label string, vP []float64, v_speed float32, v_min float64, v_max float64, format string, flags ImGuiSliderFlags) (v []float64, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_Double,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragFloat64N(label string, vP []float64) (v []float64, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_Double,(void*)vP,(int)v_len);
v = vP;
`
	return
}
