package definition

// =============================================================================
// WALKERS (slippy map) binding — plain widget + register-drain overlays
// =============================================================================
//
// Implements ADR-0056 — slippy map + H3 cell overlays via walkers + h3o/h3o-wazero.
//
// API shape (small by design; heavy lifting stays on Go):
//
//   Widget:    walkersMap(id, initLat, initLon, tileSource, noTiles)
//              .Width / .Height / .SetZoom / .CenterAt
//
//   Overlays   mapMarker(markerId, lat, lon)   .Label/.Color/.Radius
//   (per-     mapPolyline(lats[], lons[])     .Stroke/.Width
//    frame     h3CellsColored(cellIds[], rgbas[]) .StrokeWidth/.StrokeColor
//    register- h3Region(cellIds[])              .Fill/.Stroke/.Width/.Label
//    drain):
//
//   Fetcher:   fetchR15WalkersCamera — viewport bbox + zoom + pointer state
//
// Retained state per widget id lives in ImZeroFffi.walkers_states (HashMap).
// HttpTiles + MapMemory persist across frames keyed by map id — same pattern
// as dock_states / graph_states.
//
// Go owns: H3 computation (via h3o-wazero), drawing state, palettes, scales.
// Rust owns: basemap rendering, overlay painting, viewport reporting,
// H3 outline aggregation cache for h3Region.

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// --- Registered nodes (overlay accumulators, drained by walkersMap) ---

func definitionsWalkersRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 4)

	// mapMarker — one point-of-interest in the next frame's marker list.
	registered = append(registered, idl.NewBuilderFactoryNode("mapMarker").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("markerId", ctabb.U64).
			PlainArg("lat", ctabb.F64).
			PlainArg("lon", ctabb.F64).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("label").Arg("text", ctabb.S).
			CodeClientRust(rustClientCode("label = Some(text);\n")).EndMethod().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("radius").Arg("radius", ctabb.F32).
			CodeClientRust(rustClientCode("radius_px = radius;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut label: Option<String> = None;
let mut color: Option<egui::Color32> = None;
let mut radius_px: f32 = 6.0;
`)).
		WithApplyCodeClientRust(rustClientCode(
			`self.walkers_pending_markers.push(WalkersMarker { id: marker_id, lat, lon, label, color, radius_px });
`)).
		WithSettingImmediate(true).
		WithReturnType(structMapMarker()).
		Build())

	// mapPolyline — a polyline or (if closed via style choice) ring. Lats and
	// lons are parallel homogeneous arrays. Use for routes, tracks, bounding
	// sketches. For ROIs use h3Region instead.
	registered = append(registered, idl.NewBuilderFactoryNode("mapPolyline").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("lats", ctabb.F64h).
			PlainArg("lons", ctabb.F64h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("stroke").Arg("col", ctabb.U32).AsColor().Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("stroke = egui::Stroke::new(width, color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("closed").Arg("closed", ctabb.B).
			CodeClientRust(rustClientCode("closed = closed;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut stroke: egui::Stroke = egui::Stroke::new(2.0, egui::Color32::from_rgb(0x33, 0x88, 0xff));
let mut closed: bool = false;
`)).
		WithApplyCodeClientRust(rustClientCode(
			`self.walkers_pending_polylines.push(WalkersPolyline { lats, lons, stroke, closed });
`)).
		WithSettingImmediate(true).
		WithReturnType(structMapPolyline()).
		Build())

	// h3CellsColored — bulk choropleth layer. Parallel cellIds + rgbas arrays;
	// one opcode per frame for the whole layer (framing overhead stays flat
	// regardless of cell count). Stroke defaults to 0 (no per-cell outline).
	registered = append(registered, idl.NewBuilderFactoryNode("h3CellsColored").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("cellIds", ctabb.U64h).
			PlainArg("cols", ctabb.U32h).AsColors().
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("strokeWidth").Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("stroke_width = width;\n")).EndMethod().
			BeginMethod("strokeColor").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("stroke_color = color32_from_rgba_u32(col);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut stroke_width: f32 = 0.0;
let mut stroke_color: egui::Color32 = egui::Color32::TRANSPARENT;
`)).
		WithApplyCodeClientRust(rustClientCode(
			`self.walkers_pending_h3_choropleth.push(H3Choropleth { cell_ids, rgbas: cols, stroke_width, stroke_color });
`)).
		WithSettingImmediate(true).
		WithReturnType(structH3CellsColored()).
		Build())

	// h3Region — aggregated outline from a cell set. Rust dissolves the cell
	// set into a multipolygon outline (cached per walkersMap by set hash)
	// and draws fill + stroke. Use for ROIs drawn by the user, persisted
	// admin boundaries as H3, etc.
	registered = append(registered, idl.NewBuilderFactoryNode("h3Region").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("cellIds", ctabb.U64h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("fill").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("fill = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("stroke").Arg("col", ctabb.U32).AsColor().Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("stroke = Some(egui::Stroke::new(width, color32_from_rgba_u32(col)));\n")).EndMethod().
			BeginMethod("label").Arg("text", ctabb.S).
			CodeClientRust(rustClientCode("label = Some(text);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut fill: Option<egui::Color32> = None;
let mut stroke: Option<egui::Stroke> = None;
let mut label: Option<String> = None;
`)).
		WithApplyCodeClientRust(rustClientCode(
			`self.walkers_pending_h3_regions.push(H3Region { cell_ids, fill, stroke, label });
`)).
		WithSettingImmediate(true).
		WithReturnType(structH3Region()).
		Build())

	return registered
}

// --- Widget (drain node — renders basemap + overlays) ---

func definitionsWalkersWidgets() []*ir.BuilderFactoryNode {
	widgets := make([]*ir.BuilderFactoryNode, 0, 1)

	// walkersMap — renders the slippy map with all pending overlays drained
	// in. First call per id constructs HttpTiles (unless noTiles=true) and
	// MapMemory; subsequent frames reuse them.
	//
	// Tile server is configured via methods:
	//   - default (no .TileUrl):   built-in walkers::sources::OpenStreetMap
	//   - .TileUrl("https://..."): custom XYZ template with {z}/{x}/{y}
	//   - noTiles=true:             no basemap (virtual H3 canvas)
	//
	// If the tile config (url / attribution / maxZoom / tileSize) changes
	// for an existing map id across frames, HttpTiles is rebuilt — the
	// tile cache is dropped and fresh downloads begin under the new URL.
	widgets = append(widgets, idl.NewBuilderFactoryNode("walkersMap").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("initLat", ctabb.F64).
			PlainArg("initLon", ctabb.F64).
			PlainArg("noTiles", ctabb.B).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("width = wi;\n")).EndMethod().
			BeginMethod("height").Arg("he", ctabb.F32).
			CodeClientRust(rustClientCode("height = he;\n")).EndMethod().
			BeginMethod("setZoom").Arg("zoom", ctabb.F64).
			CodeClientRust(rustClientCode("override_zoom = Some(zoom);\n")).EndMethod().
			BeginMethod("centerAt").Arg("lat", ctabb.F64).Arg("lon", ctabb.F64).
			CodeClientRust(rustClientCode("override_center = Some((lat, lon));\n")).EndMethod().
			BeginMethod("zoomGesture").Arg("enabled", ctabb.B).
			CodeClientRust(rustClientCode("zoom_gesture = enabled;\n")).EndMethod().
			BeginMethod("panning").Arg("enabled", ctabb.B).
			CodeClientRust(rustClientCode("panning = enabled;\n")).EndMethod().
			// XYZ URL template; must contain {z}/{x}/{y} placeholders. Empty
			// string (default) uses the built-in OpenStreetMap source.
			BeginMethod("tileUrl").Arg("url", ctabb.S).
			CodeClientRust(rustClientCode("tile_url_template = url;\n")).EndMethod().
			// Attribution text rendered by walkers' attribution widget. Free-
			// form; pass the source provider's required credit line.
			BeginMethod("tileAttribution").Arg("text", ctabb.S).
			CodeClientRust(rustClientCode("tile_attribution_text = text;\n")).EndMethod().
			// Max zoom level for this tile source (default 19 when 0).
			BeginMethod("tileMaxZoom").Arg("zoom", ctabb.U8).
			CodeClientRust(rustClientCode("tile_max_zoom = zoom;\n")).EndMethod().
			// Tile edge length in pixels (default 256 when 0).
			BeginMethod("tileSize").Arg("size", ctabb.U32).
			CodeClientRust(rustClientCode("tile_size = size;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut width: f32 = 600.0;
let mut height: f32 = 400.0;
let mut override_zoom: Option<f64> = None;
let mut override_center: Option<(f64, f64)> = None;
let mut zoom_gesture: bool = true;
let mut panning: bool = true;
let mut tile_url_template: String = String::new();
let mut tile_attribution_text: String = String::new();
let mut tile_max_zoom: u8 = 0;
let mut tile_size: u32 = 0;
`)).
		WithApplyCodeClientRust(rustClientCode(`
self.render_walkers_map(
    {{EguiUiOptionalOuter}}, {{FuncProcIdOuter}}, {{Id}},
    init_lat, init_lon, no_tiles,
    width, height, override_zoom, override_center,
    zoom_gesture, panning,
    tile_url_template, tile_attribution_text, tile_max_zoom, tile_size,
);
`)).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithReturnType(structWalkersMap()).
		Build())

	return widgets
}

// --- Fetcher (viewport + pointer state, drained by Go) ---

func definitionsWalkersFetchers() []ir.NodeI {
	fetchers := make([]ir.NodeI, 0, 1)

	// fetchR15WalkersCamera — returns the last rendered map's viewport and
	// pointer state. `found=false` means no walkersMap was rendered since
	// last fetch.
	//
	// All coords are WGS84 degrees. `zoom` is walkers' internal zoom scalar.
	// `viewHash` is a quantized hash of (center,zoom,size) — equal across
	// frames when the camera hasn't moved meaningfully, so Go can skip its
	// heatmap recompute on still cameras.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR15WalkersCamera").
		WithApplyCodeClientRust(rustClientCode(`
// Non-consuming read: multiple readers per frame (e.g. an overlay
// emitter and an on-screen camera readout) must see the same value.
// `+"`walkers_last_camera`"+` is only overwritten by OverlayPlugin when a new
// walkersMap renders, so stale reads between map renders return the
// most recent valid camera — the desired behaviour for Go-side heatmap
// computation that runs on one-frame lag against the viewport.
match self.walkers_last_camera.as_ref() {
    Some(c) => {
        self.io.write_plain_b(true)?;
        self.io.write_plain_u64(c.map_id)?;
        self.io.write_plain_f64(c.zoom)?;
        self.io.write_plain_f64(c.center_lat)?;
        self.io.write_plain_f64(c.center_lon)?;
        self.io.write_plain_f64(c.min_lat)?;
        self.io.write_plain_f64(c.min_lon)?;
        self.io.write_plain_f64(c.max_lat)?;
        self.io.write_plain_f64(c.max_lon)?;
        self.io.write_plain_f32(c.screen_width_px)?;
        self.io.write_plain_f32(c.screen_height_px)?;
        self.io.write_plain_f64(c.hover_lat)?;
        self.io.write_plain_f64(c.hover_lon)?;
        self.io.write_plain_b(c.hover_valid)?;
        self.io.write_plain_b(c.clicked)?;
        self.io.write_plain_u64(c.view_hash)?;
    }
    None => {
        self.io.write_plain_b(false)?;
        self.io.write_plain_u64(0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f64(0.0)?;
        self.io.write_plain_f32(0.0)?;
        self.io.write_plain_f32(0.0)?;
        self.io.write_plain_f64(f64::NAN)?;
        self.io.write_plain_f64(f64::NAN)?;
        self.io.write_plain_b(false)?;
        self.io.write_plain_b(false)?;
        self.io.write_plain_u64(0)?;
    }
}
{{SendMessage}}
`)).
		AddReturnValue("found", ctabb.B).
		AddReturnValue("mapId", ctabb.U64).
		AddReturnValue("zoom", ctabb.F64).
		AddReturnValue("centerLat", ctabb.F64).
		AddReturnValue("centerLon", ctabb.F64).
		AddReturnValue("minLat", ctabb.F64).
		AddReturnValue("minLon", ctabb.F64).
		AddReturnValue("maxLat", ctabb.F64).
		AddReturnValue("maxLon", ctabb.F64).
		AddReturnValue("screenWidthPx", ctabb.F32).
		AddReturnValue("screenHeightPx", ctabb.F32).
		AddReturnValue("hoverLat", ctabb.F64).
		AddReturnValue("hoverLon", ctabb.F64).
		AddReturnValue("hoverValid", ctabb.B).
		AddReturnValue("clicked", ctabb.B).
		AddReturnValue("viewHash", ctabb.U64).
		Build())

	return fetchers
}
