package adrboard

import (
	"testing"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban"
)

func colTitles(cols []kanban.Column) (out []string) {
	for _, c := range cols {
		out = append(out, c.Title)
	}
	return out
}

func cardByID(m *kanban.Model, id uint64) (kanban.Card, bool) {
	for _, cd := range m.Cards {
		if cd.ID == id {
			return cd, true
		}
	}
	return kanban.Card{}, false
}

func titleOfColumn(m *kanban.Model, id uint64) string {
	for _, c := range m.Columns {
		if c.ID == id {
			return c.Title
		}
	}
	return ""
}

// TestBuildBoardFilesByStatus checks each ADR lands in its status lane and that
// the canonical lifecycle lanes are present and ordered even when empty.
func TestBuildBoardFilesByStatus(t *testing.T) {
	m := buildBoard([]adrcorpus.Adr{
		{Num: 1, Title: "Accepted one", Status: "accepted"},
		{Num: 2, Title: "Proposed one", Status: "proposed"},
		{Num: 3, Title: "Superseded one", Status: "superseded"},
	})
	want := []string{"proposed", "accepted", "superseded", "withdrawn", "deferred"}
	got := colTitles(m.Columns)
	if len(got) != len(want) {
		t.Fatalf("columns: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d: want %q, got %q", i, want[i], got[i])
		}
	}
	for _, tc := range []struct {
		num    uint64
		status string
	}{{1, "accepted"}, {2, "proposed"}, {3, "superseded"}} {
		cd, ok := cardByID(m, tc.num)
		if !ok {
			t.Errorf("ADR-%d: no card", tc.num)
			continue
		}
		if got := titleOfColumn(m, cd.ColumnID); got != tc.status {
			t.Errorf("ADR-%d: want lane %q, got %q", tc.num, tc.status, got)
		}
	}
}

// TestBuildBoardUnknownStatus pins the self-healing rule: a status the corpus
// grows that the lifecycle list doesn't know still gets a lane, and an empty
// one is named rather than silently folded into another lane.
func TestBuildBoardUnknownStatus(t *testing.T) {
	m := buildBoard([]adrcorpus.Adr{
		{Num: 1, Status: "accepted"},
		{Num: 2, Status: "rejected"},
		{Num: 3, Status: ""},
	})
	got := colTitles(m.Columns)
	if len(got) != len(statusOrder)+2 {
		t.Fatalf("want %d lanes (%d canonical + rejected + no-status), got %v", len(statusOrder)+2, len(statusOrder), got)
	}
	if got[len(statusOrder)] != "rejected" {
		t.Errorf("want the unknown status appended after the canonical lanes, got %v", got)
	}
	cd, _ := cardByID(m, 3)
	if lane := titleOfColumn(m, cd.ColumnID); lane != noStatusTitle {
		t.Errorf("empty status: want lane %q, got %q", noStatusTitle, lane)
	}
}

// sub builds a Subtask with the two facts cardDots buckets on.
func sub(done bool, refs int) adrcorpus.Subtask {
	return adrcorpus.Subtask{Marker: "SD1", Done: done, CodeRefs: refs}
}

// TestCardDots covers the tally the board exists to show: three disjoint
// buckets, done first, summing to the sub-item count.
func TestCardDots(t *testing.T) {
	for _, tc := range []struct {
		name string
		subs []adrcorpus.Subtask
		want []kanban.DotTally
	}{
		{"no sub-items declared: no dots at all", nil, nil},
		{
			"nothing known: one muted run",
			[]adrcorpus.Subtask{sub(false, 0), sub(false, 0), sub(false, 0)},
			[]kanban.DotTally{{ID: dotUnknown, Count: 3}},
		},
		{
			"all declared done: one success run",
			[]adrcorpus.Subtask{sub(true, 0), sub(true, 5)},
			[]kanban.DotTally{{ID: dotDone, Count: 2}},
		},
		{
			"cited but undeclared: the amber worklist",
			[]adrcorpus.Subtask{sub(false, 3), sub(false, 0)},
			[]kanban.DotTally{{ID: dotCited, Count: 1}, {ID: dotUnknown, Count: 1}},
		},
		{
			"all three, in card order",
			[]adrcorpus.Subtask{sub(true, 0), sub(false, 9), sub(false, 0), sub(false, 0)},
			[]kanban.DotTally{{ID: dotDone, Count: 1}, {ID: dotCited, Count: 1}, {ID: dotUnknown, Count: 2}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cardDots(adrcorpus.Adr{Subtasks: tc.subs})
			if len(got) != len(tc.want) {
				t.Fatalf("want %+v, got %+v", tc.want, got)
			}
			var sum int
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("tally %d: want %+v, got %+v", i, tc.want[i], got[i])
				}
				sum += got[i].Count
			}
			if sum != len(tc.subs) {
				t.Errorf("buckets must be disjoint and total the sub-items: %d dots vs %d sub-items", sum, len(tc.subs))
			}
		})
	}
}

// TestCardDotsDoneWinsOverCited pins the precedence: a ✓ is an author's claim
// and evidence is only a hint, so a declared-done sub-item that code also cites
// counts once, as done — never downgraded to amber.
func TestCardDotsDoneWinsOverCited(t *testing.T) {
	got := cardDots(adrcorpus.Adr{Subtasks: []adrcorpus.Subtask{sub(true, 12)}})
	want := []kanban.DotTally{{ID: dotDone, Count: 1}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("want %+v (done wins), got %+v", want, got)
	}
}

// TestCardDotsWithinLegend guards the widget contract: a DotTally naming an id
// absent from the legend is silently skipped, so every id the board emits must
// be in DotLegend or the dots would vanish without a trace.
func TestCardDotsWithinLegend(t *testing.T) {
	legend := make(map[uint64]struct{})
	for _, dk := range dotLegend() {
		legend[dk.ID] = struct{}{}
	}
	for _, d := range cardDots(adrcorpus.Adr{Subtasks: []adrcorpus.Subtask{sub(true, 0), sub(false, 4), sub(false, 0)}}) {
		if _, ok := legend[d.ID]; !ok {
			t.Errorf("dot id %d is not in the legend; the widget would silently drop it", d.ID)
		}
	}
	// The widget caps a card at 3 dot *kinds*; this board only ever emits 2.
	if n := len(dotLegend()); n > 3 {
		t.Errorf("legend has %d kinds; the widget renders at most 3 per card", n)
	}
}

func TestCardSubtitle(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    adrcorpus.Adr
		want string
	}{
		{"superseded shows its replacement", adrcorpus.Adr{SupersededBy: "ADR-0100", LastDate: "2026-01-01"}, "→ ADR-0100"},
		{"otherwise the freshness date", adrcorpus.Adr{LastDate: "2026-06-20"}, "2026-06-20"},
		{"neither: empty", adrcorpus.Adr{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cardSubtitle(tc.a); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// TestCardIdsNonZero guards the widget's ParentID convention: a zero card id
// would read as "no parent" and could make every card look like its child.
func TestCardIdsNonZero(t *testing.T) {
	m := buildBoard([]adrcorpus.Adr{{Num: 1, Status: "accepted"}, {Num: 115, Status: "proposed"}})
	for _, cd := range m.Cards {
		if cd.ID == 0 {
			t.Errorf("card %q has a zero id", cd.Title)
		}
		if cd.ParentID != 0 {
			t.Errorf("card %q has a parent; this board files no sub-items as cards", cd.Title)
		}
	}
}

// TestCardIdsUniqueOnDuplicateNumbers is a regression test for a real corpus
// state: two ADRs authored concurrently can land on the same number (0119 was
// both package-capability-survey and whyprovenance-witness-columns). Card ids
// keyed on the number collided, and the widget — which scopes a card's widget
// ids by its card id — then warned on every widget inside the second card.
// Both ADRs must still get their own card.
func TestCardIdsUniqueOnDuplicateNumbers(t *testing.T) {
	m := buildBoard([]adrcorpus.Adr{
		{Num: 119, Title: "Package capability survey", Status: "accepted"},
		{Num: 119, Title: "Whyprovenance witness columns", Status: "proposed"},
	})
	if len(m.Cards) != 2 {
		t.Fatalf("want both duplicate-numbered ADRs on the board, got %d cards", len(m.Cards))
	}
	if m.Cards[0].ID == m.Cards[1].ID {
		t.Errorf("duplicate ADR numbers produced colliding card ids (%d); the widget scopes by card id", m.Cards[0].ID)
	}
	seen := make(map[uint64]struct{}, len(m.Cards))
	for _, cd := range m.Cards {
		if _, dup := seen[cd.ID]; dup {
			t.Errorf("card id %d is not unique", cd.ID)
		}
		seen[cd.ID] = struct{}{}
	}
}
