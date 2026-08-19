package imztop

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSource stands in for a sysmreplay.Reader so the transport is testable
// without a database: it hands back a fixed window of bundles and records the
// windows it was asked for, which is how the Seek assertions see that the
// cursor was actually reopened.
type fakeSource struct {
	mu      sync.Mutex
	bundles []*sysmsnap.BundleSnapshot
	windows []sysmreplay.Window

	// failAt yields an error instead of the bundle at this index, when >= 0.
	failAt int
}

func newFakeSource(bundles ...*sysmsnap.BundleSnapshot) *fakeSource {
	return &fakeSource{bundles: bundles, failAt: -1}
}

func (inst *fakeSource) All(_ context.Context, w sysmreplay.Window) iter.Seq2[*sysmsnap.BundleSnapshot, error] {
	inst.mu.Lock()
	inst.windows = append(inst.windows, w)
	inst.mu.Unlock()
	return func(yield func(*sysmsnap.BundleSnapshot, error) bool) {
		for i, b := range inst.bundles {
			if !w.From.IsZero() && time.UnixMilli(b.SampledAtUnixMs).Before(w.From) {
				continue
			}
			if i == inst.failAt {
				yield(nil, errors.New("fake source failure"))
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

func (inst *fakeSource) seenWindows() []sysmreplay.Window {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]sysmreplay.Window(nil), inst.windows...)
}

// replayBundles builds n bundles spaced stepMs apart, each carrying a CPU
// sample whose busy percent is its index so a fold can be identified.
func replayBundles(baseMs int64, stepMs int64, n int) []*sysmsnap.BundleSnapshot {
	out := make([]*sysmsnap.BundleSnapshot, 0, n)
	for i := range n {
		ts := baseMs + int64(i)*stepMs
		out = append(out, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU: &sysmsnap.CPUSnapshot{
				SampledAtUnixMs: ts,
				TotalPercent:    uint8(i),
				PerCorePercent:  []uint8{uint8(i), uint8(i)},
			},
			Mem: &sysmsnap.MemSnapshot{SampledAtUnixMs: ts, TotalBytes: 1 << 30, AvailableBytes: 1 << 29},
		})
	}
	return out
}

const replayBase = int64(1_700_000_000_000)

// startReplay builds and starts a sampler, closing it when the test ends.
func startReplay(t *testing.T, opts ReplayOptions) *ReplaySampler {
	t.Helper()
	r, err := NewReplaySampler(opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	r.Start(context.Background())
	return r
}

func TestReplaySampler_NeedsASource(t *testing.T) {
	_, err := NewReplaySampler(ReplayOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Source")
}

// TestReplaySampler_PlaysTheWindowThrough is the happy path: every bundle
// reaches the fold, in order, and the sampler parks at the end rather than
// closing itself.
func TestReplaySampler_PlaysTheWindowThrough(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 5)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000})

	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond,
		"the window never played to its end")

	at, ok := r.Position()
	require.True(t, ok)
	assert.Equal(t, replayBase+4000, at.UnixMilli(), "position is the last bundle's own stamp")

	snap := r.Latest()
	require.NotNil(t, snap)
	require.NotNil(t, snap.LatestCPU)
	assert.Equal(t, uint8(4), snap.LatestCPU.TotalPercent, "the last bundle is the visible one")
	assert.Len(t, snap.HistoryCPUTotal, 5, "every bundle folded into the history")
}

// TestReplaySampler_HistoryCarriesRecordedTime pins ADR-0197 §SD3: the plot
// axis shows when the run happened, not when it was replayed.
func TestReplaySampler_HistoryCarriesRecordedTime(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000})
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)

	snap := r.Latest()
	require.NotNil(t, snap)
	require.Len(t, snap.HistoryTimeUnixSec, 3)
	assert.InDelta(t, float64(replayBase)/1000.0, snap.HistoryTimeUnixSec[0], 0.001)
	assert.InDelta(t, float64(replayBase+2000)/1000.0, snap.HistoryTimeUnixSec[2], 0.001)
}

// TestReplaySampler_IntervalIsTheRecordedCadence pins the other half of §SD3:
// playing at 10000x must not make the cadence readout report 10000x.
func TestReplaySampler_IntervalIsTheRecordedCadence(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 2000, 4)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000})
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)

	assert.Equal(t, 2*time.Second, r.Interval(),
		"cadence comes from the bundles' own stamps, not from playback speed")
}

// TestReplaySampler_StartPausedHoldsAtTheStart covers the transport's resting
// state: nothing is folded until the user asks for it.
func TestReplaySampler_StartPausedHoldsAtTheStart(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 5)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000, StartPaused: true})

	assert.True(t, r.IsPaused())
	time.Sleep(100 * time.Millisecond)
	assert.Nil(t, r.Latest(), "a paused transport must not fold anything")
	_, ok := r.Position()
	assert.False(t, ok)
}

// TestReplaySampler_StepAdvancesOneBundle is the deterministic half of the
// transport: no pacing is involved, so the count is exact rather than eventual.
func TestReplaySampler_StepAdvancesOneBundle(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 5)...)
	r := startReplay(t, ReplayOptions{Source: src, StartPaused: true})

	for want := range 3 {
		r.Step(1)
		require.Eventually(t, func() bool {
			at, ok := r.Position()
			return ok && at.UnixMilli() == replayBase+int64(want)*1000
		}, 2*time.Second, 2*time.Millisecond, "step %d never landed", want)

		time.Sleep(30 * time.Millisecond)
		at, _ := r.Position()
		assert.Equal(t, replayBase+int64(want)*1000, at.UnixMilli(),
			"a step must advance exactly one bundle and then stop")
	}
	assert.True(t, r.IsPaused(), "stepping does not resume playback")
}

// TestReplaySampler_StepN advances several at once.
func TestReplaySampler_StepN(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 6)...)
	r := startReplay(t, ReplayOptions{Source: src, StartPaused: true})

	r.Step(4)
	require.Eventually(t, func() bool {
		at, ok := r.Position()
		return ok && at.UnixMilli() == replayBase+3000
	}, 2*time.Second, 2*time.Millisecond)

	time.Sleep(30 * time.Millisecond)
	at, _ := r.Position()
	assert.Equal(t, replayBase+3000, at.UnixMilli(), "four steps, four bundles, no more")
}

func TestReplaySampler_StepIgnoresNonPositive(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	r := startReplay(t, ReplayOptions{Source: src, StartPaused: true})

	r.Step(0)
	r.Step(-2)
	time.Sleep(50 * time.Millisecond)
	assert.Nil(t, r.Latest())
}

// TestReplaySampler_PauseStopsAdvancing pins that pause halts the cursor rather
// than dropping frames — with nothing arriving, dropping would be a no-op and
// the transport would run to the end regardless.
func TestReplaySampler_PauseStopsAdvancing(t *testing.T) {
	// 1 s gaps at 20x are 50 ms apart: long enough that a 250 ms hold is
	// unambiguous evidence of not advancing, short enough that the resumed
	// window finishes promptly.
	src := newFakeSource(replayBundles(replayBase, 1000, 6)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 20})

	require.Eventually(t, func() bool { _, ok := r.Position(); return ok },
		2*time.Second, 2*time.Millisecond, "the first bundle should show immediately")

	r.Pause(true)
	time.Sleep(20 * time.Millisecond) // let an in-flight gap finish landing
	at1, _ := r.Position()
	time.Sleep(250 * time.Millisecond)
	at2, _ := r.Position()
	assert.Equal(t, at1, at2, "a paused transport must not advance")
	assert.False(t, r.Exhausted())

	r.Pause(false)
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond,
		"resuming must play the rest of the window")
}

// TestReplaySampler_FirstBundleIsNotPaced pins that a window opens on screen
// immediately: pacing is between bundles, and the first has no predecessor.
func TestReplaySampler_FirstBundleIsNotPaced(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 60_000, 3)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 1})

	require.Eventually(t, func() bool { _, ok := r.Position(); return ok },
		2*time.Second, 2*time.Millisecond)
	at, _ := r.Position()
	assert.Equal(t, replayBase, at.UnixMilli())
}

// TestReplaySampler_SpeedIsAppliedToTheGap uses the same window at two speeds:
// slow enough to still be mid-window, fast enough to be finished.
func TestReplaySampler_SpeedIsAppliedToTheGap(t *testing.T) {
	slow := newFakeSource(replayBundles(replayBase, 1000, 4)...)
	rSlow := startReplay(t, ReplayOptions{Source: slow, Speed: 1})
	time.Sleep(150 * time.Millisecond)
	assert.False(t, rSlow.Exhausted(), "at 1x, 1 s gaps cannot have played out in 150 ms")

	fast := newFakeSource(replayBundles(replayBase, 1000, 4)...)
	rFast := startReplay(t, ReplayOptions{Source: fast, Speed: 10000})
	require.Eventually(t, rFast.Exhausted, 5*time.Second, 2*time.Millisecond,
		"at 10000x the same window should finish promptly")
}

// TestReplaySampler_SetSpeedTakesEffectMidWait pins that a speed change is
// applied to the delay already being waited out, not only to the next gap —
// otherwise a user pressing "faster" on a long gap sees nothing happen.
func TestReplaySampler_SetSpeedTakesEffectMidWait(t *testing.T) {
	// Two 10 s gaps at 1x clamp to MaxReplayGap each, so waiting them out takes
	// 2*MaxReplayGap. The assertion below allows a fraction of that, which only
	// passes if the raise shortened the wait already in progress.
	src := newFakeSource(replayBundles(replayBase, 10_000, 3)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 1})

	require.Eventually(t, func() bool { _, ok := r.Position(); return ok },
		2*time.Second, 2*time.Millisecond)
	at, _ := r.Position()
	require.Equal(t, replayBase, at.UnixMilli(), "still on the first bundle")

	started := time.Now()
	r.SetSpeed(100000)
	require.Eventually(t, r.Exhausted, MaxReplayGap, 2*time.Millisecond,
		"raising speed during the wait must shorten it, not merely the next gap")
	assert.Less(t, time.Since(started), MaxReplayGap,
		"finished only because the clamp expired, which is not the behaviour under test")
}

func TestReplaySampler_SetSpeedRejectsNonsense(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 2)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 4, StartPaused: true})

	for _, bad := range []float64{0, -1} {
		r.SetSpeed(bad)
		assert.Equal(t, 4.0, r.Speed(), "%v must not become the speed", bad)
	}
}

func TestReplaySampler_DefaultSpeedIsRealTime(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 2)...)
	r := startReplay(t, ReplayOptions{Source: src, StartPaused: true})
	assert.Equal(t, DefaultReplaySpeed, r.Speed())
}

// TestReplaySampler_SeekReopensTheCursor pins that Seek reaches the source
// rather than skipping locally: the window the source is asked for must carry
// the new bound.
func TestReplaySampler_SeekReopensTheCursor(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 6)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000})
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)

	target := time.UnixMilli(replayBase + 4000).UTC()
	r.Seek(target)

	require.Eventually(t, func() bool {
		ws := src.seenWindows()
		return len(ws) >= 2 && ws[len(ws)-1].From.Equal(target)
	}, 5*time.Second, 5*time.Millisecond, "the source was never reopened at the seek target")

	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)
	at, ok := r.Position()
	require.True(t, ok)
	assert.Equal(t, replayBase+5000, at.UnixMilli(), "replay resumed from the seek and ran on")
	assert.Equal(t, target, r.Window().From, "the window records the seek")
}

// TestReplaySampler_SeekInterruptsPlayback covers seeking mid-window rather
// than from the parked state.
func TestReplaySampler_SeekInterruptsPlayback(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 10_000, 6)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 1})

	require.Eventually(t, func() bool { _, ok := r.Position(); return ok },
		2*time.Second, 2*time.Millisecond)

	target := time.UnixMilli(replayBase + 50_000).UTC()
	r.Seek(target)
	require.Eventually(t, func() bool {
		ws := src.seenWindows()
		return len(ws) >= 2 && ws[len(ws)-1].From.Equal(target)
	}, 5*time.Second, 5*time.Millisecond)

	require.Eventually(t, func() bool {
		at, ok := r.Position()
		return ok && at.UnixMilli() == replayBase+50_000
	}, 5*time.Second, 5*time.Millisecond)
}

// TestReplaySampler_SeekClearsExhausted lets a parked transport run again.
func TestReplaySampler_SeekClearsExhausted(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000})
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)

	r.Seek(time.UnixMilli(replayBase).UTC())
	require.Eventually(t, func() bool {
		return len(src.seenWindows()) >= 2
	}, 5*time.Second, 5*time.Millisecond)
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond, "and finishes again")
}

// TestReplaySampler_SourceErrorParksRatherThanWedges pins that a failed read
// leaves a usable sampler: the transport stops advancing but stays seekable.
func TestReplaySampler_SourceErrorParksRatherThanWedges(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 5)...)
	src.failAt = 2
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000})

	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond,
		"a read error must park the transport, not hang it")
	at, ok := r.Position()
	require.True(t, ok)
	assert.Equal(t, replayBase+1000, at.UnixMilli(), "the bundles before the error still folded")

	src.failAt = -1
	r.Seek(time.UnixMilli(replayBase + 2000).UTC())
	require.Eventually(t, func() bool {
		at, ok := r.Position()
		return ok && at.UnixMilli() == replayBase+4000
	}, 5*time.Second, 5*time.Millisecond, "a seek after an error must still work")
}

func TestReplaySampler_EmptyWindowParks(t *testing.T) {
	r := startReplay(t, ReplayOptions{Source: newFakeSource(), Speed: 10000})
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)
	assert.Nil(t, r.Latest())
}

// TestReplaySampler_CloseIsIdempotent pins that the second Close neither blocks
// nor panics — the app's teardown path calls it from more than one place.
func TestReplaySampler_CloseIsIdempotent(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	r, err := NewReplaySampler(ReplayOptions{Source: src, Speed: 10000})
	require.NoError(t, err)
	r.Start(context.Background())
	require.Eventually(t, r.Exhausted, 5*time.Second, 5*time.Millisecond)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
}

// TestReplaySampler_CloseWhilePausedTerminates is the deadlock case: the
// transport is blocked waiting for a control, and Close has to break it out.
func TestReplaySampler_CloseWhilePausedTerminates(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 100)...)
	r, err := NewReplaySampler(ReplayOptions{Source: src, StartPaused: true})
	require.NoError(t, err)
	r.Start(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Close() }()
	select {
	case cerr := <-done:
		require.NoError(t, cerr)
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked on a paused transport")
	}
}

// TestReplaySampler_StartIsIdempotent guards against a second goroutine.
func TestReplaySampler_StartIsIdempotent(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000, StartPaused: true})
	r.Start(context.Background())
	r.Start(context.Background())

	r.Step(1)
	require.Eventually(t, func() bool { _, ok := r.Position(); return ok },
		2*time.Second, 2*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	at, _ := r.Position()
	assert.Equal(t, replayBase, at.UnixMilli(), "one step must not be consumed by two transports")
}

// TestReplaySampler_SatisfiesSamplerI is what makes it substitutable in the
// render path (ADR-0197 §SD2).
func TestReplaySampler_SatisfiesSamplerI(t *testing.T) {
	src := newFakeSource(replayBundles(replayBase, 1000, 2)...)
	r := startReplay(t, ReplayOptions{Source: src, Speed: 10000, StartPaused: true})

	var s SamplerI = r
	s.Pause(false)
	assert.False(t, s.IsPaused())
	s.Pause(true)
	assert.True(t, s.IsPaused())
	assert.NotZero(t, s.Interval())
}
