package graphview

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	cam "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/camera"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// hostScene drives a View through the hosted pair against registers scripted
// on handles the View does not own — a stand-in for the host's canvas and
// area (ADR-0228 §SD1).
type hostScene struct {
	t    *testing.T
	sm   *c.StateManager
	v    *View
	host HostCanvas
	w, h float32
}

func newHostScene(t *testing.T, o Options, w, h float32) *hostScene {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	// Handles of a canvas this view never emits: the host's.
	canvas := widgethandle.Make(ids.PrepareStr("host-canvas").Derive())
	area := widgethandle.Make(ids.PrepareStr("host-area").Derive())
	return &hostScene{
		t: t, sm: c.CurrentApplicationState.StateManager, w: w, h: h,
		v:    New(ids, "guest", o),
		host: HostCanvas{Canvas: canvas, Area: area, W: w, H: h, Camera: cam.Camera{Zoom: 1}},
	}
}

func (s *hostScene) pointerAt(x, y float32, area c.ResponseFlagsE, mods c.ModifiersValue) {
	nan := float32(math.NaN())
	s.sm.ScriptCanvasCursor(s.host.Canvas, c.CanvasCursorValue{OriginX: 0, OriginY: 0, PosX: nan, PosY: nan})
	s.sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
	s.sm.ScriptResponse(s.host.Canvas, c.ContainsPointerResponseFlags)
	s.sm.ScriptResponse(s.host.Area, area)
	s.sm.ScriptModifiers(mods)
}

// frame runs the hosted pair and returns the claim and the events.
func (s *hostScene) frame(nodes []NodeSpec, edges []EdgeSpec) (HostClaim, []Event) {
	claim := s.v.HostedInput(s.host)
	s.v.HostedPaint(nodes, edges)
	require.False(s.t, typed.HasErrors(), typed.GetError())
	s.sm.ScriptReset()
	return claim, slices.Clone(s.v.Events())
}

// twoNodes sits at world (0,0) and (100,0); with an identity camera those are
// canvas (0,0) and (100,0).
func hostedNodes() []NodeSpec {
	return []NodeSpec{
		{Id: 1, Pinned: true, PinX: 0, PinY: 0},
		{Id: 2, Pinned: true, PinX: 100, PinY: 0},
	}
}

func TestHostedClaimsTheGestureOnANodeAndNotTheBackground(t *testing.T) {
	s := newHostScene(t, Options{NodeClicking: true, NodeSelection: true}, 400, 300)
	s.frame(hostedNodes(), nil) // a frame to place the nodes

	// Over a node: the guest takes the pointer and names it.
	s.pointerAt(0, 0, c.ResponseFlagsE(0), c.ModifiersValue{})
	claim, _ := s.frame(hostedNodes(), nil)
	require.True(t, claim.Pointer, "a pointer over a node is the guest's")
	require.True(t, claim.HasNode)
	require.Equal(t, uint64(1), claim.Node)

	// Over empty canvas: the host's.
	s.pointerAt(250, 250, c.ResponseFlagsE(0), c.ModifiersValue{})
	claim, _ = s.frame(hostedNodes(), nil)
	require.False(t, claim.Pointer, "empty canvas belongs to the host")
	require.False(t, claim.HasNode)
}

func TestHostedClaimHoldsForTheWholeNodeDrag(t *testing.T) {
	s := newHostScene(t, Options{}, 400, 300)
	s.frame(hostedNodes(), nil)

	s.pointerAt(0, 0, c.DragStartedResponseFlags|c.IsPointerButtonDownResponseFlags, c.ModifiersValue{})
	claim, evs := s.frame(hostedNodes(), nil)
	require.True(t, claim.Pointer)
	require.Contains(t, kinds(evs), EventKindNodeDragStart)

	// The pointer has left the node, but the drag is the guest's until it
	// stops — otherwise the map would start panning mid-drag.
	s.pointerAt(250, 250, c.DraggedResponseFlags|c.IsPointerButtonDownResponseFlags, c.ModifiersValue{})
	claim, _ = s.frame(hostedNodes(), nil)
	require.True(t, claim.Pointer, "a drag in flight stays claimed")

	s.pointerAt(250, 250, c.DragStoppedResponseFlags, c.ModifiersValue{})
	claim, _ = s.frame(hostedNodes(), nil)
	require.True(t, claim.Pointer, "the frame that ends it is still the guest's")

	s.pointerAt(250, 250, c.ResponseFlagsE(0), c.ModifiersValue{})
	claim, _ = s.frame(hostedNodes(), nil)
	require.False(t, claim.Pointer, "and then the host has it back")
}

func TestHostedClaimsAShiftRectangleButNotAPlainBackgroundDrag(t *testing.T) {
	s := newHostScene(t, Options{NodeSelection: true, RectSelection: true}, 400, 300)
	s.frame(hostedNodes(), nil)

	// A plain background drag is the host's pan.
	s.pointerAt(250, 250, c.DragStartedResponseFlags|c.IsPointerButtonDownResponseFlags, c.ModifiersValue{})
	claim, _ := s.frame(hostedNodes(), nil)
	require.False(t, claim.Pointer)

	// Shift makes it the guest's rectangle — the one background gesture it
	// takes (ADR-0228 §SD4).
	s2 := newHostScene(t, Options{NodeSelection: true, RectSelection: true}, 400, 300)
	s2.frame(hostedNodes(), nil)
	s2.pointerAt(250, 250, c.DragStartedResponseFlags|c.IsPointerButtonDownResponseFlags, c.ModifiersValue{Shift: true})
	claim, _ = s2.frame(hostedNodes(), nil)
	require.True(t, claim.Pointer)
}

func TestHostedCameraIsTheHostsAndIsReinstalledEachFrame(t *testing.T) {
	s := newHostScene(t, Options{}, 400, 300)
	s.host.Camera = cam.Camera{Zoom: 2, PanX: 30, PanY: 40}
	s.frame(hostedNodes(), nil)
	require.Equal(t, float32(2), s.v.cam.Zoom)
	zoom, panX, panY := s.v.Camera()
	require.Equal(t, [3]float32{2, 30, 40}, [3]float32{zoom, panX, panY})

	// A setter does not survive the next frame: the host's transform wins,
	// so the graph cannot slide against what the host drew (§SD3).
	s.v.SetCamera(9, 1, 1)
	s.host.Camera = cam.Camera{Zoom: 3, PanX: 0, PanY: 0}
	s.frame(hostedNodes(), nil)
	require.Equal(t, float32(3), s.v.cam.Zoom)

	// Nor does a fit request.
	s.v.FitNow()
	s.frame(hostedNodes(), nil)
	require.Equal(t, float32(3), s.v.cam.Zoom, "a hosted view never fits itself")
	require.False(t, s.v.Metrics().CameraMoved)
}

func TestHostedWheelAndBackgroundDragDoNotMoveTheGuestsCamera(t *testing.T) {
	s := newHostScene(t, Options{}, 400, 300)
	s.host.Camera = cam.Camera{Zoom: 1}
	s.frame(hostedNodes(), nil)

	s.pointerAt(200, 150, c.DragStartedResponseFlags|c.IsPointerButtonDownResponseFlags, c.ModifiersValue{})
	s.frame(hostedNodes(), nil)
	s.pointerAt(260, 150, c.DraggedResponseFlags|c.IsPointerButtonDownResponseFlags, c.ModifiersValue{})
	s.frame(hostedNodes(), nil)
	require.Equal(t, float32(0), s.v.cam.PanX, "the pan belongs to the host")

	s.sm.ScriptCanvasWheel(s.host.Canvas, c.CanvasWheelValue{Zoom: 2, HoverX: 200, HoverY: 150})
	s.frame(hostedNodes(), nil)
	require.Equal(t, float32(1), s.v.cam.Zoom, "the wheel belongs to the host")
}

func TestHostedPaintEmitsLessThanRender(t *testing.T) {
	// The canvas and the drag-owning region are what a hosted render leaves
	// out, so it must emit strictly fewer messages for the same declaration.
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	ids := c.NewWidgetIdStack()
	nodes := hostedNodes()

	own := New(ids, "own", Options{})
	zero()
	own.Render(nodes, nil, 400, 300)
	full := messages()

	guest := New(ids, "guest", Options{})
	guest.cam = cam.Camera{Zoom: 1}
	zero()
	guest.HostedInput(HostCanvas{
		Canvas: widgethandle.Make(ids.PrepareStr("h-canvas").Derive()),
		Area:   widgethandle.Make(ids.PrepareStr("h-area").Derive()),
		W:      400, H: 300, Camera: cam.Camera{Zoom: 1},
	})
	guest.HostedPaint(nodes, nil)
	hosted := messages()

	require.Greater(t, full, 0)
	require.Less(t, hosted, full, "a hosted render stamps no canvas and no sense region")
}

func TestHostedDrawsNoLegendEvenWhenAskedInside(t *testing.T) {
	// Inside mode would stamp rows under the host's area region, where they
	// could never be clicked; the rows are still published (ADR-0224 §SD15).
	s := newHostScene(t, Options{Auras: AuraParams{Enabled: true, Legend: AuraLegendInside}}, 400, 300)
	nodes := []NodeSpec{
		{Id: 1, Pinned: true, Auras: []string{"alpha"}},
		{Id: 2, Pinned: true, PinX: 100, Auras: []string{"beta"}},
	}
	s.frame(nodes, nil)
	require.Equal(t, []string{"alpha", "beta"}, keys(s.v.AuraLegendItems()))
}

// The mixed case: a declaration where some nodes carry coordinates and some
// carry none. ADR-0224 §SD10 already gives it — a declared pin is fixed and
// everything else still feels it — and it is what lets a map-hosted graph
// hold nodes the data never located.
func TestUnpinnedNodesAreLaidOutAmongPinnedOnes(t *testing.T) {
	s := newHostScene(t, Options{Layout: LayoutForceDirected}, 400, 300)
	nodes := []NodeSpec{
		{Id: 1, Pinned: true, PinX: 0, PinY: 0},
		{Id: 2, Pinned: true, PinX: 200, PinY: 0},
		{Id: 3, Pinned: true, PinX: 100, PinY: 150},
		{Id: 4}, // no coordinates at all
	}
	edges := []EdgeSpec{{From: 4, To: 1}, {From: 4, To: 2}, {From: 4, To: 3}}
	s.frame(nodes, edges)

	x0, y0, ok := s.v.NodePosition(4)
	require.True(t, ok, "the unlocated node was placed")

	// The pinned three are held exactly where the declaration puts them, and
	// the metrics say so.
	for _, tc := range [][3]float32{{1, 0, 0}, {2, 200, 0}, {3, 100, 150}} {
		x, y, ok := s.v.NodePosition(uint64(tc[0]))
		require.True(t, ok)
		require.Equal(t, [2]float32{tc[1], tc[2]}, [2]float32{x, y}, "pinned node %v", tc[0])
	}
	require.Equal(t, uint32(3), s.v.Metrics().PinnedCount)
	require.Equal(t, uint32(4), s.v.Metrics().NodeCount)

	// Several force steps: only the free node may move, and it does.
	for range 40 {
		s.frame(nodes, edges)
	}
	for _, tc := range [][3]float32{{1, 0, 0}, {2, 200, 0}, {3, 100, 150}} {
		x, y, _ := s.v.NodePosition(uint64(tc[0]))
		require.Equal(t, [2]float32{tc[1], tc[2]}, [2]float32{x, y}, "pinned node %v never moved", tc[0])
	}
	x1, y1, _ := s.v.NodePosition(4)
	require.False(t, x0 == x1 && y0 == y1, "the free node was moved by the step")

	// It settles inside the triangle its three neighbours make, rather than
	// flying off: the attraction to all three is what places it.
	require.Greater(t, x1, float32(-100))
	require.Less(t, x1, float32(300))
	require.Greater(t, y1, float32(-150))
	require.Less(t, y1, float32(300))
}

func TestSetHostCameraRefreshesThePaintButNotThePick(t *testing.T) {
	s := newHostScene(t, Options{}, 400, 300)
	s.host.Camera = cam.Camera{Zoom: 1}
	s.frame(hostedNodes(), nil)

	// The pick runs against the camera HostedInput was given...
	s.v.HostedInput(s.host)
	require.Equal(t, float32(1), s.v.cam.Zoom)
	// ...and the paint against the one the host has by the time it draws,
	// which is what keeps the graph from sliding against it under a pan.
	s.v.SetHostCamera(cam.Camera{Zoom: 4, PanX: 7, PanY: 9})
	require.Equal(t, float32(4), s.v.cam.Zoom)
	s.v.HostedPaint(hostedNodes(), nil)
	zoom, panX, panY := s.v.Camera()
	require.Equal(t, [3]float32{4, 7, 9}, [3]float32{zoom, panX, panY})
	s.sm.ScriptReset()

	// Outside a hosted render it does nothing: Render owns its camera.
	s.v.SetCamera(2, 0, 0)
	s.v.SetHostCamera(cam.Camera{Zoom: 8})
	zoom, _, _ = s.v.Camera()
	require.Equal(t, float32(2), zoom)
}

// A world fixed at a reference zoom is what makes a layout among pinned nodes
// stable: the pinned geometry does not change when the host's camera does, so
// the free node settles once (ADR-0228 §SD3a).
func TestAFixedWorldKeepsTheLayoutStillWhileTheHostZooms(t *testing.T) {
	s := newHostScene(t, Options{
		Layout: LayoutForceDirected,
		Force:  ForceParams{PauseOnSettle: true},
	}, 400, 300)
	nodes := []NodeSpec{
		{Id: 1, Pinned: true, PinX: 0, PinY: 0},
		{Id: 2, Pinned: true, PinX: 200, PinY: 0},
		{Id: 3}, // unlocated
	}
	edges := []EdgeSpec{{From: 3, To: 1}, {From: 3, To: 2}}
	settled := false
	for range 4000 {
		s.frame(nodes, edges)
		if s.v.IsSettled() {
			settled = true
			break
		}
	}
	require.True(t, settled, "the layout reached equilibrium")
	x0, y0, ok := s.v.NodePosition(3)
	require.True(t, ok)

	// The host zooms right in and pans. World positions are unchanged, so
	// there is nothing new to solve: the camera must not even wake the
	// settled layout, let alone move the free node.
	s.host.Camera = cam.Camera{Zoom: 32, PanX: -1000, PanY: -400}
	for range 20 {
		s.frame(nodes, edges)
	}
	require.True(t, s.v.IsSettled(), "a camera change is not a wake condition")
	x1, y1, _ := s.v.NodePosition(3)
	require.Equal(t, [2]float32{x0, y0}, [2]float32{x1, y1}, "the free node ignored the host's zoom")
}

// The frame a whole graph arrives, every slot is new, so a new node has no
// already-placed neighbour to be seated beside — except a pinned one, whose
// position the caller gave. Without that, an unlocated node starts in the box
// at the world origin, which is nowhere near the data when the world is
// projected coordinates, and crawls back at MaxStep a frame.
func TestANewNodeIsSeatedBesideItsPinnedNeighboursOnTheFirstFrame(t *testing.T) {
	s := newHostScene(t, Options{Layout: LayoutForceDirected}, 400, 300)
	// Pins far from the origin, as projected map coordinates are.
	const ox, oy float32 = 19713, 13181
	nodes := []NodeSpec{
		{Id: 1, Pinned: true, PinX: ox, PinY: oy},
		{Id: 2, Pinned: true, PinX: ox + 120, PinY: oy},
		{Id: 3}, // unlocated, declared in the same frame
	}
	edges := []EdgeSpec{{From: 3, To: 1}, {From: 3, To: 2}}

	s.frame(nodes, edges)
	x, y, ok := s.v.NodePosition(3)
	require.True(t, ok)

	// It lands among its pinned neighbours on the very first frame, not at
	// the origin twenty thousand units away.
	require.InDelta(t, ox+60, x, 400, "placed near its pinned neighbours")
	require.InDelta(t, oy, y, 400)
	dist := math.Hypot(float64(x), float64(y))
	require.Greater(t, dist, 10000.0, "and nowhere near the world origin")
}
