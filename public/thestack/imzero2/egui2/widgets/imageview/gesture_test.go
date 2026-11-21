package imageview

import (
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Under DragGestures a drag reports where it began and where it stands, in
// continuous pixels and in the direction it was made, and its release ends
// the gesture without marking a region.
func TestADragGestureReportsBothEndsAndMarksNoRegion(t *testing.T) {
	v, _ := viewerWith(t, Options{DragGestures: true}, 1000, 600)
	sm := c.CurrentApplicationState.StateManager
	v.Render(400, 300)
	sm.ScriptReset()
	v.SetView(200, 150, 1)
	v.Render(400, 300)
	sm.ScriptReset()
	canvas, area := mapHandles(v)
	frame := func(x, y float32, flags c.ResponseFlagsE) Events {
		sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: x, PosY: y})
		sm.ScriptCanvasCursor(area, c.CanvasCursorValue{PosX: 160.5, PosY: 90.5})
		sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
		sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
		sm.ScriptResponse(area, c.ContainsPointerResponseFlags|flags)
		ev := v.Render(400, 300)
		sm.ScriptReset()
		return ev
	}
	down := c.IsPointerButtonDownResponseFlags
	// From (160.5, 90.5) up and to the left: the start is the larger corner.
	frame(160.5, 90.5, c.DragStartedResponseFlags|down)
	frame(100.25, 50.75, c.DraggedResponseFlags|down)
	ev := frame(100.25, 50.75, c.DraggedResponseFlags|down)
	require.True(t, ev.DragOk)
	assert.InDelta(t, 160.5, ev.DragFrom.X, 1e-9)
	assert.InDelta(t, 90.5, ev.DragFrom.Y, 1e-9)
	assert.InDelta(t, 100.25, ev.DragTo.X, 1e-9)
	assert.InDelta(t, 50.75, ev.DragTo.Y, 1e-9)
	assert.True(t, ev.HoverOk)
	assert.InDelta(t, 100.25, ev.HoverAt.X, 1e-9)
	ev = frame(100.25, 50.75, c.DragStoppedResponseFlags)
	ev2 := frame(100.25, 50.75, 0)
	ended := ev
	if !ended.DragEnded {
		ended = ev2
	}
	require.True(t, ended.DragEnded, "the release ends the gesture")
	assert.InDelta(t, 160.5, ended.DragEndFrom.X, 1e-9)
	assert.InDelta(t, 100.25, ended.DragEndTo.X, 1e-9)
	assert.False(t, ended.RegionChanged)
	_, marked := v.Region()
	assert.False(t, marked, "a gesture marks no region")

	// The same drag while Selecting without gestures marks the region, as
	// before.
	v.Opts.DragGestures = false
	v.SetSelecting(true)
	frame(160.5, 90.5, c.DragStartedResponseFlags|down)
	frame(100.25, 50.75, c.DraggedResponseFlags|down)
	frame(100.25, 50.75, c.DragStoppedResponseFlags)
	frame(100.25, 50.75, 0)
	r, marked := v.Region()
	assert.True(t, marked)
	assert.Equal(t, image.Rect(100, 50, 161, 91), r)
}
