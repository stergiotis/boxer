package imageview

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// viewerFrame renders a viewer of a w × h image with the colour bar,
// readout and histogram out of the way, so the canvas is the whole 400 ×
// 300.
func viewerWith(t *testing.T, opts Options, w, h int) (*Viewer, func() Events) {
	t.Helper()
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	opts.HideColorBar, opts.HideReadout, opts.HideHistogram = true, true, true
	v := New(c.NewWidgetIdStack(), "", opts)
	t.Cleanup(v.Close)
	v.SetImage(grayImage(t, w, h))
	return v, func() Events {
		ev := v.Render(400, 300)
		sm.ScriptReset()
		return ev
	}
}

func TestTheViewIsReadAndSetInPixels(t *testing.T) {
	v, frame := viewerWith(t, Options{}, 1000, 600)
	_, _, _, ok := v.View()
	assert.False(t, ok, "not laid out before the first frame")
	frame()
	x, y, mag, ok := v.View()
	require.True(t, ok)
	assert.InDelta(t, 500, x, 1e-6, "fitted: the centre of the image")
	assert.InDelta(t, 300, y, 1e-6)
	assert.InDelta(t, 0.4, mag, 1e-9, "1000 wide into 400")

	v.SetView(250.5, 100.5, 2)
	x, y, mag, _ = v.View()
	assert.InDelta(t, 250.5, x, 1e-6)
	assert.InDelta(t, 100.5, y, 1e-6)
	assert.InDelta(t, 2, mag, 1e-9)
	assert.True(t, frame().ViewChanged)
	assert.False(t, frame().ViewChanged, "a frame in which nothing moved")

	v.SetView(0, 0, 1e9)
	_, _, mag, _ = v.View()
	assert.InDelta(t, math.Pow(2, maxZoom), mag, 1e-9, "held within the zooms allowed")
}

func TestAViewSetBeforeTheFirstFrameReplacesTheFit(t *testing.T) {
	v, frame := viewerWith(t, Options{}, 1000, 600)
	v.SetView(100, 50, 4)
	frame()
	x, y, mag, ok := v.View()
	require.True(t, ok)
	assert.InDelta(t, 100, x, 1e-6)
	assert.InDelta(t, 50, y, 1e-6)
	assert.InDelta(t, 4, mag, 1e-9)

	// A new source that keeps the view keeps it; one that does not fits.
	require.NoError(t, v.SetSource(newPatternSource(1000, 600, 1, 2), true))
	frame()
	_, _, mag, _ = v.View()
	assert.InDelta(t, 4, mag, 1e-9)
}

func TestRevealMovesOnlyAsMuchAsItMust(t *testing.T) {
	v, frame := viewerWith(t, Options{}, 4000, 3000)
	frame()
	v.SetView(200, 150, 1)
	frame()

	// In view already: nothing moves.
	v.Reveal(image.Rect(150, 100, 250, 200))
	x, y, mag, _ := v.View()
	assert.Equal(t, [3]float64{200, 150, 1}, [3]float64{x, y, mag})

	// Out of view but small enough: centred, the magnification kept.
	v.Reveal(image.Rect(3000, 2000, 3100, 2050))
	x, y, mag, _ = v.View()
	assert.InDelta(t, 3050, x, 1e-6)
	assert.InDelta(t, 2025, y, 1e-6)
	assert.InDelta(t, 1, mag, 1e-9)

	// Too large to fit: the view zooms out until it does.
	big := image.Rect(0, 0, 2000, 1000)
	v.Reveal(big)
	_, _, mag, _ = v.View()
	assert.Less(t, mag, 1.0)
	assert.True(t, big.In(v.visiblePixels()), "the whole rectangle is in view")
}

// The overlay paints in canvas coordinates through a projector that knows
// image pixels; the region is drawn over it.
func TestTheOverlayIsGivenAProjectorInImagePixels(t *testing.T) {
	var got []Projector
	v, frame := viewerWith(t, Options{Overlay: func(p Projector) { got = append(got, p) }}, 1000, 600)
	frame()
	v.SetView(500, 300, 2)
	frame()
	require.NotEmpty(t, got)
	p := got[len(got)-1]
	cx, cy := p.ToCanvas(500, 300)
	assert.InDelta(t, 200, cx, 0.5, "the view's centre is the canvas's")
	assert.InDelta(t, 150, cy, 0.5)
	x, y := p.ToPixel(cx+20, cy-10)
	assert.InDelta(t, 510, x, 0.5, "20 canvas pixels are 10 image pixels at 2×")
	assert.InDelta(t, 295, y, 0.5)
	assert.Equal(t, 2.0, p.Magnification())
	assert.Equal(t, image.Rect(400, 225, 600, 375), p.Visible())
}

// While a region is dragged its rectangle is reported frame by frame.
func TestTheDragIsReportedWhileItIsUnderWay(t *testing.T) {
	v, _ := viewerWith(t, Options{}, 1000, 600)
	sm := c.CurrentApplicationState.StateManager
	v.Render(400, 300)
	sm.ScriptReset()
	v.SetView(200, 150, 1)
	v.SetSelecting(true)
	v.Render(400, 300)
	sm.ScriptReset()
	canvas, area := mapHandles(v)
	frame := func(x, y float32, flags c.ResponseFlagsE) Events {
		sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: x, PosY: y})
		sm.ScriptCanvasCursor(area, c.CanvasCursorValue{PosX: 100, PosY: 50})
		sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
		sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
		sm.ScriptResponse(area, c.ContainsPointerResponseFlags|flags)
		ev := v.Render(400, 300)
		sm.ScriptReset()
		return ev
	}
	down := c.IsPointerButtonDownResponseFlags
	frame(100, 50, c.DragStartedResponseFlags|down)
	frame(160.5, 90.5, c.DraggedResponseFlags|down)
	ev := frame(160.5, 90.5, c.DraggedResponseFlags|down)
	require.True(t, ev.DragOk)
	// Canvas (100, 50) is pixel (100, 50) at 1× centred on (200, 150).
	assert.Equal(t, image.Rect(100, 50, 161, 91), ev.Drag)
	assert.False(t, ev.RegionChanged, "not a region until it is released")
}

// With WheelScrolls the wheel moves the view and keeps the magnification.
func TestTheWheelScrollsWhenAsked(t *testing.T) {
	v, _ := viewerWith(t, Options{WheelScrolls: true}, 1000, 3000)
	sm := c.CurrentApplicationState.StateManager
	v.Render(400, 300)
	sm.ScriptReset()
	v.SetView(500, 200, 1)
	v.Render(400, 300)
	sm.ScriptReset()
	canvas, area := mapHandles(v)
	nan := float32(math.NaN())
	sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: nan, PosY: nan})
	sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags)
	sm.ScriptResponse(area, c.ContainsPointerResponseFlags)
	sm.ScriptCanvasWheel(canvas, c.CanvasWheelValue{ScrollY: -120, Zoom: 1, HoverX: 200, HoverY: 150})
	ev := v.Render(400, 300)
	sm.ScriptReset()
	time.Sleep(time.Millisecond)
	_ = ev
	x, y, mag, _ := v.View()
	assert.InDelta(t, 500, x, 1e-6)
	assert.InDelta(t, 320, y, 1e-6, "120 rows further down")
	assert.InDelta(t, 1, mag, 1e-9)
}

// mapHandles are the map's handles, derived under the viewer's own scope as
// its Render derives them.
func mapHandles(v *Viewer) (canvas, area widgethandle.WidgetHandle) {
	for range c.IdScope(v.ids.PrepareStr(v.key)) {
		canvas, area = v.m.Handles()
	}
	return
}
