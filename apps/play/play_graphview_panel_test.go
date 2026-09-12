package play

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

// ADR-0225 §Verification: the parts of the Graphview panel that are pure — the
// id intern and its collision probe (§SD7), the group→aura mapping (§SD4), the
// magnitude→geometry mapping and the donut column (§SD5), the label budget
// (§SD10) and the declaration cache's key (§SD8). The gestures and the painted
// result are verified interactively, as ADR-0224 records.

// netDonutVerts builds a vertices record carrying id + a list-of-double
// `donut` (+ a scalar `donut_total` when non-nil).
func netDonutVerts(t *testing.T, id []string, donut [][]float64, total []float64) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	lb := array.NewListBuilder(alloc, arrow.PrimitiveTypes.Float64)
	defer lb.Release()
	vb := lb.ValueBuilder().(*array.Float64Builder)
	for _, row := range donut {
		if row == nil {
			lb.AppendNull()
			continue
		}
		lb.Append(true)
		vb.AppendValues(row, nil)
	}
	fields := []arrow.Field{strField("id"), {Name: "donut", Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)}}
	cols := []arrow.Array{netStrArr(t, id), lb.NewListArray()}
	if total != nil {
		tb := array.NewFloat64Builder(alloc)
		defer tb.Release()
		tb.AppendValues(total, nil)
		fields = append(fields, f64Field("donut_total"))
		cols = append(cols, tb.NewFloat64Array())
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(id)))
}

// gvBuild is the Graphview tab's whole mapping, as render performs it.
func gvBuild(t *testing.T, edgesRec arrow.RecordBatch, vertRec arrow.RecordBatch) *GraphviewDriver {
	t.Helper()
	ec, reason := resolveNetworkEdges(edgesRec.Schema())
	require.Empty(t, reason)
	vc := noVerts()
	if vertRec != nil {
		var r string
		vc, r = resolveNetworkVertices(vertRec.Schema())
		require.Empty(t, r)
	}
	d := NewGraphviewDriver(nil, nil)
	m := buildNetModel(edgesRec, ec, vertRec, vc,
		netCaps{vertices: graphviewMaxVertices, edges: graphviewMaxEdges})
	d.rebuild(&m)
	return d
}

func gvNodeByName(d *GraphviewDriver, id string) (graphview.NodeSpec, bool) {
	for _, n := range d.nodes {
		if d.names.name(n.Id) == id {
			return n, true
		}
	}
	return graphview.NodeSpec{}, false
}

// The declared string id is what the widget's uint64 key stands for, and what
// a click publishes — round-tripping through the reverse map, not through the
// hash (§SD7).
func TestGraphviewIdsRoundTrip(t *testing.T) {
	er := netEdges(t, []string{"a", "b"}, []string{"b", "c"}, nil)
	d := gvBuild(t, er, nil)

	require.Len(t, d.nodes, 3)
	for _, n := range d.nodes {
		assert.NotEmpty(t, d.names.name(n.Id), "every declared node id names a vertex")
	}
	seen := make(map[uint64]struct{}, len(d.nodes))
	for _, n := range d.nodes {
		_, dup := seen[n.Id]
		require.False(t, dup, "interned ids are unique — the widget keys nodes by them")
		seen[n.Id] = struct{}{}
	}
	// The edges reference the same keys, not a second interning.
	for _, e := range d.edges {
		assert.Contains(t, seen, e.From)
		assert.Contains(t, seen, e.To)
	}
	// Interning is a function of the string: the same id twice is one key.
	assert.Equal(t, d.names.intern("a"), d.names.intern("a"))
}

// Two strings landing on one hash must stay two vertices. The probe is what
// makes that true, so it is asserted directly rather than waited for.
func TestGraphviewIdCollisionProbes(t *testing.T) {
	ids := newNetIds(2)
	// Seat "a" on the slot "b" hashes to, which is what a collision looks
	// like from the second id's side.
	slot := xxh3.HashString("b")
	ids.to["a"], ids.from[slot] = slot, "a"

	second := ids.intern("b")
	assert.Equal(t, slot+1, second, "the probe steps to the next free slot")
	assert.Equal(t, "a", ids.name(slot), "the first id keeps the slot it holds")
	assert.Equal(t, "b", ids.name(second), "and both are still reachable by name")
}

// `group` names an aura AND colours the node (§SD4); a vertex with no group
// names none, and the group order is the order the vertices declared them.
func TestGraphviewGroupBecomesAura(t *testing.T) {
	vr := netVerts(t, []string{"a", "b", "c"}, nil, []string{"two", "one", ""}, nil)
	er := netEdges(t, []string{"a"}, []string{"b"}, nil)
	d := gvBuild(t, er, vr)

	a, ok := gvNodeByName(d, "a")
	require.True(t, ok)
	assert.Equal(t, []string{"two"}, a.Auras)
	b, _ := gvNodeByName(d, "b")
	assert.Equal(t, []string{"one"}, b.Auras)
	c, _ := gvNodeByName(d, "c")
	assert.Nil(t, c.Auras, "a vertex with no group belongs to no aura")

	assert.Equal(t, []string{"two", "one"}, d.groups,
		"groups are listed in the order the vertices first named them, not sorted")
	assert.NotEqual(t, a.Color, b.Color, "distinct groups take distinct palette positions")
}

// A toned vertex still joins its group's aura: the tone claims the FILL, not
// the membership.
func TestGraphviewTonedVertexKeepsItsAura(t *testing.T) {
	vr := netTonedVerts(t, []string{"a", "b"}, []string{"x", "x"}, []string{"error", ""})
	er := netEdges(t, []string{"a"}, []string{"b"}, nil)
	d := gvBuild(t, er, vr)

	a, _ := gvNodeByName(d, "a")
	b, _ := gvNodeByName(d, "b")
	assert.Equal(t, []string{"x"}, a.Auras)
	assert.Equal(t, []string{"x"}, b.Auras)
	want, _ := networkTone("error", false)
	assert.Equal(t, want, a.Color, "the tone still wins the fill")
	assert.NotEqual(t, a.Color, b.Color)
}

// A vertex `weight` sizes the node and an edge `weight` widens the stroke,
// both by the square root of the share (§SD5); an absent or non-positive
// weight leaves the widget's default (zero).
func TestGraphviewWeightSizesNodeAndEdge(t *testing.T) {
	vr := netWeightedVerts(t, []string{"a", "b", "c"}, []float64{100, 25, 0})
	er := netWeightedEdges(t, []string{"a", "b"}, []string{"b", "c"}, []float64{16, 4}, nil)
	d := gvBuild(t, er, vr)

	a, _ := gvNodeByName(d, "a")
	b, _ := gvNodeByName(d, "b")
	c, _ := gvNodeByName(d, "c")
	assert.InDelta(t, graphviewMaxRadius, a.Radius, 0.01, "the heaviest vertex takes the ceiling")
	assert.Greater(t, a.Radius, b.Radius)
	assert.Greater(t, b.Radius, float32(0))
	assert.Zero(t, c.Radius, "an unweighted vertex takes the style default")

	require.Len(t, d.edges, 2)
	assert.InDelta(t, graphviewMaxEdgeW, d.edges[0].Width, 0.01)
	assert.Greater(t, d.edges[0].Width, d.edges[1].Width)

	// The ramp and the width are read at the same normalised position, so an
	// unweighted result colours nothing.
	plain := netEdges(t, []string{"a"}, []string{"b"}, nil)
	pd := gvBuild(t, plain, nil)
	assert.Zero(t, pd.edges[0].Width, "no weight column leaves the style default")
}

// An explicit `tone` wins an edge's colour over the magnitude ramp, and does
// not touch the width — the two claims compose (ADR-0167 §SD4).
func TestGraphviewEdgeToneWinsColourNotWidth(t *testing.T) {
	er := netWeightedEdges(t, []string{"a", "b"}, []string{"b", "c"},
		[]float64{9, 1}, []string{"error", ""})
	d := gvBuild(t, er, nil)

	require.Len(t, d.edges, 2)
	want, _ := networkTone("error", true)
	assert.Equal(t, want, d.edges[0].Color)
	assert.Greater(t, d.edges[0].Width, d.edges[1].Width, "the toned edge still carries its magnitude")
}

// The `donut` column is claimed only when it is a list of numbers, and its
// slices reach the node in declaration order (§SD5).
func TestGraphviewDonutColumn(t *testing.T) {
	vr := netDonutVerts(t, []string{"a", "b"},
		[][]float64{{3, 1}, nil}, []float64{8, 0})
	er := netEdges(t, []string{"a"}, []string{"b"}, nil)
	d := gvBuild(t, er, vr)

	a, _ := gvNodeByName(d, "a")
	assert.Equal(t, []float32{3, 1}, a.Donut.Values)
	assert.Equal(t, float32(8), a.Donut.Total, "a total past the sum leaves a muted remainder")
	b, _ := gvNodeByName(d, "b")
	assert.True(t, b.Donut.IsEmpty(), "a null cell draws no ring")

	// A scalar column of the same name is a name collision, not a ring.
	scalar := netWeightedVerts(t, []string{"a"}, []float64{1})
	scalar = renameNetField(t, scalar, "weight", "donut")
	vc, reason := resolveNetworkVertices(scalar.Schema())
	require.Empty(t, reason)
	assert.Equal(t, -1, vc.donutCol, "a scalar `donut` stays an ordinary result column")
}

// renameNetField rebuilds a record with one field renamed — for asserting that
// a claim turns on the TYPE and not only on the name.
func renameNetField(t *testing.T, rec arrow.RecordBatch, from, to string) arrow.RecordBatch {
	t.Helper()
	fields := make([]arrow.Field, 0, rec.Schema().NumFields())
	cols := make([]arrow.Array, 0, rec.Schema().NumFields())
	for i, f := range rec.Schema().Fields() {
		if f.Name == from {
			f.Name = to
		}
		fields = append(fields, f)
		cols = append(cols, rec.Column(i))
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
}

// Labels are drawn for every node up to the budget and follow hover above it —
// a rule read off the model, so one result always draws the same way (§SD10).
func TestGraphviewLabelBudget(t *testing.T) {
	small := netEdges(t, []string{"a"}, []string{"b"}, nil)
	assert.True(t, gvBuild(t, small, nil).labeled)

	n := graphviewLabelBudget + 1
	src := make([]string, 0, n)
	tgt := make([]string, 0, n)
	for i := range n {
		src = append(src, fmt.Sprintf("v%d", i))
		tgt = append(tgt, fmt.Sprintf("v%d", (i+1)%n))
	}
	big := netEdges(t, src, tgt, nil)
	d := gvBuild(t, big, nil)
	require.Greater(t, len(d.nodes), graphviewLabelBudget)
	assert.False(t, d.labeled, "past the budget the labels follow hover and selection")
}

// The caps are this panel's own, not the layered tab's (§SD9): a result the
// Network tab caps at 400 vertices draws whole here.
func TestGraphviewCapsAreItsOwn(t *testing.T) {
	n := networkMaxVertices + 50
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("v%d", i)
	}
	vr := netVerts(t, ids, nil, nil, nil)
	er := netEdges(t, []string{}, []string{}, nil)
	d := gvBuild(t, er, vr)
	assert.Len(t, d.nodes, n)
	assert.False(t, d.capped)
}

// The declaration is rebuilt when the lanes' fingerprints change and reused
// otherwise, so a running simulation formats no cells (§SD8).
func TestGraphviewModelKeyTracksFingerprints(t *testing.T) {
	ec, _ := resolveNetworkEdges(netEdges(t, nil, nil, nil).Schema())
	k := func(fp uint64, have bool) graphviewModelKey {
		return graphviewModelKey{edgesFP: fp, haveEdges: have, ec: ec, vc: noVerts()}
	}
	assert.Equal(t, k(7, true), k(7, true), "the same served result is the same key")
	assert.NotEqual(t, k(7, true), k(8, true), "new bytes rebuild")
	assert.NotEqual(t, k(0, true), k(0, false),
		"a zero fingerprint is not the same state as nothing served")
}

// A selection the new declaration no longer carries is cleared and the clear is
// published — the widget drops it silently, having no id left to report.
func TestGraphviewSelectionClearedWhenVertexVanishes(t *testing.T) {
	d := gvBuild(t, netEdges(t, []string{"a"}, []string{"b"}, nil), nil)
	d.selectedID = "a"

	e := &emitProbe{}
	assert.False(t, d.pruneSelection(e), "a vertex still in the graph keeps its selection")
	assert.Equal(t, "a", d.selectedID)
	assert.Empty(t, e.ids)

	// A re-Run whose graph no longer names it.
	ec, _ := resolveNetworkEdges(netEdges(t, nil, nil, nil).Schema())
	next := netEdges(t, []string{"c"}, []string{"d"}, nil)
	m := buildNetModel(next, ec, nil, noVerts(),
		netCaps{vertices: graphviewMaxVertices, edges: graphviewMaxEdges})
	d.rebuild(&m)

	require.True(t, d.pruneSelection(e))
	assert.Empty(t, d.selectedID)
	require.Equal(t, []SignalID{signalSelectionKey}, e.ids)
	assert.Equal(t, "", e.vals[0], "the empty string is the honest \"nothing focused\" value")
}
