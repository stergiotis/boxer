//go:build llm_generated_opus47

// Package colorscale renders a value-axis legend for a colormap.Config — the
// same colormap type the scientific texture widgets (heatmapscroll) and treemap
// use. Pass one Config to colorscale.New and to whatever renders the data (a
// treemap via treemap.ContinuousColoringFromMap + its Config(), or a heatmap) so
// the visualization and its legend stay in sync automatically.
//
// Rendering: gradient strip + tick marks + numeric labels. Ticks are
// produced by finddivisions — Talbot with a TypesettingScorer for linear
// colormaps so overlapping labels are penalized, or TalbotLogarithmic for
// log colormaps.
//
// Interaction: when the pointer is over the gradient, the widget records
// the hovered colormap value (HoveredValue, OnHover callback) and paints
// a white vertical marker at the next frame. One-frame lag is a
// consequence of the paint/canvas/fetch ordering.
package colorscale

import (
	"fmt"
	"math"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
)

// OrientationE selects the scale's primary axis. Phase 1 only implements
// OrientationHorizontal; OrientationVertical is reserved for a follow-up.
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
	// TickerTalbot runs Talbot's extended-Wilkinson algorithm with a
	// TypesettingScorer that penalizes overlapping labels, producing
	// legible, range-aware ticks. Default.
	TickerTalbot TickerE = iota
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
	// DefaultDesiredTicks is the default tick-count target passed to the chosen TickerE.
	DefaultDesiredTicks = 6
	// DefaultFontSize is the default tick-label font size in logical pixels.
	DefaultFontSize = float32(10)
	// DefaultBg is the default background fill colour (RGBA, 0xRRGGBBAA).
	DefaultBg = uint32(0x1a1a22ff)
	// DefaultTickColor is the default tick-mark stroke colour (RGBA).
	DefaultTickColor = uint32(0xd0d0d0ff)
	// DefaultLabelColor is the default tick-label text colour (RGBA).
	DefaultLabelColor = uint32(0xe8e8f0ff)
	// DefaultBorderColor is the default outer border stroke colour (RGBA).
	DefaultBorderColor = uint32(0x444455ff)
)

// Option configures a ColorScale at construction time.
type Option func(*ColorScale)

// WithSize overrides the widget's logical-pixel size. Default 500×40 for
// horizontal orientation.
func WithSize(w, h float32) Option {
	if w <= 0 || h <= 0 {
		panic(fmt.Sprintf("colorscale: WithSize requires positive w,h (got %v,%v)", w, h))
	}
	return func(inst *ColorScale) { inst.width, inst.height = w, h }
}

// WithOrientation selects horizontal vs vertical layout. Only OrientationHorizontal is
// currently implemented.
func WithOrientation(o OrientationE) Option {
	return func(inst *ColorScale) { inst.orientation = o }
}

// WithDesiredTicks hints at the tick count the chosen ticker should aim for.
// Default 6. The algorithm may produce slightly more or fewer depending on
// the range.
func WithDesiredTicks(n int) Option {
	if n < 2 {
		panic("colorscale: WithDesiredTicks requires n >= 2")
	}
	return func(inst *ColorScale) { inst.desiredTicks = n }
}

// WithTicker selects the tick-placement algorithm. Default TickerTalbot —
// which pairs a TypesettingScorer with the user-supplied font metrics for
// overlap-aware tick selection. Heckbert/Nelder are cheaper alternatives that
// ignore label widths.
func WithTicker(t TickerE) Option {
	return func(inst *ColorScale) { inst.ticker = t }
}

// WithLabelFormat overrides the tick-label formatter. By default, Heckbert's
// pre-formatted labels (from finddivisions.AxisLayout.TickLabels) are used.
// A custom fn is only invoked when Heckbert doesn't supply labels.
func WithLabelFormat(fn func(float64) string) Option {
	if fn == nil {
		panic("colorscale: WithLabelFormat requires a non-nil fn")
	}
	return func(inst *ColorScale) { inst.labelFormat = fn }
}

// HoverInfo reports the colormap value currently under the pointer.
// Ok=false when the pointer is not over the widget.
type HoverInfo struct {
	Value float64
	PxX   float32
	Ok    bool
}

// ColorScale is a passive value-legend widget rendered as a gradient
// strip + tick axis. Construct with New and call Render once per frame.
type ColorScale struct {
	ids          *c.WidgetIdStack
	scopeKey     string
	cmap         *colormap.Config
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

	// Hover state: filled by FetchR14CanvasPointer after the PaintCanvas
	// each frame; the marker is drawn ONE FRAME LATER (since the canvas
	// has already been flushed). One-frame lag is imperceptible for a
	// live pointer indicator.
	lastHover HoverInfo
	onHover   func(HoverInfo)

	// Measurer for the Talbot legibility scorer. Initially misses the
	// cache, returning approximations; real widths arrive from egui via
	// the MeasureText FFFI binding on the next frame. Axis is then
	// invalidated (pendingRemeasure) and re-run with real widths.
	measurer         *cachingMeasurer
	pendingRemeasure bool
}

// New constructs a ColorScale bound to cm. scopeKey must be unique among
// ColorScale instances sharing the same ids stack. Panics on nil or empty
// required arguments.
func New(ids *c.WidgetIdStack, scopeKey string, cm *colormap.Config, opts ...Option) *ColorScale {
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
		ids:          ids,
		scopeKey:     scopeKey,
		cmap:         cm,
		width:        DefaultSize[0],
		height:       DefaultSize[1],
		desiredTicks: DefaultDesiredTicks,
		fontSize:     DefaultFontSize,
		bgColor:      DefaultBg,
		tickColor:    DefaultTickColor,
		labelColor:   DefaultLabelColor,
		borderColor:  DefaultBorderColor,
	}
	// Pick the default formatter based on colormap type so log colormaps
	// get SI-suffixed labels out of the box. WithLabelFormat still overrides.
	if cm.IsLog() {
		inst.labelFormat = defaultLogLabelFormat
	} else {
		inst.labelFormat = defaultLabelFormat
	}
	inst.measurer = newCachingMeasurer()
	for _, opt := range opts {
		opt(inst)
	}
	if inst.orientation != OrientationHorizontal {
		panic("colorscale: only OrientationHorizontal orientation is implemented in this version")
	}
	return inst
}

// Colormap returns the colormap.Config this scale is bound to.
func (inst *ColorScale) Colormap() *colormap.Config { return inst.cmap }

// HoveredValue returns the colormap value under the pointer as of the most
// recent Render. The returned ok is false when the pointer is not over the
// widget. State is one frame old relative to the latest mouse position.
func (inst *ColorScale) HoveredValue() HoverInfo { return inst.lastHover }

// OnHover registers a callback invoked once per Render with the current
// hover state (ok=false when not hovering). Replaces any previous callback.
// Pass nil to disable.
func (inst *ColorScale) OnHover(fn func(HoverInfo)) { inst.onHover = fn }

// Render emits the widget inside the current Ui. Wraps its body in
// c.IdScope(scopeKey) so multiple instances sharing the same WidgetIdStack
// don't collide on painter-canvas ids.
func (inst *ColorScale) Render() {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderHorizontal()
	}
}

func (inst *ColorScale) renderHorizontal() {
	cm := inst.cmap
	min, max := cm.Range()

	// Recompute only when an input that affects the Talbot score changes,
	// or when last frame's Talbot run used approximate widths (because it
	// saw new labels the measurer hadn't cached yet) and the real widths
	// have now arrived from Sync.
	if !inst.cachedValid ||
		inst.pendingRemeasure ||
		inst.cachedMin != min ||
		inst.cachedMax != max ||
		inst.cachedWidth != float64(inst.width) ||
		inst.cachedTicks != inst.desiredTicks ||
		inst.cachedTicker != inst.ticker {
		inst.cachedAxis = inst.computeAxis(min, max)
		inst.cachedMin = min
		inst.cachedMax = max
		inst.cachedWidth = float64(inst.width)
		inst.cachedTicks = inst.desiredTicks
		inst.cachedTicker = inst.ticker
		inst.cachedValid = true
	}
	// Keep measurement databindings alive for the next Sync regardless of
	// whether Talbot ran — the cache entries that are live this frame
	// should stay live next frame.
	inst.measurer.RenewBindings()
	axis := inst.cachedAxis

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
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps-1)
		val := min + t*(max-min)
		rgba := cm.At(val)
		x := float32(i) * stepW
		c.PaintRectFilled(x, 0, x+stepW+0.5, gradientH, 0, color.Hex(rgba)).Send()
	}

	// --- Gradient border.
	c.PaintRectStroke(0, 0, inst.width, gradientH, 0, color.Hex(inst.borderColor), 0.8).Send()

	// --- Tick marks + labels. Labels are center-anchored, except within an
	// `edgeGuard` px of the left/right edges where we switch to left/right
	// anchor so the text doesn't clip the widget boundary. We always format
	// via inst.labelFormat rather than using AxisLayout.TickLabels so a user-
	// supplied WithLabelFormat — and the log-aware default — are uniformly
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
	// the caller emitted before and after. The canvas's response exposes
	// pointer hover/click via r14.
	c.PaintCanvas(inst.ids.PrepareStr("canvas"), inst.width, inst.height).
		Background(color.Hex(inst.bgColor)).
		Send()

	// Read the canvas's pointer state from the StateManager cache
	// (populated last frame by Sync) and translate it to a colormap
	// value. Store for next frame's marker and fire the optional
	// callback. Reads cache rather than inline-fetching because inline
	// fetches inside deferred-block captures (e.g. dock.Tab bodies)
	// buffer instead of flushing and deadlock the render loop.
	cp := c.CurrentApplicationState.StateManager.GetCanvasPointer()
	hx := cp.HoverX
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
	if inst.onHover != nil {
		inst.onHover(hover)
	}
}

// computeAxis picks the best tick layout for the current colormap using the
// currently selected TickerE. On failure any path falls back to a two-tick
// endpoint axis and logs a warning (validation policy: log + safe default).
func (inst *ColorScale) computeAxis(min, max float64) finddivisions.AxisLayout {
	switch inst.ticker {
	case TickerHeckbert:
		return inst.computeAxisHeckbert(min, max)
	case TickerNelder:
		return inst.computeAxisNelder(min, max)
	default:
		return inst.computeAxisTalbot(min, max)
	}
}

// computeAxisTalbot uses Talbot + TypesettingScorer for linear colormaps and
// TalbotLogarithmic for log colormaps.
func (inst *ColorScale) computeAxisTalbot(min, max float64) finddivisions.AxisLayout {
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
		96.0, // assumed logical DPI; egui treats font size in logical px
		float64(inst.width),
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
