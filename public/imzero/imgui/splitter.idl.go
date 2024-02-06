//go:build fffi_idl_code

package imgui

func Splitter(split_vertically bool, thickness float32, size1P float32, size2P float32, min_size1 float32, min_size2 float32) (r bool, size1 float32, size2 float32) {
	_ = `r = ImGui::Splitter(split_vertically, thickness, &size1P, &size2P, min_size1, min_size2);
         size1 = size1P;
         size2 = size2P;`
	return
}
func SplitterV(split_vertically bool, thickness float32, size1P float32, size2P float32, min_size1 float32, min_size2 float32, splitter_long_axis float32) (r bool, size1 float32, size2 float32) {
	_ = `r = ImGui::Splitter(split_vertically, thickness, &size1P, &size2P, min_size1, min_size2, splitter_long_axis);
         size1 = size1P;
         size2 = size2P;`
	return
}
