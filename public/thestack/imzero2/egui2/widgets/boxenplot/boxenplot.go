//go:build llm_generated_opus47

// Package boxenplot is the imzero2 widget for letter-value (Hofmann,
// Wickham & Kafadar 2017) plots. It composes:
//
//   - boxer/public/analytics/stats/letterval — LV math + oracle
//   - boxer/public/analytics/stats/tdigest   — streaming quantile source
//   - egui2 plotBoxes / plotScatter / plotText primitives — rendering
//
// The widget is stateless: construct one Renderer, then call Render
// any number of times per frame. Configure-once / render-many is the
// canonical pattern (see fieldview / errorview).
//
// Caller must wrap Render calls inside a c.Plot(id) block. Each
// Render emits one BoxPlot series (with N nested BoxElems) plus
// optional outlier scatter/text annotations.
package boxenplot

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// OutlierModeE controls how observations beyond the deepest rendered
// LV level are visualised.
type OutlierModeE uint8

const (
	// OutlierModeAuto picks Points or Count based on the analytical
	// budget (per-tail expected count) against OutlierAutoThreshold.
	// Below threshold → Points; at/above → Count.
	OutlierModeAuto OutlierModeE = iota
	// OutlierModeNone draws nothing beyond the deepest box.
	OutlierModeNone
	// OutlierModePoints draws each value in the extremes slice as a
	// scatter point at (argument, value). Caller must supply the
	// extremes (e.g. K-smallest and K-largest tracked alongside the
	// digest).
	OutlierModePoints
	// OutlierModeCount draws a "+N" annotation at the lower and upper
	// edges of the deepest box, where N is the per-tail expected count.
	OutlierModeCount
)

// Renderer is the configured boxenplot. Values are immutable after
// construction; fluent setters return modified copies.
type Renderer struct {
	idPrefix              string
	outlierMode           OutlierModeE
	outlierAutoThreshold  int64
	palette               styletokens.SequentialE
	paletteTStart         float32
	paletteTEnd           float32
	fillAlpha             uint8
	strokeColorPacked     uint32
	strokeWidth           float32
	annotationColorPacked uint32
	boxWidth              float64
	widthShrink           float64
	seriesName            string
	snapWindow            float64
}

// New constructs a Renderer with IDS-aligned defaults. Palette and
// fill alpha are resolved from the styletokens Tier-1 / Tier-2 env
// surface (IDS_PALETTE_SEQUENTIAL / IDS_ACCESSIBILITY), so a typical
// caller never touches palette plumbing:
//
//	r := boxenplot.New("p99-mem")    // honours user's IDS env
//	r.Render(arg, levels, nil, -1)
//
// Static defaults:
//
//   - palette: styletokens.SequentialDefault()
//   - paletteTStart/End: [0.20, 0.85] (default) or [0.10, 0.95] under
//     AccessibilityHighContrast for boosted discriminability
//   - fillAlpha: 0xC0 (default) or 0xFF under AccessibilityHighContrast
//   - stroke: NeutralBorderDefault at 1.0 px
//   - box width: 0.6 (argument-axis units), shrink 0.85 per depth
//   - outlier mode: Auto with threshold 20
//
// idPrefix scopes any widget-id-bearing primitive emitted by Render —
// pass a stable short string (e.g. "lat-cluster", "p99-mem").
func New(idPrefix string) (inst Renderer) {
	access := styletokens.AccessibilityFromEnv()
	tStart, tEnd := float32(0.20), float32(0.85)
	fillAlpha := uint8(0xC0)
	if access == styletokens.AccessibilityHighContrast {
		tStart, tEnd = 0.10, 0.95
		fillAlpha = 0xFF
	}
	inst = Renderer{
		idPrefix:              idPrefix,
		outlierMode:           OutlierModeAuto,
		outlierAutoThreshold:  20,
		palette:               styletokens.SequentialDefault(),
		paletteTStart:         tStart,
		paletteTEnd:           tEnd,
		fillAlpha:             fillAlpha,
		strokeColorPacked:     styletokens.NeutralBorderDefault.AsHex(),
		strokeWidth:           1.0,
		annotationColorPacked: styletokens.NeutralTextSecondary.AsHex(),
		boxWidth:              0.6,
		widthShrink:           0.85,
		seriesName:            "boxen",
		snapWindow:            0.5,
	}
	return
}

// SnapWindow sets the half-width (in argument-axis units) within which
// At() claims a hover for this distribution. The default 0.5 matches
// unit-spaced arguments (1, 2, 3, …) and selects the nearest
// distribution by construction: hovering at x=1.4 lands inside argument
// 1's window but outside argument 2's. Callers with denser or sparser
// argument layouts should override accordingly. Non-positive values
// are clamped to a small positive epsilon so At() never matches
// everywhere.
func (inst Renderer) SnapWindow(w float64) (out Renderer) {
	if w <= 0 {
		w = 1e-9
	}
	inst.snapWindow = w
	out = inst
	return
}

// SeriesName sets the legend label for the BoxPlot series. Empty
// disables the legend entry for this series. Default "boxen".
func (inst Renderer) SeriesName(name string) (out Renderer) {
	inst.seriesName = name
	out = inst
	return
}

// OutlierMode selects the outlier-rendering strategy. See OutlierModeE
// for the enumerated semantics.
func (inst Renderer) OutlierMode(m OutlierModeE) (out Renderer) {
	inst.outlierMode = m
	out = inst
	return
}

// OutlierAutoThreshold sets the per-tail observation count at which
// Auto mode switches from Points (small counts) to Count (large).
// Default 20.
func (inst Renderer) OutlierAutoThreshold(n int64) (out Renderer) {
	inst.outlierAutoThreshold = n
	out = inst
	return
}

// Palette selects the IDS sequential data-encoding palette used for
// per-depth fills. Default SequentialBatlow.
func (inst Renderer) Palette(p styletokens.SequentialE) (out Renderer) {
	inst.palette = p
	out = inst
	return
}

// PaletteRange clamps the t ∈ [0, 1] range sampled from the palette.
// Defaults (0.20, 0.85) avoid the extreme dark/light ends which lose
// shape against typical dark IDS backgrounds.
//
// Note: when the resolved palette ramps in the opposite direction to
// batlow (white→black; currently only SequentialGrayC), fillForDepth
// silently swaps the supplied (start, end) so "deep=light,
// shallow=dark" reads consistently across presets. Callers needing
// the supplied range verbatim must avoid SequentialGrayC or override
// fillForDepth directly — there is no per-call opt-out today.
func (inst Renderer) PaletteRange(start, end float32) (out Renderer) {
	inst.paletteTStart = start
	inst.paletteTEnd = end
	out = inst
	return
}

// FillAlpha sets the alpha channel applied to every per-depth fill,
// overriding the palette's opaque output. Default 0xC0.
func (inst Renderer) FillAlpha(a uint8) (out Renderer) {
	inst.fillAlpha = a
	out = inst
	return
}

// Stroke sets the box outline colour and width (px). Default
// NeutralBorderDefault at 1.0 px.
func (inst Renderer) Stroke(col color.Color, widthPx float32) (out Renderer) {
	inst.strokeColorPacked = col.Literal()
	inst.strokeWidth = widthPx
	out = inst
	return
}

// AnnotationColor sets the colour used for the "+N" outlier-count
// labels and for any outlier scatter points. Default
// NeutralTextSecondary.
func (inst Renderer) AnnotationColor(col color.Color) (out Renderer) {
	inst.annotationColorPacked = col.Literal()
	out = inst
	return
}

// BoxWidth sets the depth-2 (innermost LV) box width in argument-axis
// units, plus a per-depth shrink multiplier. Each successive box is
// width × shrink^(depth-2) wide. Defaults: base 0.6, shrink 0.85.
// shrink = 1.0 gives Hofmann's constant-width convention; shrink < 1
// produces the seaborn-style taper. Values outside (0, 1] are
// clamped.
func (inst Renderer) BoxWidth(base, shrink float64) (out Renderer) {
	inst.boxWidth = base
	if shrink <= 0 {
		shrink = 0.01
	} else if shrink > 1 {
		shrink = 1
	}
	inst.widthShrink = shrink
	out = inst
	return
}

// Render emits the boxenplot primitives for one distribution at the
// given x position. Caller must already be inside a Plot block.
//
//   - levels: from letterval.RecommendedLevels(oracle) or
//     letterval.Levels(oracle, maxDepth). May be empty (no-op) or
//     contain only depth 1 (single median marker is drawn).
//   - extremes: raw extreme values for OutlierModePoints. Ignored by
//     other modes; can be nil there. Provide both lower and upper
//     extremes in one slice (the renderer treats the slice as
//     opaque points, no sign convention).
//   - perTailCountOverride: optional explicit per-tail outlier count
//     for Count / Auto modes. Pass -1 to use the analytical estimate
//     (deepest LV's TailCount). Use the override when the caller
//     tracks the true count separately (a top-K + counter).
func (inst Renderer) Render(argument float64, levels []letterval.LVLevel, extremes []float64, perTailCountOverride int64) {
	if len(levels) == 0 {
		return
	}

	medianValue := medianFromLevels(levels)

	// Only depth 1 (median) available → draw a single marker, skip boxes.
	if len(levels) == 1 {
		inst.emitMedianMarker(argument, medianValue)
		return
	}

	deepest := levels[len(levels)-1]
	// Skip any leading depth-1 entries (the median sentinel) from the
	// box list; the standard letterval.Levels output has exactly one,
	// but hand-crafted inputs may have none or multiple.
	boxes := levels
	for len(boxes) > 0 && boxes[0].Depth < 2 {
		boxes = boxes[1:]
	}
	n := len(boxes)
	if n == 0 {
		inst.emitMedianMarker(argument, medianValue)
		return
	}

	maxDepth := deepest.Depth

	arguments := make([]float64, n)
	q1s := make([]float64, n)
	medians := make([]float64, n)
	q3s := make([]float64, n)
	wmins := make([]float64, n)
	wmaxs := make([]float64, n)
	widths := make([]float64, n)
	fills := make([]uint32, n)
	strokes := make([]uint32, n)
	sws := make([]float32, n)

	for i, lv := range boxes {
		arguments[i] = argument
		q1s[i] = lv.LowerValue
		medians[i] = medianValue
		q3s[i] = lv.UpperValue
		wmins[i] = lv.LowerValue
		wmaxs[i] = lv.UpperValue
		widths[i] = computeBoxWidth(inst.boxWidth, inst.widthShrink, lv.Depth)
		fills[i] = inst.fillForDepth(lv.Depth, maxDepth)
		strokes[i] = inst.strokeColorPacked
		sws[i] = inst.strokeWidth
	}

	// SuppressElementText silences egui_plot's auto-generated text label
	// ("Max / Upper whisker / Q3 / median / Q1 / Lower whisker / Min")
	// — the auto-sized envelope that clipped at narrow tooltips and
	// windows — while keeping the on-hover box highlight and the axis
	// rulers. The bottom WriteStatusLine becomes the textual readout;
	// the highlight + rulers remain as the visual "this box is selected"
	// affordance.
	c.PlotBoxes(inst.seriesName, arguments, q1s, medians, q3s, wmins, wmaxs,
		widths, fills, strokes, sws).SuppressElementText().Send()

	tailCount := perTailCountOverride
	if tailCount < 0 {
		tailCount = deepest.TailCount
	}
	mode := resolveOutlierMode(inst.outlierMode, tailCount, inst.outlierAutoThreshold)
	switch mode {
	case OutlierModePoints:
		inst.emitOutlierPoints(argument, extremes)
	case OutlierModeCount:
		inst.emitOutlierCount(argument, deepest, tailCount)
	}
}

// emitMedianMarker draws a single scatter point when only depth-1 LV
// (the median) is available — small-n case where no boxes are
// statistically meaningful.
func (inst Renderer) emitMedianMarker(argument, median float64) {
	annotationColor := color.Hex(inst.annotationColorPacked)
	c.PlotScatter(inst.seriesName,
		[]float64{argument}, []float64{median}).
		Color(annotationColor).
		Radius(3).
		Shape(0).
		Send()
}

func (inst Renderer) emitOutlierPoints(argument float64, extremes []float64) {
	if len(extremes) == 0 {
		return
	}
	xs := make([]float64, len(extremes))
	for i := range xs {
		xs[i] = argument
	}
	ys := make([]float64, len(extremes))
	copy(ys, extremes)
	annotationColor := color.Hex(inst.annotationColorPacked)
	c.PlotScatter(inst.suffixedName("-out"), xs, ys).
		Color(annotationColor).
		Radius(2).
		Shape(0).
		Send()
}

func (inst Renderer) emitOutlierCount(argument float64, deepest letterval.LVLevel, perTailCount int64) {
	if perTailCount <= 0 {
		return
	}
	label := fmt.Sprintf("+%d", perTailCount)
	annotationColor := color.Hex(inst.annotationColorPacked)
	c.PlotText(inst.suffixedName("-cb"), argument, deepest.LowerValue, label).
		Color(annotationColor).
		Send()
	c.PlotText(inst.suffixedName("-ca"), argument, deepest.UpperValue, label).
		Color(annotationColor).
		Send()
}

// suffixedName decorates the series name with a fixed suffix. Returns
// the empty string when seriesName is empty, so SeriesName("") truly
// suppresses every legend entry the renderer emits (not just the box
// series).
func (inst Renderer) suffixedName(suffix string) string {
	if inst.seriesName == "" {
		return ""
	}
	return inst.seriesName + suffix
}

// fillForDepth maps an LV depth to its RGBA-packed fill colour. The
// innermost rendered depth (2) is sampled at paletteTStart (darker
// batlow), the outermost (maxDepth) at paletteTEnd (lighter) — a
// "shallow=dark, deep=light" gradient that mirrors Hofmann's
// shading convention.
//
// Crameri's grayC ramps white→black (opposite of batlow's dark→light),
// so when the resolved palette is grayC we swap the t-range to keep
// the Hofmann reading consistent across all palettes — toggling the
// IDS_ACCESSIBILITY preset must not silently flip "deep=light" to
// "deep=dark".
func (inst Renderer) fillForDepth(depth, maxDepth uint8) uint32 {
	tStart, tEnd := inst.paletteTStart, inst.paletteTEnd
	if paletteIsWhiteToBlack(inst.palette) {
		tStart, tEnd = tEnd, tStart
	}
	t := paletteT(depth, maxDepth, tStart, tEnd)
	rgba := styletokens.Sequential(inst.palette, t)
	// Straight 0xRRGGBBAA — the Rust unpacker
	// (interpreter.rs::color32_from_rgba_u32) calls
	// Color32::from_rgba_unmultiplied, so egui handles the
	// pre-multiplication. No Go-side scaling needed.
	packed := (uint32(rgba.R) << 24) | (uint32(rgba.G) << 16) | (uint32(rgba.B) << 8) | uint32(inst.fillAlpha)
	return packed
}

// paletteIsWhiteToBlack identifies sequential palettes whose upstream
// LUT direction is reversed relative to batlow's "low=dark, high=light"
// convention. Currently only Crameri's grayC.
func paletteIsWhiteToBlack(p styletokens.SequentialE) bool {
	return p == styletokens.SequentialGrayC
}

// Crosshair captures the cursor position over a boxenplot's Plot block
// and every derived statistic needed to interpret a hovered letter-
// value box: the matched distribution's series name and argument, the
// recovered sample size and median, the innermost LV depth whose box
// contains the hover Y (plus that depth's quantile range, value
// bounds, and analytical per-tail count), and the deepest LV's bounds
// and tail count so a cursor outside every drawn box is still placed
// relative to the outermost ring.
//
// Valid is false when no hover information is currently available —
// the cursor is outside the plot, the cached hover refers to a
// different plot id, or no distribution claimed the hover via At() this
// frame.
//
// Crosshair is intentionally analogous to ecdf.Crosshair so callers
// already familiar with the ECDF pattern (At → Render → PaintCrosshair
// → c.Plot(...).Send → WriteStatusLine) can lift the same scaffold
// across the two widgets without re-learning the contract.
type Crosshair struct {
	Valid bool

	// Raw hover position in plot-data coordinates.
	PlotX float64
	PlotY float64

	// Distribution context. TotalN is recovered from the levels slice
	// via the shallowest non-median LV (TailCount = floor(n · 2⁻ᵈ));
	// the recovery is exact for samples whose n is divisible by 2ᵈ and
	// otherwise off by < 2ᵈ — fine for a human-readable readout.
	Argument float64
	Name     string
	Median   float64
	TotalN   int64

	// Innermost LV box containing PlotY. Depth==0 signals "outside every
	// drawn box" — PlotY is in the tail beyond the deepest ring; the
	// remaining Depth* fields are zero/NaN in that case.
	Depth          uint8
	DepthLowerQ    float64
	DepthUpperQ    float64
	DepthLow       float64
	DepthHigh      float64
	DepthTailCount int64

	// Deepest LV in the distribution. Always populated when Valid so
	// the status line can describe where PlotY sits relative to the
	// outermost ring even when no box contains it.
	MaxDepth          uint8
	MaxDepthLow       float64
	MaxDepthHigh      float64
	MaxDepthTailCount int64
}

// At returns the Crosshair for the (argument, name, levels) tuple
// describing one distribution rendered into the given plot. The
// caller passes the same plotID it will hand to c.Plot — as an
// AbsoluteWidgetId (not ids.PrepareStr), so the id is stable across
// frames and matches the r15 hover register's stored value.
//
// Crosshair.Valid is true when:
//   - the r15 hover register names plotID as the hovered plot,
//   - HoverX is finite,
//   - and |HoverX - argument| ≤ snapWindow (default 0.5).
//
// The typical caller loops over its distributions and keeps the last
// Valid Crosshair the loop produced — the snap window is half the
// argument-axis spacing, so at most one distribution claims any given
// hover. levels may be empty (the depth-1-only median-marker case);
// the returned Crosshair still reports the median and Argument while
// Depth stays at 0.
func (inst Renderer) At(plotID c.AbsoluteWidgetId, argument float64, name string, levels []letterval.LVLevel) (out Crosshair) {
	out.Argument = argument
	out.Name = name
	hover := c.CurrentApplicationState.StateManager.GetPlotPointer()
	if hover.HoverPlotId != plotID.Derive() || math.IsNaN(hover.HoverX) {
		return
	}
	if math.Abs(hover.HoverX-argument) > inst.snapWindow {
		return
	}
	if len(levels) == 0 {
		return
	}
	out.Valid = true
	out.PlotX = hover.HoverX
	out.PlotY = hover.HoverY
	out.Median = medianFromLevels(levels)
	out.TotalN = recoverN(levels)
	deepest := deepestLevel(levels)
	if deepest != nil {
		out.MaxDepth = deepest.Depth
		out.MaxDepthLow = deepest.LowerValue
		out.MaxDepthHigh = deepest.UpperValue
		out.MaxDepthTailCount = deepest.TailCount
	}
	if matched := findContainingLevel(levels, hover.HoverY); matched != nil {
		out.Depth = matched.Depth
		out.DepthLowerQ = matched.LowerQ
		out.DepthUpperQ = matched.UpperQ
		out.DepthLow = matched.LowerValue
		out.DepthHigh = matched.UpperValue
		out.DepthTailCount = matched.TailCount
	} else {
		out.DepthLow = math.NaN()
		out.DepthHigh = math.NaN()
		out.DepthLowerQ = math.NaN()
		out.DepthUpperQ = math.NaN()
	}
	return
}

// PaintCrosshair emits a vertical PlotVLine at ch.Argument (snapped
// to the matched distribution's centre, not the raw hover X) using
// the renderer's annotation colour at half alpha. No-op when
// ch.Valid is false. Must be invoked inside the same c.Plot block as
// Render — the egui_plot drain renders vlines after box series so
// the crosshair sits visually on top of every box.
//
// The vline anchors to the argument rather than HoverX because the
// boxenplot's argument axis is categorical (one column per
// distribution); a vline at the raw cursor X would slide between
// columns and read as a "no-man's-land" cursor instead of a
// "selected distribution" affordance.
func (inst Renderer) PaintCrosshair(ch Crosshair) {
	if !ch.Valid {
		return
	}
	c.PlotVLine(inst.suffixedName("-cursor"), ch.Argument).
		Color(color.Hex(withAlpha(inst.annotationColorPacked, 0x80))).
		Width(1.0).
		Send()
}

// WriteStatusLine emits a single weak-styled LabelAtoms row that
// fully summarises the hovered letter-value box for the reader,
// suitable for placement immediately below c.Plot(...).Send(). The
// content names the distribution, anchors the cursor on the value
// axis, and describes the hovered ring by the quantiles its edges
// represent (the directly-meaningful concept) rather than by the
// Hofmann/Wickham/Kafadar "letter-value depth" (an academic index
// for the same thing).
//
//   - Inside a box (ch.Depth ≥ 2):
//     `<name> │ x=…, y=…  │  n=…, median=…  │  quantiles [lo%, hi%] = [v_lo, v_hi]  │  ≈… obs/tail beyond`
//   - Above the deepest box (ch.Depth == 0, ch.PlotY > ch.MaxDepthHigh):
//     `<name> │ x=…, y=…  │  n=…, median=…  │  above hi-th percentile (deepest box [v_lo, v_hi])  │  ≈… obs in this tail`
//   - Below the deepest box (ch.Depth == 0, ch.PlotY < ch.MaxDepthLow):
//     `<name> │ x=…, y=…  │  n=…, median=…  │  below lo-th percentile (deepest box [v_lo, v_hi])  │  ≈… obs in this tail`
//
// Quantile percentages render via `%g` so a depth-3 box reads
// `[12.5%, 87.5%]` and depth-2 reads `[25%, 75%]`. Coverage (the
// fraction of the distribution inside the box) is derivable as
// `hi% − lo%` and intentionally not duplicated on the line to keep
// it scannable. The tail count is the analytical estimate from the
// matched LV (inside-box) or the deepest LV (outside-box) — the
// same number OutlierModeCount draws as `+N`.
//
// No-op when ch.Valid is false; callers that want a placeholder
// message ("hover a distribution to inspect cursor values") should
// emit it themselves on the !ch.Valid branch.
func WriteStatusLine(ch Crosshair) {
	if !ch.Valid {
		return
	}
	var summary string
	if ch.Depth == 0 {
		// Cursor outside every drawn box. Identify which tail (above
		// the deepest upper edge or below the deepest lower edge) and
		// name the corresponding deepest-LV percentile.
		lowerQ := math.Ldexp(1.0, -int(ch.MaxDepth))
		upperQ := 1 - lowerQ
		if ch.PlotY > ch.MaxDepthHigh {
			summary = fmt.Sprintf(
				"above %gth percentile (deepest box [%.4g, %.4g]) │ ≈%d obs in this tail",
				upperQ*100,
				ch.MaxDepthLow, ch.MaxDepthHigh,
				ch.MaxDepthTailCount,
			)
		} else {
			summary = fmt.Sprintf(
				"below %gth percentile (deepest box [%.4g, %.4g]) │ ≈%d obs in this tail",
				lowerQ*100,
				ch.MaxDepthLow, ch.MaxDepthHigh,
				ch.MaxDepthTailCount,
			)
		}
	} else {
		summary = fmt.Sprintf(
			"quantiles [%g%%, %g%%] = [%.4g, %.4g] │ ≈%d obs/tail beyond",
			ch.DepthLowerQ*100, ch.DepthUpperQ*100,
			ch.DepthLow, ch.DepthHigh,
			ch.DepthTailCount,
		)
	}
	txt := fmt.Sprintf(
		"%s │ x=%.4g, y=%.4g │ n=%d, median=%.4g │ %s",
		ch.Name, ch.Argument, ch.PlotY, ch.TotalN, ch.Median, summary,
	)
	c.LabelAtoms(c.Atoms().BeginRichText(txt).Small().Weak().End().Keep()).Send()
}

// withAlpha replaces the alpha byte (low 8 bits) of an RGBA-packed
// uint32, mirroring the helper of the same name in widgets/ecdf.
// Used by PaintCrosshair to dim the annotation colour for the vline
// so it reads as a secondary affordance rather than competing with
// the box outlines.
func withAlpha(packed uint32, alpha uint8) uint32 {
	return (packed &^ 0xFF) | uint32(alpha)
}
