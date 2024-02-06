//go:build fffi_idl_code

package imgui

func Checkbox(label string, state Tristate) (checked Tristate, clicked bool) {
	_ = `bool *p = nullptr;
         bool v = state > 0;
         if(state != 0) {
            p = &v;
         }
         clicked = ImGui::Checkbox(label, p);
         if(p != nullptr) {
            checked = (*p > 0);
         } else {
            checked = 0;
         }`
	return
}
