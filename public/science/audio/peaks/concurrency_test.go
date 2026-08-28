package peaks_test

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// TestConcurrentReadWhileBuilding is the lock-free read contract of
// ADR-0208 §SD4 under the race detector: one goroutine folds a procedural
// source while this one queries the published prefix without a lock. Every
// value a reader sees must equal the value a synchronous build produced —
// a bin is final before it becomes readable, or the pyramid is wrong.
func TestConcurrentReadWhileBuilding(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames = 200_000
	const baseBin = 16
	fn := pcm.PerChannel(
		pcm.Gate(pcm.Sine(format, 440, 0.9), 4800, 2400),
		pcm.Chirp(format, frames, 50, 8000, 0.7),
	)
	src, err := pcm.NewSynthSourceE(format, frames, fn)
	require.NoError(t, err)

	ref, err := peaks.BuildE(context.Background(), src, baseBin, 4096, nil)
	require.NoError(t, err)
	require.True(t, ref.IsComplete())

	live, err := peaks.NewPyramidE(format, frames, baseBin)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// A chunk size that divides neither the base bin nor the frame
		// count, so bins are published mid-chunk and the last one is
		// partial.
		assert.NoError(t, live.FillFromE(context.Background(), src, 373, nil))
	}()

	r := rand.New(rand.NewPCG(0x51ce, 0xf00d))
	const window = 64
	liveMin := make([]int8, window)
	liveMax := make([]int8, window)
	refMin := make([]int8, window)
	refMax := make([]int8, window)
	partialObservations := 0
	lastBuilt := int64(0)
	lastPeak := int8(0)
	reads := 0
	for {
		built := live.Built()
		complete := live.IsComplete()
		if !assert.GreaterOrEqual(t, built, lastBuilt, "the built prefix must not shrink") {
			break
		}
		lastBuilt = built
		peak := live.GlobalPeak()
		if !assert.GreaterOrEqual(t, peak, lastPeak, "the global peak must not shrink") ||
			!assert.LessOrEqual(t, peak, ref.GlobalPeak(), "the global peak must not exceed the final one") {
			break
		}
		lastPeak = peak
		if built < frames {
			partialObservations++
		}

		level := int32(r.IntN(int(live.Levels())))
		ch := r.IntN(int(format.Channels))
		firstBin := int64(r.IntN(int(max(live.Bins(level), 1))))
		n := live.Query(level, firstBin, ch, liveMin, liveMax)
		refN := ref.Query(level, firstBin, ch, refMin, refMax)
		if !assert.GreaterOrEqual(t, refN, n, "the reference must hold at least what the live pyramid does") {
			break
		}
		if !assert.Equal(t, refMin[:n], liveMin[:n], "Query minima at level %d bin %d", level, firstBin) ||
			!assert.Equal(t, refMax[:n], liveMax[:n], "Query maxima at level %d bin %d", level, firstBin) {
			break
		}

		fromFrame := int64(r.IntN(frames))
		toFrame := fromFrame + 1 + int64(r.IntN(frames-int(fromFrame)))
		columns := 1 + r.IntN(window)
		liveCols := live.Columns(fromFrame, toFrame, ch, liveMin[:columns], liveMax[:columns])
		refCols := ref.Columns(fromFrame, toFrame, ch, refMin[:columns], refMax[:columns])
		if !assert.LessOrEqual(t, liveCols, refCols, "a partial build cannot draw more columns") {
			break
		}
		if !assert.Equal(t, refMin[:liveCols], liveMin[:liveCols], "Columns minima over [%d,%d)", fromFrame, toFrame) ||
			!assert.Equal(t, refMax[:liveCols], liveMax[:liveCols], "Columns maxima over [%d,%d)", fromFrame, toFrame) {
			break
		}
		reads++
		if complete {
			break
		}
	}
	wg.Wait()

	t.Logf("reads=%d partialObservations=%d", reads, partialObservations)
	require.Positive(t, partialObservations, "the reader never saw a partial build; the test would be vacuous")
	require.True(t, live.IsComplete())
	require.Equal(t, int64(frames), live.Built())
	require.Equal(t, ref.GlobalPeak(), live.GlobalPeak())
	for level := range live.Levels() {
		for ch := range int(format.Channels) {
			gotMin, gotMax := dumpLevel(t, live, level, ch)
			wantMin, wantMax := dumpLevel(t, ref, level, ch)
			require.Equal(t, wantMin, gotMin, "minima at level %d channel %d", level, ch)
			require.Equal(t, wantMax, gotMax, "maxima at level %d channel %d", level, ch)
		}
	}
}
