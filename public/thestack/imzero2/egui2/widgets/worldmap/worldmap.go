// Package worldmap renders a schematic world choropleth: countries from the
// embedded Natural Earth 110m admin-0 asset, filled by a per-country value
// through a colormap, drawn Go-side into a content-versioned texture
// (ADR-0114). Fixed camera — the whole world at once; deliberately no pan, no
// zoom, no tiles.
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

// Widget is the schematic world choropleth. Construct via New; all methods
// are render-thread-only (the imzero2 single-goroutine contract).
type Widget struct {
	ids      *c.WidgetIdStack
	scopeKey string
	atlas    *Atlas
	loadErr  error

	// Style knobs, settable before the first Render. Colors are 0xRRGGBBAA.
	SeaRGBA      uint32
	NoDataRGBA   uint32
	StrokeRGBA   uint32
	PresenceRGBA uint32
	Palette      []uint32

	// HighlightFillRGBA / HighlightStrokeRGBA style the hovered country's
	// painter overlay: a translucent wash so microstates still register, and
	// an opaque outline. The wash is what makes the highlight legible on the
	// dark end of a palette, the outline what makes it legible on the light
	// end.
	HighlightFillRGBA   uint32
	HighlightStrokeRGBA uint32

	// values is dense per-country (NaN = no data); vmin/vmax the mapped range.
	// presence means the caller supplied membership, not magnitudes: matched
	// countries fill uniformly (PresenceRGBA) and there is no legend.
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

	// autoRasterW tracks the canvas width with the raster width, so the
	// texture is drawn at the size it is displayed at. SetPixelWidth turns it
	// off — an explicit resolution is the caller's to own.
	autoRasterW bool

	// displayH caps the map's on-screen height in points (0 = fill the
	// available pane). See SetDisplayHeight — a caller inside a vertical
	// ScrollArea must set it, because the pane-fills-available default reads a
	// zero available height there and the map collapses to nothing.
	displayH int

	// displayW sets the map's on-screen width in points (0 = defer to displayH
	// / fill-available). When > 0 it takes precedence over displayH: the map
	// renders at exactly this width with the height derived from the projection
	// aspect, so the on-screen size needs no available-space read. See
	// SetDisplayWidth.
	displayW int

	hovered CountryIdx
	// hxs / hys are the highlight ring scratch, reused across rings and frames.
	hxs, hys []float32
}

// New constructs the widget. scopeKey seeds the widget ids and the texture
// cache key — unique per instance within the caller's id scope. The embedded
// atlas is parsed on first construction (process-wide once); a parse failure
// is held and rendered as an error label rather than returned, so a broken
// asset degrades to a dead pane instead of failing app construction.
func New(ids *c.WidgetIdStack, scopeKey string) *Widget {
	atlas, err := LoadAtlas()
	w := &Widget{
		ids:      ids,
		scopeKey: scopeKey,
		atlas:    atlas,
		loadErr:  err,
		// Sea transparent (the pane background reads through), undata mid
		// gray, borders near-black at ~55% — legible on light and dark fills.
		// Presence fill is a viridis-family teal.
		SeaRGBA:      0x00000000,
		NoDataRGBA:   0x555555ff,
		StrokeRGBA:   0x0a0a0a8c,
		PresenceRGBA: 0x2a788eff,
		// Hover: a white wash light enough to keep the underlying fill
		// readable, over a white outline that survives the palette's light end.
		HighlightFillRGBA:   0xffffff30,
		HighlightStrokeRGBA: 0xffffffe6,
		Palette:             colormap.Viridis8,
		tracker:             c.NewImageVersionTracker[string](),
		hovered:             NoCountry,
		wantW:               defaultRasterW,
		autoRasterW:         true,
		dirty:               true,
	}
	w.wantH = w.heightFor(defaultRasterW)
	return w
}

// SetPixelWidth pins the raster width (quantized to a multiple of 8, clamped
// to [128, 2048]; height follows the projection aspect) and stops it tracking
// the canvas. Callers that drive their own resolution control want this; the
// default is to follow the canvas so the texture is rasterized at the size it
// is displayed at. Re-rasterization is debounced either way, so a drag
// re-rasters once at rest.
func (inst *Widget) SetPixelWidth(px float64) {
	inst.autoRasterW = false
	inst.setRasterWidth(px)
}

// setRasterWidth is SetPixelWidth without the ownership switch — the path the
// canvas-tracking default also takes.
func (inst *Widget) setRasterWidth(px float64) {
	wi := min(max(int(px)&^7, minRasterW), maxRasterW)
	if wi != inst.wantW {
		inst.wantW, inst.wantH = wi, inst.heightFor(wi)
		inst.wantSince = time.Now()
	}
}

// PixelWidth returns the current target raster width (for binding a control).
func (inst *Widget) PixelWidth() float64 { return float64(inst.wantW) }

// SetDisplayHeight caps the map's on-screen height in points; the width then
// follows the projection aspect. Pass 0 (the default) to let the height follow
// the pane width instead. Display size is independent of the raster resolution
// set by SetPixelWidth.
func (inst *Widget) SetDisplayHeight(px float64) {
	if px <= 0 {
		inst.displayH = 0
		return
	}
	inst.displayH = max(int(px), 1)
}

// SetDisplayWidth sets the map's on-screen width in points; the height then
// follows the projection aspect. Pass 0 (the default) to size from the pane
// width instead. A finite width takes precedence over SetDisplayHeight and
// needs no probe, so it is the way to make an on-screen width control actually
// resize the map. Display size is independent of the raster resolution set by
// SetPixelWidth.
func (inst *Widget) SetDisplayWidth(px float64) {
	if px <= 0 {
		inst.displayW = 0
		return
	}
	inst.displayW = max(int(px), 1)
}

// canvasBox resolves the on-screen canvas size in points. An explicit
// SetDisplayWidth wins; otherwise the box spans the pane width read back from
// the ui-rect probe (R21 — a per-seq append register, so unlike the single-slot
// available-size capture it does not contend with whatever else the host app
// sizes from; ADR-0114 Update 2026-08-01). One frame of lag: the first frame
// draws at fallbackCanvasW. The height follows the projection aspect, bounded
// by maxCanvasH and by SetDisplayHeight, with the width pulled back to keep the
// aspect whenever the height binds.
func (inst *Widget) canvasBox(paneW float32) (w, h int) {
	if inst.displayW > 0 {
		w = inst.displayW
	} else {
		w = int(paneW) - canvasMargin
	}
	w = min(max(w, minCanvasW), maxCanvasW)
	h = inst.heightFor(w)
	capH := maxCanvasH
	if inst.displayH > 0 && inst.displayH < capH {
		capH = inst.displayH
	}
	if h > capH {
		h = capH
		w = min(max(int(float64(h)*ProjectionAspect()), minCanvasW), maxCanvasW)
	}
	return
}

// paneWidth reads back the previous frame's ui-rect probe. ok=false on the
// first frame (and in any frame where this widget went uninterpreted), which
// the caller answers with fallbackCanvasW.
func (inst *Widget) paneWidth(sm *c.StateManager) float32 {
	if r, ok := sm.GetUiRect(inst.probeSeq()); ok && r.MaxX > r.MinX {
		return r.MaxX - r.MinX
	}
	return fallbackCanvasW
}

// probeSeq is the ui-rect probe's key: derived from the widget's own scope, so
// two worldmaps in one app never share a slot.
func (inst *Widget) probeSeq() uint64 {
	return inst.ids.PrepareStr(inst.scopeKey + "-paneprobe").Derive()
}

// Atlas exposes the shared country atlas (nil when loading failed) so the
// caller can resolve its identifiers to CountryIdx values.
func (inst *Widget) Atlas() *Atlas { return inst.atlas }

// SetValues replaces the choropleth data. Missing countries render in
// NoDataRGBA. The colormap range is the data min/max; a single-valued or
// empty range widens symmetrically so the palette midpoint is used.
func (inst *Widget) SetValues(vals map[CountryIdx]float64) {
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
		inst.cm = colormap.NewConfig(inst.Palette, vmin, vmax)
		// Compact legend: the map competes for the same vertical space, so
		// the scale stays a narrow strip beside the hover readout.
		inst.legend = colorscale.New(c.NewWidgetIdStack(), inst.scopeKey+"-legend", inst.cm,
			colorscale.WithOrientation(colorscale.OrientationHorizontal),
			colorscale.WithSize(320, 44),
			colorscale.WithLabelFormat(func(v float64) string { return fmt.Sprintf("%.4g", v) }),
		)
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
// fill uniformly in PresenceRGBA, everything else is no-data, and no legend
// renders. Used when the caller's result names countries but carries no
// numeric value to grade them by.
func (inst *Widget) SetPresence(present map[CountryIdx]bool) {
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
func (inst *Widget) ClearValues() {
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
func (inst *Widget) Hovered() (idx CountryIdx, value float64, ok bool) {
	if inst.hovered == NoCountry || inst.atlas == nil {
		return NoCountry, math.NaN(), false
	}
	v := math.NaN()
	if int(inst.hovered) < len(inst.values) {
		v = inst.values[inst.hovered]
	}
	return inst.hovered, v, true
}

// Render draws the map, the legend and the hover readout, and reports a
// country click (primary button over a country) — immediate-mode style, so
// the caller reacts in the same frame. Layout: the map spans the pane width at
// the projection's aspect, or renders at exactly SetDisplayWidth when the
// caller set one.
//
// Hover and click come from last frame's canvas registers, so both lag one
// frame — the same lag the readout and the highlight are drawn under.
func (inst *Widget) Render() (clicked CountryIdx, clickedOk bool) {
	clicked = NoCountry
	if inst.loadErr != nil {
		c.Label("world atlas unavailable: " + inst.loadErr.Error()).Wrap().Send()
		return
	}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		sm := c.CurrentApplicationState.StateManager
		canvasH := widgethandle.Make(inst.ids.PrepareStr(inst.scopeKey + "-canvas").Derive())
		// The per-canvas pointer row is also the liveness signal: ok=false
		// means this canvas did not render last frame (hidden dock tab, first
		// frame), so neither the hover nor the click below is stale-true.
		cur, live := sm.GetCanvasCursor(canvasH)
		w, h := inst.canvasBox(inst.paneWidth(sm))
		inst.updateHover(cur, live, w, h)

		for range c.Vertical().KeepIter() {
			// The canvas width drives the raster resolution unless a caller
			// pinned one; either way a pending change re-rasters once the
			// debounce elapses, while data changes (dirty) re-raster at once.
			if inst.autoRasterW {
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
			// The probe reads the pane width for the NEXT frame: a horizontal
			// separator spans the pane, so the enclosing ui's min_rect does too.
			c.Separator().Horizontal().Send()
			c.CaptureUiRect(inst.probeSeq())
			if inst.rgba != nil {
				inst.paintMap(w, h)
				if live && inst.hovered != NoCountry &&
					sm.GetResponse(canvasH).HasPrimaryClicked() {
					clicked = inst.hovered
					clickedOk = true
				}
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
func (inst *Widget) paintMap(w, h int) {
	// Two separate PrepareStr creators per id: they derive the same
	// content-based value, but each is a single-use state machine — reusing one
	// across Derive() and the widget call panics ("invalid state transition").
	imgId := inst.ids.PrepareStr(inst.scopeKey + "-img").Derive()
	pixels := inst.tracker.PixelsToSendFor(inst.scopeKey+"-img", imgId, inst.version, inst.rgba)
	c.PaintImage(imgId, 0, 0, float32(w), float32(h),
		uint32(inst.rw), uint32(inst.rh), inst.version, pixels).
		Send()
	inst.paintHighlight(w, h)
	// Sense hover as well as click: the pointer row is only pushed for a
	// canvas whose response can report containment.
	c.PaintCanvas(inst.ids.PrepareStr(inst.scopeKey+"-canvas"), float32(w), float32(h)).
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
func (inst *Widget) paintHighlight(w, h int) {
	if inst.hovered == NoCountry || inst.atlas == nil ||
		int(inst.hovered) >= len(inst.atlas.Countries) {
		return
	}
	ct := &inst.atlas.Countries[inst.hovered]
	fw, fh := float32(w), float32(h)
	fill := color.Hex(inst.HighlightFillRGBA)
	stroke := color.Hex(inst.HighlightStrokeRGBA)
	for i, ring := range ct.rings {
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
func (inst *Widget) updateHover(cur c.CanvasCursorValue, live bool, w, h int) {
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
func (inst *Widget) renderReadout() {
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

func (inst *Widget) heightFor(w int) int {
	return max(int(float64(w)/ProjectionAspect()), 1)
}

// rasterizeNow repaints the texture at (w × h) from the current values and
// bumps the content version. Only the size-derived half is expensive, and it
// survives a data change: at an unchanged size this is a recolour of a cached
// geometry, not a re-rasterization. The output buffer is reused too, so a
// value change allocates only the fill table.
func (inst *Widget) rasterizeNow(w, h int) {
	fills := make([]uint32, len(inst.atlas.Countries))
	for i := range fills {
		fills[i] = inst.NoDataRGBA
		if !inst.haveValues || i >= len(inst.values) || math.IsNaN(inst.values[i]) {
			continue
		}
		switch {
		case inst.presence:
			fills[i] = inst.PresenceRGBA
		case inst.cm != nil:
			fills[i] = inst.cm.At(inst.values[i])
		}
	}
	if inst.geom == nil || inst.geom.w != w || inst.geom.h != h {
		inst.geom = buildRasterGeometry(inst.atlas, w, h)
		inst.rgba = make([]uint32, w*h)
		inst.index = inst.geom.index
	}
	inst.geom.resolve(inst.rgba, rasterStyle{
		fills:  fills,
		sea:    inst.SeaRGBA,
		stroke: inst.StrokeRGBA,
	})
	inst.rw, inst.rh = w, h
	inst.version++
	inst.dirty = false
}
