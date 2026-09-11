package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

func TestDonutArcsNormaliseSkipAndTrack(t *testing.T) {
	track := color.Hex(0x11111111)
	arcs := donutArcs(Donut{Values: []float32{1, 0, -2, 3}, Colors: color.ColorsFromU32([]uint32{0xff0000ff})}, track, nil)
	require.Len(t, arcs, 2, "non-positive values draw nothing")
	require.InDelta(t, donutStart, arcs[0].a0, 1e-6, "the first slice starts at the top")
	require.InDelta(t, 2*math.Pi/4, arcs[0].a1-arcs[0].a0, 1e-5, "a quarter of the sum")
	require.InDelta(t, 3*2*math.Pi/4, arcs[1].a1-arcs[1].a0, 1e-5)
	require.InDelta(t, donutStart+2*math.Pi, arcs[1].a1, 1e-5, "the slices close the ring")
	require.Equal(t, uint32(0xff0000ff), arcs[0].col.Literal())
	require.False(t, isUnset(arcs[1].col), "a missing colour takes the qualitative cycle")
	require.False(t, arcs[1].track)

	arcs = donutArcs(Donut{Values: []float32{1}, Total: 4}, track, nil)
	require.Len(t, arcs, 2)
	require.InDelta(t, 2*math.Pi/4, arcs[0].a1-arcs[0].a0, 1e-5, "Total is the denominator when larger")
	require.True(t, arcs[1].track)
	require.Equal(t, track.Literal(), arcs[1].col.Literal())
	require.InDelta(t, donutStart+2*math.Pi, arcs[1].a1, 1e-5, "the track fills the remainder")

	require.Empty(t, donutArcs(Donut{Total: 3}, track, nil), "a total without values draws nothing")
	require.True(t, Donut{Values: []float32{0, -1}}.IsEmpty())
	require.False(t, Donut{Values: []float32{0, 1}}.IsEmpty())
}

func TestRingSectorOutline(t *testing.T) {
	xs, ys := ringSector(0, 0, 10, 20, 0, math.Pi/2, nil, nil)
	require.Equal(t, len(xs), len(ys))
	require.Zero(t, len(xs)%2, "outer and inner arcs have the same sample count")
	n := len(xs) / 2
	require.InDelta(t, 20, xs[0], 1e-5, "outer arc starts at angle 0")
	require.InDelta(t, 0, ys[0], 1e-5)
	require.InDelta(t, 0, xs[n-1], 1e-4, "outer arc ends at a quarter turn")
	require.InDelta(t, 20, ys[n-1], 1e-4)
	require.InDelta(t, 0, xs[n], 1e-4, "inner arc returns from the end")
	require.InDelta(t, 10, ys[n], 1e-4)
	require.InDelta(t, 10, xs[len(xs)-1], 1e-5, "and closes at the start radius")
	for i := range xs {
		r := math.Hypot(float64(xs[i]), float64(ys[i]))
		require.True(t, math.Abs(r-10) < 1e-3 || math.Abs(r-20) < 1e-3, "point %d lies on one of the two radii", i)
	}
	// Sample density follows the outer arc length, capped.
	long, _ := ringSector(0, 0, 100, 400, 0, math.Pi, nil, nil)
	require.Equal(t, 2*65, len(long), "a long arc hits the 64-segment cap")
	short, _ := ringSector(0, 0, 1, 2, 0, 0.1, nil, nil)
	require.Equal(t, 2*3, len(short), "a short arc keeps at least two segments")
}

func TestSplitArcHalvesLongSpans(t *testing.T) {
	require.Len(t, splitArc(0, 3), 1)
	parts := splitArc(0, 2*math.Pi)
	require.Len(t, parts, 2)
	require.InDelta(t, math.Pi, parts[0][1], 1e-6)
	require.InDelta(t, math.Pi, parts[1][0], 1e-6)
}

func TestNodeOuterPxIncludesADrawableDonut(t *testing.T) {
	v := New(nil, "t", Options{})
	v.g.reconcile([]NodeSpec{{Id: 1, Donut: Donut{Values: []float32{1}}}, {Id: 2}}, nil)
	v.style = v.Opts.Style.withDefaults()
	st := v.style
	plain := v.nodeOuterPx(int(v.g.slot[2]))
	ringed := v.nodeOuterPx(int(v.g.slot[1]))
	require.InDelta(t, st.NodeRadius, plain, 1e-6)
	require.InDelta(t, st.NodeRadius+st.DonutWidth, ringed, 1e-6)
	v.cam.zoom = 0.01
	require.InDelta(t, st.NodeRadius*0.01, v.nodeOuterPx(int(v.g.slot[1])), 1e-6, "a ring too small to draw does not widen the pick")
}
