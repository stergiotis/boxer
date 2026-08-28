package track

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// gatedSource makes a source observably slow, and slow by the test's clock
// rather than by a sleep: every read waits for a token on release, so a test
// can let the build advance one chunk at a time and assert what is published
// in between. Closing release lets every remaining read through. A cancelled
// context ends the wait, which is what makes a build over this source
// cancellable while it is parked in a read.
type gatedSource struct {
	inner   pcm.SourceI
	release chan struct{}
	delay   time.Duration
	reads   atomic.Int64
	closes  atomic.Int64
}

var _ pcm.SourceI = (*gatedSource)(nil)

func newGatedSource(inner pcm.SourceI, release chan struct{}, delay time.Duration) (inst *gatedSource) {
	return &gatedSource{inner: inner, release: release, delay: delay}
}

func (inst *gatedSource) Format() (format pcm.Format) { return inst.inner.Format() }
func (inst *gatedSource) Frames() (frames int64)      { return inst.inner.Frames() }

func (inst *gatedSource) ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error) {
	select {
	case <-inst.release:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	if inst.delay > 0 {
		time.Sleep(inst.delay)
	}
	inst.reads.Add(1)
	return inst.inner.ReadFramesAtE(ctx, frameOffset, dst)
}

func (inst *gatedSource) CloseE() (err error) {
	inst.closes.Add(1)
	return inst.inner.CloseE()
}

// testIdentity is a synthetic [peaks.Identity]: the decoder layer derives one
// from the recording's bytes, and this package only ever compares it.
func testIdentity(seed byte) (id peaks.Identity) {
	for i := range id.Hash {
		id.Hash[i] = seed*31 + byte(i)
	}
	id.SizeBytes = 1 << 20
	id.ModTimeUnixNano = 1_755_000_000_000_000_000
	return id
}

func cacheFiles(t *testing.T, dir string) (paths []string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*"+CacheFileExt))
	require.NoError(t, err)
	return paths
}

// peaksProfile reduces a pyramid to what a pane would draw from it — the
// whole recording and a zoomed span, per channel — so two pyramids can be
// compared by what they show rather than byte by byte.
func peaksProfile(p *peaks.Pyramid, frames int64) (profile [][]int8) {
	const columns = 64
	spans := [][2]int64{{0, frames}, {frames / 3, frames / 2}, {0, frames / 16}}
	for _, span := range spans {
		for ch := range int(p.Format().Channels) {
			dstMin := make([]int8, columns)
			dstMax := make([]int8, columns)
			p.Columns(span[0], span[1], ch, dstMin, dstMax)
			profile = append(profile, dstMin, dstMax)
		}
	}
	return profile
}

// TestBackgroundBuildPublishesItsPrefix is ADR-0208 §SD4's promise: the open
// returns before the audio has been read, and what has been built is readable
// and grows while the caller draws it.
func TestBackgroundBuildPublishesItsPrefix(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const seconds = 5
	const chunkFrames = 4096
	frames := int64(seconds) * int64(format.SampleRate)

	gate := make(chan struct{})
	src := newGatedSource(newTestSource(t, format, frames), gate, 0)

	var progressMu sync.Mutex
	var reported []int64
	tr, err := OpenE(context.Background(), src, Options{
		ChunkFrames: chunkFrames,
		Background:  true,
		Progress: func(builtFrames int64, totalFrames int64) {
			progressMu.Lock()
			defer progressMu.Unlock()
			assert.Equal(t, frames, totalFrames)
			reported = append(reported, builtFrames)
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

	// OpenE has returned with nothing read: the levels are allocated, the
	// pyramid is drawable and empty.
	require.NotNil(t, tr.Peaks())
	require.Zero(t, src.reads.Load(), "the background build has not been let through the gate yet")
	bp := tr.BuildProgress()
	require.Equal(t, frames, bp.TotalFrames)
	require.Zero(t, bp.BuiltFrames)
	require.False(t, bp.Complete)
	require.False(t, bp.FromCache)
	require.NoError(t, bp.Err)
	require.NoError(t, bp.CacheErr)

	// One chunk at a time: the published prefix is monotone and lands where
	// the fold did.
	last := int64(0)
	for chunk := range 5 {
		gate <- struct{}{}
		want := int64(chunk+1) * chunkFrames
		require.Eventually(t, func() bool { return tr.Peaks().Built() >= want },
			5*time.Second, time.Millisecond, "chunk %d never landed", chunk)
		bp = tr.BuildProgress()
		require.GreaterOrEqual(t, bp.BuiltFrames, last)
		require.Equal(t, tr.Peaks().Built(), bp.BuiltFrames)
		require.False(t, bp.Complete)
		require.NoError(t, bp.Err)
		last = bp.BuiltFrames
	}

	close(gate)
	require.Eventually(t, func() bool { return tr.BuildProgress().Complete },
		30*time.Second, time.Millisecond, "the build never completed")

	bp = tr.BuildProgress()
	require.Equal(t, frames, bp.BuiltFrames)
	require.NoError(t, bp.Err)
	require.NoError(t, bp.CacheErr)
	require.False(t, bp.FromCache)
	require.True(t, tr.Peaks().IsComplete())
	require.Positive(t, tr.Peaks().GlobalPeak())

	progressMu.Lock()
	defer progressMu.Unlock()
	require.NotEmpty(t, reported)
	for i, got := range reported {
		if i > 0 {
			require.GreaterOrEqual(t, got, reported[i-1], "report %d went backwards", i)
		}
		require.LessOrEqual(t, got, frames)
	}
	require.Equal(t, frames, reported[len(reported)-1], "the last report is the whole recording")
}

// TestCloseDuringABackgroundBuildIsPrompt is what makes a background build
// safe to start: the track can be closed while the build is parked in a read,
// and the goroutine is gone by the time CloseE returns.
func TestCloseDuringABackgroundBuildIsPrompt(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := 60 * int64(format.SampleRate)

	before := runtime.NumGoroutine()
	// The gate is never released, so the build is stuck in its first read.
	src := newGatedSource(newTestSource(t, format, frames), make(chan struct{}), 0)
	tr, err := OpenE(context.Background(), src, Options{ChunkFrames: 4096, Background: true})
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, tr.CloseE())
	require.Less(t, time.Since(start), time.Second, "CloseE waited on the parked read")
	require.Equal(t, int64(1), src.closes.Load())

	bp := tr.BuildProgress()
	require.False(t, bp.Complete)
	require.ErrorIs(t, bp.Err, context.Canceled)
	require.Less(t, bp.BuiltFrames, frames)

	// Polled by hand rather than with require.Eventually, which runs its
	// condition on a goroutine of its own and would count that one too.
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.LessOrEqual(t, runtime.NumGoroutine(), before, "a goroutine outlived the track")

	// Idempotent, and still prompt.
	require.NoError(t, tr.CloseE())
}

// TestPeaksCacheRoundTrip is ADR-0208 §SD4's second open: the pyramid comes
// off disk and the recording is not read at all.
func TestPeaksCacheRoundTrip(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 120_000
	ctx := context.Background()
	dir := t.TempDir()
	id := testIdentity(1)

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{
		Identity:    &id,
		CacheDir:    dir,
		ChunkFrames: 8192,
	})
	require.NoError(t, err)
	bp := tr.BuildProgress()
	require.True(t, bp.Complete)
	require.False(t, bp.FromCache)
	require.NoError(t, bp.Err)
	require.NoError(t, bp.CacheErr)
	want := peaksProfile(tr.Peaks(), frames)
	require.NoError(t, tr.CloseE())

	written := cacheFiles(t, dir)
	require.Len(t, written, 1)
	require.Equal(t, cacheFileName(id, peaks.DefaultBaseBin()), filepath.Base(written[0]))

	t.Run("the second open loads it", func(t *testing.T) {
		src := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(ctx, src, Options{Identity: &id, CacheDir: dir})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

		bp := tr.BuildProgress()
		require.True(t, bp.FromCache)
		require.True(t, bp.Complete)
		require.Equal(t, frames, bp.BuiltFrames)
		require.NoError(t, bp.Err)
		require.Zero(t, src.reads.Load(), "a cache hit reads no audio")
		require.True(t, tr.Peaks().IsComplete())
		require.Equal(t, want, peaksProfile(tr.Peaks(), frames))
	})

	t.Run("another identity misses", func(t *testing.T) {
		other := testIdentity(9)
		src := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(ctx, src, Options{Identity: &other, CacheDir: dir, ChunkFrames: 8192})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })

		require.False(t, tr.BuildProgress().FromCache)
		require.True(t, tr.BuildProgress().Complete)
		require.Positive(t, src.reads.Load())
		require.Len(t, cacheFiles(t, dir), 2, "the second recording gets its own file")
		require.Equal(t, want, peaksProfile(tr.Peaks(), frames), "the same audio yields the same pyramid")
	})

	t.Run("another base bin is another file", func(t *testing.T) {
		dir := t.TempDir()
		for _, baseBin := range []int32{64, 256} {
			tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{
				Identity: &id, CacheDir: dir, BaseBin: baseBin, ChunkFrames: 8192,
			})
			require.NoError(t, err)
			require.False(t, tr.BuildProgress().FromCache)
			require.NoError(t, tr.CloseE())
		}
		require.Len(t, cacheFiles(t, dir), 2)
	})
}

func TestPeaksCacheIsWrittenByABackgroundBuild(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 1}
	const frames int64 = 90_000
	ctx := context.Background()
	dir := t.TempDir()
	id := testIdentity(4)

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{
		Identity: &id, CacheDir: dir, Background: true, ChunkFrames: 4096,
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return tr.BuildProgress().Complete },
		30*time.Second, time.Millisecond)
	require.NoError(t, tr.BuildProgress().CacheErr)
	require.NoError(t, tr.CloseE())
	require.Len(t, cacheFiles(t, dir), 1)

	tr, err = OpenE(ctx, newTestSource(t, format, frames), Options{Identity: &id, CacheDir: dir, Background: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	bp := tr.BuildProgress()
	require.True(t, bp.FromCache, "a cache hit skips the background build")
	require.True(t, bp.Complete)
}

// TestNoCacheNeitherReadsNorWrites keeps the knob honest: an identity is what
// the cache is keyed by, not what turns it on.
func TestNoCacheNeitherReadsNorWrites(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	const frames int64 = 40_000
	ctx := context.Background()
	id := testIdentity(7)

	t.Run("nothing is written", func(t *testing.T) {
		dir := t.TempDir()
		tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{
			Identity: &id, CacheDir: dir, NoCache: true,
		})
		require.NoError(t, err)
		require.NoError(t, tr.CloseE())
		require.Empty(t, cacheFiles(t, dir))
	})

	t.Run("a file that is there is ignored", func(t *testing.T) {
		dir := t.TempDir()
		tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{Identity: &id, CacheDir: dir})
		require.NoError(t, err)
		require.NoError(t, tr.CloseE())
		primed := cacheFiles(t, dir)
		require.Len(t, primed, 1)
		info, err := os.Stat(primed[0])
		require.NoError(t, err)

		src := newCountingSource(newTestSource(t, format, frames))
		tr, err = OpenE(ctx, src, Options{Identity: &id, CacheDir: dir, NoCache: true})
		require.NoError(t, err)
		require.NoError(t, tr.CloseE())
		require.False(t, tr.BuildProgress().FromCache)
		require.Positive(t, src.reads.Load())

		again, err := os.Stat(primed[0])
		require.NoError(t, err)
		require.Equal(t, info.ModTime(), again.ModTime(), "the file was not rewritten")
	})

	t.Run("no identity is no cache", func(t *testing.T) {
		dir := t.TempDir()
		tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{CacheDir: dir})
		require.NoError(t, err)
		require.NoError(t, tr.CloseE())
		require.Empty(t, cacheFiles(t, dir))
	})
}

// TestCacheDirComesFromTheEnvVar exercises the registry member ADR-0208's
// surfaces table adds (BOXER_AUDIO_PEAKS_CACHE_DIR).
func TestCacheDirComesFromTheEnvVar(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	const frames int64 = 40_000
	ctx := context.Background()
	dir := t.TempDir()
	id := testIdentity(11)

	PeaksCacheDir.SetForTest(t, filepath.Join(dir, "peaks"))
	require.Equal(t, filepath.Join(dir, "peaks"), ResolvePeaksCacheDir())

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{Identity: &id})
	require.NoError(t, err)
	require.NoError(t, tr.BuildProgress().CacheErr)
	require.NoError(t, tr.CloseE())
	require.Len(t, cacheFiles(t, filepath.Join(dir, "peaks")), 1, "the directory is created and written to")

	tr, err = OpenE(ctx, newTestSource(t, format, frames), Options{Identity: &id})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	require.True(t, tr.BuildProgress().FromCache)
}

// TestPeaksCacheMissesRatherThanFails covers the miss semantics: a cache is
// never a reason to fail an open.
func TestPeaksCacheMissesRatherThanFails(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	const frames int64 = 20_000
	ctx := context.Background()
	id := testIdentity(13)

	t.Run("a corrupt file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, cacheFileName(id, peaks.DefaultBaseBin()))
		require.NoError(t, os.WriteFile(path, []byte("not a peaks file at all"), 0o644))

		src := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(ctx, src, Options{Identity: &id, CacheDir: dir})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
		require.False(t, tr.BuildProgress().FromCache)
		require.True(t, tr.BuildProgress().Complete)
		require.Positive(t, src.reads.Load())
		// The build overwrote it, so the next open is a hit.
		require.Len(t, cacheFiles(t, dir), 1)
	})

	t.Run("a cache directory that cannot be written", func(t *testing.T) {
		// A file where the directory should be: MkdirAll fails, the build
		// does not.
		dir := filepath.Join(t.TempDir(), "blocked")
		require.NoError(t, os.WriteFile(dir, nil, 0o644))

		tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{Identity: &id, CacheDir: dir})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
		bp := tr.BuildProgress()
		require.True(t, bp.Complete, "a cache that cannot be written is not a failed build")
		require.NoError(t, bp.Err)
		require.Error(t, bp.CacheErr)
	})

	t.Run("a cache directory that cannot be written by a background build", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "blocked")
		require.NoError(t, os.WriteFile(dir, nil, 0o644))

		tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{
			Identity: &id, CacheDir: dir, Background: true, ChunkFrames: 4096,
		})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
		require.Eventually(t, func() bool { return tr.BuildProgress().Complete },
			30*time.Second, time.Millisecond)
		bp := tr.BuildProgress()
		require.NoError(t, bp.Err)
		require.Error(t, bp.CacheErr)
	})
}

// TestReopenGivesEachReaderItsOwnSource is ADR-0208 §SD5's decoder in mind:
// the build and the window cache must not share a file position with each
// other or with the sink.
func TestReopenGivesEachReaderItsOwnSource(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 60_000
	ctx := context.Background()

	var reopened []*countingSource
	primary := newCountingSource(newTestSource(t, format, frames))
	tr, err := OpenE(ctx, primary, Options{
		ChunkFrames: 8192,
		Reopen: func(_ context.Context) (src pcm.SourceI, err error) {
			s := newCountingSource(newTestSource(t, format, frames))
			reopened = append(reopened, s)
			return s, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, reopened, 2, "one source for the build, one for the window cache")

	build, window := reopened[0], reopened[1]
	require.Positive(t, build.reads.Load(), "the build read through its own source")
	require.Equal(t, int64(1), build.closes.Load(), "a finished build closes its source at once")
	require.Zero(t, primary.reads.Load(), "the source OpenE was given is the sink's")
	require.Zero(t, window.closes.Load())

	// The window cache reads through its own source, not the sink's.
	const windowFrames int64 = 2048
	got := waitForWindow(t, tr, 0, windowFrames)
	require.Len(t, got, int(windowFrames)*int(format.Channels))
	require.Positive(t, window.reads.Load())
	require.Zero(t, primary.reads.Load())

	require.NoError(t, tr.CloseE())
	require.Equal(t, int64(1), window.closes.Load())
	require.Equal(t, int64(1), primary.closes.Load())
	require.Equal(t, int64(1), build.closes.Load(), "the build's source is not closed twice")
}

func TestReopenSkipsTheBuildSourceOnACacheHit(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	const frames int64 = 40_000
	ctx := context.Background()
	dir := t.TempDir()
	id := testIdentity(17)

	tr, err := OpenE(ctx, newTestSource(t, format, frames), Options{Identity: &id, CacheDir: dir})
	require.NoError(t, err)
	require.NoError(t, tr.CloseE())

	calls := 0
	tr, err = OpenE(ctx, newTestSource(t, format, frames), Options{
		Identity: &id,
		CacheDir: dir,
		Reopen: func(_ context.Context) (src pcm.SourceI, err error) {
			calls++
			return newTestSource(t, format, frames), nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.CloseE()) })
	require.True(t, tr.BuildProgress().FromCache)
	require.Equal(t, 1, calls, "only the window cache needs a source of its own")
}

func TestReopenFailuresCloseWhatWasOpened(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 8192
	ctx := context.Background()

	t.Run("the first call fails", func(t *testing.T) {
		primary := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(ctx, primary, Options{
			Reopen: func(_ context.Context) (src pcm.SourceI, err error) {
				return nil, eh.New("the decoder is not there")
			},
		})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Equal(t, int64(1), primary.closes.Load())
	})

	t.Run("the second call fails", func(t *testing.T) {
		primary := newCountingSource(newTestSource(t, format, frames))
		var first *countingSource
		tr, err := OpenE(ctx, primary, Options{
			Reopen: func(_ context.Context) (src pcm.SourceI, err error) {
				if first == nil {
					first = newCountingSource(newTestSource(t, format, frames))
					return first, nil
				}
				return nil, eh.New("the decoder went away")
			},
		})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Equal(t, int64(1), primary.closes.Load())
		require.Equal(t, int64(1), first.closes.Load(), "the build's source went with the failed open")
	})

	t.Run("no source", func(t *testing.T) {
		primary := newCountingSource(newTestSource(t, format, frames))
		tr, err := OpenE(ctx, primary, Options{
			Reopen: func(_ context.Context) (src pcm.SourceI, err error) { return nil, nil },
		})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Equal(t, int64(1), primary.closes.Load())
	})

	t.Run("another recording", func(t *testing.T) {
		primary := newCountingSource(newTestSource(t, format, frames))
		other := newCountingSource(newTestSource(t, format, frames*2))
		tr, err := OpenE(ctx, primary, Options{
			Reopen: func(_ context.Context) (src pcm.SourceI, err error) { return other, nil },
		})
		require.Error(t, err)
		require.Nil(t, tr)
		require.Equal(t, int64(1), primary.closes.Load())
		require.Equal(t, int64(1), other.closes.Load())
	})
}

func TestBackgroundBuildOfAnEmptyRecording(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	dir := t.TempDir()
	id := testIdentity(23)

	tr, err := OpenE(context.Background(), newTestSource(t, format, 0), Options{
		Background: true, Identity: &id, CacheDir: dir,
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return tr.BuildProgress().Complete },
		10*time.Second, time.Millisecond)
	bp := tr.BuildProgress()
	require.NoError(t, bp.Err)
	require.NoError(t, bp.CacheErr)
	require.Zero(t, bp.BuiltFrames)
	require.Zero(t, bp.TotalFrames)

	samples, ok := tr.Window(0, 1024)
	require.True(t, ok)
	require.Empty(t, samples)
	require.NoError(t, tr.CloseE())
	require.Len(t, cacheFiles(t, dir), 1)
}
