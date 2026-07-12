package kanban

import "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"

// Column is one lane on the board. Columns render left-to-right in the order
// they appear in [Model.Columns]. ID must be unique and stable (it keys card
// membership and the widget's per-column id scope); Title is the header label.
type Column struct {
	ID    uint64
	Title string
}

// Card is one item on the board. ColumnID names the lane it currently sits in
// (matching a [Column.ID]); order within a lane is the order cards appear in
// [Model.Cards]. A card with ParentID != 0 is a sub-item of that (top-level)
// card and is scheduled independently — see the package doc. Nesting is one
// level only: a child's ParentID must reference a top-level card, never another
// child.
type Card struct {
	ID       uint64
	ColumnID uint64
	ParentID uint64
	Title    string
	Subtitle string
	// Accent tints the card's title bullet and its selected stroke. The
	// zero-value Color (kind none) falls back to the neutral theme accent.
	Accent color.Color
}

// Move records a card that changed lane (or order) this frame. The widget
// applies the change to the [Model] immediately — so the card relocates on the
// same frame — and appends the event here for the host to persist; drain it with
// [Model.DrainMoves]. A within-column reorder reports FromColumn == ToColumn.
type Move struct {
	CardID     uint64
	FromColumn uint64
	ToColumn   uint64
}

// Model is the board state. Columns and Cards are owned by the host, which
// builds them and reads them back after a move; the widget additionally holds
// the transient selection and the pending-move queue. Render mutates only a
// card's ColumnID and its position in Cards; it never touches Columns.
type Model struct {
	Columns []Column
	Cards   []Card

	sel   uint64 // selected card id; 0 = none
	moves []Move
}

// NewModel binds a board. The slices are retained (not copied); the host may
// keep references to mutate card contents (titles, accents) between frames, but
// should route lane/order changes through the widget so Move events stay
// faithful.
func NewModel(columns []Column, cards []Card) *Model {
	return &Model{Columns: columns, Cards: cards}
}

// DrainMoves returns the moves applied since the last call and empties the
// queue. Call once per frame after [Render] to persist lane changes; ignore the
// result to discard them.
func (m *Model) DrainMoves() (out []Move) {
	out = m.moves
	m.moves = nil
	return
}

// Selected returns the id of the selected card, or 0 for none.
func (m *Model) Selected() uint64 { return m.sel }

// columnIndex returns the position of column cid in Columns, or -1.
func (m *Model) columnIndex(cid uint64) int {
	for i := range m.Columns {
		if m.Columns[i].ID == cid {
			return i
		}
	}
	return -1
}

// cardIndicesIn returns the indices into Cards of the cards in column cid, in
// render (slice) order.
func (m *Model) cardIndicesIn(cid uint64) (idxs []int) {
	for i := range m.Cards {
		if m.Cards[i].ColumnID == cid {
			idxs = append(idxs, i)
		}
	}
	return
}

// cardIndex returns the position of the card with id in Cards, or -1.
func (m *Model) cardIndex(id uint64) int {
	for i := range m.Cards {
		if m.Cards[i].ID == id {
			return i
		}
	}
	return -1
}

// cardByID returns a pointer to the card with id, or nil.
func (m *Model) cardByID(id uint64) *Card {
	if i := m.cardIndex(id); i >= 0 {
		return &m.Cards[i]
	}
	return nil
}

// childCount counts the sub-items of the card with id parent.
func (m *Model) childCount(parent uint64) (n int) {
	for i := range m.Cards {
		if m.Cards[i].ParentID == parent {
			n++
		}
	}
	return
}

// shiftColumn moves the card at idx to the column dir steps away (-1 left,
// +1 right) in Columns order, landing at the bottom of the destination lane. A
// no-op at a board edge or on a bad index. Records a [Move].
func (m *Model) shiftColumn(idx, dir int) {
	if idx < 0 || idx >= len(m.Cards) {
		return
	}
	from := m.Cards[idx].ColumnID
	ci := m.columnIndex(from)
	if ci < 0 {
		return
	}
	tj := ci + dir
	if tj < 0 || tj >= len(m.Columns) {
		return
	}
	to := m.Columns[tj].ID

	card := m.Cards[idx]
	card.ColumnID = to
	m.Cards = append(m.Cards[:idx], m.Cards[idx+1:]...) // remove idx

	insert := len(m.Cards) // append past the end of the slice by default
	for i := len(m.Cards) - 1; i >= 0; i-- {
		if m.Cards[i].ColumnID == to {
			insert = i + 1 // just after the last card already in the lane
			break
		}
	}
	m.Cards = append(m.Cards, Card{})          // grow by one
	copy(m.Cards[insert+1:], m.Cards[insert:]) // shift the tail right
	m.Cards[insert] = card

	m.moves = append(m.moves, Move{CardID: card.ID, FromColumn: from, ToColumn: to})
}

// reorderWithin swaps the card at idx with its nearest same-lane neighbour dir
// steps away in slice order (-1 up, +1 down). A no-op at a lane end or on a bad
// index. Records a same-lane [Move].
func (m *Model) reorderWithin(idx, dir int) {
	if idx < 0 || idx >= len(m.Cards) {
		return
	}
	cid := m.Cards[idx].ColumnID
	j := -1
	if dir < 0 {
		for i := idx - 1; i >= 0; i-- {
			if m.Cards[i].ColumnID == cid {
				j = i
				break
			}
		}
	} else {
		for i := idx + 1; i < len(m.Cards); i++ {
			if m.Cards[i].ColumnID == cid {
				j = i
				break
			}
		}
	}
	if j < 0 {
		return
	}
	m.Cards[idx], m.Cards[j] = m.Cards[j], m.Cards[idx]
	m.moves = append(m.moves, Move{CardID: m.Cards[j].ID, FromColumn: cid, ToColumn: cid})
}
