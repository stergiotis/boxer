// Package ecdf is the imzero2 widget for plotting an empirical CDF
// together with a finite-sample exact simultaneous confidence band
// (Berk-Jones by default; DKW / equal-precision / higher-criticism
// available per the underlying ecdfbands library).
//
// The widget is stateless: construct one Renderer with a fluent
// builder, then call Render once per frame inside a c.Plot block.
// Each Render emits two FFFI2 primitives — one shaded polygon for
// the band and one polyline for the ECDF step curve.
package ecdf

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Renderer is the configured ECDF + confidence band visualiser.
// Zero value is not usable — construct via New.
type Renderer struct {
	method           ecdfbands.BandMethodE
	alpha            float64
	bandFillPacked   uint32
	bandStrokePacked uint32
	bandStrokeWidth  float32
	ecdfStrokePacked uint32
	ecdfStrokeWidth  float32
	seriesName       string
	bandSeriesName   string
}

// New constructs a Renderer with IDS-aligned defaults.
//
// Static defaults:
//
//   - method:      BandMethodBerkJones (tail-tight, default)
//   - alpha:       0.05 (95% simultaneous coverage)
//   - bandFill:    AccentDefault with 0x40 alpha overlay
//   - bandStroke:  AccentDefault at 0px width (no outline)
//   - ecdfStroke:  NeutralTextPrimary at 1.5 px
//   - seriesName:  "ECDF" (band's legend label is "<seriesName> band")
func New() (inst Renderer) {
	inst = Renderer{
		method:           ecdfbands.BandMethodBerkJones,
		alpha:            0.05,
		bandFillPacked:   packRGBA(styletokens.AccentDefault, 0x40),
		bandStrokePacked: styletokens.AccentDefault.AsHex(),
		bandStrokeWidth:  0,
		ecdfStrokePacked: styletokens.NeutralTextPrimary.AsHex(),
		ecdfStrokeWidth:  1.5,
		seriesName:       "ECDF",
		bandSeriesName:   "ECDF band",
	}
	return
}

// Method sets the confidence-band family. Default BandMethodBerkJones.
func (inst Renderer) Method(m ecdfbands.BandMethodE) (out Renderer) {
	inst.method = m
	out = inst
	return
}

// BandMethod returns the configured exact-band family — the getter
// counterpart to [Renderer.Method], so a host widget can name the band
// (e.g. in an always-visible status line) without re-deriving it.
func (inst Renderer) BandMethod() ecdfbands.BandMethodE { return inst.method }

// Alpha sets the complement-of-coverage level. The band realises
// (1-α)·100% simultaneous coverage. Default 0.05.
func (inst Renderer) Alpha(a float64) (out Renderer) {
	inst.alpha = a
	out = inst
	return
}

// BandFill overrides the polygon fill colour applied to the band
// region. Default AccentDefault at 0x40 alpha.
func (inst Renderer) BandFill(col color.Color) (out Renderer) {
	inst.bandFillPacked = col.Literal()
	out = inst
	return
}

// BandStroke sets the polygon outline colour and width for the band.
// Default 0 px (no outline) — the band reads as a fill alone.
func (inst Renderer) BandStroke(col color.Color, widthPx float32) (out Renderer) {
	inst.bandStrokePacked = col.Literal()
	inst.bandStrokeWidth = widthPx
	out = inst
	return
}

// EcdfStroke sets the colour and width of the ECDF step polyline.
// Default NeutralTextPrimary at 1.5 px.
func (inst Renderer) EcdfStroke(col color.Color, widthPx float32) (out Renderer) {
	inst.ecdfStrokePacked = col.Literal()
	inst.ecdfStrokeWidth = widthPx
	out = inst
	return
}

// SeriesName sets the legend label for the ECDF series. The band
// series uses "<seriesName> band".
func (inst Renderer) SeriesName(name string) (out Renderer) {
	inst.seriesName = name
	inst.bandSeriesName = name + " band"
	out = inst
	return
}

// Render emits the ECDF + confidence band primitives for one sorted
// iid sample. The caller must already be inside a c.Plot block.
//
// sorted must be non-decreasing; the underlying ecdfbands library
// rejects unsorted inputs with an error. n must be ≥ 2 for a
// meaningful band; n = 0 / 1 short-circuit (no emit).
//
// The render order is:
//  1. n-1 PlotPolygon rectangles for the shaded band (one per ECDF
//     plateau, drawn first so they sit under the curve).
//  2. One PlotLine for the ECDF step polyline (drawn on top).
func (inst Renderer) Render(sorted []float64) (err error) {
	n := len(sorted)
	if n < 2 {
		return
	}
	band, err := ecdfbands.BandsForSample(sorted, inst.alpha, inst.method)
	if err != nil {
		err = eh.Errorf("ecdf band: %w", err)
		return
	}
	inst.emitBandRectangles(band.Xs, band.LowerCDF, band.UpperCDF)
	inst.emitEcdfPolyline(sorted)
	return
}

// RenderGrid renders the ECDF + confidence band at an explicit
// (xs, fnAt) grid, mirroring ecdfbands.BandsForGrid. n is the total
// sample size on which the ECDF estimator was built (typically much
// larger than len(xs)) — the band's calibration depends on n, not
// on the grid resolution.
//
// Use this when the sample is too large to sort (a t-digest or
// Greenwald-Khanna sketch is the typical source) or when the
// visualisation grid is intentionally coarser than the underlying
// data. xs and fnAt must satisfy the same validation as
// BandsForGrid: monotone non-decreasing, fnAt ∈ [0, 1].
//
// Render order matches Render: band rectangles first, then the
// ECDF step curve from the (xs, fnAt) grid.
func (inst Renderer) RenderGrid(xs, fnAt []float64, n int) (err error) {
	if len(xs) < 2 {
		return
	}
	g, err := ecdfbands.BandsForGrid(xs, fnAt, n, inst.alpha, inst.method)
	if err != nil {
		err = eh.Errorf("ecdf grid band: %w", err)
		return
	}
	inst.emitBandRectangles(g.Xs, g.LowerCDF, g.UpperCDF)
	inst.emitGridEcdfPolyline(g.Xs, fnAt)
	return
}

// RenderGridPreview draws the instant closed-form DKW preview band (via
// [ecdfbands.DkwBandForGrid]) plus the ECDF grid curve. Unlike RenderGrid
// it never blocks on the O(n²) inversion, so it is the band to draw every
// frame while the tighter exact band (the renderer's configured Method)
// warms in the background or waits behind an explicit compute request. The
// conservative DKW strip is wider than the exact band — most visibly in
// the tails — so swapping to the exact band reads as a tightening. The
// caller must already be inside a c.Plot block.
func (inst Renderer) RenderGridPreview(xs, fnAt []float64, n int) (err error) {
	if len(xs) < 2 {
		return
	}
	g, err := ecdfbands.DkwBandForGrid(xs, fnAt, n, inst.alpha)
	if err != nil {
		err = eh.Errorf("ecdf preview band: %w", err)
		return
	}
	inst.emitBandRectangles(g.Xs, g.LowerCDF, g.UpperCDF)
	inst.emitGridEcdfPolyline(g.Xs, fnAt)
	return
}

// BandReady reports whether this renderer's (n, α, method) confidence
// band is already cached — i.e. whether RenderGrid/AtGrid will draw
// without blocking on the O(n²) inversion. Non-blocking probe; pair it
// with EnsureBandJob to drive the schedule-and-show-progress path.
func (inst Renderer) BandReady(n int) bool {
	return ecdfbands.BandReady(n, inst.alpha, inst.method)
}

// EnsureBandJob schedules (once, idempotently) a background warm-up of
// this renderer's (n, α, method) band under jobKey — a stable per-inspector
// identity the host widget supplies (its per-call scope) — and returns the
// current progress snapshot. tasks may be nil (the solve still runs; only
// keelson task integration is skipped). Call on frames where BandReady(n)
// is false: render RenderGridCurveOnly for the curve and show the returned
// snapshot via a progress widget below the plot. Pair it with
// CancelBandJob(jobKey) when the inspector closes so a long solve does not
// outlive the window that asked for it.
func (inst Renderer) EnsureBandJob(jobKey string, tasks task.TaskApiI, n int) BandJobSnapshot {
	return ensureBandWarm(jobKey, tasks, n, inst.alpha, inst.method)
}

// CancelBandJob aborts the background band warm-up scheduled under jobKey
// by EnsureBandJob, if one is in flight, and forgets it. Idempotent — a
// no-op when nothing is registered for jobKey — so it is safe to call every
// frame an inspector is closed. It is a package function rather than a
// Renderer method because the job is identified by jobKey alone: the
// renderer's own (α, method) configuration is irrelevant to which solve to
// stop. A band that already finished stays in the shared ecdfbands cache,
// so a reopen still renders instantly.
func CancelBandJob(jobKey string) {
	cancelBandJob(jobKey)
}

// RenderGridCurveOnly emits only the ECDF step polyline for an (xs,
// fnAt) grid — the band-free counterpart to RenderGrid, drawn while the
// confidence band is still warming in the background. The caller must
// already be inside a c.Plot block.
func (inst Renderer) RenderGridCurveOnly(xs, fnAt []float64) {
	if len(xs) < 2 {
		return
	}
	inst.emitGridEcdfPolyline(xs, fnAt)
}

// emitBandRectangles emits the confidence band as a sequence of
// convex per-segment rectangles. Each rectangle covers one ECDF
// plateau [xs[i], xs[i+1]] × [lower[i], upper[i]] and renders
// via one PlotPolygon. We use rectangles rather than a single
// staircase polygon because egui_plot's polygon tessellator
// produces visible triangulation artifacts on highly non-convex
// staircase shapes; per-rectangle emission costs more FFFI2
// primitives (n-1 vs 1) but is correct by construction.
//
// All rectangles share the same legend entry (bandSeriesName),
// achieved by passing the same name to every PlotPolygon call —
// egui_plot deduplicates legend entries by name.
func (inst Renderer) emitBandRectangles(xs, lower, upper []float64) {
	n := len(xs)
	for i := 0; i < n-1; i++ {
		rxs := []float64{xs[i], xs[i+1], xs[i+1], xs[i]}
		rys := []float64{lower[i], lower[i], upper[i], upper[i]}
		c.PlotPolygon(inst.bandSeriesName, rxs, rys,
			inst.bandFillPacked, inst.bandStrokePacked, inst.bandStrokeWidth).Send()
	}
}

// emitEcdfPolyline walks the ECDF step function for a complete
// sorted sample and emits one PlotLine. The polyline starts at
// (sorted[0], 0) and ascends in 1/n steps up to (sorted[n-1], 1).
func (inst Renderer) emitEcdfPolyline(sorted []float64) {
	xs, ys := buildEcdfPolyline(sorted)
	c.PlotLine(inst.seriesName, xs, ys).
		Color(color.Hex(inst.ecdfStrokePacked)).
		Width(inst.ecdfStrokeWidth).
		Send()
}

// emitGridEcdfPolyline emits the ECDF curve at an explicit grid
// where F_n is already known. The curve is rendered as a piecewise-
// linear polyline through (xs[i], fnAt[i]) — appropriate for grids
// dense enough that the underlying step structure is below visual
// resolution. Coarse grids will show as linear segments between
// known points; that is the right visual for sketch-backed ECDFs.
func (inst Renderer) emitGridEcdfPolyline(xs, fnAt []float64) {
	c.PlotLine(inst.seriesName, xs, fnAt).
		Color(color.Hex(inst.ecdfStrokePacked)).
		Width(inst.ecdfStrokeWidth).
		Send()
}

// packRGBA combines an RGBA color token with an explicit alpha byte
// override. The token's own alpha is replaced; this is the standard
// way IDS-aligned widgets soften a fully-opaque accent into a
// subtle fill.
func packRGBA(col styletokens.RGBA8, alpha uint8) uint32 {
	return (uint32(col.R) << 24) | (uint32(col.G) << 16) | (uint32(col.B) << 8) | uint32(alpha)
}

// BandKindE classifies which confidence band a [Crosshair]'s [LowerX,
// UpperX] edges were read from, so the readout can name it honestly.
type BandKindE uint8

const (
	// BandNone: no band edges populated. The readout omits the band line.
	BandNone BandKindE = iota
	// BandExact: edges from the configured exact family ([Crosshair.Method])
	// at calibration size [Crosshair.BandN] — the tighter band behind the
	// optional background warm-up.
	BandExact
	// BandPreview: edges from the instant closed-form DKW preview (always at
	// the true sample size); wider / conservative.
	BandPreview
)

// Crosshair captures the cursor position over an ECDF plot and the
// derived statistics most readers want to inspect at that point: the
// empirical CDF value F_n(x), the simultaneous confidence band
// [LowerX, UpperX] at x, and the nearest order statistic X_(NearestIdx+1).
// Valid is false when no hover information is currently available —
// the cursor is outside the plot, no plot has rendered yet this
// session, or the cached hover refers to a different plot id.
//
// Alpha echoes Renderer.Alpha so WriteStatusLine can derive the
// coverage label "(1-α)·100%" without the caller having to plumb it
// through. The band-provenance fields (BandKind, Method, BandN,
// SampleN, FromGrid) let WriteStatusLine name the band honestly and
// surface staleness — see those fields' notes (ADR-0093).
type Crosshair struct {
	Valid      bool
	X          float64
	Y          float64
	FnX        float64
	LowerX     float64
	UpperX     float64
	NearestX   float64
	NearestIdx int
	Alpha      float64

	// BandKind classifies [LowerX, UpperX] (exact vs DKW preview vs none)
	// so the readout can label it; Method names the exact family
	// (immaterial for the DKW preview, which the readout names explicitly).
	BandKind BandKindE
	Method   ecdfbands.BandMethodE

	// BandN is the sample size the band's critical value was calibrated at;
	// SampleN is the true current sample size. They differ when the host
	// caps or buckets the exact-band n (BandN < SampleN) — then the band is
	// a conservative over-cover and the readout says so. The grid entry
	// points set BandN; the host sets SampleN, since only it knows the true
	// count distinct from the bucketed / capped solve size.
	BandN   int
	SampleN int

	// FromGrid is true for the streaming/grid paths (AtGrid / AtGridPreview),
	// where "nearest" is a grid evaluation point rather than an observed
	// order statistic — the readout phrases it honestly instead of
	// mislabelling a grid point as X_(i).
	FromGrid bool
}

// At returns the crosshair info for the sample at the cursor
// position reported by the StateManager hover register. plotID is
// the absolute widget id you passed to c.Plot — its Derive() output
// is compared against the hover register's HoverPlotId so a stale
// cached hover from a different plot does not surface as a valid
// crosshair.
//
// Crosshair.Valid is false when the hover is unset, refers to a
// different plot, or sorted is empty. Cheap to call: BandsForSample
// is cached by (n, α, method); the per-call cost is two O(log n)
// binary searches plus a slice copy out of the band cache.
func (inst Renderer) At(plotID c.AbsoluteWidgetId, sorted []float64) (out Crosshair) {
	out.NearestIdx = -1
	out.Alpha = inst.alpha
	if len(sorted) < 1 {
		return
	}
	hover := c.CurrentApplicationState.StateManager.GetPlotPointer()
	if hover.HoverPlotId != plotID.Derive() || math.IsNaN(hover.HoverX) {
		return
	}
	band, err := ecdfbands.BandsForSample(sorted, inst.alpha, inst.method)
	if err != nil {
		return
	}
	x := hover.HoverX
	nIdx := nearestIdx(sorted, x)
	out.Valid = true
	out.X = x
	out.Y = hover.HoverY
	out.FnX = fnAtXSorted(sorted, x)
	out.LowerX, out.UpperX = bandAtX(band.Xs, band.LowerCDF, band.UpperCDF, x)
	out.NearestX = sorted[nIdx]
	out.NearestIdx = nIdx
	out.BandKind = BandExact
	out.Method = inst.method
	out.BandN = len(sorted)
	out.SampleN = len(sorted)
	out.FromGrid = false
	return
}

// AtGrid mirrors At for the streaming/grid path used by RenderGrid.
// xs and fnAt are the same grid arrays passed to RenderGrid; n is
// the total sample size on which the underlying ECDF estimator was
// built (typically much larger than len(xs)).
func (inst Renderer) AtGrid(plotID c.AbsoluteWidgetId, xs, fnAt []float64, n int) (out Crosshair) {
	out.NearestIdx = -1
	out.Alpha = inst.alpha
	if len(xs) < 1 {
		return
	}
	hover := c.CurrentApplicationState.StateManager.GetPlotPointer()
	if hover.HoverPlotId != plotID.Derive() || math.IsNaN(hover.HoverX) {
		return
	}
	g, err := ecdfbands.BandsForGrid(xs, fnAt, n, inst.alpha, inst.method)
	if err != nil {
		return
	}
	x := hover.HoverX
	nIdx := nearestIdx(xs, x)
	out.Valid = true
	out.X = x
	out.Y = hover.HoverY
	out.FnX = fnAtXGrid(xs, fnAt, x)
	out.LowerX, out.UpperX = bandAtX(g.Xs, g.LowerCDF, g.UpperCDF, x)
	out.NearestX = xs[nIdx]
	out.NearestIdx = nIdx
	out.BandKind = BandExact
	out.Method = inst.method
	out.BandN = n
	out.SampleN = n
	out.FromGrid = true
	return
}

// AtGridPreview mirrors AtGrid for the DKW preview band: it reads the band
// edges at the cursor from the instant closed-form [ecdfbands.DkwBandForGrid]
// rather than the warmed exact band, so a hover readout is available before
// (or without) the exact inversion. Crosshair.Alpha echoes the renderer's
// alpha as usual.
func (inst Renderer) AtGridPreview(plotID c.AbsoluteWidgetId, xs, fnAt []float64, n int) (out Crosshair) {
	out.NearestIdx = -1
	out.Alpha = inst.alpha
	if len(xs) < 1 {
		return
	}
	hover := c.CurrentApplicationState.StateManager.GetPlotPointer()
	if hover.HoverPlotId != plotID.Derive() || math.IsNaN(hover.HoverX) {
		return
	}
	g, err := ecdfbands.DkwBandForGrid(xs, fnAt, n, inst.alpha)
	if err != nil {
		return
	}
	x := hover.HoverX
	nIdx := nearestIdx(xs, x)
	out.Valid = true
	out.X = x
	out.Y = hover.HoverY
	out.FnX = fnAtXGrid(xs, fnAt, x)
	out.LowerX, out.UpperX = bandAtX(g.Xs, g.LowerCDF, g.UpperCDF, x)
	out.NearestX = xs[nIdx]
	out.NearestIdx = nIdx
	out.BandKind = BandPreview
	out.Method = ecdfbands.BandMethodDKW
	out.BandN = n
	out.SampleN = n
	out.FromGrid = true
	return
}

// PaintCrosshair emits a vertical PlotVLine at ch.X using the
// renderer's ECDF stroke colour at half alpha. No-op when ch.Valid
// is false. Must be invoked inside the same c.Plot block as Render —
// the egui_plot drain renders vlines after polygons and lines, so
// the crosshair sits visually on top of the band and curve.
func (inst Renderer) PaintCrosshair(ch Crosshair) {
	if !ch.Valid {
		return
	}
	c.PlotVLine(inst.seriesName+" cursor", ch.X).
		Color(color.Hex(withAlpha(inst.ecdfStrokePacked, 0x80))).
		Width(1.0).
		Send()
}

// ReadoutLineCount is the fixed number of text rows WriteStatusLine
// emits, so the status area keeps a constant height whether or not the
// cursor is over the curve. A height that jumped on hover would shift the
// layout and, in the distsummary host, re-enter the plot-width grow
// guard. Hosts budget vertical space for the readout from this.
const ReadoutLineCount = 5

// WriteStatusLine emits the verbose, plain-language cursor readout
// immediately below the c.Plot as a fixed-height stack of weak-styled
// rows: the description when ch.Valid, a one-line hover hint otherwise,
// padded with blank rows to [ReadoutLineCount] so hovering on / off never
// reflows the host. The text is produced by the pure [formatReadout] so
// the wording is unit-testable without egui.
func WriteStatusLine(ch Crosshair) {
	lines := formatReadout(ch)
	for _, ln := range lines {
		c.LabelAtoms(c.Atoms().BeginRichText(ln).Small().Weak().End().Keep()).Send()
	}
	// Pad to a constant height (NBSP keeps an empty row at line height).
	for i := len(lines); i < ReadoutLineCount; i++ {
		c.LabelAtoms(c.Atoms().BeginRichText(" ").Small().Weak().End().Keep()).Send()
	}
}

// formatReadout renders ch into the verbose readout's text rows (at most
// [ReadoutLineCount]), describing the cursor's empirical-CDF reading and
// its simultaneous confidence interval in plain language. Pure — no egui
// calls — so the wording is unit-testable. Returns a single hover-hint
// row when ch is not Valid.
//
// The "nearest" clause distinguishes an observed order statistic
// (raw-sample path) from a grid evaluation point (streaming path, where
// it is omitted) rather than mislabelling the latter as X_(i). The band
// line is delegated to [formatBandLines].
func formatReadout(ch Crosshair) (lines []string) {
	if !ch.Valid {
		return []string{"Hover over the curve to read F(x) and its confidence interval."}
	}
	lines = make([]string, 0, ReadoutLineCount)
	lines = append(lines, fmt.Sprintf("Cursor at value x = %.4g.", ch.X))
	lines = append(lines, fmt.Sprintf(
		"Empirical CDF F_n(x) = %.3f — an estimated %.1f%% of %d observations are ≤ %.4g.",
		ch.FnX, ch.FnX*100, ch.SampleN, ch.X))
	if !ch.FromGrid {
		// Raw-sample path: NearestX is a genuine order statistic.
		lines = append(lines, fmt.Sprintf(
			"Nearest observation X_(%d) = %.4g.", ch.NearestIdx+1, ch.NearestX))
	}
	lines = append(lines, formatBandLines(ch)...)
	if len(lines) > ReadoutLineCount {
		lines = lines[:ReadoutLineCount]
	}
	return
}

// formatBandLines renders the two-row confidence-band description for a
// valid crosshair, naming the band's provenance honestly: the exact
// family + the n it was calibrated at, or the conservative DKW preview.
// When the calibration n lags the true sample size (a capped or bucketed
// solve, BandN < SampleN) it flags the band as conservative — the
// staleness made visible. Coverage is rounded for display (%.6g) so the
// label reads "95%", not the float-error "94.99999999999999%".
func formatBandLines(ch Crosshair) (lines []string) {
	if ch.BandKind == BandNone {
		return []string{"No confidence band available at this point."}
	}
	cov := (1 - ch.Alpha) * 100
	var head string
	switch ch.BandKind {
	case BandPreview:
		head = fmt.Sprintf(
			"Simultaneous %.6g%% confidence band (conservative DKW preview, n=%d):",
			cov, ch.BandN)
	default: // BandExact
		if ch.BandN < ch.SampleN {
			head = fmt.Sprintf(
				"Simultaneous %.6g%% band (exact, %s, n=%d; sample %d, conservative):",
				cov, ch.Method.String(), ch.BandN, ch.SampleN)
		} else {
			head = fmt.Sprintf(
				"Simultaneous %.6g%% confidence band (exact, %s, n=%d):",
				cov, ch.Method.String(), ch.BandN)
		}
	}
	lines = append(lines, head)
	lines = append(lines, fmt.Sprintf(
		"true CDF F(x) ∈ [%.3f, %.3f] with %.6g%% joint coverage.",
		ch.LowerX, ch.UpperX, cov))
	return
}
