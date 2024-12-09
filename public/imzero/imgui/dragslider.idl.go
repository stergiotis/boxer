//go:build fffi_idl_code

package imgui

func DragFloat(label string, vP float32) (v float32, r bool) {
	_ = `r = ImGui::DragFloat(label, &vP);
v = vP;`
	return
}

func DragFloatV(label string, vP float32, v_speed float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v float32, r bool) {
	_ = `r = ImGui::DragFloat(label, &vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func DragFloat2(label string, vP [2]float32) (v [2]float32, r bool) {
	_ = `r = ImGui::DragFloat2(label, vP);
v = vP;`
	return
}

func DragFloat2V(label string, vP [2]float32, v_speed float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v [2]float32, r bool) {
	_ = `r = ImGui::DragFloat2(label, vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func DragFloat3(label string, vP [3]float32) (v [2]float32, r bool) {
	_ = `r = ImGui::DragFloat3(label, vP);
v = vP;`
	return
}

func DragFloat3V(label string, vP [3]float32, v_speed float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v [4]float32, r bool) {
	_ = `r = ImGui::DragFloat3(label, vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func DragFloat4(label string, vP [4]float32) (v [4]float32, r bool) {
	_ = `r = ImGui::DragFloat4(label, vP);
v = vP;`
	return
}

func DragFloat4V(label string, vP [4]float32, v_speed float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v [4]float32, r bool) {
	_ = `r = ImGui::DragFloat4(label, vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func DragInt2(label string, vP [2]int) (v [2]int, r bool) {
	_ = `r = ImGui::DragInt2(label, vP);
v = vP;`
	return
}

func DragInt2V(label string, vP [2]int, v_speed float32, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v [2]int, r bool) {
	_ = `r = ImGui::DragInt2(label, vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func DragInt3(label string, vP [3]int) (v [2]int, r bool) {
	_ = `r = ImGui::DragInt3(label, vP);
v = vP;`
	return
}

func DragInt3V(label string, vP [3]int, v_speed float32, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v [4]int, r bool) {
	_ = `r = ImGui::DragInt3(label, vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func DragInt4(label string, vP [4]int) (v [4]int, r bool) {
	_ = `r = ImGui::DragInt4(label, vP);
v = vP;`
	return
}

func DragInt4V(label string, vP [4]int, v_speed float32, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v [4]int, r bool) {
	_ = `r = ImGui::DragInt4(label, vP, v_speed, v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderFloat(label string, vP float32, v_min float32, v_max float32) (v float32, r bool) {
	_ = `r = ImGui::SliderFloat(label, &vP, v_min, v_max);
v = vP;`
	return
}

func SliderFloatV(label string, vP float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v float32, r bool) {
	_ = `r = ImGui::SliderFloat(label, &vP,  v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderFloat2(label string, vP [2]float32, v_min float32, v_max float32) (v [2]float32, r bool) {
	_ = `r = ImGui::SliderFloat2(label, vP, v_min, v_max);
v = vP;`
	return
}

func SliderFloat2V(label string, vP [2]float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v [2]float32, r bool) {
	_ = `r = ImGui::SliderFloat2(label, vP,  v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderFloat3(label string, vP [3]float32, v_min float32, v_max float32) (v [2]float32, r bool) {
	_ = `r = ImGui::SliderFloat3(label, vP, v_min, v_max);
v = vP;`
	return
}

func SliderFloat3V(label string, vP [3]float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v [4]float32, r bool) {
	_ = `r = ImGui::SliderFloat3(label, vP,  v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderFloat4(label string, vP [4]float32, v_min float32, v_max float32) (v [4]float32, r bool) {
	_ = `r = ImGui::SliderFloat4(label, vP, v_min, v_max);
v = vP;`
	return
}

func SliderFloat4V(label string, vP [4]float32, v_min float32, v_max float32, format string, flags ImGuiSliderFlags) (v [4]float32, r bool) {
	_ = `r = ImGui::SliderFloat4(label, vP,  v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderInt2(label string, vP [2]int, v_min int, v_max int) (v [2]int, r bool) {
	_ = `r = ImGui::SliderInt2(label, vP, v_min, v_max);
v = vP;`
	return
}

func SliderInt2V(label string, vP [2]int, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v [2]int, r bool) {
	_ = `r = ImGui::SliderInt2(label, vP,  v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderInt3(label string, vP [3]int, v_min int, v_max int) (v [2]int, r bool) {
	_ = `r = ImGui::SliderInt3(label, vP, v_min, v_max);
v = vP;`
	return
}

func SliderInt3V(label string, vP [3]int, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v [4]int, r bool) {
	_ = `r = ImGui::SliderInt3(label, vP,  v_min, v_max, format, flags);
v = vP;`
	return
}

func SliderInt4(label string, vP [4]int, v_min int, v_max int) (v [4]int, r bool) {
	_ = `r = ImGui::SliderInt4(label, vP, v_min, v_max);
v = vP;`
	return
}

func SliderInt4V(label string, vP [4]int, v_min int, v_max int, format string, flags ImGuiSliderFlags) (v [4]int, r bool) {
	_ = `r = ImGui::SliderInt4(label, vP,  v_min, v_max, format, flags);
v = vP;`
	return
}
