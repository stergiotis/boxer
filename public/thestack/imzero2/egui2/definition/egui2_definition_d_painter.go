package definition

// =============================================================================
// PAINTER binding — register-drain pattern with relative coordinates
// =============================================================================
//
// Architecture:
//   1. Go pushes drawing commands into Rust-side registers via accumulator opcodes
//   2. paintCanvas drain node calls ui.allocate_painter(size, sense)
//   3. All accumulated commands are rendered with coords offset by rect.min
//
// All coordinates are RELATIVE to the canvas origin (0,0) = top-left of
// the allocated space. Rust translates to screen coords at render time.
// Font sizes are in logical points (same as egui's FontId::size).
//
// =============================================================================

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func structPaintCmd() ir.ConcreteType {
	return ir.NewConcreteType("paintCmd")
}

func structPaintCanvas() ir.ConcreteType {
	return ir.NewConcreteType("paintCanvas")
}

// --- Drawing command accumulators ---

func definitionsPainterRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 8)

	// paintCircleFilled — filled circle
	registered = append(registered, idl.NewBuilderFactoryNode("paintCircleFilled").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("cx", ctabb.F32).
			PlainArg("cy", ctabb.F32).
			PlainArg("radius", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::CircleFilled { cx, cy, radius, fill: color32_from_rgba_u32(col) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintCircleStroke — stroked circle
	registered = append(registered, idl.NewBuilderFactoryNode("paintCircleStroke").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("cx", ctabb.F32).
			PlainArg("cy", ctabb.F32).
			PlainArg("radius", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::CircleStroke { cx, cy, radius, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintRectFilled — filled rectangle with rounding
	registered = append(registered, idl.NewBuilderFactoryNode("paintRectFilled").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("minX", ctabb.F32).
			PlainArg("minY", ctabb.F32).
			PlainArg("maxX", ctabb.F32).
			PlainArg("maxY", ctabb.F32).
			PlainArg("rounding", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::RectFilled { min_x, min_y, max_x, max_y, rounding, fill: color32_from_rgba_u32(col) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintRectStroke — stroked rectangle with rounding
	registered = append(registered, idl.NewBuilderFactoryNode("paintRectStroke").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("minX", ctabb.F32).
			PlainArg("minY", ctabb.F32).
			PlainArg("maxX", ctabb.F32).
			PlainArg("maxY", ctabb.F32).
			PlainArg("rounding", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::RectStroke { min_x, min_y, max_x, max_y, rounding, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintLine — line segment between two points
	registered = append(registered, idl.NewBuilderFactoryNode("paintLine").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("fromX", ctabb.F32).
			PlainArg("fromY", ctabb.F32).
			PlainArg("toX", ctabb.F32).
			PlainArg("toY", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::Line { from_x, from_y, to_x, to_y, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintDashedLine — dashed line segment between two points. dashLen and
	// gapLen are in pixels; egui's epaint::Shape::dashed_line decomposes
	// the input segment into multiple shapes Rust-side, so the wire cost
	// is one opcode regardless of segment count — much cheaper than the
	// Go-side simulation (multiple paintLine calls) when the per-canvas
	// dashed-line count is high (timeline annotations, plot grids, etc.).
	registered = append(registered, idl.NewBuilderFactoryNode("paintDashedLine").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("fromX", ctabb.F32).
			PlainArg("fromY", ctabb.F32).
			PlainArg("toX", ctabb.F32).
			PlainArg("toY", ctabb.F32).
			PlainArg("dashLen", ctabb.F32).
			PlainArg("gapLen", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::DashedLine { from_x, from_y, to_x, to_y, dash_len, gap_len, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintArrow — arrow from origin in direction (dx, dy)
	registered = append(registered, idl.NewBuilderFactoryNode("paintArrow").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("ox", ctabb.F32).
			PlainArg("oy", ctabb.F32).
			PlainArg("dx", ctabb.F32).
			PlainArg("dy", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::Arrow { ox, oy, dx, dy, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintText — text at position with anchor alignment
	// anchorH: 0=left, 1=center, 2=right
	// anchorV: 0=top, 1=center, 2=bottom
	registered = append(registered, idl.NewBuilderFactoryNode("paintText").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("px", ctabb.F32).
			PlainArg("py", ctabb.F32).
			PlainArg("anchorH", ctabb.U8).
			PlainArg("anchorV", ctabb.U8).
			PlainArg("text", ctabb.S).
			PlainArg("fontSize", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("monospace").
			CodeClientRust(rustClientCode("monospace = true;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut monospace = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::Text { px, py, anchor_h, anchor_v, text, font_size, color: color32_from_rgba_u32(col), monospace });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintPolyline — multi-point connected line
	registered = append(registered, idl.NewBuilderFactoryNode("paintPolyline").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("xs", ctabb.F32h).
			PlainArg("ys", ctabb.F32h).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`{
let n = xs.len().min(ys.len());
let mut points: Vec<[f32; 2]> = Vec::with_capacity(n);
for i in 0..n { points.push([xs[i], ys[i]]); }
self.paint_cmds.push(PaintCmd::Polyline { points, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
}
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintCubicBezier — cubic bezier curve (4 control points: start, cp1, cp2, end)
	registered = append(registered, idl.NewBuilderFactoryNode("paintCubicBezier").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("startX", ctabb.F32).
			PlainArg("startY", ctabb.F32).
			PlainArg("cp1x", ctabb.F32).
			PlainArg("cp1y", ctabb.F32).
			PlainArg("cp2x", ctabb.F32).
			PlainArg("cp2y", ctabb.F32).
			PlainArg("endX", ctabb.F32).
			PlainArg("endY", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::CubicBezier { x0: start_x, y0: start_y, x1: cp1x, y1: cp1y, x2: cp2x, y2: cp2y, x3: end_x, y3: end_y, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintPolygonFilled — filled convex polygon (e.g. solid arrow heads)
	registered = append(registered, idl.NewBuilderFactoryNode("paintPolygonFilled").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("xs", ctabb.F32h).
			PlainArg("ys", ctabb.F32h).
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`{
let n = xs.len().min(ys.len());
let mut points: Vec<[f32; 2]> = Vec::with_capacity(n);
for i in 0..n { points.push([xs[i], ys[i]]); }
self.paint_cmds.push(PaintCmd::PolygonFilled { points, fill: color32_from_rgba_u32(col) });
}
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintEllipseFilled — filled ellipse (rx, ry are the half-width / half-height)
	registered = append(registered, idl.NewBuilderFactoryNode("paintEllipseFilled").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("cx", ctabb.F32).
			PlainArg("cy", ctabb.F32).
			PlainArg("rx", ctabb.F32).
			PlainArg("ry", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::EllipseFilled { cx, cy, rx, ry, fill: color32_from_rgba_u32(col) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintEllipseStroke — stroked ellipse
	registered = append(registered, idl.NewBuilderFactoryNode("paintEllipseStroke").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("cx", ctabb.F32).
			PlainArg("cy", ctabb.F32).
			PlainArg("rx", ctabb.F32).
			PlainArg("ry", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::EllipseStroke { cx, cy, rx, ry, stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)) });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintSenseRegion — invisible interaction region, drained by PaintCanvas
	registered = append(registered, idl.NewBuilderFactoryNode("paintSenseRegion").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("px", ctabb.F32).
			PlainArg("py", ctabb.F32).
			PlainArg("sw", ctabb.F32).
			PlainArg("sh", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::SenseRegion { id: {{Id}}, px, py, sw, sh });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	return registered
}

// --- Canvas drain node ---

func definitionsPainterBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 1)

	blocks = append(blocks, idl.NewBuilderFactoryNode("paintCanvas").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("canvasWidth", ctabb.F32).
			PlainArg("canvasHeight", ctabb.F32).
			Build()).
		WithReturnType(structPaintCanvas()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("background").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("bg_color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("opacity").Arg("op", ctabb.F32).
			CodeClientRust(rustClientCode("opacity = Some(op);\n")).EndMethod().
			BeginMethod("sense").Arg("click", ctabb.B).Arg("drag", ctabb.B).Arg("hover", ctabb.B).
			CodeClientRust(rustClientCode("sense_click = click; sense_drag = drag; sense_hover = hover;\n")).EndMethod().
			// ADR-0140: opt in to owning the wheel while the pointer is over this
			// canvas. captureZoom is pure per-id read (no global mutation, since
			// only canvas widgets read zoom_delta); captureScroll additionally
			// zeroes the global smooth_scroll_delta so egui-native ScrollAreas /
			// later readers this frame do not also scroll. Read back per canvas id
			// via StateManager.GetCanvasWheel; needs a sense that yields a response
			// (Sense.hover() is enough for contains_pointer()).
			BeginMethod("captureZoom").
			CodeClientRust(rustClientCode("capture_zoom = true;\n")).EndMethod().
			BeginMethod("captureScroll").
			CodeClientRust(rustClientCode("capture_scroll = true;\n")).EndMethod().
			Build()...).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut bg_color: Option<egui::Color32> = None;
let mut opacity: Option<f32> = None;
let mut sense_click = false;
let mut sense_drag = false;
let mut sense_hover = false;
let mut capture_zoom = false;
let mut capture_scroll = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let desired = egui::Vec2::new(canvas_width, canvas_height);
    let mut sense = egui::Sense::empty();
    if sense_click { sense = sense.union(egui::Sense::click()); }
    if sense_drag { sense = sense.union(egui::Sense::drag()); }
    if sense_hover { sense = sense.union(egui::Sense::hover()); }
    let (resp, mut painter) = ui.allocate_painter(desired, sense);
    let origin = resp.rect.min;
    self.r14_canvas_origin_x = origin.x;
    self.r14_canvas_origin_y = origin.y;
    self.r14_canvas_clicked = resp.clicked();
    if let Some(hp) = resp.hover_pos() {
        self.r14_canvas_hover_x = hp.x - origin.x;
        self.r14_canvas_hover_y = hp.y - origin.y;
    } else {
        self.r14_canvas_hover_x = f32::NAN;
        self.r14_canvas_hover_y = f32::NAN;
    }
    // ADR-0140 hover-scoped wheel capture: own the wheel only while the pointer
    // is over this canvas (egui's own topmost-under-pointer hit-test). Scroll is
    // consumed (zeroed) so egui-native ScrollAreas and later readers this frame
    // do not also scroll; zoom needs no global mutation. Delivered per canvas id
    // with the canvas-local hover, so the Go side anchors zoom without the racy
    // single-slot global r14 pointer.
    if (capture_zoom || capture_scroll) && resp.contains_pointer() {
        let mut wheel_scroll_x = 0.0f32;
        let mut wheel_scroll_y = 0.0f32;
        let mut wheel_zoom = 1.0f32;
        if capture_zoom {
            wheel_zoom = ui.input(|inp| inp.zoom_delta());
        }
        if capture_scroll {
            let sd = ui.input(|inp| inp.smooth_scroll_delta);
            wheel_scroll_x = sd.x;
            wheel_scroll_y = sd.y;
            ui.input_mut(|inp| inp.smooth_scroll_delta = egui::Vec2::ZERO);
        }
        let hx = self.r14_canvas_hover_x;
        let hy = self.r14_canvas_hover_y;
        self.r23_canvas_wheel_push({{Id}}.value(), wheel_scroll_x, wheel_scroll_y, wheel_zoom, hx, hy);
    }
    if let Some(op) = opacity {
        painter.set_opacity(op);
    }
    if let Some(bg) = bg_color {
        painter.rect_filled(resp.rect, 0.0, bg);
    }
    self.drain_paint_cmds_to_painter(&painter, origin, Some(ui));
    let mut resp_flags = ResponseFlags::empty();
    resp_flags.populate(&resp);
    self.r7_push({{Id}}.value(), resp_flags);
} else {
    self.paint_cmds.clear();
}
`)).
		Build())

	return blocks
}
