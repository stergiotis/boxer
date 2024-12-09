//go:build fffi_idl_code

package imgui

func DragInt32(label string, vP int32) (v int32, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S32,(void*)&vP);
v = vP;
`
	return
}

func DragInt32V(label string, vP int32, v_speed float32, p_min int32, p_max int32, format string, flags ImGuiSliderFlags) (v int32, r bool) {
	_ = `
r = ImGui::DragScalar(label,ImGuiDataType_S32,(void*)&vP,v_speed,(const void*)&p_min,(const void*)&p_max,format,flags);
v = vP;
`
	return
}

func DragInt32NV(label string, vP []int32, v_speed float32, v_min int32, v_max int32, format string, flags ImGuiSliderFlags) (v []int32, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len,v_speed,(const void*)&v_min,(const void*)&v_max,format,flags);
v = vP;
`
	return
}

func DragInt32N(label string, vP []int32) (v []int32, r bool) {
	_ = `
size_t v_len = getSliceLength(vP);
r = ImGui::DragScalarN(label,ImGuiDataType_S32,(void*)vP,(int)v_len);
v = vP;
`
	return
}
