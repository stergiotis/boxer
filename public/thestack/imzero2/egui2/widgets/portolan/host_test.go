package portolan

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// A hosted guest asks for the handles every frame, before the map renders
// and derives the same ids itself, so they must be a pure function of the
// stack and the names rather than of a call counter (ADR-0228 §SD1).
func TestHandlesAreStableAndDoNotConsumeTheStack(t *testing.T) {
	ids := c.NewWidgetIdStack()
	m := New(ids, "", Options{NoTiles: true})
	c1, a1 := m.Handles()
	c2, a2 := m.Handles()
	require.Equal(t, c1, c2, "a second call gives the same canvas handle")
	require.Equal(t, a1, a2)
	require.NotEqual(t, c1, a1, "the canvas and the area are distinct")
}

// The veto is per frame: a host that is told once must not stand down for
// ever (ADR-0228 §SD2).
func TestPointerVetoIsClearedByTheRenderItAppliesTo(t *testing.T) {
	ids := c.NewWidgetIdStack()
	m := New(ids, "", Options{NoTiles: true})
	require.False(t, m.pointerVeto)
	m.SetPointerVeto(true)
	require.True(t, m.pointerVeto)
	m.SetPointerVeto(false)
	require.False(t, m.pointerVeto, "a host told false stands up again")
}

// The handles are the ones the map's own frame reads: a pointer scripted
// into the registers under them is the pointer the next Render sees. They
// are derived in a different place than the frame derives them, inside a
// different nesting of scopes, so only a rendered frame can say they agree.
func TestHandlesAreTheOnesARenderReads(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	m := New(ids, "", Options{NoTiles: true, Center: LL(10, 20), Zoom: 3})
	defer m.Close()
	m.Render(400, 300, nil)
	sm.ScriptReset()
	_, ok := m.Hover()
	require.False(t, ok, "no pointer yet")

	canvas, area := m.Handles()
	nan := float32(math.NaN())
	sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: nan, PosY: nan})
	sm.ScriptPointer(c.PointerValue{X: 200, Y: 150, Valid: true})
	sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
	sm.ScriptResponse(area, c.ContainsPointerResponseFlags|c.PrimaryClickedResponseFlags)
	ev := m.Render(400, 300, nil)
	sm.ScriptReset()

	hover, ok := m.Hover()
	require.True(t, ok, "the pointer scripted under Handles() reached the map")
	// Within a pixel: the view rounds its pixel origin, and a pixel at zoom
	// 3 is about 0.18 degrees.
	require.InDelta(t, 10, hover.Lat, 0.2, "the centre of the canvas is the centre of the view")
	require.InDelta(t, 20, hover.Lng, 0.2)
	require.True(t, ev.ClickedOk)
}

// With WheelPans the wheel moves the view the way a page scrolls — a scroll
// down shows what lies below — and leaves the zoom alone; the zoom a
// modifier makes of the wheel, which arrives as a zoom, still zooms.
func TestTheWheelPansWhenAsked(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	for _, pans := range []bool{false, true} {
		ids := c.NewWidgetIdStack()
		m := New(ids, "", Options{NoTiles: true, CRS: Simple, Center: LL(-500, 500), Zoom: 0, WheelPans: pans, NoZoomAnimation: true,
			Handlers: HandlerOptions{WheelDebounce: time.Nanosecond}})
		m.Render(400, 300, nil)
		sm.ScriptReset()
		canvas, area := m.Handles()
		nan := float32(math.NaN())
		frame := func(w c.CanvasWheelValue) {
			sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: nan, PosY: nan})
			sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
			sm.ScriptResponse(area, c.ContainsPointerResponseFlags)
			sm.ScriptCanvasWheel(canvas, w)
			m.Render(400, 300, nil)
			sm.ScriptReset()
		}
		if !pans {
			// The control: a scroll up zooms a map in (zoom 0 is its least).
			frame(c.CanvasWheelValue{ScrollY: 40, Zoom: 1, HoverX: 200, HoverY: 150})
			time.Sleep(time.Millisecond)
			frame(c.CanvasWheelValue{Zoom: 1, HoverX: nan, HoverY: nan})
			assert.Greater(t, m.View().Zoom(), 0.0, "the wheel zooms a map")
			m.Close()
			continue
		}
		frame(c.CanvasWheelValue{ScrollY: -40, Zoom: 1, HoverX: 200, HoverY: 150})
		time.Sleep(time.Millisecond)
		frame(c.CanvasWheelValue{Zoom: 1, HoverX: nan, HoverY: nan})
		ctr := m.View().Center()
		assert.Equal(t, 0.0, m.View().Zoom(), "a wheel that pans leaves the zoom alone")
		assert.InDelta(t, -540, ctr.Lat, 1e-9, "a scroll down shows what lies below: 40 rows further down")
		assert.InDelta(t, 500, ctr.Lng, 1e-9)
		frame(c.CanvasWheelValue{ScrollX: 25, Zoom: 1, HoverX: 200, HoverY: 150})
		assert.InDelta(t, 475, m.View().Center().Lng, 1e-9, "sideways too")
		frame(c.CanvasWheelValue{Zoom: 2, HoverX: 200, HoverY: 150})
		assert.Greater(t, m.View().Zoom(), 0.0, "the modifier's zoom still zooms")
		m.Close()
	}
}

// The box being dragged is reported while it is dragged, not only when it
// is released.
func TestTheBoxIsReportedWhileItIsDragged(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	m := New(ids, "", Options{NoTiles: true, CRS: Simple, Center: LL(-150, 200), Zoom: 0})
	defer m.Close()
	m.SetBoxSelect(true)
	m.Render(400, 300, nil)
	sm.ScriptReset()
	canvas, area := m.Handles()
	frame := func(x, y float32, flags c.ResponseFlagsE) Events {
		sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{OriginX: 0, OriginY: 0, PosX: x, PosY: y})
		sm.ScriptCanvasCursor(area, c.CanvasCursorValue{OriginX: 0, OriginY: 0, PosX: 10, PosY: 20})
		sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
		sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
		sm.ScriptResponse(area, c.ContainsPointerResponseFlags|flags)
		ev := m.Render(400, 300, nil)
		sm.ScriptReset()
		return ev
	}
	down := c.IsPointerButtonDownResponseFlags
	ev := frame(10, 20, c.DragStartedResponseFlags|down)
	ev = frame(110, 70, c.DraggedResponseFlags|down)
	ev = frame(110, 70, c.DraggedResponseFlags|down)
	require.True(t, ev.BoxingOk, "the box is reported while it is dragged")
	assert.False(t, ev.SelectedOk)
	assert.InDelta(t, 10, ev.Boxing.GetWest(), 1e-9)
	assert.InDelta(t, 110, ev.Boxing.GetEast(), 1e-9)
	assert.InDelta(t, -20, ev.Boxing.GetNorth(), 1e-9)
	assert.InDelta(t, -70, ev.Boxing.GetSouth(), 1e-9)
	ev = frame(110, 70, c.DragStoppedResponseFlags)
	ev = frame(110, 70, 0)
	assert.False(t, ev.BoxingOk, "and no longer once released")
}
