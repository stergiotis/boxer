package graphview

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

func alphaOf(c color.Color) uint32 { return c.Literal() & 0xff }

func TestOpacityResolvesTheUnsetValue(t *testing.T) {
	// Zero is unset and anything at or above 1 is opaque, so an undeclared
	// opacity leaves a declaration unchanged to the bit (ADR-0224 §SD14).
	require.Equal(t, float32(1), opacityOr1(0))
	require.Equal(t, float32(1), opacityOr1(1))
	require.Equal(t, float32(1), opacityOr1(2))
	require.Equal(t, float32(1), opacityOr1(-0.5), "a negative reads as unset, not as invisible")
	require.Equal(t, float32(0.25), opacityOr1(0.25))
}

func TestFadeScalesAlphaAndLeavesTheRestAlone(t *testing.T) {
	col := color.RGBA(0x10, 0x20, 0x30, 0x80)
	require.Equal(t, col.Literal(), fade(col, 1).Literal(), "an opaque item is not rewritten")
	half := fade(col, 0.5)
	require.Equal(t, uint32(0x40), alphaOf(half))
	require.Equal(t, col.Literal()&^0xff, half.Literal()&^0xff, "the rgb is untouched")
	// An unset colour has no alpha to scale; the paint resolves it to the
	// style default first, and fading it there would produce a black.
	require.True(t, isUnset(fade(color.Color{}, 0.5)))
}

func TestOpacityReachesTheNodeFillAndItsBatchKey(t *testing.T) {
	v := New(nil, "t", Options{})
	v.style = DefaultStyle()
	v.g.reconcile([]NodeSpec{
		{Id: 1, Color: color.RGBA(0xff, 0, 0, 0xff)},
		{Id: 2, Color: color.RGBA(0xff, 0, 0, 0xff), Opacity: 0.5},
		{Id: 3, Color: color.RGBA(0xff, 0, 0, 0xff)},
	}, nil)
	require.Equal(t, uint32(0xff), alphaOf(v.nodeFill(int(v.g.slot[1]))))
	require.Equal(t, uint32(0x80), alphaOf(v.nodeFill(int(v.g.slot[2]))))

	// Two batches, not three: the faded node leaves the opaque pair's batch
	// and the two opaque nodes stay together.
	v.buildBatches()
	require.Len(t, v.batches, 2)
	sizes := []int{len(v.batches[0].slots), len(v.batches[1].slots)}
	require.ElementsMatch(t, []int{2, 1}, sizes)

	// Dropping the opacity puts it back.
	v.g.reconcile([]NodeSpec{
		{Id: 1, Color: color.RGBA(0xff, 0, 0, 0xff)},
		{Id: 2, Color: color.RGBA(0xff, 0, 0, 0xff)},
		{Id: 3, Color: color.RGBA(0xff, 0, 0, 0xff)},
	}, nil)
	v.buildBatches()
	require.Len(t, v.batches, 1)
}

func TestOpacitySurvivesTheSlotSwapOnRemoval(t *testing.T) {
	v := New(nil, "t", Options{})
	v.g.reconcile([]NodeSpec{{Id: 1, Opacity: 0.2}, {Id: 2}, {Id: 3, Opacity: 0.7, NoPick: true}}, nil)
	// Dropping id 2 swap-removes it, moving id 3 into its slot.
	v.g.reconcile([]NodeSpec{{Id: 1, Opacity: 0.2}, {Id: 3, Opacity: 0.7, NoPick: true}}, nil)
	require.Equal(t, float32(0.2), v.g.opacity[v.g.slot[1]])
	require.Equal(t, float32(0.7), v.g.opacity[v.g.slot[3]])
	require.True(t, v.g.noPick[v.g.slot[3]])
	require.False(t, v.g.noPick[v.g.slot[1]])
}

func TestNoPickTakesTheNodeOutOfEveryPointerPath(t *testing.T) {
	v := twoNodeView(Options{NodeSelection: true, RectSelection: true})
	v.cam.Zoom = 1
	require.Equal(t, int32(0), v.pickNode(0, 0), "pickable to begin with")

	v.g.reconcile([]NodeSpec{{Id: 1, NoPick: true}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2, Id: 7}})
	require.Equal(t, int32(-1), v.pickNode(0, 0), "the pointer passes through")
	require.Equal(t, int32(1), v.pickNode(100, 0), "its neighbour is unaffected")

	// A rectangle over both selects only the pickable one.
	v.rectSelect(-50, -50, 150, 50, false)
	require.False(t, v.IsNodeSelected(1))
	require.True(t, v.IsNodeSelected(2))

	// The caller asked, so the setter still works — as with the aura setters.
	require.True(t, v.SelectNode(1))
	require.True(t, v.IsNodeSelected(1))
}

func TestNoPickTakesTheEdgeOutOfThePick(t *testing.T) {
	v := twoNodeView(Options{})
	v.cam.Zoom = 1
	require.Equal(t, int32(0), v.pickEdge(50, 0))
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2, Id: 7, NoPick: true}})
	require.Equal(t, int32(-1), v.pickEdge(50, 0))
}

func TestBoundsReportsTheNodeBox(t *testing.T) {
	v := New(nil, "t", Options{})
	v.style = DefaultStyle()
	_, _, _, _, ok := v.Bounds()
	require.False(t, ok, "an empty graph has no box")

	v.g.reconcile([]NodeSpec{{Id: 1, Radius: 5}, {Id: 2, Radius: 10}}, nil)
	v.g.x[v.g.slot[1]], v.g.y[v.g.slot[1]] = 0, 0
	v.g.x[v.g.slot[2]], v.g.y[v.g.slot[2]] = 100, 50
	minX, minY, maxX, maxY, ok := v.Bounds()
	require.True(t, ok)
	require.Equal(t, [4]float32{-5, -5, 110, 60}, [4]float32{minX, minY, maxX, maxY})

	// A subset, and the unknown ids that are skipped rather than fatal.
	minX, minY, maxX, maxY, ok = v.BoundsOf([]uint64{2, 99})
	require.True(t, ok)
	require.Equal(t, [4]float32{90, 40, 110, 60}, [4]float32{minX, minY, maxX, maxY})
	_, _, _, _, ok = v.BoundsOf([]uint64{99})
	require.False(t, ok)
	_, _, _, _, ok = v.BoundsOf(nil)
	require.False(t, ok)
}

func TestFitNodesStillFramesWhatBoundsOfReports(t *testing.T) {
	// The fit is expressed through BoundsOf, so the two cannot drift.
	v := New(nil, "t", Options{})
	v.style = DefaultStyle()
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}, nil)
	v.g.x[v.g.slot[1]], v.g.y[v.g.slot[1]] = 0, 0
	v.g.x[v.g.slot[2]], v.g.y[v.g.slot[2]] = 500, 0
	v.g.x[v.g.slot[3]], v.g.y[v.g.slot[3]] = 0, 500
	v.lastW, v.lastH = 200, 200
	v.FitNodes([]uint64{1, 2})
	minX, minY, maxX, maxY, ok := v.BoundsOf([]uint64{1, 2})
	require.True(t, ok)
	cx, cy := (minX+maxX)/2, (minY+maxY)/2
	sx, sy := v.cam.ToScreen(cx, cy)
	require.InDelta(t, 100, sx, 0.5, "the subset's centre lands on the canvas centre")
	require.InDelta(t, 100, sy, 0.5)
}
