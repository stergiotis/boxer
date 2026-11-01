package view

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// An ellipse node's hit region is a polygon on the ellipse the node paints:
// every vertex on the curve, so the region covers what the pick covers to
// within the chord's sag.
func TestEllipseRegionFollowsTheNode(t *testing.T) {
	b := nodeBox{shape: layeredgraph.NodeShapeEllipse, cx: 10, cy: 20, w: 100, h: 40}
	xs, ys := ellipseOutline(b.cx, b.cy, b.w/2, b.h/2)
	require.Len(t, xs, ellipseSegments)
	require.Len(t, ys, ellipseSegments)
	for k := range xs {
		dx, dy := (xs[k]-b.cx)/(b.w/2), (ys[k]-b.cy)/(b.h/2)
		require.InDelta(t, 1, dx*dx+dy*dy, 1e-5, "vertex %d is on the ellipse", k)
		// a point just inside the vertex is inside the painted ellipse
		require.True(t, b.contains(b.cx+(xs[k]-b.cx)*0.99, b.cy+(ys[k]-b.cy)*0.99))
	}
}
