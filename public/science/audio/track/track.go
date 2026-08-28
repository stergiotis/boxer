package track

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// Options configures [OpenE]. The zero value is what a caller with no
// opinion passes: the default base bin, a relative time base, a
// [sink.Null] with the process clock, and no progress reporting.
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
	// doing the build, which in this milestone is the caller's, so a callback
	// that touches UI state has to get it across to the frame thread itself.
	Progress func(builtFrames int64, totalFrames int64)
}

// Track is one open recording: its source, its peaks pyramid, its transport
// and its time base (ADR-0208 §SD1). Build one with [OpenE]; the zero value
// answers nothing and must not be used.
//
// Every method is safe to call from any goroutine.
type Track struct {
	src       *lockedSource
	pyramid   *peaks.Pyramid
	transport sink.SinkI
	tb        TimeBase
	frames    int64

	mu     sync.Mutex
	closed bool
}

// OpenE composes a track over an already-open source and takes ownership of
// it: [Track.CloseE] closes the source, and every error return from OpenE has
// already closed it, so the caller neither closes it on failure nor keeps
// using it on success.
//
// The peaks pyramid is built in full before OpenE returns, so a track is
// complete the moment it exists (ADR-0208 M1). The context is honoured
// between build chunks and inside the source's reads; a cancelled build is an
// error like any other. ADR-0208 §SD4's background build is M4 and is an
// added [Options] field rather than a different signature — the progress a
// partial build publishes is already readable from [Track.Peaks].
func OpenE(ctx context.Context, src pcm.SourceI, opts Options) (inst *Track, err error) {
	if src == nil {
		return nil, eh.New("nil source")
	}
	locked := newLockedSource(src)
	defer func() {
		if err != nil {
			// Ownership is unconditional: a half-open track leaves nothing
			// for the caller to close.
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
	var progress func(builtFrames int64)
	if opts.Progress != nil {
		report := opts.Progress
		progress = func(builtFrames int64) {
			report(builtFrames, frames)
		}
	}
	pyramid, err := peaks.BuildE(ctx, locked, baseBin, opts.ChunkFrames, progress)
	if err != nil {
		return nil, eh.Errorf("unable to build the peaks pyramid: %w", err)
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

	return &Track{
		src:       locked,
		pyramid:   pyramid,
		transport: transport,
		tb:        TimeBase{Format: format, Epoch: opts.Epoch},
		frames:    frames,
	}, nil
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
// it is safe to read from any goroutine.
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
// This is the raw-frame path ADR-0208 §SD3 hands the deepest zoom levels. In
// this milestone it is a direct, synchronous read through the shared source,
// so it costs whatever the decoder costs and blocks the calling goroutine;
// §SD3's byte-bounded cache and its off-thread fetch go in front of it in M4,
// at which point a frame that would have waited gets a miss instead. Safe to
// call from any goroutine, and concurrent calls are serialised by the locked
// source.
func (inst *Track) ReadWindowE(ctx context.Context, fromFrame int64, dst []float32) (n int, err error) {
	if fromFrame < 0 {
		return 0, eb.Build().Int64("fromFrame", fromFrame).Errorf("negative window start")
	}
	format := inst.tb.Format
	channels := int(format.Channels)
	want := format.SamplesToFrames(len(dst))
	if want <= 0 || fromFrame >= inst.frames {
		return 0, nil
	}
	if remaining := inst.frames - fromFrame; int64(want) > remaining {
		want = int(remaining)
	}

	// The source contract makes a read inside the recording complete, so the
	// loop turns at most once for a well-behaved decoder; it is here so a
	// short read is stitched rather than reported as a truncated window.
	for n < want {
		var got int
		got, err = inst.src.ReadFramesAtE(ctx, fromFrame+int64(n), dst[n*channels:want*channels])
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

// CloseE closes the transport and then the source, and is idempotent. It
// waits for a [Track.ReadWindowE] in flight rather than closing underneath
// it; reads after it return an error. Both errors are reported, joined.
func (inst *Track) CloseE() (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil
	}
	inst.closed = true
	var errs []error
	errs = eh.AppendError(errs, inst.transport.CloseE())
	errs = eh.AppendError(errs, inst.src.CloseE())
	return eh.CheckErrors(errs)
}
