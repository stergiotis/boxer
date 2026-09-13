package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Seam A, the per-row vocabulary of ADR-0231 §SD2.

// seamVerts builds a vertices record from column name → per-row values. A
// *float64 or *bool nil element is a NULL cell, which is how the contract
// spells "not declared for this row".
func seamVerts(t *testing.T, ids []string, cells map[string]any) arrow.RecordBatch {
	t.Helper()
	pool := memory.NewGoAllocator()
	fields := []arrow.Field{{Name: networkIDCol, Type: arrow.BinaryTypes.String}}
	idb := array.NewStringBuilder(pool)
	for _, id := range ids {
		idb.Append(id)
	}
	cols := []arrow.Array{idb.NewArray()}
	names := make([]string, 0, len(cells))
	for n := range cells {
		names = append(names, n)
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		switch v := cells[name].(type) {
		case []*float64:
			b := array.NewFloat64Builder(pool)
			for _, e := range v {
				if e == nil {
					b.AppendNull()
				} else {
					b.Append(*e)
				}
			}
			fields = append(fields, arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Float64, Nullable: true})
			cols = append(cols, b.NewArray())
		case []*bool:
			b := array.NewBooleanBuilder(pool)
			for _, e := range v {
				if e == nil {
					b.AppendNull()
				} else {
					b.Append(*e)
				}
			}
			fields = append(fields, arrow.Field{Name: name, Type: arrow.FixedWidthTypes.Boolean, Nullable: true})
			cols = append(cols, b.NewArray())
		case [][]string:
			b := array.NewListBuilder(pool, arrow.BinaryTypes.String)
			sb := b.ValueBuilder().(*array.StringBuilder)
			for _, e := range v {
				if e == nil {
					b.AppendNull()
					continue
				}
				b.Append(true)
				for _, s := range e {
					sb.Append(s)
				}
			}
			fields = append(fields, arrow.Field{Name: name,
				Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true})
			cols = append(cols, b.NewArray())
		default:
			t.Fatalf("unhandled cell type for %q", name)
		}
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(ids)))
	t.Cleanup(rec.Release)
	return rec
}

func f64p(v float64) *float64 { return &v }
func boolp(v bool) *bool      { return &v }

// seamModel builds a model over a vertices record and a one-edge list.
func seamModel(t *testing.T, vr arrow.RecordBatch) netModel {
	t.Helper()
	er := netEdges(t, []string{"a"}, []string{"b"}, nil)
	t.Cleanup(er.Release)
	ec, reason := resolveNetworkEdges(er.Schema())
	require.Empty(t, reason)
	vc, reason := resolveNetworkVertices(vr.Schema())
	require.Empty(t, reason)
	return buildNetModel(er, ec, vr, vc, netCaps{vertices: 100, edges: 100})
}

// The rule the guard did not have to state before: a NULL cell is "not
// declared for this row", and BECAUSE null is the unset value a declared zero
// is a zero — which the row-shaped spec could not say.
func TestSeamANullIsUnsetAndZeroIsZero(t *testing.T) {
	vr := seamVerts(t, []string{"a", "b", "c"}, map[string]any{
		networkOpacityCol: []*float64{f64p(0), f64p(0.5), nil},
		networkRadiusCol:  []*float64{nil, f64p(0), f64p(12)},
	})
	m := seamModel(t, vr)

	zero := netRowOf(t, &m, "a")
	half := netRowOf(t, &m, "b")
	null := netRowOf(t, &m, "c")

	assert.Equal(t, float32(0), m.Opacity[zero], "a declared zero opacity paints nothing")
	assert.InDelta(t, 0.5, m.Opacity[half], 0.001)
	assert.True(t, math.IsNaN(float64(m.Opacity[null])), "a NULL cell is not declared for that row")

	assert.True(t, math.IsNaN(float64(m.Radius[zero])))
	assert.Equal(t, float32(0), m.Radius[half], "a declared zero radius is a node that is only its label")
	assert.InDelta(t, 12, m.Radius[null], 0.001)
}

// `pick` is the positive spelling and the widget takes the negative, so the
// claim inverts exactly once; absent and NULL are both pickable.
func TestSeamAPickInvertsOnce(t *testing.T) {
	vr := seamVerts(t, []string{"a", "b", "c"}, map[string]any{
		networkPickCol: []*bool{boolp(false), boolp(true), nil},
	})
	m := seamModel(t, vr)
	assert.True(t, m.NoPick[netRowOf(t, &m, "a")], "pick=false takes it out of the pointer's reach")
	assert.False(t, m.NoPick[netRowOf(t, &m, "b")])
	assert.False(t, m.NoPick[netRowOf(t, &m, "c")], "a NULL cell is pickable")

	// No column at all is pickable everywhere.
	plain := seamVerts(t, []string{"a"}, nil)
	m2 := seamModel(t, plain)
	assert.False(t, m2.NoPick[netRowOf(t, &m2, "a")])
}

// Naming an axis turns the pull on, and the per-axis strength wins over the
// shared one; an axis with no strength still pulls, on CenterGravity's scale.
func TestSeamAPullResolution(t *testing.T) {
	vr := seamVerts(t, []string{"a", "b"}, map[string]any{
		networkPullXCol:  []*float64{f64p(100), f64p(-40)},
		networkPullYCol:  []*float64{nil, f64p(10)},
		networkPullSCol:  []*float64{nil, f64p(0.9)},
		networkPullSXCol: []*float64{nil, f64p(0.2)},
	})
	m := seamModel(t, vr)
	a, b := netRowOf(t, &m, "a"), netRowOf(t, &m, "b")

	assert.InDelta(t, 100, m.PullX[a], 0.001)
	assert.True(t, math.IsNaN(float64(m.PullY[a])), "an axis the query did not name pulls nothing")
	assert.InDelta(t, networkDefaultPullStrength, m.PullSX[a], 0.001,
		"a target named without a strength still pulls")

	assert.InDelta(t, 0.2, m.PullSX[b], 0.001, "the per-axis strength wins")
	assert.InDelta(t, 0.9, m.PullSY[b], 0.001, "the shared strength serves the axis without its own")
}

// `groups` declares aura membership as a set; `group` alone is the single
// aura it always was, and both feed one resolved column.
func TestSeamAGroupsAreAuraMembership(t *testing.T) {
	vr := seamVerts(t, []string{"a", "b", "c"}, map[string]any{
		networkGroupsCol: [][]string{{"x", "y"}, nil, {}},
	})
	m := seamModel(t, vr)
	require.True(t, m.GroupsDeclared)
	auraOf := func(id string) []string {
		i := netRowOf(t, &m, id)
		return m.AuraValues[m.AuraStart[i]:m.AuraStart[i+1]]
	}
	assert.Equal(t, []string{"x", "y"}, auraOf("a"), "a node may belong to two auras")
	assert.Empty(t, auraOf("b"), "a NULL set is no membership")
	assert.Empty(t, auraOf("c"))
	// Both names claimed a palette position, in declaration order.
	assert.Equal(t, []string{"x", "y"}, m.groups())

	// Without `groups`, `group` is still the single aura (§SD4).
	vr2 := netVerts(t, []string{"a", "b"}, nil, []string{"one", ""}, nil)
	defer vr2.Release()
	m2 := seamModel(t, vr2)
	assert.False(t, m2.GroupsDeclared)
	i := netRowOf(t, &m2, "a")
	assert.Equal(t, []string{"one"}, m2.AuraValues[m2.AuraStart[i]:m2.AuraStart[i+1]])
}

// A scalar column that happens to be called `groups` is a name collision, not
// a membership — the guard the rest of the contract already uses.
func TestSeamAClaimsSetColumnsOnType(t *testing.T) {
	vr := seamVerts(t, []string{"a"}, map[string]any{
		networkGroupsCol: []*float64{f64p(1)},
	})
	vc, reason := resolveNetworkVertices(vr.Schema())
	require.Empty(t, reason)
	assert.Equal(t, -1, vc.groupsCol)
}

// An edge `id` tells parallel edges apart, which is what keeps two edges of
// one ordered pair from collapsing into one.
func TestSeamAEdgeIdSeparatesParallelEdges(t *testing.T) {
	er := netEdgesWithID(t, []string{"a", "a", "a"}, []string{"b", "b", "b"},
		[]string{"first", "second", "first"})
	defer er.Release()
	ec, reason := resolveNetworkEdges(er.Schema())
	require.Empty(t, reason)
	require.NotEqual(t, -1, ec.idCol)
	m := buildNetModel(er, ec, nil, noVerticesClaim(), netCaps{vertices: 100, edges: 100})
	require.Equal(t, 2, m.NumEdges(), "distinct ids are distinct edges; the repeat collapses")
	assert.NotEqual(t, m.EdgeID[0], m.EdgeID[1])

	// Without the column every repeat of a pair still collapses.
	plain := netEdges(t, []string{"a", "a"}, []string{"b", "b"}, nil)
	defer plain.Release()
	pc, _ := resolveNetworkEdges(plain.Schema())
	m2 := buildNetModel(plain, pc, nil, noVerticesClaim(), netCaps{vertices: 100, edges: 100})
	assert.Equal(t, 1, m2.NumEdges())
}

// A located vertex is pinned in world units measured from the located set's
// centroid, and the projection round-trips so a gesture reads back in degrees.
func TestSeamALatLonBecomesAPin(t *testing.T) {
	vr := seamVerts(t, []string{"zurich", "oslo", "nowhere"}, map[string]any{
		networkLatCol: []*float64{f64p(47.37), f64p(59.91), nil},
		networkLonCol: []*float64{f64p(8.54), f64p(10.75), nil},
	})
	m := seamModel(t, vr)
	require.True(t, m.Located)

	zh, os := netRowOf(t, &m, "zurich"), netRowOf(t, &m, "oslo")
	no := netRowOf(t, &m, "nowhere")
	assert.True(t, math.IsNaN(float64(m.PinX[no])), "an unlocated vertex is left to the layout")

	// Oslo is north of Zurich, and y grows downward in the projection.
	assert.Less(t, m.PinY[os], m.PinY[zh], "the more northern vertex has the smaller y")
	assert.Greater(t, m.PinX[os], m.PinX[zh], "and the more eastern the larger x")

	// The origin is the centroid, so the pins straddle it.
	assert.InDelta(t, 0, float64(m.PinX[zh]+m.PinX[os]), 0.001)
	assert.InDelta(t, 0, float64(m.PinY[zh]+m.PinY[os]), 0.001)

	// And the projection inverts, which is what §SD3's read-back needs.
	lat, lon := netUnprojectWebMercator(float64(m.PinX[zh])+m.GeoOriginX, float64(m.PinY[zh])+m.GeoOriginY)
	assert.InDelta(t, 47.37, lat, 0.0001)
	assert.InDelta(t, 8.54, lon, 0.0001)
}

// A `lat`/`lon` pin wins over a `pin_x`/`pin_y` one: the geographic placement
// is the more specific claim.
func TestSeamALatLonWinsOverPinXY(t *testing.T) {
	vr := seamVerts(t, []string{"a"}, map[string]any{
		networkLatCol:  []*float64{f64p(47)},
		networkLonCol:  []*float64{f64p(8)},
		networkPinXCol: []*float64{f64p(999)},
		networkPinYCol: []*float64{f64p(999)},
	})
	m := seamModel(t, vr)
	assert.NotEqual(t, float32(999), m.PinX[netRowOf(t, &m, "a")])
}

// One coordinate without the other is not a location: both are required.
func TestSeamALatWithoutLonIsNotLocated(t *testing.T) {
	vr := seamVerts(t, []string{"a"}, map[string]any{
		networkLatCol: []*float64{f64p(47)},
	})
	m := seamModel(t, vr)
	assert.False(t, m.Located)
	assert.True(t, math.IsNaN(float64(m.PinX[netRowOf(t, &m, "a")])))
}

// Every Seam A column survives the id sort attached to its own vertex — the
// bug a per-column permutation would have hidden, since every column would
// still be the right length.
func TestSeamAColumnsFollowTheirVertexThroughTheSort(t *testing.T) {
	ids := []string{"delta", "alpha", "charlie", "bravo"}
	vr := seamVerts(t, ids, map[string]any{
		networkOpacityCol: []*float64{f64p(0.1), f64p(0.2), f64p(0.3), f64p(0.4)},
		networkRadiusCol:  []*float64{f64p(1), f64p(2), f64p(3), f64p(4)},
		networkPinXCol:    []*float64{f64p(10), f64p(20), f64p(30), f64p(40)},
		networkPinYCol:    []*float64{f64p(11), f64p(21), f64p(31), f64p(41)},
		networkPullXCol:   []*float64{f64p(-1), f64p(-2), f64p(-3), f64p(-4)},
		networkCenterCol:  []*bool{boolp(true), boolp(false), boolp(false), boolp(true)},
		networkGroupsCol:  [][]string{{"d"}, {"a"}, {"c"}, {"b"}},
	})
	m := seamModel(t, vr)
	want := map[string]struct {
		opacity, radius, pinX, pinY, pullX float32
		center                             bool
		aura                               string
	}{
		"delta":   {0.1, 1, 10, 11, -1, true, "d"},
		"alpha":   {0.2, 2, 20, 21, -2, false, "a"},
		"charlie": {0.3, 3, 30, 31, -3, false, "c"},
		"bravo":   {0.4, 4, 40, 41, -4, true, "b"},
	}
	for id, w := range want {
		i := netRowOf(t, &m, id)
		assert.InDelta(t, w.opacity, m.Opacity[i], 0.001, "%s opacity", id)
		assert.InDelta(t, w.radius, m.Radius[i], 0.001, "%s radius", id)
		assert.InDelta(t, w.pinX, m.PinX[i], 0.001, "%s pin_x", id)
		assert.InDelta(t, w.pinY, m.PinY[i], 0.001, "%s pin_y", id)
		assert.InDelta(t, w.pullX, m.PullX[i], 0.001, "%s pull_x", id)
		assert.Equal(t, w.center, m.Center[i], "%s center", id)
		assert.Equal(t, []string{w.aura}, m.AuraValues[m.AuraStart[i]:m.AuraStart[i+1]], "%s groups", id)
	}
}

// The declaration hands Seam A's columns to the widget as they stand, and an
// absolute `radius` wins over the size channel's share.
func TestSeamAReachesTheDeclaration(t *testing.T) {
	vr := seamVerts(t, []string{"a", "b"}, map[string]any{
		networkOpacityCol:     []*float64{f64p(0.25), nil},
		networkRadiusCol:      []*float64{f64p(7), nil},
		networkPickCol:        []*bool{boolp(false), boolp(true)},
		networkLabelAlwaysCol: []*bool{boolp(true), nil},
		networkPinXCol:        []*float64{f64p(5), nil},
		networkPinYCol:        []*float64{f64p(6), nil},
	})
	m := seamModel(t, vr)
	d := NewGraphviewDriver(nil, nil)
	d.rebuild(&m)
	require.NoError(t, d.nodes.Validate())

	i := netRowOf(t, &m, "a")
	assert.InDelta(t, 0.25, d.nodes.Opacity[i], 0.001)
	assert.InDelta(t, 7, d.nodes.Radius[i], 0.001, "an absolute radius wins over the share")
	assert.True(t, d.nodes.NoPick[i])
	assert.True(t, d.nodes.LabelAlways[i])
	assert.InDelta(t, 5, d.nodes.PinX[i], 0.001)
	assert.InDelta(t, 6, d.nodes.PinY[i], 0.001)

	j := netRowOf(t, &m, "b")
	assert.True(t, math.IsNaN(float64(d.nodes.PinX[j])), "an undeclared pin leaves the node free")
	assert.False(t, d.nodes.NoPick[j])
}

// netEdgesWithID is netEdges plus the `id` column of §SD2.
func netEdgesWithID(t *testing.T, src, tgt, ids []string) arrow.RecordBatch {
	t.Helper()
	pool := memory.NewGoAllocator()
	build := func(vs []string) arrow.Array {
		b := array.NewStringBuilder(pool)
		for _, v := range vs {
			b.Append(v)
		}
		return b.NewArray()
	}
	fields := []arrow.Field{
		{Name: networkSourceCol, Type: arrow.BinaryTypes.String},
		{Name: networkTargetCol, Type: arrow.BinaryTypes.String},
		{Name: networkEdgeIDCol, Type: arrow.BinaryTypes.String},
	}
	cols := []arrow.Array{build(src), build(tgt), build(ids)}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(src)))
}
