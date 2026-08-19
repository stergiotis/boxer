package imztop

import (
	"context"
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The transport's UI state is process-wide, because the session it drives is
// (ADR-0197 §SD5). Holding it per-window would let two windows disagree about
// the speed of one playback and fight over it every frame.
//
// No lock: every read and write below happens on the render thread, which the
// imzero2 API is single-threaded on by contract.
var replaySpeedIdx = 1 // 1x

// defaultReplaySpan is the range entering replay opens on.
//
// It used to be one of three choices behind a combo. The availability strip
// picks the range now (§SD9), against a picture of where the data is rather
// than by naming a duration blind, so all the combo did was choose where to
// start looking — which one value does.
const defaultReplaySpan = 10 * time.Minute

var replaySpeeds = []float64{0.5, 1, 2, 5, 10, 60}

func replaySpeedLabel(x float64) (s string) {
	if x < 1 {
		return fmt.Sprintf("%.1f×", x)
	}
	return fmt.Sprintf("%.0f×", x)
}

// defaultReplayWindow is the range the Replay button opens on: the span ending
// now, so entering replay shows the recent past rather than an empty window.
func defaultReplayWindow() (w sysmreplay.Window) {
	to := time.Now().UTC()
	w = sysmreplay.Window{From: to.Add(-defaultReplaySpan), To: to}
	return
}

// renderReplayEntry is the one control the top bar carries while replay is off.
// It stays a single button so the live layout is unchanged for anyone who never
// replays.
func (inst *App) renderReplayEntry() {
	clicked := c.Button(inst.ids.PrepareStr("topbar-replay"),
		c.Atoms().Text(icons.PhClockCounterClockwise+" Replay").Keep()).
		SendResp().HasPrimaryClicked()
	if clicked {
		StartReplay(context.Background(), defaultReplayWindow(), StoreSourceOptions{})
	}
}

// renderReplayBar draws the transport. It is a second row under the top bar,
// present only while a session is opening, on, or failed.
//
// Every control is a button or a combo of buttons, and every one reads its
// result from SendResp synchronously. That is deliberate: SendRespVal lands
// through the deferred sync path, so a slider bound to the speed would need
// either an unconditional per-frame push or a change-detector that does not
// work — neither worth it for a control with six useful settings.
func (inst *App) renderReplayBar(st ReplayStatus) {
	for range c.HorizontalTop().KeepIter() {
		switch st.State {
		case ReplayOpening:
			c.Spinner().Send()
			c.AddSpace(inst.spaceItems())
			c.Label("opening replay…").Send()
			c.Separator().Vertical().Send()
			if c.Button(inst.ids.PrepareStr("replay-cancel"), c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				_ = LeaveReplay()
			}
		case ReplayFailed:
			for rt := range c.RichTextLabelColored(colorHot, colorBgClear, icons.PhWarning+" replay unavailable") {
				rt.Strong()
			}
			c.AddSpace(inst.spaceItems())
			if st.Err != nil {
				c.Label(trimTo(st.Err.Error(), 140)).Send()
			}
			c.Separator().Vertical().Send()
			if c.Button(inst.ids.PrepareStr("replay-dismiss"), c.Atoms().Text("Dismiss").Keep()).
				SendResp().HasPrimaryClicked() {
				_ = LeaveReplay()
			}
		case ReplayOn:
			inst.renderReplayTransport(st)
		}
	}
}

// renderReplayTransport is the running-session control row.
func (inst *App) renderReplayTransport(st ReplayStatus) {
	session := ActiveReplay()
	if session == nil {
		// The session ended between the status read and here. Draw nothing
		// rather than a half-live transport; the next frame renders the
		// correct state.
		return
	}
	// Leaving comes first, and deliberately so: it is the way out of a mode
	// that can legitimately show nothing, and a control row is clipped from
	// the right on a narrow window. Trailing it put the escape hatch behind
	// the window edge exactly when the view gave the user most reason to want
	// it — found by driving the app, not by reading the code.
	if c.Button(inst.ids.PrepareStr("replay-golive"),
		c.Atoms().Text(icons.PhBroadcast+" Go live").Keep()).
		SendResp().HasPrimaryClicked() {
		_ = LeaveReplay()
		return
	}
	c.Separator().Vertical().Send()

	paused := session.IsPaused()

	playGlyph, playLabel := icons.PhPause, "Pause"
	if paused {
		playGlyph, playLabel = icons.PhPlay, "Play"
	}
	if c.Button(inst.ids.PrepareStr("replay-play"), c.Atoms().Text(playGlyph+" "+playLabel).Keep()).
		SendResp().HasPrimaryClicked() {
		session.Pause(!paused)
	}

	// Stepping is only meaningful while stopped; the button stays rendered
	// either way so the row does not reflow as playback toggles.
	stepped := c.Button(inst.ids.PrepareStr("replay-step"),
		c.Atoms().Text(icons.PhSkipForward+" Step").Keep()).
		SendResp().HasPrimaryClicked()
	if stepped && paused {
		session.Step(1)
	}
	c.Separator().Vertical().Send()

	// Speed.
	for range c.ComboBox(
		inst.ids.PrepareStr("replay-speed-cb"),
		c.WidgetText().Text("speed").Keep(),
		c.WidgetText().Text(replaySpeedLabel(replaySpeeds[replaySpeedIdx])).Keep(),
	).KeepIter() {
		for i, x := range replaySpeeds {
			sel := i == replaySpeedIdx
			if c.Button(inst.ids.PrepareSeq(uint64(0x900+i)), c.Atoms().Text(replaySpeedLabel(x)).Keep()).
				Selected(sel).FrameWhenInactive(!sel).Frame(true).
				SendResp().HasPrimaryClicked() {
				replaySpeedIdx = i
				session.SetSpeed(x)
			}
		}
	}
	c.Separator().Vertical().Send()

	// The window length combo is gone: the availability strip below picks the
	// range now (ADR-0197 §SD9), and it does so against a picture of where the
	// data is rather than by naming a duration blind. The jog stays — it is
	// still the fastest way to step through consecutive windows once you know
	// where to look.
	if c.Button(inst.ids.PrepareStr("replay-earlier"),
		c.Atoms().Text(icons.PhRewind+" earlier").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.jogReplay(session, -1)
	}
	if c.Button(inst.ids.PrepareStr("replay-later"),
		c.Atoms().Text(icons.PhFastForward+" later").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.jogReplay(session, +1)
	}
	c.Separator().Vertical().Send()

	// Where playback has got to, and over what.
	c.Label(availabilityWindowLabel(session.Window())).Send()
	c.AddSpace(inst.spaceItems())
	if at, ok := session.Position(); ok {
		c.Label(fmt.Sprintf("at %s", at.Local().Format("15:04:05"))).Send()
	} else {
		c.Label("at —").Send()
	}
}

// jogReplay moves the window one span earlier or later, clamped so "later"
// cannot run past now — there is no history in the future, and a window beyond
// it would replay as empty.
//
// The end is moved and the start derived from it, rather than both being
// shifted: that keeps the span exactly right under the clamp, and it is the
// only shape that behaves on an unbounded window, where there are no bounds to
// shift and the two ends would otherwise stay equal to each other.
func (inst *App) jogReplay(session *ReplaySampler, dir int) {
	cur := session.Window()
	span := defaultReplaySpan
	if s := cur.To.Sub(cur.From); s > 0 {
		span = s
	}
	to := cur.To
	if to.IsZero() {
		// An unbounded window has no edge to move from; anchor on now, which
		// is where a replay given no upper bound is effectively sitting.
		to = time.Now().UTC()
	}
	to = to.Add(time.Duration(dir) * span)
	if now := time.Now().UTC(); to.After(now) {
		to = now
	}
	session.SeekWindow(to.Add(-span), to)
}

// replayBanner is the mode indicator, the ADR-0020 "FROZEN" affordance extended
// to a third state (ADR-0197 §SD8): a replayed view is stale-but-plausible in
// exactly the way a frozen one is, and must not be mistaken for live.
func (inst *App) replayBanner(st ReplayStatus) {
	switch st.State {
	case ReplayOpening:
		for rt := range c.RichTextLabelColored(colorWarn, colorBgClear, "OPENING REPLAY") {
			rt.Strong()
		}
	case ReplayOn:
		if st.Empty {
			// Not an error: a host the tee never ran for simply has no
			// history, and saying so is the whole point of tracking Empty.
			for rt := range c.RichTextLabelColored(colorWarn, colorBgClear,
				fmt.Sprintf("REPLAY — nothing recorded for %s", st.Host)) {
				rt.Strong()
			}
			return
		}
		for rt := range c.RichTextLabelColored(colorWarn, colorBgClear,
			fmt.Sprintf("REPLAY — stored history for %s, not live", st.Host)) {
			rt.Strong()
		}
	case ReplayFailed:
		for rt := range c.RichTextLabelColored(colorHot, colorBgClear, "REPLAY UNAVAILABLE — showing live") {
			rt.Strong()
		}
	}
}

// notRecordedNote states that a panel is empty because the tee never stored its
// kind, rather than because the machine had nothing to report (ADR-0197 §SD8).
// An empty panel and an unrecorded one look identical, and only one of them is
// about the machine.
func (inst *App) notRecordedNote(what string) {
	for rt := range c.RichTextLabelColored(colorWarn, colorBgClear,
		fmt.Sprintf("%s not recorded — replay shows only what the tee stored", what)) {
		rt.Weak()
	}
}

// renderNoDataPanel explains an empty frame. There are three reasons for one
// and they call for different things from the reader, so they are not one
// message: a live imztop is waiting for a scraper, an opening replay is waiting
// on a database, and a replay of a host the tee never covered will wait forever
// and should say so.
func (inst *App) renderNoDataPanel() {
	switch inst.frameReplay.State {
	case ReplayOpening:
		c.Label("Opening replay…").Send()
	case ReplayOn:
		if inst.frameReplay.Empty {
			for rt := range c.RichTextLabelColored(colorWarn, colorBgClear,
				fmt.Sprintf("No stored history for %s in this window.", inst.frameReplay.Host)) {
				rt.Strong()
			}
			c.Label("Jog to an earlier window, or leave replay to go back to live data. " +
				"A host only has history where `sysmetricsd --tee` has run.").Send()
			return
		}
		c.Label("Replaying — waiting for the first stored sample…").Send()
	default:
		c.Label("Imztop: waiting for first sample…").Send()
	}
}
