//go:build fffi_idl_code

package imgui

func DragInt16(label string, vP int16) (v int16, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S16,(void*)&vP);
v = vP;
`
	return
}

func DragInt16V(label string, vP int16, v_speed float32, p_min int16, p_max int16, format string, flags ImGuiSliderFlags) (v int16, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S16,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}

func DragInt16NV(label string, vP []int16, v_speed float32, v_min int16, v_max int16, format string, flags ImGuiSliderFlags) (v []int16, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S16,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}

func DragInt16N(label string, vP []int16) (v []int16, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S16,(void*)vP,(int)v_len);
v = vP;
`
	return
}
