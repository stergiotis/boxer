package play

import (
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for the ADR-0172 Chart panel: the contract resolution (§SD1), the axis
// and key ordering rules (§SD2), the grouped-bar geometry (§SD4) and the
// nearest-point selection (§SD5). Everything here is pure over Arrow — the fold
// and the geometry are deliberately separable from rendering.

func chartRecord(t *testing.T, schema *arrow.Schema, fill func(b *array.RecordBuilder)) arrow.RecordBatch {
	t.Helper()
	b := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer b.Release()
	fill(b)
	return b.NewRecordBatch()
}

func chartFold(t *testing.T, schema *arrow.Schema, rec arrow.RecordBatch) (d *ChartDriver, k chartClaim) {
	t.Helper()
	k, reason := resolveChartColumns(schema)
	require.Empty(t, reason)
	d = NewChartDriver(c.NewWidgetIdStack())
	d.noteExecuted(time.Unix(1, 0))
	d.rebuild(rec, schema, k)
	require.Empty(t, d.foldErr)
	return
}

// §SD1: the lanes reading — `x` plus every OTHER numeric column, `series` and
// `x` never becoming lanes whatever their type.
func TestResolveChartColumnsLanes(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: "errors", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "oks", Type: arrow.PrimitiveTypes.Float64},
		{Name: "note", Type: arrow.BinaryTypes.String},
	}, nil)
	k, reason := resolveChartColumns(schema)
	require.Empty(t, reason)
	assert.Equal(t, chartReadingLanes, k.reading)
	assert.Equal(t, 0, k.xCol)
	assert.Equal(t, chartAxisCategorical, k.xAxis)
	assert.Equal(t, []int{1, 2}, k.laneCols, "only the numbers, and never `x` itself")
	assert.Equal(t, -1, k.seriesCol)

	// `series` is claimed BY NAME: numeric or not, it splits and never draws.
	numericSeries := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Float64},
		{Name: chartColSeries, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	k, reason = resolveChartColumns(numericSeries)
	require.Empty(t, reason)
	assert.Equal(t, 1, k.seriesCol)
	assert.Equal(t, []int{2}, k.laneCols, "`y` is an ordinary lane; `series` is not a lane")
	assert.Equal(t, chartAxisNumeric, k.xAxis)

	// A temporal x takes the time axis rather than being stringified.
	temporal := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: &arrow.TimestampType{Unit: arrow.Millisecond}},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	k, reason = resolveChartColumns(temporal)
	require.Empty(t, reason)
	assert.Equal(t, chartAxisTemporal, k.xAxis)
}

// §SD1: the grid reading, and every reject it can produce.
func TestResolveChartColumnsGridAndRejects(t *testing.T) {
	grid := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Uint8},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Uint8},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	k, reason := resolveChartColumns(grid)
	require.Empty(t, reason)
	assert.Equal(t, chartReadingGrid, k.reading)
	assert.Equal(t, 2, k.zCol)
	assert.Empty(t, k.laneCols, "the grid reading has no lanes")

	// Nothing numeric at all — the only schema-level reject the lanes reading
	// still has, now that a missing `x` numbers the rows instead. It names the
	// contract AND this result's columns.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "note", Type: arrow.BinaryTypes.String}}, nil))
	assert.Contains(t, reason, "at least one numeric column")
	assert.Contains(t, reason, "`name` utf8", "the hint names what this result carries")

	// `x` but nothing to draw against it.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: "label", Type: arrow.BinaryTypes.String}}, nil))
	assert.Contains(t, reason, "at least one numeric column besides `x`")

	// `z` present but not a value.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.BinaryTypes.String},
		{Name: chartColZ, Type: arrow.BinaryTypes.String}}, nil))
	assert.Contains(t, reason, "must be numeric")

	// `z` without `y`.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64}}, nil))
	assert.Contains(t, reason, "`x`, `y` and `z`")
	assert.Contains(t, reason, "no `y`")

	// `z` and `y` but no `x`. The lanes reading numbers the rows when `x` is
	// absent; the grid reading must NOT, so this stays a reject and says why.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: chartColY, Type: arrow.PrimitiveTypes.Uint8},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64}}, nil))
	assert.Contains(t, reason, "no `x`")
	assert.Contains(t, reason, "every row would be its own cell")

	// Neither key.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64}}, nil))
	assert.Contains(t, reason, "no `x` and no `y`")
}

// §SD2: with no `x` column the rows number themselves. The claim is otherwise
// the ordinary lanes reading — every number is still a lane, and a column that
// is not a number is still not one, which is why a `GROUP BY k` whose key is a
// string draws the ranking without drawing the key.
func TestResolveChartColumnsImplicitX(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "rows", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	k, reason := resolveChartColumns(schema)
	require.Empty(t, reason)
	assert.Equal(t, chartReadingLanes, k.reading)
	assert.Equal(t, -1, k.xCol, "there is no column to claim")
	assert.Equal(t, chartAxisRow, k.xAxis)
	assert.Equal(t, []int{1}, k.laneCols, "`name` is not numeric, so it is not a lane either")

	// `series` still splits, and still never draws.
	long := arrow.NewSchema([]arrow.Field{
		{Name: chartColSeries, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	k, reason = resolveChartColumns(long)
	require.Empty(t, reason)
	assert.Equal(t, chartAxisRow, k.xAxis)
	assert.Equal(t, 0, k.seriesCol)
	assert.Equal(t, []int{1}, k.laneCols)

	// The reject that remains is about the VALUE, not about `x`: with no `x`
	// the abscissa is free, so the message must not ask for one.
	_, reason = resolveChartColumns(arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String}}, nil))
	assert.Contains(t, reason, "at least one numeric column to draw")
	assert.NotContains(t, reason, "besides `x`")
}

// §SD2: a numeric or temporal key sorts ascending because it has an intrinsic
// order; a categorical one keeps first-appearance, because the query's row
// order is the only order it has.
func TestChartKeyerOrdering(t *testing.T) {
	cat := newChartKeyer(chartAxisCategorical)
	for _, s := range []string{"zulu", "alpha", "zulu", "mike"} {
		cat.add(s, 0)
	}
	labels, remap := cat.finish()
	assert.Equal(t, []string{"zulu", "alpha", "mike"}, labels)
	assert.Equal(t, []int{0, 1, 2}, remap, "a categorical key never remaps")

	num := newChartKeyer(chartAxisNumeric)
	assert.Equal(t, 0, num.add("30", 30))
	assert.Equal(t, 1, num.add("10", 10))
	assert.Equal(t, 0, num.add("30", 30), "an already-seen key keeps its slot")
	assert.Equal(t, 2, num.add("20", 20))
	labels, remap = num.finish()
	assert.Equal(t, []string{"10", "20", "30"}, labels)
	assert.Equal(t, []int{2, 0, 1}, remap, "provisional slot -> final position")
}

// The wide idiom: one lane per numeric column, labelled by the column name.
func TestChartFoldLanesWide(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: "errors", Type: arrow.PrimitiveTypes.Float64},
		{Name: "oks", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"eu", "us", "ap"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{1, 2, 3}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{10, 0, 30}, []bool{true, true, false})
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	require.Len(t, d.lanes, 2)
	assert.Equal(t, "errors", d.lanes[0].label)
	assert.Equal(t, "oks", d.lanes[1].label)
	assert.Equal(t, []float64{0, 1, 2}, d.lanes[0].xs, "categories take dense positions in row order")
	assert.Equal(t, []string{"eu", "us", "ap"}, d.catLabels)
	assert.Equal(t, []int64{0, 1, 2}, d.lanes[0].rows)

	// A NULL becomes NaN — the line breaks, nothing is interpolated.
	assert.Equal(t, 1, d.lanes[1].nulls)
	assert.True(t, math.IsNaN(d.lanes[1].ys[2]))
	assert.False(t, d.logOK, "a zero is present, so a log axis is not offered")

	// The fold is cached on (executed, schema): mutate the model to observe.
	d.lanes[0].label = "mutated"
	k, _ := resolveChartColumns(schema)
	d.rebuild(rec, schema, k)
	assert.Equal(t, "mutated", d.lanes[0].label)
	d.noteExecuted(time.Unix(2, 0))
	d.rebuild(rec, schema, k)
	assert.Equal(t, "errors", d.lanes[0].label, "a new execution refolds")
}

// The long idiom: `series` splits the rows, and with one lane the group IS the
// legend label.
func TestChartFoldLanesLong(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Float64},
		{Name: chartColSeries, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.Float64Builder).AppendValues([]float64{1, 1, 2, 2}, nil)
		b.Field(1).(*array.StringBuilder).AppendValues([]string{"b", "a", "b", "a"}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{10, 20, 30, 40}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	require.Len(t, d.lanes, 2)
	assert.Equal(t, "b", d.lanes[0].label, "groups keep first-appearance order")
	assert.Equal(t, "a", d.lanes[1].label)
	assert.Equal(t, []float64{1, 2}, d.lanes[0].xs)
	assert.Equal(t, []float64{10, 30}, d.lanes[0].ys)
	assert.Equal(t, []int64{1, 3}, d.lanes[1].rows, "each lane keeps its own result rows")
	assert.True(t, d.logOK, "every value is strictly positive")
}

// §SD2: with no `x`, the rows take the abscissa 1, 2, 3 … in the order the
// query returned them — the one order every result has. 1-based, because Detail
// names the same row `row 1 / N`, so the number on the axis is the number a
// click leads to.
func TestChartFoldImplicitRowNumbers(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "rows", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"alpha", "bravo", "charlie"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{30, 20, 10}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	require.Len(t, d.lanes, 1)
	assert.Equal(t, "rows", d.lanes[0].label)
	assert.Equal(t, []float64{1, 2, 3}, d.lanes[0].xs, "the rows number themselves, 1-based")
	assert.Equal(t, []int64{0, 1, 2}, d.lanes[0].rows, "which is not the same as the row INDEX")
	assert.Equal(t, 1.0, d.xMin)
	assert.Equal(t, 3.0, d.xMax)
	assert.Empty(t, d.catLabels, "an ordinal axis is not a categorical one — nothing is interned")
	assert.Equal(t, 1.0, d.barSlot, "consecutive ordinals ARE a gap of one")
	assert.False(t, d.xPerSeries)
	assert.Equal(t, "row", d.xName)

	// The abscissa is the panel's, not the query's, so the status line says so
	// rather than naming a column that is not there.
	assert.Contains(t, d.statusLine(), "the row number, in result order (implicit — the result has no `x`)")
	assert.NotContains(t, d.statusLine(), "`row`", "there is no column called row to look for")
}

// The per-series form of the same rule: the numbering restarts inside each
// group, so the groups OVERLAY on one abscissa the way they would on a real
// key. A single global counter would lay them end to end, which draws the row
// order rather than the groups.
func TestChartFoldImplicitRowNumbersPerSeries(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColSeries, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	// Unequal groups, deliberately interleaved: the counter is per group, not
	// per run of rows.
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"a", "b", "a", "b", "a"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{10, 15, 20, 25, 30}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	require.Len(t, d.lanes, 2)
	assert.Equal(t, "a", d.lanes[0].label)
	assert.Equal(t, []float64{1, 2, 3}, d.lanes[0].xs)
	assert.Equal(t, []int64{0, 2, 4}, d.lanes[0].rows)
	assert.Equal(t, []float64{1, 2}, d.lanes[1].xs, "the shorter group starts at 1 too")
	assert.Equal(t, []int64{1, 3}, d.lanes[1].rows)
	assert.True(t, d.xPerSeries)
	assert.Equal(t, "row in series", d.xName)
	assert.Contains(t, d.statusLine(), "the row number inside each `series`")
}

// §SD3: an implicit abscissa takes the continuous default, which is Line — with
// no key to label them, what a numbered result shows is the shape of an ordered
// sequence.
func TestChartImplicitXDefaultsToLine(t *testing.T) {
	d := NewChartDriver(c.NewWidgetIdStack())
	d.reading, d.xAxis = chartReadingLanes, chartAxisRow
	assert.Equal(t, chartMarkLine, d.activeMark())
	assert.Equal(t, []chartMarkE{chartMarkLine, chartMarkBar, chartMarkScatter}, d.availableMarks(),
		"and the other two stay one click away")
}

// §SD4: S bars share the slot, sit inside 0.8 of it, and stay in series order.
func TestChartBarGeometry(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Float64},
		{Name: "a", Type: arrow.PrimitiveTypes.Float64},
		{Name: "b", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		// A gap of 5 between the two x values: the slot is the smallest
		// positive gap, so bars cannot overlap their neighbours.
		b.Field(0).(*array.Float64Builder).AppendValues([]float64{0, 5}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{3, 4}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	assert.Equal(t, 5.0, d.barSlot)
	assert.Equal(t, 2.0, d.barWidth(), "0.8 * 5 / 2 series")

	// Series 0 left of centre, series 1 right; both inside x ± 0.4*slot.
	assert.Equal(t, []float64{-1, 4}, d.lanes[0].barXs)
	assert.Equal(t, []float64{1, 6}, d.lanes[1].barXs)
	half := d.barWidth() / 2
	for i := range d.lanes {
		for j, bx := range d.lanes[i].barXs {
			lo, hi := bx-half, bx+half
			assert.GreaterOrEqual(t, lo, d.lanes[i].xs[j]-0.4*d.barSlot-1e-9)
			assert.LessOrEqual(t, hi, d.lanes[i].xs[j]+0.4*d.barSlot+1e-9)
		}
	}

	// A categorical axis always divides a slot of one.
	catSchema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: "a", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	catRec := chartRecord(t, catSchema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"p", "q"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer catRec.Release()
	cd, _ := chartFold(t, catSchema, catRec)
	assert.Equal(t, 1.0, cd.barSlot)
	assert.Equal(t, 0.8, cd.barWidth())
}

// §SD1/§SD2: the grid pivots long into dense, row 0 at the TOP, holes NaN, and
// numeric keys ascending.
func TestChartFoldGrid(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	// Deliberately shuffled, and missing the (x=1, y=1) cell.
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.Int64Builder).AppendValues([]int64{2, 1, 2}, nil)
		b.Field(1).(*array.Int64Builder).AppendValues([]int64{1, 0, 0}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{7, 3, 5}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	g := &d.grid
	assert.Equal(t, []string{"1", "2"}, g.xLabels, "numeric keys sort ascending")
	assert.Equal(t, []string{"0", "1"}, g.yLabels)
	assert.Equal(t, 2, g.nRows)
	assert.Equal(t, 2, g.nCols)

	// Display row 0 is the TOP, which is the LAST y key (y=1).
	assert.True(t, math.IsNaN(g.values[0]), "(x=1, y=1) was never emitted")
	assert.Equal(t, 7.0, g.values[1])
	assert.Equal(t, []float64{3, 5}, g.values[2:], "the bottom row is y=0")
	assert.Equal(t, int64(-1), g.rows[0])
	assert.Equal(t, int64(0), g.rows[1], "the cell remembers which result row filled it")
	assert.Equal(t, 1, g.holes)
	assert.Equal(t, 3.0, g.vmin)
	assert.Equal(t, 7.0, g.vmax)
	assert.True(t, d.logOK)
}

// Every folded cell must be colourable, holes included. The fold marks an
// unfilled cell NaN and the panel hands the whole matrix to implot, which
// colours it cell by cell through Config.At — so a sparse grid crashed the GUI
// with index [-9223372036854775808] until At substituted BadColor. Any grid
// with a gap reaches this: the snippet's `GROUP BY x, y` over a query log is
// sparse whenever an hour of some weekday saw no queries.
func TestChartGridHolesAreColourable(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	// A 2×2 grid with only three of its four cells filled.
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.Int64Builder).AppendValues([]int64{0, 1, 0}, nil)
		b.Field(1).(*array.Int64Builder).AppendValues([]int64{0, 0, 1}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2, 3}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	require.Equal(t, 1, d.grid.holes)
	d.cm.DataMin, d.cm.DataMax = d.grid.vmin, d.grid.vmax

	for i, v := range d.grid.values {
		got := d.cm.At(v) // must not panic
		if math.IsNaN(v) {
			assert.Zero(t, got, "cell %d is a hole and must be transparent, not a colour", i)
		} else {
			assert.NotZero(t, got, "cell %d carries a value and must be opaque", i)
		}
	}
}

// A repeated cell rejects the WHOLE result — last-write-wins would fabricate a
// matrix the query never asked for.
func TestChartFoldGridRejectsDuplicateCell(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.BinaryTypes.String},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"mon", "mon"}, nil)
		b.Field(1).(*array.StringBuilder).AppendValues([]string{"am", "am"}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer rec.Release()

	k, reason := resolveChartColumns(schema)
	require.Empty(t, reason)
	d := NewChartDriver(c.NewWidgetIdStack())
	d.noteExecuted(time.Unix(1, 0))
	d.rebuild(rec, schema, k)

	require.NotEmpty(t, d.foldErr)
	assert.Contains(t, d.foldErr, "Row 1")
	assert.Contains(t, d.foldErr, "x = mon")
	assert.Contains(t, d.foldErr, "y = am")
	assert.Contains(t, d.foldErr, "GROUP BY x, y", "the reject carries the fix")
}

// A NULL key is a visible ∅ tick rather than a blank one.
func TestChartNullKeyIsVisible(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"a", ""}, []bool{true, false})
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	assert.Equal(t, []string{"a", "∅"}, d.catLabels)
}

// The fold records the extents the fit margin is computed from, and the pad
// itself is a fraction of the span with a magnitude-derived fallback when the
// span is degenerate — a constant series must not be fitted flat.
func TestChartFitMarginExtentsAndPad(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Float64},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.Float64Builder).AppendValues([]float64{2, 10, 6}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{100, 0, 300}, []bool{true, true, false})
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	assert.Equal(t, 2.0, d.xMin)
	assert.Equal(t, 10.0, d.xMax)
	assert.Equal(t, 0.0, d.yMin)
	assert.Equal(t, 100.0, d.yMax, "a NULL contributes no extent")

	assert.InDelta(t, 0.4, chartAxisPad(2, 10), 1e-9, "5% of the span")
	assert.InDelta(t, 1.0, chartAxisPad(7, 7), 1e-9, "a degenerate span falls back, never zero")
	assert.Greater(t, chartAxisPad(0, 0), 0.0, "including at the origin")
}

// The bar carve-out: a bar chart's baseline is a claim, so the margin never
// pushes it off the axis — only the free end moves.
func TestChartFitMarginKeepsBarBaseline(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{100, 200}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	// Bars fit the baseline themselves, so the padded span runs 0..yMax and
	// the low end must stay exactly on it.
	p := implot.NewDetached()
	d.applyFitMargin(p, chartMarkBar)  // must not panic on a detached plot
	d.applyFitMargin(p, chartMarkLine) // the line case pads both ends
	d.applyFitMargin(p, chartMarkScatter)

	// The rule itself, stated where it can be checked without a rendered plot:
	// a bar chart pads only away from the baseline.
	assert.Equal(t, 0.0, math.Min(d.yMin, 0), "the bar baseline is the fitted low end")
	assert.InDelta(t, 10.0, chartAxisPad(0, 200), 1e-9, "pad is 5% of 0..max, not of min..max")
}

// The plot box must never be taller than the pane it is drawn in. implot draws
// the x tick labels along the BOTTOM of the box, so a box that overflows loses
// them — and the part of the y range below the clip reads as missing data: a
// top-N bar chart in an applet pane looked like it had dropped two thirds of
// its rows and moved its baseline off zero, when in fact both were simply below
// the fold.
func TestChartPlotHeightNeverExceedsPane(t *testing.T) {
	d := NewChartDriver(c.NewWidgetIdStack())
	assert.Equal(t, float32(chartPlotHeight), d.plotHeight(),
		"before the probe lands, the preferred height")

	d.paneH = 1000
	assert.Equal(t, float32(chartPlotHeight), d.plotHeight(),
		"a tall pane does not stretch the box past its preferred height")

	d.paneH = 255 // the applet result pane that surfaced this
	assert.Equal(t, float32(255-chartPaneSlack), d.plotHeight())

	// The property, over every pane the probe can plausibly report. It holds
	// down to the floor; below that no height helps and the pane's ScrollArea
	// takes over, which is why the floor is set far under anything comfortable
	// rather than at it.
	for _, pane := range []float32{chartPlotMinH + chartPaneSlack, 130, 255, 380, 500, 1000} {
		d.paneH = pane
		assert.LessOrEqualf(t, d.plotHeight(), pane,
			"a %vpt pane must not get a taller box", pane)
	}

	// The SECOND way a pane-sized plot loses those labels, found while giving
	// the Distribution and Series tabs the same treatment: implot's gutters
	// come out of the box height rather than from space outside it, so a box
	// under its minimum clips its own axis while the pane still looks roomy.
	// The floor was originally an 80 picked as "far below comfortable", which
	// cleared this bound by luck; it is read from the widget now.
	assert.GreaterOrEqual(t, chartPlotMinH, implot.MinBoxHeight(false, true, true, 1),
		"the floor must clear what a both-axes-labelled plot needs")
	for _, pane := range []float32{20, 60, 130, 255, 1000} {
		d.paneH = pane
		assert.GreaterOrEqualf(t, d.plotHeight(), chartPlotMinH,
			"a %vpt pane must not drive the box under implot's minimum", pane)
	}
}

// §SD3: which marks are offered follows the resolved types, and the reader's
// pick is honoured only while it stays available.
func TestChartAvailableMarks(t *testing.T) {
	d := NewChartDriver(c.NewWidgetIdStack())
	d.reading, d.xAxis = chartReadingLanes, chartAxisCategorical
	assert.Equal(t, chartMarkBar, d.availableMarks()[0], "a category wants bars")
	assert.Equal(t, chartMarkBar, d.activeMark())

	d.xAxis = chartAxisNumeric
	assert.Equal(t, chartMarkLine, d.activeMark(), "a continuous axis wants a line")

	d.mark, d.markSet = chartMarkScatter, true
	assert.Equal(t, chartMarkScatter, d.activeMark(), "the reader's pick wins")

	d.reading = chartReadingGrid
	assert.Equal(t, []chartMarkE{chartMarkHeatmap}, d.availableMarks())
	assert.Equal(t, chartMarkHeatmap, d.activeMark(), "a pick that is no longer available falls back")
}

// The log choice outlives the result it was made on, so it can survive into a
// result the control is withdrawn from — a value at or below zero. Drawing it
// there would be a log axis with nothing on screen to turn it off, over values
// a log axis cannot place, so every consumer asks logActive rather than the
// flag. (The complementary half — that the choice is not DISCARDED, so a later
// positive result comes back the way the reader left it — is the second
// assertion.)
func TestChartLogIsInactiveWhereTheControlIsWithdrawn(t *testing.T) {
	positive := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, positive, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{10, 1000}, nil)
	})
	defer rec.Release()

	d, k := chartFold(t, positive, rec)
	require.True(t, d.logOK, "every value is positive, so the control is offered")
	d.logScale = true // the reader checks `log y`
	assert.True(t, d.logActive())

	// A result whose values reach zero: same schema, so only the DATA
	// withdraws the control.
	withZero := chartRecord(t, positive, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{0, 1000}, nil)
	})
	defer withZero.Release()
	d.noteExecuted(time.Unix(2, 0))
	d.rebuild(withZero, positive, k)
	require.False(t, d.logOK)
	assert.True(t, d.logScale, "the reader's choice is kept, not discarded")
	assert.False(t, d.logActive(), "but nothing draws a log axis while the control is absent")

	// The choice returns with the data that can carry it.
	d.noteExecuted(time.Unix(3, 0))
	d.rebuild(rec, positive, k)
	assert.True(t, d.logActive(), "a positive result again, checked as the reader left it")
}

// §SD5: selection is the nearest point in AXIS-NORMALISED space, so "nearest"
// means nearest on screen even when the two axes have wildly different spans.
func TestChartNearestRowIsAxisNormalised(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Float64},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	// Row 0 is far in x and identical in y; row 1 is near in both. For a click
	// at (0.5, 500), RAW distance picks row 0 (0.5 away, against row 1's 20) —
	// on a viewport spanning 1 in x and 1000 in y, row 1 is the one under the
	// pointer.
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.Float64Builder).AppendValues([]float64{0, 0.52}, nil)
		b.Field(1).(*array.Float64Builder).AppendValues([]float64{500, 520}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	row, ok := d.nearestRow(0.5, 500, 1, 1000)
	require.True(t, ok)
	assert.Equal(t, int64(1), row)

	// The same click on a viewport with the spans swapped picks the OTHER
	// point — the normalisation is what decides, not the raw distance.
	row, ok = d.nearestRow(0.5, 500, 1000, 1)
	require.True(t, ok)
	assert.Equal(t, int64(0), row)

	// Past the tolerance nothing is selected: an empty-space click must not
	// drag the whole dock's cursor to some far-off row.
	_, ok = d.nearestRow(0.5, 100, 1, 1000)
	assert.False(t, ok)
}

// The grid resolves a click to the cell under it, mapping the display flip
// back to the key order.
func TestChartGridRowAt(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColY, Type: arrow.PrimitiveTypes.Int64},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.Int64Builder).AppendValues([]int64{0, 1, 0, 1}, nil)
		b.Field(1).(*array.Int64Builder).AppendValues([]int64{0, 0, 1, 1}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2, 3, 4}, nil)
	})
	defer rec.Release()

	d, _ := chartFold(t, schema, rec)
	// Key index 0 sits at the BOTTOM: y ∈ [0, 1) is the first y key.
	row, ok := d.cellRowAt(0.5, 0.5)
	require.True(t, ok)
	assert.Equal(t, int64(0), row)
	row, ok = d.cellRowAt(1.5, 1.5)
	require.True(t, ok)
	assert.Equal(t, int64(3), row)

	_, ok = d.cellRowAt(2.5, 0.5)
	assert.False(t, ok, "outside the grid selects nothing")
}
