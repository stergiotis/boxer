package play

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/trendsmooth"
)

// play_series_panel.go is the ADR-0163 Series dock tab (M0): the numeric
// series-over-time carrier play did not have. Timeline draws events and
// spans, Projection a scatter; nothing drew a value against a time axis.
//
// The claim is TYPED rather than named (§SD1): x is the first temporal
// column, every numeric column is a lane, anything else is ignored. This is
// the one place the named-columns doctrine (ADR-0122 §SD1) does not apply,
// because that doctrine answers same-typed ambiguity and a time axis plus
// numeric lanes has none — `SELECT t, v` is the most ordinary shape SQL
// produces, and asking it to carry ceremony would buy nothing.
//
// What the panel refuses to do is as load-bearing as what it draws. It never
// fills, interpolates or resamples: a null value breaks the line, a gap in
// the time grid breaks the line, and the fix is offered as SQL the user can
// read (§SD2) rather than applied silently. Fabricated samples are data a
// detector would then score.

const (
	// seriesPlotHeight is the fixed plot-box height; width follows the pane,
	// as in the Distribution tab. Lower than that tab's box because this one
	// carries more above it — a status line, the smoothing controls, and a
	// grid finding with its scaffold button — and the x axis is where the
	// UTC tick labels live, so pushing them below the leaf's fold is the one
	// clipping that actually costs the reader something.
	seriesPlotHeight = 340
	// seriesPlotHeightWithScores is the series box when a score plot shares
	// the leaf below it (M2). The two together must leave the x tick labels
	// of BOTH above the fold, and the series keeps the larger share: the
	// score is read for where its peaks fall, which needs less height than
	// reading a shape does.
	seriesPlotHeightWithScores = 200
	seriesPlotMinW             = 480
	// seriesMaxLanes bounds the overlay. Beyond a dozen lines a shared axis
	// stops being readable; the excess is counted in the status line rather
	// than silently dropped.
	seriesMaxLanes = 12
	// seriesJitterTol is §SD2's grid tolerance: a Δt deviating from the
	// median by more than this fraction opens a gap. An initial constant,
	// property-tested, not a measured one.
	seriesJitterTol = 0.20
	// seriesMinGridPoints is the shortest series a Δt classification says
	// anything about — two points give one interval, which has no spread.
	seriesMinGridPoints = 3
)

// seriesPaneProbeSalt namespaces the pane probe's register slot; threading it
// through the instance id stack makes it window-unique, so two playgrounds
// size their own plot (the Distribution tab's lesson — never
// CaptureAvailableSize, which is one process-wide slot).
const seriesPaneProbeSalt uint64 = 0x5e21e50f1a7c0001

// The optional channels' node ids. Like the Sankey's `flows` / `nodes` these
// are CTE NAMES, not shapes: §SD1 rejects shape-matched auto-suggestion
// because two CTEs can both be (t, score, warm_up), which is exactly the
// ambiguity naming exists to settle.
const (
	seriesScoresNodeID NodeID = "scores"
	seriesSpansNodeID  NodeID = "spans"
)

// seriesClaim is the panel's channel claim: the resolved x column, the
// numeric lanes in schema order, and the row to highlight.
type seriesClaim struct {
	tCol   int
	vCols  []int
	selRow int64
}

// seriesGridE is §SD2's Δt classification.
type seriesGridE uint8

const (
	// seriesGridRegular: every Δt sits within tolerance of the median.
	seriesGridRegular seriesGridE = iota
	// seriesGridGapped: regular, but with holes — every out-of-tolerance Δt
	// is a near-integer multiple of the median, which is what a missing
	// sample looks like. Analysis segments at the holes.
	seriesGridGapped
	// seriesGridIrregular: the spacing is not a grid at all. Charted
	// time-true; analysis refuses and points at aggregation.
	seriesGridIrregular
	// seriesGridUnordered: time runs backwards somewhere. Every Δt reading
	// downstream of it is meaningless, so this is reported before the rest.
	seriesGridUnordered
	// seriesGridUnknown: too few points to say anything.
	seriesGridUnknown
)

func (inst seriesGridE) String() (name string) {
	switch inst {
	case seriesGridRegular:
		name = "regular"
	case seriesGridGapped:
		name = "regular with gaps"
	case seriesGridIrregular:
		name = "irregular"
	case seriesGridUnordered:
		name = "not ordered by time"
	default:
		name = "too short to classify"
	}
	return
}

// seriesGrid is the validator's finding (§SD2). It is a FINDING, not an
// error: an irregular series still charts, time-true. Only the analysis tier
// refuses, and the refusal carries the SQL that would fix it.
type seriesGrid struct {
	class seriesGridE
	// medianSec is the median Δt in seconds — the step a scaffold fills in.
	medianSec float64
	// gaps counts intervals beyond tolerance; breaks holds the index of the
	// sample STARTING each such interval, so a segment runs [prev, break).
	gaps   int
	breaks []int
}

// seriesLane is one numeric column folded for drawing.
type seriesLane struct {
	label string
	vals  []float64
	// valid is per-sample: a null value must break the line rather than be
	// interpolated across (§SD2 — fill is never applied client-side).
	valid []bool
	nulls int
	// segs are the contiguous runs to draw, split at nulls AND at grid gaps.
	// Smoothing runs per segment for the same reason analysis does: a kernel
	// spanning a hole smears across time that carries no samples.
	segs []seriesSegment
}

// seriesSegment is a half-open [lo, hi) run of samples.
type seriesSegment struct {
	lo, hi int
}

// SeriesDriver owns the Series tab state: the folded series, its cache key,
// and the panel-local view state.
type SeriesDriver struct {
	ids *c.WidgetIdStack
	// deliver writes a scaffold into the editor buffer (§SD2). It is the
	// public delivery seam, injected so the driver does not reach for the
	// app: play_delivery.go's ops are what a snippet-class pane uses.
	deliver func(sql string)

	// t is the x axis in Unix SECONDS — implot's ScaleTime unit. Timestamps
	// are read forced-UTC (§SD2); no local zone ever enters.
	t      []float64
	rows   []int64 // fold index → record row, for the selection cursor
	lanes  []seriesLane
	grid   seriesGrid
	tLabel string

	foldErr     string
	skippedRows int64
	droppedLane int

	forExecuted     time.Time
	forSchema       *arrow.Schema
	pendingExecuted time.Time

	// The M2 overlay channels, folded per result: the score lane with its
	// mandated baseline, and the flagged extents. Empty when the buffer names
	// no `scores` / `spans` CTE, which is the ordinary case.
	scores       seriesScores
	spans        []seriesSpan
	spansSkipped int
	// scoreCall is the client node behind the score channel, which is where
	// the detector's name, its causality and its window live. Nil when the
	// channel is filled by an ordinary CTE.
	scoreCall     *tsCall
	scoreWindow   int32
	scoreWindowOK bool
	// xLinkMin/xLinkMax are the shared x range of the series plot and the
	// score plot below it. A score is read AGAINST its series, so the two
	// panning independently would be a picture of nothing.
	xLinkMin, xLinkMax float64

	smooth *trendsmooth.State
	// decimate is the §SD1 render-only envelope. Exposed as a toggle so the
	// claim it makes — that it cannot drop an extreme — is checkable by eye
	// against the undecimated draw, not only by its property test.
	decimate bool
	drawn    int
	sourced  int
}

func NewSeriesDriver(ids *c.WidgetIdStack, deliver func(sql string)) (inst *SeriesDriver) {
	return &SeriesDriver{ids: ids, deliver: deliver, smooth: trendsmooth.New(), decimate: true}
}

func (inst *SeriesDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// seriesPanel is the PanelI face. Acceptance is schema-only and cheap — the
// Δt classification needs the data and runs in the fold.
type seriesPanel struct {
	driver *SeriesDriver
}

func (inst seriesPanel) ID() PanelID { return "series" }

func (inst seriesPanel) Channels() []ChannelSpec {
	// The optional channels carry the M2 overlays: a score lane on its own
	// linked plot, and flagged extents as bands behind both. Both fill BY
	// CTE NAME (§SD1) — `scores` and `spans` — the way the Sankey panel
	// takes its `flows` and `nodes`.
	return []ChannelSpec{
		{ID: chMain, Required: true, Label: "series"},
		{ID: chScores, Required: false, Label: "scores"},
		{ID: chSpans, Required: false, Label: "spans"},
	}
}

func (inst seriesPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query returning a time column and at least one number to draw a series."
		return
	}
	switch ch {
	case chScores:
		return acceptSeriesScores(schema)
	case chSpans:
		return acceptSeriesSpans(schema)
	}
	k, reason := resolveSeriesColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst seriesPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(seriesClaim)
	if !ok {
		return
	}
	scores, _ := filled[chScores]
	spans, _ := filled[chSpans]
	inst.driver.render(main.Rec, main.Rec.Schema(), k, scores.Rec, spans.Rec, emit)
}

// resolveSeriesColumns applies §SD1's typed claim to a schema: x is the FIRST
// temporal column, the lanes are every numeric column. Pure and schema-only.
func resolveSeriesColumns(schema *arrow.Schema) (k seriesClaim, reason string) {
	k = seriesClaim{tCol: -1, selRow: -1}
	for ci, f := range schema.Fields() {
		switch {
		case k.tCol < 0 && isSeriesTemporalType(f.Type):
			k.tCol = ci
		case isNumericType(f.Type):
			k.vCols = append(k.vCols, ci)
		}
	}
	if k.tCol < 0 {
		reason = "A series needs a time column — a DateTime64 or a Date. The first one " +
			"in the result becomes the x axis. Note that a plain DateTime arrives as " +
			"a bare UInt32 (epoch seconds) and cannot be told from a count, so wrap it: " +
			"`toDateTime64(toStartOfMinute(t), 3)`. " + seriesShapeHint(schema)
		return
	}
	if len(k.vCols) == 0 {
		reason = "A series needs at least one numeric column to draw against " +
			"`" + schema.Field(k.tCol).Name + "`. Every number in the result becomes a lane; " +
			seriesShapeHint(schema)
		return
	}
	return
}

// seriesShapeHint names what the result actually carries, so the reject says
// what is wrong with THIS result rather than restating the contract.
func seriesShapeHint(schema *arrow.Schema) (hint string) {
	names := make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		names = append(names, "`"+f.Name+"` "+f.Type.String())
	}
	if len(names) == 0 {
		return "this result has no columns."
	}
	return "this result has " + strings.Join(names, ", ") + "."
}

// acceptSeriesScores accepts the optional score channel: a temporal column
// plus a `score` column (ADR-0163 §SD3's tsAnomalyScores contract).
func acceptSeriesScores(schema *arrow.Schema) (claim ChannelClaim, reason string) {
	k := seriesClaim{tCol: -1, selRow: -1}
	for ci, f := range schema.Fields() {
		if k.tCol < 0 && isSeriesTemporalType(f.Type) {
			k.tCol = ci
			continue
		}
		if f.Name == "score" && isNumericType(f.Type) {
			k.vCols = append(k.vCols, ci)
		}
	}
	if k.tCol < 0 || len(k.vCols) == 0 {
		reason = "A `scores` CTE needs a time column and a numeric `score` column — " +
			"what tsAnomalyScores(t, v, window) emits."
		return
	}
	claim = k
	return
}

// acceptSeriesSpans accepts the optional span channel on the Timeline's band
// contract, which tsAnomalySpans emits directly (§SD3). Only the required
// three are checked here; the colour token vocabulary is the band reader's.
func acceptSeriesSpans(schema *arrow.Schema) (claim ChannelClaim, reason string) {
	var have int
	for _, f := range schema.Fields() {
		switch f.Name {
		case timelineSlotBandFrom, timelineSlotBandTo, timelineSlotBandColor:
			have++
		}
	}
	if have < 3 {
		reason = fmt.Sprintf("A `spans` CTE needs %q, %q and %q — the Timeline band contract, "+
			"which tsAnomalySpans(t, v, window, k) emits.",
			timelineSlotBandFrom, timelineSlotBandTo, timelineSlotBandColor)
		return
	}
	claim = struct{}{}
	return
}

// isSeriesTemporalType is the schema-only half of temporalCellMS: the types a
// time axis can be read from without looking at a value.
//
// A ClickHouse `DateTime` is deliberately NOT among them, and the omission is
// forced rather than chosen: it reaches Arrow as a bare uint32 of epoch
// seconds — under both the native and the non-native writer, with no field
// metadata naming the source type — so nothing distinguishes it from a count.
// Accepting uint32 would make `SELECT id, count()` draw an id as a time axis,
// which is the same-typed ambiguity §SD1 asserts a typed claim does not have.
// `DateTime64` and `Date` carry their types through, so the fix a reject names
// is a cast, not a rename (ADR-0163 Update 2026-08-05).
func isSeriesTemporalType(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.TIMESTAMP, arrow.DATE32, arrow.DATE64:
		ok = true
	case arrow.DICTIONARY:
		if d, is := dt.(*arrow.DictionaryType); is {
			ok = isSeriesTemporalType(d.ValueType)
		}
	}
	return
}

// classifySeriesGrid computes §SD2's Δt distribution and sorts the series into
// one of the grid classes. Pure over the time axis, which is what makes it
// property-testable independently of Arrow and of rendering.
func classifySeriesGrid(t []float64) (g seriesGrid) {
	g.class = seriesGridUnknown
	if len(t) < seriesMinGridPoints {
		return
	}
	diffs := make([]float64, 0, len(t)-1)
	for i := 1; i < len(t); i++ {
		d := t[i] - t[i-1]
		if d < 0 {
			g.class = seriesGridUnordered
			return
		}
		diffs = append(diffs, d)
	}
	sorted := make([]float64, len(diffs))
	copy(sorted, diffs)
	sort.Float64s(sorted)
	g.medianSec = sorted[len(sorted)/2]
	if !(g.medianSec > 0) {
		// Every interval is zero: repeated timestamps, not a grid. Charting
		// still works (points stack); analysis has nothing to step by.
		g.class = seriesGridIrregular
		return
	}
	tol := g.medianSec * seriesJitterTol
	multiples := true
	for i, d := range diffs {
		if math.Abs(d-g.medianSec) <= tol {
			continue
		}
		g.gaps++
		g.breaks = append(g.breaks, i+1)
		// A hole is a whole number of missing samples: Δt lands on k·median
		// for some k ≥ 2. Anything else is spacing that was never a grid.
		k := math.Round(d / g.medianSec)
		if k < 2 || math.Abs(d-k*g.medianSec) > tol*k {
			multiples = false
		}
	}
	switch {
	case g.gaps == 0:
		g.class = seriesGridRegular
	case multiples:
		g.class = seriesGridGapped
	default:
		g.class = seriesGridIrregular
	}
	return
}

// buildSeriesSegments splits a lane into the runs that may be drawn — and
// smoothed — as one curve: contiguous in the value (no null) and in the grid
// (no gap). breaks are the shared grid breaks; both bounds are fold indices.
func buildSeriesSegments(valid []bool, breaks []int) (segs []seriesSegment) {
	isBreak := make(map[int]bool, len(breaks))
	for _, b := range breaks {
		isBreak[b] = true
	}
	lo := -1
	flush := func(hi int) {
		if lo >= 0 && hi-lo > 0 {
			segs = append(segs, seriesSegment{lo: lo, hi: hi})
		}
		lo = -1
	}
	for i := range valid {
		if !valid[i] {
			flush(i)
			continue
		}
		if isBreak[i] {
			flush(i)
		}
		if lo < 0 {
			lo = i
		}
	}
	flush(len(valid))
	return
}

// envelopeDecimate reduces a run to at most two samples per pixel column: the
// minimum and the maximum inside the column, emitted in time order so the
// polyline keeps its shape (§SD1 / Q9).
//
// This is the reason the automatic decimator is NOT LTTB. A selection-based
// decimator picks representative samples and can therefore drop a narrow
// extreme — the one sample a data-quality reader is looking for. An envelope
// keeps both extremes of every column BY CONSTRUCTION: whatever the source
// reaches inside a pixel, the drawing reaches too.
//
// It is render-only. Hover, selection and every analysis read the full series.
// widthPx is the axis's pixel width; xMin/xMax the visible range. Samples
// outside it collapse into one bucket per side, so the line still enters and
// leaves the viewport at the right slope.
func envelopeDecimate(t []float64, v []float64, xMin float64, xMax float64, widthPx int) (outT []float64, outV []float64) {
	span := xMax - xMin
	if widthPx < 1 || !(span > 0) || len(t) <= 2*widthPx {
		return t, v
	}
	outT = make([]float64, 0, 2*widthPx+4)
	outV = make([]float64, 0, 2*widthPx+4)
	bucketOf := func(x float64) (b int) {
		switch {
		case x < xMin:
			return -1
		case x >= xMax:
			return widthPx
		}
		b = int((x - xMin) / span * float64(widthPx))
		if b >= widthPx {
			b = widthPx - 1
		}
		return
	}
	// Per bucket: the extremes and the times they occur at. Emitting them in
	// time order (rather than always min-then-max) is what keeps a rising run
	// rising instead of sawtoothing at pixel scale.
	var (
		cur           = bucketOf(t[0])
		lo, hi        = v[0], v[0]
		tLo, tHi      = t[0], t[0]
		tFirst, tLast = t[0], t[0]
		vFirst, vLast = v[0], v[0]
		flush         func()
	)
	flush = func() {
		type pt struct{ x, y float64 }
		pts := []pt{{tFirst, vFirst}, {tLo, lo}, {tHi, hi}, {tLast, vLast}}
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].x < pts[j].x })
		for i, p := range pts {
			// The four candidates collapse to 1–4 distinct samples; drop the
			// repeats so a flat bucket costs one point, not four.
			if i > 0 && p.x == pts[i-1].x && p.y == pts[i-1].y {
				continue
			}
			outT = append(outT, p.x)
			outV = append(outV, p.y)
		}
	}
	for i := range t {
		b := bucketOf(t[i])
		if b != cur {
			flush()
			cur = b
			lo, hi, tLo, tHi = v[i], v[i], t[i], t[i]
			tFirst, vFirst = t[i], v[i]
		}
		if v[i] < lo {
			lo, tLo = v[i], t[i]
		}
		if v[i] > hi {
			hi, tHi = v[i], t[i]
		}
		tLast, vLast = t[i], v[i]
	}
	flush()
	return
}

// render folds the result (cached), draws the status line, the grid finding
// and its scaffold, then the plot.
func (inst *SeriesDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k seriesClaim,
	scoreRec arrow.RecordBatch, spanRec arrow.RecordBatch, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	inst.rebuildOverlays(scoreRec, spanRec)
	if inst.foldErr != "" {
		for rt := range c.RichTextLabel(inst.foldErr) {
			rt.Small().Weak()
		}
		return
	}
	if len(inst.t) == 0 {
		for rt := range c.RichTextLabel("The query returned no timed rows, so there is no series to draw.") {
			rt.Small().Weak()
		}
		return
	}
	inst.smooth.BeginFrame()

	dens := styletokens.DensityFromEnv()
	c.Label(inst.statusLine()).Send()
	c.AddSpace(styletokens.GapInline(dens))
	inst.renderControls()
	inst.renderGridFinding()
	inst.renderSeriesOverlayChrome()
	c.AddSpace(styletokens.GapItems(dens))

	w := float32(seriesPlotMinW)
	if availW, _, _ := c.CapturePaneSize(inst.ids.PrepareHighEntropy(seriesPaneProbeSalt).Derive()); availW > seriesPlotMinW {
		w = availW - 8
	}
	inst.renderPlot(w, k, emit)
	if len(inst.scores.t) > 0 {
		inst.renderSeriesScorePlot(w)
	}
}

// rebuildOverlays folds the optional channels. Cheap enough to run per frame
// — a score lane is one pass over the rows and the baseline is a moving
// average — so it takes no cache of its own; the expensive fold is the series
// itself, which rebuild memoises.
func (inst *SeriesDriver) rebuildOverlays(scoreRec arrow.RecordBatch, spanRec arrow.RecordBatch) {
	inst.scores = seriesScores{}
	inst.spans, inst.spansSkipped = nil, 0
	if spanRec != nil {
		inst.spans, inst.spansSkipped = foldSeriesSpans(spanRec)
	}
	if scoreRec == nil {
		return
	}
	sc, ok := foldSeriesScores(scoreRec)
	if !ok {
		return
	}
	if inst.scoreCall != nil {
		sc.detector = inst.scoreCall.Spec.Name
		sc.causal = inst.scoreCall.Spec.Causal
		sc.window = inst.scoreWindow
	}
	sc.baseline, sc.baselineWhy = inst.buildSeriesBaseline()
	inst.scores = sc
}

// renderControls is the smoothing segment plus the envelope toggle.
func (inst *SeriesDriver) renderControls() {
	for range c.HorizontalTop().KeepIter() {
		inst.smooth.RenderControls(inst.ids)
		c.Checkbox(inst.ids.PrepareStr("series-decimate"), inst.decimate, "envelope").
			SendRespVal(&inst.decimate)
	}
}

// renderGridFinding paints §SD2's classification when it is worth acting on,
// with the scaffold that would fix it. The chart is drawn either way — this
// is a data-quality finding, not an error path.
func (inst *SeriesDriver) renderGridFinding() {
	var hint, action, sql string
	switch inst.grid.class {
	case seriesGridUnordered:
		hint = "Time runs backwards in this result, so every interval below is meaningless. " +
			"A series has to be ordered before it can be read as one."
		action = "add ORDER BY"
		sql = inst.orderScaffold()
	case seriesGridGapped:
		hint = fmt.Sprintf("The grid is regular at %s but has %s gap(s). "+
			"Lines break at the holes and analysis runs per segment — nothing is filled in. "+
			"To make the holes explicit rows instead:",
			formatSeriesStep(inst.grid.medianSec), humanize.Comma(int64(inst.grid.gaps)))
		action = "add WITH FILL"
		sql = inst.fillScaffold()
	case seriesGridIrregular:
		hint = "The spacing is not a grid, so this charts time-true but analysis has no step to work in. " +
			"Aggregate to a grid in SQL — never client-side, which would invent samples a detector then scores:"
		action = "add GROUP BY"
		sql = inst.gridScaffold()
	default:
		return
	}
	for rt := range c.RichTextLabel(hint) {
		rt.Small().Weak()
	}
	if inst.deliver == nil || sql == "" {
		return
	}
	if c.Button(inst.ids.PrepareStr("series-scaffold"), c.Atoms().Text(action).Keep()).
		SendResp().HasPrimaryClicked() {
		inst.deliver(sql)
	}
}

// renderPlot draws the lanes over a UTC time axis, decimating per pixel.
func (inst *SeriesDriver) renderPlot(w float32, k seriesClaim, emit SignalEmitterI) {
	inst.drawn, inst.sourced = 0, 0
	h := float32(seriesPlotHeight)
	if len(inst.scores.t) > 0 {
		h = seriesPlotHeightWithScores
	}
	for p := range implot.Scoped(inst.ids, "##play-series", w, h) {
		p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
		if len(inst.scores.t) > 0 {
			// Linked only when a score plot is below to link WITH: an
			// unlinked single plot must keep its own autofit.
			p.SetupAxisLinks(implot.AxisX1, &inst.xLinkMin, &inst.xLinkMax)
		}
		p.SetupAxes("", inst.yLabel(), implot.AxisFlagsNone, implot.AxisFlagsNone)
		// Bands first: they are the background the series is read against,
		// and implot draws in declaration order.
		inst.renderSeriesSpans(p)

		xMin, xMax, haveRange := p.AxisRangePrev(implot.AxisX1)
		areaW := int(w)
		if _, _, pw, _, ok := p.PlotAreaPrev(); ok && pw > 1 {
			areaW = int(pw)
		}
		for li := range inst.lanes {
			lane := &inst.lanes[li]
			cl := color.Hex(styletokens.QualitativeCycle(li).AsHex())
			for _, s := range lane.segs {
				ts, vs := inst.t[s.lo:s.hi], lane.vals[s.lo:s.hi]
				inst.sourced += len(ts)
				if inst.decimate && haveRange {
					ts, vs = envelopeDecimate(ts, vs, xMin, xMax, areaW)
				}
				inst.drawn += len(ts)
				// Only the run that ends the series carries a live edge: it
				// is the only place the smoother's boundary extrapolation
				// meets data that has not arrived yet.
				if s.hi == len(inst.t) {
					inst.smooth.LineWithEdge(p, lane.label, ts, vs, cl, 1.6)
					continue
				}
				inst.smooth.Line(p, lane.label, ts, vs, cl, 1.6)
			}
		}
		if x, _, ok := p.Clicked(); ok {
			inst.publishNearest(x, k, emit)
		}
	}
}

// publishNearest maps a click in plot space back to a RESULT ROW and writes
// the ordinary selection cursor — the panel's rows are result rows, so Detail
// and the rest of the dock follow (§SD1). The lookup reads the full series,
// never the decimated draw (Q9).
func (inst *SeriesDriver) publishNearest(x float64, k seriesClaim, emit SignalEmitterI) {
	if len(inst.t) == 0 {
		return
	}
	i := sort.SearchFloat64s(inst.t, x)
	switch {
	case i >= len(inst.t):
		i = len(inst.t) - 1
	case i > 0 && x-inst.t[i-1] < inst.t[i]-x:
		i--
	}
	row := inst.rows[i]
	if row != k.selRow {
		emit.Emit(signalSelection, row)
	}
}

// rebuild folds the result into the time axis and its lanes, keyed on
// (executed, schema) like every other panel's fold.
func (inst *SeriesDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k seriesClaim) {
	if inst.t != nil && schema == inst.forSchema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.forSchema = schema
	inst.forExecuted = inst.pendingExecuted
	inst.foldErr = ""
	inst.skippedRows = 0
	inst.droppedLane = 0
	inst.tLabel = schema.Field(k.tCol).Name

	vCols := k.vCols
	if len(vCols) > seriesMaxLanes {
		inst.droppedLane = len(vCols) - seriesMaxLanes
		vCols = vCols[:seriesMaxLanes]
	}
	n := rec.NumRows()
	tArr := rec.Column(k.tCol)
	inst.t = make([]float64, 0, n)
	inst.rows = make([]int64, 0, n)
	lanes := make([]seriesLane, len(vCols))
	for i, ci := range vCols {
		lanes[i] = seriesLane{
			label: schema.Field(ci).Name,
			vals:  make([]float64, 0, n),
			valid: make([]bool, 0, n),
		}
	}
	for row := range n {
		if tArr.IsNull(int(row)) {
			inst.skippedRows++
			continue
		}
		ms, ok := temporalCellMS(tArr, int(row), false)
		if !ok {
			inst.skippedRows++
			continue
		}
		// Unix seconds: ScaleTime's unit, and UTC by construction — epoch
		// milliseconds carry no zone for a local one to leak into (§SD2).
		inst.t = append(inst.t, float64(ms)/1000)
		inst.rows = append(inst.rows, row)
		for i, ci := range vCols {
			v, got := numericCellValue(rec.Column(ci), row)
			if !got {
				lanes[i].nulls++
			}
			lanes[i].vals = append(lanes[i].vals, v)
			lanes[i].valid = append(lanes[i].valid, got)
		}
	}
	inst.grid = classifySeriesGrid(inst.t)
	for i := range lanes {
		lanes[i].segs = buildSeriesSegments(lanes[i].valid, inst.grid.breaks)
	}
	inst.lanes = lanes
}

func (inst *SeriesDriver) yLabel() (label string) {
	if len(inst.lanes) == 1 {
		return inst.lanes[0].label
	}
	return ""
}

func (inst *SeriesDriver) statusLine() (line string) {
	var b strings.Builder
	// The readout register: this line is read AFTER the fact, where the digits
	// are the point (ADR-0097 Update 2026-08-05). The lane, column and
	// half-width counts stay plain — each is bounded by a cap, and Comma on a
	// one-digit number is noise. The gap count in the grid hint is NOT bounded
	// and takes the register; leave it grouped.
	fmt.Fprintf(&b, "%s points · %d lane(s) · x %s (UTC)", humanize.Comma(int64(len(inst.t))), len(inst.lanes), inst.tLabel)
	if len(inst.t) > 1 {
		fmt.Fprintf(&b, " · %s → %s",
			formatEpochMS(int64(inst.t[0]*1000)), formatEpochMS(int64(inst.t[len(inst.t)-1]*1000)))
	}
	fmt.Fprintf(&b, " · Δt %s", inst.grid.class)
	if inst.grid.medianSec > 0 {
		fmt.Fprintf(&b, " at %s", formatSeriesStep(inst.grid.medianSec))
	}
	var nulls int
	for i := range inst.lanes {
		nulls += inst.lanes[i].nulls
	}
	if nulls > 0 {
		fmt.Fprintf(&b, " · %s null value(s), drawn as breaks", humanize.Comma(int64(nulls)))
	}
	if inst.skippedRows > 0 {
		fmt.Fprintf(&b, " · %s row(s) without a time", humanize.Comma(inst.skippedRows))
	}
	if inst.droppedLane > 0 {
		fmt.Fprintf(&b, " · %d more numeric column(s) not drawn (cap %d)", inst.droppedLane, seriesMaxLanes)
	}
	if inst.decimate && inst.sourced > inst.drawn && inst.drawn > 0 {
		fmt.Fprintf(&b, " · drawing %s of %s (per-pixel min/max envelope; extremes kept)",
			humanize.Comma(int64(inst.drawn)), humanize.Comma(int64(inst.sourced)))
	}
	if inst.smooth.On {
		fmt.Fprintf(&b, " · smoothed ±%d, faded tail is extrapolated", inst.smooth.HalfWidth())
	}
	return b.String()
}

// formatSeriesStep renders a Δt in the coarsest unit that stays exact, which
// is also the unit a scaffold's INTERVAL should be written in. Prose spelling
// here — the SQL keeps ClickHouse's singular INTERVAL unit.
func formatSeriesStep(sec float64) (out string) {
	n, unit := seriesInterval(sec)
	out = fmt.Sprintf("%d %s", n, strings.ToLower(unit))
	if n != 1 {
		out += "s"
	}
	return
}

// seriesInterval picks the ClickHouse INTERVAL a measured Δt is spelled with.
// Exactness first: a step that divides evenly into a coarser unit is written
// there, so a 5-minute grid reads INTERVAL 5 MINUTE rather than 300 SECOND.
func seriesInterval(sec float64) (n int64, unit string) {
	s := int64(math.Round(sec))
	if s < 1 {
		return 1, "SECOND"
	}
	for _, step := range []struct {
		div  int64
		unit string
	}{{86400, "DAY"}, {3600, "HOUR"}, {60, "MINUTE"}} {
		if s%step.div == 0 {
			return s / step.div, step.unit
		}
	}
	return s, "SECOND"
}

// orderScaffold, fillScaffold and gridScaffold are §SD2's one-click SQL. Each
// is written to be READ: the measured step is substituted, and the query the
// user already has becomes the subquery, so nothing is silently rewritten.
func (inst *SeriesDriver) orderScaffold() (sql string) {
	return fmt.Sprintf("-- ADR-0163: a series has to be ordered before it can be read as one.\n"+
		"SELECT *\nFROM (\n  -- your query here\n)\nORDER BY %s\n", inst.tLabel)
}

func (inst *SeriesDriver) fillScaffold() (sql string) {
	n, unit := seriesInterval(inst.grid.medianSec)
	return fmt.Sprintf("-- ADR-0163: make the gaps explicit NULL rows at the measured step.\n"+
		"SELECT *\nFROM (\n  -- your query here\n)\nORDER BY %s WITH FILL STEP INTERVAL %d %s\n",
		inst.tLabel, n, unit)
}

func (inst *SeriesDriver) gridScaffold() (sql string) {
	n, unit := seriesInterval(inst.grid.medianSec)
	v := "value"
	if len(inst.lanes) > 0 {
		v = inst.lanes[0].label
	}
	// toDateTime64: toStartOfInterval yields a DateTime, which reaches Arrow as
	// a bare UInt32 indistinguishable from a count — so the cast is what keeps
	// the scaffold's own output claimable by this panel.
	return fmt.Sprintf("-- ADR-0163: aggregate to a grid in SQL, so the samples stay real.\n"+
		"SELECT toDateTime64(toStartOfInterval(%s, INTERVAL %d %s), 3) AS %s, avg(%s) AS %s\n"+
		"FROM (\n  -- your query here\n)\nGROUP BY %s\nORDER BY %s\n",
		inst.tLabel, n, unit, inst.tLabel, v, v, inst.tLabel, inst.tLabel)
}

// renderSeriesTab is the dock-tab entry (spec.Render): frame plumbing around
// the PanelI dispatch, mirroring the Distribution tab.
func (inst *PlayApp) renderSeriesTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query returning a time column and at least one number (ADR-0163) to draw a series.") {
			rt.Small().Weak()
		}
		return
	}
	inst.seriesDriver.noteExecuted(executed)
	// What the score channel's node IS — a client call or an ordinary CTE —
	// is resolved here, where the split and the resolved params both exist.
	// The panel needs it for two things it cannot get from the score rows:
	// the detector's causality, and the window to compute the mandated
	// baseline at (§SD5 S3).
	inst.noteSeriesScoreCall()
	inputs := map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("series"), rec: rec, schema: schema, sig: inst.frameSig},
	}
	// The optional channels are the `scores` / `spans` CTEs BY NAME (§SD1),
	// each on its own lane like the Sankey's second input. Offered only when
	// the split has them, so a buffer without them reads as "no overlay"
	// rather than as pending.
	if r, s := inst.demandSeriesAux(seriesScoresNodeID, &inst.seriesScoresLane); r != nil || s != nil {
		inputs[chScores] = channelInput{node: seriesScoresNodeID, rec: r, schema: s, sig: inst.frameSig}
		if r != nil {
			defer r.Release()
		}
	}
	if r, s := inst.demandSeriesAux(seriesSpansNodeID, &inst.seriesSpansLane); r != nil || s != nil {
		inputs[chSpans] = channelInput{node: seriesSpansNodeID, rec: r, schema: s, sig: inst.frameSig}
		if r != nil {
			defer r.Release()
		}
	}
	reject := dispatchPanel(seriesPanel{driver: inst.seriesDriver}, inputs, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// noteSeriesScoreCall tells the driver what produced the score channel. A
// `scores` CTE that is an ordinary query is perfectly legal — the channel is
// filled by NAME, not by provenance — and then the panel simply has no
// detector to label and no window to match a baseline to, which the chrome
// says rather than guesses.
func (inst *PlayApp) noteSeriesScoreCall() {
	node, ok := findSplitNode(inst.currentSplit, seriesScoresNodeID)
	if !ok || node.Client == nil {
		inst.seriesDriver.noteScoreCall(nil, 0, false)
		return
	}
	call := node.Client
	// The window is the third argument of every scoring function in the
	// roster, but the roster is meant to grow — so the position is CHECKED
	// rather than assumed, and a future function shaped differently loses
	// its baseline instead of panicking.
	if len(call.Args) < 3 || call.Spec.Args[2].Kind != tsArgInt {
		inst.seriesDriver.noteScoreCall(call, 0, false)
		return
	}
	// A slot resolves through the same params the lane sends, so a window
	// driven by a live signal moves the baseline with it.
	params := resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig)
	window, wErr := tsIntArg(call, 2, params)
	inst.seriesDriver.noteScoreCall(call, window, wErr == nil)
}

// forgetSeriesLanes drops the optional lanes' memos on Run, for the reason
// every other named-CTE panel does: the memo key is the SQL, which a re-Run
// leaves unchanged, so a lane that failed transiently would stay stuck on the
// stored error while the rest of the query recovered.
func (inst *PlayApp) forgetSeriesLanes() {
	if inst.seriesScoresLane != nil {
		inst.seriesScoresLane.forget()
	}
	if inst.seriesSpansLane != nil {
		inst.seriesSpansLane.forget()
	}
}

// demandSeriesAux drives one optional CTE on its own lane, the Sankey's
// demandSankeyNodes shape with the node id as a parameter.
func (inst *PlayApp) demandSeriesAux(nodeID NodeID, lane **nodeLane) (rec arrow.RecordBatch, schema *arrow.Schema) {
	node, ok := findSplitNode(inst.currentSplit, nodeID)
	if !ok {
		return
	}
	if *lane == nil {
		*lane = newNodeLane(clientExecutor{client: inst.client, opts: newExecOptions("series-" + string(nodeID))},
			memory.NewGoAllocator(), 0)
	}
	// compileNodeFor, not a bare fuse: `scores` and `spans` are USUALLY client
	// nodes, whose SQL is their input CTE and whose transform rides on the
	// compiled node (ADR-0163 §SD4). Fusing the body instead would send
	// `SELECT tsAnomalyScores(…)` to a server that has never heard of it —
	// which is exactly what this did until M2 wired the overlays and the
	// channels came back empty.
	v := (*lane).demand(compileNodeFor(inst.currentSplit, node, inst.lastRunBound, inst.frameSig))
	return v.rec, v.schema
}
