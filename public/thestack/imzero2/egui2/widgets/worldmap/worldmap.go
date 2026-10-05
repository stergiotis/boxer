// Package worldmap renders a schematic world choropleth: countries from the
// embedded Natural Earth 110m admin-0 asset, filled by a per-country value
// through a colormap, drawn Go-side into a content-versioned texture
// (ADR-0114). Fixed camera — the whole world at once; deliberately no pan, no
// zoom, no tiles. The projection is the caller's pick (Options.Projection):
// Natural Earth, or Equal Earth when the reading needs country areas to be
// comparable. It is a semi-retained widget (ADR-0267): the object keeps the
// raster, its geometry and the hover across frames, New takes its
// [Options], Render draws it once per frame and returns its [Events].
//
// The widget is data-agnostic: callers resolve their own strings via
// Atlas.Resolve and hand a map[CountryIdx]float64 to SetValues.
//
// Map and interaction live in one paintCanvas (ADR-0114 Update 2026-08-01):
// the choropleth ships as a paintImage — still one rasterization per data
// change, not per frame — and the hovered country is outlined over it with the
// concave painter fill. Hover hit-testing maps the canvas-relative pointer
// (R24) onto the per-pixel country index buffer the same rasterization pass
// produces: O(1), no geometry math at frame time.
package worldmap

import (
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
)

const (
	// resizeDebounce is how long a width change must sit still before the map
	// re-rasterizes — a window or slider drag otherwise re-rasters every frame.
	// The stale texture scales into the canvas rect in the meantime.
	resizeDebounce = 150 * time.Millisecond
	maxRasterW     = 2048
	minRasterW     = 128
	defaultRasterW = 960

	// Canvas box bounds, in points. fallbackCanvasW is what the first frame
	// draws at, before the pane-width probe has an answer (the same
	// conservative default the other probe-sized play panels use).
	fallbackCanvasW = 760
	minCanvasW      = 240
	maxCanvasW      = 2048
	// maxCanvasH bounds the aspect-derived height so a very wide pane does not
	// produce a map taller than the leaf; past it the width follows the height
	// back down. A leaf shorter still scrolls.
	maxCanvasH = 900
	// canvasMargin keeps the canvas clear of the pane's right edge (scrollbar).
	canvasMargin = 12

	// highlightStrokeW is the hover outline width in points.
	highlightStrokeW = 2.0
)

// Style is the map's colours, 0xRRGGBBAA; every zero takes the default the
// field doc gives.
type Style struct {
	// Sea is the water fill; the default is transparent, so the pane
	// background reads through.
	Sea uint32
	// NoData fills a country without a value; the default is the
	// faint-border neutral (mid grey on the dark spine, a light grey on a
	// light one).
	NoData uint32
	// Stroke is the border colour; the default is near-black at ~55%,
	// legible on light and dark fills.
	Stroke uint32
	// Presence fills a matched country in presence mode; the default is a
	// viridis-family teal.
	Presence uint32
	// Palette is the choropleth colormap; the default is Viridis8.
	Palette []uint32
	// HighlightFill / HighlightStroke style the hovered country's painter
	// overlay: a translucent wash so microstates still register, and an
	// opaque outline. The wash is what makes the highlight legible on the
	// dark end of a palette, the outline what makes it legible on the light
	// end. Defaults: a white wash light enough to keep the fill readable,
	// over a white outline that survives the palette's light end.
	HighlightFill, HighlightStroke uint32
}

// Options configures a Map (ADR-0267 W11). The widget re-reads [Map.Opts] on
// every Render, so a change is an assignment: a projection change drops the
// raster geometry and re-rasterizes at once (a click, not a drag), a raster
// width change goes through the resize debounce.
type Options struct {
	// Projection is the projection the outlines are drawn under; the zero
	// value is Natural Earth, and an unknown value falls back to it.
	Projection Projection
	// RasterWidth pins the texture's width in pixels (quantized to a
	// multiple of 8, clamped to [128, 2048]; the height follows the
	// projection aspect). Zero, the default, tracks the canvas width so the
	// texture is rasterized at the size it is displayed at. A caller that
	// drives its own resolution control sets it.
	RasterWidth float64
	// Style is the map's colours.
	Style Style
}

// Map is the schematic world choropleth. Construct via New; all methods
// are render-thread-only (the imzero2 single-goroutine contract).
type Map struct {
	// Opts is re-read on every Render; a change is an assignment.
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string
	atlas    *Atlas
	loadErr  error

	// projection is the one drawn; pa is the atlas geometry under it,
	// resolved lazily so the zero Map is usable.
	projection Projection
	pa         *Projected
	// sty is Opts.Style with the defaults filled in (resolve).
	sty Style

	// values is dense per-country (NaN = no data); vmin/vmax the mapped range.
	// presence means the caller supplied membership, not magnitudes: matched
	// countries fill uniformly (Style.Presence) and there is no legend.
	values     []float64
	haveValues bool
	presence   bool
	vmin, vmax float64

	cm      *colormap.Config
	legend  *colorscale.ColorScale
	tracker *c.ImageVersionTracker[string]

	// Raster state: what the texture currently shows. geom is the size-derived
	// half of the raster pass, kept across data changes — a new value set
	// re-runs only the recolour (see rasterizeNow).
	geom    *rasterGeometry
	rgba    []uint32
	index   []CountryIdx
	rw, rh  int
	version uint64
	dirty   bool

	// Resize debounce.
	wantW, wantH int
	wantSince    time.Time

	hovered CountryIdx
	// hxs / hys are the highlight ring scratch, reused across rings and frames.
	hxs, hys []float32
}

// Events is what one Render reports; hover and click come from last frame's
// canvas registers, so both lag one frame — the same lag the readout and
// the highlight are drawn under.
type Events struct {
	// Clicked is the country under a primary click, ClickedOk whether there
	// was one.
	Clicked   CountryIdx
	ClickedOk bool
	// Hovered is the country under the pointer, HoveredOk whether there is
	// one, and HoveredValue its value (NaN when it has no data).
	Hovered      CountryIdx
	HoveredOk    bool
	HoveredValue float64
}

// New constructs the widget. scopeKey seeds the widget ids and the texture
// cache key — unique per instance within the caller's id scope. The embedded
// atlas is parsed on first construction (process-wide once); a parse failure
// is held and rendered as an error label rather than returned, so a broken
// asset degrades to a dead pane instead of failing app construction.
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) *Map {
	atlas, err := LoadAtlas()
	w := &Map{
		Opts:     opts,
		ids:      ids,
		scopeKey: scopeKey,
		atlas:    atlas,
		loadErr:  err,
		tracker:  c.NewImageVersionTracker[string](),
		hovered:  NoCountry,
		wantW:    defaultRasterW,
		dirty:    true,
	}
	w.resolve()
	w.wantH = w.heightFor(w.wantW)
	return w
}

// resolve applies Opts: the style defaults, the projection (with the geometry
// drop a change needs) and a pinned raster width.
func (inst *Map) resolve() {
	o := inst.Opts
	inst.sty = o.Style
	if inst.sty.NoData == 0 {
		inst.sty.NoData = styletokens.NeutralBorderFaint.AsHex()
	}
	if inst.sty.Stroke == 0 {
		inst.sty.Stroke = 0x0a0a0a8c
	}
	if inst.sty.Presence == 0 {
		inst.sty.Presence = 0x2a788eff
	}
	if inst.sty.Palette == nil {
		inst.sty.Palette = colormap.Viridis8
	}
	if inst.sty.HighlightFill == 0 {
		inst.sty.HighlightFill = 0xffffff30
	}
	if inst.sty.HighlightStroke == 0 {
		inst.sty.HighlightStroke = 0xffffffe6
	}
	inst.setProjection(o.Projection)
	if o.RasterWidth > 0 {
		inst.setRasterWidth(o.RasterWidth)
	}
}

// setRasterWidth quantizes and clamps a raster width and starts the resize
// debounce when it changed — the path a pinned Options.RasterWidth and the
// canvas-tracking default both take.
func (inst *Map) setRasterWidth(px float64) {
	wi := min(max(int(px)&^7, minRasterW), maxRasterW)
	if wi != inst.wantW {
		inst.wantW, inst.wantH = wi, inst.heightFor(wi)
		inst.wantSince = time.Now()
	}
}

// RasterWidth returns the current target raster width (for a readout).
func (inst *Map) RasterWidth() float64 { return float64(inst.wantW) }

// setProjection switches the projection the outlines are drawn under. The
// raster geometry is a function of (outlines, size), so this drops it and
// re-rasterizes at once rather than through the resize debounce — a projection
// change is a click, not a drag. The raster height follows the new aspect; the
// values, the palette and the hover state are unaffected. An unknown value
// falls back to the default (see Atlas.Projected).
func (inst *Map) setProjection(p Projection) {
	if !p.Valid() {
		p = ProjectionNaturalEarth
	}
	if p == inst.projection {
		return
	}
	inst.projection = p
	inst.pa = nil
	inst.wantH = inst.heightFor(inst.wantW)
	inst.dirty = true
}

// projected resolves the atlas geometry for the current projection, projecting
// it on first use. The result is cached in the process-wide atlas, so a
// projection flipped back to costs one map read.
func (inst *Map) projected() *Projected {
	if inst.atlas == nil {
		return nil
	}
	if inst.pa == nil || inst.pa.Projection != inst.projection {
		inst.pa = inst.atlas.Projected(inst.projection)
	}
	return inst.pa
}

// canvasBox resolves the on-screen canvas size in points for a box the host
// offers: w is the width to span (the pane less a scrollbar margin in the
// fill case, or exactly what a host asked for), h the height cap, zero for
// none. The height follows the projection aspect, bounded by maxCanvasH and
// by h, with the width pulled back to keep the aspect whenever the height
// binds.
func (inst *Map) canvasBox(w, h float32) (cw, ch int) {
	cw = min(max(int(w), minCanvasW), maxCanvasW)
	ch = inst.heightFor(cw)
	capH := maxCanvasH
	if h > 0 && int(h) < capH {
		capH = max(int(h), 1)
	}
	if ch > capH {
		ch = capH
		cw = min(max(int(float64(ch)*inst.projection.Aspect()), minCanvasW), maxCanvasW)
	}
	return
}

// Atlas exposes the shared country atlas (nil when loading failed) so the
// caller can resolve its identifiers to CountryIdx values.
func (inst *Map) Atlas() *Atlas { return inst.atlas }

// SetValues replaces the choropleth data. Missing countries render in
// Style.NoData. The colormap range is the data min/max; a single-valued or
// empty range widens symmetrically so the palette midpoint is used.
func (inst *Map) SetValues(vals map[CountryIdx]float64) {
	if inst.atlas == nil {
		return
	}
	inst.presence = false
	if inst.values == nil {
		inst.values = make([]float64, len(inst.atlas.Countries))
	}
	for i := range inst.values {
		inst.values[i] = math.NaN()
	}
	vmin := math.Inf(1)
	vmax := math.Inf(-1)
	n := 0
	for idx, v := range vals {
		if idx < 0 || int(idx) >= len(inst.values) || math.IsNaN(v) {
			continue
		}
		inst.values[idx] = v
		if v < vmin {
			vmin = v
		}
		if v > vmax {
			vmax = v
		}
		n++
	}
	inst.haveValues = n > 0
	if !inst.haveValues {
		inst.cm = nil
		inst.legend = nil
		inst.dirty = true
		return
	}
	if !(vmin < vmax) { // degenerate range — NewConfig requires min < max
		vmin, vmax = widenDegenerate(vmin)
	}
	if inst.cm == nil || vmin != inst.vmin || vmax != inst.vmax {
		inst.vmin, inst.vmax = vmin, vmax
		inst.cm = colormap.NewConfig(inst.sty.Palette, vmin, vmax)
		// Compact legend: the map competes for the same vertical space, so
		// the scale stays a narrow strip beside the hover readout. Scoped
		// under this map's own scope, where Render draws it.
		inst.legend = colorscale.New(inst.ids, "legend", inst.cm, colorscale.Options{
			Width: 320, Height: 44,
			LabelFormat: func(v float64) string { return fmt.Sprintf("%.4g", v) },
		})
	}
	inst.dirty = true
}

// widenDegenerate brackets a single value v with a symmetric pad so
// colormap.NewConfig (which requires min < max) accepts it and v lands on the
// palette midpoint. The pad is scaled to v's magnitude: a fixed ±0.5 vanishes
// below the float64 ULP for a large value (a uint64 id/hash near 2^63 has a ULP
// of ~2048), which would leave min == max and panic NewConfig.
func widenDegenerate(v float64) (min, max float64) {
	pad := math.Max(0.5, math.Abs(v)*0x1p-30)
	return v - pad, v + pad
}

// SetPresence replaces the data with membership only: the given countries
// fill uniformly in Style.Presence, everything else is no-data, and no legend
// renders. Used when the caller's result names countries but carries no
// numeric value to grade them by.
func (inst *Map) SetPresence(present map[CountryIdx]bool) {
	if inst.atlas == nil {
		return
	}
	if inst.values == nil {
		inst.values = make([]float64, len(inst.atlas.Countries))
	}
	for i := range inst.values {
		inst.values[i] = math.NaN()
	}
	n := 0
	for idx, on := range present {
		if !on || idx < 0 || int(idx) >= len(inst.values) {
			continue
		}
		inst.values[idx] = 1
		n++
	}
	inst.presence = true
	inst.haveValues = n > 0
	inst.cm = nil
	inst.legend = nil
	inst.dirty = true
}

// ClearValues drops the data: every country renders as no-data.
func (inst *Map) ClearValues() {
	inst.haveValues = false
	inst.presence = false
	inst.cm = nil
	inst.legend = nil
	for i := range inst.values {
		inst.values[i] = math.NaN()
	}
	inst.dirty = true
}

// Hovered returns the country under the pointer (last frame's readout) and
// its value (NaN when the country has no data).
func (inst *Map) Hovered() (idx CountryIdx, value float64, ok bool) {
	if inst.hovered == NoCountry || inst.atlas == nil {
		return NoCountry, math.NaN(), false
	}
	v := math.NaN()
	if int(inst.hovered) < len(inst.values) {
		v = inst.values[inst.hovered]
	}
	return inst.hovered, v, true
}

// Render draws the map, the legend and the hover readout into a box the host
// offers — w is the width the map spans, h a height cap (zero for none), the
// map keeping the projection's aspect inside them — and reports a country
// click and the hover. Hover and click come from last frame's canvas
// registers, so both lag one frame — the same lag the readout and the
// highlight are drawn under.
func (inst *Map) Render(w, h float32) (ev Events) {
	ev.Clicked, ev.Hovered, ev.HoveredValue = NoCountry, NoCountry, math.NaN()
	if inst.loadErr != nil {
		c.Label("world atlas unavailable: " + inst.loadErr.Error()).Wrap().Send()
		return
	}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		ev = inst.frame(w, h)
	}
	return
}

// RenderFill is Render spanning the pane: its width less a scrollbar margin
// and its height as the cap, read back through this instance's own probe
// one frame behind, with the fallbacks serving until it reports (ADR-0267
// W12). A host inside a vertical ScrollArea reads a zero pane height there
// and should pass Render a height of its own instead.
func (inst *Map) RenderFill(fallbackW, fallbackH float32) (ev Events) {
	ev.Clicked, ev.Hovered, ev.HoveredValue = NoCountry, NoCountry, math.NaN()
	if inst.loadErr != nil {
		c.Label("world atlas unavailable: " + inst.loadErr.Error()).Wrap().Send()
		return
	}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		// The probe goes first: the rect is the room left for the next
		// widget, and it answers one frame late.
		w, h, ok := c.CapturePaneSize(inst.ids.ProbeSeq("pane"))
		if !ok || w < 1 {
			w = fallbackW
		}
		if !ok || h < 1 {
			h = fallbackH
		}
		ev = inst.frame(w-canvasMargin, h)
	}
	return
}

// frame runs inside the widget's id scope.
func (inst *Map) frame(boxW, boxH float32) (ev Events) {
	inst.resolve()
	sm := c.CurrentApplicationState.StateManager
	canvasH := widgethandle.Make(inst.ids.PrepareStr("canvas").Derive())
	// The per-canvas pointer row is also the liveness signal: ok=false
	// means this canvas did not render last frame (hidden dock tab, first
	// frame), so neither the hover nor the click below is stale-true.
	cur, live := sm.GetCanvasCursor(canvasH)
	if boxW <= 0 {
		boxW = fallbackCanvasW
	}
	w, h := inst.canvasBox(boxW, boxH)
	inst.updateHover(cur, live, w, h)
	ev.Clicked = NoCountry
	ev.Hovered, ev.HoveredValue, ev.HoveredOk = inst.Hovered()

	for range c.Vertical().KeepIter() {
		// The canvas width drives the raster resolution unless a caller
		// pinned one; either way a pending change re-rasters once the
		// debounce elapses, while data changes (dirty) re-raster at once.
		if inst.Opts.RasterWidth <= 0 {
			inst.setRasterWidth(float64(w))
		}
		if inst.rw != inst.wantW && time.Since(inst.wantSince) >= resizeDebounce {
			inst.dirty = true
		}
		if inst.dirty {
			inst.rasterizeNow(inst.wantW, inst.wantH)
		}
		// Legend + readout share one row above the map. AddSpace rather
		// than a vertical Separator: a rule in a horizontal row sizes to
		// the available height, which balloons inside the dock's
		// unbounded-height ScrollArea.
		for range c.Horizontal().KeepIter() {
			if inst.legend != nil {
				inst.legend.Render()
				c.AddSpace(styletokens.GapSections(styletokens.ActiveDensity()))
			}
			inst.renderReadout()
		}
		c.Separator().Horizontal().Send()
		if inst.rgba != nil {
			inst.paintMap(w, h)
			if live && inst.hovered != NoCountry &&
				sm.GetResponse(canvasH).HasPrimaryClicked() {
				ev.Clicked = inst.hovered
				ev.ClickedOk = true
			}
		}
	}
	return
}

// paintMap draws the choropleth texture and the hover overlay into one canvas
// of (w × h) points. The texture ships with an empty pixel slice while the
// version is unchanged — the host-side cache redraws it — so a still pane
// re-uploads nothing and the per-frame cost is the canvas plus one image
// command, whatever the geometry's complexity.
//
// The version-tracker protocol assumes "sent once" means "uploaded once",
// which a dock breaks: this body runs every frame into a detached buffer,
// but the host interprets only the ACTIVE tab's buffer — a hidden tab's
// upload is discarded, and the idle LRU can evict the texture while the
// widget goes uninterpreted (~10 s). PixelsToSendFor closes the loop via
// the host's starved-texture report (StateManager.TextureStarved): a
// starved id drops the "already sent" record and the full pixels re-ship
// the next frame. Costs one blank frame on tab activation, nothing while
// hidden.
func (inst *Map) paintMap(w, h int) {
	// Two separate PrepareStr creators per id: they derive the same
	// content-based value, but each is a single-use state machine — reusing one
	// across Derive() and the widget call panics ("invalid state transition").
	imgId := inst.ids.PrepareStr("image").Derive()
	pixels := inst.tracker.PixelsToSendFor("image", imgId, inst.version, inst.rgba)
	c.PaintImage(imgId, 0, 0, float32(w), float32(h),
		uint32(inst.rw), uint32(inst.rh), inst.version, pixels).
		Send()
	inst.paintHighlight(w, h)
	// Sense hover as well as click: the pointer row is only pushed for a
	// canvas whose response can report containment.
	c.PaintCanvas(inst.ids.PrepareStr("canvas"), float32(w), float32(h)).
		Sense(true, false, true).
		Send()
}

// paintHighlight outlines the hovered country over the texture. This is the
// one thing the raster cannot do: a highlight baked into the texture would
// cost a full re-rasterization and re-upload per pointer move, where the
// painter redraws it for the price of one country's rings (a few dozen points
// for most, ~800 for the largest).
//
// Rings are filled with the concave painter fill — country outlines are
// non-convex, which the convex fan-fill renders wrong. The fill has no hole
// support, so an interior ring is outlined only: filling it would wash the
// enclave it excludes (South Africa's Lesotho, the asset's only hole).
func (inst *Map) paintHighlight(w, h int) {
	if inst.hovered == NoCountry || inst.atlas == nil ||
		int(inst.hovered) >= len(inst.atlas.Countries) {
		return
	}
	ct := &inst.atlas.Countries[inst.hovered]
	rings := inst.projected().Rings(inst.hovered)
	fw, fh := float32(w), float32(h)
	fill := color.Hex(inst.sty.HighlightFill)
	stroke := color.Hex(inst.sty.HighlightStroke)
	for i, ring := range rings {
		// GeoJSON rings repeat their first point to close. The polyline needs
		// that repeat, the fill does not: a duplicated vertex is a zero-length
		// edge for the ear clipper and for the closed outline it draws.
		hole := i < len(ct.ringHole) && ct.ringHole[i]
		n := len(ring)
		if !hole && n > 1 && ring[n-1] == ring[0] {
			n--
		}
		if n < 2 {
			continue
		}
		inst.hxs = inst.hxs[:0]
		inst.hys = inst.hys[:0]
		for _, p := range ring[:n] {
			inst.hxs = append(inst.hxs, p.X*fw)
			inst.hys = append(inst.hys, p.Y*fh)
		}
		if hole {
			c.PaintPolyline(inst.hxs, inst.hys, stroke, highlightStrokeW).Send()
			continue
		}
		if n < 3 {
			continue
		}
		c.PaintPolygonFilled(inst.hxs, inst.hys, fill).
			Concave().
			Stroke(stroke, highlightStrokeW).
			Send()
	}
}

// updateHover resolves the country under last frame's canvas-relative pointer
// through the rasterization pass's per-pixel index buffer — one array load, no
// geometry math at frame time. live=false (canvas not rendered last frame)
// and a pointer outside the canvas both read as "nothing hovered".
func (inst *Map) updateHover(cur c.CanvasCursorValue, live bool, w, h int) {
	inst.hovered = NoCountry
	if !live || inst.index == nil || w <= 0 || h <= 0 || inst.rw <= 0 || inst.rh <= 0 {
		return
	}
	x, y := float64(cur.PosX), float64(cur.PosY)
	if math.IsNaN(x) || math.IsNaN(y) || x < 0 || y < 0 {
		return
	}
	col := int(x / float64(w) * float64(inst.rw))
	row := int(y / float64(h) * float64(inst.rh))
	if col < 0 || row < 0 || col >= inst.rw || row >= inst.rh {
		return
	}
	inst.hovered = inst.index[row*inst.rw+col]
}

// renderReadout is the one-line hover status under the legend. In presence
// mode the value is synthetic (1), so only membership is worded.
func (inst *Map) renderReadout() {
	text := "hover a country"
	if idx, v, ok := inst.Hovered(); ok {
		ct := &inst.atlas.Countries[idx]
		switch {
		case math.IsNaN(v):
			text = ct.Label() + " · no data"
		case inst.presence:
			text = ct.Label() + " · in result"
		default:
			text = fmt.Sprintf("%s · %.6g", ct.Label(), v)
		}
	}
	for rt := range c.RichTextLabel(text) {
		rt.Small().Weak()
	}
}

func (inst *Map) heightFor(w int) int {
	return max(int(float64(w)/inst.projection.Aspect()), 1)
}

// rasterizeNow repaints the texture at (w × h) from the current values and
// bumps the content version. Only the size-derived half is expensive, and it
// survives a data change: at an unchanged size and projection this is a
// recolour of a cached geometry, not a re-rasterization. The output buffer is
// reused too, so a value change allocates only the fill table.
func (inst *Map) rasterizeNow(w, h int) {
	fills := make([]uint32, len(inst.atlas.Countries))
	for i := range fills {
		fills[i] = inst.sty.NoData
		if !inst.haveValues || i >= len(inst.values) || math.IsNaN(inst.values[i]) {
			continue
		}
		switch {
		case inst.presence:
			fills[i] = inst.sty.Presence
		case inst.cm != nil:
			fills[i] = inst.cm.At(inst.values[i])
		}
	}
	pa := inst.projected()
	if inst.geom == nil || inst.geom.w != w || inst.geom.h != h || inst.geom.proj != pa.Projection {
		inst.geom = buildRasterGeometry(pa, w, h)
		inst.rgba = make([]uint32, w*h)
		inst.index = inst.geom.index
	}
	inst.geom.resolve(inst.rgba, rasterStyle{
		fills:  fills,
		sea:    inst.sty.Sea,
		stroke: inst.sty.Stroke,
	})
	inst.rw, inst.rh = w, h
	inst.version++
	inst.dirty = false
}
