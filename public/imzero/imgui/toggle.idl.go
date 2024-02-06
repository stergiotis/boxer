//go:build fffi_idl_code

package imgui

// ImGuiToggleFlags: A set of flags that adjust behavior and display for ImGui::Toggle().
type ImGuiTogglerFlags int

const (
	ImGuiToggleFlags_None     ImGuiTogglerFlags = 0
	ImGuiToggleFlags_Animated ImGuiTogglerFlags = 1 << 0 // The toggle's knob should be animated.
	// Bits 1-2 reserved.
	ImGuiToggleFlags_BorderedFrame ImGuiTogglerFlags = 1 << 3 // The toggle should have a border drawn on the frame.
	ImGuiToggleFlags_BorderedKnob  ImGuiTogglerFlags = 1 << 4 // The toggle should have a border drawn on the knob.
	ImGuiToggleFlags_ShadowedFrame ImGuiTogglerFlags = 1 << 5 // The toggle should have a shadow drawn under the frame.
	ImGuiToggleFlags_ShadowedKnob  ImGuiTogglerFlags = 1 << 6 // The toggle should have a shadow drawn under the knob.
	// Bit 7 reserved.
	ImGuiToggleFlags_A11y     ImGuiTogglerFlags = 1 << 8                                                         // The toggle should draw on and off glyphs to help indicate its state.
	ImGuiToggleFlags_Bordered ImGuiTogglerFlags = ImGuiToggleFlags_BorderedFrame | ImGuiToggleFlags_BorderedKnob // Shorthand for bordered frame and knob.
	ImGuiToggleFlags_Shadowed ImGuiTogglerFlags = ImGuiToggleFlags_ShadowedFrame | ImGuiToggleFlags_ShadowedKnob // Shorthand for shadowed frame and knob.
	ImGuiToggleFlags_Default  ImGuiTogglerFlags = ImGuiToggleFlags_None                                          // The default flags used when no ImGuiToggleFlags_ are specified.
)

// Toggles behave similarly to ImGui::Checkbox()
// Sometimes called a toggle switch, see also: https://en.wikipedia.org/wiki/Toggle_switch_(widget)
// They represent two mutually exclusive states, with an optional animation on the UI when toggled.
func Toggle(label string, val bool) (valR bool, changed bool) {
	_ = `changed = ImGui::Toggle(label,&val);
valR = val`
	return
}

// ToggleV:
// Toggles behave similarly to ImGui::Checkbox()
// Sometimes called a toggle switch, see also: https://en.wikipedia.org/wiki/Toggle_switch_(widget)
// They represent two mutually exclusive states, with an optional animation on the UI when toggled.
// - flags: Values from the ImGuiToggleFlags_ enumeration to set toggle modes.
// - animation_duration: Animation duration. Amount of time in seconds the toggle should animate. (0,...] default: 1.0f (Overloads with this parameter imply ImGuiToggleFlags_Animated)
// - frame_rounding: A scalar that controls how rounded the toggle frame is. 0 is square, 1 is round. (0, 1) default 1.0f
// - knob_rounding: A scalar that controls how rounded the toggle knob is. 0 is square, 1 is round. (0, 1) default 1.0f
// - size: A width and height to draw the toggle at. Defaults to `ImGui::GetFrameHeight()` and that height * Phi for the width.
func ToggleV(label string, val bool, flags ImGuiTogglerFlags, animationDuration float32, frameRounding float32, knobRounding float32, size ImVec2) (valR bool, changed bool) {
	_ = `changed = ImGui::Toggle(label,&val,flags,animationDuration,frameRounding,knobRounding,size);
valR = val`
	return
}
