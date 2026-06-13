package definition

// =============================================================================
// EGUI_PLOT binding — register-drain pattern
// =============================================================================
//
// Architecture:
//   1. Go pushes plot elements into Rust-side registers via accumulator opcodes
//   2. Plot drain node reads all registers and renders inside plot.show()
//
// Plot elements: plotLine, plotScatter, plotBars, plotHLine, plotVLine, plotText, plotBoxes
// Drain node: plot
//
// Point data is transmitted as separate xs and ys homogeneous f64 arrays
// using the standard FFFI2 PlainArg(ctabb.F64h) mechanism.
//
// =============================================================================

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// --- Return type structs ---

func structPlotElement() ir.ConcreteType {
	return ir.NewConcreteType("plotElement")
}

func structPlotDrain() ir.ConcreteType {
	return ir.NewConcreteType("plotDrain")
}

// --- Registered nodes (plot element accumulators) ---

func definitionsPlotRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 8)

	// plotLine — line series with separate xs and ys arrays
	registered = append(registered, idl.NewBuilderFactoryNode("plotLine").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("xs", ctabb.F64h).
			PlainArg("ys", ctabb.F64h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("width = wi;\n")).EndMethod().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			BeginMethod("fill").Arg("fy", ctabb.F64).
			CodeClientRust(rustClientCode("fill = Some(fy);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let n = xs.len().min(ys.len());
let mut points: Vec<[f64; 2]> = Vec::with_capacity(n);
for i in 0..n { points.push([xs[i], ys[i]]); }
let mut color: Option<egui::Color32> = None;
let mut width: f32 = 1.0;
let mut highlight = false;
let mut fill: Option<f64> = None;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_lines.push(PlotLineData { name, points, color, width, highlight, fill });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotScatter — scatter/points series
	registered = append(registered, idl.NewBuilderFactoryNode("plotScatter").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("xs", ctabb.F64h).
			PlainArg("ys", ctabb.F64h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("radius").Arg("ra", ctabb.F32).
			CodeClientRust(rustClientCode("radius = ra;\n")).EndMethod().
			BeginMethod("shape").Arg("sa", ctabb.U8).
			CodeClientRust(rustClientCode("shape = sa;\n")).EndMethod().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			BeginMethod("filled").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("filled = val;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let n = xs.len().min(ys.len());
let mut points: Vec<[f64; 2]> = Vec::with_capacity(n);
for i in 0..n { points.push([xs[i], ys[i]]); }
let mut color: Option<egui::Color32> = None;
let mut radius: f32 = 2.0;
let mut shape: u8 = 0;
let mut highlight = false;
let mut filled = true;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_scatters.push(PlotScatterData { name, points, color, radius, shape, highlight, filled });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotBars — bar chart with argument/value arrays
	registered = append(registered, idl.NewBuilderFactoryNode("plotBars").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("arguments", ctabb.F64h).
			PlainArg("values", ctabb.F64h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("width").Arg("wi", ctabb.F64).
			CodeClientRust(rustClientCode("width = wi;\n")).EndMethod().
			BeginMethod("horizontal").
			CodeClientRust(rustClientCode("horizontal = true;\n")).EndMethod().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let n = arguments.len().min(values.len());
let mut bars: Vec<(f64, f64)> = Vec::with_capacity(n);
for i in 0..n { bars.push((arguments[i], values[i])); }
let mut color: Option<egui::Color32> = None;
let mut width: f64 = 0.5;
let mut horizontal = false;
let mut highlight = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_bars.push(PlotBarsData { name, bars, color, width, horizontal, highlight });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotHLine — horizontal reference line
	registered = append(registered, idl.NewBuilderFactoryNode("plotHLine").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("yy", ctabb.F64).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("width = wi;\n")).EndMethod().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut color: Option<egui::Color32> = None;
let mut width: f32 = 1.0;
let mut highlight = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_hlines.push(PlotHLineData { name, y: yy, color, width, highlight });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotVLine — vertical reference line
	registered = append(registered, idl.NewBuilderFactoryNode("plotVLine").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("xx", ctabb.F64).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("width = wi;\n")).EndMethod().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut color: Option<egui::Color32> = None;
let mut width: f32 = 1.0;
let mut highlight = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_vlines.push(PlotVLineData { name, x: xx, color, width, highlight });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotBoxes — letter-value / boxen / boxplot series. One BoxPlot per
	// call with N BoxElems indexed by the parallel arrays. For boxenplot
	// rendering ("no whiskers"), pass whiskerMins == q1s and
	// whiskerMaxes == q3s so the whisker line collapses to zero length.
	// All slices must have the same length; the shortest wins.
	registered = append(registered, idl.NewBuilderFactoryNode("plotBoxes").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("arguments", ctabb.F64h).
			PlainArg("q1s", ctabb.F64h).
			PlainArg("medians", ctabb.F64h).
			PlainArg("q3s", ctabb.F64h).
			PlainArg("whiskerMins", ctabb.F64h).
			PlainArg("whiskerMaxes", ctabb.F64h).
			PlainArg("boxWidths", ctabb.F64h).
			PlainArg("fillColors", ctabb.U32h).
			PlainArg("strokeColors", ctabb.U32h).
			PlainArg("strokeWidths", ctabb.F32h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("horizontal").
			CodeClientRust(rustClientCode("horizontal = true;\n")).EndMethod().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			// allowHover toggles egui_plot's per-BoxElem hover affordance
			// wholesale (label + rulers + highlight). Passing false drops
			// the hovered box from the candidate set entirely (plot.rs:1566
			// filters by `entry.allow_hover()`), so no on_hover branch
			// runs — including the BoxElem::add_shapes(highlighted=true)
			// call that re-paints the box. Most callers prefer
			// suppressElementText, which silences only the auto-label.
			BeginMethod("allowHover").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("allow_hover = val;\n")).EndMethod().
			// suppressElementText silences egui_plot's auto-generated text
			// label ("Max / Upper whisker / Q3 / median / Q1 / Lower
			// whisker / Min") by installing an element_formatter closure
			// that returns an empty String. egui_plot's add_rulers_and_text
			// (items/mod.rs:208) still draws the axis rulers and the
			// caller's highlight (box_plot.rs:183
			// add_shapes(highlighted=true)) is unaffected — only the
			// auto-sized text envelope (the one that clipped at narrow
			// tooltips / windows) is gone. Used by the boxenplot widget
			// in conjunction with its own bottom WriteStatusLine readout.
			BeginMethod("suppressElementText").
			CodeClientRust(rustClientCode("suppress_element_text = true;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let n = arguments.len()
    .min(q1s.len()).min(medians.len()).min(q3s.len())
    .min(whisker_mins.len()).min(whisker_maxes.len())
    .min(box_widths.len()).min(fill_colors.len())
    .min(stroke_colors.len()).min(stroke_widths.len());
let mut boxes: Vec<PlotBoxData> = Vec::with_capacity(n);
for i in 0..n {
    boxes.push(PlotBoxData {
        argument: arguments[i],
        q1: q1s[i],
        median: medians[i],
        q3: q3s[i],
        whisker_min: whisker_mins[i],
        whisker_max: whisker_maxes[i],
        box_width: box_widths[i],
        fill_color: fill_colors[i],
        stroke_color: stroke_colors[i],
        stroke_width: stroke_widths[i],
    });
}
let mut horizontal = false;
let mut highlight = false;
let mut allow_hover = true;
let mut suppress_element_text = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_boxes.push(PlotBoxesData { name, boxes, horizontal, highlight, allow_hover, suppress_element_text });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotPolygon — filled closed-polygon series rendered via
	// egui_plot::Polygon. Use cases: shaded confidence bands, area
	// fills under a curve, any closed-loop region whose interior
	// should be tinted. Points are taken in the order supplied; the
	// polygon is implicitly closed (first vertex repeated by egui_plot).
	// fillColor is RGBA-packed (low byte = alpha); strokeColor / strokeWidth
	// control the outline. Pass strokeWidth = 0 for an unstroked fill.
	registered = append(registered, idl.NewBuilderFactoryNode("plotPolygon").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("xs", ctabb.F64h).
			PlainArg("ys", ctabb.F64h).
			PlainArg("fillColor", ctabb.U32).
			PlainArg("strokeColor", ctabb.U32).
			PlainArg("strokeWidth", ctabb.F32).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("highlight").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("highlight = val;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let n = xs.len().min(ys.len());
let mut points: Vec<[f64; 2]> = Vec::with_capacity(n);
for i in 0..n { points.push([xs[i], ys[i]]); }
let mut highlight = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_polygons.push(PlotPolygonData { name, points, fill_color, stroke_color, stroke_width, highlight });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	// plotText — text annotation at plot coordinates
	registered = append(registered, idl.NewBuilderFactoryNode("plotText").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("name", ctabb.S).
			PlainArg("px", ctabb.F64).
			PlainArg("py", ctabb.F64).
			PlainArg("text", ctabb.S).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut color: Option<egui::Color32> = None;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.plot_texts.push(PlotTextData { name, x: px, y: py, text, color });
`)).
		WithSettingImmediate(true).
		WithReturnType(structPlotElement()).
		Build())

	return registered
}

// --- Drain node (renders plot with all accumulated elements) ---

func definitionsPlotBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 1)

	blocks = append(blocks, idl.NewBuilderFactoryNode("plot").
		WithIdentityId(true).
		WithReturnType(structPlotDrain()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("plot_width = Some(wi);\n")).EndMethod().
			BeginMethod("height").Arg("he", ctabb.F32).
			CodeClientRust(rustClientCode("plot_height = Some(he);\n")).EndMethod().
			BeginMethod("viewAspect").Arg("va", ctabb.F32).
			CodeClientRust(rustClientCode("view_aspect = Some(va);\n")).EndMethod().
			BeginMethod("dataAspect").Arg("da", ctabb.F32).
			CodeClientRust(rustClientCode("data_aspect = Some(da);\n")).EndMethod().
			BeginMethod("xAxisLabel").Arg("label", ctabb.S).
			CodeClientRust(rustClientCode("x_axis_label = Some(label);\n")).EndMethod().
			BeginMethod("yAxisLabel").Arg("label", ctabb.S).
			CodeClientRust(rustClientCode("y_axis_label = Some(label);\n")).EndMethod().
			BeginMethod("legend").
			CodeClientRust(rustClientCode("show_legend = true;\n")).EndMethod().
			BeginMethod("allowZoom").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("allow_zoom = [val, val].into();\n")).EndMethod().
			BeginMethod("allowDrag").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("allow_drag = [val, val].into();\n")).EndMethod().
			BeginMethod("allowScroll").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("allow_scroll = [val, val].into();\n")).EndMethod().
			BeginMethod("allowZoom2").Arg("xa", ctabb.B).Arg("ya", ctabb.B).
			CodeClientRust(rustClientCode("allow_zoom = [xa, ya].into();\n")).EndMethod().
			BeginMethod("allowDrag2").Arg("xa", ctabb.B).Arg("ya", ctabb.B).
			CodeClientRust(rustClientCode("allow_drag = [xa, ya].into();\n")).EndMethod().
			BeginMethod("allowScroll2").Arg("xa", ctabb.B).Arg("ya", ctabb.B).
			CodeClientRust(rustClientCode("allow_scroll = [xa, ya].into();\n")).EndMethod().
			BeginMethod("allowBoxedZoom").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("allow_boxed_zoom = val;\n")).EndMethod().
			BeginMethod("allowDoubleClickReset").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("allow_double_click_reset = val;\n")).EndMethod().
			BeginMethod("showGrid").Arg("gx", ctabb.B).Arg("gy", ctabb.B).
			CodeClientRust(rustClientCode("show_grid_x = gx; show_grid_y = gy;\n")).EndMethod().
			BeginMethod("showAxes").Arg("ax", ctabb.B).Arg("ay", ctabb.B).
			CodeClientRust(rustClientCode("show_axes_x = ax; show_axes_y = ay;\n")).EndMethod().
			BeginMethod("showBackground").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("show_background = val;\n")).EndMethod().
			BeginMethod("includeX").Arg("ix", ctabb.F64).
			CodeClientRust(rustClientCode("include_x.push(ix);\n")).EndMethod().
			BeginMethod("includeY").Arg("iy", ctabb.F64).
			CodeClientRust(rustClientCode("include_y.push(iy);\n")).EndMethod().
			BeginMethod("includeXRange").Arg("lo", ctabb.F64).Arg("hi", ctabb.F64).
			CodeClientRust(rustClientCode("include_x.push(lo); include_x.push(hi);\n")).EndMethod().
			BeginMethod("includeYRange").Arg("lo", ctabb.F64).Arg("hi", ctabb.F64).
			CodeClientRust(rustClientCode("include_y.push(lo); include_y.push(hi);\n")).EndMethod().
			BeginMethod("centerXAxis").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("center_x_axis = val;\n")).EndMethod().
			BeginMethod("centerYAxis").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("center_y_axis = val;\n")).EndMethod().
			BeginMethod("yGridMarks").Arg("values", ctabb.F64h).Arg("labels", ctabb.Sh).
			CodeClientRust(rustClientCode("y_grid_values = values; y_grid_labels = labels;\n")).EndMethod().
			// clampX / clampY cap the visible viewport — useful for ECDF and
			// bounded-domain plots where the reader can easily zoom or pan
			// out into empty space. egui_plot 0.35 has no bounds_modifier
			// builder hook, so the clamp runs inside the show closure via
			// PlotUi::set_plot_bounds_x/y. Setting both ends to the same
			// value (lo == hi) effectively pins the axis. Leave unset for
			// no clamp.
			BeginMethod("clampX").Arg("lo", ctabb.F64).Arg("hi", ctabb.F64).
			CodeClientRust(rustClientCode("clamp_x = Some((lo, hi));\n")).EndMethod().
			BeginMethod("clampY").Arg("lo", ctabb.F64).Arg("hi", ctabb.F64).
			CodeClientRust(rustClientCode("clamp_y = Some((lo, hi));\n")).EndMethod().
			Build()...).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut plot_width: Option<f32> = None;
let mut plot_height: Option<f32> = None;
let mut view_aspect: Option<f32> = None;
let mut data_aspect: Option<f32> = None;
let mut x_axis_label: Option<String> = None;
let mut y_axis_label: Option<String> = None;
let mut show_legend = false;
let mut allow_zoom: egui::Vec2b = [true, true].into();
let mut allow_drag: egui::Vec2b = [true, true].into();
let mut allow_scroll: egui::Vec2b = [true, true].into();
let mut allow_boxed_zoom = true;
let mut allow_double_click_reset = true;
let mut show_grid_x = true;
let mut show_grid_y = true;
let mut show_axes_x = true;
let mut show_axes_y = true;
let mut show_background = true;
let mut include_x: Vec<f64> = Vec::new();
let mut include_y: Vec<f64> = Vec::new();
let mut center_x_axis = false;
let mut center_y_axis = false;
let mut y_grid_values: Vec<f64> = Vec::new();
let mut y_grid_labels: Vec<String> = Vec::new();
let mut clamp_x: Option<(f64, f64)> = None;
let mut clamp_y: Option<(f64, f64)> = None;
`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let mut plot = egui_plot::Plot::new({{Id}});
    if let Some(w) = plot_width { plot = plot.width(w); }
    if let Some(h) = plot_height { plot = plot.height(h); }
    if let Some(a) = view_aspect { plot = plot.view_aspect(a); }
    if let Some(a) = data_aspect { plot = plot.data_aspect(a); }
    if let Some(ref l) = x_axis_label { plot = plot.x_axis_label(l.as_str()); }
    if let Some(ref l) = y_axis_label { plot = plot.y_axis_label(l.as_str()); }
    if show_legend { plot = plot.legend(egui_plot::Legend::default()); }
    plot = plot.allow_zoom(allow_zoom);
    plot = plot.allow_drag(allow_drag);
    plot = plot.allow_scroll(allow_scroll);
    plot = plot.allow_boxed_zoom(allow_boxed_zoom);
    plot = plot.allow_double_click_reset(allow_double_click_reset);
    plot = plot.show_grid([show_grid_x, show_grid_y]);
    plot = plot.show_axes([show_axes_x, show_axes_y]);
    plot = plot.show_background(show_background);
    for &x in &include_x { plot = plot.include_x(x); }
    for &y in &include_y { plot = plot.include_y(y); }
    if center_x_axis { plot = plot.center_x_axis(true); }
    if center_y_axis { plot = plot.center_y_axis(true); }
    if !y_grid_values.is_empty() {
        let positions = y_grid_values.clone();
        let labels = y_grid_labels.clone();
        let step = if positions.len() >= 2 { (positions[1] - positions[0]).abs() } else { 1.0 };
        let positions_for_spacer = positions.clone();
        plot = plot.y_grid_spacer(move |_input: egui_plot::GridInput| -> Vec<egui_plot::GridMark> {
            positions_for_spacer
                .iter()
                .map(|&v| egui_plot::GridMark { value: v, step_size: step })
                .collect()
        });
        plot = plot.y_axis_formatter(move |gm: egui_plot::GridMark, _range: &std::ops::RangeInclusive<f64>| -> String {
            for (i, &v) in positions.iter().enumerate() {
                if (gm.value - v).abs() < step * 1e-3 && i < labels.len() {
                    return labels[i].clone();
                }
            }
            format!("{}", gm.value)
        });
    }
    let lines: Vec<PlotLineData> = self.plot_lines.drain(..).collect();
    let scatters: Vec<PlotScatterData> = self.plot_scatters.drain(..).collect();
    let bars_data: Vec<PlotBarsData> = self.plot_bars.drain(..).collect();
    let hlines: Vec<PlotHLineData> = self.plot_hlines.drain(..).collect();
    let vlines: Vec<PlotVLineData> = self.plot_vlines.drain(..).collect();
    let texts: Vec<PlotTextData> = self.plot_texts.drain(..).collect();
    let boxes_series: Vec<PlotBoxesData> = self.plot_boxes.drain(..).collect();
    let polygons_series: Vec<PlotPolygonData> = self.plot_polygons.drain(..).collect();
    let plot_response = plot.show(ui, |plot_ui| {
        // Viewport clamping. Runs first so subsequent primitives are
        // rendered against the clamped bounds and the reader cannot
        // momentarily glimpse the unbounded view. egui_plot 0.35 has
        // no bounds_modifier builder hook, so we read the current
        // bounds and call set_plot_bounds_x/y only when the user has
        // strayed past the cap. Unset (None) means no clamp on that
        // axis — leaves egui_plot's native pan/zoom behaviour intact.
        if clamp_x.is_some() || clamp_y.is_some() {
            let cur = plot_ui.plot_bounds();
            let cx = cur.range_x();
            let cy = cur.range_y();
            let (mut nxlo, mut nxhi) = (*cx.start(), *cx.end());
            let (mut nylo, mut nyhi) = (*cy.start(), *cy.end());
            let mut changed_x = false;
            let mut changed_y = false;
            if let Some((lo, hi)) = clamp_x {
                if nxlo < lo { nxlo = lo; changed_x = true; }
                if nxhi > hi { nxhi = hi; changed_x = true; }
                // Anti-inversion: if the user has panned the viewport
                // completely past the clamp range, naive per-side
                // clamping produces nxlo > nxhi (an inverted axis).
                // Fall back to the full clamp range.
                if changed_x && nxlo > nxhi { nxlo = lo; nxhi = hi; }
            }
            if let Some((lo, hi)) = clamp_y {
                if nylo < lo { nylo = lo; changed_y = true; }
                if nyhi > hi { nyhi = hi; changed_y = true; }
                if changed_y && nylo > nyhi { nylo = lo; nyhi = hi; }
            }
            if changed_x { plot_ui.set_plot_bounds_x(nxlo..=nxhi); }
            if changed_y { plot_ui.set_plot_bounds_y(nylo..=nyhi); }
        }
        // Polygons render first so other primitives (lines, scatter,
        // text) sit on top — required for ECDF + confidence-band
        // composition where the band is a fill and the curve overlays.
        for pg in &polygons_series {
            let pts: egui_plot::PlotPoints = pg.points.iter().copied().collect();
            let mut poly = egui_plot::Polygon::new(&pg.name, pts)
                .fill_color(color32_from_rgba_u32(pg.fill_color));
            // egui interprets Stroke::new(0, _) as a hairline, not "no
            // stroke" — every polygon segment then renders as a visible
            // line, creating a dense hatched appearance on staircase
            // bands. Use Stroke::NONE when the caller asks for width 0.
            if pg.stroke_width > 0.0 {
                poly = poly.stroke(egui::Stroke::new(pg.stroke_width, color32_from_rgba_u32(pg.stroke_color)));
            } else {
                poly = poly.stroke(egui::Stroke::NONE);
            }
            if pg.highlight { poly = poly.highlight(true); }
            plot_ui.polygon(poly);
        }
        for ld in &lines {
            let pts: egui_plot::PlotPoints = ld.points.iter().copied().collect();
            let mut line = egui_plot::Line::new(&ld.name, pts).width(ld.width);
            if let Some(c) = ld.color { line = line.color(c); }
            if ld.highlight { line = line.highlight(true); }
            if let Some(f) = ld.fill { line = line.fill(f as f32); }
            plot_ui.line(line);
        }
        for sd in &scatters {
            let pts: egui_plot::PlotPoints = sd.points.iter().copied().collect();
            let shape = match sd.shape {
                0 => egui_plot::MarkerShape::Circle, 1 => egui_plot::MarkerShape::Diamond,
                2 => egui_plot::MarkerShape::Square, 3 => egui_plot::MarkerShape::Cross,
                4 => egui_plot::MarkerShape::Plus, 5 => egui_plot::MarkerShape::Up,
                6 => egui_plot::MarkerShape::Down, 7 => egui_plot::MarkerShape::Left,
                8 => egui_plot::MarkerShape::Right, 9 => egui_plot::MarkerShape::Asterisk,
                _ => egui_plot::MarkerShape::Circle,
            };
            let mut p = egui_plot::Points::new(&sd.name, pts).radius(sd.radius).shape(shape).filled(sd.filled);
            if let Some(c) = sd.color { p = p.color(c); }
            if sd.highlight { p = p.highlight(true); }
            plot_ui.points(p);
        }
        for bd in &bars_data {
            let bv: Vec<egui_plot::Bar> = bd.bars.iter().map(|(a, v)| {
                let mut b = egui_plot::Bar::new(*a, *v);
                if bd.horizontal { b = b.horizontal(); }
                b
            }).collect();
            let mut chart = egui_plot::BarChart::new(&bd.name, bv).width(bd.width);
            if let Some(c) = bd.color { chart = chart.color(c); }
            if bd.highlight { chart = chart.highlight(true); }
            plot_ui.bar_chart(chart);
        }
        for hl in &hlines {
            let mut l = egui_plot::HLine::new(&hl.name, hl.y).width(hl.width);
            if let Some(c) = hl.color { l = l.color(c); }
            if hl.highlight { l = l.highlight(true); }
            plot_ui.hline(l);
        }
        for vl in &vlines {
            let mut l = egui_plot::VLine::new(&vl.name, vl.x).width(vl.width);
            if let Some(c) = vl.color { l = l.color(c); }
            if vl.highlight { l = l.highlight(true); }
            plot_ui.vline(l);
        }
        for t in &texts {
            let mut txt = egui_plot::Text::new(&t.name, egui_plot::PlotPoint::new(t.x, t.y), &t.text);
            if let Some(c) = t.color { txt = txt.color(c); }
            plot_ui.text(txt);
        }
        for bs in &boxes_series {
            let elems: Vec<egui_plot::BoxElem> = bs.boxes.iter().map(|b| {
                let spread = egui_plot::BoxSpread::new(
                    b.whisker_min, b.q1, b.median, b.q3, b.whisker_max,
                );
                egui_plot::BoxElem::new(b.argument, spread)
                    .box_width(b.box_width)
                    .fill(color32_from_rgba_u32(b.fill_color))
                    .stroke(egui::Stroke::new(
                        b.stroke_width,
                        color32_from_rgba_u32(b.stroke_color),
                    ))
            }).collect();
            let mut bp = egui_plot::BoxPlot::new(&bs.name, elems);
            if bs.horizontal { bp = bp.horizontal(); }
            if bs.highlight { bp = bp.highlight(true); }
            bp = bp.allow_hover(bs.allow_hover);
            if bs.suppress_element_text {
                // Empty formatter → text="" in add_rulers_and_text;
                // rulers + on-hover highlight still draw (see egui_plot
                // 0.35 items/mod.rs:208–266 + items/box_plot.rs:172–185).
                bp = bp.element_formatter(Box::new(|_, _| String::new()));
            }
            plot_ui.box_plot(bp);
        }
    });
    // Capture primary-click in plot-data coords. interact_pointer_pos is
    // the screen-space pointer at click time; PlotResponse.transform maps
    // it back to plot-data coords. Single-slot register: the latest click
    // wins; the fetcher (fetchR15PlotPointer) consumes it.
    if plot_response.response.clicked() {
        if let Some(pos) = plot_response.response.interact_pointer_pos() {
            let pp = plot_response.transform.value_from_position(pos);
            self.r15_plot_clicked_id = {{Id}}.value();
            self.r15_plot_clicked_x = pp.x;
            self.r15_plot_clicked_y = pp.y;
            self.r15_plot_clicked = true;
        }
    }
    // Hover state — non-consuming companion to the click capture above.
    // Each plot updates r15_plot_hover_* only if its own response.hover_pos
    // is Some, or if it previously held the hover and the cursor has left;
    // this keeps multi-plot frames stable (plot B's later render does not
    // blank plot A's hover when the cursor lives over A).
    if let Some(pos) = plot_response.response.hover_pos() {
        let pp = plot_response.transform.value_from_position(pos);
        self.r15_plot_hover_id = {{Id}}.value();
        self.r15_plot_hover_x = pp.x;
        self.r15_plot_hover_y = pp.y;
    } else if self.r15_plot_hover_id == {{Id}}.value() {
        self.r15_plot_hover_x = f64::NAN;
        self.r15_plot_hover_y = f64::NAN;
    }
} else {
    self.plot_lines.clear();
    self.plot_scatters.clear();
    self.plot_bars.clear();
    self.plot_hlines.clear();
    self.plot_vlines.clear();
    self.plot_texts.clear();
    self.plot_boxes.clear();
    self.plot_polygons.clear();
}
`)).
		Build())

	return blocks
}
