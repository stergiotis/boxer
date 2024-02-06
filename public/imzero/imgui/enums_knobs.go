//go:build fffi_idl_code

package imgui

type ImGuiKnobFlags int

const (
	ImGuiKnobFlags_NoTitle        ImGuiKnobFlags = 1 << 0
	ImGuiKnobFlags_NoInput        ImGuiKnobFlags = 1 << 1
	ImGuiKnobFlags_ValueTooltip   ImGuiKnobFlags = 1 << 2
	ImGuiKnobFlags_DragHorizontal ImGuiKnobFlags = 1 << 3
)

type ImGuiKnobVariant int

const (
	ImGuiKnobVariant_Tick      ImGuiKnobVariant = 1 << 0
	ImGuiKnobVariant_Dot       ImGuiKnobVariant = 1 << 1
	ImGuiKnobVariant_Wiper     ImGuiKnobVariant = 1 << 2
	ImGuiKnobVariant_WiperOnly ImGuiKnobVariant = 1 << 3
	ImGuiKnobVariant_WiperDot  ImGuiKnobVariant = 1 << 4
	ImGuiKnobVariant_Stepped   ImGuiKnobVariant = 1 << 5
	ImGuiKnobVariant_Space     ImGuiKnobVariant = 1 << 6
)
