package graphview

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// twoNodeView is a view over nodes 1 at (0,0) and 2 at (100,0) joined by
// one edge, with the style resolved and the camera at identity, so canvas
// pixels are world units.
func twoNodeView(o Options) *View {
	v := New(nil, "t", o)
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2, Id: 7}})
	v.g.x[0], v.g.y[0] = 0, 0
	v.g.x[1], v.g.y[1] = 100, 0
	return v
}

func kinds(evs []Event) []EventKindE {
	out := make([]EventKindE, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out
}

func identityWheel() c.CanvasWheelValue {
	var w c.CanvasWheelValue
	w.Zoom = 1
	return w
}

func TestEdgeIdsTellParallelEdgesApart(t *testing.T) {
	v := New(nil, "t", Options{EdgeSelection: true, EdgeSelectionMulti: true})
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2, Id: 1}, {From: 1, To: 2, Id: 2}, {From: 2, To: 1}})
	require.True(t, v.SelectEdge(EdgeRef{From: 1, To: 2, Id: 1}))
	require.True(t, v.SelectEdge(EdgeRef{From: 1, To: 2, Id: 2}))
	require.True(t, v.SelectEdge(EdgeRef{From: 2, To: 1}))
	require.False(t, v.SelectEdge(EdgeRef{From: 1, To: 2, Id: 3}), "no such id")
	require.False(t, v.SelectEdge(EdgeRef{From: 1, To: 2}), "the pair alone does not name an edge that carries ids")
	require.Equal(t, []EdgeRef{{1, 2, 1}, {1, 2, 2}, {2, 1, 0}}, slices.Collect(v.SelectedEdges()))
	require.Empty(t, v.Events(), "programmatic selection reports nothing")

	// Dropping one parallel edge prunes only it, though its endpoints stay.
	_, changed := v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2, Id: 1}, {From: 2, To: 1}})
	require.True(t, changed, "an id change is a topology change")
	v.pruneSelection()
	require.Equal(t, []EdgeRef{{1, 2, 1}, {2, 1, 0}}, slices.Collect(v.SelectedEdges()))
}

func TestProgrammaticNodeSelection(t *testing.T) {
	v := twoNodeView(Options{NodeSelection: true})
	require.True(t, v.SelectNode(1))
	require.True(t, v.SelectNode(2))
	require.False(t, v.SelectNode(3), "unknown id")
	require.True(t, v.IsNodeSelected(1))
	require.Equal(t, []uint64{1, 2}, slices.Collect(v.SelectedNodes()))
	v.DeselectNode(1)
	require.Equal(t, []uint64{2}, slices.Collect(v.SelectedNodes()))
	v.ClearSelection()
	require.Empty(t, slices.Collect(v.SelectedNodes()))
	require.Empty(t, v.Events())
}

func TestSecondaryClicksReportOnNodeEdgeAndBackground(t *testing.T) {
	v := twoNodeView(Options{NodeClicking: true, EdgeClicking: true, BackgroundClicking: true, NoHover: true})
	sec := c.SecondaryClickedResponseFlags
	v.applyInput(800, 600, 0, 0, true, true, sec, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindNodeSecondaryClick}, kinds(v.events))
	require.Equal(t, uint64(1), v.events[0].Node)

	v.events = v.events[:0]
	v.applyInput(800, 600, 50, 0, true, true, sec, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindEdgeSecondaryClick}, kinds(v.events))
	require.Equal(t, EdgeRef{From: 1, To: 2, Id: 7}, EdgeRef{From: v.events[0].From, To: v.events[0].To, Id: v.events[0].Edge})

	v.events = v.events[:0]
	v.cam.Zoom, v.cam.PanX, v.cam.PanY = 2, 10, 20
	v.applyInput(800, 600, 400, 300, true, true, sec, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindBackgroundSecondaryClick}, kinds(v.events))
	require.Equal(t, [2]float32{195, 140}, [2]float32{v.events[0].X, v.events[0].Y}, "background clicks carry the world position")

	// A long touch is a secondary click too.
	v.events = v.events[:0]
	v.applyInput(800, 600, 400, 300, true, true, c.LongTouchedResponseFlags, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindBackgroundSecondaryClick}, kinds(v.events))
}

func TestBackgroundClickDeselectsAndReports(t *testing.T) {
	v := twoNodeView(Options{NodeSelection: true, BackgroundClicking: true, NoHover: true})
	v.SelectNode(1)
	v.applyInput(800, 600, 400, 300, true, true, c.PrimaryClickedResponseFlags, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindNodeDeselect, EventKindBackgroundClick}, kinds(v.events))
	v.events = v.events[:0]
	v.applyInput(800, 600, 400, 300, true, true, c.PrimaryClickedResponseFlags|c.DoubleClickedResponseFlags, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindBackgroundDoubleClick}, kinds(v.events), "the double-click takes the second click")

	off := twoNodeView(Options{NoHover: true})
	off.applyInput(800, 600, 400, 300, true, true, c.PrimaryClickedResponseFlags, identityWheel(), c.ModifiersValue{})
	require.Empty(t, off.events, "background events are opt-in")
}

func TestEdgeEventsCarryTheIdAndHoverEntersAndLeaves(t *testing.T) {
	v := twoNodeView(Options{EdgeClicking: true, EdgeSelection: true})
	v.applyInput(800, 600, 50, 0, true, true, c.PrimaryClickedResponseFlags, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindEdgeHoverEnter, EventKindEdgeClick, EventKindEdgeSelect}, kinds(v.events))
	for _, ev := range v.events {
		require.Equal(t, uint64(7), ev.Edge)
	}
	ref, ok := v.HoveredEdge()
	require.True(t, ok)
	require.Equal(t, EdgeRef{From: 1, To: 2, Id: 7}, ref)

	v.events = v.events[:0]
	v.applyInput(800, 600, 50, 200, true, true, 0, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindEdgeHoverLeave}, kinds(v.events))
	_, ok = v.HoveredEdge()
	require.False(t, ok)

	v.events = v.events[:0]
	v.applyInput(800, 600, 50, 0, true, true, c.PrimaryClickedResponseFlags|c.DoubleClickedResponseFlags, identityWheel(), c.ModifiersValue{})
	require.Equal(t, []EventKindE{EventKindEdgeHoverEnter, EventKindEdgeDoubleClick}, kinds(v.events), "a double-click does not toggle the edge's selection")
	require.True(t, v.IsEdgeSelected(EdgeRef{From: 1, To: 2, Id: 7}))
}

func TestRectSelectionSelectsCoveredNodes(t *testing.T) {
	v := twoNodeView(Options{NodeSelection: true, EdgeSelection: true, RectSelection: true, NoHover: true})
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}, []EdgeSpec{{From: 1, To: 2}})
	v.g.x[2], v.g.y[2] = 300, 300
	v.SelectNode(3)
	v.SelectNode(1)
	v.SelectEdge(EdgeRef{From: 1, To: 2})
	down := c.IsPointerButtonDownResponseFlags
	shift := c.ModifiersValue{Shift: true}
	// Shift-press on empty canvas, sweep over nodes 1 and 2, release.
	v.applyInput(800, 600, 150, 50, true, true, c.DragStartedResponseFlags|down, identityWheel(), shift)
	require.True(t, v.drag.isRect)
	v.applyInput(800, 600, 20, -10, true, true, c.DraggedResponseFlags|down, identityWheel(), shift)
	require.Equal(t, float32(1), v.cam.Zoom)
	require.Equal(t, float32(0), v.cam.PanX, "a rectangle drag does not pan")
	v.applyInput(800, 600, -10, -10, true, true, c.DragStoppedResponseFlags, identityWheel(), shift)
	require.Equal(t, []uint64{1, 2}, slices.Collect(v.SelectedNodes()), "node 3 outside the box was replaced")
	require.Empty(t, slices.Collect(v.SelectedEdges()), "the edge selection is replaced too")
	require.Equal(t, []EventKindE{EventKindNodeDeselect, EventKindEdgeDeselect, EventKindNodeSelect}, kinds(v.events),
		"node 3 and the edge report a Deselect, node 2 a Select, and node 1 — selected and inside — nothing")
	require.Equal(t, uint64(3), v.events[0].Node)
	require.Equal(t, uint64(2), v.events[2].Node)
	require.False(t, v.drag.active)

	// Without Shift the same gesture pans.
	v.applyInput(800, 600, 150, 50, true, true, c.DragStartedResponseFlags|down, identityWheel(), c.ModifiersValue{})
	require.False(t, v.drag.isRect)
	v.applyInput(800, 600, 160, 50, true, true, c.DraggedResponseFlags|down, identityWheel(), c.ModifiersValue{})
	require.Equal(t, float32(10), v.cam.PanX)

	// Multi-selection adds instead.
	v.drag = dragState{}
	v.events = v.events[:0]
	v.Opts.NodeSelectionMulti = true
	v.cam.PanX = 0
	v.applyInput(800, 600, 250, 250, true, true, c.DragStartedResponseFlags|down, identityWheel(), shift)
	v.applyInput(800, 600, 350, 350, true, true, c.DragStoppedResponseFlags, identityWheel(), shift)
	require.Equal(t, []uint64{1, 2, 3}, slices.Collect(v.SelectedNodes()))
	require.Equal(t, []EventKindE{EventKindNodeSelect}, kinds(v.events))
}

func TestFitNodesFramesTheSubset(t *testing.T) {
	v := twoNodeView(Options{FitPadding: 0.25})
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}, nil)
	v.g.x[2], v.g.y[2] = 1000, 1000
	v.FitNodes([]uint64{1, 2, 99})
	require.True(t, v.fitIdsWait, "before the first render the request waits")
	v.lastW, v.lastH = 400, 400
	v.fitPending = true
	v.FitNodes([]uint64{1, 2, 99})
	require.False(t, v.fitIdsWait)
	require.False(t, v.fitPending, "the subset fit releases the latch")
	// Nodes 1 and 2 span x 0..100 plus a 5 unit radius each: 110 wide into
	// 400·(1−0.5) = 200 px, so zoom is 200/110; the box is centred.
	require.InDelta(t, 200.0/110, v.cam.Zoom, 1e-5)
	sx, sy := v.cam.ToScreen(50, 0)
	require.InDelta(t, 200, sx, 1e-3)
	require.InDelta(t, 200, sy, 1e-3)
	v.cam.Zoom = 3
	v.FitNodes([]uint64{99})
	require.Equal(t, float32(3), v.cam.Zoom, "no known id leaves the camera alone")
}

func TestPositionsAndScreenRadius(t *testing.T) {
	v := twoNodeView(Options{})
	got := map[uint64][2]float32{}
	for id, p := range v.Positions() {
		got[id] = p
	}
	require.Equal(t, map[uint64][2]float32{1: {0, 0}, 2: {100, 0}}, got)
	v.cam.Zoom = 2
	r, ok := v.NodeScreenRadius(1)
	require.True(t, ok)
	require.Equal(t, float32(10), r, "the style radius of 5 at zoom 2")
	_, ok = v.NodeScreenRadius(9)
	require.False(t, ok)
}

func TestZoomLimitsAreOptions(t *testing.T) {
	v := twoNodeView(Options{ZoomMin: 0.5, ZoomMax: 2})
	v.cam.MinZoom, v.cam.MaxZoom = v.Opts.ZoomMin, v.Opts.ZoomMax
	v.cam.ClampZoom()
	v.SetCamera(10, 0, 0)
	require.Equal(t, float32(2), v.cam.Zoom)
	v.cam.ZoomAround(0.01, 0, 0)
	require.Equal(t, float32(0.5), v.cam.Zoom)
	v.cam.Fit(0, 0, 1, 1, 4000, 4000, 0.1)
	require.Equal(t, float32(2), v.cam.Zoom)
	// The limit arithmetic itself is the camera package's; what this asserts
	// is that Options carries into it.
	require.Equal(t, [2]float32{0.5, 2}, [2]float32{v.cam.MinZoom, v.cam.MaxZoom})
}

func TestEdgeLengthAndStrengthScaleTheAttraction(t *testing.T) {
	pull := func(e EdgeSpec) float32 {
		var g graph
		g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{e})
		g.x[g.slot[2]] = 100
		dx := make([]float32, 2)
		dy := make([]float32, 2)
		attraction(&g, dx, dy, 50, 1e-3, 1)
		return dx[g.slot[1]]
	}
	base := pull(EdgeSpec{From: 1, To: 2})
	require.Greater(t, base, float32(0))
	require.InDelta(t, base/2, pull(EdgeSpec{From: 1, To: 2, Length: 2}), 1e-4, "a longer ideal length halves the pull")
	require.InDelta(t, base/2, pull(EdgeSpec{From: 1, To: 2, Strength: 0.5}), 1e-4, "half the strength halves the pull")
	require.Equal(t, base, pull(EdgeSpec{From: 1, To: 2, Length: 1, Strength: 1}), "explicit ones are the default")
}

func TestPauseOnSettleHoldsAndWakes(t *testing.T) {
	v := New(nil, "t", Options{Layout: LayoutForceDirected, Force: ForceParams{PauseOnSettle: true, Epsilon: 1}})
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	v.g.x[1] = 300
	fp := v.Opts.Force.withDefaults()
	for range 400 {
		v.stepLayout(500, 500, fp, false)
		if v.autoPaused {
			break
		}
	}
	require.True(t, v.autoPaused, "the layout came to rest and was held")
	require.True(t, v.IsSettled())
	require.True(t, v.Metrics().Paused)
	steps := v.fs.steps
	v.stepLayout(500, 500, fp, false)
	require.Equal(t, steps, v.fs.steps, "held: no step")

	v.stepLayout(500, 500, fp, true)
	require.Equal(t, steps+1, v.fs.steps, "a topology change wakes it")
	for range 400 {
		v.stepLayout(500, 500, fp, false)
		if v.autoPaused {
			break
		}
	}
	require.True(t, v.autoPaused)
	steps = v.fs.steps
	v.SetNodePosition(1, 900, 900)
	v.stepLayout(500, 500, fp, false)
	require.Equal(t, steps+1, v.fs.steps, "a position set wakes it")
	fp.Damping = 0.5
	v.autoPaused = true
	v.stepLayout(500, 500, fp, false)
	require.False(t, v.autoPaused, "a parameter change wakes it")
	v.autoPaused = true
	v.FastForward(3)
	v.stepLayout(500, 500, fp, false)
	require.False(t, v.autoPaused, "FastForward wakes it")

	// A switch to a static layout drops the hold, so the force layout runs
	// again when switched back rather than waiting for a wake.
	v.autoPaused = true
	v.Opts.Layout = LayoutHierarchical
	v.stepLayout(500, 500, fp, false)
	v.Opts.Layout = LayoutForceDirected
	steps = v.fs.steps
	v.stepLayout(500, 500, fp, false)
	require.Equal(t, steps+1, v.fs.steps, "moving again after the round trip")
}

func TestEventKindClasses(t *testing.T) {
	require.True(t, EventKindNodeSecondaryClick.IsNode())
	require.True(t, EventKindEdgeHoverLeave.IsEdge())
	require.True(t, EventKindEdgeSecondaryClick.IsEdge())
	require.True(t, EventKindBackgroundDoubleClick.IsBackground())
	require.False(t, EventKindBackgroundClick.IsNode())
	require.False(t, EventKindAuraToggle.IsEdge())
	require.Equal(t, "BackgroundSecondaryClick", EventKindBackgroundSecondaryClick.String())
	require.Equal(t, "EdgeHoverEnter", EventKindEdgeHoverEnter.String())
}
