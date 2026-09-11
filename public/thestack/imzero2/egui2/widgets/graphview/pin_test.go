package graphview

import (
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func TestDeclaredPinsPlaceAndFixTheNode(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1, Pinned: true, PinX: 100, PinY: 50}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	placeRandom(&g, g.allSlots())
	g.applyPins(-1)
	s1, s2 := g.slot[1], g.slot[2]
	require.Equal(t, [2]float32{100, 50}, [2]float32{g.x[s1], g.y[s1]}, "the pin overrides the placement")
	require.True(t, g.fixed[s1])
	require.False(t, g.fixed[s2])
	fs := forceState{lastDisp: nan32}
	for range 10 {
		g.applyPins(-1)
		fs.step(&g, 500, 500, ForceParams{}.withDefaults(), 0)
	}
	require.Equal(t, [2]float32{100, 50}, [2]float32{g.x[s1], g.y[s1]}, "ten steps later the pinned node has not moved")

	// The drag in flight keeps its node off the pin for the frame.
	g.x[s1] = 7
	g.applyPins(s1)
	require.Equal(t, float32(7), g.x[s1])
	require.True(t, g.fixed[s1], "but it is still fixed against the force step")

	// Undeclaring the pin releases the node.
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	g.applyPins(-1)
	require.False(t, g.fixed[s1])
	require.False(t, g.isPinned(int(s1)))
}

func TestPinNodeHoldsUntilUnpinned(t *testing.T) {
	v := New(nil, "t", Options{})
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	v.PinNode(1, 30, 40)
	v.g.applyPins(-1)
	require.True(t, v.IsPinned(1))
	require.False(t, v.IsPinned(2))
	x, y, _ := v.NodePosition(1)
	require.Equal(t, [2]float32{30, 40}, [2]float32{x, y})
	v.fs.step(&v.g, 500, 500, ForceParams{}.withDefaults(), 0)
	x, y, _ = v.NodePosition(1)
	require.Equal(t, [2]float32{30, 40}, [2]float32{x, y})
	v.UnpinNode(1)
	v.g.applyPins(-1)
	require.False(t, v.IsPinned(1))
	v.fs.step(&v.g, 500, 500, ForceParams{}.withDefaults(), 0)
	x, _, _ = v.NodePosition(1)
	require.NotEqual(t, float32(30), x, "released, the node moves again")
	v.PinNode(9, 0, 0)
	require.False(t, v.IsPinned(9), "an unknown id is ignored")
}

func TestPinOnDragHoldsWhereTheNodeWasDropped(t *testing.T) {
	v := New(nil, "t", Options{PinOnDrag: true})
	v.g.reconcile([]NodeSpec{{Id: 1}}, nil)
	v.g.x[0], v.g.y[0] = 100, 100
	v.style = v.Opts.Style.withDefaults()
	var wheel c.CanvasWheelValue
	wheel.Zoom = 1
	down := c.IsPointerButtonDownResponseFlags
	// Press on the node, drag 40 px right, release.
	v.applyInput(800, 600, 100, 100, true, true, c.DragStartedResponseFlags|down, wheel, c.ModifiersValue{})
	require.Len(t, v.Events(), 2, "hover enter and drag start")
	require.Equal(t, EventKindNodeDragStart, v.Events()[1].Kind)
	v.g.applyPins(v.dragSlot())
	require.True(t, v.g.fixed[0], "fixed while dragged")
	v.applyInput(800, 600, 140, 100, true, true, c.DraggedResponseFlags|down, wheel, c.ModifiersValue{})
	v.events = v.events[:0]
	// The pointer moved another 20 px on the release frame; that motion counts.
	v.applyInput(800, 600, 160, 100, true, true, c.DragStoppedResponseFlags, wheel, c.ModifiersValue{})
	require.True(t, v.IsPinned(1), "held where it was dropped")
	require.Equal(t, float32(160), v.g.x[0])
	var end *Event
	for i := range v.events {
		if v.events[i].Kind == EventKindNodeDragEnd {
			end = &v.events[i]
		}
	}
	require.NotNil(t, end)
	require.Equal(t, [2]float32{160, 100}, [2]float32{end.X, end.Y}, "the drag-end event carries the drop position")
	v.g.applyPins(-1)
	v.fs.step(&v.g, 500, 500, ForceParams{}.withDefaults(), 0.3)
	require.Equal(t, float32(160), v.g.x[0], "centre gravity does not pull a held node")
	require.Equal(t, float32(0), v.fs.lastDisp, "nothing could move, so the step reports zero motion, not NaN")
	require.True(t, v.fs.settled(1e-3))
}
