package widgets

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/science/geo/h3"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/h3overlay"
)

// The map demos: ADR-0204's slippy-map widget over the registry's basemap
// (OpenStreetMap unless BOXER_MAP_TILE_URL points elsewhere) with markers, a
// route, an H3 region, a viewport-driven H3 heatmap, a tile-server switch and
// a readout of the view and the tile pipeline a headless scene can assert on;
// an H3 choropleth on a NoTiles canvas; and the geo-raster overlay of
// ADR-0096 pinned to a bbox. Every H3 cell comes from public/science/geo/h3
// (the h3 wasm bridge) at runtime.

const (
	demoMapCenterLat = 51.0992 // Wrocław
	demoMapCenterLon = 17.0366
	portolanDemoW    = 720
	portolanDemoH    = 460
	choroplethDemoH  = 320
)

// h3 runtime + handle — one pair, made lazily on the first frame that needs
// it and kept for the process: the Go side renders on one goroutine, so one
// handle serves every demo window, and cell math is window-independent.
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

// demoDisk is a k-ring of cells around the demo centre at res 7, computed once
// per k; an h3 error leaves it empty so the rest of the demo still renders.
func demoDisk(k uint8, once *sync.Once, dst *[]uint64) []uint64 {
	once.Do(func() {
		if ensureH3() != nil {
			return
		}
		ctx := context.Background()
		center, _, err := h3Handle.LatLngToCellE(ctx, h3.ResolutionR7, demoMapCenterLat, demoMapCenterLon)
		if err != nil {
			return
		}
		out, _, err := h3Handle.GridDiskE(ctx, k, center)
		if err != nil {
			return
		}
		*dst = out
	})
	return *dst
}

var (
	demoRegionOnce, demoChoroplethOnce   sync.Once
	demoRegionCells, demoChoroplethCells []uint64
)

func getDemoRegionCells() []uint64     { return demoDisk(2, &demoRegionOnce, &demoRegionCells) }
func getDemoChoroplethCells() []uint64 { return demoDisk(3, &demoChoroplethOnce, &demoChoroplethCells) }

// demoTileServer is one entry of the tile-server switch; an empty url is the
// registry's basemap (BOXER_MAP_TILE_URL, OpenStreetMap by default).
type demoTileServer struct {
	label, url, attribution string
	maxZoom                 float64
}

var demoTileServers = []demoTileServer{
	{label: "OpenStreetMap (the basemap default)"},
	{label: "CartoDB Positron (light)", url: "https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}.png",
		attribution: "© OpenStreetMap contributors, © CARTO", maxZoom: 20},
	{label: "CartoDB Dark Matter", url: "https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}.png",
		attribution: "© OpenStreetMap contributors, © CARTO", maxZoom: 20},
	{label: "OpenTopoMap", url: "https://{s}.tile.opentopomap.org/{z}/{x}/{y}.png",
		attribution: "© OpenStreetMap contributors, SRTM, © OpenTopoMap (CC-BY-SA)", maxZoom: 17},
}

func (ts demoTileServer) source() portolan.TileSource {
	if ts.url == "" {
		return basemap.PortolanSource()
	}
	src := portolan.NewTileSource(ts.url)
	src.Attribution = ts.attribution
	src.MaxZoom = ts.maxZoom
	return src.Normalized()
}

// The three POIs and the route the overlays draw.
var (
	demoPOIs = []struct {
		name     string
		lat, lng float64
		col      color.Color
	}{
		{"Market Square", 51.1100, 17.0320, color.Hex(0xff4444ff)},
		{"University", 51.1089, 17.0300, color.Hex(0x44ccffff)},
		{"Zoo", 51.1045, 17.0752, color.Hex(0x44ff66ff)},
	}
	demoRouteLats = []float64{51.1100, 51.1080, 51.1050, 51.1045}
	demoRouteLngs = []float64{17.0320, 17.0400, 17.0550, 17.0752}
)

type portolanDemoState struct {
	m      *portolan.Map
	choro  *portolan.Map // the NoTiles canvas of the choropleth, made on first use
	cells  h3overlay.Layer
	region h3overlay.Region

	showMarkers, showPolyline, showRegion, showHeatmap, showChoropleth bool
	heatAlpha                                                          float64
	zoomVal                                                            float64
	tileSrcIdx                                                         int
	radioBound                                                         []bool
	heat                                                               demoHeatmap
	frame                                                              uint64
}

// demoHeatmap is the viewport-driven layer's cache: cells and colours are
// recomputed when the view hash or the resolution changes, so a still camera
// pays nothing.
type demoHeatmap struct {
	viewHash uint64
	res      h3.ResolutionE
	cells    []uint64
	rgbas    []uint32
	fills    []color.Color
}

func newPortolanDemoState(ids *c.WidgetIdStack) *portolanDemoState {
	return &portolanDemoState{
		m: portolan.New(ids, portolan.Options{
			Source: basemap.PortolanSource(),
			Loader: basemap.PortolanLoader(),
			Center: portolan.LL(demoMapCenterLat, demoMapCenterLon),
			Zoom:   12,
		}),
		showMarkers: true, showPolyline: true, showRegion: true, showHeatmap: true,
		heatAlpha:  0.55,
		zoomVal:    11,
		radioBound: make([]bool, len(demoTileServers)),
	}
}

func demoPortolan(ids *c.WidgetIdStack, st *portolanDemoState) {
	m := st.m
	m.Render(portolanDemoW, portolanDemoH, st.overlays)
	v := m.View()
	b := v.Bounds()
	ps := m.Stats()
	h := m.Health()
	c.Label(fmt.Sprintf("centre %.5f, %.5f   zoom %.2f   bounds %.4f,%.4f → %.4f,%.4f",
		v.Center().Lat, v.Center().Lng, v.Zoom(), b.GetSouth(), b.GetWest(), b.GetNorth(), b.GetEast())).Send()
	c.Label(fmt.Sprintf("tiles: %d requested · %d loaded · %d errors · %d unloaded · %d pending · loading %v",
		ps.TileLoadStart, ps.TileLoad, ps.TileError, ps.TileUnload, h.Pending+h.InFlight, m.Loading())).Send()
	c.Label(fmt.Sprintf("shipped %.2f MB through paintImage · re-ships %d · consecutive fetch failures %d",
		float64(m.BytesShipped())/(1<<20), m.Reships(), h.ConsecutiveFailures)).Send()
	// The canvas's screen rect, for a headless scene to aim its pointer at: the
	// canvas is painter-only and has no node in the accessibility tree.
	if ox, oy, ok := m.CanvasOrigin(); ok {
		c.Label(fmt.Sprintf("canvas at %.0f,%.0f · %d × %d px", ox, oy, portolanDemoW, portolanDemoH)).Send()
	}
	c.Label("portolan (ADR-0204): drag to pan (with inertia), wheel to zoom about the pointer, double-click to zoom in " +
		"(shift: out), shift-drag a box to zoom to it, arrows to pan once the map has focus. Tiles are retained across " +
		"levels while the new level loads; the overlays below are drawn through the projector hook.").Wrap().Send()

	for range c.CollapsingHeader(ids.PrepareStr("portolan-controls"), c.WidgetText().Text("overlays, view and tile server").Keep()).DefaultOpen(true).KeepIter() {
		st.controls(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("portolan-camera"), c.WidgetText().Text("camera (view + pointer readback)").Keep()).DefaultOpen(true).KeepIter() {
		st.camera()
	}
	for range c.CollapsingHeader(ids.PrepareStr("portolan-heatmap"), c.WidgetText().Text("heatmap (H3, uniform)").Keep()).KeepIter() {
		st.heatmapInfo()
	}
	for range c.CollapsingHeader(ids.PrepareStr("portolan-choropleth"), c.WidgetText().Text("choropleth (H3, NoTiles canvas)").Keep()).KeepIter() {
		st.choropleth(ids)
	}
	st.frame++
}

// overlays paints, bottom to top: the heatmap, the region, the route, the
// markers with their labels.
func (st *portolanDemoState) overlays(p portolan.Projector) {
	ctx := context.Background()
	if st.showHeatmap {
		st.drawHeatmap(ctx, p)
	}
	if st.showRegion {
		if cells := getDemoRegionCells(); len(cells) > 0 {
			// Translucent fill per cell, the dissolved outline stroked, a
			// label at its centroid — the former h3Region overlay.
			if err := st.region.Draw(ctx, p, h3Handle, cells, color.Hex(0x3388ff44), color.Hex(0x3388ffff), styletokens.StrokeStrong, "ROI", color.Hex(0x3388ffff), 14); err != nil {
				log.Warn().Err(err).Msg("region overlay")
			}
		}
	}
	if st.showPolyline {
		p.Polyline(demoRouteLats, demoRouteLngs, color.Hex(0xffaa00ff), styletokens.StrokeStrong)
	}
	if st.showMarkers {
		for _, poi := range demoPOIs {
			ll := portolan.LL(poi.lat, poi.lng)
			p.Marker(ll, 7, poi.col)
			p.Label(ll, 10, 0, 0, 1, poi.name, 13, color.Hex(0x202020ff))
		}
	}
}

// drawHeatmap samples a world-space function at the centroid of every H3
// cell the viewport touches and colour-maps it; pan and zoom recompute the
// cells (polygonToCells on the bounds), a still camera reuses them.
func (st *portolanDemoState) drawHeatmap(ctx context.Context, p portolan.Projector) {
	if ensureH3() != nil {
		return
	}
	v := p.View()
	res := h3overlay.ResolutionForZoom(v.Zoom())
	if vh := st.m.ViewHash(); vh != st.heat.viewHash || res != st.heat.res {
		cells, err := h3overlay.ViewportCells(ctx, h3Handle, v.Bounds(), res)
		if err != nil {
			log.Warn().Err(err).Msg("heatmap viewport cells")
			cells = nil
		}
		st.heat.viewHash, st.heat.res = vh, res
		st.heat.cells = cells
		st.heat.rgbas = computeHeatmapColors(cells)
		st.heat.fills = make([]color.Color, len(cells))
	}
	if len(st.heat.cells) == 0 {
		return
	}
	alpha := uint32(math.Max(0, math.Min(255, st.heatAlpha*255)))
	for i, rgba := range st.heat.rgbas {
		st.heat.fills[i] = color.Hex((rgba &^ 0xff) | alpha)
	}
	if err := st.cells.Cells(ctx, p, h3Handle, st.heat.cells, st.heat.fills, color.Color{}, 0); err != nil {
		log.Warn().Err(err).Msg("heatmap cells")
	}
}

func (st *portolanDemoState) controls(ids *c.WidgetIdStack) {
	c.Checkbox(ids.PrepareStr("markers"), st.showMarkers, "markers").SendRespVal(&st.showMarkers)
	c.Checkbox(ids.PrepareStr("polyline"), st.showPolyline, "polyline (route)").SendRespVal(&st.showPolyline)
	c.Checkbox(ids.PrepareStr("region"), st.showRegion, "H3 region").SendRespVal(&st.showRegion)
	c.Checkbox(ids.PrepareStr("heatmap"), st.showHeatmap, "uniform H3 heatmap overlay").SendRespVal(&st.showHeatmap)
	c.SliderF64(ids.PrepareStr("heat-alpha"), st.heatAlpha, 0.05, 1.0).Text("heatmap alpha").SendRespVal(&st.heatAlpha)
	v := st.m.View()
	c.SliderF64(ids.PrepareStr("zoom-val"), st.zoomVal, 2, 18).Text("zoom").SendRespVal(&st.zoomVal)
	if c.Button(ids.PrepareStr("zoom-btn"), c.Atoms().Text("set zoom (animated)").Keep()).SendResp().HasPrimaryClicked() {
		v.SetZoomAnimated(st.zoomVal, portolan.AnimateOptions{})
	}
	if c.Button(ids.PrepareStr("fly-btn"), c.Atoms().Text("fly to Wrocław").Keep()).SendResp().HasPrimaryClicked() {
		v.FlyTo(portolan.LL(demoMapCenterLat, demoMapCenterLon), 12, portolan.FlyOptions{})
	}
	// The tile-server switch. A bound radio that is true but no longer
	// matches the index is last frame's click; SetSource restarts the
	// pyramid on the new source at the current view.
	for range c.IdScope(ids.PrepareStr("tiles")) {
		c.Label("Tile server:").Send()
		for i := range demoTileServers {
			if st.radioBound[i] && st.tileSrcIdx != i {
				st.tileSrcIdx = i
				st.m.SetSource(demoTileServers[i].source())
				break
			}
		}
		for i, ts := range demoTileServers {
			selected := st.tileSrcIdx == i
			st.radioBound[i] = selected
			c.RadioButton(ids.PrepareSeq(uint64(i)), c.Atoms().Text(ts.label).Keep(), selected).SendRespVal(&st.radioBound[i])
		}
	}
}

// camera is the view readback an app gets from the map itself — no fetcher,
// no one-frame lag beyond the painter lane's.
func (st *portolanDemoState) camera() {
	m := st.m
	v := m.View()
	b := v.Bounds()
	sz := v.Size()
	c.Label(fmt.Sprintf("zoom       : %.3f", v.Zoom())).Send()
	c.Label(fmt.Sprintf("center     : %.5f, %.5f", v.Center().Lat, v.Center().Lng)).Send()
	c.Label(fmt.Sprintf("bbox       : [%.4f, %.4f] × [%.4f, %.4f]", b.GetSouth(), b.GetNorth(), b.GetWest(), b.GetEast())).Send()
	c.Label(fmt.Sprintf("screen px  : %.0f × %.0f", sz.X, sz.Y)).Send()
	if ll, ok := m.Hover(); ok {
		c.Label(fmt.Sprintf("hover      : %.5f, %.5f", ll.Lat, ll.Lng)).Send()
	} else {
		// designlint:ignore=L1 (aligned readout key; the Sprintf-built siblings above carry the same lowercase keys)
		c.Label("hover      : —").Send()
	}
	if ll, ok := m.Clicked(); ok {
		c.Label(fmt.Sprintf("clicked    : %.5f, %.5f (this frame)", ll.Lat, ll.Lng)).Send()
	}
	c.Label(fmt.Sprintf("view hash  : %016x   animating %v", m.ViewHash(), v.Animating())).Send()
}

func (st *portolanDemoState) heatmapInfo() {
	c.Label("Toggle 'uniform H3 heatmap overlay' above to enable. The heatmap samples a world-space function at each " +
		"visible H3 cell centroid; pan and zoom trigger a fresh polygonToCells call.").Wrap().Send()
	c.Separator().Send()
	if len(st.heat.cells) == 0 {
		c.Label("No heatmap computed yet").Send()
		return
	}
	c.Label(fmt.Sprintf("resolution : R%d", uint8(st.heat.res))).Send()
	c.Label(fmt.Sprintf("cells      : %d", len(st.heat.cells))).Send()
	c.Label(fmt.Sprintf("view hash  : %016x", st.heat.viewHash)).Send()
}

// choropleth is a NoTiles canvas with a radial ramp over a k-ring of cells,
// wiggled a little per frame so the gradient reads without data behind it.
func (st *portolanDemoState) choropleth(ids *c.WidgetIdStack) {
	c.Checkbox(ids.PrepareStr("show"), st.showChoropleth, "show choropleth").SendRespVal(&st.showChoropleth)
	if !st.showChoropleth {
		c.Label("(toggle on to render)").Send()
		return
	}
	cells := getDemoChoroplethCells()
	if len(cells) == 0 {
		c.Label("(h3 runtime not ready)").Send()
		return
	}
	for range c.IdScope(ids.PrepareStr("choropleth-map")) {
		if st.choro == nil {
			st.choro = portolan.New(ids, portolan.Options{
				NoTiles: true, HideAttribution: true,
				Center: portolan.LL(demoMapCenterLat, demoMapCenterLon), Zoom: 11,
			})
		}
		n := len(cells)
		fills := make([]color.Color, n)
		for i := range n {
			t := float64(i) / float64(n-1)
			t += 0.1 * math.Sin(float64(st.frame)*0.04+float64(i)*0.3)
			fills[i] = color.Hex(heatmapPalette(math.Max(0, math.Min(1, t))))
		}
		ctx := context.Background()
		st.choro.Render(portolanDemoW, choroplethDemoH, func(p portolan.Projector) {
			if err := st.cells.Cells(ctx, p, h3Handle, cells, fills, color.Hex(0x20202080), 0.5); err != nil {
				log.Warn().Err(err).Msg("choropleth cells")
			}
		})
	}
}

// computeHeatmapColors evaluates the synthetic world-space value at each
// cell's centroid and colour-maps it (alpha 0xff; the caller sets its own).
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
		rgbas[i] = heatmapPalette(syntheticValue(lats[i], lngs[i]))
	}
	return rgbas
}

// syntheticValue is a fixed world-space function in 0..1: (1+sin)(1+cos)/4
// varies smoothly across continents, so the heatmap visibly changes under a
// long pan and stays pinned in world space.
func syntheticValue(latDeg, lngDeg float64) float64 {
	const k = 0.15 // spatial frequency; ~6° half-wavelength
	return (1 + math.Sin(latDeg*k)) * (1 + math.Cos(lngDeg*k)) / 4.0
}

// heatmapPalette is a discrete 9-stop ramp, packed 0xRRGGBBAA with alpha 0xff.
func heatmapPalette(t float64) uint32 {
	palette := [...]uint32{
		0x2b83baff, 0x5ba9ceff, 0x91d3e0ff, 0xc7e9b4ff,
		0xffffbfff, 0xfecc5cff, 0xfd8d3cff, 0xe6550dff,
		0xa50026ff,
	}
	t = math.Max(0, math.Min(1, t))
	return palette[min(int(t*float64(len(palette)-1)), len(palette)-1)]
}

// =============================================================================
// mapRaster demo (ADR-0096) — an RGBA framebuffer (a synthetic gradient
// standing in for an in-DB-rendered tile) pinned to a lat/lon bbox and
// composited on a NoTiles map through Projector.Image. NoTiles keeps it
// network-free and tour-capturable.
// =============================================================================

const (
	rasterDemoW         = 320
	rasterDemoH         = 256
	rasterDemoMinLat    = 51.049
	rasterDemoMaxLat    = 51.149
	rasterDemoMinLon    = 16.957
	rasterDemoMaxLon    = 17.117
	rasterDemoCenterLat = (rasterDemoMinLat + rasterDemoMaxLat) / 2
	rasterDemoCenterLon = (rasterDemoMinLon + rasterDemoMaxLon) / 2
)

// getDemoRasterPixels builds a fixed, orientation-revealing pattern once: a red
// band along the north edge, a green band along the west edge, a red (west →
// east) / blue (north → south) gradient, and dark grid lines every 32 px. If
// the overlay projects correctly the red band sits at the map's top (north)
// and the green band at its left (west) — a visible check that row 0 is north.
var (
	rasterDemoPixelsOnce sync.Once
	rasterDemoPixels     []uint32
)

func getDemoRasterPixels() (w int, h int, pixels []uint32) {
	rasterDemoPixelsOnce.Do(func() {
		rasterDemoPixels = make([]uint32, rasterDemoW*rasterDemoH)
		for y := range rasterDemoH {
			for x := range rasterDemoW {
				r := uint32(x * 255 / (rasterDemoW - 1)) // west → east
				g := uint32(0)
				b := uint32(y * 255 / (rasterDemoH - 1)) // north → south (row 0 = north)
				switch {
				case y < 18:
					r, g, b = 0xff, 0x22, 0x22 // north band
				case x < 18:
					r, g, b = 0x22, 0xff, 0x22 // west band
				case x%32 == 0 || y%32 == 0:
					r, g, b = 0x10, 0x10, 0x10 // grid
				}
				rasterDemoPixels[y*rasterDemoW+x] = (r << 24) | (g << 16) | (b << 8) | 0xff
			}
		}
	})
	return rasterDemoW, rasterDemoH, rasterDemoPixels
}

type rasterDemoState struct {
	m       *portolan.Map
	opacity float64
}

// demoMapRaster paints the raster pinned to its bbox on a NoTiles map framing
// it. The full pixel buffer is handed over every frame; Projector.Image ships
// it once (its content version is constant) and an empty slice afterwards,
// which is the idiom a live panel uses too.
func demoMapRaster(ids *c.WidgetIdStack, st *rasterDemoState) {
	c.SliderF64(ids.PrepareStr("mapraster-opacity"), st.opacity, 0.1, 1.0).Text("opacity").SendRespVal(&st.opacity)
	c.Label("Synthetic raster pinned to a bbox: red band = north, green band = west.").Send()
	if st.m == nil {
		st.m = portolan.New(ids, portolan.Options{
			NoTiles: true, HideAttribution: true,
			Center: portolan.LL(rasterDemoCenterLat, rasterDemoCenterLon), Zoom: 12,
		})
	}
	w, h, pixels := getDemoRasterPixels()
	bounds := portolan.LatLngBoundsOf(portolan.LL(rasterDemoMinLat, rasterDemoMinLon), portolan.LL(rasterDemoMaxLat, rasterDemoMaxLon))
	st.m.Render(640, 460, func(p portolan.Projector) {
		p.Image("raster", bounds, uint32(w), uint32(h), 1, pixels).Opacity(float32(st.opacity)).Nearest(true).Send()
	})
}
