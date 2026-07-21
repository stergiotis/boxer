// Package worldmap renders a schematic world choropleth: countries from the
// embedded Natural Earth 110m admin-0 asset, filled by a per-country value
// through a colormap, drawn Go-side into a content-versioned Image texture
// (ADR-0114). Fixed camera — the whole world, fit to the pane; deliberately
// no pan, no zoom, no tiles.
//
// The widget is data-agnostic: callers resolve their own strings via
// Atlas.Resolve and hand a map[CountryIdx]float64 to SetValues. Hover
// hit-testing reads the Image widget's texture-space hover readout against a
// per-pixel index buffer produced by the same rasterization pass — O(1), no
// geometry math at frame time.
package worldmap

import (
	"fmt"
	"math"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
)

const (
	// resizeDebounce is how long a width change must sit still before the map
	// re-rasterizes — a slider drag otherwise re-rasters every frame. The
	// stale texture scales (FitAspectMax) in the meantime.
	resizeDebounce = 150 * time.Millisecond
	maxRasterW     = 2048
	minRasterW     = 128
	defaultRasterW = 960
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

	// Raster state: what the texture currently shows.
	rgba    []uint32
	index   []CountryIdx
	rw, rh  int
	version uint64
	dirty   bool

	// Resize debounce.
	wantW, wantH int
	wantSince    time.Time

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

	hoverRc uint64
	hovered CountryIdx
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
		Palette:      colormap.Viridis8,
		tracker:      c.NewImageVersionTracker[string](),
		hovered:      NoCountry,
		wantW:        defaultRasterW,
		dirty:        true,
	}
	w.wantH = w.heightFor(defaultRasterW)
	return w
}

// SetPixelWidth sets the raster width (quantized to a multiple of 8, clamped
// to [128, 2048]; height follows the projection aspect). Sizing is an explicit
// caller control rather than an available-size capture: the R18 capture
// register is a single global slot (last capture wins), already owned by
// other panes in the host apps — the same reason the play Map panel sizes
// explicitly. Re-rasterization is debounced so a slider drag re-rasters once
// at rest.
func (inst *Widget) SetPixelWidth(px float64) {
	wi := min(max(int(px)&^7, minRasterW), maxRasterW)
	if wi != inst.wantW {
		inst.wantW, inst.wantH = wi, inst.heightFor(wi)
		inst.wantSince = time.Now()
	}
}

// PixelWidth returns the current target raster width (for binding a control).
func (inst *Widget) PixelWidth() float64 { return float64(inst.wantW) }

// SetDisplayHeight caps the map's on-screen height in points; the width then
// follows the projection aspect (FitAspectMax against the available width).
// Pass 0 (the default) to fill the available pane instead — correct inside a
// bounded leaf such as the play World tab. Set a finite height when the widget
// renders inside a vertical ScrollArea (e.g. the widget gallery): there the
// available height is unreliable — it reads ~0 whenever the pane is scrolled
// low — and the zero-box fit would collapse the map to nothing. Display size
// is independent of the raster resolution set by SetPixelWidth.
func (inst *Widget) SetDisplayHeight(px float64) {
	if px <= 0 {
		inst.displayH = 0
		return
	}
	inst.displayH = max(int(px), 1)
}

// SetDisplayWidth sets the map's on-screen width in points; the height then
// follows the projection aspect. Pass 0 (the default) to defer to
// SetDisplayHeight / fill-available sizing. A finite width takes precedence
// over SetDisplayHeight and needs no available-space read, so it is both the
// robust choice inside a vertical ScrollArea (see SetDisplayHeight for why the
// available height is unreliable there) and the way to make an on-screen width
// control actually resize the map — a fill-available map ignores the width knob
// because it always spans the pane. Display size is independent of the raster
// resolution set by SetPixelWidth.
func (inst *Widget) SetDisplayWidth(px float64) {
	if px <= 0 {
		inst.displayW = 0
		return
	}
	inst.displayW = max(int(px), 1)
}

// fitBox is the (fixedW, fixedH) bounding box handed to the Image widget under
// FitAspectMax. An explicit SetDisplayWidth wins — the box carries the map's
// own aspect (width × width/aspect) so the render lands at exactly that width
// regardless of available space. Otherwise the map fills the available width
// (fixedW 0) capped by displayH (0 = fill the available height too).
func (inst *Widget) fitBox() (w, h uint32) {
	if inst.displayW > 0 {
		return uint32(inst.displayW), uint32(inst.heightFor(inst.displayW))
	}
	return 0, uint32(inst.displayH)
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
	if vmin == vmax { // degenerate range — NewConfig requires min < max
		vmin -= 0.5
		vmax += 0.5
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
// the caller reacts in the same frame. Layout: the map fills the available
// width at the projection's aspect, capped by the available height minus the
// legend reserve — or renders at exactly SetDisplayWidth when the caller set
// one.
func (inst *Widget) Render() (clicked CountryIdx, clickedOk bool) {
	clicked = NoCountry
	if inst.loadErr != nil {
		c.Label("world atlas unavailable: " + inst.loadErr.Error()).Wrap().Send()
		return
	}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		for range c.Vertical().KeepIter() {
			// A pending width change re-rasters once the debounce elapses;
			// data changes (dirty) re-raster immediately at the current size.
			if inst.rw != inst.wantW && time.Since(inst.wantSince) >= resizeDebounce {
				inst.dirty = true
			}
			if inst.dirty {
				inst.rasterizeNow(inst.wantW, inst.wantH)
			}
			// Legend + readout share one row ABOVE the map: the image scales
			// into whatever is left (zero-box FitAspectMax is greedy), so
			// anything placed after it would be pushed out of the pane.
			for range c.Horizontal().KeepIter() {
				if inst.legend != nil {
					inst.legend.Render()
					c.Separator().Vertical().Send()
				}
				inst.renderReadout()
			}
			if inst.rgba != nil {
				resp := inst.renderImage()
				if inst.hovered != NoCountry && resp.HasPrimaryClicked() {
					clicked = inst.hovered
					clickedOk = true
				}
			}
		}
	}
	return
}

// renderImage ships the texture (empty pixel slice while the version is
// unchanged — the Rust-side cache redraws), refreshes the hover readout, and
// returns the image's response flags for click detection. The hover readout
// is texture-pixel space regardless of fit, so the index lookup needs no
// screen-space math.
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
func (inst *Widget) renderImage() c.ResponseFlagsE {
	// Two separate PrepareStr creators: they derive the same content-based
	// value, but each is a single-use state machine — reusing one across
	// Derive() and the Image call panics ("invalid state transition").
	imgId := inst.ids.PrepareStr(inst.scopeKey + "-img").Derive()
	pixels := inst.tracker.PixelsToSendFor(inst.scopeKey+"-img", imgId, inst.version, inst.rgba)
	// FitAspectMax scales the texture aspect-preserved into a (fixedW × fixedH)
	// bounding box. SetPixelWidth sets raster *resolution*; the box (see
	// fitBox) sets display size — the two are independent. The default box
	// fills the available width (fixedW 0) capped by displayH; an explicit
	// SetDisplayWidth overrides it to render at exactly that width. The hover
	// readout stays texture-space under any fit, so hit-testing is unaffected.
	fitW, fitH := inst.fitBox()
	resp := c.Image(inst.ids.PrepareStr(inst.scopeKey+"-img"),
		uint32(inst.rw), uint32(inst.rh), inst.version,
		uint8(c.FitAspectMaxE), fitW, fitH,
		uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
		SendRespHoverPx(&inst.hoverRc)
	inst.hovered = NoCountry
	if row, col, hovered := c.UnpackHoverRc(inst.hoverRc); hovered {
		if int(row) < inst.rh && int(col) < inst.rw {
			inst.hovered = inst.index[int(row)*inst.rw+int(col)]
		}
	}
	return resp
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

// rasterizeNow rebuilds the texture + index buffer at (w × h) from the
// current values and bumps the content version.
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
	inst.rgba, inst.index = rasterize(inst.atlas, w, h, rasterStyle{
		fills:  fills,
		sea:    inst.SeaRGBA,
		stroke: inst.StrokeRGBA,
	})
	inst.rw, inst.rh = w, h
	inst.version++
	inst.dirty = false
}
