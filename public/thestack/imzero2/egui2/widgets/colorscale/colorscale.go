// Package colorscale renders a value-axis legend for a colormap.Config — the
// same colormap type the scientific texture widgets (heatmapscroll) and treemap
// use. It is a semi-retained widget (ADR-0267): New binds it to a Config and
// its [Options], Render draws it once per frame and returns its [Events], and
// the object keeps the tick layout and label measurements between frames.
// Pass one Config to colorscale.New and to whatever renders the data (a
// treemap via treemap.ContinuousColoringFromMap + its Config(), or a heatmap) so
// the visualization and its legend stay in sync automatically.
//
// Rendering: gradient strip + tick marks + numeric labels. Ticks are
// produced by finddivisions — Talbot with a TypesettingScorer for linear
// colormaps so overlapping labels are penalized, or TalbotLogarithmic for
// log colormaps.
//
// Interaction: when the pointer is over the gradient, the widget reports
// the hovered colormap value (Events.Hover, and HoveredValue between
// frames) and paints a white vertical marker at the next frame. One-frame lag is a
// consequence of the paint/canvas/fetch ordering.
package colorscale

import (
	"fmt"
	"math"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
)

// OrientationE selects the scale's primary axis: a horizontal gradient with tick
// marks and labels below it (the default), or a vertical gradient (max at the top)
// with tick marks and labels to its right — the conventional colorbar legend.
type OrientationE uint8

const (
	OrientationHorizontal OrientationE = iota
	OrientationVertical
)

// TickerE selects the finddivisions algorithm used to pick tick positions.
//
// All three support linear and log colormaps. For log colormaps, Heckbert and
// Nelder — which are linear-only algorithms — are run in log10 space and their
// tick values are exp-transformed back, so they align with the gradient (e.g.
// Heckbert on 1..1000 yields labels like 1, 3.16, 10, 31.6, 100, 316, 1000).
// Talbot has a dedicated logarithmic variant that returns clean powers of 10.
type TickerE uint8

const (
	// TickerAuto, the zero value, picks per orientation: Talbot for a
	// horizontal scale, Heckbert for a vertical one (the Talbot legibility
	// scorer penalizes label width, but a vertical axis is constrained by
	// label height, so the width model overcrowds it).
	TickerAuto TickerE = iota
	// TickerTalbot runs Talbot's extended-Wilkinson algorithm with a
	// TypesettingScorer that penalizes overlapping labels, producing
	// legible, range-aware ticks. The horizontal default.
	TickerTalbot
	// TickerHeckbert runs the classic Heckbert (Graphics Gems I) algorithm.
	// Fast, no scorer, ticks are multiples of 1/2/5 × 10ⁿ.
	TickerHeckbert
	// TickerNelder runs Nelder's 1976 scaling algorithm with a nice-number
	// step table. Fast, no scorer, slightly different aesthetic from
	// Heckbert (can pick 1.2/1.6/2.5 step multiples).
	TickerNelder
)

// String returns a short human label, used by demos and logs.
func (inst TickerE) String() string {
	switch inst {
	case TickerAuto:
		return "auto"
	case TickerTalbot:
		return "Talbot"
	case TickerHeckbert:
		return "Heckbert"
	case TickerNelder:
		return "Nelder"
	}
	return fmt.Sprintf("TickerE(%d)", uint8(inst))
}

// Package-level defaults exposed as vars so callers can globally tweak.
var (
	// DefaultSize is the default widget size [w, h] in logical pixels (horizontal orientation).
	DefaultSize = [2]float32{500, 40}
	// DefaultSizeVertical is the default widget size [w, h] for vertical orientation:
	// a narrow strip tall enough to serve as a colorbar legend beside a heatmap.
	DefaultSizeVertical = [2]float32{64, 320}
	// DefaultDesiredTicks is the default tick-count target passed to the chosen TickerE.
	DefaultDesiredTicks = 6
	// DefaultFontSize is the default tick-label font size in logical pixels.
	DefaultFontSize = float32(10)
	// DefaultBg is the default background fill colour (RGBA, 0xRRGGBBAA). Sourced
	// from the IDS neutral spine (ADR-0031 §SD4): NeutralBgSurface — the
	// raised-card/window tier that sibling widget canvases (timeline, treemap,
	// gauge) paint — so a standalone colorscale reads as a surface, not a near-
	// black rectangle. A host that embeds the scale as panel-tier chrome (e.g.
	// the spectrumdisplay colorbar) overrides this via Options.Bg so the composite
	// reads as one surface (ADR-0091 §Update 2026-06-21).
	DefaultBg = styletokens.NeutralBgSurface.AsHex()
	// DefaultTickColor is the default tick-mark stroke colour (RGBA).
	DefaultTickColor = uint32(0xd0d0d0ff)
	// DefaultLabelColor is the default tick-label text colour (RGBA).
	DefaultLabelColor = uint32(0xe8e8f0ff)
	// DefaultBorderColor is the default outer border stroke colour (RGBA).
	DefaultBorderColor = uint32(0x444455ff)
)

// Options configures a ColorScale (ADR-0267 W11). Every zero value is the
// default; the widget re-reads [ColorScale.Opts] on every Render, so a change
// is an assignment.
type Options struct {
	// Width and Height are the widget's logical-pixel size. Zero takes the
	// orientation's default: [DefaultSize] horizontal, [DefaultSizeVertical]
	// vertical.
	Width, Height float32
	// Orientation selects horizontal (the default) or vertical layout.
	Orientation OrientationE
	// DesiredTicks hints at the tick count the ticker should aim for; zero
	// (and anything below 2) takes [DefaultDesiredTicks]. The algorithm may
	// produce slightly more or fewer depending on the range.
	DesiredTicks int
	// Ticker selects the tick-placement algorithm; [TickerAuto] picks per
	// orientation.
	Ticker TickerE
	// LabelFormat overrides the tick-label formatter. nil takes the default
	// for the colormap's scale (SI-suffixed labels for a log colormap). It
	// is only invoked when Heckbert does not supply labels.
	LabelFormat func(float64) string
	// Bg is the background fill colour (RGBA, 0xRRGGBBAA) painted behind the
	// gradient and the tick/label margins; zero takes [DefaultBg]
	// (NeutralBgSurface, the standalone surface tier). A host embedding the
	// scale as panel-tier chrome — spectrumdisplay's colorbar — passes its own
	// chrome colour so the embedded legend and the surrounding panel read as
	// one surface rather than two darks.
	Bg uint32
}

// HoverInfo reports the colormap value currently under the pointer.
// Ok=false when the pointer is not over the widget.
type HoverInfo struct {
	Value float64
	PxX   float32 // hover pixel along the axis (horizontal orientation)
	PxY   float32 // hover pixel along the axis (vertical orientation)
	Ok    bool
}

// ColorScale is a passive value-legend widget rendered as a gradient
// strip + tick axis: a semi-retained widget (ADR-0267) that keeps its tick
// layout and label measurements across frames. Construct with New and call
// Render once per frame.
type ColorScale struct {
	// Opts is re-read on every Render; a change is an assignment.
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string
	cmap     *colormap.Config
	// The effective values Opts resolves to each frame (see resolve).
	width        float32
	height       float32
	orientation  OrientationE
	desiredTicks int
	ticker       TickerE
	labelFormat  func(float64) string
	fontSize     float32
	bgColor      uint32
	tickColor    uint32
	labelColor   uint32
	borderColor  uint32

	// Cached tick layout, recomputed only when an input that affects the
	// scorer output changes: range (min,max), widget width, or desiredTicks.
	// Talbot with TypesettingScorer is O(Qs × m × legibility work), so
	// caching the result across the many frames where nothing has changed
	// is a meaningful optimization.
	cachedMin, cachedMax, cachedWidth float64
	cachedTicks                       int
	cachedTicker                      TickerE
	cachedAxis                        finddivisions.AxisLayout
	cachedValid                       bool

	// Hover state: filled from the R24 canvas-pointer row (keyed by this
	// widget's canvas id) after the PaintCanvas each frame; the marker is
	// drawn ONE FRAME LATER (since the canvas has already been flushed).
	// One-frame lag is imperceptible for a live pointer indicator.
	lastHover HoverInfo

	// Measurer for the Talbot legibility scorer. Initially misses the
	// cache, returning approximations; real widths arrive from egui via
	// the MeasureText FFFI binding on the next frame. Axis is then
	// invalidated (pendingRemeasure) and re-run with real widths.
	measurer         *cachingMeasurer
	pendingRemeasure bool
}

// Events is what one Render reports.
type Events struct {
	// Hover is the colormap value under the pointer, Ok false when the
	// pointer is not over the widget. One frame old, like every canvas
	// readback.
	Hover HoverInfo
}

// New constructs a ColorScale bound to cm, scoped under scopeKey on ids
// (unique among instances sharing the stack). Panics on nil ids or cm, or an
// empty scopeKey.
func New(ids *c.WidgetIdStack, scopeKey string, cm *colormap.Config, opts Options) *ColorScale {
	if ids == nil {
		panic("colorscale: New requires a non-nil ids stack")
	}
	if scopeKey == "" {
		panic("colorscale: New requires a non-empty scopeKey")
	}
	if cm == nil {
		panic("colorscale: New requires a non-nil Colormap")
	}
	inst := &ColorScale{
		Opts:        opts,
		ids:         ids,
		scopeKey:    scopeKey,
		cmap:        cm,
		fontSize:    DefaultFontSize,
		tickColor:   DefaultTickColor,
		labelColor:  DefaultLabelColor,
		borderColor: DefaultBorderColor,
	}
	inst.measurer = newCachingMeasurer()
	inst.resolve()
	return inst
}

// resolve turns Opts into the frame's effective values: orientation-dependent
// size and ticker defaults, the colormap-dependent label formatter, the
// default background.
func (inst *ColorScale) resolve() {
	o := inst.Opts
	inst.orientation = o.Orientation
	inst.width, inst.height = o.Width, o.Height
	if inst.width <= 0 || inst.height <= 0 {
		if inst.orientation == OrientationVertical {
			inst.width, inst.height = DefaultSizeVertical[0], DefaultSizeVertical[1]
		} else {
			inst.width, inst.height = DefaultSize[0], DefaultSize[1]
		}
	}
	inst.desiredTicks = o.DesiredTicks
	if inst.desiredTicks < 2 {
		inst.desiredTicks = DefaultDesiredTicks
	}
	inst.ticker = o.Ticker
	if inst.ticker == TickerAuto {
		inst.ticker = TickerTalbot
		if inst.orientation == OrientationVertical {
			inst.ticker = TickerHeckbert
		}
	}
	inst.labelFormat = o.LabelFormat
	if inst.labelFormat == nil {
		if inst.cmap.IsLog() {
			inst.labelFormat = defaultLogLabelFormat
		} else {
			inst.labelFormat = defaultLabelFormat
		}
	}
	inst.bgColor = o.Bg
	if inst.bgColor == 0 {
		inst.bgColor = DefaultBg
	}
}

// Colormap returns the colormap.Config this scale is bound to.
func (inst *ColorScale) Colormap() *colormap.Config { return inst.cmap }

// HoveredValue returns the colormap value under the pointer as of the most
// recent Render. The returned ok is false when the pointer is not over the
// widget. State is one frame old relative to the latest mouse position.
func (inst *ColorScale) HoveredValue() HoverInfo { return inst.lastHover }

// canvasCursor reads this widget's R24 canvas-pointer row: the pointer in
// canvas-relative coordinates, NaN when it is not over the canvas. R24 is
// keyed per canvas id, so the readback survives other canvases rendering
// in the same frame — the single-slot R14 register this widget read
// before was last-canvas-wins, which blinded the hover whenever another
// canvas-bearing widget (an implot plot, a treemap) was visible. Must be
// called inside the Render id scope so the derived id matches the
// PaintCanvas above it; prepare+Derive is deterministic and does not
// consume id-stack state.
func (inst *ColorScale) canvasCursor() (hx float32, hy float32) {
	hx = float32(math.NaN())
	hy = hx
	if cur, ok := c.CurrentApplicationState.StateManager.GetCanvasCursor(
		widgethandle.Make(inst.ids.PrepareStr("canvas").Derive())); ok {
		hx, hy = cur.PosX, cur.PosY
	}
	return
}

// Render emits the widget inside the current Ui and reports the hover. Wraps
// its body in c.IdScope(scopeKey) so multiple instances sharing the same
// WidgetIdStack don't collide on painter-canvas ids.
func (inst *ColorScale) Render() (ev Events) {
	inst.resolve()
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		if inst.orientation == OrientationVertical {
			inst.renderVertical()
		} else {
			inst.renderHorizontal()
		}
	}
	ev.Hover = inst.lastHover
	return
}

func (inst *ColorScale) renderHorizontal() {
	cm := inst.cmap
	min, max := cm.Range()
	axis := inst.ensureAxis(min, max, float64(inst.width))

	// Layout: gradient takes the upper 55% of the widget height; tick marks
	// occupy a 5-px strip below it; labels fill the rest.
	const (
		tickMarkH    float32 = 5
		gradientGapY float32 = 1 // 1-px separation between gradient and tick marks
	)
	gradientH := inst.height * 0.55
	if gradientH < 10 {
		gradientH = 10
	}
	tickY0 := gradientH + gradientGapY
	tickY1 := tickY0 + tickMarkH
	labelY := tickY1 + 2

	// --- Paint gradient: N thin rects sampling Colormap.At. 128 steps is
	// plenty of smoothness for typical widget widths.
	const steps = 128
	stepW := inst.width / float32(steps)
	for i := range steps {
		t := float64(i) / float64(steps-1)
		rgba := cm.At(sampleAtNormalized(cm, t))
		x := float32(i) * stepW
		c.PaintRectFilled(x, 0, x+stepW+0.5, gradientH, 0, color.Hex(rgba)).Send()
	}

	// --- Gradient border.
	c.PaintRectStroke(0, 0, inst.width, gradientH, 0, color.Hex(inst.borderColor), styletokens.StrokeHair).Send()

	// --- Tick marks + labels. Labels are center-anchored, except within an
	// `edgeGuard` px of the left/right edges where we switch to left/right
	// anchor so the text doesn't clip the widget boundary. We always format
	// via inst.labelFormat rather than using AxisLayout.TickLabels so a user-
	// supplied Options.LabelFormat — and the log-aware default — are uniformly
	// applied.
	const edgeGuard float32 = 12
	for _, tickVal := range axis.TickValues {
		tickLabel := inst.labelFormat(tickVal)
		px := float32(cm.Normalize(tickVal)) * inst.width
		c.PaintLine(px, gradientH, px, tickY1, color.Hex(inst.tickColor), 1.0).Send()
		var anchorH uint8 = 1
		switch {
		case px < edgeGuard:
			anchorH = 0
		case px > inst.width-edgeGuard:
			anchorH = 2
		}
		c.PaintText(px, labelY, anchorH, 0 /*anchorV=top*/, tickLabel, inst.fontSize, color.Hex(inst.labelColor)).Send()
	}

	// --- Hover marker: a thick vertical line at last frame's hover x.
	// One-frame lag is a consequence of the paint/canvas/fetch ordering;
	// imperceptible for pointer tracking.
	if inst.lastHover.Ok {
		hx := inst.lastHover.PxX
		if hx < 0 {
			hx = 0
		}
		if hx > inst.width {
			hx = inst.width
		}
		c.PaintLine(hx, 0, hx, gradientH+tickMarkH+2, color.Hex(0xffffffff), 2.0).Send()
	}

	// Flush the frame's accumulated paint commands into an egui-allocated
	// canvas. PaintCanvas allocates (width, height) in the parent Ui's
	// current layout cursor — so the scale flows naturally between whatever
	// the caller emitted before and after. The canvas stamps a per-id R24
	// pointer row read back below.
	c.PaintCanvas(inst.ids.PrepareStr("canvas"), inst.width, inst.height).
		Background(color.Hex(inst.bgColor)).
		Send()

	// Read the canvas's pointer state from the StateManager cache
	// (populated last frame by Sync) and translate it to a colormap
	// value. Store for next frame's marker and fire the optional
	// callback. Reads cache rather than inline-fetching because inline
	// fetches inside deferred-block captures (e.g. dock.Tab bodies)
	// buffer instead of flushing and deadlock the render loop.
	hx, _ := inst.canvasCursor()
	hover := HoverInfo{}
	if !math.IsNaN(float64(hx)) && hx >= 0 && hx <= inst.width {
		t := float64(hx) / float64(inst.width)
		hover.PxX = hx
		hover.Value = min + t*(max-min)
		if cm.IsLog() {
			// Invert Normalize: for log colormaps the gradient's x is a
			// linear position in 0..1, but the underlying value is log-mapped.
			// Re-compute the value from the normalized fraction.
			lMin, lMax := math.Log10(min), math.Log10(max)
			hover.Value = math.Pow(10, lMin+t*(lMax-lMin))
		}
		hover.Ok = true
	}
	inst.lastHover = hover
}

// renderVertical mirrors renderHorizontal with the axes transposed: a gradient
// strip down the left (max at the top, the conventional colorbar), tick marks in a
// column to its right, labels to the right of those, and a horizontal hover marker.
func (inst *ColorScale) renderVertical() {
	cm := inst.cmap
	min, max := cm.Range()
	axis := inst.ensureAxis(min, max, float64(inst.height))

	// Layout: gradient strip on the left; a tickMarkW-wide tick column to its
	// right; labels fill the remaining width.
	const (
		tickMarkW    float32 = 5
		gradientGapX float32 = 1
	)
	gradientW := inst.width * 0.30
	if gradientW < 10 {
		gradientW = 10
	}
	tickX1 := gradientW + gradientGapX + tickMarkW
	labelX := tickX1 + 2

	// --- Paint gradient: N thin horizontal rects sampling Colormap.At, max at top.
	const steps = 128
	stepH := inst.height / float32(steps)
	for i := range steps {
		t := float64(i) / float64(steps-1)         // 0 at the top
		rgba := cm.At(sampleAtNormalized(cm, 1-t)) // top=max, bottom=min
		y := float32(i) * stepH
		c.PaintRectFilled(0, y, gradientW, y+stepH+0.5, 0, color.Hex(rgba)).Send()
	}

	// --- Gradient border.
	c.PaintRectStroke(0, 0, gradientW, inst.height, 0, color.Hex(inst.borderColor), styletokens.StrokeHair).Send()

	// --- Tick marks + labels. Labels are vertically center-anchored, except within
	// edgeGuard px of the top/bottom edges where we switch to top/bottom anchor so the
	// text doesn't clip the boundary. Format via inst.labelFormat so an Options.LabelFormat
	// override and the log-aware default apply uniformly (the renderHorizontal policy).
	const edgeGuard float32 = 8
	for _, tickVal := range axis.TickValues {
		tickLabel := inst.labelFormat(tickVal)
		py := float32(1-cm.Normalize(tickVal)) * inst.height // top=max, bottom=min
		c.PaintLine(gradientW, py, tickX1, py, color.Hex(inst.tickColor), 1.0).Send()
		var anchorV uint8 = 1 // center
		switch {
		case py < edgeGuard:
			anchorV = 0 // top
		case py > inst.height-edgeGuard:
			anchorV = 2 // bottom
		}
		c.PaintText(labelX, py, 0 /*anchorH=left*/, anchorV, tickLabel, inst.fontSize, color.Hex(inst.labelColor)).Send()
	}

	// --- Hover marker: a thick horizontal line at last frame's hover y (one-frame
	// lag, as in renderHorizontal).
	if inst.lastHover.Ok {
		hy := inst.lastHover.PxY
		if hy < 0 {
			hy = 0
		}
		if hy > inst.height {
			hy = inst.height
		}
		c.PaintLine(0, hy, tickX1, hy, color.Hex(0xffffffff), 2.0).Send()
	}

	// Flush the accumulated paint into an egui-allocated canvas (see
	// renderHorizontal for the cache-read rationale).
	c.PaintCanvas(inst.ids.PrepareStr("canvas"), inst.width, inst.height).
		Background(color.Hex(inst.bgColor)).
		Send()

	_, hy := inst.canvasCursor()
	hover := HoverInfo{}
	if !math.IsNaN(float64(hy)) && hy >= 0 && hy <= inst.height {
		t := float64(hy) / float64(inst.height) // 0 at the top
		hover.PxY = hy
		hover.Value = max - t*(max-min) // top=max
		if cm.IsLog() {
			lMin, lMax := math.Log10(min), math.Log10(max)
			hover.Value = math.Pow(10, lMax-t*(lMax-lMin))
		}
		hover.Ok = true
	}
	inst.lastHover = hover
}

// ensureAxis returns the cached tick layout, recomputing it only when an input that
// affects tick selection changes: range (min,max), the axis length in pixels (width
// for horizontal, height for vertical), desiredTicks, the ticker, or a pending
// remeasure from last frame's approximate label widths. It also renews the
// measurement databindings so cache entries live this frame stay live next Sync.
func (inst *ColorScale) ensureAxis(min, max, axisLen float64) finddivisions.AxisLayout {
	if !inst.cachedValid ||
		inst.pendingRemeasure ||
		inst.cachedMin != min ||
		inst.cachedMax != max ||
		inst.cachedWidth != axisLen ||
		inst.cachedTicks != inst.desiredTicks ||
		inst.cachedTicker != inst.ticker {
		inst.cachedAxis = inst.computeAxis(min, max, axisLen)
		inst.cachedMin = min
		inst.cachedMax = max
		inst.cachedWidth = axisLen
		inst.cachedTicks = inst.desiredTicks
		inst.cachedTicker = inst.ticker
		inst.cachedValid = true
	}
	inst.measurer.RenewBindings()
	return inst.cachedAxis
}

// computeAxis picks the best tick layout for the current colormap using the
// currently selected TickerE. axisLen is the axis length in pixels (width for
// horizontal, height for vertical), used by the Talbot legibility scorer. On
// failure any path falls back to a two-tick endpoint axis and logs a warning
// (validation policy: log + safe default).
func (inst *ColorScale) computeAxis(min, max, axisLen float64) finddivisions.AxisLayout {
	if !axisRangeResolvable(min, max) {
		// Degenerate / float-unresolvable range: a zero-or-reversed span, a
		// non-finite endpoint, or a span so small relative to its magnitude that
		// it is a single value at float64 resolution (e.g. a uint64 id/hash
		// column near 2^63 where every row is ~equal). No ticker can place
		// meaningful interior ticks there, and the Talbot search would probe
		// sub-ULP steps whose offset/count loops cannot advance — a hang. An
		// endpoints-only axis is the honest, always-terminating result.
		return endpointsAxis(min, max)
	}
	switch inst.ticker {
	case TickerHeckbert:
		return inst.computeAxisHeckbert(min, max)
	case TickerNelder:
		return inst.computeAxisNelder(min, max)
	default:
		return inst.computeAxisTalbot(min, max, axisLen)
	}
}

// axisRangeResolvable reports whether [min,max] has a finite, positive span wide
// enough — relative to its magnitude — for a tick search to resolve interior
// steps. Below ~2^-40 of the magnitude the span is effectively a single value at
// float64 resolution: candidate steps round to no-ops and the Talbot offset /
// count loops can spin without advancing (a hang). The 2^-40 cut sits ~2^13
// above the ~2^-53 magnitude at which the loops actually stall, so it rejects
// only ranges that already read as a single value.
func axisRangeResolvable(min, max float64) bool {
	if math.IsNaN(min) || math.IsNaN(max) || math.IsInf(min, 0) || math.IsInf(max, 0) {
		return false
	}
	span := max - min
	if span <= 0 {
		return false
	}
	mag := math.Max(math.Abs(min), math.Abs(max))
	return mag == 0 || span > mag*0x1p-40
}

// computeAxisTalbot uses Talbot + TypesettingScorer for linear colormaps and
// TalbotLogarithmic for log colormaps.
func (inst *ColorScale) computeAxisTalbot(min, max, axisLen float64) finddivisions.AxisLayout {
	if inst.cmap.IsLog() {
		// TalbotLogarithmic calls the inner Talbot with the supplied opts
		// (only Qs is overwritten internally). Without populated Weights
		// the scorer returns 0 for every candidate, and the algorithm
		// picks an arbitrary — possibly out-of-range — tick set
		// (e.g., 10^-10 for a 1..1000 range). DefaultWeights + FastMode
		// give sensible power-of-10 ticks.
		logOpts := finddivisions.TalbotOptions{
			Weights:  finddivisions.DefaultWeights,
			FastMode: true,
		}
		res, err := finddivisions.TalbotLogarithmic(min, max, inst.desiredTicks, logOpts, nil)
		if err != nil {
			log.Warn().
				Str("pkg", "colorscale").
				Float64("min", min).Float64("max", max).
				Err(err).
				Msg("TalbotLogarithmic failed; falling back to endpoints-only axis")
			return endpointsAxis(min, max)
		}
		return res.AxisResult
	}
	// Linear: use Talbot with a TypesettingScorer so label-overlap is part
	// of the legibility score. The scorer queries inst.measurer for each
	// candidate label; cache misses seed approximate widths now, queue a
	// MeasureText FFFI call for the real width, and the colorscale will
	// re-run Talbot next frame once the real widths have arrived.
	scorer, err := finddivisions.NewTypesettingScorer(
		float64(inst.fontSize),
		96.0,    // assumed logical DPI; egui treats font size in logical px
		axisLen, // available span along the axis (width horizontal, height vertical)
		inst.measurer,
	)
	if err != nil {
		log.Warn().Str("pkg", "colorscale").Err(err).Msg("failed to build TypesettingScorer; falling back to Heckbert")
		return inst.heckbertAxis(min, max)
	}
	opts := finddivisions.TalbotOptions{
		Weights:  finddivisions.DefaultWeights,
		Qs:       finddivisions.DefaultQ,
		FastMode: true,
	}
	axis := finddivisions.Talbot(min, max, inst.desiredTicks, opts, scorer)
	// Any cache miss means we used approximations; invalidate so next frame
	// re-runs with the real widths the measurer will have by then.
	inst.pendingRemeasure = inst.measurer.AnyNew()
	if len(axis.TickValues) == 0 {
		return inst.heckbertAxis(min, max)
	}
	return axis
}

// computeAxisHeckbert uses the classic Heckbert algorithm. For log colormaps,
// the range is log-transformed, Heckbert runs in log space, and tick values
// are exp-transformed back so they position correctly on the log gradient.
func (inst *ColorScale) computeAxisHeckbert(min, max float64) finddivisions.AxisLayout {
	return inst.runLinearTicker(min, max, "Heckbert", func(lo, hi float64) (finddivisions.AxisLayout, error) {
		return finddivisions.Heckbert(lo, hi, inst.desiredTicks)
	})
}

// computeAxisNelder uses Nelder's 1976 algorithm. Same log-space treatment
// as computeAxisHeckbert.
func (inst *ColorScale) computeAxisNelder(min, max float64) finddivisions.AxisLayout {
	return inst.runLinearTicker(min, max, "Nelder", func(lo, hi float64) (finddivisions.AxisLayout, error) {
		// Nelder doesn't return an error, but wrap it in the error-returning
		// signature for uniform handling.
		return finddivisions.Nelder(lo, hi, inst.desiredTicks, nil), nil
	})
}

// runLinearTicker executes a linear-only tick algorithm, transparently
// log-transforming the range for log colormaps and inverting ticks back.
// algName is used in warnings. On error it falls back to an endpoints axis.
func (inst *ColorScale) runLinearTicker(min, max float64, algName string, run func(lo, hi float64) (finddivisions.AxisLayout, error)) finddivisions.AxisLayout {
	if inst.cmap.IsLog() {
		// Guard the log transform: math.Log10(0) = -Inf and math.Log10(<0) =
		// NaN, either of which poison the linear algorithm's arithmetic.
		// TalbotLogarithmic validates this internally, so computeAxisTalbot
		// doesn't need the check — Heckbert/Nelder are log-agnostic and
		// would happily consume garbage bounds.
		if !(min > 0 && max > 0) {
			log.Warn().
				Str("pkg", "colorscale").
				Str("alg", algName).
				Float64("min", min).Float64("max", max).
				Msg("log colormap with non-positive bounds; falling back to endpoints-only axis")
			return endpointsAxis(min, max)
		}
		lo, hi := math.Log10(min), math.Log10(max)
		ax, err := run(lo, hi)
		if err != nil {
			log.Warn().
				Str("pkg", "colorscale").
				Str("alg", algName).
				Float64("min", min).Float64("max", max).
				Err(err).
				Msg("linear ticker failed in log space; falling back to endpoints-only axis")
			return endpointsAxis(min, max)
		}
		// Exp-transform back so positions line up with the log gradient.
		for i, v := range ax.TickValues {
			ax.TickValues[i] = math.Pow(10, v)
		}
		ax.DataMin, ax.DataMax = min, max
		ax.ViewMin = math.Pow(10, ax.ViewMin)
		ax.ViewMax = math.Pow(10, ax.ViewMax)
		return ax
	}
	ax, err := run(min, max)
	if err != nil {
		log.Warn().
			Str("pkg", "colorscale").
			Str("alg", algName).
			Float64("min", min).Float64("max", max).
			Err(err).
			Msg("linear ticker failed; falling back to endpoints-only axis")
		return endpointsAxis(min, max)
	}
	return ax
}

func (inst *ColorScale) heckbertAxis(min, max float64) finddivisions.AxisLayout {
	axis, err := finddivisions.Heckbert(min, max, inst.desiredTicks)
	if err != nil {
		log.Warn().
			Str("pkg", "colorscale").
			Float64("min", min).Float64("max", max).
			Err(err).
			Msg("Heckbert failed; falling back to endpoints-only axis")
		return endpointsAxis(min, max)
	}
	return axis
}

func endpointsAxis(min, max float64) finddivisions.AxisLayout {
	return finddivisions.AxisLayout{
		DataMin: min, DataMax: max,
		ViewMin: min, ViewMax: max,
		TickValues: []float64{min, max},
	}
}

// defaultLabelFormat produces compact numeric labels for linear colormaps:
//   - integers rendered without decimals
//   - otherwise %.3g (up to 3 significant digits, compact exponent)
func defaultLabelFormat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.3g", v)
}

// defaultLogLabelFormat produces compact numeric labels for log colormaps
// using SI suffixes (K / M / G) once values cross each threshold, so the
// axis reads "1, 10, 100, 1K, 10K, 100K, 1M" instead of
// "1, 10, 100, 1000, 10000, 100000, 1e+06".
func defaultLogLabelFormat(v float64) string {
	absv := math.Abs(v)
	if absv == 0 {
		return "0"
	}
	if absv < 1000 && v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	suffixes := []struct {
		threshold float64
		label     string
	}{
		{1e9, "G"}, {1e6, "M"}, {1e3, "K"},
	}
	for _, s := range suffixes {
		if absv >= s.threshold {
			scaled := v / s.threshold
			if scaled == float64(int64(scaled)) {
				return fmt.Sprintf("%d%s", int64(scaled), s.label)
			}
			return fmt.Sprintf("%.1f%s", scaled, s.label)
		}
	}
	return fmt.Sprintf("%.3g", v)
}

// sampleAtNormalized returns the sample value whose palette position under
// cm is t, inverting Normalize per scale. The gradient is walked in palette
// space, as the ticks and the hover readout are, so on a log or dB scale
// the colour at an x matches the tick drawn there; stepping linearly in
// value instead painted a 1..1e6 log bar almost wholly in its top decade.
// A range the scale cannot invert (a non-positive log bound) falls back to
// the linear step, which Normalize maps to the palette start either way.
func sampleAtNormalized(cm *colormap.Config, t float64) (v float64) {
	min, max := cm.Range()
	v = min + t*(max-min)
	switch cm.Scale {
	case colormap.ScaleLogE:
		if min > 0 && max > 0 {
			lMin, lMax := math.Log10(min), math.Log10(max)
			v = math.Pow(10, lMin+t*(lMax-lMin))
		}
	case colormap.ScaleDbE:
		// DataMin/DataMax are dB; the sample is the power with that dB.
		v = math.Pow(10, v/10)
	}
	return
}
