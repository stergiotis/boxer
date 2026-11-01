package windowhost

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// pausableApp records what the host told it.
type pausableApp struct {
	app.AppI
	told []bool
}

func (inst *pausableApp) SetPaused(paused bool) { inst.told = append(inst.told, paused) }

// pauseHost is a host with windows over one instance, mounted, its facts in
// memory and its pause events recorded.
func pauseHost(keys ...WindowKeyT) (h *Inst, a *pausableApp, facts *factsstore.InMemoryFactsStore, events *[]PauseEvent) {
	a = &pausableApp{}
	ms := &instMount{refs: len(keys), mounted: true}
	h = &Inst{logger: zerolog.Nop(), runId: "run-1"}
	facts = factsstore.NewInMemoryFactsStore()
	h.facts = facts
	for _, key := range keys {
		h.windows = append(h.windows, &window{key: key, manifest: app.Manifest{Id: "test.app"}, appInst: a, mount: ms})
	}
	events = &[]PauseEvent{}
	h.SetPauseAudit(func(ev PauseEvent) { *events = append(*events, ev) })
	return
}

// A request applies at the next frame; the window is paused since then,
// and its resume records how long.
func TestPauseAppliesAtTheNextFrameAndIsRecorded(t *testing.T) {
	h, a, facts, events := pauseHost(7)
	t0 := time.Unix(1000, 0)
	require.True(t, h.Pause(7, PauseFromWindow))
	assert.False(t, h.Pause(99, PauseFromWindow), "no such window")
	assert.True(t, h.PausedSince(7).IsZero(), "a request until the frame")
	h.applyPauses(h.windows, t0)
	assert.Equal(t, t0, h.PausedSince(7))
	assert.Equal(t, []bool{true}, a.told)

	h.Pause(7, PauseFromWindow)
	h.applyPauses(h.windows, t0.Add(time.Second))
	assert.Equal(t, t0, h.PausedSince(7), "pausing a paused window does nothing")
	assert.Len(t, *events, 1)

	h.Resume(7, PauseFromWindow)
	h.applyPauses(h.windows, t0.Add(90*time.Second))
	assert.True(t, h.PausedSince(7).IsZero())
	assert.Equal(t, []bool{true, false}, a.told)

	require.Len(t, *events, 2)
	assert.True(t, (*events)[0].Paused)
	assert.False(t, (*events)[1].Paused)
	assert.Equal(t, 90*time.Second, (*events)[1].For)
	assert.Equal(t, PauseFromWindow, (*events)[1].From)

	rows := facts.Lifecycles()
	require.Len(t, rows, 2)
	assert.Equal(t, factsstore.AppLifecyclePhasePaused, rows[0].Phase)
	assert.Equal(t, factsstore.AppLifecyclePhaseResumed, rows[1].Phase)
	assert.Equal(t, PauseFromWindow, rows[0].StopReason, "where the person paused it")
	assert.EqualValues(t, 7, rows[1].TileKey)
}

// Two windows over one instance pause apart; the instance hears of the
// pause once both are paused, and of the resume at the first resume.
func TestTheInstanceIsPausedWhenEveryWindowIs(t *testing.T) {
	h, a, _, _ := pauseHost(1, 2)
	t0 := time.Unix(1000, 0)
	h.Pause(1, PauseFromWindow)
	h.applyPauses(h.windows, t0)
	assert.Empty(t, a.told, "window 2 still runs")
	h.Pause(2, PauseFromWindow)
	h.applyPauses(h.windows, t0)
	assert.Equal(t, []bool{true}, a.told)
	h.Resume(1, PauseFromWindow)
	h.applyPauses(h.windows, t0)
	assert.Equal(t, []bool{true, false}, a.told)
	h.Resume(2, PauseFromWindow)
	h.applyPauses(h.windows, t0)
	assert.Equal(t, []bool{true, false}, a.told, "already told")
}

// An instance not yet mounted hears nothing: it mounts on the resume.
func TestAnUnmountedInstanceHearsNothing(t *testing.T) {
	h, a, _, _ := pauseHost(1)
	h.windows[0].mount.mounted = false
	h.Pause(1, PauseFromWindow)
	h.applyPauses(h.windows, time.Unix(1000, 0))
	assert.False(t, h.PausedSince(1).IsZero())
	assert.Empty(t, a.told)
}

func TestPausedFor(t *testing.T) {
	assert.Equal(t, "for 12 s", pausedFor(12*time.Second))
	assert.Equal(t, "for 3 min", pausedFor(3*time.Minute+20*time.Second))
	assert.Equal(t, "for 1 h 5 min", pausedFor(65*time.Minute))
}
