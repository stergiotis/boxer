//go:build fffi_idl_code

package imgui

func SetNextWindowRefreshPolicy(flags ImGuiWindowRefreshFlags) {
	_ = `ImGui::SetNextWindowRefreshPolicy(flags)`
}
func PushPasswordFont() {
	_ = `ImGui::PushPasswordFont()`
}
func Initialize() {
	_ = `ImGui::Initialize()`
}
func UpdateInputEvents(trickle_fast_inputs bool) {
	_ = `ImGui::UpdateInputEvents(trickle_fast_inputs)`
}
func UpdateHoveredWindowAndCaptureFlags() {
	_ = `ImGui::UpdateHoveredWindowAndCaptureFlags()`
}
func UpdateMouseMovingWindowNewFrame() {
	_ = `ImGui::UpdateMouseMovingWindowNewFrame()`
}
func UpdateMouseMovingWindowEndFrame() {
	_ = `ImGui::UpdateMouseMovingWindowEndFrame()`
}
func MarkIniSettingsDirty() {
	_ = `ImGui::MarkIniSettingsDirty()`
}
func ClearIniSettings() {
	_ = `ImGui::ClearIniSettings()`
}
func RemoveSettingsHandler(type_name string) {
	_ = `ImGui::RemoveSettingsHandler(type_name)`
}
func ClearWindowSettings(name string) {
	_ = `ImGui::ClearWindowSettings(name)`
}
func ScrollToItem() {
	_ = `ImGui::ScrollToItem()`
}
func ScrollToItemV(flags ImGuiScrollFlags /* = 0*/) {
	_ = `ImGui::ScrollToItem(flags)`
}
func ClearActiveID() {
	_ = `ImGui::ClearActiveID()`
}
func GetHoveredID() (r ImGuiID) {
	_ = `auto r = ImGui::GetHoveredID()`
	return
}
func SetHoveredID(id ImGuiID) {
	_ = `ImGui::SetHoveredID(id)`
}
func KeepAliveID(id ImGuiID) {
	_ = `ImGui::KeepAliveID(id)`
}

// MarkItemEdited Mark data associated to given item as "edited", used by IsItemDeactivatedAfterEdit() function.
func MarkItemEdited(id ImGuiID) {
	_ = `ImGui::MarkItemEdited(id)`
}

// PushOverrideID Push given value as-is at the top of the ID stack (whereas PushID combines old and new hashes)
func PushOverrideID(id ImGuiID) {
	_ = `ImGui::PushOverrideID(id)`
}
func GetIDWithSeed(n int, seed ImGuiID) (r ImGuiID) {
	_ = `auto r = ImGui::GetIDWithSeed(n, seed)`
	return
}
func ItemSize(size ImVec2) {
	_ = `ImGui::ItemSize(size)`
}
func ItemSizeV(size ImVec2, text_baseline_y float32 /* = -1.0f*/) {
	_ = `ImGui::ItemSize(size, text_baseline_y)`
}
func CalcItemSize(size ImVec2, default_w float32, default_h float32) (r ImVec2) {
	_ = `auto r = ImGui::CalcItemSize(size, default_w, default_h)`
	return
}
func CalcWrapWidthForPos(pos ImVec2, wrap_pos_x float32) (r float32) {
	_ = `auto r = ImGui::CalcWrapWidthForPos(pos, wrap_pos_x)`
	return
}
func PushMultiItemsWidths(components int, width_full float32) {
	_ = `ImGui::PushMultiItemsWidths(components, width_full)`
}
func BeginDisabledOverrideReenable() {
	_ = `ImGui::BeginDisabledOverrideReenable()`
}
func EndDisabledOverrideReenable() {
	_ = `ImGui::EndDisabledOverrideReenable()`
}

// LogBegin -> BeginCapture() when we design v2 api, for now stay under the radar by using the old name.
func LogBegin(flags ImGuiLogFlags, auto_open_depth int) {
	_ = `ImGui::LogBegin(flags, auto_open_depth)`
}

// LogToBuffer Start logging/capturing to internal buffer
func LogToBuffer() {
	_ = `ImGui::LogToBuffer()`
}

// LogToBufferV Start logging/capturing to internal buffer
// * auto_open_depth int = -1
func LogToBufferV(auto_open_depth int /* = -1*/) {
	_ = `ImGui::LogToBuffer(auto_open_depth)`
}
func LogSetNextTextDecoration(prefix string, suffix string) {
	_ = `ImGui::LogSetNextTextDecoration(prefix, suffix)`
}
func BeginChildEx(name string, id ImGuiID, size_arg ImVec2, child_flags ImGuiChildFlags, window_flags ImGuiWindowFlags) (r bool) {
	_ = `auto r = ImGui::BeginChildEx(name, id, size_arg, child_flags, window_flags)`
	return
}
func BeginPopupEx(id ImGuiID, extra_window_flags ImGuiWindowFlags) (r bool) {
	_ = `auto r = ImGui::BeginPopupEx(id, extra_window_flags)`
	return
}
func OpenPopupEx(id ImGuiID) {
	_ = `ImGui::OpenPopupEx(id)`
}
func OpenPopupExV(id ImGuiID, popup_flags ImGuiPopupFlags /* = ImGuiPopupFlags_None*/) {
	_ = `ImGui::OpenPopupEx(id, popup_flags)`
}
func ClosePopupToLevel(remaining int, restore_focus_to_window_under_popup bool) {
	_ = `ImGui::ClosePopupToLevel(remaining, restore_focus_to_window_under_popup)`
}
func ClosePopupsExceptModals() {
	_ = `ImGui::ClosePopupsExceptModals()`
}
func IsPopupOpenIdI(id ImGuiID, popup_flags ImGuiPopupFlags) (r bool) {
	_ = `auto r = ImGui::IsPopupOpen(id, popup_flags)`
	return
}
func BeginTooltipEx(tooltip_flags ImGuiTooltipFlags, extra_window_flags ImGuiWindowFlags) (r bool) {
	_ = `auto r = ImGui::BeginTooltipEx(tooltip_flags, extra_window_flags)`
	return
}
func BeginTooltipHidden() (r bool) {
	_ = `auto r = ImGui::BeginTooltipHidden()`
	return
}
func BeginMenuEx(label string, icon string) (r bool) {
	_ = `auto r = ImGui::BeginMenuEx(label, icon)`
	return
}
func BeginMenuExV(label string, icon string, enabled bool /* = true*/) (r bool) {
	_ = `auto r = ImGui::BeginMenuEx(label, icon, enabled)`
	return
}
func MenuItemEx(label string, icon string) (r bool) {
	_ = `auto r = ImGui::MenuItemEx(label, icon)`
	return
}
func MenuItemExV(label string, icon string, shortcut string /* = NULL*/, selected bool /* = false*/, enabled bool /* = true*/) (r bool) {
	_ = `auto r = ImGui::MenuItemEx(label, icon, shortcut, selected, enabled)`
	return
}
func BeginComboPreview() (r bool) {
	_ = `auto r = ImGui::BeginComboPreview()`
	return
}
func EndComboPreview() {
	_ = `ImGui::EndComboPreview()`
}
func NavInitRequestApplyResult() {
	_ = `ImGui::NavInitRequestApplyResult()`
}
func NavMoveRequestButNoResultYet() (r bool) {
	_ = `auto r = ImGui::NavMoveRequestButNoResultYet()`
	return
}
func NavMoveRequestSubmit(move_dir ImGuiDir, clip_dir ImGuiDir, move_flags ImGuiNavMoveFlags, scroll_flags ImGuiScrollFlags) {
	_ = `ImGui::NavMoveRequestSubmit(ImGuiDir(move_dir), ImGuiDir(clip_dir), move_flags, scroll_flags)`
}
func NavMoveRequestForward(move_dir ImGuiDir, clip_dir ImGuiDir, move_flags ImGuiNavMoveFlags, scroll_flags ImGuiScrollFlags) {
	_ = `ImGui::NavMoveRequestForward(ImGuiDir(move_dir), ImGuiDir(clip_dir), move_flags, scroll_flags)`
}
func NavMoveRequestCancel() {
	_ = `ImGui::NavMoveRequestCancel()`
}
func NavMoveRequestApplyResult() {
	_ = `ImGui::NavMoveRequestApplyResult()`
}
func NavHighlightActivated(id ImGuiID) {
	_ = `ImGui::NavHighlightActivated(id)`
}
func SetNavCursorVisibleAfterMove() {
	_ = `ImGui::SetNavCursorVisibleAfterMove()`
}
func NavUpdateCurrentWindowIsScrollPushableX() {
	_ = `ImGui::NavUpdateCurrentWindowIsScrollPushableX()`
}
func SetNavFocusScope(focus_scope_id ImGuiID) {
	_ = `ImGui::SetNavFocusScope(focus_scope_id)`
}

// FocusItem Focus last item (no selection/activation).
func FocusItem() {
	_ = `ImGui::FocusItem()`
}

// ActivateItemByID Activate an item by ID (button, checkbox, tree node etc.). Activation is queued and processed on the next frame when the item is encountered again.
func ActivateItemByID(id ImGuiID) {
	_ = `ImGui::ActivateItemByID(id)`
}
func IsMouseDragPastThreshold(button ImGuiMouseButton) (r bool) {
	_ = `auto r = ImGui::IsMouseDragPastThreshold(button)`
	return
}
func IsMouseDragPastThresholdV(button ImGuiMouseButton, lock_threshold float32 /* = -1.0f*/) (r bool) {
	_ = `auto r = ImGui::IsMouseDragPastThreshold(button, lock_threshold)`
	return
}
func GetKeyMagnitude2d(key_left ImGuiKey, key_right ImGuiKey, key_up ImGuiKey, key_down ImGuiKey) (r ImVec2) {
	_ = `auto r = ImGui::GetKeyMagnitude2d(ImGuiKey(key_left), ImGuiKey(key_right), ImGuiKey(key_up), ImGuiKey(key_down))`
	return
}
func CalcTypematicRepeatAmount(t0 float32, t1 float32, repeat_delay float32, repeat_rate float32) (r int) {
	_ = `auto r = ImGui::CalcTypematicRepeatAmount(t0, t1, repeat_delay, repeat_rate)`
	return
}
func TeleportMousePos(pos ImVec2) {
	_ = `ImGui::TeleportMousePos(pos)`
}
func SetActiveIdUsingAllKeyboardKeys() {
	_ = `ImGui::SetActiveIdUsingAllKeyboardKeys()`
}
func GetKeyOwner(key ImGuiKey) (r ImGuiID) {
	_ = `auto r = ImGui::GetKeyOwner(ImGuiKey(key))`
	return
}
func SetKeyOwner(key ImGuiKey, owner_id ImGuiID) {
	_ = `ImGui::SetKeyOwner(ImGuiKey(key), owner_id)`
}
func SetKeyOwnerV(key ImGuiKey, owner_id ImGuiID, flags ImGuiInputFlags /* = 0*/) {
	_ = `ImGui::SetKeyOwner(ImGuiKey(key), owner_id, flags)`
}

// SetItemKeyOwner Set key owner to last item if it is hovered or active. Equivalent to 'if (IsItemHovered() || IsItemActive()) { SetKeyOwner(key, GetItemID());'.
func SetItemKeyOwnerI(key ImGuiKey, flags ImGuiInputFlags) {
	_ = `ImGui::SetItemKeyOwner(ImGuiKey(key), flags)`
}

// TestKeyOwner Test that key is either not owned, either owned by 'owner_id'
func TestKeyOwner(key ImGuiKey, owner_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::TestKeyOwner(ImGuiKey(key), owner_id)`
	return
}
func IsKeyDownI(key ImGuiKey, owner_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::IsKeyDown(ImGuiKey(key), owner_id)`
	return
}

// IsKeyPressed Important: when transitioning from old to new IsKeyPressed(): old API has "bool repeat = true", so would default to repeat. New API requiress explicit ImGuiInputFlags_Repeat.
func IsKeyPressedI(key ImGuiKey, flags ImGuiInputFlags) (r bool) {
	_ = `auto r = ImGui::IsKeyPressed(ImGuiKey(key), flags)`
	return
}

// IsKeyPressedV Important: when transitioning from old to new IsKeyPressed(): old API has "bool repeat = true", so would default to repeat. New API requiress explicit ImGuiInputFlags_Repeat.
// * owner_id ImGuiID = 0
func IsKeyPressedVI(key ImGuiKey, flags ImGuiInputFlags, owner_id ImGuiID /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsKeyPressed(ImGuiKey(key), flags, owner_id)`
	return
}
func IsKeyReleasedI(key ImGuiKey, owner_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::IsKeyReleased(ImGuiKey(key), owner_id)`
	return
}
func IsMouseDownI(button ImGuiMouseButton, owner_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::IsMouseDown(button, owner_id)`
	return
}
func IsMouseClickedI(button ImGuiMouseButton, flags ImGuiInputFlags) (r bool) {
	_ = `auto r = ImGui::IsMouseClicked(button, flags)`
	return
}
func IsMouseClickedVI(button ImGuiMouseButton, flags ImGuiInputFlags, owner_id ImGuiID /* = 0*/) (r bool) {
	_ = `auto r = ImGui::IsMouseClicked(button, flags, owner_id)`
	return
}
func IsMouseReleasedI(button ImGuiMouseButton, owner_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::IsMouseReleased(button, owner_id)`
	return
}
func IsMouseDoubleClickedI(button ImGuiMouseButton, owner_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::IsMouseDoubleClicked(button, owner_id)`
	return
}
func DockNodeEndAmendTabBar() {
	_ = `ImGui::DockNodeEndAmendTabBar()`
}
func DockBuilderDockWindow(window_name string, node_id ImGuiID) {
	_ = `ImGui::DockBuilderDockWindow(window_name, node_id)`
}
func DockBuilderAddNode() (r ImGuiID) {
	_ = `auto r = ImGui::DockBuilderAddNode()`
	return
}
func DockBuilderAddNodeV(node_id ImGuiID /* = 0*/, flags ImGuiDockNodeFlags /* = 0*/) (r ImGuiID) {
	_ = `auto r = ImGui::DockBuilderAddNode(node_id, flags)`
	return
}

// DockBuilderRemoveNode Remove node and all its child, undock all windows
func DockBuilderRemoveNode(node_id ImGuiID) {
	_ = `ImGui::DockBuilderRemoveNode(node_id)`
}
func DockBuilderRemoveNodeDockedWindows(node_id ImGuiID) {
	_ = `ImGui::DockBuilderRemoveNodeDockedWindows(node_id)`
}
func DockBuilderRemoveNodeDockedWindowsV(node_id ImGuiID, clear_settings_refs bool /* = true*/) {
	_ = `ImGui::DockBuilderRemoveNodeDockedWindows(node_id, clear_settings_refs)`
}

// DockBuilderRemoveNodeChildNodes Remove all split/hierarchy. All remaining docked windows will be re-docked to the remaining root node (node_id).
func DockBuilderRemoveNodeChildNodes(node_id ImGuiID) {
	_ = `ImGui::DockBuilderRemoveNodeChildNodes(node_id)`
}
func DockBuilderSetNodePos(node_id ImGuiID, pos ImVec2) {
	_ = `ImGui::DockBuilderSetNodePos(node_id, pos)`
}
func DockBuilderSetNodeSize(node_id ImGuiID, size ImVec2) {
	_ = `ImGui::DockBuilderSetNodeSize(node_id, size)`
}
func DockBuilderCopyWindowSettings(src_name string, dst_name string) {
	_ = `ImGui::DockBuilderCopyWindowSettings(src_name, dst_name)`
}
func DockBuilderFinish(node_id ImGuiID) {
	_ = `ImGui::DockBuilderFinish(node_id)`
}
func PushFocusScope(id ImGuiID) {
	_ = `ImGui::PushFocusScope(id)`
}
func PopFocusScope() {
	_ = `ImGui::PopFocusScope()`
}
func IsDragDropActive() (r bool) {
	_ = `auto r = ImGui::IsDragDropActive()`
	return
}
func ClearDragDrop() {
	_ = `ImGui::ClearDragDrop()`
}
func IsDragDropPayloadBeingAccepted() (r bool) {
	_ = `auto r = ImGui::IsDragDropPayloadBeingAccepted()`
	return
}

// BeginColumns setup number of columns. use an identifier to distinguish multiple column sets. close with EndColumns().
func BeginColumns(str_id string, count int) {
	_ = `ImGui::BeginColumns(str_id, count)`
}

// BeginColumnsV setup number of columns. use an identifier to distinguish multiple column sets. close with EndColumns().
// * flags ImGuiOldColumnFlags = 0
func BeginColumnsV(str_id string, count int, flags ImGuiOldColumnFlags /* = 0*/) {
	_ = `ImGui::BeginColumns(str_id, count, flags)`
}

// EndColumns close columns
func EndColumns() {
	_ = `ImGui::EndColumns()`
}
func PushColumnClipRect(column_index int) {
	_ = `ImGui::PushColumnClipRect(column_index)`
}
func PushColumnsBackground() {
	_ = `ImGui::PushColumnsBackground()`
}
func PopColumnsBackground() {
	_ = `ImGui::PopColumnsBackground()`
}
func GetColumnsID(str_id string, count int) (r ImGuiID) {
	_ = `auto r = ImGui::GetColumnsID(str_id, count)`
	return
}
func TableOpenContextMenu() {
	_ = `ImGui::TableOpenContextMenu()`
}
func TableOpenContextMenuV(column_n int /* = -1*/) {
	_ = `ImGui::TableOpenContextMenu(column_n)`
}
func TableSetColumnWidth(column_n int, width float32) {
	_ = `ImGui::TableSetColumnWidth(column_n, width)`
}
func TableSetColumnSortDirection(column_n int, sort_direction ImGuiSortDirection, append_to_sort_specs bool) {
	_ = `ImGui::TableSetColumnSortDirection(column_n, (ImGuiSortDirection)sort_direction, append_to_sort_specs)`
}

// TableGetHoveredRow Retrieve *PREVIOUS FRAME* hovered row. This difference with TableGetHoveredColumn() is the reason why this is not public yet.
func TableGetHoveredRow() (r int) {
	_ = `auto r = ImGui::TableGetHoveredRow()`
	return
}
func TableGetHeaderRowHeight() (r float32) {
	_ = `auto r = ImGui::TableGetHeaderRowHeight()`
	return
}
func TableGetHeaderAngledMaxLabelWidth() (r float32) {
	_ = `auto r = ImGui::TableGetHeaderAngledMaxLabelWidth()`
	return
}
func TablePushBackgroundChannel() {
	_ = `ImGui::TablePushBackgroundChannel()`
}
func TablePopBackgroundChannel() {
	_ = `ImGui::TablePopBackgroundChannel()`
}
func BeginTableEx(name string, id ImGuiID, columns_count int) (r bool) {
	_ = `auto r = ImGui::BeginTableEx(name, id, columns_count)`
	return
}
func BeginTableExV(name string, id ImGuiID, columns_count int, flags ImGuiTableFlags /* = 0*/, outer_size ImVec2 /* = ImVec2(0, 0)*/, inner_width float32 /* = 0.0f*/) (r bool) {
	_ = `auto r = ImGui::BeginTableEx(name, id, columns_count, flags, outer_size, inner_width)`
	return
}
func TableGcCompactSettings() {
	_ = `ImGui::TableGcCompactSettings()`
}
func TableSettingsAddSettingsHandler() {
	_ = `ImGui::TableSettingsAddSettingsHandler()`
}
func TabItemSpacing(str_id string, flags ImGuiTabItemFlags, width float32) {
	_ = `ImGui::TabItemSpacing(str_id, flags, width)`
}
func TabItemCalcSize(label string, has_close_button_or_unsaved_marker bool) (r ImVec2) {
	_ = `auto r = ImGui::TabItemCalcSize(label, has_close_button_or_unsaved_marker)`
	return
}
func RenderFrame(p_min ImVec2, p_max ImVec2, fill_col uint32) {
	_ = `ImGui::RenderFrame(p_min, p_max, fill_col)`
}
func RenderFrameV(p_min ImVec2, p_max ImVec2, fill_col uint32, borders bool /* = true*/, rounding float32 /* = 0.0f*/) {
	_ = `ImGui::RenderFrame(p_min, p_max, fill_col, borders, rounding)`
}
func RenderFrameBorder(p_min ImVec2, p_max ImVec2) {
	_ = `ImGui::RenderFrameBorder(p_min, p_max)`
}
func RenderFrameBorderV(p_min ImVec2, p_max ImVec2, rounding float32 /* = 0.0f*/) {
	_ = `ImGui::RenderFrameBorder(p_min, p_max, rounding)`
}
func RenderColorRectWithAlphaCheckerboard(draw_list ImDrawListPtr, p_min ImVec2, p_max ImVec2, fill_col uint32, grid_step float32, grid_off ImVec2) {
	_ = `ImGui::RenderColorRectWithAlphaCheckerboard((ImDrawList *)draw_list, p_min, p_max, fill_col, grid_step, grid_off)`
}
func RenderColorRectWithAlphaCheckerboardV(draw_list ImDrawListPtr, p_min ImVec2, p_max ImVec2, fill_col uint32, grid_step float32, grid_off ImVec2, rounding float32 /* = 0.0f*/, flags ImDrawFlags /* = 0*/) {
	_ = `ImGui::RenderColorRectWithAlphaCheckerboard((ImDrawList *)draw_list, p_min, p_max, fill_col, grid_step, grid_off, rounding, flags)`
}
func RenderMouseCursor(pos ImVec2, scale float32, mouse_cursor ImGuiMouseCursor, col_fill uint32, col_border uint32, col_shadow uint32) {
	_ = `ImGui::RenderMouseCursor(pos, scale, mouse_cursor, col_fill, col_border, col_shadow)`
}
func RenderArrow(draw_list ImDrawListPtr, pos ImVec2, col uint32, dir ImGuiDir) {
	_ = `ImGui::RenderArrow((ImDrawList *)draw_list, pos, col, ImGuiDir(dir))`
}
func RenderArrowV(draw_list ImDrawListPtr, pos ImVec2, col uint32, dir ImGuiDir, scale float32 /* = 1.0f*/) {
	_ = `ImGui::RenderArrow((ImDrawList *)draw_list, pos, col, ImGuiDir(dir), scale)`
}
func RenderBullet(draw_list ImDrawListPtr, pos ImVec2, col uint32) {
	_ = `ImGui::RenderBullet((ImDrawList *)draw_list, pos, col)`
}
func RenderCheckMark(draw_list ImDrawListPtr, pos ImVec2, col uint32, sz float32) {
	_ = `ImGui::RenderCheckMark((ImDrawList *)draw_list, pos, col, sz)`
}
func RenderArrowPointingAt(draw_list ImDrawListPtr, pos ImVec2, half_sz ImVec2, direction ImGuiDir, col uint32) {
	_ = `ImGui::RenderArrowPointingAt((ImDrawList *)draw_list, pos, half_sz, ImGuiDir(direction), col)`
}
func RenderArrowDockMenu(draw_list ImDrawListPtr, p_min ImVec2, sz float32, col uint32) {
	_ = `ImGui::RenderArrowDockMenu((ImDrawList *)draw_list, p_min, sz, col)`
}
func ButtonEx(label string) (r bool) {
	_ = `auto r = ImGui::ButtonEx(label)`
	return
}
func ButtonExV(label string, size_arg ImVec2 /* = ImVec2(0, 0)*/, flags ImGuiButtonFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::ButtonEx(label, size_arg, flags)`
	return
}
func ArrowButtonEx(str_id string, dir ImGuiDir, size_arg ImVec2) (r bool) {
	_ = `auto r = ImGui::ArrowButtonEx(str_id, ImGuiDir(dir), size_arg)`
	return
}
func ArrowButtonExV(str_id string, dir ImGuiDir, size_arg ImVec2, flags ImGuiButtonFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::ArrowButtonEx(str_id, ImGuiDir(dir), size_arg, flags)`
	return
}
func ImageButtonEx(id ImGuiID, user_texture_id ImTextureID, image_size ImVec2, uv0 ImVec2, uv1 ImVec2, bg_col ImVec4, tint_col ImVec4) (r bool) {
	_ = `auto r = ImGui::ImageButtonEx(id, ImTextureID(user_texture_id), image_size, uv0, uv1, bg_col, tint_col)`
	return
}
func ImageButtonExV(id ImGuiID, user_texture_id ImTextureID, image_size ImVec2, uv0 ImVec2, uv1 ImVec2, bg_col ImVec4, tint_col ImVec4, flags ImGuiButtonFlags /* = 0*/) (r bool) {
	_ = `auto r = ImGui::ImageButtonEx(id, ImTextureID(user_texture_id), image_size, uv0, uv1, bg_col, tint_col, flags)`
	return
}
func SeparatorEx(flags ImGuiSeparatorFlags) {
	_ = `ImGui::SeparatorEx(flags)`
}
func SeparatorExV(flags ImGuiSeparatorFlags, thickness float32 /* = 1.0f*/) {
	_ = `ImGui::SeparatorEx(flags, thickness)`
}
func CloseButton(id ImGuiID, pos ImVec2) (r bool) {
	_ = `auto r = ImGui::CloseButton(id, pos)`
	return
}
func TreePushOverrideID(id ImGuiID) {
	_ = `ImGui::TreePushOverrideID(id)`
}
func TreeNodeGetOpen(storage_id ImGuiID) (r bool) {
	_ = `auto r = ImGui::TreeNodeGetOpen(storage_id)`
	return
}
func TreeNodeSetOpen(storage_id ImGuiID, open bool) {
	_ = `ImGui::TreeNodeSetOpen(storage_id, open)`
}

// TreeNodeUpdateNextOpen Return open state. Consume previous SetNextItemOpen() data, if any. May return true when logging.
func TreeNodeUpdateNextOpen(storage_id ImGuiID, flags ImGuiTreeNodeFlags) (r bool) {
	_ = `auto r = ImGui::TreeNodeUpdateNextOpen(storage_id, flags)`
	return
}
func InputTextDeactivateHook(id ImGuiID) {
	_ = `ImGui::InputTextDeactivateHook(id)`
}
func ShadeVertsLinearColorGradientKeepAlpha(draw_list ImDrawListPtr, vert_start_idx int, vert_end_idx int, gradient_p0 ImVec2, gradient_p1 ImVec2, col0 uint32, col1 uint32) {
	_ = `ImGui::ShadeVertsLinearColorGradientKeepAlpha((ImDrawList *)draw_list, vert_start_idx, vert_end_idx, gradient_p0, gradient_p1, col0, col1)`
}
func ShadeVertsLinearUV(draw_list ImDrawListPtr, vert_start_idx int, vert_end_idx int, a ImVec2, b ImVec2, uv_a ImVec2, uv_b ImVec2, clamp bool) {
	_ = `ImGui::ShadeVertsLinearUV((ImDrawList *)draw_list, vert_start_idx, vert_end_idx, a, b, uv_a, uv_b, clamp)`
}
func ShadeVertsTransformPos(draw_list ImDrawListPtr, vert_start_idx int, vert_end_idx int, pivot_in ImVec2, cos_a float32, sin_a float32, pivot_out ImVec2) {
	_ = `ImGui::ShadeVertsTransformPos((ImDrawList *)draw_list, vert_start_idx, vert_end_idx, pivot_in, cos_a, sin_a, pivot_out)`
}
func GcCompactTransientMiscBuffers() {
	_ = `ImGui::GcCompactTransientMiscBuffers()`
}
func ErrorLog(msg string) (r bool) {
	_ = `auto r = ImGui::ErrorLog(msg)`
	return
}
func ErrorCheckUsingSetCursorPosToExtendParentBoundaries() {
	_ = `ImGui::ErrorCheckUsingSetCursorPosToExtendParentBoundaries()`
}
func ErrorCheckEndFrameFinalizeErrorTooltip() {
	_ = `ImGui::ErrorCheckEndFrameFinalizeErrorTooltip()`
}
func BeginErrorTooltip() (r bool) {
	_ = `auto r = ImGui::BeginErrorTooltip()`
	return
}
func EndErrorTooltip() {
	_ = `ImGui::EndErrorTooltip()`
}
func DebugDrawCursorPos() {
	_ = `ImGui::DebugDrawCursorPos()`
}
func DebugDrawCursorPosV(col uint32 /* = IM_COL32(255, 0, 0, 255)*/) {
	_ = `ImGui::DebugDrawCursorPos(col)`
}
func DebugDrawLineExtents() {
	_ = `ImGui::DebugDrawLineExtents()`
}
func DebugDrawLineExtentsV(col uint32 /* = IM_COL32(255, 0, 0, 255)*/) {
	_ = `ImGui::DebugDrawLineExtents(col)`
}
func DebugDrawItemRect() {
	_ = `ImGui::DebugDrawItemRect()`
}
func DebugDrawItemRectV(col uint32 /* = IM_COL32(255, 0, 0, 255)*/) {
	_ = `ImGui::DebugDrawItemRect(col)`
}

// DebugLocateItem Call sparingly: only 1 at the same time!
func DebugLocateItem(target_id ImGuiID) {
	_ = `ImGui::DebugLocateItem(target_id)`
}

// DebugLocateItemOnHover Only call on reaction to a mouse Hover: because only 1 at the same time!
func DebugLocateItemOnHover(target_id ImGuiID) {
	_ = `ImGui::DebugLocateItemOnHover(target_id)`
}
func DebugLocateItemResolveWithLastItem() {
	_ = `ImGui::DebugLocateItemResolveWithLastItem()`
}
func DebugBreakClearData() {
	_ = `ImGui::DebugBreakClearData()`
}
func DebugBreakButton(label string, description_of_location string) (r bool) {
	_ = `auto r = ImGui::DebugBreakButton(label, description_of_location)`
	return
}
func DebugBreakButtonTooltip(keyboard_only bool, description_of_location string) {
	_ = `ImGui::DebugBreakButtonTooltip(keyboard_only, description_of_location)`
}
func DebugRenderKeyboardPreview(draw_list ImDrawListPtr) {
	_ = `ImGui::DebugRenderKeyboardPreview((ImDrawList *)draw_list)`
}
