package nav

import (
	"iter"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

type discardChannel struct{}

func (discardChannel) SyncMultiUseMsg(uint64, []byte) {}
func (discardChannel) SendSingleUseMsg([]byte)        {}
func (discardChannel) FlushMessages()                 {}
func (discardChannel) ReceiveMsg() iter.Seq[*runtime.Unmarshaller] {
	return func(func(*runtime.Unmarshaller) bool) {}
}

// The headless scene of ADR-0224's 2026-09-12 update, driven through the
// navigator: a double-click on a rendered node reaches Apply, the next
// frame renders the grown declaration, and a stub at the frontier lands on
// the pending list.
func TestSceneDoubleClickExpandsThroughApply(t *testing.T) {
	typed.SetCurrentFffiVar(runtime.NewFffi2[*runtime.Unmarshaller](discardChannel{}))
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	t.Cleanup(sm.ScriptReset)
	ids := c.NewWidgetIdStack()
	var canvas, area widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("nav")) {
		canvas = widgethandle.Make(ids.PrepareStr("graphview-canvas").Derive())
		area = widgethandle.Make(ids.PrepareStr("graphview-area").Derive())
	}
	gv := graphview.New(ids, "nav", graphview.Options{Layout: graphview.LayoutRadial, NodeClicking: true})

	nv := New(Options{Mode: ModeManual, ExpandDepth: 3})
	nv.AddNodes([]Node{{Spec: graphview.NodeSpec{Id: 1}}, {Spec: graphview.NodeSpec{Id: 2}}, {Spec: graphview.NodeSpec{Id: 3}, Stub: true}})
	nv.AddEdges([]graphview.EdgeSpec{{From: 1, To: 2}, {From: 2, To: 3}})
	nv.Show(1)

	frame := func() {
		ns, es := nv.Declare()
		gv.Opts.Radial.Centers = []uint64{1}
		gv.Render(ns, es, 400, 300)
		require.False(t, typed.HasErrors(), typed.GetError())
		for _, ev := range gv.Events() {
			nv.Apply(ev)
		}
		sm.ScriptReset()
	}
	frame()
	require.Equal(t, uint32(1), gv.Metrics().NodeCount)

	// Double-click node 1 where the last frame painted it.
	x, y, ok := gv.NodeCanvasPosition(1)
	require.True(t, ok)
	nan := float32(math.NaN())
	sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: nan, PosY: nan})
	sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
	sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
	sm.ScriptResponse(area, c.PrimaryClickedResponseFlags|c.DoubleClickedResponseFlags)
	frame()
	require.True(t, nv.Expanded(1))
	frame()
	require.Equal(t, uint32(3), gv.Metrics().NodeCount, "the expansion reached 2 and the stub 3")
	require.Equal(t, uint32(2), gv.Metrics().EdgeCount)
	require.Equal(t, []uint64{3}, nv.Pending(), "the stub sits one hop short of the depth, so it is wanted")
	_, _, ok = gv.NodeCanvasPosition(3)
	require.True(t, ok, "and it is on the picture meanwhile")
}
