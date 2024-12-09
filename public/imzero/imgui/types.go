package imgui

import "github.com/stergiotis/boxer/public/logical"

type ImGuiID uint32

type ImVec2 complex64

type ImVec4 [4]float32

type ImWchar16 uint16

type ImWchar32 rune

type ImWchar ImWchar32

type ImRect [4]float32

type Size_t uint64

type ImTextureID uintptr

type ImFontPtr uintptr

type ImGuiStyleForeignPtr uintptr

type ImDrawListPtr uintptr

type ImHexEditorPtr uintptr

type Tristate = logical.Tristate

type ImU8 uint8

// ImGuiStyle You may modify the ImGui::GetStyle() main instance during initialization and before NewFrame().
// During the frame, use ImGui::PushStyleVar(ImGuiStyleVar_XXXX)/PopStyleVar() to alter the main style values,
// and ImGui::PushStyleColor(ImGuiCol_XXX)/PopStyleColor() for colors.
type ImGuiStyle struct {
	Alpha                      float32  // Global alpha applies to everything in Dear ImGui.
	DisabledAlpha              float32  // Additional alpha multiplier applied by BeginDisabled(). Multiply over current value of Alpha.
	WindowPadding              ImVec2   // Padding within a window.
	WindowRounding             float32  // Radius of window corners rounding. Set to 0.0f to have rectangular windows. Large values tend to lead to variety of artifacts and are not recommended.
	WindowBorderSize           float32  // Thickness of border around windows. Generally set to 0.0f or 1.0f. (Other values are not well tested and more CPU/GPU costly).
	WindowMinSize              ImVec2   // Minimum window size. This is a global setting. If you want to constrain individual windows, use SetNextWindowSizeConstraints().
	WindowTitleAlign           ImVec2   // Alignment for title bar text. Defaults to (0.0f,0.5f) for left-aligned,vertically centered.
	WindowMenuButtonPosition   ImGuiDir // Side of the collapsing/docking button in the title bar (None/Left/Right). Defaults to ImGuiDir_Left.
	ChildRounding              float32  // Radius of child window corners rounding. Set to 0.0f to have rectangular windows.
	ChildBorderSize            float32  // Thickness of border around child windows. Generally set to 0.0f or 1.0f. (Other values are not well tested and more CPU/GPU costly).
	PopupRounding              float32  // Radius of popup window corners rounding. (Note that tooltip windows use WindowRounding)
	PopupBorderSize            float32  // Thickness of border around popup/tooltip windows. Generally set to 0.0f or 1.0f. (Other values are not well tested and more CPU/GPU costly).
	FramePadding               ImVec2   // Padding within a framed rectangle (used by most widgets).
	FrameRounding              float32  // Radius of frame corners rounding. Set to 0.0f to have rectangular frame (used by most widgets).
	FrameBorderSize            float32  // Thickness of border around frames. Generally set to 0.0f or 1.0f. (Other values are not well tested and more CPU/GPU costly).
	ItemSpacing                ImVec2   // Horizontal and vertical spacing between widgets/lines.
	ItemInnerSpacing           ImVec2   // Horizontal and vertical spacing between within elements of a composed widget (e.g. a slider and its label).
	CellPadding                ImVec2   // Padding within a table cell. CellPadding.y may be altered between different rows.
	TouchExtraPadding          ImVec2   // Expand reactive bounding box for touch-based system where touch position is not accurate enough. Unfortunately we don't sort widgets so priority on overlap will always be given to the first widget. So don't grow this too much!
	IndentSpacing              float32  // Horizontal indentation when e.g. entering a tree node. Generally == (FontSize + FramePadding.x*2).
	ColumnsMinSpacing          float32  // Minimum horizontal spacing between two columns. Preferably > (FramePadding.x + 1).
	ScrollbarSize              float32  // Width of the vertical scrollbar, Height of the horizontal scrollbar.
	ScrollbarRounding          float32  // Radius of grab corners for scrollbar.
	GrabMinSize                float32  // Minimum width/height of a grab box for slider/scrollbar.
	GrabRounding               float32  // Radius of grabs corners rounding. Set to 0.0f to have rectangular slider grabs.
	LogSliderDeadzone          float32  // The size in pixels of the dead-zone around zero on logarithmic sliders that cross zero.
	TabRounding                float32  // Radius of upper corners of a tab. Set to 0.0f to have rectangular tabs.
	TabBorderSize              float32  // Thickness of border around tabs.
	TabMinWidthForCloseButton  float32  // Minimum width for close button to appear on an unselected tab when hovered. Set to 0.0f to always show when hovering, set to FLT_MAX to never show close button unless selected.
	TabBarBorderSize           float32  // Thickness of tab-bar separator, which takes on the tab active color to denote focus.
	ColorButtonPosition        ImGuiDir // Side of the color button in the ColorEdit4 widget (left/right). Defaults to ImGuiDir_Right.
	ButtonTextAlign            ImVec2   // Alignment of button text when button is larger than text. Defaults to (0.5f, 0.5f) (centered).
	SelectableTextAlign        ImVec2   // Alignment of selectable text. Defaults to (0.0f, 0.0f) (top-left aligned). It's generally important to keep this left-aligned if you want to lay multiple items on a same line.
	SeparatorTextBorderSize    float32  // Thickkness of border in SeparatorText()
	SeparatorTextAlign         ImVec2   // Alignment of text within the separator. Defaults to (0.0f, 0.5f) (left aligned, center).
	SeparatorTextPadding       ImVec2   // Horizontal offset of text from each edge of the separator + spacing on other axis. Generally small values. .y is recommended to be == FramePadding.y.
	DisplayWindowPadding       ImVec2   // Window position are clamped to be visible within the display area or monitors by at least this amount. Only applies to regular windows.
	DisplaySafeAreaPadding     ImVec2   // If you cannot see the edges of your screen (e.g. on a TV) increase the safe area padding. Apply to popups/tooltips as well regular windows. NB: Prefer configuring your TV sets correctly!
	DockingSeparatorSize       float32  // Thickness of resizing border between docked windows
	MouseCursorScale           float32  // Scale software rendered mouse cursor (when io.MouseDrawCursor is enabled). We apply per-monitor DPI scaling over this scale. May be removed later.
	AntiAliasedLines           bool     // Enable anti-aliased lines/borders. Disable if you are really tight on CPU/GPU. Latched at the beginning of the frame (copied to ImDrawList).
	AntiAliasedLinesUseTex     bool     // Enable anti-aliased lines/borders using textures where possible. Require backend to render with bilinear filtering (NOT point/nearest filtering). Latched at the beginning of the frame (copied to ImDrawList).
	AntiAliasedFill            bool     // Enable anti-aliased edges around filled shapes (rounded rectangles, circles, etc.). Disable if you are really tight on CPU/GPU. Latched at the beginning of the frame (copied to ImDrawList).
	CurveTessellationTol       float32  // Tessellation tolerance when using PathBezierCurveTo() without a specific number of segments. Decrease for highly tessellated curves (higher quality, more polygons), increase to reduce quality.
	CircleTessellationMaxError float32  // Maximum error (in pixels) allowed when using AddCircle()/AddCircleFilled() or drawing rounded corner rectangles with no explicit segment count specified. Decrease for higher quality but more geometry.
	Colors                     []ImVec4

	// Behaviors
	// (It is possible to modify those fields mid-frame if specific behavior need it, unlike e.g. configuration fields in ImGuiIO)
	HoverStationaryDelay      float32           // Delay for IsItemHovered(ImGuiHoveredFlags_Stationary). Time required to consider mouse stationary.
	HoverDelayShort           float32           // Delay for IsItemHovered(ImGuiHoveredFlags_DelayShort). Usually used along with HoverStationaryDelay.
	HoverDelayNormal          float32           // Delay for IsItemHovered(ImGuiHoveredFlags_DelayNormal). "
	HoverFlagsForTooltipMouse ImGuiHoveredFlags // Default flags when using IsItemHovered(ImGuiHoveredFlags_ForTooltip) or BeginItemTooltip()/SetItemTooltip() while using mouse.
	HoverFlagsForTooltipNav   ImGuiHoveredFlags // Default flags when using IsItemHovered(ImGuiHoveredFlags_ForTooltip) or BeginItemTooltip()/SetItemTooltip() while using keyboard/gamepad.
}
