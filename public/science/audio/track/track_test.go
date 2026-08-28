package track

import (
	"context"
	"io"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// countingSource counts the closes and reads it forwards, so a test can
// assert the ownership rule of [OpenE]. A nil inner source with a zero
// format is the invalid source that reaches the pre-build error path.
type countingSource struct {
	inner  pcm.SourceI
	format pcm.Format
	frames int64
	closes atomic.Int64
	reads  atomic.Int64
}

var _ pcm.SourceI = (*countingSource)(nil)

func newCountingSource(inner pcm.SourceI) (inst *countingSource) {
	return &countingSource{inner: inner, format: inner.Format(), frames: inner.Frames()}
}

func (inst *countingSource) Format() (format pcm.Format) { return inst.format }
func (inst *countingSource) Frames() (frames int64)      { return inst.frames }

func (inst *countingSource) ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error) {
	inst.reads.Add(1)
	if inst.inner == nil {
		return 0, io.EOF
	}
	return inst.inner.ReadFramesAtE(ctx, frameOffset, dst)
}

func (inst *countingSource) CloseE() (err error) {
	inst.closes.Add(1)
	if inst.inner == nil {
		return nil
	}
	return inst.inner.CloseE()
}

// testSignal is a two-channel signal with a gated tone on the left and a
// sweep on the right — enough structure that the pyramid's bins differ and a
// window comparison is not comparing silence.
func testSignal(format pcm.Format, frames int64) (fn pcm.SampleFunc) {
	return pcm.PerChannel(
		pcm.Gate(pcm.Sine(format, 440, 0.9), 4800, 2400),
		pcm.Chirp(format, frames, 50, 8000, 0.7),
	)
}

func newTestSource(t *testing.T, format pcm.Format, frames int64) (src *pcm.SynthSource) {
	t.Helper()
	src, err := pcm.NewSynthSourceE(format, frames, testSignal(format, frames))
	require.NoError(t, err)
	return src
}

func TestOpenBuildsACompleteTrack(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const seconds = 3
	frames := int64(seconds) * int64(format.SampleRate)

	type step struct {
		built int64
		total int64
	}
	var progress []step
	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{
		ChunkFrames: 4096,
		Progress: func(builtFrames int64, totalFrames int64) {
			progress = append(progress, step{built: builtFrames, total: totalFrames})
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	require.Equal(t, format, tr.Format())
	require.Equal(t, frames, tr.Frames())
	require.Equal(t, seconds*time.Second, tr.Duration())

	tb := tr.TimeBase()
	require.Equal(t, format, tb.Format)
	require.False(t, tb.IsAbsolute())
	require.Equal(t, tr.Duration(), tb.FrameToDuration(frames))
	_, ok := tb.FrameToTime(0)
	require.False(t, ok)

	p := tr.Peaks()
	require.NotNil(t, p)
	require.True(t, p.IsComplete())
	require.Equal(t, frames, p.Built())
	require.Equal(t, frames, p.Frames())
	require.Equal(t, peaks.DefaultBaseBin(), p.BaseBin())
	require.Positive(t, p.GlobalPeak())

	s := tr.Sink()
	require.NotNil(t, s)
	null, ok := s.(*sink.Null)
	require.True(t, ok, "a track with no NewSink gets a Null")
	require.Equal(t, frames, null.Frames())
	require.Equal(t, format, null.Format())
	require.Equal(t, sink.StateStopped, null.State())

	require.NotEmpty(t, progress)
	for i, got := range progress {
		require.Equal(t, frames, got.total, "step %d reports the recording's length", i)
		if i > 0 {
			require.GreaterOrEqual(t, got.built, progress[i-1].built, "the built prefix must not shrink")
		}
		require.LessOrEqual(t, got.built, frames)
	}
	require.Equal(t, frames, progress[len(progress)-1].built, "the last report is the whole recording")
}

func TestOpenRespectsTheBaseBinAndTheEpoch(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	const frames int64 = 4000
	epoch := time.Date(2026, time.August, 28, 9, 30, 0, 0, time.UTC)

	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{
		BaseBin: 64,
		Epoch:   epoch,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	require.Equal(t, int32(64), tr.Peaks().BaseBin())
	tb := tr.TimeBase()
	require.True(t, tb.IsAbsolute())
	at, ok := tb.FrameToTime(0)
	require.True(t, ok)
	require.True(t, at.Equal(epoch))
	at, ok = tb.FrameToTime(frames)
	require.True(t, ok)
	require.True(t, at.Equal(epoch.Add(tr.Duration())))
}

func TestOpenAnEmptyRecording(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	tr, err := OpenE(context.Background(), newTestSource(t, format, 0), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	require.Equal(t, int64(0), tr.Frames())
	require.Equal(t, time.Duration(0), tr.Duration())
	require.True(t, tr.Peaks().IsComplete())

	n, err := tr.ReadWindowE(context.Background(), 0, make([]float32, 64))
	require.NoError(t, err)
	require.Zero(t, n)
}

// TestOpenClosesTheSourceOnEveryErrorPath is the ownership rule: whatever
// fails, the source OpenE was handed is closed before it returns, so a caller
// never has to guess.
func TestOpenClosesTheSourceOnEveryErrorPath(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 96_000

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		src := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(ctx, src, Options{})
		require.Error(t, err)
		require.Nil(t, tr)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, int64(1), src.closes.Load())
	})

	t.Run("invalid format", func(t *testing.T) {
		src := &countingSource{}
		tr, err := OpenE(context.Background(), src, Options{})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Zero(t, src.reads.Load(), "an invalid format is rejected before the build reads")
		require.Equal(t, int64(1), src.closes.Load())
	})

	t.Run("rejected base bin", func(t *testing.T) {
		src := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(context.Background(), src, Options{BaseBin: 100})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Equal(t, int64(1), src.closes.Load())
	})

	t.Run("sink constructor returns nothing", func(t *testing.T) {
		src := newCountingSource(newTestSource(t, format, 4096))
		tr, err := OpenE(context.Background(), src, Options{
			NewSink: func(_ pcm.SourceI) sink.SinkI { return nil },
		})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Equal(t, int64(1), src.closes.Load())
	})

	t.Run("nil source", func(t *testing.T) {
		tr, err := OpenE(context.Background(), nil, Options{})
		require.Error(t, err)
		require.Nil(t, tr)
	})
}

// TestNewSinkIsHandedTheLockedAdapter is what makes the M3 sink's callback
// goroutine safe without locking of its own.
func TestNewSinkIsHandedTheLockedAdapter(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 4096
	var handed pcm.SourceI
	tr, err := OpenE(context.Background(), newTestSource(t, format, frames), Options{
		NewSink: func(src pcm.SourceI) sink.SinkI {
			handed = src
			return sink.NewNull(src, sink.NewManualClock(time.Unix(0, 0)))
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	require.IsType(t, (*lockedSource)(nil), handed)
	require.Equal(t, frames, handed.Frames())
}

func TestReadWindowMatchesADirectRead(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 60_000
	ch := int(format.Channels)
	ctx := context.Background()

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	reference := newTestSource(t, format, frames)

	for _, tc := range []struct {
		from  int64
		count int
	}{
		{from: 0, count: 1},
		{from: 0, count: 4096},
		{from: 1, count: 333},
		{from: 12_345, count: 1024},
		{from: frames - 4096, count: 4096},
	} {
		got := make([]float32, tc.count*ch)
		n, err := tr.ReadWindowE(ctx, tc.from, got)
		require.NoError(t, err)
		require.Equal(t, tc.count, n, "window of %d frames from %d", tc.count, tc.from)

		want := make([]float32, tc.count*ch)
		rn, err := reference.ReadFramesAtE(ctx, tc.from, want)
		require.NoError(t, err)
		require.Equal(t, tc.count, rn)
		require.Equal(t, want, got, "window from %d", tc.from)
	}
}

func TestReadWindowClampsAtTheEnd(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 3}
	const frames int64 = 1000
	ch := int(format.Channels)
	ctx := context.Background()

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{BaseBin: 16})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	reference := newTestSource(t, format, frames)

	// A window straddling the end reads the frames that exist, and the
	// remainder of dst is untouched.
	dst := make([]float32, 100*ch)
	n, err := tr.ReadWindowE(ctx, frames-10, dst)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	want := make([]float32, 10*ch)
	_, err = reference.ReadFramesAtE(ctx, frames-10, want)
	require.NoError(t, err)
	require.Equal(t, want, dst[:10*ch])
	require.Equal(t, make([]float32, 90*ch), dst[10*ch:])

	// At and past the end there is nothing to read, and that is not an
	// error: a view is clamped, not refused.
	for _, from := range []int64{frames, frames + 1, frames + 10_000} {
		n, err = tr.ReadWindowE(ctx, from, dst)
		require.NoError(t, err, "window from %d", from)
		require.Zero(t, n, "window from %d", from)
	}

	// A dst that cannot hold a whole frame reads nothing.
	n, err = tr.ReadWindowE(ctx, 0, dst[:ch-1])
	require.NoError(t, err)
	require.Zero(t, n)

	// A negative start is an error rather than a silently shifted window.
	n, err = tr.ReadWindowE(ctx, -1, dst)
	require.Error(t, err)
	require.Zero(t, n)
}

// TestReadWindowConcurrentWhilePlaying is the M3 shape brought forward: four
// frame-thread-style readers and a transport being polled, all over one
// source, under the race detector.
func TestReadWindowConcurrentWhilePlaying(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 80_000
	ch := int(format.Channels)
	clock := sink.NewManualClock(time.Unix(0, 0))

	raw := newScratchSource(format, frames)
	tr, err := OpenE(context.Background(), raw, Options{
		BaseBin: 64,
		NewSink: func(src pcm.SourceI) sink.SinkI { return sink.NewNull(src, clock) },
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	tr.Sink().Play()
	require.Equal(t, sink.StatePlaying, tr.Sink().State())

	const readers = 4
	const readsPerGoroutine = 150
	var wg sync.WaitGroup
	for g := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(0xbeef, uint64(g)))
			dst := make([]float32, 256*ch)
			for range readsPerGoroutine {
				from := r.Int64N(frames)
				want := 1 + r.IntN(256)
				n, err := tr.ReadWindowE(context.Background(), from, dst[:want*ch])
				if !assert.NoError(t, err) {
					return
				}
				if !assert.Positive(t, n) {
					return
				}
				for i := range n * ch {
					if !assert.Equal(t, scratchSample(from*int64(ch)+int64(i)), dst[i], "sample %d of a window from %d", i, from) {
						return
					}
				}
				// The transport is polled from the same goroutines the
				// widget would poll it from.
				pos := tr.Sink().Position()
				assert.GreaterOrEqual(t, pos, int64(0))
				assert.LessOrEqual(t, pos, frames)
			}
		}()
	}
	// The clock runs while the reads do, so the transport is settling
	// concurrently with them.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			clock.Advance(time.Millisecond)
		}
	}()
	wg.Wait()
}

func TestCloseClosesBothAndIsIdempotent(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src := newCountingSource(newTestSource(t, format, 8192))
	tr, err := OpenE(context.Background(), src, Options{BaseBin: 16})
	require.NoError(t, err)

	require.NoError(t, tr.CloseE())
	require.NoError(t, tr.CloseE())
	require.Equal(t, int64(1), src.closes.Load(), "the source is closed once")

	// The transport went with it.
	require.Error(t, tr.Sink().SeekE(0))

	n, err := tr.ReadWindowE(context.Background(), 0, make([]float32, 64))
	require.Error(t, err)
	require.Zero(t, n)

	// The pyramid outlives the source it was built from; it holds no
	// reference to it.
	require.True(t, tr.Peaks().IsComplete())
}
