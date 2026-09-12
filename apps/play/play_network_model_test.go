package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The contract's own rules, where the two graph panels share them (ADR-0225
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

	require.Len(t, m.Vertices, 2)
	col, ok := m.vertexFill(m.Vertices[1])
	require.True(t, ok)
	assert.Equal(t, networkGroupColor(1), col, "the untoned vertex takes its own group's colour")
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
