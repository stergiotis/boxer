package launcher

// The keyboard half of the launcher (ADR-0214 §SD9).
//
// # Why the capture is on the text field
//
// The shape a launcher needs is Spotlight's: the caret stays in the query
// box, ↑/↓ move a cursor through the results, Enter opens the one under it.
// Every part of that happens while the *field* holds focus, and ADR-0177 §SD1
// gates capture on the capturing widget having focus — so a wrapping Frame's
// .CaptureKeys(), which is how the tree and the file browser do it, captures
// nothing here. That is why §SD9 put the mask form on TextEdit: the widget
// that has focus has to be the widget that eats the key.
//
// The alternative was to let egui's own focus navigation move focus out of the
// field on ↓ and give the list its own capture. That is two focus stops for
// one gesture, it loses the caret, and typing after arrowing would go
// nowhere.

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// launcherKeyMask is what the query field eats while focused.
//
// Escape is in it, unlike the tree's mask: there Escape belongs to whatever
// the widget sits inside, here the launcher *is* what Escape should act on —
// it clears the query, and on an already-empty query it gives focus back so a
// second press reaches the window. Tab stays out, so the field is not a focus
// trap (ADR-0177 §SD9).
//
// Space stays out too, which the tree includes: in a list of rows Space is
// "activate", and in a text field it is a space.
var launcherKeyMask = keycodes.MaskOf(
	keycodes.ArrowUp, keycodes.ArrowDown,
	keycodes.Home, keycodes.End,
	keycodes.PageUp, keycodes.PageDown,
	keycodes.Enter, keycodes.Escape,
)

// keyPageStep is how far PageUp/PageDown move the cursor. A fixed step rather
// than a viewport-derived one: the launcher list is virtualised, so the number
// of visible rows is known only to the table's own visible-range report, one
// frame late — and a page that changes size as the pane resizes is worse than
// one that is simply "about a screen".
const keyPageStep = 10

// applyKeys consumes the query field's captured keys and moves the cursor.
// Called after the rows are built, so the cursor moves within the list the
// user is actually looking at, and returns the row to open when Enter landed.
//
// rows is this frame's list; the cursor is an index into it. openIdx is -1
// when nothing was activated.
func (inst *Inst) applyKeys(rows []rowT, fieldId uint64) (openIdx int) {
	openIdx = -1
	captured := c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(fieldId))
	if len(captured) == 0 {
		return
	}
	for _, k := range captured {
		switch k.Code {
		case keycodes.ArrowDown:
			inst.moveCursor(rows, 1)
		case keycodes.ArrowUp:
			inst.moveCursor(rows, -1)
		case keycodes.PageDown:
			inst.moveCursor(rows, keyPageStep)
		case keycodes.PageUp:
			inst.moveCursor(rows, -keyPageStep)
		case keycodes.Home:
			inst.cursor = firstAppRow(rows)
		case keycodes.End:
			inst.cursor = lastAppRow(rows)
		case keycodes.Enter:
			if idx := inst.cursor; idx >= 0 && idx < len(rows) && rows[idx].heading == "" {
				openIdx = idx
			}
		case keycodes.Escape:
			// Clear first, surrender second. A person who typed a query and
			// pressed Escape means "not that" far more often than "close the
			// launcher", and the two-step keeps the destructive reading one
			// press further away.
			if inst.searchText != "" {
				inst.searchText = ""
				inst.cursor = 0
				continue
			}
			c.SurrenderFocus(fieldId)
		}
	}
	return
}

// moveCursor steps the cursor by delta app rows, skipping headings and
// stopping at the ends rather than wrapping. Wrapping in a ranked list means
// ↓ at the last hit jumps to the best one, which reads as a glitch.
func (inst *Inst) moveCursor(rows []rowT, delta int) {
	if len(rows) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	remaining := delta
	if remaining < 0 {
		remaining = -remaining
	}
	pos := inst.cursor
	for remaining > 0 {
		next := pos + step
		// Walk past headings without spending a step on them: a section
		// header is not a place the cursor can rest.
		for next >= 0 && next < len(rows) && rows[next].heading != "" {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		pos = next
		remaining--
	}
	inst.cursor = pos
}

// lastAppRow is the index of the last app row, or 0 when there is none.
func lastAppRow(rows []rowT) (idx int) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].heading == "" {
			idx = i
			return
		}
	}
	return
}
