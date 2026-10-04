package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoWindows(t *testing.T) (h *Inst, a, b WindowKeyT) {
	t.Helper()
	reg, _ := mkRegistryWithSingleton(t, "test.a", "test.b")
	h = NewInst(reg, zerolog.Nop())
	var err error
	a, err = h.Open("test.a")
	require.NoError(t, err)
	b, err = h.Open("test.b")
	require.NoError(t, err)
	return
}

// Raise, Place and ArrangeWindows queue for the next Frame and refuse a key
// that names no open window (ADR-0276 §SD5).
func TestWindowVerbsQueueAndRefuseUnknownKeys(t *testing.T) {
	h, a, b := twoWindows(t)

	require.NoError(t, h.Raise(b))
	assert.Equal(t, b, h.pendingRaise)
	assert.Error(t, h.Raise(99))

	r := rectAt(10, 40, 300, 200)
	require.NoError(t, h.Place(a, r))
	assert.Equal(t, r, h.pendingPlaces[a])
	assert.Error(t, h.Place(99, r))
	assert.Error(t, h.Place(a, Rect{}), "a placement without a size is refused")

	require.NoError(t, h.ArrangeWindows(ArrangeColumns, []WindowKeyT{a, b}))
	assert.Equal(t, ArrangeColumns, h.pendingArrange)
	assert.Equal(t, []WindowKeyT{a, b}, h.pendingArrangeKeys)

	// A list with one wrong key queues nothing.
	h.pendingArrange, h.pendingArrangeKeys = ArrangeNone, nil
	assert.Error(t, h.ArrangeWindows(ArrangeTile, []WindowKeyT{a, 99}))
	assert.Equal(t, ArrangeNone, h.pendingArrange)
	assert.Error(t, h.ArrangeWindows(ArrangeNone, nil))
	assert.Error(t, h.ArrangeWindows(ArrangeE(200), nil))

	// A closing window is no longer a target.
	h.Close(b, "test")
	assert.Error(t, h.Raise(b))
}

// A placement marks its window and ends an arrangement in progress, whose
// next pass would move the window again.
func TestApplyPlacesEndsAnArrangement(t *testing.T) {
	h, a, b := twoWindows(t)
	h.arranging = &arrangeRun{cmd: ArrangeTile}
	h.windows[0].maximized = true
	r := rectAt(10, 40, 300, 200)
	h.applyPlaces(map[WindowKeyT]Rect{a: r}, h.windows)
	assert.Nil(t, h.arranging)
	assert.True(t, h.windows[0].placePending)
	assert.Equal(t, r, h.windows[0].place)
	assert.False(t, h.windows[0].maximized, "a placement ends maximization")
	assert.False(t, h.windows[1].placePending, "window %d was not placed", b)
}
