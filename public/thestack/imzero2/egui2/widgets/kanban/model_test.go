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

func TestMoveToInsertsAtIndex(t *testing.T) {
	// col2 starts [B(11), D(13)]; drop A(10) from col1 at various indices.
	check := func(idx int, want []uint64) {
		t.Helper()
		m := board()
		m.moveTo(10, 2, idx)
		if c := m.cardByID(10); c == nil || c.ColumnID != 2 {
			t.Fatalf("A not in col2: %v", c)
		}
		eq(t, "col2", m.orderIn(2), want)
		eq(t, "col1", m.orderIn(1), []uint64{12}) // C remains
	}
	check(0, []uint64{10, 11, 13}) // top
	check(1, []uint64{11, 10, 13}) // between B and D
	check(2, []uint64{11, 13, 10}) // end
	check(9, []uint64{11, 13, 10}) // clamped past the end
}

func TestMoveToSameColumnReorder(t *testing.T) {
	m := board() // col1 [A(10), C(12)]
	m.moveTo(10, 1, 1)
	eq(t, "col1", m.orderIn(1), []uint64{12, 10}) // A dropped after C
	mv := m.DrainMoves()
	if len(mv) != 1 || mv[0].FromColumn != 1 || mv[0].ToColumn != 1 {
		t.Fatalf("same-column move = %#v", mv)
	}
}

func TestMoveToEmptyColumn(t *testing.T) {
	m := NewModel(
		[]Column{{ID: 1}, {ID: 2}},
		[]Card{{ID: 10, ColumnID: 1, Title: "A"}},
	)
	m.moveTo(10, 2, 0)
	eq(t, "col1", m.orderIn(1), nil)
	eq(t, "col2", m.orderIn(2), []uint64{10})
}

func TestRollup(t *testing.T) {
	m := NewModel(
		[]Column{{ID: 1, Title: "todo"}, {ID: 2, Title: "doing"}, {ID: 3, Title: "done", IsDone: true}},
		[]Card{
			{ID: 1, ColumnID: 1, Title: "parent"},
			{ID: 2, ColumnID: 3, ParentID: 1}, // done
			{ID: 3, ColumnID: 2, ParentID: 1}, // not done
			{ID: 4, ColumnID: 3, ParentID: 1}, // done
			{ID: 5, ColumnID: 1, Title: "childless"},
		},
	)
	if done, total := m.rollup(1); done != 2 || total != 3 {
		t.Fatalf("rollup(1) = %d/%d, want 2/3", done, total)
	}
	if done, total := m.rollup(5); done != 0 || total != 0 {
		t.Fatalf("rollup(childless) = %d/%d, want 0/0", done, total)
	}
}

func TestIsDoneColumnFallback(t *testing.T) {
	// No column flagged → the last column counts as done.
	m := NewModel([]Column{{ID: 1}, {ID: 2}, {ID: 3}}, nil)
	if !m.isDoneColumn(3) || m.isDoneColumn(1) {
		t.Fatalf("fallback: only the last column should be done")
	}
	// Any flag disables the fallback.
	m2 := NewModel([]Column{{ID: 1, IsDone: true}, {ID: 2}, {ID: 3}}, nil)
	if !m2.isDoneColumn(1) || m2.isDoneColumn(3) {
		t.Fatalf("flagged: only col 1 is done")
	}
}

func TestGroupingIndices(t *testing.T) {
	m := NewModel(
		[]Column{{ID: 1}},
		[]Card{
			{ID: 10, ColumnID: 1, Title: "parent"}, // has children → parent lane
			{ID: 11, ColumnID: 1, Title: "loose"},  // childless → standalone
			{ID: 20, ColumnID: 1, ParentID: 10},
			{ID: 21, ColumnID: 1, ParentID: 10},
		},
	)
	if p := m.topLevelParentIndices(); len(p) != 1 || m.Cards[p[0]].ID != 10 {
		t.Fatalf("topLevelParentIndices = %v, want [idx of 10]", p)
	}
	if sa := m.standaloneIndices(); len(sa) != 1 || m.Cards[sa[0]].ID != 11 {
		t.Fatalf("standaloneIndices = %v, want [idx of 11]", sa)
	}
	if k := m.childIndicesOf(10); len(k) != 2 {
		t.Fatalf("childIndicesOf(10) = %v, want 2", k)
	}
}

func TestFieldLanes(t *testing.T) {
	m := NewModel(
		[]Column{{ID: 1}},
		[]Card{{ID: 10, ColumnID: 1}, {ID: 11, ColumnID: 1}, {ID: 12, ColumnID: 1}, {ID: 13, ColumnID: 1}},
	)
	owner := map[uint64]string{10: "Alice", 11: "Bob", 12: "Alice"} // 13 → ""
	lanes := m.fieldLanes(func(cd *Card) (string, string) { o := owner[cd.ID]; return o, o })
	// One lane per distinct key, in first-appearance order: Alice, Bob, "".
	if len(lanes) != 3 {
		t.Fatalf("lanes = %d, want 3", len(lanes))
	}
	if lanes[0].key != "Alice" || len(lanes[0].idxs) != 2 {
		t.Fatalf("lane0 = %+v, want Alice x2", lanes[0])
	}
	if lanes[1].key != "Bob" || len(lanes[1].idxs) != 1 {
		t.Fatalf("lane1 = %+v, want Bob x1", lanes[1])
	}
	if lanes[2].key != "" || len(lanes[2].idxs) != 1 {
		t.Fatalf("lane2 = %+v, want empty x1", lanes[2])
	}
}

func TestRollupOfIdxs(t *testing.T) {
	m := NewModel(
		[]Column{{ID: 1}, {ID: 2, IsDone: true}},
		[]Card{{ID: 1, ColumnID: 2}, {ID: 2, ColumnID: 1}, {ID: 3, ColumnID: 2}},
	)
	if done, total := m.rollupOfIdxs([]int{0, 1, 2}); done != 2 || total != 3 {
		t.Fatalf("rollupOfIdxs = %d/%d, want 2/3", done, total)
	}
	if done, total := m.rollupOfIdxs(nil); done != 0 || total != 0 {
		t.Fatalf("empty rollupOfIdxs = %d/%d, want 0/0", done, total)
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
