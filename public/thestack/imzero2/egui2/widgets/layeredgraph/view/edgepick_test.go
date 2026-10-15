package view

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

func identityTf(p layeredgraph.Point) (float32, float32) { return float32(p.X), float32(p.Y) }

// straightEdge is a Graphviz-shaped spline (one cubic) from (x0,y) to (x1,y),
// with its head tip 10 px further on.
func straightEdge(id string, x0, x1, y float64) layeredgraph.EdgeLayout {
	d := (x1 - x0) / 3
	tip := layeredgraph.Point{X: x1 + 10, Y: y}
	return layeredgraph.EdgeLayout{ID: id, ArrowHead: &tip, Points: []layeredgraph.Point{
		{X: x0, Y: y}, {X: x0 + d, Y: y}, {X: x0 + 2*d, Y: y}, {X: x1, Y: y}}}
}

func pick(t *testing.T, edges []layeredgraph.EdgeLayout, strokeW float32, x, y float32) int {
	t.Helper()
	boxes := make([]edgeHitBox, len(edges))
	ok := make([]bool, len(edges))
	for i := range edges {
		boxes[i], ok[i] = newEdgeHitBox(&edges[i], identityTf, strokeW)
	}
	return pickEdge(edges, boxes, ok, identityTf, x, y)
}

func TestEdgeIsPickedNearItsStroke(t *testing.T) {
	edges := []layeredgraph.EdgeLayout{straightEdge("e", 0, 100, 50)}
	require.Equal(t, 0, pick(t, edges, 1.25, 50, 50), "on the line")
	require.Equal(t, 0, pick(t, edges, 1.25, 50, 53.9), "within the 4 px tolerance")
	require.Equal(t, -1, pick(t, edges, 1.25, 50, 60), "10 px off")
	require.Equal(t, 0, pick(t, edges, 1.25, 105, 51), "on the head's shaft past the route's end")
	require.Equal(t, -1, pick(t, edges, 1.25, 150, 50), "beyond the head")
}

// A wide stroke is hit across its width, not only within the minimum.
func TestWideEdgeTolerance(t *testing.T) {
	edges := []layeredgraph.EdgeLayout{straightEdge("e", 0, 100, 50)}
	require.Equal(t, -1, pick(t, edges, 1, 50, 57))
	require.Equal(t, 0, pick(t, edges, 8, 50, 57))
}

// Of two parallel edges the nearer wins.
func TestNearerParallelEdgeWins(t *testing.T) {
	edges := []layeredgraph.EdgeLayout{straightEdge("upper", 0, 100, 50), straightEdge("lower", 0, 100, 56)}
	require.Equal(t, 0, pick(t, edges, 1.25, 50, 52))
	require.Equal(t, 1, pick(t, edges, 1.25, 50, 54))
}

// A curved route is tested along the curve, not its chord.
func TestCurvedEdgeFollowsTheCurve(t *testing.T) {
	arc := layeredgraph.EdgeLayout{ID: "arc", Points: []layeredgraph.Point{
		{X: 0, Y: 0}, {X: 0, Y: 100}, {X: 100, Y: 100}, {X: 100, Y: 0}}}
	// The cubic's midpoint is (50, 75); its chord is y = 0.
	require.Equal(t, 0, pick(t, []layeredgraph.EdgeLayout{arc}, 1.25, 50, 75))
	require.Equal(t, -1, pick(t, []layeredgraph.EdgeLayout{arc}, 1.25, 50, 0), "the chord is not the edge")
}

// An edge with no route has no box and is never picked.
func TestEdgeWithoutRouteIsNotPicked(t *testing.T) {
	_, ok := newEdgeHitBox(&layeredgraph.EdgeLayout{}, identityTf, 1)
	require.False(t, ok)
	require.Equal(t, -1, pick(t, []layeredgraph.EdgeLayout{{ID: "empty"}}, 1, 0, 0))
}

// A node over an edge keeps the pointer; off the node, the edge is picked.
func TestNodeTakesPriorityOverEdge(t *testing.T) {
	edges := []layeredgraph.EdgeLayout{straightEdge("e", 0, 100, 50)}
	widths := []float32{1.25}
	boxes := []nodeBox{{shape: layeredgraph.NodeShapeBox, cx: 100, cy: 50, w: 20, h: 20}}

	node, edge := pickAt(boxes, edges, widths, identityTf, 95, 50)
	require.Equal(t, 0, node, "the node at the edge's end")
	require.Equal(t, -1, edge, "and not the edge beneath it")

	node, edge = pickAt(boxes, edges, widths, identityTf, 40, 51)
	require.Equal(t, -1, node)
	require.Equal(t, 0, edge)

	node, edge = pickAt(boxes, nil, nil, identityTf, 40, 51)
	require.Equal(t, -1, node)
	require.Equal(t, -1, edge, "no edges, nothing picked")
}
