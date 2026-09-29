// Package ecdf is a painter helper (ADR-0267) for plotting an empirical CDF
// together with a finite-sample exact simultaneous confidence band
// (Berk-Jones by default; DKW / equal-precision / higher-criticism
// available per the underlying ecdfbands library).
//
// The helper takes no widget ids and holds no state: [Paint] declares the
// shaded band and the ECDF step curve into the host's open *implot.Plot
// (between Begin and End — through the implot port per ADR-0149 SD7) from
// an [Input] whose [Style] fields are zero for the defaults [DefaultStyle]
// resolves. The exact band's O(n²) critical value is warmed off the render
// goroutine by [EnsureBandJob], under a key the host derives from its own
// ids, and cancelled by [CancelBandJob].
package ecdf

import (
	"fmt"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// Style is the appearance and calibration of the band and curve. A zero
// field takes the value [DefaultStyle] resolves for it; [Style.Resolved]
// returns the style with every default filled in.
type Style struct {
	// Method is the confidence-band family; zero → BandMethodBerkJones.
	Method ecdfbands.BandMethodE
	// Alpha is the complement of coverage: the band realises (1-α)·100%
	// simultaneous coverage. Zero → 0.05.
	Alpha float64
	// BandFill is the band polygon's fill; zero → AccentDefault at 0x40 alpha.
	BandFill color.Color
	// BandStroke and BandStrokeWidth outline the band; the default width 0
	// draws no outline, so the band reads as a fill alone.
	BandStroke      color.Color
	BandStrokeWidth float32
	// EcdfStroke and EcdfStrokeWidth draw the ECDF step polyline; zero →
	// NeutralTextPrimary at 1.5 px.
	EcdfStroke      color.Color
	EcdfStrokeWidth float32
	// SeriesName is the legend label of the curve; "" → "ECDF". The band's
	// label is "<SeriesName> band".
	SeriesName string
}

// Input is what one Paint or At draws or reads: the Style, the sample as
// either a sorted raw sample or an (Xs, FnAt, N) grid, and which band.
type Input struct {
	Style
	// Sorted is a non-decreasing iid sample. When set it is the data and
	// the grid fields are ignored; fewer than two values paint nothing.
	Sorted []float64
	// Xs and FnAt are an explicit ECDF grid — monotone non-decreasing Xs
	// with FnAt ∈ [0, 1] — and N the total sample size the estimator was
	// built on (typically far larger than len(Xs)); the band's calibration
	// depends on N, not on the grid resolution. For a t-digest or another
	// sketch, and for a grid intentionally coarser than the data.
	Xs   []float64
	FnAt []float64
	N    int
	// Band selects what is drawn beside the curve: BandNone draws the curve
	// alone (the band-free counterpart while a warm-up runs), BandExact the
	// configured family at N (blocking on the O(n²) inversion unless it is
	// cached — see BandReady), BandPreview the instant closed-form DKW band,
	// wider than the exact one and never blocking. The raw-sample path
	// treats BandPreview as BandExact.
	Band BandKindE
}

// Result is what one Paint reports.
type Result struct {
	// Err is the band library's rejection of the input (an unsorted sample,
	// a non-monotone grid). The curve is still drawn where it can be.
	Err error
}

// DefaultStyle is the style a zero [Style] resolves to.
func DefaultStyle() (st Style) {
	st = Style{
		Method:          ecdfbands.BandMethodBerkJones,
		Alpha:           0.05,
		BandFill:        color.Hex(packRGBA(styletokens.AccentDefault, 0x40)),
		BandStroke:      color.Hex(styletokens.AccentDefault.AsHex()),
		BandStrokeWidth: 0,
		EcdfStroke:      color.Hex(styletokens.NeutralTextPrimary.AsHex()),
		EcdfStrokeWidth: 1.5,
		SeriesName:      "ECDF",
	}
	return
}

// Resolved returns st with every zero field replaced by its default.
func (st Style) Resolved() (out Style) {
	def := DefaultStyle()
	out = st
	if out.Method == 0 {
		out.Method = def.Method
	}
	if out.Alpha == 0 {
		out.Alpha = def.Alpha
	}
	if out.BandFill == (color.Color{}) {
		out.BandFill = def.BandFill
	}
	if out.BandStroke == (color.Color{}) {
		out.BandStroke = def.BandStroke
	}
	if out.EcdfStroke == (color.Color{}) {
		out.EcdfStroke = def.EcdfStroke
	}
	if out.EcdfStrokeWidth == 0 {
		out.EcdfStrokeWidth = def.EcdfStrokeWidth
	}
	if out.SeriesName == "" {
		out.SeriesName = def.SeriesName
	}
	return
}

// bandSeriesName is the band's legend label.
func (st Style) bandSeriesName() string { return st.SeriesName + " band" }

// Paint declares the ECDF and, per in.Band, its confidence band into the
// host's open plot. The band goes first (it sits under the curve), then the
// step polyline. A raw sample of fewer than two values, or a grid of fewer
// than two points, paints nothing.
func Paint(p *implot.Plot, in Input) (res Result) {
	st := in.Style.Resolved()
	if in.Sorted != nil {
		return st.paintSample(p, in.Sorted, in.Band)
	}
	return st.paintGrid(p, in.Xs, in.FnAt, in.N, in.Band)
}

func (st Style) paintSample(p *implot.Plot, sorted []float64, band BandKindE) (res Result) {
	if len(sorted) < 2 {
		return
	}
	if band != BandNone {
		b, err := ecdfbands.BandsForSample(sorted, st.Alpha, st.Method)
		if err != nil {
			res.Err = eh.Errorf("ecdf band: %w", err)
		} else {
			st.emitBand(p, b.Xs, b.LowerCDF, b.UpperCDF)
		}
	}
	st.emitEcdfPolyline(p, sorted)
	return
}

func (st Style) paintGrid(p *implot.Plot, xs, fnAt []float64, n int, band BandKindE) (res Result) {
	if len(xs) < 2 {
		return
	}
	switch band {
	case BandExact:
		g, err := ecdfbands.BandsForGrid(xs, fnAt, n, st.Alpha, st.Method)
		if err != nil {
			res.Err = eh.Errorf("ecdf grid band: %w", err)
		} else {
			st.emitBand(p, g.Xs, g.LowerCDF, g.UpperCDF)
		}
	case BandPreview:
		g, err := ecdfbands.DkwBandForGrid(xs, fnAt, n, st.Alpha)
		if err != nil {
			res.Err = eh.Errorf("ecdf preview band: %w", err)
		} else {
			st.emitBand(p, g.Xs, g.LowerCDF, g.UpperCDF)
		}
	}
	st.emitGridEcdfPolyline(p, xs, fnAt)
	return
}

// BandReady reports whether the style's (n, α, method) exact band is
// already cached — whether a BandExact Paint or At will draw without
// blocking on the O(n²) inversion. Non-blocking; pair it with
// [EnsureBandJob] to drive the schedule-and-show-progress path.
func BandReady(st Style, n int) bool {
	st = st.Resolved()
	return ecdfbands.BandReady(n, st.Alpha, st.Method)
}

// BandJobKey identifies one consumer's band warm-up. A host derives it from
// its own ids inside its scope — `BandJobKey(ids.ProbeSeq("band-job"))` —
// so two instances never share or cancel each other's solve.
type BandJobKey uint64

// EnsureBandJob schedules (once, idempotently) a background warm-up of the
// style's (n, α, method) band under key and returns the current progress
// snapshot. tasks may be nil (the solve still runs; only keelson task
// integration is skipped). Call it on frames where BandReady is false,
// paint the curve with BandPreview or BandNone meanwhile, and show the
// snapshot through a progress widget. Pair it with [CancelBandJob] when the
// consumer closes so a long solve does not outlive the window that asked.
func EnsureBandJob(key BandJobKey, tasks task.TaskApiI, n int, st Style) BandJobSnapshot {
	st = st.Resolved()
	return ensureBandWarm(key, tasks, n, st.Alpha, st.Method)
}

// CancelBandJob aborts the warm-up scheduled under key by EnsureBandJob, if
// one is in flight, and forgets it. Idempotent — a no-op when nothing is
// registered — so it is safe to call every frame a consumer is closed. A
// band that already finished stays in the shared ecdfbands cache, so a
// reopen still renders instantly.
func CancelBandJob(key BandJobKey) {
	cancelBandJob(key)
}

// emitBand declares the confidence band as one step-expanded
// ShadedBetween: plateau i covers [xs[i], xs[i+1]] at constant
// (lower[i], upper[i]), so duplicating each interior x makes the
// per-segment quads exactly the per-plateau rectangles the bridge
// version emitted as individual polygons (a plain two-curve fill
// would slant across the plateaus). An optional staircase outline
// follows when a band stroke is configured.
func (st Style) emitBand(p *implot.Plot, xs, lower, upper []float64) {
	n := len(xs)
	if n < 2 {
		return
	}
	ex := make([]float64, 0, 2*(n-1))
	el := make([]float64, 0, 2*(n-1))
	eu := make([]float64, 0, 2*(n-1))
	for i := 0; i < n-1; i++ {
		ex = append(ex, xs[i], xs[i+1])
		el = append(el, lower[i], lower[i])
		eu = append(eu, upper[i], upper[i])
	}
	p.SetNextColor(st.BandFill.Literal())
	p.ShadedBetween(st.bandSeriesName(), ex, el, eu)
	if st.BandStrokeWidth > 0 {
		p.SetNextColor(st.BandStroke.Literal()).SetNextWeight(st.BandStrokeWidth)
		p.Stairs(st.bandSeriesName(), xs, upper)
		p.SetNextColor(st.BandStroke.Literal()).SetNextWeight(st.BandStrokeWidth)
		p.Stairs(st.bandSeriesName(), xs, lower)
	}
}

// emitEcdfPolyline walks the ECDF step function for a complete
// sorted sample and declares one line series. The polyline starts at
// (sorted[0], 0) and ascends in 1/n steps up to (sorted[n-1], 1).
func (st Style) emitEcdfPolyline(p *implot.Plot, sorted []float64) {
	xs, ys := buildEcdfPolyline(sorted)
	p.SetNextColor(st.EcdfStroke.Literal()).SetNextWeight(st.EcdfStrokeWidth)
	p.Line(st.SeriesName, xs, ys)
}

// emitGridEcdfPolyline declares the ECDF curve at an explicit grid
// where F_n is already known. The curve is rendered as a piecewise-
// linear polyline through (xs[i], fnAt[i]) — appropriate for grids
// dense enough that the underlying step structure is below visual
// resolution. Coarse grids will show as linear segments between
// known points; that is the right visual for sketch-backed ECDFs.
func (st Style) emitGridEcdfPolyline(p *implot.Plot, xs, fnAt []float64) {
	p.SetNextColor(st.EcdfStroke.Literal()).SetNextWeight(st.EcdfStrokeWidth)
	p.Line(st.SeriesName, xs, fnAt)
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
// Alpha echoes Style.Alpha so WriteStatusLine can derive the
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

// At returns the crosshair for in at the cursor position over the given
// plot (implot's per-plot hover state — one frame behind, like every
// register read; a pointer over a different plot never surfaces here).
// The band edges come from the band in.Band names, so a hover readout is
// available before (or without) the exact inversion when in.Band is
// BandPreview; BandNone reads no edges.
//
// Crosshair.Valid is false when the plot is not hovered or the input is
// empty. Cheap to call: the bands are cached by (n, α, method); the
// per-call cost is two O(log n) binary searches plus a slice copy out of
// the band cache.
func At(p *implot.Plot, in Input) (out Crosshair) {
	st := in.Style.Resolved()
	out.NearestIdx = -1
	out.Alpha = st.Alpha
	xsData, fromGrid := in.Sorted, false
	if in.Sorted == nil {
		xsData, fromGrid = in.Xs, true
	}
	if len(xsData) < 1 {
		return
	}
	x, y, ok := p.HoverPlotPos()
	if !ok {
		return
	}
	nIdx := nearestIdx(xsData, x)
	out.Valid = true
	out.X = x
	out.Y = y
	out.NearestX = xsData[nIdx]
	out.NearestIdx = nIdx
	out.FromGrid = fromGrid
	if fromGrid {
		out.FnX = fnAtXGrid(in.Xs, in.FnAt, x)
		out.BandN, out.SampleN = in.N, in.N
	} else {
		out.FnX = fnAtXSorted(in.Sorted, x)
		out.BandN, out.SampleN = len(in.Sorted), len(in.Sorted)
	}
	switch {
	case in.Band == BandNone:
		out.BandKind = BandNone
	case !fromGrid:
		band, err := ecdfbands.BandsForSample(in.Sorted, st.Alpha, st.Method)
		if err != nil {
			out.BandKind = BandNone
			return
		}
		out.LowerX, out.UpperX = bandAtX(band.Xs, band.LowerCDF, band.UpperCDF, x)
		out.BandKind = BandExact
		out.Method = st.Method
	case in.Band == BandPreview:
		g, err := ecdfbands.DkwBandForGrid(in.Xs, in.FnAt, in.N, st.Alpha)
		if err != nil {
			out.BandKind = BandNone
			return
		}
		out.LowerX, out.UpperX = bandAtX(g.Xs, g.LowerCDF, g.UpperCDF, x)
		out.BandKind = BandPreview
		out.Method = ecdfbands.BandMethodDKW
	default:
		g, err := ecdfbands.BandsForGrid(in.Xs, in.FnAt, in.N, st.Alpha, st.Method)
		if err != nil {
			out.BandKind = BandNone
			return
		}
		out.LowerX, out.UpperX = bandAtX(g.Xs, g.LowerCDF, g.UpperCDF, x)
		out.BandKind = BandExact
		out.Method = st.Method
	}
	return
}

// PaintCrosshair declares a vertical reference line at ch.X using the
// style's ECDF stroke colour at half alpha. No-op when ch.Valid is false.
// Declare it after Paint inside the same plot so the line draws on top of
// the band and curve.
func PaintCrosshair(p *implot.Plot, st Style, ch Crosshair) {
	if !ch.Valid {
		return
	}
	st = st.Resolved()
	p.SetNextColor(withAlpha(st.EcdfStroke.Literal(), 0x80)).SetNextWeight(1.0)
	p.InfLinesV(st.SeriesName+" cursor", []float64{ch.X})
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

// WriteStatusLineTerse emits the compact one-line counterpart to
// [WriteStatusLine]: a single weak row in standard ECDF notation, for
// inline placements (a status bar, a dense table row) where the
// multi-line readout is too tall. No-op when ch.Valid is false — terse
// callers that want a placeholder emit their own. The text is built by
// the pure [formatStatusLineTerse].
func WriteStatusLineTerse(ch Crosshair) {
	s := formatStatusLineTerse(ch)
	if s == "" {
		return
	}
	c.LabelAtoms(c.Atoms().BeginRichText(s).Small().Weak().End().Keep()).Send()
}

// formatStatusLineTerse renders ch as one compact line —
// `x = … │ F_n(x) = … │ <cov>% band […, …] (<provenance>) │ nearest X_(i) = …`
// — or "" when ch is not Valid. Pure; unit-tested. The band provenance is
// abbreviated (exact <family> / DKW preview, calibration n, a "cons."
// flag when it lags the sample); the nearest-order-statistic clause is
// dropped on the grid path, where it would be a grid point, not an X_(i).
func formatStatusLineTerse(ch Crosshair) string {
	if !ch.Valid {
		return ""
	}
	band := formatBandTerse(ch)
	if ch.FromGrid {
		return fmt.Sprintf("x = %.4g │ F_n(x) = %.3f │ %s", ch.X, ch.FnX, band)
	}
	return fmt.Sprintf("x = %.4g │ F_n(x) = %.3f │ %s │ nearest X_(%d) = %.4g",
		ch.X, ch.FnX, band, ch.NearestIdx+1, ch.NearestX)
}

// formatBandTerse renders the compact `(<cov>% band […, …] (<provenance>))`
// segment of the terse line: the conservative DKW preview, or the exact
// family + calibration n with a "cons." flag when that n lags the sample.
func formatBandTerse(ch Crosshair) string {
	cov := (1 - ch.Alpha) * 100
	switch ch.BandKind {
	case BandNone:
		return "no band"
	case BandPreview:
		return fmt.Sprintf("%.6g%% band [%.3f, %.3f] (DKW preview)", cov, ch.LowerX, ch.UpperX)
	default: // BandExact
		if ch.BandN < ch.SampleN {
			return fmt.Sprintf("%.6g%% band [%.3f, %.3f] (exact %s, n=%d cons.)",
				cov, ch.LowerX, ch.UpperX, ch.Method.String(), ch.BandN)
		}
		return fmt.Sprintf("%.6g%% band [%.3f, %.3f] (exact %s, n=%d)",
			cov, ch.LowerX, ch.UpperX, ch.Method.String(), ch.BandN)
	}
}
