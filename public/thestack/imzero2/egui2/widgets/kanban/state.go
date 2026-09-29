package kanban

// State is the host-owned UI state of one board (ADR-0267 W9): the selected
// card, an in-progress drag, and the rect-probe slots the drag hit-test reads.
// Its zero value is usable. Keep one per board across frames; a fresh State
// every frame would drop the selection and any drag.
type State struct {
	sel      uint64     // selected card id; 0 = none
	drag     *dragState // non-nil while a card is being dragged
	dragStop bool       // the dragged card reported drag-stopped this frame
	clicked  uint64     // card clicked this frame; scratch for Result

	// cardSeq / laneSeq are the CaptureUiRect slots of this frame's cards
	// (by slice index) and lanes (by column index), derived under each
	// card's and lane's own id scope so two boards never share one (W7).
	cardSeq []uint64
	laneSeq []uint64
}

// Selected returns the id of the selected card, or 0 for none.
func (st *State) Selected() uint64 { return st.sel }

// SetSelected selects a card by id (0 = none), the write side of
// [State.Selected]. It is for hosts whose selection is owned elsewhere — a
// board fed by a shared cursor follows it here before [Render], and reads
// back the user's own click from [Result.Clicked] after. An id no card
// carries selects nothing, which is what a cursor pointing outside this board
// should do.
func (st *State) SetSelected(id uint64) { st.sel = id }

func (st *State) sizeSeqs(columns, cards int) {
	if cap(st.laneSeq) < columns {
		st.laneSeq = make([]uint64, columns)
	}
	st.laneSeq = st.laneSeq[:columns]
	if cap(st.cardSeq) < cards {
		st.cardSeq = make([]uint64, cards)
	}
	st.cardSeq = st.cardSeq[:cards]
}
