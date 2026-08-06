package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func distTestSchema(extra ...arrow.Field) *arrow.Schema {
	fields := []arrow.Field{
		{Name: distsql.ColSeries, Type: arrow.BinaryTypes.String},
		{Name: distsql.ColN, Type: arrow.PrimitiveTypes.Uint64},
		{Name: distsql.ColPs, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
		{Name: distsql.ColQs, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
	}
	fields = append(fields, extra...)
	return arrow.NewSchema(fields, nil)
}

func TestResolveDistColumns(t *testing.T) {
	k, reason := resolveDistColumns(distTestSchema())
	require.Empty(t, reason)
	assert.Equal(t, 0, k.seriesCol)
	assert.Equal(t, 1, k.nCol)
	assert.Equal(t, 2, k.psCol)
	assert.Equal(t, 3, k.qsCol)
	assert.Equal(t, -1, k.estimatorCol)

	// Missing required columns: the hint names each one.
	_, reason = resolveDistColumns(arrow.NewSchema([]arrow.Field{
		{Name: "x", Type: arrow.PrimitiveTypes.Float64}}, nil))
	for _, want := range []string{distsql.ColSeries, distsql.ColN, distsql.ColPs, distsql.ColQs} {
		assert.Contains(t, reason, want)
	}

	// n must be an integer count.
	_, reason = resolveDistColumns(arrow.NewSchema([]arrow.Field{
		{Name: distsql.ColSeries, Type: arrow.BinaryTypes.String},
		{Name: distsql.ColN, Type: arrow.PrimitiveTypes.Float64},
		{Name: distsql.ColPs, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
		{Name: distsql.ColQs, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
	}, nil))
	assert.Contains(t, reason, "integer count")

	// Grid columns must be float arrays.
	_, reason = resolveDistColumns(arrow.NewSchema([]arrow.Field{
		{Name: distsql.ColSeries, Type: arrow.BinaryTypes.String},
		{Name: distsql.ColN, Type: arrow.PrimitiveTypes.Uint64},
		{Name: distsql.ColPs, Type: arrow.BinaryTypes.String},
		{Name: distsql.ColQs, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
	}, nil))
	assert.Contains(t, reason, "Array(Float64)")

	// Histogram triplet is all-or-none.
	_, reason = resolveDistColumns(distTestSchema(
		arrow.Field{Name: distsql.ColHistLo, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)}))
	assert.Contains(t, reason, "all-or-none")
}

// distTestRecord builds one contract record; ps/qs per row.
func distTestRecord(t *testing.T, schema *arrow.Schema, labels []string, ns []uint64, ps, qs [][]float64) arrow.RecordBatch {
	t.Helper()
	b := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer b.Release()
	for i := range labels {
		b.Field(0).(*array.StringBuilder).Append(labels[i])
		b.Field(1).(*array.Uint64Builder).Append(ns[i])
		for f, vals := range [][]float64{ps[i], qs[i]} {
			lb := b.Field(2 + f).(*array.ListBuilder)
			lb.Append(true)
			lb.ValueBuilder().(*array.Float64Builder).AppendValues(vals, nil)
		}
	}
	return b.NewRecordBatch()
}

func TestDistFold(t *testing.T) {
	schema := distTestSchema()
	grid := []float64{0.25, 0.5, 0.75}
	rec := distTestRecord(t, schema,
		[]string{"a", "b"}, []uint64{100, 200},
		[][]float64{grid, grid},
		[][]float64{{1, 2, 3}, {2, 4, 8}})
	defer rec.Release()

	k, reason := resolveDistColumns(schema)
	require.Empty(t, reason)
	d := NewDistDriver(c.NewWidgetIdStack())
	d.noteExecuted(time.Unix(1, 0))
	d.rebuild(rec, schema, k)

	require.Empty(t, d.foldErr)
	require.Len(t, d.series, 2)
	assert.Equal(t, "a", d.series[0].label)
	assert.Equal(t, int64(200), d.series[1].n)
	assert.Equal(t, []float64{2, 4, 8}, d.series[1].qs)
	assert.True(t, d.sharedGrid)
	assert.False(t, d.haveHist)

	// The fold is cached on (executed, schema): a re-render with the same
	// token must not refold (mutate the model to observe).
	d.series[0].label = "mutated"
	d.rebuild(rec, schema, k)
	assert.Equal(t, "mutated", d.series[0].label)
}

func TestDistFoldRejectsBadGrid(t *testing.T) {
	schema := distTestSchema()
	rec := distTestRecord(t, schema,
		[]string{"a", "bad"}, []uint64{100, 100},
		[][]float64{{0.25, 0.5, 0.75}, {0.5, 0.5, 0.75}}, // row 1 not ascending
		[][]float64{{1, 2, 3}, {1, 2, 3}})
	defer rec.Release()

	k, reason := resolveDistColumns(schema)
	require.Empty(t, reason)
	d := NewDistDriver(c.NewWidgetIdStack())
	d.noteExecuted(time.Unix(2, 0))
	d.rebuild(rec, schema, k)

	require.NotEmpty(t, d.foldErr)
	assert.Contains(t, d.foldErr, "Row 1")
	assert.Contains(t, d.foldErr, "bad")
	assert.Contains(t, d.foldErr, "not strictly ascending")
	assert.Empty(t, d.series)
}

func TestDistFoldGridMismatch(t *testing.T) {
	schema := distTestSchema()
	rec := distTestRecord(t, schema,
		[]string{"a", "b"}, []uint64{10, 10},
		[][]float64{{0.25, 0.5, 0.75}, {0.1, 0.5, 0.9}},
		[][]float64{{1, 2, 3}, {1, 2, 3}})
	defer rec.Release()

	k, reason := resolveDistColumns(schema)
	require.Empty(t, reason)
	d := NewDistDriver(c.NewWidgetIdStack())
	d.noteExecuted(time.Unix(3, 0))
	d.rebuild(rec, schema, k)

	require.Empty(t, d.foldErr)
	assert.False(t, d.sharedGrid, "differing ps grids must disable the shift view")
}

// The plot box must never be taller than the pane it is drawn in. implot draws
// the x tick labels along the BOTTOM of the box, so a box that overflows loses
// them — and the part of the y range below the clip reads as missing data
// rather than as a cropped view. The Chart tab carried the same bug and its
// fix (ADR-0172) is the shape copied here; the pane's ScrollArea does not
// rescue either of them, because implot captures the wheel while the pointer
// is over the plot (ADR-0140).
func TestDistPlotHeightNeverExceedsPane(t *testing.T) {
	d := NewDistDriver(c.NewWidgetIdStack())
	assert.Equal(t, float32(distPlotHeight), d.plotHeight(),
		"before the probe lands, the preferred height")

	// The probe misses on the frame a Lazy tab comes back, so the driver holds
	// its last good answer — a miss must never resize the box.
	d.paneH = 1000
	assert.Equal(t, float32(distPlotHeight), d.plotHeight(),
		"a tall pane does not stretch the box past its preferred height")

	d.paneH = 255 // the applet result pane that surfaced this
	assert.Equal(t, float32(255-distPaneSlack), d.plotHeight())

	// The property, over every pane the probe can plausibly report. It holds
	// down to the floor; below that no height helps — implot lays its gutters
	// out at the floor whatever it is handed — and the leaf's ScrollArea takes
	// over.
	for _, pane := range []float32{distPlotMinH + distPaneSlack, 130, 255, 420, 600, 1000} {
		d.paneH = pane
		assert.LessOrEqualf(t, d.plotHeight(), pane,
			"a %vpt pane must not get a taller box", pane)
	}

	// The OTHER way these labels are lost, and the one that cost the most to
	// find: the gutters carrying them come out of the box height, not from
	// space outside it, so a box under implot's minimum clips its own axis
	// while the pane still looks roomy. Charging for that chrome a second time
	// in the budget — as this panel briefly did — pushes the box below the
	// minimum and causes the clip it was meant to prevent.
	assert.GreaterOrEqual(t, distPlotMinH, implot.MinBoxHeight(false, true, true, 1),
		"the floor must clear what the deepest of the four views needs")
	for _, pane := range []float32{20, 60, 130, 255, 1000} {
		d.paneH = pane
		assert.GreaterOrEqualf(t, d.plotHeight(), distPlotMinH,
			"a %vpt pane must not drive the box under implot's minimum", pane)
	}
}
