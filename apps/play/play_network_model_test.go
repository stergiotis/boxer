package play

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The contract's own rules, where the two graph panels share them (ADR-0227
// §SD1). The mapping tests that go through the layered adapter live beside that
// panel; these are the ones about the neutral model itself.

// Every named `group` takes a palette position, whether or not the vertex that
// named it also carried a tone that wins the fill. A group's identity is not a
// property of whether one of its members meant something else as well — and
// without this the live panel would have a group with no aura colour.
func TestNetGroupIndexIsComplete(t *testing.T) {
	vr := netTonedVerts(t, []string{"a", "b"}, []string{"x", "y"}, []string{"error", ""})
	er := netEdges(t, []string{"a"}, []string{"b"}, nil)
	ec, _ := resolveNetworkEdges(er.Schema())
	vc, reason := resolveNetworkVertices(vr.Schema())
	require.Empty(t, reason)

	m := buildNetModel(er, ec, vr, vc, netCaps{vertices: networkMaxVertices, edges: networkMaxEdges})
	assert.Equal(t, map[string]int{"x": 0, "y": 1}, m.groupIdx,
		"the toned vertex's group still claims its position, in declaration order")
	assert.Equal(t, []string{"x", "y"}, m.groups())

	require.Equal(t, 2, m.NumVertices())
	// The model's rows are sorted by interned id (ADR-0232 §SD9), so the
	// vertex is found by its declared id rather than by the position it was
	// declared at.
	col, ok := m.vertexFill(netRowOf(t, &m, "b"))
	require.True(t, ok)
	assert.Equal(t, networkGroupColor(1), col, "the untoned vertex takes its own group's colour")
}

// netRowOf is the model row carrying a declared id; the columns are in
// interned-id order, which is not the order the query declared them in.
func netRowOf(t *testing.T, m *netModel, id string) int {
	t.Helper()
	for i := range m.ID {
		if m.ID[i] == id {
			return i
		}
	}
	t.Fatalf("no model row for %q", id)
	return -1
}

// A donut cell keeps its zeros: a slice's position is what pairs it with a
// colour, so dropping an empty share would rotate every share after it.
func TestNetDonutAtKeepsPositions(t *testing.T) {
	vr := netDonutVerts(t, []string{"a"}, [][]float64{{2, 0, 5}}, nil)
	defer vr.Release()
	got, ok := netDonutAt(vr.Column(1), 0, nil)
	require.True(t, ok)
	assert.Equal(t, []float32{2, 0, 5}, got)

	// A column that is not a list answers no rather than guessing.
	_, ok = netDonutAt(vr.Column(0), 0, nil)
	assert.False(t, ok)
}

// The model's rows are sorted by interned id (ADR-0232 §SD9): that order is
// the CSR's slot order and the widget's, which is what lets a metric column
// index all three without a join. The columns must stay parallel through the
// sort — including the ragged donut, whose slices cannot be swapped
// elementwise and are the one place a permutation bug would hide.
func TestNetModelSortsByInternedIdKeepingColumnsParallel(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	rings := [][]float64{{1}, {2, 2}, nil, {4, 4, 4}}
	vr := netDonutVerts(t, ids, rings, nil)
	defer vr.Release()
	er := netEdges(t, []string{"a"}, []string{"d"}, nil)
	defer er.Release()
	ec, _ := resolveNetworkEdges(er.Schema())
	vc, reason := resolveNetworkVertices(vr.Schema())
	require.Empty(t, reason)

	m := buildNetModel(er, ec, vr, vc, netCaps{vertices: networkMaxVertices, edges: networkMaxEdges})
	require.Equal(t, 4, m.NumVertices())
	require.True(t, slices.IsSorted(m.Key), "the vertex columns are in ascending interned-id order")

	// Every column still describes the vertex its row names, whatever the
	// sort did to the declaration order.
	want := map[string][]float32{"a": {1}, "b": {2, 2}, "c": {}, "d": {4, 4, 4}}
	for i := range m.ID {
		id := m.ID[i]
		require.Equal(t, m.names.intern(id), m.Key[i], "row %d: key and id disagree", i)
		require.Equal(t, id, m.Label[i], "row %d: an undeclared label is its id", i)
		got := m.donutAt(i)
		require.Equal(t, want[id], []float32(got), "row %d (%s): the ring followed the wrong vertex", i, id)
	}

	// Edges address vertices by key, so the sort leaves them alone.
	require.Equal(t, 1, m.NumEdges())
	assert.Equal(t, m.names.intern("a"), m.From[0])
	assert.Equal(t, m.names.intern("d"), m.To[0])
}

// A model whose vertices carry no ring keeps no offsets slice at all, so the
// common case hands the widget a nil donut column rather than a run of zeros.
func TestNetModelWithoutDonutsCarriesNoOffsets(t *testing.T) {
	er := netEdges(t, []string{"a", "b"}, []string{"b", "c"}, nil)
	defer er.Release()
	ec, _ := resolveNetworkEdges(er.Schema())
	m := buildNetModel(er, ec, nil, noVerticesClaim(), netCaps{vertices: 100, edges: 100})
	require.Equal(t, 3, m.NumVertices())
	assert.Nil(t, m.DonutStart)
	assert.Nil(t, m.donutAt(0))
}
