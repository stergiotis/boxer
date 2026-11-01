package windowhost

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The slow-window notice (ADR-0351 §SD6): a window whose p95 Frame stays at
// SlowFrame or above is reported once, and not again until it has stayed
// below for as long. The watch reads the frame-time ring the host keeps
// anyway, once a second.

// SlowFrame is where a window's Frame counts as slow: a p95 of half a 60 Hz
// frame leaves the other windows and the client's paint the other half. A
// signpost, not a budget any app is held to.
const SlowFrame = 8 * time.Millisecond

const (
	// slowFor is how long p95 stays at SlowFrame or above before it is
	// reported, and below before the window can be reported again.
	slowFor = 10 * time.Second
	// slowCheckEvery bounds how often the rings are read.
	slowCheckEvery = time.Second
	// slowMinFrames is how many frames the window must draw while slow: a
	// window that stopped drawing keeps its last quantiles, which say
	// nothing about now.
	slowMinFrames = 32
)

// SlowWindowsI is told when a window's Frame has been slow for slowFor, and
// when a window closes, so a notice about it can go. Called on the render
// goroutine, or on the goroutine that reaps a window; it must not block.
type SlowWindowsI interface {
	WindowSlow(key WindowKeyT, appId app.AppIdT, title string, p95 time.Duration)
	WindowGone(key WindowKeyT)
}

// SetSlowWindows installs the listener of slow windows; nil removes it.
func (inst *Inst) SetSlowWindows(l SlowWindowsI) {
	ft := &inst.frameTimes
	ft.mu.Lock()
	ft.slow = l
	ft.mu.Unlock()
}

// slowWatch is one window's state.
type slowWatch struct {
	// since is when p95 reached SlowFrame, framesAt the frame count then;
	// zero while below.
	since    time.Time
	framesAt uint64
	// below is when p95 fell below after a report.
	below    time.Time
	reported bool
}

// step advances the watch to p95 after frames frames at now, and says
// whether to report it.
func (inst *slowWatch) step(p95 time.Duration, frames uint64, now time.Time) (report bool) {
	if p95 >= SlowFrame {
		inst.below = time.Time{}
		if inst.since.IsZero() {
			inst.since, inst.framesAt = now, frames
		}
		if !inst.reported && now.Sub(inst.since) >= slowFor && frames-inst.framesAt >= slowMinFrames {
			inst.reported = true
			return true
		}
		return false
	}
	inst.since = time.Time{}
	if inst.reported {
		if inst.below.IsZero() {
			inst.below = now
		}
		if now.Sub(inst.below) >= slowFor {
			inst.reported, inst.below = false, time.Time{}
		}
	}
	return false
}

// slowReport is one window to tell the listener of.
type slowReport struct {
	key   WindowKeyT
	appId app.AppIdT
	title string
	p95   time.Duration
}

// checkSlow steps every window's watch, at most once per slowCheckEvery,
// and tells the listener of the windows that turned slow.
func (inst *frameTimes) checkSlow(now time.Time) {
	inst.mu.Lock()
	if inst.slow == nil || now.Sub(inst.slowChecked) < slowCheckEvery {
		inst.mu.Unlock()
		return
	}
	inst.slowChecked = now
	var reports []slowReport
	for key, r := range inst.windows {
		if r.n == 0 {
			continue
		}
		p95 := r.info(FrameScopeWindow, key).P95
		if r.slow.step(p95, r.frames, now) {
			reports = append(reports, slowReport{key: key, appId: r.appId, title: r.title, p95: p95})
		}
	}
	l := inst.slow
	inst.mu.Unlock()
	for _, rep := range reports {
		l.WindowSlow(rep.key, rep.appId, rep.title, rep.p95)
	}
}
