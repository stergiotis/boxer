package track

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// Options configures [OpenE]. The zero value is what a caller with no
// opinion passes: the default base bin, a relative time base, a
// [sink.Null] with the process clock, one shared source, a synchronous
// build, no peaks cache and no progress reporting.
type Options struct {
	// BaseBin is the frames per level-0 peaks bin; zero takes
	// [peaks.DefaultBaseBin].
	BaseBin int32
	// Epoch is the wall-clock instant of frame 0, or zero for a relative
	// time base (ADR-0208 §SD9).
	Epoch time.Time
	// NewSink builds the transport over the track's source. It is handed the
	// locked adapter, not the source [OpenE] was given, so a sink that reads
	// from its own goroutine needs no locking of its own. Nil takes a
	// [sink.Null] over [sink.RealClock]; a device-backed sink (ADR-0208 M3)
	// and a capability-brokered one are both supplied here, which is why the
	// track never opens a device itself (§SD6).
	NewSink func(src pcm.SourceI) sink.SinkI
	// ChunkFrames is how many frames one read of the peaks build moves; zero
	// takes the default of [peaks.BuildE] rather than a second one here.
	ChunkFrames int
	// Progress, when set, is called after every build chunk with the frames
	// folded so far and the recording's length. It runs on the goroutine
	// doing the build — the caller's for a synchronous build, the build's own
	// when Background is set — so a callback that touches UI state has to get
	// it across to the frame thread itself. A pyramid that came from the
	// cache is not built and reports no progress; [BuildProgress] tells the
	// two apart.
	Progress func(builtFrames int64, totalFrames int64)
	// Reopen opens another independent source over the same recording. When
	// set, the peaks builder and the window cache each get their own source,
	// so a decoder whose random access is a process restart (ffmpeg,
	// ADR-0208 §SD5) is never thrashed by three readers; the sink keeps the
	// source given to [OpenE]. Nil makes every reader share that one source
	// through the locked adapter, which is what a memory or WAV source
	// wants.
	//
	// It is called at most twice, and only for a reader that will run: a
	// pyramid loaded from the cache needs no build source. A reopened source
	// must describe the same recording — same format, same frame count — or
	// the open fails.
	Reopen func(ctx context.Context) (src pcm.SourceI, err error)
	// Background builds the pyramid on a goroutine of its own (ADR-0208
	// §SD4): [OpenE] returns as soon as the levels are allocated, and
	// [Track.Peaks] is a pyramid that fills while the caller draws it.
	// [Track.BuildProgress] follows it.
	Background bool
	// Identity keys the peaks cache (ADR-0208 §SD4) — a hash of the
	// recording's size, modification time and head/tail bytes is what the
	// decoder layer computes for it. Nil neither reads nor writes a cache.
	Identity *peaks.Identity
	// CacheDir overrides the directory holding the peaks file; empty
	// resolves through [ResolvePeaksCacheDir].
	CacheDir string
	// NoCache disables the peaks cache even when Identity is set.
	NoCache bool
	// WindowCacheBytes bounds the raw-window cache of ADR-0208 §SD3; zero
	// takes [DefaultWindowCacheBytes].
	WindowCacheBytes int64
}

// Track is one open recording: its sources, its peaks pyramid, its window
// cache, its transport and its time base (ADR-0208 §SD1). Build one with
// [OpenE]; the zero value answers nothing and must not be used.
//
// Every method is safe to call from any goroutine, except [Track.Window],
// which is the frame thread's.
type Track struct {
	src       *lockedSource
	pyramid   *peaks.Pyramid
	transport sink.SinkI
	wc        *windowCache
	// windowOwned is the [Options.Reopen]ed source the window cache reads
	// through, or nil when it shares src. It lives as long as the track.
	windowOwned pcm.SourceI
	// cancel ends the background build and the window cache's reads.
	cancel context.CancelFunc
	// buildDone is closed when the background build's goroutine has exited;
	// nil when there is no such goroutine.
	buildDone chan struct{}
	outcome   atomic.Pointer[buildOutcome]
	tb        TimeBase
	frames    int64
	fromCache bool

	mu     sync.Mutex
	closed bool
}

// OpenE composes a track over an already-open source and takes ownership of
// it: [Track.CloseE] closes the source, and every error return from OpenE has
// already closed it — along with anything [Options.Reopen] opened — so the
// caller neither closes it on failure nor keeps using it on success.
//
// The pyramid exists by the time OpenE returns, so [Track.Peaks] is never
// nil, but it holds audio only once it has been built or loaded:
//
//   - [Options.Identity] set and no [Options.NoCache]: a matching peaks file
//     makes the track complete at once. A missing file, another recording's
//     identity or an unreadable one is a miss, not an error, and the build
//     runs.
//   - [Options.Background] unset: the build runs to completion before OpenE
//     returns, and a cancelled or failed build is an error like any other.
//   - [Options.Background] set: OpenE returns immediately and the build runs
//     on its own goroutine, publishing the prefix the caller may draw
//     ([Track.BuildProgress]). Its lifetime is the track's, not this call's,
//     so ctx here bounds the open and [Track.CloseE] ends the build.
func OpenE(ctx context.Context, src pcm.SourceI, opts Options) (inst *Track, err error) {
	if src == nil {
		return nil, eh.New("nil source")
	}
	locked := newLockedSource(src)
	var buildOwned pcm.SourceI
	var windowOwned pcm.SourceI
	defer func() {
		if err != nil {
			// Ownership is unconditional: a half-open track leaves nothing
			// for the caller to close.
			closeReopened(buildOwned)
			closeReopened(windowOwned)
			_ = locked.CloseE()
		}
	}()

	format := locked.Format()
	err = format.ValidateE()
	if err != nil {
		return nil, err
	}
	baseBin := opts.BaseBin
	if baseBin == 0 {
		baseBin = peaks.DefaultBaseBin()
	}
	frames := locked.Frames()

	cachePath := ""
	if opts.Identity != nil && !opts.NoCache {
		dir := opts.CacheDir
		if dir == "" {
			dir = ResolvePeaksCacheDir()
		}
		cachePath = filepath.Join(dir, cacheFileName(*opts.Identity, baseBin))
	}
	pyramid, fromCache := readPeaksCache(cachePath, opts.Identity, format, frames, baseBin)
	if pyramid == nil {
		pyramid, err = peaks.NewPyramidE(format, frames, baseBin)
		if err != nil {
			return nil, err
		}
	}

	if opts.Reopen != nil {
		if !fromCache {
			buildOwned, err = openReopenedE(ctx, opts.Reopen, format, frames, "peaks build")
			if err != nil {
				return nil, err
			}
		}
		windowOwned, err = openReopenedE(ctx, opts.Reopen, format, frames, "window cache")
		if err != nil {
			return nil, err
		}
	}

	var progress func(builtFrames int64)
	if opts.Progress != nil {
		report := opts.Progress
		progress = func(builtFrames int64) {
			report(builtFrames, frames)
		}
	}
	job := buildJob{
		src:         locked,
		progress:    progress,
		cachePath:   cachePath,
		chunkFrames: opts.ChunkFrames,
	}
	if opts.Identity != nil {
		// Copied, so that a caller reusing its Identity value cannot change
		// the key a build already under way will write under.
		id := *opts.Identity
		job.identity = &id
	}
	if buildOwned != nil {
		job.src, job.owned = buildOwned, true
	}

	background := opts.Background && !fromCache
	var syncCacheErr error
	if !background {
		if !fromCache {
			err = pyramid.FillFromE(ctx, job.src, job.chunkFrames, job.progress)
			if err != nil {
				return nil, eh.Errorf("unable to build the peaks pyramid: %w", err)
			}
			if cachePath != "" {
				syncCacheErr = writePeaksFileE(cachePath, pyramid, *opts.Identity)
				if syncCacheErr != nil {
					log.Warn().Err(syncCacheErr).Str("path", cachePath).Msg("unable to write the audio peaks cache")
				}
			}
		}
		// Nothing will read through the build's own source again.
		closeReopened(buildOwned)
		buildOwned = nil
	}

	var transport sink.SinkI
	if opts.NewSink == nil {
		transport = sink.NewNull(locked, nil)
	} else {
		transport = opts.NewSink(locked)
		if transport == nil {
			return nil, eh.New("sink constructor returned no sink")
		}
	}

	// The track's own context outlives this call, so a background build and a
	// window fetch are ended by CloseE rather than by the open returning.
	trackCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	windowSrc := pcm.SourceI(locked)
	if windowOwned != nil {
		windowSrc = windowOwned
	}
	inst = &Track{
		src:         locked,
		pyramid:     pyramid,
		transport:   transport,
		wc:          newWindowCache(trackCtx, windowSrc, opts.WindowCacheBytes),
		windowOwned: windowOwned,
		cancel:      cancel,
		tb:          TimeBase{Format: format, Epoch: opts.Epoch},
		frames:      frames,
		fromCache:   fromCache,
	}
	if background {
		inst.buildDone = make(chan struct{})
		go inst.runBuild(trackCtx, job)
	} else if syncCacheErr != nil {
		inst.outcome.Store(&buildOutcome{cacheErr: syncCacheErr})
	}
	return inst, nil
}

// readPeaksCache loads the pyramid of a matching peaks file. Every failure is
// a miss — logged at debug, since a first open is expected to miss — and
// leaves the caller to build.
func readPeaksCache(cachePath string, id *peaks.Identity, format pcm.Format, frames int64, baseBin int32) (pyramid *peaks.Pyramid, fromCache bool) {
	if cachePath == "" || id == nil {
		return nil, false
	}
	pyramid, err := readPeaksFileE(cachePath, *id, format, frames, baseBin)
	if err != nil {
		log.Debug().Err(err).Str("path", cachePath).Msg("audio peaks cache miss")
		return nil, false
	}
	return pyramid, true
}

// openReopenedE opens one more source over the same recording and refuses one
// that describes a different recording, so a mis-wired [Options.Reopen] fails
// the open rather than a read three frames later.
func openReopenedE(ctx context.Context, reopen func(ctx context.Context) (src pcm.SourceI, err error), format pcm.Format, frames int64, role string) (src pcm.SourceI, err error) {
	src, err = reopen(ctx)
	if err != nil {
		return nil, eb.Build().Str("role", role).Errorf("unable to reopen the recording: %w", err)
	}
	if src == nil {
		return nil, eb.Build().Str("role", role).Errorf("reopen returned no source")
	}
	reopenedFormat, reopenedFrames := src.Format(), src.Frames()
	if reopenedFormat != format || reopenedFrames != frames {
		closeReopened(src)
		return nil, eb.Build().
			Str("role", role).
			Uint32("reopenedSampleRate", reopenedFormat.SampleRate).
			Uint16("reopenedChannels", reopenedFormat.Channels).
			Int64("reopenedFrames", reopenedFrames).
			Uint32("sampleRate", format.SampleRate).
			Uint16("channels", format.Channels).
			Int64("frames", frames).
			Errorf("reopened source describes another recording")
	}
	return src, nil
}

// closeReopened closes a source [Options.Reopen] produced. The error is
// logged rather than returned: it reaches no caller who could act on it, and
// on the paths that call this there is already an outcome to report.
func closeReopened(src pcm.SourceI) {
	if src == nil {
		return
	}
	err := src.CloseE()
	if err != nil {
		log.Warn().Err(err).Msg("unable to close a reopened audio source")
	}
}

// Format returns the recording's format.
func (inst *Track) Format() (format pcm.Format) { return inst.tb.Format }

// Frames returns the recording's length.
func (inst *Track) Frames() (frames int64) { return inst.frames }

// Duration returns the recording's length as a duration.
func (inst *Track) Duration() (d time.Duration) { return inst.tb.FrameToDuration(inst.frames) }

// TimeBase returns the base every shown time derives from (ADR-0208 §SD9).
func (inst *Track) TimeBase() (tb TimeBase) { return inst.tb }

// Peaks returns the pyramid the waveform is drawn from. It is never nil, and
// it is safe to read from any goroutine — including while a background build
// fills it, which is what [Track.BuildProgress] reports on.
func (inst *Track) Peaks() (p *peaks.Pyramid) { return inst.pyramid }

// Sink returns the transport. It is never nil.
func (inst *Track) Sink() (s sink.SinkI) {
	inst.mu.Lock()
	s = inst.transport
	inst.mu.Unlock()
	return s
}

// ReplaceSinkE swaps the transport for one built by newSink over the track's
// shared source — the seam through which an output device arrives after the
// track was opened (ADR-0208 SD6: a keelson-brokered capability hands out a
// Sink, the player never opens a device). The new sink takes over the old
// one's position, rate and volume and starts paused; the old sink is closed
// afterwards. When newSink fails the old sink stays in place, paused.
func (inst *Track) ReplaceSinkE(newSink func(src pcm.SourceI) (s sink.SinkI, err error)) (err error) {
	if newSink == nil {
		return eh.New("nil sink constructor")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return eh.New("replace sink on a closed track")
	}
	old := inst.transport
	old.Pause()
	pos, rate, volume := old.Position(), old.Rate(), old.Volume()
	next, err := newSink(inst.src)
	if err != nil {
		return eh.Errorf("unable to open the replacement sink: %w", err)
	}
	if next == nil {
		return eh.New("sink constructor returned no sink")
	}
	var errs []error
	errs = eh.AppendError(errs, next.SeekE(pos))
	errs = eh.AppendError(errs, next.SetRateE(rate))
	errs = eh.AppendError(errs, next.SetVolumeE(volume))
	inst.transport = next
	errs = eh.AppendError(errs, old.CloseE())
	return eh.CheckErrors(errs)
}

// ReadWindowE fills dst with the interleaved frames of
// [fromFrame, fromFrame+len(dst)/Channels) and returns how many frames it
// read, which is fewer than asked for when the window runs past the end of
// the recording and zero when it starts at or past it. Running off the end is
// not an error — a view is clamped, unlike [pcm.SourceI.ReadFramesAtE], which
// reports io.EOF there — but a negative fromFrame is, because clamping it
// would silently shift dst against the frames the caller means to draw.
//
// This is the synchronous raw-frame path: it costs whatever the decoder costs
// and blocks the calling goroutine, which is what a batch job wants and what
// a frame thread must not do. The frame thread's path is [Track.Window],
// where a window that is not cached is fetched off-thread (ADR-0208 §SD3).
// Safe to call from any goroutine, and concurrent calls are serialised by the
// locked source. It reads through the source [OpenE] was given, not through
// an [Options.Reopen]ed one.
func (inst *Track) ReadWindowE(ctx context.Context, fromFrame int64, dst []float32) (n int, err error) {
	return readWindowE(ctx, inst.src, inst.tb.Format, inst.frames, fromFrame, dst)
}

// readWindowE is the read loop behind both [Track.ReadWindowE] and the window
// cache's worker: it clamps the window to the recording and stitches a short
// read rather than reporting a truncated window.
func readWindowE(ctx context.Context, src pcm.SourceI, format pcm.Format, frames int64, fromFrame int64, dst []float32) (n int, err error) {
	if fromFrame < 0 {
		return 0, eb.Build().Int64("fromFrame", fromFrame).Errorf("negative window start")
	}
	channels := int(format.Channels)
	want := format.SamplesToFrames(len(dst))
	if want <= 0 || fromFrame >= frames {
		return 0, nil
	}
	if remaining := frames - fromFrame; int64(want) > remaining {
		want = int(remaining)
	}

	// The source contract makes a read inside the recording complete, so the
	// loop turns at most once for a well-behaved decoder; it is here so a
	// short read is stitched rather than reported as a truncated window.
	for n < want {
		var got int
		got, err = src.ReadFramesAtE(ctx, fromFrame+int64(n), dst[n*channels:want*channels])
		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, eb.Build().
				Int64("fromFrame", fromFrame).
				Int("frames", want).
				Errorf("unable to read a raw window: %w", err)
		}
		if got <= 0 {
			return n, eb.Build().
				Int64("fromFrame", fromFrame).
				Int64("read", int64(n)).
				Int("frames", want).
				Errorf("source delivered no frames inside its declared length")
		}
		n += got
	}
	return n, nil
}

// CloseE ends the background build and the window cache's worker, waits for
// both to have left the sources alone, and then closes the transport and
// every source the track owns. It is idempotent. It waits for a
// [Track.ReadWindowE] in flight rather than closing underneath it; reads
// after it return an error, and windows already cached stay readable. Every
// error is reported, joined.
func (inst *Track) CloseE() (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return nil
	}
	inst.closed = true
	transport := inst.transport
	inst.mu.Unlock()

	// The readers go first, and the lock is not held while they are waited
	// for: a build chunk or a window fetch in flight has to finish, and the
	// frame thread must not queue behind it to learn the track is closing.
	inst.cancel()
	if inst.buildDone != nil {
		<-inst.buildDone
	}
	inst.wc.close()

	var errs []error
	errs = eh.AppendError(errs, transport.CloseE())
	if inst.windowOwned != nil {
		errs = eh.AppendError(errs, inst.windowOwned.CloseE())
	}
	errs = eh.AppendError(errs, inst.src.CloseE())
	return eh.CheckErrors(errs)
}
