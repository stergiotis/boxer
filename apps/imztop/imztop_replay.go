package imztop

import (
	"context"
	"iter"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// BundleSourceI is where a ReplaySampler gets its bundles: one window of
// stored history, ascending in time. [sysmreplay.Reader] implements it.
//
// It is an interface so the transport can be exercised against a synthetic
// window with no database, which is the only way the pacing and stepping
// behaviour is testable at all.
type BundleSourceI interface {
	All(ctx context.Context, w sysmreplay.Window) iter.Seq2[*sysmsnap.BundleSnapshot, error]
}

// Defaults for [ReplayOptions].
const (
	// DefaultReplaySpeed plays history at the rate it was recorded.
	DefaultReplaySpeed = 1.0
	// MaxReplayGap caps how long the transport waits between two consecutive
	// bundles. Stored history has gaps — the tee is opt-in, it drops bundles
	// under back-pressure, and the scraper stops when the box does — and
	// honouring one literally would park replay for the length of the outage.
	// The gap is compressed to this, so a break in history reads as a pause in
	// the plots rather than as a hung UI.
	MaxReplayGap = 2 * time.Second
)

// ReplayOptions configures a [ReplaySampler]. Source is required.
type ReplayOptions struct {
	// Source supplies the stored bundles.
	Source BundleSourceI
	// Window is the range to replay. Seek reopens the source with From moved.
	Window sysmreplay.Window
	// Sampler sizes the history windows the bundles fold into, exactly as for
	// a live Sampler.
	Sampler SamplerOptions
	// Speed multiplies playback rate: 1 is as recorded, 2 twice as fast, 0.5
	// half. Zero takes DefaultReplaySpeed.
	Speed float64
	// StartPaused holds the transport at the first bundle until Pause(false)
	// or Step.
	StartPaused bool
	Log         zerolog.Logger
}

// ReplaySampler is the [SamplerI] that plays stored history (ADR-0197 §SD1).
//
// It is a second *source* into the fold a live Sampler already has, not a
// subtype of one: the sliding windows, the per-process EWMA and the published
// snapshot are the same code, reached through the same onBundle. What differs
// is where the bundles come from and that they arrive on a transport the user
// drives.
//
// # Two clocks
//
// The bundles carry the timestamps they were recorded with, so the plot time
// axis and the observed-cadence readout report the historical run rather than
// this playback (ADR-0197 §SD3). Wall-clock pacing is therefore free to mean
// speed, and the two never have to be reconciled.
//
// # Off the render thread
//
// Reading the store blocks. Every read happens on the goroutine Start spawns,
// and the renderer only ever touches the atomically-published snapshot the fold
// already gives it — Latest is as cheap here as it is live.
type ReplaySampler struct {
	// fold is a Sampler with no consumer: the windowing half, driven directly.
	fold   *Sampler
	source BundleSourceI
	log    zerolog.Logger

	// paused stops the transport. It is the same control as a live Sampler's
	// freeze from the user's side, but it stops advancing rather than dropping
	// frames — there is nothing arriving to drop.
	paused atomic.Bool
	// speedBits is the playback multiplier as float64 bits.
	speedBits atomic.Uint64
	// steps counts single-bundle advances requested while paused.
	steps atomic.Int64
	// posMs is the stamp of the most recently folded bundle, 0 before the
	// first.
	posMs atomic.Int64
	// exhausted is set when the window has been played to its end.
	exhausted atomic.Bool

	// wake nudges the transport goroutine to re-read the controls. Buffered so
	// a signal is never lost and never blocks the caller.
	wake chan struct{}

	// mu guards window, which Seek rewrites.
	mu     sync.Mutex
	window sysmreplay.Window
	// seekPending tells the transport to abandon its cursor and reopen.
	seekPending atomic.Bool

	cancel context.CancelFunc
	done   chan struct{}
	closed atomic.Bool
}

var _ SamplerI = (*ReplaySampler)(nil)

// NewReplaySampler validates opts and returns a sampler that is not yet
// running; call Start.
func NewReplaySampler(opts ReplayOptions) (inst *ReplaySampler, err error) {
	if opts.Source == nil {
		err = eh.Errorf("imztop: replay sampler needs a Source")
		return
	}
	if opts.Speed <= 0 {
		opts.Speed = DefaultReplaySpeed
	}
	inst = &ReplaySampler{
		fold:   newFold(opts.Sampler),
		source: opts.Source,
		log:    opts.Log,
		wake:   make(chan struct{}, 1),
		window: opts.Window,
	}
	inst.speedBits.Store(math.Float64bits(opts.Speed))
	inst.paused.Store(opts.StartPaused)
	return
}

// Start launches the transport goroutine. Calling it twice is a no-op after
// the first.
func (inst *ReplaySampler) Start(ctx context.Context) {
	if inst.done != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.run(runCtx)
}

// Latest returns the most recently folded frame, or nil before the first.
func (inst *ReplaySampler) Latest() (snap *PublishedSnapshot) {
	snap = inst.fold.Latest()
	return
}

// Pause stops or resumes the transport.
func (inst *ReplaySampler) Pause(p bool) {
	inst.paused.Store(p)
	inst.nudge()
}

// IsPaused reports whether the transport is stopped.
func (inst *ReplaySampler) IsPaused() (p bool) {
	p = inst.paused.Load()
	return
}

// Interval reports the cadence of the recorded run, not of this playback: it
// comes from the fold, which derives it from consecutive bundles' own stamps
// (ADR-0197 §SD3).
func (inst *ReplaySampler) Interval() (d time.Duration) {
	d = inst.fold.Interval()
	return
}

// Close stops the transport and waits for the goroutine to finish.
func (inst *ReplaySampler) Close() (err error) {
	if !inst.closed.CompareAndSwap(false, true) {
		return
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.nudge()
	if inst.done != nil {
		<-inst.done
	}
	err = inst.fold.Close()
	return
}

// SetSpeed changes the playback multiplier. Values <= 0 are ignored.
func (inst *ReplaySampler) SetSpeed(x float64) {
	if x <= 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return
	}
	inst.speedBits.Store(math.Float64bits(x))
	inst.nudge()
}

// Speed reports the playback multiplier.
func (inst *ReplaySampler) Speed() (x float64) {
	x = math.Float64frombits(inst.speedBits.Load())
	return
}

// Step advances n bundles while paused, ignoring pacing. It is a no-op when
// n <= 0 and has no effect while playing, where the transport is already
// advancing.
func (inst *ReplaySampler) Step(n int) {
	if n <= 0 {
		return
	}
	inst.steps.Add(int64(n))
	inst.nudge()
}

// Seek restarts the transport at t, which becomes the window's lower bound.
// The fold keeps whatever history it has already accumulated; a caller wanting
// a clean plot builds a new sampler.
func (inst *ReplaySampler) Seek(t time.Time) {
	inst.mu.Lock()
	to := inst.window.To
	inst.mu.Unlock()
	inst.SeekWindow(t, to)
}

// SeekWindow restarts the transport over a new range, moving both bounds. It is
// what a jog control needs: stepping to the previous window changes where the
// replay ends as well as where it starts.
func (inst *ReplaySampler) SeekWindow(from, to time.Time) {
	inst.mu.Lock()
	inst.window.From = from
	inst.window.To = to
	inst.mu.Unlock()
	inst.exhausted.Store(false)
	inst.seekPending.Store(true)
	inst.nudge()
}

// Position reports the stamp of the most recently folded bundle. ok is false
// before the first one.
func (inst *ReplaySampler) Position() (at time.Time, ok bool) {
	ms := inst.posMs.Load()
	if ms == 0 {
		return
	}
	at, ok = time.UnixMilli(ms).UTC(), true
	return
}

// Exhausted reports whether the window has been played to its end. The
// transport parks there rather than closing, so Seek can restart it.
func (inst *ReplaySampler) Exhausted() (done bool) {
	done = inst.exhausted.Load()
	return
}

// Window reports the range being replayed.
func (inst *ReplaySampler) Window() (w sysmreplay.Window) {
	inst.mu.Lock()
	w = inst.window
	inst.mu.Unlock()
	return
}

// nudge wakes the transport without blocking if a wake is already pending.
func (inst *ReplaySampler) nudge() {
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

// run is the transport goroutine: play the window, park at its end, and reopen
// wherever a Seek moves it.
func (inst *ReplaySampler) run(ctx context.Context) {
	defer close(inst.done)
	for {
		if ctx.Err() != nil {
			return
		}
		inst.seekPending.Store(false)
		if !inst.stream(ctx, inst.Window()) {
			return
		}
		if inst.seekPending.Load() {
			continue // a Seek cut the cursor short; reopen at the new bound
		}
		inst.exhausted.Store(true)
		if !inst.park(ctx) {
			return
		}
	}
}

// stream plays one window through the fold. It returns false when the context
// ended, and true when the window ran out or a Seek asked for a new one.
func (inst *ReplaySampler) stream(ctx context.Context, w sysmreplay.Window) (ok bool) {
	next, stop := iter.Pull2(inst.source.All(ctx, w))
	defer stop()

	prevMs := int64(0)
	for {
		snap, err, more := next()
		if err != nil {
			inst.log.Warn().Err(err).Msg("imztop: replay read failed")
			return true // park; a Seek or a retry is the caller's move
		}
		if !more {
			return true
		}
		if snap == nil {
			continue
		}
		gate := inst.awaitTurn(ctx, recordedGap(prevMs, snap.SampledAtUnixMs), time.Now())
		switch gate {
		case gateStop:
			return false
		case gateReseek:
			return true
		}
		inst.fold.onBundle(snap)
		inst.posMs.Store(snap.SampledAtUnixMs)
		prevMs = snap.SampledAtUnixMs
	}
}

// recordedGap is the distance between two bundles as history recorded it,
// before any playback scaling. The first bundle of a window has no predecessor
// and shows immediately.
func recordedGap(prevMs, ms int64) (d time.Duration) {
	if prevMs <= 0 || ms <= prevMs {
		return
	}
	d = time.Duration(ms-prevMs) * time.Millisecond
	return
}

// scale turns a recorded gap into the wall-clock wait for it at the current
// speed, clamped by MaxReplayGap.
//
// It reads the speed each time it is called rather than taking it as an
// argument, which is what lets a mid-wait change apply to the wait in progress.
func (inst *ReplaySampler) scale(gap time.Duration) (d time.Duration) {
	if gap <= 0 {
		return
	}
	d = gap
	if speed := inst.Speed(); speed > 0 {
		d = time.Duration(float64(d) / speed)
	}
	d = min(d, MaxReplayGap)
	return
}

// gate is what awaitTurn decided about the next bundle.
type gateE uint8

const (
	gateEmit gateE = iota
	gateStop
	gateReseek
)

// awaitTurn blocks until the next bundle should be folded: immediately while
// playing (after its pacing delay), on a Step while paused, and never while
// paused with no step pending.
//
// gap is the recorded distance to the previous bundle and since is when the
// wait began; the deadline is recomputed from them on every pass, so raising
// the speed during a long gap shortens the wait in progress instead of only
// affecting the next one. A step skips the wait entirely — the point of
// stepping is to move now.
func (inst *ReplaySampler) awaitTurn(ctx context.Context, gap time.Duration, since time.Time) (g gateE) {
	for {
		if ctx.Err() != nil {
			return gateStop
		}
		if inst.seekPending.Load() {
			return gateReseek
		}
		if inst.steps.Load() > 0 {
			if inst.steps.Add(-1) >= 0 {
				return gateEmit
			}
			inst.steps.Store(0) // lost a race with another consumer; re-check
			continue
		}
		if inst.paused.Load() {
			select {
			case <-ctx.Done():
				return gateStop
			case <-inst.wake:
			}
			continue
		}
		remaining := time.Until(since.Add(inst.scale(gap)))
		if remaining <= 0 {
			return gateEmit
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return gateStop
		case <-inst.wake:
			timer.Stop() // controls changed; recompute the deadline
		case <-timer.C:
			return gateEmit
		}
	}
}

// park holds at the end of the window until a Seek or the context ends.
func (inst *ReplaySampler) park(ctx context.Context) (ok bool) {
	for {
		if ctx.Err() != nil {
			return false
		}
		if inst.seekPending.Load() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-inst.wake:
		}
	}
}
