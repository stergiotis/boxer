package play

import (
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// ADR-0270, update of 2026-10-05: each result pane an agent reads has a
// bespoke get_<pane> query and set_<pane>_options command over a resource
// named after the pane.
func TestPaneOpsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, pane := range []struct{ id, get, set string }{
		{chartPaneId, opGetChart, opSetChartOptions},
		{distPaneId, opGetDist, opSetDistOptions},
		{seriesPaneId, opGetSeries, opSetSeriesOptions},
	} {
		get, ok := m.Operations.Lookup(pane.get)
		require.True(t, ok, pane.get)
		assert.Equal(t, app.OperationClassQuery, get.Class, pane.get)
		assert.Equal(t, app.OperationEffectNone, get.Effect, pane.get)
		assert.True(t, get.Untrusted, "%s quotes the data's labels", pane.get)
		assert.True(t, get.Agents, pane.get)
		assert.Equal(t, []string{pane.id, opsResResult, opsResPanes}, get.Reads, pane.get)
		set, ok := m.Operations.Lookup(pane.set)
		require.True(t, ok, pane.set)
		assert.Equal(t, app.OperationClassCommand, set.Class, pane.set)
		assert.Equal(t, app.OperationEffectDocument, set.Effect, pane.set)
		assert.False(t, set.Untrusted, pane.set)
		assert.True(t, set.Agents, pane.set)
		assert.Equal(t, []string{pane.id}, set.Writes, pane.set)
		assert.NotEmpty(t, set.Gesture, pane.set)
	}
}

// drawnPane makes the pane read as drawn last frame from result id, as
// renderTabBody leaves it. Lazy panes are made eager: their gate goes live
// only under a real frame.
func drawnPane(p *PlayApp, pane string, result ResultID, schema *arrow.Schema) {
	for i := range p.tabs.specs {
		if p.tabs.specs[i].ID == pane {
			p.tabs.specs[i].Lazy = false
		}
	}
	p.frameSchema = schema
	p.frame = 8
	if p.paneDrawn == nil {
		p.paneDrawn = map[string]paneDrawnMark{}
	}
	p.paneDrawn[pane] = paneDrawnMark{frame: 7, result: result, fed: true}
}

func applyOp(t *testing.T, h app.OperationsHandlerI, op string, args any) error {
	t.Helper()
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, op, mustEncode(t, args))
	return err
}

func queryErr(t *testing.T, h app.OperationsHandlerI, op string, args any) error {
	t.Helper()
	var raw []byte
	if args != nil {
		raw = mustEncode(t, args)
	}
	_, err := h.Snapshot().Query(op, raw)
	return err
}

// A read never draws or raises its pane: one that has not drawn, a lazy one
// whose tab is not in front, and one that drew no result are refused.
func TestPaneReadsRefuseAPaneThatHasNotDrawn(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	for _, op := range []string{opGetChart, opGetDist, opGetSeries, opGetTimeline, opGetTreemap, opGetIcicle, opGetKanban, opGetCards, opGetWorld, opGetVectorfield, opGetNetwork, opGetGraphview, opGetSankey, opGetTable, opGetFiles, opGetChatPane, opGetMap, opGetExperiments} {
		err := queryErr(t, h, op, nil)
		require.Error(t, err, op)
		assert.Contains(t, err.Error(), "not drawn: show_pane", op)
	}
	// Drawn once, but the lazy gate is not live: the fold may be of an
	// earlier result.
	p.paneDrawn = map[string]paneDrawnMark{chartPaneId: {frame: 1, result: 3, fed: true}}
	p.frame = 2
	err := queryErr(t, h, opGetChart, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "show_pane chart")

	drawnPane(p, chartPaneId, 3, nil)
	p.paneDrawn[chartPaneId] = paneDrawnMark{frame: 7, result: 3, fed: false}
	err = queryErr(t, h, opGetChart, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no result to draw")
}

func chartLanesSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: "revenue", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "cost", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
}

func foldChart(t *testing.T, p *PlayApp, schema *arrow.Schema, rec arrow.RecordBatch, at int64) {
	t.Helper()
	k, reason := resolveChartColumns(schema)
	require.Empty(t, reason)
	p.chartDriver.noteExecuted(time.Unix(at, 0))
	p.chartDriver.rebuild(rec, schema, k)
}

func TestGetChartReadsTheLastFold(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	schema := chartLanesSchema()
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"eu", "us", "apac"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{3, 0, 9}, []bool{true, false, true})
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2, 4}, nil)
	})
	defer rec.Release()
	foldChart(t, p, schema, rec, 1)
	drawnPane(p, chartPaneId, 5, schema)

	r := queryOp[ChartReading](t, h, opGetChart, nil)
	assert.Equal(t, uint64(5), r.Drawn.ResultId)
	assert.Empty(t, r.Drawn.CannotDraw)
	assert.Contains(t, r.Drawn.Status, "2 series")
	assert.Equal(t, "lanes", r.Reading)
	require.NotNil(t, r.X)
	assert.Equal(t, "categorical", r.X.Kind)
	assert.Equal(t, int32(3), r.X.Categories)
	assert.Equal(t, []string{"eu", "us", "apac"}, r.X.Labels)
	require.Len(t, r.Lanes, 2)
	assert.Equal(t, "revenue", r.Lanes[0].Label)
	assert.Equal(t, int32(3), r.Lanes[0].Points)
	assert.Equal(t, int32(1), r.Lanes[0].Nulls)
	require.NotNil(t, r.Lanes[0].YMin)
	assert.Equal(t, 3.0, *r.Lanes[0].YMin)
	assert.Equal(t, 9.0, *r.Lanes[0].YMax)
	assert.Equal(t, []string{"bar", "line", "scatter"}, r.Marks)
	assert.Equal(t, "bar", r.Mark)
	assert.False(t, r.MarkPicked)
	assert.True(t, r.LogAvailable, "every value drawn is above zero; a null is not a value")

	// A fold the data rejects: the reading says why and carries no fold.
	grid := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.BinaryTypes.String},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	dup := chartRecord(t, grid, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"mon", "mon"}, nil)
		b.Field(1).(*array.StringBuilder).AppendValues([]string{"am", "am"}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer dup.Release()
	foldChart(t, p, grid, dup, 2)
	drawnPane(p, chartPaneId, 6, grid)
	r = queryOp[ChartReading](t, h, opGetChart, nil)
	assert.Contains(t, r.Drawn.CannotDraw, "Row 1")
	assert.Empty(t, r.Reading)
	assert.Nil(t, r.Grid)
	assert.Equal(t, "heatmap", r.Mark, "the settings are read even when the pane drew nothing")
}

// The fold is copied once per fold; snapshots in between share it.
func TestGetChartCopiesAFoldOnce(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	schema := chartLanesSchema()
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"eu"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{1}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{2}, nil)
	})
	defer rec.Release()
	foldChart(t, p, schema, rec, 1)
	first := p.chartView().fold
	require.NotNil(t, first)
	assert.Same(t, first, p.chartView().fold)
	foldChart(t, p, schema, rec, 2)
	assert.NotSame(t, first, p.chartView().fold)
}

func TestSetChartOptionsSetsAndRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	str := func(s string) *string { return &s }
	yes := true
	// Before any fold, a known mark is kept for when the chart draws.
	require.NoError(t, applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{Mark: str("heatmap")}))

	schema := chartLanesSchema()
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"eu", "us"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{-1, 2}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer rec.Release()
	foldChart(t, p, schema, rec, 1)
	assert.Equal(t, chartMarkBar, p.chartDriver.activeMark(), "a mark the chart does not offer falls back to its default")

	before := h.ResourceValue(opsResChart)
	require.NoError(t, applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{Mark: str("Line")}))
	assert.Equal(t, chartMarkLine, p.chartDriver.activeMark())
	assert.NotEqual(t, before, h.ResourceValue(opsResChart))

	err := applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{Mark: str("heatmap")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bar, line, scatter")
	err = applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{Mark: str("pie")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no mark")
	err = applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{Mark: str("scatter"), Log: &yes})
	require.Error(t, err, "a negative value leaves no log axis")
	assert.Contains(t, err.Error(), "no log axis")
	assert.Equal(t, chartMarkLine, p.chartDriver.activeMark(), "a refused call applies nothing")
	require.Error(t, applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{}))

	no := false
	require.NoError(t, applyOp(t, h, opSetChartOptions, SetChartOptionsArgs{Log: &no}), "set, never toggled")
	assert.False(t, p.chartDriver.logScale)
}

// The pane's option buttons go through their commands as the person's
// gestures; without a host serving the catalog they apply directly.
func TestPaneOptionButtonsGoThroughTheirCommands(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	mark := "scatter"
	p.chartDriver.requestOptions(SetChartOptionsArgs{Mark: &mark})
	e := lastEntry(t, eng)
	assert.Equal(t, opSetChartOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, []string{opsResChart}, e.Resources)
	assert.Equal(t, chartMarkScatter, p.chartDriver.mark)

	view := "boxen"
	p.distDriver.requestOptions(SetDistOptionsArgs{View: &view})
	e = lastEntry(t, eng)
	assert.Equal(t, opSetDistOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResDist))
	assert.Equal(t, 2, p.distDriver.view)

	hw := int32(20)
	p.seriesDriver.requestOptions(SetSeriesOptionsArgs{HalfWidth: &hw})
	e = lastEntry(t, eng)
	assert.Equal(t, opSetSeriesOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResSeries))
	assert.Equal(t, int32(20), p.seriesDriver.smooth.HalfWidth())

	bare, _ := opsLauncher(t)
	require.Nil(t, bare.inner.gestureCtx)
	bare.inner.chartDriver.requestOptions(SetChartOptionsArgs{Mark: &mark})
	assert.Equal(t, chartMarkScatter, bare.inner.chartDriver.mark)
}

func foldDist(t *testing.T, p *PlayApp, labels []string, ns []uint64, ps, qs [][]float64) *arrow.Schema {
	t.Helper()
	schema := distTestSchema()
	rec := distTestRecord(t, schema, labels, ns, ps, qs)
	defer rec.Release()
	k, reason := resolveDistColumns(schema)
	require.Empty(t, reason)
	p.distDriver.noteExecuted(time.Unix(int64(len(labels)), 0))
	p.distDriver.rebuild(rec, schema, k)
	require.Empty(t, p.distDriver.foldErr)
	return schema
}

func TestGetDistReadsQuantilesAndDistances(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	grid := []float64{0.25, 0.5, 0.75}
	schema := foldDist(t, p, []string{"a", "b"}, []uint64{100, 200},
		[][]float64{grid, grid}, [][]float64{{1, 2, 3}, {2, 4, 8}})
	drawnPane(p, distPaneId, 4, schema)

	r := queryOp[DistReading](t, h, opGetDist, GetDistArgs{LetterValues: true})
	assert.Equal(t, uint64(4), r.Drawn.ResultId)
	assert.Equal(t, "ecdf", r.View)
	require.Len(t, r.Views, 4)
	assert.True(t, r.Views[1].Available, "two series on one grid admit the Shift view")
	assert.False(t, r.Views[3].Available)
	assert.Contains(t, r.Views[3].WhyNot, "hist_lo")
	assert.True(t, r.SharedGrid)
	assert.Equal(t, "a", r.Baseline)
	require.Len(t, r.Series, 2)
	a, b := r.Series[0], r.Series[1]
	assert.Nil(t, a.W1, "the baseline has no distance to itself")
	require.NotNil(t, b.W1)
	assert.InDelta(t, distsql.Wasserstein1(grid, []float64{2, 4, 8}, []float64{1, 2, 3}), *b.W1, 1e-12)
	require.NotNil(t, a.DkwEpsilon)
	assert.InDelta(t, distsql.DkwEpsilon(100, distBandAlpha), *a.DkwEpsilon, 1e-12)
	assert.Equal(t, 0.25, a.GridFrom)
	assert.Equal(t, 0.75, a.GridTo)
	require.Len(t, a.Quantiles, 5)
	assert.Equal(t, 2.0, a.Quantiles[2].Value, "the median is a grid point")
	assert.Equal(t, 1.0, a.Quantiles[0].Value, "p 0.05 is outside the grid and reads as its first value")
	require.NotEmpty(t, a.Letters)
	assert.Equal(t, int32(1), a.Letters[0].Depth)
	assert.Equal(t, 2.0, a.Letters[0].Lower)

	// The baseline is the selected series, which the selection signal moves.
	p.distDriver.selected = 1
	r = queryOp[DistReading](t, h, opGetDist, nil)
	assert.Equal(t, "b", r.Baseline)
	assert.Equal(t, int32(1), r.BaselineRow)
	assert.NotNil(t, r.Series[0].W1)
	assert.Nil(t, r.Series[1].W1)
	assert.Empty(t, r.Series[0].Letters, "letter values only when asked for")
}

func TestSetDistOptionsSetsAndRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	grid := []float64{0.25, 0.5, 0.75}
	foldDist(t, p, []string{"a", "b"}, []uint64{100, 200},
		[][]float64{grid, grid}, [][]float64{{1, 2, 3}, {2, 4, 8}})
	str := func(s string) *string { return &s }

	before := h.ResourceValue(opsResDist)
	require.NoError(t, applyOp(t, h, opSetDistOptions, SetDistOptionsArgs{View: str("shift")}))
	assert.Equal(t, 1, p.distDriver.view)
	assert.NotEqual(t, before, h.ResourceValue(opsResDist))

	err := applyOp(t, h, opSetDistOptions, SetDistOptionsArgs{View: str("histogram")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hist_lo")
	err = applyOp(t, h, opSetDistOptions, SetDistOptionsArgs{View: str("violin")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ecdf, shift, boxen, histogram")
	require.Error(t, applyOp(t, h, opSetDistOptions, SetDistOptionsArgs{}))
	assert.Equal(t, 1, p.distDriver.view)

	// One series on its own grid admits no Shift view.
	foldDist(t, p, []string{"solo"}, []uint64{10}, [][]float64{grid}, [][]float64{{1, 2, 3}})
	err = applyOp(t, h, opSetDistOptions, SetDistOptionsArgs{View: str("shift")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two or more series")
}

// seriesRecord is a minute grid with one missing sample and a null value.
func seriesRecord(t *testing.T) (arrow.RecordBatch, *arrow.Schema) {
	t.Helper()
	schema := seriesSchema(tsField("ts"), arrow.Field{Name: "latency", Type: arrow.PrimitiveTypes.Float64, Nullable: true})
	b := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer b.Release()
	base := int64(1_700_000_000_000)
	minutes := []int64{0, 1, 2, 3, 5, 6, 7, 8}
	vals := []float64{4, 7, 5, 0, 6, 2, 8, 3}
	valid := []bool{true, true, true, false, true, true, true, true}
	for i, m := range minutes {
		b.Field(0).(*array.TimestampBuilder).Append(arrow.Timestamp(base + m*60_000))
		if valid[i] {
			b.Field(1).(*array.Float64Builder).Append(vals[i])
		} else {
			b.Field(1).AppendNull()
		}
	}
	return b.NewRecordBatch(), schema
}

func TestGetSeriesReadsTheGridLanesAndOverlays(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec, schema := seriesRecord(t)
	defer rec.Release()
	k, reason := resolveSeriesColumns(schema)
	require.Empty(t, reason)
	d := p.seriesDriver
	d.noteExecuted(time.Unix(1, 0))
	d.rebuild(rec, schema, k)
	require.Empty(t, d.foldErr)
	drawnPane(p, seriesPaneId, 11, schema)

	r := queryOp[SeriesReading](t, h, opGetSeries, nil)
	assert.Equal(t, uint64(11), r.Drawn.ResultId)
	assert.Equal(t, "ts", r.X)
	assert.Equal(t, int32(8), r.Points)
	require.NotNil(t, r.Grid)
	assert.Equal(t, "regular with gaps", r.Grid.Class)
	assert.Equal(t, "1 minute", r.Grid.Step)
	assert.Equal(t, int32(1), r.Grid.Gaps)
	require.NotNil(t, r.Finding)
	assert.Equal(t, "add WITH FILL", r.Finding.Action)
	assert.Contains(t, r.Finding.Scaffold, "WITH FILL STEP INTERVAL 1 MINUTE")
	require.Len(t, r.Lanes, 1)
	assert.Equal(t, int32(1), r.Lanes[0].Nulls)
	assert.Equal(t, 2.0, *r.Lanes[0].Min)
	assert.Equal(t, 8.0, *r.Lanes[0].Max)
	assert.Equal(t, 3.0, *r.Lanes[0].Last)
	assert.False(t, r.Smoothing)
	assert.True(t, r.Envelope)
	assert.Nil(t, r.Overlays, "no scores or spans CTE, no overlays")

	// Spans with a recorded verdict are read; recording one stays the
	// person's (no operation writes boxer.tslabels).
	from := float64(1_700_000_000)
	d.spans = []seriesSpan{{from: from, to: from + 120, label: "spike"}}
	d.labels = map[tsLabelKey]tsVerdictE{{fromMS: int64(from * 1000), toMS: int64((from + 120) * 1000)}: tsVerdictConfirmed}
	r = queryOp[SeriesReading](t, h, opGetSeries, nil)
	require.NotNil(t, r.Overlays)
	require.Len(t, r.Overlays.Spans, 1)
	assert.Equal(t, "spike", r.Overlays.Spans[0].Label)
	assert.Equal(t, "confirmed", r.Overlays.Spans[0].Verdict)
	m := (&PlayLauncher{}).Manifest()
	for _, spec := range m.Operations.Operations {
		assert.NotContains(t, spec.Writes, "tslabels", spec.Name)
	}
}

func TestGetSeriesWalksAScoreLaneOnce(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	d := p.seriesDriver
	d.scores = seriesScores{t: []float64{1, 2, 3}, score: []float64{0.1, math.NaN(), 0.9}, warm: []bool{false, true, false}}
	d.scoresGen = 1
	o := p.seriesOverlays(d)
	require.NotNil(t, o)
	assert.Equal(t, int32(1), o.WarmUp)
	require.NotNil(t, o.ScoreMax)
	assert.Equal(t, 0.9, *o.ScoreMax)
	first := p.paneViews.seriesOverlay
	p.seriesOverlays(d)
	assert.Same(t, first, p.paneViews.seriesOverlay)
	d.scoresGen = 2
	p.seriesOverlays(d)
	assert.NotSame(t, first, p.paneViews.seriesOverlay)
}

func TestSetSeriesOptionsSetsAndRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	on, off := true, false
	hw := int32(24)
	before := h.ResourceValue(opsResSeries)
	require.NoError(t, applyOp(t, h, opSetSeriesOptions, SetSeriesOptionsArgs{Smoothing: &on, HalfWidth: &hw, Envelope: &off}))
	assert.True(t, p.seriesDriver.smooth.On)
	assert.Equal(t, int32(24), p.seriesDriver.smooth.HalfWidth())
	assert.False(t, p.seriesDriver.decimate)
	assert.NotEqual(t, before, h.ResourceValue(opsResSeries))

	big := int32(500)
	err := applyOp(t, h, opSetSeriesOptions, SetSeriesOptionsArgs{HalfWidth: &big, Smoothing: &off})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "half_width is")
	assert.True(t, p.seriesDriver.smooth.On, "a refused call applies nothing")
	require.Error(t, applyOp(t, h, opSetSeriesOptions, SetSeriesOptionsArgs{}))
}
