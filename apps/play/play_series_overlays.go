package play

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// play_series_overlays.go is ADR-0163 M2: the score and span channels drawn,
// and the honesty apparatus that ships WITH them rather than after them.
//
// Three of those are not features a user turns on, and that is the decision
// (§SD5):
//
//   - The BASELINE is computed in-panel and drawn by default whenever a score
//     lane is. A detector's curve alone looks impressive; beside a moving
//     average's residual it has to earn the difference. Spelling it in SQL was
//     the rejected alternative precisely because a user who did not write it
//     would never see it.
//   - The WARM-UP region is shaded. A detector that has not trained yet
//     reports nothing, which reaches the plot as a flat zero — indistinguishable
//     from calm unless the picture says otherwise.
//   - CAUSALITY is labelled from the registry. A two-sided score drawn on the
//     same axes as a causal one, unlabelled, is the specific dishonesty S1
//     exists to prevent: it invites reading hindsight as what an alert would
//     have known.

const (
	// seriesScorePlotHeight is the linked score plot's box. Shorter than the
	// series above it — the score is read for WHERE its peaks are, against a
	// time axis the series already established.
	seriesScorePlotHeight = 150
	// seriesBandAlpha is the span bands' fill alpha. Recessive by IDS
	// guidance: the series must stay legible through them, exactly as the
	// Timeline's own bands are drawn under its events.
	seriesBandAlpha = 0x30
	// seriesWarmAlpha shades the warm-up region a little more faintly still —
	// it marks the ABSENCE of a reading, so it must not read as data.
	seriesWarmAlpha = 0x22
)

// seriesScores is the folded score channel plus the baseline mandated beside
// it.
type seriesScores struct {
	t     []float64
	score []float64
	warm  []bool

	// baseline is adscore's moving-average residual over the SAME input and
	// window (§SD5 S3). Empty when the panel could not identify the input
	// column or the window — in which case the chrome says so rather than
	// drawing an unlabelled second curve.
	baseline []float64
	// baselineWhy explains an empty baseline, so its absence is a statement
	// rather than a gap.
	baselineWhy string

	// detector, causal and window come from the client node that produced
	// this channel, which is the only place they exist.
	detector string
	causal   bool
	window   int32
}

// seriesSpan is one folded band from the span channel.
type seriesSpan struct {
	from, to float64 // Unix seconds, the plot's x unit
	label    string
	packed   uint32
}

// noteScoreCall records what the client node behind the score channel is, so
// the panel can label causality and compute the baseline at the same window.
// Called from the tab body, which is where the split and the resolved params
// both exist.
func (inst *SeriesDriver) noteScoreCall(call *tsCall, window int32, windowOK bool) {
	inst.scoreCall = call
	inst.scoreWindow = window
	inst.scoreWindowOK = windowOK
}

// foldSeriesScores reads the score channel. The contract is the one
// tsAnomalyScores emits — a temporal column, `score`, and optionally
// `warm_up` — but it is read by NAME here rather than assumed, because a
// hand-written CTE may legitimately fill this channel too.
func foldSeriesScores(rec arrow.RecordBatch) (out seriesScores, ok bool) {
	if rec == nil || rec.NumRows() == 0 {
		return
	}
	schema := rec.Schema()
	tCol := -1
	for ci, f := range schema.Fields() {
		if isSeriesTemporalType(f.Type) {
			tCol = ci
			break
		}
	}
	sIdx := schema.FieldIndices("score")
	if tCol < 0 || len(sIdx) == 0 {
		return
	}
	warmCol := -1
	if wi := schema.FieldIndices("warm_up"); len(wi) > 0 {
		warmCol = wi[0]
	}
	tArr, sArr := rec.Column(tCol), rec.Column(sIdx[0])
	n := int(rec.NumRows())
	out.t = make([]float64, 0, n)
	out.score = make([]float64, 0, n)
	out.warm = make([]bool, 0, n)
	for row := range n {
		ms, got := temporalCellMS(tArr, row, false)
		if !got || tArr.IsNull(row) {
			continue
		}
		v, isNum := numericCellValue(sArr, int64(row))
		if !isNum {
			continue
		}
		out.t = append(out.t, float64(ms)/1000)
		out.score = append(out.score, v)
		warm := false
		if warmCol >= 0 {
			if b, isBool := rec.Column(warmCol).(*array.Boolean); isBool && !b.IsNull(row) {
				warm = b.Value(row)
			}
		}
		out.warm = append(out.warm, warm)
	}
	ok = len(out.t) > 0
	return
}

// foldSeriesSpans reads the span channel on the Timeline band contract, which
// is what tsAnomalySpans emits directly. A colour the band vocabulary cannot
// resolve is dropped and counted, matching the Timeline's own reader — the
// same rule in both places, so a spans CTE behaves identically wherever it is
// pointed.
func foldSeriesSpans(rec arrow.RecordBatch) (out []seriesSpan, skipped int) {
	if rec == nil || rec.NumRows() == 0 {
		return
	}
	schema := rec.Schema()
	fromIdx := schema.FieldIndices(timelineSlotBandFrom)
	toIdx := schema.FieldIndices(timelineSlotBandTo)
	colIdx := schema.FieldIndices(timelineSlotBandColor)
	if len(fromIdx) == 0 || len(toIdx) == 0 || len(colIdx) == 0 {
		return
	}
	labIdx := schema.FieldIndices(timelineSlotBandLabel)
	fromArr, toArr, colArr := rec.Column(fromIdx[0]), rec.Column(toIdx[0]), rec.Column(colIdx[0])
	n := int(rec.NumRows())
	out = make([]seriesSpan, 0, n)
	for row := range n {
		fromMS, okF := temporalCellMS(fromArr, row, false)
		toMS, okT := temporalCellMS(toArr, row, false)
		if !okF || !okT || toMS < fromMS {
			skipped++
			continue
		}
		packed, resolved := bandColorByName(readStringCell(colArr, row))
		if !resolved {
			skipped++
			continue
		}
		s := seriesSpan{from: float64(fromMS) / 1000, to: float64(toMS) / 1000, packed: packed}
		if len(labIdx) > 0 {
			s.label = readStringCell(rec.Column(labIdx[0]), row)
		}
		out = append(out, s)
	}
	return
}

// buildSeriesBaseline computes the mandated comparison (§SD5 S3) from the
// panel's OWN input: the lane the detector read, at the detector's window.
// Both are needed, and when either is missing the caller says so instead of
// drawing a curve it cannot describe.
func (inst *SeriesDriver) buildSeriesBaseline() (baseline []float64, why string) {
	switch {
	case inst.scoreCall == nil:
		return nil, "the score channel is not a ts* node, so its input and window are unknown — no baseline to compare against"
	case !inst.scoreWindowOK:
		return nil, "the detector's window is not a resolved number, so the baseline has no window to match"
	case len(inst.scoreCall.Args) < 2:
		return nil, "the detector names no value column, so the baseline cannot read the same input"
	}
	name := inst.scoreCall.Args[1]
	lane := -1
	for i := range inst.lanes {
		if inst.lanes[i].label == name {
			lane = i
			break
		}
	}
	if lane < 0 {
		return nil, "the charted result has no `" + name + "` column, so the baseline cannot read the same input the detector did"
	}
	// adscore over the panel's own values, at the detector's window. Same
	// input, same window — anything else would be a comparison of two
	// different questions.
	return adscore.BaselineScores(inst.lanes[lane].vals, adscore.BaselineMovingAverageResidual, inst.scoreWindow), ""
}

// renderSeriesSpans paints the span bands behind whatever is on the plot. The
// y extent comes from the axis's PREVIOUS range (one frame behind, like every
// readback), which is what lets a band span the full height without the panel
// having to know the data's range.
func (inst *SeriesDriver) renderSeriesSpans(p *implot.Plot) {
	if len(inst.spans) == 0 {
		return
	}
	yMin, yMax, ok := p.AxisRangePrev(implot.AxisY1)
	if !ok {
		return
	}
	for i := range inst.spans {
		s := &inst.spans[i]
		label := s.label
		if label == "" {
			label = "span"
		}
		xs := []float64{s.from, s.to}
		lo := []float64{yMin, yMin}
		hi := []float64{yMax, yMax}
		p.SetNextColor(s.packed&^uint32(0xff) | seriesBandAlpha)
		p.ShadedBetween(label, xs, lo, hi)
	}
}

// renderSeriesScorePlot draws the score lane, its baseline and the warm-up
// shading, on a plot whose x axis is LINKED to the series above — so panning
// or zooming either keeps the two readable against each other, which is the
// only way a score is read at all.
func (inst *SeriesDriver) renderSeriesScorePlot(w float32) {
	sc := &inst.scores
	for p := range implot.Scoped(inst.ids, "##play-series-scores", w, seriesScorePlotHeight) {
		p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
		p.SetupAxisLinks(implot.AxisX1, &inst.xLinkMin, &inst.xLinkMax)
		p.SetupAxes("", "score", implot.AxisFlagsNone, implot.AxisFlagsNone)

		inst.renderSeriesSpans(p)
		inst.renderSeriesWarmUp(p)

		if len(sc.baseline) == len(inst.t) && len(sc.baseline) > 0 {
			// Drawn FIRST so the detector's curve sits on top: the baseline is
			// the thing being beaten, not the thing being read.
			p.SetNextColor(color.Hex(styletokens.NeutralDefault.AsHex()).Literal()).SetNextWeight(1.2)
			p.Line("baseline (moving-average residual)", inst.t, sc.baseline)
		}
		label := sc.detector
		if label == "" {
			label = "score"
		}
		p.SetNextColor(color.Hex(styletokens.QualitativeCycle(3).AsHex()).Literal()).SetNextWeight(1.6)
		p.Line(label, sc.t, sc.score)
	}
}

// renderSeriesWarmUp shades every stretch carrying no score. Without it the
// detector's zeros there are indistinguishable from a quiet period, which is
// the reading the shading exists to prevent (§SD5 S1/S2).
func (inst *SeriesDriver) renderSeriesWarmUp(p *implot.Plot) {
	sc := &inst.scores
	if len(sc.warm) == 0 {
		return
	}
	yMin, yMax, ok := p.AxisRangePrev(implot.AxisY1)
	if !ok {
		return
	}
	packed := styletokens.NeutralSubtle.AsHex()&^uint32(0xff) | seriesWarmAlpha
	for _, run := range warmUpRuns(sc.warm) {
		xs := []float64{sc.t[run[0]], sc.t[run[1]]}
		lo := []float64{yMin, yMin}
		hi := []float64{yMax, yMax}
		p.SetNextColor(packed)
		p.ShadedBetween("no score (warm-up)", xs, lo, hi)
	}
}

// warmUpRuns collapses the per-position flags into inclusive [lo, hi] runs, so
// a stretch with no score shades as ONE band rather than as a picket fence of
// per-sample slivers. Split out from the drawing because that is the part with
// an off-by-one worth testing — particularly the run that ends the series.
func warmUpRuns(warm []bool) (runs [][2]int) {
	for i := 0; i < len(warm); {
		if !warm[i] {
			i++
			continue
		}
		j := i
		for j < len(warm) && warm[j] {
			j++
		}
		runs = append(runs, [2]int{i, j - 1})
		i = j
	}
	return
}

// seriesOverlayCaption is the honesty line the overlays carry. It states the
// three things a reader cannot get from the curves: which engine produced the
// score, whether it is causal, and what the second curve is.
func (inst *SeriesDriver) seriesOverlayCaption() (line string) {
	sc := &inst.scores
	var b strings.Builder
	if sc.detector != "" {
		fmt.Fprintf(&b, "%s, window %d — ", sc.detector, sc.window)
	}
	if sc.causal {
		b.WriteString("causal: each value uses only what came before it, so replaying this IS the backtest.")
	} else {
		b.WriteString("two-sided: every value sees the whole series, so this is NOT what an alert would have known at the time.")
	}
	switch {
	case sc.baselineWhy != "":
		b.WriteString(" No baseline — ")
		b.WriteString(sc.baselineWhy)
		b.WriteString(".")
	case len(sc.baseline) > 0:
		b.WriteString(" The grey curve is a moving-average residual at the same window: the one-liner this has to beat to be worth its cost.")
	}
	var warm int
	for _, w := range sc.warm {
		if w {
			warm++
		}
	}
	if warm > 0 {
		fmt.Fprintf(&b, " %d position(s) carry no score at all (shaded) — the detector had too little history, or too little room at the end.", warm)
	}
	return b.String()
}

// renderSeriesOverlayChrome paints the caption plus the span inventory.
func (inst *SeriesDriver) renderSeriesOverlayChrome() {
	if len(inst.scores.t) > 0 {
		for rt := range c.RichTextLabel(inst.seriesOverlayCaption()) {
			rt.Small().Weak()
		}
	}
	if len(inst.spans) == 0 && inst.spansSkipped == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d flagged extent(s)", len(inst.spans))
	b.WriteString(" — plateaus, not peaks: the band is how long the run lasted, and its label carries the peak score.")
	if inst.spansSkipped > 0 {
		fmt.Fprintf(&b, " %d row(s) skipped (an unknown colour token, or an end before its start).", inst.spansSkipped)
	}
	for rt := range c.RichTextLabel(b.String()) {
		rt.Small().Weak()
	}
}
