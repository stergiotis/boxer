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

	// paintPolygonFilled — filled polygon (e.g. solid arrow heads). Convex by
	// default: egui's native fan-fill, feathered AA, cheapest. Concave() opts
	// into ear-clip tessellation (earcutr → Shape::Mesh) for correct fills of
	// non-convex outlines — a mesh fill bypasses epaint's feathering, so its
	// edge is unantialiased; Stroke() with a hairline of the fill color covers
	// it. Stroke() outlines the closed polygon in either mode (width 0 =
	// default = no outline). Single outer ring only — holes are deferred until
	// a real need (earcut supports them via ring indices).
	registered = append(registered, idl.NewBuilderFactoryNode("paintPolygonFilled").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("xs", ctabb.F32h).
			PlainArg("ys", ctabb.F32h).
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("concave").
			CodeClientRust(rustClientCode("concave = true;\n")).EndMethod().
			BeginMethod("stroke").Arg("sc", ctabb.U32).AsColor().Arg("sw", ctabb.F32).
			CodeClientRust(rustClientCode("stroke_col = sc; stroke_width = sw;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut concave = false;
let mut stroke_col: u32 = 0;
let mut stroke_width: f32 = 0.0;
`)).
		WithApplyCodeClientRust(rustClientCode(`{
let n = xs.len().min(ys.len());
let mut points: Vec<[f32; 2]> = Vec::with_capacity(n);
for i in 0..n { points.push([xs[i], ys[i]]); }
self.paint_cmds.push(PaintCmd::PolygonFilled { points, fill: color32_from_rgba_u32(col), stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(stroke_col)), concave });
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

	// paintClipPush — push a clip rectangle (canvas-relative, like every other
	// paint command). Commands after it render clipped to the intersection of
	// this rect with the clip in effect, until the matching paintClipPop.
	// Pushes nest as a stack; unbalanced pushes end with the canvas (ADR-0149
	// SD3 - the inner plot-area clip: series clipped, tick labels outside).
	registered = append(registered, idl.NewBuilderFactoryNode("paintClipPush").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("minX", ctabb.F32).
			PlainArg("minY", ctabb.F32).
			PlainArg("maxX", ctabb.F32).
			PlainArg("maxY", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::ClipPush { min_x, min_y, max_x, max_y });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintClipPop — restore the clip in effect before the matching
	// paintClipPush. Popping with nothing pushed restores the canvas clip.
	registered = append(registered, idl.NewBuilderFactoryNode("paintClipPop").
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::ClipPop);
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintMarkers — one marker glyph per (xs[i], ys[i]) point, one opcode
	// per series (ADR-0149 SD3: a scatter series costs one opcode, not one
	// per point). shape follows ImPlot's marker numbering so the port maps
	// 1:1 — 0=circle 1=square 2=diamond 3=up 4=down 5=left 6=right 7=cross
	// 8=plus 9=asterisk; anything else falls back to circle. Shapes 0-6 are
	// filled with col; 7-9 are line glyphs stroked with col at weight.
	// radius is the glyph half-extent in pixels.
	registered = append(registered, idl.NewBuilderFactoryNode("paintMarkers").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("xs", ctabb.F32h).
			PlainArg("ys", ctabb.F32h).
			PlainArg("shape", ctabb.U8).
			PlainArg("radius", ctabb.F32).
			PlainArg("col", ctabb.U32).AsColor().
			PlainArg("weight", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::Markers { xs, ys, shape, radius, color: color32_from_rgba_u32(col), weight });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintRectsFilled — one axis-aligned filled rect per index with a
	// per-rect color (ADR-0149 SD3: a small heatmap costs one opcode per
	// grid, not one per cell; SD5 routes large grids to a texture instead).
	// Arrays are truncated to the shortest length Rust-side.
	registered = append(registered, idl.NewBuilderFactoryNode("paintRectsFilled").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("minXs", ctabb.F32h).
			PlainArg("minYs", ctabb.F32h).
			PlainArg("maxXs", ctabb.F32h).
			PlainArg("maxYs", ctabb.F32h).
			PlainArg("cols", ctabb.U32h).AsColors().
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::RectsFilled { min_xs, min_ys, max_xs, max_ys, cols });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPaintCmd()).
		Build())

	// paintImage — a textured rect by image id, clipped like any other paint
	// command (ADR-0149 SD5: the in-plot raster route — large heatmaps, map
	// underlays). Generalizes the mapRaster protocol: pixels are 0xRRGGBBAA
	// row-major (row 0 = top), ship only when contentVersion changes; an
	// unchanged version sends empty pixels and reuses the per-imageId cached
	// texture, re-shipping when the starved report (fetchR22) names the id.
	registered = append(registered, idl.NewBuilderFactoryNode("paintImage").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("imageId", ctabb.U64).
			PlainArg("minX", ctabb.F32).
			PlainArg("minY", ctabb.F32).
			PlainArg("maxX", ctabb.F32).
			PlainArg("maxY", ctabb.F32).
			PlainArg("widthPx", ctabb.U32).
			PlainArg("heightPx", ctabb.U32).
			PlainArg("contentVersion", ctabb.U64).
			PlainArg("pixels", ctabb.U32h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("opacity").Arg("op", ctabb.F32).
			CodeClientRust(rustClientCode("opacity = op;\n")).EndMethod().
			BeginMethod("nearest").Arg("on", ctabb.B).
			CodeClientRust(rustClientCode("nearest = on;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut opacity: f32 = 1.0;
let mut nearest: bool = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.paint_cmds.push(PaintCmd::Image { id: image_id, min_x, min_y, max_x, max_y, width_px, height_px, content_version, pixels, nearest, opacity });
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
    // Canvas-local hover, in canvas coordinates, NaN when the pointer is
    // elsewhere. A LOCAL: its only reader is this canvas's own r23 wheel row
    // below. It used to be written to the r14 register, which was one slot for
    // the whole frame — every canvas overwrote the previous one's, so what a
    // reader got depended on render order, and r14 is retired.
    let (hover_x, hover_y) = match resp.hover_pos() {
        Some(hp) => (hp.x - origin.x, hp.y - origin.y),
        None => (f32::NAN, f32::NAN),
    };
    // ADR-0149 M1: the per-id pointer row (r24), keyed by canvas id.
    // interact_pointer_pos first, so an active drag keeps reporting after
    // the pointer leaves the canvas (edge-crossing pan); hover_pos otherwise.
    {
        let pp = resp.interact_pointer_pos().or_else(|| resp.hover_pos());
        let (ppx, ppy) = match pp {
            Some(p) => (p.x - origin.x, p.y - origin.y),
            None => (f32::NAN, f32::NAN),
        };
        let m = ui.input(|inp| inp.modifiers);
        let mods = (m.shift as u8)
            | ((m.ctrl as u8) << 1)
            | ((m.alt as u8) << 2)
            | ((m.command as u8) << 3);
        self.r24_canvas_pointer_push({{Id}}.value(), origin.x, origin.y, ppx, ppy, mods);
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
        self.r23_canvas_wheel_push({{Id}}.value(), wheel_scroll_x, wheel_scroll_y, wheel_zoom, hover_x, hover_y);
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
