package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The `graph_opts` CTE of ADR-0231 §SD5.

// optsRec builds a one-row settings record from column name → value. A string
// value becomes a String column, a float64 a Float64 one and a bool a Bool
// one, which is how a query would write them.
func optsRec(t *testing.T, cells map[string]any, rows int) arrow.RecordBatch {
	t.Helper()
	if rows <= 0 {
		rows = 1
	}
	pool := memory.NewGoAllocator()
	var fields []arrow.Field
	var cols []arrow.Array
	names := make([]string, 0, len(cells))
	for n := range cells {
		names = append(names, n)
	}
	// Deterministic column order, so a claim's indices are stable.
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		switch v := cells[name].(type) {
		case string:
			b := array.NewStringBuilder(pool)
			for range rows {
				b.Append(v)
			}
			fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String})
			cols = append(cols, b.NewArray())
		case float64:
			b := array.NewFloat64Builder(pool)
			for range rows {
				b.Append(v)
			}
			fields = append(fields, arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Float64})
			cols = append(cols, b.NewArray())
		case bool:
			b := array.NewBooleanBuilder(pool)
			for range rows {
				b.Append(v)
			}
			fields = append(fields, arrow.Field{Name: name, Type: arrow.FixedWidthTypes.Boolean})
			cols = append(cols, b.NewArray())
		default:
			t.Fatalf("unhandled cell type for %q", name)
		}
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(rows))
	t.Cleanup(rec.Release)
	return rec
}

func readOpts(t *testing.T, cells map[string]any, rows int) graphOpts {
	t.Helper()
	rec := optsRec(t, cells, rows)
	return buildGraphOpts(rec, resolveGraphOpts(rec.Schema()))
}

// A query with no settings CTE says nothing, and every default stands.
func TestGraphOptsAbsentSaysNothing(t *testing.T) {
	o := buildGraphOpts(nil, noGraphOptsClaim())
	assert.False(t, o.LayoutSet)
	assert.False(t, o.UndirectedSet)
	assert.Zero(t, o.KScale)
	assert.Empty(t, o.SizeBy)
	assert.Empty(t, o.statusNote())
}

func TestGraphOptsReadsEveryColumn(t *testing.T) {
	o := readOpts(t, map[string]any{
		graphOptLayoutCol:       "radial",
		graphOptOrientationCol:  "left_right",
		graphOptRingDistCol:     90.0,
		graphOptRowDistCol:      70.0,
		graphOptColDistCol:      35.0,
		graphOptKScaleCol:       3.5,
		graphOptGravityCol:      0.4,
		graphOptForceModelCol:   "neighbour_embedding",
		graphOptExaggerationCol: 4.0,
		graphOptHideEdgesCol:    true,
		graphOptUndirectedCol:   true,
		graphOptPinOnDragCol:    true,
		graphOptSizeByCol:       "pagerank",
		graphOptToneByCol:       "component",
		graphOptOpacityByCol:    "-distance",
		graphOptAuraByCol:       "kcore",
		graphOptDistanceFromCol: "hover",
	}, 1)
	require.Empty(t, o.Reasons)
	assert.Equal(t, graphviewLayoutRadial, o.Layout)
	assert.True(t, o.LayoutSet)
	assert.Equal(t, graphview.OrientationLeftRight, o.Orientation)
	assert.True(t, o.OrientationSet)
	assert.InDelta(t, 90, o.RingDist, 0.001)
	assert.InDelta(t, 70, o.RowDist, 0.001)
	assert.InDelta(t, 35, o.ColDist, 0.001)
	assert.InDelta(t, 3.5, o.KScale, 0.001)
	assert.InDelta(t, 0.4, o.Gravity, 0.001)
	assert.Equal(t, graphview.ForceModelNeighborEmbedding, o.ForceModel)
	assert.InDelta(t, 4, o.Exaggeration, 0.001)
	assert.True(t, o.HideEdges && o.HideEdgesSet)
	assert.True(t, o.Undirected && o.UndirectedSet)
	assert.True(t, o.PinOnDrag && o.PinOnDragSet)
	assert.Equal(t, "pagerank", o.SizeBy)
	assert.Equal(t, "component", o.ToneBy)
	assert.Equal(t, "-distance", o.OpacityBy)
	assert.Equal(t, "kcore", o.AuraBy)
	assert.Equal(t, graphviewSeedHover, o.DistanceFrom)
	assert.True(t, o.DistanceFromSet)
}

// Both spellings of the neighbour-embedding model resolve: the record writes
// one and the widget's own identifier the other.
func TestGraphOptsAcceptsBothModelSpellings(t *testing.T) {
	for _, s := range []string{"neighbour_embedding", "neighbor_embedding"} {
		o := readOpts(t, map[string]any{graphOptForceModelCol: s}, 1)
		require.Empty(t, o.Reasons, "%q was refused", s)
		assert.Equal(t, graphview.ForceModelNeighborEmbedding, o.ForceModel)
	}
}

// A value outside a column's vocabulary is refused with a reason and leaves
// the default in charge — a settings table must not be able to blank a
// drawing.
func TestGraphOptsRefusesUnknownValues(t *testing.T) {
	o := readOpts(t, map[string]any{
		graphOptLayoutCol:       "spiral",
		graphOptOrientationCol:  "sideways",
		graphOptForceModelCol:   "physics",
		graphOptDistanceFromCol: "wherever",
	}, 1)
	require.Len(t, o.Reasons, 4)
	assert.False(t, o.LayoutSet, "a refused layout leaves the default in charge")
	assert.False(t, o.OrientationSet)
	assert.False(t, o.ForceModelSet)
	assert.False(t, o.DistanceFromSet)
	note := o.statusNote()
	for _, want := range []string{"layout = spiral", "orientation = sideways",
		"force_model = physics", "distance_from = wherever"} {
		assert.Contains(t, note, want)
	}
}

// A column is claimed only when it can carry what it promises, the guard the
// rest of the contract already uses.
func TestGraphOptsClaimsOnTypeAsWellAsName(t *testing.T) {
	// `gravity` as text is a name collision, not a setting.
	rec := optsRec(t, map[string]any{graphOptGravityCol: "strong"}, 1)
	gc := resolveGraphOpts(rec.Schema())
	assert.Equal(t, -1, gc.gravityCol)
	assert.Zero(t, buildGraphOpts(rec, gc).Gravity)

	// A numeric flag is read as 0-or-not, which is what `1 AS undirected`
	// means.
	rec2 := optsRec(t, map[string]any{graphOptUndirectedCol: 1.0}, 1)
	gc2 := resolveGraphOpts(rec2.Schema())
	require.NotEqual(t, -1, gc2.undirectedCol)
	o := buildGraphOpts(rec2, gc2)
	assert.True(t, o.Undirected && o.UndirectedSet)

	// A non-positive quantity is unset, as it is in the widget.
	rec3 := optsRec(t, map[string]any{graphOptKScaleCol: 0.0, graphOptRingDistCol: -5.0}, 1)
	o3 := buildGraphOpts(rec3, resolveGraphOpts(rec3.Schema()))
	assert.Zero(t, o3.KScale)
	assert.Zero(t, o3.RingDist)
}

// A settings CTE returning many rows is a query bug worth saying out loud; the
// first row is still read.
func TestGraphOptsManyRowsIsNoted(t *testing.T) {
	o := readOpts(t, map[string]any{graphOptLayoutCol: "hierarchical"}, 3)
	assert.True(t, o.ExtraRows)
	assert.Equal(t, graphviewLayoutTree, o.Layout, "the first row is still the settings")
	assert.Contains(t, o.statusNote(), "more than one row")

	// An empty result is not an error: it says nothing.
	empty := optsRec(t, map[string]any{graphOptLayoutCol: "force"}, 1)
	o2 := buildGraphOpts(empty.NewSlice(0, 0), resolveGraphOpts(empty.Schema()))
	assert.False(t, o2.LayoutSet)
	assert.False(t, o2.ExtraRows)
}

// The layout vocabulary round-trips against the chrome's own spelling, so a
// reader reading the status line and a query writing the column agree.
func TestGraphviewLayoutVocabularyRoundTrips(t *testing.T) {
	for _, l := range []graphviewLayoutE{graphviewLayoutForce, graphviewLayoutGravity,
		graphviewLayoutTree, graphviewLayoutRadial, graphviewLayoutRandom} {
		got, ok := parseGraphviewLayout(l.String())
		require.True(t, ok, "%q does not parse", l.String())
		assert.Equal(t, l, got)
	}
	_, ok := parseGraphviewLayout("auto")
	assert.False(t, ok, "auto is the chrome's position, not a layout the query can name")
}

// ADR-0231 §SD1's precedence: the query sets the default, an explicit chrome
// setting overrides it, and the control's auto position means "whatever the
// query said".
func TestGraphOptsPrecedence(t *testing.T) {
	d := NewGraphviewDriver(nil, nil)

	// Nothing said anywhere: the panel's own default.
	assert.Equal(t, graphviewLayoutDefault, d.effectiveLayout())
	assert.False(t, d.effectivePinOnDrag(), "a drop does not stick unless asked")
	assert.Empty(t, d.effectiveSizeBy())

	// The query speaks and the chrome is on auto.
	d.opts = graphOpts{Layout: graphviewLayoutRadial, LayoutSet: true,
		PinOnDrag: true, PinOnDragSet: true, SizeBy: "pagerank"}
	assert.Equal(t, graphviewLayoutRadial, d.effectiveLayout())
	assert.True(t, d.effectivePinOnDrag())
	assert.Equal(t, "pagerank", d.effectiveSizeBy())

	// The reader overrides each one.
	d.layout = graphviewLayoutTree
	d.pinOnDrag, d.pinOnDragSet = false, true
	d.sizeBy, d.sizeBySet = "kcore", true
	assert.Equal(t, graphviewLayoutTree, d.effectiveLayout())
	assert.False(t, d.effectivePinOnDrag())
	assert.Equal(t, "kcore", d.effectiveSizeBy())

	// Back to auto hands the channel to the query again.
	d.layout = graphviewLayoutAuto
	d.sizeBySet = false
	assert.Equal(t, graphviewLayoutRadial, d.effectiveLayout())
	assert.Equal(t, "pagerank", d.effectiveSizeBy())

	// A chrome override of "weight" is NOT the same as auto: it is an
	// explicit choice of the contract's own column over the query's metric.
	d.sizeBy, d.sizeBySet = "", true
	assert.Empty(t, d.effectiveSizeBy(), "an explicit weight overrides the query's size_by")
}

// `undirected` reaches the metrics as well as the picture, which is what makes
// the numbers describe the graph that is drawn (§SD6).
func TestGraphOptsUndirectedReachesTheMetrics(t *testing.T) {
	m := gvModel(t, []string{"a", "b"}, []string{"b", "c"})
	d := NewGraphviewDriver(nil, nil)
	d.opts = graphOpts{Undirected: true, UndirectedSet: true}
	d.rebuild(&m)
	require.NotNil(t, d.metrics.g)
	assert.False(t, d.metrics.g.IsDirected(), "the metric graph is symmetrised")
	assert.True(t, d.metrics.undirected)
}
