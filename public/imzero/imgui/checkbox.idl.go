//go:build fffi_idl_code

package imgui

func Checkbox(label string, state Tristate) (checked Tristate, clicked bool) {
	_ = `
       bool v = state > 0;
         if(state == 0) {
              ImGui::PushItemFlag(ImGuiItemFlags_MixedValue, true);
              clicked = ImGui::Checkbox(label, &v);
              ImGui::PopItemFlag();
              if(clicked) {
                      checked = 1;
              }
         } else {
              clicked = ImGui::Checkbox(label, &v);
              checked = v ? 1 : -1;
         }
`
	return
}
