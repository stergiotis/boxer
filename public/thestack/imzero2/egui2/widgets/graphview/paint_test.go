package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// A zoomed-out graph stays dots under arrow heads that shrink with it: the
// disc holds its floor, the head and the stroke follow the zoom down to
// decorScaleMin, and at zoom 1 and above every declared size is what paints.
func TestZoomedOutSizesHoldTheirFloors(t *testing.T) {
	v := twoNodeView(Options{})
	v.style = v.Opts.Style.withDefaults()
	st := v.style

	v.cam.Zoom = 1
	require.InDelta(t, st.NodeRadius, v.nodeRadiusPx(0), 1e-6)
	require.InDelta(t, st.TipSize, v.tipPx(), 1e-6)
	require.InDelta(t, st.EdgeWidth, v.edgeGeometry(0).width, 1e-6)

	v.cam.Zoom = 4
	require.InDelta(t, 4*st.NodeRadius, v.nodeRadiusPx(0), 1e-6, "the disc grows with the zoom")
	require.InDelta(t, st.TipSize, v.tipPx(), 1e-6, "the head does not")

	v.cam.Zoom = 0.01
	require.InDelta(t, nodeMinRadiusPx, v.nodeRadiusPx(0), 1e-6)
	require.InDelta(t, st.TipSize*decorScaleMin, v.tipPx(), 1e-6)
	require.InDelta(t, edgeMinWidthPx, v.edgeGeometry(0).width, 1e-6)

	// A declared 0 is 0 whatever the zoom (ADR-0232 §SD4).
	v.g.reconcile([]NodeSpec{{Id: 1, Radius: 0}, {Id: 2}}, nil)
	v.g.radius[0] = 0
	require.Equal(t, float32(0), v.nodeRadiusPx(0))

	// Hosted, the camera is the host's and the declared sizes stand.
	v.hosted = true
	require.InDelta(t, st.TipSize, v.tipPx(), 1e-6)
}

// Nodes that overlap past the trimmed ends leave the edge nothing to paint.
func TestOverlappingNodesHideTheEdge(t *testing.T) {
	v := twoNodeView(Options{})
	v.style = v.Opts.Style.withDefaults()
	require.False(t, v.edgeGeometry(0).hidden)
	v.g.x[1] = v.g.x[0] + 2*v.style.NodeRadius
	require.True(t, v.edgeGeometry(0).hidden)
}

// A label sits beside its stroke, on the side that faces up, its box clear
// of the line whether the edge runs flat or upright; a loop's label sits
// above the loop's top, not a loop radius past it.
func TestEdgeLabelsClearTheStroke(t *testing.T) {
	v := twoNodeView(Options{})
	v.style = v.Opts.Style.withDefaults()
	fs := v.style.EdgeLabelFontSize

	geo := v.edgeGeometry(0) // flat, left to right
	x, y := edgeLabelPos(geo, "1→2", fs)
	mx, my := (geo.x[0]+geo.x[3])/2, (geo.y[0]+geo.y[3])/2
	require.InDelta(t, mx, x, 1e-4)
	require.Less(t, y+edgeLabelLineH*fs/2, my, "the box's bottom is above the stroke")

	v.g.x[1], v.g.y[1] = 0, 100 // upright, top to bottom
	geo = v.edgeGeometry(0)
	x, y = edgeLabelPos(geo, "1→2", fs)
	mx, my = (geo.x[0]+geo.x[3])/2, (geo.y[0]+geo.y[3])/2
	require.InDelta(t, my, y, 1e-4)
	require.Less(t, x+edgeLabelGlyphW*fs*3/2, mx, "the box's right edge is left of the stroke")

	v.g.reconcile([]NodeSpec{{Id: 1}}, []EdgeSpec{{From: 1, To: 1}})
	v.g.x[0], v.g.y[0] = 0, 0
	geo = v.edgeGeometry(0)
	require.Equal(t, edgeKindLoop, geo.kind)
	_, y = edgeLabelPos(geo, "loop", fs)
	_, top := bezierAt(geo.x, geo.y, 0.5)
	require.Less(t, y, top, "above the loop")
	require.Greater(t, y, top-edgeLabelLineH*fs-edgeLabelGapPx-1e-3, "and close to it")
	require.False(t, math.IsNaN(float64(y)))
}
