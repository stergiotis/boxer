package graphview

import (
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// A layout still moving asks for the next frame, so its animation runs
// under the reactive cadence without input; a held or static one stops
// asking once the fit latch has counted its frames.
func TestWantsFrame_WhileTheLayoutMoves(t *testing.T) {
	t.Cleanup(scenetest.Install())
	nodes := []NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}
	edges := []EdgeSpec{{From: 1, To: 2}, {From: 2, To: 3}}
	settle := func(v *View) {
		for range fitMinFrames + 2 {
			v.Render(nodes, edges, 400, 300)
		}
	}

	moving := New(c.NewWidgetIdStack(), "moving", Options{Layout: LayoutForceDirected})
	settle(moving)
	require.False(t, moving.IsSettled())
	require.True(t, moving.wantsFrame(), "a layout still moving wants the next frame")

	static := New(c.NewWidgetIdStack(), "static", Options{Layout: LayoutRandom})
	static.Render(nodes, edges, 400, 300)
	require.True(t, static.wantsFrame(), "the fit latch counts frames even when the layout is settled")
	settle(static)
	require.False(t, static.wantsFrame(), "a static layout past its fit wants none")

	held := New(c.NewWidgetIdStack(), "held", Options{Layout: LayoutForceDirected, Force: ForceParams{Paused: true}})
	settle(held)
	require.False(t, held.wantsFrame(), "a paused layout past its fit wants none")
}

// The request reaches the wire: a moving layout sends one message more
// per frame than the same picture held.
func TestStepAndPaint_RequestsARepaintWhileMoving(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	nodes := []NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}
	edges := []EdgeSpec{{From: 1, To: 2}, {From: 2, To: 3}}
	frame := func(o Options) int {
		v := New(c.NewWidgetIdStack(), "count", o)
		for range fitMinFrames + 2 {
			v.Render(nodes, edges, 400, 300)
			reset()
		}
		zero()
		v.Render(nodes, edges, 400, 300)
		reset()
		return messages()
	}
	held := frame(Options{Layout: LayoutForceDirected, Force: ForceParams{Paused: true}})
	moving := frame(Options{Layout: LayoutForceDirected})
	require.Equal(t, held+1, moving)
}
