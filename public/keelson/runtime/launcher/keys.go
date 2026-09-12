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
//
// # Why there is a second site anyway
//
// That reasoning holds for the typed path and says nothing about the pointer
// one. A click on a row takes focus off the field, so the field captures
// nothing afterwards and every key the launcher owns goes dead until someone
// clicks back into the box. The row list therefore carries the tree's shape as
// well — a capture-only Frame the click focuses (rows.go) — and the two sites
// share one handler. The gesture that opens the field's site is typing; the
// one that opens the list's is a click; only the focused one ever delivers.

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

// launcherListKeyMask is what the ROW LIST's capture Frame eats while it has
// focus, which is what clicking a row gives it (rows.go).
//
// The field's mask plus Space. Space is the one key the two sites must
// disagree about: in a text field it is a space — and a space separates the
// battery's tokens, so it cannot be taken away there — while on a focused
// list it is "open this", the gesture every file manager and app launcher
// binds. Having a second site at all is what keeps the keyboard alive after a
// click: capture is gated on the capturing widget having focus (ADR-0177
// §SD1), and a click on a row takes focus off the field, so before this the
// arrows went dead the moment someone used the mouse.
var launcherListKeyMask = launcherKeyMask | keycodes.MaskOf(keycodes.Space)

// keyPageStep is how far PageUp/PageDown move the cursor. A fixed step rather
// than a viewport-derived one: the launcher list is virtualised, so the number
// of visible rows is known only to the table's own visible-range report, one
// frame late — and a page that changes size as the pane resizes is worse than
// one that is simply "about a screen".
const keyPageStep = 10

// applyKeys consumes the launcher's captured keys and moves the cursor.
// Called after the rows are built, so the cursor moves within the list the
// user is actually looking at, and returns the row to open when Enter or
// Space landed.
//
// Two capture sites, one handler: the query field while someone is typing,
// and the row list's Frame once a click has focused it. Only the focused one
// delivers anything — capture is gated on focus — so which of the two answers
// says where the person is working, and the merge below is a formality rather
// than an arbitration. listId is zero on the first frame, before the list has
// been rendered once.
//
// rows is this frame's list; the cursor is an index into it. openIdx is -1
// when nothing was activated.
func (inst *Inst) applyKeys(rows []rowT, fieldId uint64, listId uint64) (openIdx int) {
	openIdx = -1
	sm := c.CurrentApplicationState.StateManager
	if idx := inst.handleKeys(rows, sm.GetCapturedKeys(widgethandle.Make(fieldId)), fieldId); idx >= 0 {
		openIdx = idx
	}
	if listId != 0 {
		if idx := inst.handleKeys(rows, sm.GetCapturedKeys(widgethandle.Make(listId)), listId); idx >= 0 {
			openIdx = idx
		}
	}
	return
}

// handleKeys turns one capture site's keys into cursor moves, and reports the
// row an activation key landed on. Split from [Inst.applyKeys] so the whole
// keyboard contract is a function of (rows, keys) — the capture read-back
// needs a live client, this does not.
//
// focusId is the site the keys came from, which Escape surrenders.
func (inst *Inst) handleKeys(rows []rowT, captured []c.CapturedKey, focusId uint64) (openIdx int) {
	openIdx = -1
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
		case keycodes.Enter, keycodes.Space:
			// Space reaches here only from the list site: the field's mask
			// leaves it out, so typing a query still types spaces.
			if idx := inst.cursor; idx >= 0 && idx < len(rows) && rows[idx].heading == "" {
				openIdx = idx
			}
		case keycodes.Escape:
			// Clear first, surrender second. A person who typed a query and
			// pressed Escape means "not that" far more often than "close the
			// launcher", and the two-step keeps the destructive reading one
			// press further away.
			//
			// Clearing the query moves the cursor too, but not here: a filter
			// change puts it on the first row wherever the change came from
			// (launcher.go, syncCursorToFilter).
			if inst.searchText != "" {
				inst.searchText = ""
				continue
			}
			// Giving the focus up takes both ops, and only because of where
			// focus lives. SurrenderFocus clears focus only when the id it
			// names is the one holding it, and it names the Focusable-Frame id
			// (ADR-0177 §SD7) — which the list site does hold and the field
			// does not: a TextEdit holds focus under its own widget id
			// (launcher.go, renderSearchBox). Taking that id and handing it
			// straight back leaves NOTHING focused either way, which is what
			// Escape means here — the next press reaches the window rather
			// than the field eating Escape forever.
			c.RequestFocus(focusId)
			c.SurrenderFocus(focusId)
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
