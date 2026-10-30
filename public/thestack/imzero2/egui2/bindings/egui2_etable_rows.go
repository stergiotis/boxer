package bindings

import "math"

// Which rows of an etable to emit cells for.
//
// egui_table reports the rows it drew (VisibleRange), one frame late, and a
// host that emits only those keeps its per-frame cost to a screenful whatever
// the table's length. Before the first report there is no window to go by, and
// that is not only the first frame a table is on screen: a table that has
// never reached the screen — in a dock tab never raised, whose buffer the host
// drops uninterpreted — reports nothing on every frame it stays hidden. Emitting
// every row in that state builds the whole table each frame for a host that
// discards it, so the fallback is one screenful instead. A report, once made,
// is kept, so a table hidden after it has been seen goes on emitting its last
// window.

// etUnreportedPaneHeight is the height EtScreenfulRows assumes for a table
// with no MaxHeight (a FillPane table, or one left to the auto-fit cap): a
// tall display's, so the first frame on screen is covered whatever pane it
// lands in.
const etUnreportedPaneHeight = 2160

// EtScreenfulRows is how many rows of rowHeight fill a table of maxHeight,
// with one more for a row cut by the bottom edge. A non-positive maxHeight
// takes etUnreportedPaneHeight.
func EtScreenfulRows(maxHeight, rowHeight float32) (rows uint64) {
	if maxHeight <= 0 || math.IsNaN(float64(maxHeight)) {
		maxHeight = etUnreportedPaneHeight
	}
	if rowHeight <= 0 || math.IsNaN(float64(rowHeight)) {
		rowHeight = 1
	}
	rows = uint64(math.Ceil(float64(maxHeight/rowHeight))) + 1
	return
}

// EtFallbackRows is the window [lo, hi) of an n-row table to emit while it has
// reported no visible range: the first screenful rows, or, when the table is
// asked to scroll to anchor this frame, the rows a screenful either side of
// it — wide enough for any alignment the scroll lands on. A negative anchor
// is none.
func EtFallbackRows(n, screenful uint64, anchor int64) (lo, hi uint64) {
	if n == 0 {
		return
	}
	if anchor < 0 {
		hi = min(screenful, n)
		return
	}
	a := min(uint64(anchor), n-1)
	if a+1 > screenful {
		lo = a + 1 - screenful
	}
	hi = min(a+screenful, n)
	return
}

// VisibleRows is the window [lo, hi) of an n-row table to emit cells for: the
// rows egui_table reported drawing last frame, clamped to n — the table may
// have shrunk since — or, while it has reported none, EtFallbackRows. Pass
// screenful from EtScreenfulRows over the height the table is given, and as
// anchor the row a ScrollToRow targets this frame, or -1.
func (inst EndETableFluid) VisibleRows(n, screenful uint64, anchor int64) (lo, hi uint64) {
	rb, re, _, _, _, ok := inst.VisibleRange()
	if !ok {
		return EtFallbackRows(n, screenful, anchor)
	}
	hi = min(re, n)
	lo = min(rb, hi)
	return
}
