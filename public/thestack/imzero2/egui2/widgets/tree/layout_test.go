package tree

import (
	"strings"
	"testing"
)

// taxonomy is the fixture most tests flatten. Deliberately declared with a
// parent appearing AFTER one of its children (Felidae at index 4 parents
// Panthera leo at index 2, which is fine, but Canidae at index 6 is declared
// after its child at index 5) so every test exercises the "no ordering
// required" guarantee rather than a conveniently sorted input.
//
//	Animalia            0
//	  Chordata          1
//	    Felidae         4
//	      Panthera leo  2
//	      Panthera onca 3
//	    Canidae         6
//	      Canis lupus   5
//	Arthropoda          7
//	  Insecta           8
func taxonomy() Tree {
	return Tree{
		Labels: []string{
			"Animalia",      // 0
			"Chordata",      // 1
			"Panthera leo",  // 2
			"Panthera onca", // 3
			"Felidae",       // 4
			"Canis lupus",   // 5
			"Canidae",       // 6
			"Arthropoda",    // 7
			"Insecta",       // 8
		},
		Parents: []int32{-1, 0, 4, 4, 1, 6, 1, -1, 7},
	}
}

// labels renders a flattened outline as indented text, which is what makes a
// failure readable: a wrong depth or a missing subtree shows up as a shape,
// not as an index mismatch buried in a struct dump.
func labels(t Tree, rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.Repeat("  ", int(r.Depth)))
		b.WriteString(t.Labels[r.Node])
		b.WriteByte('\n')
	}
	return b.String()
}

func mustFlatten(tb testing.TB, t Tree, st *State) []Row {
	tb.Helper()
	rows, err := Flatten(t, st, nil)
	if err != nil {
		tb.Fatalf("Flatten: unexpected error: %v", err)
	}
	return rows
}

func TestFlattenCollapsedShowsOnlyRoots(t *testing.T) {
	tr := taxonomy()
	got := labels(tr, mustFlatten(t, tr, &State{}))
	want := "Animalia\nArthropoda\n"
	if got != want {
		t.Errorf("collapsed outline:\ngot:\n%swant:\n%s", got, want)
	}
}

func TestFlattenNilStateIsFullyCollapsed(t *testing.T) {
	tr := taxonomy()
	got := labels(tr, mustFlatten(t, tr, nil))
	want := "Animalia\nArthropoda\n"
	if got != want {
		t.Errorf("nil-state outline:\ngot:\n%swant:\n%s", got, want)
	}
}

// TestFlattenExpandedOrderAndDepth is the core guarantee: pre-order, roots and
// siblings in input order, depth one more than the parent — on an input whose
// parents are not sorted before their children.
func TestFlattenExpandedOrderAndDepth(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.ExpandAll(tr)

	got := labels(tr, mustFlatten(t, tr, st))
	want := "" +
		"Animalia\n" +
		"  Chordata\n" +
		"    Felidae\n" +
		"      Panthera leo\n" +
		"      Panthera onca\n" +
		"    Canidae\n" +
		"      Canis lupus\n" +
		"Arthropoda\n" +
		"  Insecta\n"
	if got != want {
		t.Errorf("expanded outline:\ngot:\n%swant:\n%s", got, want)
	}
}

// TestFlattenPartialExpansion pins that collapsing an interior node removes
// its whole subtree, not just its immediate children.
func TestFlattenPartialExpansion(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.SetExpanded(0, true) // Animalia
	st.SetExpanded(1, true) // Chordata
	st.SetExpanded(4, true) // Felidae — open
	// Canidae (6) stays closed, so Canis lupus (5) must not appear.

	got := labels(tr, mustFlatten(t, tr, st))
	want := "" +
		"Animalia\n" +
		"  Chordata\n" +
		"    Felidae\n" +
		"      Panthera leo\n" +
		"      Panthera onca\n" +
		"    Canidae\n" +
		"Arthropoda\n"
	if got != want {
		t.Errorf("partial outline:\ngot:\n%swant:\n%s", got, want)
	}
}

// TestFlattenExpandedLeafIsHarmless backs SetExpanded's promise that a host
// driving expansion from a search result need not check for children first.
func TestFlattenExpandedLeafIsHarmless(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.SetExpanded(2, true) // Panthera leo, a leaf

	rows := mustFlatten(t, tr, st)
	if got := labels(tr, rows); got != "Animalia\nArthropoda\n" {
		t.Errorf("expanding a leaf changed the outline:\n%s", got)
	}
	for _, r := range rows {
		if r.Expanded {
			t.Errorf("node %d reported Expanded with no children", r.Node)
		}
	}
}

func TestFlattenRowFlags(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.ExpandAll(tr)
	rows := mustFlatten(t, tr, st)

	byNode := make(map[int32]Row, len(rows))
	for _, r := range rows {
		byNode[r.Node] = r
	}

	for _, tc := range []struct {
		node                               int32
		hasChildren, expanded, isLastChild bool
	}{
		{0, true, true, false},   // Animalia — root, another root follows
		{7, true, true, true},    // Arthropoda — final root
		{1, true, true, true},    // Chordata — Animalia's only child
		{4, true, true, false},   // Felidae — Canidae follows
		{6, true, true, true},    // Canidae — last under Chordata
		{2, false, false, false}, // Panthera leo — leaf, sibling follows
		{3, false, false, true},  // Panthera onca — leaf, last sibling
		{5, false, false, true},  // Canis lupus — only child
	} {
		r, ok := byNode[tc.node]
		if !ok {
			t.Fatalf("node %d (%s) missing from the outline", tc.node, tr.Labels[tc.node])
		}
		if r.HasChildren != tc.hasChildren || r.Expanded != tc.expanded || r.IsLastChild != tc.isLastChild {
			t.Errorf("node %d (%s): got hasChildren=%v expanded=%v isLastChild=%v; want %v %v %v",
				tc.node, tr.Labels[tc.node],
				r.HasChildren, r.Expanded, r.IsLastChild,
				tc.hasChildren, tc.expanded, tc.isLastChild)
		}
	}
}

// TestFlattenForestRootsInInputOrder pins that several roots are laid out one
// after another at depth 0, in the order they appear — no virtual root.
func TestFlattenForestRootsInInputOrder(t *testing.T) {
	tr := Tree{
		Labels:  []string{"c", "a", "b"},
		Parents: []int32{-1, -1, -1},
	}
	rows := mustFlatten(t, tr, &State{})
	if got := labels(tr, rows); got != "c\na\nb\n" {
		t.Errorf("forest order:\ngot:\n%swant:\nc\na\nb\n", got)
	}
	for _, r := range rows {
		if r.Depth != 0 {
			t.Errorf("root %d at depth %d, want 0", r.Node, r.Depth)
		}
	}
	if !rows[2].IsLastChild || rows[0].IsLastChild || rows[1].IsLastChild {
		t.Error("IsLastChild wrong among roots: want only the final root set")
	}
}

func TestFlattenEmptyTree(t *testing.T) {
	rows, err := Flatten(Tree{}, &State{}, nil)
	if err != nil {
		t.Fatalf("empty tree should be valid, got: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty tree produced %d rows", len(rows))
	}
}

// TestFlattenDeepChainDoesNotRecurse walks a degenerate chain far deeper than
// a goroutine's initial stack would take recursively. It passes trivially with
// the explicit stack and is here to fail loudly if someone rewrites the walk
// recursively.
func TestFlattenDeepChainDoesNotRecurse(t *testing.T) {
	const n = 50000
	tr := Tree{Labels: make([]string, n), Parents: make([]int32, n)}
	for i := range n {
		tr.Labels[i] = "n"
		tr.Parents[i] = int32(i - 1) // node 0 gets -1
	}
	st := &State{}
	st.ExpandAll(tr)

	rows := mustFlatten(t, tr, st)
	if len(rows) != n {
		t.Fatalf("got %d rows, want %d", len(rows), n)
	}
	if rows[n-1].Depth != n-1 {
		t.Errorf("deepest row at depth %d, want %d", rows[n-1].Depth, n-1)
	}
}

func TestFlattenReusesDestination(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.ExpandAll(tr)

	dst := make([]Row, 0, 64)
	first, err := Flatten(tr, st, dst[:0])
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}
	second, err := Flatten(tr, st, first[:0])
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}
	if len(second) != tr.Len() {
		t.Fatalf("got %d rows, want %d", len(second), tr.Len())
	}
	if &second[0] != &dst[:1][0] {
		t.Error("Flatten reallocated instead of reusing the caller's backing array")
	}
}

func TestValidateRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		tr   Tree
		want string
	}{
		{
			name: "column lengths disagree",
			tr:   Tree{Labels: []string{"a", "b"}, Parents: []int32{-1}},
			want: "column lengths disagree",
		},
		{
			name: "self parent",
			tr:   Tree{Labels: []string{"a"}, Parents: []int32{0}},
			want: "its own parent",
		},
		{
			name: "dangling parent",
			tr:   Tree{Labels: []string{"a"}, Parents: []int32{7}},
			want: "out of range",
		},
		{
			name: "negative parent below -1",
			tr:   Tree{Labels: []string{"a"}, Parents: []int32{-2}},
			want: "out of range",
		},
		{
			name: "two-node cycle",
			tr:   Tree{Labels: []string{"a", "b"}, Parents: []int32{1, 0}},
			want: "parent cycle",
		},
		{
			name: "three-node cycle off a root",
			tr:   Tree{Labels: []string{"r", "a", "b", "c"}, Parents: []int32{-1, 3, 1, 2}},
			want: "parent cycle",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tr.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %v", tc.tr.Parents)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			// Flatten must refuse too, and return no partial outline — a
			// half-drawn tree is worse than an error.
			rows, ferr := Flatten(tc.tr, &State{}, nil)
			if ferr == nil {
				t.Error("Flatten accepted a tree Validate rejected")
			}
			if len(rows) != 0 {
				t.Errorf("Flatten returned %d rows for an invalid tree", len(rows))
			}
		})
	}
}

func TestExpandAncestorsRevealsWithoutOpeningTarget(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.ExpandAncestors(tr, 4) // Felidae: opens Chordata and Animalia

	if st.IsExpanded(4) {
		t.Error("ExpandAncestors opened the target node itself")
	}
	got := labels(tr, mustFlatten(t, tr, st))
	want := "" +
		"Animalia\n" +
		"  Chordata\n" +
		"    Felidae\n" +
		"    Canidae\n" +
		"Arthropoda\n"
	if got != want {
		t.Errorf("revealed outline:\ngot:\n%swant:\n%s", got, want)
	}
}

func TestExpandAncestorsOnRootAndOutOfRange(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.ExpandAncestors(tr, 0)  // a root — nothing to open
	st.ExpandAncestors(tr, -1) // out of range — must not panic
	st.ExpandAncestors(tr, 99)
	if got := labels(tr, mustFlatten(t, tr, st)); got != "Animalia\nArthropoda\n" {
		t.Errorf("outline changed:\n%s", got)
	}
}

// TestExpandAncestorsTerminatesOnCycle covers the bound in ExpandAncestors: a
// host may call it before Validate, and an unbounded parent walk on a cyclic
// tree would hang rather than misdraw.
func TestExpandAncestorsTerminatesOnCycle(t *testing.T) {
	tr := Tree{Labels: []string{"a", "b"}, Parents: []int32{1, 0}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&State{}).ExpandAncestors(tr, 0)
	}()
	<-done
}

func TestCursorZeroValueIsNone(t *testing.T) {
	var st State
	if got := st.Cursor(); got != -1 {
		t.Errorf("zero State cursor = %d, want -1 (none)", got)
	}
	st.SetCursor(0)
	if got := st.Cursor(); got != 0 {
		t.Errorf("cursor after SetCursor(0) = %d, want 0", got)
	}
	st.SetCursor(-1)
	if got := st.Cursor(); got != -1 {
		t.Errorf("cursor after SetCursor(-1) = %d, want -1", got)
	}
}

// TestCursorIsIndependentOfSelection is the property ADR-0176 SD2 carries the
// cursor for: moving one must never change the other.
func TestCursorIsIndependentOfSelection(t *testing.T) {
	var st State
	st.SelectOnly(3)
	st.SetCursor(7)

	if !st.IsSelected(3) || st.IsSelected(7) {
		t.Error("moving the cursor changed the selection")
	}
	if got := st.Cursor(); got != 7 {
		t.Errorf("cursor = %d, want 7", got)
	}

	st.SelectOnly(5)
	if got := st.Cursor(); got != 7 {
		t.Errorf("selecting moved the cursor to %d, want it left at 7", got)
	}
}

func TestSelection(t *testing.T) {
	var st State
	st.SetSelected(1, true)
	st.SetSelected(4, true)
	if st.SelectionLen() != 2 {
		t.Fatalf("SelectionLen = %d, want 2", st.SelectionLen())
	}

	st.SetSelected(1, false)
	if st.IsSelected(1) || !st.IsSelected(4) {
		t.Error("SetSelected(1,false) did not deselect exactly node 1")
	}

	st.SelectOnly(9)
	if st.SelectionLen() != 1 || !st.IsSelected(9) {
		t.Error("SelectOnly did not replace the selection")
	}

	sel := st.Selection(nil)
	if len(sel) != 1 || sel[0] != 9 {
		t.Errorf("Selection = %v, want [9]", sel)
	}

	st.ClearSelection()
	if st.SelectionLen() != 0 {
		t.Error("ClearSelection left a selection")
	}
}

func TestCollapseAllAndToggle(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.ExpandAll(tr)
	st.CollapseAll()
	if got := labels(tr, mustFlatten(t, tr, st)); got != "Animalia\nArthropoda\n" {
		t.Errorf("after CollapseAll:\n%s", got)
	}

	if !st.ToggleExpanded(0) {
		t.Error("ToggleExpanded on a closed node returned false")
	}
	if st.ToggleExpanded(0) {
		t.Error("ToggleExpanded on an open node returned true")
	}
}

func TestRowOf(t *testing.T) {
	tr := taxonomy()
	st := &State{}
	st.SetExpanded(0, true)
	rows := mustFlatten(t, tr, st)

	// Animalia, Chordata, Arthropoda.
	if got := RowOf(rows, 1); got != 1 {
		t.Errorf("RowOf(Chordata) = %d, want 1", got)
	}
	if got := RowOf(rows, 4); got != -1 {
		t.Errorf("RowOf(Felidae) = %d, want -1 (not visible)", got)
	}
}
