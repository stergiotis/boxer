package windowhost

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestFrameRingQuantiles(t *testing.T) {
	var r frameRing
	for i := 1; i <= 100; i++ {
		r.add(frameSample{dur: time.Duration(i) * time.Millisecond, msgs: 2})
	}
	info := r.info(FrameScopeWindow, 7)
	assert.EqualValues(t, 100, info.Frames)
	assert.Equal(t, 100, info.Samples)
	assert.Equal(t, 50*time.Millisecond, info.P50)
	assert.Equal(t, 95*time.Millisecond, info.P95)
	assert.Equal(t, 100*time.Millisecond, info.Max)
	assert.Equal(t, 100*time.Millisecond, info.Last)
	assert.Equal(t, 50500*time.Microsecond, info.Mean)
	assert.Equal(t, 5050*time.Millisecond, info.Total)
	assert.InDelta(t, 2.0, info.MessagesMean, 1e-9)
}

// The quantiles are over the recent samples; the totals are over all of them.
func TestFrameRingForgetsOldSamples(t *testing.T) {
	var r frameRing
	for range frameRingLen {
		r.add(frameSample{dur: time.Second})
	}
	for range frameRingLen {
		r.add(frameSample{dur: time.Millisecond})
	}
	info := r.info(FrameScopeWindow, 1)
	assert.EqualValues(t, 2*frameRingLen, info.Frames)
	assert.Equal(t, frameRingLen, info.Samples)
	assert.Equal(t, time.Millisecond, info.Max)
	assert.Equal(t, frameRingLen*(time.Second+time.Millisecond), info.Total)
}

func TestFrameTimesRowsPerWindow(t *testing.T) {
	h := &Inst{}
	a := &window{key: 2, manifest: app.Manifest{Id: "test.a"}}
	b := &window{key: 1, manifest: app.Manifest{Id: "test.b"}}
	ft := &h.frameTimes
	ft.recordMount(a, 3*time.Millisecond)
	ft.recordFrame(a, time.Millisecond, 10)
	ft.recordFrame(b, 2*time.Millisecond, 4)

	t0 := time.Now()
	ft.beginLoop(t0)
	ft.beginLoop(t0.Add(16 * time.Millisecond))

	rows := h.FrameTimes()
	require.Len(t, rows, 3)
	assert.Equal(t, FrameScopeLoop, rows[0].Scope, "the loop row comes first")
	assert.Equal(t, 16*time.Millisecond, rows[0].Last, "one period between two loop starts")
	assert.EqualValues(t, 1, rows[0].Frames)

	assert.Equal(t, WindowKeyT(1), rows[1].Key, "windows by key")
	assert.Equal(t, app.AppIdT("test.b"), rows[1].AppId)
	assert.Zero(t, rows[1].Mount)
	assert.Equal(t, app.AppIdT("test.a"), rows[2].AppId)
	assert.Equal(t, 3*time.Millisecond, rows[2].Mount)
	assert.EqualValues(t, 10, rows[2].MessagesLast)

	ft.forget(2)
	rows = h.FrameTimes()
	require.Len(t, rows, 2, "a reaped window's row goes with it")
}

func TestFrameTimesEmptyHost(t *testing.T) {
	rows := (&Inst{}).FrameTimes()
	require.Len(t, rows, 1, "the loop row is there before the first frame")
	assert.Zero(t, rows[0].Samples)
	assert.Zero(t, rows[0].P95)
}
