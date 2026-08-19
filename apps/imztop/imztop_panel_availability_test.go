package imztop

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stretchr/testify/assert"
)

// followRange is what makes the jog buttons read as range picks rather than as
// dead controls (ADR-0197 §SD12): a window stepped off the visible span has to
// bring the strip with it, or the brush paints nothing and the button looks
// like it did nothing.

const followMinute = int64(60_000)

func TestFollowRange_LeavesAContainedWindowAlone(t *testing.T) {
	_, _, move := followRange(0, 60*followMinute, 10*followMinute, 20*followMinute)
	assert.False(t, move, "a window already on the strip needs no pan")
}

// TestFollowRange_KeepsTheZoom pins that a jog steps along the axis rather
// than reframing it: the span the user picked survives the move.
func TestFollowRange_KeepsTheZoom(t *testing.T) {
	viewFrom, viewTo := int64(0), 60*followMinute
	from, to := 80*followMinute, 90*followMinute

	newFrom, newTo, move := followRange(viewFrom, viewTo, from, to)
	assert.True(t, move)
	assert.Equal(t, viewTo-viewFrom, newTo-newFrom, "the zoom is kept")
	assert.LessOrEqual(t, newFrom, from)
	assert.GreaterOrEqual(t, newTo, to)
	assert.Equal(t, (from+to)/2, (newFrom+newTo)/2, "centred on the window")
}

// TestFollowRange_WidensForAWindowThatWouldNotFit covers the brush drawn wider
// than the current zoom — legitimate, since a brush is clamped to the view but
// the view can then be zoomed in.
func TestFollowRange_WidensForAWindowThatWouldNotFit(t *testing.T) {
	from, to := 100*followMinute, 200*followMinute

	newFrom, newTo, move := followRange(0, 10*followMinute, from, to)
	assert.True(t, move)
	assert.Equal(t, 3*(to-from), newTo-newFrom,
		"a window too wide for the zoom lands with context on both sides")
	assert.Less(t, newFrom, from)
	assert.Greater(t, newTo, to)
}

func TestFollowRange_RefusesDegenerateSpans(t *testing.T) {
	_, _, move := followRange(0, 60*followMinute, 5*followMinute, 5*followMinute)
	assert.False(t, move, "an empty window is not somewhere to look")

	_, _, move = followRange(0, 0, 5*followMinute, 6*followMinute)
	assert.False(t, move, "an empty view carries no zoom to keep")
}

// TestMirrorReplayWindow_TracksTheSessionWindow pins the bookkeeping the pan
// is edge-triggered on. The timeline has never rendered here, so the pan
// itself is a no-op — what is under test is that a window is recorded once and
// a repeat of it is recognised.
func TestMirrorReplayWindow_TracksTheSessionWindow(t *testing.T) {
	inst := newApp()
	tl := inst.ensureAvailability()
	to := time.Now().UTC().Add(-time.Hour)
	from := to.Add(-10 * time.Minute)

	inst.mirrorReplayWindow(tl, sysmreplay.Window{From: from, To: to})
	assert.Equal(t, from.UnixMilli(), inst.availabilityBrushFromMS)
	assert.Equal(t, to.UnixMilli(), inst.availabilityBrushToMS)

	r, ok := tl.Brush()
	assert.True(t, ok, "the strip shows what is being replayed")
	assert.Equal(t, from.UnixMilli(), r.FromMS)
	assert.Equal(t, to.UnixMilli(), r.ToMS)
}

// TestMirrorReplayWindow_IgnoresAnUnboundedWindow: there is no range to brush,
// and half a window would claim one that is not being replayed.
func TestMirrorReplayWindow_IgnoresAnUnboundedWindow(t *testing.T) {
	inst := newApp()
	tl := inst.ensureAvailability()

	inst.mirrorReplayWindow(tl, sysmreplay.Window{})
	_, ok := tl.Brush()
	assert.False(t, ok)
	assert.Zero(t, inst.availabilityBrushFromMS)
}

// TestMirrorReplayWindow_RestoresABrushAClickCleared pins why the brush is set
// every frame rather than on change: clearing it does not cancel the window,
// so the strip must not go on claiming nothing is being replayed.
func TestMirrorReplayWindow_RestoresABrushAClickCleared(t *testing.T) {
	inst := newApp()
	tl := inst.ensureAvailability()
	to := time.Now().UTC().Add(-time.Hour)
	w := sysmreplay.Window{From: to.Add(-10 * time.Minute), To: to}

	inst.mirrorReplayWindow(tl, w)
	tl.ClearBrush()
	inst.mirrorReplayWindow(tl, w)

	_, ok := tl.Brush()
	assert.True(t, ok, "the range came back on the next frame")
}
