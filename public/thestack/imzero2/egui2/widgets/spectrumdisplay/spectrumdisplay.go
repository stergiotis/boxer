// Package spectrumdisplay renders a spectrum-analyzer display: a scrolling
// waterfall with a labelled frequency axis (engineering Hz/kHz/MHz units), a
// power/dB colorbar legend, an optional spectrum-line trace, frequency/time
// annotations (markers and named regions), and a cursor readout in physical
// units. It composes existing widgets — it owns a
// [heatmapscroll.HeatmapScroll] for the waterfall and a vertical
// [colorscale.ColorScale] for the colorbar, both bound to one
// [colormap.Config] so they stay in lock-step — and paints the axes,
// annotations, and cursor on painter overlays placed with AllocateUiAtRect.
// No Rust changes are involved; the substrate is the same painter idiom as
// treemap and gauge. See ADR-0091.
//
// The caller owns the data and the physical ranges. Each frame it sets the
// frequency and power axes, pushes one column of dB samples, and renders:
//
//	sd := spectrumdisplay.New(ids, "rx", cfg, 256, 512)
//	// ... each frame:
//	sd.SetFrequencyAxis(spectrumdisplay.AxisSpec{Min: 868.59e6, Max: 871.59e6, Unit: spectrumdisplay.AxisUnitHertz})
//	sd.SetPowerAxis(spectrumdisplay.AxisSpec{Min: -110, Max: -20, Unit: spectrumdisplay.AxisUnitDecibel, UnitLabel: "dBFS"})
//	sd.SetWaterfallRange(-110, -20)
//	sd.PushColumn(magsDB)
//	sd.SetDisplaySize(0, 0) // fill the available area
//	sd.Render()
//	if r := sd.Readout(); r.Ok { /* r.Freq, r.Db, r.Age */ }
//
// The cursor readout and any markers carry the one-frame hover lag inherent to
// the canvas-pointer/HoveredCell capture (the colorscale/heatmapscroll
// discipline). Not goroutine-safe; drive from the UI goroutine.
package spectrumdisplay

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/axisruler"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/heatmapscroll"
)

// Package-level defaults exposed as vars so callers can globally tweak them.
var (
	// DefaultSize is the fallback widget size [w,h] in logical pixels, used when
	// SetDisplaySize is auto (0,0) and the available size is not yet known.
	DefaultSize = [2]float32{640, 400}
	// DefaultLeftGutterW is the fallback left-gutter width (logical px) used when
	// no power/time axis is set so there are no labels to measure; otherwise the
	// width is derived per frame from the widest label (leftGutterWidth, the
	// ADR-0091 §SD2 "widest label" rule). DefaultFreqGutterH reserves the bottom
	// gutter; DefaultColorbarW the colorbar.
	DefaultLeftGutterW = float32(48)
	DefaultFreqGutterH = float32(22)
	DefaultColorbarW   = float32(60)
	// DefaultFontSize is the axis-label font size in logical pixels.
	DefaultFontSize = float32(11)

	// Colors are sourced from the IDS neutral spine / semantic palette
	// (ADR-0031) rather than ad-hoc hex, so the display reads as part of the
	// fleet and its chrome matches the surrounding panel — the gutters, the line
	// panel, and the colorbar all paint NeutralBgPanel, so the composite shows no
	// internal seams (ADR-0091 §Update 2026-06-21). The axis treatment mirrors
	// the timeline's, since both now render through axisruler.
	DefaultBg              = styletokens.NeutralBgPanel.AsHex()       // chrome bg (gutters, line panel)
	DefaultAxisColor       = styletokens.NeutralBorderFaint.AsHex()   // axis baselines + tick marks
	DefaultLabelColor      = styletokens.NeutralTextSecondary.AsHex() // axis tick labels
	DefaultGridColor       = styletokens.NeutralBorderFaint.AsHex()   // line-panel grid lines
	DefaultMarkerColor     = styletokens.AccentDefault.AsHex()        // marker line (annotation)
	DefaultCursorColor     = withAlpha(styletokens.NeutralTextPrimary.AsHex(), 0xcc)
	DefaultRegionColor     = withAlpha(styletokens.AccentSubtle.AsHex(), 0x44)
	DefaultTraceColor      = styletokens.InfoDefault.AsHex()        // spectrum-line trace (single series)
	DefaultAnnotationColor = styletokens.NeutralTextPrimary.AsHex() // marker/region label text
)

// withAlpha replaces the alpha byte of a 0xRRGGBBAA color, so a translucent
// overlay (cursor, region wash) can be derived from an opaque IDS token without
// a raw color literal.
func withAlpha(packed uint32, a uint8) uint32 { return (packed &^ 0xff) | uint32(a) }

// AxisSpec describes one physical axis range the caller supplies each frame.
type AxisSpec struct {
	Min, Max     float64
	Unit         AxisUnitE // engineering-format family for the tick labels
	UnitLabel    string    // axis caption suffix shown once (e.g. "dBFS"); informational in v1
	DesiredTicks int       // 0 ⇒ defaultDesiredTicks
}

// MarkerKindE selects how a Marker is drawn.
type MarkerKindE uint8

const (
	MarkerVertical   MarkerKindE = iota // a frequency marker: a full-height vertical line
	MarkerHorizontal                    // a dB marker (meaningful on the line panel)
	MarkerCrosshair                     // both
)

// Marker is a caller-placed annotation line in physical coordinates.
type Marker struct {
	Kind   MarkerKindE
	Freq   float64 // physical X (Hz) for Vertical / Crosshair
	Db     float64 // physical Y (dB) for Horizontal / Crosshair
	Color  uint32  // 0xRRGGBBAA; 0 ⇒ DefaultMarkerColor
	Label  string  // optional, drawn at the line head
	Dashed bool    // reserved; v1 draws solid
}

// PlacementE selects where a Region band is drawn within the texture height.
type PlacementE uint8

const (
	PlacementFull   PlacementE = iota // span the full height
	PlacementTop                      // top strip only
	PlacementBottom                   // bottom strip only
)

// Region is a shaded, named frequency band (a band-plan entry, a channel, or a
// demodulator passband bracket).
type Region struct {
	StartHz, EndHz float64
	Label          string
	Color          uint32 // 0xRRGGBBAA; 0 ⇒ DefaultRegionColor
	Placement      PlacementE
}

// Readout is the value under the cursor, in physical units. Ok is false when the
// pointer is not over the waterfall. One-frame lag.
type Readout struct {
	Freq    float64 // interpolated across the frequency axis
	Db      float64 // the latest column's value at the hovered frequency bin
	Age     int     // columns back from the newest (0 = newest)
	BinRow  uint32  // hovered frequency-bin index
	RingCol uint32  // hovered ring column
	Ok      bool
}

// SpectrumDisplay is the composite display widget. Construct with New, configure
// per frame, push columns, and call Render once per frame.
type SpectrumDisplay struct {
	ids      *c.WidgetIdStack
	scopeKey string
	cfg      *colormap.Config
	hs       *heatmapscroll.HeatmapScroll
	cbar     *colorscale.ColorScale

	heightSlots uint32

	freqAxis    AxisSpec
	powerAxis   AxisSpec
	timeAxis    AxisSpec
	timeAxisSet bool

	markers []Marker
	regions []Region

	showColorbar  bool
	showLinePanel bool
	splitRatio    float32
	fontSize      float32

	dispW, dispH float32 // explicit display size; 0 ⇒ derive from available size

	lastCol     []float32
	lastReadout Readout

	bgColor, axisColor, labelColor, gridColor uint32
}

// New constructs a SpectrumDisplay owning a heatmapscroll waterfall (widthSlots
// time × heightSlots frequency bins) and a vertical colorbar, both bound to cfg.
// Panics on nil ids/cfg, empty scopeKey, or zero dimensions (the heatmapscroll
// contract).
func New(ids *c.WidgetIdStack, scopeKey string, cfg *colormap.Config, widthSlots, heightSlots uint32) *SpectrumDisplay {
	if ids == nil {
		panic("spectrumdisplay: New requires a non-nil ids stack")
	}
	if scopeKey == "" {
		panic("spectrumdisplay: New requires a non-empty scopeKey")
	}
	if cfg == nil {
		panic("spectrumdisplay: New requires a non-nil colormap.Config")
	}
	hs := heatmapscroll.New(ids, scopeKey+".wf", cfg, widthSlots, heightSlots)
	hs.SetOrientation(heatmapscroll.ScrollDown) // RF waterfall: newest on top, frequency across X
	// Pin the colorbar to this display's chrome bg (panel tier) rather than the
	// colorscale's standalone default (surface tier), so the colorbar, gutters and
	// line panel read as one surface with no internal seam.
	cbar := colorscale.New(ids, scopeKey+".cb", cfg,
		colorscale.WithOrientation(colorscale.OrientationVertical),
		colorscale.WithBg(DefaultBg))
	return &SpectrumDisplay{
		ids:          ids,
		scopeKey:     scopeKey,
		cfg:          cfg,
		hs:           hs,
		cbar:         cbar,
		heightSlots:  heightSlots,
		showColorbar: true,
		splitRatio:   0.4,
		fontSize:     DefaultFontSize,
		bgColor:      DefaultBg,
		axisColor:    DefaultAxisColor,
		labelColor:   DefaultLabelColor,
		gridColor:    DefaultGridColor,
	}
}

// SetFrequencyAxis sets the physical frequency range mapped across the texture's X.
func (inst *SpectrumDisplay) SetFrequencyAxis(a AxisSpec) { inst.freqAxis = a }

// SetPowerAxis sets the dB range for the colorbar labels and the line panel's Y.
func (inst *SpectrumDisplay) SetPowerAxis(a AxisSpec) { inst.powerAxis = a }

// SetTimeAxis sets the waterfall's time range (left gutter, e.g. seconds of history).
// Without it the left gutter omits time labels.
func (inst *SpectrumDisplay) SetTimeAxis(a AxisSpec) { inst.timeAxis, inst.timeAxisSet = a, true }

// SetWaterfallRange sets the colormap range used to color the waterfall, independent
// of the line panel's power axis. It mutates the shared colormap.Config in place, so
// both the texture and the colorbar track it.
func (inst *SpectrumDisplay) SetWaterfallRange(min, max float64) {
	inst.cfg.DataMin, inst.cfg.DataMax = min, max
}

// SetColormap copies cfg into the shared config in place (palette + range), so the
// texture and colorbar both switch without re-allocating either child. Already-mapped
// texels keep their old colors (the heatmapscroll live-swap caveat).
func (inst *SpectrumDisplay) SetColormap(cfg *colormap.Config) {
	if cfg != nil {
		*inst.cfg = *cfg
	}
}

// SetMarkers replaces the marker set. SetRegions replaces the region set.
func (inst *SpectrumDisplay) SetMarkers(m []Marker) { inst.markers = m }
func (inst *SpectrumDisplay) SetRegions(r []Region) { inst.regions = r }

// AddMarker appends one marker; ClearMarkers empties the set.
func (inst *SpectrumDisplay) AddMarker(m Marker) { inst.markers = append(inst.markers, m) }
func (inst *SpectrumDisplay) ClearMarkers()      { inst.markers = inst.markers[:0] }

// SetColorbarVisible / SetLinePanelVisible toggle the colorbar and the spectrum-line
// subpanel. SetSplitRatio sets the line panel's height as a fraction (0,1) of the data
// area when it is shown.
func (inst *SpectrumDisplay) SetColorbarVisible(b bool)  { inst.showColorbar = b }
func (inst *SpectrumDisplay) SetLinePanelVisible(b bool) { inst.showLinePanel = b }
func (inst *SpectrumDisplay) SetSplitRatio(f float32) {
	if f > 0 && f < 1 {
		inst.splitRatio = f
	}
}

// SetDisplaySize sets the total widget box in logical pixels. 0,0 derives it from the
// available size each frame (window-fill).
func (inst *SpectrumDisplay) SetDisplaySize(wPx, hPx float32) { inst.dispW, inst.dispH = wPx, hPx }

// SetFont sets the axis-label font size in logical pixels.
func (inst *SpectrumDisplay) SetFont(sizePx float32) {
	if sizePx > 0 {
		inst.fontSize = sizePx
	}
}

// PushColumn forwards one column of heightSlots dB samples to the waterfall and
// retains it for the line trace and the cursor readout.
func (inst *SpectrumDisplay) PushColumn(samples []float32) colormap.ColumnStats {
	if cap(inst.lastCol) < len(samples) {
		inst.lastCol = make([]float32, len(samples))
	}
	inst.lastCol = inst.lastCol[:len(samples)]
	copy(inst.lastCol, samples)
	return inst.hs.PushColumn(samples)
}

// Readout returns the cursor readout in physical units (one-frame lag).
func (inst *SpectrumDisplay) Readout() Readout { return inst.lastReadout }

// FrequencyAxis / PowerAxis return the current axis specs.
func (inst *SpectrumDisplay) FrequencyAxis() AxisSpec { return inst.freqAxis }
func (inst *SpectrumDisplay) PowerAxis() AxisSpec     { return inst.powerAxis }

// Clicked / Head / Size forward to the owned waterfall. Release drops its texture.
func (inst *SpectrumDisplay) Clicked() bool       { return inst.hs.Clicked() }
func (inst *SpectrumDisplay) Head() uint32        { return inst.hs.Head() }
func (inst *SpectrumDisplay) Size() (w, h uint32) { return inst.hs.Size() }
func (inst *SpectrumDisplay) Release()            { inst.hs.Release() }

// Render emits the whole composite for this frame. Call once per frame.
func (inst *SpectrumDisplay) Render() {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderInner()
	}
}

func (inst *SpectrumDisplay) renderInner() {
	// Capture this Ui's available size for next frame's window-fill resolve.
	if inst.dispW <= 0 || inst.dispH <= 0 {
		c.CaptureAvailableSize()
	}
	W, H := inst.resolveSize()
	if W < 8 || H < 8 {
		return
	}
	o := layoutOpts{
		leftGutterW: inst.leftGutterWidth(),
		freqGutterH: DefaultFreqGutterH,
		showLine:    inst.showLinePanel,
		splitRatio:  inst.splitRatio,
		lineGapY:    2,
	}
	if inst.showColorbar {
		o.colorbarW = DefaultColorbarW
	}
	r := partition(W, H, o)

	// Sub-rects are widget-local (origin top-left); AllocateUiAtRect anchors them
	// to the parent Ui's min_rect origin (interpreter.rs), not to the cursor. So
	// render inside our own child Ui: its origin is our slot at the cursor, and a
	// single Ui makes every sub-rect share that one origin so the gutters stay in
	// register. Without this wrapper the rects anchor to the *enclosing* panel's
	// top-left and paint over anything placed above us — e.g. the demo's controls.
	for range c.Vertical().KeepIter() {
		if r.texture.valid() {
			for range c.AllocateUiAtRect(r.texture.minX, r.texture.minY, r.texture.maxX, r.texture.maxY).KeepIter() {
				c.UiClipToMaxRect()
				inst.hs.SetDisplaySize(r.texture.w(), r.texture.h())
				inst.hs.Render()
			}
			inst.renderOverlay(r.texture)
		}
		if inst.showLinePanel && r.linePanel.valid() {
			inst.renderLinePanel(r.linePanel)
		}
		if inst.showColorbar && r.colorbar.valid() {
			for range c.AllocateUiAtRect(r.colorbar.minX, r.colorbar.minY, r.colorbar.maxX, r.colorbar.maxY).KeepIter() {
				c.UiClipToMaxRect()
				inst.cbar.SetSize(r.colorbar.w(), r.colorbar.h())
				inst.cbar.Render()
			}
		}
		if r.leftGutter.valid() {
			inst.renderLeftGutter(r)
		}
		if r.freqGutter.valid() {
			inst.renderFreqGutter(r.freqGutter)
		}
	}
	inst.updateReadout()
}

func (inst *SpectrumDisplay) resolveSize() (w, h float32) {
	if inst.dispW > 0 && inst.dispH > 0 {
		return inst.dispW, inst.dispH
	}
	av := c.CurrentApplicationState.StateManager.GetAvailableSize()
	w, h = av.W, av.H
	if math.IsNaN(float64(w)) || w < 1 {
		w = DefaultSize[0]
	}
	if math.IsNaN(float64(h)) || h < 1 {
		h = DefaultSize[1]
	}
	return
}

// renderOverlay paints regions, markers, and the cursor crosshair on a transparent,
// non-sensing canvas above the texture (so the texture keeps its own hover for the
// readout).
func (inst *SpectrumDisplay) renderOverlay(tr rect) {
	tw, th := tr.w(), tr.h()
	for range c.AllocateUiAtRect(tr.minX, tr.minY, tr.maxX, tr.maxY).KeepIter() {
		c.UiClipToMaxRect()
		for _, rg := range inst.regions {
			x0 := inst.freqToPx(rg.StartHz, tw)
			x1 := inst.freqToPx(rg.EndHz, tw)
			if x1 < x0 {
				x0, x1 = x1, x0
			}
			y0, y1 := regionBand(rg.Placement, th)
			col := rg.Color
			if col == 0 {
				col = DefaultRegionColor
			}
			c.PaintRectFilled(x0, y0, x1, y1, 0, color.Hex(col)).Send()
			if rg.Label != "" {
				c.PaintText((x0+x1)/2, y0+1, 1 /*center*/, 0 /*top*/, rg.Label, inst.fontSize, color.Hex(DefaultAnnotationColor)).Send()
			}
		}
		for _, m := range inst.markers {
			if m.Kind == MarkerHorizontal {
				continue // dB markers belong to the line panel, not the time-axis texture
			}
			col := m.Color
			if col == 0 {
				col = DefaultMarkerColor
			}
			x := inst.freqToPx(m.Freq, tw)
			c.PaintLine(x, 0, x, th, color.Hex(col), 1.0).Send()
			if m.Label != "" {
				c.PaintText(x+3, 2, 0 /*left*/, 0 /*top*/, m.Label, inst.fontSize, color.Hex(DefaultAnnotationColor)).Send()
			}
		}
		if inst.lastReadout.Ok {
			x := inst.freqToPx(inst.lastReadout.Freq, tw)
			c.PaintLine(x, 0, x, th, color.Hex(DefaultCursorColor), 1.0).Send()
		}
		c.PaintCanvas(inst.ids.PrepareStr("overlay"), tw, th).Sense(false, false, false).Send()
	}
}

func (inst *SpectrumDisplay) renderLinePanel(lp rect) {
	pw, ph := lp.w(), lp.h()
	for range c.AllocateUiAtRect(lp.minX, lp.minY, lp.maxX, lp.maxY).KeepIter() {
		c.UiClipToMaxRect()
		if inst.powerAxis.Max > inst.powerAxis.Min {
			positions, _ := AxisTicks(inst.powerAxis)
			for _, v := range positions {
				y := inst.dbToPx(v, ph)
				c.PaintLine(0, y, pw, y, color.Hex(inst.gridColor), 0.5).Send()
			}
		}
		if n := len(inst.lastCol); n > 1 && inst.powerAxis.Max > inst.powerAxis.Min {
			xs := make([]float32, n)
			ys := make([]float32, n)
			for i, v := range inst.lastCol {
				xs[i] = (float32(i) + 0.5) / float32(n) * pw
				ys[i] = inst.dbToPx(float64(v), ph)
			}
			c.PaintPolyline(xs, ys, color.Hex(DefaultTraceColor), 1.0).Send()
		}
		c.PaintCanvas(inst.ids.PrepareStr("line"), pw, ph).Background(color.Hex(inst.bgColor)).Send()
	}
}

func (inst *SpectrumDisplay) renderLeftGutter(r layoutRects) {
	g := r.leftGutter
	gw, gh := g.w(), g.h()
	st := inst.rulerStyle()
	st.Baseline = false // the power and time axes share the gutter; neither owns a full-height baseline
	for range c.AllocateUiAtRect(g.minX, g.minY, g.maxX, g.maxY).KeepIter() {
		c.UiClipToMaxRect()
		if inst.showLinePanel && r.linePanel.valid() && inst.powerAxis.Max > inst.powerAxis.Min {
			top, hgt := r.linePanel.minY-g.minY, r.linePanel.h()
			ticks := inst.gutterTicks(inst.powerAxis, top, hgt, true) // dB: max signal at the top
			axisruler.Paint(axisruler.SideLeft, gw, top, top+hgt, ticks, st)
		}
		if inst.timeAxisSet && inst.timeAxis.Max > inst.timeAxis.Min {
			top, hgt := r.texture.minY-g.minY, r.texture.h()
			ticks := inst.gutterTicks(inst.timeAxis, top, hgt, false) // time-since: newest row at the top
			axisruler.Paint(axisruler.SideLeft, gw, top, top+hgt, ticks, st)
		}
		c.PaintCanvas(inst.ids.PrepareStr("lgutter"), gw, gh).Background(color.Hex(inst.bgColor)).Send()
	}
}

// gutterTicks maps an axis's ticks to gutter-local y positions over [top,
// top+hgt]. maxAtTop puts the axis maximum at the top edge (the dB convention,
// strongest signal up); otherwise the minimum is at the top (the time-since
// convention, newest row up). Off-range ticks are dropped.
func (inst *SpectrumDisplay) gutterTicks(a AxisSpec, top, hgt float32, maxAtTop bool) []axisruler.Tick {
	positions, labels := AxisTicks(a)
	span := a.Max - a.Min
	ticks := make([]axisruler.Tick, 0, len(positions))
	for i, v := range positions {
		frac := (v - a.Min) / span
		if maxAtTop {
			frac = (a.Max - v) / span
		}
		if frac < 0 || frac > 1 {
			continue
		}
		ticks = append(ticks, axisruler.Tick{Pos: top + float32(frac)*hgt, Label: labels[i]})
	}
	return ticks
}

// rulerStyle builds the axisruler treatment from this display's configured axis
// colors and font, so the gutters and the timeline share one visual language.
func (inst *SpectrumDisplay) rulerStyle() axisruler.Style {
	st := axisruler.DefaultStyle()
	axis := color.Hex(inst.axisColor)
	st.AxisColor, st.TickColor = axis, axis
	st.LabelColor = color.Hex(inst.labelColor)
	st.FontSize = inst.fontSize
	return st
}

// leftGutterWidth derives the gutter width from the widest power/time label it
// will draw (the ADR-0091 §SD2 "widest label" rule), so signed dB or GHz labels
// are not clipped; it falls back to DefaultLeftGutterW when there is nothing to
// label.
func (inst *SpectrumDisplay) leftGutterWidth() float32 {
	widest := 0
	consider := func(a AxisSpec, on bool) {
		if !on || !(a.Max > a.Min) {
			return
		}
		_, labels := AxisTicks(a)
		for _, l := range labels {
			if n := len(l); n > widest {
				widest = n
			}
		}
	}
	consider(inst.powerAxis, inst.showLinePanel)
	consider(inst.timeAxis, inst.timeAxisSet)
	if widest == 0 {
		return DefaultLeftGutterW
	}
	st := inst.rulerStyle()
	// charWidthEst: a per-glyph width estimate at the current font (6.5px at 11pt,
	// the timeline's ASCII estimate). Reserve tick + gap + a small left pad.
	charWidthEst := st.FontSize * (6.5 / 11.0)
	w := float32(widest)*charWidthEst + st.TickLen + st.LabelGap + leftGutterPadPx
	return max(minLeftGutterWPx, min(maxLeftGutterWPx, w))
}

const (
	leftGutterPadPx  float32 = 8  // breathing room left of the left-gutter labels
	minLeftGutterWPx float32 = 28 // never collapse the gutter below this
	maxLeftGutterWPx float32 = 96 // nor let a pathological label balloon it
)

func (inst *SpectrumDisplay) renderFreqGutter(fg rect) {
	fw, fh := fg.w(), fg.h()
	for range c.AllocateUiAtRect(fg.minX, fg.minY, fg.maxX, fg.maxY).KeepIter() {
		c.UiClipToMaxRect()
		if inst.freqAxis.Max > inst.freqAxis.Min {
			positions, labels := AxisTicks(inst.freqAxis)
			ticks := make([]axisruler.Tick, len(positions))
			for i, v := range positions {
				ticks[i] = axisruler.Tick{Pos: inst.freqToPx(v, fw), Label: labels[i]}
			}
			// Baseline at the gutter top (y=0) sits flush under the texture, so the
			// frequency ruler reads as the texture's bottom axis; ticks + labels
			// hang below, the end labels anchored inward so they stay in the gutter.
			axisruler.Paint(axisruler.SideBottom, 0, 0, fw, ticks, inst.rulerStyle())
		}
		c.PaintCanvas(inst.ids.PrepareStr("fgutter"), fw, fh).Background(color.Hex(inst.bgColor)).Send()
	}
}

// updateReadout maps the waterfall's hovered cell to physical (freq, dB, age).
func (inst *SpectrumDisplay) updateReadout() {
	row, col, hovered := inst.hs.HoveredCell()
	ro := Readout{}
	if hovered && inst.heightSlots > 0 && inst.freqAxis.Max > inst.freqAxis.Min {
		frac := (float64(row) + 0.5) / float64(inst.heightSlots)
		ro.Freq = inst.freqAxis.Min + frac*(inst.freqAxis.Max-inst.freqAxis.Min)
		ro.BinRow = row
		ro.RingCol = col
		if w, _ := inst.hs.Size(); w > 0 {
			// Signed arithmetic: a plain uint32 (Head-1-col+w)%w is only correct when w
			// is a power of two (the wraparound modulus is 2³²), but widthSlots is
			// unconstrained.
			age := (int64(inst.hs.Head()) - 1 - int64(col)) % int64(w)
			if age < 0 {
				age += int64(w)
			}
			ro.Age = int(age)
		}
		if int(row) < len(inst.lastCol) {
			ro.Db = float64(inst.lastCol[row])
		}
		ro.Ok = true
	}
	inst.lastReadout = ro
}

// freqToPx maps a frequency to a clamped pixel x within width w.
func (inst *SpectrumDisplay) freqToPx(f float64, w float32) float32 {
	lo, hi := inst.freqAxis.Min, inst.freqAxis.Max
	if hi <= lo {
		return 0
	}
	frac := (f - lo) / (hi - lo)
	return float32(clamp01(frac)) * w
}

// dbToPx maps a dB value to a clamped pixel y within height h (max at top).
func (inst *SpectrumDisplay) dbToPx(v float64, h float32) float32 {
	lo, hi := inst.powerAxis.Min, inst.powerAxis.Max
	if hi <= lo {
		return 0
	}
	frac := (hi - v) / (hi - lo)
	return float32(clamp01(frac)) * h
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// regionBand returns the [y0,y1] band for a region placement within height h.
func regionBand(p PlacementE, h float32) (y0, y1 float32) {
	switch p {
	case PlacementTop:
		return 0, h * 0.18
	case PlacementBottom:
		return h * 0.82, h
	default:
		return 0, h
	}
}
