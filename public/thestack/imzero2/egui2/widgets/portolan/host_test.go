package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
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
	t.Cleanup(scenetest.Install())
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
