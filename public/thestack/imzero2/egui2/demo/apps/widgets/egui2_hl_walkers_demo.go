//go:build llm_generated_opus47

package widgets

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/science/geo/h3"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// =============================================================================
// walkers (slippy map) demo — OSM map with markers, polyline, H3 region,
// H3 choropleth, H3 uniform heatmap, and viewport/pointer readback via
// fetchR15WalkersCamera. All H3 cells are computed live through
// boxer/public/science/geo/h3 (h3o-wazero).
// =============================================================================

const (
	walkersDemoCenterLat = 51.0992
	walkersDemoCenterLon = 17.0366
)

// h3 Runtime + Handle — one global pair, lazy-initialised on first demo frame.
// ImZero2's Go side is single-threaded per skill doc, so a single Handle kept
// across frames is sufficient. Runtime is never Close()d: it lives for the
// process lifetime (acceptable for a demo). Shared across all walkers demo
// windows — H3 cell math is window-independent.
var (
	h3InitOnce sync.Once
	h3Runtime  *h3.Runtime
	h3Handle   *h3.Handle
	h3InitErr  error
)

func ensureH3() error {
	h3InitOnce.Do(func() {
		ctx := context.Background()
		rt, err := h3.NewRuntime(ctx, h3.RuntimeConfig{PoolSize: 1})
		if err != nil {
			h3InitErr = err
			log.Error().Err(err).Msg("h3 runtime init failed")
			return
		}
		handle, err := rt.AcquireE(ctx)
		if err != nil {
			h3InitErr = err
			log.Error().Err(err).Msg("h3 handle acquire failed")
			return
		}
		h3Runtime = rt
		h3Handle = handle
		log.Info().Msg("h3 runtime ready")
	})
	return h3InitErr
}

// h3ResForZoom picks an H3 resolution from a walkers zoom scalar. Rough
// map: world zooms hit res 2-3, continental 4-5, country 6-7, metro 8-9,
// street 10-12. Clamped to [1, 12] to keep cell counts manageable.
func h3ResForZoom(zoom float64) h3.ResolutionE {
	r := int(math.Round(zoom/2.0 - 1.0))
	if r < 1 {
		r = 1
	}
	if r > 12 {
		r = 12
	}
	return h3.ResolutionE(r)
}

// demoRegionCells — lazy-cached k-ring around Wrocław at res 7. Shared
// across windows (input is fixed: center lat/lon + radius). h3Handle
// errors fall back to an empty slice so the rest of the demo still renders.
var (
	demoRegionCellsOnce sync.Once
	demoRegionCells     []uint64
)

func getDemoRegionCells() []uint64 {
	demoRegionCellsOnce.Do(func() {
		err := ensureH3()
		if err != nil {
			return
		}
		ctx := context.Background()
		var center uint64
		center, _, err = h3Handle.LatLngToCellE(ctx,
			h3.ResolutionR7, walkersDemoCenterLat, walkersDemoCenterLon)
		if err != nil {
			return
		}
		out, _, err := h3Handle.GridDiskE(ctx, 2, center)
		if err != nil {
			return
		}
		demoRegionCells = out
	})
	return demoRegionCells
}

var (
	demoChoroplethCellsOnce sync.Once
	demoChoroplethCells     []uint64
)

func getDemoChoroplethCells() []uint64 {
	demoChoroplethCellsOnce.Do(func() {
		err := ensureH3()
		if err != nil {
			return
		}
		ctx := context.Background()
		var center uint64
		center, _, err = h3Handle.LatLngToCellE(ctx,
			h3.ResolutionR7, walkersDemoCenterLat, walkersDemoCenterLon)
		if err != nil {
			return
		}
		out, _, err := h3Handle.GridDiskE(ctx, 3, center)
		if err != nil {
			return
		}
		demoChoroplethCells = out
	})
	return demoChoroplethCells
}

type walkersTileServer struct {
	label       string
	urlTemplate string // empty = use built-in OpenStreetMap
	attribution string
	maxZoom     uint8
	tileSize    uint32
}

// Note on {s} subdomain templates: walkers v1 binding doesn't rotate
// subdomains. Where a provider uses {s}, pick a single subdomain (a/b/c)
// by hand-coding the URL to reduce rate-limit risk.
//
// Shared across windows — fixed catalog of tile servers.
var walkersTileServers = []walkersTileServer{
	{
		label:       "OpenStreetMap (built-in)",
		urlTemplate: "",
		attribution: "",
		maxZoom:     19,
	},
	{
		label:       "CartoDB Positron (light)",
		urlTemplate: "https://a.basemaps.cartocdn.com/light_all/{z}/{x}/{y}.png",
		attribution: "© OpenStreetMap contributors, © CARTO",
		maxZoom:     20,
	},
	{
		label:       "CartoDB Dark Matter",
		urlTemplate: "https://a.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}.png",
		attribution: "© OpenStreetMap contributors, © CARTO",
		maxZoom:     20,
	},
	{
		label:       "OpenTopoMap",
		urlTemplate: "https://a.tile.opentopomap.org/{z}/{x}/{y}.png",
		attribution: "© OpenStreetMap contributors, SRTM, © OpenTopoMap (CC-BY-SA)",
		maxZoom:     17,
	},
}

// walkersDemoState carries per-window walkers UI state: visibility
// toggles, dimensions, override-zoom edge-trigger flags, the frame
// counter, the tile-server radio binding, and the per-window heatmap
// cache (invalidated when viewHash or H3 resolution changes).
type walkersDemoState struct {
	showMarkers     bool
	showPolyline    bool
	showRegion      bool
	showChoropleth  bool
	mapWidth        float64
	mapHeight       float64
	overrideZoomVal float64
	applyZoom       bool
	applyCenter     bool
	frame           uint64

	// Tile-server selector. Default is 0 = OSM built-in. The other
	// presets exercise the `.TileUrl / .TileAttribution / .TileMaxZoom
	// / .TileSize` methods and the Rust-side rebuild-on-change path.
	tileSrcIdx int
	radioBound []bool

	heatmap walkersHeatmapState
}

// walkersHeatmapState — per-window viewport-driven choropleth cache.
type walkersHeatmapState struct {
	show           bool
	alpha          float64
	cachedViewHash uint64
	cachedRes      h3.ResolutionE
	cachedCells    []uint64
	cachedRgbas    []uint32
	cachedCount    int
}

func newWalkersDemoState() (st *walkersDemoState) {
	st = &walkersDemoState{
		showMarkers:     true,
		showPolyline:    true,
		showRegion:      true,
		showChoropleth:  false, // second map window owns it
		mapWidth:        640.0,
		mapHeight:       440.0,
		overrideZoomVal: 11.0,
		tileSrcIdx:      0,
		radioBound:      make([]bool, len(walkersTileServers)),
		heatmap: walkersHeatmapState{
			show:  true,
			alpha: 0.55,
		},
	}
	return
}

// demoWalkersBasic — OSM map with markers + a polyline + an H3 region outline
// + optional uniform H3 heatmap overlay. All H3 overlays ride the basic map's
// viewport.
func demoWalkersBasic(ids *c.WidgetIdStack, st *walkersDemoState) {
	hm := &st.heatmap

	// Controls
	c.Checkbox(ids.PrepareStr("walkers-markers"), st.showMarkers, "markers").
		SendRespVal(&st.showMarkers)
	c.Checkbox(ids.PrepareStr("walkers-polyline"), st.showPolyline, "polyline (route)").
		SendRespVal(&st.showPolyline)
	c.Checkbox(ids.PrepareStr("walkers-region"), st.showRegion, "H3 region outline").
		SendRespVal(&st.showRegion)
	c.Checkbox(ids.PrepareStr("walkers-hm-show"), hm.show, "uniform H3 heatmap overlay").
		SendRespVal(&hm.show)
	c.SliderF64(ids.PrepareStr("walkers-hm-alpha"), hm.alpha, 0.05, 1.0).
		Text("heatmap alpha").SendRespVal(&hm.alpha)
	c.SliderF64(ids.PrepareStr("walkers-w"), st.mapWidth, 300, 1200).
		Text("width px").SendRespVal(&st.mapWidth)
	c.SliderF64(ids.PrepareStr("walkers-h"), st.mapHeight, 200, 800).
		Text("height px").SendRespVal(&st.mapHeight)
	if c.Button(ids.PrepareStr("walkers-zoom-btn"), c.Atoms().Text("set zoom").Keep()).
		SendResp().HasPrimaryClicked() {
		st.applyZoom = true
	}
	c.SliderF64(ids.PrepareStr("walkers-zoom-val"), st.overrideZoomVal, 2, 18).
		Text("zoom").SendRespVal(&st.overrideZoomVal)
	if c.Button(ids.PrepareStr("walkers-center-btn"), c.Atoms().Text("center on Wrocław").Keep()).
		SendResp().HasPrimaryClicked() {
		st.applyCenter = true
	}

	// Tile-server selector. Rust rebuilds HttpTiles when the tile config
	// signature changes and keeps MapMemory (pan/zoom) across the switch.
	for range c.IdScope(ids.PrepareStr("walkers-tiles-scope")) {
		c.Label("Tile server:").Send()
		// Edge-detect last frame's r10 apply: a bound bool that's true but
		// no longer matches tileSrcIdx is a click that landed there.
		for i := range walkersTileServers {
			if st.radioBound[i] && st.tileSrcIdx != i {
				st.tileSrcIdx = i
				break
			}
		}
		for i, ts := range walkersTileServers {
			isSelected := st.tileSrcIdx == i
			st.radioBound[i] = isSelected
			c.RadioButton(
				ids.PrepareSeq(uint64(i)),
				c.Atoms().Text(ts.label).Keep(),
				isSelected,
			).SendRespVal(&st.radioBound[i])
		}
	}

	// Markers — three POIs around Wrocław.
	if st.showMarkers {
		c.MapMarker(1, 51.1100, 17.0320).
			Label("Market Square").Color(color.Hex(0xff4444ff)).Radius(7).Send()
		c.MapMarker(2, 51.1089, 17.0300).
			Label("University").Color(color.Hex(0x44ccffff)).Radius(7).Send()
		c.MapMarker(3, 51.1045, 17.0752).
			Label("Zoo").Color(color.Hex(0x44ff66ff)).Radius(7).Send()
	}

	// Polyline — a simple 4-segment route.
	if st.showPolyline {
		c.MapPolyline(
			[]float64{51.1100, 51.1080, 51.1050, 51.1045},
			[]float64{17.0320, 17.0400, 17.0550, 17.0752},
		).Stroke(color.Hex(0xffaa00ff), styletokens.StrokeStrong).Send()
	}

	// H3 region — translucent fill + crisp stroke + label at centroid.
	// Cells computed via h3o-wazero (lazy, cached).
	if st.showRegion {
		if cells := getDemoRegionCells(); len(cells) > 0 {
			c.H3Region(cells).
				Fill(color.Hex(0x3388ff44)).Stroke(color.Hex(0x3388ffff), styletokens.StrokeStrong).Label("ROI").Send()
		}
	}

	// Uniform H3 heatmap overlay — viewport-driven. Reads the previous
	// frame's camera via fetchR15WalkersCamera (one-frame lag under pan,
	// imperceptible at interactive cadence), computes cells covering the
	// bbox via h3o-wazero polygonToCells, evaluates a synthetic world-
	// space function, and colormaps. Sent BEFORE the walkersMap opcode
	// below so it joins the overlay drain for this frame's map render.
	if hm.show {
		emitUniformHeatmap(hm)
	}

	// Map widget itself. override_zoom / override_center are sticky for one
	// frame at a time. Tile config flows in via .TileUrl / .TileAttribution
	// / .TileMaxZoom; swapping `tileSrcIdx` rebuilds HttpTiles.
	ts := walkersTileServers[st.tileSrcIdx]
	mw := c.WalkersMap(
		ids.PrepareStr("walkers-main"),
		walkersDemoCenterLat, walkersDemoCenterLon,
		false, /*noTiles*/
	).
		Width(float32(st.mapWidth)).
		Height(float32(st.mapHeight)).
		TileUrl(ts.urlTemplate).
		TileAttribution(ts.attribution).
		TileMaxZoom(ts.maxZoom)
	if st.applyZoom {
		mw = mw.SetZoom(st.overrideZoomVal)
		st.applyZoom = false
	}
	if st.applyCenter {
		mw = mw.CenterAt(walkersDemoCenterLat, walkersDemoCenterLon)
		st.applyCenter = false
	}
	mw.Send()

	st.frame++
}

// demoWalkersChoropleth — NoTiles map with an H3 choropleth layer.
// Values follow a radial ramp from the center cell so the color gradient
// reads clearly without needing real data attached.
func demoWalkersChoropleth(ids *c.WidgetIdStack, st *walkersDemoState) {
	c.Checkbox(ids.PrepareStr("walkers-choropleth-show"), st.showChoropleth, "show choropleth").
		SendRespVal(&st.showChoropleth)
	if !st.showChoropleth {
		c.Label("(toggle on to render)").Send()
		return
	}

	cells := getDemoChoroplethCells()
	if len(cells) == 0 {
		c.Label("(h3 runtime not ready)").Send()
		return
	}
	n := len(cells)
	rgbas := make([]uint32, n)
	for i := 0; i < n; i++ {
		// Synthetic radial value with a small per-frame wiggle.
		t := float64(i) / float64(n-1)
		t += 0.1 * math.Sin(float64(st.frame)*0.04+float64(i)*0.3)
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		rgbas[i] = heatmapPalette(t)
	}
	c.H3CellsColored(cells, color.ColorsFromU32(rgbas)).
		StrokeWidth(0.5).StrokeColor(color.Hex(0x20202080)).Send()

	c.WalkersMap(
		ids.PrepareStr("walkers-choro"),
		walkersDemoCenterLat, walkersDemoCenterLon,
		true, // noTiles = true (virtual H3 canvas)
	).
		Width(float32(st.mapWidth)).
		Height(float32(st.mapHeight)).
		Send()
}

// demoWalkersCamera — reads the cached camera (drained at last frame's
// end by StateManager.Sync) and shows bbox, zoom, hover lat/lon. Use
// with the basic map above. Reads cache rather than inline-fetching:
// inline fetches inside a dock.Tab body deadlock the render loop.
func demoWalkersCamera(ids *c.WidgetIdStack, st *walkersDemoState) {
	_ = ids
	_ = st
	cam := c.CurrentApplicationState.StateManager.GetWalkersCamera()
	if !cam.Found {
		c.Label("No walkersMap rendered since last fetch").Send()
		return
	}
	c.Label(fmt.Sprintf("map id     : 0x%x", cam.MapId)).Send()
	c.Label(fmt.Sprintf("zoom       : %.3f", cam.Zoom)).Send()
	c.Label(fmt.Sprintf("center     : %.5f, %.5f", cam.CenterLat, cam.CenterLon)).Send()
	c.Label(fmt.Sprintf("bbox       : [%.4f, %.4f] × [%.4f, %.4f]", cam.MinLat, cam.MaxLat, cam.MinLon, cam.MaxLon)).Send()
	c.Label(fmt.Sprintf("screen px  : %.0f × %.0f", cam.ScreenWidthPx, cam.ScreenHeightPx)).Send()
	if cam.HoverValid {
		c.Label(fmt.Sprintf("hover      : %.5f, %.5f", cam.HoverLat, cam.HoverLon)).Send()
	} else {
		c.Label("Hover      : —").Send()
	}
	if cam.Clicked {
		c.Label("Clicked this frame").Send()
	}
	c.Label(fmt.Sprintf("view hash  : %016x", cam.ViewHash)).Send()
}

// =============================================================================
// Uniform heatmap challenger — viewport-driven H3 choropleth.
// =============================================================================
// Exercises the full round-trip we designed the H3-as-exchange protocol for:
//   1. Fetch viewport bbox via fetchR15WalkersCamera (Rust → Go).
//   2. Resolve zoom → H3 resolution.
//   3. polygonToCells on the bbox (h3o-wazero).
//   4. cellsToLatLngs on the result to get per-cell centers.
//   5. Apply a synthetic world-space value function: f(lat, lng).
//   6. Colormap via heatmapPalette.
//   7. Send as H3CellsColored (Go → Rust), rendered over OSM tiles.
//
// As the user pans/zooms, the heatmap re-computes from the new bbox and
// stays pinned in world space — the value function is evaluated at actual
// lat/lng, not relative to the viewport center.

// demoWalkersHeatmapInfo — informational readout of the heatmap cache state.
// Does not send any overlay; that happens in demoWalkersBasic via
// emitUniformHeatmap so it rides the main map's render.
func demoWalkersHeatmapInfo(ids *c.WidgetIdStack, st *walkersDemoState) {
	_ = ids
	hm := &st.heatmap
	c.Label("Toggle 'uniform H3 heatmap overlay' on the main map to enable.").Send()
	c.Label("The heatmap samples a world-space function at each visible H3").Send()
	c.Label("cell centroid; pan/zoom triggers a fresh polygonToCells call.").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Separator().Send()
	if hm.cachedCount == 0 {
		c.Label("No heatmap computed yet").Send()
		return
	}
	c.Label(fmt.Sprintf("resolution : R%d", uint8(hm.cachedRes))).Send()
	c.Label(fmt.Sprintf("cells      : %d", hm.cachedCount)).Send()
	c.Label(fmt.Sprintf("view hash  : %016x", hm.cachedViewHash)).Send()
}

// emitUniformHeatmap sends an H3CellsColored overlay covering the previous
// frame's viewport. Called from demoWalkersBasic so the overlay joins the
// main map's render. Caches cells+colors by viewHash — still cameras pay
// zero work.
func emitUniformHeatmap(hm *walkersHeatmapState) {
	err := ensureH3()
	if err != nil {
		return
	}
	// Read cached camera (drained at last frame's end by
	// StateManager.Sync). Inline fetching here would deadlock when the
	// walkers demo is mounted inside a dock.Tab body — see
	// CanvasPointerValue docstring.
	cam := c.CurrentApplicationState.StateManager.GetWalkersCamera()
	if !cam.Found {
		return
	}

	res := h3ResForZoom(cam.Zoom)
	if cam.ViewHash != hm.cachedViewHash || res != hm.cachedRes {
		cells := computeViewportCells(cam.MinLat, cam.MaxLat, cam.MinLon, cam.MaxLon, res)
		rgbas := computeHeatmapColors(cells)
		hm.cachedViewHash = cam.ViewHash
		hm.cachedRes = res
		hm.cachedCells = cells
		hm.cachedRgbas = rgbas
		hm.cachedCount = len(cells)
	}
	if hm.cachedCount == 0 {
		return
	}
	// Apply user alpha without redoing polygonToCells + sampling.
	alphaU8 := uint32(math.Max(0, math.Min(255, hm.alpha*255)))
	out := make([]uint32, len(hm.cachedRgbas))
	for i, rgba := range hm.cachedRgbas {
		out[i] = (rgba &^ 0xff) | alphaU8
	}
	c.H3CellsColored(hm.cachedCells, out).Send()
}

// computeViewportCells returns the H3 cells at res that cover the given
// bbox. On error or empty result, returns nil (caller treats as no-op).
func computeViewportCells(minLat, maxLat, minLon, maxLon float64, res h3.ResolutionE) (cells []uint64) {
	// Closed ring for polygonToCells. One exterior ring, no holes — use
	// the scalar convenience wrapper and skip ringOffsets plumbing.
	lats := []float64{minLat, minLat, maxLat, maxLat, minLat}
	lngs := []float64{minLon, maxLon, maxLon, minLon, minLon}
	var err error
	cells, err = h3Handle.PolygonToCellsSimpleE(
		context.Background(), res, h3.ContainmentIntersectsBoundary,
		lats, lngs,
	)
	if err != nil {
		log.Warn().Err(err).Msg("heatmap polygonToCells failed")
		cells = nil
		return
	}
	return
}

// computeHeatmapColors evaluates the synthetic world-space value function
// at each cell's centroid and colormaps to rgba. Value function is chosen
// to vary smoothly across continents so the user can see it change when
// panning large distances.
func computeHeatmapColors(cells []uint64) []uint32 {
	if len(cells) == 0 {
		return nil
	}
	lats, lngs, _, err := h3Handle.CellsToLatLngsE(context.Background(), cells, nil, nil, nil)
	if err != nil {
		log.Warn().Err(err).Msg("heatmap cellsToLatLngs failed")
		return nil
	}
	rgbas := make([]uint32, len(cells))
	for i := range cells {
		v := syntheticValue(lats[i], lngs[i])
		rgbas[i] = heatmapPalette(v)
	}
	return rgbas
}

// syntheticValue — fixed world-space function producing 0..1. The
// (1+sin)(1+cos)/4 construction is smoothly varying, strictly bounded,
// and looks pleasant under any colormap.
func syntheticValue(latDeg, lngDeg float64) float64 {
	const k = 0.15 // spatial frequency; ~6° half-wavelength
	s := math.Sin(latDeg * k)
	co := math.Cos(lngDeg * k)
	return (1 + s) * (1 + co) / 4.0
}

// heatmapPalette — discrete 9-stop viridis-ish ramp. Returns rgba u32
// (packed big-endian R,G,B,A). Alpha is 0xff; callers may overwrite.
func heatmapPalette(t float64) uint32 {
	palette := []uint32{
		0x2b83baff, 0x5ba9ceff, 0x91d3e0ff, 0xc7e9b4ff,
		0xffffbfff, 0xfecc5cff, 0xfd8d3cff, 0xe6550dff,
		0xa50026ff,
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	idx := int(t * float64(len(palette)-1))
	if idx >= len(palette) {
		idx = len(palette) - 1
	}
	return palette[idx]
}
