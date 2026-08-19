package imztop

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
)

// ReplayStateE is where the process-wide replay session has got to.
type ReplayStateE uint8

const (
	// ReplayOff is live data: no session, no connection.
	ReplayOff ReplayStateE = iota
	// ReplayOpening means a session is being opened on its own goroutine.
	// Opening dials ClickHouse, so it is never instant and never on the render
	// thread.
	ReplayOpening
	// ReplayOn means the session is playing, paused, or parked at its end.
	ReplayOn
	// ReplayFailed means opening did not succeed; the status carries why.
	ReplayFailed
)

func (inst ReplayStateE) String() (s string) {
	switch inst {
	case ReplayOpening:
		return "opening"
	case ReplayOn:
		return "on"
	case ReplayFailed:
		return "failed"
	default:
		return "off"
	}
}

// ReplayStatus is what a renderer needs to describe the session without
// touching it. It is a copy taken under the session lock, so it is safe to hold
// across a frame.
type ReplayStatus struct {
	State ReplayStateE
	// Err is set only in ReplayFailed.
	Err error
	// Host and Endpoint say whose history this is and where it came from. Set
	// from ReplayOn onwards.
	Host     string
	Endpoint string
	// Empty distinguishes the two ways a replay shows nothing: the window held
	// no bundles at all (this is true) versus playback not having reached the
	// first one yet (false). It is not an error — a host the tee never ran for
	// is simply a host with no history — and the UI has to say so rather than
	// leave the panels reading "waiting for first sample".
	Empty bool
}

// The replay session is process-wide, matching Freeze (ADR-0197 §SD5): one
// session serves every open imztop window, and entering replay puts them all on
// the same historical cursor.
//
// The source is held as a close function rather than as a *StoreSource so the
// session has no opinion about where bundles come from — which is what lets the
// lifecycle be tested without a database.
var (
	replayMu       sync.Mutex
	replayState    ReplayStateE
	replayErr      error
	replaySession  *ReplaySampler
	replaySource   *StoreSource
	replayHost     string
	replayEndpoint string
	replayClose    func()
)

// StartReplay opens a replay session in the background and returns immediately.
//
// It is the entry point a UI uses: opening dials ClickHouse and verifies the
// schema, which blocks for as long as the network takes, and the render thread
// cannot afford that. Progress is read with [CurrentReplayStatus]. Calling it
// while a session is opening or open is a no-op.
func StartReplay(ctx context.Context, w sysmreplay.Window, opts StoreSourceOptions) {
	if !beginOpening() {
		return
	}
	go func() {
		if err := enterReplay(ctx, w, opts); err != nil {
			log.Warn().Err(err).Msg("imztop: replay session could not open")
		}
	}()
}

// EnterReplay opens the session synchronously and makes it the active sampler.
//
// It blocks on a network round trip and must not be called from the render
// thread; [StartReplay] is the safe form. Exported for a caller that already
// owns a background goroutine.
func EnterReplay(ctx context.Context, w sysmreplay.Window, opts StoreSourceOptions) (err error) {
	if !beginOpening() {
		return
	}
	err = enterReplay(ctx, w, opts)
	return
}

// beginOpening claims the session for an open, reporting false when one is
// already opening or open.
func beginOpening() (claimed bool) {
	replayMu.Lock()
	defer replayMu.Unlock()
	if replayState == ReplayOpening || replayState == ReplayOn {
		return
	}
	replayState = ReplayOpening
	replayErr = nil
	claimed = true
	return
}

// enterReplay builds the source and the session, then installs them. Every
// failure it can hit means "there is nothing to replay", and lands the session
// in ReplayFailed with the reason.
func enterReplay(ctx context.Context, w sysmreplay.Window, opts StoreSourceOptions) (err error) {
	src, err := NewStoreSource(ctx, opts)
	if err != nil {
		failOpening(err)
		return
	}
	session, err := NewReplaySampler(ReplayOptions{
		Source: src,
		Window: w,
		Log:    opts.Log,
	})
	if err != nil {
		src.Close()
		failOpening(err)
		return
	}
	session.Start(ctx)
	if !installReplay(session, src, src.Host(), src.Endpoint(), src.Close) {
		// A LeaveReplay landed while this was opening. Honour it rather than
		// installing a session nobody asked for any more.
		_ = session.Close()
		src.Close()
	}
	return
}

// failOpening records why an open did not succeed, unless it was cancelled.
func failOpening(err error) {
	replayMu.Lock()
	defer replayMu.Unlock()
	if replayState != ReplayOpening {
		return // a LeaveReplay already took the session back to off
	}
	replayState = ReplayFailed
	replayErr = err
}

// installReplay makes a built session the active one, reporting false when the
// open was abandoned while it ran.
func installReplay(session *ReplaySampler, src *StoreSource, host, endpoint string, closeSrc func()) (installed bool) {
	replayMu.Lock()
	defer replayMu.Unlock()
	if replayState != ReplayOpening {
		return
	}
	replaySession = session
	replaySource = src
	replayHost = host
	replayEndpoint = endpoint
	replayClose = closeSrc
	replayState = ReplayOn
	installed = true
	return
}

// LeaveReplay ends the session and returns every window to live data. It is
// safe to call when no session is open, and safe to call while one is opening
// — the open then discards itself rather than installing.
func LeaveReplay() (err error) {
	replayMu.Lock()
	session, closeSrc := replaySession, replayClose
	replaySession, replaySource, replayClose = nil, nil, nil
	replayHost, replayEndpoint = "", ""
	replayState = ReplayOff
	replayErr = nil
	replayMu.Unlock()

	// Drop the availability the old session loaded, so a new one cannot
	// briefly draw the previous host's coverage as its own.
	resetCoverage()

	if session != nil {
		err = session.Close()
	}
	if closeSrc != nil {
		closeSrc()
	}
	return
}

// CurrentReplayStatus reports the session for a renderer. It never blocks on
// the session's goroutine.
func CurrentReplayStatus() (st ReplayStatus) {
	replayMu.Lock()
	st.State = replayState
	st.Err = replayErr
	st.Host = replayHost
	st.Endpoint = replayEndpoint
	session := replaySession
	replayMu.Unlock()

	// Nothing was stored for this host in this window: the transport reached
	// the end of it without ever folding a bundle.
	if session != nil && session.Exhausted() && session.Latest() == nil {
		st.Empty = true
	}
	return
}

// ActiveReplay returns the running session, or nil when replay is not on. A UI
// drives the transport through it.
func ActiveReplay() (session *ReplaySampler) {
	replayMu.Lock()
	if replayState == ReplayOn {
		session = replaySession
	}
	replayMu.Unlock()
	return
}

// ActiveReplaySource returns the running session's store source, or nil. The
// availability strip needs it to query coverage.
func ActiveReplaySource() (src *StoreSource) {
	replayMu.Lock()
	if replayState == ReplayOn {
		src = replaySource
	}
	replayMu.Unlock()
	return
}

// activeSampler returns the sampler the render path should draw: the replay
// session when one is on, the live singleton otherwise.
//
// While a session is opening or has failed, the live sampler stays on screen,
// so entering replay never blanks the window on a slow connection and a failed
// open degrades to live data plus a message rather than to nothing.
func activeSampler() (s SamplerI, err error) {
	if session := ActiveReplay(); session != nil {
		s = session
		return
	}
	return ensureSampler()
}
