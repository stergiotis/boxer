package kanban

import "testing"

// orderIn returns the ids of the cards in column cid, in render (slice) order —
// the white-box view the move helpers are specified against.
func (m *Model) orderIn(cid uint64) (ids []uint64) {
	for _, i := range m.cardIndicesIn(cid) {
		ids = append(ids, m.Cards[i].ID)
	}
	return
}

// board builds a fixed three-column fixture: col1 [A,C], col2 [B,D], col3 [E].
func board() *Model {
	return NewModel(
		[]Column{{ID: 1, Title: "one"}, {ID: 2, Title: "two"}, {ID: 3, Title: "three"}},
		[]Card{
			{ID: 10, ColumnID: 1, Title: "A"},
			{ID: 11, ColumnID: 2, Title: "B"},
			{ID: 12, ColumnID: 1, Title: "C"},
			{ID: 13, ColumnID: 2, Title: "D"},
			{ID: 14, ColumnID: 3, Title: "E"},
		},
	)
}

func eq(t *testing.T, name string, got, want []uint64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
}

func TestShiftColumnLandsAtBottomOfDestination(t *testing.T) {
	m := board()
	m.shiftColumn(m.cardIndex(10), +1) // A: col1 -> col2

	if c := m.cardByID(10); c == nil || c.ColumnID != 2 {
		t.Fatalf("A.ColumnID = %v, want 2", c)
	}
	eq(t, "col1", m.orderIn(1), []uint64{12})         // A removed, C remains
	eq(t, "col2", m.orderIn(2), []uint64{11, 13, 10}) // A appended after B,D
	eq(t, "col3", m.orderIn(3), []uint64{14})         // untouched

	moves := m.DrainMoves()
	if len(moves) != 1 || moves[0] != (Move{CardID: 10, FromColumn: 1, ToColumn: 2}) {
		t.Fatalf("moves = %#v", moves)
	}
	if m.DrainMoves() != nil {
		t.Fatalf("DrainMoves did not clear the queue")
	}
}

func TestShiftColumnEdgesAreNoOps(t *testing.T) {
	m := board()
	m.shiftColumn(m.cardIndex(10), -1) // A already in the first column
	m.shiftColumn(m.cardIndex(14), +1) // E already in the last column

	eq(t, "col1", m.orderIn(1), []uint64{10, 12})
	eq(t, "col2", m.orderIn(2), []uint64{11, 13})
	eq(t, "col3", m.orderIn(3), []uint64{14})
	if moves := m.DrainMoves(); moves != nil {
		t.Fatalf("edge shifts recorded moves: %#v", moves)
	}
}

func TestReorderWithinColumn(t *testing.T) {
	m := board()
	m.reorderWithin(m.cardIndex(12), -1) // C up past A within col1
	eq(t, "col1 after up", m.orderIn(1), []uint64{12, 10})

	m.reorderWithin(m.cardIndex(12), +1) // C back down
	eq(t, "col1 after down", m.orderIn(1), []uint64{10, 12})

	// Cross-column neighbours are ignored: A is the top of col1, nothing above.
	before := m.orderIn(1)
	m.reorderWithin(m.cardIndex(10), -1)
	eq(t, "col1 top no-op", m.orderIn(1), before)

	moves := m.DrainMoves()
	if len(moves) != 2 {
		t.Fatalf("want 2 reorder moves, got %#v", moves)
	}
	for _, mv := range moves {
		if mv.FromColumn != mv.ToColumn {
			t.Fatalf("reorder move crossed columns: %#v", mv)
		}
	}
}

func TestChildCountAndLookup(t *testing.T) {
	m := NewModel(
		[]Column{{ID: 1}},
		[]Card{
			{ID: 1, ColumnID: 1, Title: "parent"},
			{ID: 2, ColumnID: 1, ParentID: 1, Title: "kid-a"},
			{ID: 3, ColumnID: 1, ParentID: 1, Title: "kid-b"},
		},
	)
	if n := m.childCount(1); n != 2 {
		t.Fatalf("childCount = %d, want 2", n)
	}
	if p := m.cardByID(2); p == nil || p.ParentID != 1 {
		t.Fatalf("cardByID(2) = %v", p)
	}
	if m.cardByID(99) != nil {
		t.Fatalf("cardByID(99) should be nil")
	}
}
