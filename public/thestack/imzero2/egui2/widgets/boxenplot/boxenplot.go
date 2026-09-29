// Package boxenplot is a painter helper (ADR-0267) for letter-value
// (Hofmann, Wickham & Kafadar 2017) plots. It composes:
//
//   - boxer/public/analytics/stats/letterval — LV math + oracle
//   - boxer/public/analytics/stats/tdigest   — streaming quantile source
//   - the implot port's Boxes / Scatter / Text items — rendering
//     (ADR-0149 SD7; formerly the egui_plot bridge's plotBoxes)
//
// The helper takes no widget ids and holds no state: [Paint] declares one
// nested letter-value box series plus optional outlier scatter / text
// annotations into the host's open *implot.Plot (between Begin and End),
// from an [Input] whose [Style] fields are zero for the defaults
// [DefaultStyle] resolves. A host that draws several distributions into one
// plot calls Paint once per distribution with the same Style.
package boxenplot

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
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

// Style is the appearance of a letter-value plot. A zero field takes the
// value [DefaultStyle] resolves for it; [Style.Resolved] returns the style
// with every default filled in.
type Style struct {
	// OutlierMode selects the outlier-rendering strategy; see OutlierModeE.
	// The zero value is OutlierModeAuto.
	OutlierMode OutlierModeE
	// OutlierAutoThreshold is the per-tail observation count at which Auto
	// mode switches from Points (small counts) to Count (large). 0 → 20.
	OutlierAutoThreshold int64
	// Palette is the IDS sequential data-encoding palette for the per-depth
	// fills. The zero value takes [styletokens.SequentialDefault], which
	// honours the IDS_PALETTE_SEQUENTIAL environment.
	Palette styletokens.SequentialE
	// PaletteTStart / PaletteTEnd clamp the t ∈ [0, 1] range sampled from the
	// palette. Both zero → (0.20, 0.85), or (0.10, 0.95) under
	// AccessibilityHighContrast, avoiding the extreme dark/light ends.
	//
	// When the resolved palette ramps in the opposite direction to batlow
	// (white→black; only SequentialGrayC) fillForDepth swaps the range so
	// "deep=light, shallow=dark" reads consistently across presets.
	PaletteTStart float32
	PaletteTEnd   float32
	// FillAlpha is the alpha applied to every per-depth fill. 0 → 0xC0, or
	// 0xFF under AccessibilityHighContrast.
	FillAlpha uint8
	// Stroke and StrokeWidth are the box outline. Zero → NeutralBorderDefault
	// at 1.0 px.
	Stroke      color.Color
	StrokeWidth float32
	// AnnotationColor colours the "+N" outlier-count labels and the outlier
	// scatter points. Zero → NeutralTextSecondary.
	AnnotationColor color.Color
	// BoxWidth is the depth-2 (innermost LV) box width in argument-axis units
	// and WidthShrink the per-depth multiplier: each successive box is
	// BoxWidth × WidthShrink^(depth-2) wide. Zero → 0.6 and 0.85; a shrink
	// above 1 is clamped to 1 (Hofmann's constant-width convention).
	BoxWidth    float64
	WidthShrink float64
	// SeriesName is the legend label for the box series; "" → "boxen".
	// NoLegend suppresses every legend entry the helper emits.
	SeriesName string
	NoLegend   bool
	// SnapWindow is the half-width (argument-axis units) within which [At]
	// claims a hover for a distribution. 0 → 0.5, which matches unit-spaced
	// arguments and selects the nearest distribution by construction. A
	// negative value is clamped to a small positive epsilon so At never
	// matches everywhere.
	SnapWindow float64
}

// Input is one distribution to paint: the Style and where and what.
type Input struct {
	Style
	// Argument is the x position (the column) of this distribution.
	Argument float64
	// Levels are from letterval.RecommendedLevels(oracle) or
	// letterval.Levels(oracle, maxDepth). Empty paints nothing; a single
	// depth-1 entry paints the median marker alone.
	Levels []letterval.LVLevel
	// Extremes are raw extreme values for OutlierModePoints — both tails in
	// one slice, treated as opaque points. Ignored by the other modes.
	Extremes []float64
	// TailCount is an explicit per-tail outlier count for the Count / Auto
	// modes, used when TailCountKnown is set; otherwise the deepest level's
	// analytical estimate is drawn. Set it when the caller tracks the true
	// count (a top-K plus a counter).
	TailCount      int64
	TailCountKnown bool
	// Name is the distribution's display name in [Crosshair.Name]; "" takes
	// the resolved SeriesName.
	Name string
}

// DefaultStyle is the style a zero [Style] resolves to, with the palette
// range and fill alpha read from the styletokens accessibility preset
// (IDS_ACCESSIBILITY) so a typical caller never touches palette plumbing.
func DefaultStyle() (st Style) {
	access := styletokens.AccessibilityFromEnv()
	tStart, tEnd := float32(0.20), float32(0.85)
	fillAlpha := uint8(0xC0)
	if access == styletokens.AccessibilityHighContrast {
		tStart, tEnd = 0.10, 0.95
		fillAlpha = 0xFF
	}
	st = Style{
		OutlierMode:          OutlierModeAuto,
		OutlierAutoThreshold: 20,
		Palette:              styletokens.SequentialDefault(),
		PaletteTStart:        tStart,
		PaletteTEnd:          tEnd,
		FillAlpha:            fillAlpha,
		Stroke:               color.Hex(styletokens.NeutralBorderDefault.AsHex()),
		StrokeWidth:          1.0,
		AnnotationColor:      color.Hex(styletokens.NeutralTextSecondary.AsHex()),
		BoxWidth:             0.6,
		WidthShrink:          0.85,
		SeriesName:           "boxen",
		SnapWindow:           0.5,
	}
	return
}

// Resolved returns st with every zero field replaced by its default and
// the clamps applied.
func (st Style) Resolved() (out Style) {
	def := DefaultStyle()
	out = st
	if out.OutlierAutoThreshold == 0 {
		out.OutlierAutoThreshold = def.OutlierAutoThreshold
	}
	if out.Palette == 0 {
		out.Palette = def.Palette
	}
	if out.PaletteTStart == 0 && out.PaletteTEnd == 0 {
		out.PaletteTStart, out.PaletteTEnd = def.PaletteTStart, def.PaletteTEnd
	}
	if out.FillAlpha == 0 {
		out.FillAlpha = def.FillAlpha
	}
	if out.Stroke == (color.Color{}) {
		out.Stroke = def.Stroke
	}
	if out.StrokeWidth == 0 {
		out.StrokeWidth = def.StrokeWidth
	}
	if out.AnnotationColor == (color.Color{}) {
		out.AnnotationColor = def.AnnotationColor
	}
	if out.BoxWidth == 0 {
		out.BoxWidth = def.BoxWidth
	}
	switch {
	case out.WidthShrink == 0:
		out.WidthShrink = def.WidthShrink
	case out.WidthShrink < 0:
		out.WidthShrink = 0.01
	case out.WidthShrink > 1:
		out.WidthShrink = 1
	}
	if out.SeriesName == "" {
		out.SeriesName = def.SeriesName
	}
	switch {
	case out.SnapWindow == 0:
		out.SnapWindow = def.SnapWindow
	case out.SnapWindow < 0:
		out.SnapWindow = 1e-9
	}
	return
}

// seriesName is the legend label, or "" under NoLegend.
func (st Style) seriesName() string {
	if st.NoLegend {
		return ""
	}
	return st.SeriesName
}

// suffixedName decorates the series name with a fixed suffix, or stays ""
// under NoLegend so every legend entry the helper emits is suppressed.
func (st Style) suffixedName(suffix string) string {
	if st.NoLegend {
		return ""
	}
	return st.SeriesName + suffix
}

// Paint declares the boxenplot items for one distribution into the host's
// open plot: the nested letter-value boxes as one Boxes series, then the
// outliers as the resolved mode asks. Empty Levels paint nothing; a single
// depth-1 level paints the median marker alone. There is no per-box hover
// highlight on the port — [At], [PaintCrosshair] and the status lines are
// the "this box is selected" affordance.
func Paint(p *implot.Plot, in Input) {
	inst := in.Style.Resolved()
	levels, argument, extremes := in.Levels, in.Argument, in.Extremes
	if len(levels) == 0 {
		return
	}

	medianValue := medianFromLevels(levels)

	// Only depth 1 (median) available → draw a single marker, skip boxes.
	if len(levels) == 1 {
		inst.emitMedianMarker(p, argument, medianValue)
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
		inst.emitMedianMarker(p, argument, medianValue)
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

	for i, lv := range boxes {
		arguments[i] = argument
		q1s[i] = lv.LowerValue
		medians[i] = medianValue
		q3s[i] = lv.UpperValue
		wmins[i] = lv.LowerValue
		wmaxs[i] = lv.UpperValue
		widths[i] = computeBoxWidth(inst.BoxWidth, inst.WidthShrink, lv.Depth)
		fills[i] = inst.fillForDepth(lv.Depth, maxDepth)
	}

	// The nested rings render as one Boxes series (whiskers coincide with
	// the box edges here, so only the rects and the median line draw).
	// There is no per-box hover highlight on the port — the crosshair +
	// WriteStatusLine remain the "this box is selected" affordance.
	p.Boxes(inst.seriesName(), arguments, q1s, medians, q3s, wmins, wmaxs,
		widths, fills, inst.Stroke.Literal(), inst.StrokeWidth)

	tailCount := deepest.TailCount
	if in.TailCountKnown {
		tailCount = in.TailCount
	}
	mode := resolveOutlierMode(inst.OutlierMode, tailCount, inst.OutlierAutoThreshold)
	switch mode {
	case OutlierModePoints:
		inst.emitOutlierPoints(p, argument, extremes)
	case OutlierModeCount:
		inst.emitOutlierCount(p, argument, deepest, tailCount)
	}
}

// emitMedianMarker draws a single scatter point when only depth-1 LV
// (the median) is available — small-n case where no boxes are
// statistically meaningful.
func (inst Style) emitMedianMarker(p *implot.Plot, argument, median float64) {
	p.SetNextColor(inst.AnnotationColor.Literal())
	p.Scatter(inst.seriesName(), []float64{argument}, []float64{median}, implot.MarkerCircle, 3)
}

func (inst Style) emitOutlierPoints(p *implot.Plot, argument float64, extremes []float64) {
	if len(extremes) == 0 {
		return
	}
	xs := make([]float64, len(extremes))
	for i := range xs {
		xs[i] = argument
	}
	ys := make([]float64, len(extremes))
	copy(ys, extremes)
	p.SetNextColor(inst.AnnotationColor.Literal())
	p.Scatter(inst.suffixedName("-out"), xs, ys, implot.MarkerCircle, 2)
}

func (inst Style) emitOutlierCount(p *implot.Plot, argument float64, deepest letterval.LVLevel, perTailCount int64) {
	if perTailCount <= 0 {
		return
	}
	label := fmt.Sprintf("+%d", perTailCount)
	p.Text(argument, deepest.LowerValue, inst.AnnotationColor.Literal(), label)
	p.Text(argument, deepest.UpperValue, inst.AnnotationColor.Literal(), label)
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
func (inst Style) fillForDepth(depth, maxDepth uint8) uint32 {
	tStart, tEnd := inst.PaletteTStart, inst.PaletteTEnd
	if paletteIsWhiteToBlack(inst.Palette) {
		tStart, tEnd = tEnd, tStart
	}
	t := paletteT(depth, maxDepth, tStart, tEnd)
	rgba := styletokens.Sequential(inst.Palette, t)
	// Straight 0xRRGGBBAA — the Rust unpacker
	// (interpreter.rs::color32_from_rgba_u32) calls
	// Color32::from_rgba_unmultiplied, so egui handles the
	// pre-multiplication. No Go-side scaling needed.
	packed := (uint32(rgba.R) << 24) | (uint32(rgba.G) << 16) | (uint32(rgba.B) << 8) | uint32(inst.FillAlpha)
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
// → End → WriteStatusLine) can lift the same scaffold
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

// At returns the Crosshair for the distribution in describes, using the
// plot's own hover state (one frame behind, like every register read).
//
// Crosshair.Valid is true when the plot area is hovered and
// |hoverX - in.Argument| ≤ the resolved SnapWindow. The typical caller
// loops over its distributions and keeps the last Valid Crosshair the loop
// produced — the snap window is half the argument-axis spacing, so at most
// one distribution claims any given hover. Empty Levels give an invalid
// Crosshair that still carries Argument and Name.
func At(p *implot.Plot, in Input) (out Crosshair) {
	inst := in.Style.Resolved()
	argument, levels := in.Argument, in.Levels
	out.Argument = argument
	out.Name = in.Name
	if out.Name == "" {
		out.Name = inst.SeriesName
	}
	hx, hy, ok := p.HoverPlotPos()
	if !ok {
		return
	}
	if math.Abs(hx-argument) > inst.SnapWindow {
		return
	}
	if len(levels) == 0 {
		return
	}
	out.Valid = true
	out.PlotX = hx
	out.PlotY = hy
	out.Median = medianFromLevels(levels)
	out.TotalN = recoverN(levels)
	deepest := deepestLevel(levels)
	if deepest != nil {
		out.MaxDepth = deepest.Depth
		out.MaxDepthLow = deepest.LowerValue
		out.MaxDepthHigh = deepest.UpperValue
		out.MaxDepthTailCount = deepest.TailCount
	}
	if matched := findContainingLevel(levels, hy); matched != nil {
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

// PaintCrosshair declares a vertical reference line at ch.Argument
// (snapped to the matched distribution's centre, not the raw hover X)
// using the style's annotation colour at half alpha. No-op when ch.Valid
// is false. Declare it after Paint inside the same plot so the line draws
// on top of every box.
//
// The vline anchors to the argument rather than HoverX because the
// boxenplot's argument axis is categorical (one column per
// distribution); a vline at the raw cursor X would slide between
// columns and read as a "no-man's-land" cursor instead of a
// "selected distribution" affordance.
func PaintCrosshair(p *implot.Plot, st Style, ch Crosshair) {
	if !ch.Valid {
		return
	}
	inst := st.Resolved()
	p.SetNextColor(withAlpha(inst.AnnotationColor.Literal(), 0x80)).SetNextWeight(1.0)
	p.InfLinesV(inst.suffixedName("-cursor"), []float64{ch.Argument})
}

// WriteStatusLine emits a single weak-styled LabelAtoms row that
// fully summarises the hovered letter-value box for the reader,
// suitable for placement immediately below the plot block. The
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
// emit it themselves on the !ch.Valid branch. For the explaining,
// multi-line counterpart use [WriteStatusLineVerbose].
func WriteStatusLine(ch Crosshair) {
	s := formatStatusLine(ch)
	if s == "" {
		return
	}
	c.LabelAtoms(c.Atoms().BeginRichText(s).Small().Weak().End().Keep()).Send()
}

// formatStatusLine renders the terse single line, or "" when ch is not
// Valid. Pure (no egui) so the wording is unit-testable.
func formatStatusLine(ch Crosshair) string {
	if !ch.Valid {
		return ""
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
	return fmt.Sprintf(
		"%s │ x=%.4g, y=%.4g │ n=%d, median=%.4g │ %s",
		ch.Name, ch.Argument, ch.PlotY, ch.TotalN, ch.Median, summary,
	)
}

// VerboseReadoutLineCount is the fixed number of rows
// [WriteStatusLineVerbose] emits, so a host panel's status area keeps a
// constant height whether or not the cursor is over a distribution
// (mirrors ecdf.ReadoutLineCount). Hosts budget vertical space from this.
const VerboseReadoutLineCount = 3

// WriteStatusLineVerbose emits the explaining, multi-line counterpart to
// [WriteStatusLine]: it describes the hovered letter-value box in plain
// language — what quantiles its edges represent, the central fraction it
// covers, and the tail beyond it — rather than packing the same facts
// into one dense line. It always emits [VerboseReadoutLineCount] rows: the
// description when ch.Valid, a one-line hover hint otherwise, padded with
// blank rows so hovering on / off never reflows the host. Text via the
// pure [formatVerbose].
func WriteStatusLineVerbose(ch Crosshair) {
	lines := formatVerbose(ch)
	for _, ln := range lines {
		c.LabelAtoms(c.Atoms().BeginRichText(ln).Small().Weak().End().Keep()).Send()
	}
	for i := len(lines); i < VerboseReadoutLineCount; i++ {
		c.LabelAtoms(c.Atoms().BeginRichText(" ").Small().Weak().End().Keep()).Send()
	}
}

// formatVerbose renders ch into the explaining readout's rows (at most
// [VerboseReadoutLineCount]) — pure, so the wording is unit-testable. It
// describes the hovered letter-value box by the quantiles its edges
// represent (the directly-meaningful concept) and the central fraction it
// covers, or, outside every box, which tail the cursor sits in beyond the
// deepest ring. Returns a single hover hint when ch is not Valid.
func formatVerbose(ch Crosshair) (lines []string) {
	if !ch.Valid {
		return []string{"Hover a distribution to inspect its letter-value boxes."}
	}
	lines = make([]string, 0, VerboseReadoutLineCount)
	lines = append(lines, fmt.Sprintf(
		"Cursor at value y = %.4g in %q (n=%d, median=%.4g).",
		ch.PlotY, ch.Name, ch.TotalN, ch.Median))
	if ch.Depth == 0 {
		// Outside every box — in a tail beyond the deepest drawn ring.
		lowerQ := math.Ldexp(1.0, -int(ch.MaxDepth))
		upperQ := 1 - lowerQ
		if ch.PlotY > ch.MaxDepthHigh {
			lines = append(lines, fmt.Sprintf(
				"Above the %.6gth percentile — beyond the deepest drawn box [%.4g, %.4g].",
				upperQ*100, ch.MaxDepthLow, ch.MaxDepthHigh))
		} else {
			lines = append(lines, fmt.Sprintf(
				"Below the %.6gth percentile — beyond the deepest drawn box [%.4g, %.4g].",
				lowerQ*100, ch.MaxDepthLow, ch.MaxDepthHigh))
		}
		lines = append(lines, fmt.Sprintf(
			"About %d observations lie in this tail.", ch.MaxDepthTailCount))
		return
	}
	// Inside a box: name its quantile edges and the central fraction it spans.
	coverage := (ch.DepthUpperQ - ch.DepthLowerQ) * 100
	lines = append(lines, fmt.Sprintf(
		"Hovered letter-value box spans the [%.6g%%, %.6g%%] quantiles — value range [%.4g, %.4g].",
		ch.DepthLowerQ*100, ch.DepthUpperQ*100, ch.DepthLow, ch.DepthHigh))
	lines = append(lines, fmt.Sprintf(
		"It holds the central %.6g%% of the distribution; ≈%d observations lie in each tail beyond it.",
		coverage, ch.DepthTailCount))
	return
}

// withAlpha replaces the alpha byte (low 8 bits) of an RGBA-packed
// uint32, mirroring the helper of the same name in widgets/ecdf.
// Used by PaintCrosshair to dim the annotation colour for the vline
// so it reads as a secondary affordance rather than competing with
// the box outlines.
func withAlpha(packed uint32, alpha uint8) uint32 {
	return (packed &^ 0xFF) | uint32(alpha)
}
