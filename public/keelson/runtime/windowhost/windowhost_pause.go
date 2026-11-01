package windowhost

import (
	"context"
	"fmt"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Pausing a window (ADR-0354). The host stops calling a paused window's
// Frame and draws an empty state in its place; an app implementing
// app.PausableAppI is told, so it can stop its own work; the window's
// operations engine refuses commands. Pause and Resume are requests, taken
// under the lock from any goroutine and applied on the render goroutine at
// the start of the next frame, so the app hears of them under its own
// labels and between its Frames.

// Where the person paused or resumed a window from: the reason on the
// lifecycle row and the audit event.
const (
	PauseFromWindow     = "window"
	PauseFromRunningTab = "running-tab"
	PauseFromSlowNotice = "slow-window-notice"
)

// pausedChromeGuess is a first guess at the height above and around a
// window's body: the title bar and the host's row. renderPaused corrects it
// from the height the window comes out at.
const pausedChromeGuess = 90

// pausedReason is what a command to a paused window is refused with.
const pausedReason = "the person paused this window; it takes no commands until they resume it"

// PauseEvent is one pause or resume, for the host's audit (ADR-0354 §SD5).
type PauseEvent struct {
	Key    WindowKeyT
	AppId  app.AppIdT
	Paused bool
	// From is where the person did it, one of the PauseFrom constants.
	From string
	At   time.Time
	// For is how long the window was paused, on a resume.
	For time.Duration
}

// SetPauseAudit installs the listener told of every pause and resume, on
// the render goroutine; it must not block.
func (inst *Inst) SetPauseAudit(f func(PauseEvent)) {
	inst.mu.Lock()
	inst.pauseAudit = f
	inst.mu.Unlock()
}

// Pause asks for the window to be paused at the next frame; from says
// where the person asked. It reports whether the window is open.
func (inst *Inst) Pause(key WindowKeyT, from string) (ok bool) {
	return inst.requestPause(key, 1, from)
}

// Resume asks for the window to be resumed at the next frame.
func (inst *Inst) Resume(key WindowKeyT, from string) (ok bool) {
	return inst.requestPause(key, -1, from)
}

func (inst *Inst) requestPause(key WindowKeyT, req int8, from string) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, w := range inst.windows {
		if w.key == key && !w.closeReq {
			w.pauseReq, w.pauseFrom = req, from
			return true
		}
	}
	return false
}

// PausedSince says since when the window is paused; zero while it runs.
func (inst *Inst) PausedSince(key WindowKeyT) (since time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, w := range inst.windows {
		if w.key == key {
			return w.pausedSince
		}
	}
	return
}

// applyPauses applies the pending requests of the windows drawn this frame.
// Render goroutine.
func (inst *Inst) applyPauses(windows []*window, now time.Time) {
	for _, w := range windows {
		inst.mu.Lock()
		req, from := w.pauseReq, w.pauseFrom
		w.pauseReq, w.pauseFrom = 0, ""
		inst.mu.Unlock()
		if req != 0 {
			inst.applyPause(w, req > 0, from, now)
		}
	}
}

// applyPause takes the window through one edge; a request for the state it
// is in does nothing.
func (inst *Inst) applyPause(w *window, pause bool, from string, now time.Time) {
	inst.mu.Lock()
	if pause == !w.pausedSince.IsZero() {
		inst.mu.Unlock()
		return
	}
	since := w.pausedSince
	if pause {
		w.pausedSince = now
		w.pauseH = w.geom.Rect.H()
		w.pauseMinH = max(0, w.pauseH-pausedChromeGuess)
	} else {
		w.pausedSince = time.Time{}
	}
	tell, paused := inst.instancePausedLocked(w)
	facts, runId, audit := inst.facts, inst.runId, inst.pauseAudit
	inst.mu.Unlock()

	if eng := w.ops.Load(); eng != nil {
		reason := ""
		if pause {
			reason = pausedReason
		}
		eng.SetPaused(reason)
	}
	// The window drew no frames while paused; its quantiles say nothing
	// about the time since.
	inst.frameTimes.resetSlow(w.key)
	if p, ok := w.appInst.(app.PausableAppI); ok && tell {
		w.setLabels()
		p.SetPaused(paused)
		pprof.SetGoroutineLabels(context.Background())
	}

	ev := PauseEvent{Key: w.key, AppId: w.manifest.Id, Paused: pause, From: from, At: now}
	phase, verb := factsstore.AppLifecyclePhasePaused, "paused"
	if !pause {
		ev.For = now.Sub(since)
		phase, verb = factsstore.AppLifecyclePhaseResumed, "resumed"
	}
	log := inst.logger.Info().Str("id", string(w.manifest.Id)).Uint64("windowKey", uint64(w.key)).Str("from", from)
	if !pause {
		log = log.Dur("pausedFor", ev.For)
	}
	log.Msg("windowhost: window " + verb)
	if facts != nil {
		if _, err := facts.WriteAppLifecycle(factsstore.AppLifecycleRow{RunId: runId, AppId: w.manifest.Id, TileKey: uint64(w.key),
			Phase: phase, StopReason: from, Ts: now}); err != nil {
			inst.logger.Warn().Err(err).Uint64("windowKey", uint64(w.key)).Msg("windowhost: write app-lifecycle " + verb + " failed")
		}
	}
	if audit != nil {
		audit(ev)
	}
}

// instancePausedLocked says whether the instance under w must hear of a
// change, and what: it is paused while every open window over it is, and
// hears of it only once mounted. Under inst.mu.
func (inst *Inst) instancePausedLocked(w *window) (tell bool, paused bool) {
	ms := w.mount
	if ms == nil || !ms.mounted {
		return
	}
	paused = true
	for _, cand := range inst.windows {
		if cand.mount == ms && !cand.closeReq && cand.pausedSince.IsZero() {
			paused = false
			break
		}
	}
	if paused != ms.toldPaused {
		ms.toldPaused, tell = paused, true
	}
	return
}

// renderPaused is a paused window's body: since when, whether its app stops
// its background work, and Resume.
func (inst *Inst) renderPaused(w *window, ids *c.WidgetIdStack, now time.Time) {
	// The window keeps the height it had: egui sizes a window's height to
	// its content, so the body claims what the app's did, corrected up by
	// whatever the last frame came out short.
	if h := w.geom.Rect.H(); w.geom.Shown && h > 0 && h < w.pauseH {
		w.pauseMinH += w.pauseH - h
	}
	c.UiSetMinHeight(w.pauseMinH)
	c.AddSpace(12)
	for rt := range c.RichTextLabel(icons.PhPause + " Paused " + pausedFor(now.Sub(w.pausedSince))) {
		rt.Heading()
	}
	c.AddSpace(6)
	note := "The app keeps its state. Its background work continues: the app does not stop it while paused."
	if _, ok := w.appInst.(app.PausableAppI); ok {
		note = "The app keeps its state and stops its background work until it is resumed."
	}
	for rt := range c.RichTextLabel(note + " Commands to it are refused meanwhile.") {
		rt.Weak()
	}
	c.AddSpace(6)
	if c.Button(ids.PrepareStr("windowhost-resume-"+strconv.FormatUint(uint64(w.key), 10)),
		c.Atoms().Text(icons.PhPlay+" Resume").Keep()).SendResp().HasPrimaryClicked() {
		inst.Resume(w.key, PauseFromWindow)
	}
}

// renderPauseButton is Pause, or Resume while paused, in the row the host
// draws above every window's body (ADR-0354 §SD4).
func (inst *Inst) renderPauseButton(w *window, ids *c.WidgetIdStack) {
	id := ids.PrepareStr("windowhost-pause-" + strconv.FormatUint(uint64(w.key), 10))
	if w.pausedSince.IsZero() {
		if c.Button(id, c.Atoms().Text(icons.PhPause+" Pause").Keep()).SendResp().HasPrimaryClicked() {
			inst.Pause(w.key, PauseFromWindow)
		}
		return
	}
	if c.Button(id, c.Atoms().Text(icons.PhPlay+" Resume").Keep()).SendResp().HasPrimaryClicked() {
		inst.Resume(w.key, PauseFromWindow)
	}
}

// pausedFor says how long, to the second under a minute and to the minute
// after.
func pausedFor(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("for %d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("for %d min", int(d.Minutes()))
	}
	return fmt.Sprintf("for %d h %d min", int(d.Hours()), int(d.Minutes())%60)
}
