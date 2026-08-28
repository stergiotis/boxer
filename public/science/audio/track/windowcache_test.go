package track

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// failingSource fails its reads while fail is set, so a test can break the
// decoder under a track that is already open.
type failingSource struct {
	inner  pcm.SourceI
	fail   atomic.Bool
	reads  atomic.Int64
	closes atomic.Int64
}

var _ pcm.SourceI = (*failingSource)(nil)

func newFailingSource(inner pcm.SourceI) (inst *failingSource) {
	return &failingSource{inner: inner}
}

func (inst *failingSource) Format() (format pcm.Format) { return inst.inner.Format() }
func (inst *failingSource) Frames() (frames int64)      { return inst.inner.Frames() }

func (inst *failingSource) ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error) {
	inst.reads.Add(1)
	if inst.fail.Load() {
		return 0, eh.New("the decoder is broken")
	}
	return inst.inner.ReadFramesAtE(ctx, frameOffset, dst)
}

func (inst *failingSource) CloseE() (err error) {
	inst.closes.Add(1)
	return inst.inner.CloseE()
}

// testingTB is what the helpers below need of a *testing.T and of a
// *rapid.T alike.
type testingTB interface {
	require.TestingT
	Helper()
}

// waitForWindow polls [Track.Window] the way a frame thread would — ask,
// draw something else, ask again — until the fetch has landed.
func waitForWindow(t testingTB, tr *Track, fromFrame int64, toFrame int64) (samples []float32) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		samples, ok := tr.Window(fromFrame, toFrame)
		if ok {
			return samples
		}
		require.True(t, time.Now().Before(deadline), "the window [%d,%d) never arrived", fromFrame, toFrame)
		time.Sleep(200 * time.Microsecond)
	}
}

func requireFetches(t *testing.T, tr *Track, want uint64) {
	t.Helper()
	_, _, _, _, fetches := tr.WindowCacheStats()
	require.Equal(t, want, fetches)
}

// TestWindowFetchesOffThreadAndCaches is ADR-0208 §SD3's contract: the frame
// that asks gets a miss and keeps drawing, and the window is there a frame or
// two later.
func TestWindowFetchesOffThreadAndCaches(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 60_000
	const windowFrames int64 = 1024
	ch := int(format.Channels)
	ctx := context.Background()

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{ChunkFrames: 8192})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	samples, ok := tr.Window(0, windowFrames)
	require.False(t, ok, "the first ask is a miss")
	require.Nil(t, samples)
	require.True(t, tr.WindowPending(), "the miss scheduled a fetch")

	got := waitForWindow(t, tr, 0, windowFrames)
	want := make([]float32, windowFrames*int64(ch))
	n, err := tr.ReadWindowE(ctx, 0, want)
	require.NoError(t, err)
	require.Equal(t, int(windowFrames), n)
	require.Equal(t, want, got, "the cached window is what a synchronous read returns")
	require.False(t, tr.WindowPending())

	entries, bytes, hits, misses, fetches := tr.WindowCacheStats()
	require.Equal(t, 1, entries)
	require.Equal(t, windowFrames*int64(ch)*4, bytes)
	require.Equal(t, uint64(1), fetches)
	require.Positive(t, misses)

	again, ok := tr.Window(0, windowFrames)
	require.True(t, ok)
	require.Equal(t, got, again)
	entries, _, hits2, misses2, fetches2 := tr.WindowCacheStats()
	require.Equal(t, 1, entries)
	require.Equal(t, hits+1, hits2, "a second ask for a cached window is a hit")
	require.Equal(t, misses, misses2)
	require.Equal(t, fetches, fetches2, "and costs no fetch")
}

func TestWindowPastTheEndAndOutsideTheRecording(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 3}
	const frames int64 = 5000
	ch := int(format.Channels)

	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{BaseBin: 16})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	// Nothing to read past the end, and nothing to wait for.
	samples, ok := tr.Window(frames, frames+1000)
	require.True(t, ok)
	require.Empty(t, samples)
	require.False(t, tr.WindowPending())

	// A window straddling the end holds the frames that exist.
	got := waitForWindow(t, tr, frames-100, frames+900)
	require.Len(t, got, 100*ch)
	requireFetches(t, tr, 1)
	// It is keyed by the clamped range, so the honest request for the same
	// frames is a hit rather than a second fetch.
	clamped, ok := tr.Window(frames-100, frames)
	require.True(t, ok)
	require.Equal(t, got, clamped)
	requireFetches(t, tr, 1)

	// An empty or negative range is refused rather than queued.
	for _, tc := range [][2]int64{{-1, 100}, {100, 100}, {200, 100}} {
		samples, ok = tr.Window(tc[0], tc[1])
		require.False(t, ok, "window [%d,%d)", tc[0], tc[1])
		require.Nil(t, samples)
	}
	require.False(t, tr.WindowPending())
}

// TestWindowRefusesWhatCannotFit covers the two bounds a caller cannot poll
// its way past.
func TestWindowRefusesWhatCannotFit(t *testing.T) {
	t.Run("longer than the cap", func(t *testing.T) {
		format := pcm.Format{SampleRate: 8000, Channels: 1}
		frames := MaxWindowFrames + 4096
		src, err := pcm.NewSynthSourceE(format, frames, pcm.Silence())
		require.NoError(t, err)
		tr, err := OpenE(context.Background(), src, Options{ChunkFrames: 1 << 18})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

		samples, ok := tr.Window(0, MaxWindowFrames+1)
		require.False(t, ok)
		require.Nil(t, samples)
		require.False(t, tr.WindowPending(), "an oversized request is refused, not queued")
		requireFetches(t, tr, 0)

		// One frame under the cap is a request like any other.
		_, ok = tr.Window(0, MaxWindowFrames)
		require.False(t, ok)
		require.True(t, tr.WindowPending())
	})

	t.Run("larger than the whole cache", func(t *testing.T) {
		format := pcm.Format{SampleRate: 48000, Channels: 2}
		const frames int64 = 20_000
		tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{
			WindowCacheBytes: 4096,
		})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

		// 2048 stereo frames are 16 KiB, four times the bound.
		for range 5 {
			samples, ok := tr.Window(0, 2048)
			require.False(t, ok)
			require.Nil(t, samples)
		}
		require.False(t, tr.WindowPending())
		requireFetches(t, tr, 0)

		// A window that does fit still arrives.
		got := waitForWindow(t, tr, 0, 512)
		require.Len(t, got, 512*2)
	})
}

func TestWindowEvictsLeastRecentlyUsed(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 20_000
	const windowFrames int64 = 512
	// Two windows of 512 stereo frames fill the bound exactly.
	const bound = 2 * windowFrames * 2 * 4

	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{
		WindowCacheBytes: bound,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	first := waitForWindow(t, tr, 0, windowFrames)
	firstCopy := append([]float32(nil), first...)
	waitForWindow(t, tr, windowFrames, 2*windowFrames)
	entries, bytes, _, _, _ := tr.WindowCacheStats()
	require.Equal(t, 2, entries)
	require.Equal(t, int64(bound), bytes)

	waitForWindow(t, tr, 2*windowFrames, 3*windowFrames)
	entries, bytes, _, _, _ = tr.WindowCacheStats()
	require.Equal(t, 2, entries, "the third window evicted one")
	require.Equal(t, int64(bound), bytes)

	// The one evicted is the one longest unasked-for.
	_, ok := tr.Window(0, windowFrames)
	require.False(t, ok)
	// Its buffer is still the caller's: an evicted window is released to the
	// collector, never reused for another fetch.
	require.Equal(t, firstCopy, first)
}

// TestWindowMailboxKeepsTheLatestRequest is why the cache has one slot: a
// view being zoomed supersedes its own requests faster than a decoder can
// serve them, and serving the superseded ones would spend the decoder on
// windows nobody will look at.
func TestWindowMailboxKeepsTheLatestRequest(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 40_000
	const windowFrames int64 = 512

	gate := make(chan struct{})
	calls := 0
	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{
		ChunkFrames: 8192,
		Reopen: func(_ context.Context) (src pcm.SourceI, err error) {
			calls++
			if calls == 1 {
				// The build's source runs at full speed; only the window
				// cache's is held up.
				return newTestSource(t, format, frames), nil
			}
			return newGatedSource(newTestSource(t, format, frames), gate, 0), nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	require.Equal(t, 2, calls)

	_, ok := tr.Window(0, windowFrames)
	require.False(t, ok)
	require.Eventually(t, func() bool {
		_, _, _, _, fetches := tr.WindowCacheStats()
		return fetches == 1
	}, 10*time.Second, time.Millisecond, "the worker never picked the first request up")

	// Three more asks while that fetch is held at the gate: the mailbox keeps
	// the last of them.
	for _, from := range []int64{windowFrames, 2 * windowFrames, 3 * windowFrames} {
		_, ok = tr.Window(from, from+windowFrames)
		require.False(t, ok)
	}
	require.True(t, tr.WindowPending())
	requireFetches(t, tr, 1)

	gate <- struct{}{} // the in-flight fetch finishes
	require.Eventually(t, func() bool {
		_, _, _, _, fetches := tr.WindowCacheStats()
		return fetches == 2
	}, 10*time.Second, time.Millisecond, "the queued request never started")
	gate <- struct{}{} // and so does the one the mailbox kept
	require.Eventually(t, func() bool { return !tr.WindowPending() }, 10*time.Second, time.Millisecond)

	requireFetches(t, tr, 2)
	entries, _, _, _, _ := tr.WindowCacheStats()
	require.Equal(t, 2, entries)

	cached, ok := tr.Window(0, windowFrames)
	require.True(t, ok, "the fetch that was in flight was not dropped")
	require.Len(t, cached, int(windowFrames)*2)
	cached, ok = tr.Window(3*windowFrames, 4*windowFrames)
	require.True(t, ok, "the last request wins")
	require.Len(t, cached, int(windowFrames)*2)
	for _, from := range []int64{windowFrames, 2 * windowFrames} {
		_, ok = tr.Window(from, from+windowFrames)
		require.False(t, ok, "the superseded request at %d was dropped", from)
	}
}

// TestWindowBacksOffAfterAFailure keeps a broken decoder from being asked
// sixty times a second.
func TestWindowBacksOffAfterAFailure(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 20_000
	const windowFrames int64 = 512

	src := newFailingSource(newTestSource(t, format, frames))
	tr, err := OpenE(context.Background(), src, Options{ChunkFrames: 8192})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	src.fail.Store(true)
	_, ok := tr.Window(0, windowFrames)
	require.False(t, ok)
	require.Eventually(t, func() bool { return !tr.WindowPending() }, 10*time.Second, time.Millisecond)
	requireFetches(t, tr, 1)

	// The frame thread keeps asking; the decoder is not asked again.
	for range 50 {
		_, ok = tr.Window(0, windowFrames)
		require.False(t, ok)
		time.Sleep(time.Millisecond)
	}
	requireFetches(t, tr, 1)
	entries, bytes, _, _, _ := tr.WindowCacheStats()
	require.Zero(t, entries)
	require.Zero(t, bytes)

	// The backoff is per window: another one is fetched at once.
	src.fail.Store(false)
	got := waitForWindow(t, tr, windowFrames, 2*windowFrames)
	require.Len(t, got, int(windowFrames)*2)
	requireFetches(t, tr, 2)
}

// TestWindowWhileTheBackgroundBuildRuns is the frame thread's real shape:
// polling for windows and drawing the pyramid while the build fills it, all
// through one shared source. Run under -race, the reads that the locked
// adapter would otherwise let overlap are what the detector would catch.
func TestWindowWhileTheBackgroundBuildRuns(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 300_000
	const windowFrames int64 = 1024
	ch := int64(format.Channels)

	raw := newScratchSource(format, frames)
	tr, err := OpenE(context.Background(), raw, Options{Background: true, ChunkFrames: 4096})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	dstMin := make([]int8, 128)
	dstMax := make([]int8, 128)
	arrived := 0
	built := int64(0)
	deadline := time.Now().Add(20 * time.Second)
	for frame := 0; frame < 400 && time.Now().Before(deadline); frame++ {
		from := int64(frame%40) * windowFrames
		samples, ok := tr.Window(from, from+windowFrames)
		if ok {
			arrived++
			for i, got := range samples {
				want := scratchSample(from*ch + int64(i))
				if got != want {
					require.Equal(t, want, got, "sample %d of the window at %d", i, from)
				}
			}
		}
		// The frame thread draws the pyramid whether or not the window came.
		tr.Peaks().Columns(0, frames, 0, dstMin, dstMax)
		bp := tr.BuildProgress()
		require.GreaterOrEqual(t, bp.BuiltFrames, built)
		require.NoError(t, bp.Err)
		built = bp.BuiltFrames
		time.Sleep(time.Millisecond)
	}
	require.Positive(t, arrived, "no window arrived while the build ran")
	require.Eventually(t, func() bool { return tr.BuildProgress().Complete },
		30*time.Second, time.Millisecond)
}

func TestWindowAfterCloseIsRefused(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 20_000

	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{})
	require.NoError(t, err)
	cached := waitForWindow(t, tr, 0, 512)
	require.NoError(t, tr.CloseE())

	// A cached window outlives the source it was read from; an uncached one
	// is never scheduled again.
	again, ok := tr.Window(0, 512)
	require.True(t, ok)
	require.Equal(t, cached, again)
	_, ok = tr.Window(512, 1024)
	require.False(t, ok)
	require.False(t, tr.WindowPending())
	requireFetches(t, tr, 1)
}

// TestWindowMatchesTheSynchronousPath is the property the widget rests on: a
// window that arrives off the frame thread holds exactly what a synchronous
// read of the same range would, whatever order the ranges were asked for in
// and whatever the cache evicted along the way.
func TestWindowMatchesTheSynchronousPath(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 2}
	const frames int64 = 30_000
	// Four times the longest window a run can ask for, so eviction happens
	// but no request is ever refused.
	const bound int64 = 4 * 4096 * 2 * 4
	ch := int64(format.Channels)
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		src, err := pcm.NewSynthSourceE(format, frames, testSignal(format, frames))
		require.NoError(rt, err)
		tr, err := OpenE(ctx, src, Options{BaseBin: 16, ChunkFrames: 8192, WindowCacheBytes: bound})
		require.NoError(rt, err)
		defer func() { require.NoError(rt, tr.CloseE()) }()

		for range rapid.IntRange(1, 4).Draw(rt, "requests") {
			from := rapid.Int64Range(0, frames-1).Draw(rt, "from")
			to := from + rapid.Int64Range(1, 4096).Draw(rt, "length")
			got := waitForWindow(rt, tr, from, to)

			want := make([]float32, (min(to, frames)-from)*ch)
			n, err := tr.ReadWindowE(ctx, from, want)
			require.NoError(rt, err)
			require.Equal(rt, len(want)/int(ch), n)
			require.Equal(rt, want, got)

			entries, bytes, _, _, _ := tr.WindowCacheStats()
			require.Positive(rt, entries)
			require.LessOrEqual(rt, bytes, bound, "the cache outgrew its bound")
		}
	})
}
