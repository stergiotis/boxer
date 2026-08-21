package fsbrowser

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// listKeyMask is what the list eats while focused. Escape and Tab stay with
// the container (ADR-0177 SD9); Backspace is the one key a file manager adds
// to a tree's vocabulary — up one directory.
var listKeyMask = keycodes.MaskOf(
	keycodes.ArrowUp, keycodes.ArrowDown,
	keycodes.Home, keycodes.End,
	keycodes.PageUp, keycodes.PageDown,
	keycodes.Enter, keycodes.Space,
	keycodes.Backspace,
)

// keysPageRows is the page size before the table has reported a window.
const keysPageRows = 10

// keyIntent is what last frame's keys ask of the list: a cursor move (row
// index into the view), an activation of the cursor row, or going up.
type keyIntent struct {
	row      int
	activate bool
	up       bool
	moved    bool
}

// applyKeys turns the keys captured last frame into one intent over the
// current view. The cursor is resolved once per key, so key repeat walks
// several rows per frame rather than collapsing into one move.
func applyKeys(st *State, rows []Entry, frameID uint64, visibleRows int) (in keyIntent) {
	in.row = -1
	caps := c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(frameID))
	if len(caps) == 0 {
		return
	}
	page := visibleRows
	if page <= 1 {
		page = keysPageRows
	}
	row := rowOfPath(rows, st.cursor)
	for _, k := range caps {
		switch k.Code {
		case keycodes.ArrowDown:
			row = min(row+1, len(rows)-1)
			in.moved = true
		case keycodes.ArrowUp:
			if row < 0 {
				row = 0
			} else {
				row = max(row-1, 0)
			}
			in.moved = true
		case keycodes.Home:
			row = 0
			in.moved = true
		case keycodes.End:
			row = len(rows) - 1
			in.moved = true
		case keycodes.PageDown:
			row = min(max(row, 0)+page, len(rows)-1)
			in.moved = true
		case keycodes.PageUp:
			row = max(max(row, 0)-page, 0)
			in.moved = true
		case keycodes.Enter, keycodes.Space:
			if row >= 0 {
				in.activate = true
			}
		case keycodes.Backspace:
			in.up = true
		}
	}
	if in.moved && len(rows) > 0 {
		in.row = max(0, min(row, len(rows)-1))
	}
	return
}

func rowOfPath(rows []Entry, p string) int {
	if p == "" {
		return -1
	}
	for i := range rows {
		if rows[i].Path == p {
			return i
		}
	}
	return -1
}
