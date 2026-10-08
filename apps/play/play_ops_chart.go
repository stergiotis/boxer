package play

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The Chart pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05): which reading the pane resolved, the axis it drew, its lanes
// or grid, the mark and log scale, and what the caps left out. The pane's
// mark chips go through set_chart_options; its log box is bound, so the
// write-back records the person's change.

const (
	opGetChart        = "get_chart"
	opSetChartOptions = "set_chart_options"
	opsResChart       = chartPaneId
	chartPaneId       = "chart"
)

// ChartAxisReading is the x axis a lanes chart drew.
type ChartAxisReading struct {
	Name       string   `desc:"the column the axis reads; row or row in series when the result has no x"`
	Kind       string   `desc:"categorical (distinct values in the order they first appear), numeric, time (UTC), row (the row number in result order) or row in series (the row number inside each series)"`
	Categories int32    `json:",omitzero" desc:"for a categorical axis, how many distinct values"`
	Labels     []string `json:",omitzero" desc:"for a categorical axis, the first 40 category labels in axis order, each cut at 64 bytes"`
}

// ChartLaneReading is one drawn series of a lanes chart.
type ChartLaneReading struct {
	Label  string   `desc:"the series' legend label: its column, its series value, or both"`
	Points int32    `desc:"points drawn, nulls included"`
	Nulls  int32    `json:",omitzero" desc:"null values; the line breaks there and nothing is interpolated"`
	YMin   *float64 `json:",omitzero" desc:"the smallest value; absent when every value is null"`
	YMax   *float64 `json:",omitzero" desc:"the largest value; absent when every value is null"`
}

// ChartGridReading is the heatmap a grid chart drew.
type ChartGridReading struct {
	X       string   `desc:"the column across"`
	Y       string   `desc:"the column down"`
	Z       string   `desc:"the column coloured"`
	Columns int32    `desc:"distinct x keys, drawn at equal width, not to numeric scale"`
	Rows    int32    `desc:"distinct y keys"`
	ZMin    *float64 `json:",omitzero" desc:"the smallest z"`
	ZMax    *float64 `json:",omitzero" desc:"the largest z"`
	Holes   int32    `json:",omitzero" desc:"cells no row fills"`
}

// ChartReading is get_chart's result.
type ChartReading struct {
	Drawn   PaneDraw           `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Reading string             `json:",omitzero" desc:"lanes (x plus numeric columns, split by series) or grid (x, y and z as a heatmap)"`
	X       *ChartAxisReading  `json:",omitzero" desc:"the x axis of a lanes chart"`
	Lanes   []ChartLaneReading `json:",omitzero" desc:"the series of a lanes chart, at most 24"`
	Grid    *ChartGridReading  `json:",omitzero" desc:"the heatmap of a grid chart"`
	Marks   []string           `json:",omitzero" desc:"the marks this chart offers; the first is its default"`
	Mark    string             `desc:"the mark drawn: bar, line, scatter or heatmap"`
	// MarkPicked separates a picked mark from the default that follows the
	// data.
	MarkPicked   bool  `desc:"true when the person or set_chart_options picked the mark; otherwise it follows the data"`
	LogAvailable bool  `desc:"whether the chart offers a log axis: only when every value drawn is above zero"`
	Log          bool  `desc:"whether a log axis is asked for; it is drawn only while the chart offers one"`
	LogDrawn     bool  `desc:"whether the chart draws a log axis (log y, or log colour for a heatmap)"`
	RowsNotRead  int64 `json:",omitzero" desc:"rows past the 100,000 the chart reads"`
	SeriesHidden int32 `json:",omitzero" desc:"series past the 24 the chart draws; GROUP BY coarser to see them"`
}

// SetChartOptionsArgs is set_chart_options' argument; a field left out
// keeps the person's setting.
type SetChartOptionsArgs struct {
	Mark *string `json:",omitzero" desc:"bar, line, scatter or heatmap; one get_chart lists in marks"`
	Log  *bool   `json:",omitzero" desc:"draw a log axis (log y, or log colour for a heatmap); true only when get_chart says log is available"`
}

// chartFoldView is the copied part of a fold, immutable once built.
type chartFoldView struct {
	reading       string
	x             *ChartAxisReading
	lanes         []ChartLaneReading
	grid          *ChartGridReading
	marks         []string
	logOK         bool
	truncated     int64
	droppedSeries int32
}

// chartOpsView is what get_chart reads: the shared fold copy and the
// options as they stand.
type chartOpsView struct {
	fold       *chartFoldView
	mark       string
	markPicked bool
	log        bool
	logDrawn   bool
}

// chartMarkName is a mark as the operations spell it.
func chartMarkName(m chartMarkE) string { return strings.ToLower(m.String()) }

// chartMarkByName is the mark a name spells.
func chartMarkByName(name string) (m chartMarkE, ok bool) {
	for _, m = range []chartMarkE{chartMarkBar, chartMarkLine, chartMarkScatter, chartMarkHeatmap} {
		if strings.EqualFold(strings.TrimSpace(name), chartMarkName(m)) {
			return m, true
		}
	}
	return 0, false
}

func chartMarkNames(marks []chartMarkE) (names []string) {
	names = make([]string, 0, len(marks))
	for _, m := range marks {
		names = append(names, chartMarkName(m))
	}
	return
}

// chartAxisKind is a lanes chart's x axis as get_chart names it.
func (inst *ChartDriver) chartAxisKind() string {
	switch inst.xAxis {
	case chartAxisCategorical:
		return "categorical"
	case chartAxisTemporal:
		return "time"
	case chartAxisRow:
		if inst.xPerSeries {
			return "row in series"
		}
		return "row"
	default:
		return "numeric"
	}
}

// foldView copies the driver's last fold. It walks every drawn value once,
// so the caller copies it once per fold.
func (inst *ChartDriver) foldView() (v *chartFoldView) {
	v = &chartFoldView{marks: chartMarkNames(inst.availableMarks()), logOK: inst.logOK,
		truncated: inst.truncated, droppedSeries: int32(inst.droppedSeries)}
	if inst.reading == chartReadingGrid {
		g := &inst.grid
		v.reading = "grid"
		v.grid = &ChartGridReading{X: opsLabel(inst.xName), Y: opsLabel(inst.yName), Z: opsLabel(inst.zName),
			Columns: int32(g.nCols), Rows: int32(g.nRows), Holes: int32(g.holes)}
		if g.nCols*g.nRows > g.holes {
			v.grid.ZMin, v.grid.ZMax = finite(g.vmin), finite(g.vmax)
		}
		return
	}
	v.reading = "lanes"
	v.x = &ChartAxisReading{Name: opsLabel(inst.xName), Kind: inst.chartAxisKind()}
	if inst.xAxis == chartAxisCategorical {
		v.x.Categories = int32(len(inst.catLabels))
		n := min(len(inst.catLabels), chartMaxTickLabels)
		v.x.Labels = make([]string, 0, n)
		for _, l := range inst.catLabels[:n] {
			v.x.Labels = append(v.x.Labels, opsLabel(l))
		}
	}
	v.lanes = make([]ChartLaneReading, 0, len(inst.lanes))
	for i := range inst.lanes {
		l := &inst.lanes[i]
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, y := range l.ys {
			if !math.IsNaN(y) {
				lo, hi = math.Min(lo, y), math.Max(hi, y)
			}
		}
		v.lanes = append(v.lanes, ChartLaneReading{Label: opsLabel(l.label), Points: int32(len(l.ys)),
			Nulls: int32(l.nulls), YMin: finite(lo), YMax: finite(hi)})
	}
	return
}

// chartView is the Chart's view for the snapshot: the fold copied once
// per fold, the options every snapshot.
func (inst *PlayApp) chartView() (v chartOpsView) {
	d := inst.chartDriver
	if d.foldedOnce && (inst.paneViews.chart == nil || inst.paneViews.chartGen != d.foldGen) {
		inst.paneViews.chart, inst.paneViews.chartGen = d.foldView(), d.foldGen
	}
	active := d.activeMark()
	v = chartOpsView{fold: inst.paneViews.chart, mark: chartMarkName(active),
		markPicked: d.markSet && d.mark == active, log: d.logScale, logDrawn: d.logActive()}
	return
}

// chartReading is get_chart.
func chartReading(sn *opsSnap, _ appops.None) (out ChartReading, err error) {
	d, readable, err := paneDrawOf(sn, chartPaneId)
	if err != nil {
		return
	}
	v := sn.paneViews.chart
	out = ChartReading{Drawn: d, Mark: v.mark, MarkPicked: v.markPicked, Log: v.log}
	if !readable || v.fold == nil {
		return
	}
	f := v.fold
	out.Reading, out.X, out.Lanes, out.Grid, out.Marks = f.reading, f.x, f.lanes, f.grid, f.marks
	out.LogAvailable, out.LogDrawn = f.logOK, v.logDrawn
	out.RowsNotRead, out.SeriesHidden = f.truncated, f.droppedSeries
	return
}

// setOptions is set_chart_options on the driver: every named option is
// checked against the last fold before any is applied. Before the chart
// has folded anything, a known mark is kept for when it does.
func (inst *ChartDriver) setOptions(in SetChartOptionsArgs) (err error) {
	if in.Mark == nil && in.Log == nil {
		return noOptionsRefusal(chartPaneId, "mark", "log")
	}
	var mark chartMarkE
	if in.Mark != nil {
		var ok bool
		mark, ok = chartMarkByName(*in.Mark)
		if !ok {
			return app.RefuseOperation("no mark " + strconv.Quote(*in.Mark) + "; a mark is bar, line, scatter or heatmap")
		}
		if avail := inst.availableMarks(); inst.foldedOnce && !slices.Contains(avail, mark) {
			return app.RefuseOperation("this chart offers " + strings.Join(chartMarkNames(avail), ", ") +
				"; a heatmap needs the grid reading (x, y and z), the other marks the lanes reading")
		}
	}
	if in.Log != nil && *in.Log && inst.foldedOnce && !inst.logOK {
		return app.RefuseOperation("this chart offers no log axis: a value it draws is zero, negative or missing")
	}
	if in.Mark != nil {
		inst.mark, inst.markSet = mark, true
	}
	if in.Log != nil {
		inst.logScale = *in.Log
	}
	return
}

// requestOptions is a mark chip's click: through set_chart_options when
// play's launcher routes it, directly otherwise.
func (inst *ChartDriver) requestOptions(in SetChartOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// chartOptionsDigest is the chart resource: the picked mark and the log
// box, compared across the write-back.
func chartOptionsDigest(p *PlayApp) string {
	d := p.chartDriver
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("mark=")
	if d.markSet {
		b.WriteString(chartMarkName(d.mark))
	}
	b.WriteString("|log=" + strconv.FormatBool(d.logScale))
	return b.String()
}

func addChartOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[ChartReading, appops.None, SetChartOptionsArgs]{
		pane:     chartPaneId,
		resource: "the Chart pane's options: the picked mark and the log axis",
		digest:   chartOptionsDigest,
		get:      opGetChart,
		getSummary: "read what the Chart pane last drew: its reading (lanes or grid), the x axis and its categories, " +
			"each series' points, nulls and range or the heatmap's keys, the mark and log axis, and what its caps left out",
		set:        opSetChartOptions,
		setSummary: "set the Chart pane's mark (bar, line, scatter, heatmap) or log axis",
		gesture:    "the mark chips and the log box above the chart",
		follows:    []string{"the pane draws with the new mark or axis from its next frame; the result is not rerun"},
		read:       chartReading,
		apply:      func(p *PlayApp, in SetChartOptionsArgs) error { return p.chartDriver.setOptions(in) },
	})
}
