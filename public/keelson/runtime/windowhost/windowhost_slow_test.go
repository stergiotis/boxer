package windowhost

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Reported after slowFor above the line with frames drawn, once; again only
// after slowFor below it.
func TestSlowWatchReportsOnceUntilItRecovers(t *testing.T) {
	var w slowWatch
	t0 := time.Unix(1000, 0)
	slow, fast := SlowFrame, SlowFrame-time.Millisecond
	frames := uint64(0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	step := func(p95 time.Duration, s int) bool {
		frames += slowMinFrames
		return w.step(p95, frames, at(s))
	}
	for s := range 10 {
		assert.False(t, step(slow, s), "not before slowFor (%d s)", s)
	}
	assert.True(t, step(slow, 10), "slowFor at the line")
	assert.False(t, step(slow, 11), "once")
	assert.False(t, step(fast, 12))
	assert.False(t, step(slow, 13), "a short dip does not re-arm it")
	assert.False(t, step(fast, 14))
	assert.False(t, step(fast, 23))
	assert.False(t, step(fast, 24), "re-armed after slowFor below")
	assert.False(t, step(slow, 25))
	assert.True(t, step(slow, 35), "and reported again")
}

// A window that stopped drawing keeps its quantiles; it is not reported on
// them.
func TestSlowWatchNeedsFramesWhileSlow(t *testing.T) {
	var w slowWatch
	t0 := time.Unix(1000, 0)
	assert.False(t, w.step(SlowFrame, 100, t0))
	assert.False(t, w.step(SlowFrame, 100+slowMinFrames-1, t0.Add(slowFor)), "too few frames since")
	assert.True(t, w.step(SlowFrame, 100+slowMinFrames, t0.Add(slowFor+time.Second)))
}

type slowRecorder struct {
	slow []WindowKeyT
	gone []WindowKeyT
	p95  time.Duration
	app  app.AppIdT
}

func (inst *slowRecorder) WindowSlow(key WindowKeyT, appId app.AppIdT, _ string, p95 time.Duration) {
	inst.slow, inst.p95, inst.app = append(inst.slow, key), p95, appId
}
func (inst *slowRecorder) WindowGone(key WindowKeyT) { inst.gone = append(inst.gone, key) }

func TestCheckSlowTellsTheListener(t *testing.T) {
	h := &Inst{}
	rec := &slowRecorder{}
	h.SetSlowWindows(rec)
	ft := &h.frameTimes
	slowW := &window{key: 4, manifest: app.Manifest{Id: "test.slow"}}
	fastW := &window{key: 5, manifest: app.Manifest{Id: "test.fast"}}
	t0 := time.Unix(1000, 0)
	for s := 0; s <= 11; s++ {
		for range 10 {
			ft.recordFrame(slowW, 12*time.Millisecond, 1)
			ft.recordFrame(fastW, time.Millisecond, 1)
		}
		ft.checkSlow(t0.Add(time.Duration(s) * time.Second))
		ft.checkSlow(t0.Add(time.Duration(s)*time.Second + 100*time.Millisecond))
	}
	require.Equal(t, []WindowKeyT{4}, rec.slow)
	assert.Equal(t, 12*time.Millisecond, rec.p95)
	assert.Equal(t, app.AppIdT("test.slow"), rec.app)

	ft.forget(4)
	assert.Equal(t, []WindowKeyT{4}, rec.gone, "a reaped window is gone")
}
