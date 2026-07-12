package kanban

import "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"

// Column is one lane on the board. Columns render left-to-right in the order
// they appear in [Model.Columns]. ID must be unique and stable (it keys card
// membership and the widget's per-column id scope); Title is the header label.
type Column struct {
	ID    uint64
	Title string
	// IsDone marks a terminal column: a child sitting in a done column counts
	// toward its parent's rollup (◱ k/n). If no column sets it, the last column
	// is treated as done.
	IsDone bool
}

// GroupModeE selects how the board arranges cards ([Input.Group]).
type GroupModeE uint8

const (
	// GroupNone lays every card out flat in its column (children included, with
	// a parent-link trailer).
	GroupNone GroupModeE = iota
	// GroupByParent stacks a swimlane per parent — its children in the columns,
	// the parent as the lane header with a rollup — plus a trailing Standalone
	// lane for childless top-level cards.
	GroupByParent
	// GroupByField stacks a swimlane per distinct value of a caller-supplied key
	// ([Input.GroupField]) — e.g. owner, priority, label. Every card (parent or
	// child) sits in its value's lane; an empty key becomes an Unassigned lane.
	GroupByField
)

// DotKind is one entry in a board's dot legend ([Model.DotLegend]): a small
// coloured indicator a card can carry ([Card.Dots]), plus the label and hover
// detail [RenderLegend] shows for it. ID must be unique and stable (it is what
// a [Card.Dots] entry references); Label is the always-visible legend caption;
// Tooltip is the hover detail text, or "" for none.
type DotKind struct {
	ID      uint64
	Color   color.Color
	Label   string
	Tooltip string
}

// DotTally is one [Card.Dots] entry: Count small dots of DotKind ID's colour,
// packed with no gap — a tally mark, not a single presence flag. Count <= 0
// is silently skipped at render.
type DotTally struct {
	ID    uint64
	Count int
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
	// Dots names up to 3 [DotTally] entries (from [Model.DotLegend]) painted
	// as a small packed run of coloured tally dots along the card's bottom
	// edge — a compact stand-in for labels or flags, explained board-wide by
	// [RenderLegend] rather than repeating a tooltip on every card. Entries
	// past the third, and any id absent from the board's DotLegend, are
	// silently skipped at render.
	Dots []DotTally
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

// dragState tracks an in-progress card drag (widget-owned). It is created on a
// card frame's drag-start and cleared on drop or cancel; the drop target is
// recomputed every frame from the pointer and the previous frame's captured
// rects.
type dragState struct {
	cardID uint64
	title  string
	accent color.Color
	// grabDX/grabDY is the pointer's offset inside the card at grab time, and
	// w/h the card size — so the floating ghost tracks the pointer without
	// jumping to a corner.
	grabDX, grabDY float32
	w, h           float32
	// Recomputed each frame while dragging.
	dropColumn uint64
	dropIndex  int
	dropOK     bool
}

// Model is the board state. Columns and Cards are owned by the host, which
// builds them and reads them back after a move; the widget additionally holds
// the transient selection, the pending-move queue, and any in-progress drag.
// Render mutates only a card's ColumnID and its position in Cards; it never
// touches Columns.
type Model struct {
	Columns []Column
	Cards   []Card
	// DotLegend is the board's dot vocabulary — [Card.Dots] entries reference
	// it by [DotKind.ID]. Optional: nil means no card carries a dot. Render it
	// with [RenderLegend], wherever the host wants the legend placed; it is
	// not drawn automatically by [Render].
	DotLegend []DotKind

	sel      uint64 // selected card id; 0 = none
	moves    []Move
	drag     *dragState // non-nil while a card is being dragged
	dragStop bool       // the dragged card reported drag-stopped this frame
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

// dotKind looks up a [DotKind] by id in the board's DotLegend, for rendering a
// [Card.Dots] entry. ok is false for an id absent from DotLegend (e.g. a stale
// reference after the host edited its legend); the caller skips it.
func (m *Model) dotKind(id uint64) (dk DotKind, ok bool) {
	for i := range m.DotLegend {
		if m.DotLegend[i].ID == id {
			return m.DotLegend[i], true
		}
	}
	return DotKind{}, false
}

// columnTitle returns the title of the column with id cid, or "".
func (m *Model) columnTitle(cid uint64) string {
	for i := range m.Columns {
		if m.Columns[i].ID == cid {
			return m.Columns[i].Title
		}
	}
	return ""
}

// isDoneColumn reports whether cid is a terminal (done) column: one flagged
// [Column.IsDone], or — when none are flagged — the last column.
func (m *Model) isDoneColumn(cid uint64) bool {
	last := uint64(0)
	anyFlagged := false
	for i := range m.Columns {
		if m.Columns[i].IsDone {
			anyFlagged = true
			if m.Columns[i].ID == cid {
				return true
			}
		}
		last = m.Columns[i].ID
	}
	return !anyFlagged && cid == last && len(m.Columns) > 0
}

// rollup returns (done, total) over the children of parentID — the numbers the
// ◱ k/n pill shows. done counts children sitting in a done column.
func (m *Model) rollup(parentID uint64) (done, total int) {
	return m.rollupOfIdxs(m.childIndicesOf(parentID))
}

// rollupOfIdxs returns (done, total) over an arbitrary set of card indices —
// the general rollup a swimlane header shows (children for a parent lane, the
// lane's cards for a field lane).
func (m *Model) rollupOfIdxs(idxs []int) (done, total int) {
	for _, i := range idxs {
		if i < 0 || i >= len(m.Cards) {
			continue
		}
		total++
		if m.isDoneColumn(m.Cards[i].ColumnID) {
			done++
		}
	}
	return
}

// fieldLane is one attribute-grouped swimlane: its key + display label and the
// slice indices of the cards that fall in it.
type fieldLane struct {
	key   string
	label string
	idxs  []int
}

// fieldLanes buckets every card by groupField (which maps a card to a key and a
// display label), one lane per distinct key in first-appearance order.
func (m *Model) fieldLanes(groupField func(*Card) (key, label string)) (lanes []fieldLane) {
	byKey := make(map[string]int, len(m.Cards))
	for i := range m.Cards {
		k, lbl := groupField(&m.Cards[i])
		li, ok := byKey[k]
		if !ok {
			li = len(lanes)
			byKey[k] = li
			lanes = append(lanes, fieldLane{key: k, label: lbl})
		}
		lanes[li].idxs = append(lanes[li].idxs, i)
	}
	return
}

// childIndicesOf returns the slice indices of the children of parentID, in order.
func (m *Model) childIndicesOf(parentID uint64) (idxs []int) {
	for i := range m.Cards {
		if m.Cards[i].ParentID == parentID {
			idxs = append(idxs, i)
		}
	}
	return
}

// topLevelParentIndices returns the slice indices of top-level cards that have
// at least one child, in order — one swimlane each under GroupByParent.
func (m *Model) topLevelParentIndices() (idxs []int) {
	for i := range m.Cards {
		if m.Cards[i].ParentID == 0 && m.childCount(m.Cards[i].ID) > 0 {
			idxs = append(idxs, i)
		}
	}
	return
}

// standaloneIndices returns the slice indices of top-level cards with no
// children, in order — the shared Standalone swimlane under GroupByParent.
func (m *Model) standaloneIndices() (idxs []int) {
	for i := range m.Cards {
		if m.Cards[i].ParentID == 0 && m.childCount(m.Cards[i].ID) == 0 {
			idxs = append(idxs, i)
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

// moveTo relocates the card cardID into column toColumn at insertion position
// toIndex among that column's other cards (clamped to [0, len]). Records a
// [Move]. This is the drag-drop mutator; shiftColumn / reorderWithin back the
// button controls. toIndex counts the destination column's cards excluding the
// dragged one, so it composes directly with the drag hit-test.
func (m *Model) moveTo(cardID, toColumn uint64, toIndex int) {
	src := m.cardIndex(cardID)
	if src < 0 || m.columnIndex(toColumn) < 0 {
		return
	}
	from := m.Cards[src].ColumnID
	card := m.Cards[src]
	card.ColumnID = toColumn
	m.Cards = append(m.Cards[:src], m.Cards[src+1:]...) // remove

	dest := m.cardIndicesIn(toColumn) // post-removal slice indices, in order
	var insert int
	switch {
	case len(dest) == 0:
		insert = len(m.Cards) // first card of an otherwise-empty column
	case toIndex <= 0:
		insert = dest[0]
	case toIndex >= len(dest):
		insert = dest[len(dest)-1] + 1
	default:
		insert = dest[toIndex]
	}
	m.Cards = append(m.Cards, Card{})
	copy(m.Cards[insert+1:], m.Cards[insert:])
	m.Cards[insert] = card
	m.moves = append(m.moves, Move{CardID: cardID, FromColumn: from, ToColumn: toColumn})
}
