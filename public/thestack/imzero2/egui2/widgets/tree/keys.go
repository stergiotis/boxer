package tree

// keys.go is the keyboard half of the widget (ADR-0177 M3). The cursor, the
// reveal and the scroll that follows one already existed — ADR-0176's Keys
// refactor filed all three under the identity column and the renderer already
// consumes a pending reveal. What was missing was the input: something to move
// the cursor with.
//
// # Why the capture is on a wrapping Frame
//
// endETable has no capture method; a Frame does. Wrapping the table costs one
// widget and gets the mask, the focus registration and the r26 read-back for
// free.
//
// The Frame asks for `.CaptureKeys()` and NOT `.Focusable()`, which is the
// difference between a tree that works and one whose rows stop responding.
// `.Focusable()` registers a click-sensing rect AFTER the body, so it sits
// above every row and swallows the clicks selection is made of. Capture alone
// registers the id as focus-able without sensing clicks, so the rows keep
// theirs — and the tree takes focus by asking for it explicitly when a row is
// hit (see focusOnClick).

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// treeKeyMask is what a tree eats while focused. Escape and Tab are absent on
// purpose: Escape usually belongs to whatever the tree is inside (a dialog, a
// popup), and capturing Tab would make the tree a focus trap — ADR-0177 SD9
// wants a container to be one focus stop, not a room with no door.
var treeKeyMask = keycodes.MaskOf(
	keycodes.ArrowUp, keycodes.ArrowDown,
	keycodes.ArrowLeft, keycodes.ArrowRight,
	keycodes.Home, keycodes.End,
	keycodes.PageUp, keycodes.PageDown,
	keycodes.Enter, keycodes.Space,
)

// keysPageRows is how far PageUp / PageDown move when the visible row count is
// unknown. Used only on the first frame, before the etable has reported a
// window; after that the real count is used, so a page matches what the reader
// can see.
const keysPageRows = 10

// applyKeys turns last frame's captured keys into cursor and expansion moves.
// It runs AFTER State.Bind and after the flatten, because every move is
// expressed in ROW positions — "the next visible row" is a fact about the
// flattened sequence, not about the tree — and because writing to the State by
// index before the bind would file under the previous build's keys.
//
// Returns the node to activate (Enter / Space on the cursor), or -1.
func applyKeys(st *State, rows []Row, frameID uint64, visibleRows int) (activated int32) {
	activated = -1
	caps := c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(frameID))
	if len(caps) == 0 || len(rows) == 0 {
		return
	}

	page := visibleRows
	if page <= 1 {
		page = keysPageRows
	}

	// The cursor is resolved to a row ONCE per key, not once per call: an
	// ArrowDown that lands on a row is the starting point for the next
	// ArrowDown in the same batch. Key repeat delivers several per frame and
	// treating them as a batch of independent moves would collapse them into
	// one.
	for _, k := range caps {
		cur := st.Cursor()
		row := RowOf(rows, cur)
		if row < 0 {
			// No cursor yet, or it points at a node the flatten no longer
			// emits (an ancestor was collapsed). Either way the first key
			// starts at the top rather than doing nothing, which is what a
			// reader pressing ↓ on a fresh tree expects.
			row = -1
		}

		switch k.Code {
		case keycodes.ArrowDown:
			row = min(row+1, len(rows)-1)
		case keycodes.ArrowUp:
			if row < 0 {
				row = 0
			} else {
				row = max(row-1, 0)
			}
		case keycodes.Home:
			row = 0
		case keycodes.End:
			row = len(rows) - 1
		case keycodes.PageDown:
			row = min(max(row, 0)+page, len(rows)-1)
		case keycodes.PageUp:
			row = max(max(row, 0)-page, 0)
		case keycodes.ArrowRight:
			// Open, or descend into an already-open node. The two-step is what
			// every file manager does and it keeps → useful on a leaf's
			// parent: press once to open, again to walk in.
			if row >= 0 {
				r := rows[row]
				switch {
				case r.HasChildren && !st.IsExpanded(r.Node):
					st.SetExpanded(r.Node, true)
					return applyKeysDone(st, rows, row)
				case r.HasChildren:
					row = min(row+1, len(rows)-1)
				}
			}
		case keycodes.ArrowLeft:
			// Close, or climb to the parent. The mirror of ArrowRight, and the
			// reason a reader can leave a deep subtree without the mouse.
			if row >= 0 {
				r := rows[row]
				switch {
				case r.HasChildren && st.IsExpanded(r.Node):
					st.SetExpanded(r.Node, false)
					return applyKeysDone(st, rows, row)
				default:
					if p := parentRow(rows, row); p >= 0 {
						row = p
					}
				}
			}
		case keycodes.Enter, keycodes.Space:
			if row >= 0 {
				activated = rows[row].Node
			}
			continue
		default:
			continue
		}

		if row >= 0 && row < len(rows) {
			st.SetCursor(rows[row].Node)
			// Selection follows the cursor. A tree whose keyboard moved a
			// highlight that meant nothing would be a second, competing
			// notion of "current"; hosts read Selected, so the cursor has to
			// drive it or arrow keys change nothing the host can see.
			st.SelectOnly(rows[row].Node)
			// The reveal is what makes the scroll follow, through the path the
			// renderer already had. Asking for it every move is right: it is a
			// no-op when the row is on screen.
			st.Reveal(rows[row].Node)
		}
	}
	return
}

// applyKeysDone re-points the cursor after an expansion change, where the row
// sequence this call was given is already stale — the flatten that reflects the
// new expansion has not run yet. Only the cursor is set; the reveal would
// resolve against the old rows.
func applyKeysDone(st *State, rows []Row, row int) (activated int32) {
	if row >= 0 && row < len(rows) {
		st.SetCursor(rows[row].Node)
		st.SelectOnly(rows[row].Node)
	}
	return -1
}

// parentRow finds the row of the given row's parent — the nearest row above it
// at one less depth. Walking the flattened sequence rather than the Tree keeps
// this in the same coordinate system as every other move here, and a parent is
// always present above its child in a flatten that emitted the child at all.
func parentRow(rows []Row, row int) int {
	if row <= 0 || row >= len(rows) {
		return -1
	}
	d := rows[row].Depth
	for i := row - 1; i >= 0; i-- {
		if rows[i].Depth < d {
			return i
		}
	}
	return -1
}
