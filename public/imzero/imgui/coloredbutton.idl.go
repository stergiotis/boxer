//go:build fffi_idl_code

package imgui

func ColoredButtonV(label string, size ImVec2, text_color uint32, bg_color1 uint32, bg_color2 uint32) (r bool) {
	_ = `r = ImGui::ColoredButtonV1(label,size,text_color,bg_color1,bg_color2);`
	return
}
