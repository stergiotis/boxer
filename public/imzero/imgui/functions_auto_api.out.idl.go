//go:build fffi_idl_code

package imgui

// DestroyContext NULL = destroy current context
func DestroyContext() {
	_ = `ImGui::DestroyContext()`
}

// NewFrame start a new Dear ImGui frame, you can submit any command from this point until Render()/EndFrame().
func NewFrame() {
	_ = `ImGui::NewFrame()`
}

// EndFrame ends the Dear ImGui frame. automatically called by Render(). If you don't need to render data (skipping rendering) you may call EndFrame() without Render()... but you'll have wasted CPU already! If you don't need to render, better to not create any windows and not call NewFrame() at all!
func EndFrame() {
	_ = `ImGui::EndFrame()`
}

// Render ends the Dear ImGui frame, finalize the draw data. You can then get call GetDrawData().
func Render() {
	_ = `ImGui::Render()`
}

// ShowDemoWindow create Demo window. demonstrate most ImGui features. call this to learn about the library! try to make it always available in your application!
func ShowDemoWindow() {
	_ = `ImGui::ShowDemoWindow()`
}

// ShowMetricsWindow create Metrics/Debugger window. display Dear ImGui internals: windows, draw commands, various internal state, etc.
func ShowMetricsWindow() {
	_ = `ImGui::ShowMetricsWindow()`
}

// ShowDebugLogWindow create Debug Log window. display a simplified log of important dear imgui events.
func ShowDebugLogWindow() {
	_ = `ImGui::ShowDebugLogWindow()`
}

// ShowIDStackToolWindow create Stack Tool window. hover items with mouse to query information about the source of their unique ID.
func ShowIDStackToolWindow() {
	_ = `ImGui::ShowIDStackToolWindow()`
}

// ShowAboutWindow create About window. display Dear ImGui version, credits and build/system information.
func ShowAboutWindow() {
	_ = `ImGui::ShowAboutWindow()`
}

// ShowStyleEditor add style editor block (not a window). you can pass in a reference ImGuiStyle structure to compare to, revert to and save to (else it uses the default style)
func ShowStyleEditor() {
	_ = `ImGui::ShowStyleEditor()`
}

// ShowStyleSelector add style selector block (not a window), essentially a combo listing the default styles.
func ShowStyleSelector(label string) (r bool) {
	_ = `auto r = ImGui::ShowStyleSelector(label)`
	return
}

// ShowFontSelector add font selector block (not a window), essentially a combo listing the loaded fonts.
func ShowFontSelector(label string) {
	_ = `ImGui::ShowFontSelector(label)`
}

// ShowUserGuide add basic help/info block (not a window): how to manipulate ImGui as an end-user (mouse/keyboard controls).
func ShowUserGuide() {
	_ = `ImGui::ShowUserGuide()`
}

// GetVersion get the compiled version string e.g. "1.80 WIP" (essentially the value for IMGUI_VERSION from the compiled version of imgui.cpp)
func GetVersion() (r string) {
	_ = `auto r = ImGui::GetVersion()`
	return
}

// StyleColorsDark new, recommended style (default)
func StyleColorsDark() {
	_ = `ImGui::StyleColorsDark()`
}

// StyleColorsLight best used with borders and a custom, thicker font
func StyleColorsLight() {
	_ = `ImGui::StyleColorsLight()`
}

// StyleColorsClassic classic imgui style
func StyleColorsClassic() {
	_ = `ImGui::StyleColorsClassic()`
}
func Begin(name string) (r bool) {
	_ = `auto r = ImGui::Begin(name)`
	return
}
func BeginV(name string, flags ImGuiWindowFlags /* = 0*/) (r bool, p_open bool) {
	_ = `auto r = ImGui::Begin(name, &p_open, flags)`
	return
}
func End() {
	_ = `ImGui::End()`
}
func BeginChild(str_id string) (r bool) {
	_ = `auto r = ImGui::BeginChild(str_id)`
	return
}
func BeginChildV(str_id string, size ImVec2 /* = ImVec2(0, 0)*/, child_flags ImGuiChildFlags /* = 0*/, window_flags ImGuiWindowFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::BeginChild(str_id, size, child_flags, window_flags)`
	return
}
func BeginChildID(id ImGuiID) (r bool) {
	_ = `auto r = ImGui::BeginChild(id)`
	return
}
func BeginChildVID(id ImGuiID, size ImVec2 /* = ImVec2(0, 0)*/, child_flags ImGuiChildFlags /* = 0*/, window_flags ImGuiWindowFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::BeginChild(id, size, child_flags, window_flags)`
	return
}
func EndChild() {
	_ = `ImGui::EndChild()`
}
func IsWindowAppearing() (r bool) {
	_ = `auto r = ImGui::IsWindowAppearing()`
	return
}
func IsWindowCollapsed() (r bool) {
	_ = `auto r = ImGui::IsWindowCollapsed()`
	return
}

// IsWindowFocused is current window focused? or its root/child, depending on flags. see flags for options.
func IsWindowFocused() (r bool) {
	_ = `auto r = ImGui::IsWindowFocused()`
	return
}

// IsWindowFocusedV is current window focused? or its root/child, depending on flags. see flags for options.
// * flags ImGuiFocusedFlags = 0
func IsWindowFocusedV(flags ImGuiFocusedFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsWindowFocused(flags)`
	return
}

// IsWindowHovered is current window hovered and hoverable (e.g. not blocked by a popup/modal)? See ImGuiHoveredFlags_ for options. IMPORTANT: If you are trying to check whether your mouse should be dispatched to Dear ImGui or to your underlying app, you should not use this function! Use the 'io.WantCaptureMouse' boolean for that! Refer to FAQ entry "How can I tell whether to dispatch mouse/keyboard to Dear ImGui or my application?" for details.
func IsWindowHovered() (r bool) {
	_ = `auto r = ImGui::IsWindowHovered()`
	return
}

// IsWindowHoveredV is current window hovered and hoverable (e.g. not blocked by a popup/modal)? See ImGuiHoveredFlags_ for options. IMPORTANT: If you are trying to check whether your mouse should be dispatched to Dear ImGui or to your underlying app, you should not use this function! Use the 'io.WantCaptureMouse' boolean for that! Refer to FAQ entry "How can I tell whether to dispatch mouse/keyboard to Dear ImGui or my application?" for details.
// * flags ImGuiHoveredFlags = 0
func IsWindowHoveredV(flags ImGuiHoveredFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsWindowHovered(flags)`
	return
}

// GetWindowDrawList get draw list associated to the current window, to append your own drawing primitives
func GetWindowDrawList() (r ImDrawListPtr) {
	_ = `auto r = ImGui::GetWindowDrawList()`
	return
}

// GetWindowDpiScale get DPI scale currently associated to the current window's viewport.
func GetWindowDpiScale() (r float32) {
	_ = `auto r = ImGui::GetWindowDpiScale()`
	return
}

// GetWindowPos get current window position in screen space (note: it is unlikely you need to use this. Consider using current layout pos instead, GetCursorScreenPos())
func GetWindowPos() (r ImVec2) {
	_ = `auto r = ImGui::GetWindowPos()`
	return
}

// GetWindowSize get current window size (note: it is unlikely you need to use this. Consider using GetCursorScreenPos() and e.g. GetContentRegionAvail() instead)
func GetWindowSize() (r ImVec2) {
	_ = `auto r = ImGui::GetWindowSize()`
	return
}

// GetWindowWidth get current window width (shortcut for GetWindowSize().x)
func GetWindowWidth() (r float32) {
	_ = `auto r = ImGui::GetWindowWidth()`
	return
}

// GetWindowHeight get current window height (shortcut for GetWindowSize().y)
func GetWindowHeight() (r float32) {
	_ = `auto r = ImGui::GetWindowHeight()`
	return
}

// SetNextWindowPos set next window position. call before Begin(). use pivot=(0.5f,0.5f) to center on given point, etc.
func SetNextWindowPos(pos ImVec2) {
	_ = `ImGui::SetNextWindowPos(pos)`
}

// SetNextWindowPosV set next window position. call before Begin(). use pivot=(0.5f,0.5f) to center on given point, etc.
// * cond ImGuiCond = 0
// * pivot const ImVec2 & = ImVec2(0, 0)
func SetNextWindowPosV(pos ImVec2, cond ImGuiCond /* = 0*/, pivot ImVec2 /* = ImVec2(0, 0)*/) {
	_ = `ImGui::SetNextWindowPos(pos, cond, pivot)`
}

// SetNextWindowSize set next window size. set axis to 0.0f to force an auto-fit on this axis. call before Begin()
func SetNextWindowSize(size ImVec2) {
	_ = `ImGui::SetNextWindowSize(size)`
}

// SetNextWindowSizeV set next window size. set axis to 0.0f to force an auto-fit on this axis. call before Begin()
// * cond ImGuiCond = 0
func SetNextWindowSizeV(size ImVec2, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetNextWindowSize(size, cond)`
}

// SetNextWindowSizeConstraints set next window size limits. use 0.0f or FLT_MAX if you don't want limits. Use -1 for both min and max of same axis to preserve current size (which itself is a constraint). Use callback to apply non-trivial programmatic constraints.
func SetNextWindowSizeConstraints(size_min ImVec2, size_max ImVec2) {
	_ = `ImGui::SetNextWindowSizeConstraints(size_min, size_max)`
}

// SetNextWindowContentSize set next window content size (~ scrollable client area, which enforce the range of scrollbars). Not including window decorations (title bar, menu bar, etc.) nor WindowPadding. set an axis to 0.0f to leave it automatic. call before Begin()
func SetNextWindowContentSize(size ImVec2) {
	_ = `ImGui::SetNextWindowContentSize(size)`
}

// SetNextWindowCollapsed set next window collapsed state. call before Begin()
func SetNextWindowCollapsed(collapsed bool) {
	_ = `ImGui::SetNextWindowCollapsed(collapsed)`
}

// SetNextWindowCollapsedV set next window collapsed state. call before Begin()
// * cond ImGuiCond = 0
func SetNextWindowCollapsedV(collapsed bool, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetNextWindowCollapsed(collapsed, cond)`
}

// SetNextWindowFocus set next window to be focused / top-most. call before Begin()
func SetNextWindowFocus() {
	_ = `ImGui::SetNextWindowFocus()`
}

// SetNextWindowScroll set next window scrolling value (use < 0.0f to not affect a given axis).
func SetNextWindowScroll(scroll ImVec2) {
	_ = `ImGui::SetNextWindowScroll(scroll)`
}

// SetNextWindowBgAlpha set next window background color alpha. helper to easily override the Alpha component of ImGuiCol_WindowBg/ChildBg/PopupBg. you may also use ImGuiWindowFlags_NoBackground.
func SetNextWindowBgAlpha(alpha float32) {
	_ = `ImGui::SetNextWindowBgAlpha(alpha)`
}

// SetNextWindowViewport set next window viewport
func SetNextWindowViewport(viewport_id ImGuiID) {
	_ = `ImGui::SetNextWindowViewport(viewport_id)`
}

// SetWindowPos set named window position.
func SetWindowPos(name string, pos ImVec2) {
	_ = `ImGui::SetWindowPos(name, pos)`
}

// SetWindowPosV set named window position.
// * cond ImGuiCond = 0
func SetWindowPosV(name string, pos ImVec2, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetWindowPos(name, pos, cond)`
}

// SetWindowSize set named window size. set axis to 0.0f to force an auto-fit on this axis.
func SetWindowSize(name string, size ImVec2) {
	_ = `ImGui::SetWindowSize(name, size)`
}

// SetWindowSizeV set named window size. set axis to 0.0f to force an auto-fit on this axis.
// * cond ImGuiCond = 0
func SetWindowSizeV(name string, size ImVec2, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetWindowSize(name, size, cond)`
}

// SetWindowCollapsed set named window collapsed state
func SetWindowCollapsed(name string, collapsed bool) {
	_ = `ImGui::SetWindowCollapsed(name, collapsed)`
}

// SetWindowCollapsedV set named window collapsed state
// * cond ImGuiCond = 0
func SetWindowCollapsedV(name string, collapsed bool, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetWindowCollapsed(name, collapsed, cond)`
}

// SetWindowFocus set named window to be focused / top-most. use NULL to remove focus.
func SetWindowFocus(name string) {
	_ = `ImGui::SetWindowFocus(name)`
}

// GetContentRegionAvail == GetContentRegionMax() - GetCursorPos()
func GetContentRegionAvail() (r ImVec2) {
	_ = `auto r = ImGui::GetContentRegionAvail()`
	return
}

// GetContentRegionMax current content boundaries (typically window boundaries including scrolling, or current column boundaries), in windows coordinates
func GetContentRegionMax() (r ImVec2) {
	_ = `auto r = ImGui::GetContentRegionMax()`
	return
}

// GetWindowContentRegionMin content boundaries min for the full window (roughly (0,0)-Scroll), in window coordinates
func GetWindowContentRegionMin() (r ImVec2) {
	_ = `auto r = ImGui::GetWindowContentRegionMin()`
	return
}

// GetWindowContentRegionMax content boundaries max for the full window (roughly (0,0)+Size-Scroll) where Size can be overridden with SetNextWindowContentSize(), in window coordinates
func GetWindowContentRegionMax() (r ImVec2) {
	_ = `auto r = ImGui::GetWindowContentRegionMax()`
	return
}

// GetScrollX get scrolling amount [0 .. GetScrollMaxX()]
func GetScrollX() (r float32) {
	_ = `auto r = ImGui::GetScrollX()`
	return
}

// GetScrollY get scrolling amount [0 .. GetScrollMaxY()]
func GetScrollY() (r float32) {
	_ = `auto r = ImGui::GetScrollY()`
	return
}

// SetScrollX set scrolling amount [0 .. GetScrollMaxX()]
func SetScrollX(scroll_x float32) {
	_ = `ImGui::SetScrollX(scroll_x)`
}

// SetScrollY set scrolling amount [0 .. GetScrollMaxY()]
func SetScrollY(scroll_y float32) {
	_ = `ImGui::SetScrollY(scroll_y)`
}

// GetScrollMaxX get maximum scrolling amount ~~ ContentSize.x - WindowSize.x - DecorationsSize.x
func GetScrollMaxX() (r float32) {
	_ = `auto r = ImGui::GetScrollMaxX()`
	return
}

// GetScrollMaxY get maximum scrolling amount ~~ ContentSize.y - WindowSize.y - DecorationsSize.y
func GetScrollMaxY() (r float32) {
	_ = `auto r = ImGui::GetScrollMaxY()`
	return
}

// SetScrollHereX adjust scrolling amount to make current cursor position visible. center_x_ratio=0.0: left, 0.5: center, 1.0: right. When using to make a "default/current item" visible, consider using SetItemDefaultFocus() instead.
func SetScrollHereX() {
	_ = `ImGui::SetScrollHereX()`
}

// SetScrollHereXV adjust scrolling amount to make current cursor position visible. center_x_ratio=0.0: left, 0.5: center, 1.0: right. When using to make a "default/current item" visible, consider using SetItemDefaultFocus() instead.
// * center_x_ratio float = 0.5f
func SetScrollHereXV(center_x_ratio float32 /* = 0.5f*/) {
	_ = `ImGui::SetScrollHereX(center_x_ratio)`
}

// SetScrollHereY adjust scrolling amount to make current cursor position visible. center_y_ratio=0.0: top, 0.5: center, 1.0: bottom. When using to make a "default/current item" visible, consider using SetItemDefaultFocus() instead.
func SetScrollHereY() {
	_ = `ImGui::SetScrollHereY()`
}

// SetScrollHereYV adjust scrolling amount to make current cursor position visible. center_y_ratio=0.0: top, 0.5: center, 1.0: bottom. When using to make a "default/current item" visible, consider using SetItemDefaultFocus() instead.
// * center_y_ratio float = 0.5f
func SetScrollHereYV(center_y_ratio float32 /* = 0.5f*/) {
	_ = `ImGui::SetScrollHereY(center_y_ratio)`
}

// SetScrollFromPosX adjust scrolling amount to make given position visible. Generally GetCursorStartPos() + offset to compute a valid position.
func SetScrollFromPosX(local_x float32) {
	_ = `ImGui::SetScrollFromPosX(local_x)`
}

// SetScrollFromPosXV adjust scrolling amount to make given position visible. Generally GetCursorStartPos() + offset to compute a valid position.
// * center_x_ratio float = 0.5f
func SetScrollFromPosXV(local_x float32, center_x_ratio float32 /* = 0.5f*/) {
	_ = `ImGui::SetScrollFromPosX(local_x, center_x_ratio)`
}

// SetScrollFromPosY adjust scrolling amount to make given position visible. Generally GetCursorStartPos() + offset to compute a valid position.
func SetScrollFromPosY(local_y float32) {
	_ = `ImGui::SetScrollFromPosY(local_y)`
}

// SetScrollFromPosYV adjust scrolling amount to make given position visible. Generally GetCursorStartPos() + offset to compute a valid position.
// * center_y_ratio float = 0.5f
func SetScrollFromPosYV(local_y float32, center_y_ratio float32 /* = 0.5f*/) {
	_ = `ImGui::SetScrollFromPosY(local_y, center_y_ratio)`
}
func PopFont() {
	_ = `ImGui::PopFont()`
}

// PushStyleColor modify a style color. always use this if you modify the style after NewFrame().
func PushStyleColor(idx ImGuiCol, col uint32) {
	_ = `ImGui::PushStyleColor(idx, col)`
}
func PushStyleColorImVec4(idx ImGuiCol, col ImVec4) {
	_ = `ImGui::PushStyleColor(idx, col)`
}
func PopStyleColor() {
	_ = `ImGui::PopStyleColor()`
}
func PopStyleColorV(count int /* = 1*/) {
	_ = `ImGui::PopStyleColor(count)`
}

// PushStyleVar modify a style float variable. always use this if you modify the style after NewFrame().
func PushStyleVar(idx ImGuiStyleVar, val float32) {
	_ = `ImGui::PushStyleVar(idx, val)`
}

// PushStyleVar modify a style ImVec2 variable. always use this if you modify the style after NewFrame().
func PushStyleVarImVec2(idx ImGuiStyleVar, val ImVec2) {
	_ = `ImGui::PushStyleVar(idx, val)`
}
func PopStyleVar() {
	_ = `ImGui::PopStyleVar()`
}
func PopStyleVarV(count int /* = 1*/) {
	_ = `ImGui::PopStyleVar(count)`
}

// PushTabStop == tab stop enable. Allow focusing using TAB/Shift-TAB, enabled by default but you can disable it for certain widgets
func PushTabStop(tab_stop bool) {
	_ = `ImGui::PushTabStop(tab_stop)`
}
func PopTabStop() {
	_ = `ImGui::PopTabStop()`
}

// PushButtonRepeat in 'repeat' mode, Button*() functions return repeated true in a typematic manner (using io.KeyRepeatDelay/io.KeyRepeatRate setting). Note that you can call IsItemActive() after any Button() to tell if the button is held in the current frame.
func PushButtonRepeat(repeat bool) {
	_ = `ImGui::PushButtonRepeat(repeat)`
}
func PopButtonRepeat() {
	_ = `ImGui::PopButtonRepeat()`
}

// PushItemWidth push width of items for common large "item+label" widgets. >0.0f: width in pixels, <0.0f align xx pixels to the right of window (so -FLT_MIN always align width to the right side).
func PushItemWidth(item_width float32) {
	_ = `ImGui::PushItemWidth(item_width)`
}
func PopItemWidth() {
	_ = `ImGui::PopItemWidth()`
}

// SetNextItemWidth set width of the _next_ common large "item+label" widget. >0.0f: width in pixels, <0.0f align xx pixels to the right of window (so -FLT_MIN always align width to the right side)
func SetNextItemWidth(item_width float32) {
	_ = `ImGui::SetNextItemWidth(item_width)`
}

// CalcItemWidth width of item given pushed settings and current cursor position. NOT necessarily the width of last item unlike most 'Item' functions.
func CalcItemWidth() (r float32) {
	_ = `auto r = ImGui::CalcItemWidth()`
	return
}

// PushTextWrapPos push word-wrapping position for Text*() commands. < 0.0f: no wrapping; 0.0f: wrap to end of window (or column); > 0.0f: wrap at 'wrap_pos_x' position in window local space
func PushTextWrapPos() {
	_ = `ImGui::PushTextWrapPos()`
}

// PushTextWrapPosV push word-wrapping position for Text*() commands. < 0.0f: no wrapping; 0.0f: wrap to end of window (or column); > 0.0f: wrap at 'wrap_pos_x' position in window local space
// * wrap_local_pos_x float = 0.0f
func PushTextWrapPosV(wrap_local_pos_x float32 /* = 0.0f*/) {
	_ = `ImGui::PushTextWrapPos(wrap_local_pos_x)`
}
func PopTextWrapPos() {
	_ = `ImGui::PopTextWrapPos()`
}

// GetFontSize get current font size (= height in pixels) of current font with current scale applied
func GetFontSize() (r float32) {
	_ = `auto r = ImGui::GetFontSize()`
	return
}

// GetFontTexUvWhitePixel get UV coordinate for a while pixel, useful to draw custom shapes via the ImDrawList API
func GetFontTexUvWhitePixel() (r ImVec2) {
	_ = `auto r = ImGui::GetFontTexUvWhitePixel()`
	return
}

// GetColorU32 retrieve given style color with style alpha applied and optional extra alpha multiplier, packed as a 32-bit value suitable for ImDrawList
func GetColorU32ImGuiCol(idx ImGuiCol) (r uint32) {
	_ = `auto r = ImGui::GetColorU32(idx)`
	return
}

// GetColorU32V retrieve given style color with style alpha applied and optional extra alpha multiplier, packed as a 32-bit value suitable for ImDrawList
// * alpha_mul float = 1.0f
func GetColorU32V(idx ImGuiCol, alpha_mul float32 /* = 1.0f*/) (r uint32) {
	_ = `auto r = ImGui::GetColorU32(idx, alpha_mul)`
	return
}

// GetColorU32 retrieve given color with style alpha applied, packed as a 32-bit value suitable for ImDrawList
func GetColorU32ImVec4(col ImVec4) (r uint32) {
	_ = `auto r = ImGui::GetColorU32(col)`
	return
}

// GetColorU32 retrieve given color with style alpha applied, packed as a 32-bit value suitable for ImDrawList
func GetColorU32(col uint32) (r uint32) {
	_ = `auto r = ImGui::GetColorU32(col)`
	return
}

// GetStyleColorVec4 retrieve style color as stored in ImGuiStyle structure. use to feed back into PushStyleColor(), otherwise use GetColorU32() to get style color with style alpha baked in.
func GetStyleColorVec4(idx ImGuiCol) (r ImVec4) {
	_ = `auto r = ImGui::GetStyleColorVec4(idx)`
	return
}

// GetCursorScreenPos cursor position in absolute coordinates (prefer using this, also more useful to work with ImDrawList API).
func GetCursorScreenPos() (r ImVec2) {
	_ = `auto r = ImGui::GetCursorScreenPos()`
	return
}

// SetCursorScreenPos cursor position in absolute coordinates
func SetCursorScreenPos(pos ImVec2) {
	_ = `ImGui::SetCursorScreenPos(pos)`
}

// GetCursorPos [window-local] cursor position in window coordinates (relative to window position)
func GetCursorPos() (r ImVec2) {
	_ = `auto r = ImGui::GetCursorPos()`
	return
}

// GetCursorPosX [window-local] "
func GetCursorPosX() (r float32) {
	_ = `auto r = ImGui::GetCursorPosX()`
	return
}

// GetCursorPosY [window-local] "
func GetCursorPosY() (r float32) {
	_ = `auto r = ImGui::GetCursorPosY()`
	return
}

// SetCursorPos [window-local] "
func SetCursorPos(local_pos ImVec2) {
	_ = `ImGui::SetCursorPos(local_pos)`
}

// SetCursorPosX [window-local] "
func SetCursorPosX(local_x float32) {
	_ = `ImGui::SetCursorPosX(local_x)`
}

// SetCursorPosY [window-local] "
func SetCursorPosY(local_y float32) {
	_ = `ImGui::SetCursorPosY(local_y)`
}

// GetCursorStartPos [window-local] initial cursor position, in window coordinates
func GetCursorStartPos() (r ImVec2) {
	_ = `auto r = ImGui::GetCursorStartPos()`
	return
}

// Separator separator, generally horizontal. inside a menu bar or in horizontal layout mode, this becomes a vertical separator.
func Separator() {
	_ = `ImGui::Separator()`
}

// SameLine call between widgets or groups to layout them horizontally. X position given in window coordinates.
func SameLine() {
	_ = `ImGui::SameLine()`
}

// SameLineV call between widgets or groups to layout them horizontally. X position given in window coordinates.
// * offset_from_start_x float = 0.0f
// * spacing float = -1.0f
func SameLineV(offset_from_start_x float32 /* = 0.0f*/, spacing float32 /* = -1.0f*/) {
	_ = `ImGui::SameLine(offset_from_start_x, spacing)`
}

// NewLine undo a SameLine() or force a new line when in a horizontal-layout context.
func NewLine() {
	_ = `ImGui::NewLine()`
}

// Spacing add vertical spacing.
func Spacing() {
	_ = `ImGui::Spacing()`
}

// Dummy add a dummy item of given size. unlike InvisibleButton(), Dummy() won't take the mouse click or be navigable into.
func Dummy(size ImVec2) {
	_ = `ImGui::Dummy(size)`
}

// Indent move content position toward the right, by indent_w, or style.IndentSpacing if indent_w <= 0
func Indent() {
	_ = `ImGui::Indent()`
}

// IndentV move content position toward the right, by indent_w, or style.IndentSpacing if indent_w <= 0
// * indent_w float = 0.0f
func IndentV(indent_w float32 /* = 0.0f*/) {
	_ = `ImGui::Indent(indent_w)`
}

// Unindent move content position back to the left, by indent_w, or style.IndentSpacing if indent_w <= 0
func Unindent() {
	_ = `ImGui::Unindent()`
}

// UnindentV move content position back to the left, by indent_w, or style.IndentSpacing if indent_w <= 0
// * indent_w float = 0.0f
func UnindentV(indent_w float32 /* = 0.0f*/) {
	_ = `ImGui::Unindent(indent_w)`
}

// BeginGroup lock horizontal starting position
func BeginGroup() {
	_ = `ImGui::BeginGroup()`
}

// EndGroup unlock horizontal starting position + capture the whole group bounding box into one "item" (so you can use IsItemHovered() or layout primitives such as SameLine() on whole group, etc.)
func EndGroup() {
	_ = `ImGui::EndGroup()`
}

// AlignTextToFramePadding vertically align upcoming text baseline to FramePadding.y so that it will align properly to regularly framed items (call if you have text on a line before a framed item)
func AlignTextToFramePadding() {
	_ = `ImGui::AlignTextToFramePadding()`
}

// GetTextLineHeight ~ FontSize
func GetTextLineHeight() (r float32) {
	_ = `auto r = ImGui::GetTextLineHeight()`
	return
}

// GetTextLineHeightWithSpacing ~ FontSize + style.ItemSpacing.y (distance in pixels between 2 consecutive lines of text)
func GetTextLineHeightWithSpacing() (r float32) {
	_ = `auto r = ImGui::GetTextLineHeightWithSpacing()`
	return
}

// GetFrameHeight ~ FontSize + style.FramePadding.y * 2
func GetFrameHeight() (r float32) {
	_ = `auto r = ImGui::GetFrameHeight()`
	return
}

// GetFrameHeightWithSpacing ~ FontSize + style.FramePadding.y * 2 + style.ItemSpacing.y (distance in pixels between 2 consecutive lines of framed widgets)
func GetFrameHeightWithSpacing() (r float32) {
	_ = `auto r = ImGui::GetFrameHeightWithSpacing()`
	return
}

// PushID push string into the ID stack (will hash string).
func PushID(str_id string) {
	_ = `ImGui::PushID(str_id)`
}

// PushID push integer into the ID stack (will hash integer).
func PushIDInt(int_id int) {
	_ = `ImGui::PushID(int_id)`
}

// PopID pop from the ID stack.
func PopID() {
	_ = `ImGui::PopID()`
}

// GetID calculate unique ID (hash of whole ID stack + given parameter). e.g. if you want to query into ImGuiStorage yourself
func GetID(str_id string) (r ImGuiID) {
	_ = `auto r = ImGui::GetID(str_id)`
	return
}

// SeparatorText currently: formatted text with an horizontal line
func SeparatorText(label string) {
	_ = `ImGui::SeparatorText(label)`
}

// Button button
func Button(label string) (r bool) {
	_ = `auto r = ImGui::Button(label)`
	return
}

// ButtonV button
// * size const ImVec2 & = ImVec2(0, 0)
func ButtonV(label string, size ImVec2 /* = ImVec2(0, 0)*/) (r bool) {
	_ = `auto r = ImGui::Button(label, size)`
	return
}

// SmallButton button with (FramePadding.y == 0) to easily embed within text
func SmallButton(label string) (r bool) {
	_ = `auto r = ImGui::SmallButton(label)`
	return
}

// InvisibleButton flexible button behavior without the visuals, frequently useful to build custom behaviors using the public api (along with IsItemActive, IsItemHovered, etc.)
func InvisibleButton(str_id string, size ImVec2) (r bool) {
	_ = `auto r = ImGui::InvisibleButton(str_id, size)`
	return
}

// InvisibleButtonV flexible button behavior without the visuals, frequently useful to build custom behaviors using the public api (along with IsItemActive, IsItemHovered, etc.)
// * flags ImGuiButtonFlags = 0
func InvisibleButtonV(str_id string, size ImVec2, flags ImGuiButtonFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::InvisibleButton(str_id, size, flags)`
	return
}

// ArrowButton square button with an arrow shape
func ArrowButton(str_id string, dir ImGuiDir) (r bool) {
	_ = `auto r = ImGui::ArrowButton(str_id, dir)`
	return
}

// RadioButton use with e.g. if (RadioButton("one", my_value==1)) { my_value = 1; }
func RadioButton(label string, active bool) (r bool) {
	_ = `auto r = ImGui::RadioButton(label, active)`
	return
}
func ProgressBar(fraction float32) {
	_ = `ImGui::ProgressBar(fraction)`
}
func ProgressBarV(fraction float32, size_arg ImVec2 /* = ImVec2(-FLT_MIN, 0)*/, overlay string /* = NULL*/) {
	_ = `ImGui::ProgressBar(fraction, size_arg, overlay)`
}

// Bullet draw a small circle + keep the cursor on the same line. advance cursor x position by GetTreeNodeToLabelSpacing(), same distance that TreeNode() uses
func Bullet() {
	_ = `ImGui::Bullet()`
}
func Image(user_texture_id ImTextureID, image_size ImVec2) {
	_ = `ImGui::Image(ImTextureID(user_texture_id), image_size)`
}
func ImageV(user_texture_id ImTextureID, image_size ImVec2, uv0 ImVec2 /* = ImVec2(0, 0)*/, uv1 ImVec2 /* = ImVec2(1, 1)*/, tint_col ImVec4 /* = ImVec4(1, 1, 1, 1)*/, border_col ImVec4 /* = ImVec4(0, 0, 0, 0)*/) {
	_ = `ImGui::Image(ImTextureID(user_texture_id), image_size, uv0, uv1, tint_col, border_col)`
}
func ImageButton(str_id string, user_texture_id ImTextureID, image_size ImVec2) (r bool) {
	_ = `auto r = ImGui::ImageButton(str_id, ImTextureID(user_texture_id), image_size)`
	return
}
func ImageButtonV(str_id string, user_texture_id ImTextureID, image_size ImVec2, uv0 ImVec2 /* = ImVec2(0, 0)*/, uv1 ImVec2 /* = ImVec2(1, 1)*/, bg_col ImVec4 /* = ImVec4(0, 0, 0, 0)*/, tint_col ImVec4 /* = ImVec4(1, 1, 1, 1)*/) (r bool) {
	_ = `auto r = ImGui::ImageButton(str_id, ImTextureID(user_texture_id), image_size, uv0, uv1, bg_col, tint_col)`
	return
}
func BeginCombo(label string, preview_value string) (r bool) {
	_ = `auto r = ImGui::BeginCombo(label, preview_value)`
	return
}
func BeginComboV(label string, preview_value string, flags ImGuiComboFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::BeginCombo(label, preview_value, flags)`
	return
}

// EndCombo only call EndCombo() if BeginCombo() returns true!
func EndCombo() {
	_ = `ImGui::EndCombo()`
}

// ColorButton display a color square/button, hover for details, return true when pressed.
func ColorButton(desc_id string, col ImVec4) (r bool) {
	_ = `auto r = ImGui::ColorButton(desc_id, col)`
	return
}

// ColorButtonV display a color square/button, hover for details, return true when pressed.
// * flags ImGuiColorEditFlags = 0
// * size const ImVec2 & = ImVec2(0, 0)
func ColorButtonV(desc_id string, col ImVec4, flags ImGuiColorEditFlags /* = 0*/, size ImVec2 /* = ImVec2(0, 0)*/) (r bool) {
	_ = `auto r = ImGui::ColorButton(desc_id, col, flags, size)`
	return
}

// SetColorEditOptions initialize current options (generally on application startup) if you want to select a default format, picker type, etc. User will be able to change many settings, unless you pass the _NoOptions flag to your calls.
func SetColorEditOptions(flags ImGuiColorEditFlags) {
	_ = `ImGui::SetColorEditOptions(flags)`
}
func TreeNode(label string) (r bool) {
	_ = `auto r = ImGui::TreeNode(label)`
	return
}
func TreeNodeEx(label string) (r bool) {
	_ = `auto r = ImGui::TreeNodeEx(label)`
	return
}
func TreeNodeExV(label string, flags ImGuiTreeNodeFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::TreeNodeEx(label, flags)`
	return
}

// TreePush ~ Indent()+PushID(). Already called by TreeNode() when returning true, but you can call TreePush/TreePop yourself if desired.
func TreePush(str_id string) {
	_ = `ImGui::TreePush(str_id)`
}

// TreePop ~ Unindent()+PopID()
func TreePop() {
	_ = `ImGui::TreePop()`
}

// GetTreeNodeToLabelSpacing horizontal distance preceding label when using TreeNode*() or Bullet() == (g.FontSize + style.FramePadding.x*2) for a regular unframed TreeNode
func GetTreeNodeToLabelSpacing() (r float32) {
	_ = `auto r = ImGui::GetTreeNodeToLabelSpacing()`
	return
}

// CollapsingHeader if returning 'true' the header is open. doesn't indent nor push on ID stack. user doesn't have to call TreePop().
func CollapsingHeader(label string) (r bool) {
	_ = `auto r = ImGui::CollapsingHeader(label)`
	return
}

// CollapsingHeaderV if returning 'true' the header is open. doesn't indent nor push on ID stack. user doesn't have to call TreePop().
// * flags ImGuiTreeNodeFlags = 0
func CollapsingHeaderV(label string, flags ImGuiTreeNodeFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::CollapsingHeader(label, flags)`
	return
}

// SetNextItemOpen set next TreeNode/CollapsingHeader open state.
func SetNextItemOpen(is_open bool) {
	_ = `ImGui::SetNextItemOpen(is_open)`
}

// SetNextItemOpenV set next TreeNode/CollapsingHeader open state.
// * cond ImGuiCond = 0
func SetNextItemOpenV(is_open bool, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetNextItemOpen(is_open, cond)`
}

// Selectable "bool selected" carry the selection state (read-only). Selectable() is clicked is returns true so you can modify your selection state. size.x==0.0: use remaining width, size.x>0.0: specify width. size.y==0.0: use label height, size.y>0.0: specify height
func Selectable(label string) (r bool) {
	_ = `auto r = ImGui::Selectable(label)`
	return
}

// SelectableV "bool selected" carry the selection state (read-only). Selectable() is clicked is returns true so you can modify your selection state. size.x==0.0: use remaining width, size.x>0.0: specify width. size.y==0.0: use label height, size.y>0.0: specify height
// * selected bool = false
// * flags ImGuiSelectableFlags = 0
// * size const ImVec2 & = ImVec2(0, 0)
func SelectableV(label string, selected bool /* = false*/, flags ImGuiSelectableFlags /* = 0*/, size ImVec2 /* = ImVec2(0, 0)*/) (r bool) {
	_ = `auto r = ImGui::Selectable(label, selected, flags, size)`
	return
}

// BeginListBox open a framed scrolling region
func BeginListBox(label string) (r bool) {
	_ = `auto r = ImGui::BeginListBox(label)`
	return
}

// BeginListBoxV open a framed scrolling region
// * size const ImVec2 & = ImVec2(0, 0)
func BeginListBoxV(label string, size ImVec2 /* = ImVec2(0, 0)*/) (r bool) {
	_ = `auto r = ImGui::BeginListBox(label, size)`
	return
}

// EndListBox only call EndListBox() if BeginListBox() returned true!
func EndListBox() {
	_ = `ImGui::EndListBox()`
}

// BeginMenuBar append to menu-bar of current window (requires ImGuiWindowFlags_MenuBar flag set on parent window).
func BeginMenuBar() (r bool) {
	_ = `auto r = ImGui::BeginMenuBar()`
	return
}

// EndMenuBar only call EndMenuBar() if BeginMenuBar() returns true!
func EndMenuBar() {
	_ = `ImGui::EndMenuBar()`
}

// BeginMainMenuBar create and append to a full screen menu-bar.
func BeginMainMenuBar() (r bool) {
	_ = `auto r = ImGui::BeginMainMenuBar()`
	return
}

// EndMainMenuBar only call EndMainMenuBar() if BeginMainMenuBar() returns true!
func EndMainMenuBar() {
	_ = `ImGui::EndMainMenuBar()`
}

// BeginMenu create a sub-menu entry. only call EndMenu() if this returns true!
func BeginMenu(label string) (r bool) {
	_ = `auto r = ImGui::BeginMenu(label)`
	return
}

// BeginMenuV create a sub-menu entry. only call EndMenu() if this returns true!
// * enabled bool = true
func BeginMenuV(label string, enabled bool /* = true*/) (r bool) {
	_ = `auto r = ImGui::BeginMenu(label, enabled)`
	return
}

// EndMenu only call EndMenu() if BeginMenu() returns true!
func EndMenu() {
	_ = `ImGui::EndMenu()`
}

// MenuItem return true when activated.
func MenuItem(label string) (r bool) {
	_ = `auto r = ImGui::MenuItem(label)`
	return
}

// MenuItemV return true when activated.
// * shortcut const char * = NULL
// * selected bool = false
// * enabled bool = true
func MenuItemV(label string, shortcut string /* = NULL*/, selected bool /* = false*/, enabled bool /* = true*/) (r bool) {
	_ = `auto r = ImGui::MenuItem(label, shortcut, selected, enabled)`
	return
}

// BeginTooltip begin/append a tooltip window.
func BeginTooltip() (r bool) {
	_ = `auto r = ImGui::BeginTooltip()`
	return
}

// EndTooltip only call EndTooltip() if BeginTooltip()/BeginItemTooltip() returns true!
func EndTooltip() {
	_ = `ImGui::EndTooltip()`
}

// BeginItemTooltip begin/append a tooltip window if preceding item was hovered.
func BeginItemTooltip() (r bool) {
	_ = `auto r = ImGui::BeginItemTooltip()`
	return
}

// BeginPopup return true if the popup is open, and you can start outputting to it.
func BeginPopup(str_id string) (r bool) {
	_ = `auto r = ImGui::BeginPopup(str_id)`
	return
}

// BeginPopupV return true if the popup is open, and you can start outputting to it.
// * flags ImGuiWindowFlags = 0
func BeginPopupV(str_id string, flags ImGuiWindowFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::BeginPopup(str_id, flags)`
	return
}

// BeginPopupModal return true if the modal is open, and you can start outputting to it.
func BeginPopupModal(name string) (r bool) {
	_ = `auto r = ImGui::BeginPopupModal(name)`
	return
}

// BeginPopupModalV return true if the modal is open, and you can start outputting to it.
// * p_open bool * = NULL
// * flags ImGuiWindowFlags = 0
func BeginPopupModalV(name string, flags ImGuiWindowFlags /* = 0*/) (r bool, p_open bool) {
	_ = `auto r = ImGui::BeginPopupModal(name, &p_open, flags)`
	return
}

// EndPopup only call EndPopup() if BeginPopupXXX() returns true!
func EndPopup() {
	_ = `ImGui::EndPopup()`
}

// OpenPopup call to mark popup as open (don't call every frame!).
func OpenPopup(str_id string) {
	_ = `ImGui::OpenPopup(str_id)`
}

// OpenPopupV call to mark popup as open (don't call every frame!).
// * popup_flags ImGuiPopupFlags = 0
func OpenPopupV(str_id string, popup_flags ImGuiPopupFlags /* = 0*/) {
	_ = `ImGui::OpenPopup(str_id, popup_flags)`
}

// OpenPopup id overload to facilitate calling from nested stacks
func OpenPopupID(id ImGuiID) {
	_ = `ImGui::OpenPopup(id)`
}

// OpenPopupV id overload to facilitate calling from nested stacks
// * popup_flags ImGuiPopupFlags = 0
func OpenPopupVID(id ImGuiID, popup_flags ImGuiPopupFlags /* = 0*/) {
	_ = `ImGui::OpenPopup(id, popup_flags)`
}

// OpenPopupOnItemClick helper to open popup when clicked on last item. Default to ImGuiPopupFlags_MouseButtonRight == 1. (note: actually triggers on the mouse _released_ event to be consistent with popup behaviors)
func OpenPopupOnItemClick() {
	_ = `ImGui::OpenPopupOnItemClick()`
}

// OpenPopupOnItemClickV helper to open popup when clicked on last item. Default to ImGuiPopupFlags_MouseButtonRight == 1. (note: actually triggers on the mouse _released_ event to be consistent with popup behaviors)
// * str_id const char * = NULL
// * popup_flags ImGuiPopupFlags = 1
func OpenPopupOnItemClickV(str_id string /* = NULL*/, popup_flags ImGuiPopupFlags /* = 1*/) {
	_ = `ImGui::OpenPopupOnItemClick(str_id, popup_flags)`
}

// CloseCurrentPopup manually close the popup we have begin-ed into.
func CloseCurrentPopup() {
	_ = `ImGui::CloseCurrentPopup()`
}

// BeginPopupContextItem open+begin popup when clicked on last item. Use str_id==NULL to associate the popup to previous item. If you want to use that on a non-interactive item such as Text() you need to pass in an explicit ID here. read comments in .cpp!
func BeginPopupContextItem() (r bool) {
	_ = `auto r = ImGui::BeginPopupContextItem()`
	return
}

// BeginPopupContextItemV open+begin popup when clicked on last item. Use str_id==NULL to associate the popup to previous item. If you want to use that on a non-interactive item such as Text() you need to pass in an explicit ID here. read comments in .cpp!
// * str_id const char * = NULL
// * popup_flags ImGuiPopupFlags = 1
func BeginPopupContextItemV(str_id string /* = NULL*/, popup_flags ImGuiPopupFlags /* = 1*/) (r bool) {
	_ = `auto r = ImGui::BeginPopupContextItem(str_id, popup_flags)`
	return
}

// BeginPopupContextWindow open+begin popup when clicked on current window.
func BeginPopupContextWindow() (r bool) {
	_ = `auto r = ImGui::BeginPopupContextWindow()`
	return
}

// BeginPopupContextWindowV open+begin popup when clicked on current window.
// * str_id const char * = NULL
// * popup_flags ImGuiPopupFlags = 1
func BeginPopupContextWindowV(str_id string /* = NULL*/, popup_flags ImGuiPopupFlags /* = 1*/) (r bool) {
	_ = `auto r = ImGui::BeginPopupContextWindow(str_id, popup_flags)`
	return
}

// BeginPopupContextVoid open+begin popup when clicked in void (where there are no windows).
func BeginPopupContextVoid() (r bool) {
	_ = `auto r = ImGui::BeginPopupContextVoid()`
	return
}

// BeginPopupContextVoidV open+begin popup when clicked in void (where there are no windows).
// * str_id const char * = NULL
// * popup_flags ImGuiPopupFlags = 1
func BeginPopupContextVoidV(str_id string /* = NULL*/, popup_flags ImGuiPopupFlags /* = 1*/) (r bool) {
	_ = `auto r = ImGui::BeginPopupContextVoid(str_id, popup_flags)`
	return
}

// IsPopupOpen return true if the popup is open.
func IsPopupOpen(str_id string) (r bool) {
	_ = `auto r = ImGui::IsPopupOpen(str_id)`
	return
}

// IsPopupOpenV return true if the popup is open.
// * flags ImGuiPopupFlags = 0
func IsPopupOpenV(str_id string, flags ImGuiPopupFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsPopupOpen(str_id, flags)`
	return
}
func BeginTable(str_id string, column int) (r bool) {
	_ = `auto r = ImGui::BeginTable(str_id, column)`
	return
}
func BeginTableV(str_id string, column int, flags ImGuiTableFlags /* = 0*/, outer_size ImVec2 /* = ImVec2(0.0f, 0.0f)*/, inner_width float32 /* = 0.0f*/) (r bool) {
	_ = `auto r = ImGui::BeginTable(str_id, column, flags, outer_size, inner_width)`
	return
}

// EndTable only call EndTable() if BeginTable() returns true!
func EndTable() {
	_ = `ImGui::EndTable()`
}

// TableNextRow append into the first cell of a new row.
func TableNextRow() {
	_ = `ImGui::TableNextRow()`
}

// TableNextRowV append into the first cell of a new row.
// * row_flags ImGuiTableRowFlags = 0
// * min_row_height float = 0.0f
func TableNextRowV(row_flags ImGuiTableRowFlags /* = 0*/, min_row_height float32 /* = 0.0f*/) {
	_ = `ImGui::TableNextRow(row_flags, min_row_height)`
}

// TableNextColumn append into the next column (or first column of next row if currently in last column). Return true when column is visible.
func TableNextColumn() (r bool) {
	_ = `auto r = ImGui::TableNextColumn()`
	return
}

// TableSetColumnIndex append into the specified column. Return true when column is visible.
func TableSetColumnIndex(column_n int) (r bool) {
	_ = `auto r = ImGui::TableSetColumnIndex(column_n)`
	return
}
func TableSetupColumn(label string) {
	_ = `ImGui::TableSetupColumn(label)`
}
func TableSetupColumnV(label string, flags ImGuiTableColumnFlags /* = 0*/, init_width_or_weight float32 /* = 0.0f*/, user_id ImGuiID /* = 0*/) {
	_ = `ImGui::TableSetupColumn(label, flags, init_width_or_weight, user_id)`
}

// TableSetupScrollFreeze lock columns/rows so they stay visible when scrolled.
func TableSetupScrollFreeze(cols int, rows int) {
	_ = `ImGui::TableSetupScrollFreeze(cols, rows)`
}

// TableHeader submit one header cell manually (rarely used)
func TableHeader(label string) {
	_ = `ImGui::TableHeader(label)`
}

// TableHeadersRow submit a row with headers cells based on data provided to TableSetupColumn() + submit context menu
func TableHeadersRow() {
	_ = `ImGui::TableHeadersRow()`
}

// TableAngledHeadersRow submit a row with angled headers for every column with the ImGuiTableColumnFlags_AngledHeader flag. MUST BE FIRST ROW.
func TableAngledHeadersRow() {
	_ = `ImGui::TableAngledHeadersRow()`
}

// TableGetColumnCount return number of columns (value passed to BeginTable)
func TableGetColumnCount() (r int) {
	_ = `auto r = ImGui::TableGetColumnCount()`
	return
}

// TableGetColumnIndex return current column index.
func TableGetColumnIndex() (r int) {
	_ = `auto r = ImGui::TableGetColumnIndex()`
	return
}

// TableGetRowIndex return current row index.
func TableGetRowIndex() (r int) {
	_ = `auto r = ImGui::TableGetRowIndex()`
	return
}

// TableGetColumnName return "" if column didn't have a name declared by TableSetupColumn(). Pass -1 to use current column.
func TableGetColumnName() (r string) {
	_ = `auto r = ImGui::TableGetColumnName()`
	return
}

// TableGetColumnNameV return "" if column didn't have a name declared by TableSetupColumn(). Pass -1 to use current column.
// * column_n int = -1
func TableGetColumnNameV(column_n int /* = -1*/) (r string) {
	_ = `auto r = ImGui::TableGetColumnName(column_n)`
	return
}

// TableGetColumnFlags return column flags so you can query their Enabled/Visible/Sorted/Hovered status flags. Pass -1 to use current column.
func TableGetColumnFlags() (r ImGuiTableColumnFlags) {
	_ = `auto r = ImGui::TableGetColumnFlags()`
	return
}

// TableGetColumnFlagsV return column flags so you can query their Enabled/Visible/Sorted/Hovered status flags. Pass -1 to use current column.
// * column_n int = -1
func TableGetColumnFlagsV(column_n int /* = -1*/) (r ImGuiTableColumnFlags) {
	_ = `auto r = ImGui::TableGetColumnFlags(column_n)`
	return
}

// TableSetColumnEnabled change user accessible enabled/disabled state of a column. Set to false to hide the column. User can use the context menu to change this themselves (right-click in headers, or right-click in columns body with ImGuiTableFlags_ContextMenuInBody)
func TableSetColumnEnabled(column_n int, v bool) {
	_ = `ImGui::TableSetColumnEnabled(column_n, v)`
}

// TableSetBgColor change the color of a cell, row, or column. See ImGuiTableBgTarget_ flags for details.
func TableSetBgColor(target ImGuiTableBgTarget, color uint32) {
	_ = `ImGui::TableSetBgColor(target, color)`
}

// TableSetBgColorV change the color of a cell, row, or column. See ImGuiTableBgTarget_ flags for details.
// * column_n int = -1
func TableSetBgColorV(target ImGuiTableBgTarget, color uint32, column_n int /* = -1*/) {
	_ = `ImGui::TableSetBgColor(target, color, column_n)`
}
func Columns() {
	_ = `ImGui::Columns()`
}
func ColumnsV(count int /* = 1*/, id string /* = NULL*/, border bool /* = true*/) {
	_ = `ImGui::Columns(count, id, border)`
}

// NextColumn next column, defaults to current row or next row if the current row is finished
func NextColumn() {
	_ = `ImGui::NextColumn()`
}

// GetColumnIndex get current column index
func GetColumnIndex() (r int) {
	_ = `auto r = ImGui::GetColumnIndex()`
	return
}

// GetColumnWidth get column width (in pixels). pass -1 to use current column
func GetColumnWidth() (r float32) {
	_ = `auto r = ImGui::GetColumnWidth()`
	return
}

// GetColumnWidthV get column width (in pixels). pass -1 to use current column
// * column_index int = -1
func GetColumnWidthV(column_index int /* = -1*/) (r float32) {
	_ = `auto r = ImGui::GetColumnWidth(column_index)`
	return
}

// SetColumnWidth set column width (in pixels). pass -1 to use current column
func SetColumnWidth(column_index int, width float32) {
	_ = `ImGui::SetColumnWidth(column_index, width)`
}

// GetColumnOffset get position of column line (in pixels, from the left side of the contents region). pass -1 to use current column, otherwise 0..GetColumnsCount() inclusive. column 0 is typically 0.0f
func GetColumnOffset() (r float32) {
	_ = `auto r = ImGui::GetColumnOffset()`
	return
}

// GetColumnOffsetV get position of column line (in pixels, from the left side of the contents region). pass -1 to use current column, otherwise 0..GetColumnsCount() inclusive. column 0 is typically 0.0f
// * column_index int = -1
func GetColumnOffsetV(column_index int /* = -1*/) (r float32) {
	_ = `auto r = ImGui::GetColumnOffset(column_index)`
	return
}

// SetColumnOffset set position of column line (in pixels, from the left side of the contents region). pass -1 to use current column
func SetColumnOffset(column_index int, offset_x float32) {
	_ = `ImGui::SetColumnOffset(column_index, offset_x)`
}
func GetColumnsCount() (r int) {
	_ = `auto r = ImGui::GetColumnsCount()`
	return
}

// BeginTabBar create and append into a TabBar
func BeginTabBar(str_id string) (r bool) {
	_ = `auto r = ImGui::BeginTabBar(str_id)`
	return
}

// BeginTabBarV create and append into a TabBar
// * flags ImGuiTabBarFlags = 0
func BeginTabBarV(str_id string, flags ImGuiTabBarFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::BeginTabBar(str_id, flags)`
	return
}

// EndTabBar only call EndTabBar() if BeginTabBar() returns true!
func EndTabBar() {
	_ = `ImGui::EndTabBar()`
}

// BeginTabItem create a Tab. Returns true if the Tab is selected.
func BeginTabItem(label string) (r bool) {
	_ = `auto r = ImGui::BeginTabItem(label)`
	return
}

// BeginTabItemV create a Tab. Returns true if the Tab is selected.
// * p_open bool * = NULL
// * flags ImGuiTabItemFlags = 0
func BeginTabItemV(label string, flags ImGuiTabItemFlags /* = 0*/) (r bool, p_open bool) {
	_ = `auto r = ImGui::BeginTabItem(label, &p_open, flags)`
	return
}

// EndTabItem only call EndTabItem() if BeginTabItem() returns true!
func EndTabItem() {
	_ = `ImGui::EndTabItem()`
}

// TabItemButton create a Tab behaving like a button. return true when clicked. cannot be selected in the tab bar.
func TabItemButton(label string) (r bool) {
	_ = `auto r = ImGui::TabItemButton(label)`
	return
}

// TabItemButtonV create a Tab behaving like a button. return true when clicked. cannot be selected in the tab bar.
// * flags ImGuiTabItemFlags = 0
func TabItemButtonV(label string, flags ImGuiTabItemFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::TabItemButton(label, flags)`
	return
}

// SetTabItemClosed notify TabBar or Docking system of a closed tab/window ahead (useful to reduce visual flicker on reorderable tab bars). For tab-bar: call after BeginTabBar() and before Tab submissions. Otherwise call with a window name.
func SetTabItemClosed(tab_or_docked_window_label string) {
	_ = `ImGui::SetTabItemClosed(tab_or_docked_window_label)`
}
func DockSpace(id ImGuiID) (r ImGuiID) {
	_ = `auto r = ImGui::DockSpace(id)`
	return
}
func DockSpaceOverViewport() (r ImGuiID) {
	_ = `auto r = ImGui::DockSpaceOverViewport()`
	return
}

// SetNextWindowDockID set next window dock id
func SetNextWindowDockID(dock_id ImGuiID) {
	_ = `ImGui::SetNextWindowDockID(dock_id)`
}

// SetNextWindowDockIDV set next window dock id
// * cond ImGuiCond = 0
func SetNextWindowDockIDV(dock_id ImGuiID, cond ImGuiCond /* = 0*/) {
	_ = `ImGui::SetNextWindowDockID(dock_id, cond)`
}
func GetWindowDockID() (r ImGuiID) {
	_ = `auto r = ImGui::GetWindowDockID()`
	return
}

// IsWindowDocked is current window docked into another window?
func IsWindowDocked() (r bool) {
	_ = `auto r = ImGui::IsWindowDocked()`
	return
}

// LogToTTY start logging to tty (stdout)
func LogToTTY() {
	_ = `ImGui::LogToTTY()`
}

// LogToTTYV start logging to tty (stdout)
// * auto_open_depth int = -1
func LogToTTYV(auto_open_depth int /* = -1*/) {
	_ = `ImGui::LogToTTY(auto_open_depth)`
}

// LogToFile start logging to file
func LogToFile() {
	_ = `ImGui::LogToFile()`
}

// LogToFileV start logging to file
// * auto_open_depth int = -1
// * filename const char * = NULL
func LogToFileV(auto_open_depth int /* = -1*/, filename string /* = NULL*/) {
	_ = `ImGui::LogToFile(auto_open_depth, filename)`
}

// LogToClipboard start logging to OS clipboard
func LogToClipboard() {
	_ = `ImGui::LogToClipboard()`
}

// LogToClipboardV start logging to OS clipboard
// * auto_open_depth int = -1
func LogToClipboardV(auto_open_depth int /* = -1*/) {
	_ = `ImGui::LogToClipboard(auto_open_depth)`
}

// LogFinish stop logging (close file, etc.)
func LogFinish() {
	_ = `ImGui::LogFinish()`
}

// LogButtons helper to display buttons for logging to tty/file/clipboard
func LogButtons() {
	_ = `ImGui::LogButtons()`
}

// BeginDragDropSource call after submitting an item which may be dragged. when this return true, you can call SetDragDropPayload() + EndDragDropSource()
func BeginDragDropSource() (r bool) {
	_ = `auto r = ImGui::BeginDragDropSource()`
	return
}

// BeginDragDropSourceV call after submitting an item which may be dragged. when this return true, you can call SetDragDropPayload() + EndDragDropSource()
// * flags ImGuiDragDropFlags = 0
func BeginDragDropSourceV(flags ImGuiDragDropFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::BeginDragDropSource(flags)`
	return
}

// EndDragDropSource only call EndDragDropSource() if BeginDragDropSource() returns true!
func EndDragDropSource() {
	_ = `ImGui::EndDragDropSource()`
}

// BeginDragDropTarget call after submitting an item that may receive a payload. If this returns true, you can call AcceptDragDropPayload() + EndDragDropTarget()
func BeginDragDropTarget() (r bool) {
	_ = `auto r = ImGui::BeginDragDropTarget()`
	return
}

// EndDragDropTarget only call EndDragDropTarget() if BeginDragDropTarget() returns true!
func EndDragDropTarget() {
	_ = `ImGui::EndDragDropTarget()`
}
func BeginDisabled() {
	_ = `ImGui::BeginDisabled()`
}
func BeginDisabledV(disabled bool /* = true*/) {
	_ = `ImGui::BeginDisabled(disabled)`
}
func EndDisabled() {
	_ = `ImGui::EndDisabled()`
}
func PushClipRect(clip_rect_min ImVec2, clip_rect_max ImVec2, intersect_with_current_clip_rect bool) {
	_ = `ImGui::PushClipRect(clip_rect_min, clip_rect_max, intersect_with_current_clip_rect)`
}
func PopClipRect() {
	_ = `ImGui::PopClipRect()`
}

// SetItemDefaultFocus make last item the default focused item of a window.
func SetItemDefaultFocus() {
	_ = `ImGui::SetItemDefaultFocus()`
}

// SetKeyboardFocusHere focus keyboard on the next widget. Use positive 'offset' to access sub components of a multiple component widget. Use -1 to access previous widget.
func SetKeyboardFocusHere() {
	_ = `ImGui::SetKeyboardFocusHere()`
}

// SetKeyboardFocusHereV focus keyboard on the next widget. Use positive 'offset' to access sub components of a multiple component widget. Use -1 to access previous widget.
// * offset int = 0
func SetKeyboardFocusHereV(offset int /* = 0*/) {
	_ = `ImGui::SetKeyboardFocusHere(offset)`
}

// SetNextItemAllowOverlap allow next item to be overlapped by a subsequent item. Useful with invisible buttons, selectable, treenode covering an area where subsequent items may need to be added. Note that both Selectable() and TreeNode() have dedicated flags doing this.
func SetNextItemAllowOverlap() {
	_ = `ImGui::SetNextItemAllowOverlap()`
}

// IsItemHovered is the last item hovered? (and usable, aka not blocked by a popup, etc.). See ImGuiHoveredFlags for more options.
func IsItemHovered() (r bool) {
	_ = `auto r = ImGui::IsItemHovered()`
	return
}

// IsItemHoveredV is the last item hovered? (and usable, aka not blocked by a popup, etc.). See ImGuiHoveredFlags for more options.
// * flags ImGuiHoveredFlags = 0
func IsItemHoveredV(flags ImGuiHoveredFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsItemHovered(flags)`
	return
}

// IsItemActive is the last item active? (e.g. button being held, text field being edited. This will continuously return true while holding mouse button on an item. Items that don't interact will always return false)
func IsItemActive() (r bool) {
	_ = `auto r = ImGui::IsItemActive()`
	return
}

// IsItemFocused is the last item focused for keyboard/gamepad navigation?
func IsItemFocused() (r bool) {
	_ = `auto r = ImGui::IsItemFocused()`
	return
}

// IsItemClicked is the last item hovered and mouse clicked on? (**) == IsMouseClicked(mouse_button) && IsItemHovered()Important. (**) this is NOT equivalent to the behavior of e.g. Button(). Read comments in function definition.
func IsItemClicked() (r bool) {
	_ = `auto r = ImGui::IsItemClicked()`
	return
}

// IsItemClickedV is the last item hovered and mouse clicked on? (**) == IsMouseClicked(mouse_button) && IsItemHovered()Important. (**) this is NOT equivalent to the behavior of e.g. Button(). Read comments in function definition.
// * mouse_button ImGuiMouseButton = 0
func IsItemClickedV(mouse_button ImGuiMouseButton /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsItemClicked(mouse_button)`
	return
}

// IsItemVisible is the last item visible? (items may be out of sight because of clipping/scrolling)
func IsItemVisible() (r bool) {
	_ = `auto r = ImGui::IsItemVisible()`
	return
}

// IsItemEdited did the last item modify its underlying value this frame? or was pressed? This is generally the same as the "bool" return value of many widgets.
func IsItemEdited() (r bool) {
	_ = `auto r = ImGui::IsItemEdited()`
	return
}

// IsItemActivated was the last item just made active (item was previously inactive).
func IsItemActivated() (r bool) {
	_ = `auto r = ImGui::IsItemActivated()`
	return
}

// IsItemDeactivated was the last item just made inactive (item was previously active). Useful for Undo/Redo patterns with widgets that require continuous editing.
func IsItemDeactivated() (r bool) {
	_ = `auto r = ImGui::IsItemDeactivated()`
	return
}

// IsItemDeactivatedAfterEdit was the last item just made inactive and made a value change when it was active? (e.g. Slider/Drag moved). Useful for Undo/Redo patterns with widgets that require continuous editing. Note that you may get false positives (some widgets such as Combo()/ListBox()/Selectable() will return true even when clicking an already selected item).
func IsItemDeactivatedAfterEdit() (r bool) {
	_ = `auto r = ImGui::IsItemDeactivatedAfterEdit()`
	return
}

// IsItemToggledOpen was the last item open state toggled? set by TreeNode().
func IsItemToggledOpen() (r bool) {
	_ = `auto r = ImGui::IsItemToggledOpen()`
	return
}

// IsAnyItemHovered is any item hovered?
func IsAnyItemHovered() (r bool) {
	_ = `auto r = ImGui::IsAnyItemHovered()`
	return
}

// IsAnyItemActive is any item active?
func IsAnyItemActive() (r bool) {
	_ = `auto r = ImGui::IsAnyItemActive()`
	return
}

// IsAnyItemFocused is any item focused?
func IsAnyItemFocused() (r bool) {
	_ = `auto r = ImGui::IsAnyItemFocused()`
	return
}

// GetItemID get ID of last item (~~ often same ImGui::GetID(label) beforehand)
func GetItemID() (r ImGuiID) {
	_ = `auto r = ImGui::GetItemID()`
	return
}

// GetItemRectMin get upper-left bounding rectangle of the last item (screen space)
func GetItemRectMin() (r ImVec2) {
	_ = `auto r = ImGui::GetItemRectMin()`
	return
}

// GetItemRectMax get lower-right bounding rectangle of the last item (screen space)
func GetItemRectMax() (r ImVec2) {
	_ = `auto r = ImGui::GetItemRectMax()`
	return
}

// GetItemRectSize get size of last item
func GetItemRectSize() (r ImVec2) {
	_ = `auto r = ImGui::GetItemRectSize()`
	return
}

// GetBackgroundDrawList get background draw list for the viewport associated to the current window. this draw list will be the first rendering one. Useful to quickly draw shapes/text behind dear imgui contents.
func GetBackgroundDrawList() (r ImDrawListPtr) {
	_ = `auto r = ImGui::GetBackgroundDrawList()`
	return
}

// GetForegroundDrawList get foreground draw list for the viewport associated to the current window. this draw list will be the last rendered one. Useful to quickly draw shapes/text over dear imgui contents.
func GetForegroundDrawList() (r ImDrawListPtr) {
	_ = `auto r = ImGui::GetForegroundDrawList()`
	return
}

// IsRectVisible test if rectangle (of given size, starting from cursor position) is visible / not clipped.
func IsRectVisible(size ImVec2) (r bool) {
	_ = `auto r = ImGui::IsRectVisible(size)`
	return
}

// IsRectVisible test if rectangle (in screen space) is visible / not clipped. to perform coarse clipping on user's side.
func IsRectVisible2(rect_min ImVec2, rect_max ImVec2) (r bool) {
	_ = `auto r = ImGui::IsRectVisible(rect_min, rect_max)`
	return
}

// GetTime get global imgui time. incremented by io.DeltaTime every frame.
func GetTime() (r float64) {
	_ = `auto r = ImGui::GetTime()`
	return
}

// GetFrameCount get global imgui frame count. incremented by 1 every frame.
func GetFrameCount() (r int) {
	_ = `auto r = ImGui::GetFrameCount()`
	return
}

// GetStyleColorName get a string corresponding to the enum value (for display, saving, etc.).
func GetStyleColorName(idx ImGuiCol) (r string) {
	_ = `auto r = ImGui::GetStyleColorName(idx)`
	return
}
func ColorConvertU32ToFloat4(in uint32) (r ImVec4) {
	_ = `auto r = ImGui::ColorConvertU32ToFloat4(in)`
	return
}
func ColorConvertFloat4ToU32(in ImVec4) (r uint32) {
	_ = `auto r = ImGui::ColorConvertFloat4ToU32(in)`
	return
}

// IsKeyDown is key being held.
func IsKeyDown(key ImGuiKey) (r bool) {
	_ = `auto r = ImGui::IsKeyDown(ImGuiKey(key))`
	return
}

// IsKeyPressed was key pressed (went from !Down to Down)? if repeat=true, uses io.KeyRepeatDelay / KeyRepeatRate
func IsKeyPressed(key ImGuiKey) (r bool) {
	_ = `auto r = ImGui::IsKeyPressed(ImGuiKey(key))`
	return
}

// IsKeyPressedV was key pressed (went from !Down to Down)? if repeat=true, uses io.KeyRepeatDelay / KeyRepeatRate
// * repeat bool = true
func IsKeyPressedV(key ImGuiKey, repeat bool /* = true*/) (r bool) {
	_ = `auto r = ImGui::IsKeyPressed(ImGuiKey(key), repeat)`
	return
}

// IsKeyReleased was key released (went from Down to !Down)?
func IsKeyReleased(key ImGuiKey) (r bool) {
	_ = `auto r = ImGui::IsKeyReleased(ImGuiKey(key))`
	return
}

// GetKeyPressedAmount uses provided repeat rate/delay. return a count, most often 0 or 1 but might be >1 if RepeatRate is small enough that DeltaTime > RepeatRate
func GetKeyPressedAmount(key ImGuiKey, repeat_delay float32, rate float32) (r int) {
	_ = `auto r = ImGui::GetKeyPressedAmount(ImGuiKey(key), repeat_delay, rate)`
	return
}

// GetKeyName [DEBUG] returns English name of the key. Those names a provided for debugging purpose and are not meant to be saved persistently not compared.
func GetKeyName(key ImGuiKey) (r string) {
	_ = `auto r = ImGui::GetKeyName(ImGuiKey(key))`
	return
}

// SetNextFrameWantCaptureKeyboard Override io.WantCaptureKeyboard flag next frame (said flag is left for your application to handle, typically when true it instructs your app to ignore inputs). e.g. force capture keyboard when your widget is being hovered. This is equivalent to setting "io.WantCaptureKeyboard = want_capture_keyboard"; after the next NewFrame() call.
func SetNextFrameWantCaptureKeyboard(want_capture_keyboard bool) {
	_ = `ImGui::SetNextFrameWantCaptureKeyboard(want_capture_keyboard)`
}

// IsMouseDown is mouse button held?
func IsMouseDown(button ImGuiMouseButton) (r bool) {
	_ = `auto r = ImGui::IsMouseDown(button)`
	return
}

// IsMouseClicked did mouse button clicked? (went from !Down to Down). Same as GetMouseClickedCount() == 1.
func IsMouseClicked(button ImGuiMouseButton) (r bool) {
	_ = `auto r = ImGui::IsMouseClicked(button)`
	return
}

// IsMouseClickedV did mouse button clicked? (went from !Down to Down). Same as GetMouseClickedCount() == 1.
// * repeat bool = false
func IsMouseClickedV(button ImGuiMouseButton, repeat bool /* = false*/) (r bool) {
	_ = `auto r = ImGui::IsMouseClicked(button, repeat)`
	return
}

// IsMouseReleased did mouse button released? (went from Down to !Down)
func IsMouseReleased(button ImGuiMouseButton) (r bool) {
	_ = `auto r = ImGui::IsMouseReleased(button)`
	return
}

// IsMouseDoubleClicked did mouse button double-clicked? Same as GetMouseClickedCount() == 2. (note that a double-click will also report IsMouseClicked() == true)
func IsMouseDoubleClicked(button ImGuiMouseButton) (r bool) {
	_ = `auto r = ImGui::IsMouseDoubleClicked(button)`
	return
}

// GetMouseClickedCount return the number of successive mouse-clicks at the time where a click happen (otherwise 0).
func GetMouseClickedCount(button ImGuiMouseButton) (r int) {
	_ = `auto r = ImGui::GetMouseClickedCount(button)`
	return
}

// IsMouseHoveringRect is mouse hovering given bounding rect (in screen space). clipped by current clipping settings, but disregarding of other consideration of focus/window ordering/popup-block.
func IsMouseHoveringRect(r_min ImVec2, r_max ImVec2) (r bool) {
	_ = `auto r = ImGui::IsMouseHoveringRect(r_min, r_max)`
	return
}

// IsMouseHoveringRectV is mouse hovering given bounding rect (in screen space). clipped by current clipping settings, but disregarding of other consideration of focus/window ordering/popup-block.
// * clip bool = true
func IsMouseHoveringRectV(r_min ImVec2, r_max ImVec2, clip bool /* = true*/) (r bool) {
	_ = `auto r = ImGui::IsMouseHoveringRect(r_min, r_max, clip)`
	return
}

// IsMousePosValid by convention we use (-FLT_MAX,-FLT_MAX) to denote that there is no mouse available
func IsMousePosValid() (r bool) {
	_ = `auto r = ImGui::IsMousePosValid()`
	return
}

// GetMousePos shortcut to ImGui::GetIO().MousePos provided by user, to be consistent with other calls
func GetMousePos() (r ImVec2) {
	_ = `auto r = ImGui::GetMousePos()`
	return
}

// GetMousePosOnOpeningCurrentPopup retrieve mouse position at the time of opening popup we have BeginPopup() into (helper to avoid user backing that value themselves)
func GetMousePosOnOpeningCurrentPopup() (r ImVec2) {
	_ = `auto r = ImGui::GetMousePosOnOpeningCurrentPopup()`
	return
}

// IsMouseDragging is mouse dragging? (if lock_threshold < -1.0f, uses io.MouseDraggingThreshold)
func IsMouseDragging(button ImGuiMouseButton) (r bool) {
	_ = `auto r = ImGui::IsMouseDragging(button)`
	return
}

// IsMouseDraggingV is mouse dragging? (if lock_threshold < -1.0f, uses io.MouseDraggingThreshold)
// * lock_threshold float = -1.0f
func IsMouseDraggingV(button ImGuiMouseButton, lock_threshold float32 /* = -1.0f*/) (r bool) {
	_ = `auto r = ImGui::IsMouseDragging(button, lock_threshold)`
	return
}

// GetMouseDragDelta return the delta from the initial clicking position while the mouse button is pressed or was just released. This is locked and return 0.0f until the mouse moves past a distance threshold at least once (if lock_threshold < -1.0f, uses io.MouseDraggingThreshold)
func GetMouseDragDelta() (r ImVec2) {
	_ = `auto r = ImGui::GetMouseDragDelta()`
	return
}

// GetMouseDragDeltaV return the delta from the initial clicking position while the mouse button is pressed or was just released. This is locked and return 0.0f until the mouse moves past a distance threshold at least once (if lock_threshold < -1.0f, uses io.MouseDraggingThreshold)
// * button ImGuiMouseButton = 0
// * lock_threshold float = -1.0f
func GetMouseDragDeltaV(button ImGuiMouseButton /* = 0*/, lock_threshold float32 /* = -1.0f*/) (r ImVec2) {
	_ = `auto r = ImGui::GetMouseDragDelta(button, lock_threshold)`
	return
}
func ResetMouseDragDelta() {
	_ = `ImGui::ResetMouseDragDelta()`
}
func ResetMouseDragDeltaV(button ImGuiMouseButton /* = 0*/) {
	_ = `ImGui::ResetMouseDragDelta(button)`
}

// GetMouseCursor get desired mouse cursor shape. Important: reset in ImGui::NewFrame(), this is updated during the frame. valid before Render(). If you use software rendering by setting io.MouseDrawCursor ImGui will render those for you
func GetMouseCursor() (r ImGuiMouseCursor) {
	_ = `auto r = ImGui::GetMouseCursor()`
	return
}

// SetMouseCursor set desired mouse cursor shape
func SetMouseCursor(cursor_type ImGuiMouseCursor) {
	_ = `ImGui::SetMouseCursor(cursor_type)`
}

// SetNextFrameWantCaptureMouse Override io.WantCaptureMouse flag next frame (said flag is left for your application to handle, typical when true it instucts your app to ignore inputs). This is equivalent to setting "io.WantCaptureMouse = want_capture_mouse;" after the next NewFrame() call.
func SetNextFrameWantCaptureMouse(want_capture_mouse bool) {
	_ = `ImGui::SetNextFrameWantCaptureMouse(want_capture_mouse)`
}
func GetClipboardText() (r string) {
	_ = `auto r = ImGui::GetClipboardText()`
	return
}
func SetClipboardText(text string) {
	_ = `ImGui::SetClipboardText(text)`
}

// LoadIniSettingsFromDisk call after CreateContext() and before the first call to NewFrame(). NewFrame() automatically calls LoadIniSettingsFromDisk(io.IniFilename).
func LoadIniSettingsFromDisk(ini_filename string) {
	_ = `ImGui::LoadIniSettingsFromDisk(ini_filename)`
}

// LoadIniSettingsFromMemory call after CreateContext() and before the first call to NewFrame() to provide .ini data from your own data source.
func LoadIniSettingsFromMemory(ini_data string) {
	_ = `ImGui::LoadIniSettingsFromMemory(ini_data)`
}

// LoadIniSettingsFromMemoryV call after CreateContext() and before the first call to NewFrame() to provide .ini data from your own data source.
// * ini_size size_t = 0
func LoadIniSettingsFromMemoryV(ini_data string, ini_size Size_t /* = 0*/) {
	_ = `ImGui::LoadIniSettingsFromMemory(ini_data, ini_size)`
}

// SaveIniSettingsToDisk this is automatically called (if io.IniFilename is not empty) a few seconds after any modification that should be reflected in the .ini file (and also by DestroyContext).
func SaveIniSettingsToDisk(ini_filename string) {
	_ = `ImGui::SaveIniSettingsToDisk(ini_filename)`
}

// SaveIniSettingsToMemory return a zero-terminated string with the .ini data which you can save by your own mean. call when io.WantSaveIniSettings is set, then save data by your own mean and clear io.WantSaveIniSettings.
func SaveIniSettingsToMemory() (r string) {
	_ = `auto r = ImGui::SaveIniSettingsToMemory()`
	return
}
func DebugTextEncoding(text string) {
	_ = `ImGui::DebugTextEncoding(text)`
}
func DebugFlashStyleColor(idx ImGuiCol) {
	_ = `ImGui::DebugFlashStyleColor(idx)`
}

// DebugCheckVersionAndDataLayout This is called by IMGUI_CHECKVERSION() macro.
func DebugCheckVersionAndDataLayout(version_str string, sz_io Size_t, sz_style Size_t, sz_vec2 Size_t, sz_vec4 Size_t, sz_drawvert Size_t, sz_drawidx Size_t) (r bool) {
	_ = `auto r = ImGui::DebugCheckVersionAndDataLayout(version_str, sz_io, sz_style, sz_vec2, sz_vec4, sz_drawvert, sz_drawidx)`
	return
}

// UpdatePlatformWindows call in main loop. will call CreateWindow/ResizeWindow/etc. platform functions for each secondary viewport, and DestroyWindow for each inactive viewport.
func UpdatePlatformWindows() {
	_ = `ImGui::UpdatePlatformWindows()`
}

// RenderPlatformWindowsDefault call in main loop. will call RenderWindow/SwapBuffers platform functions for each secondary viewport which doesn't have the ImGuiViewportFlags_Minimized flag set. May be reimplemented by user for custom rendering needs.
func RenderPlatformWindowsDefault() {
	_ = `ImGui::RenderPlatformWindowsDefault()`
}

// DestroyPlatformWindows call DestroyWindow platform functions for all viewports. call from backend Shutdown() if you need to close platform windows before imgui shutdown. otherwise will be called by DestroyContext().
func DestroyPlatformWindows() {
	_ = `ImGui::DestroyPlatformWindows()`
}

// GetKeyIndex map ImGuiKey_* values into legacy native key index. == io.KeyMap[key]
func GetKeyIndex(key ImGuiKey) (r ImGuiKey) {
	_ = `auto r = ImGui::GetKeyIndex(ImGuiKey(key))`
	return
}

// SetItemAllowOverlap Use SetNextItemAllowOverlap() before item.
func SetItemAllowOverlap() {
	_ = `ImGui::SetItemAllowOverlap()`
}

// ImageButton Use new ImageButton() signature (explicit item id, regular FramePadding)
func ImageButtonOld(user_texture_id ImTextureID, size ImVec2) (r bool) {
	_ = `auto r = ImGui::ImageButton(ImTextureID(user_texture_id), size)`
	return
}

// ImageButtonV Use new ImageButton() signature (explicit item id, regular FramePadding)
// * uv0 const ImVec2 & = ImVec2(0, 0)
// * uv1 const ImVec2 & = ImVec2(1, 1)
// * frame_padding int = -1
// * bg_col const ImVec4 & = ImVec4(0, 0, 0, 0)
// * tint_col const ImVec4 & = ImVec4(1, 1, 1, 1)
func ImageButtonVOld(user_texture_id ImTextureID, size ImVec2, uv0 ImVec2 /* = ImVec2(0, 0)*/, uv1 ImVec2 /* = ImVec2(1, 1)*/, frame_padding int /* = -1*/, bg_col ImVec4 /* = ImVec4(0, 0, 0, 0)*/, tint_col ImVec4 /* = ImVec4(1, 1, 1, 1)*/) (r bool) {
	_ = `auto r = ImGui::ImageButton(ImTextureID(user_texture_id), size, uv0, uv1, frame_padding, bg_col, tint_col)`
	return
}
