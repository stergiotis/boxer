package trailviewer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// The numeric series of ADR-0350 §SD11 (proposed), built by M5: one
// measure of the timeline — rows, tokens in, tokens out, elapsed
// milliseconds — under one statistic — sum, p50, p95, max — per bucket of
// the strip's step, over the case, window and filter the table last read,
// drawn as an implot line under a line that states the measure, the
// buckets, the peak and the total. The buckets are chosen from the strip's
// width (stepFor, stripBuckets), so they never outnumber the pixels and no
// decimator is applied: a selection-based one such as LTTB can drop the
// narrow extreme a reader is looking for (play's series panel,
// envelopeDecimate).

// MeasureE is what the series measures per row.
type MeasureE uint8

const (
	// MeasureRows counts rows; its one statistic is the count.
	MeasureRows MeasureE = iota
	MeasureTokensIn
	MeasureTokensOut
	MeasureElapsedMs
)

// AllMeasures lists the measures in the order the combo shows them.
var AllMeasures = []MeasureE{MeasureRows, MeasureTokensIn, MeasureTokensOut, MeasureElapsedMs}

var measureNames = [...]string{"rows", "tokens-in", "tokens-out", "elapsed-ms"}

func (inst MeasureE) String() string {
	if int(inst) < len(measureNames) {
		return measureNames[inst]
	}
	return "unknown"
}

// column is the timeline column the measure reads; empty for rows.
func (inst MeasureE) column() string {
	if inst == MeasureRows {
		return ""
	}
	return inst.String()
}

// ParseMeasure reads a measure's name.
func ParseMeasure(s string) (m MeasureE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range measureNames {
		if n == s {
			return MeasureE(i), true
		}
	}
	return
}

func measureNameList() string { return strings.Join(measureNames[:], ", ") }

// StatE is the statistic over a bucket's rows.
type StatE uint8

const (
	StatSum StatE = iota
	StatP50
	StatP95
	StatMax
)

// AllStats lists the statistics in the order the combo shows them.
var AllStats = []StatE{StatSum, StatP50, StatP95, StatMax}

var statNames = [...]string{"sum", "p50", "p95", "max"}

func (inst StatE) String() string {
	if int(inst) < len(statNames) {
		return statNames[inst]
	}
	return "unknown"
}

// ParseStat reads a statistic's name.
func ParseStat(s string) (st StatE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range statNames {
		if n == s {
			return StatE(i), true
		}
	}
	return
}

func statNameList() string { return strings.Join(statNames[:], ", ") }

// expr is the statistic over col in SQL.
func (inst StatE) expr(col string) string {
	switch inst {
	case StatP50:
		return "quantileTDigest(0.5)(" + col + ")"
	case StatP95:
		return "quantileTDigest(0.95)(" + col + ")"
	case StatMax:
		return "max(" + col + ")"
	}
	return "sum(" + col + ")"
}

// seriesPick is the two picks, the series resource's value.
type seriesPick struct {
	Measure MeasureE
	Stat    StatE
}

// validate refuses a statistic the measure has not: rows has the count.
func (inst seriesPick) validate() (err error) {
	if inst.Measure == MeasureRows && inst.Stat != StatSum {
		return app.RefuseOperation("the rows measure has one statistic, sum, which is the count")
	}
	return
}

func (inst seriesPick) String() string {
	if inst.Measure == MeasureRows {
		return "rows"
	}
	return inst.Measure.String() + " " + inst.Stat.String()
}

// seriesBucketsMax bounds the buckets one read returns; the step is chosen
// from the width so a read holds at most stripBuckets of them.
const seriesBucketsMax = 2000

// SeriesPoint is one bucket: its start, the rows carrying the measure and
// the statistic over them.
type SeriesPoint struct {
	Bucket int64   `ch:"bucket"`
	N      uint64  `ch:"n"`
	V      float64 `ch:"v"`
}

// seriesSQL is the series' read: per bucket of step, the rows carrying the
// measure and the statistic over them; for rows, the count.
func (inst *marts) seriesSQL(q listQuery, pick seriesPick, step time.Duration) string {
	tl := trailviews.ViewTimeline
	where := inst.rowsWhere(q, nil)
	v := "count()"
	if col := pick.Measure.column(); col != "" {
		where = append(where, inst.col(tl, col)+" > 0")
		v = pick.Stat.expr(inst.col(tl, col))
	}
	return "SELECT toInt64(toUnixTimestamp(toStartOfInterval(" + inst.col(tl, "ts") + ", INTERVAL " +
		strconv.FormatInt(max(int64(step/time.Second), 1), 10) + " SECOND))) AS bucket, count() AS n, toFloat64(" + v + ") AS v FROM " +
		inst.table(tl) + " WHERE " + strings.Join(where, " AND ") + " GROUP BY bucket ORDER BY bucket LIMIT " + strconv.Itoa(seriesBucketsMax)
}

// seriesData is the series: a point per bucket, the peak, the total, and
// what the line says.
type seriesData struct {
	pick    seriesPick
	step    time.Duration
	xs, ys  []float64
	points  []SeriesPoint
	rows    uint64
	peakT   int64
	peakV   float64
	total   float64
	cut     bool
	hasPeak bool
}

// buildSeries folds the points into the plot's arrays, each at its
// bucket's middle; cut says the read hit its bucket bound.
func buildSeries(points []SeriesPoint, pick seriesPick, step time.Duration, cut bool) (d seriesData) {
	d.pick, d.step, d.points, d.cut = pick, step, points, cut
	half := step.Seconds() / 2
	for _, p := range points {
		d.xs = append(d.xs, float64(p.Bucket)+half)
		d.ys = append(d.ys, p.V)
		d.rows += p.N
		d.total += p.V
		if !d.hasPeak || p.V > d.peakV {
			d.hasPeak, d.peakT, d.peakV = true, p.Bucket, p.V
		}
	}
	return
}

// line is the series in words, times in UTC.
func (inst *seriesData) line() string { return inst.lineIn(time.UTC) }

// lineIn is the series in words: the measure and statistic, buckets of
// what size, the rows, the peak bucket, the total for a sum, and what was
// cut; times in loc.
func (inst *seriesData) lineIn(loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	if len(inst.points) == 0 {
		if inst.pick.Measure == MeasureRows {
			return "0 rows in the case and window"
		}
		return fmt.Sprintf("0 rows in the case and window carry %s", inst.pick.Measure)
	}
	s := fmt.Sprintf("%s over %d buckets of %s from %d rows; peak %s with %s", inst.pick, len(inst.points), inst.step, inst.rows,
		time.Unix(inst.peakT, 0).In(loc).Format("2006-01-02 15:04:05 MST"), formatMeasure(inst.peakV))
	if inst.pick.Stat == StatSum {
		s += "; total " + formatMeasure(inst.total)
	}
	if inst.cut {
		s += fmt.Sprintf("; the read stopped at %d buckets", seriesBucketsMax)
	}
	return s
}

// formatMeasure renders a statistic: whole where it is whole.
func formatMeasure(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func (inst *App) seriesKey() aggKey {
	return aggKey{gen: inst.cache.gen, pick: uint8(inst.an.series.pick.Measure), pk2: uint8(inst.an.series.pick.Stat), step: inst.cache.step}
}

// readSeries is the series' read for tickAnalytics.
func (inst *App) readSeries(q listQuery) func(ctx context.Context, p *probe) (seriesData, error) {
	pick, step := inst.an.series.pick, inst.cache.step
	return func(ctx context.Context, p *probe) (d seriesData, err error) {
		points, err := queryRows[SeriesPoint](ctx, p, p.marts.seriesSQL(q, pick, step))
		if err != nil {
			return
		}
		return buildSeries(points, pick, step, len(points) == seriesBucketsMax), nil
	}
}

// applySeries sets the picks; the series tab's combos and set_series both
// land here.
func (inst *App) applySeries(pick seriesPick) {
	inst.an.series.pick = pick
}

func (inst *App) setSeriesGesture(pick seriesPick) {
	gestureOr(inst, opSetSeries, SetSeriesArgs{Measure: pick.Measure.String(), Statistic: pick.Stat.String()}, func() { inst.applySeries(pick) })
}

const seriesPlotHeight = 240.0

// renderSeries is the series tab: the two picks, the line, and the plot
// with time on x.
func (inst *App) renderSeries() {
	an := &inst.an
	pick := an.series.pick
	for range c.HorizontalTop().KeepIter() {
		m := MeasureE(inst.pickCombo("sr-measure", "measure", measureNames[:], int(pick.Measure)))
		st := pick.Stat
		if m == MeasureRows {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, "the count per bucket").Small().End().Keep()).Send()
			st = StatSum
		} else {
			st = StatE(inst.pickCombo("sr-stat", "statistic", statNames[:], int(pick.Stat)))
		}
		if next := (seriesPick{Measure: m, Stat: st}); next != pick {
			inst.setSeriesGesture(next)
		}
	}
	if inst.cache.step <= 0 {
		c.Label("(waiting for the table's first read)").Send()
		return
	}
	d, ok := aggStatus(&an.series.data, inst.seriesKey())
	if !ok {
		return
	}
	c.Label(d.lineIn(inst.picker.loc)).Wrap().Send()
	if len(d.points) == 0 {
		return
	}
	if w, _, ok := c.CapturePaneSize(inst.ids.ProbeSeq("series-pane")); ok && w > 100 {
		an.plotW = w
	}
	w := an.plotW
	if w <= 0 {
		w = stripFallbackWidth
	}
	p := implot.Begin(inst.ids, "##trail-series", w, seriesPlotHeight)
	p.SetupAxes("", d.pick.String(), implot.AxisFlagsNone, implot.AxisFlagsAutoFit)
	p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
	if from, to := inst.cache.from, inst.cache.to; !from.IsZero() {
		p.SetupAxisLimits(implot.AxisX1, float64(from.Unix()), float64(to.Unix()), implot.CondAlways)
	}
	p.SetNextColor(seriesColor(0))
	p.Line(d.pick.String(), d.xs, d.ys)
	p.End()
}
