package view

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// The pick replaced a sense region per node (ADR-0228 §SD6). These pin what
// the regions used to answer, which nothing tested before.

func TestBoxNodeIsHitInsideItsRectAndNotOutside(t *testing.T) {
	b := nodeBox{shape: layeredgraph.NodeShapeBox, cx: 100, cy: 50, w: 40, h: 20}
	require.True(t, b.contains(100, 50), "centre")
	require.True(t, b.contains(80, 40), "top-left corner")
	require.True(t, b.contains(120, 60), "bottom-right corner")
	require.False(t, b.contains(79.9, 50))
	require.False(t, b.contains(100, 60.1))
}

func TestCircleNodeUsesTheRadiusItWasDrawnWith(t *testing.T) {
	// drawNode takes r = min(w, h) / 2, so a wide-but-short node is a small
	// circle and the corners of its box are outside it.
	b := nodeBox{shape: layeredgraph.NodeShapeCircle, cx: 0, cy: 0, w: 100, h: 20}
	require.True(t, b.contains(0, 0))
	require.True(t, b.contains(9.9, 0), "just inside r = 10")
	require.False(t, b.contains(10.1, 0), "just outside")
	require.False(t, b.contains(40, 0), "inside the box, outside the circle")
}

func TestEllipseNodeUsesBothRadii(t *testing.T) {
	b := nodeBox{shape: layeredgraph.NodeShapeEllipse, cx: 0, cy: 0, w: 100, h: 20}
	require.True(t, b.contains(49, 0))
	require.False(t, b.contains(51, 0))
	require.True(t, b.contains(0, 9))
	require.False(t, b.contains(0, 11))
	require.False(t, b.contains(40, 8), "inside the box, outside the ellipse")

	// A degenerate ellipse is not hit rather than dividing by zero.
	require.False(t, nodeBox{shape: layeredgraph.NodeShapeEllipse}.contains(0, 0))
}

func TestTopmostNodeWins(t *testing.T) {
	// Nodes paint in the layout's order, so the later one covers the earlier
	// and must be the one the pointer hits.
	boxes := []nodeBox{
		{shape: layeredgraph.NodeShapeBox, cx: 50, cy: 50, w: 100, h: 100},
		{shape: layeredgraph.NodeShapeBox, cx: 60, cy: 60, w: 20, h: 20},
	}
	require.Equal(t, 1, pickNode(boxes, 60, 60), "the one on top")
	require.Equal(t, 0, pickNode(boxes, 20, 20), "only the one underneath reaches here")
	require.Equal(t, -1, pickNode(boxes, 500, 500), "nothing there")
	require.Equal(t, -1, pickNode(nil, 0, 0))
}

func TestPickMatchesWhatTheLayoutPlaced(t *testing.T) {
	// End to end through the same transform Render builds: a node's centre in
	// layout points must pick that node in canvas pixels.
	lay := &layeredgraph.Layout{
		Width: 200, Height: 100,
		Nodes: []layeredgraph.NodeLayout{
			{ID: "a", Center: layeredgraph.Point{X: 50, Y: 50}, W: 40, H: 20},
			{ID: "b", Center: layeredgraph.Point{X: 150, Y: 50}, W: 40, H: 20},
		},
	}
	scale, offX, offY, _, _ := fit(lay, 800, 400)
	require.Positive(t, scale)
	tf := func(p layeredgraph.Point) (float32, float32) {
		return float32(p.X*scale + offX), float32(p.Y*scale + offY)
	}
	boxes := make([]nodeBox, len(lay.Nodes))
	for i, n := range lay.Nodes {
		cx, cy := tf(n.Center)
		boxes[i] = nodeBox{shape: n.Shape, cx: cx, cy: cy, w: float32(n.W * scale), h: float32(n.H * scale)}
	}
	for i, n := range lay.Nodes {
		cx, cy := tf(n.Center)
		require.Equal(t, i, pickNode(boxes, cx, cy), "node %s at its own centre", n.ID)
	}
	// The gap between them belongs to neither.
	gx, gy := tf(layeredgraph.Point{X: 100, Y: 50})
	require.Equal(t, -1, pickNode(boxes, gx, gy))
}
