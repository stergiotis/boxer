package trailviewer

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// Replay steps the selection through the rows the table holds, in time
// order (ADR-0350 §SD13). The band's own transport is hidden: its steps
// are buckets, and a bucket of three rows would play as one. Each row
// visited lands through select_row, so the band's playhead follows and the
// trail logs the replay as the person's. No read is made for it: the rows
// the table holds are the rows replay visits.
type replay struct {
	playing bool
	// next is when playback moves on to the row after the selected one.
	next time.Time
}

// replayDwell is how long playback shows each row.
const replayDwell = 1500 * time.Millisecond

// replayKeys are the keys the replay row answers while it has focus: Space
// plays and pauses, Left and Right step.
var replayKeys = keycodes.MaskOf(keycodes.Space, keycodes.ArrowLeft, keycodes.ArrowRight)

// neighbourRow is the held row after (dir > 0) or before (dir < 0) the
// selected one in time order; without a selection, or with one the table
// does not hold, the oldest held row (dir > 0) or the newest (dir < 0).
// rows are newest first, so later in time is earlier in the slice.
func neighbourRow(rows []Row, selected uint64, has bool, dir int) (id uint64, ok bool) {
	n := len(rows)
	if n == 0 || dir == 0 {
		return
	}
	at := -1
	if has {
		for i := range rows {
			if rows[i].Id == selected {
				at = i
				break
			}
		}
	}
	switch {
	case at < 0 && dir > 0:
		at = n
	case at < 0:
		at = -1
	}
	at -= dir
	if at < 0 || at >= n {
		return
	}
	return rows[at].Id, true
}

// rowPlace is the selected row's place among the held rows in time order,
// one-based, and how many there are; zero when it is not held.
func rowPlace(rows []Row, selected uint64, has bool) (place, of int) {
	of = len(rows)
	if !has {
		return
	}
	for i := range rows {
		if rows[i].Id == selected {
			return of - i, of
		}
	}
	return
}

// stepRow selects the row after or before the selected one, in time.
func (inst *App) stepRow(dir int) {
	if id, ok := neighbourRow(inst.cache.rows, inst.selectionId(), inst.hasSelected, dir); ok {
		inst.selectGesture(id)
	}
}

// toggleReplay starts or stops playback. Starting with no row selected, or
// at the newest row, begins at the oldest; starting elsewhere shows the
// selected row for one dwell and moves on.
func (inst *App) toggleReplay(now time.Time) {
	if inst.replay.playing {
		inst.replay.playing = false
		return
	}
	if len(inst.cache.rows) == 0 {
		return
	}
	if _, ok := neighbourRow(inst.cache.rows, inst.selectionId(), inst.hasSelected, 1); !ok || !inst.hasSelected {
		inst.selectGesture(inst.cache.rows[len(inst.cache.rows)-1].Id)
	}
	inst.replay.playing, inst.replay.next = true, now.Add(replayDwell)
}

// tickReplay moves playback on when its dwell is over, and stops it at the
// newest row. A selected row a refill no longer holds is no selection, so
// replay goes on from the oldest held row.
func (inst *App) tickReplay(now time.Time) {
	if !inst.replay.playing || now.Before(inst.replay.next) {
		return
	}
	id, ok := neighbourRow(inst.cache.rows, inst.selectionId(), inst.hasSelected, 1)
	if !ok || !inst.hasSelected {
		inst.replay.playing = false
		return
	}
	inst.selectGesture(id)
	inst.replay.next = now.Add(replayDwell)
}

// renderReplay is the row above the band: Prev row, Play or Pause, Next row
// and the selected row's place among the held rows. Its keys land while
// the row has focus, which Play gives it.
func (inst *App) renderReplay(now time.Time) {
	rows := inst.cache.rows
	_, canPrev := neighbourRow(rows, inst.selectionId(), inst.hasSelected, -1)
	_, canNext := neighbourRow(rows, inst.selectionId(), inst.hasSelected, 1)
	kf := c.Frame(inst.ids.PrepareStr("replay-keys")).CaptureKeys(uint64(replayKeys))
	frameID := kf.Id()
	for range kf.KeepIter() {
		for range c.Horizontal().KeepIter() {
			if inst.replayButton("replay-prev", icons.PhCaretLeft+" Prev row", "The row before the selected one, in time · Left", canPrev) {
				inst.stepRow(-1)
			}
			play, tip := icons.PhPlay+" Play", fmt.Sprintf("Replay the window's rows in time order, one every %.1f s, stopping at the newest · Space", replayDwell.Seconds())
			if inst.replay.playing {
				play, tip = icons.PhPause+" Pause", "Pause the replay · Space"
			}
			if inst.replayButton("replay-play", play, tip, len(rows) > 0) {
				inst.toggleReplay(now)
				c.RequestFocus(frameID)
			}
			if inst.replayButton("replay-next", "Next row "+icons.PhCaretRight, "The row after the selected one, in time · Right", canNext) {
				inst.stepRow(1)
			}
			if place, of := rowPlace(rows, inst.selectionId(), inst.hasSelected); place > 0 {
				c.Label(fmt.Sprintf("row %d of %d, oldest first", place, of)).Send()
			}
		}
	}
	for _, k := range c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(frameID)) {
		switch k.Code {
		case keycodes.Space:
			inst.toggleReplay(now)
		case keycodes.ArrowLeft:
			inst.stepRow(-1)
		case keycodes.ArrowRight:
			inst.stepRow(1)
		}
	}
}

// replayButton is one control of the replay row, disabled in place when it
// does not apply so the row does not move under the reader.
func (inst *App) replayButton(key, label, tip string, enabled bool) (clicked bool) {
	for range c.HoverText(tip).KeepIter() {
		for range c.EnabledUi(enabled).KeepIter() {
			clicked = c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked()
		}
	}
	return clicked && enabled
}
