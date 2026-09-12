package portolan

import (
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// A hosted guest asks for the handles every frame, before the map renders
// and derives the same ids itself, so they must be a pure function of the
// stack and the names rather than of a call counter (ADR-0228 §SD1).
func TestHandlesAreStableAndDoNotConsumeTheStack(t *testing.T) {
	ids := c.NewWidgetIdStack()
	m := New(ids, Options{NoTiles: true})
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
	m := New(ids, Options{NoTiles: true})
	require.False(t, m.pointerVeto)
	m.SetPointerVeto(true)
	require.True(t, m.pointerVeto)
	m.SetPointerVeto(false)
	require.False(t, m.pointerVeto, "a host told false stands up again")
}
