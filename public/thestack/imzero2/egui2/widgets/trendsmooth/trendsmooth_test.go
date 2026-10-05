package trendsmooth

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderHeadless renders the control row for one frame under
// the discard channel and pins the W17 nil-input path (ADR-0267 W19).
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	st := New()
	res := Render(Input{Ids: ids, ScopeKey: "t", State: st})
	require.NoError(t, res.Err)
	assert.False(t, res.Changed)
	assert.ErrorIs(t, Render(Input{Ids: ids}).Err, ErrNeedsIdsAndState)
	assert.Equal(t, 0, ids.Depth(), "the id stack is left balanced")
}

// TestSetHalfWidthClamps pins the stepper's bounds.
func TestSetHalfWidthClamps(t *testing.T) {
	st := New()
	st.SetHalfWidth(10_000)
	assert.Equal(t, MaxHalfWidth, st.HalfWidth())
	st.SetHalfWidth(-5)
	assert.Greater(t, st.HalfWidth(), int32(0))
}
