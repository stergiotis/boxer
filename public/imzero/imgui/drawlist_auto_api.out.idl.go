//go:build fffi_idl_code

package imgui

// PushClipRect Render-level scissoring. This is passed down to your render function but not used for CPU-side coarse clipping. Prefer using higher-level ImGui::PushClipRect() to affect logic (hit-testing and widget culling)
func (foreignptr ImDrawListPtr) PushClipRect(clip_rect_min ImVec2, clip_rect_max ImVec2) {
	_ = `((ImDrawList*)foreignptr)->PushClipRect(clip_rect_min, clip_rect_max)`
}

// PushClipRectV Render-level scissoring. This is passed down to your render function but not used for CPU-side coarse clipping. Prefer using higher-level ImGui::PushClipRect() to affect logic (hit-testing and widget culling)
// * intersect_with_current_clip_rect bool = false
func (foreignptr ImDrawListPtr) PushClipRectV(clip_rect_min ImVec2, clip_rect_max ImVec2, intersect_with_current_clip_rect bool /* = false*/) {
	_ = `((ImDrawList*)foreignptr)->PushClipRect(clip_rect_min, clip_rect_max, intersect_with_current_clip_rect)`
}
func (foreignptr ImDrawListPtr) PushClipRectFullScreen() {
	_ = `((ImDrawList*)foreignptr)->PushClipRectFullScreen()`
}
func (foreignptr ImDrawListPtr) PopClipRect() {
	_ = `((ImDrawList*)foreignptr)->PopClipRect()`
}
func (foreignptr ImDrawListPtr) PushTextureID(texture_id ImTextureID) {
	_ = `((ImDrawList*)foreignptr)->PushTextureID(ImTextureID(texture_id))`
}
func (foreignptr ImDrawListPtr) PopTextureID() {
	_ = `((ImDrawList*)foreignptr)->PopTextureID()`
}
func (foreignptr ImDrawListPtr) AddLine(p1 ImVec2, p2 ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddLine(p1, p2, col)`
}
func (foreignptr ImDrawListPtr) AddLineV(p1 ImVec2, p2 ImVec2, col uint32, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddLine(p1, p2, col, thickness)`
}

// AddRect a: upper-left, b: lower-right (== upper-left + size)
func (foreignptr ImDrawListPtr) AddRect(p_min ImVec2, p_max ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddRect(p_min, p_max, col)`
}

// AddRectV a: upper-left, b: lower-right (== upper-left + size)
// * rounding float = 0.0f
// * flags ImDrawFlags = 0
// * thickness float = 1.0f
func (foreignptr ImDrawListPtr) AddRectV(p_min ImVec2, p_max ImVec2, col uint32, rounding float32 /* = 0.0f*/, flags ImDrawFlags /* = 0*/, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddRect(p_min, p_max, col, rounding, flags, thickness)`
}

// AddRectFilled a: upper-left, b: lower-right (== upper-left + size)
func (foreignptr ImDrawListPtr) AddRectFilled(p_min ImVec2, p_max ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddRectFilled(p_min, p_max, col)`
}

// AddRectFilledV a: upper-left, b: lower-right (== upper-left + size)
// * rounding float = 0.0f
// * flags ImDrawFlags = 0
func (foreignptr ImDrawListPtr) AddRectFilledV(p_min ImVec2, p_max ImVec2, col uint32, rounding float32 /* = 0.0f*/, flags ImDrawFlags /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->AddRectFilled(p_min, p_max, col, rounding, flags)`
}
func (foreignptr ImDrawListPtr) AddRectFilledMultiColor(p_min ImVec2, p_max ImVec2, col_upr_left uint32, col_upr_right uint32, col_bot_right uint32, col_bot_left uint32) {
	_ = `((ImDrawList*)foreignptr)->AddRectFilledMultiColor(p_min, p_max, col_upr_left, col_upr_right, col_bot_right, col_bot_left)`
}
func (foreignptr ImDrawListPtr) AddQuad(p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddQuad(p1, p2, p3, p4, col)`
}
func (foreignptr ImDrawListPtr) AddQuadV(p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2, col uint32, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddQuad(p1, p2, p3, p4, col, thickness)`
}
func (foreignptr ImDrawListPtr) AddQuadFilled(p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddQuadFilled(p1, p2, p3, p4, col)`
}
func (foreignptr ImDrawListPtr) AddTriangle(p1 ImVec2, p2 ImVec2, p3 ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddTriangle(p1, p2, p3, col)`
}
func (foreignptr ImDrawListPtr) AddTriangleV(p1 ImVec2, p2 ImVec2, p3 ImVec2, col uint32, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddTriangle(p1, p2, p3, col, thickness)`
}
func (foreignptr ImDrawListPtr) AddTriangleFilled(p1 ImVec2, p2 ImVec2, p3 ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddTriangleFilled(p1, p2, p3, col)`
}
func (foreignptr ImDrawListPtr) AddCircle(center ImVec2, radius float32, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddCircle(center, radius, col)`
}
func (foreignptr ImDrawListPtr) AddCircleV(center ImVec2, radius float32, col uint32, num_segments int /* = 0*/, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddCircle(center, radius, col, num_segments, thickness)`
}
func (foreignptr ImDrawListPtr) AddCircleFilled(center ImVec2, radius float32, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddCircleFilled(center, radius, col)`
}
func (foreignptr ImDrawListPtr) AddCircleFilledV(center ImVec2, radius float32, col uint32, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->AddCircleFilled(center, radius, col, num_segments)`
}
func (foreignptr ImDrawListPtr) AddNgon(center ImVec2, radius float32, col uint32, num_segments int) {
	_ = `((ImDrawList*)foreignptr)->AddNgon(center, radius, col, num_segments)`
}
func (foreignptr ImDrawListPtr) AddNgonV(center ImVec2, radius float32, col uint32, num_segments int, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddNgon(center, radius, col, num_segments, thickness)`
}
func (foreignptr ImDrawListPtr) AddNgonFilled(center ImVec2, radius float32, col uint32, num_segments int) {
	_ = `((ImDrawList*)foreignptr)->AddNgonFilled(center, radius, col, num_segments)`
}
func (foreignptr ImDrawListPtr) AddEllipse(center ImVec2, radius_x float32, radius_y float32, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddEllipse(center, radius_x, radius_y, col)`
}
func (foreignptr ImDrawListPtr) AddEllipseV(center ImVec2, radius_x float32, radius_y float32, col uint32, rot float32 /* = 0.0f*/, num_segments int /* = 0*/, thickness float32 /* = 1.0f*/) {
	_ = `((ImDrawList*)foreignptr)->AddEllipse(center, radius_x, radius_y, col, rot, num_segments, thickness)`
}
func (foreignptr ImDrawListPtr) AddEllipseFilled(center ImVec2, radius_x float32, radius_y float32, col uint32) {
	_ = `((ImDrawList*)foreignptr)->AddEllipseFilled(center, radius_x, radius_y, col)`
}
func (foreignptr ImDrawListPtr) AddEllipseFilledV(center ImVec2, radius_x float32, radius_y float32, col uint32, rot float32 /* = 0.0f*/, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->AddEllipseFilled(center, radius_x, radius_y, col, rot, num_segments)`
}

// AddBezierCubic Cubic Bezier (4 control points)
func (foreignptr ImDrawListPtr) AddBezierCubic(p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2, col uint32, thickness float32) {
	_ = `((ImDrawList*)foreignptr)->AddBezierCubic(p1, p2, p3, p4, col, thickness)`
}

// AddBezierCubicV Cubic Bezier (4 control points)
// * num_segments int = 0
func (foreignptr ImDrawListPtr) AddBezierCubicV(p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2, col uint32, thickness float32, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->AddBezierCubic(p1, p2, p3, p4, col, thickness, num_segments)`
}

// AddBezierQuadratic Quadratic Bezier (3 control points)
func (foreignptr ImDrawListPtr) AddBezierQuadratic(p1 ImVec2, p2 ImVec2, p3 ImVec2, col uint32, thickness float32) {
	_ = `((ImDrawList*)foreignptr)->AddBezierQuadratic(p1, p2, p3, col, thickness)`
}

// AddBezierQuadraticV Quadratic Bezier (3 control points)
// * num_segments int = 0
func (foreignptr ImDrawListPtr) AddBezierQuadraticV(p1 ImVec2, p2 ImVec2, p3 ImVec2, col uint32, thickness float32, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->AddBezierQuadratic(p1, p2, p3, col, thickness, num_segments)`
}
func (foreignptr ImDrawListPtr) AddImage(user_texture_id ImTextureID, p_min ImVec2, p_max ImVec2) {
	_ = `((ImDrawList*)foreignptr)->AddImage(ImTextureID(user_texture_id), p_min, p_max)`
}
func (foreignptr ImDrawListPtr) AddImageV(user_texture_id ImTextureID, p_min ImVec2, p_max ImVec2, uv_min ImVec2 /* = ImVec2(0, 0)*/, uv_max ImVec2 /* = ImVec2(1, 1)*/, col uint32 /* = IM_COL32_WHITE*/) {
	_ = `((ImDrawList*)foreignptr)->AddImage(ImTextureID(user_texture_id), p_min, p_max, uv_min, uv_max, col)`
}
func (foreignptr ImDrawListPtr) AddImageQuad(user_texture_id ImTextureID, p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2) {
	_ = `((ImDrawList*)foreignptr)->AddImageQuad(ImTextureID(user_texture_id), p1, p2, p3, p4)`
}
func (foreignptr ImDrawListPtr) AddImageQuadV(user_texture_id ImTextureID, p1 ImVec2, p2 ImVec2, p3 ImVec2, p4 ImVec2, uv1 ImVec2 /* = ImVec2(0, 0)*/, uv2 ImVec2 /* = ImVec2(1, 0)*/, uv3 ImVec2 /* = ImVec2(1, 1)*/, uv4 ImVec2 /* = ImVec2(0, 1)*/, col uint32 /* = IM_COL32_WHITE*/) {
	_ = `((ImDrawList*)foreignptr)->AddImageQuad(ImTextureID(user_texture_id), p1, p2, p3, p4, uv1, uv2, uv3, uv4, col)`
}
func (foreignptr ImDrawListPtr) AddImageRounded(user_texture_id ImTextureID, p_min ImVec2, p_max ImVec2, uv_min ImVec2, uv_max ImVec2, col uint32, rounding float32) {
	_ = `((ImDrawList*)foreignptr)->AddImageRounded(ImTextureID(user_texture_id), p_min, p_max, uv_min, uv_max, col, rounding)`
}
func (foreignptr ImDrawListPtr) AddImageRoundedV(user_texture_id ImTextureID, p_min ImVec2, p_max ImVec2, uv_min ImVec2, uv_max ImVec2, col uint32, rounding float32, flags ImDrawFlags /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->AddImageRounded(ImTextureID(user_texture_id), p_min, p_max, uv_min, uv_max, col, rounding, flags)`
}
func (foreignptr ImDrawListPtr) PathArcTo(center ImVec2, radius float32, a_min float32, a_max float32) {
	_ = `((ImDrawList*)foreignptr)->PathArcTo(center, radius, a_min, a_max)`
}
func (foreignptr ImDrawListPtr) PathArcToV(center ImVec2, radius float32, a_min float32, a_max float32, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->PathArcTo(center, radius, a_min, a_max, num_segments)`
}

// PathArcToFast Use precomputed angles for a 12 steps circle
func (foreignptr ImDrawListPtr) PathArcToFast(center ImVec2, radius float32, a_min_of_12 int, a_max_of_12 int) {
	_ = `((ImDrawList*)foreignptr)->PathArcToFast(center, radius, a_min_of_12, a_max_of_12)`
}

// PathEllipticalArcTo Ellipse
func (foreignptr ImDrawListPtr) PathEllipticalArcTo(center ImVec2, radius_x float32, radius_y float32, rot float32, a_min float32, a_max float32) {
	_ = `((ImDrawList*)foreignptr)->PathEllipticalArcTo(center, radius_x, radius_y, rot, a_min, a_max)`
}

// PathEllipticalArcToV Ellipse
// * num_segments int = 0
func (foreignptr ImDrawListPtr) PathEllipticalArcToV(center ImVec2, radius_x float32, radius_y float32, rot float32, a_min float32, a_max float32, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->PathEllipticalArcTo(center, radius_x, radius_y, rot, a_min, a_max, num_segments)`
}

// PathBezierCubicCurveTo Cubic Bezier (4 control points)
func (foreignptr ImDrawListPtr) PathBezierCubicCurveTo(p2 ImVec2, p3 ImVec2, p4 ImVec2) {
	_ = `((ImDrawList*)foreignptr)->PathBezierCubicCurveTo(p2, p3, p4)`
}

// PathBezierCubicCurveToV Cubic Bezier (4 control points)
// * num_segments int = 0
func (foreignptr ImDrawListPtr) PathBezierCubicCurveToV(p2 ImVec2, p3 ImVec2, p4 ImVec2, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->PathBezierCubicCurveTo(p2, p3, p4, num_segments)`
}

// PathBezierQuadraticCurveTo Quadratic Bezier (3 control points)
func (foreignptr ImDrawListPtr) PathBezierQuadraticCurveTo(p2 ImVec2, p3 ImVec2) {
	_ = `((ImDrawList*)foreignptr)->PathBezierQuadraticCurveTo(p2, p3)`
}

// PathBezierQuadraticCurveToV Quadratic Bezier (3 control points)
// * num_segments int = 0
func (foreignptr ImDrawListPtr) PathBezierQuadraticCurveToV(p2 ImVec2, p3 ImVec2, num_segments int /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->PathBezierQuadraticCurveTo(p2, p3, num_segments)`
}
func (foreignptr ImDrawListPtr) PathRect(rect_min ImVec2, rect_max ImVec2) {
	_ = `((ImDrawList*)foreignptr)->PathRect(rect_min, rect_max)`
}
func (foreignptr ImDrawListPtr) PathRectV(rect_min ImVec2, rect_max ImVec2, rounding float32 /* = 0.0f*/, flags ImDrawFlags /* = 0*/) {
	_ = `((ImDrawList*)foreignptr)->PathRect(rect_min, rect_max, rounding, flags)`
}

// AddDrawCmd This is useful if you need to forcefully create a new draw call (to allow for dependent rendering / blending). Otherwise primitives are merged into the same draw-call as much as possible
func (foreignptr ImDrawListPtr) AddDrawCmd() {
	_ = `((ImDrawList*)foreignptr)->AddDrawCmd()`
}

// CloneOutput Create a clone of the CmdBuffer/IdxBuffer/VtxBuffer.
func (foreignptr ImDrawListPtr) CloneOutput() (r ImDrawListPtr) {
	_ = `auto r = ((ImDrawList*)foreignptr)->CloneOutput()`
	return
}
func (foreignptr ImDrawListPtr) PrimReserve(idx_count int, vtx_count int) {
	_ = `((ImDrawList*)foreignptr)->PrimReserve(idx_count, vtx_count)`
}
func (foreignptr ImDrawListPtr) PrimUnreserve(idx_count int, vtx_count int) {
	_ = `((ImDrawList*)foreignptr)->PrimUnreserve(idx_count, vtx_count)`
}

// PrimRect Axis aligned rectangle (composed of two triangles)
func (foreignptr ImDrawListPtr) PrimRect(a ImVec2, b ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->PrimRect(a, b, col)`
}
func (foreignptr ImDrawListPtr) PrimRectUV(a ImVec2, b ImVec2, uv_a ImVec2, uv_b ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->PrimRectUV(a, b, uv_a, uv_b, col)`
}
func (foreignptr ImDrawListPtr) PrimQuadUV(a ImVec2, b ImVec2, c ImVec2, d ImVec2, uv_a ImVec2, uv_b ImVec2, uv_c ImVec2, uv_d ImVec2, col uint32) {
	_ = `((ImDrawList*)foreignptr)->PrimQuadUV(a, b, c, d, uv_a, uv_b, uv_c, uv_d, col)`
}
func (foreignptr ImDrawListPtr) _ResetForNewFrame() {
	_ = `((ImDrawList*)foreignptr)->_ResetForNewFrame()`
}
func (foreignptr ImDrawListPtr) _ClearFreeMemory() {
	_ = `((ImDrawList*)foreignptr)->_ClearFreeMemory()`
}
func (foreignptr ImDrawListPtr) _PopUnusedDrawCmd() {
	_ = `((ImDrawList*)foreignptr)->_PopUnusedDrawCmd()`
}
func (foreignptr ImDrawListPtr) _TryMergeDrawCmds() {
	_ = `((ImDrawList*)foreignptr)->_TryMergeDrawCmds()`
}
func (foreignptr ImDrawListPtr) _OnChangedClipRect() {
	_ = `((ImDrawList*)foreignptr)->_OnChangedClipRect()`
}
func (foreignptr ImDrawListPtr) _OnChangedTextureID() {
	_ = `((ImDrawList*)foreignptr)->_OnChangedTextureID()`
}
func (foreignptr ImDrawListPtr) _OnChangedVtxOffset() {
	_ = `((ImDrawList*)foreignptr)->_OnChangedVtxOffset()`
}
func (foreignptr ImDrawListPtr) _CalcCircleAutoSegmentCount(radius float32) (r int) {
	_ = `auto r = ((ImDrawList*)foreignptr)->_CalcCircleAutoSegmentCount(radius)`
	return
}
func (foreignptr ImDrawListPtr) _PathArcToFastEx(center ImVec2, radius float32, a_min_sample int, a_max_sample int, a_step int) {
	_ = `((ImDrawList*)foreignptr)->_PathArcToFastEx(center, radius, a_min_sample, a_max_sample, a_step)`
}
func (foreignptr ImDrawListPtr) _PathArcToN(center ImVec2, radius float32, a_min float32, a_max float32, num_segments int) {
	_ = `((ImDrawList*)foreignptr)->_PathArcToN(center, radius, a_min, a_max, num_segments)`
}
