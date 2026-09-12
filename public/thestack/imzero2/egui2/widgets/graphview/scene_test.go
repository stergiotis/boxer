package graphview

import (
	"iter"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// discardChannel is a fffi2 channel with no host behind it: every paint
// command is dropped and nothing is ever received. It is what lets Render
// run end to end in a test, with the input registers scripted.
type discardChannel struct{}

func (discardChannel) SyncMultiUseMsg(uint64, []byte) {}
func (discardChannel) SendSingleUseMsg([]byte)        {}
func (discardChannel) FlushMessages()                 {}
func (discardChannel) ReceiveMsg() iter.Seq[*runtime.Unmarshaller] {
	return func(func(*runtime.Unmarshaller) bool) {}
}

// scene is a headless widget under a scripted state manager. The handles
// are derived the way Render derives them, from the same id stack, so a
// scripted register lands on the widget's own canvas and area.
type scene struct {
	t      *testing.T
	ids    *c.WidgetIdStack
	sm     *c.StateManager
	v      *View
	canvas widgethandle.WidgetHandle
	area   widgethandle.WidgetHandle
	w, h   float32
}

func newScene(t *testing.T, key string, o Options, w, h float32) *scene {
	typed.SetCurrentFffiVar(runtime.NewFffi2[*runtime.Unmarshaller](discardChannel{}))
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	t.Cleanup(sm.ScriptReset)
	ids := c.NewWidgetIdStack()
	s := &scene{t: t, ids: ids, sm: sm, w: w, h: h}
	for range c.IdScope(ids.PrepareStr(key)) {
		s.canvas = widgethandle.Make(ids.PrepareStr("graphview-canvas").Derive())
		s.area = widgethandle.Make(ids.PrepareStr("graphview-area").Derive())
	}
	s.v = New(ids, key, o)
	return s
}

// frame renders one frame against the scripted registers, then clears them
// as the frame boundary would, and returns the events the frame produced.
func (s *scene) frame(nodes []NodeSpec, edges []EdgeSpec) []Event {
	s.v.Render(nodes, edges, s.w, s.h)
	require.False(s.t, typed.HasErrors(), typed.GetError())
	s.sm.ScriptReset()
	return slices.Clone(s.v.Events())
}

// pointerAt scripts the pointer over the canvas at canvas pixel (x, y): the
// canvas's R24 row with its origin at the screen origin, the global pointer,
// and the canvas containing the pointer.
func (s *scene) pointerAt(x, y float32, area c.ResponseFlagsE) {
	nan := float32(math.NaN())
	s.sm.ScriptCanvasCursor(s.canvas, c.CanvasCursorValue{OriginX: 0, OriginY: 0, PosX: nan, PosY: nan})
	s.sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
	s.sm.ScriptResponse(s.canvas, c.ContainsPointerResponseFlags)
	s.sm.ScriptResponse(s.area, area)
}

func TestSceneRendersFitsClicksAndZooms(t *testing.T) {
	s := newScene(t, "scene", Options{
		Layout: LayoutRandom, NodeClicking: true, NodeSelection: true, EdgeClicking: true, BackgroundClicking: true,
	}, 400, 300)
	nodes := []NodeSpec{{Id: 1, Label: "a"}, {Id: 2, Label: "b"}, {Id: 3, Label: "c"}}
	edges := []EdgeSpec{{From: 1, To: 2, Id: 5}, {From: 2, To: 3}}

	// Frame 1: placement and the one-shot fit; no input.
	evs := s.frame(nodes, edges)
	require.Empty(t, evs)
	m := s.v.Metrics()
	require.Equal(t, uint32(3), m.NodeCount)
	require.Equal(t, uint32(2), m.EdgeCount)
	require.True(t, m.CameraMoved, "the first frame fits")
	_, _, ok := s.v.CanvasScreenOrigin()
	require.False(t, ok, "no canvas row has been scripted yet")

	// Frame 2: click node 1 where the last frame painted it.
	x1, y1, ok := s.v.NodeCanvasPosition(1)
	require.True(t, ok)
	s.pointerAt(x1, y1, c.PrimaryClickedResponseFlags)
	evs = s.frame(nodes, edges)
	require.Equal(t, []EventKindE{EventKindNodeHoverEnter, EventKindNodeClick, EventKindNodeSelect}, kinds(evs))
	require.Equal(t, uint64(1), evs[1].Node)
	require.Equal(t, []uint64{1}, slices.Collect(s.v.SelectedNodes()))
	_, _, ok = s.v.CanvasScreenOrigin()
	require.True(t, ok)
	id, ok := s.v.HoveredNode()
	require.True(t, ok)
	require.Equal(t, uint64(1), id)

	// Frame 3: a secondary click on empty canvas reports the world position
	// and leaves the selection alone.
	x1, y1, _ = s.v.NodeCanvasPosition(1)
	bx, by := float32(2), float32(2)
	if math.Hypot(float64(bx-x1), float64(by-y1)) < 20 {
		bx, by = s.w-2, s.h-2
	}
	s.pointerAt(bx, by, c.SecondaryClickedResponseFlags)
	evs = s.frame(nodes, edges)
	require.Equal(t, []EventKindE{EventKindNodeHoverLeave, EventKindBackgroundSecondaryClick}, kinds(evs))
	wx, wy := s.v.CanvasToWorld(bx, by)
	require.Equal(t, [2]float32{wx, wy}, [2]float32{evs[1].X, evs[1].Y})
	require.Equal(t, []uint64{1}, slices.Collect(s.v.SelectedNodes()))

	// Frames 4 to 12: the fit latch is still armed on a static layout until
	// fitMinFrames have passed; a wheel zoom releases it and doubles the
	// zoom around the pointer.
	for range fitMinFrames {
		s.frame(nodes, edges)
	}
	zoom0, _, _ := s.v.Camera()
	s.pointerAt(200, 150, 0)
	s.sm.ScriptCanvasWheel(s.canvas, c.CanvasWheelValue{Zoom: 2, HoverX: 200, HoverY: 150})
	s.frame(nodes, edges)
	zoom1, _, _ := s.v.Camera()
	require.InDelta(t, zoom0*2, zoom1, 1e-5)
	require.True(t, s.v.Metrics().CameraMoved)
	s.frame(nodes, edges)
	zoom2, _, _ := s.v.Camera()
	require.Equal(t, zoom1, zoom2, "manual zoom sticks: the latch is released")
	require.False(t, s.v.Metrics().CameraMoved)

	// FitNodes frames node 2 at the canvas centre on the next frame.
	s.v.FitNodes([]uint64{2})
	s.frame(nodes, edges)
	x2, y2, _ := s.v.NodeCanvasPosition(2)
	require.InDelta(t, 200, x2, 1e-2)
	require.InDelta(t, 150, y2, 1e-2)

	// A node that vanishes leaves the selection; the survivors keep their
	// positions.
	px, py, _ := s.v.NodePosition(2)
	s.frame(nodes[1:], edges[1:])
	require.Empty(t, slices.Collect(s.v.SelectedNodes()))
	qx, qy, ok := s.v.NodePosition(2)
	require.True(t, ok)
	require.Equal(t, [2]float32{px, py}, [2]float32{qx, qy})
}

func TestSceneForceLayoutSettlesAndPauses(t *testing.T) {
	s := newScene(t, "force", Options{
		Layout: LayoutForceDirectedCG,
		Force:  ForceParams{PauseOnSettle: true, Epsilon: 0.5},
	}, 500, 500)
	var nodes []NodeSpec
	var edges []EdgeSpec
	for i := uint64(1); i <= 12; i++ {
		nodes = append(nodes, NodeSpec{Id: i})
		if i > 1 {
			edges = append(edges, EdgeSpec{From: (i-1)/2 + 1, To: i})
		}
	}
	s.v.FastForward(50)
	for range 400 {
		s.frame(nodes, edges)
		if s.v.Metrics().Paused {
			break
		}
	}
	m := s.v.Metrics()
	require.True(t, m.Paused, "the tree came to rest and was held")
	require.True(t, m.Settled)
	steps := m.Steps
	s.frame(nodes, edges)
	require.Equal(t, steps, s.v.Metrics().Steps, "held: no step per frame")

	// A drag on a node wakes it: press, move, release over three frames.
	x, y, _ := s.v.NodeCanvasPosition(3)
	down := c.IsPointerButtonDownResponseFlags
	s.pointerAt(x, y, c.DragStartedResponseFlags|down)
	evs := s.frame(nodes, edges)
	require.Contains(t, kinds(evs), EventKindNodeDragStart)
	s.pointerAt(x+30, y, c.DraggedResponseFlags|down)
	s.frame(nodes, edges)
	s.pointerAt(x+30, y, c.DragStoppedResponseFlags)
	evs = s.frame(nodes, edges)
	require.Contains(t, kinds(evs), EventKindNodeDragEnd)
	require.False(t, s.v.Metrics().Paused, "moving again after the drag")
	require.Greater(t, s.v.Metrics().Steps, steps)
}

func TestSceneAurasAndLegendRender(t *testing.T) {
	s := newScene(t, "auras", Options{
		Layout: LayoutRandom,
		Auras:  AuraParams{Enabled: true, Legend: true},
	}, 400, 300)
	nodes := []NodeSpec{{Id: 1, Auras: []string{"a"}}, {Id: 2, Auras: []string{"a", "b"}}, {Id: 3, Auras: []string{"b"}}}
	s.frame(nodes, nil)
	require.Equal(t, []string{"a", "b"}, slices.Collect(s.v.AuraIds()))
	require.Greater(t, len(s.v.auraOrder), 0, "two auras were contoured")
	s.v.HideAura("b")
	s.frame(nodes, nil)
	require.True(t, s.v.AuraHidden("b"))
	require.Equal(t, 1, len(s.v.legendItems)-1, "both rows are listed, the hidden one dimmed")
}
