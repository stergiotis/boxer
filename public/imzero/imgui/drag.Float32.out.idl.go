//go:build fffi_idl_code

package imgui

func DragFloat32(label string, vP float32) (v float32, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_Float,(void*)&vP);
v = vP;
`
	return
}
func DragFloat32V(label string, vP float32, v_speed float32, p_min float32, p_max float32, format string, flags ImGuiSliderFlags) (v float32, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_Float,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}
func DragFloat32NV(label string, vP []float32, v_speed float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v []float32, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_Float,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}
func DragFloat32N(label string, vP []float32) (v []float32, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_Float,(void*)vP,(int)v_len);
v = vP;
`
	return
}
